# Prior Art — Lessons from oagen, Kiota, and TypeSpec

Condensed findings from studying three reference codebases, and the decisions Morphic takes from
each. This is the evidence base behind `architecture.md` and `ir-design.md`.

---

## 1. oagen (WorkOS) — a single-spec SDK generator with a flat IR

oagen parses OpenAPI 3.x into a small structural IR (`ApiSpec` → services → operations,
plus a flat model/enum registry) and hands that IR to per-language emitter plugins.

### Worth adopting

- **Strict one-way dependency layering**, enforced by an architecture test: the IR package
  imports nothing, the parser imports only the IR, the engine imports both, emitters are external
  plugins that receive IR values. The IR is the ABI between parsing and generation.
- **`TypeRef` as a closed discriminated union** with exhaustiveness checking, so adding a type
  kind breaks every switch that must handle it.
- **Nullability as an explicit node**, not a boolean scattered across every type.
- **Models referenced by name/handle, never embedded** — a flat registry makes deduplication,
  traversal, and serialization trivial and makes recursive types a non-issue.
- **A "resolved plan" layer between IR and templates**: semantic rendering decisions
  (is-paginated, has-body, idempotent-post…) are computed once, language-neutrally, so string
  templates contain no policy.
- **Runtime SDK policy (retry, telemetry, error taxonomy) kept in a separate tree** from the
  structural API description.

### Mistakes to avoid

| oagen behavior | Morphic decision |
|---|---|
| Models keyed by *derived string names*; collisions resolved silently ("keep largest"), refs fixed by string rewriting | Stable synthetic IDs (source-pointer-derived); names are presentation metadata |
| Inline-schema naming logic duplicated across ≥4 call sites that must stay byte-identical | One hoisting pass, keyed by source-pointer identity |
| `allOf` eagerly flattened to a field list — the inheritance relationship is lost | Composition preserved un-lowered (base + mixin provenance kept) |
| oneOf branches without discriminator merged as *optional fields* — exclusivity constraint lost | Unions always survive as union nodes |
| Pagination/envelope/discriminator detection via hardcoded name lists | Heuristics are injectable frontend policy, never IR semantics |
| Failures are `console.warn`; unresolved refs degrade to `unknown` silently | Typed diagnostics with severity + provenance, collected on the IR document |
| Single "primary response" privileged; media-type multiplicity collapsed by fixed priority | All responses and all content types are first-class |
| Mutable module-global parser state (non-reentrant) | Frontends are pure `(source, options) → (IR, diagnostics)` |

---

## 2. Kiota (Microsoft) — a language-neutral CodeDOM with per-language refiners

Kiota's pipeline: `OpenApiDocument → URL tree → CodeDOM (language-neutral) → per-language
refiners (in-place tree mutations) → per-language writers`. The CodeDOM is a *code* model
(classes, methods, properties), not a *spec* model.

### Worth adopting

- **Kind enums on every element** (`CodeClassKind.Model/RequestBuilder/…`,
  20 `CodeMethodKind`s): generic tree machinery + precise semantic tagging.
- **Wire-name vs symbol-name split on every named thing** (properties, parameters, enum
  members). Serialization correctness never depends on identifier casing.
- **Discriminator information kept un-lowered** on both classes and union types, so each
  refiner can choose its own lowering (factory methods, wrapper classes, native unions).
- **Per-status-code error mapping** on the operation (`"404" → Type`), with range codes
  (`4XX`, `5XX`, `XXX`) and deduplication.
- **Serialization behind runtime abstraction interfaces** (`IParseNode`/`ISerializationWriter`)
  — generated code is format-agnostic; formats are pluggable at runtime.
- **Documentation as templates with type references**, so doc cross-links survive renames.
- **The refiner contract itself**: everything a language needs to re-shape (unions → wrapper
  classes, indexers → `ById()` methods, reserved-word renames) must still be present,
  un-lowered, in the neutral model. This defines the IR's minimum retained information.

### Mistakes to avoid

| Kiota behavior | Morphic decision |
|---|---|
| Children keyed by *name* in each block → elaborate collision-reconciliation logic | ID-keyed containers; name uniqueness is a validation, not an identity mechanism |
| Unions flattened early into `MemberN`-named wrapper conventions; unresolvable shapes fall back to opaque `UntypedNode` | Unions keep variant identity, tag mode, and open/closed semantics until a backend lowers them |
| `allOf` supports only one true base; extra entries silently intersection-merged | Base + explicit mixin list, merge deferred |
| Enums are string-backed only; no numeric values, no open/closed distinction | Enums carry a value type, member values, and an explicit open/closed bit |
| Collections are flags on the type reference (`CollectionKind`), maps are not a distinct type | List/Map/Tuple are proper type nodes |
| No streaming, events, webhooks, or channel concepts — everything is request/response | Streaming direction on the operation core; channels/messages as first-class nodes |
| Recursion handled by depth caps + visited sets (can truncate) | Flat ID-referenced registry makes recursion structurally safe |
| The "neutral" model is HTTP-shaped at its core | Operation core is protocol-neutral; HTTP is one binding among several |

### Kiota's request-side modeling (reference for the generator layer, not the IR)

RequestBuilder-per-URL-segment fluent navigation, RFC 6570 URI templates with a path-parameter
dictionary, per-operation executor/generator method pairs, query-parameter classes, and
request-configuration wrappers are all *generator-side* constructs. Morphic keeps these out of
the IR; they belong to backend plan layers.

---

## 3. TypeSpec (Microsoft) — the richest source type system, plus Microsoft's own client IR

Two distinct layers were studied: the compiler's core type graph, and the client-generator
input model (the in-repo mirror of TCGC, `typespec-client-generator-core`) — Microsoft's own
answer to "what should a codegen IR look like."

### Type-system concepts the IR must be able to represent

- **Types vs Values are separate channels.** Defaults, constants, examples, and decorator
  arguments are typed *values*, not types. Numeric values are arbitrary-precision.
- **Scalar extension chains** (`scalar customId extends string`) with constraints and encoding
  accumulated along the chain.
- **The logical/wire/encoding triple**: a property has a logical type (`utcDateTime`), an
  encoding name (`unixTimestamp`), and a wire type (`int32`). TCGC reifies exactly this.
- **Nullability is a union with `null`** in the source graph; TCGC *reifies* it as an explicit
  nullable wrapper when lowering. Morphic reifies it on the type reference.
- **Visibility is lifecycle-class based** (Read/Create/Update/Delete/Query), and one logical
  model legitimately produces N wire shapes (create body vs read body vs PATCH body where
  everything becomes optional). `readOnly`/`writeOnly` are a two-bit projection of this.
- **Templates are monomorphized before emit** — emitters only see finished instances. An IR does
  not need generics as declarations, only optionally as provenance on instantiated types.
- **Spread/intersection are flattened at check-time but provenance is retained**
  (`sourceModels` with `is|spread|intersection` tags).
- **Named union variants** independent of any discriminator property.
- **Auth is OR-of-ANDs**: a list of options, each option a set of schemes that apply together.
- **Paging metadata uses property *paths*** (segments from response root), not top-level names —
  next-links and page items can live inside envelopes.
- **Versioning as a timeline** (`@added/@removed/@renamedFrom/@typeChangedFrom` per element),
  lowered by projecting per-version snapshots. Both representations are legitimate; snapshots
  are what emitters consume.
- **Multi-service**: a Service root is distinct from Namespace; one compilation can emit several.

### The TCGC/C#-emitter client IR — the closest existing thing to Morphic's target

Its design choices, most of which Morphic adopts in spirit:

- **Two-level operation split**: `ServiceMethod` (idiomatic client method: paging/LRO kind,
  method params, convenience/protocol flags) wraps `Operation` (the wire call: HTTP verb, URI,
  wire parameters, responses). Spec IR ≈ the wire level; method-level intent (paging, LRO)
  is metadata the spec layer carries so the client layer can be derived.
- **`crossLanguageDefinitionId`** — a stable, spec-derived identity on every named type and
  method, used for cross-language/cross-version correlation. Direct precedent for Morphic's
  stable IDs.
- **`UsageFlags`** (Input/Output/Error/Json/Xml/MultipartFormData/…) computed per model — drives
  which serializers and shape-variants get generated.
- **Open vs closed enums as an explicit bit** (`isFixed`), plus flags-enums.
- **Discriminated polymorphism fully materialized**: base model holds the subtype map
  (wire value → model) and the discriminator property; each subtype holds its value.
- **Explicit nullable, dictionary, and literal (constant) type nodes.**
- **External-type mapping** (`external: {identity, package, minVersion}`) — a type can resolve
  to a well-known library type instead of being generated.
- **Per-parameter wire detail** kept on the binding: query explode/delimiter, path style +
  allowReserved, header prefixes, body content types.

### TypeSpec concepts OpenAPI cannot express (why the IR designs above the OpenAPI ceiling)

Interfaces/operation grouping, custom scalars with encodings, named union variants, lifecycle
visibility, paging/LRO as semantics rather than convention, versioning timelines, values/consts,
multi-service — all of these exist in TypeSpec and Smithy today, appear in the client IR, and are
lost when squeezed through OpenAPI. Morphic's IR keeps them first-class so a future TypeSpec or
Smithy frontend loses nothing.
