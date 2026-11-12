// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package sourceindex

import (
	"context"
	"math"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// Measure takes how deeply a subtree nests: the mappings and sequences its
// deepest path passes through, an alias standing for the node it names, as in
// the tree the parser builds, whose cost per node grows with depth (GitHub
// #905).
//
// A `<<` merge key's value counts where it is written, a level below the
// mapping the parser folds it into, two for a list, so the measure never
// falls short of the parser's. An alias back into a node still being measured
// counts 0. What it measures (shape) it keeps, for the one tree it belongs to.
type Measure struct {
	memo map[*yaml.Node]shape
	// inFlight holds the nodes Of has expanded and not yet measured. A call that
	// finishes leaves it empty, so every call shares it.
	inFlight map[*yaml.Node]bool
	// steps bounds the steps every Of together takes. A step expands a node,
	// once per node, or drops a frame, and a frame is pushed once per call and
	// once per child or alias target an expansion reaches. A tree of n nodes
	// has fewer than n of each, so a tree within the caller's node bound,
	// measured with at most one call per node, takes fewer than 4n steps.
	steps     budget.Budget
	truncated bool
}

// NewMeasure returns a measure for a tree of at most maxNodes nodes, a bound
// so large four steps a node would overflow being taken as no bound at all.
func NewMeasure(maxNodes int) *Measure {
	steps := math.MaxInt
	if maxNodes < steps/4 {
		steps = 4 * max(maxNodes, 0)
	}
	return &Measure{memo: map[*yaml.Node]shape{}, inFlight: map[*yaml.Node]bool{},
		steps: budget.New(budget.AtMost(steps))}
}

// measureFrame is one entry of Of's post-order stack: a node, and whether
// what it nests over has been pushed already.
type measureFrame struct {
	n        *yaml.Node
	expanded bool
}

// Of is the nesting of the subtree under n, and 0 for nil. Past the measure's
// limit, or once ctx is done, it stops, answers in part and reports Truncated.
// It asks ctx every nodeview.AskEvery steps, counted across calls.
//
// The walk is iterative, so a deep tree costs no stack. A node is pushed only
// while neither measured nor in flight, so each is expanded once whatever
// shares it, and an alias cycle ends the walk rather than looping it: one
// below the anchor-cycle check's tracked depth, or in a tree built by hand.
func (m *Measure) Of(ctx context.Context, n *yaml.Node) int {
	return m.shapeOf(ctx, n).nesting
}

// shapeOf is the shape of the subtree under n, measured as Of measures it.
func (m *Measure) shapeOf(ctx context.Context, n *yaml.Node) shape {
	if n == nil {
		return shape{}
	}
	stack := []*measureFrame{{n: n}}
	for len(stack) > 0 {
		if m.steps.Spent()%nodeview.AskEvery == 0 && ctx.Err() != nil || !m.steps.Spend(1) {
			m.truncated = true
			break
		}
		top := stack[len(stack)-1]
		if _, done := m.memo[top.n]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if !top.expanded {
			top.expanded = true
			m.inFlight[top.n] = true
			stack = m.push(stack, top.n)
			continue
		}
		delete(m.inFlight, top.n)
		m.memo[top.n] = m.measured(top.n)
		stack = stack[:len(stack)-1]
	}
	return m.memo[n]
}

// Truncated reports whether a call to Of stopped at the measure's limit or its
// context, so that some answer it gave is partial.
func (m *Measure) Truncated() bool { return m.truncated }

// push adds what n nests over to the stack: an alias's target, or its
// children. One already measured, or in flight on the walk, is left off.
func (m *Measure) push(stack []*measureFrame, n *yaml.Node) []*measureFrame {
	over := n.Content
	if n.Kind == yaml.AliasNode {
		over = []*yaml.Node{n.Alias}
	}
	for _, c := range over {
		if _, done := m.memo[c]; c == nil || done || m.inFlight[c] {
			continue
		}
		stack = append(stack, &measureFrame{n: c})
	}
	return stack
}

// measured is n's shape from what it nests over, every part of which is
// measured already or in flight; one in flight counts as nothing, as does nil.
func (m *Measure) measured(n *yaml.Node) shape {
	if n.Kind == yaml.AliasNode {
		return m.memo[n.Alias]
	}
	out := shape{nodes: 1}
	for i, c := range n.Content {
		out = out.holding(m.memo[c], edge(n, i))
	}
	out.nesting += Level(n)
	return out
}

// shape is what a subtree, its aliases expanded, is to a compile reading it:
// how deeply it nests, how many nodes it holds, and how many bytes the JSON
// pointers naming them from the subtree's root total, each a key or an index
// and a '/' per level, escapes not counted. The counts saturate at math.MaxInt.
type shape struct {
	nesting, nodes, pointers int
}

// holding is s having taken child as one more child, under a pointer edge
// bytes longer than its own: the child nests beside the others, and the pointer
// to each of its nodes from s's root is edge bytes longer than from its own.
func (s shape) holding(child shape, edge int) shape {
	s.nesting = max(s.nesting, child.nesting)
	s.nodes = sum(s.nodes, child.nodes)
	s.pointers = sum(s.pointers, sum(product(child.nodes, edge), child.pointers))
	return s
}

// edge is how many bytes a pointer grows by from n to the child at index i of
// its Content: the key of the pair that child belongs to in a mapping, its
// index in a sequence, each with a '/', and nothing anywhere else.
func edge(n *yaml.Node, i int) int {
	switch n.Kind {
	case yaml.MappingNode:
		if key := nodeview.Deref(n.Content[i&^1]); key != nil {
			return len(key.Value) + 1
		}
		return 1
	case yaml.SequenceNode:
		return digits(i) + 1
	default:
		return 0
	}
}

// digits is how many decimal digits i, which is not negative, is written in.
func digits(i int) int {
	n := 1
	for ; i >= 10; i /= 10 {
		n++
	}
	return n
}

// sum is a+b for counts that are not negative, saturating at math.MaxInt.
func sum(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

// product is a*b for counts that are not negative, saturating at math.MaxInt.
func product(a, b int) int {
	if a != 0 && b > math.MaxInt/a {
		return math.MaxInt
	}
	return a * b
}

// Level is how much n itself adds to the nesting it heads: one for a mapping
// or sequence, none for a scalar, an alias or a node of no kind.
func Level(n *yaml.Node) int {
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		return 1
	}
	return 0
}
