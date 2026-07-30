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
