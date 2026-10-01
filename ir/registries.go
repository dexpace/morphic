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
// A value's Go type is what makes it a reference: a [ChannelID]-typed field
// references Document.Channels wherever it sits, a node's own ID included, so
// no field has to be listed and none can be forgotten.
//
// Type-driven coverage is not total. An integer index into a slice has nothing
// to key on, and [PropID] names a position inside a model; both are resolved
// by hand where checked. An [Operation] and a [Service] live in a tree and a
// slice, so [Registries.WithDeclarations] covers them.
type Registries map[reflect.Type]Registry

// WithDeclarations returns r extended with a registry for every ID class
// Document holds no map for, filled from decls, so a reference to such a class
// resolves (#50).
//
// The classes come from [idClasses], not from decls, so a class no node
// declares is still reported for.
//
// A class r covers with a map keeps that map, since a derived one could only
// disagree. A declaration-derived registry r carries is replaced, so this call
// never mutates another's result.
//
// [PropID] is left out: its checks are tighter where the model is known, and a
// document-wide answer would report one defect twice.
func (r Registries) WithDeclarations(decls []IDDeclaration) Registries {
	out := make(Registries, len(r))
	maps.Copy(out, r)
	for class := range idClasses() {
		if reg, mapped := out[class]; class == propIDType || (mapped && reg.ids == nil) {
			continue
		}
		out[class] = Registry{Label: RefNoun(class) + " declarations", ids: map[string]bool{}}
	}
	for _, d := range decls {
		// A class Document keys a map by carries no ids set, and PropID has no
		// registry here at all; both leave the declaration to its own checker.
		if ids := out[d.Class].ids; ids != nil {
			ids[d.ID] = true
		}
	}
	return out
}

// idClasses returns every class of ID a node of a [Document] can declare as its
// own, derived from the IR's type graph rather than from any one document, so a
// class nothing in this document declares still has to resolve references.
//
// The walk is over reflect.Type, so the finite set of types the package declares
// bounds it: each is expanded once. [TypeDef] is an interface and a type walk
// cannot see what implements it, so the concrete kinds are seeded from
// newTypeDefByKind, already the single source of truth for kind dispatch.
func idClasses() map[reflect.Type]bool {
	classes := map[reflect.Type]bool{}
	seen := map[reflect.Type]bool{}
	queue := idClassRoots()
	for len(queue) > 0 {
		t := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[t] {
			continue
		}
		seen[t] = true
		queue = appendIDClasses(queue, classes, t)
	}
	return classes
}

// idClassRoots are the types the node graph is reachable from: Document, plus one
// of each concrete TypeDef, which the type registry holds behind an interface.
func idClassRoots() []reflect.Type {
	roots := make([]reflect.Type, 0, 1+len(newTypeDefByKind))
	roots = append(roots, reflect.TypeFor[Document]())
	for _, newTypeDef := range newTypeDefByKind {
		roots = append(roots, reflect.TypeOf(newTypeDef()))
	}
	return roots
}

// appendIDClasses records the ID class t declares for itself, if any, and appends
// the types reachable from t. It reads the same rule declaredID reads off a
// value: a field named ID, declared by t rather than promoted into it, whose type
// is a named string. A promoted field belongs to the struct that declares it, and
// that struct is reached in its own right.
func appendIDClasses(dst []reflect.Type, classes map[reflect.Type]bool, t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return append(dst, t.Elem())
	case reflect.Map:
		return append(dst, t.Key(), t.Elem())
	case reflect.Struct:
		for f := range t.Fields() {
			if f.Name == idFieldName && namedString(f.Type) {
				classes[f.Type] = true
			}
			dst = append(dst, f.Type)
		}
		return dst
	default:
		return dst // no other kind holds a field or an element.
	}
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

// DeclaredIDs returns every identity the nodes of doc declare, in walk order,
// and reports whether the bounded walk was cut short.
//
// Reading declarations off the value graph covers a new ID-bearing node the
// moment it exists. It answers what [DocumentRegistries] cannot: which IDs a
// class declares when Document holds no map for it, and whether one ID is
// declared twice, which a map key cannot express.
//
// An empty ID is skipped, since nothing can reference one and several nodes
// carrying one are not duplicates. Reporting it is irverify.checkRegistryKeys's
// job for a map-keyed class and irverify.checkDeclaredIDs's for the rest.
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
// A promoted field is not its own declaration: len(f.Index) is 1 only for a
// field the struct declares directly. Every type node embeds [TypeCommon] and
// so promotes its ID, and counting that too would declare each type ID twice. A
// field named ID that is not a named string type settles no class of reference.
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
// Document.Unmodeled, keyed by plain string, names a source construct rather
// than an identity and is no registry.
//
// A nil doc declares nothing: every reference then resolves against no registry
// and is reported, rather than the call panicking.
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
