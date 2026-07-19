package ir

// Operation is the protocol-neutral core of a callable API operation
// (ir-design §7.2). Parameters carry no protocol location; each binding in
// Bindings maps the core onto a concrete protocol.
type Operation struct {
	// ID is the operation's stable synthetic identity.
	ID OpID `json:"id,omitempty"`
	// Name is the operation's naming.
	Name Naming `json:"name"`
	// Docs is the operation's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the operation as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the operation's versioning timeline.
	Availability *Availability `json:"availability,omitempty"`
	// Params are all logical inputs, protocol-unbound.
	Params []Parameter `json:"params,omitempty"`
	// Request is the body/message content; nil = none.
	Request *Payload `json:"request,omitempty"`
	// Responses are the ordered success and alternative-success responses.
	Responses []Response `json:"responses,omitempty"`
	// Errors are the declared failure shapes.
	Errors []ErrorCase `json:"errors,omitempty"`
	// OneWay marks fire-and-forget operations for which no response ever exists
	// (OTP cast, AsyncAPI send-without-reply, Thrift oneway, JSON-RPC
	// notifications). Distinct from a response with no body (an ack still
	// exists); validate rejects OneWay with a non-empty Responses.
	OneWay bool `json:"oneWay"`
	// Streaming is the derived streaming summary: none | client | server | bidi.
	Streaming StreamingMode `json:"streaming,omitempty"`
	// RequestStream carries client-to-server streaming semantics, when present.
	RequestStream *StreamDetail `json:"requestStream,omitempty"`
	// ResponseStream carries server-to-client streaming semantics, when present.
	ResponseStream *StreamDetail `json:"responseStream,omitempty"`
	// Pagination describes the operation's pagination, when present.
	Pagination *Pagination `json:"pagination,omitempty"`
	// LongRunning describes long-running-operation semantics, when present.
	LongRunning *LongRunning `json:"longRunning,omitempty"`
	// Idempotency is the operation's idempotency classification: unknown | safe |
	// idempotent | idempotency_token(param). safe = no side effects
	// (Smithy @readonly, HTTP GET semantics).
	Idempotency Idempotency `json:"idempotency"`
	// Auth overrides the service default; an empty slice differs from nil (empty
	// = explicitly public).
	Auth []AuthRequirement `json:"auth,omitempty"`
	// Tags are the operation's tag memberships.
	Tags []string `json:"tags,omitempty"`
	// ParameterVisibility overrides the visibility filter for the request view
	// (TypeSpec @parameterVisibility); nil = protocol default.
	ParameterVisibility []Lifecycle `json:"parameterVisibility,omitempty"`
	// ReturnTypeVisibility overrides the visibility filter for the response view;
	// nil = protocol default.
	ReturnTypeVisibility []Lifecycle `json:"returnTypeVisibility,omitempty"`
	// OverloadOf points at the operation this one overloads (TypeSpec @overload).
	OverloadOf *OpID `json:"overloadOf,omitempty"`
	// Bindings describes how the neutral core maps onto concrete protocols (§8).
	Bindings OpBindings `json:"bindings"`
	// Examples are operation-scenario examples.
	Examples []Example `json:"examples,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the operation came from.
	Provenance Provenance `json:"provenance"`
}

// Parameter is one logical input of an Operation, protocol-unbound; its wire
// location is HTTP-binding detail (ir-design §7.2). GraphQL field arguments
// (Property.Args) reuse this shape.
type Parameter struct {
	// Name is the parameter's naming.
	Name Naming `json:"name"`
	// Type is the parameter's type.
	Type TypeRef `json:"type"`
	// Required reports whether the caller must supply the parameter.
	Required bool `json:"required"`
	// Default is the parameter's default value.
	Default *Value `json:"default,omitempty"`
	// Constraints restricts the parameter's admissible values.
	Constraints *Constraints `json:"constraints,omitempty"`
	// ValueFrom derives the parameter's value from a location in the
	// outgoing/incoming message (AsyncAPI parameter location runtime
	// expressions); SDKs may auto-fill it. nil = caller-supplied.
	ValueFrom *PropPath `json:"valueFrom,omitempty"`
	// Docs is the parameter's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the parameter as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the parameter's versioning timeline.
	Availability *Availability `json:"availability,omitempty"`
	// Examples are parameter-level example values.
	Examples []Example `json:"examples,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// Payload is the body/message content of a request, response, or message
// (ir-design §7.2). All media types are kept.
type Payload struct {
	// Contents holds one entry per media type / message schema — all kept.
	Contents []Content `json:"contents,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// Content is one media-type view of a Payload (ir-design §7.2).
type Content struct {
	// MediaType is "application/json", "multipart/form-data", or "" for non-HTTP.
	MediaType string `json:"mediaType,omitempty"`
	// SchemaFormat is the schema language the type graph was lowered from, in
	// media-type form (AsyncAPI multiFormatSchema: Avro/Protobuf/RAML/…);
	// "" = source-native. The verbatim source schema is preserved in Extensions.
	SchemaFormat string `json:"schemaFormat,omitempty"`
	// Type is the content's type.
	Type TypeRef `json:"type"`
	// Item is the element shape of a sequential stream declared per media type
	// (OpenAPI 3.2 itemSchema for SSE/JSONL/json-seq); nil = not sequential.
	Item *TypeRef `json:"item,omitempty"`
	// ItemEncoding holds per-item encoding for sequential media types
	// (3.2 itemEncoding).
	ItemEncoding map[string]PartEncoding `json:"itemEncoding,omitempty"`
	// Encoding holds multipart/form per-property (part) wire config, keyed by
	// PropID.
	Encoding map[string]PartEncoding `json:"encoding,omitempty"`
	// File marks the body as a file upload/download (TypeSpec file bodies, binary
	// payloads).
	File *FileInfo `json:"file,omitempty"`
	// Examples are content-level example values.
	Examples []Example `json:"examples,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// PartEncoding is the wire configuration of one multipart part or sequential
// item (ir-design §7.2). TypeSpec tuple-form multipart lowers to a synthesized
// model whose properties are the parts.
type PartEncoding struct {
	// ContentTypes are the media type(s) of this part.
	ContentTypes []string `json:"contentTypes,omitempty"`
	// Headers are the per-part headers.
	Headers []Property `json:"headers,omitempty"`
	// Multi reports that the part repeats (array member → repeated parts).
	Multi bool `json:"multi"`
	// Filename reports that the part carries a filename (file part).
	Filename bool `json:"filename"`
	// Style is the form-style serialization for non-file parts.
	Style string `json:"style,omitempty"`
	// Explode overrides the default explode behavior; nil = default.
	Explode *bool `json:"explode,omitempty"`
}

// FileInfo describes a file-upload/download body (ir-design §7.2).
type FileInfo struct {
	// IsText reports textual vs binary contents.
	IsText bool `json:"isText"`
	// Contents is the declared contents scalar chain (string/bytes extensions);
	// nil = bytes.
	Contents *TypeRef `json:"contents,omitempty"`
	// ContentTypes is the declared allowed content-type set (TypeSpec
	// File<"image/png" | "image/jpeg">); runtime Content-Type comes from the file
	// value.
	ContentTypes []string `json:"contentTypes,omitempty"`
	// ContentTypeDefault is the default used when the file value carries none.
	ContentTypeDefault string `json:"contentTypeDefault,omitempty"`
	// FilenameLocation is "content-disposition" (default) | "path" | "header".
	FilenameLocation string `json:"filenameLocation,omitempty"`
	// FilenameWireName is the wire name when FilenameLocation is path/header.
	FilenameWireName string `json:"filenameWireName,omitempty"`
}

// Response is one declared output of an Operation (ir-design §7.2). All
// responses and all content types survive; the plan layer picks a primary.
type Response struct {
	// Name is the response naming for formats with named outputs; Hint elsewhere.
	Name Naming `json:"name"`
	// Conditions are the HTTP status codes/ranges; empty for RPC single-response.
	Conditions ResponseConditions `json:"conditions"`
	// Payload is the response body; nil = no body.
	Payload *Payload `json:"payload,omitempty"`
	// Headers are the response metadata fields.
	Headers []Property `json:"headers,omitempty"`
	// StatusCodeProp is the output member populated from the runtime HTTP status
	// line (Smithy @httpResponseCode, TypeSpec non-literal @statusCode); the
	// member is suppressed from the body.
	StatusCodeProp *PropPath `json:"statusCodeProp,omitempty"`
	// Docs is the response's documentation.
	Docs Docs `json:"docs"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// ResponseConditions selects the status codes a Response or ErrorCase applies to
// (ir-design §7.2).
type ResponseConditions struct {
	// StatusCodes are the applicable status ranges; empty = unconditional.
	StatusCodes []StatusRange `json:"statusCodes,omitempty"`
}

// StatusRange is an inclusive HTTP status-code range: 200–200, 400–499 ("4XX"),
// 0–0 = default/catch-all (ir-design §7.2).
type StatusRange struct {
	// From is the inclusive lower bound.
	From int `json:"from"`
	// To is the inclusive upper bound.
	To int `json:"to"`
}

// ErrorCase is a declared failure shape of an Operation (ir-design §7.2).
type ErrorCase struct {
	// Type is an error-flagged model.
	Type TypeRef `json:"type"`
	// Conditions are the status codes/ranges this error maps to.
	Conditions ResponseConditions `json:"conditions"`
	// Fault is "" | "client" | "server" — protocol-neutral fault classification
	// (Smithy @error; OpenAPI 4XX/5XX is its HTTP lowering). Drives exception
	// hierarchies and default status synthesis.
	Fault string `json:"fault,omitempty"`
	// Retryable reports whether the error is retryable (Smithy @retryable);
	// nil = unknown.
	Retryable *bool `json:"retryable,omitempty"`
	// Throttling reports that the error is retryable specifically due to
	// throttling — a distinct backoff class (Smithy @retryable(throttling: true));
	// nil = unknown.
	Throttling *bool `json:"throttling,omitempty"`
	// Docs is the error case's documentation.
	Docs Docs `json:"docs"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// StreamingMode is the protocol-independent streaming direction of an Operation
// (ir-design §7.3).
type StreamingMode string

// Streaming modes.
const (
	// StreamingNone means the operation does not stream.
	StreamingNone StreamingMode = "none"
	// StreamingClient means the client streams to the server.
	StreamingClient StreamingMode = "client"
	// StreamingServer means the server streams to the client.
	StreamingServer StreamingMode = "server"
	// StreamingBidi means both directions stream.
	StreamingBidi StreamingMode = "bidi"
)

// StreamDetail describes one direction of a streaming Operation (ir-design §7.3).
type StreamDetail struct {
	// Events is the stream element type when it differs from the payload content
	// type — for event streams a WireTagged Union of event models; within an
	// event model, Property.EventHeader marks frame-header members and
	// Property.EventPayload marks a raw-payload member (Smithy @eventHeader/
	// @eventPayload). Per-event content types and terminal events live on
	// Variant.Event.
	Events *TypeRef `json:"events,omitempty"`
	// Initial is the initial-request/initial-response message preceding the
	// stream (Smithy event stream initial messages); nil = none.
	Initial *TypeRef `json:"initial,omitempty"`
	// RequiresLength reports that the streamed content must have a known finite
	// length up front (Smithy @requiresLength) — changes the generated parameter
	// type.
	RequiresLength bool `json:"requiresLength"`
}

// IdempotencyKind names the idempotency class of an Operation (ir-design §7.2).
type IdempotencyKind string

// Idempotency kinds.
const (
	// IdempotencyUnknown means idempotency is undeclared.
	IdempotencyUnknown IdempotencyKind = ""
	// IdempotencySafe means the operation has no side effects (Smithy @readonly,
	// HTTP GET semantics).
	IdempotencySafe IdempotencyKind = "safe"
	// IdempotencyIdempotent means repeating the call has the same effect as one
	// call.
	IdempotencyIdempotent IdempotencyKind = "idempotent"
	// IdempotencyToken means idempotency is achieved via a client-supplied token
	// parameter named by Idempotency.TokenParam.
	IdempotencyToken IdempotencyKind = "idempotency_token"
)

// Idempotency is the resolved idempotency classification of an Operation. It
// reifies the spec's shorthand states unknown | safe | idempotent |
// idempotency_token(param); TokenParam is set only for the token kind
// (ir-design §7.2).
type Idempotency struct {
	// Kind is the idempotency class.
	Kind IdempotencyKind `json:"kind,omitempty"`
	// TokenParam names the idempotency-token parameter; set only when Kind is
	// IdempotencyToken.
	TokenParam string `json:"tokenParam,omitempty"`
}

// PageStrategy names the pagination mechanism of an Operation (ir-design §7.3).
type PageStrategy string

// Pagination strategies.
const (
	// PageStrategyCursor is opaque-cursor pagination.
	PageStrategyCursor PageStrategy = "cursor"
	// PageStrategyOffset is numeric-offset pagination.
	PageStrategyOffset PageStrategy = "offset"
	// PageStrategyPage is page-number pagination.
	PageStrategyPage PageStrategy = "page"
	// PageStrategyLinkHeader is RFC 5988 Link-header pagination.
	PageStrategyLinkHeader PageStrategy = "link_header"
	// PageStrategyNextLink is body next-link pagination.
	PageStrategyNextLink PageStrategy = "next_link"
	// PageStrategyToken is continuation-token pagination.
	PageStrategyToken PageStrategy = "token"
)

// Pagination describes an Operation's pagination (ir-design §7.3).
type Pagination struct {
	// Strategy is the pagination mechanism.
	Strategy PageStrategy `json:"strategy,omitempty"`
	// Inferred reports heuristic detection (OpenAPI name-matching policy) vs
	// declared (Smithy/TypeSpec).
	Inferred bool `json:"inferred"`
	// InputCursor is the input that continues iteration.
	InputCursor *ParamPath `json:"inputCursor,omitempty"`
	// InputLimit is the page-size input.
	InputLimit *ParamPath `json:"inputLimit,omitempty"`
	// Items is where result items live in the response — a path, not a name.
	Items *PropPath `json:"items,omitempty"`
	// NextCursor is the continuation source in the response.
	NextCursor *PropPath `json:"nextCursor,omitempty"`
	// NextLink is the next-page link in the response.
	NextLink *PropPath `json:"nextLink,omitempty"`
	// PrevLink is the previous-page navigation link.
	PrevLink *PropPath `json:"prevLink,omitempty"`
	// FirstLink is the first-page navigation link.
	FirstLink *PropPath `json:"firstLink,omitempty"`
	// LastLink is the last-page navigation link.
	LastLink *PropPath `json:"lastLink,omitempty"`
	// TotalCount is the total-count source in the response.
	TotalCount *PropPath `json:"totalCount,omitempty"`
}

// PropPath addresses a member within a type by identity, not by name
// (ir-design §7.3).
type PropPath struct {
	// Root is the type the path roots in; nil = determined by context (the
	// enclosing response body, message payload, …).
	Root *TypeRef `json:"root,omitempty"`
	// In is "" = body/payload | "header"; continuation tokens and reply addresses
	// can live in response/message headers, not just bodies.
	In string `json:"in,omitempty"`
	// Segments are the ordered property IDs walked from the root.
	Segments []PropID `json:"segments,omitempty"`
}

// ParamPath addresses a member within a named parameter (ir-design §7.3).
type ParamPath struct {
	// Param is the parameter name the path roots in.
	Param string `json:"param,omitempty"`
	// Segments are the ordered property IDs walked from the parameter.
	Segments []PropID `json:"segments,omitempty"`
}

// LongRunning describes long-running-operation semantics (ir-design §7.3).
type LongRunning struct {
	// FinalStateVia is "operation-location" | "status-monitor" | "original-uri" |
	// … — how the final state is located.
	FinalStateVia string `json:"finalStateVia,omitempty"`
	// PollingOperation is the declared poll op (Azure.Core @pollingOperation — a
	// library convention, not core TypeSpec).
	PollingOperation *OpID `json:"pollingOperation,omitempty"`
	// FinalOperation is the declared final-result op (Azure.Core @finalOperation).
	FinalOperation *OpID `json:"finalOperation,omitempty"`
	// PollingType is the status-monitor type.
	PollingType *TypeRef `json:"pollingType,omitempty"`
	// FinalType is the final-result type.
	FinalType *TypeRef `json:"finalType,omitempty"`
	// ResultPath locates the final result within the polling response.
	ResultPath *PropPath `json:"resultPath,omitempty"`
}
