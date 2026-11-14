// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

// tarjan finds the strongly connected components of a directed graph given as
// adjacency lists, with Tarjan's algorithm run on an explicit stack: a chain of
// types as long as a registry must not exhaust the goroutine stack. Both the
// subtype relation and the type graph are condensed with it.
type tarjan struct {
	adj     [][]int
	order   []int // discovery number of each vertex, unvisited until reached
	low     []int // lowest discovery number reachable from the vertex's subtree
	onStack []bool
	stack   []int   // vertices of components not yet closed
	frames  []frame // the depth-first path, innermost last
	found   int     // vertices discovered so far
	// members holds the vertices of the components found, back to back, and
	// ends where each component's end in it.
	members []int
	ends    []int
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
		members: make([]int, 0, len(adj)),
		ends:    make([]int, 0, len(adj)),
	}
	for v := range t.order {
		t.order[v] = unvisited
	}
	return t
}

// run searches from each vertex in turn and returns the components in the
// order they close, which puts a component after every one its edges reach.
// Every vertex is opened once and every edge followed once. Each component is
// a slice of one backing array, so the search allocates per index and not per
// component.
func (t *tarjan) run() [][]int {
	for root := range t.adj {
		if t.order[root] == unvisited {
			t.search(root)
		}
	}
	out := make([][]int, len(t.ends))
	start := 0
	for c, end := range t.ends {
		out[c] = t.members[start:end:end]
		start = end
	}
	return out
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
		t.pop(v)
	}
	if len(t.frames) > 0 {
		parent := t.frames[len(t.frames)-1].vertex
		t.low[parent] = min(t.low[parent], t.low[v])
	}
}

// pop moves the component rooted at v from the component stack to members.
func (t *tarjan) pop(v int) {
	i := len(t.stack) - 1
	for t.stack[i] != v {
		i--
	}
	for _, w := range t.stack[i:] {
		t.onStack[w] = false
	}
	t.members = append(t.members, t.stack[i:]...)
	t.ends = append(t.ends, len(t.members))
	t.stack = t.stack[:i]
}
