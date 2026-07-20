# Reference Learnings — Consolidated & Prioritized (FINAL)

Synthesis of seven reference-generator deep-dives (ogen, openapi-generator, oagen, TypeSpec,
openapi-python-client, datamodel-code-generator, fastapi-code-generator) against Morphic's
`ir-design.md`, `architecture.md`, and `prior-art.md`. De-duplicated and ranked by leverage.

**How to read the verdicts.** *CONFIRMS* = a shipped generator independently arrived at Morphic's
design (or paid dearly for not having it). *CHALLENGES* = something Morphic's plan should reconsider
or explicitly guard. *Counterexample* = the generator does the opposite and its scars prove Morphic's
invariant. Every finding cites the repo(s) it came from.

The single strongest cross-cutting result: **six of seven references have no spec-agnostic,
target-neutral IR** (only Kiota/TCGC come close, and TCGC still bakes HTTP in). Every recurring
failure — name-keyed refs, eager allOf flattening, primary-response collapse, float64 bounds,
optionality≡nullability, target concepts leaking into the parser — traces back to that one missing
seam. Morphic invariant #1 ("IR is the ABI") is the root remedy; protect it with the import-graph
architecture test from milestone 1.

---

# SECTION A — FOR THE BACKEND / EMITTER DESIGN

## A0. Template vs typed-AST: what the references actually do (and what to pick)

| Repo | Emit mechanism | Structural correctness rescued by |
|---|---|---|
| ogen | `text/template`, ~25 fixed files, usage-gated | goimports post-pass; `.dump`-on-error; manual import registry (`gen/imports.go`) |
| openapi-generator | Mustache, **logic-heavy**, untyped `Map<String,Object>` | per-language `postProcessFile` (gofmt); 204 subclasses re-litigate casing/reserved words |
| oagen | **typed writers** — `Emitter` = fixed `generateX()` methods returning `GeneratedFile[]`, string concat via `mapTypeRef` | per-language `formatCommand`; no template DSL |
| TypeSpec | **migrating** imperative string visitor (`asset-emitter`) → typed component tree (`emitter-framework`) | the migration itself is the lesson |
| datamodel / openapi-python / fastapi | Jinja2 | ruff/black post-pass; fastapi even does `.replace("constr(regex=","constr(pattern=")` as a "correctness" hack |

**Verdict — CONFIRMS the plan/refine/emit split, and points to typed emit for structural code.**
The three generators that scaled cleanest either use typed writers (oagen) or are actively migrating
toward them (TypeSpec). The pure-template shops (openapi-generator, fastapi) accumulated the worst
correctness hacks — regex dead-import elimination, string-replace patches, untyped `Map` context
drift. **Recommendation for Morphic's first Go backend:** use a **typed output model / structured
writer** for everything with cross-references (type decls, method signatures, imports, forward
declarations, recursion breaking); reserve string templates for leaf/boilerplate bodies
(encode/decode). Whatever the choice, three things are table stakes, all proven across repos:
(1) a mandatory `gofmt`/formatter post-pass so templates need not be whitespace-perfect
(every repo); (2) buffer-dump-on-failure so template errors aren't opaque invalid-Go (ogen `.dump`);
(3) **never** rely on post-render string substitution for correctness (fastapi's `constr` patch is
the anti-pattern). This *confirms* architecture.md §2.3's "templates contain no policy" and the
emit-is-a-thin-layer design.

## A1. Imports/dependencies: derive from the ID graph, never accumulate as a side effect — HIGH

Every template-based repo computes imports by **mutation or text-grep**, and every one has an
import-bug class: openapi-generator's `addImport` mutates `model.imports` during lowering (dead +
missing imports recurring); fastapi greps the rendered string with `IDENTIFIER_PATTERN` to drop
unused imports; openapi-python-client threads mutable import sets via `object.__setattr__`.
**CONFIRMS** that import computation is a backend concern, and **challenges** any accumulator design.
**Recommendation:** Morphic's flat ID-keyed registry (invariant #3) makes the import set a *pure
reachability walk* over `TypeRef` targets per output file — compute it as a derived function in
refine, and **track symbol references structurally** as you emit each reference (oagen/TypeSpec do
this via a dependency connector). TypeSpec's `typeDependencyConnector` (yields base/property/variant/
tuple/param edges) + `scc-set` is the reference design for both import routing *and* topological file
ordering. Morphic can walk the same edges cheaper because they're already IDs, not live nodes.

## A2. oneOf/anyOf → Go lowering: lift ogen's decision *taxonomy* into refine, make "no strategy" a fallback not a fatal — HIGH

`ogen/gen/schema_gen_sum.go` (1200+ lines) is the richest artifact in any reference. Its union
discrimination tries, in order: (1) explicit discriminator+mapping → (2) implicit discriminator
(schema-name) → (3) type-based (`canUseTypeDiscriminator`, variants distinguishable by JSON type) →
(4) unique-field → (5) value/enum-based → (6) array-element-type. `anyOf` supports only type-based or
explicit, else `ErrNotImplemented{"complex anyOf"}`. Divergent target lowerings are real:
datamodel-code-generator emits msgspec **tagged Structs** vs Pydantic `Annotated[Union, Field(
discriminator=...)]` from the *same* oneOf; openapi-python-client emits a **try-each-variant** decoder
(O(variants), semantically wrong for overlapping variants, discriminator thrown away entirely).

**CONFIRMS invariant #2 (unions stay un-lowered in IR) emphatically** — this is target-and-
serialization-specific reasoning that has no business in the IR. **Recommendation for Morphic's Go
refine layer:** (1) port ogen's ordered strategy taxonomy as the union-strategy selector; (2) because
Morphic's `Union` node already carries `Variants[].{Name,WireName,WireID}`, `Exclusive`, `WireTagged`,
and `Discriminator` un-lowered (everything ogen's tree needs as *input*), the Go backend can **degrade
gracefully** (e.g. `json.RawMessage` / sealed-interface with a fallback) where ogen aborts the whole
spec. Make "no strategy found" a `Diagnostic` + fallback, never fatal — this is the concrete payoff of
having the ABI seam ogen lacks. (3) Preserve `Exclusive` in the decoder: oneOf must reject
multi-match, anyOf must not (datamodel/openapi-python collapse both — the bug to avoid). (4) Generate
**O(1) tag-dispatch** decoders when a discriminator exists; openapi-python's trial-decode is the
cautionary artifact.

## A3. nullable/optional/required in Go via OptT/NilT/OptNilT — STRONGLY CONFIRMS #8

`ogen/gen/ir/generics.go` models the four states as generic boxes: unboxed (required non-null),
`OptT` (optional, `Set bool` ≠ null), `NilT` (nullable), `OptNilT` (both). Crucially the boxing
decision lives in the **emitter** (`boxType`), while `jsonschema.Schema` keeps `Required []string` +
`Nullable bool` orthogonal — *exactly* Morphic's IR/refine split. Independent corroboration across the
stack: openapi-python-client's `UNSET` sentinel (four states: `int`, `int|None`, `int|Unset`,
`int|None|Unset`); datamodel-code-generator conflated the two and had to bolt on `strict_nullable`
years later (a documented one-way door); fastapi exposes `--strict-nullable` for the same reason.

**Recommendation:** `Opt`/`Nil`/`OptNil` generics are the **reference Go lowering** — implement in the
Go refiner, not the IR. Also absorb ogen's `NilSemantic` optimization (`gen/ir/nil_semantic.go`): for
slices/maps/pointers, encode one of the four states in the type's *native nil* (`NilInvalid`/
`NilOptional`/`NilNull`) instead of adding a wrapper — a real ergonomic win, and precisely the kind of
target-specific decision that belongs in refine. Keep `Property.Required` ⊥ `TypeRef.Nullable`
orthogonal in the IR; never let one imply the other (openapi-generator attaches `isNullable` to the
type object not the usage-site — Morphic's placement on `TypeRef` is strictly better and must hold).

## A4. Discriminators: keep typed, generate tag-dispatch, synthesize the implicit tag property — HIGH

Confirmed necessary by every union-capable reference. TypeSpec is the sharpest cross-check: it treats
**discriminated models (inheritance) and discriminated unions as two mechanisms** sharing a
discriminator concept, and it *synthesizes the discriminator property into the schema when it's
implicit* (`openapi3/schema-emitter.ts` 389–393). ogen's `implicitDiscriminatorKey` prefers a
variant's own const field value over the schema name; ogen also resolves the OpenAPI bare-name-vs-
JSON-pointer mapping ambiguity (try `#/components/schemas/<name>` first, then pointer). openapi-
generator triple-stores name variants (`mappingName`/`modelName`/`schemaName`) to paper over name-
keying, and hides two incompatible interpretations behind `legacyDiscriminatorBehavior` (the mess to
avoid — model facts, let backends decide). fastapi *drops* the discriminator when a variant is a
non-object because Pydantic can't express it — **target capability leaking into the frontend**, the
exact anti-pattern.

**Recommendation:** Morphic's `Discriminator{Property | PropertyName | Index, Mapping map[string]
TypeID, Default, Envelope, EnvelopeValueName, Inferred}` already covers all of this and points mappings
at **TypeIDs not names** (severing openapi-generator's triple-name coupling). Two concrete backend
obligations the references surface: (1) the plan/emit layer **must be able to materialize a
discriminator property that is not declared** on the base (`Discriminator.PropertyName` deliberately
has no PropID) — TypeSpec proves this is required; (2) the "drop discriminator for non-object variants"
decision belongs in the Go/Python refiner **with a diagnostic**, never the frontend (fastapi's
mistake). Copy ogen's mapping-ref resolution fallback into the frontend.

## A5. Pagination & streaming: plan-layer traversal over un-lowered IR — CONFIRMS

oagen's `OperationPlan` computes `isPaginated`, `paginatedItemModelName`, `isArrayResponse` once per
operation; `extractModelName` walks the TypeRef collapsing nullable→inner, array→items, union→first
model variant (a lossy ergonomic shortcut correctly made *in the plan layer*, not the IR). TypeSpec/
TCGC keep paging as *metadata* (property **paths** from response root, not top-level names) so next-
links/items can live inside envelopes. **Recommendation:** Morphic's `Pagination` (path-based
`InputCursor`/`NextCursor`/`NextLink`/`Items` as `PropPath`s, multiple strategies) is richer than all
references — keep it; do the primary-model TypeRef walk in **plan** (like oagen §A2), never pre-flatten
in IR. Add oagen's two plan predicates Morphic's §2.3 list omits: **`pathParamsInOptions`**
(positional-vs-options-bag: `pathParams>1 || (pathParams>0 && (hasBody||hasQuery))`) and
**`isArrayResponse`** (unpaginated array→`Model[]` + elementwise decode, distinct from the pagination
wrapper). Streaming stays on the operation core (`StreamDetail`/`Variant.Event`) — Kiota's lack of any
streaming concept is the counterexample.

## A6. Naming / casing / reserved words: neutral in IR, rendered in refine — STRONGLY CONFIRMS #3/#4

Unanimous across all seven. TypeSpec refines it best: a swappable `TransformNamePolicy` with **two
channels** — `getTransportName` (wire) and `getApplicationName` (idiomatic) — and casing itself is a
trivial emitter function. ogen keeps only source names in the parsed model; every Go identifier is
produced late (`namer().pascal`, initialism rules a *feature flag*). openapi-generator is the
counterexample: it eagerly stores `nameInCamelCase/PascalCase/SnakeCase` + `dataType` on the shared
model, so casing/acronym/reserved-word bugs are re-litigated in **204 subclasses**. Reserved-word
escaping is per-language data + a hook everywhere (openapi-generator's `escapeReservedWord` throws by
default; Go appends `_`).

**Recommendation:** store `Naming{Source, Canonical (neutral word sequence), Wire (+numeric ID)}` and
**nothing cased** — validated by all. Model the backend name resolver as TypeSpec's two-function
policy (wire namer reads `WireName`; application namer reads `Canonical` + per-language casing +
reserved words), injectable per backend (invariant #6). Give anonymous-type `Hint`s enough parent+role
context to be legible (ogen: `FooItem`, `UserAdditional`). **CHALLENGES to guard:** oagen froze a
snake_case method-name deriver + English singularizer/verb-list into its shared layer-0
(`operation-hints.ts`), un-swappable per target — Morphic must run the verb/singularization heuristic
as **injectable policy marked `Inferred`**, never a shared frozen function. Route per-language name
overrides through overlays keyed by **stable ID** (openapi-generator/openapi-python key by name →
collisions; Morphic's fix).

## A7. Multi-file output + regeneration/manifest/integration — CONFIRMS §2.3 point-for-point

This is oagen's most valuable contribution (it shipped it for ~9 languages) and every reference
touches some slice of it:

- **Generation manifest** (oagen `.oagen-manifest.json` v2, openapi-generator `FILES`+`.openapi-
  generator-ignore`): `{version, language, specSha, emitterSha, configSha, files:[sorted],
  operations: Record<key,{sdkMethod,service}>}`. **Header-gated pruning**: delete a stale file *only
  if its content starts with the auto-generated header*, else preserve + report as hand-edited;
  **skip pruning entirely on first adoption**. Adopt this schema directly. Morphic's one improvement
  (already specced): key the entity→symbol map by **IR stable ID**, not oagen's `"METHOD /path"`
  string, so it survives path/name churn. Record `service` per op (oagen needs it for scoped runs).
- **Scoped regeneration NEVER filters the spec** (oagen `orchestrator.ts:37`, `generate-files.ts:44`;
  fastapi `--specify-tags`; openapi-python full-rmtree). Placement/dedup/shared-files/barrels are
  computed over the *full* (or `selected ∪ already-on-disk`) surface so shared files stay byte-
  identical; only per-service emission is gated. Reachability from **selected operations only** (a
  service can split across mount targets). This is exactly architecture.md §2.2's recorded lesson —
  confirmed real and subtle. Carry a `scope` + `presentEntities` set into the backend.
- **AST additive merge into live repos** (oagen `merger.ts`): parse existing+generated (tree-sitter),
  append only absent top-level symbols, **never modify/remove hand-written code**; honor
  `@ignore-start/@ignore-end` islands; preserve `@deprecated`. Plus file→directory conflict repair
  (`foo.py` shadowed by `foo/__init__.py` → move to `foo/_compat.py` + re-export). fastapi/openapi-
  python's substring-grep merge signals (`"app.include_router" in main.py`) are the brittle anti-
  pattern — Morphic needs **principled managed regions**, not string sniffing.
- **Staleness detection** = (old-spec entities − new-spec entities) ∩ live surface (oagen
  `staleness.ts` — needs BOTH specs to distinguish hand-written from removed-spec symbols). This is
  the dead code additive-merge can't prune. Reinforces caching serialized IR per revision (#7).
- **Formatter runs last; generated tree is disposable** (openapi-python `rmtree`+ruff; all repos).
  Removes enormous template complexity — declare an optional per-backend format post-step.
- **Barrel/re-export file** (`__init__.py`/`mod.rs`/`index.ts`) is a recurring explicit artifact.
- Tree-sitter-per-language merge (oagen) silently no-ops on missing grammar — Morphic (Go) must decide
  deliberately: bundle grammars, shell to the target toolchain, or whole-file overwrite — and make the
  fallback **explicit, not silent**.

TypeSpec adds the emit-layout primitive: model output as a **scope tree** (source-file → namespace →
declarations) with per-file `imports`, resolve cross-file refs by common-ancestor diff
(`ref-scope.ts`), and keep a free `SourceFile.meta` bag for manifest hashes.

## A8. Runtime SDK policy is a separate backend input, NOT IR — CONFIRMS #10 (unanimous)

Counterexamples everywhere: ogen bakes otel/retry/middleware into generated code via feature flags +
a vendored runtime; openapi-generator smears retry/auth/timeout across `additionalProperties`/`CliOption`
bags consumed by templates; datamodel has ~200 CLI flags instead. **oagen is the positive model** and
the vocabulary source: `SdkBehavior` (layer-0, imports nothing) covers `retry` (statusCodes, backoff
{initialDelay,multiplier,maxDelay,jitter}), `errors` (statusCode→class map, doc-URL template),
`telemetry`, `pagination.autoPageDelayMs`, `idempotency`, `logging` (closed lifecycle-event union),
`userAgent` (with **`aiAgentEnvVars`** — detect Claude Code/Cursor/Cline via env vars — and
`requestGuard.optionKeys`), `timeout`. Merge semantics: **arrays replace, objects merge recursively**.
**Recommendation:** lift oagen's field list wholesale as Morphic's policy-input vocabulary (add the
non-obvious `aiAgentEnvVars` + `requestGuard.optionKeys`), adopt the array-replace/object-merge rule,
but keep it a **separate backend input, not a `Document` field** — oagen's one violation
(`ApiSpec.sdk` on the IR root) is why it can't cleanly drive docs/mocks from one IR. Precedence rule
(oagen §A4, openapi-python): **declared IR facts win; policy fills silence** — plan reads
`ErrorCase.Retryable`/`Operation.Idempotency` first, consults policy only when the IR field is nil.

## A9. Compat / surface-verification (future diff pass) — CONFIRMS §2.3/§4

oagen's ~20-file compat subsystem is the reference: (1) neutral change-category enum (`symbol_removed`,
`parameter_renamed`, `parameter_type_narrowed`, `constructor_reordered`, …) with default severity
**overridable by per-language policy** (`methodParameterNamesArePublicApi`, `constructorOrderMatters`,
per-param `passing` style) — "breaking-ness is per-language, not a property of a change" (breaking in
PHP, invisible in JS). (2) `conceptualChangeId` for cross-language rollup — key it to Morphic's
**stable ID** (oagen keys by spec-name, its `id`-vs-name dual-key rename branch is *dead* because id
derives from name). (3) `CompatProvenance` bucket (spec_shape vs emitter_template vs operation_hint vs
normalization) for CI triage. (4) Behavioral changes (`default_value_changed`) as a **separate soft-
risk channel** from structural. (5) Structured expiring approvals per (symbol×category) with reason —
Morphic's §4 (code×entity-ID, expiry, no wildcards) is stricter; keep it. The comparison plane is an
extracted **SDK surface model**, not the IR — nothing in the whole subsystem needed info Morphic's IR
lacks (the strongest validation the oagen audit produced).

## A10. Sealed sums, recursion breaking, memoized emit — CONFIRMS Go conventions

- **Sealed sum via `Kind()` + switch, generated exhaustiveness test.** oagen (`assertNever`), TypeSpec
  (`EmitEntity = Declaration|RawCode|NoEmit|CircularEmit`), and openapi-generator's ~120-boolean flag
  bag (the counterexample: no compiler check, every new kind touches every template) all confirm.
  ogen's *fat single `ir.Type` struct* with `// only for X` fields + `panic(unexpected kind)` is the
  cautionary middle ground. **Ship Morphic's per-kind-struct + generated switch-completeness test in
  milestone 1** — ogen's panics are exactly the runtime failure the test prevents. Also ship
  `walkTypeRef`/`mapTypeRef` generic-traversal helpers (oagen leans on them constantly).
- **Recursion is a backend problem, solved at emit.** Morphic's ID-referenced registry makes the IR
  cycle-free (invariant #3) — vindicated against ogen's whole recursion module (`reflect.DeepEqual`
  cycle finder + hard errors on required recursive fields), datamodel's SCC cross-module relocation,
  and openapi-python's two-phase fixpoint. But the Go **backend still must break emitted-output cycles
  with pointers**, and should **degrade required-recursive fields to pointer, not error** (ogen
  errors). Adopt TypeSpec's Placeholder/back-patch mechanism (`asset-emitter/placeholder.ts`) with
  named declarations as cycle break-points, + SCC grouping so mutually-recursive types land in one
  file.
- **Memoize projections on an interned key.** TypeSpec caches emit on interned `(method, type,
  context)`; it splits **lexical vs reference context** (decl-site context stays, use-site context
  propagates across ref edges) — Morphic's plan hits this the moment one model is used under two
  lifecycles/media-types. Key visibility/shape projections on `(TypeID, lifecycle-set, options-hash)`
  and intern the option set (answers ir-design open-question #4: compute Usage once, store, key by
  interned context). Support `manuallyTrack`-style usage injection for backend-synthesized types
  (envelopes, page wrappers) or you get mis-scoped duplicate declarations.

## A11. Error-response collapse, dedup equality, encode/decode pairing — plan-layer patterns to steal

- ogen `reduceDefault` (opt-in `ConvenientErrors`): hoist an identical `default` response shared across
  all ops into one package-level error type — a **plan-layer ergonomic collapse**, toggleable (matches
  #6). Its `responseComparator` uses a `_ = (*check)(a)` compile-time guard so the comparator fails to
  build if a struct field is added but not compared — a nice pattern for Morphic's dedup equality code.
- Model **encode-to-wire and decode-from-wire as explicit paired plan operations per type** (openapi-
  python's `construct`/`transform` macro pair) — the pairing is what keeps round-trip serialization
  correct. Generate them from Morphic's `Encoding{Name, WireType, MediaType}` triple.
- Reachability must **keep discriminated/union bases as public surface even when no operation returns
  them** (oagen `generate-files.ts:167` — event/webhook envelopes) or the `filter` pass drops variant
  structs and produces dangling dispatchers. Morphic's reachability trim needs this carve-out.

---

# SECTION B — FOR THE IR & OPENAPI FRONTEND

## B0. The headline: frontends never flatten — CONFIRMED by every reference's scars

allOf eager-merge appears in **all six** non-Kiota references and is irreversible every time:
openapi-python `merge_properties` (two regexes = fatal, "required wins", arbitrary description-override
order); ogen `flattenAllOfSchema`/`mergeSchemes` (conflicts → `ErrNotImplemented`; discriminator-merge
unimplemented; base/mixin/conformance all lost); openapi-generator collapses to one `parent` string +
merged `allVars`; datamodel has **~6 allOf strategies × a dozen interacting flags**
(`allof_merge_mode`×`allof_class_hierarchy`×…) that "metastasized into a documented bug source"; oagen
`extractAllOfModel`. **openapi-generator even flattens in the frontend by default** (`OpenAPINormalizer`
turns `SIMPLIFY_ONEOF_ANYOF`, `REFACTOR_ALLOF_WITH_PROPERTIES_ONLY` **on** before any language is
chosen). This is the single sharpest contrast with invariant #2.

**Recommendation (unchanged, now overwhelmingly evidenced):** classify allOf per §4.3 (sole-$ref/
discriminator-participant → `Base`; other $refs → `Mixins`; inline → `Properties` with provenance),
**never merge**; keep `FlattenedProperties()` computed, never stored (openapi-generator's
`allVars`-beside-`vars` is the storage smell). TypeSpec independently validates the split with distinct
`sourceModels[].usage: "is"|"spread"|"intersection"` provenance — **consider a `MixinKind`/provenance
tag on each `Mixins` entry** so a backend can tell structural-copy (`is`/spread) from compose-by-
reference. Harvest the merge algorithms as **backend-refiner** material (below). Any convenience
simplification lives in `pass/` (composable, order-explicit, default OFF, provenance-recording), never
a default frontend mutation.

## B1. Nullable normalization: fold every spelling to one bit — CONFIRMS #8/§3.3

ogen (`extendInfo`, `handleNullableEnum`), oagen (`schemas.ts:1110`), openapi-python
(`Schema.handle_nullable` pydantic validator rewriting 3.0 `nullable`→3.1 `[T,"null"]`), TypeSpec
(reverse: OpenAPI emitter collapses `T|null` back to a bit) all normalize 3.0 `nullable` / 3.1
`type:[T,"null"]` / `x-nullable` to one internal signal. A `oneOf/anyOf` whose only extra arm is
`null` collapses to a plain nullable — **Morphic already legislates this (§3.3), and it's the exact
spot ogen left unfinished** (its TODO "convert oneOf+null into generic"). Do the 3.0↔3.1 reconciliation
*inside the frontend before IR lowering*, emit an `info` diagnostic recording the original spelling.
**Guard:** openapi-python's `allOf`→`oneOf[null,allOf]` trick mixes null with composition — Morphic
must instead set `TypeRef.Nullable=true` on the reference to the composed model and invent **no union**
(§3.3). **Edge case from TypeSpec (B4):** a union whose *only* member is `null` (bare-null) has no
nullable-bit home — lower it to `Literal{Value:null}` (or `Any`+Nullable) + diagnostic, or the frontend
emits a `TypeRef` with no non-null target. Cover the full four-state matrix (required×nullable) in the
conformance corpus — datamodel needing `strict_nullable` years late is the argument for day-one.

## B2. No float64 anywhere — CONFIRMS the BigVal rule, with a subtle warning

TypeSpec's `Numeric` (`{n:bigint,e,d,s}`, `asNumber(): number|null` returning **null on precision
loss**) is the cited source lesson — adopt its "convenience-but-never-authoritative" pattern (any float
accessor returns null/error, never silently rounds). Counterexamples: openapi-generator stores
`multipleOf` as `Number`/float64 (but min/max as `String` — the *inconsistency* is itself a lesson,
make it uniform); openapi-python parses `maximum`/`multipleOf` as Python `float`; datamodel uses
`UnionIntFloat`; oagen stores enum/const/default as `any` (int64/float64). **Subtle warning from
ogen:** even though it *stores* `jx.Num` (decimal) correctly, its allOf merge `minNum`/`maxNum` calls
`Float64()` to compare bounds — **storing BigVal is necessary but not sufficient; every compare/merge/
dedup must stay on decimal/`big.Rat`.** Also from TypeSpec: `EnumMember.value?: string|number` is a
lossy hole *inside* an otherwise-precise system — Morphic's typed `Value` on enum members is strictly
better; do **not** add a numeric convenience field. Corpus: bounds like `1e308` / `0.1` / int64-max
that float corrupts.

## B3. Reference resolution & stable IDs — CONFIRMS #3, with the exact keying to adopt

ogen keys resolution by `RefKey{Loc, Ptr}` (file + JSON pointer), caches, shares `*Schema` by identity,
stamps `.Ref`; a `ResolveCtx` ref-stack detects cycles; generic `resolveComponent[Raw,Target]` avoids
per-kind duplication. openapi-python keeps *two* registries (`classes_by_reference` by $ref path =
identity, `classes_by_name` = presentation) but name-collisions are **fatal** (`"duplicate models with
name X"`). **Adopt `RefKey{file, jsonpointer}` → `TypeID` verbatim** for Morphic's ID construction
(`t/openapi/components/schemas/User`); use the two-map idea only as *frontend bookkeeping* (source
pointer → TypeID), never as IR storage. Morphic's ID-sole-key design means renames never collide — the
`duplicate name` fatal error simply cannot occur. Keep `operationId` as `Naming.Source` (display) but
derive identity from the source pointer (fastapi confirms the two roles are distinct; it injects
synthetic IDs for callbacks whose path is a runtime expression).

## B4. Diagnostics — CONFIRMS #5, with three upgrades over the references

openapi-python's typed `ParseError`/`PropertyError` (level/header/detail/provenance `data`, bubble as
return values, engine prints + sets exit code) and ogen's `location.Error`/`MultiError` are the model.
**Three upgrades Morphic should make:**
1. **Stable string codes** (Morphic already has `Code: "openapi/unresolved-ref"`). openapi-python has
   only free-text headers, so its *tests regex-match warning prose* — brittle. Corpus must assert
   diagnostic **codes**, not text.
2. **Multi-provenance per diagnostic** (ogen `MultiError` points at *two* spans — e.g. duplicate enum
   value at both occurrences). Single-provenance would be a step back for collision/conflict diagnostics
   (field-name clash pointing at both definers).
3. **Partial IR + degrade, never destructive removal.** openapi-python `_propogate_removal` deletes a
   bad schema *and everything depending on it*; fastapi silently degrades form/octet-stream bodies and
   drops discriminators with **no diagnostic**. Morphic's lossless stance: a node with an
   unrepresentable sub-part degrades + diagnoses, never vanishes; **every** lossy lowering emits a typed
   diagnostic.
Also add a configurable depth/size limit surfaced as a fatal diagnostic (ogen's depth-1000 typed-panic-
recovered-at-boundary — but keep it inside the pure stage, don't let the panic escape). And **purity**:
datamodel's module-global `activeSchemaNameTransform`, oagen's file-scoped `let`s, fastapi's
`_temporary_operation` mutation + `self.results.remove` during parse all violate reentrancy — Morphic
frontends must thread state through args/context, never package globals (invariant #5, concurrent
parses).

## B5. Keep the full capability surface even when the first backend won't use it — CONFIRMS #9

Repeatedly, references *drop* what a richer backend needs: openapi-python captures constraints then
ignores them (only `format` drives type choice), drops `patternProperties`/`unevaluatedProperties`/
non-string keys, has no open/closed enum bit; oagen/openapi-generator/datamodel have no enum openness;
fastapi strips `x-*` from `info` (a losslessness violation). Morphic must keep `Constraints`,
`Enum{Closed,Flags,FallbackMember,ValueType}`, `AdditionalProps{Value,Key,Patterns}` + `AdditionalMode`,
`Exclusive`/`WireTagged`, `x-*` everywhere — the OpenAPI frontend sets `Enum.Closed=true` (JSON-Schema
default) but the bit exists for Smithy(open)/protobuf/Avro(fallback) frontends. Enum specifics: allow
**duplicate member values** (protobuf `allow_alias`) — ogen's hard rejection is the thing NOT to copy
into the shared validate pass; capture `x-enum-varnames` member names (openapi-python) so backends
don't invent `VALUE_1`. TypeSpec confirms `additionalProperties:false`→`Additional=closed`,
`unevaluatedProperties:false`→`closed_after_composition`, and typed map keys (`indexer.key: Scalar`).

## B6. Heuristics are injectable, `Inferred`-marked policy — CONFIRMS #6, with cautionary tales

Every reference bakes heuristics *un-disableable and un-marked*: oagen's ~600-line discriminator
inference + pagination name-lists + snake_case method deriver are "so load-bearing and entangled with
naming they can't be turned off or audited"; openapi-generator steers behavior via `x-*` extensions as
*scattered inline control flow* (`if extensions.contains(...)`), unauditable; datamodel's dozen
interacting flags. **Recommendation:** implement the *recipes* (they're genuinely useful) but as
**injectable policy that sets `Inferred=true` and keys output by TypeID**:
- **Discriminator inference** — const-property-on-every-variant detection, single-string-const-union →
  enum collapse (oagen/datamodel/ogen `implicitDiscriminatorKey`). Disableable, auditable.
- **Pagination** — cursor if a query param ∈ {cursor, after, before, starting_after, ending_before,
  page_token, next_token} (priority-ordered so `after` wins), offset if offset+limit present (oagen
  `pagination.ts`). Target Morphic's path-based fields.
- **List-envelope detection** (oagen `detectListEnvelope`) — object with one array prop + pagination-
  metadata companions; known data-paths {data,items,results,records,entries,values,nodes,edges}. This
  is **inference that must be structural-not-destructive**: oagen unwraps unconditionally at parse time
  (mutates IR shape, no opt-out, no marker). Morphic keeps the wrapper model and records
  `Pagination.Items` as a `PropPath`; unwrap only in a refiner/opt-in policy.
- **Type inference from sibling keywords** when `type` absent (ogen `inferTypes`) — implement, but
  **stamp `Provenance.Inferred`** (ogen doesn't — a cheap place Morphic is strictly better).
Treat `Extensions` as **preserved data**; route any *behavioral* use through explicit `Inferred`-marked
policy/overlays, never inline `x-*` branches. **Do NOT put target-language directives in the IR/spec**
(ogen's `x-ogen-type`=Go type, `x-oapi-codegen-extra-tags`=Go tags couple spec to one target) — route
per-target naming/type overrides through per-backend overlays keyed by IR ID; `x-ogen-name` (a naming
hint) is the one neutral exception → maps to `Naming`.

## B7. Single hoist pass keyed by source pointer, Hint not name — CONFIRMS §2.1 phase 3

The clearest validation in the whole survey: oagen hoists inline types across **~6 functions**
(`collectNestedInlineModels`, `extractNestedSchema`, `nameVariantModel`, …) each independently
synthesizing names with `Foo2`/`Foo3` numeric fallback and "keep the model with more fields" collision
reconciliation; datamodel materializes inline models eagerly during `parse_item` (which is *why* it
needs later dedup/rename passes); openapi-generator's `InlineModelResolver` (62KB) dedups by *fragile
signature strings* stripping volatile fields. **Recommendation:** hoist in **ONE pass keyed by source
JSON pointer → TypeID**, store a `Naming.Hint` (not a final name) computed once from context, never
re-derive elsewhere. The **dedup pass** (content-hash + retained ID **aliases**) replaces oagen's
"more fields wins" — record aliases, never rewrite refs (datamodel/openapi-python's destructive
delete/rename loses provenance; fastapi's `reuse_model` flag is weaker than an alias-retaining pass).
Snapshot-test that alias provenance round-trips.

## B8. Layer the frontend, share JSON-Schema lowering, keep the IR seeing only the result — CONFIRMS milestone 2

datamodel subclasses `OpenAPIParser(JsonSchemaParser)` with a `SCHEMA_PATHS` override; fastapi
subclasses datamodel's parser and adds only the operation layer; openapi-python parses into a full
typed pydantic spec model *first*, then lowers. **Recommendation:** Morphic's OpenAPI frontend should
parse into a typed spec model (Go structs / schema lib) as a distinct stage, then lower to IR — two
clean stages beat dict-walking, and a shared JSON-Schema lowering **core** serves OpenAPI/Swagger/
AsyncAPI frontends (they layer the envelope). Keep it shared *frontend* code, **never shared IR**.
Steal openapi-python's targeted error hints ("you may be using Swagger 2.0", wrong-document-kind) as
typed diagnostics. Keep the ref-site-vs-target annotation merge as **one uniform rule with use-site
precedence** (TypeSpec's OpenAPI emitter is the confirming mirror; openapi-generator patches it ad-hoc
for parameters only and loses target defaults on $ref-typed fields — the bug).

## B9. $ref + sibling keywords, $dynamicRef, validation-only constructs — frontend edge-case recipes

- **`$ref` + sibling keywords** (2020-12 legal): datamodel distinguishes "`$ref` + nullable only" (use
  ref directly + Optional — Morphic's `TypeRef{Target, Nullable}` handles this with **zero duplicate-
  model risk**, a clean win) from "`$ref` + real keywords" (extend the ref: Base/Mixin + local
  overrides, **use-site wins** — preserve both ref identity and overrides losslessly, don't merge-copy).
- **`$recursiveRef`/`$dynamicRef`**: datamodel's `_resolve_recursive_ref`/`_resolve_dynamic_ref` +
  anchor-index (nearest enclosing `$recursiveAnchor`, outermost `$dynamicAnchor`) is a working
  reference implementation of ir-design §4.7's "resolve per site by frontend expansion (dynamic scope
  is static per document)". Port the anchor-index logic; add Morphic's promised "irreducible cases
  preserved verbatim + diagnostic" fallback (datamodel just best-effort resolves).
- **Validation-only (`not`/`if-then-else`/`dependentSchemas`)**: §4.7 carve-out — preserve **verbatim
  in `Extensions`** + one `info` diagnostic; do **not** model structurally. datamodel's
  `_merge_conditional_properties` (folds then/else props up as optional) and openapi-generator's ad-hoc
  mix (some fields, some extensions, some dropped) both **silently change the shape** — Morphic's
  verbatim approach is cleaner. Keep the one carve-back: `unevaluatedProperties:false` →
  `Additional=closed_after_composition`. Corpus: assert "preserved-not-modeled".

## B10. Latent frontend bug #1 — named scalar component registering no node → dangling $ref

**The bug.** A named component that is a plain scalar — e.g. `components/schemas/UserId: {type:
string, format: uuid}` or even `{type: string}` — gets resolved *through* to the shared primitive/scalar
at frontend time, so **no node is registered at its own source pointer** `#/components/schemas/UserId`.
Any `$ref: '#/components/schemas/UserId'` elsewhere then resolves to a TypeID with no registry entry →
dangling ref (`ir/dangling-type-ref` fires, or worse, silently).

**How the references avoid it — all three ensure the ref target always exists:**
- **datamodel-code-generator**: materializes a named scalar component as a **Pydantic root model**
  registered in `ModelResolver.classes_by_reference` keyed by the $ref path. Every named component gets
  a registry entry at its pointer — the target of `$ref` is *always* present. (`parser/jsonschema.py`
  root-type handling; `reference.py` ModelResolver.) It never resolves-through.
- **ogen**: reference resolution is **by pointer identity to the parsed `*Schema`**, which always
  exists (`RefKey{Loc,Ptr}` cache, `.Ref` stamped). A named `type:string` schema stays a real resolved
  object; ogen may generate `type UserId string` or inline the primitive at the *emitter*, but the
  resolution target never disappears — there is no separate "registry entry that might be missing".
- **openapi-generator**: maintains an explicit **`typeAliases` map** (`DefaultCodegen.typeAliases`)
  precisely so a `$ref` to a primitive-alias component resolves to the primitive instead of dangling.
  The alias need not be a `CodegenModel`, but it *is* in the alias table, so resolution never fails.

**Fix for Morphic (and it's the clean one given the IR):** the frontend must **register a `Scalar`
node (§4.2) at every named component's pointer-derived TypeID**, even when it trivially aliases a
primitive — `Scalar{Base: <primitive TypeRef>}`. Do **not** resolve-through in the frontend; resolving
a scalar chain "to the nearest representable base" is explicitly a **backend refiner** job (§4.2). This
matches ogen and datamodel (register the node) and is cleaner than openapi-generator's side alias-table
(Morphic gets the alias-table's guarantee for free because every pointer maps to a registered node).
Belt-and-suspenders: keep the `validate` pass's `ir/dangling-type-ref` check so any future
resolve-through regression is caught by a golden/corpus test. **Corpus item:** a named
`{type:string,format:uuid}` component referenced from two sites — assert a `Scalar` node exists at its
pointer and both refs resolve to it.

## B11. Latent frontend bug #2 — allOf + oneOf co-occurrence dropping allOf

**The bug.** A schema carries **both** `allOf` and `oneOf` (or `anyOf`) at the same level. Semantics:
the value must satisfy **all** allOf members **AND** exactly one oneOf member (composition ∧ union). A
frontend that dispatches on "saw oneOf → emit `Union`, return" **silently drops the sibling allOf**,
losing the base/mixin composition entirely.

**How the references handle co-occurrence — the two correct shapes, both retain allOf:**
- **datamodel-code-generator** — `parse_combined_schema` (jsonschema.py:3555) **deep-merges the
  enclosing schema's sibling keywords into each oneOf/anyOf branch** (`_deep_merge`, 3594): every
  non-`$ref` branch absorbs the outer keywords, so a sibling `allOf` is *distributed into each variant*.
  Each variant becomes `allOf ∧ branch` — semantically exact, allOf never dropped. (It also has
  `detectAllOfVariantDiscriminator` for the specific `allOf:[base,{oneOf}]` nesting.) This is the
  reference for *semantic* correctness.
- **openapi-generator** — `updateModelForComposedSchema` + the parent-selection heuristic ("inspect
  allOf, then anyOf, then oneOf; if one is a discriminator use it, else the first"): allOf becomes the
  `parent`/merged `vars`, while oneOf is retained in the `oneOf` set / `composedSchemas`. Both survive —
  allOf as inheritance, oneOf as the union. (Lossy in *other* ways — it merges allOf — but it does not
  *drop* it.)
- **ogen** — AND-merges allOf **sibling** keywords when composition co-occurs (`hasSiblingConstraints`:
  "allOf with sibling keywords must be AND-merged"), and collects oneOf as sum variants; genuinely
  complex allOf+oneOf combinations it can't reconcile hit `ErrNotImplemented` rather than silently
  dropping — a hard stop, not silent loss.

**Fix for Morphic (lossless, per §4.3/§4.4):** when allOf and oneOf/anyOf co-occur, the frontend must
process **both** — never return early on the combinator. Two lossless representations, in preference
order:
1. **Distribute the allOf composition across the Union variants** (datamodel's semantics, kept
   lossless not merged): emit a `Union` whose each `Variant.Type` references a `Model` carrying the
   allOf classification (`Base`/`Mixins`/inline-`Properties` per §4.3) composed with that variant. The
   `Union` is the value; the composition rides on every variant. This is the most faithful to the
   `∧`-of-`∨` semantics.
2. If the allOf is pure inheritance and the oneOf is the *discriminated subtype set* of that base
   (the common `allOf:[base] + oneOf:[subtypes]` idiom), classify allOf → `Base` on the enclosing
   `Model` and attach the `Discriminator` + subtype mapping (§4.3), so it's one polymorphic model, not
   a synthetic union.
Either way the allOf is **classified, never merged and never dropped**. Emit an `info` diagnostic
recording the co-occurrence lowering chosen (provenance). **Corpus item (this is the exact regression):**
`{allOf: [{$ref: Base}], oneOf: [{$ref: A}, {$ref: B}]}` — assert both the Base composition and both
union variants survive in the IR; a second case with a discriminator to exercise shape #2.

---

## Coverage-corpus items to steal (schema-lowering edge cases the references encode)

From datamodel-code-generator (`_intersect_constraint`, `_merge_primitive_schemas_for_allof`) and
openapi-python (`merge_properties`) — these double as **backend-refiner merge algorithms** (relocate
the merge to refine; keep the cases as fixtures):
- allOf of primitives with conflicting bounds → intersection: **max-of-mins, min-of-maxes**,
  `multipleOf` → **LCM**, `pattern` conjunction via `(?=a)(?=b)` lookahead, incompatible `format`s →
  bail. Do all bound math on `big.Rat`/decimal, never float (ogen's `minNum`/`maxNum` float leak).
- allOf = single `$ref` to (a) object [inheritance], (b) scalar/root + extra constraints, (c) enum,
  (d) `true` schema — each lowers differently.
- allOf with multiple `$ref`s having **conflicting** property signatures (MRO hazard — a *target*
  constraint, must NOT drive frontend flattening) vs non-conflicting.
- `$ref` + `nullable` only (→ `TypeRef{Target,Nullable}`, zero dup) vs `$ref` + real keyword siblings.
- **allOf + oneOf/anyOf co-occurrence** (bug #2) — with and without discriminator.
- **named scalar component `{type:string,format:uuid}` referenced from ≥2 sites** (bug #1) — assert a
  `Scalar` node at its pointer.
- `$recursiveRef`/`$recursiveAnchor`, `$dynamicRef`/`$dynamicAnchor` nearest-anchor resolution.
- circular `$ref` (self + mutual) — must lower without flattening (Morphic's ID edges support what
  openapi-python/ogen error on — a selling point to prove).
- `type:[T,"null"]` (3.1) vs `nullable:true` (3.0) vs `T|null` → one bit; the four required×nullable
  states; **bare-`null`/all-null union** → `Literal{null}` + diagnostic (TypeSpec edge).
- `required` list containing `allOf`/`anyOf` dicts (malformed-but-seen, datamodel issue #2297).
- boolean schemas (`true`/`false`) as allOf/oneOf members and as `additionalProperties`; `false` =
  unsatisfiable.
- `unevaluatedProperties:false` → `closed_after_composition` (the one structural carve-back).
- discriminator object-form vs bare-string; discriminator inferred from a shared const field
  (marked `Inferred`); mapping bare-name vs JSON-pointer resolution (ogen).
- numeric bounds `1e308` / `0.1` / int64-max (float-corruption cases for BigVal).
- `x-enum-varnames` member names; duplicate enum values (allow_alias — must NOT reject).
- oneOf vs anyOf (`Exclusive` must survive — every Python reference collapses them).

## Testing patterns to adopt (from openapi-python-client's ~100% coverage + fastapi/oagen)

- **Golden trees per config permutation** + one-command regen script (openapi-python `golden-record/`,
  `regen_golden_record.py`; fastapi `--disable-timestamp` for reproducible headers). Maps to Morphic's
  golden IR snapshots (per frontend) + golden output (per backend).
- **Black-box round-trip on *generated* code**: generate from a tiny inline spec, compile+run, assert
  `from_dict → == → to_dict == original` (openapi-python `assert_model_decode_encode`). The strongest
  correctness signal; maps to Morphic's round-trip property test. oagen goes further — **wire-
  conformance**: derive expected request bytes from the spec alone, diff against the SDK under HTTP
  interception (request-side mismatch blocks, response-side informs). "The decisive test is bytes-on-
  the-wire, not compilation."
- **Diagnostics corpus**: one minimal spec per diagnostic *code*, asserting the right code fires —
  match on **codes, not prose** (openapi-python's prose-regex tests are the anti-pattern).
- **Tiny colocated inline fixtures** (one minimal spec per `ir-spec-matrix.md` row, harness fills
  `openapi:`/`info:` defaults), not kitchen-sink specs — this is what keeps "lossless by default"
  honest per capability.
- **Architecture import-graph test in milestone 1** — datamodel's msgspec-in-parser leakage and
  openapi-generator's fused pipeline are the concrete failures it prevents; write it against the exact
  layering in CLAUDE.md before the first frontend lands.
