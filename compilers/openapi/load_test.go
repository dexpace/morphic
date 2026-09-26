package openapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
// the resolver's reason. The #74 shape — a $ref this compile follows into
// another document but cannot lower any further — is the one case the load
// phase has nothing to report, so the lowering's own report survives
// withoutRereported instead of being dropped as the duplicate it is everywhere
// else.
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

	t.Run("the #74 shape keeps the lowering's report", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
			"openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\npaths: {}\n"+
				"components:\n  schemas:\n    X: {type: string}\n"), 0o600))
		rootPath := filepath.Join(dir, "root.yaml")
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  schemas:\n    S: {$ref: 'other.yaml#/components/schemas/X'}\n"

		doc, diags, err := New().Compile(t.Context(),
			[]compilers.Source{{Path: rootPath, Data: []byte(root)}},
			compilers.Options{FormatOptions: Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		require.NotNil(t, doc)

		msg := openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/S")
		assert.Equal(t, `unresolved $ref "other.yaml#/components/schemas/X"`, msg,
			"the lowering's own report survives: the load phase resolved this reference successfully")
	})
}

// TestCompile_AFindingInAnExternalDocumentKeepsItsRule is the #537 repro: a
// finding inside a document reached only through an external $ref keeps its own
// rule and severity, reported at the $ref rather than as an unresolved
// reference, with its position in that document going into the message rather
// than into a Provenance the compiler has no source table entry for (GitHub
// #74).
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
		compilers.Options{FormatOptions: Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)

	msg := openapitest.DiagMessageAt(t, diags, diag.Validation+"/validation-required-field", ir.SeverityError, "/paths/~1x")
	assert.Contains(t, msg, "`parameter.in` is required")
	assert.Contains(t, msg, "of the document the $ref resolves to")
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
