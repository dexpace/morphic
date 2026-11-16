// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
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
	return service.New(0, doc, service.Policy{}).Ctx, doc
}

// extDoc returns the bindings' number for ext.yaml.
func extDoc(t *testing.T, c lowering.Ctx) bind.DocID {
	t.Helper()
	table := c.Bindings().Table()
	for id := range bind.DocID(table.Documents()) {
		if filepath.Base(table.Document(id).Name) == "ext.yaml" {
			return id
		}
	}
	require.FailNow(t, "the bindings read no ext.yaml")
	return bind.Source
}

// TestInDocument_PlacesWhatAnotherDocumentDeclares pins the context a lowering
// of another document's content runs in: its references read in that
// document's scope, each Provenance naming its Document.Sources entry, and its
// IDs qualified by its label. The source's own number returns to the source's
// coordinates, and a context given no document holding that one cannot place
// it.
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
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X"}, there.Place().At("/X"))
	assert.True(t, there.RefScope().Foreign)
	assert.Equal(t, c.Bindings().Table().Document(id).Name, there.RefScope().Holder)

	back, ok := lowering.InDocument(there, bind.Source)
	require.True(t, ok)
	assert.Equal(t, ids.Document{}, back.IDs())
	assert.Equal(t, ir.Provenance{Pointer: "/X"}, back.Place().At("/X"))
	assert.False(t, back.RefScope().Foreign)
	assert.Empty(t, back.RefScope().Holder)
	assert.Equal(t, there.Position("/X").Pointer, back.Position("/X").Pointer)
	assert.Equal(t, bind.Source, back.Position("/X").Doc)
	assert.Equal(t, id, there.Position("/X").Doc)

	none := *doc
	none.Others = load.Others{}
	unread := service.New(0, &none, service.Policy{})
	same, ok := lowering.InDocument(unread.Ctx, id)
	assert.False(t, ok, "a document the compile did not list")
	assert.Equal(t, ids.Document{}, same.IDs(), "and the context is left as it was")
}

// TestSchemaRules_FollowTheDocumentTheScopeReads pins which document's rows a
// context reads a schema with: that of the document its scope reads, through
// InDocument, and back to the source's. Rules().In answers
// for a document of the bindings' table, and with the source's for one the
// compile did not read.
func TestSchemaRules_FollowTheDocumentTheScopeReads(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "openapi: 3.0.3\nX: {type: string}\nP: {name: p, in: query}\n")
	id := extDoc(t, c)
	assert.False(t, c.SchemaRules().Defines("nullable"), "the 3.1 source")

	there, ok := lowering.InDocument(c, id)
	require.True(t, ok)
	assert.True(t, there.SchemaRules().Defines("nullable"), "ext.yaml declares 3.0.3")
	back, ok := lowering.InDocument(there, bind.Source)
	require.True(t, ok)
	assert.False(t, back.SchemaRules().Defines("nullable"))

	assert.True(t, c.Rules().In(id).Defines("nullable"))
	assert.False(t, c.Rules().In(bind.Source).Defines("nullable"))

	unread := bind.DocID(c.Bindings().Table().Documents())
	assert.False(t, c.Rules().In(unread).Defines("nullable"), "a document the compile did not read")
}

// TestRules_OfReadsTheDialectTheSchemaHoldingItNames pins whose rows a
// schema is read with by its node: those of the dialect the root holding it
// names, here a document of schemas naming draft 4 at its root, and else the
// source's.
func TestRules_OfReadsTheDialectTheSchemaHoldingItNames(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "$schema: 'http://json-schema.org/draft-04/schema#'\nX: {type: integer}\nP: {name: p, in: query}\n")
	a, ok := doc.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	x := c.Bindings().End(a)
	require.NotNil(t, x)
	assert.True(t, c.Rules().Of(x.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Boolean))
	assert.True(t, c.Rules().Of(a.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Number),
		"the source's own schema reads its document's")
}

// TestRules_OfReadsANodeInItsOwnDocumentsDialect pins that a reading by
// node answers in the dialect of the document holding the schema, not of the
// one whose content the context reads: ext.yaml's X, a 3.0 document's, reads
// 3.0's rows from the source's context, and the source's A reads the source's
// from ext.yaml's, as a $ref followed either way finds them.
func TestRules_OfReadsANodeInItsOwnDocumentsDialect(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "openapi: 3.0.3\nX: {type: integer}\nP: {name: p, in: query}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	a, ok := doc.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	x := c.Bindings().End(a)
	require.NotNil(t, x)
	boolean := func(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) bool {
		return c.Rules().Of(js.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, boolean(c, x), "ext.yaml's X, from the source")
	assert.True(t, boolean(there, x), "ext.yaml's X, from ext.yaml")
	assert.False(t, boolean(there, a), "the source's A, from ext.yaml")
	assert.False(t, boolean(c, a), "the source's A, from the source")
}

// TestRules_AtReadsTheDialectOfWhereASchemaIs pins whose rows a schema
// is read with by where it is: those of the dialect the root at or above it
// names, here a document of schemas naming draft 4 at its root, read in that
// document's context; and the context's own where none is, as for the
// source's A.
func TestRules_AtReadsTheDialectOfWhereASchemaIs(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "$schema: 'http://json-schema.org/draft-04/schema#'\nX: {type: integer}\nP: {name: p, in: query}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	boolean := func(c lowering.Ctx, at jsontext.Pointer) bool {
		return c.Rules().At(c.Position(at)).Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, boolean(there, "/X"), "ext.yaml's root names draft 4")
	assert.False(t, boolean(c, "/components/schemas/A"), "the source's own schema reads its document's")
}

// TestNew_ReadsTheSourcesRowsFromTheLoad pins where a context over a loaded
// document gets the source's rows: the load phase's own (load.Document.Rows),
// held by identity, so building the context builds no rule table, which alone
// allocates more than the whole context does. It runs alone, since the
// allocation counts are the process's.
func TestNew_ReadsTheSourcesRowsFromTheLoad(t *testing.T) {
	c, doc := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	if c.SchemaRules() != doc.Rows.Schema {
		t.Error("the context read rows of its own, not the load phase's")
	}
	table := testing.AllocsPerRun(10, func() { _ = oas.Table() })
	built := testing.AllocsPerRun(10, func() {
		_ = service.New(0, doc, service.Policy{})
	})
	assert.Less(t, built, table, "building the context built a rule table")
}

// TestNameAt_NamesAPositionInTheContextsDocument pins that one pointer in two
// documents is two positions: ext.yaml's /P/schema is a parameter's schema,
// named by the parameter's name, while the source's /P/schema is no schema the
// grammar reads, named from its pointer.
func TestNameAt_NamesAPositionInTheContextsDocument(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query, schema: {type: string}}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	assert.Equal(t, "p", lowering.NameAt(there, "/P/schema"))
	assert.Equal(t, "schema", lowering.NameAt(c, "/P/schema"))
}

// TestNameThrough_NamesWhereAReferenceLands pins that the name a $ref suggests
// is that of the position it lands on, in whichever document, and that a
// position writing no $ref suggests none.
func TestNameThrough_NamesWhereAReferenceLands(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	assert.Equal(t, "X", lowering.NameThrough(c, "/components/schemas/A"))
	assert.Empty(t, lowering.NameThrough(c, "/paths/~1a/get"), "nothing is referenced there")
}

// TestNameTitle_IsTheNameATitleGivesInTheContextsDocument pins that the title
// a context reads is the one written at the pointer in its own document:
// ext.yaml's parameter schema is titled, and the source's /P/schema is no
// schema at all.
func TestNameTitle_IsTheNameATitleGivesInTheContextsDocument(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query, schema: {title: Page Size, type: object}}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	title, titled := lowering.NameTitle(there, "/P/schema")
	assert.True(t, titled)
	assert.Equal(t, "page_size", title)
	title, titled = lowering.NameTitle(c, "/P/schema")
	assert.False(t, titled)
	assert.Empty(t, title)
}

// TestNameOfDocument_NamesWhatADocumentsRootDeclares pins the name of what is
// declared at another document's root, which no key names: the document's,
// what follows the last '/' of its name.
func TestNameOfDocument_NamesWhatADocumentsRootDeclares(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\n")
	assert.Equal(t, "ext.yaml", lowering.NameOfDocument(c, extDoc(t, c)))
}
