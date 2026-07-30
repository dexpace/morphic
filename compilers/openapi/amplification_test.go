package openapi

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// amplificationBombFixture is the GitHub #27 reproducer: a 10-level x 10-way
// YAML alias fan-out whose 739 parsed bytes stand in for an alias-expanded
// form past 37 billion nodes.
const amplificationBombFixture = "../../testdata/openapi/amplification_alias_bomb.yaml"

// TestAliasAmplification_BombFixtureIsRefused pins the bomb at the detector
// boundary: an error diagnostic with the new code and line:col provenance,
// the same shape TestDetectCycles_Reproducers pins for the older cycle
// classes.
func TestAliasAmplification_BombFixtureIsRefused(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	diags := detectCycles(0, data)
	require.NotEmpty(t, diags, "an amplifying document must be diagnosed")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.NotEmpty(t, diags[0].Provenance.Pointer, "line:col provenance")
}

// TestCompile_AliasBombDoesNotExhaustMemory drives the bomb through the full
// public Compile path, mirroring TestCompile_CyclicSpecDoesNotCrash's shape: a
// nil document and an error diagnostic, never a Go error, since an amplifying
// document is a spec problem, not I/O. It runs on a goroutine with a bound
// tight enough that soa.Unmarshal could never have been reached and returned
// in time — a timeout here means the bomb slipped past the pre-parse scan and
// reached the parser, the exact failure GitHub #27 reports. A regression
// therefore leaks the goroutine for the rest of the test binary's life; that
// is the deliberate trade scanWithin (cycles_test.go) already makes for a
// fast, legible failure, and it cannot leak on a pass.
func TestCompile_AliasBombDoesNotExhaustMemory(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	type result struct {
		doc   *ir.Document
		diags []ir.Diagnostic
		err   error
	}
	done := make(chan result, 1)
	go func() {
		doc, diags, cerr := New().Compile(t.Context(),
			[]compilers.Source{{Path: "amplification_alias_bomb.yaml", Data: data}}, compilers.Options{})
		done <- result{doc, diags, cerr}
	}()

	const bound = 5 * time.Second
	select {
	case r := <-done:
		require.NoError(t, r.err, "an alias bomb is a spec problem, not a Go error")
		assert.Nil(t, r.doc, "the compiler refuses to lower an amplifying document")
		assertHasErrorCode(t, r.diags, diag.AliasAmplification)
	case <-time.After(bound):
		t.Fatalf("Compile did not return within %v — the bomb likely reached soa.Unmarshal", bound)
	}
}

// bigDocSchemaCount is sized so bigAliasFreeSpec's total raw node count clears
// minExpandedNodes (32,768) by a comfortable margin: proof requires actually
// exceeding the floor, not merely approaching it.
const bigDocSchemaCount = 2000

// bigAliasFreeSpec builds a large document with no aliases at all: n small,
// distinct component schemas, each with a handful of properties so the whole
// document comfortably clears minExpandedNodes on raw node count alone.
func bigAliasFreeSpec(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d: {type: object, properties: {a: {type: string}, b: {type: integer}, c: {type: boolean}}}\n", i)
	}
	return b.String()
}

// TestDetectCycles_LargeAliasFreeDocumentIsClean is the critical false-positive
// guard: it proves the budget is amplification-relative, not an absolute
// ceiling. bigAliasFreeSpec has no aliases at all, so its expandedWeight equals
// its own rawNodeCount exactly — and that raw count is verified (not assumed)
// to exceed minExpandedNodes before the real assertion runs, so this document
// is exactly the shape a regression that treated minExpandedNodes as an
// absolute cap (rather than a floor under the ratio) would wrongly refuse.
func TestDetectCycles_LargeAliasFreeDocumentIsClean(t *testing.T) {
	t.Parallel()
	src := bigAliasFreeSpec(bigDocSchemaCount)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	raw := rawNodeCount(documentRoot(&root))
	require.Greater(t, raw, int64(minExpandedNodes),
		"the fixture must actually exceed the floor for this test to prove anything")

	assert.Empty(t, detectCycles(0, []byte(src)),
		"a large alias-free document must never be refused: what it costs is what its own bytes already bought")
}

// TestDetectCycles_AnchorReuseWithinBudgetIsClean pins ordinary, legal anchor
// reuse: one anchored schema aliased from many sibling schemas (not repeated
// within a single one, unlike the fan-out bomb), well under the allowance.
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

	assert.Empty(t, detectCycles(0, []byte(b.String())),
		"ordinary anchor reuse well under the budget is not amplification")
}

// wideBaseReuseSpec builds a document with one YAML-anchored base schema of
// props fields pulled into siblings sibling schemas via `allOf: [*base,
// {type: object}]` — ordinary DRY inheritance, the shape
// maxAliasAmplification's doc comment cites by exact measurement.
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

// TestDetectCycles_RealWorldAnchorReuseIsClean pins ordinary DRY anchor reuse —
// one base schema shared by many siblings through allOf, not recursion — at the
// worst profile measured across 1,693 real specs (labrinth_dart's openapi.yaml:
// raw 5,765, surplus 15,727). The sanity checks below confirm this fixture
// meets or exceeds that profile before asserting it is clean, so the test is
// demonstrably at least as demanding as the worst real document, not merely
// shaped like it.
func TestDetectCycles_RealWorldAnchorReuseIsClean(t *testing.T) {
	t.Parallel()
	const props = 20
	const siblings = 900
	src := wideBaseReuseSpec(props, siblings)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	docRoot := documentRoot(&root)
	raw := rawNodeCount(docRoot)
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

	assert.Empty(t, detectCycles(0, []byte(src)),
		"ordinary DRY reuse of one shared base across many sibling schemas, at least as demanding as the worst real spec measured, is not amplification")
}

// TestDetectCycles_SyntheticWideBaseReuseIsNowRefused pins the far more extreme
// synthetic shape that once justified a looser bound: a 200-field base reused
// across 500 siblings (surplus ≈703,000, ratio ≈130.6). That is 44x the worst
// real surplus and costs roughly 2 GiB to compile, so refusing it is deliberate.
// Keeping it pinned forces a future change to justify reopening that door.
func TestDetectCycles_SyntheticWideBaseReuseIsNowRefused(t *testing.T) {
	t.Parallel()
	const props = 200
	const siblings = 500
	src := wideBaseReuseSpec(props, siblings)

	diags := detectCycles(0, []byte(src))
	require.NotEmpty(t, diags, "44x beyond any real spec's surplus must be refused")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// flatFanOutSpec builds a document with one modest anchor (props fields)
// referenced n times in a single flat allOf list — the shape whose ratio
// converges to the anchor's own weight and so never grows with n, however
// large n is made.
func flatFanOutSpec(props, n int) string {
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

// TestDetectCycles_FlatFanOutOfModestAnchorIsEventuallyRefused pins the
// maxAliasSurplus backstop: a 4-field anchor (a small, entirely ordinary
// schema) referenced directly, many times, in one flat allOf list. Its ratio
// converges to 27 (measured) — comfortably under maxAliasAmplification at any
// reuse count, per the anchor's own weight rather than the number of
// references — so no ratio threshold this file could choose would ever
// refuse it, while real memory keeps growing with every added reference.
// maxAliasSurplus is what refuses it once that growth passes a fixed, finite
// bound. Each added reference costs one raw node and contributes the anchor's
// whole weight, so the surplus grows by 26 per reference (measured): 9,000
// references (surplus = 234,000) stays clean and 11,000 (surplus = 286,000)
// is refused, either side of the 1<<18 budget. The sanity checks confirm the
// ratio at the refused size is still nowhere near maxAliasAmplification —
// proving the surplus bound, not the ratio, is what catches this shape.
func TestDetectCycles_FlatFanOutOfModestAnchorIsEventuallyRefused(t *testing.T) {
	t.Parallel()
	const props = 4

	const under = 9_000
	assert.Empty(t, detectCycles(0, []byte(flatFanOutSpec(props, under))),
		"a modest anchor reused this many times has not yet crossed the surplus budget")

	const over = 11_000
	src := flatFanOutSpec(props, over)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	docRoot := documentRoot(&root)
	raw := rawNodeCount(docRoot)
	probe := newAliasWeigher(raw * 1000)
	_, exceeded := probe.weigh(docRoot)
	require.False(t, exceeded, "sanity: the probe's own allowance must not itself be crossed")
	expanded := probe.weight[docRoot]
	require.Less(t, expanded, int64(maxAliasAmplification)*raw,
		"sanity: this document's ratio must stay under maxAliasAmplification, so the refusal below is provably the surplus bound's doing, not the ratio's")

	diags := detectCycles(0, []byte(src))
	require.NotEmpty(t, diags, "unbounded reuse of even a modest anchor must eventually be refused")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestComputeAllowance_TakesTheLesserBound pins computeAllowance's contract
// directly: it returns whichever of the ratio-relative and absolute-surplus
// allowances is smaller, so a document must clear both to be accepted.
func TestComputeAllowance_TakesTheLesserBound(t *testing.T) {
	t.Parallel()

	// A small raw count: the minExpandedNodes floor dominates the ratio side,
	// and maxAliasSurplus is far larger still, so the floor wins outright.
	assert.Equal(t, int64(minExpandedNodes), computeAllowance(10))

	// A raw count large enough that the ratio allowance
	// (maxAliasAmplification*raw) exceeds maxAliasSurplus: the surplus side
	// must win.
	const largeRaw = int64(maxAliasSurplus)
	ratioAllowance := int64(maxAliasAmplification) * largeRaw
	surplusAllowance := largeRaw + int64(maxAliasSurplus)
	require.Less(t, surplusAllowance, ratioAllowance,
		"sanity: this raw count must actually put the surplus side in the lead")
	assert.Equal(t, surplusAllowance, computeAllowance(largeRaw))
}

// aliasFanOutNode builds, as bare *yaml.Node values rather than parsed YAML
// text, the same doubling shape as the amplification bomb fixture: level i
// is {allOf: [*level(i-1), *level(i-1)]}, so expandedWeight follows the
// recurrence w(0)=3, w(i)=3+2*w(i-1). Built this way rather than through YAML
// text and x-anchors, rawNodeCount(aliasFanOutNode(levels)) is a constant 5
// for any levels>=1 (the node's own mapping, its "allOf" key, the sequence,
// and its two alias elements — rawNodeCount does not follow either alias to
// count the level beneath), which is what lets the tests below pin an exact
// allowance (the minExpandedNodes floor, since neither 64*5 nor 5 plus
// maxAliasSurplus reaches it) rather than one that shifts with levels.
func aliasFanOutNode(levels int) *yaml.Node {
	cur := ymap(yscalar("type"), yscalar("string"))
	for range levels {
		cur = ymap(yscalar("allOf"), yseq(yalias(cur), yalias(cur)))
	}
	return cur
}

// TestAliasAmplification_BoundaryPair pins the constants rather than letting
// them be incidental: two documents from the same family, one level apart,
// straddle the minExpandedNodes floor. w(12)=24,573 stays under 32,768; the
// very next level, w(13)=49,149, crosses it. Both share the same rawNodeCount
// (5, see aliasFanOutNode), so the allowance is the same fixed floor in both
// cases and the pair isolates the weight computation, not a shifting ratio.
func TestAliasAmplification_BoundaryPair(t *testing.T) {
	t.Parallel()

	under := aliasFanOutNode(12)
	require.Equal(t, int64(5), rawNodeCount(under), "sanity: the raw count aliasFanOutNode promises")
	_, refused := aliasAmplification(0, under)
	assert.False(t, refused, "expandedWeight 24,573 stays under the 32,768 floor")

	over := aliasFanOutNode(13)
	require.Equal(t, int64(5), rawNodeCount(over), "sanity: the raw count aliasFanOutNode promises")
	d, refused := aliasAmplification(0, over)
	require.True(t, refused, "expandedWeight 49,149 crosses the 32,768 floor")
	assert.Equal(t, diag.AliasAmplification, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
}

// TestAliasWeigher_NilRoot pins that a nil root has no weight and is never
// refused: aliasAmplification's caller (scanCycles) already guarantees a nil
// root reaches nothing else either, but the weigher's own contract is pinned
// here directly.
func TestAliasWeigher_NilRoot(t *testing.T) {
	t.Parallel()
	culprit, exceeded := newAliasWeigher(100).weigh(nil)
	assert.Nil(t, culprit)
	assert.False(t, exceeded)
}

// TestExpandedWeight_NoAliasesEqualsRawCount pins the property the design
// leans on to argue the budget never refuses a genuinely large alias-free
// spec: with no alias anywhere in the tree, expandedWeight is exactly
// rawNodeCount, node for node.
func TestExpandedWeight_NoAliasesEqualsRawCount(t *testing.T) {
	t.Parallel()
	root := ymap(
		yscalar("a"), yscalar("1"),
		yscalar("b"), yseq(yscalar("x"), yscalar("y"), ymap(yscalar("c"), yscalar("2"))),
		yscalar("d"), ymap(yscalar("e"), yscalar("3"), yscalar("f"), yscalar("4")),
	)
	raw := rawNodeCount(root)

	w := newAliasWeigher(raw + 1000) // an allowance nothing here can cross
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, raw, w.weight[root], "an alias-free tree's expandedWeight is exactly its own node count")
}

// TestAliasWeigher_AliasToSubtreeMultiplies pins the arithmetic an alias
// contributes: five aliases to the same three-node subtree add 5*3, not 3,
// to their container's weight — substituting five copies of the target, not
// five references to one.
func TestAliasWeigher_AliasToSubtreeMultiplies(t *testing.T) {
	t.Parallel()
	base := ymap(yscalar("a"), yscalar("1")) // weight 3: itself, one key, one value
	const reuses = 5
	aliases := make([]*yaml.Node, reuses)
	for i := range aliases {
		aliases[i] = yalias(base)
	}
	root := ymap(yscalar("k"), yseq(aliases...))

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(3), w.weight[base])
	assert.Equal(t, int64(18), w.weight[root],
		"root = 1(itself) + 1(key \"k\") + [1(seq) + 5*3(each alias)] = 18")
}

// TestAliasWeigher_MemoizesSharedTarget pins that a node aliased many times is
// weighed exactly once, by construction rather than by timing: aliasWeigher
// counts every computeWeight call, so equality with the number of distinct
// nodes reached proves none was recomputed, however many aliases pointed at
// it.
func TestAliasWeigher_MemoizesSharedTarget(t *testing.T) {
	t.Parallel()
	base := ymap(yscalar("a"), yscalar("1"))
	const reuses = 25
	aliases := make([]*yaml.Node, reuses)
	for i := range aliases {
		aliases[i] = yalias(base)
	}
	root := ymap(yscalar("k"), yseq(aliases...))

	w := newAliasWeigher(1_000_000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)

	assert.Equal(t, int64(len(w.weight)), w.computations,
		"every distinct node — base included, despite 25 aliases pointing at it — is computed exactly once")
	// Distinct nodes: root, its key "k", the sequence, 25 alias nodes, base,
	// and base's own key and value: 3 + 25 + 3 = 31.
	assert.Equal(t, int64(31), w.computations)
}

// TestAliasWeigher_SaturatesWithoutOverflow pins the arithmetic safety net at
// a scale (40 levels of 2-way fan-out, the same shape
// TestDetectCycles_ChainedAliasFanOutIsRefusedFast drives through the full
// scan) whose true expandedWeight is far beyond int64 range to compute
// directly. With a small allowance, the walk must still terminate promptly,
// report the document refused, and never let a saturated weight wrap negative
// or otherwise misbehave: it must land exactly on the ceiling, since any total
// that crosses the allowance is clamped there before the walk can act on it.
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

// TestRawNodeCount_NilRoot pins the empty-document arm: documentRoot returns
// nil for a document with no content, and the count of nothing is zero rather
// than a panic on the walk's first dereference.
func TestRawNodeCount_NilRoot(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(0), rawNodeCount(nil))
}

// TestChildrenOf_AliasWithoutTarget pins the arm that keeps a malformed alias
// from being read as a node with children. yaml.v3 always populates Alias on a
// parsed alias node, so this is unreachable from a real document — but the
// weigher would dereference nil without it, and the whole point of the
// pre-parse scan is that it never faults on input the parser has not vetted.
func TestChildrenOf_AliasWithoutTarget(t *testing.T) {
	t.Parallel()
	orphan := &yaml.Node{Kind: yaml.AliasNode}
	assert.Nil(t, childrenOf(orphan), "an alias with no target depends on nothing")

	pair := ymap(yscalar("k"), yscalar("v"))
	assert.Equal(t, pair.Content, childrenOf(pair), "every other node depends on its own Content")
}

// TestAliasWeigher_AliasWithoutTargetWeighsZero is the weight-side half of the
// same guard: an alias standing in for nothing contributes nothing, so a
// document carrying one is neither refused nor able to fault the walk.
func TestAliasWeigher_AliasWithoutTargetWeighsZero(t *testing.T) {
	t.Parallel()
	orphan := &yaml.Node{Kind: yaml.AliasNode}
	root := ymap(yscalar("k"), orphan)

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(0), w.weight[orphan], "an alias with no target substitutes nothing")
	assert.Equal(t, int64(2), w.weight[root], "root = 1(itself) + 1(key) + 0(the orphan alias)")
}

// TestAliasWeigher_NilChildIsSkipped pins that a nil entry in a node's Content
// — which a parsed document never produces, but which a hand-built or
// third-party-mutated tree could — is passed over rather than dereferenced.
func TestAliasWeigher_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ymap(yscalar("k"), yscalar("v"))
	root.Content = append(root.Content, nil)

	w := newAliasWeigher(1000)
	_, exceeded := w.weigh(root)
	require.False(t, exceeded)
	assert.Equal(t, int64(3), w.weight[root], "the nil child contributes nothing and costs no dereference")
}

// TestAliasWeigher_InFlightCycleSaturates pins the defensive guard behind
// pushChildren's in-flight check. anchorCycle refuses every recursive anchor
// before this walk runs, so a cyclic alias graph is unreachable from a parsed
// document — but "unreachable" is an argument about a sibling function, not a
// property of this one, so the walk defends itself: re-entering a node whose
// own weight is still being computed saturates it at the ceiling rather than
// looping forever. The cycle is built directly as nodes, since no YAML text
// could produce one that anchorCycle would let through.
func TestAliasWeigher_InFlightCycleSaturates(t *testing.T) {
	t.Parallel()
	loop := ymap(yscalar("k"), yscalar("v"))
	loop.Content = append(loop.Content, yalias(loop)) // loop's own subtree aliases loop

	w := newAliasWeigher(1000)
	culprit, exceeded := w.weigh(loop)
	require.True(t, exceeded, "a cyclic alias graph expands without bound and must be refused")
	require.NotNil(t, culprit)
	assert.Equal(t, w.ceiling, w.weight[culprit], "the cycle saturates rather than looping")
}
