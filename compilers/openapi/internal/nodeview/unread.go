// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview

import yaml "go.yaml.in/yaml/v3"

// Unread turns node into the null every reader takes for a field left out,
// keeping what the source wrote there for Written. The null keeps node's place in
// the tree, its position, anchor and comments, so an alias of node reads the null
// too. The decoder and the encoder read a scalar's Alias field nowhere
// (docs/parser-library.md#alias-field-on-scalar), and no walk here follows it
// but on an alias. A node already turned is left as it is.
func Unread(node *yaml.Node) {
	if node == nil || Written(node) != node {
		return
	}
	was := *node
	*node = yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!null", Value: "null", Anchor: node.Anchor,
		HeadComment: node.HeadComment, LineComment: node.LineComment, FootComment: node.FootComment,
		Line: node.Line, Column: node.Column, Alias: &was,
	}
}

// Written returns what the source wrote at node: the node Unread replaced, kept
// in the null's Alias field (docs/parser-library.md#alias-field-on-scalar), or
// node itself, nil for nil. Only a renderer of what the source wrote, which a
// verbatim copy shows, asks it.
func Written(node *yaml.Node) *yaml.Node {
	if node != nil && node.Kind == yaml.ScalarNode && node.Alias != nil {
		return node.Alias
	}
	return node
}

// NoString reports whether node, its alias followed, is a node but no string: a
// null, a mapping or a list. A Reference Object whose $ref is so written names
// nothing (docs/compilers/openapi.md#references).
func NoString(node *yaml.Node) bool {
	node = Deref(node)
	return node != nil && (node.Kind != yaml.ScalarNode || node.ShortTag() == "!!null")
}
