package irfacts

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// hierarchy indexes the subtype relation of a registry, the closure of
// ir.Supertypes. Its strongly connected components are numbered in post-order
// down the condensation, and each carries the interval from the least number
// below it to its own. A component outside another's interval is never below
// it, and one numbered by the other's own search always is. Only a component
// an earlier search reached first, through another parent, can lie inside an
// interval without being below it.
type hierarchy struct {
	vertex   map[ir.TypeID]int         // declared types in sorted order, then supertypes the registry lacks
	comp     []int                     // each vertex's component
	cyclic   []bool                    // each component's members are subtypes of one another
	lo, hi   []int                     // each component's interval: the least number below it, and its own
	first    []int                     // the first number each component's own search gave out
	children map[ir.TypeID][]ir.TypeID // the declared models naming each type among their supertypes
	aliases  aliasTargets              // what each alias scalar stands for
}

// newHierarchy indexes the subtype relation of the models in types.
func newHierarchy(types ir.TypeRegistry) *hierarchy {
	h := &hierarchy{
		vertex:   make(map[ir.TypeID]int, len(types)),
		children: map[ir.TypeID][]ir.TypeID{},
		aliases:  newAliasTargets(types),
	}
	h.number(h.condense(h.supertypeEdges(types)))
	return h
}

// supertypes appends the supertypes of m to dst as ir.Supertypes returns them:
// its Base, then each Implements entry, each read through its alias scalars,
// skipping a reference that names no type.
func (h *hierarchy) supertypes(dst []ir.TypeID, m *ir.Model) []ir.TypeID {
	if m.Base != nil {
		dst = appendTarget(dst, h.aliases.target(m.Base.Target))
	}
	for _, r := range m.Implements {
		dst = appendTarget(dst, h.aliases.target(r.Target))
	}
	return dst
}

// supertypeEdges gives every declared type a vertex, in sorted ID order, and
// each supertype the registry lacks one as it is first named. It returns each
// vertex's edges to its supertypes, and records the reverse as children.
func (h *hierarchy) supertypeEdges(types ir.TypeRegistry) [][]int {
	ids := slices.Sorted(maps.Keys(types))
	for i, id := range ids {
		h.vertex[id] = i
	}
	edges := make([][]int, len(ids))
	var supers []ir.TypeID
	for i, id := range ids {
		m, ok := types[id].(*ir.Model)
		if !ok || m == nil {
			continue
		}
		supers = h.supertypes(supers[:0], m)
		for _, super := range supers {
			v, named := h.vertex[super]
			if !named {
				v = len(edges)
				h.vertex[super] = v
				edges = append(edges, nil)
			}
			edges[i] = append(edges[i], v)
			h.children[super] = append(h.children[super], id)
		}
	}
	return edges
}

// condense records each vertex's component and whether each component holds a
// cycle, and returns each component's child components, a child once for each
// edge that names the parent.
func (h *hierarchy) condense(edges [][]int) [][]int {
	comps := newTarjan(edges).run()
	h.comp = make([]int, len(edges))
	for c, members := range comps {
		for _, v := range members {
			h.comp[v] = c
		}
	}
	h.cyclic = make([]bool, len(comps))
	kids := make([][]int, len(comps))
	for c, members := range comps {
		for _, v := range members {
			for _, w := range edges[v] {
				p := h.comp[w]
				if p == c {
					h.cyclic[c] = true // a member naming itself, or another: a component of two has such an edge.
					continue
				}
				kids[p] = append(kids[p], c)
			}
		}
	}
	return kids
}

// number searches down kids from each component no other names as a child, and
// gives each component its post-order number, as hi, the least number of any
// component below it, as lo, and the first number its own search gives out, as
// first. The search is iterative, as a hierarchy may be as deep as the registry
// is long.
func (h *hierarchy) number(kids [][]int) {
	h.lo, h.hi, h.first = make([]int, len(kids)), make([]int, len(kids)), make([]int, len(kids))
	isKid := make([]bool, len(kids))
	for _, ks := range kids {
		for _, k := range ks {
			isKid[k] = true
		}
	}
	visited := make([]bool, len(kids))
	next := 0
	var path []frame
	for root := range kids {
		if isKid[root] {
			continue
		}
		visited[root] = true
		h.first[root] = next
		path = append(path, frame{vertex: root})
		for len(path) > 0 {
			top := &path[len(path)-1]
			if top.next < len(kids[top.vertex]) {
				k := kids[top.vertex][top.next]
				top.next++
				if !visited[k] {
					visited[k] = true
					h.first[k] = next
					path = append(path, frame{vertex: k})
				}
				continue
			}
			c := top.vertex
			path = path[:len(path)-1]
			h.close(c, kids[c], next)
			next++
		}
	}
}

// close numbers component c, whose kids are all numbered already: the
// condensation has no cycle, so a kid the search met before has been closed.
func (h *hierarchy) close(c int, kids []int, number int) {
	h.hi[c], h.lo[c] = number, number
	for _, k := range kids {
		h.lo[c] = min(h.lo[c], h.lo[k])
	}
}

// subtype answers [Index.IsSubtype] from the intervals, with settled false when
// they cannot: target's interval lies inside base's, but a search before base's
// own numbered it, so it may lie on no path down from base. That needs one of
// base's subtypes to have a supertype that is neither base nor below it, so in
// a forest every pair is settled.
func (h *hierarchy) subtype(target, base ir.TypeID) (sub, settled bool) {
	t, named := h.vertex[target]
	b, alsoNamed := h.vertex[base]
	if !named || !alsoNamed {
		return false, true
	}
	ct, cb := h.comp[t], h.comp[b]
	if ct == cb {
		return h.cyclic[ct], true
	}
	if h.lo[ct] < h.lo[cb] || h.hi[ct] >= h.hi[cb] {
		return false, true
	}
	return true, h.hi[ct] >= h.first[cb]
}

// descendants returns the sorted declared types with base above them, walking
// down children. A type is expanded once, so a cyclic hierarchy ends.
func (h *hierarchy) descendants(base ir.TypeID) []ir.TypeID {
	found := map[ir.TypeID]bool{}
	pending := []ir.TypeID{base}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, child := range h.children[id] {
			if !found[child] {
				found[child] = true
				pending = append(pending, child)
			}
		}
	}
	return slices.Sorted(maps.Keys(found))
}
