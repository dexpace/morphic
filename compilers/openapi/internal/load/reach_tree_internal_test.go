package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// treeOf indexes spec the way newReach does and returns the tree with what
// index found. The spec is a bare YAML document, not an OpenAPI one: tree reads
// nodes, not the model.
func treeOf(t *testing.T, spec string) (*tree, []*yaml.Node, []*yaml.Node) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(spec), &root))
	tr := newTree(&root, &budget{limit: maxReachWork})
	declared, defsRefs := tr.index()
	return tr, declared, defsRefs
}

func TestBudget_SpendReportsWhetherTheLimitHolds(t *testing.T) {
	t.Parallel()
	b := &budget{limit: 3}
	assert.True(t, b.spend(2))
	assert.False(t, b.exhausted())
	assert.True(t, b.spend(1), "spending exactly the limit still holds")
	assert.False(t, b.exhausted())
	assert.False(t, b.spend(1))
	assert.True(t, b.exhausted(), "a spend past the limit is remembered")
	assert.False(t, b.spend(0), "and so is every later one")
}

// TestTree_ReadsThroughMergeKeys pins what the raw-Content reader this
// replaced got wrong: a key a merge supplies is the mapping's own key as far
// as the resolver is concerned, so child, refValue and index must all see it.
func TestTree_ReadsThroughMergeKeys(t *testing.T) {
	t.Parallel()
	tr, declared, defsRefs := treeOf(t, `base: &b {$anchor: a, $ref: "#/$defs/n"}
merged: {<<: *b}
plain: {type: string}
`)
	merged := tr.child(tr.root, "merged")
	require.NotNil(t, merged)

	assert.Equal(t, "#/$defs/n", tr.refValue(merged), "a merged $ref is read")
	require.NotNil(t, tr.child(merged, "$anchor"), "a merged $anchor is read")
	assert.Contains(t, declared, merged, "a mapping declaring only through a merge is a declaration")
	assert.Contains(t, defsRefs, merged, "a mapping whose $defs pointer a merge supplies is a $defs reference")
	assert.NotContains(t, declared, tr.child(tr.root, "plain"))
}

// TestTree_ParentsIncludeEveryMappingASharedNodeIsAChildOf pins parents: a node
// an alias or merge key shares has each of them as a parent, which is what
// markSearched and scopesOf climb.
func TestTree_ParentsIncludeEveryMappingASharedNodeIsAChildOf(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, `shared: &s {type: string}
first: {x: *s}
second: {y: *s}
`)
	shared := tr.child(tr.root, "shared")
	require.NotNil(t, shared)
	assert.ElementsMatch(t,
		[]*yaml.Node{tr.root, tr.child(tr.root, "first"), tr.child(tr.root, "second")},
		tr.parents[shared])
}

func TestTree_PairsKeyASequenceByIndex(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, "list: [a, b]\nscalar: x\n")
	list := tr.child(tr.root, "list")

	pairs := tr.pairs(list)
	require.Len(t, pairs, 2)
	assert.Equal(t, "0", pairs[0].Key)
	assert.Equal(t, "b", pairs[1].Val.Value)
	assert.Nil(t, tr.pairs(tr.child(tr.root, "scalar")), "a scalar has no children")
	assert.Nil(t, tr.pairs(nil), "and neither does nothing")
	assert.Equal(t, "b", tr.child(list, "1").Value)
	assert.Nil(t, tr.child(list, "2"), "past the end of the sequence")
	assert.Nil(t, tr.child(nil, "x"))
	for _, token := range []string{"01", "+1", "-0", "-1", "x"} {
		assert.Nil(t, tr.child(list, token), "the library reads %q as no index", token)
	}
}

func TestTree_WalkStopsAtAMissingToken(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, "a: {b: {c: found}}\n")
	assert.Equal(t, "found", tr.walk(tr.root, []string{"a", "b", "c"}).Value)
	assert.Same(t, tr.root, tr.walk(tr.root, nil), "no tokens is the node itself")
	assert.Nil(t, tr.walk(tr.root, []string{"a", "x", "c"}))
}

func TestTree_RefValueIsEmptyUnlessAScalar(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, "none: {type: string}\nlist: {$ref: [x]}\nlive: {$ref: \"#/a\"}\n")
	assert.Empty(t, tr.refValue(tr.child(tr.root, "none")))
	assert.Empty(t, tr.refValue(tr.child(tr.root, "list")), "a non-scalar $ref names nothing")
	assert.Equal(t, "#/a", tr.refValue(tr.child(tr.root, "live")))
}

// TestTree_NavigateIndexesEachSetOnce pins navigate: a set's children are
// indexed by key on the first step through it, keys a merge supplies included,
// and every later pointer through the same prefix reads that index.
func TestTree_NavigateIndexesEachSetOnce(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, `common: &c {m: {type: string}}
one: {$defs: {n: {type: string}}}
two: {$defs: {<<: *c, n: {type: integer}}}
`)
	defs := tr.navigate(tr.anywhere, []string{"$defs"})
	require.NotNil(t, defs)

	assert.Len(t, tr.navigate(tr.anywhere, []string{"$defs", "n"}).members, 2, "one n under each $defs")
	assert.Len(t, tr.navigate(tr.anywhere, []string{"$defs", "m"}).members, 1,
		"a key a merge supplies is indexed under the mapping that inherits it")
	assert.Nil(t, tr.navigate(tr.anywhere, []string{"$defs", "absent"}))
	assert.Same(t, defs, tr.navigate(tr.anywhere, []string{"$defs"}), "no tokens past the set is the set itself")

	planted := &posSet{}
	defs.next["planted"] = planted
	assert.Same(t, planted, tr.navigate(tr.anywhere, []string{"$defs", "planted"}),
		"a later pointer reads the index the first one built")
}

// TestTree_SetsHoldAChildTwoParentsShareOnce pins the deduplication in index and
// below: an alias that puts one node under the same key of two parents leaves
// one member, so a set never grows with the number of paths to a node.
func TestTree_SetsHoldAChildTwoParentsShareOnce(t *testing.T) {
	t.Parallel()
	tr, _, _ := treeOf(t, `shared: &s {type: string}
one: {x: {k: *s}}
two: {x: {k: *s}}
`)
	assert.Len(t, tr.navigate(tr.anywhere, []string{"k"}).members, 1, "index reads both parents")
	require.Len(t, tr.navigate(tr.anywhere, []string{"x"}).members, 2)
	assert.Len(t, tr.navigate(tr.anywhere, []string{"x", "k"}).members, 1, "below reads both members")
}

func TestTree_NavigateStopsWhenTheBudgetRunsOut(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("one: {$defs: {a: x}}\ntwo: {$defs: {b: y}}\n"), &root))
	b := &budget{limit: maxReachWork}
	tr := newTree(&root, b)
	tr.index()
	b.limit = b.spent + 3

	defs := tr.navigate(tr.anywhere, []string{"$defs"})
	require.NotNil(t, defs)
	assert.Nil(t, tr.navigate(defs, []string{"b"}), "it stops at the member that crossed the limit")
	assert.True(t, b.exhausted())
	assert.Nil(t, tr.navigate(tr.anywhere, []string{"one"}), "and takes no further step")
}

// TestIsDefsRef pins isDefsRef: the resolver decides on the decoded pointer
// between the first '#' and the next, and a reference with no '#' has none.
func TestIsDefsRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw  string
		want bool
	}{
		{"#/$defs/n", true},
		{"#/$defs/n/properties/p", true},
		{"a.json#/$defs/n", true},
		{"  #/$defs/n  ", true},
		{"#/%24defs/n", true},
		{"#/$defs%2Fn", true},
		{"#/$defs/n#x", true},
		{"#/$defs/", true},
		{"#/$defs", false},
		{"#/components/schemas/A", false},
		{"#/components/schemas/A#/$defs/n", false},
		{"#a#/$defs/n", false},
		{"$defs/n", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isDefsRef(tc.raw))
		})
	}
}

// TestKeyOfURI pins keyOfURI: the last segment is read after the library's own
// normalization, so what a '.' or '..' segment or a percent-encoding changes in
// it is changed here too, and a URI with no path of its own matches anything.
func TestKeyOfURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, uri string
		want      uriKey
	}{
		{"an absolute URI", "https://x.test/y/a", uriKey{segment: "a"}},
		{"a trailing slash", "https://x.test/y/", uriKey{}},
		{"a fragment is dropped", "https://x.test/y/a#frag", uriKey{segment: "a"}},
		{"a query is dropped", "https://x.test/y/a?v=1", uriKey{segment: "a"}},
		{"a relative file", "a.json", uriKey{segment: "a.json"}},
		{"a leading ./", "./a", uriKey{segment: "a"}},
		{"parent segments", "../../a", uriKey{segment: "a"}},
		{"a dot is a directory", ".", uriKey{}},
		{"a trailing .. is a directory", "foo/..", uriKey{}},
		{"a .. mid-path", "a/../b", uriKey{segment: "b"}},
		{"a network path", "//x.test/a", uriKey{segment: "a"}},
		{"a space and its encoding read alike", "https://x.test/a b", uriKey{segment: "a%20b"}},
		{"an encoded space", "https://x.test/a%20b", uriKey{segment: "a%20b"}},
		{"a URN has no slash to cut at", "urn:x:a", uriKey{segment: "urn:x:a"}},
		{"a URI that does not parse is kept as written", "http://x/%zz", uriKey{segment: "%zz"}},
		{"a query alone takes its base's path", "?q=1", uriKey{any: true}},
		{"a fragment alone takes its base's path", "#frag", uriKey{any: true}},
		{"nothing takes its base's path", "", uriKey{any: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, keyOfURI(tc.uri))
		})
	}
}

// TestDecls_Identified pins which $ids a reference's URI is compared with: those
// sharing its last segment and those with no path of their own, or every one
// for a reference with no path of its own.
func TestDecls_Identified(t *testing.T) {
	t.Parallel()
	scope, a, b, pathless := &yaml.Node{}, &yaml.Node{}, &yaml.Node{}, &yaml.Node{}
	d := newDecls()
	d.addID([]*yaml.Node{scope}, keyOfURI("http://x.test/a"), a)
	d.addID([]*yaml.Node{scope}, keyOfURI("http://x.test/b"), b)
	d.addID([]*yaml.Node{scope}, keyOfURI("?q=1"), pathless)
	members := func(sets []*posSet) []*yaml.Node {
		var out []*yaml.Node
		for _, s := range sets {
			if s != nil {
				out = append(out, s.members...)
			}
		}
		return out
	}

	assert.ElementsMatch(t, []*yaml.Node{a, pathless}, members(d.identified(scope, keyOfURI("a"))))
	assert.ElementsMatch(t, []*yaml.Node{a, b, pathless}, members(d.identified(scope, keyOfURI("?x"))))
	assert.Empty(t, members(d.identified(&yaml.Node{}, keyOfURI("a"))), "another scope holds none of them")
}
