package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// bigValDecimalForms lists spellings NewBigVal accepts and stores unchanged,
// because each is already in canonical JSON form. bigval_property_test.go seeds
// its fuzz target from this slice rather than restating it, so a form added
// here cannot go missing there.
var bigValDecimalForms = []string{"0", "-1", "42", "3.14", "-0.5", "1e10", "2.5E-3", "9007199254740993"}

func TestNewBigVal_AcceptsDecimalForms(t *testing.T) {
	t.Parallel()
	for _, s := range bigValDecimalForms {
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
		})
	}
}

// TestNewBigVal_RejectsAMagnitudeParseFloatWillNotCarry covers the two refusals
// that outlive the grammar check, and asserts which arm each input takes.
//
// Every row of TestNewBigVal_RejectsNonNumeric is now answered by
// isDecimalLiteral, so none of them reaches big.ParseFloat at all — before the
// grammar check existed those rows were what covered these two arms ("Inf"
// parsed to an infinity, "0x10" failed to parse). What still gets past the
// grammar is a well-formed decimal naming a magnitude math/big will not carry,
// and it reports that two different ways, so each needs a row of its own: an
// exponent ParseFloat cannot scan is an error, and one it scans but cannot
// scale to parses and reports IsInf.
func TestNewBigVal_RejectsAMagnitudeParseFloatWillNotCarry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{
			name: "an exponent that overflows to infinity",
			in:   "1e2000000000",
			want: `bigval: non-finite numeric literal "1e2000000000"`,
		},
		{
			// ParseFloat returns a nil *Float with this error, so the error arm
			// is also what keeps the IsInf call after it from dereferencing one.
			name: "an exponent ParseFloat cannot scan",
			in:   "1e999999999999999999",
			want: `bigval: parse "1e999999999999999999"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, v, "a refused literal is never stored")
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
