// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/ir"
)

// expansion counts what a run's $refs copy against
// Policy.MaxReferenceExpansion. A declaration a $ref names, a schema aside,
// lowers again at every $ref naming it (GitHub #1240), so each such $ref
// is charged what the declarations its chain names weigh, and the sum bounds
// the product of references and size that the source budgets leave free, as
// it bounds what a path item's additionalOperations copy (ChargeExpansion). A
// declaration is weighed once for each reading and remembered: one weighed past
// what was left is remembered past it, which nothing reads again, since the
// budget is then crossed.
type expansion struct {
	spent   budget.Budget
	weights map[measuredDeclaration]int
}

// measuredDeclaration is a declaration as referenceExpansionSize weighs it: its node, how the grammar
// reads it, which decides what in it is a schema, and whether it is a hop of a
// chain, weighed for what it writes beside its $ref (weightBeside).
type measuredDeclaration struct {
	node   *yaml.Node
	at     reftable.Reading
	beside bool
}

// newExpansion returns an expansion charging against limit.
func newExpansion(limit budget.Limit) *expansion {
	return &expansion{spent: budget.New(limit), weights: map[measuredDeclaration]int{}}
}

// admit charges one copy of the declaration whose node is end, and of each hop
// of the chain reaching it, each read as at, and reports whether the copies so
// far are within the budget: end is weighed whole, and a hop for what a use
// copies of it (weightBeside). Once they are not it admits nothing, so no later
// $ref lowers. An unbounded one admits every copy without weighing it.
func (e *expansion) admit(at reftable.Reading, end *yaml.Node, hops ...*yaml.Node) bool {
	if _, bounded := e.spent.Limit().Max(); !bounded {
		return true
	}
	if e.spent.Exhausted() {
		return false
	}
	if !e.charge(measuredDeclaration{node: nodeview.Deref(end), at: at}) {
		return false
	}
	for _, hop := range hops {
		if !e.charge(measuredDeclaration{node: nodeview.Deref(hop), at: at, beside: true}) {
			return false
		}
	}
	return true
}

// charge spends what key weighs, weighed once and remembered, and reports
// whether the copies so far are within the budget.
func (e *expansion) charge(key measuredDeclaration) bool {
	weight, known := e.weights[key]
	if !known {
		weigh := referenceExpansionSize
		if key.beside {
			weigh = weightBeside
		}
		weight = weigh(key.node, key.at, e.spent.Remaining())
		e.weights[key] = weight
	}
	return e.spent.Spend(weight)
}

// spend charges raw, what a copy holds as raw values.
func (e *expansion) spend(raw int) {
	e.spent.Spend(raw)
}

// ChargeExpansionAt charges the expansion one more copy of the declaration at
// node, written at at in serviceContext's document, that a lowering makes with no $ref
// naming it: an inline parameter of a path item, at each additionalOperations
// entry that takes it (GitHub #1428). It weighs the copy as Declared weighs a
// $ref's. Whether the copies have crossed the budget is ExpansionCrossed's to
// say.
func ChargeExpansionAt(serviceContext Ctx, at jsontext.Pointer, node *yaml.Node) {
	serviceContext.expansion.admit(readingOf(serviceContext, at), node)
}

// ChargeExpansion charges the expansion raw more bytes that a lowering copies
// as raw values with no $ref naming them: what a path item keeps onto each of
// its additionalOperations entries, and the servers it names on them.
func ChargeExpansion(serviceContext Ctx, raw int) {
	serviceContext.expansion.spend(raw)
}

// ExpansionCrossed reports whether the copies made so far weigh past the
// budget, for a lowering to ask before it makes one more: the document is then
// refused (Ctx.ExpansionRefusal), so nothing more need copy.
func ExpansionCrossed(serviceContext Ctx) bool {
	return serviceContext.expansion.spent.Exhausted()
}

// chainable is a reference-or-inline entry whose chain Declared reads: the
// component aliases it passes through (componentlookup.Hops), each written as a node
// of its own (marshaller.Model.GetCoreAny).
type chainable[T, S any] interface {
	*S
	componentlookup.Referenced[T]
	GetCoreAny() any
}

// readingOf returns how the grammar reads what a $ref written at usePtr in serviceContext's
// document names: as the object it reads there, or as raw YAML, weighed whole,
// where serviceContext holds no table or reads there no object a $ref can name.
func readingOf(serviceContext Ctx, usePtr jsontext.Pointer) reftable.Reading {
	if at := serviceContext.Bindings().Table().ReadingAt(serviceContext.Position(usePtr)); at.Referable() {
		return at
	}
	return reftable.Reading{}
}

// copied returns the hops a copy of what ref names is lowered from, beside the
// declaration its chain ends at: ref's own mapping, then each component alias
// the chain passes through, whose fields beside its own $ref lower at every use
// too (componentlookup.UseDocs, and a path item's every field). What ref writes beside
// its $ref is kept on each node the use lowers to, so it is charged as a hop's
// is, and a bare ref one node. A hop whose core holds no node, as the library's
// own GetRootNode reads one, adds none.
func copied[T, S any, R chainable[T, S]](serviceContext Ctx, ref R) []*yaml.Node {
	hops := componentlookup.Hops[T, S](serviceContext.Bindings(), ref)
	nodes := appendRoot(make([]*yaml.Node, 0, 1+len(hops)), ref.GetCoreAny())
	for _, hop := range hops {
		nodes = appendRoot(nodes, hop.Ref.GetCoreAny())
	}
	return nodes
}

// appendRoot appends the node core holds, as the library's own GetRootNode
// reads it, to nodes, and nothing for a core holding none.
func appendRoot(nodes []*yaml.Node, core any) []*yaml.Node {
	if c, ok := core.(interface{ GetRootNode() *yaml.Node }); ok {
		return append(nodes, c.GetRootNode())
	}
	return nodes
}

// refusal returns the error refusing a document whose $refs and path items'
// additional operations copied past the budget, at source srcIndex, and false
// while they have not.
// It names no position: which copy crossed the budget follows the walk's order.
func (e *expansion) refusal(srcIndex int) (ir.Diagnostic, bool) {
	if !e.spent.Exhausted() {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, ir.Provenance{Source: srcIndex},
		"the copies its $refs and path items make weigh more than the %d budgeted for them: each $ref "+
			"to a declaration other than a schema lowers its own copy of it, and each "+
			"additionalOperations entry its own copy of what its path item declares", e.spent.Limit().Cap()), true
}

// frame is a node referenceExpansionSize has yet to weigh, and how the grammar reads it.
type frame struct {
	node *yaml.Node
	at   reftable.Reading
}

// referenceExpansionSize returns what a copy of the declaration under root, read as at,
// weighs: one for each node, an alias as a copy of its target, as the model is
// built (docs/parser-library.md#alias-expansion), one for each byte of a
// scalar, and one for a schema, which lowers once, to a type every copy shares.
// A schema beneath a merge key weighs in full. It visits at most one node past
// within, so a weight past within is all it says. No alias leads back up:
// lowering never sees an anchor naming its own ancestor.
func referenceExpansionSize(root *yaml.Node, at reftable.Reading, within budget.Limit) int {
	return weigh([]frame{{node: root, at: at}}, within.Past())
}

// weightBeside returns what a use copies of hop, a mapping of a chain writing a
// $ref, read as at: one for the mapping, and each key and value beside its $ref
// as referenceExpansionSize weighs them. The $ref is read, not copied, so it is charged
// nothing; one merged in through `<<` is weighed as written, beneath the merge
// key. A hop that is no mapping, or one read as a schema, is weighed whole.
func weightBeside(hop *yaml.Node, at reftable.Reading, within budget.Limit) int {
	node := expanded(hop)
	if node == nil || node.Kind != yaml.MappingNode || at.Schema() {
		return referenceExpansionSize(hop, at, within)
	}
	var stack []frame
	for i := 0; i+1 < len(node.Content); i += 2 {
		nodeValue := expanded(node.Content[i])
		if nodeValue != nil && nodeValue.Kind == yaml.ScalarNode && nodeValue.Value == "$ref" {
			continue
		}
		stack = pair(stack, node.Content[i], node.Content[i+1], at)
	}
	return 1 + weigh(stack, within.Past()-1)
}

// weigh returns what the nodes on stack weigh, as referenceExpansionSize weighs one, visiting
// at most left of them.
func weigh(stack []frame, left int) int {
	weight := 0
	for len(stack) > 0 && left > 0 {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := expanded(frame.node)
		if node == nil {
			continue
		}
		left--
		weight++
		switch {
		case frame.at.Schema():
			// What it holds lowers once, to the type, not at each copy.
		case node.Kind == yaml.ScalarNode:
			weight += len(node.Value)
		default:
			stack = beneath(stack, node, frame.at)
		}
	}
	return weight
}

// expanded returns what referenceExpansionSize visits for node: what an alias names, in its
// place, or node itself, as nodeview.AliasTarget reads it.
func expanded(node *yaml.Node) *yaml.Node {
	if nodeValue := nodeview.Deref(node); nodeValue != nil {
		return nodeValue
	}
	return node
}

// beneath returns stack with a frame pushed onto it for each position directly
// below node, read as at: a key as raw YAML, and its value as the grammar reads
// the key's position.
func beneath(stack []frame, node *yaml.Node, at reftable.Reading) []frame {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			stack = pair(stack, node.Content[i], node.Content[i+1], at)
		}
	case yaml.SequenceNode:
		for i, item := range node.Content {
			stack = append(stack, frame{node: item, at: at.Below(strconv.Itoa(i))})
		}
	default:
	}
	return stack
}

// pair returns stack with a frame pushed onto it for key, as raw YAML, and one
// for value, read as the grammar reads the position key names below at.
func pair(stack []frame, key, value *yaml.Node, at reftable.Reading) []frame {
	token := ""
	if node := expanded(key); node != nil {
		token = node.Value
	}
	return append(stack, frame{node: key}, frame{node: value, at: at.Below(token)})
}
