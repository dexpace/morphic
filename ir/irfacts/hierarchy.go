package irfacts

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// hierarchy indexes the subtype relation of a registry, the closure of
// ir.Supertypes: a type reaches each of its supertypes, so the intervals of its
// reach answer whether one type is below another.
type hierarchy struct {
	vertex   map[ir.TypeID]int         // declared types in sorted order, then supertypes the registry lacks
	reach    *intervals                // what each vertex is a subtype of
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
	h.reach = newIntervals(h.supertypeEdges(types))
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

// subtype answers [Index.IsSubtype] from the intervals, with settled false where
// they leave the pair open.
func (h *hierarchy) subtype(target, base ir.TypeID) (sub, settled bool) {
	t, named := h.vertex[target]
	b, alsoNamed := h.vertex[base]
	if !named || !alsoNamed {
		return false, true
	}
	return h.reach.below(t, b)
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
