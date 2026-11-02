// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// buildDoc runs a source through the steps build runs before resolving:
// decode, release anchors, populate the model. It returns the populated
// document and the raw tree, so a test can drive one stage directly instead
// of paying for the whole Load pipeline.
func buildDoc(t *testing.T, spec string) (*soa.OpenAPI, *yaml.Node) {
	t.Helper()
	parsed, err := parsedFor(t.Context(), compilers.Source{Path: "spec.yaml", Data: []byte(spec)})
	require.NoError(t, err)
	releaseAnchors(t.Context(), parsed.root)
	doc, err := unmarshal(t.Context(), []byte(spec), parsed.root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, parsed.root
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

// resolverPanicSpec is a document the parser accepts and the resolver faults on:
// `B: {$ref}` is a mapping whose $ref key carries no value, so populating the
// response reference that points at it nil-dereferences inside speakeasy.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const resolverPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

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

// resolveSpecWith is resolveSpec at root.yaml with a caller-supplied rebuild, for
// the tests that need to observe or fail whether it is called.
func resolveSpecWith(t *testing.T, spec string, rebuild func() (*soa.OpenAPI, error)) (*soa.OpenAPI, []ir.Diagnostic, error) {
	t.Helper()
	resolved, _, _, pass, missed, err := resolveExternal(t.Context(), modelOf(t, spec),
		Siblings{}, MediaTypes{}, sourceDocument{path: "root.yaml"}, Options{Reader: reader.Disk{}}, rebuild)
	return resolved, passDiags(pointerAt(0, overlay.Origin{}), pass, missed), err
}

// respelled returns url with its scheme spelled scheme, which net/url respells
// back to "http" in the request built from it.
func respelled(url, scheme string) string {
	return scheme + "://" + strings.TrimPrefix(url, "http://")
}

// respellings is the set of URL spellings net/url respells relative to how
// http.NewRequest's caller wrote them (GitHub #538): the resolver keys a
// document by the reference's absolute URL as written, while the reader stores
// it under req.URL.String().
//
// A literal space in the path is absent: url.Parse and http.NewRequest
// normalize it and its %20 escape to one request key, so the two spellings
// never produce different keys. An empty port is covered by TestPreparedFor,
// since a test server cannot listen on the default port.
var respellings = []struct {
	name  string
	spell func(canonical string) string
}{
	{"upper-case scheme", func(c string) string {
		return "HTTP://" + strings.TrimPrefix(c, "http://")
	}},
	{"mixed-case scheme", func(c string) string {
		return "Http://" + strings.TrimPrefix(c, "http://")
	}},
	{"escaped userinfo", func(c string) string {
		return strings.Replace(c, "http://", "http://us%65r@", 1)
	}},
}
