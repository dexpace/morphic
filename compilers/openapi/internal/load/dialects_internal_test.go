// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// draft04 names JSON Schema draft 4, a dialect the table has rows for.
const draft04 = "http://json-schema.org/draft-04/schema#"

// TestDialects_ReadADocumentAtTheMinorItDeclares pins the minor a document a
// reference reaches is read at: the one it declares, else the source's, for a
// document declaring no version or one the table does not cover, and the
// rows of a minor shared by every document read at it. A document's
// jsonSchemaDialect chooses its rows at that minor.
func TestDialects_ReadADocumentAtTheMinorItDeclares(t *testing.T) {
	t.Parallel()
	d := &dialects{spec: oas.Table(), version: "3.1.0", minor: "3.1"}
	version, minor, r30 := d.of("3.0.3", "")
	assert.Equal(t, []string{"3.0.3", "3.0"}, []string{version, minor})
	assert.True(t, r30.Schema.Defines("nullable"))
	for _, declared := range []string{"", "3.3.0", "2.0"} {
		version, minor, r := d.of(declared, "")
		assert.Equal(t, []string{"3.1.0", "3.1"}, []string{version, minor}, "%q", declared)
		assert.False(t, r.Schema.Defines("nullable"), "%q", declared)
	}
	// Identity, not equality: rows built twice would be equal, and the claim is
	// that a minor's rows are built once.
	if _, _, again := d.of("3.0.0", ""); again != r30 {
		t.Error("a second document declaring 3.0 built the minor's rows again")
	}
	_, _, r04 := d.of("3.2.0", draft04)
	assert.True(t, r04.Schema.Takes("exclusiveMinimum", oas.Boolean), "a jsonSchemaDialect chooses the rows")
	_, _, r04 = d.of("", draft04)
	assert.True(t, r04.Schema.Takes("exclusiveMinimum", oas.Boolean), "at the source's minor")
	assert.True(t, d.at("3.1.0", "https://example.com/custom").unread)
	assert.False(t, d.at("3.0.3", "https://example.com/custom").unread, "3.0 names no dialect")
}

// TestDeclaredDialect_ReadsAnOpenAPIDocumentsOwn pins whose jsonSchemaDialect
// a document a reference reaches declares: an OpenAPI document's, and none for
// a document declaring no version, whose root is no OpenAPI Object.
func TestDeclaredDialect_ReadsAnOpenAPIDocumentsOwn(t *testing.T) {
	t.Parallel()
	tree := func(src string) *yaml.Node {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &n))
		return &n
	}
	assert.Equal(t, draft04, declaredDialect(tree("openapi: 3.1.0\njsonSchemaDialect: '"+draft04+"'\n")))
	assert.Empty(t, declaredDialect(tree("jsonSchemaDialect: '"+draft04+"'\n")), "no OpenAPI Object")
	assert.Empty(t, declaredDialect(tree("openapi: 3.1.0\njsonSchemaDialect: null\n")))
	assert.Empty(t, declaredDialect(tree("openapi: 3.1.0\n")))
}

// documentsReader answers each request with the document its location names.
type documentsReader map[string]string

// ReadDocument answers req.
func (r documentsReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) ([]byte, error) {
	if d, ok := r[req.Location]; ok {
		return []byte(d), nil
	}
	return nil, fs.ErrNotExist
}

// TestReadDialects_GovernsWhatEachRootHoldsAndWarnsOfWhatItCannotRead pins
// what the load phase hands the lowering of the dialects the documents name:
// the rows of each schema resource root that names one, for every node it
// holds, a document of schemas named at its root included, and an
// openapi/unread-dialect warning at each `$schema` and jsonSchemaDialect
// naming one the table has no rows for, in whichever document writes it. A
// schema below none, or below one whose `$schema` names no dialect, reads no
// rows of its own.
func TestReadDialects_GovernsWhatEachRootHoldsAndWarnsOfWhatItCannotRead(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: T, version: '1'}\njsonSchemaDialect: 'https://example.com/source'\n" +
		"paths: {}\ncomponents:\n  schemas:\n" +
		"    T: {$schema: '" + draft04 + "', properties: {a: {type: integer}}}\n" +
		"    U: {$schema: 'https://example.com/component'}\n" +
		"    R: {$schema: 'rel/dialect', type: integer}\n" +
		"    V: {$ref: 'o.yaml#/components/schemas/W'}\n" +
		"    X: {$ref: 'defs.json#/definitions/E'}\n"
	reader := documentsReader{
		"/r/o.yaml": "openapi: 3.1.0\ninfo: {title: O, version: '1'}\njsonSchemaDialect: 'https://example.com/other'\n" +
			"paths: {}\ncomponents:\n  schemas:\n    W: {type: integer}\n",
		"/r/defs.json": `{"$schema": "` + draft04 + `", "definitions": {"E": {"type": "integer"}}}`,
	}
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(src)},
		Options{Reader: reader})
	require.NoError(t, err)
	require.NotNil(t, doc)

	var unread []ir.Provenance
	for _, d := range diags {
		if d.Code == diag.UnreadDialect {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			unread = append(unread, ir.Provenance{Source: d.Provenance.Source, Pointer: d.Provenance.Pointer})
		}
	}
	other, ok := doc.Others.Named("/r/o.yaml")
	require.True(t, ok)
	assert.ElementsMatch(t, []ir.Provenance{
		{Source: 0, Pointer: "/jsonSchemaDialect"},
		{Source: 0, Pointer: "/components/schemas/U/$schema"},
		{Source: other.Index, Pointer: "/jsonSchemaDialect"},
	}, unread)

	schemas := doc.Doc.Components.GetSchemas()
	node := func(name string, below ...string) *yaml.Node {
		s := schemas.GetOrZero(name).GetSchema()
		for _, p := range below {
			s = s.GetProperties().GetOrZero(p).GetSchema()
		}
		return s.GetRootNode()
	}
	governed := func(n *yaml.Node) bool {
		r, ok := doc.Dialects.Of(n)
		return ok && r.Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, governed(node("T")))
	assert.True(t, governed(node("T", "a")), "a subschema reads its root's")
	_, ok = doc.Dialects.Of(node("V"))
	assert.False(t, ok, "a schema below no root naming one reads its document's")
	_, ok = doc.Dialects.Of(node("R"))
	assert.False(t, ok, "a $schema naming no dialect chooses none")
	r, ok := doc.Dialects.Of(node("U"))
	assert.True(t, ok)
	assert.True(t, r.Takes("exclusiveMinimum", oas.Number), "a dialect it cannot read reads as the minor's own")

	table := doc.Bindings.Table()
	var defs bind.DocID
	for id := range bind.DocID(table.Documents()) {
		if strings.HasSuffix(table.Document(id).Name, "defs.json") {
			defs = id
		}
	}
	require.NotZero(t, defs)
	e := doc.Bindings.At(bind.Position{Doc: defs, Pointer: jsontext.Pointer("/definitions/E")})
	require.NotNil(t, e)
	assert.True(t, governed(e.GetSchema().GetRootNode()), "a document of schemas names its dialect at its root")
	assert.True(t, other.Rows.Schema.Takes("exclusiveMinimum", oas.Number), "o.yaml's own dialect is unread")
	_, ok = Dialects{}.Of(nil)
	assert.False(t, ok, "the zero Dialects governs nothing")
}

// customServed answers every request with a document of schemas naming a
// dialect the table has no rows for at its root.
type customServed struct{}

// ReadDocument answers every request alike.
func (customServed) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	return []byte("{$schema: 'https://example.com/custom', X: {type: string}}\n"), nil
}

// TestReadDialects_WarnsOfADocumentReadUnderTwoNamesOnce pins that a document
// the binder reads under two spellings, one Document.Sources entry, is warned
// of once: its `$schema` is one finding, whichever name reached it.
func TestReadDialects_WarnsOfADocumentReadUnderTwoNamesOnce(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    A: {$ref: 'http://h/a b.yaml#/X'}\n    B: {$ref: 'http://h/a%20b.yaml#/X'}\n",
	)}, Options{Reader: customServed{}})
	require.NoError(t, err)
	require.Equal(t, 3, doc.Bindings.Table().Documents(), "sanity: the binder reads two names")
	require.Len(t, doc.Others.List(), 1, "sanity: of one document")
	var unread []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.UnreadDialect {
			unread = append(unread, d)
		}
	}
	require.Len(t, unread, 1)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/$schema"}, unread[0].Provenance)
}

// TestReadDialects_ReadsNothingOfADocumentNoSourceEntryHolds pins that a
// document only a pass that did not stand read, which Document.Sources leaves
// out, draws no warning: its `$schema` governs nothing the result holds, and
// no entry would place the finding.
func TestReadDialects_ReadsNothingOfADocumentNoSourceEntryHolds(t *testing.T) {
	t.Parallel()
	reader := documentsReader{
		"/r/a.yaml": "{$schema: 'https://example.com/custom', $defs: {T: {type: integer}}}\n",
		"/r/z.yaml": "S: {$id: 'a.yaml', $defs: {T: {type: string}}}\n",
	}
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    A: {$ref: 'a.yaml#/$defs/T'}\n    Z: {$ref: 'z.yaml#/S'}\n",
	)}, Options{Reader: reader})
	require.NoError(t, err)
	table := doc.Bindings.Table()
	unreached := false
	for id := range bind.DocID(table.Documents()) {
		d := table.Document(id)
		unreached = unreached || d.Unreached && len(d.Dialects) == 1
	}
	require.True(t, unreached, "sanity: a.yaml is read, named, and left out")
	assert.False(t, openapitest.HasDiag(diags, diag.UnreadDialect))
}

// TestDialects_ANodeTwoRootsHoldReadsTheFirsts pins that a node two roots
// naming a dialect hold reads the rows of the first to govern it, which
// readDialects asks innermost first, and that each node is read once, however
// many aliases place it.
func TestDialects_ANodeTwoRootsHoldReadsTheFirsts(t *testing.T) {
	t.Parallel()
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("A: {x: &n {type: integer}, y: *n}\nB: {z: *n}\n"), &tree))
	root := tree.Content[0]
	a, b := root.Content[1], root.Content[3]
	first, _ := oas.Table().SchemaRules("3.1.0", draft04)
	second, _ := oas.Table().SchemaRules("3.1.0", "")
	d := Dialects{byNode: map[*yaml.Node]oas.SchemaRules{}}
	d.govern(a, first)
	d.govern(b, second)
	shared := a.Content[1]
	// Identity, not equality: two rows of one dialect are equal, and the claim
	// is whose rows the node holds.
	got, ok := d.Of(shared)
	require.True(t, ok)
	if got != first {
		t.Error("the shared node holds the second root's rows, not the first's")
	}
	got, ok = d.Of(b)
	require.True(t, ok)
	if got != second {
		t.Error("B holds another root's rows than its own")
	}
	assert.Len(t, d.byNode, 8, "A, its keys x and y, the shared node and its two scalars; B and its key z")
	d.govern(nil, first)
	assert.Len(t, d.byNode, 8, "no tree governs nothing")
}

// TestReadDialects_ANestedRootGovernsItsOwnTree pins that a resource an $id
// embeds in another root reads the dialect its own `$schema` names, the
// innermost root governing the nodes it holds, while the rest of the outer
// root reads the outer's.
func TestReadDialects_ANestedRootGovernsItsOwnTree(t *testing.T) {
	t.Parallel()
	doc, _, err := Load(t.Context(), 0, compilers.Source{Path: "/r/root.yaml", Data: []byte(
		"openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    T: {$schema: 'https://json-schema.org/draft/2020-12/schema', properties: {\n" +
			"      a: {$id: 'https://example.com/a', $schema: '" + draft04 + "', properties: {x: {type: integer}}},\n" +
			"      b: {type: integer}}}\n",
	)}, Options{})
	require.NoError(t, err)
	tSchema := doc.Doc.Components.GetSchemas().GetOrZero("T").GetSchema()
	a := tSchema.GetProperties().GetOrZero("a").GetSchema()
	boolean := func(s interface{ GetRootNode() *yaml.Node }) bool {
		r, ok := doc.Dialects.Of(s.GetRootNode())
		require.True(t, ok)
		return r.Takes("exclusiveMinimum", oas.Boolean)
	}
	assert.True(t, boolean(a), "the embedded root reads its own")
	assert.True(t, boolean(a.GetProperties().GetOrZero("x").GetSchema()), "and what it holds")
	assert.False(t, boolean(tSchema), "the outer root reads its own")
	assert.False(t, boolean(tSchema.GetProperties().GetOrZero("b").GetSchema()))
}

// TestLoad_TheSourceSharesItsRowsWithADocumentOfItsMinor pins that the load
// phase reads the source's Schema Object rows off the one table it judged the
// source against, as it reads each other document's: a document declaring the
// source's minor holds the same rows, by identity, and one of another minor
// rows of its own.
func TestLoad_TheSourceSharesItsRowsWithADocumentOfItsMinor(t *testing.T) {
	t.Parallel()
	declaring := func(version string) string {
		return "openapi: " + version + "\ninfo: {title: E, version: \"1\"}\npaths: {}\n" +
			"components: {schemas: {S: {type: string}}}\n"
	}
	dir := externalDir(t, map[string]string{"same.yaml": declaring("3.1.1"), "other.yaml": declaring("3.0.3")})
	doc, _ := loadExternal(t, dir, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n"+
		"  schemas:\n    A: {$ref: 'same.yaml#/components/schemas/S'}\n    B: {$ref: 'other.yaml#/components/schemas/S'}\n",
		Options{})
	rows := map[string]Rows{}
	for _, o := range doc.Others.List() {
		rows[o.Label] = o.Rows
	}
	require.Len(t, rows, 2, "both documents are read")
	if rows["same.yaml"] != doc.Rows {
		t.Error("the source's rows were built apart from those of a document of its minor")
	}
	assert.True(t, rows["other.yaml"].Schema.Defines("nullable"))
	assert.False(t, doc.Rows.Schema.Defines("nullable"))
}

// TestLoad_TheSourcesRowsAreThoseOfItsJSONSchemaDialect pins that the rows the
// load phase hands over for the source are in the dialect its jsonSchemaDialect
// names, shared, by identity, with a document of its minor naming the same one,
// and apart from those of one naming none.
func TestLoad_TheSourcesRowsAreThoseOfItsJSONSchemaDialect(t *testing.T) {
	t.Parallel()
	named := "jsonSchemaDialect: '" + draft04 + "'\n"
	declaring := func(dialect string) string {
		return "openapi: 3.1.1\ninfo: {title: E, version: \"1\"}\n" + dialect + "paths: {}\n" +
			"components: {schemas: {S: {type: string}}}\n"
	}
	dir := externalDir(t, map[string]string{"named.yaml": declaring(named), "none.yaml": declaring("")})
	doc, _ := loadExternal(t, dir, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"+named+"paths: {}\n"+
		"components:\n  schemas:\n    A: {$ref: 'named.yaml#/components/schemas/S'}\n"+
		"    B: {$ref: 'none.yaml#/components/schemas/S'}\n", Options{})
	rows := map[string]Rows{}
	for _, o := range doc.Others.List() {
		rows[o.Label] = o.Rows
	}
	require.Len(t, rows, 2, "both documents are read")
	assert.True(t, doc.Rows.Schema.Takes("exclusiveMinimum", oas.Boolean), "the source's rows are draft 4's")
	if rows["named.yaml"] != doc.Rows {
		t.Error("the source's rows were built apart from those of a document naming its dialect")
	}
	if rows["none.yaml"] == doc.Rows {
		t.Error("a document naming no dialect shares the rows of the source's")
	}
}
