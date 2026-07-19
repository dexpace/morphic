package ir

// IDs are opaque to consumers but constructed deterministically by frontends
// from the source pointer of the defining occurrence (ir-design §3.1). They are
// never derived from display names and never rewritten by renames.

// TypeID identifies a TypeDef in Document.Types,
// e.g. "t/openapi/components/schemas/User".
type TypeID string

// OpID identifies an Operation, same construction as TypeID.
type OpID string

// ServiceID identifies a Service.
type ServiceID string

// ChannelID identifies a Channel in Document.Channels.
type ChannelID string

// MessageID identifies a Message in Document.Messages.
type MessageID string

// AuthID identifies an AuthScheme in Document.Auth.
type AuthID string

// PropID identifies a Property within the document.
type PropID string
