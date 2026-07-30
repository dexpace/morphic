package load

import (
	"errors"
	"os"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

const minimal31 = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
`

func TestLoad_Minimal31(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}, Options{})
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
	got, diags, err := Load(t.Context(), 0, src, Options{})
	require.NoError(t, err) // spec problems are diagnostics, not Go errors
	assert.Nil(t, got)
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.UnsupportedVersion, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestLoad_ValidationErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	// paths entry with a bogus structure triggers library validation errors.
	src := compilers.Source{Path: "bad.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {/x: {get: {responses: \"nope\"}}}\n")}
	_, diags, err := Load(t.Context(), 0, src, Options{})
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
	path := "../../../../testdata/openapi/resolve_main_external.yaml"
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := Load(t.Context(), 0, compilers.Source{Path: path, Data: data}, Options{})
	require.NoError(t, loadErr)
	require.NotNil(t, ld)
	assert.GreaterOrEqual(t, countErrorsAt(diags, diag.UnresolvedRef), 1,
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
	assert.Equal(t, diag.Validation+"/dup-tag", structured.Code)

	bare := validationDiag(3, errors.New("plain problem"))
	assert.Equal(t, ir.SeverityError, bare.Severity)
	assert.Equal(t, diag.Validation, bare.Code)
	assert.Equal(t, 3, bare.Provenance.Source)
}

func TestResolveDiag(t *testing.T) {
	t.Parallel()
	structured := resolveDiag(0, validation.Error{Severity: "error", Rule: "bad-ref", UnderlyingError: errors.New("x")})
	assert.Equal(t, diag.UnresolvedRef, structured.Code)
	assert.NotEmpty(t, structured.Provenance.Pointer, "line:col provenance from validation error")

	bare := resolveDiag(2, errors.New("io problem"))
	assert.Equal(t, diag.UnresolvedRef, bare.Code)
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
	visit := func(*yaml.Node) { visited++ }
	walkNumericScalars(nil, 0, visit)
	walkNumericScalars(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}, maxSchemaScanDepth+1, visit)
	assert.Zero(t, visited)
}

// TestInvalidSyntaxOnValidNumbers_Candidacy pins which literals can excuse a
// JSON-syntax finding. Only a spelling JSON rejects is a candidate cause, and
// every candidate must be one Morphic recovers.
func TestInvalidSyntaxOnValidNumbers_Candidacy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		scalars []*yaml.Node
		want    bool
	}{
		{"leading dot", []*yaml.Node{scalarNode("!!float", ".5")}, true},
		{"octal", []*yaml.Node{scalarNode("!!int", "0644")}, true},
		{"separators", []*yaml.Node{scalarNode("!!int", "1_000")}, true},
		{"recoverable beside a json-valid literal",
			[]*yaml.Node{scalarNode("!!float", ".5"), scalarNode("!!int", "42")}, true},

		{"nothing to recover", []*yaml.Node{scalarNode("!!int", "42")}, false},
		{"infinity", []*yaml.Node{scalarNode("!!float", ".inf")}, false},
		{"recoverable beside an unrecoverable literal",
			[]*yaml.Node{scalarNode("!!float", ".5"), scalarNode("!!float", ".inf")}, false},
		// JSON accepts "-0", so normalizing it to "0" is not evidence that it
		// provoked anything.
		{"negative zero", []*yaml.Node{scalarNode("!!int", "-0")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := &yaml.Node{Kind: yaml.SequenceNode, Content: tc.scalars}
			assert.Equal(t, tc.want, invalidSyntaxOnValidNumbers(node))
		})
	}
}

// TestMatchSchemas_StopsOnMatchError drives the branch no document can reach:
// WalkItem.Match returns exactly what the matcher handed it, and the collector
// schemaFindings passes returns nil unconditionally. A hand-built item is the
// only way to observe the stop, and observing it is what says the walk ends
// there rather than skipping the item and carrying on — the difference between
// a smaller reconciliation and a wrong one.
func TestMatchSchemas_StopsOnMatchError(t *testing.T) {
	t.Parallel()
	visited := 0
	failing := soa.WalkItem{Match: func(soa.Matcher) error { return errors.New("walk stopped") }}
	after := soa.WalkItem{Match: func(m soa.Matcher) error {
		visited++
		return nil
	}}
	matchSchemas(func(yield func(soa.WalkItem) bool) {
		if !yield(failing) {
			return
		}
		yield(after)
	}, func(*oas3.JSONSchema[oas3.Referenceable]) error { return nil })
	assert.Zero(t, visited, "a failed match ends the walk rather than skipping one item")
}

// TestMetaSchemaVersionArtifacts_OtherMinorsUntouched pins the gate: only 3.2
// findings are reconciled, because the library defaults to the 3.1 meta-schema
// and reconciling 3.0 changes what a 3.0 document reports rather than removing a
// false positive.
func TestMetaSchemaVersionArtifacts_OtherMinorsUntouched(t *testing.T) {
	t.Parallel()
	doc, _, err := unmarshal(t.Context(), []byte(minimal31))
	require.NoError(t, err)
	assert.Nil(t, metaSchemaVersionArtifacts(t.Context(), doc, "3.1"))
	assert.Nil(t, metaSchemaVersionArtifacts(t.Context(), doc, "3.0"))
}

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

// scalarNode builds a bare scalar yaml.Node with the given tag and value.
func scalarNode(tag, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: val}
}
