// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

// spelled is the party of a name the source spells as its words.
func spelled(words string) party { return party{words: words, source: words} }

// TestNameReader_PlacesANameByItselfWhereOnlyItsHashAgrees places names under
// one hash, which no two different names share in a run, so the sets that
// only a hash collision makes are held here rather than through Names.
func TestNameReader_PlacesANameByItselfWhereOnlyItsHashAgrees(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                  string
		by                    comparison
		parties               []party
		wantLater, wantShared []int
	}{
		{"words as they are", byWords,
			[]party{spelled("pet"), spelled("set_up"), spelled("setup"), spelled("pet"), spelled("set_up")},
			[]int{3, 4, -1, -1, -1}, []int{0, 1}},
		{"words joined", byJoinedWords,
			[]party{spelled("pet"), spelled("set_up"), spelled("setup"), spelled("pet"), spelled("pets")},
			[]int{3, 2, -1, -1, -1}, []int{1, 0}},
		{"a spelling with no word character, as written and apart from one a hint gives words", byJoinedWords,
			[]party{{source: "_-"}, {source: "-"}, {words: "minus", source: "-"}, {source: "-"}},
			[]int{-1, 3, -1, -1}, []int{1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := len(tt.parties)
			r := nameReader{parties: tt.parties, later: make([]int, n), last: make([]int, n), other: make([]int, n)}
			seen := map[uint64]int{}
			for i := range tt.parties {
				r.later[i] = -1
				r.place(tt.by, seen, 7, i)
			}

			assert.Empty(t, cmp.Diff(tt.wantLater, r.later), "each party's next party named alike")
			assert.Empty(t, cmp.Diff(tt.wantShared, r.shared), "each set's first party, by its second")
		})
	}
}
