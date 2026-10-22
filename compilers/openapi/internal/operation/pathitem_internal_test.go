package operation

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestApplyPathServers_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	op := &ir.Operation{}
	diags := applyPathServers(l.ctx, onOperation(op),
		&soa.PathItem{Servers: []*soa.Server{{URL: "https://x"}}}, "/paths/~1a", owners{}.cedes(0))
	assert.Nil(t, op.Unmodeled, "servers with no raw node are not preserved")
	assert.Empty(t, diags)
}

// TestApplyPathItemDocs_WithoutRootNode is the documentation counterpart of the
// two tests above: a declared pair whose source node cannot be read keeps
// nothing, and announces nothing it did not keep.
//
// The input is synthetic, but the branch is not hypothetical — RawChildNode does
// not expand a merge key, so a real document supplying the pair through one
// reaches here with GetSummary non-empty and loses it silently (GitHub #384).
// This pins the current behaviour rather than endorsing it; when #384 makes the
// lookup view-aware, what changes is the node this receives, not this contract.
func TestApplyPathItemDocs_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	summary, description := "documented", "at length"
	op := &ir.Operation{}

	diags := applyPathItemDocs(l.ctx, onOperation(op),
		&soa.PathItem{Summary: &summary, Description: &description}, "/paths/~1a", owners{}.cedes(0))

	assert.Nil(t, op.Unmodeled, "documentation with no raw node is not preserved")
	assert.Empty(t, diags)
}

// TestPathOperations_NilAdditionalOperationSkipped pins the guard on the
// additionalOperations walk. The parser never yields a nil entry, but the map is
// a plain pointer map and lowerOperation reads the operation's fields directly,
// so a nil would panic rather than lower.
func TestPathOperations_NilAdditionalOperationSkipped(t *testing.T) {
	t.Parallel()
	pi := &soa.PathItem{
		AdditionalOperations: sequencedmap.New(
			sequencedmap.NewElem("EMPTY", (*soa.Operation)(nil)),
			sequencedmap.NewElem("PURGE", &soa.Operation{}),
		),
	}

	ops := pathOperations(pi)

	require.Len(t, ops, 1, "the nil entry is skipped and the real one is not")
	assert.Equal(t, "PURGE", ops[0].method)
	assert.Equal(t, jsontext.Pointer("/additionalOperations/PURGE"), ops[0].seg)
}

// httpMethodsNames is the plain-string projection of httpMethods, which
// TestHTTPMethods_AgreesWithLibraryVocabulary compares against
// soa.IsStandardMethod. httpMethods itself carries a func field, so it cannot
// be a slices.Contains argument directly.
func httpMethodsNames() []string {
	names := make([]string, len(httpMethods))
	for i, m := range httpMethods {
		names[i] = m.name
	}
	return names
}

// TestHTTPMethods_AgreesWithLibraryVocabulary holds httpMethods to
// soa.IsStandardMethod: they must agree on every name either holds an opinion
// about. If the library learns a method before this compiler does,
// undeclaredPathItemKeys grades the key as declared, so no field lowers it and
// no diagnostic reports it (GitHub #413); this turns that disagreement into a
// build failure.
//
// The library exports no list to compare against, so this probes a candidate
// set: the methods RFC 9110 §9.3 defines, plus QUERY and PATCH, plus every name
// httpMethods declares, each in both letter cases because IsStandardMethod
// compares case-sensitively and a mismatch would hide a real gap.
func TestHTTPMethods_AgreesWithLibraryVocabulary(t *testing.T) {
	t.Parallel()

	rfc9110 := []string{
		"GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE",
		"QUERY", "PATCH",
	}
	methods := httpMethodsNames()
	candidates := make([]string, 0, len(rfc9110)+len(methods))
	candidates = append(candidates, rfc9110...)
	candidates = append(candidates, methods...)

	seen := make(map[string]bool, 2*len(candidates))
	for _, c := range candidates {
		seen[strings.ToUpper(c)] = true
		seen[strings.ToLower(c)] = true
	}
	names := slices.Sorted(maps.Keys(seen))

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, soa.IsStandardMethod(name), slices.Contains(httpMethodsNames(), name),
				"%q: soa.IsStandardMethod and httpMethods disagree on whether this is a "+
					"standard HTTP method; one vocabulary fell behind the other", name)
		})
	}
}

// TestPathItemFields_MatchTheLibraryModel holds pathItemFields to the key tags
// of the library's Path Item core model. The list decides which raw keys the
// census leaves alone as declared fields, so a field the library adds and the
// list lacks would be announced as a key the document was not permitted to
// write, and one the list names and the library dropped would escape the
// census.
//
// The extensions field is left out: its tag is "extensions", but the document
// reaches it by x- prefix, which pathItemDeclares tests for on its own.
func TestPathItemFields_MatchTheLibraryModel(t *testing.T) {
	t.Parallel()
	var declared []string
	typ := reflect.TypeFor[core.PathItem]()
	for field := range typ.Fields() {
		key := field.Tag.Get("key")
		if key == "" || key == "extensions" {
			continue
		}
		declared = append(declared, key)
	}
	diff := cmp.Diff(declared, pathItemFields)
	assert.Empty(t, diff,
		"pathItemFields must name every keyed field of core.PathItem, once each, in its order (-model +listed)")
}

// TestPathItemDeclares_AnchoredDeclaredKeysAreNotUndeclared pins that declared
// keys written with anchored values are not reported as undeclared. The library
// skips every anchored entry it would fold into a path item's operations map
// (docs/parser-library.md#anchored-entry-dropped), so the raw mapping presents
// a declared key as it presents an undeclared one, and only the vocabulary
// tells them apart.
//
// The item is unmarshalled through the library directly, the one way left to
// reach this shape: anchors are cleared before any model is built (GitHub #459,
// #501, #538). The anchored `get` is not lowered there, a loss this census
// cannot answer.
func TestPathItemDeclares_AnchoredDeclaredKeysAreNotUndeclared(t *testing.T) {
	t.Parallel()
	pi := pathItemOf(t, `
summary: &s s
description: &d d
servers: &sv [{url: 'https://a.example'}]
parameters: &pa [{name: p, in: query, schema: {type: string}}]
additionalOperations: &ao
  PURGE: {operationId: purgeY, responses: {"200": {description: ok}}}
x-mark: &xm XVAL
get: &g {operationId: getY, responses: {"200": {description: ok}}}
put: {operationId: putY, responses: {"200": {description: ok}}}
bogus: &b {responses: {"200": {description: BOGUS}}}
`)
	assert.Equal(t, []string{"bogus"}, undeclaredPathItemKeys(pi),
		"every declared key is left alone whether or not the map holds it; the undeclared one is not")
}

// pathItemOf unmarshals src as a Path Item Object through the library, the way
// the compiler reads one, so the folded map and the raw node are both what a
// document produces.
func pathItemOf(t *testing.T, src string) *soa.PathItem {
	t.Helper()
	pi := &soa.PathItem{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(src), pi)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return pi
}
