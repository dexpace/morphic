# Streams in the plan

```go
type StreamPlan struct {
    Direction  ir.StreamingMode       // client | server | bidi (below)
    Send       *StreamSide            // what the client writes; nil = the request does not stream
    Recv       *StreamSide            // what the client reads; nil = the response does not stream
}

type StreamSide struct {              // one direction: its StreamDetail and streaming content
    Content    *ir.Content            // the content that streams, elected as a body's is
    Framing    Framing                // how the wire divides into frames
    Element    *ir.TypeRef            // what a frame decodes into, any kind; nil = none is decoded
    FrameMembers bool                 // an SSE element's data, event, id, retry are frame fields
    Initial    *ir.TypeRef            // initial-request/response message preceding the stream; nil = none
    RequiresLength bool               // streamed content needs a known finite length up front
    Terminator *string                // StreamDetail.Terminator: frame data that ends the stream
    Condition  *ir.StreamCondition    // StreamDetail.Condition: the input that asks for the stream
}

// Framing is how a stream's wire bytes divide into frames, a small closed enum:
type Framing string                   // "sse" | "json_lines" | "json_seq" | "multipart" |
                                      // "protocol" | "bytes"
```

`Operation.Streaming` describes an operation, and a stream belongs to one of its contents: an
operation may offer `application/json` beside `application/x-ndjson`. A content *streams* when it
declares an `Item`, or when its direction has a `StreamDetail` and `plan.Policy.Framings` names its
media type. `Send` elects among the request's contents that stream, and `Recv` among those of the
operation's responses, a primary `Default` included; a direction with a `StreamDetail` and none that
streams elects among all its contents, and the content it elects then streams. Where `Recv` holds
another content than `Return`, or `Send` another than `RequestContent`, the operation offers both,
and a target generates a method for each ([streaming](../operations/streaming.md)). The IR records
no content's stream (#601), so the plan reads it from the media type. A compiler that comes to
record one on `Content` must keep it apart from a declared `Item`, which `Element` and
`FrameMembers` below read differently, and `TypeRef.Inferred` speaks only of null. `Direction` is
`Operation.Streaming`, a primary `Default` that streams, which `Streaming` does not summarize,
counting as a server side: `server`, or `bidi` beside a `Send`. A `StreamSide` holds:

- **`Framing`**, the first that applies: `protocol` under a primary binding other than HTTP, which
  frames its own messages; `bytes` for a file, which has no frames; what `plan.Policy.Framings` maps
  the media type to; and otherwise `bytes`, reported once as a warning
  `emitters/plan/unframed-stream`, for an `Item` on a media type the table does not name or a media
  type only a compiler option names.
- **`Element`**: `Content.Item` where the content declares one; else, on a side framed by its media
  type, the `Elem` of a content `Type` whose root ([TypePlan](types.md)), read through `Alias`, is a
  list; else `StreamDetail.Events`; else the content's `Type`, of any kind. OpenAPI applies `schema`
  to the complete content (OpenAPI 3.2 §4.14.3) and maps a sequential body to an array of its values
  (§4.14.3.1), so a list describes the body and its `Elem` one frame. A `bytes` side has no
  `Element`. Nor does a side framed by its media type, with no `Item`, whose content `Type`'s root
  is `string` or `bytes`: such a schema says the body is text and declares no item, and decoding
  each line as a JSON string would fail on every line that is not one. Each frame reaches the caller
  as its text, an SSE frame as its `data`. The compiler names such a `Type` as `Events` all the same
  ([`streaming-media`](../../compilers/openapi.md#streaming-media)), and keeps it, since what it
  means is the plan's to read. `Variant.Event` applies where `Element`, read through `Alias`, is a
  union.
- **`FrameMembers`**, on an `sse` side with an `Element`. An item of `text/event-stream` is the
  event itself, its `data`, `event`, `id` and `retry` (OpenAPI 3.2 §4.14.4), so a declared `Item`
  sets it, whatever its kind. Below 3.2 a schema may be the event or only what `data` holds, and
  `plan.Policy.FrameMembers` reads it as the event where a model's members, or those of every
  variant of a union, are among those four and include `data`, marked `Inferred`. Unset, the frame's
  `data` decodes into `Element`, and an `Element` whose root is `string` takes it as it arrived,
  since `data` is text.

**A terminator is no frame.** On a side the client reads, each frame's data is compared with the
side's `Terminator`, the whole data verbatim: a frame matching it ends the stream as the connection
closing does, and is not decoded into `Element`, so a sentinel such as `[DONE]` never reaches a
caller as an event that failed to decode. On a side the client writes, the client sends it as the
last frame once the caller closes the stream. A `bytes` side has no frames to compare or send, so
the plan reports its `Terminator` once as a warning `emitters/plan/terminator-unframed`, and the
stream ends with its connection.

**A stream sent only when asked is two calls.** A side whose `Condition` is set streams only when
its input holds the condition's value; otherwise that side carries the operation's non-streaming
content. A target offers the two as two methods of one operation. The stream's method sets the input
to the condition's value, whatever the caller's request holds there. The method for that content
never sends the value: it sends a boolean input as its other value, and any other as the caller's
request holds it, refusing the condition's value unsent. So neither method asks for what it cannot
return, and a side with no other content has the stream's method alone. The method names are the
target's ([naming](../refine/naming.md)).
