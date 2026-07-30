# Micro-Compiler Architecture — Design

Status: approved, not yet implemented. Scope: `compilers/compile`, `compilers/openapi`,
`internal/archtest`, `internal/harness`, `ir/irverify`.

Claims about code in this document were true at `a095636`. Where a number would rot, this
document gives the command that derives it instead of the number.

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
§8 records why, because the reasoning is not obvious and will otherwise be re-litigated.

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

**Tier 1 — the god object.** Every method lives in eleven files: `schema`, `compose`, `operations`,
`content`, `resolve`, `params`, `hoist`, `auth`, `meta`, `constraints`, `openapi`. Target packages
follow the call graph: `internal/schema` (schema ⇄ compose), `internal/operation` (operations,
params, content), `internal/resolve`, with the small leaves distributed.

**Unsettled:** `hoist` is called from four places and its job — intern an anonymous type at a
pointer, return a ref — is close to `compile.Types.Intern`. Whether it becomes a package or largely
dissolves into the framework is determined by reading its methods during implementation, not
decided here.

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

## 7. Enforcement and testing

### 7.1 Enforcement

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

### 7.2 The missing oracle

**The general two-order diff does not exist.** CLAUDE.md prescribes it;
`TestOneOf_CoDeclaredDistributionIsOrderIndependent` implements it by hand for three cases, at
exactly the site where the pointer-collision bug was found. `harness.deterministic` looks like the
general version and is not — it recompiles *the same bytes*, proving same-input-same-output rather
than permuted-input-same-output. Only the second property catches order-dependent interning.

This refactor moves interning, which is what order dependence is sensitive to. The oracle — permute
declaration order in a source, `cmp.Diff` the two documents — therefore lands **before Tier 1**, not
after. A byte-identical golden cannot provide this guarantee, because a golden is one order.

### 7.3 Held at every step

Goldens byte-identical; the 27-cell annotation grid green with no `knownGap` reintroduced;
`irverify` extended with the naming-segmentation check; 100% statement coverage. Per the repo's
verification discipline, every new guard gets a planted defect to prove it reddens — including the
new oracle, which is proven against a known order-dependent input before it is trusted.

## 8. Rejected: a functional-programming dependency

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

## 9. Deliberately out of scope

- **The source index.** Indexing the raw tree once (pointer → node + shape) would make resolution a
  lookup, share one walk across cycle and amplification detection, and give `--explain` a substrate.
  It is held back because `$ref` handling currently carries open defects — #40 (percent-encoded
  fragments fail to resolve), #141 (an `$anchor`-resolved schema interns a malformed type ID), #143
  (siblings adjacent to a `$ref` on an allOf branch are dropped) — and an index built over them
  would bake them in. Filed as a follow-up blocked on those closing.
- **Rebasing the GraphQL and Protobuf drafts.** They are evidence here, not work items.
- **A new-compiler skeleton demo.**

## 10. Risks

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

## 11. Sequencing

Each phase is one or more PRs, each scoped to one logical change.

| Phase | Work | Depends on |
|---|---|---|
| 0 | Fix the archtest prefix hole (#57); build the general two-order oracle and prove it reddens | — |
| 1 | Promote naming grammar and ID grammar into `compilers/compile`; extend `irverify` with the segmentation check (#73) | 0 |
| 2 | Tier-0 extractions into packages, one per PR, each byte-identical | 0 |
| 3 | Introduce `Ctx`; convert leaves, then `resolve`, then `operation`, then `schema` ⇄ `compose`; delete the `lowerer` struct | 1, 2 |
| 4 | Type-surface cap in archtest; function-size and complexity caps in lint (#83) | 3 |
| 5 | Follow-ups: source index (blocked on #40, #141, #143); draft rebases; disposition of #66 and #142 | 4 |

Phase 4 lands after Phase 3 rather than before it, because the caps are calibrated against the
finished shape; landing them first would only encode the current one.
