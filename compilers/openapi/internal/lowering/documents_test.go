package lowering_test

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
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
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
func loadSplit(t *testing.T, ext string) (lowering.Ctx, *load.Document) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte(ext), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(splitRoot)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(doc), doc
}

// extDoc returns the bindings' number for ext.yaml: where A's $ref lands.
func extDoc(t *testing.T, c lowering.Ctx) bind.DocID {
	t.Helper()
	a, ok := c.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	e, ok := c.Bindings().Entry(a)
	require.True(t, ok)
	require.NotEqual(t, bind.Source, e.Hop.Target.Doc, "sanity: A lands in ext.yaml")
	return e.Hop.Target.Doc
}

// TestInDocument_PlacesWhatAnotherDocumentDeclares pins the context a lowering
// of another document's content runs in: its references read in that
// document's scope, each Provenance naming its Document.Sources entry, and its
// IDs qualified by its label. The source's own number returns to the source's
// coordinates, as InScope does in any scope, and a context given no documents,
// or none holding that one, cannot place it.
func TestInDocument_PlacesWhatAnotherDocumentDeclares(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	others := doc.Others.List()
	require.Len(t, others, 1)
	assert.Equal(t, 1, others[0].Index)
	assert.Equal(t, "ext.yaml", others[0].Label)
	assert.Equal(t, []ir.SourceInfo{doc.Source, others[0].Info}, c.Sources(), "the source, then ext.yaml")
	id := extDoc(t, c)

	there, ok := lowering.InDocument(c, id)
	require.True(t, ok)
	assert.Equal(t, ids.In("ext.yaml"), there.IDs())
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X"}, there.ProvenanceAt("/X"))
	assert.True(t, there.RefScope().Foreign)
	assert.Equal(t, c.Bindings().Table().Document(id).Name, there.RefScope().Holder)

	back, ok := lowering.InDocument(there, bind.Source)
	require.True(t, ok)
	assert.Equal(t, ids.Document{}, back.IDs())
	assert.Equal(t, ir.Provenance{Pointer: "/X"}, back.ProvenanceAt("/X"))
	assert.False(t, back.RefScope().Foreign)
	assert.Equal(t, ids.Document{}, there.InScope(there.RefScope().InSource()).IDs(),
		"the source's scope is read at the source's coordinates")
	kept := there.InScope(there.RefScope())
	assert.True(t, kept.RefScope().Foreign)
	assert.Equal(t, ir.Provenance{Pointer: "/X"}, kept.ProvenanceAt("/X"),
		"InScope reads at the source's coordinates, in whichever scope")

	_, ok = lowering.InDocument(lowering.Ctx{}, id)
	assert.False(t, ok, "a context given no load holds no documents")
	none := *doc
	none.Others = load.Others{}
	unread := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&none)
	same, ok := lowering.InDocument(unread, id)
	assert.False(t, ok, "a document the compile did not list")
	assert.Equal(t, ids.Document{}, same.IDs(), "and the context is left as it was")
}

// TestIDsAt_NamesEachSourceEntrysDocument pins how the IDs of what each
// Document.Sources entry declares are qualified: the source's and its
// overlay's bare, and another document's by its label, which OtherIDs lists
// in Sources order.
func TestIDsAt_NamesEachSourceEntrysDocument(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")

	assert.Equal(t, ids.Document{}, lowering.IDsAt(c, 0))
	assert.Equal(t, ids.In("ext.yaml"), lowering.IDsAt(c, 1))
	assert.Equal(t, ids.Document{}, lowering.IDsAt(c, 2), "an index no document is at")
	assert.Equal(t, ids.Document{}, lowering.IDsAt(lowering.Ctx{}, 1), "a context given no load")
	assert.Equal(t, []ids.Document{ids.In("ext.yaml")}, lowering.OtherIDs(c))
	assert.Nil(t, lowering.OtherIDs(lowering.Ctx{}))
}

// TestDeclareHint_KeepsOnePointerInTwoDocumentsApart pins that one pointer in
// two documents is two positions: a hint recorded at the source's /X names
// nothing at another document's /X, and each keeps its own. ext.yaml's content
// read in its scope at the source's coordinates (InScope) is a third, though
// its scope is ext.yaml's.
func TestDeclareHint_KeepsOnePointerInTwoDocumentsApart(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	c.DeclareHint("/X", "source")
	_, recorded := there.HintAt("/X")
	assert.False(t, recorded, "the source's /X is not ext.yaml's")
	there.DeclareHint("/X", "ext")
	hint, _ := c.HintAt("/X")
	assert.Equal(t, "source", hint)
	hint, _ = there.HintAt("/X")
	assert.Equal(t, "ext", hint)
	_, recorded = there.InScope(there.RefScope()).HintAt("/X")
	assert.False(t, recorded, "ext.yaml's scope at the source's coordinates is neither")
}

// TestOperationIDAt_ReadsTheDeclaringDocument pins where a position's
// operationId is read: in the source's YAML, and its model where only a
// reference reaches the position; in another document's YAML alone, never the
// source's model, for that document's; and nowhere for a context given no
// document.
func TestOperationIDAt_ReadsTheDeclaringDocument(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\nO: {operationId: extOp}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	assert.Equal(t, "extOp", lowering.OperationIDAt(there, "/O"), "ext.yaml's, read in its YAML")
	assert.Empty(t, lowering.OperationIDAt(c, "/O"), "the source holds no /O")
	assert.Equal(t, "getA", lowering.OperationIDAt(c, "/paths/~1a/get"))
	assert.Empty(t, lowering.OperationIDAt(there, "/paths/~1a/get"), "ext.yaml writes none there, whatever the source does")

	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a: {get: {operationId: getA}}\n" +
			"  /b: {$ref: '#/components/pathItems/P'}\ncomponents:\n  pathItems:\n    P: {put: {operationId: putP}}\n")},
		load.Options{})
	require.NoError(t, err)
	source := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(doc)
	assert.Equal(t, "getA", lowering.OperationIDAt(source, "/paths/~1a/get"), "the source's, read where it is written")
	assert.Equal(t, "putP", lowering.OperationIDAt(source, "/paths/~1b/put"), "and in its model past a $ref")
	assert.Empty(t, lowering.OperationIDAt(lowering.Ctx{}, "/paths/~1a/get"), "a context given no document")
}

// TestDeclared_LowersAReferencedObjectWhereItIsDeclared pins where a reference
// object's declaration is lowered: in the document that declares it, at the
// position the bindings' table places its end. Where the table places none, as
// for the first of a key written twice, which the parser resolves to and the
// binder does not, the use site is kept, in the source.
func TestDeclared_LowersAReferencedObjectWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		ext  string
		home ids.Document
		at   string
	}{
		"declared once":       {"X: {type: string}\nP: {name: p, in: query}\n", ids.In("ext.yaml"), "/P"},
		"a key written twice": {"X: {type: string}\nP: {name: p, in: query}\nP: {name: q, in: query}\n", ids.Document{}, "/paths/~1a/get/parameters/0"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, _ := loadSplit(t, tc.ext)
			a, ok := c.Doc.Paths.Get("/a")
			require.True(t, ok)
			ref := a.GetObject().Get().GetParameters()[0]

			p, there, at := lowering.Declared[soa.Parameter](c, ref, "/paths/~1a/get/parameters/0")

			require.NotNil(t, p)
			assert.Equal(t, tc.home, there.IDs())
			assert.Equal(t, tc.at, string(at))
			assert.True(t, there.RefScope().Foreign, "read in ext.yaml's scope either way")
		})
	}
}

// TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference pins that a
// reference object's end is asked of by the document writing it (GitHub #751):
// one pointer in two documents is two positions, so where ext.yaml writes
// nothing at the pointer the source's reference is written at, the source's
// end there is not read, and the use site is kept.
func TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	ref := a.GetObject().Get().GetParameters()[0]
	inExt, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	_, there, at := lowering.Declared[soa.Parameter](inExt, ref, "/paths/~1a/get/parameters/0")

	assert.Equal(t, "/paths/~1a/get/parameters/0", string(at), "the source's end at that pointer is not read")
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

	p, there, at := lowering.Declared[soa.Parameter](c, ref, "/paths/~1a/get/parameters/0")

	assert.Nil(t, p)
	assert.Equal(t, "/paths/~1a/get/parameters/0", string(at))
	assert.Equal(t, ids.Document{}, there.IDs())
}
