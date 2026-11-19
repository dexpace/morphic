// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"
)

// Pairs is MappingPairs read by a view of its own, for a caller reading one
// mapping once, a document node unwrapped first: the view a walk shares is
// what keeps a merge chain from going cubic when every level of it is read.
func Pairs(node *yaml.Node) []Pair {
	return New().MappingPairs(DocumentRoot(node))
}

// Child returns the value the parser reads under key in the mapping node stands
// for, its alias followed, or nil where it reads none: the value MappingPairs
// pairs key with, found without building the pairs
// (docs/parser-library.md#merge-keys, docs/parser-library.md#duplicate-key).
// node is dereferenced, a document node unwrapped first; a node that is no
// mapping holds nothing.
//
// It builds no view, and allocates nothing for a mapping that merges nothing,
// which is what lets every reader of a schema keyword ask it. Its work is the
// merge structure's, which the alias scans bound; the load phase reports a
// chain past MergeDepthLimit (reftable.ReferenceTargetTable.Truncated).
func Child(node *yaml.Node, key string) *yaml.Node {
	child, _ := childIn(Deref(DocumentRoot(node)), key, lastCopy, nil)
	return child
}

// childIn is Child for the mapping node, read at the copy rule names, which the
// mappings on path merge in, node's depth being len(path). Its own pair wins;
// else the first `<<` source holding key, in merge order, as flatten settles
// them. A source past MergeDepthLimit, or on path, which is a merge cycle,
// holds nothing, as for MappingPairs. found reports a pair whose value is nil.
func childIn(node *yaml.Node, key string, rule copyRule, path []*yaml.Node) (child *yaml.Node, found bool) {
	if node == nil || node.Kind != yaml.MappingNode || len(path) > MergeDepthLimit || slices.Contains(path, node) {
		return nil, false
	}
	merges := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		if IsMergeKey(node.Content[i]) {
			merges = true
			continue
		}
		if k, own := KeyOf(node.Content[i]); own && k == key && (rule == lastCopy || !found) {
			child, found = node.Content[i+1], true
		}
	}
	if found || !merges {
		return Deref(child), found
	}
	path = append(path, node)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !IsMergeKey(node.Content[i]) {
			continue
		}
		if child, found = childOfSource(Deref(node.Content[i+1]), key, path); found {
			return child, true
		}
	}
	return nil, false
}

// childOfSource is childIn over one `<<` value: a mapping, or a sequence of
// them in order, each read as a merge source reads, at its first copy.
func childOfSource(val *yaml.Node, key string, path []*yaml.Node) (*yaml.Node, bool) {
	if val == nil || val.Kind != yaml.SequenceNode {
		return childIn(val, key, firstCopy, path)
	}
	for _, item := range val.Content {
		if node, found := childIn(Deref(item), key, firstCopy, path); found {
			return node, true
		}
	}
	return nil, false
}

// Strings returns the text of each item of the sequence node, and whether node is a
// list of strings as specrules.Checker reads one: scalars, none null, aliases followed.
// A scalar's tag does not matter, as it does to the model
// (docs/parser-library.md#undecoded-scalar).
func Strings(node *yaml.Node) ([]string, bool) {
	node = Deref(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		item = Deref(item)
		if item == nil || item.Kind != yaml.ScalarNode || item.Tag == "!!null" {
			return nil, false
		}
		out = append(out, item.Value)
	}
	return out, true
}

// WrittenChildren maps each key the mapping node writes itself to the value its
// text holds there: no `<<` merge read, an alias left an alias, each key as
// KeyOf names it and at its last copy. It answers where a document's text puts
// a node, which a read through merges and aliases (Child) cannot: a node
// merged or aliased into a mapping is written somewhere else. A node that is
// no mapping, an alias to one included, writes nothing.
func WrittenChildren(node *yaml.Node) map[string]*yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key, own := KeyOf(node.Content[i]); own {
			out[key] = node.Content[i+1]
		}
	}
	return out
}
