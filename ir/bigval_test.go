// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/ircases"
	"github.com/dexpace/morphic/ir"
)

func TestNewBigVal_AcceptsDecimalForms(t *testing.T) {
	t.Parallel()
	for _, s := range ircases.DecimalForms {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(s)
			require.NoError(t, err)
			assert.Equal(t, s, v.String())
		})
	}
}

func TestNewBigVal_RejectsNonNumeric(t *testing.T) {
	t.Parallel()
	cases := []string{
		"", "abc", "1.2.3", "0x10", "NaN", "Infinity", "Inf", "+Inf", "-Inf", ".inf", "1,5", "1_000",
		"0b101", "0o17", "--5",
		// An "e"/"E" with no exponent digits after it, which is the one way the
		// grammar can consume a whole mantissa and still refuse.
		"1e", "1e+",
		// A binary (p/P) exponent: math/big's own base-10 parser accepts one
		// regardless of base, so these reproduce GitHub #45 — "1p4" is 1×2⁴ =
		// 16, "2.5p-2" is 0.625, neither the decimal value its digits suggest —
		// and must be rejected rather than stored verbatim.
		"1p4", "2.5p-2", "5.p3", "1P4",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := ir.NewBigVal(s)
			require.Error(t, err)
			assert.ErrorIs(t, err, ir.ErrNotDecimal)
		})
	}
}

// TestNewBigVal_RefusesAnExponentPastTheRange covers the one refusal that
// outlives the grammar check. Each input is a well-formed decimal whose leading
// digit carries a power of ten past MaxDecimalExp, whether through its written
// exponent, its digit count or both, and each is refused with ErrExponentRange
// rather than as a malformed literal, since nothing about its spelling is wrong.
func TestNewBigVal_RefusesAnExponentPastTheRange(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"1e1000000000000000000", "-1e1000000000000000000", "1e-1000000000000000000",
		"10e999999999999999999", "0.1e-999999999999999999", "1e99999999999999999999999999999",
		"1e-99999999999999999999999999999", "1e4000000000000000000",
		"1e18446744073709551616", "1e-18446744073709551617",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(in)

			require.ErrorIs(t, err, ir.ErrExponentRange)
			assert.Empty(t, v, "a refused literal is never stored")
		})
	}
}

// TestNewBigVal_KeepsEveryExponentWithinTheRange pins that the range is the
// bound's own and not a binary float's. Each literal was refused when an
// arbitrary-precision float gated NewBigVal, and each is carried verbatim now,
// down to a zero written at an exponent no integer holds.
func TestNewBigVal_KeepsEveryExponentWithinTheRange(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"1e2000000000", "9e646456992", "1e-2147483650", "1e999999999999999999",
		"0.01e1000000000000000000", "0e99999999999999999999999",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(in)

			require.NoError(t, err)
			assert.Equal(t, in, v.String())
		})
	}
}

func TestNewBigVal_CanonicalizesToJSONForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{".5", "0.5"},          // leading dot gains its zero
		{"-.5", "-0.5"},        // leading dot with sign
		{"5.", "5"},            // trailing dot dropped
		{"5.e3", "5e3"},        // trailing dot before an exponent
		{"+5", "5"},            // leading plus dropped
		{"0.", "0"},            // bare trailing dot
		{"1.8e308", "1.8e308"}, // beyond float64: preserved verbatim
		{"1e400", "1e400"},     // beyond float64: preserved verbatim
		{"2.5E-3", "2.5E-3"},   // exponent case preserved
		{"0.30000000000000004", "0.30000000000000004"},                       // full precision kept
		{"123456789012345678901234567890", "123456789012345678901234567890"}, // huge int kept
		{"09", "9"},      // leading zero is JSON-invalid
		{"-09", "-9"},    // leading zero with sign
		{"007e2", "7e2"}, // leading zeros before an exponent
		{"00", "0"},      // all-zero keeps one digit
		{"00.5", "0.5"},  // the integer part's zeros collapse to one
		{"0.5", "0.5"},   // a meaningful zero survives
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, v.String())

			// Canonical form is idempotent and JSON round-trips unchanged.
			again, err := ir.NewBigVal(v.String())
			require.NoError(t, err)
			assert.Equal(t, v, again, "canonical form is a fixed point")

			raw, err := json.Marshal(v)
			require.NoError(t, err)
			var back ir.BigVal
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, v, back)
		})
	}
}

// TestNewBigVal_ReturnsAJSONSpelledLiteralAsItIs holds the shortcut that keeps
// a literal already in JSON form to exactly the literals jsonSpelling leaves
// unchanged, over every string of up to six runes drawn from the runes the
// grammar treats differently: a sign, a zero, another digit, a point and an
// exponent marker. A literal it takes wrongly would be stored un-canonicalized,
// and one it misses only costs a string.
func TestNewBigVal_ReturnsAJSONSpelledLiteralAsItIs(t *testing.T) {
	t.Parallel()
	const runes, longest = "+-01.e", 6
	size, width := 1, 1
	for range longest {
		width *= len(runes)
		size += width
	}
	literals, shorter := make([]string, 1, size), []string{""}
	for range longest {
		longer := make([]string, 0, len(runes)*len(shorter))
		for _, s := range shorter {
			for _, r := range runes {
				longer = append(longer, s+string(r))
			}
		}
		literals, shorter = append(literals, longer...), longer
	}
	var read, asIs int
	for _, s := range literals {
		spelling, spelled, ok := ir.JSONSpelling(s)
		if !ok {
			continue
		}
		read++
		if spelled {
			asIs++
		}
		require.Equal(t, spelling == s, spelled, "%q spells as %q", s, spelling)
		v, err := ir.NewBigVal(s)
		require.NoError(t, err, s)
		require.Equal(t, spelling, v.String(), s)
	}
	assert.Positive(t, asIs, "some literal is in JSON form already")
	assert.Less(t, asIs, read, "some literal is rewritten")
}

// TestNewBigVal_AllocatesOnlyToRewrite pins NewBigVal's cost: nothing for a
// literal in JSON form, whatever its parts, and the one string a rewrite
// builds otherwise. Reading the range joins no digits, so a literal with digits
// either side of its point costs no more, even past the length a join could be
// built on the stack. It is not run in parallel: AllocsPerRun measures the
// process.
func TestNewBigVal_AllocatesOnlyToRewrite(t *testing.T) {
	for _, tc := range []struct {
		in     string
		allocs float64
	}{
		{"0", 0}, {"-0", 0}, {"42", 0}, {"3.14", 0}, {"-0.5", 0}, {"0.0", 0}, {"1.5e+3", 0},
		{"2.5E-3", 0}, {"12345678901234567890.12345678901234567890", 0},
		{"+3.14", 1}, {".5", 1}, {"5.", 1}, {"007.5", 1}, {"-00.5e3", 1},
	} {
		var err error
		allocs := testing.AllocsPerRun(10, func() { _, err = ir.NewBigVal(tc.in) })

		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.allocs, allocs, tc.in)
	}
}

func TestBigVal_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	v, err := ir.NewBigVal("123456789012345678901234567890.5")
	require.NoError(t, err)

	raw, err := json.Marshal(v)
	require.NoError(t, err)
	assert.Equal(t, `"123456789012345678901234567890.5"`, string(raw))

	var back ir.BigVal
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, v, back)
}
