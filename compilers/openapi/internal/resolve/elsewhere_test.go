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

// TestHeldElsewhere pins which entries another document holds, and the path
// it was read by: one whose chain ends in another document's object. One
// written here, one an internal $ref names, one that comes back into the
// source and one that resolved nothing are held by the source.
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

	ext := filepath.Join(dir, "ext.yaml")
	for path, want := range map[string]string{
		"/inline": "", "/internal": "", "/ext": ext, "/back": "", "/missing": "",
	} {
		rp, ok := doc.Doc.Paths.Get(path)
		require.True(t, ok, path)
		holder, elsewhere := resolve.HeldElsewhere[soa.PathItem](scope, rp)
		assert.Equal(t, want, holder, path)
		assert.Equal(t, want != "", elsewhere, path)
	}
}

// TestInternalPointer_InAForeignScope pins how a Foreign scope reads a
// reference: against the document holding it, as the resolver does. A pointer
// alone names a position there, and so does a document part naming another
// file. One naming the source, however spelled from there, is internal, and
// nothing is internal with no holder to read against, though read from the
// working directory the last row would name the source.
func TestInternalPointer_InAForeignScope(t *testing.T) {
	t.Parallel()
	const self = "spec.yaml"
	for _, c := range []struct {
		holder, ref string
		internal    bool
	}{
		{"ext.yaml", "#/components/schemas/A", false},
		{"ext.yaml", "spec.yaml#/components/schemas/A", true},
		{"ext.yaml", "./spec.yaml#/components/schemas/A", true},
		{"ext.yaml", "other.yaml#/components/schemas/A", false},
		{"sub/ext.yaml", "../spec.yaml#/components/schemas/A", true},
		{"sub/ext.yaml", "spec.yaml#/components/schemas/A", false},
		{"https://example.com/ext.yaml", "spec.yaml#/components/schemas/A", false},
		{"", "spec.yaml#/components/schemas/A", false},
	} {
		scope := resolve.Scope{SelfPath: self, Foreign: true, Holder: c.holder}
		pointer, ok := scope.InternalPointer(c.ref)
		assert.Equal(t, c.internal, ok, "%s from %s", c.ref, c.holder)
		if c.internal {
			assert.Equal(t, "/components/schemas/A", string(pointer))
		}
	}
}

// TestNamesHolder pins which references in a Foreign scope name a position in
// the document holding them: a pointer alone, or a document part naming that
// document. Outside a Foreign scope, none does.
func TestNamesHolder(t *testing.T) {
	t.Parallel()
	scope := resolve.Scope{SelfPath: "api/spec.yaml", Foreign: true, Holder: "api/ext.yaml"}
	for ref, want := range map[string]bool{
		"#/components/schemas/A":            true,
		"ext.yaml#/components/schemas/A":    true,
		"spec.yaml#/components/schemas/A":   false,
		"third.yaml#/components/schemas/A":  false,
		"https://example.com/ext.yaml#/x/y": false,
	} {
		assert.Equal(t, want, scope.NamesHolder(ref), ref)
	}
	assert.False(t, resolve.Scope{SelfPath: "api/spec.yaml"}.NamesHolder("#/components/schemas/A"))
	unplaceable := resolve.Scope{SelfPath: "api/spec.yaml", Foreign: true, Holder: "http://[::1"}
	assert.False(t, unplaceable.NamesHolder("#/components/schemas/A"), "a holder the resolver cannot place")
}

// TestSameDocument pins when the resolver reading one path reads the document
// at another: the same spelling, or the same file however reached. A URL is
// one document only as spelled, and an empty path names none.
func TestSameDocument(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("api/spec.yaml")
	require.NoError(t, err)
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"api/spec.yaml", "api/spec.yaml", true},
		{"api/spec.yaml", "api/./sub/../spec.yaml", true},
		{"api/spec.yaml", abs, true},
		{"api/spec.yaml", "api/other.yaml", false},
		{"https://example.com/a.yaml", "https://example.com/a.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/./a.yaml", false},
		{"", "", false},
	} {
		assert.Equal(t, c.want, resolve.SameDocument(c.a, c.b), "%q %q", c.a, c.b)
	}
}
