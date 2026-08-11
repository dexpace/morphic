package irverify_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// aliasViolations returns everything Verify reports on a model named with
// aliases.
//
// Unfiltered on purpose. The test below asserting this is empty is the one
// pinning that no neutrality rule reaches an alias, so it has to be able to see
// ir/naming-cased and ir/naming-not-words if some later change starts holding
// aliases to Canonical's grammar; filtering to the alias codes would leave that
// test unable to fail for the reason it exists. Nothing unrelated is in the way
// either — TestVerify_NeutralCanonicalIsClean asserts this same document, minus
// the aliases, verifies empty.
func aliasViolations(t *testing.T, aliases ...string) []irverify.Violation {
	t.Helper()
	return irverify.Verify(modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases}))
}

// TestVerify_VerbatimAliasesAreClean pins the settlement this check rests on: an
// alias is matched against a name another schema wrote, so it is a verbatim
// channel like Source and none of Canonical's neutrality rules apply to it.
// "com.example.User" is what an Avro alias looks like, and every one of
// ir/naming-cased, ir/naming-not-words and ir/naming-unsegmented would fire on
// it if the alias were held to Canonical's grammar.
func TestVerify_VerbatimAliasesAreClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t, "com.example.User", "UserID", "user_id", "API2Key"))
}

// TestVerify_BlankAliasIsAViolation covers the whole of what "names nothing"
// means. Whitespace is as blank as "" — no format's grammar admits a name made
// of it — and testing emptiness alone would let " " through the one rule that
// exists to catch an entry matching nothing.
func TestVerify_BlankAliasIsAViolation(t *testing.T) {
	t.Parallel()
	for _, alias := range []string{"", " ", "\t", "\n", "  \t "} {
		t.Run(strconv.Quote(alias), func(t *testing.T) {
			t.Parallel()
			got := aliasViolations(t, "ok", alias)
			require.Len(t, got, 1, "one violation for the one blank entry")
			assert.Equal(t, "ir/naming-alias-blank", got[0].Code)
			assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[1]", got[0].Path,
				"the violation names the offending entry, not just the naming")
		})
	}
}

func TestVerify_DuplicateAliasIsAViolation(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "dup", "other", "dup")
	require.Len(t, got, 1, "the repeat is reported, not the first occurrence")
	assert.Equal(t, "ir/naming-alias-duplicate", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[2]", got[0].Path)
	assert.Contains(t, got[0].Message, "dup")
}

// TestVerify_RepeatedBlankAliasReportsEachAsBlank holds the interaction between
// the two rules: a second blank entry is a repeat as well as a blank one, and
// reporting it as a duplicate would name the wrong repair.
func TestVerify_RepeatedBlankAliasReportsEachAsBlank(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "", " ")
	require.Len(t, got, 2)
	for _, v := range got {
		assert.Equal(t, "ir/naming-alias-blank", v.Code)
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
		"ir/naming-alias-blank":     "doc.Types[t/x/M].Name.Aliases[2]",
		"ir/naming-alias-duplicate": "doc.Types[t/x/M].Name.Aliases[4]",
	}, byCode)
}

func TestVerify_NoAliasesIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t))
}
