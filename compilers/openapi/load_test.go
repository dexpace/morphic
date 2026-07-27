package openapi

import (
	"errors"
	"os"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

const minimal31 = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
`

func TestLoad_Minimal31(t *testing.T) {
	t.Parallel()
	got, diags, err := load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "openapi@3.1", got.Source.Format, "3.1.0 normalizes to 3.1")
	assert.Equal(t, "spec.yaml", got.Source.Path)
	assert.Len(t, got.Source.Hash, 64)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

func TestLoad_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "old.yaml", Data: []byte("swagger: \"2.0\"\ninfo: {title: T, version: \"1\"}\npaths: {}\n")}
	got, diags, err := load(t.Context(), 0, src, Options{}.withDefaults())
	require.NoError(t, err) // spec problems are diagnostics, not Go errors
	assert.Nil(t, got)
	require.NotEmpty(t, diags)
	assert.Equal(t, codeUnsupportedVersion, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestLoad_ValidationErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	// paths entry with a bogus structure triggers library validation errors.
	src := compilers.Source{Path: "bad.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {/x: {get: {responses: \"nope\"}}}\n")}
	_, diags, err := load(t.Context(), 0, src, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotEmpty(t, diags)
	found := false
	for _, d := range diags {
		if d.Provenance.Pointer != "" {
			found = true
		}
	}
	assert.True(t, found, "diagnostics should carry line:col provenance")
}

// TestLoad_ExternalRefResolutionErrors drives the resErrs branch of load: an
// external $ref to a malformed response yields per-reference validation errors
// (not a single hard error), which load forwards as unresolved-ref diagnostics.
func TestLoad_ExternalRefResolutionErrors(t *testing.T) {
	t.Parallel()
	path := "../../testdata/openapi/resolve_main_external.yaml"
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := load(t.Context(), 0, compilers.Source{Path: path, Data: data}, Options{}.withDefaults())
	require.NoError(t, loadErr)
	require.NotNil(t, ld)
	assert.GreaterOrEqual(t, countDiagsAt(diags, codeUnresolvedRef, ir.SeverityError), 1,
		"external resolution validation errors surface as diagnostics")
}

// TestUnmarshal_RecoversParserPanic pins the no-panics-escape invariant: the
// third-party parser faults on a whitespace-only document, and unmarshal must
// convert that panic into an errParse error instead of letting it escape.
func TestUnmarshal_RecoversParserPanic(t *testing.T) {
	t.Parallel()
	doc, valErrs, err := unmarshal(t.Context(), []byte(" "))
	require.Error(t, err)
	assert.ErrorIs(t, err, errParse)
	assert.Nil(t, doc)
	assert.Nil(t, valErrs)
}

// resolverPanicSpec is a document the parser accepts and the resolver faults on:
// `B: {$ref}` is a mapping whose $ref key carries no value, so populating the
// response reference that points at it nil-dereferences inside speakeasy.
// FuzzCycleDetector found it; the same bytes are committed as a corpus entry.
const resolverPanicSpec = "openapi: 3.0\ncomponents:\n responses:\n  000: {$ref: '#/B'}\nB: {$ref}"

// TestResolveAll_RecoversResolverPanic pins the resolve half of the
// no-panics-escape invariant. unmarshal has guarded the parser since GitHub #12;
// ResolveAllReferences was left bare, so a document that parses cleanly and
// faults during resolution took the caller's process with it.
func TestResolveAll_RecoversResolverPanic(t *testing.T) {
	t.Parallel()
	doc, _, err := unmarshal(t.Context(), []byte(resolverPanicSpec))
	require.NoError(t, err, "the parser accepts this document")
	require.NotNil(t, doc)

	resErrs, err := resolveAll(t.Context(), doc, soa.ResolveAllOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, errParse)
	assert.Contains(t, err.Error(), "reference resolver panicked")
	assert.Nil(t, resErrs, "a partially-populated result never leaks")
}

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
	assertHasErrorCode(t, diags, codeUnresolvedRef)
}

func TestMapSeverity(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.SeverityWarning, mapSeverity(validation.Severity("warning")))
	assert.Equal(t, ir.SeverityInfo, mapSeverity(validation.Severity("hint")))
	assert.Equal(t, ir.SeverityError, mapSeverity(validation.Severity("error")))
	assert.Equal(t, ir.SeverityError, mapSeverity(validation.Severity("")))
}

func TestAsValidationError(t *testing.T) {
	t.Parallel()
	v := validation.Error{Rule: "r"}
	got, ok := asValidationError(v)
	assert.True(t, ok, "by value")
	assert.Equal(t, "r", got.Rule)

	pv := &validation.Error{Rule: "p"}
	got, ok = asValidationError(pv)
	assert.True(t, ok, "by pointer")
	assert.Equal(t, "p", got.Rule)

	_, ok = asValidationError(errors.New("plain"))
	assert.False(t, ok, "plain error is not a validation error")
}

func TestValidationDiag(t *testing.T) {
	t.Parallel()
	structured := validationDiag(0, validation.Error{Severity: "warning", Rule: "dup-tag", UnderlyingError: errors.New("x")})
	assert.Equal(t, ir.SeverityWarning, structured.Severity)
	assert.Equal(t, codeValidation+"/dup-tag", structured.Code)

	bare := validationDiag(3, errors.New("plain problem"))
	assert.Equal(t, ir.SeverityError, bare.Severity)
	assert.Equal(t, codeValidation, bare.Code)
	assert.Equal(t, 3, bare.Provenance.Source)
}

func TestResolveDiag(t *testing.T) {
	t.Parallel()
	structured := resolveDiag(0, validation.Error{Severity: "error", Rule: "bad-ref", UnderlyingError: errors.New("x")})
	assert.Equal(t, codeUnresolvedRef, structured.Code)
	assert.NotEmpty(t, structured.Provenance.Pointer, "line:col provenance from validation error")

	bare := resolveDiag(2, errors.New("io problem"))
	assert.Equal(t, codeUnresolvedRef, bare.Code)
	assert.Equal(t, 2, bare.Provenance.Source)
}

// TestIsNumericBoundKeyword_UnderlyingNotTypeMismatch drives the errors.As guard:
// a type-mismatch-ruled finding whose underlying error is not a *TypeMismatchError
// names no bound keyword, so the classifier declines to suppress it.
func TestIsNumericBoundKeyword_UnderlyingNotTypeMismatch(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: errors.New("not a type mismatch"),
	}
	assert.False(t, isNumericBoundKeyword(verr))
}

// TestIsNumericBoundKeyword_NonBoundKeyword covers the not-in-map arm: a genuine
// type-mismatch on a keyword Morphic does not own (here `type`) is never
// suppressed, so the library's finding is kept.
func TestIsNumericBoundKeyword_NonBoundKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.type"},
	}
	assert.False(t, isNumericBoundKeyword(verr))
}

// TestIsNumericBoundKeyword_BoundKeyword covers the in-map arm: a type-mismatch on
// a numeric-bound keyword is recognized (whatever the parent path's prefix) so
// load suppresses the library's redundant float64 finding on it.
func TestIsNumericBoundKeyword_BoundKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.properties.n.minimum"},
	}
	assert.True(t, isNumericBoundKeyword(verr))
}

// TestInvalidSyntaxOnValidNumbers_NilNode covers the nil guard.
func TestInvalidSyntaxOnValidNumbers_NilNode(t *testing.T) {
	t.Parallel()
	assert.False(t, invalidSyntaxOnValidNumbers(nil))
}

// TestWalkNumericScalars_NilAndDepthGuards covers the recursion guards: neither a
// nil node nor a node past the scan-depth cap visits any scalar.
func TestWalkNumericScalars_NilAndDepthGuards(t *testing.T) {
	t.Parallel()
	var visited int
	visit := func(string) { visited++ }
	walkNumericScalars(nil, 0, visit)
	walkNumericScalars(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}, maxSchemaScanDepth+1, visit)
	assert.Zero(t, visited)
}
