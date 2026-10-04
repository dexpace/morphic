package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
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

// mappings resolves the discriminator mapping targets of the document a
// resolveWith walk resolves. The walk shows it every model (see) and every
// reference's chain (reached), since what the model holds as raw YAML is built
// only when something resolves it. resolve then resolves each target the
// collected discriminators name, collecting those in what each brings in.
type mappings struct {
	self   sourceDocument
	doc    *soa.OpenAPI
	opts   references.ResolveOptions
	reader *external
	// model holds each object the walk saw, and walked each other object whose
	// discriminators were collected.
	model, walked map[any]bool
	// built holds what references reached in the source, which is raw YAML
	// built unless the model holds it, as the walk's end tells.
	built   []builtObject
	entries []mappingEntry
	tried   map[string]bool
	out     map[jsontext.Pointer]*schemaRef
	// from holds the target each pointer's schema was resolved from: the least
	// of the spellings naming it, so declaration order never decides it.
	from map[jsontext.Pointer]string
}

// builtObject is an object a resolution reached in the source: the record
// holding it, the pointer it is at, and the site that reached it.
type builtObject struct {
	site    jsontext.Pointer
	pointer jsontext.Pointer
	record  record
}

// mappingEntry is a mapping target as written, and the pointer of the entry or
// defaultMapping writing it.
type mappingEntry struct {
	site   jsontext.Pointer
	target string
}

// newMappings returns the resolver of the mapping targets of doc, the model of
// self, which resolves each with opts, through reader when external references
// are allowed.
func newMappings(self sourceDocument, doc *soa.OpenAPI, opts references.ResolveOptions, reader *external) *mappings {
	return &mappings{self: self, doc: doc, opts: opts, reader: reader,
		model: map[any]bool{}, walked: map[any]bool{}, tried: map[string]bool{},
		out: map[jsontext.Pointer]*schemaRef{}, from: map[jsontext.Pointer]string{}}
}

// targets returns what resolve resolved.
func (m *mappings) targets() MappingTargets {
	return MappingTargets{byPointer: m.out}
}

// see notes a model the walk reached at site.
func (m *mappings) see(site jsontext.Pointer, model any) {
	switch v := model.(type) {
	case *oas3.Discriminator:
		m.collect(site, v)
	case resolvable:
		m.model[v] = true
	}
}

// collect notes the targets of the discriminator d at site.
func (m *mappings) collect(site jsontext.Pointer, d *oas3.Discriminator) {
	for tag, target := range d.GetMapping().All() {
		m.entries = append(m.entries, mappingEntry{site: site.AppendToken("mapping").AppendToken(tag), target: target})
	}
	if target := d.GetDefaultMapping(); target != "" {
		m.entries = append(m.entries, mappingEntry{site: site.AppendToken("defaultMapping"), target: target})
	}
}

// reached notes each object c's hops reached in the source, for the reference
// written at site.
func (m *mappings) reached(site jsontext.Pointer, c chain) {
	for _, r := range c.records {
		if inSource(r) {
			m.built = append(m.built, builtObject{site: site, pointer: pointerIn(r), record: r})
		}
	}
}

// inSource reports whether r's hop reached an object in the source, which is
// what it resolved against once mended.
func inSource(r record) bool {
	_, ok := (*r.document).(*soa.OpenAPI)
	return r.reached && ok
}

// pointerIn returns the pointer r's hop reached, in its document.
func pointerIn(r record) jsontext.Pointer {
	return jsontext.Pointer(r.target.GetJSONPointer())
}

// resolve resolves each target the discriminators collected name, then those
// in what each target brings in, and notes what each resolution draws in
// found. Each target is resolved once and each object walked once, so the
// work is bounded by the source's text.
//
// A panic stops it, reported as an error naming site, the entry or reference
// whose work was running, as a panic in resolveWith's walk is.
func (m *mappings) resolve(ctx context.Context, found *reachedFindings) (site jsontext.Pointer, err error) {
	defer recovered(&err, "discriminator mapping resolver")
	for _, b := range m.built {
		site = b.site
		if !m.model[b.record.object] {
			m.walk(ctx, b.pointer, b.record)
		}
	}
	for i := 0; i < len(m.entries); i++ {
		site = m.entries[i].site
		m.resolveEntry(ctx, m.entries[i], found)
	}
	return "", nil
}

// walk collects the discriminators in the object r reached at pointer, built
// from raw YAML, unless it was walked already.
func (m *mappings) walk(ctx context.Context, pointer jsontext.Pointer, r record) {
	if m.walked[r.object] {
		return
	}
	m.walked[r.object] = true
	m.collectFrom(pointer, r.walk(ctx))
}

// collectFrom collects the discriminators items reach, each at its location
// under base. Match returns what its callback does, which here is always nil,
// so only a walk a test builds stops early.
func (m *mappings) collectFrom(base jsontext.Pointer, items iter.Seq[soa.WalkItem]) {
	for item := range items {
		if err := item.Match(soa.Matcher{Discriminator: func(d *oas3.Discriminator) error {
			m.collect(base+jsontext.Pointer(item.Location.ToJSONPointer()), d)
			return nil
		}}); err != nil {
			return
		}
	}
}

// resolveEntry resolves e's target as a $ref to it in the source is resolved,
// and records the schema it names there, which its first hop reaches. A target
// resolved already, or not in the source (see pointerOf), is skipped.
//
// A target that does not resolve is recorded nowhere: the lowering reports
// every target it cannot resolve, and reporting it here too would report it
// twice. What its resolution drew is noted all the same.
func (m *mappings) resolveEntry(ctx context.Context, e mappingEntry, found *reachedFindings) {
	pointer, ok := m.pointerOf(e.target)
	if !ok || m.tried[e.target] {
		return
	}
	m.tried[e.target] = true
	ref := oas3.NewJSONSchemaFromReference(references.Reference(e.target))
	vErrs, err := ref.Resolve(ctx, m.opts)
	if m.reader != nil {
		vErrs, err = m.reader.settle(ctx, ref, m.opts, vErrs, err)
	}
	c := resolutionChain(ref)
	found.note(e.site, c.trail(), vErrs)
	if err != nil {
		return
	}
	m.record(pointer, e.target, c.records[0])
	for _, r := range c.records {
		if inSource(r) && !m.model[r.object] {
			m.walk(ctx, pointerIn(r), r)
		}
	}
}

// record keeps r's object as the schema at pointer, which target names, unless
// a lesser spelling of it named one already.
func (m *mappings) record(pointer jsontext.Pointer, target string, r record) {
	declared, ok := r.object.(*schemaRef)
	if from, held := m.from[pointer]; !ok || held && from < target {
		return
	}
	m.out[pointer], m.from[pointer] = declared, target
}

// pointerOf returns the pointer target names in the source, as the lowering
// reads it, and whether to resolve it here. Only a target in the source is: an
// internal $ref, or one whose document part the resolver reads as the source,
// which it reads only with external references allowed. A declared
// component's name is the component, as the lowering reads it. The lowering
// asks only for a pointer, so what another fragment names is never read.
func (m *mappings) pointerOf(target string) (jsontext.Pointer, bool) {
	if _, declared := m.doc.GetComponents().GetSchemas().Get(target); declared {
		return "", false
	}
	ref := references.Reference(target)
	pointer := jsontext.Pointer(ref.GetJSONPointer())
	uri := ref.GetURI()
	if uri == "" {
		return pointer, true
	}
	abs, err := references.ResolveAbsoluteReference(references.Reference(uri), m.self.path)
	return pointer, err == nil && (slices.Contains(m.self.keys(), abs.AbsoluteReference) ||
		m.self.names(abs.AbsoluteReference))
}
