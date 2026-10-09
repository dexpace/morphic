package schema_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// memberIDs lowers one enum schema named S and returns its members' IDs in
// source order, plus the diagnostics.
func memberIDs(t *testing.T, enum string) ([]ir.EnumMemberID, []ir.Diagnostic) {
	t.Helper()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec("    S:\n      enum: "+enum+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	e, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	out := make([]ir.EnumMemberID, 0, len(e.Members))
	for _, m := range e.Members {
		out = append(out, m.ID)
	}
	return out, diags
}

func assertDistinct(t *testing.T, ids []ir.EnumMemberID) {
	t.Helper()
	seen := map[ir.EnumMemberID]bool{}
	for _, id := range ids {
		assert.NotEmpty(t, id)
		assert.False(t, seen[id], "%q repeats", id)
		seen[id] = true
	}
}

func TestEnumMemberIDs_SharedCanonicalNameStillDistinct(t *testing.T) {
	t.Parallel()
	ids, diags := memberIDs(t, `["foo-bar", "foo_bar", "Foo Bar"]`)
	assert.Len(t, ids, 3)
	assertDistinct(t, ids)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DuplicateEnumValue, ir.SeverityWarning))
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/S/s:foo-bar"), ids[0])
}

func TestEnumMemberIDs_SignedNumbersAreThreeIDs(t *testing.T) {
	t.Parallel()
	ids, _ := memberIDs(t, `[-1, 1, 0]`)
	assert.Len(t, ids, 3)
	assertDistinct(t, ids)
}

func TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs(t *testing.T) {
	t.Parallel()
	ids, _ := memberIDs(t, `["<", "<=", ">", ">="]`)
	assert.Len(t, ids, 4)
	assertDistinct(t, ids)
}

func TestEnumMemberIDs_StringAndNumberOfEqualTextDiffer(t *testing.T) {
	t.Parallel()
	one, _ := memberIDs(t, `[1]`)
	str, _ := memberIDs(t, `["1"]`)
	assert.NotEqual(t, one, str)
}

func TestEnumMemberIDs_ReorderKeepsTheIDSet(t *testing.T) {
	t.Parallel()
	forward, _ := memberIDs(t, `["a", "b", "c"]`)
	backward, _ := memberIDs(t, `["c", "b", "a"]`)
	inserted, _ := memberIDs(t, `["a", "x", "b", "c"]`)

	assert.Empty(t, cmp.Diff(sortedIDs(forward), sortedIDs(backward)))
	for _, id := range forward {
		assert.Contains(t, inserted, id, "an insertion keeps every existing ID")
	}
}

func sortedIDs(ids []ir.EnumMemberID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	slices.Sort(out)
	return out
}

func TestEnumMemberIDs_RepeatedValueIsKeptSuffixedAndWarned(t *testing.T) {
	t.Parallel()
	ids, diags := memberIDs(t, `["a", "b", "a", "a"]`)
	assert.Equal(t, []ir.EnumMemberID{
		"e/openapi/components/schemas/S/s:a",
		"e/openapi/components/schemas/S/s:b",
		"e/openapi/components/schemas/S/s:a#2",
		"e/openapi/components/schemas/S/s:a#3",
	}, ids)
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DuplicateEnumValue, ir.SeverityWarning),
		"one warning per repeat; got %+v", diags)
}

func TestEnumMemberIDs_AreScopedToTheirEnum(t *testing.T) {
	t.Parallel()
	doc, diags := lowerSpec(t, openapitest.ComponentSpec("    A:\n      enum: [x]\n    B:\n      enum: [x]\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	a := typeByName(doc, "A").(*ir.Enum)
	b := typeByName(doc, "B").(*ir.Enum)
	assert.NotEqual(t, a.Members[0].ID, b.Members[0].ID)
	assert.True(t, strings.HasSuffix(string(a.Members[0].ID), "/A/s:x"), fmt.Sprint(a.Members[0].ID))
}
