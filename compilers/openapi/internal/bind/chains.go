// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

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
		for _, p := range path {
			b.wait.on[p] = true
		}
		return Chain{}, true
	}
	// closeLoop settled a loop's members and took them off path, so every
	// position left on it leads into what end is.
	end.PositionLiesOnReferenceCycle = false
	for _, p := range slices.Backward(path) {
		if end.Status == Resolved {
			end.Hops++
		}
		b.chains[p] = end
	}
	return b.chains[site], false
}

// follow walks the chain from p onto path until it reaches a position settled
// before, a position already on path, which closes a loop, or a hop that ends
// it, and returns the answer for the last position left on path's successor,
// or reports that it waits (chain).
func (b *binder) follow(p DocumentPointer, path *[]DocumentPointer, on map[DocumentPointer]int) (Chain, bool) {
	for {
		if c, done := b.chains[p]; done {
			return c, false
		}
		if b.wait.on[p] {
			return Chain{}, true
		}
		if i, looped := on[p]; looped {
			b.closeLoop((*path)[i:])
			*path = (*path)[:i]
			return Chain{Status: Cyclic}, false
		}
		h := b.hop(p)
		if b.work.over() {
			return Chain{Status: OverBudget}, false
		}
		on[p] = len(*path)
		*path = append(*path, p)
		switch {
		case h.Status != Resolved:
			return Chain{Status: Unresolved}, h.Reason == PendingExternalDocumentRead
		case !b.holdsRef(h.Target):
			return Chain{Status: Resolved, End: h.Target}, false
		}
		p = h.Target
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
// reference object in f to settle again, now the documents they wait for are
// read. A held position waits no longer, so its own chain reads its hop again;
// one that leads to it waits on until release.
func (w *waits) requeue(f *finder, hops map[DocumentPointer]Hop, objHops map[DocumentPointer]objectHop) {
	for _, p := range w.held {
		delete(hops, p)
		delete(w.on, p)
		if w.sites[p] {
			delete(w.sites, p)
			f.fresh = append(f.fresh, p)
		}
	}
	for _, p := range w.heldObjects {
		delete(objHops, p)
		for kind := range RefKind(len(refStates)) {
			k := objectKey{at: p, kind: kind + 1}
			delete(w.onObjects, k)
			if w.objects[k] {
				delete(w.objects, k)
				f.freshObjects = append(f.freshObjects, k)
			}
		}
	}
	for _, k := range w.heldMappings {
		delete(w.mappings, k)
		f.freshMappings = append(f.freshMappings, k)
	}
	w.held, w.heldObjects, w.heldMappings, w.released, w.declaring = nil, nil, nil, false, nil
}

// release notes in f every site, mapping value and reference object that
// waits, to settle once more now nothing is held, and reports whether there
// was any. A second release with no held document read since would meet the
// same hops, so it releases nothing, which bounds the pass: what still waits
// then has no entry.
func (w *waits) release(f *finder) bool {
	if w.released || len(w.sites) == 0 && len(w.mappings) == 0 && len(w.objects) == 0 {
		return false
	}
	w.released = true
	clear(w.on)
	clear(w.onObjects)
	f.fresh = slices.AppendSeq(f.fresh, maps.Keys(w.sites))
	f.freshMappings = slices.AppendSeq(f.freshMappings, maps.Keys(w.mappings))
	f.freshObjects = slices.AppendSeq(f.freshObjects, maps.Keys(w.objects))
	clear(w.sites)
	clear(w.mappings)
	clear(w.objects)
	return true
}

// holdsRef reports whether p writes a $ref of its own, which a chain landing
// there follows on, or a $dynamicRef that is the $ref it spells, or may be
// once the document its hop waits for is read (namesDynamic).
func (b *binder) holdsRef(p DocumentPointer) bool {
	// Only a hop's target is asked, a position its document holds, so the path
	// reaches it.
	path, _ := b.docs.path(p)
	n := path[len(path)-1]
	if _, ok := b.docs.refOf(p.Doc, n); ok {
		return true
	}
	value, ok := dynamicRefValue(b.docs, p.Doc, n)
	if !ok {
		return false
	}
	named, _ := namesDynamic(b, p, path, value)
	return !named
}

// closeLoop records members as a loop, each settled as its member.
func (b *binder) closeLoop(members []DocumentPointer) {
	sorted := slices.SortedFunc(slices.Values(members), b.docs.compare)
	b.loops = append(b.loops, Loop{Members: sorted})
	for _, m := range members {
		b.chains[m] = Chain{Status: Cyclic, PositionLiesOnReferenceCycle: true}
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
	slices.SortFunc(b.loops, func(x, y Loop) int {
		if c := b.docs.compare(x.Site, y.Site); c != 0 {
			return c
		}
		return int(x.Kind) - int(y.Kind)
	})
	b.loops = slices.CompactFunc(b.loops, func(x, y Loop) bool { return x.Site == y.Site })
}
