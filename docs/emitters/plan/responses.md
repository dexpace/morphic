# How a response is read

`Return`, `Others` and `Errors` ([`OpPlan`](operations.md)) each hold a response the operation
declares, read by one `ResponsePlan`.

```go
// ResponsePlan is how one response reaches its reader. A success and a failure are read by the
// same rules, so the election, the shapes, the codec and the headers have one home.
type ResponsePlan struct {
    Shape   BodyShape     // value | void | raw | stream
    Content *ir.Content   // the content elected from the payload; nil = nothing to read
    Type    *ir.TypeRef   // what a value body decodes into, of any kind; nil unless Shape is value
    Headers []ir.Header   // the headers the response declares, each whole (below)
}

// BodyShape is how a body reaches its reader, a small closed enum:
type BodyShape string                 // "value" | "void" | "raw" | "stream"

type PlannedResponse struct {
    Conditions ir.ResponseConditions  // the statuses it answers, ranges collapsed
    Response   ResponsePlan           // how it is read (below)
    AsReturn   bool                   // it reads as Return does, so a method returns it likewise
}

type PlannedError struct {
    Conditions ir.ResponseConditions  // the statuses it answers, ranges collapsed
    Response   ResponsePlan           // how it is read (below); Type is of any kind
    Fault      string                 // "" | "client" | "server" — declared; "" = by status (../operations/errors.md)
    Retryable  *bool                  // declared (nil = unknown) → wins over policy (../contract/runtime-policy.md)
    Throttling *bool                  // declared throttling class (nil = unknown)
}
```

## One election

A payload may offer several contents, and `plan.Policy.ContentRanking` ([plan policy](policy.md))
elects the one a `ResponsePlan` or a `StreamSide` reads and the one `RequestContent` sends. A
success, a failure, a request and each side of a stream share that helper, so two targets cannot
read one 429 as different media types, or send one request as them. An entry is one of four classes
— `json` is `application/json` and any `+json` suffix, `form` is
`application/x-www-form-urlencoded`, `multipart` any `multipart/*` and `binary`
`application/octet-stream` — or a single media type. A media type is compared lowercased, trimmed
and cut at its first `;`, as RFC 6838 §4.2 and the compiler's own reading
([`media-types`](../../compilers/openapi.md#media-types)) have it, since `Content.MediaType` keeps
the key as written. A media type no entry ranks comes after every one that is ranked, and a tie goes
to the smaller key by bytes, since a format's map of media types has no order to honour.

## Body shapes

`Shape` says how a body is read, not what it holds: what it holds is `Type`, and every type kind
already has a lowering ([step list](../refine/steps.md)), so a shape per kind would repeat the type
in a second channel. The first rule that applies decides:

1. **`void`** — nothing is read: the operation has no primary response ([plan policy](policy.md)),
   the response carries no content, or the binding's method is `HEAD`, whose response has no body
   (RFC 9110 §9.3.2). An operation with no primary response returns only an error, and "Which entry
   answers" below says which statuses raise one.
2. **`raw`** — the elected content is a file (`Content.File`, ir-design §7.2): the body reaches the
   caller unread, a byte stream beside the media type it arrived as, and nothing decodes it.
3. **`stream`** — the elected content streams ([streams](streams.md)): frames are read one at a
   time, as `Stream` describes ([streaming](../operations/streaming.md)).
4. **`value`** — otherwise: the elected content's codec decodes the body into `Type`, whatever its
   kind — a model, a list, a map, a union, an enum, a scalar, `any`. A paginated operation's
   `Return.Type` is one page, from which `Page` takes the items.

`Type` is the elected content's type, lowered through `TypePlan.Alias` as any reference is ([the
plan](overview.md), [decorators](../refine/decorators.md)), so an anonymous `allOf` decorator around
a model returns the model. Its `Nullable` is the content's, and a nullable return takes the target's
nullable form ([optionality](../refine/models.md#optionality--nullability--presence-inv8)).
Unwrapping an envelope, which `plan.Policy` may enable ([plan policy](policy.md)), would return a
value found inside the content instead; what it records and where is #695's. Which codec reads a
media type is the target's, and a media type it has none for is a coded degrade of that target's
that reads the body as `raw`. Every body of every document takes one of the four, since the rules
end in a default; [Return shapes](../operations/returns.md) traces each through the first targets.

## Headers

`Headers` are what the response declares, each whole, so its name, its type and the `Style` and
`Explode` it is read by reach the reader. `Return`'s are the primary response's, and each entry of
`Others` and `Errors` holds its own. A success and a failure then read alike: the raw headers are on
the value, and the declared ones typed beside its body ([failures](../operations/errors.md), [return
shapes](../operations/returns.md)).

## Other responses

`Others` holds every entry of `Operation.Responses` but the primary, and entries that agree on
everything but `Conditions` collapse into one. A declared response is one the API said it may send,
so it is never a failure, whatever its status: it has no `Fault`, no kind and no retry. Each is read
by the rules above, and what a method does with it follows from how that reading compares with
`Return` ([return shapes](../operations/returns.md)):

1. **It reads as `Return` does**: the same `Shape`, the same `Type` through `Alias` for a `value`
   and the same `Element` for a `stream`. `AsReturn` is set, and the method returns it as it returns
   the primary.
2. **It has no body, beside a `Return` that has one**: a `204` or a `304` beside a `200`. The method
   has nothing to return, so the caller gets no value and no error: a return takes the target's
   nullable form ([optionality](../refine/models.md#optionality--nullability--presence-inv8)), and a
   pager or a stream ends, as an empty one does.
3. **It has a body the method cannot return**: a `202` of another type beside a `201`, or any body
   beside a `void` `Return`. It is read whole, as a failure is, so a content that streams is `raw`,
   and it reaches the caller as the runtime's `OtherResponse`, apart from every failure. Nothing
   decodes it as `Return.Type`.

A stream method reads `Recv` in `Return`'s place, and a poller takes each response of its start as
its `LROPlan` says ([long-running operations](../operations/long-running.md)).

## A body that does not decode

A method that cannot decode a body it reads, the primary's among them, has nothing to return, and
the server answered, so this is no failure either. It reaches the caller as an `OtherResponse` whose
`BodyErr` says why, so the status and the bytes survive and nothing retries a call that may have
acted. A stream frame that does not decode ends the stream the same way. A failure's body keeps its
own form ([failures](../operations/errors.md)).

## Errors

`Errors` holds every failure the operation declares: its `ErrorCase`s, then its service's
`CommonErrors`, then its `Default` where the policy takes it as a failure ([plan
policy](policy.md)), whose condition is the catch-all. Entries that agree on everything but
`Conditions` collapse into one. `Response` follows the rules above with one exception: a failure is
read whole, so a content that streams is `raw`. `Type` may be of any kind, since an error body may
be a union, a list or a primitive as well as a model, and where each lands in the SDK is in
[failures](../operations/errors.md).

## Which entry answers

A response takes, across the primary response, `Others` and `Errors`, the entry with the most
specific condition that holds — an exact status, then a range, then the catch-all — and the first
such entry where several tie, so a declared `202` never reaches a catch-all. A status's class bounds
a catch-all (ir-design §7.2): a primary `Default` holds only below 400, and the catch-all of
`Errors` for no 2xx. A 2xx no entry answers is a success the document does not list, so it reads as
`Return` does: a server that starts sending a `201` must not turn a call that worked into a failure.
Any other status no entry answers is a failure ([failures](../operations/errors.md)).
