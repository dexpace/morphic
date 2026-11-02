# Interaction patterns

Part of the [IR design](./README.md). Pagination, long-running operations and streaming: an
operation that takes more than one request and one response.

## Pagination

```go
type Pagination struct {
    Strategy   PageStrategy   // cursor | offset | page | link_header | next_link | token
    Inferred   bool           // heuristic (OpenAPI name-matching policy) vs declared (Smithy/TypeSpec)
    InputCursor  *ParamPath   // which input continues iteration
    InputLimit   *ParamPath
    Items        *PropPath    // where result items live in the response — a PATH, not a name
    NextCursor   *PropPath    // continuation source in the response
    NextLink     *PropPath
    PrevLink     *PropPath    // server-driven navigation links beyond next
    FirstLink    *PropPath    // (TypeSpec @prevLink/@firstLink/@lastLink, OpenAPI links)
    LastLink     *PropPath
    TotalCount   *PropPath
}

type PropPath struct {
    Root     *TypeRef  // the type the path roots in; nil = determined by context
                       // (the enclosing response body, message payload, …)
    In       string    // "" = body/payload | "header" — continuation tokens and reply addresses
                       // can live in response/message headers, not just bodies
    Segments []PropID
}
type ParamPath struct{ Param ParamID; Segments []PropID }
// ParamPath.Param, like Idempotency.TokenParam, names a parameter of the operation
// carrying it: another operation's ParamID, or a field argument's, is a well-formed
// reference to the wrong parameter, and pass.Validate rejects it
// (pass/param-binding-mismatch).
```

## Long-running operations

```go
type LongRunning struct {
    FinalStateVia string       // "operation-location" | "status-monitor" | "original-uri" | …
    PollingOperation *OpID     // declared poll op (Azure.Core @pollingOperation — a library
                               // convention, not core TypeSpec)
    FinalOperation   *OpID     // declared final-result op (Azure.Core @finalOperation)
    PollingType   *TypeRef
    FinalType     *TypeRef
    ResultPath    *PropPath
}
```

Smithy **waiters** (`smithy.waiters#waitable`) are *not* folded into `LongRunning` —
acceptor lists and JMESPath matchers are preserved verbatim in `Operation.Unmodeled`
([boundaries](./boundaries.md#deliberate-exclusions)).

## Streaming

```go
type StreamingMode string      // "" (none) | "client" | "server" | "bidi"

type StreamDetail struct {
    Events  *TypeRef   // stream element type, of any kind; nil where the contents that stream
                       // name different elements, which stay on the payload. For a format with
                       // event streams it is a WireTagged Union of event models: within an event
                       // model, Property.EventHeader marks frame-header members and
                       // Property.EventPayload marks a raw-payload member (Smithy
                       // @eventHeader/@eventPayload), and per-event content types and terminal
                       // events live on Variant.Event
    Initial *TypeRef   // initial-request / initial-response message preceding the stream
                       // (Smithy event stream initial messages); nil = none
    RequiresLength bool // the streamed content must have a known finite length up front
                        // (Smithy @requiresLength) — changes the generated parameter type
}
```

Streaming mode is on the operation core because it is protocol-independent (gRPC streams, SSE,
WebSocket, Smithy event streams all project onto it). An operation that does not stream holds the
zero mode, as `IdempotencyUnknown` and `AdditionalUnspecified` are their fields' absent states, so
such an operation encodes no member; a `"none"` spelling would add one to nearly every operation
and say nothing `RequestStream` and `ResponseStream` do not. `Streaming` summarizes an operation,
and a stream belongs to one content of its payload, so an OpenAPI operation can offer
`application/json` beside `application/x-ndjson`, and the plan elects among them
([streams](../emitters/plan/streams.md)). The wire mechanism (SSE vs JSON Lines vs WS frames vs
length-prefixed events) has no field of its own: over HTTP the streaming content's `MediaType`
states it, another binding frames its own messages, and the plan derives which
([streams](../emitters/plan/streams.md)). `Content.Item` declares what one item is; a compiler that
reads a stream from a media type alone names what one frame holds as `Events`, a list `Type`'s
`Elem`, else the `Type`, or `bytes` for a file, whose `Type` describes the whole, a reading
`Provenance.Inferred` marks. An element need not
be a union, and `Variant.Event` applies only where it is one.
