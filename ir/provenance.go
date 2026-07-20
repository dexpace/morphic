package ir

// Provenance records where a node came from and whether it was declared or
// inferred (ir-design §13). Everything heuristic is auditable; everything
// broken is reportable with an exact source location.
type Provenance struct {
	// Source indexes into Document.Sources.
	Source int `json:"source"`
	// Pointer is a JSON pointer or line:col into that source.
	Pointer string `json:"pointer,omitempty"`
	// Inferred is "" for declared facts; otherwise it names the heuristic that
	// produced this node (e.g. "pagination-name-match").
	Inferred string `json:"inferred,omitempty"`
}

// Severity classifies a Diagnostic. The engine decides what is fatal.
type Severity string

// Diagnostic severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic is a typed report from a frontend or pass. Codes are stable
// strings ("openapi/unresolved-ref", "ir/dangling-type-ref") so CI can
// allowlist them.
type Diagnostic struct {
	Severity   Severity   `json:"severity"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Provenance Provenance `json:"provenance"`
}

// SourceInfo describes one input file of a Document.
type SourceInfo struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Hash   string `json:"hash"`
}
