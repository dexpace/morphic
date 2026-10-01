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

// orderInvariant compiles a source twice, once with its mappings as declared
// and once with every mapping's entry order reversed, and reports what the
// permutation changed beyond source order. Sequences are not reversed: their
// order is semantic.
//
// Unlike `deterministic`, which recompiles the same bytes, permuted input
// catches a lowering that depends on which declaration reached a pointer
// first (#108, #112). Both arms compile the encoder's output (see
// reencodeMappings). A source whose reversal changes its meaning, or whose
// re-encoding does not compile, is excluded rather than reported. It proves
// order-independence only for the constructs its input contains, so it runs
// over the corpus.
func orderInvariant(ctx context.Context, spec string, data []byte) (string, bool) {
	baseline, ok := reencodeMappings(data)
	if !ok {
		return "", true // the source does not survive a parse and re-encode
	}
	reversed, ok := reverseMappings(data)
	if !ok {
		return "", true // its permutation would not be meaning-preserving
	}
	if bytes.Equal(reversed, baseline) {
		return "", true // nothing to permute; the oracle has no question to ask
	}
	doc, _, err := compile(ctx, spec, baseline)
	if err != nil {
		return "", true // the re-encoding does not compile, so there is no baseline
	}
	if doc == nil {
		return "", true // nor does a source the compiler declines outright
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
// untouched: the type registry and the diagnostics.
//
// The registry is ID-keyed, so a permutation alone leaves it unchanged, while
// every interning collision shows up as a difference in it: a node minted at
// the wrong pointer, a hint kept from whichever lowering arrived first, a body
// that lost what a second declaration wrote. Operations, responses and content
// types follow source order (invariant #7), so are not compared.
//
// Properties, pattern properties and named examples inside a type are sorted
// by identity, not ignored: a changed property still differs, a moved one does
// not.
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
// and where, so two orders of one document compare on their findings rather
// than on append order, which follows traversal order.
//
// The registry comparison does not subsume this: a collision can leave the
// registry identical yet change what the compiler reports.
//
// Message text is not compared: some messages list source keywords in the
// author's order ("type, maxLength" against "maxLength, type"), which is
// invariant #7 reaching the message. Source position is left out because a
// permutation moves constructs by design; whether a finding has one stays in.
func diagnosticSet(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		p := d.Provenance
		positioned := p.Position != ir.Position{}
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%t\x00%s\x00%d",
			d.Severity, d.Code, p.Pointer, positioned, p.Node, p.Source))
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

// reencodeMappings returns src parsed and re-encoded with its entry order
// intact: the same normalization reverseMappings applies, minus the permutation.
// It is what the permuted source is compared against, so a spelling the encoder
// rewrites (a flow-style implicit null comes back as an empty string) changes
// both sides alike.
func reencodeMappings(src []byte) ([]byte, bool) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, false
	}
	out, err := encodeYAML(&root)
	if err != nil {
		return nil, false
	}
	return out, true
}

// reverseMappings returns src with the entry order of every YAML mapping
// reversed, and ok=false for a source the rewrite cannot faithfully permute: one
// that does not parse, one that will not re-encode, one carrying a duplicate
// mapping key, whose meaning depends on the order being changed, or one whose
// permutation no longer parses.
//
// That last case is the rewrite's own doing rather than a fact about the
// compiler: reversing a mapping can carry an alias above the anchor it names,
// which YAML forbids. Re-parsing catches it without enumerating it, and covers
// any later ordering rule of the same kind.
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
	var check yaml.Node
	if err := yaml.Unmarshal(out, &check); err != nil {
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
// An alias needs no case of its own. It holds its target in Alias rather than in
// Content, so the descent below reaches nothing through it and the anchored
// mapping is reversed exactly once, at the anchor's own position — which is what
// a guard here would have had to arrange, for a state the parser does not
// produce. TestReverseMappings_AliasIsNotFollowed pins the resulting shape.
func reverseNode(n *yaml.Node, depth int) bool {
	if n == nil || depth > maxReverseDepth {
		return n == nil
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
