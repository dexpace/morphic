// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"slices"
	"strconv"
	"strings"
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
			ms[i] = ir.EnumMember{Value: stringValue("v" + strconv.Itoa(from+i))}
		}
		return &ir.Enum{ID: id, ValueType: ir.PrimitiveString, Members: ms}
	}
	cost := func(n int) float64 {
		reg := map[ir.TypeID]ir.TypeDef{"type/x": enum("type/x", 0, n), "type/y": enum("type/y", n/2, n)}
		stubbed, recorded := stubMerger(reg)
		decls := declsOf("type/x", "type/y")
		got := stubbed.fold(decls)
		irEnum, ok := reg[got.Type.Target].(*ir.Enum)
		require.True(t, ok)
		require.Len(t, irEnum.Members, n/2, "the two meet in the values they share")
		require.Empty(t, *recorded)
		least := testing.AllocsPerRun(1, func() { stubbed.fold(decls) })
		for range 4 {
			least = min(least, testing.AllocsPerRun(1, func() { stubbed.fold(decls) }))
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
	memberIndex := indexMembers([]ir.EnumMember{{Value: number("1")}, {Value: raw}, {Value: number("1.0")}, {Value: stringValue("a")}})
	for _, tc := range []struct {
		name  string
		v     ir.Value
		at    int
		found bool
	}{
		{"a number spelled another way", number("1.00"), 0, true},
		{"a string", stringValue("a"), 3, true},
		{"bytes", raw, 1, true},
		{"other bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("y")}, 0, false},
		{"a string no member holds", stringValue("b"), 0, false},
	} {
		at, found := memberIndex.find(tc.v)
		assert.Equal(t, tc.found, found, tc.name)
		assert.Equal(t, tc.at, at, tc.name)
	}
}

// valueTypedRegistry holds enums of numbers under several value types, each
// anonymous at the declaration it is written at: t/<kind>-<values>.
func valueTypedRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	for n, e := range []struct {
		kind   ir.PrimitiveKind
		values []string
	}{
		{ir.PrimitiveNumber, []string{"1", "2"}}, {ir.PrimitiveInteger, []string{"1"}},
		{ir.PrimitiveNumber, []string{"1"}}, {ir.PrimitiveInteger, []string{"1", "2"}},
		{ir.PrimitiveNumber, []string{"1", "2.5"}}, {ir.PrimitiveInteger, []string{"1", "3"}},
		{ir.PrimitiveNumber, []string{"1", "2", "3"}}, {ir.PrimitiveInteger, []string{"1", "2", "5"}},
		{ir.PrimitiveInt32, []string{"2", "3", "7"}}, {ir.PrimitiveInt32, []string{"1", "2", "4"}},
		{ir.PrimitiveUint32, []string{"2", "3", "5"}},
	} {
		id := ir.TypeID("type/" + string(e.kind) + "-" + strings.Join(e.values, ","))
		members := make([]ir.EnumMember, len(e.values))
		for k, v := range e.values {
			members[k] = ir.EnumMember{Value: number(v)}
		}
		reg[id] = &ir.Enum{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(n)), ValueType: e.kind, Members: members}
	}
	return reg
}

// TestFold_EnumsOfDifferentValueTypesAreJudgedByTheirValues pins GitHub #1654's
// enums in both orders: the one whose values the other holds every one of, of
// the narrower value type, is the field's type, with nothing minted, kept or
// reported, and two each saying what the other lacks, in their values or their
// value types, meet in an enum of the values they share, of the narrower value
// type.
func TestFold_EnumsOfDifferentValueTypesAreJudgedByTheirValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b   ir.TypeID
		within ir.TypeID // the field's type, or "" where the two meet
		values []ir.Value
	}{
		{a: "type/number-1,2", b: "type/integer-1", within: "type/integer-1"},
		{a: "type/number-1", b: "type/integer-1,2", values: []ir.Value{number("1")}},
		{a: "type/number-1,2.5", b: "type/integer-1,3", values: []ir.Value{number("1")}},
	} {
		for _, order := range [][]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			reg := valueTypedRegistry()
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(declsOf(order...))

			assert.Empty(t, *recorded, "%v", order)
			assert.Empty(t, got.Unmodeled, "%v", order)
			if tc.within != "" {
				assert.Equal(t, tc.within, got.Type.Target, "%v", order)
				assert.Zero(t, stubbed.walk.minted, "%v: nothing is minted", order)
				continue
			}
			enum, ok := reg[got.Type.Target].(*ir.Enum)
			require.True(t, ok, "%v", order)
			assert.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "%v", order)
			assert.Equal(t, ir.PrimitiveInteger, enum.ValueType, "%v: of the narrower value type", order)
			assert.Equal(t, tc.values, valuesOf(enum), "%v", order)
		}
	}
}

// TestFold_ThreeEnumsMeetInTheNarrowestValueType pins three enums of value types
// one within the next, each pair sharing values, in every order: the field is an
// enum of the value all three hold, of the narrowest value type. Where no value
// type is within both others, no enum holds the three, so the field keeps the
// one meeting both, and each other is reported and kept, in every order.
func TestFold_ThreeEnumsMeetInTheNarrowestValueType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		enums []ir.TypeID
		kept  ir.TypeID // the field's type where no enum holds the three
	}{
		{"one value type within the next", []ir.TypeID{"type/number-1,2,3", "type/integer-1,2,5", "type/int32-2,3,7"}, ""},
		{"no value type within both others", []ir.TypeID{"type/number-1,2,3", "type/int32-1,2,4", "type/uint32-2,3,5"}, "type/number-1,2,3"},
	} {
		for _, perm := range permutations(len(tc.enums)) {
			order := make([]ir.TypeID, len(perm))
			for k, i := range perm {
				order[k] = tc.enums[i]
			}
			reg := valueTypedRegistry()
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(declsOf(order...))

			if tc.kept != "" {
				assert.Equal(t, tc.kept, got.Type.Target, "%s: %v", tc.name, order)
				assert.Zero(t, stubbed.walk.minted, "%s: %v: nothing is minted", tc.name, order)
				require.Len(t, *recorded, 2, "%s: %v", tc.name, order)
				for _, diagnostic := range *recorded {
					assert.NotEqual(t, declaredAt(slices.Index(order, tc.kept)), diagnostic.Provenance.Pointer, "%s: %v", tc.name, order)
				}
				assert.Len(t, got.Unmodeled, 2, "%s: %v: each other is kept", tc.name, order)
				continue
			}
			enum, ok := reg[got.Type.Target].(*ir.Enum)
			require.True(t, ok, "%s: %v", tc.name, order)
			assert.Equal(t, ir.PrimitiveInt32, enum.ValueType, "%s: %v", tc.name, order)
			assert.Equal(t, []ir.Value{number("2")}, valuesOf(enum), "%s: %v", tc.name, order)
			assert.Empty(t, *recorded, "%s: %v", tc.name, order)
		}
	}
}

// valuesOf returns the values enum lists, in order.
func valuesOf(enum *ir.Enum) []ir.Value {
	out := make([]ir.Value, len(enum.Members))
	for k, enumMember := range enum.Members {
		out[k] = enumMember.Value
	}
	return out
}
