package compile

import (
	"strings"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

// NamingFor builds the neutral Naming of a name the source declares: the
// spelling it used, plus the canonical word sequence derived from it.
//
// It is the constructor to reach for wherever a source name becomes an IR name,
// because the pairing is the invariant — a Source with no Canonical leaves an
// emitter to segment the spelling itself, which is the casing decision invariant
// 4 moves out of the compilers. A Naming that carries only a Hint is a different
// case and does not come through here: nothing declared it.
func NamingFor(source string) ir.Naming {
	return ir.Naming{Source: source, Canonical: CanonicalWords(source)}
}

// CanonicalWords renders name as the neutral lower_snake word sequence
// ir.Naming.Canonical promises: it splits on every non-word rune and on
// camel-case and letter/digit boundaries, lowercases, and joins with "_". It
// holds no acronym opinion beyond boundary detection; casing policy is an
// emitter concern.
//
// The framework owns this rather than each compiler because Canonical is ABI.
// Invariant 4 makes neutral naming a property of the IR, so "example.v1" cannot
// canonicalize to example_v1 from one compiler and example.v1 from another: an
// emitter reading Canonical has no way to tell which grammar produced it. Three
// copies of this function disagreed on exactly that (GitHub #163).
//
// A name written with no word rune in it at all ("***") canonicalizes to the
// empty string. Naming.Source keeps the spelling either way, so nothing is lost
// — there is simply no word sequence to report, and inventing one from the
// punctuation would be a naming opinion the IR does not hold.
func CanonicalWords(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		if !isWordRune(r) {
			flush()
			continue
		}
		if len(cur) > 0 && wordBoundary(cur[len(cur)-1], r, runes, i) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return strings.Join(words, "_")
}

// isWordRune reports whether r belongs to a word rather than separating two.
// Letters and digits are the word characters, and a combining mark is part of
// the letter it follows — a decomposed "é" is one letter written as two runes,
// so reading the mark as a separator would split a word in half.
//
// Everything else separates, which is what makes the result a word sequence
// whatever the source spelled the boundary as: a dot in a namespaced component
// name or a proto package, a slash in a media type or a path template, brackets
// around a query parameter, and the _/-/space a name may already use.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}
