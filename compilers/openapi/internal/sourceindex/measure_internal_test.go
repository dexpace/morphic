package sourceindex

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
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
