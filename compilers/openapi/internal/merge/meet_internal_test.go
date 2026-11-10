// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
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
			out[i] = ir.EnumMember{ID: ir.EnumMemberID("e/anon" + string(declaredAt(n)) + "/s:" + v), Value: str(v)}
		}
		return out
	}
	enum := func(id ir.TypeID, n int, values ...string) {
		reg[id] = &ir.Enum{ID: id, Anonymous: true, Provenance: at(n),
			ValueType: ir.PrimString, Members: members(n, values...)}
	}
	enum("t/ab", 0, "a", "b")
	enum("t/a", 1, "a")
	enum("t/bc", 1, "b", "c")
	enum("t/c", 1, "c")
	enum("t/ac", 1, "a", "c")
	enum("t/bc2", 2, "b", "c")
	list := func(id ir.TypeID, n int, elem ir.TypeRef, c ir.TypeCommon) {
		c.ID, c.Anonymous, c.Provenance = id, true, at(n)
		reg[id] = &ir.List{TypeCommon: c, Elem: elem}
	}
	described := func(d string) ir.TypeCommon { return ir.TypeCommon{Docs: ir.Docs{Description: d}} }
	list("t/ints-described", 0, primRef(ir.PrimInteger), described("d"))
	list("t/i32s", 1, primRef(ir.PrimInt32), ir.TypeCommon{})
	list("t/anys-described", 0, primRef(ir.PrimAny), described("d"))
	list("t/strs", 1, primRef(ir.PrimString), ir.TypeCommon{})
	list("t/bools", 2, primRef(ir.PrimBool), ir.TypeCommon{})
	list("t/i32s-one", 1, primRef(ir.PrimInt32), described("one"))
	list("t/i32s-other", 2, primRef(ir.PrimInt32), described("other"))
	reg["t/A"] = &ir.Model{ID: "t/A", Provenance: ptrAt("/components/schemas/A"),
		Properties: []ir.Property{{WireName: "a"}}}
	reg["t/B"] = &ir.Model{ID: "t/B", Provenance: ptrAt("/components/schemas/B"),
		Properties: []ir.Property{{WireName: "b"}}}
	list("t/As", 0, ref("t/A"), ir.TypeCommon{})
	list("t/Bs", 1, ref("t/B"), ir.TypeCommon{})
	reg["t/A-map"] = &ir.MapT{ID: "t/A-map", Anonymous: true, Provenance: at(0), Value: ref("t/A")}
	reg["t/B-map"] = &ir.MapT{ID: "t/B-map", Anonymous: true, Provenance: at(1), Value: ref("t/B")}
	reg["t/A-tuple"] = &ir.Tuple{ID: "t/A-tuple", Anonymous: true, Provenance: at(0),
		Elems: []ir.TypeRef{ref("t/A")}}
	reg["t/B-tuple"] = &ir.Tuple{ID: "t/B-tuple", Anonymous: true, Provenance: at(1),
		Elems: []ir.TypeRef{ref("t/B")}}
	reg["t/time-described"] = &ir.Scalar{ID: "t/time-described", Anonymous: true,
		Provenance: at(0), Docs: ir.Docs{Description: "d"}, Base: &ir.TypeRef{Target: "t/prim/string"},
		Encoding: &ir.Encoding{Name: "time"}}
	reg["t/noon"] = &ir.Literal{ID: "t/noon", Anonymous: true, Provenance: at(1),
		Value: str("12:00:00Z"), ValueType: ir.PrimString}
	return reg
}

// foldOf folds a field declared as each of targets, in order, and returns the
// field, the registry and what the fold reported.
func foldOf(targets ...ir.TypeID) (ir.Property, map[ir.TypeID]ir.TypeDef, []ir.Diagnostic) {
	reg := meetRegistry()
	g, recorded := stubMerger(reg)
	return g.fold(declsOf(targets...)), reg, *recorded
}

// TestFold_EnumsAreJudgedByTheValuesTheyShare pins GitHub #1042's enums in both
// orders: the one whose values the other holds every one of is the field's
// type, with nothing minted, kept or reported; two sharing only some meet in an
// enum of those, minted at the field with its members beneath it; and two
// sharing none are disjoint, which warns.
func TestFold_EnumsAreJudgedByTheValuesTheyShare(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"t/ab", "t/a"}, {"t/a", "t/ab"}} {
		got, _, recorded := foldOf(order...)
		assert.Equal(t, ir.TypeID("t/a"), got.Type.Target, "%v", order)
		assert.Empty(t, recorded, "%v", order)
		assert.Empty(t, got.Unmodeled, "%v", order)
	}

	got, reg, recorded := foldOf("t/ab", "t/bc")
	require.Equal(t, composedAt(declaredAt(1)), got.Type.Target)
	e, ok := reg[got.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, e.Members, 1)
	assert.Equal(t, str("b"), e.Members[0].Value)
	assert.Empty(t, recorded)
	assert.Len(t, reg["t/ab"].(*ir.Enum).Members, 2, "the declared enum is as it was written")

	got, _, recorded = foldOf("t/ab", "t/c")
	assert.Equal(t, ir.TypeID("t/ab"), got.Type.Target)
	require.Len(t, recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, recorded[0].Code)
}

// TestFold_ANarrowerListAdoptsTheBroadersDetail pins GitHub #1042's list in
// both orders: a list of int32 beside a described list of integer is a list of
// int32 holding the description, minted at the field, with nothing reported.
func TestFold_ANarrowerListAdoptsTheBroadersDetail(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"t/ints-described", "t/i32s"}, {"t/i32s", "t/ints-described"}} {
		got, reg, recorded := foldOf(order...)
		require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%v", order)
		l, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, primRef(ir.PrimInt32), l.Elem, "%v", order)
		assert.Equal(t, "d", l.Docs.Description, "%v", order)
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
	for _, order := range [][]ir.TypeID{{"t/ints-encoded", "t/i32s"}, {"t/i32s", "t/ints-encoded"}} {
		reg := meetRegistry()
		reg["t/ints-encoded"] = &ir.List{ID: "t/ints-encoded", Anonymous: true,
			Provenance: ptrAt(declaredAt(0)), Elem: primRef(ir.PrimInteger), Encoding: &ir.Encoding{Name: "csv"}}
		g, recorded := stubMerger(reg)

		got := g.fold(declsOf(order...))

		l, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, primRef(ir.PrimInt32), l.Elem, "%v", order)
		assert.Equal(t, &ir.Encoding{Name: "csv"}, l.Encoding, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
	}

	reg := meetRegistry()
	encoded := func(id ir.TypeID, n int, elem ir.PrimKind, enc *ir.Encoding, c *ir.Constraints) {
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			Elem: primRef(elem), Encoding: enc, Constraints: c}
	}
	encoded("t/ints-min", 0, ir.PrimInteger, nil, &ir.Constraints{MinItems: new(int64(1))})
	encoded("t/i32s-csv", 1, ir.PrimInt32, &ir.Encoding{Name: "csv"}, nil)
	encoded("t/i32s-tsv", 2, ir.PrimInt32, &ir.Encoding{Name: "tsv"}, nil)
	g, recorded := stubMerger(reg)

	got := g.fold(declsOf("t/ints-min", "t/i32s-csv", "t/i32s-tsv"))

	l, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Encoding{Name: "csv"}, l.Encoding, "the first encoding given is kept")
	require.Len(t, *recorded, 1, "and the other reported")
	assert.Equal(t, declaredAt(2), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
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
		{"t/As", "t/Bs", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.List).Elem }, "/items"},
		{"t/A-map", "t/B-map", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.MapT).Value }, "/additionalProperties"},
		{"t/A-tuple", "t/B-tuple", func(td ir.TypeDef) ir.TypeRef { return td.(*ir.Tuple).Elems[0] }, "/prefixItems/0"},
	} {
		for _, order := range [][]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			got, reg, recorded := foldOf(order...)
			require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%v", order)
			member := tc.member(reg[got.Type.Target])
			require.Equal(t, composedAt(declaredAt(1)+tc.at), member.Target, "%v", order)
			m, ok := reg[member.Target].(*ir.Model)
			require.True(t, ok, "%v", order)
			assert.ElementsMatch(t, []ir.TypeID{"t/A", "t/B"}, targetsOf(m.Mixins), "%v", order)
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
	for _, order := range [][]ir.TypeID{{"t/time-described", "t/noon"}, {"t/noon", "t/time-described"}} {
		got, reg, recorded := foldOf(order...)
		lit, ok := reg[got.Type.Target].(*ir.Literal)
		require.True(t, ok, "%v", order)
		assert.Equal(t, str("12:00:00Z"), lit.Value, "%v", order)
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
	got, reg, recorded := foldOf("t/anys-described", "t/strs", "t/bools")
	count := len(meetRegistry())

	assert.Equal(t, ir.TypeID("t/anys-described"), got.Type.Target)
	assert.Len(t, reg, count, "nothing is minted")
	assert.Len(t, recorded, 2)
	assert.ElementsMatch(t, []string{losingDeclarationKey + string(declaredAt(1)), losingDeclarationKey + string(declaredAt(2))},
		slices.Collect(maps.Keys(got.Unmodeled)))
}

// TestFold_ADetailTwoMeetingDeclarationsGiveDifferentlyIsKept pins a detail two
// declarations meeting the winner each add, differently: the minted node takes
// the first's, and the second is reported and kept, its narrower element still
// adopted.
func TestFold_ADetailTwoMeetingDeclarationsGiveDifferentlyIsKept(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	reg["t/ints-min"] = &ir.List{ID: "t/ints-min", Anonymous: true, Provenance: ptrAt(declaredAt(0)),
		Elem: primRef(ir.PrimInteger), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	g, recorded := stubMerger(reg)

	got := g.fold(declsOf("t/ints-min", "t/i32s-one", "t/i32s-other"))

	l, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, "one", l.Docs.Description)
	assert.Equal(t, primRef(ir.PrimInt32), l.Elem)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(1))}, l.Constraints)
	require.Len(t, *recorded, 1)
	assert.Equal(t, declaredAt(2), (*recorded)[0].Provenance.Pointer)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
}

// TestFold_EnumsSharingNoValueTogetherKeepTheWinner pins three enums each
// meeting the others in a value no third holds: no value is in all three, so no
// enum holds them, and the field keeps the first, each other reported and kept.
func TestFold_EnumsSharingNoValueTogetherKeepTheWinner(t *testing.T) {
	t.Parallel()
	got, reg, recorded := foldOf("t/ab", "t/ac", "t/bc2")

	assert.Equal(t, ir.TypeID("t/ab"), got.Type.Target)
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
	list := func(id ir.TypeID, n int, elem ir.TypeRef, c *ir.Constraints) {
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n) + "/prefixItems/0"),
			Elem: elem, Constraints: c}
	}
	list("t/l0", 0, primRef(ir.PrimAny), &ir.Constraints{Pattern: "^a$"})
	list("t/l1", 1, primRef(ir.PrimString), &ir.Constraints{Pattern: "^b$"})
	list("t/l2", 2, primRef(ir.PrimAny), &ir.Constraints{MaxItems: new(int64(3))})
	tuple := func(id ir.TypeID, n int, first ir.TypeID, second ir.TypeRef) {
		reg[id] = &ir.Tuple{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			Elems: []ir.TypeRef{ref(first), second}}
	}
	tuple("t/t0", 0, "t/l0", primRef(ir.PrimAny))
	tuple("t/t1", 1, "t/l1", primRef(ir.PrimString))
	tuple("t/t2", 2, "t/l2", primRef(ir.PrimBool))
	g, recorded := stubMerger(reg)

	got := g.fold(declsOf("t/t0", "t/t1", "t/t2"))

	assert.Equal(t, ir.TypeID("t/t0"), got.Type.Target)
	require.Len(t, *recorded, 2, "one report per dropped declaration")
	for _, d := range *recorded {
		assert.Equal(t, diag.DegradedConstruct, d.Code)
	}
}

// TestFold_AnEnumWithinAnotherAdoptsItsDetail pins GitHub #1042's enum with the
// broader side's detail in both orders: the field is an enum of the narrower's
// values, minted holding the broader's description.
func TestFold_AnEnumWithinAnotherAdoptsItsDetail(t *testing.T) {
	t.Parallel()
	for _, order := range [][]ir.TypeID{{"t/ab-described", "t/a"}, {"t/a", "t/ab-described"}} {
		reg := meetRegistry()
		ab := *reg["t/ab"].(*ir.Enum)
		ab.ID, ab.Docs = "t/ab-described", ir.Docs{Description: "d"}
		reg["t/ab-described"] = &ab
		g, recorded := stubMerger(reg)

		got := g.fold(declsOf(order...))

		e, ok := reg[got.Type.Target].(*ir.Enum)
		require.True(t, ok, "%v", order)
		require.Len(t, e.Members, 1, "%v", order)
		assert.Equal(t, str("a"), e.Members[0].Value, "%v", order)
		assert.Equal(t, "d", e.Docs.Description, "%v", order)
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
	for _, order := range [][]ir.TypeID{{"t/ints-kept", "t/i32s"}, {"t/i32s", "t/ints-kept"}} {
		reg := meetRegistry()
		reg["t/ints-kept"] = &ir.List{ID: "t/ints-kept", Anonymous: true,
			Provenance: ptrAt(declaredAt(0)), Unmodeled: kept("1"), Elem: primRef(ir.PrimInteger)}
		g, recorded := stubMerger(reg)

		got := g.fold(declsOf(order...))

		l, ok := reg[got.Type.Target].(*ir.List)
		require.True(t, ok, "%v", order)
		assert.Equal(t, kept("1"), l.Unmodeled, "%v", order)
		assert.Empty(t, *recorded, "%v", order)
	}

	reg := meetRegistry()
	reg["t/ints-min"] = &ir.List{ID: "t/ints-min", Anonymous: true, Provenance: ptrAt(declaredAt(0)),
		Elem: primRef(ir.PrimInteger), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	for n, v := range []string{"1", "2"} {
		id := ir.TypeID("t/i32s-kept-" + v)
		reg[id] = &ir.List{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n + 1)),
			Unmodeled: kept(v), Elem: primRef(ir.PrimInt32)}
	}
	g, recorded := stubMerger(reg)

	got := g.fold(declsOf("t/ints-min", "t/i32s-kept-1", "t/i32s-kept-2"))

	l, ok := reg[got.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, kept("1"), l.Unmodeled)
	require.Len(t, *recorded, 1)
	assert.Equal(t, declaredAt(2), (*recorded)[0].Provenance.Pointer)
}

// TestFold_TuplesBuildWhatFollowsTheirPositions pins a tuple's rest: two tuples
// of different models with rests of different models stand on one tuple whose
// position and rest each compose both, the rest minted at its own position.
func TestFold_TuplesBuildWhatFollowsTheirPositions(t *testing.T) {
	t.Parallel()
	reg := meetRegistry()
	for n, m := range []ir.TypeID{"t/A", "t/B"} {
		id := ir.TypeID("t/" + string(m[2:]) + "-rest-tuple")
		reg[id] = &ir.Tuple{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			Elems: []ir.TypeRef{ref(m)}, Rest: new(ref(m))}
	}
	g, recorded := stubMerger(reg)

	got := g.fold(declsOf("t/A-rest-tuple", "t/B-rest-tuple"))

	tu, ok := reg[got.Type.Target].(*ir.Tuple)
	require.True(t, ok)
	require.NotNil(t, tu.Rest)
	assert.Equal(t, composedAt(declaredAt(1)+"/items"), tu.Rest.Target)
	assert.Equal(t, composedAt(declaredAt(1)+"/prefixItems/0"), tu.Elems[0].Target)
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
	reg["t/A-pair-tuple"] = &ir.Tuple{ID: "t/A-pair-tuple", Anonymous: true, Provenance: ptrAt(declaredAt(1)),
		Elems: []ir.TypeRef{ref("t/A"), ref("t/A")}}
	g, _ := stubMerger(reg)
	f := newFolding(g, declsOf("t/A", "t/B"), declaredAt(1))
	m := func(id ir.TypeID, decl int) member { return member{ref: ref(id), decl: decl} }
	for _, tc := range []struct {
		name          string
		base, partner member
	}{
		{"a partner the registry does not hold", m("t/A", 0), m("t/absent", 1)},
		{"two models", m("t/A", 0), m("t/B", 1)},
		{"tuples of different lengths", m("t/A-tuple", 0), m("t/A-pair-tuple", 1)},
		{"an enum beside a list", m("t/ab", 0), m("t/i32s", 1)},
	} {
		b := builder{f: f, dry: true}
		b.meetNode(tc.base, []member{tc.partner}, declaredAt(1), 0)
		assert.True(t, b.failed, tc.name)
	}

	j := judge(g)
	assert.Equal(t, relOverlap, j.literalBesideDetail(&ir.Literal{Value: str("a")}, reg["t/ab"]))
}
