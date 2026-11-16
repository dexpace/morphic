// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package dialect

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// Table is the rows each schema of a compile is read with: by where it is
// (At), by its node (Of), and by its document (In). Nothing writes it once a
// Builder returns it, so every copy may read it at once. The zero Table reads
// every schema with the zero rows, which define nothing.
type Table struct {
	// byRoot is the rows of each schema resource root naming its dialect
	// (bind.Document.Dialects), and byNode those of each mapping and list such
	// a root's tree holds, a root it holds governing its own.
	byRoot map[bind.Position]oas.SchemaRules
	byNode map[*yaml.Node]oas.SchemaRules
	// byDoc is the rows each document's schemas no root governs are read with,
	// the source's at bind.Source.
	byDoc map[bind.DocID]oas.SchemaRules
}

// At returns the rows the schema at p is read with: those of the innermost
// root at or above it naming its dialect, else those of p's document, else
// the source's, as for a document the compile did not read beside it.
func (t Table) At(p bind.Position) oas.SchemaRules {
	if r, ok := bind.DialectAt(t.byRoot, p); ok {
		return r
	}
	return t.In(p.Doc)
}

// RootAt returns the rows of the root naming its dialect at p, and false
// where none is there, a root above p not counted: what a check entering the
// schema at p switches to.
func (t Table) RootAt(p bind.Position) (oas.SchemaRules, bool) {
	r, ok := t.byRoot[p]
	return r, ok
}

// Of is At for a reading that has the schema's node and not where it is: the
// rows of the root whose tree holds it, else the source's. A node an alias
// places at several positions reads at all of them the rows of the first root
// to govern it (Builder.Root), so a reading asks Of of a schema the bindings
// place nowhere, and asks At of one they place.
func (t Table) Of(n *yaml.Node) oas.SchemaRules {
	if r, ok := t.byNode[nodeview.Deref(n)]; ok {
		return r
	}
	return t.byDoc[bind.Source]
}

// In returns the rows the schemas document id declares are read with where
// no root names a dialect: the document's own, else the source's, as for the
// source and for a document the compile did not read beside it.
func (t Table) In(id bind.DocID) oas.SchemaRules {
	if r, ok := t.byDoc[id]; ok {
		return r
	}
	return t.byDoc[bind.Source]
}

// Builder fills a Table as the load phase reads each document's dialects:
// each document's rows (Document), and each root naming one, innermost first
// (Root). It is used once, by one goroutine, and Table hands its maps over.
type Builder struct {
	t Table
}

// NewBuilder returns a Builder of a Table reading the source's schemas no
// root governs with source.
func NewBuilder(source oas.SchemaRules) *Builder {
	return &Builder{t: Table{byRoot: map[bind.Position]oas.SchemaRules{}, byNode: map[*yaml.Node]oas.SchemaRules{},
		byDoc: map[bind.DocID]oas.SchemaRules{bind.Source: source}}}
}

// Document notes that the schemas of document id that no root governs are
// read with rules.
func (b *Builder) Document(id bind.DocID, rules oas.SchemaRules) {
	b.t.byDoc[id] = rules
}

// Root notes that the schema resource root at at, whose node is node, names a
// dialect read with rules, and governs what its tree holds (Govern). Roots
// are noted innermost first, so a nested root keeps its own.
func (b *Builder) Root(at bind.Position, node *yaml.Node, rules oas.SchemaRules) {
	b.t.byRoot[at] = rules
	b.Govern(node, rules)
}

// Govern notes that every mapping and list of the tree under root, which
// aliases place there included, reads rules, but those a root governed before
// it holds. No scalar is noted, since the only schema one can be is a boolean,
// which writes no keyword. Each mapping and list is walked once, so the walk
// reads each entry the document writes once.
func (b *Builder) Govern(root *yaml.Node, rules oas.SchemaRules) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := nodeview.Deref(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.ScalarNode {
			continue
		}
		if _, governed := b.t.byNode[n]; governed {
			continue
		}
		b.t.byNode[n] = rules
		stack = append(stack, n.Content...)
	}
}

// Table returns the table b filled. b is not used after.
func (b *Builder) Table() Table {
	return b.t
}
