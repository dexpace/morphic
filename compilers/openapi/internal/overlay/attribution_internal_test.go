// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"encoding/json/jsontext"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestAttribute_StopsPastItsBoundNotAtIt pins the edge of the walk: a tree
// of as many positions as the bound, keys aside, is attributed whole, and one
// position more cuts the walk, which then attributes nothing.
func TestAttribute_StopsPastItsBoundNotAtIt(t *testing.T) {
	t.Parallel()
	value := ynode.Scalar("c")
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		ynode.Map(ynode.Scalar("a"), ynode.Map(ynode.Scalar("b"), value), ynode.Scalar("d"), ynode.Scalar("e")),
	}}
	written := map[*yaml.Node]bool{value: true}
	const positions = 4 // the root mapping, a, a/b and d

	got, complete := attribute(t.Context(), root, written, budget.AtMost(positions))
	require.True(t, complete, "a tree of the bound is read whole")
	assert.Equal(t, 1, provenance.Attributed(1, got).WriterAt("/a/b"))

	_, complete = attribute(t.Context(), root, written, budget.AtMost(positions-1))
	assert.False(t, complete, "one short cuts it")
}

// TestAttribute_StopsOnceTheContextIsDone pins the walk's context, asked at
// each position: handed a done one, it attributes nothing.
func TestAttribute_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("a"), ynode.Scalar("b"))}}

	_, complete := attribute(cancelled, root, nil, budget.AtMost(maxNodes))
	assert.False(t, complete)
	_, complete = attribute(t.Context(), root, nil, budget.AtMost(maxNodes))
	assert.True(t, complete, "the same tree is attributed under a live context")
}

// TestAttribute_TakesShapesAParseNeverProduces pins the walk against a
// document holding no root and a nil child, neither of which yaml.v3 hands
// back and a caller assembling nodes can: each attributes nothing, rather than
// faulting.
func TestAttribute_TakesShapesAParseNeverProduces(t *testing.T) {
	t.Parallel()
	got, complete := attribute(t.Context(), &yaml.Node{Kind: yaml.DocumentNode}, nil, budget.AtMost(maxNodes))
	require.True(t, complete)
	assert.Equal(t, 0, provenance.Attributed(1, got).WriterAt(""))

	withNil := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("a"), nil)}}
	_, complete = attribute(t.Context(), withNil, nil, budget.AtMost(maxNodes))
	assert.True(t, complete, "a nil child is skipped, not dereferenced")
}

// TestAttribute_PlacesEachNodeAsTheParserReadsIt pins the positions the walk
// gives a written node: a key written through an alias at the key it names, a
// `<<` merge's value, a mapping or a list of them, at the mapping merging it,
// since the parser reads its members as that mapping's own, a list that is no
// merge's under its indexes, and a written key at its member's position.
func TestAttribute_PlacesEachNodeAsTheParserReadsIt(t *testing.T) {
	t.Parallel()
	aliased, inPlace := ynode.Scalar("v1"), ynode.Map(ynode.Scalar("m"), ynode.Scalar("v2"))
	itemKey, itemValue := ynode.Scalar("n"), ynode.Scalar("v3")
	listed := ynode.Seq(ynode.Map(itemKey, itemValue))
	plain := ynode.Scalar("v4")
	top := ynode.Map(ynode.Alias(ynode.Scalar("key")), aliased, ynode.Merge(), inPlace, ynode.Merge(), listed,
		ynode.Scalar("s"), ynode.Seq(plain))
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{top}}
	written := map[*yaml.Node]bool{aliased: true, inPlace: true, itemKey: true, itemValue: true, plain: true}

	got, complete := attribute(t.Context(), root, written, budget.AtMost(maxNodes))
	require.True(t, complete)
	a := provenance.Attributed(1, got)

	for node, want := range map[*yaml.Node]jsontext.Pointer{
		aliased: "/key", inPlace: "", itemKey: "/n", itemValue: "/n", plain: "/s/0",
	} {
		at, found := a.At(node)
		require.True(t, found, "%s", want)
		assert.Equal(t, want, at)
	}
	assert.Equal(t, 1, a.WriterAt("/n"), "a merged member is the merging mapping's")
	assert.Equal(t, 1, a.WriterAt("/s/0"))
	assert.Equal(t, 0, a.WriterAt("/s"), "what holds a written node is not written")
}

// TestAttribute_AllocatesInTheGraftsSizeNotItsPointers pins the cost of
// attributing a graft (GitHub #875): one long key over many written children
// allocates in proportion to the nodes, not to the key once per node. It
// attributes the tree, asks the writer of one deep pointer and the pointer of
// one deep node, and holds the bytes allocated to 32 MiB, where a pointer
// stored per written node spells the key for each: a copy per child alone is
// 256 MiB.
//
// It is not parallel: a parallel test would allocate during the measurement.
func TestAttribute_AllocatesInTheGraftsSizeNotItsPointers(t *testing.T) {
	const (
		keyLen   = 1 << 16
		children = 4096
		bound    = 32 << 20
	)
	require.Less(t, bound, children*keyLen, "the bound must be under what a stored pointer per value allocates")
	longKey := strings.Repeat("k", keyLen)
	written := map[*yaml.Node]bool{}
	var deep *yaml.Node
	members := make([]*yaml.Node, 0, 2*children)
	for i := range children {
		key := ynode.Scalar("c" + strconv.Itoa(i))
		deep = ynode.Scalar("v")
		value := ynode.Map(ynode.Scalar("leaf"), deep)
		written[key], written[value], written[value.Content[0]], written[deep] = true, true, true, true
		members = append(members, key, value)
	}
	graft := ynode.Map(members...)
	written[graft] = true
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar(longKey), graft)}}
	want := jsontext.Pointer("/" + longKey + "/c" + strconv.Itoa(children-1) + "/leaf")

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	positions, complete := attribute(t.Context(), root, written, budget.AtMost(maxNodes))
	attribution := provenance.Attributed(1, positions)
	writer := attribution.WriterAt(want)
	got, found := attribution.At(deep)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(root)

	require.True(t, complete)
	assert.Equal(t, 1, writer)
	require.True(t, found)
	// Compared as a bool, so a failure reports the lengths rather than printing
	// two pointers of tens of kilobytes each.
	same := got == want
	assert.True(t, same, "the pointer asked for is %d bytes, want %d", len(got), len(want))
	alloc := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, alloc, uint64(bound), "attributing the graft and asking for one pointer allocated %d bytes", alloc)
}
