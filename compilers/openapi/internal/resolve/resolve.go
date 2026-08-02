// Package resolve answers what a $ref names: which same-document pointer it
// addresses, which interned type, if any, already lives there, and — for the
// components that are not schemas — which concrete value and declaration site a
// reference-or-inline entry stands for.
//
// It is deliberately only that. Following a reference far enough to lower what
// it points at is schema lowering reached through a reference, not resolution,
// and it recurses back into the schema walk — so it stays with the walk rather
// than crossing this boundary. What is here needs nothing but the document's own
// path, what it declares, and a registry to look an ID up in, which is why it is
// a package at all.
//
// Reference resolution is not promoted to compilers/compile: two of the three
// compilers need it and need it by different mechanisms, which is the shape that
// looks promotable and is not.
package resolve

import (
	"path"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// Scope is what resolving a reference needs to know about the document doing
// the referencing: which file it is, and which component schemas it declares.
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
}

// sameFile reports whether a $ref document part names this compilation's own
// source file. An exact path match is internal; so is a bare filename (no
// directory) equal to our own basename, since self-references are
// conventionally spelled with just the file's own name (e.g. `m.yaml#/...`
// inside m.yaml). A doc part carrying its own directory is matched in full,
// never on basename alone — otherwise `dir2/m.yaml` referenced from
// `dir1/m.yaml` would misread as a self-reference.
//
// The equality alone already gives that, since path.Base never yields a string
// containing a separator; the explicit slash test states the rule rather than
// leaving it to be re-derived, and is what holds if the comparison is ever
// loosened. The two answers differ only for a source path of "/", which is not
// a document.
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

// InternalPointer returns the same-document JSON pointer a $ref (or discriminator
// mapping) target addresses, and ok=false for a genuine cross-document reference,
// a bare schema name, or a malformed ref. A document part naming this same source
// file (an OpenAPI self-reference) is treated as internal — Milestone 1 interns
// only same-file targets; genuinely external ones are diagnosed and dropped.
//
// The split into document and pointer is the resolver's own (references.Reference,
// v1.24.0) rather than a hand-rolled one: a $ref is a URI, so its fragment is
// percent-encoded, and `#/components/schemas/Foo%2DBar` names the component
// "Foo-Bar". Comparing the raw fragment against declared names instead reported a
// reference the resolver had resolved as unresolved, and degraded the position to
// `any` (GitHub #40). Asking the resolver is what stops the answer drifting from
// it again; nodeview.InternalPointer mirrors the same two methods for the cycle
// scan, and records what a dependency bump should re-check.
//
// A fragment that is not a JSON pointer is refused here rather than passed on.
// `#addr` names a JSON Schema `$anchor`, not a coordinate, and Milestone 1
// resolves no anchors; letting it through returned "addr" as though it were a
// pointer, and every ID derived from it was a path no source coordinate spells
// (GitHub #141). The resolver library happens to reject it too, but relying on
// that puts the refusal outside this compiler, where a library that started
// resolving anchors would silently reinstate the malformed derivation.
func (s Scope) InternalPointer(ref string) (string, bool) {
	r := references.Reference(ref)
	if !r.HasJSONPointer() {
		return "", false
	}
	pointer := string(r.GetJSONPointer())
	if !strings.HasPrefix(pointer, "/") {
		return "", false
	}
	if doc := r.GetURI(); doc != "" && !s.sameFile(doc) {
		return "", false
	}
	return pointer, true
}

// ComponentRef resolves an internal pointer addressing a top-level
// component schema to its stable named ID, but only when that component is
// declared. It returns handled=true once the pointer is classified as a
// component pointer (declared or not), so callers can stop; a declared
// component yields ok=true, an undeclared one ok=false (a dangling reference to
// drop). The ID is rebuilt from the component's canonical name — unescaped, then
// re-escaped by ids.Ptr — rather than from the incoming pointer text, so a
// non-canonically escaped reference (e.g. `A~B` for a component named "A~B",
// interned under `A~0B`) still resolves to the interned node instead of an
// unbacked ID.
func (s Scope) ComponentRef(pointer string) (id ir.TypeID, ok, handled bool) {
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
func InternedID(ts *compile.Types, pointer string) (ir.TypeID, bool) {
	if id, ok := ts.Lookup(pointer); ok {
		return id, true
	}
	id := ids.ForPointer(pointer)
	if _, ok := ts.Node(id); ok {
		return id, true
	}
	return "", false
}

// NamesReferent reports whether ref names a schema this compilation can
// point a TypeRef at, answering the question resolveSchemaRef answers without
// interning anything on the way. A classifier needs that: deciding how to lower
// a schema must not hoist nodes as a side effect of asking.
//
// It sits beside resolveSchemaRef because it must stay in step with it, and
// mirrors it minus the two steps that are not pure lookups — hoistSubSchema,
// which interns (its own only failure is a target that declares no schema body,
// which is the last condition here), and the internedID cache hit, which would
// make the answer depend on which schema happened to lower first.
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
// classification: schemaRef, refTargetSchema, and bodySchemaPointer
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
