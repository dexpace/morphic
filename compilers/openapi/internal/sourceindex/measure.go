package sourceindex

import (
	"math"

	yaml "gopkg.in/yaml.v3"
)

// Measure takes how deeply a subtree nests: the mappings and sequences its
// deepest path passes through, an alias standing for the node it names, as in
// the tree the parser builds, whose cost per node grows with depth (GitHub
// #905).
//
// A `<<` merge key's value counts where it is written, a level below the
// mapping the parser folds it into, two for a list, so the measure never
// falls short of the parser's. An alias back into a node still being measured
// counts 0. What it measures it keeps, for the one tree it belongs to.
type Measure struct {
	memo map[*yaml.Node]int64
	// inFlight holds the nodes Of has expanded and not yet measured. A call that
	// finishes leaves it empty, so every call shares it.
	inFlight map[*yaml.Node]bool
	// limit bounds the steps every Of together takes. A step expands a node,
	// once per node, or drops a frame, and a frame is pushed once per call and
	// once per child or alias target an expansion reaches. A tree of n nodes
	// has fewer than n of each, so a tree within the caller's node bound,
	// measured with at most one call per node, takes fewer than 4n steps.
	limit, steps int64
	truncated    bool
}

// NewMeasure returns a measure for a tree of at most maxNodes nodes, a bound
// so large its limit would overflow being taken as no bound at all.
func NewMeasure(maxNodes int64) *Measure {
	limit := int64(math.MaxInt64)
	if maxNodes < limit/4 {
		limit = 4 * max(maxNodes, 0)
	}
	return &Measure{memo: map[*yaml.Node]int64{}, inFlight: map[*yaml.Node]bool{}, limit: limit}
}

// measureFrame is one entry of Of's post-order stack: a node, and whether
// what it nests over has been pushed already.
type measureFrame struct {
	n        *yaml.Node
	expanded bool
}

// Of is the nesting of the subtree under n, and 0 for nil. Past the measure's
// limit it stops, answers in part and reports Truncated.
//
// The walk is iterative, so a deep tree costs no stack. A node is pushed only
// while neither measured nor in flight, so each is expanded once whatever
// shares it, and an alias cycle ends the walk rather than looping it: one
// below the anchor-cycle check's tracked depth, or in a tree built by hand.
func (m *Measure) Of(n *yaml.Node) int64 {
	if n == nil {
		return 0
	}
	stack := []*measureFrame{{n: n}}
	for len(stack) > 0 {
		if m.steps >= m.limit {
			m.truncated = true
			break
		}
		m.steps++
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
		m.memo[top.n] = m.nestingOf(top.n)
		stack = stack[:len(stack)-1]
	}
	return m.memo[n]
}

// Truncated reports whether a call to Of stopped at the measure's limit, so
// that some answer it gave is partial.
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

// nestingOf is n's nesting from what it nests over, every part of which is
// measured already or in flight; one in flight counts 0, as does nil.
func (m *Measure) nestingOf(n *yaml.Node) int64 {
	if n.Kind == yaml.AliasNode {
		return m.memo[n.Alias]
	}
	var tallest int64
	for _, c := range n.Content {
		tallest = max(tallest, m.memo[c])
	}
	return tallest + Level(n)
}

// Level is how much n itself adds to the nesting it heads: one for a mapping
// or sequence, none for a scalar, an alias or a node of no kind.
func Level(n *yaml.Node) int64 {
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		return 1
	}
	return 0
}
