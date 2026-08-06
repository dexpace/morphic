package ir

import (
	"maps"
	"reflect"
	"strings"
)

// idFieldName is the field through which a node declares its own identity. It is
// spelled once here because [DeclaredIDs] reaches it by name, which the Go
// compiler cannot check.
const idFieldName = "ID"

// propIDType is the reflect.Type of [PropID], the one ID class
// [Registries.WithDeclarations] leaves out; see there for why.
var propIDType = reflect.TypeFor[PropID]()

// Registry is one registry of IDs a [Document] declares: the entries themselves,
// plus the name a report about them is spelled with.
//
// The zero value declares nothing, which is what a lookup for an ID class the
// document registers nothing for yields. [Registry.Has] reports false for it
// rather than indexing an invalid value, so a checker holding a site built
// against another document reports it as unresolved instead of crashing.
type Registry struct {
	// Label is the name a report spells the registry with: the lowercased
	// Document field that declares it — "types", "channels" — or "<noun>
	// declarations" for a registry derived from the nodes themselves.
	Label string

	// entries is the ID-keyed map a Document field declares; ids is the set a
	// declaration-derived registry carries instead. At most one is set. A
	// map-keyed registry stays a view of the document's own map rather than a copy
	// of its keys, and a class with no such map has nothing to view.
	entries reflect.Value
	ids     map[string]bool
}

// Has reports whether the registry declares id.
func (r Registry) Has(id string) bool {
	if r.ids != nil {
		return r.ids[id]
	}
	if !r.entries.IsValid() {
		return false
	}
	return r.entries.MapIndex(reflect.ValueOf(id).Convert(r.entries.Type().Key())).IsValid()
}

// Registries maps each ID type to the [Registry] that declares those IDs.
//
// A value's Go type is what makes it a reference: a [ChannelID]-typed field is a
// reference into Document.Channels wherever it sits — a node's own ID included,
// which resolves against its own entry — so no field has to be listed here and
// none can be forgotten.
//
// Type-driven coverage is not total, and what it misses is a category rather
// than a stray field. A reference carried as an integer index into a slice is an
// int like any other, and reflection has nothing to key on; [PropID] names a
// position inside a model rather than an entry in a document-level map. Both
// classes are resolved by hand where they are checked.
//
// Not every ID class Document declares references to has a map on Document at
// all: an [Operation] is declared in the Service→OperationGroup tree and a
// [Service] in a slice. [Registries.WithDeclarations] covers those.
type Registries map[reflect.Type]Registry

// WithDeclarations returns r extended with a registry per ID class in decls that
// r has no entry for, so a reference whose class Document holds no map for still
// resolves — against the nodes that declare it.
//
// An OpID and a ServiceID reference resolved against nothing before this, in
// both of Morphic's checkers, because both are driven by the map fields
// [DocumentRegistries] reads and neither class has one (GitHub #50).
//
// A class r already covers keeps its map. The map is what a consumer looks an ID
// up in, and irverify holds every entry to being keyed by its own node's ID, so a
// second answer derived here could only disagree with the first.
//
// [PropID] is left out. A property is a position inside its model rather than a
// document-level identity, and the checks that resolve one — against the
// properties a document declares, and against the parts of the model a content
// names — are tighter claims made where the root is known. Adding a document-wide
// answer here would report one defect twice, under two codes.
func (r Registries) WithDeclarations(decls []IDDeclaration) Registries {
	out := make(Registries, len(r))
	maps.Copy(out, r)
	for _, d := range decls {
		if _, mapped := r[d.Class]; mapped || d.Class == propIDType {
			continue
		}
		reg, derived := out[d.Class]
		if !derived {
			reg = Registry{Label: RefNoun(d.Class) + " declarations", ids: map[string]bool{}}
			out[d.Class] = reg
		}
		reg.ids[d.ID] = true // reg is a copy of the entry, but ids is the same map
	}
	return out
}

// IDDeclaration is one node's declaration of its own identity: the class of ID,
// the ID itself, and the path of the node that declares it.
type IDDeclaration struct {
	// Class is the Go type of the ID, which names the reference class the
	// declaration settles.
	Class reflect.Type
	// ID is the declared identity.
	ID string
	// Path locates the declaring node, as [WalkValues] spells it.
	Path string
}

// DeclaredIDs returns every identity the nodes of doc declare — the ID a node
// carries for itself — in walk order, and reports whether the bounded walk was
// cut short.
//
// A node's own ID is what a reference to it resolves against, and reading those
// declarations off the value graph rather than a list of carriers covers a new
// ID-bearing node the moment it exists. It answers the two questions
// [DocumentRegistries] cannot: which IDs a class declares when Document holds no
// map for it, and whether one ID is declared twice — which no map key can
// express, since a map has one entry per key however many nodes claim it.
//
// Only a struct declaring the field itself counts. Every type node embeds
// [TypeCommon] and so promotes its ID, and counting promoted fields would reach
// each of them twice: a document with nothing wrong with it would read as one
// where every type ID is declared twice.
//
// An empty ID declares no identity and is skipped. A node carrying one is its own
// defect, reported where the node's registry key is, and calling several of them
// duplicates of each other would name the wrong problem.
func DeclaredIDs(doc *Document) ([]IDDeclaration, bool) {
	var decls []IDDeclaration
	truncated := WalkValues(doc, DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct {
			return true
		}
		if id, class, declares := declaredID(v); declares {
			decls = append(decls, IDDeclaration{Class: class, ID: id, Path: path})
		}
		return true
	})
	return decls, truncated
}

// declaredID returns the identity v declares for itself: the value of a field
// named ID that v's own type declares and that is a named string type.
//
// The three ways that can fail read as one test because they are one question —
// whether this struct declares an identity of its own. A promoted field is not
// its own declaration: len(f.Index) is 1 only for a field the struct declares
// directly, and a field named ID that is not a named string type settles no
// class of reference.
func declaredID(v reflect.Value) (id string, class reflect.Type, declares bool) {
	f, isDeclared := v.Type().FieldByName(idFieldName)
	if !isDeclared || len(f.Index) != 1 || !namedString(f.Type) {
		return "", nil, false
	}
	value := v.Field(f.Index[0]).String()
	if value == "" {
		return "", nil, false // an empty ID names nothing to resolve against
	}
	return value, f.Type, true
}

// DocumentRegistries derives doc's registries from Document's own shape: a field
// that is a map keyed by a named string type is an ID-keyed registry, and its key
// type names the reference class it resolves. Deriving them covers a registry
// added to Document the moment it exists, where a hand-written list would drift.
// Document.Unmodeled is the counterexample: keyed by plain string, it keys on a
// source construct's name rather than an identity, and is no registry.
//
// A nil doc declares nothing, which is the answer a report-only caller wants:
// every reference then resolves against no registry and is reported, rather than
// the call panicking on the way to saying so.
func DocumentRegistries(doc *Document) Registries {
	out := Registries{}
	if doc == nil {
		return out
	}
	for shape, f := range reflect.ValueOf(doc).Elem().Fields() {
		key, isRegistry := registryKeyType(f)
		if !isRegistry {
			continue
		}
		out[key] = Registry{Label: strings.ToLower(shape.Name), entries: f}
	}
	return out
}

// registryKeyType returns the named string type f is keyed by, and whether f is
// an ID-keyed registry at all.
func registryKeyType(f reflect.Value) (reflect.Type, bool) {
	if f.Kind() != reflect.Map {
		return nil, false
	}
	key := f.Type().Key()
	if !namedString(key) {
		return nil, false
	}
	return key, true
}

// namedString reports whether t is a named string type, the shape every ID class
// takes. A plain string is not one: Document.Unmodeled keys on a source
// construct's name, which is a name rather than an identity.
func namedString(t reflect.Type) bool {
	return t.Kind() == reflect.String && t.PkgPath() != ""
}

// RefNoun names the reference class an ID type identifies: its type name minus
// the ID suffix, lowercased ("ChannelID" → "channel"). Both of Morphic's
// checkers spell a dangling-reference code with it, so one defect reads under
// one code whichever of them reports it.
func RefNoun(idType reflect.Type) string {
	return strings.ToLower(strings.TrimSuffix(idType.Name(), "ID"))
}
