package load

import (
	"path/filepath"

	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// sourceDocument is the source as its references' resolution reads it: the
// path they are relative to, the bytes and the tree its model was built from,
// and the findings its own validation made.
//
// A $ref from another document back into the source is answered from here,
// not from the file, so it reaches what the compile holds: the tree an overlay
// patched, and nodes a finding is reported for once (GitHub #759).
// One that spells $id is not held (see keys).
type sourceDocument struct {
	path     string
	data     []byte
	root     *yaml.Node
	found    map[findingKey]bool
	holdless bool
}

// newSourceDocument returns the source at path, built from data into root,
// whose own validation found valErrs. Only a finding at a node is kept: one at
// none is told apart by where it is placed, which a $ref reaching it changes.
func newSourceDocument(path string, data []byte, root *yaml.Node, valErrs []error) sourceDocument {
	found := make(map[findingKey]bool, len(valErrs))
	for _, ve := range valErrs {
		if verr, ok := asValidationError(ve); ok && verr.Node != nil {
			found[keyOf(ve, "")] = true
		}
	}
	return sourceDocument{path: path, data: data, root: root, found: found,
		holdless: anyScalar(root, isID) || spellsManyRefs(root)}
}

// maxHeldRefs is the most $ref scalars a source held under its file name may
// write. loops reads each hop of their chains once, and a pending one again per
// reference, so about three reads a $ref; past this the guard could run out of
// reads and answer nothing, and a held source lets a cycle through its file
// name run the stack out (GitHub #768). A larger source is not held, so a
// reference naming its file reads the file.
const maxHeldRefs = maxLoopReads / 4

// spellsManyRefs reports whether the tree under root writes more than
// maxHeldRefs scalars spelling $ref, as a key or otherwise: counted widely, a
// source is only held less.
func spellsManyRefs(root *yaml.Node) bool {
	n := 0
	return anyScalar(root, func(v string) bool {
		if v == "$ref" {
			n++
		}
		return n > maxHeldRefs
	})
}

// isID reports whether a scalar is the keyword that rebases the references
// under it. Any scalar of that text counts, written as a key, a value, or an
// anchor an alias supplies as a key: reading the tree as the parser does is
// not certain, and a match that is too wide only holds the source less.
func isID(v string) bool { return v == "$id" }

// keys returns the keys the resolver looks the source up by: its path, as an
// internal $ref resolves against it, and its path cleaned, as a relative $ref
// from another document reaches it. A source with no path or no tree is held
// under none, and one named by a URL is kept as spelled. So is one that spells
// $id, which rebases the references under it in a way loops does not read, or
// more $refs than its reads bound (maxHeldRefs): a chain through the source's
// file would go unchecked, so a reference naming that file reads it instead.
func (s sourceDocument) keys() []string {
	if s.holdless || s.path == "" || s.root == nil {
		return nil
	}
	clean := filepath.Clean(s.path)
	if clean == s.path || refscope.IsURL(s.path) {
		return []string{s.path}
	}
	return []string{s.path, clean}
}

// names reports whether the resolver reading the document name reads the
// source as held (see keys), as the lowering reads a document part naming it
// (refscope.SameDocument). A source named by a URL is held, and so named,
// under its own spelling alone; one keys holds under none is never named.
func (s sourceDocument) names(name string) bool {
	return len(s.keys()) > 0 && refscope.SameDocument(s.path, name)
}

// within is withinDocument for the source, where a reference naming it by its
// file name names a position in it too, as the lowering reads it
// (sourcePointer).
func (s sourceDocument) within(ref references.Reference) (string, bool) {
	pointer, ok := sourcePointer(s.path, string(ref))
	return string(pointer), ok
}
