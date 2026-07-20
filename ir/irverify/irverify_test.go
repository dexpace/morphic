package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// validDoc is a minimal structurally-sound document: one model whose registry
// key matches its own Common().ID.
func validDoc() *ir.Document {
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/Model", Name: ir.Naming{Source: "Model", Canonical: "model"}}}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{m.ID: m},
	}
}

func TestVerify_CleanDocHasNoViolations(t *testing.T) {
	got := irverify.Verify(validDoc())
	assert.Empty(t, got)
}

func TestVerify_NilDocIsAViolation(t *testing.T) {
	got := irverify.Verify(nil)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/nil-document", got[0].Code)
}

func TestVerify_RegistryKeyMismatchIsAViolation(t *testing.T) {
	doc := validDoc()
	// Re-key the model under an ID that disagrees with its Common().ID.
	m := doc.Types["t/x/Model"]
	delete(doc.Types, "t/x/Model")
	doc.Types["t/x/WrongKey"] = m

	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/type-id-mismatch", got[0].Code)
}

func TestVerify_EmptyTypeIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{"": &ir.Any{}}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-type-id", got[0].Code)
}
