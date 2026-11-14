// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
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
