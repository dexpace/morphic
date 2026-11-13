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
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/engine/reader"
)

// TestInScope_ReadsWhereTheBindingsPlaceASchema pins the copy InScope returns
// for lowering what a schema the bindings know holds (resolve.Scope.InDocumentOf):
// the source's own content for one the source holds, whatever copy it is taken
// from, and ext.yaml's for one a $ref reaches past /ext's $ref into ext.yaml.
// A schema the bindings do not know is read as the source's. The context it
// was taken from is left as it was.
func TestInScope_ReadsWhereTheBindingsPlaceASchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
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
	c := lowering.New(0, doc, lowering.Policy{})
	declared := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		t.Helper()
		js, ok := doc.Doc.Components.Schemas.Get(name)
		require.True(t, ok)
		decl := c.Bindings().Declared(js)
		require.NotNil(t, decl, "the $ref of %s lands on a schema", name)
		return decl
	}

	past := c.InScope(c.RefScope().InDocumentOf(declared("Past"))).RefScope()
	assert.True(t, past.Foreign, "past /ext's $ref, the schema is ext.yaml's content")
	assert.Equal(t, filepath.Join(dir, "ext.yaml"), past.Holder)
	assert.False(t, c.RefScope().Foreign, "the context it was taken from is left as it was")

	foreign := c.InScope(past)
	home := foreign.InScope(foreign.RefScope().InDocumentOf(declared("Home"))).RefScope()
	assert.False(t, home.Foreign, "what the source holds is read as the source's")
	assert.Empty(t, home.Holder)
	assert.True(t, foreign.RefScope().Foreign, "the context it was taken from is left as it was")

	unknown := foreign.RefScope().InDocumentOf(&oas3.JSONSchema[oas3.Referenceable]{})
	assert.False(t, unknown.Foreign, "a schema the bindings do not know is read as the source's")
}
