# Morphic Backend — Emitter Design

The backend is the second half of the Morphic pipeline: it turns an `ir.Document` (plus its own
options) into generated SDK source, docs, mock servers, and other artifacts. This document is the
normative contract for that half, the way `docs/ir-design.md` is normative for the IR. It extends
the backend sketch in `docs/architecture.md §2.3`–`§2.4`; where the two disagree, `architecture.md`
wins on stage boundaries and this document wins on the shapes inside a backend.

Type sketches are written as Go because the implementation is Go. As in `ir-design.md`, field names
and struct shapes here are the contract; receiver methods and helpers are not. The IR is consumed,
never modified — a backend imports `ir` and reads it.

---

## 1. Purpose, pipeline relationship, and the chosen architecture

### 1.1 Where the backend sits

```
frontend (spec → IR)  →  passes (IR → IR)  →  backend (IR → artifacts)
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

The engine (Layer 3) runs the frontend, then the passes (`validate · link · dedup · filter ·
version-slice · overlay`), then — **once per document** — the shared plan, and finally each
requested backend. A backend never sees a source spec, a frontend, another backend, or the engine
(INV1). Its only structural input is an `ir.Document`; everything else it needs is either derived
from the IR in the plan/refine stages or supplied as a separate, non-IR input (runtime policy,
naming policy, shaping hints).

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
   docs backend, the mock backend, and every language SDK agree *by construction* rather than
   re-derive decisions and drift (the OpenAPI-Generator failure mode, prior-art.md §3). The
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
real but back-loaded onto the first target, which is precisely milestone 3's charter. §5.5 and §14
address the velocity cost directly, and the `Emit` seam is per-backend, so a future target that
genuinely wants templates for structure can implement `Emit` differently while reusing the same
`plan` and refine step library.

### 1.3 Backend design rules (in addition to the ten CLAUDE.md invariants)

1. **The IR is read-only.** No stage of a backend mutates the `ir.Document`. Lowering lands on the
   target AST, which is freshly built per (target × document).
2. **Decisions live in Go, never in templates.** Any branch that depends on an IR fact or a policy
   value is resolved in plan or refine and reaches emit as a decided node. A template that inspects
   the IR is a design bug.
3. **The un-lowered IR is the acceptance test.** If a refine step cannot make its decision from
   `IR + Plan + Options`, either the IR is under-designed (file a bug against `ir-design.md`) or the
   fact is runtime policy (§6) — never a frontend change (INV2, INV9).
4. **Wire truth is a separate channel from identifiers.** Serialization always reads `WireName` /
   `WireID` / `WireNameByFormat`; it never reads a rendered identifier (INV4).
5. **Nothing heuristic is silent, nothing un-lowerable is silent.** Inferred outputs are marked
   `Inferred`; un-lowerable constructs emit a coded `Diagnostic`, never a fabricated `any` and never
   a panic (INV5, INV6).

---

## 2. The backend contract

Package `backend` is the Layer-2 contract. It imports **only** `ir` (Layer 0) plus its own shared
sub-packages (`plan`, `policy`, `naming`, `manifest`, `verify`) — never `frontend`, never `engine`,
never a sibling target package (INV1; architecture.md §3). Enforced by the import-graph test (§7).

```go
package backend

// TargetKey identifies a backend in the registry: "go", "python", "docs", "mock/go", …
type TargetKey string

// Backend is one target — a language SDK, a docs site, a mock server, a validator.
// Pure and reentrant: no package-level mutable state, no clock, no randomness, no stderr,
// no filesystem I/O. Same input ⇒ byte-identical output (INV5, INV7).
type Backend interface {
    Target() TargetKey
    Capabilities() Capabilities          // lets the engine reason without running (§2.3)
    Generate(BackendInput) BackendOutput // the whole backend, as one pure function
}

type BackendInput struct {
    Doc     *ir.Document          // the ABI — the ONLY structural input (INV1)
    Plan    *plan.Plan            // language-neutral decisions, computed ONCE by the engine (§3)
    Policy  policy.Policy         // runtime/SDK behavior — a SEPARATE tree, never in the IR (INV10; §6)
    Naming  naming.Policy         // per-target casing/acronym/reserved-word engine (INV4; §4.12)
    Hints   map[EntityID]ShapeHint// SDK-shaping hints keyed by IR stable ID (§6.4)
    Scope   *Scope                // nil = whole document; else "emit only these" — full Doc still
                                  // consumed so global decisions stay byte-identical
    Prior   *PriorState           // prior manifest + prior surface for write/integrate; nil first run
    Options TargetOptions         // module path, root package, output layout — free-form per target
}

type BackendOutput struct {
    Artifacts   []Artifact          // files (+ integration directives) — no I/O performed here
    Diagnostics []ir.Diagnostic     // typed, coded, provenance-bearing; engine decides fatality
    Manifest    *manifest.Manifest  // entity→symbol map keyed by IR stable IDs (§11)
    Surface     *verify.Surface     // neutral API-surface projection of the generated output (§12)
}

// EntityID is the string form of any IR stable ID (TypeID, OpID, PropID, ServiceID, …).
// Every IR ID is a `type X string`, so one keyspace addresses them all.
type EntityID = string

// Artifact is oagen's GeneratedFile shape, minus every SDK-policy-shaped field.
type Artifact struct {
    Path         string          // deterministic, forward-slash, relative to output root
    Content      []byte
    Kind         ArtifactKind    // source | test | doc | manifest | dependency-manifest | scaffold
    Header       HeaderPlacement // where the generated-by provenance header goes (top/after-shebang/none)
    SkipIfExists bool            // scaffolding written once (config, .gitignore) — never overwritten
    Overwrite    bool            // regenerate but preserve ignore-regions (default is additive)
    Integrate    *IntegrateHint  // path-prefix stripping / tree-shake root for live-repo merge (§11)
    Origins      []EntityID      // IR IDs whose emission produced this file (drives ID-keyed prune)
}
```

`error` is not part of the `Generate` signature: a backend has no failure mode that is not either a
`Diagnostic` (something about the input it cannot lower) or a programmer bug (a `TypeKind` with no
dispatch — caught by the switch-completeness test at build time, §13, not at runtime). This keeps
the contract pure and total.

### 2.1 The registry

Keyed by `TargetKey`, populated by each target's `init()` — mirroring the frontend registry
(architecture.md §2.1):

```go
var registry = map[TargetKey]func() Backend{}

func Register(t TargetKey, ctor func() Backend) { registry[t] = ctor }
func New(t TargetKey) (Backend, bool)           { c, ok := registry[t]; if !ok { return nil, false }; return c(), true }
func Targets() []TargetKey                        // sorted — determinism (INV7)
```

The engine is the only caller of `New`, the only thing that runs `Generate`, and the only thing
that touches the filesystem, prunes, and renders diagnostics. Backends are I/O-free.

### 2.2 Who computes the plan

`plan.Build(doc, planPolicy)` is a pure function (§3). For a multi-target run the **engine computes
it once** and passes the same `*plan.Plan` into every `Generate` call. This is what makes "computed
once, shared, cannot drift" literally true rather than a convention, and it lets the plan be cached
and golden-tested on its own corpus, independent of any language. Plan policy is document-level, not
per-target — the plan is language-neutral by definition, so there is nothing per-language to vary in
it. A single-target invocation may compute the plan inline; the function is the same.

### 2.3 Capabilities

`Capabilities()` lets the engine reason about a backend without running it — which IR `TypeKind`s
it lowers, whether it supports write/integrate, whether it produces a surface for compat. It is
advisory metadata, not a second decision channel; it never changes what `Generate` emits.

```go
type Capabilities struct {
    LowersKinds     []ir.TypeKind // the kinds this backend has an emit path for
    WriteIntegrate  bool          // supports additive merge into a live repo (§11)
    SurfaceVerify   bool          // produces a verify.Surface (§12)
    Protocols       []string      // "http", "grpc", "message", "graphql", "otp"
}
```

### 2.4 Contract rules the type system and tests enforce

- **Purity / determinism.** No I/O, clock, randomness, or stderr inside `Generate`. Same
  `(Doc, Plan, Policy, Naming, Hints, Scope, Options)` ⇒ byte-identical `Artifacts`. Maps are
  walked in sorted-key order, slices in source order (§13 test T-6).
- **Diagnostics are the only input-failure channel.** A construct the backend cannot lower emits
  `ir.Diagnostic{Severity, Code:"backend/<target>/…", Message, Provenance}` — never a silent
  fallback, never a panic (§13 test T-7). Codes live in a stable `backend/<target>/…` namespace.
- **Full document under scope.** Scope gates *emission*, not the plan or naming; global decisions
  are computed over the whole document so a scoped run is byte-identical to the corresponding slice
  of a full run. Scope is never a filtered `Document`.
- **Snapshots only.** The engine runs `version-slice` first; the backend receives a single version
  snapshot and never interprets `Availability`.

---

## 3. The plan layer

`backend/plan` (Layer 2, imports `ir` only). `Build` is pure, deterministic, and JSON-serializable
(INV7) so it can be cached, shared out-of-process, and golden-tested independently of any language.

```go
package plan

func Build(doc *ir.Document, pp Policy) (*Plan, []ir.Diagnostic)

type Plan struct {
    Ops     map[ir.OpID]OpPlan          // one entry per operation
    Types   map[ir.TypeID]TypePlan      // one entry per registry type
    Shapes  map[ShapeKey]ModelShape     // (TypeID × Lifecycle) → projected wire shape
    Groups  map[GroupKey]GroupPlan      // sub-client / resource nesting (from OperationGroup)
    Version string                      // hash(doc content-hash + PlanPolicy); feeds the manifest
}
```

Every key is an IR stable ID — never `"METHOD /path"`, never a rendered name (INV3; the oagen
fragility, prior-art §1). A spec rename never orphans a plan entry: it is "same ID, new symbol",
a lookup. Every inferred field carries `Inferred` provenance, propagated into diagnostics so it is
auditable and disable-able (INV6).

The plan **classifies; it does not pick target types and it never cases anything.** It records
facts ("this union is `WireTagged` with a discriminator and three variants with IDs X/Y/Z; closed")
so a refiner can choose a strategy from the same fact base. It contains no casing, no reserved-word
handling, no target types, and no policy.

### 3.1 OpPlan — the fixed per-operation decision set

Adopted from oagen's production `OperationPlan` (prior-art §1), the decision set the emitters
demonstrably needed:

```go
type OpPlan struct {
    HasBody         bool             // Operation.Request != nil
    Idempotency     IdempotencyKind  // from Operation.Idempotency — a declared fact
    ParamShape      ParamShape       // positional | options-bag — Inferred, injectable (§3.5)
    Return          ReturnShape      // model | void | list-of-models(elementwise) | stream
    ReturnType      *ir.TypeRef      // unwrapped primary return type
    PrimaryResponse *ir.Response     // SDK-ergonomic pick; IR keeps ALL responses
    PrimaryContent  *ir.Content      // json>form>multipart>binary — a SHARED plan helper
    Errors          []PlannedError   // status→error, ranges collapsed (404/4XX/catch-all)
    Page            *PagePlan        // nil = not paginated
    Stream          *StreamPlan      // nil = not streaming (ir-design.md §7.3)
    LRO             *LROPlan         // nil = not long-running
    OneWay          bool             // fire-and-forget: no response ever (ir-design.md §7.2)
    Auth            []AuthOption     // OR-of-ANDs, priority-ordered (§8.6)
    Bindings        BindingView      // per-protocol param/request/response view from OpBindings
    Inferred        map[string]ir.Provenance // which fields above were heuristic (INV6)
}

type PagePlan struct {                // item-type unwrap is a PLAN decision
    Strategy   ir.PageStrategy
    ItemType   *ir.TypeRef            // element type items resolve to, unwrapped from the envelope
    ItemPath   *ir.PropPath           // where items live — a PATH (may be nested / in headers)
    CursorIn   *ir.ParamPath
    CursorOut  *ir.PropPath
    Inferred   bool                   // heuristic pagination (may be disabled by policy)
}

type PlannedError struct {
    Conditions ir.ResponseConditions  // status ranges, collapsed
    Type       *ir.TypeRef            // an Error-flagged model
    Fault      string                 // "" | "client" | "server" — declared
    Retryable  *bool                  // declared (nil = unknown) → wins over policy (§6.3)
    Throttling *bool                  // declared throttling class (nil = unknown)
}

// The remaining operation-facet plans. Each is nil when the facet is absent, so a refiner's
// presence check is a nil check, and each carries only classified facts — never a target type.
type StreamPlan struct {
    Direction  ir.StreamingMode       // client | server | bidi (from the operation core)
    EventUnion ir.TypeID              // the WireTagged event union (Variant.Event carries per-event
                                      // content-type + terminal bit); resolved by ID
    Initial    *ir.TypeRef            // initial-request/response message preceding the stream; nil = none
    RequiresLength bool               // streamed content needs a known finite length up front
}

type LROPlan struct {                 // maps ir.LongRunning (ir-design.md §7.3)
    FinalStateVia string              // "operation-location" | "status-monitor" | "original-uri" | …
    PollOp     *ir.OpID               // declared polling operation; nil = poll the start op's monitor
    FinalOp    *ir.OpID               // declared final-result operation; nil = result is inline
    PollType   *ir.TypeRef            // status-monitor type
    FinalType  *ir.TypeRef            // what PollUntilDone resolves to
    ResultPath *ir.PropPath           // where the final result lives in the terminal response
}

type AuthOption struct {              // one option in the OR-of-ANDs; slice order is PRIORITY order
    Schemes []ir.SchemeUse            // ALL must be satisfied together; empty = "no auth is acceptable"
}

// IdempotencyKind / ParamShape / ReturnShape are small closed enums:
type IdempotencyKind string          // "unknown" | "safe" | "idempotent" | "idempotency_token"
type ParamShape      string          // "positional" | "options_bag"
type ReturnShape     string          // "model" | "void" | "list_elementwise" | "stream"
```

`PrimaryResponse` / `PrimaryContent` are the *one legitimate home* for the collapse oagen performs
destructively in its frontend and Morphic forbids there (INV2): the IR keeps every response and
content type whole; "pick a primary for the ergonomic method" happens here, lowered late. `Content
negotiation` default (`json > form > multipart > binary`) is a shared, overridable plan helper
(resolves `ir-design.md` open Q3).

### 3.2 TypePlan — per-type projections computed once

```go
type TypePlan struct {
    Usage      ir.UsageFlags        // Input|Output|Error|Multipart|Json|Xml — drives serializers
    Flattened  []ir.PropID          // FlattenedProperties() over Base+Implements+Mixins+own — COMPUTED
    Relations  Relations            // {Base, Implements, Mixins} kept DISTINCT for refine
    Union      *UnionClass          // tag-mode classification (untagged/discriminated/key-tagged)
    Enum       *EnumFacts           // open/closed, value type, flags, fallback member
    Discrim    *DiscrimFacts        // discriminator resolved to variant IDs, not names
    ScalarBase ir.TypeID            // nearest representable base after chain walk + constraint merge
    NeedShapes []ir.Lifecycle       // which visibility projections this type actually needs
    Alias      ir.TypeID            // dedup alias target — follow to the single emitted type (architecture.md §2.2)
    Inferred   map[string]ir.Provenance
}

type Relations struct {             // kept DISTINCT so a refiner maps each relation its own way
    Base       *ir.TypeRef          // single inheritance (→ Go embed / class `extends`)
    Implements []ir.TypeRef         // N-ary conformance to Abstract models (→ Go interfaces)
    Mixins     []ir.TypeRef         // composition without subtyping (→ inline embed)
}

type UnionClass struct {            // the tag-mode classification, resolved to IDs (§4.4)
    Mode      UnionTagMode          // untagged | discriminated | key_tagged
    Exclusive bool                  // oneOf/tagged exactly-one vs anyOf one-or-more
    Variants  []ir.TypeID           // variant type IDs, source order
    Discrim   *DiscrimFacts         // present iff Mode == discriminated
}
type UnionTagMode string            // "untagged" | "discriminated" | "key_tagged"

type EnumFacts struct {
    Closed    bool                  // false = open/extensible (unknown values must survive)
    ValueType ir.PrimKind
    Flags     bool                  // bitfield semantics
    Fallback  string                // FallbackMember wire name; "" = none
}

type DiscrimFacts struct {          // Mapping/Default resolved to TypeIDs, never names (INV3)
    Locator  DiscrimLocator         // property | property_name | index
    WireKey  string                 // wire name of the tag property ("" when Locator == index)
    Index    *int                   // positional tag element (Erlang tagged tuples); nil otherwise
    Mapping  map[string]ir.TypeID   // wire tag value → variant ID
    Default  ir.TypeID              // variant for absent/unrecognized tag; "" = none
    Envelope string                 // "" = inline tag | "object" = {kind,value} wrapper
    EnvelopeValueName string        // wire name of the envelope value property (Envelope=="object")
}
type DiscrimLocator string          // "property" | "property_name" | "index"
```

Note what `TypePlan` does *not* do: it does not choose Go's sealed interface vs a native union, does
not case anything, does not resolve reserved words. `Flattened` is the *computed* flattening; the
plan carries it **alongside** the still-distinct `Relations` so a class-based refiner can map
`Base→extends` / `Implements→interfaces` / `Mixins→inline` while a Go-struct refiner embeds them all.
The plan does not decide which — that is per-language (§4).

### 3.3 ShapePlan — visibility projection

One logical model produces N wire shapes across the lifecycle. `ModelShape(model, lifecycle)` is a
**computed traversal in the plan layer**; the IR stores only the single logical model plus visibility
facts and never the projected variants (INV2; `ir-design.md §5.2`). The plan emits one `ModelShape`
per `(TypeID, Lifecycle)` the document actually exercises, so a refiner knows exactly which shapes
to build.

```go
type ShapeKey struct { Type ir.TypeID; Lifecycle ir.Lifecycle }
type ModelShape struct {
    Props   []ir.PropID  // properties visible in this lifecycle
    Patch   bool         // PATCH shape: properties are implicitly optional unless the binding disables it
    Inferred map[string]ir.Provenance
}
```

`readOnly → Only:[read]`; `writeOnly → Only:[create,update]`; `Visibility.None` → excluded from
every projection (distinct from the zero value); PATCH flips properties optional unless
`HTTPBinding.PatchImplicitOptionality == false`; operation-level `ParameterVisibility` /
`ReturnTypeVisibility` override which filter applies. Unknown visibility classes are opaque
filters evaluated per class (`ir-design.md §5.2`).

### 3.4 BindingView — the per-protocol request/response projection

`Parameter`s carry no protocol location (`ir-design.md §7.2`). The plan derives, from `OpBindings`,
a neutral per-protocol view a refiner turns into a request builder: which params are path/query/
header/cookie/body, their style/explode/prefix, RPC message folding, message channel + reply, the
GraphQL entry point, the OTP call/cast shape. One operation may carry several bindings (gRPC
transcoding `additional_bindings`, HTTP+RPC); all are represented, primary first. This resolves
`ir-design.md` open Q6 on the backend side: status conditions and `StatusCodeProp` are read through
the HTTP `BindingView`, so RPC/OTP single-response ops with empty conditions are handled uniformly.

```go
type BindingView struct {
    Primary  ProtoBinding    // the binding a single-protocol SDK uses
    All      []ProtoBinding  // every binding, primary first (transcoding, dual HTTP+RPC)
}
type ProtoBinding struct {
    Protocol string          // "http" | "grpc" | "message" | "graphql" | "otp"
    Params   []ParamBind     // neutralized param placement (one per bound param, per location)
    BodyPath *ir.PropPath    // sub-field of the payload the wire body maps to; nil = whole payload
    // Protocol-specific handles kept by ID/reference, not restructured:
    HTTP     *ir.HTTPBinding // method/URITemplate/status map, when Protocol == http
    RPC      *ir.RPCBinding
    Message  *MsgView        // channel + direction + reply, when Protocol == message
    Raw      ir.Extensions   // GraphQL entry point / OTP call-cast tag / deployment bindings, verbatim
}
type ParamBind struct {
    Param    string          // Operation.Params name
    Where    string          // path | query | header | cookie | body | body_property | host
    WireName string          // never a rendered identifier (INV4)
    Style    string; Explode *bool; Prefix string   // form/label/matrix/deepObject serialization
}
type MsgView struct { Channel ir.ChannelID; Direction ir.MsgDirection; Reply *ir.Reply; Messages []ir.MessageID }
```

The Go-first walkthrough (§8) exercises `Protocol == "http"`; the RPC/message/OTP arms exist in the
type so the second-protocol backend (open Q4) does not force a plan schema change (INV9). Channels
and messages (`ir-design.md §8.3`) reach a backend through `MsgView`; a messaging target consumes
them the same way an HTTP target consumes `HTTP`.

`GroupPlan` mirrors the `OperationGroup` tree so a refiner can build sub-clients / fluent navigation:

```go
type GroupKey string                 // stable, derived from OperationGroup identity
type GroupPlan struct {
    Name     ir.Naming               // neutral — cased in refine
    Parent   GroupKey                // "" = top-level; nesting → sub-clients
    Ops      []ir.OpID
    Resource *ir.ResourceInfo        // Smithy resource lifecycle, when declared
}
```

### 3.5 plan.Policy — the plan's injectable heuristics

Everything the plan *infers* rather than reads is a knob here; every inferred output is marked
`Inferred` (INV6):

```go
type Policy struct {
    ParamShapeThreshold func(*ir.Operation) ParamShape // default: pathParams>1 || (pathParams>0 && (body||query)) → bag
    ContentRanking      []string                        // default: json > form > multipart > binary
    PrimaryResponse     func([]ir.Response) *ir.Response // default: lowest 2xx
    EnvelopeDetect      EnvelopePolicy                   // off unless explicitly enabled
}
```

A test asserts that disabling a heuristic removes the corresponding `Inferred` plan field — the
guard against a heuristic silently defaulting on and quietly making the plan non-neutral.

---

## 4. The refine layer

`backend/<target>/refine`. This is where the "lossless, lowered late" bet is cashed (INV2) and
the **only** place names get cased (INV4). It reads the immutable IR and the shared Plan and
**constructs typed nodes of a per-language target AST**. It never mutates the IR and never mutates a
shared cross-language tree.

### 4.1 The refiner contract — an ordered pipeline of pure lowerings

Following Kiota's concrete contract — an ordered list of small, named, idempotent steps, each valid
only because the un-lowered fact it needs is still present (prior-art.md §2) — but each step
is a **pure function `IR+Plan → nodes`**, not an in-place edit:

```go
package refine

type Refiner interface {
    Target() backend.TargetKey
    Steps()  []Step             // ORDERED — composition order is part of the contract
}

type Step interface {
    Name() string               // stable, for diagnostics + golden step-trace tests
    Apply(*Ctx) []ir.Diagnostic // appends typed nodes to the target AST under construction; idempotent
}

type Ctx struct {
    Doc     *ir.Document        // read-only source of un-lowered facts
    Plan    *plan.Plan          // read-only shared decisions
    Policy  policy.Policy        // read-only SDK runtime policy (§6)
    Naming  naming.Policy        // per-target casing/acronym/reserved-word engine (INV4)
    Hints   map[backend.EntityID]backend.ShapeHint
    Out     TargetAST            // the per-language AST being built (e.g. *goast.Module)
    Syms    *SymbolTable         // ID-keyed; collision resolution never touches IR identity
}

func Run(r Refiner, c *Ctx) []ir.Diagnostic  // runs Steps in order

// TargetAST is the per-language AST root marker; each target supplies a concrete type
// (e.g. *goast.Module). It is an interface here only so the shared refiner contract is language-
// neutral; every real step type-asserts to its own AST and appends typed nodes.
type TargetAST interface{ targetAST() }
```

**This ordered, named step list IS the IR acceptance test.** Each step below is only implementable
because a specific un-lowered IR fact reached it; a missing IR capability shows up as an
unimplementable step, not a silent degrade. Ordering is asserted by a golden *step-trace* test:
casing precedes reserved-word escaping; both run after every structural lowering; import
computation runs last. Because each step is idempotent and reads only `IR + Plan + AST`, a step-trace
snapshot pins the whole lowering deterministically.

### 4.2 The canonical step list

| Step | Reads (un-lowered IR fact) | Lowers to (Go example) | IR ref |
|---|---|---|---|
| `ResolveScalarChains` | `Scalar.Base` chain, `Constraints`, `Encoding` | nearest representable base + merged constraints; nil-base → newtype (opaque-scalar strategy) | §4.2 |
| `LowerContainers` | `List/MapT/Tuple` nodes (with IDs) | `[]T` / `map[K]V` / positional struct; non-string map key → custom-key map or diagnostic | §4.6 |
| `LowerLiterals` | `Literal{Value}` | typed constant / single-value newtype (incl. `symbol`) | §4.6 |
| `LowerModels` | `Properties`, `Relations`, `AdditionalProps`, `Additional`, `Positional` | struct (+embeds), catch-all map, `DisallowUnknownFields` on `closed` | §4.3 |
| `ProjectVisibilityShapes` | `plan.ModelShape` per lifecycle | `User` / `UserCreate` / `UserPatch` structs | §5.2 |
| `LowerOptionality` | `Property.Required` ⊥ `TypeRef.Nullable` ⊥ `Property.Presence` | the four+ states → `T` / `*T` / `Opt[T]`/`Nil[T]`/`OptNil[T]` / omitempty + presence bit | §5.1; INV8 |
| `LowerConstraints` | `Constraints` on properties/scalars/lists | field-level `validate` tags + a refine-built `Validate()` method structure | §5.3 |
| `LowerUnions` | `Union{Variants, Exclusive, WireTagged, Discriminator}` | sealed interface + marker method + concrete variants | §4.4; INV2 |
| `LowerDiscriminator` | `Discriminator{Property/PropertyName/Index, Mapping→TypeID, Default, Envelope}` | tag-dispatched `UnmarshalJSON` + typed factories | §4.3–§4.4 |
| `LowerOpenEnums` | `Enum.Closed`, `ValueType`, `Members`, `Flags`, `FallbackMember` | closed → typed consts; open → `string`+consts+`Unknown(raw)`; flags → bitset | §4.5 |
| `ExtractInterfaces` | `Model.Abstract`, `Implements`, union membership | Go interfaces / client interfaces for testability | §4.3 |
| `MapExternalTypes` | `External{Identity, Package, MinVersion}` | imported library type + dependency-manifest entry; unmappable → diagnostic | §4.6 |
| `MaterializeValues` | `Value{Kind, Num BigVal, Ref, Ctor}` | literals; `Num` → bignum/decimal, never float64; `Ctor` → call; `symbol` → string explicitly | §6 |
| `BuildRequestShaping` | `plan.BindingView`, `plan.ParamShape` | request builders, per-protocol param binding, options-bag structs | §8 |
| `LowerPagination` | `plan.PagePlan` (item unwrap, cursor paths) | iterator/auto-pager type + `Next()/Err()`; pacing left to policy | §7.3 |
| `LowerStreaming` | `plan.StreamPlan` (direction, event union) | `EventStream.Recv()`/`Send()` over the event union; terminal ends | §7.3 |
| `LowerLRO` | `plan.LROPlan` (final-state-via, poll/final ops, result path) | poller type + `Poll()/PollUntilDone()` returning the final type | §7.3 |
| `LowerErrors` | `plan.PlannedError`, `UsageFlags.Error`, `Fault` | error type tree (`APIError` → client/server → concrete) | §7.2 |
| `RenderCasing` | neutral `Ident` (= `Naming.Canonical`) + `Naming` acronyms | cased identifiers — the ONLY place names get cased | §3.2; INV4 |
| `EscapeReservedWords` | cased `Ident` + reserved set | keyword-safe identifiers; NEVER touches `WireName`/`WireID` | §3.2; INV4 |
| `ResolveCollisions` | `Ctx.Syms` (ID-keyed symbol table) | deterministic disambiguation; never mutates IR identity | §3.1; INV3 |
| `ComputeImports` | referenced symbols in the module | per-file import sets | — |

Every one of the eleven `ir.TypeKind`s has a dispatch here: `primitive` and `any` map directly
in `ResolveScalarChains`/type-reference lowering, `literal` in `LowerLiterals`, the rest in the
named steps above. The switch-completeness test (§13) proves the coverage at build time. The
concrete lowering strategies for the load-bearing axes follow.

### 4.3 The target AST substrate

The target AST (`backend/golang/goast` for the first target) is a small, purpose-built,
sealed-sum syntax layer — the exact Go the SDK needs, at declaration/signature granularity — modelled
with the same discipline the IR uses (INV Go-conventions): an unexported marker method, one
struct per node kind, a `Kind()` accessor, a generated switch-completeness test over the node enum.

```go
package goast

type Node interface { node(); Kind() NodeKind; Origin() backend.EntityID }

type Module struct { Packages []*Package }
type StructType struct {
    Common
    Fields  []*Field
    Embeds  []Expr           // base/mixins after the flatten decision
}
type Field struct {
    Common
    Name Ident               // cased identifier
    Type Expr                // a typed expression node — cannot be a raw string
    Tag  StructTag           // built from WIRE name/ID/format, NEVER the identifier
    Opt  Optionality
}
type InterfaceType struct { Common; Methods []MethodSig; Marker string } // union base / abstract model
type Method struct { Common; Name Ident; Params []*Param; Returns Expr; Kind MethodKind }
type ErrorType struct { Common; Fault string; Retryable *bool; Throttling *bool }

type Common struct {
    origin backend.EntityID   // ← the IR ID this node came from (manifest / surface / prune; INV3)
    prov   ir.Provenance      // passthrough for diagnostics + Inferred marking
    ident  Ident              // NEUTRAL until RenderCasing runs — holds Naming.Canonical, not a cased string
}

type Expr interface{ isExpr() }
type Named   struct { Pkg, Name string }   // time.Time, uuid.UUID
type Pointer struct { Elem Expr }
type Slice   struct { Elem Expr }
type MapExpr struct { Key, Value Expr }
type Iface   struct{}                       // any
```

Two properties make this the right substrate: **illegal code is unrepresentable** (a field's type is
an `Expr`, not a string; a union variant that forgot its wire tag is a missing struct-literal field
the Go compiler flags inside Morphic — the antidote to the Mustache `oneOf`/`allOf` failure class),
and **every node carries its `Origin`**, so the manifest, import graph, and surface model are one
walk (§11, §12).

### 4.4 Unions (the hardest axis)

`LowerUnions` reads the plan's tag-mode classification and the target's capability. The tag-mode
matrix (each a distinct serializer) is normative (`ir-design.md §4.4`):

- `WireTagged=false` → untagged; variant inferred by validation (JSON oneOf/anyOf).
  `Exclusive=true` = exactly-one; `false` = one-or-more (anyOf, open).
- `WireTagged=true` + `Discriminator` → tag is a property inside the variant payload.
- `WireTagged=true` + nil `Discriminator` → key-tagged: a single-key object keyed by the variant's
  wire name (Smithy unions, proto3-JSON oneof, GraphQL `@oneOf`).

Go has no sum type, so the Go refiner picks the **sealed-interface** strategy — the same closed-sum
idiom the IR itself mandates: `type Payment interface { isPayment() }`, one concrete struct per
variant with an unexported marker method, a generated switch-completeness test over the variant set.
Variant identity (`Variant.Name`) survives for named accessors / constructors; `WireName`/`WireID`
survive for (de)serialization. **Unions never degrade to optional-field merges** — flattening a
*body* union into method arguments is an opt-in `ShapeHint` (§6.4), never a silent loss (the oagen
counterexample, prior-art §1). A future TS/Python target lowers the *same* facts to a native
`A | B | C`; the decision differs, the fact base is identical.

Within the untagged case, choosing *how* a variant is recognized on decode is refine's job, and
ogen's `schema_gen_sum.go` is the battle-tested enumeration to lift: try, in order, explicit
discriminator → implicit discriminator (variant const/name) → JSON-type discrimination (variants
distinguishable by JSON kind) → unique-field discrimination → value-based. The decisive divergence:
ogen computes this *while building the Go model*, so a union it cannot discriminate aborts the whole
generation. Because Morphic keeps `Union` lossless in the IR and only a backend picks a strategy, a
Go refiner that finds no strategy emits a coded diagnostic and falls back to `json.RawMessage` per
variant — a degrade, never a whole-document failure (§13 test T-7). That graceful-degradation margin
is exactly what the ABI seam buys and the fused-pipeline generator cannot have.

### 4.5 Open enums

`plan.EnumFacts` tells the refiner extensibility must survive. Go cannot express open enums, so
`Closed=false` lowers to a named string type + typed consts + a passthrough that preserves unknown
values and honors `FallbackMember` on decode; `Closed=true` lowers to typed consts with strict
decode; `Flags=true` → bitset; numeric enums render `Value` through the Values channel (§4.10).
Duplicate member values (`allow_alias`) must not be rejected; slice order is the canonical name for
serialization. The open/closed bit, value type, member values, and flags bit all survived from the
IR to this point — Kiota's string-only-closed-enum mistake is designed out because the fact reached
refine.

### 4.6 Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` map and emits the concrete
create/read/update/patch structs the target needs — Speakeasy's "public type vs wire (de)serialize
shape" split made explicit. `None` properties are excluded from every projection; PATCH shapes make
properties optional unless the binding disables it. A model used as both input and output
(`UsageFlags` = Input|Output, `InputOnly` distinct identity for GraphQL inputs) may emit two shapes.

### 4.7 Optionality × Nullability × Presence (INV8)

`LowerOptionality` renders the four states — `Property.Required` (wire presence) ⊥ `TypeRef.Nullable`
(admits null) — plus protobuf `Property.Presence` (implicit/explicit/required) as a *third* axis that
is not nullability (protobuf has no null). The reference Go lowering is ogen's generic-box scheme
(`gen/ir/generics.go`), which independently arrives at exactly these four states: required-nonnull →
bare `T`; optional → `Opt[T]` (a `{value T; set bool}` box, so presence is distinct from the zero
value); nullable → `Nil[T]`; both → `OptNil[T]`. A mature real-world generator needing all four
first-class boxes is the strongest external confirmation of INV8, and — decisively — ogen makes this
decision in its *emitter* (`boxType`), keeping `Required` and `Nullable` orthogonal in the schema
model right up to that point. That is Morphic's split precisely: orthogonal bits in the IR, boxed in
refine. Two Go-specific optimizations to inherit: reference types (slice/map/pointer) can encode one
of the four states in their own `nil` (ogen's `NilSemantic: invalid | optional | null`), saving a
wrapper; `explicit` protobuf presence → pointer/hazzer, `implicit` → `,omitempty` with
zero-not-serialized. `ClientOptional` relaxes the client-side type even when `Required=true`;
`DefaultAdded` may be suppressed for back-compat. Where Go's zero value would force a *deliberate*
collapse of two states, a `Diagnostic` is emitted — never a silent conflation (§13 test T-3).

### 4.8 Discriminators

`LowerDiscriminator` reads a `Discriminator` whose `Mapping` points at **TypeIDs, not names**, so a
presentation rename never breaks the wiring. It emits a tag-dispatched `UnmarshalJSON` that peeks
the discriminator **wire** key (never the Go field name), typed factory functions, and a
`MarshalJSON` that writes the tag. `Property` vs `PropertyName` vs `Index` (positional), `Envelope`
vs inline, and the `Default` variant on an absent/unrecognized tag all survive un-lowered; the
choice of factory vs native vs wrapper is the refiner's.

### 4.9 Containers, scalars, external types

- **Containers**: `List`/`MapT`/`Tuple` are real IR nodes with IDs, so `List<Map<K,List<T>>>`
  composes cleanly; constrained lists keep their bounds; `List.Encoding` (packed/expanded) reaches
  the serializer; non-string map keys are handled or diagnosed; tuples become fixed-arity structs.
- **Scalars**: `ResolveScalarChains` walks `Base` transitively, accumulating
  `Constraints`/`Encoding`; nil-base scalars (GraphQL custom) go to the opaque-scalar strategy
  (a Go newtype), never a fabricated chain.
- **External** (R4 §2.8): `External{Identity, Package, MinVersion}` becomes an imported `Named` node
  at `MinVersion` plus a dependency-manifest entry — imported, not generated. Unmappable identities
  (`erlang:pid`, `erlang:fun`) get the passthrough/opaque strategy or a `warning` diagnostic, never
  a silent `any`.

### 4.10 Values

`MaterializeValues` renders defaults, consts, enum values, literals, and examples as language
literals. `Num` (`BigVal` decimal string) parses straight to the target numeric/decimal type —
**never through float64** (§13 test T-5). `symbol` renders as a string only *explicitly* and
diagnosed (Erlang atoms: `ok ≠ <<"ok">>`); `Ref` renders as an `Enum.Member` reference, not an
inlined copy; `Ctor` renders as a constructor *call* (`plainDate.fromISO(...)`), never folded to a
frozen literal.

### 4.11 Constraints and validation

`LowerConstraints` reads `Constraints` on properties, scalars, and lists (§5.3 of `ir-design.md`)
and does two things: it stamps field-level validation metadata (a `validate:"…"` struct tag or an
equivalent registration) and it *builds the structure* of a per-type `Validate()` method — which
checks apply to which fields — as typed AST. Validation is therefore neither pure boilerplate (the
checks depend on the spec) nor free-form template logic: refine decides the set of checks, and the
individual check primitives (`checkMinLength`, `checkMultipleOf` on `big.Rat`) are runtime helpers.
This is ogen's `oas_validators_gen.go` split, relocated to honor the structure-vs-boilerplate line
(§5.2). Numeric bounds flow through `BigVal`/`big.Rat`, **never float64** — ogen itself gets storage
right yet leaks `Float64()` in its `minNum`/`maxNum` merge path, so any constraint intersection a
refiner performs (e.g. when flattening `allOf` bounds) must stay on decimal. A `Pattern` a target's
regex engine cannot honor is dropped with a `warning` diagnostic, never silently mistranslated.

### 4.12 Naming, casing, reserved words, collisions (INV4)

Identifier rendering is owned **exclusively** by refine, via an injectable `naming.Policy`:

```go
package naming

type Policy interface {
    Case(words []string, role Role) string  // Role: TypeName | Field | Method | Const | Param | Package
    Acronyms() AcronymTable
    Reserved() ReservedSet
}
```

The `Ident` on every AST node starts as the neutral `Naming.Canonical` word sequence — a *word
sequence, not an identifier*. `RenderCasing` is the only step that cases it; because the IR stores
the pre-split neutral words, there is no lossy runtime re-splitting of a cased string (oagen's
`splitWords` counterexample). `EscapeReservedWords` runs after casing and **never touches
`WireName`/`WireID`** — struct tags are built from the wire channel, so wire correctness never
depends on the rendered identifier (§13 test T-4). Collisions are resolved deterministically in
`Ctx.Syms`, which is keyed by IR ID: adding one endpoint never renames an existing method (the oagen
collision-cascade counterexample; INV3). Anonymous hoisted types get a backend-chosen name from
`Naming.Hint`. Per-service presentation renames (`Service.Renames`) change how a shape is presented
in a service without changing its `TypeID` or its own `Naming`.

### 4.13 Interface extraction & request shaping

`ExtractInterfaces` derives per-language interfaces from `Abstract` / `Implements` / union
membership, plus client interfaces for testability from the `OperationGroup` structure.
`BuildRequestShaping` turns the plan's `BindingView` into request builders and the `ParamShape` into
positional params or an options-bag struct — RequestBuilder-style constructs are generator-side, out
of the IR (prior-art §2). `OperationGroup` nesting becomes sub-clients / fluent navigation.

---

## 5. The emit layer

`backend/<target>/emit` plus per-target printer. Emit is a **pure fold** `print(TargetAST) → []byte`,
then the language's native formatter canonicalises the result.

### 5.1 Typed AST → text → native formatter (the rationale)

The Stainless model, adapted to Go: the printer emits syntactically complete text and hands it to
`go/format.Source` (Python `black`, TS `prettier`), which is the canonical authority on formatting.
Indentation, spacing, and import ordering stop being the emitter's problem — an entire class of
whitespace/import-order bugs is eliminated by delegation.

**Why a domain AST → text → `go/format`, not `go/ast` directly.** `go/ast` is awkward to *synthesize*
(it wants a `token.FileSet` and real positions and cannot carry generator intent like a field's
`Origin` IR ID or a `{t:TypeID}` doc token). The domain AST is smaller, holds provenance and wire
metadata on its nodes, and prints to text that `go/format` then makes canonical. `go/ast` is used in
exactly one place — re-parsing *existing* files for additive merge (§11) — where its parser is the
right tool. The two Go trees are kept aligned by ID-keying: "a symbol I generated" ↔ "a symbol I
parsed" is a lookup.

### 5.2 The structure-vs-boilerplate line (the honest boundary)

| Content | Representation | Why |
|---|---|---|
| Models, enums, unions, discriminators, operations, clients, error types, serializers | **Typed AST nodes** | Spec-derived *structure*; correctness must be compiler-checked; must be uniform across languages |
| Runtime core: HTTP transport, retry loop, backoff+jitter, pagination iterator runtime, auth injectors, logging hooks | **Templates**, one small file each, parameterised by `Policy` | Policy-driven *boilerplate* whose shape does not vary with the spec |
| Package docs, README, per-op usage examples | **Docs backend** (`docast`, §10) | Prose with holes |

A retry loop is the same code for every API; only numbers and header names vary. A typed AST there
buys nothing and costs readability. But a template with an `if` that inspects the IR is a design bug
— that `if` belongs in refine, producing a different node. The boilerplate templates therefore
contain **no policy branching and no structural branching**: all decisions were made in plan/refine;
the template only interpolates scalar policy values (retry count, header name). So templates cannot
diverge across languages the way Mustache *structure* templates do.

### 5.3 Two guards on the boilerplate templates

Grafted from the template-first stance, applied only to the narrow boilerplate surface:

- **A template lint** (a unit test per pack) parses every `.tmpl` and rejects kind-discriminating
  conditionals, arithmetic, and comparisons beyond boolean presence — only `range`, field access,
  whitelisted pure funcs, and `{{template}}` are allowed. This turns "a template *could* smuggle a
  decision" from a discipline problem into a failing test.
- **A mandatory format pass** (`Formatter.Format(path, src)`), run on *all* emitted files. Any
  syntactic slip a template produces is caught here as a located, hard `Diagnostic` (file + the
  formatter's message), never shipped. Because typed-AST structure is already syntactically valid by
  construction, in practice the format pass only ever catches boilerplate-template mistakes.

### 5.4 Serialization behind a runtime abstraction

The generated *typed veneer* references a `Transport` / `Serializer` interface (Kiota's
`IParseNode`/`ISerializationWriter` seam); the concrete transport is a runtime-policy-configured
library. This is the seam that keeps runtime policy (INV10) out of the typed structural model:
the veneer of models/unions/methods is policy-free, and `Policy` only ever parameterises the
boilerplate that backs the interface.

### 5.5 Determinism, and the velocity answer

Emit walks the AST in source order and any map in sorted-key order (INV7); identical `IR + options`
⇒ byte-identical output (§13 test T-6), which is what makes golden snapshots and the ID-keyed
manifest meaningful. On velocity: a new *language* is expensive (node set + printer + pipeline), but
a new *non-SDK backend* (docs, mock) is cheap because it reuses the shared plan and most of the
refine step library (§10); and the per-language cost buys compiler-enforced correctness, a free
surface model, and identity-based merge that a template pack cannot. The `Emit` seam remaining
per-backend means a target may opt into templates-for-structure if it ever needs to — the design
does not mandate typed-AST emit, it defaults to it.

---

## 6. Runtime/SDK policy — a separate input

`backend/policy` (INV10; architecture.md §2.4). The behavioral configuration of generated
SDKs is a backend input alongside the IR, **never a field on `ir.Document`** — the divergence from
oagen, which attaches `SdkBehavior` to its spec root. Keeping the trees separate lets one IR drive
SDKs, docs, and mock servers without dragging SDK opinions along.

### 6.1 The canonical vocabulary

From oagen's production `SdkBehavior`, the best real-world enumeration (architecture.md §2.4):

```go
package policy

type Policy struct {
    Retry       Retry        // retryable status codes, max attempts, Backoff{initial, multiplier, max, jitter}
    Timeout     Timeout      // default seconds + env override
    Errors      ErrorTax     // status→logical exception-kind, client/server catch-all, doc-URL template
    Telemetry   Telemetry    // request-ID header, client-telemetry header
    Logging     Logging      // enabled + a CLOSED lifecycle-event list
    UserAgent   UserAgent    // identifier template, app-info enrichment, ai-agent env vars
    Idempotency Idempotency  // header name, auto-generate rules
    Pagination  Pacing       // auto-page delay
    Guards      RequestGuard // option keys that must NOT appear as params (misuse detection)
}

func Defaults() Policy
func Merge(base Policy, over DeepPartial) Policy // scalars override; ARRAYS REPLACE (the oagen rule)
```

### 6.2 Delivery

Canonical `Defaults()` + deep-partial per-backend/per-project overrides. Arrays replace
entirely (not concat) — an explicit, documented rule. Policy is delivered in `BackendInput.Policy`;
refiners read it via `Ctx.Policy`, and it shapes only the generated *runtime binding* (the retry
loop, transport config, user-agent string, telemetry headers, pagination pacing) behind the
`Transport`/`Serializer` abstraction. It never enters the typed structural model (§5.4).

### 6.3 Precedence — declared IR facts win (hard rule)

`ErrorCase.Retryable/Throttling/Fault` and `Operation.Idempotency` come from the spec; policy fills
in only where the spec is silent — never the reverse. The refiner enforces this by reading the IR
fact first and consulting policy only on `nil`. A `policy-precedence` test asserts a spec
`Retryable:false` beats a policy that lists that status as retryable (§13 test T-9). When policy is
overridden by a spec fact, an `Inferred`-marked diagnostic is emitted so nothing is silent.

### 6.4 SDK-shaping hints — a third, ID-keyed input

oagen's `OperationHint` / rename / remount / split-union-body / url-builder machinery is *SDK
ergonomics, not API semantics* — the same category as runtime policy. It lives in
`BackendInput.Hints`, keyed by **IR stable ID** (never `"METHOD /path"`), so a path rename never
drops a hint (INV3):

```go
type ShapeHint struct {
    Rename         string    // presentation rename for the entity's symbol; "" = none
    Remount        GroupKey  // move an operation under a different sub-client; "" = keep
    SplitUnionBody bool      // flatten a request-body union into typed wrapper methods — OPT-IN,
                             // the one sanctioned union→arguments collapse (§4.4); default off
    URLBuilder     bool      // expose a URL-builder variant of the operation
    Extra          ir.Extensions // free-form, forward-compatible per-target ergonomics
}
```

Method-name *derivation* is itself an injectable policy marked `Inferred`, disable-able and
overridable. Per-target naming/compat overlays are the same shape: a backend input keyed by IR ID,
so one IR document drives different compat baselines per language.

---

## 7. Package layout & layering

Consistent with architecture.md §3 (`backend/*` is Layer 2: imports `ir` + the backend contract,
never `frontend`, never `engine`, never a sibling target):

```
backend/
├── contract.go        # Backend, BackendInput/Output, Artifact, Capabilities, TargetKey, registry
├── plan/              # SHARED, language-neutral — imports ir only. Build(), Plan, OpPlan, TypePlan, ModelShape
│   └── plantest/      #   golden snapshots of Plan (language-independent corpus)
├── policy/            # runtime SDK-policy vocabulary + Defaults() + Merge(DeepPartial) (INV10)
├── naming/            # SHARED naming engine: acronym/compound tables, reserved-word set type,
│                      #   ID-keyed collision resolver. Casing FUNCTIONS are per-target (naming.Policy impl)
├── refine/            # SHARED refiner contract + shared helper lowerings (scalar-chain resolution,
│                      #   flattening walk, discriminator resolution, import-set harness)
├── manifest/          # generation manifest (ID-keyed), header provenance, additive-merge driver (§11)
├── verify/            # neutral Surface projection, differ, injectable per-language severity (§12)
├── golang/            # ◀ FIRST TARGET (TargetKey "go")
│   ├── backend.go     #   init() Register("go", …); wires plan→refine→emit
│   ├── goast/         #   typed target AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan → goast — the ordered lowering pipeline (§4)
│   ├── emit/          #   goast → []byte via printer + go/format; templates/ (boilerplate only)
│   ├── naming/        #   Go reserved words + default acronym table (naming.Policy impl)
│   └── mergeadapter.go#   go/ast additive-merge adapter (registers into manifest driver)
├── docs/              # docs backend: reuses plan + refine step library; docast + Markdown/HTML printer (§10)
└── mock/              # mock-server backend: reuses plan; server-routing AST + printer (§10)
```

`goast` lives *under* `backend/golang`, not in the shared contract, because a target AST is
per-language by definition. `plan`, `policy`, `naming`, `refine` (the contract + shared helpers),
`manifest`, and `verify` are shared because they are language-neutral.

**Architecture test.** The import-graph assertion (written alongside the first backend packages, not
after) asserts: `backend/golang` may import `backend`, `backend/plan`, `backend/policy`,
`backend/naming`, `backend/refine`, `backend/manifest`, `backend/verify`, `ir`; it may **not** import
`frontend/*`, `engine`, or any other `backend/<target>`. **Switch-completeness tests** live beside
`goast` (over its `NodeKind`) and beside every refiner (over `ir.TypeKind`'s eleven kinds): adding an
IR kind breaks compilation of every dispatch that must handle it (the `assertNever` obligation).

---

## 8. First-target walkthrough — a Go SDK backend

`TargetKey("go")`. Six IR constructs traced through `plan → refine → emit`. IR field names per
`ir-design.md`.

### 8.1 A model with lifecycle visibility

IR: `Model{ Properties:[id (readOnly), name (required), email, password (writeOnly, Secret)],
Additional:"closed" }`.

- **plan.** `ModelShape(User, read)` = {id,name,email}; `ModelShape(User, create)` =
  {name,email,password}. `Usage` = Input|Output. `Flattened` walks `Relations` (none here).
- **refine.** `ProjectVisibilityShapes` emits `StructType{Name:"User"}` (read) and
  `StructType{Name:"UserCreate"}` (create). `LowerOptionality`: required-nonnull → value fields,
  optional → `Pointer`. `RenderCasing` turns `["email"]`→`Email`, keeping `Tag:json:"email"` from
  `WireName`. `password` (`Secret`) gets a redacting `String()` and is excluded from the read shape.
  `Additional:"closed"` → decoder uses `DisallowUnknownFields`.
- **emit.** Printer folds the two structs, prints through `go/format`. File `models/user.go`.
  Manifest records `User.ID → "User"` and `User.ID#create → "UserCreate"`.

### 8.2 A discriminated union (`Payment = Card | BankTransfer`, tag `type`)

IR: `Union{ Variants:[Card, BankTransfer], Exclusive:true, WireTagged:true,
Discriminator{PropertyName:"type", Mapping:{"card":Card.ID, "bank":BankTransfer.ID}} }`.

- **plan.** `UnionClass` = payload-discriminated. No primary-variant collapse — the union stays
  whole (INV2).
- **refine.** Go has no sum type → `LowerUnions` picks the sealed-interface strategy:
  `type Payment interface { isPayment() }`, `Card`/`BankTransfer` each implement an unexported
  marker. `LowerDiscriminator` reads `Mapping` (keyed by **TypeID**) and generates `UnmarshalJSON`
  that peeks the `"type"` wire key and dispatches; an unrecognized tag with no `Default` becomes a
  coded decode error, not a silent nil. Variant identity survives → `NewPaymentCard(...)`.
- **emit.** Printer emits the interface, markers, concrete structs, and the custom
  `Unmarshal/Marshal` keyed on the **wire** name `"type"`. The switch-completeness test guarantees
  every variant is handled.

### 8.3 A paginated operation (`ListUsers`, cursor pagination)

IR: `Operation{ Params:[limit, cursor], Responses:[200 → {items:[]User, next_cursor}],
Pagination{Strategy:cursor, InputCursor:{cursor}, Items:{body→items}, NextCursor:{body→next_cursor}} }`,
bound `HTTPBinding{GET /users, query limit+cursor}`.

- **plan.** `OpPlan{ HasBody:false, Return:list-of-models(elementwise), ReturnType:[]User,
  ParamShape:options-bag (Inferred), Page:&PagePlan{Strategy:cursor, ItemType:User,
  ItemPath:body→items, CursorIn:cursor, CursorOut:next_cursor} }`. Item-type unwrap is a plan
  decision. Pacing delay is **not** here — it is runtime policy.
- **refine.** `BuildRequestShaping` builds the request from the query bindings.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) *UserIterator`, where `UserIterator` walks pages
  using `CursorOut` to read `next_cursor` and `CursorIn` to feed it back; items deserialize
  elementwise over `ItemPath`.
- **emit.** Printer emits `ListUsers`, `ListUsersParams`, and the `UserIterator` type + `Next()/Err()`.
  The iterator *runtime* (fetch-and-pace loop) is `pagination.go.tmpl` parameterised by
  `Policy.Pagination.AutoPageDelay` — the legitimate boilerplate seam.

### 8.4 A streaming operation (`StreamEvents`, server-sent events)

IR: `Operation{ Streaming:server, ResponseStream:&StreamDetail{ Events: &TypeRef→EventUnion } }`
where `EventUnion = Union{ WireTagged:true, Variants:[Message{Event}, Heartbeat{Event, Terminal:false},
Done{Event, Terminal:true}] }`, bound `HTTPBinding{GET /events}` with the SSE wire mechanism on the
binding.

- **plan.** `OpPlan{ Return:stream, Stream:&StreamPlan{Direction:server, EventUnion:EventUnion.ID} }`.
  Per-event content-types and the terminal bit come from `Variant.Event`.
- **refine.** The event union lowers exactly like §8.2 (sealed interface `Event` with concrete
  `MessageEvent`/`HeartbeatEvent`/`DoneEvent`). Server streaming lowers to
  `func (c *Client) StreamEvents(ctx, *StreamEventsParams) (*EventStream, error)`, where
  `EventStream.Recv() (Event, error)` returns the sealed-interface event and returns `io.EOF` after a
  variant whose `Variant.Event.Terminal` is true. Direction (server) is read from the *core*; the SSE
  framing is read from the *binding* — a bidi op (`Streaming:bidi`, both `RequestStream` and
  `ResponseStream`) would additionally emit a `Send(...)` method, and a gRPC binding of the same core
  would reuse the same `EventStream` shape over length-prefixed frames.
- **emit.** Printer emits `StreamEvents`, `StreamEventsParams`, `EventStream`, and the event union.
  The stream *runtime* (SSE frame reader) is `stream.go.tmpl` boilerplate behind the `Transport`
  interface.

### 8.5 An error taxonomy (`RateLimited`, 429, retryable-throttling)

IR: `ErrorCase{ Type:RateLimited (Model, Usage.Error), Conditions:{429}, Fault:"client",
Retryable:&true, Throttling:&true }`; policy default maps `429 → "TooManyRequests"`.

- **plan.** `OpPlan.Errors` = `[PlannedError{ Conditions:{429}, Type:RateLimited, Fault:"client",
  Retryable:true, Throttling:true }]` — declared IR facts, carried as such.
- **refine.** `LowerErrors` builds the error tree: `APIError` interface → a `ClientError`/`ServerError`
  split from `Fault` → concrete `RateLimited` implementing `error`. The retry wiring reads
  `Retryable`/`Throttling` **from the IR fact** and falls back to `Policy.Retry` only where the spec
  is silent (§6.3); throttling selects the throttling backoff class. The `Error` suffix is a
  `naming.Policy` rendering of the neutral taxonomy — Python would render `TooManyRequestsError` from
  the same neutral kind.
- **emit.** Printer emits the error type + `Error() string`, registers `429 → *RateLimited` in the
  response decoder, and the retry runtime consults `IsRetryable()`/`IsThrottling()` which return the
  spec-declared values.

### 8.6 Auth (OR-of-ANDs, service default with an op override)

IR: `Service.Auth = [ AuthRequirement{Schemes:[apiKey]} ]`;
`Operation.Auth = [ AuthRequirement{Schemes:[oauth2(scopes:[read])]},
AuthRequirement{Schemes:[apiKey]} ]` (priority order); auth schemes in `Document.Auth`.

- **plan.** `OpPlan.Auth` = the operation's options, priority-ordered (the op overrides the service
  default). An empty option inside a non-empty list = "no auth is one acceptable choice"; an empty
  `Operation.Auth` slice would mean "explicitly public".
- **refine.** Auth *schemes* are structural IR, so refine emits an `Authenticator` interface, a
  per-scheme option type (`WithAPIKey(...)`, `WithOAuth2(...)`), and the request-time application
  code that satisfies the AND-within-option / OR-across-options-by-priority contract. Scope sets from
  `oauth2` flows reach the option types. What refine does **not** emit is any credential value or
  token-refresh behavior — the actual secrets and token acquisition are runtime configuration, not
  IR. SASL/x509/user_password schemes (Kafka/AMQP) lower the same way for a messaging target.
- **emit.** Printer emits the `Authenticator` interface, the option constructors, and the per-request
  auth selector.

### 8.7 A long-running operation (`StartExport`, status-monitor polling)

IR: `Operation{ LongRunning:&LongRunning{ FinalStateVia:"status-monitor", PollingOperation:GetExport.ID,
FinalType:&TypeRef→Export, ResultPath:body→result } }`, bound `HTTPBinding{POST /exports}`; the
declared poll op `GetExport` is `HTTPBinding{GET /exports/{id}}` returning an `ExportStatus` monitor.

- **plan.** `OpPlan{ Return:model, LRO:&LROPlan{ FinalStateVia:"status-monitor", PollOp:GetExport.ID,
  FinalType:Export, PollType:ExportStatus, ResultPath:body→result } }`. Which response signals
  terminal state, and where the result lives, are plan facts unwrapped from the monitor type — the
  same "classify, don't decide" split pagination uses. Poll *pacing* is **not** here; it is runtime
  policy, exactly like `Pagination` pacing (§8.3).
- **refine.** `LowerLRO` emits a poller:
  `func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller, error)`, where
  `ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
  `ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
  `ResultPath`. `FinalStateVia` selects how the poller finds the monitor URL (status-monitor vs
  `operation-location` header vs original-URI). The poller *type* and result extraction are typed AST;
  the poll loop's backoff between attempts is `lro.go.tmpl` boilerplate parameterised by policy — the
  same structure/runtime line as the iterator (§8.3) and the stream reader (§8.4). Smithy waiters are
  *not* folded in here: their acceptor/JMESPath lists arrive verbatim in `Operation.Extensions`
  (`ir-design.md §7.3`) and a waiter-aware refiner is a separate, opt-in step.
- **emit.** Printer emits `StartExport`, `StartExportParams`, and `ExportPoller` +
  `Poll()/PollUntilDone()`. `OneWay` operations take the opposite path — no poller, no response
  binding, a fire-and-forget method — read straight from `OpPlan.OneWay`.

---

## 9. The lowering table (normative IR → Go)

The complete backend acceptance surface is `ir-design.md §4–§9`; this table is the
condensed normative mapping for the first target. Every row must have a Go emit path or a diagnosed
degrade; a missing row is an IR bug (INV9), not a frontend change.

| IR construct | Refine decision (Go) | Wire truth preserved |
|---|---|---|
| `Primitive` (incl. `integer`/`number`/`decimal`) | native type; arbitrary-precision → `math/big`/decimal lib or diagnostic; `any`→`any`; nil `*TypeRef`→void | — |
| `Scalar{Base}` chain | resolve to nearest base + merged constraints; nil-base → newtype | encoding triple |
| `Model` (Base/Implements/Mixins) | struct; embed base+mixins, interfaces for `Implements`; `Abstract`→interface; `Positional`→index-ordered struct | `WireID` order |
| `AdditionalProps` / `Additional` | catch-all `map`; `closed`→`DisallowUnknownFields` | pattern channel |
| `Union` (4 tag modes) | sealed interface + marker + concrete variants + tag-dispatch (de)serializer | `WireName`/`WireID` per variant |
| `Enum` open/closed/flags | closed→typed consts; open→string+consts+`Unknown`; flags→bitset; `FallbackMember` on decode | member `WireName` |
| `List`/`MapT`/`Tuple` | `[]T`/`map[K]V`/struct; `List.Encoding`→serializer mode | packed/expanded |
| `Literal` | typed constant (incl. `symbol`) | — |
| `External` | imported library type at `MinVersion` + dep-manifest; opaque handle→passthrough/diagnostic | — |
| `Any` | `any` (distinct from a diagnosed unknown) | — |
| `Visibility` sets | N structs from `ModelShape`; PATCH → optional | — |
| `Required` ⊥ `Nullable` ⊥ `Presence` | `T`/`Opt[T]`/`Nil[T]`/`OptNil[T]`/omitempty+presence; collapse→diagnostic | — |
| `Constraints` | field `validate` tags + refine-built `Validate()`; unrepresentable regex→diagnostic | numeric bounds as decimal |
| `Discriminator{Mapping→ID}` | tag-dispatch `UnmarshalJSON` + factories | discriminator wire key |
| `Pagination` (PropPaths) | iterator/auto-pager; item unwrap from plan; pacing from policy | path-based item/cursor read |
| `Streaming` + `StreamDetail` | `EventStream.Recv()`/`Send()` over event union; terminal ends; direction from core, framing from binding | per-event content-type |
| `LongRunning` | poller type + `Poll()`/`PollUntilDone()`; result from `ResultPath`; pacing from policy | monitor/final-state channel |
| `OneWay` | fire-and-forget method, no response binding | — |
| `Channel`/`Message`/`MessageBinding` | `MsgView` → channel client + send/receive + reply routing (messaging target) | address/correlation paths |
| `ErrorCase{Fault, Retryable}` | error tree; declared facts win over policy | status→error map |
| `Auth` OR-of-ANDs | `Authenticator` + per-scheme options; priority order | — |
| `Value{Num BigVal, Ctor, Ref, symbol}` | literals via decimal (never float64); `Ctor`→call; `symbol`→string explicitly | — |
| `Naming{Canonical}` | cased via `naming.Policy`; reserved-escaped; collisions ID-keyed | `WireName`/`WireID` never cased |

---

## 10. Docs and mock-server backends reuse the contract

Both are `Backend` implementations consuming the **same `Doc + Plan`** — the payoff of computing the
plan once: docs and mocks agree with the SDK by construction, not by re-derivation.

- **Docs backend (`backend/docs`).** Reuses the shared plan verbatim (primary response/content,
  pagination, return-shape, error taxonomy — exactly what docs must describe) and most of the refine
  step library, but lowers into a `docast` (pages, operation blocks, schema tables, examples) and
  runs a **reduced pipeline**: no `EscapeReservedWords`, and casing is a presentation choice (SDK-style
  identifiers or spec-faithful source names, a `naming.Policy` selection). `Docs.Description`
  `{t:TypeID}` cross-reference tokens resolve to intra-doc links via the *same* IR-ID lookup the Go
  backend uses for doc comments — links survive renames. Examples use the schema arm (`Value/Headers`)
  vs the operation arm (`Input/Output/Error`) correctly. No runtime `Policy` is consulted — proving
  the IR/policy split pays off.
- **Mock-server backend (`backend/mock`).** Reuses `PrimaryResponse`/`PrimaryContent` and the IR
  `Examples` to synthesize canned responses, the error taxonomy to simulate declared failures, the
  `BindingView` to route requests, and the visibility projections to honor the same wire shapes an
  SDK sends/receives. Refine lowers into a server-routing AST (routes from `HTTPBinding.URITemplate`,
  handlers validating against constraints). Because the same plan feeds SDK and mock, the two are
  **wire-consistent by construction** — the mock can *be* the interception target the SDK's
  wire-conformance harness (§13) is diffed against.
- **Validation backend (future).** Additionally consumes the validation-only constructs preserved
  verbatim in `Extensions` (`not`/`if-then-else`/`dependentSchemas`) that SDK/docs backends ignore.
  The contract already carries them; no IR change is needed (INV9).

Adding any of these is a registry entry over one `Doc + Plan` — no change to the ABI, no change to
the IR.

---

## 11. Write / integrate

Post-milestone (architecture.md §2.3), but the contract is designed so it bolts on with **no IR
change** (INV9). The `manifest.Manifest` and `Artifact` fields returned from `Generate` are the
seams; the `Origin`-tagged AST makes the manifest a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records spec/plan/emitter/config **hashes**,
  a **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OpID`/`PropID`** — assembled from the `Origin` on every emitted node. This is the biggest
  win of stable-ID keying: a spec rename is "same ID, new symbol" — a lookup, not oagen's structural
  rename-inference machinery. Rungs 2–5 of the integration ladder (architecture.md §2.3) rest on it.
- **File-header provenance gates pruning.** `Artifact.Header` writes a generated-by header; the
  writer **never deletes a file lacking it** (hand-edited). First adoption (no prior manifest) skips
  pruning, writes a baseline, emits a notice.
- **Ignore regions & additive-only merge.** Two provenance markers — whole-file
  (`@morphic:ignore-file`) and region (`@morphic:keep`/`:end`) — with append-only merge. The
  structural merge is **language-parameterised** by an adapter registered into the manifest driver;
  the Go adapter (`backend/golang/mergeadapter.go`) uses `go/ast` to parse existing files and append
  only new top-level symbols, never delete. No adapter ⇒ skip, never clobber. Case-only renames go
  via a temp name (APFS/NTFS hazard).
- **Staleness check.** `(prior-manifest entities − current entities) ∩ files-on-disk` surfaces
  dead code additive writers cannot prune — cheap because both manifests are ID-keyed and the prior
  IR/plan snapshot is serializable (INV7).

---

## 12. Surface verification / compat

Post-milestone. `BackendOutput.Surface` is the neutral API-surface projection — classes, interfaces,
enums, methods, fields as neutral records — built by the **same walk** over `Origin`-tagged AST nodes
that emit performs.

- **Neutral projection with IR IDs.** Both the freshly-generated SDK and an extracted
  existing SDK (via a per-language extractor) project into one `verify.Surface`; each record carries
  its **IR stable ID** (threaded from `Origin`), so cross-language findings correlate without
  name-matching, and rename detection is an ID lookup rather than oagen's Jaccard-similarity
  heuristics. `Generate` returns the generated side directly; only the extractor parses source.
- **Injectable per-language severity.** The differ emits **neutral change records**; a
  per-target `verify.Severity` function decides breaking/additive (parameter names are public API in
  PHP, arity in Go, ~nothing in JS) — heuristics-as-policy (INV6).
- **Behavioral channel separate from structural.** A default-value change is behavioral,
  tracked in a distinct channel from a signature change — detectable without re-parsing because the
  plan already carries defaults as typed `Value`s. Every finding carries drift provenance (which
  stage caused it).

---

## 13. Mandatory backend tests

Built as the code lands, extending the testing strategy in `architecture.md §5`. Cited as `T-N`
throughout this document.

1. **T-1 · TypeKind switch-completeness** — a generated test over all eleven `ir.TypeKind`s and over
   the target-AST `NodeKind`; adding a kind breaks every dispatch (`assertNever`).
2. **T-2 · Union tag-mode coverage** — all four wire shapes emit distinct (de)serializers.
3. **T-3 · Four-state field matrix** — required×nullable×presence produce distinguishable
   declarations, or a diagnosed collapse.
4. **T-4 · Wire fidelity** — serialized output uses wire names/IDs, never rendered identifiers;
   positional models serialize by index; `WireID==0` ≠ absent.
5. **T-5 · No-float64** — numeric defaults/constraints/enum values round-trip through `BigVal` with
   no float parsing, including any constraint-merge path.
6. **T-6 · Determinism / golden IR→artifact** — identical IR ⇒ identical bytes; a scoped run equals
   the corresponding slice of a full run.
7. **T-7 · Diagnostic-not-silence** — every un-lowerable construct emits a coded diagnostic, never a
   silent `any` and never a panic.
8. **T-8 · Manifest ID-keying** — the entity→symbol map survives path/name churn.
9. **T-9 · Policy precedence** — declared `Retryable`/`Fault`/`Idempotency` override policy defaults.
10. **T-10 · Step-trace golden** — the ordered refine step list produces a pinned, deterministic
    trace (guards implicit inter-step coupling).
11. **T-11 · Architecture import-graph** — the layering in §7.
12. **T-12 · Wire-conformance (milestone 3)** — expected request shapes derived from `Doc + Plan`
    alone, diffed against the generated SDK under HTTP interception; request-side mismatches block,
    response-side inform.

---

## 14. Per-target milestone plan

Extends architecture.md §6 milestone 3 (the first backend) and beyond.

1. **M3a — plan layer + Go target skeleton.** `backend/plan` with its language-independent golden
   corpus; `backend/golang` emitting models, enums, and one service client end-to-end. Proves the
   plan/refine/emit boundary and that the IR retains what a refiner needs (architecture.md §6 M3).
2. **M3b — the hard axes.** Unions (all four tag modes), discriminators, visibility projections, the
   four optionality states, containers, external types, values. Each is a refine step tied to a
   specific un-lowered IR fact — the acceptance test cashed.
3. **M3c — operations & runtime.** Pagination, streaming, LRO, error taxonomy, auth; the runtime
   policy tree and the boilerplate templates; the wire-conformance harness.
4. **M3d — docs + mock backends.** Prove reuse of `Doc + Plan` over a second and third target AST,
   and that docs/mock agree with the SDK by construction.
5. **M4 — write/integrate + surface verification.** Manifest, header-gated pruning, ignore regions,
   additive `go/ast` merge, staleness; neutral surface diff with injectable per-language severity.
6. **M5 — second language (TS or Python).** Proves cross-language uniformity: a new target is a new
   target AST + printer + refine pipeline reusing the shared plan and step contract, forcing **no**
   IR or plan change. This is where the shared-plan investment visibly pays back.

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

## 16. Open questions (tracked for implementation)

1. **Plan-fact vs refine-fact boundary.** The plan classifies; refine decides. The rule is stated
   and golden-tested (a plan decision must not assume a target's capabilities) but not
   compiler-enforced. Needs a documented decision procedure for "is this a plan fact or a refine
   fact?" before the second language lands, so the plan does not bloat with speculative,
   target-shaped fields.
2. **Target-AST granularity.** The line is "declarations and signatures = typed AST; behavior and
   boilerplate = template". Method *bodies* that are pure boilerplate (the iterator `Next`, the retry
   loop) stay templated; an over-fine AST that models statement-level Go grammar is a non-goal. The
   exact granularity is a per-feature judgment that will be litigated as features land.
3. **Where content-negotiation ties break.** The `json > form > multipart > binary` default is a
   shared plan helper (resolves `ir-design.md` Q3), but a target that wants a different primary needs
   a clean override path that does not fork the plan. Leaning: an override on `plan.Policy`, since the
   choice is document-neutral.
4. **Protocol-view neutrality.** The `BindingView` and target-AST `MethodKind` must be designed
   against the full binding matrix (HTTP/RPC/message/GraphQL/OTP) from day one, or they ossify around
   HTTP the way Kiota's model did. The Go-first walkthrough is HTTP-heavy; the RPC/messaging targets
   are the real test. Revisit when the second-protocol backend lands (tracks `ir-design.md` Q6).
5. **Manifest granularity for merge.** Whether the entity→symbol map keys at property level (not just
   type/op level) to support field-level additive merge, or leaves sub-type merge to the `go/ast`
   adapter. Decide when write/integrate (M4) is built.
6. **Surface behavioral channel scope.** Which behavioral changes beyond default-value changes
   (e.g. a `Retryable` flip, an idempotency change) belong in the separate behavioral channel vs the
   structural one. Decide when surface verification (M4) is built against real cross-revision diffs.

---

## 17. Invariant compliance

| INV | Where satisfied |
|---|---|
| 1 IR is the ABI | `BackendInput.Doc` is the only structural input; `backend/*` imports `ir` + shared contract, never `frontend`/`engine`/a sibling target (§2, §7) |
| 2 Lossless, lowered late | all lowering is in `refine` steps building a fresh target AST; the IR is never mutated; unions/enums/discriminators/visibility/pagination/streaming/LRO arrive un-lowered and the step list is the acceptance test (§4, §8) |
| 3 Stable IDs, names presentation | plan, AST `Origin`, manifest, surface, hints, and the collision table are all keyed by IR stable ID; renames are lookups; collisions never touch identity (§3, §4.12, §11) |
| 4 Names neutral, backend cases | `Ident` starts as `Naming.Canonical` word sequence; `RenderCasing`/`EscapeReservedWords` are the only casing steps; struct tags come from `WireName`/`WireID`, a separate channel (§4.12, §5) |
| 5 Pure, typed diagnostics | `Generate` is pure/reentrant/no-stderr/no-I/O; every un-lowerable construct → coded `ir.Diagnostic`; the engine decides fatality (§2, §2.4) |
| 6 Heuristics are policy | `plan.Policy`, `naming.Policy`, `verify.Severity`, method-name derivation, and shaping hints are injectable and disable-able; inferred outputs marked `Inferred` (§3.5, §6.4, §12) |
| 7 Serializable/deterministic | `Plan` + manifest are JSON-serializable; maps walked sorted, slices source order; identical input ⇒ identical bytes (§3, §5.5, §13) |
| 8 Optionality ≠ nullability | `LowerOptionality` renders all four states plus protobuf `Presence` as a third axis; collapses are diagnosed (§4.7) |
| 9 Capability surface complete | every `ir.TypeKind` and every operation facet (pagination/streaming/LRO/one-way/messaging) has a lowering path or diagnosed degrade (§4.2, §9); docs/mock/validation are registry entries over the same `Doc + Plan`; a new language forces no IR or plan change (§10, §14 M5–M6) |
| 10 Runtime policy separate | `policy.Policy` tree in `BackendInput.Policy`, never on `ir.Document`; it parameterises only boilerplate; declared IR facts win over its defaults (§6) |
