// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// meetRegistry holds the nodes the builder tests declare fields as: enums,
// lists and a time scalar, each anonymous at the declaration it is written at,
// so none is a copy of another, and the models A and B.
func meetRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	at := func(n int) ir.Provenance { return ptrAt(declaredAt(n)) }
	members := func(n int, values ...string) []ir.EnumMember {
		out := make([]ir.EnumMember, len(values))
		for i, v := range values {
			out[i] = ir.EnumMember{ID: ir.EnumMemberID("enum-member/anonymous" + string(declaredAt(n)) + "/s:" + v), Value: stringValue(v)}
		}
		return out
	}
	enum := func(id ir.TypeID, n int, values ...string) {
		reg[id] = &ir.Enum{ID: id, Anonymous: true, Provenance: at(n),
			ValueType: ir.PrimitiveString, Members: members(n, values...)}
	}
	enum("type/ab", 0, "a", "b")
	enum("type/a", 1, "a")
	enum("type/bc", 1, "b", "c")
	enum("type/c", 1, "c")
	enum("type/ac", 1, "a", "c")
	enum("type/bc2", 2, "b", "c")
	enum("type/abc", 0, "a", "b", "c")
	enum("type/bcd", 1, "b", "c", "d")
	enum("type/cbd", 1, "c", "b", "d")
	enum("type/ba", 1, "b", "a")
	enum("type/abc-described", 0, "a", "b", "c")
	reg["type/abc-described"].Common().Docs.Description = "d"
	list := func(id ir.TypeID, n int, element ir.TypeRef, typeCommon ir.TypeCommon) {
		typeCommon.ID, typeCommon.Anonymous, typeCommon.Provenance = id, true, at(n)
		reg[id] = &ir.List{TypeCommon: typeCommon, ElementType: element}
	}
	described := func(d string) ir.TypeCommon { return ir.TypeCommon{Docs: ir.Docs{Description: d}} }
	list("type/ints-described", 0, primitiveRef(ir.PrimitiveInteger), described("d"))
	list("type/i32s", 1, primitiveRef(ir.PrimitiveInt32), ir.TypeCommon{})
	list("type/anys-described", 0, primitiveRef(ir.PrimitiveAny), described("d"))
	list("type/strs", 1, primitiveRef(ir.PrimitiveString), ir.TypeCommon{})
	list("type/bools", 2, primitiveRef(ir.PrimitiveBool), ir.TypeCommon{})
	list("type/i32s-one", 1, primitiveRef(ir.PrimitiveInt32), described("one"))
	list("type/i32s-other", 2, primitiveRef(ir.PrimitiveInt32), described("other"))
	reg["type/A"] = &ir.Model{ID: "type/A", Provenance: ptrAt("/components/schemas/A"),
		Properties: []ir.Property{{WireName: "a"}}}
	reg["type/B"] = &ir.Model{ID: "type/B", Provenance: ptrAt("/components/schemas/B"),
		Properties: []ir.Property{{WireName: "b"}}}
	list("type/As", 0, ref("type/A"), ir.TypeCommon{})
	list("type/Bs", 1, ref("type/B"), ir.TypeCommon{})
	reg["type/A-map"] = &ir.MapT{ID: "type/A-map", Anonymous: true, Provenance: at(0), Value: ref("type/A")}
	reg["type/B-map"] = &ir.MapT{ID: "type/B-map", Anonymous: true, Provenance: at(1), Value: ref("type/B")}
	reg["type/A-tuple"] = &ir.Tuple{ID: "type/A-tuple", Anonymous: true, Provenance: at(0),
		ElementTypes: []ir.TypeRef{ref("type/A")}}
	reg["type/B-tuple"] = &ir.Tuple{ID: "type/B-tuple", Anonymous: true, Provenance: at(1),
		ElementTypes: []ir.TypeRef{ref("type/B")}}
	reg["type/time-described"] = &ir.Scalar{ID: "type/time-described", Anonymous: true,
		Provenance: at(0), Docs: ir.Docs{Description: "d"}, Base: &ir.TypeRef{Target: "type/primitive/string"},
		Encoding: &ir.Encoding{Name: "time"}}
	reg["type/noon"] = &ir.Literal{ID: "type/noon", Anonymous: true, Provenance: at(1),
		Value: stringValue("12:00:00Z"), ValueType: ir.PrimitiveString}
	return reg
}

// foldOf folds a field declared as each of targets, in order, and returns the
// field, the registry and what the fold reported.
func foldOf(targets ...ir.TypeID) (ir.Property, map[ir.TypeID]ir.TypeDef, []ir.Diagnostic) {
	reg := meetRegistry()
	stubbed, recorded := stubMerger(reg)
	return stubbed.fold(declsOf(targets...)), reg, *recorded
}

// TestFold_EnumsAreJudgedByTheValuesTheyShare pins GitHub #1042's enums in both
// orders: the one whose values the other holds every one of is the field's
// type, with nothing minted, kept or reported; two sharing only some meet in an
// enum of those, minted at the field with its members beneath it; and two
// sharing none are disjoint, which warns.
func TestFold_EnumsAreJudgedByTheValuesTheyShare(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"type/ab", "type/a"}, {"type/a", "type/ab"}} {
		got, _, recorded := foldOf(order...)
		assert.Equal(t, ir.TypeID("type/a"), got.Type.Target, "%v", order)
		assert.Empty(t, recorded, "%v", order)
		assert.Empty(t, got.Unmodeled, "%v", order)
	}

	got, reg, recorded := foldOf("type/ab", "type/bc")
	require.Equal(t, composedAt(declaredAt(1)), got.Type.Target)
	enum, ok := reg[got.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 1)
	assert.Equal(t, stringValue("b"), enum.Members[0].Value)
	assert.Empty(t, recorded)
	assert.Len(t, reg["type/ab"].(*ir.Enum).Members, 2, "the declared enum is as it was written")

	got, _, recorded = foldOf("type/ab", "type/c")
	assert.Equal(t, ir.TypeID("type/c"), got.Type.Target, "a tie goes to the later")
	require.Len(t, recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, recorded[0].Code)
}

// TestFold_ANarrowerListAdoptsTheBroadersDetail pins GitHub #1042's list in
// both orders: a list of int32 beside a described list of integer is a list of
// int32 holding the description, minted at the field, with nothing reported.
func TestFold_ANarrowerListAdoptsTheBroadersDetail(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"type/ints-described", "type/i32s"}, {"type/i32s", "type/ints-described"}} {
		got, reg, recorded := foldOf(order...)
		require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%v", order)
		list, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, primitiveRef(ir.PrimitiveInt32), list.ElementType, "%v", order)
		assert.Equal(t, "d", list.Docs.Description, "%v", order)
		assert.Empty(t, recorded, "%v", order)
		assert.Empty(t, got.Unmodeled, "%v", order)
	}
}

// TestFold_ANarrowerListAdoptsTheBroadersEncoding pins the encoding a list
// holds beside its detail: adopted as its documentation is, in either order, and
// where two declarations meeting the first give it differently, the first's
// kept and the other reported and kept.
func TestFold_ANarrowerListAdoptsTheBroadersEncoding(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"type/ints-encoded", "type/i32s"}, {"type/i32s", "type/ints-encoded"}} {
		reg := meetRegistry()
		reg["type/ints-encoded"] = &ir.List{ID: "type/ints-encoded", Anonymous: true,
			Provenance: ptrAt(declaredAt(0)), ElementType: primitiveRef(ir.PrimitiveInteger), Encoding: &ir.Encoding{Name: "csv"}}
		stubbed, recorded := stubMerger(reg)

		got := stubbed.fold(declsOf(order...))

		list, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, primitiveRef(ir.PrimitiveInt32), list.ElementType, "%v", order)
		assert.Equal(t, &ir.Encoding{Name: "csv"}, list.Encoding, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
	}

	reg := meetRegistry()
	encoded := func(id ir.TypeID, n int, element ir.PrimitiveKind, enc *ir.Encoding, constraints *ir.Constraints) {
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			ElementType: primitiveRef(element), Encoding: enc, Constraints: constraints}
	}
	encoded("type/ints-min", 0, ir.PrimitiveInteger, nil, &ir.Constraints{MinItems: new(int64(1))})
	encoded("type/i32s-csv", 1, ir.PrimitiveInt32, &ir.Encoding{Name: "csv"}, nil)
	encoded("type/i32s-tsv", 2, ir.PrimitiveInt32, &ir.Encoding{Name: "tsv"}, nil)
	stubbedValue, recorded := stubMerger(reg)

	got := stubbedValue.fold(declsOf("type/ints-min", "type/i32s-csv", "type/i32s-tsv"))

	irList, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Encoding{Name: "tsv"}, irList.Encoding, "the last encoding given is kept")
	require.Len(t, *recorded, 1, "and the other reported")
	assert.Equal(t, declaredAt(1), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(1)))
}

// TestFold_ContainersOfModelsComposeTheirMembers pins GitHub #1180 in both
// orders: two lists, maps or tuples of different models stand on one minted
// container whose member is a model composing both, minted at a position of its
// own beneath the field's, with nothing kept or reported.
func TestFold_ContainersOfModelsComposeTheirMembers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b   ir.TypeID
		member func(ir.TypeDef) ir.TypeRef
		at     jsontext.Pointer
	}{
		{"type/As", "type/Bs", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.List).ElementType }, "/items"},
		{"type/A-map", "type/B-map", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.MapT).Value }, "/additionalProperties"},
		{"type/A-tuple", "type/B-tuple", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.Tuple).ElementTypes[0] }, "/prefixItems/0"},
	} {
		for _, order := range [][]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			got, reg, recorded := foldOf(order...)
			require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%v", order)
			member := tc.member(reg[got.Type.Target])
			require.Equal(t, composedAt(declaredAt(1)+tc.at), member.Target, "%v", order)
			model, ok := reg[member.Target].(*ir.Model)
			require.True(t, ok, "%v", order)
			assert.ElementsMatch(t, []ir.TypeID{"type/A", "type/B"}, targetsOf(model.Mixins), "%v", order)
			assert.Empty(t, recorded, "%v", order)
			assert.Empty(t, got.Unmodeled, "%v", order)
		}
	}
}

// TestFold_AConstMeetsATimeScalarHoldingDetail pins the const a time scalar
// admits beside one holding a description: the field is the const, minted
// holding the description too, in either order.
func TestFold_AConstMeetsATimeScalarHoldingDetail(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"type/time-described", "type/noon"}, {"type/noon", "type/time-described"}} {
		got, reg, recorded := foldOf(order...)
		lit, ok := reg[got.Type.Target].(*ir.Literal)
		require.True(t, ok, "%v", order)
		assert.Equal(t, stringValue("12:00:00Z"), lit.Value, "%v", order)
		assert.Equal(t, "d", lit.Docs.Description, "%v", order)
		assert.Empty(t, recorded, "%v", order)
	}
}

// TestFold_AMeetingNoNodeHoldsKeepsTheWinner pins the fall back to the winner's
// own node: three lists, each of the others meeting the first, whose elements a
// string and a bool share no value, stand on no list, so the field keeps the
// first, and each declaration meeting it is dropped, reported and kept, with
// nothing minted.
func TestFold_AMeetingNoNodeHoldsKeepsTheWinner(t *testing.T) {
	t.Parallel()
	got, reg, recorded := foldOf("type/anys-described", "type/strs", "type/bools")
	count := len(meetRegistry())

	assert.Equal(t, ir.TypeID("type/anys-described"), got.Type.Target)
	assert.Len(t, reg, count, "nothing is minted")
	assert.Len(t, recorded, 2)
	assert.ElementsMatch(t, []string{losingDeclarationKey + string(declaredAt(1)), losingDeclarationKey + string(declaredAt(2))},
		slices.Collect(maps.Keys(got.Unmodeled)))
}

// TestFold_ADetailTwoMeetingDeclarationsGiveDifferentlyIsKept pins a detail two
// declarations meeting the winner each add, differently: the minted node takes
// the later's, and the earlier is reported, naming the detail as a document
// spells it, and kept, its narrower element still adopted.
func TestFold_ADetailTwoMeetingDeclarationsGiveDifferentlyIsKept(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	reg["type/ints-min"] = &ir.List{ID: "type/ints-min", Anonymous: true, Provenance: ptrAt(declaredAt(0)),
		ElementType: primitiveRef(ir.PrimitiveInteger), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	stubbed, recorded := stubMerger(reg)

	got := stubbed.fold(declsOf("type/ints-min", "type/i32s-one", "type/i32s-other"))

	list, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, "other", list.Docs.Description)
	assert.Equal(t, primitiveRef(ir.PrimitiveInt32), list.ElementType)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, list.Constraints)
	require.Len(t, *recorded, 1)
	assert.Equal(t, declaredAt(1), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, (*recorded)[0].Message, "give the docs of a type", "the detail is named as a document spells it")
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(1)))
}

// TestFold_EnumsListingSharedValuesApartKeepTheWinner pins two enums listing
// the values they share in different orders, in either branch order: an enum of
// both would take one declaration's order, so the two overlap, and the field
// keeps the later, reports the other and mints nothing, whether each holds a
// value the other lacks or one holds only the other's values beside less
// detail. Two listing them in one order meet in an enum of those in that order.
func TestFold_EnumsListingSharedValuesApartKeepTheWinner(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]ir.TypeID{{"type/abc", "type/cbd"}, {"type/abc-described", "type/ba"}} {
		for _, order := range [][2]ir.TypeID{pair, {pair[1], pair[0]}} {
			got, reg, recorded := foldOf(order[:]...)

			assert.Equal(t, order[1], got.Type.Target, "%v", order)
			assert.Len(t, reg, len(meetRegistry()), "nothing is minted: %v", order)
			require.Len(t, recorded, 1, "%v", order)
			assert.Equal(t, diag.DegradedConstruct, recorded[0].Code, "%v", order)
		}
	}

	for _, order := range [][]ir.TypeID{{"type/abc", "type/bcd"}, {"type/bcd", "type/abc"}} {
		got, reg, recorded := foldOf(order...)

		enum, ok := reg[got.Type.Target].(*ir.Enum)
		require.True(t, ok, "%v", order)
		require.Len(t, enum.Members, 2, "%v", order)
		assert.Equal(t, []ir.Value{stringValue("b"), stringValue("c")}, []ir.Value{enum.Members[0].Value, enum.Members[1].Value}, "%v", order)
		assert.Empty(t, recorded, "%v", order)
	}
}

// TestFold_EnumsSharingNoValueTogetherKeepTheWinner pins three enums each
// meeting the others in a value no third holds: no value is in all three, so no
// enum holds them, and the field keeps the last, each other reported and kept.
func TestFold_EnumsSharingNoValueTogetherKeepTheWinner(t *testing.T) {
	t.Parallel()
	got, reg, recorded := foldOf("type/ab", "type/ac", "type/bc2")

	assert.Equal(t, ir.TypeID("type/bc2"), got.Type.Target)
	assert.Len(t, reg, len(meetRegistry()), "nothing is minted")
	assert.Len(t, recorded, 2)
}

// TestFold_AFailedMeetingReportsOnlyItsDrops pins the dry run's silence: two
// tuples meeting the first, whose first positions meet in lists giving a pattern
// differently and whose second hold a string and a bool, stand on no tuple. The
// pattern no list was built with is not reported, only each dropped declaration.
func TestFold_AFailedMeetingReportsOnlyItsDrops(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	list := func(id ir.TypeID, n int, element ir.TypeRef, constraints *ir.Constraints) {
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n) + "/prefixItems/0"),
			ElementType: element, Constraints: constraints}
	}
	list("type/l0", 0, primitiveRef(ir.PrimitiveAny), &ir.Constraints{Pattern: "^a$"})
	list("type/l1", 1, primitiveRef(ir.PrimitiveString), &ir.Constraints{Pattern: "^b$"})
	list("type/l2", 2, primitiveRef(ir.PrimitiveAny), &ir.Constraints{MaxItems: new(int64(3))})
	tuple := func(id ir.TypeID, n int, first ir.TypeID, second ir.TypeRef) {
		reg[id] = &ir.Tuple{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			ElementTypes: []ir.TypeRef{ref(first), second}}
	}
	tuple("type/t0", 0, "type/l0", primitiveRef(ir.PrimitiveAny))
	tuple("type/t1", 1, "type/l1", primitiveRef(ir.PrimitiveString))
	tuple("type/t2", 2, "type/l2", primitiveRef(ir.PrimitiveBool))
	stubbed, recorded := stubMerger(reg)

	got := stubbed.fold(declsOf("type/t0", "type/t1", "type/t2"))

	assert.Equal(t, ir.TypeID("type/t0"), got.Type.Target)
	require.Len(t, *recorded, 2, "one report per dropped declaration")
	for _, diagnostic := range *recorded {
		assert.Equal(t, diag.DegradedConstruct, diagnostic.Code)
	}
}

// TestFold_AnEnumWithinAnotherAdoptsItsDetail pins GitHub #1042's enum with the
// broader side's detail in both orders: the field is an enum of the narrower's
// values, minted holding the broader's description.
func TestFold_AnEnumWithinAnotherAdoptsItsDetail(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"type/ab-described", "type/a"}, {"type/a", "type/ab-described"}} {
		reg := meetRegistry()
		ab := *reg["type/ab"].(*ir.Enum)
		ab.ID, ab.Docs = "type/ab-described", ir.Docs{Description: "d"}
		reg["type/ab-described"] = &ab
		stubbed, recorded := stubMerger(reg)

		got := stubbed.fold(declsOf(order...))

		enum, ok := reg[got.Type.Target].(*ir.Enum)
		require.True(t, ok, "%v", order)
		require.Len(t, enum.Members, 1, "%v", order)
		assert.Equal(t, stringValue("a"), enum.Members[0].Value, "%v", order)
		assert.Equal(t, "d", enum.Docs.Description, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
	}
}

// TestFold_ANarrowerListAdoptsWhatTheBroaderKeeps pins what a node keeps under
// Unmodeled: adopted with the provenance it was kept at, in either order, and a
// key two declarations meeting the first keep differently is the first's, the
// other reported and kept.
func TestFold_ANarrowerListAdoptsWhatTheBroaderKeeps(t *testing.T) {
	t.Parallel()
	kept := func(v string) ir.Unmodeled {
		return ir.Unmodeled{"openapi:x-a": {Value: ir.RawValue(v), Provenance: ptrAt("/kept/" + jsontext.Pointer(v))}}
	}
	for _, order := range [][]ir.TypeID{{"type/ints-kept", "type/i32s"}, {"type/i32s", "type/ints-kept"}} {
		reg := meetRegistry()
		reg["type/ints-kept"] = &ir.List{ID: "type/ints-kept", Anonymous: true,
			Provenance: ptrAt(declaredAt(0)), Unmodeled: kept("1"), ElementType: primitiveRef(ir.PrimitiveInteger)}
		stubbed, recorded := stubMerger(reg)

		got := stubbed.fold(declsOf(order...))

		list, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, kept("1"), list.Unmodeled, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
	}

	reg := meetRegistry()
	reg["type/ints-min"] = &ir.List{ID: "type/ints-min", Anonymous: true, Provenance: ptrAt(declaredAt(0)),
		ElementType: primitiveRef(ir.PrimitiveInteger), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	for n, v := range []string{"1", "2"} {
		id := ir.TypeID("type/i32s-kept-" + v)
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n + 1)),
			Unmodeled: kept(v), ElementType: primitiveRef(ir.PrimitiveInt32)}
	}
	stubbedValue, recorded := stubMerger(reg)

	got := stubbedValue.fold(declsOf("type/ints-min", "type/i32s-kept-1", "type/i32s-kept-2"))

	irList, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, kept("2"), irList.Unmodeled)
	require.Len(t, *recorded, 1)
	assert.Equal(t, declaredAt(1), (*recorded)[0].Provenance.Pointer)
}

// TestFold_TuplesBuildWhatFollowsTheirPositions pins a tuple's rest: two tuples
// of different models with rests of different models stand on one tuple whose
// position and rest each compose both, the rest minted at its own position.
func TestFold_TuplesBuildWhatFollowsTheirPositions(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	for n, typeID := range []ir.TypeID{"type/A", "type/B"} {
		id := ir.TypeID("type/" + strings.TrimPrefix(string(typeID), "type/") + "-rest-tuple")
		reg[id] = &ir.Tuple{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			ElementTypes: []ir.TypeRef{ref(typeID)}, Rest: new(ref(typeID))}
	}
	stubbed, recorded := stubMerger(reg)

	got := stubbed.fold(declsOf("type/A-rest-tuple", "type/B-rest-tuple"))

	tu, ok := reg[got.Type.Target].(*ir.Tuple)
	require.True(t, ok)
	require.NotNil(t, tu.Rest)
	assert.Equal(t, composedAt(declaredAt(1)+"/items"), tu.Rest.Target)
	assert.Equal(t, composedAt(declaredAt(1)+"/prefixItems/0"), tu.ElementTypes[0].Target)
	assert.Empty(t, *recorded)
}

// TestBuilder_RefusesWhatNoNodeHolds pins the build's own guards, which the
// judgment keeps every fold from reaching: a partner the registry does not
// hold, nodes of a kind no node is built of, tuples of different shapes, and an
// enum beside a node of another kind each fail the build, and a literal meets
// only a Scalar holding more than its format.
func TestBuilder_RefusesWhatNoNodeHolds(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	reg["type/A-pair-tuple"] = &ir.Tuple{ID: "type/A-pair-tuple", Anonymous: true, Provenance: ptrAt(declaredAt(1)),
		ElementTypes: []ir.TypeRef{ref("type/A"), ref("type/A")}}
	reg["type/A-rest-tuple"] = &ir.Tuple{ID: "type/A-rest-tuple", Anonymous: true, Provenance: ptrAt(declaredAt(1)),
		ElementTypes: []ir.TypeRef{ref("type/A")}, Rest: new(ref("type/A"))}
	stubbed, _ := stubMerger(reg)
	folding := newFolding(&stubbed.Merger, declsOf("type/A", "type/B"), declaredAt(1))
	m := func(id ir.TypeID, decl int) member { return member{ref: ref(id), decl: decl} }
	for _, tc := range []struct {
		name          string
		base, partner member
	}{
		{"a partner the registry does not hold", m("type/A", 0), m("type/absent", 1)},
		{"two models", m("type/A", 0), m("type/B", 1)},
		{"tuples of different lengths", m("type/A-tuple", 0), m("type/A-pair-tuple", 1)},
		{"a tuple beside one admitting more", m("type/A-tuple", 0), m("type/A-rest-tuple", 1)},
		{"a tuple admitting more beside one that does not", m("type/A-rest-tuple", 0), m("type/A-tuple", 1)},
		{"an enum beside a list", m("type/ab", 0), m("type/i32s", 1)},
	} {
		b := builder{f: folding, dry: true}
		b.meetNode(tc.base, []member{tc.partner}, declaredAt(1), 0)
		assert.True(t, b.failed, tc.name)
	}

	typeJudge := judge(stubbed)
	assert.Equal(t, relOverlap, typeJudge.literalBesideDetail(&ir.Literal{Value: stringValue("a")}, reg["type/ab"]))
}

// TestFold_AValueIsJudgedByTheNodeTheFieldHolds pins a value written beside a
// type the merge builds the field's node from: that node can hold members or
// values the declaration's own does not, so the value is judged against it, in
// either order. A list of integer's default past int32 is refused beside a list
// of int32, and an enum's default the meeting cut is refused beside the enum it
// meets, each reported and kept.
func TestFold_AValueIsJudgedByTheNodeTheFieldHolds(t *testing.T) {
	t.Parallel()
	big := ir.Value{Kind: ir.ValueList, List: []ir.Value{number("5000000000")}}
	for _, tc := range []struct {
		name          string
		valued, other ir.TypeID
		dflt          ir.Value
	}{
		{"a list", "type/ints-described", "type/i32s", big},
		{"an enum", "type/ab", "type/bc", stringValue("a")},
	} {
		for i := range 2 {
			reg := meetRegistry()
			stubbed, recorded := stubMerger(reg)
			decls := declsOf(tc.valued, tc.other)
			decls[0].property.Default = &tc.dflt
			if i == 1 {
				decls = declsOf(tc.other, tc.valued)
				decls[1].property.Default = &tc.dflt
			}

			got := stubbed.fold(decls)

			assert.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%s (order %d)", tc.name, i)
			assert.Nil(t, got.Default, "%s (order %d)", tc.name, i)
			require.Len(t, *recorded, 1, "%s (order %d)", tc.name, i)
			assert.Equal(t, declaredAt(i), (*recorded)[0].Provenance.Pointer, "%s (order %d)", tc.name, i)
		}
	}
}

// TestFold_AnAliasMeetsAsTheNodeItStandsFor pins GitHub #1672 in both orders: a
// field declared as an alias of an enum or a list and as a node it meets stands
// on the node they meet in, built from the node the alias stands for, holding
// what the alias adds: its bounds and its docs. Two aliases of one node meet in
// an alias of it holding what each adds. Nothing is kept or reported.
func TestFold_AnAliasMeetsAsTheNodeItStandsFor(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	alias := func(id, base ir.TypeID, docs string, constraints *ir.Constraints) {
		reg[id] = &ir.Scalar{ID: id, Provenance: ptrAt("/components/schemas/" + jsontext.Pointer(strings.TrimPrefix(string(id), "type/"))),
			Docs: ir.Docs{Description: docs}, Base: &ir.TypeRef{Target: base}, Constraints: constraints}
	}
	alias("type/ab-alias", "type/ab", "alias", nil)
	alias("type/ints-alias", "type/ints-described", "", &ir.Constraints{MaxItems: new(int64(3))})
	alias("type/As-min", "type/As", "", &ir.Constraints{MinItems: new(int64(1))})
	alias("type/As-max", "type/As", "", &ir.Constraints{MaxItems: new(int64(3))})
	for _, tc := range []struct {
		name  string
		a, b  ir.TypeID
		check func(t *testing.T, td ir.TypeDef, label string)
	}{
		{"an alias of an enum", "type/ab-alias", "type/bc", func(t *testing.T, td ir.TypeDef, label string) {
			enum, ok := td.(*ir.Enum)
			require.True(t, ok, label)
			assert.Equal(t, []ir.Value{stringValue("b")}, valuesOf(enum), label)
			assert.Equal(t, "alias", enum.Docs.Description, label)
		}},
		{"an alias of a list", "type/ints-alias", "type/i32s", func(t *testing.T, td ir.TypeDef, label string) {
			list, ok := td.(*ir.List)
			require.True(t, ok, label)
			assert.Equal(t, primitiveRef(ir.PrimitiveInt32), list.ElementType, label)
			assert.Equal(t, "d", list.Docs.Description, "%s: the docs the alias reads from its node", label)
			assert.Equal(t, &ir.Constraints{MaxItems: new(int64(3))}, list.Constraints, label)
		}},
		{"two aliases of one list", "type/As-min", "type/As-max", func(t *testing.T, td ir.TypeDef, label string) {
			scalar, ok := td.(*ir.Scalar)
			require.True(t, ok, "%s: an alias of the list they share", label)
			assert.Equal(t, ir.TypeRef{Target: "type/As"}, *scalar.Base, label)
			assert.Equal(t, &ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(3))}, scalar.Constraints, label)
		}},
	} {
		for _, order := range [][]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			stubbed, recorded := stubMerger(maps.Clone(reg))
			label := fmt.Sprintf("%s: %v", tc.name, order)

			got := stubbed.fold(declsOf(order...))

			require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, label)
			tc.check(t, stubbed.Reg[got.Type.Target], label)
			assert.Empty(t, *recorded, label)
			assert.Empty(t, got.Unmodeled, label)
		}
	}
}

// TestFold_AnAliasIsWithinTheNodeItStandsFor pins an alias against the node it
// stands for, in either order: the alias says all the node does and may say more,
// so the field takes it, with nothing minted, kept or reported.
func TestFold_AnAliasIsWithinTheNodeItStandsFor(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	reg["type/ab-alias"] = &ir.Scalar{ID: "type/ab-alias", Base: &ir.TypeRef{Target: "type/ab"}}
	for _, order := range [][]ir.TypeID{{"type/ab-alias", "type/ab"}, {"type/ab", "type/ab-alias"}} {
		stubbed, recorded := stubMerger(maps.Clone(reg))

		got := stubbed.fold(declsOf(order...))

		assert.Equal(t, ir.TypeID("type/ab-alias"), got.Type.Target, "%v", order)
		assert.Zero(t, stubbed.walk.minted, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
		assert.Empty(t, got.Unmodeled, "%v", order)
	}
}
