// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

var strKey = strT

// shapesDoc is valueDoc holding the types a row of these tests names its
// positions' types by, one of each shape the checks tell apart.
func shapesDoc(props ...ir.Property) *ir.Document {
	doc := modelOf(props...)
	for _, kind := range []ir.PrimKind{ir.PrimBytes, ir.PrimFloat64} {
		id := ir.PrimTypeID(kind)
		doc.Types[id] = &ir.Primitive{ID: id, Prim: kind}
	}
	for _, td := range []ir.TypeDef{
		&ir.List{ID: "t/list", Elem: strT},
		&ir.Tuple{ID: "t/tuple", Elems: []ir.TypeRef{strT, strT}},
		&ir.MapT{ID: "t/map", Key: &strKey, Value: strT},
		&ir.Model{ID: "t/obj"},
		&ir.Any{ID: "t/any"},
		&ir.Scalar{ID: "t/alias", Base: &ir.TypeRef{Target: "t/list"}},
		&ir.Scalar{ID: "t/loop-a", Base: &ir.TypeRef{Target: "t/loop-b"}},
		&ir.Scalar{ID: "t/loop-b", Base: &ir.TypeRef{Target: "t/loop-a"}},
		&ir.Enum{ID: "t/mixed", ValueType: ir.PrimString, Members: []ir.EnumMember{
			{ID: "e/null", Value: *null()}, {ID: "e/flag", Value: ir.Value{Kind: ir.ValueBool, Bool: true}},
			{ID: "e/symbol", Value: ir.Value{Kind: ir.ValueSymbol, Str: "sym"}},
			{ID: "e/bytes", Value: ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ab")}},
			{ID: "e/list", Value: ir.Value{Kind: ir.ValueList}}, {ID: "e/bad", Value: *num("x")},
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
	members := &ir.Constraints{MaxProps: new(int64(1))}
	for _, tc := range []struct {
		name string
		typ  ir.TypeRef
		c    *ir.Constraints
		want int
	}{
		{"a tuple", ir.TypeRef{Target: "t/tuple"}, items, 1},
		{"a list through an alias", ir.TypeRef{Target: "t/alias"}, items, 1},
		{"a map", ir.TypeRef{Target: "t/map"}, members, 1},
		{"a model", ir.TypeRef{Target: "t/obj"}, members, 1},
		{"bytes", ir.TypeRef{Target: ir.PrimTypeID(ir.PrimBytes)}, lengths(2, 1), 1},
		{"a float", ir.TypeRef{Target: ir.PrimTypeID(ir.PrimFloat64)}, numeric("3", "", "2", ""), 1},
		{"anything", ir.TypeRef{Target: "t/any"}, items, 0},
		{"a chain of aliases that never ends", ir.TypeRef{Target: "t/loop-a"}, items, 0},
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
			doc := shapesDoc(prop("p/a/x", tc.typ, bounded(tc.c)))
			atLeast := &ir.Constraints{MinItems: new(int64(2)), MinProps: new(int64(2))}
			doc.Types["t/list"].(*ir.List).Constraints = atLeast
			doc.Types["t/tuple"].(*ir.Tuple).Constraints = atLeast
			doc.Types["t/map"].(*ir.MapT).Constraints = atLeast
			doc.Types["t/obj"].(*ir.Model).Constraints = atLeast
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
		"a symbol":     {Kind: ir.ValueSymbol, Str: "s"},
		"a number":     num("1"),
		"bytes":        {Kind: ir.ValueBytes, Bytes: []byte("a")},
		"a list":       {Kind: ir.ValueList},
		"an object":    {Kind: ir.ValueObject},
		"an infinity":  {Kind: ir.ValueInf},
		"an infinity ": {Kind: ir.ValueNegInf},
		"a NaN":        {Kind: ir.ValueNaN},
		"a thing":      {Kind: "thing"},
	}
	holds := map[string]ir.TypeRef{
		"a bool": boolT, "a string": strT, "bytes": {Target: ir.PrimTypeID(ir.PrimBytes)},
		"a number": {Target: ir.PrimTypeID(ir.PrimFloat64)}, "an integer": intT,
		"a list": {Target: "t/list"}, "an object": {Target: "t/obj"},
	}
	for kindWords, v := range kinds {
		for shapeWords, typ := range holds {
			t.Run(kindWords+" for "+shapeWords, func(t *testing.T) {
				t.Parallel()
				got := found(t, shapesDoc(prop("p/a/x", typ, defaulted(v))), defaultCode)
				if kindWords == shapeWords || (kindWords == "a string" && shapeWords == "bytes") ||
					(kindWords == "a symbol" && shapeWords == "a string") || (shapeWords == "a number" && v.Kind != ir.ValueNumber &&
					(v.Kind == ir.ValueInf || v.Kind == ir.ValueNegInf || v.Kind == ir.ValueNaN)) ||
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
	bytes := ir.TypeRef{Target: ir.PrimTypeID(ir.PrimBytes)}
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
		{"an object over its maximum", ir.TypeRef{Target: "t/obj"}, obj, &ir.Constraints{MaxProps: new(int64(1))}, 1},
		{"an object within it", ir.TypeRef{Target: "t/obj"}, obj, &ir.Constraints{MaxProps: new(int64(2))}, 0},
		{"a number over its maximum", intT, num("5"), numeric("", "", "4", ""), 1},
		{"a number at an exclusive maximum", intT, num("4"), numeric("", "", "", "4"), 1},
		{"a number at its maximum", intT, num("4"), numeric("", "", "4", ""), 0},
		{"an infinity under bounds on numbers", ir.TypeRef{Target: ir.PrimTypeID(ir.PrimFloat64)},
			&ir.Value{Kind: ir.ValueInf}, numeric("", "", "4", ""), 0},
		{"a number that does not parse", intT, num("x"), numeric("", "", "4", ""), 0},
		{"a bool under bounds", boolT, &ir.Value{Kind: ir.ValueBool}, lengths(0, 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := found(t, shapesDoc(prop("p/a/x", tc.typ, func(p *ir.Property) { p.Default = tc.def; p.Constraints = tc.c })), defaultCode)
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
	enum := ir.TypeRef{Target: "t/mixed"}
	for name, tc := range map[string]struct {
		def  *ir.Value
		want int
	}{
		"null":                     {null(), 0},
		"true":                     {&ir.Value{Kind: ir.ValueBool, Bool: true}, 0},
		"false":                    {&ir.Value{Kind: ir.ValueBool}, 1},
		"a symbol written as text": {text("sym"), 0},
		"text written as a symbol": {&ir.Value{Kind: ir.ValueSymbol, Str: "other"}, 1},
		"the same bytes":           {&ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ab")}, 0},
		"other bytes":              {&ir.Value{Kind: ir.ValueBytes, Bytes: []byte("ac")}, 1},
		"any list":                 {&ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("x")}}, 0},
		"a number beside one that does not parse": {num("1"), 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := found(t, shapesDoc(prop("p/a/x", enum, defaulted(tc.def))), defaultCode)
			assert.Len(t, got, tc.want, "%v", got)
		})
	}
}

// TestValidate_ATypeOnlyAKnotNeedsHasNoFiniteValueOfItsOwn holds a type that
// needs only a type of a knot to none, however long the chain that leads into
// it: each is dropped once nothing unfounded needs it, so the finding is at the
// knot alone. A scalar with no base ends a chain of aliases unjudged.
func TestValidate_ATypeOnlyAKnotNeedsHasNoFiniteValueOfItsOwn(t *testing.T) {
	t.Parallel()
	doc := knotDoc(
		composed("t/n", nil, needs("p/n/0", ref("t/n"))),
		composed("t/m", nil, needs("p/m/0", ref("t/n"))),
		composed("t/h", nil, needs("p/h/0", ref("t/m")), needs("p/h/1", strT)),
	)

	assert.Equal(t, []finding{{"t/n", "no finite value: each needs its required property 0, t/n, which has none either"}},
		found(t, doc, finiteCode))

	bare := shapesDoc(prop("p/a/x", ir.TypeRef{Target: "t/bare"}, bounded(lengths(2, 1))))
	bare.Types["t/bare"] = &ir.Scalar{ID: "t/bare"}
	assert.Empty(t, found(t, bare, boundsCode))
}
