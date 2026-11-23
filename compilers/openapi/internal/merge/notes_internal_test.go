// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
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
// node's, is judged with each note on its node where the types as they stand
// drop one: two maps apart, which the bare object's note composes with, and not
// two bare objects, which compose without it, nor a member the model declares
// once. Either way the brought node itself holds no note afterwards, each kind
// of note tells it apart alike, and docs the model's own nodes hold are kept
// over those brought, which is reported.
func TestSettle_ReadsANodeHoldingItsNotesWhereTheModelDeclaresTheMemberAgain(t *testing.T) {
	t.Parallel()
	mapOf := func(value ir.TypeID) func(common ir.TypeCommon) ir.TypeDef {
		return func(common ir.TypeCommon) ir.TypeDef {
			return &ir.MapT{TypeCommon: common, Value: ir.TypeRef{Target: value}}
		}
	}
	bare := func(common ir.TypeCommon) ir.TypeDef { return &ir.Model{TypeCommon: common} }
	for detail, notes := range map[string]CarriedNotes{
		"description": {Docs: ir.Docs{Description: "brought"}},
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
			reports := make([]string, len(diags))
			for i, diagnostic := range diags {
				reports[i] = diagnostic.Message
			}
			if detail == "description" {
				require.Len(t, reports, 1, label)
				assert.Contains(t, reports[0], "give its description differently; kept the one the declaration at /M/allOf/", label)
				assert.Contains(t, reports[0], "over the one at /A/p, which stays declared on its model", label)
			} else {
				assert.Empty(t, reports, label)
			}
			require.Len(t, model.Properties, 1, label)
			assert.Empty(t, model.Properties[0].Docs, "%s: the field holds no docs, its types holding the model's own", label)
			composed, isModel := reg[model.Properties[0].Type.Target].(*ir.Model)
			require.True(t, isModel, label)
			assert.Equal(t, tc.conjuncts, targetsOf(composed.Mixins), label)
			left := reg["type/brought"].Common()
			assert.Equal(t, CarriedNotes{}, CarriedNotes{Docs: left.Docs, Deprecation: left.Deprecation, Examples: left.Examples},
				"%s: the node is not written to", label)
		}
	}
}

// TestSettle_ReadsNotesWhereTheTypesAsTheyStandDropOne pins which judgment a
// member takes that the model declares once beside two other models': where
// the types as they stand drop one, whoever declares it, the one reading each
// node holding the notes its property carries, which keeps a map that composes
// with a noted bare object, drops the model's own between two lists a note
// tells apart, or keeps it, the later of two. Where they drop none, two lists
// alike but for a note are one type, and nothing is reported.
func TestSettle_ReadsNotesWhereTheTypesAsTheyStandDropOne(t *testing.T) {
	t.Parallel()
	common := func(id ir.TypeID, at jsontext.Pointer) ir.TypeCommon {
		return ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(at)}
	}
	mapOf := func(value ir.TypeID) func(common ir.TypeCommon) ir.TypeDef {
		return func(common ir.TypeCommon) ir.TypeDef {
			return &ir.MapT{TypeCommon: common, Value: ir.TypeRef{Target: value}}
		}
	}
	bare := func(common ir.TypeCommon) ir.TypeDef { return &ir.Model{TypeCommon: common} }
	list := func(common ir.TypeCommon) ir.TypeDef {
		return &ir.List{TypeCommon: common, ElementType: ir.TypeRef{Target: "type/primitive/string"}}
	}
	for _, tc := range []struct {
		name               string
		own, first, second func(common ir.TypeCommon) ir.TypeDef
		ownLast            bool        // a tie goes to the later, the own one where read apart
		conjuncts          []ir.TypeID // what the field composes
		keeps              ir.TypeID   // or the type it keeps, composing none
		reports            int
		verbatim           bool
	}{
		{name: "the model's own, which composes with the rest", own: mapOf("type/B"), first: bare, second: mapOf("type/primitive/string"),
			conjuncts: []ir.TypeID{"type/own", "type/first", "type/second"}},
		{name: "the model's own, of no type with either", own: mapOf("type/B"), first: list, second: list,
			keeps: "type/second", reports: 2, verbatim: true},
		{name: "a brought one alone", own: list, first: list, second: mapOf("type/B"), ownLast: true, keeps: "type/own", reports: 2},
		{name: "none", own: list, first: list, second: list, ownLast: true, keeps: "type/first"},
	} {
		reg := membersRegistry()
		reg["type/own"] = tc.own(common("type/own", "/M/allOf/0/properties/p"))
		reg["type/first"] = tc.first(common("type/first", "/A/p"))
		reg["type/second"] = tc.second(common("type/second", "/C/p"))
		reg["type/A"] = &ir.Model{ID: "type/A", Properties: []ir.Property{declared("p", "type/first", "/A/p")}}
		reg["type/C"] = &ir.Model{ID: "type/C", Properties: []ir.Property{declared("p", "type/second", "/C/p")}}
		model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
		reg["type/M"] = model
		idx := NewModelPropertyDeclarations(model)
		steps := []func(idx *ModelPropertyDeclarations){own("p", "type/own", "0"), composes("type/A", "1"), composes("type/C", "2")}
		if tc.ownLast {
			slices.Reverse(steps)
		}
		for _, step := range steps {
			step(idx)
		}
		stubbed, _ := stubMerger(reg)
		stubbed.Carried = map[ir.TypeID]CarriedNotes{
			"type/first": {Docs: ir.Docs{Description: "first"}}, "type/own": {Docs: ir.Docs{Description: "own"}},
		}

		diags := settleFlat(stubbed, idx, reg)

		assert.Len(t, diags, tc.reports, tc.name)
		require.Len(t, model.Properties, 1, tc.name)
		property := model.Properties[0]
		assert.Equal(t, tc.verbatim, property.Unmodeled != nil, "%s: the model's own kept verbatim", tc.name)
		if composed, isModel := reg[property.Type.Target].(*ir.Model); isModel {
			assert.Equal(t, tc.conjuncts, targetsOf(composed.Mixins), tc.name)
			continue
		}
		assert.Nil(t, tc.conjuncts, tc.name)
		assert.Equal(t, tc.keeps, property.Type.Target, tc.name)
	}
}

// stringEnum is an enum of values, strings, with common.
func stringEnum(common ir.TypeCommon, values ...string) *ir.Enum {
	members := make([]ir.EnumMember, len(values))
	for i, value := range values {
		members[i] = ir.EnumMember{ID: ir.EnumMemberID(string(common.ID) + "/" + value), Value: ir.Value{Kind: ir.ValueString, String: value}}
	}
	return &ir.Enum{TypeCommon: common, ValueType: ir.PrimitiveString, Closed: true, Members: members}
}

// TestSettle_ATypeANoteAloneTellsApartIsNoNodeOfItsOwn pins meetNode where two
// types a carried note tells apart meet, a third being dropped: the field names
// the narrower one's own node, in every order of the three, which the report of
// the third names as kept, and mints none. Where each says
// what the other lacks, the node built of them holds no carried note, and the
// example the own node holds, whichever of them it is built from.
func TestSettle_ATypeANoteAloneTellsApartIsNoNodeOfItsOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		own         []string
		wantEnum    []string
		wantOwnNode bool
	}{
		{"the own one within the brought", []string{"a", "b"}, []string{"a", "b"}, true},
		{"the two meeting", []string{"a", "b", "d"}, []string{"a", "b"}, false},
	} {
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			reg := membersRegistry()
			examples := []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, String: "a"}}}
			reg["type/own"] = stringEnum(ir.TypeCommon{ID: "type/own", Anonymous: true, Provenance: ptrAt("/M/allOf/0/properties/p"), Examples: examples}, tc.own...)
			reg["type/first"] = stringEnum(ir.TypeCommon{ID: "type/first", Anonymous: true, Provenance: ptrAt("/A/p")}, "a", "b", "c")
			brought := declared("p", "type/first", "/A/p")
			brought.Docs.Description, brought.Deprecation, brought.Examples = "carried", &ir.Deprecation{Message: "carried"}, examples
			reg["type/A"] = &ir.Model{ID: "type/A", Properties: []ir.Property{brought}}
			model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
			reg["type/M"] = model
			idx := NewModelPropertyDeclarations(model)
			steps := []func(idx *ModelPropertyDeclarations){own("p", "type/own", "0"), composes("type/A", "1"), composes("type/B", "2")}
			for _, k := range order {
				steps[k](idx)
			}
			stubbed, _ := stubMerger(reg)
			stubbed.Carried = map[ir.TypeID]CarriedNotes{"type/first": {Docs: brought.Docs, Deprecation: brought.Deprecation, Examples: examples}}

			diags := settleFlat(stubbed, idx, reg)

			require.Len(t, diags, 1, "%s, %v", tc.name, order)
			if tc.wantOwnNode {
				assert.Contains(t, diags[0].Message, "kept what the declaration at /M/allOf/0/properties/p says over the one at /B/p", "%s, %v", tc.name, order)
			}
			require.Len(t, model.Properties, 1, "%s, %v", tc.name, order)
			property := model.Properties[0]
			enum, isEnum := reg[property.Type.Target].(*ir.Enum)
			require.True(t, isEnum, "%s, %v", tc.name, order)
			values := make([]string, len(enum.Members))
			for i, member := range enum.Members {
				values[i] = member.Value.String
			}
			assert.Equal(t, tc.wantEnum, values, "%s, %v", tc.name, order)
			assert.Equal(t, tc.wantOwnNode, property.Type.Target == "type/own", "%s, %v: the own node", tc.name, order)
			assert.Equal(t, !tc.wantOwnNode, stubbed.walk.minted == 1, "%s, %v: one minted", tc.name, order)
			assert.Empty(t, enum.Docs, "%s, %v", tc.name, order)
			assert.Nil(t, enum.Deprecation, "%s, %v", tc.name, order)
			assert.Equal(t, "carried", property.Docs.Description, "%s, %v: the field holds the note", tc.name, order)
			assert.Equal(t, examples, enum.Examples, "%s, %v: the own node's example stays on the type", tc.name, order)
			assert.Empty(t, property.Examples, "%s, %v: so the field holds none", tc.name, order)
		}
	}
}

// TestSayingAll_NamesTheFirstSortingNodeThatSaysAll pins which node stands for
// the one several meet in: of those saying what it does, sites and member IDs
// apart, the one sorting first in either order, and none where none does.
func TestSayingAll_NamesTheFirstSortingNodeThatSaysAll(t *testing.T) {
	t.Parallel()
	built := stringEnum(ir.TypeCommon{ID: "type/built", Provenance: ptrAt("/built")}, "a")
	same := func(id ir.TypeID) ir.TypeDef {
		return stringEnum(ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(jsontext.Pointer("/" + id))}, "a")
	}
	wider := stringEnum(ir.TypeCommon{ID: "type/0"}, "a", "b")
	noted := stringEnum(ir.TypeCommon{ID: "type/1", Docs: ir.Docs{Description: "noted"}}, "a")
	for _, reversed := range []bool{false, true} {
		nodes := []ir.TypeDef{wider, noted, same("type/3"), same("type/2")}
		if reversed {
			slices.Reverse(nodes)
		}
		all := make([]member, len(nodes))
		for k, td := range nodes {
			all[k] = member{ref: ir.TypeRef{Target: td.Common().ID}}
		}

		target, said := sayingAll(built, all, nodes)

		assert.True(t, said, "reversed %t", reversed)
		assert.Equal(t, ir.TypeID("type/2"), target, "reversed %t", reversed)
	}
	_, said := sayingAll(built, []member{{}, {}}, []ir.TypeDef{wider, noted})
	assert.False(t, said, "neither says what it does")
}

// TestFold_ANoteOnAnOwnNodeIsKeptOverOneBrought pins givenOnNodes: a note the
// node the model's own declaration built holds is kept over one another model's
// property brings, reported. Where the field's type is the own node or one
// built holding its note, read there, the field holds neither; where it is one
// the registry does not hold, the field holds the own one, but an example,
// which no node built adopts, and which the field takes as it would were there
// no own one. A named node's note is not its declaration's own, and a node the
// registry does not hold holds none.
func TestFold_ANoteOnAnOwnNodeIsKeptOverOneBrought(t *testing.T) {
	t.Parallel()
	for _, note := range noteCases {
		for _, tc := range []struct {
			name    string
			own     []string // the own enum's values, none a list's
			brought ir.TypeID
			want    string // what the field holds, but of an example
			example string
			change  func(reg ir.TypeRegistry) // what else is so of the own node, or nil
		}{
			{"the type the own node", []string{"own", "brought"}, "type/wide", "", "", nil},
			{"the type a node built of both", []string{"own", "other"}, "type/wide", "", "brought", nil},
			{"a type the registry does not hold", nil, "type/absent", "own", "brought", nil},
			{"a named own node", []string{"own", "brought"}, "type/wide", "brought", "brought",
				func(reg ir.TypeRegistry) { reg["type/own"].Common().Anonymous = false }},
			{"an own node the registry does not hold", []string{"own", "brought"}, "type/wide", "brought", "brought",
				func(reg ir.TypeRegistry) { delete(reg, "type/own") }},
		} {
			reg := narrowingRegistry()
			reg["type/wide"] = stringEnum(ir.TypeCommon{ID: "type/wide", Anonymous: true, Provenance: ptrAt("/part/p")}, "own", "brought", "third")
			held := ir.Property{}
			note.write(&held, "own")
			common := ir.TypeCommon{ID: "type/own", Anonymous: true, Provenance: ir.Provenance{Pointer: "/a", Inferred: promoted},
				Docs: held.Docs, Deprecation: held.Deprecation, Examples: held.Examples}
			reg["type/own"] = stringEnum(common, tc.own...)
			if tc.own == nil {
				reg["type/own"] = &ir.List{TypeCommon: common, ElementType: primitiveRef(ir.PrimitiveString)}
			}
			own := declaration{property: ir.Property{WireName: "p", Type: ir.TypeRef{Target: "type/own"}, Provenance: ptrAt("/a")}, source: written(`{"own":true}`)}
			brought := declaration{property: ir.Property{WireName: "p", Type: ir.TypeRef{Target: tc.brought}, Provenance: ptrAt("/part/p")},
				via: &composedEntry{target: "type/part", at: "/M/allOf/1"}, typeAt: sitesAt("/part/p")}
			note.write(&brought.property, "brought")
			if tc.change != nil {
				tc.change(reg)
			}
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold([]declaration{own, brought})

			label := note.detail + ", " + tc.name
			want := tc.want
			if note.detail == "examples" {
				want = tc.example
			}
			expected := ir.Property{}
			if want != "" {
				note.write(&expected, want)
			}
			assert.Equal(t, expected.Docs, got.Docs, label)
			assert.Equal(t, expected.Deprecation, got.Deprecation, label)
			assert.Equal(t, expected.Examples, got.Examples, label)
			marker := ir.Inferred("")
			if got.Deprecation != nil && want == "own" {
				assert.NotSame(t, reg["type/own"].Common().Deprecation, got.Deprecation, "%s: a copy", label)
				marker = promoted
			}
			assert.Equal(t, marker, got.Provenance.Inferred, "%s: the marker of the node whose deprecation the field holds", label)
			ties := 0
			for _, diagnostic := range *recorded {
				if strings.Contains(diagnostic.Message, "give its "+note.detail+" differently; kept the one the declaration at /a gives over the one at /part/p") {
					ties++
				}
			}
			wantTies := 1
			if tc.change != nil || note.detail == "examples" && tc.name != "the type the own node" {
				wantTies = 0
			}
			assert.Equal(t, wantTies, ties, label)
		}
	}
}

// TestSettle_JudgesAMemberTheModelAloneDeclaresAsItsNodesStand pins the other
// side: a member only the model declares, more than once, is judged as its
// nodes stand, whatever Carried says of one and though a type drops, as no
// property carries a note off a node of such a member. A bare object says
// nothing a map does not, so the later of two maps is the field's type.
func TestSettle_JudgesAMemberTheModelAloneDeclaresAsItsNodesStand(t *testing.T) {
	t.Parallel()
	reg := membersRegistry()
	reg["type/own0"] = &ir.Model{ID: "type/own0", Anonymous: true, Provenance: ptrAt("/M/allOf/0/properties/p")}
	for n, value := range []ir.TypeID{"type/B", "type/primitive/string"} {
		id := ir.TypeID(fmt.Sprintf("type/own%d", n+1))
		reg[id] = &ir.MapT{ID: id, Anonymous: true, Provenance: ptrAt(jsontext.Pointer(fmt.Sprintf("/M/allOf/%d/properties/p", n+1))),
			Value: ir.TypeRef{Target: value}}
	}
	model := &ir.Model{ID: "type/M", Provenance: ptrAt("/M")}
	reg["type/M"] = model
	idx := NewModelPropertyDeclarations(model)
	own("p", "type/own0", "0")(idx)
	own("p", "type/own1", "1")(idx)
	own("p", "type/own2", "2")(idx)
	stubbed, _ := stubMerger(reg)
	stubbed.Carried = map[ir.TypeID]CarriedNotes{"type/own0": {Docs: ir.Docs{Description: "carried"}}}

	diags := settleFlat(stubbed, idx, reg)

	assert.Len(t, diags, 1, "the earlier map is dropped")
	require.Len(t, model.Properties, 1)
	assert.Equal(t, ir.TypeID("type/own2"), model.Properties[0].Type.Target)
}

// TestWithNotes_HoldsTheNotesOnACopy pins that a node read holding its notes
// is a copy holding them and all the node does, which is not written to.
func TestWithNotes_HoldsTheNotesOnACopy(t *testing.T) {
	t.Parallel()
	carried := CarriedNotes{Docs: ir.Docs{Summary: "title"}, Deprecation: &ir.Deprecation{Message: "gone"}, Examples: []ir.Example{{Summary: "one"}}}
	node := &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/item"}}

	got := withNotes(node, carried)

	assert.Equal(t, &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/item"},
		Docs: carried.Docs, Deprecation: carried.Deprecation, Examples: carried.Examples}, got)
	assert.Equal(t, &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/item"}}, node, "the node is not written to")
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
