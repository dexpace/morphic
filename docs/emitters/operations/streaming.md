# Streaming

The plan says which contents stream and how ([streams](../plan/streams.md)), and each target builds
its methods from that ([Go](../targets/go/operations.md#streaming),
[Kotlin](../targets/kotlin/operations.md)).

## A streaming operation

`TailMetrics`, JSON or server-sent events.

IR: `Operation{ Streaming:server, ResponseStream:&StreamDetail{ Events:&TypeRef→Metrics },
Responses:[200 → {application/json → MetricsPage, text/event-stream → Metrics}] }`, bound
`HTTPBinding{GET /metrics}`. No binding field states the framing: the content's `MediaType` does.

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
non-streaming content, each setting that input itself ([streams](../plan/streams.md)).

The stream *runtime* is boilerplate behind the `Transport` interface: one template, and one per
framing the document uses, which refine picks rather than a template branching on it ([structure and
boilerplate](../emit/templates.md)).
