// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// trace records a walk: each visit as the node's value at its depth, and each
// leave marked with a slash.
type trace []string

func (tr *trace) visit(step nodeview.Step) func(n *yaml.Node, depth int) nodeview.Step {
	return func(n *yaml.Node, depth int) nodeview.Step {
		*tr = append(*tr, strings.Repeat(".", depth)+label(n))
		return step
	}
}

func (tr *trace) leave(n *yaml.Node, depth int) {
	*tr = append(*tr, strings.Repeat(".", depth)+"/"+label(n))
}

// label names a node for a trace: a scalar by its value, anything else by its
// kind.
func label(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		return "map"
	case yaml.SequenceNode:
		return "seq"
	default:
		return "alias"
	}
}

// TestWalk_VisitsInDocumentOrderAndLeavesAfterChildren pins the order: each
// node before its children, which come in Content order, and each node's leave
// once its children are done, a node holding none left at once.
func TestWalk_VisitsInDocumentOrderAndLeavesAfterChildren(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), ynode.Seq(ynode.Scalar("a"), ynode.Scalar("b")))
	var tr trace

	out := nodeview.Walk{Nodes: 10, Visit: tr.visit(nodeview.Descend), Leave: tr.leave}.Run(t.Context(), root)

	assert.Equal(t, trace{"map", ".k", "./k", ".seq", "..a", "../a", "..b", "../b", "./seq", "/map"}, tr)
	assert.Equal(t, nodeview.Outcome{Visited: 5}, out)
}

// TestWalk_PruneAndStop pins the two answers that cut a walk short on purpose:
// a pruned node's children go unvisited and it is never left, and a stop ends
// the walk there, reported as Stopped rather than Cut.
func TestWalk_PruneAndStop(t *testing.T) {
	t.Parallel()
	root := ynode.Seq(ynode.Seq(ynode.Scalar("hidden")), ynode.Scalar("after"))

	var pruned trace
	out := nodeview.Walk{Nodes: 10, Leave: pruned.leave, Visit: func(n *yaml.Node, depth int) nodeview.Step {
		pruned.visit(nodeview.Descend)(n, depth)
		if depth == 1 && n.Kind == yaml.SequenceNode {
			return nodeview.Prune
		}
		return nodeview.Descend
	}}.Run(t.Context(), root)
	assert.Equal(t, trace{"seq", ".seq", ".after", "./after", "/seq"}, pruned)
	assert.Equal(t, nodeview.Outcome{Visited: 3}, out)

	var stopped trace
	out = nodeview.Walk{Nodes: 10, Visit: stopped.visit(nodeview.Stop)}.Run(t.Context(), root)
	assert.Equal(t, trace{"seq"}, stopped)
	assert.Equal(t, nodeview.Outcome{Visited: 1, Stopped: true}, out)
}

// TestWalk_BoundsAreReportedNotSilent pins the two bounds: a tree of exactly
// Nodes nodes is walked whole, one node more is Cut there, and a bound below
// one visits none; a node past Depth is not visited, its siblings are, and the
// walk says it was Cut.
func TestWalk_BoundsAreReportedNotSilent(t *testing.T) {
	t.Parallel()
	root := ynode.Seq(ynode.Seq(ynode.Scalar("deep")), ynode.Scalar("shallow"))

	assert.Equal(t, nodeview.Outcome{Visited: 4}, nodeview.Walk{Nodes: 4}.Run(t.Context(), root))
	assert.Equal(t, nodeview.Outcome{Visited: 3, Cut: true}, nodeview.Walk{Nodes: 3}.Run(t.Context(), root))
	assert.Equal(t, nodeview.Outcome{Cut: true}, nodeview.Walk{Nodes: 0}.Run(t.Context(), root))

	var tr trace
	out := nodeview.Walk{Nodes: 10, Depth: 1, Visit: tr.visit(nodeview.Descend)}.Run(t.Context(), root)
	assert.Equal(t, trace{"seq", ".seq", ".shallow"}, tr)
	assert.Equal(t, nodeview.Outcome{Visited: 3, Cut: true}, out)
}

// TestWalk_AnAliasIsALeafOrItsTarget pins the alias rule: as a leaf, an alias
// is visited as itself and what it names only where that is written; standing
// for its target, the target is visited in its place each time, and an alias
// naming nothing is visited as itself.
func TestWalk_AnAliasIsALeafOrItsTarget(t *testing.T) {
	t.Parallel()
	target := ynode.Seq(ynode.Scalar("x"))
	root := ynode.Seq(target, ynode.Alias(target), ynode.Alias(nil))

	var leaf trace
	nodeview.Walk{Nodes: 10, Visit: leaf.visit(nodeview.Descend)}.Run(t.Context(), root)
	assert.Equal(t, trace{"seq", ".seq", "..x", ".alias", ".alias"}, leaf)

	var copies trace
	nodeview.Walk{Nodes: 10, Aliases: nodeview.AliasTarget, Visit: copies.visit(nodeview.Descend)}.Run(t.Context(), root)
	assert.Equal(t, trace{"seq", ".seq", "..x", ".seq", "..x", ".alias"}, copies)
}

// TestWalk_SkipsWhatNoParseProduces pins the walk on trees built by hand: a
// nil root and a nil child are not visited, and an unset Visit descends
// everywhere, which counts the nodes.
func TestWalk_SkipsWhatNoParseProduces(t *testing.T) {
	t.Parallel()
	assert.Equal(t, nodeview.Outcome{}, nodeview.Walk{Nodes: 10}.Run(t.Context(), nil))
	assert.Equal(t, nodeview.Outcome{Visited: 2}, nodeview.Walk{Nodes: 10}.Run(t.Context(), ynode.Map(ynode.Scalar("a"), nil)))
}

// TestWalk_DeepAndWideTreesAreIterative holds the walk to the inputs a reader
// of untrusted text meets: thousands of levels, which a recursive walk would
// meet one stack frame at a time, and a mapping wide enough that a walk
// re-reading it per node would show. Both are walked whole, at their depth.
func TestWalk_DeepAndWideTreesAreIterative(t *testing.T) {
	t.Parallel()
	const levels = 100_000
	deep := ynode.Scalar("leaf")
	for range levels {
		deep = ynode.Seq(deep)
	}
	deepest := 0
	out := nodeview.Walk{Nodes: levels + 1, Visit: func(_ *yaml.Node, depth int) nodeview.Step {
		deepest = max(deepest, depth)
		return nodeview.Descend
	}}.Run(t.Context(), deep)
	require.Equal(t, nodeview.Outcome{Visited: levels + 1}, out)
	assert.Equal(t, levels, deepest)

	wide := ynode.Map()
	for range levels {
		wide.Content = append(wide.Content, ynode.Scalar("k"), ynode.Scalar("v"))
	}
	assert.Equal(t, nodeview.Outcome{Visited: 2*levels + 1}, nodeview.Walk{Nodes: 2*levels + 1}.Run(t.Context(), wide))
}

// TestWalk_StopsOnceItsContextIsDone pins a walk's context: asked before the
// first node and then once every AskEvery nodes, so a cancel ends a walk
// within that many, which reports itself Cut, as running out of its bound
// does. A walk handed a done context visits nothing.
func TestWalk_StopsOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	items := make([]*yaml.Node, 4*nodeview.AskEvery)
	for i := range items {
		items[i] = ynode.Scalar("x")
	}
	root := ynode.Seq(items...)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Equal(t, nodeview.Outcome{Cut: true}, nodeview.Walk{Nodes: len(items) + 1}.Run(cancelled, root))

	ctx, stop := context.WithCancel(t.Context())
	t.Cleanup(stop)
	out := nodeview.Walk{Nodes: len(items) + 1, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if n.Kind == yaml.ScalarNode {
			stop()
		}
		return nodeview.Descend
	}}.Run(ctx, root)
	assert.Equal(t, nodeview.Outcome{Visited: nodeview.AskEvery, Cut: true}, out,
		"cancelled at the second node, the walk stopped at the next ask")
}
