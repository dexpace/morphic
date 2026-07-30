package harness

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// maxReverseDepth bounds the rewrite's descent. It sits above the nesting any
// document the compiler accepts can reach — the OpenAPI compiler refuses schema
// nesting past 128 — so hitting it means a pathological input, not legitimate
// depth.
const maxReverseDepth = 512

// orderInvariant compiles data as written and again with every mapping's entry
// order reversed, and reports what the permutation changed beyond source order.
//
// It is the general form of the two-order diff CLAUDE.md prescribes.
// `deterministic`, next to it, looks like this and is a different property: it
// recompiles the *same bytes*, proving same-input-same-output. Only permuted
// input catches a lowering whose result depends on which declaration reached a
// pointer first — the shape of the pointer collisions in #108 and #112, which
// produce no diagnostic in either order and leave pass/validate clean on both.
//
// Two limits belong here so the oracle is not over-trusted:
//
// Reversing a mapping is meaning-preserving only while its keys are distinct. A
// document with duplicate mapping keys resolves to the last one (#95), so
// reversing changes which declaration wins and the two compiles legitimately
// differ; such sources are excluded rather than reported. Sequences are left
// alone throughout — allOf precedence, oneOf variant order and prefixItems
// positions are all semantic, and reversing them would change the document's
// meaning rather than only its spelling.
//
// And it proves order-independence only for the constructs its input contains,
// which is why it runs over the corpus rather than over one hand-written spec.
func orderInvariant(ctx context.Context, spec string, data []byte, doc *ir.Document) (string, bool) {
	reversed, ok := reverseMappings(data)
	if !ok {
		return "", true // not a document whose permutation is meaning-preserving
	}
	if bytes.Equal(reversed, data) {
		return "", true // nothing to permute; the oracle has no question to ask
	}
	other, otherDiags, err := compile(ctx, spec, reversed)
	if err != nil {
		return "recompile permuted: " + err.Error(), false
	}
	if other == nil {
		return "permuted source compiled to no document", false
	}
	return diffOrderInvariants(doc, other, otherDiags)
}

// diffOrderInvariants compares what a declaration-order permutation must leave
// untouched.
//
// The type registry is the whole of it. It is ID-keyed rather than ordered, so a
// permutation that changes nothing about identity changes nothing about it at
// all — while every way an interning collision shows up is a difference in it:
// a node minted at the wrong pointer, a hint kept from whichever lowering
// arrived first, a body that lost what a second declaration wrote.
//
// The rest of the document is deliberately not compared. Operations, responses
// and content types are all held in source order by invariant #7, so they
// reorder with the source by design; a diff over them would report the
// permutation working rather than a defect.
//
// The same is true of the three collections *inside* a type that a mapping
// declares — properties, pattern properties and named examples — so those are
// ordered by identity before comparing rather than left to reorder. Sorting
// rather than ignoring is what keeps the contents in the comparison: a property
// that changed shape between the two orders still differs after both sides are
// sorted, while one that merely moved does not.
func diffOrderInvariants(first, second *ir.Document, secondDiags []ir.Diagnostic) (string, bool) {
	if a, b := len(first.Types), len(second.Types); a != b {
		return fmt.Sprintf("permuted source interns %d types against %d", b, a), false
	}
	if d := cmp.Diff(first.Types, second.Types, sourceOrderedCollections()...); d != "" {
		return "type registry depends on declaration order (-as-written +reversed):\n" + d, false
	}
	if d := cmp.Diff(diagnosticSet(first.Diagnostics), diagnosticSet(secondDiags)); d != "" {
		return "diagnostics depend on declaration order (-as-written +reversed):\n" + d, false
	}
	return "", true
}

// diagnosticSet renders diagnostics as a sorted multiset of what was reported
// and where, so two orders of one document are compared on their findings rather
// than on the order they were appended in — that follows traversal order, which
// a permutation changes by design.
//
// The registry comparison above does not subsume this. A collision can leave the
// registry identical in both orders and still change what the compiler says
// about the document: reinstating the pointer collision this oracle exists for
// interns the same nine types either way while reporting two facts in one order
// against four in the other.
//
// The message text is deliberately not compared. Some messages enumerate a set
// of source keywords and list them as the author wrote them — the unmerged-branch
// residue names "type, maxLength" for one spelling and "maxLength, type" for its
// reverse — which is invariant #7's source ordering reaching the message rather
// than a lowering that depends on order. Severity, code and pointer identify the
// finding without that.
func diagnosticSet(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%d",
			d.Severity, d.Code, d.Provenance.Pointer, d.Provenance.Source))
	}
	sort.Strings(out)
	return out
}

// sourceOrderedCollections orders the collections a mapping's entry order
// decides, so comparing two permutations of one document does not report their
// own permutation.
func sourceOrderedCollections() []cmp.Option {
	return []cmp.Option{
		cmpopts.SortSlices(func(a, b ir.Property) bool { return a.ID < b.ID }),
		cmpopts.SortSlices(func(a, b ir.PatternProps) bool { return a.Pattern < b.Pattern }),
		cmpopts.SortSlices(func(a, b ir.Example) bool { return renderExample(a) < renderExample(b) }),
	}
}

// renderExample gives an example a total order for sorting. Name alone will not
// do: the singular `example` keyword carries none, so two of them would tie and
// the sort would not be a strict weak ordering.
func renderExample(e ir.Example) string {
	return fmt.Sprintf("%s\x00%s\x00%v", e.Name, e.ExternalURL, e.Value)
}

// reverseMappings returns src with the entry order of every YAML mapping
// reversed, and ok=false for a source the rewrite cannot faithfully permute: one
// that does not parse, one that will not re-encode, or one carrying a duplicate
// mapping key, whose meaning depends on the order being changed.
func reverseMappings(src []byte) ([]byte, bool) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, false
	}
	if !reverseNode(&root, 0) {
		return nil, false
	}
	out, err := encodeYAML(&root)
	if err != nil {
		return nil, false
	}
	return out, true
}

// encodeYAML re-encodes a permuted node tree. It is a package-level seam over
// yaml.Marshal that defaults to it in production, where a tree that has just
// parsed cannot fail to re-encode; tests replace it to drive that
// otherwise-unreachable defensive path, exactly as reserializeJSON is a seam
// over json.Marshal for the same reason.
var encodeYAML = yaml.Marshal

// reverseNode reverses n's entry pairs when n is a mapping, then descends. It
// reports false for a mapping with a duplicate key, and for a tree deeper than
// the bound.
//
// An alias is not descended into: its content belongs to the anchor that
// declares it, which the walk reaches at the anchor's own position, and
// following it would both reverse that mapping twice and revisit it once per
// alias.
func reverseNode(n *yaml.Node, depth int) bool {
	if n == nil || depth > maxReverseDepth {
		return n == nil
	}
	if n.Kind == yaml.AliasNode {
		return true
	}
	if n.Kind == yaml.MappingNode {
		if duplicateKeys(n) {
			return false
		}
		n.Content = reversedPairs(n.Content)
	}
	for _, child := range n.Content {
		if !reverseNode(child, depth+1) {
			return false
		}
	}
	return true
}

// reversedPairs reverses a mapping's key/value pairs, keeping each key with its
// own value. A trailing odd element cannot occur in a parsed mapping and is left
// in place rather than dropped, so a malformed tree is passed through unchanged
// instead of silently losing a node.
func reversedPairs(content []*yaml.Node) []*yaml.Node {
	pairs := len(content) / 2
	out := make([]*yaml.Node, 0, len(content))
	for i := pairs - 1; i >= 0; i-- {
		out = append(out, content[2*i], content[2*i+1])
	}
	return append(out, content[2*pairs:]...)
}

// duplicateKeys reports whether a mapping declares one key twice. Reversing such
// a mapping changes which declaration wins (#95), so the two compiles would
// differ for a reason that is not a defect.
func duplicateKeys(n *yaml.Node) bool {
	seen := make(map[string]bool, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if seen[key] {
			return true
		}
		seen[key] = true
	}
	return false
}
