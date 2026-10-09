package ir

import (
	"strings"
	"unicode"
)

// Naming carries the identity of a named entity as words, never as a cased
// identifier: emitters apply casing, acronym policy, and reserved-word escaping
// (ir-design §3.2). Anonymous (hoisted) types have an empty Source and a Hint.
//
// At least one of Source, Canonical and Hint is set. A Naming with all three
// empty names the entity to nobody, and every neutrality rule is vacuously true
// of it, so irverify reports it as ir/naming-absent.
type Naming struct {
	// Source is the name exactly as written in the spec ("user_id", a $ref
	// name, a GraphQL field).
	Source string `json:"source,omitempty"`
	// Canonical is the IR-normalized identifier in neutral form: lower_snake
	// words with no casing opinions. A word is letters and digits (plus the
	// combining marks belonging to them); every other character in the source
	// name separates two words rather than surviving into the sequence.
	Canonical string `json:"canonical,omitempty"`
	// Hint is a context-derived suggestion for an entity with no source name to
	// render: a hoisted anonymous type (e.g. "connection_domain"), or one the
	// source named with the empty string. It is in the same neutral form as
	// Canonical and for the same reason — it is the only name such an entity
	// has, so it is what an emitter renders its identifier from — however the
	// position it was derived from was spelled.
	Hint string `json:"hint,omitempty"`
	// Aliases are alternate names for schema-resolution matching (Avro
	// aliases). Versionless: rename history tied to version labels lives in
	// Availability.RenamedFrom.
	//
	// An alias is verbatim like Source, not neutral like Canonical: it is
	// matched against a name another schema wrote, so casing and punctuation
	// are the value ("com.example.User"). irverify holds an entry only to what
	// needs no grammar: something visible, valid UTF-8, no repeat of another
	// entry or of Source, and no claim by another type (ir-design §3.2).
	//
	// A source's blank or repeated entry is recorded once in a Diagnostic and
	// dropped, which loses no name, so invariant #2 does not forbid it.
	Aliases []string `json:"aliases,omitempty"`
}

// CanonicalWords renders name as the neutral lower_snake word sequence
// ir.Naming.Canonical promises: it splits on every non-word rune and on
// camel-case and letter/digit boundaries, lowercases, and joins with "_"
// (ir-design §3.2). Acronym and casing policy are the emitter's.
//
// The grammar lives here, not in the compiler framework, because Canonical is
// ABI. A Document from outside this repository's compilers is held by irverify
// alone, and as a Layer 0 package it can recompute a canonical from the source
// beside it because the grammar is here (GitHub #163). A name with no word rune
// ("***") canonicalizes to the empty string; Naming.Source keeps the spelling.
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
// Letters and digits are word characters, and a combining mark belongs to the
// letter it follows: a decomposed "é" is one letter written as two runes, so
// reading the mark as a separator would split a word in half.
//
// Everything else separates, so the result is a word sequence whatever the
// source spelled the boundary as: a dot, a slash, brackets, _, - or a space.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case carriesCase(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case acronymTail(prev, r, runes, i):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}

// acronymTail reports whether runes[i] ends a run of capitals and opens the
// next word, the S of "HTTPServer": two capitals followed by a lowercase
// letter.
//
// At least one capital must be one lowercasing changes, or the rule splits its
// own output: a boundary between runes lowercasing leaves alone survives into
// the result, and a second pass splits there again (GitHub #336). The lowercase
// letter after the pair need not be lowercase in the source — "ℤℤA" has none
// until lowercasing supplies one — so the source runes cannot settle it.
// Requiring case of both would lose ℤ_server and http_ℤerver; canonicalCases
// pins both.
func acronymTail(prev, r rune, runes []rune, i int) bool {
	if !isCapital(prev) || !isCapital(r) {
		return false
	}
	if !carriesCase(prev) && !carriesCase(r) {
		return false // neither is lowercased, so the split would reappear
	}
	return i+1 < len(runes) && unicode.IsLower(runes[i+1])
}

// carriesCase reports whether lowercasing changes r, as irverify.isCased asks
// of a whole canonical; the two must agree (GitHub #187). A rune the grammar
// splits on but lowercasing leaves alone survives into the output still looking
// like a boundary, and re-canonicalizing would split it again. Double-struck ℤ
// and ϒ are IsUpper with no lowercase form; titlecase letters are not IsUpper
// but do change.
//
// That makes the grammar a fixed point: one pass lowercases every rune that
// carries case, leaving the two case rules nothing to fire on, and the
// letter/digit rule is case-independent.
func carriesCase(r rune) bool { return unicode.ToLower(r) != r }

// isCapital reports whether r is a capital letter form — uppercase or
// titlecase. It asks about the letter's form, not a transition: ℤ belongs to a
// run of capitals whether or not lowercasing changes it, and so does a
// titlecase letter, though IsUpper reports false. acronymTail pairs it with
// carriesCase.
func isCapital(r rune) bool { return unicode.IsUpper(r) || unicode.IsTitle(r) }
