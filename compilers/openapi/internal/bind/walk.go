package bind

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// root is a position a walk for reference sites starts from, and the state
// it is read at: the source's root, a position a hop lands on, read as a
// schema, or what a reference object resolved to, read as its kind.
type root struct {
	at    Position
	state state
}

// frame is one position a walk has yet to read.
type frame struct {
	node  *yaml.Node
	at    Position
	state state
}

// finder finds the positions a table answers for: the schema sites, the
// discriminator mapping values, the landings, and where each of the source's
// reference objects resolves to, walking each root as it is noted. It keeps
// what it has found, and which of that is new since the table last settled.
type finder struct {
	docs          *docs
	work          *budget
	ends          map[jsontext.Pointer]Position
	sites         map[Position]bool
	mappingSites  map[mappingKey]bool
	fresh         []Position
	freshMappings []mappingKey
	landings      map[Position]bool
	pending       []root
	covered       map[root]bool
}

// newFinder returns a finder over docs that has found nothing.
func newFinder(d *docs, work *budget) *finder {
	return &finder{docs: d, work: work, ends: map[jsontext.Pointer]Position{}, sites: map[Position]bool{},
		mappingSites: map[mappingKey]bool{}, landings: map[Position]bool{}, covered: map[root]bool{}}
}

// walk reads every position below r by the grammar, noting each schema
// position that writes a $ref (a site), each discriminator mapping value that
// can name a position (mapping), and what each of the source's reference
// objects resolved to (a root to walk in turn). A node an alias places at
// several positions is read at each, as the parser reads it. With mark, each
// position read at a state is noted, so a later root inside one already read
// is not read again; the source's own walk from its root needs no notes.
func (w *finder) walk(r root, mark bool) {
	stack := []frame{{node: w.docs.node(r.at), at: r.at, state: r.state}}
	for len(stack) > 0 && w.work.spend(1) {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := nodeview.Deref(f.node)
		if n == nil || !modelled(f.state) {
			continue
		}
		if mark {
			if w.covered[root{f.at, f.state}] {
				continue
			}
			w.covered[root{f.at, f.state}] = true
		}
		w.visit(n, f)
		stack = append(stack, w.children(n, f)...)
	}
}

// visit notes what the position f, holding n, gives the table.
func (w *finder) visit(n *yaml.Node, f frame) {
	switch {
	case f.state == sSchema:
		if _, ok := w.docs.refOf(f.at.Doc, n); ok && !w.sites[f.at] {
			w.sites[f.at] = true
			w.fresh = append(w.fresh, f.at)
		}
		w.mappingsOf(n, f.at)
	case referenceKinds[f.state] && f.at.Doc == Source:
		if r, reason := w.docs.through(reading{at: f.at, node: n, state: f.state}, 0); reason == NoReason && r.at != f.at {
			w.ends[f.at.Pointer] = r.at
			w.pending = append(w.pending, root{at: r.at, state: f.state})
		}
	}
}

// mappingsOf notes each value of the discriminator the schema at at, holding
// n, writes that can name a position: one holding a '#', which a component's
// bare name cannot (lowering reads any other as a name). The parser reads none
// (docs/parser-library.md#mapping-unresolved), so each is bound here.
func (w *finder) mappingsOf(n *yaml.Node, at Position) {
	view := w.docs.list[at.Doc].view
	d := nodeview.Deref(view.ChildByToken(n, "discriminator"))
	if d == nil || d.Kind != yaml.MappingNode {
		return
	}
	disc := Position{Doc: at.Doc, Pointer: at.Pointer.AppendToken("discriminator")}
	var values []*yaml.Node
	if m := nodeview.Deref(view.ChildByToken(d, "mapping")); m != nil && m.Kind == yaml.MappingNode {
		for _, p := range view.MappingPairs(m) {
			values = append(values, p.Val)
		}
	}
	values = append(values, view.ChildByToken(d, "defaultMapping"))
	for _, v := range values {
		if v = nodeview.Deref(v); v == nil || v.Kind != yaml.ScalarNode || !containsHash(v.Value) {
			continue
		}
		if k := (mappingKey{at: disc, value: v.Value}); !w.mappingSites[k] {
			w.mappingSites[k] = true
			w.freshMappings = append(w.freshMappings, k)
		}
	}
}

// containsHash reports whether value holds a '#'.
func containsHash(value string) bool {
	return slices.Contains([]byte(value), '#')
}

// children are the positions directly below f, whose node is n.
func (w *finder) children(n *yaml.Node, f frame) []frame {
	var out []frame
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range w.docs.list[f.at.Doc].view.MappingPairs(n) {
			out = append(out, frame{node: p.Val, at: f.at.below(p.Key), state: step(f.state, p.Key)})
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			token := strconv.Itoa(i)
			out = append(out, frame{node: c, at: f.at.below(token), state: step(f.state, token)})
		}
	}
	return out
}

// below is the position token names under p.
func (p Position) below(token string) Position {
	return Position{Doc: p.Doc, Pointer: p.Pointer.AppendToken(token)}
}

// landed notes the root a resolved hop gives a walk, and, where the model
// holds no schema at its target, a landing the table builds one for.
func (w *finder) landed(h Hop) {
	w.pending = append(w.pending, root{at: h.Target, state: sSchema})
	if h.Role != Schema {
		w.landings[h.Target] = true
	}
}

// walkPending walks each root noted since the last round, in order, skipping
// a source position its own grammar already reads at that state.
func (w *finder) walkPending() bool {
	roots := w.pending
	w.pending = nil
	slices.SortFunc(roots, func(x, y root) int {
		if c := comparePositions(x.at, y.at); c != 0 {
			return c
		}
		return int(x.state) - int(y.state)
	})
	walked := false
	for _, r := range slices.Compact(roots) {
		if r.at.Doc == Source && w.docs.stateAt(r.at) == r.state {
			continue
		}
		w.walk(r, true)
		walked = true
	}
	return walked
}

// mappingKey is a mapping value and the discriminator writing it.
type mappingKey struct {
	at    Position
	value string
}

// mapping is where the mapping value k names, read as a $ref written at the
// discriminator: a hop, and the chain on from where it lands.
func (b *binder) mapping(k mappingKey) Mapping {
	m := Mapping{At: k.at, Value: k.value, Hop: b.resolve(k.at, k.value, false)}
	if b.work.over() {
		m.Hop = Hop{Ref: k.value, Status: OverBudget}
	}
	if m.Hop.Status != Resolved {
		m.Chain = Chain{Status: m.Hop.Status, Failed: k.at}
		return m
	}
	b.find.landed(m.Hop)
	if !b.holdsRef(m.Hop.Target) {
		m.Chain = Chain{Status: Resolved, End: m.Hop.Target, Role: m.Hop.Role, Hops: 1, Named: m.Hop.Named}
		return m
	}
	m.Chain = b.chain(m.Hop.Target)
	if m.Chain.Status == Resolved {
		m.Chain.Hops++
		m.Chain.Named = m.Chain.Named && m.Hop.Named
	}
	return m
}
