// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_AnUnknownCodeRuleIsATableError pins that a Code row the engine
// does not implement is reported, never skipped.
func TestCheck_AnUnknownCodeRuleIsATableError(t *testing.T) {
	spec := Table()
	obj := spec.must(Info)
	obj.Rules = append(obj.Rules, code(Always(), "no-such-predicate", "", Cite{Minor: V30, Section: "Info Object", Sentence: "x"}))
	spec.objects[spec.index[Info]] = obj
	diags := Checker{spec: spec}.Check(t.Context(), openapitest.YAMLDocument(t, `{title: t, version: v}`), "3.1.0", Options{Start: Info})
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, "no-such-predicate")
}

// TestCheck_WorkIsBounded pins that the walk stops past its budget, not at it,
// and at its depth whatever the budget, and says so once, as an error at the
// node it stopped on, one inside a value read as no object among them. An unset
// budget is unbounded.
func TestCheck_WorkIsBounded(t *testing.T) {
	deep := "{}"
	for range MaxDepth + 8 {
		deep = "{properties: {a: " + deep + "}}"
	}
	const five = `{properties: {a: {}, b: {}, c: {}, d: {}}}` // five schemas, each an object read
	tests := []struct {
		name  string
		doc   string
		limit budget.Limit
		at    string
	}{
		{"a budget one short of the objects", five, budget.AtMost(4), "/properties/d"},
		{"a budget of the objects", five, budget.AtMost(5), ""},
		// One schema, then a list and three mappings read as no object.
		{"a budget one short of a value's mappings and lists", `{default: [{a: {}}, {}]}`, budget.AtMost(4), "/default/1"},
		{"a budget of a value's mappings and lists", `{default: [{a: {}}, {}]}`, budget.AtMost(5), ""},
		{"no budget", five, budget.Unbounded(), ""},
		{"depth", deep, budget.Unbounded(), strings.Repeat("/properties/a", MaxDepth+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := NewChecker().Check(t.Context(), openapitest.YAMLDocument(t, tc.doc), "3.1.0", Options{Start: Schema, Budget: tc.limit,
				Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }})
			if tc.at == "" {
				assert.Empty(t, diags)
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, tc.at, string(diags[0].Provenance.Pointer))
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestKeyPlace_NamesNoPointerItCannotDerive holds an aliased key of an object
// placed by its line alone, as a merge source written inline is, to no pointer:
// only the root's key is the empty pointer's own, so only it is named.
func TestKeyPlace_NamesNoPointerItCannotDerive(t *testing.T) {
	doc := openapitest.YAMLDocument(t, "openapi: 3.1.0\nx-m: &m 1\ninfo: {title: t, version: v, bogus: *m}\npaths: {}\nroot: *m\n")
	diags := NewChecker().Check(t.Context(), doc, "3.1.0", Options{
		Locate: func(jsontext.Pointer) ir.Provenance { return ir.Provenance{Position: ir.Position{Line: 1}} },
	})
	var places []ir.Provenance
	for _, d := range diags {
		if d.Code == diag.UnknownObjectKey {
			places = append(places, d.Provenance)
		}
	}
	assert.Equal(t, []ir.Provenance{
		{Position: ir.Position{Line: 3, Column: 37}},
		{Pointer: "/root", Position: ir.Position{Line: 5, Column: 7}},
	}, places, "each at its alias, and a pointer only for the root's key")
}

// TestCheck_DefaultsFillTheUnsetOptions pins the options a caller may leave out.
func TestCheck_DefaultsFillTheUnsetOptions(t *testing.T) {
	c := NewChecker()
	doc := openapitest.YAMLDocument(t, "openapi: 3.1.0\ninfo: {title: t}\n")
	got := c.Check(t.Context(), doc, "3.1.0", Options{})
	require.NotEmpty(t, got, "the document is read from its root")
	for _, d := range got {
		assert.Equal(t, ir.Provenance{}, d.Provenance, "unset Locate places nothing")
	}
	var placed []jsontext.Pointer
	c.Check(t.Context(), doc, "3.1.0", Options{Locate: func(p jsontext.Pointer) ir.Provenance {
		placed = append(placed, p)
		return ir.Provenance{}
	}})
	assert.NotEmpty(t, placed)
	assert.Empty(t, c.Check(t.Context(), nil, "3.1.0", Options{}), "nothing to read")
}

// TestCheck_MergedAndAliasedMappingsAreRead pins that objects are read through
// `<<` and aliases, as the parser reads them.
func TestCheck_MergedAndAliasedMappingsAreRead(t *testing.T) {
	doc := `
openapi: 3.1.0
x-base: &base {title: t, version: v}
info:
  <<: *base
  summary: s
components:
  schemas:
    A: &a {type: object}
    B: *a
`
	assert.Empty(t, NewChecker().Check(t.Context(), openapitest.YAMLDocument(t, doc), "3.1.0", Options{}))
	merged := openapitest.YAMLDocument(t, "{<<: {version: v}, title: t, license: {}}")
	diags := NewChecker().Check(t.Context(), merged, "3.1.0", Options{Start: Info})
	require.Len(t, diags, 1, "the merged version is read, the license is not named")
	assert.Equal(t, diag.Validation+"/validation-required-field", diags[0].Code)
}

// TestCheck_IsDeterministic pins that one document draws one report.
func TestCheck_IsDeterministic(t *testing.T) {
	doc := openapitest.YAMLDocument(t, `{title: [a], version: 1, license: {identifier: i, url: u}, contact: {email: x}}`)
	first := NewChecker().Check(t.Context(), doc, "3.1.0", Options{Start: Info})
	again := NewChecker().Check(t.Context(), doc, "3.1.0", Options{Start: Info})
	assert.NotEmpty(t, first)
	assert.Empty(t, cmp.Diff(first, again))
}

// TestCiteFor_PicksTheLatestCitationTheMinorHasReached pins the citation a
// finding names.
func TestCiteFor_PicksTheLatestCitationTheMinorHasReached(t *testing.T) {
	cites := []Cite{{Minor: V30, Sentence: "a"}, {Minor: V32, Sentence: "c"}, {Minor: V31, Sentence: "b"}}
	assert.Equal(t, "a", citeFor(cites, V30).Sentence)
	assert.Equal(t, "b", citeFor(cites, V31).Sentence)
	assert.Equal(t, "c", citeFor(cites, V32).Sentence)
	assert.Equal(t, "c", citeFor([]Cite{{Minor: V32, Sentence: "c"}}, V30).Sentence, "none reached: the first")
	assert.Equal(t, Cite{}, citeFor(nil, V30))
}

// TestLessPointer_OrdersTokenByToken holds the order a finding names the
// position that read it by: token by token, each by its bytes, a prefix first.
func TestLessPointer_OrdersTokenByToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b jsontext.Pointer
		less bool
	}{
		{"/A/x", "/A-/x", true},
		{"/A-/x", "/A/x", false},
		{"/A", "/A/x", true},
		{"/A/x", "/A/x", false},
	} {
		assert.Equal(t, tc.less, lessPointer(tc.a, tc.b), "%s < %s", tc.a, tc.b)
	}
}

// TestSharedNode_IsTrueForAnAliasAndForAnAnchoredNodeAlone holds the node
// another place may read to the two ways it is shared, and an absent node to
// none.
func TestSharedNode_IsTrueForAnAliasAndForAnAnchoredNodeAlone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: &m {k: 1}\nb: *m\nc: {k: 1}\n"), &root))
	pairs := root.Content[0].Content

	assert.False(t, sharedNode(nil), "an absent node")
	assert.True(t, sharedNode(pairs[1]), "an anchored mapping")
	assert.True(t, sharedNode(pairs[3]), "an alias")
	assert.False(t, sharedNode(pairs[5]), "a mapping written once")
}

// TestOutsideRuleset_IsFalseForAnAbsentNodeAndTrueForATagOutsideIt holds a
// node under a tag YAML 1.2's JSON schema ruleset lacks to being outside it,
// and an absent node, a plain scalar and a tag the ruleset has to being inside.
func TestOutsideRuleset_IsFalseForAnAbsentNodeAndTrueForATagOutsideIt(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: !acme x\nb: !!int 3\nc: plain\nd: !acme [x]\n"), &root))
	pairs := root.Content[0].Content

	assert.False(t, outsideRuleset(nil), "an absent node")
	assert.True(t, outsideRuleset(pairs[1]), "a scalar under a local tag")
	assert.False(t, outsideRuleset(pairs[3]), "a scalar under a tag the ruleset has")
	assert.False(t, outsideRuleset(pairs[5]), "a plain scalar")
	assert.True(t, outsideRuleset(pairs[7]), "a sequence under a local tag")
}

// TestKeyPlacer_ReadsTheMappingOnce holds a placer to one scan of its mapping
// for its aliases, however many keys it places, which is what keeps n findings
// about the keys of one object at n: an alias added after the first key is
// placed is not seen. A merge key is no key the mapping writes itself.
func TestKeyPlacer_ReadsTheMappingOnce(t *testing.T) {
	doc := openapitest.YAMLDocument(t, "x-m: &m 1\n<<: {c: 2}\na: *m\n")
	located := func(p jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Pointer: p, Position: ir.Position{Line: 99}}
	}
	w := &walker{}
	w.locate = located
	places := newKeyPlacer(w, doc.Content[0], "")
	assert.Equal(t, ir.Provenance{Pointer: "/a", Position: ir.Position{Line: 3, Column: 4}}, places.place("a"))
	assert.Equal(t, ir.Provenance{Pointer: "/c", Position: ir.Position{Line: 99}}, places.place("c"))

	mapping := places.mapping
	alias := mapping.Content[len(mapping.Content)-1]
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "b"}, alias)
	assert.Equal(t, ir.Provenance{Pointer: "/b", Position: ir.Position{Line: 99}}, places.place("b"),
		"the mapping was read for its aliases once, before b was written")
}

// TestKeyPlacer_PlacesAKeyWhoseValueIsAnchoredAtItsOwn holds a key whose value
// is anchored, and read again through another key's alias, to its own pointer
// and its value's coordinate: the locator places a shared node at the least
// position reading it, here the other key, and a finding about a key is about
// the key.
func TestKeyPlacer_PlacesAKeyWhoseValueIsAnchoredAtItsOwn(t *testing.T) {
	t.Parallel()
	doc := openapitest.YAMLDocument(t, "b: &m 1\na: *m\n")
	w := &walker{}
	w.locate = func(p jsontext.Pointer) ir.Provenance {
		if p == "" {
			return ir.Provenance{}
		}
		return ir.Provenance{Pointer: "/a"}
	}
	places := newKeyPlacer(w, doc.Content[0], "")
	assert.Equal(t, ir.Provenance{Pointer: "/b", Position: ir.Position{Line: 1, Column: 4}}, places.place("b"))
}
