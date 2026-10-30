# Failures

How a failure is spelled is each target's ([Go](../targets/go/errors.md),
[Kotlin](../targets/kotlin/errors.md)); what follows holds for every target.

## An error taxonomy

`RateLimited`, 429, retryable-throttling.

IR: `ErrorCase{ Name:{Hint:"429"}, Conditions:{429}, Payload:{Contents:[{application/json →
RateLimited (Model, reached from this error case)}]}, Headers:[Retry-After], Fault:"client",
Retryable:&true, Throttling:&true }`; policy default maps `429 → "TooManyRequests"`.

`OpPlan.Errors` = `[PlannedError{ Conditions:{429}, Response:{Shape:value, Content:application/json,
Type:RateLimited, Headers:[Retry-After]}, Fault:"client", Retryable:true, Throttling:true }]` —
declared IR facts, carried as such, the content elected as a success's is ([one
election](../plan/responses.md#one-election)).

## One value of the runtime

`LowerErrors` generates no error type. A failure is one value of a runtime type, which the
operation's response decoder fills from the entry a response takes. The retry wiring reads
`Retryable`/`Throttling` **from the IR fact** and falls back to `Policy.Retry` only where the spec
is silent ([precedence](../contract/runtime-policy.md#precedence--declared-ir-facts-win-hard-rule));
throttling selects the throttling backoff class. The error types are runtime boilerplate ([structure
and boilerplate](../emit/templates.md)): their kinds come from `Policy.Errors`, so a template
renders them and no spec changes them.

## The status tells failures apart

A body type rarely does: most specs name one error model for every status, or no body at all. On the
golden spec at `ef1d30e8`, 453 of the 746 operations declaring several error bodies use one type for
all of them; across 1,859 corpus documents, 172 of the 205 with an error body use a single type, and
8,538 of 13,079 failures have none. So the design keys on the status and carries the body as a
value. Three designs lost to it. A generated type per body distinguishes what a spec rarely does,
and gives a spec with one model one type for every failure; nor can a model be the error itself,
since `Error()` beside a member `error` does not compile in Go. A client and a server exception
wrapping a value per body leaves a 404 to a check. And fixed classes with an untyped body give up
what the spec declares.

## How a failure reaches its caller

How a failure reaches its caller is the target's ([target options](../contract/options.md)): a
setting of its options where it has a choice
([Go](../targets/go/errors.md#how-a-failure-reaches-its-caller),
[Kotlin](../targets/kotlin/errors.md#how-a-failure-reaches-its-caller)).

## The kinds are the taxonomy's

`Policy.Errors`' default table names `BadRequest` 400, `Unauthorized` 401, `Forbidden` 403,
`NotFound` 404, `Conflict` 409, `UnprocessableEntity` 422, `TooManyRequests` 429, `InternalServer`
500 and `ServiceUnavailable` 503. A configuration adds rows, and a protocol with statuses of its own
has rows of its own. A range such as `4XX` is no obstacle: the status that arrives is exact.

## Every failure has one place

Every failure has one place, which the status and the entry it takes decide ([which entry
answers](../plan/responses.md#which-entry-answers)):

- its place in the target's error tree follows from its kind and its `Fault`
  ([Go](../targets/go/errors.md#a-failures-place),
  [Kotlin](../targets/kotlin/errors.md#a-failures-place));
- `Fault` is the entry's, else the class the status states: 4xx `client`, 5xx `server`, any other
  none, which is what `Policy.Errors` classifies by default
  ([precedence](../contract/runtime-policy.md#precedence--declared-ir-facts-win-hard-rule)). A
  `default` reads this way, and so does any status but a 2xx that no entry answers ([which entry
  answers](../plan/responses.md#which-entry-answers));
- a `value` body decodes into `Body`, whatever `Type`'s kind, and a `void` or `raw` one leaves it
  nil, with `RawBody` holding whatever arrived;
- a body that does not decode never hides the failure: `Body` stays nil and `BodyErr` holds the
  decode error, since the status is what the caller must learn.

A body is buffered up to `Policy.Errors`' bound and no further, so an endless one cannot exhaust the
caller; `RawBody` holds what was read, and a body cut there is one that does not decode. A union
body is its struct ([unions](../refine/unions.md)), and being read it is `Open`, so a new variant in
an error reaches the caller as one rather than as a decode failure. `Headers` holds a struct of what
the entry declares, named by its operation and case (`GetZone429Headers`) and read as a success's
are ([return shapes](returns.md)), so two cases sharing a body may declare different headers.
