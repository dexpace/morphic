package ynode_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

func TestScalar_IsUntagged(t *testing.T) {
	t.Parallel()
	n := ynode.Scalar("x")
	assert.Equal(t, yaml.ScalarNode, n.Kind)
	assert.Equal(t, "x", n.Value)
	assert.Empty(t, n.Tag, "an untagged scalar is what distinguishes a plain key from a merge key")
}

func TestMap_HoldsPairsAsAlternatingContent(t *testing.T) {
	t.Parallel()
	key, val := ynode.Scalar("k"), ynode.Scalar("v")
	n := ynode.Map(key, val)
	assert.Equal(t, yaml.MappingNode, n.Kind)
	require.Len(t, n.Content, 2)
	assert.Same(t, key, n.Content[0])
	assert.Same(t, val, n.Content[1])
	assert.Empty(t, ynode.Map().Content, "a mapping with no pairs holds nothing")
}

func TestSeq_HoldsItemsInOrder(t *testing.T) {
	t.Parallel()
	first, second := ynode.Scalar("a"), ynode.Scalar("b")
	n := ynode.Seq(first, second)
	assert.Equal(t, yaml.SequenceNode, n.Kind)
	require.Len(t, n.Content, 2)
	assert.Same(t, first, n.Content[0])
	assert.Same(t, second, n.Content[1])
}

func TestAlias_ResolvesToItsTarget(t *testing.T) {
	t.Parallel()
	target := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	n := ynode.Alias(target)
	assert.Equal(t, yaml.AliasNode, n.Kind)
	assert.Same(t, target, n.Alias, "yaml.v3 resolves an alias to the node its anchor names")
}

// TestMerge_MatchesTheParsedMergeKey pins MergeTag against yaml.v3 rather than
// against itself: the constant is only worth anything if a `<<` key the parser
// produced carries exactly it.
func TestMerge_MatchesTheParsedMergeKey(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("b: &b {k: v}\nA:\n  <<: *b\n"), &root))
	parsed := findScalar(&root, "<<")
	require.NotNil(t, parsed, "no '<<' scalar in the parsed tree")

	built := ynode.Merge()
	assert.Equal(t, parsed.Kind, built.Kind)
	assert.Equal(t, parsed.Value, built.Value)
	assert.Equal(t, parsed.Tag, built.Tag, "the built key carries the tag the parser resolved")
	assert.Equal(t, ynode.MergeTag, built.Tag)
}

func findScalar(n *yaml.Node, value string) *yaml.Node {
	if n.Kind == yaml.ScalarNode && n.Value == value {
		return n
	}
	for _, c := range n.Content {
		if found := findScalar(c, value); found != nil {
			return found
		}
	}
	return nil
}

func TestMergeChain_NestsOneMergePerLevelOverALeaf(t *testing.T) {
	t.Parallel()
	const levels = 3
	n := ynode.MergeChain(levels)

	for i := range levels {
		require.Equal(t, yaml.MappingNode, n.Kind, "level %d is a mapping", i)
		require.Len(t, n.Content, 2, "level %d holds one merge pair", i)
		assert.Equal(t, ynode.MergeTag, n.Content[0].Tag, "level %d merges rather than naming a key", i)
		require.Equal(t, yaml.AliasNode, n.Content[1].Kind, "level %d merges through an alias", i)
		n = n.Content[1].Alias
	}

	assert.Equal(t, []string{"leaf", "v"}, []string{n.Content[0].Value, n.Content[1].Value},
		"the chain bottoms out in one ordinary pair")

	bare := ynode.MergeChain(0)
	assert.Equal(t, []string{"leaf", "v"}, []string{bare.Content[0].Value, bare.Content[1].Value},
		"a chain of no levels is the leaf alone")
}

func TestMergeChainSpec_AnchorsEveryLevelAndNamesItFromASchema(t *testing.T) {
	t.Parallel()
	const levels = 3
	src := ynode.MergeChainSpec(levels)

	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root), "the fixture is a parseable document")

	// The preamble is asserted key by key because 3.1 makes paths optional: a
	// fixture that stopped writing it would still parse, still compile, and still
	// satisfy every caller, so nothing else here would notice it went missing.
	assert.Contains(t, src, "openapi: 3.1.0\n", "the fixture declares its version")
	assert.Contains(t, src, "info: {title: t, version: '1'}\n", "and the info object 3.1 requires")
	assert.Contains(t, src, "paths: {}\n", "and paths, which 3.0 requires and callers pass both versions")

	assert.Contains(t, src, "  m0: &m0 {type: object}\n", "the chain starts at an anchored mapping")
	for i := 1; i <= levels; i++ {
		assert.Contains(t, src, fmt.Sprintf("  m%d: &m%d {<<: *m%d,", i, i, i-1),
			"level %d merges the level below it", i)
	}
	for i := range levels + 1 {
		assert.Contains(t, src, fmt.Sprintf("    S%d: {properties: {x: *m%d}}\n", i, i),
			"one schema names level %d, so the chain is expanded once per level", i)
	}
}
