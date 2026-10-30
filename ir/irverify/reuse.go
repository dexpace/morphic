// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

// reuseLimit is how many entries a reusedMap may have held for it to be
// emptied in place rather than replaced. Emptying a map costs in proportion to
// the most it has ever held, as Go never shrinks one, so a map kept after one
// huge collection would tax every collection read through it after.
const reuseLimit = 1024

// reusedMap is a map a check reads one collection after another through, so a
// collection costs no map of its own. The zero reusedMap is ready to use; pass
// keeps the same type for its checks.
type reusedMap[K comparable, V any] struct {
	m map[K]V
}

// emptied returns the map empty for the next collection: the one r holds,
// cleared, while the last collection left it at most reuseLimit entries, and a
// fresh one past that, which r holds from then on.
func (r *reusedMap[K, V]) emptied() map[K]V {
	if r.m == nil || len(r.m) > reuseLimit {
		r.m = make(map[K]V)
		return r.m
	}
	clear(r.m)
	return r.m
}
