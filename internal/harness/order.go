package harness

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

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

// positionPlaceholder stands in for a provenance pointer that locates a
// construct by source position. The leading control byte is what makes it a
// placeholder rather than a value: no producer spells a pointer with one, so it
// cannot collide with a pointer that means itself.
const positionPlaceholder = "\x01position"

// orderInvariant compiles a source twice — once with its mappings as declared,
// once with every mapping's entry order reversed — and reports what the
// permutation changed beyond source order.
//
// It is the general form of the two-order diff CLAUDE.md prescribes.
// `deterministic`, next to it, looks like this and is a different property: it
// recompiles the *same bytes*, proving same-input-same-output. Only permuted
// input catches a lowering whose result depends on which declaration reached a
// pointer first — the shape of the pointer collisions in #108 and #112, which
// produce no diagnostic in either order and leave pass/validate clean on both.
//
// Both arms are compiled from the encoder's output rather than one from the
// source and one from the rewrite. The encoder does not preserve every spelling
// — a flow-style implicit null comes back carrying an empty string — and
// comparing against the source would read that rewriting as a lowering that
// depends on order. Passing both sides through it leaves declaration order as
// the only difference between them, which is the question being asked.
//
// The limits belong here so the oracle is not over-trusted. Reversing a mapping
// is meaning-preserving only while its keys are distinct: duplicate keys resolve
// to the last declaration (#95), so reversing changes which one wins and the two
// compiles legitimately differ. Such a source is excluded rather than reported,
// as is one whose permutation no longer parses — see reverseMappings — and one
// whose re-encoding will not compile, which leaves nothing faithful to compare
// against. Sequences are left alone throughout: allOf precedence, oneOf variant
// order and prefixItems positions are all semantic, and reversing them would
// change the document's meaning rather than only its spelling.
//
// And it proves order-independence only for the constructs its input contains,
// which is why it runs over the corpus rather than over one hand-written spec.
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
//
// A pointer spelled line:col is excluded for the same reason. Provenance.Pointer
// admits either a structural pointer or a source position, and a permutation
// moves a construct to a different line by design. Replacing it rather than
// dropping the field keeps the finding in the multiset, so a permutation that
// changes how many were reported still shows.
func diagnosticSet(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		pointer := d.Provenance.Pointer
		if isSourcePosition(pointer) {
			pointer = positionPlaceholder
		}
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s\x00%d",
			d.Severity, d.Code, pointer, d.Provenance.Source))
	}
	sort.Strings(out)
	return out
}

// isSourcePosition reports whether a provenance pointer is a line:col position
// rather than a structural pointer.
func isSourcePosition(pointer string) bool {
	line, col, ok := strings.Cut(pointer, ":")
	return ok && isDigits(line) && isDigits(col)
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
// rewrites changes both sides alike.
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
