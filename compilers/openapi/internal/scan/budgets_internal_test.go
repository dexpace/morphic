// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourcetreestats"
	"github.com/dexpace/morphic/ir"
)

// TestAliases_RefusesTheTextAliasesAddPastTheByteBudget holds the last of the
// alias screens: a tree whose aliases add more bytes of scalar text than the
// byte budget admits is refused with one budget finding naming both counts,
// and one within it draws nothing.
func TestAliases_RefusesTheTextAliasesAddPastTheByteBudget(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: &t twelve-chars\nb: *t\nc: *t\n"), &doc))
	idx := sourcetreestats.Build(t.Context(), &doc, 1000)
	require.Equal(t, 24, idx.AliasText(), "two aliases add the anchored text twice")
	whole := ir.Provenance{Source: 0}

	past := Aliases(t.Context(), provenance.Tree{}, whole, idx, budget.Unbounded(), budget.AtMost(23))
	within := Aliases(t.Context(), provenance.Tree{}, whole, idx, budget.Unbounded(), budget.AtMost(24))

	require.Len(t, past, 1)
	assert.Equal(t, diag.BudgetExceeded, past[0].Code)
	assert.Contains(t, past[0].Message, "add 24 bytes of scalar text to the document, past the 23-byte budget")
	assert.Empty(t, within)
}

// TestLessTokens_APrefixComesFirst holds the order of two pointers whose
// shorter one is a prefix of the longer, token by token: the prefix first.
func TestLessTokens_APrefixComesFirst(t *testing.T) {
	t.Parallel()
	assert.True(t, lessTokens([]string{"A"}, []string{"A", "x"}))
	assert.False(t, lessTokens([]string{"A", "x"}, []string{"A"}))
	assert.False(t, lessTokens([]string{"A"}, []string{"A"}), "a pointer is not less than itself")
}
