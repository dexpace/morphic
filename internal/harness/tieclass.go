// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// tieClasses is what two compiles' documents say an allOf's entry order
// decided among declarations several models compose: the members it tied, and
// the classes their declarations fall in. The compiler reports such a tie
// once, at the declaration that lost, naming no member, so the reversal moves
// the report to another declaration of its class, and two ties can trade
// theirs. A report naming no node at a class's declarations is matched by its
// key at the class's least pointer (classed), whatever its code, so one of
// another code moving between them is read as the tie's.
type tieClasses struct {
	// least maps each declaration pointer of a class, erased, to the class's
	// least such pointer.
	least map[string]string
	// members are the tied members' IDs, erased, sorted.
	members []string
}

// tieClassesOf reads the tie classes of first and second, compiled as written
// and reversed, whose reports are a and b. A member is tied where it folds
// declarations at two or more pointers (ir.FlatProperty), takes something
// else in each document (memberForms), whatever made it differ, its
// declarations hold a report naming no node in each compile, and its class is
// shown a tie (shown). A member declared alike, or one no report in one
// compile sits beside, is not. A differing member's declarations are one
// class, and classes sharing one are one. It declines where a member form
// does (formDecline).
func tieClassesOf(first, second *formReader, a, b []report) (tieClasses, Decline) {
	written, reversed := unnamedAt(a, first.seqs), unnamedAt(b, first.seqs)
	if first.doc == nil || second.doc == nil || len(written) == 0 || len(reversed) == 0 {
		return tieClasses{}, ""
	}
	members := foldedDeclarations(first.doc, first.seqs)
	maps.DeleteFunc(members, func(_ string, at []string) bool {
		return !holdsAny(at, written) || !holdsAny(at, reversed)
	})
	if len(members) == 0 {
		return tieClasses{}, ""
	}
	asWritten, err := memberForms(first, members)
	if err != nil {
		return tieClasses{}, formDecline(err)
	}
	asReversed, err := memberForms(second, members)
	if err != nil {
		return tieClasses{}, formDecline(err)
	}
	classes := tieClasses{least: map[string]string{}}
	var differing []string
	for _, id := range slices.Sorted(maps.Keys(members)) {
		if !slices.Equal(asWritten[id], asReversed[id]) {
			differing = append(differing, id)
			classes.join(members[id])
		}
	}
	classes.settle()
	forms := map[string][2][]memberForm{}
	for _, id := range differing {
		least := classes.least[members[id][0]]
		both := forms[least]
		forms[least] = [2][]memberForm{append(both[0], asWritten[id]...), append(both[1], asReversed[id]...)}
	}
	shown := classes.shown(a, b, first.seqs, forms)
	classes.members = slices.DeleteFunc(differing, func(id string) bool { return !shown[classes.least[members[id][0]]] })
	return classes, ""
}

// shown returns which of c's classes, keyed by their least pointer, a tie is
// shown to decide: a report naming no node at its declarations moves between
// a and b, or, where none does, as where two ties trade declarations, its
// members take the same shapes in either compile, each in another model.
// forms holds each class's members' forms as written and reversed. A class
// whose members differ while its reports stay put shows none: a tie moves
// the report at the declaration that lost.
func (c tieClasses) shown(a, b []report, written sequences, forms map[string][2][]memberForm) map[string]bool {
	balance := map[string]map[string]int{}
	for i, rs := range [][]report{a, b} {
		for _, r := range rs {
			least, ok := c.least[erase(r.pointer, written)]
			if r.node != "" || !ok {
				continue
			}
			if balance[least] == nil {
				balance[least] = map[string]int{}
			}
			balance[least][r.key] += 1 - 2*i
		}
	}
	out := map[string]bool{}
	for least, both := range forms {
		moved := slices.ContainsFunc(slices.Collect(maps.Values(balance[least])), func(n int) bool { return n != 0 })
		out[least] = moved || maps.Equal(shapes(both[0]), shapes(both[1]))
	}
	return out
}

// shapes is the set of the shapes forms hold, whatever model holds each.
func shapes(forms []memberForm) map[string]bool {
	out := map[string]bool{}
	for _, f := range forms {
		out[f.shape] = true
	}
	return out
}

// unnamedAt counts the reports of rs naming no node at each pointer, erased in
// written.
func unnamedAt(rs []report, written sequences) map[string]int {
	out := map[string]int{}
	for _, r := range rs {
		if r.node == "" {
			out[erase(r.pointer, written)]++
		}
	}
	return out
}

// holdsAny reports whether counts holds one at least of pointers.
func holdsAny(pointers []string, counts map[string]int) bool {
	return slices.ContainsFunc(pointers, func(at string) bool { return counts[at] > 0 })
}

// foldedDeclarations maps the ID of each member of doc's models' shapes that
// folds declarations at two or more pointers to those pointers, sorted, each
// erased in seqs, doc's coordinates: a member folds its own declaration and
// those it overrides (ir.FlatProperty).
func foldedDeclarations(doc *ir.Document, seqs sequences) map[string][]string {
	out := map[string][]string{}
	for id, td := range doc.Types {
		if _, isModel := td.(*ir.Model); !isModel {
			continue
		}
		for _, p := range ir.FlattenedProperties(doc.Types, id).Properties {
			member := erase(string(p.Property.ID), seqs)
			for _, d := range append([]ir.PropertyDecl{p.PropertyDecl}, p.Overridden...) {
				out[member] = append(out[member], erase(string(d.Property.Provenance.Pointer), seqs))
			}
		}
	}
	for member, at := range out {
		slices.Sort(at)
		if at = slices.Compact(at); len(at) < 2 {
			delete(out, member)
			continue
		}
		out[member] = at
	}
	return out
}

// join puts pointers, a tied member's declarations, in one class with every
// class one of them is in, whose least pointer is the least of theirs and of
// those classes'.
func (c tieClasses) join(pointers []string) {
	least := pointers[0]
	for _, at := range pointers {
		least = min(least, c.find(at))
	}
	for _, at := range pointers {
		c.least[c.find(at)] = least
		c.least[at] = least
	}
}

// find is the least pointer of at's class as joined so far: the end of the
// chain least makes from at, each link to a lesser pointer, so no longer than
// the pointers joined.
func (c tieClasses) find(at string) string {
	for range len(c.least) {
		next, ok := c.least[at]
		if !ok || next == at {
			break
		}
		at = next
	}
	return at
}

// settle maps each declaration straight to its class's least pointer.
func (c tieClasses) settle() {
	for at := range c.least {
		c.least[at] = c.find(at)
	}
}

// classed returns rs with each report naming no node at a declaration of a
// class matched as one at that class's least pointer, so one class's reports
// in either compile meet whichever declaration each sits at, and kept once per
// key: a compiler says one thing once for every model saying it, and the
// reversal changes which of a class's reports say the same, so how many there
// are is not the tie's. Any other report keeps its key.
func (c tieClasses) classed(rs []report, written sequences) []report {
	out := make([]report, 0, len(rs))
	seen := map[string]bool{}
	for _, r := range rs {
		least, ok := c.least[erase(r.pointer, written)]
		if r.node == "" && ok {
			d := r.raw
			d.Provenance.Pointer = jsontext.Pointer(least)
			if r.match = diagnosticKey(d); seen[r.match] {
				continue
			}
			seen[r.match] = true
		}
		out = append(out, r)
	}
	return out
}
