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

// TestRepeat_FindsAKeyWrittenTwiceWhereverTheViewReadsIt pins Repeat: a key
// the mapping writes twice, plainly or through an alias key, then one a merge
// source writes twice, in merge order, each named with the mapping writing
// it; a key a merge shares with the mapping is precedence, not a repeat.
func TestRepeat_FindsAKeyWrittenTwiceWhereverTheViewReadsIt(t *testing.T) {
	t.Parallel()
	src := "s1: &s1 {p: 1, p: 2}\ns2: &s2 {q: 1, q: 2}\n" +
		"own: {b: 1, a: 1, b: 2, a: 2}\naliased: {&k k: 1, *k : 2}\n" +
		"merged: {<<: [*s2, *s1]}\nboth: {<<: *s1, x: 1, x: 2}\nshared: {<<: {a: 1}, a: 2}\n"
	root := nodeview.YAMLDoc(t, src)
	v := nodeview.New()
	for _, tc := range []struct {
		name, key, in string
	}{
		{"own", "b", "own"}, {"aliased", "k", "aliased"}, {"merged", "q", "s2"}, {"both", "x", "both"},
	} {
		key, in, ok := v.Repeat(nodeview.Child(root, tc.name))
		assert.True(t, ok, tc.name)
		assert.Equal(t, tc.key, key, tc.name)
		assert.Same(t, nodeview.Child(root, tc.in), in, tc.name)
	}
	_, _, ok := v.Repeat(nodeview.Child(root, "shared"))
	assert.False(t, ok, "a key a merge shares with the mapping is no repeat")
	_, _, ok = v.Repeat(ynode.Scalar("x"))
	assert.False(t, ok, "a node that is no mapping repeats nothing")
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
