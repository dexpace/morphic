// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// TestTreeOf_IndexesUnderTheLoadsContext pins that the tree's pointer
// index is built under the context the load runs under, once a node is first
// asked about (scan.Pointers).
func TestTreeOf_IndexesUnderTheLoadsContext(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n")
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}

	tree := treeOf(ctx, provenance.Doc{}, root)
	require.Zero(t, ctx.Calls, "nothing is indexed before a node is asked about")

	assert.True(t, treeHolds(tree, root.Content[0]))
	assert.Positive(t, ctx.Calls)
}

// TestWalkedFrom_AnswersAtTheLastNodeItReaches pins that a pointer
// leading nowhere is placed at the last node it reached, and that a tree
// holding no node places every finding at the reference that reached it.
func TestWalkedFrom_AnswersAtTheLastNodeItReaches(t *testing.T) {
	t.Parallel()
	root := decodeRoot(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n")
	tree := treeOf(t.Context(), provenance.Doc{}, root)

	for _, locate := range []oas.Locator{walkedFrom(root, tree.Node), reachedLocator(tree, root)} {
		got := locate("/info/nowhere/at/all")
		assert.Equal(t, jsontext.Pointer("/info"), got.Pointer, "placed at /info, the last node the pointer reached")
	}
	assert.Equal(t, ir.Provenance{Source: 0}, walkedFrom(&yaml.Node{Kind: yaml.DocumentNode}, tree.Node)("/x"), "an empty tree is placed by source alone")

	assert.False(t, treeHolds(provenance.Tree{}, root), "a tree that holds nothing holds no node")
	assert.True(t, treeHolds(tree, root.Content[0]), "and one holds the nodes it indexed")
	foreign := reachedLocator(provenance.Tree{}, root)("/info")
	assert.Equal(t, ir.Provenance{Source: foreign.Source, Position: foreign.Position}, foreign)
	assert.Equal(t, 2, foreign.Position.Line)
	assert.False(t, holds(ir.Diagnostic{Provenance: foreign}), "marked foreign")
}
