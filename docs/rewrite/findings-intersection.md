# Should the IR gain an intersection combinator?

**Recommendation: no.** Do not add an `Intersection` node, a `Model` flag, or a `Mixins`
reinterpretation. Record the rejection in `ir-design.md §15`, split the `ir-spec-matrix.md`
`Intersection` row so it stops being silent about operand types, and fix the four sub-shapes
that hide behind the one syntactic trigger — three of the four are already reachable with
existing IR nodes, and one of those routes is already written down in this repo's own evidence
base (`reference-learnings.md §B11`) and was not followed.

The case against is not "it costs too much". It is that **the capability is one source
format's keyword-conjunction rule, not a cross-format concept** — verified below against all
eight planned formats — and that the shape it is supposed to model decomposes into pieces the
IR already holds.

---

## 1. Who needs it

The question is narrower than "does the format have intersection". Every format with `allOf`
has *model ∧ model*, which `§4.3 Base`/`Mixins` already covers. The capability under review is
**intersection where one operand is not a model** — specifically a union.

| Format | model ∧ model | **struct ∧ union** | How / why not |
|---|---|---|---|
| **OpenAPI 3.x** | ✅ `allOf` | **✅ native** | JSON Schema conjoins sibling keywords: `{type: object, properties: …, oneOf: […]}` is `Model ∧ Union` by definition. Also `{allOf: [$ref], oneOf: […]}`. |
| **Swagger 2.0** | ✅ `allOf` | **— impossible** | Swagger 2.0 has no `oneOf`, `anyOf`, or `not` at all (matrix rows *Tagged unions* / *Untagged unions* / *Negation*: all `—`). No union operand exists to intersect with. |
| **TypeSpec** | ⚠ `&` (spread-equivalent) | **— forbidden by the compiler** | `&` is model∧model only. The TypeSpec compiler emits `intersect-non-model`: *"Cannot intersect non-model types (including union types)."* — the parenthetical names this exact case. It also rejects arrays: `intersect-invalid-index` → *"Cannot intersect an array model."* The result of `A & B` is a merged anonymous model, which the IR already lowers to `Mixins` + spread provenance (`§14`). |
| **Smithy 2.0** | ⚠ mixins | **—** | No intersection operator. Mixins compose structures only; a structure and a union are different shapes and cannot be conjoined. Matrix: *Intersection* `—`. |
| **GraphQL** | — | **—** | `implements A & B` is a *separator* in a list of interfaces, not a type operator; interfaces are not unions, and a GraphQL union is a closed set of object types that cannot also be an object type. Matrix: *Intersection* `—`. |
| **AsyncAPI** | ✅ `allOf` | **✅ native — but the same JSON Schema** | AsyncAPI payload schemas are a JSON Schema dialect and `§14` states the lowering is literally *"payload JSON-Schema lowering shared with OpenAPI"*. This is the same capability arriving through the same code path, not independent corroboration. |
| **Protobuf** | — | **— (and already served)** | No type intersection. Protobuf's `message { string kind = 1; oneof body { A a = 2; B b = 3; } }` *is* a struct-with-a-union at the wire level, and the IR already models it without an intersection combinator: `§14` lowers it to a `Model` with a `Union`-typed property carrying `Property.Flatten` (`ir/property.go:98`). This is direct evidence that the shape has a home. |
| **Erlang/OTP** | — | **—** | The type language has `\|` (union) and no `&`. The one intersection-ish form is the **overloaded contract** (multi-clause `-spec`), which is an intersection of *function* types — and Dialyzer degrades it to the union of the domains rather than honouring it. Function types are excluded outright by `§15` (*"no target language marshals closures"*). Nothing at the data-shape level. |

**Score: 1 capability, 2 rows, 1 code path.** Six of eight formats cannot express it; the
seventh (TypeSpec) has a dedicated compiler diagnostic refusing it; the eighth (AsyncAPI)
inherits it from the same JSON Schema lowering as OpenAPI. Compare `Union` itself — six of
eight formats have it natively — or `Discriminator`, or `Mixins`. This is the weakest
cross-format case in the matrix short of a single-format capability.

### Where the matrix is silent

`ir-spec-matrix.md` **does** have an `Intersection` row (line 20) — the issue's claim that it
has none is wrong. What the row is silent about is **operand types**. `✅ allOf` for OpenAPI and
`⚠ & (model is)` for TypeSpec conflate two different capabilities: composing two structs, which
five formats have and the IR models, and conjoining a struct with a non-struct, which one format
has and the IR does not. The `⚠` on TypeSpec is doing all the work of that distinction and
nobody reading the row would recover it.

**Matrix change to make (whatever is decided):** split line 20 into two rows.

```
| Intersection (model ∧ model) | ✅ allOf | ✅ allOf | ✅ & / spread | ⚠ mixins | — | ✅ allOf | — | — |
| Intersection w/ a non-model operand | ✅ keyword conjunction | — (no unions) | — (intersect-non-model) | — | — | ✅ (JSON Schema) | — | — |
```

The second row is the honest statement of the capability, and it is the argument against
modelling it.

### Invariant 9 argues *against*, not for

Invariant 9 says the IR surface must be complete so that *later compilers never force a schema
change*. The table above establishes that no later compiler produces this shape. Adding a node
whose only producer is the first compiler is precisely the OpenAPI-shaped bias invariant 9
exists to prevent — the same failure mode `prior-art.md` records against Kiota's HTTP-shaped
core. Invariant 9 is not a mandate to model everything OpenAPI can write; it is a mandate to
model what the *union of formats* can write.

---

## 2. What it would cost

The sum has 11 members (`ir/typedef.go:6–30`). A 12th is not one struct.

**Enumerated edit sites: 27. Sites the Go compiler would catch if you missed one: 0.**

There is no `go:generate` anywhere in the repo, `.golangci.yml` does not enable `exhaustive`,
and **all four type-switches over the sealed interface have a silent `default:`**. The
"generated switch-completeness test" promised by `CLAUDE.md` and `ir-design.md §1` is
hand-written and only proves `NewTypeDef` and `Kind()` agree — it cannot see a missing case in
`pass/` or `compilers/`.

**`ir/` — the sum itself (8 sites)**

| File | What |
|---|---|
| `ir/typedef.go:6–30` | new `TypeKind` const |
| `ir/typedef.go:33` | doc comment hard-lists all 11 kind names |
| `ir/typedef.go:124–139` | `newTypeDefByKind` decoder registry entry |
| `ir/types.go` (~195–209 region) | the new struct |
| `ir/types.go:342–395` | `typeDef()` marker + `Kind()` accessor |
| `ir/types.go:397–403` | `Common()` doc comment says *"all eleven of them"* — stale-count landmine |
| `ir/json.go:39–102` | a 12th hand-written `MarshalJSON` (decode side is table-driven and free) |

**Dispatch outside `ir/` (6 sites, all silently-defaulting)**

| File | Function | Failure mode if missed |
|---|---|---|
| `pass/validate.go:65–91` | `walkTypeDefRefs` | dangling-ref checking skips the node's children; comment at `:87` already asks the next person to add a case |
| `pass/validate.go:174–183` | `checkDiscriminators` | discriminator validation skipped |
| `pass/validate.go:381–402` | `graphqlReachableTypes` | inherits the gap transitively through `walkTypeDefRefs` |
| `compilers/openapi/schema.go:488–500` | `resolvePrimKind` | allOf conflict detection misses |
| `compilers/openapi/schema.go:534–539` | `isStructuralType` | scalar/structural misclassification |
| `compilers/openapi/schema.go:460–469` | `isAnyType` | predicate, kind-sensitive |

**`irverify` / `irtest` / `harness` — the good news, stated fairly**

- `ir/irverify` costs **zero** production edits. It has no per-kind dispatch, no rule table and
  no exhaustiveness guard anywhere: `refs.go:127–176` `walkValues` is a bounded
  (`maxWalkDepth = 4096`), cycle-guarded reflection traversal, so referential integrity comes
  free. The catch: if the new node needs *semantic* checks (operand count, no self-reference,
  no nested intersection), you would be adding **the first per-kind switch the package has ever
  had** — a design decision, not a mechanical edit.
- `ir/irverify/nofloat_test.go:41–44` hand-lists all 11 concrete kinds (reflection cannot
  enumerate a sealed sum). Omitting the 12th leaves it **silently unchecked** for float fields.
- `ir/irtest` costs **zero** — 86 lines of golden read/write (`golden.go`), no per-kind builders.
- `internal/harness` costs **zero** structurally; its five oracles (panic, error diag,
  `irverify`, round-trip, determinism) are all kind-agnostic. But
  `internal/harness/annotations.go:41–59` is a doc comment that **normatively documents the
  current union-sibling lowering by name** (`lowerWithUnionSiblings` → `lower()` → `lowerModel`);
  changing the lowering invalidates it, and a 4th `SiteKind` means **+9 cells** in the
  annotation-retention grid, each needing a `retentionCase`
  (`TestAnnotationRetention_EveryCellCovered`, `compilers/openapi/annotations_test.go:1024`).

**Hand-maintained all-kind lists (8, of which 5 fail silently):** `ir/typedef.go:9–29` and
`:127–139`, `ir/types.go:342–395`, `ir/json.go:38–102` (**silent** — a kind without a
`MarshalJSON` emits no `kind` tag and only fails at decode), `ir/typedef_test.go:14–17`
(`allKinds`) and `:41–44` (a second literal), `ir/types_test.go:77–137` (zero-shape byte table),
`ir/irverify/nofloat_test.go:41–44`. Plus a 12th `Test<Kind>_PopulatedRoundTrip` and
`ir/json_test.go:22–34` `sampleDocument`, which drives round-trip *and* determinism.

**The coverage gate multiplies all of it.** `scripts/check-coverage.sh:30–47` enforces
**100.0% statement coverage per package and overall**. Every new statement — the new
`MarshalJSON`, every new switch case, every `irverify` rule — needs a covering test before CI
goes green.

**Golden churn: 0 of 43 — and that is itself the finding.** 42 conformance goldens + 1
(`testdata/golden/openapi/petstore.golden.json`). Only 4 contain a `union` node. **Zero contain
`degraded_lowering`, `openapi:oneOf`, or `openapi:anyOf`.** All 10 corpus specs that mention
`oneOf`/`anyOf` were checked against `hasUnionSiblings`: **none** has a structural sibling
(`discriminator` is not one). The fuzz seed list (`compilers/openapi/fuzz_test.go:212–228`) has
no co-declared seed either. The shape is exercised *only* by inline YAML inside Go tests.

**What would actually break: ~9 test functions / ~11 schemas**, all in
`compilers/openapi/schema_test.go` — `:80` (`TestLower_OneOfWithStructuralSiblingsPreserved`,
which asserts `.(*ir.Model)` and would **invert**), `:117`, `:416` (4 schemas: object,
`patternProperties`, `const`, `type: string`), `:639`, `:741`, `:1190`, `:1205`, `:1240`,
`:1292`. Plus ≥1 new conformance row (spec + golden + `assertX` + table entry), which
auto-joins three more sweeps (`TestHarness_InRepoCorpus`, `TestDanglingRefs_Corpus`, the fuzz
seed corpus).

**The recurring cost is downstream, not here.** `emitter-design.md:1085–1108` is a *normative*
IR→Go lowering table with one row per kind and the rule *"Every row must have a Go emit path or
a diagnosed degrade; a missing row is an IR bug (INV9)"*, and `:943` states switch-completeness
tests live *"beside every refiner (over `ir.TypeKind`'s eleven kinds)"*. So a 12th kind is a new
normative row and a new refiner case **per target language, forever** — and for every target but
TypeScript that row's content is "diagnosed degrade". The degradation does not disappear; it
moves from one compiler to N emitters.

---

## 3. What it undoes — and why `degraded_lowering` survives either way

If the node lands, `§4.8`'s first bullet is deleted and the single write site of
`ReasonDegradedLowering` (`compilers/openapi/schema.go:168`) goes with it. Today that is the
**only** producer in the repo.

**The value must stay regardless.** `§4.8` lists five further constructs that will populate it,
all preserved-with-a-reason by construction:

| §4.8 construct | Preserved key | Lands with |
|---|---|---|
| TypeSpec `StringTemplate` | `typespec:string-template` | Milestone 4 (TypeSpec) |
| Erlang bit-sized binaries `<<_:M,_:_*N>>` | `erlang:bit-size` | Milestone 5 (Erlang/OTP) |
| Erlang `fun()` | `erlang:fun-spec` | Milestone 5 |
| Erlang map assoc lists | `erlang:map-assocs` | Milestone 5 |
| Erlang `-opaque` | `erlang:opaque` | Milestone 5 |

(The sixth entry, TypeSpec `never`, preserves nothing by design — it deletes the member.)

`ir/preserved_test.go:38` **already pins** `"erlang:opaque": {Reason: ir.ReasonDegradedLowering}`
as its round-trip fixture, so the reason is exercised today by a case that has nothing to do
with OpenAPI. Removing the value would break that test and contradict a normative table
(`§12`).

**One test would need rewriting either way.**
`compilers/openapi/schema_test.go:639–668` `TestPreserve_AllReasonsReachable` asserts all four
reasons are reachable from the compiler, and `:662–666` uses the union-sibling schema as its
**only** `degraded_lowering` witness. If the node lands, that test loses its witness and — with
no other compiler-side producer until Milestone 4 — cannot be repaired within
`compilers/openapi` at all. That is a small but real signal: the reason has no OpenAPI-side
member other than this one.

So: **step 3 is not a driver in either direction.** A zero-write-site enum value would be a
*staged* state, not a vestigial one — the same state `UsageFlags` is already in (declared in
`ir/typedef.go:41–55`, computed by nothing). Two things worth doing: name the §4.8 constructs
that will populate it in the `ReasonDegradedLowering` doc comment (`ir/preserved.go:23–27`) so a
future reader does not delete it, and either scope `TestPreserve_AllReasonsReachable` to the
reasons this compiler can actually produce or move it to `ir/`.

---

## 4. What targets can express

| Target | `M ∧ (A \| B)` | Notes |
|---|---|---|
| TypeScript | **yes** | `{kind: string} & (A \| B)`; distributes to `(M&A) \| (M&B)`. The only one. |
| Go | no | no sum types, no intersection; `emitter-design.md` already picks sealed-interface for `Union` |
| Java | no | intersection types exist only in generic bounds (`<T extends A & B>`) and casts — not declarable as a field type |
| Python | no | confirmed 2026: no `Intersection` in `typing`; the last proposal round died without consensus |
| C# / Rust | no | neither has type intersection |
| Kotlin | no | `A & Any` (definitely-non-nullable) only, not general |
| Swift | no | `P & Q` is protocol composition; an enum (Swift's union analogue) cannot participate |

`§4.7`'s argument — *"no target language's type system represents them"* — is **1/7 short of
applying**, and `§4.8` already concedes the point in writing (*"target languages are not the
constraint here; TypeScript writes `{a: string} & (X | Y)` directly"*). That concession is
correct and should not be overstated in either direction: the honest form is *one of seven
plausible targets can render it, and even that one would probably prefer the distributed form
for discriminated-union ergonomics.*

Combined with §2's finding that each target's refiner needs its own case and its own normative
row, a first-class node buys a faithful rendering for one target and a mandatory diagnosed
degrade for the rest. That is the strongest argument against, and it holds.

---

## 5. The cheaper shapes — and the one the repo already chose

The premise that this is *one* capability is wrong. `hasUnionSiblings`
(`compilers/openapi/schema.go:122–139`) fires on properties, `required`, `allOf`, `const`,
`enum`, `additionalProperties`, `patternProperties`, or any declared `type`. Those trigger four
semantically different situations, and treating them as one is what makes an intersection node
look necessary.

**(a) Structural branches — `{type: object, properties: {kind}, oneOf: [$A, $B]}`.**
Distribution is *exact*: `M ∧ (A|B) ≡ (M∧A) | (M∧B)`, and it holds for `anyOf` too under the
IR's own semantics (`Exclusive=false` means one-or-more, and matching both distributed variants
is exactly `M∧A∧B`). The result uses only existing nodes: `Union{Variants: [Model{Mixins:[M,A]},
Model{Mixins:[M,B]}]}`. Each variant's `TypeID` comes from its own `oneOf` branch pointer, so
`§3.1`'s pointer-derived identity holds. Nothing is merged — the composition is *classified*
onto each variant, exactly as `§4.3` requires.

**This is already the repo's researched recommendation and it was not followed.**
`reference-learnings.md §B11` studies this exact co-occurrence against four reference
implementations and prescribes, verbatim: *"Distribute the allOf composition across the Union
variants … emit a `Union` whose each `Variant.Type` references a `Model` carrying the allOf
classification (`Base`/`Mixins`/inline-`Properties` per §4.3) composed with that variant. The
`Union` is the value; the composition rides on every variant. This is the most faithful to the
`∧`-of-`∨` semantics."* It also records that **none of the four references — datamodel-code-
generator, openapi-generator, ogen, openapi-python-client — has an intersection node**; they
distribute, classify, or hard-stop. The branch under audit implements the *inverse* of B11: it
keeps the structural body as the value and degrades the union. B11's bug (dropping `allOf`) is
fixed; the loss moved to the other operand.

**(b) The discriminated-subtype idiom — `{allOf: [$Base], oneOf: [$Sub1, $Sub2]}`.** B11's
recommendation #2: classify `allOf` → `Base` and attach `Discriminator` + mapping. One
polymorphic model, no synthetic union, existing nodes.

**(c) Constraint-only branches — `oneOf: [{required: [a]}, {required: [b]}]` beside
`type: object, properties: {a, b}`.** This is the single most common instance of the shape in
real specs, and its branches carry **no type structure at all** — they are a validation
constraint over the parent's own property set. It is the direct sibling of `dependentRequired` /
`dependentSchemas`, which `§4.7` already classifies as validation-only. TypeScript's `&` does
not help here either. **These belong under `ReasonValidationOnly`, not `ReasonDegradedLowering`
— and today they are misfiled.** A validation emitter selecting on
`Reason == ir.ReasonValidationOnly` (the mechanism `emitter-design.md:1141` specifies) will miss
the most common case of the shape it exists to catch. This is a real defect in the current
branch, independent of the intersection question.

**(d) Irreducible — contradictory or un-distributable bodies.** `{type: string, oneOf: [<objects>]}`
is unsatisfiable; branch counts past a cap explode. Keep the current preservation with
`ReasonDegradedLowering` and the `info` diagnostic. That is what the reason is for.

### Options considered and rejected

| Option | Verdict |
|---|---|
| New sealed-sum member `Intersection{Operands []TypeRef}` | **Rejected.** 27 edit sites, 0 compile-time enforcement, a new normative row + refiner case per target forever, for a capability 1 format family has and 1 target can render. |
| Reuse `Model.Mixins` to point at a `Union` | **Rejected, actively harmful.** `§4.3` defines `Mixins` as allOf-shaped model composition and `FlattenedProperties()` walks it expecting models. A `Union` target silently makes a well-typed traversal ill-typed at runtime, in the one helper every consumer uses. |
| New `Model` field (`Refines []TypeRef` / a flag + union pointer) | **Rejected, but it is the right fallback if the decision is reversed.** Cheap — no kind, no marker, no JSON tag, no switch cases; costs one traversal case in `walkTypeDefRefs`, one `irverify` rule, one doc section. But it only covers `Model` bodies, while `hasUnionSiblings` fires on scalars, enums, literals and tuples too, so it is a partial answer to a problem that has no consumer yet (no emitter exists — Milestone 3 is unstarted). Speculative generality. |
| A `TypeCommon.Refines` field (universal) | **Rejected.** Puts a combinator on `Primitive` and `Any`, and becomes a dumping ground. `§4.3` deliberately uses named fields with distinct meanings (`Base` / `Implements` / `Mixins`) rather than one generic edge. |
| Keep preservation, fix the reason | **Adopt** — see (c). |
| Distribute in a `pass/` | **Adopt for (a)/(b)** — with the caveat below. |

**Caveat on the pass route, stated honestly.** A `pass/` cannot distribute what it cannot see:
`Preserved` holds raw source JSON, and a pass that re-parsed OpenAPI's `oneOf` would violate
*"the IR is the ABI"*. So the distribution has to happen in the compiler at lowering time. That
is defensible — the repo already does exactly this for the two shapes B11 names, and the
"lower late" precedent it might seem to violate (*"a oneOf/anyOf whose variants are all string
consts normalizes to a closed `Enum` in a `pass/` — not in the compiler"*) is about a
*lossy* collapse, whereas distribution is value-set-exact and keeps every branch as its own
node with its own pointer-derived ID. The authoring shape is the only thing lost, and a
`Preserved` entry plus provenance records it. It needs an explicit branch cap
(`CLAUDE.md`: *"bounded everything"*) with fallback to (d).

---

## 6. What to do instead — concrete

1. **Record the rejection in `ir-design.md §15`**, in the register the section already uses
   (compare the Smithy-waiters entry): *intersection with a non-model operand — one source
   format family expresses it (JSON Schema keyword conjunction, reaching the IR through OpenAPI
   and AsyncAPI payloads); TypeSpec's `&` rejects union operands (`intersect-non-model`) and no
   other format has the construct; one target language can render it. Promoted to a typed node
   only if a second independent format lands or a TypeScript emitter demonstrates the need.*
   That is the same bar `§15` already applies to Smithy waiters.
2. **Split the `ir-spec-matrix.md` `Intersection` row** as in §1. The matrix is the authority
   the corpus is generated from; a row that conflates model∧model with model∧union is why this
   question had to be asked twice.
3. **Keep `ReasonDegradedLowering`** and add the §4.8 constructs that will populate it to its
   doc comment (`ir/preserved.go:23–27`), so the zero-write-site state after any refactor reads
   as staged rather than dead.
4. **Reclassify constraint-only branches to `ReasonValidationOnly`** (§5(c)). Cheapest and
   highest-value item on this list: today the most common real instance of the shape is invisible
   to the validation emitter the reason taxonomy was built for.
5. **Implement B11's distribution for the structural sub-shapes** (§5(a)/(b)), bounded, with the
   authoring shape kept in `Preserved`. This closes the actual complaint in the issue — the SDK
   is under-constrained — using nodes that already exist, and it makes the `null`-branch case
   work again: once the union is the value, `§3.3`'s null-variant → `Nullable` rule applies
   normally.
6. **Add corpus coverage.** `testdata/` has zero cases for any of this. At minimum:
   `{allOf:[$Base], oneOf:[$A,$B]}` with and without a discriminator (B11 names this as the exact
   regression), `{type: object, properties, oneOf:[{required},{required}]}`, and an irreducible
   case that must stay preserved.

Items 3, 4 and 6 are worth doing whether or not anyone ever reverses the decision on the node.
