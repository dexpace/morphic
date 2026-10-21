package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestCompile_OneFileUnderTwoNamesIsOneDocument pins that a file references
// spell two ways, by a path relative to the spec and by an absolute one, is
// one document: one Document.Sources entry, its declarations lowered once
// under one label, each finding in it reported once, and every ID the same
// whether the spec itself was named by a relative path or an absolute one.
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
			compilers.Options{Reader: reader.Disk{}})
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
