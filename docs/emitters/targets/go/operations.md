### 8.3 A paginated operation (`ListUsers`, cursor pagination)

IR: `Operation{ Params:[limit, cursor], Responses:[200 → {items:[]User, next_cursor}],
Pagination{Strategy:cursor, InputCursor:{cursor}, Items:{body→items}, NextCursor:{body→next_cursor}} }`,
bound `HTTPBinding{GET /users, query limit+cursor}`.

- **plan.** `OpPlan{ Return:{Shape:value, Type:ListUsersResponse}, Page:&PagePlan{Strategy:cursor,
  ItemType:User, ItemPath:body→items, CursorIn:cursor, CursorOut:next_cursor} }`: each page decodes
  as the response's own type, and `Page` takes the items from it. Item-type unwrap is a plan
  decision. Pacing delay is **not** here — it is runtime policy.
- **refine.** `BuildRequestShaping` builds the request from the query bindings. By the default
  parameter shape (§4.13), marked `Inferred`, `limit` and `cursor` are fields of `ListUsersParams`.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) iter.Seq2[User, error]` is a range-over-func
  auto-pager: it walks pages using `CursorOut` to read `next_cursor` and `CursorIn` to feed it back,
  yields each item deserialized elementwise over `ItemPath`, yields one non-nil error and stops when
  a fetch fails, and fetches no further page once the caller's loop breaks.
- **emit.** Printer emits `ListUsers` and `ListUsersParams`; the pager needs no named iterator type.
  The pager *runtime* (fetch-and-pace loop) is `pagination.go.tmpl` parameterised by
  `Policy.Pagination.AutoPageDelay` — the legitimate boilerplate seam.

### 8.4 A streaming operation (`TailMetrics`, JSON or server-sent events)

IR: `Operation{ Streaming:server, ResponseStream:&StreamDetail{ Events:&TypeRef→Metrics },
Responses:[200 → {application/json → MetricsPage, text/event-stream → Metrics}] }`, bound
`HTTPBinding{GET /metrics}`. No binding field states the framing: the content's `MediaType` does.

- **plan.** `OpPlan{ Return:{Shape:value, Content:application/json, Type:MetricsPage},
  Stream:&StreamPlan{ Direction:server, Recv:&StreamSide{ Content:text/event-stream, Framing:sse,
  Element:Metrics } } }`. The ranking elects JSON for `Return`, and `Recv` holds the stream beside
  it (§3.1).
- **refine.** Each shape an operation offers is a method, sending the `Accept` of its content:
  `func (c *Client) TailMetrics(ctx) (*MetricsPage, error)` and
  `func (c *Client) TailMetricsStream(ctx) (*Stream[Metrics], error)` — no request type, since the
  operation has no input (§4.13). An operation whose elected content streams has the one method,
  under its own name. `Stream[T]` is the runtime's: `Recv() (T, error)` returns each element and
  then `io.EOF`, and `Close` releases the response, which a caller that stops early must do. A frame
  that does not decode ends the stream with an `OtherResponse` holding that frame (§3.1). What
  `T` is follows from `Recv`:
  - an `Element` lowers as a field of its type does (§9), whatever its kind. A union's is its struct
    (§8.2), and `Recv` returns `io.EOF` after a variant whose `Variant.Event.Terminal` is true, so
    an event union needs nothing of its own beyond §8.2. Under `FrameMembers` the element decodes
    from the frame's `data`, `event`, `id` and `retry`, and otherwise from its `data`.
  - no `Element` is `Stream[string]`, each frame's text, unread.
  - a `bytes` side has no frames and no `Stream`: `Recv` lowers as a file does, to `*RawBody`
    (§8.9), and `Send` to an `io.Reader` field of the request type.

  Direction comes from the *core* (§3.1), and the framing from the plan. A client stream gives
  `Send(T)` and a `CloseAndRecv` that returns what `Return` reads; a bidi operation has both sides,
  each with its own element and framing; and a gRPC binding of the same core would reuse the same
  `Stream` shape over length-prefixed frames. A sentinel frame that ends a stream without being a
  variant has no IR home yet (#666), so a target cannot tell one from an element; nor does the IR
  say which input selects a stream, where a request field does (#667).
- **emit.** Printer emits the methods and the element's declarations. The stream *runtime* is
  boilerplate behind the `Transport` interface: `stream.go.tmpl`, and one template per framing the
  document uses, which refine picks rather than a template branching on it (§5.2).

### 8.6 Auth (OR-of-ANDs, service default with an op override)

IR: `Service.Auth = [ AuthRequirement{Schemes:[apiKey]} ]`;
`Operation.Auth = [ AuthRequirement{Schemes:[oauth2(scopes:[read])]},
AuthRequirement{Schemes:[apiKey]} ]` (priority order); auth schemes in `Document.Auth`.

- **plan.** `OpPlan.Auth` = the operation's options, priority-ordered (the op overrides the service
  default). An empty option inside a non-empty list = "no auth is one acceptable choice"; an empty
  `Operation.Auth` slice would mean "explicitly public".
- **refine.** Auth *schemes* are structural IR, so refine emits an `Authenticator` interface, a
  per-scheme option type (`WithAPIKey(...)`, `WithOAuth2(...)`), and the request-time application
  code that satisfies the AND-within-option / OR-across-options-by-priority contract. Scope sets from
  `oauth2` flows reach the option types. What refine does **not** emit is any credential value or
  token-refresh behavior — the actual secrets and token acquisition are runtime configuration, not
  IR. SASL/x509/user_password schemes (Kafka/AMQP) lower the same way for a messaging target.
- **emit.** Printer emits the `Authenticator` interface, the option constructors, and the per-request
  auth selector.

### 8.7 A long-running operation (`StartExport`, status-monitor polling)

IR: `Operation{ LongRunning:&LongRunning{ FinalStateVia:"status-monitor", PollingOperation:GetExport.ID,
FinalType:&TypeRef→Export, ResultPath:body→result }, Request:{ExportRequest} }`, bound
`HTTPBinding{POST /exports}`; the
declared poll op `GetExport` is `HTTPBinding{GET /exports/{id}}` returning an `ExportStatus` monitor.

- **plan.** `OpPlan{ Return:{Shape:value}, LRO:&LROPlan{ FinalStateVia:"status-monitor",
  PollOp:GetExport.ID, FinalType:Export, PollType:ExportStatus, ResultPath:body→result } }`. Which
  response signals terminal state, and where the result lives, are plan facts unwrapped from the
  monitor type — the same "classify, don't decide" split pagination uses. Poll *pacing* is **not**
  here; it is runtime policy, exactly like `Pagination` pacing (§8.3).
- **refine.** `LowerLRO` emits a poller:
  `func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller, error)`, where
  `ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
  `ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
  `ResultPath`. `FinalStateVia` selects how the poller finds the monitor URL (status-monitor vs
  `operation-location` header vs original-URI). The poller *type* and result extraction are typed AST;
  the poll loop's backoff between attempts is `lro.go.tmpl` boilerplate parameterised by policy — the
  same structure/runtime line as the iterator (§8.3) and the stream reader (§8.4). Smithy waiters are
  *not* folded in here: their acceptor/JMESPath lists arrive verbatim in `Operation.Unmodeled`
  (`ir-design.md §7.3`) and a waiter-aware refiner is a separate, opt-in step.
- **emit.** Printer emits `StartExport`, `StartExportParams`, and `ExportPoller` +
  `Poll()/PollUntilDone()`. `OneWay` operations take the opposite path — no poller, no response
  binding, a fire-and-forget method — read straight from `Operation.OneWay`.
