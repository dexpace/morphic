// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// nested returns a sequence nesting depth sequences beneath it, the last
// empty: deeper than a parse of a document the loader admits holds.
func nested(depth int) *yaml.Node {
	n := ynode.Seq()
	for range depth {
		n = ynode.Seq(n)
	}
	return n
}

// TestGrafter_RefusesWhatItCannotCopy pins the shapes the grafter refuses to
// write that a parse of an overlay the loader admits never hands it, each
// built by hand: an alias naming nothing, written where an update, a member,
// an element or a key is, and a value nested past maxPayloadDepth, met by an
// update, a merge, an append and a copy. Each is refused, not followed or
// copied in part.
func TestGrafter_RefusesWhatItCannotCopy(t *testing.T) {
	t.Parallel()
	nowhere := &yaml.Node{Kind: yaml.AliasNode}
	tooDeep := nested(maxPayloadDepth + 1)
	tests := map[string]struct {
		target, update *yaml.Node
		version        version
		says           string
	}{
		"an update naming nothing":       {ynode.Map(), nowhere, v10, "names nothing"},
		"a member naming nothing":        {ynode.Map(ynode.Scalar("k"), ynode.Map()), ynode.Map(ynode.Scalar("k"), nowhere), v10, "names nothing"},
		"a new member naming nothing":    {ynode.Map(), ynode.Map(ynode.Scalar("k"), nowhere), v10, "names nothing"},
		"a key naming nothing":           {ynode.Map(), ynode.Map(nowhere, ynode.Scalar("v")), v10, "names nothing"},
		"an element naming nothing":      {ynode.Seq(), ynode.Seq(nowhere), v11, "names nothing"},
		"an appended value too deep":     {ynode.Seq(), tooDeep, v10, "nested past"},
		"a concatenated value too deep":  {ynode.Seq(), ynode.Seq(tooDeep), v11, "nested past"},
		"a new member too deep":          {ynode.Map(), ynode.Map(ynode.Scalar("k"), tooDeep), v10, "nested past"},
		"a merge too deep":               {deepMapping(maxPayloadDepth + 2), deepMapping(maxPayloadDepth + 2), v10, "an update nested past"},
		"a member that names no value":   {ynode.Map(ynode.Scalar("k"), nowhere), ynode.Map(ynode.Scalar("k"), ynode.Map()), v10, "YAML alias"},
		"a target holding a nested list": {ynode.Map(ynode.Scalar("k"), ynode.Seq()), ynode.Map(ynode.Scalar("k"), ynode.Seq(tooDeep)), v10, "nested past"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newGrafter(tc.version, nil)
			_, err := g.write([]*yaml.Node{tc.target}, tc.update)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.says)
		})
	}

	_, err := copyOf(tooDeep, nil, 0, nil)
	require.Error(t, err, "a copy's template is held to the bound too")
}

// deepMapping returns a mapping nesting depth mappings beneath it under the
// key k, so merging one into another descends that deep.
func deepMapping(depth int) *yaml.Node {
	n := ynode.Map()
	for range depth {
		n = ynode.Map(ynode.Scalar("k"), n)
	}
	return n
}

// TestShape_NamesEveryShape pins the words a finding names a shape by, and
// where names a node the overlay built rather than inventing a position.
func TestShape_NamesEveryShape(t *testing.T) {
	t.Parallel()
	for s, want := range map[shape]string{
		shapeObject: "an object", shapeArray: "an array", shapePrimitive: "a primitive",
		shapeAlias: "a YAML alias", shapeNone: "no value",
	} {
		assert.Equal(t, want, s.String())
	}
	assert.Equal(t, "a position the overlay built", where(ynode.Scalar("v")))
	assert.Equal(t, "line 3, column 4", where(&yaml.Node{Line: 3, Column: 4}))
}

// TestGrafter_NamesTheKindsOfMixedTargetsInOneOrder pins the refusal of an
// Overlay 1.1 write whose targets are of several kinds: it names each kind
// once, in one order whatever order the targets are selected in.
func TestGrafter_NamesTheKindsOfMixedTargetsInOneOrder(t *testing.T) {
	t.Parallel()
	orders := [][]*yaml.Node{
		{ynode.Seq(), ynode.Map(), ynode.Seq()},
		{ynode.Map(), ynode.Seq(), ynode.Map()},
	}
	messages := make([]string, 0, len(orders))
	for _, targets := range orders {
		_, err := newGrafter(v11, nil).write(targets, ynode.Map())
		require.Error(t, err)
		messages = append(messages, err.Error())
	}
	assert.Equal(t, messages[0], messages[1])
	assert.Contains(t, messages[0], "it selects an object and an array,")
}

// TestGrafter_RefusesAMixOfAnAliasAndAValue pins the 1.1 rule for the shapes
// of the selected nodes when one is an alias: an alias is a shape of its own,
// so the targets are of two kinds.
func TestGrafter_RefusesAMixOfAnAliasAndAValue(t *testing.T) {
	t.Parallel()
	g := newGrafter(v11, nil)
	_, err := g.write([]*yaml.Node{ynode.Map(), ynode.Alias(ynode.Map())}, ynode.Map())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "an object and a YAML alias")
}

// TestDetach_RemovesAMemberOrAnElementAndNothingElse pins what a remove takes
// out of what holds a node: a member's key and value, an element alone, and
// nothing for a node held as a key or not held at all, which the query never
// selects and which remove refuses.
func TestDetach_RemovesAMemberOrAnElementAndNothingElse(t *testing.T) {
	t.Parallel()
	key, value, other := ynode.Scalar("k"), ynode.Scalar("v"), ynode.Scalar("o")
	mapping := ynode.Map(key, value, ynode.Scalar("x"), other)
	assert.False(t, detach(mapping, key), "a key is no member's value")
	assert.False(t, detach(mapping, ynode.Scalar("v")), "an equal node is not the node")
	assert.True(t, detach(mapping, value))
	assert.Equal(t, []*yaml.Node{mapping.Content[0], other}, mapping.Content, "the member went with its key")

	element := ynode.Scalar("e")
	list := ynode.Seq(ynode.Scalar("a"), element)
	assert.True(t, detach(list, element))
	assert.Len(t, list.Content, 1)

	g := newGrafter(v10, nil)
	g.parents = map[*yaml.Node]*yaml.Node{key: mapping}
	err := g.remove([]*yaml.Node{key})
	require.Error(t, err, "remove refuses a node its holder does not hold as a value")
	assert.Contains(t, err.Error(), "holds as no member or element")
}

// TestIndexParents_BuildsOnceAndStopsOnceTheContextIsDone pins the index a
// remove reads: built under a live context once, then kept, and not built
// under a done one, which the sequence takes as its cancel.
func TestIndexParents_BuildsOnceAndStopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	value := ynode.Scalar("v")
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("k"), value)}}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	g := newGrafter(v10, root)
	assert.False(t, g.indexParents(cancelled))
	assert.Nil(t, g.parents)

	require.True(t, g.indexParents(t.Context()))
	assert.Same(t, root.Content[0], g.parents[value])
	built := g.parents
	require.True(t, g.indexParents(cancelled), "an index built is kept, and asks nothing of the context")
	assert.Equal(t, built, g.parents)
}
