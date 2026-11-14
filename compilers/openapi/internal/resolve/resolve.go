// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

// Scope is what resolving a reference needs to know about the document doing
// the referencing: which file it is, which component schemas it declares, and
// where its references land.
//
// Declares is a Names rather than a map because a copied struct shares a map,
// and a reader has no business writing to one.
type Scope struct {
	// SelfPath is where the document being compiled lies, as its references
	// are read against (load.Location), against which a reference's document
	// part is judged internal or external.
	SelfPath string
	// Places is where the compile's files lie, which a document part naming
	// the source is compared by.
	Places *Places
	// Declares holds the names of the component schemas the document declares.
	Declares Names
	// Bindings answers where each reference lands, a schema's $ref or a
	// Reference Object's, as the compiler resolved it (internal/bind), and the
	// models there; nil knows none, so no reference resolves.
	Bindings *typed.Bindings
	// Foreign marks a scope over content another document holds, read where
	// that document holds it (GitHub #762). A reference there is internal only
	// when its document part, resolved against Holder, names the source. Any
	// other names a position in that document or a third.
	Foreign bool
	// Holder is the name the bindings read that document by.
	Holder string
	// Reached records each position in the source a reference ObjectAt follows
	// resolves to; nil records nothing.
	Reached *Reached
}

// Names is a set of names, such as the component schemas a document declares,
// never written once NewNames builds it. A copy shares the set, which only
// resolve can reach, so no reader writes through one, and a Names is safe for
// concurrent use. The zero Names holds no name.
type Names struct {
	set map[string]bool
}

// NewNames returns the set holding each of names.
func NewNames(names ...string) Names {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return Names{set: set}
}

// Has reports whether name is one of n's.
func (n Names) Has(name string) bool { return n.set[name] }

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
