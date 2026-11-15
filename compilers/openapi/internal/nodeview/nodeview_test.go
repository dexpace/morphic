// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestIsMergeKey_IsTheLibrarysPredicate pins IsMergeKey to the parsed `<<`
// and to nothing else: the same text untagged, an alias to it, or nil.
func TestIsMergeKey_IsTheLibrarysPredicate(t *testing.T) {
	t.Parallel()
	assert.True(t, nodeview.IsMergeKey(ynode.Merge()))
	assert.False(t, nodeview.IsMergeKey(ynode.Scalar("<<")), "untagged, it is an ordinary key")
	assert.False(t, nodeview.IsMergeKey(ynode.Alias(ynode.Merge())), "an alias standing for it is an ordinary key")
	assert.False(t, nodeview.IsMergeKey(nil))
}

// TestMergeSources_NamesEachElementOfAListOrTheValue pins what a `<<` names: the
// elements of a list written in place or through an alias, and any other value
// itself, whatever its kind.
func TestMergeSources_NamesEachElementOfAListOrTheValue(t *testing.T) {
	t.Parallel()
	a, b := ynode.Map(), ynode.Scalar("5")
	list := ynode.Seq(a, b)
	assert.Equal(t, []*yaml.Node{a, b}, nodeview.MergeSources(list))
	assert.Equal(t, []*yaml.Node{a, b}, nodeview.MergeSources(ynode.Alias(list)), "a list through its alias")
	assert.Equal(t, []*yaml.Node{b}, nodeview.MergeSources(b))
	alias := ynode.Alias(a)
	assert.Equal(t, []*yaml.Node{alias}, nodeview.MergeSources(alias), "a mapping's alias is one source")
	assert.Empty(t, nodeview.MergeSources(ynode.Seq()), "an empty list names nothing")
}

// TestNotMapping_FollowsTheAlias pins NotMapping to the node an alias names.
func TestNotMapping_FollowsTheAlias(t *testing.T) {
	t.Parallel()
	assert.False(t, nodeview.NotMapping(ynode.Map()))
	assert.False(t, nodeview.NotMapping(ynode.Alias(ynode.Map())))
	assert.True(t, nodeview.NotMapping(ynode.Scalar("5")))
	assert.True(t, nodeview.NotMapping(ynode.Alias(ynode.Scalar("5"))))
	assert.True(t, nodeview.NotMapping(ynode.Seq()))
	assert.True(t, nodeview.NotMapping(nil))
}

// TestMergesNothing_FindsASourceThatIsNoMapping pins each way a `<<` names
// nothing to merge, at the mapping and in a mapping it merges, beside the
// spellings that merge a mapping, an empty one and an empty list included.
func TestMergesNothing_FindsASourceThatIsNoMapping(t *testing.T) {
	t.Parallel()
	src := "s: &s 5\nm: &m {k: v}\nbad: &bad {<<: 5}\n" + "cases:\n" +
		"  scalar: {<<: 5}\n  null: {<<: null}\n  scalars: {<<: [5, 6]}\n  mixed: {<<: [{}, 5]}\n" +
		"  nested: {<<: {<<: 5}}\n  aliasToScalar: {<<: *s}\n  aliasToBad: {<<: *bad}\n" +
		"  inList: {<<: [*m, *bad]}\n  besideAName: {<<: 5, k: v}\n" +
		"  mapping: {<<: {k: v}}\n  empty: {<<: {}}\n  aliasToMapping: {<<: *m}\n  list: {<<: [*m, {}]}\n" +
		"  emptyList: {<<: []}\n  noMerge: {k: v}\n  quoted: {\"<<\": 5}\n"
	root := nodeview.YAMLDoc(t, src)
	cases := nodeview.Child(root, "cases")
	for name, want := range map[string]bool{
		"scalar": true, "null": true, "scalars": true, "mixed": true, "nested": true, "aliasToScalar": true,
		"aliasToBad": true, "inList": true, "besideAName": true,
		"mapping": false, "empty": false, "aliasToMapping": false, "list": false, "emptyList": false,
		"noMerge": false, "quoted": false,
	} {
		assert.Equal(t, want, nodeview.MergesNothing(nodeview.Child(cases, name)), name)
	}
	assert.True(t, nodeview.MergesNothing(nodeview.Child(root, "bad")), "the mapping itself")
	assert.True(t, nodeview.MergesNothing(ynode.Alias(nodeview.Child(root, "bad"))), "through its alias")
	assert.False(t, nodeview.MergesNothing(ynode.Scalar("5")), "a node that is no mapping writes no `<<`")
	assert.False(t, nodeview.MergesNothing(nil))
}

// TestMergesNothing_ReadsTheChainToItsEnd pins that a chain longer than
// MergeDepthLimit is read whole, as the parser merges it, so its last link
// decides: a reader stopping at the bound would call the clean chain faulty, or
// the faulty one clean.
func TestMergesNothing_ReadsTheChainToItsEnd(t *testing.T) {
	t.Parallel()
	const levels = nodeview.MergeDepthLimit + 6
	clean := ynode.MergeChain(levels)
	assert.False(t, nodeview.MergesNothing(clean))

	faulty := ynode.MergeChain(levels)
	leaf := faulty
	for range levels {
		leaf = nodeview.Deref(leaf.Content[1])
	}
	leaf.Content = []*yaml.Node{ynode.Merge(), ynode.Scalar("5")}
	assert.True(t, nodeview.MergesNothing(faulty))
}

// TestMergesNothing_ReadsAMappingOnce pins that each mapping is read once,
// however many paths reach it: each level here merges the next twice, and no
// level is faulty, so a reading per path would take 2^64 steps to say so. A
// cycle, which load refuses before any lowering reads the tree, ends as well.
func TestMergesNothing_ReadsAMappingOnce(t *testing.T) {
	t.Parallel()
	next := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	for range nodeview.MergeDepthLimit {
		next = ynode.Map(ynode.Merge(), ynode.Seq(ynode.Alias(next), ynode.Alias(next)))
	}
	assert.False(t, nodeview.MergesNothing(next))

	cycle := ynode.Map()
	cycle.Content = []*yaml.Node{ynode.Merge(), ynode.Alias(cycle)}
	assert.False(t, nodeview.MergesNothing(cycle))
}
