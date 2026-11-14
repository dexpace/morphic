# Streaming

The plan says which contents stream and how ([streams](../plan/streams.md)), and each target builds
its methods from that ([Go](../targets/go/operations.md#streaming),
[Kotlin](../targets/kotlin/operations.md)).

## A streaming operation

`TailMetrics`, JSON or server-sent events.

IR: `Operation{ Streaming:server, ResponseStream:&StreamDetail{ Events:&TypeRef→Metrics },
Responses:[200 → {application/json → MetricsPage, text/event-stream → Metrics
Stream:&ContentStream{Inferred:streaming-inference}}] }`, bound `HTTPBinding{GET /metrics}`. The
event stream's `Stream` says it is the content that streams, and no binding field states the
framing: the content's `MediaType` does.

`OpPlan{ Return:{Shape:value, Content:application/json, Type:MetricsPage}, Stream:&StreamPlan{
Direction:server, Recv:&StreamSide{ Content:text/event-stream, Framing:sse, Element:Metrics } }
}`. The ranking elects JSON for `Return`, and `Recv` holds the stream beside it
([streams](../plan/streams.md)).

## What a target generates

Each shape an operation offers is a method, sending the `Accept` of its content. An operation whose
elected content streams has the one method, under its own name. A frame that does not decode ends
the stream with an `OtherResponse` holding that frame ([a body that does not
decode](../plan/responses.md#a-body-that-does-not-decode)). What a stream yields follows from
`Recv`:

- an `Element` lowers as a field of its type does ([lowering table](../refine/lowering-table.md)),
  whatever its kind. A stream ends after an element holding a variant whose
  `Variant.Event.Terminal` is true, any of the several an [inclusive
  union](../refine/unions.md#a-value-several-variants-admit) holds included, so an event union
  needs nothing of its own. Under `FrameMembers` the element decodes from the frame's
  `data`, `event`, `id` and `retry`, and otherwise from its `data`.
- no `Element` yields each frame's text, unread.
- a `bytes` side has no frames: a side the client reads lowers as a file does ([return
  shapes](returns.md)), and a side it writes as a byte source among the request's inputs.

Direction comes from the *core* ([streams](../plan/streams.md)), and the framing from the plan. A
client stream ends by reading what `Return` reads, a bidi operation has both sides, each with its
own element and framing, and a gRPC binding of the same core would reuse the same stream shape over
length-prefixed frames. A frame whose data is a side's `Terminator` ends that stream and is no
element, and a stream a request input selects is a method of its own beside one for the
non-streaming content, which never sends the value asking for the stream
([streams](../plan/streams.md)).

A stream method gives its caller the declared headers of the response it reads, that of
`Return.Headers` where its content is the primary response's and that `Others` entry's where it is
another's, read as a `Response`'s are ([return shapes](returns.md)) once the response's head has
arrived and before its first frame, so a caller reads a rate limit or a session the stream belongs
to before any element. Those headers are where an HTTP binding places an `Initial` response's
members ([streams](../plan/streams.md)), so a stream reaches them there. Their type is named for the
stream's method and declared whatever the response holds, so a response's first header adds a field
and changes no signature, which a type parameter of `struct{}` or `Unit` turning into a named type
would ([Go](../targets/go/operations.md#streaming),
[Kotlin](../targets/kotlin/operations.md#streams)). Leaving them to the transport, as an undeclared
header is, was refused: an initial response's members would reach no typed field. T-24 holds this
paragraph.

A stream's reader waits no longer than `Timeout.Idle` for its next bytes, and its writer no longer
for the transport to take what it wrote; past that the stream ends with a timeout ([runtime
policy](../contract/runtime-policy.md#what-a-layer-writes)).

The stream *runtime* is boilerplate behind the `Transport` interface: one template, and one per
framing the document uses, which refine picks rather than a template branching on it ([structure and
boilerplate](../emit/templates.md)). Each reads its framing as the framing's standard defines a
reader: `sse` ends a line at a CR, an LF or a CRLF, skips a line starting with `:`, and dispatches
no event for a frame with no `data` line (WHATWG HTML §9.2.5, §9.2.6), `json_lines` ends a line at
an LF alone, dropping a CR before it, keeping a lone CR in its line and skipping a blank line, as
NDJSON writes lines, and `json_seq` starts a frame at each record separator (RFC 7464). A line
reader a library offers is not that: Go's `bufio.ScanLines` keeps a lone CR, while Java's
`BufferedReader.readLine` and Ktor's `readUTF8Line` end a line at one, so `a\rb` was one frame on Go
and two on Kotlin, and a reader keeping the CR before an LF read a `Terminator` sent as `[DONE]\r\n`
as `[DONE]\r` and missed it. An `Initial` message is no frame over HTTP
([streams](../plan/streams.md)), so no reader takes the first frame for it.
