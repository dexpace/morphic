// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package sourceindex

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// TestMeasure_MeasuresEachNodeOnce counts the measure's steps rather than
// timing it. Every anchor below is aliased twice by the next, so the tree the
// parser builds doubles at each one, while the measure, which remembers each
// node it takes, stays linear in the tree as written. Measuring a node a second
// time costs one step.
func TestMeasure_MeasuresEachNodeOnce(t *testing.T) {
	t.Parallel()
	const anchors = 40
	var b strings.Builder
	b.WriteString("a0: &a0 [1]\n")
	for i := 1; i < anchors; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	root := openapitest.YAMLDocument(t, b.String())
	idx := Build(t.Context(), root, MaxIndexedNodes)
	m := NewMeasure(MaxIndexedNodes)

	assert.Equal(t, anchors+1, m.Of(t.Context(), idx.Root()))
	assert.LessOrEqual(t, m.steps.Spent(), 4*idx.Nodes(), "a step per frame, a frame per edge")
	assert.Equal(t, 2*len(m.memo)+anchors-1, m.steps.Spent(),
		"two steps a node, and one each to pop the lists the root pushed before an alias measured them")
	steps := m.steps.Spent()
	assert.Equal(t, anchors+1, m.Of(t.Context(), idx.Root()))
	assert.Equal(t, steps+1, m.steps.Spent(), "a node measured once is not measured again")
}

// TestMeasure_EndsAnAliasCycleNoAncestorHolds pins the defence the in-flight
// set is: two mappings each aliasing the other are a cycle no ancestor path
// shows, which only a tree built by hand holds, and the measure ends on it.
func TestMeasure_EndsAnAliasCycleNoAncestorHolds(t *testing.T) {
	t.Parallel()
	a, b := ynode.Map(), ynode.Map()
	a.Content = append(a.Content, ynode.Scalar("k"), ynode.Alias(b))
	b.Content = append(b.Content, ynode.Scalar("k"), ynode.Alias(a))
	root := ynode.Map(ynode.Scalar("a"), a, ynode.Scalar("b"), b)

	idx := Build(t.Context(), root, MaxIndexedNodes)
	assert.Empty(t, idx.AnchorCycles(), "neither alias names its own ancestor")
	assert.False(t, idx.Truncated())
	assert.Equal(t, 4, idx.Nesting(), "root, a, b through a's alias, and a again, whose alias back is cut")
}

// TestMeasure_StopsAtItsLimit drives the measure's own bound, which only a
// hand-built tree reaches: three nodes as written, one an alias out of the
// tree to twenty-one more. The index reports the measure's truncation as its
// own, so the caller refuses rather than reading a partial nesting.
func TestMeasure_StopsAtItsLimit(t *testing.T) {
	t.Parallel()
	outside := ynode.Seq()
	for range 20 {
		outside.Content = append(outside.Content, ynode.Seq())
	}
	root := ynode.Map(ynode.Scalar("k"), ynode.Alias(outside))

	assert.False(t, Build(t.Context(), root, MaxIndexedNodes).Truncated())
	assert.True(t, Build(t.Context(), root, 3).Truncated(), "a bound the tree as written fits and its alias does not")

	whole := openapitest.YAMLDocument(t, "a: &x [1, 1, 1, 1, 1, 1, 1, 1]\nb: *x\n")
	nodes := Build(t.Context(), whole, MaxIndexedNodes).Nodes()
	assert.False(t, Build(t.Context(), whole, nodes).Truncated(),
		"a parsed tree at its node bound, its alias measuring half of it, stays within the limit")

	m := NewMeasure(1)
	m.Of(t.Context(), outside)
	assert.True(t, m.Truncated())
}

// TestMeasure_NilAndTargetlessAliasNestNothing pins the two nodes that name
// nothing to measure.
func TestMeasure_NilAndTargetlessAliasNestNothing(t *testing.T) {
	t.Parallel()
	m := NewMeasure(MaxIndexedNodes)
	assert.Equal(t, 0, m.Of(t.Context(), nil))
	assert.Equal(t, 0, m.Of(t.Context(), &yaml.Node{Kind: yaml.AliasNode}))
	assert.Equal(t, 1, m.Of(t.Context(), ynode.Seq(nil)), "a nil child adds nothing to its sequence")
}

// TestNewMeasure_SaturatesALimitPastOverflow pins the node bound so large that
// four times it overflows: it is taken as no limit, not wrapped to a negative
// one that truncates the first measure.
func TestNewMeasure_SaturatesALimitPastOverflow(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLDocument(t, "a: &x [1]\nb: *x\n")

	assert.False(t, Build(t.Context(), root, math.MaxInt/2).Truncated())
	assert.Equal(t, budget.AtMost(math.MaxInt), NewMeasure(math.MaxInt).steps.Limit())
	assert.Equal(t, budget.AtMost(math.MaxInt), NewMeasure(math.MaxInt/4).steps.Limit(), "the least bound four times overflows")
	assert.Equal(t, budget.AtMost(4*(math.MaxInt/4-1)), NewMeasure(math.MaxInt/4-1).steps.Limit(), "and the largest it does not")
	assert.Equal(t, budget.AtMost(0), NewMeasure(-1).steps.Limit(), "a negative bound admits nothing")
}

// TestMeasure_StopsOnceTheContextIsDone pins the measure's asks: before its
// first step and every nodeview.AskEvery steps after, counted across calls.
// Once refused it answers in part and reports Truncated, and a later call
// asks once and answers at once.
func TestMeasure_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	const depth = 2 * nodeview.AskEvery
	deep := ynode.Seq()
	for range depth - 1 {
		deep = ynode.Seq(deep)
	}

	live := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	m := NewMeasure(MaxIndexedNodes)
	assert.Equal(t, depth, m.Of(live, deep))
	assert.False(t, m.Truncated())
	assert.GreaterOrEqual(t, live.Calls, 2*depth/nodeview.AskEvery, "an ask every AskEvery of two steps a node")

	cut := &ctxtest.Countdown{Context: t.Context(), Left: 1}
	m = NewMeasure(MaxIndexedNodes)
	assert.Less(t, m.Of(cut, deep), depth)
	assert.True(t, m.Truncated())
	asked, steps := cut.Calls, m.steps.Spent()
	m.Of(cut, deep)
	assert.Equal(t, asked+1, cut.Calls, "a later call asks once")
	assert.Equal(t, steps, m.steps.Spent(), "and takes no step")
}

// TestMeasure_ShapesATreeAsTheIndexWalksIt holds the two ways a tree's shape is
// taken to one answer: the index's walk, which takes the tree as written and
// an alias's target from the measure, and the measure's own walk of the whole
// tree, on trees whose aliases, merges, sequences and nil children each move
// the nesting and the bytes of the pointers.
func TestMeasure_ShapesATreeAsTheIndexWalksIt(t *testing.T) {
	t.Parallel()
	nilChild := ynode.Seq(ynode.Scalar("a"), nil, ynode.Map(ynode.Scalar("long-key"), ynode.Seq(ynode.Scalar("v"))))
	for name, root := range map[string]*yaml.Node{
		"aliases":   openapitest.YAMLDocument(t, "ab: &s [x, {c: y}]\nd: *s\ne: {f: *s, g: [*s, *s]}\n"),
		"merges":    openapitest.YAMLDocument(t, "base: &b {k: v, kk: [1, 2]}\nm: {<<: *b, own: {deep: [x]}}\n"),
		"sequences": openapitest.YAMLDocument(t, "s: [[a, [b, [c]]], "+strings.Repeat("x, ", 12)+"{y: z}]\n"),
		"nil child": ynode.Map(ynode.Scalar("top"), nilChild),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			idx := Build(t.Context(), root, MaxIndexedNodes)
			measured := NewMeasure(MaxIndexedNodes).shapeOf(t.Context(), idx.Root())
			assert.Equal(t, measured.nesting, idx.Nesting())
			assert.Equal(t, measured.pointers, idx.PointerBytes())
			assert.Positive(t, idx.PointerBytes())
		})
	}
}

// TestShape_SaturatesItsCounts pins the arithmetic a shape's counts take, which
// an alias bomb carries past any int: a sum or product past math.MaxInt holds
// there, and one within it is exact.
func TestShape_SaturatesItsCounts(t *testing.T) {
	t.Parallel()
	assert.Equal(t, math.MaxInt, sum(math.MaxInt-1, 2))
	assert.Equal(t, math.MaxInt, product(math.MaxInt/2, 3))
	assert.Equal(t, 7, sum(3, 4))
	assert.Equal(t, 12, product(3, 4))
	assert.Zero(t, product(0, math.MaxInt))
	huge := shape{nodes: math.MaxInt, pointers: math.MaxInt}
	assert.Equal(t, shape{nesting: 1, nodes: math.MaxInt, pointers: math.MaxInt}, shape{nesting: 1, nodes: 1}.holding(huge, 5))
}
