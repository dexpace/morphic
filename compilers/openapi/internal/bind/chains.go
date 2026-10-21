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
			end.Named = end.Named && b.hops[p].Named
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
			id := b.closeLoop((*path)[i:])
			*path = (*path)[:i]
			return Chain{Status: Cyclic, Loop: id}
		}
		h := b.hop(p)
		if b.work.over() {
			return Chain{Status: OverBudget}
		}
		on[p] = len(*path)
		*path = append(*path, p)
		switch {
		case h.Status != Resolved:
			return Chain{Status: Unresolved, Failed: p}
		case !b.holdsRef(h.Target):
			return Chain{Status: Resolved, End: h.Target, Role: h.Role, Named: true}
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

// closeLoop records members as a loop, each settled as its member, and
// returns the loop's index.
func (b *binder) closeLoop(members []Position) int {
	id := len(b.loops)
	sorted := slices.SortedFunc(slices.Values(members), comparePositions)
	b.loops = append(b.loops, Loop{Members: sorted})
	for _, m := range members {
		b.chains[m] = Chain{Status: Cyclic, Loop: id, Member: true}
	}
	return id
}

// placeLoops sets where each loop is reported: its least member in the source,
// or, when none is, the least source position whose chain leads into it. A
// loop neither reaches is not reported.
func (b *binder) placeLoops() {
	leadIn := map[int]Position{}
	for p, c := range b.chains {
		if c.Status != Cyclic || c.Member || p.Doc != Source {
			continue
		}
		if least, seen := leadIn[c.Loop]; !seen || p.less(least) {
			leadIn[c.Loop] = p
		}
	}
	for i := range b.loops {
		l := &b.loops[i]
		if first := l.Members[0]; first.Doc == Source {
			l.Site, l.Reported = first, true
			continue
		}
		l.Site, l.Reported = leadIn[i]
	}
}
