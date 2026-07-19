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
   Only frontends are staged over time — the IR's capability surface is complete from day one,
   so shipping the OpenAPI 3.x frontend first never forces an IR schema change when TypeSpec,
   Smithy, GraphQL, AsyncAPI, or Protobuf frontends land.
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
    Tags       []string     // free-form labels (Smithy @tags, OpenAPI tag membership on types via policy)
    Sensitive  bool         // whole-type redaction (Smithy @sensitive on shapes); Property.Secret is the per-use form
    Deprecation *Deprecation
    Availability *Availability
    Usage      UsageFlags   // computed by a pass: Input | Output | Error | Multipart | …
    Instantiation *TemplateInstantiation // provenance for monomorphized generics (TypeSpec templates):
                                         // {Template string; Args []TypeRef} — naming hint + cross-version identity
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
    Base           *TypeRef        // declared single inheritance (TypeSpec extends, allOf-as-inheritance)
    Implements     []TypeRef       // interface conformance, N-ary (GraphQL `implements A & B`); targets are Abstract models
    Mixins         []TypeRef       // composition without subtyping (Smithy mixins, TypeSpec spread provenance, extra allOf entries)
    AdditionalProps *AdditionalProps // map-like catch-all alongside declared properties
    Abstract       bool            // cannot be instantiated directly; fields may be typed by it and
                                   // resolve to a conforming concrete model (GraphQL interface types)
    Discriminator  *Discriminator  // set on the polymorphic base
    DiscriminatorValue string      // set on each subtype: its wire tag value
    InputOnly      bool            // GraphQL input types; distinct identity from output types
}

type AdditionalProps struct {
    Value    TypeRef
    Key      *TypeRef       // nil = string keys
    Patterns []PatternProps // key-pattern-scoped value schemas (JSON Schema patternProperties)
}
type PatternProps struct { Pattern string; Value TypeRef }

type Discriminator struct {
    Property PropID                // which property carries the tag
    Mapping  map[string]TypeID     // wire value → subtype; nil mapping = infer by type name
    Envelope string                // "" = tag inline in the variant object;
                                   // "object" = {kind, value} wrapper (TypeSpec @discriminated envelope)
    Inferred bool                  // discovered heuristically (const-property detection), not declared
}
```

**Composition semantics.** `Properties` contains only the model's *own* properties. Consumers
walk `Base`, `Implements`, and `Mixins` for the full shape (a provided `FlattenedProperties()`
traversal helper does this; the flattening is computed, never stored). This preserves what oagen
and Kiota lose: the difference between "inherits from", "conforms to", "mixes in", and
"declares". `Base` is single (subtype identity); `Implements` is N-ary conformance to `Abstract`
models — the distinction matters because GraphQL allows multiple interfaces while every
class-based target language allows at most one base class.

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

### 4.7 Validation-only schema constructs — a documented boundary

JSON Schema's `not`, `if`/`then`/`else`, and `dependentSchemas` express *validation logic*, not
data shape; no target language's type system represents them, and none of the other six formats
has an equivalent. The IR deliberately does not model them structurally. Frontends preserve them
verbatim in `Extensions` (`openapi:not`, `openapi:if-then-else`, …) and emit an `info`
diagnostic, so the information is never silently lost and a future validation-oriented backend
(request validators, mock servers) can still consume them. This is the one intentional carve-out
from "lossless means structural": losslessness is satisfied by verbatim preservation, and the
carve-out is explicit rather than accidental.

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
    Args       []Parameter         // parameterized fields: GraphQL field arguments on any property,
                                   // at any depth — not just operation entry points. Empty elsewhere.
    Flatten    bool                // property's fields hoisted into parent on the wire (Smithy/TCGC flatten)
    EventHeader bool               // in an event-stream event model: member travels in the frame
                                   // header, not the event payload (Smithy @eventHeader)
    Secret     bool                // redact in logs/docs (TypeSpec @secret, format:password)
    XML        *XMLHints           // XML wire shape when it diverges from the JSON-implied shape
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Extensions Extensions
    Provenance Provenance
}
```

**Scope rule for `Args`:** parameterized properties are only legal on models reachable from a
GraphQL binding (the only format with per-field arguments); the validate pass rejects them
elsewhere. This keeps ordinary models pure data shapes — a model without `Args` anywhere in its
graph serializes conventionally in every backend.

### 5.2 Visibility — lifecycle sets, not booleans

```go
type Lifecycle = string // OPEN set. Canonical values: "create" | "read" | "update" | "delete" | "query".
                        // TypeSpec's new visibility system allows arbitrary user-defined visibility
                        // classes; frontends lower custom classes as "<class>:<member>" strings so
                        // nothing is dropped. Backends treat unknown classes as opaque filters.

type Visibility struct {
    Only []Lifecycle   // empty = visible in all lifecycles
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

### 5.4 XML hints

XML-capable formats (OpenAPI's `xml` object, Smithy's `xmlName`/`xmlAttribute`/`xmlNamespace`/
`xmlFlattened` traits) describe an XML wire shape that diverges from the JSON-implied one. This
is typed, not an extension, because two formats express it and backends must act on it:

```go
type XMLHints struct {
    Name      string   // element/attribute name override
    Namespace string   // namespace URI
    Prefix    string
    Attribute bool     // serialize as attribute, not element
    Wrapped   bool     // list items wrapped in a container element
    Text      bool     // value is the element's text content (Smithy httpPayload text)
}
```

---

## 6. The Values channel

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "bytes" | "list" | "object" | "ref"
    Bool   bool
    Str    string
    Num    BigVal         // arbitrary precision, decimal string form
    Bytes  []byte         // base64 in JSON form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
    Ref    *ValueRef      // reference to a declared constant: {Type TypeID; Member string}
                          // (TypeSpec enum-member defaults, references to named consts)
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
    CommonErrors []ErrorCase      // errors every operation can return (Smithy service-level errors)
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
    Properties  []Property                  // resource state fields (Smithy 2.0 resource properties)
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

    Streaming  StreamingMode      // none | client | server | bidi (derived summary of the two below)
    RequestStream  *StreamDetail  // client→server streaming semantics, when present
    ResponseStream *StreamDetail  // server→client streaming semantics, when present
    Pagination *Pagination
    LongRunning *LongRunning
    Idempotency Idempotency       // unknown | safe | idempotent | idempotency_token(param)
                                  // safe = no side effects (Smithy @readonly, HTTP GET semantics)
    Auth       []AuthRequirement  // override of service default; empty slice ≠ nil (empty = explicitly public)
    Tags       []string
    OverloadOf *OpID              // TypeSpec @overload grouping

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
    Encoding  map[string]PartEncoding // multipart/form: per-property (part) wire config, keyed by PropID
    File      *FileInfo           // body is a file upload/download (TypeSpec file bodies, binary payloads)
    Examples  []Example
}

type PartEncoding struct {
    ContentTypes []string          // media type(s) of this part
    Headers      []Property        // per-part headers
    Multi        bool              // part repeats (array member → repeated parts)
    Filename     bool              // part carries a filename (file part)
    Style        string            // form-style serialization for non-file parts
    Explode      *bool
}
// TypeSpec tuple-form multipart lowers to a synthesized model whose properties are the parts.

type FileInfo struct { IsText bool; FilenameParam string }

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
    PollingOperation *OpID     // declared poll op (TypeSpec @pollingOperation)
    FinalOperation   *OpID     // declared final-result op (TypeSpec @finalOperation)
    PollingType   *TypeRef
    FinalType     *TypeRef
    ResultPath    *PropPath
}

type StreamingMode string      // "none" | "client" | "server" | "bidi"

type StreamDetail struct {
    Events  *TypeRef   // stream element type when it differs from the payload content type —
                       // for event streams this is a WireTagged Union of event models; within an
                       // event model, Property.EventHeader marks frame-header members and the
                       // remaining members form the event payload (Smithy @eventHeader/@eventPayload)
    Initial *TypeRef   // initial-request / initial-response message preceding the stream
                       // (Smithy event stream initial messages); nil = none
}
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
    HostPrefix  string             // endpoint host prefix, may contain {param} labels (Smithy @endpoint)
    ParamBindings []HTTPParamBinding
    RequestContentTypes  []string  // priority-ordered
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    IsWebhook   bool               // OpenAPI 3.1 webhooks: direction is inbound
    Callbacks   []Callback         // out-of-band operations keyed by runtime expressions
}

type HTTPParamBinding struct {
    Param      string             // Operation.Params name it binds
    Location   HTTPLocation       // path | query | header | cookie | body | body_property | host
                                  // host = param fills a HostPrefix label (Smithy @hostLabel)
    WireName   string
    Style      string             // simple | form | label | matrix | deepObject | pipe/space-delimited
    Explode    *bool
    AllowReserved bool
    Prefix     string             // map-typed param spread as prefixed wire entries:
                                  // prefixed headers (Smithy @httpPrefixHeaders) or catch-all
                                  // query maps (@httpQueryParams with Prefix "")
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

GraphQL lowering: schema types → models (`InputOnly` for inputs), interfaces → `Abstract`
models with implementors listing them in `Implements`, unions → `WireTagged` unions
discriminated by `__typename`, entry-point fields → operations (field args → `Params`, field
type → response), nested field arguments → `Property.Args` at any depth. Arbitrary
client-composed selection sets are out of scope by design: Morphic generates SDK surface, and
the full type graph — including per-field arguments — is retained so a backend can still offer
query builders.

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
    RenamedFrom     []VersionedName // {Version, Name}
    TypeChangedFrom []VersionedType // {Version, Type TypeRef} — property/return type changed
                                    // across versions (TypeSpec @typeChangedFrom)
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
| **OpenAPI 3.x** | components/schemas → registry (IDs from pointers); inline schemas hoisted with hints; `allOf` → Base/Mixins per §4.3; `oneOf`/`anyOf` → Union (Exclusive bit), null-variant → Nullable ref; `discriminator` → Discriminator; `nullable`/type-arrays → Nullable; readOnly/writeOnly → Visibility; parameters → Params + HTTPBinding locations w/ style/explode; requestBody/responses all content types → Payload.Contents; per-status responses/default → Conditions + ranges; webhooks → MessageBinding or HTTPBinding.IsWebhook; callbacks → Callbacks; links → extensions (promotable later); securitySchemes/security → Auth OR-of-ANDs; servers+variables → Servers; `xml` object → XMLHints; `not`/`if-then-else`/`dependentSchemas` → verbatim Extensions per §4.7; `patternProperties` → AdditionalProps.Patterns; `x-*` → namespaced Extensions; pagination only via injectable policy, marked Inferred |
| **Swagger 2.0** | lifted to OpenAPI 3.x shape first (body/formData → Payload; host/basePath/schemes → Servers; consumes/produces → content types), then the OpenAPI lowering runs |
| **TypeSpec** | consumed post-check (monomorphized, `isFinished`); template instances → TypeCommon.Instantiation; models → Model w/ Base + spread provenance → Mixins; scalars → Scalar chains; `@encode` → Encoding triple; unions w/ named variants → Union, `@discriminated` envelope → Discriminator.Envelope; `| null` → Nullable; visibility enums (incl. custom classes) → Visibility; interfaces → OperationGroups; `@overload` → OverloadOf; `@service` → Service; versioning decorators incl. `@typeChangedFrom` → Availability timeline; `@list`/`@pageItems` paths → Pagination PropPaths; `@pollingOperation`/`@finalOperation` → LongRunning; multipart w/ parts → Content.Encoding/PartEncoding, file bodies → FileInfo; `@error` → UsageFlags.Error; values/consts incl. enum-member refs → Values channel |
| **Smithy 2.0** | structures → Model, mixins → Mixins; `document` → Any; unions → WireTagged Union; enum/intEnum → Enum (open by default); `@sparse` → element Nullable; traits: constraints → Constraints, `@paginated` → Pagination (declared), `@retryable` → ErrorCase.Retryable, `@readonly` → Idempotency safe, `@idempotent`/`@idempotencyToken` → Idempotency, `@sensitive` → Sensitive/Secret, `@tags` → Tags; `@streaming` blob → StreamDetail; event streams → StreamDetail.Events union + Property.EventHeader + Initial messages; service-level errors → Service.CommonErrors; resources → OperationGroup + ResourceInfo (identifiers, properties, lifecycle map); http traits → HTTPBinding incl. `@endpoint`/`@hostLabel` → HostPrefix/host location, `@httpPrefixHeaders`/`@httpQueryParams` → Prefix bindings; `@jsonName` → WireName; xml traits → XMLHints; other traits → namespaced Extensions |
| **GraphQL** | §8.4; interfaces → Abstract models + Implements; field arguments → Property.Args; input objects → `InputOnly` models; non-null wrapping → Required/Nullable; deprecation w/ reason; custom scalars → Scalar; directives → Extensions |
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
5. **Dual identity for cross-revision correlation** — pointer-derived `TypeID`s are stable across
   runs but churn when a schema moves within its source file. TCGC solves cross-version
   correlation with name-based `crossLanguageDefinitionId`s. Likely resolution: keep pointer IDs
   as the structural identity and add a derived, name-based correlation key computed by a pass —
   decide once IR diffing between spec revisions is actually built.
6. **Response discrimination placement** — `ResponseConditions` (status ranges) is HTTP-shaped
   living on the neutral core. Alternatives: move conditions into `HTTPBinding` alongside
   parameter locations, leaving responses as an ordered named list. Revisit when the RPC or
   messaging frontend lands and either validates or strains the current shape.
