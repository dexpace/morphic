package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
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
// every model the parser folds into a map: a path item, operation, response,
// callback or security requirement. The parser once skipped an entry whose
// value carried an anchor, taking it for an alias definition, so the entry
// reached the IR in no form and with no diagnostic. An anchor only names a
// node for reuse, so a document and its unanchored twin must compile to one IR.
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
// and over HTTP. The resolver parsed such a document itself, out of reach of
// the release of the source's anchors, and the same entries were skipped there
// in silence (GitHub #501).
//
// The two halves of a comparison differ in the external document's anchors
// alone: a file is named by one relative path from either directory, and a
// URL is served from one server, so the IDs qualified by the document agree.
// The paths and hashes, which differ, the comparison clears.
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
			anchored, twin := compileServed(t, ext, unanchored(ext))
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

// compileServed compiles, twice, a source whose one path is a reference to /x
// in a document served over HTTP: first, then second. One server serves both
// from one URL, so the two compiles name the document alike.
func compileServed(t *testing.T, first, second string) (*ir.Document, *ir.Document) {
	t.Helper()
	var served atomic.Pointer[string]
	served.Store(&first)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, err := w.Write([]byte(*served.Load()))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	a := compileReferring(t, "root.yaml", srv.URL+"/other.yaml")
	served.Store(&second)
	return a, compileReferring(t, "root.yaml", srv.URL+"/other.yaml")
}

// compileReferring compiles, with external references allowed, a source at
// path whose one path is a reference to /x in the document at uri. The source
// text names the document, so it differs between a file and a URL; the
// comparison is between documents compiled the same way, and the returned IR
// has the source's path and content hash cleared so a caller can compare two.
func compileReferring(t *testing.T, path, uri string) *ir.Document {
	t.Helper()
	src := anchorFixtureHead + "paths:\n  /x: {$ref: \"" + uri + "#/paths/~1x\"}\n"
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	out := withoutSourceHash(doc)
	for i := range out.Sources {
		out.Sources[i].Path = ""
	}
	return out
}

// TestCompile_ARecursiveAnchorInAnExternalDocumentIsRefused pins GitHub #536.
// A recursive anchor in an external document, on a folded entry or in a
// self-referencing schema, would build its model without end. Both are refused
// as the failure of the reference that named the document. That the compile
// returns at all, not only the diagnostic, is what the test proves.
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

// TestCompile_ARefusedExternalDocumentIsFetchedOnce pins that refusing an
// external document costs one fetch of it, however many references name it,
// while each of them still reports the refusal at its own $ref. The resolver
// keeps a document only once it has built something from it, so it asks for a
// refused one again on every reference.
func TestCompile_ARefusedExternalDocumentIsFetchedOnce(t *testing.T) {
	t.Parallel()
	const budget = 1 << 10
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, err := w.Write([]byte("openapi: 3.1.0\ninfo: {title: O, version: \"1\", description: " +
			strings.Repeat("x", budget) + "}\npaths: {}\ncomponents: {schemas: {A: {type: string}}}\n"))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	ref := `{$ref: "` + srv.URL + `/other.yaml#/components/schemas/A"}`
	src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    R1: " + ref + "\n    R2: " + ref + "\n    R3: " + ref + "\n"

	_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "root.yaml", Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true, Limits: openapi.Limits{MaxSourceBytes: budget}}})

	require.NoError(t, err)
	var refusedAt []jsontext.Pointer
	for _, d := range diags {
		if strings.Contains(d.Message, fmt.Sprintf("refused: it is past the %d-byte budget", budget)) {
			refusedAt = append(refusedAt, d.Provenance.Pointer)
		}
	}
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/R1", "/components/schemas/R2", "/components/schemas/R3"},
		refusedAt, "the refusal is reported at each reference: %+v", diags)
	assert.Equal(t, int32(1), fetches.Load(), "and the document fetched once, for three references")
}

// TestCompile_AnExternalDocumentIsParsedAsItWasJudged pins that no bytes reach
// the parser unjudged. A reference naming nothing in a document leaves it
// uncached, so the next reference into it asks for it again; and, under a $ref
// URL that net/url respells, the resolver misses the prepared tree and parses
// what it is handed itself (GitHub #538). It is handed the bytes the one read
// got and judged: a second request would have served a document past the node
// budget, from which nothing must be lowered.
func TestCompile_AnExternalDocumentIsParsedAsItWasJudged(t *testing.T) {
	t.Parallel()
	const budget = 100
	small := "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths:\n" +
		"  /x:\n    get: {operationId: FIRST, responses: {\"200\": {description: ok}}}\n"
	large := "openapi: 3.1.0\ninfo: {title: O, version: \"1\", x-pad: [" + strings.Repeat("1, ", budget) + "1]}\n" +
		"paths:\n  /x:\n    get: {operationId: SECOND, responses: {\"200\": {description: ok}}}\n"
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := large
		if fetches.Add(1) == 1 {
			body = small
		}
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	respelled := "HTTP://" + strings.TrimPrefix(srv.URL, "http://")
	src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a: {$ref: \"" + srv.URL + "/doc.yaml#/paths/~1missing\"}\n" +
		"  /b: {$ref: \"" + respelled + "/doc.yaml#/paths/~1x\"}\n"

	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "root.yaml", Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true, Limits: openapi.Limits{MaxSourceNodes: budget}}})

	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, int32(1), fetches.Load(), "one read answers both references")
	for _, d := range diags {
		assert.NotContains(t, d.Message, "node budget", "the document read was within it")
	}
	_, lowered := operationNamed(doc, "SECOND")
	assert.False(t, lowered, "nothing is lowered from bytes never judged")
	_, lowered = operationNamed(doc, "FIRST")
	assert.True(t, lowered, "what was judged is what /b mounts")
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

// operationNamed finds the first operation whose source operationId is name, in
// any group of any service.
func operationNamed(doc *ir.Document, name string) (ir.Operation, bool) {
	ops := operationsNamed(doc, name)
	if len(ops) == 0 {
		return ir.Operation{}, false
	}
	return ops[0], true
}
