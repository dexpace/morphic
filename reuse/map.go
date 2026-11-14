// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reuse

// Limit is how many entries a Map may hold for it to be emptied in place
// rather than replaced. Emptying a map costs in proportion to the most it has
// ever held, as Go never shrinks one, so a map kept after one huge collection
// would tax every collection read through it after.
const Limit = 1024

// Map is a map read one collection after another. The zero Map is ready.
type Map[K comparable, V any] struct {
	m map[K]V
}

// Emptied returns the map empty for the next collection, which only adds to it:
// the one r holds, cleared, while the last collection left it at most Limit
// entries, and a fresh one past that, which r holds from then on. It reads the
// length the last collection left, and a map that grew and had entries deleted
// costs as much to clear as at its largest.
func (r *Map[K, V]) Emptied() map[K]V {
	if r.m == nil || len(r.m) > Limit {
		r.m = make(map[K]V)
		return r.m
	}
	clear(r.m)
	return r.m
}
