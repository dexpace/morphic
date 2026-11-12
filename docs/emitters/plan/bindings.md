# BindingView — the per-protocol request/response projection

`Parameter`s carry no protocol location ([operations](../../ir/operations.md)). The plan derives,
from `OpBindings`, a neutral per-protocol view a refiner turns into a request builder: which params
are path/query/ header/cookie/body, their style/explode/prefix, escaping, empty value and media
type, RPC message folding, message channel + reply, the GraphQL entry point, the OTP call/cast
shape. One operation may carry several bindings (gRPC transcoding `additional_bindings`, HTTP+RPC);
all are represented, in the order below. This resolves [open question 6](../../ir/open-questions.md)
on the emitter side: status conditions and `StatusCodeProp` are read through the HTTP binding a
target lowers, so RPC/OTP single-response ops with empty conditions are handled uniformly.

```go
type BindingView struct {
    All      []ProtoBinding  // every binding: OpBindings.HTTP in its order, then RPC, Message,
                             // GraphQL and OTP (below)
}
type ProtoBinding struct {
    Protocol Protocol        // the OpBindings field this binding comes from
    Params   []ParamBind     // HTTP's neutralized param placement (one per param, per location)
    ResponseBodyPath *ir.PropPath // HTTPBinding.ResponseBodyPath: the response body is this
                                  // sub-field of the payload; nil = the whole payload
    // The binding itself, by reference, not restructured; exactly the one Protocol names is set:
    HTTP     *ir.HTTPBinding    // method/URITemplate/status map
    RPC      *ir.RPCBinding     // System says which RPC system
    Message  *MsgView           // channel + direction + reply
    GraphQL  *ir.GraphQLBinding // the entry point: query, mutation or subscription, and its field
    OTP      *ir.OTPBinding     // the behaviour, call/cast/info, the process and the request tag
}
type ParamBind struct {
    Param    ir.ParamID      // the bound Operation.Params entry; one name may be two params
    Where    ir.HTTPLocation // where it is sent, in HTTPParamBinding.Location's vocabulary
    WireName string          // never a rendered identifier (INV4)
    Style    string; Explode *bool; Prefix string   // form/label/matrix/deepObject serialization
    AllowReserved, AllowEmptyValue bool             // the binding's escaping and empty-value facts
    PathPattern string       // HTTPParamBinding.PathPattern: a pattern of several segments is
                             // escaped by its own rule (../refine/text.md#escaping)
    Text     bool            // no Style arranges a value of the param's type, so the input is its
                             // caller's text: a string in a path or a header, pairs of strings
                             // elsewhere (../refine/text.md#a-value-no-style-arranges)
    ContentType string       // a content-style param's media type; "" = serialized by Style
    BodyPath []ir.PropID     // body_property: where in the body model the param lands
}
type MsgView struct {
    Channel ir.ChannelID; Direction ir.MsgDirection; Reply *ir.Reply; Messages []ir.MessageID
    Bindings map[string]ir.RawConfig // MessageBinding.Bindings, raw as the IR keeps them
}
type Protocol string         // "http" | "rpc" | "message" | "graphql" | "otp"
```

The traces in these documents exercise `Protocol == "http"`; the RPC/message/OTP arms exist in the
type so the second-protocol emitter ([open question 4](../open-questions.md)) does not force a plan
schema change (INV9). Channels and messages ([messaging](../../ir/messaging.md)) reach an emitter
through `MsgView`; a messaging target consumes them the same way an HTTP target consumes `HTTP`.

A `Protocol` names the `OpBindings` field a binding comes from, never the system speaking it: `rpc`
is every `RPCBinding`, gRPC, Connect, JSON-RPC and Smithy RPC alike, and its `RPC.System` says which
([RPC binding](../../ir/rpc-binding.md)). A label for each system was rejected: `System` is an open
string, so the vocabulary would be open too and no switch over `Protocol` could be complete, while
labelling every system `grpc`, as the vocabulary once did, handed a gRPC target a JSON-RPC method to
lower as gRPC.

**The order.** The IR orders an operation's HTTP mappings, the primary first, and nothing across
`OpBindings`' fields, which hold one RPC, message, GraphQL and OTP binding each. So `All` is a rule
over the fields: `HTTP` in its order, then `RPC`, `Message`, `GraphQL` and `OTP`, whatever order the
source wrote them in. "The binding the source states first" was rejected as underivable: a Protobuf
method states its RPC binding and its transcoding in one declaration, and no field records which of
a Smithy service's protocol traits came first.

**The binding a target lowers** is the first of `All` its
[capabilities](../contract/registry.md#capabilities) cover, an RPC binding only where `RPCSystems`
lists its `System`. Every target's reader of an operation's binding reads that one, the request's
path parameters ([request shaping](../refine/requests.md)), a stream's frames
([streams](streams.md)) and the mock's routes ([docs and
mocks](../docs-and-mock.md#what-the-mock-streams)) among them, so a target covering `http` lowers
`HTTP[0]` wherever there is one, and the mock routes the binding an HTTP SDK sends. A plan fact,
which every target shares, reads `All` instead, as `Effects` takes only what every binding states
([plan policy](policy.md#an-operations-effects-come-from-its-binding)), or `HTTP[0]`, as a `HEAD`
response's `void` does ([responses](responses.md)). An HTTP target reaches a gRPC method through its
transcoding, as does a target covering `rpc` and `http`, and one listing `rpc` with `grpc` alone
reaches it as gRPC. An operation none of whose bindings a target covers is a warning
`emitters/<target>/unsupported-binding` naming the operation and its bindings, the mock's included,
with nothing generated for it (T-68 holds these). `RPC` first was weighed: either order moves a
target covering both protocols under some additive change, a gRPC method gaining a transcoding under
this one and a Smithy service gaining an RPC protocol under that, and no IR fact says which protocol
a source prefers, so `All` keeps the order the IR states first, `HTTP`'s, at its head. Reading the
order from the target's `Protocols` was rejected too: that list is a set the registry checks, and
its order would decide what a target generates while no check reads it.

No `ProtoBinding` holds a binding as `ir.RawConfig`: the GraphQL entry point and the OTP call shape
are typed in the IR, so the plan hands them over by reference as it does HTTP's and RPC's. A
`RawConfig` reaches a target only where the IR declares one, a message binding's per-protocol
`Bindings` (kafka's `groupId`, say), whose shape the IR deliberately leaves open. The `Unmodeled`
map each binding struct carries (the [binding documents](../../ir/bindings.md)) is the other thing
entirely: the escape hatch for constructs the IR does *not* model — a directive applied to a
GraphQL entry-point field, say — where `Reason` and `Provenance` are exactly what a policy layer
selects on. A refiner reads both, for different purposes.

Sub-clients and fluent navigation are built from the `OperationGroup` tree itself — its nesting,
names, operations and Smithy `Resource` — which the IR states, so the plan mirrors none of it ([plan
or refine](plan-or-refine.md)).
