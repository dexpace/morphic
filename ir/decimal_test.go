package ir_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// decimalSpellings are spellings of a handful of values, several of each, and
// values a rounding reading would merge. decimal_property_test.go seeds its fuzz
// targets from this slice rather than restating it.
var decimalSpellings = []string{
	"0", "-0.0", "0e9", "1", "1.0", "1e0", "10e-1", "10", "1e1", "10.0", "1500", "1500.0", "15e2", "1.5e3",
	"0.001", "1e-3", "5", "-5", "-5.00", "-0.5e1", "1e1000001", "10e1000000", "1e-1000001",
	"9007199254740993", "9007199254740992", "1.0000000000000001", "1.0000000000000002",
}

// mustParseDecimal reads a literal the caller knows is a decimal in range.
func mustParseDecimal(t *testing.T, s string) ir.Decimal {
	t.Helper()
	d, err := ir.ParseDecimal(s)
	require.NoError(t, err, "%q is a decimal literal in range", s)
	return d
}

// TestParseDecimal_ReadsSignDigitsAndExponent pins the one reading every
// consumer takes: a sign, the significant digits with no leading or trailing
// zero, and the power of ten the leading digit carries. Spellings of one value
// share a row, since they must read to one result, and the canonical decimal
// is derived from that result alone.
func TestParseDecimal_ReadsSignDigitsAndExponent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ins      []string
		sign     int
		digits   string
		exponent int64
		isInt    bool
		str      string
	}{
		{[]string{"0", "-0.0", "0e9", "0e999999999999999999999999"}, 0, "", 0, true, "0"},
		{[]string{"1500", "1500.0", "15e2", "1.5e3", "+1500", "001500", ".15e4"}, 1, "15", 3, true, "1.5e3"},
		{[]string{"15"}, 1, "15", 1, true, "1.5e1"},
		{[]string{"-1.5"}, -1, "15", 0, false, "-1.5e0"},
		{[]string{"5.0"}, 1, "5", 0, true, "5e0"},
		{[]string{"0.05"}, 1, "5", -2, false, "5e-2"},
		{[]string{"1e1000001", "10e1000000"}, 1, "1", 1000001, true, "1e1000001"},
		{[]string{"1e-1000001"}, 1, "1", -1000001, false, "1e-1000001"},
		{[]string{"9007199254740993"}, 1, "9007199254740993", 15, true, "9.007199254740993e15"},
		{[]string{"1.0000000000000001"}, 1, "10000000000000001", 0, false, "1.0000000000000001e0"},
		{[]string{"123.456e-1"}, 1, "123456", 1, false, "1.23456e1"},
		{[]string{"-5.00"}, -1, "5", 0, true, "-5e0"},
		{[]string{"0.001"}, 1, "1", -3, false, "1e-3"},
	} {
		for _, in := range tc.ins {
			t.Run(in, func(t *testing.T) {
				t.Parallel()
				d := mustParseDecimal(t, in)

				assert.Equal(t, tc.sign, d.Sign())
				assert.Equal(t, tc.digits, d.Digits())
				assert.Equal(t, tc.exponent, d.Exponent())
				assert.Equal(t, tc.isInt, d.IsInt())
				assert.Equal(t, tc.str, d.String())
			})
		}
	}
}

// TestParseDecimal_RefusesWhatIsNotADecimalLiteral holds the grammar's edge:
// reading the digits out of "1p4", which is 16, would put a number the source
// never wrote into the IR. Every refusal is ErrNotDecimal and an unusable
// result, so a caller that drops the error reads zero rather than garbage.
func TestParseDecimal_RefusesWhatIsNotADecimalLiteral(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "-", "+", ".", "e5", "1e", "1e+", "1p4", "2.5p-2", "5.p3", "1P4", "0x10", "0b101", "0o17",
		"1_000", "1e1_0", "1,5", "1.2.3", "--5", "+-5", " 1", "1 ", "Inf", "-Inf", "NaN", ".inf",
		"１", // fullwidth digit one: the grammar is ASCII only
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			d, err := ir.ParseDecimal(in)

			require.ErrorIs(t, err, ir.ErrNotDecimal)
			assert.Equal(t, ir.Decimal{}, d)
		})
	}
}

// TestParseDecimal_HoldsTheExponentRange pins where the bound falls: on the
// power of ten the leading significant digit carries, not on the exponent as
// written. 0.01e1000000000000000000 is in range and 10e999999999999999999 is
// not, though their written exponents say the opposite, and zero is held at any
// exponent. A saturated exponent never changes a verdict.
func TestParseDecimal_HoldsTheExponentRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in       string
		exponent int64
	}{
		{"1e999999999999999999", 999999999999999999},
		{"-1e-999999999999999999", -999999999999999999},
		{"0.01e1000000000000000000", 999999999999999998},
		{"1000e999999999999999996", 999999999999999999},
		{"0e99999999999999999999999999999", 0},
	} {
		t.Run("accepts "+tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.exponent, mustParseDecimal(t, tc.in).Exponent())
		})
	}
	for _, in := range []string{
		"1e1000000000000000000", "-1e1000000000000000000", "1e-1000000000000000000",
		"10e999999999999999999", "0.1e-999999999999999999", "1e99999999999999999999999999999",
		"1e-99999999999999999999999999999", "1e4000000000000000000",
		// 2⁶⁴ and 2⁶⁴ + 1 written out: an exponent read without saturating wraps
		// these to 0 and 1, which are in range.
		"1e18446744073709551616", "1e-18446744073709551617",
	} {
		t.Run("refuses "+in, func(t *testing.T) {
			t.Parallel()
			d, err := ir.ParseDecimal(in)

			require.ErrorIs(t, err, ir.ErrExponentRange)
			assert.Equal(t, ir.Decimal{}, d)
		})
	}
}

// TestDecimal_CmpOrdersEveryDecimalSpellingExactly pins the ordering the bound
// reconciliation rests on. Every row is a pair a rounding comparison gets
// wrong: values that differ past float64's precision, spellings of one value,
// magnitudes past what a rational holds, the signed zeros, and digit runs one
// of which is a prefix of the other. Each is asserted in both directions, so an
// order that is right one way round is not mistaken for one that orders.
func TestDecimal_CmpOrdersEveryDecimalSpellingExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "one value, one spelling", a: "1", b: "1", want: 0},
		{name: "an exponent against the digits it stands for", a: "1e2", b: "100", want: 0},
		{name: "a trailing zero the exponent moved", a: "5e1", b: "50", want: 0},
		{name: "a fraction against its exponent form", a: "0.001", b: "1e-3", want: 0},
		{name: "zero however it is written", a: "0", b: "-0.0", want: 0},
		{name: "zero with an exponent is still zero", a: "-0.0", b: "0e9", want: 0},
		{name: "a digit past float64's exact range", a: "9007199254740993", b: "9007199254740992", want: 1},
		{name: "a decimal float64 cannot hold", a: "1.0000000000000001", b: "1.0000000000000002", want: -1},
		{name: "a magnitude no rational holds", a: "1e1000001", b: "5", want: 1},
		{name: "a magnitude too small for one", a: "1e-1000001", b: "5", want: -1},
		{name: "sign beats magnitude", a: "-1e1000001", b: "5", want: -1},
		{name: "two negatives order by magnitude", a: "-5", b: "-10", want: 1},
		{name: "negative against positive", a: "-5", b: "5", want: -1},
		{name: "zero against a negative", a: "0", b: "-1", want: 1},
		{name: "a leading zero carries nothing", a: "0.5", b: "00.50", want: 0},
		{name: "a digit the exponent moved, past a rational", a: "1e1000001", b: "10e1000000", want: 0},
		{name: "two values in order", a: "1", b: "2", want: -1},
		{name: "a trailing zero in the fraction", a: "2.0", b: "2", want: 0},
		{name: "two negatives, the larger digit", a: "-5", b: "-6", want: 1},
		{name: "past the largest int64", a: "1e1000001", b: "9223372036854775807", want: 1},
		{name: "past the smallest int64", a: "-1e1000001", b: "-9223372036854775808", want: -1},
		{name: "the exponent before the digits", a: "1e2", b: "99", want: 1},
		{name: "a longer run beats its prefix", a: "1.5", b: "1.49", want: 1},
		{name: "a shorter run below a longer one", a: "1.4", b: "1.49", want: -1},
		{name: "a run that is a prefix of a longer one", a: "1", b: "1.0000000000000000000000001", want: -1},
		{name: "the largest magnitudes in range", a: "1e999999999999999999", b: "-1e999999999999999999", want: 1},
		{name: "the smallest positive in range", a: "1e-999999999999999999", b: "0", want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := mustParseDecimal(t, tc.a), mustParseDecimal(t, tc.b)

			assert.Equal(t, tc.want, a.Cmp(b), "%s against %s", tc.a, tc.b)
			assert.Equal(t, -tc.want, b.Cmp(a), "%s against %s", tc.b, tc.a)
		})
	}
}

// TestDecimal_EqualityIsOneValue holds the three ways to ask whether two
// literals are one value to one answer over every pair of decimalSpellings:
// Cmp, ==, and the canonical decimal. A member ID keys a number by that
// spelling, so a pair the three split on would merge two enum members or
// split one. A map keyed by Decimal then holds one entry per value.
func TestDecimal_EqualityIsOneValue(t *testing.T) {
	t.Parallel()
	for _, a := range decimalSpellings {
		for _, b := range decimalSpellings {
			da, db := mustParseDecimal(t, a), mustParseDecimal(t, b)

			byCmp, byEq, byText := da.Cmp(db) == 0, da == db, da.String() == db.String()
			assert.Equal(t, byCmp, byEq, "Cmp and == on %s and %s", a, b)
			assert.Equal(t, byCmp, byText, "Cmp and String on %s and %s", a, b)
		}
	}

	byValue := map[ir.Decimal]bool{}
	for _, s := range []string{"1500", "15e2", "1.5e3", "1500.000"} {
		byValue[mustParseDecimal(t, s)] = true
	}
	assert.Len(t, byValue, 1, "four spellings of one value are one key")
}

// TestDecimal_ZeroValueIsZero pins what a caller that drops ParseDecimal's
// error gets: the number zero, which is also what every spelling of zero reads
// to, so the zero value is a usable number and not a trap state.
func TestDecimal_ZeroValueIsZero(t *testing.T) {
	t.Parallel()
	var d ir.Decimal

	assert.Equal(t, 0, d.Sign())
	assert.Equal(t, "0", d.String())
	assert.True(t, d.IsInt())
	assert.Empty(t, d.Digits())
	assert.Zero(t, d.Exponent())
	assert.Equal(t, mustParseDecimal(t, "-0.0"), d, "negative zero reads to the zero value")
}

// TestParseDecimal_ReadsAHugeLiteralWithoutAllocating pins the cost of a hostile
// literal: a million-digit exponent and a million-digit mantissa both read in a
// single pass over the text with no allocation, so a spec cannot make the
// reading expensive by writing a long number. It does not run in parallel
// because testing.AllocsPerRun panics under t.Parallel.
func TestParseDecimal_ReadsAHugeLiteralWithoutAllocating(t *testing.T) {
	zeros := strings.Repeat("0", 1<<20)
	for _, tc := range []struct {
		name     string
		in       string
		exponent int64
	}{
		{"a million-digit exponent", "1e" + zeros + "7", 7},
		{"a million-digit mantissa", "1" + zeros, 1 << 20},
	} {
		var (
			got ir.Decimal
			err error
		)
		allocs := testing.AllocsPerRun(10, func() { got, err = ir.ParseDecimal(tc.in) })

		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.exponent, got.Exponent(), tc.name)
		assert.Zero(t, allocs, tc.name)
	}
}

// TestBigVal_DecimalReadsWhatNewBigValBuilt holds the two to one grammar: a
// BigVal NewBigVal built always reads back, and a BigVal that was never built
// through it, such as one a decoder produced, is refused rather than guessed at.
func TestBigVal_DecimalReadsWhatNewBigValBuilt(t *testing.T) {
	t.Parallel()
	for _, s := range bigValDecimalForms {
		v, err := ir.NewBigVal(s)
		require.NoError(t, err, s)

		_, err = v.Decimal()
		assert.NoError(t, err, s)
	}

	_, err := ir.BigVal("1p4").Decimal()
	assert.ErrorIs(t, err, ir.ErrNotDecimal)
}
