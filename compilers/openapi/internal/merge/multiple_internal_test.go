// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"fmt"
	"math/big"
	"strings"
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

// TestCommonStep_HoldsTheMultiplesOfBoth pins the step two multipleOf keywords
// make together. A step that is a multiple of the other is that step, as written.
// Otherwise it is their least common multiple, which the rows check by magnitude
// in both orders, some of them at scales no rational builds, where the fold must
// not multiply the shift out.
func TestCommonStep_HoldsTheMultiplesOfBoth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		x, y ir.BigVal
		want ir.BigVal
	}{
		{"one step", "3", "3", "3"},
		{"one step spelled two ways", "10", "1e1", "10"},
		{"a multiple of the other", "6", "2", "6"},
		{"a decimal multiple of the other", "0.25", "0.01", "0.25"},
		{"two primes", "3", "5", "15"},
		{"sharing a factor", "4", "6", "12"},
		{"two decimals", "0.4", "0.6", "1.2"},
		{"a decimal beside an integer", "0.4", "3", "6"},
		{"trailing zeros on one side", "30", "4", "60"},
		{"a shift past the cap", "3e100", "7", "21e100"},
		{"a shift past the cap, with powers of two to cover", "3e100", "7168", "21e100"},
		{"a shift past what a rational builds", "3e1000001", "7", "21e1000001"},
		{"far below one", "3e-1000001", "7e-1000002", "21e-1000001"},
		{"far apart around one", "3e1000001", "7e-1000001", "21e1000001"},
		{"at the bottom of the range", "3e-999999999999999999", "7e-999999999999999999", "21e-999999999999999999"},
		{"a shift no integer could hold", "3e999999999999999998", "7e-999999999999999999", "21e999999999999999998"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, pair := range [][2]ir.BigVal{{tc.x, tc.y}, {tc.y, tc.x}} {
				got, ok := commonStep(pair[0], pair[1])
				require.True(t, ok, "%s and %s", pair[0], pair[1])
				assert.True(t, sameNumber(tc.want, got), "%s and %s make %s, not %s", pair[0], pair[1], tc.want, got)
			}
		})
	}
}

// TestCommonStep_KeepsTheStepItChoseAsWritten pins that a step the other divides
// is the literal a declaration wrote, so only a number no declaration wrote is
// spelled by the merge.
func TestCommonStep_KeepsTheStepItChoseAsWritten(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]ir.BigVal{{"6.0", "2"}, {"2", "6.0"}} {
		got, ok := commonStep(pair[0], pair[1])
		require.True(t, ok)
		assert.Equal(t, ir.BigVal("6.0"), got)
	}
}

// TestCommonStep_MatchesTheIntegerLeastCommonMultiple holds the decimal reading
// to integers it can be computed from: every pair of small coefficients at every
// pair of small scales, multiplied up to integers and compared with math/big.
func TestCommonStep_MatchesTheIntegerLeastCommonMultiple(t *testing.T) {
	t.Parallel()
	for a := int64(1); a <= 36; a++ {
		for b := int64(1); b <= 36; b++ {
			for _, scale := range [][2]int{{0, 0}, {0, 2}, {2, 0}, {-2, 1}, {1, -2}, {-1, -1}} {
				x := ir.BigVal(fmt.Sprintf("%de%d", a, scale[0]))
				y := ir.BigVal(fmt.Sprintf("%de%d", b, scale[1]))
				want := wholeMultiple(a, scale[0], b, scale[1])

				got, ok := commonStep(x, y)

				require.True(t, ok, "%s and %s", x, y)
				assert.True(t, sameNumber(want, got), "%s and %s make %s, not %s", x, y, want, got)
			}
		}
	}
}

// wholeMultiple is the least common multiple of a·10^ae and b·10^be, computed
// over integers scaled up past both exponents.
func wholeMultiple(a int64, ae int, b int64, be int) ir.BigVal {
	const unit = 3 // past every scale the caller uses
	scaleUp := func(v int64, e int) *big.Int {
		return new(big.Int).Mul(big.NewInt(v), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e+unit)), nil))
	}
	x, y := scaleUp(a, ae), scaleUp(b, be)
	gcd := new(big.Int).GCD(nil, nil, x, y)
	lcm := new(big.Int).Mul(new(big.Int).Quo(x, gcd), y)
	return ir.BigVal(fmt.Sprintf("%se-%d", lcm, unit))
}

// TestCommonStep_RefusesWhatItCannotRead pins each pair that has no common step
// to report: the merge keeps the first and reports the other, rather than folding
// two values into one the source never wrote. The digit bound is held at the
// limit and one past it.
func TestCommonStep_RefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	digits := func(n int) ir.BigVal { return ir.BigVal(strings.Repeat("3", n)) }
	for _, tc := range []struct {
		name string
		x, y ir.BigVal
		ok   bool
	}{
		{name: "one literal twice, though no Decimal holds it", x: "nan", y: "nan", ok: true},
		{name: "a literal no Decimal holds", x: "nan", y: "2"},
		{name: "a zero step", x: "0", y: "2"},
		{name: "a negative step", x: "-3", y: "2"},
		{name: "digits at the bound", x: digits(maxStepDigits - 1), y: "7", ok: true},
		{name: "digits past the bound", x: digits(maxStepDigits), y: "7"},
		{name: "a multiple past what a Decimal holds", x: "9e999999999999999999", y: "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, pair := range [][2]ir.BigVal{{tc.x, tc.y}, {tc.y, tc.x}} {
				_, ok := commonStep(pair[0], pair[1])
				assert.Equal(t, tc.ok, ok, "%.40s and %.40s", pair[0], pair[1])
			}
		})
	}
}
