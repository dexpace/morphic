// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxPayloadDepth bounds how deep a payload is weighed or copied, per the
// bounded-recursion rule. yaml.v3 refuses to parse nesting past 10,000, the
// loader refuses an overlay nested past its scan's bound, and an alias adds one
// level per hop, so no payload this compiler is handed comes near it. A weigh
// that reaches it answers "past any budget", and a copy refuses the action.
const maxPayloadDepth = 1 << 14

// sequence applies a plan's actions in order, each to the tree the ones before
// it left, as the specification requires, and charges each to the node budget
// before it builds anything.
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

	// size counts the tree's nodes against the node budget: a count that
	// stops one node past the budget, then raised by each action's cost,
	// which is what the action can add and no less. Within the budget it is
	// an upper bound on the tree; past it, it says only that the tree may be
	// past too. So it is recounted before a refusal, and an action that merges
	// into keys already there, adding less than it is charged, or that follows
	// a remove is never refused on the charge alone.
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
// maxNodes.
func runSequence(ctx context.Context, p plan, root *yaml.Node, lax bool, maxNodes budget.Limit,
) (*grafter, []ir.Diagnostic, bool) {
	s := &sequence{plan: p, root: root, lax: lax, size: budget.New(maxNodes), grafts: newGrafter(p.version, root)}
	if s.budgeted() {
		s.measure(ctx)
	}
	found, read := readAliasing(ctx, root)
	if found.any() {
		s.diags = append(s.diags, aliasesNotRead(p.at))
	}
	if s.grafts.named = found.named; !read || len(found.named) > 0 && !s.grafts.indexParents(ctx) {
		return nil, nil, false
	}
	for _, a := range p.actions {
		targets, sources := s.selected(a)
		if over, ok := s.charge(ctx, a, targets, sources); !ok {
			return nil, append(s.diags, s.overBudget(a, over)), false
		}
		// A remove reads the tree's index of what holds each node, built under
		// ctx the first time. The library's evaluator reads no context, and a
		// charge or an index a cancel cut short may be partial, so the cancel
		// is seen before the action is applied. The tree is left half applied,
		// which no caller builds on.
		if (a.kind == kindRemove && !s.grafts.indexParents(ctx)) || ctx.Err() != nil {
			return nil, nil, false
		}
		s.grafts.reached, s.grafts.mergeKey = nil, false
		if err := s.step(a, targets, sources); err != nil {
			return nil, append(s.diags, fail(a, err)), false
		}
		if !s.reportAliasing(ctx, a) {
			return nil, nil, false
		}
	}
	if s.missed {
		return nil, s.diags, false
	}
	return s.grafts, s.diags, true
}

// selected returns the nodes a's target selects in the tree as it now stands,
// each once, and for a copy the nodes its copy selector selects. A query may
// select a node twice, and an action writes a node it selects once.
func (s *sequence) selected(a action) (targets, sources []*yaml.Node) {
	if a.kind == kindNone {
		return nil, nil
	}
	seen := map[*yaml.Node]bool{}
	for _, t := range a.targets.Query(s.root) {
		if !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	if a.kind == kindCopy {
		sources = a.sources.Query(s.root)
	}
	return targets, sources
}

// step applies a to the nodes it selects, and returns why it cannot, which
// ends the sequence. A copy selecting more than the one node a copy copies is
// refused whatever its target selects.
func (s *sequence) step(a action, targets, sources []*yaml.Node) error {
	switch {
	case a.kind == kindNone:
		return nil
	case a.kind == kindCopy && len(sources) > 1:
		return fmt.Errorf("its copy selector selects %d nodes, and a copy copies one", len(sources))
	case len(targets) == 0:
		s.miss(a, "its target selects nothing")
		return nil
	case a.kind == kindRemove:
		return s.grafts.remove(targets)
	case a.kind == kindUpdate:
		return s.write(a, targets, a.payload)
	case len(sources) == 0:
		s.miss(a, "its copy selector selects nothing")
		return nil
	default:
		// A template is taken of the source first, so each target receives
		// it as it stood when the action began, though the source may hold a
		// target, or a target the source.
		template, err := copyOf(sources[0], nil, 0, nil)
		if err != nil {
			return err
		}
		return s.write(a, targets, template)
	}
}

// write writes payload into each of targets and reports what that did: each
// value the source declares that it replaced, as an info naming both values,
// and a write that changed nothing, as a warning.
func (s *sequence) write(a action, targets []*yaml.Node, payload *yaml.Node) error {
	s.grafts.replaced = s.grafts.replaced[:0]
	changed, err := s.grafts.write(targets, payload)
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

// reportAliasing warns of what a's write made of the tree's aliases: that it
// changed what a node an alias names holds, and that it wrote a merge key.
// After a remove, which can take aliases with it, it counts the aliases left,
// and reports whether ctx let it.
func (s *sequence) reportAliasing(ctx context.Context, a action) bool {
	if g := s.grafts; g.reached != nil {
		s.diags = append(s.diags, writeReachesAliases(a, g.reached, g.named[g.reached]))
	}
	if s.grafts.mergeKey {
		s.diags = append(s.diags, mergeKeyWritten(a))
	}
	if a.kind != kindRemove || len(s.grafts.named) == 0 {
		return true
	}
	found, read := readAliasing(ctx, s.root)
	s.grafts.named = found.named
	return read
}

// fail builds the error ending a sequence on an action that cannot apply as
// written, for the reason err gives.
func fail(a action, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.OverlayFailed, a.overlay, "%s: %s", a.label(), err)
}

// budgeted reports whether the application has a budget to keep to.
func (s *sequence) budgeted() bool {
	_, bounded := s.size.Limit().Max()
	return bounded
}

// measure takes the tree's node count afresh as the running size.
func (s *sequence) measure(ctx context.Context) {
	s.size = budget.New(s.size.Limit())
	s.size.Spend(countNodes(ctx, s.root, s.size.Limit()))
}

// charge adds what a can build to the running size and reports whether it
// still fits, and if not, the size it would reach. An action that builds
// nothing, a remove among them, fits however large the tree: the source may be
// past the budget until an action shrinks it, and the node budget is read of
// the tree the overlay leaves. One that builds is charged against the tree as
// it stands, so it is refused while the tree is past the budget, whatever a
// later action would remove.
func (s *sequence) charge(ctx context.Context, a action, targets, sources []*yaml.Node) (int, bool) {
	if !s.budgeted() {
		return 0, true
	}
	cost := s.cost(a, targets, sources)
	if cost == 0 || s.size.Spend(cost) {
		return 0, true
	}
	// The running size only ever overstates, so take the real count before
	// refusing on it.
	s.measure(ctx)
	if s.size.Spend(cost) {
		return 0, true
	}
	return s.size.Spent(), false
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

// overBudget builds the refusal for the action that would have built past the
// budget, before it built anything.
func (s *sequence) overBudget(a action, reached int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, a.overlay,
		"%s would grow the document to at least %d nodes, past the %d-node budget; nothing it would build was built",
		a.label(), reached, s.size.Limit().Cap())
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
