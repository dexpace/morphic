// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// requirementSpec writes a requirement naming an entry of the source by name,
// one naming a scheme under an extension by URI, and one written as no mapping.
const requirementSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{named: [], "#/x/s": []}, 5]
paths: {}
x:
  s: {type: http, scheme: basic}
components:
  securitySchemes:
    named: {type: http, scheme: bearer}
`

// TestBind_ARequirementKeyIsAReferenceWhereItsDocumentReadsOneAsAURI pins the
// keys the table reads as references: each that names no securitySchemes entry
// of the source, written where the key is and read against its document, only
// where that document reads a key as a URI.
func TestBind_ARequirementKeyIsAReferenceWhereItsDocumentReadsOneAsAURI(t *testing.T) {
	t.Parallel()
	byURI := func(string) bool { return true }
	table := bind.Bind(t.Context(), openapitest.YAMLDocument(t, requirementSpec),
		bind.Config{Self: "spec.yaml", SecuritySchemes: []string{"named"}, KeysByURI: byURI})
	r, ok := table.Reference(bind.Position{Pointer: "/security/0/#~1x~1s"}, bind.RefSecurityScheme)
	require.True(t, ok, "a key naming no entry is a reference")
	assert.Equal(t, "#/x/s", r.Hop.Ref)
	assert.Equal(t, bind.Resolved, r.Chain.Status)
	assert.Equal(t, bind.Position{Pointer: "/x/s"}, r.Chain.End)
	_, ok = table.Reference(bind.Position{Pointer: "/security/0/named"}, bind.RefSecurityScheme)
	assert.False(t, ok, "a key naming an entry is that entry's name")

	for name, cfg := range map[string]bind.Config{
		"no reading":         {Self: "spec.yaml", SecuritySchemes: []string{"named"}},
		"a reading of names": {Self: "spec.yaml", KeysByURI: func(string) bool { return false }},
	} {
		table := bind.Bind(t.Context(), openapitest.YAMLDocument(t, requirementSpec), cfg)
		_, ok := table.Reference(bind.Position{Pointer: "/security/0/#~1x~1s"}, bind.RefSecurityScheme)
		assert.False(t, ok, name)
	}
}
