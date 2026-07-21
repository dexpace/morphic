package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestNewBigVal_AcceptsDecimalForms(t *testing.T) {
	t.Parallel()
	cases := []string{"0", "-1", "42", "3.14", "-0.5", "1e10", "2.5E-3", "9007199254740993"}
	for _, s := range cases {
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
	for _, s := range []string{"", "abc", "1.2.3", "0x10", "NaN", "Infinity", "Inf", "+Inf", "-Inf", ".inf", "1,5", "1_000"} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := ir.NewBigVal(s)
			require.Error(t, err)
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
