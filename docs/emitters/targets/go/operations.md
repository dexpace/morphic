# Operations in Go

## The request type

Go's request type (§4.13) is a `<Operation>Params` struct.

## Pagination

The IR and the plan are in [pagination](../../operations/pagination.md#a-paginated-operation).

- **refine.** `BuildRequestShaping` builds the request from the query bindings. By the default
  parameter shape (§4.13), marked `Inferred`, `limit` and `cursor` are fields of `ListUsersParams`.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) iter.Seq2[User, error]` is a range-over-func
  auto-pager: it walks pages using `CursorOut` to read `next_cursor` and `CursorIn` to feed it back,
  yields each item deserialized elementwise over `ItemPath`, yields one non-nil error and stops when
  a fetch fails, and fetches no further page once the caller's loop breaks.
- **emit.** Printer emits `ListUsers` and `ListUsersParams`; the pager needs no named iterator type.
  The pager *runtime* (fetch-and-pace loop) is `pagination.go.tmpl` parameterised by
  `Policy.Pagination.AutoPageDelay` — the legitimate boilerplate seam.

## Streaming

The IR, the plan and what every target generates are in [streaming](../../operations/streaming.md).

`TailMetrics` has two methods: `func (c *Client) TailMetrics(ctx) (*MetricsPage, error)` and
`func (c *Client) TailMetricsStream(ctx) (*Stream[Metrics], error)` — no request type, since the
operation has no input (§4.13). `Stream[T]` is the runtime's: `Recv() (T, error)` returns each
element and then `io.EOF`, and `Close` releases the response, which a caller that stops early must
do. What `T` is follows from `Recv`:

- an `Element` is what its type lowers to. A union's is its struct (§8.2), and `Recv` returns
  `io.EOF` after a variant whose `Variant.Event.Terminal` is true, so an event union needs nothing of
  its own beyond §8.2.
- no `Element` is `Stream[string]`, each frame's text, unread.
- a `bytes` side has no frames and no `Stream`: `Recv` lowers as a file does, to `*RawBody` (§8.9),
  and `Send` to an `io.Reader` field of the request type.

A client stream gives `Send(T)` and a `CloseAndRecv` that returns what `Return` reads.

Printer emits the methods and the element's declarations. The stream *runtime* is `stream.go.tmpl`,
and one template per framing the document uses.

## Auth

Go's per-scheme option types (§8.6) are `WithAPIKey(...)` and `WithOAuth2(...)`.

## Long-running operations

The IR, the plan and what every target generates are in [long-running operations](../../operations/long-running.md).

`LowerLRO` emits a poller: `func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller,
error)`, where `ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
`ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
`ResultPath`. The poll loop's backoff between attempts is `lro.go.tmpl`. Printer emits `StartExport`,
`StartExportParams`, and `ExportPoller` + `Poll()/PollUntilDone()`.

## The client

The client's settings (§8.10) are functional options — `NewClient(WithRegion(RegionEU))`, `WithServer(…)`, `WithBaseURL(u)`.
