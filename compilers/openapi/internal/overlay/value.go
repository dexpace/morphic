// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// errPastNodeBudget reports a copy the node budget has no room for, which the
// sequence refuses the action for.
var errPastNodeBudget = errors.New("it would copy past the node budget")

// position is where a target lies in the tree: the node holding it, the
// index in its holder's Content of its slot, and the node there, an alias
// followed. The root has no holder.
type position struct {
	holder *yaml.Node
	// raw is the node the slot held when the position was taken.
	raw  *yaml.Node
	node *yaml.Node
	slot int
}

// ownSlot makes the value at holder.Content[slot] the action's own to write
// and returns it: as it is, or, where an alias stands there or an alias names
// the node, a copy of it in its place. The copy keeps the aliases it holds as
// aliases, and drops its anchors, since no alias names the copy.
func (g *grafter) ownSlot(ctx context.Context, holder *yaml.Node, slot int) (*yaml.Node, error) {
	raw := holder.Content[slot]
	node := nodeview.Deref(raw)
	if node == nil {
		return nil, fmt.Errorf("it reaches the YAML alias at %s, which names nothing", where(raw))
	}
	if raw.Kind != yaml.AliasNode && g.named[raw] == 0 {
		return raw, nil
	}
	copied, err := g.copyContent(ctx, node, raw.Kind == yaml.AliasNode)
	if err != nil {
		return nil, err
	}
	holder.Content[slot] = copied
	g.detached = g.detached || raw.Kind != yaml.AliasNode
	return copied, nil
}

// flatten makes every member of the mapping node its own: its `<<` merge keys
// are replaced by a copy of each member they bring in that node does not write,
// in the order the value holds them, so a write to one replaces it and
// writes nothing the merged mapping holds. A merge reaching what the source
// refuses (refusedMerge) stays beside the copies. A node that is no mapping,
// or one merging nothing, is left as it is.
func (g *grafter) flatten(ctx context.Context, node *yaml.Node) error {
	if node.Kind != yaml.MappingNode || !mergesAny(node) {
		return nil
	}
	view := nodeview.New()
	pairs := view.MappingPairs(node)
	if view.Exhausted() {
		return fmt.Errorf("it writes into the mapping at %s, whose `<<` merges run deeper than %d levels",
			where(node), nodeview.MergeDepthLimit)
	}
	written, keys := map[string]bool{}, mergedKeys(node)
	var own, brought []*yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		if nodeview.IsMergeKey(node.Content[i]) && !refusedMerge(node.Content[i+1]) {
			continue
		}
		// A key that is no scalar names no member, and a merge reaching what
		// the source refuses brings in what the copies leave out, so each
		// stays for the reading that refuses it.
		own = append(own, node.Content[i], node.Content[i+1])
		if key, scalar := nodeview.KeyOf(node.Content[i]); scalar {
			written[key] = true
		}
	}
	for _, pair := range pairs {
		if !written[pair.Key] {
			brought = append(brought, keys[pair], pair.Val)
		}
	}
	size := 0
	for _, broughtNode := range brought {
		size += nodeview.Walk{Nodes: math.MaxInt}.Run(ctx, broughtNode).Visited
	}
	if ctx.Err() != nil || !g.admit(ctx, size) {
		return errPastNodeBudget
	}
	for i, broughtNode := range brought {
		brought[i] = g.copyTree(broughtNode, true)
	}
	node.Content = slices.Concat(brought, own)
	return nil
}

// refusedMerge reports whether the `<<` value val reaches what the source's
// format check refuses in a merge (specrules.CheckFormat), which no copy of a member
// it brings in carries: a merge naming no mapping, or a key that is no scalar
// in a mapping a merge brings in, at any depth. Each mapping is read once.
func refusedMerge(val *yaml.Node) bool {
	seen := map[*yaml.Node]bool{}
	for todo := []*yaml.Node{val}; len(todo) > 0; {
		node := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for _, src := range nodeview.MergeSources(node) {
			if nodeview.NotMapping(src) {
				return true
			}
			dereferenced := nodeview.Deref(src)
			if seen[dereferenced] {
				continue
			}
			seen[dereferenced] = true
			for i := 0; i+1 < len(dereferenced.Content); i += 2 {
				if nodeview.IsMergeKey(dereferenced.Content[i]) {
					todo = append(todo, dereferenced.Content[i+1])
				} else if _, scalar := nodeview.KeyOf(dereferenced.Content[i]); !scalar {
					return true
				}
			}
		}
	}
	return false
}

// mergesAny reports whether the mapping node writes a `<<` merge key.
func mergesAny(node *yaml.Node) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if nodeview.IsMergeKey(node.Content[i]) {
			return true
		}
	}
	return false
}

// mergedKeys returns the key node each member a `<<` of node brings in is
// written under, by its key and value, read through every mapping its merges
// reach, each once.
func mergedKeys(node *yaml.Node) map[nodeview.Pair]*yaml.Node {
	keys := map[nodeview.Pair]*yaml.Node{}
	seen := map[*yaml.Node]bool{node: true}
	for todo := []*yaml.Node{node}; len(todo) > 0; {
		current := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for i := 0; i+1 < len(current.Content); i += 2 {
			if !nodeview.IsMergeKey(current.Content[i]) {
				key, _ := nodeview.KeyOf(current.Content[i])
				if pair := (nodeview.Pair{Key: key, Val: nodeview.Deref(current.Content[i+1])}); keys[pair] == nil {
					keys[pair] = current.Content[i]
				}
				continue
			}
			for _, src := range nodeview.MergeSources(current.Content[i+1]) {
				if otherNode := nodeview.Deref(src); otherNode != nil && otherNode.Kind == yaml.MappingNode && !seen[otherNode] {
					seen[otherNode] = true
					todo = append(todo, otherNode)
				}
			}
		}
	}
	return keys
}

// copyContent returns a copy of the tree under node, built after its nodes are
// charged to the node budget: each node the tree holds, an alias kept as an
// alias, so the copy is the size of what the source wrote and not of the
// value it stands for. It drops anchors, keeps the line and column the source
// writes each node at, and records as written what the overlay wrote. A copy
// moved to another position than node's, an alias's or a merge's, is recorded as
// one, since the source does not write it where it stands.
func (g *grafter) copyContent(ctx context.Context, node *yaml.Node, moved bool) (*yaml.Node, error) {
	size := nodeview.Walk{Nodes: math.MaxInt}.Run(ctx, node)
	if size.Cut || !g.admit(ctx, size.Visited) {
		return nil, errPastNodeBudget
	}
	return g.copyTree(node, moved), nil
}

// copyTree builds the copy copyContent charged for, with a stack of its own.
func (g *grafter) copyTree(node *yaml.Node, moved bool) *yaml.Node {
	type pair struct{ from, to *yaml.Node }
	root := g.copyNode(node, moved)
	for stack := []pair{{node, root}}; len(stack) > 0; {
		pairValue := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if len(pairValue.from.Content) == 0 {
			continue
		}
		pairValue.to.Content = make([]*yaml.Node, len(pairValue.from.Content))
		for i, child := range pairValue.from.Content {
			pairValue.to.Content[i] = g.copyNode(child, moved)
			stack = append(stack, pair{child, pairValue.to.Content[i]})
		}
	}
	return root
}

// copyNode returns a copy of node alone, its anchor dropped, moved where it does
// not stand at node's position; a copied alias is one more alias naming what node
// names.
func (g *grafter) copyNode(node *yaml.Node, moved bool) *yaml.Node {
	copied := &yaml.Node{Kind: node.Kind, Style: node.Style, Tag: node.Tag, Value: node.Value, Alias: node.Alias,
		Line: node.Line, Column: node.Column}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		g.named[node.Alias]++
	}
	if g.written[node] {
		g.written[copied] = true
	}
	g.copies[copied] = moved
	return copied
}

// admit charges n nodes to the node budget and reports whether they fit. The
// running count only ever overstates the tree, so the tree is counted afresh
// before a charge is refused: at the budget's edge, before each action that
// charges a node, a walk of the whole tree that no budget counts (GitHub #1959).
func (g *grafter) admit(ctx context.Context, n int) bool {
	if g.size == nil || g.size.Spend(n) {
		return true
	}
	g.measure(ctx)
	return g.size.Spend(n)
}

// measure takes the tree's node count afresh as the running size.
func (g *grafter) measure(ctx context.Context) {
	*g.size = budget.New(g.size.Limit())
	g.size.Spend(countNodes(ctx, g.root, g.size.Limit()))
}

// aliasCounts counts the aliases naming each node of the tree under root, and
// reports whether ctx let the walk finish.
func aliasCounts(ctx context.Context, root *yaml.Node) (map[*yaml.Node]int, bool) {
	named := map[*yaml.Node]int{}
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(node *yaml.Node, _ int) nodeview.Step {
		if node.Kind == yaml.AliasNode && node.Alias != nil {
			named[node.Alias]++
		}
		return nodeview.Descend
	}}.Run(ctx, nodeview.DocumentRoot(root))
	return named, !out.Cut
}
