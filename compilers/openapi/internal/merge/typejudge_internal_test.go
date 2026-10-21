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
	reg["t/enum-int"] = &ir.Enum{ID: "t/enum-int", ValueType: ir.PrimInteger}
	reg["t/enum-str"] = &ir.Enum{ID: "t/enum-str", ValueType: ir.PrimString}
	reg["t/lit-a"] = &ir.Literal{ID: "t/lit-a", Value: str("a")}
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
	reg["t/list-str"] = &ir.List{ID: "t/list-str", Elem: primRef(ir.PrimString)}
	reg["t/list-list-int"] = &ir.List{ID: "t/list-list-int", Elem: ir.TypeRef{Target: "t/list-int"}}
	reg["t/list-list-i32"] = &ir.List{ID: "t/list-list-i32", Elem: ir.TypeRef{Target: "t/list-i32"}}
	reg["t/map-int"] = &ir.MapT{ID: "t/map-int", Value: integer}
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
		{"a model against a bare kind", "t/model", prim(ir.PrimInteger), relUnrelated},
		{"two literals", "t/lit-a", "t/lit-x", relUnrelated},
		{"a narrower element", "t/list-int-null", "t/list-i32", relBNarrower},
		{"a narrower element, the other way", "t/list-i32", "t/list-int-null", relANarrower},
		{"a narrower element that admits null the broader does not", "t/list-int", "t/list-i32-null", relOverlap},
		{"one element admitting null and the other not", "t/list-i32-null", "t/list-i32", relBNarrower},
		{"a loser-only constraint", "t/list-int-min", "t/list-i32", relOverlap},
		{"a winner-only constraint", "t/list-int", "t/list-i32-min", relBNarrower},
		{"the same constraint on both", "t/list-int-min", "t/list-i32-min", relBNarrower},
		{"two lists of one element", "t/list-i32", "t/list-i32-twin", relSame},
		{"two lists of one element, one constrained", "t/list-i32", "t/list-i32-min", relOverlap},
		{"disjoint elements overlap in the empty list", "t/list-str", "t/list-i32", relOverlap},
		{"disjoint elements where one list is never empty", "t/list-str", "t/list-int-min", relDisjoint},
		{"nested lists", "t/list-list-int", "t/list-list-i32", relBNarrower},
		{"a list against a map", "t/list-int", "t/map-int", relUnrelated},
		{"a map value", "t/map-int", "t/map-i32", relBNarrower},
		{"maps with different keys", "t/map-i32", "t/map-i32-keyed", relUnrelated},
		{"nesting past the cap", "t/cycle", "t/cycle-b", relUnrelated},
	} {
		want := tc.want
		assert.Equal(t, want, j.relate(ref(tc.a), ref(tc.b)), tc.name)
		assert.Equal(t, want.flipped(), j.relate(ref(tc.b), ref(tc.a)), "%s, swapped", tc.name)
	}

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
	assert.False(t, j.admits(ref("t/cycle"), list(list(list(list(list(list(list(list(list(list())))))))))),
		"a list nested past the cap is refused")
}

func TestRelation_DroppedAndFlipped(t *testing.T) {
	t.Parallel()
	for _, r := range []relation{relUnrelated, relDisjoint, relOverlap} {
		assert.True(t, r.dropped())
		assert.Equal(t, r, r.flipped())
	}
	for _, r := range []relation{relSame, relANarrower, relBNarrower} {
		assert.False(t, r.dropped())
	}
	assert.Equal(t, relSame, relSame.flipped())
	assert.Equal(t, relBNarrower, relANarrower.flipped())
	assert.Equal(t, relANarrower, relBNarrower.flipped())
}
