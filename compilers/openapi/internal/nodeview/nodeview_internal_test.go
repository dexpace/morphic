package nodeview

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/yml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

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

// TestDocumentPath_Cases pins how a pointer is walked: an index, an alias
// dereferenced on the way, each escape, and a token naming nothing.
func TestDocumentPath_Cases(t *testing.T) {
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
		pointer jsontext.Pointer
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
			path, complete := New().DocumentPath(root, tc.pointer)
			if tc.want == nil {
				assert.False(t, complete, "the pointer names nothing")
				return
			}
			require.True(t, complete)
			assert.Same(t, tc.want, path[len(path)-1])
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

// holding charges v's memo pairs pairs, as if it held them already.
func holding(v *View, pairs int) {
	v.cached.Spend(pairs)
}

func TestNodeView_MemoizeRespectsPairBudget(t *testing.T) {
	t.Parallel()
	pairs := []Pair{{Key: "a", Val: ynode.Scalar("1")}, {Key: "b", Val: ynode.Scalar("2")}}
	e := expansion{pairs: pairs, complete: true}

	t.Run("within budget: retained and counted", func(t *testing.T) {
		t.Parallel()
		v := New()
		n := ynode.Map()
		v.memoize(n, e)
		assert.Contains(t, v.pairs, n)
		assert.Equal(t, len(pairs), v.cached.Spent())
	})

	t.Run("past budget: dropped, count unchanged", func(t *testing.T) {
		t.Parallel()
		v := New()
		holding(v, maxCachedPairs-1)
		n := ynode.Map()
		v.memoize(n, e)
		assert.NotContains(t, v.pairs, n, "an entry that would overrun the budget is not kept")
		assert.Equal(t, maxCachedPairs-1, v.cached.Spent(), "and does not count against it")

		one := ynode.Map()
		v.memoize(one, expansion{pairs: pairs[:1], complete: true})
		assert.Contains(t, v.pairs, one, "so an entry that fits what is left is kept after it")
		assert.Equal(t, maxCachedPairs, v.cached.Spent())
	})

	t.Run("a dropped entry still reads correctly", func(t *testing.T) {
		t.Parallel()
		base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
		n := ynode.Map(ynode.Merge(), ynode.Alias(base), ynode.Scalar("b"), ynode.Scalar("2"))
		v := New()
		holding(v, maxCachedPairs)
		want := map[string]string{"a": "1", "b": "2"}
		assert.Equal(t, want, pairMap(v.MappingPairs(n)), "an uncached read is a full read")
		assert.Equal(t, want, pairMap(v.MappingPairs(n)), "and repeats identically")
		assert.Empty(t, v.pairs, "nothing was retained")
	})
}

func TestMappingPairs_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    *yaml.Node
		want map[string]string
	}{
		{"nil node yields no pairs", nil, nil},
		{"non-mapping node yields no pairs", ynode.Scalar("x"), nil},
		{
			"alias-valued mapping is dereferenced at entry",
			ynode.Alias(ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"alias key and alias value are dereferenced",
			ynode.Map(ynode.Alias(ynode.Scalar("k")), ynode.Alias(ynode.Scalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"non-scalar key is read as the empty string",
			ynode.Map(
				ynode.Map(ynode.Scalar("x"), ynode.Scalar("1")), ynode.Scalar("empty"),
				ynode.Scalar("real"), ynode.Scalar("kept"),
			),
			map[string]string{"": "empty", "real": "kept"},
		},
		{
			"a nil key, which no parse produces, is skipped",
			ynode.Map(nil, ynode.Scalar("ignored"), ynode.Scalar("real"), ynode.Scalar("kept")),
			map[string]string{"real": "kept"},
		},
		{
			"key aliasing a nil target is read as its own text",
			ynode.Map(&yaml.Node{Kind: yaml.AliasNode, Value: "anchor"}, ynode.Scalar("named"),
				ynode.Scalar("real"), ynode.Scalar("kept")),
			map[string]string{"anchor": "named", "real": "kept"},
		},
		{
			"odd trailing key without a value is ignored",
			trailingKeyNode(),
			map[string]string{"a": "1"},
		},
		{
			"duplicate explicit Key: last wins",
			ynode.Map(ynode.Scalar("k"), ynode.Scalar("first"), ynode.Scalar("k"), ynode.Scalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"a merged key still yields to a repeated explicit key",
			ynode.Map(ynode.Scalar("k"), ynode.Scalar("first"), ynode.Merge(), ynode.Map(ynode.Scalar("k"), ynode.Scalar("from-merge")),
				ynode.Scalar("k"), ynode.Scalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"merge key contributes a mapping's pairs",
			ynode.Map(ynode.Merge(), ynode.Alias(ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))), ynode.Scalar("b"), ynode.Scalar("2")),
			map[string]string{"a": "1", "b": "2"},
		},
		{
			"merge value that is not a mapping contributes nothing",
			ynode.Map(ynode.Merge(), ynode.Scalar("not-a-mapping"), ynode.Scalar("b"), ynode.Scalar("2")),
			map[string]string{"b": "2"},
		},
		{
			"explicit key wins over merged key",
			ynode.Map(ynode.Scalar("a"), ynode.Scalar("explicit"), ynode.Merge(), ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-merge"))),
			map[string]string{"a": "explicit"},
		},
		{
			"merge sequence: earlier source wins on a shared key",
			ynode.Map(ynode.Merge(), ynode.Seq(
				ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-first")),
				ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-second"), ynode.Scalar("b"), ynode.Scalar("only-in-second")),
			)),
			map[string]string{"a": "from-first", "b": "only-in-second"},
		},
		{
			"merge sequence reusing one source keeps its pairs",
			mergeSeqReuseNode(),
			map[string]string{"a": "1"},
		},
		{
			"self-referential merge is bounded, not infinite",
			selfReferentialMergeNode(),
			map[string]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := New().MappingPairs(tc.n)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tc.want, pairMap(got))
		})
	}

	// The remaining two shapes assert something pairMap erases (slice order,
	// and the view's own exhausted bookkeeping across a depth-bound merge
	// chain), so they stay as dedicated subtests rather than table rows.

	t.Run("duplicate explicit key keeps the last occurrence's position", func(t *testing.T) {
		t.Parallel()
		n := ynode.Map(
			ynode.Scalar("k"), ynode.Scalar("first"),
			ynode.Scalar("other"), ynode.Scalar("o"),
			ynode.Scalar("k"), ynode.Scalar("second"),
		)
		got := New().MappingPairs(n)
		require.Len(t, got, 2)
		assert.Equal(t, "other", got[0].Key)
		assert.Equal(t, "k", got[1].Key)
		assert.Equal(t, "second", got[1].Val.Value)
	})

	t.Run("merge chain at the depth bound still reaches the leaf", func(t *testing.T) {
		t.Parallel()
		v := New()
		got := v.MappingPairs(ynode.MergeChain(MergeDepthLimit))
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(got),
			"a chain exactly at the bound expands in full")
		assert.False(t, v.Exhausted(), "expanding to the bound is not exceeding it")
	})
}

func trailingKeyNode() *yaml.Node {
	n := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	n.Content = append(n.Content, ynode.Scalar("dangling"))
	return n
}

func mergeSeqReuseNode() *yaml.Node {
	base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	return ynode.Map(ynode.Merge(), ynode.Seq(ynode.Alias(base), ynode.Alias(base)))
}

func selfReferentialMergeNode() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	n.Content = []*yaml.Node{ynode.Merge(), ynode.Alias(n)}
	return n
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
			name: "a non-scalar key is read as the empty string",
			src:  "use:\n  ? [a, b]\n  : v\n  k: 1\n",
			want: map[string]string{"": "v", "k": "1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := YAMLDoc(t, tc.src)
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

// TestMappingPairs_MergeKeySpellings holds the view to the library on which key
// merges: each key is read through both, and the view must present the keys the
// unmarshaller does. The spellings are the ones a hand-written predicate gets
// wrong: ignoring the tag merges a quoted '<<', reading only the tag merges
// `!!merge other`, and resolving the key first merges an alias the library
// reads as the ordinary key "<<".
func TestMappingPairs_MergeKeySpellings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, key string
		merges    bool
	}{
		{"plain", "<<", true},
		{"non-specific tag", "! <<", true},
		{"merge tag", "!!merge <<", true},
		{"verbatim merge tag", "!<tag:yaml.org,2002:merge> <<", true},
		{"merge tag on a quoted key", "!!merge '<<'", true},
		{"quoted", "'<<'", false},
		{"string tag", "!!str <<", false},
		{"local tag", "!merge <<", false},
		{"merge tag on another key", "!!merge other", false},
		{"alias to a merge key", "*m", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// m anchors a merge key for the alias case to stand in for.
			src := fmt.Sprintf("b: &b {k: v}\nm: {&m <<: *b}\nuse:\n  %s : *b\n", tc.key)
			use := New().ChildByToken(YAMLDoc(t, src), "use")
			require.NotNil(t, use, "the fixture declares a `use` mapping")

			got := pairMap(New().MappingPairs(use))

			assert.Equal(t, libraryKeys(use), slices.Sorted(maps.Keys(got)), "the view presents the library's keys")
			_, merged := got["k"]
			assert.Equal(t, tc.merges, merged, "pairs %v", got)
		})
	}
}

// libraryKeys returns the keys of mapping n as the unmarshaller reads them,
// sorted: expanded by yml.ResolveMergeKeys, each key resolved through an alias.
func libraryKeys(n *yaml.Node) []string {
	content := yml.ResolveMergeKeys(n.Content)
	keys := make([]string, 0, len(content)/2)
	for i := 0; i+1 < len(content); i += 2 {
		keys = append(keys, yml.ResolveAlias(content[i]).Value)
	}
	slices.Sort(keys)
	return keys
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

// TestExpanded_ReportsAMappingWhoseMergesRunPastTheBound pins Expanded on each
// side of the bound, for the mapping that hit it alone, and for a node that is
// no mapping.
func TestExpanded_ReportsAMappingWhoseMergesRunPastTheBound(t *testing.T) {
	t.Parallel()
	v := New()
	deep := ynode.MergeChain(MergeDepthLimit + 2)
	assert.Nil(t, v.ChildByToken(deep, "leaf"), "the key lies past the bound")
	assert.False(t, v.Expanded(deep), "so its mapping is not expanded in full")
	assert.True(t, v.Expanded(ynode.MergeChain(MergeDepthLimit)), "a chain within the bound is")
	assert.True(t, v.Expanded(ynode.Alias(ynode.MergeChain(2))), "read through an alias")
	assert.True(t, v.Expanded(ynode.Scalar("x")), "a scalar has nothing to expand")
	assert.True(t, v.Expanded(nil))
}

// TestDocumentPath_StopsAtAMissingToken pins a walk a token stops: incomplete,
// its path ending at the last node reached.
func TestDocumentPath_StopsAtAMissingToken(t *testing.T) {
	t.Parallel()
	inner := ynode.Map(ynode.Scalar("b"), ynode.Scalar("leaf"))
	root := ynode.Map(ynode.Scalar("a"), inner)

	path, complete := New().DocumentPath(root, "/a/missing/deeper")
	assert.False(t, complete, "a token that names nothing stops the walk")
	assert.Equal(t, []*yaml.Node{root, inner}, path)
}

// TestDocumentPath_RootAndNil pins the walk's two ends: the empty pointer names
// the root, and a nil root reaches nothing.
func TestDocumentPath_RootAndNil(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), ynode.Scalar("v"))

	path, complete := New().DocumentPath(root, "")
	assert.True(t, complete, "a pointer with no tokens names the root")
	assert.Equal(t, []*yaml.Node{root}, path)

	path, complete = New().DocumentPath(nil, "/a")
	assert.False(t, complete)
	assert.Nil(t, path, "a nil root reaches nothing")
}

// TestDocumentPath_SeparatesTheLoneSlashFromTheRoot pins '/' as RFC 6901 reads
// it, one empty token: ids.Ptr("") spells the root member keyed "" exactly '/',
// so reading it as the root walks past the member the pointer names, and a
// caller reading $id down the path would miss one written there.
func TestDocumentPath_SeparatesTheLoneSlashFromTheRoot(t *testing.T) {
	t.Parallel()
	member := ynode.Map(ynode.Scalar("$id"), ynode.Scalar("https://example.com/root-member"))
	root := ynode.Map(ynode.Scalar(""), member)

	path, complete := New().DocumentPath(root, "/")
	assert.True(t, complete, `"/" resolves the one token it carries`)
	assert.Equal(t, []*yaml.Node{root, member}, path,
		`ids.Ptr("") spells the root member keyed "" as "/", so the walk descends into it`)

	path, complete = New().DocumentPath(root, "")
	assert.True(t, complete, "only the empty pointer names the root here")
	assert.Equal(t, []*yaml.Node{root}, path)
}

// TestDocumentPath_AnEmptyTokenNamesTheEmptyKey pins that an empty token names
// the key "", trailing or not, as RFC 6901 reads it. A walk that dropped or
// stayed on a trailing one would land '/a/' on a.
func TestDocumentPath_AnEmptyTokenNamesTheEmptyKey(t *testing.T) {
	t.Parallel()
	leaf := ynode.Scalar("leaf")
	member := ynode.Map(ynode.Scalar("b"), leaf)
	inner := ynode.Map(ynode.Scalar(""), member)
	root := ynode.Map(ynode.Scalar("a"), inner, ynode.Scalar("c"), ynode.Map(ynode.Scalar("b"), leaf))

	path, complete := New().DocumentPath(root, "/a/")
	require.True(t, complete, `the trailing token names the key ""`)
	assert.Same(t, member, path[len(path)-1])

	path, complete = New().DocumentPath(root, "/a//b")
	require.True(t, complete, `so does one before the last`)
	assert.Same(t, leaf, path[len(path)-1])

	_, complete = New().DocumentPath(root, "/c/")
	assert.False(t, complete, `c declares no key "", so the pointer lands nowhere rather than on c`)
}

// TestDocumentPath_SegmentCapStopsTheWalk pins the cap at its edge. A mapping
// whose only key is "a" and whose value is itself cannot be built from parsed
// YAML, but an alias can stand in: the walk follows "a" as long as tokens last,
// so only the cap can end it.
func TestDocumentPath_SegmentCapStopsTheWalk(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("a"), nil)
	root.Content[1] = root

	path, complete := New().DocumentPath(root, jsontext.Pointer(strings.Repeat("/a", maxPointerSegments)))
	require.True(t, complete, "a pointer at the segment cap resolves")
	assert.Same(t, root, path[len(path)-1])

	_, complete = New().DocumentPath(root, jsontext.Pointer(strings.Repeat("/a", maxPointerSegments+1)))
	assert.False(t, complete, "one past it does not")
}

// wideMap builds a mapping of minIndexedPairs pairs named k0..kN-1, plus any
// extra pairs given, so a fixture reaches the width at which a view indexes.
func wideMap(extra ...*yaml.Node) *yaml.Node {
	var content []*yaml.Node
	for i := range minIndexedPairs {
		content = append(content,
			ynode.Scalar(fmt.Sprintf("k%d", i)), ynode.Scalar(fmt.Sprintf("v%d", i)))
	}
	return ynode.Map(append(content, extra...)...)
}

// TestChildByToken_IndexAgreesWithTheScanItReplaces holds the key index to the
// scan it replaces, over mappings whose effective pairs are not their literal
// ones: a merge source and a key written twice.
//
// A map answers by key where a scan answers by position, so they agree only
// because expandContent yields each key once; comparing against MappingPairs
// key by key reddens if a duplicate ever survives an expansion.
//
// Every fixture is wide enough to index, and the index is asserted present
// before the reads, since otherwise the table passes with the index disabled,
// comparing the scan against itself.
func TestChildByToken_IndexAgreesWithTheScanItReplaces(t *testing.T) {
	t.Parallel()
	base := wideMap(ynode.Scalar("a"), ynode.Scalar("1"), ynode.Scalar("b"), ynode.Scalar("2"))
	tests := []struct {
		name string
		n    *yaml.Node
	}{
		{name: "explicit keys", n: wideMap()},
		{
			name: "merged keys",
			n:    wideMap(ynode.Merge(), ynode.Alias(base), ynode.Scalar("c"), ynode.Scalar("3")),
		},
		{
			name: "explicit beats merged",
			n:    wideMap(ynode.Merge(), ynode.Alias(base), ynode.Scalar("a"), ynode.Scalar("9")),
		},
		{name: "an aliased value", n: wideMap(ynode.Scalar("a"), ynode.Alias(ynode.Scalar("1")))},
		{
			name: "a key written twice",
			n:    wideMap(ynode.Scalar("a"), ynode.Scalar("1"), ynode.Scalar("a"), ynode.Scalar("2")),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := New()
			pairs := v.MappingPairs(tc.n)
			require.GreaterOrEqual(t, len(pairs), minIndexedPairs,
				"the fixture must be wide enough to index")

			for _, read := range []string{"builds the index", "reads it back"} {
				for _, p := range pairs {
					assert.Same(t, p.Val, v.ChildByToken(tc.n, p.Key), "%s: key %q", read, p.Key)
				}
				assert.Nil(t, v.ChildByToken(tc.n, "absent"), "%s: an unwritten key names nothing", read)
				require.NotNil(t, v.keys[tc.n], "%s: through the index, not the fallback scan", read)
			}
		})
	}
}

// TestKeyIndex_IsBoundedByThePairsMemoRatherThanCharged pins the two halves of
// the index's bound: it holds one entry per pair of a mapping the pairs memo
// kept, and it takes nothing from that memo's own budget.
//
// Charging it would halve the memo, and that memo is not a speed budget — it is
// what keeps a merge chain from going cubic, where the bug being fixed was a
// hang. Bounding it by the memo instead costs the memo nothing and still caps
// the index, because a mapping the memo declined is never indexed at all.
func TestKeyIndex_IsBoundedByThePairsMemoRatherThanCharged(t *testing.T) {
	t.Parallel()
	n := wideMap()
	v := New()

	pairs := v.MappingPairs(n)
	require.Len(t, pairs, minIndexedPairs)
	require.Equal(t, minIndexedPairs, v.cached.Spent(), "the expansion is charged")

	require.Same(t, n.Content[1], v.ChildByToken(n, "k0"))
	require.Nil(t, v.keys[n], "one read records the mapping without indexing it")
	require.Same(t, n.Content[3], v.ChildByToken(n, "k1"), "a second read is what builds one")

	require.NotNil(t, v.keys[n], "and indexed")
	assert.Equal(t, minIndexedPairs, v.cached.Spent(), "the index charges the pair budget nothing")
	assert.Len(t, v.keys[n], len(pairs), "one entry per pair, so the memo bounds it")
}

// TestKeyIndex_DeclinesWhatThePairsMemoDidNotKeep covers the gate that makes
// the index optional, from each state a read can reach it in: the budget, a
// merge cycle, and a mapping too narrow to repay an index.
//
// Each reaches the same outcome down a different path, and only the first is
// about the budget: a cycle yields no pairs, so width turns it away before the
// memo is consulted. None distinguishes the memo gate from a copy of memoize's
// budget test (see keyIndex), since at depth 0 the two select the same
// mappings.
func TestKeyIndex_DeclinesWhatThePairsMemoDidNotKeep(t *testing.T) {
	t.Parallel()
	t.Run("past the budget the scan still answers", func(t *testing.T) {
		t.Parallel()
		n := wideMap()
		v := New()
		holding(v, maxCachedPairs)
		require.NotContains(t, v.pairs, n, "the pairs were declined")

		assert.Same(t, n.Content[1], v.ChildByToken(n, "k0"), "the scan still answers")
		assert.Nil(t, v.ChildByToken(n, "absent"))
		assert.Empty(t, v.keys, "and nothing was indexed, not even a marker")
	})

	t.Run("a merge cycle expands to nothing, so nothing is indexed", func(t *testing.T) {
		t.Parallel()
		n := wideMap()
		v := New()
		v.inFlight[n] = true // expand refuses a mapping already being expanded

		assert.Nil(t, v.ChildByToken(n, "k0"), "an in-flight mapping expands to nothing")
		assert.NotContains(t, v.pairs, n, "and is never memoized")
		assert.Empty(t, v.keys, "so there is no expansion to index")
	})

	t.Run("a mapping too narrow to repay an index is scanned", func(t *testing.T) {
		t.Parallel()
		n := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
		v := New()

		assert.Same(t, n.Content[1], v.ChildByToken(n, "a"), "the scan answers")
		assert.Nil(t, v.ChildByToken(n, "absent"))
		assert.Empty(t, v.keys, "and nothing was indexed")
	})
}

// TestDocumentPath_ReachesTheIndexOnAWideMapping settles by assertion what
// BenchmarkDocumentPath_IntoAWideMapping can only show with a stopwatch, and
// nothing runs that in CI: a walk resolving many pointers through one view must
// leave an index on the mapping every one of them descends. If a gate stopped
// admitting it, through a raised width or a reuse marker never promoted, the
// walk would silently return to scanning.
//
// The pointers name distinct components because it is the shared mapping that
// must be indexed, not the entries they end at.
func TestDocumentPath_ReachesTheIndexOnAWideMapping(t *testing.T) {
	t.Parallel()
	const width = minIndexedPairs
	root := ComponentsDoc(width)
	v := New()

	for i := range width {
		_, complete := v.DocumentPath(root, jsontext.Pointer(fmt.Sprintf("/components/schemas/S%d", i)))
		require.True(t, complete, "every pointer resolves")
	}

	schemas := v.ChildByToken(v.ChildByToken(root, "components"), "schemas")
	require.NotNil(t, schemas, "the fixture has the mapping the walk descends")
	require.NotNil(t, v.keys[schemas],
		"the mapping every pointer descends is indexed, which a flat per-component cost rests on")
	assert.Len(t, v.keys[schemas], width, "one entry per component")
}

// TestNodeView_SharedViewAnswersAsAFreshOne pins that a memo hit never changes
// an answer (GitHub #404). The expansion is a function of the node and the depth
// it is reached at, and an entry memoized from a shallow read must not be
// served to a read that reaches the same node too deep to expand it in full —
// otherwise which schema a walk happened to read first decides what the view
// reports, and a view cannot be shared across walks.
func TestNodeView_SharedViewAnswersAsAFreshOne(t *testing.T) {
	t.Parallel()

	// head -> ... -> leaf, two links past the bound: a read from head truncates.
	// inner sits ten links down, where a read from it fits inside the bound.
	head := ynode.MergeChain(MergeDepthLimit + 2)
	inner := head
	for range 10 {
		inner = Deref(inner.Content[1])
	}

	fresh := New()
	wantHead := pairMap(fresh.MappingPairs(head))
	require.Empty(t, wantHead, "the chain from head is past the bound, so nothing survives")
	require.True(t, fresh.Exhausted())

	t.Run("a shallow memo is not served to a read that is too deep for it", func(t *testing.T) {
		t.Parallel()
		v := New()
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(v.MappingPairs(inner)),
			"from inner the chain fits inside the bound")
		require.Contains(t, v.pairs, inner, "and is memoized")
		assert.False(t, v.Exhausted())

		assert.Equal(t, wantHead, pairMap(v.MappingPairs(head)),
			"reading head afterwards still truncates where a fresh view does")
		assert.True(t, v.Exhausted(), "and still reports it")
	})

	t.Run("a truncating read does not spoil a later read beneath it", func(t *testing.T) {
		t.Parallel()
		v := New()
		require.Empty(t, v.MappingPairs(head))
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(v.MappingPairs(inner)),
			"inner is read at depth 0 and expands in full, as on a fresh view")
	})

	t.Run("a memo that fits the reader's budget is served", func(t *testing.T) {
		t.Parallel()
		v := New()
		_ = v.MappingPairs(inner)
		retained := v.cached.Spent()

		// Two links above inner: inner is reached at depth 2, and its own chain
		// fits under the bound from there, so the memo answers the read.
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(v.MappingPairs(linksAbove(inner, 2))))
		assert.False(t, v.Exhausted())
		assert.Equal(t, retained+2, v.cached.Spent(),
			"only the two new mappings were memoized; nothing beneath inner was recomputed")
	})

	// inner's chain is MergeDepthLimit-8 links, so from 8 links above it the whole
	// chain is exactly the bound and fits; from 9 it is one past and truncates.
	// Both readings must agree with a fresh view's, and the fitting one must be
	// the memo answering: refusing it recomputes the same pairs, so only the memo
	// count can see a bound drawn one short, where the answer sees one drawn one
	// long. Together they fix the bound's meaning.
	t.Run("the budget is exact at the bound", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			links int
			want  map[string]string
		}{
			{8, map[string]string{"leaf": "v"}},
			{9, map[string]string{}},
		} {
			above := linksAbove(inner, tc.links)
			fresh := New()
			require.Equal(t, tc.want, pairMap(fresh.MappingPairs(above)), "links=%d: fresh", tc.links)

			shared := New()
			_ = shared.MappingPairs(inner)
			retained := shared.cached.Spent()
			assert.Equal(t, tc.want, pairMap(shared.MappingPairs(above)), "links=%d: after inner", tc.links)
			assert.Equal(t, fresh.Exhausted(), shared.Exhausted(), "links=%d: exhausted", tc.links)
			if len(tc.want) == 0 {
				continue
			}
			assert.Equal(t, retained+tc.links, shared.cached.Spent(),
				"links=%d: the memo answered for inner; only the links above it are new", tc.links)
		}
	})
}

// linksAbove returns a mapping that reaches n through the given number of `<<`
// merges, one mapping per link.
func linksAbove(n *yaml.Node, links int) *yaml.Node {
	for range links {
		n = ynode.Map(ynode.Merge(), ynode.Alias(n))
	}
	return n
}

// pairSource is a document whose components/responses section holds the one
// mapping pair written as pair. Its anchors stand behind the aliases the pairs
// use, each named differently from what it anchors so that an alias left
// unfollowed reads differently from one followed.
func pairSource(pair string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n" +
		"x-anchors: {&scalar named: 1, &merge <<: {}, &mapping {k: v}: 1}\n" +
		"paths: {}\ncomponents:\n  responses:\n    " + pair + "\n"
}

// pairRead parses the document pairSource builds for pair and returns the keys
// the parser gives the section's entries, and the raw key node of the one pair
// as the view reads it.
func pairRead(t *testing.T, pair string) ([]string, *yaml.Node) {
	t.Helper()
	src := pairSource(pair)
	parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	require.NotNil(t, parsed.Components.Responses, "the parser reads the section")

	v := New()
	section := v.ChildByToken(v.ChildByToken(YAMLDoc(t, src), "components"), "responses")
	require.NotNil(t, section, "the fixture declares the section")
	require.Len(t, section.Content, 2, "the section holds the one pair")
	return slices.Collect(parsed.Components.Responses.Keys()), section.Content[0]
}

// TestKeyOf_KeysAPairAsTheParserDoes holds the key KeyOf gives a raw pair key to
// the parser's: each pair is written into a section the parser reads, and the
// keys it gives that section's entries are the one KeyOf names, or none where
// KeyOf says the pair names none. A `<<` merge key names none, here a merge
// of nothing; a scalar tagged !!merge with any other value, a quoted `<<` and
// an alias standing in for `<<` are ordinary keys; an alias key names the
// scalar it stands for. The key and own columns pin today's parser.
func TestKeyOf_KeysAPairAsTheParserDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, pair string
		key        string
		own        bool
	}{
		{"a plain key", "a: {description: x}", "a", true},
		{"a merge key", "<<: {}", "", false},
		{"a quoted <<", "'<<': {description: x}", "<<", true},
		{"a key tagged !!merge that is not <<", "!!merge tagged: {description: x}", "tagged", true},
		{"an alias key", "*scalar : {description: x}", "named", true},
		{"an alias standing in for <<", "*merge : {description: x}", "<<", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parser, raw := pairRead(t, tc.pair)
			want := []string{}
			if tc.own {
				want = append(want, tc.key)
			}
			assert.ElementsMatch(t, want, parser, "the keys the parser gives the section's entries")

			key, own := KeyOf(raw)

			assert.Equal(t, tc.key, key)
			assert.Equal(t, tc.own, own)
		})
	}
}

// TestKeyOf_ReadsAKeyThatIsNoScalarAsTheEmptyString holds KeyOf to the parser,
// which keys a pair whose key is no scalar, aliased or not, with the empty
// string (docs/parser-library.md#non-scalar-key), so the view reads the entry
// the parser reads. No node at all is the one row the parser cannot be asked,
// since it never hands one back, and it names no key.
func TestKeyOf_ReadsAKeyThatIsNoScalarAsTheEmptyString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, pair string
	}{
		{"a mapping key", "? {k: v}\n    : {description: x}"},
		{"a sequence key", "? [k]\n    : {description: x}"},
		{"an alias to a mapping", "*mapping : {description: x}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parser, raw := pairRead(t, tc.pair)
			assert.Equal(t, []string{""}, parser, "the parser keys the pair with the empty string")

			key, own := KeyOf(raw)

			assert.Empty(t, key)
			assert.True(t, own)
		})
	}

	key, own := KeyOf(nil)

	assert.Empty(t, key)
	assert.False(t, own)
}

func TestRepeatedKeys_NamesEachRepeatedKeyOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, doc string
		want      []string
	}{
		{"no repeat", "{a: 1, b: 2}", nil},
		{"one repeat", "{a: 1, b: 2, a: 3}", []string{"a"}},
		{"three copies", "{a: 1, a: 2, a: 3}", []string{"a"}},
		{"two keys repeated", "{a: 1, b: 1, b: 2, a: 2}", []string{"b", "a"}},
		{"a key shared with a merge is no repeat", "{<<: {a: 1}, a: 2}", nil},
		{"a merge key twice is no repeat", "{<<: {a: 1}, <<: {b: 1}}", nil},
		{"two keys that are no scalar both read as the empty string", "{? [x] : 1, ? [y] : 2}", []string{""}},
		{"a sequence repeats nothing", "[a, a]", nil},
		{"a scalar repeats nothing", "a", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.doc), &doc))
			assert.Equal(t, tc.want, RepeatedKeys(doc.Content[0]))
		})
	}
	assert.Nil(t, RepeatedKeys(nil))
}
