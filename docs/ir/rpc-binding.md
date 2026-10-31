# RPC binding

Part of the [IR design](./README.md). An operation's mapping onto an RPC system.

```go
type RPCBinding struct {
    System      string     // "grpc" | "smithy-rpc" | "connect" | "jsonrpc" | …
    FullMethod  string     // "/pkg.Service/Method"
    InputType   *TypeRef   // the request message type params fold into (nil = synthesize from Params)
    ParamStructure string  // "" | "by_name" | "by_position" | "either" — how params serialize
                           // (JSON-RPC positional vs named; OpenRPC paramStructure). Param order
                           // is already source order per design rule 5
                           // (docs/ir/README.md#design-rules); this is the mode
    IdempotencyLevel string
    Unmodeled   Unmodeled
}
```
