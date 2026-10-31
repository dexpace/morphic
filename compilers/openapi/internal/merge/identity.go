// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/ir"
)

// fieldPointer returns the pointer of the field decls make together: the wire
// name under `properties` of the narrowest schema holding every declaration,
// whether or not that schema writes it there. Each schema that folds one set of
// declarations reaches that schema, and no two sets reach one, because a
// wrapper's declarations are all in every model that flattens it. The first or
// last declaration alone could not say so: a model and a wrapper it flattens
// may share either while folding different sets.
func fieldPointer(decls []declaration) jsontext.Pointer {
	first := decls[0].prop.Provenance.Pointer
	schema := schemaOf(first)
	for _, d := range decls[1:] {
		for !schema.Contains(d.prop.Provenance.Pointer) {
			schema = schemaOf(schema)
		}
	}
	return schema.AppendToken("properties").AppendToken(first.LastToken())
}

// schemaOf returns the schema a property or an allOf branch at pointer is
// written in, two tokens above either.
func schemaOf(pointer jsontext.Pointer) jsontext.Pointer { return pointer.Parent().Parent() }

// identify gives merged the identity of field where a model holding its first
// declaration without the others would otherwise share it: a $ref to an allOf
// branch makes one (ir-design §3.1). Otherwise the field keeps the first
// declaration's identity, which the walk has already handed out.
func (g *Merger) identify(merged *ir.Property, field jsontext.Pointer) {
	if !g.carriedApart(merged.Provenance.Pointer, field) {
		return
	}
	merged.ID = g.PropAt(field)
	merged.Provenance.Pointer = field
}

// carriedApart reports whether a node stands at a schema inside field's that
// holds the declaration at first. Such a node's field holds fewer declarations
// than this one, so the two are different fields.
func (g *Merger) carriedApart(first, field jsontext.Pointer) bool {
	whole := schemaOf(field)
	for schema := schemaOf(first); len(schema) > len(whole); schema = schemaOf(schema) {
		if g.Interned(schema) {
			return true
		}
	}
	return false
}
