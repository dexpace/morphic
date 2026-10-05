package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

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
type MappingTargets struct {
	byPointer map[jsontext.Pointer]*schemaRef
}

// At returns the schema a mapping target resolved to at pointer, or nil.
func (t MappingTargets) At(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return t.byPointer[pointer]
}

// maxMappingWork bounds the steps resolving the mapping targets takes: each
// sighting it sorts, entry it reads, object it reaches, walk item it reads and
// position it resolves. Each object is walked once and each position resolved
// once, so the steps follow what the library builds for the targets, which is
// what a $ref to each would build.
const maxMappingWork = 1 << 24

// mappings resolves, at load, the targets of the discriminators the lowering
// reads, and the schema $refs inside the raw objects it builds, so what the
// lowering reads does not follow what it lowered first. One the lowering
// never reads is left alone: what resolving it found would be reported
// against content nothing lowers.
//
// The walk shows it every model and every reference's chain (see, reached),
// each sighted in the region it is lowered from (see regions). resolve then
// releases each region something the lowering reads reaches.
type mappings struct {
	self   sourceDocument
	doc    *soa.OpenAPI
	opts   references.ResolveOptions
	reader *external
	// view reads the source's tree when a chain's end is asked after.
	view *nodeview.View
	// lowering tracks the region of each model the walk yields.
	lowering *regions
	// model holds each object the walk saw, and modelAt each site it saw one of
	// a kind at; walked holds each position and kind of other object read.
	model   map[any]bool
	modelAt map[builtKey]bool
	walked  map[builtKey]bool
	// refRegions holds, by site, the region of each reference sighted outside
	// the document's own.
	refRegions map[jsontext.Pointer]jsontext.Pointer
	// pending holds each sighting, by region, until a reach releases it.
	pending map[jsontext.Pointer][]sighting
	// released holds each model position reached; named, each entry read.
	released, named map[jsontext.Pointer]bool
	queue           []arrival
	order           []jsontext.Pointer
	byPointer       map[jsontext.Pointer]*target
	out             map[jsontext.Pointer]*schemaRef
	// work counts the steps taken, against limit. resolutions counts the
	// positions resolved: one each, however many entries name it.
	work, limit, resolutions int
}

// sighting is what the walk saw at site that the lowering may read, and the
// region it is lowered from: a discriminator, or the records of the hops a
// reference's chain took in the source.
type sighting struct {
	site, region  jsontext.Pointer
	discriminator *oas3.Discriminator
	records       []record
}

// arrival is an object something the lowering reads reaches in the source, and
// the site whose work reached it.
type arrival struct {
	site   jsontext.Pointer
	record record
}

// target is a position mapping entries name: the least entry naming it, and,
// once resolved, what its resolution drew.
type target struct {
	least    jsontext.Pointer
	resolved bool
	trail    trail
	found    []error
}

// newMappings returns the resolver of the mapping targets of doc, the model of
// self, which resolves each with opts, through reader when external references
// are allowed.
func newMappings(self sourceDocument, doc *soa.OpenAPI, opts references.ResolveOptions, reader *external) *mappings {
	return &mappings{self: self, doc: doc, opts: opts, reader: reader, view: nodeview.New(),
		lowering: newRegions(map[*schemaRef]bool{}),
		model:    map[any]bool{}, modelAt: map[builtKey]bool{}, walked: map[builtKey]bool{},
		refRegions: map[jsontext.Pointer]jsontext.Pointer{},
		pending:    map[jsontext.Pointer][]sighting{}, released: map[jsontext.Pointer]bool{},
		named: map[jsontext.Pointer]bool{}, byPointer: map[jsontext.Pointer]*target{},
		out: map[jsontext.Pointer]*schemaRef{}, limit: maxMappingWork}
}

// targets returns what resolve resolved.
func (m *mappings) targets() MappingTargets {
	return MappingTargets{byPointer: m.out}
}

// hold notes the references refs holds out of the walk.
func (m *mappings) hold(refs []heldRef) {
	for _, r := range refs {
		m.lowering.held[r.js] = true
	}
}

// see notes a model the walk reached at loc, which site names.
func (m *mappings) see(site jsontext.Pointer, loc soa.Locations, model any) {
	s, inner := documentStep(loc)
	region := m.lowering.enter(s, inner, model, func() jsontext.Pointer { return site })
	switch v := model.(type) {
	case *oas3.Discriminator:
		m.pending[region] = append(m.pending[region], sighting{site: site, region: region, discriminator: v})
	case resolvable:
		m.model[v] = true
		m.modelAt[builtKey{pointer: site, kind: reflect.TypeOf(v)}] = true
		// Anything but a schema fails the assertion, and no nil is held.
		js, _ := v.(*schemaRef)
		if region != "" && (v.IsReference() || m.lowering.held[js]) {
			m.refRegions[site] = region
		}
	default:
		// Any other kind of model holds no mapping and resolves nothing.
	}
}

// reached notes the objects c's hops reached in the source, for the reference
// written at site, held until a reach releases the reference's region.
func (m *mappings) reached(site jsontext.Pointer, c chain) {
	if in := inSourceRecords(m.doc, c); len(in) > 0 {
		region := m.refRegions[site]
		m.pending[region] = append(m.pending[region], sighting{site: site, region: region, records: in})
	}
}

// inSourceRecords returns the records of c's hops that reached an object in
// doc, the source's model.
func inSourceRecords(doc *soa.OpenAPI, c chain) []record {
	var in []record
	for _, r := range c.records {
		if inSource(doc, r) {
			in = append(in, r)
		}
	}
	return in
}

// inSource reports whether r's hop reached an object in doc, the source's
// model: one it resolved against once mended, at a pointer whose walk passes no
// $ref into another document. The resolver reports such a pointer resolved
// against the source too, but what it reaches is that document's content
// (refscope.Scope.At, GitHub #762).
func inSource(doc *soa.OpenAPI, r record) bool {
	if _, ok := (*r.document).(*soa.OpenAPI); !ok || !r.reached {
		return false
	}
	return !refscope.Scope{Doc: doc, Ends: referenceEnd}.At(pointerIn(r)).Foreign
}

// referenceEnd returns where the chain of the reference node ends, as the
// records of its hops say once mended, and false for a node that is no
// reference or whose chain reached nothing.
func referenceEnd(node any) (refscope.End, bool) {
	var end refscope.End
	reached := false
	for _, r := range resolutionChain(node).records {
		if r.reached {
			end, reached = refscope.End{Document: *r.document, Path: r.path, Pointer: pointerIn(r)}, true
		}
	}
	return end, reached
}

// pointerIn returns the pointer r's hop reached, in its document. The absolute
// reference holds it decoded already, as the resolver read it, so it is taken
// as written: GetJSONPointer would decode it again, reading a key's '+' as a
// space and its '%41' as 'A'.
func pointerIn(r record) jsontext.Pointer {
	_, pointer, _ := strings.Cut(string(r.target), "#")
	return jsontext.Pointer(pointer)
}

// resolve releases the document's own region, then reads each object what is
// released reaches and resolves each position an entry names, until neither
// adds work, and notes in found what each resolution drew. Each object is read
// once and each position resolved once, every step charged to limit.
//
// A panic stops it, reported as an error naming site, the entry or reference
// whose work was running, as a panic in resolveWith's walk is.
func (m *mappings) resolve(ctx context.Context, found *reachedFindings) (site jsontext.Pointer, err error) {
	defer recovered(&err, "discriminator mapping resolver")
	defer m.note(found)
	m.release("")
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
			found.note(t.least, t.trail, t.found)
		}
	}
}

// release takes every sighting the lowering reads once it reaches the model
// position at pointer: each held in a region at or above pointer, at a site
// at or below it. Each sighting is taken once, and each it sorts is charged.
func (m *mappings) release(pointer jsontext.Pointer) {
	if m.released[pointer] {
		return
	}
	m.released[pointer] = true
	for _, region := range prefixes(pointer) {
		held := m.pending[region]
		if len(held) == 0 {
			continue
		}
		m.work += len(held)
		kept := held[:0]
		for _, s := range held {
			if under(s.site, pointer) {
				m.take(s)
				continue
			}
			kept = append(kept, s)
		}
		m.pending[region] = kept
	}
}

// prefixes returns pointer and each position holding it, the document's last.
func prefixes(pointer jsontext.Pointer) []jsontext.Pointer {
	out := []jsontext.Pointer{pointer}
	for i := len(pointer) - 1; i >= 0; i-- {
		if pointer[i] == '/' {
			out = append(out, pointer[:i])
		}
	}
	return out
}

// under reports whether site is pointer or a position below it.
func under(site, pointer jsontext.Pointer) bool {
	return pointer == "" || site == pointer || strings.HasPrefix(string(site), string(pointer)+"/")
}

// take reads a released sighting: a discriminator's entries, and the objects a
// reference's chain reached.
func (m *mappings) take(s sighting) {
	if s.discriminator != nil {
		m.collect(s.site, s.discriminator)
	}
	for _, r := range s.records {
		m.queue = append(m.queue, arrival{site: s.site, record: r})
	}
}

// arrive reads the object q reaches: the sightings a model position releases,
// or, once per position and kind, what an object built from raw YAML holds.
//
// A $ref naming the source by its file name reaches a copy of what it names,
// built anew each time. A copy of what the model holds there, as that kind, is
// read as the model's own, which the walk reads already: walked as raw YAML, a
// chain of components each naming the next so walked every copy down it.
func (m *mappings) arrive(ctx context.Context, found *reachedFindings, q arrival) {
	at := builtKey{pointer: pointerIn(q.record), kind: reflect.TypeOf(q.record.object)}
	if m.model[q.record.object] || m.modelAt[at] {
		m.release(at.pointer)
		return
	}
	if m.walked[at] {
		return
	}
	m.walked[at] = true
	m.walk(ctx, found, at.pointer, q.record)
}

// builtKey is an object's kind and the position it is at.
type builtKey struct {
	pointer jsontext.Pointer
	kind    reflect.Type
}

// walk reads the object r reached at base, built from raw YAML, which the
// model holds none of: the discriminators and schema $refs in it that the
// lowering reads, each at its location under base. What lies in a region of
// its own is read only if something reaches that region, which builds an
// object of its own there.
//
// A schema is walked as one: the document's walk copies each item's whole
// location, which costs a deep object its depth again at every item.
func (m *mappings) walk(ctx context.Context, found *reachedFindings, base jsontext.Pointer, r record) {
	lowering := newRegions(m.lowering.held)
	if js, ok := r.object.(*schemaRef); ok {
		for item := range oas3.Walk(ctx, js) {
			at := func() jsontext.Pointer { return jsontext.Pointer(item.Location.ToJSONPointer()) }
			s, inner := schemaStep(item)
			// Match returns only what its matcher does, and this one never fails.
			_ = item.Match(oas3.SchemaMatcher{Any: func(model any) error {
				m.read(ctx, found, base, at, lowering.enter(s, inner, model, at), model)
				return nil
			}})
		}
		return
	}
	for item := range r.walk(ctx) {
		at := func() jsontext.Pointer { return jsontext.Pointer(item.Location.ToJSONPointer()) }
		s, inner := documentStep(item.Location)
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			m.read(ctx, found, base, at, lowering.enter(s, inner, model, at), model)
			return nil
		}})
	}
}

// read reads model, which a built object's walk yields at the location at
// returns, in region: a discriminator's entries, or a schema $ref, resolved,
// where the lowering reads them. One step is charged for each, and once the
// steps are spent nothing more is read, so a walk past the bound only finishes
// its object.
func (m *mappings) read(ctx context.Context, found *reachedFindings, base jsontext.Pointer,
	at func() jsontext.Pointer, region jsontext.Pointer, model any,
) {
	if !m.spend(1) || region != "" {
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
// schema its first hop reaches there. Each spelling of one position resolves
// one $ref, so each reaches the object the first did, and a $ref to it would.
//
// A chain that fails further on is recorded all the same, as a $ref to the
// position lowers what its first hop reached; one that reaches nothing is
// recorded nowhere. The lowering reports every target it cannot resolve, so
// reporting the failure here too would report it twice. One not provably ending
// is left out (see provablyEnds).
func (m *mappings) resolveTarget(ctx context.Context, pointer jsontext.Pointer) {
	ref := oas3.NewJSONSchemaFromReference(references.Reference("#" + fragmentOf(pointer)))
	if !m.self.provablyEnds(m.view, ref.GetRef()) {
		return
	}
	vErrs, err := ref.Resolve(ctx, m.opts)
	if m.reader != nil {
		vErrs, err = m.reader.settle(ctx, ref, m.opts, vErrs, err)
	}
	m.resolutions++
	c := resolutionChain(ref)
	t := m.byPointer[pointer]
	t.resolved, t.trail, t.found = true, c.trail(), vErrs
	if len(c.records) > 0 && c.records[0].reached {
		if declared, ok := c.records[0].object.(*schemaRef); ok {
			m.out[pointer] = declared
		}
	}
	if err == nil {
		m.enqueue(t.least, c)
	}
}

// enqueue queues each object c's hops reached in the source, for the work at
// site.
func (m *mappings) enqueue(site jsontext.Pointer, c chain) {
	for _, r := range inSourceRecords(m.doc, c) {
		m.queue = append(m.queue, arrival{site: site, record: r})
	}
}

// resolveNested resolves the schema $ref js inside a built object, at site, as
// the walk resolves the model's: unless it names another document, which the
// lowering never lowers, or a "#/$defs/..." pointer, which GitHub #557's rule
// reads in the model alone (GitHub #570). A failure is left to the lowering,
// which reports the reference unresolved, and so is a chain that does not
// provably end (see provablyEnds).
func (m *mappings) resolveNested(ctx context.Context, found *reachedFindings, site jsontext.Pointer, js *schemaRef) {
	if _, ok := sourcePointer(m.self.path, string(js.GetRef())); !ok || !m.self.provablyEnds(m.view, js.GetRef()) {
		return
	}
	vErrs, err := js.Resolve(ctx, m.opts)
	if m.reader != nil {
		vErrs, err = m.reader.settle(ctx, js, m.opts, vErrs, err)
	}
	c := resolutionChain(js)
	found.note(site, c.trail(), vErrs)
	if err == nil {
		m.enqueue(site, c)
	}
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
	return sourcePointer(m.self.path, target)
}

// sourcePointer returns the position ref names in the source at self, as the
// lowering reads it, and whether it names one other than a "#/$defs/..."
// pointer.
func sourcePointer(self, ref string) (jsontext.Pointer, bool) {
	pointer, ok := refscope.Scope{SelfPath: self}.InternalPointer(ref)
	return pointer, ok && !defs.IsPointer(pointer)
}

// loweredKeywords are the schema keywords whose schemas the lowering lowers
// where they are written: the structure it builds (ir-design §4.3). It keeps
// the others verbatim, such as §4.7's validation-only keywords, or lowers them
// only where a reference names them, as $defs. A keyword this list does not
// name is taken to be one of those, never to be lowered.
var loweredKeywords = map[string]bool{
	"properties": true, "patternProperties": true, "additionalProperties": true, "items": true,
	"prefixItems": true, "allOf": true, "oneOf": true, "anyOf": true, "contentSchema": true,
	"discriminator": true,
}

// regions tracks, through one walk, the region each model it yields is
// lowered from: "" when the lowering takes every step there as written, and
// otherwise the position it stops taking them, last. What is in such a region
// is lowered only once something the lowering reads reaches that position, or
// one between it and the model. The walk yields a parent before its children,
// so each region is its parent's unless the step between starts one.
type regions struct {
	// held holds the references the walk sees taken out of the resolver's
	// reach (see withDefsHeld), which are references all the same.
	held map[*schemaRef]bool
	// of holds each model's region; branch, each schema an allOf step reached.
	of     map[any]jsontext.Pointer
	branch map[any]bool
}

// newRegions returns the tracker of a walk, held naming its held references.
func newRegions(held map[*schemaRef]bool) *regions {
	return &regions{held: held, of: map[any]jsontext.Pointer{}, branch: map[any]bool{}}
}

// enter returns the region of model, which the walk yields at the position
// site returns, after the step s when inner, and holds it for model's
// children. A model that is not inner is the walk's root.
func (r *regions) enter(s step, inner bool, model any, site func() jsontext.Pointer) jsontext.Pointer {
	var region jsontext.Pointer
	if inner {
		region = r.of[s.parent]
		switch r.start(s) {
		case startsHere:
			region = site()
		case startsAtHolder:
			region = trimTokens(site(), s.schemaTokens())
		default:
			// The step lowers as written, so model shares its parent's region.
		}
		if s.field == "allOf" {
			r.branch[model] = true
		}
	}
	r.of[model] = region
	return region
}

// start is where a region the step into a model starts, if one does.
type start int

const (
	startsNowhere  start = iota // the lowering takes the step as written
	startsHere                  // at the model the step leads to
	startsAtHolder              // at what holds the step, which a reference hoists whole
)

// start returns where the step from h into a model starts a region. Only a
// reference lowers a component other than a schema, a schema keyword
// loweredKeywords does not name, or one an election passes over (see
// passedOver, unelected). What is written beside a $ref is kept beside the
// alias over its target (GitHub #283, #406), and an inline allOf branch gives
// the model its properties alone (ir-design §4.3).
func (r *regions) start(s step) start {
	switch {
	case s.components:
		return startsIf(s.field != "schemas")
	case s.spelled:
		return startsIf(s.unelected())
	case s.schema == nil:
		return startsNowhere
	case s.schema.IsReference() || r.held[s.schema] || !loweredKeywords[s.field] ||
		passedOver(s.schema.GetSchema(), s.field):
		return startsHere
	case r.branch[s.schema] && s.field != "properties":
		return startsAtHolder
	default:
		return startsNowhere
	}
}

// startsIf returns startsHere when cut, and startsNowhere otherwise.
func startsIf(cut bool) start {
	if cut {
		return startsHere
	}
	return startsNowhere
}

// schemaTokens returns how many pointer tokens s, a step a schema takes,
// adds: its keyword's, and its key's or index's.
func (s step) schemaTokens() int {
	if s.key != nil || s.indexed {
		return 2
	}
	return 1
}

// trimTokens returns pointer less its last n tokens.
func trimTokens(pointer jsontext.Pointer, n int) jsontext.Pointer {
	for range n {
		pointer = pointer[:strings.LastIndexByte(string(pointer), '/')]
	}
	return pointer
}

// step is the last step of a walk's location: what holds it, the field it
// takes there, and the key it takes, or whether it takes an index.
type step struct {
	holder
	field   string
	key     *string
	indexed bool
}

// holder is what holds a step of a walk's location, as regions reads it: the
// model, and whether it is the components, a schema, or a parameter or
// header, which states its type by a content entry or else by its schema.
type holder struct {
	parent              any
	components, spelled bool
	schema              *schemaRef
	// content is the media type of the content entry a parameter or header
	// states its type by, "" when it states it by schema.
	content string
}

// documentStep returns the last step of a document walk's location, and
// false at the walk's root, which has none.
func documentStep(loc soa.Locations) (step, bool) {
	if len(loc) == 0 {
		return step{}, false
	}
	last := loc[len(loc)-1]
	var h holder
	// Match returns only what its matcher does, and this one never fails.
	_ = last.ParentMatchFunc(soa.Matcher{
		Any:        func(model any) error { h.parent = model; return nil },
		Components: func(*soa.Components) error { h.components = true; return nil },
		Schema:     func(js *schemaRef) error { h.schema = js; return nil },
		ReferencedParameter: func(p *soa.ReferencedParameter) error {
			h.spelled, h.content = true, firstContent(p.GetObject().GetContent().All())
			return nil
		},
		ReferencedHeader: func(r *soa.ReferencedHeader) error {
			h.spelled, h.content = true, firstContent(r.GetObject().GetContent().All())
			return nil
		},
	})
	return step{holder: h, field: last.ParentField, key: last.ParentKey, indexed: last.ParentIndex != nil}, true
}

// schemaStep is documentStep for a schema walk's item, each step of which a
// schema, or what one holds, takes.
func schemaStep(item oas3.SchemaWalkItem) (step, bool) {
	if len(item.Location) == 0 {
		return step{}, false
	}
	last := item.Location[len(item.Location)-1]
	var h holder
	// Match returns only what its matcher does, and this one never fails.
	_ = last.ParentMatchFunc(oas3.SchemaMatcher{
		Any:    func(model any) error { h.parent = model; return nil },
		Schema: func(js *schemaRef) error { h.schema = js; return nil },
	})
	return step{holder: h, field: last.ParentField, key: last.ParentKey, indexed: last.ParentIndex != nil}, true
}

// firstContent returns the media type of the first entry in content that
// names one, which the lowering takes over a schema beside it, and over any
// entry after it, or "" when none does.
func firstContent(content iter.Seq2[string, *soa.MediaType]) string {
	for mediaType, media := range content {
		if media != nil {
			return mediaType
		}
	}
	return ""
}

// unelected reports whether s, from a parameter or header, leads to a
// spelling of its type the lowering passes over: its schema beside a content
// entry, or any content entry but the one it takes.
func (s step) unelected() bool {
	switch s.field {
	case "schema":
		return s.content != ""
	case "content":
		return s.key == nil || *s.key != s.content
	default:
		return false
	}
}

// passedOver reports whether the lowering keeps keyword, written in s,
// verbatim beside the keyword it elects in its place (ir-design §4.3, §4.8):
// items beside prefixItems, anyOf beside oneOf, and allOf beside const or
// enum.
func passedOver(s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "items":
		return len(s.GetPrefixItems()) > 0
	case "anyOf":
		return len(s.GetOneOf()) > 0
	case "allOf":
		return s.GetConst() != nil || s.GetEnum() != nil
	default:
		return false
	}
}
