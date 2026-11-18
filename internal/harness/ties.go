// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// decision is one thing a report carrying a pair states: the member it
// decided, by its ID, lost what the declaration at lost says and kept what
// the one at kept says (placeOf), each with every index of a reversed
// sequence erased. A member a model brings is named by its first declaration
// where its type is written at several places
// (docs/compilers/openapi.md#allof-redeclared-property), which the reversal
// moves to another entry of that model's allOf, so only the erased place
// names it in both orders.
type decision struct{ member, lost, kept string }

// fact is a decision, the report stating it, and its two places as written,
// not erased, which tell two entries of one allOf apart. A conflict between
// two declarations is such a report (ir.RelatedKept, ir.RelatedDecided), said
// once for every model composing them, so it states a fact per member it
// lists.
type fact struct {
	decision
	lostAt, keptAt string
	report         report
}

// factsOf splits rs into the facts its reports carrying a pair state and the
// reports carrying none, in order: one naming one kept place and a member at
// least. written erases the IDs and the places.
func factsOf(rs []report, written sequences) (facts []fact, rest []report) {
	for _, r := range rs {
		kept, decided := r.concerns()
		if len(kept) != 1 || len(decided) == 0 {
			rest = append(rest, r)
			continue
		}
		lostAt, keptAt := placeOf(r.rebased.Provenance), placeOf(kept[0])
		for _, member := range decided {
			facts = append(facts, fact{
				member: erase(member, written), lost: erase(lostAt, written), kept: erase(keptAt, written),
				lostAt: lostAt, keptAt: keptAt, report: r,
			})
		}
	}
	return facts, rest
}

// concerns reads what r concerns besides its place, as rebased: the places it
// says were kept, and the nodes it says it decided.
func (r report) concerns() (kept []ir.Provenance, decided []string) {
	for _, related := range r.rebased.Related {
		switch related.Role {
		case ir.RelatedKept:
			kept = append(kept, related.Provenance)
		case ir.RelatedDecided:
			decided = append(decided, related.Provenance.Node)
		default:
		}
	}
	return kept, decided
}

// placeOf is where p locates a declaration, as one string: its pointer, then
// its source and the input that wrote it.
func placeOf(p ir.Provenance) string {
	return string(p.Pointer) + "\x00" + strconv.Itoa(p.Source) + "\x00" + strconv.Itoa(p.WrittenBy)
}

// tiedBy returns, sorted, the members a tie decided: each one a fact of a,
// the source as written, says lost one place and kept another, while a fact
// of b, the source reversed, says it lost that other and kept the first. Two
// places only an erased index tells apart, entries of one allOf, are mirrored
// where they are as written. Nothing else of the two compiles is read: a tie
// is what the compiler reports as one in both orders.
func tiedBy(a, b []fact) []string {
	mirrored, exactly := make(map[decision]bool, len(b)), make(map[decision]bool, len(b))
	for _, f := range b {
		mirrored[decision{member: f.member, lost: f.kept, kept: f.lost}] = true
		exactly[decision{member: f.member, lost: f.keptAt, kept: f.lostAt}] = true
	}
	var tied []string
	for _, f := range a {
		apart := f.lost != f.kept || exactly[decision{member: f.member, lost: f.lostAt, kept: f.keptAt}]
		if mirrored[f.decision] && apart {
			tied = append(tied, f.member)
		}
	}
	slices.Sort(tied)
	return slices.Compact(tied)
}

// unpairedFacts returns the reports of a and of b stating a fact the other
// side does not, of a member no tie decided (tied, sorted): each side's facts
// pair one to one by the member and the place that lost. The place kept is
// not compared there: of several declarations saying the same, a report names
// the one written last, which follows the order
// (docs/compilers/openapi.md#allof-redeclared-property). A tied member's
// facts are not compared: three declarations disagreeing each with the
// others state, beside the mirrored pair, a fact each order holds alone, so a
// report dropped there in one order passes.
func unpairedFacts(a, b []fact, tied []string) (left, right []report) {
	return factsWithout(a, b, tied), factsWithout(b, a, tied)
}

// factsWithout returns, once each and in order, the reports of a stating a
// fact of an untied member left once each fact of b takes one of a of its
// member and lost place.
func factsWithout(a, b []fact, tied []string) []report {
	type lost struct{ member, at string }
	taken := map[lost]int{}
	for _, f := range b {
		taken[lost{f.member, f.lost}]++
	}
	var out []report
	seen := map[string]bool{}
	for _, f := range a {
		key := lost{f.member, f.lost}
		if _, isTied := slices.BinarySearch(tied, f.member); isTied {
			continue
		}
		if taken[key] > 0 {
			taken[key]--
			continue
		}
		if !seen[f.report.key] {
			seen[f.report.key] = true
			out = append(out, f.report)
		}
	}
	return out
}

// held is what a document shows of one member that folds declarations: the
// type it takes, and the type each declaration it overrides names, by that
// declaration's pointer. A type is its target, erased, and whether it admits
// null.
type held struct {
	takes string
	said  map[string]string
}

// heldBy maps the ID, erased in seqs, of each member of doc's models' shapes
// that overrides a declaration to what doc shows of it; a nil doc, as a
// refused compile leaves, shows nothing.
func heldBy(doc *ir.Document, seqs sequences) map[string]held {
	out := map[string]held{}
	if doc == nil {
		return out
	}
	typeOf := func(p *ir.Property) string {
		return erase(string(p.Type.Target), seqs) + " " + strconv.FormatBool(p.Type.Nullable)
	}
	for id, td := range doc.Types {
		if _, isModel := td.(*ir.Model); !isModel {
			continue
		}
		for _, p := range ir.FlattenedProperties(doc.Types, id).Properties {
			if p.Owner != id || len(p.Overridden) == 0 {
				continue
			}
			h := held{takes: typeOf(p.Property), said: make(map[string]string, len(p.Overridden))}
			for _, d := range p.Overridden {
				h.said[string(d.Property.Provenance.Pointer)] = typeOf(d.Property)
			}
			out[erase(string(p.Property.ID), seqs)] = h
		}
	}
	return out
}

// contradicted returns a line for each tied member (tied, sorted) that holds,
// in each order, exactly the type of the declaration its report says lost
// there: as written the type the declaration at lost names, and reversed the
// one at kept, the two types differing. A tie trades one declaration's say for
// the other's, so a member doing so took the loser's in both orders, whatever
// its reports say. It reads the two declarations as the document as written
// holds them, so it checks a member only where both are declarations its
// flattening overrides, named by their own pointers.
func contradicted(first, second *ir.Document, stated []fact, tied []string, perm permutation) []string {
	if len(tied) == 0 {
		return nil
	}
	written, reversed := heldBy(first, perm.written), heldBy(second, perm.moved)
	var out []string
	for _, f := range stated {
		if _, isTied := slices.BinarySearch(tied, f.member); !isTied {
			continue
		}
		w, r := written[f.member], reversed[f.member]
		lost, saysLost := w.said[pointerOf(f.lostAt)]
		kept, saysKept := w.said[pointerOf(f.keptAt)]
		if saysLost && saysKept && lost != kept && w.takes == lost && r.takes == kept {
			out = append(out, f.member+" takes in each order the type of the declaration its report says lost: "+
				strconv.Quote(lost)+" as written, at "+pointerOf(f.lostAt)+", and "+strconv.Quote(kept)+" reversed, at "+pointerOf(f.keptAt))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// pointerOf is the pointer of a place placeOf wrote.
func pointerOf(place string) string {
	pointer, _, _ := strings.Cut(place, "\x00")
	return pointer
}

// statedAlone returns the members of stated, sorted, that no other tie
// reaches in doc: none a moved report names or sits over (moved), and none
// overriding a declaration a tie decided (tied, every tied node, sorted),
// which reads what that tie decided. A tie between two declarations that
// disagree decides what the member takes of them, not its docs, which fold
// over every declaration and draw a report of their own where two differ
// (docs/compilers/openapi.md#allof-redeclared-property). IDs are erased in
// seqs, doc's coordinates; a nil doc removes none.
func statedAlone(stated []string, moved tieSites, tied []string, doc *ir.Document, seqs sequences) []string {
	if len(stated) == 0 || doc == nil {
		return stated
	}
	reached := map[string]bool{}
	for _, node := range moved.nodes {
		reached[node] = true
	}
	for id, td := range doc.Types {
		if _, isModel := td.(*ir.Model); !isModel {
			continue
		}
		for _, p := range ir.FlattenedProperties(doc.Types, id).Properties {
			settled := slices.ContainsFunc(p.Overridden, func(d ir.PropertyDecl) bool {
				_, isTied := slices.BinarySearch(tied, erase(string(d.Property.ID), seqs))
				return isTied
			})
			if p.Owner == id && (settled || atSite(moved.pointers, erase(string(p.Property.Provenance.Pointer), seqs))) {
				reached[erase(string(p.Property.ID), seqs)] = true
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(stated), func(member string) bool { return reached[member] })
}
