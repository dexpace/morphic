// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// MaxGroupDepth bounds [ForEachOperation]'s descent into nested operation
// groups. A top-level group of a [Service] sits at level 0, and an operation at a
// deeper level than this is not visited. Nesting that deep is pathological and
// is truncated rather than allowed to exhaust the stack.
const MaxGroupDepth = 128

// ForEachOperation calls the non-nil fn for every operation in doc, in
// declaration order, descending nested operation groups up to [MaxGroupDepth].
// A nil doc visits nothing.
//
// Groups past the cap are skipped and every other group is still visited. It
// reports whether any were skipped, so a caller deciding something from what
// it saw can tell an absent operation from an unvisited one. A group slice two
// groups share is walked from each, so aliasing costs the unfolded size
// (#859); one that contains itself is not walked again from inside itself.
func ForEachOperation(doc *Document, fn func(Operation)) (truncated bool) {
	if doc == nil {
		return false
	}
	var path descent
	for _, svc := range doc.Services {
		if forEachGroupOperation(svc.Groups, 0, &path, fn) {
			truncated = true
		}
	}
	return truncated
}

// forEachGroupOperation walks groups depth-first, calling fn per operation, and
// reports whether the bound cut the descent short.
func forEachGroupOperation(groups []OperationGroup, depth int, path *descent, fn func(Operation)) bool {
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
		for _, op := range g.Operations {
			fn(op)
		}
		if forEachGroupOperation(g.Groups, depth+1, path, fn) {
			truncated = true
		}
	}
	path.leave()
	return truncated
}
