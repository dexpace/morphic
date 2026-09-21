package sourceindex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// decode is the one place these tests parse source text, so a fixture written as
// YAML is indexed over exactly the tree a compile would index.
func decode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	return &root
}

func TestBuild_EmptyDocumentIndexesNothing(t *testing.T) {
	t.Parallel()
	for name, root := range map[string]*yaml.Node{
		"no node at all":             nil,
		"a document with no content": {Kind: yaml.DocumentNode},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			idx := Build(root, MaxIndexedNodes)
			assert.Nil(t, idx.Root(), "an empty document has no content node")
			assert.Equal(t, int64(0), idx.Nodes())
			assert.False(t, idx.Truncated(), "nothing to index is fully indexed")
			_, found := idx.AnchorCycle()
			assert.False(t, found)
			_, found = idx.TaggedMapping()
			assert.False(t, found)
		})
	}
}

// TestBuild_WhitespaceOnlySourceIsOneEmptyNode records what yaml.v3 actually
// hands back for a source with no document in it: not a document node with no
// content, but a node of no kind at all. It indexes as the single node it is,
// which is why the scan reads such a source as carrying nothing rather than as
// having failed.
func TestBuild_WhitespaceOnlySourceIsOneEmptyNode(t *testing.T) {
	t.Parallel()
	idx := Build(decode(t, "\n\n\n"), MaxIndexedNodes)

	require.NotNil(t, idx.Root())
	assert.Equal(t, yaml.Kind(0), idx.Root().Kind, "yaml.v3 leaves the node untouched")
	assert.Equal(t, int64(1), idx.Nodes())
	_, found := idx.AnchorCycle()
	assert.False(t, found)
}

func TestBuild_RootIsTheDocumentsContent(t *testing.T) {
	t.Parallel()
	content := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{content}}

	assert.Same(t, content, Build(doc, MaxIndexedNodes).Root(),
		"a document node is unwrapped to the node every consumer scans from")
	assert.Same(t, content, Build(content, MaxIndexedNodes).Root(),
		"a content node passed directly is already the root")
}

func TestBuild_CountsEveryNodeOnce(t *testing.T) {
	t.Parallel()
	// root, "a", "1", "b", the sequence, "x", "y" = 7.
	root := ynode.Map(
		ynode.Scalar("a"), ynode.Scalar("1"),
		ynode.Scalar("b"), ynode.Seq(ynode.Scalar("x"), ynode.Scalar("y")),
	)
	assert.Equal(t, int64(7), Build(root, MaxIndexedNodes).Nodes())
}

// TestBuild_AnAliasCountsOnceAndIsNotFollowed pins the count to the document's
// own size rather than its expansion. The expansion is what the amplification
// refusal measures against this number, so a count that followed alias edges
// would compare a document against itself.
func TestBuild_AnAliasCountsOnceAndIsNotFollowed(t *testing.T) {
	t.Parallel()
	base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")) // 3 nodes
	root := ynode.Map(
		ynode.Scalar("base"), base,
		ynode.Scalar("reuse"), ynode.Alias(base),
	)
	// root, "base", base's 3, "reuse", the alias = 7.
	assert.Equal(t, int64(7), Build(root, MaxIndexedNodes).Nodes(),
		"the alias contributes one node, not a copy of its target")
}

func TestBuild_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root.Content = append(root.Content, nil)

	idx := Build(root, MaxIndexedNodes)
	assert.Equal(t, int64(3), idx.Nodes(),
		"the nil child contributes nothing and costs no dereference")
	assert.False(t, idx.Truncated())
}

func TestBuild_NonPositiveBoundIndexesNothing(t *testing.T) {
	t.Parallel()
	idx := Build(ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")), 0)
	assert.True(t, idx.Truncated(), "a bound that admits no node admits no answer either")
	assert.Equal(t, int64(0), idx.Nodes())
	assert.NotNil(t, idx.Root(), "the tree is still named, so a caller can say what it refused")
}

// TestBuild_StopsAtItsNodeBound is what keeps the walk bounded by something
// other than the input. The count it stops at is deliberately not reported as a
// fact: Truncated is the answer to every other question.
func TestBuild_StopsAtItsNodeBound(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"), ynode.Scalar("b"), ynode.Scalar("2")) // 5 nodes

	assert.False(t, Build(root, 5).Truncated(), "a tree exactly at the bound is fully indexed")

	stopped := Build(root, 4)
	assert.True(t, stopped.Truncated(), "one node past the bound stops the walk")
	_, found := stopped.AnchorCycle()
	assert.False(t, found, "a truncated walk reports no finding it did not reach")
}

// TestBuild_TruncationDoesNotMisreportAnAnchorCycle guards the direction that
// matters: a walk that stopped early must not claim the document is clean in a
// way a caller could act on. It reports Truncated, and the caller refuses.
func TestBuild_TruncationDoesNotMisreportAnAnchorCycle(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	root.Content = append(root.Content, ynode.Scalar("b"), ynode.Alias(root))

	full := Build(root, MaxIndexedNodes)
	_, found := full.AnchorCycle()
	require.True(t, found, "the fixture really does carry a recursive anchor")

	assert.True(t, Build(root, 2).Truncated(),
		"the same tree under a bound it crosses reports truncation rather than cleanliness")
}

func TestAnchorCycle_AliasToAnAncestorIsFound(t *testing.T) {
	t.Parallel()
	inner := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root := ynode.Map(ynode.Scalar("outer"), inner)
	alias := ynode.Alias(root)
	inner.Content = append(inner.Content, ynode.Scalar("loop"), alias)

	got, found := Build(root, MaxIndexedNodes).AnchorCycle()
	require.True(t, found, "an alias naming a node it is nested inside expands without bound")
	assert.Same(t, alias, got, "the reported node is the alias, where the author wrote it")
}

// TestAnchorCycle_LegalReuseIsNotACycle is the control. Anchor reuse is ordinary
// YAML, and a walk that called every alias a cycle would refuse most specs that
// use anchors at all.
func TestAnchorCycle_LegalReuseIsNotACycle(t *testing.T) {
	t.Parallel()
	root := decode(t, "a: &x {p: 1}\nb: *x\n")

	_, found := Build(root, MaxIndexedNodes).AnchorCycle()
	assert.False(t, found, "an alias to a node that is not an ancestor is legal reuse")
}

func TestAnchorCycle_AliasWithoutATargetIsNotACycle(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), &yaml.Node{Kind: yaml.AliasNode})

	idx := Build(root, MaxIndexedNodes)
	_, found := idx.AnchorCycle()
	assert.False(t, found, "an alias that names nothing cannot name an ancestor")
	assert.Equal(t, int64(3), idx.Nodes(), "it is still one of the document's nodes")
}

// TestAnchorCycle_FirstInDocumentOrderWins pins which of several recursive
// anchors is reported. The answer has to be the first a depth-first descent
// would reach, or the diagnostic a document draws would depend on the walk's
// shape rather than on what the document says.
func TestAnchorCycle_FirstInDocumentOrderWins(t *testing.T) {
	t.Parallel()
	first, second := ynode.Map(), ynode.Map()
	root := ynode.Map(ynode.Scalar("a"), first, ynode.Scalar("b"), second)
	firstAlias, secondAlias := ynode.Alias(root), ynode.Alias(root)
	first.Content = append(first.Content, ynode.Scalar("loop"), firstAlias)
	second.Content = append(second.Content, ynode.Scalar("loop"), secondAlias)

	got, found := Build(root, MaxIndexedNodes).AnchorCycle()
	require.True(t, found)
	assert.Same(t, firstAlias, got, "the earlier alias is the one reported")
}

// TestBuild_TracksAncestorsOnlyToItsDepthBound pins the one thing maxTrackedDepth
// decides. Below it a node is still the document's — it is counted, and its
// children are walked — but it is no longer a candidate for an anchor cycle,
// which is exactly where the bounded recursive descent this walk replaced
// stopped looking.
func TestBuild_TracksAncestorsOnlyToItsDepthBound(t *testing.T) {
	t.Parallel()
	// A chain of single-entry mappings: root at depth 0, and each mapping's
	// value two levels below its parent's.
	root := ynode.Map()
	deepest := root
	const links = maxTrackedDepth
	for range links {
		next := ynode.Map()
		deepest.Content = append(deepest.Content, ynode.Scalar("k"), next)
		deepest = next
	}
	deepest.Content = append(deepest.Content, ynode.Scalar("loop"), ynode.Alias(root))

	idx := Build(root, MaxIndexedNodes)
	assert.False(t, idx.Truncated())
	// root, then two nodes per link (its key and the mapping it names), then the
	// deepest mapping's own key and alias.
	assert.Equal(t, int64(2*links+3), idx.Nodes(),
		"every node below the tracked depth is still counted")
	_, found := idx.AnchorCycle()
	assert.False(t, found,
		"an alias deeper than the tracked depth is out of the walk's reach, as it was before")
}

// TestBuild_TracksAncestorsAtItsDepthBound is the control for the test above,
// and the half that pins where the bound falls. Without it the comparison could
// be off by one — tracking to maxTrackedDepth-1 — and every assertion above
// would still hold, because an alias past the bound is out of reach either way.
// An alias at exactly the bound is the case that separates them, and it is the
// case the recursive descent this walk replaced still caught.
func TestBuild_TracksAncestorsAtItsDepthBound(t *testing.T) {
	t.Parallel()
	root := ynode.Map()
	deepest := root
	const links = maxTrackedDepth - 1
	for range links {
		next := ynode.Map()
		deepest.Content = append(deepest.Content, ynode.Scalar("k"), next)
		deepest = next
	}
	// One level shallower than the test above, so these land at the bound itself.
	deepest.Content = append(deepest.Content, ynode.Scalar("loop"), ynode.Alias(root))

	idx := Build(root, MaxIndexedNodes)
	assert.False(t, idx.Truncated())
	cycle, found := idx.AnchorCycle()
	require.True(t, found, "an alias at exactly the tracked depth is still a candidate")
	assert.Same(t, root, cycle.Alias, "the cycle reported is the one back to the root")
}

// TestBuild_IsAFunctionOfTheTreeAlone is the determinism the compiler's output
// rests on: the same tree indexed twice answers identically, so nothing
// downstream can vary with when or how often the index was built.
func TestBuild_IsAFunctionOfTheTreeAlone(t *testing.T) {
	t.Parallel()
	root := decode(t, "a: &x {p: 1}\nb: [*x, {c: 2}]\n")

	first, second := Build(root, MaxIndexedNodes), Build(root, MaxIndexedNodes)
	assert.Same(t, first.Root(), second.Root())
	assert.Equal(t, first.Nodes(), second.Nodes())
	assert.Equal(t, first.Truncated(), second.Truncated())

	firstCycle, firstFound := first.AnchorCycle()
	secondCycle, secondFound := second.AnchorCycle()
	assert.Equal(t, firstFound, secondFound)
	assert.Same(t, firstCycle, secondCycle)

	firstTagged, firstTaggedFound := first.TaggedMapping()
	secondTagged, secondTaggedFound := second.TaggedMapping()
	assert.Equal(t, firstTaggedFound, secondTaggedFound)
	assert.Same(t, firstTagged, secondTagged)
}

// TestTaggedMapping_LocalTagIsFound pins the shape that faults the parser: a
// mapping carrying a tag other than the one YAML resolves a mapping to. The
// node reported is the mapping itself, where the author wrote the tag.
func TestTaggedMapping_LocalTagIsFound(t *testing.T) {
	t.Parallel()
	root := decode(t, "a:\n  b: !content:\n    c: 1\n")
	want := root.Content[0].Content[1].Content[1]
	require.Equal(t, "!content:", want.Tag, "the fixture's tag lands where this test reads it")

	got, found := Build(root, MaxIndexedNodes).TaggedMapping()
	require.True(t, found)
	assert.Same(t, want, got)
}

// TestTaggedMapping_AnyTagButMapIsOne pins the predicate as the parser's own:
// the tag must be exactly "!!map", so a standard tag naming another type, and
// the empty tag of a node assembled rather than parsed, are both reported. A
// parse never leaves a mapping's tag empty, so only the first is reachable
// from source; the second keeps the answer honest for a caller that builds
// nodes. The root case is the one mapping the walk starts at rather than
// descends to, and it is not exempt.
func TestTaggedMapping_AnyTagButMapIsOne(t *testing.T) {
	t.Parallel()
	for name, root := range map[string]*yaml.Node{
		"!!str":       decode(t, "a: !!str {b: 1}\n"),
		"!!set":       decode(t, "a: !!set {b: 1}\n"),
		"at the root": decode(t, "--- !x\na: 1\n"),
		"assembled":   ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, found := Build(root, MaxIndexedNodes).TaggedMapping()
			assert.True(t, found)
		})
	}
}

// TestTaggedMapping_OnlyAMappingCounts is the control. A tag on a scalar or a
// sequence is a different question — the parser degrades those to a finding
// rather than faulting — and a mapping spelled with the map tag in any of its
// forms is what every ordinary document is made of.
func TestTaggedMapping_OnlyAMappingCounts(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"untagged":          "a: {b: 1}\n",
		"explicit !!map":    "a: !!map {b: 1}\n",
		"non-specific !":    "a: ! {b: 1}\n",
		"verbatim map tag":  "a: !<tag:yaml.org,2002:map> {b: 1}\n",
		"tagged scalar":     "a: !x b\n",
		"tagged sequence":   "a: !x [1, 2]\n",
		"timestamp scalar":  "a: 2001-12-14\n",
		"tagged null value": "a: !!null\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, found := Build(decode(t, src), MaxIndexedNodes).TaggedMapping()
			assert.False(t, found)
		})
	}
}

// TestTaggedMapping_FirstInDocumentOrderWins pins which of several is reported,
// for the same reason AnchorCycle pins it: the diagnostic must depend on the
// document, not on the walk.
func TestTaggedMapping_FirstInDocumentOrderWins(t *testing.T) {
	t.Parallel()
	root := decode(t, "a: !x {b: 1}\nc: !y {d: 2}\n")
	first := root.Content[0].Content[1]

	got, found := Build(root, MaxIndexedNodes).TaggedMapping()
	require.True(t, found)
	assert.Same(t, first, got, "the earlier mapping is the one reported")
}

// TestTaggedMapping_IsFoundThroughAnAnchor pins that an alias to a tagged
// mapping needs no handling of its own: the walk reaches the anchored node
// where it is declared, and that is the node reported.
func TestTaggedMapping_IsFoundThroughAnAnchor(t *testing.T) {
	t.Parallel()
	root := decode(t, "a: &x !t {b: 1}\nc: *x\n")
	anchored := root.Content[0].Content[1]

	got, found := Build(root, MaxIndexedNodes).TaggedMapping()
	require.True(t, found)
	assert.Same(t, anchored, got)
}
