// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

const (
	// head32 is a 3.2 document's head.
	head32 = "openapi: 3.2.0\ninfo: {title: t, version: \"1\"}\n"
	// selfURL is the $self the documents below write.
	selfURL = "https://example.com/api/openapi.yaml"
	// onNetwork is the stable part of the refusal of a URL a read would
	// reach over the network.
	onNetwork = "it is on the network, which a read reaches only when external references are allowed"
)

// selfSpec is a 3.2 document declaring selfURL, whose schemas B, U and V
// name B by the $self, and by a relative path that the $self resolves to the
// document itself.
const selfSpec = "openapi: 3.2.0\n$self: " + selfURL + "\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
	"    B: {type: string}\n    U: {$ref: '" + selfURL + "#/components/schemas/B'}\n" +
	"    V: {$ref: 'openapi.yaml#/components/schemas/B'}\n"

// TestCompile_ASelfURIIsTheNameOfTheDocumentAndItsBase holds a reference that
// names the source by its $self, in full or by a relative path the $self
// resolves, to the source's own schema with no read at all, whether external
// references are refused or allowed.
func TestCompile_ASelfURIIsTheNameOfTheDocumentAndItsBase(t *testing.T) {
	t.Parallel()
	for name, allow := range map[string]bool{"refused": false, "allowed": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingReader{inner: failingReader{}}

			doc, diags := compileWithReader(t, "api/openapi.yaml", selfSpec, openapi.Options{AllowExternalRefs: allow}, rec)

			assertNoErrorDiags(t, diags)
			assert.Empty(t, rec.requests(), "the document names itself, so nothing is read")
			assert.Equal(t, componentID("B"), useTarget(t, doc, "U"))
			assert.Equal(t, componentID("B"), useTarget(t, doc, "V"))
		})
	}
}

// TestCompile_ARelativeReferenceUnderAnHTTPSelfNamesAURL holds a reference to a
// document beside the source to the URL the $self puts it at: refused, it is an
// error that names the locate option; allowed and served, it is read from the
// URL and lists it in Document.Sources.
func TestCompile_ARelativeReferenceUnderAnHTTPSelfNamesAURL(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.2.0\n$self: " + selfURL + "\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    S: {$ref: 'other.yaml#/OS'}\n"
	dir := t.TempDir()
	layout(t, dir, map[string]string{"api/other.yaml": "OS: {type: object}\n"})

	_, diags := compileWithReader(t, filepath.Join(dir, "api", "openapi.yaml"), src, openapi.Options{}, &recordingReader{inner: loopbackDisk{}})

	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/S", string(bad[0].Provenance.Pointer))
	assert.Contains(t, bad[0].Message, "external document https://example.com/api/other.yaml refused: "+onNetwork+
		"; a locate option maps its URL to a location")

	srv := serveDocs(t, map[string]string{"/api/other.yaml": "OS: {type: object}\n"})
	rd := hostMapReader{inner: loopbackDisk{}, routes: map[string]string{"https://example.com/": srv.URL + "/"}}

	doc, diags := compileWithReader(t, "api/openapi.yaml", src, openapi.Options{AllowExternalRefs: true}, rd)

	assertNoErrorDiags(t, diags)
	assert.Equal(t, []string{"api/openapi.yaml", "https://example.com/api/other.yaml"}, sourcePaths(doc))
	assert.Equal(t, []string{"/api/other.yaml"}, srv.requests())
}

// TestCompile_ASelfURIMovesNoFetchedDocumentOntoAFile holds a fetched document
// writing a $self of a file: URI to the judgement of the location it was read
// from: a reference beside it names a file, which a document read over the
// network reaches by no route, so it is refused and the only read is the
// document itself.
func TestCompile_ASelfURIMovesNoFetchedDocumentOntoAFile(t *testing.T) {
	t.Parallel()
	srv := serveDocs(t, map[string]string{"/far.yaml": "openapi: 3.2.0\n$self: 'file:///etc/'\ninfo: {title: o, version: \"1\"}\n" +
		"paths: {}\ncomponents:\n  schemas:\n    P: {$ref: 'passwd#/x'}\n"})
	rec := &recordingReader{inner: loopbackDisk{}}
	src := head31 + "paths: {}\ncomponents:\n  schemas:\n    Q: {$ref: '" + srv.URL + "/far.yaml#/components/schemas/P'}\n"

	_, diags := compileWithReader(t, "spec.yaml", src, openapi.Options{AllowExternalRefs: true}, rec)

	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Equal(t, 1, bad[0].Provenance.Source, "in far.yaml's entry")
	assert.Contains(t, bad[0].Message, reftable.OffNetwork)
	assert.Equal(t, []string{srv.URL + "/far.yaml"}, rec.locations())
}

// TestCompile_ARelativeSelfIsResolvedAgainstWhereTheDocumentWasRead holds a
// relative $self to the directory its document lies in and not to the working
// directory: reads the document's references make move with the $self, a
// response's as a schema's, as GitHub #863 has it, and land on the same file
// whichever directory the compile runs from.
func TestCompile_ARelativeSelfIsResolvedAgainstWhereTheDocumentWasRead(t *testing.T) {
	root := realDir(t)
	layout(t, root, map[string]string{
		"res/spec.yaml": "openapi: 3.2.0\n$self: sub/x.yaml\ninfo: {title: t, version: \"1\"}\n" +
			"paths:\n  /a:\n    get:\n      operationId: a\n      responses:\n        \"200\": {$ref: \"../other.yaml#/R\"}\n" +
			"components:\n  schemas:\n    S: {$ref: '../other.yaml#/OS'}\n",
		"res/other.yaml": "OS: {type: object}\nR: {description: r}\n",
	})
	irs := make([]string, 0, 2)
	for name, run := range map[string]struct{ dir, spec string }{
		"from the parent": {root, "res/spec.yaml"}, "from the directory": {filepath.Join(root, "res"), "spec.yaml"},
	} {
		t.Chdir(run.dir)
		data, err := os.ReadFile(filepath.Join(root, "res", "spec.yaml"))
		require.NoError(t, err)

		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: run.spec, Data: data}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

		require.NoError(t, err, name)
		assertNoErrorDiags(t, diags)
		assert.Equal(t, ids.In("other.yaml").ForPointer("/OS"), useTarget(t, doc, "S"), name)
		op, ok := opByName(doc, "a")
		require.True(t, ok, name)
		require.Len(t, op.Responses, 1, name)
		assert.Equal(t, "r", op.Responses[0].Docs.Description, "%s: the response other.yaml declares", name)
		irs = append(irs, jsonOf(t, doc))
	}
	require.Len(t, irs, 2)
	assert.Equal(t, irs[0], irs[1], "the IR does not depend on the working directory")
}

// TestCompile_ASelfNamingTheIdentifierOfASchemaRootsNothing holds GitHub
// #863's document: a property schema whose $id is the document's own $self
// names no resource of its own, so its draft 4 $schema chooses no dialect and
// the boolean exclusive bound is refused as a 3.2 one; beside it a property
// with another $id roots a resource and reads in draft 4. The schemas are
// properties, held by another, since a schema no other holds roots a resource
// whatever its $id.
func TestCompile_ASelfNamingTheIdentifierOfASchemaRootsNothing(t *testing.T) {
	t.Parallel()
	const draft4 = "'http://json-schema.org/draft-04/schema#'"
	src := "openapi: 3.2.0\n$self: 'https://example.com/api.yaml'\ninfo: {title: D, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    T:\n      type: object\n      properties:\n" +
		"        a: {$id: 'https://example.com/api.yaml', $schema: " + draft4 + ", type: integer, minimum: 5, exclusiveMinimum: true}\n" +
		"        b: {$id: 'https://example.com/b', $schema: " + draft4 + ", type: integer, minimum: 5, exclusiveMinimum: true}\n"

	doc, diags := compileText(t, "api.yaml", src, openapi.Options{})

	assert.Equal(t, []jsontext.Pointer{"/components/schemas/T/properties/a/exclusiveMinimum"},
		openapitest.PointersOf(diags, diag.ExclusiveBoundForm))
	bounds := lowerBounds(doc)
	assert.Equal(t, [2]ir.BigVal{"5", ""}, bounds["/components/schemas/T/properties/a"], "a has no exclusive bound")
	assert.Equal(t, [2]ir.BigVal{"", "5"}, bounds["/components/schemas/T/properties/b"], "b reads in draft 4")
}

// TestCompile_ARequirementKeyNamingTheSelfNamesTheSchemeOfTheSource holds a
// security requirement keyed by the $self-qualified pointer of a scheme the
// source declares to name that scheme, with no unresolved-scheme finding.
func TestCompile_ARequirementKeyNamingTheSelfNamesTheSchemeOfTheSource(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.2.0\n$self: " + selfURL + "\ninfo: {title: t, version: \"1\"}\n" +
		"paths:\n  /a:\n    get:\n      operationId: a\n" +
		"      security: [{'" + selfURL + "#/components/securitySchemes/key': []}]\n" +
		"      responses: {'200': {description: ok}}\n" +
		"components:\n  securitySchemes:\n    key: {type: apiKey, in: header, name: X-Key}\n"

	doc, diags := compileText(t, "api/openapi.yaml", src, openapi.Options{})

	assert.Empty(t, byCode(diags, diag.UnresolvedSecurityScheme), "%+v", diags)
	assert.Empty(t, irverify.Verify(doc))
	op, ok := opByName(doc, "a")
	require.True(t, ok)
	require.Len(t, op.Auth, 1)
	require.Len(t, op.Auth[0].Schemes, 1)
	assert.Equal(t, ir.AuthID("auth/openapi/components/securitySchemes/key"), op.Auth[0].Schemes[0].Scheme)
}

// TestCompile_ASelfHoldingAFragmentIsAnErrorAndNamesNothing holds a $self with
// a fragment, which OpenAPI 3.2 forbids, to one error at the field, and to
// naming nothing: a reference to the URL it spells is still the network's. A
// twin without the fragment draws neither.
func TestCompile_ASelfHoldingAFragmentIsAnErrorAndNamesNothing(t *testing.T) {
	t.Parallel()
	const body = "paths: {}\ncomponents:\n  schemas:\n    B: {type: string}\n    U: {$ref: '" + selfURL + "#/components/schemas/B'}\n"

	_, diags := compileWithReader(t, "api/openapi.yaml", head32+"$self: '"+selfURL+"#frag'\n"+body, openapi.Options{}, loopbackDisk{})

	got := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		got = append(got, string(diagnostic.Severity)+" "+string(diagnostic.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{"error /$self", "error /components/schemas/U"}, got, "%+v", diags)
	format := diagsAtPointer(diags, "/$self")
	require.Len(t, format, 1)
	assert.Contains(t, format[0].Message, "fragment", "the finding says a $self holds no fragment")
	assert.Equal(t, ir.Position{Line: 3, Column: 8}, format[0].Provenance.Position)
	ref := unresolvedRefs(diags)
	require.Len(t, ref, 1)
	assert.Contains(t, ref[0].Message, onNetwork)

	_, twin := compileWithReader(t, "api/openapi.yaml", head32+"$self: '"+selfURL+"'\n"+body, openapi.Options{}, loopbackDisk{})

	assert.Empty(t, byCode(twin, diag.UnresolvedRef), "the twin's own $self names the document")
	assertNoErrorDiags(t, twin)
}

// diagsAtPointer returns the diagnostics placed at pointer.
func diagsAtPointer(diags []ir.Diagnostic, pointer string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if string(diagnostic.Provenance.Pointer) == pointer {
			out = append(out, diagnostic)
		}
	}
	return out
}

// TestCompile_NothingIsMappedByInference holds the compile to the caller's
// mapping alone: without one, a $self naming a directory maps nothing to the
// directory the source lies in, so the URL is refused rather than read.
func TestCompile_NothingIsMappedByInference(t *testing.T) {
	t.Parallel()
	dir := realDir(t)
	layout(t, dir, map[string]string{"b.yaml": "B: {type: string}\n"})
	rec := &recordingReader{inner: loopbackDisk{}}
	src := "openapi: 3.2.0\n$self: " + "https://example.com/x/" + "spec.yaml\ninfo: {title: t, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    p: {$ref: '" + "https://example.com/x/" + "b.yaml#/B'}\n"

	_, diags := compileWithReader(t, filepath.Join(dir, "spec.yaml"), src, openapi.Options{}, rec)

	bad := unresolvedRefs(diags)
	require.Len(t, bad, 1, "%+v", diags)
	assert.Contains(t, bad[0].Message, onNetwork)
	for _, loc := range rec.locations() {
		assert.True(t, strings.HasPrefix(loc, "https://"), "%s is no file: the source's directory is not a mapping", loc)
	}
}

// TestCompile_ASelfMovesARelativeReadAsWrittenOrAsResolved holds the judgement
// of a read a $self moves, confined to the source's directory: under a
// relative $self the reference is judged as written, so the read is refused
// where it lands, outside the directory; under a $self written as an absolute
// path the URI it resolves to is judged, an absolute path refused unread.
func TestCompile_ASelfMovesARelativeReadAsWrittenOrAsResolved(t *testing.T) {
	t.Parallel()
	dir := realDir(t)
	layout(t, dir, map[string]string{"outside/s.yaml": "S: {type: string}\n"})
	for self, want := range map[string]string{
		"../outside/api.yaml":                     "it lies outside the source's directory",
		filepath.Join(dir, "outside", "api.yaml"): "the $ref names it by an absolute path",
	} {
		src := head32 + "$self: '" + self + "'\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: 's.yaml#/S'}\n"

		_, diags := compileWithReader(t, filepath.Join(dir, "spec", "api.yaml"), src, openapi.Options{}, loopbackDisk{})

		bad := unresolvedRefs(diags)
		require.Len(t, bad, 1, "%s: %+v", self, diags)
		assert.Contains(t, bad[0].Message, want, self)
	}
}
