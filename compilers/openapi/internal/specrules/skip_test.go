// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// kindNode pairs a mapping with the kind it was read as.
type kindNode struct {
	kind specrules.Kind
	node *yaml.Node
}

// TestCheck_SkipLeavesUnreadAMappingItAnswersTrueFor holds the hook to what it
// says: a mapping about to be read as an object of a kind for which it answers
// true is not read, so it draws no finding and is not told to Seen, and one it
// answers false for is read as always.
func TestCheck_SkipLeavesUnreadAMappingItAnswersTrueFor(t *testing.T) {
	t.Parallel()
	const doc = "{parameters: {a: {name: n, in: bogus, schema: {}}, b: {name: m, in: query, schema: {}}}}"
	var parsed yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &parsed))
	a := nodeview.Child(nodeview.Child(nodeview.DocumentRoot(&parsed), "parameters"), "a")
	require.NotNil(t, a)
	skipped := map[*yaml.Node]bool{a: true}

	var seen []kindNode
	diags := specrules.NewChecker().Check(t.Context(), &parsed, "3.1.0", specrules.Options{
		Start: specrules.Components,
		Skip:  func(_ specrules.Kind, node *yaml.Node) bool { return skipped[node] },
		Seen:  func(kind specrules.Kind, node *yaml.Node) { seen = append(seen, kindNode{kind, node}) },
	})

	assert.Empty(t, diags, "the parameter written in: bogus is not read")
	for _, kindNode := range seen {
		assert.False(t, skipped[kindNode.node], "a skipped mapping is not told to Seen")
	}
}

// TestCheck_ANodeSeenIsSkippedWhenItIsReadAgain holds Seen and Skip, handed one
// set, to reading each (node, kind) once: a second check of the same tree reads
// nothing and reports nothing, where the first reported its finding.
func TestCheck_ANodeSeenIsSkippedWhenItIsReadAgain(t *testing.T) {
	t.Parallel()
	var parsed yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{parameters: {a: {name: n, in: bogus, schema: {}}}}"), &parsed))
	read := map[kindNode]bool{}
	opts := specrules.Options{
		Start: specrules.Components,
		Seen:  func(kind specrules.Kind, node *yaml.Node) { read[kindNode{kind, node}] = true },
		Skip:  func(kind specrules.Kind, node *yaml.Node) bool { return read[kindNode{kind, node}] },
	}

	first := specrules.NewChecker().Check(t.Context(), &parsed, "3.1.0", opts)
	readByFirst := len(read)
	second := specrules.NewChecker().Check(t.Context(), &parsed, "3.1.0", opts)

	assert.NotEmpty(t, first)
	assert.Positive(t, readByFirst)
	assert.Empty(t, second, "each (node, kind) is read once")
	assert.Len(t, read, readByFirst, "and the second check read no node of its own")
}
