# Emitter design

The emitter is the second half of the Morphic pipeline: it turns an `ir.Document` (plus its own
options) into generated SDK source, docs, mock servers, and other artifacts. This directory is the
normative contract for that half, the way `docs/ir-design.md` is normative for the IR. It extends
the emitter sketch in `docs/architecture.md §2.3`–`§2.4`; where the two disagree, `architecture.md`
wins on stage boundaries and these documents win on the shapes inside an emitter.

Type sketches are written as Go because the implementation is Go. As in `ir-design.md`, field names
and struct shapes here are the contract; receiver methods and helpers are not. The IR is consumed,
never modified — an emitter imports `ir` and reads it.

## How the documents divide

A shared document says what every target must do; a target's document says how that target does it.
So a rule that holds for every target has one home, under the stage or the domain it belongs to, and
a document under `targets/` states what its language decides and restates no shared rule. A shared
document may name a language to illustrate a rule, or for a fact about it that explains the rule,
and it spells a runtime type as Go does; every choice a target makes is in that target's documents.

A trace of one construct divides where the pipeline does. Its IR and its plan are the same for every
target, so they sit in the shared document, and what refine and emit build from them sits in the
target's.

A new target adds a directory under `targets/` holding the documents every target has (`overview`,
`lowering`, `types`, `models`, `unions`, `naming`, `operations`, `returns` and `errors`) and any of
its own, and answers every construct of the [lowering table](refine/lowering-table.md). `INV<n>`
names an invariant of `CLAUDE.md`, and `T-<n>` a test of the [mandatory
tests](testing/mandatory-tests.md).

## The documents

**What an emitter is given and returns**

- [The emitter contract](contract/emitter.md): `Emitter`, its input and output, who computes the
  plan, and the rules the contract holds to.
- [The registry and capabilities](contract/registry.md): how targets are composed and what the
  engine knows of one without running it.
- [Target options](contract/options.md): strict, versioned, each default belonging to a version.
- [The type table](contract/type-table.md): which target type each IR leaf type becomes.
- [Runtime policy](contract/runtime-policy.md) and [shaping hints](contract/shaping-hints.md): the
  two inputs that are not the IR.

**The plan: language-neutral decisions, computed once**

- [The plan layer](plan/overview.md), and [plan fact or refine fact](plan/plan-or-refine.md), the
  test a candidate field must pass.
- [OpPlan](plan/operations.md), [responses](plan/responses.md), [streams](plan/streams.md) and
  [bindings](plan/bindings.md): what is decided per operation.
- [TypePlan](plan/types.md): what is decided per type.
- [plan.Policy](plan/policy.md): the plan's injectable heuristics.

**Refine: what every target's lowering does**

- [The refine layer](refine/pipeline.md), its [step list](refine/steps.md), the [target
  AST](refine/target-ast.md) and the [lowering table](refine/lowering-table.md).
- Types: [models](refine/models.md), [unions and discriminators](refine/unions.md),
  [enums](refine/enums.md), [containers, scalars and external types](refine/scalars.md),
  [decorators](refine/decorators.md).
- Values: [values](refine/values.md), [constraints](refine/constraints.md), [unmodeled
  entries](refine/unmodeled.md).
- Names and place: [naming](refine/naming.md), [reserved names](refine/reserved-names.md), [output
  units](refine/output-units.md).
- [Interface extraction and request shaping](refine/requests.md).

**Operations: how a call reaches its caller on every target**

- [Return shapes](operations/returns.md), [failures](operations/errors.md),
  [pagination](operations/pagination.md), [streaming](operations/streaming.md), [long-running and
  one-way operations](operations/long-running.md), [auth](operations/auth.md), [the client and its
  base URL](operations/client.md).

**Emit**

- [The emit layer](emit/printing.md), [structure and boilerplate](emit/templates.md) and [its
  rules](emit/rules.md).

**Each target**

- Go: [overview](targets/go/overview.md), [target AST and printer](targets/go/emit.md), [lowering
  table](targets/go/lowering.md), [types](targets/go/types.md), [models](targets/go/models.md),
  [unions](targets/go/unions.md), [naming](targets/go/naming.md),
  [operations](targets/go/operations.md), [return shapes](targets/go/returns.md),
  [failures](targets/go/errors.md).
- Kotlin/JVM and Kotlin Multiplatform: [overview](targets/kotlin/overview.md), [lowering
  table](targets/kotlin/lowering.md), [types](targets/kotlin/types.md),
  [models](targets/kotlin/models.md), [unions](targets/kotlin/unions.md),
  [naming](targets/kotlin/naming.md), [operations](targets/kotlin/operations.md), [return
  shapes](targets/kotlin/returns.md), [failures](targets/kotlin/errors.md), [Multiplatform common
  code](targets/kotlin/multiplatform.md), [Swift](targets/kotlin/swift.md),
  [scale](targets/kotlin/scale.md).

**Other artifacts, and how the work is held**

- [Docs and mock-server emitters](docs-and-mock.md), [write / integrate](write-integrate.md),
  [surface verification](surface-verification.md).
- [Package layout](packages.md), [mandatory tests](testing/mandatory-tests.md), the [behavior
  server](testing/behavior-server.md), the [milestone plan](milestones.md) and the [open
  questions](open-questions.md).

## Where the emitter sits

```
compiler (spec → IR)  →  passes (IR → IR)  →  emitter (IR → artifacts)
                                                 │
                    ┌────────────────────────────┴─────────────────────────────┐
                    │  plan   — language-NEUTRAL decisions, computed ONCE,       │
                    │           ID-keyed, shared across every target             │
                    │  refine — per-language lowering: IR+Plan → a typed,        │
                    │           per-language target AST (the acceptance test)    │
                    │  emit   — a pure printer over the target AST + the native  │
                    │           formatter; templates only for runtime boilerplate│
                    └────────────────────────────────────────────────────────────┘
```

The engine (Layer 3) runs the compiler, then the passes, then — **once per document** — the
derived-fact index and the shared plan, and finally each requested emitter. The passes designed are
`link · dedup · filter · version-slice · overlay · validate`, and only `validate` is built
(architecture.md §2.2). An emitter never sees a source spec, a compiler, another emitter, or the
engine (INV1). Its only structural input is an `ir.Document`; everything else it needs is either
derived from the IR in the plan/refine stages or supplied as a separate, non-IR input (runtime
policy, shaping hints, the target's own options).

## The architecture, and why this synthesis

The industry spans a spectrum, from logic-in-templates (OpenAPI Generator) through a mutable
shared CodeDOM with per-language refiners (Kiota) to a typed-AST-plus-pretty-printer (Stainless).
Morphic lands deliberately at the typed end, because it fails least on exactly the axes Morphic
cares most about — unions, composition, cross-language consistency (prior-art.md §2–§3).

OpenAPI Generator is the reference for the template pole, and its concrete anti-patterns motivate
every decision below. Its `CodegenModel`/`CodegenProperty`/`CodegenOperation` plan objects express
type kind as ~120 mutually-entangled booleans (`isString isArray isMap isEnum isNullable
isDiscriminator …`) with no exhaustiveness guarantee; they are *mutated in place* by each of ~204
per-language `*Codegen` subclasses; every casing is pre-rendered and stored on the shared object
(`nameInCamelCase`/`nameInPascalCase`/`nameInSnakeCase`); and the result is handed to logic-heavy
Mustache templates as an untyped `Map<String,Object>` that branch on the boolean bag. The costs are
exactly the ones Morphic's design negates: casing and reserved-word bugs re-litigated in every
subclass, no compiler check that kind-flags are exclusive, cross-language drift as each subclass
re-derives decisions, and unions/composition stringified (`oneOf<A,B>`, a single merged `parent`)
because there is no first-class node to carry them. Morphic answers each with a specific structural
choice — sealed-sum plan facts and a sealed-sum target AST (not a flag bag), a shared immutable plan
(not per-language mutation), neutral `Naming` cased only in refine (not stored casings), and a typed
AST printer (not an untyped template context).

The definitive design is a **strict three-stage pipeline** with these decisions:

1. **The plan layer is shared and computed once by the engine, keyed by IR stable IDs, and passed
   into every target.** This is the single most important structural choice: it is what makes the
   docs emitter, the mock emitter, and every language SDK agree *by construction* rather than
   re-derive decisions and drift — the OpenAPI Generator failure mode set out above. The
   plan is JSON-serializable and has its own language-independent golden-test corpus.

2. **Refine is an ordered pipeline of small, named, idempotent lowerings — but each one is a *pure
   function that constructs typed target-AST nodes*, not an in-place mutation of a shared tree.**
   This grafts the Kiota refiner *discipline* (an ordered, named step list is the IR acceptance
   test — every step is legal only because the un-lowered IR fact it consumes survived) onto an
   immutable-IR, build-don't-mutate model. There is **no separate neutral code model**: the only
   code models are the per-language target ASTs. That deliberately drops the "second sealed sum
   parallel to the IR" that a shared neutral CodeDOM would require, and with it Kiota's
   "minimize refiner changes or maintenance explodes" hazard and its HTTP-shaped-core ceiling.

3. **Emit is a pure printer over the typed target AST, canonicalised by the language's native
   formatter (`go/format`, `black`, `prettier`).** Structure — every declaration, field, method,
   type reference — is a compiler-checked node, so a malformed struct or a union variant missing
   its wire tag is a *Go type error inside Morphic*, not a `go build` failure in the customer's
   repo. String templates are permitted **only** for spec-invariant runtime boilerplate (the retry
   loop, the transport core, the pagination iterator runtime), parameterised by runtime policy —
   and even there they are guarded by a template lint and a mandatory format pass (the one idea
   worth keeping from the template-first stance).

4. **Every target-AST node carries its originating IR ID (`Origin`).** The AST therefore does
   triple duty: it is the code model, the source of the ID-keyed generation manifest and import
   graph, and the neutral surface model for compat verification. Write/integrate and surface
   verification fall out of a single node walk instead of being separate subsystems.

The trade this accepts, honestly: a new language costs a target-AST node set + a printer + a refine
pipeline before it emits a line — heavier than dropping in a template pack. The payoff (compiler-
enforced correctness, a free surface model, cross-language uniformity, identity-based merge) is real
but back-loaded onto the first targets, which is precisely milestone 3's charter.
[Determinism](emit/printing.md#determinism-and-the-velocity-answer) and the
[milestones](milestones.md) address the velocity cost directly, and the `Emit` seam is per-emitter,
so a future target that genuinely wants templates for structure can implement `Emit` differently
while reusing the same `plan` and refine step library.

## Emitter design rules (in addition to the ten CLAUDE.md invariants)

1. **The IR is read-only.** No stage of an emitter mutates the `ir.Document`. Lowering lands on the
   target AST, which is freshly built per (target × document).
2. **Decisions live in Go, never in templates.** Any branch that depends on an IR fact or a policy
   value is resolved in plan or refine and reaches emit as a decided node. A template that inspects
   the IR is a design bug.
3. **The un-lowered IR is the acceptance test.** If a refine step cannot make its decision from
   `IR + Plan + Options`, either the IR is under-designed (file a bug against `ir-design.md`) or the
   fact is runtime policy ([runtime policy](contract/runtime-policy.md)) — never a compiler change
   (INV2, INV9).
4. **Wire truth is a separate channel from identifiers.** Serialization always reads `WireName` /
   `WireID` / `WireNameByFormat`; it never reads a rendered identifier (INV4).
5. **Nothing heuristic is silent, nothing un-lowerable is silent.** Inferred outputs are marked
   `Inferred`; un-lowerable constructs emit a coded `Diagnostic`, never a fabricated `any` and never
   a panic (INV5, INV6).

## The first targets

The first milestone ships three targets from one plan: Go (`"go"`), Kotlin/JVM (`"kotlin-jvm"`) and
Kotlin Multiplatform (`"kotlin-multiplatform"`: JVM, Native/iOS and JS/Wasm from common code).

The Kotlin targets read the same plan and lower the same facts as Go.

## Deliberate exclusions

- **A separate neutral code model.** Rejected in favor of per-language target ASTs. A shared
  neutral CodeDOM would add a second sealed sum parallel to the IR, duplicate the plan's traversal,
  and re-import Kiota's HTTP-shaped-core ceiling and its "minimize refiner changes" hazard. The plan
  *is* the shared neutral layer; below it, code is per-language.
- **Logic in templates.** Templates are confined to spec-invariant runtime boilerplate, guarded by a
  lint and a format pass. All structure is typed AST. A template that inspects the IR is a bug.
- **Runtime SDK policy in the IR.** Retry/timeout/telemetry/error-taxonomy/user-agent/idempotency-
  injection/pagination-pacing/request-guards are a separate `policy.Policy` tree; declared IR facts
  win over its defaults ([runtime policy](contract/runtime-policy.md)).
- **Generator plan artifacts in the IR.** Request builders, executor/generator method pairs,
  per-visibility model variants, primary-response/primary-content selection — all computed views in
  the plan/refine layers, never stored.
- **Credential values / token acquisition.** Auth *schemes* are structural IR; secrets and refresh
  behavior are runtime configuration.
- **Name-keyed manifests or `"METHOD /path"` keys.** Everything cross-run correlates by IR stable ID.

## Invariant compliance

| INV | Where satisfied |
|---|---|
| 1 IR is the ABI | `EmitterInput.Doc` is the only structural input; `emitters/*` imports `ir`, `ir/irfacts` + shared contract, never `compiler`/`engine`/a sibling target ([the contract](contract/emitter.md), [package layout](packages.md)) |
| 2 Lossless, lowered late | all lowering is in `refine` steps building a fresh target AST; the IR is never mutated; unions/enums/discriminators/visibility/pagination/streaming/LRO arrive un-lowered and the step list is the acceptance test ([refine](refine/pipeline.md), [operations](operations/returns.md)) |
| 3 Stable IDs, names presentation | plan, AST `Origin`, manifest, surface, hints, and the collision table are all keyed by IR stable ID; renames are lookups; collisions never touch identity; a name with no words is named from its own characters alone, and placement reads no count, so neither moves when something else changes ([the plan](plan/overview.md), [naming](refine/naming.md), [output units](refine/output-units.md), [write / integrate](write-integrate.md)) |
| 4 Names neutral, emitter cases | `Ident` starts as the `Naming.Canonical` word sequence, the source spelling beside each word for its letter case only; `RenderCasing`/`EscapeReservedWords` are the only casing steps; struct tags come from `WireName`/`WireID`, a separate channel ([naming](refine/naming.md), [emit](emit/printing.md)) |
| 5 Pure, typed diagnostics | `Generate` is pure/reentrant/no-stderr/no-I/O; every un-lowerable construct → coded `ir.Diagnostic`; the engine decides fatality ([the contract](contract/emitter.md), [contract rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)) |
| 6 Heuristics are policy | `plan.Policy`, each target's versioned options (its type table and naming tables among them), `verify.Severity`, method-name derivation, and shaping hints are injectable and disable-able; the type table's width-picking rows, the acronyms, the character names and the leading words are defaults a configuration replaces; inferred outputs marked `Inferred` ([target options](contract/options.md), [type table](contract/type-table.md), [plan policy](plan/policy.md), [naming](refine/naming.md), [shaping hints](contract/shaping-hints.md), [surface verification](surface-verification.md)) |
| 7 Serializable/deterministic | `Plan` + manifest are JSON-serializable, and `Facts` is rebuilt from the document; maps walked sorted, slices source order; identical input ⇒ identical bytes ([who computes the plan](contract/emitter.md#who-computes-the-plan), [the plan](plan/overview.md), [determinism](emit/printing.md#determinism-and-the-velocity-answer), [mandatory tests](testing/mandatory-tests.md)) |
| 8 Optionality ≠ nullability | `LowerOptionality` renders all four states plus protobuf `Presence` as a third axis; collapses are diagnosed ([optionality](refine/models.md#optionality--nullability--presence-inv8)) |
| 9 Capability surface designed against every format | every `ir.TypeKind` and every operation facet (pagination/streaming/LRO/one-way/messaging) has a lowering path or diagnosed degrade ([step list](refine/steps.md), [lowering table](refine/lowering-table.md)); docs/mock/validation are registry entries over the same `Doc + Plan`; a new language forces no IR or plan change ([docs and mocks](docs-and-mock.md), [M5](milestones.md)) |
| 10 Runtime policy separate | `policy.Policy` tree in `EmitterInput.Policy`, never on `ir.Document`; it parameterises only boilerplate; declared IR facts win over its defaults ([runtime policy](contract/runtime-policy.md)) |
