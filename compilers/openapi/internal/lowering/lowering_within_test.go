// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/engine/reader"
)

// TestRefScope_ReadsWhereTheBindingsPlaceASchema pins the scope a context's
// own gives for reading what a schema the bindings know holds
// (resolve.Scope.InDocumentOf): ext.yaml's for one a $ref reaches past /ext's
// $ref into ext.yaml, and the source's own content for one the source holds,
// whatever scope it is taken from. A schema the bindings do not know is read
// as the source's. The scope it was taken from is left as it was.
func TestRefScope_ReadsWhereTheBindingsPlaceASchema(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte("paths:\n  /x: {get: {responses: {\"200\": "+
		"{description: ok, content: {application/json: {schema: {type: string}}}}}}}\n"), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /ext: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Past: {$ref: '#/paths/~1ext/get/responses/200/content/application~1json/schema'}
    Home: {$ref: '#/components/schemas/Own'}
    Own: {type: integer}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	c := service.New(0, doc, service.Policy{})
	declared := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		t.Helper()
		js, ok := doc.Doc.Components.Schemas.Get(name)
		require.True(t, ok)
		decl := c.Bindings().Declared(js)
		require.NotNil(t, decl, "the $ref of %s lands on a schema", name)
		return decl
	}

	past := c.RefScope().InDocumentOf(declared("Past"))
	assert.True(t, past.Foreign, "past /ext's $ref, the schema is ext.yaml's content")
	assert.Equal(t, filepath.Join(dir, "ext.yaml"), past.Holder)
	assert.False(t, c.RefScope().Foreign, "the scope it was taken from is left as it was")

	home := past.InDocumentOf(declared("Home"))
	assert.False(t, home.Foreign, "what the source holds is read as the source's")
	assert.Empty(t, home.Holder)
	assert.True(t, past.Foreign, "the scope it was taken from is left as it was")

	unknown := past.InDocumentOf(&oas3.JSONSchema[oas3.Referenceable]{})
	assert.False(t, unknown.Foreign, "a schema the bindings do not know is read as the source's")
}
