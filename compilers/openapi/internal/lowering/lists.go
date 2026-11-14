// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "slices"

// Repeat is an entry of a list written again: the entry at At repeats the one
// at First, its first occurrence.
type Repeat struct{ At, First int }

// pairwiseEntries is the length up to which a list's entries are compared pair
// by pair rather than hashed: a list of tags or scopes holds a handful, and a
// set for each is what reading a long one spends.
const pairwiseEntries = 16

// FirstOccurrences returns entries with each kept at its first occurrence, in
// the order written, and every repeat with the index it repeats, in order.
// Entries compare as the text written. A list repeating nothing is returned as
// it is, with no repeat, and one of at most pairwiseEntries allocates nothing.
func FirstOccurrences(entries []string) (kept []string, repeats []Repeat) {
	repeats = repeatsIn(entries)
	if len(repeats) == 0 {
		return entries, nil
	}
	kept = make([]string, 0, len(entries)-len(repeats))
	next := 0
	for i, e := range entries {
		if next < len(repeats) && repeats[next].At == i {
			next++
			continue
		}
		kept = append(kept, e)
	}
	return kept, repeats
}

// repeatsIn returns every entry of entries an earlier one equals, in order.
func repeatsIn(entries []string) []Repeat {
	var out []Repeat
	if len(entries) <= pairwiseEntries {
		for i := 1; i < len(entries); i++ {
			if first := slices.Index(entries[:i], entries[i]); first >= 0 {
				out = append(out, Repeat{At: i, First: first})
			}
		}
		return out
	}
	firsts := make(map[string]int, len(entries))
	for i, e := range entries {
		if first, seen := firsts[e]; seen {
			out = append(out, Repeat{At: i, First: first})
			continue
		}
		firsts[e] = i
	}
	return out
}
