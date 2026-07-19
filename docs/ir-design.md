# Morphic IR — Model Design

The intermediate representation is the single contract between spec frontends and generator
backends. This document specifies every node in the model, the semantics of the hard cases, and
how each source format lowers into it. The pipeline around it is defined in
[`architecture.md`](./architecture.md); the capability analysis it is designed against is
[`ir-spec-matrix.md`](./ir-spec-matrix.md); the evidence base is [`prior-art.md`](./prior-art.md).

Type sketches are written as Go because the implementation will be Go, but this document is the
spec — field names and shapes here are normative, receiver methods and helpers are not.

---

## 1. Design rules

1. **Union of capabilities.** The IR represents everything any supported spec can express
   (`ir-spec-matrix.md`). A backend may ignore a capability; a frontend must never drop one.
2. **Un-lowered.** Composition, unions, visibility, discriminators, encodings, and streaming stay
   in source-semantic form. Flattening and language fitting happen in backends.
3. **Stable IDs, flat registries.** All named entities live in flat, ID-keyed registries and
   reference each other by ID. No node embeds another named node. Recursive types need no special
   handling; renames never break references.
4. **Three-plus-one naming.** Every nameable carries: the original source name, an IR canonical
   name, a wire name, and (where the format has one) a numeric wire ID. Language casing is never
   stored.
5. **Serializable.** The whole document round-trips through JSON deterministically (maps are
   emitted in sorted-key order; slices preserve source order).
6. **Provenance everywhere.** Every node records where it came from and whether it was declared
   or inferred.

### Go representation of closed sums

Go has no sum types; the IR uses sealed interfaces with unexported marker methods, one concrete
struct per kind, and a `Kind()` accessor for switch-dispatch:

```go
type TypeDef interface {
    typeDef()          // sealed
    Kind() TypeKind
    Common() *TypeCommon
}
```

JSON encoding of sums uses an adjacent `kind` tag (`{"kind": "model", ...}`). Exhaustiveness is
enforced by a generated `switch`-completeness test over `TypeKind` values (the oagen
`assertNever` lesson, adapted to Go).

---

## 2. Document root

```go
type Document struct {
    IRVersion   string                 // version of the IR schema itself, semver
    Name        string                 // API title
    Version     string                 // API version string (source-declared)
    Docs        Docs
    Services    []Service              // ≥1; multi-service documents are normal (TypeSpec, stitching)
    Types       map[TypeID]TypeDef     // the type registry — the only owner of TypeDefs
    Channels    map[ChannelID]Channel  // event/messaging layer (AsyncAPI, webhooks, subscriptions)
    Auth        map[AuthID]AuthScheme  // auth scheme registry
    Servers     []Server               // endpoint templates
    Versions    []string               // ordered version labels when availability metadata is used
    Extensions  Extensions
    Diagnostics []Diagnostic           // accumulated by frontend + passes; not part of API meaning
    Sources     []SourceInfo           // input files: format, path, content hash
}
```

A `Document` is self-contained: no node references anything outside it.

---

## 3. Identity, names, references

### 3.1 IDs

```go
type TypeID string      // e.g. "t/openapi/components/schemas/User" or "t/anon/paths/~1users/get/responses/200/content/application~1json"
type OpID   string      // operation identity, same construction
type ChannelID, AuthID, PropID string
```

IDs are opaque to consumers but constructed deterministically by frontends from the source
pointer of the defining occurrence, so the same input always yields the same IDs (stable
snapshots, cachable, diffable across spec revisions). IDs are never derived from display names
and never rewritten by renames. The `dedup` pass may alias two structurally identical anonymous
types; aliases are recorded so both IDs stay resolvable.

This is the direct answer to oagen's name-keyed registry (silent collision merging, string-rewrite
ref fixing) and Kiota's name-keyed children (collision reconciliation logic), and adopts the
intent of TCGC's `crossLanguageDefinitionId`.

### 3.2 Names

```go
type Naming struct {
    Source    string   // exactly as written in the spec ("user_id", "$ref name", GraphQL field)
    Canonical string   // IR-normalized identifier in neutral form: lower_snake words, no casing opinions
    Hint      string   // for anonymous types only: context-derived suggestion ("connection_domain")
}
```

`Canonical` is a cleaned word sequence, not a cased identifier — backends apply casing, acronym
policy, and reserved-word escaping. Anonymous (hoisted) types have empty `Source` and a `Hint`;
whether a backend inlines them or names them is its choice.

### 3.3 Type references

```go
type TypeRef struct {
    Target   TypeID
    Nullable bool     // this *usage* admits null on the wire
}
```

Nullability lives on the reference, not the target type, because the same type is nullable in one
position and not another. Combined with `Property.Required` this yields the four distinct states
(required/optional × nullable/non-null) that OpenAPI 3.1, TypeSpec, and GraphQL all distinguish.
Frontends normalize every source spelling to this one bit: OAS 3.0 `nullable: true`, OAS 3.1
`type: [T, "null"]`, TypeSpec `T | null`, GraphQL's absence-of-`!`, proto3 explicit presence.
A oneOf/anyOf/union whose only distinction is a null variant becomes a plain nullable `TypeRef`,
never a union node.

---

## 4. The type graph

```go
type TypeKind string // "primitive" | "scalar" | "model" | "union" | "enum" | "list" | "map" |
                     // "tuple" | "literal" | "external" | "any"

type TypeCommon struct {
    ID         TypeID
    Name       Naming
    Anonymous  bool         // hoisted inline type
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Usage      UsageFlags   // computed by a pass: Input | Output | Error | Multipart | …
    Extensions Extensions
    Provenance Provenance
}
```

### 4.1 Primitives

```go
type Primitive struct {
    TypeCommon
    Prim PrimKind
}

type PrimKind string
// bool string bytes
// int8 int16 int32 int64 uint8 uint16 uint32 uint64
// integer  (arbitrary precision — JSON Schema "integer", TypeSpec "integer")
// float32 float64
// number   (arbitrary precision — JSON Schema "number", TypeSpec "numeric")
// decimal decimal128
// date time datetime datetime_offset duration
// url uuid
// any      (unknown/JSON any — schemaless)
```

The set is the union of TypeSpec's intrinsic scalars, JSON Schema's types, and Protobuf's needs.
Protobuf's `fixed32`/`sfixed64`/`sint*` are **encodings of** `uint32`/`int64`/…, not distinct
primitives — they lower to `Encoding` (§5.3). There is no `void` type: absence of a body/return
is a `nil *TypeRef`.

### 4.2 Scalars (named restricted/extended primitives)

```go
type Scalar struct {
    TypeCommon
    Base        TypeRef      // primitive or another scalar — extension chains preserved
    Constraints *Constraints
    Encoding    *Encoding
}
```

Covers TypeSpec `scalar X extends Y`, GraphQL custom scalars, OpenAPI `type+format` pairs that a
frontend chooses to name, Smithy simple shapes with traits. Backends resolve the chain to the
nearest representable base and accumulate constraints/encoding along the way.

### 4.3 Models (structs / objects / messages)

```go
type Model struct {
    TypeCommon
    Properties     []Property
    Base           *TypeRef        // declared single inheritance (TypeSpec extends, GraphQL implements-as-base, allOf-as-inheritance)
    Mixins         []TypeRef       // composition without subtyping (Smithy mixins, TypeSpec spread provenance, extra allOf entries)
    AdditionalProps *AdditionalProps // map-like catch-all alongside declared properties
    Discriminator  *Discriminator  // set on the polymorphic base
    DiscriminatorValue string      // set on each subtype: its wire tag value
    InputOnly      bool            // GraphQL input types; distinct identity from output types
}

type AdditionalProps struct {
    Value    TypeRef
    Key      *TypeRef   // nil = string keys
}

type Discriminator struct {
    Property PropID                // which property carries the tag
    Mapping  map[string]TypeID     // wire value → subtype; nil mapping = infer by type name
    Inferred bool                  // discovered heuristically (const-property detection), not declared
}
```

**Composition semantics.** `Properties` contains only the model's *own* properties. Consumers
walk `Base` and `Mixins` for the full shape (a provided `FlattenedProperties()` traversal helper
does this; the flattening is computed, never stored). This preserves what oagen and Kiota lose:
the difference between "inherits from", "mixes in", and "declares".

How frontends classify `allOf`: an entry that is a `$ref` to a schema participating in a
discriminator hierarchy, or the sole `$ref` entry, becomes `Base`; other `$ref` entries become
`Mixins`; inline entries merge into `Properties` (with provenance). This mirrors Kiota's
decision table but keeps every branch reconstructible.

### 4.4 Unions

```go
type Union struct {
    TypeCommon
    Variants      []Variant
    Exclusive     bool           // oneOf/tagged: exactly one variant matches; anyOf: one-or-more
    WireTagged    bool           // the wire format itself encodes the variant (protobuf oneof,
                                 // Smithy union, GraphQL __typename) vs untagged JSON oneOf
    Discriminator *Discriminator // internal tag property, when one exists
}

type Variant struct {
    Name       Naming    // named variants (TypeSpec, Smithy, protobuf oneof fields); Hint-only for bare oneOf members
    Type       TypeRef
    WireID     int       // protobuf oneof field number; 0 = none
    Docs       Docs
    Deprecation *Deprecation
    Extensions Extensions
}
```

One node covers the whole design space from the capability matrix: untagged `anyOf`
(`Exclusive=false`), untagged `oneOf` (`Exclusive=true`), discriminated `oneOf`
(`+Discriminator`), natively tagged unions (`WireTagged=true`). Variant identity survives so
backends can generate named accessors, sealed interfaces, or wrapper types per their refiner
strategy. Unions never degrade to optional-field merges in the IR.

### 4.5 Enums

```go
type Enum struct {
    TypeCommon
    ValueType PrimKind      // string | int32 | int64 | float64 …
    Members   []EnumMember
    Closed    bool          // false = open/extensible: unknown values must be representable
    Flags     bool          // bitfield semantics
}

type EnumMember struct {
    Name       Naming
    Value      Value        // typed value, matches ValueType
    WireName   string       // serialized form when it differs from Value (rare) 
    Docs       Docs
    Deprecation *Deprecation
    Extensions Extensions
}
```

`Closed` defaults per source semantics: JSON Schema enums are closed, Smithy and proto3 enums are
open. Backends that can't express open enums (plain Go consts, TS string literals) lower via
their refiners — the bit must survive to that point (Kiota's string-only closed enums are the
counterexample).

### 4.6 Containers and the rest

```go
type List struct    { TypeCommon; Elem TypeRef; Constraints *Constraints } // minItems/maxItems/uniqueItems
type MapT struct    { TypeCommon; Key TypeRef; Value TypeRef }             // Record/additionalProperties-only/proto map
type Tuple struct   { TypeCommon; Elems []TypeRef }                        // prefixItems, TypeSpec tuples
type Literal struct { TypeCommon; Value Value }                            // const / single-value enum / discriminator pins
type External struct{ TypeCommon; Identity string; Package string; MinVersion string } // resolve to a well-known library type (TCGC external)
type Any struct     { TypeCommon }                                         // schemaless
```

Containers are real type nodes with IDs (hoisted like all anonymous types), not flags on
references — Kiota's `CollectionKind` flag couldn't express nested collections or constrained
lists cleanly.

---

## 5. Properties, constraints, encodings, visibility

### 5.1 Property

```go
type Property struct {
    ID         PropID
    Name       Naming
    WireName   string              // serialized name; defaults to Name.Source
    WireNameByFormat map[string]string // per-mime overrides (TypeSpec @encodedName json/xml)
    WireID     int                 // protobuf field number / thrift id; 0 = none
    Type       TypeRef
    Required   bool                // presence on the wire; orthogonal to Type.Nullable
    Visibility Visibility          // lifecycle set; zero value = visible in all
    Default    *Value
    Constraints *Constraints
    Encoding   *Encoding
    Flatten    bool                // property's fields hoisted into parent on the wire (Smithy/TCGC flatten)
    Secret     bool                // redact in logs/docs (TypeSpec @secret, format:password)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Extensions Extensions
    Provenance Provenance
}
```

### 5.2 Visibility — lifecycle sets, not booleans

```go
type Lifecycle string // "create" | "read" | "update" | "delete" | "query"

type Visibility struct {
    Only []Lifecycle   // empty = all lifecycles
}
```

`readOnly` lowers to `Only: [read]` (plus delete/query per OpenAPI semantics); `writeOnly` to
`Only: [create, update]`; GraphQL input-vs-output types to `create/update` vs `read`; TypeSpec
`@visibility` maps directly. One logical model therefore produces N wire shapes; the projection
(`ModelShape(model, lifecycle)`, with PATCH additionally making properties optional) is a
computed traversal in backends' plan layer — the IR stores the single logical model plus the
visibility facts, never the projected variants. This is TypeSpec's `MetadataInfo` design with
storage and computation split.

### 5.3 Constraints and encodings

```go
type Constraints struct {
    // numeric — arbitrary-precision decimal strings, never float64 (TypeSpec Numeric lesson)
    Min, Max           *BigVal
    ExclusiveMin, ExclusiveMax bool
    MultipleOf         *BigVal
    // string
    MinLength, MaxLength *int64
    Pattern            string      // ECMA-262 regex as written; backends translate or drop with a diagnostic
    // collections
    MinItems, MaxItems *int64
    UniqueItems        bool
    MinProps, MaxProps *int64
}

type Encoding struct {
    Name     string    // "rfc3339" | "unixTimestamp" | "base64" | "base64url" | "iso8601" |
                       // "seconds" | "http-date" | "varint" | "zigzag" | "fixed" | format strings
    WireType *TypeRef  // the on-wire primitive when it differs from the logical type
                       // (utcDateTime encoded as int32; bytes as base64 string)
}
```

The logical-type / encoding-name / wire-type triple is TCGC's reification of TypeSpec `@encode`
and also absorbs OpenAPI `format` and Protobuf's `sint*/fixed*` wire variants. Encoding attaches
at the scalar definition or overrides at the property — property wins.

---

## 6. The Values channel

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "list" | "object"
    Bool   bool
    Str    string
    Num    BigVal         // arbitrary precision, decimal string form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
}
```

`BigVal` is a decimal-string wrapper: no float64 round-tripping anywhere in the IR.

---

## 7. Service layer

### 7.1 Services and operation groups

```go
type Service struct {
    Name       Naming
    Docs       Docs
    Namespace  []string           // source namespace path (TypeSpec/Smithy/proto package)
    Groups     []OperationGroup   // hierarchical; a group ≈ TypeSpec interface / Smithy resource / tag
    Auth       []AuthRequirement  // service-level default (OR-of-ANDs, §9)
    Servers    []int              // indexes into Document.Servers scoped to this service
    Extensions Extensions
    Provenance Provenance
}

type OperationGroup struct {
    Name       Naming
    Docs       Docs
    Groups     []OperationGroup   // nesting: Smithy resources, sub-clients
    Operations []Operation
    Resource   *ResourceInfo      // Smithy resource semantics when declared
    Extensions Extensions
}

type ResourceInfo struct {
    Identifiers []Property                  // resource identity fields
    Lifecycle   map[string]OpID             // "create"|"read"|"update"|"delete"|"list" → op
}
```

OpenAPI frontends build groups from tags (policy-controllable: tag-based vs path-prefix-based);
TypeSpec from interfaces/namespaces; Smithy from resources; GraphQL yields three groups
(query/mutation/subscription); Protobuf one group per `service`.

### 7.2 Operation — the protocol-neutral core

```go
type Operation struct {
    ID         OpID
    Name       Naming
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability

    Params     []Parameter        // all logical inputs, protocol-unbound
    Request    *Payload           // body/message content, nil = none
    Responses  []Response         // ordered; success + alternative successes
    Errors     []ErrorCase        // declared failure shapes

    Streaming  StreamingMode      // none | client | server | bidi
    Pagination *Pagination
    LongRunning *LongRunning
    Idempotency Idempotency       // unknown | idempotent | idempotency_token(param)
    Auth       []AuthRequirement  // override of service default; empty slice ≠ nil (empty = explicitly public)

    Bindings   OpBindings         // §8 — how the core maps onto concrete protocols
    Examples   []Example
    Extensions Extensions
    Provenance Provenance
}

type Parameter struct {
    Name       Naming
    Type       TypeRef
    Required   bool
    Default    *Value
    Constraints *Constraints
    Docs       Docs
    Deprecation *Deprecation
    Extensions Extensions
    // NOTE: no location here — path/query/header is HTTP-binding detail (§8.1)
}

type Payload struct {
    Contents []Content            // one per media type / message schema — all kept
}

type Content struct {
    MediaType string              // "application/json", "multipart/form-data", "" for non-HTTP
    Type      TypeRef
    Encoding  map[string]PartEncoding // multipart/form field encodings
    Examples  []Example
}

type Response struct {
    Name       Naming             // for formats with named outputs; Hint elsewhere
    Conditions ResponseConditions // HTTP status codes/ranges; empty for RPC single-response
    Payload    *Payload           // nil = no body
    Headers    []Property         // response metadata fields
    Docs       Docs
}

type ResponseConditions struct {
    StatusCodes []StatusRange     // {From,To}: 200–200, 400–499 ("4XX"), 0–0 = default/catch-all
}

type ErrorCase struct {
    Type       TypeRef            // an error-flagged model
    Conditions ResponseConditions
    Retryable  *bool              // Smithy @retryable; nil = unknown
    Docs       Docs
}
```

Design notes:

- **Parameters carry no protocol location.** The same logical operation is bindable to HTTP
  (locations assigned in the binding), to gRPC (all params fold into a request message), or to a
  channel message. This is the split TCGC makes between `ServiceMethod` and `InputOperation`,
  turned inside out: Morphic stores the neutral core and each binding, and backends derive the
  per-protocol view.
- **All responses and all content types survive** (oagen's "primary response" collapse is the
  counterexample). The plan layer picks a primary for SDK ergonomics; the IR doesn't.
- **Errors reference error models** (`Model` with `UsageFlags.Error`), with status conditions and
  range collapsing exactly as Kiota's error mappings (`404`, `4XX`, catch-all).

### 7.3 Pagination, long-running, streaming

```go
type Pagination struct {
    Strategy   PageStrategy   // cursor | offset | page | link_header | next_link | token
    Inferred   bool           // heuristic (OpenAPI name-matching policy) vs declared (Smithy/TypeSpec)
    InputCursor  *ParamPath   // which input continues iteration
    InputLimit   *ParamPath
    Items        *PropPath    // where result items live in the response — a PATH, not a name
    NextCursor   *PropPath    // continuation source in the response
    NextLink     *PropPath
    TotalCount   *PropPath
}

type PropPath struct{ Root TypeRef; Segments []PropID } // survives envelope nesting (TypeSpec paging lesson)
type ParamPath struct{ Param string; Segments []PropID }

type LongRunning struct {
    FinalStateVia string       // "operation-location" | "status-monitor" | "original-uri" | …
    PollingType   *TypeRef
    FinalType     *TypeRef
    ResultPath    *PropPath
}

type StreamingMode string      // "none" | "client" | "server" | "bidi"
```

Streaming mode is on the operation core because it is protocol-independent (gRPC streams, SSE,
WebSocket, Smithy event streams all project onto it); the wire mechanism (SSE vs chunked vs
WS frames vs length-prefixed events) lives in the binding.

---

## 8. Protocol bindings

```go
type OpBindings struct {
    HTTP    *HTTPBinding      // OpenAPI/Swagger ops, TypeSpec @route, Smithy http traits
    RPC     *RPCBinding       // Protobuf/gRPC, Smithy RPC protocols
    Message *MessageBinding   // AsyncAPI operations, GraphQL subscriptions, webhooks
    GraphQL *GraphQLBinding   // query/mutation/subscription field binding
}
```

An operation has at least one binding; more than one is legal (Smithy service exposed over both
HTTP and RPC; gRPC with HTTP transcoding).

### 8.1 HTTP

```go
type HTTPBinding struct {
    Method      string             // GET, POST, … uppercase
    URITemplate string             // RFC 6570 — the one true path representation (Kiota + TypeSpec agree)
    ParamBindings []HTTPParamBinding
    RequestContentTypes  []string  // priority-ordered
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    IsWebhook   bool               // OpenAPI 3.1 webhooks: direction is inbound
    Callbacks   []Callback         // out-of-band operations keyed by runtime expressions
}

type HTTPParamBinding struct {
    Param      string             // Operation.Params name it binds
    Location   HTTPLocation       // path | query | header | cookie | body | body_property
    WireName   string
    Style      string             // simple | form | label | matrix | deepObject | pipe/space-delimited
    Explode    *bool
    AllowReserved bool
    ContentType string            // param serialized as a media type (OpenAPI content-style params)
    BodyPath   []PropID           // for body_property: where in the body model it lands (TypeSpec HttpProperty)
}

type Callback struct { Expression string; Operations []OpID }
```

Every logical parameter is bound exactly once (validated). TypeSpec's `HttpProperty` role+path
flattening is the model here: nested `@header`/`@body` annotations resolve to explicit
`(role, wire name, path)` bindings rather than restructured models.

### 8.2 RPC

```go
type RPCBinding struct {
    System      string     // "grpc" | "smithy-rpc" | "connect" | …
    FullMethod  string     // "/pkg.Service/Method"
    InputType   *TypeRef   // the request message type params fold into (nil = synthesize from Params)
    IdempotencyLevel string
}
```

### 8.3 Messaging / events

```go
type MessageBinding struct {
    Channel   ChannelID
    Direction MsgDirection     // send | receive (application perspective, AsyncAPI 3 semantics)
    Messages  []MessageRef     // which of the channel's messages this operation uses
    ReplyTo   *ChannelID
}

type Channel struct {
    ID        ChannelID
    Name      Naming
    Address   string             // topic/routing key/path, may contain {params}
    Params    []Parameter
    Messages  []Message
    Servers   []int
    Bindings  map[string]Extensions // protocol-specific ("kafka", "amqp", "ws", "mqtt") kept as namespaced raw config
    Extensions Extensions
    Provenance Provenance
}

type Message struct {
    Name        Naming
    Payload     Payload
    Headers     []Property
    CorrelationID *PropPath
    ContentType string
    Docs        Docs
    Extensions  Extensions
}
```

Kafka/AMQP/MQTT binding minutiae stay as namespaced raw extension config rather than modeled
structs — they are protocol deployment detail, not API shape, and modeling them structurally
would chase every AsyncAPI binding spec revision.

### 8.4 GraphQL

```go
type GraphQLBinding struct {
    Kind      string    // "query" | "mutation" | "subscription"
    FieldPath []string  // entry-point field (nesting for namespaced schemas)
}
```

GraphQL lowering: schema types → models (`InputOnly` for inputs), interfaces → `Base` +
implementors, unions → `WireTagged` unions discriminated by `__typename`, entry-point fields →
operations (field args → `Params`, field type → response). Arbitrary client-composed selection
sets are out of scope by design: Morphic generates SDK surface, and the full type graph is
retained so a backend can still offer query builders.

---

## 9. Auth

```go
type AuthScheme struct {
    ID   AuthID
    Name Naming
    Kind AuthKind        // apiKey | http_basic | http_bearer | oauth2 | openid_connect | mutual_tls | custom
    // apiKey
    In       string      // header | query | cookie
    KeyName  string
    // http
    Scheme       string  // bearer, basic, digest…
    BearerFormat string
    // oauth2
    Flows []OAuthFlow    // {Kind: authorization_code|client_credentials|implicit|password|device,
                         //  AuthorizationURL, TokenURL, RefreshURL, Scopes map[string]string}
    // openid
    OpenIDConnectURL string
    Extensions Extensions
}

type AuthRequirement struct {          // one *option*
    Schemes []SchemeUse                // ALL must be satisfied together
}
type SchemeUse struct { Scheme AuthID; Scopes []string }
// []AuthRequirement on a service/operation = OR across options (TypeSpec/OpenAPI security semantics)
```

## 10. Servers

```go
type Server struct {
    URLTemplate string                  // may contain {variables}
    Description Docs
    Variables   []ServerVariable        // {Name, Default, Enum []string, Docs}
    Protocol    string                  // "https" default; "kafka", "wss", … for messaging servers
    Extensions  Extensions
}
```

## 11. Versioning & availability

```go
type Availability struct {
    Added      string   // version label from Document.Versions
    Removed    string
    Deprecated string
    RenamedFrom []VersionedName // {Version, Name}
}
```

The IR stores the **timeline** (TypeSpec model); the `version-slice` pass produces a concrete
snapshot document per version for consumption. Backends always receive snapshots — they never
interpret availability themselves. Formats without versioning semantics simply leave this nil.

## 12. Docs, deprecation, examples, extensions

```go
type Docs struct {
    Summary     string
    Description string        // CommonMark; may contain {t:TypeID} cross-reference tokens that
                              // backends resolve to language-appropriate links (Kiota's doc templates)
    ExternalDocs []Link       // {URL, Description}
}

type Deprecation struct { Message, Since, RemovalVersion string }

type Example struct { Name string; Summary string; Value *Value; ExternalURL string }

type Extensions map[string]RawValue
// keys are namespaced by origin: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "typespec:@myOrg/decorator", "asyncapi:bindings.kafka"
// RawValue is the source JSON preserved verbatim.
```

Extensions are the lossless escape hatch: any source metadata without a first-class IR node
survives, namespaced so two formats' extensions never collide, and typed passes can promote
well-known extensions (`x-ms-pagination` → `Pagination`) without losing the original.

## 13. Provenance & diagnostics

```go
type Provenance struct {
    Source   int       // index into Document.Sources
    Pointer  string    // JSON pointer or line:col into that source
    Inferred string    // "" = declared; else the heuristic that produced this node ("pagination-name-match")
}

type Diagnostic struct {
    Severity   Severity  // error | warning | info
    Code       string    // stable: "openapi/unresolved-ref", "ir/dangling-type-ref"
    Message    string
    Provenance Provenance
}
```

Everything heuristic is auditable; everything broken is reportable with an exact source location.

---

## 14. Frontend lowering summaries

How each format's distinctive concepts land in the IR (full details live with each frontend):

| Format | Lowering highlights |
|---|---|
| **OpenAPI 3.x** | components/schemas → registry (IDs from pointers); inline schemas hoisted with hints; `allOf` → Base/Mixins per §4.3; `oneOf`/`anyOf` → Union (Exclusive bit), null-variant → Nullable ref; `discriminator` → Discriminator; `nullable`/type-arrays → Nullable; readOnly/writeOnly → Visibility; parameters → Params + HTTPBinding locations w/ style/explode; requestBody/responses all content types → Payload.Contents; per-status responses/default → Conditions + ranges; webhooks → MessageBinding or HTTPBinding.IsWebhook; callbacks → Callbacks; links → extensions (promotable later); securitySchemes/security → Auth OR-of-ANDs; servers+variables → Servers; `x-*` → namespaced Extensions; pagination only via injectable policy, marked Inferred |
| **Swagger 2.0** | lifted to OpenAPI 3.x shape first (body/formData → Payload; host/basePath/schemes → Servers; consumes/produces → content types), then the OpenAPI lowering runs |
| **TypeSpec** | consumed post-check (monomorphized, `isFinished`); models → Model w/ Base + spread provenance → Mixins; scalars → Scalar chains; `@encode` → Encoding triple; unions w/ named variants → Union; `| null` → Nullable; visibility enums → Visibility; interfaces → OperationGroups; `@service` → Service; versioning decorators → Availability timeline; `@list`/`@pageItems` paths → Pagination PropPaths; `@error` → UsageFlags.Error; values/consts → Values channel |
| **Smithy 2.0** | structures → Model, mixins → Mixins; unions → WireTagged Union; enum/intEnum → Enum (open by default); traits: constraints → Constraints, `@paginated` → Pagination (declared), `@retryable` → ErrorCase.Retryable, `@idempotencyToken` → Idempotency, `@streaming`/eventstreams → StreamingMode + MessageBinding; resources → OperationGroup + ResourceInfo lifecycle map; http traits → HTTPBinding; other traits → namespaced Extensions |
| **GraphQL** | §8.4; input objects → `InputOnly` models; non-null wrapping → Required/Nullable; deprecation w/ reason; custom scalars → Scalar; directives → Extensions |
| **AsyncAPI** | servers w/ protocols → Servers; channels/messages → Channels/Messages; operations send/receive → Operation + MessageBinding; correlation IDs → PropPath; protocol bindings → Channel.Bindings raw; message payload schemas share the same JSON-Schema lowering as OpenAPI |
| **Protobuf** | messages → Model w/ WireIDs; oneof → WireTagged Exclusive Union w/ variant WireIDs; enums → open Enum w/ int32 values; map/repeated → MapT/List; scalar wire variants (sint/fixed) → Encoding; services/rpcs → Service/Operation + RPCBinding; streaming modifiers → StreamingMode; options → Extensions; reserved ranges → Extensions (guarded by validate pass) |

## 15. Deliberate exclusions

- **Language names/casings** — backends own identifier rendering entirely (IR stores neutral
  word sequences + wire names).
- **SDK runtime policy** (retry/timeout/telemetry/error-class taxonomy) — a separate backend
  input, never IR (§2.4 of architecture.md).
- **Generator plan artifacts** (request builders, executor pairs, per-visibility model variants,
  primary-response selection) — computed views in backends, not stored.
- **Structured modeling of transport-deployment minutiae** (Kafka partition configs, AMQP
  exchange args) — preserved as raw namespaced extensions.
- **Arbitrary GraphQL persisted queries** — the type graph and entry points are retained; query
  composition is a backend/runtime feature.

## 16. Open questions (tracked for implementation)

1. **BigVal representation** — decimal string wrapper vs `math/big` types at the boundary;
   leaning decimal-string in the IR, `math/big` in helpers.
2. **Dedup aliasing mechanics** — alias table on Document vs ID rewriting with a redirect map;
   leaning alias table (IDs immutable, always).
3. **Content negotiation in the plan layer** — the IR keeps all media types; the default
   primary-selection policy (json > form > multipart > binary?) belongs to backends but should be
   shared as a standard plan helper.
4. **Whether `UsageFlags` is stored or always recomputed** — leaning: computed by a standard pass
   and stored, since backends all need it and it's expensive to derive.
5. **XML serialization metadata** (OpenAPI `xml` object) — likely a typed `XMLHints` on Property
   rather than extensions, needed before any XML-heavy corpus lands.
