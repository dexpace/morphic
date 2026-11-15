// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestRecognize_DeclinesASourcePastTheNodeBudget holds recognition to the node
// budget it reads within: a source past it is declined with the load phase's
// refusal, whatever version it declares.
func TestRecognize_DeclinesASourcePastTheNodeBudget(t *testing.T) {
	t.Parallel()

	rec, diags, ok := detect.Recognize(t.Context(), 0, []byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"),
		budget.Unbounded(), budget.AtMost(3))

	assert.False(t, ok)
	assert.Equal(t, compilers.Recognition{}, rec)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Contains(t, diags[0].Message, "source document parses to")
}
