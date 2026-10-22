package load

import (
	"encoding/json/jsontext"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
)

// sourceDocument is the source as its references' resolution reads it: the
// path they are relative to, the bytes and the tree its model was built from,
// what the table found in it, and where a finding about a node of that tree is
// placed.
//
// A $ref from another document back into the source is answered from here,
// not from the file, so it reaches what the compile holds: the tree an overlay
// patched, and nodes a finding is reported for once (GitHub #759).
type sourceDocument struct {
	path   string
	data   []byte
	root   *yaml.Node
	judged judgment
	tree   sourceTree
	// escapes holds the $ref values rewritten in the source's tree and in the
	// trees of the documents it reads, or is nil for none (GitHub #911).
	escapes *escapes
}

// sourceTree places a finding about a node of the source's patched tree as the
// source's own findings are placed (see locator), and names the node's pointer
// there. Its zero value holds no node, so every finding is placed at the $ref
// that reached it, as one in a document no entry lists is.
type sourceTree struct {
	locate   scan.Locator
	pointers func(*yaml.Node) (jsontext.Pointer, bool)
}

// newSourceTree returns the places of the nodes of root, the patched tree,
// under the overlay attribution origin, indexing root once for both.
func newSourceTree(srcIndex int, origin overlay.Origin, root *yaml.Node) sourceTree {
	pointers := scan.Pointers(root)
	return sourceTree{locate: locator(srcIndex, origin, pointers), pointers: pointers}
}

// has reports whether the tree holds n.
func (t sourceTree) has(n *yaml.Node) bool {
	if t.pointers == nil {
		return false
	}
	_, held := t.pointers(n)
	return held
}

// newSourceDocument returns the source at path, built from data into root,
// which the table judged as judged, and whose findings tree places. esc holds
// the $refs rewritten in root.
func newSourceDocument(path string, data []byte, root *yaml.Node, judged judgment, tree sourceTree, esc *escapes,
) sourceDocument {
	return sourceDocument{path: path, data: data, root: root, judged: judged, tree: tree, escapes: esc}
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
	if clean == s.path || refscope.IsURL(s.path) {
		return []string{s.path}
	}
	return []string{s.path, clean}
}

// names reports whether the resolver reading the document name reads the
// source as held (see keys), as the lowering reads a document part naming it
// (refscope.SameDocument). A source named by a URL is held, and so named,
// under its own spelling alone; one keys holds under none is never named.
// The path is read lexically, as a URI reference's is, so one through a
// directory that does not exist, or through a link, names the source; Open
// reads the file it names (see fileNamed).
func (s sourceDocument) names(name string) bool {
	return len(s.keys()) > 0 && refscope.SameDocument(s.path, name)
}
