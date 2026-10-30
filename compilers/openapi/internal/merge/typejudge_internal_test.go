// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

func stubResolve(reg map[ir.TypeID]ir.TypeDef) func(ir.TypeID) (ir.TypeDef, bool) {
	return func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
}

// judgeRegistry is narrowingRegistry plus one node of every kind the arms
// compare against a bare primitive, and the container pairs.
func judgeRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	i32, integer := primRef(ir.PrimInt32), primRef(ir.PrimInteger)
	nullable := func(r ir.TypeRef) ir.TypeRef { r.Nullable = true; return r }
	minItems := int64(1)
	zero := int64(0)
	reg["t/enum-int"] = &ir.Enum{ID: "t/enum-int", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("1")}}}
	reg["t/enum-big"] = &ir.Enum{ID: "t/enum-big", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("5000000000")}}}
	reg["t/enum-none"] = &ir.Enum{ID: "t/enum-none", ValueType: ir.PrimInteger}
	reg["t/enum-bytes"] = &ir.Enum{ID: "t/enum-bytes", ValueType: ir.PrimBytes, Members: []ir.EnumMember{{Value: ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}}}}
	reg["t/enum-str"] = &ir.Enum{ID: "t/enum-str", ValueType: ir.PrimString, Members: []ir.EnumMember{{Value: str("a")}}}
	reg["t/lit-a"] = &ir.Literal{ID: "t/lit-a", Value: str("a")}
	reg["t/list-enum-str-min"] = &ir.List{ID: "t/list-enum-str-min", Elem: ir.TypeRef{Target: "t/enum-str"},
		Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["t/list-enum-str"] = &ir.List{ID: "t/list-enum-str", Elem: ir.TypeRef{Target: "t/enum-str"}}
	reg["t/list-enum-int"] = &ir.List{ID: "t/list-enum-int", Elem: ir.TypeRef{Target: "t/enum-int"}}
	reg["t/lit-a-twin"] = &ir.Literal{ID: "t/lit-a-twin", Value: str("a")}
	reg["t/enum-str-twin"] = &ir.Enum{ID: "t/enum-str-twin", ValueType: ir.PrimString, Members: []ir.EnumMember{{ID: "twin/a", Value: str("a")}}}
	reg["t/scalar-str-twin"] = &ir.Scalar{ID: "t/scalar-str-twin", Base: &ir.TypeRef{Target: "t/prim/string"}}
	for _, id := range []ir.TypeID{"t/lit-a-h1", "t/lit-a-h2"} {
		reg[id] = &ir.Literal{ID: id, Anonymous: true, Value: str("a")}
	}
	reg["t/enum-str-h"] = &ir.Enum{ID: "t/enum-str-h", Anonymous: true,
		ValueType: ir.PrimString, Members: []ir.EnumMember{{ID: "h/a", Value: str("a")}}}
	reg["t/lit-x"] = &ir.Literal{ID: "t/lit-x", Value: str("x")}
	reg["t/lit-1.5"] = &ir.Literal{ID: "t/lit-1.5", Value: num("1.5")}
	reg["t/lit-null"] = &ir.Literal{ID: "t/lit-null", Value: ir.Value{Kind: ir.ValueNull}}
	reg["t/lit-1"] = &ir.Literal{ID: "t/lit-1", Value: num("1")}
	reg["t/scalar-str"] = &ir.Scalar{ID: "t/scalar-str", Base: &ir.TypeRef{Target: "t/prim/string"}}
	reg["t/scalar-int"] = &ir.Scalar{ID: "t/scalar-int", Base: &integer}
	reg["t/scalar-any"] = &ir.Scalar{ID: "t/scalar-any", Base: &ir.TypeRef{Target: "t/prim/any"}}
	reg["t/scalar-enc"] = &ir.Scalar{ID: "t/scalar-enc", Base: &integer, Encoding: &ir.Encoding{Name: "x"}}
	reg["t/scalar-opaque"] = &ir.Scalar{ID: "t/scalar-opaque"}
	reg["t/any"] = &ir.Any{ID: "t/any"}
	reg["t/model"] = &ir.Model{ID: "t/model"}
	reg["t/list-int-null"] = &ir.List{ID: "t/list-int-null", Elem: nullable(integer)}
	reg["t/list-int"] = &ir.List{ID: "t/list-int", Elem: integer}
	reg["t/list-i32"] = &ir.List{ID: "t/list-i32", Elem: i32}
	reg["t/list-i32-null"] = &ir.List{ID: "t/list-i32-null", Elem: nullable(i32)}
	reg["t/list-i32-min"] = &ir.List{ID: "t/list-i32-min", Elem: i32, Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["t/list-int-min"] = &ir.List{ID: "t/list-int-min", Elem: integer, Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["t/list-i32-twin"] = &ir.List{ID: "t/list-i32-twin", Elem: i32}
	for _, id := range []ir.TypeID{"t/list-i32-h1", "t/list-i32-h2"} {
		reg[id] = &ir.List{ID: id, Anonymous: true, Elem: i32}
	}
	reg["t/list-i32-other"] = &ir.List{ID: "t/list-i32-other", Anonymous: true, Elem: i32,
		Provenance: ir.Provenance{Source: 1}}
	reg["t/list-int-min0"] = &ir.List{ID: "t/list-int-min0", Elem: integer, Constraints: &ir.Constraints{MinItems: &zero}}
	reg["t/list-str-null"] = &ir.List{ID: "t/list-str-null", Elem: nullable(primRef(ir.PrimString))}
	reg["t/list-int-null-min"] = &ir.List{ID: "t/list-int-null-min", Elem: nullable(integer),
		Constraints: &ir.Constraints{MinItems: &minItems}}
	reg["t/list-str"] = &ir.List{ID: "t/list-str", Elem: primRef(ir.PrimString)}
	reg["t/list-list-int"] = &ir.List{ID: "t/list-list-int", Elem: ir.TypeRef{Target: "t/list-int"}}
	reg["t/list-list-i32"] = &ir.List{ID: "t/list-list-i32", Elem: ir.TypeRef{Target: "t/list-i32"}}
	reg["t/map-int"] = &ir.MapT{ID: "t/map-int", Value: integer}
	reg["t/map-int-h"] = &ir.MapT{ID: "t/map-int-h", Anonymous: true, Value: integer}
	reg["t/map-str"] = &ir.MapT{ID: "t/map-str", Value: primRef(ir.PrimString)}
	reg["t/map-int-min"] = &ir.MapT{ID: "t/map-int-min", Value: integer, Constraints: &ir.Constraints{MinProps: &minItems}}
	reg["t/map-i32"] = &ir.MapT{ID: "t/map-i32", Value: i32}
	reg["t/map-i32-keyed"] = &ir.MapT{ID: "t/map-i32-keyed", Key: &ir.TypeRef{Target: "t/prim/uuid"}, Value: i32}
	reg["t/cycle"] = &ir.List{ID: "t/cycle", Elem: ir.TypeRef{Target: "t/cycle2"}}
	reg["t/cycle2"] = &ir.List{ID: "t/cycle2", Elem: ir.TypeRef{Target: "t/cycle"}}
	reg["t/cycle-b"] = &ir.List{ID: "t/cycle-b", Elem: ir.TypeRef{Target: "t/cycle2-b"}}
	reg["t/cycle2-b"] = &ir.List{ID: "t/cycle2-b", Elem: ir.TypeRef{Target: "t/cycle-b"}}
	return reg
}

func ref(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

func TestTypeJudge_RelateArms(t *testing.T) {
	t.Parallel()
	j := typeJudge{Resolve: stubResolve(judgeRegistry())}
	prim := func(k ir.PrimKind) ir.TypeID { return primRef(k).Target }

	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		want relation
	}{
		{"one target", "t/lit-a", "t/lit-a", relSame},
		{"a dangling target", "t/absent", "t/prim/integer", relUnrelated},
		{"a dangling second", "t/prim/integer", "t/absent", relUnrelated},
		{"the top loses to a bare kind", prim(ir.PrimAny), prim(ir.PrimInt32), relBNarrower},
		{"a degraded any node is not the top", "t/model", "t/any", relUnrelated},
		{"`{}` is unrelated to a degraded any", prim(ir.PrimAny), "t/any", relUnrelated},
		{"bare narrower", prim(ir.PrimInt32), prim(ir.PrimInteger), relANarrower},
		{"bare broader", prim(ir.PrimInteger), prim(ir.PrimInt32), relBNarrower},
		{"bare overlap", prim(ir.PrimInt32), prim(ir.PrimUint32), relOverlap},
		{"bare disjoint", prim(ir.PrimString), prim(ir.PrimInteger), relDisjoint},
		{"an enum of integer against bare int32", "t/enum-int", prim(ir.PrimInt32), relOverlap},
		{"bare int32 against an enum of integer", prim(ir.PrimInt32), "t/enum-int", relOverlap},
		{"an enum no member of which the kind admits", "t/enum-big", prim(ir.PrimInt32), relDisjoint},
		{"bare int32 against an enum none of whose members it admits", prim(ir.PrimInt32), "t/enum-big", relDisjoint},
		{"an enum with no members against an overlapping kind", "t/enum-none", prim(ir.PrimInt32), relDisjoint},
		{"an enum against a kind with no judge", "t/enum-str", prim(ir.PrimBytes), relOverlap},
		{"an enum of bytes against a format no member meets", "t/enum-bytes", prim(ir.PrimUUID), relDisjoint},
		{"an enum of string over bare string", "t/enum-str", prim(ir.PrimString), relANarrower},
		{"bare string under an enum of string", prim(ir.PrimString), "t/enum-str", relBNarrower},
		{"an enum against another family", "t/enum-str", prim(ir.PrimInteger), relDisjoint},
		{"a scalar over its own kind", "t/scalar-str", prim(ir.PrimString), relANarrower},
		{"a scalar over a narrower kind", "t/scalar-int", prim(ir.PrimNumber), relANarrower},
		{"a scalar over a broader kind", "t/scalar-int", prim(ir.PrimInt32), relOverlap},
		{"an alias over the top type", "t/scalar-any", prim(ir.PrimInt32), relOverlap},
		{"an opaque scalar", "t/scalar-opaque", prim(ir.PrimInt32), relUnrelated},
		{"a literal the kind admits", "t/lit-1", prim(ir.PrimInteger), relANarrower},
		{"a literal string against integer", "t/lit-a", prim(ir.PrimInteger), relDisjoint},
		{"a literal 1.5 against integer", "t/lit-1.5", prim(ir.PrimInteger), relDisjoint},
		{"a literal against a kind with no judge", "t/lit-x", prim(ir.PrimBytes), relOverlap},
		{"a null literal against a bare integer", "t/lit-null", prim(ir.PrimInteger), relDisjoint},
		{"a model against a bare kind", "t/model", prim(ir.PrimInteger), relDisjoint},
		{"the top type spelled as an alias against a model", "t/scalar-any", "t/model", relUnrelated},
		{"two literals", "t/lit-a", "t/lit-x", relOverlap},
		{"two identical hoisted literals", "t/lit-a-h1", "t/lit-a-h2", relSame},
		{"a named enum against a hoisted copy", "t/enum-str", "t/enum-str-h", relANarrower},
		{"two identical named literals", "t/lit-a", "t/lit-a-twin", relOverlap},
		{"two identical named enums", "t/enum-str", "t/enum-str-twin", relOverlap},
		{"two identical named scalars", "t/scalar-str", "t/scalar-str-twin", relOverlap},
		{"a literal against an enum", "t/lit-a", "t/enum-str", relOverlap},
		{"a scalar against an enum", "t/scalar-str", "t/enum-str", relOverlap},
		{"a scalar against a literal", "t/scalar-str", "t/lit-a", relOverlap},
		{"a narrower element", "t/list-int-null", "t/list-i32", relBNarrower},
		{"a narrower element, the other way", "t/list-i32", "t/list-int-null", relANarrower},
		{"a narrower element that admits null the broader does not", "t/list-int", "t/list-i32-null", relOverlap},
		{"one element admitting null and the other not", "t/list-i32-null", "t/list-i32", relBNarrower},
		{"a loser-only constraint", "t/list-int-min", "t/list-i32", relOverlap},
		{"a winner-only constraint", "t/list-int", "t/list-i32-min", relBNarrower},
		{"the same constraint on both", "t/list-int-min", "t/list-i32-min", relBNarrower},
		{"two lists alike, declared apart", "t/list-i32", "t/list-i32-twin", relOverlap},
		{"two hoisted copies of one list", "t/list-i32-h1", "t/list-i32-h2", relSame},
		{"a list declared apart against a hoisted copy", "t/list-i32", "t/list-i32-h1", relANarrower},
		{"a map declared apart against a hoisted copy", "t/map-int", "t/map-int-h", relANarrower},
		{"a list another document hoists at the declaration's pointer", "t/list-i32-other", "t/list-i32-h1", relANarrower},
		{"two lists of one element, one constrained", "t/list-i32", "t/list-i32-min", relOverlap},
		{"disjoint elements overlap in the empty list", "t/list-str", "t/list-i32", relOverlap},
		{"disjoint elements where one list is never empty", "t/list-str", "t/list-int-min", relDisjoint},
		{"a minItems of zero admits the empty list", "t/list-str", "t/list-int-min0", relOverlap},
		{"disjoint values where one map is never empty", "t/map-str", "t/map-int-min", relDisjoint},
		{"disjoint elements that both admit null overlap", "t/list-str-null", "t/list-int-null-min", relOverlap},
		{"disjoint elements where only one admits null", "t/list-str-null", "t/list-int-min", relDisjoint},
		{"nested lists", "t/list-list-int", "t/list-list-i32", relBNarrower},
		{"a list against a map", "t/list-int", "t/map-int", relDisjoint},
		{"enum members sharing no value, one list never empty", "t/list-enum-str-min", "t/list-enum-int", relDisjoint},
		{"enum members sharing no value, both lists admitting the empty one", "t/list-enum-str", "t/list-enum-int", relOverlap},
		{"a map value", "t/map-int", "t/map-i32", relBNarrower},
		{"maps with different keys", "t/map-i32", "t/map-i32-keyed", relUnrelated},
		{"nesting past the cap", "t/cycle", "t/cycle-b", relUnrelated},
	} {
		want := tc.want
		assert.Equal(t, want, j.relate(ref(tc.a), ref(tc.b)), tc.name)
		assert.Equal(t, want.flipped(), j.relate(ref(tc.b), ref(tc.a)), "%s, swapped", tc.name)
	}

	nullTwin := ref("t/lit-a-h2")
	nullTwin.Nullable = true
	assert.Equal(t, relBNarrower, j.relate(nullTwin, ref("t/lit-a-h1")), "identical hoisted nodes order by null")

	nullInt := primRef(ir.PrimInteger)
	nullInt.Nullable = true
	assert.Equal(t, relANarrower, j.relate(ref("t/lit-null"), nullInt), "a null literal against a nullable integer")
	assert.Equal(t, relBNarrower, j.relate(nullInt, ref("t/lit-null")), "a nullable integer against a null literal")
}

func TestTypeJudge_AdmitsPerNode(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	reg["t/enum-1"] = &ir.Enum{ID: "t/enum-1", ValueType: ir.PrimInteger,
		Members: []ir.EnumMember{{Value: num("1")}, {Value: num("2")}}}
	reg["t/list-1"] = &ir.List{ID: "t/list-1", Elem: ref("t/enum-1")}
	reg["t/map-1"] = &ir.MapT{ID: "t/map-1", Value: ref("t/enum-1")}
	short, two := int64(3), int64(2)
	reg["t/scalar-short"] = &ir.Scalar{ID: "t/scalar-short", Base: &ir.TypeRef{Target: "t/prim/string"},
		Constraints: &ir.Constraints{MaxLength: &short}}
	reg["t/list-pair"] = &ir.List{ID: "t/list-pair", Elem: ref("t/enum-1"), Constraints: &ir.Constraints{MaxItems: &two}}
	reg["t/map-pair"] = &ir.MapT{ID: "t/map-pair", Value: ref("t/enum-1"), Constraints: &ir.Constraints{MaxProps: &two}}
	j := typeJudge{Resolve: stubResolve(reg)}
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	obj := func(v ir.Value) ir.Value {
		return ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "k", Value: v}}}
	}

	assert.True(t, j.admits(ref("t/enum-1"), num("1.0")), "an enum member by magnitude")
	assert.False(t, j.admits(ref("t/enum-1"), num("3")))
	assert.True(t, j.admits(ref("t/lit-a"), str("a")))
	assert.False(t, j.admits(ref("t/lit-a"), str("b")))
	assert.True(t, j.admits(ref("t/scalar-int"), num("3")))
	assert.False(t, j.admits(ref("t/scalar-enc"), num("3")), "an encoded scalar admits nothing")
	assert.False(t, j.admits(ref("t/scalar-opaque"), num("3")), "an opaque scalar admits nothing")
	assert.True(t, j.admits(ref("t/any"), str("a")))
	assert.False(t, j.admits(ref("t/model"), str("a")))
	assert.False(t, j.admits(ref("t/absent"), str("a")))
	assert.True(t, j.admits(ref("t/list-1"), list(num("1"), num("2"))))
	assert.False(t, j.admits(ref("t/list-1"), list(num("1"), num("3"))))
	assert.False(t, j.admits(ref("t/list-1"), str("a")))
	assert.True(t, j.admits(ref("t/map-1"), obj(num("2"))))
	assert.False(t, j.admits(ref("t/map-1"), obj(num("3"))))
	assert.False(t, j.admits(ref("t/map-1"), list(num("1"))))
	assert.True(t, j.admits(ref("t/scalar-short"), str("abc")), "a scalar's constraints admit what meets them")
	assert.False(t, j.admits(ref("t/scalar-short"), str("abcd")), "and refuse what does not")
	assert.False(t, j.admits(ref("t/list-pair"), list(num("1"), num("2"), num("1"))), "a list's constraints are judged")
	pairs := ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: num("1")}, {Name: "b", Value: num("1")},
		{Name: "c", Value: num("2")}}}
	assert.False(t, j.admits(ref("t/map-pair"), pairs), "and so are a map's")
	assert.False(t, j.admits(ref("t/cycle"), list(list(list(list(list(list(list(list(list(list())))))))))),
		"a list nested past the cap is refused")
}

func TestRelation_Flipped(t *testing.T) {
	t.Parallel()
	for _, r := range []relation{relUnrelated, relDisjoint, relOverlap, relSame} {
		assert.Equal(t, r, r.flipped())
	}
	assert.Equal(t, relBNarrower, relANarrower.flipped())
	assert.Equal(t, relANarrower, relBNarrower.flipped())
}

func TestTypeJudge_NullIsJudgedByNullability(t *testing.T) {
	t.Parallel()
	null := ir.Value{Kind: ir.ValueNull}
	j := typeJudge{Resolve: stubResolve(narrowingRegistry())}
	nullable := func(k ir.PrimKind) ir.TypeRef { r := primRef(k); r.Nullable = true; return r }

	assert.True(t, j.admits(nullable(ir.PrimInteger), null))
	assert.False(t, j.admits(primRef(ir.PrimInteger), null))
	assert.False(t, j.admits(nullable(ir.PrimInteger), str("x")))
	assert.True(t, j.admits(primRef(ir.PrimInteger), num("1")))
	assert.True(t, j.admits(primRef(ir.PrimAny), null), "the top type admits null whatever the bit says")
}

// TestClassify_ComparesResolvedKinds pins the last-resort comparison classify
// makes when neither side ends in a primitive kind. Two composites of the same
// kind are not provably contradictory; two of different kinds are disjoint.
func TestClassify_ComparesResolvedKinds(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model":  &ir.Model{ID: "t/model"},
		"t/other":  &ir.Model{ID: "t/other"},
		"t/union":  &ir.Union{ID: "t/union"},
		"t/opaque": &ir.Scalar{ID: "t/opaque"},
	})

	assert.Equal(t, relUnrelated, judge(g).relate(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/other"}),
		"two models are the same kind, so not provably contradictory")
	assert.Equal(t, relDisjoint, judge(g).relate(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/union"}),
		"a model and a union are different kinds")
	assert.Equal(t, relDisjoint, judge(g).relate(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/union"}),
		"and the comparison is reached when neither side is a primitive")
}

// TestTypeJudge_UnrelatedPrimitivesAreDisjoint pins the pairs the relation
// calls disjoint, and the enum against a bare kind of another family.
func TestTypeJudge_UnrelatedPrimitivesAreDisjoint(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	reg["t/enm"] = &ir.Enum{ID: "t/enm", ValueType: ir.PrimString}
	g, _ := stubMerger(reg)

	assert.Equal(t, relDisjoint, judge(g).relate(primRef(ir.PrimUUID), primRef(ir.PrimDatetimeOffset)))
	assert.Equal(t, relDisjoint, judge(g).relate(ir.TypeRef{Target: "t/enm"}, primRef(ir.PrimInteger)))
	assert.Equal(t, relOverlap, judge(g).relate(primRef(ir.PrimInt32), primRef(ir.PrimUint32)),
		"overlapping kinds are no conflict")
}

// TestSameValue_ComparesJSONValues pins the value equality the fold and the
// enum judgment share: one JSON value spelled two ways is one, and a value
// with no JSON instance, which canonicalValue cannot spell, compares deeply.
func TestSameValue_ComparesJSONValues(t *testing.T) {
	t.Parallel()
	obj := func(fs ...ir.Field) ir.Value { return ir.Value{Kind: ir.ValueObject, Object: fs} }
	sym := func(s string) ir.Value { return ir.Value{Kind: ir.ValueSymbol, Str: s} }
	for _, tc := range []struct {
		name string
		a, b ir.Value
		same bool
	}{
		{"one number, two spellings", num("1"), num("1.0"), true},
		{"two numbers", num("1"), num("2"), false},
		{"one object, two member orders", obj(ir.Field{Name: "a", Value: num("1")}, ir.Field{Name: "b", Value: str("x")}),
			obj(ir.Field{Name: "b", Value: str("x")}, ir.Field{Name: "a", Value: num("1e0")}), true},
		{"a number and a string", num("1"), str("1"), false},
		{"one symbol, compared deeply", sym("s"), sym("s"), true},
		{"two symbols, compared deeply", sym("s"), sym("t"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.same, sameValue(tc.a, tc.b))
			assert.Equal(t, tc.same, sameValue(tc.b, tc.a))
		})
	}
}

// TestTypeJudge_ComparesReferentsNotReferences pins that the judgment reads
// the nodes two references name, not the references.
func TestTypeJudge_ComparesReferentsNotReferences(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/a":   &ir.Primitive{ID: "t/a", Prim: ir.PrimString},
		"t/b":   &ir.Primitive{ID: "t/b", Prim: ir.PrimString},
		"t/i":   &ir.Primitive{ID: "t/i", Prim: ir.PrimInt32},
		"t/any": &ir.Any{ID: "t/any"},
		"t/m":   &ir.Model{ID: "t/m"},
	})
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

	assert.Equal(t, relSame, judge(g).relate(ref("t/a"), ref("t/b")),
		"two distinct nodes of one primitive kind are compared by kind")
	assert.Equal(t, relDisjoint, judge(g).relate(ref("t/a"), ref("t/i")),
		"string and integer cannot both be satisfied")
	assert.Equal(t, relUnrelated, judge(g).relate(ref("t/any"), ref("t/i")),
		"a degraded node stands for a declaration the judgment cannot see")
	assert.Equal(t, relDisjoint, judge(g).relate(ref("t/m"), ref("t/a")),
		"a structural type and a primitive are different kinds")
}
