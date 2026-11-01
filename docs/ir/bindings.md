# Protocol bindings

Part of the [IR design](./README.md). How the neutral operation core maps onto concrete protocols.
Each protocol has a document of its own: [HTTP](./http-binding.md), [RPC](./rpc-binding.md),
[messaging](./messaging.md), [GraphQL](./graphql-binding.md) and [Erlang/OTP](./otp-binding.md).

```go
type OpBindings struct {
    HTTP    []HTTPBinding     // OpenAPI/Swagger ops, TypeSpec @route, Smithy http traits.
                              // A slice: one operation may carry several HTTP mappings —
                              // gRPC transcoding's additional_bindings — with the primary first
    RPC     *RPCBinding       // Protobuf/gRPC, Smithy RPC protocols, JSON-RPC
    Message *MessageBinding   // AsyncAPI operations, webhooks
    GraphQL *GraphQLBinding   // query/mutation/subscription field binding. GraphQL subscriptions
                              // bind here + streaming fields on the core — NOT via MessageBinding
                              // (GraphQL defines no channel; synthesizing one is deployment-aware
                              // policy, marked Inferred, never a compiler default)
    OTP     *OTPBinding       // Erlang/OTP behaviour operations (docs/ir/otp-binding.md)
}
```

An operation has at least one binding; more than one is legal (Smithy service exposed over both
HTTP and RPC; gRPC with HTTP transcoding).
