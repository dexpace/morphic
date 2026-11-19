// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_ANarrowerPrimitiveWinsInEitherOrder pins the refinement
// rule: a redeclaration that narrows a bare primitive loses nothing, so the
// narrower target wins whichever branch is written first, with no diagnostic
// and no kept declaration.
func TestFold_ANarrowerPrimitiveWinsInEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		broad, narrow ir.PrimitiveKind
	}{
		{"number and integer", ir.PrimitiveNumber, ir.PrimitiveInteger},
		{"integer and int32", ir.PrimitiveInteger, ir.PrimitiveInt32},
		{"string and datetime_offset", ir.PrimitiveString, ir.PrimitiveDatetimeOffset},
		{"int32 narrows int64", ir.PrimitiveInt64, ir.PrimitiveInt32},
		{"url and string", ir.PrimitiveString, ir.PrimitiveURL},
		{"float64 and number", ir.PrimitiveNumber, ir.PrimitiveFloat64},
		{"uuid and string", ir.PrimitiveString, ir.PrimitiveUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var merged [2]ir.TypeRef
			for i, order := range [][2]ir.PrimitiveKind{{tc.broad, tc.narrow}, {tc.narrow, tc.broad}} {
				stubbed, recorded := stubMerger(narrowingRegistry())
				dst := ir.Property{WireName: "x", Type: primitiveRef(order[0])}
				src := ir.Property{WireName: "x", Type: primitiveRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

				dst = foldTwo(stubbed, dst, src, unread(t))

				assert.Equal(t, primitiveRef(tc.narrow).Target, dst.Type.Target, "the narrower target wins (order %d)", i)
				assert.Empty(t, *recorded, "a refinement is not a conflict (order %d)", i)
				assert.Empty(t, dst.Unmodeled, "nothing is dropped, so nothing is kept (order %d)", i)
				merged[i] = dst.Type
			}
			assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
		})
	}
}

// TestFold_AUint8NarrowsInt16InEitherOrder pins a containment the
// old chain omitted: every uint8 is an int16, so int16 declared beside uint8
// keeps uint8 whichever branch is written first, with nothing reported or kept.
func TestFold_AUint8NarrowsInt16InEitherOrder(t *testing.T) {
	t.Parallel()
	var merged [2]ir.TypeRef
	for i, order := range [][2]ir.PrimitiveKind{{ir.PrimitiveInt16, ir.PrimitiveUint8}, {ir.PrimitiveUint8, ir.PrimitiveInt16}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primitiveRef(order[0])}
		src := ir.Property{WireName: "x", Type: primitiveRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(stubbed, dst, src, unread(t))

		assert.Equal(t, primitiveRef(ir.PrimitiveUint8).Target, dst.Type.Target, "uint8 wins (order %d)", i)
		assert.Empty(t, *recorded, "a containment is not a conflict (order %d)", i)
		assert.Empty(t, dst.Unmodeled, "nothing is dropped (order %d)", i)
		merged[i] = dst.Type
	}
	assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
}

// TestFold_ANarrowingIntersectsNullability pins that the swap
// keeps the conjunction the equal-target case already states: over every pair
// of null admissions, in both orders, the merged field admits null only where
// both branches do.
func TestFold_ANarrowingIntersectsNullability(t *testing.T) {
	t.Parallel()
	for _, broadFirst := range []bool{true, false} {
		for _, broadNull := range []bool{true, false} {
			for _, narrowNull := range []bool{true, false} {
				name := fmt.Sprintf("broadFirst=%v/broadNullable=%v/narrowNullable=%v", broadFirst, broadNull, narrowNull)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					stubbed, recorded := stubMerger(narrowingRegistry())
					broad := primitiveRef(ir.PrimitiveNumber)
					broad.Nullable = broadNull
					narrow := primitiveRef(ir.PrimitiveInteger)
					narrow.Nullable = narrowNull
					dst, src := broad, narrow
					if !broadFirst {
						dst, src = narrow, broad
					}
					property := ir.Property{WireName: "x", Type: dst}

					property = foldTwo(stubbed, property, ir.Property{WireName: "x", Type: src, Provenance: ptrAt("/p")}, unread(t))

					assert.Equal(t, narrow.Target, property.Type.Target)
					assert.Equal(t, broadNull && narrowNull, property.Type.Nullable,
						"null is admitted only where both branches admit it")
					assert.Empty(t, *recorded)
				})
			}
		}
	}
}

// TestFold_ADroppedTargetKeepsTheWinnersNullAdmission pins a drop:
// an unrelated declaration's type is lost whole, so the merged field keeps the
// winner's null admission, not the conjunction a same-referent or narrowing
// pair takes.
func TestFold_ADroppedTargetKeepsTheWinnersNullAdmission(t *testing.T) {
	t.Parallel()
	for _, firstNull := range []bool{true, false} {
		t.Run(fmt.Sprintf("firstNullable=%v", firstNull), func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(narrowingRegistry())
			dst := ir.Property{WireName: "x", Type: primitiveRef(ir.PrimitiveString)}
			dst.Type.Nullable = firstNull
			src := ir.Property{WireName: "x", Type: primitiveRef(ir.PrimitiveInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			src.Type.Nullable = !firstNull

			dst = foldTwo(stubbed, dst, src, written(`{"type":"integer"}`))

			assert.Equal(t, primitiveRef(ir.PrimitiveInteger).Target, dst.Type.Target, "the later declaration keeps the shape")
			assert.Equal(t, !firstNull, dst.Type.Nullable, "a dropped type takes its null admission with it")
			require.Len(t, *recorded, 1)
			assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
		})
	}
}

// TestFold_ANarrowingAdoptsTheBroaderDeclarationsDetail pins that
// nothing is dropped, so the broader declaration's constraints and default are
// folded as for any agreeing pair.
func TestFold_ANarrowingAdoptsTheBroaderDeclarationsDetail(t *testing.T) {
	t.Parallel()
	for _, broadFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("broadFirst=%v", broadFirst), func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(narrowingRegistry())
			broad := ir.Property{
				WireName:    "x",
				Type:        primitiveRef(ir.PrimitiveNumber),
				Default:     &ir.Value{Kind: ir.ValueNumber, Number: "1"},
				Constraints: &ir.Constraints{Min: new(ir.BigVal("0"))},
				Provenance:  ptrAt("/allOf/0/properties/x"),
			}
			narrow := ir.Property{WireName: "x", Type: primitiveRef(ir.PrimitiveInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			dst, src := broad, narrow
			if !broadFirst {
				dst, src = narrow, broad
			}

			dst = foldTwo(stubbed, dst, src, unread(t))

			assert.Equal(t, primitiveRef(ir.PrimitiveInteger).Target, dst.Type.Target)
			require.NotNil(t, dst.Constraints)
			assert.Equal(t, new(ir.BigVal("0")), dst.Constraints.Min, "the broader declaration's bound lands on the merged field")
			require.NotNil(t, dst.Default)
			assert.Equal(t, ir.BigVal("1"), dst.Default.Number, "and so does its default")
			assert.Empty(t, *recorded)
		})
	}
}

// TestFold_ANarrowingIsNoDrop pins that a bare narrowing settles on one target
// in either order and is no drop, so the redeclaration is neither classified
// nor reported nor kept.
func TestFold_ANarrowingIsNoDrop(t *testing.T) {
	t.Parallel()
	for i, order := range [][2]ir.PrimitiveKind{{ir.PrimitiveInteger, ir.PrimitiveNumber}, {ir.PrimitiveNumber, ir.PrimitiveInteger}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primitiveRef(order[0])}
		src := ir.Property{WireName: "x", Type: primitiveRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		got := foldTwo(stubbed, dst, src, unread(t))

		assert.Equal(t, primitiveRef(ir.PrimitiveInteger), got.Type, "the narrower target wins (order %d)", i)
		assert.Empty(t, *recorded, "a narrowing draws no diagnostic (order %d)", i)
		assert.Empty(t, got.Unmodeled, "and keeps nothing (order %d)", i)
	}
}

// TestFold_ANarrowingCarriesTheNullMarkers pins TypeRef.Inferred through a
// narrowing: the field's null rests on a heuristic wherever either side's did
// (intersectNull), whichever side wins, except where the loser is the top
// type, whose null bit and marker say nothing, so the winner's stand alone.
func TestFold_ANarrowingCarriesTheNullMarkers(t *testing.T) {
	t.Parallel()
	nullRef := func(primitiveKind ir.PrimitiveKind, nullable bool, inferred ir.Inferred) ir.TypeRef {
		typeRef := primitiveRef(primitiveKind)
		typeRef.Nullable, typeRef.Inferred = nullable, inferred
		return typeRef
	}
	for _, tc := range []struct {
		name           string
		dst, src, want ir.TypeRef
	}{
		{"the narrower redeclaration takes the first's marker",
			nullRef(ir.PrimitiveString, true, "h"), nullRef(ir.PrimitiveURL, true, ""), nullRef(ir.PrimitiveURL, true, "h")},
		{"the narrower first declaration takes the redeclaration's marker",
			nullRef(ir.PrimitiveURL, true, ""), nullRef(ir.PrimitiveString, true, "h"), nullRef(ir.PrimitiveURL, true, "h")},
		{"both markers, the first's first",
			nullRef(ir.PrimitiveString, true, "a"), nullRef(ir.PrimitiveURL, true, "b"), nullRef(ir.PrimitiveURL, true, "a,b")},
		{"a side admitting no null drops the marker",
			nullRef(ir.PrimitiveString, true, "h"), nullRef(ir.PrimitiveURL, false, ""), nullRef(ir.PrimitiveURL, false, "")},
		{"the top type's marker says nothing",
			nullRef(ir.PrimitiveAny, true, "h"), nullRef(ir.PrimitiveURL, true, "w"), nullRef(ir.PrimitiveURL, true, "w")},
		{"nor does its missing null",
			nullRef(ir.PrimitiveURL, true, "w"), nullRef(ir.PrimitiveAny, false, ""), nullRef(ir.PrimitiveURL, true, "w")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(narrowingRegistry())
			got := foldTwo(stubbed, ir.Property{WireName: "x", Type: tc.dst},
				ir.Property{WireName: "x", Type: tc.src, Provenance: ptrAt("/allOf/1/properties/x")}, unread(t))

			assert.Equal(t, tc.want, got.Type)
			assert.Empty(t, *recorded)
		})
	}
}

// TestFold_AScalarOverANarrowerKindWins pins that a Scalar over a
// kind the bare side equals or contains wins in either order, with no
// diagnostic and no kept declaration.
func TestFold_AScalarOverANarrowerKindWins(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	for i, order := range [][2]ir.TypeRef{
		{ref("type/scalar-int"), primitiveRef(ir.PrimitiveNumber)},
		{primitiveRef(ir.PrimitiveNumber), ref("type/scalar-int")},
	} {
		stubbed, recorded := stubMerger(reg)
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(stubbed, dst, src, unread(t))

		assert.Equal(t, ir.TypeID("type/scalar-int"), dst.Type.Target, "order %d", i)
		assert.Empty(t, *recorded, "order %d", i)
		assert.Empty(t, dst.Unmodeled, "order %d", i)
	}
}

// TestFold_TheTopTypeTakesTheOtherSidesNullAdmission pins that a
// narrower type over the top type keeps its own null admission rather than the
// conjunction with the top type's, since the top type admits null yet lowers
// non-nullable.
func TestFold_TheTopTypeTakesTheOtherSidesNullAdmission(t *testing.T) {
	t.Parallel()
	nullable := func(typeRef ir.TypeRef) ir.TypeRef { typeRef.Nullable = true; return typeRef }
	for i, order := range [][2]ir.TypeRef{
		{primitiveRef(ir.PrimitiveAny), nullable(primitiveRef(ir.PrimitiveInteger))},
		{nullable(primitiveRef(ir.PrimitiveInteger)), primitiveRef(ir.PrimitiveAny)},
		{ref("type/scalar-any-doc"), nullable(primitiveRef(ir.PrimitiveInteger))},
		{nullable(primitiveRef(ir.PrimitiveInteger)), ref("type/scalar-any-doc")},
	} {
		stubbed, recorded := stubMerger(judgeRegistry())
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(stubbed, dst, src, unread(t))

		assert.Equal(t, nullable(primitiveRef(ir.PrimitiveInteger)), dst.Type, "nullable integer (order %d)", i)
		assert.Empty(t, *recorded, "order %d", i)
	}
}
