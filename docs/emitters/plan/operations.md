### 3.1 OpPlan — the fixed per-operation decision set

Adopted from oagen's production `OperationPlan` (prior-art §1), the decision set the emitters
demonstrably needed:

```go
type OpPlan struct {
    PrimaryResponse *ir.Response      // SDK-ergonomic pick; IR keeps ALL responses
    Return          ResponsePlan      // how the primary response is read
    Others          []PlannedResponse // every other response the operation declares (below)
    RequestContent  *ir.Content       // the request content elected; nil = no body
    Errors          []PlannedError    // every failure the operation declares, ranges collapsed
                                      // (404/4XX/catch-all)
    Page            *PagePlan         // nil = not paginated
    Stream          *StreamPlan       // nil = neither direction streams (ir-design.md §7.3)
    LRO             *LROPlan          // nil = not long-running
    Auth            []AuthOption      // OR-of-ANDs, priority-ordered (§8.6)
    Bindings        BindingView       // per-protocol param/request/response view from OpBindings
    Inferred        map[string]ir.Provenance  // which fields above were heuristic (INV6)
}

// ResponsePlan is how one response reaches its reader. A success and a failure are read by the
// same rules, so the election, the shapes, the codec and the headers have one home.
type ResponsePlan struct {
    Shape   BodyShape     // value | void | raw | stream
    Content *ir.Content   // the content elected from the payload; nil = nothing to read
    Type    *ir.TypeRef   // what a value body decodes into, of any kind; nil unless Shape is value
    Headers []ir.Header   // the headers the response declares, each whole (below)
}

type PagePlan struct {                // item-type unwrap is a PLAN decision
    Strategy   ir.PageStrategy
    ItemType   *ir.TypeRef            // element type items resolve to, unwrapped from the envelope
    ItemPath   *ir.PropPath           // where items live — a PATH (may be nested / in headers)
    CursorIn   *ir.ParamPath
    CursorOut  *ir.PropPath
    Inferred   bool                   // heuristic pagination (may be disabled by policy)
}

type PlannedResponse struct {
    Conditions ir.ResponseConditions  // the statuses it answers, ranges collapsed
    Response   ResponsePlan           // how it is read (below)
    AsReturn   bool                   // it reads as Return does, so a method returns it likewise
}

type PlannedError struct {
    Conditions ir.ResponseConditions  // the statuses it answers, ranges collapsed
    Response   ResponsePlan           // how it is read (below); Type is of any kind
    Fault      string                 // "" | "client" | "server" — declared; "" = by status (§8.5)
    Retryable  *bool                  // declared (nil = unknown) → wins over policy (§6.3)
    Throttling *bool                  // declared throttling class (nil = unknown)
}

// The remaining operation-facet plans. Each is nil when the facet is absent, so a refiner's
// presence check is a nil check, and each carries only classified facts — never a target type.
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
}

// Framing is how a stream's wire bytes divide into frames, a small closed enum:
type Framing string                   // "sse" | "json_lines" | "json_seq" | "multipart" |
                                      // "protocol" | "bytes"

type LROPlan struct {                 // maps ir.LongRunning (ir-design.md §7.3)
    FinalStateVia string              // "operation-location" | "status-monitor" | "original-uri" | …
    PollOp     *ir.OpID               // declared polling operation; nil = poll the start op's monitor
    FinalOp    *ir.OpID               // declared final-result operation; nil = result is inline
    PollType   *ir.TypeRef            // status-monitor type
    FinalType  *ir.TypeRef            // what PollUntilDone resolves to
    ResultPath *ir.PropPath           // where the final result lives in the terminal response
}

type AuthOption struct {              // one option in the OR-of-ANDs; slice order is PRIORITY order
    Schemes []ir.SchemeUse            // ALL must be satisfied together; empty = "no auth is acceptable"
}

// BodyShape is how a body reaches its reader, a small closed enum:
type BodyShape string                 // "value" | "void" | "raw" | "stream"
```

**One election.** A payload may offer several contents, and `plan.Policy.ContentRanking` (§3.5)
elects the one a `ResponsePlan` or a `StreamSide` reads and the one `RequestContent` sends. A
success, a failure, a request and each side of a stream share that helper, so two targets cannot
read one 429 as different media types, or send one request as them. An entry is one of four classes
— `json` is `application/json` and any `+json` suffix, `form` is
`application/x-www-form-urlencoded`, `multipart` any `multipart/*` and `binary`
`application/octet-stream` — or a single media type. A media type is compared lowercased, trimmed
and cut at its first `;`, as RFC 6838 §4.2 and the compiler's own reading
([`media-types`](./compilers/openapi.md#media-types)) have it, since `Content.MediaType` keeps the
key as written. A media type no entry ranks comes after every one that is ranked, and a tie goes to
the smaller key by bytes, since a format's map of media types has no order to honour.

**Body shapes.** `Shape` says how a body is read, not what it holds: what it holds is `Type`, and
every type kind already has a lowering (§4.2), so a shape per kind would repeat the type in a second
channel. The first rule that applies decides:

1. **`void`** — nothing is read: the operation has no primary response (§3.5), the response carries
   no content, or the binding's method is `HEAD`, whose response has no body (RFC 9110 §9.3.2). An
   operation with no primary response returns only an error, and "Which entry answers" below says
   which statuses raise one.
2. **`raw`** — the elected content is a file (`Content.File`, ir-design §7.2): the body reaches the
   caller unread, a byte stream beside the media type it arrived as, and nothing decodes it.
3. **`stream`** — the elected content streams (below): frames are read one at a time, as `Stream`
   describes (§8.4).
4. **`value`** — otherwise: the elected content's codec decodes the body into `Type`, whatever its
   kind — a model, a list, a map, a union, an enum, a scalar, `any`. A paginated operation's
   `Return.Type` is one page, from which `Page` takes the items.

`Type` is the elected content's type, lowered through `TypePlan.Alias` as any reference is (§3,
§4.15), so an anonymous `allOf` decorator around a model returns the model. Its `Nullable` is the
content's, and a nullable return takes the target's nullable form (§4.7). Unwrapping an envelope,
which `plan.Policy` may enable (§3.5), would return a value found inside the content instead; what
it records and where is #695's. Which codec reads a media type is the target's, and a media type it
has none for is a coded degrade of that target's that reads the body as `raw`. Every body of every
document takes one of the four, since the rules end in a default; §8.9 traces each through the first
targets.

**Headers.** `Headers` are what the response declares, each whole, so its name, its type and the
`Style` and `Explode` it is read by reach the reader. `Return`'s are the primary response's, and
each entry of `Others` and `Errors` holds its own. A success and a failure then read alike: the raw
headers are on the value, and the declared ones typed beside its body (§8.5, §8.9).

**Streams.** `Operation.Streaming` describes an operation, and a stream belongs to one of its
contents: an operation may offer `application/json` beside `application/x-ndjson`. A content
*streams* when it declares an `Item`, or when its direction has a `StreamDetail` and
`plan.Policy.Framings` names its media type. `Send` elects among the request's contents that stream,
and `Recv` among those of the operation's responses, a primary `Default` included; a direction with
a `StreamDetail` and none that streams elects among all its contents, and the content it elects
then streams. Where `Recv` holds another content than `Return`, or `Send` another than
`RequestContent`, the operation offers both, and a target generates a method for each (§8.4). The
IR records no content's stream (#601), so the plan reads it from the media type. A compiler that
comes to record one on `Content` must keep it apart from a declared `Item`, which `Element` and
`FrameMembers` below read differently, and `TypeRef.Inferred` speaks only of null. `Direction` is
`Operation.Streaming`, a primary `Default` that streams, which `Streaming` does not summarize,
counting as a server side: `server`, or `bidi` beside a `Send`. A `StreamSide` holds:

- **`Framing`**, the first that applies: `protocol` under a primary binding other than HTTP, which
  frames its own messages; `bytes` for a file, which has no frames; what `plan.Policy.Framings` maps
  the media type to; and otherwise `bytes`, reported once as a warning
  `emitters/plan/unframed-stream`, for an `Item` on a media type the table does not name or a media
  type only a compiler option names.
- **`Element`**: `Content.Item` where the content declares one; else, on a side framed by its media
  type, the `Elem` of a content `Type` whose root (§3.2), read through `Alias`, is a list; else
  `StreamDetail.Events`; else the content's `Type`, of any kind. OpenAPI applies `schema` to the
  complete content (OpenAPI 3.2 §4.14.3) and maps a sequential body to an array of its values
  (§4.14.3.1), so a list describes the body and its `Elem` one frame. A `bytes` side has no
  `Element`. Nor does a side framed by its media type, with no `Item`, whose content `Type`'s root
  is `string` or `bytes`: such a schema says the body is text and declares no item, and decoding
  each line as a JSON string would fail on every line that is not one. Each frame reaches the caller
  as its text, an SSE frame as its `data`. The compiler names the content's `Type` as `Events` all
  the same ([`streaming-media`](./compilers/openapi.md#streaming-media)), and keeps it, since what
  it means is the plan's to read; #630 would name a list's `Elem` there, which this rule reads
  already. `Variant.Event` applies where `Element`, read through `Alias`, is a union.
- **`FrameMembers`**, on an `sse` side with an `Element`. An item of `text/event-stream` is the
  event itself, its `data`, `event`, `id` and `retry` (OpenAPI 3.2 §4.14.4), so a declared `Item`
  sets it, whatever its kind. Below 3.2 a schema may be the event or only what `data` holds, and
  `plan.Policy.FrameMembers` reads it as the event where a model's members, or those of every
  variant of a union, are among those four and include `data`, marked `Inferred`. Unset, the frame's
  `data` decodes into `Element`, and an `Element` whose root is `string` takes it as it arrived,
  since `data` is text.

**Other responses.** `Others` holds every entry of `Operation.Responses` but the primary, and
entries that agree on everything but `Conditions` collapse into one. A declared response is one the
API said it may send, so it is never a failure, whatever its status: it has no `Fault`, no kind and
no retry. Each is read by the rules above, and what a method does with it follows from how that
reading compares with `Return` (§8.9):

1. **It reads as `Return` does**: the same `Shape`, the same `Type` through `Alias` for a `value`
   and the same `Element` for a `stream`. `AsReturn` is set, and the method returns it as it returns
   the primary.
2. **It has no body, beside a `Return` that has one**: a `204` or a `304` beside a `200`. The method
   has nothing to return, so the caller gets no value and no error: a return takes the target's
   nullable form (§4.7), and a pager or a stream ends, as an empty one does.
3. **It has a body the method cannot return**: a `202` of another type beside a `201`, or any body
   beside a `void` `Return`. It is read whole, as a failure is, so a content that streams is `raw`,
   and it reaches the caller as the runtime's `OtherResponse`, apart from every failure. Nothing
   decodes it as `Return.Type`.

A stream method reads `Recv` in `Return`'s place, and a poller takes each response of its start as
its `LROPlan` says (§8.7).

**A body that does not decode.** A method that cannot decode a body it reads, the primary's among
them, has nothing to return, and the server answered, so this is no failure either. It reaches the
caller as an `OtherResponse` whose `BodyErr` says why, so the status and the bytes survive and
nothing retries a call that may have acted. A stream frame that does not decode ends the stream the
same way. A failure's body keeps its own form (§8.5).

**Errors.** `Errors` holds every failure the operation declares: its `ErrorCase`s, then its
service's `CommonErrors`, then its `Default` where the policy takes it as a failure (§3.5), whose
condition is the catch-all. Entries that agree on everything but `Conditions` collapse into one.
`Response` follows the rules above with one exception: a failure is read whole, so a content that
streams is `raw`. `Type` may be of any kind, since an error body may be a union, a list or a
primitive as well as a model, and where each lands in the SDK is §8.5's.

**Which entry answers.** A response takes, across the primary response, `Others` and `Errors`, the
entry with the most specific condition that holds — an exact status, then a range, then the
catch-all — and the first such entry where several tie, so a declared `202` never reaches a
catch-all. A status's class bounds a catch-all (ir-design §7.2): a primary `Default` holds only
below 400, and the catch-all of `Errors` for no 2xx. A 2xx no entry answers is a success the
document does not list, so it reads as `Return` does: a server that starts sending a `201` must not
turn a call that worked into a failure. Any other status no entry answers is a failure (§8.5).

`PrimaryResponse` and the contents a `ResponsePlan` elects are the *one legitimate home* for the
collapse oagen performs destructively in its compiler and Morphic forbids there (INV2): the IR keeps
every response and content type whole; "pick a primary for the ergonomic method" happens here,
lowered late. The content ranking (`json > form > multipart > binary`) is a shared, overridable plan
helper (resolves `ir-design.md` open Q3).

Some facts oagen's plan carries are not here, by §3.6. Whether there is a body, whether the
operation is one-way and its idempotency are stated by the IR, so a refiner reads them there:
`Operation.Request`, `Operation.OneWay` and `Operation.Idempotency`, the last of which also names
the idempotency-token parameter. And whether an operation's inputs are passed positionally or
inside an options object differs by language, so it is each target's (§4.13); the neutral facts it
is decided from are already here — `BindingView` says where each input binds, and
`Parameter.Required` and `Payload.Required` which are required — so moving the decision out added
no field.
