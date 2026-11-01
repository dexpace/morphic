# Operations

Part of the [IR design](./README.md). The protocol-neutral core of an operation and its parameters.
A body is in [payloads](./payloads.md), an outcome in [responses](./responses.md), and how the core
reaches a protocol in [bindings](./bindings.md).

```go
type Operation struct {
    ID         OpID
    Name       Naming
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Stability  Stability          // alpha | beta | stable; "" = none stated, never read as stable
                                  // (see "Stability" below)

    Params     []Parameter        // all logical inputs, protocol-unbound
    Request    *Payload           // body/message content, nil = none
    Responses  []Response         // ordered; success + alternative successes
    Errors     []ErrorCase        // declared failure shapes
    Default    *Response          // the response for every outcome no entry of Responses or Errors
                                  // names, which the source classes neither a success nor a failure
                                  // (OpenAPI `default`); Conditions hold the catch-all alone;
                                  // nil = none

    OneWay     bool               // fire-and-forget: no response EVER exists (OTP cast, AsyncAPI
                                  // send-without-reply, Thrift oneway, JSON-RPC notifications).
                                  // Distinct from a response with no body (an ack still exists);
                                  // validate rejects OneWay with any response, Default included
    Streaming  StreamingMode      // none | client | server | bidi (derived summary of the two below)
    RequestStream  *StreamDetail  // client→server streaming semantics, when present
    ResponseStream *StreamDetail  // server→client streaming semantics, when present
    Pagination *Pagination
    LongRunning *LongRunning
    Idempotency Idempotency       // unknown | safe | idempotent | idempotency_token(param)
                                  // safe = no side effects (Smithy @readonly, HTTP GET semantics)
                                  // TokenParam is set exactly for idempotency_token; validate
                                  // rejects either half alone (pass/idempotency-token-mismatch)
    Servers    []ServerID         // the servers it is sent to, most preferred first, in place
                                  // of its service's; empty = its service's (docs/ir/servers.md)
    Auth       []AuthRequirement  // override of service default; empty slice ≠ nil (empty = explicitly public)
    Tags       []string
    ParameterVisibility  []Lifecycle // visibility filter override for the request view
                                     // (TypeSpec @parameterVisibility); nil = protocol default
    ReturnTypeVisibility []Lifecycle // filter override for the response view; nil = protocol default
    OverloadOf *OpID              // TypeSpec @overload grouping

    Bindings   OpBindings         // docs/ir/bindings.md — how the core maps onto concrete protocols
    Examples   []Example
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Parameter struct {
    ID         ParamID             // param/<space>/<operation's path>/parameters/<name>/<in>, and
                                   // /<n> after it on the n-th holder of one name and location;
                                   // the bindings, pagination inputs and idempotency token name a
                                   // parameter by it, not by Name, which two locations may share
    Name       Naming
    Type       TypeRef
    Required   bool
    Default    *Value
    Constraints *Constraints
    ValueFrom  *PropPath           // the parameter's value is derived from a location in the
                                   // outgoing/incoming message (AsyncAPI parameter `location`
                                   // runtime expressions) — SDKs may auto-fill it; nil = caller-supplied
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Secret     bool                // redact this use in logs/docs, as Property.Secret; a value whose
                                   // type is TypeCommon.Sensitive is redacted whether or not it is set
    Examples   []Example
    Unmodeled  Unmodeled
    Provenance Provenance      // the parameter's own declaration; a parameter merged into several
                               // operations (an OpenAPI path-item parameter) points at that one
                               // declaration, not at the operation it was merged into. For a
                               // referenced entry that is the component it names, and the mount
                               // site is not recorded — so inherited-vs-declared is readable off
                               // the pointer only for an entry written inline
    // NOTE: no location here — path/query/header is HTTP-binding detail (docs/ir/http-binding.md); the
    // location is part of the ID, so one name in two locations is two parameters.
    // A path-item parameter is copied by value into each operation it applies to,
    // with an ID of that operation's own.
}
```

## Stability

`Stability` is the tier a source states for an operation: how far it commits to the contract. An SDK
gates or annotates a tier, docs badge it and a CLI hides it behind a flag, and each would derive it
from a format's spelling differently if the IR did not hold it. It describes the API and holds for
every target, as `Deprecation` does, which is what keeps it out of
[client shaping](./boundaries.md#access).

| Tier | Meaning | Sources |
|---|---|---|
| `alpha` | breaking changes are expected | Smithy `@unstable`, AIP-181 alpha, an OpenAPI extension |
| `beta` | breaking changes are possible but not expected | AIP-181 beta, an OpenAPI extension |
| `stable` | the source commits to the contract | AIP-181 stable, an OpenAPI extension |

The empty value states no tier. It says nothing about the contract, and is never read as `stable`:
of the 3,763 operations in the golden spec, 573 state none (spec @319dc756).

**`OperationGroup.Stability`** ([services](./services.md)) is the tier of every member stating none,
nearest group first. It is read through the group and never copied down, so a member's own field
keeps saying what the source says of that member.

**Deprecation is a separate fact.** An operation can be `beta` and deprecated: the first says what a
caller may rely on, the second that a caller should leave. A source's vocabulary may mix the two, as
a word `deprecated` in a stability list does, and a compiler then fills `Deprecation`.

Designs this replaced, each with the fact it fails on:

- **A `Stable` bool** cannot give the badge: a docs emitter tells `alpha` from `beta`.
- **`preview` as a fourth tier** is one vocabulary's word among the sources above, with 0 uses in the
  golden spec. A compiler reads it as `beta`.
- **An open string** leaves each emitter to interpret the vocabulary, the outcome this field exists
  to prevent.
- **`Availability`** is a version timeline no emitter interprets, and has no tier in it.
- **A field on `TypeDef` and `Property` as well.** The golden spec states a tier on no type, property
  or parameter: its 2 such values both say `deprecated`, beside `deprecated: true` (spec @319dc756).
  A format that states a tier there keeps it under `no_ir_home` until a field lands for it.

`irverify` reports a value outside the declared tiers (`ir/unknown-stability`), wherever the field
sits.

## Design notes

- **Parameters carry no protocol location.** The same logical operation is bindable to HTTP
  (locations assigned in the binding), to gRPC (all params fold into a request message), or to a
  channel message. This is the split TCGC makes between `ServiceMethod` and `InputOperation`,
  turned inside out: Morphic stores the neutral core and each binding, and emitters derive the
  per-protocol view.

- **`Parameter.Secret` is a field and not a rule about the parameter's type.** A parameter carries
  its annotations and never hoists a node of its own, so a `Sensitive` scalar would have to be minted
  for it: 8,325 of the 11,915 parameters in the golden spec own no node (spec @319dc756), and a node
  minted for a key on an OpenAPI Parameter Object would sit at a schema pointer a `$ref` can name, the
  collision invariant 3 forbids. `Secret` is the per-use form a property already has, and TypeSpec's
  `@secret` on a property bound to a header or query lowers to it.

- **`OneWay` is on the neutral core**, like `Streaming` and `Idempotency`, because
  fire-and-forget is protocol-independent — an SDK that blocks awaiting a reply on a cast is
  wrong regardless of transport. The AsyncAPI compiler sets it for send-operations without a
  reply, rather than leaving one-way-ness inferable only from binding fields.
