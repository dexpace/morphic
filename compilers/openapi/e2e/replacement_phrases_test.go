// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// assertReplacementPhrases pins the replacement policy on the messages of
// replacement-inference that name a declared entity for another reason than
// to say what replaces it: one comparing the entity, one saying it was
// replaced, one naming a former name, each filling nothing, and one telling
// the caller what to use and what not to, filling the first alone. None is
// reported. The policy switched off leaves every message and fills nothing.
func assertReplacementPhrases(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	for _, name := range []string{"listZonesOld", "createZoneOld"} {
		op, ok := opByName(doc, name)
		require.True(t, ok, name)
		require.NotNil(t, op.Deprecation, name)
		assert.Empty(t, op.Deprecation.ReplacedBy, "%s: its message names a route without saying it replaces it", name)
		assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred, "%s: marked for its message alone", name)
	}

	things, ok := opByName(doc, "listThings")
	require.True(t, ok)
	members := map[string]ir.Property{}
	for _, c := range things.Responses[0].Payload.Contents {
		for _, p := range propsOfBody(t, doc, c) {
			members[p.EffectiveWireName()] = p
		}
	}
	require.Len(t, members, 4)
	a := members["a"]
	require.NotNil(t, a.Deprecation)
	assert.Empty(t, a.Deprecation.ReplacedBy, "a is formerly b, which replaces nothing")
	c := members["c"]
	require.NotNil(t, c.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Property: members["d"].ID}}, c.Deprecation.ReplacedBy,
		"c is replaced by d alone, and the b it says not to use is no replacement")
	assert.Contains(t, markersOf(c.Provenance.Inferred), "replacement-inference")
	assert.NotContains(t, markersOf(a.Provenance.Inferred), "replacement-inference")
	assert.Len(t, diags, 1, "only the member the document declares beside the one it names is reported: %+v", diags)

	off, _ := corpusWith(t, "replacement-inference",
		openapi.Options{ReplacementInference: openapi.ReplacementInference{Disabled: true}})
	for _, name := range []string{"listZonesV1", "listZonesOld", "listThings"} {
		op, ok := opByName(off, name)
		require.True(t, ok, name)
		if op.Deprecation != nil {
			assert.Empty(t, op.Deprecation.ReplacedBy, name)
			assert.NotEmpty(t, op.Deprecation.Message, "%s keeps the message", name)
		}
		assert.NotContains(t, markersOf(op.Provenance.Inferred), "replacement-inference", name)
	}
}

// propsOfBody returns the members of the model c's body is.
func propsOfBody(t *testing.T, doc *ir.Document, c ir.Content) []ir.Property {
	t.Helper()
	m, ok := doc.Types[c.Type.Target].(*ir.Model)
	require.True(t, ok, "the body is a model")
	return m.Properties
}
