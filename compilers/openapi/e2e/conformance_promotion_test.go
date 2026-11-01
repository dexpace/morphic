// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test // external test package — exercises only the public API

import (
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
	assert.Len(t, diags, 4, "agreeing keys, the word deprecated and an absent key report nothing: %+v", diags)
}

// assertExtensionRedaction is x-sensitive at every position format: password
// reaches: the two carriers, the node a position owns, and the hoisted nodes of
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

	info := []ir.Severity{ir.SeverityInfo}
	assert.Equal(t, info, degradedAt(diags, "/paths/~1keys/post/parameters/5/schema/x-sensitive"), "text")
	assert.Equal(t, info, degradedAt(diags, "/components/schemas/Login/properties/misread/x-sensitive"), "a number")
	assert.Len(t, diags, 2, "false states nothing, and a key read reports nothing: %+v", diags)
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
