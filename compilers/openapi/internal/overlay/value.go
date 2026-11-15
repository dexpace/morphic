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
	n := nodeview.Deref(raw)
	if n == nil {
		return nil, fmt.Errorf("it reaches the YAML alias at %s, which names nothing", where(raw))
	}
	if raw.Kind != yaml.AliasNode && g.named[raw] == 0 {
		return raw, nil
	}
	c, err := g.copyContent(ctx, n, raw.Kind == yaml.AliasNode)
	if err != nil {
		return nil, err
	}
	holder.Content[slot] = c
	g.detached = g.detached || raw.Kind != yaml.AliasNode
	return c, nil
}

// flatten makes every member of the mapping m its own: its `<<` merge keys
// are replaced by a copy of each member they bring in that m does not write,
// in the order the value holds them, so a write to one replaces it and
// writes nothing the merged mapping holds. A node that is no mapping, or one
// merging nothing, is left as it is.
func (g *grafter) flatten(ctx context.Context, m *yaml.Node) error {
	if m.Kind != yaml.MappingNode || !mergesAny(m) {
		return nil
	}
	view := nodeview.New()
	pairs := view.MappingPairs(m)
	if view.Exhausted() {
		return fmt.Errorf("it writes into the mapping at %s, whose `<<` merges run deeper than %d levels",
			where(m), nodeview.MergeDepthLimit)
	}
	written, keys := map[string]bool{}, mergedKeys(m)
	var own, brought []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if nodeview.IsMergeKey(m.Content[i]) {
			continue
		}
		// A key that is no scalar names no member, and stays for the
		// reading that refuses it.
		own = append(own, m.Content[i], m.Content[i+1])
		if key, scalar := nodeview.KeyOf(m.Content[i]); scalar {
			written[key] = true
		}
	}
	for _, p := range pairs {
		if !written[p.Key] {
			brought = append(brought, keys[p], p.Val)
		}
	}
	size := 0
	for _, n := range brought {
		size += nodeview.Walk{Nodes: math.MaxInt}.Run(ctx, n).Visited
	}
	if ctx.Err() != nil || !g.admit(ctx, size) {
		return errPastNodeBudget
	}
	for i, n := range brought {
		brought[i] = g.copyTree(n, true)
	}
	m.Content = slices.Concat(brought, own)
	return nil
}

// mergesAny reports whether the mapping m writes a `<<` merge key.
func mergesAny(m *yaml.Node) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if nodeview.IsMergeKey(m.Content[i]) {
			return true
		}
	}
	return false
}

// mergedKeys returns the key node each member a `<<` of m brings in is
// written under, by its key and value, read through every mapping its merges
// reach, each once.
func mergedKeys(m *yaml.Node) map[nodeview.Pair]*yaml.Node {
	keys := map[nodeview.Pair]*yaml.Node{}
	seen := map[*yaml.Node]bool{m: true}
	for todo := []*yaml.Node{m}; len(todo) > 0; {
		n := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for i := 0; i+1 < len(n.Content); i += 2 {
			if !nodeview.IsMergeKey(n.Content[i]) {
				key, _ := nodeview.KeyOf(n.Content[i])
				if p := (nodeview.Pair{Key: key, Val: nodeview.Deref(n.Content[i+1])}); keys[p] == nil {
					keys[p] = n.Content[i]
				}
				continue
			}
			for _, src := range nodeview.MergeSources(n.Content[i+1]) {
				if s := nodeview.Deref(src); s != nil && s.Kind == yaml.MappingNode && !seen[s] {
					seen[s] = true
					todo = append(todo, s)
				}
			}
		}
	}
	return keys
}

// copyContent returns a copy of the tree under n, built after its nodes are
// charged to the node budget: each node the tree holds, an alias kept as an
// alias, so the copy is the size of what the source wrote and not of the
// value it stands for. It drops anchors, keeps the line and column the source
// writes each node at, and records as written what the overlay wrote. A copy
// moved to another position than n's, an alias's or a merge's, is recorded as
// one, since the source does not write it where it stands.
func (g *grafter) copyContent(ctx context.Context, n *yaml.Node, moved bool) (*yaml.Node, error) {
	size := nodeview.Walk{Nodes: math.MaxInt}.Run(ctx, n)
	if size.Cut || !g.admit(ctx, size.Visited) {
		return nil, errPastNodeBudget
	}
	return g.copyTree(n, moved), nil
}

// copyTree builds the copy copyContent charged for, with a stack of its own.
func (g *grafter) copyTree(n *yaml.Node, moved bool) *yaml.Node {
	type pair struct{ from, to *yaml.Node }
	root := g.copyNode(n, moved)
	for stack := []pair{{n, root}}; len(stack) > 0; {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if len(p.from.Content) == 0 {
			continue
		}
		p.to.Content = make([]*yaml.Node, len(p.from.Content))
		for i, c := range p.from.Content {
			p.to.Content[i] = g.copyNode(c, moved)
			stack = append(stack, pair{c, p.to.Content[i]})
		}
	}
	return root
}

// copyNode returns a copy of n alone, its anchor dropped, moved where it does
// not stand at n's position; a copied alias is one more alias naming what n
// names.
func (g *grafter) copyNode(n *yaml.Node, moved bool) *yaml.Node {
	c := &yaml.Node{Kind: n.Kind, Style: n.Style, Tag: n.Tag, Value: n.Value, Alias: n.Alias,
		Line: n.Line, Column: n.Column}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		g.named[n.Alias]++
	}
	if g.written[n] {
		g.written[c] = true
	}
	g.copies[c] = moved
	return c
}

// admit charges n nodes to the node budget and reports whether they fit. The
// running count only ever overstates the tree, so the tree is counted afresh
// before a charge is refused.
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
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if n.Kind == yaml.AliasNode && n.Alias != nil {
			named[n.Alias]++
		}
		return nodeview.Descend
	}}.Run(ctx, nodeview.DocumentRoot(root))
	return named, !out.Cut
}
