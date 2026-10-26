package bind

import (
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// DefaultLimit bounds the work one table costs when Config.Limit is zero:
// each node read, token walked, hop followed and document opened is a step.
// The source and every document it names are each held to the compiler's
// node budget before they get here, so this is far above what any admitted
// document costs, and only a document built to multiply that work meets it.
const DefaultLimit = 1 << 26

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
	// Limit bounds the work; zero is DefaultLimit.
	Limit int
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

// budget is the work a table may spend, and what it has.
type budget struct {
	spent, limit int
}

// spend charges n steps and reports whether the budget still holds.
func (w *budget) spend(n int) bool {
	w.spent += n
	return w.spent <= w.limit
}

// over reports whether the budget has run out.
func (w *budget) over() bool { return w.spent > w.limit }

// binder is the state of one table's building.
type binder struct {
	cfg       Config
	docs      *docs
	work      *budget
	defs      definitions
	resources resources
	find      *finder
	ids       *identifiers
	hops      map[Position]Hop
	chains    map[Position]Chain
	loops     []Loop
}

// Bind reads every schema reference the source tree src holds, and every
// position a reference reaches through it, into a table, within the budget.
func Bind(src *yaml.Node, cfg Config) *Table {
	if cfg.Limit <= 0 {
		cfg.Limit = DefaultLimit
	}
	work := &budget{limit: cfg.Limit}
	d := newDocs(src, cfg, work)
	b := &binder{cfg: cfg, docs: d, work: work, defs: definitions{docs: d, work: work}, resources: resources{d},
		find: newFinder(d, work), hops: map[Position]Hop{}, chains: map[Position]Chain{}}
	b.find.walk(rootOf(Source, sDocument), false)
	b.ids = b.find.ids
	t := &Table{entries: map[Position]Entry{}, mappings: map[mappingKey]Mapping{}}
	for more := true; more && !work.over(); more = b.find.walkPending() {
		b.settle(t)
	}
	b.placeLoops()
	return b.table(t)
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
		c := b.chain(site)
		t.entries[site] = Entry{Site: site, Hop: b.hops[site], Chain: c}
	}
	slices.SortFunc(keys, func(x, y mappingKey) int {
		if c := b.docs.compare(x.at, y.at); c != 0 {
			return c
		}
		return cmp.Compare(x.value, y.value)
	})
	for _, k := range keys {
		t.mappings[k] = b.mapping(k)
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
	t.loops, t.steps, t.limit, t.exhausted = b.loops, b.work.spent, b.work.limit, b.work.over()
	t.ends, t.roots = b.find.ends, b.find.roots
	for _, d := range b.docs.list {
		t.docs = append(t.docs, Document{Name: d.name, Root: d.root})
		t.truncated = t.truncated || d.view.Exhausted()
	}
	reach := b.attribute(t)
	for at := range b.find.landings {
		site, reached := reach[at]
		t.landings = append(t.landings, Landing{At: at, Name: t.docs[at.Doc].Name, Site: site, Reached: reached})
	}
	slices.SortFunc(t.landings, func(x, y Landing) int { return t.compare(x.At, y.At) })
	return t
}

// attribute returns, for each position some $ref's or mapping value's chain
// reaches, the least such position, in whichever document. Starting from each
// in order, a chain is followed until it meets a position a lesser start
// reached, whose chain on from there that start has marked, so each position
// is marked once.
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

// Document is a document a table read: its name and its root node.
type Document struct {
	Name string
	Root *yaml.Node
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
	limit     int
	exhausted bool
	truncated bool
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

// Steps reports the work the table cost.
func (t *Table) Steps() int { return t.steps }

// Limit reports the work the table was allowed (Config.Limit).
func (t *Table) Limit() int { return t.limit }

// Exhausted reports whether the budget ran out, so that a site may be missing
// or answered OverBudget.
func (t *Table) Exhausted() bool { return t.exhausted }

// Truncated reports whether a mapping the table read merges through `<<`
// deeper than the view expands (nodeview.MergeDepthLimit), so that a site past
// the bound may be missing, and a position past it read as holding nothing.
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
