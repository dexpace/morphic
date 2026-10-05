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
type MappingTargets struct {
	byPointer map[jsontext.Pointer]*schemaRef
}

// At returns the schema a mapping target resolved to at pointer, or nil.
func (t MappingTargets) At(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return t.byPointer[pointer]
}

// maxMappingWork bounds the steps resolving the mapping targets takes: each
// entry it reads, object it reaches, walk item it reads and position it
// resolves, and for each position the keys of the object holding it, which the
// library may scan to find it. Each object is walked once and each position
// resolved once, so the steps follow what the library builds for the targets,
// which is what a $ref to each would build.
const maxMappingWork = 1 << 28

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
	// model holds each object the walk saw, and modelAt each site it saw one of
	// a kind at; walked holds each position and kind of other object read.
	model   map[any]bool
	modelAt map[builtKey]bool
	walked  map[builtKey]bool
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
		model: map[any]bool{}, modelAt: map[builtKey]bool{}, walked: map[builtKey]bool{},
		named: map[jsontext.Pointer]bool{}, byPointer: map[jsontext.Pointer]*target{},
		out: map[jsontext.Pointer]*schemaRef{}, limit: maxMappingWork}
}

// targets returns what resolve resolved.
func (m *mappings) targets() MappingTargets {
	return MappingTargets{byPointer: m.out}
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
	return !refscope.Scope{Doc: doc, Ends: refscope.ReferenceEnd}.At(pointerIn(r)).Foreign
}

// pointerIn returns the pointer r's hop reached, in its document. The absolute
// reference holds it decoded already, as the resolver read it, so it is taken
// as written: GetJSONPointer would decode it again, reading a key's '+' as a
// space and its '%41' as 'A'.
func pointerIn(r record) jsontext.Pointer {
	_, pointer, _ := strings.Cut(string(r.target), "#")
	return jsontext.Pointer(pointer)
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
			found.note(t.least, t.trail, t.found)
		}
	}
}

// arrive reads the object q reaches, once per position and kind, when it is
// built from raw YAML: what the model holds the walk has read already.
//
// A $ref naming the source by its file name reaches a copy of what it names,
// built anew each time. A copy of what the model holds there, as that kind, is
// read as the model's own: walked as raw YAML, a chain of components each
// naming the next so walked every copy down it.
func (m *mappings) arrive(ctx context.Context, found *reachedFindings, q arrival) {
	at := builtKey{pointer: pointerIn(q.record), kind: reflect.TypeOf(q.record.object)}
	if m.model[q.record.object] || m.modelAt[at] || m.walked[at] {
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
// resolve. One not provably ending is left out (see provablyEnds).
func (m *mappings) resolveTarget(ctx context.Context, pointer jsontext.Pointer) {
	ref := oas3.NewJSONSchemaFromReference(references.Reference("#" + fragmentOf(pointer)))
	if !m.self.provablyEnds(m.doc, m.view, ref.GetRef()) {
		return
	}
	if !m.spend(m.keysHolding(pointer)) {
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
	} else {
		vErrs = nil
	}
	t.resolved, t.trail, t.found = true, c.trail(), vErrs
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
	pointer, ok := sourcePointer(m.self.path, string(js.GetRef()))
	if !ok || !m.self.provablyEnds(m.doc, m.view, js.GetRef()) || !m.spend(m.keysHolding(pointer)) {
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

// keysHolding returns the steps resolving the position pointer costs: one, and
// one for each key of the object that holds it in the source's tree, which the
// library scans to find a position under an extension. The tree is not the
// model's, so the count is an upper bound for a position the model holds in a
// map.
func (m *mappings) keysHolding(pointer jsontext.Pointer) int {
	root := m.self.root
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	holder := pointer[:max(strings.LastIndexByte(string(pointer), '/'), 0)]
	path, complete := m.view.DocumentPath(root, holder)
	if !complete || len(path) == 0 {
		return 1
	}
	return 1 + len(path[len(path)-1].Content)/2
}

// pointerOf returns the position target names in the source, and whether it
// names one: as the lowering reads it (refscope.Scope.InternalPointer), so the
// targets resolved here are exactly those it asks DeclaredAt about. A declared
// component's name is the component, and a "#/$defs/..." pointer is the
// definition the lowering reads relative to the discriminator (GitHub #557),
// which no $ref to the pointer resolves. One ending in '/' names an empty key,
// which the resolver cannot read: it trims the slash and reads the parent.
func (m *mappings) pointerOf(target string) (jsontext.Pointer, bool) {
	if _, declared := m.doc.GetComponents().GetSchemas().Get(target); declared {
		return "", false
	}
	pointer, ok := sourcePointer(m.self.path, target)
	return pointer, ok && !strings.HasSuffix(string(pointer), "/")
}

// sourcePointer returns the position ref names in the source at self, as the
// lowering reads it, and whether it names one other than a "#/$defs/..."
// pointer.
func sourcePointer(self, ref string) (jsontext.Pointer, bool) {
	pointer, ok := refscope.Scope{SelfPath: self}.InternalPointer(ref)
	return pointer, ok && !defs.IsPointer(pointer)
}
