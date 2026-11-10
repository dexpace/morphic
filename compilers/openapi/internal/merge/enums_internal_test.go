// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestFold_TwoEnumsCostTheirWidth holds a field declared as two enums sharing
// half their values to a cost linear in their members, through the judgment and
// the enum the fold builds of them: each finds the other's members by value
// through an index. A scan spells two values a comparison, so its allocations
// grow with the square of the width. Not parallel: AllocsPerRun measures the
// process.
func TestFold_TwoEnumsCostTheirWidth(t *testing.T) {
	enum := func(id ir.TypeID, from, n int) *ir.Enum {
		ms := make([]ir.EnumMember, n)
		for i := range ms {
			ms[i] = ir.EnumMember{Value: str("v" + strconv.Itoa(from+i))}
		}
		return &ir.Enum{ID: id, ValueType: ir.PrimString, Members: ms}
	}
	cost := func(n int) float64 {
		reg := map[ir.TypeID]ir.TypeDef{"t/x": enum("t/x", 0, n), "t/y": enum("t/y", n/2, n)}
		g, recorded := stubMerger(reg)
		decls := declsOf("t/x", "t/y")
		got := g.fold(decls)
		e, ok := reg[got.Type.Target].(*ir.Enum)
		require.True(t, ok)
		require.Len(t, e.Members, n/2, "the two meet in the values they share")
		require.Empty(t, *recorded)
		least := testing.AllocsPerRun(1, func() { g.fold(decls) })
		for range 4 {
			least = min(least, testing.AllocsPerRun(1, func() { g.fold(decls) }))
		}
		return least
	}

	const n = 200
	one, two, four := cost(n), cost(2*n), cost(4*n)
	assert.LessOrEqual(t, four-two, 2.5*(two-one), "allocations at %d, %d and %d members: %v, %v and %v",
		n, 2*n, 4*n, one, two, four)
}

// TestMemberIndex_FindsTheFirstMemberHoldingAValue pins the index enums are
// judged through: a value is found at the first member holding it, by its
// canonical spelling where it has one, so 1 and 1.00 are one value, and by a
// deep comparison among the members no spelling names, such as bytes. A value
// no member holds is not found.
func TestMemberIndex_FindsTheFirstMemberHoldingAValue(t *testing.T) {
	t.Parallel()
	raw := ir.Value{Kind: ir.ValueBytes, Bytes: []byte("x")}
	x := indexMembers([]ir.EnumMember{{Value: num("1")}, {Value: raw}, {Value: num("1.0")}, {Value: str("a")}})
	for _, tc := range []struct {
		name  string
		v     ir.Value
		at    int
		found bool
	}{
		{"a number spelled another way", num("1.00"), 0, true},
		{"a string", str("a"), 3, true},
		{"bytes", raw, 1, true},
		{"other bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("y")}, 0, false},
		{"a string no member holds", str("b"), 0, false},
	} {
		at, found := x.find(tc.v)
		assert.Equal(t, tc.found, found, tc.name)
		assert.Equal(t, tc.at, at, tc.name)
	}
}
