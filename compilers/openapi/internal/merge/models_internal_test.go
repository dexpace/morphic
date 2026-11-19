// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// alikeRegistry holds a component Owner, inline copies of it written at the
// first and second declarations of a field, and models that differ from it in
// one way each. Every model names a nested object, so a copy's nested node is
// compared as a copy too.
func alikeRegistry() map[ir.TypeID]ir.TypeDef {
	stringType := primitiveRef(ir.PrimitiveString)
	reg := narrowingRegistry()
	owner := func(id ir.TypeID, at jsontext.Pointer, anonymous bool, name ir.TypeRef, addr ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Anonymous: anonymous, Provenance: ptrAt(at),
			Properties: []ir.Property{
				{ID: ir.PropertyID("property" + at + "/name"), WireName: "name", Type: name},
				{ID: ir.PropertyID("property" + at + "/addr"), WireName: "addr", Type: ref(addr)},
			}}
	}
	addr := func(id ir.TypeID, at jsontext.Pointer, anonymous bool) *ir.Model {
		return &ir.Model{ID: id, Anonymous: anonymous, Provenance: ptrAt(at),
			Properties: []ir.Property{{ID: ir.PropertyID("property" + at + "/street"), WireName: "street", Type: stringType}}}
	}
	for _, model := range []*ir.Model{
		owner("type/Owner", "/components/schemas/Owner", false, stringType, "type/Owner/addr"),
		addr("type/Owner/addr", "/components/schemas/Owner/properties/addr", true),
		owner("type/Twin", "/components/schemas/Twin", false, stringType, "type/Twin/addr"),
		addr("type/Twin/addr", "/components/schemas/Twin/properties/addr", true),
		addr("type/Addr", "/components/schemas/Addr", false),
		owner("type/Home", "/components/schemas/Home", false, stringType, "type/Addr"),
		owner("type/House", "/components/schemas/House", false, stringType, "type/Addr"),
		owner("type/copy0", declaredAt(0), true, stringType, "type/copy0/addr"),
		addr("type/copy0/addr", declaredAt(0)+"/properties/addr", true),
		owner("type/copy1", declaredAt(1), true, stringType, "type/copy1/addr"),
		addr("type/copy1/addr", declaredAt(1)+"/properties/addr", true),
		owner("type/copy1-addr", declaredAt(1), true, stringType, "type/Addr"),
		owner("type/copy1-uuid", declaredAt(1), true, primitiveRef(ir.PrimitiveUUID), "type/copy1/addr"),
		owner("type/copy1-int", declaredAt(1), true, primitiveRef(ir.PrimitiveInteger), "type/copy1/addr"),
	} {
		reg[model.ID] = model
	}
	more := *reg["type/copy1"].(*ir.Model)
	more.Properties = append(slices.Clone(more.Properties), ir.Property{WireName: "more", Type: stringType})
	reg["type/copy1-more"] = &more
	required := *reg["type/copy1"].(*ir.Model)
	required.Properties = slices.Clone(required.Properties)
	required.Properties[0].Required = true
	reg["type/copy1-required"] = &required
	for n, id := range []ir.TypeID{"type/copy0-tagged", "type/copy1-tagged"} {
		model := *reg[ir.TypeID(fmt.Sprintf("type/copy%d", n))].(*ir.Model)
		model.Discriminator = &ir.Discriminator{Property: model.Properties[0].ID}
		reg[id] = &model
	}
	for n, id := range []ir.TypeID{"type/copy0-ref", "type/copy1-ref"} {
		model := *reg[ir.TypeID(fmt.Sprintf("type/copy%d", n))].(*ir.Model)
		model.Properties = slices.Clone(model.Properties)
		model.Properties[0].Unmodeled = ir.Unmodeled{"openapi:not": {Value: ir.RawValue(`{"$ref":"#n"}`)}}
		reg[id] = &model
	}
	for n, id := range []ir.TypeID{"type/copy0-open", "type/copy1-open", "type/copy1-open-other"} {
		model := *reg[ir.TypeID(fmt.Sprintf("type/copy%d", min(n, 1)))].(*ir.Model)
		value := ref(ir.TypeID(fmt.Sprintf("type/copy%d/addr", min(n, 1))))
		if n == 2 {
			value = primitiveRef(ir.PrimitiveInteger)
		}
		model.AdditionalProperties = &ir.AdditionalProperties{Value: &value, Key: new(primitiveRef(ir.PrimitiveString)),
			Patterns: []ir.PatternProperties{{Pattern: "^x", Value: primitiveRef(ir.PrimitiveString)}}}
		model.Implements, model.Base = []ir.TypeRef{ref("type/Addr")}, new(ref("type/Addr"))
		reg[id] = &model
	}
	byAddr := *reg["type/copy1"].(*ir.Model)
	byAddr.Discriminator = &ir.Discriminator{Property: byAddr.Properties[1].ID}
	reg["type/copy1-tagged-addr"] = &byAddr
	stray := *reg["type/copy1"].(*ir.Model)
	stray.Discriminator = &ir.Discriminator{Property: "property/elsewhere"}
	reg["type/copy1-stray-tag"] = &stray
	return reg
}

// TestTypeJudge_ModelsAlikeAreCopies pins GitHub #1083's verdicts, each in both
// orders: two models alike in every member are judged as two identical nodes
// are, a component winning over an inline copy of itself and two inline copies
// being one type, where each type they name, a nested object included, is
// judged the same way. A model differing in a member, or naming a type that
// relates otherwise, is unrelated, and two components alike overlap where what
// they name is one type, so either composes as two models do.
func TestTypeJudge_ModelsAlikeAreCopies(t *testing.T) {
	t.Parallel()
	reg := alikeRegistry()
	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		want relation
	}{
		{"a component, then an inline copy", "type/Owner", "type/copy1", firstTypeIsNarrower},
		{"an inline copy, then a component", "type/copy0", "type/Owner", secondTypeIsNarrower},
		{"two inline copies", "type/copy0", "type/copy1", relSame},
		{"two components alike, naming one nested type", "type/Home", "type/House", relOverlap},
		{"two components alike, each nesting its own object", "type/Owner", "type/Twin", relUnrelated},
		{"a copy naming a component where the other names a copy", "type/copy0", "type/copy1-addr", relUnrelated},
		{"a copy with one more property", "type/copy0", "type/copy1-more", relUnrelated},
		{"a copy requiring a property the other does not", "type/copy0", "type/copy1-required", relUnrelated},
		{"a copy whose member is narrower", "type/copy0", "type/copy1-uuid", relUnrelated},
		{"a component whose member is broader than a copy's", "type/Owner", "type/copy1-uuid", relUnrelated},
		{"a copy whose member is disjoint", "type/copy0", "type/copy1-int", relUnrelated},
		{"two copies tagged by the same property", "type/copy0-tagged", "type/copy1-tagged", relSame},
		{"a tag naming no property of its model", "type/copy0", "type/copy1-stray-tag", relUnrelated},
		{"two copies tagged by different properties", "type/copy0-tagged", "type/copy1-tagged-addr", relUnrelated},
		{"two copies with one catch-all and composition", "type/copy0-open", "type/copy1-open", relSame},
		{"two copies whose catch-alls differ", "type/copy0-open", "type/copy1-open-other", relUnrelated},
	} {
		for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: declarationSites{
				walk: &stubWalk{}, written: [2][]ir.Provenance{sitesAt(declaredAt(i)), sitesAt(declaredAt(1 - i))}}}
			want := tc.want
			if i == 1 {
				want = want.flipped()
			}
			assert.Equal(t, want, typeJudgeValue.relate(ref(order[0]), ref(order[1])), "%s (order %d)", tc.name, i)
		}
	}
}

// TestTypeJudge_ModelsKeepingAReferenceAreAlikeUnderOneBase pins that a member
// keeping a reference under Unmodeled reads as the same text only against one
// base, as a node's own entry does (declarationSites.copies): under two, the
// copies are two types.
func TestTypeJudge_ModelsKeepingAReferenceAreAlikeUnderOneBase(t *testing.T) {
	t.Parallel()
	reg := alikeRegistry()
	for _, tc := range []struct {
		apart bool
		want  relation
	}{
		{false, relSame},
		{true, relUnrelated},
	} {
		typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: declarationSites{
			walk: &stubWalk{apart: tc.apart}, written: [2][]ir.Provenance{sitesAt(declaredAt(0)), sitesAt(declaredAt(1))}}}
		assert.Equal(t, tc.want, typeJudgeValue.relate(ref("type/copy0-ref"), ref("type/copy1-ref")))
	}
}

// TestFold_ModelsAlikeAreOneType pins what a field declared as alike models
// takes: the component over an inline copy of it, whichever comes first, and the
// first of two inline copies, with nothing kept or reported. Two components
// alike still compose.
func TestFold_ModelsAlikeAreOneType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		targets []ir.TypeID
		want    ir.TypeID
	}{
		{"a component, then a copy", []ir.TypeID{"type/Owner", "type/copy1"}, "type/Owner"},
		{"a copy, then a component", []ir.TypeID{"type/copy0", "type/Owner"}, "type/Owner"},
		{"two copies", []ir.TypeID{"type/copy0", "type/copy1"}, "type/copy0"},
		{"two components", []ir.TypeID{"type/Owner", "type/Twin"}, composedAt(declaredAt(1))},
	} {
		reg := alikeRegistry()
		stubbed, recorded := stubMerger(reg)
		decls := declsOf(tc.targets...)
		for i := range decls {
			decls[i].source = unread(t)
		}

		got := stubbed.fold(decls)

		assert.Equal(t, tc.want, got.Type.Target, tc.name)
		assert.Empty(t, *recorded, tc.name)
		assert.Empty(t, got.Unmodeled, tc.name)
	}
}

// TestTypeJudge_ModelPairsStopAtTheirBudget pins maxModelPairs from both sides:
// two copies naming as many nested pairs as the budget leaves are one type, and
// one pair more leaves them unrelated, which composes them.
func TestTypeJudge_ModelPairsStopAtTheirBudget(t *testing.T) {
	t.Parallel()
	for _, nested := range []int{maxModelPairs - 1, maxModelPairs} {
		reg := map[ir.TypeID]ir.TypeDef{"type/primitive/string": &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString}}
		for n := range 2 {
			outer := &ir.Model{ID: ir.TypeID(fmt.Sprintf("type/%d", n)), Anonymous: true,
				Provenance: ptrAt(declaredAt(n))}
			for k := range nested {
				at := declaredAt(n) + jsontext.Pointer(fmt.Sprintf("/properties/f%d", k))
				id := ir.TypeID(fmt.Sprintf("type/%d/%d", n, k))
				reg[id] = &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(at),
					Properties: []ir.Property{{WireName: "x", Type: primitiveRef(ir.PrimitiveString)}}}
				outer.Properties = append(outer.Properties, ir.Property{WireName: fmt.Sprintf("f%d", k), Type: ref(id)})
			}
			reg[outer.ID] = outer
		}
		typeJudgeValue := typeJudge{resolve: stubResolve(reg), sites: declarationSites{
			walk: &stubWalk{}, written: [2][]ir.Provenance{sitesAt(declaredAt(0)), sitesAt(declaredAt(1))}}}

		want := relSame
		if nested == maxModelPairs {
			want = relUnrelated
		}
		assert.Equal(t, want, typeJudgeValue.relate(ref("type/0"), ref("type/1")), "%d nested pairs", nested)
	}
}

// TestTypeJudge_NestedModelsCostTheirBudgetNotTheirWidthToTheDepth pins why the
// budget exists: each level's properties name one model of the next, as $refs
// to a sibling position write, so comparing them all would read width^depth
// nodes from a document as small as width times depth.
func TestTypeJudge_NestedModelsCostTheirBudgetNotTheirWidthToTheDepth(t *testing.T) {
	t.Parallel()
	const width, depth = 4, maxJudgeDepth
	reg := map[ir.TypeID]ir.TypeDef{}
	for n := range 2 {
		for level := range depth {
			id := ir.TypeID(fmt.Sprintf("type/%d/%d", n, level))
			next := ref(ir.TypeID(fmt.Sprintf("type/%d/%d", n, level+1)))
			model := &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n))}
			for k := range width {
				model.Properties = append(model.Properties, ir.Property{WireName: fmt.Sprintf("f%d", k), Type: next})
			}
			reg[id] = model
		}
		leaf := ir.TypeID(fmt.Sprintf("type/%d/%d", n, depth))
		reg[leaf] = &ir.Primitive{ID: leaf, PrimitiveKind: ir.PrimitiveString}
	}
	reads := 0
	typeJudgeValue := typeJudge{
		resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			reads++
			td, ok := reg[id]
			return td, ok
		},
		sites: declarationSites{written: [2][]ir.Provenance{sitesAt(declaredAt(0)), sitesAt(declaredAt(1))}, walk: &stubWalk{}},
	}

	rel := typeJudgeValue.relate(ref("type/0/0"), ref("type/1/0"))

	require.Equal(t, relUnrelated, rel, "past the budget the two are unrelated")
	assert.LessOrEqual(t, reads, 4*maxModelPairs*width, "the reads grow with the budget, not with the width to the depth")
}
