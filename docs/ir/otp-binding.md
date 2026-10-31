# Erlang/OTP binding

Part of the [IR design](./README.md). A behaviour operation, and the conventions an OTP compiler
lowers by.

```go
type OTPBinding struct {
    Behaviour  string    // "gen_server" | "gen_statem" | "gen_event"
    Kind       string    // "call" (synchronous request→reply) | "cast" (fire-and-forget; the
                         // operation also sets OneWay) | "info" (raw message send)
    Process    ChannelID // the channel modeling the target process: Address = registered name
                         // (nil Address = unregistered/runtime pid); registration kind
                         // (local/global/via) in Channel.Bindings["otp"]
    RequestTag *Value    // tag of the request tuple (a symbol Value, e.g. 'get');
                         // nil = the whole term is the request
    Unmodeled  Unmodeled  // source constructs the IR does not model, kept verbatim (docs/ir/unmodeled.md)
}
```

An OTP compiler consumes module type information (`-spec`/`-type` on behaviour callbacks and
API functions). Lowering conventions: a gen_server ≈ one `Channel`; `handle_call`/`handle_cast`
APIs are `Operation`s with an `OTPBinding`; unsolicited `handle_info` messages are channel
`Messages` consumed via `MessageBinding{Direction: receive}`; a gen_event manager is a Channel
with one message shape per event. Tagged-tuple protocols (`{ok, V} | {error, R}`) are `WireTagged`
Unions of Tuples discriminated by `Discriminator.Index`; records are `Model{Positional: true}`
with the record tag as an index-1 `Literal`-typed property. Splitting a tagged reply union into
`Responses` vs `Errors` (`{error, Reason}` is in-band data, not transport failure) is injectable
compiler policy, marked `Inferred`. Erlang `string()` is `[char()]` and must lower as a List of
a char-ranged scalar — never as PrimKind `string`. Delayed replies (`noreply` +
`gen_server:reply/2`) are invisible at the protocol surface and need no representation; call
timeouts and `multi_call`/`send_request` machinery are SDK runtime policy, not IR.
