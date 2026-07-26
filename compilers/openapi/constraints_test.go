package openapi

import (
	"errors"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/speakeasy-api/openapi/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

func TestConstraintsFromSchema_NilSchema(t *testing.T) {
	t.Parallel()
	c, diags := constraintsFromSchema(nil, false)
	assert.Nil(t, c)
	assert.Nil(t, diags)
}

func TestPreserveKeyword_NilRaw(t *testing.T) {
	t.Parallel()
	l := &lowerer{}
	m := &ir.Model{}
	l.preserveKeyword(m, "openapi:not", nil, "/p", "not")
	assert.Nil(t, m.Extensions, "nil raw is a no-op")
	assert.Empty(t, l.diags)
}

func TestConstraints_ExclusiveBoolean30(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.0.3", `    S:
      type: object
      properties:
        n:
          type: integer
          minimum: 5
          exclusiveMinimum: true
          maximum: 10
          exclusiveMaximum: true
`)
	// The library models exclusiveMinimum/Maximum as numbers and flags the valid
	// 3.0 boolean form with a type-mismatch; because those keywords are Morphic's
	// to own, load suppresses that false positive, so a valid 3.0 boolean exclusive
	// bound lowers cleanly with the flag set and no error diagnostic.
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	c := propConstraints(t, doc, "S", "n")
	assert.True(t, c.ExclusiveMin)
	assert.True(t, c.ExclusiveMax)
	require.NotNil(t, c.Min)
	assert.Equal(t, ir.BigVal("5"), *c.Min)
}

func TestConstraints_ExclusiveNumeric31(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        n:
          type: number
          exclusiveMinimum: 1.5
          exclusiveMaximum: 9.5
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	c := propConstraints(t, doc, "S", "n")
	assert.True(t, c.ExclusiveMin)
	assert.True(t, c.ExclusiveMax)
	require.NotNil(t, c.Min)
	require.NotNil(t, c.Max)
	assert.Equal(t, ir.BigVal("1.5"), *c.Min)
	assert.Equal(t, ir.BigVal("9.5"), *c.Max)
}

func TestConstraints_MalformedNumericLiterals(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    S:
      type: object
      properties:
        a: {type: number, minimum: .inf}
        b: {type: number, exclusiveMinimum: .inf}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	var count int
	for _, d := range diags {
		if d.Code == codeNumericPrecision {
			count++
			assert.Equal(t, ir.SeverityError, d.Severity)
		}
	}
	assert.GreaterOrEqual(t, count, 2, "both malformed literals error")
}

func TestConstraints_LosslessNumericLiterals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		literal string
		want    ir.BigVal
	}{
		{"beyond float64 range", "1.8e308", "1.8e308"},
		{"far beyond float64 range", "1e400", "1e400"},
		{"leading dot spelling", ".5", "0.5"},
		{"trailing dot spelling", "5.", "5"},
		{"huge integer beyond int64", "123456789012345678901234567890", "123456789012345678901234567890"},
		{"high-precision decimal", "0.12345678901234567890123456789", "0.12345678901234567890123456789"},
		{"exponential notation", "6.022e23", "6.022e23"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := componentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: " + tc.literal + "}\n")
			doc, diags := lowerSpec(t, spec)
			// A valid number, however spelled, is accepted with no error: the
			// library's float64/JSON complaint is not surfaced.
			requireNoErrorDiags(t, diags)
			c := propConstraints(t, doc, "S", "n")
			require.NotNil(t, c.Min)
			assert.Equal(t, tc.want, *c.Min)
		})
	}
}

// countErrors returns the error-severity diagnostics carrying code.
func countErrors(diags []ir.Diagnostic, code string) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			n++
		}
	}
	return n
}

// TestConstraints_HoistedSubSchemaBadBoundSingleError pins that a malformed bound
// on a component-property sub-schema reached by a $ref is reported exactly once,
// even though the sub-schema's constraints are read from two positions (the owning
// property and the $ref hoist). Without per-pointer de-duplication both reads
// would emit the same error at the same pointer.
func TestConstraints_HoistedSubSchemaBadBoundSingleError(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    Foo:\n      type: object\n      properties:\n        bar: {type: number, minimum: hello}\n" +
		"    User:\n      type: object\n      properties:\n        b: {$ref: '#/components/schemas/Foo/properties/bar'}\n")
	_, diags := lowerSpec(t, spec)
	assert.Equal(t, 1, countErrors(diags, codeNumericPrecision),
		"one error for the shared bad bound, got: %+v", diags)
}

// TestConstraints_ExclusiveWrongDialectForm pins that an exclusiveMinimum/Maximum
// written in the wrong form for the document's dialect is reported as a single
// error, not silently accepted as a degenerate constraint: a boolean under the
// 2020-12 dialect (3.1/3.2) and a number under 3.0.
func TestConstraints_ExclusiveWrongDialectForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, value string
	}{
		{"boolean under 3.1", "3.1.0", "true"},
		{"boolean under 3.2", "3.2.0", "false"},
		{"number under 3.0", "3.0.3", "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := componentSpecVer(tc.version,
				"    S:\n      type: object\n      properties:\n        n: {type: number, exclusiveMinimum: "+tc.value+"}\n")
			doc, diags := lowerSpec(t, spec)
			require.NotNil(t, doc)
			assert.Equal(t, 1, countErrors(diags, codeExclusiveBoundForm),
				"one dialect-form error, got: %+v", diags)
			// The degenerate bound is dropped, not recorded.
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			for _, p := range m.Properties {
				if p.WireName == "n" && p.Constraints != nil {
					assert.False(t, p.Constraints.ExclusiveMin, "wrong-form exclusive bound is not set")
				}
			}
		})
	}
}

func TestConstraints_TypeWrongBoundYieldsSingleError(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: hello}\n")
	_, diags := lowerSpec(t, spec)
	// Exactly one diagnostic: Morphic's error with the schema's own provenance.
	// The library emits two redundant float64 type-mismatch findings on the same
	// keyword; load suppresses both because Morphic owns numeric-bound keywords.
	require.Len(t, diags, 1, "one diagnostic for a type-wrong bound, got: %+v", diags)
	assert.Equal(t, codeNumericPrecision, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.NotEmpty(t, diags[0].Provenance.Pointer)
}

func TestConstraints_NonNumericMinimumErrors(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: hello}\n")
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	// A genuinely non-numeric bound is never dropped silently: Morphic owns the
	// keyword and reports it as an error with the property's exact provenance.
	var reported bool
	for _, d := range diags {
		if d.Code == codeNumericPrecision {
			reported = true
			assert.Equal(t, ir.SeverityError, d.Severity)
		}
	}
	assert.True(t, reported, "a non-numeric minimum yields an error diagnostic")
}

// propConstraints returns the constraints of a named model's property.
func propConstraints(t *testing.T, doc *ir.Document, model, wire string) *ir.Constraints {
	t.Helper()
	m, ok := typeByName(doc, model).(*ir.Model)
	require.True(t, ok)
	for _, p := range m.Properties {
		if p.WireName == wire {
			require.NotNil(t, p.Constraints, "property %s has constraints", wire)
			return p.Constraints
		}
	}
	t.Fatalf("property %s not found", wire)
	return nil
}

func TestApplyExclusive_NumericWithoutRootNode(t *testing.T) {
	t.Parallel()
	f := 5.0
	s := &oas3.Schema{ExclusiveMinimum: &values.EitherValue[bool, bool, float64, float64]{Right: &f}}
	c := &ir.Constraints{}
	diags := applyExclusive(c, s, true, false)
	// The numeric arm is taken (2020-12 dialect, numeric value) but there is no raw
	// node to read the exact literal from, so nothing is set and no diagnostic.
	assert.Nil(t, diags)
	assert.False(t, c.ExclusiveMin)
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

// compileComponentSpec compiles an inline spec and fails on any error diagnostic.
func compileComponentSpec(t *testing.T, spec string) *ir.Document {
	t.Helper()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "t.yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	for _, d := range diags {
		require.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
	require.NotNil(t, doc)
	return doc
}

// TestCompile_ComponentScalarConstraintsPreserved pins that a top-level scalar
// component keeps its numeric constraints losslessly: a magnitude beyond float64
// range and a valid leading-dot spelling both survive to the exact BigVal,
// whether the component declares a type or not. A float64 path silently dropped
// the first and rejected the second, and the attachment path dropped both.
func TestCompile_ComponentScalarConstraintsPreserved(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents: {schemas: {N: "
	cases := []struct {
		name    string
		body    string
		wantMin ir.BigVal
	}{
		{"typeless-beyond-float64", "{minimum: 1.8e308}}}", "1.8e308"},
		{"leading-dot-canonicalized", "{minimum: .5}}}", "0.5"},
		{"typed-beyond-float64", "{type: number, minimum: 1.8e308}}}", "1.8e308"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := compileComponentSpec(t, head+tc.body+"\n")
			sc, ok := doc.Types["t/openapi/components/schemas/N"].(*ir.Scalar)
			require.True(t, ok, "N lowers to a scalar")
			require.NotNil(t, sc.Constraints, "component scalar keeps its constraints")
			require.NotNil(t, sc.Constraints.Min)
			assert.Equal(t, tc.wantMin, *sc.Constraints.Min)
		})
	}
}

// TestCompile_HoistedSubSchemaConstraintsPreserved pins that a $ref to an internal
// scalar sub-schema keeps that sub-schema's numeric constraints on the hoisted
// alias node the reference resolves to. The hoisted node is distinct from the
// property whose position the sub-schema also occupies, so before the alias
// carried constraints a $ref to a bounded scalar sub-schema silently dropped them.
func TestCompile_HoistedSubSchemaConstraintsPreserved(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\n" +
		"info: {title: t, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n" +
		"  schemas:\n" +
		"    Foo:\n" +
		"      type: object\n" +
		"      properties:\n" +
		"        bar: {type: number, minimum: 5, maximum: 10}\n" +
		"    Uses:\n" +
		"      type: object\n" +
		"      properties:\n" +
		"        b: {$ref: '#/components/schemas/Foo/properties/bar'}\n"
	doc := compileComponentSpec(t, spec)

	uses, ok := doc.Types["t/openapi/components/schemas/Uses"].(*ir.Model)
	require.True(t, ok, "Uses lowers to a model")
	var target ir.TypeID
	for _, p := range uses.Properties {
		if p.WireName == "b" {
			target = p.Type.Target
		}
	}
	require.NotEmpty(t, target, "property b resolves to the hoisted sub-schema")

	sc, ok := doc.Types[target].(*ir.Scalar)
	require.True(t, ok, "the hoisted sub-schema is a scalar alias")
	require.NotNil(t, sc.Constraints, "the hoisted scalar keeps its constraints")
	require.NotNil(t, sc.Constraints.Min)
	require.NotNil(t, sc.Constraints.Max)
	assert.Equal(t, ir.BigVal("5"), *sc.Constraints.Min)
	assert.Equal(t, ir.BigVal("10"), *sc.Constraints.Max)
}
