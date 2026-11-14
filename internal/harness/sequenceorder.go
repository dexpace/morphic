// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
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
// orderInvariant does. Of a sequence of three it tries one other order, it
// swaps nothing in another file a reference reaches, and it compiles at the
// default options alone.
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
		func(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic) (string, bool) {
			return diffSequenceInvariants(first, firstDiags, second, secondDiags, perm)
		})
}

// diffSequenceInvariants compares what reversing the sequences of perm must
// leave untouched: the diagnostics, the document and each model's flattened
// shape, each read in the coordinates of its own source. A pair of diagnostics
// equal but for which element of a reversed sequence their pointers name is a
// report that moves with the order, a tie the lowering states, so nothing the
// documents hold at or beneath its pointer is compared. Every difference is
// reported, one after another.
func diffSequenceInvariants(first *ir.Document, firstDiags []ir.Diagnostic, second *ir.Document, secondDiags []ir.Diagnostic, perm permutation) (string, bool) {
	var found []string
	ties, unpaired := movedReports(firstDiags, secondDiags, perm)
	if unpaired != "" {
		found = append(found, "diagnostics depend on sequence order (-as-written +reversed):\n"+unpaired)
	}
	switch refused := oneRefusal(first, second); {
	case refused != "":
		found = append(found, refused)
	case first != nil:
		found = append(found, compareForms(first, second, perm, ties)...)
	}
	return strings.Join(found, "\n"), len(found) == 0
}

// compareForms compares two documents' canonical forms (canonicalForm), each
// in its own source's coordinates, line by line, and their flattened shapes.
func compareForms(first, second *ir.Document, perm permutation, ties []string) []string {
	a, err := canonicalForm(first, perm.written, ties)
	if err != nil {
		return []string{"encode as written: " + err.Error()}
	}
	b, err := canonicalForm(second, perm.moved, ties)
	if err != nil {
		return []string{"encode permuted: " + err.Error()}
	}
	var found []string
	if !slices.Equal(a.document, b.document) {
		found = append(found, "document depends on sequence order (-as-written +reversed):\n"+cmp.Diff(a.document, b.document))
	}
	if !slices.Equal(a.shape, b.shape) {
		found = append(found, "flattened shape depends on sequence order (-as-written +reversed):\n"+cmp.Diff(a.shape, b.shape))
	}
	return found
}

// report is one diagnostic as the comparison reads it, in the coordinates of
// the source as written: as diagnosticKey renders it, the same with every
// index of a reversed sequence erased, and its pointer.
type report struct{ key, erased, pointer string }

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
		out = append(out, report{key: diagnosticKey(d), erased: diagnosticKey(e), pointer: string(d.Provenance.Pointer)})
	}
	return out
}

// movedReports pairs what one compile reports and the other does not, a
// report of each equal once indices are erased and differing in their
// pointers, which is a report the reversal moved: its erased pointer is a tie
// site. Pairs match greedily, in key order; what stays unpaired is rendered a
// quoted key per line, '-' as written and '+' reversed, empty when nothing does.
func movedReports(firstDiags, secondDiags []ir.Diagnostic, perm permutation) (ties []string, unpaired string) {
	onlyFirst, onlySecond := difference(reportsOf(firstDiags, nil, perm.written), reportsOf(secondDiags, perm.moved, perm.written))
	candidates := map[string][]report{}
	for _, b := range onlySecond {
		candidates[b.erased] = append(candidates[b.erased], b)
	}
	var left []string
	for _, a := range onlyFirst {
		i := slices.IndexFunc(candidates[a.erased], func(b report) bool { return b.pointer != a.pointer })
		if i < 0 {
			left = append(left, "-"+strconv.Quote(a.key))
			continue
		}
		candidates[a.erased] = slices.Delete(candidates[a.erased], i, i+1)
		ties = append(ties, erase(a.pointer, perm.written))
	}
	var leftSecond []string
	for _, rest := range candidates {
		for _, b := range rest {
			leftSecond = append(leftSecond, "+"+strconv.Quote(b.key))
		}
	}
	slices.Sort(leftSecond)
	slices.Sort(ties)
	return slices.Compact(ties), strings.Join(append(left, leftSecond...), "\n")
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
