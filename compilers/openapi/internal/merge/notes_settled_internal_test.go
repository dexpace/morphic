// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// broughtNoting is a declaration of p that an entry brings, at at, of the type
// target names, its property describing it as description where it is not
// empty.
func broughtNoting(target ir.TypeID, at jsontext.Pointer, description string) declaration {
	property := ir.Property{WireName: "p", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(at), Docs: ir.Docs{Description: description}}
	return declaration{property: property, via: &composedEntry{target: "type/part", at: "/M/allOf" + at}, typeAt: sitesAt(at)}
}

// describing is the notes a property carries off a node described as
// description.
func describing(description string) CarriedNotes {
	return CarriedNotes{Docs: ir.Docs{Description: description}}
}

// notesRegistry holds, written at /a, /b and /c, two enums, one within the
// other, three lists of strings, one bounded, and two models saying nothing.
func notesRegistry() ir.TypeRegistry {
	reg := membersRegistry()
	common := func(id ir.TypeID, at jsontext.Pointer) ir.TypeCommon {
		return ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(at)}
	}
	reg["type/wide"] = stringEnum(common("type/wide", "/a"), "a", "b", "c")
	reg["type/narrow"] = stringEnum(common("type/narrow", "/b"), "a", "b")
	reg["type/list"] = &ir.List{TypeCommon: common("type/list", "/a"), ElementType: primitiveRef(ir.PrimitiveString)}
	reg["type/list-b"] = &ir.List{TypeCommon: common("type/list-b", "/b"), ElementType: primitiveRef(ir.PrimitiveString)}
	reg["type/bounded"] = &ir.List{TypeCommon: common("type/bounded", "/c"), ElementType: primitiveRef(ir.PrimitiveString),
		Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	reg["type/bare-a"] = &ir.Model{TypeCommon: common("type/bare-a", "/a")}
	reg["type/bare-b"] = &ir.Model{TypeCommon: common("type/bare-b", "/b")}
	return reg
}

// TestFold_ATieOfBroughtNotesFollowsTheTypesNode pins keptNote between notes
// other models' declarations bring, in both orders: one off no node is kept
// over one of a node, the later of two such; of several of nodes, the one of
// the node the field's type is built on with each node holding its note, the
// winner's or one it meets, the later where the types tie; and none where they
// compose, but one the node the field's type names holds, where it stays.
func TestFold_ATieOfBroughtNotesFollowsTheTypesNode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		decls   []declaration
		carried map[ir.TypeID]CarriedNotes
		onNode  map[ir.TypeID]string // what a node holds itself
		want    [2]string            // as written, and reversed
		reports int
		none    bool
	}{
		{"one off no node", []declaration{broughtNoting("type/primitive/string", "/a", "a"), broughtNoting("type/narrow", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/narrow": describing("b")}, nil, [2]string{"a", "a"}, 1, false},
		{"two off no node", []declaration{broughtNoting("type/primitive/string", "/a", "a"), broughtNoting("type/primitive/string", "/b", "b")},
			nil, nil, [2]string{"b", "a"}, 1, false},
		{"the types tie", []declaration{broughtNoting("type/wide", "/a", "a"), broughtNoting("type/narrow", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/wide": describing("a"), "type/narrow": describing("b")}, nil, [2]string{"b", "a"}, 1, false},
		{"one saying the same of another type", []declaration{broughtNoting("type/wide", "/a", "a"), broughtNoting("type/list", "/b", "a")},
			map[ir.TypeID]CarriedNotes{"type/wide": describing("a"), "type/list": describing("a")}, nil, [2]string{"a", "a"}, 0, false},
		{"the winner meets one, the other dropped", []declaration{broughtNoting("type/bounded", "/c", ""), broughtNoting("type/list", "/a", "a"), broughtNoting("type/narrow", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/list": describing("a"), "type/narrow": describing("b")}, nil, [2]string{"a", "a"}, 1, false},
		{"the winner holds none, both dropped", []declaration{broughtNoting("type/primitive/integer", "/c", ""), broughtNoting("type/primitive/integer", "/d", ""),
			broughtNoting("type/list", "/a", "a"), broughtNoting("type/narrow", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/list": describing("a"), "type/narrow": describing("b")}, nil, [2]string{"", ""}, 2, true},
		{"they compose", []declaration{broughtNoting("type/bare-a", "/a", "a"), broughtNoting("type/bare-b", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/bare-a": describing("a"), "type/bare-b": describing("b")}, nil, [2]string{"", ""}, 2, true},
		{"they compose, the node the type names holding one", []declaration{broughtNoting("type/bare-a", "/a", ""), broughtNoting("type/bare-b", "/b", "b")},
			map[ir.TypeID]CarriedNotes{"type/bare-b": describing("b")}, map[ir.TypeID]string{"type/bare-a": "a"}, [2]string{"", ""}, 1, false},
		{"another node holding one the type does not", []declaration{broughtNoting("type/primitive/integer", "/c", "c"), broughtNoting("type/primitive/integer", "/d", ""),
			broughtNoting("type/list", "/a", "")},
			nil, map[ir.TypeID]string{"type/list": "a"}, [2]string{"c", "c"}, 0, false},
	} {
		for order := range 2 {
			reg := notesRegistry()
			for id, description := range tc.onNode {
				reg[id].Common().Docs.Description = description
			}
			decls := slices.Clone(tc.decls)
			if order == 1 {
				slices.Reverse(decls)
			}
			stubbed, _ := stubMerger(reg)
			stubbed.Carried = tc.carried

			got, _, diags := stubbed.foldAt(decls, "/M/allOf/properties/p", nil)

			label := tc.name + map[bool]string{true: ", reversed"}[order == 1]
			assert.Equal(t, tc.want[order], got.Docs.Description, label)
			says := map[bool]string{true: "differently, and the type it takes is of none of them alone; kept none, and the one the declaration at ",
				false: "differently; kept the one the declaration at "}[tc.none]
			ties := 0
			for _, diagnostic := range diags {
				if strings.Contains(diagnostic.Message, "give its description "+says) {
					assert.Contains(t, diagnostic.Message, "stays declared on its model", label)
					ties++
				}
			}
			assert.Equal(t, tc.reports, ties, "%s: %v", label, diags)
		}
	}
}

// TestFold_ASettledMembersNotesAreThoseOfItsNodes pins SettledNotes: the node
// of a member is read holding the notes of those its type is built on, judged
// with each holding its own, none of a dropped one's or where they compose; and
// the member took a note off a node where none of its declarations gives one
// off none. A member no entry brings a declaration of says nothing.
func TestFold_ASettledMembersNotesAreThoseOfItsNodes(t *testing.T) {
	t.Parallel()
	carried := CarriedNotes{Docs: ir.Docs{Summary: "a", Description: "a", ExternalDocs: []ir.Link{{URL: "a"}}}, Deprecation: &ir.Deprecation{Message: "a"}, Examples: []ir.Example{{Summary: "a"}}}
	noting := broughtNoting("type/list", "/a", "a")
	noting.property.Docs, noting.property.Deprecation, noting.property.Examples = carried.Docs, carried.Deprecation, carried.Examples
	described := describing("a")
	other := CarriedNotes{Docs: ir.Docs{Description: "b"}, Deprecation: &ir.Deprecation{Message: "b"}}
	noting2 := broughtNoting("type/list-b", "/b", "b")
	noting2.property.Deprecation = other.Deprecation
	for _, tc := range []struct {
		name    string
		decls   []declaration
		carried map[ir.TypeID]CarriedNotes
		want    *SettledNotes
	}{
		{"the winner's", []declaration{noting, broughtNoting("type/primitive/any", "/c", "")}, map[ir.TypeID]CarriedNotes{"type/list": carried},
			&SettledNotes{Node: carried, OffNodes: carried}},
		{"one it meets", []declaration{broughtNoting("type/list", "/a", "a"), broughtNoting("type/bounded", "/c", "")}, map[ir.TypeID]CarriedNotes{"type/list": described},
			&SettledNotes{Node: described, OffNodes: described}},
		{"of two it meets, the later's", []declaration{broughtNoting("type/bounded", "/c", ""), noting, noting2},
			map[ir.TypeID]CarriedNotes{"type/list": {Docs: carried.Docs, Deprecation: carried.Deprecation}, "type/list-b": other},
			&SettledNotes{Node: other, OffNodes: CarriedNotes{Docs: ir.Docs{Summary: "a", Description: "b", ExternalDocs: carried.Docs.ExternalDocs}, Deprecation: other.Deprecation}}},
		{"a dropped one's", []declaration{broughtNoting("type/primitive/integer", "/c", ""), broughtNoting("type/primitive/integer", "/d", ""), broughtNoting("type/list", "/a", "a")},
			map[ir.TypeID]CarriedNotes{"type/list": described}, &SettledNotes{OffNodes: described}},
		{"one composed", []declaration{broughtNoting("type/bare-a", "/a", "a"), broughtNoting("type/bare-b", "/b", "a")},
			map[ir.TypeID]CarriedNotes{"type/bare-a": describing("a"), "type/bare-b": describing("other")}, &SettledNotes{}},
		{"one off no node", []declaration{broughtNoting("type/list", "/a", "a"), broughtNoting("type/primitive/any", "/c", "c")}, map[ir.TypeID]CarriedNotes{"type/list": described},
			&SettledNotes{Node: described}},
		{"a member the model alone declares", []declaration{{property: noting.property, source: written("{}")}, {property: noting.property, source: written("{}")}}, nil, nil},
	} {
		reg := notesRegistry()
		reg["type/primitive/any"] = &ir.Primitive{ID: "type/primitive/any", PrimitiveKind: ir.PrimitiveAny}
		stubbed, _ := stubMerger(reg)
		stubbed.Carried = tc.carried

		_, got, _ := stubbed.foldAt(tc.decls, "/M/allOf/properties/p", nil)

		assert.Equal(t, tc.want, got, tc.name)
	}
}

// TestFold_ASettledMemberIsJudgedWithItsNotes pins notedBeside: beside a type
// the types as they stand drop, a settled member's node is read holding what
// its model says, over what its own property carries, so a bounded list says
// nothing of its note and the two tie; and of two settled on one node, the one
// written first says, in either order.
func TestFold_ASettledMemberIsJudgedWithItsNotes(t *testing.T) {
	t.Parallel()
	settledAs := func(at jsontext.Pointer, description string) declaration {
		out := broughtNoting("type/list", at, description)
		out.settled, out.typeAt = &SettledNotes{Node: describing(description), OffNodes: describing(description)}, sitesAt("/a")
		return out
	}
	apart := broughtNoting("type/primitive/integer", "/d", "")
	for _, tc := range []struct {
		name  string
		decls []declaration
		want  []string // the declarations whose types the field drops
	}{
		{"of another note", []declaration{apart, settledAs("/a", "a"), broughtNoting("type/bounded", "/c", "c")}, []string{"/a", "/d"}},
		{"of the same", []declaration{apart, settledAs("/a", "c"), broughtNoting("type/bounded", "/c", "c")}, []string{"/d"}},
		{"two of one node", []declaration{apart, settledAs("/a", "c"), settledAs("/b", "a"), broughtNoting("type/bounded", "/c", "c")}, []string{"/d"}},
		{"two of one node, reversed", []declaration{apart, settledAs("/b", "a"), settledAs("/a", "c"), broughtNoting("type/bounded", "/c", "c")}, []string{"/d"}},
	} {
		stubbed, _ := stubMerger(notesRegistry())
		stubbed.Carried = map[ir.TypeID]CarriedNotes{"type/bounded": describing("c"), "type/list": describing("carried")}

		got, _, diags := stubbed.foldAt(tc.decls, "/M/allOf/properties/p", nil)

		require.Equal(t, ir.TypeID("type/bounded"), got.Type.Target, tc.name)
		var dropped []string
		for _, diagnostic := range diags {
			for _, at := range []string{"/a", "/b", "/c", "/d"} {
				if strings.Contains(diagnostic.Message, "types ") && strings.Contains(diagnostic.Message, " over the one at "+at+",") {
					dropped = append(dropped, at)
				}
			}
		}
		assert.ElementsMatch(t, tc.want, dropped, "%s: %v", tc.name, diags)
	}
}

// TestFold_ANoteOfATypeItMeetsInNoNodeIsNotItsTypes pins that a declaration
// whose type meets the winner's, in no node the fold can build, is none the
// field's type is built on: of the two deprecations such ones bring, the field
// holds neither, and its node is read holding the winner's notes alone.
func TestFold_ANoteOfATypeItMeetsInNoNodeIsNotItsTypes(t *testing.T) {
	t.Parallel()
	reg := notesRegistry()
	carried := map[ir.TypeID]CarriedNotes{"type/anys": describing("w")}
	decls := []declaration{broughtNoting("type/anys", "/c", "w"), {}, {}}
	for i, element := range []ir.PrimitiveKind{ir.PrimitiveString, ir.PrimitiveBool} {
		at := []jsontext.Pointer{"/a", "/b"}[i]
		id := ir.TypeID("type/of" + string(at))
		reg[primitiveRef(element).Target] = &ir.Primitive{ID: primitiveRef(element).Target, PrimitiveKind: element}
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(at), ElementType: primitiveRef(element)}
		carried[id] = CarriedNotes{Deprecation: &ir.Deprecation{Message: string(at)}}
		deprecated := broughtNoting(id, at, "")
		deprecated.property.Deprecation = carried[id].Deprecation
		decls[i+1] = deprecated
	}
	reg["type/anys"] = &ir.List{ID: "type/anys", Anonymous: true, Provenance: ptrAt("/c"), ElementType: primitiveRef(ir.PrimitiveAny)}
	reg["type/primitive/any"] = &ir.Primitive{ID: "type/primitive/any", PrimitiveKind: ir.PrimitiveAny}
	stubbed, _ := stubMerger(reg)
	stubbed.Carried = carried

	got, settled, _ := stubbed.foldAt(decls, "/M/allOf/properties/p", nil)

	assert.Equal(t, ir.TypeID("type/anys"), got.Type.Target)
	assert.Nil(t, got.Deprecation)
	assert.Equal(t, &SettledNotes{Node: describing("w"), OffNodes: describing("w")}, settled)
}
