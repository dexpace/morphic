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
)

// TestNumericBoundKeyword_UnderlyingNotTypeMismatch drives the errors.As guard: a
// type-mismatch-ruled finding whose underlying error is not a *TypeMismatchError
// names no bound keyword, so the artifact classifier declines it.
func TestNumericBoundKeyword_UnderlyingNotTypeMismatch(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: errors.New("not a type mismatch"),
	}
	name, ok := numericBoundKeyword(verr)
	assert.False(t, ok)
	assert.Empty(t, name)
}

// TestTypeMismatchOnValidNumber_NilNode covers the nil-node guard: a bound-keyword
// mismatch that carries no node cannot be reclassified as a numeric artifact.
func TestTypeMismatchOnValidNumber_NilNode(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.minimum"},
		Node:            nil,
	}
	assert.False(t, typeMismatchOnValidNumber(verr))
}

// TestTypeMismatchOnValidNumber_MappingWithoutKeyword covers the no-literals arm:
// a mismatch whose node is the enclosing schema mapping but which carries no
// instance of the bound keyword yields no literals, so the finding is genuine.
func TestTypeMismatchOnValidNumber_MappingWithoutKeyword(t *testing.T) {
	t.Parallel()
	verr := validation.Error{
		Rule:            validation.RuleValidationTypeMismatch,
		UnderlyingError: &validation.TypeMismatchError{ParentName: "schema.maximum"},
		Node:            &yaml.Node{Kind: yaml.MappingNode},
	}
	assert.False(t, typeMismatchOnValidNumber(verr))
}

// TestKeywordScalars_NilAndDepthGuards covers the recursion guards: a nil node and
// a node past the scan-depth cap both yield no literals.
func TestKeywordScalars_NilAndDepthGuards(t *testing.T) {
	t.Parallel()
	assert.Nil(t, keywordScalars(nil, "minimum", 0))
	deep := &yaml.Node{Kind: yaml.MappingNode}
	assert.Nil(t, keywordScalars(deep, "minimum", maxSchemaScanDepth+1))
}

// TestKeywordScalars_SequenceBranch covers the non-mapping recursion arm: a
// sequence node is walked child-by-child to reach a nested keyword literal.
func TestKeywordScalars_SequenceBranch(t *testing.T) {
	t.Parallel()
	nested := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "minimum"},
		{Kind: yaml.ScalarNode, Value: "5"},
	}}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{nested}}
	assert.Equal(t, []string{"5"}, keywordScalars(seq, "minimum", 0))
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

// TestComponentConstraints_NonSchemaInputs covers the first early return: a nil,
// boolean, or reference component has no scalar body to carry constraints.
func TestComponentConstraints_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Nil(t, l.componentConstraints(nil, "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromBool(true), "/p"))
	assert.Nil(t, l.componentConstraints(oas3.NewJSONSchemaFromReference("#/components/schemas/Other"), "/p"))
}

// TestComponentConstraints_EmptyRefSchema covers the second early return: a schema
// whose $ref pointer is present but empty is not a reference (IsReference is false)
// yet its Ref field is set, so it holds no readable constraint body. The parser
// never emits this shape, so it is built by hand.
func TestComponentConstraints_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})
	assert.Nil(t, l.componentConstraints(js, "/p"))
}

// TestComponentConstraints_DiagnosticProvenance covers the diagnostic-stamping
// loop: a scalar component whose numeric bound is not a valid number surfaces a
// numeric-precision diagnostic stamped with the component's own pointer.
func TestComponentConstraints_DiagnosticProvenance(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    BadN: {type: number, minimum: hello}\n")
	_, diags := lowerSpec(t, spec)
	var found bool
	for _, d := range diags {
		if d.Code == codeNumericPrecision {
			found = true
			assert.NotEmpty(t, d.Provenance.Pointer, "component numeric diagnostic carries its pointer")
		}
	}
	assert.True(t, found, "a non-numeric component bound warns")
}
