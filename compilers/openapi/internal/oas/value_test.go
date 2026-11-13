// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestShape_Admits pins the shape check another package reads a value the check
// owns by: a count is a plain number from zero up with no fraction, whatever its
// magnitude, past float64's range included, which the YAML resolver tags a
// string, and an alias is read as the node it names. A fraction, a negative
// number, a quoted one and no node at all are none.
func TestShape_Admits(t *testing.T) {
	t.Parallel()
	for literal, want := range map[string]bool{
		"5": true, "0": true, "3.0": true, "1e400": true, "1e1000000000000000000": true, "0x10": true,
		"99999999999999999999": true, "2.5": false, "-1": false, `"3"`: false, "many": false, "[1]": false,
	} {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("a: &n "+literal+"\nb: *n\n"), &doc), literal)
		pairs := doc.Content[0].Content
		assert.Equal(t, want, oas.Natural.Admits(pairs[1]), literal)
		assert.Equal(t, want, oas.Natural.Admits(pairs[3]), "%s, through an alias", literal)
	}
	assert.False(t, oas.Natural.Admits(nil), "no node is no count")
	assert.True(t, oas.String.Admits(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"}), "a shape other than a count")
}
