// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"slices"
	"strings"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

// The bounds of the edit-distance reading of a near miss, past which it is
// skipped: a hint is a courtesy, and the two readings by lookup still answer.
const (
	// maxNearNames is the most declared names compared with a key one by one.
	maxNearNames = 64
	// maxNearKeyBytes is the longest key compared with them.
	maxNearKeyBytes = 64
	// minEditKeyBytes is the shortest: one edit apart, short names such as
	// jwt and jwk would name each other more often than a typo would.
	minEditKeyBytes = 4
)

// nearIndex answers which declared scheme a name no entry declares is a near
// miss of, so its error can name it. It is built once per registry, from the
// names of the schemes that reach a mechanism, and never written after.
type nearIndex struct {
	// canonical maps a neutral name (ir.CanonicalWords) to the least declared
	// name that has it, and folded a name's letters and digits, case folded.
	canonical map[string]string
	folded    map[string]string
	// names are the declared names in byte order, nil past maxNearNames.
	names []string
}

// nearIndexOf returns the index over names, the schemes that reach a mechanism.
func nearIndexOf(names []string) nearIndex {
	sorted := slices.Sorted(slices.Values(names))
	ix := nearIndex{canonical: make(map[string]string, len(sorted)), folded: make(map[string]string, len(sorted))}
	for _, n := range sorted {
		addLeast(ix.canonical, ir.CanonicalWords(n), n)
		addLeast(ix.folded, foldName(n), n)
	}
	if len(sorted) <= maxNearNames {
		ix.names = sorted
	}
	return ix
}

// addLeast holds name under key unless a name came first: names arrive in byte
// order, so the one held is the least. An empty key, a name with no letter or
// digit, matches nothing.
func addLeast(index map[string]string, key, name string) {
	if _, held := index[key]; key != "" && !held {
		index[key] = name
	}
}

// nearest returns the declared name key is a near miss of, and whether there is
// one. key is a name no entry declares. The first reading that matches decides,
// in this order: the same neutral name, as another case or other separators
// spell it; the same letters and digits in any case; then, for a key of
// minEditKeyBytes to maxNearKeyBytes, a name one edit apart (oneEditApart).
// Within a reading the least name in byte order is named, so the answer is a
// function of key and the declared names.
func (ix nearIndex) nearest(key string) (string, bool) {
	if n, ok := ix.canonical[ir.CanonicalWords(key)]; ok && n != key {
		return n, true
	}
	if n, ok := ix.folded[foldName(key)]; ok && n != key {
		return n, true
	}
	if len(key) < minEditKeyBytes || len(key) > maxNearKeyBytes {
		return "", false
	}
	for _, n := range ix.names {
		if oneEditApart(key, n) {
			return n, true
		}
	}
	return "", false
}

// foldName returns name's letters and digits, case folded, and nothing else.
func foldName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}

// oneEditApart reports whether a and b differ by one edit: a rune inserted,
// deleted or substituted, or two adjacent runes swapped. Equal strings are no
// edit apart. It reads each once, comparing runes.
func oneEditApart(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) < len(y) {
		x, y = y, x
	}
	i := commonPrefix(x, y)
	switch len(x) - len(y) {
	case 0:
		if i == len(x) {
			return false
		}
		swapped := i+1 < len(x) && x[i] == y[i+1] && x[i+1] == y[i] && slices.Equal(x[i+2:], y[i+2:])
		return swapped || slices.Equal(x[i+1:], y[i+1:])
	case 1:
		return slices.Equal(x[i+1:], y[i:])
	default:
		return false
	}
}

// commonPrefix returns how many leading runes x and y share.
func commonPrefix(x, y []rune) int {
	n := min(len(x), len(y))
	for i := range n {
		if x[i] != y[i] {
			return i
		}
	}
	return n
}
