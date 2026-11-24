// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

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
	// Map returns where the document a located name names is read: a location
	// the caller put its URL at, else name itself. It is asked where a document
	// is opened and never where an identifier is resolved, so an $id or a
	// $self keeps the URI it writes, and a reference relative to one is read
	// against that URI. Nil maps nothing.
	Map func(name string) string
	// Open returns the tree of the document named name, as a reference
	// written in the document named from reads it, under the compile's
	// budgets, or an *UnparsableError for one that was read and does not
	// parse, ErrEmptyDocument for one that holds nothing, or a *BarredError
	// for one refused to from alone. It is asked again of a document read
	// before, for each asker read over http(s), which it may refuse. Nil
	// reads no other document.
	Open func(name, from string) (*yaml.Node, error)
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
	// and none of these, as a reference (ReferenceTargetTable.Mapping).
	Components []string
	// SecuritySchemes is the keys of the source's securitySchemes components.
	// A Security Requirement key spelling one names that component, in
	// whichever document it is written; where KeysByURI holds for the document
	// writing it, the table reads any other as a reference to a Security
	// Scheme Object, written where the key is (ReferenceTargetTable.Reference).
	SecuritySchemes []string
	// KeysByURI reports whether the document named name reads a requirement
	// key as a URI, as OpenAPI 3.2 does. Nil reads none so.
	KeysByURI func(name string) bool
	// pushed, where set, is told each position a walk pushes a frame for, and
	// whether a mapping holds it, whose key is a node at that pointer too. Only
	// this package's tests set it, to total the pointers a table's walks spell
	// (Config.Spelling).
	pushed func(at DocumentPointer, member bool)
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
// once against the URIs its identifiers hold (identifiers.commit). reads is
// who asked for each document, the pass's held reads among them, and wait
// what waits for those documents. committed reports that a drain of the pass has
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
	reads     *reads
	wait      *waits
	hops      map[DocumentPointer]Hop
	chains    map[DocumentPointer]Chain
	objs      *objects
	dyn       *dynamicRefs
	loops     []Loop
	committed bool
	stale     bool
	// grew says the pass's last drain reached a document no drain before it
	// had.
	grew bool
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
func Bind(ctx context.Context, src *yaml.Node, cfg Config) *ReferenceTargetTable {
	workValue := &work{ctx: ctx, steps: budget.New(cfg.Limit)}
	docs := newDocs(src, cfg, workValue)
	ids := newIdentifiers(resources{docs}.document(Source))
	asked := newReads()
	for passes := 1; ; passes++ {
		b := newBinder(cfg, docs, workValue, ids, asked)
		referenceTargetTable := run(b)
		if !reread(b) {
			referenceTargetTable.passes = passes
			return b.table(referenceTargetTable)
		}
	}
}

// newBinder returns a pass over docsValue, knowing what ids holds and who asked
// for what (asked), and nothing else.
func newBinder(cfg Config, docsValue *docs, workValue *work, ids *identifiers, asked *reads) *binder {
	docsValue.reached, docsValue.fresh = map[DocID]bool{Source: true}, nil
	clear(asked.held)
	b := &binder{cfg: cfg, docs: docsValue, work: workValue, defs: definitions{docs: docsValue, work: workValue}, resources: resources{docsValue},
		find: newFinder(docsValue, workValue, ids), ids: ids, reads: asked, wait: newWaits(), hops: map[DocumentPointer]Hop{},
		chains: map[DocumentPointer]Chain{}, dyn: newDynamicRefs()}
	b.objs = &objects{b: b, hops: map[DocumentPointer]objectHop{}, chains: map[objectKey]Chain{}}
	return b
}

// run answers every reference the pass b reaches, going on past what it held
// for as long as that changes no answer it gave (goOn). A site still waiting
// when the pass ends, as the budget or a change stopped it, has no entry: the
// table says why (ReferenceTargetTable.Unread), or the pass is read again.
func run(b *binder) *ReferenceTargetTable {
	b.find.walk(rootOf(Source, sDocument), false)
	b.ids.begin()
	referenceTargetTable := &ReferenceTargetTable{entries: map[DocumentPointer]ReferenceSite{}, mappings: map[mappingKey]Mapping{}, references: map[objectKey]Reference{}}
	for {
		for more := true; more && !b.work.over(); more = b.find.walkPending() {
			b.settle(referenceTargetTable)
		}
		if b.work.over() || !goOn(b) {
			break
		}
	}
	b.placeLoops()
	return referenceTargetTable
}

// goOn reports whether the pass b, its walks done, goes on. It does while
// what the documents it reached declare changes no URI whose answer it kept
// (settled): to read the documents it held (readHeld), to ask again the hops
// that wait for a declaration of a document read and not yet reached, or,
// once nothing is held or waits so, to settle what waited. A change ends it,
// to be read again. Each round is a step of the work, so one that changes
// nothing ends on the budget rather than going on unbounded.
func goOn(b *binder) bool {
	b.work.steps.Spend(1)
	if !settled(b) {
		b.stale = true
		return false
	}
	switch {
	case len(b.reads.held) > 0:
		readHeld(b)
	case len(b.wait.held) > 0 || len(b.wait.heldObjects) > 0 || len(b.wait.heldMappings) > 0:
		// What waits, waits for a declaration of a document read and not yet
		// reached: while a drain reaches one, ask again. Once one reaches none,
		// the documents declaring what waits are reached, as a read reaches the
		// document it reads; once that reaches none either, what still waits is
		// read as a location.
		b.wait.unwaited = b.wait.unwaited || !b.grew && !reachDeclaring(b)
	default:
		return b.wait.release(b.find)
	}
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
	b.grew = len(fresh) > 0
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
// its answers stand, so no pass begins with nothing to spend. Only a location
// whose own content leads to a declaration of its URI reads a pass again: a
// reference a declaration may answer waits for it (goOn).
func reread(b *binder) bool {
	return b.stale && !b.work.over()
}

// readHeld reads the documents the pass b held back, as no document it read
// answers for their names (OAS 3.1 §4.3.1), in a drain's two tiers
// (reads.drain): a URL a document open declares to its asker is not read
// while what waits for that declaration may wait.
func readHeld(b *binder) {
	b.reads.drain(b.docs, b.find.identifyAll, func(uri string, from DocID) bool {
		return !b.wait.unwaited && declaredFor(b, uri, from)
	})
}

// reachDeclaring reaches each document whose declaration a hop waits for, and
// reports whether one was not reached before.
func reachDeclaring(b *binder) bool {
	reached := false
	for _, id := range b.wait.declaring {
		reached = reached || !b.docs.reached[id]
		b.docs.reach(id)
	}
	return reached
}

// declaredFor reports whether a document open declares uri to the document
// asker (admitted, reads.admits).
func declaredFor(b *binder, uri string, asker DocID) bool {
	for _, documentPointer := range b.ids.declared[uri] {
		if admitted(b.docs, documentPointer.Doc, asker, uri) && b.reads.admits(b.docs, documentPointer.Doc, asker) {
			return true
		}
	}
	return false
}

// rootOf is the root at the root of document id, read at stateValue.
func rootOf(id DocID, stateValue state) root {
	return root{at: DocumentPointer{Doc: id}, state: stateValue}
}

// settle answers every site, mapping value and reference object noted since it
// last ran, in order, so that where the budget runs out does not depend on what
// was noted first. Each is sorted once, in the round that noted it, and costs a
// step, so a round that answered one again would show in the work.
func (b *binder) settle(referenceTargetTable *ReferenceTargetTable) {
	staticSites(b)
	sites, keys, objects := b.find.fresh, b.find.freshMappings, b.find.freshObjects
	b.find.fresh, b.find.freshMappings, b.find.freshObjects = nil, nil, nil
	b.work.spend(len(sites) + len(keys) + len(objects))
	slices.SortFunc(sites, b.docs.compare)
	for _, site := range sites {
		if b.work.cancelled() {
			return
		}
		if resettle(b, site) {
			continue
		}
		if chain, waits := b.chain(site); waits {
			b.wait.sites[site] = true
		} else {
			referenceTargetTable.entries[site] = ReferenceSite{Site: site, Hop: b.hops[site], Chain: chain, Keyword: keywordAt(b, site)}
		}
	}
	slices.SortFunc(keys, mappingOrder(b.docs))
	for _, mappingKey := range keys {
		if b.work.cancelled() {
			return
		}
		if mapping, waits := b.mapping(mappingKey); waits {
			b.wait.mappings[mappingKey] = true
		} else {
			referenceTargetTable.mappings[mappingKey] = mapping
		}
	}
	b.objs.settleAll(referenceTargetTable, objects)
	landDynamic(b)
}

// landDynamic lands on what each dynamic reference noted since the table last
// settled names, as the $ref naming its anchor would, for the schema an
// expansion builds where the model holds none: the one schema declaring a
// $dynamicRef's anchor, and the root of a $recursiveRef's resource, which a
// $ref of "#" written there resolves to. staticSites, asking namesDynamic, has
// taken each $dynamicRef that is the $ref it spells, so those left are
// dynamic. A hop left unresolved lands nowhere. A name is asked only of a
// document open, whose identifiers are read whole. It asks no context
// between sites (GitHub #1968).
func landDynamic(b *binder) {
	dynamic, recursive := b.find.freshDynamic, b.find.freshRecursive
	b.find.freshDynamic, b.find.freshRecursive = nil, nil
	for _, dynamicSite := range dynamic {
		name, _, ok := DynamicFragment(dynamicSite.value)
		if !ok {
			continue
		}
		b.find.identify(dynamicSite.at.Doc)
		at := b.ids.dynamic[dynamicKey{doc: dynamicSite.at.Doc, name: name}]
		if len(at) != 1 {
			continue
		}
		path, _ := b.docs.path(dynamicSite.at)
		if hop := b.resolve(dynamicSite.at, path, "#"+url.PathEscape(name), true); hop.Status == Resolved && hop.Target == at[0] {
			b.find.landOn(dynamicSite.at, hop)
		}
	}
	for _, site := range recursive {
		path, _ := b.docs.path(site)
		if hop := b.resolve(site, path, "#", true); hop.Status == Resolved {
			b.find.landOn(site, hop)
		}
	}
}

// mappingOrder orders mapping values by their discriminator's position in
// docsValue, then by value.
func mappingOrder(docsValue *docs) func(mappingKeyValue, otherMappingKey mappingKey) int {
	return func(mappingKeyValue, otherMappingKey mappingKey) int {
		if c := docsValue.compare(mappingKeyValue.at, otherMappingKey.at); c != 0 {
			return c
		}
		return cmp.Compare(mappingKeyValue.value, otherMappingKey.value)
	}
}

// table finishes referenceTargetTable: an entry for each position a chain
// passed that no walk found, which only a budget cutting the walks leaves, the
// loops, the landings, and the verdicts of the budget and of the merge bound.
func (b *binder) table(referenceTargetTable *ReferenceTargetTable) *ReferenceTargetTable {
	for documentPointer, chain := range b.chains {
		if _, found := referenceTargetTable.entries[documentPointer]; !found {
			referenceTargetTable.entries[documentPointer] = ReferenceSite{Site: documentPointer, Hop: b.hops[documentPointer], Chain: chain, Keyword: keywordAt(b, documentPointer)}
		}
	}
	for objectKey, chain := range b.objs.chains {
		if _, found := referenceTargetTable.references[objectKey]; !found {
			_, key := b.docs.keyRefs[objectKey.at]
			referenceTargetTable.references[objectKey] = Reference{Site: objectKey.at, Kind: objectKey.kind, Hop: ofKind(b.objs.hops[objectKey.at], objectKey.kind), Chain: chain,
				IsSecurityRequirementKey: key}
		}
	}
	referenceTargetTable.entryList = slices.SortedFunc(maps.Values(referenceTargetTable.entries), func(referenceSite, referenceSiteValue ReferenceSite) int {
		return b.docs.compare(referenceSite.Site, referenceSiteValue.Site)
	})
	referenceTargetTable.targets = referenceTargetsOf(b, referenceTargetTable)
	referenceTargetTable.roots, referenceTargetTable.bases, referenceTargetTable.ids = b.find.roots, newBases(b.resources, b.ids), b.ids
	shared := sharedNames(b.ids)
	var graph reach
	if len(b.ids.recursive) > 0 || len(shared) > 0 {
		graph = reachOf(b, referenceTargetTable)
	}
	referenceTargetTable.recursive = recursiveAnchorsOf(b.ids, referenceTargetTable.bases, graph)
	reached := dynamicReachOf(b.ids, referenceTargetTable.bases, graph, shared)
	referenceTargetTable.loops, referenceTargetTable.steps, referenceTargetTable.limit, referenceTargetTable.exhausted = b.loops, b.work.steps.Spent(), b.work.steps.Limit(), b.work.steps.Exhausted()
	referenceTargetTable.dynamic = dynamicAnchorsOf(b.ids, referenceTargetTable.exhausted, reached)
	dialects := dialectsByDocument(withLandingRoots(b.find, b.ids.dialects))
	for id, document := range b.docs.list {
		reached := b.docs.reached[DocID(id)]
		referenceTargetTable.docs = append(referenceTargetTable.docs, ReferencedDocument{Name: document.name, Self: document.self, Root: document.root, Unreached: !reached,
			Dialects: dialects[DocID(id)]})
		referenceTargetTable.truncated = referenceTargetTable.truncated || reached && document.view.Exhausted()
	}
	reach := b.attribute(referenceTargetTable)
	for at := range b.find.landings {
		site, reached := reach[at]
		referenceTargetTable.landings = append(referenceTargetTable.landings, Landing{At: at, Name: referenceTargetTable.docs[at.Doc].Name, Site: site, Reached: reached})
	}
	for at, holder := range b.find.tupled {
		if !b.find.landings[at] {
			referenceTargetTable.landings = append(referenceTargetTable.landings, Landing{At: at, Name: referenceTargetTable.docs[at.Doc].Name, Site: holder, Reached: true})
		}
	}
	slices.SortFunc(referenceTargetTable.landings, func(landing, landingValue Landing) int { return b.docs.compare(landing.At, landingValue.At) })
	referenceTargetTable.objLandings = b.objs.landings(referenceTargetTable)
	referenceTargetTable.refList = slices.SortedFunc(maps.Values(referenceTargetTable.references), func(reference, referenceValue Reference) int {
		return compareObjects(b.docs)(objectKey{at: reference.Site, kind: reference.Kind}, objectKey{at: referenceValue.Site, kind: referenceValue.Kind})
	})
	return referenceTargetTable
}

// referenceTargetsOf returns each position a schema reference of
// referenceTargetTable lands on: a $ref's hop where it lands
// (ReferenceSite.Lands), each position a chain passes writing a $ref of its
// own, so every hop of a chain is one; a discriminator mapping value's hop; and
// the target of an expanding $dynamicRef or $recursiveRef.
func referenceTargetsOf(b *binder, referenceTargetTable *ReferenceTargetTable) map[DocumentPointer]bool {
	targets := map[DocumentPointer]bool{}
	for _, referenceSite := range referenceTargetTable.entries {
		if referenceSite.Lands() {
			targets[referenceSite.Hop.Target] = true
		}
	}
	for _, mapping := range referenceTargetTable.mappings {
		if mapping.Hop.Status == Resolved {
			targets[mapping.Hop.Target] = true
		}
	}
	for _, expansion := range b.find.expansions {
		targets[expansion.to] = true
	}
	return targets
}

// attribute returns, for each position some $ref's, mapping value's or
// expanding $dynamicRef's chain reaches, the least such position, in whichever
// document. Starting from each in order, a chain is followed until it meets a
// position a lesser start reached, whose chain on from there that start has
// marked, so each position is marked once.
func (b *binder) attribute(referenceTargetTable *ReferenceTargetTable) map[DocumentPointer]DocumentPointer {
	type start struct {
		from, first DocumentPointer
	}
	var starts []start
	add := func(from DocumentPointer, hop Hop) {
		if hop.Status == Resolved {
			starts = append(starts, start{from: from, first: hop.Target})
		}
	}
	for _, referenceSite := range referenceTargetTable.entries {
		add(referenceSite.Site, referenceSite.Hop)
	}
	for _, mapping := range referenceTargetTable.mappings {
		add(mapping.At, mapping.Hop)
	}
	for _, expansion := range b.find.expansions {
		add(expansion.from, Hop{Status: Resolved, Target: expansion.to})
	}
	slices.SortFunc(starts, func(startValue, otherStart start) int {
		if c := b.docs.compare(startValue.from, otherStart.from); c != 0 {
			return c
		}
		return b.docs.compare(startValue.first, otherStart.first)
	})
	reach := map[DocumentPointer]DocumentPointer{}
	for _, start := range starts {
		for documentPointer := start.first; ; {
			if _, seen := reach[documentPointer]; seen {
				break
			}
			reach[documentPointer] = start.from
			hop, ok := b.hops[documentPointer]
			if !ok || hop.Status != Resolved {
				break
			}
			documentPointer = hop.Target
		}
	}
	return reach
}

// ReferencedDocument is a document a table read: its name and its root node.
// Unreached reports that only a pass that did not stand asked for it, so that
// nothing the table holds is in it: a reference read it as a location before a
// later document declared that URI. Dialects is each schema resource root in it
// that names its dialect, in pointer order.
type ReferencedDocument struct {
	Name string
	// Self is the URI the document's $self names, resolved against Name, or
	// empty for none: a second name of the document, and its base.
	Self      string
	Root      *yaml.Node
	Dialects  []DialectRoot
	Unreached bool
}

// ReferenceTargetTable is every reference a source holds, a schema's $ref or a
// Reference Object's, where each lands and where its chain ends, the loops
// among them, and what the reading cost. It is built once, and no answer it
// gives changes after: only its base lookups write, to remember the walks they
// make (bases), so one is not safe for concurrent use. Each list it gives is
// sorted once, as it is built, in the order docs.compare reads positions in.
type ReferenceTargetTable struct {
	entries     map[DocumentPointer]ReferenceSite
	references  map[objectKey]Reference
	roots       map[DocumentPointer]state
	mappings    map[mappingKey]Mapping
	loops       []Loop
	landings    []Landing
	objLandings []ObjectLanding
	entryList   []ReferenceSite
	refList     []Reference
	targets     map[DocumentPointer]bool
	docs        []ReferencedDocument
	steps       int
	passes      int
	cost
	// bases remembers each walk Base and Resource make, filled as they
	// ask. It changes no answer, each being what a walk from the root gives,
	// and nothing guards it, as only the lowering asks, on one goroutine.
	bases     *bases
	ids       *identifiers
	recursive map[DocID]recursiveAnchors
	dynamic   map[dynamicKey]DynamicAnchor
}

// Entry returns the entry of the $ref written at site, and false for a
// position the table read no $ref at: one writing none, or one the budget
// left unread. A nil table read none.
func (t *ReferenceTargetTable) Entry(site DocumentPointer) (ReferenceSite, bool) {
	if t == nil {
		return ReferenceSite{}, false
	}
	referenceSite, ok := t.entries[site]
	return referenceSite, ok
}

// Entries returns every entry, ordered by site.
func (t *ReferenceTargetTable) Entries() []ReferenceSite { return slices.Clone(t.entryList) }

// Referenced reports whether a schema reference the table read names at: a
// $ref's hop, a discriminator mapping value's or an expanding dynamic
// reference's lands there, so the node built at at is what that reference
// reads. A nil table read none.
func (t *ReferenceTargetTable) Referenced(at DocumentPointer) bool {
	return t != nil && t.targets[at]
}

// Mapping returns where the mapping value written by the discriminator at at
// lands, and false for one the table did not read: a value naming a schema
// component of the source (Config.Components), or one it left unread (Unread).
func (t *ReferenceTargetTable) Mapping(at DocumentPointer, value string) (Mapping, bool) {
	mapping, ok := t.mappings[mappingKey{at: at, value: value}]
	return mapping, ok
}

// Loops returns every loop of $refs.
func (t *ReferenceTargetTable) Loops() []Loop { return slices.Clone(t.loops) }

// Landings returns every landing, ordered by position.
func (t *ReferenceTargetTable) Landings() []Landing { return slices.Clone(t.landings) }

// Document returns the document id names.
func (t *ReferenceTargetTable) Document(id DocID) ReferencedDocument { return t.docs[id] }

// Documents reports how many documents the table read, the source included.
func (t *ReferenceTargetTable) Documents() int { return len(t.docs) }

// Reference returns the entry of the Reference Object written at site, read
// as kind, and false for a position the table read no such reference at: one
// writing no $ref, one no walk read as that kind, or one the budget left
// unread.
func (t *ReferenceTargetTable) Reference(site DocumentPointer, kind RefKind) (Reference, bool) {
	reference, ok := t.references[objectKey{at: site, kind: kind}]
	return reference, ok
}

// References returns every Reference Object's entry, ordered by site, then
// kind.
func (t *ReferenceTargetTable) References() []Reference { return slices.Clone(t.refList) }

// ObjectLandings returns every object landing, ordered by position, then
// kind.
func (t *ReferenceTargetTable) ObjectLandings() []ObjectLanding { return slices.Clone(t.objLandings) }

// DynamicAnchor is what the $dynamicAnchor declarations of one name in one
// document say: the first in pointer order, how many there are, whether a
// resource of another document declaring the name reaches that one, and
// whether that is all known: false where the budget or a merge past the bound
// may have hidden a declaration or a path, so a count of one proves nothing.
type DynamicAnchor struct {
	Site     DocumentPointer
	Count    int
	Reached  bool
	Complete bool
}

// DynamicAnchor returns what document id's declarations of the $dynamicAnchor
// name say, sorted once as the table was built. A nil table knows of none.
func (t *ReferenceTargetTable) DynamicAnchor(id DocID, name string) DynamicAnchor {
	if t == nil {
		return DynamicAnchor{Complete: true}
	}
	if a, ok := t.dynamic[dynamicKey{doc: id, name: name}]; ok {
		return a
	}
	return DynamicAnchor{Complete: !t.exhausted && !t.ids.truncated[id]}
}

// dynamicAnchorsOf returns what each name's declarations in each document say
// (ReferenceTargetTable.DynamicAnchor), each name's sorted once, and whether
// another document's declaring resource reaches the first, as reached read it.
func dynamicAnchorsOf(ids *identifiers, exhausted bool, reached dynamicReach) map[dynamicKey]DynamicAnchor {
	out := make(map[dynamicKey]DynamicAnchor, len(ids.dynamic))
	for dynamicKey, sites := range ids.dynamic {
		out[dynamicKey] = DynamicAnchor{Site: slices.MinFunc(sites, comparePositions), Count: len(sites),
			Reached: reached.by[dynamicKey], Complete: !exhausted && !ids.truncated[dynamicKey.doc] && !reached.unknown[dynamicKey]}
	}
	return out
}

// RecursiveAnchor reports what the schemas declaring `$recursiveAnchor` true,
// where their dialect defines the keyword (DialectRule.Recursive), say of a
// $recursiveRef reaching root, a resource's root: whether root declares it,
// whether a schema resource other than root's holding a declaration, in any
// document, reaches it (reach), so is in its dynamic scope, and whether both
// are known, as DynamicAnchor says, each declaration's resource and the
// resources on the way included. A nil table knows of none.
func (t *ReferenceTargetTable) RecursiveAnchor(root DocumentPointer) (declared, reached, known bool) {
	if t == nil {
		return false, false, true
	}
	recursiveAnchors := t.recursive[root.Doc]
	return recursiveAnchors.sites[root], recursiveAnchors.reached[root], !t.exhausted && !t.ids.truncated[root.Doc] && !recursiveAnchors.unplaced
}

// recursiveAnchors is what the `$recursiveAnchor: true` declarations of one
// document say (ReferenceTargetTable.RecursiveAnchor): where they are, which of
// the resources holding them another such resource reaches, and whether one
// lies where a merge past the bound may hide the resource it is in, or a
// resource on the way.
type recursiveAnchors struct {
	sites    map[DocumentPointer]bool
	reached  map[DocumentPointer]bool
	unplaced bool
}

// recursiveAnchorsOf returns what the declarations ids holds say, by document,
// the resource of each read from b once, and which resource another declaring
// one reaches read over reachValue in one walk, so a $recursiveRef asks a
// lookup rather than a read of every declaration.
func recursiveAnchorsOf(ids *identifiers, b *bases, reachValue reach) map[DocID]recursiveAnchors {
	out := make(map[DocID]recursiveAnchors, len(ids.recursive))
	var origins []DocumentPointer
	for id, sites := range ids.recursive {
		recursiveAnchorsValue := recursiveAnchors{sites: make(map[DocumentPointer]bool, len(sites)), reached: map[DocumentPointer]bool{},
			unplaced: !reachValue.known}
		for _, site := range sites {
			recursiveAnchorsValue.sites[site] = true
			if !reachValue.known {
				continue // no verdict is known, so none is read
			}
			res, known := b.holding(site)
			recursiveAnchorsValue.reached[res.root] = false
			recursiveAnchorsValue.unplaced = recursiveAnchorsValue.unplaced || !known
			origins = append(origins, res.root)
		}
		out[id] = recursiveAnchorsValue
	}
	reached := reachValue.reachers(origins)
	for _, recursiveAnchors := range out {
		for root := range recursiveAnchors.reached {
			recursiveAnchors.reached[root] = reached(root)
		}
	}
	return out
}

// Resource returns the root of the schema resource the schema at
// documentPointer lies in, its own identifier counted: the schema whose
// identifier starts it, or its document's root where none on the way does. It
// is false where a schema on the way may hide one past the merge bound. A nil
// table reads every position as its document's.
func (t *ReferenceTargetTable) Resource(documentPointer DocumentPointer) (root DocumentPointer, known bool) {
	if t == nil {
		return DocumentPointer{Doc: documentPointer.Doc}, true
	}
	res, known := t.bases.holding(documentPointer)
	return res.root, known
}

// Base returns the URI a reference written in the schema at documentPointer
// resolves against, its own identifier counted: its document's, or the
// resource's an identifier at or above it names. Two schemas with one base read
// every reference they write alike. It is false where a schema on the way may
// hide one past the merge bound, and a nil table knows none.
func (t *ReferenceTargetTable) Base(documentPointer DocumentPointer) (string, bool) {
	if t == nil {
		return "", false
	}
	res, known := t.bases.holding(documentPointer)
	return res.uri, known
}

// cost is what reading a table cost: the work it was allowed, and whether the
// budget or a merge past the bound cut the reading short. The table embeds it,
// so its verdicts are the table's.
type cost struct {
	limit     budget.Limit
	exhausted bool
	truncated bool
}

// Limit reports the work the table was allowed (Config.Limit).
func (c cost) Limit() budget.Limit { return c.limit }

// Exhausted reports whether the budget ran out, so that a site may be missing
// or answered OverBudget.
func (c cost) Exhausted() bool { return c.exhausted }

// Truncated reports whether a mapping of a document the table reached merges
// through `<<` deeper than the view expands (nodeview.MergeDepthLimit), so that
// a site past the bound may be missing, and a position past it read as holding
// nothing.
func (c cost) Truncated() bool { return c.truncated }

// Unread returns why a schema $ref the table holds no entry for went unread:
// the budget ran out, or a merge past the bound hid it. It is empty when
// neither happened, and every $ref was read, or for a nil table, which read
// none.
func (t *ReferenceTargetTable) Unread() string {
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
