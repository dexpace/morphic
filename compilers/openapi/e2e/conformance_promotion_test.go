// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test // external test package — exercises only the public API

import (
	"encoding/json/v2"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// degradedAt is where an info openapi/degraded-construct lands, in one list.
func degradedAt(diags []ir.Diagnostic, pointer string) []ir.Severity {
	return diagsAt(diags, "openapi/degraded-construct", pointer)
}

// assertExtensionStability is the matrix's stability row: each operation of the
// spec states one word, and the table holds what reaches the typed field, what
// the word deprecates, and that the entries survive. Every row whose operation
// writes a key names its marker, so a field written without one fails on it.
func assertExtensionStability(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const marker ir.Inferred = "extension-promotion"
	for _, tc := range []struct {
		op         string
		tier       ir.Stability
		deprecated bool
		inferred   ir.Inferred
		kept       []string
	}{
		{"alphaWord", ir.StabilityAlpha, false, marker, []string{"openapi:x-fern-availability"}},
		{"betaWord", ir.StabilityBeta, false, marker, []string{"openapi:x-stability"}},
		{"previewWord", ir.StabilityBeta, false, marker, []string{"openapi:x-fern-availability"}},
		{"stableWord", ir.StabilityStable, false, marker, []string{"openapi:x-stability"}},
		{"gaWord", ir.StabilityStable, false, marker, []string{"openapi:x-fern-availability"}},
		{"sourcesAgree", ir.StabilityStable, false, marker, []string{"openapi:x-stability", "openapi:x-fern-availability"}},
		{"sourcesDisagree", ir.StabilityBeta, false, marker, []string{"openapi:x-stability", "openapi:x-fern-availability"}},
		{"deprecatedWord", ir.StabilityUnstated, true, marker, []string{"openapi:x-fern-availability"}},
		{"deprecatedAndBeta", ir.StabilityBeta, true, marker, []string{"openapi:x-stability", "openapi:x-fern-availability"}},
		{"stabilityKeyDeprecated", ir.StabilityUnstated, true, marker, []string{"openapi:x-stability"}},
		{"keywordFalse", ir.StabilityUnstated, true, marker, []string{"openapi:x-fern-availability"}},
		{"legacyWord", ir.StabilityUnstated, false, "", []string{"openapi:x-fern-availability"}},
		{"unknownWord", ir.StabilityUnstated, false, "", []string{"openapi:x-stability"}},
		{"notText", ir.StabilityUnstated, false, "", []string{"openapi:x-stability"}},
		{"notAWord", ir.StabilityUnstated, false, "", []string{"openapi:x-stability"}},
		{"unstated", ir.StabilityUnstated, false, "", nil},
		{"underPathItemKey", ir.StabilityUnstated, false, "", []string{"openapi:pathItem/x-stability"}},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, tc.op)
		assert.Equal(t, tc.tier, op.Stability, "%s tier", tc.op)
		assert.Equal(t, tc.deprecated, op.Deprecation != nil, "%s deprecated", tc.op)
		assert.Equal(t, tc.inferred, op.Provenance.Inferred, "%s marker", tc.op)
		for _, key := range tc.kept {
			entry, kept := op.Unmodeled[key]
			require.True(t, kept, "%s keeps %s", tc.op, key)
			assert.Equal(t, ir.ReasonVendorExtension, entry.Reason, "%s: %s stays what it was", tc.op, key)
		}
	}

	info := []ir.Severity{ir.SeverityInfo}
	assert.Equal(t, info, degradedAt(diags, "/paths/~1sources/put/x-fern-availability"),
		"the key a disagreement lost")
	assert.Equal(t, info, degradedAt(diags, "/paths/~1unread/get/x-fern-availability"), "legacy names no tier")
	assert.Equal(t, info, degradedAt(diags, "/paths/~1unread/put/x-stability"), "nor does an unknown word")
	assert.Equal(t, info, degradedAt(diags, "/paths/~1unread/post/x-stability"), "and text is what a tier is")
	assert.Equal(t, info, degradedAt(diags, "/paths/~1unread/patch/x-stability"), "a true is no tier, nor deprecates")
	assert.Len(t, diags, 5, "agreeing keys, the word deprecated and an absent key report nothing: %+v", diags)
}

// assertExtensionRedaction is x-sensitive at every position format: password
// reaches: the three carriers, the node a position owns, and the hoisted nodes of
// the positions with no carrier. secret-format holds the same positions for the
// format, so the two are one rule at two spellings.
func assertExtensionRedaction(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const marker ir.Inferred = "extension-promotion"
	op, ok := opByName(doc, "keys")
	require.True(t, ok)

	for _, tc := range []struct {
		name   string
		secret bool
		marked bool
	}{
		{"onSchema", true, true},
		{"onObject", true, true},
		{"beside", true, true},
		{"ownsNode", false, false},
		{"stated", false, false},
		{"misread", false, false},
		{"plain", false, false},
	} {
		p, ok := openapitest.ParamBySource(op, tc.name)
		require.True(t, ok, tc.name)
		assert.Equal(t, tc.secret, p.Secret, "parameter %s", tc.name)
		assert.Equal(t, tc.marked, p.Provenance.Inferred == marker, "parameter %s marker", tc.name)
	}
	owns, ok := openapitest.ParamBySource(op, "ownsNode")
	require.True(t, ok)
	requireSensitive(t, doc, owns.Type.Target, "a parameter whose schema owns a node")

	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 2)
	for _, h := range op.Responses[0].Headers {
		assert.True(t, h.Secret, h.WireName)
		assert.Equal(t, marker, h.Provenance.Inferred, h.WireName)
	}

	login := secretModel(t, doc, "Login")
	for _, tc := range []struct {
		wire   string
		secret bool
		kept   bool
	}{
		{"inline", true, true},
		{"beside", true, true},
		{"ownsNode", false, false}, // its node holds the entry
		{"stated", false, true},
		{"misread", false, true},
		{"plain", false, false},
	} {
		p, ok := propByWire(login, tc.wire)
		require.True(t, ok, tc.wire)
		assert.Equal(t, tc.secret, p.Secret, "property %s", tc.wire)
		assert.Equal(t, tc.secret, p.Provenance.Inferred == marker, "property %s marker", tc.wire)
		_, kept := p.Unmodeled["openapi:x-sensitive"]
		assert.Equal(t, tc.kept, kept, "property %s entry", tc.wire)
	}
	ownsProp, ok := propByWire(login, "ownsNode")
	require.True(t, ok)
	requireSensitive(t, doc, ownsProp.Type.Target, "a property whose schema owns a node")

	for _, tc := range []struct {
		id       ir.TypeID
		position string
	}{
		{componentID("Tok"), "a component"},
		{componentID("Credential"), "a model"},
		{openapitest.BodyTarget(t, op.Request), "request content schema"},
		{mapOf(t, doc, "Bag").Value.Target, "additionalProperties"},
		{listOf(t, doc, "Tokens").Elem.Target, "items"},
		{unionOf(t, doc, "Either").Variants[0].Type.Target, "a union branch"},
	} {
		node := requireSensitive(t, doc, tc.id, tc.position)
		assert.Equal(t, marker, node.Common().Provenance.Inferred, tc.position)
		assert.Contains(t, node.Common().Unmodeled, "openapi:x-sensitive", tc.position)
	}
	assert.False(t, doc.Types[componentID("Plain")].Common().Sensitive, "a referent beside which a use wrote it")
	assert.Greater(t, assertEveryRedactionLanded(t, doc), 10, "the spec states it at every position above")

	info := []ir.Severity{ir.SeverityInfo}
	assert.Equal(t, info, degradedAt(diags, "/paths/~1keys/post/parameters/5/schema/x-sensitive"), "text")
	assert.Equal(t, info, degradedAt(diags, "/components/schemas/Login/properties/misread/x-sensitive"), "a number")
	assert.Len(t, diags, 2, "false states nothing, and a key read reports nothing: %+v", diags)
}

// assertEveryRedactionLanded is the join of the entries and the fields, with no
// list of positions to go stale: every node of the encoded document keeping a
// true x-sensitive entry has to say so in a field, so a carrier a promotion does
// not reach fails here. It returns how many it met.
func assertEveryRedactionLanded(t *testing.T, doc *ir.Document) int {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var tree any
	require.NoError(t, json.Unmarshal(raw, &tree))
	met := 0
	pending := []any{tree}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		switch n := node.(type) {
		case []any:
			pending = append(pending, n...)
		case map[string]any:
			pending = slices.AppendSeq(pending, maps.Values(n))
			entry, kept := redactionEntry(n)
			if kept && entry["value"] == true {
				met++
				assert.True(t, n["secret"] == true || n["sensitive"] == true, "%v", n["id"])
			}
		}
	}
	return met
}

// redactionEntry returns the x-sensitive entry an encoded node keeps.
func redactionEntry(node map[string]any) (map[string]any, bool) {
	unmodeled, ok := node["unmodeled"].(map[string]any)
	if !ok {
		return nil, false
	}
	entry, ok := unmodeled["openapi:x-sensitive"].(map[string]any)
	return entry, ok
}

// mapOf returns the component name as a MapT.
func mapOf(t *testing.T, doc *ir.Document, name string) *ir.MapT {
	t.Helper()
	m, ok := doc.Types[componentID(name)].(*ir.MapT)
	require.True(t, ok, "%s is a map", name)
	return m
}

// listOf returns the component name as a List.
func listOf(t *testing.T, doc *ir.Document, name string) *ir.List {
	t.Helper()
	l, ok := doc.Types[componentID(name)].(*ir.List)
	require.True(t, ok, "%s is a list", name)
	return l
}

// unionOf returns the component name as a Union.
func unionOf(t *testing.T, doc *ir.Document, name string) *ir.Union {
	t.Helper()
	u, ok := doc.Types[componentID(name)].(*ir.Union)
	require.True(t, ok, "%s is a union", name)
	return u
}

// wireNames spells segments by the wire names of the properties they name,
// read off every model of doc, so a page value reads as the document writes
// it.
func wireNames(doc *ir.Document, segments []ir.PropID) []string {
	names := map[ir.PropID]string{}
	for _, td := range doc.Types {
		if m, ok := td.(*ir.Model); ok {
			for _, p := range m.Properties {
				names[p.ID] = p.EffectiveWireName()
			}
		}
	}
	out := make([]string, len(segments))
	for i, s := range segments {
		out[i] = names[s]
	}
	return out
}

// assertExtensionPagination is a page walk the default mapping reads in each
// convention it maps: one operation's object of expressions, another's keys
// marking a parameter, a member and the type a member names, and a third's
// next link. Each is marked inferred, as is each node whose key it read; an
// offset the default maps no source for leaves a walk with no next page,
// reported and unfilled.
func assertExtensionPagination(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const marker ir.Inferred = "extension-promotion"
	param := func(op ir.Operation, in *ir.InputPath) string {
		require.NotNil(t, in)
		require.NotNil(t, in.Param)
		for _, p := range op.Params {
			if p.ID == *in.Param {
				return p.Name.Source
			}
		}
		return ""
	}
	byExpression, ok := opByName(doc, "byExpression")
	require.True(t, ok)
	page := byExpression.Pagination
	require.NotNil(t, page)
	assert.Equal(t, ir.PageStrategyCursor, page.Strategy)
	assert.True(t, page.Inferred)
	assert.Equal(t, "page_token", param(byExpression, page.InputCursor))
	assert.Equal(t, []string{"result"}, wireNames(doc, page.Items.Segments))
	assert.Equal(t, []string{"result_info", "next_page_token"}, wireNames(doc, page.NextCursor.Segments))
	assert.Equal(t, marker, byExpression.Provenance.Inferred)

	byMarks, ok := opByName(doc, "byMarks")
	require.True(t, ok)
	page = byMarks.Pagination
	require.NotNil(t, page)
	assert.Equal(t, ir.PageStrategyCursor, page.Strategy)
	assert.Equal(t, "cursor", param(byMarks, page.InputCursor))
	assert.Equal(t, []string{"result"}, wireNames(doc, page.Items.Segments))
	assert.Equal(t, []string{"result_info", "cursor"}, wireNames(doc, page.NextCursor.Segments))
	assert.Equal(t, marker, byMarks.Params[0].Provenance.Inferred, "the parameter whose key marks the cursor")
	assert.Equal(t, marker, doc.Types[componentID("Cursor")].Common().Provenance.Inferred,
		"the type whose key marks the next cursor")
	assert.Empty(t, byMarks.Provenance.Inferred, "the operation keeps no key")

	byNextLink, ok := opByName(doc, "byNextLink")
	require.True(t, ok)
	require.NotNil(t, byNextLink.Pagination)
	assert.Equal(t, ir.PageStrategyNextLink, byNextLink.Pagination.Strategy)
	assert.Equal(t, []string{"next"}, wireNames(doc, byNextLink.Pagination.NextLink.Segments))
	assert.Equal(t, []string{"data"}, wireNames(doc, byNextLink.Pagination.Items.Segments))

	noNextPage, ok := opByName(doc, "noNextPage")
	require.True(t, ok)
	assert.Nil(t, noNextPage.Pagination)
	assert.Empty(t, noNextPage.Provenance.Inferred)
	require.Len(t, diags, 1, "the one walk with no next page: %+v", diags)
	assert.Equal(t, `operation "op/openapi/paths/~1offsets/get": its extensions state a pagination that names `+
		`neither an input continuing it nor a next link, so it fills none`, diags[0].Message)
}

// assertExtensionServerName is a server named by the default key in each list
// a document writes servers in, and one the key does not name keeping the hint
// its URL gives.
func assertExtensionServerName(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const marker ir.Inferred = "extension-promotion"
	assert.Empty(t, diags)
	for id, want := range map[ir.ServerID]string{
		"server/openapi/servers/0":                    "production",
		"server/openapi/paths/~1stream/servers/0":     "stream",
		"server/openapi/paths/~1stream/put/servers/0": "ingest",
	} {
		s, ok := doc.Servers[id]
		require.True(t, ok, id)
		assert.Equal(t, ir.NamingFor(want), s.Name, id)
		assert.Equal(t, marker, s.Provenance.Inferred, id)
	}
	unnamed, ok := doc.Servers["server/openapi/servers/1"]
	require.True(t, ok)
	assert.Equal(t, "https_staging_example_com_v_1", unnamed.Name.Hint)
	assert.Empty(t, unnamed.Provenance.Inferred)
}
