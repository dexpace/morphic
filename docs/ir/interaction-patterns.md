# Interaction patterns

Part of the [IR design](./README.md). Pagination, long-running operations and streaming: an
operation that takes more than one request and one response.

## Pagination

```go
type Pagination struct {
    Strategy   PageStrategy   // cursor | offset | page | link_header | next_link | token
    Inferred   bool           // heuristic (OpenAPI name-matching policy) vs declared (Smithy/TypeSpec)
    InputCursor  *InputPath   // which input continues iteration: a parameter, the body, or a member
    InputLimit   *InputPath
    Items        *PropPath    // where result items live in the response — a PATH, not a name
    NextCursor   *PropPath    // continuation source in the response
    NextLink     *PropPath
    PrevLink     *PropPath    // server-driven navigation links beyond next
    FirstLink    *PropPath    // (TypeSpec @prevLink/@firstLink/@lastLink, OpenAPI links)
    LastLink     *PropPath
    TotalCount   *PropPath    // counts items
    TotalPages   *PropPath    // counts pages
    Stop         []PageStop   // when iteration ends, any one ending it; empty = none stated
}

type PageStop string // "empty" | "no_next" | "short" | "total"

type PropPath struct {
    Root     *TypeRef  // the type the path roots in; nil = determined by context
                       // (the enclosing response body, message payload, …)
    In       string    // "" = body/payload | "header" — continuation tokens and reply addresses
                       // can live in response/message headers, not just bodies
    Segments []PropID
}
type InputPath struct{ Param *ParamID; Segments []PropID } // nil Param = the request body;
                                                            // no Segments = the root whole
// InputPath.Param, like Idempotency.TokenParam, names a parameter of the operation
// carrying it: another operation's ParamID, or a field argument's, is a well-formed
// reference to the wrong parameter, and pass.Validate rejects it
// (pass/param-binding-mismatch).
```

An input the pagination reads is an `InputPath`: a parameter and, for a member of one, the path
into it, or, with no parameter, a path into the request body, since a search endpoint taking its
cursor or page size as body fields is as common as one taking them as query parameters. With no
segment either, it is the request body whole, as a [link](./responses.md#links) can pass it on.
`TotalCount` counts items and `TotalPages` pages, the two totals an API returns.

`Stop` is when iteration ends, which the source states and no consumer can recover from the other
fields: the same cursor shape ends at an empty page on one API and at an empty next cursor on
another. The conditions are a closed set, any one ending the loop (`ir/unknown-page-stop` for one
outside it):

| Stop | Iteration ends at a page |
|---|---|
| `empty` | holding no items |
| `no_next` | whose next cursor or next link is empty, null or absent |
| `short` | holding fewer items than the limit asked for |
| `total` | after which the items read reach `TotalCount`, or the pages read reach `TotalPages` |

An empty `Stop` states no rule, and a consumer applies its own policy rather than reading one into
the shape ([pagination policy](../emitters/plan/operations.md)).


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
    Terminator *string          // frame data ending the stream, no element ([DONE]); nil = none
    Condition  *StreamCondition // the request input asking for the stream; nil = it always streams
}

type StreamCondition struct { Input InputPath; Value Value }
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

**A sentinel that ends a stream** is `Terminator`, the frame's data verbatim: many event-stream APIs
end with a frame such as `data: [DONE]` that is no event and must not be decoded as one, and
without the field every emitter hard-codes or guesses it. It is the data, compared whole, not an
element type, since the sentinel sits outside the element's schema; an event a union lists that
ends the stream is `Variant.Event.Terminal`, which is a variant and is decoded as one.

**A stream sent only when asked** carries a `Condition`: the request input and the value that ask
for it, as a body's `stream: true` does, the operation otherwise returning its non-streaming
contents. The response then lists both contents, and the plan offers the typed pair of methods such
an API calls for ([streams](../emitters/plan/streams.md)). Lowering such an operation to two
operations sharing one route (`SharedRoute`), each with a literal-typed selector, was weighed and
declined: the source declares one operation, so two would mint an operation no position names, and
the selector would be a type the document never wrote.
