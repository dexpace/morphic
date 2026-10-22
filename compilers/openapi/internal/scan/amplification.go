package scan

import (
	"slices"

	yaml "gopkg.in/yaml.v3"

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
// whatever the budget. surplus, the nodes aliasing may add beyond raw (zero for
// none), catches one anchor repeated in a flat list, whose ratio stays put
// while memory climbs, as diag.BudgetExceeded.
//
// Run it only once recursive anchors are refused, which makes the alias graph
// a DAG (see Aliases).
func aliasAmplification(locate Locator, root *yaml.Node, raw, surplus int64) (ir.Diagnostic, bool) {
	shape := shapeAllowance(raw)
	// A surplus the ratio already bounds more tightly can never be the bound
	// crossed, and leaving it out also keeps raw+surplus from overflowing.
	budgeted := surplus > 0 && surplus < shape-raw
	bound := shape
	if budgeted {
		bound = raw + surplus
	}

	// One walk against whichever bound binds, so a document refused by neither —
	// every document a compile goes on to lower — costs a single walk.
	culprits := newAliasWeigher(bound).weigh(root)
	if len(culprits) == 0 {
		return ir.Diagnostic{}, false
	}
	if !budgeted {
		return aliasAmplificationDiag(locate, Least(root, culprits), shape, raw), true
	}

	// Past the budget, the document is weighed again against the ratio, so one
	// past both is named for the shape, which no budget admits. Only a refused
	// document pays for this walk.
	if bombs := newAliasWeigher(shape).weigh(root); len(bombs) > 0 {
		return aliasAmplificationDiag(locate, Least(root, bombs), shape, raw), true
	}
	return aliasBudgetDiag(locate, Least(root, culprits), surplus, raw), true
}

// shapeAllowance returns the expanded weight the ratio rule admits for a
// document of raw nodes: maxAliasAmplification times its size, floored at
// minExpandedNodes.
func shapeAllowance(raw int64) int64 {
	return max(maxAliasAmplification*raw, minExpandedNodes)
}

// aliasAmplificationDiag builds a diag.AliasAmplification error diagnostic
// anchored where locate puts the node whose expansion first crossed allowance.
// The reported node count is a lower bound ("at least"), not the exact
// expansion: aliasWeigher saturates its arithmetic at the allowance, so the
// true expansion of a severe bomb (the 10-level x 10-way fixture expands past
// 37 billion nodes) is never actually computed, only proven to exceed the
// allowance.
func aliasAmplificationDiag(locate Locator, n *yaml.Node, allowance, raw int64) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.AliasAmplification, locate(n),
		"YAML alias expansion reaches at least %d nodes, past the %d-node allowance for a %d-node document",
		allowance+1, allowance, raw)
}

// aliasBudgetDiag builds the diag.BudgetExceeded error diagnostic for a
// document whose aliases add more nodes than the surplus budget, anchored as
// aliasAmplificationDiag anchors its own and a lower bound for the same reason.
func aliasBudgetDiag(locate Locator, n *yaml.Node, surplus, raw int64) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, locate(n),
		"YAML aliases add at least %d nodes to a %d-node document, past the %d-node alias budget",
		surplus+1, raw, surplus)
}

// weighFrame is one entry of aliasWeigher's iterative post-order stack: a
// node awaiting the weight of what it depends on (its Content, or for an
// alias node, its target), and whether that dependency step has already run.
type weighFrame struct {
	n        *yaml.Node
	expanded bool
}

// aliasWeigher computes expandedWeight(n) for every node reachable from a
// root, stopping once one node's weight exceeds its allowance. A nil node
// weighs 0, an alias weighs what its target weighs, and any other node weighs 1
// plus the weight of its Content.
//
// That is what soa.Unmarshal pays: it builds a model subtree for every path
// reaching a node (docs/parser-library.md#alias-expansion). The count is exact
// for aliases and an over-count, the safe side, for `<<` merge keys, whose
// pairs are deduplicated.
type aliasWeigher struct {
	allowance int64
	ceiling   int64
	weight    map[*yaml.Node]int64
	inFlight  map[*yaml.Node]bool
	// cut holds each node a cycle reached while it was in flight, whose weight is
	// the ceiling it was saturated to rather than one weighed.
	cut map[*yaml.Node]bool

	// computations counts how many times computeWeight actually ran, as
	// opposed to how many times a node's weight was merely read from the
	// memo. It exists so the memoization invariant — every distinct node is
	// computed at most once, however many aliases point to it — is a plain
	// equality check (computations == len(weight)) rather than something only
	// a timing bound can observe.
	computations int64
}

// newAliasWeigher returns a weigher that refuses at allowance, capping every
// intermediate sum at allowance+1 so no addition can overflow regardless of
// how large the document's true expansion is.
func newAliasWeigher(allowance int64) *aliasWeigher {
	return &aliasWeigher{
		allowance: allowance,
		ceiling:   allowance + 1,
		weight:    map[*yaml.Node]int64{},
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
// Each distinct node enters the stack at most once, because pushChildren skips
// one whose weight is known or that is in flight, so the stack is bounded by
// the distinct nodes reachable from root.
func (w *aliasWeigher) weigh(root *yaml.Node) []*yaml.Node {
	if root == nil {
		return nil
	}
	var innermost []*yaml.Node
	stack := []*weighFrame{{n: root}}
	for len(stack) > 0 {
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
		if wt > w.allowance && !w.dependsOnAmplifier(top.n) {
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
		return !w.cut[c] && w.weight[c] > w.allowance
	})
}

// pushChildren enqueues the nodes n's weight depends on (see childrenOf). A
// child whose weight is already known is not re-pushed. A child already in
// flight is a cycle that slipped past anchorCycle having refused every
// recursive anchor — unreachable from a parsed document, but defended against
// anyway: rather than re-enter it and loop, its weight is saturated at
// w.ceiling, which is always large enough to carry the document to refusal
// without ever pretending the cyclic subtree is small.
func (w *aliasWeigher) pushChildren(stack *[]*weighFrame, n *yaml.Node) {
	for _, c := range childrenOf(n) {
		if c == nil {
			continue
		}
		if w.inFlight[c] {
			w.weight[c], w.cut[c] = w.ceiling, true
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
// (itself) plus its children's, saturated at w.ceiling so the running sum can
// never overflow, with the loop exiting the moment the total already exceeds
// the allowance, since nothing further down the same Content list can change
// that verdict.
func (w *aliasWeigher) computeWeight(n *yaml.Node) int64 {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return 0
		}
		return w.weight[n.Alias]
	}
	total := int64(1)
	for _, c := range n.Content {
		total = saturatingAdd(total, w.weight[c], w.ceiling)
		if total > w.allowance {
			return total
		}
	}
	return total
}

// childrenOf returns the nodes n's expandedWeight depends on: an alias node
// depends only on its target, never on its own (always empty) Content, and
// every other node depends on its Content. This is the one place the two node
// kinds are told apart, so every other method can treat "the nodes n depends
// on" uniformly.
func childrenOf(n *yaml.Node) []*yaml.Node {
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil
		}
		return []*yaml.Node{n.Alias}
	}
	return n.Content
}

// saturatingAdd returns a+b clamped to ceiling. Both aliasWeigher's addends
// are always non-negative and individually at most ceiling, which is what
// makes the comparison overflow-free: b >= ceiling-a can be evaluated without
// a+b ever being computed when it would exceed ceiling.
func saturatingAdd(a, b, ceiling int64) int64 {
	if b >= ceiling-a {
		return ceiling
	}
	return a + b
}
