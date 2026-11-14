// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
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
