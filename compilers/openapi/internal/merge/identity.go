// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// fieldSpan returns the narrowest schema holding every declaration of a field,
// and the declaration that names the field: the last to widen that schema, so
// the first outside the branch holding decls[0]. A model and a wrapper it
// flattens can share a first or a last declaration while folding different sets,
// but not this one: a wrapper's is never its own first, and an enclosing fold's
// is at most the wrapper's first. Folds of one set reach the same declaration.
// Past the flattening depth a model can miss what its wrapper reads, and
// Register refuses that collision.
func fieldSpan(decls []declaration) (schema, field jsontext.Pointer) {
	field = decls[0].prop.Provenance.Pointer
	schema = schemaOf(field)
	for _, d := range decls[1:] {
		held := d.prop.Provenance.Pointer
		if schema.Contains(held) {
			continue
		}
		for !schema.Contains(held) {
			schema = schemaOf(schema)
		}
		field = held
	}
	return schema, field
}

// schemaOf returns the schema a property or an allOf branch at pointer is
// written in, two tokens above either.
func schemaOf(pointer jsontext.Pointer) jsontext.Pointer { return pointer.Parent().Parent() }

// identify gives merged an identity of its own, named for field, where a model
// holding its first declaration without the others would otherwise share it: a
// $ref to an allOf branch makes one. That is a minted identity, so it takes a
// namespace of its own (ir-design §3.1). Otherwise the field keeps the first
// declaration's, which the walk has already handed out. schema holds every
// declaration (fieldSpan).
func (g *Merger) identify(merged *ir.Property, schema, field jsontext.Pointer) {
	if !g.carriedApart(merged.Provenance.Pointer, schema) {
		return
	}
	merged.ID = g.PropAt(field)
	merged.Provenance.Pointer = field
}

// carriedApart reports whether a node stands at a schema inside whole that holds
// the declaration at first. Such a node's field holds fewer declarations than
// one of whole's, so the two are different fields.
func (g *Merger) carriedApart(first, whole jsontext.Pointer) bool {
	for schema := schemaOf(first); len(schema) > len(whole); schema = schemaOf(schema) {
		if g.Interned(schema) {
			return true
		}
	}
	return false
}
