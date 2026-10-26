package resolve

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
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
	// third.
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

// InSource returns s for reading what the source holds, which is never
// Foreign. A reference read as internal names a position in the source, and
// what is written there reads its own references as the source's content
// does, wherever the reference naming it was written.
func (s Scope) InSource() Scope {
	s.Foreign, s.Holder = false, ""
	return s
}

// InDocumentOf returns s for reading what js holds, a schema the bindings
// know, in the document they place it in (InDocument), as where a pointer the
// source spells passes a reference object into another (GitHub #762); a
// schema they do not know is read in the source.
func (s Scope) InDocumentOf(js *oas3.JSONSchema[oas3.Referenceable]) Scope {
	p, ok := s.Bindings.Site(js)
	if !ok {
		return s.InSource()
	}
	return s.InDocument(p.Doc)
}

// InDocument returns s for reading what document id of the bindings' table
// holds: the source's own content (InSource), or that document's.
func (s Scope) InDocument(id bind.DocID) Scope {
	if id == bind.Source {
		return s.InSource()
	}
	s.Foreign, s.Holder = true, s.Bindings.Table().Document(id).Name
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

// maxRefChain bounds how many $ref hops ObjectAt, UseDocs and Hops follow, and
// how many a chain's end (endOf) and a pointer's scope (Scope.reached) are
// followed through. A $ref cycle among the non-schema components is reported by
// the resolver (docs/parser-library.md#reference-object-cycle), not refused by
// internal/scan, and a failed chain resolves to nothing, so the bound only fires
// on an absurd alias chain. Past it, ObjectAt keeps the use site, and what the
// chain reaches is read in no document's scope.
//
// It stays unexported: no code outside the package reads it, and the tests
// outside it reach it through export_test.go.
const maxRefChain = 32
