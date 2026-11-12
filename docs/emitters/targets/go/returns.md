# Return shapes in Go

How each [return shape](../../operations/returns.md) lowers in Go.

- **A scalar, a list, a map or a union.** `func (c *Client) Count(ctx) (int64, error)`, `Labels`
  returning `map[string]string`, `GetPet` returning the struct `Pet` ([unions](unions.md)).
- **A file.** `func (c *Client) Download(ctx) (*runtime.RawBody, error)`, where the runtime's
  `RawBody` holds the response's `io.ReadCloser` and the media type it arrived as, and the caller
  closes it.
- **No body.** The method returns `error` alone.
- **Declared headers.** `GetZone` returns `*Zone` whatever the response declares, and a response
  that declares headers adds
  `func (c *Client) GetZoneWithResponse(ctx) (*runtime.Response[*Zone, GetZoneHeaders], error)`. The
  `Response`'s `Body` is `struct{}` for `void`, and its `Headers` is `struct{}` where they declare
  none.
- **An envelope.** `GetZone` returns `*Zone`, and `GetZoneWithResponse` returns
  `*runtime.EnvelopedResponse[*Zone, GetZoneHeaders, *GetZoneResponse]`. The runtime's
  `EnvelopedResponse[T, H, E]` embeds `Response[T, H]` and adds `Envelope E`, so the envelope's
  `Messages` need no type assertion. A response the method returns with no envelope, such as a
  `204`, leaves `Envelope` nil, which `*GetZoneResponse` already holds, and `E` is `any` only where
  the responses it returns are wrapped in different envelope types.
- **Another declared response.** With no body, `DeleteMethod` returns a nil `*Deleted` and a nil
  error, and a stream's `Recv` returns `io.EOF`.

## What a response prints

The runtime's `Response`, `EnvelopedResponse`, `OtherResponse` and `APIError`
([failures](errors.md)) hold a response's headers and body as they arrived, any of which may be a
credential ([what the runtime holds raw](../../refine/redaction.md#what-the-runtime-holds-raw)).
Each embeds the one type the runtime declares for them:

```go
type Raw state.Raw                   // the runtime's: a response as it arrived

func (r Raw) RawHeader() http.Header // every header, by wire name
func (r Raw) RawBody() []byte        // the body, up to Policy.Errors' bound
```

`state.Raw` is a struct whose one field is a function returning both ([the
runtime](overview.md#what-a-caller-reaches-of-the-runtime)), so no form shows either, whatever reads
the value. Measured with Go 1.27.3: a failure as a pointer, as a value and in an exported and an
unexported field of a caller's struct, the other three types, a sub-client and the client were
rendered under 23 `fmt` verbs and flags, `%O` and `%p` among them, `Sprint`, `encoding/json`,
`encoding/json/v2`, both `log/slog` handlers and `encoding/gob`, and none of the 464 renderings held
a header's value, the body's bytes or the base URL's password in any spelling, where 66 of 87 did
for a struct holding the same as plain fields. `encoding/json/v2` refuses the types (`Go struct has
no exported fields`) and `encoding/json` writes their other members. What a type declares beyond
that shows only what has a form of its own: `Response` and `EnvelopedResponse` declare `String`,
`GoString` and `LogValue`, showing the status and `Body` and `Headers` through theirs
([redaction](../../refine/redaction.md#a-redacted-value)), and `OtherResponse` and `APIError` are Go
errors, whose `Error` quotes neither.

Three other holders were built, and each leaks to some reader. Plain fields, with a `Format` method
on each type beside `GoString`, answer every verb `fmt` is handed the value under, and nothing once
the value is a field of a caller's struct that `fmt` cannot ask, nor an encoder. Unexported fields
print as exported ones do. A pointer to them prints as an address under `%v` and in full under a
verb `fmt` has no form for: `%O` of a failure began `%!O(*state.raw=&{map[ Set-Cookie:[…`. The
function's cost is that two responses holding equal bytes are unequal to `reflect.DeepEqual`, which
never finds two functions equal, a value and itself among them, so a test compares what `RawHeader`
and `RawBody` return; that `==` on a `Response` does not compile, a function being incomparable; and
that a test double a caller builds by literal holds no raw response, so the runtime exports
`NewRaw(h http.Header, body []byte) Raw`, which holds the caller's own values. A `RawBody` and a
`Stream`, which hold a response still open, are declared over state structs as a sub-client is, and
read it through their methods. T-65 holds this section.

## `OtherResponse`

```go
type OtherResponse struct {    // the runtime's: a response its method cannot return
    StatusCode int
    Raw                        // RawHeader() and RawBody(): what arrived (above)
    BodyErr    error           // why the body did not decode, a *DecodeError; nil when it did, or
                               // had none
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
`errors.AsType[*runtime.OtherResponse](err)` finds it.
