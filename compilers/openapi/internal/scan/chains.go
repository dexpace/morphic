package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// chainGraph settles how the chain from every pure-$ref node ends, reading
// each hop once and settling outward from each chain's end, so no chain is
// walked again per start (GitHub #903, #923, #924). The verdict is a walk's
// however long the chain: the resolver overflows its stack on a schema loop of
// any length (docs/parser-library.md#schema-cycle-overflow, GitHub #922).
//
// A pointer passing through a node the walk followed always comes before the
// chain's end, so a node re-enters when a hop on its own chain passes through
// it or the node it lands on re-enters, and otherwise ends as that node does.
type chainGraph struct {
	view *nodeview.View
	root *yaml.Node
	hops map[*yaml.Node]*chainHop
	// order holds the nodes in the order read, which every pass follows.
	order []*yaml.Node
	// passedBy holds, for each reference, the passes through it on the way to
	// another hop's target.
	passedBy map[*yaml.Node][]pass
	// loops holds the nodes of each loop, numbered as chainHop.loop numbers it,
	// from the node met first, in the order its walk follows them.
	loops [][]*yaml.Node
	// maxNodes bounds how many nodes are read: MaxIndexedNodes, which no tree
	// Cycles takes can reach (see read), or less in a test of the bound.
	maxNodes int
	// work counts each hop read, each passing pointer weighed and each node
	// settled, so a test can bound the work without timing it.
	work int
}

// pass is one hop's pointer passing through a reference: the node whose hop it
// is, and where along that pointer's path the reference lies, which is the
// order the resolver reaches the references one hop passes.
type pass struct {
	by *yaml.Node
	at int
}

// chainHop is one node's hop, and how the chain from the node ends.
type chainHop struct {
	// next is the node the pointer lands on. It is nil when the pointer names
	// nothing, or passes through this node itself, which re-enters it on every
	// walk; such a node ends its chain, as does one landing on a node with no
	// $ref. selfAt is where along the pointer's path it passes this node.
	next        *yaml.Node
	selfThrough bool
	selfAt      int
	left        bool // the pointer names a node outside components
	// loop numbers the loop the node is on, from 1; pos is its place there.
	loop, pos int
	out       chainOutcome
	// end numbers the loop the chain cycles in, for chainCycles; first is where
	// the walk re-enters first, for chainReenters.
	end   int
	first reentry
}

// reentry is a pass, on one walk, through a node already on it: the hop that
// makes it, counted from the walk's start, where along that hop's pointer the
// node lies, and the node.
type reentry struct {
	hop, at int
	node    *yaml.Node
}

// before reports whether r comes before o on one walk: at an earlier hop, or
// earlier along one hop's pointer, where the resolver reaches a reference
// sooner.
func (r reentry) before(o reentry) bool {
	return r.hop < o.hop || (r.hop == o.hop && r.at < o.at)
}

// settleChains reads and settles every chain the starts reach.
func settleChains(view *nodeview.View, root *yaml.Node, starts []*yaml.Node, maxNodes int) *chainGraph {
	g := &chainGraph{
		view: view, root: root, maxNodes: maxNodes,
		hops: map[*yaml.Node]*chainHop{}, passedBy: map[*yaml.Node][]pass{},
	}
	g.read(starts)
	g.loops = g.findLoops()
	for i, loop := range g.loops {
		g.settleLoop(i+1, loop)
	}
	g.settleTrees()
	return g
}

// outcome is how the chain from n, a node the starts reach, ends.
func (g *chainGraph) outcome(n *yaml.Node) chainOutcome {
	return g.hops[n].out
}

// failsOn is what the chain from n, a node the starts reach, fails on: the
// loop it cycles in, by number, and that loop's nodes; or, when it re-enters,
// no loop and the node it first passes through again, where the resolver
// re-enters the chain. A chain that ends fails on nothing.
func (g *chainGraph) failsOn(n *yaml.Node) (int, []*yaml.Node) {
	h := g.hops[n]
	switch h.out.verdict {
	case chainCycles:
		return h.end, g.loops[h.end-1]
	case chainReenters:
		return 0, []*yaml.Node{h.first.node}
	default:
		return 0, nil
	}
}

// read follows the chains from starts, reading each node's hop once. Each node
// read is a $ref mapping holding a key and a value of its own, and Cycles takes
// only a tree indexed in full within MaxIndexedNodes, so the reads stay far
// below it. Reading past maxNodes is a scan bug; recoverCycleScan reports its
// panic.
func (g *chainGraph) read(starts []*yaml.Node) {
	pending := slices.Clone(starts)
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, read := g.hops[n]; read {
			continue
		}
		ref, ok := g.view.PureRefTarget(n)
		if !ok {
			continue // no top-level $ref: where the resolver stops
		}
		if len(g.order) == g.maxNodes {
			panic(fmt.Sprintf("cycle scan: $ref chains outgrew their %d-node bound", g.maxNodes))
		}
		h := g.readHop(n, ref)
		g.hops[n] = h
		g.order = append(g.order, n)
		if h.next != nil {
			pending = append(pending, h.next)
		}
	}
}

// readHop walks n's pointer ref as the resolver does and records each
// reference it passes through on the way. The destination is not passed
// through: arriving at a node on the chain is a cycle (chainCycles), not a
// re-entry. A pointer that names nothing has no destination, so every node it
// reached is passed through, which is the re-entry the scan refuses
// (docs/parser-library.md#reference-object-cycle).
func (g *chainGraph) readHop(n *yaml.Node, ref jsontext.Pointer) *chainHop {
	g.work++
	h := &chainHop{left: !componentsRef(ref)}
	path, complete := g.view.PointerPath(g.root, ref)
	through := path
	if complete {
		through = path[:len(path)-1]
	}
	for at, p := range through {
		if _, isRef := g.view.PureRefTarget(p); !isRef {
			continue
		}
		if p == n && !h.selfThrough {
			h.selfThrough, h.selfAt = true, at
		}
		g.passedBy[p] = append(g.passedBy[p], pass{by: n, at: at})
	}
	if complete && !h.selfThrough {
		h.next = path[len(path)-1]
	}
	return h
}

// findLoops returns each loop among the read nodes, from the node of it met
// first. A node is followed once, so this is linear in the nodes.
func (g *chainGraph) findLoops() [][]*yaml.Node {
	// 0 for a node not yet followed, -1 for one done, and i+1 for the node at
	// index i of the path being followed.
	state := make(map[*yaml.Node]int, len(g.order))
	var loops [][]*yaml.Node
	for _, start := range g.order {
		var path []*yaml.Node
		cur := start
		for h := g.hops[cur]; h != nil && state[cur] == 0; h = g.hops[cur] {
			path = append(path, cur)
			state[cur] = len(path)
			cur = h.next
		}
		if i := state[cur]; i > 0 {
			loops = append(loops, path[i-1:])
		}
		for _, n := range path {
			state[n] = -1
		}
	}
	return loops
}

// settleLoop settles loop number id. A walk from loop[i] goes once round, and
// re-enters if a node it follows is passed through before the walk comes back.
// first[j] is where, counted round from loop[0], the walk from loop[j] first
// meets a pass through loop[j]. The walk from loop[i] follows loop[i:] and then
// loop[:i] a turn later, so it re-enters at the least of those firsts when that
// falls before i+n.
func (g *chainGraph) settleLoop(id int, loop []*yaml.Node) {
	n := len(loop)
	left := false
	for i, node := range loop {
		h := g.hops[node]
		h.loop, h.pos = id, i
		left = left || h.left
	}
	first := make([]reentry, n)
	for i, node := range loop {
		first[i] = g.passOnLoop(node, i, id, n)
	}
	suffix := slices.Clone(first)
	for i := n - 2; i >= 0; i-- {
		if suffix[i+1].before(suffix[i]) {
			suffix[i] = suffix[i+1]
		}
	}
	wrapped := reentry{hop: 2 * n} // the least of first[0:i], a turn later
	for i, node := range loop {
		g.work++
		h := g.hops[node]
		h.out, h.end = chainOutcome{verdict: chainCycles, leftComponents: left}, id
		least := suffix[i]
		if wrapped.before(least) {
			least = wrapped
		}
		if least.hop < i+n {
			h.out, h.end = chainOutcome{verdict: chainReenters}, 0
			h.first = reentry{hop: least.hop - i, at: least.at, node: least.node}
		}
		if turned := (reentry{hop: first[i].hop + n, at: first[i].at, node: first[i].node}); turned.before(wrapped) {
			wrapped = turned
		}
	}
}

// passOnLoop is the first pass through node, at index i of loop id of length
// n, on a walk from node: its hop counted round from loop[0], i+n when no hop
// on the loop passes through node.
func (g *chainGraph) passOnLoop(node *yaml.Node, i, id, n int) reentry {
	first := reentry{hop: i + n, node: node}
	for _, p := range g.passedBy[node] {
		g.work++
		h := g.hops[p.by]
		if h == nil || h.loop != id {
			continue
		}
		if r := (reentry{hop: i + (h.pos-i+n)%n, at: p.at, node: node}); r.before(first) {
			first = r
		}
	}
	return first
}

// settleTrees settles every node off a loop, outward from where its chain
// ends: a loop, or a node ending it, settled before the nodes landing on it.
func (g *chainGraph) settleTrees() {
	landing := map[*yaml.Node][]*yaml.Node{}
	for _, n := range g.order {
		if h := g.hops[n]; h.loop == 0 && g.hops[h.next] != nil {
			landing[h.next] = append(landing[h.next], n)
		}
	}
	for _, n := range g.order {
		h := g.hops[n]
		switch {
		case h.loop != 0:
		case g.hops[h.next] == nil:
			g.settleEnd(n, h)
		default:
			continue // settled outward from where its chain ends
		}
		g.settleFrom(n, landing)
	}
}

// settleEnd settles n, a node that ends its chain: its pointer passes through
// n itself, names nothing, or lands on a node with no $ref.
func (g *chainGraph) settleEnd(n *yaml.Node, h *chainHop) {
	g.work++
	h.out = chainOutcome{verdict: chainTerminates}
	if h.selfThrough {
		h.out.verdict = chainReenters
		h.first = reentry{at: h.selfAt, node: n}
	}
}

// settleFrom settles every node whose chain reaches root, a settled node on a
// loop or ending its chain, outward from it. between holds the nodes from root
// up to the one being settled, not including it: the rest of that node's
// chain, each with its distance from root.
func (g *chainGraph) settleFrom(root *yaml.Node, landing map[*yaml.Node][]*yaml.Node) {
	type visit struct {
		n     *yaml.Node
		dist  int
		leave bool
	}
	between := map[*yaml.Node]int{}
	stack := []visit{{n: root}}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v.leave {
			delete(between, v.n)
			continue
		}
		if v.n != root {
			g.settleTail(v.n, v.dist, between, g.hops[root])
		}
		between[v.n] = v.dist
		stack = append(stack, visit{n: v.n, leave: true})
		for _, from := range landing[v.n] {
			stack = append(stack, visit{n: from, dist: v.dist + 1})
		}
	}
}

// settleTail settles n, dist hops from root, once the node it lands on is
// settled. n's chain is that node's after n's own hop, so it ends the same way
// unless a hop on it passes through n first.
func (g *chainGraph) settleTail(n *yaml.Node, dist int, between map[*yaml.Node]int, root *chainHop) {
	g.work++
	h := g.hops[n]
	next := g.hops[h.next]
	h.out, h.end, h.first = next.out, next.end, next.first
	h.first.hop++
	if h.out.verdict == chainCycles {
		h.out.leftComponents = h.out.leftComponents || h.left
	}
	own, passed := g.passOnChain(n, dist, between, root)
	if passed && (h.out.verdict != chainReenters || own.before(h.first)) {
		h.out, h.end, h.first = chainOutcome{verdict: chainReenters}, 0, own
	}
}

// passOnChain is the first pass through n on the walk from n, dist hops from
// root, and whether there is one: by the hop of a node between n and root, or
// of one on root's loop.
func (g *chainGraph) passOnChain(n *yaml.Node, dist int, between map[*yaml.Node]int, root *chainHop) (reentry, bool) {
	var first reentry
	passed := false
	for _, p := range g.passedBy[n] {
		g.work++
		hop, on := g.hopOnChain(p.by, dist, between, root)
		if !on {
			continue
		}
		if r := (reentry{hop: hop, at: p.at, node: n}); !passed || r.before(first) {
			first, passed = r, true
		}
	}
	return first, passed
}

// hopOnChain is how far into the walk from a node dist hops from root the hop
// of by comes, when it is on that walk at all: by between the node and root, or
// on root's loop, which the walk goes round once from root.
func (g *chainGraph) hopOnChain(by *yaml.Node, dist int, between map[*yaml.Node]int, root *chainHop) (int, bool) {
	if d, on := between[by]; on {
		return dist - d, true
	}
	h := g.hops[by]
	if h == nil || root.loop == 0 || h.loop != root.loop {
		return 0, false
	}
	n := len(g.loops[root.loop-1])
	return dist + (h.pos-root.pos+n)%n, true
}
