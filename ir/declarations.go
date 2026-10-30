package ir

import (
	"iter"
	"reflect"
)

var _ Visitor = (*IDDeclarations)(nil)

// idFieldName is the field through which a node declares its own identity. It is
// spelled once here because [IDReader.DeclaredIDOf] reaches it by name, which
// the Go compiler cannot check.
const idFieldName = "ID"

// IDDeclaration is one node's declaration of its own identity: the class of ID
// and the ID itself. Where the node sits is [IDDeclarations.Paths]'s to spell.
type IDDeclaration struct {
	// Class is the Go type of the ID, which names the reference class the
	// declaration settles.
	Class reflect.Type
	// ID is the declared identity.
	ID string
}

// IDDeclarations collects, as a [Visitor], the identities the values its walk
// reaches declare, in walk order, for a check reporting where one is declared,
// such as twice, each with its path kept unspelled for the check to spell
// only those it reports. Reading them off the value graph covers a new
// ID-bearing node the moment it exists.
//
// An empty ID is skipped, since nothing can reference one and several nodes
// carrying one are not duplicates. Reporting it is irverify.checkRegistryKeys's
// job for a map-keyed class and irverify.checkDeclaredIDs's for the rest. The
// zero IDDeclarations is ready to walk with.
type IDDeclarations struct {
	ids   IDReader
	list  []IDDeclaration
	paths PathLog
}

// Visit records the identity v declares, if any, and asks for v's children.
func (d *IDDeclarations) Visit(v reflect.Value, at WalkPath) bool {
	if class, id, declares := d.ids.DeclaredIDOf(v); declares && id != "" {
		d.list = appendGrowing(d.list, IDDeclaration{Class: class, ID: id})
		d.paths.Record(at)
	}
	return true
}

// List returns the declarations collected so far, in walk order. The slice is
// d's own rather than a copy, so changing it changes what d holds.
func (d *IDDeclarations) List() []IDDeclaration {
	return d.list
}

// Paths yields the path of each declaration List holds, with its position
// there, in walk order, as a WalkPath valid until the next is yielded: a check
// keeps the String of those it reports and spells no other.
func (d *IDDeclarations) Paths() iter.Seq2[int, WalkPath] {
	return d.paths.All()
}

// IDReader reads the identity a value declares for itself, reading where a
// struct type declares one only the first time it meets the type: a visitor
// shown every value of a walk meets a few dozen struct types many times over,
// and reading a type's fields costs far more than remembering the answer. The
// zero IDReader is ready to use; one is not safe for concurrent use.
type IDReader struct {
	// fields holds where each struct type met declares its identity, read
	// through fieldOf, which is declaredIDField unless a test is counting.
	fields  map[reflect.Type]idField
	fieldOf func(reflect.Type) idField
}

// DeclaredIDOf returns the class of ID v declares for itself, that ID, and
// whether v declares one at all: a field named ID that v's own struct type
// declares, of a named string type. A field an embedded struct promotes is that
// struct's declaration, not v's.
//
// An empty ID is reported with declares true, so a checker can flag it where
// [IDDeclarations] drops it. A value that is no such struct, a pointer, a
// non-struct and the zero [reflect.Value] included, declares nothing.
func (r *IDReader) DeclaredIDOf(v reflect.Value) (class reflect.Type, id string, declares bool) {
	if v.Kind() != reflect.Struct {
		return nil, "", false
	}
	f := r.field(v.Type())
	if !f.declares() {
		return nil, "", false
	}
	return f.class, f.of(v), true
}

// field returns where struct type t declares its identity, reading t only the
// first time r meets it.
func (r *IDReader) field(t reflect.Type) idField {
	if f, read := r.fields[t]; read {
		return f
	}
	read := r.fieldOf
	if read == nil {
		read = declaredIDField
	}
	f := read(t)
	if r.fields == nil {
		r.fields = map[reflect.Type]idField{}
	}
	r.fields[t] = f
	return f
}

// idField is where a struct type declares its own identity: the index of the
// field, and the field's type, which is the class of ID. The zero idField
// declares nothing.
type idField struct {
	index int
	class reflect.Type
}

// declares reports whether the type f was read off declares an identity.
func (f idField) declares() bool {
	return f.class != nil
}

// of returns the identity v, a value of the type f was read off, declares.
func (f idField) of(v reflect.Value) string {
	return v.Field(f.index).String()
}

// declaredIDField returns the field through which struct type t declares its
// own identity, so the rule is written once for a value and for a type.
//
// Only t's own fields are read. A promoted ID is not t's declaration: every type
// node embeds [TypeCommon] and so promotes its ID, and counting that too would
// declare each type ID twice. Reading them by index also allocates nothing,
// where reflect.Type.FieldByName, finding none of the name among them, searches
// the embedded structs breadth-first, which allocates. A field named ID that is
// not a named string type settles no class of reference.
func declaredIDField(t reflect.Type) idField {
	for i := range t.NumField() {
		if f := t.Field(i); f.Name == idFieldName && namedString(f.Type) {
			return idField{index: i, class: f.Type}
		}
	}
	return idField{}
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
	return idClassesOf(idClassRoots())
}

// idClassesOf returns every class of ID a value of one of the types in queue
// can declare, or hold a value declaring.
func idClassesOf(queue []reflect.Type) map[reflect.Type]bool {
	classes := map[reflect.Type]bool{}
	seen := map[reflect.Type]bool{}
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
// the types reachable from t. The class is the one [IDReader.DeclaredIDOf] reads
// off a value, through declaredIDField. A promoted field belongs to the struct that
// declares it, and that struct is reached in its own right.
func appendIDClasses(dst []reflect.Type, classes map[reflect.Type]bool, t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return append(dst, t.Elem())
	case reflect.Map:
		return append(dst, t.Key(), t.Elem())
	case reflect.Struct:
		if f := declaredIDField(t); f.declares() {
			classes[f.class] = true
		}
		for f := range t.Fields() {
			dst = append(dst, f.Type)
		}
		return dst
	default:
		return dst // no other kind holds a field or an element.
	}
}

// namedString reports whether t is a named string type, the shape every ID class
// takes. A plain string is not one: Document.Unmodeled keys on a source
// construct's name, which is a name rather than an identity.
func namedString(t reflect.Type) bool {
	return t.Kind() == reflect.String && t.PkgPath() != ""
}
