# Operations in Go

## The request type

Go's request type ([request shaping](../../refine/requests.md)) is a `<Operation>Params` struct.

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
`func (c *Client) TailMetricsStream(ctx) (*Stream[Metrics], error)` — no request type, since the
operation has no input ([request shaping](../../refine/requests.md)). `Stream[T]` is the runtime's:
`Recv() (T, error)` returns each element and then `io.EOF`, and `Close` releases the response, which
a caller that stops early must do. What `T` is follows from `Recv`:

- an `Element` is what its type lowers to. A union's is its struct
  ([`Payment`](unions.md#payment-in-go)), and `Recv` returns `io.EOF` after a variant whose
  `Variant.Event.Terminal` is true, so an event union needs nothing of its own beyond that.
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
`ResultPath`. The poll loop's backoff between attempts is `lro.go.tmpl`. Printer emits
`StartExport`, `StartExportParams`, and `ExportPoller` + `Poll()/PollUntilDone()`.

## The client

The client's settings ([the client](../../operations/client.md)) are functional options —
`NewClient(WithRegion(RegionEU))`, `WithServer(…)`, `WithBaseURL(u)`.
