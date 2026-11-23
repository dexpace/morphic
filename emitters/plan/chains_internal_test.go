// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// missing names a type no registry chainOf builds holds.
const missing ir.TypeID = "type/missing"

// chainOf builds a registry of n anonymous scalars, each decorating the next
// and the last decorating end, which the registry holds unless it is missing,
// with the type sensitive marked Sensitive where it names one.
func chainOf(n int, end, sensitive ir.TypeID) ir.TypeRegistry {
	types := ir.TypeRegistry{}
	for i := range n {
		next := end
		if i+1 < n {
			next = link(i + 1)
		}
		types[link(i)] = &ir.Scalar{
			ID: link(i), Anonymous: true,
			Base: &ir.TypeRef{Target: next},
		}
	}
	if end != missing && types[end] == nil {
		types[end] = &ir.Any{ID: end}
	}
	if sensitive != "" {
		types[sensitive].Common().Sensitive = true
	}
	return types
}

// link names the i-th scalar of a chain.
func link(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("type/link-%d", i)) }

// TestBaseChains_AnswerAlikeInEveryOrder asks one registry's chains the same
// questions from each end. Build asks in the order a map ranges, which no
// test controls, and a walk settles the types it passes, so the second
// question of each order is answered from what the first left behind.
func TestBaseChains_AnswerAlikeInEveryOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		types         ir.TypeRegistry
		wantAlias     ir.TypeID
		wantSensitive bool
	}{
		{"a chain ending at a type", chainOf(4, "type/end", ""), "type/end", false},
		{"a chain ending at a sensitive type", chainOf(4, "type/end", "type/end"), "type/end", true},
		{"a chain ending at a type the registry lacks", chainOf(4, missing, ""), "", false},
		{"a chain returning to its first decorator", chainOf(4, link(0), ""), "", false},
		{"a chain returning to its last decorator", chainOf(4, link(3), ""), "", false},
	}
	for _, tt := range tests {
		for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}} {
			t.Run(fmt.Sprintf("%s, asked as %v", tt.name, order), func(t *testing.T) {
				t.Parallel()
				chains := newBaseChains(tt.types)
				for _, i := range order {
					assert.Equal(t, tt.wantAlias, chains.alias(link(i)), "alias of link %d", i)
					assert.Equal(t, tt.wantSensitive, chains.sensitive(link(i)), "sensitivity of link %d", i)
				}
				assert.Empty(t, chains.passed, "no decorator stays marked as on a chain between walks")
			})
		}
	}
}

// TestBaseChains_ASensitiveLinkMarksOnlyWhatLeadsToIt holds a sensitive type
// in the middle of a chain to marking the types whose chain reaches it, in
// either order of asking.
func TestBaseChains_ASensitiveLinkMarksOnlyWhatLeadsToIt(t *testing.T) {
	t.Parallel()
	want := []bool{true, true, true, false}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		chains := newBaseChains(chainOf(4, "type/end", link(2)))
		for _, i := range order {
			assert.Equal(t, want[i], chains.sensitive(link(i)), "link %d, asked as %v", i, order)
		}
	}
}

// TestBaseChains_AWalkSettlesEveryTypeItPassed holds what keeps the walks of
// one registry to a step for each type: one question of a long chain's first
// decorator settles every decorator of the chain, so a later question of any
// of them is answered from the record and walks nothing.
func TestBaseChains_AWalkSettlesEveryTypeItPassed(t *testing.T) {
	t.Parallel()
	const n = 2_000
	chains := newBaseChains(chainOf(n, "type/end", "type/end"))

	assert.Equal(t, ir.TypeID("type/end"), chains.alias(link(0)))
	assert.True(t, chains.sensitive(link(0)))

	assert.Len(t, chains.aliases, n)
	assert.Len(t, chains.sensitivity, n)
	for i := range n {
		assert.Equal(t, ir.TypeID("type/end"), chains.aliases[link(i)], "link %d", i)
		assert.True(t, chains.sensitivity[link(i)], "link %d", i)
	}
}

// TestBaseChains_CostGrowsWithTheRegistry counts the turns the walks take
// over a chain of n decorators, asked of each from either end, at n, 2n and
// 4n, and holds their growth to the registry's: a question stops at the first
// type an earlier walk settled, where walking each chain to its end would
// take the square of n from the far end.
func TestBaseChains_CostGrowsWithTheRegistry(t *testing.T) {
	t.Parallel()
	const n = 400
	orders := map[string]func(size int) []int{
		"from the far end": func(size int) []int {
			order := make([]int, 0, size)
			for i := size - 1; i >= 0; i-- {
				order = append(order, i)
			}
			return order
		},
		"from the near end": func(size int) []int {
			order := make([]int, 0, size)
			for i := range size {
				order = append(order, i)
			}
			return order
		},
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			steps := func(size int) int {
				chains := newBaseChains(chainOf(size, "type/end", "type/end"))
				for _, i := range order(size) {
					require.Equal(t, ir.TypeID("type/end"), chains.alias(link(i)))
					require.True(t, chains.sensitive(link(i)))
				}
				return chains.steps
			}
			small, double, large := steps(n), steps(2*n), steps(4*n)

			require.Positive(t, double-small, "the walks take more turns over a longer chain")
			assert.LessOrEqual(t, float64(large-double), 2.5*float64(double-small),
				"%d, %d and %d turns at %d, %d and %d decorators", small, double, large, n, 2*n, 4*n)
		})
	}
}
