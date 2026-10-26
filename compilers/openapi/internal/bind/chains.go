package bind

import "slices"

// chain is where the chain of $refs from site ends, each position's answer
// read once and remembered. Every position has one hop, so a chain either
// ends, fails at a hop, or runs into a loop; the walk keeps the positions it
// passes on a path and settles them all on the way back, so no chain is
// followed twice however many share it (GitHub #903).
func (b *binder) chain(site Position) Chain {
	var path []Position
	on := map[Position]int{}
	end := b.follow(site, &path, on)
	// closeLoop settled a loop's members and took them off path, so every
	// position left on it leads into what end is.
	end.Member = false
	for _, p := range slices.Backward(path) {
		if end.Status == Resolved {
			end.Hops++
		}
		b.chains[p] = end
	}
	return b.chains[site]
}

// follow walks the chain from p onto path until it reaches a position settled
// before, a position already on path, which closes a loop, or a hop that ends
// it, and returns the answer for the last position left on path's successor.
func (b *binder) follow(p Position, path *[]Position, on map[Position]int) Chain {
	for {
		if c, done := b.chains[p]; done {
			return c
		}
		if i, looped := on[p]; looped {
			b.closeLoop((*path)[i:])
			*path = (*path)[:i]
			return Chain{Status: Cyclic}
		}
		h := b.hop(p)
		if b.work.over() {
			return Chain{Status: OverBudget}
		}
		on[p] = len(*path)
		*path = append(*path, p)
		switch {
		case h.Status != Resolved:
			return Chain{Status: Unresolved}
		case !b.holdsRef(h.Target):
			return Chain{Status: Resolved, End: h.Target}
		}
		p = h.Target
	}
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
