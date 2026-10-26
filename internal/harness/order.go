package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// maxReverseDepth bounds the rewrite's descent, which counts the document node
// and every scalar. It sits one past the 1,024 levels of mappings and
// sequences the OpenAPI compiler admits, the scalars below the deepest of
// them included, so a document the rewrite stops on is one the compiler
// refuses. TestMaxReverseDepth_ReachesEveryAdmittedDocument holds it there.
const maxReverseDepth = 1025

// orderInvariant compiles a source twice, once with its mappings as declared
// and once with every mapping's entry order reversed, and reports what the
// permutation changed beyond source order. Sequences are not reversed: their
// order is semantic.
//
// Unlike `deterministic`, which recompiles the same bytes, permuted input
// catches a lowering that depends on which declaration reached a pointer
// first (#108, #112). Both arms compile the encoder's output (see
// reencodeMappings). A source whose reversal changes its meaning, or whose
// re-encoding yields no document, is excluded rather than reported. It proves
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
	doc, _, err := compile(ctx, spec, baseline, passes())
	if errors.Is(err, errUnrecognized) {
		return "", true // the re-encoding reads as no spec, so there is no baseline
	}
	if err != nil {
		// A Go error is never the spec's, so it is reported, as on the other arm.
		return "compile re-encoded: " + err.Error(), false
	}
	if doc == nil {
		return "", true // nor does a re-encoding the compiler declines outright
	}
	other, otherDiags, err := compile(ctx, spec, reversed, passes())
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
// The collections whose order follows the source's (sourceOrderedCollections)
// are compared regardless of order, not ignored: a changed property still
// differs, a moved one does not.
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
	slices.Sort(out)
	return out
}

// sourceOrderedCollections makes the comparison blind to the order of the
// collections a mapping's entry order decides, so two permutations of one
// document do not differ by their own permutation.
//
// Each is sorted by what identifies its elements. An object value's members are among them: the
// rewrite reverses a default's or an example's mapping like any other, and
// Value.Object keeps source order on purpose (#683). A member's name is unique,
// because reverseMappings refuses a mapping that declares a key twice.
//
// Examples have no identity, so sameExamples matches them under the keyed
// options: an example holds no list of examples to need more.
func sourceOrderedCollections() []cmp.Option {
	keyed := cmp.Options{
		cmpopts.SortSlices(func(a, b ir.Property) bool { return a.ID < b.ID }),
		cmpopts.SortSlices(func(a, b ir.PatternProps) bool { return a.Pattern < b.Pattern }),
		cmpopts.SortSlices(func(a, b ir.Field) bool { return a.Name < b.Name }),
	}
	return []cmp.Option{keyed, cmp.Comparer(func(a, b []ir.Example) bool { return sameExamples(a, b, keyed) })}
}

// sameExamples reports whether a and b hold equal examples, each as often, in
// any order. Name cannot sort them, since the singular `example` keyword
// carries none. A rendering could, but it would be a second reading of
// equality, and one taken as written pairs two unnamed examples crosswise once
// the permutation reverses their members. Matching under cmp.Equal leaves
// nothing to drift from the comparison itself.
//
// Equal examples share a name, so each is sought only among b's of its name,
// which keeps matching a reversed list of named examples linear.
func sameExamples(a, b []ir.Example, opts ...cmp.Option) bool {
	if len(a) != len(b) || (a == nil) != (b == nil) {
		return false
	}
	unmatched := make(map[string][]int, len(b))
	for j, y := range b {
		unmatched[y.Name] = append(unmatched[y.Name], j)
	}
	for _, x := range a {
		js := unmatched[x.Name]
		i := slices.IndexFunc(js, func(j int) bool { return cmp.Equal(x, b[j], opts...) })
		if i < 0 {
			return false
		}
		unmatched[x.Name] = slices.Delete(js, i, i+1)
	}
	return true
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
