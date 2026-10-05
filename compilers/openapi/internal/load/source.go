package load

import (
	"path/filepath"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
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
	return sourceDocument{path: path, data: data, root: root, found: found, holdless: spellsID(root)}
}

// spellsID reports whether a mapping in the tree under root has a $id key, the
// keyword that rebases the references under it. A key an alias or a merge key
// supplies is still written somewhere in the tree, so each node is read once
// and an alias, which holds no content, reads as none.
func spellsID(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < sourceindex.MaxIndexedNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if key := n.Content[i]; key.Kind == yaml.ScalarNode && key.Value == "$id" {
				return true
			}
		}
		stack = append(stack, n.Content...)
	}
	return false
}

// keys returns the keys the resolver looks the source up by: its path, as an
// internal $ref resolves against it, and its path cleaned, as a relative $ref
// from another document reaches it. A source with no path or no tree is held
// under none, and one named by a URL is kept as spelled. So is one that spells
// $id: it rebases the references under it, which loops does not read, so a
// chain through the source's file would go unchecked, and a reference naming
// that file reads it instead.
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

// names reports whether the resolver opening the file name opens the source,
// as the lowering reads a document part naming it (refscope.SameDocument). A
// source keys holds under none, or named by a URL, is no file.
func (s sourceDocument) names(name string) bool {
	return len(s.keys()) > 0 && !refscope.IsURL(s.path) && refscope.SameDocument(s.path, name)
}

// provablyEnds reports whether the chain of ref, a schema $ref resolved in the
// source whose model is model, ends. The resolver follows a hop it resolved
// before without tracking where the chain has been, so a chain closing on such
// hops recurses until the stack runs out (GitHub #558), and the cycle scan
// misses a cycle in raw YAML or through the source's file name (GitHub #768).
// A mapping names either wherever the input says, so the chain is read as the
// resolver reads it: in the model, and in the tree after a hop by file name.
func (s sourceDocument) provablyEnds(model *soa.OpenAPI, view *nodeview.View, ref references.Reference) bool {
	if s.root == nil || model == nil {
		return false
	}
	var document any = model
	return endsWithin(ref, func(ref references.Reference) (references.Reference, hopKind) {
		pointer, ok := s.within(ref)
		if !ok {
			return "", hopUnread
		}
		if ref.GetURI() != "" {
			document = s.root
		}
		return readHop(view, document, pointer)
	})
}

// within is withinDocument for the source, where a reference naming it by its
// file name names a position in it too, as the lowering reads it
// (sourcePointer).
func (s sourceDocument) within(ref references.Reference) (string, bool) {
	pointer, ok := sourcePointer(s.path, string(ref))
	return string(pointer), ok
}
