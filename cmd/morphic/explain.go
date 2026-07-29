package main

import (
	"io"
	"sort"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// explainDocument reports what a compile produced at one source coordinate: the
// type node interned there, the coordinates interned beneath it, and every
// diagnostic stamped at it.
//
// It answers "why did my example disappear" from the emitted document alone.
// Every hoisted node records the coordinate it came from in Provenance.Pointer,
// so the coordinate-to-node relation the compiler builds during lowering is
// recoverable afterwards without widening the compiler contract to expose the
// walk's internal map.
//
// What it does not report is which reader filled which field. That would need
// the annotation readers to be separable units, which they are not; claiming it
// here would describe a compiler this is not.
func explainDocument(w io.Writer, doc *ir.Document, diags []ir.Diagnostic, pointer string) {
	emitf(w, "coordinate %s\n", pointer)

	if id, td, ok := nodeAtPointer(doc, pointer); ok {
		emitf(w, "  node %s (%s)\n", id, td.Kind())
	} else {
		emitf(w, "  no type node was interned at this coordinate\n")
	}

	below := coordinatesBelow(doc, pointer)
	if len(below) > 0 {
		emitf(w, "  interned below it (%d):\n", len(below))
		for _, c := range below {
			emitf(w, "    %s -> %s (%s)\n", c.pointer, c.id, c.kind)
		}
	}

	at := diagnosticsAt(diags, pointer)
	emitf(w, "  diagnostics (%d):\n", len(at))
	for _, d := range at {
		emitf(w, "    %s %s: %s\n", d.Severity, d.Code, d.Message)
	}
}

// nodeAtPointer returns the type node whose provenance names pointer exactly.
//
// A nil entry is skipped rather than dereferenced: a malformed registry is what
// pass.Validate and irverify exist to report, and explain must not be the thing
// that crashes on one.
func nodeAtPointer(doc *ir.Document, pointer string) (ir.TypeID, ir.TypeDef, bool) {
	for _, id := range sortedTypeIDs(doc) {
		td := doc.Types[id]
		if td == nil {
			continue
		}
		if td.Common().Provenance.Pointer == pointer {
			return id, td, true
		}
	}
	return "", nil, false
}

// coordinate is one interned source position and the node it produced.
type coordinate struct {
	pointer string
	id      ir.TypeID
	kind    ir.TypeKind
}

// coordinatesBelow returns the coordinates strictly beneath pointer, in pointer
// order. It is what makes a miss actionable: a schema that lowered to a shared
// primitive owns no node at its own coordinate, and seeing what did intern below
// it is the difference between "nothing happened here" and "the node moved".
func coordinatesBelow(doc *ir.Document, pointer string) []coordinate {
	prefix := strings.TrimSuffix(pointer, "/") + "/"
	var out []coordinate
	for _, id := range sortedTypeIDs(doc) {
		td := doc.Types[id]
		if td == nil {
			continue
		}
		p := td.Common().Provenance.Pointer
		if strings.HasPrefix(p, prefix) {
			out = append(out, coordinate{pointer: p, id: id, kind: td.Kind()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pointer < out[j].pointer })
	return out
}

// diagnosticsAt returns the diagnostics stamped at pointer, in emitted order.
func diagnosticsAt(diags []ir.Diagnostic, pointer string) []ir.Diagnostic {
	out := make([]ir.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Provenance.Pointer == pointer {
			out = append(out, d)
		}
	}
	return out
}

// sortedTypeIDs returns the registry's keys in sorted order so explain output is
// deterministic (invariant 7).
func sortedTypeIDs(doc *ir.Document) []ir.TypeID {
	if doc == nil {
		return nil
	}
	ids := make([]ir.TypeID, 0, len(doc.Types))
	for id := range doc.Types {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
