package irverify_test

import (
	"fmt"
	"reflect"
	"testing"
	"unicode/utf8"

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
// either — TestVerify_NoAliasesIsClean asserts this exact document, with the
// alias list empty, verifies empty.
func aliasViolations(t *testing.T, aliases ...string) []irverify.Violation {
	t.Helper()
	return irverify.Verify(modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases}))
}

// TestVerify_VerbatimAliasesAreClean pins the settlement this check rests on: an
// alias is matched against a name another schema wrote, so it is a verbatim
// channel like Source and none of Canonical's neutrality rules apply to it.
// "com.example.User" is what an Avro alias looks like, and between them these
// four aliases would draw all three of ir/naming-cased, ir/naming-not-words and
// ir/naming-unsegmented if an alias were held to Canonical's grammar — the
// dotted name the first two, "API2Key" the first and last.
func TestVerify_VerbatimAliasesAreClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t, "com.example.User", "UserID", "user_id", "API2Key"))
}

// TestVerify_BlankAliasIsAViolation covers the whole of what "names nothing"
// means. An entry made only of runes no grammar can render a name from is as
// blank as "", and testing emptiness alone — or trimming only what
// unicode.IsSpace reports — would let most of these through the one rule that
// exists to catch an entry matching nothing.
func TestVerify_BlankAliasIsAViolation(t *testing.T) {
	t.Parallel()
	blanks := map[string]string{
		"empty":         "",
		"space":         " ",
		"tab":           "\t",
		"newline":       "\n",
		"mixed spaces":  "  \t ",
		"no-break":      "\u00a0",
		"ideographic":   "\u3000",
		"zero width":    "\u200b",
		"byte order":    "\ufeff",
		"joiner":        "\u200d",
		"soft hyphen":   "\u00ad",
		"word joiner":   "\u2060",
		"nul":           "\x00",
		"hangul filler": "\u3164",
		"jamo filler":   "\u115f",
		"invisible mix": "\u200b\t\ufeff\u3164",
	}
	for name, alias := range blanks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := aliasViolations(t, "ok", alias)
			require.Len(t, got, 1, "one violation for the one blank entry")
			assert.Equal(t, "ir/naming-alias-blank", got[0].Code)
			assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[1]", got[0].Path,
				"the violation names the offending entry, not just the naming")
		})
	}
}

// TestVerify_InvisibleRuneBesideAVisibleOneIsNotBlank holds the other side of
// the line isBlankName draws — see its comment for why the IR declines to judge
// this one. U+2800 renders as blank but is a graphic character, so it belongs
// here rather than in the table above.
func TestVerify_InvisibleRuneBesideAVisibleOneIsNotBlank(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t, "com.example.\u200bUser", " padded ", "\u2800"))
}

// TestVerify_IllFormedAliasIsAViolation covers the rule every channel shares.
// The bytes here are a lone continuation byte: json.Marshal writes it as the
// replacement rune, so a document carrying one decodes to a different document
// and stops round-tripping — which no other rule in this file would notice,
// since ill-formed bytes are neither blank nor a repeat.
func TestVerify_IllFormedAliasIsAViolation(t *testing.T) {
	t.Parallel()
	ill := string([]byte{'c', 'a', 'f', 0xe9})
	require.False(t, utf8.ValidString(ill), "the fixture has to be ill-formed to test anything")

	got := aliasViolations(t, "ok", ill)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/naming-invalid-utf8", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[1]", got[0].Path)
	assert.NotContains(t, got[0].Message, ill, "the report does not repeat the bad bytes")
}

// TestVerify_AliasRepeatingItsOwnSourceIsAViolation covers the other way an
// entry can admit no name that was not already admitted. The entity's own
// Source is matched before any alias is, so listing it again adds nothing —
// the same argument the duplicate rule rests on, one channel over.
func TestVerify_AliasRepeatingItsOwnSourceIsAViolation(t *testing.T) {
	t.Parallel()
	doc := modelNamed(ir.Naming{Source: "User", Canonical: "user", Aliases: []string{"User"}})
	got := irverify.Verify(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/naming-alias-redundant", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[0]", got[0].Path)
	assert.Contains(t, got[0].Message, "User")
}

// TestVerify_AliasMatchingDerivedChannelsIsClean holds the boundary that rule
// stops at. Canonical and Hint are names the IR derived for an emitter to
// render, not names a writer schema could have spelled, so an alias equal to
// one of them is not redundant with anything a reader would match.
func TestVerify_AliasMatchingDerivedChannelsIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, irverify.Verify(modelNamed(
		ir.Naming{Source: "User", Canonical: "user", Aliases: []string{"user"}})))
	assert.Empty(t, irverify.Verify(modelNamed(
		ir.Naming{Hint: "user", Aliases: []string{"user"}})))
}

// TestVerify_AliasSharedByTwoNamings pins the scope boundary appendAliasViolations
// declares: a repeat across two Namings goes unreported today, and closing
// GitHub #398 is what should change it.
//
// Without this, nothing holds seen to being per-Naming. Hoisting it into
// checkNaming's closure — the one-line change anyone implementing #398 reaches
// for first — makes this document report ir/naming-alias-duplicate, and every
// other test in this file stays green because each drives a document with one
// Naming in it.
func TestVerify_AliasSharedByTwoNamings(t *testing.T) {
	t.Parallel()
	const shared = "com.example.User"
	a := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/A",
		Name: ir.Naming{Source: "a", Canonical: "a", Aliases: []string{shared}}}}
	b := &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/B",
		Name: ir.Naming{Source: "b", Canonical: "b", Aliases: []string{shared}}}}

	got := irverify.Verify(&ir.Document{IRVersion: ir.IRVersion,
		Types: ir.TypeRegistry{a.ID: a, b.ID: b}})
	assert.Empty(t, got, "out of scope until GitHub #398; this is the fixture that says so")
}

// TestVerify_DuplicateAliasIsAViolation asserts both ends of the pair. The path
// carries the entry to delete; the message carries the one it repeats, so a
// reader of a long list is not left scanning for the twin — which is how
// checkDuplicateIDs words the same defect ("declared here and at …").
func TestVerify_DuplicateAliasIsAViolation(t *testing.T) {
	t.Parallel()
	got := aliasViolations(t, "dup", "other", "dup")
	require.Len(t, got, 1, "the repeat is reported, not the first occurrence")
	assert.Equal(t, "ir/naming-alias-duplicate", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[2]", got[0].Path)
	assert.Equal(t, "alias dup is listed here and at index 0", got[0].Message)
}

// TestVerify_RepeatedBlankAliasReportsEachAsBlank holds the interaction between
// the two rules: a second blank entry is a repeat as well as a blank one, and
// reporting it as a duplicate would name the wrong repair.
//
// Each case repeats *the same* string, which is what makes the claim testable.
// With two different blanks the duplicate rule cannot fire whatever the
// implementation does, so the natural rewrite — report the repeat, then the
// blank, setting seen unconditionally — passes a two-different-blanks fixture
// while emitting exactly the wrong repair this test forbids.
func TestVerify_RepeatedBlankAliasReportsEachAsBlank(t *testing.T) {
	t.Parallel()
	for name, alias := range map[string]string{"empty": "", "space": " ", "zero width": "\u200b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := aliasViolations(t, alias, alias)
			require.Len(t, got, 2)
			for _, v := range got {
				assert.Equal(t, "ir/naming-alias-blank", v.Code)
			}
		})
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

// TestVerify_AliasPathIsSpelledAsTheWalkWould ties the hand-assembled violation
// path to ir.WalkValues' own grammar.
//
// checkNaming prunes at ir.Naming — it holds no reference and no nested Naming
// to descend into — so the walk never renders these paths itself and the check
// spells them by hand. That leaves two statements of one grammar with nothing
// between them: were ir's slice-index rendering to change, every walk-produced
// path in every other check would move while these two codes alone kept the old
// spelling, and no test would say so. This is that seam, so it reddens here.
//
// Past the single digits too, which is where a hand-built path and a formatted
// one last agree.
func TestVerify_AliasPathIsSpelledAsTheWalkWould(t *testing.T) {
	t.Parallel()
	const size = 12
	aliases := make([]string, size)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("alias.%d", i) // distinct, so no entry is a repeat
	}
	doc := modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases})

	walked := map[string]string{} // alias → the path the walk renders for it
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && v.String() != "" {
			walked[v.String()] = path
		}
		return true
	})
	for _, alias := range aliases {
		require.Contains(t, walked, alias, "the walk reaches every entry when nothing prunes it")
	}

	// Blank every entry so the check reports one violation per index, then hold
	// each reported path to the one the walk rendered at that same index.
	blank := make([]string, size)
	got := aliasViolations(t, blank...)
	require.Len(t, got, size)
	paths := make([]string, len(got))
	for i, v := range got {
		paths[i] = v.Path
	}
	want := make([]string, 0, size)
	for _, alias := range aliases {
		want = append(want, walked[alias])
	}
	assert.ElementsMatch(t, want, paths)
}
