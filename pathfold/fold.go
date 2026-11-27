// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pathfold

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Fold returns name folded as a case-insensitive, normalization-insensitive
// file system compares paths: canonical decomposition (NFD) of the whole name,
// simple upper-casing of each rune, then Unicode full case folding
// (docs/emitters/emit/rules.md).
//
// Any byte that is not valid UTF-8 is read as U+FFFD, joining rather than
// splitting invalid spellings; [Portable] refuses such segments.
func Fold(name string) string {
	// Build the caser per call: a cases.Caser holds transform state and is not
	// safe to share across goroutines.
	return cases.Fold().String(strings.Map(unicode.ToUpper, norm.NFD.String(name)))
}
