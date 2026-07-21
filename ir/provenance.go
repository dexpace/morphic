package ir

import "strings"

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

// Diagnostic is a typed report from a compiler or pass. Codes are stable
// strings ("openapi/unresolved-ref", "ir/dangling-type-ref") so CI can
// allowlist them.
type Diagnostic struct {
	Severity   Severity   `json:"severity"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Provenance Provenance `json:"provenance"`
}

// NewDiagnostic builds a Diagnostic, coercing message to well-formed UTF-8 so
// the enclosing Document round-trips through JSON byte-for-byte (invariant #7).
// A third-party validator can render a truncated multibyte rune into its error
// text; stored verbatim, that ill-formed byte run would reach Document
// diagnostics, and json.Marshal rewrites invalid UTF-8 to U+FFFD — so the
// document would no longer re-encode to identical bytes. Coercing at
// construction keeps every message idempotent under marshal/unmarshal.
//
// Compilers and passes build diagnostics through this constructor; irverify's
// ir/diagnostic-invalid-utf8 check flags any message that still reaches a
// Document ill-formed. strings.ToValidUTF8 returns message unchanged, without
// allocating, when it is already valid, so the common path costs one scan.
func NewDiagnostic(sev Severity, code, message string, prov Provenance) Diagnostic {
	return Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    strings.ToValidUTF8(message, "\uFFFD"),
		Provenance: prov,
	}
}

// SourceInfo describes one input file of a Document.
type SourceInfo struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Hash   string `json:"hash"`
}
