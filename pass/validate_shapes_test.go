// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

var stringKey = stringT

// shapesDoc is valueDoc holding the types a row of these tests names its
// positions' types by, one of each shape the checks tell apart.
func shapesDoc(properties ...ir.Property) *ir.Document {
	doc := modelOf(properties...)
	for _, kind := range []ir.PrimitiveKind{ir.PrimitiveBytes, ir.PrimitiveFloat64} {
		id := ir.PrimitiveTypeID(kind)
		doc.Types[id] = &ir.Primitive{ID: id, PrimitiveKind: kind}
	}
	for _, td := range []ir.TypeDef{
		&ir.List{ID: "type/list", ElementType: stringT},
		&ir.Tuple{ID: "type/tuple", ElementTypes: []ir.TypeRef{stringT, stringT}},
		&ir.MapT{ID: "type/map", Key: &stringKey, Value: stringT},
		&ir.Model{ID: "type/obj"},
		&ir.Any{ID: "type/any"},
		&ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/list"}},
		&ir.Scalar{ID: "type/loop-a", Base: &ir.TypeRef{Target: "type/loop-b"}},
		&ir.Scalar{ID: "type/loop-b", Base: &ir.TypeRef{Target: "type/loop-a"}},
		&ir.Enum{ID: "type/mixed", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{
			{ID: "enum-member/null", Value: *null()}, {ID: "enum-member/flag", Value: ir.Value{Kind: ir.ValueBool, Bool: true}},
			{ID: "enum-member/symbol", Value: ir.Value{Kind: ir.ValueSymbol, String: "sym"}},
			{ID: "enum-member/bytes", Value: ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ab")}},
			{ID: "enum-member/list", Value: ir.Value{Kind: ir.ValueList}}, {ID: "enum-member/bad", Value: *number("x")},
		}},
	} {
		doc.Types[td.Common().ID] = td
	}
	return doc
}

// TestValidate_BoundsAreJoinedAcrossTheShapesAChainEnds holds each type a chain
// of aliases can end at to the bounds it takes, and a bound that does not parse
// and a chain that never ends to no finding: a tuple, a map, a model and a
// list read the bounds of their own, here a minimum the position's maximum
// contradicts, an alias of one reads them as well, and the rest are left to
// irverify or to the types that hold them. Of two ends of one value, the
// exclusive one is kept.
func TestValidate_BoundsAreJoinedAcrossTheShapesAChainEnds(t *testing.T) {
	t.Parallel()
	items := &ir.Constraints{MaxItems: new(int64(1))}
	members := &ir.Constraints{MaxProperties: new(int64(1))}
	for _, tc := range []struct {
		name string
		typ  ir.TypeRef
		c    *ir.Constraints
		want int
	}{
		{"a tuple", ir.TypeRef{Target: "type/tuple"}, items, 1},
		{"a list through an alias", ir.TypeRef{Target: "type/alias"}, items, 1},
		{"a map", ir.TypeRef{Target: "type/map"}, members, 1},
		{"a model", ir.TypeRef{Target: "type/obj"}, members, 1},
		{"bytes", ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveBytes)}, lengths(2, 1), 1},
		{"a float", ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveFloat64)}, numeric("3", "", "2", ""), 1},
		{"anything", ir.TypeRef{Target: "type/any"}, items, 0},
		{"a chain of aliases that never ends", ir.TypeRef{Target: "type/loop-a"}, items, 0},
		{"a minimum that does not parse", intT, numeric("x", "", "2", ""), 0},
		{"a maximum that does not parse", intT, numeric("3", "", "x", ""), 0},
		{"a maximum above a minimum", intT, numeric("1", "", "2", ""), 0},
		{"two minimums, the greater kept", intT, &ir.Constraints{Min: big("3"), ExclusiveMin: big("1"), Max: big("2")}, 1},
		{"two maximums, the lesser kept", intT, &ir.Constraints{Min: big("3"), Max: big("9"), ExclusiveMax: big("2")}, 1},
		{"a minimum and an exclusive one at it", intT, &ir.Constraints{Min: big("5"), ExclusiveMin: big("5"), Max: big("5")}, 1},
		{"a maximum and an exclusive one at it", intT, &ir.Constraints{Min: big("5"), Max: big("5"), ExclusiveMax: big("5")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := shapesDoc(property("property/a/x", tc.typ, bounded(tc.c)))
			atLeast := &ir.Constraints{MinItems: new(int64(2)), MinProperties: new(int64(2))}
			doc.Types["type/list"].(*ir.List).Constraints = atLeast
			doc.Types["type/tuple"].(*ir.Tuple).Constraints = atLeast
			doc.Types["type/map"].(*ir.MapT).Constraints = atLeast
			doc.Types["type/obj"].(*ir.Model).Constraints = atLeast
			got := found(t, doc, boundsCode)
			assert.Len(t, got, tc.want, "%v", got)
		})
	}
}

// TestValidate_ADefaultIsHeldToTheShapeOfItsType holds a default of each kind to
// each shape of type, with the words each side is told in.
func TestValidate_ADefaultIsHeldToTheShapeOfItsType(t *testing.T) {
	t.Parallel()
	kinds := map[string]*ir.Value{
		"a bool":       {Kind: ir.ValueBool},
		"a string":     text("s"),
		"a symbol":     {Kind: ir.ValueSymbol, String: "s"},
		"a number":     number("1"),
		"bytes":        {Kind: ir.ValueBytes, Bytes: []byte("a")},
		"a list":       {Kind: ir.ValueList},
		"an object":    {Kind: ir.ValueObject},
		"an infinity":  {Kind: ir.ValueInfinity},
		"an infinity ": {Kind: ir.ValueNegativeInfinity},
		"a NaN":        {Kind: ir.ValueNaN},
		"a thing":      {Kind: "thing"},
	}
	holds := map[string]ir.TypeRef{
		"a bool": boolT, "a string": stringT, "bytes": {Target: ir.PrimitiveTypeID(ir.PrimitiveBytes)},
		"a number": {Target: ir.PrimitiveTypeID(ir.PrimitiveFloat64)}, "an integer": intT,
		"a list": {Target: "type/list"}, "an object": {Target: "type/obj"},
	}
	for kindWords, value := range kinds {
		for shapeWords, typ := range holds {
			t.Run(kindWords+" for "+shapeWords, func(t *testing.T) {
				t.Parallel()
				got := found(t, shapesDoc(property("property/a/x", typ, defaulted(value))), defaultCode)
				if kindWords == shapeWords || (kindWords == "a string" && shapeWords == "bytes") ||
					(kindWords == "a symbol" && shapeWords == "a string") || (shapeWords == "a number" && value.Kind != ir.ValueNumber &&
					(value.Kind == ir.ValueInfinity || value.Kind == ir.ValueNegativeInfinity || value.Kind == ir.ValueNaN)) ||
					(kindWords == "a number" && shapeWords == "an integer") {
					assert.Empty(t, got)
					return
				}
				assert.Len(t, got, 1)
			})
		}
	}
}

// TestValidate_ADefaultIsHeldToTheBoundsOfItsShape holds a default of a count
// of bytes, members or items and a number to the bounds its position declares
// at either end, and leaves a value the bounds do not apply to alone.
func TestValidate_ADefaultIsHeldToTheBoundsOfItsShape(t *testing.T) {
	t.Parallel()
	bytes := ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveBytes)}
	obj := &ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: *text("x")}, {Name: "b", Value: *text("y")}}}
	for _, tc := range []struct {
		name string
		typ  ir.TypeRef
		def  *ir.Value
		c    *ir.Constraints
		want int
	}{
		{"bytes over their maximum", bytes, &ir.Value{Kind: ir.ValueBytes, Bytes: []byte("abc")}, lengths(0, 2), 1},
		{"bytes within it", bytes, &ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ab")}, lengths(0, 2), 0},
		{"a string given for bytes, not measured", bytes, text("abcdef"), lengths(0, 2), 0},
		{"an object over its maximum", ir.TypeRef{Target: "type/obj"}, obj, &ir.Constraints{MaxProperties: new(int64(1))}, 1},
		{"an object within it", ir.TypeRef{Target: "type/obj"}, obj, &ir.Constraints{MaxProperties: new(int64(2))}, 0},
		{"a number over its maximum", intT, number("5"), numeric("", "", "4", ""), 1},
		{"a number at an exclusive maximum", intT, number("4"), numeric("", "", "", "4"), 1},
		{"a number at its maximum", intT, number("4"), numeric("", "", "4", ""), 0},
		{"an infinity under bounds on numbers", ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveFloat64)},
			&ir.Value{Kind: ir.ValueInfinity}, numeric("", "", "4", ""), 0},
		{"a number that does not parse", intT, number("x"), numeric("", "", "4", ""), 0},
		{"a bool under bounds", boolT, &ir.Value{Kind: ir.ValueBool}, lengths(0, 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := found(t, shapesDoc(property("property/a/x", tc.typ, func(property *ir.Property) { property.Default = tc.def; property.Constraints = tc.c })), defaultCode)
			assert.Len(t, got, tc.want, "%v", got)
		})
	}
}

// TestValidate_ADefaultIsHeldToTheMembersOfAnEnumAsTheyAreWritten holds a
// default to an enum whose members are of each kind: a symbol is a string, a
// pair of bytes the same by content, a null a member of its own, and what a
// source cannot compare any further, a list or a number that does not parse,
// is not judged.
func TestValidate_ADefaultIsHeldToTheMembersOfAnEnumAsTheyAreWritten(t *testing.T) {
	t.Parallel()
	enum := ir.TypeRef{Target: "type/mixed"}
	for name, tc := range map[string]struct {
		def  *ir.Value
		want int
	}{
		"null":                     {null(), 0},
		"true":                     {&ir.Value{Kind: ir.ValueBool, Bool: true}, 0},
		"false":                    {&ir.Value{Kind: ir.ValueBool}, 1},
		"a symbol written as text": {text("sym"), 0},
		"text written as a symbol": {&ir.Value{Kind: ir.ValueSymbol, String: "other"}, 1},
		"the same bytes":           {&ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ab")}, 0},
		"other bytes":              {&ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ac")}, 1},
		"any list":                 {&ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("x")}}, 0},
		"a number beside one that does not parse": {number("1"), 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := found(t, shapesDoc(property("property/a/x", enum, defaulted(tc.def))), defaultCode)
			assert.Len(t, got, tc.want, "%v", got)
		})
	}
}

// TestValidate_ATypeOnlyATypeCycleNeedsHasNoFiniteValueOfItsOwn holds a type
// that needs only a type of a type cycle to none, however long the chain that
// leads into it: each is dropped once nothing unfounded needs it, so the
// finding is at the type cycle alone. A scalar with no base ends a chain of
// aliases unjudged.
func TestValidate_ATypeOnlyATypeCycleNeedsHasNoFiniteValueOfItsOwn(t *testing.T) {
	t.Parallel()
	doc := noFiniteValueDoc(
		composed("type/n", nil, needs("property/n/0", ref("type/n"))),
		composed("type/m", nil, needs("property/m/0", ref("type/n"))),
		composed("type/h", nil, needs("property/h/0", ref("type/m")), needs("property/h/1", stringT)),
	)

	assert.Equal(t, []finding{{"type/n", "no finite value: each needs its required property 0, type/n, which has none either"}},
		found(t, doc, finiteCode))

	bare := shapesDoc(property("property/a/x", ir.TypeRef{Target: "type/bare"}, bounded(lengths(2, 1))))
	bare.Types["type/bare"] = &ir.Scalar{ID: "type/bare"}
	assert.Empty(t, found(t, bare, boundsCode))
}
