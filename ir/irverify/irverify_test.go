// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// validDoc is a minimal structurally-sound document: one model whose registry
// key matches its own Common().ID.
func validDoc() *ir.Document {
	m := &ir.Model{ID: "t/x/Model", Name: ir.Naming{Source: "Model", Canonical: "model"}}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{m.ID: m},
	}
}

// named is the naming of an entity whose source spelling is already a neutral
// word sequence, so the grammar check recomputes the same canonical from it. It
// exists so a fixture built to exercise some other check can satisfy the
// presence rule without spelling a Naming literal at every node.
func named(source string) ir.Naming {
	return ir.Naming{Source: source, Canonical: source}
}

func TestVerify_CleanDocHasNoViolations(t *testing.T) {
	got := verifyDeclared(validDoc())
	assert.Empty(t, got)
}

func TestVerify_NilDocIsAViolation(t *testing.T) {
	got := verifyDeclared(nil)
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

	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/type-id-mismatch")
}

func TestVerify_SortsSameCodeByPath(t *testing.T) {
	// Eleven properties dangle distinct missing targets, yielding violations that
	// share the ir/dangling-type-ref code. The walk reports them by index, but as
	// bytes Properties[10] sorts before Properties[1], so walk order and Path
	// order part at the second entry. With fewer than eleven the two orders agree
	// and the sort's equal-code tiebreaker would go unexercised.
	const count = 11
	holder := &ir.Model{ID: "t/x/Holder"}
	for i := range count {
		n := strconv.Itoa(i)
		holder.Properties = append(holder.Properties, ir.Property{
			ID:   ir.PropID("p/x/Holder/" + n),
			Type: ir.TypeRef{Target: ir.TypeID("t/x/Missing" + n)},
		})
	}
	doc := &ir.Document{Types: ir.TypeRegistry{holder.ID: holder}}

	var got []string
	for _, v := range verifyDeclared(doc) {
		if v.Code == "ir/dangling-type-ref" {
			got = append(got, v.Path)
		}
	}
	require.Len(t, got, count)

	want := make([]string, 0, count)
	for _, i := range []int{0, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9} {
		want = append(want, "doc.Types[t/x/Holder].Properties["+strconv.Itoa(i)+"].Type.Target")
	}
	assert.Equal(t, want, got, "same-code violations are ordered by Path as bytes")
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
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/nil-type")
}

func TestVerify_TypedNilTypeDefIsAViolation(t *testing.T) {
	// A typed nil pointer is a non-nil interface whose Common() still panics, so
	// it must be reported as ir/nil-type rather than crashing the walk.
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/typednil": (*ir.Model)(nil)}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/nil-type")
}

func TestVerify_EmptyTypeIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{"": &ir.Any{}}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-type-id", got[0].Code)
}

func TestVerify_EmptyChannelIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Channels: map[ir.ChannelID]ir.Channel{"": {}}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-channel-id", got[0].Code)
}

func TestVerify_EmptyMessageIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Messages: map[ir.MessageID]ir.Message{"": {}}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-message-id", got[0].Code)
}

func TestVerify_EmptyAuthIDIsAViolation(t *testing.T) {
	doc := &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{"": {}}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Equal(t, "ir/empty-auth-id", got[0].Code)
}

func TestVerify_ChannelIDMismatchIsAViolation(t *testing.T) {
	// Key the channel under an ID that disagrees with its node ID. This also
	// dangles the channel's own self-reference, so both codes are expected.
	doc := &ir.Document{Channels: map[ir.ChannelID]ir.Channel{
		"c/x/WrongKey": {ID: "c/x/Ch"},
	}}
	got := verifyDeclared(doc)
	require.NotEmpty(t, got)
	assert.Contains(t, codesOf(got), "ir/channel-id-mismatch")
}
