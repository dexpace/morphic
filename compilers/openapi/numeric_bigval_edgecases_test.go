package openapi

import (
	"errors"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

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

// TestComponentConstraints_NonSchemaInputs covers the js-level early return: a nil,
// boolean, or reference component has no scalar body to carry constraints.
func TestComponentConstraints_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Nil(t, l.componentConstraints(nil, "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromBool(true), "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromReference("#/components/schemas/Other"), "/p"))
}

// TestSchemaConstraints_EmptyRefSchema covers the schemaConstraints early return: a
// schema whose $ref pointer is present but empty is not a reference (IsReference is
// false) yet its Ref field is set, so it holds no readable constraint body. The
// parser never emits this shape, so it is built by hand.
func TestSchemaConstraints_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})
	assert.Nil(t, l.componentConstraints(js, "/p"))
}

// TestComponentConstraints_DiagnosticProvenance covers the diagnostic-stamping
// loop: a scalar component whose numeric bound is not a valid number surfaces a
// numeric-precision error stamped with the component's own pointer.
func TestComponentConstraints_DiagnosticProvenance(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    BadN: {type: number, minimum: hello}\n")
	_, diags := lowerSpec(t, spec)
	var found bool
	for _, d := range diags {
		if d.Code == codeNumericPrecision {
			found = true
			assert.NotEmpty(t, d.Provenance.Pointer, "component numeric diagnostic carries its pointer")
			assert.Equal(t, ir.SeverityError, d.Severity, "a non-numeric bound is an error")
		}
	}
	assert.True(t, found, "a non-numeric component bound is reported")
}
