// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// Reporter records one diagnostic at pointer, attributed to the input document
// that supplied that position.
type Reporter func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any)

// Source renders the construct a declaration was written as, verbatim. It is a
// function because nearly every property is declared once, and rendering a node
// nobody will keep is a cost the merge asks for only when it has something to
// keep.
type Source func() (ir.RawValue, error)

// Mint registers node, which stands for the declarations of the field pointer
// names (fieldSpan), and returns the ID it is registered under. It fills what in
// node's TypeCommon names the node, which is the lowering's to derive, and keeps
// the rest.
type Mint func(pointer jsontext.Pointer, node ir.TypeDef) ir.TypeID

// Settle folds what a PropIndex queued through g, whose Resolve reads the
// registry every lowering left.
type Settle func(g *Merger)

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
// model's fields fold once the registry is whole (Settle). Which type a field
// folds to compares the nodes its declarations name, which the walk may not
// have built yet, so a fold mid-walk would follow declaration order (GitHub
// #898).
type PropIndex struct {
	m      *ir.Model
	later  func(Settle)
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
// every property reaches it through MergeProperty. later queues the model's
// folds, once, at its first redeclaration, for once every lowering has
// returned; it must not be nil.
func NewPropIndex(m *ir.Model, later func(Settle)) *PropIndex {
	return &PropIndex{m: m, later: later, byWire: map[string]*field{}}
}

// MergeProperty appends p to the model, or records it as one more declaration
// of the field that already carries its wire name: overlapping allOf branches
// (and properties co-declared alongside allOf) redeclare one logical field
// under allOf's intersection semantics
// (docs/compilers/openapi.md#allof-redeclared-property). Callers must set
// p.WireName (fillModelProperties always does); it keys idx directly.
//
// p.Provenance locates the declaration and is the only source of that fact
// here, so no second copy of it can disagree. secretBy names the heuristics
// that set p.Secret, empty where a keyword declared it (folding.foldUse).
func (idx *PropIndex) MergeProperty(p ir.Property, source Source, secretBy ir.Inferred) {
	f, ok := idx.byWire[p.WireName]
	if !ok {
		idx.byWire[p.WireName] = &field{pos: len(idx.m.Properties), first: source}
		idx.m.Properties = append(idx.m.Properties, p)
		return
	}
	if len(f.rest) == 0 {
		if len(idx.redeclared) == 0 {
			idx.later(idx.settle)
		}
		idx.redeclared = append(idx.redeclared, f)
	}
	f.rest = append(f.rest, declaration{prop: p, source: source, secretBy: secretBy})
}

// settle folds each field declared more than once against reg, in the order of
// its first redeclaration. A field's first declaration is read off the model as
// the walk left it, required flag included. A fold reads only nodes the walk
// built and writes only its own field, and two folds mint under one pointer
// only where they fold one set of declarations (fieldSpan), so no fold reads
// another's result. The model's discriminator read the field's ID off the walk,
// so it follows the field to any identity of its own.
func (idx *PropIndex) settle(g *Merger) {
	for _, f := range idx.redeclared {
		first := idx.m.Properties[f.pos]
		merged := g.fold(append([]declaration{{prop: first, source: f.first}}, f.rest...))
		idx.m.Properties[f.pos] = merged
		if d := idx.m.Discriminator; d != nil && d.Property == first.ID {
			d.Property = merged.ID
		}
	}
}
