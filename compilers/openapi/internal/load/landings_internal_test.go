// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestBuildObjects_ModelsFieldsBesideAForeignPathItemRef pins the fields beside
// a path item $ref another document writes, which no walk of the source meets:
// the item the source's $ref lands on, and one a callback inside it names, are
// each modelled where that document writes them, though the source itself
// writes nothing beside a $ref.
func TestBuildObjects_ModelsFieldsBesideAForeignPathItemRef(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `items:
  I:
    $ref: '#/items/J'
    put:
      responses: {"200": {description: ok}}
      callbacks:
        cb:
          '{$request.query.u}':
            $ref: '#/items/J'
            post: {responses: {"200": {description: ok}}}
  J:
    get: {responses: {"200": {description: ok}}}
`})
	doc, diags := loadExternal(t, dir, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: 'other.yaml#/items/I'}
`, Options{})
	assert.Empty(t, diags)

	a, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	r, ok := doc.Bindings.Reference(a)
	require.True(t, ok)
	landing, ok := doc.Bindings.ObjectAt(r.Hop.Target, bind.RefPathItem).(*soa.ReferencedPathItem)
	require.True(t, ok)
	sib, ok := doc.Siblings.Of(landing)
	require.True(t, ok, "the landing's fields are modelled")
	assert.Equal(t, jsontext.Pointer("/items/I"), sib.Pointer)
	require.NotNil(t, sib.Item.Put())

	cb, ok := sib.Item.Put().GetCallbacks().Get("cb")
	require.True(t, ok)
	inner, ok := cb.GetObject().Get("{$request.query.u}")
	require.True(t, ok)
	nested, ok := doc.Siblings.Of(inner)
	require.True(t, ok, "and those beside a $ref inside them")
	assert.Equal(t, jsontext.Pointer("/items/I/put/callbacks/cb/{$request.query.u}"), nested.Pointer)
	assert.NotNil(t, nested.Item.Post())
}

// TestBuildObjects_AFaultBesideALandingsRefIsReportedOnce pins a landing whose
// fields beside its $ref the parser faults on: no model is kept, and the fault
// is reported once, at the $ref reaching the landing, though two reach it.
func TestBuildObjects_AFaultBesideALandingsRefIsReportedOnce(t *testing.T) {
	t.Parallel()
	doc, diags := loadTargets(t, "landing.yaml", `openapi: 3.1.0
info: {title: T, version: "1"}
x-shared:
  item:
    $ref: '#/paths/~1b'
    parameters: [{$ref}]
paths:
  /a: {$ref: '#/x-shared/item'}
  /c: {$ref: '#/x-shared/item'}
  /b:
    get: {responses: {"200": {description: ok}}}
`, false)
	var faults []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.Validation {
			faults = append(faults, d)
		}
	}
	require.Len(t, faults, 1, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), faults[0].Provenance.Pointer)
	assert.Contains(t, faults[0].Message, "the fields written beside its $ref could not be read: ")

	a, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	r, ok := doc.Bindings.Reference(a)
	require.True(t, ok)
	landing, ok := doc.Bindings.ObjectAt(r.Hop.Target, bind.RefPathItem).(*soa.ReferencedPathItem)
	require.True(t, ok)
	_, ok = doc.Siblings.Of(landing)
	assert.False(t, ok, "a faulted landing holds no model")
}
