package ir_test

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestNewDiagnostic_CoercesMessageToValidUTF8 covers the constructor's message
// contract: valid input is preserved exactly, and each ill-formed byte run is
// replaced by a single U+FFFD. Coercion is what keeps invariant #7 honest for
// diagnostics — see TestNewDiagnostic_RoundTripsByteForByte.
func TestNewDiagnostic_CoercesMessageToValidUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain ascii", in: "no problem here", want: "no problem here"},
		{name: "valid multibyte", in: "café ☕ 音", want: "café ☕ 音"},
		{name: "empty", in: "", want: ""},
		// "\xe0\xa5" is the truncated lead of U+0965 (E0 A5 A5): one ill-formed run.
		{name: "truncated rune", in: "bad \xe0\xa5 here", want: "bad \uFFFD here"},
		// Two separate ill-formed runs → two replacements.
		{name: "two bad runs", in: "\xff a \xfe", want: "\uFFFD a \uFFFD"},
		{name: "trailing bad byte", in: "tail \xc3", want: "tail \uFFFD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := ir.NewDiagnostic(ir.SeverityError, "openapi/validation", tt.in, ir.Provenance{})
			assert.Equal(t, tt.want, d.Message)
			assert.True(t, utf8.ValidString(d.Message), "coerced message must be valid UTF-8")
		})
	}
}

// TestNewDiagnostic_PreservesOtherFields confirms the constructor only touches
// the message: severity, code, and provenance pass through verbatim.
func TestNewDiagnostic_PreservesOtherFields(t *testing.T) {
	t.Parallel()
	prov := ir.Provenance{Source: 2, Pointer: "/paths/~1x", Inferred: "pagination-name-match"}
	d := ir.NewDiagnostic(ir.SeverityWarning, "openapi/degraded-construct", "fine", prov)
	assert.Equal(t, ir.SeverityWarning, d.Severity)
	assert.Equal(t, "openapi/degraded-construct", d.Code)
	assert.Equal(t, "fine", d.Message)
	assert.Equal(t, prov, d.Provenance)
}

// TestNewDiagnostic_RoundTripsByteForByte is the invariant #7 property: a
// constructed diagnostic survives marshal → unmarshal → marshal unchanged.
// Without coercion the raw ill-formed bytes would encode as the \uFFFD escape on
// the first marshal but as raw U+FFFD bytes on the second, breaking the
// byte-for-byte guarantee and the deep-equal of the in-memory value.
func TestNewDiagnostic_RoundTripsByteForByte(t *testing.T) {
	t.Parallel()
	d := ir.NewDiagnostic(ir.SeverityError, "openapi/validation", "bad \xe0\xa5 byte", ir.Provenance{})

	first, err := json.Marshal(d)
	require.NoError(t, err)
	var back ir.Diagnostic
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(back)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "re-encoding is byte-for-byte stable")
	assert.Equal(t, d, back, "the in-memory diagnostic survives a JSON round-trip")
}
