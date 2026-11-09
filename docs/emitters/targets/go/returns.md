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
- **An envelope.** `GetZone` returns `*Zone`, and `GetZoneWithResponse` returns
  `*EnvelopedResponse[*Zone, GetZoneHeaders, *GetZoneResponse]`. The runtime's
  `EnvelopedResponse[T, H, E]` embeds `Response[T, H]` and adds `Envelope E`, so the envelope's
  `Messages` need no type assertion. A response the method returns with no envelope, such as a
  `204`, leaves `Envelope` nil, which `*GetZoneResponse` already holds, and `E` is `any` only where
  the responses it returns are wrapped in different envelope types.
- **Another declared response.** With no body, `DeleteMethod` returns a nil `*Deleted` and a nil
  error, and a stream's `Recv` returns `io.EOF`.

The runtime's `Response`, `EnvelopedResponse`, `OtherResponse` and `APIError`
([failures](errors.md)) hold `RawHeader` and a body, so each takes from its response decoder the
wire names of the headers its entry redacts, whose values it shows as `[REDACTED]`, and shows `Body`
and `Headers` through their own forms ([redaction](../../refine/redaction.md#a-redacted-value)).
`Response` and `EnvelopedResponse` declare `String`, `GoString` and `LogValue`. `OtherResponse` and
`APIError` are Go errors, whose `Error` quotes neither and is what `fmt`'s `%v` and `log/slog` read,
so they declare `GoString` alone, which `%#v` reads instead of dumping `RawHeader` and `RawBody`.

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
