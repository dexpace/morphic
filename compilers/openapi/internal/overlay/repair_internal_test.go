// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// detached returns a document whose one member is an alias naming a mapping
// of three members the tree does not hold, as a remove of its anchor leaves
// it, and a grafter that removed a node.
func detached() (*yaml.Node, *grafter) {
	gone := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"), ynode.Scalar("b"), ynode.Scalar("2"), ynode.Scalar("c"), ynode.Scalar("3"))
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("x"), ynode.Alias(gone))}}
	g := newGrafter(v11, root)
	g.removed = true
	return root, g
}

// TestRepairAliases_CopiesWithinItsBoundAndRefusesPastIt pins the bound on
// what a repair copies: the seven nodes of the mapping fit a bound of seven,
// and a bound of six refuses the overlay rather than leave the alias, which
// would hide what it names.
func TestRepairAliases_CopiesWithinItsBoundAndRefusesPastIt(t *testing.T) {
	t.Parallel()
	root, g := detached()
	done, safe, _ := repairAliases(t.Context(), root, g, budget.AtMost(7), budget.Unbounded())
	assert.True(t, done && safe, "seven nodes fit a bound of seven")
	assert.Equal(t, yaml.MappingNode, root.Content[0].Content[1].Kind)

	root, g = detached()
	done, safe, _ = repairAliases(t.Context(), root, g, budget.AtMost(6), budget.Unbounded())
	assert.True(t, done)
	assert.False(t, safe, "and one fewer refuses")
}

// TestRepairAliases_CopiesWithinTheRoomTheNodeBudgetLeaves pins the node
// budget's hold on a repair: the tree of a mapping holding the alias, three
// nodes, and the seven-node copy that replaces the alias, nine in all, fits a
// budget of nine, the alias's own node counted as room, and a budget of eight
// refuses the repair before the copy passes it, reported as the budget's.
func TestRepairAliases_CopiesWithinTheRoomTheNodeBudgetLeaves(t *testing.T) {
	t.Parallel()
	root, g := detached()
	done, safe, budgeted := repairAliases(t.Context(), root, g, budget.AtMost(maxNodes), budget.AtMost(9))
	require.True(t, done && safe, "nine nodes fit a budget of nine")
	assert.False(t, budgeted)
	assert.Equal(t, 9, countNodes(t.Context(), root, budget.Unbounded()), "what the budget is held to")

	root, g = detached()
	done, safe, budgeted = repairAliases(t.Context(), root, g, budget.AtMost(maxNodes), budget.AtMost(8))
	assert.True(t, done)
	assert.False(t, safe, "and one fewer refuses")
	assert.True(t, budgeted, "on the budget's account")

	root, g = detached()
	_, safe, budgeted = repairAliases(t.Context(), root, g, budget.AtMost(6), budget.AtMost(20))
	assert.False(t, safe)
	assert.False(t, budgeted, "where the bound on a repair is the tighter, it is the bound's")
}

// TestApplyWithin_RefusesARepairPastTheNodeBudget pins the refusal a repair
// the node budget cannot hold draws: the budget's, naming it, with nothing
// applied.
func TestApplyWithin_RefusesARepairPastTheNodeBudget(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("x-t: &s {a: 1, b: 2, c: 3}\nx-use: *s\n"), &root))
	const ov = "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $['x-t']\n    remove: true\n"

	origin, diags := applyWithin(t.Context(), 1, &root, Options{Data: []byte(ov), MaxNodes: budget.AtMost(8)}, budget.AtMost(maxNodes))

	assert.False(t, origin.Applied())
	require.Len(t, diags, 2, "the source's alias is warned of first: %+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
	assert.Contains(t, diags[1].Message, "past the 8-node budget")

	require.NoError(t, yaml.Unmarshal([]byte("x-t: &s {a: 1, b: 2, c: 3}\nx-use: *s\n"), &root))
	origin, diags = applyWithin(t.Context(), 1, &root, Options{Data: []byte(ov), MaxNodes: budget.AtMost(9)}, budget.AtMost(maxNodes))
	assert.True(t, origin.Applied(), "a budget of nine holds the mapping, x-use and its seven-node copy: %+v", diags)
}

// TestRepairAliases_IsRefusedOnceTheContextIsDone pins the repair's context,
// asked by both of its walks: handed a done one, it reports the repair undone,
// which applyWithin refuses, never a tree it skipped.
func TestRepairAliases_IsRefusedOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	root, g := detached()
	done, _, _ := repairAliases(cancelled, root, g, budget.AtMost(maxNodes), budget.Unbounded())
	assert.False(t, done)

	reachable, walked := reachableNodes(t.Context(), root.Content[0])
	require.True(t, walked)
	work := budget.New(budget.AtMost(maxNodes))
	done, _ = substituteDetached(cancelled, root.Content[0], reachable, g.written, &work)
	assert.False(t, done, "the substitution's walk asks it too")
}

// TestRepairAliases_RunsOnlyOnceARemoveApplied pins the gate: nothing but a
// remove detaches a node, so a grafter that removed none leaves the tree to
// the actions, and an alias naming nothing, which no parse produces, refuses
// a repair rather than stand for no value.
func TestRepairAliases_RunsOnlyOnceARemoveApplied(t *testing.T) {
	t.Parallel()
	root, g := detached()
	g.removed = false
	done, safe, _ := repairAliases(t.Context(), root, g, budget.AtMost(maxNodes), budget.Unbounded())
	assert.True(t, done && safe)
	assert.Equal(t, yaml.AliasNode, root.Content[0].Content[1].Kind, "no remove, no repair")

	nowhere := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{ynode.Map(ynode.Scalar("x"), &yaml.Node{Kind: yaml.AliasNode})}}
	g.removed = true
	done, safe, _ = repairAliases(t.Context(), nowhere, g, budget.AtMost(maxNodes), budget.Unbounded())
	assert.True(t, done)
	assert.False(t, safe)

	done, safe, _ = repairAliases(t.Context(), &yaml.Node{Kind: yaml.DocumentNode}, g, budget.AtMost(maxNodes), budget.Unbounded())
	assert.True(t, done && safe, "a document holding nothing holds no alias")
}

// TestApplyWithin_RefusesARepairPastItsBound pins applyWithin's answer to a
// repair it cannot finish: the overlay is refused, as a partly repaired tree
// hides what its aliases name. The bound is set between the two: large enough
// for the source's walk, too small for the copy the remove makes needed.
func TestApplyWithin_RefusesARepairPastItsBound(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("x-t: &s {a: 1, b: 2, c: 3}\nx-use: *s\n"), &root))
	const ov = "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $['x-t']\n    remove: true\n"

	origin, diags := applyWithin(t.Context(), 1, &root, Options{Data: []byte(ov)}, budget.AtMost(6))

	assert.False(t, origin.Applied())
	require.Len(t, diags, 2, "the source's alias is warned of first: %+v", diags)
	assert.Equal(t, diag.OverlayFailed, diags[1].Code)
	assert.Contains(t, diags[1].Message, "aliases a remove left")
}

// TestReachableNodes_TakesShapesAParseNeverProduces pins the walk's guards for
// a tree it did not parse: one node in two places is walked once, not walked
// again beneath each, and a nil child is skipped, not dereferenced.
func TestReachableNodes_TakesShapesAParseNeverProduces(t *testing.T) {
	t.Parallel()
	shared := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	reachable, walked := reachableNodes(t.Context(), ynode.Map(ynode.Scalar("a"), shared, ynode.Scalar("b"), shared, ynode.Scalar("c"), nil))
	require.True(t, walked)
	assert.True(t, reachable[shared])
	assert.Len(t, reachable, 7, "the mapping, its three keys, the shared node once and its two")
}

// TestApplyWithin_NeverAppliesAnUnrepairedTreeOnceItsContextRefused cancels
// an application at every check of its context a whole one makes, on an
// overlay whose remove leaves an alias to repair: no cancelled run reports a
// refusal, which a cancel is not, and none that returns the overlay applied
// leaves an alias naming a node outside the tree.
func TestApplyWithin_NeverAppliesAnUnrepairedTreeOnceItsContextRefused(t *testing.T) {
	t.Parallel()
	const src = "x-t: &s {a: 1}\nx-use: *s\n"
	const ov = "overlay: 1.1.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $['x-t']\n    remove: true\n"
	tree := func() *yaml.Node {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &root))
		return &root
	}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	origin, _ := applyWithin(whole, 1, tree(), Options{Data: []byte(ov)}, budget.AtMost(maxNodes))
	require.True(t, origin.Applied())

	for left := range whole.Calls {
		root := tree()
		origin, diags := applyWithin(&ctxtest.Countdown{Context: t.Context(), Left: left}, 1, root,
			Options{Data: []byte(ov)}, budget.AtMost(maxNodes))

		assert.False(t, diag.HasError(diags), "check %d: %+v", left+1, diags)
		if origin.Applied() {
			use := root.Content[0].Content[1]
			assert.NotEqual(t, yaml.AliasNode, use.Kind, "check %d: the alias was left naming what the remove removed", left+1)
		}
	}
}
