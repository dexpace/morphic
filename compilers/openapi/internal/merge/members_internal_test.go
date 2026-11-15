// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// declared is wire's declaration of target, written at pointer.
func declared(wire string, target ir.TypeID, pointer string) ir.Property {
	return ir.Property{ID: ir.PropID("p" + pointer), Name: ir.NamingFor(wire), WireName: wire,
		Type: ir.TypeRef{Target: target}, Provenance: ptrAt(jsontext.Pointer(pointer))}
}

// membersRegistry holds A and B declaring p apart, Sub composing A with a p of
// its own, and C and D declaring z and y apart.
func membersRegistry() ir.TypeRegistry {
	model := func(id ir.TypeID, base *ir.TypeRef, props ...ir.Property) *ir.Model {
		return &ir.Model{ID: id, Base: base, Properties: props}
	}
	return ir.TypeRegistry{
		"t/prim/string":  &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString},
		"t/prim/integer": &ir.Primitive{ID: "t/prim/integer", Prim: ir.PrimInteger},
		"t/A":            model("t/A", nil, declared("p", "t/prim/string", "/A/p")),
		"t/B":            model("t/B", nil, declared("p", "t/prim/integer", "/B/p")),
		"t/Sub":          model("t/Sub", &ir.TypeRef{Target: "t/A"}, declared("p", "t/prim/string", "/Sub/p")),
		"t/C":            model("t/C", nil, declared("z", "t/prim/string", "/C/z"), declared("y", "t/prim/string", "/C/y")),
		"t/D":            model("t/D", nil, declared("z", "t/prim/integer", "/D/z"), declared("y", "t/prim/integer", "/D/y")),
	}
}

// settled builds model M in reg as compose does, by the steps the walk takes,
// settles it, and returns it with what it reports.
func settled(t *testing.T, reg ir.TypeRegistry, steps ...func(idx *PropIndex)) (*ir.Model, []ir.Diagnostic) {
	t.Helper()
	m := &ir.Model{ID: "t/M", Provenance: ptrAt("/M")}
	reg["t/M"] = m
	idx := NewPropIndex(m)
	for _, step := range steps {
		step(idx)
	}
	g, _ := stubMerger(reg)
	return m, idx.Settle(g.Merger)
}

// composes composes target as written at /M/allOf/<n>, recording it as Mixins.
func composes(target ir.TypeID, n string) func(idx *PropIndex) {
	return func(idx *PropIndex) {
		idx.m.Mixins = append(idx.m.Mixins, ir.TypeRef{Target: target})
		idx.Compose(target, jsontext.Pointer("/M/allOf/"+n))
	}
}

// own declares wire as target in the model's own entry n.
func own(wire string, target ir.TypeID, n string) func(idx *PropIndex) {
	return func(idx *PropIndex) {
		idx.MergeProperty(declared(wire, target, "/M/allOf/"+n+"/properties/"+wire), written(`{"own":true}`), "")
	}
}

// TestPropIndex_SettlesAMemberItsEntriesDeclareApart pins a member two entries
// declare apart, in either entry order: the model declares the later's at
// <model>/allOf/properties/<name> in the composed namespace, reports at the
// entry bringing the one that lost, naming the property, and keeps nothing,
// the loser staying declared on its model.
func TestPropIndex_SettlesAMemberItsEntriesDeclareApart(t *testing.T) {
	t.Parallel()
	id := ids.Document{}.ComposedProp("/M/allOf/properties/p")
	for _, tc := range []struct {
		first, second ir.TypeID
		want          ir.TypeID
	}{
		{"t/A", "t/B", "t/prim/integer"},
		{"t/B", "t/A", "t/prim/string"},
	} {
		m, diags := settled(t, membersRegistry(), composes(tc.first, "0"), composes(tc.second, "1"))

		require.Len(t, m.Properties, 1, "%v", tc)
		p := m.Properties[0]
		assert.Equal(t, id, p.ID)
		assert.Equal(t, tc.want, p.Type.Target, "the later entry's")
		assert.Empty(t, p.Unmodeled)
		require.Len(t, diags, 1, "%v", tc)
		assert.Equal(t, diag.ConflictingRedecl, diags[0].Code)
		assert.Equal(t, jsontext.Pointer("/M/allOf/0"), diags[0].Provenance.Pointer)
		assert.Equal(t, string(id), diags[0].Provenance.Node)
	}
}

// TestPropIndex_ReadsAnOwnDeclarationWhereTheWalkMetIt pins a model's own
// declaration beside an entry's, read where the walk met each: the later wins
// a tie, and the result replaces the own declaration at the composed identity,
// which, losing, is kept verbatim; a name the model requires and declares only
// through an entry is the member's, leaving Requires.
func TestPropIndex_ReadsAnOwnDeclarationWhereTheWalkMetIt(t *testing.T) {
	t.Parallel()
	m, diags := settled(t, membersRegistry(), own("p", "t/prim/string", "0"), composes("t/B", "1"))
	require.Len(t, m.Properties, 1)
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.Properties[0].Type.Target)
	assert.Contains(t, m.Properties[0].Unmodeled, losingDeclarationKey+"/M/allOf/0/properties/p", "the own declaration is kept")
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/M/allOf/0/properties/p"), diags[0].Provenance.Pointer, "where the own one is written")

	m, _ = settled(t, membersRegistry(), composes("t/B", "0"), own("p", "t/prim/string", "1"))
	assert.Equal(t, ir.TypeID("t/prim/string"), m.Properties[0].Type.Target, "the own declaration, written later")

	reg := membersRegistry()
	m, _ = settled(t, reg, func(idx *PropIndex) { idx.m.Requires = []string{"p"} }, composes("t/A", "0"), composes("t/B", "1"))
	assert.True(t, m.Properties[0].Required)
	assert.Empty(t, m.Requires)
}

// TestPropIndex_PlacesADeclarationAtTheEntryBringingIt pins where a brought
// declaration is read and reported: at the entry naming its owner or a type
// composing it, and at the model, read first, where no entry does; an owner
// another entry's composes brings nothing, Sub's p standing within A's.
func TestPropIndex_PlacesADeclarationAtTheEntryBringingIt(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	reg["t/viaSub"] = &ir.Model{ID: "t/viaSub", Base: &ir.TypeRef{Target: "t/B"}}
	m, diags := settled(t, reg, composes("t/viaSub", "0"), composes("t/A", "1"))
	assert.Equal(t, ir.TypeID("t/prim/string"), m.Properties[0].Type.Target)
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/M/allOf/0"), diags[0].Provenance.Pointer, "B's, through the entry composing it")

	reg = membersRegistry()
	m, diags = settled(t, reg, func(idx *PropIndex) { idx.m.Base = &ir.TypeRef{Target: "t/B"} }, composes("t/A", "0"))
	assert.Equal(t, ir.TypeID("t/prim/string"), m.Properties[0].Type.Target, "B's, read first at the model")
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/M"), diags[0].Provenance.Pointer)

	m, diags = settled(t, membersRegistry(), composes("t/Sub", "0"), composes("t/A", "1"))
	assert.Empty(t, m.Properties, "Sub's p stands within A's, so one declaration decides")
	assert.Empty(t, diags)
}

// TestPropIndex_SettlesTheMembersOnlyEntriesDeclareByWireName pins the order
// members only entries declare are appended in: by wire name, not as an entry
// lists them.
func TestPropIndex_SettlesTheMembersOnlyEntriesDeclareByWireName(t *testing.T) {
	t.Parallel()
	m, _ := settled(t, membersRegistry(), composes("t/C", "0"), composes("t/D", "1"))
	require.Len(t, m.Properties, 2)
	assert.Equal(t, []string{"y", "z"}, []string{m.Properties[0].WireName, m.Properties[1].WireName})
}

// TestPropIndex_APromotedRedactionBringsItsMarkerInEitherOrder pins an entry
// whose declaration a promotion made secret beside one declaring nothing: the
// field is secret and carries the promotion's marker whichever is written
// later.
func TestPropIndex_APromotedRedactionBringsItsMarkerInEitherOrder(t *testing.T) {
	t.Parallel()
	const promoted ir.Inferred = "extension-promotion"
	for _, order := range [][2]ir.TypeID{{"t/A", "t/B"}, {"t/B", "t/A"}} {
		reg := membersRegistry()
		a := reg["t/A"].(*ir.Model)
		a.Properties[0].Type.Target, a.Properties[0].Secret = "t/prim/integer", true
		a.Properties[0].Provenance.Inferred = promoted
		m, _ := settled(t, reg, composes(order[0], "0"), composes(order[1], "1"))
		require.Len(t, m.Properties, 1)
		assert.True(t, m.Properties[0].Secret, "%v", order)
		assert.Equal(t, promoted, m.Properties[0].Provenance.Inferred, "%v", order)
	}
}

// TestTypeJudge_AUnionsVariantIsJudgedAtTheUnionsSite pins the sites a
// variant is read at: its union's declaration's, whichever side the union is
// on, so a variant and a node hoisted alike at the other declaration are one
// type, and swapping the two flips the verdict and changes nothing else.
func TestTypeJudge_AUnionsVariantIsJudgedAtTheUnionsSite(t *testing.T) {
	t.Parallel()
	enum := func(id ir.TypeID, at jsontext.Pointer) *ir.Enum {
		return &ir.Enum{ID: id, Anonymous: true, Provenance: ptrAt(at), ValueType: ir.PrimString,
			Members: []ir.EnumMember{{Value: ir.Value{Kind: ir.ValueString, Str: "x"}}}}
	}
	reg := ir.TypeRegistry{
		"t/prim/integer": &ir.Primitive{ID: "t/prim/integer", Prim: ir.PrimInteger},
		"t/u/0":          enum("t/u/0", "/d0/anyOf/0"),
		"t/u":            &ir.Union{ID: "t/u", Anonymous: true, Provenance: ptrAt("/d0"), Variants: variantsOf("t/u/0", "t/prim/integer")},
		"t/e":            enum("t/e", "/d1"),
	}
	g, _ := stubMerger(reg)
	union, copied := ir.TypeRef{Target: "t/u"}, ir.TypeRef{Target: "t/e"}

	assert.Equal(t, relBNarrower, g.judgeAt(ptrAt("/d0"), ptrAt("/d1")).relateFrom(union, copied, 0))
	assert.Equal(t, relANarrower, g.judgeAt(ptrAt("/d1"), ptrAt("/d0")).relateFrom(copied, union, 0))
}

// TestPropIndex_ComposesModelsAsTheirEntriesBringThem pins a member entries
// declare as two models: the field composes them in the order the entries are
// written, recording where each part was brought, so the composed model's own
// members report at those entries and read the later as the specialization.
func TestPropIndex_ComposesModelsAsTheirEntriesBringThem(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]ir.TypeID{{"t/E", "t/F"}, {"t/F", "t/E"}} {
		reg := membersRegistry()
		for _, owner := range []string{"E", "F"} {
			part := ir.TypeID("t/" + owner + "part")
			reg[part] = &ir.Model{ID: part, Properties: []ir.Property{declared(owner, "t/prim/string", "/"+owner+"part/"+owner)}}
			reg[ir.TypeID("t/"+owner)] = &ir.Model{ID: ir.TypeID("t/" + owner), Properties: []ir.Property{declared("p", part, "/"+owner+"/p")}}
		}
		m := &ir.Model{ID: "t/M", Provenance: ptrAt("/M")}
		reg["t/M"] = m
		idx := NewPropIndex(m)
		composes(order[0], "0")(idx)
		composes(order[1], "1")(idx)
		g, _ := stubMerger(reg)

		diags := idx.Settle(g.Merger)

		require.Empty(t, diags, "%v", order)
		minted, ok := reg[m.Properties[0].Type.Target].(*ir.Model)
		require.True(t, ok, "%v", order)
		assert.Equal(t, []ir.TypeRef{{Target: order[0] + "part"}, {Target: order[1] + "part"}}, minted.Mixins, "%v", order)
		assert.Equal(t, []jsontext.Pointer{"/M/allOf/0", "/M/allOf/1"}, g.walk.brought[minted.ID], "%v", order)
	}
}

// TestFold_ABroughtDeclarationPastTheBoundStaysOnItsModel pins a declaration an
// entry brings past the declarations one fold judges: reported at the entry
// bringing it, and kept nowhere, as it stays declared on its model.
func TestFold_ABroughtDeclarationPastTheBoundStaysOnItsModel(t *testing.T) {
	t.Parallel()
	decls := make([]declaration, 0, maxFoldDeclarations+1)
	for range maxFoldDeclarations {
		decls = append(decls, nthDecl(0, "t/prim/string"))
	}
	past := declared("data", "t/prim/string", "/B/data")
	decls = append(decls, declaration{prop: past, via: &composedEntry{target: "t/B", at: "/M/allOf/9"}})
	g, _ := stubMerger(membersRegistry())

	got, diags := g.foldAt(decls, "/M/allOf/properties/data")

	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/M/allOf/9"), diags[0].Provenance.Pointer)
	assert.NotContains(t, got.Unmodeled, losingDeclarationKey+"/B/data")
}
