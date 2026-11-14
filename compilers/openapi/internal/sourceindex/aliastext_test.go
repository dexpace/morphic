// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package sourceindex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
)

// TestAliasText_IsTheTextAliasesAdd holds the count to the scalar text each
// alias adds once substituted, and a tree with no alias to none.
func TestAliasText_IsTheTextAliasesAdd(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]int{
		"a: &t abc\nb: *t\n":            3,
		"a: &m {k: vv}\nb: *m\nc: *m\n": 6,
		"a: abc\nb: abc\n":              0,
	} {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(text), &doc))

		got := sourceindex.Build(t.Context(), &doc, 1000).AliasText()

		assert.Equal(t, want, got, "%q", text)
	}
}
