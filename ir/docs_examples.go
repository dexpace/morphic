//go:build ignore

// This file holds the Example and ErrorExample shapes, which reference Value
// and TypeRef (declared in Tasks 2–3). It carries the //go:build ignore tag so
// the package compiles at Task 1; the structs move into docs.go once value.go
// and typeref.go land, with their shapes unchanged (ir-design §12).

package ir

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
