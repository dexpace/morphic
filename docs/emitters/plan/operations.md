# OpPlan — the fixed per-operation decision set

Adopted from oagen's production `OperationPlan` (prior-art §1), the decision set the emitters
demonstrably needed:

```go
type OpPlan struct {
    PrimaryResponse *ir.Response      // SDK-ergonomic pick; IR keeps ALL responses
    Return          ResponsePlan      // how the primary response is read
    Others          []PlannedResponse // every other response the operation declares (responses.md)
    RequestContent  *ir.Content       // the request content elected; nil = no body
    Errors          []PlannedError    // every failure the operation declares, ranges collapsed
                                      // (404/4XX/catch-all)
    Page            *PagePlan         // nil = not paginated
    Stream          *StreamPlan       // nil = neither direction streams (docs/ir/interaction-patterns.md)
    LRO             *LROPlan          // nil = not long-running
    Auth            []AuthOption      // OR-of-ANDs, priority-ordered (../operations/auth.md)
    Effects         Effects           // what calling it may change: declared, else by method
                                      // (policy.md)
    Bindings        BindingView       // per-protocol param/request/response view from OpBindings
    Inferred        map[string]ir.Provenance  // which fields above were heuristic (INV6)
}

// The remaining operation-facet plans. Each is nil when the facet is absent, so a refiner's
// presence check is a nil check, and each carries only classified facts — never a target type.

type PagePlan struct {                // item-type unwrap is a PLAN decision
    Strategy   ir.PageStrategy
    ItemType   *ir.TypeRef            // element type items resolve to, unwrapped from the envelope
    ItemPath   *ir.PropPath           // where items live — a PATH (may be nested / in headers)
    CursorIn   *ir.ParamPath
    CursorOut  *ir.PropPath
    Inferred   bool                   // heuristic pagination (may be disabled by policy)
}

type LROPlan struct {                 // maps ir.LongRunning (docs/ir/interaction-patterns.md)
    FinalStateVia string              // "operation-location" | "status-monitor" | "original-uri" | …
    PollOp     *ir.OpID               // declared polling operation; nil = poll the start op's monitor
    FinalOp    *ir.OpID               // declared final-result operation; nil = result is inline
    PollType   *ir.TypeRef            // status-monitor type
    FinalType  *ir.TypeRef            // what PollUntilDone resolves to
    ResultPath *ir.PropPath           // where the final result lives in the terminal response
}

type Effects struct {
    Safe       bool                   // asks for no change on the server (RFC 9110 §9.2.1)
    Idempotent bool                   // a second call changes nothing the first did not
                                      // (RFC 9110 §9.2.2); true of every safe operation
}

type AuthOption struct {              // one option in the OR-of-ANDs; slice order is PRIORITY order
    Schemes []ir.SchemeUse            // ALL must be satisfied together; empty = "no auth is acceptable"
}
```

How `Return`, `Others` and `Errors` read a response is in [responses](responses.md), and
`StreamPlan` is in [streams](streams.md).

`PrimaryResponse` and the contents a `ResponsePlan` elects are the *one legitimate home* for the
collapse oagen performs destructively in its compiler and Morphic forbids there (INV2): the IR keeps
every response and content type whole; "pick a primary for the ergonomic method" happens here,
lowered late. The content ranking (`json > form > multipart > binary`) is a shared, overridable plan
helper (resolves [open question 3](../../ir/open-questions.md)).

Some facts oagen's plan carries are not here, by the [plan-or-refine test](plan-or-refine.md).
Whether there is a body and whether the operation is one-way are stated by the IR, so a refiner
reads them there, `Operation.Request` and `Operation.OneWay`, as it reads the idempotency-token
parameter `Operation.Idempotency` names. Whether the operation is safe or idempotent is `Effects`,
since the IR states it for few operations and a method states it for most ([plan
policy](policy.md#an-operations-effects-come-from-its-method)). And whether an operation's inputs
are passed positionally or inside an options object differs by language, so it is each target's
([request shaping](../refine/requests.md)); the neutral facts it is decided from are already here —
`BindingView` says where each input binds, and `Parameter.Required` and `Payload.Required` which are
required — so moving the decision out added no field.
