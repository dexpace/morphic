package nodeview

import (
	"slices"

	yaml "gopkg.in/yaml.v3"
)

// Pairs is MappingPairs read by a view of its own, for a caller reading one
// mapping once, a document node unwrapped first: the view a walk shares is
// what keeps a merge chain from going cubic when every level of it is read.
func Pairs(n *yaml.Node) []Pair {
	return New().MappingPairs(DocumentRoot(n))
}

// Child returns the value the parser reads under key in the mapping n stands
// for, its alias followed, or nil where it reads none: the value MappingPairs
// pairs key with, found without building the pairs. n is dereferenced, a
// document node unwrapped first; a node that is no mapping holds nothing.
//
// It builds no view, and allocates nothing for a mapping that merges nothing,
// which is what lets every reader of a schema keyword ask it. Its work is the
// merge structure's, which the alias scans bound; the load phase reports a
// chain past MergeDepthLimit (bind.Table.Truncated).
func Child(n *yaml.Node, key string) *yaml.Node {
	v, _ := childIn(Deref(DocumentRoot(n)), key, lastCopy, nil)
	return v
}

// childIn is Child for the mapping n, read at the copy rule names, which the
// mappings on path merge in, n's depth being len(path). Its own pair wins;
// else the first `<<` source holding key, in merge order, as flatten settles
// them. A source past MergeDepthLimit, or on path, which is a merge cycle,
// holds nothing, as for MappingPairs. found reports a pair whose value is nil.
func childIn(n *yaml.Node, key string, rule copyRule, path []*yaml.Node) (v *yaml.Node, found bool) {
	if n == nil || n.Kind != yaml.MappingNode || len(path) > MergeDepthLimit || slices.Contains(path, n) {
		return nil, false
	}
	merges := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		if IsMergeKey(n.Content[i]) {
			merges = true
			continue
		}
		if k, own := KeyOf(n.Content[i]); own && k == key && (rule == lastCopy || !found) {
			v, found = n.Content[i+1], true
		}
	}
	if found || !merges {
		return Deref(v), found
	}
	path = append(path, n)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if !IsMergeKey(n.Content[i]) {
			continue
		}
		if v, found = childOfSource(Deref(n.Content[i+1]), key, path); found {
			return v, true
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
		if v, found := childIn(Deref(item), key, firstCopy, path); found {
			return v, true
		}
	}
	return nil, false
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
