package ir_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

// adversarialRunes seeds the fuzzer with the spellings that make the grammar's
// hard cases reachable at all. A fuzzer exploring bytes is unlikely to construct
// a titlecase digraph or a letter with no lowercase form on its own, and the
// defect these properties were written for lived on exactly such a rune
// (GitHub #187).
//
// Each entry names why it is here, because a seed nobody can justify is a seed
// nobody will maintain:
//
//   - ℤ, ϒ: IsUpper reports true and ToLower returns them unchanged — a letter
//     with no lowercase form.
//   - ℤℤA: two of those before one that does lowercase. The seed beside it,
//     ℤℤa, was already here and passed; the input that broke idempotence was
//     the uppercase spelling one mutation away from it, because lowercasing the
//     A is what supplied the lowercase letter the tail rule looks for
//     (GitHub #336).
//   - ǅ: titlecase, which is neither IsUpper nor IsLower.
//   - ẞ: uppercase whose lowercase ß is a different letter.
//   - İ: uppercase whose lowercase is two runes, so lowercasing changes length.
//   - the decomposed é: one letter written as a letter and a combining mark.
//   - the scripts: a grammar can mishandle exactly one of them, so Latin alone
//     would not show it. Cherokee and Deseret are cased and rarely tested,
//     Greek and Cyrillic are the common non-Latin cased scripts, and Han and
//     Hebrew are cased by nothing at all.
//   - the rest: the boundaries the grammar splits on, and names with no words.
var adversarialRunes = []string{
	"Aℤ", "aℤ", "COUNTℤ", "ℤℤa", "ℤℤA", "aℤℤb",
	"Aϒ", "xǅy", "aẞb", "İstanbul", "ǅungla",
	"café_v2", "́x", "x́",
	"ᎠᎡ", "ꭰx", "𐐀𐐨", "Δε", "Жx", "中文", "אב",
	"userID", "HTTPServer", "APIKey2", "v2Beta", "foo2bar",
	"com.example.User", "get /pets/{petId}", "filter[name]",
	"", "***", "_", "__", "a_1", "1a", "  ", "--",
}

// FuzzCanonicalWords_Properties asserts what must hold of the grammar's answer
// for *any* input, as opposed to what its answer should be for a listed one.
//
// The distinction is the point. canonicalCases pins the answers — that userID
// becomes user_id is a specification decision and a table is the right shape for
// it. But a table only covers the rows someone thought to write, and these are
// not rows: they are claims about every input at once, and the defect in #187 sat
// in a spelling no row contained (GitHub #186).
//
// None of these can say the grammar is *right*; a property computed through the
// grammar moves with it. They say the grammar is self-consistent and that its
// output is something the rest of the IR will accept, which is a different
// question and the one nothing was asking.
//
// What the gate runs is the seed corpus: `go test` executes a fuzz target's seeds
// and does not search. So the standing coverage is exactly the spellings listed
// above plus the table's, and a class absent from both is unprotected until
// someone runs `-fuzz`. That is why the seeds are chosen adversarially rather
// than drawn from real specs — a grammar mishandling one script is invisible to a
// corpus that only contains Latin.
func FuzzCanonicalWords_Properties(f *testing.F) {
	for _, seed := range adversarialRunes {
		f.Add(seed)
	}
	for _, tc := range canonicalCases {
		f.Add(tc.in)
	}

	f.Fuzz(func(t *testing.T, name string) {
		got := ir.CanonicalWords(name)

		assertIdempotent(t, name, got)
		assertWordSequenceShape(t, name, got)
		assertWordRunesPreserved(t, name, got)
	})
}

// assertIdempotent pins the fixed point ir.Naming.Canonical depends on: a
// canonical fed back through the grammar is unchanged.
//
// Without it a name that crosses two stages drifts, and — since the grammar
// lowercases — the second pass sees a different input from the first, so the
// segmentation can depend on the casing of the source spelling rather than on
// its words. That is what #187 was.
func assertIdempotent(t *testing.T, name, got string) {
	t.Helper()
	if again := ir.CanonicalWords(got); again != got {
		t.Fatalf("not a fixed point\n  input:  %q\n  once:   %q\n  twice:  %q", name, got, again)
	}
}

// assertWordSequenceShape pins that the grammar's output is what
// ir.Naming.Canonical's doc comment promises, restated from the field rather
// than from the implementation: words of letters, digits and marks, joined by
// single underscores, carrying no casing and straddling no letter/digit
// boundary.
//
// This is the property that ties the producer to the consumer. irverify rejects
// a canonical failing any of these, so a grammar that could emit one would put
// the compiler and the verifier permanently at odds — which is the shape of the
// disagreement #187 turned out to be.
func assertWordSequenceShape(t *testing.T, name, got string) {
	t.Helper()
	if got == "" {
		return // a name with no word rune has no words to report
	}
	if lowered := strings.ToLower(got); lowered != got {
		t.Fatalf("output carries casing\n  input: %q\n  got:   %q\n  lower: %q", name, got, lowered)
	}
	for _, word := range strings.Split(got, "_") {
		if word == "" {
			t.Fatalf("empty word: a leading, trailing or doubled separator\n  input: %q\n  got: %q", name, got)
		}
		assertWordRunes(t, name, got, word)
	}
}

// assertWordRunes checks one word of the output: every rune belongs to a word,
// and no letter sits next to a digit — the boundary the grammar splits on, so
// no word it produces may straddle one.
func assertWordRunes(t *testing.T, name, got, word string) {
	t.Helper()
	var prev rune
	for i, r := range word {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			t.Fatalf("non-word rune %q in a word\n  input: %q\n  got: %q", r, name, got)
		}
		straddles := (unicode.IsLetter(prev) && unicode.IsDigit(r)) ||
			(unicode.IsDigit(prev) && unicode.IsLetter(r))
		if i > 0 && straddles {
			t.Fatalf("word %q straddles a letter/digit boundary\n  input: %q\n  got: %q", word, name, got)
		}
		prev = r
	}
}

// assertWordRunesPreserved pins that the grammar only inserts separators and
// drops case: the word runes of the output, in order, are the lowercased word
// runes of the input.
//
// It is the one property here that can see a word going missing or arriving in
// the wrong place — every shape check above is satisfied by an output that
// simply dropped half its input.
func assertWordRunesPreserved(t *testing.T, name, got string) {
	t.Helper()
	want := wordRunesOf(name)
	stripped := wordRunesOf(strings.ReplaceAll(got, "_", ""))
	if want != stripped {
		t.Fatalf("word runes not preserved\n  input: %q\n  got:   %q\n  want runes: %q\n  got runes:  %q",
			name, got, want, stripped)
	}
}

// wordRunesOf returns the lowercased word characters of s, in order, with
// everything the grammar treats as a separator removed.
func wordRunesOf(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
