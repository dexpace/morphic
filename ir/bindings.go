package ir

// OpBindings holds the concrete-protocol mappings of an Operation's neutral core
// (ir-design §8). An operation has at least one binding; more than one is legal
// (a Smithy service exposed over both HTTP and RPC; gRPC with HTTP transcoding).
type OpBindings struct {
	// HTTP holds the HTTP mappings (OpenAPI/Swagger ops, TypeSpec @route, Smithy
	// http traits). A slice: one operation may carry several HTTP mappings — gRPC
	// transcoding additional_bindings — with the primary first.
	HTTP []HTTPBinding `json:"http,omitempty"`
	// RPC is the Protobuf/gRPC, Smithy RPC, or JSON-RPC binding.
	RPC *RPCBinding `json:"rpc,omitempty"`
	// Message is the AsyncAPI operation / webhook binding.
	Message *MessageBinding `json:"message,omitempty"`
	// GraphQL is the query/mutation/subscription field binding. GraphQL
	// subscriptions bind here plus streaming fields on the core — not via
	// MessageBinding.
	GraphQL *GraphQLBinding `json:"graphql,omitempty"`
	// OTP is the Erlang/OTP behaviour-operation binding (§8.5).
	OTP *OTPBinding `json:"otp,omitempty"`
}

// HTTPLocation is where an HTTP parameter binds on the wire (ir-design §8.1).
type HTTPLocation string

// HTTP parameter locations.
const (
	// HTTPLocationPath binds to a path template segment.
	HTTPLocationPath HTTPLocation = "path"
	// HTTPLocationQuery binds to a single query parameter.
	HTTPLocationQuery HTTPLocation = "query"
	// HTTPLocationQuerystring binds the whole query string serialized from one
	// schema (OpenAPI 3.2 in: querystring, combined with ContentType).
	HTTPLocationQuerystring HTTPLocation = "querystring"
	// HTTPLocationHeader binds to a request header.
	HTTPLocationHeader HTTPLocation = "header"
	// HTTPLocationCookie binds to a cookie.
	HTTPLocationCookie HTTPLocation = "cookie"
	// HTTPLocationBody binds the whole request body.
	HTTPLocationBody HTTPLocation = "body"
	// HTTPLocationBodyProperty binds to a property within the body model.
	HTTPLocationBodyProperty HTTPLocation = "body_property"
	// HTTPLocationHost binds to a host-prefix label (Smithy @hostLabel).
	HTTPLocationHost HTTPLocation = "host"
)

// HTTPBinding maps an Operation onto one HTTP method+path (ir-design §8.1).
type HTTPBinding struct {
	// Method is the method as sent on the wire (OpenAPI 3.2 additionalOperations
	// keys carry exact capitalization; QUERY and custom methods are legal).
	Method string `json:"method,omitempty"`
	// URITemplate is the RFC 6570 path template — the one true path
	// representation.
	URITemplate string `json:"uriTemplate,omitempty"`
	// HostPrefix is the endpoint host prefix, may contain {param} labels
	// (Smithy @endpoint).
	HostPrefix string `json:"hostPrefix,omitempty"`
	// SharedRoute reports that multiple operations legally share method+path,
	// disambiguated by request content (TypeSpec @sharedRoute); validate must not
	// reject the duplicate, and single-route emitters must merge.
	SharedRoute bool `json:"sharedRoute"`
	// ParamBindings assigns each logical parameter its HTTP location.
	ParamBindings []HTTPParamBinding `json:"paramBindings,omitempty"`
	// RequestContentTypes are the priority-ordered request media types.
	RequestContentTypes []string `json:"requestContentTypes,omitempty"`
	// ResponseBodyPath sets the HTTP response body to this sub-field of the
	// response type (gRPC transcoding response_body); nil = the whole payload.
	ResponseBodyPath *PropPath `json:"responseBodyPath,omitempty"`
	// SuccessStatus maps response index to primary status (denormalized
	// convenience; conditions are the truth).
	SuccessStatus map[int]int `json:"successStatus,omitempty"`
	// Compression requires the client to compress the request body
	// (Smithy @requestCompression).
	Compression *RequestCompression `json:"compression,omitempty"`
	// ChecksumRequired requires the client to send a payload checksum
	// (Smithy @httpChecksumRequired).
	ChecksumRequired bool `json:"checksumRequired"`
	// PatchImplicitOptionality controls PATCH implicit optionality: nil = protocol
	// default (PATCH projections make properties optional); false = disabled
	// (TypeSpec @patch implicitOptionality).
	PatchImplicitOptionality *bool `json:"patchImplicitOptionality,omitempty"`
	// IsWebhook marks an inbound webhook operation (OpenAPI 3.1 webhooks).
	IsWebhook bool `json:"isWebhook"`
	// Callbacks are out-of-band operations keyed by runtime expressions.
	Callbacks []Callback `json:"callbacks,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// RequestCompression declares required request-body compression encodings
// (ir-design §8.1).
type RequestCompression struct {
	// Encodings are the priority-ordered compression encodings ("gzip", …).
	Encodings []string `json:"encodings,omitempty"`
}

// HTTPParamBinding assigns one logical parameter its HTTP wire location
// (ir-design §8.1).
type HTTPParamBinding struct {
	// Param is the Operation.Params name it binds.
	Param string `json:"param,omitempty"`
	// ParamPath is the nested source field within the logical param, when the
	// binding targets a sub-field of a message-typed param (gRPC transcoding
	// {book.name}, dotted query params); empty = the whole param.
	ParamPath []PropID `json:"paramPath,omitempty"`
	// Location is where the parameter binds on the wire. host = the param fills a
	// HostPrefix label (Smithy @hostLabel); querystring = the whole query string
	// serialized from one schema (OpenAPI 3.2 in: querystring, with ContentType).
	Location HTTPLocation `json:"location,omitempty"`
	// WireName is the serialized parameter name.
	WireName string `json:"wireName,omitempty"`
	// Style is the serialization style: simple | form | label | matrix |
	// deepObject | pipe/space-delimited.
	Style string `json:"style,omitempty"`
	// Explode overrides the default explode behavior; nil = default.
	Explode *bool `json:"explode,omitempty"`
	// AllowReserved permits reserved characters unescaped in the value.
	AllowReserved bool `json:"allowReserved"`
	// PathPattern is a multi-segment path pattern constraint for this param
	// (gRPC transcoding {name=shelves/*/books/*}); "" = single segment. The URI
	// template uses reserved expansion; emitters that cannot validate the pattern
	// drop it with a diagnostic.
	PathPattern string `json:"pathPattern,omitempty"`
	// Prefix spreads a map-typed param as prefixed wire entries: prefixed headers
	// (Smithy @httpPrefixHeaders) or catch-all query maps (@httpQueryParams with
	// Prefix "").
	Prefix string `json:"prefix,omitempty"`
	// ContentType serializes the param as a media type (OpenAPI content-style
	// params).
	ContentType string `json:"contentType,omitempty"`
	// BodyPath is, for body_property, where in the body model it lands
	// (TypeSpec HttpProperty).
	BodyPath []PropID `json:"bodyPath,omitempty"`
}

// Callback is an out-of-band operation set keyed by a runtime expression
// (ir-design §8.1).
type Callback struct {
	// Expression is the runtime expression that resolves the callback URL.
	Expression string `json:"expression,omitempty"`
	// Operations are the callback operations keyed by that expression.
	Operations []OpID `json:"operations,omitempty"`
}

// RPCBinding maps an Operation onto an RPC method (ir-design §8.2).
type RPCBinding struct {
	// System is "grpc" | "smithy-rpc" | "connect" | "jsonrpc" | ….
	System string `json:"system,omitempty"`
	// FullMethod is the fully-qualified method, e.g. "/pkg.Service/Method".
	FullMethod string `json:"fullMethod,omitempty"`
	// InputType is the request message type params fold into (nil = synthesize
	// from Params).
	InputType *TypeRef `json:"inputType,omitempty"`
	// ParamStructure is "" | "by_name" | "by_position" | "either" — how params
	// serialize (JSON-RPC positional vs named; OpenRPC paramStructure). Param
	// order is already source order; this is the mode.
	ParamStructure string `json:"paramStructure,omitempty"`
	// IdempotencyLevel is the RPC-declared idempotency level.
	IdempotencyLevel string `json:"idempotencyLevel,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// MsgDirection is the application-perspective direction of a MessageBinding
// (ir-design §8.3, AsyncAPI 3 semantics).
type MsgDirection string

// Message directions.
const (
	// MsgDirectionSend means the application sends the message.
	MsgDirectionSend MsgDirection = "send"
	// MsgDirectionReceive means the application receives the message.
	MsgDirectionReceive MsgDirection = "receive"
)

// MessageBinding maps an Operation onto a messaging channel (ir-design §8.3).
type MessageBinding struct {
	// Channel is the channel the operation acts on.
	Channel ChannelID `json:"channel,omitempty"`
	// Direction is send | receive (application perspective).
	Direction MsgDirection `json:"direction,omitempty"`
	// Messages are which of the channel's messages this operation uses (must be a
	// subset — validated).
	Messages []MessageID `json:"messages,omitempty"`
	// Reply carries request-reply semantics; nil = none. A send-op with no Reply
	// and no Responses is one-way (set Operation.OneWay).
	Reply *Reply `json:"reply,omitempty"`
	// Bindings holds operation-level protocol bindings kept raw (kafka
	// groupId/clientId — constrain SDK client config).
	Bindings map[string]Extensions `json:"bindings,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// Reply describes request-reply semantics of a MessageBinding (ir-design §8.3).
type Reply struct {
	// Channel is the static reply channel; nil when the address is dynamic-only
	// (an AsyncAPI reply channel's own address is null by spec).
	Channel *ChannelID `json:"channel,omitempty"`
	// Address is the dynamic reply address: where in the request message the
	// reply destination lives, e.g. In:"header", Segments:[replyTo] (AsyncAPI
	// Operation Reply Address runtime expressions).
	Address *PropPath `json:"address,omitempty"`
	// Messages is the reply payload message set.
	Messages []MessageID `json:"messages,omitempty"`
	// Docs is the reply's documentation.
	Docs Docs `json:"docs"`
}

// GraphQLBinding maps an Operation onto a GraphQL entry point (ir-design §8.4).
type GraphQLBinding struct {
	// Kind is "query" | "mutation" | "subscription".
	Kind string `json:"kind,omitempty"`
	// FieldPath is the entry-point field (nesting for namespaced schemas).
	FieldPath []string `json:"fieldPath,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// OTPBinding maps an Operation onto an Erlang/OTP behaviour callback
// (ir-design §8.5).
type OTPBinding struct {
	// Behaviour is "gen_server" | "gen_statem" | "gen_event".
	Behaviour string `json:"behaviour,omitempty"`
	// Kind is "call" (synchronous request→reply) | "cast" (fire-and-forget; the
	// operation also sets OneWay) | "info" (raw message send).
	Kind string `json:"kind,omitempty"`
	// Process is the channel modeling the target process: Address = registered
	// name (nil Address = unregistered/runtime pid); registration kind
	// (local/global/via) in Channel.Bindings["otp"].
	Process ChannelID `json:"process,omitempty"`
	// RequestTag is the tag of the request tuple (a symbol Value, e.g. 'get');
	// nil = the whole term is the request.
	RequestTag *Value `json:"requestTag,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}
