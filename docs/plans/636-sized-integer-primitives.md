# Plan — issue #636: sized integer formats lower to Scalars although the IR has primitives for them

## Behaviour

**Today.** `formatTable` (`compilers/openapi/internal/schema/schema.go:2104`) maps `integer/int32`, `integer/int64`, `number/float`, `number/double`, `number/decimal` and the string formats; nothing else. For any other pairing `scalarTypeID` (`schema.go:1462-1464`) takes the `!known` branch into `hoistFormatScalar` (`schema.go:1501`), interning an anonymous `Scalar` over `baseForType(st)` with the format verbatim as `Encoding.Name`. So `{type: integer, format: int8}` is a `Scalar{t/prim/integer, encoding "int8"}` although `ir.PrimKind` has `PrimInt8`; likewise int16, uint8, uint16, uint32, uint64, and `{type: number, format: decimal128}`.

**After.** Seven new entries make those pairs resolve exactly like int32/int64: the position returns the shared `t/prim/<kind>` primitive — no anonymous node, no `Encoding`, no diagnostic, and `formatHome` (`schema.go:781`) still reports the keyword homed through the primitive's kind.

Cases:
- Newly mapped: `integer/int8 → ir.PrimInt8`, `integer/int16 → ir.PrimInt16`, `integer/uint8 → ir.PrimUint8`, `integer/uint16 → ir.PrimUint16`, `integer/uint32 → ir.PrimUint32`, `integer/uint64 → ir.PrimUint64`, `number/decimal128 → ir.PrimDecimal128`.
- Controls, unchanged: `integer/int32 → ir.PrimInt32`, `integer/int64 → ir.PrimInt64`, `number/float → ir.PrimFloat32`, `number/double → ir.PrimFloat64`, `number/decimal → ir.PrimDecimal`.
- Deliberately **not** added: the literal format strings `float32`/`float64`. OpenAPI's spellings for those primitives are `float`/`double`, which already map, and the issue's "already maps" sentence names exactly those; the table's style maps format spellings, not TypeSpec type names. No float16/uint128 — `PrimKind` names neither.
- Fallback stays: any unlisted format keeps today's lowering — anonymous `Scalar` over `baseForType(st)` (`integer`/`number`/`bool`/`string`) with the format as `Encoding.Name`. `byte` keeps its own hoister (`schema.go:1455`). `{type: string, format: int64}` still hoists over `t/prim/string` — that orientation is #637, out of scope, and the test pins it as a boundary.
- Named component `I8: {type: integer, format: int8}` becomes a Scalar alias at its component ID over `t/prim/int8` (`lowerComponentSchema`, `schema.go:65`), same shape as `format: uuid` today; $refs resolve as before.
- `allOf` merging compares primitive kinds for equality, so a sized format against a bare `integer` now reads as narrowing exactly as int32 does — the already-documented KNOWN GAP in `internal/merge/merge.go:459-465` (#446, out of scope). No merge code changes; no committed fixture exercises it.
- Docs: `ir-design.md §4.1` lists the primitives but defines **no** format→primitive table, and `ir-spec-matrix.md` names `custom-scalars`/`encoding-hints` only generically. Docs are silent, so `formatTable` stays the single definition and its comment records the rule. Sweep result: `formatTable` is the only (type, format)→primitive table in the tree; the other `GetFormat()` call sites are password redaction (`schema.go:1205`) and binary/byte detection (`internal/operation/content.go:830,836`), untouched.

## Seams

- `compilers/openapi/internal/schema/schema.go` — `formatTable` (2102–2122) and its doc comment: the only edit. Untouched: `scalarTypeID` (1448–1470), `hoistFormatScalar` (1499–1517), `hoistContentScalar` (1519–1539), `baseForType` (2124–2136), `formatHome` (776–790).
- `compilers/openapi/internal/schema/schema_test.go` (package `schema_test`) — new table-driven test next to `TestScalar_UnknownFormatPerBaseType` (260–285), using helpers from `helpers_test.go` (`lowerSpec`, `typeByName`, `componentID`) and `openapitest.ComponentSpec` / `openapitest.PropsByWire` / `openapitest.RequireNoErrorDiags`; the mapped target comes from `ir.PrimTypeID`.
- Goldens: **none change.** No spec under `testdata/` (verified by sweep) uses any of the seven format pairs; `scalar-format.*` (unknown-format hoist) and `unhomed-keywords.yaml` (int32) stay byte-identical.
- Off limits (other worktree): `internal/auth/auth.go`, `meta.go`, `internal/schema/accumulate.go`, `internal/operation/params.go`.

## Ordered steps

1. Add the seven entries to `formatTable` at their grouped positions (integer widths after `integer`, `number/decimal128` after `number/decimal`) and extend the comment to state the rule: an integer width or decimal/decimal128 format maps to its primitive, `float`/`double`/`decimal` are the OpenAPI spellings, everything else hoists. Check: `go build ./...` and `gofmt -l` clean.
2. Add the table-driven test (suggested: `TestScalar_NumericFormatsMapToPrimitive`): one `Holder` model with a property per case; rows for the seven new pairs, the int32/int64 and float/double/decimal controls, and two fallback boundary rows (`{type: number, format: float16}`, `{type: string, format: int64}`). Mapped row asserts property target == `ir.PrimTypeID(kind)`, the node is `*ir.Primitive` with `Prim == kind`, and no `t/anon/...` node exists at the property pointer; fallback row asserts `*ir.Scalar`, base target, and `Encoding.Name` verbatim. Check: `go test ./compilers/openapi/internal/schema/ -run TestScalar -count=1` green.
3. Mutation probe: delete one new entry (e.g. `integer/uint32`), confirm the new test reddens, restore it, confirm green. This is how the test is known to catch the defect rather than describe it.
4. Golden check: `go test ./compilers/openapi -run TestConformance -count=1`; expect no diff. Only if a golden really changed, regenerate with `-update` and read the diff as a real change.
5. `make gate` (fmt, vet, lint, nolint sweeps, build, coverage-count, coverage at exactly 100%, fuzz, bench-smoke). Check: exit 0. A pre-existing gate failure unrelated to this change halts the round and is reported, not worked around.
6. Ship: rename the worktree branch to `fix/sized-integer-primitives`, commit `fix(compilers/openapi): map sized integer formats to primitives` (PR title incl. GitHub's ` (#636)` = 70 chars; verify with `printf '%s (#636)' "<title>" | wc -c`), push, and open with `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah` — body Summary / Test plan. Check: PR exists and its title is ≤72 with the number.

Verification commands named: targeted `go test ./compilers/openapi/internal/schema/ -run TestScalar -count=1`, conformance `go test ./compilers/openapi -run TestConformance -count=1`, full `make gate`.

## Deleted behaviour (closed list)

1. The anonymous-`Scalar` lowering (`hoistFormatScalar` over `baseForType`) for exactly these seven pairs: `integer/int8`, `integer/int16`, `integer/uint8`, `integer/uint16`, `integer/uint32`, `integer/uint64`, `number/decimal128`. Those positions now return the shared primitive; the path itself remains for every other format.
2. Nothing else is deleted: no test, fixture, golden, doc, diagnostic or code path is removed; the fallback for `byte`, unknown formats and `string/int64` stays, and `TestScalar_UnknownFormatPerBaseType` keeps passing unchanged.

## Report must include

- Changed paths with line ranges (`schema.go` formatTable; the new test in `schema_test.go`).
- Exact commands run and outcomes: targeted test, conformance run, `make gate`.
- Golden status: "no golden changed" or the regenerated golden plus why the change is real.
- Mutation-probe result: entry removed, test that reddened, restored green.
- Boundary statement: fallback intact, int32/int64 controls unchanged, `string/int64` (#637) untouched, and the float32/float64-spelling decision recorded.
- Branch name, PR URL, and the measured title length.
