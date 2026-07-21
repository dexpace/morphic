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
	// Re-key the model under an ID that disagrees with its Common().ID. This also
	// dangles the model's own self-reference, so both codes are expected.
	m := doc.Types["t/x/Model"]
	delete(doc.Types, "t/x/Model")
	doc.Types["t/x/WrongKey"] = m

	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/type-id-mismatch")
}

func TestVerify_SortsSameCodeByPath(t *testing.T) {
	// Two properties dangle distinct missing targets, yielding two violations that
	// share the ir/dangling-type-ref code. Verify must order them by Path, which
	// exercises the sort's equal-code tiebreaker.
	holder := &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/x/Holder"},
		Properties: []ir.Property{
			{ID: "p/x/Holder/a", Type: ir.TypeRef{Target: "t/x/MissingA"}},
			{ID: "p/x/Holder/b", Type: ir.TypeRef{Target: "t/x/MissingB"}},
		},
	}
	doc := &ir.Document{Types: ir.TypeRegistry{holder.ID: holder}}

	var dangling []irverify.Violation
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/dangling-type-ref" {
			dangling = append(dangling, v)
		}
	}
	require.Len(t, dangling, 2)
	assert.Less(t, dangling[0].Path, dangling[1].Path, "same-code violations are ordered by Path")
}

// codesOf extracts the Code of each violation for order-independent assertions.
func codesOf(vs []irverify.Violation) []string {
	codes := make([]string, len(vs))
	for i, v := range vs {
		codes[i] = v.Code
	}
	return codes
}

func TestVerify_NilTypeDefIsAViolation(t *testing.T) {
	// An untyped nil in the Types registry must be reported, not dereferenced:
	// Verify stays a report-only oracle that never panics on a malformed document.
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/nil": nil}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/nil-type")
}

func TestVerify_TypedNilTypeDefIsAViolation(t *testing.T) {
	// A typed nil pointer is a non-nil interface whose Common() still panics, so
	// it must be reported as ir/nil-type rather than crashing the walk.
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/typednil": (*ir.Model)(nil)}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/nil-type")
}

func TestVerify_EmptyTypeIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{"": &ir.Any{}}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-type-id", got[0].Code)
}

func TestVerify_EmptyChannelIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Channels: map[ir.ChannelID]ir.Channel{"": {}}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-channel-id", got[0].Code)
}

func TestVerify_EmptyMessageIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Messages: map[ir.MessageID]ir.Message{"": {}}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-message-id", got[0].Code)
}

func TestVerify_EmptyAuthIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{"": {}}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-auth-id", got[0].Code)
}

func TestVerify_ChannelIDMismatchIsAViolation(t *testing.T) {
	// Key the channel under an ID that disagrees with its node ID. This also
	// dangles the channel's own self-reference, so both codes are expected.
	doc := &ir.Document{Channels: map[ir.ChannelID]ir.Channel{
		"c/x/WrongKey": {ID: "c/x/Ch"},
	}}
	got := irverify.Verify(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/channel-id-mismatch")
}
