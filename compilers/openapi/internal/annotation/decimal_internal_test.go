package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestCompareDecimalBounds_OrdersEveryDecimalSpellingExactly pins the ordering
// the bound reconciliation rests on. Every row is a pair a rounding comparison
// would get wrong: two values that differ past float64's precision, two that
// differ only in spelling, magnitudes past what a rational will hold, and the
// signed zeros. Each is asserted in both directions, so a comparison that
// happens to be right one way round is not mistaken for one that orders.
func TestCompareDecimalBounds_OrdersEveryDecimalSpellingExactly(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, aOK := parseDecimalBound(ir.BigVal(tc.a))
			b, bOK := parseDecimalBound(ir.BigVal(tc.b))
			require.True(t, aOK, "%q is a decimal literal", tc.a)
			require.True(t, bOK, "%q is a decimal literal", tc.b)

			assert.Equal(t, tc.want, compareDecimalBounds(a, b), "%s against %s", tc.a, tc.b)
			assert.Equal(t, -tc.want, compareDecimalBounds(b, a), "%s against %s", tc.b, tc.a)
		})
	}
}

// TestBigValEqual_SeparatesOneValueFromTwo pins what the allOf redeclaration
// merge asks of this comparison: one value written two ways stays one value at
// any magnitude, and two values stay two.
//
// The rows that spell one magnitude two ways past a rational's range are what
// carry the test. math/big will not build 1e1000001 as a rational at all, so a
// comparison resting on one has to answer that pair from their text, which
// reports a disagreement between a bound and itself. Both directions are
// asserted, so an answer that happens to be right one way round is not mistaken
// for a comparison.
func TestBigValEqual_SeparatesOneValueFromTwo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "one literal against itself", a: "1e1000001", b: "1e1000001", want: true},
		{name: "an exponent against the digits it stands for", a: "1e2", b: "100", want: true},
		{name: "a digit the exponent moved, past a rational", a: "1e1000001", b: "10e1000000", want: true},
		{name: "a magnitude too small for a rational", a: "1e-1000001", b: "10e-1000002", want: true},
		{name: "zero however it is written", a: "0", b: "-0.0", want: true},
		{name: "one exponent apart, past a rational", a: "1e1000001", b: "1e1000002"},
		{name: "one leading digit apart, past a rational", a: "1e1000001", b: "2e1000001"},
		{name: "a sign apart, past a rational", a: "1e1000001", b: "-1e1000001"},
		{name: "a digit past float64's exact range", a: "9007199254740993", b: "9007199254740992"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := ir.NewBigVal(tc.a)
			require.NoError(t, err, "%q is a literal a schema may write", tc.a)
			b, err := ir.NewBigVal(tc.b)
			require.NoError(t, err, "%q is a literal a schema may write", tc.b)

			assert.Equal(t, tc.want, BigValEqual(a, b), "%s against %s", a, b)
			assert.Equal(t, tc.want, BigValEqual(b, a), "%s against %s", b, a)
		})
	}
}

// TestBigValEqual_ReadsAPairOutsideTheGrammarByItsTextAlone covers the answer
// for a literal this reader cannot order. No bound reaches it today — every one
// comes through ir.NewBigVal, whose grammar
// TestBigValGrammarStaysWithinTheDecimalReading holds inside this one — so the
// literals here are fed in directly, and they are the ones NewBigVal refuses.
//
// Identical text is one literal and so one value, whatever grammar it belongs
// to. Differing text is the honest "not the same value I can see": claiming
// equality there would fold two bounds into one on a reading never made.
func TestBigValEqual_ReadsAPairOutsideTheGrammarByItsTextAlone(t *testing.T) {
	t.Parallel()
	_, err := ir.NewBigVal("1p4")
	require.Error(t, err, "the guard's inputs are literals no schema turns into a BigVal")

	assert.True(t, BigValEqual(ir.BigVal("1p4"), ir.BigVal("1p4")),
		"one literal is one value whether or not it can be read")
	assert.False(t, BigValEqual(ir.BigVal("1p4"), ir.BigVal("16")),
		"an unreadable literal is never folded into one that reads")
	assert.False(t, BigValEqual(ir.BigVal("16"), ir.BigVal("1p4")),
		"nor the other way round")
}

// TestParseDecimalBound_DeclinesWhatIsNotADecimalLiteral pins the one honest
// answer for a literal outside the grammar: none. Reading digits out of one and
// ordering what is left would put a bound the source never wrote into the IR —
// "1p4" is 16, not 1, and a comparison that took the 1 would keep the wrong
// bound while reporting a comparison it never made.
func TestParseDecimalBound_DeclinesWhatIsNotADecimalLiteral(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"", "-", ".", "1p4", "2.5p-2", "1e", "1e1_0", "1.2.3", "+5", "0x10"} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			_, ok := parseDecimalBound(ir.BigVal(literal))
			assert.False(t, ok, "%q is not a decimal literal", literal)
		})
	}
}

// TestBigValGrammarStaysWithinTheDecimalReading pins the coupling that decides
// whether BigValEqual's incomparable guard is reachable: every literal
// ir.NewBigVal accepts must be one parseDecimalBound can order.
//
// The two grammars live in different packages and have already moved apart
// once. A widening of NewBigVal this reader cannot order has to fail here
// rather than in a compiled document, where it would report a conflict between
// two spellings of one magnitude.
//
// The spellings NewBigVal refuses today are the load-bearing half of the
// corpus, since no mix of decimal spellings generates a binary exponent. They
// are skipped until one is accepted, when the assertion meets it.
func TestBigValGrammarStaysWithinTheDecimalReading(t *testing.T) {
	t.Parallel()
	signs := []string{"", "-", "+"}
	mantissas := []string{
		"0", "00", "0.0", ".5", "5.", "1", "10", "100", "1.0", "01", "007", "0.5",
		"0.05", "123456789012345678901234567890", "9007199254740993", "1.7976931348623157",
	}
	exponents := []string{"", "e0", "e1", "e-1", "e+1", "E5", "e308", "e-308", "e1000001", "e-1000001"}

	// Refused today, every one of them a way the grammar could widen: a binary
	// exponent (the one it has already had), another base, a digit separator,
	// and the non-numbers big.ParseFloat knows by name.
	widenings := []string{
		"1p4", "1P4", "0x10", "0X1p-2", "0b11", "0o17", "1_000", "1e1_0",
		"Inf", "-Inf", "NaN", "1e", "1.2.3", "--1", ".",
	}

	literals := make([]string, 0, len(widenings)+len(signs)*len(mantissas)*len(exponents))
	literals = append(literals, widenings...)
	for _, sign := range signs {
		for _, mantissa := range mantissas {
			for _, exponent := range exponents {
				literals = append(literals, sign+mantissa+exponent)
			}
		}
	}

	var accepted int
	for _, literal := range literals {
		v, err := ir.NewBigVal(literal)
		if err != nil {
			continue // not a bound at all; nothing for this reader to order
		}
		accepted++
		_, ok := parseDecimalBound(v)
		assert.True(t, ok, "NewBigVal(%q) = %q, which this reader cannot order", literal, v)
	}
	// Without this the loop would pass by accepting nothing at all, which is
	// exactly what a widened rejection in ir would look like from here.
	require.NotZero(t, accepted, "the corpus reached NewBigVal")
}

// canonicalLiterals are spellings of a handful of values, several of each, and
// values a rounding reading would merge.
var canonicalLiterals = []string{
	"0", "-0.0", "0e9", "1", "1.0", "1e0", "10e-1", "10", "1e1", "10.0", "1500", "1500.0", "15e2", "1.5e3",
	"0.001", "1e-3", "5", "-5", "-5.00", "-0.5e1", "1e1000001", "10e1000000", "1e-1000001",
	"9007199254740993", "9007199254740992", "1.0000000000000001", "1.0000000000000002",
}

func TestCanonicalDecimal_SpellsEachValueOnce(t *testing.T) {
	t.Parallel()
	tests := []struct{ literal, want string }{
		{"1", "1e0"}, {"1.0", "1e0"}, {"10", "1e1"}, {"10.0", "1e1"}, {"1500", "1.5e3"}, {"15e2", "1.5e3"},
		{"0.001", "1e-3"}, {"-5.00", "-5e0"}, {"-0.0", "0"}, {"0e9", "0"},
		{"10e1000000", "1e1000001"}, {"1.0000000000000001", "1.0000000000000001e0"},
	}
	for _, tc := range tests {
		got, ok := CanonicalDecimal(ir.BigVal(tc.literal))
		require.True(t, ok, tc.literal)
		assert.Equal(t, tc.want, got, tc.literal)
	}
}

// TestCanonicalDecimal_AgreesWithBigValEqual holds the two readings to one
// answer over every pair of canonicalLiterals: a shared spelling for a pair that
// is one value, and distinct spellings for a pair that is two.
func TestCanonicalDecimal_AgreesWithBigValEqual(t *testing.T) {
	t.Parallel()
	for _, a := range canonicalLiterals {
		for _, b := range canonicalLiterals {
			ca, okA := CanonicalDecimal(ir.BigVal(a))
			cb, okB := CanonicalDecimal(ir.BigVal(b))
			require.True(t, okA && okB, "%s, %s", a, b)
			assert.Equal(t, BigValEqual(ir.BigVal(a), ir.BigVal(b)), ca == cb, "%s and %s", a, b)
		}
	}
}

func TestCanonicalDecimal_DeclinesWhatIsNotADecimalLiteral(t *testing.T) {
	t.Parallel()
	_, ok := CanonicalDecimal("1p4")
	assert.False(t, ok)
}

// FuzzCanonicalDecimal_AgreesWithBigValEqual searches for two values that share
// a canonical spelling, or one value spelled two ways that does not. A member
// ID keys a number by it, so the first would merge two enum members.
func FuzzCanonicalDecimal_AgreesWithBigValEqual(f *testing.F) {
	for i, a := range canonicalLiterals {
		f.Add(a, canonicalLiterals[(i+1)%len(canonicalLiterals)])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		va, errA := ir.NewBigVal(a)
		vb, errB := ir.NewBigVal(b)
		if errA != nil || errB != nil {
			return
		}
		ca, okA := CanonicalDecimal(va)
		cb, okB := CanonicalDecimal(vb)
		require.True(t, okA && okB, "every BigVal is in the decimal grammar: %q, %q", va, vb)
		require.Equal(t, BigValEqual(va, vb), ca == cb, "%q (%s) and %q (%s)", va, ca, vb, cb)
	})
}

// TestReadDecimal_ReadsWithoutMaterializing pins the reading admits judges a
// value by: integrality, significant digits and the leading power of ten, for
// literals no machine type holds.
func TestReadDecimal_ReadsWithoutMaterializing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in       ir.BigVal
		neg      bool
		zero     bool
		integral bool
		digits   int
		msd      string
	}{
		{"0", false, true, true, 0, "0"},
		{"-0.0", false, true, true, 0, "0"},
		{"1500", false, false, true, 2, "3"},
		{"-1.5", true, false, false, 2, "0"},
		{"5.0", false, false, true, 1, "0"},
		{"0.05", false, false, false, 1, "-2"},
		{"1e1000001", false, false, true, 1, "1000001"},
		{"1e-1000001", false, false, false, 1, "-1000001"},
	} {
		got, ok := ReadDecimal(tc.in)
		require.True(t, ok, "%s", tc.in)
		order, ordered := CompareBigVal(tc.in, "0")
		require.True(t, ordered, "%s", tc.in)
		assert.Equal(t, tc.neg, order < 0, "%s", tc.in)
		assert.Equal(t, tc.zero, got.Zero, "%s", tc.in)
		assert.Equal(t, tc.integral, got.Integral, "%s", tc.in)
		assert.Equal(t, tc.digits, got.Digits, "%s", tc.in)
		assert.Equal(t, tc.msd, got.MsdExp.String(), "%s", tc.in)
	}

	_, ok := ReadDecimal("1p4")
	assert.False(t, ok, "a literal outside the decimal grammar is not read")
}

// TestCompareBigVal_OrdersByValue pins the order admits bounds a value by.
func TestCompareBigVal_OrdersByValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b ir.BigVal
		want int
	}{
		{"1", "2", -1},
		{"2.0", "2", 0},
		{"-5", "-6", 1},
		{"1e1000001", "9223372036854775807", 1},
		{"-1e1000001", "-9223372036854775808", -1},
	} {
		got, ok := CompareBigVal(tc.a, tc.b)
		require.True(t, ok)
		assert.Equal(t, tc.want, got, "%s against %s", tc.a, tc.b)
	}

	_, ok := CompareBigVal("1p4", "1")
	assert.False(t, ok, "no order is claimed outside the grammar")
	_, ok = CompareBigVal("1", "1p4")
	assert.False(t, ok)
}

// TestIsMultipleOf_JudgesExactly pins the multipleOf judgment the merge holds a
// value to. Each row is one a float division gets wrong or one where no answer
// is claimed: a decimal step float64 cannot hold, values far apart in scale,
// and the pairs ok refuses, a zero step, a scale past the shift budget and a
// literal outside the grammar.
func TestIsMultipleOf_JudgesExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		v, m         ir.BigVal
		multiple, ok bool
	}{
		{"an integer multiple", "10", "5", true, true},
		{"an integer that is not one", "7", "2", false, true},
		{"a decimal step float64 cannot hold", "0.3", "0.1", true, true},
		{"a decimal off the step", "0.35", "0.1", false, true},
		{"a fraction against an integral step", "2.5", "2", false, true},
		{"a fraction against a fractional step", "2.5", "0.5", true, true},
		{"zero, a multiple of anything", "0", "3", true, true},
		{"a negative multiple", "-6", "3", true, true},
		{"a step larger than the value", "2", "5", false, true},
		{"magnitudes far apart within the budget", "1e100", "5", true, true},
		{"a step far below the value", "1", "1e-100", true, true},
		{"magnitudes past the budget", "1e5000", "2", false, false},
		{"a zero step, which divides nothing", "4", "0", false, false},
		{"a value outside the grammar", "1p4", "2", false, false},
		{"a step outside the grammar", "4", "2p1", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			multiple, ok := IsMultipleOf(tc.v, tc.m)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.multiple, multiple)
		})
	}
}
