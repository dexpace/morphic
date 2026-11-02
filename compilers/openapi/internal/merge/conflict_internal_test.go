// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// The redeclaration-conflict helpers carry defensive guards for states a
// well-formed lowered document never reaches: a reference into no interned type,
// a base-less opaque scalar, and a cyclic base chain. These exercise those
// guards directly.

func TestPrimKind_DanglingTargetIsNotResolved(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	_, ok := judge(g).primKind(ir.TypeRef{Target: "t/missing"})
	assert.False(t, ok, "a target absent from the registry resolves to no primitive")
}

func TestPrimKind_BaselessScalarIsNotResolved(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/opaque": &ir.Scalar{ID: "t/opaque"},
	})
	_, ok := judge(g).primKind(ir.TypeRef{Target: "t/opaque"})
	assert.False(t, ok, "a base-less opaque scalar has no underlying primitive")
}

func TestPrimKind_CyclicBaseChainTerminates(t *testing.T) {
	t.Parallel()
	// A scalar whose Base points back at itself: the bounded walk must terminate
	// and report no primitive rather than spin.
	self := ir.TypeRef{Target: "t/cycle"}
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/cycle": &ir.Scalar{ID: "t/cycle", Base: &self},
	})
	_, ok := judge(g).primKind(self)
	assert.False(t, ok, "a cyclic base chain terminates without resolving")
}

func TestTypeJudge_IsAnyTypeDanglingTargetIsNotAny(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	assert.False(t, judge(g).isAnyType(ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not classified as the top type")
}

func TestClassify_AnUnresolvableTargetIsUnrelated(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model": &ir.Model{ID: "t/model"},
	})
	assert.Equal(t, relUnrelated, judge(g).relate(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not judged disjoint from anything")
}

// TestClassify_MapAndModelAreOneObjectKind pins that a map and a model are no
// differing kinds: both are objects, so a redeclaration naming one beside the
// other is satisfiable. Kinds that are different shapes are disjoint, a map
// against a list included.
func TestClassify_MapAndModelAreOneObjectKind(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/map":   &ir.MapT{ID: "t/map"},
		"t/model": declaring("t/model"),
		"t/list":  &ir.List{ID: "t/list"},
	})
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

	assert.Equal(t, relUnrelated, judge(g).relate(ref("t/map"), ref("t/model")), "map against model")
	assert.Equal(t, relUnrelated, judge(g).relate(ref("t/model"), ref("t/map")), "model against map")
	assert.Equal(t, relDisjoint, judge(g).relate(ref("t/map"), ref("t/list")), "map against list")
	assert.Equal(t, relDisjoint, judge(g).relate(ref("t/list"), ref("t/model")), "list against model")
}

// Every BigVal bound rests on one magnitude comparison, driven here over two
// values that differ, mostly at a magnitude math/big will not build as a rational
// at all. The larger and the smaller are asserted in both orders, so a comparison
// that only took the first would be caught.
//
// Every literal is built through ir.NewBigVal, the only way a bound reaches these
// helpers, so no row holds a pair that could not arrive.
func TestLargerAndSmallerNumber_OrderMagnitudesAtAnyScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		a, b            string
		larger, smaller string
	}{
		{name: "one exponent apart", a: "1e1000001", b: "1e1000002", larger: "1e1000002", smaller: "1e1000001"},
		{name: "one leading digit apart", a: "1e1000001", b: "2e1000001", larger: "2e1000001", smaller: "1e1000001"},
		{name: "a sign apart", a: "1e1000001", b: "-1e1000001", larger: "1e1000001", smaller: "-1e1000001"},
		{name: "too small for a rational", a: "1e-1000001", b: "2e-1000001", larger: "2e-1000001", smaller: "1e-1000001"},
		{name: "in a rational's own range", a: "3", b: "20", larger: "20", smaller: "3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := ir.NewBigVal(tc.a)
			require.NoError(t, err, "%q is a literal a schema may write", tc.a)
			b, err := ir.NewBigVal(tc.b)
			require.NoError(t, err, "%q is a literal a schema may write", tc.b)

			for _, pair := range [][2]ir.BigVal{{a, b}, {b, a}} {
				larger, ok := largerNumber(pair[0], pair[1])
				require.True(t, ok, "%s and %s have an order", pair[0], pair[1])
				assert.Equal(t, ir.BigVal(tc.larger), larger, "larger of %s and %s", pair[0], pair[1])

				smaller, ok := smallerNumber(pair[0], pair[1])
				require.True(t, ok, "%s and %s have an order", pair[0], pair[1])
				assert.Equal(t, ir.BigVal(tc.smaller), smaller, "smaller of %s and %s", pair[0], pair[1])
			}
		})
	}
}

// TestLargerAndSmallerNumber_KeepTheFirstSpellingOfOneNumber pins that one number
// written twice meets to the first spelling, as a default written twice does: no
// magnitude is decided by which came first.
func TestLargerAndSmallerNumber_KeepTheFirstSpellingOfOneNumber(t *testing.T) {
	t.Parallel()
	for _, meet := range []func(x, y ir.BigVal) (ir.BigVal, bool){largerNumber, smallerNumber} {
		got, ok := meet("10", "1e1")
		require.True(t, ok)
		assert.Equal(t, ir.BigVal("10"), got)
		got, ok = meet("1e1", "10")
		require.True(t, ok)
		assert.Equal(t, ir.BigVal("1e1"), got)
	}
}

func TestIsStructural_DistinguishesCompositeFromOpaque(t *testing.T) {
	t.Parallel()
	assert.True(t, isStructural(&ir.Model{ID: "t/model"}), "model is a structural type")
	assert.True(t, isStructural(&ir.List{ID: "t/list"}), "list is a structural type")
	assert.False(t, isStructural(&ir.Scalar{ID: "t/opaque"}), "base-less opaque scalar is not structural")
	assert.False(t, isStructural(&ir.Primitive{ID: "t/string", Prim: ir.PrimString}), "primitive is not structural")
}

func TestClassify_OpaqueScalarVsPrimitiveIsUnrelated(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/opaque": &ir.Scalar{ID: "t/opaque"},
		"t/string": &ir.Primitive{ID: "t/string", Prim: ir.PrimString},
	})

	assert.Equal(t, relUnrelated, judge(g).relate(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/string"}),
		"opaque scalar vs primitive is not flagged (never guess)")
	assert.Equal(t, relUnrelated, judge(g).relate(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/opaque"}),
		"primitive vs opaque scalar is not flagged (never guess)")
}

func TestClassify_StructuralVsPrimitiveIsDisjoint(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model":  &ir.Model{ID: "t/model"},
		"t/string": &ir.Primitive{ID: "t/string", Prim: ir.PrimString},
	})

	assert.Equal(t, relDisjoint, judge(g).relate(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/string"}),
		"model vs primitive is a provable conflict")
	assert.Equal(t, relDisjoint, judge(g).relate(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/model"}),
		"primitive vs model is a provable conflict")
}

// The tests below reach constraintsConflict and Constraints for keywords
// no OpenAPI spec can drive them through. ir.Constraints is one struct
// shared by every user of the type (properties and lists alike), and the
// OpenAPI compiler does not yet populate Precision, Scale, PatternMessage, or
// the list-owned MinItems/MaxItems/UniqueItems trio from any wire keyword —
// so no YAML spec can drive a redeclaration through those fields. The merge
// and conflict logic must still be correct for them.

func TestConstraintsConflict_PatternMessageConflictReported(t *testing.T) {
	t.Parallel()
	a := &ir.Constraints{PatternMessage: "must be alphanumeric"}
	b := &ir.Constraints{PatternMessage: "must match the legacy format"}
	_, detail, ok := constraintsConflict(a, b)
	require.True(t, ok, "differing patternMessage is a conflict")
	assert.Contains(t, detail, "patternMessage")
	assert.Contains(t, detail, "must be alphanumeric")
	assert.Contains(t, detail, "must match the legacy format")
}

func TestConstraintsConflict_UniqueItemsNeverConflicts(t *testing.T) {
	t.Parallel()
	// UniqueItems is a bare bool: false cannot be distinguished from "not set",
	// so a differing UniqueItems must never be reported as a conflict in
	// either direction.
	cases := []struct {
		name string
		a, b bool
	}{
		{"true vs false", true, false},
		{"false vs true", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, ok := constraintsConflict(&ir.Constraints{UniqueItems: tc.a}, &ir.Constraints{UniqueItems: tc.b})
			assert.False(t, ok, "differing uniqueItems must not be reported as a conflict")
		})
	}
}

func TestConstraints_UniqueItemsIntersectsToTrue(t *testing.T) {
	t.Parallel()
	// allOf is an intersection: a collection satisfying both branches must be
	// unique whenever either branch requires it, so the merge always adopts
	// true and never downgrades true back to false.
	dst := Constraints(&ir.Constraints{UniqueItems: false}, &ir.Constraints{UniqueItems: true})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst adopts true from src")

	dst = Constraints(&ir.Constraints{UniqueItems: true}, &ir.Constraints{UniqueItems: false})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst is never downgraded from true to false")
}

func TestConstraints_AdoptsEveryUnsetKeyword(t *testing.T) {
	t.Parallel()
	// Every keyword ir.Constraints carries: dst leaves each one unset, so the
	// merge must adopt all of them from src, not just the ones exercised by
	// the spec-driven table tests in the compiler package.
	five, ten := int64(5), int64(10)
	minVal, maxVal, multipleOf := ir.BigVal("1"), ir.BigVal("9"), ir.BigVal("2")
	exclMin, exclMax := ir.BigVal("0"), ir.BigVal("10")
	src := &ir.Constraints{
		Min: &minVal, ExclusiveMin: &exclMin,
		Max: &maxVal, ExclusiveMax: &exclMax,
		MultipleOf:     &multipleOf,
		Precision:      &ten,
		Scale:          &five,
		MinLength:      &five,
		MaxLength:      &ten,
		Pattern:        "^a$",
		PatternMessage: "must start with a",
		MinItems:       &five,
		MaxItems:       &ten,
		UniqueItems:    true,
		MinProps:       &five,
		MaxProps:       &ten,
	}
	merged := Constraints(&ir.Constraints{}, src)
	require.NotNil(t, merged)
	assert.Same(t, src.Min, merged.Min)
	assert.Same(t, src.ExclusiveMin, merged.ExclusiveMin)
	assert.Same(t, src.Max, merged.Max)
	assert.Same(t, src.ExclusiveMax, merged.ExclusiveMax)
	assert.Same(t, src.MultipleOf, merged.MultipleOf)
	assert.Same(t, src.Precision, merged.Precision)
	assert.Same(t, src.Scale, merged.Scale)
	assert.Same(t, src.MinLength, merged.MinLength)
	assert.Same(t, src.MaxLength, merged.MaxLength)
	assert.Equal(t, src.Pattern, merged.Pattern)
	assert.Equal(t, src.PatternMessage, merged.PatternMessage)
	assert.Same(t, src.MinItems, merged.MinItems)
	assert.Same(t, src.MaxItems, merged.MaxItems)
	assert.Equal(t, src.UniqueItems, merged.UniqueItems)
	assert.Same(t, src.MinProps, merged.MinProps)
	assert.Same(t, src.MaxProps, merged.MaxProps)
}

func TestConstraints_ABoundTakesItsTighterValue(t *testing.T) {
	t.Parallel()
	// Under allOf every bound holds, so each keyword both sides set takes the
	// value admitting fewer, whichever side wrote it. Both orders are folded,
	// so a merge keeping one side would fail one of them.
	loose := &ir.Constraints{
		Min: new(ir.BigVal("1")), ExclusiveMin: new(ir.BigVal("2")), Max: new(ir.BigVal("9")), ExclusiveMax: new(ir.BigVal("8")),
		MinLength: new(int64(3)), MaxLength: new(int64(7)), MinItems: new(int64(3)), MaxItems: new(int64(7)),
		MinProps: new(int64(3)), MaxProps: new(int64(7)), MultipleOf: new(ir.BigVal("2")),
	}
	tight := &ir.Constraints{
		Min: new(ir.BigVal("4")), ExclusiveMin: new(ir.BigVal("5")), Max: new(ir.BigVal("6")), ExclusiveMax: new(ir.BigVal("5.5")),
		MinLength: new(int64(4)), MaxLength: new(int64(6)), MinItems: new(int64(4)), MaxItems: new(int64(6)),
		MinProps: new(int64(4)), MaxProps: new(int64(6)), MultipleOf: new(ir.BigVal("6")),
	}
	for name, pair := range map[string][2]*ir.Constraints{"looser first": {loose, tight}, "tighter first": {tight, loose}} {
		first := *pair[0]
		assert.Equal(t, tight, Constraints(&first, pair[1]), name)
	}
}

func TestConstraints_AKeywordWithNoOrderKeepsTheFirst(t *testing.T) {
	t.Parallel()
	a := &ir.Constraints{Pattern: "^a$", PatternMessage: "a", Precision: new(int64(4)), Scale: new(int64(2))}
	b := &ir.Constraints{Pattern: "^b$", PatternMessage: "b", Precision: new(int64(9)), Scale: new(int64(1))}
	merged := *a
	assert.Equal(t, a, Constraints(&merged, b))
}

func TestConstraints_APairWithNoMeetKeepsTheFirst(t *testing.T) {
	t.Parallel()
	// A pair no one value states is reported by constraintsConflict, and the
	// field keeps the first of it: a failed meet writes nothing, since what it
	// returns is no value.
	a := &ir.Constraints{MultipleOf: new(ir.BigVal("3")), Min: new(ir.BigVal("1"))}
	b := &ir.Constraints{MultipleOf: new(ir.BigVal("nan")), Min: new(ir.BigVal("nan"))}
	merged := *a
	assert.Equal(t, a, Constraints(&merged, b))
}

func TestConstraints_NilOperands(t *testing.T) {
	t.Parallel()
	// A nil dst adopts src wholesale; a nil src leaves dst untouched; both nil
	// stays nil.
	full := &ir.Constraints{Pattern: "^a$"}
	assert.Same(t, full, Constraints(nil, full), "nil dst adopts src wholesale")
	assert.Same(t, full, Constraints(full, nil), "nil src leaves dst untouched")
	assert.Nil(t, Constraints(nil, nil))
}

func TestConstraintsConflict_DeterministicKeywordOrder(t *testing.T) {
	t.Parallel()
	// precision is checked before pattern in constraintsConflict's fixed order,
	// so when both conflict at once the reported keyword is always precision,
	// never pattern.
	a := &ir.Constraints{Precision: new(int64(2)), Pattern: "^a$"}
	b := &ir.Constraints{Precision: new(int64(3)), Pattern: "^b$"}
	_, detail, ok := constraintsConflict(a, b)
	require.True(t, ok)
	assert.Contains(t, detail, "precision", "the earlier-checked keyword is reported")
	assert.NotContains(t, detail, "pattern", "the later-checked keyword is not also reported")
}

// TestClassify_ALiteralOverlapsWhatHoldsValues pins that a literal against
// another literal or against a type with a primitive kind is an overlap, never
// silent, while a scalar literal against a structure still conflicts and a
// dangling target stays silent.
func TestClassify_ALiteralOverlapsWhatHoldsValues(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(judgeRegistry())

	assert.Equal(t, relOverlap, judge(g).relate(ref("t/lit-a"), ref("t/lit-x")))
	assert.Equal(t, relOverlap, judge(g).relate(ref("t/lit-1"), ref("t/enum-int")))
	assert.Equal(t, relOverlap, judge(g).relate(ref("t/scalar-str"), ref("t/lit-a")))
	assert.Equal(t, relDisjoint, judge(g).relate(ref("t/lit-a"), ref("t/model")))
	assert.Equal(t, relUnrelated, judge(g).relate(ref("t/absent"), ref("t/prim/string")))
	assert.Equal(t, relUnrelated, judge(g).relate(ref("t/prim/string"), ref("t/absent")))
}

// TestMeets_ANoOrderPairIsNoMeet pins what Meets asks of constraintsConflict: two
// ordered bounds always meet, and two values of a keyword with no order meet only
// where they are one.
func TestMeets_ANoOrderPairIsNoMeet(t *testing.T) {
	t.Parallel()
	assert.True(t, Meets(&ir.Constraints{MaxLength: new(int64(3))}, &ir.Constraints{MaxLength: new(int64(5))}))
	assert.True(t, Meets(&ir.Constraints{Pattern: "a"}, &ir.Constraints{Pattern: "a"}))
	assert.False(t, Meets(&ir.Constraints{Pattern: "a"}, &ir.Constraints{Pattern: "b"}))
	assert.True(t, Meets(nil, &ir.Constraints{Pattern: "b"}), "nothing conflicts with nothing")
}
