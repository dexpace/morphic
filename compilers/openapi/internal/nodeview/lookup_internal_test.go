package nodeview

import (
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestChild_ReadsOnlyWhatAMappingHolds pins the inputs Child answers nil for
// and the shapes its one pass serves without a view: a node that is no
// mapping, a key nothing writes, a document node or an alias standing for the
// mapping, and a value written as an alias, which it hands back followed.
func TestChild_ReadsOnlyWhatAMappingHolds(t *testing.T) {
	t.Parallel()
	value := ynode.Scalar("v")
	mapping := ynode.Map(ynode.Scalar("k"), ynode.Alias(value))

	assert.Nil(t, Child(nil, "k"), "no node holds nothing")
	assert.Nil(t, Child(ynode.Scalar("k"), "k"), "a scalar holds nothing")
	assert.Nil(t, Child(ynode.Seq(ynode.Scalar("k")), "0"), "a sequence holds no key")
	assert.Same(t, value, Child(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}, "k"),
		"a document node is unwrapped")
	assert.Nil(t, Child(mapping, "absent"))
	assert.Same(t, value, Child(mapping, "k"), "the value's alias is followed")
	assert.Same(t, value, Child(ynode.Alias(mapping), "k"), "an alias standing for the mapping is followed")
}

// TestChild_AnswersAsMappingPairs holds Child to the pair MappingPairs gives
// each key, on the shapes where a lookup could part from the expansion: a key
// written twice in the mapping (its last copy) and in a source (its first), a
// key two sources share (the first's), a list of sources, an own key holding
// nothing or an alias to nothing, a merge cycle, and a chain at MergeDepthLimit
// and one level past it, for every key either names and one neither writes.
// The cycle branches through a list, so a lookup following it would not end.
func TestChild_AnswersAsMappingPairs(t *testing.T) {
	t.Parallel()
	s := ynode.Scalar
	source := ynode.Map(s("a"), s("src-a1"), s("a"), s("src-a2"), s("b"), s("src-b"))
	other := ynode.Map(s("b"), s("other-b"), s("c"), s("other-c"))
	cycle := ynode.Map(s("own"), s("v"))
	inner := ynode.Map(ynode.Merge(), ynode.Seq(ynode.Alias(cycle), ynode.Alias(cycle)), s("in"), s("w"))
	cycle.Content = append(cycle.Content, ynode.Merge(), ynode.Alias(inner))
	cases := map[string]*yaml.Node{
		"own copies":         ynode.Map(s("k"), s("1"), s("k"), s("2")),
		"a source's copies":  ynode.Map(ynode.Merge(), ynode.Alias(source)),
		"own over merged":    ynode.Map(ynode.Merge(), ynode.Alias(source), s("a"), s("own-a")),
		"two sources":        ynode.Map(ynode.Merge(), ynode.Alias(source), ynode.Merge(), ynode.Alias(other)),
		"a list of sources":  ynode.Map(ynode.Merge(), ynode.Seq(ynode.Alias(other), ynode.Alias(source))),
		"own nil over merge": ynode.Map(s("b"), &yaml.Node{Kind: yaml.AliasNode}, ynode.Merge(), ynode.Alias(source)),
		"own nil value":      ynode.Map(s("b"), nil, ynode.Merge(), ynode.Alias(source)),
		"a cycle":            cycle,
		"at the bound":       ynode.MergeChain(MergeDepthLimit),
		"past the bound":     ynode.MergeChain(MergeDepthLimit + 1),
	}
	for name, n := range cases {
		pairs := New().MappingPairs(n)
		asked := map[string]*yaml.Node{"absent": nil, "a": nil, "b": nil, "c": nil, "k": nil, "leaf": nil, "in": nil, "own": nil}
		for _, p := range pairs {
			asked[p.Key] = p.Val
		}
		for key, want := range asked {
			assert.Same(t, want, Child(n, key), "%s: key %q", name, key)
		}
	}
	assert.NotNil(t, Child(cases["at the bound"], "leaf"), "a chain at the bound is read whole")
	assert.Nil(t, Child(cases["past the bound"], "leaf"), "one level past it is not")
}

// TestPairs_IsAFreshViewsReading pins Pairs to MappingPairs read by a view of
// its own, merges included, a document node unwrapped, and to nothing for a
// node that is no mapping.
func TestPairs_IsAFreshViewsReading(t *testing.T) {
	t.Parallel()
	n := ynode.Map(ynode.Merge(), ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")), ynode.Scalar("b"), ynode.Scalar("2"))

	assert.Equal(t, New().MappingPairs(n), Pairs(n))
	assert.Equal(t, []string{"a", "b"}, []string{Pairs(n)[0].Key, Pairs(n)[1].Key})
	assert.Equal(t, Pairs(n), Pairs(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}}),
		"a document node is unwrapped")
	assert.Nil(t, Pairs(ynode.Scalar("x")))
}

// TestWrittenChildren_ReadsTheTextNotThroughIt pins the reading that answers
// where a node is written: a mapping's own pairs, each value as written, an
// alias left an alias, nothing a `<<` merges in, each key as KeyOf names it at
// its last copy, and nothing for a node that is no mapping, an alias standing
// for one included.
func TestWrittenChildren_ReadsTheTextNotThroughIt(t *testing.T) {
	t.Parallel()
	target := ynode.Map(ynode.Scalar("m"), ynode.Scalar("merged"))
	alias := ynode.Alias(target)
	last := ynode.Scalar("last")
	n := ynode.Map(
		ynode.Scalar("a"), alias,
		ynode.Merge(), ynode.Alias(target),
		ynode.Alias(ynode.Scalar("k")), ynode.Scalar("first"),
		ynode.Scalar("k"), last,
	)

	got := WrittenChildren(n)

	assert.Equal(t, map[string]*yaml.Node{"a": alias, "k": last}, got)
	assert.Nil(t, WrittenChildren(nil))
	assert.Nil(t, WrittenChildren(ynode.Seq(ynode.Scalar("a"))))
	assert.Nil(t, WrittenChildren(ynode.Alias(n)), "an alias is not where the mapping is written")
}

// TestIsMergeKey_IsTheLibrarysPredicate pins IsMergeKey to the parsed `<<`
// and to nothing else: the same text untagged, an alias to it, or nil.
func TestIsMergeKey_IsTheLibrarysPredicate(t *testing.T) {
	t.Parallel()
	assert.True(t, IsMergeKey(ynode.Merge()))
	assert.False(t, IsMergeKey(ynode.Scalar("<<")), "untagged, it is an ordinary key")
	assert.False(t, IsMergeKey(ynode.Alias(ynode.Merge())), "an alias standing for it is an ordinary key")
	assert.False(t, IsMergeKey(nil))
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
	root := yamlDoc(t, src)
	v := New()
	for _, tc := range []struct {
		name, key, in string
	}{
		{"own", "b", "own"}, {"aliased", "k", "aliased"}, {"merged", "q", "s2"}, {"both", "x", "both"},
	} {
		key, in, ok := v.Repeat(Child(root, tc.name))
		assert.True(t, ok, tc.name)
		assert.Equal(t, tc.key, key, tc.name)
		assert.Same(t, Child(root, tc.in), in, tc.name)
	}
	_, _, ok := v.Repeat(Child(root, "shared"))
	assert.False(t, ok, "a key a merge shares with the mapping is no repeat")
	_, _, ok = v.Repeat(ynode.Scalar("x"))
	assert.False(t, ok, "a node that is no mapping repeats nothing")
}
