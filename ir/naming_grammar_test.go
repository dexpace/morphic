// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/ircases"
	"github.com/dexpace/morphic/ir"
)

func TestCanonicalWords_Conformance(t *testing.T) {
	t.Parallel()
	for _, tc := range ircases.Canonical() {
		t.Run(tc.Rule, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.Want, ir.CanonicalWords(tc.In), "input %q", tc.In)
		})
	}
}

// TestCanonicalWords_IsIdempotent asserts the grammar is a fixed point: feeding
// a canonical back in returns it unchanged. Without that, a name that crossed
// two stages would drift, and it is the property irverify's segmentation check
// relies on to be decidable from the value alone.
func TestCanonicalWords_IsIdempotent(t *testing.T) {
	t.Parallel()
	for _, tc := range ircases.Canonical() {
		t.Run(tc.Rule, func(t *testing.T) {
			t.Parallel()
			once := ir.CanonicalWords(tc.In)
			assert.Equal(t, once, ir.CanonicalWords(once), "re-canonicalizing %q", once)
		})
	}
}

func TestHasCanonicalWords_ComparesAsTheGrammarRenders(t *testing.T) {
	t.Parallel()
	for _, tc := range ircases.HasCanonical() {
		t.Run(tc.Rule, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.Want, ir.HasCanonicalWords(tc.Name, tc.Words), "%q against %q", tc.Name, tc.Words)
			assert.Equal(t, tc.Want, ir.CanonicalWords(tc.Name) == tc.Words, "the row disagrees with the grammar")
		})
	}
}

// TestHasCanonicalWords_Conformance holds the comparison to every conformance
// row: it accepts the row's canonical form and refuses each spelling one edit
// from it.
func TestHasCanonicalWords_Conformance(t *testing.T) {
	t.Parallel()
	for _, tc := range ircases.Canonical() {
		t.Run(tc.Rule, func(t *testing.T) {
			t.Parallel()
			assert.True(t, ir.HasCanonicalWords(tc.In, tc.Want), "input %q", tc.In)
			for _, miss := range ircases.NearMisses(tc.Want) {
				assert.False(t, ir.HasCanonicalWords(tc.In, miss), "input %q against %q", tc.In, miss)
			}
		})
	}
}

// TestCanonicalWords_RendersInAtMostTwiceItsLength holds, for every rune, the
// bound CanonicalWords sizes its stack rendering by: a rune renders lowercased,
// after at most one separator, in at most twice the bytes it is written in. A
// Unicode version giving some rune a longer lowercase would spill the rendering
// of a name CanonicalWords takes to fit onto the heap.
func TestCanonicalWords_RendersInAtMostTwiceItsLength(t *testing.T) {
	t.Parallel()
	for r := range rune(unicode.MaxRune + 1) {
		if !utf8.ValidRune(r) {
			continue
		}
		if rendered := 1 + utf8.RuneLen(unicode.ToLower(r)); rendered > 2*utf8.RuneLen(r) {
			t.Errorf("%U renders in %d bytes, past twice its own %d", r, rendered, utf8.RuneLen(r))
		}
	}
}

// TestCanonicalWords_AllocatesOnlyItsResult holds the grammar to the cost its
// doc promises, at every length: a canonical name comes back as it is, any
// other as one new string, on the stack rendering and past it, and
// HasCanonicalWords builds nothing. "aB" a hundred times fits the stack's
// buffer but renders too long for it. irverify compares every Naming that
// carries a source, and compilers derive every name. It is not run in
// parallel: AllocsPerRun measures the process.
func TestCanonicalWords_AllocatesOnlyItsResult(t *testing.T) {
	short, long := strings.Repeat("ab", 64), strings.Repeat("user_name_", 30)+"x"
	for _, tc := range []struct {
		name   string
		allocs float64
	}{
		{"user_name", 0},
		{"userName", 1},
		{"***", 0},
		{short, 0},
		{short + "C", 1},
		{strings.Repeat("aB", 100), 1},
		{long, 0},
		{strings.ToUpper(long), 1},
		{strings.Repeat("*", 200), 0},
	} {
		words := ir.CanonicalWords(tc.name)
		var got string
		assert.Equal(t, tc.allocs, testing.AllocsPerRun(10, func() { got = ir.CanonicalWords(tc.name) }),
			"CanonicalWords(%q)", tc.name)
		assert.Equal(t, words, got)
		assert.Zero(t, testing.AllocsPerRun(10, func() { _ = ir.HasCanonicalWords(tc.name, words) }),
			"HasCanonicalWords(%q)", tc.name)
	}
}
