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
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestWithin pins the copy Within returns for lowering what an entry names:
// when another document holds it, a scope marked Foreign that reads each
// reference against that document, so only one naming the source is internal.
// A copy already Foreign stays so for an entry written inline in that content,
// and is the source's again for one whose chain ends in the source. An entry
// whose pointer passes /ext's $ref names ext.yaml's content, read as /ext's is.
func TestWithin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"+
			"  /back: {$ref: 'root.yaml#/paths/~1inline'}\n"), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /inline: {get: {responses: {"200": {description: ok}}}}
  /ext: {$ref: './ext.yaml#/paths/~1x'}
  /back: {$ref: './ext.yaml#/paths/~1back'}
  /through: {get: {responses: {"200": {$ref: '#/paths/~1ext/get/responses/200'}}}}
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

	assert.False(t, lowering.Within[soa.PathItem](c, inline).RefScope().Foreign)
	foreign := lowering.Within[soa.PathItem](c, ext)
	scope := foreign.RefScope()
	require.True(t, scope.Foreign)
	assert.Equal(t, filepath.Join(dir, "ext.yaml"), scope.Holder)
	assert.False(t, c.RefScope().Foreign, "c itself is left as it was")
	_, internal := scope.InternalPointer("#/paths/~1inline")
	assert.False(t, internal, "a pointer alone names a position in the other document")
	_, internal = scope.InternalPointer("root.yaml#/paths/~1inline")
	assert.True(t, internal, "the source's file name, read beside ext.yaml, names the source")
	assert.True(t, lowering.Within[soa.PathItem](foreign, inline).RefScope().Foreign, "content within stays foreign")
	back, ok := doc.Doc.Paths.Get("/back")
	require.True(t, ok)
	home := lowering.Within[soa.PathItem](foreign, back).RefScope()
	assert.False(t, home.Foreign, "what the source holds is read as the source's")
	assert.Empty(t, home.Holder)
	through, ok := doc.Doc.Paths.Get("/through")
	require.True(t, ok)
	passing, ok := through.GetObject().Get().GetResponses().Get("200")
	require.True(t, ok)
	require.NotNil(t, resolve.Object[soa.Response](passing), "the resolver walked through /ext's $ref")
	past := lowering.Within[soa.Response](c, passing).RefScope()
	assert.True(t, past.Foreign, "past /ext's $ref, the pointer names ext.yaml's content")
	assert.Equal(t, scope.Holder, past.Holder, "read where /ext's own entry reads it")
}

// TestAt pins the copy At returns for lowering what an internal pointer names:
// the source's own content, whatever copy it is taken from, so a pointer alone
// names the source's position again, unless the resolver's walk to it passes a
// $ref into another document, whose content it then is. The $ref the pointer
// ends at is not passed. The context it was taken from is left as it was.
func TestAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("paths:\n  /x: {get: {responses: {\"200\": {description: ok}}}}\n"), 0o600))
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /ext: {$ref: './ext.yaml#/paths/~1x'}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{AllowExternalRefs: true})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, overlay.Origin{})
	ext, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)
	foreign := lowering.Within[soa.PathItem](c, ext)
	require.True(t, foreign.RefScope().Foreign)

	home := foreign.InScope(foreign.RefScope().At("/paths/~1ext")).RefScope()
	assert.False(t, home.Foreign, "what the source holds is read as the source's")
	assert.Empty(t, home.Holder)
	_, internal := home.InternalPointer("#/paths/~1ext")
	assert.True(t, internal, "a pointer alone names the source's position again")
	assert.True(t, foreign.RefScope().Foreign, "the context it was taken from is left as it was")

	past := c.InScope(c.RefScope().At("/paths/~1ext/get/responses/200")).RefScope()
	assert.True(t, past.Foreign, "past /ext's $ref, the pointer names ext.yaml's content")
	assert.Equal(t, filepath.Join(dir, "ext.yaml"), past.Holder)
	assert.False(t, c.RefScope().Foreign, "the context it was taken from is left as it was")
}
