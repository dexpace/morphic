package sourceindex

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestNesting_CountsTheMappingsAndSequencesOnTheDeepestPath pins what one
// level is: a mapping or a sequence, empty or not, and never a scalar.
func TestNesting_CountsTheMappingsAndSequencesOnTheDeepestPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want int64
	}{
		{"a scalar document", "x\n", 0},
		{"an empty mapping", "{}\n", 1},
		{"an empty sequence", "[]\n", 1},
		{"a flat mapping", "a: 1\nb: 2\n", 1},
		{"an empty mapping nested", "a: {}\n", 2},
		{"mappings and sequences alternating", "a: [1, {b: [2]}]\n", 4},
		{"the deepest of two branches", "a: {b: {c: 1}}\nd: [1]\n", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Build(Decode(t, tc.src), MaxIndexedNodes).Nesting())
		})
	}
}

// TestNesting_AnAliasNestsAsItsTarget pins that the nesting is the parser's,
// which builds an alias's target where the alias stands. Each anchor below
// nests the one before it through an alias, so the tree as written is two
// levels deep while the one the parser builds is one level deeper per anchor:
// what let a document nested a few hundred levels as written reach thousands.
func TestNesting_AnAliasNestsAsItsTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want int64
	}{
		{"one alias", "a: &x {b: {c: 1}}\nd: {e: *x}\n", 4},
		{"an alias at the top", "a: &x {b: 1}\nc: *x\n", 2},
		{"an alias to a scalar", "a: &x 1\nb: *x\n", 1},
		{"a chain of anchors", chainedAnchors(8), 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			idx := Build(Decode(t, tc.src), MaxIndexedNodes)
			require.Empty(t, idx.AnchorCycles())
			assert.Equal(t, tc.want, idx.Nesting())
		})
	}
}

// chainedAnchors is n anchors a0 … a(n-1), each a mapping holding an alias to
// the one before it.
func chainedAnchors(n int) string {
	var b strings.Builder
	b.WriteString("a0: &a0 {k: 1}\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {k: *a%d}\n", i, i, i-1)
	}
	return b.String()
}

// TestNesting_CountsAMergeValueWhereItIsWritten pins the one place the measure
// counts more than the parser builds: the parser folds a `<<` value's pairs
// into the mapping holding it, and the measure counts the value as the level
// it is written at. The excess is a level per merge, never a shortfall.
func TestNesting_CountsAMergeValueWhereItIsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want int64
	}{
		{"an inline merge", "a: {<<: {k: {v: 1}}}\n", 4},
		{"a merge through an alias", "b: &b {k: {v: 1}}\na: {<<: *b}\n", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Build(Decode(t, tc.src), MaxIndexedNodes).Nesting())
		})
	}
}

// TestNesting_DoesNotReadAnchorNames pins that an alias's target is measured
// by identity. The loader clears every anchor name before the model is built
// (releaseAnchors), and a hand-built tree carries none, so a measure keyed on
// names would fall short of the parser on both.
func TestNesting_DoesNotReadAnchorNames(t *testing.T) {
	t.Parallel()
	root := Decode(t, "a: &x {b: {c: 1}}\nd: {e: *x}\n")
	named := Build(root, MaxIndexedNodes).Nesting()
	clearAnchors(root)

	assert.Equal(t, named, Build(root, MaxIndexedNodes).Nesting(), "the same tree without its anchor names")

	target := ynode.Map(ynode.Scalar("k"), ynode.Map(ynode.Scalar("v"), ynode.Scalar("1")))
	built := ynode.Map(ynode.Scalar("a"), target, ynode.Scalar("b"), ynode.Map(ynode.Scalar("c"), ynode.Alias(target)))
	assert.Equal(t, int64(4), Build(built, MaxIndexedNodes).Nesting(), "a hand-built alias to an unnamed node")
}

// clearAnchors empties the anchor name of every node under n.
func clearAnchors(n *yaml.Node) {
	stack := []*yaml.Node{n}
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		at.Anchor = ""
		stack = append(stack, at.Content...)
	}
}

// TestNesting_AnAnchorCycleCountsOneTurn pins that a recursive anchor, which
// the caller refuses on AnchorCycles, leaves a finite answer behind rather
// than a walk that never ends.
func TestNesting_AnAnchorCycleCountsOneTurn(t *testing.T) {
	t.Parallel()
	inner := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root := ynode.Map(ynode.Scalar("outer"), inner)
	inner.Content = append(inner.Content, ynode.Scalar("loop"), ynode.Alias(root))

	idx := Build(root, MaxIndexedNodes)
	require.Len(t, idx.AnchorCycles(), 1)
	assert.Equal(t, int64(4), idx.Nesting(), "root, inner, and one turn back through root and inner")
}

// TestNesting_DeepTreeCostsNoStack pins that neither the walk nor the measure
// recurses: a tree nested ten times deeper than yaml.v3 parses, reached both
// as written and through an alias, is measured whole.
func TestNesting_DeepTreeCostsNoStack(t *testing.T) {
	t.Parallel()
	const depth = 100000
	deep := ynode.Seq()
	at := deep
	for range depth - 1 {
		next := ynode.Seq()
		at.Content = append(at.Content, next)
		at = next
	}
	root := ynode.Map(ynode.Scalar("deep"), deep, ynode.Scalar("again"), ynode.Alias(deep))

	idx := Build(root, MaxIndexedNodes)
	require.False(t, idx.Truncated())
	assert.Equal(t, int64(depth+1), idx.Nesting())
	assert.Equal(t, int64(depth), NewMeasure(MaxIndexedNodes).Of(deep))
}

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
	root := Decode(t, b.String())
	idx := Build(root, MaxIndexedNodes)
	m := NewMeasure(MaxIndexedNodes)

	assert.Equal(t, int64(anchors+1), m.Of(idx.Root()))
	assert.LessOrEqual(t, m.steps, 4*idx.Nodes(), "a step per frame, a frame per edge")
	assert.Equal(t, 2*int64(len(m.memo))+anchors-1, m.steps,
		"two steps a node, and one each to pop the lists the root pushed before an alias measured them")
	steps := m.steps
	assert.Equal(t, int64(anchors+1), m.Of(idx.Root()))
	assert.Equal(t, steps+1, m.steps, "a node measured once is not measured again")
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

	idx := Build(root, MaxIndexedNodes)
	assert.Empty(t, idx.AnchorCycles(), "neither alias names its own ancestor")
	assert.False(t, idx.Truncated())
	assert.Equal(t, int64(4), idx.Nesting(), "root, a, b through a's alias, and a again, whose alias back is cut")
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

	assert.False(t, Build(root, MaxIndexedNodes).Truncated())
	assert.True(t, Build(root, 3).Truncated(), "a bound the tree as written fits and its alias does not")

	whole := Decode(t, "a: &x [1, 1, 1, 1, 1, 1, 1, 1]\nb: *x\n")
	nodes := Build(whole, MaxIndexedNodes).Nodes()
	assert.False(t, Build(whole, nodes).Truncated(),
		"a parsed tree at its node bound, its alias measuring half of it, stays within the limit")

	m := NewMeasure(1)
	m.Of(outside)
	assert.True(t, m.Truncated())
}

// TestMeasure_NilAndTargetlessAliasNestNothing pins the two nodes that name
// nothing to measure.
func TestMeasure_NilAndTargetlessAliasNestNothing(t *testing.T) {
	t.Parallel()
	m := NewMeasure(MaxIndexedNodes)
	assert.Equal(t, int64(0), m.Of(nil))
	assert.Equal(t, int64(0), m.Of(&yaml.Node{Kind: yaml.AliasNode}))
	assert.Equal(t, int64(1), m.Of(ynode.Seq(nil)), "a nil child adds nothing to its sequence")
}

// TestNewMeasure_SaturatesALimitPastOverflow pins the node bound so large that
// four times it overflows: it is taken as no limit, not wrapped to a negative
// one that truncates the first measure.
func TestNewMeasure_SaturatesALimitPastOverflow(t *testing.T) {
	t.Parallel()
	root := Decode(t, "a: &x [1]\nb: *x\n")

	assert.False(t, Build(root, 1<<61).Truncated())
	assert.Equal(t, int64(math.MaxInt64), NewMeasure(math.MaxInt64).limit)
	assert.Equal(t, int64(0), NewMeasure(-1).limit, "a negative bound admits nothing")
}
