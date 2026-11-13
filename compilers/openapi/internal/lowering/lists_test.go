// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestFirstOccurrences_KeepsEachEntryOnceAtItsFirst pins the fold: every entry
// at its first occurrence, in the order written, and every repeat with the
// index it repeats. Entries compare as written, so another case or a blank is
// another entry, and an entry repeated twice is reported twice.
func TestFirstOccurrences_KeepsEachEntryOnceAtItsFirst(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		entries []string
		kept    []string
		repeats []lowering.Repeat
	}{
		{"none", nil, nil, nil},
		{"no repeat", []string{"a", "A", "", " "}, []string{"a", "A", "", " "}, nil},
		{"repeats", []string{"a", "a", "", "b", "a", ""},
			[]string{"a", "", "b"}, []lowering.Repeat{{At: 1, First: 0}, {At: 4, First: 0}, {At: 5, First: 2}}},
		{"all one", []string{"x", "x", "x"}, []string{"x"}, []lowering.Repeat{{At: 1, First: 0}, {At: 2, First: 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kept, repeats := lowering.FirstOccurrences(tc.entries)
			assert.Equal(t, tc.kept, kept)
			assert.Equal(t, tc.repeats, repeats)
		})
	}
}

// TestFirstOccurrences_ALongListFoldsAsAShortOne pins the reading of a list
// past the length compared pair by pair, read through a set, to the same fold.
func TestFirstOccurrences_ALongListFoldsAsAShortOne(t *testing.T) {
	t.Parallel()
	entries := make([]string, 0, 23)
	for i := range 20 {
		entries = append(entries, "s"+strconv.Itoa(i))
	}
	want := slices.Clone(entries)
	entries = append(entries, "s3", "s0", "s3")
	kept, repeats := lowering.FirstOccurrences(entries)
	assert.Equal(t, want, kept)
	assert.Equal(t, []lowering.Repeat{{At: 20, First: 3}, {At: 21, First: 0}, {At: 22, First: 3}}, repeats)
}

// TestFirstOccurrences_ShortListRepeatingNothingCostsNothing pins the common
// case: a list of tags or scopes, written once each, is handed back as it is
// with nothing allocated. Not parallel: AllocsPerRun measures the process.
func TestFirstOccurrences_ShortListRepeatingNothingCostsNothing(t *testing.T) {
	entries := []string{"read", "write", "admin", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}
	var kept []string
	allocs := testing.AllocsPerRun(100, func() { kept, _ = lowering.FirstOccurrences(entries) })
	assert.Zero(t, allocs)
	assert.Same(t, &entries[0], &kept[0], "the list itself")
}
