// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
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
	return lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
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
	unread := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&none)
	same, ok := lowering.InDocument(unread, id)
	assert.False(t, ok, "a document the compile did not list")
	assert.Equal(t, ids.Document{}, same.IDs(), "and the context is left as it was")
}

// TestScoped_KeysAPositionByTheDocumentItIsRead pins that a position makes a
// key for each document it is read in: in another document's coordinates and
// in that document's scope at the source's, which differ only in where
// positions are placed, and in the scopes of two other documents, which differ
// only in which document holds them.
func TestScoped_KeysAPositionByTheDocumentItIsRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{"ext.yaml": "X: {type: string}\n", "other.yaml": "Y: {type: string}\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A: {$ref: './ext.yaml#/X'}\n    B: {$ref: './other.yaml#/Y'}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(root)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(doc)
	in := func(component string) lowering.Ctx {
		t.Helper()
		s, ok := c.Doc.Components.Schemas.Get(component)
		require.True(t, ok)
		e, ok := c.Bindings().Entry(s)
		require.True(t, ok)
		there, ok := lowering.InDocument(c, e.Hop.Target.Doc)
		require.True(t, ok)
		return there
	}
	there, elsewhere := in("A"), in("B")
	atSource, elsewhereAtSource := there.InScope(there.RefScope()), elsewhere.InScope(elsewhere.RefScope())
	require.Equal(t, there.RefScope().Holder, atSource.RefScope().Holder, "one scope")
	require.True(t, atSource.RefScope().Foreign && elsewhereAtSource.RefScope().Foreign, "two documents' scopes")
	at := bind.Position{Pointer: "/X"}

	assert.NotEqual(t, lowering.Scoped(there, at), lowering.Scoped(atSource, at), "placed in ext.yaml and in the source")
	assert.NotEqual(t, lowering.Scoped(atSource, at), lowering.Scoped(elsewhereAtSource, at),
		"read in ext.yaml's scope and in other.yaml's")
}

// TestSchemaRules_FollowTheDocumentTheScopeReads pins which document's rows a
// context reads a schema with: that of the document its scope reads, through
// InDocument and InScope alike, and back to the source's. SchemaRulesIn answers
// for a document of the bindings' table, and a context that did not list the
// document, or has no load phase, answers with the source's.
func TestSchemaRules_FollowTheDocumentTheScopeReads(t *testing.T) {
	t.Parallel()
	c, doc := loadSplit(t, "openapi: 3.0.3\nX: {type: string}\nP: {name: p, in: query}\n")
	id := extDoc(t, c)
	assert.False(t, c.SchemaRules().Defines("nullable"), "the 3.1 source")

	there, ok := lowering.InDocument(c, id)
	require.True(t, ok)
	assert.True(t, there.SchemaRules().Defines("nullable"), "ext.yaml declares 3.0.3")
	assert.True(t, there.InScope(there.RefScope()).SchemaRules().Defines("nullable"),
		"ext.yaml's content read at the source's coordinates")
	back, ok := lowering.InDocument(there, bind.Source)
	require.True(t, ok)
	assert.False(t, back.SchemaRules().Defines("nullable"))

	assert.True(t, lowering.SchemaRulesIn(c, id).Defines("nullable"))
	assert.False(t, lowering.SchemaRulesIn(c, bind.Source).Defines("nullable"))
	assert.False(t, lowering.SchemaRulesIn(lowering.Ctx{}, id).Defines("nullable"), "no load phase")

	none := *doc
	none.Others = load.Others{}
	unread := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&none)
	assert.False(t, lowering.SchemaRulesIn(unread, id).Defines("nullable"), "a document the compile did not list")
	assert.False(t, unread.InScope(there.RefScope()).SchemaRules().Defines("nullable"))
}

// TestSchemaRulesOf_ReadsTheDialectTheSchemaHoldingItNames pins whose rows a
// schema is read with by its node: those of the dialect the root holding it
// names, here a document of schemas naming draft 4 at its root, and else the
// source's. A context with no load phase knows no root.
func TestSchemaRulesOf_ReadsTheDialectTheSchemaHoldingItNames(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "$schema: 'http://json-schema.org/draft-04/schema#'\nX: {type: integer}\nP: {name: p, in: query}\n")
	a, ok := c.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	x := c.Bindings().End(a)
	require.NotNil(t, x)
	assert.True(t, lowering.SchemaRulesOf(c, x.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Boolean))
	assert.True(t, lowering.SchemaRulesOf(c, a.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Number),
		"the source's own schema reads its document's")
	bare := lowering.New(0, c.Doc, c.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	assert.True(t, lowering.SchemaRulesOf(bare, x.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Number),
		"no load phase")
}

// TestSchemaRulesOf_ReadsANodeInItsOwnDocumentsDialect pins that a reading by
// node answers in the dialect of the document holding the schema, not of the
// one whose content the context reads: ext.yaml's X, a 3.0 document's, reads
// 3.0's rows from the source's context, and the source's A reads the source's
// from ext.yaml's, as a $ref followed either way finds them.
func TestSchemaRulesOf_ReadsANodeInItsOwnDocumentsDialect(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "openapi: 3.0.3\nX: {type: integer}\nP: {name: p, in: query}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	a, ok := c.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	x := c.Bindings().End(a)
	require.NotNil(t, x)
	boolean := func(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) bool {
		return lowering.SchemaRulesOf(c, js.GetSchema().GetRootNode()).Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, boolean(c, x), "ext.yaml's X, from the source")
	assert.True(t, boolean(there, x), "ext.yaml's X, from ext.yaml")
	assert.False(t, boolean(there, a), "the source's A, from ext.yaml")
	assert.False(t, boolean(c, a), "the source's A, from the source")
}

// TestSchemaRulesAt_ReadsTheDialectOfWhereASchemaIs pins whose rows a schema
// is read with by where it is: those of the dialect the root at or above it
// names, here a document of schemas naming draft 4 at its root, read in that
// document's context; the context's own where none is, as for the source's A;
// and the context's where it places no position, as InScope reads another
// document's content at the source's. A context with no load phase knows no
// root.
func TestSchemaRulesAt_ReadsTheDialectOfWhereASchemaIs(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "$schema: 'http://json-schema.org/draft-04/schema#'\nX: {type: integer}\nP: {name: p, in: query}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)
	boolean := func(c lowering.Ctx, at jsontext.Pointer) bool {
		return lowering.SchemaRulesAt(c, at).Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, boolean(there, "/X"), "ext.yaml's root names draft 4")
	assert.False(t, boolean(c, "/components/schemas/A"), "the source's own schema reads its document's")
	assert.False(t, boolean(there.InScope(there.RefScope()), "/X"), "no position, the scope's document's rows")
	bare := lowering.New(0, c.Doc, c.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
		lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{})
	assert.False(t, boolean(bare, "/X"), "no load phase")
}

// TestSchemaRulesIn_AsksTheSourcesRowsAtNoCost pins the cost of the question
// the schema readers ask of every schema they read across the bindings: the
// source's rows come without a lookup, where one by its name would parse it as
// a URL on each miss. It runs alone, since the allocation counts are the
// process's.
func TestSchemaRulesIn_AsksTheSourcesRowsAtNoCost(t *testing.T) {
	c, _ := loadSplit(t, "openapi: 3.0.3\nX: {type: string}\nP: {name: p, in: query}\n")
	var defines bool
	allocs := testing.AllocsPerRun(100, func() { defines = lowering.SchemaRulesIn(c, bind.Source).Defines("nullable") })
	assert.Zero(t, allocs)
	assert.False(t, defines, "the 3.1 source defines no nullable")
}

// TestWithLoaded_ReadsTheSourcesRowsFromTheLoad pins where a context over a
// loaded document gets the source's rows: the load phase's own
// (load.Document.Rows), held by identity, so building the context builds no
// rule table, which alone allocates more than the whole context does. It runs
// alone, since the allocation counts are the process's.
func TestWithLoaded_ReadsTheSourcesRowsFromTheLoad(t *testing.T) {
	c, doc := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	if c.SchemaRules() != doc.Rows.Schema || lowering.EncodingRules(c) != doc.Rows.Encoding {
		t.Error("the context read rows of its own, not the load phase's")
	}
	table := testing.AllocsPerRun(10, func() { _ = oas.Table() })
	built := testing.AllocsPerRun(10, func() {
		_ = lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.BinaryMedia{},
			lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(doc)
	})
	assert.Less(t, built, table, "building the context built a rule table")
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

// TestNameAt_NamesAPositionInTheContextsDocument pins that one pointer in two
// documents is two positions: ext.yaml's /P/schema is a parameter's schema,
// named by the parameter's name, while the source's /P/schema is no schema the
// grammar reads, named from its pointer. ext.yaml's content read in its scope
// at the source's coordinates (InScope) names the source's, and a context
// given no load names every position from its pointer.
func TestNameAt_NamesAPositionInTheContextsDocument(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query, schema: {type: string}}\n")
	there, ok := lowering.InDocument(c, extDoc(t, c))
	require.True(t, ok)

	assert.Equal(t, "p", lowering.NameAt(there, "/P/schema"))
	assert.Equal(t, "schema", lowering.NameAt(c, "/P/schema"))
	assert.Equal(t, "schema", lowering.NameAt(there.InScope(there.RefScope()), "/P/schema"))
	assert.Equal(t, "schema", lowering.NameAt(lowering.Ctx{}, "/P/schema"))
}

// TestNameThrough_NamesWhereAReferenceLands pins that the name a $ref suggests
// is that of the position it lands on, in whichever document, and that a
// position writing no $ref, or a context given no load, suggests none.
func TestNameThrough_NamesWhereAReferenceLands(t *testing.T) {
	t.Parallel()
	c, _ := loadSplit(t, "X: {type: string}\nP: {name: p, in: query}\n")
	assert.Equal(t, "X", lowering.NameThrough(c, "/components/schemas/A"))
	assert.Empty(t, lowering.NameThrough(c, "/paths/~1a/get"), "nothing is referenced there")
	assert.Empty(t, lowering.NameThrough(lowering.Ctx{}, "/components/schemas/A"))
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
