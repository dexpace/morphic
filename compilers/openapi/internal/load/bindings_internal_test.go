// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// TestBindSchemas_HoldsTheTableToMaxSteps pins the binder's bound where the
// load phase sets it: left unset, as the zero budget.Limit is, the table's work
// would be unbounded, and only a document built to multiply it would show.
func TestBindSchemas_HoldsTheTableToMaxSteps(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	assert.Equal(t, budget.AtMost(bind.MaxSteps), doc.Bindings.Table().Limit())
}

// TestBindingDiags_ReportsTheBudgetOnce pins that a table whose work ran out is
// reported once, at the document, with the bound it crossed, and that a $ref it
// left unread is not reported unresolved: it was not found wanting.
func TestBindingDiags_ReportsTheBudgetOnce(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("#/components/schemas/B")+"    B: {type: string}\n")
	table := bind.Bind(t.Context(), root, bind.Config{Self: "spec.yaml", Limit: budget.AtMost(2)})
	require.True(t, table.Exhausted(), "sanity: two steps are not enough to bind the document")

	diags := bindingDiags(inSource(pointerAt(0, overlay.Origin{})), table)

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Empty(t, diags[0].Provenance.Pointer, "at the document")
	assert.Contains(t, diags[0].Message, "takes more than 2 steps", "the bound the table crossed")
}

// TestBindSchemas_RecordsWhatAnUnplacedEndHolds pins the models in an end the
// table cannot place, here the first of a key written twice in raw YAML,
// which the parser resolved to and the binder reads as its last: each schema
// and discriminator is recorded as unplaced, under the least reference whose
// end it is, /a's though /b's resolves to it too.
func TestBindSchemas_RecordsWhatAnUnplacedEndHolds(t *testing.T) {
	t.Parallel()
	doc, _, err := Load(t.Context(), 0, openapitest.SourceOf(`openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {get: {parameters: [{$ref: '#/x-params/q'}], responses: {'200': {description: ok}}}}
  /b: {get: {parameters: [{$ref: '#/x-params/q'}], responses: {'200': {description: ok}}}}
x-params:
  q: {name: q, in: query, schema: {oneOf: [{type: string}], discriminator: {propertyName: k}}}
  q: {name: r, in: query}
`), Options{})
	require.NoError(t, err)
	op := doc.Doc.Paths.GetOrZero("/a").GetObject().Get()
	require.NotNil(t, op)
	js := op.GetParameters()[0].GetObject().GetSchema()
	require.NotNil(t, js)
	for _, model := range []any{js, js.GetSchema().GetDiscriminator()} {
		site, ok := doc.Bindings.Unplaced(model)
		assert.True(t, ok, "%T", model)
		assert.Equal(t, "/paths/~1a/get/parameters/0", site, "%T", model)
	}
}

// TestBindingDiags_ReportsAFailedRefOnceWhereItIsWrittenAtEveryBudget pins
// the report of a $ref failing in another document's content: at each limit
// the budget cuts the table at, it is reported at most once, at the $ref in
// that document and not at the source $refs whose chains reach it, and a
// table cut before the walk read it reports the budget instead.
func TestBindingDiags_ReportsAFailedRefOnceWhereItIsWrittenAtEveryBudget(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("other.yaml#/X")+"    B: {$ref: 'other.yaml#/P'}\n")
	var other yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("X: {$ref: '#/Y'}\nY: {$ref: '#/Missing'}\nP: {$ref: '#/X'}\n"), &other))
	cfg := bind.Config{Self: "spec.yaml", Open: func(string) (*yaml.Node, error) { return &other, nil }}
	full := bind.Bind(t.Context(), root, cfg)
	want := []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.UnresolvedRef, ir.Provenance{Source: 1, Pointer: "/Y"},
		`unresolved $ref "#/Missing": it names nothing in the document it points into`)}
	reported, silent := 0, 0
	for limit := 1; limit <= full.Steps(); limit++ {
		cfg.Limit = budget.AtMost(limit)
		table := bind.Bind(t.Context(), root, cfg)
		var failed []ir.Diagnostic
		crossed := false
		for _, d := range bindingDiags(byDocument, table) {
			switch d.Code {
			case diag.UnresolvedRef:
				failed = append(failed, d)
			case diag.BudgetExceeded:
				crossed = true
			}
		}
		assert.Equal(t, table.Exhausted(), crossed, "a cut table reports the budget, limit %d", limit)
		if len(failed) == 0 {
			silent++
			continue
		}
		reported++
		assert.Empty(t, cmp.Diff(want, failed), "limit %d", limit)
	}
	assert.Positive(t, reported, "some limit cut short of the full table reports the failure")
	assert.Positive(t, silent, "and some limit cuts the table before the walk that reads it")
}

// TestBindingDiags_NamesALoopsMembersByTheirDocument pins how a loop is
// reported: once, at its least member, in whichever document holds it, naming
// a member in another document than that one by its pointer in that document's
// name, never by the number the table reads it under. A $ref leading into a
// loop is not reported for it.
func TestBindingDiags_NamesALoopsMembersByTheirDocument(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("other.yaml#/X")+"    B: {$ref: '#/components/schemas/B'}\n"+
		"    C: {$ref: 'other.yaml#/P'}\n    D: {$ref: 'other.yaml#/Q'}\n")
	docs := map[string]string{
		"other.yaml": "X: {$ref: '#/Y'}\nY: {$ref: '#/X'}\nP: {$ref: 'spec.yaml#/components/schemas/C'}\n" +
			"Q: {$ref: 'third.yaml#/R'}\n",
		"third.yaml": "R: {$ref: 'other.yaml#/Q'}\n",
	}
	table := bind.Bind(t.Context(), root, bind.Config{Self: "spec.yaml", Open: func(name string) (*yaml.Node, error) {
		var node yaml.Node
		return &node, yaml.Unmarshal([]byte(docs[name]), &node)
	}})
	require.Equal(t, 3, table.Documents(), "sanity: the source and the two documents it leads into")

	diags := bindingDiags(byDocument, table)
	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, fmt.Sprintf("%d%s: %s", d.Provenance.Source, d.Provenance.Pointer, d.Message))
	}

	const rest = " comes back to where it started and never reaches a schema"
	assert.Empty(t, cmp.Diff([]string{
		"1/X: cyclic $ref: the chain of $refs through #/X, #/Y" + rest,
		"0/components/schemas/B: cyclic $ref: the chain of $refs through #/components/schemas/B" + rest,
		"0/components/schemas/C: cyclic $ref: the chain of $refs through #/components/schemas/C, #/P in other.yaml" + rest,
		"1/Q: cyclic $ref: the chain of $refs through #/Q, #/R in third.yaml" + rest,
	}, got))
}

// TestBindSchemas_HoldsEveryEndPastOneAlreadyWalked pins that the ends of
// reference objects are each held where the table places them, however many
// references before them resolved to one object already walked: R's schema,
// after the second reference to Q, is held at R, not recorded as unplaced.
func TestBindSchemas_HoldsEveryEndPastOneAlreadyWalked(t *testing.T) {
	t.Parallel()
	doc, _, err := Load(t.Context(), 0, openapitest.SourceOf(`openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {get: {parameters: [{$ref: '#/components/parameters/Q'}], responses: {'200': {description: ok}}}}
  /b:
    get:
      parameters: [{$ref: '#/components/parameters/Q'}, {$ref: '#/components/parameters/R'}]
      responses: {'200': {description: ok}}
components:
  parameters:
    Q: {name: q, in: query, schema: {type: string}}
    R: {name: r, in: query, schema: {type: integer}}
`), Options{})
	require.NoError(t, err)
	r, ok := doc.Doc.Components.GetParameters().Get("R")
	require.True(t, ok)
	js := r.GetObject().GetSchema()
	require.NotNil(t, js)
	_, unplaced := doc.Bindings.Unplaced(js)
	assert.False(t, unplaced)
	site, ok := doc.Bindings.Site(js)
	require.True(t, ok)
	assert.Equal(t, bind.Position{Pointer: "/components/parameters/R/schema"}, site)
}

// TestBindingDiags_ReportsAChainBackIntoTheSourceWhereItFails pins that a
// source $ref whose chain leaves the source and fails back in it is not
// reported for that failure: B's $ref is, where it is written.
func TestBindingDiags_ReportsAChainBackIntoTheSourceWhereItFails(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("other.yaml#/S")+"    B: {$ref: '#/components/schemas/Gone'}\n")
	var other yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("S: {$ref: 'spec.yaml#/components/schemas/B'}\n"), &other))
	table := bind.Bind(t.Context(), root, bind.Config{Self: "spec.yaml",
		Open: func(string) (*yaml.Node, error) { return &other, nil }})

	diags := bindingDiags(inSource(pointerAt(0, overlay.Origin{})), table)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/schemas/B"), diags[0].Provenance.Pointer)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

// TestExternal_DocumentReportsAReadWithNoTreeAsAFault pins the invariant
// document holds the reads to: a document read without error has a tree
// prepared for it, so one with none is the compiler's fault, said so.
func TestExternal_DocumentReportsAReadWithNoTreeAsAFault(t *testing.T) {
	t.Parallel()
	ext := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: &fakeReader{}}, newExternalReads(sourceDocument{}, budget.Unbounded()))
	ext.read.once("doc.yaml", func() answer { return answer{body: []byte("a: 1\n")} })

	tree, err := ext.document(t.Context(), "doc.yaml")

	assert.Nil(t, tree)
	require.EqualError(t, err, "internal: no tree was prepared for doc.yaml")
}

// TestLoad_ASchemaRefIsUnresolvedWhereItsDocumentCannotBeRead pins what a
// schema $ref reports when the document it names cannot be read, by the reason
// the read gave: a URL that answers an error, 300 being the least, and a
// location the source's own URL, or its own path, gives no document for; and
// when it was read and is no YAML, by the parser's reason, its line included.
func TestLoad_ASchemaRefIsUnresolvedWhereItsDocumentCannotBeRead(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/garbage.yaml":
			_, err := w.Write([]byte("a: [\n"))
			assert.NoError(t, err)
		case "/choices.yaml":
			w.WriteHeader(http.StatusMultipleChoices)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	dir := externalDir(t, map[string]string{"bad.yaml": "a: [\n"})
	const unread, unparsed = "the document it names could not be read: ", "the document it names does not parse as YAML: yaml: line 1: "
	for _, c := range []struct {
		name, path, ref, phrase, says string
	}{
		{"a URL answering an error", "", srv.URL + "/missing.yaml#/x", unread, "404 Not Found"},
		{"a URL answering the least status past success", "", srv.URL + "/choices.yaml#/x", unread, "300 Multiple Choices"},
		{"a URL serving no YAML", "", srv.URL + "/garbage.yaml#/x", unparsed, "did not find expected node content"},
		{"a file holding no YAML", "", "./bad.yaml#/x", unparsed, "did not find expected node content"},
		{"a location the source's URL cannot be joined with", "https://example.com/api/spec.yaml", "%zz.yaml#/x",
			unread, "it names no document a reference can"},
		{"a source whose path is no URL reference", "spec%zz.yaml", "./other.yaml#/x",
			unread, "it names no document a reference can"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := c.path
			if path == "" {
				path = dir + "/root.yaml"
			}
			_, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(schemaRoot(c.ref))},
				Options{Reader: reader.Disk{}})

			require.NoError(t, err)
			var unresolved []ir.Diagnostic
			for _, d := range diags {
				if d.Code == diag.UnresolvedRef {
					unresolved = append(unresolved, d)
				}
			}
			require.Len(t, unresolved, 1, "%+v", diags)
			assert.Equal(t, "/components/schemas/A", string(unresolved[0].Provenance.Pointer))
			assert.Contains(t, unresolved[0].Message, c.phrase)
			assert.Contains(t, unresolved[0].Message, c.says)
		})
	}
}

// TestBindingDiags_ReportsTheMergeBound pins the warning a table cut short by
// a merge chain past the bound draws, at the document, beside the report each
// $ref past it draws where the lowering meets it.
func TestBindingDiags_ReportsTheMergeBound(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 {$ref: '#/components/schemas/T'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    T: {type: string}\n    A: {<<: *c70}\n")
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(b.String()), &root))
	at := pointerAt(0, overlay.Origin{})

	want := []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.DegradedConstruct, at(""),
		"a mapping merges through `<<` deeper than 64 levels, which resolving the schema $refs does not "+
			"expand; a $ref past that, or one that reads a key past it, is left unresolved, and where that "+
			"mapping is a schema, no $dynamicRef in its document is expanded")}
	assert.Empty(t, cmp.Diff(want, bindingDiags(inSource(at), bind.Bind(t.Context(), &root, bind.Config{Self: "spec.yaml"}))))
}

// TestBuildFailures_ReportsALandingNoSchemaWasBuiltFor pins where a landing
// the decoder could not build is reported: at the least source position
// reaching it, with why, and not at all for one only another document's
// content reaches, or one that was built, whichever comes first.
func TestBuildFailures_ReportsALandingNoSchemaWasBuiltFor(t *testing.T) {
	t.Parallel()
	at := pointerAt(0, overlay.Origin{})
	failed := typed.Built{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/S"},
		Site: bind.Position{Pointer: "/components/schemas/A"}, Reached: true}, Err: errors.New("it failed")}
	elsewhere := typed.Built{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/S"}, Name: "other.yaml",
		Site: bind.Position{Pointer: "/components/schemas/B"}, Reached: true}, Err: errors.New("it failed")}
	foreign := failed
	foreign.Landing.Reached = false
	built := typed.Built{Landing: failed.Landing}

	want := []ir.Diagnostic{
		diag.Newf(ir.SeverityError, diag.Validation, at("/components/schemas/A"),
			"the $ref here reaches #/x-lib/S, where no schema could be built: it failed"),
		diag.Newf(ir.SeverityError, diag.Validation, at("/components/schemas/B"),
			"the $ref here reaches #/S in other.yaml, where no schema could be built: it failed"),
	}
	assert.Empty(t, cmp.Diff(want, buildFailures(inSource(at), []typed.Built{built, failed, foreign, elsewhere})))
}

// TestLoad_AMergeFanOutPastTheMergeBoundIsRefusedForItsAliases pins GitHub
// #1011: merges fanning out past the merge bound, each level merging the one
// below twice, are refused as the alias expansion they are, before anything
// reads a mapping through them. Read for $refs first, each mapping past the
// bound expanded again on every path to it, and the load never returned.
func TestLoad_AMergeFanOutPastTheMergeBoundIsRefusedForItsAliases(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n  m0: &m0 {type: object}\n")
	for i := 1; i <= 65; i++ {
		fmt.Fprintf(&b, "  m%d: &m%d {<<: [*m%d, *m%d], p%d: %d}\n", i, i, i-1, i-1, i, i)
	}
	b.WriteString("components:\n  schemas:\n    S: {properties: {x: *m65}}\n")

	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(b.String())}, Options{})

	require.NoError(t, err)
	assert.Nil(t, doc, "the document is refused")
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
}

// inSource places a finding at a position of a table that read the source
// alone: at's Provenance for its pointer.
func inSource(at func(jsontext.Pointer) ir.Provenance) func(bind.Position) ir.Provenance {
	return func(p bind.Position) ir.Provenance { return at(p.Pointer) }
}

// byDocument places a finding at a position of a table that read other
// documents: Source is the number the table reads the position's document
// under, which indexes Document.Sources in the order these tests read them.
func byDocument(p bind.Position) ir.Provenance {
	return ir.Provenance{Source: int(p.Doc), Pointer: p.Pointer}
}

// TestPositionOf_PlacesASiteInTheDocumentTheTableRead pins positionOf and
// siteIn: a source site is the source's position, another document's is found
// by the key a request reads a URL by (readKey), however the table or the site
// spells it, and one in a document the table never read is placed nowhere.
func TestPositionOf_PlacesASiteInTheDocumentTheTableRead(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"+
		"    A: {$ref: 'other.yaml#/T'}\n    B: {$ref: 'http://h/a%20b.yaml#/T'}\n    C: {$ref: 'http://h/c d.yaml#/T'}\n")
	table := bind.Bind(t.Context(), root, bind.Config{Self: "spec.yaml", Open: func(string) (*yaml.Node, error) {
		var n yaml.Node
		return &n, yaml.Unmarshal([]byte("T: {type: string}\n"), &n)
	}})
	require.Equal(t, 4, table.Documents(), "the source, other.yaml and the two URLs")
	other, remote, spaced := bind.DocID(1), bind.DocID(2), bind.DocID(3)
	require.Equal(t, "other.yaml", table.Document(other).Name)
	require.Equal(t, "http://h/a%20b.yaml", table.Document(remote).Name)
	require.Equal(t, "http://h/c d.yaml", table.Document(spaced).Name)
	self := sourceDocument{path: "spec.yaml"}

	for site, want := range map[refSite]bind.Position{
		{pointer: "/x"}:                             {Doc: bind.Source, Pointer: "/x"},
		{doc: "other.yaml", pointer: "/y"}:          {Doc: other, Pointer: "/y"},
		{doc: "http://h/a b.yaml", pointer: "/z"}:   {Doc: remote, Pointer: "/z"},
		self.siteIn("spec.yaml", "/w"):              {Doc: bind.Source, Pointer: "/w"},
		self.siteIn("http://h/a b.yaml", "/v"):      {Doc: remote, Pointer: "/v"},
		{doc: "http://h/c%20d.yaml", pointer: "/u"}: {Doc: spaced, Pointer: "/u"},
	} {
		got, ok := self.positionOf(table, site)
		require.True(t, ok, "%+v", site)
		assert.Equal(t, want, got, "%+v", site)
	}
	_, ok := self.positionOf(table, refSite{doc: "gone.yaml", pointer: "/x"})
	assert.False(t, ok, "a document the table never read")
}

// TestUnplaced_NamesTheDocumentOfASiteOutsideTheSource pins how unplaced
// spells the reference each schema of an end the table could not place was
// read under: by its pointer in the source, and by its pointer in its document
// elsewhere.
func TestUnplaced_NamesTheDocumentOfASiteOutsideTheSource(t *testing.T) {
	t.Parallel()
	inSource := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	inA := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	bd := typed.NewBuilder(nil)

	unplaced(t.Context(), bd, referenceEnd{walk: walkOf(&soa.ReferencedParameter{Object: &soa.Parameter{Schema: inSource}})},
		refSite{pointer: "/paths/~1x/get/parameters/0"})
	unplaced(t.Context(), bd, referenceEnd{walk: walkOf(&soa.ReferencedParameter{Object: &soa.Parameter{Schema: inA}})},
		refSite{doc: "a.yaml", pointer: "/components/parameters/P"})

	site, ok := bd.Bindings().Unplaced(inSource)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1x/get/parameters/0", site)
	site, ok = bd.Bindings().Unplaced(inA)
	require.True(t, ok)
	assert.Equal(t, "/components/parameters/P in a.yaml", site)
}

// TestPlacer_PlacesAFindingInTheDocumentThatHoldsIt pins where a finding at a
// position of the bindings' table is placed: the source's at its own
// Provenance, another document's in that document's Document.Sources entry,
// and one in a document no entry lists, which a load never produces, at the
// source's root rather than nowhere.
func TestPlacer_PlacesAFindingInTheDocumentThatHoldsIt(t *testing.T) {
	t.Parallel()
	doc, codes := loadReferencing(t, "./ext.yaml#/X")
	require.Empty(t, codes)
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: "/at" + p} }
	ext := bind.Position{Doc: 1, Pointer: "/X"}

	listed := bound{bindings: doc.Bindings, others: doc.Others}.placer(at)
	assert.Equal(t, ir.Provenance{Pointer: "/at/components"}, listed(bind.Position{Pointer: "/components"}))
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X"}, listed(ext))

	unlisted := bound{bindings: doc.Bindings}.placer(at)
	assert.Equal(t, ir.Provenance{Pointer: "/at"}, unlisted(ext))
}

// TestBindSchemas_AChainEndsInTheDocumentItEndsIn is TestTargetOf for schema
// $refs, which the bindings read: each chain ends in the document its last
// hop lands in, not the one it first leaves for, and a schema the model does
// not hold there is built for the landing, a boolean one included, and lends
// its use site that schema, whichever documents the chain passes.
func TestBindSchemas_AChainEndsInTheDocumentItEndsIn(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": reachedFixtureOther, "third.yaml": reachedFixtureThird})
	got, _ := loadExternal(t, dir, reachedFixtureRoot, Options{})
	doc, bindings := got.Doc, got.Bindings
	table := bindings.Table()
	for name, want := range map[string]struct {
		doc     string
		pointer jsontext.Pointer
	}{
		"S":       {filepath.Join(dir, "other.yaml"), "/components/schemas/Obj"},
		"Bool":    {filepath.Join(dir, "other.yaml"), "/components/schemas/AnyBool"},
		"Chained": {filepath.Join(dir, "third.yaml"), "/components/schemas/Leaf"},
		"ToS":     {filepath.Join(dir, "other.yaml"), "/components/schemas/Obj"},
		"ToLocal": {filepath.Join(dir, "root.yaml"), "/components/schemas/Local"},
	} {
		js, ok := doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		e, ok := bindings.Entry(js)
		require.True(t, ok, name)
		require.Equal(t, bind.Resolved, e.Chain.Status, name)
		assert.Equal(t, want.doc, table.Document(e.Chain.End.Doc).Name, name)
		assert.Equal(t, want.pointer, e.Chain.End.Pointer, name)
		held := bindings.At(e.Chain.End)
		assert.NotNil(t, held, "%s ends on a schema held for it", name)
		assert.Same(t, held, bindings.End(js), name)
	}
}

// TestIndexEnds_StopsBetweenEndsOnceTheContextIsDone pins indexEnds' context,
// asked before each end it places and each it records as unplaced: cancelled
// once the ends are placed, no unplaced end is walked.
func TestIndexEnds_StopsBetweenEndsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLDocument(t, minimal31)
	table := bind.Bind(t.Context(), root, bind.Config{Self: "spec.yaml"})
	walks := 0
	walk := func(context.Context) iter.Seq[soa.WalkItem] {
		walks++
		return func(func(soa.WalkItem) bool) {}
	}
	ends := map[refSite]referenceEnd{}
	for i := range 4 {
		ends[refSite{doc: "other.yaml", pointer: jsontext.Pointer(fmt.Sprintf("/x%d", i))}] = referenceEnd{
			object: &soa.ReferencedResponse{}, walk: walk}
	}
	self := sourceDocument{path: "spec.yaml"}
	indexEnds(t.Context(), typed.NewBuilder(table), table, ends, self)
	require.Equal(t, len(ends), walks, "live, each unplaced end is walked")

	walks = 0
	// The placing loop asks once for each end, all live, then the next ask refuses.
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: len(ends)}
	indexEnds(ctx, typed.NewBuilder(table), table, ends, self)

	assert.Zero(t, walks, "no unplaced end is walked once the context refused")
}
