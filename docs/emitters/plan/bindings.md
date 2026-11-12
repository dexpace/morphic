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
    All      []ProtoBinding  // every binding: OpBindings.RPC, then HTTP in its order, then
                             // Message, GraphQL and OTP (below)
}
type ProtoBinding struct {
    Protocol Protocol        // the OpBindings field this binding comes from
    Params   []ParamBind     // HTTP's neutralized param placement, one per HTTPParamBinding
    ResponseBodyPath *ir.PropPath // HTTPBinding.ResponseBodyPath: the response body is this
                                  // sub-field of the payload; nil = the whole payload
    NoBody   bool            // no response under this binding carries a body: an HTTP binding
                             // whose method is HEAD (below)
    // The binding itself, by reference, not restructured; exactly the one Protocol names is set:
    HTTP     *ir.HTTPBinding    // method/URITemplate/status map
    RPC      *ir.RPCBinding     // System says which RPC system
    Message  *ir.MessageBinding // channel, direction, messages, reply and its raw Bindings
    GraphQL  *ir.GraphQLBinding // the entry point: query, mutation or subscription, and its field
    OTP      *ir.OTPBinding     // the behaviour, call/cast/info, the process and the request tag
}
type ParamBind struct {
    Param    ir.ParamID      // the bound Operation.Params entry; one name may be two params
    ParamPath []ir.PropID    // HTTPParamBinding.ParamPath: the member of the param this binding
                             // sends, as a transcoding's {book.name} does; empty = the whole param
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
type Protocol string         // "http" | "rpc" | "message" | "graphql" | "otp"
```

The traces in these documents exercise `Protocol == "http"`; the RPC/message/OTP arms exist in the
type so the second-protocol emitter ([open question 4](../open-questions.md)) does not force a plan
schema change (INV9). Channels and messages ([messaging](../../ir/messaging.md)) reach an emitter
through `Message`; a messaging target consumes them the same way an HTTP target consumes `HTTP`. A
view copying the message binding's fields, as `MsgView` once did, was dropped: it restructured the
one binding and left out its `Unmodeled`, which a refiner reads (below).

A `Protocol` names the `OpBindings` field a binding comes from, never the system speaking it: `rpc`
is every `RPCBinding`, gRPC, Connect, JSON-RPC and Smithy RPC alike, and its `RPC.System` says which
([RPC binding](../../ir/rpc-binding.md)). A label for each system was rejected: `System` is an open
string, so the vocabulary would be open too and no switch over `Protocol` could be complete, while
labelling every system `grpc`, as the vocabulary once did, handed a gRPC target a JSON-RPC method to
lower as gRPC.

**The order.** The IR orders an operation's HTTP mappings, the primary first, and nothing across
`OpBindings`' fields, which hold one RPC, message, GraphQL and OTP binding each. So `All` is a rule
over the fields: `RPC`, then `HTTP` in its order, then `Message`, `GraphQL` and `OTP`, whatever
order the source wrote them in. "The binding the source states first" was rejected as underivable: a
Protobuf method states its RPC binding and its transcoding in one declaration, and no field records
which of a Smithy service's protocol traits came first.

**The binding a target lowers** is the first of `All` its
[capabilities](../contract/registry.md#capabilities) cover, an RPC binding only where `RPCSystems`
lists its `System`. Every target's reader of an operation's binding reads that one, the request's
path parameters ([request shaping](../refine/requests.md)), a stream's frames
([streams](streams.md)), whether a response carries a body (`NoBody`) and the mock's routes ([docs
and mocks](../docs-and-mock.md#what-the-mock-streams)) among them. So a target covering `http` alone
lowers `HTTP[0]` wherever there is one and reaches a gRPC method through its transcoding, which is
the binding the mock routes; one listing `rpc` with `grpc` reaches it as gRPC, whether or not it
covers `http` too, and lowers an HTTP binding only of an operation with no RPC binding of a system
it lists. An operation none of whose bindings a target covers is a warning
`emitters/<target>/unsupported-binding` naming the operation and its bindings, the mock's included,
with nothing generated for it.

`RPC` leads because a client has one transport ([output units](../refine/output-units.md)). Every
format with a planned lowering that states an RPC binding states it for a whole service, each `rpc`
of a Protobuf service and each operation of a Smithy service under a protocol trait ([planned
lowerings](../../compilers/planned-lowerings.md)), while an HTTP mapping is each operation's own and
may be missing. `HTTP` first was rejected on a Protobuf service two of whose four methods are
transcoded, one the client streams to having no transcoding to state: a target covering both
protocols sent two methods over HTTP and two over gRPC, one client on two transports, as it did a
Smithy service under an RPC protocol only some of whose operations carry `@http`. `RPC` first parts
only a service stating an RPC binding for some of its operations, which no planned lowering
produces, and changes nothing for a target covering one protocol. Either order moves a target
covering both under some additive change, a service gaining an RPC protocol under this one, which
moves its operations together, and a gRPC method gaining a transcoding under that, which moved the
one method off its service's transport. Reading the order from the target's `Protocols` was rejected
too: that list is a set the registry checks, and its order would decide what a target generates
while no check reads it.

A plan fact, which every target shares, cannot read one target's binding. It reads `All`, as
`Effects` takes only what every binding states ([plan
policy](policy.md#an-operations-effects-come-from-its-binding)), or the primary mapping, `HTTP[0]`,
where the fact is what the operation does and not how it travels, as the lifecycles a request's
shape takes are ([shapes](types.md#a-shape-reaches-what-it-holds)). What one wire alone fixes stays
on its binding. `NoBody` is set on an HTTP binding whose method is `HEAD`, whose responses have no
body (RFC 9110 §9.3.2), and a target lowering such a binding reads every response of the operation
as `void` ([body shapes](responses.md#body-shapes)), `Return`, `Others` and `Errors` alike and
whatever `Shape` each holds, and lowers no `Page`, `Stream` or `LRO` of it, each of which reads a
body. Holding that as the `Shape` of `Return`, read off `HTTP[0]`, was rejected: a method declared
`rpc CheckBook(…) returns (Book)` with a `HEAD` transcoding returned nothing on a target lowering it
through gRPC, which receives the `Book`. T-68 holds these.

No `ProtoBinding` holds a binding as `ir.RawConfig`: the GraphQL entry point and the OTP call shape
are typed in the IR, so the plan hands them over by reference as it does HTTP's and RPC's. A
`RawConfig` reaches a target only where the IR declares one, a message binding's per-protocol
`Bindings` (kafka's `groupId`, say), whose shape the IR deliberately leaves open. The `Unmodeled`
map each binding struct carries (the [binding documents](../../ir/bindings.md)) is the other thing
entirely: the escape hatch for constructs the IR does *not* model — a directive applied to a GraphQL
entry-point field, say — where `Reason` and `Provenance` are exactly what a policy layer selects on.
A refiner reads both, for different purposes.

Sub-clients and fluent navigation are built from the `OperationGroup` tree itself — its nesting,
names, operations and Smithy `Resource` — which the IR states, so the plan mirrors none of it ([plan
or refine](plan-or-refine.md)).
