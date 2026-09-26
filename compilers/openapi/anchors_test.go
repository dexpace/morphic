package openapi

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// anchorName matches the anchors the fixtures below write. None of them writes
// an `&` anywhere else.
var anchorName = regexp.MustCompile(`&[A-Za-z0-9_]+`)

// unanchored returns src with every anchor blanked to spaces of its own width,
// so each line and column of the twin is the anchored document's own and the
// two compile to documents whose provenance agrees.
func unanchored(src string) string {
	return anchorName.ReplaceAllStringFunc(src, func(a string) string { return strings.Repeat(" ", len(a)) })
}

// anchorFixtureHead opens every fixture: a document with one security scheme,
// so a requirement naming it has something to name.
const anchorFixtureHead = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
	"components:\n  securitySchemes:\n    key: {type: apiKey, in: header, name: X-Key}\n"

// anchoredEntries writes an anchored value at every model the parser folds into
// a map, each as the paths of a document with one path, /x. The path item case
// is the one the external twin cannot tell apart from its twin: a reference to
// a path item hands the resolver the item itself, which no map folds.
var anchoredEntries = []struct{ name, paths string }{
	{"a path item", `
  /x: &item
    get: {operationId: getX, responses: {"200": {description: ok}}}
`},
	{"an operation", `
  /x:
    get: &op
      operationId: getX
      responses: {"200": {description: ok}}
`},
	{"a response", `
  /x:
    get:
      operationId: getX
      responses:
        "200": {description: ok}
        "404": &nf {description: not found}
`},
	{"a callback expression", `
  /x:
    post:
      operationId: postX
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          "{$request.body#/url}": &cb
            post: {operationId: onEventPost, responses: {"200": {description: ok}}}
`},
	{"a security requirement", `
  /x:
    get:
      operationId: getX
      security:
        - key: &scopes []
      responses: {"200": {description: ok}}
`},
}

// TestCompile_AnAnchoredEntryCompilesAsItsUnanchoredTwin pins GitHub #459 at
// every model the parser folds into a map. The parser skipped such an entry
// when its value carried an anchor, taking it for an alias definition, and the
// entry reached the IR in no form and with no diagnostic: a whole path item, an
// operation, a response, a callback, or a security requirement — which came
// back empty, and an empty requirement is the one that admits a caller with no
// credentials at all. An anchor names a node for an alias to reuse and says
// nothing about the node itself, so a document and its unanchored twin are one
// document, and they must compile to one IR.
func TestCompile_AnAnchoredEntryCompilesAsItsUnanchoredTwin(t *testing.T) {
	t.Parallel()
	for _, tc := range anchoredEntries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := anchorFixtureHead + "paths:" + tc.paths
			require.True(t, anchorName.MatchString(src), "sanity: the fixture carries an anchor")

			anchored, diags := parseFull(t, src)
			openapitest.RequireNoErrorDiags(t, diags)
			twin, twinDiags := parseFull(t, unanchored(src))
			openapitest.RequireNoErrorDiags(t, twinDiags)

			if diff := cmp.Diff(withoutSourceHash(twin), withoutSourceHash(anchored)); diff != "" {
				t.Errorf("the anchored document compiled to other IR than its twin (-twin +anchored):\n%s", diff)
			}
		})
	}
}

// TestCompile_AnAnchoredEntryInAnExternalDocumentCompilesAsItsTwin extends the
// twin comparison to a document an external reference names, read from a file
// and over HTTP. The resolver parses such a document itself, so clearing the
// source's anchors never reached it, and the same entries were skipped there in
// silence (GitHub #501).
//
// The source is the same bytes in both halves of each comparison; only the
// external document differs, by its anchors.
func TestCompile_AnAnchoredEntryInAnExternalDocumentCompilesAsItsTwin(t *testing.T) {
	t.Parallel()
	for _, tc := range anchoredEntries {
		ext := "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths:" + tc.paths
		require.True(t, anchorName.MatchString(ext), "sanity: the fixture carries an anchor")

		t.Run(tc.name+" in a file", func(t *testing.T) {
			t.Parallel()
			anchored := compileBeside(t, ext)
			twin := compileBeside(t, unanchored(ext))
			if diff := cmp.Diff(twin, anchored); diff != "" {
				t.Errorf("the anchored document compiled to other IR than its twin (-twin +anchored):\n%s", diff)
			}
		})
		t.Run(tc.name+" over HTTP", func(t *testing.T) {
			t.Parallel()
			anchored := compileServed(t, ext)
			twin := compileServed(t, unanchored(ext))
			if diff := cmp.Diff(twin, anchored); diff != "" {
				t.Errorf("the anchored document compiled to other IR than its twin (-twin +anchored):\n%s", diff)
			}
		})
	}
}

// compileBeside compiles a source whose one path is a reference to /x in ext,
// written to a file beside it, and returns the IR with the source's path
// cleared: each call writes to a directory of its own.
func compileBeside(t *testing.T, ext string) *ir.Document {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(ext), 0o600))
	return compileReferring(t, filepath.Join(dir, "root.yaml"), "./other.yaml")
}

// compileServed compiles a source whose one path is a reference to /x in ext,
// served over HTTP, and returns the IR with the source's path cleared.
func compileServed(t *testing.T, ext string) *ir.Document {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, err := w.Write([]byte(ext))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return compileReferring(t, "root.yaml", srv.URL+"/other.yaml")
}

// compileReferring compiles, with external references allowed, a source at
// path whose one path is a reference to /x in the document at uri. The source
// text names the document, so it differs between a file and a URL; the
// comparison is between documents compiled the same way, and the returned IR
// has the source's path and content hash cleared so a caller can compare two.
func compileReferring(t *testing.T, path, uri string) *ir.Document {
	t.Helper()
	src := anchorFixtureHead + "paths:\n  /x: {$ref: \"" + uri + "#/paths/~1x\"}\n"
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(src)}},
		compilers.Options{FormatOptions: Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	out := withoutSourceHash(doc)
	for i := range out.Sources {
		out.Sources[i].Path = ""
	}
	return out
}

// compileBesideExpectingErrors compiles root, written beside ext, with
// external references allowed, and returns the diagnostics instead of
// requiring none: the twin comparisons above need a clean compile, and a
// refused external document needs the diagnostic itself.
func compileBesideExpectingErrors(t *testing.T, ext, root string) []ir.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(ext), 0o600))
	_, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{FormatOptions: Options{AllowExternalRefs: true}})
	require.NoError(t, err, "a refused external document is a diagnostic, not a Go error")
	return diags
}

// TestCompile_ARecursiveAnchorInAnExternalDocumentIsRefused pins GitHub #536.
// The skip that let #501 drop an anchored entry in silence also kept the
// parser off a recursive anchor there; releasing the name removes that skip,
// so a folded entry whose anchor recurses would build its model without end.
// A recursive anchor elsewhere in an external document — a self-referencing
// schema — already ran a compile out of memory on main for the same reason.
// Both are refused instead, as the failure of the reference that named the
// document, and the compile returns either way: that it returns at all, under
// a timeout, is what this test exists to prove, not only the diagnostic.
func TestCompile_ARecursiveAnchorInAnExternalDocumentIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, ext, root string }{
		{
			name: "a folded entry",
			ext: "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths:\n  /x:\n" +
				"    get: &g\n      operationId: getX\n      responses: {\"200\": {description: ok}}\n" +
				"      callbacks:\n        onEvent:\n          \"{$request.body#/url}\":\n            get: *g\n",
			root: "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
				"paths:\n  /x: {$ref: \"./other.yaml#/paths/~1x\"}\n",
		},
		{
			name: "a schema",
			ext: "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
				"    A: &a {type: object, properties: {self: *a}}\n",
			root: "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
				"components:\n  schemas:\n    Ext: {$ref: \"./other.yaml#/components/schemas/A\"}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := compileBesideExpectingErrors(t, tc.ext, tc.root)

			found := false
			for _, d := range diags {
				if d.Code == diag.UnresolvedRef && d.Severity == ir.SeverityError &&
					strings.Contains(d.Message, "refused") && strings.Contains(d.Message, "recursive YAML anchor") {
					found = true
				}
			}
			assert.True(t, found, "no unresolved-ref diagnostic named the refusal: %+v", diags)
		})
	}
}

// TestCompile_AnAliasStillStandsForAReleasedAnchorInAnExternalDocument is the
// external analogue of TestCompile_AnAliasStillStandsForAReleasedAnchor: a twin
// comparison has no alias to resolve, so this pins the half it cannot reach.
// Two operations in the external document share one anchored 404 through *nf,
// and the root mounts the path holding both by reference.
func TestCompile_AnAliasStillStandsForAReleasedAnchorInAnExternalDocument(t *testing.T) {
	t.Parallel()
	ext := "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths:\n  /x:\n" +
		"    get:\n      operationId: getX\n      responses:\n        \"200\": {description: ok}\n" +
		"        \"404\": &nf {description: SHARED_NOT_FOUND}\n" +
		"    put:\n      operationId: putX\n      responses:\n        \"200\": {description: ok}\n" +
		"        \"404\": *nf\n"

	doc := compileBeside(t, ext)

	for _, name := range []string{"getX", "putX"} {
		op, ok := operationNamed(doc, name)
		require.True(t, ok, "%s is lowered", name)
		lowered, err := json.Marshal(op)
		require.NoError(t, err)
		assert.Contains(t, string(lowered), "SHARED_NOT_FOUND", "%s carries the anchored 404", name)
	}
}

// TestCompile_AnAliasStillStandsForAReleasedAnchor pins the half the twin
// comparison cannot: a twin has no alias to resolve. The anchor's name is
// cleared before the model is built, and an alias reaches its target through
// the pointer the YAML parser resolved it to, so the alias still stands for the
// anchored response — here, both operations carry the one 404 it names.
func TestCompile_AnAliasStillStandsForAReleasedAnchor(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, anchorFixtureHead+`paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {description: ok}
        "404": &nf {description: SHARED_NOT_FOUND}
  /b:
    get:
      operationId: getB
      responses:
        "200": {description: ok}
        "404": *nf
`)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"getA", "getB"} {
		op, ok := operationNamed(doc, name)
		require.True(t, ok, "%s is lowered", name)
		lowered, err := json.Marshal(op)
		require.NoError(t, err)
		assert.Contains(t, string(lowered), "SHARED_NOT_FOUND", "%s carries the anchored 404", name)
	}
}

// withoutSourceHash returns doc with each source's content hash cleared: the
// one field a document and its twin legitimately differ in, since their bytes
// do.
func withoutSourceHash(doc *ir.Document) *ir.Document {
	out := *doc
	out.Sources = make([]ir.SourceInfo, len(doc.Sources))
	for i, s := range doc.Sources {
		s.Hash = ""
		out.Sources[i] = s
	}
	return &out
}

// operationNamed finds the operation whose source operationId is name, in any
// group of any service.
func operationNamed(doc *ir.Document, name string) (ir.Operation, bool) {
	groups := make([]ir.OperationGroup, 0, len(doc.Services))
	for _, svc := range doc.Services {
		groups = append(groups, svc.Groups...)
	}
	for len(groups) > 0 {
		g := groups[0]
		groups = append(groups[1:], g.Groups...)
		for _, op := range g.Operations {
			if op.Name.Source == name {
				return op, true
			}
		}
	}
	return ir.Operation{}, false
}
