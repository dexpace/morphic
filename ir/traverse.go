package ir

// MaxGroupDepth bounds [ForEachOperation]'s descent into nested operation
// groups. A top-level group of a [Service] sits at level 0, and an operation at a
// deeper level than this is not visited. Nesting that deep is pathological and
// is truncated rather than allowed to exhaust the stack.
const MaxGroupDepth = 128

// ForEachOperation calls fn for every operation in doc, in declaration order,
// descending nested operation groups up to [MaxGroupDepth]. A nil doc visits
// nothing.
//
// It reports whether the walk stopped at the cap with groups left unvisited, so
// a caller deciding something from what it saw can tell an absent operation from
// an unvisited one.
func ForEachOperation(doc *Document, fn func(Operation)) (truncated bool) {
	if doc == nil {
		return false
	}
	for _, svc := range doc.Services {
		if forEachGroupOperation(svc.Groups, 0, fn) {
			truncated = true
		}
	}
	return truncated
}

// forEachGroupOperation walks groups depth-first, calling fn per operation, and
// reports whether the bound cut the descent short.
func forEachGroupOperation(groups []OperationGroup, depth int, fn func(Operation)) bool {
	if depth > MaxGroupDepth {
		// Only a non-empty slice is being skipped. Every leaf recurses once into
		// its own empty Groups, so reporting the cap unconditionally would claim
		// truncation for a walk that reached everything.
		return len(groups) > 0
	}
	truncated := false
	for _, g := range groups {
		for _, op := range g.Operations {
			fn(op)
		}
		if forEachGroupOperation(g.Groups, depth+1, fn) {
			truncated = true
		}
	}
	return truncated
}
