// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
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
	// Self is the source's name, as Locate names documents.
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
	// Limit bounds the work, in steps (see MaxSteps). Unset, it is unbounded,
	// as the zero budget.Limit is.
	Limit budget.Limit
	// Dialect returns how a schema of document name read in dialect, the URI
	// a `$schema` or a jsonSchemaDialect names, or "" for none, names its
	// resource and holds its subschemas (DialectRule). Nil, or the zero rule,
	// reads it as JSON Schema 2020-12 does.
	Dialect func(name, dialect string) DialectRule
	// Components is the keys of the source's schema components. A
	// discriminator mapping value spelling one names that component, in
	// whichever document it is written, so the table reads every other value,
	// and none of these, as a reference (Table.Mapping).
	Components []string
}

// DialectRule is what the table reads otherwise in a dialect than in 2020-12.
// Keyword is the identifier's keyword, `$id` where empty, or draft 4's `id`.
// With Fragments one naming a fragment alone declares that name: any no JSON
// pointer spells, as in draft 4, or with PlainFragments a plain name, as in
// drafts 6 and 7. With Definitions `definitions` holds schemas, with
// Dependencies a schema in `dependencies` is one, as before 2019-09, with
// PlainNames `$anchor` takes that grammar, as in 2019-09, and with Tuples an
// `items` list is a tuple, as before 2020-12. With NamesNone the value names
// no dialect, so its `$schema` chooses none.
type DialectRule struct {
	Keyword        string
	Fragments      bool
	PlainFragments bool
	Definitions    bool
	Dependencies   bool
	PlainNames     bool
	Tuples         bool
	// Recursive has a `$recursiveAnchor` of true mark its resource for a
	// `$recursiveRef`, as in 2019-09.
	Recursive bool
	// IgnoresRefFields has a schema writing a `$ref` write no identifier, as
	// the drafts before 2019-09 ignore every keyword beside one.
	IgnoresRefFields bool
	NamesNone        bool
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
func (e *UnparsableError) Error() string { return "it does not parse: " + e.Err.Error() }

// Unwrap returns the parser's reason.
func (e *UnparsableError) Unwrap() error { return e.Err }

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
	objs      *objects
	loops     []Loop
	committed bool
	stale     bool
}

// Bind reads every reference the source tree src holds, a schema's or a
// Reference Object's, and every position one reaches, into a table, within the
// budget. It stops once ctx is done, leaving a table no caller may read.
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
	b := &binder{cfg: cfg, docs: d, work: w, defs: definitions{docs: d, work: w}, resources: resources{d},
		find: newFinder(d, w, ids), ids: ids, held: map[string]bool{}, wait: newWaits(), hops: map[Position]Hop{},
		chains: map[Position]Chain{}}
	b.objs = &objects{b: b, hops: map[Position]objectHop{}, chains: map[objectKey]Chain{}}
	return b
}

// run answers every reference the pass b reaches, going on past what it held
// for as long as that changes no answer it gave (goOn). A site still waiting
// when the pass ends, as the budget or a change stopped it, has no entry: the
// table says why (Table.Unread), or the pass is read again.
func run(b *binder) *Table {
	b.find.walk(rootOf(Source, sDocument), false)
	b.ids.begin()
	t := &Table{entries: map[Position]Entry{}, mappings: map[mappingKey]Mapping{}, references: map[objectKey]Reference{}}
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
	b.wait.requeue(b.find, b.hops, b.objs.hops)
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

// settle answers every site, mapping value and reference object noted since it
// last ran, in order, so that where the budget runs out does not depend on what
// was noted first. Each is sorted once, in the round that noted it, and costs a
// step, so a round that answered one again would show in the work.
func (b *binder) settle(t *Table) {
	sites, keys, objects := b.find.fresh, b.find.freshMappings, b.find.freshObjects
	b.find.fresh, b.find.freshMappings, b.find.freshObjects = nil, nil, nil
	b.work.spend(len(sites) + len(keys) + len(objects))
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
	b.objs.settleAll(t, objects)
	landDynamic(b)
}

// landDynamic lands on what each dynamic reference noted since the table last
// settled names, as the $ref it spells would, for the schema an expansion takes
// to be built and read where the model holds none: the one schema declaring a
// $dynamicRef's anchor, reached as a $ref naming that anchor in its resource
// is, and the root of a $recursiveRef's resource, which a $ref of "#" written
// there resolves to. A hop the resolution leaves unresolved lands nowhere. A
// name is only asked of a document open, whose identifiers are read whole.
func landDynamic(b *binder) {
	dynamic, recursive := b.find.freshDynamic, b.find.freshRecursive
	b.find.freshDynamic, b.find.freshRecursive = nil, nil
	for _, d := range dynamic {
		b.find.identify(d.key.doc)
		at := b.ids.dynamic[d.key]
		if len(at) != 1 {
			continue
		}
		path, _ := b.docs.path(d.at)
		if h := b.resolve(d.at, path, "#"+url.PathEscape(d.key.name), true); h.Status == Resolved && h.Target == at[0] {
			b.find.landOn(d.at, h)
		}
	}
	for _, site := range recursive {
		path, _ := b.docs.path(site)
		if h := b.resolve(site, path, "#", true); h.Status == Resolved {
			b.find.landOn(site, h)
		}
	}
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
	for k, c := range b.objs.chains {
		if _, found := t.references[k]; !found {
			t.references[k] = Reference{Site: k.at, Kind: k.kind, Hop: ofKind(b.objs.hops[k.at], k.kind), Chain: c}
		}
	}
	t.loops, t.steps, t.limit, t.exhausted = b.loops, b.work.steps.Spent(), b.work.steps.Limit(), b.work.steps.Exhausted()
	t.roots, t.bases, t.ids = b.find.roots, newBases(b.resources, b.ids), b.ids
	t.recursive = recursiveAnchorsOf(b.ids, t.bases)
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
	for at, holder := range b.find.tupled {
		if !b.find.landings[at] {
			t.landings = append(t.landings, Landing{At: at, Name: t.docs[at.Doc].Name, Site: holder, Reached: true})
		}
	}
	slices.SortFunc(t.landings, func(x, y Landing) int { return b.docs.compare(x.At, y.At) })
	t.objLandings = b.objs.landings(t)
	t.entryList = slices.SortedFunc(maps.Values(t.entries), func(x, y Entry) int { return b.docs.compare(x.Site, y.Site) })
	t.refList = slices.SortedFunc(maps.Values(t.references), func(x, y Reference) int {
		return compareObjects(b.docs)(objectKey{at: x.Site, kind: x.Kind}, objectKey{at: y.Site, kind: y.Kind})
	})
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
// document declared that URI. Dialects is each schema resource root in it
// that names its dialect, in pointer order.
type Document struct {
	Name      string
	Root      *yaml.Node
	Dialects  []DialectRoot
	Unreached bool
}

// Table is every reference a source holds, a schema's $ref or a Reference
// Object's, where each lands and where its chain ends, the loops among them,
// and what the reading cost. It is built once, and no answer it gives changes
// after: only its base lookups write, to remember the walks they make (bases),
// so one is not safe for concurrent use. Each list it gives is sorted once, as
// it is built, in the order docs.compare reads positions in.
type Table struct {
	entries     map[Position]Entry
	references  map[objectKey]Reference
	roots       map[Position]state
	mappings    map[mappingKey]Mapping
	loops       []Loop
	landings    []Landing
	objLandings []ObjectLanding
	entryList   []Entry
	refList     []Reference
	docs        []Document
	steps       int
	limit       budget.Limit
	exhausted   bool
	truncated   bool
	// bases remembers each walk Base and Resource make, filled as they
	// ask. It changes no answer, each being what a walk from the root gives,
	// and nothing guards it, as only the lowering asks, on one goroutine.
	bases     *bases
	ids       *identifiers
	recursive map[DocID]recursiveAnchors
}

// Entry returns the entry of the $ref written at site, and false for a
// position the table read no $ref at: one writing none, or one the budget
// left unread.
func (t *Table) Entry(site Position) (Entry, bool) {
	e, ok := t.entries[site]
	return e, ok
}

// Entries returns every entry, ordered by site.
func (t *Table) Entries() []Entry { return slices.Clone(t.entryList) }

// Mapping returns where the mapping value written by the discriminator at at
// lands, and false for one the table did not read: a value naming a schema
// component of the source (Config.Components), or one it left unread (Unread).
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

// Documents reports how many documents the table read, the source included.
func (t *Table) Documents() int { return len(t.docs) }

// Reference returns the entry of the Reference Object written at site, read
// as kind, and false for a position the table read no such reference at: one
// writing no $ref, one no walk read as that kind, or one the budget left
// unread.
func (t *Table) Reference(site Position, kind RefKind) (Reference, bool) {
	r, ok := t.references[objectKey{at: site, kind: kind}]
	return r, ok
}

// References returns every Reference Object's entry, ordered by site, then
// kind.
func (t *Table) References() []Reference { return slices.Clone(t.refList) }

// ObjectLandings returns every object landing, ordered by position, then
// kind.
func (t *Table) ObjectLandings() []ObjectLanding { return slices.Clone(t.objLandings) }

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

// RecursiveAnchor reports what the schemas of root's document declaring
// `$recursiveAnchor` true, where their dialect defines the keyword
// (DialectRule.Recursive), say of a $recursiveRef reaching root: whether root
// declares it, how many schema resources hold a declaration, and whether both
// are known, as DynamicAnchors says, each declaration's resource included. A
// nil table knows of none.
func (t *Table) RecursiveAnchor(root Position) (declared bool, resources int, known bool) {
	if t == nil {
		return false, 0, true
	}
	r := t.recursive[root.Doc]
	return r.sites[root], r.resources, !t.exhausted && !t.ids.truncated[root.Doc] && !r.unplaced
}

// recursiveAnchors is what the `$recursiveAnchor: true` declarations of one
// document say (Table.RecursiveAnchor): where they are, how many schema
// resources hold them, and whether one lies where a merge past the bound may
// hide the resource it is in.
type recursiveAnchors struct {
	sites     map[Position]bool
	resources int
	unplaced  bool
}

// recursiveAnchorsOf returns what the declarations ids holds say, by document,
// the resource of each read from b once, so a $recursiveRef asks a lookup
// rather than a read of every declaration its document makes.
func recursiveAnchorsOf(ids *identifiers, b *bases) map[DocID]recursiveAnchors {
	out := make(map[DocID]recursiveAnchors, len(ids.recursive))
	for id, sites := range ids.recursive {
		r := recursiveAnchors{sites: make(map[Position]bool, len(sites))}
		roots := map[Position]bool{}
		for _, site := range sites {
			res, known := b.holding(site)
			r.sites[site], roots[res.root] = true, true
			r.unplaced = r.unplaced || !known
		}
		r.resources = len(roots)
		out[id] = r
	}
	return out
}

// Resource returns the root of the schema resource the schema at p lies in,
// its own identifier counted: the schema whose identifier starts it, or its
// document's root where none on the way does. It is false where a schema on
// the way may hide one past the merge bound. A nil table reads every position
// as its document's.
func (t *Table) Resource(p Position) (root Position, known bool) {
	if t == nil {
		return Position{Doc: p.Doc}, true
	}
	res, known := t.bases.holding(p)
	return res.root, known
}

// Base returns the URI a reference written in the schema at p resolves
// against, its own identifier counted: its document's, or the resource's an
// identifier at or above it names. Two schemas with one base read every
// reference they write alike. It is false where a schema on the way may hide
// one past the merge bound, and a nil table knows none.
func (t *Table) Base(p Position) (string, bool) {
	if t == nil {
		return "", false
	}
	res, known := t.bases.holding(p)
	return res.uri, known
}

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
