# Return shapes in Go

How each [return shape](../../operations/returns.md) lowers in Go.

- **A scalar, a list, a map or a union.** `func (c *Client) Count(ctx) (int64, error)`, `Labels`
  returning `map[string]string`, `GetPet` returning the struct `Pet` ([unions](unions.md)).
- **A file.** `func (c *Client) Download(ctx) (*RawBody, error)`, where the runtime's `RawBody`
  holds the response's `io.ReadCloser` and the media type it arrived as, and the caller closes it.
- **No body.** The method returns `error` alone.
- **Declared headers.** `GetZone` returns `*Zone` whatever the response declares, and a response
  that declares headers adds
  `func (c *Client) GetZoneWithResponse(ctx) (*Response[*Zone, GetZoneHeaders], error)`. The
  `Response`'s `Body` is `struct{}` for `void`, and its `Headers` is `struct{}` where they declare
  none.
- **An envelope.** `GetZone` returns `*Zone`, and `GetZoneWithResponse`'s `Response` holds the
  envelope in `Envelope any`, a `*GetZoneResponse` there and nil on an operation with no envelope,
  as `OtherResponse.Body` holds a body of any kind.
- **Another declared response.** With no body, `DeleteMethod` returns a nil `*Deleted` and a nil
  error, and a stream's `Recv` returns `io.EOF`.

## `OtherResponse`

```go
type OtherResponse struct {    // the runtime's: a response its method cannot return
    StatusCode int
    RawHeader  http.Header     // every header as it arrived, by wire name
    RawBody    []byte          // the body as it arrived, up to Policy.Errors' bound
    BodyErr    error           // why the body did not decode; nil when it did, or had none
    Body       any             // the body as its Type lowers, of any kind; nil = none decoded
    Headers    any             // the entry's declared headers, a struct; nil = none declared
}

func (r *OtherResponse) Error() string {
    return fmt.Sprintf("response %d is not what this method returns", r.StatusCode)
}
func (r *OtherResponse) Unwrap() error { return r.BodyErr }
```

It takes Go's `error` path because the method's result has no place for it, and it is no
`*APIError`, so `errors.Is` answers neither fault, no retry reads it, and
`errors.AsType[*sdk.OtherResponse](err)` finds it.
