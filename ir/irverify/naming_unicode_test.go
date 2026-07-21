package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_NoLowercaseRunesAreNeutral pins that a canonical containing a rune
// with no lowercase form — double-struck ℤ, Mathematical Bold 𝐀 — is accepted as
// neutral. A compiler neutralizes with strings.ToLower, which is a no-op on those
// runes, so the verifier must judge casing by lowercase-idempotence rather than
// unicode.IsUpper (which reports true for them).
func TestVerify_NoLowercaseRunesAreNeutral(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{"ℤ", "\U0001D400", "count_ℤ"} {
		m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: canon}}}
		doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
		for _, v := range irverify.Verify(doc) {
			assert.NotEqualf(t, "ir/naming-cased", v.Code, "no-lowercase rune %q must be neutral", canon)
		}
	}
}

// TestVerify_TrulyCasedCanonicalStillFlagged pins that a canonical strings.ToLower
// would still change is reported — the check did not become permissive.
func TestVerify_TrulyCasedCanonicalStillFlagged(t *testing.T) {
	t.Parallel()
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: "userID"}}}
	doc := &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
	var found bool
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/naming-cased" {
			found = true
		}
	}
	assert.True(t, found, "a canonical ToLower would change must still be flagged")
}
