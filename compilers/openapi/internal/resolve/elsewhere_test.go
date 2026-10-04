package resolve_test

import (
	"os"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// elsewhereRoot is a source whose path items are reached each way an entry can
// be: written inline, by an internal $ref, by a $ref into ext.yaml, by one into
// ext.yaml that comes back into the source, and by one naming nothing.
const elsewhereRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /inline: {get: {responses: {"200": {description: ok}}}}
  /internal: {$ref: '#/components/pathItems/P'}
  /ext: {$ref: './ext.yaml#/paths/~1x'}
  /back: {$ref: './ext.yaml#/paths/~1back'}
  /missing: {$ref: '#/components/pathItems/Nope'}
components:
  pathItems:
    P: {get: {responses: {"200": {description: ok}}}}
`

// TestHeldElsewhere pins which entries another document holds: one whose chain
// ends in another document's object. One written here, one an internal $ref
// names, one that comes back into the source and one that resolved nothing
// are held by the source.
func TestHeldElsewhere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(`paths:
  /x: {get: {responses: {"200": {description: ok}}}}
  /back: {$ref: 'root.yaml#/components/pathItems/P'}
`), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(elsewhereRoot)}, load.Options{AllowExternalRefs: true})
	require.NoError(t, err)
	require.NotNil(t, doc)
	scope := resolve.Scope{Doc: doc.Doc}

	for path, want := range map[string]bool{
		"/inline": false, "/internal": false, "/ext": true, "/back": false, "/missing": false,
	} {
		rp, ok := doc.Doc.Paths.Get(path)
		require.True(t, ok, path)
		assert.Equal(t, want, resolve.HeldElsewhere[soa.PathItem](scope, rp), path)
	}
}

// TestInternalPointer_NoneInAForeignScope pins that a Foreign scope reads no
// reference as internal, however it is spelled: in another document's content
// each names a position in that document.
func TestInternalPointer_NoneInAForeignScope(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"#/components/schemas/A", "spec.yaml#/components/schemas/A"} {
		_, ok := resolve.Scope{SelfPath: "spec.yaml"}.InternalPointer(ref)
		require.True(t, ok, ref)
		_, ok = resolve.Scope{SelfPath: "spec.yaml", Foreign: true}.InternalPointer(ref)
		assert.False(t, ok, ref)
	}
}
