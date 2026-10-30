// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// wideTree is a sequence of n one-pair mappings, and the value of its first.
// The index reaches a mapping's pair only once it takes the mapping, and it
// takes the first mapping last, so a cut leaves that value unreached.
func wideTree(n int) (root, firstValue *yaml.Node) {
	items := make([]*yaml.Node, n)
	for i := range items {
		items[i] = ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"+strconv.Itoa(i)))
	}
	return ynode.Seq(items...), items[0].Content[1]
}

// TestPointers_IndexesNoFurtherOnceTheContextIsDone pins the asks of the index
// Pointers builds: before its first node and every nodeview.AskEvery nodes
// after, and none once refused. A cut index holds only the nodes it reached.
func TestPointers_IndexesNoFurtherOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	const n = 4 * nodeview.AskEvery
	root, first := wideTree(n)

	live := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, held := Pointers(live, root)(first)
	require.True(t, held)
	assert.GreaterOrEqual(t, live.Calls, 3*n/nodeview.AskEvery, "an ask every AskEvery of the 3n nodes taken")

	cut := &ctxtest.Countdown{Context: t.Context(), Left: 1}
	_, held = Pointers(cut, root)(first)
	assert.False(t, held, "the first mapping, taken last, was never reached")
	assert.Equal(t, 2, cut.Calls, "one ask passed, one refused, and none after")
}

// TestLeast_IndexesUnderItsContext pins that Least builds its index under the
// context it is given, as Pointers does.
func TestLeast_IndexesUnderItsContext(t *testing.T) {
	t.Parallel()
	root, first := wideTree(4 * nodeview.AskEvery)
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}

	got := Least(ctx, root, []*yaml.Node{root.Content[1], first})

	assert.Same(t, first, got)
	assert.Positive(t, ctx.Calls)
}

// TestDeepSites_MeasuresUnderItsContext pins that deepSites measures under the
// context it is given: the tree as a whole, and each node its walk takes, which
// is what paces the walk. The walk's measures ask on top of the whole tree's.
func TestDeepSites_MeasuresUnderItsContext(t *testing.T) {
	t.Parallel()
	const n = 4 * nodeview.AskEvery
	root, _ := wideTree(n)
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	sourceindex.NewMeasure(sourceindex.MaxIndexedNodes).Of(whole, root)
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}

	sites := deepSites(ctx, root, 1)

	assert.Len(t, sites, n, "each mapping nests a level past the bound")
	assert.GreaterOrEqual(t, whole.Calls, 3*n/nodeview.AskEvery, "the whole tree's measure asks")
	assert.GreaterOrEqual(t, ctx.Calls-whole.Calls, n/nodeview.AskEvery, "and the walk's, a step each")
}
