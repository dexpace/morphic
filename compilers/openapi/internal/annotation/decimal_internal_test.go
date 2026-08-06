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
