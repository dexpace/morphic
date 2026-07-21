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
	// The library flags the 3.0 boolean exclusiveMinimum form with a type-mismatch
	// validation diagnostic, but still parses it, so lowering sets the flag.
	doc, _ := lowerSpec(t, spec)
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
			assert.Equal(t, ir.SeverityWarning, d.Severity)
		}
	}
	assert.GreaterOrEqual(t, count, 2, "both malformed literals warn")
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

func TestConstraints_NonNumericMinimumWarns(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    S:\n      type: object\n      properties:\n        n: {type: number, minimum: hello}\n")
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	// A genuinely non-numeric bound is never dropped silently; it is reported.
	var reported bool
	for _, d := range diags {
		if d.Code == codeNumericPrecision || d.Severity == ir.SeverityError {
			reported = true
		}
	}
	assert.True(t, reported, "a non-numeric minimum yields a diagnostic")
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
