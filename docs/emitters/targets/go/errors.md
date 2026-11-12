# Failures in Go

## How a failure reaches its caller

Go needs no setting, since an `error` is already a value.

## `RateLimited` in Go

The IR and the plan are in [failures](../../operations/errors.md#an-error-taxonomy).

The runtime type is `APIError`, which the operation's response decoder fills from the entry a
response takes:

```go
type APIError struct {         // the runtime's: what one failed response said
    StatusCode int
    Raw                        // RawHeader() and RawBody(): the headers and body as they arrived
                               // (returns.md#what-a-response-prints)
    BodyErr    error           // why the body did not decode; nil when it did, or had none
    Fault      Fault           // the entry's, else the status's class
    Kind       Kind            // the taxonomy's logical kind for the status; "" = none
    Body       any             // the body as its Type lowers, of any kind; nil = none decoded
    Headers    any             // the entry's declared headers, a struct; nil = none declared
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

`Headers[H]` reads the same way, and `(*APIError).Is` answers `errors.Is` for `runtime.ErrClient`,
`runtime.ErrServer` and a sentinel per kind (`runtime.ErrNotFound`), each declared in the
[runtime's unit](../../refine/output-units.md#the-runtimes-unit).

Printer emits the decoder, registering each entry: `429 → RateLimited`, with its headers struct,
fault and retry facts. The decoder hands the entry's `Retryable` and `Throttling` to the send loop
beside the value, not inside it: `APIError` has exported fields alone, so a decoder in another
package builds it by a literal ([the runtime](overview.md#what-a-caller-reaches-of-the-runtime)).

A caller branches on what happened, and reads the body when it needs it:

```go
switch {
case errors.Is(err, runtime.ErrNotFound): // …
case errors.Is(err, runtime.ErrClient):
    if p, ok := runtime.Body[sdk.Problem](err); ok { show(p) }
}
```

## A failure's place

`errors.Is` answers a failure's kind and its fault each.
