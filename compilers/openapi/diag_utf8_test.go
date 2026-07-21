package openapi

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestDiagf_SanitizesInvalidUTF8 pins invariant #7 at the compiler boundary: the
// openapi diagnostic constructor routes messages through ir.NewDiagnostic, so a
// message built from ill-formed UTF-8 — as a third-party validator emits when it
// truncates a multibyte rune — is coerced to valid UTF-8. The exhaustive
// constructor contract lives in ir; this test proves diagf is wired to it.
func TestDiagf_SanitizesInvalidUTF8(t *testing.T) {
	t.Parallel()
	// "\xe0\xa5" is the truncated lead of U+0965 (E0 A5 A5): one ill-formed byte
	// run, coerced to a single U+FFFD.
	d := diagf(ir.SeverityError, codeValidation, ir.Provenance{}, "bad byte %s here", "\xe0\xa5")
	require.True(t, utf8.ValidString(d.Message), "message must be valid UTF-8")
	assert.Equal(t, "bad byte \uFFFD here", d.Message, "ill-formed run collapses to one U+FFFD")

	first, err := json.Marshal(d)
	require.NoError(t, err)
	var back ir.Diagnostic
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(back)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "sanitized message round-trips byte-for-byte")
	assert.Equal(t, d.Message, back.Message, "message survives marshal/unmarshal unchanged")
}
