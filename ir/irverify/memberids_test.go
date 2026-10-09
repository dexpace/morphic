package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

func enumDoc(members ...ir.EnumMember) *ir.Document {
	e := &ir.Enum{
		ID: "t/openapi/components/schemas/E", Name: ir.Naming{Source: "E", Canonical: "e"},
		ValueType: ir.PrimString, Members: members,
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{e.ID: e}}
}

func member(id ir.EnumMemberID, v string) ir.EnumMember {
	return ir.EnumMember{ID: id, Name: ir.Naming{Source: v, Canonical: v}, Value: ir.Value{Kind: ir.ValueString, Str: v}}
}

func TestVerify_DerivedMemberIDsAreClean(t *testing.T) {
	t.Parallel()
	doc := enumDoc(
		member("e/openapi/components/schemas/E/s:a", "a"),
		member("e/openapi/components/schemas/E/s:a#2", "a"),
	)
	assert.Empty(t, irverify.Verify(doc))
}

func TestVerify_MalformedMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	for _, id := range []ir.EnumMemberID{"x/openapi/c/E/s:a", "e/", "e//E/s:a", "e/openapi/"} {
		doc := enumDoc(member(id, "a"))
		assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/id-malformed", "%q", id)
	}
}

func TestVerify_MemberIDOutsideItsEnumIsAViolation(t *testing.T) {
	t.Parallel()
	for _, id := range []ir.EnumMemberID{
		"e/anon/components/schemas/E/s:a",        // another space
		"e/openapi/components/schemas/Other/s:a", // another enum's path
		"e/openapi/components/schemas/E",         // no member segment
		"e/openapi/components/schemas/EX/s:a",    // a sibling sharing a prefix
	} {
		doc := enumDoc(member(id, "a"))
		assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/member-id-scope", "%q", id)
	}
}

func TestVerify_DuplicateMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	doc := enumDoc(
		member("e/openapi/components/schemas/E/s:a", "a"),
		member("e/openapi/components/schemas/E/s:a", "b"),
	)
	assert.Contains(t, violationCodes(irverify.Verify(doc)), "ir/duplicate-enummember-id")
}

func TestVerify_EmptyMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	doc := enumDoc(member("", "a"))
	got := violationCodes(irverify.Verify(doc))
	assert.Contains(t, got, "ir/empty-enummember-id")
	assert.NotContains(t, got, "ir/id-malformed", "the empty one is reported once, by its own check")
}
