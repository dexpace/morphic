// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// combiningGraphemeJoiner is U+034F, which norm.NFD writes into a run of marks
// longer than Unicode's stream-safe form allows.
const combiningGraphemeJoiner = "\u034f"

// Fold returns name folded as a case-insensitive, normalization-insensitive
// file system compares paths: canonical decomposition (NFD) of the whole name,
// simple upper-casing of each rune, then Unicode full case folding
// (docs/emitters/emit/rules.md).
//
// The decomposition is whole: a run of marks norm.NFD's stream-safe form cuts
// is ordered across the cut.
//
// Any byte that is not valid UTF-8 is read as U+FFFD, joining rather than
// splitting invalid spellings; [Portable] refuses such segments.
func Fold(name string) string {
	// Build the caser per call: a cases.Caser holds transform state and is not
	// safe to share across goroutines.
	return cases.Fold().String(strings.Map(unicode.ToUpper, decompose(name)))
}

// decomposeStreamSafe returns name as norm.NFD decomposes it and whether that
// cut a run of marks: norm.NFD writes Unicode's stream-safe form, a U+034F
// after 30 marks in a row, and orders no mark across one.
func decomposeStreamSafe(name string) (decomposed string, cut bool) {
	decomposed = norm.NFD.String(name)
	written := strings.Count(decomposed, combiningGraphemeJoiner)
	return decomposed, written != strings.Count(name, combiningGraphemeJoiner)
}

// decompose returns name's canonical decomposition, whole: a name norm.NFD
// cuts is decomposed again with no cut, since APFS orders a run of 31 marks.
func decompose(name string) string {
	decomposed, cut := decomposeStreamSafe(name)
	if !cut {
		return decomposed
	}
	return decomposeUncut(name)
}

// decomposeUncut decomposes each character of name alone, which is too short
// for norm.NFD to cut, then puts each run of marks in canonical order: sorted
// by combining class, marks of one class keeping their order.
func decomposeUncut(name string) string {
	characters := make([]rune, 0, len(name))
	for _, character := range name {
		characters = append(characters, []rune(norm.NFD.String(string(character)))...)
	}
	for start := 0; start < len(characters); {
		end := start
		for end < len(characters) && combiningClass(characters[end]) != 0 {
			end++
		}
		slices.SortStableFunc(characters[start:end], func(left, right rune) int {
			return cmp.Compare(combiningClass(left), combiningClass(right))
		})
		start = end + 1
	}
	return string(characters)
}

// combiningClass returns character's canonical combining class, which is zero
// for a starter.
func combiningClass(character rune) uint8 {
	return norm.NFD.PropertiesString(string(character)).CCC()
}
