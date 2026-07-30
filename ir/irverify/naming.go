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

// appendNamingViolations reports both ways canon can break neutrality. They are
// checked separately because either can hold without the other — "userID" is
// segmented but cased, "com.example.user" is lowercase but unsegmented — and
// each names a different repair.
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
	return vs
}

// isWordSequence reports whether s is the shape Naming.Canonical promises: words
// joined by single underscores, each word made of letters, digits and the
// combining marks that belong to them. The empty string qualifies — a Naming may
// carry only a Hint, and a source name with no word rune in it has no words to
// report.
//
// What this cannot check is where a compiler puts the boundaries *inside* a
// word: "foo2bar" and "foo_2_bar" are both word sequences, so two compilers
// splitting letter/digit runs differently would both pass here. Only one shared
// implementation of the grammar settles that (GitHub #163); this check holds the
// line that ships today, which is that a canonical is words at all.
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
