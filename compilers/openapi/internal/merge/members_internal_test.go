// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
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
	return ir.Property{ID: ir.PropertyID("property" + pointer), Name: ir.NamingFor(wire), WireName: wire,
		Type: ir.TypeRef{Target: target}, Provenance: ptrAt(jsontext.Pointer(pointer))}
}

// membersRegistry holds A and B declaring p apart, Sub composing A with a p of
// its own, and C and D declaring z and y apart.
func membersRegistry() ir.TypeRegistry {
	model := func(id ir.TypeID, base *ir.TypeRef, properties ...ir.Property) *ir.Model {
		return &ir.Model{ID: id, Base: base, Properties: properties}
	}
	return ir.TypeRegistry{
		"type/primitive/string":  &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString},
		"type/primitive/integer": &ir.Primitive{ID: "type/primitive/integer", PrimitiveKind: ir.PrimitiveInteger},
		"type/A":                 model("type/A", nil, declared("p", "type/primitive/string", "/A/p")),
		"type/B":                 model("type/B", nil, declared("p", "type/primitive/integer", "/B/p")),
		"type/Sub":               model("type/Sub", &ir.TypeRef{Target: "type/A"}, declared("p", "type/primitive/string", "/Sub/p")),
		"type/C":                 model("type/C", nil, declared("z", "type/primitive/string", "/C/z"), declared("y", "type/primitive/string", "/C/y")),
		"type/D":                 model("type/D", nil, declared("z", "type/primitive/integer", "/D/z"), declared("y", "type/primitive/integer", "/D/y")),
	}
}

// settled builds model M in reg as compose does, by the steps the walk takes,
// settles it, and returns it with what it reports.
func settled(t *testing.T, reg ir.TypeRegistry, steps ...func(idx *ModelPropertyDeclarations)) (*ir.Model, []ir.Diagnostic) {
	t.Helper()
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	for _, step := range steps {
		step(idx)
	}
	stubbed, _ := stubMerger(reg)
	return model, settleFlat(stubbed, idx, reg)
}

// settleFlat settles idx's model against reg, reading each part as its
// flattening stands and every name as shared, as a run reads a part it has
// not settled.
func settleFlat(stubbedValue *stubbed, idx *ModelPropertyDeclarations, reg ir.TypeRegistry) []ir.Diagnostic {
	parts, shared := map[ir.TypeID]RedeclaredWireNameTable{}, map[string]bool{}
	for _, target := range ComposedOf(idx.m) {
		parts[target] = RedeclaredWireNameTable{}
		for _, member := range ir.FlattenedProperties(reg, target).Properties {
			if propertyDecl, ok := TakenOutside(member, idx.m.ID); ok {
				parts[target][member.Property.WireName] = DeclaredAt(propertyDecl)
			}
		}
	}
	for _, td := range reg {
		if model, ok := td.(*ir.Model); ok {
			for _, property := range model.Properties {
				shared[property.WireName] = true
			}
		}
	}
	diags, _ := idx.Settle(stubbedValue.Merger, parts, shared)
	return diags
}

// composes composes target as written at /M/allOf/<n>, recording it as Mixins.
func composes(target ir.TypeID, n string) func(idx *ModelPropertyDeclarations) {
	return func(idx *ModelPropertyDeclarations) {
		idx.m.Mixins = append(idx.m.Mixins, ir.TypeRef{Target: target})
		idx.Compose(target, jsontext.Pointer("/M/allOf/"+n))
	}
}

// own declares wire as target in the model's own entry n.
func own(wire string, target ir.TypeID, n string) func(idx *ModelPropertyDeclarations) {
	return func(idx *ModelPropertyDeclarations) {
		idx.MergeProperty(declared(wire, target, "/M/allOf/"+n+"/properties/"+wire), written(`{"own":true}`), "")
	}
}

// TestPropertyIndex_SettlesAMemberItsEntriesDeclareApart pins a member two
// entries declare apart, in either entry order: the model declares the later's
// at <model>/allOf/properties/<name> in the composed namespace, reports where
// the one that lost is written, naming the property, and keeps nothing, the
// loser staying declared on its model.
func TestPropertyIndex_SettlesAMemberItsEntriesDeclareApart(t *testing.T) {
	t.Parallel()
	id := ids.DocumentIDNamespace{}.ComposedProperty("/M/allOf/properties/p")
	for _, tc := range []struct {
		first, second ir.TypeID
		want          ir.TypeID
		lost          jsontext.Pointer
	}{
		{"type/A", "type/B", "type/primitive/integer", "/A/p"},
		{"type/B", "type/A", "type/primitive/string", "/B/p"},
	} {
		model, diags := settled(t, membersRegistry(), composes(tc.first, "0"), composes(tc.second, "1"))

		require.Len(t, model.Properties, 1, "%v", tc)
		property := model.Properties[0]
		assert.Equal(t, id, property.ID)
		assert.Equal(t, tc.want, property.Type.Target, "the later entry's")
		assert.Empty(t, property.Unmodeled)
		require.Len(t, diags, 1, "%v", tc)
		assert.Equal(t, diag.ConflictingRedecl, diags[0].Code)
		assert.Equal(t, tc.lost, diags[0].Provenance.Pointer)
		assert.Equal(t, string(id), diags[0].Provenance.Node)
	}
}

// TestPropertyIndex_AComposedModelDeclaresItsMemberAnAllOfDeeper pins where a
// model the merge composed declares a member its entries declare apart: an
// allOf deeper than a model a schema names, since the schema written at its
// pointer may settle a member of that name too.
func TestPropertyIndex_AComposedModelDeclaresItsMemberAnAllOfDeeper(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	id := ids.DocumentIDNamespace{}.ComposedType("/M")
	model := &ir.Model{ID: id, Provenance: ptrAt("/M")}
	reg[id] = model
	idx := NewModelPropertyDeclarations(model)
	composes("type/A", "0")(idx)
	composes("type/B", "1")(idx)
	stubbed, _ := stubMerger(reg)

	settleFlat(stubbed, idx, reg)

	require.Len(t, model.Properties, 1)
	assert.Equal(t, ids.DocumentIDNamespace{}.ComposedProperty("/M/allOf/allOf/properties/p"), model.Properties[0].ID)
}

// TestPropertyIndex_ReadsAnOwnDeclarationWhereTheWalkMetIt pins a model's own
// declaration beside an entry's, read where the walk met each: the later wins
// a tie, and the result replaces the own declaration at the composed identity,
// which, losing, is kept verbatim; a name the model requires and declares only
// through an entry is the member's, leaving Requires.
func TestPropertyIndex_ReadsAnOwnDeclarationWhereTheWalkMetIt(t *testing.T) {
	t.Parallel()
	model, diags := settled(t, membersRegistry(), own("p", "type/primitive/string", "0"), composes("type/B", "1"))
	require.Len(t, model.Properties, 1)
	assert.Equal(t, ir.TypeID("type/primitive/integer"), model.Properties[0].Type.Target)
	assert.Contains(t, model.Properties[0].Unmodeled, losingDeclarationKey+"/M/allOf/0/properties/p", "the own declaration is kept")
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/M/allOf/0/properties/p"), diags[0].Provenance.Pointer, "where the own one is written")

	model, _ = settled(t, membersRegistry(), composes("type/B", "0"), own("p", "type/primitive/string", "1"))
	assert.Equal(t, ir.TypeID("type/primitive/string"), model.Properties[0].Type.Target, "the own declaration, written later")

	reg := membersRegistry()
	model, _ = settled(t, reg, func(idx *ModelPropertyDeclarations) { idx.m.Requires = []string{"p"} }, composes("type/A", "0"), composes("type/B", "1"))
	assert.True(t, model.Properties[0].Required)
	assert.Empty(t, model.Requires)
}

// TestPropertyIndex_PlacesADeclarationAtTheEntryBringingIt pins where a brought
// declaration is read: at the entry naming its owner or a type composing it,
// and at the model, read first, where no entry does, a conflict over it sitting
// where it is written either way; an owner
// another entry's composes brings nothing, Sub's p standing within A's, but
// one of two models composing each other does, and a declaration two entries
// bring is read once.
func TestPropertyIndex_PlacesADeclarationAtTheEntryBringingIt(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	reg["type/viaSub"] = &ir.Model{ID: "type/viaSub", Base: &ir.TypeRef{Target: "type/B"}}
	model, diags := settled(t, reg, composes("type/viaSub", "0"), composes("type/A", "1"))
	assert.Equal(t, ir.TypeID("type/primitive/string"), model.Properties[0].Type.Target)
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/B/p"), diags[0].Provenance.Pointer, "B's, through the entry composing it")

	reg = membersRegistry()
	model, diags = settled(t, reg, func(idx *ModelPropertyDeclarations) { idx.m.Base = &ir.TypeRef{Target: "type/B"} }, composes("type/A", "0"))
	assert.Equal(t, ir.TypeID("type/primitive/string"), model.Properties[0].Type.Target, "B's, read first at the model")
	require.Len(t, diags, 1)
	assert.Equal(t, jsontext.Pointer("/B/p"), diags[0].Provenance.Pointer)

	model, diags = settled(t, membersRegistry(), composes("type/Sub", "0"), composes("type/A", "1"))
	assert.Empty(t, model.Properties, "Sub's p stands within A's, so one declaration decides")
	assert.Empty(t, diags)

	reg = membersRegistry()
	for _, via := range []ir.TypeID{"type/viaA1", "type/viaA2"} {
		reg[via] = &ir.Model{ID: via, Base: &ir.TypeRef{Target: "type/A"}}
	}
	model, diags = settled(t, reg, composes("type/viaA1", "0"), composes("type/viaA2", "1"))
	assert.Empty(t, model.Properties, "A's p, which both entries bring, is one declaration")
	assert.Empty(t, diags)
	model, diags = settled(t, membersRegistry(), composes("type/A", "0"), composes("type/A", "1"))
	assert.Empty(t, model.Properties, "A written twice brings its p once")
	assert.Empty(t, diags)

	reg = membersRegistry()
	reg["type/P"] = &ir.Model{ID: "type/P", Base: &ir.TypeRef{Target: "type/Q"}, Properties: []ir.Property{declared("p", "type/primitive/string", "/P/p")}}
	reg["type/Q"] = &ir.Model{ID: "type/Q", Base: &ir.TypeRef{Target: "type/P"}, Properties: []ir.Property{declared("p", "type/primitive/integer", "/Q/p")}}
	model, diags = settled(t, reg, composes("type/P", "0"), composes("type/Q", "1"))
	require.Len(t, model.Properties, 1, "two models composing each other each bring theirs")
	assert.Equal(t, ir.TypeID("type/primitive/integer"), model.Properties[0].Type.Target)
	assert.Len(t, diags, 1)
}

// TestPropertyIndex_SettlesTheMembersOnlyEntriesDeclareByWireName pins the
// order members only entries declare are appended in: by wire name, not as an
// entry lists them.
func TestPropertyIndex_SettlesTheMembersOnlyEntriesDeclareByWireName(t *testing.T) {
	t.Parallel()
	model, _ := settled(t, membersRegistry(), composes("type/C", "0"), composes("type/D", "1"))
	require.Len(t, model.Properties, 2)
	assert.Equal(t, []string{"y", "z"}, []string{model.Properties[0].WireName, model.Properties[1].WireName})
}

// TestPropertyIndex_APromotedRedactionBringsItsMarkerInEitherOrder pins an
// entry whose declaration a promotion made secret beside one declaring nothing:
// the field is secret and carries the promotion's marker whichever is written
// later.
func TestPropertyIndex_APromotedRedactionBringsItsMarkerInEitherOrder(t *testing.T) {
	t.Parallel()
	const promoted ir.Inferred = "extension-promotion"
	for _, order := range [][2]ir.TypeID{{"type/A", "type/B"}, {"type/B", "type/A"}} {
		reg := membersRegistry()
		a := reg["type/A"].(*ir.Model)
		a.Properties[0].Type.Target, a.Properties[0].Secret = "type/primitive/integer", true
		a.Properties[0].Provenance.Inferred = promoted
		model, _ := settled(t, reg, composes(order[0], "0"), composes(order[1], "1"))
		require.Len(t, model.Properties, 1)
		assert.True(t, model.Properties[0].Secret, "%v", order)
		assert.Equal(t, promoted, model.Properties[0].Provenance.Inferred, "%v", order)
	}
}

// TestTypeJudge_AUnionsVariantIsJudgedAtTheUnionsSite pins the sites a
// variant is read at: its union's declaration's, whichever side the union is
// on, so a variant and a node hoisted alike at the other declaration are one
// type, and swapping the two flips the verdict and changes nothing else.
func TestTypeJudge_AUnionsVariantIsJudgedAtTheUnionsSite(t *testing.T) {
	t.Parallel()
	enum := func(id ir.TypeID, at jsontext.Pointer) *ir.Enum {
		return &ir.Enum{ID: id, Anonymous: true, Provenance: ptrAt(at), ValueType: ir.PrimitiveString,
			Members: []ir.EnumMember{{Value: ir.Value{Kind: ir.ValueString, String: "x"}}}}
	}
	reg := ir.TypeRegistry{
		"type/primitive/integer": &ir.Primitive{ID: "type/primitive/integer", PrimitiveKind: ir.PrimitiveInteger},
		"type/u/0":               enum("type/u/0", "/d0/anyOf/0"),
		"type/u":                 &ir.Union{ID: "type/u", Anonymous: true, Provenance: ptrAt("/d0"), Variants: variantsOf("type/u/0", "type/primitive/integer")},
		"type/e":                 enum("type/e", "/d1"),
	}
	stubbed, _ := stubMerger(reg)
	union, copied := ir.TypeRef{Target: "type/u"}, ir.TypeRef{Target: "type/e"}

	assert.Equal(t, secondTypeIsNarrower, stubbed.judgeAt(sitesAt("/d0"), sitesAt("/d1")).relateFrom(union, copied, 0))
	assert.Equal(t, firstTypeIsNarrower, stubbed.judgeAt(sitesAt("/d1"), sitesAt("/d0")).relateFrom(copied, union, 0))
}

// TestPropertyIndex_ComposesModelsAsTheirEntriesBringThem pins a member entries
// declare as two models: the field composes them in the order the entries are
// written, recording where each part was brought, so the composed model's own
// members report at those entries and read the later as the specialization.
func TestPropertyIndex_ComposesModelsAsTheirEntriesBringThem(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]ir.TypeID{{"type/E", "type/F"}, {"type/F", "type/E"}} {
		reg := membersRegistry()
		for _, owner := range []string{"E", "F"} {
			part := ir.TypeID("type/" + owner + "part")
			reg[part] = &ir.Model{ID: part, Properties: []ir.Property{declared(owner, "type/primitive/string", "/"+owner+"part/"+owner)}}
			reg[ir.TypeID("type/"+owner)] = &ir.Model{ID: ir.TypeID("type/" + owner), Properties: []ir.Property{declared("p", part, "/"+owner+"/p")}}
		}
		model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
		reg["type/M"] = model
		idx := NewModelPropertyDeclarations(model)
		composes(order[0], "0")(idx)
		composes(order[1], "1")(idx)
		stubbed, _ := stubMerger(reg)

		diags := settleFlat(stubbed, idx, reg)

		require.Empty(t, diags, "%v", order)
		minted, ok := reg[model.Properties[0].Type.Target].(*ir.Model)
		require.True(t, ok, "%v", order)
		assert.Equal(t, []ir.TypeRef{{Target: order[0] + "part"}, {Target: order[1] + "part"}}, minted.Mixins, "%v", order)
		assert.Equal(t, []jsontext.Pointer{"/M/allOf/0", "/M/allOf/1"}, stubbed.walk.brought[minted.ID], "%v", order)
	}
}

// TestFold_ABroughtDeclarationPastTheBoundStaysOnItsModel pins a declaration an
// entry brings past the declarations one fold judges: reported at the entry
// bringing it, and kept nowhere, as it stays declared on its model.
func TestFold_ABroughtDeclarationPastTheBoundStaysOnItsModel(t *testing.T) {
	t.Parallel()
	decls := make([]declaration, 0, maxFoldDeclarations+1)
	for range maxFoldDeclarations {
		decls = append(decls, nthDecl(0, "type/primitive/string"))
	}
	past := declared("data", "type/primitive/string", "/B/data")
	decls = append(decls, declaration{property: past, via: &composedEntry{target: "type/B", at: "/M/allOf/9"}})
	stubbed, _ := stubMerger(membersRegistry())

	got, diags := stubbed.foldAt(decls, "/M/allOf/properties/data", nil)

	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/M/allOf/9"), diags[0].Provenance.Pointer)
	assert.NotContains(t, got.Unmodeled, losingDeclarationKey+"/B/data")
}

// TestPropertyIndex_KeepsTheIdentityOfTheDeclarationItHoldsAlone pins the
// identity of a member the model declares beside an entry's declaration: its
// own declaration's, where that is written, unless another model holds that
// declaration too, as a node interned at the allOf entry writing it does, or it
// is written outside the model's schema, as the body a distributed variant
// shares is; then one in the composed namespace. The conflict over the entry's
// declaration sits where that is written either way, naming the member.
func TestPropertyIndex_KeepsTheIdentityOfTheDeclarationItHoldsAlone(t *testing.T) {
	t.Parallel()
	const composedAt jsontext.Pointer = "/M/allOf/properties/p"
	composed := ids.DocumentIDNamespace{}.ComposedProperty(composedAt)
	for name, tc := range map[string]struct {
		at, wantAt jsontext.Pointer
		interned   []jsontext.Pointer
		want       ir.PropertyID
	}{
		"held alone":         {"/M/allOf/1/properties/p", "/M/allOf/1/properties/p", nil, "property/M/allOf/1/properties/p"},
		"its entry interned": {"/M/allOf/1/properties/p", composedAt, []jsontext.Pointer{"/M/allOf/1"}, composed},
		"outside the model":  {"/body/properties/p", composedAt, nil, composed},
	} {
		reg := membersRegistry()
		model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
		reg["type/M"] = model
		idx := NewModelPropertyDeclarations(model)
		composes("type/B", "0")(idx)
		idx.MergeProperty(declared("p", "type/primitive/string", string(tc.at)), written(`{}`), "")
		stubbed, _ := stubMerger(reg)
		stubbed.walk.interned = tc.interned

		diags := settleFlat(stubbed, idx, reg)

		require.Len(t, model.Properties, 1, name)
		property := model.Properties[0]
		assert.Equal(t, tc.want, property.ID, name)
		assert.Equal(t, tc.wantAt, property.Provenance.Pointer, "%s: where the identity says", name)
		assert.Equal(t, ir.TypeID("type/primitive/string"), property.Type.Target, "%s: the own declaration, written later", name)
		require.Len(t, diags, 1, name)
		assert.Equal(t, jsontext.Pointer("/B/p"), diags[0].Provenance.Pointer, name)
		assert.Equal(t, string(property.ID), diags[0].Provenance.Node, name)
	}
}

// TestFold_NamesADeclarationAnotherDocumentHolds pins a conflict over a
// declaration an entry brings from another document: it sits where that
// declaration is written, in its document, by the input that wrote it, and so
// names it by its pointer alone and the kept one, in the model's document, by
// its pointer and that document's name, since the pointer alone reads as one
// in the document the report is in.
func TestFold_NamesADeclarationAnotherDocumentHolds(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(membersRegistry())
	stubbed.Sources = []ir.SourceInfo{{Path: "root.yaml"}, {Path: "other.yaml"}}
	theirs := declared("p", "type/primitive/integer", "/B/p")
	theirs.Provenance.Source, theirs.Provenance.WrittenBy = 1, 2
	entry := &composedEntry{target: "type/B", at: "/M/allOf/0", order: 1}
	decls := []declaration{
		{property: theirs, order: 1, via: entry},
		{property: declared("p", "type/primitive/string", "/M/allOf/1/properties/p"), source: written(`{}`), order: 2},
	}

	_, diags := stubbed.foldAt(decls, "/M/allOf/1/properties/p", nil)

	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message,
		"kept what the declaration at /M/allOf/1/properties/p in root.yaml says over the one at /B/p,")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/B/p", WrittenBy: 2}, diags[0].Provenance)
}

// TestFold_NamesEachPlaceAgainstTheDocumentTheReportIsIn pins how a conflict
// names the two declarations: against the document it stands in, the losing
// declaration's, so that one is named by its pointer alone and the one kept
// carries its document's name where it is in another, wherever the composing
// model is. The kept place the report lists is the one its message names.
func TestFold_NamesEachPlaceAgainstTheDocumentTheReportIsIn(t *testing.T) {
	t.Parallel()
	at := func(target ir.TypeID, pointer string, source int) declaration {
		property := declared("p", target, pointer)
		property.Provenance.Source = source
		return declaration{property: property, via: &composedEntry{target: "type/E", at: "/M/allOf/0", order: 1}}
	}
	for name, tc := range map[string]struct {
		sources     []string
		lost, kept  declaration
		message     string
		lostIn      int
		keptIn      int
		keptNamesIn bool
	}{
		"the loser in another document": {
			sources: []string{"root.yaml", "other.yaml"},
			lost:    at("type/primitive/integer", "/B/p", 1), kept: at("type/primitive/string", "/A/p", 0),
			message: "kept what the declaration at /A/p in root.yaml says over the one at /B/p,", lostIn: 1, keptIn: 0, keptNamesIn: true,
		},
		"the winner in another document": {
			sources: []string{"root.yaml", "other.yaml"},
			lost:    at("type/primitive/integer", "/A/p", 0), kept: at("type/primitive/string", "/B/p", 1),
			message: "kept what the declaration at /B/p in other.yaml says over the one at /A/p,", lostIn: 0, keptIn: 1, keptNamesIn: true,
		},
		"both in the model's document": {
			sources: []string{"root.yaml"},
			lost:    at("type/primitive/integer", "/A/p", 0), kept: at("type/primitive/string", "/B/p", 0),
			message: "kept what the declaration at /B/p says over the one at /A/p,", lostIn: 0, keptIn: 0,
		},
		"each in a document of its own": {
			sources: []string{"root.yaml", "a.yaml", "b.yaml"},
			lost:    at("type/primitive/integer", "/A/p", 1), kept: at("type/primitive/string", "/B/p", 2),
			message: "kept what the declaration at /B/p in b.yaml says over the one at /A/p,", lostIn: 1, keptIn: 2, keptNamesIn: true,
		},
	} {
		stubbed, _ := stubMerger(membersRegistry())
		for _, path := range tc.sources {
			stubbed.Sources = append(stubbed.Sources, ir.SourceInfo{Path: path})
		}
		lost, kept := tc.lost, tc.kept
		lost.order, kept.order = 1, 2

		_, diags := stubbed.foldAt([]declaration{lost, kept}, "/M/allOf/properties/p", nil)

		require.Len(t, diags, 1, name)
		assert.Contains(t, diags[0].Message, tc.message, name)
		assert.Equal(t, tc.lostIn, diags[0].Provenance.Source, "%s: the report stands in the losing declaration's document", name)
		require.Len(t, diags[0].Related, 1, name)
		listed := diags[0].Related[0].Provenance
		assert.Equal(t, ir.Provenance{Source: tc.keptIn, Pointer: kept.property.Provenance.Pointer}, listed, name)
		assert.Equal(t, tc.keptNamesIn, listed.Source != diags[0].Provenance.Source, "%s: the message names a document where the entry lists another", name)
		assert.Equal(t, tc.keptNamesIn, strings.Contains(diags[0].Message, string(listed.Pointer)+" in "+tc.sources[listed.Source]), name)
	}
}

// TestTypeSites_NamesWhereAFoldedTypesNodesAreWritten pins where a settled
// member says the nodes of its type are written: where the one declaration
// naming its node says, as that declaration's own list; nowhere for a
// primitive several name, or a node the registry lacks; for a hoisted node
// several name, where the first holding it says, else the first. For a node
// the fold built: where it stands and where each node it holds is written,
// through a node built beneath it or one holding itself, however many places
// a declaration brings, but for a node a $ref reached and what that holds.
func TestTypeSites_NamesWhereAFoldedTypesNodesAreWritten(t *testing.T) {
	t.Parallel()
	hoisted := func(id ir.TypeID, at jsontext.Pointer) ir.TypeCommon {
		return ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(at)}
	}
	reg := membersRegistry()
	reg["type/enum"] = &ir.Enum{TypeCommon: hoisted("type/enum", "/part/allOf/1/p/items")}
	reg["type/reached"] = &ir.List{TypeCommon: hoisted("type/reached", "/elsewhere/q"), ElementType: ir.TypeRef{Target: "type/within"}}
	reg["type/within"] = &ir.Enum{TypeCommon: hoisted("type/within", "/part/allOf/0/p/items")}
	held, lacking := ir.TypeRef{Target: "type/enum"}, ir.TypeRef{Target: "type/unregistered"}
	reg["type/built"] = &ir.Tuple{TypeCommon: hoisted("type/built", "/M/p"),
		ElementTypes: []ir.TypeRef{held, {Target: "type/primitive/string"}, {Target: "type/reached"}, lacking, held}}
	reg["type/built-list"] = &ir.List{TypeCommon: hoisted("type/built-list", "/M/p"), ElementType: held}
	reg["type/built-map"] = &ir.MapT{TypeCommon: hoisted("type/built-map", "/M/p"), Value: held}
	reg["type/built-rest"] = &ir.Tuple{TypeCommon: hoisted("type/built-rest", "/M/p"), Rest: &held}
	reg["type/built-model"] = &ir.Model{TypeCommon: hoisted("type/built-model", "/M/p"), Mixins: []ir.TypeRef{held}}
	reg["type/built-items"] = &ir.List{TypeCommon: hoisted("type/built-items", "/M/p/items"), ElementType: held}
	reg["type/built-nested"] = &ir.List{TypeCommon: hoisted("type/built-nested", "/M/p"), ElementType: ir.TypeRef{Target: "type/built-items"}}
	reg["type/built-cycle"] = &ir.Tuple{TypeCommon: hoisted("type/built-cycle", "/M/p"), ElementTypes: []ir.TypeRef{{Target: "type/built-cycle"}, held}}
	stubbed, _ := stubMerger(reg)
	part := &composedEntry{target: "type/part", at: "/M/allOf/0"}
	partSites := append([]ir.Provenance{ptrAt("/part/allOf/0/p")}, sitesAt("/part/allOf/1/p")...)
	for i := range maxTypeSites {
		partSites = append(partSites, ptrAt(jsontext.Pointer(fmt.Sprintf("/part/deeper/%d", i))))
	}
	brought := declaration{property: declared("p", "type/primitive/string", "/part/allOf/0/p"), via: part, typeAt: partSites}
	own := declaration{property: declared("p", "type/primitive/integer", "/M/p")}
	twin := declaration{property: declared("p", "type/primitive/string", "/M/q")}
	field := func(target ir.TypeID) ir.Property { return declared("p", target, "/M/p") }

	kept := stubbed.typeSites(field("type/primitive/string"), []declaration{own, brought})
	assert.Equal(t, partSites, kept)
	assert.Same(t, &partSites[0], &kept[0], "one declaration's list is the member's, uncopied")
	assert.Nil(t, stubbed.typeSites(field("type/primitive/string"), []declaration{brought, own, twin}), "no declaration writes a primitive")
	assert.Nil(t, stubbed.typeSites(field("type/unregistered"), []declaration{own}))

	named := func(pointer string) declaration { return declaration{property: declared("p", "type/enum", pointer)} }
	assert.Equal(t, sitesAt("/part/allOf/1/p"),
		stubbed.typeSites(field("type/enum"), []declaration{named("/M/a"), own, named("/part/allOf/1/p")}), "the one whose place holds the node")
	assert.Equal(t, sitesAt("/M/a"),
		stubbed.typeSites(field("type/enum"), []declaration{named("/M/a"), named("/M/b")}), "the first, where a $ref brought it to each")
	for _, built := range []ir.TypeID{"type/built", "type/built-list", "type/built-map", "type/built-rest", "type/built-model",
		"type/built-nested", "type/built-cycle"} {
		assert.Equal(t, []ir.Provenance{ptrAt("/M/p"), ptrAt("/part/allOf/1/p")},
			stubbed.typeSites(field(built), []declaration{own, brought}), "%s: where it stands, then where what it holds is written", built)
	}
}

// TestBuiltSites_ReadsNoMoreNodesThanItsBound pins the nodes a settled member
// reads of the type its fold built: each once, and maxTypeSites of them at
// most. Of a tuple holding that many hoisted nodes, each written at its own
// declaration and the first held twice, every place is recorded; of one
// holding one more, the last is left out.
func TestBuiltSites_ReadsNoMoreNodesThanItsBound(t *testing.T) {
	t.Parallel()
	for _, held := range []int{maxTypeSites, maxTypeSites + 1} {
		reg := membersRegistry()
		built := &ir.Tuple{ID: "type/built", Anonymous: true, Provenance: ptrAt("/M/p")}
		decls := make([]declaration, held)
		for i := range decls {
			at := jsontext.Pointer(fmt.Sprintf("/part%d/p", i))
			id := ir.TypeID(fmt.Sprintf("type/held-%d", i))
			reg[id] = &ir.Enum{ID: id, Anonymous: true, Provenance: ptrAt(at)}
			built.ElementTypes = append(built.ElementTypes, ir.TypeRef{Target: id})
			decls[i] = declaration{property: declared("p", id, string(at))}
		}
		built.ElementTypes = append([]ir.TypeRef{built.ElementTypes[0]}, built.ElementTypes...)
		reg["type/built"] = built
		stubbed, _ := stubMerger(reg)

		got := stubbed.typeSites(declared("p", "type/built", "/M/p"), decls)

		require.Len(t, got, 1+maxTypeSites, "%d held", held)
		assert.Equal(t, ptrAt(jsontext.Pointer(fmt.Sprintf("/part%d/p", maxTypeSites-1))), got[maxTypeSites], "%d held", held)
	}
}

// TestFold_NamesABroughtDeclarationWhereItSaysWhatItDoes pins how a report
// names a declaration an entry brings. One a part settled names its first
// declaration whichever gave its type, so the report names the one place its
// type is written; where its type is written at several places or none, the
// member's own place; and where the place it would name is the member's own
// and its model's composition settled it there, at either depth, the entry
// bringing it, since no schema is written there.
func TestFold_NamesABroughtDeclarationWhereItSaysWhatItDoes(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	reg["type/part"] = &ir.Model{ID: "type/part", Provenance: ptrAt("/part")}
	stubbed, _ := stubMerger(reg)
	entry := &composedEntry{target: "type/part", at: "/M/allOf/0"}
	built := []ir.Provenance{ptrAt("/part/p"), ptrAt("/part/allOf/0/p"), ptrAt("/part/allOf/1/p")}
	brought := func(owner ir.TypeID, pointer string, typeAt []ir.Provenance) declaration {
		property := declared("p", "type/primitive/string", pointer)
		return broughtDeclaration(Declared{Owner: owner, Property: &property, TypeAt: typeAt}, entry)
	}
	foldingValue := &folding{g: &stubbed.Merger, decls: []declaration{
		brought("type/part", "/part/allOf/0/p", sitesAt("/part/allOf/1/p")),
		brought("type/part", "/part/allOf/0/p", built),
		brought("type/part", "/part/allOf/properties/p", built),
		brought("type/part", "/part/allOf/allOf/properties/p", built),
		brought("type/part", "/part/allOf/properties/p", sitesAt("/part/allOf/1/p")),
		brought("type/part", "/part/allOf/properties/p", sitesAt("/part/allOf/properties/p")),
		brought("type/part", "/part/allOf/0/p", nil),
		brought("type/primitive/string", "/part/allOf/properties/p", built),
		{property: declared("p", "type/primitive/string", "/M/allOf/properties/p")},
	}}

	for i, want := range []string{"/part/allOf/1/p", "/part/allOf/0/p", "/M/allOf/0", "/M/allOf/0", "/part/allOf/1/p",
		"/M/allOf/0", "/part/allOf/0/p", "/part/allOf/properties/p", "/M/allOf/properties/p"} {
		assert.Equal(t, want, foldingValue.at(i), "declaration %d", i)
	}
}

// TestHoistedAt_ReadsEachPlaceADeclarationHoldsItsType pins a hoisted node as
// written at a declaration where it sits at or under any place the
// declaration holds its type, in that place's document, and a named node, or
// one written elsewhere, as declared apart.
func TestHoistedAt_ReadsEachPlaceADeclarationHoldsItsType(t *testing.T) {
	t.Parallel()
	node := func(anonymous bool, source int, pointer jsontext.Pointer) ir.TypeDef {
		return &ir.Enum{Anonymous: anonymous, Provenance: ir.Provenance{Source: source, Pointer: pointer}}
	}
	sites := []ir.Provenance{ptrAt("/A/allOf/0/p"), ptrAt("/A/allOf/1/p")}

	assert.True(t, hoistedAt(node(true, 0, "/A/allOf/1/p"), sites), "at a later place")
	assert.True(t, hoistedAt(node(true, 0, "/A/allOf/1/p/items"), sites), "under one")
	assert.False(t, hoistedAt(node(true, 0, "/A/allOf/1/pq"), sites), "beside one")
	assert.False(t, hoistedAt(node(true, 1, "/A/allOf/1/p"), sites), "in another document")
	assert.False(t, hoistedAt(node(false, 0, "/A/allOf/1/p"), sites), "a named node")
	assert.False(t, hoistedAt(node(true, 0, "/A/allOf/1/p"), nil), "a declaration holding it nowhere")
}

// TestFold_AnEntryAlikeInTwoDocumentsKeepsOneWhateverTheOrder pins the entry a
// field keeps of two written alike at one pointer in two documents, as two
// files each holding one schema write /properties/p: the one in the document
// listed first, in either entry order, so no order decides which file the
// field says wrote it.
func TestFold_AnEntryAlikeInTwoDocumentsKeepsOneWhateverTheOrder(t *testing.T) {
	t.Parallel()
	brought := func(source, order int) declaration {
		property := declared("p", "type/primitive/string", "/properties/p")
		property.Provenance.Source = source
		at := ir.Provenance{Source: source, Pointer: "/properties/p/x-tag"}
		property.Unmodeled = ir.Unmodeled{"openapi:x-tag": {Value: ir.RawValue(`"same"`), Provenance: at}}
		entry := &composedEntry{target: "type/part", at: "/M/allOf/0", order: order}
		return broughtDeclaration(DeclaredAt(ir.PropertyDecl{Owner: "type/part", Property: &property}), entry)
	}

	for _, sources := range [][2]int{{1, 2}, {2, 1}} {
		stubbed, _ := stubMerger(membersRegistry())
		got, diags := stubbed.foldAt([]declaration{brought(sources[0], 1), brought(sources[1], 2)}, "/M/allOf/properties/p", nil)

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
		reg := map[ir.TypeID]ir.TypeDef{"type/link-0": declaring("type/link-0")}
		for i := 1; i <= maxComposedParts; i++ {
			id := ir.TypeID(fmt.Sprintf("type/link-%d", i))
			reg[id] = &ir.Model{ID: id, Base: &ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("type/link-%d", i-1))}}
		}
		cs := make([]candidate, n)
		for i := range cs {
			owner := ir.TypeID(fmt.Sprintf("type/owner-%d", i))
			property := declared("p", "type/primitive/string", "/"+string(owner)+"/p")
			last := ir.TypeID(fmt.Sprintf("type/link-%d", maxComposedParts))
			reg[owner] = &ir.Model{ID: owner, Base: &ir.TypeRef{Target: last}, Properties: []ir.Property{property}}
			cs[i] = candidate{decl: DeclaredAt(ir.PropertyDecl{Owner: owner, Property: &property}), part: i}
		}
		count := 0
		ownership := newOwnership(registry{resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			count++
			td, ok := reg[id]
			return td, ok
		}}, nil)

		require.Len(t, ownership.left(cs), n, "no model composes another")
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
		id := ir.TypeID(fmt.Sprintf("type/level-%d", i))
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
	baseP := declared("p", "type/primitive/integer", "/Base/p")
	reg["type/Base"] = &ir.Model{ID: "type/Base", Properties: []ir.Property{baseP}}
	top := chainTo(reg, "type/Base", depth)
	leafP := declared("p", "type/primitive/integer", "/Leaf/p")
	leaf := &ir.Model{ID: "type/Leaf", Base: &ir.TypeRef{Target: top}, Properties: []ir.Property{leafP}}
	if direct {
		leaf.Mixins = []ir.TypeRef{{Target: "type/Base"}}
	}
	reg["type/Leaf"] = leaf
	return reg, []candidate{
		{decl: DeclaredAt(ir.PropertyDecl{Owner: "type/Leaf", Property: &leafP}), part: 0},
		{decl: DeclaredAt(ir.PropertyDecl{Owner: "type/Base", Property: &baseP}), part: 1},
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
		registry, _ := countingRegistry(reg)

		left := newOwnership(registry, nil).left(cs)

		require.Len(t, left, 1, "at depth %d", depth)
		assert.Equal(t, ir.TypeID("type/Leaf"), left[0].decl.Owner, "at depth %d", depth)
	}
}

// TestOwnership_ReadsPastAPartNamingNoType pins a composition one of whose
// parts names no type, as a reference left dangling does: the walk reads past
// it to the rest, so the base it reaches beside it is still found.
func TestOwnership_ReadsPastAPartNamingNoType(t *testing.T) {
	t.Parallel()
	reg, cs := leafOver(3, false)
	leaf, ok := reg["type/Leaf"].(*ir.Model)
	require.True(t, ok)
	leaf.Mixins = []ir.TypeRef{*leaf.Base}
	leaf.Base = &ir.TypeRef{Target: "type/missing"}
	registry, _ := countingRegistry(reg)

	left := newOwnership(registry, nil).left(cs)

	require.Len(t, left, 1)
	assert.Equal(t, ir.TypeID("type/Leaf"), left[0].decl.Owner)
}

// TestOwnership_AWalkReturningToItsModelReadsOnToTheOthers pins a walk whose
// model's composition leads back to the model: it is none of the others the
// walk reads on for, so the walk passes it and reaches a candidate's model
// read after it, whose declaration then stands within the walk's own.
func TestOwnership_AWalkReturningToItsModelReadsOnToTheOthers(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{}
	declaring := func(id ir.TypeID, parts ...ir.TypeID) candidate {
		model := &ir.Model{ID: id, Properties: []ir.Property{declared("p", "type/primitive/string", "/"+string(id)+"/p")}}
		for _, part := range parts {
			model.Mixins = append(model.Mixins, ir.TypeRef{Target: part})
		}
		reg[id] = model
		return candidate{decl: DeclaredAt(ir.PropertyDecl{Owner: id, Property: &model.Properties[0]})}
	}
	top := declaring("type/top", "type/loop")
	reg["type/loop"] = &ir.Model{ID: "type/loop", Mixins: []ir.TypeRef{{Target: "type/top"}, {Target: "type/near"}, {Target: "type/mid"}}}
	reg["type/mid"] = &ir.Model{ID: "type/mid", Base: &ir.TypeRef{Target: "type/far"}}
	near, far := declaring("type/near"), declaring("type/far")
	near.part, far.part = 1, 2

	left := newOwnership(registry{resolve: stubResolve(reg)}, nil).left([]candidate{top, near, far})

	require.Len(t, left, 1, "each other declaration stands within the first's composition")
	assert.Equal(t, ir.TypeID("type/top"), left[0].decl.Owner)
}

// TestOwnership_StopsOnceItReachesEveryOtherModel pins what telling one
// candidate from another costs where one model composes the other directly
// beside a deep hierarchy: the walk stops at the first level, so a hierarchy
// composing a shared base at every level costs its size, not its depth squared.
func TestOwnership_StopsOnceItReachesEveryOtherModel(t *testing.T) {
	t.Parallel()
	reads := func(depth int) int {
		reg, cs := leafOver(depth, true)
		registry, count := countingRegistry(reg)

		require.Len(t, newOwnership(registry, nil).left(cs), 1, "at depth %d", depth)
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
	registry, _ := countingRegistry(reg)
	reads := budget.New(budget.AtMost(50))

	left := newOwnership(registry, &reads).left(cs)

	assert.Len(t, left, 2, "a cut walk shows no candidate within another")
	assert.True(t, reads.Exhausted(), "the walk charged its reads")
}

// TestPropertyIndex_ADiscriminatorNamingAnAppendedMemberNamesItsID pins the
// locator a discriminator takes once the settlement appends the member it names
// by name: the member's ID alone, as for a member the model declares, while one
// naming another member keeps its name.
func TestPropertyIndex_ADiscriminatorNamingAnAppendedMemberNamesItsID(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]ir.Discriminator{
		"kind":  {Property: "property/composed/M/allOf/properties/kind"},
		"other": {PropertyName: "other"},
	} {
		model := &ir.Model{ID: "type/M", Discriminator: &ir.Discriminator{PropertyName: name}}
		member := declared("kind", "type/primitive/string", "/M/allOf/properties/kind")
		member.ID = "property/composed/M/allOf/properties/kind"

		NewModelPropertyDeclarations(model).place(-1, member)

		assert.Equal(t, want, *model.Discriminator, name)
	}
}

// TestTakenOutside_LeavesOutTheComposingModelsOwn pins the declaration a part's
// flattening stands for: the one it takes, or, where that is the composing
// model's own, as a cycle reaches it, the last other it reads, and none where
// every declaration is the model's.
func TestTakenOutside_LeavesOutTheComposingModelsOwn(t *testing.T) {
	t.Parallel()
	decl := func(owner ir.TypeID) ir.PropertyDecl {
		property := declared("p", "type/primitive/string", "/"+string(owner)+"/p")
		return ir.PropertyDecl{Owner: owner, Property: &property}
	}
	for name, tc := range map[string]struct {
		member ir.FlatProperty
		want   ir.TypeID
		ok     bool
	}{
		"another's":        {ir.FlatProperty{PropertyDecl: decl("type/A")}, "type/A", true},
		"the last other":   {ir.FlatProperty{PropertyDecl: decl("type/M"), Overridden: []ir.PropertyDecl{decl("type/A"), decl("type/B"), decl("type/M")}}, "type/B", true},
		"only the model's": {ir.FlatProperty{PropertyDecl: decl("type/M"), Overridden: []ir.PropertyDecl{decl("type/M")}}, "", false},
	} {
		got, ok := TakenOutside(tc.member, "type/M")
		assert.Equal(t, tc.ok, ok, name)
		assert.Equal(t, tc.want, got.Owner, name)
	}
}

// TestPropertyIndex_ReadsItsOwnDeclarationBackAsItsOwn pins a part on a cycle
// whose RedeclaredWireNameTable holds the model's own declaration: it is the
// model's, so the model brings nothing from that part, folds nothing, and keeps
// the member's identity, which a fold would move to the composed namespace, as
// an interned entry writes the declaration.
func TestPropertyIndex_ReadsItsOwnDeclarationBackAsItsOwn(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	composes("type/A", "0")(idx)
	idx.MergeProperty(declared("p", "type/primitive/integer", "/M/allOf/1/properties/p"), written(`{}`), "")
	stubbed, _ := stubMerger(reg)
	stubbed.walk.interned = []jsontext.Pointer{"/M/allOf/1"}
	parts := map[ir.TypeID]RedeclaredWireNameTable{"type/A": {"p": DeclaredAt(ir.PropertyDecl{Owner: "type/M", Property: &model.Properties[0]})}}

	diags, table := idx.Settle(stubbed.Merger, parts, map[string]bool{"p": true})

	assert.Empty(t, diags)
	require.Len(t, model.Properties, 1)
	assert.Equal(t, ir.PropertyID("property/M/allOf/1/properties/p"), model.Properties[0].ID)
	assert.Equal(t, ir.TypeID("type/M"), table["p"].Owner)
}

// TestPropertyIndex_DiscardReportsDroppedTargets pins that Merger.Discarded
// records every declaration target that the settled property did not adopt and
// never the winning target.
func TestPropertyIndex_DiscardReportsDroppedTargets(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	own("q", "type/primitive/string", "0")(idx)
	own("q", "type/primitive/integer", "1")(idx)
	stubbed, _ := stubMerger(reg)
	dropped := map[ir.TypeID]bool{}
	stubbed.Discarded = dropped

	settleFlat(stubbed, idx, reg)

	assert.Equal(t, map[ir.TypeID]bool{"type/primitive/string": true}, dropped)
}
