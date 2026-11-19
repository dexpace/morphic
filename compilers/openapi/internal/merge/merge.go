// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// Source renders the construct a declaration was written as, verbatim. It is a
// function because nearly every property is declared once, and rendering a node
// nobody will keep is a cost the merge asks for only when it has something to
// keep.
type Source func() (ir.RawValue, error)

// WireNameIndex maps each property's wire name to its position in props, so a
// lookup by name is one map read rather than a rescan of a wide model.
func WireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// PropIndex is one Model's composition as the walk reads it: each property it
// declares and each entry it composes, in the order the walk meets them, which
// is the order the source writes them, a schema's own properties after its
// allOf. The first declaration of a wire name lands on the model at once, so
// the walk can read its ID and mark it required. The model's members settle
// once the registry is whole (Settle): which type a field folds to compares
// nodes the walk may not have built yet, so a fold mid-walk would follow
// declaration order (GitHub #898).
type PropIndex struct {
	m      *ir.Model
	byWire map[string]*field
	// entries are the types the model composes, as Compose records them.
	entries []composedEntry
	// read counts what the walk has met, declarations and entries alike.
	read int
	// redeclared reports whether a wire name has been declared twice.
	redeclared bool
	// typeAt is where the nodes of the type of each member Settle folded may
	// be written, by wire name (Merger.typeSites).
	typeAt map[string][]ir.Provenance
}

// field is one wire name the model declares: where its property sits, and
// every declaration of it, the first one's property read off the model when
// the field settles.
type field struct {
	pos   int
	decls []declaration
}

// composedEntry is a type the model composes, where the model writes it, and when the
// walk met it among the model's declarations.
type composedEntry struct {
	target ir.TypeID
	at     jsontext.Pointer
	order  int
}

// NewPropIndex returns an empty index of m's properties; m holds none yet, so
// every property reaches it through MergeProperty.
func NewPropIndex(m *ir.Model) *PropIndex {
	return &PropIndex{m: m, byWire: map[string]*field{}, typeAt: map[string][]ir.Provenance{}}
}

// MergeProperty appends p to the model, or records it as one more declaration
// of the field that already carries its wire name: overlapping allOf entries
// (and properties co-declared alongside allOf) redeclare one logical field
// under allOf's intersection semantics
// (docs/compilers/openapi.md#allof-redeclared-property). Callers must set
// p.WireName (fillModelProperties always does); it keys idx directly.
//
// p.Provenance alone locates the declaration, so no second copy can disagree.
// secretBy names the heuristics that set p.Secret, empty where a keyword
// declared it (folding.foldUse). It reports whether p is the model's first
// redeclaration, from which the model has members to settle.
func (idx *PropIndex) MergeProperty(p ir.Property, source Source, secretBy ir.Inferred) (firstRedeclaration bool) {
	d := declaration{prop: p, source: source, secretBy: secretBy, order: idx.next()}
	f, ok := idx.byWire[p.WireName]
	if !ok {
		idx.byWire[p.WireName] = &field{pos: len(idx.m.Properties), decls: []declaration{d}}
		idx.m.Properties = append(idx.m.Properties, p)
		return false
	}
	first := !idx.redeclared
	idx.redeclared = true
	f.decls = append(f.decls, d)
	return first
}

// Compose records that the model composes target, written at at, after what
// the walk has met so far. The caller adds target to the model's Base or
// Mixins; the index reads it for the declarations it brings (Settle).
func (idx *PropIndex) Compose(target ir.TypeID, at jsontext.Pointer) {
	idx.entries = append(idx.entries, composedEntry{target: target, at: at, order: idx.next()})
}

// next returns the place of what the walk meets next.
func (idx *PropIndex) next() int {
	idx.read++
	return idx.read
}
