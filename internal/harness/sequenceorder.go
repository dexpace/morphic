// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// sequenceOrderInvariant compiles a source twice, once as re-encoded and once
// with its allOf and required sequences reversed (reverseSequences), and
// reports what the reversal changed beyond which element of a reversed
// sequence a pointer names (diffSequenceInvariants). It declines as
// orderInvariant does, and a source whose compile reads another file. Of a
// sequence of three it tries one other order, and it compiles at the default
// options alone.
func sequenceOrderInvariant(ctx context.Context, spec string, data []byte, compiled bool) Verdict {
	baseline, why := reencodeMappings(data)
	if why != "" {
		return declined(OracleSequenceOrder, why)
	}
	reversed, perm, why := reverseSequences(data)
	if why != "" {
		return declined(OracleSequenceOrder, why)
	}
	if bytes.Equal(reversed, baseline) {
		return declined(OracleSequenceOrder, DeclineNothingToPermute)
	}
	return comparePermuted(ctx, OracleSequenceOrder, spec, baseline, reversed, compiled,
		func(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic) Verdict {
			detail, ok, why := diffSequenceInvariants(first, firstDiags, second, secondDiags, perm)
			if why != "" {
				return declined(OracleSequenceOrder, why)
			}
			return OracleSequenceOrder.judge(detail, ok)
		})
}

// diffSequenceInvariants compares what reversing the sequences of perm must
// leave untouched: the diagnostics, the document and the flattened shapes,
// in their sources' coordinates. A tie is a member two reports alike but
// for their places decide between two declarations, the other way round in
// each order (tiedBy), or what a moved report decided. Of a node a tie names,
// or one at or beneath a moved report's site or a tied member's fold mints
// (rewriter.minted), only what a property's fold settles from every
// declaration alike is compared (rewriter.foldedOf, rewriter.statedOf). It
// declines as compareForms does, and several files.
func diffSequenceInvariants(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic, perm permutation) (string, bool, Decline) {
	if first != nil && len(first.Sources) > 1 {
		return "", false, DeclineReadsAnotherFile
	}
	stated, a := factsOf(reportsOf(firstDiags, nil, perm.written), perm.written)
	restated, b := factsOf(reportsOf(secondDiags, perm.moved, perm.written), perm.written)
	ties, left, right := movedReports(a, b, perm.written)
	tied, moved := tiedBy(stated, restated), ties
	ties.nodes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(ties.nodes, tied))))
	ties = settledFrom(settledFrom(ties, first, perm.written), second, perm.moved)
	ties.stated = statedAlone(statedAlone(tied, moved, ties.nodes, first, perm.written), moved, ties.nodes, second, perm.moved)
	left, right = untied(left, ties, perm.written), untied(right, ties, perm.written)
	refused := oneRefusal(first, second)
	var forms []string
	if refused == "" && first != nil {
		var why Decline
		left, right, forms, why = compareForms(first, second, perm, ties, left, right)
		if why != "" {
			return "", false, why
		}
	}
	written, reversed := heldBy(first, perm.written, nil), heldBy(second, perm.moved, perm.moved)
	onlyStated, onlyRestated := unpairedFacts(keptTypes(stated, written), keptTypes(restated, reversed), ties.nodes)
	var found []string
	if unpaired := renderUnpaired(append(left, onlyStated...), append(right, onlyRestated...)); unpaired != "" {
		found = append(found, "diagnostics depend on sequence order (-as-written +reversed):\n"+unpaired)
	}
	if refused != "" {
		found = append(found, refused)
	}
	found = append(found, forms...)
	if wrong := contradicted(written, reversed, stated, tied); len(wrong) > 0 {
		found = append(found, "a tie's reports name the declaration the member takes as the one that lost:\n"+strings.Join(wrong, "\n"))
	}
	return strings.Join(found, "\n"), len(found) == 0, ""
}

// compareForms compares two documents' canonical forms under ties
// (canonicalForm), each in its own source's coordinates, line by line, and
// their flattened shapes, and pairs what left and right, the reports only one
// order makes, say of one node by what the forms read (sameNodes). It
// declines where either form nests past its bound or meets a cycle of digests.
func compareForms(first, second *ir.Document, perm permutation, ties tieSites, left, right []report) ([]report, []report, []string, Decline) {
	a, err := canonicalForm(first, perm.written, ties)
	if why := formDecline(err); why != "" {
		return nil, nil, nil, why
	}
	if err != nil {
		return left, right, []string{"encode as written: " + err.Error()}, ""
	}
	b, err := canonicalForm(second, perm.moved, ties)
	if why := formDecline(err); why != "" {
		return nil, nil, nil, why
	}
	if err != nil {
		return left, right, []string{"encode permuted: " + err.Error()}, ""
	}
	left, right = sameNodes(left, right, a.identify, b.identify)
	var found []string
	if !slices.Equal(a.document, b.document) {
		found = append(found, "document depends on sequence order (-as-written +reversed):\n"+lineDifference(a.document, b.document))
	}
	if !slices.Equal(a.shape, b.shape) {
		found = append(found, "flattened shape depends on sequence order (-as-written +reversed):\n"+lineDifference(a.shape, b.shape))
	}
	return left, right, found, ""
}

// sameNodes returns left and right without each pair saying one thing of one
// node: a node whose ID the reversal moves only by which element of a reversed
// sequence it names, as a fold mints one at the declaration written later in
// either order, is one where each form tells it alike by what it holds
// (rewriter.identify), as a reference to it is. first and second identify a
// node in the coordinates of the compile its report came from.
func sameNodes(left, right []report, first, second func(string) (string, bool)) ([]report, []report) {
	rightKeys := make([]string, len(right))
	unmatched := map[string]int{}
	for i, report := range right {
		key := nodeKey(report, second)
		rightKeys[i] = key
		if key != "" {
			unmatched[key]++
		}
	}
	paired := map[string]int{}
	var keptLeft []report
	for _, report := range left {
		key := nodeKey(report, first)
		if key != "" && unmatched[key] > 0 {
			unmatched[key]--
			paired[key]++
			continue
		}
		keptLeft = append(keptLeft, report)
	}
	keptRight := make([]report, 0, len(right))
	for i, report := range right {
		if key := rightKeys[i]; key != "" && paired[key] > 0 {
			paired[key]--
			continue
		}
		keptRight = append(keptRight, report)
	}
	return keptLeft, keptRight
}

// nodeKey is reportValue's key with its node as identify writes it and no pointer, as a
// report naming a node is erased (reportsOf), or empty, pairing with nothing,
// where reportValue names no node or identify cannot tell the node by what it holds.
func nodeKey(reportValue report, identify func(string) (string, bool)) string {
	if reportValue.raw.Provenance.Node == "" {
		return ""
	}
	node, ok := identify(reportValue.raw.Provenance.Node)
	if !ok {
		return ""
	}
	diagnostic := reportValue.raw
	diagnostic.Provenance.Pointer, diagnostic.Provenance.Node = "", node
	return diagnosticKey(diagnostic)
}

// formDecline is the decline a canonical form's error stands for, if any.
func formDecline(err error) Decline {
	switch {
	case errors.Is(err, errTooDeep):
		return DeclineTooDeep
	case errors.Is(err, errCycle):
		return DeclineReferenceCycle
	default:
		return ""
	}
}

// report is one diagnostic as the comparison reads it, in the coordinates of
// the source as written: as diagnosticKey renders it, the same with every
// index of a reversed sequence erased, and with no pointer where it names a
// node, its pointer and its node; rebased, the diagnostic in those
// coordinates, what else it concerns too; and raw, the diagnostic in its own
// compile's coordinates. A report naming the node it decided is told apart by that
// node: it sits at the declaration the order made lose, which a nested allOf
// can place at another depth.
type report struct {
	key, erased, pointer, node string
	rebased, raw               ir.Diagnostic
}

// tieSites is what the reports a reversal moved decided: the erased pointer of
// each, at or beneath which nothing is compared, the erased node of each naming
// one (ir.Provenance's Node) or of each member a report states a tie decided
// (tiedBy), which is not compared wherever it sits, the erased pointer of each
// tied member (settledFrom), at or beneath which only what its fold mints is
// left out (rewriter.minted), the models holding a member a tie decided, whose
// reports follow it, and the tied members a stated tie alone decided
// (statedAlone), whose docs are compared.
type tieSites struct{ pointers, nodes, members, holders, stated []string }

// reportsOf reads diags, rebasing each pointer and node from the coordinates
// of from, when given, to those of the source as written, where written lists
// the reversed sequences.
func reportsOf(diags []ir.Diagnostic, from, written sequences) []report {
	out := make([]report, 0, len(diags))
	for _, diagnostic := range diags {
		raw := diagnostic
		diagnostic.Provenance.Pointer = jsontext.Pointer(rebase(string(diagnostic.Provenance.Pointer), from))
		diagnostic.Provenance.Node = rebase(diagnostic.Provenance.Node, from)
		diagnostic.Related = slices.Clone(diagnostic.Related)
		for i := range diagnostic.Related {
			provenance := &diagnostic.Related[i].Provenance
			provenance.Pointer, provenance.Node = jsontext.Pointer(rebase(string(provenance.Pointer), from)), rebase(provenance.Node, from)
		}
		irDiagnostic := diagnostic
		irDiagnostic.Provenance.Pointer = jsontext.Pointer(erase(string(diagnostic.Provenance.Pointer), written))
		irDiagnostic.Provenance.Node = erase(diagnostic.Provenance.Node, written)
		if irDiagnostic.Provenance.Node != "" {
			irDiagnostic.Provenance.Pointer = ""
		}
		irDiagnostic.Related = slices.Clone(diagnostic.Related)
		for i := range irDiagnostic.Related {
			provenance := &irDiagnostic.Related[i].Provenance
			provenance.Pointer, provenance.Node = jsontext.Pointer(erase(string(provenance.Pointer), written)), erase(provenance.Node, written)
		}
		out = append(out, report{key: diagnosticKey(diagnostic), erased: diagnosticKey(irDiagnostic), pointer: string(diagnostic.Provenance.Pointer),
			node: diagnostic.Provenance.Node, rebased: diagnostic, raw: raw})
	}
	return out
}

// movedReports pairs what one compile reports, a, and the other, b, does not, a
// report of each equal as report.erased reads them and differing in their
// pointers, which is a report the reversal moved: its erased pointer is a tie
// site, and its erased node, where it names one, a tied node. Pairs match
// greedily, in key order; it returns what stays unpaired as written and
// reversed.
func movedReports(a, b []report, written sequences) (ties tieSites, left, right []report) {
	onlyFirst, onlySecond := difference(a, b)
	candidates := map[string][]report{}
	for _, b := range onlySecond {
		candidates[b.erased] = append(candidates[b.erased], b)
	}
	for _, a := range onlyFirst {
		i := slices.IndexFunc(candidates[a.erased], func(b report) bool { return b.pointer != a.pointer })
		if i < 0 {
			left = append(left, a)
			continue
		}
		candidates[a.erased] = slices.Delete(candidates[a.erased], i, i+1)
		ties.pointers = append(ties.pointers, erase(a.pointer, written))
		if a.node != "" {
			ties.nodes = append(ties.nodes, erase(a.node, written))
		}
	}
	for _, rest := range candidates {
		right = append(right, rest...)
	}
	slices.Sort(ties.pointers)
	slices.Sort(ties.nodes)
	ties.pointers, ties.nodes = slices.Compact(ties.pointers), slices.Compact(ties.nodes)
	return ties, left, right
}

// untied returns the reports of rs no tie decided: one naming a tied node or a
// model holding one, or sitting at or beneath a tie site or a tied member's
// pointer, says what a tie decided, in the one order it decided it.
func untied(rs []report, ties tieSites, written sequences) []report {
	return slices.DeleteFunc(rs, func(reportValue report) bool {
		node := erase(reportValue.node, written)
		_, tied := slices.BinarySearch(ties.nodes, node)
		_, holds := slices.BinarySearch(ties.holders, node)
		at := erase(reportValue.pointer, written)
		return reportValue.node != "" && (tied || holds) || atSite(ties.pointers, at) || atSite(ties.members, at)
	})
}

// atSite reports whether erased, an erased pointer, is at or beneath one of
// sites.
func atSite(sites []string, erased string) bool {
	return slices.ContainsFunc(sites, func(site string) bool {
		return erased == site || strings.HasPrefix(erased, site) && strings.HasPrefix(erased[len(site):], "/")
	})
}

// renderUnpaired renders left and right, the reports only one order makes, a
// quoted key per line, '-' as written and '+' reversed, each side sorted, and
// empty when there are none.
func renderUnpaired(left, right []report) string {
	lines := make([]string, 0, len(left)+len(right))
	for _, report := range left {
		lines = append(lines, "-"+strconv.Quote(report.key))
	}
	reversed := make([]string, 0, len(right))
	for _, report := range right {
		reversed = append(reversed, "+"+strconv.Quote(report.key))
	}
	slices.Sort(reversed)
	return strings.Join(append(lines, reversed...), "\n")
}

// settledFrom returns ties with every member doc settles from a tied one: a
// member a moved report names, one declared at or beneath where a moved report
// sits, as a fold of inline declarations reports where they are written, or at
// a tied member's pointer or minted beneath it, and a model's own member whose
// flattening overrides a tied declaration (ir.FlatProperty.Overridden), which
// reads what that tie decided, to a fixpoint (tieSettled).
// Each tied member's pointer joins ties.members. IDs are erased in seqs, doc's
// coordinates; a nil doc, as a refused compile leaves, adds none.
func settledFrom(ties tieSites, doc *ir.Document, seqs sequences) tieSites {
	if len(ties.nodes)+len(ties.pointers)+len(ties.members) == 0 || doc == nil {
		return ties
	}
	tied := map[string]bool{}
	for _, n := range ties.nodes {
		tied[n] = true
	}
	models := slices.Sorted(maps.Keys(doc.Types))
	sites, holders := map[string]bool{}, map[string]bool{}
	for range len(models) + 1 {
		if !tieSettled(doc, models, seqs, ties, tied, sites, holders) {
			break
		}
	}
	if len(tied) == 0 {
		return ties
	}
	ties.nodes = slices.Sorted(maps.Keys(tied))
	ties.members = slices.Compact(slices.Sorted(slices.Values(slices.Concat(ties.members, slices.Collect(maps.Keys(sites))))))
	ties.holders = slices.Compact(slices.Sorted(slices.Values(slices.Concat(ties.holders, slices.Collect(maps.Keys(holders))))))
	return ties
}

// tieSettled ties each own member of models declared at or beneath where a
// moved report sits (ties.pointers), at a tied member's pointer (ties.members),
// or overriding a tied declaration, and each union whose inferred
// discriminator reads a tied member (inferenceTied), adds each tied member's
// pointer to sites and its model to holders, and reports whether it tied any
// or found a tied member's pointer. A member declared beneath a tied member's
// pointer is written there in either order, so it is not tied for that, unless
// the tied member's fold minted it (minted), as a model it composes declares
// one.
func tieSettled(doc *ir.Document, models []ir.TypeID, seqs sequences, ties tieSites, tied, sites, holders map[string]bool) bool {
	grew := false
	members := slices.Concat(ties.members, slices.Collect(maps.Keys(sites)))
	isTied := func(propertyDecl ir.PropertyDecl) bool {
		at := erase(string(propertyDecl.Property.Provenance.Pointer), seqs)
		_, atMember := slices.BinarySearch(ties.members, at)
		return tied[erase(string(propertyDecl.Property.ID), seqs)] || atSite(ties.pointers, at) || atMember ||
			minted(members, string(propertyDecl.Property.ID), at)
	}
	for _, id := range models {
		if union, isUnion := doc.Types[id].(*ir.Union); isUnion && !tied[erase(string(id), seqs)] && inferenceTied(doc, union, isTied) {
			tied[erase(string(id), seqs)], grew = true, true
		}
		if _, isModel := doc.Types[id].(*ir.Model); !isModel {
			continue
		}
		for _, flatProperty := range ir.FlattenedProperties(doc.Types, id).Properties {
			own := erase(string(flatProperty.Property.ID), seqs)
			if flatProperty.Owner == id && !tied[own] && (isTied(flatProperty.PropertyDecl) || slices.ContainsFunc(flatProperty.Overridden, isTied)) {
				tied[own], grew = true, true
			}
			if at := erase(string(flatProperty.Property.Provenance.Pointer), seqs); flatProperty.Owner == id && tied[own] {
				grew = grew || !sites[at]
				sites[at] = true
				holders[erase(string(id), seqs)] = true
			}
		}
	}
	return grew
}

// inferenceTied reports whether union's discriminator was inferred from a member a
// tie decided: the inference reads each variant's member the discriminator
// names, so what it finds follows the tie.
func inferenceTied(doc *ir.Document, union *ir.Union, isTied func(ir.PropertyDecl) bool) bool {
	discriminator := union.Discriminator
	if discriminator == nil || !discriminator.Inferred {
		return false
	}
	for _, variant := range union.Variants {
		for _, flatProperty := range ir.FlattenedProperties(doc.Types, variant.Type.Target).Properties {
			if flatProperty.Property.WireName == discriminator.PropertyName && isTied(flatProperty.PropertyDecl) {
				return true
			}
		}
	}
	return false
}

// difference returns the reports of a that b does not hold and those of b
// that a does not, as multisets of their keys, each sorted by key.
func difference(a, b []report) (onlyA, onlyB []report) {
	return without(a, b), without(b, a)
}

// without returns the reports of a left once each report of b takes one of a
// of the same key, sorted by key.
func without(a, b []report) []report {
	taken := make(map[string]int, len(b))
	for _, report := range b {
		taken[report.key]++
	}
	var out []report
	for _, report := range a {
		if taken[report.key] > 0 {
			taken[report.key]--
			continue
		}
		out = append(out, report)
	}
	slices.SortFunc(out, func(reportValue, otherReport report) int { return strings.Compare(reportValue.key, otherReport.key) })
	return out
}
