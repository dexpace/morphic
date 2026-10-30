# Planned lowerings

The lowering each format without a compiler is planned to follow, and where each format with one
states its rules.

A format whose compiler exists states its lowering rules in a specification beside it, under
`docs/compilers/`, one rule per entry; the [IR design](../ir/README.md) keeps the shapes they
lower into. A format whose compiler is still to come keeps its planned lowering here, as a list of
highlights, and the list moves to a specification of its own when the compiler lands.

| Format | Lowering rules |
|---|---|
| **OpenAPI 3.x** | [`compilers/openapi.md`](../compilers/openapi.md) |
| **Swagger 2.0** | lifted inside the OpenAPI compiler: [`swagger-lift`](../compilers/openapi.md#swagger-lift) |

## TypeSpec

- consumed post-check (monomorphized, `isFinished`)
- template instances → TypeCommon.Instantiation incl. value args → TemplateArg
- models → Model w/ Base + spread provenance → Mixins
- `Record<T>` → MapT, and a `Record` spread or extended beside properties → AdditionalProps
- scalars → Scalar chains, constructors in values → Value.Ctor
- `@encode`/`@format` → Encoding triple
- `@encodedName` → WireNameByFormat at property AND type level
- unions w/ named variants → Union, `@discriminated` → WireTagged +
  Discriminator.PropertyName/Envelope/EnvelopeValueName
- `| null` → Nullable
- visibility classes (incl. custom, `@invisible` → Visibility.None) → Visibility, op overrides →
  ParameterVisibility/ReturnTypeVisibility
- `@patch` implicitOptionality → HTTPBinding.PatchImplicitOptionality
- interfaces → OperationGroups (versionable)
- `@overload` → OverloadOf
- `@sharedRoute` → SharedRoute
- `@service` → Service
- versioning decorators incl. `@typeChangedFrom`/`@madeOptional`/`@madeRequired` and add/remove
  cycles → Availability timeline (on members/variants/params too)
- pagination decorators incl. prev/first/last links and header continuation tokens → Pagination
  PropPaths (In:"header")
- Azure.Core `@pollingOperation`/`@finalOperation` → LongRunning
- multipart w/ parts → Content.Encoding/PartEncoding, `Http.File` → FileInfo (content-type set,
  contents chain, filename location)
- streams/SSE → StreamDetail + Variant.Event (contentType, terminal)
- `@error` → the response becomes an `ErrorCase`, whose payload `irfacts.Index.Usage` marks
  `UsageError`
- `@example`/`@opExample` → Examples (Input/Output pairs)
- `@pattern` message → Constraints.PatternMessage
- `@mediaTypeHint` → TypeCommon.MediaTypeHint
- `never` members deleted + diagnostic per
  [boundaries](../ir/boundaries.md#degraded-constructs)
- tuples → Tuple with `Constraints.MinItems = len(Elems)` and no Rest, since a TypeSpec tuple
  requires every position and admits nothing past them
- TCGC client-shaping decorators (`@clientName`, `@access`, `@usage`, `@scope`, `@override`, …) →
  namespaced Unmodeled (`out_of_scope`) consumed by emitter policy, never IR semantics
- values/consts incl. enum-member refs → Values channel

## Smithy 2.0

- structures → Model, mixins → Mixins (non-structure mixins flattened — spec-sanctioned)
- `document` → Any
- map shapes → MapT (key shape → Key, nil for `smithy.api#String`, `@length` → Constraints)
- unions → WireTagged Union, member `@jsonName` → Variant.WireName
- enum/intEnum → Enum (open by default)
- `@sparse` → element Nullable
- traits: constraints → Constraints, `@paginated` → Pagination (declared), `@retryable` →
  ErrorCase.Retryable + Throttling, `@error` fault → ErrorCase.Fault, `@readonly` → Idempotency
  safe, `@idempotent`/`@idempotencyToken` → Idempotency, `@sensitive` → Sensitive/Secret, `@tags` →
  Tags, `@clientOptional`/`@input` → Property.ClientOptional (+InputOnly), `@addedDefault` →
  DefaultAdded, root-shape `@default` pushed down to properties w/ provenance
- `@streaming` blob → StreamDetail (+`@requiresLength` → RequiresLength)
- event streams → StreamDetail.Events union + Property.EventHeader/EventPayload + Initial messages
- service-level errors → Service.CommonErrors
- protocol traits → Service.Protocols
- service `rename`/`version` → Service.Renames/Version
- resources → OperationGroup + ResourceInfo (identifiers, properties, lifecycle incl.
  put/@noReplace, instance vs collection ops)
- http traits → HTTPBinding incl. `@endpoint`/`@hostLabel` → HostPrefix/host location (additive
  binding), `@httpPrefixHeaders`/`@httpQueryParams` → Prefix bindings, `@httpResponseCode` →
  Response.StatusCodeProp, `@requestCompression` → Compression, `@httpChecksumRequired` →
  ChecksumRequired
- `@auth` order → priority-ordered Auth, `@optionalAuth` → empty option
- `@jsonName` → WireName
- `@mediaType` → Encoding.MediaType
- xml traits → XMLHints at type and property level
- `@examples` → Examples (Input/Output/Error)
- waiters + rules-engine traits → verbatim Unmodeled (`out_of_scope`,
  [boundaries](../ir/boundaries.md#deliberate-exclusions))
- `smithy.api#Unit` → nil payload / shared empty Model for tag-only variants
- other traits → namespaced Unmodeled

## GraphQL

- [GraphQL binding](../ir/graphql-binding.md)
- interfaces (incl. interface hierarchies) → Abstract models + Implements
- field arguments → Property.Args
- input objects → `InputOnly` models, `@oneOf` inputs → WireTagged Exclusive Union
- non-null wrapping → Required/Nullable (all `[T!]!` combinations via per-layer list nodes)
- defaults on args AND input fields → Default (list-input coercion normalized)
- custom scalars → Scalar{Base: nil}, `@specifiedBy` → Unmodeled
- deprecation w/ reason at every location incl. args/input fields
- directives → ordered-array Unmodeled convention + document-level definitions

## AsyncAPI

- servers w/ protocols/protocolVersion/security → Servers (named, w/ Auth)
- channels → Channels (Address nil = unknown), messages → Document.Messages registry referenced by
  ID; a channel's `servers` → Channel.Servers, and one naming none lists every server, which
  AsyncAPI reads it as available on ([servers](../ir/servers.md))
- operations send/receive → Operation + MessageBinding, no-reply sends → OneWay
- reply objects → Reply (static channel + dynamic PropPath address + message set)
- correlation IDs → PropPath (In: header|payload)
- message headers → hoisted header model (Headers *TypeRef)
- message examples → Example{Headers, Value} pairs
- multi-format payload schemas → Content.SchemaFormat + verbatim schema in Unmodeled (Avro payloads
  lower through the type graph: record→Model, map→MapT, enum→closed Enum w/ FallbackMember,
  fixed→bytes w/ length, decimal→Constraints.Precision/Scale, aliases→Naming.Aliases)
- parameter `location` → Parameter.ValueFrom
- traits applied by merge w/ provenance
- protocol bindings at server/channel/operation/message level → raw Bindings maps
- payload JSON-Schema lowering shared with OpenAPI

## Protobuf

- messages → Model w/ WireIDs
- oneof → WireTagged Exclusive Union w/ variant WireIDs + Flatten on the synthetic wrapper property
  (oneof members are top-level wire fields; synthetic oneofs from proto3 `optional` do NOT lower to
  unions — they are presence markers)
- presence disciplines → Property.Presence (never Nullable)
- enums → Enum open/closed per syntax/edition, allow_alias → duplicate member values
- map/repeated → MapT/List (a `string` map key → nil Key; packed/expanded → List.Encoding)
- groups / editions DELIMITED → nested Model + Encoding "delimited"
- scalar wire variants (sint/fixed) → Encoding
- `extend` fields → Property.ExtensionOf, extension ranges → Model.ExtensionRanges
- well-known types → External (wrapper types → External or nullable primitive per injectable policy)
- custom options → namespaced Unmodeled (proto-JSON), file options keyed by path in
  Document.Unmodeled
- services/rpcs → Service/Operation + RPCBinding
- streaming modifiers → StreamingMode
- gRPC transcoding (google.api.http) → additional HTTPBinding entries (slice), path patterns →
  PathPattern, nested bindings → ParamPath, response_body → ResponseBodyPath
- reserved ranges → Unmodeled (guarded by validate pass)

## Erlang/OTP

- [Erlang/OTP binding](../ir/otp-binding.md)
- `-type`/`-spec` type language → type graph: tuples → Tuple with
  `Constraints.MinItems = len(Elems)` and no Rest, `tuple()` of any arity → Tuple with no Elems and
  a Rest of the top type, tagged-tuple unions → WireTagged Union + Discriminator.Index, records →
  Model{Positional} w/ 1-based WireIDs, atoms → symbol Values / Literal types /
  `Scalar{Base: string, Encoding: "erlang:atom"}`, integer ranges → integer Scalar + Min/Max, maps
  `:=`/`=>` → Properties(Required)/AdditionalProps w/ typed keys, a map of `=>` associations alone →
  MapT, `pid()`/`port()`/`reference()`/`fun()` → External, `string()` → List of char scalar,
  parametrized types monomorphized w/ Instantiation
- behaviours: gen_server call/cast → Operations + OTPBinding (cast → OneWay), info → channel
  Messages received, gen_event → Channel w/ N messages, gen_statem states →
  `Unmodeled["otp:states"]`
- registered process → Channel.Address, registration kind → Channel.Bindings["otp"]
- reply-union Errors split → injectable Inferred policy
- bit-sized binaries/map assoc lists/`-opaque` →
  [boundaries](../ir/boundaries.md#degraded-constructs) degraded lowerings
- `-deprecated` → Deprecation
- `-doc`/EDoc → Docs
