package report

import (
	"cmp"
	"encoding/json/jsontext"
	"io"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Explanation is what a compile produced at one source coordinate: the type
// node interned there, the coordinates interned beneath it, and the
// diagnostics stamped at it.
//
// It answers "why did my example disappear" from the document alone: every
// hoisted node records its coordinate in Provenance.Pointer, so the
// coordinate-to-node relation is recoverable without widening the compiler
// contract. It does not say which reader filled which field; the annotation
// readers are not separable units.
type Explanation struct {
	// Pointer is the coordinate explained; "" is the whole document.
	Pointer jsontext.Pointer `json:"pointer"`
	// Node is the type node interned at Pointer, or nil when none was.
	Node *Interned `json:"node,omitzero"`
	// Below holds the coordinates strictly beneath Pointer that interned a
	// node, in pointer order and, at one pointer, in ID order.
	Below []Interned `json:"below,omitempty"`
	// Diagnostics holds the diagnostics stamped at Pointer, in reported order.
	Diagnostics []ir.Diagnostic `json:"diagnostics,omitempty"`
}

// Interned is one source coordinate and the type node it interned.
type Interned struct {
	Pointer jsontext.Pointer `json:"pointer"`
	ID      ir.TypeID        `json:"id"`
	Kind    ir.TypeKind      `json:"kind"`
}

// Explain reports what the compile that produced doc and diags produced at
// pointer in the source it compiled. sources is the table each
// Provenance.Source indexes, a run's engine.Result.Sources: a position in the
// source, or credited to an overlay patching it (ir.SourceInfo.IsOverlay), is
// read, and one in another document a split source reads is not, since its
// pointer spells a position of that document. A nil document has no types; a
// nil registry entry, untyped or a typed nil pointer, is skipped rather than
// dereferenced, since a malformed registry is what pass.Validate and irverify
// exist to report.
func Explain(doc *ir.Document, diags []ir.Diagnostic, sources []ir.SourceInfo, pointer jsontext.Pointer) Explanation {
	e := Explanation{Pointer: pointer}
	for _, id := range sortedTypeIDs(doc) {
		td := doc.Types[id]
		if ir.IsNilTypeDef(td) {
			continue
		}
		prov := td.Common().Provenance
		if !inSource(sources, prov.Source) {
			continue
		}
		// The first node at the pointer, in ID order, names it: several can
		// share one, and the answer must not follow map order (invariant 7).
		if e.Node == nil && stampedAt(prov, pointer) {
			e.Node = &Interned{Pointer: pointer, ID: id, Kind: td.Kind()}
		}
		if prov.Pointer != pointer && pointer.Contains(prov.Pointer) {
			e.Below = append(e.Below, Interned{Pointer: prov.Pointer, ID: id, Kind: td.Kind()})
		}
	}
	slices.SortStableFunc(e.Below, func(a, b Interned) int { return cmp.Compare(a.Pointer, b.Pointer) })
	for _, d := range diags {
		if inSource(sources, d.Provenance.Source) && stampedAt(d.Provenance, pointer) {
			e.Diagnostics = append(e.Diagnostics, d)
		}
	}
	return e
}

// inSource reports whether a position in the Document.Sources entry at source
// is located in the source's coordinates: it is the source's own, the first
// entry, or an overlay's, which locates what it patched there.
func inSource(sources []ir.SourceInfo, source int) bool {
	if source == 0 {
		return true
	}
	return source > 0 && source < len(sources) && sources[source].IsOverlay()
}

// WriteExplanation writes e to w as the lines --explain prints: the
// coordinate, the node at it or a line saying none was interned, what interned
// below it when anything did, and its diagnostics. It stops at the first write
// that fails and returns that write's error, and refuses a nil w.
func WriteExplanation(w io.Writer, e Explanation) error {
	if w == nil {
		return errNoWriter
	}
	lw := &lineWriter{w: w}
	shown := string(e.Pointer)
	if e.Pointer == "" {
		shown = `"" (the whole document)` // printed bare, the root reads as a missing argument
	}
	lw.printf("coordinate %s\n", shown)

	if e.Node != nil {
		lw.printf("  node %s (%s)\n", e.Node.ID, e.Node.Kind)
	} else {
		lw.printf("  no type node was interned at this coordinate\n")
	}

	if len(e.Below) > 0 {
		lw.printf("  interned below it (%d):\n", len(e.Below))
		for _, c := range e.Below {
			lw.printf("    %s -> %s (%s)\n", c.Pointer, c.ID, c.Kind)
		}
	}

	lw.printf("  diagnostics (%d):\n", len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		lw.printf("    %s %s: %s\n", d.Severity, d.Code, d.Message)
	}
	return lw.err
}

// stampedAt reports whether prov locates a node or finding at pointer in a
// source. A shared primitive and an IR pass's finding sit on NoSource, in no
// source at all, so neither is at any coordinate (GitHub #528). An empty
// Pointer beside a position is not the whole document's: no pointer and the
// top-level mapping's are spelled alike, and either way the position locates
// the finding finer, so matching it at the root would put every such one
// there.
func stampedAt(prov ir.Provenance, pointer jsontext.Pointer) bool {
	if prov.Pointer != pointer || prov.Source == ir.NoSource {
		return false
	}
	return pointer != "" || prov.Position == ir.Position{}
}

// sortedTypeIDs returns the registry's keys in sorted order, or none for a nil
// document.
func sortedTypeIDs(doc *ir.Document) []ir.TypeID {
	if doc == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(doc.Types))
}
