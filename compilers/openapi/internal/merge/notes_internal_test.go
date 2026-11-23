// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// promoted marks a declaration whose deprecation a promotion read, as the
// promotion policy spells it.
const promoted ir.Inferred = "extension-promotion"

// noteCase is one note a declaration's property holds: how to write it on a
// property from a value's name, and how to read it off a property.
type noteCase struct {
	detail string
	write  func(property *ir.Property, value string)
	read   func(property ir.Property) string
}

// noteCases are the notes lastWritten ranks a declaration the model writes
// itself first for.
var noteCases = []noteCase{
	{"title", func(p *ir.Property, v string) { p.Docs.Summary = v }, func(p ir.Property) string { return p.Docs.Summary }},
	{"description", func(p *ir.Property, v string) { p.Docs.Description = v },
		func(p ir.Property) string { return p.Docs.Description }},
	{"external docs", func(p *ir.Property, v string) { p.Docs.ExternalDocs = []ir.Link{{URL: v}} },
		func(p ir.Property) string { return p.Docs.ExternalDocs[0].URL }},
	{"deprecation", func(p *ir.Property, v string) { p.Deprecation = &ir.Deprecation{Message: v} },
		func(p ir.Property) string { return p.Deprecation.Message }},
	{"examples", func(p *ir.Property, v string) {
		p.Examples = []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, String: v}}}
	}, func(p ir.Property) string { return p.Examples[0].Value.String }},
}

// TestFold_ANoteTheModelWritesIsKeptOverOneItBrings pins lastWritten's
// ranking: of a declaration the model writes itself and one another model
// holds giving a note differently, the field keeps the model's own in either
// order, and the other, which stays declared on its model, is reported at the
// entry bringing it and kept nowhere verbatim; a deprecation brings the mark of
// what promoted it. A default and XML hints keep the later declaration's, as
// the flattening's override does.
func TestFold_ANoteTheModelWritesIsKeptOverOneItBrings(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		own := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/a")},
			source: written(`{"own":true}`)}
		brought := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/part/p")},
			via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
		note.write(&own.property, "own")
		note.write(&brought.property, "brought")
		own.property.Provenance.Inferred = promoted
		for _, reversed := range []bool{false, true} {
			decls := []declaration{own, brought}
			if reversed {
				slices.Reverse(decls)
			}
			stubbed, recorded := stubMerger(narrowingRegistry())

			got := stubbed.fold(decls)

			assert.Equal(t, "own", note.read(got), "%s, reversed %t: the model's own is the field's", note.detail, reversed)
			if note.detail == "deprecation" {
				assert.Equal(t, promoted, got.Provenance.Inferred, "reversed %t: with the mark of what promoted it", reversed)
			}
			assert.Empty(t, got.Unmodeled, "%s, reversed %t: nothing is kept verbatim", note.detail, reversed)
			require.Len(t, *recorded, 1, "%s, reversed %t", note.detail, reversed)
			assert.Equal(t, jsontext.Pointer("/M/allOf/1"), (*recorded)[0].Provenance.Pointer, "%s, reversed %t", note.detail, reversed)
			assert.Contains(t, (*recorded)[0].Message, "give its "+note.detail+" differently; kept the one the declaration at /a "+
				"gives over the one at /part/p, which stays declared on its model", "%s, reversed %t", note.detail, reversed)
		}
	}

	t.Run("a default and xml hints", func(t *testing.T) {
		t.Parallel()
		one, two := number("1"), number("2")
		own := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Default: &one,
			XML: &ir.XMLHints{Name: "own"}, Provenance: ptrAt("/a")}, source: written(`{"own":true}`)}
		brought := declaration{property: ir.Property{WireName: "p", Type: primitiveRef(ir.PrimitiveInteger), Default: &two,
			XML: &ir.XMLHints{Name: "brought"}, Provenance: ptrAt("/part/p")},
			via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
		stubbed, _ := stubMerger(narrowingRegistry())

		got := stubbed.fold([]declaration{own, brought})

		assert.Equal(t, &two, got.Default, "the later default is the field's")
		assert.Equal(t, "brought", got.XML.Name, "and so are the later xml hints")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+"/a", "the model's own is kept verbatim")
	})
}

// TestSettle_ReadsANodeHoldingItsNotesWhereTheModelDeclaresTheMemberAgain pins
// Merger.noted: a member the model declares twice itself, whose nodes hold
// their notes, beside one another model brings, whose property carries its
// node's, is judged with each note on its node where the types do not compose
// as they stand: two maps apart, which the bare object's note composes with,
// and not two bare objects, which compose without it, nor a member the model
// declares once. Either way the brought node itself holds no note afterwards,
// and each kind of note tells it apart alike.
func TestSettle_ReadsANodeHoldingItsNotesWhereTheModelDeclaresTheMemberAgain(t *testing.T) {
	t.Parallel()
	mapOf := func(value ir.TypeID) func(common ir.TypeCommon) ir.TypeDef {
		return func(common ir.TypeCommon) ir.TypeDef {
			return &ir.MapT{TypeCommon: common, Value: ir.TypeRef{Target: value}}
		}
	}
	bare := func(common ir.TypeCommon) ir.TypeDef { return &ir.Model{TypeCommon: common} }
	for detail, notes := range map[string]CarriedNotes{
		"docs":        {Docs: ir.Docs{Description: "brought"}},
		"deprecation": {Deprecation: &ir.Deprecation{Message: "brought"}},
		"examples":    {Examples: []ir.Example{{Summary: "brought"}}},
	} {
		for _, tc := range []struct {
			name       string
			own0, own2 func(common ir.TypeCommon) ir.TypeDef // own2 nil where the model declares the member once
			conjuncts  []ir.TypeID
		}{
			{"two maps apart", mapOf("type/primitive/string"), mapOf("type/A"), []ir.TypeID{"type/own0", "type/brought", "type/own2"}},
			{"two bare objects", bare, bare, []ir.TypeID{"type/own0", "type/own2"}},
			{"one bare object", bare, nil, []ir.TypeID{}},
		} {
			reg := membersRegistry()
			node := func(id ir.TypeID, at jsontext.Pointer, docs ir.Docs) ir.TypeCommon {
				return ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(at), Docs: docs}
			}
			reg["type/brought"] = bare(node("type/brought", "/A/p", ir.Docs{}))
			reg["type/own0"] = tc.own0(node("type/own0", "/M/allOf/0/properties/p", ir.Docs{Description: "own 0"}))
			brought := declared("p", "type/brought", "/A/p")
			brought.Docs, brought.Deprecation, brought.Examples = notes.Docs, notes.Deprecation, notes.Examples
			reg["type/A"] = &ir.Model{ID: "type/A", Properties: []ir.Property{brought}}
			model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
			reg["type/M"] = model
			idx := NewModelPropertyDeclarations(model)
			own("p", "type/own0", "0")(idx)
			composes("type/A", "1")(idx)
			if tc.own2 != nil {
				reg["type/own2"] = tc.own2(node("type/own2", "/M/allOf/2/properties/p", ir.Docs{Description: "own 2"}))
				own("p", "type/own2", "2")(idx)
			}
			stubbed, _ := stubMerger(reg)
			stubbed.Carried = map[ir.TypeID]CarriedNotes{"type/brought": notes}

			diags := settleFlat(stubbed, idx, reg)

			label := detail + ", " + tc.name
			assert.Empty(t, diags, label)
			require.Len(t, model.Properties, 1, label)
			composed, isModel := reg[model.Properties[0].Type.Target].(*ir.Model)
			require.True(t, isModel, label)
			assert.Equal(t, tc.conjuncts, targetsOf(composed.Mixins), label)
			left := reg["type/brought"].Common()
			assert.Equal(t, CarriedNotes{}, CarriedNotes{Docs: left.Docs, Deprecation: left.Deprecation, Examples: left.Examples},
				"%s: the node is not written to", label)
		}
	}
}

// TestSettle_JudgesAMemberTheModelAloneDeclaresAsItsNodesStand pins the other
// side: a member only the model declares, more than once, is judged as its
// nodes stand, whatever Carried says of one, as no property carries a note off
// a node of such a member. A bare object says nothing a map does not, so the
// map is the field's type.
func TestSettle_JudgesAMemberTheModelAloneDeclaresAsItsNodesStand(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	reg["type/own0"] = &ir.Model{ID: "type/own0", Anonymous: true, Provenance: ptrAt("/M/allOf/0/properties/p")}
	reg["type/own1"] = &ir.MapT{ID: "type/own1", Anonymous: true, Provenance: ptrAt("/M/allOf/1/properties/p"),
		Value: ir.TypeRef{Target: "type/primitive/string"}}
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	own("p", "type/own0", "0")(idx)
	own("p", "type/own1", "1")(idx)
	stubbed, _ := stubMerger(reg)
	stubbed.Carried = map[ir.TypeID]CarriedNotes{"type/own0": {Docs: ir.Docs{Description: "carried"}}}

	diags := settleFlat(stubbed, idx, reg)

	assert.Empty(t, diags)
	require.Len(t, model.Properties, 1)
	assert.Equal(t, ir.TypeID("type/own1"), model.Properties[0].Type.Target)
}

// TestWithNotes_CopiesTheDeprecation pins that a node read holding its notes
// shares no deprecation with the property carrying it, as a replacement is
// written into each once the document is whole.
func TestWithNotes_CopiesTheDeprecation(t *testing.T) {
	t.Parallel()
	carried := &ir.Deprecation{Message: "gone"}
	node := &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/item"}}

	got := withNotes(node, CarriedNotes{Deprecation: carried})

	assert.Equal(t, &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/item"}, Deprecation: carried}, got)
	assert.NotSame(t, carried, got.Common().Deprecation)
	assert.Nil(t, node.Deprecation, "the node is not written to")
}

// TestDeclaredAgain_ListsTheMembersTheModelDeclaresMoreThanOnce pins that the
// members DeclaredAgain lists are those the model declares more than once
// itself, by the IDs they settle with, and none another model brings a second
// declaration of.
func TestDeclaredAgain_ListsTheMembersTheModelDeclaresMoreThanOnce(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	composes("type/A", "0")(idx)
	own("p", "type/primitive/string", "1")(idx)
	own("q", "type/primitive/string", "1")(idx)
	own("q", "type/primitive/string", "2")(idx)
	own("r", "type/primitive/string", "2")(idx)
	stubbed, _ := stubMerger(reg)
	stubbed.walk.interned = []jsontext.Pointer{"/M/allOf/1"}

	settleFlat(stubbed, idx, reg)

	assert.Equal(t, []ir.PropertyID{mintedProperty("/M/allOf/2/properties/q")}, idx.DeclaredAgain())
}
