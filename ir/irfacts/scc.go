package irfacts

import "slices"

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
// order they close, which puts a component after every one its edges reach.
// Every vertex is opened once and every edge followed once.
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
