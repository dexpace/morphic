package ir

// Docs is the human-readable documentation attached to a named entity
// (ir-design §12).
type Docs struct {
	// Summary is a short single-line description.
	Summary string `json:"summary,omitempty"`
	// Description is CommonMark; it may contain {t:TypeID} cross-reference
	// tokens that emitters resolve to language-appropriate links.
	Description string `json:"description,omitempty"`
	// ExternalDocs links to supplementary documentation.
	ExternalDocs []Link `json:"externalDocs,omitempty"`
}

// Link is an external documentation reference.
type Link struct {
	// URL is the link target.
	URL string `json:"url,omitempty"`
	// Description labels the link.
	Description string `json:"description,omitempty"`
}

// Deprecation marks an entity as deprecated with optional migration guidance.
type Deprecation struct {
	// Message explains the deprecation and any migration path.
	Message string `json:"message,omitempty"`
	// Since is the version in which the entity was deprecated.
	Since string `json:"since,omitempty"`
	// RemovalVersion is the version in which the entity is scheduled for removal.
	RemovalVersion string `json:"removalVersion,omitempty"`
}

// Example is a documentation example. Field legality is contextual (validated):
// Value/Headers apply to types, properties, parameters, contents, and messages;
// Input/Output/Error apply to operations. An Example never mixes the two arms.
type Example struct {
	// Name identifies the example.
	Name string `json:"name,omitempty"`
	// Summary is a short description of the example.
	Summary string `json:"summary,omitempty"`
	// Description is a long-form description of the example.
	Description string `json:"description,omitempty"`
	// Value is a single-value example (schemas, properties, parameters); for
	// message examples it is the payload.
	Value *Value `json:"value,omitempty"`
	// Headers holds the correlated header values for message examples (AsyncAPI
	// message examples are header+payload pairs — never split them).
	Headers *Value `json:"headers,omitempty"`
	// Input is the operation-scenario input, paired with Output or Error.
	Input *Value `json:"input,omitempty"`
	// Output is the operation-scenario success result paired with Input.
	Output *Value `json:"output,omitempty"`
	// Error ends the scenario in this error instead of Output.
	Error *ErrorExample `json:"error,omitempty"`
	// ExternalURL points to an externally hosted example.
	ExternalURL string `json:"externalURL,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// ErrorExample is the error arm of an operation-scenario Example.
type ErrorExample struct {
	// Type is the error type produced by the scenario.
	Type TypeRef `json:"type"`
	// Content is the error value.
	Content Value `json:"content"`
}
