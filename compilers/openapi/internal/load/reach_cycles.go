package load

import (
	"slices"

	yaml "gopkg.in/yaml.v3"
)

// cycleSearch finds the cycles of the graph reach walks: a depth-first search
// over classes and the sets they land in, from each start in the library's
// walk order, keeping Tarjan's low links on its explicit stack so that it
// reads each strongly connected component whole. A component of more than one
// vertex is a cycle; the graph has no edge from a vertex to itself.
type cycleSearch struct {
	r *reach
	// seen holds what the search knows of each vertex it has entered.
	seen map[vertex]*searched
	// open is Tarjan's stack: the vertices entered whose component is not yet
	// whole.
	open []vertex
	// members holds the references each whole cycle runs through, and first
	// those of the first cycle the search closed: all a budget cutting the
	// search short leaves known.
	members, first []*yaml.Node
}

// searched is one vertex the search entered: its number in the order entered,
// from 1, the least number it reaches back to, the frame it was entered at,
// and whether it is still on Tarjan's stack.
type searched struct {
	order, low, depth int
	open              bool
}

// searchFrame is one vertex on the search's path and the edges it has left.
type searchFrame struct {
	key   vertex
	v     *searched
	sets  []*posSet    // a class's sets not yet entered
	nodes []*yaml.Node // a set's members not yet entered
	via   *yaml.Node   // the set member the frame above was entered through
}

// cycleMembers is the references the cycles closed from starts run through:
// every one of each cycle the search read whole, and those of the first it
// closed. None means no cycle closed, which the budget can cut short too
// (incomplete).
//
// It searches as the depth-first search it replaces did, a step and its
// charge at a time, so it closes its first cycle where that one stopped and
// the budget allows one exactly when it allowed the other. Going on past it
// costs a document that is refused anyway.
func (r *reach) cycleMembers(starts []*yaml.Node) []*yaml.Node {
	s := &cycleSearch{r: r, seen: map[vertex]*searched{}}
	for _, start := range starts {
		s.from(vertex{class: r.classOf(start)})
	}
	return slices.Concat(s.members, s.first)
}

// from searches from v unless an earlier start already entered it. It asks
// the budget before every step, so with nothing left it takes none.
func (s *cycleSearch) from(v vertex) {
	if _, entered := s.seen[v]; entered {
		return
	}
	frames := []searchFrame{s.enter(v, 0)}
	for len(frames) > 0 && s.r.budget.spend(1) {
		top := &frames[len(frames)-1]
		next, ok := s.advance(top)
		if !ok {
			frames = s.leave(frames)
			continue
		}
		known, entered := s.seen[next]
		switch {
		case !entered:
			frames = append(frames, s.enter(next, len(frames)))
		case known.open:
			top.v.low = min(top.v.low, known.order)
			if s.first == nil {
				// Until a cycle closes, every vertex on Tarjan's stack is on the
				// path: one left behind reaches back past itself, through a cycle.
				s.first = cycleThrough(frames[known.depth:])
			}
		}
	}
}

// enter puts v on the search's path at frame depth, numbered next.
func (s *cycleSearch) enter(v vertex, depth int) searchFrame {
	order := len(s.seen) + 1
	known := &searched{order: order, low: order, depth: depth, open: true}
	s.seen[v] = known
	s.open = append(s.open, v)
	if v.set != nil {
		return searchFrame{key: v, v: known, nodes: v.set.members}
	}
	return searchFrame{key: v, v: known, sets: s.r.lookups(v.class)}
}

// advance takes the next edge out of top's vertex, or reports there is none
// left. A set's edge leads through a member to that member's class.
func (s *cycleSearch) advance(top *searchFrame) (vertex, bool) {
	switch {
	case len(top.sets) > 0:
		next := vertex{set: top.sets[0]}
		top.sets = top.sets[1:]
		return next, true
	case len(top.nodes) > 0:
		top.via, top.nodes = top.nodes[0], top.nodes[1:]
		return vertex{class: s.r.classOf(top.via)}, true
	default:
		return vertex{}, false
	}
}

// leave takes the vertex on top off the path once its edges are spent,
// closing its component when it heads one, and returns the path left.
func (s *cycleSearch) leave(frames []searchFrame) []searchFrame {
	top := frames[len(frames)-1]
	frames = frames[:len(frames)-1]
	if top.v.low == top.v.order {
		s.close(top.key)
	}
	if len(frames) > 0 {
		parent := frames[len(frames)-1].v
		parent.low = min(parent.low, top.v.low)
	}
	return frames
}

// close takes the component v heads off Tarjan's stack, and records the
// references a cycle there runs through: each member of a set in it whose own
// class is in it too. A member whose class lies outside only leads out of the
// cycle, as a reference sharing a class but no set leads only into it.
func (s *cycleSearch) close(v vertex) {
	at := len(s.open) - 1
	for s.open[at] != v {
		at-- // everything above v on the stack is in its component, so this costs the component
	}
	component := s.open[at:]
	s.open = s.open[:at]
	in := make(map[vertex]bool, len(component))
	for _, u := range component {
		s.seen[u].open, in[u] = false, true
	}
	if len(component) < 2 {
		return
	}
	for _, u := range component {
		if u.set == nil {
			continue
		}
		for _, m := range u.set.members {
			s.r.budget.spend(1)
			if in[vertex{class: s.r.classOf(m)}] {
				s.members = append(s.members, m)
			}
		}
	}
}

// cycleThrough is the references the search's path runs through from its
// first frame to its last: the member each set frame on it was left through.
func cycleThrough(frames []searchFrame) []*yaml.Node {
	var through []*yaml.Node
	for _, f := range frames {
		if f.key.set != nil {
			through = append(through, f.via)
		}
	}
	return through
}
