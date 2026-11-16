// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service_test

import (
	"os"
	"path/filepath"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/engine/reader"
)

// splitRoot is a source whose component A names ext.yaml's X, and whose /a
// takes ext.yaml's parameter P.
const splitRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters: [{$ref: './ext.yaml#/P'}]
      responses: {"200": {description: ok}}
components:
  schemas:
    A: {$ref: './ext.yaml#/X'}
`

// loadSplit loads splitRoot beside ext, written as ext.yaml, reading ext.yaml
// from disk, and returns the context over it.
func loadSplit(t *testing.T, ext string) (service.Ctx, *load.Document) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(ext), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(splitRoot)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return service.New(0, doc, service.Policy{}), doc
}

// extDoc returns the bindings' number for ext.yaml: where A's $ref lands.
func extDoc(t *testing.T, c service.Ctx) bind.DocID {
	t.Helper()
	a, ok := c.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	e, ok := c.Bindings().Entry(a)
	require.True(t, ok)
	require.NotEqual(t, bind.Source, e.Hop.Target.Doc, "sanity: A lands in ext.yaml")
	return e.Hop.Target.Doc
}

// TestEncodingRules_FollowTheDocumentTheContextReads pins which document's
// rows an encoding is read with: those of the document whose content the
// context reads, the load phase's own for each.
func TestEncodingRules_FollowTheDocumentTheContextReads(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "openapi: 3.0.3\nX: {type: string}\nP: {name: p, in: query}\n")
	if service.EncodingRules(c) != doc.Rows.Encoding {
		t.Error("an encoding the source declares reads another document's rows")
	}
	there, ok := service.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	if service.EncodingRules(there) != doc.Others.List()[0].Rows.Encoding {
		t.Error("an encoding ext.yaml declares reads another document's rows")
	}
}

// TestOtherIDs_NamesEachDocumentReadBesideTheSource pins how the IDs of what
// a document a reference read beside the source declares are qualified: by
// its label, in Sources order.
func TestOtherIDs_NamesEachDocumentReadBesideTheSource(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")

	assert.Equal(t, []ids.Document{ids.In("ext.yaml")}, service.OtherIDs(c))
}

// TestDeclared_LowersAReferencedObjectWhereItIsDeclared pins where a reference
// object's declaration is lowered: in the document that declares it, at the
// position the bindings' table places its end, a key written twice read as its
// last copy, as the model reads one.
func TestDeclared_LowersAReferencedObjectWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		ext, name string
	}{
		"declared once":       {"X: {type: string}\nP: {name: p, in: query}\n", "p"},
		"a key written twice": {"X: {type: string}\nP: {name: p, in: query}\nP: {name: q, in: query}\n", "q"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, _ := loadSplit(t, tc.ext)
			a, ok := c.Doc.Paths.Get("/a")
			require.True(t, ok)
			ref := a.GetObject().Get().GetParameters()[0]

			p, there, at := service.Declared[soa.Parameter](c, ref, "/paths/~1a/get/parameters/0")

			require.NotNil(t, p)
			assert.Equal(t, tc.name, p.GetName())
			assert.Equal(t, ids.In("ext.yaml"), there.IDs())
			assert.Equal(t, "/P", string(at))
			assert.True(t, there.RefScope().Foreign, "read in ext.yaml's scope")
		})
	}
}

// TestDeclared_LowersNothingInADocumentTheContextDidNotList pins a reference
// object whose chain ends in a document the context's compile did not list:
// it lowers nothing, rather than read the declaration in the use site's
// document.
func TestDeclared_LowersNothingInADocumentTheContextDidNotList(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	none := *doc
	none.Others = load.Others{}
	unread := service.New(0, &none, service.Policy{})
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	ref := a.GetObject().Get().GetParameters()[0]

	p, at, ptr := service.Declared[soa.Parameter](unread, ref, "/paths/~1a/get/parameters/0")

	assert.Nil(t, p)
	assert.Equal(t, unread.IDs(), at.IDs())
	assert.Equal(t, "/paths/~1a/get/parameters/0", string(ptr))
}

// TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference pins that a
// reference object's end is the one the bindings' table reads for the
// position the wrapper was read at, in the document writing it (GitHub #751),
// whichever context asks: ext.yaml writes nothing at the pointer the source's
// reference is written at, and that pointer is no key.
func TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	ref := a.GetObject().Get().GetParameters()[0]
	inExt, ok := service.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	_, there, at := service.Declared[soa.Parameter](inExt, ref, "/paths/~1a/get/parameters/0")

	assert.Equal(t, "/P", string(at), "the source's reference ends at ext.yaml's P")
	assert.Equal(t, ids.In("ext.yaml"), there.IDs())
}

// TestDeclared_KeepsTheUseSiteOfAReferenceThatResolvedNothing pins Declared
// for a reference with no end: ext.yaml declares no P, so nothing is lowered,
// and the use site is kept in the source's coordinates, where the load phase
// reported it.
func TestDeclared_KeepsTheUseSiteOfAReferenceThatResolvedNothing(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\n")
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	ref := a.GetObject().Get().GetParameters()[0]

	p, there, at := service.Declared[soa.Parameter](c, ref, "/paths/~1a/get/parameters/0")

	assert.Nil(t, p)
	assert.Equal(t, "/paths/~1a/get/parameters/0", string(at))
	assert.Equal(t, ids.Document{}, there.IDs())
}
