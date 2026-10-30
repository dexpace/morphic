# Return shapes

What a success body is read as is `Return.Shape` ([body shapes](../plan/responses.md#body-shapes)).
[Pagination](pagination.md) and [long-running operations](long-running.md) read a model as a
`value`, behind a pager and a poller, and [streaming](streaming.md) a `stream`; the kinds a `value`
holds besides a model, and the `raw` and `void` shapes, lower as follows on every target, each
spelled as the target spells it ([Go](../targets/go/returns.md),
[Kotlin](../targets/kotlin/returns.md)).

- **A scalar, a list, a map or a union** (`GET /count` → `integer`, `GET /labels` → a map of
  strings, `GET /pet` → a `oneOf`). **plan.** `Shape` `value`, `Type` the content's type.
  **refine.** The method returns that type as any field of it lowers ([lowering
  table](../refine/lowering-table.md)). A nullable return takes the target's nullable form.
- **A file** (`GET /file`, `application/octet-stream`, `format: binary`). **plan.** `Shape` `raw`,
  since the compiler marks the content a file (`Content.File`). **refine.** The method returns the
  runtime's `RawBody`, holding the response's bytes and the media type it arrived as. Nothing
  decodes or buffers the body, so a large download streams.
- **No body** (`DELETE /pet/{id}` → `204`, or any `HEAD`). **plan.** `Shape` `void`. **refine.** The
  method returns no value.
- **A lone default** (`POST /purge`, whose only response is `default`). **plan.** The IR lowers it
  to `Operation.Default`, classed neither way (ir-design §7.2), and the default `PrimaryResponse`
  takes it for the primary response, marked `Inferred` ([plan policy](../plan/policy.md)), so
  `Shape` reads it as any other: `value` with its body, `void` without one. **refine.** As the line
  for the body it carries.
- **Declared headers** (`GET /zone` → `200` with an `ETag`). **plan.** `Return.Headers` lists them,
  and each entry of `Others` holds its own. **refine.** The plain method is unchanged, whatever the
  response declares. A result type that appeared with the first declared header would change that
  signature for every caller, and one on every method would put a field in front of every call for
  the few operations that declare any. So a response that declares headers adds a method, as a
  stream does ([streaming](streaming.md)). The runtime's `Response` reads as `APIError` does
  ([failures](errors.md)): `StatusCode`, `RawHeader`, `Body`, what the plain method returns, or the
  target's empty value for `void`, and `Headers`, a struct with a field per header of the primary
  response and of each entry of `Others` that reaches no `OtherResponse` ([other
  responses](../plan/responses.md#other-responses)), the primary's declaration first, or the
  target's empty value where they declare none. An `OtherResponse` holds its own headers, and they
  add no method. A spec's first header adds a method and its next a field, each additive, as
  [request shaping](../refine/requests.md) asks of an input. A field is found by its wire name,
  ignoring case as HTTP does, read by its type, `Style` and `Explode`, and optional whatever
  `Required` says. A value that does not parse leaves it unset: a response that delivered its body
  succeeded, and `RawHeader` still holds the value. A type no header reader lowers, such as a model,
  is a coded degrade to its text. A pager or a poller has no one response to return, so a paginated
  or long-running operation has no such method, and its plan says which headers it reads. A header
  the document does not declare reaches a caller through the transport ([runtime
  abstraction](../emit/templates.md#serialization-behind-a-runtime-abstraction)), which a caller may
  wrap.
- **Another declared response** (`POST /rules` → `201` `Rule` or `202` `ApprovalPending`;
  `DELETE /methods/{id}` → `200` `Deleted` or `204`; `GET /object` → `200` or `304`). **plan.**
  `Others` holds the second response ([other responses](../plan/responses.md#other-responses)).
  **refine.** One that reads as `Return` is returned as the primary is. One with no body leaves
  nothing to return, and any return with no absent state of its own takes the target's nullable form
  ([optionality](../refine/models.md#optionality--nullability--presence-inv8)). A pager or a stream
  ends, so an empty list is no error. Which status came is then all the caller lacks, so such an
  operation, a paginated one aside, has a `WithResponse` method whatever its headers. A first such
  response changes a Kotlin return, and a Go return of no nullable type, a break the surface differ
  reports ([surface verification](../surface-verification.md)) and rightly: a call that always had a
  value may now have none.

  A body the method cannot return is a value of the runtime, `OtherResponse`, and no signature
  changes for one. It holds the status, every header as it arrived, the body as it arrived up to
  `Policy.Errors`' bound, why the body did not decode, the body as its `Type` lowers and the entry's
  declared headers. It is no failure, so no fault or kind answers it and no retry reads it. Its
  `Headers` struct is named as a failure's is ([failures](errors.md)), `CreateRule202Headers`.

  A zero value would not do here: Kotlin's `Rule` has none, a pager has no value to zero, and a nil
  `*Rule` beside a nil error reads as a created rule until the caller's next line dereferences it.
- **A status the document does not list, and a body that does not decode** (`GET /zone` → `201`, or
  a `200` holding a proxy's HTML). **plan.** No field: [the
  plan](../plan/responses.md#which-entry-answers) says which entry answers. **refine.** The response
  decoder reads a 2xx no entry answers as `Return`, so `GetZone` returns the `*Zone` a `201`
  carries. A body that then does not decode, an empty one under a `value` among them, is the
  `OtherResponse` above with `BodyErr` set, never an `APIError`: the server answered and may have
  acted, so nothing retries it, and the caller still has the status and the bytes read.
- **No success response** (`POST /purge` declaring only `4XX`). **plan.** `Shape` `void`: the method
  returns only an error, a 2xx returning none since it reads as `Return` does, and any other status
  no declared response or error case matches is the error taxonomy's to classify
  ([failures](errors.md)).
- **An anonymous decorator** (`200 → {allOf: [{$ref: Zone}], description: …}`). **plan.** `Shape`
  `value`, `Type` the decorator, as the content names it, whose `TypePlan.Alias` is `Zone`
  ([decorators](../refine/decorators.md)). **refine.** `CollapseAliases` lowers the reference to
  `Zone`, so the method returns `*Zone`; the description, which no return position holds, stays in
  the IR for the docs emitter and is reported once ([decorators](../refine/decorators.md)).
