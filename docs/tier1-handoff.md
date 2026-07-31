# Morphic — state of the work, and what remains

A working document, written to let a fresh session pick up without re-deriving
anything. It covers the whole arc, not only the restructuring currently in
flight.

Claims about code were true at `main` = the merge of #208. Where a number would
rot, the command that derives it is given instead. Issue lists are a snapshot;
`gh issue list --state open --limit 100` is the truth.

**Delete this file when #177 closes.** At that point `docs/architecture.md`,
`docs/ir-design.md`, `docs/micro-compiler-design.md` and
`docs/micro-compiler-plan.md` are the record again, and a second place to look
is a second place to rot.

---

# Part I — What Morphic is for

Any API spec → **one spec-agnostic intermediate representation** → idiomatic SDKs
and docs for many languages.

```
compilers (spec → IR) → IR passes (IR → IR) → emitters (IR → artifacts)
```

OpenAPI, Swagger 2.0, TypeSpec, Smithy, GraphQL, AsyncAPI, Protobuf and
Erlang/OTP all lower into the same IR. Emitters never see a spec; compilers never
see a language. The IR is the contract between them.

The value is in what that buys and what it costs to get wrong. Ten invariants in
`CLAUDE.md` are load-bearing, and four decide most arguments:

- **The IR is the ABI.** A compiler's only output is an IR document plus
  diagnostics. Compilers and emitters never see each other.
- **Lossless by default, lowered late.** Compilers never flatten — no `allOf`
  merging, no union-to-optional collapse, no primary-response selection.
  Narrowing to what a target language can express happens in emitter refiners.
  Anything the IR cannot model is kept verbatim under `Unmodeled` with a recorded
  reason.
- **Stable IDs; names are presentation.** Every entity has a synthetic ID derived
  from its source pointer, never from a display name. The IR stores neutral word
  sequences; emitters own all casing.
- **Pure, reentrant stages.** Every stage is `f(input, options) → (output,
  diagnostics)`. No stage writes to stderr, and none holds package-level mutable
  state.

## The milestone arc

| Milestone | Scope | State |
|---|---|---|
| 1 | IR package + OpenAPI 3.x compiler, `validate` pass, golden corpus, JSON round-trip | **Implemented** |
| 2 | Swagger 2.0 lift into the OpenAPI compiler | Planned |
| 3 | First emitter — one language end to end (plan / refine / emit boundary) | Planned |
| 4 | Second family compiler (TypeSpec or Smithy) — proves the spec-agnostic claim | Planned |
| 5 | AsyncAPI, then GraphQL, Protobuf, Erlang/OTP | Planned |

Milestone 1 is done and the restructuring below is the work that makes 2–5
affordable. Two compiler drafts exist as read-only evidence and are **not**
rebased as part of it: `feat/graphql-compiler` (#20) and
`feat/protobuf-compiler` (#21). They are the three-compiler evidence the
framework-promotion rule requires — reading them settled two questions already
(see Part IV).

## Where the project actually stands

Milestone 1 works, and roughly sixty open issues say what it does not yet do
well. The honest summary: the OpenAPI compiler is thorough, the IR is sound, and
the surrounding machinery — engine, CLI, passes, verification — is thinner than
the documents imply. Several documented behaviours are not implemented (#53,
#67, #72), and several implemented behaviours are not documented (#63, #64, #65).

---

# Part II — The restructuring in flight

## Why

`compilers/openapi` was one package whose ~20 files were a filing convention, not
a boundary. One struct — `lowerer` — accumulated the whole compiler: 159 methods,
13 fields, four unrelated concerns. Three consequences mattered:

- **Nothing was unit-testable in isolation.** A method needed a `lowerer`, which
  needed a parsed document, options, a registry and six accumulators.
- **Shared mutable state hid order dependence.** The pointer-collision bugs
  (#108, #119) came from exactly this, and were invisible to a suite that
  compiled every fixture in one order. #185 added the guard that refuses an ID
  derived from two coordinates.
- **The same code existed three times** across the compiler drafts.

The end state (`micro-compiler-design.md` §4): every lowering is a function of an
immutable context and an explicit effect handle, returning its value and its
diagnostics. #177 — delete the `lowerer` struct — is the milestone that says it
happened.

## Progress

```bash
grep -hE '^func \(l \*?lowerer\)' compilers/openapi/*.go | wc -l
sed -n '/^type lowerer struct/,/^}/p' compilers/openapi/hoist.go | grep -cE '^\t[a-z]'
ls compilers/openapi/internal/
```

| | start | now |
|---|---|---|
| `lowerer` methods | 159 | **117** |
| `lowerer` fields | 13 | **7** |
| `internal/` packages | 1 | **9** |

Packages: `annotation`, `diag`, `ids`, `load`, `merge`, `nodeview`, `resolve`,
`scan`, `value`. Each has its own `internal/archtest` rules entry, each proven by
planting a forbidden import.

Remaining fields: `ctx`, `out`, `types`, `diags`, `anchors`, `operationIDs`,
`depth`.

**Merged**: #190–#203 and #208 — fourteen PRs, not fifteen: #202 falls in that
range and is an issue.
**Closed**: #165–#171 (tier 0 — `diag` is one of its eight files), #172–#174
(tier 1), #86. Those tiers are `micro-compiler-design.md` §5's grouping, not
tracker labels; every one of these is labelled `enhancement`.

## The number that decides what is left

A free function can only call free functions, so converting the mutually
recursive schema walk drags in everything it transitively calls:

| | |
|---|---|
| the mutual cycle | 27 methods |
| **+ its downward closure — the atomic set** | **65** |
| outside it | 52 |
| by file | schema 35, compose 18, resolve 8, accumulate 4 |

The 52 outside are content 22, operations 18, params 9, `openapi.go` 1, and the
two in `schema.go` the cycle never reaches — `lowerComponentSchema` and
`lowerComponentSchemas`. Every figure here comes from the same `go/ast` parse
the recursion pin uses, walked one step further into the closure; none of it is
a regex count, and Part V says why that matters.

**Only the 27 are atomic.** The other 38 sit strictly below the cycle — an SCC
cannot be re-entered from outside itself, and a reachability check confirms none
of the 38 gets back in — so they convert bottom-up, one at a time, with the tree
compiling at every step: a method may call a free function, and only the reverse
is barred. #197 and #198 already did exactly this for the lowest layer, which is
the precedent as well as the proof.

They stack six layers deep, callees first — 5, 5, 13, 9, 4, 2. Layer 0 is
`appendDiag`, `classifyUnionSiblings`, `dynamicHop`, `soleAnchorSite` and
`subtypeDiscriminatorValue`; the top is `fillPropertyDetail` and
`homeDeclaration`. A layer is only convertible once every layer beneath it is,
so the order is forced, but each step is its own PR.

Two of those dependencies are method values rather than calls, and a
call-expression scan sees neither: `merger` holds `Report: l.diag`, and
`lowerOneOfAnyOf` passes `l.schemaRef` into `buildUnion`. Only the first bites —
`merger` cannot convert before `diag` does, which puts it in layer 2 rather than
layer 0, where counting calls alone would file it. `buildUnion` already takes
its function as a parameter, so that edge blocks nothing. The recursion pin has
the same blind spot; both edges leave its three sets unchanged, but nothing
holds that, which is #210.

Each conversion grows a `[]ir.Diagnostic` return, and 153 call sites aggregate —
every `l.<member>(…)` in a non-test file, 172 if the tests are counted too.
`diag` (28 call sites) and `appendDiag` (5) are inside the closure and
*disappear* rather than convert — a free function cannot append to the lowerer —
and replacing them is most of what layers 0 and 1 are.

The cycle's membership is asserted by
`internal/archtest.TestLowererRecursion_IsOnlyTheKnownCycles`, which names all
three recursions (schema walk, callbacks, property lookup) and fails when a
method joins one. **Do not re-measure by hand** — see Part V.

## The decision waiting at the top of the next session

**(a) Convert as designed.** Free functions taking `(c Ctx, ts *compile.Types,
depth int, …)` and returning `(result, []ir.Diagnostic)`; 153 aggregation edges
across the whole set. The struct genuinely dies and the design's acceptance
criterion — unit tests from a literal `Ctx` and a fresh `compile.NewTypes(0)` —
becomes achievable for the core.

**(b) A `walker` struct** holding `ctx`, `types`, `diags`, `depth`. The same
methods move with almost no plumbing. But that walker *is* today's lowerer minus
three fields: a rename, not a dissolution, and #177 would need rewriting.

This is less open than it looks. `micro-compiler-design.md` §4 already raised (b)
— "passing a `*compile.Diags` handle alongside `ts`" — and rejected it by name,
because it "reinstates accumulation as a side effect, which is the property being
removed", with invariant 5 behind it. A `walker` holding `diags` is that handle
with a struct around it. Departing is allowed, but the decision is already on the
record with a reason, so it needs its own argument and not merely a cheaper bill.

The bill argues for (a) anyway now. The recommendation was made in #203 against
"27 methods and 44 edges … a tractable single change"; the true set is 65 and
153. But only the 27 land together — the other 38 are six ordinary PRs that cost
the same under either option, so what (b) actually buys is smaller than the
raw totals suggest.

Either way `out`, `anchors` and `operationIDs` belong to the caller: a document
being built, a memo, and a loop-local set.

## Order of the remaining restructuring

1. **The 65-method conversion** (#176's substance). Atomic.
2. **`internal/schema`** — schema, compose, and the eight resolve functions #174
   left behind. Only possible after (1).
3. **`internal/operation`** — operations, params, content. Needs (2), because
   all three reach into the atomic set: `content.go` calls `fillPropertyDetail`,
   `schemaRef` and `carriedSchemaRef`; `params.go` calls `carriedSchemaRef`;
   `operations.go` calls `schemaRef`. Five edges over three symbols, and that is
   the whole dependency — read from the call graph, not from #175, whose
   comment still names
   `loweredToOwnNode` (already a free function) and `residueKeywords` (deleted).
   The callbacks and property-lookup cycles convert here.
4. **#177** — delete the struct.
5. **#178** (type-surface cap), **#83** (function-size/complexity caps), **#180**
   (docs refresh) — last, because the caps are calibrated against the finished
   shape and the layout docs would go stale again after each extraction.
6. **#179** — index the source tree once instead of rediscovering it. Independent
   of the above; blocked on `$ref` handling being correct (#40).

**#175 runs after #176**, contrary to the plan's ordering: the upper layer calls
`schemaRef` and `fillPropertyDetail`, which are methods, and a free function
cannot call a method. Recorded on both issues.

---

# Part III — Everything else that remains

Roughly sixty open issues. Grouped by what they are, not by number. This is a
snapshot — check the tracker.

## Correctness and losslessness (the highest-value group)

Every one of these is a way the compiler currently loses or corrupts something a
source declared. They matter more than the restructuring, and the restructuring
exists partly to make them findable.

- **#32** raw preservation routes numbers through `float64`, corrupting
  extensions and preserved keywords. Directly violates the no-`float64` rule.
- **#39** several source details silently dropped — lossless-by-default violated.
- **#33** numeric `exclusiveMinimum` overwrites a tighter co-declared `minimum`.
- **#34** `readOnly`/`writeOnly` visibility lost when an `allOf` branch
  redeclares a property.
- **#35** first-match schema dispatch drops co-declared composition keywords.
- **#44** 3.1 nullable enum lowers to a union of literals, not a nullable `Enum`.
- **#37** unparseable response-status key becomes a catch-all success response.
- **#41** security requirement naming an undeclared scheme degrades to
  "explicitly public" — a security-relevant wrong answer.
- **#40** percent-encoded `$ref` fragments fail to resolve.
- **#42** operations without `operationId` get a `Naming.Hint` containing `/`,
  `{`, `}` — violates neutral naming.
- **#43** extension-serialization diagnostics dropped when *every* extension
  fails. (The same shape was fixed for operations in #199; check whether this is
  now stale.)
- **#31** `Compile` performs filesystem and network I/O by default via external
  `$ref` resolution. Security-relevant default.
- **#10** `allOf` reconciliation is first-declaration-wins for a property's
  shape: a later branch's type, constraints, default, docs, visibility and
  extensions are dropped rather than intersected. The reconciliation itself now
  lives in `internal/merge` (#171), so this is fixable against that package's own
  tests without standing up a compiler.
- **#202** `componentSchemaAt`'s guard is covered but unverified — found by
  mutation during this effort.

## IR

- **#45** `NewBigVal` accepts binary-exponent and leading-zero literals, storing
  non-JSON canonical forms.
- **#46** `TypeRegistry` unmarshals JSON `null` to a non-nil empty map, breaking
  the round-trip fixed point.
- **#47** `omitempty` on `AuthRequirement.Schemes` erases the documented
  empty-option spelling.
- **#70** enforce `IRVersion` and define a compatibility policy.
- **#72** provide the typed IR traversal the design documents promise — currently
  documented but absent.
- **#73** partly answered: the naming grammar moved to `ir`; the `t/prim/<kind>`
  constructor it also asks for is still open.
- **#25** spike: first-class IR representation for input-only unions and
  non-finite numeric values.

## Passes and verification

- **#67** introduce the documented pass framework — the framework is documented
  and does not exist.
- **#50** reference-integrity checking misses whole classes of references.
- **#52** discriminator validation ignores `Default` and rejects transitive
  subtypes.
- **#53** the documented message-subset check for operation bindings is not
  implemented.
- **#51** field-argument check false-positives on valid GraphQL subscription IR.
- **#54** cased `Naming.Hint` passes the neutrality check.
- **#55** silent truncation and drift hazards in the verification harness.

## Engine and CLI

- **#56** `Run` replaces compiler-returned diagnostics with
  `Document.Diagnostics`.
- **#58** spec problems surface as Go errors and usage exit codes instead of
  diagnostics — breaks the stage contract at the boundary users see.
- **#68** move format detection to a compiler-owned seam.
- **#69** make compiler options reachable end to end.
- **#74** multi-file specs — bounded load-and-bundle and a sources-based entry
  point. (`load.Load` already takes a source index for this.)
- **#75** resource budgets for pathological-but-legal inputs.
- **#76** opt-in content-addressed IR cache.
- **#82** document and pin the concurrent-use guarantee.
- **#60** `-o` writes are non-atomic — a failed run destroys previous output.
- **#62** `--help` exits 2 and prints two overlapping help texts.
- **#79** diagnostics UX at scale — aggregation, machine-readable output,
  allowlist.
- **#80** default `-o` to compact JSON through a streaming encoder.
- **#81** add a `validate` subcommand.

## Build, CI and test infrastructure

- **#59** `check-coverage.sh` hides test failures, misnames packages, fails under
  root. *This is the gate everything else relies on.*
- **#61** the documented `golangci-lint` gate cannot run against go 1.26.3.
- **#77** run the race detector and fuzz targets in CI; commit a seed corpus.
- **#78** benchmarks for compile, marshal, validate. (One exists —
  `BenchmarkAnchorWalk`, added in #201.)
- **#71** make the conformance corpus traceable to the capability matrix.
- **#89** boundary assertions on `NewWithRegistry` and `WriteGolden`.
- **#90** single-command reproduction of the CI gate.
- **#91** static coverage badge and internal gitignore entries.

## OpenAPI hygiene

- **#84** collapse the eight copy-pasted reference-resolution helpers into one
  generic.
- **#87** consolidate test scaffolding, retire the edge-case grab-bag files.
- **#88** stale `nolint` directives and awkward signatures.

## Documentation

- **#63** `CLAUDE.md` still describes the pre-implementation repository.
- **#64** architecture and README drift from the implementation.
- **#65** test and verification workflows are undocumented.
- **#180** refresh the package layout and testing strategy after the split.

## Suggested order once the restructuring lands

1. **#59** first. Everything else is verified by a gate that currently hides test
   failures.
2. The **losslessness group** — #32, #39, #33, #34, #35, #44. These are the
   project's central claim.
3. **#31** and **#41** — the two with security consequences.
4. **#67** and **#72** — the documented-but-absent pass framework and traversal,
   which Milestone 3 needs.
5. Then Milestone 2 (Swagger lift) or Milestone 3 (first emitter). Milestone 3 is
   the one that tests whether the IR is actually emitter-ready; nothing has ever
   consumed it.

---

# Part IV — What the design documents got wrong

The planning documents were written before the code was tried. Five prescriptions
did not survive it. All are corrected in place; the pattern matters more than the
list: **check the measurement before trusting a prescription.**

| Claim | Reality | Corrected in |
|---|---|---|
| `load` and `scan` are "imported only by `openapi.go`" | Three other edges | design §5.1 |
| `nodeView` belongs inside the cycle scan | `schema` and `compose` read it too | design §5.1 |
| `schema ⇄ compose` is "the one genuine cycle" | `resolve` is inside it | design §5 |
| `dynamicAnchors` derives at entry into `Ctx` | Building it emits a diagnostic, and costs ~1.4% of a compile | design §4.1 |
| `diagnosedConstraints` is "not subsumed by identity dedup" | It was, entirely | design §4.1 |

Two questions were settled by evidence rather than corrected:

- **The bounded-recursion guard is not promoted** to the framework (design §3.3,
  with the command that re-derives the three-compiler table). The issue asserting
  256 was "known wrong for an SDL-shaped source" was itself wrong — the GraphQL
  draft independently chose 256.
- **Hoisting dissolves** rather than joining a package (#195). Three of its six
  methods were one-line delegations to `compile.Types`.

---

# Part V — How to work on this safely

Not style preferences. Each was learned by shipping its opposite here.

**Parse Go with a parser.** The recursion was measured with a regex and came out
57; it is 27. The pattern ended a function at the first `}` in column zero, which
a one-line method does not have, so every such method absorbed its successor's
body and inherited its calls. `internal/archtest` parses with `go/ast` and takes
no new dependency — extend that.

**The diagnostic stream is the safety net for every conversion.** Goldens cannot
see a diagnostic that moved, was added, or was dropped, because none of that
changes the IR. Compile every fixture on `main` and on the branch and diff the
full stream — severity, code, pointer, message, in order:

```go
// throwaway test; run on both revisions via `git worktree add`
for _, f := range fixtures {
    _, diags, err := openapi.New().Compile(ctx,
        []compilers.Source{{Path: name, Data: data}}, compilers.Options{})
    fmt.Fprintf(out, "=== %s err=%v\n", name, err)
    for i, d := range diags {
        fmt.Fprintf(out, "  %03d %s %s %s | %s\n",
            i, d.Severity, d.Code, d.Provenance.Pointer, d.Message)
    }
}
```

**Mutate what you claim to have guarded.** Twice a conversion opened a hole the
whole suite missed — a returned diagnostic nobody recorded. Both were found by
dropping it deliberately. 100% coverage does not mean discriminated: #202 is
fully covered and unverified.

**An inconclusive mutation is worth nothing.** Three times a mutant "survived"
against a pattern that did not exist, or reddened because the file no longer
parsed. Read what the mutation actually did before believing it.

**Check the tracker before settling something.** #86 wanted provenance
centralised; design §4 wanted diagnostics returned. Those look opposed and are
not — a constructor that stamps and *returns* satisfies both.

**Two-order compilation.** `internal/harness` compiles each fixture in both
declaration orders and diffs the result. It is what catches the ID-collision
class; keep it green.

# Part VI — The gate

CI's `gate` job, in order. All must pass before a PR opens.

```bash
gofmt -l $(git ls-files '*.go')   # must print nothing
go vet ./...
golangci-lint run
go build ./...
./scripts/check-coverage.sh       # exactly 100% of statements
```

Coverage is per-package with no `-coverpkg`: code moved into a new package needs
that package's **own** tests, and integration coverage from the compiler does not
count. This bit every extraction in this effort.

Also true of every PR here: goldens byte-identical
(`git diff --stat origin/main...HEAD -- testdata/` empty), and PR titles within
72 characters **including** the ` (#NNN)` GitHub appends —
`printf '%s (#999)' "<title>" | wc -c`.
