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
		broad, narrow ir.PrimKind
	}{
		{"number and integer", ir.PrimNumber, ir.PrimInteger},
		{"integer and int32", ir.PrimInteger, ir.PrimInt32},
		{"string and datetime_offset", ir.PrimString, ir.PrimDatetimeOffset},
		{"int32 narrows int64", ir.PrimInt64, ir.PrimInt32},
		{"url and string", ir.PrimString, ir.PrimURL},
		{"float64 and number", ir.PrimNumber, ir.PrimFloat64},
		{"uuid and string", ir.PrimString, ir.PrimUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var merged [2]ir.TypeRef
			for i, order := range [][2]ir.PrimKind{{tc.broad, tc.narrow}, {tc.narrow, tc.broad}} {
				g, recorded := stubMerger(narrowingRegistry())
				dst := ir.Property{WireName: "x", Type: primRef(order[0])}
				src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

				dst = foldTwo(g, dst, src, unread(t))

				assert.Equal(t, primRef(tc.narrow).Target, dst.Type.Target, "the narrower target wins (order %d)", i)
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
	for i, order := range [][2]ir.PrimKind{{ir.PrimInt16, ir.PrimUint8}, {ir.PrimUint8, ir.PrimInt16}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0])}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(g, dst, src, unread(t))

		assert.Equal(t, primRef(ir.PrimUint8).Target, dst.Type.Target, "uint8 wins (order %d)", i)
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
					g, recorded := stubMerger(narrowingRegistry())
					broad := primRef(ir.PrimNumber)
					broad.Nullable = broadNull
					narrow := primRef(ir.PrimInteger)
					narrow.Nullable = narrowNull
					dst, src := broad, narrow
					if !broadFirst {
						dst, src = narrow, broad
					}
					p := ir.Property{WireName: "x", Type: dst}

					p = foldTwo(g, p, ir.Property{WireName: "x", Type: src, Provenance: ptrAt("/p")}, unread(t))

					assert.Equal(t, narrow.Target, p.Type.Target)
					assert.Equal(t, broadNull && narrowNull, p.Type.Nullable,
						"null is admitted only where both branches admit it")
					assert.Empty(t, *recorded)
				})
			}
		}
	}
}

// TestFold_ADroppedTargetKeepsTheFirstNullAdmission pins a drop:
// an unrelated redeclaration's type is lost whole, so
// the merged field keeps the first declaration's null admission, not the
// conjunction a same-referent or narrowing pair takes.
func TestFold_ADroppedTargetKeepsTheFirstNullAdmission(t *testing.T) {
	t.Parallel()
	for _, firstNull := range []bool{true, false} {
		t.Run(fmt.Sprintf("firstNullable=%v", firstNull), func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(narrowingRegistry())
			dst := ir.Property{WireName: "x", Type: primRef(ir.PrimString)}
			dst.Type.Nullable = firstNull
			src := ir.Property{WireName: "x", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			src.Type.Nullable = !firstNull

			dst = foldTwo(g, dst, src, written(`{"type":"integer"}`))

			assert.Equal(t, primRef(ir.PrimString).Target, dst.Type.Target, "the first declaration keeps the shape")
			assert.Equal(t, firstNull, dst.Type.Nullable, "a dropped type takes its null admission with it")
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
			g, recorded := stubMerger(narrowingRegistry())
			broad := ir.Property{
				WireName:    "x",
				Type:        primRef(ir.PrimNumber),
				Default:     &ir.Value{Kind: ir.ValueNumber, Num: "1"},
				Constraints: &ir.Constraints{Min: bigVal("0")},
				Provenance:  ptrAt("/allOf/0/properties/x"),
			}
			narrow := ir.Property{WireName: "x", Type: primRef(ir.PrimInteger), Provenance: ptrAt("/allOf/1/properties/x")}
			dst, src := broad, narrow
			if !broadFirst {
				dst, src = narrow, broad
			}

			dst = foldTwo(g, dst, src, unread(t))

			assert.Equal(t, primRef(ir.PrimInteger).Target, dst.Type.Target)
			require.NotNil(t, dst.Constraints)
			assert.Equal(t, bigVal("0"), dst.Constraints.Min, "the broader declaration's bound lands on the merged field")
			require.NotNil(t, dst.Default)
			assert.Equal(t, ir.BigVal("1"), dst.Default.Num, "and so does its default")
			assert.Empty(t, *recorded)
		})
	}
}

// TestFold_ANarrowingIsNoDrop pins that a bare narrowing settles on one target
// in either order and is no drop, so the redeclaration is neither classified
// nor reported nor kept.
func TestFold_ANarrowingIsNoDrop(t *testing.T) {
	t.Parallel()
	for i, order := range [][2]ir.PrimKind{{ir.PrimInteger, ir.PrimNumber}, {ir.PrimNumber, ir.PrimInteger}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0])}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		got := foldTwo(g, dst, src, unread(t))

		assert.Equal(t, primRef(ir.PrimInteger), got.Type, "the narrower target wins (order %d)", i)
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
	nullRef := func(k ir.PrimKind, nullable bool, inferred string) ir.TypeRef {
		r := primRef(k)
		r.Nullable, r.Inferred = nullable, inferred
		return r
	}
	for _, tc := range []struct {
		name           string
		dst, src, want ir.TypeRef
	}{
		{"the narrower redeclaration takes the first's marker",
			nullRef(ir.PrimString, true, "h"), nullRef(ir.PrimURL, true, ""), nullRef(ir.PrimURL, true, "h")},
		{"the narrower first declaration takes the redeclaration's marker",
			nullRef(ir.PrimURL, true, ""), nullRef(ir.PrimString, true, "h"), nullRef(ir.PrimURL, true, "h")},
		{"both markers, the first's first",
			nullRef(ir.PrimString, true, "a"), nullRef(ir.PrimURL, true, "b"), nullRef(ir.PrimURL, true, "a,b")},
		{"a side admitting no null drops the marker",
			nullRef(ir.PrimString, true, "h"), nullRef(ir.PrimURL, false, ""), nullRef(ir.PrimURL, false, "")},
		{"the top type's marker says nothing",
			nullRef(ir.PrimAny, true, "h"), nullRef(ir.PrimURL, true, "w"), nullRef(ir.PrimURL, true, "w")},
		{"nor does its missing null",
			nullRef(ir.PrimURL, true, "w"), nullRef(ir.PrimAny, false, ""), nullRef(ir.PrimURL, true, "w")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(narrowingRegistry())
			got := foldTwo(g, ir.Property{WireName: "x", Type: tc.dst},
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
		{ref("t/scalar-int"), primRef(ir.PrimNumber)},
		{primRef(ir.PrimNumber), ref("t/scalar-int")},
	} {
		g, recorded := stubMerger(reg)
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(g, dst, src, unread(t))

		assert.Equal(t, ir.TypeID("t/scalar-int"), dst.Type.Target, "order %d", i)
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
	nullable := func(r ir.TypeRef) ir.TypeRef { r.Nullable = true; return r }
	for i, order := range [][2]ir.TypeRef{
		{primRef(ir.PrimAny), nullable(primRef(ir.PrimInteger))},
		{nullable(primRef(ir.PrimInteger)), primRef(ir.PrimAny)},
	} {
		g, recorded := stubMerger(judgeRegistry())
		dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(g, dst, src, unread(t))

		assert.Equal(t, nullable(primRef(ir.PrimInteger)), dst.Type, "nullable integer (order %d)", i)
		assert.Empty(t, *recorded, "order %d", i)
	}
}
