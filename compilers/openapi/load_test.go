package openapi

import (
	"encoding/json/jsontext"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// resolverPanicSpec is a document the parser accepts and the resolver faults on:
// `B: {$ref}` is a mapping whose $ref key carries no value, so populating the
// response reference that points at it nil-dereferences inside speakeasy.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const resolverPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

// TestCompile_ResolverPanicIsADiagnostic is the end-to-end half: the panic
// becomes an ordinary unresolved-ref diagnostic, so a malformed spec is refused
// as a spec problem rather than reported as a Go error or crashing the process.
func TestCompile_ResolverPanicIsADiagnostic(t *testing.T) {
	t.Parallel()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "resolver-panic.yaml", Data: []byte(resolverPanicSpec)}},
		compilers.Options{})
	require.NoError(t, err, "a malformed spec is a spec problem, not a Go error")
	assert.NotNil(t, doc, "resolution failure does not stop the document being lowered")
	assertHasErrorCode(t, diags, diag.UnresolvedRef)
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

		doc, diags, err := New().Compile(t.Context(),
			[]compilers.Source{{Path: rootPath, Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})
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

		_, diags, err := New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(t.TempDir(), "root.yaml"), Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})
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
// (GitHub #1044): one unresolved-ref error at the $ref, naming the kind as a
// schema $ref to a position holding no schema is worded, and, when the chain
// went through another reference object first, the $ref written at the hop
// before. No finding of the parser's stands beside it, and it does not depend
// on which $ref reaches the target first. A target with findings of its own
// still reports them.
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
			pointer: "/paths/~1x/get/parameters/0",
			message: `unresolved $ref "#/components/parameters/P", through "#/x-lib/S": ` +
				"it names a position that holds no parameter",
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
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "mid.yaml"),
			[]byte("P: {$ref: '#/S'}\nS: just a string\n"), 0o600))
		root := openapitest.PathsSpec("  /x:\n    get:\n      parameters: [{$ref: 'mid.yaml#/P'}]\n" +
			"      responses: {\"200\": {description: ok}}\n")

		_, diags, err := New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})
		require.NoError(t, err)

		want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: diag.UnresolvedRef,
			Message: `unresolved $ref "mid.yaml#/P", through "#/S" in ` + filepath.Join(dir, "mid.yaml") +
				": it names a position that holds no parameter",
			Provenance: ir.Provenance{Pointer: "/paths/~1x/get/parameters/0"}}}
		assert.Empty(t, cmp.Diff(want, diags), "the hop before names where it is written")
	})

	t.Run("a target with findings of its own still reports them", func(t *testing.T) {
		t.Parallel()
		_, diags := parseFull(t, openapitest.PathsSpec(strings.Replace(refParam, "x-lib/S", "x-lib/P", 1))+
			"x-lib: {P: {name: q, in: nowhere}}\n")

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, jsontext.Pointer("/x-lib/P/in"), diags[0].Provenance.Pointer, "at the node it is about")
		assert.False(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "the $ref resolved to an object")
	})

	t.Run("a target in another document with findings of its own still reports them", func(t *testing.T) {
		t.Parallel()
		root := openapitest.PathsSpec(strings.Replace(refParam, "#/x-lib/S", "other.yaml#/x-lib/P", 1))

		diags := compileBesideExpectingErrors(t, "x-lib: {P: {name: q, in: nowhere}}\n", root)

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, diag.Validation+"/validation-allowed-values", diags[0].Code)
		assert.Equal(t, jsontext.Pointer("/paths/~1x/get/parameters/0"), diags[0].Provenance.Pointer, "at the $ref that reached it")
		assert.Contains(t, diags[0].Message, "other.yaml", "naming the document it is in")
	})
}

// TestCompile_AFindingInASchemaOnlyAnotherDocumentReachesIsPlacedThere pins
// where the validation of a schema built for raw YAML is reported when only
// another document's content reaches it, through a parameter that document
// declares: at that document's $ref reaching it, in its Document.Sources
// entry, never at the same pointer in the source, which holds nothing there.
func TestCompile_AFindingInASchemaOnlyAnotherDocumentReachesIsPlacedThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
		"components:\n  parameters:\n    P: {name: p, in: query, schema: {$ref: '#/x-lib/B'}}\n"+
			"x-lib:\n  B: {type: string, minLength: notanumber}\n"), 0o600))
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a:\n    get:\n" +
		"      parameters: [{$ref: './other.yaml#/components/parameters/P'}]\n" +
		"      responses: {\"200\": {description: ok}}\n"

	_, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}})
	require.NoError(t, err)

	require.NotEmpty(t, diags)
	for _, d := range diags {
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/parameters/P/schema"}, d.Provenance, "%+v", d)
		assert.True(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "%+v", d)
	}
}

// TestCompile_AFindingInAnExternalDocumentKeepsItsRule is the #537 repro: a
// finding the parser's validation makes inside a document reached only through
// an external $ref keeps its own rule and severity, reported at the $ref rather
// than as an unresolved reference, with its position in that document in the
// message. Placing it in that document's own Document.Sources entry is GitHub
// #1030.
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

	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: rootPath, Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	msg := openapitest.DiagMessageAt(t, diags, diag.Validation+"/validation-required-field", ir.SeverityError, "/paths/~1x")
	assert.Contains(t, msg, "`parameter.in` is required")
	assert.True(t, strings.HasSuffix(msg, ", at 7:11 of "+filepath.Join(dir, "other.yaml")),
		"its position, in the document it is in: %q", msg)
}

// TestLoad_RecoverableLiteralSuppressesFindingAmongOtherScalars pins the
// end-to-end shape the candidacy rule has to preserve. The library reports this
// document as invalid JSON because of the .5, and names that character in the
// message; the unconvertible custom tag beside it is a bystander that converts
// fine on its own, so the finding is still an artifact and must not surface.
func TestLoad_RecoverableLiteralSuppressesFindingAmongOtherScalars(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(`    S: {type: string, default: !custom foo, example: .5}`))
	assert.False(t, openapitest.HasDiag(diags, diag.Validation+"/"+string(validation.RuleValidationInvalidSyntax)),
		"a finding a recoverable literal explains stays suppressed: %+v", diags)
}

// TestCompile_OneFileUnderTwoNamesIsOneDocument pins that, with external
// references allowed, a file references spell two ways, by a path relative to
// the spec and by an absolute one, is one document: one Document.Sources
// entry, its declarations lowered once under one label, each finding in it
// reported once, and every ID the same whether the spec itself was named by a
// relative path or an absolute one.
func TestCompile_OneFileUnderTwoNamesIsOneDocument(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "inner"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inner", "real.yaml"), []byte(
		"components:\n  schemas:\n    S: {type: object, properties: {gone: {$ref: '#/components/schemas/Missing'}}}\n"), 0o600))
	root := []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A: {$ref: './inner/real.yaml#/components/schemas/S'}\n" +
		"    B: {$ref: '" + filepath.Join(dir, "inner", "real.yaml") + "#/components/schemas/S'}\n")
	t.Chdir(dir)
	compileAt := func(path string) (*ir.Document, []ir.Diagnostic) {
		t.Helper()
		doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: path, Data: root}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true}})
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
		doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: path, Data: root}},
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
// nothing, fails a schema $ref, which the compiler binds, and a parameter's,
// which the parser's resolver follows, with one wording, whether the $ref names
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
				doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: root}},
					compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: allow}})
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
// where the resolver joins it to that same "foo:b.yaml". Both routes word the
// refusal alike.
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
			doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: run.spec, Data: root}},
				compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: allow}})
			require.NoError(t, err)
			require.NotNil(t, doc)

			got := make([]string, 0, len(diags))
			for _, d := range diags {
				got = append(got, string(d.Provenance.Pointer)+": "+d.Message[strings.LastIndex(d.Message, ": ")+2:])
				assert.Contains(t, d.Message, ": the document it names could not be read: external document foo:b.yaml refused: ",
					"the binder and the parser's resolver word one refusal alike")
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
		doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: path, Data: root}},
			compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: allow}})
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
		doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: root}},
			compilers.Options{Reader: reader.Disk{Client: srv.Client()}, FormatOptions: Options{AllowExternalRefs: allow}})
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
// the spec itself resolves. Under the first order, with the spec named by an
// absolute path, an earlier reading refused the relative $ref too.
func TestCompile_ByDefaultEachReferenceIsJudgedByItsOwnSpelling(t *testing.T) {
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
	t.Chdir(dir)
	const refused = "the $ref names it by an absolute path, which a read follows only when external references are allowed"

	for _, order := range [][2]string{{"C", "D"}, {"D", "C"}} {
		types := make([]ir.TypeRegistry, 0, 2)
		for _, path := range []string{"root.yaml", self} {
			doc, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: path, Data: spec(order[0], order[1])}},
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
