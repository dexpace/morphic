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

	"github.com/google/go-cmp/cmp"

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
// leave untouched: the diagnostics, the document and each model's flattened
// shape, each read in the coordinates of its own source. A pair of diagnostics
// equal but for which element of a reversed sequence their pointers name is a
// report that moves with the order, a tie the lowering states, so nothing the
// documents hold at or beneath its pointer, or at the node it names, is
// compared. Every difference is reported. It declines a document read from more
// than one file, and what compareForms declines.
func diffSequenceInvariants(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic, perm permutation) (string, bool, Decline) {
	if first != nil && len(first.Sources) > 1 {
		return "", false, DeclineReadsAnotherFile
	}
	var found []string
	ties, left, right := movedReports(firstDiags, secondDiags, perm)
	ties = settledFrom(settledFrom(ties, first, perm.written), second, perm.moved)
	if unpaired := renderUnpaired(untied(left, ties, perm.written), untied(right, ties, perm.written)); unpaired != "" {
		found = append(found, "diagnostics depend on sequence order (-as-written +reversed):\n"+unpaired)
	}
	switch refused := oneRefusal(first, second); {
	case refused != "":
		found = append(found, refused)
	case first != nil:
		forms, why := compareForms(first, second, perm, ties)
		if why != "" {
			return "", false, why
		}
		found = append(found, forms...)
	}
	return strings.Join(found, "\n"), len(found) == 0, ""
}

// compareForms compares two documents' canonical forms (canonicalForm), each
// in its own source's coordinates, line by line, and their flattened shapes,
// or declines where either form nests past its bound or meets a cycle of
// digests.
func compareForms(first, second *ir.Document, perm permutation, ties tieSites) ([]string, Decline) {
	a, err := canonicalForm(first, perm.written, ties)
	if why := formDecline(err); why != "" {
		return nil, why
	}
	if err != nil {
		return []string{"encode as written: " + err.Error()}, ""
	}
	b, err := canonicalForm(second, perm.moved, ties)
	if why := formDecline(err); why != "" {
		return nil, why
	}
	if err != nil {
		return []string{"encode permuted: " + err.Error()}, ""
	}
	var found []string
	if !slices.Equal(a.document, b.document) {
		found = append(found, "document depends on sequence order (-as-written +reversed):\n"+cmp.Diff(a.document, b.document))
	}
	if !slices.Equal(a.shape, b.shape) {
		found = append(found, "flattened shape depends on sequence order (-as-written +reversed):\n"+cmp.Diff(a.shape, b.shape))
	}
	return found, ""
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
// node, its pointer and its node. A report naming the node it decided is told
// apart by that node: it sits at the declaration the order made lose, which a
// nested allOf can place at another depth.
type report struct{ key, erased, pointer, node string }

// tieSites is what the reports a reversal moved decided: the erased pointer of
// each, at or beneath which nothing is compared, the erased node of each naming
// one (ir.Provenance's Node), which is not compared wherever it sits, and the
// models holding a member a tie decided (settledFrom), whose reports follow it.
type tieSites struct{ pointers, nodes, holders []string }

// reportsOf reads diags, rebasing each pointer and node from the coordinates
// of from, when given, to those of the source as written, where written lists
// the reversed sequences.
func reportsOf(diags []ir.Diagnostic, from, written sequences) []report {
	out := make([]report, 0, len(diags))
	for _, d := range diags {
		d.Provenance.Pointer = jsontext.Pointer(rebase(string(d.Provenance.Pointer), from))
		d.Provenance.Node = rebase(d.Provenance.Node, from)
		e := d
		e.Provenance.Pointer = jsontext.Pointer(erase(string(d.Provenance.Pointer), written))
		e.Provenance.Node = erase(d.Provenance.Node, written)
		if e.Provenance.Node != "" {
			e.Provenance.Pointer = ""
		}
		out = append(out, report{key: diagnosticKey(d), erased: diagnosticKey(e), pointer: string(d.Provenance.Pointer),
			node: d.Provenance.Node})
	}
	return out
}

// movedReports pairs what one compile reports and the other does not, a
// report of each equal as report.erased reads them and differing in their
// pointers, which is a report the reversal moved: its erased pointer is a tie
// site, and its erased node, where it names one, a tied node. Pairs match
// greedily, in key order; it returns what stays unpaired as written and
// reversed.
func movedReports(firstDiags, secondDiags []ir.Diagnostic, perm permutation) (ties tieSites, left, right []report) {
	onlyFirst, onlySecond := difference(reportsOf(firstDiags, nil, perm.written), reportsOf(secondDiags, perm.moved, perm.written))
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
		ties.pointers = append(ties.pointers, erase(a.pointer, perm.written))
		if a.node != "" {
			ties.nodes = append(ties.nodes, erase(a.node, perm.written))
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
// model holding one, or sitting at or beneath a tie site, says what a tie
// decided, in the one order it decided it.
func untied(rs []report, ties tieSites, written sequences) []report {
	return slices.DeleteFunc(rs, func(r report) bool {
		node := erase(r.node, written)
		_, tied := slices.BinarySearch(ties.nodes, node)
		_, holds := slices.BinarySearch(ties.holders, node)
		return r.node != "" && (tied || holds) || atSite(ties.pointers, erase(r.pointer, written))
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
	for _, r := range left {
		lines = append(lines, "-"+strconv.Quote(r.key))
	}
	reversed := make([]string, 0, len(right))
	for _, r := range right {
		reversed = append(reversed, "+"+strconv.Quote(r.key))
	}
	slices.Sort(reversed)
	return strings.Join(append(lines, reversed...), "\n")
}

// settledFrom returns ties with every member doc settles from a tied one: a
// model's own member whose flattening overrides a tied declaration
// (ir.FlatProperty.Overridden) reads what that tie decided, so it is tied too,
// to a fixpoint each pass of which ties one more member at least. Each tied
// member's pointer is a tie site, as its fold mints there. IDs are erased in
// seqs, doc's coordinates; a nil doc, as a refused compile leaves, adds none.
func settledFrom(ties tieSites, doc *ir.Document, seqs sequences) tieSites {
	if len(ties.nodes) == 0 || doc == nil {
		return ties
	}
	tied := map[string]bool{}
	for _, n := range ties.nodes {
		tied[n] = true
	}
	models := slices.Sorted(maps.Keys(doc.Types))
	sites, holders := map[string]bool{}, map[string]bool{}
	for range len(models) + 1 {
		if !tieSettled(doc, models, seqs, tied, sites, holders) {
			break
		}
	}
	ties.nodes = slices.Sorted(maps.Keys(tied))
	ties.pointers = slices.Compact(slices.Sorted(slices.Values(slices.Concat(ties.pointers, slices.Collect(maps.Keys(sites))))))
	ties.holders = slices.Compact(slices.Sorted(slices.Values(slices.Concat(ties.holders, slices.Collect(maps.Keys(holders))))))
	return ties
}

// tieSettled ties each own member of models that overrides a tied declaration,
// and each union whose inferred discriminator reads a tied member
// (inferenceTied), adds each tied member's pointer to sites and its model to
// holders, and reports whether it tied any.
func tieSettled(doc *ir.Document, models []ir.TypeID, seqs sequences, tied, sites, holders map[string]bool) bool {
	grew := false
	isTied := func(d ir.PropertyDecl) bool { return tied[erase(string(d.Property.ID), seqs)] }
	for _, id := range models {
		if u, isUnion := doc.Types[id].(*ir.Union); isUnion && !tied[erase(string(id), seqs)] && inferenceTied(doc, u, isTied) {
			tied[erase(string(id), seqs)], grew = true, true
		}
		if _, isModel := doc.Types[id].(*ir.Model); !isModel {
			continue
		}
		for _, p := range ir.FlattenedProperties(doc.Types, id).Properties {
			own := erase(string(p.Property.ID), seqs)
			if p.Owner == id && !tied[own] && slices.ContainsFunc(p.Overridden, isTied) {
				tied[own], grew = true, true
			}
			if p.Owner == id && tied[own] {
				sites[erase(string(p.Property.Provenance.Pointer), seqs)] = true
				holders[erase(string(id), seqs)] = true
			}
		}
	}
	return grew
}

// inferenceTied reports whether u's discriminator was inferred from a member a
// tie decided: the inference reads each variant's member the discriminator
// names, so what it finds follows the tie.
func inferenceTied(doc *ir.Document, u *ir.Union, isTied func(ir.PropertyDecl) bool) bool {
	d := u.Discriminator
	if d == nil || !d.Inferred {
		return false
	}
	for _, v := range u.Variants {
		for _, p := range ir.FlattenedProperties(doc.Types, v.Type.Target).Properties {
			if p.Property.WireName == d.PropertyName && isTied(p.PropertyDecl) {
				return true
			}
		}
	}
	return false
}

// difference returns the reports of a that b does not hold and those of b
// that a does not, as multisets of keys, each sorted by key.
func difference(a, b []report) (onlyA, onlyB []report) {
	return without(a, b), without(b, a)
}

// without returns the reports of a left once each of b's keys takes one
// report of a with that key, sorted by key.
func without(a, b []report) []report {
	taken := make(map[string]int, len(b))
	for _, r := range b {
		taken[r.key]++
	}
	var out []report
	for _, r := range a {
		if taken[r.key] > 0 {
			taken[r.key]--
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(x, y report) int { return strings.Compare(x.key, y.key) })
	return out
}
