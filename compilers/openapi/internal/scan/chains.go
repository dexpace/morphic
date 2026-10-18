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
// however long the chain: the resolver recurses once per hop of a loop, so any
// loop runs it out of stack (GitHub #922).
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
	// passedBy holds, for each reference, the nodes whose pointer passed
	// through it on the way to its target.
	passedBy map[*yaml.Node][]*yaml.Node
	// maxNodes bounds how many nodes are read: MaxIndexedNodes, which no tree
	// Cycles takes holds, or less in a test of the bound.
	maxNodes int
	// work counts each hop read, each passing pointer weighed and each node
	// settled, so a test can bound the work without timing it.
	work int
}

// chainHop is one node's hop, and how the chain from the node ends.
type chainHop struct {
	// next is the node the pointer lands on. It is nil when the pointer names
	// nothing, or passes through this node itself, which re-enters it on every
	// walk; such a node ends its chain, as does one landing on a node with no
	// $ref.
	next        *yaml.Node
	selfThrough bool
	left        bool // the pointer names a node outside components
	// loop numbers the loop the node is on, from 1; pos is its place there.
	loop, pos int
	out       chainOutcome
}

// settleChains reads and settles every chain the starts reach.
func settleChains(view *nodeview.View, root *yaml.Node, starts []*yaml.Node, maxNodes int) *chainGraph {
	g := &chainGraph{
		view: view, root: root, maxNodes: maxNodes,
		hops: map[*yaml.Node]*chainHop{}, passedBy: map[*yaml.Node][]*yaml.Node{},
	}
	g.read(starts)
	for i, loop := range g.loops() {
		g.settleLoop(i+1, loop)
	}
	g.settleTrees()
	return g
}

// outcome is how the chain from n, a node the starts reach, ends.
func (g *chainGraph) outcome(n *yaml.Node) chainOutcome {
	return g.hops[n].out
}

// read follows the chains from starts, reading each node's hop once. A tree
// Cycles takes is indexed in full, so it holds fewer than maxNodes nodes; one
// past them is a scan bug, and recoverCycleScan reports its panic.
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
// through: arriving at a node on the chain is a cycle, which speakeasy reports
// itself. A pointer that names nothing has no destination, so every node it
// reached is passed through, which is the case that hangs.
func (g *chainGraph) readHop(n *yaml.Node, ref jsontext.Pointer) *chainHop {
	g.work++
	h := &chainHop{left: !componentsRef(ref)}
	path, complete := g.view.PointerPath(g.root, ref)
	through := path
	if complete {
		through = path[:len(path)-1]
	}
	for _, p := range through {
		if _, isRef := g.view.PureRefTarget(p); !isRef {
			continue
		}
		h.selfThrough = h.selfThrough || p == n
		g.passedBy[p] = append(g.passedBy[p], n)
	}
	if complete && !h.selfThrough {
		h.next = path[len(path)-1]
	}
	return h
}

// loops returns each loop among the read nodes, from the node of it met first.
// A node is followed once, so this is linear in the nodes.
func (g *chainGraph) loops() [][]*yaml.Node {
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

// settleLoop settles loop number id. A walk from one of its nodes goes once
// round, re-entering if a hop passes through a node it already followed. first
// says, for each node, how far round from loop[0] the walk from it first meets
// a pass through it; a walk from loop[i] re-enters when some node it follows
// before coming round is passed within that turn, which a least first over the
// loop read twice answers for every start at once.
func (g *chainGraph) settleLoop(id int, loop []*yaml.Node) {
	n := len(loop)
	left := false
	for i, node := range loop {
		h := g.hops[node]
		h.loop, h.pos = id, i
		left = left || h.left
	}
	first := make([]int, n)
	for i, node := range loop {
		first[i] = i + g.passOnLoop(node, i, id, n)
	}
	suffix := slices.Clone(first)
	for i := n - 2; i >= 0; i-- {
		suffix[i] = min(suffix[i], suffix[i+1])
	}
	wrapped := 2 * n // the least first[0:i], a turn later
	for i, node := range loop {
		g.work++
		h := g.hops[node]
		h.out = chainOutcome{verdict: chainCycles, leftComponents: left}
		if min(suffix[i], wrapped) < i+n {
			h.out = chainOutcome{verdict: chainReenters}
		}
		wrapped = min(wrapped, first[i]+n)
	}
}

// passOnLoop is how many hops after node, at index i of loop id of length n, a
// walk from node first meets a pointer passing through it; n when no hop on
// the loop passes through it.
func (g *chainGraph) passOnLoop(node *yaml.Node, i, id, n int) int {
	first := n
	for _, by := range g.passedBy[node] {
		g.work++
		if h := g.hops[by]; h != nil && h.loop == id {
			first = min(first, (h.pos-i+n)%n)
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
			g.settleEnd(h)
		default:
			continue // settled outward from where its chain ends
		}
		g.settleFrom(n, landing)
	}
}

// settleEnd settles a node that ends its chain: its pointer passes through
// the node itself, names nothing, or lands on a node with no $ref.
func (g *chainGraph) settleEnd(h *chainHop) {
	g.work++
	h.out = chainOutcome{verdict: chainTerminates}
	if h.selfThrough {
		h.out.verdict = chainReenters
	}
}

// settleFrom settles every node whose chain reaches root, a settled node on a
// loop or ending its chain, outward from it. between holds the nodes from root
// to the one being settled, which are the rest of that node's chain.
func (g *chainGraph) settleFrom(root *yaml.Node, landing map[*yaml.Node][]*yaml.Node) {
	type visit struct {
		n     *yaml.Node
		leave bool
	}
	between := map[*yaml.Node]bool{}
	stack := []visit{{n: root}}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v.leave {
			delete(between, v.n)
			continue
		}
		if v.n != root {
			g.settleTail(v.n, between, g.hops[root])
		}
		between[v.n] = true
		stack = append(stack, visit{n: v.n, leave: true})
		for _, from := range landing[v.n] {
			stack = append(stack, visit{n: from})
		}
	}
}

// settleTail settles n once the node it lands on is settled. n's chain is that
// node's after n's own hop, so it ends the same way unless a hop on it passes
// through n.
func (g *chainGraph) settleTail(n *yaml.Node, between map[*yaml.Node]bool, root *chainHop) {
	g.work++
	h := g.hops[n]
	h.out = g.hops[h.next].out
	if h.out.verdict == chainCycles {
		h.out.leftComponents = h.out.leftComponents || h.left
	}
	if g.passedOnChain(n, between, root) {
		h.out = chainOutcome{verdict: chainReenters}
	}
}

// passedOnChain reports whether a hop on the chain after n passes through n:
// the hop of a node between n and root, or of one on root's loop.
func (g *chainGraph) passedOnChain(n *yaml.Node, between map[*yaml.Node]bool, root *chainHop) bool {
	for _, by := range g.passedBy[n] {
		g.work++
		if between[by] {
			return true
		}
		if h := g.hops[by]; h != nil && root.loop != 0 && h.loop == root.loop {
			return true
		}
	}
	return false
}
