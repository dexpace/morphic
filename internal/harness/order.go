// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/go-cmp/cmp"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// maxReverseDepth bounds the rewrite's descent, which counts the document node
// and every scalar. It sits one past the 1,024 levels of mappings and
// sequences the OpenAPI compiler admits, the scalars below the deepest of
// them included, so a document the rewrite stops on is one the compiler
// refuses. TestMaxReverseDepth_ReachesEveryAdmittedDocument holds it there.
const maxReverseDepth = 1025

// orderInvariant compiles a source twice, once as re-encoded and once with
// every mapping's entry order reversed, and reports what the permutation
// changed beyond source order. It reverses no sequence: an allOf's and a
// required list's are sequenceOrderInvariant's, and any other's order is
// semantic.
//
// Unlike `deterministic`, which recompiles the same bytes, permuted input
// catches a lowering that depends on which declaration reached a pointer
// first (#108, #112). Both arms compile the encoder's output
// (reencodeMappings). It declines a source it cannot permute faithfully, or
// whose re-encoding alone is refused (compiled), and proves order-independence
// only for what its input contains, at the default options.
func orderInvariant(ctx context.Context, spec string, data []byte, compiled bool) Verdict {
	baseline, why := reencodeMappings(data)
	if why != "" {
		return declined(OracleOrder, why)
	}
	reversed, why := reverseMappings(data)
	if why != "" {
		return declined(OracleOrder, why)
	}
	if bytes.Equal(reversed, baseline) {
		return declined(OracleOrder, DeclineNothingToPermute)
	}
	return comparePermuted(ctx, OracleOrder, spec, baseline, reversed, compiled, diffOrderInvariants)
}

// comparePermuted is o's verdict on a source whose re-encoding is baseline and
// whose permutation is permuted: it compiles both and holds them to diff. It
// declines a baseline no compiler takes, or one refused where the source
// compiled.
func comparePermuted(ctx context.Context, o Oracle, spec string, baseline, permuted []byte, compiled bool,
	diff func(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic) (string, bool),
) Verdict {
	doc, diags, err := compile(ctx, spec, baseline, passes())
	if errors.Is(err, errUnrecognized) {
		return declined(o, DeclineReencodingNoSpec)
	}
	if err != nil {
		// A Go error is never the spec's, so it is reported, as on the other arm.
		return found(o, "compile re-encoded: "+err.Error())
	}
	if doc == nil && compiled {
		return declined(o, DeclineReencodingRefused)
	}
	other, otherDiags, err := compile(ctx, spec, permuted, passes())
	if err != nil {
		return found(o, "recompile permuted: "+err.Error())
	}
	return o.judge(diff(doc, diags, other, otherDiags))
}

// diffOrderInvariants compares what a declaration-order permutation must leave
// untouched: the whole document, and the diagnostics as a multiset. A refusal
// returns no document, so two refusals compare their diagnostics alone, and a
// refusal in one order only is a finding.
//
// IDs derive from pointers, so a permutation leaves every ID and registry key
// unchanged, while an interning collision shows as a difference: a node minted
// at the wrong pointer, a hint kept from whichever lowering came first, a body
// that lost what a second declaration wrote. What the permutation reorders by
// design is compared regardless of order (canonicalJSON).
func diffOrderInvariants(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic) (string, bool) {
	if refused := oneRefusal(first, second); refused != "" {
		return refused, false
	}
	if first != nil {
		if a, b := len(first.Types), len(second.Types); a != b {
			return fmt.Sprintf("permuted source interns %d types against %d", b, a), false
		}
		if detail, ok := sameDocument(first, second); !ok {
			return detail, false
		}
	}
	if d := cmp.Diff(diagnosticSet(firstDiags), diagnosticSet(secondDiags)); d != "" {
		return "diagnostics depend on declaration order (-as-written +reversed):\n" + d, false
	}
	return "", true
}

// oneRefusal says which of two compiles, the source as written and its
// permutation, alone returned no document, and is empty when both or neither
// did.
func oneRefusal(first, second *ir.Document) string {
	switch {
	case first == nil && second != nil:
		return "the permuted source compiles to a document, and as written to none"
	case first != nil && second == nil:
		return "the permuted source compiles to no document, and as written to one"
	default:
		return ""
	}
}

// sameDocument compares two documents' canonical encodings, and reports the
// lines the second changes.
func sameDocument(first, second *ir.Document) (string, bool) {
	a, err := canonicalJSON(first)
	if err != nil {
		return "encode as written: " + err.Error(), false
	}
	b, err := canonicalJSON(second)
	if err != nil {
		return "encode permuted: " + err.Error(), false
	}
	if !bytes.Equal(a, b) {
		return "document depends on declaration order (-as-written +reversed):\n" + cmp.Diff(lines(a), lines(b)), false
	}
	return "", true
}

// diagnosticSet renders diagnostics as a sorted multiset of what was reported
// and where, so two orders of one document compare on their findings rather
// than on append order, which follows traversal order.
//
// The document comparison does not subsume this: a collision can leave the
// document identical yet change what the compiler reports.
//
// Message text is not compared: some messages list source keywords in the
// author's order ("type, maxLength" against "maxLength, type"), which is
// invariant #7 reaching the message. Of its provenance, only line and column
// are left out, which a permutation moves by design, keeping whether it has any.
func diagnosticSet(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, diagnosticKey(d))
	}
	slices.Sort(out)
	return out
}

// diagnosticKey is what diagnosticSet compares of one diagnostic.
func diagnosticKey(d ir.Diagnostic) string {
	p := d.Provenance
	positioned := p.Position != ir.Position{}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%t\x00%s\x00%d\x00%d\x00%s",
		d.Severity, d.Code, p.Pointer, positioned, p.Node, p.Source, p.WrittenBy, p.Inferred)
}

// lines splits an encoding into its lines, so a difference reads line by line.
func lines(encoded []byte) []string { return strings.Split(string(encoded), "\n") }

// canonicalJSON encodes doc as the permutation must leave it, indented so a
// difference reads line by line. It leaves out the diagnostics, compared apart
// (diagnosticSet), and the hash of the source compiled, the first of its
// table, whose bytes the permutation rewrote; another source is the same file
// in both orders.
//
// Each collection a mapping's entry order decides is written in one order
// (orderFree), and a verbatim JSON value's object members are sorted, so a
// changed element still differs and a moved one does not. A collection a
// sequence orders, such as an enum's members, keeps its order.
func canonicalJSON(doc *ir.Document) ([]byte, error) {
	return encodeComparable(doc, sortedArray, jsontext.WithIndent("  "))
}

// encodeComparable encodes doc without its diagnostics and its compiled
// source's hash, as canonicalJSON describes, with each collection orderFree
// lists written by arrange.
func encodeComparable(doc *ir.Document, arrange func(elems [][]byte) []byte, opts ...json.Options) ([]byte, error) {
	type plain ir.Document // no methods, so the options below reach it
	own := plain(*doc)
	own.Diagnostics = nil
	if len(own.Sources) > 0 {
		own.Sources = slices.Clone(own.Sources)
		own.Sources[0].Hash = ""
	}
	return json.Marshal(&own, append([]json.Options{json.Deterministic(true),
		json.WithMarshalers(orderFree(arrange)), jsontext.ReorderRawObjects(true)}, opts...)...)
}

// orderFree writes the collections a mapping's entry order decides, each one
// the corpus's two orders tell apart by design, as arrange writes their
// elements' own encodings: sorted, or as they stand for a later rewrite to
// sort. Sorting needs no key, so elements carrying none unique compare as the
// multiset they are: an element's encoding sorts its own collections first, so
// two whose members the permutation reversed still pair up (#683). Examples
// alone hold both a mapping's entries and a sequence's elements
// (sortedExamples).
func orderFree(arrange func(elems [][]byte) []byte) *json.Marshalers {
	return json.JoinMarshalers(
		sortedElements[ir.Property](arrange),
		sortedElements[ir.Header](arrange),
		// The service tree's elements are read by value, never through a generic
		// of this module, whose writes the lint keeping them in ir cannot read.
		json.MarshalToFunc(func(enc *jsontext.Encoder, v []ir.Operation) error {
			return writeOrderFree(enc, len(v), func(i int) any { return v[i] }, arrange)
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, v []ir.OperationGroup) error {
			return writeOrderFree(enc, len(v), func(i int) any { return v[i] }, arrange)
		}),
		sortedElements[ir.PatternProps](arrange),
		sortedElements[ir.Field](arrange),
		sortedElements[ir.ServerVariable](arrange),
		sortedElements[ir.OAuthScope](arrange),
		json.MarshalToFunc(func(enc *jsontext.Encoder, v []ir.Example) error {
			return sortedExamples(enc, v, arrange)
		}),
		sortedElements[ir.Content](arrange),
		sortedElements[ir.Response](arrange),
		sortedElements[ir.ErrorCase](arrange),
		sortedElements[ir.OperationLink](arrange),
		sortedElements[ir.LinkParam](arrange),
		sortedElements[ir.Callback](arrange),
		sortedElements[ir.SchemeUse](arrange),
		json.MarshalToFunc(sortedRequestContentTypes),
	)
}

// sortedElements writes a []T as arrange writes its elements' encodings
// (writeOrderFree).
func sortedElements[T any](arrange func(elems [][]byte) []byte) *json.Marshalers {
	return json.MarshalToFunc(func(enc *jsontext.Encoder, v []T) error {
		return writeOrderFree(enc, len(v), func(i int) any { return &v[i] }, arrange)
	})
}

// sortedExamples writes a list of examples holding a named one, an `examples`
// mapping's entry, as arrange writes it (writeOrderFree), and a list of unnamed
// ones in order. An unnamed example is the `example` keyword's value or an
// element of a schema's `examples` array, which no mapping orders. A mapping
// entry keyed by the empty string is unnamed too, so a list holding a named one
// is arranged whole, unnamed ones among it included.
func sortedExamples(enc *jsontext.Encoder, v []ir.Example, arrange func(elems [][]byte) []byte) error {
	if !slices.ContainsFunc(v, func(e ir.Example) bool { return e.Name != "" }) {
		return errors.ErrUnsupported
	}
	return writeOrderFree(enc, len(v), func(i int) any { return &v[i] }, arrange)
}

// writeOrderFree writes the n elements at gives as arrange writes their
// encodings, each encoded under the options writing the collection, these
// marshalers among them. An empty collection is written as it would be
// otherwise.
func writeOrderFree(enc *jsontext.Encoder, n int, at func(i int) any, arrange func(elems [][]byte) []byte) error {
	if n == 0 {
		return errors.ErrUnsupported
	}
	elems := make([][]byte, n)
	for i := range n {
		b, err := json.Marshal(at(i), enc.Options())
		if err != nil {
			return err
		}
		elems[i] = b
	}
	return enc.WriteValue(arrange(elems))
}

// sortedArray is an array of elems in the byte order of their encodings.
func sortedArray(elems [][]byte) []byte {
	slices.SortFunc(elems, bytes.Compare)
	return array(elems)
}

// array is an array of elems as they stand.
func array(elems [][]byte) []byte {
	return append(append([]byte{'['}, bytes.Join(elems, []byte{','})...), ']')
}

// sortedRequestContentTypes writes an HTTP binding with its request's content
// types sorted: the request body's content map orders them. Every other []string
// of the IR keeps its order, so this one alone is sorted, here.
func sortedRequestContentTypes(enc *jsontext.Encoder, b ir.HTTPBinding) error {
	type plain ir.HTTPBinding // no methods, so this function does not reach it again
	b.RequestContentTypes = slices.Sorted(slices.Values(b.RequestContentTypes))
	return json.MarshalEncode(enc, (*plain)(&b))
}

// maxPermutedDocuments bounds the documents of a stream the rewrite reads. A
// stream holding more is declined rather than permuted in part, since a
// compile of the part would answer for another source.
const maxPermutedDocuments = 1024

// reencodeMappings returns src parsed and re-encoded with its entry order
// intact, every document of its stream: the same normalization reverseMappings
// applies, minus the permutation. It is what the permuted source is compared
// against, so a spelling the encoder rewrites (a flow-style implicit null comes
// back as an empty string) changes both sides alike. A Decline says why there is
// no re-encoding; it is empty when there is one.
func reencodeMappings(src []byte) ([]byte, Decline) {
	docs, why := parseStream(src)
	if why != "" {
		return nil, why
	}
	out, err := encodeYAML(docs)
	if err != nil {
		return nil, DeclineUnencodable
	}
	return out, ""
}

// reverseMappings returns src with the entry order of every YAML mapping
// reversed, in every document of its stream, or why the rewrite cannot permute
// it faithfully: it does not parse, declares a key twice in one mapping, whose
// meaning depends on the order changed, gives two nodes one anchor name, nests
// too deep, or will not re-encode.
//
// Reversing can carry an alias above its anchor, which YAML forbids, so each
// anchor moves to the first place naming it (hoistAnchors). A permutation that
// does not parse again is declined, as the rewrite's fault, not the compiler's.
func reverseMappings(src []byte) ([]byte, Decline) {
	docs, why := parseStream(src)
	if why != "" {
		return nil, why
	}
	for _, doc := range docs {
		if why := reverseNode(doc, 0); why != "" {
			return nil, why
		}
		if why := hoistAnchors(doc); why != "" {
			return nil, why
		}
	}
	out, err := encodeYAML(docs)
	if err != nil {
		return nil, DeclineUnencodable
	}
	if _, why := parseStream(out); why != "" {
		return nil, DeclinePermutationUnparsable
	}
	return out, ""
}

// parseStream parses src as a YAML stream, a node per document, or says why
// it cannot: it does not parse, or holds more documents than the rewrite reads.
func parseStream(src []byte) ([]*yaml.Node, Decline) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var docs []*yaml.Node
	for range maxPermutedDocuments + 1 {
		doc := &yaml.Node{}
		err := dec.Decode(doc)
		if errors.Is(err, io.EOF) {
			return docs, ""
		}
		if err != nil {
			return nil, DeclineNotYAML
		}
		docs = append(docs, doc)
	}
	return nil, DeclineTooManyDocuments
}

// encodeYAML encodes a parsed or permuted stream. It is a package-level seam:
// a tree that has just parsed cannot fail to re-encode, nor encode to text that
// does not parse, and tests replace it to drive those otherwise-unreachable
// defensive paths, as reserializeJSON is a seam over json.Marshal for the same
// reason.
var encodeYAML = func(docs []*yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeStream(&buf, docs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeStream writes docs to w as one YAML stream, a document each. A stream
// of no documents writes nothing, where the encoder would refuse to close.
func encodeStream(w io.Writer, docs []*yaml.Node) error {
	if len(docs) == 0 {
		return nil
	}
	enc := yaml.NewEncoder(w)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return err
		}
	}
	return enc.Close()
}

// reverseNode reverses n's entry pairs when n is a mapping, then descends. It
// declines a mapping with a duplicate key, and a tree deeper than the bound.
//
// An alias needs no case of its own. It holds its target in Alias rather than in
// Content, so the descent below reaches nothing through it and the anchored
// mapping is reversed exactly once, at the anchor's own position.
// TestReverseMappings_AliasIsNotFollowed pins the resulting shape.
func reverseNode(n *yaml.Node, depth int) Decline {
	if n == nil {
		return ""
	}
	if depth > maxReverseDepth {
		return DeclineTooDeep
	}
	if n.Kind == yaml.MappingNode {
		if duplicateKeys(n) {
			return DeclineDuplicateKey
		}
		n.Content = reversedPairs(n.Content)
	}
	for _, child := range n.Content {
		if why := reverseNode(child, depth+1); why != "" {
			return why
		}
	}
	return ""
}

// slot is a place in a node tree: the index'th of parent's Content.
type slot struct {
	parent *yaml.Node
	index  int
}

// hoistAnchors moves each anchored node of doc to the first place that names
// it, in the order an encoder writes the tree, and leaves an alias where it
// was. Which occurrence carries the anchor means nothing in YAML, but an alias
// written before its anchor does not parse, and reversing mappings writes them
// so. It declines a document giving one anchor name to two nodes, since the
// encoder writes an alias by name and a move could rebind it, and one nested
// past the bound.
func hoistAnchors(doc *yaml.Node) Decline {
	anchored := map[*yaml.Node]slot{}
	named := map[string]*yaml.Node{}
	if why := findAnchors(doc, 0, anchored, named); why != "" {
		return why
	}
	if len(anchored) == 0 {
		return ""
	}
	return hoistFirstUses(doc, 0, anchored, map[*yaml.Node]bool{})
}

// findAnchors records, below n, where each anchored node sits, and declines a
// tree giving one name to two nodes or nested past the bound.
func findAnchors(n *yaml.Node, depth int, anchored map[*yaml.Node]slot, named map[string]*yaml.Node) Decline {
	if depth > maxReverseDepth {
		return DeclineTooDeep
	}
	for i, child := range n.Content {
		if child.Anchor != "" {
			if other, ok := named[child.Anchor]; ok && other != child {
				return DeclineAnchorRedefined
			}
			named[child.Anchor], anchored[child] = child, slot{parent: n, index: i}
		}
		if why := findAnchors(child, depth+1, anchored, named); why != "" {
			return why
		}
	}
	return ""
}

// hoistFirstUses walks below n in the order an encoder writes it. An alias met
// before the node it names trades places with that node, which then sits where
// it is first named, and the walk goes on into it there. anchored tracks where
// each anchored node sits, and written holds those already passed.
func hoistFirstUses(n *yaml.Node, depth int, anchored map[*yaml.Node]slot, written map[*yaml.Node]bool) Decline {
	if depth > maxReverseDepth {
		return DeclineTooDeep
	}
	for i := range n.Content {
		child := n.Content[i]
		if child.Kind == yaml.AliasNode && !written[child.Alias] {
			target, at := child.Alias, anchored[child.Alias]
			at.parent.Content[at.index], n.Content[i] = child, target
			anchored[target] = slot{parent: n, index: i}
			child = target
		}
		if child.Anchor != "" {
			written[child] = true
		}
		if why := hoistFirstUses(child, depth+1, anchored, written); why != "" {
			return why
		}
	}
	return ""
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

// duplicateKeys reports whether a mapping declares one key twice, each key read
// as the parser reads it: an alias stands for the node it names, whose Value is
// empty when that is no scalar (docs/parser-library.md#non-scalar-key). Reversing
// such a mapping changes which declaration wins (#95), so the two compiles would
// differ for a reason that is not a defect. It compares text alone, so a `<<`
// merge key beside a quoted or aliased `<<`, which the parser reads apart, is
// declined too, leaving that source unchecked rather than misjudged.
func duplicateKeys(n *yaml.Node) bool {
	seen := make(map[string]bool, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Kind == yaml.AliasNode {
			key = key.Alias // a parsed alias names its anchor, and that is no alias
		}
		if seen[key.Value] {
			return true
		}
		seen[key.Value] = true
	}
	return false
}
