// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
