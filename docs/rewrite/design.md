# Compiler Decomposition — Design

Status: superseded. TEMPORARY WORKING NOTE — see docs/rewrite/README.md. Not normative.
Scope: `compilers/openapi`, new `compilers/compile`, `internal/harness`, `internal/archtest`

**Every file:line citation below was taken against `main` at `996e40a`, before the work on this
branch, and none has been re-anchored.** Most no longer land where they did, and four symbols they
name — `attachSchemaExamples` among them — no longer exist at all. They are left as written because
this document records an argument that measurement overturned; re-anchoring evidence for a
superseded recommendation would suggest it is still live. Read a citation as "this was true then",
and check the current tree before acting on any of it.

**What shipped, and what this design got wrong.** Two of the four sequenced steps were built: the
annotation-retention suite (`test/openapi-conformance-grid`) and the resolver extraction
(`refactor/openapi-resolver-extraction`). The framework (`compile.Types` / `compile.Diags`) and the
`--explain` flag were not.

The facet extraction — the change this document was written to justify — is **not recommended as
specified**, and the measurement is why. Its case rested on the claim that the site-versus-referent
rule was being re-decided per attachment point and getting it wrong. Measured, it is already correct
for 8 of 9 reference-site annotations. The real duplication is on a different seam entirely
(`fillModelDetail` is reached by only two of six lowering paths), and the twelve gaps split into
seven compiler defects and five IR design questions — see "Follow-up work".

## Problem

The OpenAPI compiler is one mutable context object with everything hanging off it. `lowerer`
(`compilers/openapi/hoist.go:23-49`) carries twelve fields spanning four unrelated concerns —
document assembly, pointer interning, diagnostic accumulation and dedup, recursion bounding — and
123 methods are defined on it across the package's non-test files. `schema.go` alone is 1,570 lines.

The cost is not raw duplication. Annotation *conversion* is already reasonably shared —
`appendExample` (`schema.go:939`) states in its own doc comment that it serves every example site,
and the four docs readers (`fillTypeDocs`, `fillOperationDocs`, `tagDocsFrom`, `infoDocs`) read four
genuinely different source types rather than repeating one another. A refactor sold on collapsing
duplicate readers would be selling something that mostly is not there.

The cost is in three specific things.

**The site-versus-referent decision is made independently at each attachment point.** An `example`
or a bound written beside a `$ref` belongs to the position it is written at, not to the referent.
Nothing centralizes that rule, so each attachment site re-decides it — and the sub-schema
`$ref`-sibling defect was several of those sites deciding it wrong, which is why fixing it meant
finding all of them. The conversion being shared did not help, because conversion was never the
part that was wrong.

**Ownership and ordering are coupled implicitly.** `attachSchemaExamples` (`schema.go:917`) checks
`byPointer` for node ownership before converting, and its doc comment explains why: *"the callers
cover a pointer in either order, and only the one that finds a node may emit conversion
diagnostics."* Correctness depends on an ordering relationship between callers that no signature
expresses. The same comment records the ownership rule — *"a schema whose body reduced to a shared
primitive owns no node; its examples stay with the declaring property"* — as prose rather than as
anything enforced.

**Annotation readers are untestable in isolation.** Because they are methods that mutate an IR node
in place (`fillPropertyDetail`, `fillPropertyDefault`, `fillPropertyExamples`,
`fillPropertyConstraints`, `fillModelDetail`, `fillAdditional`, `fillValidationOnly`), answering
"does this schema yield these examples" requires standing up a whole `lowerer`. Two functions in the
package already escaped this shape — `constraintsFromSchema` (`constraints.go:17`) and
`extensionsFrom` (`schema.go:1399`) are pure `(source) → (irFragment, []ir.Diagnostic)` — and they
are markedly easier to test and reason about. This design generalizes what those two already
demonstrate.

## Goals

- One place where the site-versus-referent rule is decided, rather than once per attachment point.
- Annotation readers unit-testable without constructing a compiler.
- A shared seam that keeps invariant 3 (stable IDs; one node per source coordinate) mechanically
  true as further compilers land, rather than by convention.
- A conformance grid that makes an unhandled annotation position a visible empty cell instead of a
  user-reported bug.

## Non-goals

- **Sharing traversal across compilers.** Of the eight planned source formats, Smithy, GraphQL, and
  Protobuf have effectively no anonymous-type hoisting — every shape is already named and
  top-level. A framework that owned the walk would push three formats through machinery they do not
  need, and its ordering and cycle rules would be JSON-Schema-shaped permanently.
- **Changing IR semantics.** No IR schema change, no `ir-design.md` change. Golden output is
  byte-identical at every step except where a step fixes a genuine defect, which is then a normal
  reviewed change with a corpus update.
- **Reorganizing operations, auth, or meta lowering** beyond what falls out of the facet extraction.

## The dividing line

Three categories, split on two questions: *does it need the registry or recursion*, and *does it
produce an IR fragment*.

> A **facet** is pure — needing neither the type registry nor recursion — and produces an IR
> fragment. An annotation reader.
>
> A **predicate** is pure and produces a structural answer about the source: a bool, a type list, a
> pointer. Not IR.
>
> **The walk** is everything that needs `Types` or recurses.

Both of the first two are mechanically checkable per function, and both are unit-testable without a
compiler.

The predicate category exists because the package already has about twenty of them — `schemaHasNull`,
`schemaAdmitsNull`, `isNullSchema`, `effectiveTypes`, `hasUnionSiblings`, `isFalseSchema`,
`schemaIsBinary`, `schemaIsArray`, `schemaHasType`, `declaredSchema`, `oneOfAnyOfHasNull`,
`compositionRequired`, and the rest — already written as pure free functions. A two-way split would
file them under "the walk" for want of anywhere else, and a migration acting on that could
reasonably fold them into `*lowerer` methods, losing the cheapest tests in the package. Naming the
category costs nothing and is purely protective: **predicates stay free functions.**

It also right-sizes the problem. Roughly twenty predicates plus nine facets are already pure, so the
genuinely stateful walk is smaller than a line count over `schema.go` suggests.

It is also what makes the "gradually build the IR from independent pieces" goal safe without
running facets as separate passes over the document. A sequential chain of passes cannot work here:
the source-coordinate → IR-node relation is *computed by* lowering, not derivable from the
coordinate. `nullUnionCollapse` (`schema.go:1233`) leaves a position owning no node at all;
`lowerComponentSchema` (`schema.go:50`) branches on whether `schemaRef` already interned at the
component's own ID. A later pass walking the source independently could not reliably find where to
attach what it produced, and would have to consume a table the first pass published — sequential
dependence, not isolation. It would also reintroduce the multi-walk name derivation that
`architecture.md` §2.1 phase 3 explicitly designs out.

Because a facet cannot consult the registry, it cannot depend on the walk's output, so it does not
need to be a separate pass. It is called *during* the walk and returns a value.

**Stays in the walk:** reference resolution, interning, hoisting, `allOf`/`oneOf`/`anyOf`
(`compose.go`), discriminators, property merging (`mergeProperty`, `reconcileProperty`),
enum-as-union, and all naming. Naming looks like an annotation because it fills an IR slot, but it
derives from position rather than from the source node — `variantHint(b, i)` takes a branch index,
`declarationHint(pointer, fallback)` takes a pointer — so it is walk knowledge. `canonicalWords` is
a pure string helper: neither facet nor predicate, since it reads no source node at all.

Null handling splits across the categories rather than sitting in one: `schemaHasNull`,
`schemaAdmitsNull`, `isNullSchema`, and `nullUnionCollapse` are all pure and therefore predicates —
only *acting* on a collapse, which interns, is walk work.

## Facets

Two classes, distinguished by what happens at a reference site. The existing code already knows the
difference; it just has no name for it.

**Site-only** — the annotation belongs to the position it is written at, with no fallback to the
referent. `fillPropertyExamples` and `fillPropertyConstraints` (`schema.go:878`, `889`) take only
the reference-site schema. This is the class the `$ref`-sibling defect broke.

**Site-overrides-referent** — read at the site, fall back to the target. `effectiveDescription`,
`effectiveDeprecated`, and `effectiveVisibility` (`schema.go:1312-1342`) all take `(ref, tgt)`, and
`pickFlag` (`schema.go:1344`) is already a generic helper for exactly this shape.
`fillPropertyDefault` takes `(ref, tgt)` too.

| facet | class | becomes the facet | stays outside it |
|---|---|---|---|
| docs | overrides | `fillTypeDocs`, `fillOperationDocs`, `tagDocsFrom`, `infoDocs` — four readers over four source types, kept distinct — plus `effectiveDescription` | the `*ir.Docs` write |
| examples | site-only | `schemaExamples`, `exampleList`, `mediaExamples`, `appendPluralExample`, `appendValuelessExample`, `appendExample` | `attachSchemaExamples`'s `byPointer` ownership lookup and node write |
| constraints | site-only | `constraintsFromSchema` — already pure | — |
| default | overrides | `valueFromNode` plus `fillPropertyDefault`'s site/referent pick | the `*ir.Property` write |
| deprecated | overrides | `effectiveDeprecated`, `pickFlag` | — |
| visibility | overrides | `effectiveVisibility` | — |
| extensions | site-only | `extensionsFrom` — already pure | — |
| xml hints | site-only | `xmlHints` — already pure | — |
| validation-only | site-only | `ifThenElseRaw`, `containsRaw`, `unevaluatedRaw` | `fillValidationOnly`'s `Extensions` write |

The site/referent *pick* — `effectiveDescription`, `effectiveDeprecated`, `effectiveVisibility`,
`pickFlag` — is a facet, not walk work: those are pure two-argument functions needing neither the
registry nor recursion. What stays in the walk is *supplying* the referent, since obtaining it
requires reference resolution (`refTargetSchema`, `schema.go:843`).

Note what the fourth column shows: the dividing line does not classify existing functions so much as
**split** several of them. `attachSchemaExamples` is a pure read (`schemaExamples`) fused to an
ownership resolution (`byPointer` lookup and node mutation); the read becomes the facet and the
ownership half stays in the walk, where it can be stated once instead of at each attachment point.
That split is the mechanism by which the site-versus-referent rule stops being re-decided per site.
The docs row is the counter-example worth keeping honest about: its four readers stay four, because
they read four different source shapes and merging them would buy nothing.

Example `$ref` resolution (`appendPluralExample` dereferencing `/components/examples/`) stays on the
facet side. It resolves a *reference to an example object* and never consults `Types`, so it does
not cross the line — but it is the closest thing to a straddler in the set.

### Facet conventions

Every facet is an unexported free function in `compilers/openapi`, pure, returning
`(value, []ir.Diagnostic)`. The framework never sees an `*oas3.Schema`.

A facet that needs configuration takes it as an explicit parameter rather than reading `l.opts` —
`constraintsFromSchema(s, exclusiveBoolean)` already does exactly this, deriving the flag from
`exclusiveBoundIsBoolean()` at the call site. That is what keeps a facet testable without a compiler,
and it is the property to preserve when extracting the rest.

`valueFromNode` (`value.go:23`) currently returns `error`. A malformed scalar in a `default` is a
spec problem, not an I/O or programmer error, so it normalizes to `[]ir.Diagnostic` per the repo's
stage contract.

`run()`'s four-phase order (`openapi.go:52`) — component schemas, then security schemes, then the
service walk, then meta — is unchanged. It exists so `$ref`s resolve against already-registered IDs,
which this design does not touch.

## Site

A **site** is a position that owns an IR node, tagged with whether it declares a type or references
one. The rule it encodes:

> A position that carries its own annotations and also references another type must own its own
> node.

That is what `internAlias` (`schema.go:114`) does today, and violating it is what the `$ref`-sibling
defect was. `siteSchema` (`schema.go:74`) is the beginning of this concept already — its doc comment
states that every position owning a node reads through it.

Site is deliberately **not** a framework type. Its framework-side content would be two fields
(coordinate, kind), while the useful part — the site node and the referent node — is format-typed
and cannot leave the compiler. So it lives in three places:

- the **rule** in `docs/architecture.md`, as a constraint every compiler must satisfy;
- a **format-typed struct** in `compilers/openapi`, carrying the coordinate, kind, and the two
  `*oas3.Schema` values;
- a **`SiteKind` enum** in `internal/harness`, next to `Aspect`, because those two are the axes of
  the same conformance grid and should not straddle packages.

Being precise about what this buys: `Site` does not enforce the rule at runtime. It puts the
declaration-versus-reference question into every facet signature so it cannot be silently skipped.
The discipline is the shared insight, and it generalizes — Smithy traits on a member versus on the
target shape, GraphQL directives, and Protobuf field-versus-message options are all the same fork.

## Components inside the walk

"The walk" is not one thing, and leaving it undifferentiated would put most of the package in an
unnamed bucket. Two components inside it are cohesive enough to extract, test, and reason about
independently.

### The resolver

Eleven functions answer one question — *given a `$ref` at this position, what node does it point at,
and what does this position own?* `schemaRef`, `refTypeRef`, `resolveSchemaRef`, `declaredSchema`,
`hoistSubSchema`, `refNullable`, `refTargetSchema` (`schema.go`), and `internalPointer`,
`internedID`, `resolveComponentRef`, `sameFile` (`ids.go`).

**This is where the site rule is decided**, and naming it closes a hole in this design: the goal
stated above is that the rule stops being re-decided at each attachment point, but without a named
component there is nowhere for it to move *to*. Every site's coordinate, kind, and referent
originate here, and every attachment point downstream consumes what the resolver produced rather
than re-deriving it.

It is also where the defects have been. Both recent reference-lowering fixes — hoisting referenced
components at their declaration, and the sub-schema `$ref`-sibling gap — were changes to functions
in this set. It has the highest defect density in the package and the cleanest interface, which
makes it the first thing to extract, not the last.

### The merge engine

Nineteen functions across roughly 376 lines of `schema.go` (`fillModelProperties` through
`strConflictDetail`) implement one job: *when two `allOf` branches redeclare the same field,
reconcile them or report the conflict.* It owns `codeConflictingRedecl` and the whole
constraint-compatibility lattice — `mergeConstraints`, `constraintsConflict`, `boundConflictDetail`,
`typesConflict`, `differentTypeKind`, and the rest.

It is the largest cohesive block in the package and the most intricate logic in it, so the
testability win is the largest here too. It needs the registry — `typesConflict` resolves `TypeRef`s
to compare them — so it is genuinely walk work, but that dependency is narrow enough to inject as a
`func(ir.TypeRef) (ir.PrimKind, bool)` and test against a stub. Unlike the resolver it has no recent
defect history, so it is a testability and legibility extraction rather than an urgent one.

Everything else — structural type construction (`lower`, `lowerTyped`, `lowerModel`, `lowerArray`),
composition (`compose.go`, already its own file), operations, params, content, auth, meta — stays as
it is. Splitting the core type construction would not isolate anything; it *is* the walk.

## The framework: `compilers/compile`

Two types, roughly 120 lines, importing only `ir`.

| type | owns |
|---|---|
| `compile.Types` | `ir.TypeRegistry`, the coordinate → `ir.TypeID` map, `Intern`, shared primitive interning |
| `compile.Diags` | diagnostic accumulation and dedup by identity |

`Types` absorbs `lowerer`'s `out.Types`, `byPointer`, `intern`, `internNode`, `primRef`, `primID`,
and `primTypeID` (primitives are IR-universal; every compiler should intern `t/prim/string`
identically). `Diags` absorbs `diags`, `emitted`, and `appendDiag`.

Deliberately excluded:

- **`ir.Document`.** It has eighteen fields and exactly one — `Types` — carries an invariant the
  framework protects. The compiler assembles the rest, as `run()` (`openapi.go:52`) already does.
- **Depth bounding.** Twenty lines, no cross-compiler invariant, and OpenAPI's cap of 256
  (`hoist.go:12`) would not be the right number for an SDL walk. Stays in `compilers/openapi`.
- **`Site` and `Aspect`.** Covered above.
- **`commonFor`.** Its `componentSchemaName` branch is an OpenAPI fact about which coordinates name
  a declaration, so the compiler supplies that rather than the framework sniffing for it.
- **`namedTypeID` / `anonTypeID`.** The ID scheme stays per-compiler; `Intern` already takes the ID
  as a parameter.

The governing principle for anything borderline: **promoting into the framework later is additive;
demoting is a breaking change across every compiler.** Borderline items start outside.

A 120-line package is worth a package boundary for one reason: the enforcement rule below is
inexpressible without an outside.

## Assembly discipline

Facets return values; nodes are built as struct literals; nothing mutates an IR node in place. The
`fill*(dst *ir.X, ...)` shape is removed, and its absence is greppable.

Each site calls one `annotations(site)` function that invokes every site-local facet. That single
call site is the "one place to fix" property the whole design is for: not because it merges
duplicate readers — mostly there are none — but because the site-versus-referent choice, and the
node-ownership check `attachSchemaExamples` currently fuses into its read, are made there once
instead of at each attachment point.

Referent population is centralized in the walk's reference handling, where `refTargetSchema`
(`schema.go:843`) already resolves it.

## Diagnostics

Facets are pure and return `[]ir.Diagnostic`; the walk appends them through `compile.Diags`, which
dedups by full identity (severity, code, message, provenance) — the policy `emitted` implements
today (`hoist.go:36-44`).

`diagnosedConstraints` (`hoist.go:32-35`) is **not** assumed to disappear. It suppresses a repeat
report when one sub-schema is reached from two different coordinates, and two different coordinates
produce two different provenances, so identity dedup would not collapse them. Whether it survives
is a question about desired policy — should one defect reachable from two positions report twice? —
and policy of that kind is compiler-side. The migration decides it with a test rather than by
assumption; the framework offers identity dedup and nothing more.

## Conformance grid

The grid is **(format × aspect × site-kind)**, extending the capability corpus `CLAUDE.md` already
commits to. `Aspect` and `SiteKind` are string enums in `internal/harness` — test vocabulary, not
production types. Nothing in the write path references them, and `ir.Diagnostic` gains no field, so
`ir-design.md` is untouched.

Each cell asserts lossless capture of one annotation at one kind of position for one format. The
`$ref`-sibling defect is two cells — `(openapi, examples, reference-site)` and
`(openapi, constraints, reference-site)` — visibly unfilled rather than discovered downstream.

This grid, not the decomposition, is what prevents the defect class. Facets consolidate where the
site rule is applied; they do not stop the walk from failing to build a site at some position in the
first place. Being accurate about that division of labour is why the grid ships first — and why it
is worth shipping even if nothing after it ever happens.

## Sequencing

1. **Conformance grid** against the compiler as it stands. No refactor required, finds empty cells
   immediately, and becomes the acceptance test for everything after.
2. **Resolver extraction** — the eleven reference-resolution functions become a named component that
   produces sites. Highest defect density, cleanest interface, and it is what gives the site rule
   somewhere to live.
3. **Facet and predicate extraction**, one facet per PR. Predicates mostly need no work beyond
   staying free functions as things move around them.
4. **`compile.Types` + `compile.Diags`** and the enforcement rule.
5. **Merge engine extraction** — testability and legibility, no urgency.
6. **`morphic compile --explain <pointer>`** — with `Types` owning the coordinate map, "coordinate →
   node → which facets filled it" becomes dumpable, answering "why did my example disappear" in one
   command.

Step 1 first because it is what makes refactoring 17k lines safe: it proves nothing was dropped,
rather than trusting the golden corpus to have happened to cover it.

The resolver moved ahead of the facets on review. The original ordering put examples first on the
grounds of call-site count and defect history; the call-site count turned out to be wrong (see
Problem), and the defect history is really *site-resolution* history — both recent reference-lowering
fixes were changes to resolver functions, not to example readers. Extracting the resolver first also
means the facets land against a component that already produces sites, rather than each facet
inventing its own site handling and being reconciled later.

The framework lands fourth so it is designed against extracted components rather than against the
current tangle.

## Enforcement

The rule: **no package outside `compilers/compile` writes to `ir.TypeRegistry` directly.** This is
what keeps invariant 3 true across eight compilers rather than by convention.

`internal/archtest/arch_test.go` currently parses imports only, so this is a **new test**, not an
entry in the `rules` map — an AST check for assignment into an `ir.TypeRegistry`-typed value or
composite literals of it outside the allowed package. The file already carries the `go/parser` and
`go/token` machinery to build on.

The import graph itself needs **no change**. `hasAllowedPrefix` matches on path-segment boundaries,
so `compilers/openapi`'s existing `module + "/compilers"` entry already permits importing
`compilers/compile`. And because `hasOwnRule` only skips subdirectories carrying their own entry, an
unkeyed `compilers/compile` is walked under the `compilers` rule — `{module + "/ir"}` — which is
exactly the constraint it should be held to. Adding an explicit entry is optional and buys only
readability.

## Testing strategy

- **Facet unit tests** — table-driven, on pure functions, with no `lowerer`. This is the testability
  win; it is currently impossible for anything in a `fill*` function.
- **Conformance grid** — as above, the acceptance test for the migration.
- **Golden corpus** — unchanged, and the regression net for each strangler step. Byte-identical
  output is the pass condition for any step that is not itself a fix.
- **Architecture test** — extended with the registry-write rule.
- **Coverage** — 100% holds at every step, as it does today.

## Measured outcome — the grid has run

This section has been measured twice, and the first measurement was wrong in a way worth keeping on
the record.

**The first pass.** An 18-cell grid recorded 14 preserved, 4 gaps, and concluded that the facet
extraction was not warranted: the site-versus-referent rule was already right for 8 of 9
reference-site aspects, and the four gaps were missing structural homes rather than precedence
failures.

**Why it was wrong.** Every declaration cell had been measured with an *object-shaped* component,
which routes through `lowerModel`/`fillModelDetail`. A scalar-shaped component routes through
`internAlias`, which forwarded only `Constraints` and `Examples`. So `docs`, `deprecated`,
`extensions` and `validationOnly` were silently dropped on a declaration as ordinary as
`UserId: {type: string, format: uuid, description: "..."}` — and the grid could not see it, because
the object shape was baked into the fixture rather than varied. The table was not measuring what its
row labels said it measured.

**The second pass** split the declaration site into its model-shaped and scalar-shaped forms, taking
the grid to 27 cells and roughly doubling the gap count. `compilers/openapi/annotations_test.go` is
the live version; read the `knownGap` markers there rather than a count copied into prose.

**What survived.** The conclusion held, but not its reasoning. The facet extraction was still the
wrong change — not because the compiler reads annotations correctly everywhere, but because the real
duplication was on a seam this grid never measured: annotation handling was bolted to
`fillModelDetail`, reachable from two of `lower()`'s six destinations. Making one reader run above
the dispatch fixed every declaration-site cell at once, and it is a fraction of the decomposition's
size. That is #114.

**What this cost and bought.** The sequencing still earned its keep — shipping a measurement first
killed a multi-PR refactor that reasoning alone had justified twice. But a grid is a fixture like
any other, and a fixture that varies one axis while silently pinning another measures the pin. The
first table was not corrected by review reading it; it was corrected by someone compiling a scalar
declaration and finding the annotation gone.

## Risks

- **The framework is designed against one compiler.** Its first real validation is Milestone 4
  (TypeSpec or Smithy); Milestone 3 is the first emitter. Holding the shared surface to two types is
  the mitigation — a small surface is cheap to revise when a second compiler disagrees with it.
- **Strangler steps that are meant to be behaviour-neutral may not be.** Golden byte-equality per
  step is the guard; any step that does change output must justify it as a fix with a corpus update
  in the same PR.
- **The facet extraction's payoff is narrower than a decomposition refactor usually promises, and
  its cost is not.** The compiler is not carrying much duplicated annotation reading — conversion is
  already shared, and the docs readers are genuinely distinct. So the win is testability plus
  consolidating the site rule, both real but neither dramatic, against a migration touching a
  17k-line package that currently holds 100% coverage and has just been hardened through several
  rounds of review. It is the step to cut or defer if the first emitter starts competing for time;
  the grid, the resolver, and the framework all stand without it. The grid decides it better than
  an estimate can — a large number of wrong site-rule cells argues for the extraction, a small
  number argues against.
- **The merge-engine extraction is the most deferrable item here.** It is the largest block and the
  best testability win, but it has no defect history, and its registry dependency means it is the
  one extraction that cannot be made purely mechanical. If anything slips, it should be this.
- **The facet/walk line has one near-straddler**, noted under Facets: example `$ref` resolution.
  It holds as written, but it is where to look if a later facet wants registry access.

## Follow-up work identified during implementation

None of these are in scope for the two PRs this design produced. Each is recorded with the
evidence that motivated it, so it can be picked up without re-deriving the argument.

### Triage the twelve gaps by whether the IR has a home

The retention suite reports twelve gaps, but they are two different problems and only one
needs design work:

| | gaps | |
|---|---|---|
| **IR gap** — no field exists | `constraints`/model, `default`/model, `default`/scalar, `visibility`/model, `visibility`/scalar | 5 |
| **Compiler gap** — field exists, never populated | `docs`/scalar, `deprecated`/scalar, `extensions`/scalar, `validationOnly`/scalar, `xmlHints`/model, `xmlHints`/scalar, `validationOnly`/reference | 7 |

The seven compiler gaps are actionable immediately — `TypeCommon.Docs`, `.Deprecation`,
`.Extensions` and `.XML` all exist and are simply never written on the scalar-alias path, and
`validationOnly`/reference is the `$ref` dispatch short-circuit. No IR change needed.

The five IR gaps cluster into one question, not five: `constraints`, `default`, and `visibility`
each have a home on `Property` but none on a *type*. "Should a declared type carry a default, a
visibility, and value constraints the way a property can?" is worth answering once, deliberately,
rather than patching field by field.

### Rename `Extensions` — it is one source format's word, and it collides with another's

`Extensions` is OpenAPI/AsyncAPI vocabulary. Worse, Protobuf — a planned compiler — uses
`extensions` for a different feature entirely (proto2 reserved field-number ranges that let
outside code add fields to a message). A Protobuf compiler author reading `ir.Extensions` would
reasonably expect that.

Every planned format has its own term: OpenAPI/AsyncAPI *extensions*, Protobuf *options*, GraphQL
*directives*, Smithy *traits*, TypeSpec *decorators*, Erlang module *attributes*. A spec-agnostic
IR should carry none of them.

Suggested: **`Preserved`** — it names the contract (§4.7's own "preserve them verbatim") rather
than any source concept, and states the guarantee: this survives untouched and the IR makes no
claim about its meaning. `Additional` is unavailable (`Model.Additional` holds the
`additionalProperties` state) and `Extra` invites "extra what?".

Cost: an `ir-design.md` change plus an IR schema change, so the JSON key changes and every golden
file regenerates. Mechanical, but its own piece of work.

### Do NOT split `Extensions` into typed buckets

Considered and rejected: splitting it into metadata / attributes / constraints. The defining
property of its contents is that they are *unmodeled* — if you knew enough to bucket `x-foo`, you
would know enough to model it. Classification would require the compiler to guess, making it a
heuristic, which invariant 6 then requires be injectable policy, marked `Inferred`, and
disableable — real machinery to sort data nobody can sort. The distinction already exists as
data: keys are namespaced (`openapi:if-then-else` vs `openapi:x-internal`), so a consumer
separates them without the IR growing fields.

### "Annotation" is used more broadly here than JSON Schema uses it

Decided and documented in `internal/harness/annotations.go` rather than renamed. JSON Schema Core
reserves *annotation* for keywords with no validation effect and calls `minimum`/`maxLength`
*assertions*; this suite groups both, because from the IR's perspective all of them attach to a
source position rather than define the type there. `constraints` stays the kind name — `ir.Constraints`
is an existing normative type and the vocabulary must match it.

### An emitter-side droppability split was considered and rejected for the IR

LLVM distinguishes *attributes* (affect codegen) from *metadata* (droppable without semantic
change). It does not transfer: morphic has many emitters with different products, so droppability
is a property of the (annotation, emitter) pair, not of the annotation. `docs` are droppable for
an SDK's behaviour and are the entire product of a docs emitter; `examples` are droppable for an
SDK and semantic for a mock server. Encoding it IR-side would bake one emitter's viewpoint into a
spec-agnostic IR, which invariants 1 and 10 exist to prevent. If a split is ever wanted IR-side,
use an objective axis (does it change bytes on the wire) rather than a consumer-dependent one.
