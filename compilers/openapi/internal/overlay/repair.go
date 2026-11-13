// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"math"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// repairAliases replaces each alias the actions left naming a node outside the
// tree, as a remove of its anchor leaves it, with a copy of the value it
// stands for, since readers here walk Content and read an alias as a leaf
// while the parser follows it (GitHub #477). It runs once g removed a node,
// the one way g detaches one, and is whole or refused: done is false once ctx
// is done, and safe false for copies past copies nodes, or past the room the
// node budget, nodes, leaves the tree, which budgeted then reports.
func repairAliases(ctx context.Context, root *yaml.Node, g *grafter, copies, nodes budget.Limit) (done, safe, budgeted bool) {
	content := nodeview.DocumentRoot(root)
	if !g.removed || content == nil {
		return true, true, false
	}
	reachable, walked := reachableNodes(ctx, content)
	if !walked {
		return false, true, false
	}
	limit := copies
	if most, bounded := nodes.Max(); bounded {
		// Each alias a copy replaces leaves the tree, so its node is room too.
		if room := max(most-len(reachable)+detachedAliases(reachable), 0); room < copies.Cap() {
			limit, budgeted = budget.AtMost(room), true
		}
	}
	work := budget.New(limit)
	done, safe = substituteDetached(ctx, content, reachable, g.written, &work)
	return done, safe, budgeted && !safe
}

// detachedAliases counts the aliases among reachable naming a node outside it,
// each of which a repair replaces with a copy.
func detachedAliases(reachable map[*yaml.Node]bool) int {
	count := 0
	for n := range reachable {
		if n.Kind == yaml.AliasNode && !reachable[n.Alias] {
			count++
		}
	}
	return count
}

// reachableNodes collects every node a Content walk from root reaches, which
// is every node this compiler's other readings can see, and reports whether
// ctx let it finish. It reads the whole tree, whose size the node budget and
// the loader's refusals bound.
func reachableNodes(ctx context.Context, root *yaml.Node) (map[*yaml.Node]bool, bool) {
	reachable := make(map[*yaml.Node]bool)
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if reachable[n] {
			return nodeview.Prune
		}
		reachable[n] = true
		return nodeview.Descend
	}}.Run(ctx, root)
	return reachable, !out.Cut
}

// substituteDetached walks the tree and replaces each child that is an alias
// to a node outside reachable with a copy of what it names, reporting whether
// ctx let it finish and whether every copy was built within work, which each
// node copied spends.
//
// A node is replaced in its parent's Content, which is why the walk reads
// children rather than the node it is at: no action replaces the tree's own
// root, so no alias can sit there. A copy holds no alias and is not walked.
func substituteDetached(ctx context.Context, n *yaml.Node, reachable, written map[*yaml.Node]bool, work *budget.Budget,
) (done, safe bool) {
	safe = true
	copies := map[*yaml.Node]bool{}
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		if copies[n] {
			return nodeview.Prune
		}
		if safe = substituteChildren(n, reachable, written, work, copies); !safe {
			return nodeview.Stop
		}
		return nodeview.Descend
	}}.Run(ctx, n)
	return !out.Cut, safe
}

// substituteChildren replaces each child of n that is a detached alias (see
// substituteDetached), recording each copy made in copies, and reports whether
// work held for all of them.
//
// A copy of a node the overlay wrote is the overlay's too, and a copy of one
// the source declares is the source's, keeping the line and column the source
// writes it at: it stands at the alias's position for what the source wrote
// at the anchor.
func substituteChildren(n *yaml.Node, reachable, written map[*yaml.Node]bool, work *budget.Budget, copies map[*yaml.Node]bool) bool {
	for i, child := range n.Content {
		if child == nil || child.Kind != yaml.AliasNode || reachable[child.Alias] {
			continue
		}
		expanded, err := copyOf(child, nil, 0, func(from, _, built *yaml.Node) bool {
			if written[from] {
				written[built] = true
			} else {
				built.Line, built.Column = from.Line, from.Column
			}
			return work.Spend(1)
		})
		if err != nil {
			return false
		}
		n.Content[i], copies[expanded] = expanded, true
	}
	return true
}
