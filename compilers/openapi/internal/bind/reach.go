// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"maps"
	"slices"
)

// reach is which schema resource leads to which, over every document a
// table reached: an edge from a resource to each resource nested directly in
// it, and from the resource holding a reference site, a $ref's, a static
// $dynamicRef's or a discriminator mapping value's, to the resource its hop
// lands in. A resource evaluation passes through on its way to another is in
// that one's dynamic scope (JSON Schema 2019-09 §8.2.4.2, 2020-12 §8.2.3.2).
// known is false where a resource on the way was not placed, as a merge past
// the bound may hide one.
type reach struct {
	out   map[Position][]Position
	work  *work
	known bool
}

// reachOf builds the graph over what the pass b settled into t, placing each
// position through t's bases, in the order t lists them, so where the budget
// runs out depends on no order of reading. Past the budget, or once the
// binding's context is done, it links nothing more and is not known.
func reachOf(b *binder, t *Table) reach {
	r := reach{out: map[Position][]Position{}, work: b.work, known: true}
	cut := false
	place := func(p Position) Position {
		if cut = cut || b.work.over(); cut {
			return Position{}
		}
		in, known := t.bases.holding(p)
		r.known = r.known && known
		return in.root
	}
	link := func(from, to Position) {
		inFrom, inTo := place(from), place(to)
		r.known = r.known && !cut
		if !cut && inFrom != inTo {
			r.out[inFrom] = append(r.out[inFrom], inTo)
		}
	}
	for _, res := range b.ids.resources {
		if res.root.Pointer != "" && b.docs.reached[res.root.Doc] {
			link(Position{Doc: res.root.Doc, Pointer: res.root.Pointer.Parent()}, res.root)
		}
	}
	for _, e := range t.entryList {
		if e.Hop.Status == Resolved {
			link(e.Site, e.Hop.Target)
		}
	}
	for _, k := range slices.SortedFunc(maps.Keys(t.mappings), mappingOrder(b.docs)) {
		if m := t.mappings[k]; m.Hop.Status == Resolved {
			link(m.At, m.Hop.Target)
		}
	}
	for at, out := range r.out {
		slices.SortFunc(out, comparePositions)
		r.out[at] = slices.Compact(out)
	}
	return r
}

// reachers returns whether a resource is reached by an origin other than
// itself, along the graph's edges, origins being resources. Each resource
// keeps at most two distinct origins, which is enough to tell one other than
// itself, so each edge is walked at most twice for each resource it leaves,
// and each walk is a step of the binding's budget: past it, the walk stops
// short and the budget says so. A graph not known is not walked.
func (r reach) reachers(origins []Position) func(resource Position) bool {
	held := map[Position][]Position{}
	if !r.known {
		// What no verdict rests on, as it is not known, is not walked.
		origins = nil
	}
	type visit struct{ at, origin Position }
	var queue []visit
	add := func(at, origin Position) {
		if len(held[at]) < 2 && !slices.Contains(held[at], origin) {
			held[at] = append(held[at], origin)
			queue = append(queue, visit{at: at, origin: origin})
		}
	}
	for _, o := range origins {
		add(o, o)
	}
	for len(queue) > 0 && !r.work.over() {
		v := queue[0]
		queue = queue[1:]
		for _, next := range r.out[v.at] {
			if !r.work.spend(1) {
				break
			}
			add(next, v.origin)
		}
	}
	return func(resource Position) bool {
		return slices.ContainsFunc(held[resource], func(o Position) bool { return o != resource })
	}
}
