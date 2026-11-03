// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"context"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// expansion counts what a run's $refs copy against Limits.MaxReferenceExpansion.
// A declaration a $ref names, a schema or a link aside, lowers again at every
// $ref naming it (GitHub #1240), so each such $ref is charged what it weighs,
// and the sum bounds the product of references and size that the source budgets
// leave free. A declaration is weighed once and its weight remembered: one
// weighed past what was left is remembered past it, which nothing reads again,
// since the budget is then crossed.
type expansion struct {
	spent   budget.Budget
	weights map[*yaml.Node]int
}

// newExpansion returns an expansion charging against l.
func newExpansion(l budget.Limit) *expansion {
	return &expansion{spent: budget.New(l), weights: map[*yaml.Node]int{}}
}

// admit charges one copy of the declaration whose node is root, and reports
// whether the copies so far are within the budget. Once they are not it admits
// nothing, so no later $ref lowers. A nil expansion, a zero context's, and an
// unbounded one admit every copy without weighing it.
func (e *expansion) admit(root *yaml.Node) bool {
	if e == nil {
		return true
	}
	if _, bounded := e.spent.Limit().Max(); !bounded {
		return true
	}
	if e.spent.Exhausted() {
		return false
	}
	root = nodeview.Deref(root)
	weight, known := e.weights[root]
	if !known {
		weight = weigh(root, e.spent.Remaining())
		e.weights[root] = weight
	}
	return e.spent.Spend(weight)
}

// refusal returns the error refusing a document whose $refs copied past the
// budget, at source srcIndex, and false while they have not. It names no
// position: which $ref crossed the budget follows the walk's order.
func (e *expansion) refusal(srcIndex int) (ir.Diagnostic, bool) {
	if e == nil || !e.spent.Exhausted() {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, ir.Provenance{Source: srcIndex},
		"the copies its $refs make weigh more than the %d budgeted for them: each $ref to a "+
			"declaration other than a schema or a link lowers its own copy of it", e.spent.Limit().Cap()), true
}

// weigh returns what a copy of the declaration under root weighs: one for each
// node, an alias counting as a copy of what it names, as the model is built
// (docs/parser-library.md#alias-expansion), and one for each byte of a scalar's
// text. It visits no more than one node past within, and a declaration of more
// nodes than that weighs past within anyway, so a weight past within is all
// weigh says of it.
//
// No alias leads back up: an anchor naming its own ancestor was refused before
// lowering. It takes no context, as nothing below a path item does.
func weigh(root *yaml.Node, within budget.Limit) int {
	weight := 0
	nodeview.Walk{Nodes: within.Past(), Aliases: nodeview.AliasTarget, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		weight++
		if n.Kind == yaml.ScalarNode {
			weight += len(n.Value)
		}
		return nodeview.Descend
	}}.Run(context.Background(), root)
	return weight
}
