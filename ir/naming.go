package ir

import (
	"iter"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// entry or of Source (ir-design §3.2).
	//
	// A source declaring a blank or repeated entry is recorded once in a
	// Diagnostic, not carried through. Dropping it loses no name, so it is not
	// the lossy flattening invariant #2 forbids.
	Aliases []string `json:"aliases,omitempty"`
}

// CanonicalWords renders name as the neutral lower_snake word sequence
// ir.Naming.Canonical promises: it splits on every non-word rune and on
// camel-case and letter/digit boundaries, lowercases, and joins with "_"
// (ir-design §3.2). Acronym and casing policy are the emitter's. A name with no
// word rune ("***") canonicalizes to "", and a canonical name is returned as it
// is, allocating nothing.
//
// The grammar lives here, not in the compiler framework, because Canonical is
// ABI. A Document from outside this repository's compilers is held by irverify
// alone, and as a Layer 0 package it can recompute a canonical from the source
// beside it because the grammar is here (GitHub #163).
func CanonicalWords(name string) string {
	if len(name) > stackRendering/2 {
		return canonicalWordsExactly(name)
	}
	// Rendered on the stack, so the string is the one allocation: a name
	// renders in at most twice its length, as
	// TestCanonicalWords_RendersInAtMostTwiceItsLength holds of every rune.
	var buf [stackRendering]byte
	words := buf[:0]
	for r := range canonicalRunes(name) {
		words = utf8.AppendRune(words, r)
	}
	if string(words) == name {
		return name
	}
	return string(words)
}

// stackRendering is how many bytes CanonicalWords renders a name into on the
// stack. Names in the largest documents compiled so far stay under half of
// it, so every one of them renders there.
const stackRendering = 256

// canonicalWordsExactly is CanonicalWords for a name whose canonical form could
// outgrow the stack. It reads name once to see whether it is canonical already
// and once to size the result, so the result is still the one allocation.
func canonicalWordsExactly(name string) string {
	if HasCanonicalWords(name, name) {
		return name
	}
	size := 0
	for r := range canonicalRunes(name) {
		size += utf8.RuneLen(r)
	}
	var b strings.Builder
	b.Grow(size)
	for r := range canonicalRunes(name) {
		b.WriteRune(r)
	}
	return b.String()
}

// HasCanonicalWords reports whether words is CanonicalWords(name). It compares
// as it reads name, building no canonical form, so it allocates nothing:
// irverify asks it of every Naming that carries a source.
func HasCanonicalWords(name, words string) bool {
	rest := words
	for r := range canonicalRunes(name) {
		// An empty or undecodable rest reads as RuneError, which no canonical
		// rune is: it is no letter, digit or mark, nor any one's lowercase.
		got, size := utf8.DecodeRuneInString(rest)
		if got != r {
			return false
		}
		rest = rest[size:]
	}
	return rest == ""
}

// canonicalRunes yields the runes of CanonicalWords(name) in order: each word
// rune lowercased, and "_" between two words. Lowercasing rune by rune is what
// strings.ToLower does to a valid string, and a word holds only runes that
// decoded. It is the one rendering of the grammar's words, which everything
// above reads.
func canonicalRunes(name string) iter.Seq[rune] {
	return func(yield func(rune) bool) {
		first := true
		for word := range sourceWords(name) {
			if !first && !yield('_') {
				return
			}
			first = false
			for _, r := range word {
				if !yield(unicode.ToLower(r)) {
					return
				}
			}
		}
	}
}

// sourceWords yields the words the grammar reads in name, each as name spells
// it: a run of word runes ended by a non-word rune, by a camel-case or
// letter/digit boundary, or by the end of name. A word is a substring of name,
// so reading one allocates nothing.
//
// It is the grammar's one reading of a name, which canonicalRunes renders for
// CanonicalWords and HasCanonicalWords alike, so the two cannot segment a name
// differently.
func sourceWords(name string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start, inWord := 0, false
		var prev rune
		for i := 0; i < len(name); {
			// A byte that does not decode reads as RuneError, which is no word
			// rune, so it separates.
			r, size := utf8.DecodeRuneInString(name[i:])
			switch {
			case !isWordRune(r):
				if inWord && !yield(name[start:i]) {
					return
				}
				inWord = false
			case !inWord:
				start, inWord = i, true
			case wordBoundary(prev, r, name[i+size:]):
				if !yield(name[start:i]) {
					return
				}
				start = i
			}
			prev = r
			i += size
		}
		if inWord {
			yield(name[start:])
		}
	}
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

// wordBoundary reports whether a new word starts at word rune r, which follows
// word rune prev and comes before rest, the remainder of the name.
func wordBoundary(prev, r rune, rest string) bool {
	switch {
	case carriesCase(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case acronymTail(prev, r, rest):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}

// acronymTail reports whether r ends a run of capitals and opens the next word,
// the S of "HTTPServer": two capitals followed by a lowercase letter.
//
// At least one capital must be one lowercasing changes, or the rule splits its
// own output: a boundary between runes lowercasing leaves alone survives into
// the result, and a second pass splits there again (GitHub #336). The lowercase
// letter after the pair need not be lowercase in the source — "ℤℤA" has none
// until lowercasing supplies one — so the source runes cannot settle it.
// Requiring case of both would lose ℤ_server and http_ℤerver; canonicalCases
// pins both.
func acronymTail(prev, r rune, rest string) bool {
	if !isCapital(prev) || !isCapital(r) {
		return false
	}
	if !carriesCase(prev) && !carriesCase(r) {
		return false // neither is lowercased, so the split would reappear
	}
	next, _ := utf8.DecodeRuneInString(rest) // RuneError, no lowercase letter, at the end
	return unicode.IsLower(next)
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
