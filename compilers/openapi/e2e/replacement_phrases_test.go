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
		operation, ok := operationByName(doc, name)
		require.True(t, ok, name)
		require.NotNil(t, operation.Deprecation, name)
		assert.Empty(t, operation.Deprecation.ReplacedBy, "%s: its message names a route without saying it replaces it", name)
		assert.Equal(t, ir.Inferred("extension-promotion"), operation.Provenance.Inferred, "%s: marked for its message alone", name)
	}

	things, ok := operationByName(doc, "listThings")
	require.True(t, ok)
	members := map[string]ir.Property{}
	for _, content := range things.Responses[0].Payload.Contents {
		for _, property := range propertiesOfBody(t, doc, content) {
			members[property.EffectiveWireName()] = property
		}
	}
	require.Len(t, members, 4)
	a := members["a"]
	require.NotNil(t, a.Deprecation)
	assert.Empty(t, a.Deprecation.ReplacedBy, "a is formerly b, which replaces nothing")
	irProperty := members["c"]
	require.NotNil(t, irProperty.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Property: members["d"].ID}}, irProperty.Deprecation.ReplacedBy,
		"c is replaced by d alone, and the b it says not to use is no replacement")
	assert.Contains(t, markersOf(irProperty.Provenance.Inferred), "replacement-inference")
	assert.NotContains(t, markersOf(a.Provenance.Inferred), "replacement-inference")
	assert.Len(t, diags, 1, "only the member the document declares beside the one it names is reported: %+v", diags)

	off, _ := corpusWith(t, "replacement-inference",
		openapi.Options{ReplacementInference: openapi.ReplacementInference{Disabled: true}})
	for _, name := range []string{"listZonesV1", "listZonesOld", "listThings"} {
		operation, ok := operationByName(off, name)
		require.True(t, ok, name)
		if operation.Deprecation != nil {
			assert.Empty(t, operation.Deprecation.ReplacedBy, name)
			assert.NotEmpty(t, operation.Deprecation.Message, "%s keeps the message", name)
		}
		assert.NotContains(t, markersOf(operation.Provenance.Inferred), "replacement-inference", name)
	}
}

// propertiesOfBody returns the members of the model content's body is.
func propertiesOfBody(t *testing.T, doc *ir.Document, content ir.Content) []ir.Property {
	t.Helper()
	model, ok := doc.Types[content.Type.Target].(*ir.Model)
	require.True(t, ok, "the body is a model")
	return model.Properties
}
