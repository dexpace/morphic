// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// declaredIn returns a declaration of string type at each pointer.
func declaredIn(pointers ...jsontext.Pointer) []declaration {
	decls := make([]declaration, 0, len(pointers))
	for _, p := range pointers {
		decls = append(decls, declaration{prop: ir.Property{
			ID: ir.PropID("p/test" + p), WireName: "p", Provenance: ptrAt(p), Type: ir.TypeRef{Target: "t/prim/string"},
		}, source: written(`{}`)})
	}
	return decls
}

// TestFieldSpan_NamesEachSetOfDeclarations pins the one key that tells two folds
// apart. A model and a wrapper it flattens can share a first declaration or a
// last one, so neither names a field: the declaration that last widened the
// schema holding them all does, and it is one the source wrote. Each pair of
// rows sharing a first or a last differs in that. It reads tokens, so an index
// that prefixes another is no ancestor, and a property named allOf is no allOf.
func TestFieldSpan_NamesEachSetOfDeclarations(t *testing.T) {
	t.Parallel()
	const (
		a  = "/s/allOf/0/properties/p"
		b  = "/s/allOf/1/properties/p"
		in = "/s/allOf/0/allOf/0/properties/p"
		on = "/s/allOf/0/allOf/1/properties/p"
		x  = "/s/allOf/1/allOf/0/properties/p"
		y  = "/s/allOf/1/allOf/1/properties/p"
	)
	for _, tc := range []struct {
		name   string
		decls  []jsontext.Pointer
		schema jsontext.Pointer
		field  jsontext.Pointer
	}{
		{"two branches", []jsontext.Pointer{a, b}, "/s", b},
		{"a branch and the schema's own", []jsontext.Pointer{a, "/s/properties/p"}, "/s", "/s/properties/p"},
		{"two branches of a wrapper", []jsontext.Pointer{in, on}, "/s/allOf/0", on},
		{"that wrapper, then a later branch", []jsontext.Pointer{in, on, b}, "/s", b},
		{"a branch, then a wrapper", []jsontext.Pointer{a, x, y}, "/s", x},
		{"that wrapper alone", []jsontext.Pointer{x, y}, "/s/allOf/1", y},
		{"two wrappers", []jsontext.Pointer{in, on, x, y}, "/s", x},
		{"a wrapper's own beside its branch", []jsontext.Pointer{in, "/s/allOf/0/properties/p"}, "/s/allOf/0", "/s/allOf/0/properties/p"},
		{"an index prefixing another", []jsontext.Pointer{"/s/allOf/1/properties/p", "/s/allOf/10/properties/p"}, "/s", "/s/allOf/10/properties/p"},
		{
			"a schema under a property named allOf",
			[]jsontext.Pointer{"/s/properties/allOf/allOf/0/properties/p", "/s/properties/allOf/allOf/1/properties/p"},
			"/s/properties/allOf", "/s/properties/allOf/allOf/1/properties/p",
		},
		{"a declaration inside what the first spans", []jsontext.Pointer{"/s/properties/p", a}, "/s", "/s/properties/p"},
	} {
		schema, field := fieldSpan(declaredIn(tc.decls...))
		assert.Equal(t, tc.schema, schema, tc.name)
		assert.Equal(t, tc.field, field, tc.name)
	}
}

// TestFold_AFieldAnotherModelCarriesTakesAnIdentityOfItsOwn pins when a field
// leaves its first declaration's identity: where a node inside the schema
// holding every declaration holds the first, since that model's field is a
// different one. A node at that schema, or above it, folds these same
// declarations, so it is this one field. The identity is a minted one, named for
// a declaration the source wrote.
func TestFold_AFieldAnotherModelCarriesTakesAnIdentityOfItsOwn(t *testing.T) {
	t.Parallel()
	const (
		first = "/s/allOf/0/allOf/0/properties/p"
		next  = "/s/allOf/0/allOf/1/properties/p"
		later = "/s/allOf/1/properties/p"
	)
	for _, tc := range []struct {
		name    string
		decls   []jsontext.Pointer
		nodes   []jsontext.Pointer
		id      ir.PropID
		pointer jsontext.Pointer
	}{
		{"no other node", []jsontext.Pointer{first, later}, nil, "p/test" + first, first},
		{"the branch holding the first", []jsontext.Pointer{first, later}, []jsontext.Pointer{"/s/allOf/0/allOf/0"}, "p/minted" + later, later},
		{"a wrapper around it", []jsontext.Pointer{first, later}, []jsontext.Pointer{"/s/allOf/0"}, "p/minted" + later, later},
		{"the schema holding every declaration", []jsontext.Pointer{first, later}, []jsontext.Pointer{"/s"}, "p/test" + first, first},
		{"a wrapper holding every declaration", []jsontext.Pointer{first, next}, []jsontext.Pointer{"/s/allOf/0"}, "p/test" + first, first},
		{"a branch holding only a later one", []jsontext.Pointer{first, later}, []jsontext.Pointer{"/s/allOf/1"}, "p/test" + first, first},
	} {
		g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}})
		g.Interned = func(p jsontext.Pointer) bool { return slices.Contains(tc.nodes, p) }

		got := g.fold(declaredIn(tc.decls...))

		assert.Equal(t, tc.pointer, got.Provenance.Pointer, tc.name)
		assert.Equal(t, tc.id, got.ID, tc.name)
	}
}

// TestPropIndex_ADiscriminatorFollowsItsFieldToANewIdentity pins the one
// reference the walk takes off a field before it folds: a model's discriminator
// keeps naming the property, and one naming another field is left alone.
func TestPropIndex_ADiscriminatorFollowsItsFieldToANewIdentity(t *testing.T) {
	t.Parallel()
	const first, later = "/s/allOf/0/properties/p", "/s/allOf/1/properties/p"
	for _, tc := range []struct {
		name  string
		names ir.PropID
		want  ir.PropID
	}{
		{"the folded field", "p/test" + first, "p/minted" + later},
		{"another field", "p/test/other", "p/test/other"},
	} {
		g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}})
		g.Interned = func(p jsontext.Pointer) bool { return p == "/s/allOf/0" }
		m := &ir.Model{Discriminator: &ir.Discriminator{Property: tc.names}}
		idx, settle := indexOver(m, g)
		for _, d := range declaredIn(first, later) {
			idx.MergeProperty(d.prop, d.source)
		}

		settle()

		assert.Equal(t, tc.want, m.Discriminator.Property, tc.name)
	}
}
