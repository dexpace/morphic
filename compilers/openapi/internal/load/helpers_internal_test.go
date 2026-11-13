// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// buildTree runs a source through the steps the load phase runs before
// binding: decode, release anchors, populate the model. It returns the raw
// tree, so a test can drive one stage directly instead of paying for the whole
// Load pipeline.
func buildTree(t *testing.T, spec string) *yaml.Node {
	t.Helper()
	parsed, err := parsedFor(t.Context(), compilers.Source{Path: "spec.yaml", Data: []byte(spec)})
	require.NoError(t, err)
	readyForModel(t.Context(), parsed.root)
	doc, err := unmarshal(t.Context(), []byte(spec), parsed.root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return parsed.root
}

// mappingSpec is a source whose component schemas are schemas, and whose
// other top-level entries are rest, which follows them.
func mappingSpec(schemas, rest string) string {
	return openapitest.ComponentSpec(schemas) + rest
}

// petMapping is a Pet base whose discriminator maps each tag to its target.
func petMapping(mapping string) string {
	return "    Pet:\n      type: object\n      discriminator: {propertyName: k, mapping: {" + mapping + "}}\n"
}

// loadTargets loads spec at path, external references allowed or not, and
// requires it to load.
func loadTargets(t *testing.T, path, spec string, external bool) (*Document, []ir.Diagnostic) {
	t.Helper()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(spec)},
		readingIf(external))
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// readingIf returns the Options of a compile that reads the documents its
// references name from disk when external is set, and reads none otherwise.
func readingIf(external bool) Options {
	if external {
		return Options{Reader: reader.Disk{}}
	}
	return Options{}
}

// parserPanicSpec is a document the parser accepts and faults on building what
// a reference reaches: `B: {$ref}` is a mapping whose $ref key carries no
// value, so building a response from it nil-dereferences inside the parser.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const parserPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

// countErrorsAt counts the error-severity diagnostics carrying code. Severity is
// fixed rather than a parameter because every refusal this package reports is an
// error, and a lower severity here would be a different assertion than any test
// makes.
func countErrorsAt(diags []ir.Diagnostic, code string) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			n++
		}
	}
	return n
}

// diagLines renders diags as "pointer code", sorted, which is what an order
// comparison and an expectation both need.
func diagLines(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Provenance.Pointer)+" "+d.Code)
	}
	slices.Sort(out)
	return out
}

// response200 returns the 200 response the get operation at path names.
func response200(t *testing.T, doc *soa.OpenAPI, path string) *soa.ReferencedResponse {
	t.Helper()
	item, ok := doc.Paths.Get(path)
	require.True(t, ok, path)
	resp, ok := item.GetObject().Get().GetResponses().Get("200")
	require.True(t, ok, path)
	return resp
}

// markerPosition returns the line and column, both counted from 1, at which
// marker starts in spec. The marker must occur once, so a fixture edited to
// repeat it fails here rather than naming the wrong node.
func markerPosition(t *testing.T, spec, marker string) (line, col int) {
	t.Helper()
	require.Equal(t, 1, strings.Count(spec, marker), "the marker %q must occur once in the fixture", marker)
	at := strings.Index(spec, marker)
	return 1 + strings.Count(spec[:at], "\n"), at - strings.LastIndex(spec[:at], "\n")
}

// modelOf builds spec's model the way build does before binding it.
func modelOf(t *testing.T, spec string) *soa.OpenAPI {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(t.Context(), data)
	require.NoError(t, err)
	readyForModel(t.Context(), root)
	doc, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	return doc
}

// decodeRoot decodes src, which must decode.
func decodeRoot(t *testing.T, src string) *yaml.Node {
	t.Helper()
	parsed, err := Decode(t.Context(), []byte(src))
	require.NoError(t, err)
	return parsed.Root()
}

// rootReferencing is a minimal source document whose one path is a reference to
// /x in the document at url.
func rootReferencing(url string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x: {$ref: \"" + url + "#/paths/~1x\"}\n"
}

// anchoredExternalDoc anchors both of /x's operations: the parser folds a path
// item's methods into a map and skips an entry whose value carries an anchor,
// so a document read without this compiler's own anchor release drops both.
const anchoredExternalDoc = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get: &g
      operationId: EXTGET
      responses: {"200": {description: ok}}
    put: &p
      operationId: EXTPUT
      responses: {"200": {description: ok}}
`

// assertRecoveredAt requires that the path item at path of loaded, read
// through its bindings, carries both of anchoredExternalDoc's anchored
// operations: the outcome that fails silently, an empty or half-populated path
// item, where a document was read without the anchor release.
func assertRecoveredAt(t *testing.T, loaded *Document, path string) {
	t.Helper()
	ref, ok := loaded.Doc.Paths.Get(path)
	require.True(t, ok, "%s is in the document", path)
	item := refscope.Object[soa.PathItem](loaded.Bindings, ref)
	require.NotNil(t, item, "%s's reference resolved to a path item", path)
	get := item.Get()
	require.NotNil(t, get, "the anchored GET entry was folded, not skipped")
	assert.Equal(t, "EXTGET", get.GetOperationID())
	put := item.Put()
	require.NotNil(t, put, "the anchored PUT entry was folded, not skipped")
	assert.Equal(t, "EXTPUT", put.GetOperationID())
}

// countingServer starts an httptest.Server serving body to every request, and
// returns it with a counter of how many requests it received.
func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

// writeFile writes content to name under dir, failing the test on error.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}
