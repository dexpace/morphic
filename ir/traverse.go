// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// MaxGroupDepth bounds [ForEachGroup]'s descent into nested operation groups,
// and so [ForEachOperation]'s. A top-level group of a [Service] sits at level 0,
// and a group at a deeper level than this, with what it holds, is not visited.
// Nesting that deep is pathological and is truncated rather than allowed to
// exhaust the stack.
const MaxGroupDepth = 128

// MaxWalkSteps bounds a typed walk: [ForEachGroup] takes a step per group,
// [ForEachOperation] one per group and one per operation, [TypeEdges] one per
// value it enters, and an [Editor] one per group and operation it reads. A node
// a step counts shows a visitor of [WalkValues] at least eight values, so a
// typed walk is cut only in a document a visitor pruning nothing is cut in too.
const MaxWalkSteps = MaxWalkValues / 8

// stepLimit is what a typed walk may still take of its limit, and whether it
// met the end of it with more to read.
type stepLimit struct {
	left int
	cut  bool
}

// take takes one step if one is left, and otherwise records the cut.
func (s *stepLimit) take() bool {
	if s.left <= 0 {
		s.cut = true
		return false
	}
	s.left--
	return true
}

// ForEachOperation calls the non-nil fn for every operation in doc, in
// declaration order, through [ForEachGroup], whose bounds, guard and report it
// shares, taking a step for each operation beside each group. A nil doc visits
// nothing.
func ForEachOperation(doc *Document, fn func(Operation)) (truncated bool) {
	return forEachOperation(MaxWalkSteps, doc, fn)
}

// forEachOperation is ForEachOperation within limit steps.
func forEachOperation(limit int, doc *Document, fn func(Operation)) bool {
	return groupsWithin(limit, doc, func(g OperationGroup, steps *stepLimit) {
		for _, op := range g.Operations {
			if !steps.take() {
				return
			}
			fn(op)
		}
	})
}

// ForEachGroup calls the non-nil fn for every operation group in doc, in
// declaration order, each before the groups it nests, descending up to
// [MaxGroupDepth] and taking up to [MaxWalkSteps] steps, one a group. A nil doc
// visits nothing.
//
// It reports whether either bound left a group unvisited, so a caller deciding
// something from what it saw can tell an absent group from an unvisited one. A
// group slice two groups share is walked from each, as the document's encoding
// writes it at each; one that contains itself is not walked again from inside
// itself.
func ForEachGroup(doc *Document, fn func(OperationGroup)) (truncated bool) {
	return forEachGroup(MaxWalkSteps, doc, fn)
}

// forEachGroup is ForEachGroup within limit steps.
func forEachGroup(limit int, doc *Document, fn func(OperationGroup)) bool {
	return groupsWithin(limit, doc, func(g OperationGroup, _ *stepLimit) { fn(g) })
}

// groupsWithin calls fn for every group of doc ForEachGroup reaches within
// limit steps, handing it the steps left for what fn reads of the group, and
// reports whether a bound cut the walk short.
func groupsWithin(limit int, doc *Document, fn func(OperationGroup, *stepLimit)) bool {
	if doc == nil {
		return false
	}
	w := groupWalk{steps: stepLimit{left: limit}, fn: fn}
	for _, svc := range doc.Services {
		w.groups(svc.Groups, 0)
	}
	return w.steps.cut
}

// groupWalk is one walk of a document's groups: the steps it may still take,
// the group slices it is inside, and what it does with each group.
type groupWalk struct {
	steps stepLimit
	path  descent
	fn    func(OperationGroup, *stepLimit)
}

// groups walks groups depth-first, taking a step and calling fn per group.
func (w *groupWalk) groups(groups []OperationGroup, depth int) {
	// Every leaf recurses once into its own empty Groups, which has nothing to
	// skip. Checking for it first keeps the cap from claiming truncation for a
	// walk that reached everything.
	if len(groups) == 0 {
		return
	}
	if depth > MaxGroupDepth {
		w.steps.cut = true
		return
	}
	if !w.path.enter(&groups[0], len(groups)) {
		return
	}
	for _, g := range groups {
		if !w.steps.take() {
			break
		}
		w.fn(g, &w.steps)
		w.groups(g.Groups, depth+1)
	}
	w.path.leave()
}
