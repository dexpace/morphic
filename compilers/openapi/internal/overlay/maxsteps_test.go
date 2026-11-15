// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// TestApply_HoldsAnApplicationsSelectorsToMaxSteps pins overlay.MaxSteps
// through Apply, the entry a compile takes: one search whose charge, its
// input's length and one more times its program's weight, lands within the
// bound applies, and one byte more is refused. The input each needs is read
// off the evaluator's own count at two short inputs, which grows by the
// weight and a reading step per eight bytes.
func TestApply_HoldsAnApplicationsSelectorsToMaxSteps(t *testing.T) {
	t.Parallel()
	query := "$[?search(@, '" + strings.TrimSuffix(strings.Repeat("[a-z]{999}b|", 8), "|") + "')]"
	src := func(n int) string { return head + "x-s: " + strings.Repeat("a", n) + "\n" }
	need := func(n int) int {
		q, err := jsonpath.Parse(query)
		require.NoError(t, err)
		b := budget.New(budget.Unbounded())
		_, err = q.Select(t.Context(), nodeview.New(), decoded(t, src(n)), &b)
		require.NoError(t, err)
		return b.Spent()
	}
	weight := (need(16) - need(8) - 1) / 8
	base := need(8) - 9*weight - 1
	most := (overlay.MaxSteps - base - weight) * 8 / (8*weight + 1)
	for base+(most+2)*weight+(most+1)/8 <= overlay.MaxSteps {
		most++
	}
	require.LessOrEqual(t, base+(most+1)*weight+most/8, overlay.MaxSteps, "the input the bound admits")

	refused := func(n int) bool {
		ov := v11 + "  - target: \"" + query + "\"\n    update: {t: 1}\n"
		_, _, diags := applied(t, src(n), ov, false)
		return slices.Contains(codes(diags), "error "+diag.BudgetExceeded)
	}
	assert.False(t, refused(most), "an input of %d bytes is within the bound", most)
	assert.True(t, refused(most+1), "one of %d is past it", most+1)
}
