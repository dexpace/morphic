// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
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

// PropIndex is the properties of one Model as the walk declares them. The
// first declaration of a wire name lands on the model at once, so the walk can
// read its ID and mark it required; every later one is recorded, and the
// model's fields fold once the registry is whole (Fold). Which type a field
// folds to compares the nodes its declarations name, which the walk may not
// have built yet, so a fold mid-walk would follow declaration order (GitHub
// #898).
type PropIndex struct {
	m      *ir.Model
	byWire map[string]*field
	// redeclared lists the fields declared more than once, in the order of
	// their first redeclaration.
	redeclared []*field
}

// field is one wire name of the model: where its property sits, how to render
// the declaration that put it there, and each declaration after that one.
type field struct {
	pos   int
	first Source
	rest  []declaration
}

// NewPropIndex returns an empty index of m's properties; m holds none yet, so
// every property reaches it through MergeProperty.
func NewPropIndex(m *ir.Model) *PropIndex {
	return &PropIndex{m: m, byWire: map[string]*field{}}
}

// MergeProperty appends p to the model, or records it as one more declaration
// of the field that already carries its wire name: overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field
// under allOf's intersection semantics
// (docs/compilers/openapi.md#allof-redeclared-property). Callers must set
// p.WireName (fillModelProperties always does); it keys idx directly.
//
// p.Provenance alone locates the declaration, so no second copy can disagree.
// secretBy names the heuristics that set p.Secret, empty where a keyword
// declared it (folding.foldUse). It reports whether p is the model's first
// redeclaration, where the walk queues the model's folds (Fold).
func (idx *PropIndex) MergeProperty(p ir.Property, source Source, secretBy ir.Inferred) (firstRedeclaration bool) {
	f, ok := idx.byWire[p.WireName]
	if !ok {
		idx.byWire[p.WireName] = &field{pos: len(idx.m.Properties), first: source}
		idx.m.Properties = append(idx.m.Properties, p)
		return false
	}
	first := len(idx.redeclared) == 0
	if len(f.rest) == 0 {
		idx.redeclared = append(idx.redeclared, f)
	}
	f.rest = append(f.rest, declaration{prop: p, source: source, secretBy: secretBy})
	return first
}

// Fold folds each field declared more than once through g, in the order of
// its first redeclaration, and returns what the folds report. A field's first
// declaration is read off the model as the walk left it, required flag
// included. A fold reads only nodes the walk built and writes only its own
// field, and two folds mint under one pointer only where they fold one set of
// declarations (fieldSpan), so no fold reads another's result. The model's
// discriminator read the field's ID off the walk, so it follows the field to
// any identity of its own.
func (idx *PropIndex) Fold(g Merger) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, f := range idx.redeclared {
		first := idx.m.Properties[f.pos]
		merged, found := g.fold(append([]declaration{{prop: first, source: f.first}}, f.rest...))
		diags = append(diags, found...)
		idx.m.Properties[f.pos] = merged
		if d := idx.m.Discriminator; d != nil && d.Property == first.ID {
			d.Property = merged.ID
		}
	}
	return diags
}
