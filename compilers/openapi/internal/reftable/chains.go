// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"maps"
	"slices"
)

// chain is where the chain of $refs from site ends, each position's answer
// read once and remembered. Every position has one hop, so a chain either
// ends, fails at a hop, or runs into a loop; the walk keeps the positions it
// passes on a path and settles them all on the way back, so no chain is
// followed twice however many share it (GitHub #903). It reports waits, and
// remembers nothing, where the chain meets a hop held for documents the pass
// has yet to read, or a position whose chain does (waits).
func (b *binder) chain(site DocumentPointer) (Chain, bool) {
	var path []DocumentPointer
	on := map[DocumentPointer]int{}
	end, waits := b.follow(site, &path, on)
	if waits {
		for _, documentPointer := range path {
			b.wait.on[documentPointer] = true
		}
		return Chain{}, true
	}
	// closeLoop settled a loop's members and took them off path, so every
	// position left on it leads into what end is.
	end.PositionLiesOnReferenceCycle = false
	for _, documentPointer := range slices.Backward(path) {
		if end.Status == Resolved {
			end.Hops++
		}
		b.chains[documentPointer] = end
	}
	return b.chains[site], false
}

// follow walks the chain from documentPointer onto path until it reaches a
// position settled before, a position already on path, which closes a loop, or
// a hop that ends it, and returns the answer for the last position left on
// path's successor, or reports that it waits (chain).
func (b *binder) follow(documentPointer DocumentPointer, path *[]DocumentPointer, on map[DocumentPointer]int) (Chain, bool) {
	for {
		if chain, done := b.chains[documentPointer]; done {
			return chain, false
		}
		if b.wait.on[documentPointer] {
			return Chain{}, true
		}
		if i, looped := on[documentPointer]; looped {
			b.closeLoop((*path)[i:])
			*path = (*path)[:i]
			return Chain{Status: Cyclic}, false
		}
		hop := b.hop(documentPointer)
		if b.work.over() {
			return Chain{Status: OverBudget}, false
		}
		on[documentPointer] = len(*path)
		*path = append(*path, documentPointer)
		switch {
		case hop.Status != Resolved:
			return Chain{Status: Unresolved}, hop.Reason == PendingExternalDocumentRead
		case !b.holdsRef(hop.Target):
			return Chain{Status: Resolved, End: hop.Target}, false
		}
		documentPointer = hop.Target
	}
}

// waits is what a pass defers until it has read the documents it held
// (readHeld): each position, a schema $ref's or a reference object's of a
// kind, whose chain meets a hop held for them, or whose pointer passes a
// reference whose chain waits; each site, mapping value and reference object
// whose answer waits so; and the held hops to read again. A chain that waits
// is followed again once nothing is held (release), once between reads
// (released), so a chain of held documents is followed once.
type waits struct {
	on           map[DocumentPointer]bool
	onObjects    map[objectKey]bool
	sites        map[DocumentPointer]bool
	mappings     map[mappingKey]bool
	objects      map[objectKey]bool
	held         []DocumentPointer
	heldObjects  []DocumentPointer
	heldMappings []mappingKey
	released     bool
	// declaring is each document read and not yet reached whose declaration
	// a hop waits for, and unwaited says what waits so is read as a location
	// instead, for the rest of the pass (goOn).
	declaring []DocID
	unwaited  bool
}

// newWaits returns a pass's deferrals, holding none.
func newWaits() *waits {
	return &waits{on: map[DocumentPointer]bool{}, onObjects: map[objectKey]bool{}, sites: map[DocumentPointer]bool{},
		mappings: map[mappingKey]bool{}, objects: map[objectKey]bool{}}
}

// requeue forgets each held hop, and notes each held site, mapping value and
// reference object in finderValue to settle again, now the documents they wait
// for are read. A held position waits no longer, so its own chain reads its hop
// again; one that leads to it waits on until release.
func (w *waits) requeue(finderValue *finder, hops map[DocumentPointer]Hop, objHops map[DocumentPointer]objectHop) {
	for _, documentPointer := range w.held {
		delete(hops, documentPointer)
		delete(w.on, documentPointer)
		if w.sites[documentPointer] {
			delete(w.sites, documentPointer)
			finderValue.fresh = append(finderValue.fresh, documentPointer)
		}
	}
	for _, documentPointer := range w.heldObjects {
		delete(objHops, documentPointer)
		for kind := range RefKind(len(refStates)) {
			objectKeyValue := objectKey{at: documentPointer, kind: kind + 1}
			delete(w.onObjects, objectKeyValue)
			if w.objects[objectKeyValue] {
				delete(w.objects, objectKeyValue)
				finderValue.freshObjects = append(finderValue.freshObjects, objectKeyValue)
			}
		}
	}
	for _, mappingKey := range w.heldMappings {
		delete(w.mappings, mappingKey)
		finderValue.freshMappings = append(finderValue.freshMappings, mappingKey)
	}
	w.held, w.heldObjects, w.heldMappings, w.released, w.declaring = nil, nil, nil, false, nil
}

// release notes in finderValue every site, mapping value and reference object
// that waits, to settle once more now nothing is held, and reports whether
// there was any. A second release with no held document read since would meet
// the same hops, so it releases nothing, which bounds the pass: what still
// waits then has no entry.
func (w *waits) release(finderValue *finder) bool {
	if w.released || len(w.sites) == 0 && len(w.mappings) == 0 && len(w.objects) == 0 {
		return false
	}
	w.released = true
	clear(w.on)
	clear(w.onObjects)
	finderValue.fresh = slices.AppendSeq(finderValue.fresh, maps.Keys(w.sites))
	finderValue.freshMappings = slices.AppendSeq(finderValue.freshMappings, maps.Keys(w.mappings))
	finderValue.freshObjects = slices.AppendSeq(finderValue.freshObjects, maps.Keys(w.objects))
	clear(w.sites)
	clear(w.mappings)
	clear(w.objects)
	return true
}

// holdsRef reports whether documentPointer writes a $ref of its own, which a
// chain landing there follows on, or a $dynamicRef that is the $ref it spells,
// or may be once the document its hop waits for is read (namesDynamic).
func (b *binder) holdsRef(documentPointer DocumentPointer) bool {
	// Only a hop's target is asked, a position its document holds, so the path
	// reaches it.
	path, _ := b.docs.path(documentPointer)
	node := path[len(path)-1]
	if _, ok := b.docs.refOf(documentPointer.Doc, node); ok {
		return true
	}
	value, ok := dynamicRefValue(b.docs, documentPointer.Doc, node)
	if !ok {
		return false
	}
	named, _ := namesDynamic(b, documentPointer, path, value)
	return !named
}

// closeLoop records members as a loop, each settled as its member.
func (b *binder) closeLoop(members []DocumentPointer) {
	sorted := slices.SortedFunc(slices.Values(members), b.docs.compare)
	b.loops = append(b.loops, Loop{Members: sorted})
	for _, documentPointer := range members {
		b.chains[documentPointer] = Chain{Status: Cyclic, PositionLiesOnReferenceCycle: true}
	}
}

// placeLoops sets where each loop is reported: its least member, which is
// written where the defect is, in whichever document holds it. Every position
// has one hop, so one member's loop is the same loop whichever kind of object
// a chain read it as: a loop read as several is kept once, as the least.
func (b *binder) placeLoops() {
	for i := range b.loops {
		b.loops[i].Site = b.loops[i].Members[0]
	}
	slices.SortFunc(b.loops, func(loop, loopValue Loop) int {
		if c := b.docs.compare(loop.Site, loopValue.Site); c != 0 {
			return c
		}
		return int(loop.Kind) - int(loopValue.Kind)
	})
	b.loops = slices.CompactFunc(b.loops, func(loop, loopValue Loop) bool { return loop.Site == loopValue.Site })
}
