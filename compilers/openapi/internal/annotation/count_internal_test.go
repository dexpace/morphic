// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExactCount_TakesTheCountFromItsLiteral holds a count to the number its
// literal states as YAML 1.2's core schema reads it, not to the model's reading
// of it, and to none where the literal reads as no number.
func TestExactCount_TakesTheCountFromItsLiteral(t *testing.T) {
	t.Parallel()
	got := ExactCount(schemaFromYAMLUnvalidated(t, "minLength: 010"), "minLength", new(int64(8)))
	require.NotNil(t, got)
	assert.Equal(t, int64(10), *got, "010 is ten, whatever the model read")

	assert.Nil(t, ExactCount(schemaFromYAMLUnvalidated(t, "minLength: .inf"), "minLength", new(int64(0))),
		"an infinity, which reads as no number, states no count")
}

// TestExactCount_AFractionCostsNoMoreForAWiderExponent counts the allocations
// of judging a count written as a fraction at two exponents a hundred times
// apart: they are equal, as the literal's decimal is read and never built,
// where math/big would build the power of ten.
func TestExactCount_AFractionCostsNoMoreForAWiderExponent(t *testing.T) {
	// AllocsPerRun counts the whole process, so the test runs alone.
	allocs := func(exp string) float64 {
		s := schemaFromYAMLUnvalidated(t, "minItems: 1e-"+exp)
		count := new(int64(0))
		return testing.AllocsPerRun(3, func() {
			assert.Nil(t, ExactCount(s, "minItems", count))
		})
	}

	assert.Equal(t, allocs("1000"), allocs("100000"))
}
