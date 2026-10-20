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
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
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
	// it; nil reads nothing there.
	Doc any
	// Bindings answers where each schema $ref lands, as the compiler resolved
	// it (internal/bind), and the schemas there; nil knows none, so no schema
	// $ref resolves.
	Bindings *typed.Bindings
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
	// Reached records each position in the source a reference ObjectAt follows
	// resolves to; nil records nothing.
	Reached *Reached
	// Beside returns the path item use site, and the key written beside its
	// $ref, that a pointer in the source passes through (load.Siblings.Beside).
	// The resolver reads such a pointer through the $ref, so a reference landing
	// there is refused rather than interning what it found at the pointer the
	// field written there is declared at (GitHub #824). Nil refuses none.
	Beside func(jsontext.Pointer) (use jsontext.Pointer, key string, ok bool)
	// Writes reports whether obj is the field written beside a path item $ref
	// at a pointer, held or copied (load.Siblings.Writes): an answer ObjectAt
	// takes although Beside names its pointer. Nil writes none.
	Writes func(pointer jsontext.Pointer, obj any) bool
}

// refuses reports whether ObjectAt refuses answer, the object the resolver
// found at pointer: one Beside names, unless Writes says it is the field
// written there.
func (s Scope) refuses(pointer jsontext.Pointer, answer any) bool {
	return s.beside(pointer) && (s.Writes == nil || !s.Writes(pointer, answer))
}

// beside reports whether pointer is one Beside refuses.
func (s Scope) beside(pointer jsontext.Pointer) bool {
	if s.Beside == nil {
		return false
	}
	_, _, ok := s.Beside(pointer)
	return ok
}

// Reached records the positions in the source a lowering resolved a reference
// to, each hop of a chain included: the declarations the IR carries through
// their uses rather than on a node of their own (GitHub #616).
//
// No lowering reads it. The compiler asks it once the last lowering has
// returned, so what it holds cannot change a lowering's answer, and as a set it
// cannot depend on the order lowerings ran in. That is why sharing it between
// copies of a context is safe, as sharing a memo is.
type Reached struct {
	at map[jsontext.Pointer]bool
}

// NewReached returns an empty record.
func NewReached() *Reached {
	return &Reached{at: map[jsontext.Pointer]bool{}}
}

// Has reports whether a lowering resolved a reference to pointer. A nil record
// has recorded nothing.
func (r *Reached) Has(pointer jsontext.Pointer) bool {
	return r != nil && r.at[pointer]
}

// note records pointer, unless r is nil.
func (r *Reached) note(pointer jsontext.Pointer) {
	if r != nil {
		r.at[pointer] = true
	}
}

// InSource returns s for reading what the source holds, which is never
// Foreign. A reference read as internal names a position in the source, and
// what is written there reads its own references as the source's content
// does, wherever the reference naming it was written.
func (s Scope) InSource() Scope {
	s.Foreign, s.Holder = false, ""
	return s
}

// InDocumentOf returns s for reading what js holds, a schema the bindings
// know: the source's own content (InSource), unless the bindings place js in
// another document, whose content it then is (GitHub #762), as where a
// pointer the source spells passes a reference object into one.
func (s Scope) InDocumentOf(js *oas3.JSONSchema[oas3.Referenceable]) Scope {
	p, ok := s.Bindings.Site(js)
	if !ok || p.Doc == bind.Source {
		return s.InSource()
	}
	s.Foreign, s.Holder = true, s.Bindings.Table().Document(p.Doc).Name
	return s
}

// reached returns s for reading what a hop ending at end found: a Foreign scope
// over the document it resolved against, unless that is the source (its model,
// or its file read again where the load phase does not hold it). There it is
// the source's own content, unless the walk to its pointer passes a $ref,
// which is followed to where its own chain ends, a turn each. Past maxRefChain
// turns, and for a chain endOf cut, it is no document's: Foreign with no
// Holder, so none of its references reads as internal.
func (s Scope) reached(end End) Scope {
	return s.turned(end, maxRefChain)
}

// turned is reached with turns walks left to take.
func (s Scope) turned(end End, turns int) Scope {
	for range turns {
		if end.Document != s.Doc && !SameDocument(s.SelfPath, end.Path) {
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

// located is where the chain of the last reference a walk of a pointer
// through the model passed ends, and whether it passed one.
type located struct {
	end    End
	passes bool
}

// locate walks pointer through Doc as the resolver's walk reads it, a token at
// a time, noting each reference Ends names that it steps past, read as what it
// resolved to. A schema's own $ref is read as a keyword, so the walk ends at a
// schema, as it does at a token that leaves the model (navigation.ReadingOf):
// raw YAML holds no reference the resolver resolved (see leave). Without Ends
// it notes nothing.
func (s Scope) locate(pointer jsontext.Pointer) located {
	var l located
	tokens, ok := navigation.Tokens(pointer)
	if !ok || s.Ends == nil {
		return l
	}
	node := s.Doc
	for _, token := range tokens {
		if _, schema := node.(*oas3.JSONSchema[oas3.Referenceable]); schema {
			return l
		}
		reading, raw := navigation.ReadingOf(node, token)
		if reading == navigation.Leaving {
			l.leave(s, node, raw, token)
			return l
		}
		next, ok := navigation.Step(node, token)
		if !ok {
			return l
		}
		if e, isRef := s.Ends(node); isRef {
			l.end, l.passes = e, true
		}
		node = next
	}
	return l
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

// TargetPointer returns the pointer of the position the $ref at js lands on
// in the source, as the compiler resolved it (bind.Hop.Spelled): a
// "#/$defs/..." pointer's definition, read by the $defs rule, or the position
// any other names from the source's root. ok is false for a $ref that lands on
// no schema, one that lands in another document, and one on a loop of $refs,
// which lands nowhere a type can be read.
func (s Scope) TargetPointer(js *oas3.JSONSchema[oas3.Referenceable]) (jsontext.Pointer, bool) {
	e, ok := s.Bindings.Entry(js)
	if !ok || !lands(e.Hop) || e.Chain.Member {
		return "", false
	}
	return e.Hop.Spelled, true
}

// lands reports whether h lands on a schema in the source.
func lands(h bind.Hop) bool {
	return h.Status == bind.Resolved && h.Named
}

// MappingTarget returns the pointer the mapping value of the discriminator d
// names, read as a $ref written at d is (bind.Mapping), and the schema written
// there. ok is false for a value that lands on no schema in the source.
func (s Scope) MappingTarget(d *oas3.Discriminator, value string,
) (jsontext.Pointer, *oas3.JSONSchema[oas3.Referenceable], bool) {
	m, ok := s.Bindings.Mapping(d, value)
	if !ok || !lands(m.Hop) {
		return "", nil, false
	}
	return m.Hop.Spelled, s.Bindings.At(m.Hop.Target), true
}

// FragmentPointer returns the JSON pointer a $ref's fragment spells, whatever
// document the reference names, as fragment.Pointer reads it. Unlike
// InternalPointer, it does not ask what this document can resolve.
//
// It reports ok=false for a reference with no fragment, a fragment that is not
// a pointer (`#name` names a $anchor), a bare `#`, which names the whole
// document, and a fragment decoding to bytes that are not UTF-8, which no
// document key can spell and the IR cannot encode in an ID (GitHub #520).
func FragmentPointer(ref string) (jsontext.Pointer, bool) {
	pointer := fragment.Pointer(ref)
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
// It decodes the fragment (fragment.Pointer), since fragments are
// percent-encoded: comparing them raw missed resolvable references and interned
// a second node at a named position (GitHub #40). nodeview.InternalPointer
// reads the same fragment.
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
// one, or from one not below it (filepath.IsLocal).
func SameDocument(a, b string) bool {
	return sameDocument(a, b, systemRules())
}

// pathRules is how the file system reads a path. A test hands in another
// system's reading, so the Windows forms are checked on any host.
type pathRules struct {
	abs     func(path string) (string, error) // against the working directory
	isAbs   func(path string) bool
	isLocal func(path string) bool
}

// systemRules returns filepath's reading of a path on this system.
func systemRules() pathRules {
	return pathRules{abs: filepath.Abs, isAbs: filepath.IsAbs, isLocal: filepath.IsLocal}
}

// sameDocument is SameDocument with the file system's reading handed in, so a
// test can count how often the working directory is read, and read paths as
// another system does.
func sameDocument(a, b string, rules pathRules) bool {
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
	if sameAnchor(a, b, rules) {
		return false
	}
	absA, errA := rules.abs(a)
	absB, errB := rules.abs(b)
	return errA == nil && errB == nil && absA == absB
}

// sameAnchor reports whether two cleaned paths mean the same wherever the
// working directory is, so that when they differ they name different files:
// both absolute, or both below it. Below is filepath.IsLocal, which on Windows
// also refuses a path rooted without a volume (\a\b), one naming a drive
// (C:a\b) and a reserved name (NUL): none is absolute, and each is made so
// before it is compared.
func sameAnchor(a, b string, rules pathRules) bool {
	if rules.isAbs(a) != rules.isAbs(b) {
		return false
	}
	return rules.isAbs(a) || rules.isLocal(a) && rules.isLocal(b)
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

// NamesReferent reports whether the $ref at js names a schema this
// compilation can point a TypeRef at, answering what resolveSchemaRef answers
// without interning anything on the way, since deciding how to lower a schema
// must not hoist nodes as a side effect of asking.
//
// It must stay in step with resolveSchemaRef, and mirrors it minus the two
// steps that are not pure lookups: hoistSubSchema, which interns (its only
// failure, a target with neither a body nor a boolean, is the last condition
// here), and the InternedID hit, which would make the answer depend on which
// schema lowered first.
func (s Scope) NamesReferent(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	pointer, ok := s.TargetPointer(js)
	if !ok {
		return false
	}
	if _, resolved, handled := s.ComponentRef(pointer); handled {
		return resolved
	}
	decl := s.Bindings.Declared(js)
	return decl != nil && (decl.IsBool() || annotation.At(decl).Node != nil)
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

// TargetSchema returns the schema the chain of $refs from js ends on when js
// is a $ref, so use-site annotations can fall back to the referent, and nil
// otherwise or where the chain ends on none.
func (s Scope) TargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !IsRefSite(js, ref) {
		return nil
	}
	return s.Bindings.End(js).GetSchema()
}
