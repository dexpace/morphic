// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
)

// spentOn returns the steps query spends over the value text writes, which
// it must evaluate within no bound.
func spentOn(t *testing.T, query, text string) int {
	t.Helper()
	_, spent, err := selectWithin(t, query, readTree(t, text), budget.Unbounded())
	require.NoError(t, err)
	return spent
}

// TestSelect_ChargesEachElementTwoArraysCompare pins the cost of comparing
// two arrays: a step for each pair of elements, spent before any is paired,
// so four candidates comparing arrays twice as long spend four times their
// length more, though the arrays differ in their last element alone.
func TestSelect_ChargesEachElementTwoArraysCompare(t *testing.T) {
	t.Parallel()
	steps := func(n int) int {
		zeros := strings.Repeat("0, ", n-1)
		return spentOn(t, "$.c[?$.a == $.b]", "{a: ["+zeros+"1], b: ["+zeros+"2], c: [0, 0, 0, 0]}")
	}

	assert.Equal(t, 4*100, steps(200)-steps(100))
}

// TestSelect_ReadsAScalarOnceAndChargesTheBytesItCompares pins what a
// comparison of two long scalars costs: each is read once an evaluation, a
// step a jsonpath.ReadBytes, and each of the ten candidates compares them at a
// step a jsonpath.CompareBytes. Doubling the scalars costs one more reading of
// each and one more comparison a candidate, as plain strings, quoted strings
// and numbers.
func TestSelect_ReadsAScalarOnceAndChargesTheBytesItCompares(t *testing.T) {
	t.Parallel()
	const size, candidates = 6400, 10
	tests := []struct {
		name, query string
		spell       func(size int, last string) string
	}{
		{"plain strings", "$.c[?$.a == $.b]", func(n int, last string) string { return strings.Repeat("x", n-1) + last }},
		{"quoted strings", "$.c[?$.a == $.b]", func(n int, last string) string { return `"` + strings.Repeat("x", n-1) + last + `"` }},
		{"numbers", "$.c[?$.a < $.b]", func(n int, last string) string {
			return strings.Repeat("9", n-1) + map[string]string{"a": "1", "b": "2"}[last]
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			steps := func(n int) int {
				return spentOn(t, tc.query, "a: "+tc.spell(n, "a")+"\nb: "+tc.spell(n, "b")+"\nc: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]\n")
			}

			want := 2*size/jsonpath.ReadBytes + candidates*size/jsonpath.CompareBytes
			assert.Equal(t, want, steps(2*size)-steps(size))
		})
	}
}

// TestSelect_ChargesAPatternsProgramForAnEmptyInput pins the least a call of
// match or search costs: its program's weight, which the engine runs at the
// input's end however short the input. One more empty string searched by a
// program of a thousand optional atoms costs hundreds of steps, not the two a
// candidate costs before it is tested.
func TestSelect_ChargesAPatternsProgramForAnEmptyInput(t *testing.T) {
	t.Parallel()
	const query = "$[?search(@, '(a?){1000}')]"

	one := spentOn(t, query, `[""]`)
	two := spentOn(t, query, `["", ""]`)

	assert.Greater(t, two-one, 250, "one more call of the program")
}

// pinnedDocument is the document the step pins below are taken over: arrays
// to select, compare and search, a pattern, six copies of one, and 65
// distinct ones, each twice, which pass the patterns an evaluation caches.
func pinnedDocument() string {
	var distinct strings.Builder
	for range 2 {
		for i := range 65 {
			fmt.Fprintf(&distinct, "{s: x, p: 'q%02d'}, ", i)
		}
	}
	return "{a: [1, 2, {x: 3}], b: [1, 2, {x: 3}], s: abc, r: 'b.', p: [b., b., b., b., b., b.], q: [" +
		distinct.String() + "]}"
}

// TestSelect_SpendsTheStepsEachSitePins pins the steps fixed queries spend
// over one fixed document, a query for each place a step is spent: each node
// returned, visited by a descent, tested by a filter, compared equal, ordered
// or deep-compared, a pattern's bytes and program, and a pattern compiled
// once an evaluation where it repeats, past 64 distinct ones each time.
func TestSelect_SpendsTheStepsEachSitePins(t *testing.T) {
	t.Parallel()
	doc := pinnedDocument()
	for query, want := range map[string]int{
		"$.a[*]":                 10,
		"$..x":                   949,
		"$.a[?@ == 1]":           14,
		"$.a[?@ < 2]":            14,
		"$[?@ == $.b]":           84,
		"$[?search(@, $.r)]":     39,
		"$.p[?match(@, $.p[0])]": 89,
		"$.q[?match(@.s, @.p)]":  1573,
	} {
		assert.Equal(t, want, spentOn(t, query, doc), query)
	}
}

// TestMaxResults_IsTheBoundOnWhatAnEvaluationHolds pins the constant the nodes
// an evaluation's nodelists hold at once are bounded by; SelectHolding holds
// the mechanism to a bound a test reaches.
func TestMaxResults_IsTheBoundOnWhatAnEvaluationHolds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1<<22, jsonpath.MaxResults)
}
