// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// root is a position a walk for reference sites starts from, and the state
// it is read at: the source's root, a position a hop lands on, read as a
// schema, or what a reference object resolved to, read as its kind.
type root struct {
	at    Position
	state state
}

// frame is one position a walk has yet to read. In the source's own walk,
// which reads the identifiers its schemas declare, res indexes the resource
// its parent's content is in (identifiers.resources), dialect is the one its
// parent is read in, and inert marks the fields beside a reference object's
// $ref, which the model ignores, so no identifier is read there.
type frame struct {
	node    *yaml.Node
	at      Position
	dialect string
	res     int32
	state   state
	inert   bool
}

// finder finds the positions a table answers for: the schema sites, the
// discriminator mapping values, the landings, and where each reference object
// resolves to, walking each root as it is noted, and the identifiers each
// document's schemas declare. It keeps what it has found, and which of that is
// new since the table last settled.
type finder struct {
	docs          *docs
	ids           *identifiers
	work          *work
	ends          map[Position]Position
	sites         map[Position]bool
	mappingSites  map[mappingKey]bool
	fresh         []Position
	freshMappings []mappingKey
	freshDynamic  []dynamicSite
	expansions    []expansion
	landings      map[Position]bool
	// tupled is each schema the parser's model cannot hold, by the schema
	// writing it (tupleOf).
	tupled  map[Position]Position
	pending []root
	covered map[root]bool
	roots   map[Position]state
}

// newFinder returns a finder over docs that has found nothing, noting what the
// documents declare in ids.
func newFinder(d *docs, w *work, ids *identifiers) *finder {
	return &finder{docs: d, ids: ids, work: w, ends: map[Position]Position{},
		sites: map[Position]bool{}, mappingSites: map[mappingKey]bool{}, landings: map[Position]bool{},
		tupled:  map[Position]Position{},
		covered: map[root]bool{}, roots: map[Position]state{}}
}

// walk reads every position below r by the grammar, noting each schema position
// that writes a $ref (a site), each discriminator mapping value that can name a
// position (mapping), and what each reference object resolved to (a root to
// walk in turn). A node an alias places at several positions is read at each,
// as the parser reads it. With mark, each position read at a state is noted, so
// a later root inside one already read is not read again; the source's own walk
// from its root notes none, and reads its identifiers the first time it runs.
func (w *finder) walk(r root, mark bool) {
	if prev, seen := w.roots[r.at]; !seen || r.state < prev {
		w.roots[r.at] = r.state
	}
	declaring := !mark && !w.ids.read[r.at.Doc]
	if !declaring {
		// The dialect roots a walk reads are those its document declares.
		w.identify(r.at.Doc)
	}
	w.ids.read[r.at.Doc] = w.ids.read[r.at.Doc] || declaring
	stack := []frame{{node: w.docs.node(r.at), at: r.at, state: r.state, dialect: dialectAbove(w.docs, w.ids, r.at)}}
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
		if declaring {
			f = w.declared(n, f)
		} else if len(w.ids.dialects) > 0 {
			f.dialect = rootDialect(w.ids, f.at, f.dialect)
		}
		w.tupleOf(n, f)
		stack = w.pushChildren(stack, n, f)
	}
}

// identify reads what the schemas of document id declare, the first time it is
// asked, whole: OAS 3.1 §4.3.1 has a document parsed complete before a reference
// to it is deemed unresolvable, so what a reference names of it depends on
// nothing read before. It notes no site, as a position of another document
// answers only for what a reference reaches.
func (w *finder) identify(id DocID) {
	if w.ids.read[id] {
		return
	}
	w.ids.read[id] = true
	res := w.ids.document(id, resources{w.docs}.document(id))
	stack := []frame{{node: w.docs.list[id].root, at: Position{Doc: id}, state: w.docs.identityState(id), res: res,
		dialect: defaultDialect(w.docs, id)}}
	for len(stack) > 0 && w.work.spend(1) {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := nodeview.Deref(f.node)
		if n == nil || !modelled(f.state) {
			continue
		}
		stack = w.pushChildren(stack, n, w.declared(n, f))
	}
}

// identifyAll reads what each document opened so far declares, starting past
// those an earlier call read, so a pass that drains many times reads each once.
func (w *finder) identifyAll() {
	for ; w.ids.scanned < DocID(len(w.docs.list)); w.ids.scanned++ {
		if !w.work.spend(1) {
			return
		}
		w.identify(w.ids.scanned)
	}
}

// declared notes the identifiers the schema at f, holding n, declares, and
// returns f as its children read it: in the resource its content is in, and
// inert below a reference object's $ref other than a path item's, whose fields
// beside it are read (Config.Beside).
func (w *finder) declared(n *yaml.Node, f frame) frame {
	switch {
	case f.inert:
	case f.state == sSchema || f.state == sLoose:
		f.res, f.dialect = w.ids.declare(resources{w.docs}, f.res, f.at, n, f.dialect)
	case referenceKinds[f.state] && f.state != sPathItem:
		_, f.inert = w.docs.refOf(f.at.Doc, n)
	}
	return f
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
		w.dynamicOf(n, f.at)
	case referenceKinds[f.state]:
		if r, reason := w.docs.through(reading{at: f.at, node: n, state: f.state}, 0); reason == NoReason && r.at != f.at {
			w.ends[f.at] = r.at
			w.pending = append(w.pending, root{at: r.at, state: f.state})
		}
	}
}

// tupleOf notes each schema the schema at f, holding n and read in f's
// dialect, writes where the parser's model has no member to hold one: each
// entry of an `items` written as a list the dialect reads as a tuple, and the
// `additionalItems` beside it, its rest. The table builds a schema for each,
// as for a landing, held by the schema writing it.
func (w *finder) tupleOf(n *yaml.Node, f frame) {
	schema := f.state == sSchema || f.state == sLoose
	if !schema || !dialectRule(w.docs, f.at.Doc, f.dialect).Tuples || !listsItems(w.docs, f.at.Doc, n) {
		return
	}
	view := w.docs.list[f.at.Doc].view
	items := f.at.below("items")
	for i := range nodeview.Deref(view.ChildByToken(n, "items")).Content {
		w.tupled[items.below(strconv.Itoa(i))] = f.at
	}
	if nodeview.Deref(view.ChildByToken(n, "additionalItems")) != nil {
		w.tupled[f.at.below("additionalItems")] = f.at
	}
}

// dynamicSite is a $dynamicRef of another document and the anchor it names.
type dynamicSite struct {
	at  Position
	key dynamicKey
}

// expansion is a $dynamicRef written at from and the schema at to declaring the
// one anchor of its name, which it reaches as a $ref reaches what it names.
type expansion struct {
	from, to Position
}

// dynamicOf notes the anchor the $dynamicRef the schema at at, holding n, names,
// where another document holds it. Nothing but a reference builds a schema of
// another document (landed), and an expansion is one, so settle lands on the
// anchor. The source's are the model's own.
func (w *finder) dynamicOf(n *yaml.Node, at Position) {
	if at.Doc == Source {
		return
	}
	ref, ok := scalarOf(w.docs.list[at.Doc].view.ChildByToken(n, "$dynamicRef"))
	if !ok {
		return
	}
	if name, _, ok := DynamicFragment(ref); ok {
		w.freshDynamic = append(w.freshDynamic, dynamicSite{at: at, key: dynamicKey{doc: at.Doc, name: name}})
	}
}

// landOnAnchors lands on the one schema declaring each anchor a $dynamicRef in
// another document names, as a reference would, for the schema an expansion
// takes to be built and read. It waits for no later pass: a name is only asked
// of a document open, whose identifiers it reads whole.
func (w *finder) landOnAnchors() {
	dynamic := w.freshDynamic
	w.freshDynamic = nil
	for _, d := range dynamic {
		w.identify(d.key.doc)
		if at := w.ids.dynamic[d.key]; len(at) == 1 {
			w.landed(Hop{Target: at[0], Role: Raw})
			w.expansions = append(w.expansions, expansion{from: d.at, to: at[0]})
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

// pushChildren returns stack with the positions directly below f, whose node is
// n, appended in order, so a walk reads its children with no slice of their
// own.
func (w *finder) pushChildren(stack []frame, n *yaml.Node, f frame) []frame {
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range w.docs.list[f.at.Doc].view.MappingPairs(n) {
			stack = append(stack, frame{node: p.Val, at: f.at.below(p.Key),
				state: stepIn(w.docs, f.at.Doc, n, f.state, p.Key, f.dialect), res: f.res, inert: f.inert, dialect: f.dialect})
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			token := strconv.Itoa(i)
			stack = append(stack, frame{node: c, at: f.at.below(token), state: step(f.state, token), res: f.res,
				inert: f.inert, dialect: f.dialect})
		}
	}
	return stack
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
		if c := w.docs.compare(x.at, y.at); c != 0 {
			return c
		}
		return int(x.state) - int(y.state)
	})
	walked := false
	for _, r := range slices.Compact(roots) {
		if w.work.cancelled() {
			break
		}
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
// discriminator: a hop, and the chain on from where it lands. It reports waits
// where either waits for a document the pass has yet to read (waits).
func (b *binder) mapping(k mappingKey) (Mapping, bool) {
	// A discriminator is a position its document holds, so the path reaches it.
	path, _ := b.docs.path(k.at)
	m := Mapping{At: k.at, Hop: b.resolve(k.at, path, k.value, false)}
	if b.work.over() {
		m.Hop = Hop{Ref: k.value, Status: OverBudget}
	}
	if m.Hop.Status != Resolved {
		m.Chain = Chain{Status: m.Hop.Status}
		if m.Hop.Reason == Held {
			b.wait.heldMappings = append(b.wait.heldMappings, k)
			return m, true
		}
		return m, false
	}
	b.find.landed(m.Hop)
	if !b.holdsRef(m.Hop.Target) {
		m.Chain = Chain{Status: Resolved, End: m.Hop.Target, Hops: 1}
		return m, false
	}
	c, waits := b.chain(m.Hop.Target)
	if c.Status == Resolved {
		c.Hops++
	}
	m.Chain = c
	return m, waits
}
