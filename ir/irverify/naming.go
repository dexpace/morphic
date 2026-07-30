package irverify

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

var namingType = reflect.TypeFor[ir.Naming]()

// checkNaming asserts every Naming.Canonical is what invariant #4 promises: a
// neutral lower_snake word sequence, carrying no casing an emitter should own
// and no character that is not part of a word. It reuses walkValues to reach
// every ir.Naming value in the document.
//
// Only Canonical is checked. Naming.Hint — the generated-name channel for
// anonymous types — is held to none of these rules, so casing and punctuation
// still reach the IR through it. That is GitHub #54, left open deliberately:
// closing it means changing how the compilers derive hints and regenerating
// every golden, which is a different change from tightening this checker.
func checkNaming(doc *ir.Document) []Violation {
	var vs []Violation
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != namingType {
			return true
		}
		vs = appendNamingViolations(vs, v.FieldByName("Canonical").String(), path)
		return false // Naming holds no references or nested Naming to descend into
	})
	return vs
}

// appendNamingViolations reports the ways canon can break neutrality. They are
// checked separately because each can hold without the others — "userID" is
// segmented but cased, "com.example.user" is lowercase but unsegmented,
// "foo2bar" is both lowercase and made of word characters yet runs two words
// together — and each names a different repair.
func appendNamingViolations(vs []Violation, canon, path string) []Violation {
	if isCased(canon) {
		vs = append(vs, Violation{
			Code:    "ir/naming-cased",
			Message: "canonical name " + canon + " carries casing; store neutral words",
			Path:    path,
		})
	}
	if !isWordSequence(canon) {
		vs = append(vs, Violation{
			Code:    "ir/naming-not-words",
			Message: "canonical name " + canon + " is not a word sequence; split it on every non-word character",
			Path:    path,
		})
	}
	if !isSegmented(canon) {
		vs = append(vs, Violation{
			Code: "ir/naming-unsegmented",
			Message: "canonical name " + canon +
				" runs a letter and a digit together in one word; the grammar splits that boundary",
			Path: path,
		})
	}
	return vs
}

// isSegmented reports whether canon puts a boundary everywhere the canonical
// grammar requires one *inside* a run of word characters. Only the letter/digit
// boundary is decidable here: the grammar splits "APIKey2" into api|key|2, so no
// word it produces holds a letter next to a digit.
//
// This is the half of segmentation a neutral name still carries evidence of.
//
// The other half — the camel-case boundary that turns "userID" into user|id — is
// unverifiable from Canonical alone, because lowercasing has already erased the
// case change that marked it: a compiler that neutralized "userID" to "userid"
// without splitting produces one all-letter word, indistinguishable from a
// genuine single word. Catching that means reading Naming.Source through the
// grammar itself, which lives in compilers/compile and is out of reach from
// Layer 0. It is unchecked here and stays that way until either the grammar
// moves into ir or a second compiler makes a disagreement possible: today
// compile.NamingFor is the only constructor that sets Canonical, so it pairs the
// two by construction (GitHub #164).
func isSegmented(s string) bool {
	for _, word := range strings.Split(s, "_") {
		var prev rune
		for i, r := range word {
			if i > 0 && straddlesLetterDigit(prev, r) {
				return false
			}
			prev = r
		}
	}
	return true
}

// straddlesLetterDigit reports whether prev and r sit either side of a boundary
// the grammar splits on. A combining mark is transparent: it belongs to the
// letter it follows rather than starting a run of its own.
func straddlesLetterDigit(prev, r rune) bool {
	return (unicode.IsLetter(prev) && unicode.IsDigit(r)) ||
		(unicode.IsDigit(prev) && unicode.IsLetter(r))
}

// isWordSequence reports whether s is the shape Naming.Canonical promises: words
// joined by single underscores, each word made of letters, digits and the
// combining marks that belong to them. The empty string qualifies — a Naming may
// carry only a Hint, and a source name with no word rune in it has no words to
// report.
//
// It says nothing about where the boundaries fall inside a run of word
// characters: "foo2bar" and "foo_2_bar" are both word sequences by this test.
// isSegmented is what holds that line.
func isWordSequence(s string) bool {
	if s == "" {
		return true
	}
	for _, word := range strings.Split(s, "_") {
		if word == "" {
			return false // a leading, trailing, or doubled separator
		}
		for _, r := range word {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
				return false
			}
		}
	}
	return true
}

// isCased reports whether s still carries casing an emitter should own. The test
// is lowercase-idempotence, not unicode.IsUpper: a compiler neutralizes names
// with strings.ToLower, so a rune that has no lowercase form (double-struck ℤ,
// Mathematical Bold 𝐀, a Roman numeral) is already neutral even though IsUpper
// reports true for it.
func isCased(s string) bool {
	return strings.ToLower(s) != s
}
