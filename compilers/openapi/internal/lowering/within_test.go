package lowering_test

import (
	"os"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// TestWithin pins the copy Within returns for lowering what an entry names:
// marked Foreign, with a scope reading no reference as internal, when another
// document holds it, and c as it was otherwise. A copy already Foreign stays
// so for an entry written inline in that content.
func TestWithin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /inline: {get: {responses: {"200": {description: ok}}}}
  /ext: {$ref: './ext.yaml#/paths/~1x'}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{AllowExternalRefs: true})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, overlay.Origin{})
	inline, ok := doc.Doc.Paths.Get("/inline")
	require.True(t, ok)
	ext, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)

	assert.False(t, lowering.Within[soa.PathItem](c, inline).Foreign())
	foreign := lowering.Within[soa.PathItem](c, ext)
	require.True(t, foreign.Foreign())
	assert.False(t, c.Foreign(), "c itself is left as it was")
	_, internal := foreign.RefScope().InternalPointer("#/paths/~1inline")
	assert.False(t, internal, "the copy's scope reads no reference as internal")
	assert.True(t, lowering.Within[soa.PathItem](foreign, inline).Foreign(), "content within stays foreign")
}
