# Operations in Go

## The request type

Go's request type ([request shaping](../../refine/requests.md)) is a `<Operation>Params` struct. An
input no style arranges ([values as text](../../refine/text.md#a-value-no-style-arranges)) is a
`string` field, or a `[][2]string` of names and values where its position carries pairs.

## Pagination

The IR, the plan and what every target generates are in
[pagination](../../operations/pagination.md).

- **refine.** `BuildRequestShaping` builds the request from the query bindings. By the default
  parameter shape ([request shaping](../../refine/requests.md)), marked `Inferred`, `limit` and
  `cursor` are fields of `ListUsersParams`.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) iter.Seq2[User, error]` is a range-over-func
  auto-pager: it yields one non-nil error when a fetch fails, and fetches no further page once the
  caller's loop breaks.
- **emit.** Printer emits `ListUsers` and `ListUsersParams`; the pager needs no named iterator type.
  The pager *runtime* is `pagination.go.tmpl`.

## Streaming

The IR, the plan and what every target generates are in [streaming](../../operations/streaming.md).

`TailMetrics` has two methods: `func (c *Client) TailMetrics(ctx) (*MetricsPage, error)` and
`func (c *Client) TailMetricsStream(ctx) (*runtime.Stream[Metrics], error)` — no request type, since
the operation has no input ([request shaping](../../refine/requests.md)). `Stream[T]` is the
runtime's: `Recv() (T, error)` returns each element and then `io.EOF`, and `Close` releases the
response, which a caller that stops early must do. A `Recv` that gets no byte for
`Timeout.Idle` fails with a timeout ([runtime
policy](../../contract/runtime-policy.md#what-a-layer-writes)): `net/http` gives a response's
body no read deadline, so the stream and a `RawBody` read through a wrapper that cancels the
request when one read waits past the bound, and a stream the caller writes times each write the
same way. Measured with Go 1.27.3 and a bound of 300ms, a reader of a stream held open after one
frame returned at 300ms, and without the wrapper only when the server closed the stream; one
whose server wrote a comment line every 100ms read on; and a `Send` to a server that had stopped
reading returned at 300ms, and without the bound had not after 1.5s. What
`T` is follows from `Recv`:

- an `Element` is what its type lowers to. A union's is its struct
  ([`Payment`](unions.md#payment-in-go)), and `Recv` returns `io.EOF` after an element holding a
  variant whose `Variant.Event.Terminal` is true, so an event union needs nothing of its own beyond
  that.
- no `Element` is `Stream[string]`, each frame's text, unread.
- a `bytes` side has no frames and no `Stream`: `Recv` lowers as a file does, to `*RawBody` ([return
  shapes](returns.md)), and `Send` to an `io.Reader` field of the request type.

A client stream gives `Send(T)` and a `CloseAndRecv` that returns what `Return` reads.

Printer emits the methods and the element's declarations. The stream *runtime* is `stream.go.tmpl`,
and one template per framing the document uses.

## Auth

Go's per-scheme option types ([auth](../../operations/auth.md)) are `WithAPIKey(...)` and
`WithOAuth2(...)`.

## Long-running operations

The IR, the plan and what every target generates are in [long-running
operations](../../operations/long-running.md).

`LowerLRO` emits a poller:
`func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller, error)`, where
`ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
`ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
`ResultPath`. The poll loop is `lro.go.tmpl`, paced and bounded by `Policy.Polling` ([runtime
policy](../../contract/runtime-policy.md#what-a-layer-writes)): `PollUntilDone` returns an error
naming `MaxPolls` after that many polls, and the poller polls on if called again. Printer emits
`StartExport`, `StartExportParams`, and `ExportPoller` + `Poll()/PollUntilDone()`.

## Stability

Each method an `alpha` or `beta` operation lowers to, its `WithResponse` method among them, gives
its tier a paragraph of its doc comment after the summary
([stability](../../operations/stability.md)), such as `Beta: breaking changes are possible but not
expected.`, so the comment still opens with the sentence summarizing the method, as a Go doc comment
does, and `go doc` and an editor show the tier at the call. Go has no annotation a caller must
acknowledge, and a build tag compiling an `alpha` method only under `-tags alpha` would split the
package's API by tag, every test then building twice, so Go gates nothing.

## Operations the API sends

A service's receiver ([operations the API sends](../../operations/inbound.md)) is `Receiver`, built
by `NewReceiver(opts ...ReceiverOption)` with an option for each scheme its operations check, named
for the scheme as the client's are, and `WithVerify(func(http.Header, []byte) error)` for the user's
check. A parse method takes the `*http.Request` every Go HTTP server hands a handler, `func (r
*Receiver) ParseZoneCreated(req *http.Request) (*webhooks.ZoneCreatedRequest, error)`, and a refused
check is an error `errors.Is` matches to the runtime's `ErrUnverified`. A response type writes
itself with `func (resp *ZoneCreated200Response) WriteTo(w http.ResponseWriter) error`. With Go
1.27.3, a handler built from them across a root and a group package answered a valid request 200, a
wrong key and a wrong HMAC signature 401, and a valid body past the bound 400, which with the bound
removed it answered 200.

## The client

The client's settings ([the client](../../operations/client.md)) are functional options —
`NewClient(WithRegion(RegionEU))`, `WithServer(…)`, `WithBaseURL(u)`, and `WithHook(h)` for the
hook its events go to ([what an event
holds](../../contract/runtime-policy.md#what-an-event-holds)). A [client
setting](../../operations/client.md#client-settings) is one more, `WithAccountID("…")`, and the
request type's field for it a pointer, `AccountID *string`, nil taking the client's.
