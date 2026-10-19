package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// chainGraph settles how the chain from every pure-$ref node ends, reading
// each hop once, so no chain is walked again per start (GitHub #903, #923).
// A loop is found however long: the resolver recurses once per hop of one, so
// any loop runs it out of stack (GitHub #922).
//
// A hop goes where its pointer lands (landing), so every chain ends or runs
// into one loop. A schema $ref the pointer passes is read by its own keys, as
// the resolver reads it (GitHub #932); a reference object it reads through what
// it names, which this does not follow (GitHub #945).
type chainGraph struct {
	view *nodeview.View
	root *yaml.Node
	hops map[*yaml.Node]*chainHop
	// order holds the nodes in the order read, which settling follows.
	order []*yaml.Node
	// loops holds the nodes of each loop, numbered as chainHop.loop numbers it,
	// in the order a walk from the node met first follows them.
	loops [][]*yaml.Node
	// maxNodes bounds how many nodes are read: MaxIndexedNodes, which no tree
	// Cycles takes can reach (see read), or less in a test of the bound.
	maxNodes int
	// work counts each hop read and each node settled, so a test can bound the
	// work without timing it.
	work int
}

// chainHop is one node's hop, and where the chain from the node ends.
type chainHop struct {
	// next is the node the pointer lands on, nil when it names nothing.
	next *yaml.Node
	// loop numbers, from 1, the loop the chain from the node runs into, and is 0
	// for a chain that ends.
	loop int
}

// settleChains reads and settles every chain the starts reach.
func settleChains(view *nodeview.View, root *yaml.Node, starts []*yaml.Node, maxNodes int) *chainGraph {
	g := &chainGraph{view: view, root: root, maxNodes: maxNodes, hops: map[*yaml.Node]*chainHop{}}
	g.read(starts)
	g.settle()
	return g
}

// loopOf numbers the loop the chain from n, a node the starts reach, runs
// into, or is 0 when the chain ends.
func (g *chainGraph) loopOf(n *yaml.Node) int {
	return g.hops[n].loop
}

// loopsReached returns the nodes of every loop a chain from starts runs into,
// each loop once however many chains run into it.
func (g *chainGraph) loopsReached(starts []*yaml.Node) []*yaml.Node {
	reached := map[int]bool{}
	var nodes []*yaml.Node
	for _, start := range starts {
		loop := g.loopOf(start)
		if loop == 0 || reached[loop] {
			continue
		}
		reached[loop] = true
		nodes = append(nodes, g.loops[loop-1]...)
	}
	return nodes
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
		g.work++
		next := landing(g.view, g.root, ref)
		g.hops[n] = &chainHop{next: next}
		g.order = append(g.order, n)
		if next != nil {
			pending = append(pending, next)
		}
	}
}

// landing returns the node the resolver lands ref on, read from root, or nil
// where it lands nowhere. It reads each token as RFC 6901 does, which is
// nodeview.PointerTarget, but a trailing empty token by where the walk has got
// to (emptyTokenLanding): the library reads that one differently in its model
// and in raw YAML (GitHub #944).
func landing(view *nodeview.View, root *yaml.Node, ref jsontext.Pointer) *yaml.Node {
	if before, trailing := strings.CutSuffix(string(ref), "/"); trailing {
		switch emptyTokenLanding(jsontext.Pointer(before)) {
		case landsNowhere:
			return nil
		case landsOnNode:
			path, complete := view.DocumentPath(root, jsontext.Pointer(before))
			if !complete {
				return nil
			}
			return path[len(path)-1]
		default: // landsOnKey: the member keyed "", which RFC 6901 names
		}
	}
	next, _ := view.PointerTarget(root, ref)
	return next
}

// emptyLanding is where the resolver lands a pointer's trailing empty token.
type emptyLanding int

const (
	// landsOnKey: on the member keyed "", where the library looks the token up
	// as a key: in a map of schemas, a link's parameters, any map. A list has
	// no such member, nor does the library find one. Outside a schema it is
	// also the reading wherever the scan cannot tell the position's kind
	// (GitHub #949): at an object of the model the library fails the pointer,
	// so a landing only adds a loop through a reference it reports anyway; in
	// raw YAML, where the library stays, a loop can be missed.
	landsOnKey emptyLanding = iota
	// landsNowhere: at a schema, which reads the token as a field no schema
	// has.
	landsNowhere
	// landsOnNode: on the node the walk has reached, under a schema past a key
	// naming no sub-schema, in raw YAML, where the library stays. Where the key
	// names a field of the model holding no schema instead, the library fails
	// the pointer or lands on no schema, either way a reference it reports.
	landsOnNode
)

// emptyTokenLanding returns where the resolver lands an empty token after the
// tokens of before, from the role walkRole.child gives each from the document
// root. A key child reads no further under is known for raw YAML only under a
// schema, whose keys are the model's. Outside one, child skips a key by its
// name alone, so past it the scan does not know where the library's walk is.
func emptyTokenLanding(before jsontext.Pointer) emptyLanding {
	role := roleOutside
	for token := range before.Tokens() {
		next, held := role.child(token)
		switch {
		case held:
			role = next
		case role == roleSchema:
			return landsOnNode
		default:
			return landsOnKey
		}
	}
	if role == roleSchema {
		return landsNowhere
	}
	return landsOnKey
}

// settle numbers the loop each chain runs into, following each node once. A
// walk from a node not yet settled stops where the chain ends, at a settled
// node, whose answer every node walked takes, or back on itself, which is a
// loop first met.
func (g *chainGraph) settle() {
	// 0 for a node not yet walked, -1 for one settled, and i+1 for the node at
	// index i of the walk under way.
	state := make(map[*yaml.Node]int, len(g.order))
	for _, start := range g.order {
		var path []*yaml.Node
		cur := start
		for h := g.hops[cur]; h != nil && state[cur] == 0; h = g.hops[cur] {
			path = append(path, cur)
			state[cur] = len(path)
			cur = h.next
		}
		loop := 0
		if i := state[cur]; i > 0 {
			g.loops = append(g.loops, path[i-1:])
			loop = len(g.loops)
		} else if h := g.hops[cur]; h != nil {
			loop = h.loop
		}
		for _, n := range path {
			g.work++
			g.hops[n].loop = loop
			state[n] = -1
		}
	}
}
