// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_ATypeWithAWayOutOfItsTypeCycleHasAFiniteValue holds the types that
// look like a type cycle and are not: each leads back to itself, and each has a
// value that needs no other. A list with no bounds at all may be empty, a
// union with a variant that is null or names nothing the document declares
// has that variant for a value, and a map may be empty as well.
func TestValidate_ATypeWithAWayOutOfItsTypeCycleHasAFiniteValue(t *testing.T) {
	t.Parallel()
	for name, def := range map[string]ir.TypeDef{
		"a list of itself with no bounds":          &ir.List{ID: "type/k", ElementType: ref("type/k"), Provenance: provAt("type/k")},
		"a map of itself with no bounds":           &ir.MapT{ID: "type/k", Value: ref("type/k"), Provenance: provAt("type/k")},
		"a map of itself that may be empty":        &ir.MapT{ID: "type/k", Value: ref("type/k"), Constraints: &ir.Constraints{MinProperties: new(int64(0))}, Provenance: provAt("type/k")},
		"a union with a variant that is nullable":  &ir.Union{ID: "type/k", Variants: []ir.Variant{{Type: ref("type/k")}, {Type: maybe("type/primitive/string")}}, Provenance: provAt("type/k")},
		"a union with a variant nothing declares":  &ir.Union{ID: "type/k", Variants: []ir.Variant{{Type: ref("type/k")}, {Type: ref("type/ghost")}}, Provenance: provAt("type/k")},
		"a union of nothing":                       &ir.Union{ID: "type/k", Provenance: provAt("type/k")},
		"a model requiring itself, where nullable": composed("type/k", nil, needs("property/k/0", maybe("type/k"))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, found(t, noFiniteValueDoc(def), finiteCode))
		})
	}

	forced := &ir.MapT{ID: "type/k", Value: ref("type/k"), Constraints: &ir.Constraints{MinProperties: new(int64(1))}, Provenance: provAt("type/k")}
	assert.Equal(t, []finding{{"type/k", "no finite value: each needs its value, type/k, which has none either"}},
		found(t, noFiniteValueDoc(forced), finiteCode), "a map that must hold an entry")
}

// TestValidate_AModelComposingTheUnionThatListsItHasAFiniteValue holds the
// pattern docs/ir/models.md names, a union of Circle and Square each of which
// composes the union, to no finding: the model is a conjunct of the union and
// narrows nothing, each instance of the union being one variant, so a base or a
// mixin that is a union, or an alias of one, is no need of the model.
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
	alias := func(id, base string) *ir.Scalar {
		return &ir.Scalar{ID: ir.TypeID(id), Base: new(ref(base)), Provenance: provAt(id)}
	}
	doc := noFiniteValueDoc(
		extending("type/circle", "type/shape", false), extending("type/square", "type/shape", false), listing("type/shape", "type/circle", "type/square"),
		extending("type/log", "type/rule", true), extending("type/block", "type/rule", true), listing("type/rule", "type/log", "type/block"),
		extending("type/oval", "type/figure", false), alias("type/figure", "type/rounded"), alias("type/rounded", "type/round"), listing("type/round", "type/oval"),
	)

	assert.Empty(t, found(t, doc, finiteCode))
	assert.Empty(t, found(t, doc, cycleCode), "a union has no composition to loop")
}
