package ir

// Server is a named endpoint template (ir-design §10). Servers are named
// entities (AsyncAPI name-keyed servers, OpenAPI 3.2 Server.name).
type Server struct {
	// Name is the server's naming.
	Name Naming `json:"name"`
	// URLTemplate is the endpoint URL, may contain {variables}.
	URLTemplate string `json:"urlTemplate,omitempty"`
	// Description is the server's documentation.
	Description Docs `json:"description"`
	// Variables are the URL template variables.
	Variables []ServerVariable `json:"variables,omitempty"`
	// Protocol is "https" (default); "kafka", "wss", … for messaging servers.
	Protocol string `json:"protocol,omitempty"`
	// ProtocolVersion is the protocol version, e.g. Kafka "3.5", AMQP "0-9-1"
	// (AsyncAPI).
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	// Tags are the server's tag memberships.
	Tags []string `json:"tags,omitempty"`
	// Auth is server-scoped security — AsyncAPI's primary auth placement (broker
	// connections authenticate per server; different servers of one service may
	// require different schemes). An empty non-nil slice (explicitly public)
	// differs from nil, so the field carries no omitempty.
	Auth []AuthRequirement `json:"auth"`
	// Bindings holds server-level protocol bindings kept raw.
	Bindings map[string]Extensions `json:"bindings,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// ServerVariable is one variable of a Server's URL template (ir-design §10).
type ServerVariable struct {
	// Name is the variable name referenced in the URL template.
	Name string `json:"name,omitempty"`
	// Default is the default value used when the caller supplies none.
	Default string `json:"default,omitempty"`
	// Enum is the closed set of permitted values, when constrained.
	Enum []string `json:"enum,omitempty"`
	// Docs is the variable's documentation.
	Docs Docs `json:"docs"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}
