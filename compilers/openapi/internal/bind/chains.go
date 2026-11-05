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
func (b *binder) chain(site Position) (Chain, bool) {
	var path []Position
	on := map[Position]int{}
	end, waits := b.follow(site, &path, on)
	if waits {
		for _, p := range path {
			b.wait.on[p] = true
		}
		return Chain{}, true
	}
	// closeLoop settled a loop's members and took them off path, so every
	// position left on it leads into what end is.
	end.Member = false
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
func (b *binder) follow(p Position, path *[]Position, on map[Position]int) (Chain, bool) {
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
			return Chain{Status: Unresolved}, h.Reason == Held
		case !b.holdsRef(h.Target):
			return Chain{Status: Resolved, End: h.Target}, false
		}
		p = h.Target
	}
}

// waits is what a pass defers until it has read the documents it held
// (readHeld): each position whose chain meets a hop held for them, each site
// and mapping value whose answer waits so, and the held hops and mapping
// values to read again once those documents are read. A chain that waits is
// followed again only once nothing is held (release), so a chain of held
// documents is followed once however many rounds it takes to read them.
// released bounds that: once between reads of held documents.
type waits struct {
	on           map[Position]bool
	sites        map[Position]bool
	mappings     map[mappingKey]bool
	held         []Position
	heldMappings []mappingKey
	released     bool
}

// newWaits returns a pass's deferrals, holding none.
func newWaits() *waits {
	return &waits{on: map[Position]bool{}, sites: map[Position]bool{}, mappings: map[mappingKey]bool{}}
}

// requeue forgets each held hop, and notes each held site and mapping value
// in f to settle again, now the documents they wait for are read. A held
// position waits no longer, so its own chain reads its hop again; one that
// leads to it waits on until release.
func (w *waits) requeue(f *finder, hops map[Position]Hop) {
	for _, p := range w.held {
		delete(hops, p)
		delete(w.on, p)
		if w.sites[p] {
			delete(w.sites, p)
			f.fresh = append(f.fresh, p)
		}
	}
	for _, k := range w.heldMappings {
		delete(w.mappings, k)
		f.freshMappings = append(f.freshMappings, k)
	}
	w.held, w.heldMappings, w.released = nil, nil, false
}

// release notes in f every site and mapping value that waits, to settle once
// more now nothing is held, and reports whether there was any. A second
// release with no held document read since would meet the same hops, so it
// releases nothing, which bounds the pass: what still waits then has no entry.
func (w *waits) release(f *finder) bool {
	if w.released || len(w.sites) == 0 && len(w.mappings) == 0 {
		return false
	}
	w.released = true
	clear(w.on)
	f.fresh = slices.AppendSeq(f.fresh, maps.Keys(w.sites))
	f.freshMappings = slices.AppendSeq(f.freshMappings, maps.Keys(w.mappings))
	clear(w.sites)
	clear(w.mappings)
	return true
}

// holdsRef reports whether p writes a $ref of its own, which a chain landing
// there follows on.
func (b *binder) holdsRef(p Position) bool {
	_, ok := b.docs.refOf(p.Doc, b.docs.node(p))
	return ok
}

// closeLoop records members as a loop, each settled as its member.
func (b *binder) closeLoop(members []Position) {
	sorted := slices.SortedFunc(slices.Values(members), b.docs.compare)
	b.loops = append(b.loops, Loop{Members: sorted})
	for _, m := range members {
		b.chains[m] = Chain{Status: Cyclic, Member: true}
	}
}

// placeLoops sets where each loop is reported: its least member, which is
// written where the defect is, in whichever document holds it.
func (b *binder) placeLoops() {
	for i := range b.loops {
		b.loops[i].Site = b.loops[i].Members[0]
	}
}
