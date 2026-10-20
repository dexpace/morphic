package load

import (
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
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// TestBound_ABuildAfterTheFirstChangesNothing pins that a bound builds its
// schemas once: the builder is spent by the first build, so a second leaves the
// bindings and what was built as they were, and a bound that never held a
// builder builds nothing.
func TestBound_ABuildAfterTheFirstChangesNothing(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, schemaRoot("#/components/schemas/B")+"    B: {type: string}\n")
	data := []byte(schemaRoot("#/components/schemas/B"))
	self := newSourceDocument("spec.yaml", data, root, nil, newSourceTree(0, overlay.Origin{}, root), nil)

	b := bindSchemas(t.Context(), pointerAt(0, overlay.Origin{}), doc, Siblings{}, self, nil)
	b.build(t.Context())
	first, built := b.bindings, b.built
	require.NotNil(t, first)

	b.build(t.Context())

	assert.Same(t, first, b.bindings, "the second build leaves the bindings")
	assert.Equal(t, built, b.built, "and what was built")
	var none bound
	none.build(t.Context())
	assert.Nil(t, none.bindings)
	assert.Empty(t, none.built)
}

// TestBindingDiags_ReportsTheBudgetOnce pins that a table whose work ran out is
// reported once, at the document, with the bound it crossed, and that a $ref it
// left unread is not reported unresolved: it was not found wanting.
func TestBindingDiags_ReportsTheBudgetOnce(t *testing.T) {
	t.Parallel()
	_, root := buildDoc(t, schemaRoot("#/components/schemas/B")+"    B: {type: string}\n")
	table := bind.Bind(root, bind.Config{Self: "spec.yaml", Limit: 1})
	require.True(t, table.Exhausted(), "sanity: one step is not enough to bind the document")

	diags := bindingDiags(pointerAt(0, overlay.Origin{}), table)

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Empty(t, diags[0].Provenance.Pointer, "at the document")
	assert.Contains(t, diags[0].Message, "takes more than 67108864 steps", "the bound crossed is the default")
}

// TestLoad_ASchemaRefIsUnresolvedWhereItsDocumentCannotBeRead pins what a
// schema $ref reports when the document it names cannot be read, by the reason
// the read gave: a URL that answers an error, a document that is no YAML, and a
// location the source's own URL gives no document for.
func TestLoad_ASchemaRefIsUnresolvedWhereItsDocumentCannotBeRead(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/garbage.yaml" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte("a: [\n"))
		assert.NoError(t, err)
	})
	dir := externalDir(t, map[string]string{"bad.yaml": "a: [\n"})
	for _, c := range []struct {
		name, path, ref, says string
	}{
		{"a URL answering an error", "", srv.URL + "/missing.yaml#/x", "fetching it answered 404"},
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
			"expand; a $ref past that, or naming a position past it, is left unresolved")}
	assert.Empty(t, cmp.Diff(want, bindingDiags(at, bind.Bind(&root, bind.Config{Self: "spec.yaml"}))))
}

// TestBuildFailures_ReportsALandingNoSchemaWasBuiltFor pins where a landing
// the decoder could not build is reported: at the least source position
// reaching it, with why, and not at all for one only another document's
// content reaches, or one that was built.
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
	assert.Empty(t, cmp.Diff(want, buildFailures(at, []typed.Built{failed, elsewhere, foreign, built})))
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
