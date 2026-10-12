package load

import (
	"context"
	"encoding/json/jsontext"
	"reflect"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// MappingTargets holds the schema each discriminator mapping target in the
// source names, by the pointer it names, resolved at load as a $ref to it is
// (GitHub #757). The resolver follows no mapping value, so a schema the model
// holds as raw YAML, such as an extension's value or an enum member, was
// otherwise found only once a $ref had lowered it. Its zero value holds none.
//
// It also holds the schema walked at each raw position (see arrive), so every
// reference to one reaches the object whose own $refs were resolved, and its
// index of raw YAML (see Holds).
type MappingTargets struct {
	byPointer map[jsontext.Pointer]*schemaRef
	built     map[jsontext.Pointer]*schemaRef
	reads     *treeReads
}

// At returns the schema a mapping target resolved to at pointer, or nil.
func (t MappingTargets) At(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return t.byPointer[pointer]
}

// Built returns the schema the load phase walked at a raw position, or nil.
// A $ref spelled with the source's file name builds a copy of what it names,
// and the walk resolves the $refs in one object per position alone.
func (t MappingTargets) Built(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return t.built[pointer]
}

// Holds is resolve.Scope.Holds, answered from the index the targets were read
// through, which builds each mapping's index once, whoever asks. The zero value
// answers from an index of its own each time.
func (t MappingTargets) Holds(raw *yaml.Node, token string) bool {
	reads := t.reads
	if reads == nil {
		reads = newTreeReads()
	}
	return reads.holds(raw, token)
}

// maxMappingWork bounds the steps resolving the mapping targets takes: each
// entry it reads, object it reaches, walk item it reads, and hop of a chain it
// reads and position it resolves, priced as the library's read of each (see
// priced). Each is done once, so the steps follow what the library builds for
// the targets, which is what a $ref to each would build.
const maxMappingWork = 1 << 28

// farHops is a distance past maxResolutionHops, at which chainEnds remembers a
// chain no nearer its end: one that never ends is as far. Saturating there keeps
// "ends within maxResolutionHops reads" exact for every hop before it.
const farHops = maxResolutionHops + 1

// mappings resolves, at load, the targets of every discriminator mapping in the
// source, and the schema $refs inside the raw objects it builds, so what the
// lowering reads does not follow what it lowered first. An entry is a reference
// wherever it is written, as a $ref is: what it reaches is resolved and
// reported, whether or not the lowering reads the position that holds it.
//
// The walk shows it every discriminator and every reference's chain (see,
// enqueue), and resolve reads the rest.
type mappings struct {
	self   sourceDocument
	doc    *soa.OpenAPI
	opts   references.ResolveOptions
	reader *external
	// view reads the $ref a raw node carries, when a chain's end is asked after.
	view *nodeview.View
	// reads prices the reads of the source the work makes (see priced).
	reads *treeReads
	// ends holds, for each hop chainEnds read, the reads from it to its chain's
	// end, at most farHops.
	ends map[hop]int
	// model holds each object the walk saw, and modelAt each site it saw one of
	// a kind at; walked holds each position and kind of other object arrived at.
	model   map[any]bool
	modelAt map[builtKey]bool
	walked  map[builtKey]bool
	// built holds the schema walked at each raw position, which a reference to
	// the position is to reach whatever object its own resolution built.
	built map[jsontext.Pointer]*schemaRef
	// named holds each entry read.
	named     map[jsontext.Pointer]bool
	queue     []arrival
	order     []jsontext.Pointer
	byPointer map[jsontext.Pointer]*target
	out       map[jsontext.Pointer]*schemaRef
	// work counts the steps taken, against limit. resolutions counts the
	// positions resolved: one each, however many entries name it.
	work, limit, resolutions int
}

// arrival is an object a reference's chain reaches in the source, and the site
// whose work reached it.
type arrival struct {
	site   jsontext.Pointer
	record record
}

// target is a position mapping entries name: the least entry naming it, and,
// once resolved, what its resolution drew and the nodes it reached that hold
// no schema (see noSchemaNodes).
type target struct {
	least    jsontext.Pointer
	resolved bool
	trail    trail
	found    []error
	noSchema map[*yaml.Node]bool
}

// newMappings returns the resolver of the mapping targets of doc, the model of
// self, which resolves each with opts, through reader when external references
// are allowed.
func newMappings(self sourceDocument, doc *soa.OpenAPI, opts references.ResolveOptions, reader *external) *mappings {
	return &mappings{self: self, doc: doc, opts: opts, reader: reader, view: nodeview.New(), reads: newTreeReads(),
		ends: map[hop]int{}, model: map[any]bool{}, modelAt: map[builtKey]bool{}, walked: map[builtKey]bool{},
		built: map[jsontext.Pointer]*schemaRef{},
		named: map[jsontext.Pointer]bool{}, byPointer: map[jsontext.Pointer]*target{},
		out: map[jsontext.Pointer]*schemaRef{}, limit: maxMappingWork}
}

// targets returns what resolve resolved.
func (m *mappings) targets() MappingTargets {
	return MappingTargets{byPointer: m.out, built: m.built, reads: m.reads}
}

// see notes a model the walk reached at site: a discriminator's entries are
// collected, and an object that may hold a reference is noted as the model's.
func (m *mappings) see(site jsontext.Pointer, model any) {
	switch v := model.(type) {
	case *oas3.Discriminator:
		m.collect(site, v)
	case resolvable:
		m.model[v] = true
		m.modelAt[builtKey{pointer: site, kind: reflect.TypeOf(v)}] = true
	default:
		// Any other kind of model holds no mapping and resolves nothing.
	}
}

// resolve reads each object a reference's chain reaches and resolves each
// position an entry names, until neither adds work, and notes in found what
// each resolution drew. Each object is read once and each position resolved
// once, every step charged to limit.
//
// A panic stops it, reported as an error naming site, the entry or reference
// whose work was running, as a panic in resolveWith's walk is.
func (m *mappings) resolve(ctx context.Context, found *reachedFindings) (site jsontext.Pointer, err error) {
	defer recovered(&err, "discriminator mapping resolver")
	defer m.note(found)
	for q, t := 0, 0; m.spend(1); {
		switch {
		case q < len(m.queue):
			site = m.queue[q].site
			m.arrive(ctx, found, m.queue[q])
			q++
		case t < len(m.order):
			site = m.byPointer[m.order[t]].least
			m.resolveTarget(ctx, m.order[t])
			t++
		default:
			return "", nil
		}
	}
	return "", nil
}

// spend charges n steps, and reports whether they are within limit.
func (m *mappings) spend(n int) bool {
	m.work += n
	return m.work <= m.limit
}

// exhausted reports the step limit crossed, at the document rather than a
// position: which position crossed it follows declaration order.
func (m *mappings) exhausted(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	if m.work <= m.limit {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, at(""),
		"resolving the discriminator mapping targets takes more than %d steps; "+
			"those past them are found only as the lowering meets them", m.limit)}
}

// note notes in found what each position's resolution drew, at the least
// entry naming it, so the entries naming one position place what its one
// resolution drew whichever came first.
func (m *mappings) note(found *reachedFindings) {
	for _, pointer := range m.order {
		if t := m.byPointer[pointer]; t.resolved {
			found.noteEntry(t.least, t.trail, t.found, t.noSchema)
		}
	}
}

// arrive reads the object q reaches, once per position and kind, when it is
// built from raw YAML and is the source's: the walk read what the model holds,
// and past a $ref into another document a pointer reaches that document's
// content, though the resolver reports the source (GitHub #762).
//
// A $ref naming the source by its file name reaches a new copy of what it
// names each time. One of what the model holds there, as that kind, is read
// as the model's own: walked as raw YAML, a chain of components each naming
// the next walked every copy.
func (m *mappings) arrive(ctx context.Context, found *reachedFindings, q arrival) {
	at := builtKey{pointer: q.record.pointer, kind: reflect.TypeOf(q.record.object)}
	if m.model[q.record.object] || m.modelAt[at] || m.walked[at] {
		return
	}
	m.walked[at] = true
	if positionScope(m.doc, m.reads).At(at.pointer).Foreign {
		return
	}
	if js, ok := q.record.object.(*schemaRef); ok && !namesEmptyKey(at.pointer) &&
		readsAsSchema(nodeview.Deref(js.GetRootNode())) {
		m.built[at.pointer] = js
	}
	m.walk(ctx, found, at.pointer, q.record)
}

// positionScope is the scope arrive reads a position of doc in: each reference
// a walk passes is followed as the resolver follows it, and a mapping a walk
// leaves the model for at one is read through reads' index (see
// resolve.Scope.Holds), the one the mappings' pricing reads through too.
func positionScope(doc *soa.OpenAPI, reads *treeReads) refscope.Scope {
	return refscope.Scope{Doc: doc, Ends: refscope.ReferenceEnd, Holds: reads.holds}
}

// builtKey is an object's kind and the position it is at.
type builtKey struct {
	pointer jsontext.Pointer
	kind    reflect.Type
}

// walk reads the object r reached at base, built from raw YAML, which the
// model holds none of: each discriminator and schema $ref in it, at its
// location under base.
//
// A schema is walked as one: the document's walk copies each item's whole
// location, which costs a deep object its depth again at every item.
func (m *mappings) walk(ctx context.Context, found *reachedFindings, base jsontext.Pointer, r record) {
	if js, ok := r.object.(*schemaRef); ok {
		for item := range oas3.Walk(ctx, js) {
			at := func() jsontext.Pointer { return jsontext.Pointer(item.Location.ToJSONPointer()) }
			// Match returns only what its matcher does, and this one never fails.
			_ = item.Match(oas3.SchemaMatcher{Any: func(model any) error {
				m.read(ctx, found, base, at, model)
				return nil
			}})
		}
		return
	}
	for item := range r.walk(ctx) {
		at := func() jsontext.Pointer { return jsontext.Pointer(item.Location.ToJSONPointer()) }
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			m.read(ctx, found, base, at, model)
			return nil
		}})
	}
}

// read reads model, which a built object's walk yields at the location at
// returns: a discriminator's entries, or a schema $ref, resolved. One step is
// charged for each, and once the steps are spent nothing more is read, so a
// walk past the bound only finishes its object.
func (m *mappings) read(ctx context.Context, found *reachedFindings, base jsontext.Pointer,
	at func() jsontext.Pointer, model any,
) {
	if !m.spend(1) {
		return
	}
	switch v := model.(type) {
	case *oas3.Discriminator:
		m.collect(base+at(), v)
	case *schemaRef:
		if v.IsReference() && !v.IsResolved() {
			m.resolveNested(ctx, found, base+at(), v)
		}
	default:
		// Nothing else holds a mapping or a reference this resolves.
	}
}

// collect reads the entries of the discriminator d at site.
func (m *mappings) collect(site jsontext.Pointer, d *oas3.Discriminator) {
	for tag, value := range d.GetMapping().All() {
		m.name(site.AppendToken("mapping").AppendToken(tag), value)
	}
	if value := d.GetDefaultMapping(); value != "" {
		m.name(site.AppendToken("defaultMapping"), value)
	}
}

// name reads the entry at site, written as value: the position it names in
// the source, unless it names none (see pointerOf), to be resolved once, and
// placed at the least entry naming it.
func (m *mappings) name(site jsontext.Pointer, value string) {
	if m.named[site] || !m.spend(1) {
		return
	}
	m.named[site] = true
	pointer, ok := m.pointerOf(value)
	if !ok {
		return
	}
	if t, seen := m.byPointer[pointer]; seen {
		t.least = min(t.least, site)
		return
	}
	m.byPointer[pointer] = &target{least: site}
	m.order = append(m.order, pointer)
}

// resolveTarget resolves pointer as a $ref to it is resolved, and records the
// schema its first hop reaches there, as a $ref to it would lower. A chain that
// fails further on is recorded all the same. One whose first hop reaches no
// schema is recorded nowhere, and its findings go unreported, which say only
// that the position is no schema: the lowering reports every target it cannot
// resolve. One not provably ending is left out (see chainEnds).
func (m *mappings) resolveTarget(ctx context.Context, pointer jsontext.Pointer) {
	ref := oas3.NewJSONSchemaFromReference(references.Reference("#" + fragmentOf(pointer)))
	if !m.chainEnds(ref.GetRef()) {
		return
	}
	if !m.spend(m.priced(string(pointer), true, false)) {
		return
	}
	vErrs, err := ref.Resolve(ctx, m.opts)
	if m.reader != nil {
		vErrs, err = m.reader.settle(ctx, ref, m.opts, vErrs, err)
	}
	m.resolutions++
	c := resolutionChain(ref)
	t := m.byPointer[pointer]
	if declared := firstSchema(c); declared != nil {
		m.out[pointer] = declared
	}
	t.resolved, t.trail, t.found, t.noSchema = true, c.trail(), vErrs, noSchemaNodes(c)
	if err == nil {
		m.enqueue(t.least, c)
	}
}

// firstSchema returns the schema c's first hop reached, or nil when it reached
// none: nothing resolved, or the position holds a value no schema reads as one,
// such as a string, which the library builds a schema from all the same.
func firstSchema(c chain) *schemaRef {
	if len(c.records) == 0 || !c.records[0].reached {
		return nil
	}
	declared, ok := c.records[0].object.(*schemaRef)
	if !ok || !readsAsSchema(nodeview.Deref(declared.GetRootNode())) {
		return nil
	}
	return declared
}

// noSchemaNodes returns the nodes c's hops reached that hold no schema (see
// readsAsSchema). A finding about one says only that, and the lowering reports
// a target or $ref it cannot resolve, so such a finding an entry's resolution
// draws is reported only where a $ref of the model's own drew it too (see
// reachedFindings.noteEntry).
func noSchemaNodes(c chain) map[*yaml.Node]bool {
	noSchema := map[*yaml.Node]bool{}
	for _, r := range c.records {
		if js, ok := r.object.(*schemaRef); ok && r.reached && !readsAsSchema(nodeview.Deref(js.GetRootNode())) {
			noSchema[js.GetRootNode()] = true
		}
	}
	return noSchema
}

// readsAsSchema reports whether node, a schema's source, is a value a schema
// is read from: a mapping, or a boolean. The library builds an empty schema
// from any other, which holds neither side of the union, and IsSchema is true
// of it.
func readsAsSchema(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	return node.Kind == yaml.MappingNode || node.Kind == yaml.ScalarNode && node.Tag == "!!bool"
}

// enqueue queues each object c's hops reached in doc, the source's model, for
// the work at site: one resolved against it once mended. Whether the object is
// the source's content is left to arrive, which asks once a position rather
// than once for each hop of every chain reaching it.
func (m *mappings) enqueue(site jsontext.Pointer, c chain) {
	for _, r := range c.records {
		if _, ok := (*r.document).(*soa.OpenAPI); ok && r.reached {
			m.queue = append(m.queue, arrival{site: site, record: r})
		}
	}
}

// resolveNested resolves the schema $ref js inside a built object, at site, as
// the walk resolves the model's: unless it names another document, which the
// lowering never lowers, or a "#/$defs/..." pointer, which GitHub #557's rule
// reads in the model alone (GitHub #570). A failure is left to the lowering,
// which reports the reference unresolved, and so is a chain that does not
// provably end (see chainEnds). As for a target, a finding about a position
// holding no schema is noted as an entry's (see noSchemaNodes).
func (m *mappings) resolveNested(ctx context.Context, found *reachedFindings, site jsontext.Pointer, js *schemaRef) {
	pointer, ok := sourcePointer(m.self.path, string(js.GetRef()))
	if !ok || !m.chainEnds(js.GetRef()) || !m.spend(m.priced(string(pointer), true, false)) {
		return
	}
	readInModel(js, m.doc, m.opts.TargetLocation)
	vErrs, err := js.Resolve(ctx, m.opts)
	if m.reader != nil {
		vErrs, err = m.reader.settle(ctx, js, m.opts, vErrs, err)
	}
	c := resolutionChain(js)
	found.noteEntry(site, c.trail(), vErrs, noSchemaNodes(c))
	if err == nil {
		m.enqueue(site, c)
	}
}

// readInModel has js, a $ref naming the source in an object built from raw
// YAML, resolve against doc, the model, at location, as the lowering reads it.
// The library resolves it against the document its object's own resolution
// read: the model after an internal $ref, the source's tree after one naming
// its file. The two read a pointer through a $ref, or ending in '/',
// differently, so whether it resolved followed which copy was walked first.
func readInModel(js *schemaRef, doc *soa.OpenAPI, location string) {
	if info := js.GetReferenceResolutionInfo(); info != nil && info.Object == nil {
		info.ResolvedDocument, info.AbsoluteDocumentPath = doc, location
	}
}

// chainEnds reports whether the chain of ref, a schema $ref resolved in the
// source, ends within maxResolutionHops reads. The resolver follows a hop it
// resolved before without tracking where the chain has been, so a chain closing
// on such hops recurses until the stack runs out (GitHub #558), and the cycle
// scan misses a cycle in raw YAML or through the source's file name (GitHub
// #768). A mapping names either wherever the input says, so each hop is read as
// the resolver reads it (see hopOf, readAt), once, for a step, and remembered.
// A walk the step bound cuts remembers nothing.
func (m *mappings) chainEnds(ref references.Reference) bool {
	if m.self.root == nil || m.doc == nil {
		return false
	}
	var path []hop
	onPath := map[hop]bool{}
	toEnd, named := farHops, false
	for {
		pointer, ok := m.self.within(ref)
		if !ok {
			break
		}
		named = named || ref.GetURI() != ""
		h := m.hopOf(pointer, ref.GetURI() != "", named)
		if known, seen := m.ends[h]; seen {
			toEnd = known
			break
		}
		if onPath[h] {
			break
		}
		if !m.spend(m.priced(h.pointer, !h.inTree, h.inTree || m.reader != nil)) {
			return false
		}
		onPath[h] = true
		path = append(path, h)
		next, kind := m.readAt(h)
		if kind == hopEnds {
			toEnd = 0
			break
		}
		if kind == hopUnread {
			break
		}
		ref = next
	}
	for i := len(path) - 1; i >= 0; i-- {
		toEnd = min(toEnd+1, farHops)
		m.ends[path[i]] = toEnd
	}
	return toEnd <= maxResolutionHops
}

// hopOf returns the hop a chain reads at pointer, from a reference with a
// document part or not (byName), after a hop naming the source by its file name
// or not (named). Without external references, every hop from the first so
// named on reads the tree. With them, a hop so named reads the tree and any
// other the model, checked against the tree (see readAt), whatever came
// before: inTree says which reading the hop takes.
func (m *mappings) hopOf(pointer string, byName, named bool) hop {
	if m.reader != nil {
		return hop{pointer: pointer, inTree: byName}
	}
	return hop{pointer: pointer, inTree: named}
}

// readAt reads the hop h with the call the resolver makes (readHop), in the
// source's tree or its model. With external references allowed, a hop read in
// the model is read in the tree too, and is no answer where the two find
// different $refs, or one finds none: the resolver answers a hop from its cache
// first, and a reference back into the source through the tree it holds can
// leave a copy built from the tree there, so either reading may be the one it
// follows.
func (m *mappings) readAt(h hop) (references.Reference, hopKind) {
	if h.inTree {
		return readHop(m.view, m.self.root, h.pointer)
	}
	next, kind := readHop(m.view, m.doc, h.pointer)
	if m.reader == nil {
		return next, kind
	}
	if inTree, _ := readHop(m.view, m.self.root, h.pointer); inTree != next {
		return "", hopUnread
	}
	return next, kind
}

// priced returns the steps of reading pointer in the source as the library
// does, in its model, its tree, or both (see readAt), with a step for the read
// and the keys indexed to count it. A position the model holds, such as a
// component, costs a step a token however wide its map; raw YAML costs the
// keys the library compares (see treeReads), along its own reading of them, so
// a key written twice cannot steer the price off the mapping it scans (GitHub
// #777).
func (m *mappings) priced(pointer string, model, tree bool) int {
	steps := 1
	if model {
		steps += m.reads.modelCost(m.doc, jsontext.Pointer(pointer), m.limit-m.work)
	}
	if tree {
		read, _ := m.reads.cost(m.self.root, pointer, m.limit-m.work)
		steps += read
	}
	return steps + m.reads.drain()
}

// pointerOf returns the position target names in the source, and whether it
// names one: as the lowering reads it (refscope.Scope.InternalPointer), so the
// targets resolved here are exactly those it asks DeclaredAt about. A declared
// component's name is the component, and a "#/$defs/..." pointer is the
// definition the lowering reads relative to the discriminator (GitHub #557),
// which no $ref to the pointer resolves.
func (m *mappings) pointerOf(target string) (jsontext.Pointer, bool) {
	if _, declared := m.doc.GetComponents().GetSchemas().Get(target); declared {
		return "", false
	}
	pointer, ok := sourcePointer(m.self.path, target)
	return pointer, ok && !namesEmptyKey(pointer)
}

// sourcePointer returns the position ref names in the source at self, as the
// lowering reads it, and whether it names one other than a "#/$defs/..."
// pointer.
func sourcePointer(self, ref string) (jsontext.Pointer, bool) {
	pointer, ok := refscope.Scope{SelfPath: self}.InternalPointer(ref)
	return pointer, ok && !defs.IsPointer(pointer)
}

// namesEmptyKey reports whether pointer ends in '/', so names an empty key,
// which the resolver cannot read: it trims the slash and reads the parent
// (GitHub #770). A mapping value so spelled is left to the lowering, and the
// parent is held as no object walked there.
func namesEmptyKey(pointer jsontext.Pointer) bool {
	return strings.HasSuffix(string(pointer), "/")
}
