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
	"path/filepath"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
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
	// Built returns the schema the load phase walked at a raw position, or nil:
	// the one whose $refs it resolved, which a $ref to the position may not
	// reach when spelled so the resolver built it a copy.
	Built func(jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable]
	// Foreign marks a scope over content another document holds, read where
	// that document holds it (GitHub #762). A reference there is internal only
	// when its document part, resolved against Holder as the resolver resolves
	// it, names the source. Any other names a position in that document or a
	// third, which this compile cannot address (GitHub #74).
	Foreign bool
	// Holder is the path the resolver read that document by.
	Holder string
	// Ends returns where the chain of a reference a walk through Doc passes
	// ends (EndOf), and false for a node that is no reference. The library's
	// reference kinds are each a type of their own, which the caller names;
	// nil passes no reference.
	Ends func(node any) (End, bool)
	// Holds reports whether the library's read of token in raw finds a node:
	// what a walk leaving the model at a reference asks of the mapping its
	// target was built from. Answered from an index, it spares the scan of that
	// mapping the library makes (GitHub #778); nil asks the library.
	Holds func(raw *yaml.Node, token string) bool
}

// InSource returns s for reading what the source holds, which is never
// Foreign. A reference read as internal names a position in the source, and
// what is written there reads its own references as the source's content
// does, wherever the reference naming it was written.
func (s Scope) InSource() Scope {
	s.Foreign, s.Holder = false, ""
	return s
}

// At returns s for reading what an internal pointer names: the source's own
// content (InSource), unless the resolver's walk to it passes a $ref, whose
// target it then lies in (GitHub #762). Past one into another document, it is
// that document's content, though the resolver reports the source as the
// document the pointer resolved against.
func (s Scope) At(pointer jsontext.Pointer) Scope {
	return s.reached(End{Document: s.Doc, Pointer: pointer})
}

// reached returns s for reading what a hop ending at end found: a Foreign scope
// over the document it resolved against, unless that is the source (its model,
// or its file read again where the load phase does not hold it), where the
// position its pointer names is read as At reads it. A $ref the walk to that
// position passes is followed to where its own chain ends, a turn each. Past
// maxRefChain turns, and for a chain endOf cut, it is no document's: Foreign
// with no Holder, so none of its references reads as internal.
func (s Scope) reached(end End) Scope {
	return s.turned(end, maxRefChain)
}

// turned is reached with turns walks left to take.
func (s Scope) turned(end End, turns int) Scope {
	for range turns {
		if end.Document != any(s.Doc) && !SameDocument(s.SelfPath, end.Path) {
			s.Foreign, s.Holder = true, end.Path
			return s
		}
		walk := s.locate(end.Pointer)
		if !walk.passes {
			return s.InSource()
		}
		end = walk.end
	}
	s.Foreign, s.Holder = true, ""
	return s
}

// located is what a walk of a pointer through the model found: what the model
// holds there, nil where the read finds nothing or leaves the model, and where
// the chain of the last reference the walk passed ends.
type located struct {
	target any
	end    End
	passes bool
}

// locate walks pointer through Doc as the resolver's walk reads it, a token at
// a time, noting each reference Ends names that it steps past, read as what it
// resolved to. A schema's own $ref is read as a keyword, so none is noted past
// a schema. A token that leaves the model (navigation.Leaves) ends the walk:
// raw YAML holds no schema and no reference the resolver resolved (see leave).
// Past an index the library retries (navigation.Retried), the pointer names
// what the library's read of the rest finds, though references are still noted
// a step at a time.
func (s Scope) locate(pointer jsontext.Pointer) located {
	var l located
	tokens, ok := navigation.Tokens(pointer)
	if !ok {
		return l
	}
	noting, whole := s.Ends != nil, false
	var node any = s.Doc
	for i, token := range tokens {
		if _, schema := node.(*oas3.JSONSchema[oas3.Referenceable]); schema {
			noting = false
		}
		reading, raw := navigation.ReadingOf(node, token)
		if reading == navigation.Leaving {
			if noting {
				l.leave(s, node, raw, token)
			}
			return l
		}
		if reading == navigation.Retried && !whole {
			l.target, whole = retriedRead(node, tokens[i:]), true
		}
		next, ok := navigation.Step(node, token)
		if !ok {
			return l
		}
		if e, isRef := s.ends(noting, node); isRef {
			l.end, l.passes = e, true
		}
		node = next
	}
	if !whole {
		l.target = node
	}
	return l
}

// retriedRead returns what the library's read of tokens from node finds, or
// nil where it finds nothing. The library retries tokens[0] as an index once
// the rest fails below it as a key, so only the whole read says which answers
// (navigation.Walk).
func retriedRead(node any, tokens []string) any {
	target, _, err := navigation.Walk(node, tokens)
	if err != nil {
		return nil
	}
	return target
}

// ends is Ends's answer for node, and false while a walk notes nothing.
func (s Scope) ends(noting bool, node any) (End, bool) {
	if !noting {
		return End{}, false
	}
	return s.Ends(node)
}

// leave notes node passed where the walk leaves the model from it for raw: a
// reference is stepped past only where raw, the mapping its target was built
// from, holds token, as the resolver's read of the pointer finds it (see
// Scope.Holds). Raw YAML is no reference, and the walk passes nothing it holds.
func (l *located) leave(s Scope, node any, raw *yaml.Node, token string) {
	if _, isRaw := node.(*yaml.Node); isRaw {
		return
	}
	if e, isRef := s.Ends(node); isRef && held(s.Holds, node, raw, token) {
		l.end, l.passes = e, true
	}
}

// held reports whether the library's read of token from node, which leaves the
// model for raw, finds a node: holds's answer, or the library's without one.
func held(holds func(*yaml.Node, string) bool, node any, raw *yaml.Node, token string) bool {
	if holds != nil {
		return holds(raw, token)
	}
	_, found := navigation.Step(node, token)
	return found
}

// Location is what one walk of a same-document pointer through the model found
// (Locate): ModelAt's, DeclaredAt's and At's answers for it at once.
type Location struct {
	scope   Scope
	pointer jsontext.Pointer
	walk    located
}

// Locate walks pointer through the model once, for the answers ModelAt,
// DeclaredAt and At would each walk it again for: a hoisted reference asks two
// of them about one pointer.
func (s Scope) Locate(pointer jsontext.Pointer) Location {
	return Location{scope: s, pointer: pointer, walk: s.locate(pointer)}
}

// Pointer returns the pointer l located.
func (l Location) Pointer() jsontext.Pointer { return l.pointer }

// Model is ModelAt's answer for l's pointer: nil for anything but a schema,
// as for a keyword a schema leaves unset, which the walk reaches as a nil one.
func (l Location) Model() *oas3.JSONSchema[oas3.Referenceable] {
	js, _ := l.walk.target.(*oas3.JSONSchema[oas3.Referenceable])
	return js
}

// Declared is DeclaredAt's answer for l's pointer.
func (l Location) Declared() *oas3.JSONSchema[oas3.Referenceable] {
	if js := l.Model(); js != nil {
		return js
	}
	if l.scope.Mapped == nil {
		return nil
	}
	return l.scope.Mapped(l.pointer)
}

// At is At's answer for l's pointer, whose first turn is the walk Locate took.
func (l Location) At() Scope {
	if !l.walk.passes {
		return l.scope.InSource()
	}
	return l.scope.turned(l.walk.end, maxRefChain-1)
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
// declaration of its own (GitHub #530). The model's schema answers first: the
// load phase's may be a copy a $ref by the source's file name built, whose own
// $refs nothing resolved. Mapped answers where the model holds raw YAML, such
// as an extension's value or an enum member (GitHub #757).
func (s Scope) DeclaredAt(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return s.Locate(pointer).Declared()
}

// ModelAt returns the schema the source's model holds at a same-document
// pointer, found the way the resolver finds a $ref's target, or nil where it
// holds none, as at a position it holds as raw YAML (see locate).
func (s Scope) ModelAt(pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	return s.Locate(pointer).Model()
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
// source file is internal (see namesSelf).
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
	if !ok || !s.namesSelf(references.Reference(ref)) {
		return "", false
	}
	return pointer, true
}

// namesSelf reports whether ref names a position in the source. One with no
// document part does in the source's own content, and in no other. Otherwise
// its document part is read as the resolver reads it, against the document
// holding the reference (documentOf), and names the source when that is the
// source. The source's own content and a Foreign scope differ in that base
// only: SelfPath, or Holder.
func (s Scope) namesSelf(ref references.Reference) bool {
	base := s.SelfPath
	if s.Foreign {
		base = s.Holder
	} else if ref.GetURI() == "" {
		return true
	}
	doc, ok := documentOf(ref, base)
	return ok && SameDocument(s.SelfPath, doc)
}

// NamesHolder reports whether ref, in a Foreign scope, names a position in the
// document holding it, which this compile cannot lower (GitHub #74).
func (s Scope) NamesHolder(ref string) bool {
	if !s.Foreign {
		return false
	}
	doc, ok := documentOf(references.Reference(ref), s.Holder)
	return ok && SameDocument(s.Holder, doc)
}

// documentOf returns the path of the document ref names when it is written in
// the document at base: its document part joined onto base's directory and
// cleaned, as the resolver resolves it, or base for none. It reports false for
// no base and for a reference the resolver could not place.
func documentOf(ref references.Reference, base string) (string, bool) {
	if base == "" {
		return "", false
	}
	abs, err := references.ResolveAbsoluteReference(ref, base)
	if err != nil {
		return "", false
	}
	return abs.AbsoluteReference, true
}

// SameDocument reports whether paths a and b name one document: the same
// spelling, or, for two file paths, the same path once cleaned and made
// absolute. The test is lexical, as for a URI reference, so a file reached
// through a symlink or another link is another document, and a directory that
// does not exist cleans away like one that does. A URL (IsURL) names one only
// as spelled, and an empty path names none.
//
// The working directory is read only to tell a relative path from an absolute
// one, or one climbing out of it.
func SameDocument(a, b string) bool {
	return sameDocument(a, b, filepath.Abs)
}

// sameDocument is SameDocument with the way to make a path absolute handed in,
// so a test can count how often the working directory is read.
func sameDocument(a, b string, abs func(string) (string, error)) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if IsURL(a) || IsURL(b) {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if sameAnchor(a, b) {
		return false
	}
	absA, errA := abs(a)
	absB, errB := abs(b)
	return errA == nil && errB == nil && absA == absB
}

// sameAnchor reports whether two cleaned paths mean the same wherever the
// working directory is, so that when they differ they name different files:
// both absolute, or both relative and below it.
func sameAnchor(a, b string) bool {
	if filepath.IsAbs(a) != filepath.IsAbs(b) {
		return false
	}
	return filepath.IsAbs(a) || !climbs(a) && !climbs(b)
}

// climbs reports whether a cleaned relative path leaves the directory it is
// relative to.
func climbs(clean string) bool {
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// IsURL reports whether location names a document by URL rather than as a
// file, as the resolver classifies it (references.ResolveAbsoluteReference):
// by the scheme url.Parse finds, but for a drive letter before a backslash. A
// scheme needs no "//" after it, and a file path holding "://" past a
// directory named like a scheme is still a file. A location the resolver
// cannot parse is none; it resolves nothing.
func IsURL(location string) bool {
	abs, err := references.ResolveAbsoluteReference("", location)
	return err == nil && abs.Classification != nil && abs.Classification.IsURL
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
