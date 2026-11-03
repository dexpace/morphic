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

// TestStrings_ReadsAListOfScalarsByItsText pins what counts as a list of strings:
// scalars of any tag but null, aliases followed, and nothing else, so an item that
// is no string spoils the list rather than vanishing from it.
func TestStrings_ReadsAListOfScalarsByItsText(t *testing.T) {
	t.Parallel()
	date := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: "2021-01-01"}
	null := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "~"}
	read := ynode.Scalar("read")
	tests := []struct {
		name string
		node *yaml.Node
		want []string
		ok   bool
	}{
		{name: "no node"},
		{name: "a scalar", node: read},
		{name: "a mapping", node: ynode.Map(read, read)},
		{name: "an empty list", node: ynode.Seq(), want: []string{}, ok: true},
		{name: "a date beside a string", node: ynode.Seq(read, date), want: []string{"read", "2021-01-01"}, ok: true},
		{name: "an alias of an item", node: ynode.Seq(ynode.Alias(date)), want: []string{"2021-01-01"}, ok: true},
		{name: "an alias of the list", node: ynode.Alias(ynode.Seq(read)), want: []string{"read"}, ok: true},
		{name: "a null item", node: ynode.Seq(read, null)},
		{name: "a list item", node: ynode.Seq(read, ynode.Seq(read))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := nodeview.Strings(tc.node)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestChild_ReadsOnlyWhatAMappingHolds pins the inputs Child answers nil for
// and the shapes its one pass serves without a view: a node that is no
// mapping, a key nothing writes, a document node or an alias standing for the
// mapping, and a value written as an alias, which it hands back followed.
func TestChild_ReadsOnlyWhatAMappingHolds(t *testing.T) {
	t.Parallel()
	value := ynode.Scalar("v")
	mapping := ynode.Map(ynode.Scalar("k"), ynode.Alias(value))

	assert.Nil(t, nodeview.Child(nil, "k"), "no node holds nothing")
	assert.Nil(t, nodeview.Child(ynode.Scalar("k"), "k"), "a scalar holds nothing")
	assert.Nil(t, nodeview.Child(ynode.Seq(ynode.Scalar("k")), "0"), "a sequence holds no key")
	assert.Same(t, value, nodeview.Child(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}, "k"),
		"a document node is unwrapped")
	assert.Nil(t, nodeview.Child(mapping, "absent"))
	assert.Same(t, value, nodeview.Child(mapping, "k"), "the value's alias is followed")
	assert.Same(t, value, nodeview.Child(ynode.Alias(mapping), "k"), "an alias standing for the mapping is followed")
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
		"at the bound":       ynode.MergeChain(nodeview.MergeDepthLimit),
		"past the bound":     ynode.MergeChain(nodeview.MergeDepthLimit + 1),
	}
	for name, n := range cases {
		pairs := nodeview.New().MappingPairs(n)
		asked := map[string]*yaml.Node{"absent": nil, "a": nil, "b": nil, "c": nil, "k": nil, "leaf": nil, "in": nil, "own": nil}
		for _, p := range pairs {
			asked[p.Key] = p.Val
		}
		for key, want := range asked {
			assert.Same(t, want, nodeview.Child(n, key), "%s: key %q", name, key)
		}
	}
	assert.NotNil(t, nodeview.Child(cases["at the bound"], "leaf"), "a chain at the bound is read whole")
	assert.Nil(t, nodeview.Child(cases["past the bound"], "leaf"), "one level past it is not")
}

// TestPairs_IsAFreshViewsReading pins Pairs to MappingPairs read by a view of
// its own, merges included, a document node unwrapped, and to nothing for a
// node that is no mapping.
func TestPairs_IsAFreshViewsReading(t *testing.T) {
	t.Parallel()
	n := ynode.Map(ynode.Merge(), ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")), ynode.Scalar("b"), ynode.Scalar("2"))

	assert.Equal(t, nodeview.New().MappingPairs(n), nodeview.Pairs(n))
	assert.Equal(t, []string{"a", "b"}, []string{nodeview.Pairs(n)[0].Key, nodeview.Pairs(n)[1].Key})
	assert.Equal(t, nodeview.Pairs(n), nodeview.Pairs(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}}),
		"a document node is unwrapped")
	assert.Nil(t, nodeview.Pairs(ynode.Scalar("x")))
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

	got := nodeview.WrittenChildren(n)

	assert.Equal(t, map[string]*yaml.Node{"a": alias, "k": last}, got)
	assert.Nil(t, nodeview.WrittenChildren(nil))
	assert.Nil(t, nodeview.WrittenChildren(ynode.Seq(ynode.Scalar("a"))))
	assert.Nil(t, nodeview.WrittenChildren(ynode.Alias(n)), "an alias is not where the mapping is written")
}
