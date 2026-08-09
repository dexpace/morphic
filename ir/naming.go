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
	// source named with the empty string.
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
	case acronymTail(prev, r, runes, i):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}

// acronymTail reports whether runes[i] is the last capital of a run and opens
// the next word — the S of "HTTPServer" — which is two capitals followed by a
// lowercase letter.
//
// At least one of the two capitals must be one lowercasing changes. That is what
// keeps the rule from splitting its own output: a boundary between two runes
// lowercasing leaves alone survives into the result still looking like a
// boundary, so a second pass splits there again (GitHub #336). The lowercase
// letter the rule looks for need not have been lowercase in the source — "ℤℤA"
// has none, and lowercasing the A supplies one — so asking only about the source
// runes either side is not enough to know the pattern will not reappear.
//
// Requiring case of both would be too strong in either direction: it would lose
// ℤ_server, where only the S carries case, and http_ℤerver, where only the P
// does. Both are pinned in canonicalCases.
func acronymTail(prev, r rune, runes []rune, i int) bool {
	if !isCapital(prev) || !isCapital(r) {
		return false
	}
	if !carriesCase(prev) && !carriesCase(r) {
		return false // neither is lowercased, so the split would reappear
	}
	return i+1 < len(runes) && unicode.IsLower(runes[i+1])
}

// The two boundary rules above ask different questions about a rune, and the
// difference is what GitHub #187 turned on.
//
// carriesCase asks whether lowercasing *changes* r, which is what a case
// transition means and what irverify.isCased applies to a whole canonical. The
// two must agree: a rune the grammar splits on but lowercasing leaves alone
// survives into the output still looking like a boundary, so feeding that output
// back through the grammar splits it again. Double-struck ℤ, GREEK UPSILON WITH
// HOOK ϒ and the Roman numerals are IsUpper with no lowercase form of their own,
// and made the grammar disagree with itself on the second pass. It also takes in
// the titlecase letters, which IsUpper reports false for.
//
// isCapital asks whether r *belongs to a run of capitals*, which is a question
// about the letter's form rather than about a transition — the acronym-tail rule
// splits at the last capital of a run, and ℤ is one of those whether or not
// lowercasing would change it. Using carriesCase alone there would lose
// ℤ_server, and using IsUpper alone would lose the titlecase forms. Which is why
// the tail rule asks both: isCapital of each rune, and carriesCase of the pair.
//
// The grammar is a fixed point because no rule can fire on its own output. One
// pass lowercases every rune that carries case, so the two case rules — which
// each require one at the boundary — have nothing left to fire on, and the
// letter/digit rule is case-independent and has already been applied everywhere
// it applies.
func carriesCase(r rune) bool { return unicode.ToLower(r) != r }

// isCapital reports whether r is a capital letter form — uppercase or titlecase.
func isCapital(r rune) bool { return unicode.IsUpper(r) || unicode.IsTitle(r) }
