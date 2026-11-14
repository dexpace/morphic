// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview

import yaml "go.yaml.in/yaml/v3"

// Unread turns n into the null every reader takes for a field left out,
// keeping what the source wrote there for Written. The null keeps n's place in
// the tree, its position, anchor and comments, so an alias of n reads the null
// too. The decoder and the encoder read a scalar's Alias field nowhere
// (docs/parser-library.md#alias-field-on-scalar), and no walk here follows it
// but on an alias. A node already turned is left as it is.
func Unread(n *yaml.Node) {
	if n == nil || Written(n) != n {
		return
	}
	was := *n
	*n = yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!null", Value: "null", Anchor: n.Anchor,
		HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment,
		Line: n.Line, Column: n.Column, Alias: &was,
	}
}

// Written returns what the source wrote at n: the node Unread replaced, kept
// in the null's Alias field (docs/parser-library.md#alias-field-on-scalar), or
// n itself, nil for nil. Only a renderer of what the source wrote, which a
// verbatim copy shows, asks it.
func Written(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.ScalarNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

// NamesNothing reports whether n, its alias followed, is a mapping writing a
// `$ref` whose value is no string: read as a Reference Object, it names
// nothing, and the entry it stands for is read as left out, its other keys
// with it (docs/compilers/openapi.md#references). A Path Item's own `$ref` is
// the item's field, which a reader of one does not ask this of.
func NamesNothing(n *yaml.Node) bool {
	n = Deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if key, own := KeyOf(n.Content[i]); own && key == "$ref" {
			return Deref(n.Content[i+1]) == nil || NoString(n.Content[i+1])
		}
	}
	return false
}

// NoString reports whether v, its alias followed, is a node but no string: a
// null, a mapping or a list. A $ref so written names nothing (NamesNothing).
func NoString(v *yaml.Node) bool {
	v = Deref(v)
	return v != nil && (v.Kind != yaml.ScalarNode || v.ShortTag() == "!!null")
}
