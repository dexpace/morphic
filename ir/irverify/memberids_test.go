package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// memberDoc returns a document holding the enum id with members of the given IDs.
func memberDoc(id ir.TypeID, members ...ir.EnumMemberID) *ir.Document {
	enum := &ir.Enum{ID: id, Name: ir.Naming{Source: "E", Canonical: "e"}}
	for _, m := range members {
		enum.Members = append(enum.Members, ir.EnumMember{ID: m})
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{id: enum}}
}

// memberCodes are the codes a member ID can draw, so a test asserts all of them
// at once and no check silently absorbs another's defect.
var memberCodes = []string{
	"ir/member-id-scope", "ir/id-malformed", "ir/empty-enummember-id",
	"ir/duplicate-enummember-id", "ir/id-space-undeclared",
}

// memberViolations returns doc's violations among memberCodes, by code.
func memberViolations(doc *ir.Document) map[string][]string {
	got := verifyDeclared(doc)
	out := map[string][]string{}
	for _, code := range memberCodes {
		if paths := violationPaths(got, code); len(paths) > 0 {
			out[code] = paths
		}
	}
	return out
}

func TestVerify_DerivedMemberIDsAreClean(t *testing.T) {
	t.Parallel()
	named := memberDoc("t/openapi/components/schemas/E",
		"e/openapi/components/schemas/E/s:a", "e/openapi/components/schemas/E/s:a#2", "e/openapi/components/schemas/E/n:-1")
	inline := memberDoc("t/anon/paths/~1x/get/schema", "e/anon/paths/~1x/get/schema/s:a")
	assert.Empty(t, memberViolations(named))
	assert.Empty(t, memberViolations(inline))
}

func TestVerify_MalformedMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	doc := memberDoc("t/openapi/components/schemas/E", "x/space/path")
	assert.Equal(t, map[string][]string{
		"ir/id-malformed": {"doc.Types[t/openapi/components/schemas/E].Members[0]"},
	}, memberViolations(doc), "reported once, by the grammar check, not again by the scope check")
}

func TestVerify_MemberIDOutsideItsEnumIsAViolation(t *testing.T) {
	t.Parallel()
	const enum = ir.TypeID("t/openapi/components/schemas/E")
	for _, tc := range []struct {
		name   string
		member ir.EnumMemberID
	}{
		{"another enum's path", "e/openapi/components/schemas/Other/s:a"},
		{"a sibling sharing the enum's name as a prefix", "e/openapi/components/schemas/E2/s:a"},
		{"another namespace", "e/anon/components/schemas/E/s:a"},
		{"the enum's own path with no key", "e/openapi/components/schemas/E"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := memberViolations(memberDoc(enum, tc.member))
			assert.Equal(t, []string{"doc.Types[t/openapi/components/schemas/E].Members[0]"}, got["ir/member-id-scope"])
			assert.NotContains(t, got, "ir/id-malformed")
		})
	}
}

func TestVerify_DuplicateMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	doc := memberDoc("t/openapi/components/schemas/E",
		"e/openapi/components/schemas/E/s:a", "e/openapi/components/schemas/E/s:a")
	assert.Equal(t, map[string][]string{
		"ir/duplicate-enummember-id": {"doc.Types[t/openapi/components/schemas/E].Members[1]"},
	}, memberViolations(doc))
}

func TestVerify_EmptyMemberIDIsAViolation(t *testing.T) {
	t.Parallel()
	doc := memberDoc("t/openapi/components/schemas/E", "")
	assert.Equal(t, map[string][]string{
		"ir/empty-enummember-id": {"doc.Types[t/openapi/components/schemas/E].Members[0]"},
	}, memberViolations(doc), "empty is checkDeclaredIDs' to report, and the scope check leaves it alone")
}

// TestVerify_MemberIDInAnUndeclaredSpaceIsAViolation pins that a member minted in
// a namespace the document does not declare for its kind, such as the one that
// holds synthesized union variants, is seen by the declaration check.
func TestVerify_MemberIDInAnUndeclaredSpaceIsAViolation(t *testing.T) {
	t.Parallel()
	doc := memberDoc("t/composed/components/schemas/E", "e/composed/components/schemas/E/s:a")
	doc.IDSpaces = map[string][]string{
		ir.IDKindType:       {"composed"},
		ir.IDKindEnumMember: {"anon", "openapi"},
	}
	got := irverify.Verify(doc)
	require.Equal(t, []string{"doc.Types[t/composed/components/schemas/E].Members[0]"},
		violationPaths(got, "ir/id-space-undeclared"))
	assert.Empty(t, violationPaths(got, "ir/member-id-scope"), "the member is in its enum's namespace")
}
