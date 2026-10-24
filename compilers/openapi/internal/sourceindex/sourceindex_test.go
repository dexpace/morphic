package sourceindex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

func TestBuild_EmptyDocumentIndexesNothing(t *testing.T) {
	t.Parallel()
	for name, root := range map[string]*yaml.Node{
		"no node at all":             nil,
		"a document with no content": {Kind: yaml.DocumentNode},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			idx := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
			assert.Nil(t, idx.Root(), "an empty document has no content node")
			assert.Equal(t, int64(0), idx.Nodes())
			assert.False(t, idx.Truncated(), "nothing to index is fully indexed")
			assert.Equal(t, int64(0), idx.Nesting())
			assert.Empty(t, idx.AnchorCycles())
			assert.Empty(t, idx.TaggedMappings())
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
	idx := sourceindex.Build(sourceindex.Decode(t, "\n\n\n"), sourceindex.MaxIndexedNodes)

	require.NotNil(t, idx.Root())
	assert.Equal(t, yaml.Kind(0), idx.Root().Kind, "yaml.v3 leaves the node untouched")
	assert.Equal(t, int64(1), idx.Nodes())
	assert.Equal(t, int64(0), idx.Nesting(), "a node of no kind nests nothing")
	assert.Empty(t, idx.AnchorCycles())
}

func TestBuild_RootIsTheDocumentsContent(t *testing.T) {
	t.Parallel()
	content := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{content}}

	assert.Same(t, content, sourceindex.Build(doc, sourceindex.MaxIndexedNodes).Root(),
		"a document node is unwrapped to the node every consumer scans from")
	assert.Same(t, content, sourceindex.Build(content, sourceindex.MaxIndexedNodes).Root(),
		"a content node passed directly is already the root")
}

func TestBuild_CountsEveryNodeOnce(t *testing.T) {
	t.Parallel()
	// root, "a", "1", "b", the sequence, "x", "y" = 7.
	root := ynode.Map(
		ynode.Scalar("a"), ynode.Scalar("1"),
		ynode.Scalar("b"), ynode.Seq(ynode.Scalar("x"), ynode.Scalar("y")),
	)
	assert.Equal(t, int64(7), sourceindex.Build(root, sourceindex.MaxIndexedNodes).Nodes())
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
	assert.Equal(t, int64(7), sourceindex.Build(root, sourceindex.MaxIndexedNodes).Nodes(),
		"the alias contributes one node, not a copy of its target")
}

func TestBuild_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root.Content = append(root.Content, nil)

	idx := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	assert.Equal(t, int64(3), idx.Nodes(),
		"the nil child contributes nothing and costs no dereference")
	assert.False(t, idx.Truncated())
}

func TestBuild_NonPositiveBoundIndexesNothing(t *testing.T) {
	t.Parallel()
	idx := sourceindex.Build(ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")), 0)
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

	assert.False(t, sourceindex.Build(root, 5).Truncated(), "a tree exactly at the bound is fully indexed")

	stopped := sourceindex.Build(root, 4)
	assert.True(t, stopped.Truncated(), "one node past the bound stops the walk")
	assert.Empty(t, stopped.AnchorCycles(), "a truncated walk reports no finding it did not reach")
}

// TestBuild_TruncationDoesNotMisreportAnAnchorCycle guards the direction that
// matters: a walk that stopped early must not claim the document is clean in a
// way a caller could act on. It reports Truncated, and the caller refuses.
func TestBuild_TruncationDoesNotMisreportAnAnchorCycle(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	root.Content = append(root.Content, ynode.Scalar("b"), ynode.Alias(root))

	full := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	require.NotEmpty(t, full.AnchorCycles(), "the fixture really does carry a recursive anchor")

	assert.True(t, sourceindex.Build(root, 2).Truncated(),
		"the same tree under a bound it crosses reports truncation rather than cleanliness")
}

func TestAnchorCycle_AliasToAnAncestorIsFound(t *testing.T) {
	t.Parallel()
	inner := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	root := ynode.Map(ynode.Scalar("outer"), inner)
	alias := ynode.Alias(root)
	inner.Content = append(inner.Content, ynode.Scalar("loop"), alias)

	got := sourceindex.Build(root, sourceindex.MaxIndexedNodes).AnchorCycles()
	require.Len(t, got, 1, "an alias naming a node it is nested inside expands without bound")
	assert.Same(t, alias, got[0], "the reported node is the alias, where the author wrote it")
}

// TestAnchorCycle_LegalReuseIsNotACycle is the control. Anchor reuse is ordinary
// YAML, and a walk that called every alias a cycle would refuse most specs that
// use anchors at all.
func TestAnchorCycle_LegalReuseIsNotACycle(t *testing.T) {
	t.Parallel()
	root := sourceindex.Decode(t, "a: &x {p: 1}\nb: *x\n")

	assert.Empty(t, sourceindex.Build(root, sourceindex.MaxIndexedNodes).AnchorCycles(), "an alias to a node that is not an ancestor is legal reuse")
}

func TestAnchorCycle_AliasWithoutATargetIsNotACycle(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("k"), &yaml.Node{Kind: yaml.AliasNode})

	idx := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	assert.Empty(t, idx.AnchorCycles(), "an alias that names nothing cannot name an ancestor")
	assert.Equal(t, int64(3), idx.Nodes(), "it is still one of the document's nodes")
}

// TestAnchorCycles_EveryOneIsListedInDocumentOrder pins that the walk lists
// every recursive anchor rather than the first it meets: which of several a
// refusal names is the caller's to choose by something a declaration order
// cannot move (GitHub #912), and it cannot choose among answers it was not
// given.
func TestAnchorCycles_EveryOneIsListedInDocumentOrder(t *testing.T) {
	t.Parallel()
	first, second := ynode.Map(), ynode.Map()
	root := ynode.Map(ynode.Scalar("a"), first, ynode.Scalar("b"), second)
	firstAlias, secondAlias := ynode.Alias(root), ynode.Alias(root)
	first.Content = append(first.Content, ynode.Scalar("loop"), firstAlias)
	second.Content = append(second.Content, ynode.Scalar("loop"), secondAlias)

	got := sourceindex.Build(root, sourceindex.MaxIndexedNodes).AnchorCycles()
	require.Len(t, got, 2)
	assert.Same(t, firstAlias, got[0])
	assert.Same(t, secondAlias, got[1])
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
	const links = sourceindex.MaxTrackedDepth
	for range links {
		next := ynode.Map()
		deepest.Content = append(deepest.Content, ynode.Scalar("k"), next)
		deepest = next
	}
	deepest.Content = append(deepest.Content, ynode.Scalar("loop"), ynode.Alias(root))

	idx := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	assert.False(t, idx.Truncated())
	// root, then two nodes per link (its key and the mapping it names), then the
	// deepest mapping's own key and alias.
	assert.Equal(t, int64(2*links+3), idx.Nodes(),
		"every node below the tracked depth is still counted")
	assert.Empty(t, idx.AnchorCycles(),
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
	const links = sourceindex.MaxTrackedDepth - 1
	for range links {
		next := ynode.Map()
		deepest.Content = append(deepest.Content, ynode.Scalar("k"), next)
		deepest = next
	}
	// One level shallower than the test above, so these land at the bound itself.
	deepest.Content = append(deepest.Content, ynode.Scalar("loop"), ynode.Alias(root))

	idx := sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	assert.False(t, idx.Truncated())
	cycles := idx.AnchorCycles()
	require.Len(t, cycles, 1, "an alias at exactly the tracked depth is still a candidate")
	assert.Same(t, root, cycles[0].Alias, "the cycle reported is the one back to the root")
}

// TestBuild_IsAFunctionOfTheTreeAlone is the determinism the compiler's output
// rests on: the same tree indexed twice answers identically, so nothing
// downstream can vary with when or how often the index was built.
func TestBuild_IsAFunctionOfTheTreeAlone(t *testing.T) {
	t.Parallel()
	root := sourceindex.Decode(t, "a: &x {p: 1}\nb: [*x, {c: 2}]\n")

	first, second := sourceindex.Build(root, sourceindex.MaxIndexedNodes), sourceindex.Build(root, sourceindex.MaxIndexedNodes)
	assert.Same(t, first.Root(), second.Root())
	assert.Equal(t, first.Nodes(), second.Nodes())
	assert.Equal(t, first.Truncated(), second.Truncated())

	assert.Equal(t, first.Nesting(), second.Nesting())
	assert.Equal(t, first.AnchorCycles(), second.AnchorCycles())
	assert.Equal(t, first.TaggedMappings(), second.TaggedMappings())
}

// TestTaggedMapping_LocalTagIsFound pins the shape that faults the parser: a
// mapping carrying a tag other than the one YAML resolves a mapping to. The
// node reported is the mapping itself, where the author wrote the tag.
func TestTaggedMapping_LocalTagIsFound(t *testing.T) {
	t.Parallel()
	root := sourceindex.Decode(t, "a:\n  b: !content:\n    c: 1\n")
	want := root.Content[0].Content[1].Content[1]
	require.Equal(t, "!content:", want.Tag, "the fixture's tag lands where this test reads it")

	got := sourceindex.Build(root, sourceindex.MaxIndexedNodes).TaggedMappings()
	require.Len(t, got, 1)
	assert.Same(t, want, got[0])
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
		"!!str":       sourceindex.Decode(t, "a: !!str {b: 1}\n"),
		"!!set":       sourceindex.Decode(t, "a: !!set {b: 1}\n"),
		"at the root": sourceindex.Decode(t, "--- !x\na: 1\n"),
		"assembled":   ynode.Map(ynode.Scalar("a"), ynode.Scalar("1")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, sourceindex.Build(root, sourceindex.MaxIndexedNodes).TaggedMappings())
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
			assert.Empty(t, sourceindex.Build(sourceindex.Decode(t, src), sourceindex.MaxIndexedNodes).TaggedMappings())
		})
	}
}

// TestTaggedMappings_EveryOneIsListedInDocumentOrder pins that every tagged
// mapping is listed, for the reason AnchorCycles lists every cycle: the caller
// chooses which a refusal names.
func TestTaggedMappings_EveryOneIsListedInDocumentOrder(t *testing.T) {
	t.Parallel()
	root := sourceindex.Decode(t, "a: !x {b: 1}\nc: !y {d: 2}\n")
	first, second := root.Content[0].Content[1], root.Content[0].Content[3]

	got := sourceindex.Build(root, sourceindex.MaxIndexedNodes).TaggedMappings()
	require.Len(t, got, 2)
	assert.Same(t, first, got[0])
	assert.Same(t, second, got[1])
}

// TestTaggedMapping_IsFoundThroughAnAnchor pins that an alias to a tagged
// mapping needs no handling of its own: the walk reaches the anchored node
// where it is declared, and that is the node reported.
func TestTaggedMapping_IsFoundThroughAnAnchor(t *testing.T) {
	t.Parallel()
	root := sourceindex.Decode(t, "a: &x !t {b: 1}\nc: *x\n")
	anchored := root.Content[0].Content[1]

	got := sourceindex.Build(root, sourceindex.MaxIndexedNodes).TaggedMappings()
	require.Len(t, got, 1, "the alias is not a second mapping")
	assert.Same(t, anchored, got[0])
}
