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

// NoString reports whether v, its alias followed, is a node but no string: a
// null, a mapping or a list. A Reference Object whose $ref is so written names
// nothing (docs/compilers/openapi.md#references).
func NoString(v *yaml.Node) bool {
	v = Deref(v)
	return v != nil && (v.Kind != yaml.ScalarNode || v.ShortTag() == "!!null")
}
