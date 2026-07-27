package openapi

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// maxAliasAmplification bounds how many times larger a document's alias-
// expanded form may be than the document as parsed. The bound is on
// amplification, not absolute size: an alias-free document expands to exactly
// its own node count (expandedWeight == rawNodeCount, see
// TestExpandedWeight_NoAliasesEqualsRawCount), so a genuinely large spec is
// never refused by this rule — what it costs to compile is what its own bytes
// already bought. What crosses this bound instead is a small number of bytes
// standing in, through aliases, for a structure many times their own size —
// the shape a billion-laughs document uses to turn a few hundred bytes into a
// heap that exhausts the process inside soa.Unmarshal (GitHub #27).
//
// This constant is calibrated against a corpus of 1,693 real-world OpenAPI and
// Swagger specs, not against invented documents: 1,491 pulled from APIs.guru,
// 199 unique hand-authored YAML-anchor-using specs surfaced by 14 rounds of
// GitHub code search (roughly 1,700 candidates filtered down to these), plus
// GitHub's own api.github.com.yaml, Stripe's spec3.yaml, and Kubernetes'
// swagger.json. Across every one of those specs, the highest ratio measured —
// expandedWeight divided by rawNodeCount — was 3.728, on a hand-authored spec
// (github.com/willhuff0/labrinth_dart's openapi.yaml: raw 5,765, 35 anchors,
// 144 aliases, expanding to 21,492 nodes). The three largest real specs
// measured (GitHub, Stripe, Kubernetes — up to 468,244 raw nodes) use no YAML
// anchors at all and ratio exactly 1.0, as do all 1,491 APIs.guru specs, which
// re-serialize and strip anchors in the process; anchor use itself is a
// small-minority pattern in this corpus, concentrated in personal and
// small-team projects rather than flagship APIs. 128 leaves a 34x margin over
// the worst real ratio measured — every real spec in this corpus clears it
// with room to spare, while a document amplifying by even an order of
// magnitude less than the classic billion-laughs shape is still refused.
//
// That margin is deliberately wider than the one maxAliasSurplus carries, and
// the reason is the merge-key over-count described below: a `<<` merge chain
// inflates this ratio far past what it actually costs, because the pairs a
// merge contributes are deduplicated by key rather than turned into objects.
// A 200-level merge chain measures a ratio of 67 while costing 16 MiB — about
// 103 bytes per counted node, against roughly 6 KiB for a nested allOf
// fan-out. Setting this bound near the real-spec maximum would therefore
// refuse cheap merge-heavy documents on a number that overstates their cost by
// some sixty-fold. Choosing the wider margin costs nothing in the worst case,
// because the ceiling on total expansion is maxAliasSurplus's to set, not this
// constant's: past roughly 2,000 raw nodes the surplus bound is always the
// lesser of the two (see computeAllowance), so raising this one moves only
// which small documents are refused, never how large an expansion can get.
//
// The shape that once argued for loosening this bound — one large,
// YAML-anchored base schema pulled into many sibling schemas via `allOf:
// [*base, {...}]`, ordinary DRY inheritance rather than recursion — is still
// real, and still accepted at real-world scale (see
// TestDetectCycles_RealWorldAnchorReuseIsClean). But the synthetic extreme
// once used to justify raising this constant (a 200-field base reused across
// 500 siblings: ratio≈130.6, surplus≈703,000) is 44x beyond the worst surplus
// ever measured in a real spec and costs roughly 2 GiB to compile, so
// recalibrating against real data means that extreme is now deliberately
// refused (see TestDetectCycles_SyntheticWideBaseReuseIsNowRefused) rather
// than accommodated.
//
// The two bounds in this file (this one and maxAliasSurplus) exist because
// neither alone can catch every amplifying shape. This one catches
// compounding, nested aliasing — the ratio grows without bound as fan-out
// depth increases, which is the billion-laughs shape itself. It cannot catch
// a single anchor reused many times in one flat list (`allOf: [*x, *x,
// ...]`): there each reuse costs one raw node and contributes the anchor's
// own weight, so the ratio converges to that fixed weight and never grows
// with reuse count no matter how large real memory use gets.
// maxAliasSurplus is the separate, additive backstop for that shape; see its
// comment and computeAllowance.
//
// This ratio is an exact object count, not a heuristic proxy, and that is why
// it is worth calibrating this precisely rather than picking a round number
// with slack built in. This repo depends on github.com/speakeasy-api/openapi
// v1.24.0 (see go.mod) to parse OpenAPI documents, and its yml.ResolveAlias
// (yml/yml.go) correctly returns the one shared *yaml.Node for every
// occurrence of an alias — but nothing in marshaller/ or jsonschema/ memoizes
// on that pointer. Every unmarshal entry point (unmarshalModel and its
// callers in marshaller/unmarshaller.go, around lines 146, 269, 289, and 554)
// resolves the alias and then builds a fresh model subtree for whichever path
// reached it, so the library allocates one model object per path through the
// alias graph, not one per distinct node. expandedWeight counts exactly that:
// it is the count of model objects the parser will build, exact for plain
// alias substitution and conservative (an over-count, the safe direction) for
// `<<` merge keys, whose contributed pairs are deduplicated by key rather
// than turned into new objects. This is a property of speakeasy v1.24.0's
// unmarshaller, not of the OpenAPI format, so a dependency bump should
// re-check it before trusting this reasoning again — the same register
// isMergeKey's comment in cycles.go already keeps for the resolver behavior
// it depends on.
//
// The node budget this constant scales still cannot be turned into one
// byte-per-node figure: measured cost per expanded node runs roughly 2.5 KiB
// in a wide-reuse shape and roughly 6.3 KiB in a nested allOf fan-out — the
// same object count costs different bytes depending on the kind of node it
// is. What this constant bounds precisely is the object count; the bytes
// follow from it only approximately.
const maxAliasAmplification = 128

// minExpandedNodes is the expansion budget granted regardless of source size,
// so a small document with ordinary anchor reuse — a handful of aliases to
// one shared block — is never refused on a noisy ratio. It only binds for
// documents under 256 raw nodes: at or above that size,
// maxAliasAmplification*rawNodeCount (128*256 = 32,768) already exceeds this
// floor and the ratio alone decides. Below it, this constant is the whole
// budget.
//
// This value is unchanged by the corpus recalibration above and did not need
// to be: it bounds a different regime (documents too small for the ratio to
// mean anything) than maxAliasAmplification and maxAliasSurplus do, and real
// specs in this size range are common (the corpus's smallest real spec has 13
// raw nodes) with surpluses measured in the low hundreds at most — nowhere
// near this floor, so it stays generous rather than binding.
const minExpandedNodes = 1 << 15 // 32768

// maxAliasSurplus bounds the absolute number of nodes aliasing may add beyond
// the document's own raw size — expandedWeight minus rawNodeCount, the extra
// weight attributable to substituting copies of aliased targets rather than
// referencing them once. It is a second, independent refusal alongside
// maxAliasAmplification, because the ratio alone cannot bound every shape:
// for a single anchor referenced N times in one flat list, expandedWeight
// grows as N*L and rawNodeCount as N (one raw node per reference), so their
// ratio converges to the anchor's own weight L and stops growing however
// large N gets — an anchor with L under maxAliasAmplification can be repeated
// without limit and the ratio check never fires, while real memory keeps
// growing with N regardless (see
// TestDetectCycles_FlatFanOutOfModestAnchorIsEventuallyRefused). The surplus
// this constant bounds is exactly N*(L-1) for that shape, so it grows with N
// even where the ratio does not, and it is what refuses that document once N
// is large enough to matter.
//
// This bound is additive, not relative, which is deliberate here and nowhere
// else in this file: an alias-free document always has surplus exactly zero
// (expandedWeight == rawNodeCount with no aliases to substitute), so no
// document is ever refused by this constant for its size alone — the
// "alias-free document of any size is accepted" guarantee this file leans on
// elsewhere holds for this check too, unconditionally rather than by
// calibration.
//
// Calibrated against the same 1,693-spec corpus maxAliasAmplification cites
// (1,491 APIs.guru specs, 199 hand-authored anchor-using specs, plus GitHub's,
// Stripe's, and Kubernetes' flagship specs): the largest surplus measured in
// any real spec is 15,727 nodes, on the same labrinth_dart document that set
// the ratio maximum (raw 5,765, expanded 21,492). The runner-up real surpluses
// measured were 9,750 / 4,877 / 4,847 / 4,446 / 3,502 — an order of magnitude
// below the maximum, not clustered near it. 1<<18 (262,144) gives a 16.7x
// margin over the worst real surplus measured.
//
// That margin still matters because the surplus is what an attacker maximizes
// once they pad a document's raw size enough to lift the ratio allowance out
// of the way: at the old 1<<21, a purpose-built 93 KiB document was measured
// peaking at 10.9 GiB and still accepted, which would still be a fatal crash
// on the 4 GB process GitHub #27 reports. This bound is also the only one in
// this file that translates approximately into memory, which is what makes it
// worth calibrating against measurement at all: surplus nodes are the nodes
// soa.Unmarshal materializes that the document did not itself declare — see
// maxAliasAmplification's comment for why this is an exact object count for
// speakeasy v1.24.0's unmarshaller, not an estimate — and they were measured
// to cost roughly 2.5 KiB each in a wide-reuse document and roughly 6.3 KiB
// each in a nested allOf fan-out, the most expensive shape found. The same
// node budget costs 2.5x more in one shape than another, so what this
// constant bounds precisely is the surplus object count, and only
// approximately the bytes.
const maxAliasSurplus = 1 << 18

// aliasAmplification reports whether root's alias-substituted form would
// contain far more nodes than the document itself declares, and if so returns
// an error diagnostic anchored at the innermost node whose expansion first
// crossed the allowance — the node the post-order weigh walk finishes first,
// and a genuinely useful place to point the document's author. The allowance
// is computeAllowance's combination of the ratio and absolute bounds; see its
// comment for why a document must clear both.
//
// Callers must run this only after anchorCycle has already refused a
// recursive YAML anchor: that is what makes the alias graph a DAG and this
// walk's termination provable without a cap of its own. See scanCycles for
// the ordering and aliasWeigher.pushChildren for the defensive guard kept
// anyway.
func aliasAmplification(srcIndex int, root *yaml.Node) (ir.Diagnostic, bool) {
	raw := rawNodeCount(root)
	allowance := computeAllowance(raw)

	culprit, exceeded := newAliasWeigher(allowance).weigh(root)
	if !exceeded {
		return ir.Diagnostic{}, false
	}
	return aliasAmplificationDiag(srcIndex, culprit, allowance, raw), true
}

// computeAllowance returns the expandedWeight a document with this many raw
// nodes may reach before it is refused: the lesser of a ratio-relative
// allowance (maxAliasAmplification*raw, floored at minExpandedNodes) and an
// absolute one (raw+maxAliasSurplus). A document must clear both, because
// each bounds a shape the other cannot: the ratio catches a small document
// standing in for an astronomically larger one (nested, compounding
// aliasing), while the absolute surplus catches a single anchor repeated
// without limit in one flat list, whose ratio never grows past the anchor's
// own weight no matter how many times it is referenced. Taking the minimum
// rather than requiring both independently keeps this a single number the
// weigher can enforce with one early-exiting walk.
func computeAllowance(raw int64) int64 {
	ratioAllowance := maxAliasAmplification * raw
	if ratioAllowance < minExpandedNodes {
		ratioAllowance = minExpandedNodes
	}

	surplusAllowance := raw + maxAliasSurplus
	if surplusAllowance < ratioAllowance {
		return surplusAllowance
	}
	return ratioAllowance
}

// rawNodeCount returns the number of nodes in the parsed tree rooted at n —
// the node count soa.Unmarshal's input actually holds, before any alias is
// substituted. An alias node contributes exactly one to the count and is
// never descended into: yaml.v3 gives every alias node empty Content, so this
// is a plain node-counting walk with no special casing required to keep an
// alias from being read as a copy of its target.
//
// The walk is iterative and bounded by the tree's own size. The raw parse
// tree is a tree, not a graph — aliasing only adds edges that this walk never
// follows — so no node is reachable by more than one path, and an explicit
// stack visits each node exactly once.
func rawNodeCount(root *yaml.Node) int64 {
	if root == nil {
		return 0
	}
	var count int64
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, n.Content...)
	}
	return count
}

// aliasAmplificationDiag builds a codeAliasAmplification error diagnostic
// anchored at the node whose expansion first crossed allowance, following
// cyclicDiag's line:col provenance convention. The reported node count is a
// lower bound ("at least"), not the exact expansion: aliasWeigher saturates
// its arithmetic at the allowance, so the true expansion of a severe bomb
// (the 10-level x 10-way fixture expands past 37 billion nodes) is never
// actually computed, only proven to exceed the budget.
func aliasAmplificationDiag(srcIndex int, n *yaml.Node, allowance, raw int64) ir.Diagnostic {
	prov := ir.Provenance{Source: srcIndex}
	if n != nil {
		prov.Pointer = fmt.Sprintf("%d:%d", n.Line, n.Column)
	}
	return diagf(ir.SeverityError, codeAliasAmplification, prov,
		"YAML alias expansion reaches at least %d nodes, past the %d-node budget for a %d-node document",
		allowance+1, allowance, raw)
}

// weighFrame is one entry of aliasWeigher's iterative post-order stack: a
// node awaiting the weight of what it depends on (its Content, or for an
// alias node, its target), and whether that dependency step has already run.
type weighFrame struct {
	n        *yaml.Node
	expanded bool
}

// aliasWeigher computes expandedWeight(n) for every node reachable from a
// root, stopping the instant one node's weight exceeds its allowance. It is
// the pre-parse stand-in for the cost soa.Unmarshal would actually pay: nil
// weighs 0, an alias node weighs whatever its target weighs (an alias stands
// in for a copy of its target, not a reference to it), and every other node
// weighs 1 plus the weight of its own Content.
type aliasWeigher struct {
	allowance int64
	ceiling   int64
	weight    map[*yaml.Node]int64
	inFlight  map[*yaml.Node]bool

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
	}
}

// weigh computes expandedWeight for every node reachable from root and
// returns the first node (in post-order) whose weight exceeds w.allowance,
// or nil if none does. Post-order is what makes the returned node the
// innermost amplifier: a node's weight is finished, and checked, before any
// of its ancestors' — so the walk exits at the smallest structure already
// known to be too large, rather than only at the document root.
//
// Each distinct node enters the stack at most once: pushChildren skips a node
// whose weight is already known or that is already in flight, so the stack is
// bounded by the number of distinct nodes reachable from root.
func (w *aliasWeigher) weigh(root *yaml.Node) (*yaml.Node, bool) {
	if root == nil {
		return nil, false
	}
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
		if wt > w.allowance {
			return top.n, true
		}
	}
	return nil, false
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
			w.weight[c] = w.ceiling
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
