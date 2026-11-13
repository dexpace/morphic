// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestIsDefaultIgnorable_WasSweptAgainstThisUnicode fails when a Go release
// moves unicode.Version, since IsDefaultIgnorable holds two ranges no table of
// package unicode carries, and Unicode has changed the derivation itself
// between releases, not only those ranges. Rebuild it from
// DerivedCoreProperties.txt's Default_Ignorable_Code_Point derivation and sweep
// every code point against it, then move the version here.
func TestIsDefaultIgnorable_WasSweptAgainstThisUnicode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "17.0.0", unicode.Version,
		"rebuild IsDefaultIgnorable from DerivedCoreProperties.txt's Default_Ignorable_Code_Point "+
			"derivation and sweep every code point against it, then move this version")
}

// TestIsDefaultIgnorable_HoldsEachTermAndExclusion witnesses each term of the
// derivation with a rune it alone decides, past the Basic Multilingual Plane
// too wherever the term reaches there, and each exclusion with a rune its term
// would otherwise hold, beside the bounds of the two ranges no table carries.
func TestIsDefaultIgnorable_HoldsEachTermAndExclusion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		why  string
		r    rune
		want bool
	}{
		{"Cf: a zero-width space", '\u200b', true},
		{"Cf: the soft hyphen", '\u00ad', true},
		{"Cf past the BMP, in no other term", '\U0001bca0', true},
		{"Other_Default_Ignorable_Code_Point: a Hangul filler, a letter", '\u3164', true},
		{"Other_Default_Ignorable_Code_Point: the grapheme joiner, a mark", '\u034f', true},
		{"Other_Default_Ignorable_Code_Point: unassigned", '\u2065', true},
		{"Other_Default_Ignorable_Code_Point directly below the annotations", '\ufff8', true},
		{"Other_Default_Ignorable_Code_Point past the BMP, in no other term", '\U000e0000', true},
		{"Variation_Selector, a mark", '\ufe0f', true},
		{"Variation_Selector: a Mongolian one", '\u180b', true},
		{"Variation_Selector past the BMP, in no other term", '\U000e0100', true},
		{"a prepended concatenation mark is Cf and renders", '\u0600', false},
		{"a prepended concatenation mark past the BMP", '\U000110bd', false},
		{"the first interlinear annotation character", '\ufff9', false},
		{"the last interlinear annotation character", '\ufffb', false},
		{"after the annotations, So", '\ufffc', false},
		{"the first Egyptian hieroglyph format control", '\U00013430', false},
		{"the last Egyptian hieroglyph format control", '\U0001343f', false},
		{"before the format controls, Lo", '\U0001342f', false},
		{"after the format controls, Mn", '\U00013440', false},
		{"a letter", 'a', false},
		{"a combining mark", '\u0301', false},
		{"white space", ' ', false},
		{"white space that renders a line", '\u1680', false},
		{"a control", '\x00', false},
		{"a pattern that renders as white space", '\u2800', false},
	} {
		t.Run(tc.why, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.IsDefaultIgnorable(tc.r), "%U", tc.r)
		})
	}
}

// TestIsWordRune_LeavesOutWhatRendersAsNothing holds the word rune to a letter,
// digit or mark that is not default-ignorable, each class witnessed in ASCII,
// beyond it and past the Basic Multilingual Plane.
func TestIsWordRune_LeavesOutWhatRendersAsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		r    rune
		want bool
	}{
		{'a', true}, {'Z', true}, {'7', true}, {'é', true}, {'\u0301', true}, {'\u0663', true},
		{'中', true}, {'\U00010400', true},
		{'_', false}, {'-', false}, {' ', false}, {'\u200b', false},
		{'\ufe0f', false}, {'\u180b', false}, {'\u034f', false}, {'\u17b4', false},
		{'\u115f', false}, {'\u3164', false}, {'\uffa0', false}, {'\U000e0100', false},
	} {
		t.Run(fmt.Sprintf("%U", tc.r), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.IsWordRune(tc.r))
		})
	}
}
