package load

import (
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// sourceDocument is the source as its references' resolution reads it: the
// path they are relative to, the bytes and the tree its model was built from,
// and the findings its own validation made.
//
// A $ref from another document back into the source is answered from here,
// not from the file, so it reaches what the compile holds: the tree an overlay
// patched, and nodes a finding is reported for once (GitHub #759).
type sourceDocument struct {
	path  string
	data  []byte
	root  *yaml.Node
	found map[findingKey]bool
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
	return sourceDocument{path: path, data: data, root: root, found: found}
}

// keys returns the keys the resolver looks the source up by: its path, as an
// internal $ref resolves against it, and its path cleaned, as a relative $ref
// from another document reaches it. A source with no path or no tree is held
// under none, and one named by a URL is kept as spelled.
func (s sourceDocument) keys() []string {
	if s.path == "" || s.root == nil {
		return nil
	}
	clean := filepath.Clean(s.path)
	if clean == s.path || isURL(s.path) {
		return []string{s.path}
	}
	return []string{s.path, clean}
}

// names reports whether the resolver opening the file name opens the source:
// both name one path once made absolute. A source keys holds under none, or
// named by a URL, is no file.
func (s sourceDocument) names(name string) bool {
	if len(s.keys()) == 0 || isURL(s.path) {
		return false
	}
	self, errSelf := filepath.Abs(s.path)
	other, errOther := filepath.Abs(name)
	return errSelf == nil && errOther == nil && self == other
}

// isURL reports whether path names a document by URL rather than as a file.
func isURL(path string) bool {
	return strings.Contains(path, "://")
}
