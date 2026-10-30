// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestIsMultipleOf_JudgesExactly pins the multipleOf judgment the merge holds a
// value to. Each row is one a float division gets wrong, or one where no answer
// is claimed: a decimal step float64 cannot hold, values far apart in scale,
// magnitudes no rational builds, steps whose powers of two and five outrun the
// shift, and the one pair ok refuses, a zero step.
func TestIsMultipleOf_JudgesExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		v, m         string
		multiple, ok bool
	}{
		{"an integer multiple", "10", "5", true, true},
		{"an integer that is not one", "7", "2", false, true},
		{"a decimal step float64 cannot hold", "0.3", "0.1", true, true},
		{"a last digit finer than the step's", "0.35", "0.1", false, true},
		{"a fraction against an integral step", "2.5", "2", false, true},
		{"a fraction against a fractional step", "2.5", "0.5", true, true},
		{"zero, a multiple of anything", "0", "3", true, true},
		{"a negative multiple", "-6", "3", true, true},
		{"the step's sign does not matter", "6", "-3", true, true},
		{"a step larger than the value", "2", "5", false, true},
		{"magnitudes far apart", "1e100", "5", true, true},
		{"a step far below the value", "1", "1e-100", true, true},
		{"magnitudes past what a rational builds", "1e5000", "2", true, true},
		{"a value far above a step it is not a multiple of", "1e1000001", "3", false, true},
		{"a value at the top of the range", "1e999999999999999999", "2", true, true},
		{"a value and step at the bottom of the range", "3e-999999999999999999", "1e-999999999999999999", true, true},
		{"a value below its step's lowest digit", "1e-999999999999999999", "1", false, true},
		{"a power of two the shift covers", "1e3", "8", true, true},
		{"a power of two the shift falls short of", "1e2", "8", false, true},
		{"a coefficient's own powers of two and five", "5e10", "1024", true, true},
		{"one power of two short", "5e9", "1024", false, true},
		{"a shift past the cap", "1e100", "1125899906842624", true, true},
		{"a shift under the cap, one power short", "1e49", "1125899906842624", false, true},
		{"a zero step, which divides nothing", "4", "0", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, err := ir.ParseDecimal(tc.v)
			require.NoError(t, err, "%q is a decimal in range", tc.v)
			m, err := ir.ParseDecimal(tc.m)
			require.NoError(t, err, "%q is a decimal in range", tc.m)

			multiple, ok := isMultipleOf(v, m)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.multiple, multiple)
		})
	}
}
