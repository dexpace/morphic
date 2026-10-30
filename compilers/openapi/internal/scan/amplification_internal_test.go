// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// rawNodes is a tree's own node count, before any alias is substituted, as the
// source index derives it.
func rawNodes(t testing.TB, n *yaml.Node) int {
	t.Helper()
	return sourceindex.Build(t.Context(), n, sourceindex.MaxIndexedNodes).Nodes()
}

const amplificationBombFixture = "../../../../testdata/openapi/amplification_alias_bomb.yaml"

func TestAliasAmplification_BombFixtureIsRefused(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	diags := scanBytes(t, data)
	require.NotEmpty(t, diags, "an amplifying document must be diagnosed")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.NotZero(t, diags[0].Provenance.Position, "position provenance")
}

const bigDocSchemaCount = 2000

func bigAliasFreeSpec(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d: {type: object, properties: {a: {type: string}, b: {type: integer}, c: {type: boolean}}}\n", i)
	}
	return b.String()
}

func TestAliases_LargeAliasFreeDocumentIsClean(t *testing.T) {
	t.Parallel()
	src := bigAliasFreeSpec(bigDocSchemaCount)

	raw := indexOf(t, []byte(src)).Nodes()
	require.Greater(t, raw, minExpandedNodes,
		"the fixture must actually exceed the floor for this test to prove anything")

	assert.Empty(t, scanBytes(t, []byte(src)),
		"a large alias-free document must never be refused: what it costs is what its own bytes already bought")
}

func TestAliases_AnchorReuseWithinBudgetIsClean(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n")
	b.WriteString("x-anchors: {base: &base {type: object, properties: {a: {type: string}, b: {type: integer}}}}\n")
	b.WriteString("components:\n  schemas:\n")
	const reuses = 40
	for i := range reuses {
		fmt.Fprintf(&b, "    S%d: {properties: {p: *base}}\n", i)
	}

	assert.Empty(t, scanBytes(t, []byte(b.String())),
		"ordinary anchor reuse well under the budget is not amplification")
}

func wideBaseReuseSpec(props, siblings int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	b.WriteString("    Base: &base {type: object, properties: {")
	for i := range props {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "p%d: {type: string, description: 'field %d'}", i, i)
	}
	b.WriteString("}, required: [")
	for i := range props {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "p%d", i)
	}
	b.WriteString("]}\n")
	for i := range siblings {
		fmt.Fprintf(&b, "    S%d: {allOf: [*base, {type: object}]}\n", i)
	}
	return b.String()
}

func TestAliases_RealWorldAnchorReuseIsClean(t *testing.T) {
	t.Parallel()
	const props = 20
	const siblings = 900
	src := wideBaseReuseSpec(props, siblings)

	docRoot := indexOf(t, []byte(src)).Root()
	raw := rawNodes(t, docRoot)
	probe := newAliasWeigher(budget.AtMost(raw * 1000))
	exceeded := len(probe.weigh(t.Context(), docRoot)) > 0
	require.False(t, exceeded, "sanity: the probe's own allowance must not itself be crossed")
	expanded := probe.weight[docRoot]
	surplus := expanded - raw

	const realWorldWorstRaw = 5_765
	const realWorldWorstSurplus = 15_727
	require.GreaterOrEqual(t, raw, realWorldWorstRaw,
		"sanity: this fixture's raw node count must meet or exceed the worst real spec measured")
	require.GreaterOrEqual(t, surplus, realWorldWorstSurplus,
		"sanity: this fixture's surplus must meet or exceed the worst real spec measured")

	assert.Empty(t, scanBytes(t, []byte(src)),
		"ordinary DRY reuse of one shared base across many sibling schemas, at least as demanding as the worst real spec measured, is not amplification")
}

func TestAliases_SyntheticWideBaseReuseIsNowRefused(t *testing.T) {
	t.Parallel()
	const props = 200
	const siblings = 500
	src := wideBaseReuseSpec(props, siblings)

	root := indexOf(t, []byte(src)).Root()
	pastShape := len(newAliasWeigher(shapeAllowance(rawNodes(t, root))).weigh(t.Context(), root)) > 0
	require.True(t, pastShape,
		"sanity: this document expands past maxAliasAmplification times itself, so the ratio names it and no budget admits it")

	diags := scanBytes(t, []byte(src))
	require.NotEmpty(t, diags, "44x beyond any real spec's surplus must be refused")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)

	unbounded := Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.Unbounded())
	require.NotEmpty(t, unbounded, "turning the alias budget off leaves the ratio rule standing")
	assert.Equal(t, diag.AliasAmplification, unbounded[0].Code)
}

// flatFanOutSpec writes a component listing n aliases to one four-property
// leaf: a document whose ratio settles near the leaf's size however large n
// grows, so only the surplus bound can refuse it.
func flatFanOutSpec(n int) string {
	const props = 4
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	b.WriteString("    Leaf: &leaf {type: object, properties: {")
	for i := range props {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "p%d: {type: string}", i)
	}
	b.WriteString("}, required: [")
	for i := range props {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "p%d", i)
	}
	b.WriteString("]}\n")
	b.WriteString("    Bomb: {allOf: [")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("*leaf")
	}
	b.WriteString("]}\n")
	return b.String()
}

func TestAliases_FlatFanOutOfModestAnchorIsEventuallyRefused(t *testing.T) {
	t.Parallel()
	const under = 9_000
	assert.Empty(t, scanBytes(t, []byte(flatFanOutSpec(under))),
		"a modest anchor reused this many times has not yet crossed the surplus budget")

	const over = 11_000
	src := flatFanOutSpec(over)

	docRoot := indexOf(t, []byte(src)).Root()
	raw := rawNodes(t, docRoot)
	probe := newAliasWeigher(budget.AtMost(raw * 1000))
	exceeded := len(probe.weigh(t.Context(), docRoot)) > 0
	require.False(t, exceeded, "sanity: the probe's own allowance must not itself be crossed")
	expanded := probe.weight[docRoot]
	require.Less(t, expanded, maxAliasAmplification*raw,
		"sanity: this document's ratio must stay under maxAliasAmplification, so the refusal below is provably the surplus bound's doing, not the ratio's")

	diags := scanBytes(t, []byte(src))
	require.NotEmpty(t, diags, "unbounded reuse of even a modest anchor must eventually be refused")
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code,
		"inside the ratio, the document is large once expanded rather than a bomb, so it is past a budget")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, fmt.Sprintf("past the %d-node alias budget", defaultSurplus))

	assert.Empty(t, Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.AtMost(2*defaultSurplus)),
		"a caller who raised the budget has the document admitted")
	assert.Empty(t, Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.Unbounded()),
		"a caller who turned the budget off has the document admitted")
}

// TestAliasAmplification_SurplusBoundIsExact pins where the surplus budget
// binds: a document whose aliases add exactly the budget is admitted, and one
// node fewer of budget refuses it.
func TestAliasAmplification_SurplusBoundIsExact(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte(flatFanOutSpec(100))).Root()
	raw := rawNodes(t, root)
	probe := newAliasWeigher(shapeAllowance(raw))
	pastShape := len(probe.weigh(t.Context(), root)) > 0
	require.False(t, pastShape, "sanity: the fixture is inside the ratio, so only the surplus can refuse it")
	added := probe.weight[root] - raw
	require.Positive(t, added, "sanity: the fixture's aliases add nodes")

	_, refused := aliasAmplification(t.Context(), InSource(0), root, raw, budget.AtMost(added))
	assert.False(t, refused, "aliases adding exactly the budget are within it")

	d, refused := aliasAmplification(t.Context(), InSource(0), root, raw, budget.AtMost(added-1))
	require.True(t, refused, "aliases adding one node past the budget are refused")
	assert.Equal(t, diag.BudgetExceeded, d.Code)
	assert.Equal(t, fmt.Sprintf("YAML aliases add at least %d nodes to a %d-node document, past the %d-node alias budget",
		added, raw, added-1), d.Message)
	assert.NotZero(t, d.Provenance.Position, "the refusal points at the node that crossed")
}

// TestAliasAmplification_ABombIsNamedForItsShapeWhateverTheBudget pins the
// precedence of the two bounds. A bomb crosses any budget small enough to bind
// before the ratio as well as the ratio itself, and naming it for the budget
// would tell its caller that raising the budget admits it, which none does.
func TestAliasAmplification_ABombIsNamedForItsShapeWhateverTheBudget(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	for name, surplus := range map[string]budget.Limit{
		"a budget of one": budget.AtMost(1), "the default": budget.AtMost(defaultSurplus), "no budget": budget.Unbounded(),
	} {
		diags := Aliases(t.Context(), InSource(0), indexOf(t, data), surplus)
		require.Len(t, diags, 1, name)
		assert.Equal(t, diag.AliasAmplification, diags[0].Code, name)
	}
}

// TestAliasAmplification_ABudgetPastTheRatioRefusesNothing covers a surplus at
// or past what the ratio already admits. It can never be the bound crossed,
// and the largest one added to the document's size is held at math.MaxInt
// rather than wrapping to a negative allowance that refuses everything.
func TestAliasAmplification_ABudgetPastTheRatioRefusesNothing(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte(flatFanOutSpec(11_000))).Root()
	raw := rawNodes(t, root)

	for _, surplus := range []int{shapeAllowance(raw).Cap() - raw, math.MaxInt} {
		_, refused := aliasAmplification(t.Context(), InSource(0), root, raw, budget.AtMost(surplus))
		assert.False(t, refused, "surplus %d", surplus)
	}
}

// TestAliasAmplification_AnAdmittedDocumentIsWeighedOnce pins the cost of the
// common case. Every compile of a document that aliases nothing out of the way
// runs this, so two bounds must not mean two walks for it: a second walk is
// paid only by a document already being refused. It compares allocations
// rather than time, since each walk builds a weight map as large as the tree
// and allocation counts do not depend on what else the machine is doing.
func TestAliasAmplification_AnAdmittedDocumentIsWeighedOnce(t *testing.T) {
	root := indexOf(t, []byte(flatFanOutSpec(2_000))).Root()
	raw := rawNodes(t, root)
	shape := shapeAllowance(raw).Cap()
	surplus := (shape - raw) / 2
	probe := newAliasWeigher(budget.AtMost(raw + surplus))
	exceeded := len(probe.weigh(t.Context(), root)) > 0
	require.False(t, exceeded, "sanity: the document is inside the budget, so it is admitted")
	require.Less(t, surplus, shape-raw, "sanity: the budget binds before the ratio, so both bounds are live")

	one := testing.AllocsPerRun(5, func() { newAliasWeigher(budget.AtMost(raw+surplus)).weigh(t.Context(), root) })
	both := testing.AllocsPerRun(5, func() { aliasAmplification(t.Context(), InSource(0), root, raw, budget.AtMost(surplus)) })
	assert.Less(t, both, 1.5*one, "an admitted document costs one walk (one=%v both=%v)", one, both)
}

// TestAliases_HoldsAnOverlayToTheBudgetItIsGiven pins that the overlay's scan
// reads the budget it is handed rather than a fixed one.
func TestAliases_HoldsAnOverlayToTheBudgetItIsGiven(t *testing.T) {
	t.Parallel()
	src := "a: &x {p: 1, q: 2}\nb: [*x, *x, *x, *x]\n"

	diags := Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.AtMost(1))
	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Empty(t, Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.Unbounded()), "no budget, and well inside the ratio")
}

// chainedAliasSpec writes a document whose Root schema is the top of a chain
// of levels anchors, each an allOf naming the one below it twice, so its alias
// expansion doubles with each level.
func chainedAliasSpec(levels int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  a0: &a0 {type: string}\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "  a%d: &a%d {allOf: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	fmt.Fprintf(&b, "components:\n  schemas:\n    Root: *a%d\n", levels)
	return b.String()
}

// TestAliases_ChainedAliasFanOutIsRefused refuses a millionfold
// expansion. Twenty levels keep this test finite even for a walk that lost its
// memo, a defect TestAliases_AllocatesLinearlyInItsInput reports.
func TestAliases_ChainedAliasFanOutIsRefused(t *testing.T) {
	t.Parallel()
	diags := scanBytes(t, []byte(chainedAliasSpec(20)))
	require.Len(t, diags, 1, "a fan-out this deep is refused, not silently accepted")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestAliases_MergeChainWithinBoundIsClean(t *testing.T) {
	t.Parallel()
	diags := scanBytes(t, []byte(ynode.MergeChainSpec(nodeview.MergeDepthLimit)))
	assert.Empty(t, diags, "a merge chain of this depth is clean")
}

// TestAliases_MergeChainPastBoundIsRefused pins a merge chain longer than
// nodeview.MergeDepthLimit and past the alias allowance with room to spare. The
// scan does not read merge keys through the mapping view, whose depth bound
// would make it report an incomplete scan beside the refusal, so the refusal
// stands alone.
func TestAliases_MergeChainPastBoundIsRefused(t *testing.T) {
	t.Parallel()
	diags := scanBytes(t, []byte(ynode.MergeChainSpec(500)))
	require.Len(t, diags, 1, "the amplification refusal, and no incomplete-scan warning beside it")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity,
		"the document's alias expansion is refused outright")
}

// TestAliases_AllocatesLinearlyInItsInput holds Aliases to a cost linear in its
// input, on each shape a weigher without its memo blows up on. A row is
// measured at n, 2n and 4n, which must take one path through the scan, and the
// second doubling may add at most 2.5 times what the first did: a linear walk
// adds twice as much, a quadratic one four times, and the fixed cost cancels.
// Allocations do not move with load. It runs serially, since AllocsPerRun
// counts the whole process.
func TestAliases_AllocatesLinearlyInItsInput(t *testing.T) {
	rows := []struct {
		name string
		spec func(n int) string
		n    int
	}{
		// Sizes the expansion ratio admits keep one path, and a weigher that
		// lost its memo finite.
		{"chained aliases", chainedAliasSpec, 2},
		{"merge chain", ynode.MergeChainSpec, 80},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			sizes := []int{row.n, 2 * row.n, 4 * row.n}
			allocs := make([]float64, len(sizes))
			paths := make([][]string, len(sizes))
			for i, size := range sizes {
				idx := indexOf(t, []byte(row.spec(size)))
				var diags []ir.Diagnostic
				// No alias budget, so only the expansion ratio refuses, and it
				// refuses none of these sizes.
				allocs[i] = testing.AllocsPerRun(1, func() { diags = Aliases(t.Context(), InSource(0), idx, budget.Unbounded()) })
				for _, d := range diags {
					paths[i] = append(paths[i], d.Code)
				}
			}
			require.Equal(t, paths[0], paths[1], "sizes %v must take one path through the scan", sizes)
			require.Equal(t, paths[0], paths[2], "sizes %v must take one path through the scan", sizes)

			first, second := allocs[1]-allocs[0], allocs[2]-allocs[1]
			require.Positive(t, first, "a doubling that adds nothing measures nothing")
			assert.LessOrEqual(t, second, 2.5*first,
				"sizes %v took %v allocations: the second doubling added over 2.5 times the first", sizes, allocs)
		})
	}
}

func TestShapeAllowance_FloorsTheRatio(t *testing.T) {
	t.Parallel()
	assert.Equal(t, budget.AtMost(minExpandedNodes), shapeAllowance(10), "a small document gets the floor")
	assert.Equal(t, budget.AtMost(maxAliasAmplification*1000), shapeAllowance(1000), "a larger one gets the ratio")
	assert.Equal(t, budget.AtMost(math.MaxInt), shapeAllowance(math.MaxInt/maxAliasAmplification+1),
		"and one too large to multiply is held at math.MaxInt rather than wrapping")
}

func aliasFanOutNode(levels int) *yaml.Node {
	cur := ynode.Map(ynode.Scalar("type"), ynode.Scalar("string"))
	for range levels {
		cur = ynode.Map(ynode.Scalar("allOf"), ynode.Seq(ynode.Alias(cur), ynode.Alias(cur)))
	}
	return cur
}

func TestAliasAmplification_BoundaryPair(t *testing.T) {
	t.Parallel()

	under := aliasFanOutNode(12)
	require.Equal(t, 5, rawNodes(t, under), "sanity: the raw count aliasFanOutNode promises")
	_, refused := aliasAmplification(t.Context(), InSource(0), under, rawNodes(t, under), budget.AtMost(defaultSurplus))
	assert.False(t, refused, "expandedWeight 24,573 stays under the 32,768 floor")

	over := aliasFanOutNode(13)
	require.Equal(t, 5, rawNodes(t, over), "sanity: the raw count aliasFanOutNode promises")
	d, refused := aliasAmplification(t.Context(), InSource(0), over, rawNodes(t, over), budget.AtMost(defaultSurplus))
	require.True(t, refused, "expandedWeight 49,149 crosses the 32,768 floor")
	assert.Equal(t, diag.AliasAmplification, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
}

func TestAliasWeigher_NilRoot(t *testing.T) {
	t.Parallel()
	assert.Empty(t, newAliasWeigher(budget.AtMost(100)).weigh(t.Context(), nil))
}

func TestExpandedWeight_NoAliasesEqualsRawCount(t *testing.T) {
	t.Parallel()
	root := ynode.Map(
		ynode.Scalar("a"), ynode.Scalar("1"),
		ynode.Scalar("b"), ynode.Seq(ynode.Scalar("x"), ynode.Scalar("y"), ynode.Map(ynode.Scalar("c"), ynode.Scalar("2"))),
		ynode.Scalar("d"), ynode.Map(ynode.Scalar("e"), ynode.Scalar("3"), ynode.Scalar("f"), ynode.Scalar("4")),
	)
	raw := rawNodes(t, root)

	w := newAliasWeigher(budget.AtMost(raw + 1000)) // an allowance nothing here can cross
	exceeded := len(w.weigh(t.Context(), root)) > 0
	require.False(t, exceeded)
	assert.Equal(t, raw, w.weight[root], "an alias-free tree's expandedWeight is exactly its own node count")
}

func TestAliasWeigher_AliasToSubtreeMultiplies(t *testing.T) {
	t.Parallel()
	base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")) // weight 3: itself, one key, one value
	const reuses = 5
	aliases := make([]*yaml.Node, reuses)
	for i := range aliases {
		aliases[i] = ynode.Alias(base)
	}
	root := ynode.Map(ynode.Scalar("k"), ynode.Seq(aliases...))

	w := newAliasWeigher(budget.AtMost(1000))
	exceeded := len(w.weigh(t.Context(), root)) > 0
	require.False(t, exceeded)
	assert.Equal(t, 3, w.weight[base])
	assert.Equal(t, 18, w.weight[root],
		"root = 1(itself) + 1(key \"k\") + [1(seq) + 5*3(each alias)] = 18")
}

func TestAliasWeigher_MemoizesSharedTarget(t *testing.T) {
	t.Parallel()
	base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	const reuses = 25
	aliases := make([]*yaml.Node, reuses)
	for i := range aliases {
		aliases[i] = ynode.Alias(base)
	}
	root := ynode.Map(ynode.Scalar("k"), ynode.Seq(aliases...))

	w := newAliasWeigher(budget.AtMost(1_000_000))
	exceeded := len(w.weigh(t.Context(), root)) > 0
	require.False(t, exceeded)

	assert.Equal(t, len(w.weight), w.computations,
		"every distinct node — base included, despite 25 aliases pointing at it — is computed exactly once")
	// Distinct nodes: root, its key "k", the sequence, 25 alias nodes, base,
	// and base's own key and value: 3 + 25 + 3 = 31.
	assert.Equal(t, 31, w.computations)
}

func TestAliasWeigher_SaturatesWithoutOverflow(t *testing.T) {
	t.Parallel()
	root := aliasFanOutNode(40)

	w := newAliasWeigher(budget.AtMost(1000))
	culprits := w.weigh(t.Context(), root)
	require.Len(t, culprits, 1, "a 40-level doubling fan-out crosses any modest allowance, in one innermost place")
	culprit := culprits[0]
	assert.Equal(t, w.allowance.Past(), w.weight[culprit], "an exceeding weight always lands exactly on the first count past the allowance")
	assert.Positive(t, w.weight[culprit], "the saturated value must not have wrapped negative")
}

func TestChildrenOf_AliasWithoutTarget(t *testing.T) {
	t.Parallel()
	orphan := &yaml.Node{Kind: yaml.AliasNode}
	assert.Nil(t, childrenOf(orphan), "an alias with no target depends on nothing")

	pair := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	assert.Equal(t, pair.Content, childrenOf(pair), "every other node depends on its own Content")
}

func TestAliasWeigher_AliasWithoutTargetWeighsZero(t *testing.T) {
	t.Parallel()
	orphan := &yaml.Node{Kind: yaml.AliasNode}
	root := ynode.Map(ynode.Scalar("k"), orphan)

	w := newAliasWeigher(budget.AtMost(1000))
	exceeded := len(w.weigh(t.Context(), root)) > 0
	require.False(t, exceeded)
	assert.Equal(t, 0, w.weight[orphan], "an alias with no target substitutes nothing")
	assert.Equal(t, 2, w.weight[root], "root = 1(itself) + 1(key) + 0(the orphan alias)")
}

func TestAliasWeigher_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root.Content = append(root.Content, nil)

	w := newAliasWeigher(budget.AtMost(1000))
	exceeded := len(w.weigh(t.Context(), root)) > 0
	require.False(t, exceeded)
	assert.Equal(t, 3, w.weight[root], "the nil child contributes nothing and costs no dereference")
}

func TestAliasWeigher_InFlightCycleSaturates(t *testing.T) {
	t.Parallel()
	loop := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	loop.Content = append(loop.Content, ynode.Alias(loop)) // loop's own subtree aliases loop

	w := newAliasWeigher(budget.AtMost(1000))
	culprits := w.weigh(t.Context(), loop)
	require.NotEmpty(t, culprits, "a cyclic alias graph expands without bound and must be refused")
	culprit := culprits[0]
	assert.Equal(t, w.allowance.Past(), w.weight[culprit], "the cycle saturates rather than looping")
}

// TestYAMLUnmarshal_IntoANodeSpendsNoAliasBudget pins the yaml.v3 fact the alias
// weigher's reason for existing rests on, and that Aliases's comment states: the
// library's excessive-aliasing guard counts expansions, and decoding into a
// yaml.Node performs none, so the bomb this package refuses decodes into a tree
// without a word from the library. The Go-value decode beside it is the control
// that the guard exists and would fire on the same bytes if anything expanded
// them. A yaml.v3 upgrade that changed either answer would change what this
// package's refusal is for, and should be met here rather than in the comment.
func TestYAMLUnmarshal_IntoANodeSpendsNoAliasBudget(t *testing.T) {
	t.Parallel()
	bomb := readReproducer(t, "amplification_alias_bomb")

	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal(bomb, &tree), "a node tree holds an alias as a pointer; nothing is expanded")

	var value any
	err := yaml.Unmarshal(bomb, &value)
	require.Error(t, err, "the same bytes expanded into a Go value trip the guard")
	assert.Contains(t, err.Error(), "excessive aliasing")
}

// TestAliasAmplification_NamesTheLeastInnermostAmplifier pins the site of an
// alias bomb: the smallest structure already past the allowance, l4 of laughs'
// eight levels, not the bomb or the level holding it; and of two bombs, the one
// with the least pointer, in either order.
func TestAliasAmplification_NamesTheLeastInnermostAmplifier(t *testing.T) {
	t.Parallel()
	for order, data := range bothOrders([]string{laughs("b"), laughs("a")}) {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			d := onlyRefusal(t, data)
			assert.Equal(t, diag.AliasAmplification, d.Code)
			assert.Equal(t, jsontext.Pointer("/a/l4"), d.Provenance.Pointer)
		})
	}
}

// TestAliasAmplification_NamesTheLeastSiteUnderABudget pins the two budgeted
// branches, in either order: two lists each adding more than the budget allows
// but staying inside the ratio, named for the budget; and, under the same
// budget, two bombs past the ratio, named for their shape.
func TestAliasAmplification_NamesTheLeastSiteUnderABudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		code    string
		want    jsontext.Pointer
	}{
		{"past the budget", []string{fanned("b"), fanned("a")}, diag.BudgetExceeded, "/a/u"},
		{"past the ratio", []string{laughs("b"), laughs("a")}, diag.AliasAmplification, "/a/l4"},
	}
	for _, tc := range tests {
		for order, data := range bothOrders(tc.members) {
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				root := indexOf(t, data).Root()
				d, refused := aliasAmplification(t.Context(), InTree(0, Pointers(t.Context(), root)), root, rawNodes(t, root), budget.AtMost(100))
				require.True(t, refused)
				assert.Equal(t, tc.code, d.Code)
				assert.Equal(t, tc.want, d.Provenance.Pointer)
			})
		}
	}
}

// fanned is an anchor of ten scalars under name, aliased twenty times in a
// list beside it.
func fanned(name string) string {
	return fmt.Sprintf("%s: {x: &%s [1, 1, 1, 1, 1, 1, 1, 1, 1, 1], u: [%s]}\n", name, name,
		strings.TrimSuffix(strings.Repeat("*"+name+", ", 20), ", "))
}

// laughs is a billion-laughs bomb named prefix: eight anchors, the first a list
// of ten scalars and each after it a list of ten aliases to the one before.
func laughs(prefix string) string {
	const levels = 8
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n  l0: &%s0 [x, x, x, x, x, x, x, x, x, x]\n", prefix, prefix)
	for i := 1; i < levels; i++ {
		aliases := strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*%s%d, ", prefix, i-1), 10), ", ")
		fmt.Fprintf(&b, "  l%d: &%s%d [%s]\n", i, prefix, i, aliases)
	}
	return b.String()
}

// TestWeigh_StopsOnceTheContextIsDone pins the weigher's context, asked
// before each step of its walk: a document past its allowance, weighed under a
// done context, draws no amplifier and weighs nothing, and cancelled partway
// the weigher weighs no node more than the steps it was let take.
func TestWeigh_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte(wideBaseReuseSpec(200, 500))).Root()
	allowance := shapeAllowance(rawNodes(t, root))
	live := newAliasWeigher(allowance)
	require.NotEmpty(t, live.weigh(t.Context(), root), "live, the document is past its allowance")

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	stopped := newAliasWeigher(allowance)
	assert.Empty(t, stopped.weigh(cancelled, root))
	assert.Zero(t, stopped.computations)

	partway := newAliasWeigher(allowance)
	partway.weigh(&ctxtest.Countdown{Context: t.Context(), Left: 100}, root)
	assert.Positive(t, partway.computations)
	assert.LessOrEqual(t, partway.computations, 100)
	assert.Less(t, partway.computations, live.computations)
}
