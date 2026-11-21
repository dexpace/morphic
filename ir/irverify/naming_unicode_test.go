// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestVerify_NoLowercaseRunesAreNeutral pins that a canonical containing a rune
// with no lowercase form — double-struck ℤ, Mathematical Bold 𝐀 — is accepted as
// neutral. A compiler neutralizes with strings.ToLower, which is a no-op on those
// runes, so the verifier must judge casing by lowercase-idempotence rather than
// unicode.IsUpper (which reports true for them).
func TestVerify_NoLowercaseRunesAreNeutral(t *testing.T) {
	t.Parallel()
	for _, canon := range []string{"ℤ", "\U0001D400", "count_ℤ"} {
		model := &ir.Model{ID: "type/x/M", Name: ir.Naming{Source: "M", Canonical: canon}}
		doc := &ir.Document{Types: ir.TypeRegistry{model.ID: model}}
		for _, violation := range verifyDeclared(doc) {
			assert.NotEqualf(t, "ir/naming-cased", violation.Code, "no-lowercase rune %q must be neutral", canon)
		}
	}
}

// TestVerify_TrulyCasedCanonicalStillFlagged pins that a canonical strings.ToLower
// would still change is reported — the check did not become permissive.
func TestVerify_TrulyCasedCanonicalStillFlagged(t *testing.T) {
	t.Parallel()
	model := &ir.Model{ID: "type/x/M", Name: ir.Naming{Source: "M", Canonical: "userID"}}
	doc := &ir.Document{Types: ir.TypeRegistry{model.ID: model}}
	var found bool
	for _, violation := range verifyDeclared(doc) {
		if violation.Code == "ir/naming-cased" {
			found = true
		}
	}
	assert.True(t, found, "a canonical ToLower would change must still be flagged")
}
