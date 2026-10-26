package nodeview

import (
	"context"
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// Step is what a Walk's visitor asks of the node it was handed.
type Step uint8

const (
	// Descend visits the node's children next.
	Descend Step = iota
	// Prune leaves the node's children unvisited.
	Prune
	// Stop ends the walk.
	Stop
)

// Aliases says what a Walk takes an alias node for.
type Aliases uint8

const (
	// AliasLeaf takes an alias for a node of its own with no children: what it
	// names is written elsewhere in the tree and visited there, so the walk
	// visits each node written once.
	AliasLeaf Aliases = iota
	// AliasTarget visits what an alias names in its place, every time, as the
	// parser's model builds a copy of it at each alias. An alias whose target
	// is nil is visited as a leaf.
	AliasTarget
)

// Walk is a bounded, iterative walk over a raw YAML tree as written, through
// Content and in document order, visiting each node before its children. A
// reader that visits every node written runs one rather than keeping a stack
// and a bound of its own, so a cut short walk is reported rather than silent
// wherever one runs. A walk addressing positions as the parser reads them,
// where a merged pair's value is no child of the mapping merging it, keeps a
// frame per position instead.
type Walk struct {
	// Nodes bounds how many nodes the walk visits. A walk that would visit
	// more stops there and reports itself Cut; a bound below one visits none.
	Nodes int
	// Depth, when above zero, bounds how deep the walk visits, the root at
	// depth zero: a node deeper is not visited, and the walk is Cut. Nodes
	// bounds the depth either way.
	Depth int
	// Aliases says what an alias stands for.
	Aliases Aliases
	// Visit is handed each node and its depth, and says whether to descend.
	// Unset, the walk descends everywhere, which counts a tree's nodes.
	Visit func(n *yaml.Node, depth int) Step
	// Leave, when set, is handed each node Visit descended into once its
	// children have all been visited, or at once for a node holding none.
	Leave func(n *yaml.Node, depth int)
}

// Outcome is how a walk ended.
type Outcome struct {
	// Visited counts the nodes handed to Visit.
	Visited int
	// Cut reports that a bound, or the walk's context, left nodes unvisited,
	// so whatever the walk answers is partial.
	Cut bool
	// Stopped reports that Visit ended the walk.
	Stopped bool
}

// walkFrame is one entry of a walk's stack: a node to visit, or the leaving of
// one whose children have been pushed.
type walkFrame struct {
	n     *yaml.Node
	depth int
	leave bool
}

// AskEvery is how many nodes a walk of a tree visits between two asks of its
// context: a node costs a few nanoseconds, so asking at each would be most of a
// walk's work, and a stretch this long still ends within microseconds. The walks
// that do not go through Walk ask at the same pace.
const AskEvery = 256

// Run walks the tree under root. A nil root, or a nil node in a Content list,
// which no parse produces, is not visited. Once ctx is done the walk stops,
// within AskEvery nodes, and reports itself Cut.
func (w Walk) Run(ctx context.Context, root *yaml.Node) Outcome {
	var out Outcome
	stack := []walkFrame{{n: root}}
	for len(stack) > 0 {
		if out.Visited%AskEvery == 0 && ctx.Err() != nil {
			out.Cut = true
			return out
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.leave {
			w.Leave(f.n, f.depth)
			continue
		}
		n := w.node(f.n)
		if n == nil {
			continue
		}
		if out.Visited >= w.Nodes {
			out.Cut = true
			return out
		}
		if w.Depth > 0 && f.depth > w.Depth {
			out.Cut = true
			continue
		}
		out.Visited++
		switch w.step(n, f.depth) {
		case Stop:
			out.Stopped = true
			return out
		case Prune:
			continue
		case Descend:
		}
		stack = w.push(stack, n, f.depth)
	}
	return out
}

// step asks Visit what to do with n, descending where no visitor is set.
func (w Walk) step(n *yaml.Node, depth int) Step {
	if w.Visit == nil {
		return Descend
	}
	return w.Visit(n, depth)
}

// node returns what the walk visits for n: n itself, or the node an alias
// names where aliases stand for their targets.
func (w Walk) node(n *yaml.Node) *yaml.Node {
	if w.Aliases != AliasTarget {
		return n
	}
	if target := Deref(n); target != nil {
		return target
	}
	return n
}

// push returns stack with the leaving of n and then its children pushed, so
// the first child is visited next. An alias visited as a leaf holds no
// children: yaml.v3 keeps its target in Alias, not in Content.
func (w Walk) push(stack []walkFrame, n *yaml.Node, depth int) []walkFrame {
	if w.Leave != nil {
		stack = append(stack, walkFrame{n: n, depth: depth, leave: true})
	}
	for _, c := range slices.Backward(n.Content) {
		stack = append(stack, walkFrame{n: c, depth: depth + 1})
	}
	return stack
}
