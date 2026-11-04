// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// expansion counts what a run's $refs copy against
// Limits.MaxReferenceExpansion. A declaration a $ref names, a schema or a link
// aside, lowers again at every $ref naming it (GitHub #1240), so each such $ref
// is charged what the declarations its chain names weigh, and the sum bounds
// the product of references and size that the source budgets leave free, as
// it bounds what a path item copies onto its operations (Recopied). A
// declaration is weighed once for each reading and remembered: one weighed past
// what was left is remembered past it, which nothing reads again, since the
// budget is then crossed.
type expansion struct {
	spent   budget.Budget
	weights map[weighed]int
}

// weighed is a declaration as weightOf weighs it: its node, and how the
// grammar reads it, which decides what in it is a schema.
type weighed struct {
	node *yaml.Node
	at   bind.Reading
}

// newExpansion returns an expansion charging against l.
func newExpansion(l budget.Limit) *expansion {
	return &expansion{spent: budget.New(l), weights: map[weighed]int{}}
}

// admit charges one copy of the declarations whose nodes are roots, each read
// as at, and reports whether the copies so far are within the budget. Once they
// are not it admits nothing, so no later $ref lowers. A nil expansion, a zero
// context's, and an unbounded one admit every copy without weighing it.
func (e *expansion) admit(at bind.Reading, roots ...*yaml.Node) bool {
	if e == nil {
		return true
	}
	if _, bounded := e.spent.Limit().Max(); !bounded {
		return true
	}
	if e.spent.Exhausted() {
		return false
	}
	for _, root := range roots {
		key := weighed{node: nodeview.Deref(root), at: at}
		weight, known := e.weights[key]
		if !known {
			weight = weightOf(key.node, at, e.spent.Remaining())
			e.weights[key] = weight
		}
		if !e.spent.Spend(weight) {
			return false
		}
	}
	return true
}

// spend charges raw, what a copy holds as raw values, and reports whether the
// copies so far are within the budget. A nil expansion admits every copy.
func (e *expansion) spend(raw int) bool {
	return e == nil || e.spent.Spend(raw)
}

// RecopiedAt charges the expansion one more copy of the declaration at node,
// written at at in c's document, that a lowering makes with no $ref naming it:
// an inline parameter of a path item, at each of the item's operations past its
// first (GitHub #1428). It weighs the copy as Declared weighs a $ref's, and
// reports whether the copies so far are within the budget.
func RecopiedAt(c Ctx, at jsontext.Pointer, node *yaml.Node) bool {
	return c.expansion.admit(readingOf(c, at), node)
}

// Recopied charges the expansion raw more bytes that a lowering copies as raw
// values with no $ref naming them: what a path item keeps onto each of its
// operations past its first, and the servers it names on them. It reports
// whether the copies so far are within the budget.
func Recopied(c Ctx, raw int) bool {
	return c.expansion.spend(raw)
}

// chainable is a reference-or-inline entry whose chain Declared reads: the
// component aliases it passes through (resolve.Hops), each written as a node
// of its own (marshaller.Model.GetCoreAny).
type chainable[T, S any] interface {
	*S
	resolve.Referenced[T, S]
	IsReference() bool
	GetCoreAny() any
}

// readingOf returns how the grammar reads what a $ref written at usePtr in c's
// document names: as the object it reads there, or as raw YAML, weighed whole,
// where c places no such position, holds no table, or reads there no object a
// $ref can name.
func readingOf(c Ctx, usePtr jsontext.Pointer) bind.Reading {
	p, placed := c.Position(usePtr)
	if !placed {
		return bind.Reading{}
	}
	if at := c.Bindings().Table().ReadingAt(p); at.Referable() {
		return at
	}
	return bind.Reading{}
}

// copied returns the nodes a copy of what ref names is lowered from: end, the
// declaration its chain ends at, then each component alias it passes through,
// whose fields beside its own $ref lower at every use too (resolve.UseDocs,
// and a path item's every field). A hop whose core holds no node, as the
// library's own GetRootNode reads one, adds none.
func copied[T, S any, R chainable[T, S]](c Ctx, ref R, end *yaml.Node) []*yaml.Node {
	nodes := []*yaml.Node{end}
	if !resolve.Chained[T, S](ref) {
		return nodes
	}
	for _, hop := range resolve.Hops[T, S](c.RefScope(), ref) {
		if core, ok := hop.Ref.GetCoreAny().(interface{ GetRootNode() *yaml.Node }); ok {
			nodes = append(nodes, core.GetRootNode())
		}
	}
	return nodes
}

// refusal returns the error refusing a document whose $refs and path items
// copied past the budget, at source srcIndex, and false while they have not.
// It names no position: which copy crossed the budget follows the walk's order.
func (e *expansion) refusal(srcIndex int) (ir.Diagnostic, bool) {
	if e == nil || !e.spent.Exhausted() {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, ir.Provenance{Source: srcIndex},
		"the copies its $refs and path items make weigh more than the %d budgeted for them: each $ref "+
			"to a declaration other than a schema or a link lowers its own copy of it, and each operation "+
			"past a path item's first its own copy of what the item declares", e.spent.Limit().Cap()), true
}

// frame is a node weightOf has yet to weigh, and how the grammar reads it.
type frame struct {
	node *yaml.Node
	at   bind.Reading
}

// weightOf returns what a copy of the declaration under root, read as at,
// weighs: one for each node, an alias as a copy of its target, as the model is
// built (docs/parser-library.md#alias-expansion), one for each byte of a
// scalar, and one for a schema, which lowers once, to a type every copy shares.
// A schema beneath a merge key weighs in full. It visits at most one node past
// within, so a weight past within is all it says. No alias leads back up:
// lowering never sees an anchor naming its own ancestor.
func weightOf(root *yaml.Node, at bind.Reading, within budget.Limit) int {
	weight, left := 0, within.Past()
	stack := []frame{{node: root, at: at}}
	for len(stack) > 0 && left > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := expanded(f.node)
		if n == nil {
			continue
		}
		left--
		weight++
		switch {
		case f.at.Schema():
			// What it holds lowers once, to the type, not at each copy.
		case n.Kind == yaml.ScalarNode:
			weight += len(n.Value)
		default:
			stack = beneath(stack, n, f.at)
		}
	}
	return weight
}

// expanded returns what weightOf visits for n: what an alias names, in its
// place, or n itself, as nodeview.AliasTarget reads it.
func expanded(n *yaml.Node) *yaml.Node {
	if t := nodeview.Deref(n); t != nil {
		return t
	}
	return n
}

// beneath returns stack with a frame pushed onto it for each position directly
// below n, read as at: a key as raw YAML, and its value as the grammar reads
// the key's position.
func beneath(stack []frame, n *yaml.Node, at bind.Reading) []frame {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			token := ""
			if k := expanded(key); k != nil {
				token = k.Value
			}
			stack = append(stack, frame{node: key}, frame{node: n.Content[i+1], at: at.Below(token)})
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			stack = append(stack, frame{node: item, at: at.Below(strconv.Itoa(i))})
		}
	default:
	}
	return stack
}
