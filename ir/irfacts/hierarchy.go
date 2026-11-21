// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	vertex   map[ir.TypeID]int // declared types in sorted order, then supertypes the registry lacks
	reach    *intervals        // what each vertex is a subtype of
	children [][]ir.TypeID     // by vertex, the declared models naming it among their supertypes
	aliases  aliasTargets      // what each alias scalar stands for
}

// newHierarchy indexes the subtype relation of the models in types.
func newHierarchy(types ir.TypeRegistry) *hierarchy {
	hierarchyValue := &hierarchy{
		vertex:  make(map[ir.TypeID]int, len(types)),
		aliases: newAliasTargets(types),
	}
	hierarchyValue.reach = newIntervals(hierarchyValue.supertypeEdges(types))
	return hierarchyValue
}

// childrenOf returns the declared models naming id among their supertypes.
func (h *hierarchy) childrenOf(id ir.TypeID) []ir.TypeID {
	if v, named := h.vertex[id]; named {
		return h.children[v]
	}
	return nil
}

// supertypes appends the supertypes of model to dst as ir.Supertypes returns
// them: its Base, then each Implements entry, each read through its alias
// scalars, skipping a reference that names no type.
func (h *hierarchy) supertypes(dst []ir.TypeID, model *ir.Model) []ir.TypeID {
	if model.Base != nil {
		dst = appendTarget(dst, h.aliases.target(model.Base.Target))
	}
	for _, typeRef := range model.Implements {
		dst = appendTarget(dst, h.aliases.target(typeRef.Target))
	}
	return dst
}

// supertypeEdges gives every declared type a vertex, in sorted ID order, and
// each supertype the registry lacks one as it is first named. It returns each
// vertex's edges to its supertypes, and records the reverse as children. A
// supertype the registry lacks declares nothing, so its vertex has no edges.
func (h *hierarchy) supertypeEdges(types ir.TypeRegistry) [][]int {
	ids := slices.Sorted(maps.Keys(types))
	for i, id := range ids {
		h.vertex[id] = i
	}
	most := mostSupertypes(types)
	edges, children := pairsOf[int](most), pairsOf[ir.TypeID](most)
	var supers []ir.TypeID
	for i, id := range ids {
		model, ok := types[id].(*ir.Model)
		if !ok || model == nil {
			continue
		}
		supers = h.supertypes(supers[:0], model)
		for _, super := range supers {
			v, named := h.vertex[super]
			if !named {
				v = len(h.vertex)
				h.vertex[super] = v
			}
			edges.add(i, v)
			children.add(v, id)
		}
	}
	h.children = children.grouped(len(h.vertex))
	return edges.grouped(len(h.vertex))
}

// mostSupertypes returns how many supertypes the models of types name at most:
// a Base and each Implements entry, before any is read through its aliases.
func mostSupertypes(types ir.TypeRegistry) int {
	most := 0
	for _, td := range types {
		if model, ok := td.(*ir.Model); ok && model != nil {
			most += len(model.Implements)
			if model.Base != nil {
				most++
			}
		}
	}
	return most
}

// subtype answers [DocumentFacts.IsSubtype] from the intervals, with settled
// false where they leave the pair open.
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
		for _, child := range h.childrenOf(id) {
			if !found[child] {
				found[child] = true
				pending = append(pending, child)
			}
		}
	}
	return slices.Sorted(maps.Keys(found))
}
