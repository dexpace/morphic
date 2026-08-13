// Package ynode spells yaml.v3 nodes: the tag a resolved `<<` merge key
// carries, the constructors for the node kinds a parse produces, and the merge
// chain the compiler's depth bounds are measured against.
//
// It sits below nodeview, which reads MergeTag back for its merge-key
// predicate, rather than beside it. Both the view and the cycle scan build
// these nodes from their own internal test files, and an internal test file
// cannot import a package that imports its own — so a home above nodeview would
// be one the view's tests could not reach. Holding the tag here is what lets a
// single definition of Merge serve both.
package ynode

import (
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// MergeTag is the tag yaml.v3 resolves every `<<` merge key to, and the exact
// tag speakeasy's yml.IsMergeKey requires before treating one as a merge.
const MergeTag = "!!merge"

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
// scalar without MergeTag is an ordinary key.
func Merge() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: MergeTag}
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
