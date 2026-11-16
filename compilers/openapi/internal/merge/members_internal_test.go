// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
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
	return m, settleFlat(g, idx, reg)
}

// settleFlat settles idx's model against reg, reading each part as its
// flattening stands and every name as shared, as a run reads a part it has
// not settled.
func settleFlat(g *stubbed, idx *PropIndex, reg ir.TypeRegistry) []ir.Diagnostic {
	parts, shared := map[ir.TypeID]Table{}, map[string]bool{}
	for _, target := range ComposedOf(idx.m) {
		parts[target] = Table{}
		for _, member := range ir.FlattenedProperties(reg, target).Properties {
			if d, ok := TakenOutside(member, idx.m.ID); ok {
				parts[target][member.Property.WireName] = d
			}
		}
	}
	for _, td := range reg {
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				shared[p.WireName] = true
			}
		}
	}
	diags, _ := idx.Settle(g.Merger, parts, shared)
	return diags
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

// TestPropIndex_AComposedModelDeclaresItsMemberAnAllOfDeeper pins where a
// model the merge composed declares a member its entries declare apart: an
// allOf deeper than a model a schema names, since the schema written at its
// pointer may settle a member of that name too.
func TestPropIndex_AComposedModelDeclaresItsMemberAnAllOfDeeper(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	id := ids.Document{}.ComposedType("/M")
	m := &ir.Model{ID: id, Provenance: ptrAt("/M")}
	reg[id] = m
	idx := NewPropIndex(m)
	composes("t/A", "0")(idx)
	composes("t/B", "1")(idx)
	g, _ := stubMerger(reg)

	settleFlat(g, idx, reg)

	require.Len(t, m.Properties, 1)
	assert.Equal(t, ids.Document{}.ComposedProp("/M/allOf/allOf/properties/p"), m.Properties[0].ID)
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
// another entry's composes brings nothing, Sub's p standing within A's, but
// one of two models composing each other does, and a declaration two entries
// bring is read once.
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

	reg = membersRegistry()
	for _, via := range []ir.TypeID{"t/viaA1", "t/viaA2"} {
		reg[via] = &ir.Model{ID: via, Base: &ir.TypeRef{Target: "t/A"}}
	}
	m, diags = settled(t, reg, composes("t/viaA1", "0"), composes("t/viaA2", "1"))
	assert.Empty(t, m.Properties, "A's p, which both entries bring, is one declaration")
	assert.Empty(t, diags)
	m, diags = settled(t, membersRegistry(), composes("t/A", "0"), composes("t/A", "1"))
	assert.Empty(t, m.Properties, "A written twice brings its p once")
	assert.Empty(t, diags)

	reg = membersRegistry()
	reg["t/P"] = &ir.Model{ID: "t/P", Base: &ir.TypeRef{Target: "t/Q"}, Properties: []ir.Property{declared("p", "t/prim/string", "/P/p")}}
	reg["t/Q"] = &ir.Model{ID: "t/Q", Base: &ir.TypeRef{Target: "t/P"}, Properties: []ir.Property{declared("p", "t/prim/integer", "/Q/p")}}
	m, diags = settled(t, reg, composes("t/P", "0"), composes("t/Q", "1"))
	require.Len(t, m.Properties, 1, "two models composing each other each bring theirs")
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.Properties[0].Type.Target)
	assert.Len(t, diags, 1)
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

		diags := settleFlat(g, idx, reg)

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

// TestPropIndex_KeepsTheIdentityOfTheDeclarationItHoldsAlone pins the identity
// of a member the model declares beside an entry's declaration: its own
// declaration's, where that is written, unless another model holds that
// declaration too, as a node interned at the allOf branch writing it does, or
// it is written outside the model's schema, as the body a distributed variant
// shares is; then one in the composed namespace. The report about the entry's
// declaration sits at the entry either way, naming the member.
func TestPropIndex_KeepsTheIdentityOfTheDeclarationItHoldsAlone(t *testing.T) {
	t.Parallel()
	const composedAt jsontext.Pointer = "/M/allOf/properties/p"
	composed := ids.Document{}.ComposedProp(composedAt)
	for name, tc := range map[string]struct {
		at, wantAt jsontext.Pointer
		interned   []jsontext.Pointer
		want       ir.PropID
	}{
		"held alone":          {"/M/allOf/1/properties/p", "/M/allOf/1/properties/p", nil, "p/M/allOf/1/properties/p"},
		"its branch interned": {"/M/allOf/1/properties/p", composedAt, []jsontext.Pointer{"/M/allOf/1"}, composed},
		"outside the model":   {"/body/properties/p", composedAt, nil, composed},
	} {
		reg := membersRegistry()
		m := &ir.Model{ID: "t/M", Provenance: ptrAt("/M")}
		reg["t/M"] = m
		idx := NewPropIndex(m)
		composes("t/B", "0")(idx)
		idx.MergeProperty(declared("p", "t/prim/string", string(tc.at)), written(`{}`), "")
		g, _ := stubMerger(reg)
		g.walk.interned = tc.interned

		diags := settleFlat(g, idx, reg)

		require.Len(t, m.Properties, 1, name)
		p := m.Properties[0]
		assert.Equal(t, tc.want, p.ID, name)
		assert.Equal(t, tc.wantAt, p.Provenance.Pointer, "%s: where the identity says", name)
		assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target, "%s: the own declaration, written later", name)
		require.Len(t, diags, 1, name)
		assert.Equal(t, jsontext.Pointer("/M/allOf/0"), diags[0].Provenance.Pointer, name)
		assert.Equal(t, string(p.ID), diags[0].Provenance.Node, name)
	}
}

// TestFold_NamesADeclarationAnotherDocumentHolds pins how a report names a
// declaration an entry brings from another document: its pointer and that
// document's name, since the pointer alone reads as one in the document the
// report is in; a declaration in that document is named by its pointer alone.
func TestFold_NamesADeclarationAnotherDocumentHolds(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(membersRegistry())
	g.Sources = []ir.SourceInfo{{Path: "root.yaml"}, {Path: "other.yaml"}}
	theirs := declared("p", "t/prim/integer", "/B/p")
	theirs.Provenance.Source = 1
	entry := &composedEntry{target: "t/B", at: "/M/allOf/0", order: 1}
	decls := []declaration{
		{prop: theirs, order: 1, via: entry},
		{prop: declared("p", "t/prim/string", "/M/allOf/1/properties/p"), source: written(`{}`), order: 2},
	}

	_, diags := g.foldAt(decls, "/M/allOf/1/properties/p")

	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message,
		"kept what the declaration at /M/allOf/1/properties/p says over the one at /B/p in other.yaml,")
}

// TestFold_AnEntryAlikeInTwoDocumentsKeepsOneWhateverTheOrder pins the entry a
// field keeps of two written alike at one pointer in two documents, as two
// files each holding one schema write /properties/p: the one in the document
// listed first, in either entry order, so no order decides which file the
// field says wrote it.
func TestFold_AnEntryAlikeInTwoDocumentsKeepsOneWhateverTheOrder(t *testing.T) {
	t.Parallel()
	brought := func(source, order int) declaration {
		p := declared("p", "t/prim/string", "/properties/p")
		p.Provenance.Source = source
		at := ir.Provenance{Source: source, Pointer: "/properties/p/x-tag"}
		p.Unmodeled = ir.Unmodeled{"openapi:x-tag": {Value: ir.RawValue(`"same"`), Provenance: at}}
		entry := &composedEntry{target: "t/part", at: "/M/allOf/0", order: order}
		return broughtDeclaration(p, entry)
	}

	for _, sources := range [][2]int{{1, 2}, {2, 1}} {
		g, _ := stubMerger(membersRegistry())
		got, diags := g.foldAt([]declaration{brought(sources[0], 1), brought(sources[1], 2)}, "/M/allOf/properties/p")

		assert.Empty(t, diags, "entries written alike say one thing")
		assert.Equal(t, 1, got.Unmodeled["openapi:x-tag"].Provenance.Source, "entries from %v", sources)
	}
}

// TestOwnership_ReadsEachModelsCompositionOnce pins what sorting one name's
// candidates costs: each model's composition is read once, however many other
// candidates ask whether it composes theirs, so the reads grow with the
// candidates, not their square. Each candidate's model composes a chain as
// deep as the judge's walk reads, and none composes another, so every one is
// left.
func TestOwnership_ReadsEachModelsCompositionOnce(t *testing.T) {
	t.Parallel()
	reads := func(n int) int {
		reg := map[ir.TypeID]ir.TypeDef{"t/link-0": declaring("t/link-0")}
		for i := 1; i <= maxComposedParts; i++ {
			id := ir.TypeID(fmt.Sprintf("t/link-%d", i))
			reg[id] = &ir.Model{ID: id, Base: &ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("t/link-%d", i-1))}}
		}
		cs := make([]candidate, n)
		for i := range cs {
			owner := ir.TypeID(fmt.Sprintf("t/owner-%d", i))
			p := declared("p", "t/prim/string", "/"+string(owner)+"/p")
			last := ir.TypeID(fmt.Sprintf("t/link-%d", maxComposedParts))
			reg[owner] = &ir.Model{ID: owner, Base: &ir.TypeRef{Target: last}, Properties: []ir.Property{p}}
			cs[i] = candidate{decl: ir.PropertyDecl{Owner: owner, Property: &p}, part: i}
		}
		count := 0
		o := newOwnership(registry{resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			count++
			td, ok := reg[id]
			return td, ok
		}}, nil)

		require.Len(t, o.left(cs), n, "no model composes another")
		return count
	}
	a, b := reads(50), reads(100)
	assert.LessOrEqual(t, float64(b), 2.5*float64(a), "reads at 50 and 100 candidates: %d, %d", a, b)
}

// chainTo returns reg with a chain of depth models above base, each composing
// the one below it, and the top one's ID.
func chainTo(reg map[ir.TypeID]ir.TypeDef, base ir.TypeID, depth int) ir.TypeID {
	below := base
	for i := 1; i <= depth; i++ {
		id := ir.TypeID(fmt.Sprintf("t/level-%d", i))
		reg[id] = &ir.Model{ID: id, Base: &ir.TypeRef{Target: below}}
		below = id
	}
	return below
}

// countingRegistry returns a registry reading reg, and the count of its reads.
func countingRegistry(reg map[ir.TypeID]ir.TypeDef) (registry, *int) {
	count := new(int)
	return registry{resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
		*count++
		td, ok := reg[id]
		return td, ok
	}}, count
}

// leafOver returns a registry holding t/Base declaring p and t/Leaf
// redeclaring it, which composes t/Base through a chain of depth models and,
// where direct, as a mixin too, and a candidate of each, Leaf's part first.
func leafOver(depth int, direct bool) (map[ir.TypeID]ir.TypeDef, []candidate) {
	reg := map[ir.TypeID]ir.TypeDef{}
	baseP := declared("p", "t/prim/integer", "/Base/p")
	reg["t/Base"] = &ir.Model{ID: "t/Base", Properties: []ir.Property{baseP}}
	top := chainTo(reg, "t/Base", depth)
	leafP := declared("p", "t/prim/integer", "/Leaf/p")
	leaf := &ir.Model{ID: "t/Leaf", Base: &ir.TypeRef{Target: top}, Properties: []ir.Property{leafP}}
	if direct {
		leaf.Mixins = []ir.TypeRef{{Target: "t/Base"}}
	}
	reg["t/Leaf"] = leaf
	return reg, []candidate{
		{decl: ir.PropertyDecl{Owner: "t/Leaf", Property: &leafP}, part: 0},
		{decl: ir.PropertyDecl{Owner: "t/Base", Property: &baseP}, part: 1},
	}
}

// TestOwnership_LeavesOutADeclarationAnotherComposesAtAnyDepth pins that a
// declaration another candidate's model composes stands within it however deep
// that model's hierarchy nests, past the judge's walk too: a model composing a
// hierarchy and its base again folds the hierarchy's declaration alone.
func TestOwnership_LeavesOutADeclarationAnotherComposesAtAnyDepth(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{1, maxComposedParts, 4 * maxComposedParts} {
		reg, cs := leafOver(depth, false)
		r, _ := countingRegistry(reg)

		left := newOwnership(r, nil).left(cs)

		require.Len(t, left, 1, "at depth %d", depth)
		assert.Equal(t, ir.TypeID("t/Leaf"), left[0].decl.Owner, "at depth %d", depth)
	}
}

// TestOwnership_StopsOnceItReachesEveryOtherModel pins what telling one
// candidate from another costs where one model composes the other directly
// beside a deep hierarchy: the walk stops at the first level, so a hierarchy
// composing a shared base at every level costs its size, not its depth squared.
func TestOwnership_StopsOnceItReachesEveryOtherModel(t *testing.T) {
	t.Parallel()
	reads := func(depth int) int {
		reg, cs := leafOver(depth, true)
		r, count := countingRegistry(reg)

		require.Len(t, newOwnership(r, nil).left(cs), 1, "at depth %d", depth)
		return *count
	}
	assert.Equal(t, reads(10), reads(1000), "reads at depth 10 and 1,000")
}

// TestOwnership_ChargesWhatItReadsAndLeavesInWhatACutWalkMisses pins the
// run's reads as the walk's bound: each type it reads is charged, and a walk
// they cut short tells nothing, so both candidates are left and the reads say
// they ran out, which the settlement reports.
func TestOwnership_ChargesWhatItReadsAndLeavesInWhatACutWalkMisses(t *testing.T) {
	t.Parallel()
	reg, cs := leafOver(100, false)
	r, _ := countingRegistry(reg)
	reads := budget.New(budget.AtMost(50))

	left := newOwnership(r, &reads).left(cs)

	assert.Len(t, left, 2, "a cut walk shows no candidate within another")
	assert.True(t, reads.Exhausted(), "the walk charged its reads")
}

// TestTakenOutside_LeavesOutTheComposingModelsOwn pins the declaration a part's
// flattening stands for: the one it takes, or, where that is the composing
// model's own, as a cycle reaches it, the last other it reads, and none where
// every declaration is the model's.
func TestTakenOutside_LeavesOutTheComposingModelsOwn(t *testing.T) {
	t.Parallel()
	decl := func(owner ir.TypeID) ir.PropertyDecl {
		p := declared("p", "t/prim/string", "/"+string(owner)+"/p")
		return ir.PropertyDecl{Owner: owner, Property: &p}
	}
	for name, tc := range map[string]struct {
		member ir.FlatProperty
		want   ir.TypeID
		ok     bool
	}{
		"another's":        {ir.FlatProperty{PropertyDecl: decl("t/A")}, "t/A", true},
		"the last other":   {ir.FlatProperty{PropertyDecl: decl("t/M"), Overridden: []ir.PropertyDecl{decl("t/A"), decl("t/B"), decl("t/M")}}, "t/B", true},
		"only the model's": {ir.FlatProperty{PropertyDecl: decl("t/M"), Overridden: []ir.PropertyDecl{decl("t/M")}}, "", false},
	} {
		got, ok := TakenOutside(tc.member, "t/M")
		assert.Equal(t, tc.ok, ok, name)
		assert.Equal(t, tc.want, got.Owner, name)
	}
}

// TestPropIndex_ReadsItsOwnDeclarationBackAsItsOwn pins a part on a cycle
// whose Table holds the model's own declaration: it is the model's, so the
// model brings nothing from that part, folds nothing, and keeps the member's
// identity, which a fold would move to the composed namespace, as an interned
// branch writes the declaration.
func TestPropIndex_ReadsItsOwnDeclarationBackAsItsOwn(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	m := &ir.Model{ID: "t/M", Provenance: ptrAt("/M")}
	reg["t/M"] = m
	idx := NewPropIndex(m)
	composes("t/A", "0")(idx)
	idx.MergeProperty(declared("p", "t/prim/integer", "/M/allOf/1/properties/p"), written(`{}`), "")
	g, _ := stubMerger(reg)
	g.walk.interned = []jsontext.Pointer{"/M/allOf/1"}
	parts := map[ir.TypeID]Table{"t/A": {"p": ir.PropertyDecl{Owner: "t/M", Property: &m.Properties[0]}}}

	diags, table := idx.Settle(g.Merger, parts, map[string]bool{"p": true})

	assert.Empty(t, diags)
	require.Len(t, m.Properties, 1)
	assert.Equal(t, ir.PropID("p/M/allOf/1/properties/p"), m.Properties[0].ID)
	assert.Equal(t, ir.TypeID("t/M"), table["p"].Owner)
}
