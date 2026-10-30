// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ynode

import (
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Scalar builds an untagged plain scalar holding v.
func Scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
}

// Map builds a mapping from pairs, given as alternating key and value nodes —
// the layout yaml.v3 gives a mapping's Content.
func Map(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: pairs}
}

// Seq builds a sequence holding items.
func Seq(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}

// Alias builds an alias resolved to target, as yaml.v3 resolves one to the node
// its anchor names.
func Alias(target *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.AliasNode, Alias: target}
}

// Merge builds the `<<` merge key a parse produces, tag included: the same
// scalar without its tag is an ordinary key.
func Merge() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: "!!merge"}
}

// MergeChain builds levels mappings, each merging the next through `<<` and an
// alias, over a leaf mapping of one pair. Expanding the whole chain
// re-materializes every pair beneath each level, which is the cost the
// merge-depth bound exists to cap.
func MergeChain(levels int) *yaml.Node {
	nodes := make([]*yaml.Node, levels+1)
	for i := range nodes {
		nodes[i] = &yaml.Node{Kind: yaml.MappingNode}
	}
	for i := range levels {
		nodes[i].Content = []*yaml.Node{Merge(), Alias(nodes[i+1])}
	}
	nodes[levels].Content = []*yaml.Node{Scalar("leaf"), Scalar("v")}
	return nodes[0]
}

// MergeChainSpec is MergeChain in source form: an OpenAPI document whose
// x-anchors nest levels `<<` merges, with one schema per level naming a
// different point on the chain, so a reader that re-expands the chain at every
// reference pays the depth once per schema.
func MergeChainSpec(levels int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  m0: &m0 {type: object}\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "  m%d: &m%d {<<: *m%d, p%d: %d}\n", i, i, i-1, i, i)
	}
	b.WriteString("components:\n  schemas:\n")
	for i := levels; i >= 0; i-- {
		fmt.Fprintf(&b, "    S%d: {properties: {x: *m%d}}\n", i, i)
	}
	return b.String()
}
