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
	// Doc is the parsed document a pointer is read against, as the resolver
	// reads it; see DeclaredAt. It is typed as the pointer walk takes it, which
	// keeps the document model out of this package's imports.
	Doc any
}

// DeclaredAt returns the schema declared at a same-document pointer, found the
// way the resolver finds a $ref's target, and ok=false when the pointer
// addresses no schema.
//
// It is for a reference that is only a string: a discriminator mapping value is
// never resolved, so it carries no declaration of its own (GitHub #530). A
// position the parsed model holds as raw YAML, such as an extension's value or
// an enum member, is no schema here, although the resolver parses one when a
// $ref names it, so a mapping reaches it only once a $ref has interned it
// (GitHub #757).
func (s Scope) DeclaredAt(pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], bool) {
	target, err := jsonpointer.GetTarget(s.Doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if err != nil {
		return nil, false
	}
	js, ok := target.(*oas3.JSONSchema[oas3.Referenceable])
	return js, ok && js != nil
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
// reference, a bare schema name or a malformed ref. A document part naming this
// source file is internal.
//
// It uses the resolver's own references.Reference, since fragments are
// percent-encoded: comparing them raw missed resolvable references and interned
// a second node at a named position (GitHub #40). nodeview.InternalPointer
// mirrors that split.
//
// A fragment that is not a JSON pointer is refused. `#addr` names a $anchor,
// which only the library resolves, and an ID derived from it would be a path no
// coordinate spells (GitHub #141).
func (s Scope) InternalPointer(ref string) (jsontext.Pointer, bool) {
	pointer, ok := FragmentPointer(ref)
	if !ok {
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
	pointer, ok := s.InternalPointer(ref)
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
