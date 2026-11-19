// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// carriedKeysSpec is the conformance spec carried-keys.
func carriedKeysSpec(t *testing.T) string { return corpusFile(t, "carried-keys") }

// checkCarriedParams holds a parameter's own keys apart from the entries its
// schema writes: the schema's sit under openapi:schema/, a content entry's
// schema's under openapi:content/<mt>/schema/, and a fact either object states
// (redaction) fills the parameter and marks it.
func checkCarriedParams(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	assert.Equal(t, []string{"openapi/unpromoted-extension info /tags/1/x-stability"}, findings(diags),
		"the whole diagnostic list: the one tier no group reads")
	a := operationTree(t, doc, "a")

	p := namedItem(t, valueAtPath(a, "params"), "p")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:x-a":                "FROM_PARAMETER",
		"openapi:schema/x-a":         "FROM_SCHEMA",
		"openapi:schema/x-sensitive": true,
	}, keptValues(valueAtPath(p, "unmodeled"))))
	assert.Equal(t, true, valueAtPath(p, "secret"), "the schema states the redaction")
	assert.Equal(t, "extension-promotion", valueAtPath(p, "provenance", "inferred"))

	c := namedItem(t, valueAtPath(a, "params"), "c")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:content/application~1json/x-a":         "FROM_MEDIA",
		"openapi:content/application~1json/x-sensitive": true,
		"openapi:content/application~1json/schema/x-a":  "FROM_SCHEMA",
	}, keptValues(valueAtPath(c, "unmodeled"))))
	assert.Equal(t, true, valueAtPath(c, "secret"), "the content entry states the redaction")
	assert.Equal(t, "extension-promotion", valueAtPath(c, "provenance", "inferred"))

	h := namedItem(t, valueAtPath(a, "responses", "0", "headers"), "X-H")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:x-a": "FROM_HEADER",
		"openapi:content/text~1plain/x-sensitive": true,
	}, keptValues(valueAtPath(h, "unmodeled"))))
	assert.Equal(t, true, valueAtPath(h, "secret"), "a header states redaction through its content as a parameter does")
	assert.Equal(t, "extension-promotion", valueAtPath(h, "provenance", "inferred"))
}

// checkCarriedTiers holds an operation to its own tier first and its path
// item's otherwise, marked as promoted, with the path item's key kept and no
// finding about it.
func checkCarriedTiers(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	a := operationTree(t, doc, "a")
	assert.Equal(t, "beta", valueAtPath(a, "stability"), "the path item states beta")
	assert.Equal(t, "extension-promotion", valueAtPath(a, "provenance", "inferred"))
	assert.Contains(t, keptValues(valueAtPath(a, "unmodeled")), "openapi:pathItem/x-stability")

	own := operationTree(t, doc, "own")
	assert.Equal(t, "stable", valueAtPath(own, "stability"), "the operation's own key is the nearer statement")
	for _, f := range findings(diags) {
		assert.NotContains(t, f, "/paths/~1a/x-stability", "a carried key draws no finding")
	}
}

// checkCarriedTags holds the tag's keys to the tag, unscoped, and its tier to
// the group the tag forms, which also keeps the tag's extensions and records
// the promotion. A tag forming no group keeps its key on the tag and says so
// in an info.
func checkCarriedTags(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	d := tree(t, doc)
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:x-note":             "n",
		"openapi:x-stability":        "beta",
		"openapi:externalDocs/x-doc": float64(1),
	}, keptValues(valueAtPath(d, "tagDefs", "0", "unmodeled"))))
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:x-stability": "alpha"},
		keptValues(valueAtPath(d, "tagDefs", "1", "unmodeled"))), "the tag forming no group keeps its tier")
	for key := range keptValues(valueAtPath(d, "unmodeled")) {
		assert.False(t, strings.HasPrefix(key, "openapi:tags/"), "the document keeps no tag's key: %s", key)
	}
	assert.Equal(t, []string{"openapi/unpromoted-extension info /tags/1/x-stability"}, findings(diags))

	g, ok := groupTree(t, doc, "operation-group/tags/T")
	require.True(t, ok, "tag T forms a group")
	assert.Equal(t, "beta", valueAtPath(g, "stability"))
	assert.Equal(t, "extension-promotion", valueAtPath(g, "inferred"))
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:x-note": "n", "openapi:x-stability": "beta"},
		keptValues(valueAtPath(g, "unmodeled"))))
	_, ok = groupTree(t, doc, "operation-group/tags/Lone")
	assert.False(t, ok, "no operation names Lone, so it forms no group")
}

// assertCarriedKeys is the corpus row for what one object keeps of another's
// keys: a parameter's schema's, a tag's, a path item's.
func assertCarriedKeys(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	checkCarriedParams(t, doc, diags)
	checkCarriedTiers(t, doc, diags)
	checkCarriedTags(t, doc, diags)
}

// TestCarriedKeys_AParameterScopesItsSchemasEntries compiles carried-keys
// verified and holds its parameters and header to checkCarriedParams.
func TestCarriedKeys_AParameterScopesItsSchemasEntries(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "carried-keys.yaml", carriedKeysSpec(t))
	checkCarriedParams(t, doc, diags)
}

// TestCarriedKeys_AnOperationReadsItsPathItemsTier compiles carried-keys
// verified and holds its operations to checkCarriedTiers.
func TestCarriedKeys_AnOperationReadsItsPathItemsTier(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "carried-keys.yaml", carriedKeysSpec(t))
	checkCarriedTiers(t, doc, diags)
}

// TestCarriedKeys_ATagKeepsItsKeysAndItsGroupTakesTheTier compiles
// carried-keys verified and holds its tags and groups to checkCarriedTags.
func TestCarriedKeys_ATagKeepsItsKeysAndItsGroupTakesTheTier(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "carried-keys.yaml", carriedKeysSpec(t))
	checkCarriedTags(t, doc, diags)
}

// TestCarriedKeys_DeclarationOrderChangesNothing compiles the spec with every
// mapping's entries reversed (the tags list keeps its order) and requires the
// same document and the same findings: a parameter's own keys and its schema's
// are read apart, so neither order wins. It also requires the entries this
// change keys to be present, so two documents that both lost them do not pass.
func TestCarriedKeys_DeclarationOrderChangesNothing(t *testing.T) {
	t.Parallel()
	src := carriedKeysSpec(t)
	a, aDiags := compileNamedVerified(t, "carried-keys.yaml", src)
	b, bDiags := compileNamedVerified(t, "carried-keys.yaml", string(reversed(t, src)))
	assert.Equal(t, findings(aDiags), findings(bDiags))
	assert.Empty(t, cmp.Diff(withoutVersion(t, a), withoutVersion(t, b)))
	assert.Contains(t, withoutVersion(t, b), "openapi:schema/x-a")
}

// TestParameter_SchemaAndObjectSpellOneKeyEndToEnd compiles the spec writing
// one key on a parameter and on its schema, verified: each is kept, and the
// collision finding is gone.
func TestParameter_SchemaAndObjectSpellOneKeyEndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "p.yaml", testdataSpec(t, "param_schema_keys_apart"))
	assert.Equal(t, []string{
		"openapi/degraded-construct info /paths/~1a/get/parameters/1/schema",
		"openapi/unknown-object-key warning /paths/~1a/get/parameters/0/divisibleBy",
		"openapi/unknown-object-key warning /paths/~1a/get/parameters/1/format",
		"openapi/unknown-object-key warning /paths/~1a/get/responses/200/headers/X-H/divisibleBy",
		"openapi/unknown-schema-keyword info /paths/~1a/get/parameters/0/schema/divisibleBy",
		"openapi/unknown-schema-keyword info /paths/~1a/get/responses/200/headers/X-H/schema/divisibleBy",
	}, findings(diags))
	p := namedItem(t, valueAtPath(operationTree(t, doc, "a"), "params"), "p")
	assert.Contains(t, keptValues(valueAtPath(p, "unmodeled")), "openapi:schema/divisibleBy")
}

// TestParameter_XMLAndBranchKeptOnceEndToEnd compiles the two specs in which a
// parameter's schema keeps a declaration whole beside a key read from inside
// it, and requires the declaration alone.
func TestParameter_XMLAndBranchKeptOnceEndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "x.yaml", testdataSpec(t, "param_xml_kept_once"))
	q := namedItem(t, valueAtPath(tree(t, operationAt(t, doc, "GET", "/p")), "params"), "q")
	assert.Equal(t, []string{"openapi:schema/xml"}, sortedKeys(keptValues(valueAtPath(q, "unmodeled"))))
	assert.Equal(t, []string{"openapi/degraded-construct info /paths/~1p/get/parameters/0/schema/xml"}, findings(diags))

	doc, diags = compileNamedVerified(t, "x.yaml", testdataSpec(t, "param_branch_kept_once"))
	g := operationTree(t, doc, "g")
	id := namedItem(t, valueAtPath(g, "params"), "schema_id")
	assert.Equal(t, []string{"openapi:schema/format", "openapi:schema/readOnly"}, sortedKeys(keptValues(valueAtPath(id, "unmodeled"))),
		"each keyword the entry keeps is kept once, under its own key")
	assert.Equal(t, []string{
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1",
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1/readOnly",
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/1/schema/xml",
	}, findings(diags))
}

// TestParameter_RedactionIsSingleSidedEndToEnd compiles the spec stating
// redaction on a schema and withdrawing it on the parameter: the schema's
// statement stands, as it does when it sits behind a $ref.
func TestParameter_RedactionIsSingleSidedEndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "x.yaml", testdataSpec(t, "param_redaction_single_sided"))
	assert.Empty(t, diags)
	a := operationTree(t, doc, "a")
	for _, name := range []string{"inl", "ref"} {
		p := namedItem(t, valueAtPath(a, "params"), name)
		assert.Equal(t, true, valueAtPath(p, "secret"), name)
		assert.Equal(t, "extension-promotion", valueAtPath(p, "provenance", "inferred"), name)
	}
}

// TestTags_KeysOnTheTagAndTierOnItsGroupEndToEnd compiles a one-tag document:
// the tag holds both keys, the group the tier and its marker, and no operation
// takes a tier it did not state.
func TestTags_KeysOnTheTagAndTierOnItsGroupEndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "x.yaml", testdataSpec(t, "tag_keys_and_group_tier"))
	assert.Empty(t, diags)
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:x-note": "n", "openapi:x-stability": "beta"},
		keptValues(valueAtPath(tree(t, doc), "tagDefs", "0", "unmodeled"))))
	g, ok := groupTree(t, doc, "operation-group/tags/Billing")
	require.True(t, ok)
	assert.Equal(t, "beta", valueAtPath(g, "stability"))
	assert.Equal(t, "extension-promotion", valueAtPath(g, "inferred"))
	assert.Nil(t, valueAtPath(operationTree(t, doc, "a"), "stability"), "the tier is read through the group alone")
}

// TestTags_ARepeatedNameKeepsEachDeclaration compiles two tags of one name: the
// load's duplicate-key error stands and the document is still built, each tag
// keeping its own keys, and the group taking the first declaration's.
func TestTags_ARepeatedNameKeepsEachDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "x.yaml", testdataSpec(t, "tag_repeated_name"))
	assert.Equal(t, []string{"openapi/validation/validation-duplicate-key error /tags/1/name"}, findings(diags))
	d := tree(t, doc)
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:x-first": float64(1), "openapi:x-stability": "beta"},
		keptValues(valueAtPath(d, "tagDefs", "0", "unmodeled"))))
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:x-second": float64(2), "openapi:x-stability": "alpha"},
		keptValues(valueAtPath(d, "tagDefs", "1", "unmodeled"))))
	g, ok := groupTree(t, doc, "operation-group/tags/T")
	require.True(t, ok)
	assert.Equal(t, "beta", valueAtPath(g, "stability"), "the first declaration of a name wins")
	assert.Equal(t, []string{"openapi:x-first", "openapi:x-stability"}, sortedKeys(keptValues(valueAtPath(g, "unmodeled"))))
}

// sortedKeys returns the keys of m in order.
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
