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
