// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"fmt"
	"reflect"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// aliasViolations returns everything Verify reports on a model named with
// aliases. Its Source is "m", which no fixture below lists, so the
// redundant-with-Source rule stays out of the way.
//
// Unfiltered on purpose: TestVerify_VerbatimAliasesAreClean must be able to see
// ir/naming-cased and ir/naming-not-words if aliases are ever held to
// Canonical's grammar, which filtering to the alias codes would hide. Nothing
// unrelated is in the way, since TestVerify_NoAliasesIsClean asserts this
// document verifies empty with no aliases.
func aliasViolations(t *testing.T, aliases ...string) []irverify.Violation {
	t.Helper()
	return verifyDeclared(modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases}))
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

func TestVerify_NoAliasesIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t))
}

// TestVerify_BlankAliasIsAViolation covers the whole of what "names nothing"
// means. An entry made only of runes the rule takes as invisible is as blank as
// "", and testing emptiness alone — or trimming only what unicode.IsSpace
// reports — would let most of these through the one rule that exists to catch
// an entry matching nothing.
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
		// Variation_Selector: Mn, so neither Cf nor Other_Default_Ignorable.
		"variation selector":            "\ufe0f",
		"variation selector supplement": "\U000e0100",
		"mongolian selector":            "\u180b",
		"mongolian selector four":       "\u180f",
		// Other_Default_Ignorable, directly below the excluded U+FFF9.
		"reserved before annotation": "\ufff8",
		// White_Space, though it renders a line.
		"ogham space mark": "\u1680",
		// White_Space in neither Zs nor Cc.
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
		// Controls past C0, which only their own term holds.
		"delete":     "\x7f",
		"c1 control": "\u0080",
		// Other_Default_Ignorable in Mn, which no other term holds.
		"combining grapheme joiner": "\u034f",
		// Cf and Other_Default_Ignorable past the BMP, which no other row holds.
		"tag character": "\U000e0041",
		"reserved tag":  "\U000e0000",
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

// TestVerify_InvisibleRuneBesideAVisibleOneIsNotBlank holds one side of the line
// isBlankName draws — see its comment for why the IR declines to judge an
// invisible rune that is not the whole entry.
func TestVerify_InvisibleRuneBesideAVisibleOneIsNotBlank(t *testing.T) {
	t.Parallel()
	assert.Empty(t, aliasViolations(t, "com.example.\u200bUser", " padded "))
}

// TestVerify_VisibleAliasIsNotBlank holds the other side. U+2800
// BRAILLE PATTERN BLANK renders as whitespace but is outside the set the blank
// rules take as invisible, so the rule leaves it alone rather than deciding on
// its own account what looks empty. It leaves alone, too, the format characters
// Unicode takes out of Default_Ignorable_Code_Point because they render: a
// prepended concatenation mark, an interlinear annotation character and an
// Egyptian hieroglyph format control. The rows beside those ranges and the
// private use one hold a term from widening over them: U+13440 from Mn, U+E000
// from C, U+FFFC from So, U+1342F from Lo.
func TestVerify_VisibleAliasIsNotBlank(t *testing.T) {
	t.Parallel()
	visible := map[string]string{
		"braille blank":       "\u2800",
		"arabic number sign":  "\u0600",
		"arabic end of ayah":  "\u06dd",
		"syriac abbreviation": "\u070f",
		"arabic pound mark":   "\u0890",
		"arabic disputed end": "\u08e2",
		"kaithi number sign":  "\U000110bd",
		"annotation anchor":   "\ufff9",
		"annotation end":      "\ufffb",
		"egyptian format":     "\U00013430",
		"egyptian last":       "\U0001343f",
		"object replacement":  "\ufffc",
		"egyptian before":     "\U0001342f",
		"egyptian mirror":     "\U00013440",
		"private use":         "\ue000",
	}
	for name, alias := range visible {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, aliasViolations(t, alias))
		})
	}
}

// TestIsInvisible_RangesWereCheckedAgainstThisUnicode fails when a Go release moves
// unicode.Version, since isInvisible holds two ranges no table of package unicode
// carries, and Unicode has changed the derivation itself between releases, not
// only those ranges. Rebuild isInvisible from DerivedCoreProperties.txt's
// Default_Ignorable_Code_Point derivation and sweep every code point against it,
// then move the version here.
func TestIsInvisible_RangesWereCheckedAgainstThisUnicode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "17.0.0", unicode.Version,
		"rebuild isInvisible from DerivedCoreProperties.txt's Default_Ignorable_Code_Point "+
			"derivation and sweep every code point against it, then move this version")
}

// TestVerify_IllFormedAliasIsAViolation covers the rule every channel shares,
// now checkUTF8's rather than the alias switch's own: the bytes end in a
// multibyte sequence that never completes, and a Document refuses to encode a
// string that is not UTF-8, so a document carrying one cannot be marshaled. No
// alias rule fires beside it: read as runes, the stray byte is the visible
// replacement rune, so the entry is not blank, and the switch's own case for
// an ill-formed entry is empty — checkUTF8 reports it instead — which is why
// this is exactly one violation rather than two.
func TestVerify_IllFormedAliasIsAViolation(t *testing.T) {
	t.Parallel()
	ill := string([]byte{'c', 'a', 'f', 0xe9})
	require.False(t, utf8.ValidString(ill), "the fixture has to be ill-formed to test anything")

	got := aliasViolations(t, "ok", ill)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/invalid-utf8", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[1]", got[0].Path)
	assert.NotContains(t, got[0].Message, ill, "the report does not repeat the bad bytes")
}

// TestVerify_DuplicateIllFormedAliasIsNotFlaggedTwice holds the same
// interaction one rule further than TestVerify_RepeatedBlankAliasReportsEachAsBlank
// and TestVerify_RepeatedSourceAliasReportsEachAsRedundant do for their own
// switch cases: an ill-formed alias never reaches seen, so a second occurrence
// of the exact same bytes is not a repeat either, and checkUTF8 alone reports
// each occurrence at its own index. Were the switch's empty case removed
// instead of kept, the second occurrence would fall through to the duplicate
// rule, which names the first occurrence by quoting it — putting the ill-formed
// bytes into a message for the first time.
func TestVerify_DuplicateIllFormedAliasIsNotFlaggedTwice(t *testing.T) {
	t.Parallel()
	ill := string([]byte{'c', 'a', 'f', 0xe9})
	require.False(t, utf8.ValidString(ill), "the fixture has to be ill-formed to test anything")

	got := aliasViolations(t, ill, ill)
	require.Len(t, got, 2, "one per occurrence, from checkUTF8 alone")
	for i, v := range got {
		assert.Equal(t, "ir/invalid-utf8", v.Code)
		assert.Equal(t, fmt.Sprintf("doc.Types[t/x/M].Name.Aliases[%d]", i), v.Path)
		assert.NotContains(t, v.Message, ill, "the report does not repeat the bad bytes")
	}
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
	assert.Equal(t, `alias "dup" is listed here and at index 0`, got[0].Message)
}

// TestVerify_AliasRepeatingItsOwnSourceIsAViolation covers the other way an
// entry can admit no name that was not already admitted. A reader matches the
// entity's own Source before any alias, so listing it again adds nothing — the
// argument the duplicate rule rests on, one channel over.
func TestVerify_AliasRepeatingItsOwnSourceIsAViolation(t *testing.T) {
	t.Parallel()
	got := verifyDeclared(modelNamed(
		ir.Naming{Source: "User", Canonical: "user", Aliases: []string{"User"}}))
	require.Len(t, got, 1)
	assert.Equal(t, "ir/naming-alias-redundant", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Name.Aliases[0]", got[0].Path)
	assert.Equal(t, `alias "User" is the entity's own source name, so it matches nothing more`,
		got[0].Message)
}

// TestVerify_AliasMatchingDerivedChannelsIsClean holds the boundary that rule
// stops at. Canonical and Hint are names the IR derived for an emitter to
// render, not names a writer schema could have spelled, so an alias equal to
// one of them is not redundant with anything a reader would match.
func TestVerify_AliasMatchingDerivedChannelsIsClean(t *testing.T) {
	t.Parallel()
	for channel, n := range map[string]ir.Naming{
		"canonical": {Source: "User", Canonical: "user", Aliases: []string{"user"}},
		"hint":      {Hint: "user", Aliases: []string{"user"}},
	} {
		t.Run(channel, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, verifyDeclared(modelNamed(n)))
		})
	}
}

// TestVerify_RepeatedBlankAliasReportsEachAsBlank holds the interaction between
// the blank rule and the duplicate rule: a second blank entry is a repeat as
// well as a blank one, and reporting it as a duplicate would name the wrong
// repair.
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

// TestVerify_RepeatedSourceAliasReportsEachAsRedundant holds the same
// interaction one rule over. Both entries name what Source already names, so
// both go; recording a redundant entry in seen would report the second as a
// duplicate of the first and leave a reader deleting one of two entries that
// are each wrong on their own.
func TestVerify_RepeatedSourceAliasReportsEachAsRedundant(t *testing.T) {
	t.Parallel()
	got := verifyDeclared(modelNamed(
		ir.Naming{Source: "User", Canonical: "user", Aliases: []string{"User", "User"}}))
	require.Len(t, got, 2)
	for i, v := range got {
		assert.Equal(t, "ir/naming-alias-redundant", v.Code)
		assert.Equal(t, fmt.Sprintf("doc.Types[t/x/M].Name.Aliases[%d]", i), v.Path)
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

// TestVerify_AliasClaimedByAnotherEntityIsClean pins what irverify leaves to
// others (docs/ir/naming.md). Across the type registry no rule decides a
// collision: a relative alias resolves against its own namespace, and two
// independent schemas may each declare one. Between sibling properties it is a
// problem in the spec, which pass/validate reports.
//
// The first case puts one alias on two Namings, so hoisting seen into
// checkNaming's closure, the first change a cross-Naming rule reaches for,
// reddens it as ir/naming-alias-duplicate.
func TestVerify_AliasClaimedByAnotherEntityIsClean(t *testing.T) {
	t.Parallel()
	const shared = "com.example.User"
	aliased := func(source string) ir.Naming {
		return ir.Naming{Source: source, Canonical: source, Aliases: []string{shared}}
	}
	declared := ir.Naming{Source: shared, Canonical: ir.CanonicalWords(shared)}
	property := func(id ir.PropID, n ir.Naming) ir.Property {
		return ir.Property{ID: id, Name: n, Type: ir.TypeRef{Target: "t/x/A"}}
	}

	for name, types := range map[string]ir.TypeRegistry{
		"alias on two types": {
			"t/x/A": &ir.Model{ID: "t/x/A", Name: aliased("a")},
			"t/x/B": &ir.Model{ID: "t/x/B", Name: aliased("b")},
		},
		"alias naming another type": {
			"t/x/A": &ir.Model{ID: "t/x/A", Name: aliased("a")},
			"t/x/B": &ir.Model{ID: "t/x/B", Name: declared},
		},
		"alias naming a sibling property": {
			"t/x/A": &ir.Model{ID: "t/x/A", Name: ir.Naming{Source: "a", Canonical: "a"},
				Properties: []ir.Property{
					property("p/x/A/b", aliased("b")),
					property("p/x/A/c", declared),
				}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, verifyDeclared(&ir.Document{IRVersion: ir.IRVersion, Types: types}))
		})
	}
}

// TestVerify_AliasPathIsSpelledAsTheWalkWould ties the hand-assembled violation
// path to ir.WalkValues' own grammar.
//
// checkNaming prunes at ir.Naming, so the walk never renders these paths and
// aliasPath spells them by hand. Were ir's slice-index rendering to change,
// every walk-produced path would move while these kept the old spelling, and
// nothing else would say so. The fixture runs past the single digits, beyond
// which a hand-built path and a formatted one can differ.
func TestVerify_AliasPathIsSpelledAsTheWalkWould(t *testing.T) {
	t.Parallel()
	const size = 12
	aliases := make([]string, size)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("alias.%d", i) // distinct, so no entry is a repeat
	}
	doc := modelNamed(ir.Naming{Source: "m", Canonical: "m", Aliases: aliases})

	walked := map[string]string{} // alias → the path the walk renders for it
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		path := at.String()
		if v.Kind() == reflect.String && v.String() != "" {
			walked[v.String()] = path
		}
		return true
	}))
	for _, alias := range aliases {
		require.Contains(t, walked, alias, "the walk reaches every entry when nothing prunes it")
	}

	// Blank every entry so the check reports one violation per index, then hold
	// each reported path to the one the walk rendered at that same index.
	got := aliasViolations(t, make([]string, size)...)
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
