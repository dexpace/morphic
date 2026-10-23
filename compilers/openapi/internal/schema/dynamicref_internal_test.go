package schema

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestDeclaresResourceIDAbove_WithoutARawTree pins the guard on the path walk.
// The walk reads the raw source because $id has no oas3.Schema field, so a
// document with no raw tree behind it — one built in memory, or a pointer naming
// a position the source does not have — must read as "no resource boundary
// found" rather than wander off the tree.
//
// No committed spec reaches this: every pointer a $dynamicRef is lowered at was
// built by walking the very tree this re-walks. The guard is exercised directly
// because the hazard is a later caller passing a synthesized pointer, not a live
// defect.
func TestDeclaresResourceIDAbove_WithoutARawTree(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})

	assert.False(t, declaresResourceIDAbove(l.ctx, "/components/schemas/A"),
		"a document with no raw tree declares no resource anywhere")
}

// TestDeclaresResourceIDAbove_EmptySegmentIsATokenNotAnArtifact pins which
// empty segments the path walk may drop. Splitting a pointer on '/' yields one
// leading empty segment no reference token stands behind; every later one names
// the key "", which is how a schema literally named "" is addressed. Dropping
// those would read the $id of the components/schemas map instead of the
// schema's own.
//
// The document root carries a member named "" with an $id, which makes the two
// rootward cases discriminating: the empty pointer names the root and must not
// reach it, while "/" is how ids.Ptr spells that member and must. Without the
// $id both read false.
func TestDeclaresResourceIDAbove_EmptySegmentIsATokenNotAnArtifact(t *testing.T) {
	t.Parallel()
	l, diags := loweredFor(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
"": {$id: https://example.com/root-member}
components:
  schemas:
    "":
      $id: https://example.com/empty
      properties: {x: {type: string}}
    Sibling: {type: string}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	for name, tc := range map[string]struct {
		pointer jsontext.Pointer
		want    bool
		why     string
	}{
		"the position is named by a trailing empty token": {
			"/components/schemas/", true,
			"the trailing token names the schema itself, whose own $id is the boundary",
		},
		"an interior empty token still descends": {
			"/components/schemas//properties/x", true,
			"the $id above a property is still a boundary over it",
		},
		"a sibling sits above no $id at all": {
			"/components/schemas/Sibling", false,
			"a neighbour of the named schema inherits no boundary from it",
		},
		"the empty pointer names the document root": {
			"", false,
			`the empty pointer stops at the root, so the $id under the root's "" member is out of reach`,
		},
		"a lone slash names the root member keyed \"\"": {
			"/", true,
			`ids.Ptr("") spells that member "/", so its own $id is the boundary — reading "/" as the root, ` +
				"the way a reference resolves, would walk straight past it",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, declaresResourceIDAbove(l.ctx, tc.pointer), tc.why)
		})
	}
}

// TestDynamicAnchors_WalksEveryNodeShape drives the raw-tree walk over the
// shapes a YAML document can present, rather than only the mappings a schema
// happens to be written as. The walk reads the raw tree because oas3.Schema has
// no $dynamicAnchor field, so it meets whatever the source wrote — a sequence, a
// bare scalar, a non-string key — and must index anchors under a sequence while
// declining the rest without wandering off the tree.
func TestDynamicAnchors_WalksEveryNodeShape(t *testing.T) {
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
			"a non-string key cannot name a keyword and is skipped",
			openapitest.YAMLNode(t, "? [a, b]\n: {$dynamicAnchor: buried}\n$dynamicAnchor: reached\n"),
			map[string][]jsontext.Pointer{"reached": {""}},
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, complete := dynamicAnchors(tc.node)
			assert.Empty(t, cmp.Diff(tc.want, got))
			assert.True(t, complete, "nothing here reaches a walk bound")
		})
	}
}

// TestDynamicAnchors_CountsWhatAnAliasBringsIn pins the count the whole
// expansion rests on. A YAML alias and a `<<` merge key each hand a second
// position the anchor the first one wrote, and the parser downstream reads both
// positions as declaring it — so an index that walked only the spelled-out tree
// would report one declaration where the document has three, and "declared
// exactly once" would expand a reference that is genuinely ambiguous.
func TestDynamicAnchors_CountsWhatAnAliasBringsIn(t *testing.T) {
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, complete := dynamicAnchors(openapitest.YAMLNode(t, tc.source))
			assert.True(t, complete)
			assert.Equal(t, tc.want, got["tail"])
		})
	}
}

// TestDynamicAnchors_DocumentNodeUnwraps pins that the walk accepts a whole
// document as well as a content node, since dynamicAnchors is handed whatever
// the loader kept.
func TestDynamicAnchors_DocumentNodeUnwraps(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("$dynamicAnchor: top\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	got, complete := dynamicAnchors(&doc)
	assert.Equal(t, map[string][]jsontext.Pointer{"top": {""}}, got)
	assert.True(t, complete)
}

// TestDynamicAnchors_StopsAtTheDepthCap pins the bound. A raw tree deeper than
// the cap is a legal document, so the walk must stop rather than recur to
// exhaustion; anchors above the cap are still indexed, and the truncation is
// reported rather than passed off as a complete index.
func TestDynamicAnchors_StopsAtTheDepthCap(t *testing.T) {
	t.Parallel()
	deep, shallow := nestedAnchor(maxDynamicAnchorDepth+2), nestedAnchor(0)

	deepIndex, deepComplete := dynamicAnchors(deep)
	assert.NotContains(t, deepIndex, "toodeep",
		"the walk stops at the cap instead of recurring to exhaustion")
	assert.False(t, deepComplete, "and says so, since an unseen anchor undercounts every name")

	shallowIndex, shallowComplete := dynamicAnchors(shallow)
	assert.Contains(t, shallowIndex, "toodeep",
		"an anchor within the cap is still indexed, so the cap is what excluded the other")
	assert.True(t, shallowComplete)
}

// TestAnchorWalk_StopsAtTheNodeBudget pins the other bound. Depth alone stopped
// bounding the walk once it began following aliases, since an alias DAG presents
// many more paths than the tree has nodes; the budget is what caps the total.
func TestAnchorWalk_StopsAtTheNodeBudget(t *testing.T) {
	t.Parallel()
	source := openapitest.YAMLNode(t, "a: {$dynamicAnchor: first}\nb: {$dynamicAnchor: second}\n")

	w := newAnchorWalk(2) // the root mapping and its first value, and no more
	w.walk(source, "", 0)

	assert.True(t, w.truncated, "the budget ran out mid-walk")
	assert.NotContains(t, w.out, "second", "so the anchors past it are missing")
}

// TestDynamicAnchorIndex_ReportsATruncatedWalk pins what a reader is told when
// the index is partial. Truncation only ever drops declarations, and a dropped
// duplicate reads as "declared exactly once" — the count that expands — so an
// unreported truncation would turn an ambiguous reference into a confident one.
func TestDynamicAnchorIndex_ReportsATruncatedWalk(t *testing.T) {
	t.Parallel()
	// The walk descends into every key, so an extension at the document root
	// nests the tree past the cap without the schema lowering ever seeing it.
	deep := strings.Repeat("{a: ", maxDynamicAnchorDepth) + "1" + strings.Repeat("}", maxDynamicAnchorDepth)
	l, diags := loweredFor(t, openapitest.ComponentSpec("    A: {type: string}\n")+"x-deep: "+deep+"\n")
	openapitest.RequireNoErrorDiags(t, diags)

	_, got := l.anchors.sites(l.ctx, "absent")
	require.NotNil(t, l.anchors.byName, "the index is built even when partial")
	require.Len(t, got, 1, "the truncation is announced exactly once: %+v", got)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
	assert.Contains(t, got[0].Message, "not verified")

	_, again := l.anchors.sites(l.ctx, "absent")
	assert.Empty(t, again, "and the cached index does not announce it again")
}

// TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody pins the split
// the function exists for: a component pointer has a body, and a pointer into
// that same component does not.
//
// The fixture declares an empty-named component beside the real one, which
// makes the guard observable. A deeper pointer's component name comes back
// empty, so without the guard the lookup asks for "" and, in a document
// declaring no such name, misses and returns nil, which looks exactly like the
// right answer (#202).
func TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody(t *testing.T) {
	t.Parallel()
	l, diags := loweredFor(t, openapitest.ComponentSpec(
		"    Outer:\n      type: object\n      title: outer\n"+
			"      properties: {inner: {type: string, title: inner}}\n"+
			"    \"\": {type: string, title: empty}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	tests := []struct {
		name, wantTitle string
		pointer         jsontext.Pointer
	}{
		{name: "the component itself", pointer: "/components/schemas/Outer", wantTitle: "outer"},
		// The empty name is a name the document may declare, but it is not one this
		// grammar addresses: a pointer ending at the slash names no entry, so the
		// component it declares is reached as an anonymous position instead.
		{name: "the pointer that names no entry", pointer: "/components/schemas/"},
		{name: "a property inside that component", pointer: "/components/schemas/Outer/properties/inner"},
		{name: "a component of another kind", pointer: "/components/parameters/Outer"},
		{name: "no component pointer at all", pointer: "/paths/~1a/get"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := componentSchemaAt(l.ctx, tc.pointer)
			if tc.wantTitle == "" {
				assert.Nil(t, got, "only a top-level component schema has a body here")
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantTitle, got.GetTitle(), "and it is that component's own body")
		})
	}
}

// TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed pins every way the
// chain walk can stop, since a wrong verdict either expands what it should
// refuse or refuses what it should expand (#202).
//
// A hop needs a component at the pointer, a $dynamicRef written on it, and
// exactly one site declaring the anchor it names. Anything else ends the chain,
// and a chain that ends has no cycle and expands, which is the direction sites
// already errs in. Hopping on any other count would pick an arbitrary branch,
// reporting a cycle the document does not have, or index an empty slice.
func TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, schemas, anchor string
		at                    jsontext.Pointer
		wantSites             int
		wantNext              jsontext.Pointer
	}{
		{
			name: "the anchor is declared twice, so it names no single target",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    B: {$dynamicAnchor: m, type: integer}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 2,
		},
		{
			name:    "the anchor is declared nowhere, so it names nothing",
			schemas: "    C: {$dynamicRef: '#ghost'}\n",
			at:      "/components/schemas/C", anchor: "ghost", wantSites: 0,
		},
		{
			name:    "the component writes no $dynamicRef at all",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C",
		},
		{
			name:    "the pointer names no component",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C/properties/inner",
		},
		{
			name: "declared once, which is the only shape that hops",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 1,
			wantNext: "/components/schemas/A",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, diags := loweredFor(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)
			if tc.anchor != "" {
				sites, siteDiags := l.anchors.sites(l.ctx, tc.anchor)
				require.Len(t, sites, tc.wantSites, "the fixture must set up the case it is named for")
				require.Empty(t, siteDiags)
			}

			next, hops, hopDiags := dynamicHop(l.ctx, &l.anchors, tc.at)

			assert.Equal(t, tc.wantNext != "", hops)
			assert.Equal(t, tc.wantNext, next)
			assert.Empty(t, hopDiags)
		})
	}
}
