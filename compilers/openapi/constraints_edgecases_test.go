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
