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
	return groupsWithin(limit, doc, nil, func(operationGroup OperationGroup, steps *stepLimit) {
		for _, operation := range operationGroup.Operations {
			if !steps.take() {
				return
			}
			fn(operation)
		}
	})
}

// OperationPlace is where [ForEachOperationAt] finds an operation. Service is
// a copy of the service holding it, and Groups are copies of the groups
// enclosing it, outermost first, the group holding it last. Groups is the
// walk's own and is rewritten once the callback returns, so a caller copies
// what it keeps.
type OperationPlace struct {
	Service Service
	Groups  []OperationGroup
}

// ForEachOperationAt calls the non-nil fn for exactly the operations
// [ForEachOperation] visits, in its order, within its bounds and steps and
// with its report, handing each the place it was found at. A nil doc visits
// nothing.
func ForEachOperationAt(doc *Document, fn func(Operation, OperationPlace)) (truncated bool) {
	return forEachOperationAt(MaxWalkSteps, doc, fn)
}

// forEachOperationAt is ForEachOperationAt within limit steps. It takes a step
// for each operation as forEachOperation does, which a test holds the two to.
func forEachOperationAt(limit int, doc *Document, fn func(Operation, OperationPlace)) bool {
	place := &OperationPlace{}
	return groupsWithin(limit, doc, place, func(operationGroup OperationGroup, steps *stepLimit) {
		for _, operation := range operationGroup.Operations {
			if !steps.take() {
				return
			}
			fn(operation, *place)
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
	return groupsWithin(limit, doc, nil, func(operationGroup OperationGroup, _ *stepLimit) { fn(operationGroup) })
}

// groupsWithin calls fn for every group of doc ForEachGroup reaches within
// limit steps, handing it the steps left for what fn reads of the group, and
// reports whether a bound cut the walk short. Where place is not nil, the walk
// keeps it current while fn reads a group: the service walked, and the groups
// enclosing that group, the group itself last. ForEachGroup and
// ForEachOperation pass nil, so they copy no place.
func groupsWithin(limit int, doc *Document, place *OperationPlace, fn func(OperationGroup, *stepLimit)) bool {
	if doc == nil {
		return false
	}
	groupWalkValue := groupWalk{place: place, fn: fn, steps: stepLimit{left: limit}}
	for i := range doc.Services {
		if place != nil {
			*place = OperationPlace{Service: doc.Services[i], Groups: place.Groups[:0]}
		}
		groupWalkValue.groups(doc.Services[i].Groups, 0)
	}
	return groupWalkValue.steps.cut
}

// groupWalk is one walk of a document's groups: the place it keeps, if any,
// what it does with each group, the group slices it is inside, and the steps
// it may still take.
type groupWalk struct {
	place *OperationPlace
	fn    func(OperationGroup, *stepLimit)
	path  descent
	steps stepLimit
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
	for _, operationGroup := range groups {
		if !w.steps.take() {
			break
		}
		if w.place != nil {
			w.place.Groups = append(w.place.Groups, operationGroup)
		}
		w.fn(operationGroup, &w.steps)
		w.groups(operationGroup.Groups, depth+1)
		if w.place != nil {
			w.place.Groups = w.place.Groups[:len(w.place.Groups)-1]
		}
	}
	w.path.leave()
}
