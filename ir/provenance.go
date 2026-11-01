// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"slices"
	"strings"
)

// NoSource is the Source value for a node that addresses no input file. An IR
// pass reporting on the document it was handed has no source to name, and a
// shared primitive, reached by kind from every source, has no one source to
// name. Every other index — 0 included — names an input file that was actually
// read, which would make a renderer fabricate a location.
//
// It is the only out-of-table Source value the IR declares: irverify accepts it
// and reports every other index that addresses no declared source, so a producer
// that invents a second sentinel is caught rather than tolerated.
const NoSource = -1

// Provenance records where a node came from and whether it was declared or
// inferred (docs/ir/provenance.md). Everything heuristic is auditable;
// everything broken is reportable with an exact source location.
//
// Each kind of locator has a field of its own, because a consumer holding one
// cannot tell which kind it is from its spelling: a renderer printed a line and
// column as a pointer fragment (GitHub #509), and no check could hold a pointer
// to RFC 6901 while the same field admitted the other two (GitHub #511).
type Provenance struct {
	// Source indexes into Document.Sources, or for a refused compile's finding
	// into the table that document would have carried (docs/ir/provenance.md),
	// or is NoSource for a node that addresses no input file. Nothing else is
	// in range.
	Source int `json:"source"`
	// Pointer is the RFC 6901 pointer to the construct inside Source. Empty
	// locates nothing finer than the source itself: it is the pointer to the
	// whole document, so a node with no single place in any source, such as a
	// shared primitive, is on NoSource rather than at the root of one.
	Pointer jsontext.Pointer `json:"pointer,omitempty"`
	// Position is where the construct starts inside Source, for a finding made
	// on a raw node: beside the node's Pointer when the tree holds it, alone for
	// a part of the source no pointer reaches and for the document's top-level
	// mapping, whose pointer is the empty one.
	Position Position `json:"position,omitzero"`
	// Node locates a finding in the IR rather than in a source: a stable ID, or a
	// path through the document's own fields, for what an IR pass reports about
	// the document it was handed. Spelling is the producer's; nothing parses it.
	Node string `json:"node,omitempty"`
	// Inferred is empty for a declared fact; otherwise it lists the heuristics
	// that produced this node, in the order they applied ([Inferred]), e.g.
	// "group-path-prefix,extension-promotion".
	Inferred Inferred `json:"inferred,omitempty"`
}

// InTable reports whether p.Source addresses one of a source table's n entries,
// or is NoSource, the one value outside every table the IR declares. It is the
// test each holder of a table applies to what indexes it, so none reads the
// sentinel apart from the others.
func (p Provenance) InTable(n int) bool {
	return p.Source == NoSource || 0 <= p.Source && p.Source < n
}

// Position is a 1-based line and column inside a source. The zero value is no
// position, and a zero Column is a line whose column the producer does not
// know.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column,omitzero"`
}

// Severity classifies a Diagnostic as one of the constants below. The engine
// decides from it whether a run fails, and refuses a diagnostic carrying any
// other value, which no threshold could rank.
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
// ir/invalid-utf8 check flags any message that still reaches a Document
// ill-formed; strings.ToValidUTF8 doesn't allocate when message is already
// valid, so the common path costs one scan.
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
	i := slices.IndexFunc(diags, func(d Diagnostic) bool { return d.Severity == SeverityError })
	if i < 0 {
		return Diagnostic{}, false
	}
	return diags[i], true
}

// HasError reports whether diags contains at least one error-severity
// diagnostic. It is FirstError's boolean-only form, for call sites — an if
// condition, a fuzz-target skip gate — that only need the yes/no answer and
// cannot consume a two-value return.
func HasError(diags []Diagnostic) bool {
	_, ok := FirstError(diags)
	return ok
}

// FirstOutsideTable returns the first diagnostic in diags whose provenance
// indexes none of a source table's n entries (Provenance.InTable) and true, or
// the zero Diagnostic and false if each indexes one. A holder of a table asks
// it of the findings that will be read against the table.
func FirstOutsideTable(diags []Diagnostic, n int) (Diagnostic, bool) {
	i := slices.IndexFunc(diags, func(d Diagnostic) bool { return !d.Provenance.InTable(n) })
	if i < 0 {
		return Diagnostic{}, false
	}
	return diags[i], true
}

// MergeDiagnostics returns stored followed by every diagnostic of produced
// that stored does not already hold, identity being the whole value, the one
// compilers/compile dedupes on. A compiler hands its findings back on two
// channels and need fill only one, so taking either list over the other loses
// what the other held.
//
// The result is freshly allocated when there is anything to merge: the two
// lists routinely alias, and appending into a shared backing array would
// overwrite entries still to be read.
func MergeDiagnostics(stored, produced []Diagnostic) []Diagnostic {
	if len(produced) == 0 {
		return stored
	}
	held := make(map[Diagnostic]struct{}, len(stored))
	for _, d := range stored {
		held[d] = struct{}{}
	}
	merged := make([]Diagnostic, len(stored), len(stored)+len(produced))
	copy(merged, stored)
	for _, d := range produced {
		if _, dup := held[d]; dup {
			continue
		}
		merged = append(merged, d)
	}
	return merged
}

// SourceInfo describes one input file of a Document. The table a refused
// compile's findings index names files nothing read, so it sets Path alone.
type SourceInfo struct {
	// Format names the format and version the file was read as, such as
	// "openapi@3.2", or "overlay@1.0.0" for an overlay (IsOverlay).
	Format string `json:"format"`
	// Path is the path the file was handed to the compiler under.
	Path string `json:"path"`
	// Hash is the lowercase hex SHA-256 of the file's bytes, as SourceHash
	// spells it.
	Hash string `json:"hash"`
	// SelfURI is the URI the document declared for itself, OpenAPI's `$self`,
	// as written: a relative reference is not resolved against Path. It is
	// empty when the document declared none, and for an empty `$self`, which
	// says the same: an empty reference resolves to the location it was read
	// from (RFC 3986 §5.2.2), the location a document declaring none is known by.
	//
	// It is the value the compiler lowered, so a `$self` an overlay writes is
	// recorded here, on the patched document's entry, not on the overlay's.
	SelfURI string `json:"selfURI,omitempty"`
}

// SourceHash returns the SourceInfo.Hash of a file whose bytes are data: their
// SHA-256 in lowercase hex. It is the one spelling a producer writes and a
// caller holding the bytes checks, so the two cannot drift apart.
func SourceHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// OverlayFormat is the format an overlay applied to the source is read as,
// before the "@" and the overlay version its Format carries.
const OverlayFormat = "overlay"

// IsOverlay reports whether s is an overlay applied to the source. A construct
// an overlay wrote into the document it patched is credited to the overlay at
// its position in that document, so a Provenance naming an overlay can spell
// a position of either document. Another document a split source reads names
// its own positions alone.
func (s SourceInfo) IsOverlay() bool {
	return strings.HasPrefix(s.Format, OverlayFormat+"@")
}
