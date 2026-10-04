// Package resolve answers what a $ref names: the same-document pointer it
// addresses, the schema declared there and the type interned there, and, for
// components that are not schemas, the concrete value and declaration site a
// reference-or-inline entry stands for.
//
// It is only that. Following a reference far enough to lower its target
// recurses into the schema walk, so it stays with the walk. What is here needs
// only the document, its path, its declared names and a registry.
//
// Reference resolution is not promoted to compilers/compile: not every compiler
// needs it, and those that do reach it by different mechanisms
// (docs/micro-compiler-design.md §3.2).
package resolve

import (
	"encoding/json/jsontext"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// Scope is what resolving a reference needs to know about the document doing
// the referencing: which file it is, which component schemas it declares, and
// what it parsed to.
//
// Declares is a predicate rather than the name set itself, for the reason the
// lowering context keeps that set behind an accessor: a copied struct shares a
// map, and a reader has no business writing to one.
type Scope struct {
	// SelfPath is the source path of the document being compiled, against which
	// a reference's document part is judged internal or external.
	SelfPath string
	// Declares reports whether the document declares a component schema of this
	// name.
	Declares func(name string) bool
	// Doc is the parsed document a pointer is read against, as the resolver reads
	// it; nil leaves every "#/$defs/..." pointer unresolved and every DeclaredAt
	// answer nil.
	Doc defs.Navigable
	// Defs reads the document's "#/$defs/..." pointers and remembers what it has
	// read, so references that share ancestors share the work. Nil reads each
	// through a fresh reader over Doc, which answers the same and shares nothing.
	Defs *defs.Reader
	// Mapped returns the schema the load phase resolved at a pointer a
	// discriminator mapping target names, or nil; see DeclaredAt.
	Mapped func(jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable]
	// Foreign marks a scope over content another document holds, where a
	// reference names a position in that document, which this compile cannot
	// address (GitHub #74): no reference is internal (GitHub #762).
	Foreign bool
}

// reader returns the reader s reads "#/$defs/..." pointers through.
func (s Scope) reader() *defs.Reader {
	if s.Defs != nil {
		return s.Defs
	}
	return defs.NewReader(s.Doc)
}

// TargetPointer returns the pointer of the position the $ref at js resolves to
// in this document: InternalPointer's answer, except that a "#/$defs/..."
// pointer names the definition the resolver's rule finds relative to js
// (defs.Reader.Target), never the document-rooted /$defs/... it spells, which
// the document does not have (GitHub #557). ok is false for a reference into
// another document and for a $defs pointer the rule finds nothing for.
func (s Scope) TargetPointer(js *oas3.JSONSchema[oas3.Referenceable], ref string) (jsontext.Pointer, bool) {
	pointer, ok := s.InternalPointer(ref)
	if !ok || !defs.IsPointer(pointer) {
		return pointer, ok
	}
	if _, held := defs.PointerOf(references.Reference(ref)); !held {
		return "", false // left to the resolver, as load leaves it: see load.heldRefs
	}
	_, at, found := s.reader().Target(js, pointer)
	return at, found
}

// MappingPointer is TargetPointer for a discriminator mapping value: the
// pointer it names, which for a "#/$defs/..." value is the definition read from
// the discriminator d that holds it (defs.Reader.MappingTarget), as a $ref in
// the same schema reads one (GitHub #557). ok is false for a value into another
// document, for a $defs value the rule finds nothing for, and for no
// discriminator.
func (s Scope) MappingPointer(d *oas3.Discriminator, value string) (jsontext.Pointer, bool) {
	pointer, ok := s.InternalPointer(value)
	if !ok || !defs.IsPointer(pointer) {
		return pointer, ok
	}
	if _, held := defs.PointerOf(references.Reference(value)); !held {
		return "", false
	}
	_, at, found := s.reader().MappingTarget(d, pointer)
	return at, found
}

// DeclaredAt returns the schema declared at a same-document pointer, found the
// way the resolver finds a $ref's target, or nil where there is none, like
// annotation.DeclaredSchema for a followed $ref.
//
// It is for a mapping value, which the resolver never follows, so it carries no
// declaration of its own (GitHub #530). Mapped answers first: the load phase
// resolves each value as a $ref, which parses a position the model holds as
// raw YAML, such as an extension's value or an enum member (GitHub #757). The
// model answers for a value spelled so the load phase does not resolve it.
func (s Scope) DeclaredAt(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	if s.Mapped != nil {
		if js := s.Mapped(pointer); js != nil {
			return js
		}
	}
	target, err := jsonpointer.GetTarget(s.Doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if err != nil {
		return nil
	}
	// Anything but a schema fails the assertion and leaves js nil, and so does a
	// keyword the schema leaves unset, which the walk reaches as a typed nil.
	js, _ := target.(*oas3.JSONSchema[oas3.Referenceable])
	return js
}

// sameFile reports whether a $ref document part names this compilation's own
// source file: an exact path match, or a bare filename equal to our own
// basename, since self-references are conventionally spelled that way
// (`m.yaml#/...` inside m.yaml). A part carrying its own directory is matched
// in full, so `dir2/m.yaml` referenced from `dir1/m.yaml` is not a
// self-reference.
//
// The slash test is redundant with the basename equality: path.Base yields a
// separator only for "/", which the exact match has already taken. It states
// the intent, and holds if the comparison is loosened.
func (s Scope) sameFile(doc string) bool {
	self := s.SelfPath
	if self == "" {
		return false
	}
	if doc == self {
		return true
	}
	return !strings.Contains(doc, "/") && doc == path.Base(self)
}

// FragmentPointer returns the JSON pointer a $ref's fragment spells, whatever
// document the reference names: the text after '#', trimmed and percent-decoded
// as the resolver reads it (references.Reference). Unlike InternalPointer, it
// does not ask what this document can resolve.
//
// It reports ok=false for a reference with no fragment, a fragment that is not
// a pointer (`#name` names a $anchor), a bare `#`, which names the whole
// document, and a fragment decoding to bytes that are not UTF-8, which no
// document key can spell and the IR cannot encode in an ID (GitHub #520).
func FragmentPointer(ref string) (jsontext.Pointer, bool) {
	pointer := jsontext.Pointer(references.Reference(ref).GetJSONPointer())
	if !strings.HasPrefix(string(pointer), "/") {
		return "", false
	}
	if !utf8.ValidString(string(pointer)) {
		return "", false
	}
	return pointer, true
}

// InternalPointer returns the same-document JSON pointer a $ref (or
// discriminator mapping) target addresses, and ok=false for a cross-document
// reference, a bare schema name, a malformed ref, or any ref in a Foreign
// scope. A document part naming this source file is internal.
//
// It uses the resolver's own references.Reference, since fragments are
// percent-encoded: comparing them raw missed resolvable references and interned
// a second node at a named position (GitHub #40). nodeview.InternalPointer
// mirrors that split.
//
// A `#addr` fragment names a $anchor, which only the library resolves, and an
// ID derived from it would be a path no coordinate spells (GitHub #141).
func (s Scope) InternalPointer(ref string) (jsontext.Pointer, bool) {
	pointer, ok := FragmentPointer(ref)
	if !ok || s.Foreign {
		return "", false
	}
	if doc := references.Reference(ref).GetURI(); doc != "" && !s.sameFile(doc) {
		return "", false
	}
	return pointer, true
}

// ComponentRef resolves an internal pointer addressing a top-level component
// schema to its stable named ID, but only when that component is declared. It
// returns handled=true once the pointer is classified as a component pointer,
// declared or not, so callers can stop: a declared component yields ok=true, an
// undeclared one ok=false (a dangling reference to drop). The ID is rebuilt
// from the component's canonical name, unescaped then re-escaped by ids.Ptr,
// not from the incoming pointer text, so a non-canonically escaped reference
// (`A~B` for a component named "A~B", interned under `A~0B`) still resolves to
// the interned node.
func (s Scope) ComponentRef(pointer jsontext.Pointer) (id ir.TypeID, ok, handled bool) {
	name, isComponent := ids.ComponentSchemaName(pointer)
	if !isComponent {
		return "", false, false
	}
	if s.Declares(name) {
		return ids.NamedType(ids.Ptr("components", "schemas", name)), true, true
	}
	return "", false, true
}

// InternedID returns the TypeID a node was interned under at pointer, when one
// already exists there — either a previously hoisted sub-schema (via byPointer)
// or a node registered directly under its pointer-derived ID.
func InternedID(ts *compile.Types, pointer jsontext.Pointer) (ir.TypeID, bool) {
	if id, ok := ts.Lookup(string(pointer)); ok {
		return id, true
	}
	id := ids.ForPointer(pointer)
	if _, ok := ts.Node(id); ok {
		return id, true
	}
	return "", false
}

// NamesReferent reports whether ref names a schema this compilation can point a
// TypeRef at, answering what resolveSchemaRef answers without interning
// anything on the way, since deciding how to lower a schema must not hoist
// nodes as a side effect of asking.
//
// It must stay in step with resolveSchemaRef, and mirrors it minus the two
// steps that are not pure lookups: hoistSubSchema, which interns (its own only
// failure, a target declaring no schema body, is the last condition here), and
// the InternedID hit, which would make the answer depend on which schema
// lowered first.
func (s Scope) NamesReferent(js *oas3.JSONSchema[oas3.Referenceable], ref string) bool {
	pointer, ok := s.TargetPointer(js, ref)
	if !ok {
		return false
	}
	if _, resolved, handled := s.ComponentRef(pointer); handled {
		return resolved
	}
	decl := annotation.DeclaredSchema(js)
	return decl != nil && annotation.At(decl).Node != nil
}

// IsRefSite reports whether a position is $ref-shaped: the resolver's own
// IsReference (a non-empty $ref), or a schema body that carries a Ref field of
// its own even when empty. That is deliberately broader than annotation.At's
// classification: schemaRefHomed, TargetSchema, and bodySchemaPointer
// (content.go) all need "there is a $ref-carrying body here," not "there is a
// genuine, followable reference," so the degenerate {$ref: ""} shape counts for
// them even though it does not count as an annotation.Reference. s is the
// schema body the caller already holds; a nil s (a boolean schema carries none)
// is never $ref-shaped.
func IsRefSite(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	return js.IsReference() || (s != nil && s.Ref != nil)
}

// TargetSchema returns the resolved target schema when js is a $ref, so
// use-site annotations can fall back to the referent; it returns nil otherwise.
func TargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !IsRefSite(js, ref) {
		return nil
	}
	resolved := js.GetResolvedSchema()
	if resolved == nil {
		return nil
	}
	return resolved.GetSchema()
}
