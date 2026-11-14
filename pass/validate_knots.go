// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// edgeKind says what a type needs another for, so a finding can name it.
type edgeKind uint8

const (
	edgeBase edgeKind = iota
	edgeMixin
	edgeRequired
	edgeElement
	edgeValue
	edgeElementOf // an element of a tuple
	edgeVariant
)

// edgeSet picks which of a type's edges a graph holds.
type edgeSet uint8

const (
	// compositionOnly holds a model's base and mixins and a scalar's base.
	compositionOnly edgeSet = iota
	// needsWithComposition holds everything a type needs for a value.
	needsWithComposition
	// needsAlone holds what a type needs but for its composition, which is told
	// as a cycle of its own where it is one.
	needsAlone
)

// edge is one need of a type: the vertex it needs, and what for.
type edge struct {
	target int32
	kind   edgeKind
	index  int
}

// typeIndex numbers the live types, which liveTypeIDs sorts, so a reference
// finds its vertex by search and no map is built.
type typeIndex struct{ ids []ir.TypeID }

// of returns the vertex a reference to a live type that is not nullable names.
func (x typeIndex) of(ref *ir.TypeRef) (int32, bool) {
	if ref.Nullable {
		return 0, false
	}
	i, ok := slices.BinarySearch(x.ids, ref.Target)
	return int32(i), ok
}

// edgeSink takes the edges eachEdge reads: it counts them, then fills a list
// of their targets, or remembers the first into a set of vertices, so no
// function value is made for a type.
type edgeSink struct {
	count   int32
	targets []int32
	into    []bool
	first   edge
	found   bool
}

func (s *edgeSink) add(e edge) {
	if s.into != nil {
		if !s.found && s.into[e.target] {
			s.first, s.found = e, true
		}
		return
	}
	if s.targets != nil {
		s.targets[s.count] = e.target
	}
	s.count++
}

// emit adds the edge to the type ref names, if it is a live one a value of
// which is needed: a nullable reference leaves a value without it.
func (s *edgeSink) emit(ix typeIndex, ref *ir.TypeRef, kind edgeKind, index int) {
	if i, ok := ix.of(ref); ok {
		s.add(edge{target: i, kind: kind, index: index})
	}
}

// eachEdge adds to sink the edges of td that set picks, in the order a finding
// names the first of: a model's base, its mixins, then its required properties.
// A scalar needs its base, a list its element once it must hold one, a map its
// value likewise, a tuple each element, and a union each variant, none where
// one is nullable or names no live type, since that is a value needing no
// other. docs/ir/models.md lists what is not read.
func eachEdge(td ir.TypeDef, ix typeIndex, set edgeSet, sink *edgeSink) {
	switch t := td.(type) {
	case *ir.Model:
		if set != needsAlone {
			sink.composition(ix, t)
		}
		if set != compositionOnly {
			for j := range t.Properties {
				if t.Properties[j].Required {
					sink.emit(ix, &t.Properties[j].Type, edgeRequired, j)
				}
			}
		}
	case *ir.Scalar:
		if t.Base != nil {
			sink.emit(ix, t.Base, edgeBase, 0)
		}
	case *ir.List:
		if set != compositionOnly && atLeastOne(t.Constraints, func(c *ir.Constraints) *int64 { return c.MinItems }) {
			sink.emit(ix, &t.Elem, edgeElement, 0)
		}
	case *ir.MapT:
		if set != compositionOnly && atLeastOne(t.Constraints, func(c *ir.Constraints) *int64 { return c.MinProps }) {
			sink.emit(ix, &t.Value, edgeValue, 0)
		}
	case *ir.Tuple:
		if set != compositionOnly {
			for j := range t.Elems {
				sink.emit(ix, &t.Elems[j], edgeElementOf, j)
			}
		}
	case *ir.Union:
		if set != compositionOnly && everyVariantNeeded(t, ix) {
			for j := range t.Variants {
				sink.emit(ix, &t.Variants[j].Type, edgeVariant, j)
			}
		}
	default:
		// no other type needs another for a value
	}
}

// composition adds the base and the mixins of m.
func (s *edgeSink) composition(ix typeIndex, m *ir.Model) {
	if m.Base != nil {
		s.emit(ix, m.Base, edgeBase, 0)
	}
	for j := range m.Mixins {
		s.emit(ix, &m.Mixins[j], edgeMixin, j)
	}
}

// atLeastOne reports whether the bound pick reads from c is set and at least 1.
func atLeastOne(c *ir.Constraints, pick func(*ir.Constraints) *int64) bool {
	if c == nil {
		return false
	}
	n := pick(c)
	return n != nil && *n >= 1
}

// everyVariantNeeded reports whether each variant of u is a live type that is
// not nullable: otherwise that variant is a value that needs nothing. A union
// of no variant has none to need, and no edge.
func everyVariantNeeded(u *ir.Union, ix typeIndex) bool {
	for j := range u.Variants {
		if _, ok := ix.of(&u.Variants[j].Type); !ok {
			return false
		}
	}
	return true
}

// knot returns the vertices of a graph that no finite construction reaches.
// Vertex i needs targets[starts[i]:starts[i+1]], all of them where every[i] and
// any one where not. A vertex is founded when it has no need, when every of
// its needs is, or when one is. The unfounded vertices that no other
// unfounded vertex needs are then dropped, one at a time, so what remains lies
// on a cycle of needs, or between two, and not merely before one. Each edge is
// read a constant number of times and the lists are made once.
func knot(n int, starts, targets []int32, every []bool) []bool {
	reverse, from := reversed(n, starts, targets)
	founded := make([]bool, n)
	left := make([]int32, n)
	work := make([]int32, 0, n)
	for i := range n {
		left[i] = starts[i+1] - starts[i]
		if left[i] == 0 {
			founded[i] = true
			work = append(work, int32(i))
		}
	}
	for len(work) > 0 {
		t := work[len(work)-1]
		work = work[:len(work)-1]
		for _, u := range from[reverse[t]:reverse[t+1]] {
			if founded[u] {
				continue
			}
			if left[u]--; !every[u] || left[u] == 0 {
				founded[u] = true
				work = append(work, u)
			}
		}
	}
	return peel(n, starts, targets, founded, work[:0])
}

// reversed returns the edges of the graph turned around: the users of vertex t
// are from[reverse[t]:reverse[t+1]].
func reversed(n int, starts, targets []int32) (reverse, from []int32) {
	reverse = make([]int32, n+1)
	for _, t := range targets {
		reverse[t+1]++
	}
	for i := range n {
		reverse[i+1] += reverse[i]
	}
	from = make([]int32, len(targets))
	next := make([]int32, n)
	copy(next, reverse)
	for i := range n {
		for _, t := range targets[starts[i]:starts[i+1]] {
			from[next[t]] = int32(i)
			next[t]++
		}
	}
	return reverse, from
}

// peel returns the unfounded vertices that remain once each one no remaining
// unfounded vertex needs is dropped. work is scratch for it, empty.
func peel(n int, starts, targets []int32, founded []bool, work []int32) []bool {
	needed := make([]int32, n)
	for i := range n {
		if founded[i] {
			continue
		}
		for _, t := range targets[starts[i]:starts[i+1]] {
			if !founded[t] {
				needed[t]++
			}
		}
	}
	remains := make([]bool, n)
	for i := range n {
		remains[i] = !founded[i]
		if remains[i] && needed[i] == 0 {
			work = append(work, int32(i))
		}
	}
	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]
		remains[i] = false
		for _, t := range targets[starts[i]:starts[i+1]] {
			if !founded[t] {
				if needed[t]--; needed[t] == 0 {
					work = append(work, t)
				}
			}
		}
	}
	return remains
}

// graph is the edges the types of a document hold, in the form knot reads.
type graph struct {
	starts, targets []int32
}

// buildGraph reads the edges of each of ix's types that set picks, or, for the
// needs, that the types in cyclic leave their composition out of: a type in a
// cycle of composition tells it as one and is not told again as lacking a value.
func buildGraph(doc *ir.Document, ix typeIndex, composition bool, cyclic []bool) graph {
	n := len(ix.ids)
	set := func(i int) edgeSet {
		switch {
		case composition:
			return compositionOnly
		case cyclic[i]:
			return needsAlone
		default:
			return needsWithComposition
		}
	}
	g := graph{starts: make([]int32, n+1)}
	var count edgeSink
	for i, id := range ix.ids {
		eachEdge(doc.Types[id], ix, set(i), &count)
		g.starts[i+1] = count.count
	}
	g.targets = make([]int32, count.count)
	fill := edgeSink{targets: g.targets}
	for i, id := range ix.ids {
		eachEdge(doc.Types[id], ix, set(i), &fill)
	}
	return g
}

// checkKnots reports each type that lies on a cycle of bases and mixins
// (pass/composition-cycle), and each that has no finite value, since each of
// what it needs for one needs it in turn (pass/no-finite-value). Both are
// warnings: the IR permits a composition cycle and a target reads it to its
// fixpoint, so no target is refused (docs/ir/models.md). Each is told at every
// type of the knot, naming the entry that leads on, the composition ones first.
func checkKnots(doc *ir.Document) []ir.Diagnostic {
	ids := liveTypeIDs(doc)
	if len(ids) == 0 {
		return nil
	}
	ix := typeIndex{ids: ids}
	every := make([]bool, len(ids))
	for i, id := range ids {
		_, isUnion := doc.Types[id].(*ir.Union)
		every[i] = !isUnion
	}
	comp := buildGraph(doc, ix, true, nil)
	cyclic := knot(len(ids), comp.starts, comp.targets, every)
	needs := buildGraph(doc, ix, false, cyclic)
	empty := knot(len(ids), needs.starts, needs.targets, every)

	diags := appendKnot(nil, doc, ix, cyclic, compositionOnly, "pass/composition-cycle")
	return appendKnot(diags, doc, ix, empty, needsWithComposition, "pass/no-finite-value")
}

// appendKnot reports each type in knot, naming the first edge of its set that
// leads into the knot. A type of the composition knot leaves its composition
// out of its needs, so it is read as the graph was built.
func appendKnot(diags []ir.Diagnostic, doc *ir.Document, ix typeIndex, knotOf []bool, set edgeSet, code string) []ir.Diagnostic {
	for i, id := range ix.ids {
		if !knotOf[i] {
			continue
		}
		td := doc.Types[id]
		sink := edgeSink{into: knotOf}
		eachEdge(td, ix, set, &sink)
		var message string
		if set == compositionOnly {
			message = "composition never ends: " + entryWords(sink.first) + " " + string(ix.ids[sink.first.target]) +
				" leads back into a cycle of bases and mixins"
		} else {
			message = "no finite value: each needs its " + entryWords(sink.first) + ", " + string(ix.ids[sink.first.target]) +
				", which has none either"
		}
		diags = append(diags, diagAt(ir.SeverityWarning, code, message, string(id), td.Common().Provenance))
	}
	return diags
}

// entryWords names what an edge is a need for.
func entryWords(e edge) string {
	switch e.kind {
	case edgeBase:
		return "base"
	case edgeMixin:
		return "mixin " + strconv.Itoa(e.index)
	case edgeRequired:
		return "required property " + strconv.Itoa(e.index)
	case edgeElement:
		return "element"
	case edgeValue:
		return "value"
	case edgeElementOf:
		return "element " + strconv.Itoa(e.index)
	default:
		return "variant " + strconv.Itoa(e.index)
	}
}
