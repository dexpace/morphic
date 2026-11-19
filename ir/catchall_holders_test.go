// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestCatchAllHolders_ReadsTheFlatteningsModels holds the holders to the
// types a member of a value of root falls under: a map read through its
// aliases, and each model its flattening reads holding a catch-all, root's
// first, through every kind of composition entry, each once however it is
// reached, and nothing for a type naming no map or model.
func TestCatchAllHolders_ReadsTheFlatteningsModels(t *testing.T) {
	t.Parallel()
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	open := &ir.AdditionalProperties{Value: &stringType}
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	types := ir.TypeRegistry{
		"type/primitive/string": &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: "string"},
		"type/map":              &ir.MapT{ID: "type/map", Value: stringType},
		"type/alias":            &ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/map"}},
		"type/loop1":            &ir.Scalar{ID: "type/loop1", Base: &ir.TypeRef{Target: "type/loop2"}},
		"type/loop2":            &ir.Scalar{ID: "type/loop2", Base: &ir.TypeRef{Target: "type/loop1"}},
		"type/root": &ir.Model{ID: "type/root", AdditionalProperties: open, Base: &ir.TypeRef{Target: "type/base"},
			Implements: []ir.TypeRef{ref("type/iface")}, Mixins: []ir.TypeRef{ref("type/mixin"), ref("type/base")}},
		"type/base":    &ir.Model{ID: "type/base", AdditionalProperties: open, Base: &ir.TypeRef{Target: "type/root"}},
		"type/iface":   &ir.Model{ID: "type/iface", AdditionalProperties: open, Mixins: []ir.TypeRef{ref("type/map")}},
		"type/mixin":   &ir.Model{ID: "type/mixin", Mixins: []ir.TypeRef{ref("type/through")}},
		"type/through": &ir.Scalar{ID: "type/through", Base: &ir.TypeRef{Target: "type/deep"}},
		"type/deep":    &ir.Model{ID: "type/deep", AdditionalProperties: open},
		"type/closed":  &ir.Model{ID: "type/closed"},
		"type/nil":     (*ir.Model)(nil),
	}
	for _, tc := range []struct {
		name string
		root ir.TypeID
		want []ir.TypeID
	}{
		{"a map", "type/map", []ir.TypeID{"type/map"}},
		{"a map through an alias", "type/alias", []ir.TypeID{"type/map"}},
		{"a model's composition, each once", "type/root", []ir.TypeID{"type/root", "type/base", "type/iface", "type/deep"}},
		{"a model with no catch-all", "type/closed", nil},
		{"a typed nil", "type/nil", nil},
		{"a cyclic alias chain", "type/loop1", nil},
		{"no type", "type/ghost", nil},
		{"a primitive", "type/primitive/string", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.CatchAllHolders(types, tc.root))
		})
	}
}
