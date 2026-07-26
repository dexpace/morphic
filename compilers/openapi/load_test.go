package openapi

import (
	"errors"
	"os"
	"testing"

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
	assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, got.Format)
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
	var unresolved int
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			unresolved++
		}
	}
	assert.GreaterOrEqual(t, unresolved, 1, "external resolution validation errors surface as diagnostics")
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

func TestNodeToRaw(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nodeToRaw(nil), "nil node")
	assert.Nil(t, nodeToRaw(&yaml.Node{Kind: yaml.Kind(99)}), "decode error")
	assert.Nil(t, nodeToRaw(yamlNode(t, "1: a\n2: b")), "int-key map: json marshal error")
	raw := nodeToRaw(yamlNode(t, "{a: 1}"))
	assert.JSONEq(t, `{"a":1}`, string(raw))
}

func TestRawChildNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawChildNode(nil, "x"), "nil root")
	assert.Nil(t, rawChildNode(scalarNode("!!str", "x"), "k"), "non-mapping root")

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2"), &doc))
	// doc is a DocumentNode wrapping the mapping — exercises the unwrap branch.
	got := rawChildNode(&doc, "b")
	require.NotNil(t, got)
	assert.Equal(t, "2", got.Value)
	assert.Nil(t, rawChildNode(&doc, "missing"), "absent key")
}

func TestRawPropertyNode_NilSchema(t *testing.T) {
	t.Parallel()
	assert.Nil(t, rawPropertyNode(nil, "x"))
}
