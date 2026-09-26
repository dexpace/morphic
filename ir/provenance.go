package ir

import (
	"encoding/json/jsontext"
	"strings"
)

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
//
// Each kind of locator has a field of its own, because a consumer holding one
// cannot tell which kind it is from its spelling: a renderer printed a line and
// column as a pointer fragment (GitHub #509), and no check could hold a pointer
// to RFC 6901 while the same field admitted the other two (GitHub #511).
type Provenance struct {
	// Source indexes into Document.Sources, or is NoSource for a node that
	// addresses no input file. Nothing else is in range.
	Source int `json:"source"`
	// Pointer is the RFC 6901 pointer to the construct inside Source. Empty
	// locates nothing finer than the source itself: it is the pointer to the
	// whole document, and what a node with no single place in it records.
	Pointer jsontext.Pointer `json:"pointer,omitempty"`
	// Position is where the construct starts inside Source, for a finding made
	// before the construct has a pointer — on a raw node, or in a part of the
	// source no pointer reaches.
	Position Position `json:"position,omitzero"`
	// Node locates a finding in the IR rather than in a source: a stable ID, or a
	// path through the document's own fields, for what an IR pass reports about
	// the document it was handed. Spelling is the producer's; nothing parses it.
	Node string `json:"node,omitempty"`
	// Inferred is "" for declared facts; otherwise it names the heuristic that
	// produced this node (e.g. "pagination-name-match").
	Inferred string `json:"inferred,omitempty"`
}

// Position is a 1-based line and column inside a source. The zero value is no
// position, and a zero Column is a line whose column the producer does not
// know.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column,omitzero"`
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
// the enclosing Document can be written at all: a third-party validator can
// emit a truncated multibyte rune in its error text, and a Document refuses to
// encode a string that is not UTF-8 rather than rewrite it to U+FFFD. irverify's
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
