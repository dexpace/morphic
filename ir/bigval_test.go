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
	for _, s := range []string{"", "abc", "1.2.3", "0x10", "NaN", "Infinity", "1,5"} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := ir.NewBigVal(s)
			require.Error(t, err)
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
