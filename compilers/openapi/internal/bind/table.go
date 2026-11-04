// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxSteps bounds the work the load phase lets one table cost: each node
// read, token walked, hop followed and document opened, read for what it
// declares or counted at a drain is a step. The source and every document it
// names are each held to the compiler's node budget before they get here, so
// this is far above what any admitted document costs, and only a document built
// to multiply that work meets it.
const MaxSteps = 1 << 26

// Config is what a table needs from the compile reading it.
type Config struct {
	// Self is the source's name, as Locate and Through name documents.
	Self string
	// Locate returns the name of the document uri, a reference's document
	// part, names when written in the document named base, and false when it
	// names none. Nil names each document by its uri.
	Locate func(uri, base string) (string, bool)
	// Open returns the tree of the document named name, read under the
	// compile's budgets, or an *UnparsableError for one that was read and does
	// not parse, or ErrEmptyDocument for one that holds nothing. Nil reads no
	// other document.
	Open func(name string) (*yaml.Node, error)
	// Refuse returns why a reference whose document part is uri, as written,
	// is refused unread by that spelling alone, however another reference
	// names the same document, or nil. Nil refuses none.
	Refuse func(uri string) error
	// Through returns where the chain of the reference object at a pointer of
	// the document named name ends, as the parser's resolver followed it. Nil,
	// or false, is a reference that resolved to nothing.
	Through func(name string, pointer jsontext.Pointer) (ReferenceEnd, bool)
	// Beside returns the path item $ref, and the key written beside it, whose
	// fields a pointer passes through. Nil passes none.
	Beside func(pointer jsontext.Pointer) (use jsontext.Pointer, key string, ok bool)
	// Written returns the value a $ref node holds as the source writes it, and
	// false where the tree holds it so: the load phase respells some for the
	// parser while the binding reads (GitHub #911). Nil reads every node as it
	// stands.
	Written func(n *yaml.Node) (string, bool)
	// Limit bounds the work, in steps (see MaxSteps). Unset, it is unbounded,
	// as the zero budget.Limit is.
	Limit budget.Limit
}

// ErrEmptyDocument is the failure of Config.Open, or of the tree it returns,
// for a document that was read and holds nothing (see IsEmpty).
var ErrEmptyDocument = errors.New("the document is empty")

// IsEmpty reports whether tree, a document as parsed, holds nothing: no node,
// though it may hold white space and comments. A document holding null is not
// empty.
func IsEmpty(tree *yaml.Node) bool {
	root := nodeview.DocumentRoot(tree)
	return root == nil || root.Kind == 0
}

// UnparsableError is the failure of Config.Open for a document that was read
// and does not parse: Err is the parser's reason, which says where.
type UnparsableError struct {
	Err error
}

// Error says the document does not parse, and the parser's reason.
func (e *UnparsableError) Error() string { return "it does not parse as YAML: " + e.Err.Error() }

// Unwrap returns the parser's reason.
func (e *UnparsableError) Unwrap() error { return e.Err }

// ReferenceEnd is where the parser's resolver ended a reference object's
// chain: the name of the document, the pointer its last $ref writes there, and
// the node the chain reached, a node of the tree Config.Open or Bind is handed
// for that document. The end is the position on the pointer's read that holds
// Node, since the resolver reads some pointers otherwise than RFC 6901 does
// (docs/parser-library.md#trailing-empty-token). A nil Node places the end
// where the pointer reads.
type ReferenceEnd struct {
	Name    string
	Pointer jsontext.Pointer
	Node    *yaml.Node
}

// work is the steps a table has spent, against its limit, and the context the
// binding runs under. Every loop of a binding asks it before it does more, so
// the context is asked there with the budget, and a cancel stops the binding
// at its next step, as running out does.
type work struct {
	ctx   context.Context
	steps budget.Budget
}

// spend charges n steps and reports whether the binding may go on.
func (w *work) spend(n int) bool {
	w.steps.Spend(n)
	return !w.over()
}

// over reports whether the binding must stop: its budget has run out, or its
// context is done.
func (w *work) over() bool { return w.steps.Exhausted() || w.cancelled() }

// cancelled reports whether the binding's context is done. A loop over what is
// left to read asks it, and leaves: past an exhausted budget each item is
// still recorded, cut, but a cancelled binding is read by no one. A binding
// its context stopped reports nothing, the budget included.
func (w *work) cancelled() bool { return w.ctx.Err() != nil }

// binder is the state of one pass of a table's building: every reference read
// once against the URIs its identifiers hold (identifiers.commit). held is each
// document an absolute URI named that the pass has not read, and wait what
// waits for those documents. committed reports that a drain of the pass has
// committed what it reached, and stale that the declarations of a URI it asked
// for changed, so the pass is read again (reread).
type binder struct {
	cfg       Config
	docs      *docs
	work      *work
	defs      definitions
	resources resources
	find      *finder
	ids       *identifiers
	held      map[string]bool
	wait      *waits
	hops      map[Position]Hop
	chains    map[Position]Chain
	loops     []Loop
	committed bool
	stale     bool
}

// Bind reads every schema reference the source tree src holds, and every
// position a reference reaches through it, into a table, within the budget. It
// stops once ctx is done, leaving a table its caller must not read.
//
// It reads in passes, as whether a URI is an identifier turns on which
// documents are read, and which are read on what the URIs name. A pass reruns
// only when what it reached changes a URI it asked for, and only the last
// stands, so no declaration order reaches an answer (reread); each reads what
// none before it did (identifiers.commit).
func Bind(ctx context.Context, src *yaml.Node, cfg Config) *Table {
	w := &work{ctx: ctx, steps: budget.New(cfg.Limit)}
	d := newDocs(src, cfg, w)
	ids := newIdentifiers(resources{d}.document(Source))
	for {
		b := newBinder(cfg, d, w, ids)
		t := run(b)
		if !reread(b) {
			return b.table(t)
		}
	}
}

// newBinder returns a pass over d, knowing what ids holds and nothing else.
func newBinder(cfg Config, d *docs, w *work, ids *identifiers) *binder {
	d.reached, d.fresh = map[DocID]bool{Source: true}, nil
	return &binder{cfg: cfg, docs: d, work: w, defs: definitions{docs: d, work: w}, resources: resources{d},
		find: newFinder(d, w, ids), ids: ids, held: map[string]bool{}, wait: newWaits(), hops: map[Position]Hop{},
		chains: map[Position]Chain{}}
}

// run answers every reference the pass b reaches, going on past what it held
// for as long as that changes no answer it gave (goOn). A site still waiting
// when the pass ends, as the budget or a change stopped it, has no entry: the
// table says why (Table.Unread), or the pass is read again.
func run(b *binder) *Table {
	b.find.walk(rootOf(Source, sDocument), false)
	b.ids.begin()
	t := &Table{entries: map[Position]Entry{}, mappings: map[mappingKey]Mapping{}}
	for {
		for more := true; more && !b.work.over(); more = b.find.walkPending() {
			b.settle(t)
		}
		if b.work.over() || !goOn(b) {
			break
		}
	}
	b.placeLoops()
	return t
}

// goOn reports whether the pass b, its walks done, goes on. It does while
// what the documents it reached declare changes no URI it asked for (settled):
// to read the documents it held and the hops that waited for them, which the
// next drain holds to the same test, or, once nothing is held, to settle what
// waited. A change ends it, to be read again.
func goOn(b *binder) bool {
	if !settled(b) {
		b.stale = true
		return false
	}
	if len(b.held) == 0 {
		return b.wait.release(b.find)
	}
	readHeld(b)
	b.wait.requeue(b.find, b.hops)
	return true
}

// settled reports whether what the documents the pass b reached declare leaves
// the declarations of every URI it asked for as it read them. Its first drain
// commits what it reached whole, as a document an earlier pass reached may no
// longer be. A pass only adds to what it reaches, so a later drain grows by
// what it reached since (identifiers.grow), and commits only to judge a change.
// Each document a drain counts is a step.
func settled(b *binder) bool {
	b.find.identifyAll()
	fresh := b.docs.takeFresh()
	b.work.spend(len(fresh))
	if b.committed && !b.ids.grow(fresh) {
		return true
	}
	b.committed = true
	b.work.spend(len(b.docs.reached))
	return !b.ids.commit(b.docs.reached)
}

// reread reports whether the pass b, ended, must be run again: what the
// documents it reached declare changed a URI it asked for, so an answer it gave
// is not theirs (goOn). A document only an earlier pass reached declares
// nothing. The pass the budget runs out in, wherever in it, is the last, and
// its answers stand, so no pass begins with nothing to spend. A chain of
// documents each declaring the URI the one before it asked for, held or not,
// so costs a pass per link (GitHub #1372).
func reread(b *binder) bool {
	return b.stale && !b.work.over()
}

// readHeld reads the documents the pass b held back, as no document it read
// answers for their URIs (OAS 3.1 §4.3.1). They are read together, a round for
// all rather than one each, so one may declare another's URI, read then for
// nothing: the next drain sees the change, and the next pass reads against it.
func readHeld(b *binder) {
	for _, name := range slices.Sorted(maps.Keys(b.held)) {
		// Its failure is kept for the hop that asks for it again (docs.open).
		_, _ = b.docs.open(name)
	}
	clear(b.held)
}

// rootOf is the root at the root of document id, read at s.
func rootOf(id DocID, s state) root {
	return root{at: Position{Doc: id}, state: s}
}

// settle answers every site and mapping value noted since it last ran, in
// order, so that where the budget runs out does not depend on what was
// noted first. Each is sorted once, in the round that noted it, and costs a
// step, so a round that answered one again would show in the work.
func (b *binder) settle(t *Table) {
	sites, keys := b.find.fresh, b.find.freshMappings
	b.find.fresh, b.find.freshMappings = nil, nil
	b.work.spend(len(sites) + len(keys))
	slices.SortFunc(sites, b.docs.compare)
	for _, site := range sites {
		if b.work.cancelled() {
			return
		}
		if c, waits := b.chain(site); waits {
			b.wait.sites[site] = true
		} else {
			t.entries[site] = Entry{Site: site, Hop: b.hops[site], Chain: c}
		}
	}
	slices.SortFunc(keys, mappingOrder(b.docs))
	for _, k := range keys {
		if b.work.cancelled() {
			return
		}
		if m, waits := b.mapping(k); waits {
			b.wait.mappings[k] = true
		} else {
			t.mappings[k] = m
		}
	}
	b.find.landOnAnchors()
}

// mappingOrder orders mapping values by their discriminator's position in d,
// then by value.
func mappingOrder(d *docs) func(x, y mappingKey) int {
	return func(x, y mappingKey) int {
		if c := d.compare(x.at, y.at); c != 0 {
			return c
		}
		return cmp.Compare(x.value, y.value)
	}
}

// table finishes t: an entry for each position a chain passed that no walk
// found, which only a budget cutting the walks leaves, the loops, the
// landings, and the verdicts of the budget and of the merge bound.
func (b *binder) table(t *Table) *Table {
	for p, c := range b.chains {
		if _, found := t.entries[p]; !found {
			t.entries[p] = Entry{Site: p, Hop: b.hops[p], Chain: c}
		}
	}
	t.loops, t.steps, t.limit, t.exhausted = b.loops, b.work.steps.Spent(), b.work.steps.Limit(), b.work.steps.Exhausted()
	t.ends, t.roots, t.res, t.ids = b.find.ends, b.find.roots, b.resources, b.ids
	dialects := dialectsByDocument(b.ids.dialects)
	for id, d := range b.docs.list {
		reached := b.docs.reached[DocID(id)]
		t.docs = append(t.docs, Document{Name: d.name, Root: d.root, Unreached: !reached, Dialects: dialects[DocID(id)]})
		t.truncated = t.truncated || reached && d.view.Exhausted()
	}
	reach := b.attribute(t)
	for at := range b.find.landings {
		site, reached := reach[at]
		t.landings = append(t.landings, Landing{At: at, Name: t.docs[at.Doc].Name, Site: site, Reached: reached})
	}
	slices.SortFunc(t.landings, func(x, y Landing) int { return t.compare(x.At, y.At) })
	return t
}

// attribute returns, for each position some $ref's, mapping value's or
// expanding $dynamicRef's chain reaches, the least such position, in whichever
// document. Starting from each in order, a chain is followed until it meets a
// position a lesser start reached, whose chain on from there that start has
// marked, so each position is marked once.
func (b *binder) attribute(t *Table) map[Position]Position {
	type start struct {
		from, first Position
	}
	var starts []start
	add := func(from Position, h Hop) {
		if h.Status == Resolved {
			starts = append(starts, start{from: from, first: h.Target})
		}
	}
	for _, e := range t.entries {
		add(e.Site, e.Hop)
	}
	for _, m := range t.mappings {
		add(m.At, m.Hop)
	}
	for _, e := range b.find.expansions {
		add(e.from, Hop{Status: Resolved, Target: e.to})
	}
	slices.SortFunc(starts, func(x, y start) int {
		if c := b.docs.compare(x.from, y.from); c != 0 {
			return c
		}
		return b.docs.compare(x.first, y.first)
	})
	reach := map[Position]Position{}
	for _, s := range starts {
		for p := s.first; ; {
			if _, seen := reach[p]; seen {
				break
			}
			reach[p] = s.from
			h, ok := b.hops[p]
			if !ok || h.Status != Resolved {
				break
			}
			p = h.Target
		}
	}
	return reach
}

// Document is a document a table read: its name and its root node. Unreached
// reports that only a pass that did not stand asked for it, so that nothing the
// table holds is in it: a reference read it as a location before a later
// document declared that URI. Dialects is each schema in it no other schema
// holds that names its dialect, in pointer order.
type Document struct {
	Name      string
	Root      *yaml.Node
	Dialects  []DialectRoot
	Unreached bool
}

// Table is every schema reference a source holds, where each lands and where
// its chain ends, the loops among them, and what the reading cost. It is built
// once and never written after.
type Table struct {
	entries   map[Position]Entry
	roots     map[Position]state
	mappings  map[mappingKey]Mapping
	loops     []Loop
	landings  []Landing
	docs      []Document
	ends      map[Position]Position
	steps     int
	limit     budget.Limit
	exhausted bool
	truncated bool
	res       resources
	ids       *identifiers
}

// Entry returns the entry of the $ref written at site, and false for a
// position the table read no $ref at: one writing none, or one the budget
// left unread.
func (t *Table) Entry(site Position) (Entry, bool) {
	e, ok := t.entries[site]
	return e, ok
}

// Entries returns every entry, ordered by site.
func (t *Table) Entries() []Entry {
	out := slices.Collect(maps.Values(t.entries))
	slices.SortFunc(out, func(x, y Entry) int { return t.compare(x.Site, y.Site) })
	return out
}

// Mapping returns where the mapping value written by the discriminator at at
// lands, and false for one the table did not read: a value with no '#', which
// names a component by name.
func (t *Table) Mapping(at Position, value string) (Mapping, bool) {
	m, ok := t.mappings[mappingKey{at: at, value: value}]
	return m, ok
}

// Loops returns every loop of $refs.
func (t *Table) Loops() []Loop { return slices.Clone(t.loops) }

// Landings returns every landing, ordered by position.
func (t *Table) Landings() []Landing { return slices.Clone(t.landings) }

// Document returns the document id names.
func (t *Table) Document(id DocID) Document { return t.docs[id] }

// compare orders positions as the table reads them: the source's first,
// another document's by its name, then by pointer (comparePositions).
func (t *Table) compare(p, q Position) int {
	if c, ok := byName(p, q, t.name); ok {
		return c
	}
	return comparePositions(p, q)
}

// name returns the name of the document id names.
func (t *Table) name(id DocID) string { return t.docs[id].Name }

// Documents reports how many documents the table read, the source included.
func (t *Table) Documents() int { return len(t.docs) }

// Through returns where the reference object at site resolves to, in
// whichever document: where the parser's resolver ended its chain
// (Config.Through), read on through any reference object that end lies past.
// It is false for a position writing no $ref, or one whose end the table could
// not read.
func (t *Table) Through(site Position) (Position, bool) {
	at, ok := t.ends[site]
	return at, ok
}

// DynamicAnchors returns the positions of document id declaring the
// $dynamicAnchor name, and whether that is every one: false where the budget or
// a merge past the bound may have hidden one, so a count of one proves nothing.
// A nil table knows of none.
func (t *Table) DynamicAnchors(id DocID, name string) ([]Position, bool) {
	if t == nil {
		return nil, true
	}
	sites := slices.SortedFunc(slices.Values(t.ids.dynamic[dynamicKey{doc: id, name: name}]), comparePositions)
	return sites, !t.exhausted && !t.ids.truncated[id]
}

// InOwnResource reports whether the schema at p, its own $id counted, lies in a
// resource an $id names rather than in its document's, and whether that is
// known: false where a schema on the way may hide an $id past the merge bound.
func (t *Table) InOwnResource(p Position) (own, known bool) {
	if t == nil {
		return false, true
	}
	path, _ := t.res.docs.path(p)
	res, known := t.res.holding(p, path, true /* schema */)
	return res.id, known
}

// Base returns the URI a reference written in the schema at p resolves
// against, its own $id counted: its document's, or the resource's an $id at or
// above it names. Two schemas with one base read every reference they write
// alike. It is false where a schema on the way may hide an $id past the merge
// bound, and a nil table knows none. Each call walks from the document's root,
// so asking at every level of a deep schema costs the square of its depth
// (GitHub #1431).
func (t *Table) Base(p Position) (string, bool) {
	if t == nil {
		return "", false
	}
	path, _ := t.res.docs.path(p)
	res, known := t.res.holding(p, path, true /* schema */)
	return res.uri, known
}

// Steps reports the work the table cost.
func (t *Table) Steps() int { return t.steps }

// Limit reports the work the table was allowed (Config.Limit).
func (t *Table) Limit() budget.Limit { return t.limit }

// Exhausted reports whether the budget ran out, so that a site may be missing
// or answered OverBudget.
func (t *Table) Exhausted() bool { return t.exhausted }

// Truncated reports whether a mapping of a document the table reached merges
// through `<<` deeper than the view expands (nodeview.MergeDepthLimit), so that
// a site past the bound may be missing, and a position past it read as holding
// nothing.
func (t *Table) Truncated() bool { return t.truncated }

// Unread returns why a schema $ref the table holds no entry for went unread:
// the budget ran out, or a merge past the bound hid it. It is empty when
// neither happened, and every $ref was read, or for a nil table, which read
// none.
func (t *Table) Unread() string {
	switch {
	case t == nil:
		return ""
	case t.exhausted:
		return Hop{Status: OverBudget}.Why()
	case t.truncated:
		return fmt.Sprintf("it is past a `<<` merge chain deeper than the %d levels the compiler expands",
			nodeview.MergeDepthLimit)
	default:
		return ""
	}
}
