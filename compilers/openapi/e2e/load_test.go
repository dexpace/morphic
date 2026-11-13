// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// parserPanicSpec is a document the parser accepts and faults on building:
// `B: {$ref}` is a mapping whose $ref key carries no value, so building the
// response the reference names from it nil-dereferences inside the parser.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const parserPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

// TestCompile_AParserPanicIsADiagnostic is the end-to-end half: the panic
// building what a reference reaches becomes an ordinary openapi/validation
// error at the reference, so a malformed spec is refused as a spec problem
// rather than reported as a Go error or crashing the process.
func TestCompile_AParserPanicIsADiagnostic(t *testing.T) {
	t.Parallel()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "parser-panic.yaml", Data: []byte(parserPanicSpec)}},
		compilers.Options{})
	require.NoError(t, err, "a malformed spec is a spec problem, not a Go error")
	assert.NotNil(t, doc, "a failed build does not stop the document being lowered")
	assert.Equal(t, 1, countCode(diags, diag.Validation, "/components/responses/000"), "%+v", diags)
}

// TestCompile_AResolutionFailureIsReportedOnceAtItsRef pins the once-per-site
// promise (#235's acceptance) at the compiler's public surface: a schema $ref
// to nothing, whether written directly, as an allOf branch, or as a oneOf
// branch, is reported exactly once, by the load phase, naming the reference and
// why it does not resolve, and the lowering's own report at that position is
// dropped as the duplicate it is (withoutRereported). In another document the
// failure is reported where it is written there, once, and the source's $ref
// into it, which resolves, reports nothing. A cycle is reported once, as one.
func TestCompile_AResolutionFailureIsReportedOnceAtItsRef(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		spec    string
		pointer string
	}{
		"a direct $ref": {
			spec:    openapitest.ComponentSpec(`    S: {$ref: '#/components/schemas/Ghost'}` + "\n"),
			pointer: "/components/schemas/S",
		},
		"an allOf branch": {
			spec:    openapitest.ComponentSpec(`    S: {allOf: [{$ref: '#/components/schemas/Ghost'}]}` + "\n"),
			pointer: "/components/schemas/S/allOf/0",
		},
		"a oneOf branch": {
			spec:    openapitest.ComponentSpec(`    S: {oneOf: [{$ref: '#/components/schemas/Ghost'}]}` + "\n"),
			pointer: "/components/schemas/S/oneOf/0",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			msg := openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, tc.pointer)
			assert.Contains(t, msg, `unresolved $ref "#/components/schemas/Ghost"`,
				"the load phase's own report, with the resolver's reason")
		})
	}

	t.Run("a failure in another document is reported there, once", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
			"openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths: {}\n"+
				"components:\n  schemas:\n    X: {$ref: '#/components/schemas/Ghost'}\n"), 0o600))
		rootPath := filepath.Join(dir, "root.yaml")
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  schemas:\n    S: {$ref: 'other.yaml#/components/schemas/X'}\n"

		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: rootPath, Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)

		want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: diag.UnresolvedRef,
			Message:    `unresolved $ref "#/components/schemas/Ghost": it names nothing in the document it points into`,
			Provenance: ir.Provenance{Source: 1, Pointer: "/components/schemas/X"}}}
		assert.Empty(t, cmp.Diff(want, diags), "reported where other.yaml writes it, and nowhere in the source")
		assert.Equal(t, filepath.Join(dir, "other.yaml"), doc.Sources[1].Path)
	})

	t.Run("a cycle is reported once, as a cycle", func(t *testing.T) {
		t.Parallel()
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  schemas:\n    A: {$ref: 'root.yaml#/x-a'}\nx-a: {$ref: '#/components/schemas/A'}\n"

		_, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(t.TempDir(), "root.yaml"), Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)

		var at []string
		for _, d := range diags {
			if d.Provenance.Pointer == "/components/schemas/A" {
				at = append(at, d.Code)
			}
		}
		assert.Equal(t, []string{diag.CyclicRef}, at, "the lowering reports nothing for a member of the loop")
	})
}

// TestCompile_AReferenceObjectToANonObjectIsReportedOnce pins what a reference
// object whose $ref names a position holding no object of its kind draws
// (GitHub #1044): one unresolved-ref error at the $ref whose own hop lands
// there, naming the kind as a schema $ref to a position holding no schema is
// worded, in whichever document writes it, and none at a reference whose chain
// passes that one. No finding stands beside it at the target, and it does not
// depend on which $ref reaches the target first. A target with findings of its
// own still reports them.
func TestCompile_AReferenceObjectToANonObjectIsReportedOnce(t *testing.T) {
	t.Parallel()
	const (
		stringLib = "x-lib: {S: just a string}\n"
		okGet     = "    get:\n      responses: {\"200\": {description: ok}}\n"
		refParam  = "  /x:\n    get:\n      parameters: [{$ref: '#/x-lib/S'}]\n" +
			"      responses: {\"200\": {description: ok}}\n"
	)
	notKind := func(kind string) string {
		return `unresolved $ref "#/x-lib/S": it names a position that holds no ` + kind
	}
	paths := func(item string) string { return openapitest.PathsSpec(item) + stringLib }
	tests := map[string]struct {
		spec    string
		pointer string
		message string
	}{
		"a parameter": {
			spec: paths(refParam), pointer: "/paths/~1x/get/parameters/0", message: notKind("parameter"),
		},
		"a request body": {
			spec: paths("  /x:\n    post:\n      requestBody: {$ref: '#/x-lib/S'}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			pointer: "/paths/~1x/post/requestBody", message: notKind("request body"),
		},
		"a response": {
			spec:    paths("  /x:\n    get:\n      responses:\n        '204': {$ref: '#/x-lib/S'}\n"),
			pointer: "/paths/~1x/get/responses/204", message: notKind("response"),
		},
		"a header": {
			spec: paths("  /x:\n    get:\n      responses:\n        '204':\n          description: ok\n" +
				"          headers: {H: {$ref: '#/x-lib/S'}}\n"),
			pointer: "/paths/~1x/get/responses/204/headers/H", message: notKind("header"),
		},
		"an example": {
			spec: paths("  /x:\n    get:\n      responses:\n        '200':\n          description: ok\n" +
				"          content:\n            application/json:\n              examples: {e: {$ref: '#/x-lib/S'}}\n"),
			pointer: "/paths/~1x/get/responses/200/content/application~1json/examples/e", message: notKind("example"),
		},
		"a link": {
			spec: paths("  /x:\n    get:\n      responses:\n        '204':\n          description: ok\n" +
				"          links: {l: {$ref: '#/x-lib/S'}}\n"),
			pointer: "/paths/~1x/get/responses/204/links/l", message: notKind("link"),
		},
		"a callback": {
			spec: paths("  /x:\n    get:\n      callbacks: {cb: {$ref: '#/x-lib/S'}}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			pointer: "/paths/~1x/get/callbacks/cb", message: notKind("callback"),
		},
		"a path item": {
			spec:    paths("  /x: {$ref: '#/x-lib/S'}\n"),
			pointer: "/paths/~1x", message: notKind("path item"),
		},
		"a security scheme": {
			spec: openapitest.PathsSpec("  /x:\n"+okGet) + "components:\n  securitySchemes:\n    s: {$ref: '#/x-lib/S'}\n" +
				stringLib,
			pointer: "/components/securitySchemes/s", message: notKind("security scheme"),
		},
		"a target that is null": {
			spec:    openapitest.PathsSpec(refParam) + "x-lib: {S: null}\n",
			pointer: "/paths/~1x/get/parameters/0", message: notKind("parameter"),
		},
		"a target that is a sequence": {
			spec:    openapitest.PathsSpec(refParam) + "x-lib: {S: [a, b]}\n",
			pointer: "/paths/~1x/get/parameters/0", message: notKind("parameter"),
		},
		"a chain through another reference object": {
			spec: openapitest.PathsSpec("  /x:\n    get:\n      parameters: [{$ref: '#/components/parameters/P'}]\n"+
				"      responses: {\"200\": {description: ok}}\n") +
				"components:\n  parameters:\n    P: {$ref: '#/x-lib/S'}\n" + stringLib,
			pointer: "/components/parameters/P", message: notKind("parameter"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: diag.UnresolvedRef, Message: tc.message,
				Provenance: ir.Provenance{Pointer: jsontext.Pointer(tc.pointer)}}}
			assert.Empty(t, cmp.Diff(want, diags), "the one finding of the document")
		})
	}

	t.Run("two references to one target are reported alike in either order", func(t *testing.T) {
		t.Parallel()
		site := func(path string) string {
			return "  " + path + ":\n    get:\n      parameters: [{$ref: '#/x-lib/S'}]\n" +
				"      responses: {\"200\": {description: ok}}\n"
		}
		want := map[jsontext.Pointer]string{
			"/paths/~1a/get/parameters/0": notKind("parameter"),
			"/paths/~1b/get/parameters/0": notKind("parameter"),
		}
		for _, order := range []string{site("/a") + site("/b"), site("/b") + site("/a")} {
			_, diags := parseFull(t, openapitest.PathsSpec(order)+stringLib)
			got := map[jsontext.Pointer]string{}
			for _, d := range diags {
				assert.Equal(t, diag.UnresolvedRef, d.Code, "%+v", d)
				assert.Equal(t, ir.SeverityError, d.Severity, "%+v", d)
				got[d.Provenance.Pointer] = d.Message
			}
			assert.Len(t, diags, len(want), "one finding at each $ref, none beside it: %+v", diags)
			assert.Empty(t, cmp.Diff(want, got), "declared in the order %q", order)
		}
	})

	t.Run("a target that is null in another document", func(t *testing.T) {
		t.Parallel()
		root := openapitest.PathsSpec("  /x:\n    get:\n      parameters: [{$ref: 'other.yaml'}]\n" +
			"      responses: {\"200\": {description: ok}}\n")

		diags := compileBesideExpectingErrors(t, "null\n", root)

		want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: diag.UnresolvedRef,
			Message:    `unresolved $ref "other.yaml": it names a position that holds no parameter`,
			Provenance: ir.Provenance{Pointer: "/paths/~1x/get/parameters/0"}}}
		assert.Empty(t, cmp.Diff(want, diags), "reported at the $ref in the source, once")
	})

	t.Run("a chain through a reference written in another document", func(t *testing.T) {
		t.Parallel()
		dir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "mid.yaml"),
			[]byte("P: {$ref: '#/S'}\nS: just a string\n"), 0o600))
		root := openapitest.PathsSpec("  /x:\n    get:\n      parameters: [{$ref: 'mid.yaml#/P'}]\n" +
			"      responses: {\"200\": {description: ok}}\n")

		_, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)

		want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: diag.UnresolvedRef,
			Message:    `unresolved $ref "#/S": it names a position that holds no parameter`,
			Provenance: ir.Provenance{Source: 1, Pointer: "/P"}}}
		assert.Empty(t, cmp.Diff(want, diags), "reported where the hop that fails is written, in mid.yaml")
	})

	t.Run("a target with findings of its own still reports them", func(t *testing.T) {
		t.Parallel()
		_, diags := parseFull(t, openapitest.PathsSpec(strings.Replace(refParam, "x-lib/S", "x-lib/P", 1))+
			"x-lib: {P: {name: q, in: nowhere, schema: {type: string}}}\n")

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, jsontext.Pointer("/x-lib/P/in"), diags[0].Provenance.Pointer, "at the node it is about")
		assert.False(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "the $ref resolved to an object")
	})

	t.Run("a target in another document with findings of its own still reports them", func(t *testing.T) {
		t.Parallel()
		root := openapitest.PathsSpec(strings.Replace(refParam, "#/x-lib/S", "other.yaml#/x-lib/P", 1))

		diags := compileBesideExpectingErrors(t, "x-lib: {P: {name: q, in: nowhere, schema: {type: string}}}\n", root)

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, 1, diags[0].Provenance.Source, "in the entry for the document that writes it")
		assert.Equal(t, jsontext.Pointer("/x-lib/P/in"), diags[0].Provenance.Pointer, "at the node it is about")
	})
}

// TestCompile_AFindingInASchemaOnlyAnotherDocumentReachesIsPlacedThere pins
// where the validation of a schema built for raw YAML is reported when only
// another document's content reaches it, through a parameter that document
// declares: in that document's Document.Sources entry, at the keyword it is
// about, never at the same pointer in the source, which holds nothing there.
func TestCompile_AFindingInASchemaOnlyAnotherDocumentReachesIsPlacedThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
		"components:\n  parameters:\n    P: {name: p, in: query, schema: {$ref: '#/x-lib/B'}}\n"+
			"x-lib:\n  B: {type: string, minLength: notanumber}\n"), 0o600))
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a:\n    get:\n" +
		"      parameters: [{$ref: './other.yaml#/components/parameters/P'}]\n" +
		"      responses: {\"200\": {description: ok}}\n"

	_, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}})
	require.NoError(t, err)

	require.NotEmpty(t, diags)
	for _, d := range diags {
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/x-lib/B/minLength", Position: ir.Position{Line: 5, Column: 32}},
			d.Provenance, "%+v", d)
		assert.True(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "%+v", d)
	}
}

// TestCompile_AFindingInAnExternalDocumentKeepsItsRule is the #537 and #1030
// repro: a finding inside a document reached only through an external $ref
// keeps its own rule and severity, rather than reading as an unresolved
// reference, and is placed in that document's own Document.Sources entry, at
// the node it is about.
func TestCompile_AFindingInAnExternalDocumentKeepsItsRule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
		"openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\n"+
			"paths:\n  /y:\n    get:\n      parameters:\n        - {name: q, schema: {type: string}}\n"+
			"      responses: {\"200\": {description: ok}}\n"), 0o600))
	rootPath := filepath.Join(dir, "root.yaml")
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x: {$ref: 'other.yaml#/paths/~1y'}\n"

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: rootPath, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	var found []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.Validation+"/validation-required-field" {
			found = append(found, d)
		}
	}
	require.Len(t, found, 1, "%+v", diags)
	assert.Equal(t, ir.SeverityError, found[0].Severity)
	assert.Equal(t, `Parameter Object field "in" is REQUIRED in OpenAPI 3.1 (Parameter Object)`, found[0].Message)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1y/get/parameters/0", Position: ir.Position{Line: 7, Column: 11}},
		found[0].Provenance)
	require.Len(t, doc.Sources, 2)
	assert.Equal(t, filepath.Join(dir, "other.yaml"), doc.Sources[1].Path, "the document it is in")
}

// TestLoad_RecoverableLiteralAmongOtherScalarsIsNotAFinding pins that a schema
// writing a number JSON does not spell (.5) beside a custom-tagged scalar draws
// no validation finding: the table reads the YAML nodes, and both are values a
// default and an example may hold.
func TestLoad_RecoverableLiteralAmongOtherScalarsIsNotAFinding(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(`    S: {type: string, default: !custom foo, example: .5}`))
	for _, d := range diags {
		assert.False(t, strings.HasPrefix(d.Code, diag.Validation), "no finding: %+v", d)
	}
}

// TestCompile_OneFileUnderTwoNamesIsOneDocument pins that, with external
// references allowed, a file references spell two ways, by a path relative to
// the spec and by an absolute one, is one document: one Document.Sources
// entry, its declarations lowered once under one label, each finding in it
// reported once, and every ID the same whether the spec itself was named by a
// relative path or an absolute one, located where it lies as the engine
// locates it.
func TestCompile_OneFileUnderTwoNamesIsOneDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "inner"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inner", "real.yaml"), []byte(
		"components:\n  schemas:\n    S: {type: object, properties: {gone: {$ref: '#/components/schemas/Missing'}}}\n"), 0o600))
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A: {$ref: './inner/real.yaml#/components/schemas/S'}\n" +
		"    B: {$ref: '" + filepath.Join(dir, "inner", "real.yaml") + "#/components/schemas/S'}\n")
	compileAt := func(path string) (*ir.Document, []ir.Diagnostic) {
		t.Helper()
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: path, Location: filepath.Join(dir, "root.yaml"), Data: root}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		return doc, diags
	}

	relative, diags := compileAt("root.yaml")
	absolute, _ := compileAt(filepath.Join(dir, "root.yaml"))

	assert.Len(t, relative.Sources, 2, "the spec and the one file it reads")
	s := ids.In("inner/real.yaml").NamedType("/components/schemas/S")
	for _, name := range []string{"A", "B"} {
		a, ok := relative.Types[ids.Document{}.NamedType(ids.Ptr("components", "schemas", name))].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, a.Base, name)
		assert.Equal(t, s, a.Base.Target, "%s names the one S", name)
	}
	gone := 0
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			gone++
		}
	}
	assert.Equal(t, 1, gone, "the finding in the file is reported once: %+v", diags)
	assert.Empty(t, cmp.Diff(relative.Types, absolute.Types), "the IDs do not depend on how the spec was named")
}

// TestCompile_ByDefaultOnlyARelativeReferenceIsFollowed pins what the default
// reads: a document a relative $ref names. A $ref naming one by an absolute
// path, or by a file URI, is refused unread with the reason, though the file
// lies inside the spec's directory, so the result depends on no spelling of the
// working directory: it is the same from a directory reached through a
// symbolic link and from its target, the spec named relatively or not.
func TestCompile_ByDefaultOnlyARelativeReferenceIsFollowed(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "inner"), 0o700))
	for name, body := range map[string]string{"real.yaml": "X: {type: string}\n", "only.yaml": "Y: {type: integer}\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "inner", name), []byte(body), 0o600))
	}
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    Rel: {$ref: './inner/real.yaml#/X'}\n" +
		"    Abs: {$ref: '" + filepath.Join(dir, "inner", "only.yaml") + "#/Y'}\n" +
		"    URI: {$ref: 'file://" + filepath.Join(dir, "inner", "only.yaml") + "#/Y'}\n")
	compileFrom := func(cwd, path string) (ir.TypeRegistry, []string) {
		t.Helper()
		t.Chdir(cwd)
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: root}},
			compilers.Options{Reader: reader.Disk{}})
		require.NoError(t, err)
		require.NotNil(t, doc)
		reasons := make([]string, 0, len(diags))
		for _, d := range diags {
			reasons = append(reasons, string(d.Provenance.Pointer)+": "+d.Message[strings.LastIndex(d.Message, ": ")+2:])
		}
		return doc.Types, reasons
	}

	types, reasons := compileFrom(dir, "root.yaml")

	assert.Equal(t, []string{
		"/components/schemas/Abs: the $ref names it by an absolute path, which a read follows only when external references are allowed",
		"/components/schemas/URI: it is named by a file URI, which a read follows only when external references are allowed",
	}, reasons)
	assert.Contains(t, types, ids.In("inner/real.yaml").AnonType("/X"), "the relative $ref is followed")
	for _, run := range []struct{ cwd, path string }{
		{link, "root.yaml"}, {dir, filepath.Join(dir, "root.yaml")}, {link, filepath.Join(link, "root.yaml")},
	} {
		again, againReasons := compileFrom(run.cwd, run.path)
		assert.Empty(t, cmp.Diff(types, again), "from %s as %s", run.cwd, run.path)
		assert.Empty(t, cmp.Diff(reasons, againReasons), "from %s as %s", run.cwd, run.path)
	}
}

// TestCompile_AnUnusableDocumentIsReportedAlikeOnEitherRoute pins that a
// document read and not usable, one that does not parse or one holding
// nothing, fails a schema $ref and a parameter's with one wording, whether the
// $ref names
// a node in it or the whole of it, in either mode, and draws no other finding.
func TestCompile_AnUnusableDocumentIsReportedAlikeOnEitherRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, says string }{
		{"one that does not parse", "a: [unclosed\n",
			"the document it names does not parse as YAML: yaml: line 1: did not find expected ',' or ']'"},
		{"one holding nothing", "", "the document it names is empty"},
		{"one holding only a comment", "# nothing yet\n", "the document it names is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "u.yaml"), []byte(tc.body), 0o600))
			root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n    get:\n" +
				"      parameters:\n        - {$ref: 'u.yaml#/P'}\n        - {$ref: 'u.yaml'}\n" +
				"      responses: {\"200\": {description: ok}}\n" +
				"components:\n  schemas:\n    S: {$ref: 'u.yaml#/B'}\n    T: {$ref: 'u.yaml'}\n")

			for _, allow := range []bool{false, true} {
				doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: root}},
					compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
				require.NoError(t, err)
				require.NotNil(t, doc)

				got := make([]string, 0, len(diags))
				for _, d := range diags {
					got = append(got, string(d.Provenance.Pointer)+": "+d.Message)
				}
				slices.Sort(got)
				assert.Equal(t, []string{
					`/components/schemas/S: unresolved $ref "u.yaml#/B": ` + tc.says,
					`/components/schemas/T: unresolved $ref "u.yaml": ` + tc.says,
					`/paths/~1x/get/parameters/0: unresolved $ref "u.yaml#/P": ` + tc.says,
					`/paths/~1x/get/parameters/1: unresolved $ref "u.yaml": ` + tc.says,
				}, got, "external references allowed: %v", allow)
				assert.Len(t, doc.Sources, 1, "a document refused is no source")
			}
		})
	}
}

// TestCompile_AURLNoReaderReadsIsRefusedUnread pins that a $ref whose
// document part is a URL with a scheme other than http, https or file fails
// unread with the reason, in a schema and in a parameter, with external
// references allowed or not, and whatever the working directory: a reader
// would read "foo:b.yaml" as a path against it. A file whose name holds a
// colon, written "./foo:b.yaml", is read, the spec named relatively too,
// where the parser library's join would make it that same "foo:b.yaml"
// (resolve.Locate keeps the "./"). Both routes word the refusal alike.
func TestCompile_AURLNoReaderReadsIsRefusedUnread(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	for _, at := range []string{dir, sub} {
		require.NoError(t, os.WriteFile(filepath.Join(at, "foo:b.yaml"), []byte(
			"B: {type: string}\nP: {name: q, in: query, schema: {type: string}}\n"), 0o600))
	}
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n    get:\n" +
		"      parameters:\n        - {$ref: 'foo:b.yaml#/P'}\n        - {$ref: './foo:b.yaml#/P'}\n" +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  schemas:\n    URL: {$ref: 'foo:b.yaml#/B'}\n    Dot: {$ref: './foo:b.yaml#/B'}\n")
	const refused = `it is named by a URL with the scheme "foo", and only http, https and file URLs are read`

	for _, run := range []struct{ cwd, spec string }{
		{dir, filepath.Join(dir, "root.yaml")}, {sub, filepath.Join(dir, "root.yaml")}, {dir, "root.yaml"},
	} {
		t.Chdir(run.cwd)
		for _, allow := range []bool{false, true} {
			doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: run.spec, Data: root}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
			require.NoError(t, err)
			require.NotNil(t, doc)

			got := make([]string, 0, len(diags))
			for _, d := range diags {
				got = append(got, string(d.Provenance.Pointer)+": "+d.Message[strings.LastIndex(d.Message, ": ")+2:])
				assert.Contains(t, d.Message, ": the document it names could not be read: external document foo:b.yaml refused: ",
					"a schema's $ref and a parameter's word one refusal alike")
			}
			slices.Sort(got)
			assert.Equal(t, []string{"/components/schemas/URL: " + refused, "/paths/~1x/get/parameters/0: " + refused}, got,
				"%s from %s, external references allowed: %v", run.spec, run.cwd, allow)
			require.Len(t, doc.Sources, 2, "%s from %s, external references allowed: %v", run.spec, run.cwd, allow)
			assert.Equal(t, "foo:b.yaml", filepath.Base(doc.Sources[1].Path), "the file the dot segment names")
			assert.Contains(t, doc.Types, ids.In("foo:b.yaml").AnonType("/B"), "the dot-segment $ref is followed")
		}
	}
}

// TestCompile_ARefAnIDAnswersIsNeverRefusedUnread pins that a $ref naming a
// URI an $id declares resolves to that schema whatever its scheme, with
// external references allowed or not, since no document is read for an
// identifier, while one no $id declares is refused unread by its scheme.
func TestCompile_ARefAnIDAnswersIsNeverRefusedUnread(t *testing.T) {
	t.Parallel()
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    Pet:\n      type: object\n      properties:\n        kind: {$ref: 'urn:example:kind'}\n" +
		"        other: {$ref: 'urn:example:none'}\n    Kind: {$id: 'urn:example:kind', type: string}\n")
	path := filepath.Join(t.TempDir(), "root.yaml")
	component := func(name string) ir.TypeID { return ids.Document{}.ForPointer(ids.Ptr("components", "schemas", name)) }
	for _, allow := range []bool{false, true} {
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: root}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
		require.NoError(t, err)
		require.NotNil(t, doc)

		pet, ok := doc.Types[component("Pet")].(*ir.Model)
		require.True(t, ok, "Pet is a Model")
		kind, ok := propByWire(pet, "kind")
		require.True(t, ok, "Pet declares kind")
		assert.Equal(t, component("Kind"), kind.Type.Target, "external references allowed: %v", allow)
		assert.Equal(t, `unresolved $ref "urn:example:none": the document it names could not be read: `+
			`external document urn:example:none refused: it is named by a URL with the scheme "urn", `+
			`and only http, https and file URLs are read`,
			openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError,
				"/components/schemas/Pet/properties/other"))
		errs := slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool { return d.Severity != ir.SeverityError })
		assert.Len(t, errs, 1, "the identifier draws no refusal: %v", errs)
	}
}

// TestCompile_ARelativeRefUnderAnIDReadsWhereTheIDPlacesIt pins the document a
// relative $ref no $id declares names under an $id: the one at the URI the $id
// makes of it, read as any reference to that URI is, and never the file of
// that name beside the source. Under a relative $id it lies beside the
// location the $id gives, read by default; under a URL it is on the network,
// read only when external references are allowed.
func TestCompile_ARelativeRefUnderAnIDReadsWhereTheIDPlacesIt(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.FileServerFS(fstest.MapFS{"x/b.yaml": {Data: []byte("B: {type: integer}\n")}}))
	t.Cleanup(srv.Close)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for name, body := range map[string]string{"b.yaml": "B: {type: boolean}\n", "tag.json": "T: {type: boolean}\n",
		"schemas/tag.json": "T: {type: string}\n"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    Pet: {$id: 'schemas/pet.json', type: object, properties: {tag: {$ref: 'tag.json#/T'}}}\n" +
		"    S: {$id: '" + srv.URL + "/x/s.json', type: object, properties: {b: {$ref: 'b.yaml#/B'}}}\n")
	for _, allow := range []bool{false, true} {
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: root}},
			compilers.Options{Reader: reader.Disk{Client: srv.Client()}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
		require.NoError(t, err)
		require.NotNil(t, doc)

		pet, ok := doc.Types[ids.Document{}.ForPointer("/components/schemas/Pet")].(*ir.Model)
		require.True(t, ok, "Pet is a Model")
		tag, ok := propByWire(pet, "tag")
		require.True(t, ok, "Pet declares tag")
		assert.Equal(t, ids.In("schemas/tag.json").AnonType("/T"), tag.Type.Target, "external references allowed: %v", allow)
		for _, beside := range []ir.TypeID{ids.In("tag.json").AnonType("/T"), ids.In("b.yaml").AnonType("/B")} {
			assert.NotContains(t, doc.Types, beside, "the file beside the source is never read")
		}
		unresolved := openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/S/properties/b")
		assert.Equal(t, !allow, unresolved, "the URL is read only when allowed: %v", diags)
		if !allow {
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError,
				"/components/schemas/S/properties/b"), "external document "+srv.URL+"/x/b.yaml refused: it is on the network")
		}
	}
}

// TestCompile_ByDefaultEachReferenceIsJudgedByItsOwnSpelling pins that the
// default refuses a $ref by how it spells its document, never by how another
// $ref spells it or by which document was read first: an absolute $ref is
// refused while a relative one naming the same file is followed, in a schema
// and in a parameter, whichever of the documents holding the schema $refs is
// declared first, the spec named relatively or not. An absolute $ref naming
// the spec itself resolves, the spec located as the engine locates it.
func TestCompile_ByDefaultEachReferenceIsJudgedByItsOwnSpelling(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	b, p, self := filepath.Join(dir, "b.yaml"), filepath.Join(dir, "p.yaml"), filepath.Join(dir, "root.yaml")
	for name, body := range map[string]string{
		"b.yaml":   "B: {type: string}\n",
		"abs.yaml": "Y: {$ref: '" + b + "#/B'}\n",
		"rel.yaml": "W: {$ref: 'b.yaml#/B'}\n",
		"p.yaml":   "P: {name: q, in: query, schema: {type: string}}\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	spec := func(first, second string) []byte {
		return []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n    get:\n" +
			"      parameters:\n        - {$ref: '" + p + "#/P'}\n        - {$ref: 'p.yaml#/P'}\n" +
			"        - {$ref: '" + self + "#/components/parameters/Own'}\n" +
			"      responses: {\"200\": {description: ok}}\ncomponents:\n" +
			"  parameters:\n    Own: {name: o, in: query, schema: {type: string}}\n  schemas:\n" +
			"    Leaf: {type: integer}\n    Self: {$ref: '" + self + "#/components/schemas/Leaf'}\n" +
			"    " + first + ": {$ref: 'abs.yaml#/Y'}\n    " + second + ": {$ref: 'rel.yaml#/W'}\n")
	}
	const refused = "the $ref names it by an absolute path, which a read follows only when external references are allowed"

	for _, order := range [][2]string{{"C", "D"}, {"D", "C"}} {
		types := make([]ir.TypeRegistry, 0, 2)
		for _, path := range []string{"root.yaml", self} {
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: path, Location: self, Data: spec(order[0], order[1])}},
				compilers.Options{Reader: reader.Disk{}})
			require.NoError(t, err)
			require.NotNil(t, doc)
			var got []string
			for _, d := range diags {
				got = append(got, filepath.Base(doc.Sources[d.Provenance.Source].Path)+"#"+string(d.Provenance.Pointer)+": "+
					d.Message[strings.LastIndex(d.Message, ": ")+2:])
			}
			slices.Sort(got)
			assert.Equal(t, []string{"abs.yaml#/Y: " + refused, "root.yaml#/paths/~1x/get/parameters/0: " + refused}, got,
				"%v as %s", order, path)
			w, ok := doc.Types[ids.In("rel.yaml").AnonType("/W")].(*ir.Scalar)
			require.True(t, ok, "%v as %s", order, path)
			require.NotNil(t, w.Base)
			assert.Equal(t, ids.In("b.yaml").AnonType("/B"), w.Base.Target, "the relative $ref is followed")
			types = append(types, doc.Types)
		}
		assert.Empty(t, cmp.Diff(types[0], types[1]), "%v: the IDs do not depend on how the spec was named", order)
	}
}

// TestCompile_AnAbsolutePathInANetworkDocumentNamesItsHost pins that a $ref a
// document fetched over the network writes as an absolute path, or as a
// network-path reference, names what RFC 3986 §5.2 resolves it to on the
// network, never a local file: it is read from the host, by a schema's $ref
// and by a parameter's, and nothing is refused.
func TestCompile_AnAbsolutePathInANetworkDocumentNamesItsHost(t *testing.T) {
	t.Parallel()
	served := map[string]string{
		"/v1/api.yaml": "components:\n  schemas:\n    S: {$ref: '/schemas/pet.yaml#/Pet'}\n" +
			"  parameters:\n    P: {$ref: '/params.yaml#/P'}\n    N: {$ref: '//{host}/params.yaml#/N'}\n",
		"/schemas/pet.yaml": "Pet: {type: object, properties: {name: {type: string}}}\n",
		"/params.yaml": "P: {name: p, in: query, schema: {type: string}}\n" +
			"N: {name: n, in: query, schema: {$ref: '/schemas/pet.yaml#/Pet'}}\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := served[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, err := w.Write([]byte(strings.ReplaceAll(body, "{host}", r.Host)))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	api := srv.URL + "/v1/api.yaml"
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n    get:\n      parameters:\n" +
		"        - {$ref: '" + api + "#/components/parameters/P'}\n        - {$ref: '" + api + "#/components/parameters/N'}\n" +
		"      responses: {\"200\": {description: ok}}\ncomponents:\n  schemas:\n" +
		"    S: {$ref: '" + api + "#/components/schemas/S'}\n")

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(t.TempDir(), "root.yaml"), Data: root}},
		compilers.Options{Reader: reader.Disk{Client: srv.Client()}, FormatOptions: openapi.Options{AllowExternalRefs: true}})

	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	pet := ids.In(srv.URL + "/schemas/pet.yaml").AnonType("/Pet")
	assert.Contains(t, doc.Types, pet, "the schema the absolute path names, read from the host")
	ops := doc.Services[0].Groups[0].Operations
	require.Len(t, ops, 1)
	names := make([]string, 0, len(ops[0].Params))
	for _, p := range ops[0].Params {
		names = append(names, p.Name.Source)
	}
	assert.Equal(t, []string{"p", "n"}, names, "each parameter the network-path reference and the absolute path name")
}

// TestCompile_ANetworkDocumentReadsNoLocalFile pins the boundary a document
// fetched over the network is held to, one the source names or one another
// network document does: every request names an http(s) URL. An absolute
// path, a network-path reference and a path climbing past the root resolve on
// the network by RFC 3986, under an $id too. A file: URI or a drive path, as a
// $ref, or as an $id or draft 4's `id` above one, nested or not, is refused
// where it is written, though a local file exists, and the rest of the
// document is read; an example's $ref is data.
func TestCompile_ANetworkDocumentReadsNoLocalFile(t *testing.T) {
	t.Parallel()
	local := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(local, "secret.yaml"), []byte(secretDoc), 0o600))
	fileURI := "file://" + filepath.ToSlash(local) + "/"
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(secretDoc))
		assert.NoError(t, err)
	}))
	t.Cleanup(other.Close)
	otherHost := strings.TrimPrefix(other.URL, "http://")
	below := func(keywords ...string) string {
		s := "{$ref: 'secret.yaml#/X'}"
		for _, k := range slices.Backward(keywords) {
			s = "{" + k + ", properties: {p: " + s + "}}"
		}
		return s
	}
	for _, tc := range []struct {
		name, schema, param string
		refused             bool
	}{
		{"an absolute path", "{$ref: '/x/secret.yaml#/X'}", "/x/secret.yaml#/P", false},
		{"a network-path reference", "{$ref: '//" + otherHost + "/secret.yaml#/X'}", "//" + otherHost + "/secret.yaml#/P", false},
		{"a file URI", "{$ref: '" + fileURI + "secret.yaml#/X'}", fileURI + "secret.yaml#/P", true},
		{"a drive path", `{$ref: 'C:\x\secret.yaml#/X'}`, `C:\x\secret.yaml#/P`, true},
		{"an absolute $id", below("$id: '/x/'"), "", false},
		{"a network-path $id", below("$id: '//" + otherHost + "/'"), "", false},
		{"an $id climbing past the root", below("$id: '../../../../x/'"), "", false},
		{"a file URI $id", below("$id: '" + fileURI + "'"), "", true},
		{"a drive $id", below(`$id: 'C:\x\'`), "", true},
		{"an absolute $id below another", below("$id: '//"+otherHost+"/a/'", "$id: '/x/'"), "", false},
		{"a file URI $id below another", below("$id: '/a/'", "$id: '"+fileURI+"'"), "", true},
		{"a draft 4 file URI id", below("$schema: 'http://json-schema.org/draft-04/schema#', id: '" + fileURI + "'"), "", true},
		{"an example's $ref", "{type: object, example: {$ref: '" + fileURI + "secret.yaml#/X'}}", "", false},
	} {
		for _, hops := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s, %d documents deep", tc.name, hops), func(t *testing.T) {
				t.Parallel()
				entries := []networkEntry{{"schemas", "S", tc.schema}}
				if tc.param != "" {
					entries = append(entries, networkEntry{"parameters", "P", "{$ref: '" + tc.param + "'}"})
				}
				assertNoLocalRead(t, compileOverTheNetwork(t, "3.1.0", entries, hops), otherHost, tc.refused)
			})
		}
	}
}

// TestCompile_ANetworkDocumentReadsNoLocalFileByAnyReference pins the same
// boundary for each kind of reference the compiler follows, a file: URI in a
// document fetched over the network, one and two network documents deep: a
// schema's $ref, a reference object of every kind, and a discriminator's
// mapping value and defaultMapping are each refused where they are written,
// and nothing local is read;
// a link's operationRef and an example's externalValue name nothing a compile
// reads.
func TestCompile_ANetworkDocumentReadsNoLocalFileByAnyReference(t *testing.T) {
	t.Parallel()
	local := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(local, "secret.yaml"), []byte(secretDoc), 0o600))
	secret := "file://" + filepath.ToSlash(local) + "/secret.yaml"
	oneOf := "{oneOf: [{type: object, properties: {k: {type: string}}}], discriminator: {propertyName: k, "
	for _, tc := range []struct {
		name    string
		entry   networkEntry
		refused bool
	}{
		{"a schema", networkEntry{"schemas", "S", "{$ref: '" + secret + "#/X'}"}, true},
		{"a parameter", networkEntry{"parameters", "P", "{$ref: '" + secret + "#/P'}"}, true},
		{"a response", networkEntry{"responses", "R", "{$ref: '" + secret + "#/R'}"}, true},
		{"a request body", networkEntry{"requestBodies", "B", "{$ref: '" + secret + "#/B'}"}, true},
		{"a header", networkEntry{"headers", "H", "{$ref: '" + secret + "#/H'}"}, true},
		{"an example", networkEntry{"examples", "E", "{$ref: '" + secret + "#/E'}"}, true},
		{"a link", networkEntry{"links", "L", "{$ref: '" + secret + "#/L'}"}, true},
		{"a callback", networkEntry{"callbacks", "C", "{$ref: '" + secret + "#/C'}"}, true},
		{"a path item", networkEntry{"pathItems", "I", "{$ref: '" + secret + "#/I'}"}, true},
		{"a security scheme", networkEntry{"securitySchemes", "Z", "{$ref: '" + secret + "#/Z'}"}, true},
		{"a media type", networkEntry{"mediaTypes", "M", "{$ref: '" + secret + "#/M'}"}, true},
		{"a mapping value", networkEntry{"schemas", "S", oneOf + "mapping: {a: '" + secret + "#/X'}}}"}, true},
		{"a defaultMapping", networkEntry{"schemas", "S", oneOf + "defaultMapping: '" + secret + "#/X'}}"}, true},
		{"a link's operationRef", networkEntry{"links", "L", "{operationRef: '" + secret + "#/O'}"}, false},
		{"an example's externalValue", networkEntry{"examples", "E", "{externalValue: '" + secret + "'}"}, false},
	} {
		for _, hops := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s, %d documents deep", tc.name, hops), func(t *testing.T) {
				t.Parallel()
				got := compileOverTheNetwork(t, "3.2.0", []networkEntry{tc.entry}, hops)
				assertNoLocalRead(t, got, "", tc.refused)
			})
		}
	}
}

// TestCompile_ASourceReadOverTheNetworkReadsNoLocalFile pins the boundary for
// a source itself read over the network, with external references allowed or
// not: a file: URI in a schema's $ref, a parameter's, a mapping value, or
// below an $id is refused. A parameter's relative $ref under a 3.2 `$self`
// naming a file URI or a path is read beside the source, on the network, as
// no document's `$self` is a base (docs/compilers/openapi.md#self-uri).
// Nothing local is read.
func TestCompile_ASourceReadOverTheNetworkReadsNoLocalFile(t *testing.T) {
	t.Parallel()
	local := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(local, "secret.yaml"), []byte(secretDoc), 0o600))
	dir := filepath.ToSlash(local) + "/"
	self := func(base string) string {
		return "openapi: 3.2.0\n$self: '" + base + "'\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  parameters:\n    P: {$ref: 'secret.yaml#/P'}\n"
	}
	in31 := func(components string) string {
		return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n" + components
	}
	for _, tc := range []struct{ name, spec string }{
		{"a schema's file URI", in31("  schemas:\n    S: {$ref: 'file://" + dir + "secret.yaml#/X'}\n")},
		{"a parameter's file URI", in31("  parameters:\n    P: {$ref: 'file://" + dir + "secret.yaml#/P'}\n")},
		{"a mapping value's file URI", in31("  schemas:\n    S: {oneOf: [{type: object}], discriminator: " +
			"{propertyName: k, mapping: {a: 'file://" + dir + "secret.yaml#/X'}}}\n")},
		{"a file URI $id", in31("  schemas:\n    S: {$id: 'file://" + dir + "', properties: {p: {$ref: 'secret.yaml#/X'}}}\n")},
		{"a $self naming a file URI", self("file://" + dir)},
		{"a $self naming a path", self(dir)},
	} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, external references allowed: %v", tc.name, allow), func(t *testing.T) {
				t.Parallel()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, err := w.Write([]byte(secretDoc))
					assert.NoError(t, err)
				}))
				t.Cleanup(srv.Close)
				rec := &recordingReader{inner: reader.Disk{Client: srv.Client()}}
				got, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: srv.URL + "/v1/root.yaml", Data: []byte(tc.spec)}},
					compilers.Options{Reader: rec, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
				require.NoError(t, err)
				require.NotNil(t, got)

				compiled := networkCompile{diags: diags, locations: rec.locations(),
					host: strings.TrimPrefix(srv.URL, "http://")}
				if !strings.HasPrefix(tc.name, "a $self") {
					assertNoLocalRead(t, compiled, "", true)
					return
				}
				// A $self is no base, so its row names the document beside the
				// source, on the network, read only where external references are.
				if allow {
					assertNoLocalRead(t, compiled, "", false)
					return
				}
				for _, l := range rec.locations() {
					assert.True(t, strings.HasPrefix(l, srv.URL+"/"), "nothing local is read: %s", l)
				}
				require.Len(t, diags, 1, "%+v", diags)
				assert.Contains(t, diags[0].Message, "/v1/secret.yaml refused: it is on the network")
			})
		}
	}
}

// secretDoc is the document the references of
// TestCompile_ANetworkDocumentReadsNoLocalFile and its kin name, on disk and
// on each host: an object of each kind a reference may name.
const secretDoc = "X: {type: integer}\nP: {name: p, in: query, schema: {type: integer}}\n" +
	"R: {description: r}\nB: {content: {application/json: {schema: {type: integer}}}}\n" +
	"H: {schema: {type: integer}}\nE: {value: 1}\nL: {operationId: o}\nC: {}\nI: {}\n" +
	"Z: {type: http, scheme: basic}\nM: {schema: {type: integer}}\n"

// networkEntry is a components entry of a document served over the network:
// its section, its name, and its value as YAML.
type networkEntry struct{ section, name, value string }

// networkCompile is what compileOverTheNetwork returns: what the compile
// reported, the location of every request it made, and the host the network
// documents are served from.
type networkCompile struct {
	diags     []ir.Diagnostic
	locations []string
	host      string
}

// compileOverTheNetwork compiles a source at version naming each of entries,
// as a document served over HTTP declares it beside a schema OK, which the
// source names too, reached through hops network documents, the last
// declaring them, and asserts OK lowers.
func compileOverTheNetwork(t *testing.T, version string, entries []networkEntry, hops int) networkCompile {
	t.Helper()
	entries = append(slices.Clone(entries), networkEntry{"schemas", "OK", "{type: string}"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := secretDoc
		switch {
		case r.URL.Path == "/v1/d/a.yaml" && hops > 1:
			body = componentsOf(entries, func(e networkEntry) string { return "{$ref: 'b.yaml" + e.at() + "'}" })
		case r.URL.Path == "/v1/d/a.yaml", r.URL.Path == "/v1/d/b.yaml":
			body = componentsOf(entries, func(e networkEntry) string { return e.value })
		}
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	api := srv.URL + "/v1/d/a.yaml"
	root := "openapi: " + version + "\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		componentsOf(entries, func(e networkEntry) string { return "{$ref: '" + api + e.at() + "'}" })
	rec := &recordingReader{inner: reader.Disk{Client: srv.Client()}}
	got, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(t.TempDir(), "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: rec, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, got)
	ok := slices.ContainsFunc(slices.Collect(maps.Keys(got.Types)), func(id ir.TypeID) bool {
		return strings.Contains(string(id), "@http") && strings.HasSuffix(string(id), "/components/schemas/OK")
	})
	assert.True(t, ok, "the rest of the network document is read")
	return networkCompile{diags: diags, locations: rec.locations(), host: strings.TrimPrefix(srv.URL, "http://")}
}

// at is the pointer naming e in the document declaring it.
func (e networkEntry) at() string { return "#/components/" + e.section + "/" + e.name }

// componentsOf is a components object declaring each of entries, as value
// spells it, one key per section.
func componentsOf(entries []networkEntry, value func(networkEntry) string) string {
	bySection := map[string]string{}
	for _, e := range entries {
		bySection[e.section] += "    " + e.name + ": " + value(e) + "\n"
	}
	out := "components:\n"
	for _, section := range slices.Sorted(maps.Keys(bySection)) {
		out += "  " + section + ":\n" + bySection[section]
	}
	return out
}

// assertNoLocalRead holds what compileOverTheNetwork returns to the boundary:
// every request named an http URL on the network documents' host or on other,
// and every error says a reference was refused for naming no such URL, or,
// where refused is false, there is none.
func assertNoLocalRead(t *testing.T, got networkCompile, other string, refused bool) {
	t.Helper()
	for _, l := range got.locations {
		u, err := url.Parse(l)
		require.NoError(t, err, l)
		assert.Equal(t, "http", u.Scheme, "nothing local is read: %s", l)
		assert.Contains(t, []string{got.host, other}, u.Host, l)
	}
	var why []string
	for _, d := range got.diags {
		if d.Severity == ir.SeverityError {
			why = append(why, d.Message)
		}
	}
	if !refused {
		assert.Empty(t, why, "it is read from the network")
		return
	}
	require.NotEmpty(t, why)
	for _, w := range why {
		assert.Contains(t, w, bind.OffNetwork, "the reference is refused where it is written")
	}
}

// TestCompile_AFileNamedLikeAURLIsReadAsAFile pins a relative $ref to a file
// whose name's first segment holds a colon, written with a leading "./" as RFC
// 3986 §4.2 writes one: it names that file, though the scheme before the colon
// is one a reader reads, from a schema and from a parameter, with external
// references allowed or not, the spec located by the engine or named by a
// relative path alone. Not parallel: it changes directory.
func TestCompile_AFileNamedLikeAURLIsReadAsAFile(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for name, body := range map[string]string{
		"file:x.yaml": "F: {type: integer}\nP: {name: f, in: query, schema: {type: integer}}\n",
		"http:h.yaml": "H: {type: boolean}\nP: {name: h, in: query, schema: {type: boolean}}\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /x:\n    get:\n" +
		"      parameters:\n        - {$ref: './file:x.yaml#/P'}\n        - {$ref: './http:h.yaml#/P'}\n" +
		"      responses: {\"200\": {description: ok}}\n" +
		"components:\n  schemas:\n    F: {$ref: './file:x.yaml#/F'}\n    H: {$ref: './http:h.yaml#/H'}\n")
	t.Chdir(dir)
	for _, location := range []string{"", filepath.Join(dir, "root.yaml")} {
		for _, allow := range []bool{false, true} {
			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "root.yaml", Location: location, Data: root}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: allow}})
			require.NoError(t, err)
			require.NotNil(t, doc)

			assert.Empty(t, diags, "located at %q, external references allowed: %v", location, allow)
			assert.Contains(t, doc.Types, ids.In("file:x.yaml").AnonType("/F"))
			assert.Contains(t, doc.Types, ids.In("http:h.yaml").AnonType("/H"))
			require.Len(t, doc.Services[0].Groups[0].Operations, 1)
			assert.Len(t, doc.Services[0].Groups[0].Operations[0].Params, 2, "each parameter is read")
		}
	}
}
