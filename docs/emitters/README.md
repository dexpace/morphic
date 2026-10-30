# Morphic Emitter — Emitter Design

The emitter is the second half of the Morphic pipeline: it turns an `ir.Document` (plus its own
options) into generated SDK source, docs, mock servers, and other artifacts. This document is the
normative contract for that half, the way `docs/ir-design.md` is normative for the IR. It extends
the emitter sketch in `docs/architecture.md §2.3`–`§2.4`; where the two disagree, `architecture.md`
wins on stage boundaries and this document wins on the shapes inside an emitter.

Type sketches are written as Go because the implementation is Go. As in `ir-design.md`, field names
and struct shapes here are the contract; receiver methods and helpers are not. The IR is consumed,
never modified — an emitter imports `ir` and reads it.

---

## 1. Purpose, pipeline relationship, and the chosen architecture

### 1.1 Where the emitter sits

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

### 1.2 The architecture, and why this synthesis

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
enforced correctness, a free surface model, cross-language uniformity, identity-based merge) is
real but back-loaded onto the first targets, which is precisely milestone 3's charter. §5.5 and §14
address the velocity cost directly, and the `Emit` seam is per-emitter, so a future target that
genuinely wants templates for structure can implement `Emit` differently while reusing the same
`plan` and refine step library.

### 1.3 Emitter design rules (in addition to the ten CLAUDE.md invariants)

1. **The IR is read-only.** No stage of an emitter mutates the `ir.Document`. Lowering lands on the
   target AST, which is freshly built per (target × document).
2. **Decisions live in Go, never in templates.** Any branch that depends on an IR fact or a policy
   value is resolved in plan or refine and reaches emit as a decided node. A template that inspects
   the IR is a design bug.
3. **The un-lowered IR is the acceptance test.** If a refine step cannot make its decision from
   `IR + Plan + Options`, either the IR is under-designed (file a bug against `ir-design.md`) or the
   fact is runtime policy (§6) — never a compiler change (INV2, INV9).
4. **Wire truth is a separate channel from identifiers.** Serialization always reads `WireName` /
   `WireID` / `WireNameByFormat`; it never reads a rendered identifier (INV4).
5. **Nothing heuristic is silent, nothing un-lowerable is silent.** Inferred outputs are marked
   `Inferred`; un-lowerable constructs emit a coded `Diagnostic`, never a fabricated `any` and never
   a panic (INV5, INV6).

---

## 15. Deliberate exclusions

- **A separate neutral code model.** Rejected in favor of per-language target ASTs. A shared
  neutral CodeDOM would add a second sealed sum parallel to the IR, duplicate the plan's traversal,
  and re-import Kiota's HTTP-shaped-core ceiling and its "minimize refiner changes" hazard. The plan
  *is* the shared neutral layer; below it, code is per-language.
- **Logic in templates.** Templates are confined to spec-invariant runtime boilerplate, guarded by a
  lint and a format pass. All structure is typed AST. A template that inspects the IR is a bug.
- **Runtime SDK policy in the IR.** Retry/timeout/telemetry/error-taxonomy/user-agent/idempotency-
  injection/pagination-pacing/request-guards are a separate `policy.Policy` tree; declared IR facts
  win over its defaults (§6).
- **Generator plan artifacts in the IR.** Request builders, executor/generator method pairs,
  per-visibility model variants, primary-response/primary-content selection — all computed views in
  the plan/refine layers, never stored.
- **Credential values / token acquisition.** Auth *schemes* are structural IR; secrets and refresh
  behavior are runtime configuration.
- **Name-keyed manifests or `"METHOD /path"` keys.** Everything cross-run correlates by IR stable ID.

---

## 17. Invariant compliance

| INV | Where satisfied |
|---|---|
| 1 IR is the ABI | `EmitterInput.Doc` is the only structural input; `emitters/*` imports `ir`, `ir/irfacts` + shared contract, never `compiler`/`engine`/a sibling target (§2, §7) |
| 2 Lossless, lowered late | all lowering is in `refine` steps building a fresh target AST; the IR is never mutated; unions/enums/discriminators/visibility/pagination/streaming/LRO arrive un-lowered and the step list is the acceptance test (§4, §8) |
| 3 Stable IDs, names presentation | plan, AST `Origin`, manifest, surface, hints, and the collision table are all keyed by IR stable ID; renames are lookups; collisions never touch identity; a name with no words is named from its own characters alone, and placement reads no count, so neither moves when something else changes (§3, §4.12, §4.17, §11) |
| 4 Names neutral, emitter cases | `Ident` starts as the `Naming.Canonical` word sequence, the source spelling beside each word for its letter case only; `RenderCasing`/`EscapeReservedWords` are the only casing steps; struct tags come from `WireName`/`WireID`, a separate channel (§4.12, §5) |
| 5 Pure, typed diagnostics | `Generate` is pure/reentrant/no-stderr/no-I/O; every un-lowerable construct → coded `ir.Diagnostic`; the engine decides fatality (§2, §2.4) |
| 6 Heuristics are policy | `plan.Policy`, each target's versioned options (its type table and naming tables among them), `verify.Severity`, method-name derivation, and shaping hints are injectable and disable-able; the type table's width-picking rows, the acronyms, the character names and the leading words are defaults a configuration replaces; inferred outputs marked `Inferred` (§2.5, §2.6, §3.5, §4.12, §6.4, §12) |
| 7 Serializable/deterministic | `Plan` + manifest are JSON-serializable, and `Facts` is rebuilt from the document; maps walked sorted, slices source order; identical input ⇒ identical bytes (§2.2, §3, §5.5, §13) |
| 8 Optionality ≠ nullability | `LowerOptionality` renders all four states plus protobuf `Presence` as a third axis; collapses are diagnosed (§4.7) |
| 9 Capability surface designed against every format | every `ir.TypeKind` and every operation facet (pagination/streaming/LRO/one-way/messaging) has a lowering path or diagnosed degrade (§4.2, §9); docs/mock/validation are registry entries over the same `Doc + Plan`; a new language forces no IR or plan change (§10, §14 M5) |
| 10 Runtime policy separate | `policy.Policy` tree in `EmitterInput.Policy`, never on `ir.Document`; it parameterises only boilerplate; declared IR facts win over its defaults (§6) |
