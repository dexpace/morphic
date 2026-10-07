// Package componentreach partitions a document's component entries by what its
// `$ref` strings reach.
//
// A compiler that lowers a component only where a reference finds it lets
// every unnamed entry vanish from the IR in silence. Which entries those are is
// a question about the source alone, so it is asked here over the raw tree
// (GitHub #616).
//
// An entry is *referenced* when the transitive closure of `$ref` strings rooted
// outside the retained sections reaches its pointer. A `$ref` inside an
// unreferenced entry is not a root, so the component it names is kept too.
package componentreach

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// maxNodes bounds how many positions one walk visits, and with them how many
// times an alias may be expanded. The loader refuses a source that amplifies
// past its own budget before a document is lowered, and clears its anchors, so a
// real document cannot reach this; the bound is for a tree assembled by hand.
//
// Reaching it costs nothing: a walk that stopped early has collected fewer
// references, so more entries come back unreferenced — the direction that keeps
// a component rather than dropping it.
const maxNodes = 1 << 20

// maxAliasDepth bounds deref. A chain that long is not a document; the loader's
// amplification check refuses it long before this runs.
const maxAliasDepth = 32

// Entry is one retained components entry that no reference reaches.
type Entry struct {
	// Pointer is the entry's own source pointer (/components/<section>/<name>).
	Pointer jsontext.Pointer
	// Node is the entry's raw node, for the caller to keep verbatim.
	Node *yaml.Node
}

// Unreferenced returns every entry of the named component sections that no
// `$ref` reachable from outside them names, in the order the document declares
// them.
//
// sections is the set of sections the caller retains verbatim. Every other
// section — components/schemas and components/securitySchemes in particular — is
// a root rather than a subject: a reference written inside one of those counts,
// because a document's own schemas are all reachable.
func Unreferenced(root *yaml.Node, sections []string) []Entry {
	if root == nil || len(sections) == 0 {
		return nil
	}
	retained := make(map[string]bool, len(sections))
	for _, section := range sections {
		retained[section] = true
	}
	declared, rootRefs := walk(root, retained)
	return unreferenced(declared, reach(rootRefs, declared))
}

// declaredEntry is one entry of a retained section, as the document declares it.
type declaredEntry struct {
	pointer jsontext.Pointer
	node    *yaml.Node
}

// frame is one position on walk's stack: the node there and the pointer that
// locates it.
type frame struct {
	node *yaml.Node
	ptr  jsontext.Pointer
}

// walk returns every retained section's declared entries, in the document's
// order, together with the `$ref` targets written outside every one of them.
//
// A retained section is recorded rather than descended into: an entry is reached
// by a reference or not at all, which is what the rule says, so the walk reads
// the section's names and leaves each subtree to reach. Every other position,
// including the sections that lower unconditionally, is walked as a root.
func walk(root *yaml.Node, retained map[string]bool) ([]declaredEntry, []jsontext.Pointer) {
	var declared []declaredEntry
	var refs []jsontext.Pointer
	budget := maxNodes
	stack := []frame{{node: root}}
	for len(stack) > 0 && budget > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		budget--
		node := deref(f.node)
		if node == nil {
			continue
		}
		if _, ok := retainedSection(f.ptr, retained); ok {
			declared = append(declared, sectionEntries(node, f.ptr)...)
			continue
		}
		nested, found := children(node, f.ptr)
		refs = append(refs, found...)
		stack = append(stack, nested...)
	}
	return declared, refs
}

// children returns the positions directly beneath one node, pushed in reverse
// document order so the stack pops them in document order — the order the
// declared entries come back in — together with the `$ref` targets the node
// writes. A `$ref` member is a reference rather than a position: its target is
// returned and its value is not descended into.
func children(node *yaml.Node, ptr jsontext.Pointer) ([]frame, []jsontext.Pointer) {
	switch node.Kind {
	case yaml.MappingNode:
		nested := make([]frame, 0, len(node.Content)/2)
		var refs []jsontext.Pointer
		for i := len(node.Content) - 2; i >= 0; i -= 2 {
			key, val := node.Content[i], node.Content[i+1]
			if key.Value == "$ref" {
				if target, ok := internalTarget(deref(val)); ok {
					refs = append(refs, target)
				}
				continue
			}
			nested = append(nested, frame{node: val, ptr: ptr.AppendToken(key.Value)})
		}
		return nested, refs
	case yaml.SequenceNode:
		nested := make([]frame, 0, len(node.Content))
		for i := len(node.Content) - 1; i >= 0; i-- {
			nested = append(nested, frame{node: node.Content[i], ptr: ptr.AppendToken(strconv.Itoa(i))})
		}
		return nested, nil
	}
	return nil, nil
}

// sectionEntries returns the direct entries of one retained section, in the
// order the document declares them.
func sectionEntries(section *yaml.Node, sectionPtr jsontext.Pointer) []declaredEntry {
	out := make([]declaredEntry, 0, len(section.Content)/2)
	for i := 0; i+1 < len(section.Content); i += 2 {
		out = append(out, declaredEntry{
			pointer: sectionPtr.AppendToken(section.Content[i].Value),
			node:    section.Content[i+1],
		})
	}
	return out
}

// reach returns the entry pointers the document reaches: those the root
// references name, and then the ones those entries' own references name, until
// nothing new is reached. Each entry is expanded once, so a reference cycle
// between two entries terminates and the walk is bounded by the document's
// references.
func reach(rootRefs []jsontext.Pointer, declared []declaredEntry) map[jsontext.Pointer]bool {
	byPointer := make(map[jsontext.Pointer]*yaml.Node, len(declared))
	for _, entry := range declared {
		byPointer[entry.pointer] = entry.node
	}
	out := map[jsontext.Pointer]bool{}
	frontier := rootRefs
	budget := maxNodes
	for len(frontier) > 0 && budget > 0 {
		budget--
		target := frontier[0]
		frontier = frontier[1:]
		if out[target] {
			continue
		}
		entry, ok := byPointer[target]
		if !ok {
			continue // a reference into a section that lowers unconditionally, or nowhere
		}
		out[target] = true
		frontier = append(frontier, subtreeRefs(entry)...)
	}
	return out
}

// subtreeRefs returns every `$ref` target written in one entry's subtree. It
// carries no pointer, because a reference here only has to be found rather than
// located: the entry it sits in is already known.
func subtreeRefs(node *yaml.Node) []jsontext.Pointer {
	var out []jsontext.Pointer
	budget := maxNodes
	stack := []*yaml.Node{node}
	for len(stack) > 0 && budget > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		budget--
		cur = deref(cur)
		if cur == nil {
			continue
		}
		switch cur.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(cur.Content); i += 2 {
				key, val := cur.Content[i], cur.Content[i+1]
				if key.Value == "$ref" {
					if target, ok := internalTarget(deref(val)); ok {
						out = append(out, target)
					}
					continue
				}
				stack = append(stack, val)
			}
		case yaml.SequenceNode:
			stack = append(stack, cur.Content...)
		}
	}
	return out
}

// unreferenced returns the entries no reference reached, in declaration order.
func unreferenced(declared []declaredEntry, reachable map[jsontext.Pointer]bool) []Entry {
	var out []Entry
	for _, entry := range declared {
		if reachable[entry.pointer] {
			continue
		}
		out = append(out, Entry{Pointer: entry.pointer, Node: entry.node})
	}
	return out
}

// retainedSection reports whether ptr is a retained section's own mapping node,
// and names the section. The components object sits one token beneath the root,
// so a section is exactly /components/<name>.
func retainedSection(ptr jsontext.Pointer, retained map[string]bool) (string, bool) {
	if ptr.Parent() != jsontext.Pointer("/components") {
		return "", false
	}
	name := ptr.LastToken()
	return name, retained[name]
}

// internalTarget returns the pointer a `$ref` string names inside this document,
// and ok=false for one that leaves it: a reference with no fragment names the
// whole of another document, and nothing local is reached by it.
func internalTarget(ref *yaml.Node) (jsontext.Pointer, bool) {
	if ref == nil || ref.Kind != yaml.ScalarNode {
		return "", false
	}
	fragment, found := strings.CutPrefix(ref.Value, "#")
	if !found || !strings.HasPrefix(fragment, "/") {
		return "", false
	}
	return jsontext.Pointer(fragment), true
}

// deref follows a YAML alias to the node it names. An alias cycle is refused
// before lowering, and the depth bound is what keeps this walk terminating
// without relying on that.
func deref(node *yaml.Node) *yaml.Node {
	for range maxAliasDepth {
		if node == nil || node.Kind != yaml.AliasNode || node.Alias == nil {
			return node
		}
		node = node.Alias
	}
	return nil
}
