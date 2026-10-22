package resolve

import (
	"encoding/json/jsontext"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

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

// InternalPointer returns the same-document JSON pointer a $ref target
// addresses, and ok=false for a cross-document reference, a bare schema name or
// a malformed ref. A document part naming this source file is internal (see
// namesSelf).
//
// It decodes the fragment (fragment.Pointer), since fragments are
// percent-encoded: comparing them raw missed resolvable references and interned
// a second node at a named position (GitHub #40).
//
// A `#addr` fragment names a $anchor, not a position, and an ID derived from it
// would be a path no coordinate spells (GitHub #141).
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
