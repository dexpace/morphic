package scan

import (
	"context"
	"math"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// maxAliasAmplification bounds how many times larger a document's alias-
// expanded form may be than the document as parsed. An alias-free document
// expands to exactly its own size, so a large spec is never refused by it; what
// crosses it is the billion-laughs shape that exhausts memory inside
// soa.Unmarshal (GitHub #27).
//
// The highest ratio among 1,693 real specs is 3.728, so 128 leaves a 34x
// margin, wide on purpose: a `<<` merge chain inflates the ratio far past its
// cost (a 200-level chain measures 67 and compiles in 16 MiB). It is a limit on
// a shape, not a budget; the amount is the caller's surplus.
const maxAliasAmplification = 128

// minExpandedNodes is the expansion granted regardless of source size, so a
// small document with ordinary anchor reuse is never refused on a noisy ratio.
// It binds only under 256 raw nodes; from 256 up, maxAliasAmplification times
// the raw count reaches it. Real specs that small carry surpluses in the low
// hundreds, nowhere near this floor.
const minExpandedNodes = 1 << 15 // 32768

// aliasAmplification reports whether root's alias-substituted form, what the
// parser decodes (docs/parser-library.md#alias-expansion), exceeds what the
// document may expand to, with an error at the innermost node that crossed,
// least by pointer. raw is the document's own node count.
//
// Two bounds apply. The ratio (maxAliasAmplification, floored at
// minExpandedNodes) catches compounding aliasing, as diag.AliasAmplification
// whatever the budget. surplus, the nodes aliasing may add beyond raw,
// catches one anchor repeated in a flat list, whose ratio stays put while
// memory climbs, as diag.BudgetExceeded.
//
// Run it only once recursive anchors are refused, which makes the alias graph
// a DAG (see Aliases).
func aliasAmplification(ctx context.Context, locate Locator, root *yaml.Node, raw int, surplus budget.Limit,
) (ir.Diagnostic, bool) {
	shape := shapeAllowance(raw)
	// A surplus the ratio already bounds more tightly can never be the bound
	// crossed, so only a tighter one is weighed against.
	bound := budget.AtMost(raw).Plus(surplus)
	budgeted := bound.Cap() < shape.Cap()
	allowance := shape
	if budgeted {
		allowance = bound
	}

	// One walk against whichever bound binds, so a document refused by neither —
	// every document a compile goes on to lower — costs a single walk.
	culprits := newAliasWeigher(allowance).weigh(ctx, root)
	if len(culprits) == 0 {
		return ir.Diagnostic{}, false
	}
	if !budgeted {
		return aliasAmplificationDiag(locate, Least(ctx, root, culprits), shape, raw), true
	}

	// Past the budget, the document is weighed again against the ratio, so one
	// past both is named for the shape, which no budget admits. Only a refused
	// document pays for this walk.
	if bombs := newAliasWeigher(shape).weigh(ctx, root); len(bombs) > 0 {
		return aliasAmplificationDiag(locate, Least(ctx, root, bombs), shape, raw), true
	}
	return aliasBudgetDiag(locate, Least(ctx, root, culprits), surplus, raw), true
}

// shapeAllowance returns the bound the ratio rule sets on the expanded weight
// of a document of raw nodes: maxAliasAmplification times its size, floored at
// minExpandedNodes and held at math.MaxInt.
func shapeAllowance(raw int) budget.Limit {
	if raw > math.MaxInt/maxAliasAmplification {
		return budget.AtMost(math.MaxInt)
	}
	return budget.AtMost(max(maxAliasAmplification*raw, minExpandedNodes))
}

// aliasAmplificationDiag builds a diag.AliasAmplification error diagnostic
// anchored where locate puts n, the innermost node past allowance that
// aliasAmplification names. The reported node count is a lower bound ("at
// least"), not the exact expansion: aliasWeigher holds its sums one past the
// allowance, so the true expansion of a severe bomb (the 10-level x 10-way
// fixture expands past 37 billion nodes) is never actually computed, only
// proven to exceed the allowance.
func aliasAmplificationDiag(locate Locator, n *yaml.Node, allowance budget.Limit, raw int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.AliasAmplification, locate(n),
		"YAML alias expansion reaches at least %d nodes, past the %d-node allowance for a %d-node document",
		allowance.Past(), allowance.Cap(), raw)
}

// aliasBudgetDiag builds the diag.BudgetExceeded error diagnostic for a
// document whose aliases add more nodes than the surplus budget, anchored as
// aliasAmplificationDiag anchors its own and a lower bound for the same reason.
func aliasBudgetDiag(locate Locator, n *yaml.Node, surplus budget.Limit, raw int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, locate(n),
		"YAML aliases add at least %d nodes to a %d-node document, past the %d-node alias budget",
		surplus.Past(), raw, surplus.Cap())
}

// weighFrame is one entry of aliasWeigher's iterative post-order stack: a
// node awaiting the weight of what it depends on (its Content, or for an
// alias node, its target), and whether that dependency step has already run.
type weighFrame struct {
	n        *yaml.Node
	expanded bool
}

// aliasWeigher computes expandedWeight(n), held at the first count past its
// allowance, for every node reachable from a root. A nil node weighs 0, an
// alias weighs what its target weighs, and any other node weighs 1 plus the
// weight of its Content.
//
// That is what soa.Unmarshal pays: it builds a model subtree for every path
// reaching a node (docs/parser-library.md#alias-expansion). The count is exact
// for aliases and an over-count, the safe side, for `<<` merge keys, whose
// pairs are deduplicated.
type aliasWeigher struct {
	allowance budget.Limit
	weight    map[*yaml.Node]int
	inFlight  map[*yaml.Node]bool
	// cut holds each node a cycle reached while it was in flight, whose weight is
	// the first count past the allowance rather than one weighed.
	cut map[*yaml.Node]bool

	// computations counts how many times computeWeight actually ran, as
	// opposed to how many times a node's weight was merely read from the
	// memo. It exists so the memoization invariant — every distinct node is
	// computed at most once, however many aliases point to it — is a plain
	// equality check (computations == len(weight)) rather than something only
	// a timing bound can observe.
	computations int
}

// newAliasWeigher returns a weigher that refuses past allowance, holding every
// sum at the first count past it, so no addition can overflow regardless of
// how large the document's true expansion is.
func newAliasWeigher(allowance budget.Limit) *aliasWeigher {
	return &aliasWeigher{
		allowance: allowance,
		weight:    map[*yaml.Node]int{},
		inFlight:  map[*yaml.Node]bool{},
		cut:       map[*yaml.Node]bool{},
	}
}

// weigh computes expandedWeight for every node reachable from root, and returns
// in post-order every innermost amplifier: a node whose weight exceeds
// w.allowance though nothing it depends on does, the smallest structure already
// too large. It returns none for a document within the allowance. It weighs on
// past the first, since which comes first is the document's declaration order.
//
// A node is expanded once at most: a weighed node's frame pops unexpanded, and
// pushChildren skips one in flight. So the stack holds root's frame and at most
// one per Content entry or alias. A done ctx stops it, partway.
func (w *aliasWeigher) weigh(ctx context.Context, root *yaml.Node) []*yaml.Node {
	if root == nil {
		return nil
	}
	var innermost []*yaml.Node
	stack := []*weighFrame{{n: root}}
	for len(stack) > 0 && ctx.Err() == nil {
		top := stack[len(stack)-1]
		if _, done := w.weight[top.n]; done {
			stack = stack[:len(stack)-1]
			continue
		}
		if !top.expanded {
			top.expanded = true
			w.inFlight[top.n] = true
			w.pushChildren(&stack, top.n)
			continue
		}

		delete(w.inFlight, top.n)
		wt := w.computeWeight(top.n)
		w.computations++
		w.weight[top.n] = wt
		stack = stack[:len(stack)-1]
		if !w.allowance.Admits(wt) && !w.dependsOnAmplifier(top.n) {
			innermost = append(innermost, top.n)
		}
	}
	return innermost
}

// dependsOnAmplifier reports whether something n's weight depends on (see
// childrenOf), every one of which is weighed by the time n is, is itself past
// the allowance. A node a cycle cut is not weighed and never finishes, so the
// node depending on it is the innermost there is.
func (w *aliasWeigher) dependsOnAmplifier(n *yaml.Node) bool {
	return slices.ContainsFunc(childrenOf(n), func(c *yaml.Node) bool {
		return !w.cut[c] && !w.allowance.Admits(w.weight[c])
	})
}

// pushChildren enqueues the nodes n's weight depends on (see childrenOf). A
// child whose weight is already known is not re-pushed. A child already in
// flight is a cycle that slipped past anchorCycle having refused every
// recursive anchor — unreachable from a parsed document, but defended against
// anyway: rather than re-enter it and loop, its weight is held at the first
// count past the allowance, which is always large enough to carry the
// document to refusal without ever pretending the cyclic subtree is small.
func (w *aliasWeigher) pushChildren(stack *[]*weighFrame, n *yaml.Node) {
	for _, c := range childrenOf(n) {
		if c == nil {
			continue
		}
		if w.inFlight[c] {
			w.weight[c], w.cut[c] = w.allowance.Past(), true
			continue
		}
		if _, done := w.weight[c]; done {
			continue
		}
		*stack = append(*stack, &weighFrame{n: c})
	}
}

// computeWeight returns n's expandedWeight, given that every node it depends
// on (via childrenOf) already has a recorded weight. An alias node's weight is
// exactly its target's — substituting a copy of the target, unexpanded
// further, is what an alias stands in for. Every other node's weight is 1
// (itself) plus its children's, counted against the allowance and held at the
// first count past it, with the loop exiting the moment the total exceeds the
// allowance, since nothing further down the same Content list can change that
// verdict.
func (w *aliasWeigher) computeWeight(n *yaml.Node) int {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return 0
		}
		return w.weight[n.Alias]
	}
	total := budget.New(w.allowance)
	total.Spend(1)
	for _, c := range n.Content {
		if !total.Spend(w.weight[c]) {
			break
		}
	}
	return min(total.Spent(), w.allowance.Past())
}

// childrenOf returns the nodes n's expandedWeight depends on: an alias node
// depends only on its target, never on its own (always empty) Content, and
// every other node depends on its Content. pushChildren and dependsOnAmplifier
// read dependencies through it, so only it and computeWeight, which weighs an
// alias as its target alone, tell the two kinds apart.
func childrenOf(n *yaml.Node) []*yaml.Node {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil
		}
		return []*yaml.Node{n.Alias}
	}
	return n.Content
}
