package nodeview

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

func TestDocumentRoot_Cases(t *testing.T) {
	t.Parallel()
	content := ynode.Scalar("x")
	tests := []struct {
		name string
		in   *yaml.Node
		want *yaml.Node
	}{
		{"nil", nil, nil},
		{"empty document", &yaml.Node{Kind: yaml.DocumentNode}, nil},
		{"document unwraps to content", &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{content}}, content},
		{"plain node returned as-is", content, content},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DocumentRoot(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tc.want, got)
		})
	}
}

func TestChildByToken_NilNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, New().ChildByToken(nil, "anything"))
}

func TestPointerPath_Cases(t *testing.T) {
	t.Parallel()
	leaf := ynode.Scalar("leaf")
	target := ynode.Map(ynode.Scalar("b"), leaf)
	root := ynode.Map(
		ynode.Scalar("arr"), ynode.Seq(ynode.Scalar("zero"), ynode.Scalar("one")),
		ynode.Scalar("via"), ynode.Alias(target),
		ynode.Scalar("a/b"), ynode.Scalar("slash"),
		ynode.Scalar("c~d"), ynode.Scalar("tilde"),
	)
	tests := []struct {
		name    string
		pointer string
		want    *yaml.Node
	}{
		{"sequence index in range", "/arr/1", root.Content[1].Content[1]},
		{"sequence index out of range", "/arr/9", nil},
		{"sequence index non-numeric", "/arr/x", nil},
		{"alias dereferenced along path", "/via/b", leaf},
		{"escaped slash token", "/a~1b", root.Content[5]},
		{"escaped tilde token", "/c~0d", root.Content[7]},
		{"missing key", "/nope", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, complete := New().PointerPath(root, tc.pointer)
			if tc.want == nil {
				assert.False(t, complete, "the pointer names nothing")
				return
			}
			require.True(t, complete)
			assert.Same(t, tc.want, path[len(path)-1], "the last element is the destination")
		})
	}
}

func TestInternalPointer_MatchesTheResolversNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, ref, want string
		internal        bool
	}{
		{name: "plain", ref: "#/components/schemas/A", want: "/components/schemas/A", internal: true},
		{name: "trailing space", ref: "#/paths/~1a ", want: "/paths/~1a", internal: true},
		{name: "leading space", ref: " #/paths/~1a", want: "/paths/~1a", internal: true},
		{name: "percent-decoded", ref: "#/paths/%7E1a", want: "/paths/~1a", internal: true},
		{name: "second hash ends the pointer", ref: "#/a#b", want: "/a", internal: true},
		{name: "bare hash names the root", ref: "#", want: "", internal: true},
		{name: "undecodable escape kept raw", ref: "#/a%zz", want: "/a%zz", internal: true},
		{name: "no fragment", ref: "other.yaml", internal: false},
		{name: "another document", ref: "other.yaml#/components/schemas/A", internal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, internal := InternalPointer(tc.ref)
			assert.Equal(t, tc.internal, internal)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeref_FollowsAliasChain(t *testing.T) {
	t.Parallel()
	target := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	require.Same(t, target, Deref(ynode.Alias(target)))
	require.Same(t, target, Deref(target))
}

func pairMap(pairs []Pair) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		out[p.Key] = p.Val.Value
	}
	return out
}

func TestIsMergeKey_MatchesResolver(t *testing.T) {
	t.Parallel()
	tagged := func(tag string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: tag}
	}
	tests := []struct {
		name string
		in   *yaml.Node
		want bool
	}{
		{"resolved merge tag", tagged(ynode.MergeTag), true},
		{"quoted string tag", tagged("!!str"), false},
		{"untagged scalar", ynode.Scalar("<<"), false},
		{"non-specific tag", tagged("!"), false},
		{"long-form merge tag", tagged("tag:yaml.org,2002:merge"), false},
		{"other value", ynode.Scalar("$ref"), false},
		{"alias key", ynode.Alias(ynode.Merge()), false},
		{"mapping key", ynode.Map(), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsMergeKey(tc.in))
		})
	}
}

func TestIsMergeKey_AgreesWithParsedTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src string
		want      bool
	}{
		{"plain", "b: &b {k: v}\nA:\n  <<: *b\n", true},
		{"non-specific tag", "b: &b {k: v}\nA:\n  ! <<: *b\n", true},
		{"explicit merge tag", "b: &b {k: v}\nA:\n  !!merge <<: *b\n", true},
		{"quoted", "b: &b {k: v}\nA:\n  '<<': *b\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			key := findScalar(&root, "<<")
			require.NotNil(t, key, "no '<<' scalar in the parsed tree")
			assert.Equal(t, tc.want, IsMergeKey(key), "parsed tag was %q", key.Tag)
		})
	}
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

func TestPureRefTarget_Cases(t *testing.T) {
	t.Parallel()

	t.Run("non-mapping node has no target", func(t *testing.T) {
		t.Parallel()
		_, ok := New().PureRefTarget(ynode.Scalar("x"))
		assert.False(t, ok)
	})

	tests := []struct {
		name string
		n    *yaml.Node
		want string
	}{
		{"sibling key before the ref", ynode.Map(ynode.Scalar("type"), ynode.Scalar("object"),
			ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/A")), "/components/schemas/A"},
		{"external ref is not internal", ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("other.yaml#/A")), ""},
		{"non-scalar ref value", ynode.Map(ynode.Scalar("$ref"), ynode.Map(ynode.Scalar("a"), ynode.Scalar("b"))), ""},
		{"nil ref value via broken alias", ynode.Map(ynode.Scalar("$ref"), ynode.Alias(nil)), ""},
		{"no ref key at all", ynode.Map(ynode.Scalar("type"), ynode.Scalar("object")), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := New().PureRefTarget(tc.n)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestChildByToken_MappingResolvesThroughAliasKey(t *testing.T) {
	t.Parallel()
	keyTarget := ynode.Scalar("k")
	val := ynode.Scalar("v")
	n := ynode.Map(ynode.Alias(keyTarget), val)
	assert.Same(t, val, New().ChildByToken(n, "k"))
}

func TestChildByToken_ScalarNodeHasNoChild(t *testing.T) {
	t.Parallel()
	assert.Nil(t, New().ChildByToken(ynode.Scalar("x"), "0"))
}

func TestNodeView_CachesOnlyReproducibleExpansions(t *testing.T) {
	t.Parallel()

	t.Run("complete expansion is cached", func(t *testing.T) {
		t.Parallel()
		base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
		n := ynode.Map(ynode.Merge(), ynode.Alias(base), ynode.Scalar("b"), ynode.Scalar("2"))
		v := New()
		first := v.MappingPairs(n)
		require.Contains(t, v.pairs, n, "a complete expansion is memoized")
		require.Contains(t, v.pairs, base, "so is every complete sub-expansion")
		assert.Equal(t, pairMap(first), pairMap(v.MappingPairs(n)), "the cached read matches")
	})

	// outer -> shared -> deep -> outer, all by `<<`. Reaching deep through outer
	// breaks the cycle at outer, so deep contributes only its own pair.
	// Expanding deep first instead breaks the cycle at deep, and it contributes
	// all three — so deep's truncated value must not be cached by the first read.
	mergeCycle := func() (outer, shared, deep *yaml.Node) {
		outer = &yaml.Node{Kind: yaml.MappingNode}
		shared = &yaml.Node{Kind: yaml.MappingNode}
		deep = &yaml.Node{Kind: yaml.MappingNode}
		outer.Content = []*yaml.Node{ynode.Scalar("outerkey"), ynode.Scalar("o"), ynode.Merge(), ynode.Alias(shared)}
		shared.Content = []*yaml.Node{ynode.Scalar("keep"), ynode.Scalar("v"), ynode.Merge(), ynode.Alias(deep)}
		deep.Content = []*yaml.Node{ynode.Scalar("deepkey"), ynode.Scalar("d"), ynode.Merge(), ynode.Alias(outer)}
		return outer, shared, deep
	}

	t.Run("a truncation reached from inside a chain is not cached", func(t *testing.T) {
		t.Parallel()
		outer, shared, deep := mergeCycle()
		v := New()
		assert.Equal(t, map[string]string{"outerkey": "o", "keep": "v", "deepkey": "d"},
			pairMap(v.MappingPairs(outer)), "the cycle is broken, not followed")
		assert.NotContains(t, v.pairs, shared, "an inner truncated expansion may not be cached")
		assert.NotContains(t, v.pairs, deep, "nor the one below it")

		assert.Equal(t, map[string]string{"deepkey": "d", "outerkey": "o", "keep": "v"},
			pairMap(v.MappingPairs(deep)),
			"entering at deep breaks the cycle elsewhere and sees more pairs than the first read gave it")
	})

	t.Run("a top-level truncation is cached and stable", func(t *testing.T) {
		t.Parallel()
		outer, _, _ := mergeCycle()
		v := New()
		first := pairMap(v.MappingPairs(outer))
		require.Contains(t, v.pairs, outer,
			"the entry point's own expansion does not depend on any caller")
		assert.Equal(t, first, pairMap(v.MappingPairs(outer)), "so re-reading it agrees")
	})

	t.Run("nothing is cached while another expansion is in flight", func(t *testing.T) {
		t.Parallel()
		outer, _, _ := mergeCycle()
		v := New()
		other := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
		v.inFlight[other] = true // as if this read came from inside other's

		assert.NotEmpty(t, v.MappingPairs(outer), "the read still answers")
		assert.NotContains(t, v.pairs, outer,
			"but a truncation under an in-flight expansion is not reproducible, so it is not kept")
	})
}

func TestNodeView_TruncationIsPerNode(t *testing.T) {
	t.Parallel()
	v := New()
	require.Empty(t, v.MappingPairs(ynode.MergeChain(MergeDepthLimit+2)))
	require.True(t, v.exhausted)

	other := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/A"))
	assert.Equal(t, map[string]string{"$ref": "#/components/schemas/A"},
		pairMap(v.MappingPairs(other)), "an unrelated mapping still expands in full")
	assert.Equal(t, map[string]string{"leaf": "v"},
		pairMap(v.MappingPairs(ynode.MergeChain(MergeDepthLimit))),
		"so does a chain that fits inside the bound")
}

func TestNodeView_MemoizeRespectsPairBudget(t *testing.T) {
	t.Parallel()
	pairs := []Pair{{Key: "a", Val: ynode.Scalar("1")}, {Key: "b", Val: ynode.Scalar("2")}}

	t.Run("within budget: retained and counted", func(t *testing.T) {
		t.Parallel()
		v := New()
		n := ynode.Map()
		v.memoize(n, pairs)
		assert.Contains(t, v.pairs, n)
		assert.Equal(t, len(pairs), v.cachedPairs)
	})

	t.Run("past budget: dropped, count unchanged", func(t *testing.T) {
		t.Parallel()
		v := New()
		v.cachedPairs = maxCachedPairs - 1
		n := ynode.Map()
		v.memoize(n, pairs)
		assert.NotContains(t, v.pairs, n, "an entry that would overrun the budget is not kept")
		assert.Equal(t, maxCachedPairs-1, v.cachedPairs, "and does not count against it")
	})

	t.Run("a dropped entry still reads correctly", func(t *testing.T) {
		t.Parallel()
		base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
		n := ynode.Map(ynode.Merge(), ynode.Alias(base), ynode.Scalar("b"), ynode.Scalar("2"))
		v := New()
		v.cachedPairs = maxCachedPairs
		want := map[string]string{"a": "1", "b": "2"}
		assert.Equal(t, want, pairMap(v.MappingPairs(n)), "an uncached read is a full read")
		assert.Equal(t, want, pairMap(v.MappingPairs(n)), "and repeats identically")
		assert.Empty(t, v.pairs, "nothing was retained")
	})
}

// TestMappingPairs_MergeKeySources covers the `<<` expansion, which is the half
// of the view a raw yaml.Node walk does not give you: the resolver reads merged
// keys as if they were written in place, so a scan that missed them would not
// see a $ref a merge contributes.
//
// The precedence rules are the resolver's, and they differ by position: an
// explicit key beats any merged one, and among the elements of a merge sequence
// the earlier source wins.
func TestMappingPairs_MergeKeySources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want map[string]string
	}{
		{
			name: "a single mapping source merges in",
			src:  "base: &b {a: 1, b: 2}\nuse:\n  <<: *b\n  c: 3\n",
			want: map[string]string{"a": "1", "b": "2", "c": "3"},
		},
		{
			name: "an explicit key beats the merged one",
			src:  "base: &b {a: 1}\nuse:\n  <<: *b\n  a: 2\n",
			want: map[string]string{"a": "2"},
		},
		{
			name: "in a sequence the earlier source wins",
			src:  "one: &x {a: 1}\ntwo: &y {a: 2, b: 9}\nuse:\n  <<: [*x, *y]\n",
			want: map[string]string{"a": "1", "b": "9"},
		},
		{
			name: "every merge key is honored, not only the last",
			src:  "one: &x {a: 1}\ntwo: &y {b: 2}\nuse:\n  <<: *x\n  <<: *y\n",
			want: map[string]string{"a": "1", "b": "2"},
		},
		{
			name: "a non-scalar key names no keyword and is skipped",
			src:  "use:\n  ? [a, b]\n  : v\n  k: 1\n",
			want: map[string]string{"k": "1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := yamlDoc(t, tc.src)
			use := New().ChildByToken(doc, "use")
			require.NotNil(t, use, "the fixture declares a `use` mapping")

			got := map[string]string{}
			for _, p := range New().MappingPairs(use) {
				got[p.Key] = p.Val.Value
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestExhausted_ReportsAnIncompleteExpansion pins the signal a caller refusing a
// document on incomplete information depends on. A view that stopped at its
// merge-depth bound has not seen every pair, and reporting a clean read from
// there would hide exactly what the bound exists to bound.
func TestExhausted_ReportsAnIncompleteExpansion(t *testing.T) {
	t.Parallel()
	v := New()
	assert.False(t, v.Exhausted(), "a view that has expanded nothing has exhausted nothing")

	deep := ynode.MergeChain(MergeDepthLimit + 2)
	_ = v.MappingPairs(deep)
	assert.True(t, v.Exhausted(), "past the bound the view says its expansion is incomplete")

	shallow := New()
	_ = shallow.MappingPairs(ynode.MergeChain(2))
	assert.False(t, shallow.Exhausted(), "within the bound it does not")
}

// yamlDoc parses src and returns its document root as the view sees it.
func yamlDoc(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	return DocumentRoot(&doc)
}

func TestPointerPath_KeepsTheNodesTheWalkPassesThrough(t *testing.T) {
	t.Parallel()
	leaf := ynode.Scalar("leaf")
	inner := ynode.Map(ynode.Scalar("b"), leaf)
	root := ynode.Map(ynode.Scalar("a"), inner)

	path, complete := New().PointerPath(root, "/a/b")
	require.True(t, complete, "every token resolves")
	assert.Equal(t, []*yaml.Node{root, inner, leaf}, path,
		"element 0 is the root and each later element is one more token")
}

func TestPointerPath_IncompleteStopsAtTheLastNodeReached(t *testing.T) {
	t.Parallel()
	inner := ynode.Map(ynode.Scalar("b"), ynode.Scalar("leaf"))
	root := ynode.Map(ynode.Scalar("a"), inner)

	path, complete := New().PointerPath(root, "/a/missing/deeper")
	assert.False(t, complete, "a token that names nothing stops the walk")
	assert.Equal(t, []*yaml.Node{root, inner}, path,
		"an unresolvable pointer has no destination, so every node it reached is one it passed through")
}

func TestPointerPath_RootTokenlessAndNil(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), ynode.Scalar("v"))

	path, complete := New().PointerPath(root, "")
	assert.True(t, complete, "a pointer with no tokens names the root")
	assert.Equal(t, []*yaml.Node{root}, path)

	path, complete = New().PointerPath(nil, "/a")
	assert.False(t, complete)
	assert.Nil(t, path, "a nil root reaches nothing")
}

func TestPointerPath_SegmentCapStopsTheWalk(t *testing.T) {
	t.Parallel()
	// A mapping whose only key is "a" and whose value is itself cannot be built
	// from parsed YAML, but an alias can stand in: the walk follows "a" as long
	// as tokens last, so only the cap can end it.
	root := ynode.Map(ynode.Scalar("a"), nil)
	root.Content[1] = root

	ref := strings.Repeat("/a", maxPointerSegments+1)
	path, complete := New().PointerPath(root, ref)
	assert.False(t, complete, "a pointer past the segment cap does not resolve")
	assert.Len(t, path, maxPointerSegments+1,
		"the walk stops at the cap: the root plus one node per followed token")
}
