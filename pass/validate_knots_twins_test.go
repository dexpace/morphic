// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_ATypeWithAWayOutOfItsKnotHasAFiniteValue holds the types that
// look like a knot and are not: each leads back to itself, and each has a
// value that needs no other. A list with no bounds at all may be empty, a
// union with a variant that is null or names nothing the document declares
// has that variant for a value, and a map may be empty as well.
func TestValidate_ATypeWithAWayOutOfItsKnotHasAFiniteValue(t *testing.T) {
	t.Parallel()
	for name, def := range map[string]ir.TypeDef{
		"a list of itself with no bounds":          &ir.List{ID: "t/k", Elem: ref("t/k"), Provenance: provAt("t/k")},
		"a map of itself with no bounds":           &ir.MapT{ID: "t/k", Value: ref("t/k"), Provenance: provAt("t/k")},
		"a map of itself that may be empty":        &ir.MapT{ID: "t/k", Value: ref("t/k"), Constraints: &ir.Constraints{MinProps: new(int64(0))}, Provenance: provAt("t/k")},
		"a union with a variant that is nullable":  &ir.Union{ID: "t/k", Variants: []ir.Variant{{Type: ref("t/k")}, {Type: maybe("t/prim/string")}}, Provenance: provAt("t/k")},
		"a union with a variant nothing declares":  &ir.Union{ID: "t/k", Variants: []ir.Variant{{Type: ref("t/k")}, {Type: ref("t/ghost")}}, Provenance: provAt("t/k")},
		"a union of nothing":                       &ir.Union{ID: "t/k", Provenance: provAt("t/k")},
		"a model requiring itself, where nullable": composed("t/k", nil, needs("p/k/0", maybe("t/k"))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, found(t, knotDoc(def), finiteCode))
		})
	}

	forced := &ir.MapT{ID: "t/k", Value: ref("t/k"), Constraints: &ir.Constraints{MinProps: new(int64(1))}, Provenance: provAt("t/k")}
	assert.Equal(t, []finding{{"t/k", "no finite value: each needs its value, t/k, which has none either"}},
		found(t, knotDoc(forced), finiteCode), "a map that must hold an entry")
}

// TestValidate_AModelComposingTheUnionThatListsItHasAFiniteValue holds the
// pattern docs/ir/models.md names, a union of Circle and Square each of which
// composes the union, to no finding: the model is a conjunct of the union and
// narrows nothing, each instance of the union being one variant, so a base or a
// mixin that is a union is no need of the model.
func TestValidate_AModelComposingTheUnionThatListsItHasAFiniteValue(t *testing.T) {
	t.Parallel()
	extending := func(id, union string, asMixin bool) *ir.Model {
		m := composed(id, nil)
		if asMixin {
			m.Mixins = []ir.TypeRef{ref(union)}
		} else {
			m.Base = new(ref(union))
		}
		return m
	}
	listing := func(id string, variants ...string) *ir.Union {
		u := &ir.Union{ID: ir.TypeID(id), Provenance: provAt(id)}
		for _, v := range variants {
			u.Variants = append(u.Variants, ir.Variant{Type: ref(v)})
		}
		return u
	}
	doc := knotDoc(
		extending("t/circle", "t/shape", false), extending("t/square", "t/shape", false), listing("t/shape", "t/circle", "t/square"),
		extending("t/log", "t/rule", true), extending("t/block", "t/rule", true), listing("t/rule", "t/log", "t/block"),
	)

	assert.Empty(t, found(t, doc, finiteCode))
	assert.Empty(t, found(t, doc, cycleCode), "a union has no composition to loop")
}
