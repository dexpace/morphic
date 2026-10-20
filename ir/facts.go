package ir

import (
	"maps"
	"slices"
	"sync"
)

// Facts is the index of what a [Document] implies and does not state: the
// subtypes of a model, how each type is used across the API surface, and which
// types contain themselves (ir-design §4). Each fact is derived when first
// asked for and kept, so nothing is stored in the document and none goes stale.
//
// A Facts never writes its document. Answers already built do not follow later
// changes to it, so a caller that changes the document builds a new Facts. One
// mutex guards every memo, so a Facts is safe for concurrent use. Its zero
// value indexes no document.
type Facts struct {
	doc *Document

	mu       sync.Mutex
	closures map[TypeID]map[TypeID]bool // target -> every type above it
	children map[TypeID][]TypeID        // supertype -> models naming it directly; nil until built
	usage    map[TypeID]UsageFlags      // nil until built
	comps    *components                // nil until built
	steps    int                        // types the supertype walks expanded; a work counter
}

// NewFacts returns the index of doc's facts. It builds nothing: each fact is
// derived when first asked. A nil doc is allowed and has no facts.
func NewFacts(doc *Document) *Facts {
	return &Facts{doc: doc}
}

// IsSubtype reports whether target is a declared subtype of base at any
// distance along [Supertypes]. It answers exactly as [IsSubtype] does: the
// relation is not reflexive unless a cycle leads back, and target is not read
// through an alias (#844). The first question about a target walks the
// hierarchy above it once; every later question about it is a lookup.
func (f *Facts) IsSubtype(target, base TypeID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closure(target)[base]
}

// Subtypes returns every type of the document that [Facts.IsSubtype] holds a
// subtype of base, in sorted order, as a slice the caller owns. It is nil when
// there are none.
func (f *Facts) Subtypes(base TypeID) []TypeID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.descendants(base)
}

// types returns the document's registry, nil when there is no document.
func (f *Facts) types() TypeRegistry {
	if f.doc == nil {
		return nil
	}
	return f.doc.Types
}

// closure returns every type reachable from target by one or more steps along
// [Supertypes], the set IsSubtype looks base up in. It holds target itself only
// when a cycle leads back. The walk expands each type once, because a type
// enters the set once, and the result is kept per target.
func (f *Facts) closure(target TypeID) map[TypeID]bool {
	if up, ok := f.closures[target]; ok {
		return up
	}
	if f.closures == nil {
		f.closures = map[TypeID]map[TypeID]bool{}
	}
	up := map[TypeID]bool{}
	pending := []TypeID{target}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		f.steps++
		for _, super := range Supertypes(f.doc, id) {
			if !up[super] {
				up[super] = true
				pending = append(pending, super)
			}
		}
	}
	f.closures[target] = up
	return up
}

// descendants returns the sorted types with base above them, walking down the
// reverse index of direct supertype edges. A type is expanded once, so a cyclic
// hierarchy ends. Only the index is kept, so each call walks the subtree again.
func (f *Facts) descendants(base TypeID) []TypeID {
	children := f.childIndex()
	found := map[TypeID]bool{}
	pending := []TypeID{base}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, child := range children[id] {
			if !found[child] {
				found[child] = true
				pending = append(pending, child)
			}
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// childIndex returns, for each type, the models that name it among their
// [Supertypes]. It is the reverse of the edges closure follows, built once.
func (f *Facts) childIndex() map[TypeID][]TypeID {
	if f.children != nil {
		return f.children
	}
	children := map[TypeID][]TypeID{}
	for id := range f.types() {
		for _, super := range Supertypes(f.doc, id) {
			children[super] = append(children[super], id)
		}
	}
	f.children = children
	return children
}
