// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestWritten_ReadsWhatADeclarationWritesItself pins what a fold learns of a
// declaration from the walk (modelFields.Written): each detail a $ref's
// referent may lend that the declaration writes by its own keyword or key or
// an allOf entry read as written beside it, and none its referent lends. A
// place in another document, or holding no schema object, cannot be read.
func TestWritten_ReadsWhatADeclarationWritesItself(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Lender:
      type: string
      title: Lent
      description: Lent.
      externalDocs: {url: 'https://lent.example'}
      deprecated: true
      default: lent
    M:
      type: object
      properties:
        bare: {$ref: '#/components/schemas/Lender'}
        beside: {$ref: '#/components/schemas/Lender', description: Beside., x-deprecated-reason: beside}
        folded: {allOf: [{$ref: '#/components/schemas/Lender'}, {title: Folded, default: folded}]}
        own: {type: string, title: Own, externalDocs: {url: 'https://own.example'}, deprecated: true}
        refused: false
`))
	lowerer.Diags.AppendAll(LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
	doc := lowerer.Document(t)
	model, ok := doc.Types[lowerer.Ctx.IDs().ForPointer("/components/schemas/M")].(*ir.Model)
	require.True(t, ok)
	fields := modelFields{c: lowerer.Ctx}
	byWire := openapitest.PropertiesByWire(model.Properties)

	for wire, want := range map[string]merge.Details{
		"bare":   0,
		"beside": merge.DetailDescription | merge.DetailDeprecation,
		"folded": merge.DetailTitle | merge.DetailDefault,
		"own":    merge.DetailTitle | merge.DetailExternalDocs | merge.DetailDeprecation,
	} {
		got, known := fields.Written(byWire[wire])
		assert.True(t, known, wire)
		assert.Equal(t, want, got, wire)
	}

	_, known := fields.Written(byWire["refused"])
	assert.False(t, known, "a boolean schema holds no schema object")
	elsewhere := byWire["bare"]
	elsewhere.Provenance.Source = 1
	_, known = fields.Written(elsewhere)
	assert.False(t, known, "a declaration in another document")
	nowhere := byWire["bare"]
	nowhere.Provenance.Pointer = jsontext.Pointer("/components/schemas/M/properties/absent")
	_, known = fields.Written(nowhere)
	assert.False(t, known, "a pointer naming no schema")
}
