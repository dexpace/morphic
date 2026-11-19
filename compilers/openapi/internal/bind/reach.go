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
	out   map[DocumentPointer][]DocumentPointer
	work  *work
	known bool
}

// reachOf builds the graph over what the pass b settled into t, placing each
// position through t's bases, in the order t lists them, so where the budget
// runs out depends on no order of reading. Past the budget, or once the
// binding's context is done, it links nothing more and is not known.
func reachOf(b *binder, t *ReferenceTargetTable) reach {
	r := reach{out: map[DocumentPointer][]DocumentPointer{}, work: b.work, known: true}
	cut := false
	place := func(p DocumentPointer) DocumentPointer {
		if cut = cut || b.work.over(); cut {
			return DocumentPointer{}
		}
		in, known := t.bases.holding(p)
		r.known = r.known && known
		return in.root
	}
	link := func(from, to DocumentPointer) {
		inFrom, inTo := place(from), place(to)
		r.known = r.known && !cut
		if !cut && inFrom != inTo {
			r.out[inFrom] = append(r.out[inFrom], inTo)
		}
	}
	for _, res := range b.ids.resources {
		if res.root.Pointer != "" && b.docs.reached[res.root.Doc] {
			link(DocumentPointer{Doc: res.root.Doc, Pointer: res.root.Pointer.Parent()}, res.root)
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
func (r reach) reachers(origins []DocumentPointer) func(resource DocumentPointer) bool {
	held := map[DocumentPointer][]DocumentPointer{}
	if !r.known {
		// What no verdict rests on, as it is not known, is not walked.
		origins = nil
	}
	type visit struct{ at, origin DocumentPointer }
	var queue []visit
	add := func(at, origin DocumentPointer) {
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
	return func(resource DocumentPointer) bool {
		return slices.ContainsFunc(held[resource], func(o DocumentPointer) bool { return o != resource })
	}
}

// sharedNames returns each $dynamicAnchor name more than one document
// declares, sorted, each with the documents declaring it: a reference to a
// name one document alone declares needs no walk.
func sharedNames(ids *identifiers) map[string][]dynamicKey {
	out := map[string][]dynamicKey{}
	for k := range ids.dynamic {
		out[k.name] = append(out[k.name], k)
	}
	for name, keys := range out {
		if len(keys) < 2 {
			delete(out, name)
			continue
		}
		slices.SortFunc(keys, func(x, y dynamicKey) int { return int(x.doc) - int(y.doc) })
	}
	return out
}

// dynamicReach is, for each document's declarations of a name several
// documents declare, whether a resource of another document declaring the
// name reaches the resource holding the first (by), and where that is not
// known: the walk was cut, or a declaration's resource was not placed.
type dynamicReach struct {
	by      map[dynamicKey]bool
	unknown map[dynamicKey]bool
}

// dynamicReachOf walks g once for each name shared declares, its declaring
// resources the origins, in name order, so where the budget runs out depends
// on no order of reading (JSON Schema 2020-12 §8.2.3.2: the outermost
// resource of the dynamic scope declaring the name wins). Once the graph is
// not known, the budget is spent or the binding's context is done, nothing
// more is known.
func dynamicReachOf(ids *identifiers, b *bases, g reach, shared map[string][]dynamicKey) dynamicReach {
	out := dynamicReach{by: map[dynamicKey]bool{}, unknown: map[dynamicKey]bool{}}
	stopped := !g.known
	place := func(p DocumentPointer) (DocumentPointer, bool) {
		if stopped = stopped || g.work.over(); stopped {
			return DocumentPointer{}, false
		}
		res, placed := b.holding(p)
		return res.root, placed
	}
	for _, name := range slices.Sorted(maps.Keys(shared)) {
		keys := shared[name]
		first := map[dynamicKey]DocumentPointer{}
		var origins []DocumentPointer
		known := true
		for _, k := range keys {
			site := slices.MinFunc(ids.dynamic[k], comparePositions)
			for _, at := range ids.dynamic[k] {
				root, placed := place(at)
				known = known && placed
				origins = append(origins, root)
				if at == site {
					first[k] = root
				}
			}
		}
		if stopped {
			origins = nil
		}
		reached := g.reachers(origins)
		for _, k := range keys {
			out.by[k], out.unknown[k] = reached(first[k]), !known
		}
	}
	return out
}
