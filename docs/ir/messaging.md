# Messaging

Part of the [IR design](./README.md). Event and messaging operations, and the channels and messages
they name.

```go
type MessageBinding struct {
    Channel   ChannelID
    Direction MsgDirection     // send | receive (application perspective, AsyncAPI 3 semantics)
    Messages  []MessageID      // which of the channel's messages this operation uses
                               // (must be a subset — validated)
    Reply     *Reply           // request-reply semantics; nil = none. A send-op with no Reply and
                               // no Responses is one-way (set Operation.OneWay)
    Bindings  map[string]RawConfig // operation-level protocol bindings kept raw
                                    // (kafka groupId/clientId — constrain SDK client config)
    Unmodeled  Unmodeled
}

type Reply struct {
    Channel  *ChannelID    // static reply channel; nil when the address is dynamic-only.
                           // (An AsyncAPI reply channel's own address is null by spec)
    Address  *PropPath     // dynamic reply address: where in the *request* message the reply
                           // destination lives, e.g. In:"header", Segments:[replyTo]
                           // (AsyncAPI Operation Reply Address runtime expressions)
    Messages []MessageID   // reply payload message set (a subset of Channel's when set — validated)
    Docs     Docs
}

type Channel struct {
    ID        ChannelID
    Name      Naming
    Address   *string            // topic/routing key/path, may contain {params}.
                                 // nil = unknown/runtime-assigned address (load-bearing: reply
                                 // channels and dynamic topics; SDKs expose a runtime address arg)
    Docs      Docs
    Tags      []string
    Params    []Parameter
    Messages  []MessageID        // the channel's message set — messages live in Document.Messages
    Servers   []ServerID         // the servers the channel is available on, most preferred first;
                                 // empty = no declared server is (docs/ir/servers.md)
    Bindings  map[string]RawConfig // protocol-specific ("kafka", "amqp", "ws", "mqtt") kept as namespaced raw config
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Message struct {
    ID          MessageID
    Name        Naming
    Payload     Payload
    Headers     *TypeRef           // header *schema* — an object-constrained model hoisted into the
                                   // type registry like any anonymous type (headers can be named,
                                   // composed, even Avro-defined; and $message.header#/… paths need
                                   // a type to resolve against). Emitters compute flat lists per docs/ir/models.md
    CorrelationID *PropPath        // In: "header" | "" (payload)
    ContentType string
    Tags        []string
    Docs        Docs
    Deprecation *Deprecation
    Examples    []Example          // correlated header+payload example pairs (Example.Headers + .Value)
    Bindings    map[string]RawConfig // message-level protocol bindings (kafka message key, …)
    Unmodeled   Unmodeled
    Provenance  Provenance
}
```

Kafka/AMQP/MQTT binding minutiae stay as namespaced `RawConfig` rather than modeled
structs — they are protocol deployment detail, not API shape, and modeling them structurally
would chase every AsyncAPI binding spec revision. The raw-bindings channel exists at every
level that AsyncAPI defines bindings: server, channel, operation, and message.
