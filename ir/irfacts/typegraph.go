package irfacts

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// typeGraph is the graph [Index.Usage] spreads along and [Index.Recursive]
// reads cycles in (ir-design §4.9). Each declared type has two vertices: its
// shape, what a value of exactly that type holds, and its range, every shape a
// value declared as that type may take. A held part reaches its target's range
// when the target is polymorphic, and its shape otherwise. A composed part
// reaches a shape, never a range, so a subtype including its base's members
// does not reach the base's other subtypes through it.
type typeGraph struct {
	ids   []ir.TypeID       // declared types, sorted: type i has shape 2i and range 2i+1
	index map[ir.TypeID]int // each declared type's position in ids
	poly  []bool            // a reference to type i reaches its range
	next  [][]int           // each vertex's edges to what a value there holds or may be
	args  [][]int           // each shape's edges to its field arguments, which usage alone follows
	loops []bool            // vertices with an edge to themselves
}

// newTypeGraph builds the graph over types, reading subtypes and alias targets
// from h, which indexes the same registry.
func newTypeGraph(types ir.TypeRegistry, h *hierarchy) *typeGraph {
	ids := slices.Sorted(maps.Keys(types))
	g := &typeGraph{
		ids:   ids,
		index: make(map[ir.TypeID]int, len(ids)),
		poly:  make([]bool, len(ids)),
		next:  make([][]int, 2*len(ids)),
		args:  make([][]int, 2*len(ids)),
		loops: make([]bool, 2*len(ids)),
	}
	for i, id := range ids {
		g.index[id] = i
		g.poly[i] = polymorphic(types, h.aliases.target(id))
	}
	var p typeParts
	for i, id := range ids {
		p.read(types[id])
		g.addType(i, &p, h.children[id])
	}
	return g
}

// polymorphic reports whether a value declared as the type id names may hold
// any of its subtypes: id names a model with a discriminator, or an abstract
// one. A plain model names values of exactly its own shape.
func polymorphic(types ir.TypeRegistry, id ir.TypeID) bool {
	m, ok := types[id].(*ir.Model)
	return id != "" && ok && m != nil && (m.Discriminator != nil || m.Abstract)
}

// addType adds the edges out of type i's two vertices. Its shape reaches what
// it holds, the shapes it composes and the shape it aliases; its range reaches
// its own shape, the range it aliases and its direct subtypes' ranges.
func (g *typeGraph) addType(i int, p *typeParts, children []ir.TypeID) {
	shape, whole := 2*i, 2*i+1
	for _, id := range p.held {
		g.linkReference(shape, id)
	}
	for _, id := range p.composed {
		g.linkTo(shape, id, false)
	}
	g.linkTo(shape, p.alias, false)
	g.linkTo(whole, p.alias, true)
	g.link(whole, shape)
	for _, id := range children {
		g.linkTo(whole, id, true)
	}
	for _, id := range p.args {
		if v, ok := g.reference(id); ok {
			g.args[shape] = append(g.args[shape], v)
		}
	}
}

// linkTo adds an edge from one vertex to the shape of the type id, or its
// range when whole is set, and none where the registry declares no such type.
func (g *typeGraph) linkTo(from int, id ir.TypeID, whole bool) {
	if to, ok := g.vertex(id, whole); ok {
		g.link(from, to)
	}
}

// linkReference adds an edge from one vertex to the vertex a reference to id
// reaches, and none where the registry declares no such type.
func (g *typeGraph) linkReference(from int, id ir.TypeID) {
	if to, ok := g.reference(id); ok {
		g.link(from, to)
	}
}

// vertex returns the shape of the declared type id, or its range when whole is
// set, and false when the registry declares no such type.
func (g *typeGraph) vertex(id ir.TypeID, whole bool) (int, bool) {
	i, declared := g.index[id]
	switch {
	case !declared || id == "":
		return 0, false
	case whole:
		return 2*i + 1, true
	default:
		return 2 * i, true
	}
}

// reference returns the vertex a reference to id reaches: its range when it is
// polymorphic, its shape otherwise, and false when id is not declared.
func (g *typeGraph) reference(id ir.TypeID) (int, bool) {
	i, declared := g.index[id]
	if !declared {
		return 0, false
	}
	return g.vertex(id, g.poly[i])
}

// typeOf returns the type vertex v belongs to.
func (g *typeGraph) typeOf(v int) ir.TypeID {
	return g.ids[v/2]
}

// link adds an edge from one vertex to another.
func (g *typeGraph) link(from, to int) {
	if from == to {
		g.loops[from] = true
	}
	g.next[from] = append(g.next[from], to)
}
