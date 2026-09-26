package scan

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

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

func TestDetectCycles_LargeAliasFreeDocumentIsClean(t *testing.T) {
	t.Parallel()
	src := bigAliasFreeSpec(bigDocSchemaCount)

	raw := indexOf(t, []byte(src)).Nodes()
	require.Greater(t, raw, int64(minExpandedNodes),
		"the fixture must actually exceed the floor for this test to prove anything")

	assert.Empty(t, scanBytes(t, []byte(src)),
		"a large alias-free document must never be refused: what it costs is what its own bytes already bought")
}

func TestDetectCycles_AnchorReuseWithinBudgetIsClean(t *testing.T) {
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

func TestDetectCycles_RealWorldAnchorReuseIsClean(t *testing.T) {
	t.Parallel()
	const props = 20
	const siblings = 900
	src := wideBaseReuseSpec(props, siblings)

	docRoot := indexOf(t, []byte(src)).Root()
	raw := rawNodes(docRoot)
	probe := newAliasWeigher(raw * 1000)
	_, exceeded := probe.weigh(docRoot)
	require.False(t, exceeded, "sanity: the probe's own allowance must not itself be crossed")
	expanded := probe.weight[docRoot]
	surplus := expanded - raw

	const realWorldWorstRaw = 5_765
	const realWorldWorstSurplus = 15_727
	require.GreaterOrEqual(t, raw, int64(realWorldWorstRaw),
		"sanity: this fixture's raw node count must meet or exceed the worst real spec measured")
	require.GreaterOrEqual(t, surplus, int64(realWorldWorstSurplus),
		"sanity: this fixture's surplus must meet or exceed the worst real spec measured")

	assert.Empty(t, scanBytes(t, []byte(src)),
		"ordinary DRY reuse of one shared base across many sibling schemas, at least as demanding as the worst real spec measured, is not amplification")
}

func TestDetectCycles_SyntheticWideBaseReuseIsNowRefused(t *testing.T) {
	t.Parallel()
	const props = 200
	const siblings = 500
	src := wideBaseReuseSpec(props, siblings)

	root := indexOf(t, []byte(src)).Root()
	_, pastShape := newAliasWeigher(shapeAllowance(rawNodes(root))).weigh(root)
	require.True(t, pastShape,
		"sanity: this document expands past maxAliasAmplification times itself, so the ratio names it and no budget admits it")

	diags := scanBytes(t, []byte(src))
	require.NotEmpty(t, diags, "44x beyond any real spec's surplus must be refused")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)

	unbounded := Cycles(InSource(0), indexOf(t, []byte(src)), 0)
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

func TestDetectCycles_FlatFanOutOfModestAnchorIsEventuallyRefused(t *testing.T) {
	t.Parallel()
	const under = 9_000
	assert.Empty(t, scanBytes(t, []byte(flatFanOutSpec(under))),
		"a modest anchor reused this many times has not yet crossed the surplus budget")

	const over = 11_000
	src := flatFanOutSpec(over)

	docRoot := indexOf(t, []byte(src)).Root()
	raw := rawNodes(docRoot)
	probe := newAliasWeigher(raw * 1000)
	_, exceeded := probe.weigh(docRoot)
	require.False(t, exceeded, "sanity: the probe's own allowance must not itself be crossed")
	expanded := probe.weight[docRoot]
	require.Less(t, expanded, int64(maxAliasAmplification)*raw,
		"sanity: this document's ratio must stay under maxAliasAmplification, so the refusal below is provably the surplus bound's doing, not the ratio's")

	diags := scanBytes(t, []byte(src))
	require.NotEmpty(t, diags, "unbounded reuse of even a modest anchor must eventually be refused")
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code,
		"inside the ratio, the document is large once expanded rather than a bomb, so it is past a budget")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, fmt.Sprintf("past the %d-node alias budget", defaultSurplus))

	assert.Empty(t, Cycles(InSource(0), indexOf(t, []byte(src)), 2*defaultSurplus),
		"a caller who raised the budget has the document admitted")
	assert.Empty(t, Cycles(InSource(0), indexOf(t, []byte(src)), 0),
		"a caller who turned the budget off has the document admitted")
}

// TestAliasAmplification_SurplusBoundIsExact pins where the surplus budget
// binds: a document whose aliases add exactly the budget is admitted, and one
// node fewer of budget refuses it.
func TestAliasAmplification_SurplusBoundIsExact(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte(flatFanOutSpec(100))).Root()
	raw := rawNodes(root)
	probe := newAliasWeigher(shapeAllowance(raw))
	_, pastShape := probe.weigh(root)
	require.False(t, pastShape, "sanity: the fixture is inside the ratio, so only the surplus can refuse it")
	added := probe.weight[root] - raw
	require.Positive(t, added, "sanity: the fixture's aliases add nodes")

	_, refused := aliasAmplification(InSource(0), root, raw, added)
	assert.False(t, refused, "aliases adding exactly the budget are within it")

	d, refused := aliasAmplification(InSource(0), root, raw, added-1)
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

	for _, surplus := range []int64{1, defaultSurplus, 0} {
		diags := Cycles(InSource(0), indexOf(t, data), surplus)
		require.Len(t, diags, 1, "surplus %d", surplus)
		assert.Equal(t, diag.AliasAmplification, diags[0].Code, "surplus %d", surplus)
	}
}

// TestAliasAmplification_ABudgetPastTheRatioRefusesNothing covers a surplus at
// or past what the ratio already admits. It can never be the bound crossed,
// and adding the largest one to the document's size would overflow into a
// negative allowance that refuses everything.
func TestAliasAmplification_ABudgetPastTheRatioRefusesNothing(t *testing.T) {
	t.Parallel()
	root := indexOf(t, []byte(flatFanOutSpec(11_000))).Root()
	raw := rawNodes(root)

	for _, surplus := range []int64{shapeAllowance(raw) - raw, math.MaxInt64} {
		_, refused := aliasAmplification(InSource(0), root, raw, surplus)
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
	raw := rawNodes(root)
	shape := shapeAllowance(raw)
	surplus := (shape - raw) / 2
	probe := newAliasWeigher(raw + surplus)
	_, exceeded := probe.weigh(root)
	require.False(t, exceeded, "sanity: the document is inside the budget, so it is admitted")
	require.Less(t, surplus, shape-raw, "sanity: the budget binds before the ratio, so both bounds are live")

	one := testing.AllocsPerRun(5, func() { newAliasWeigher(raw + surplus).weigh(root) })
	both := testing.AllocsPerRun(5, func() { aliasAmplification(InSource(0), root, raw, surplus) })
	assert.Less(t, both, 1.5*one, "an admitted document costs one walk (one=%v both=%v)", one, both)
}

// TestAliases_HoldsAnOverlayToTheBudgetItIsGiven pins that the overlay's scan
// reads the budget it is handed rather than a fixed one.
func TestAliases_HoldsAnOverlayToTheBudgetItIsGiven(t *testing.T) {
	t.Parallel()
	src := "a: &x {p: 1, q: 2}\nb: [*x, *x, *x, *x]\n"

	diags := Aliases(InSource(0), indexOf(t, []byte(src)), 1)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Empty(t, Aliases(InSource(0), indexOf(t, []byte(src)), 0), "no budget, and well inside the ratio")
}

func TestShapeAllowance_FloorsTheRatio(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(minExpandedNodes), shapeAllowance(10), "a small document gets the floor")
	assert.Equal(t, int64(maxAliasAmplification*1000), shapeAllowance(1000), "a larger one gets the ratio")
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
	require.Equal(t, int64(5), rawNodes(under), "sanity: the raw count aliasFanOutNode promises")
	_, refused := aliasAmplification(InSource(0), under, rawNodes(under), defaultSurplus)
	assert.False(t, refused, "expandedWeight 24,573 stays under the 32,768 floor")

	over := aliasFanOutNode(13)
	require.Equal(t, int64(5), rawNodes(over), "sanity: the raw count aliasFanOutNode promises")
	d, refused := aliasAmplification(InSource(0), over, rawNodes(over), defaultSurplus)
	require.True(t, refused, "expandedWeight 49,149 crosses the 32,768 floor")
	assert.Equal(t, diag.AliasAmplification, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
}

func TestAliasWeigher_NilRoot(t *testing.T) {
	t.Parallel()
	culprit, exceeded := newAliasWeigher(100).weigh(nil)
	assert.Nil(t, culprit)
	assert.False(t, exceeded)
}

func TestExpandedWeight_NoAliasesEqualsRawCount(t *testing.T) {
	t.Parallel()
	root := ynode.Map(
		ynode.Scalar("a"), ynode.Scalar("1"),
		ynode.Scalar("b"), ynode.Seq(ynode.Scalar("x"), ynode.Scalar("y"), ynode.Map(ynode.Scalar("c"), ynode.Scalar("2"))),
		ynode.Scalar("d"), ynode.Map(ynode.Scalar("e"), ynode.Scalar("3"), ynode.Scalar("f"), ynode.Scalar("4")),
	)
	raw := rawNodes(root)

	w := newAliasWeigher(raw + 1000) // an allowance nothing here can cross
	_, exceeded := w.weigh(root)
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

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(3), w.weight[base])
	assert.Equal(t, int64(18), w.weight[root],
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

	w := newAliasWeigher(1_000_000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)

	assert.Equal(t, int64(len(w.weight)), w.computations,
		"every distinct node — base included, despite 25 aliases pointing at it — is computed exactly once")
	// Distinct nodes: root, its key "k", the sequence, 25 alias nodes, base,
	// and base's own key and value: 3 + 25 + 3 = 31.
	assert.Equal(t, int64(31), w.computations)
}

func TestAliasWeigher_SaturatesWithoutOverflow(t *testing.T) {
	t.Parallel()
	root := aliasFanOutNode(40)

	w := newAliasWeigher(1000)
	culprit, exceeded := w.weigh(root)
	require.True(t, exceeded, "a 40-level doubling fan-out crosses any modest allowance")
	require.NotNil(t, culprit)
	assert.Equal(t, w.ceiling, w.weight[culprit], "an exceeding weight always lands exactly on the ceiling")
	assert.Greater(t, w.weight[culprit], int64(0), "the saturated value must not have wrapped negative")
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

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(0), w.weight[orphan], "an alias with no target substitutes nothing")
	assert.Equal(t, int64(2), w.weight[root], "root = 1(itself) + 1(key) + 0(the orphan alias)")
}

func TestAliasWeigher_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root.Content = append(root.Content, nil)

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(3), w.weight[root], "the nil child contributes nothing and costs no dereference")
}

func TestAliasWeigher_InFlightCycleSaturates(t *testing.T) {
	t.Parallel()
	loop := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	loop.Content = append(loop.Content, ynode.Alias(loop)) // loop's own subtree aliases loop

	w := newAliasWeigher(1000)
	culprit, exceeded := w.weigh(loop)
	require.True(t, exceeded, "a cyclic alias graph expands without bound and must be refused")
	require.NotNil(t, culprit)
	assert.Equal(t, w.ceiling, w.weight[culprit], "the cycle saturates rather than looping")
}

// TestYAMLUnmarshal_IntoANodeSpendsNoAliasBudget pins the yaml.v3 fact the alias
// weigher's reason for existing rests on, and that Cycles's comment states: the
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
