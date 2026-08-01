# Micro-Compiler Architecture — Design

Status: **implemented**. This is a record of a restructuring that happened, not a proposal —
`compilers/openapi` is now its public face over thirteen internal packages, the god object §1 is
about is gone, and the caps that stop it regrowing are in force. §12 remains the row-by-row record.
Scope: `compilers/compile`, `compilers/openapi`, `internal/archtest`, `internal/harness`,
`ir/irverify`.

To read the shape it describes off the tree rather than off this page, see
`docs/architecture.md` §3, which gives the command.

Claims about code in this document were true at `a095636`, except where a row or an *as landed*
note says otherwise. Where a number would rot, this document gives the command that derives it
instead of the number — and now that the restructuring has landed, several of those commands name
files that no longer exist. That is the point of the revision above: they described the tree the
work started from, and re-pointing them at the tree it ended at would turn a record into a claim.

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
| Canonical naming grammar | `schema.go` | `naming.go` | `naming.go` | **promoted to `ir`** — `ir.CanonicalWords`, with `compile.NamingFor` the compiler-facing constructor |
| ID grammar | `ids.go` | `ids.go` | `ids.go` | **promoted, derivation left behind** — `compile.TypeID` and friends over a `compile.Space` |
| Bounded-recursion guard | `depth`, cap 256 | `depth`, cap 256 | `depth`, cap 32 | **do not promote** — see §3.2 |
| Reference resolution | `resolve.go` | `resolve.go` | — | do not promote |
| Loading, options | yes | yes | yes | do not promote — format-specific |

### 3.1 Naming is the load-bearing case

`canonicalWords` exists three times. The openapi and graphql copies are byte-identical apart from
two comment lines. The protobuf copy diverges behaviourally: it treats `.` as a word separator.

`ir.Naming.Canonical` is ABI. Its doc comment defines it as "lower_snake words with no casing
opinions", and invariant 4 makes neutral naming a property of the IR rather than of a compiler. So
today `example.v1` canonicalizes to `example_v1` from one compiler and `example.v1` from another,
and an emitter reading `Canonical` cannot tell which grammar produced it.

Nothing caught this: `irverify.checkNaming` tested lowercase-idempotence — the "never cased" half
of invariant 4 — and was silent on the "neutral canonical word sequence" half. Both spellings are
lowercase, so both passed.

**The grammar and its check landed ahead of this plan** (#161), because the divergence is a live
invariant-4 violation rather than an architecture question. `ir-design.md` §3.2 now fixes the
segmentation — a word is letters and digits, every other character separates — `compilers/openapi`
implements it, and `irverify` rejects a canonical that is not a word sequence, so a compiler
disagreeing about `.` is caught wherever the corpus reaches it.

Two boundaries were listed here as beyond that check and are no longer. The letter/digit split
(`foo2bar` against `foo_2_bar`) is decidable from the neutral name alone. The camel-case split is
not — lowercasing erases the case change that marked it — so the grammar itself moved to `ir`,
beside the field whose doc comment already states it in prose, and `irverify` recomputes a canonical
from the `Naming.Source` next to it. `compilers/compile` keeps `NamingFor`, the compiler-facing
constructor that pairs the two; what moved is the derivation, not the API a compiler calls.

That matters because the architecture sweep reaches only this repository's own compilers. A
`Document` arriving any other way — decoded from JSON, produced by a compiler outside this tree,
rewritten by a pass — is held by `irverify` alone. Wiring the check found a live instance
immediately: the webhook group named itself and shipped no words at all, leaving every emitter to
segment `webhooks` itself, which is the work invariant 4 moves out of compilers.

So 1.2 was the move rather than the decision, and it has **landed**: there is one implementation,
`compilers/openapi` derives no name of its own, and the graphql and protobuf copies are deleted
against it as those drafts rebase. Their outputs move at that point — the
protobuf copy separates on `_`, `-`, space and `.`, and the graphql copy on the first three alone,
so both leave `/`, `[`, `]`, `+`, `:` and braces inside a word — and each rebase carries its own
golden update, argued there. The OpenAPI goldens did not move, since #161 had already made its copy
the grammar that ships.

Deleting two copies is a state rather than a rule, so the architecture test now asserts that only
the framework and `ir` may fill `Naming.Canonical`. What it still cannot see is a `Hint` (#54).

The divergence was filed separately so it would not be lost if this work were deferred; that is
what it is now closed by.

### 3.2 What the framework must not absorb

Loading and options are format-specific and stay put. Reference resolution is needed by two of three
compilers and by different mechanisms, which is exactly the shape that looks promotable and is not.
`ir.Document` stays out: only `Types` carries a framework invariant.

### 3.3 The recursion guard is not promoted

The promotion rule wants evidence from all three compilers, and the drafts now supply it.

| Compiler | What is bounded | Cap | Shape |
|---|---|---|---|
| openapi | schema nesting | 256 | counter on the lowerer |
| openapi | value nesting | 128 | explicit parameter |
| graphql | type nesting | 256 | counter on the lowerer |
| graphql | value nesting | 128 | explicit parameter |
| protobuf | custom-option nesting | 32 | explicit parameter |

Read from the drafts rather than assumed:

```bash
for b in graphql protobuf; do
  for f in $(git ls-tree -r --name-only "origin/feat/$b-compiler" |
             grep -E "^compilers/$b/.*\.go$" | grep -v _test); do
    git show "origin/feat/$b-compiler:$f" | grep -nE 'max[A-Za-z]*Depth *=' | sed "s|^|$f:|"
  done
done
```

Three findings, and the third contradicts what this section previously assumed.

1. All five sites are the same three lines: a counter, a named cap, and a degradation that reports
   rather than crashes. There is no shared state and no shared cap — only a shared shape.
2. The caps track **what is bounded**, not which compiler bounds it. Both compilers that lower
   values chose 128; both that lower types chose 256. Protobuf's 32 is a different construct, not a
   different opinion.
3. GitHub #174 asserted "the OpenAPI cap of 256 is known to be wrong for an SDL-shaped source". It
   is not: the GraphQL draft independently chose 256 for exactly that. The argument that the guard
   might be shared while the cap stays per-compiler rested on a difference that does not exist.

So there is nothing to promote. A helper wrapping three lines that share no state would add an
indirection to every recursion site and remove nothing, and the per-site degradation — lowered as
any, dropped, unrepresentable — differs at every one of them.

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
```

The derived indexes are maps, and a struct copy shares a map rather than copying it — so "immutable
by value" would be a half-truth if they were exported fields. They are unexported and read through
accessors instead, which makes the immutability real rather than conventional. This costs two
methods and removes a class of bug where a callee's write is visible to its caller's caller.

### 4.1 Where the thirteen fields go

| Field | Becomes |
|---|---|
| `doc`, `opts`, `source`, `srcIndex` | `Ctx`, by value |
| `schemas` | derived once at entry, into `Ctx` |
| `dynamicAnchors` | **stays a memo** — deriving it at entry reports on documents that never ask; see below |
| `types` | the one effect handle |
| `diags`, `out`, `merge` | return values |
| `depth` | explicit parameter — which the bounded-recursion rule wants regardless |
| `operationIDs` | local to the operations loop, which is not recursive |
| `diagnosedConstraints` | **removed** — it was subsumed by identity dedup all along; see below |

*As landed:* two of these rows were written before the code was tried and did not
survive it.

`dynamicAnchors` stays a memo. This section put it in `Ctx` alongside `schemas`,
and §4 offered an `AnchorsNamed` accessor for it. Neither happened, for two
reasons that only appear once you build it. Deriving the index emits a
diagnostic — it warns when a bound stopped the walk short — so deriving it at
entry reports on every document whose tree exceeds those bounds, including the
overwhelming majority that never write `$dynamicRef`. And the walk is not free:

```bash
# what deriving it at entry would cost a document that never asks
go test ./compilers/openapi -run XXX -bench BenchmarkAnchorWalk -benchtime=2000x
```

It measured about 3.7µs and 75 allocations against a compile's 270µs and 2,767 —
roughly 1.4% of the time and 2.7% of the allocations, spent on a keyword almost
nothing uses. So it is a memo, built on the first `$dynamicRef` and not before.

That makes it the one counterexample to §4's claim that interning is
"irreducibly shared and stateful; nothing else is". It is a weaker exception than
that phrasing suggests — a memo caches a pure function of the document, where the
interning table accumulates decisions — but it is shared and it is mutated, and
the claim as written does not admit it.

`diagnosedConstraints` suppressed duplicate constraint diagnostics when a sub-schema is read from
two positions.

*As landed:* it was removed, and memoization never came into it. This section asserted the field was
**not** subsumed by `Diags` identity dedup "because the two reads carry different provenance". They
do not. The stamping overwrites provenance with the pointer being read, and both reads pass the same
pointer — so the two diagnostics agree in severity, code, message *and* provenance, and the second
was already being dropped as a repeat. The field could only ever fire where identity had fired
first.

The two-position read is real and reached, which is why the answer is not obvious from the field's
absence: instrumenting the call shows it entered twice for one pointer with the set reporting
already-seen. What settles it is removing the dedup underneath instead — the targeted test then
reports the identical error twice, printing the two provenances side by side.

```bash
# the test that holds it, and what reddens it
go test ./compilers/openapi -run TestConstraints_HoistedSubSchemaBadBoundSingleError
```

## 5. The OpenAPI split

The call graph is a DAG with exactly one cycle:

```
operations (called by nobody)
  → params → content ┐
                     ├→ schema ⇄ compose ⇄ resolve   the genuine cycle
                     └→ hoist, constraints
```

`schema`, `compose` and `resolve` are mutually recursive and stay one package. Everything else can
be layered.

*As landed:* this diagram first put `resolve` beneath the cycle rather than inside it, and called
`schema ⇄ compose` "the one genuine cycle". The type checker disagrees — `resolve.go` used nine
symbols from `schema.go` and two from `compose.go`, while `schema.go` used five back. Resolving a
`$ref` hoists the sub-schema it names, which lowers a schema, which resolves the references inside
it: one algorithm, not three layers.

What that hid is where the boundary actually is. It does not run between files; it runs between two
questions. *What does this `$ref` name, and is something already interned there* needs two facts
about the document and nothing else — that is `internal/resolve`. *Follow it far enough to lower
what it points at* is the schema walk reached through a reference, and recurses back into it. Seven
of `resolve.go`'s functions answered the first question and left; eight answered the second and
stayed. Deriving that split is what the command below does:

The split is derivable rather than asserted: a function belongs on the resolution side exactly when
its body reaches nothing but the two document facts and the type registry.

```bash
# every lowerer method left in resolve.go, and what it reaches for
awk '/^func \(l \*lowerer\)/{name=$4} /^\tl\./{print name, $0}' compilers/openapi/resolve.go
```

**Tier 0 — already pure.** Eight files contain no `*lowerer` method at all: `value`, `ids`,
`amplification`, `cycles`, `merge`, `facets`, `load`, `diag` — roughly a third of the package's
non-test lines. Four of them own a small local type with a focused method set (`aliasWeigher`,
`nodeView`, `refScan`, `merger`), which is the target shape rather than a counter-example: a type
whose methods serve one purpose is a micro-compiler, and a type whose methods serve eleven is the
problem. These need only a package. Extraction is a move, so goldens are byte-identical by
construction and each step is near-zero risk.

```bash
# the tier-0 set as it was diagnosed, before any of it moved
git show a095636 --stat >/dev/null && for f in value ids amplification cycles merge facets load diag; do
  printf '%s lowerer-methods  %s\n' \
    "$(git show a095636:compilers/openapi/$f.go | grep -cE '^func \(l \*?lowerer\)')" "$f.go"
done

# and where each landed: every tier-0 file is now a package under internal/
ls compilers/openapi/internal/
```

Those eight files are no longer files. Reading the set from the working tree finds nothing, which is
why the command above reads it from the revision it was diagnosed at.

One Tier-0 file does not move alone, and it moved with more than this section predicted. `facets.go`
reads a `site`, and `site`, `siteKind`, `siteAt` and `siteSchema` are declared in `resolve.go` — as
free functions and types, not as `lowerer` methods. They belong with the readers that consume them:
a site is a position, and annotations are what a position carries.

Those four were not the whole of it. `facets.go` and `schema.go` were mutually dependent, so the
readers `schema.go` declared — the raw-node and JSON-object helpers, the docs, flag, XML and
extension readers, and the keyword combinations kept verbatim — had to move as well, along with the
site-home constants and the one-hop reference resolution the site model is built on. What is left in
`resolve.go` is the methods; to see the split as it stands rather than trust this sentence:

```bash
grep -cE '^func ' compilers/openapi/resolve.go compilers/openapi/internal/annotation/annotation.go
```

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
    diag/        OpenAPI diagnostic codes, Newf, HasError
    load/        parse and load the document
    scan/        cycle and amplification refusal over raw source
    nodeview/    the raw source read the way the resolver reads it
    ids/         OpenAPI pointer arithmetic and ID derivation
    value/       value and BigVal lowering
    annotation/  the site model and the facet readers (+ constraints, from 3.2)
    merge/       allOf property reconciliation
    resolve/     reference resolution
    schema/      schema ⇄ compose (⇄ hoist) — the recursive core
    operation/   operations, params, content, auth, meta
```

Imports run one way: `operation` → `schema` → {`resolve`, `annotation`, `merge`, `value`, `ids`} →
`diag` → `compile` → `ir`. `load` and `scan` sit on the entry side.

*As landed:* not, as this section first claimed, "imported only by `openapi.go`". Extraction found
three edges the file-level reading missed — `load` calls `scan` before it parses, `constraints`
reads `load.SupportedMinor` for the document minor, and `hoist` takes the loaded document as its
input. Entry-side means nothing on the entry side calls back into lowering, which holds; it does not
mean one importer. The same reading put `nodeView` inside the cycle scan, where it is declared;
`schema` and `compose` read the source through it too, so it founded a package of its own beneath
both. The lesson generalizes past these two: which file declares a type says nothing about how many
packages consume it, and only the type checker answers that.

```bash
# what actually imports a package, rather than what its declaring file suggests
go list -f '{{$p := .ImportPath}}{{range .Imports}}{{$p}} -> {{.}}
{{end}}' ./compilers/openapi/... | grep -E '> .*internal/(nodeview|load)$'
```

`diag` sits at the bottom because its constructor is the single one that populates severity, code and
provenance, and eight files call it. The OpenAPI diagnostic codes are format-specific strings, so
they stay in the compiler rather than moving to `compilers/compile`; whether the constructor itself later
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
3. **What a package may *write* is enforced beside what it may import.** Deleting a duplicate
   leaves a state, not a rule, so each promotion into the framework arrives with a sweep over the
   syntax that would rebuild it: an `ir.TypeRegistry` written outside the framework, a
   `Naming.Canonical` derived locally, an ID built from a string inside a compiler. Each sweep
   carries a planted counter-test, because a matcher that recognizes nothing passes a clean tree and
   reads as proof.
4. **Anti-regrowth needs a type-surface cap.** Function-size and complexity caps (#83) are worth
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

### 8.2 Two oracles that did not exist yet

**Both landed**, ahead of Tier 1 as this section required. What each was for is kept below, because
the reasoning is what a reader needs to judge whether they still cover it; what changed on landing is
noted at the end of each.

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

*As landed*: it reverses **every** mapping rather than three named ones — mapping order is never
semantic where sequence order is, so the wider permutation costs nothing and reaches more. It
compares the type registry and the *set* of diagnostics rather than whole documents: operations,
responses and content types are held in source order by invariant #7 and reorder by design, and the
three collections a mapping declares inside a type are sorted before comparing rather than ignored.
The diagnostic set is not redundant — reinstating #108's collision interns the same nine types in
both orders while reporting two facts against four, so the registry comparison alone would have
missed it.

**An ID-collision oracle.** Nothing in the repo asserts that two distinct source constructs cannot
mint the same `ir.TypeID`. Invariant 3's corollary — a minted node needs a namespace of its own —
is currently held by reading alone, and CLAUDE.md records the failure mode precisely: violating it
produces no diagnostic in either order, with `pass/validate` clean on both.

This is the most likely way the refactor breaks something no existing guard sees, because moving
lowering between packages is exactly when an ID derivation gets rewritten.

The same traversal answers a second question nothing else can — whether a node's provenance pointer
is *correct*. Invariant 3 derives every named entity's ID from its source pointer, so
`grammar(type.Provenance.Pointer) == type.ID` is exact for any type in a pointer-derived namespace.
Two assertions, neither implying the other:

- `grammar(pointer) == ID` catches an ID minted from the **wrong** pointer — the failure a signature
  change introduces when it passes the wrong `at`.
- `ID → pointer` injectivity catches a grammar that **collapses** two distinct pointers.

Primitives are excluded: `t/prim/<kind>` is shared and derives from no source position.

`irverify` cannot host this as things stand — it is Layer 0 and imports only `ir`, while the grammar
is headed for `compilers/compile`. `internal/harness` is outside the pipeline and can, which is the
fallback. The better answer, to be settled when the grammar moves, is whether the ID *shape* belongs
in `ir` with compilers choosing only the namespace — in which case `irverify` enforces it for every
compiler permanently rather than only under test.

*As landed*: the better answer was taken, so neither assertion lives in the harness. The kind
prefixes moved to `ir` and `compilers/compile` spells its grammar from them, which lets
`irverify.checkIDs` hold every compiler's IDs to the shape and to `path == Provenance.Pointer` —
permanently, for every consumer of a `Document`, rather than only under test.

Injectivity went the other way, to `compile.Types`, because it is not decidable from a finished
document: a derivation that collapses two coordinates overwrites one node with the other, and the
survivor is well-formed, carries its own provenance, and satisfies the agreement check the loser is
no longer present to fail. Only the registry that saw both interns can see the collision, and it
refuses the second through the same channel as its other refusals.

Both oracles landed before Tier 1, proven against the current code first.

### 8.3 What this refactor specifically endangers

| Change | How it breaks | What catches it |
|---|---|---|
| `depth` field → parameter | one path forgets to increment; unbounded recursion on a cyclic or deep spec | `FuzzCycleDetector`, cycle corpus — but only through paths a fixture reaches. Add a per-package depth test |
| ID derivation crosses a package boundary | pointer prefix changes; collision or dangling ref | `danglingcheck_test.go`, plus the new collision oracle |
| Signature change alters the `at` passed | a node records a wrong-but-valid pointer | `irverify.checkIDs`, landed per §8.2. `checkProvenance` validates only the source *index range*, so until that check existed this was caught by nothing but a human reading a golden diff — which is why it was a prerequisite rather than a later improvement |
| Diagnostics returned instead of accumulated | ordering shifts | Goldens, handled per §6.1 |
| ~~`diagnosedConstraints` dissolves into memoization~~ | **Not the risk it looked like.** The field was redundant with identity dedup, so removing it changes nothing; the targeted test for the two-position read already existed and still holds | — |
| `Ctx` copied while its maps are shared | a callee's write is visible to its caller | Accessors make it unrepresentable; assert no exported `Ctx` field is a map |
| Tests move with their code | a test stops reaching what it claims to cover | The coverage gate protects the *code*, not the *test*. Plant a defect in each moved package and confirm that package's own suite reddens |

### 8.4 Blind spots, stated so they are not mistaken for coverage

- **`harness.Check` returns at the first error diagnostic, before `irverify` runs.** A fixture
  written to exercise an invariant check that also trips an error diagnostic never reaches it.
  Establish where each new fixture lands rather than assuming it arrives.
- ~~**The annotation grid cannot express a carrier position** (#142)~~ — **closed.** The grid gained
  a kind for each carrier, and they are separate kinds because the two hold different sets:
  `ir.Property` has XML and Visibility fields and `ir.Parameter` has neither, so one shared row would
  have had to pick an answer and covering either would have read as covering both.
- **The golden corpus shares the blind spots of the code that produced it.** A construct no fixture
  contains is unprotected by byte-equality. The conformance corpus is the intended counterweight and
  is only as complete as `ir-spec-matrix.md`.
- ~~**`irverify` checks provenance index range, not pointer correctness.**~~ — **closed**, and in the
  verifier rather than the harness: the ID shape went to `ir`, so `checkIDs` holds the property for
  every consumer of a `Document`. What it still cannot see is a pointer that is wrong in a way the ID
  is wrong in too, since it compares them against each other rather than against the source.
- **Byte-identical goldens are the load-bearing neutrality guard, and the comparison had a known
  hazard.** `compareGolden` has no line-ending guard, and the repository carried no
  `.gitattributes` (#48), so a checkout with `core.autocrlf=true` converted 346 files and failed
  three test functions for reasons unrelated to the IR. Harmless on the Linux runner; corrosive if
  it trains a reader to treat golden diffs as environmental noise. **Landed** before the programme
  started rather than during it: the pin, and the test that reports its removal, since CI checks out
  LF whether the rule is there or not.

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
  It is held back because `$ref` handling still carries an open defect — #40, percent-encoded
  fragments failing to resolve — and an index built over it would bake it in. #143 (siblings
  adjacent to a `$ref` on an allOf branch dropped) and #141 (an `$anchor` fragment derived from as
  though it were a pointer) are closed. Filed as a follow-up blocked on the rest closing.
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
| ~~0.1~~ | ~~Fix archtest prefix matching so one compiler cannot import another (#57)~~ | **Landed.** Allowlist entries are exact unless suffixed `/...`, and a planted sibling import now fails | — |
| ~~0.2~~ | ~~General two-order oracle in `internal/harness`~~ | **Landed.** Reverses every mapping across the conformance corpus and diffs the registry and the diagnostic set; reverting #108's fix reddens the sweep | — |
| ~~0.3~~ | ~~ID-collision oracle~~ | **Landed**, split by where each half is decidable (§8.2): `irverify.checkIDs` holds every ID to its shape and to `path == Provenance.Pointer`, and `compile.Types` refuses a derivation that collapses two coordinates. Both proven by planting one | — |
| ~~1.1~~ | ~~Promote the ID grammar into `compilers/compile`~~ | **Landed.** `compilers/openapi` derives no ID except through the framework, goldens byte-identical, and the minted-namespace rule is refused by `compile.Types` rather than asserted in a comment | — |
| ~~1.2~~ | ~~Promote the canonical naming grammar (the segmentation is decided — §3.1)~~ | **Landed.** `compilers/compile` holds the one implementation, an architecture test keeps a second from being written, and each rebasing draft carries its own golden update | — |
| ~~1.3~~ | ~~Extend `irverify` to check segmentation, not only casing (#73, #54)~~ | **Landed with #161**, ahead of 1.2: `ir/naming-not-words` rejects a lowercase but unsegmented canonical, proven by planting the old grammar and watching the corpus sweep redden. #54 (`Hint`) stays open | — |
| ~~2.1~~ | ~~Tier-0 extraction: `diag`~~ | **Landed.** Goldens byte-identical, its own rules entry admits `ir` alone, and the package carries table-driven tests needing no document | 0.1 |
| ~~2.2–2.7~~ | ~~The remaining Tier-0 extractions: `load`, `scan`, `ids`, `value`, `annotation` (+ site), `merge`~~ | **Landed.** Goldens byte-identical, each package carries its own rules entry proven by planting a forbidden import, and each carries unit tests needing no document. Two departures from the plan above: the scan took `nodeview` with it as a package of its own, because `schema` and `compose` read the source through it too, and `annotation` took the readers `schema.go` declared as well as the four in `resolve.go` — the two files were mutually dependent | 0.1 |
| 3.1 | Introduce `Ctx` with accessors; derive indexes at entry | No exported `Ctx` field is a map; goldens byte-identical | 2.x |
| 3.2 | Convert the leaves to the contract: `constraints` → `annotation`, `meta` and `auth` → `operation` | Those files hold no `lowerer` method | 3.1 |
| 3.3 | Extract `internal/resolve` | ditto, plus a bounded-recursion test | 3.2 |
| 3.4 | Extract `internal/schema` (schema ⇄ compose ⇄ resolve) | ditto. Both open questions are already settled: `hoist` dissolved into the framework, and `diagnosedConstraints` was redundant and is gone | 3.3 |
| 3.5 | Extract `internal/operation` (operations, params, content) | ditto | 3.4 |
| 3.6 | Delete the `lowerer` struct | `grep -c '^func (l \*\?lowerer)'` returns 0 | 3.5 |
| 4.1 | Type-surface cap in `internal/archtest` | Re-adding ten methods to one type fails the build | 3.6 |
| 4.2 | Function-size and complexity caps in `golangci-lint` (#83) | The gate runs them; a 71-line function fails | 3.6 |

Phase 4 lands after Phase 3, not before: the caps are calibrated against the finished shape, and
landing them first would only encode the current one.

## 13. Disposition of the existing backlog

| Issue | Disposition |
|---|---|
| #57 archtest cannot enforce compiler isolation | **Closed.** Landed with #161/#143; it was a prerequisite for every package boundary here |
| #73 naming grammar and primitive IDs are cross-compiler ABI in one compiler | **Partly answered, and its own proposal was right about the naming half.** That grammar now lives in `ir` with `irverify` validating against it, which is its first acceptance bullet met as written. The ID grammar went to `compilers/compile` — a compiler's path is its own and nothing in `ir` can compute one — but the `t/prim/<kind>` constructor #73 also asks for is still there, so its second bullet is open and it stays open with it |
| #54 cased `Naming.Hint` passes the neutrality check | **Still open.** 1.3's segmentation work did not reach `Hint`: closing it means changing how hints are derived and regenerating every golden, which is a different change from tightening the checker. The exclusion is now stated in `checkNaming` rather than left to be inferred |
| #83 enforce size and complexity caps in lint | **Closed by 4.2**, deliberately last |
| #66 extract a shared JSON-Schema→IR lowering core before the next compilers land | **Superseded.** Its premise expired — the next compilers landed without it (#20, #21). §3 replaces it with evidence-based promotion. To be closed with that reasoning, not silently |
| #142 the annotation matrix cannot reach a carrier position | **Closed**, independently of this work as §8.4 said it could be: the grid gained a kind per carrier, and the two are separate kinds because their carriers hold different sets |
| #40, #141 `$ref` handling defects | **#141 closed**: a fragment that is not a JSON pointer is refused rather than derived from, and `irverify` now rejects an ID the grammar could not have produced. #40 still **blocks the source index** (§10). #143, listed here before, is closed |
| #20, #21 GraphQL and Protobuf drafts | **Evidence, not work items** (§2). Rebasing is later work |
| Naming grammar divergence across compilers | **Closed by #161**, filed and fixed separately: a live invariant-4 violation, independent of whether this architecture work proceeds |
