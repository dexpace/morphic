// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// TestReference_AReferenceNoKeyNamesIsReadFromItsText pins the state a schema
// reaches where the model holds a $ref merged in past nodeview.MergeDepthLimit
// and its keys list none: it still conjoins with the keys beside it, and its
// reference, read alone, writes the reference's text rather than indexing a
// key it lacks.
func TestReference_AReferenceNoKeyNamesIsReadFromItsText(t *testing.T) {
	t.Parallel()
	s := &Schema{
		Ref:  &Reference{Keyword: "$ref", Text: "#/components/schemas/T"},
		Keys: []Key{{Name: "description", Node: &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "d"}}},
	}
	_, named := refKey(s)
	require.False(t, named)
	assert.True(t, conjoins(s), "the description is beside the reference wherever it was merged from")
	table := NewTable(Source{})
	var r *Schema
	require.NotPanics(t, func() { r = table.reference(s) })
	assert.Empty(t, r.Keys)
	assert.Same(t, s.Ref, r.Ref)
	require.Len(t, r.Node.Content, 2)
	assert.Equal(t, "$ref", r.Node.Content[0].Value)
	assert.Equal(t, "#/components/schemas/T", r.Node.Content[1].Value)
	assert.Same(t, r, table.reference(s), "read once")
}
