package ir

// Channel is an event/messaging endpoint: an AsyncAPI channel, a webhook, a
// subscription, or an OTP process (ir-design §8.3). Its messages live in
// Document.Messages and are referenced here by identity.
type Channel struct {
	// ID is the channel's stable synthetic identity.
	ID ChannelID `json:"id,omitempty"`
	// Name is the channel's naming.
	Name Naming `json:"name"`
	// Address is the topic/routing key/path, may contain {params}; nil =
	// unknown/runtime-assigned address (reply channels and dynamic topics; SDKs
	// expose a runtime address arg).
	Address *string `json:"address,omitempty"`
	// Docs is the channel's documentation.
	Docs Docs `json:"docs"`
	// Tags are the channel's tag memberships.
	Tags []string `json:"tags,omitempty"`
	// Params are the channel's address parameters.
	Params []Parameter `json:"params,omitempty"`
	// Messages is the channel's message set; messages live in Document.Messages.
	Messages []MessageID `json:"messages,omitempty"`
	// Servers indexes into Document.Servers scoped to this channel.
	Servers []int `json:"servers,omitempty"`
	// Bindings holds protocol-specific config ("kafka", "amqp", "ws", "mqtt")
	// kept as namespaced raw config.
	Bindings map[string]Extensions `json:"bindings,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the channel came from.
	Provenance Provenance `json:"provenance"`
}

// Message is a reusable message shape referenced by channels, operations, and
// replies by identity (ir-design §8.3, AsyncAPI 3).
type Message struct {
	// ID is the message's stable synthetic identity.
	ID MessageID `json:"id,omitempty"`
	// Name is the message's naming.
	Name Naming `json:"name"`
	// Payload is the message payload.
	Payload Payload `json:"payload"`
	// Headers is the header schema — an object-constrained model hoisted into the
	// type registry like any anonymous type (headers can be named, composed, even
	// Avro-defined; and $message.header#/… paths need a type to resolve against).
	// Emitters compute flat lists per §4.3.
	Headers *TypeRef `json:"headers,omitempty"`
	// CorrelationID locates the correlation value: In: "header" | "" (payload).
	CorrelationID *PropPath `json:"correlationID,omitempty"`
	// ContentType is the message content type.
	ContentType string `json:"contentType,omitempty"`
	// Tags are the message's tag memberships.
	Tags []string `json:"tags,omitempty"`
	// Docs is the message's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the message as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Examples are correlated header+payload example pairs (Example.Headers +
	// .Value).
	Examples []Example `json:"examples,omitempty"`
	// Bindings holds message-level protocol bindings (kafka message key, …).
	Bindings map[string]Extensions `json:"bindings,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the message came from.
	Provenance Provenance `json:"provenance"`
}
