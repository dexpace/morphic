# Long-running and one-way operations

## A long-running operation

`StartExport`, status-monitor polling.

IR: `Operation{ LongRunning:&LongRunning{ FinalStateVia:"status-monitor", PollingOperation:GetExport.ID,
FinalType:&TypeRef→Export, ResultPath:body→result }, Request:{ExportRequest} }`, bound
`HTTPBinding{POST /exports}`; the
declared poll op `GetExport` is `HTTPBinding{GET /exports/{id}}` returning an `ExportStatus` monitor.

`OpPlan{ Return:{Shape:value}, LRO:&LROPlan{ FinalStateVia:"status-monitor", PollOp:GetExport.ID,
FinalType:Export, PollType:ExportStatus, ResultPath:body→result } }`. Which response signals
terminal state, and where the result lives, are plan facts unwrapped from the monitor type — the
same "classify, don't decide" split pagination uses. Poll *pacing* is **not** here; it is runtime
policy, exactly like `Pagination` pacing ([pagination](pagination.md)).

## What a target generates

`LowerLRO` emits a poller ([Go](../targets/go/operations.md#long-running-operations),
[Kotlin](../targets/kotlin/operations.md)): one poll through the `PollOp` binding, and a loop until
terminal that extracts the final type from `ResultPath`. `FinalStateVia` selects how the poller
finds the monitor URL (status-monitor vs `operation-location` header vs original-URI). The poller
*type* and result extraction are typed AST; the poll loop's backoff between attempts is boilerplate
parameterised by policy — the same structure/runtime line as the iterator
([pagination](pagination.md)) and the stream reader ([streaming](streaming.md)). Smithy waiters are
*not* folded in here: their acceptor/JMESPath lists arrive verbatim in `Operation.Unmodeled`
([interaction-patterns](../../ir/interaction-patterns.md)) and a waiter-aware refiner is a separate,
opt-in step.

`OneWay` operations take the opposite path — no poller, no response binding, a fire-and-forget
method — read straight from `Operation.OneWay`.
