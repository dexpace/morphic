# BindingView — the per-protocol request/response projection

`Parameter`s carry no protocol location (`ir-design.md §7.2`). The plan derives, from `OpBindings`,
a neutral per-protocol view a refiner turns into a request builder: which params are path/query/
header/cookie/body, their style/explode/prefix, escaping, empty value and media type, RPC message
folding, message channel + reply, the GraphQL entry point, the OTP call/cast shape. One operation
may carry several bindings (gRPC transcoding `additional_bindings`, HTTP+RPC); all are represented,
primary first. This resolves `ir-design.md` open Q6 on the emitter side: status conditions and
`StatusCodeProp` are read through the HTTP `BindingView`, so RPC/OTP single-response ops with empty
conditions are handled uniformly.

```go
type BindingView struct {
    Primary  ProtoBinding    // the binding a single-protocol SDK uses
    All      []ProtoBinding  // every binding, primary first (transcoding, dual HTTP+RPC)
}
type ProtoBinding struct {
    Protocol string          // "http" | "grpc" | "message" | "graphql" | "otp"
    Params   []ParamBind     // neutralized param placement (one per bound param, per location)
    ResponseBodyPath *ir.PropPath // HTTPBinding.ResponseBodyPath: the response body is this
                                  // sub-field of the payload; nil = the whole payload
    // Protocol-specific handles kept by ID/reference, not restructured:
    HTTP     *ir.HTTPBinding // method/URITemplate/status map, when Protocol == http
    RPC      *ir.RPCBinding
    Message  *MsgView        // channel + direction + reply, when Protocol == message
    Raw      ir.RawConfig    // GraphQL entry point / OTP call-cast tag / deployment bindings, verbatim
}
type ParamBind struct {
    Param    ir.ParamID      // the bound Operation.Params entry; one name may be two params
    Where    string          // path | query | querystring | header | cookie | body |
                             // body_property | host
    WireName string          // never a rendered identifier (INV4)
    Style    string; Explode *bool; Prefix string   // form/label/matrix/deepObject serialization
    AllowReserved, AllowEmptyValue bool             // the binding's escaping and empty-value facts
    ContentType string       // a content-style param's media type; "" = serialized by Style
    BodyPath []ir.PropID     // body_property: where in the body model the param lands
}
type MsgView struct { Channel ir.ChannelID; Direction ir.MsgDirection; Reply *ir.Reply; Messages []ir.MessageID }
```

The traces in these documents exercise `Protocol == "http"`; the RPC/message/OTP arms exist in the
type so the second-protocol emitter ([open question 4](../open-questions.md)) does not force a plan
schema change (INV9). Channels and messages (`ir-design.md §8.3`) reach an emitter through
`MsgView`; a messaging target consumes them the same way an HTTP target consumes `HTTP`.

`Raw` is `ir.RawConfig`, not `ir.Unmodeled`, and the distinction is not cosmetic. It carries
*declared* protocol configuration the plan chooses to pass through unstructured — the GraphQL
entry point, the OTP call/cast tag, AsyncAPI deployment bindings — all of which the IR models
fields for; there is no unmodeled construct to give a reason for, and the source position a
consumer would want is the owning binding's. The `Unmodeled` map each binding struct carries
(`ir-design.md §8.1`–`§8.5`) is the other thing entirely: the escape hatch for constructs the IR
does *not* model — a directive applied to a GraphQL entry-point field, say — where `Reason` and
`Provenance` are exactly what a policy layer selects on. A refiner reads both, for different
purposes.

Sub-clients and fluent navigation are built from the `OperationGroup` tree itself — its nesting,
names, operations and Smithy `Resource` — which the IR states, so the plan mirrors none of it ([plan
or refine](plan-or-refine.md)).
