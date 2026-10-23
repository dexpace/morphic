package nodeview

import (
	yaml "gopkg.in/yaml.v3"
)

// Pairs is MappingPairs read by a view of its own, for a caller reading one
// mapping once, a document node unwrapped first: the view a walk shares is
// what keeps a merge chain from going cubic when every level of it is read.
func Pairs(n *yaml.Node) []Pair {
	return New().MappingPairs(DocumentRoot(n))
}

// Child returns the value the parser reads under key in the mapping n stands
// for, its alias followed, or nil where it reads none (MappingPairs). n is
// dereferenced, a document node unwrapped first; a node that is no mapping
// holds nothing.
//
// A mapping that merges nothing is answered by one pass over its pairs, with
// no view and no allocation, which is what lets every reader of a schema
// keyword ask it. One that merges is read through a view of its own, as far
// as MergeDepthLimit; the load phase reports a chain past it
// (bind.Table.Truncated).
func Child(n *yaml.Node, key string) *yaml.Node {
	n = Deref(DocumentRoot(n))
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		raw := n.Content[i]
		if IsMergeKey(raw) {
			return New().mappingChild(n, key)
		}
		if k, own := KeyOf(raw); own && k == key {
			found = n.Content[i+1]
		}
	}
	return Deref(found)
}

// WrittenChildren maps each key the mapping n writes itself to the value its
// text holds there: no `<<` merge read, an alias left an alias, each key as
// KeyOf names it and at its last copy. It answers where a document's text puts
// a node, which a read through merges and aliases (Child) cannot: a node
// merged or aliased into a mapping is written somewhere else. A node that is
// no mapping, an alias to one included, writes nothing.
func WrittenChildren(n *yaml.Node) map[string]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]*yaml.Node, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if key, own := KeyOf(n.Content[i]); own {
			out[key] = n.Content[i+1]
		}
	}
	return out
}
