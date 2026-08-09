package sourceindex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

func yscalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
}

func ymap(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: pairs}
}

func yseq(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}

func yalias(target *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.AliasNode, Alias: target}
}

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
	content := ymap(yscalar("a"), yscalar("1"))
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{content}}

	assert.Same(t, content, Build(doc, MaxIndexedNodes).Root(),
		"a document node is unwrapped to the node every consumer scans from")
	assert.Same(t, content, Build(content, MaxIndexedNodes).Root(),
		"a content node passed directly is already the root")
}

func TestBuild_CountsEveryNodeOnce(t *testing.T) {
	t.Parallel()
	// root, "a", "1", "b", the sequence, "x", "y" = 7.
	root := ymap(
		yscalar("a"), yscalar("1"),
		yscalar("b"), yseq(yscalar("x"), yscalar("y")),
	)
	assert.Equal(t, int64(7), Build(root, MaxIndexedNodes).Nodes())
}

// TestBuild_AnAliasCountsOnceAndIsNotFollowed pins the count to the document's
// own size rather than its expansion. The expansion is what the amplification
// refusal measures against this number, so a count that followed alias edges
// would compare a document against itself.
func TestBuild_AnAliasCountsOnceAndIsNotFollowed(t *testing.T) {
	t.Parallel()
	base := ymap(yscalar("a"), yscalar("1")) // 3 nodes
	root := ymap(
		yscalar("base"), base,
		yscalar("reuse"), yalias(base),
	)
	// root, "base", base's 3, "reuse", the alias = 7.
	assert.Equal(t, int64(7), Build(root, MaxIndexedNodes).Nodes(),
		"the alias contributes one node, not a copy of its target")
}

func TestBuild_NilChildIsSkipped(t *testing.T) {
	t.Parallel()
	root := ymap(yscalar("k"), yscalar("v"))
	root.Content = append(root.Content, nil)

	idx := Build(root, MaxIndexedNodes)
	assert.Equal(t, int64(3), idx.Nodes(),
		"the nil child contributes nothing and costs no dereference")
	assert.False(t, idx.Truncated())
}

func TestBuild_NonPositiveBoundIndexesNothing(t *testing.T) {
	t.Parallel()
	idx := Build(ymap(yscalar("a"), yscalar("1")), 0)
	assert.True(t, idx.Truncated(), "a bound that admits no node admits no answer either")
	assert.Equal(t, int64(0), idx.Nodes())
	assert.NotNil(t, idx.Root(), "the tree is still named, so a caller can say what it refused")
}

// TestBuild_StopsAtItsNodeBound is what keeps the walk bounded by something
// other than the input. The count it stops at is deliberately not reported as a
// fact: Truncated is the answer to every other question.
func TestBuild_StopsAtItsNodeBound(t *testing.T) {
	t.Parallel()
	root := ymap(yscalar("a"), yscalar("1"), yscalar("b"), yscalar("2")) // 5 nodes

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
	root := ymap(yscalar("a"), yscalar("1"))
	root.Content = append(root.Content, yscalar("b"), yalias(root))

	full := Build(root, MaxIndexedNodes)
	_, found := full.AnchorCycle()
	require.True(t, found, "the fixture really does carry a recursive anchor")

	assert.True(t, Build(root, 2).Truncated(),
		"the same tree under a bound it crosses reports truncation rather than cleanliness")
}

func TestAnchorCycle_AliasToAnAncestorIsFound(t *testing.T) {
	t.Parallel()
	inner := ymap(yscalar("k"), yscalar("v"))
	root := ymap(yscalar("outer"), inner)
	alias := yalias(root)
	inner.Content = append(inner.Content, yscalar("loop"), alias)

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
	root := ymap(yscalar("k"), &yaml.Node{Kind: yaml.AliasNode})

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
	first, second := ymap(), ymap()
	root := ymap(yscalar("a"), first, yscalar("b"), second)
	firstAlias, secondAlias := yalias(root), yalias(root)
	first.Content = append(first.Content, yscalar("loop"), firstAlias)
	second.Content = append(second.Content, yscalar("loop"), secondAlias)

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
	root := ymap()
	deepest := root
	const links = maxTrackedDepth
	for range links {
		next := ymap()
		deepest.Content = append(deepest.Content, yscalar("k"), next)
		deepest = next
	}
	deepest.Content = append(deepest.Content, yscalar("loop"), yalias(root))

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
}
