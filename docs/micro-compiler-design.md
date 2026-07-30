# Micro-Compiler Architecture — Design

Status: approved, not yet implemented. Scope: `compilers/compile`, `compilers/openapi`,
`internal/archtest`, `internal/harness`, `ir/irverify`.

Claims about code in this document were true at `a095636`. Where a number would rot, this
document gives the command that derives it instead of the number.

**A micro-compiler is a function that lowers one source construct at one position, given only what
that construct needs.** It takes an immutable context and a coordinate, returns a value and its
diagnostics, and touches no state its caller cannot see. The unit is a *position in the source*, not
a stage in a pipeline — §4 records why the pipeline framing does not survive contact with the
recursion. A type may own several such functions when they serve one construct, which is what
`merger` and `refScan` already do; it becomes a god object when they serve eleven.

## 1. Problem

`compilers/openapi` is one Go package. Its ~20 files are a filing convention, not a boundary:
everything in the package can reach everything else, and one struct accumulated the whole
compiler.

```bash
# methods on the lowerer, and the size of the package they live in
grep -hE '^func \(l \*?lowerer\)' $(git ls-files 'compilers/openapi/*.go' | grep -v _test.go) | wc -l
git ls-files 'compilers/openapi/*.go' | grep -v _test.go | xargs wc -l | tail -1
```

The per-file split shipped in #136 did not reduce this; the method count is higher now than when
the previous decomposition was designed. That is the diagnosis: **file boundaries do not isolate,
and the individual functions were never the problem.** No function body in the package exceeds 50
lines, against a 70-line cap:

```bash
for f in $(git ls-files 'compilers/openapi/*.go' | grep -v _test); do
  awk -v F="$f" '
    /^func /{name=$0; start=NR; depth=0; inf=1}
    inf{ for(i=1;i<=length($0);i++){c=substr($0,i,1); if(c=="{")depth++; if(c=="}")depth--}
         if(depth==0 && NR>start){ printf "%d %s:%d\n", NR-start+1, F, start; inf=0 } }' "$f"
done | sort -rn | head -5
```

The god object is a type-surface failure, and no rule in force measures type surface.

Three consequences that are not stylistic:

- **Nothing is unit-testable in isolation.** A method on `lowerer` needs a `lowerer`, which needs a
  parsed document, options, a registry, and six accumulators. The functions that *are* pleasant to
  test — `facets.go`, `constraints.go`, `value.go` — are exactly the ones that are already free
  functions over explicit inputs.
- **Shared mutable state hides order dependence.** Interning decisions depend on what was interned
  before. The pointer-collision class of bug (#108, #112) came from precisely this and was invisible
  to a suite that compiled each fixture in one order.
- **The same code is written three times.** See §3.

## 2. Decisions

Four constraints were settled before this design and are not re-opened here.

1. **The framework is designed against all three compilers.** `compilers/openapi` on `main`, plus
   the GraphQL (#20) and Protobuf (#21) drafts as *read-only evidence* of what a non-JSON-Schema
   compiler needs. The drafts are not rebased as part of this work.
2. **Behaviour-neutral by default.** Every restructuring step produces byte-identical golden output.
   Where a defect is structural — only fixable once a seam moves — it gets its own PR with a test
   that reddens first and an explicit golden update.
3. **Isolation is enforced by Go package boundaries**, not by convention or by bespoke AST rules
   where a package boundary would do.
4. **Done means the lowerer is dissolved and the structural backlog closes.** Not: a second compiler
   rebased onto the framework, and not: a new-compiler skeleton demo.

No functional-programming dependency is adopted. The architecture is functional in the way that
pays here — pure functions over explicit inputs, values returned rather than mutated — and Go
expresses that with parameters and return values. A combinator library was evaluated and rejected;
§9 records why, because the reasoning is not obvious and will otherwise be re-litigated.

## 3. The framework boundary

`compilers/compile` owns what every compiler must agree on. The governing asymmetry: **promoting
into the framework later is additive; demoting is a breaking change across every compiler.** So
promotion requires evidence from all three, not two and an expectation.

| Concern | openapi | graphql | protobuf | Verdict |
|---|---|---|---|---|
| Interning + type registry | `compile.Types` | own `types.go` | own `types.go` | already framework |
| Diagnostic accumulation | `compile.Diags` | own `diag.go` | own `diag.go` | already framework |
| Canonical naming grammar | `schema.go` | `naming.go` | `naming.go` | **promote** |
| ID grammar | `ids.go` | `ids.go` | `ids.go` | **promote grammar, not derivation** |
| Bounded-recursion guard | `depth`, cap 256 | — | — | promote only if the drafts show need |
| Reference resolution | `resolve.go` | `resolve.go` | — | do not promote |
| Loading, options | yes | yes | yes | do not promote — format-specific |

### 3.1 Naming is the load-bearing case

`canonicalWords` exists three times. The openapi and graphql copies are byte-identical apart from
two comment lines. The protobuf copy diverges behaviourally: it treats `.` as a word separator.

`ir.Naming.Canonical` is ABI. Its doc comment defines it as "lower_snake words with no casing
opinions", and invariant 4 makes neutral naming a property of the IR rather than of a compiler. So
today `example.v1` canonicalizes to `example_v1` from one compiler and `example.v1` from another,
and an emitter reading `Canonical` cannot tell which grammar produced it.

Nothing catches this. `irverify.checkNaming` tests lowercase-idempotence — it enforces the
"never cased" half of invariant 4 and is silent on the "neutral canonical word sequence" half. Both
spellings are lowercase, so both pass.

Promotion therefore lands with a test, not just a home: the grammar moves to `compilers/compile`
with a conformance suite, and `irverify` gains a segmentation check. That closes the live half of
#73 and is adjacent to #54.

**Promoting the grammar is not behaviour-neutral, and the plan must budget for that.** The three
copies disagree, so at most one survives promotion unchanged. If the promoted grammar treats `.` as
a separator, OpenAPI's output changes for every name containing a dot; if it does not, Protobuf's
does. Under decision 2 this is a structural fix rather than a move: its own PR, a test that reddens
first, an explicit golden update, and the choice of grammar argued there rather than assumed here.
The IR's own wording — "lower_snake words" — is an argument that `.` should separate everywhere,
since a dot is not a word character. That is a recommendation, not a decision.

The divergence is also a live defect independent of this work: two compilers producing different
`Naming.Canonical` grammars is a violation of invariant 4 that ships today. It is filed separately
so it is not lost if the architecture work is deferred.

### 3.2 What the framework must not absorb

Loading and options are format-specific and stay put. Reference resolution is needed by two of three
compilers and by different mechanisms, which is exactly the shape that looks promotable and is not.
`ir.Document` stays out: only `Types` carries a framework invariant.

## 4. The micro-compiler contract

Lowering is a **recursive tree walk, not a linear pipeline.** The source-coordinate → IR-node
relation is computed *by* lowering, not derivable from the coordinate: `nullUnionCollapse` leaves a
source position owning no IR node, and `lowerComponentSchema` branches on whether a ref was already
interned at the component's own ID. A second pass over coordinates would need the first pass's
decisions as input, so sequential passes over positions cannot work. This was established by
measurement in the previous decomposition and is re-affirmed here.

What that leaves is not a pipeline stage but a pure function over a position:

```go
func lowerX(ctx Ctx, ts *compile.Types, node N, at string) (Out, []ir.Diagnostic)
```

- `Ctx` is immutable and passed **by value**. It carries the parsed document, options, source
  identity, and indexes derived once at entry.
- `ts` is the **single** effect handle. Interning is irreducibly shared and stateful; nothing else
  is.
- Diagnostics are **returned, never accumulated through a handle.** This is the repo's stage
  contract (invariant 5) and what `facets.go` already does. Passing a `*compile.Diags` handle
  alongside `ts` is the obvious alternative and is rejected: it reinstates accumulation as a side
  effect, which is the property being removed.

Every function is then constructible in a test from a literal `Ctx` and a fresh `compile.NewTypes(0)`.

```go
// Ctx is everything a lowering needs and must not change. Copied, not shared.
type Ctx struct {
    Doc      *soa.OpenAPI  // the parsed source
    Opts     Options       // the caller's options
    Source   ir.SourceInfo // identity of the loaded source
    SrcIndex int           // index into Document.Sources
    // contains the derived indexes; see below
}

func (c Ctx) DeclaresSchema(name string) bool   // was the `schemas` field
func (c Ctx) AnchorsNamed(name string) []string // was the `dynamicAnchors` field
```

The derived indexes are maps, and a struct copy shares a map rather than copying it — so "immutable
by value" would be a half-truth if they were exported fields. They are unexported and read through
accessors instead, which makes the immutability real rather than conventional. This costs two
methods and removes a class of bug where a callee's write is visible to its caller's caller.

### 4.1 Where the thirteen fields go

| Field | Becomes |
|---|---|
| `doc`, `opts`, `source`, `srcIndex` | `Ctx`, by value |
| `schemas`, `dynamicAnchors` | derived once at entry, into `Ctx` |
| `types` | the one effect handle |
| `diags`, `out`, `merge` | return values |
| `depth` | explicit parameter — which the bounded-recursion rule wants regardless |
| `operationIDs` | local to the operations loop, which is not recursive |
| `diagnosedConstraints` | hypothesis: dissolves into memoization — see below |

`diagnosedConstraints` suppresses duplicate constraint diagnostics when a sub-schema is read from
two positions. It is **not** subsumed by `Diags` identity dedup, because the two reads carry
different provenance. But if lowering a pointer is memoized, its diagnostics are produced once and
the field is unnecessary — and `Types.Intern` already memoizes the node. The previous design left
this as "a policy question for the migration to settle with a test". It is settled by trying
memoization and keeping the field only if a test demands it.

## 5. The OpenAPI split

The call graph is a DAG with exactly one cycle:

```
operations (called by nobody)
  → params → content ┐
                     ├→ schema ⇄ compose      the one genuine cycle
                     └→ hoist, resolve, constraints
```

`schema` and `compose` are mutually recursive and stay one package. Everything else can be layered.

**Tier 0 — already pure.** Eight files contain no `*lowerer` method at all: `value`, `ids`,
`amplification`, `cycles`, `merge`, `facets`, `load`, `diag` — roughly a third of the package's
non-test lines. Four of them own a small local type with a focused method set (`aliasWeigher`,
`nodeView`, `refScan`, `merger`), which is the target shape rather than a counter-example: a type
whose methods serve one purpose is a micro-compiler, and a type whose methods serve eleven is the
problem. These need only a package. Extraction is a move, so goldens are byte-identical by
construction and each step is near-zero risk.

```bash
# the tier-0 set: no lowerer methods, and what it weighs
for f in value ids amplification cycles merge facets load diag; do
  printf '%s lowerer-methods  %s\n' \
    "$(grep -cE '^func \(l \*?lowerer\)' compilers/openapi/$f.go)" "compilers/openapi/$f.go"
done
wc -l compilers/openapi/{value,ids,amplification,cycles,merge,facets,load,diag}.go | tail -1
```

One Tier-0 file does not move alone. `facets.go` reads a `site`, and `site`, `siteKind`, `siteAt`
and `siteSchema` are declared in `resolve.go` — as free functions and types, not as `lowerer`
methods. They belong with the readers that consume them: a site is a position, and annotations are
what a position carries. So `internal/annotation` takes `facets.go` plus those four declarations,
and `resolve.go` keeps only its fourteen methods.

**Tier 1 — the god object.** Every method lives in eleven files: `schema`, `compose`, `operations`,
`content`, `resolve`, `params`, `hoist`, `auth`, `meta`, `constraints`, `openapi`.

**Unsettled:** `hoist` is called from four places and its job — intern an anonymous type at a
pointer, return a ref — is close to `compile.Types.Intern`. Whether it becomes part of
`internal/schema` or largely dissolves into the framework is determined by reading its methods
during implementation, not decided here.

### 5.1 Target layout

```
compilers/compile/                  framework — imports ir only
  types.go       interning and the type registry                (exists)
  diags.go       accumulation and identity dedup                (exists)
  naming.go      canonical word grammar                         (promoted, §3.1)
  ids.go         ID grammar and the minted-node namespace rule   (promoted)

compilers/openapi/                  the public face, and nothing else
  openapi.go     Compiler, New, Formats, Compile
  options.go     Options, GroupingStrategy
  internal/
    diag/        OpenAPI diagnostic codes, diagf, hasErrorDiag
    load/        parse and load the document
    scan/        cycle and amplification refusal over raw source
    ids/         OpenAPI pointer arithmetic and ID derivation
    value/       value and BigVal lowering
    annotation/  site, siteAt, the facet readers (+ constraints, from 3.2)
    merge/       allOf property reconciliation
    resolve/     reference resolution
    schema/      schema ⇄ compose (⇄ hoist) — the recursive core
    operation/   operations, params, content, auth, meta
```

Imports run one way: `operation` → `schema` → {`resolve`, `annotation`, `merge`, `value`, `ids`} →
`diag` → `compile` → `ir`. `load` and `scan` sit on the entry side, imported only by `openapi.go`.

`diag` sits at the bottom because `diagf` is the single constructor that populates severity, code and
provenance, and eight files call it. The OpenAPI diagnostic codes are format-specific strings, so
they stay in the compiler rather than moving to `compilers/compile`; whether `diagf` itself later
promotes is left open, since the promotion rule in §3 requires evidence from all three compilers
and the drafts' constructors have not been compared.

Two files in the Tier-0 list join a package later rather than founding one. `constraints.go` carries
two `lowerer` methods, so it is Tier 1 by the definition above and moves into `annotation` at step
3.2; `hoist.go` is unsettled per §5. Everything else in the Tier-0 set founds a package directly.

### 5.2 Test migration is not free

Twenty-one of the package's thirty-one test files are `package openapi` — internal tests reaching
unexported symbols:

```bash
grep -h '^package ' $(git ls-files 'compilers/openapi/*_test.go') | sort | uniq -c
```

Tests move with the code they cover, and an internal test for `value.go` becomes an internal test of
`internal/value` unchanged. The ones that will bite are the suites spanning concerns —
`conformance_test.go`, `golden_test.go`, `annotations_test.go`, `fuzz_test.go` — which exercise the
whole compiler and belong at the top level as external tests against `Compile`. Any test that
survives only by reaching across a new boundary is evidence the boundary is in the wrong place, and
should be treated as such rather than worked around with an export.

The 100% coverage gate is what makes this safe: a helper stranded on the wrong side of a split shows
up immediately as an uncovered statement rather than as quietly dead code.

## 6. Data flow

At the document level the pipeline *is* linear. Only step 4 fans into a tree, and that is forced by
recursion rather than chosen:

```
Compile(source, opts) → (ir.Document, []ir.Diagnostic)
  1. load      parse → raw document
  2. scan      cycle and amplification refusal, pure over raw source
  3. index     derive Ctx once
  4. lower     recursive descent; each node f(ctx, ts, at) → (value, diags)
  5. assemble  Document from returned values + ts.Registry()
```

### 6.1 Diagnostic fan-in is the neutrality risk

Nothing changes about *kinds* of failure: `ir.Diagnostic` for spec problems, Go errors for I/O and
programmer errors, `irverify.Violation` for our bugs, panic barrier at `Compile`. What changes is
the fan-in — central accumulation becomes bottom-up returns, deduped once at the top.

Golden files include diagnostics, so a reordering surfaces as a golden diff. That sounds like a
safety net and is the opposite: the temptation at that moment is to run `-update`. **A golden diff
during a Tier-1 step is a stop-and-explain, never an `-update`.** `compile.Diags` preserves
first-seen order, but "first-seen" differs between central accumulation and bottom-up returns, so
some reordering is expected and each instance must be understood rather than absorbed.

## 7. Enforcement

1. **`internal/archtest`'s prefix matching is a live hole and is fixed first.** `hasAllowedPrefix`
   accepts `ip == prefix || strings.HasPrefix(ip, prefix+"/")`, and `compilers/openapi`'s allowlist
   contains `<module>/compilers`. So `<module>/compilers/graphql` matches: **one compiler may import
   another today and the architecture test says nothing**, contradicting invariant 1. This is the
   live instance of #57, and until it is fixed the package boundaries this design rests on are not
   enforceable.
2. **Every new package gets a `rules` entry**, or `TestImportGraph_EveryPackageIsRuledOrExempt`
   fails. An unkeyed subdirectory is audited under its nearest keyed ancestor's allowlist, so
   nesting must not become a way to widen one.
3. **Anti-regrowth needs a type-surface cap.** Function-size and complexity caps (#83) are worth
   having but were already satisfied while the god object grew; they measure the wrong dimension.
   `internal/archtest` already carries `go/parser`, so a cap on methods per type in `compilers/*` is
   cheap, and it is the only guard that would have caught this happening.

## 8. Testing

Most of this work is not writing behaviour but proving behaviour did not change, so the testing
obligations are the design rather than a postscript to it.

### 8.1 What each step must prove

**A Tier-0 move must show:** goldens byte-identical with no `-update`; coverage still exactly 100%;
an `internal/archtest` rules entry for the new package; and — the obligation that makes the move
worth doing — **table-driven unit tests for the package's own surface**, built from a literal `Ctx`
and a fresh `compile.NewTypes(0)`, calling neither `Compile` nor a document fixture.

That last requirement is the acceptance criterion separating this from a file shuffle. Today's suite
is integration-heavy enough that a purely cosmetic split would keep passing. **If a package cannot
be tested without constructing a document, its boundary is in the wrong place** — that is a signal
to move the boundary, not to add a fixture.

**A Tier-1 conversion must additionally show:** the two-order oracle green across the conformance
corpus; the ID-collision oracle green; a bounded-recursion test for every package that recurses; and
every diagnostic-order change explained individually rather than absorbed by a golden update.

### 8.2 Two oracles that do not exist yet

**The general two-order diff.** CLAUDE.md prescribes it;
`TestOneOf_CoDeclaredDistributionIsOrderIndependent` implements it by hand for three cases, at
exactly the site where the pointer-collision bug was found. `harness.deterministic` looks like the
general version and is not — it recompiles *the same bytes*, proving same-input-same-output rather
than permuted-input-same-output. Only the second catches order-dependent interning, and this
refactor moves interning.

Mechanism: permute *declaration order* — reverse the entries of `components/schemas`, of `paths`,
and of each `properties` map — then compile both and `cmp.Diff` the documents. Two limits belong in
the doc comment so the oracle is not over-trusted. Some permutations are not meaning-preserving: a
document with duplicate mapping keys resolves to the last one (#95), so permuting those tests
nothing and such sources are excluded. And it proves order-independence only for constructs the
fixture contains, which is why it belongs on the conformance corpus rather than on one hand-written
spec.

**An ID-collision oracle.** Nothing in the repo asserts that two distinct source constructs cannot
mint the same `ir.TypeID`. Invariant 3's corollary — a minted node needs a namespace of its own —
is currently held by reading alone, and CLAUDE.md records the failure mode precisely: violating it
produces no diagnostic in either order, with `pass/validate` clean on both.

This is the most likely way the refactor breaks something no existing guard sees, because moving
lowering between packages is exactly when an ID derivation gets rewritten. The check: over every
corpus spec, assert the map from `ir.TypeID` to originating source pointer is injective, and that
every minted ID sits in a namespace no source pointer can produce. It lands before Tier 1 alongside
the two-order oracle.

### 8.3 What this refactor specifically endangers

| Change | How it breaks | What catches it |
|---|---|---|
| `depth` field → parameter | one path forgets to increment; unbounded recursion on a cyclic or deep spec | `FuzzCycleDetector`, cycle corpus — but only through paths a fixture reaches. Add a per-package depth test |
| ID derivation crosses a package boundary | pointer prefix changes; collision or dangling ref | `danglingcheck_test.go`, plus the new collision oracle |
| Signature change alters the `at` passed | a node records a wrong-but-valid pointer | **Nothing directly.** `irverify.checkProvenance` validates the source *index range*, not pointer correctness. Goldens carry provenance, so it surfaces as a golden diff — which is precisely why §6.1's no-`-update` rule is load-bearing rather than fussy |
| Diagnostics returned instead of accumulated | ordering shifts | Goldens, handled per §6.1 |
| `diagnosedConstraints` dissolves into memoization | a duplicate diagnostic returns on an unmemoized path | Goldens, but only for corpus-covered shapes. Add a targeted test for the two-position read the field exists to suppress |
| `Ctx` copied while its maps are shared | a callee's write is visible to its caller | Accessors make it unrepresentable; assert no exported `Ctx` field is a map |
| Tests move with their code | a test stops reaching what it claims to cover | The coverage gate protects the *code*, not the *test*. Plant a defect in each moved package and confirm that package's own suite reddens |

### 8.4 Blind spots, stated so they are not mistaken for coverage

- **`harness.Check` returns at the first error diagnostic, before `irverify` runs.** A fixture
  written to exercise an invariant check that also trips an error diagnostic never reaches it.
  Establish where each new fixture lands rather than assuming it arrives.
- **The annotation grid cannot express a carrier position** (#142), so annotations landing on
  `ir.Parameter` or `ir.Property` are not measured by it and still need hand-written tests. The grid
  looks complete over a set of axes that cannot name the position.
- **The golden corpus shares the blind spots of the code that produced it.** A construct no fixture
  contains is unprotected by byte-equality. The conformance corpus is the intended counterweight and
  is only as complete as `ir-spec-matrix.md`.
- **`irverify` checks provenance index range, not pointer correctness** — see the table above.

### 8.5 Held at every step

Goldens byte-identical; the 27-cell annotation grid green with no `knownGap` reintroduced;
`irverify` extended with the naming-segmentation check; all three fuzz targets still building and
seeded; 100% statement coverage. Every new guard gets a planted defect to prove it reddens —
including both new oracles, each proven against a known-bad input before it is trusted.

## 9. Rejected: a functional-programming dependency

`IBM/fp-go`, `TeaEntityLab/fpGo` and `emirpasic/gods` were evaluated. Recorded here so the
evaluation is not repeated.

Of the two FP libraries, `IBM/fp-go` is the only serious candidate: generics-native, actively
released, real `fp-ts` lineage. `fpGo` runs on abandoned CI, ships an `interface{}`-and-reflection
legacy branch, and mixes monads with an actor model, an HTTP client and a worker pool.

Neither is adopted, for four reasons:

1. **It would disarm the coverage gate.** `scripts/check-coverage.sh` runs `go test ./...
   -coverprofile` with no `-coverpkg`, so only module-local statements enter the profile —
   dependencies contribute none. Today an `if err != nil { return ... }` is a block that must be
   executed, which is why every error path here has a test that reaches it. `F.Pipe2(x, f,
   E.Chain(g))` is one block satisfied by any happy-path test, with the short-circuit logic living
   in a package the profile cannot see. The gate would stay green while losing what it was buying.
2. **The channel that would benefit cannot have it.** `ir` has an empty archtest allowlist —
   stdlib only — and `ir.Diagnostic` lives there. The interesting channel is a diagnostic *log*, not
   a failure: that is a Writer over a monoid, and the monoid is `append`.
3. **Interning is irreducibly stateful.** Modelled functionally it is `State` stacked with Writer
   and Either; Go has no higher-kinded types, and fp-go's own documentation concedes it addresses
   this by writing out each combination by hand.
4. **It works against the stated goal.** The motivation for this work is debuggability. Point-free
   chains produce stack traces through generic combinator frames and step badly under a debugger.

`gods` is separately unsuitable: v1 is `interface{}`-based, and the generic v2 has been at
`v2.0.0-alpha` since January 2024. The actual need is a few dozen set-shaped maps and a handful of
sorts, which `maps.Keys` + `slices.Sorted` and the existing generic `sortedKeys` already cover.

## 10. Deliberately out of scope

- **The compiler's public surface.** `openapi.Compiler`, `New`, `Options` and `GroupingStrategy`
  are unchanged, and so is the `compilers.Compiler` contract they satisfy. Every package this design
  creates lives under `internal/`, so none of it is reachable from outside the compiler. The IR is
  the ABI (invariant 1); a restructuring that altered the compiler's own surface would be a second,
  unrelated change.
- **The source index.** Indexing the raw tree once (pointer → node + shape) would make resolution a
  lookup, share one walk across cycle and amplification detection, and give `--explain` a substrate.
  It is held back because `$ref` handling currently carries open defects — #40 (percent-encoded
  fragments fail to resolve), #141 (an `$anchor`-resolved schema interns a malformed type ID), #143
  (siblings adjacent to a `$ref` on an allOf branch are dropped) — and an index built over them
  would bake them in. Filed as a follow-up blocked on those closing.
- **Rebasing the GraphQL and Protobuf drafts.** They are evidence here, not work items.
- **A new-compiler skeleton demo.**

## 11. Risks

- **A framework nothing else compiles against is still a guess.** No second compiler is rebased
  during this work, so generalization is argued from written evidence rather than proven. Mitigated
  by promoting only on three-compiler evidence, recording per promoted symbol which compilers
  demonstrated the need, and pinning shared grammar with tests. This makes a later rebase a
  verification rather than a discovery; it does not eliminate the risk.
- **Strangler steps may not be behaviour-neutral.** Byte-identical goldens are the guard, and §6.1
  names the specific place they will be tempting to override.
- **Tier 1 is large.** `schema` ⇄ `compose` is the single biggest unit and cannot be split further
  without breaking the recursion. It is sequenced last, after the contract is proven on smaller
  concerns.
- **A guard written during a refactor is a guard calibrated to the refactor.** Both new oracles are
  written by the same effort they are meant to police. Each is therefore proven against a
  known-bad input *before* the code it guards changes — an oracle first seen passing has
  demonstrated nothing.

## 12. Sequencing

Each row is one PR unless noted. "Done when" is the acceptance test, not a summary of the work.

| # | Work | Done when | Blocked by |
|---|---|---|---|
| 0.1 | Fix archtest prefix matching so one compiler cannot import another (#57) | A test asserting `compilers/openapi` may not import `compilers/graphql` fails before the fix and passes after | — |
| 0.2 | General two-order oracle in `internal/harness` | Reverses declaration order across the conformance corpus and diffs; proven by reverting #108's fix and watching it redden | — |
| 0.3 | ID-collision oracle | `TypeID` → source pointer is injective across the corpus, and minted IDs occupy a namespace no source pointer produces; proven by planting a colliding derivation | — |
| 1.1 | Promote the ID grammar into `compilers/compile` | `compilers/openapi` derives no ID except through the framework; goldens byte-identical | 0.1 |
| 1.2 | Promote the canonical naming grammar, choosing one segmentation | Grammar has a conformance suite; the chosen rule is argued in the PR; goldens updated deliberately with a reddening test (**not** neutral — §3.1) | 0.1 |
| 1.3 | Extend `irverify` to check segmentation, not only casing (#73, #54) | A cased-correct but wrongly-segmented `Canonical` is rejected; proven by planting one | 1.2 |
| 2.1–2.7 | Tier-0 extractions, one PR each: `diag`, `load`, `scan`, `ids`, `value`, `annotation` (+ site), `merge` | Per §8.1: goldens byte-identical, rules entry added, and each package carries table-driven unit tests needing no document | 0.1 |
| 3.1 | Introduce `Ctx` with accessors; derive indexes at entry | No exported `Ctx` field is a map; goldens byte-identical | 2.x |
| 3.2 | Convert the leaves to the contract: `constraints` → `annotation`, `meta` and `auth` → `operation` | Those files hold no `lowerer` method | 3.1 |
| 3.3 | Extract `internal/resolve` | ditto, plus a bounded-recursion test | 3.2 |
| 3.4 | Extract `internal/operation` (operations, params, content) | ditto | 3.3 |
| 3.5 | Extract `internal/schema` (schema ⇄ compose ⇄ hoist); settle `hoist` and `diagnosedConstraints` | ditto; both open questions in §4.1 and §5 resolved by test, not assertion | 3.4 |
| 3.6 | Delete the `lowerer` struct | `grep -c '^func (l \*\?lowerer)'` returns 0 | 3.5 |
| 4.1 | Type-surface cap in `internal/archtest` | Re-adding ten methods to one type fails the build | 3.6 |
| 4.2 | Function-size and complexity caps in `golangci-lint` (#83) | The gate runs them; a 71-line function fails | 3.6 |

Phase 4 lands after Phase 3, not before: the caps are calibrated against the finished shape, and
landing them first would only encode the current one.

## 13. Disposition of the existing backlog

| Issue | Disposition |
|---|---|
| #57 archtest cannot enforce compiler isolation | **Closed by 0.1.** Prerequisite for every package boundary here |
| #73 naming grammar and primitive IDs are cross-compiler ABI in one compiler | **Closed by 1.1–1.3.** This design is the answer to it |
| #54 cased `Naming.Hint` passes the neutrality check | **Adjacent to 1.3**; fixed there if the segmentation work reaches `Hint`, otherwise left open |
| #83 enforce size and complexity caps in lint | **Closed by 4.2**, deliberately last |
| #66 extract a shared JSON-Schema→IR lowering core before the next compilers land | **Superseded.** Its premise expired — the next compilers landed without it (#20, #21). §3 replaces it with evidence-based promotion. To be closed with that reasoning, not silently |
| #142 the annotation matrix cannot reach a carrier position | **Untouched, and recorded in §8.4** as a standing blind spot. Independently fixable |
| #40, #141, #143 `$ref` handling defects | **Block the source index** (§10). Not fixed here |
| #20, #21 GraphQL and Protobuf drafts | **Evidence, not work items** (§2). Rebasing is later work |
| Naming grammar divergence across compilers | **New issue**, filed separately: a live invariant-4 violation that ships today, independent of whether this architecture work proceeds |
