package ir

import (
	"strings"
	"unicode"
)

// Naming carries the identity of a named entity as words, never as a cased
// identifier: emitters apply casing, acronym policy, and reserved-word escaping
// (ir-design §3.2). Anonymous (hoisted) types have an empty Source and a Hint.
type Naming struct {
	// Source is the name exactly as written in the spec ("user_id", a $ref
	// name, a GraphQL field).
	Source string `json:"source,omitempty"`
	// Canonical is the IR-normalized identifier in neutral form: lower_snake
	// words with no casing opinions. A word is letters and digits (plus the
	// combining marks belonging to them); every other character in the source
	// name separates two words rather than surviving into the sequence.
	Canonical string `json:"canonical,omitempty"`
	// Hint is a context-derived suggestion for anonymous types only
	// (e.g. "connection_domain").
	Hint string `json:"hint,omitempty"`
	// Aliases are alternate names for schema-resolution matching (Avro
	// aliases). Versionless — rename history tied to version labels lives in
	// Availability.RenamedFrom.
	Aliases []string `json:"aliases,omitempty"`
}

// CanonicalWords renders name as the neutral lower_snake word sequence
// ir.Naming.Canonical promises: it splits on every non-word rune and on
// camel-case and letter/digit boundaries, lowercases, and joins with "_". It
// holds no acronym opinion beyond boundary detection; casing policy is an
// emitter concern.
//
// It lives beside the field it fills rather than in the compiler framework
// because Canonical is ABI and the field's own doc comment above already states
// this grammar in prose. Three copies of it disagreed about exactly that
// (GitHub #163), and while an architecture test now stops a fourth being
// written, that rule reaches only this repository's own compilers. A Document
// arriving any other way — decoded from JSON, produced by a compiler outside
// this tree, rewritten by a pass — is held by irverify alone, and irverify is
// Layer 0: with the grammar here it can recompute a canonical from the source
// beside it and see a segmentation that lowercasing had erased the evidence of.
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
	case carriesCase(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case carriesCase(prev) && carriesCase(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}

// carriesCase reports whether r is a cased form that lowercasing will change —
// the rune the boundary rules above mean by "upper".
//
// It asks whether lowercasing changes the rune rather than unicode.IsUpper,
// which is the same test irverify.isCased applies to a whole canonical, and the
// two must agree: a rune the grammar splits on but lowercasing leaves alone
// survives into the output still looking like a boundary, so feeding that output
// back through the grammar splits it again. Double-struck ℤ, GREEK UPSILON WITH
// HOOK ϒ and the Roman numerals are IsUpper with no lowercase form of their own,
// and made the grammar disagree with itself on the second pass (GitHub #187).
//
// It also takes in the titlecase letters, which are a case boundary — ǅ is the
// form that opens a word — and which IsUpper reports false for.
func carriesCase(r rune) bool { return unicode.ToLower(r) != r }
