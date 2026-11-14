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
	str := ir.TypeRef{Target: "t/prim/string"}
	open := &ir.AdditionalProps{Value: &str}
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	types := ir.TypeRegistry{
		"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: "string"},
		"t/map":         &ir.MapT{ID: "t/map", Value: str},
		"t/alias":       &ir.Scalar{ID: "t/alias", Base: &ir.TypeRef{Target: "t/map"}},
		"t/loop1":       &ir.Scalar{ID: "t/loop1", Base: &ir.TypeRef{Target: "t/loop2"}},
		"t/loop2":       &ir.Scalar{ID: "t/loop2", Base: &ir.TypeRef{Target: "t/loop1"}},
		"t/root": &ir.Model{ID: "t/root", AdditionalProps: open, Base: &ir.TypeRef{Target: "t/base"},
			Implements: []ir.TypeRef{ref("t/iface")}, Mixins: []ir.TypeRef{ref("t/mixin"), ref("t/base")}},
		"t/base":    &ir.Model{ID: "t/base", AdditionalProps: open, Base: &ir.TypeRef{Target: "t/root"}},
		"t/iface":   &ir.Model{ID: "t/iface", AdditionalProps: open, Mixins: []ir.TypeRef{ref("t/map")}},
		"t/mixin":   &ir.Model{ID: "t/mixin", Mixins: []ir.TypeRef{ref("t/through")}},
		"t/through": &ir.Scalar{ID: "t/through", Base: &ir.TypeRef{Target: "t/deep"}},
		"t/deep":    &ir.Model{ID: "t/deep", AdditionalProps: open},
		"t/closed":  &ir.Model{ID: "t/closed"},
		"t/nil":     (*ir.Model)(nil),
	}
	for _, tc := range []struct {
		name string
		root ir.TypeID
		want []ir.TypeID
	}{
		{"a map", "t/map", []ir.TypeID{"t/map"}},
		{"a map through an alias", "t/alias", []ir.TypeID{"t/map"}},
		{"a model's composition, each once", "t/root", []ir.TypeID{"t/root", "t/base", "t/iface", "t/deep"}},
		{"a model with no catch-all", "t/closed", nil},
		{"a typed nil", "t/nil", nil},
		{"a cyclic alias chain", "t/loop1", nil},
		{"no type", "t/ghost", nil},
		{"a primitive", "t/prim/string", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.CatchAllHolders(types, tc.root))
		})
	}
}
