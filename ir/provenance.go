package ir

import "strings"

// NoSource is the Source value for a node that came from no input file at all.
// An IR pass reporting on the document it was handed has no source to name, and
// every other index — 0 included — names a file the document actually loaded,
// which would make a renderer fabricate a location for the finding.
//
// It is the only out-of-table Source value the IR declares: irverify accepts it
// and reports every other index that addresses no declared source, so a producer
// that invents a second sentinel is caught rather than tolerated.
const NoSource = -1

// Provenance records where a node came from and whether it was declared or
// inferred (ir-design §13). Everything heuristic is auditable; everything
// broken is reportable with an exact source location.
type Provenance struct {
	// Source indexes into Document.Sources, or is NoSource for a node that
	// addresses no input file. Nothing else is in range.
	Source int `json:"source"`
	// Pointer locates the construct: a JSON pointer or line:col into Source for
	// anything read from a file, or an IR-space location — a stable ID, or a path
	// through the document's own fields — for a finding an IR pass made about the
	// document rather than about a source. Spelling is the producer's; nothing
	// parses this.
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
// the enclosing Document round-trips through JSON byte-for-byte (invariant
// #7): a third-party validator can emit a truncated multibyte rune in its
// error text, and json.Marshal silently rewrites invalid UTF-8 to U+FFFD,
// breaking that round-trip if left uncoerced until marshal time. irverify's
// ir/diagnostic-invalid-utf8 check flags any message that still reaches a
// Document ill-formed; strings.ToValidUTF8 doesn't allocate when message is
// already valid, so the common path costs one scan.
func NewDiagnostic(sev Severity, code, message string, prov Provenance) Diagnostic {
	return Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    strings.ToValidUTF8(message, "\uFFFD"),
		Provenance: prov,
	}
}

// FirstError returns the first error-severity diagnostic in diags and true,
// or the zero Diagnostic and false if none exists — a two-value return so a
// zero-value error diagnostic can't be mistaken for "no error found".
//
// Compilers and the harness use it to tell a refusal (a real spec problem)
// from advisory warnings that must be carried forward, and to report the
// offending diagnostic once a refusal is confirmed.
func FirstError(diags []Diagnostic) (Diagnostic, bool) {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return d, true
		}
	}
	return Diagnostic{}, false
}

// HasError reports whether diags contains at least one error-severity
// diagnostic. It is FirstError's boolean-only form, for call sites — an if
// condition, a fuzz-target skip gate — that only need the yes/no answer and
// cannot consume a two-value return.
func HasError(diags []Diagnostic) bool {
	_, ok := FirstError(diags)
	return ok
}

// SourceInfo describes one input file of a Document.
type SourceInfo struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Hash   string `json:"hash"`
}
