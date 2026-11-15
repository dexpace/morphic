// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"errors"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxPayloadDepth bounds how deep a payload is weighed or copied, per the
// bounded-recursion rule. yaml.v3 refuses to parse nesting past 10,000, the
// loader refuses an overlay nested past its scan's bound, and an alias adds one
// level per hop, so no payload this compiler is handed comes near it. A weigh
// that reaches it answers "past any budget", and a copy refuses the action.
const maxPayloadDepth = 1 << 14

// MaxSteps bounds the steps the selectors of one application of an overlay
// spend between them (jsonpath.Query.Select), about 268 scans of every node
// of a document of a million. A selector costs what it visits, which a
// document can make far more than its text; past the bound the action is
// refused, and nothing more applies. A step is a count, not a time: the
// costliest take some hundreds of nanoseconds, so the bound admits tens of
// seconds of selection, which only the caller's context cuts shorter.
const MaxSteps = 1 << 28

// sequence applies a plan's actions in order, each to the tree the ones before
// it left, as the specification requires, and charges each to the node budget
// before it builds anything, and its selectors to one budget of steps.
//
// An action whose update cannot be applied as the specification says ends the
// sequence with an error, since the actions that landed are not undone. One
// whose selector selects nothing is the specification's no-op, which strict
// application refuses at the end, having reported every such action, and lax
// application reports as a warning.
type sequence struct {
	plan plan
	root *yaml.Node
	lax  bool

	// steps is what the application's selectors may yet spend.
	steps budget.Budget

	// size counts the tree's nodes against the node budget: a count that
	// stops one node past the budget, then raised by each action's cost and
	// each copy it makes. Within the budget it is an upper bound on the tree;
	// past it, it says only that the tree may be past too, so it is recounted
	// before a refusal (grafter.admit).
	size budget.Budget

	// grafts writes each action's update into the tree.
	grafts *grafter

	diags []ir.Diagnostic
	// missed reports that an action selected nothing under strict application.
	missed bool
}

// runSequence applies p to root and returns the grafter that wrote it, which
// holds what the actions did, what it reported, and whether the tree is the
// overlay applied: false once an action cannot apply, or one would build past
// maxNodes or select past steps.
func runSequence(ctx context.Context, p plan, root *yaml.Node, lax bool, maxNodes, steps budget.Limit,
) (*grafter, []ir.Diagnostic, bool) {
	s := &sequence{plan: p, root: root, lax: lax, steps: budget.New(steps), grafts: newGrafter(p.version, root)}
	s.size = budget.New(maxNodes)
	if _, bounded := maxNodes.Max(); bounded {
		s.grafts.size = &s.size
		s.grafts.measure(ctx)
	}
	named, read := aliasCounts(ctx, root)
	if !read {
		return nil, nil, false
	}
	s.grafts.named = named
	for _, a := range p.actions {
		if refused, ok := s.apply(ctx, a); !ok {
			return nil, refused, false
		}
	}
	if s.missed {
		return nil, s.diags, false
	}
	return s.grafts, s.diags, true
}

// apply applies one action, and returns what refuses the sequence where it
// cannot: nothing where a cancel cut it short, since the caller reads its
// context next.
func (s *sequence) apply(ctx context.Context, a action) ([]ir.Diagnostic, bool) {
	sel, err := s.selected(ctx, a)
	switch {
	case ctx.Err() != nil:
		return nil, false
	case err != nil:
		return append(s.diags, s.unselectable(a, err)), false
	}
	if cost := s.cost(a, nodesOf(sel.targets), nodesOf(sel.sources)); cost > 0 && !s.grafts.admit(ctx, cost) {
		return append(s.diags, s.overBudget(a)), false
	}
	// A charge a cancel cut short may be partial, so the cancel is seen
	// before the action writes. The tree is left as the actions before left
	// it, which no caller builds on.
	if ctx.Err() != nil {
		return nil, false
	}
	err = s.step(ctx, a, sel)
	switch {
	case ctx.Err() != nil:
		return nil, false
	case errors.Is(err, jsonpath.ErrSteps):
		return append(s.diags, s.unselectable(a, err)), false
	case errors.Is(err, errPastNodeBudget):
		return append(s.diags, s.overBudget(a)), false
	case err != nil:
		return append(s.diags, fail(a, err)), false
	default:
		return nil, true
	}
}

// selection is what an action's selectors select, read before anything is
// written: each target once, the number the locator gave its position, and
// for a copy the nodes its copy selector selects.
type selection struct {
	at      *locator
	targets []jsonpath.Located
	ids     []int
	sources []jsonpath.Located
}

// selected returns what a's selectors select in the value the tree now
// stands for, each target location once. A view is made for the action,
// since each write changes the tree.
func (s *sequence) selected(ctx context.Context, a action) (selection, error) {
	if a.kind == kindNone {
		return selection{}, nil
	}
	if nodeview.DocumentRoot(s.root) == nil {
		return selection{}, errors.New("it selects no value: the document holds none")
	}
	view := nodeview.New()
	found, err := a.targets.Select(ctx, view, s.root, &s.steps)
	if err != nil {
		return selection{}, err
	}
	sel := selection{at: newLocator(s.grafts, &s.steps)}
	for _, t := range found {
		id, again, err := sel.at.number(ctx, t.Path)
		if err != nil {
			return selection{}, err
		}
		if !again {
			sel.targets, sel.ids = append(sel.targets, t), append(sel.ids, id)
		}
	}
	if a.kind == kindCopy {
		sel.sources, err = a.sources.Select(ctx, view, s.root, &s.steps)
	}
	return sel, err
}

// unselectable builds the refusal of an action whose selectors could not be
// read to the end: past the steps or the nodelist an overlay's selectors may
// spend, or through a mapping whose merges pass the depth the compiler reads.
func (s *sequence) unselectable(a action, err error) ir.Diagnostic {
	switch {
	case errors.Is(err, jsonpath.ErrSteps):
		return diag.Newf(ir.SeverityError, diag.BudgetExceeded, a.overlay,
			"%s: its selectors cost more than the %d steps an overlay's selectors may spend; nothing more applies",
			a.label(), s.steps.Limit().Cap())
	case errors.Is(err, jsonpath.ErrResults):
		return diag.Newf(ir.SeverityError, diag.BudgetExceeded, a.overlay,
			"%s: its selectors hold more than the %d nodes an evaluation's nodelists may hold at once; nothing more applies",
			a.label(), jsonpath.MaxResults)
	default:
		return fail(a, err)
	}
}

// step applies a to the nodes it selects, and returns why it cannot, which
// ends the sequence. A copy selecting more than the one node a copy copies is
// refused whatever its target selects.
func (s *sequence) step(ctx context.Context, a action, sel selection) error {
	targets, sources := sel.targets, sel.sources
	switch {
	case a.kind == kindNone:
		return nil
	case a.kind == kindCopy && len(sources) > 1:
		return fmt.Errorf("its copy selector selects %d nodes, and a copy copies one", len(sources))
	case len(targets) == 0:
		s.miss(a, "its target selects nothing")
		return nil
	case a.kind == kindCopy && len(sources) == 0:
		s.miss(a, "its copy selector selects nothing")
		return nil
	}
	payload := a.payload
	if a.kind == kindCopy {
		// A template is taken of the source first, so each target receives
		// it as it stood when the action began, though the source may hold a
		// target, or a target the source.
		template, err := copyOf(sources[0].Node, nil, 0, nil)
		if err != nil {
			return err
		}
		payload = template
	}
	if a.kind == kindRemove {
		positions, err := sel.at.positions(ctx, sel.ids)
		if err != nil {
			return err
		}
		return s.grafts.remove(positions)
	}
	positions := make([]position, len(sel.ids))
	for i, id := range sel.ids {
		node, err := sel.at.own(ctx, id)
		if err != nil {
			return err
		}
		positions[i] = position{node: node}
	}
	return s.write(ctx, a, positions, payload)
}

// write writes payload into each of targets and reports what that did: each
// value the source declares that it replaced, as an info naming both values,
// and a write that changed nothing, as a warning.
func (s *sequence) write(ctx context.Context, a action, positions []position, payload *yaml.Node) error {
	targets := make([]*yaml.Node, len(positions))
	for i, p := range positions {
		targets[i] = p.node
	}
	s.grafts.replaced = s.grafts.replaced[:0]
	changed, err := s.grafts.write(ctx, targets, payload)
	if err != nil {
		return err
	}
	for _, r := range s.grafts.replaced {
		s.diags = append(s.diags, replaced(a, r))
	}
	if !changed {
		s.diags = append(s.diags, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
			"%s changes nothing: what it writes is there already", a.label()))
	}
	return nil
}

// replaced reports r, a value the source declares that a replaced, at the
// value that replaced it where the overlay writes it, and at the action where
// it copies it from the source.
func replaced(a action, r replacement) ir.Diagnostic {
	at := a.overlay
	if r.with.Line > 0 {
		at.Position = ir.Position{Line: r.with.Line, Column: r.with.Column}
	}
	return diag.Newf(ir.SeverityInfo, diag.OverlayReplacedValue, at,
		"%s replaces %q, which the source declares at line %d, column %d, with %q", a.label(), r.was, r.line, r.column, r.with.Value)
}

// miss reports an action that, for the reason given, changes nothing: the
// specification's no-op, which strict application refuses and lax
// application warns of.
func (s *sequence) miss(a action, reason string) {
	if s.lax {
		s.diags = append(s.diags, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
			"%s changes nothing: %s", a.label(), reason))
		return
	}
	s.missed = true
	s.diags = append(s.diags, diag.Newf(ir.SeverityError, diag.OverlayFailed, a.overlay,
		"%s changes nothing: %s, which strict application refuses as a likely typo", a.label(), reason))
}

// fail builds the error ending a sequence on an action that cannot apply as
// written, for the reason err gives.
func fail(a action, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.OverlayFailed, a.overlay, "%s: %s", a.label(), err)
}

// cost is the most nodes a can add to the tree: what it copies, times how many
// nodes its target selects. It is held at the first count past the node
// budget, since anything past it is refused whatever its size.
//
// A remove adds nothing, and nor does a copy whose source is not exactly one
// node: the sequence refuses or skips it without copying anything. The
// template a copy takes of its source first is not charged, as it never joins
// the tree; it weighs what one more target would.
func (s *sequence) cost(a action, targets, sources []*yaml.Node) int {
	matches := len(targets)
	payload := a.payload
	switch {
	case a.kind == kindRemove || a.kind == kindNone || matches == 0:
		return 0
	case a.kind == kindCopy && len(sources) != 1:
		return 0
	case a.kind == kindCopy:
		payload = sources[0]
	default:
	}
	limit := s.size.Limit()
	per := newWeigher(limit).weigh(payload, 0)
	if per > limit.Past()/matches {
		return limit.Past() // the product is past the budget; computing it could overflow
	}
	return per * matches
}

// overBudget builds the refusal for the action that would have built, or
// copied, past the budget, before it built that.
func (s *sequence) overBudget(a action) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, a.overlay,
		"%s would grow the document to at least %d nodes, past the %d-node budget; it was refused before building past it",
		a.label(), s.size.Spent(), s.size.Limit().Cap())
}

// nodesOf returns the nodes of a selection.
func nodesOf(located []jsonpath.Located) []*yaml.Node {
	nodes := make([]*yaml.Node, len(located))
	for i, l := range located {
		nodes[i] = l.Node
	}
	return nodes
}

// countNodes counts the nodes a Content walk from root reaches, no further than
// one past limit: a tree past limit is refused at any size, and a refusal
// already says "at least".
func countNodes(ctx context.Context, root *yaml.Node, limit budget.Limit) int {
	return nodeview.Walk{Nodes: limit.Past()}.Run(ctx, nodeview.DocumentRoot(root)).Visited
}

// weigher counts the nodes a copy of a node builds: every node beneath it,
// and through each alias everything the alias names, since the copy follows
// the alias and copies that too.
//
// Each node is weighed once, so an anchor named from many places costs one walk
// of it. A node met again on its own path is a cycle, which a copy would
// follow without end; it weighs the first count past the limit, putting the
// budget out of reach. The loader refuses such an overlay first (GitHub #489);
// this only terminates.
type weigher struct {
	memo   map[*yaml.Node]int
	onPath map[*yaml.Node]bool
	limit  budget.Limit
}

// newWeigher returns a weigher whose answers stop at the first count past
// limit.
func newWeigher(limit budget.Limit) *weigher {
	return &weigher{memo: map[*yaml.Node]int{}, onPath: map[*yaml.Node]bool{}, limit: limit}
}

// weigh returns how many nodes copying n builds, held at the first count past
// the limit, which no sum of them overflows.
func (w *weigher) weigh(n *yaml.Node, depth int) int {
	if n == nil {
		return 0
	}
	if v, ok := w.memo[n]; ok {
		return v
	}
	if depth > maxPayloadDepth || w.onPath[n] {
		return w.limit.Past()
	}
	w.onPath[n] = true
	defer delete(w.onPath, n)

	total := budget.New(w.limit)
	total.Spend(1)
	if n.Kind == yaml.AliasNode {
		total.Spend(w.weigh(n.Alias, depth+1))
	}
	for _, child := range n.Content {
		if total.Exhausted() {
			break
		}
		total.Spend(w.weigh(child, depth+1))
	}
	weight := min(total.Spent(), w.limit.Past())
	w.memo[n] = weight
	return weight
}
