// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// EntityRef names one entity of the document by its ID, whatever its class:
// exactly one field is set, the one of the entity's class, so a reference to
// an operation and one to a type are told apart by where the ID sits rather
// than by reading the ID (docs/ir/annotations.md#replacements). An empty field
// is unset, since no entity's ID is empty.
type EntityRef struct {
	// Type is a type's ID.
	Type TypeID `json:"type,omitempty"`
	// Operation is an operation's ID.
	Operation OpID `json:"operation,omitempty"`
	// Channel is a channel's ID.
	Channel ChannelID `json:"channel,omitempty"`
	// Message is a message's ID.
	Message MessageID `json:"message,omitempty"`
	// Auth is an auth scheme's ID.
	Auth AuthID `json:"auth,omitempty"`
	// Service is a service's ID.
	Service ServiceID `json:"service,omitempty"`
	// Group is an operation group's ID.
	Group GroupID `json:"group,omitempty"`
	// Property is a property's ID.
	Property PropID `json:"property,omitempty"`
	// Param is a parameter's ID.
	Param ParamID `json:"param,omitempty"`
	// Member is an enum member's ID.
	Member EnumMemberID `json:"member,omitempty"`
	// Server is a server's ID.
	Server ServerID `json:"server,omitempty"`
}
