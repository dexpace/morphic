### 8.5 An error taxonomy (`RateLimited`, 429, retryable-throttling)

IR: `ErrorCase{ Name:{Hint:"429"}, Conditions:{429}, Payload:{Contents:[{application/json →
RateLimited (Model, reached from this error case)}]}, Headers:[Retry-After], Fault:"client",
Retryable:&true, Throttling:&true }`; policy default maps `429 → "TooManyRequests"`.

- **plan.** `OpPlan.Errors` = `[PlannedError{ Conditions:{429}, Response:{Shape:value,
  Content:application/json, Type:RateLimited, Headers:[Retry-After]}, Fault:"client",
  Retryable:true, Throttling:true }]` — declared IR facts, carried as such, the content elected as a
  success's is (§3.1).
- **refine.** `LowerErrors` generates no error type. A failure is one value of a runtime type, which
  the operation's response decoder fills from the entry a response takes:

  ```go
  type APIError struct {         // the runtime's: what one failed response said
      StatusCode int
      RawHeader  http.Header     // every header as it arrived, by wire name
      RawBody    []byte          // the body as it arrived, up to Policy.Errors' bound
      BodyErr    error           // why the body did not decode; nil when it did, or had none
      Fault      Fault           // the entry's, else the status's class
      Kind       Kind            // the taxonomy's logical kind for the status; "" = none
      Body       any             // the body as its Type lowers, of any kind; nil = none decoded
      Headers    any             // the entry's declared headers, a struct; nil = none declared
      retry      retryFacts      // the entry's Retryable and Throttling, behind IsRetryable()
  }

  func (e *APIError) Error() string { return fmt.Sprintf("api error: status %d", e.StatusCode) }
  func (e *APIError) Unwrap() error { return e.BodyErr }

  func Body[B any](err error) (B, bool) { // err's failure's body, where it is a B
      var zero B
      e, ok := errors.AsType[*APIError](err)
      if !ok {
          return zero, false
      }
      b, ok := e.Body.(B)
      return b, ok
  }
  ```

  `Headers[H]` reads the same way, and `(*APIError).Is` answers `errors.Is` for `sdk.ErrClient`,
  `sdk.ErrServer` and a sentinel per kind (`sdk.ErrNotFound`). The retry wiring reads
  `Retryable`/`Throttling` **from the IR fact** and falls back to `Policy.Retry` only where the spec
  is silent (§6.3); throttling selects the throttling backoff class.
- **emit.** Printer emits the decoder, registering each entry: `429 → RateLimited`, with its headers
  struct, fault and retry facts, which the retry runtime consults through
  `IsRetryable()`/`IsThrottling()`. The error types are runtime boilerplate (§5.2): their kinds come
  from `Policy.Errors`, so a template renders them and no spec changes them.

A caller branches on what happened, and reads the body when it needs it:

```go
switch {
case errors.Is(err, sdk.ErrNotFound): // …
case errors.Is(err, sdk.ErrClient):
    if p, ok := sdk.Body[sdk.Problem](err); ok { show(p) }
}
```

**The status tells failures apart.** A body type rarely does: most specs name one error model for
every status, or no body at all. On the golden spec at `ef1d30e8`, 453 of the 746 operations
declaring several error bodies use one type for all of them; across 1,859 corpus documents, 172 of
the 205 with an error body use a single type, and 8,538 of 13,079 failures have none. So the design
keys on the status and carries the body as a value. Three designs lost to it. A generated type per
body distinguishes what a spec rarely does, and gives a spec with one model one type for every
failure; nor can a model be the error itself, since `Error()` beside a member `error` does not
compile in Go. A client and a server exception wrapping a value per body leaves a 404 to a check.
And fixed classes with an untyped body give up what the spec declares.

**How a failure reaches its caller is the target's** (§2.5). Go needs no setting, since an `error`
is already a value. Kotlin carries it in an exception, the default of `errorStyle` and the only
style M3 builds:

```kotlin
class ApiFailure(val statusCode: Int, val rawHeaders: Headers, val rawBody: ByteArray,
                 val bodyError: Throwable?, val fault: Fault, val kind: Kind?,
                 val body: Any?, val headers: Any?)

open class ApiException(val failure: ApiFailure) : Exception()
open class ClientException(failure: ApiFailure) : ApiException(failure)
open class ServerException(failure: ApiFailure) : ApiException(failure)
class NotFoundException(failure: ApiFailure) : ClientException(failure)   // one per kind

inline fun <reified B> ApiException.body(): B? = failure.body as? B
inline fun <reified H> ApiException.headers(): H? = failure.headers as? H
```

`e.body<T>()` checks a class, and Kotlin erases a generic type's arguments even when `T` is reified,
so `e.body<List<A>>()` also matches a `List<B>` and fails when an element is read: a caller reads a
list or a map body by status. Go's assertion is exact.

A kind nests under its fault because a status states its class, so `catch (e: NotFoundException)`
and `catch (e: ClientException)` both work; a body type could not nest, since one body can arrive
under either fault. `Fault` is an enum there, with `Unknown` for what Go leaves empty, so a `when`
over it is exhaustive. An entry whose declared `Fault` contradicts its status takes that fault's
class, and `fault` and `kind` still state each. `ApiFailure` is no `Throwable`, so a later style,
such as a `Result` return, or exceptions typed per body for a spec whose errors are named, is one
more value of `errorStyle`: a template and a signature over the same value, with no plan or IR
change.

**The kinds are the taxonomy's.** `Policy.Errors`' default table names `BadRequest` 400,
`Unauthorized` 401, `Forbidden` 403, `NotFound` 404, `Conflict` 409, `UnprocessableEntity` 422,
`TooManyRequests` 429, `InternalServer` 500 and `ServiceUnavailable` 503. A configuration adds rows,
and a protocol with statuses of its own has rows of its own. A range such as `4XX` is no obstacle:
the status that arrives is exact.

**Every failure has one place**, which the status and the entry it takes decide (§3.1):

- Kotlin's class is its kind's where the kind nests under its `Fault`, else its fault's, else the
  root's, and Go's `errors.Is` answers its kind and its fault each;
- `Fault` is the entry's, else the class the status states: 4xx `client`, 5xx `server`, any other
  none, which is what `Policy.Errors` classifies by default (§6.3). A `default` reads this way, and
  so does any status but a 2xx that no entry answers (§3.1);
- a `value` body decodes into `Body`, whatever `Type`'s kind, and a `void` or `raw` one leaves it
  nil, with `RawBody` holding whatever arrived;
- a body that does not decode never hides the failure: `Body` stays nil and `BodyErr` holds the
  decode error, since the status is what the caller must learn.

A body is buffered up to `Policy.Errors`' bound and no further, so an endless one cannot exhaust the
caller; `RawBody` holds what was read, and a body cut there is one that does not decode. A union
body is its struct (§4.4), and being read it is `Open`, so a new variant in an error reaches the
caller as one rather than as a decode failure. `Headers` holds a struct of what the entry declares,
named by its operation and case (`GetZone429Headers`) and read as a success's are (§8.9), so two
cases sharing a body may declare different headers.
