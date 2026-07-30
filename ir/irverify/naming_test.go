package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

func modelNamed(n ir.Naming) *ir.Document {
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: n}}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
}

func TestVerify_NeutralCanonicalIsClean(t *testing.T) {
	got := irverify.Verify(modelNamed(ir.Naming{Source: "UserID", Canonical: "user_id"}))
	assert.Empty(t, got)
}

func TestVerify_CasedCanonicalIsAViolation(t *testing.T) {
	got := irverify.Verify(modelNamed(ir.Naming{Source: "UserID", Canonical: "userID"}))
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/naming-cased", got[0].Code)
}

// TestVerify_UnsegmentedCanonicalIsAViolation covers the half of invariant 4 the
// casing check cannot see. Every spelling here is lowercase-idempotent, so a
// compiler that passes a source name through unsegmented — a namespaced
// component, a path template, a bracketed parameter — used to satisfy the
// verifier while emitting something no emitter can read as words.
func TestVerify_UnsegmentedCanonicalIsAViolation(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"com.example.user", "get_/pets", "filter[name]", "application/json",
		"_leading", "trailing_", "double__underscore",
	} {
		got := irverify.Verify(modelNamed(ir.Naming{Source: "S", Canonical: canon}))
		require.NotEmpty(t, got, "canonical %q must be reported", canon)
		assert.Equal(t, "ir/naming-not-words", got[0].Code, "canonical %q", canon)
	}
}

// TestVerify_WordSequencesAreClean pins the other direction: the shapes a
// correct compiler emits — including a canonical with no lowercase form and a
// decomposed accent, which the casing check already documents — are not
// reported, so the segmentation check cannot pass by rejecting everything.
func TestVerify_WordSequencesAreClean(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"", "user", "user_id", "api_key_2", "count_ℤ", "cafe\u0301_v_2",
	} {
		assert.Empty(t, irverify.Verify(modelNamed(ir.Naming{Source: "S", Canonical: canon})),
			"canonical %q is a word sequence", canon)
	}
}

// TestVerify_LetterDigitRunIsAViolation is the half of segmentation a neutral
// name still carries evidence of: the grammar splits every letter/digit
// boundary, so no word it produces holds a letter next to a digit. Each value
// here is correctly lower-cased and made only of word characters, so the two
// older checks pass it — which is exactly why it went unreported (GitHub #164).
func TestVerify_LetterDigitRunIsAViolation(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"foo2bar",   // the boundary in both directions, in one word
		"api2",      // letter then digit
		"2fa",       // digit then letter
		"ok_v2beta", // in a later word, so the first must not mask it
		"count_ℤ2",  // beside a rune with no lowercase form
		"café2",     // a precomposed accent is a letter, so the boundary is real
	} {
		got := irverify.Verify(modelNamed(ir.Naming{Source: "S", Canonical: canon}))
		require.NotEmpty(t, got, "canonical %q must be reported", canon)
		assert.Equal(t, "ir/naming-unsegmented", got[0].Code, "canonical %q", canon)
	}
}

// TestVerify_SegmentationCheckDoesNotOverreach guards the other direction. A
// check that rejected every digit, or every word boundary, would satisfy the
// test above while failing every real document: these are shapes
// compile.CanonicalWords genuinely produces.
func TestVerify_SegmentationCheckDoesNotOverreach(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{
		"api_key_2", "v_2", "2", "user_2_id", "oauth_2_token", "a_1_b_2",
		// A decomposed accent puts a combining mark between the letter and the
		// digit, and the grammar does not split after a mark: CanonicalWords leaves
		// "cafe\u0301" + "2" as one word, so reporting it would reject its own output.
		"cafe\u03012",
	} {
		assert.Empty(t, irverify.Verify(modelNamed(ir.Naming{Source: "S", Canonical: canon})),
			"canonical %q is what the grammar produces", canon)
	}
}
