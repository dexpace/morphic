package ir

import (
	"reflect"
)

// idFieldName is the field through which a node declares its own identity. It is
// spelled once here because [IDReader.DeclaredIDOf] reaches it by name, which
// the Go compiler cannot check.
const idFieldName = "ID"

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

// IDDeclarations collects, as a [Visitor], the identities the values its walk
// reaches declare, in walk order and each with its path, for a check reporting
// where one is declared, such as twice. Reading them off the value graph covers
// a new ID-bearing node the moment it exists. [IDReferences] reads the same
// identities to resolve references, rendering no path.
//
// An empty ID is skipped, since nothing can reference one and several nodes
// carrying one are not duplicates. Reporting it is irverify.checkRegistryKeys's
// job for a map-keyed class and irverify.checkDeclaredIDs's for the rest. The
// zero IDDeclarations is ready to walk with.
type IDDeclarations struct {
	ids  IDReader
	list []IDDeclaration
}

// Visit records the identity v declares, if any, and asks for v's children.
func (d *IDDeclarations) Visit(v reflect.Value, at WalkPath) bool {
	if class, id, declares := d.ids.DeclaredIDOf(v); declares && id != "" {
		d.list = append(d.list, IDDeclaration{Class: class, ID: id, Path: at.String()})
	}
	return true
}

// All returns the declarations collected so far, in walk order. The slice is
// d's own, as bytes.Buffer.Bytes's is, and valid until the next Visit.
func (d *IDDeclarations) All() []IDDeclaration {
	return d.list
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
// where reflect.Type.FieldByName, missing a field of the name, searches every
// embedded struct and allocates for each. A field named ID that is not a named
// string type settles no class of reference.
func declaredIDField(t reflect.Type) idField {
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Name != idFieldName {
			continue
		}
		if !namedString(f.Type) {
			return idField{} // Go allows one field of a name, so no later one is
		}
		return idField{index: i, class: f.Type}
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
