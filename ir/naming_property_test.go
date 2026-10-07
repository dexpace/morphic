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
// defect these properties were written for lived on exactly such a rune (#187).
// Each entry's reason sits beside it, because a seed nobody can justify is a
// seed nobody will maintain.
var adversarialRunes = []string{
	// ℤ: IsUpper reports true and ToLower returns it unchanged, a letter with
	// no lowercase form. ℤℤA is two of those before one that does lowercase.
	// ℤℤa passed, but the uppercase spelling one mutation away broke
	// idempotence: lowercasing the A supplied the lowercase letter the tail
	// rule looks for (#336).
	"Aℤ", "aℤ", "COUNTℤ", "ℤℤa", "ℤℤA", "aℤℤb",
	// ϒ is another letter with no lowercase form. ǅ is titlecase, neither
	// IsUpper nor IsLower. ẞ is uppercase and its lowercase ß is a different
	// letter. İ is uppercase and lowercases to two runes, so lowercasing
	// changes length.
	"Aϒ", "xǅy", "aẞb", "İstanbul", "ǅungla",
	// A decomposed é: one letter written as a letter and a combining mark.
	"café_v2", "́x", "x́",
	// The scripts: a grammar can mishandle exactly one of them, so Latin alone
	// would not show it. Cherokee and Deseret are cased and rarely tested,
	// Greek and Cyrillic are the common non-Latin cased scripts, and Han and
	// Hebrew are cased by nothing at all.
	"ᎠᎡ", "ꭰx", "𐐀𐐨", "Δε", "Жx", "中文", "אב",
	// The boundaries the grammar splits on, and names with no words.
	"userID", "HTTPServer", "APIKey2", "v2Beta", "foo2bar",
	"com.example.User", "get /pets/{petId}", "filter[name]",
	"", "***", "_", "__", "a_1", "1a", "  ", "--",
}

// FuzzCanonicalWords_Properties asserts what must hold of the grammar's answer
// for any input, not only a listed one: canonicalCases pins the answers, but a
// table covers only the rows someone wrote, and the defect in #187 sat in a
// spelling no row contained (#186).
//
// None of these says the grammar is right, since a property computed through it
// moves with it. They say its output is self-consistent and acceptable to the
// rest of the IR.
//
// The seeds carry most of the weight: an ordinary `go test` runs them without
// searching, and the gate's per-target search is bounded to seconds
// (scripts/fuzz.sh).
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
	for word := range strings.SplitSeq(got, "_") {
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
