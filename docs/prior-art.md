# Prior Art — Lessons from oagen, Kiota, and TypeSpec

Condensed findings from studying three reference codebases, and the decisions Morphic takes from
each. This is the evidence base behind `architecture.md` and `ir-design.md`.

---

## 1. oagen (WorkOS) — a single-spec SDK generator with a flat IR

oagen parses OpenAPI 3.x into a small structural IR (`ApiSpec` → services → operations,
plus a flat model/enum registry) and hands that IR to per-language emitter plugins.
Every claim in this section has been verified against the source (parser, IR, engine,
and the differ/compat/verify subsystems added since the original survey).

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
  (is-paginated, has-body, idempotent-post, primary-response selection, return-shape
  classification, parameter-passing shape…) are computed once, language-neutrally, so string
  templates contain no policy.
- **Runtime SDK policy (retry, telemetry, error taxonomy) kept in a separate tree** from the
  structural API description.
- **Policy addressed by wire identity, not derived names** — consumer operation hints are keyed
  `"METHOD /path"`, never by generated method name, because derived names churn. Independent
  convergence on stable source identity; Morphic's IDs make the same key durable across path
  renames too. The hint vocabulary itself (rename, remount, split-union-body into typed wrapper
  methods, constant defaults, client-config-derived fields, URL-builder ops) is a concrete
  inventory of what a client-shaping emitter policy input must express.
- **Group metadata over an untouched wire list** — mutually-exclusive parameter groups
  (`x-mutually-exclusive-parameter-groups`) are modeled as grouping metadata whose variants
  share the parameter objects by identity, while the flat wire arrays stay authoritative for
  serialization. Ergonomic structure layered on wire truth, neither duplicated nor destroyed.
- **A generation manifest + provenance-gated integration** — regenerating into a live repo
  rests on: a manifest (spec/emitter/config hashes, sorted file list, operation → generated
  symbol map), file-header provenance that gates pruning (never delete a file lacking the
  generated-by header), ignore-region markers for hand-written islands, and additive-only
  AST merges. Manifests should key entities by stable ID — oagen's `"METHOD /path"` keys break
  on path renames.
- **Wire-conformance smoke testing with a spec-only offline baseline** — the expected request
  shape (method, path, query, body keys) is derived from the spec alone and diffed against the
  generated SDK running under HTTP interception; request-side mismatches block, response-side
  inform. The decisive test of a generator is bytes-on-the-wire, not compilation.
- **A behavioral-change diff channel** — default-value changes are surfaced separately from
  structural signature changes (same signature, different runtime behavior), and every compat
  finding carries *drift provenance* (which pipeline stage caused it) plus, where detectable,
  a spec-level *remediation* hint (e.g. "new schema forks the old — extend instead").
- **Narrow, expiring diagnostic approvals** — intentional breaking changes are allowlisted per
  (symbol × change category) with a required reason, optional expiry and tracker link, and
  wildcards rejected. The shape a mature CI allowlist takes.

### Mistakes to avoid

| oagen behavior | Morphic decision |
|---|---|
| Models keyed by *derived string names*; collisions resolved silently ("keep largest"), refs fixed by string rewriting. Same failure class for hoisted inline enums (name-colliding enums silently adopt the first's member set) | Stable synthetic IDs (source-pointer-derived); names are presentation metadata |
| Inline-schema naming logic duplicated across ≥6 call sites that must stay byte-identical — worse, *inferred discriminator mappings are name strings* that must reproduce the hoisting pass's naming byte-for-byte across files or they dangle | One hoisting pass, keyed by source-pointer identity; `Discriminator.Mapping` points at IDs, severing the naming coupling |
| Reference nodes bake in the *target's* kind (`enum` vs `model` ref), so no reference can be built without global knowledge of all targets — the root cause of the two-pass module-global state | One `TypeRef{Target TypeID}`; kind lives on the registry entry |
| `allOf` eagerly flattened to a field list — the inheritance relationship is lost | Composition preserved un-lowered (base + mixin provenance kept) |
| oneOf merged as *optional fields* at top-level-schema and request-body positions — even when explicitly discriminated, for argument-spreading ergonomics (variant `required` discarded); only property-level unions survive as union nodes | Unions always survive as union nodes; ergonomic flattening is a emitter plan decision |
| Pagination/envelope detection via hardcoded name lists (with a fabricated `after` cursor param when none exists); single-resource envelope unwrapping applied *destructively* — the wrapper key vanishes from the IR with no recorded path | Heuristics are injectable compiler policy, never IR semantics; unwrap decisions are recorded as `PropPath`s, never applied to the stored shape |
| Discriminator *inference* is structural (const-property across variants — sound), but its output is welded to derived names (see above); name lists appear as tie-break preference order | Structural inference marked `Inferred`; output keyed by ID |
| Failures are `console.warn`; unresolvable model refs left dangling with a warning; unrecognized schema shapes degrade to `unknown` — silently when anonymous | Typed diagnostics with severity + provenance, collected on the IR document |
| Single "primary response" privileged (all 2xx kept, but inline-model extraction and pagination classification follow whichever iterates last); media-type multiplicity collapsed by fixed priority | All responses and all content types are first-class |
| Mutable module-global parser state (non-reentrant; concurrent parses race) | Compilers are pure `(source, options) → (IR, diagnostics)` |
| Collision-cascade operation renaming: same-named ops renamed in place by appending path context — adding one endpoint can rename *existing* SDK methods | Names are presentation; identity is the ID; naming collisions are a emitter policy concern |
| Name sanitization accretes hardcoded domain word-lists (acronym sets, `Json`-fork collapse passes doing registry surgery via string rewriting) | Acronym/cleanup policy is injectable per-compiler; the registry is never edited by string rewrite |
| Catch-all `additionalProperties` smuggled as a magic-named synthetic field (collides with a real property of that name); `patternProperties` truncated to the first pattern | `AdditionalProps{Value, Key, Patterns}` is its own channel |
| `$ref`-site vs ref-target annotation merging patched ad-hoc for parameters only; model fields typed by `$ref` still lose target defaults | One documented precedence rule (use-site overrides target), applied uniformly in the compiler |

### The diff/compat/verify subsystems — identity re-derived by heuristic

oagen grew an IR-to-IR spec differ, a cross-language backwards-compat checker (per-language
extractors parse SDK source into a neutral API-surface model, diffed under per-language policy),
and a self-correcting overlay loop. Three structural lessons:

- **Everything correlates by display name, and it costs hundreds of lines of heuristics.**
  The differ matches models/operations by name, so a rename is indistinguishable from
  remove+add; the compat layer then *re-derives* identity structurally (field-set superset
  matching, enum value-set equality, Jaccard-similarity overlay matching ≥0.6). Its symbol
  model even has a dual-key design (`id` + name) intended for rename detection — but `id` is
  derived from the name, so the rename branch is dead. Cross-language change rollup finally
  reinvents a spec-level identity (`conceptualChangeId`). Morphic's pointer-derived IDs make
  rename detection a lookup; the compat experience adds one refinement — when both pointer
  *and* name churn, a third *structural* correlation tier (content-hash / field-set
  similarity, shared with the `dedup` pass) is the fallback (see ir-design.md open question 5).
- **Breaking-ness is per-language policy, not a property of a change.** The same spec change
  is breaking in PHP (param names are public API), soft-risk in Kotlin, invisible in Node.
  oagen's two-stage split — neutral change records, then `(change × language) → severity`
  policy — is the right architecture for any future Morphic diff pass.
- **The comparison plane is an extracted "SDK surface", not the IR.** Live SDK source and
  generated output are both projected into a neutral surface model and diffed there; the IR's
  roles are to generate one side and to scope the baseline. Nothing in the ~20-file compat
  subsystem needed information Morphic's IR lacks — the strongest validation the audit
  produced.

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
| Unions flattened early into `MemberN`-named wrapper conventions; unresolvable shapes fall back to opaque `UntypedNode` | Unions keep variant identity, tag mode, and open/closed semantics until a emitter lowers them |
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
the IR; they belong to emitter plan layers.

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
Smithy compiler loses nothing.
