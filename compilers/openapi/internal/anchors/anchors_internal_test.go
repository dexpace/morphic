package anchors

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBuild_WalksEveryNodeShape drives the walk over the shapes a YAML
// document can present, rather than only the mappings a schema happens to be
// written as. It meets whatever the source wrote — a sequence, a bare scalar,
// a key that is no scalar — and must index anchors under a sequence and under
// the empty key the parser reads such a key as, while declining the rest.
func TestBuild_WalksEveryNodeShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		node *yaml.Node
		want map[string][]jsontext.Pointer
	}{
		{"a nil node yields nothing", nil, map[string][]jsontext.Pointer{}},
		{
			"a bare scalar declares no anchor",
			openapitest.YAMLNode(t, `just-a-string`),
			map[string][]jsontext.Pointer{},
		},
		{
			"a sequence indexes its elements by ordinal",
			openapitest.YAMLNode(t, "- {$dynamicAnchor: first}\n- {other: 1}\n- {$dynamicAnchor: third}\n"),
			map[string][]jsontext.Pointer{"first": {"/0"}, "third": {"/2"}},
		},
		{
			"a sequence element standing in for a mapping is followed",
			openapitest.YAMLNode(t, "- &a {$dynamicAnchor: first}\n- *a\n"),
			map[string][]jsontext.Pointer{"first": {"/0", "/1"}},
		},
		{
			"a key that is no scalar is read as the empty string",
			openapitest.YAMLNode(t, "? [a, b]\n: {$dynamicAnchor: buried}\n$dynamicAnchor: reached\n"),
			map[string][]jsontext.Pointer{"buried": {"/"}, "reached": {""}},
		},
		{
			"an empty anchor name is not indexed",
			openapitest.YAMLNode(t, `{$dynamicAnchor: ""}`),
			map[string][]jsontext.Pointer{},
		},
		{
			"a non-scalar anchor value is not indexed",
			openapitest.YAMLNode(t, `{$dynamicAnchor: [a]}`),
			map[string][]jsontext.Pointer{},
		},
		{
			"an anchor written as an alias of a scalar is indexed under the scalar",
			openapitest.YAMLNode(t, "names: [&n tail]\nA: {$dynamicAnchor: *n}\n"),
			map[string][]jsontext.Pointer{"tail": {"/A"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x := Build(tc.node)
			assert.Empty(t, cmp.Diff(tc.want, x.byName))
			assert.True(t, x.Complete(), "nothing here reaches a walk bound")
		})
	}
}

// TestBuild_CountsWhatAnAliasBringsIn pins the count the whole expansion rests
// on. A YAML alias and a `<<` merge key each hand a second position the anchor
// the first one wrote, and the parser reads both positions as declaring it, so
// an index that walked only the spelled-out tree would report one declaration
// where the document has two, and "declared exactly once" would expand a
// reference that is genuinely ambiguous.
func TestBuild_CountsWhatAnAliasBringsIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, source string
		want         []jsontext.Pointer
	}{
		{
			name:   "an alias re-declares the anchor at its own position",
			source: "A: &base {$dynamicAnchor: tail}\nB: *base\n",
			want:   []jsontext.Pointer{"/A", "/B"},
		},
		{
			name:   "a merge key does too",
			source: "A: &base {$dynamicAnchor: tail}\nB: {<<: *base, description: merged}\n",
			want:   []jsontext.Pointer{"/A", "/B"},
		},
		{
			name:   "an explicit key still beats the merged one",
			source: "A: &base {$dynamicAnchor: tail}\nB: {<<: *base, $dynamicAnchor: own}\n",
			want:   []jsontext.Pointer{"/A"},
		},
		{
			name:   "a key written twice declares the anchor its last copy names",
			source: "A: {$dynamicAnchor: own, $dynamicAnchor: tail}\n",
			want:   []jsontext.Pointer{"/A"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x := Build(openapitest.YAMLNode(t, tc.source))
			assert.True(t, x.Complete())
			assert.Equal(t, tc.want, x.Sites("tail"))
		})
	}
}

// TestBuild_DocumentNodeUnwraps pins that the walk accepts a whole document as
// well as a content node, since it is handed whatever the loader kept.
func TestBuild_DocumentNodeUnwraps(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("$dynamicAnchor: top\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	x := Build(&doc)
	assert.Equal(t, map[string][]jsontext.Pointer{"top": {""}}, x.byName)
	assert.True(t, x.Complete())
}

// TestBuild_StopsAtTheDepthCap pins the depth bound at its boundary. A raw tree
// deeper than the cap is a legal document, so the walk must stop rather than
// read on; an anchor within the cap is still indexed, and the truncation is
// reported rather than passed off as a complete index.
func TestBuild_StopsAtTheDepthCap(t *testing.T) {
	t.Parallel()
	within, past := Build(nestedAnchor(MaxDepth)), Build(nestedAnchor(MaxDepth+1))

	assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(strings.Repeat("/k", MaxDepth))}, within.Sites("deep"),
		"the deepest anchor the cap admits is indexed")
	assert.True(t, within.Complete())
	assert.Empty(t, past.Sites("deep"), "one level further is not read")
	assert.False(t, past.Complete(), "and says so, since an unseen anchor undercounts every name")
}

// TestBuild_StopsAtTheNodeBudget pins the other bound. Depth alone does not
// bound a walk that follows aliases, since an alias DAG presents many more
// paths than the tree has nodes; the budget is what caps the total.
func TestBuild_StopsAtTheNodeBudget(t *testing.T) {
	t.Parallel()
	source := openapitest.YAMLNode(t, "a: {$dynamicAnchor: first}\nb: {$dynamicAnchor: second}\n")

	cut := build(source, MaxDepth, 2) // the root mapping and its first value, and no more
	assert.False(t, cut.Complete(), "the budget ran out mid-walk")
	assert.Equal(t, []jsontext.Pointer{"/a"}, cut.Sites("first"))
	assert.Empty(t, cut.Sites("second"), "so the anchors past it are missing")

	whole := build(source, MaxDepth, 3)
	assert.True(t, whole.Complete(), "a budget of every node reads them all")
	assert.Equal(t, []jsontext.Pointer{"/b"}, whole.Sites("second"))
}

// TestBuild_ReportsAMergeChainPastTheBound pins the third bound: a mapping
// whose `<<` chain runs deeper than the view reads holds keys the walk never
// sees, which reads as an undercount just as a depth cut does.
func TestBuild_ReportsAMergeChainPastTheBound(t *testing.T) {
	t.Parallel()
	within := Build(openapitest.YAMLNode(t, openapitest.NestedMerges(nodeview.MergeDepthLimit, "{$dynamicAnchor: tail}")))
	past := Build(openapitest.YAMLNode(t, openapitest.NestedMerges(nodeview.MergeDepthLimit+1, "{$dynamicAnchor: tail}")))

	assert.True(t, within.Complete())
	assert.NotEmpty(t, within.Sites("tail"), "a chain the view reads brings the anchor in")
	assert.False(t, past.Complete(), "one past the bound is reported")
}

// TestSites_ReturnsACopy pins that a reader cannot change the index through
// what Sites hands it.
func TestSites_ReturnsACopy(t *testing.T) {
	t.Parallel()
	x := Build(openapitest.YAMLNode(t, "A: {$dynamicAnchor: tail}\n"))
	x.Sites("tail")[0] = "/B"
	assert.Equal(t, []jsontext.Pointer{"/A"}, x.Sites("tail"))
}

// nestedAnchor builds a chain of depth mappings, each holding the next under
// the key k, around one declaring $dynamicAnchor deep, so the declaring
// mapping sits depth levels below the root. It is built rather than
// parsed because nesting this deep in source text is indentation arithmetic.
func nestedAnchor(depth int) *yaml.Node {
	n := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{openapitest.StrNode("$dynamicAnchor"), openapitest.StrNode("deep")},
	}
	for range depth {
		n = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{openapitest.StrNode("k"), n}}
	}
	return n
}
