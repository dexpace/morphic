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
	str := primRef(ir.PrimString)
	reg := narrowingRegistry()
	owner := func(id ir.TypeID, at jsontext.Pointer, anonymous bool, name ir.TypeRef, addr ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Anonymous: anonymous, Provenance: ptrAt(at),
			Properties: []ir.Property{
				{ID: ir.PropID("p" + at + "/name"), WireName: "name", Type: name},
				{ID: ir.PropID("p" + at + "/addr"), WireName: "addr", Type: ref(addr)},
			}}
	}
	addr := func(id ir.TypeID, at jsontext.Pointer, anonymous bool) *ir.Model {
		return &ir.Model{ID: id, Anonymous: anonymous, Provenance: ptrAt(at),
			Properties: []ir.Property{{ID: ir.PropID("p" + at + "/street"), WireName: "street", Type: str}}}
	}
	for _, m := range []*ir.Model{
		owner("t/Owner", "/components/schemas/Owner", false, str, "t/Owner/addr"),
		addr("t/Owner/addr", "/components/schemas/Owner/properties/addr", true),
		owner("t/Twin", "/components/schemas/Twin", false, str, "t/Twin/addr"),
		addr("t/Twin/addr", "/components/schemas/Twin/properties/addr", true),
		addr("t/Addr", "/components/schemas/Addr", false),
		owner("t/Home", "/components/schemas/Home", false, str, "t/Addr"),
		owner("t/House", "/components/schemas/House", false, str, "t/Addr"),
		owner("t/copy0", declaredAt(0), true, str, "t/copy0/addr"),
		addr("t/copy0/addr", declaredAt(0)+"/properties/addr", true),
		owner("t/copy1", declaredAt(1), true, str, "t/copy1/addr"),
		addr("t/copy1/addr", declaredAt(1)+"/properties/addr", true),
		owner("t/copy1-addr", declaredAt(1), true, str, "t/Addr"),
		owner("t/copy1-uuid", declaredAt(1), true, primRef(ir.PrimUUID), "t/copy1/addr"),
		owner("t/copy1-int", declaredAt(1), true, primRef(ir.PrimInteger), "t/copy1/addr"),
	} {
		reg[m.ID] = m
	}
	more := *reg["t/copy1"].(*ir.Model)
	more.Properties = append(slices.Clone(more.Properties), ir.Property{WireName: "more", Type: str})
	reg["t/copy1-more"] = &more
	required := *reg["t/copy1"].(*ir.Model)
	required.Properties = slices.Clone(required.Properties)
	required.Properties[0].Required = true
	reg["t/copy1-required"] = &required
	for n, id := range []ir.TypeID{"t/copy0-tagged", "t/copy1-tagged"} {
		m := *reg[ir.TypeID(fmt.Sprintf("t/copy%d", n))].(*ir.Model)
		m.Discriminator = &ir.Discriminator{Property: m.Properties[0].ID}
		reg[id] = &m
	}
	for n, id := range []ir.TypeID{"t/copy0-ref", "t/copy1-ref"} {
		m := *reg[ir.TypeID(fmt.Sprintf("t/copy%d", n))].(*ir.Model)
		m.Properties = slices.Clone(m.Properties)
		m.Properties[0].Unmodeled = ir.Unmodeled{"openapi:not": {Value: ir.RawValue(`{"$ref":"#n"}`)}}
		reg[id] = &m
	}
	for n, id := range []ir.TypeID{"t/copy0-open", "t/copy1-open", "t/copy1-open-other"} {
		m := *reg[ir.TypeID(fmt.Sprintf("t/copy%d", min(n, 1)))].(*ir.Model)
		value := ref(ir.TypeID(fmt.Sprintf("t/copy%d/addr", min(n, 1))))
		if n == 2 {
			value = primRef(ir.PrimInteger)
		}
		m.AdditionalProps = &ir.AdditionalProps{Value: &value, Key: new(primRef(ir.PrimString)),
			Patterns: []ir.PatternProps{{Pattern: "^x", Value: primRef(ir.PrimString)}}}
		m.Implements, m.Base = []ir.TypeRef{ref("t/Addr")}, new(ref("t/Addr"))
		reg[id] = &m
	}
	stray := *reg["t/copy1"].(*ir.Model)
	stray.Discriminator = &ir.Discriminator{Property: "p/elsewhere"}
	reg["t/copy1-stray-tag"] = &stray
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
		{"a component, then an inline copy", "t/Owner", "t/copy1", relANarrower},
		{"an inline copy, then a component", "t/copy0", "t/Owner", relBNarrower},
		{"two inline copies", "t/copy0", "t/copy1", relSame},
		{"two components alike, naming one nested type", "t/Home", "t/House", relOverlap},
		{"two components alike, each nesting its own object", "t/Owner", "t/Twin", relUnrelated},
		{"a copy naming a component where the other names a copy", "t/copy0", "t/copy1-addr", relUnrelated},
		{"a copy with one more property", "t/copy0", "t/copy1-more", relUnrelated},
		{"a copy requiring a property the other does not", "t/copy0", "t/copy1-required", relUnrelated},
		{"a copy whose member is narrower", "t/copy0", "t/copy1-uuid", relUnrelated},
		{"a component whose member is broader than a copy's", "t/Owner", "t/copy1-uuid", relUnrelated},
		{"a copy whose member is disjoint", "t/copy0", "t/copy1-int", relUnrelated},
		{"two copies tagged by the same property", "t/copy0-tagged", "t/copy1-tagged", relSame},
		{"a tag naming no property of its model", "t/copy0", "t/copy1-stray-tag", relUnrelated},
		{"two copies with one catch-all and composition", "t/copy0-open", "t/copy1-open", relSame},
		{"two copies whose catch-alls differ", "t/copy0-open", "t/copy1-open-other", relUnrelated},
	} {
		for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			j := typeJudge{Resolve: stubResolve(reg), sites: declarationSites{
				alike: alwaysAlike, written: [2]ir.Provenance{ptrAt(declaredAt(i)), ptrAt(declaredAt(1 - i))}}}
			want := tc.want
			if i == 1 {
				want = want.flipped()
			}
			assert.Equal(t, want, j.relate(ref(order[0]), ref(order[1])), "%s (order %d)", tc.name, i)
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
		alike func(a, b ir.Provenance) bool
		want  relation
	}{
		{alwaysAlike, relSame},
		{func(ir.Provenance, ir.Provenance) bool { return false }, relUnrelated},
	} {
		j := typeJudge{Resolve: stubResolve(reg), sites: declarationSites{
			alike: tc.alike, written: [2]ir.Provenance{ptrAt(declaredAt(0)), ptrAt(declaredAt(1))}}}
		assert.Equal(t, tc.want, j.relate(ref("t/copy0-ref"), ref("t/copy1-ref")))
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
		{"a component, then a copy", []ir.TypeID{"t/Owner", "t/copy1"}, "t/Owner"},
		{"a copy, then a component", []ir.TypeID{"t/copy0", "t/Owner"}, "t/Owner"},
		{"two copies", []ir.TypeID{"t/copy0", "t/copy1"}, "t/copy0"},
		{"two components", []ir.TypeID{"t/Owner", "t/Twin"}, composedAt(declaredAt(1))},
	} {
		reg := alikeRegistry()
		g, recorded := stubMerger(reg)
		decls := declsOf(tc.targets...)
		for i := range decls {
			decls[i].source = unread(t)
		}

		got := g.fold(decls)

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
		reg := map[ir.TypeID]ir.TypeDef{"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}}
		for n := range 2 {
			outer := &ir.Model{ID: ir.TypeID(fmt.Sprintf("t/%d", n)), Anonymous: true,
				Provenance: ptrAt(declaredAt(n))}
			for k := range nested {
				at := declaredAt(n) + jsontext.Pointer(fmt.Sprintf("/properties/f%d", k))
				id := ir.TypeID(fmt.Sprintf("t/%d/%d", n, k))
				reg[id] = &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(at),
					Properties: []ir.Property{{WireName: "x", Type: primRef(ir.PrimString)}}}
				outer.Properties = append(outer.Properties, ir.Property{WireName: fmt.Sprintf("f%d", k), Type: ref(id)})
			}
			reg[outer.ID] = outer
		}
		j := typeJudge{Resolve: stubResolve(reg), sites: declarationSites{
			alike: alwaysAlike, written: [2]ir.Provenance{ptrAt(declaredAt(0)), ptrAt(declaredAt(1))}}}

		want := relSame
		if nested == maxModelPairs {
			want = relUnrelated
		}
		assert.Equal(t, want, j.relate(ref("t/0"), ref("t/1")), "%d nested pairs", nested)
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
			id := ir.TypeID(fmt.Sprintf("t/%d/%d", n, level))
			next := ref(ir.TypeID(fmt.Sprintf("t/%d/%d", n, level+1)))
			m := &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n))}
			for k := range width {
				m.Properties = append(m.Properties, ir.Property{WireName: fmt.Sprintf("f%d", k), Type: next})
			}
			reg[id] = m
		}
		leaf := ir.TypeID(fmt.Sprintf("t/%d/%d", n, depth))
		reg[leaf] = &ir.Primitive{ID: leaf, Prim: ir.PrimString}
	}
	reads := 0
	j := typeJudge{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			reads++
			td, ok := reg[id]
			return td, ok
		},
		sites: declarationSites{written: [2]ir.Provenance{ptrAt(declaredAt(0)), ptrAt(declaredAt(1))}, alike: alwaysAlike},
	}

	rel := j.relate(ref("t/0/0"), ref("t/1/0"))

	require.Equal(t, relUnrelated, rel, "past the budget the two are unrelated")
	assert.LessOrEqual(t, reads, 4*maxModelPairs*width, "the reads grow with the budget, not with the width to the depth")
}
