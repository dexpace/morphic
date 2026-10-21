package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// TestBindingDiags_ReportsTheBudgetOnce pins that a table whose work ran out is
// reported once, at the document, with the bound it crossed, and that a $ref it
// left unread is not reported unresolved: it was not found wanting.
func TestBindingDiags_ReportsTheBudgetOnce(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("#/components/schemas/B")+"    B: {type: string}\n")
	table := bind.Bind(root, bind.Config{Self: "spec.yaml", Limit: 2})
	require.True(t, table.Exhausted(), "sanity: two steps are not enough to bind the document")

	diags := bindingDiags(pointerAt(0, overlay.Origin{}), table)

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
		assert.Equal(t, jsontext.Pointer("/paths/~1a/get/parameters/0"), site, "%T", model)
	}
}

// TestBindingDiags_NamesWhereAChainFailsAtEveryBudget pins the report of a
// source $ref whose chain fails in another document's content: at every
// limit the budget cuts the table at, it names the $ref that fails there and
// why, though the walk that would have found that $ref as a site never ran.
func TestBindingDiags_NamesWhereAChainFailsAtEveryBudget(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("other.yaml#/X")+"    B: {$ref: 'other.yaml#/P'}\n")
	var other yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("X: {$ref: '#/Y'}\nY: {$ref: '#/Missing'}\nP: {$ref: '#/X'}\n"), &other))
	cfg := bind.Config{Self: "spec.yaml", Open: func(string) (*yaml.Node, error) { return &other, nil }}
	full := bind.Bind(root, cfg)
	reported := 0
	for limit := 1; limit <= full.Steps(); limit++ {
		cfg.Limit = limit
		for _, d := range bindingDiags(pointerAt(0, overlay.Origin{}), bind.Bind(root, cfg)) {
			if d.Code != diag.UnresolvedRef {
				continue
			}
			reported++
			assert.Contains(t, d.Message, `its chain reaches #/Y in other.yaml, whose $ref "#/Missing" is unresolved: `+
				"it names nothing in the document it points into", "limit %d", limit)
		}
	}
	assert.Positive(t, reported, "some limit cut short of the full table reports the chain")
}

// TestBindingDiags_NamesALoopsMembersByTheirDocument pins how a loop's report
// names its members: a source position by its pointer, and another document's
// by its pointer in that document's name, as an unresolved $ref names one,
// never by the number the table reads that document under.
func TestBindingDiags_NamesALoopsMembersByTheirDocument(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("other.yaml#/X")+"    B: {$ref: '#/components/schemas/B'}\n")
	var other yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("X: {$ref: '#/Y'}\nY: {$ref: '#/X'}\n"), &other))
	table := bind.Bind(root, bind.Config{Self: "spec.yaml",
		Open: func(string) (*yaml.Node, error) { return &other, nil }})

	diags := bindingDiags(pointerAt(0, overlay.Origin{}), table)
	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, string(d.Provenance.Pointer)+": "+d.Message)
	}

	assert.Empty(t, cmp.Diff([]string{
		"/components/schemas/A: cyclic $ref: the chain of $refs through #/X in other.yaml, #/Y in other.yaml " +
			"comes back to where it started and never reaches a schema",
		"/components/schemas/B: cyclic $ref: the chain of $refs through #/components/schemas/B " +
			"comes back to where it started and never reaches a schema",
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
	table := bind.Bind(root, bind.Config{Self: "spec.yaml",
		Open: func(string) (*yaml.Node, error) { return &other, nil }})

	diags := bindingDiags(pointerAt(0, overlay.Origin{}), table)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/schemas/B"), diags[0].Provenance.Pointer)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

// TestLoad_ASchemaRefIsUnresolvedWhereItsDocumentCannotBeRead pins what a
// schema $ref reports when the document it names cannot be read, by the reason
// the read gave: a URL that answers an error, 300 being the least, a document
// that is no YAML, and a location the source's own URL gives no document for.
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
	for _, c := range []struct {
		name, path, ref, says string
	}{
		{"a URL answering an error", "", srv.URL + "/missing.yaml#/x", "fetching it answered 404"},
		{"a URL answering the least status past success", "", srv.URL + "/choices.yaml#/x", "fetching it answered 300"},
		{"a URL serving no YAML", "", srv.URL + "/garbage.yaml#/x", "it does not parse as YAML"},
		{"a file holding no YAML", "", "./bad.yaml#/x", "it does not parse as YAML"},
		{"a location the source's URL cannot be joined with", "https://example.com/api/spec.yaml", "%zz.yaml#/x",
			"it names no document a reference can"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := c.path
			if path == "" {
				path = dir + "/root.yaml"
			}
			_, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(schemaRoot(c.ref))},
				Options{AllowExternalRefs: true})

			require.NoError(t, err)
			var unresolved []ir.Diagnostic
			for _, d := range diags {
				if d.Code == diag.UnresolvedRef {
					unresolved = append(unresolved, d)
				}
			}
			require.Len(t, unresolved, 1, "%+v", diags)
			assert.Equal(t, "/components/schemas/A", string(unresolved[0].Provenance.Pointer))
			assert.Contains(t, unresolved[0].Message, "the document it names could not be read: ")
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
			"expand; a $ref past that, or one that reads a key past it, is left unresolved")}
	assert.Empty(t, cmp.Diff(want, bindingDiags(at, bind.Bind(&root, bind.Config{Self: "spec.yaml"}))))
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
	assert.Empty(t, cmp.Diff(want, buildFailures(at, []typed.Built{built, failed, foreign, elsewhere})))
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
