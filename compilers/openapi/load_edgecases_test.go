package openapi

import (
	"errors"
	"os"
	"testing"

	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

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
