// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// MaxGroupDepth bounds [ForEachGroup]'s descent into nested operation groups,
// and so [ForEachOperation]'s. A top-level group of a [Service] sits at level 0,
// and a group at a deeper level than this, with what it holds, is not visited.
// Nesting that deep is pathological and is truncated rather than allowed to
// exhaust the stack.
const MaxGroupDepth = 128

// ForEachOperation calls the non-nil fn for every operation in doc, in
// declaration order, through [ForEachGroup], whose bound, guard and report it
// shares. A nil doc visits nothing.
func ForEachOperation(doc *Document, fn func(Operation)) (truncated bool) {
	return ForEachGroup(doc, func(g OperationGroup) {
		for _, op := range g.Operations {
			fn(op)
		}
	})
}

// ForEachGroup calls the non-nil fn for every operation group in doc, in
// declaration order, each before the groups it nests, descending up to
// [MaxGroupDepth]. A nil doc visits nothing.
//
// Groups past the cap are skipped and every other group is still visited. It
// reports whether any were skipped, so a caller deciding something from what
// it saw can tell an absent group from an unvisited one. A group slice two
// groups share is walked from each, so aliasing costs the unfolded size
// (#859); one that contains itself is not walked again from inside itself.
func ForEachGroup(doc *Document, fn func(OperationGroup)) (truncated bool) {
	if doc == nil {
		return false
	}
	var path descent
	for _, svc := range doc.Services {
		if forEachGroup(svc.Groups, 0, &path, fn) {
			truncated = true
		}
	}
	return truncated
}

// forEachGroup walks groups depth-first, calling fn per group, and reports
// whether the bound cut the descent short.
func forEachGroup(groups []OperationGroup, depth int, path *descent, fn func(OperationGroup)) bool {
	// Every leaf recurses once into its own empty Groups, which has nothing to
	// skip. Checking for it first keeps the cap from claiming truncation for a
	// walk that reached everything.
	if len(groups) == 0 {
		return false
	}
	if depth > MaxGroupDepth {
		return true
	}
	if !path.enter(&groups[0], len(groups)) {
		return false
	}
	truncated := false
	for _, g := range groups {
		fn(g)
		if forEachGroup(g.Groups, depth+1, path, fn) {
			truncated = true
		}
	}
	path.leave()
	return truncated
}
