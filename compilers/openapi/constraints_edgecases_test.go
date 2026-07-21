package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

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
