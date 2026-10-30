# plan.Policy — the plan's injectable heuristics

Everything the plan *infers* rather than reads is a knob here; every inferred output is marked
`Inferred` (INV6):

```go
type Policy struct {
    ContentRanking  []string                          // default: json > form > multipart > binary
    PrimaryResponse func(*ir.Operation) *ir.Response  // default: lowest 2xx, else the
                                                      // unconditional one, else the Default
    EnvelopeDetect  EnvelopePolicy                    // off unless explicitly enabled
    Framings        map[string]Framing                // media type → how it frames a stream (§3.1)
    FrameMembers    bool                              // default on: an SSE schema naming only frame
                                                      // fields describes the frame (§3.1)
    OpenUnions      bool                              // default on: a union a client reads keeps a
                                                      // variant it does not know (below)
}
```

A test asserts that disabling a heuristic removes the corresponding `Inferred` plan field — the
guard against a heuristic silently defaulting on and quietly making the plan non-neutral.

## The default response is classed here

The IR states an operation's `Default` — OpenAPI's
`default` — as neither a success nor a failure, since it answers every status the others do not name
(ir-design §7.2). `PrimaryResponse` reads the whole operation for that reason, and its default takes
the lowest 2xx `Response`, an exact status before a range that starts there, else one with no
condition, which is an RPC's one response, or the `Default` where the operation declares neither,
since a `default` answering every status is then the operation's only description of a success. A
`Default` that is not the primary response is the catch-all of `Errors`, with its condition. Either
use is marked `Inferred`, and a policy may take it as neither or both. Disabling the heuristic
leaves an operation with no success response `void` (§3.1) and its `Default` in no plan field.
`Streaming` summarizes only the streams an operation's success responses declare, so no
`StreamDetail` covers a `Default`, and a primary one streams only where a content of it declares an
`Item` (§3.1).

## A union a client reads is open

An API that adds a variant must not break every SDK already
deployed on the first response carrying it, so under `OpenUnions` a union or polymorphic base is
`Open` wherever `Facts.Usage` marks it `Output`, marked `Inferred`. A value no variant recognizes is
then kept rather than refused (§4.4). That covers a type read and written alike: a response can
carry it whatever else the type does, and a kept value a caller sends back goes out as it came. A
type only ever written stays closed, and no SDK decodes one. Turning the policy off closes every
union. The IR has no field saying a union is open or closed (#664); once it does, a declared answer
wins, as §6.3 has it. The plan records the choice so that docs and a mock agree with the SDK (§10).

## Framings

Framings state what a media type means, not a guess about an API, so the table marks nothing
`Inferred`: whether a content streams is the source's to declare or the compiler's to infer, and the
table says only how it divides. Its default holds the sequential media types OpenAPI 3.2 names:
`text/event-stream` is `sse`; `application/jsonl` and `application/x-ndjson` are `json_lines`;
`application/json-seq` and `application/geo+json-seq` are `json_seq`; and `multipart/mixed` is
`multipart`. An entry matches a media type compared as §3.1 says, and a configuration adds one for
a media type its compiler was told streams. A framing a target has no reader for is that target's
coded degrade to `raw` (§3.1).
