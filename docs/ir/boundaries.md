# Boundaries

Part of the [IR design](./README.md). What the IR declines to model, on purpose: each boundary here
is one an `Unmodeled` [reason](./unmodeled.md#reasons) names.

## Validation-only constructs

JSON Schema's `not`, `if`/`then`/`else`, `dependentSchemas`, `dependentRequired`,
`contains`/`minContains`/`maxContains`, `propertyNames`, `unevaluatedItems`, and
`unevaluatedProperties` express *validation logic*, not data shape; no target language's type
system represents them, and none of the other source formats has an equivalent. The IR
deliberately does not model them structurally. Compilers keep them verbatim in `Unmodeled`
(`openapi:not`, `openapi:if-then-else`, `openapi:contains`, …) under `ReasonValidationOnly` and
emit an `info` diagnostic, so the information is never silently lost and a future
validation-oriented emitter (request validators, mock servers) can still consume them — selecting
on the reason rather than sifting vendor metadata out of the same bag
([unmodeled](./unmodeled.md#reasons)). The one structural carve-back: `unevaluatedProperties: false`
is *shape* (a closed model after composition) and lowers to
`Model.Additional = closed_after_composition`. Every other `unevaluated*` form stays verbatim,
`unevaluatedProperties` with a schema value included. `propertyNames` is not a second carve-back
onto `AdditionalProps.Key`: it constrains *every* key of the object, including the ones `properties`
declares — where there is no `AdditionalProps` to hold a key type at all — and it is a whole schema
rather than a type, so only its degenerate `{type: string}` form names one.

Two neighbouring keyword groups are *not* part of this boundary — the content vocabulary
(`contentEncoding`, `contentMediaType`, `contentSchema`) and the keywords that identify a schema
resource (`$id`, `$schema`, `$vocabulary`) — and neither is `$dynamicRef`. How the OpenAPI compiler
lowers each is stated with its rules:
[`content-vocabulary`](../compilers/openapi.md#content-vocabulary),
[`dialect-keywords`](../compilers/openapi.md#dialect-keywords) and
[`dynamic-ref`](../compilers/openapi.md#dynamic-ref).

This is the one intentional carve-out from "lossless means structural": losslessness is satisfied by
verbatim preservation, and the carve-out is explicit rather than accidental. The OpenAPI compiler
holds every 2020-12 keyword to being lowered or kept
([`vocabulary-coverage`](../compilers/openapi.md#vocabulary-coverage)).

## Degraded constructs

A few source constructs have no faithful target — either because the IR has no combinator for the
shape, or because no target language's type system holds it. Each gets a *normative* weaker
lowering, so the degradation is a decision rather than an accident, and — wherever that weaker
shape actually loses something — verbatim preservation under `ReasonDegradedLowering` beside it,
so the loss stays recoverable. A construct kept raw merely for want of an IR field at that
position is a different situation and carries `ReasonNoIRHome` instead
([unmodeled](./unmodeled.md#reasons)).

The list is the set of *source-construct* degradations: one entry per distinctive source shape the
IR declines to model as written. It is not a list of every lowering that can weaken a type: the
whole-schema fallbacks a compiler takes at its own limits are its rules, as the OpenAPI compiler's
are ([`whole-schema-fallbacks`](../compilers/openapi.md#whole-schema-fallbacks), and for a `false`
`allOf` branch [`allof-false-branch`](../compilers/openapi.md#allof-false-branch)).

- **JSON Schema shapes** — `oneOf`/`anyOf` co-declared with structural keywords that cannot be
  distributed, competing keywords at one position, an inline `allOf` branch declaring more than the
  merge consumes, a redeclared property the merge cannot fold whole, and a shape applicator the
  lowered node cannot carry. Each is a rule of the OpenAPI compiler:
  [`union-beside-structure`](../compilers/openapi.md#union-beside-structure),
  [`competing-keywords`](../compilers/openapi.md#competing-keywords),
  [`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue),
  [`allof-redeclared-property`](../compilers/openapi.md#allof-redeclared-property) and
  [`shape-applicator`](../compilers/openapi.md#shape-applicator).
- **TypeSpec `never`-typed properties/variants** — no `never` node exists, so compilers delete
  the member and emit an `info` diagnostic. Nothing is preserved beside it: a `never` member
  admits no value, so there is no content the deletion could lose.
- **TypeSpec `StringTemplate` types** (interpolating non-literal types) — degrade to `string` +
  `Unmodeled["typespec:string-template"]` + `info` diagnostic.
- **Erlang bit-sized binaries** (`<<_:M, _:_*N>>`) — `bytes` + `Min/MaxLength` when
  byte-aligned, plus `Unmodeled["erlang:bit-size"] = {base, unit}` + `info` diagnostic.
- **Erlang `fun()` types** — `External{Identity: "erlang:fun"}` + full spec text in
  `Unmodeled["erlang:fun-spec"]` + `warning` diagnostic (non-portable member). No function
  type node exists.
- **Erlang heterogeneous map association lists** (`#{atom() => a(), integer() => b()}`) —
  lower to a `MapT` whose key and value are unions ([containers](./containers.md)), or to
  union-typed `AdditionalProps` beside a map's `:=` associations, + `Unmodeled["erlang:map-assocs"]`
  verbatim, since a union on each side loses which key type pairs with which value type.
- **Erlang `-opaque`** — the shape lowers structurally and losslessly; what degrades is the
  abstraction boundary the attribute declares, which no IR node states.
  `Unmodeled["erlang:opaque"] = true` records it, and emitter policy decides whether to expose
  or wrap.

## Deliberate exclusions

Constructs on this list that a source format actually declares are still kept — losslessness is
not negotiable — but they are kept under `ReasonOutOfScope` ([unmodeled](./unmodeled.md#reasons)),
never `no_ir_home`: the boundary is drawn on purpose, so no promotion pass should read them as a gap
to close. Entries here that no format declares (language casings, generator plan artifacts) are
simply absent from the IR and preserve nothing.

- **Language names/casings** — emitters own identifier rendering entirely (IR stores neutral
  word sequences + wire names).
- **SDK runtime policy** (retry/timeout/telemetry/error-class taxonomy) — a separate emitter
  input, never IR (§2.4 of architecture.md). OTP call timeouts and `multi_call`/`send_request`
  machinery fall here too.
- **Generator plan artifacts** (request builders, executor pairs, per-visibility model variants,
  primary-response selection) — computed views in emitters, not stored.
- **Structured modeling of transport-deployment minutiae** (Kafka partition configs, AMQP
  exchange args) — preserved as raw namespaced entries.
- **Smithy rules-engine traits** (`@endpointRuleSet`, context params) — runtime endpoint
  resolution machinery; preserved as raw namespaced entries on the service.
- **Smithy waiters** (`smithy.waiters#waitable`) — preserved verbatim in `Operation.Unmodeled`,
  never folded into `LongRunning`; promoted to a typed node only if a second format lands.
- **TCGC-style client-shaping decorators** (`@clientName`, `@access`, `@usage`, `@scope`, …) —
  per-language SDK-surface policy, preserved as namespaced entries for emitter policy layers.
- **Arbitrary GraphQL persisted queries** — the type graph and entry points are retained; query
  composition is a emitters/runtime feature.
- **Capability/interface-typed fields** (Cap'n Proto capability passing) — requires
  services-as-types; out of scope for a data-SDK compiler.
- **Function types** — Erlang funs degrade per [degraded constructs](#degraded-constructs); no
  target language marshals closures.
- **A general intersection combinator** (an `Intersection` node, or an `Intersect` sum arm, for the
  *struct ∧ union* shape a JSON Schema `oneOf` beside structural keywords writes) — asked and
  answered, so it does not need re-litigating. The capability is not cross-format: only OpenAPI and
  AsyncAPI can express it, and they share a compiler code path, so a node would be one format's
  artifact given IR-wide status against invariant 9. TypeSpec's `&` is *model-only*
  (`ir-spec-matrix.md`'s Intersection row records it as `⚠ & (model is)`) and so is already covered
  by `Base`/`Mixins` ([models](./models.md#composition)); Smithy, GraphQL, Protobuf and Erlang/OTP
  cannot express it at all. The shape is instead handled where it arises: distributed across the
  union's variants per [models](./models.md#composition-beside-a-union) when every branch names a
  referent, and kept verbatim under `ReasonDegradedLowering` in the five residual shapes
  [`union-beside-structure`](../compilers/openapi.md#union-beside-structure) lists. A second format
  that declares a genuine intersection — not a model-only `&` — is what would reopen this.
