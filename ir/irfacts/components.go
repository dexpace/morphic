package irfacts

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Recursive reports whether a value of exactly the type id names can hold, at
// any depth, a value of the same shape (ir-design §4.9): its shape lies on a
// cycle of the type graph. A reference to a polymorphic model counts every
// subtype it may hold, so a subtype holding its own base is recursive, while a
// subtype that only composes its base is not. A type the document does not
// declare is not recursive.
func (x *Index) Recursive(id ir.TypeID) bool {
	return x.componentIndex().recursive[id]
}

// Component returns the sorted types whose shapes share a strongly connected
// component of the type graph with id's, as a slice the caller owns: id alone
// for a type on no cycle, or one recursive only through itself. It is nil when
// id is not in the document.
func (x *Index) Component(id ir.TypeID) []ir.TypeID {
	return slices.Clone(x.componentIndex().of[id])
}

// components holds the strongly connected components of a type graph, read per
// type through its shape.
type components struct {
	of        map[ir.TypeID][]ir.TypeID // the sorted types sharing each declared type's component
	recursive map[ir.TypeID]bool        // types whose shape lies on a cycle
}

// newComponents condenses g. A shape lies on a cycle when its component has
// another vertex, or it has an edge to itself.
func newComponents(g *typeGraph) *components {
	c := &components{of: make(map[ir.TypeID][]ir.TypeID, len(g.ids)), recursive: map[ir.TypeID]bool{}}
	for _, vertices := range newTarjan(g.next).run() {
		var shapes []int
		for _, v := range vertices {
			if v%2 == 0 {
				shapes = append(shapes, v)
			}
		}
		slices.Sort(shapes) // shape order is ID order.
		members := make([]ir.TypeID, len(shapes))
		for k, v := range shapes {
			members[k] = g.typeOf(v)
		}
		for k, v := range shapes {
			c.of[members[k]] = members
			if len(vertices) > 1 || g.loops[v] {
				c.recursive[members[k]] = true
			}
		}
	}
	return c
}
