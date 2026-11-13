// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report

import (
	"cmp"
	"encoding/json/jsontext"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// Explanation is what a compile produced at one source coordinate: the type
// node its document holds there, the coordinates beneath it that hold one, the
// constructs kept verbatim at or beneath it, and the diagnostics stamped at it.
// A pass may replace or remove a node, so it describes the document.
//
// It answers "why did my example disappear" from the document alone: every
// hoisted node records its coordinate in Provenance.Pointer, so the
// coordinate-to-node relation is recoverable without widening the compiler
// contract. It does not say which reader filled which field; the annotation
// readers are not separable units.
type Explanation struct {
	// Pointer is the coordinate explained; "" is the whole document.
	Pointer jsontext.Pointer `json:"pointer"`
	// Node is the type node the document holds at Pointer, or nil when it
	// holds none: none was interned there, or a pass removed it.
	Node *Interned `json:"node,omitzero"`
	// Below holds the coordinates strictly beneath Pointer that hold a node,
	// in pointer order and, at one pointer, in ID order.
	Below []Interned `json:"below,omitempty"`
	// Diagnostics holds the diagnostics stamped at Pointer, in reported order.
	Diagnostics []ir.Diagnostic `json:"diagnostics,omitempty"`
	// Unmodeled holds the Unmodeled entries written at or beneath Pointer, in
	// pointer and then key order, and an entry kept on several nodes in the
	// order the document holds them.
	Unmodeled []Kept `json:"unmodeled,omitempty"`
	// Truncated reports that the document nests past ir.MaxWalkDepth, so
	// Unmodeled can lack an entry held deeper.
	Truncated bool `json:"truncated,omitzero"`
}

// Kept is one Unmodeled entry: a construct the document keeps verbatim rather
// than models, and the node keeping it.
type Kept struct {
	// Pointer is the coordinate the construct is written at.
	Pointer jsontext.Pointer `json:"pointer"`
	// Key is the entry's key in its Unmodeled map.
	Key    string             `json:"key"`
	Reason ir.UnmodeledReason `json:"reason"`
	// Holder is the path to the node whose Unmodeled map holds the entry, as
	// ir.WalkValues spells it, such as doc.Types[<id>]. A construct a compiler
	// keeps on several nodes, as a path item's on each operation, is one Kept
	// per node.
	Holder string `json:"holder"`
}

// Interned is one source coordinate and the type node it interned.
type Interned struct {
	Pointer jsontext.Pointer `json:"pointer"`
	ID      ir.TypeID        `json:"id"`
	Kind    ir.TypeKind      `json:"kind"`
}

// Explain reports what the compile that produced doc and diags produced at
// pointer in the source it compiled: a position in the source is read, whoever
// wrote it, an overlay included, and one in another document, a split source's
// or the overlay's own, is not, since its pointer spells a position of that
// document. A nil document has no types; a nil registry entry, untyped or a
// typed nil pointer, is skipped rather than dereferenced, since a malformed
// registry is what pass.Validate and irverify exist to report.
func Explain(doc *ir.Document, diags []ir.Diagnostic, pointer jsontext.Pointer) Explanation {
	e := Explanation{Pointer: pointer}
	for _, id := range sortedTypeIDs(doc) {
		td := doc.Types[id]
		if ir.IsNilTypeDef(td) {
			continue
		}
		prov := td.Common().Provenance
		if !inSource(prov.Source) {
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
		if inSource(d.Provenance.Source) && stampedAt(d.Provenance, pointer) {
			e.Diagnostics = append(e.Diagnostics, d)
		}
	}
	e.Unmodeled, e.Truncated = keptAtOrBelow(doc, pointer)
	return e
}

// unmodeledType is the type of the maps keptAtOrBelow reads.
var unmodeledType = reflect.TypeFor[ir.Unmodeled]()

// keptAtOrBelow returns doc's Unmodeled entries written in its source at or
// beneath pointer, sorted, and whether the walk finding them was truncated. It
// walks the whole document, since every kind of node can carry the map. The
// walk is the IR's own, so a map a new field holds is read at once; it reads the
// entries' fields, as one reached through an unexported field cannot be read
// whole.
func keptAtOrBelow(doc *ir.Document, pointer jsontext.Pointer) ([]Kept, bool) {
	var kept []Kept
	truncated := ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Type() != unmodeledType {
			return true // any other value may still hold a map below it
		}
		holder := strings.TrimSuffix(at.String(), ".Unmodeled")
		for iter := v.MapRange(); iter.Next(); {
			entry := iter.Value()
			prov := entry.FieldByName("Provenance")
			written := jsontext.Pointer(prov.FieldByName("Pointer").String())
			if inSource(int(prov.FieldByName("Source").Int())) && pointer.Contains(written) {
				kept = append(kept, Kept{Pointer: written, Key: iter.Key().String(),
					Reason: ir.UnmodeledReason(entry.FieldByName("Reason").String()), Holder: holder})
			}
		}
		return false // an entry holds no node, so no map is beneath one
	}))
	slices.SortStableFunc(kept, func(a, b Kept) int {
		return cmp.Or(cmp.Compare(a.Pointer, b.Pointer), cmp.Compare(a.Key, b.Key))
	})
	return kept, truncated
}

// inSource reports whether a position the Document.Sources entry at source
// names is in the source's coordinates: it is the source's own, the first
// entry. An overlay's writing into the source is located there too, at the
// source's index with the overlay as its writer.
func inSource(source int) bool {
	return source == 0
}

// WriteExplanation writes e to w as the lines --explain prints: the
// coordinate, the node at it or a line saying the document holds none, what is
// interned below it and what is kept unmodeled at or below it when anything
// is, and its diagnostics. What the kept entries' lines spell is the
// document's, escaped (printable). It stops at the first write that fails and
// returns that write's error, and refuses a nil w.
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
		lw.printf("  the document holds no type node at this coordinate\n")
	}

	if len(e.Below) > 0 {
		lw.printf("  interned below it (%d):\n", len(e.Below))
		for _, c := range e.Below {
			lw.printf("    %s -> %s (%s)\n", c.Pointer, c.ID, c.Kind)
		}
	}

	writeKept(lw, e)

	lw.printf("  diagnostics (%d):\n", len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		lw.printf("    %s %s: %s\n", d.Severity, d.Code, d.Message)
	}
	return lw.err
}

// writeKept writes e's Unmodeled entries, a line each, when it has any, and a
// line saying the list can lack some when the walk was truncated.
func writeKept(lw *lineWriter, e Explanation) {
	if len(e.Unmodeled) > 0 {
		lw.printf("  kept unmodeled at or below it (%d):\n", len(e.Unmodeled))
		for _, k := range e.Unmodeled {
			lw.printf("    %s -> %s (%s) on %s\n",
				printable(string(k.Pointer)), printable(k.Key), printable(string(k.Reason)), printable(k.Holder))
		}
	}
	if e.Truncated {
		lw.printf("  the document nests deeper than the walk reads, so more may be kept below\n")
	}
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
