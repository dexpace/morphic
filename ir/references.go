package ir

import (
	"reflect"
	"slices"
	"strings"
)

var _ Visitor = (*IDReferences)(nil)

// propIDType is the reflect.Type of [PropID], the one class of ID no registry
// resolves; see [IDReferences] for why.
var propIDType = reflect.TypeFor[PropID]()

// IDReference is one typed-ID reference a walk reached: its class, the ID it
// names, and where it sits.
type IDReference struct {
	// Class is the Go type of the ID, which names the registry it resolves in.
	Class reflect.Type
	// ID is the identity referenced.
	ID string
	// Path locates the reference, as [WalkValues] spells it.
	Path string
}

// IDReferences gathers, as a [Visitor], the typed-ID references a walk reaches
// and what they resolve against, so both checkers report a dangling reference
// alike. A value's Go type is what makes it a reference: a [ChannelID]
// references Document.Channels wherever it sits, a node's own ID included, so
// no field is listed and none can be forgotten.
//
// Integer indices have nothing to key on, and a [PropID] names a position inside
// a model, so neither is kept; each is resolved by hand where checked. Only
// [NewIDReferences] builds a usable one, for one walk on one goroutine.
type IDReferences struct {
	// labels names the registry each class of reference resolves in, and so
	// holds every class a reference can be of.
	labels map[reflect.Type]string
	// keyed holds, for each class a Document map is keyed by, the keys that map
	// holds.
	keyed map[reflect.Type]map[string]bool
	// declared holds the IDs the walk's nodes declare for every other class,
	// read through ids.
	declared map[reflect.Type]map[string]bool
	ids      IDReader
	// sites holds the references that may not resolve, in walk order, each
	// with its path unspelled at the same position of paths.
	sites []IDReference
	paths PathLog
}

// NewIDReferences returns the IDReferences of a walk of doc, holding the IDs
// each of doc's map-keyed registries declares. Every other class resolves
// against what the walk's nodes declare, PropID aside; such a registry is
// labelled "<noun> declarations". A nil doc keys nothing, so a reference into
// one of its maps is unresolved rather than the call panicking.
func NewIDReferences(doc *Document) *IDReferences {
	r := &IDReferences{
		labels:   map[reflect.Type]string{},
		keyed:    map[reflect.Type]map[string]bool{},
		declared: map[reflect.Type]map[string]bool{},
	}
	for _, reg := range keyedRegistries() {
		r.labels[reg.class] = reg.label
		r.keyed[reg.class] = registryKeys(doc, reg.index)
	}
	for class := range idClasses() {
		if _, keyed := r.keyed[class]; keyed || class == propIDType {
			continue
		}
		r.labels[class] = RefNoun(class) + " declarations"
	}
	return r
}

// Visit records the identity v declares, if any, and keeps v if it is a
// reference that may not resolve, asking for v's children either way.
//
// A reference resolving as the walk reaches it, in a map-keyed registry or to
// an identity already declared, is not kept: declarations only accumulate. Any
// other is kept for [IDReferences.Unresolved], with its path unspelled, since a
// declared class's registry is complete only once the walk is over. A node's own ID is
// reached just after the node declares it. An empty ID is no reference: some
// positions spell "none" with one (Discriminator.Default).
func (r *IDReferences) Visit(v reflect.Value, at WalkPath) bool {
	switch v.Kind() {
	case reflect.Struct:
		r.declare(v)
	case reflect.String:
		r.refer(v, at)
	default:
		// no other kind is a reference or declares an identity
	}
	return true
}

// declare records the identity struct v declares, if any, unless a Document map
// keys its class: those resolve against the map, whatever a node declares.
func (r *IDReferences) declare(v reflect.Value) {
	class, id, declares := r.ids.DeclaredIDOf(v)
	if !declares || id == "" {
		return
	}
	if _, keyed := r.keyed[class]; keyed {
		return
	}
	ids := r.declared[class]
	if ids == nil {
		ids = map[string]bool{}
		r.declared[class] = ids
	}
	ids[id] = true
}

// refer keeps string v, reached at at, if it is a reference that may not
// resolve.
func (r *IDReferences) refer(v reflect.Value, at WalkPath) {
	id := v.String()
	if id == "" {
		return
	}
	t := v.Type()
	if _, isRef := r.labels[t]; !isRef {
		return
	}
	ids, keyed := r.keyed[t]
	if !keyed {
		ids = r.declared[t]
	}
	if ids[id] {
		return
	}
	r.sites = append(r.sites, IDReference{Class: t, ID: id})
	r.paths.Record(at)
}

// Unresolved returns, in walk order, the references the walk reached that
// resolve in no registry, each with its path. Only these have their paths
// spelled, so a kept reference that resolves once the walk is over costs none.
//
// A declared class's registry is complete only when the walk read every
// declaration. A walk cut short, as truncated says, would report a reference to
// a node past the cap as dangling, so nothing is claimed for such a class then;
// a map-keyed registry is read off the document and still answers.
func (r *IDReferences) Unresolved(truncated bool) []IDReference {
	if !slices.ContainsFunc(r.sites, func(s IDReference) bool { return r.dangles(s, truncated) }) {
		return nil
	}
	var out []IDReference
	for i, at := range r.paths.All() {
		if s := r.sites[i]; r.dangles(s, truncated) {
			s.Path = at.String()
			out = append(out, s)
		}
	}
	return out
}

// dangles reports whether the kept reference s resolves in no registry, as
// Unresolved says, truncated saying whether the walk was cut short.
func (r *IDReferences) dangles(s IDReference, truncated bool) bool {
	_, keyed := r.keyed[s.Class]
	return keyed || (!truncated && !r.declared[s.Class][s.ID])
}

// Declares reports whether a node the walk reached declares id as its identity
// of class, which is what a reference of a class no Document map keys resolves
// against. PropID is answered too, so a check holding one to a declared property
// need not read the declarations again. A map-keyed class is not: its references
// resolve against the map, so Declares reports false for it.
func (r *IDReferences) Declares(class reflect.Type, id string) bool {
	return r.declared[class][id]
}

// Label returns the name a report spells the registry of class with: the
// lowercased Document field that keys it — "types", "channels" — or "<noun>
// declarations" for a class the walk's declarations resolve. It is empty for a
// class no registry resolves.
func (r *IDReferences) Label(class reflect.Type) string {
	return r.labels[class]
}

// KeyedClasses returns the classes of ID a Document field keys a registry map
// by, such as [TypeID] for Types and [ChannelID] for Channels. It reads
// Document's type rather than a document, so it answers alike for every one,
// and covers a registry added to Document the moment it exists.
func KeyedClasses() map[reflect.Type]bool {
	regs := keyedRegistries()
	classes := make(map[reflect.Type]bool, len(regs))
	for _, reg := range regs {
		classes[reg.class] = true
	}
	return classes
}

// keyedRegistry is a Document field that keys a registry map by a class of ID:
// the field's index, that class, and the field's name lowercased, which a report
// spells the registry with.
type keyedRegistry struct {
	index int
	class reflect.Type
	label string
}

// keyedRegistries derives the registries Document keys by ID from its own shape:
// a field that is a map keyed by a named string type is one, and the key type
// names the class it resolves. Document.Unmodeled, keyed by plain string, names
// a source construct rather than an identity and is no registry.
func keyedRegistries() []keyedRegistry {
	var regs []keyedRegistry
	doc := reflect.TypeFor[Document]()
	for i := range doc.NumField() {
		f := doc.Field(i)
		if f.Type.Kind() != reflect.Map || !namedString(f.Type.Key()) {
			continue
		}
		regs = append(regs, keyedRegistry{index: i, class: f.Type.Key(), label: strings.ToLower(f.Name)})
	}
	return regs
}

// registryKeys returns the IDs the registry at Document field index keys in
// doc, as a set: one pass over the map, after which a lookup allocates
// nothing, where reflect.Value.MapIndex allocates the key it is given and the
// entry it copies out. A nil doc keys nothing.
func registryKeys(doc *Document, index int) map[string]bool {
	if doc == nil {
		return nil
	}
	m := reflect.ValueOf(doc).Elem().Field(index)
	ids := make(map[string]bool, m.Len())
	key := reflect.New(m.Type().Key()).Elem()
	for entries := m.MapRange(); entries.Next(); {
		key.SetIterKey(entries)
		ids[key.String()] = true
	}
	return ids
}

// RefNoun names the reference class an ID type identifies: its type name minus
// the ID suffix, lowercased ("ChannelID" → "channel"). Both of Morphic's
// checkers spell a dangling-reference code with it, so one defect reads under
// one code whichever of them reports it.
func RefNoun(idType reflect.Type) string {
	return strings.ToLower(strings.TrimSuffix(idType.Name(), "ID"))
}
