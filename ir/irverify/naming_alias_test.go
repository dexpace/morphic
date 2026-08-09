package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// aliasViolations returns the alias violations in a model named with aliases,
// filtered by code prefix so a test asserting none is not satisfied by some
// unrelated violation being absent.
func aliasViolations(t *testing.T, aliases ...string) []irverify.Violation {
	t.Helper()
	doc := modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases})
	var out []irverify.Violation
	for _, v := range irverify.Verify(doc) {
		if strings.HasPrefix(v.Code, "ir/naming-alias-") {
			out = append(out, v)
		}
	}
	return out
}

// TestVerify_VerbatimAliasesAreClean pins the settlement this check rests on: an
// alias is matched against a name another schema wrote, so it is a verbatim
// channel like Source and none of Canonical's neutrality rules apply to it.
// "com.example.User" is what an Avro alias looks like, and every one of
// ir/naming-cased, ir/naming-not-words and ir/naming-unsegmented would fire on
// it if the alias were held to Canonical's grammar.
func TestVerify_VerbatimAliasesAreClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t, "com.example.User", "UserID", "user_id"))
}

func TestVerify_EmptyAliasIsAViolation(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "ok", "")
	require.Len(t, got, 1, "one violation for the one empty entry")
	assert.Equal(t, "ir/naming-alias-empty", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[1]", got[0].Path,
		"the violation names the offending entry, not just the naming")
}

func TestVerify_DuplicateAliasIsAViolation(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "dup", "other", "dup")
	require.Len(t, got, 1, "the repeat is reported, not the first occurrence")
	assert.Equal(t, "ir/naming-alias-duplicate", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[2]", got[0].Path)
	assert.Contains(t, got[0].Message, "dup")
}

// TestVerify_RepeatedEmptyAliasReportsEachAsEmpty holds the interaction between
// the two rules: a second empty entry is a repeat as well as an empty one, and
// reporting it as a duplicate would name the wrong repair.
func TestVerify_RepeatedEmptyAliasReportsEachAsEmpty(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "", "")
	require.Len(t, got, 2)
	for _, v := range got {
		assert.Equal(t, "ir/naming-alias-empty", v.Code)
	}
}

// TestVerify_IssueReproducerIsReported drives the exact value from the issue —
// cased, punctuated, empty and duplicated together — and states which of the
// four the IR objects to and which it accepts by design.
func TestVerify_IssueReproducerIsReported(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "UserID", "com.example.User", "", "dup", "dup")
	require.Len(t, got, 2, "the cased and dotted entries are legitimate aliases")

	// Keyed rather than indexed: Verify sorts by (Code, Path), so asserting
	// positionally would pin the sort order rather than what was reported.
	byCode := map[string]string{}
	for _, v := range got {
		byCode[v.Code] = v.Path
	}
	assert.Equal(t, map[string]string{
		"ir/naming-alias-empty":     "doc.Types[t/x/M].Name.Aliases[2]",
		"ir/naming-alias-duplicate": "doc.Types[t/x/M].Name.Aliases[4]",
	}, byCode)
}

func TestVerify_NoAliasesIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t))
}
