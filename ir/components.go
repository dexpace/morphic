package ir

import (
	"maps"
	"slices"
)

// Recursive reports whether a value of the type id names can contain a value of
// the same type: its strongly connected component, taken over the types a
// value is made of, has more than one member, or the type contains itself
// directly. A discriminator mapping is not containment, so a base routing to
// its own subtype is not recursive by that alone. A type that is not in the
// document is not recursive.
func (f *Facts) Recursive(id TypeID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.componentIndex().recursive(id)
}

// Component returns the sorted members of the strongly connected component of
// id in the containment graph, as a slice the caller owns: id alone for a type
// in no cycle. It is nil when id is not in the document. An edge to a type the
// document does not declare is ignored; reporting one is irverify's.
func (f *Facts) Component(id TypeID) []TypeID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.componentIndex().component(id)
}

// componentIndex returns the components of the containment graph, built once.
func (f *Facts) componentIndex() *components {
	if f.comps == nil {
		f.comps = newComponents(f.types())
	}
	return f.comps
}

// components is the strongly connected components of the containment graph
// over a registry's types.
type components struct {
	of       map[TypeID][]TypeID // the sorted component of each type of the registry
	selfLoop map[TypeID]bool     // types that contain themselves directly
}

// newComponents computes the components of the containment graph over types.
// Types are taken in sorted ID order and edges in containment order, so the
// result does not depend on map iteration.
func newComponents(types TypeRegistry) *components {
	ids := slices.Sorted(maps.Keys(types))
	position := make(map[TypeID]int, len(ids))
	for i, id := range ids {
		position[id] = i
	}
	c := &components{
		of:       make(map[TypeID][]TypeID, len(ids)),
		selfLoop: map[TypeID]bool{},
	}
	adj := make([][]int, len(ids))
	var contained []TypeID
	for i, id := range ids {
		contained = appendContained(contained[:0], types[id])
		for _, target := range contained {
			j, declared := position[target]
			if !declared {
				continue
			}
			adj[i] = append(adj[i], j)
			if j == i {
				c.selfLoop[id] = true
			}
		}
	}
	for _, vertices := range newTarjan(adj).run() {
		slices.Sort(vertices) // vertex order is ID order.
		members := make([]TypeID, len(vertices))
		for k, v := range vertices {
			members[k] = ids[v]
		}
		for _, id := range members {
			c.of[id] = members
		}
	}
	return c
}

// recursive reports whether id's component has several members or id contains
// itself.
func (c *components) recursive(id TypeID) bool {
	members, declared := c.of[id]
	return declared && (len(members) > 1 || c.selfLoop[id])
}

// component returns a copy of id's component, nil when id is not declared.
func (c *components) component(id TypeID) []TypeID {
	return slices.Clone(c.of[id])
}

// tarjan finds the strongly connected components of a directed graph given as
// adjacency lists, with Tarjan's algorithm run on an explicit stack: a
// containment chain as long as a registry must not exhaust the goroutine stack.
type tarjan struct {
	adj     [][]int
	order   []int // discovery number of each vertex, unvisited until reached
	low     []int // lowest discovery number reachable from the vertex's subtree
	onStack []bool
	stack   []int   // vertices of components not yet closed
	frames  []frame // the depth-first path, innermost last
	found   int     // vertices discovered so far
	out     [][]int // the components found, each a list of vertices
}

// frame is one vertex on the depth-first path and the next edge to follow.
type frame struct {
	vertex int
	next   int
}

// unvisited marks a vertex the search has not reached.
const unvisited = -1

// newTarjan returns a search over adj.
func newTarjan(adj [][]int) *tarjan {
	t := &tarjan{
		adj:     adj,
		order:   make([]int, len(adj)),
		low:     make([]int, len(adj)),
		onStack: make([]bool, len(adj)),
	}
	for v := range t.order {
		t.order[v] = unvisited
	}
	return t
}

// run searches from each vertex in turn and returns the components in the
// order they close. Every vertex is opened once and every edge followed once.
func (t *tarjan) run() [][]int {
	for root := range t.adj {
		if t.order[root] == unvisited {
			t.search(root)
		}
	}
	return t.out
}

// search explores everything reachable from root that is not yet visited.
func (t *tarjan) search(root int) {
	t.open(root)
	for len(t.frames) > 0 {
		top := &t.frames[len(t.frames)-1]
		if top.next == len(t.adj[top.vertex]) {
			t.finish()
			continue
		}
		v, w := top.vertex, t.adj[top.vertex][top.next]
		top.next++
		switch {
		case t.order[w] == unvisited:
			t.open(w)
		case t.onStack[w]:
			t.low[v] = min(t.low[v], t.order[w])
		}
	}
}

// open discovers v and pushes it on the path and on the component stack.
func (t *tarjan) open(v int) {
	t.order[v] = t.found
	t.low[v] = t.found
	t.found++
	t.stack = append(t.stack, v)
	t.onStack[v] = true
	t.frames = append(t.frames, frame{vertex: v})
}

// finish leaves the innermost vertex once its edges are followed. A vertex that
// reaches no earlier one roots a component, which is everything above it on
// the component stack; either way it hands its low link to its parent.
func (t *tarjan) finish() {
	v := t.frames[len(t.frames)-1].vertex
	t.frames = t.frames[:len(t.frames)-1]
	if t.low[v] == t.order[v] {
		t.out = append(t.out, t.pop(v))
	}
	if len(t.frames) > 0 {
		parent := t.frames[len(t.frames)-1].vertex
		t.low[parent] = min(t.low[parent], t.low[v])
	}
}

// pop removes the component rooted at v from the component stack.
func (t *tarjan) pop(v int) []int {
	i := len(t.stack) - 1
	for t.stack[i] != v {
		i--
	}
	members := slices.Clone(t.stack[i:])
	for _, w := range members {
		t.onStack[w] = false
	}
	t.stack = t.stack[:i]
	return members
}
