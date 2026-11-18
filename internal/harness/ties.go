// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// decision is one thing a report carrying a pair states: the member it
// decided, by its ID, lost what the declaration at lost says and kept what
// the one at kept says (placeOf), each with every index of a reversed
// sequence erased, and what the report says beside those places (sayingOf).
// A member a model brings is named by its first declaration where its type is
// written at several places
// (docs/compilers/openapi.md#allof-redeclared-property), which the reversal
// moves to another entry of that model's allOf, so only the erased place
// names it in both orders.
type decision struct{ member, lost, kept, says string }

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
// reports carrying none, in order: one naming one kept place, a member at
// least and nothing of another role. written erases the IDs and the places.
func factsOf(rs []report, written sequences) (facts []fact, rest []report) {
	for _, r := range rs {
		kept, decided, known := r.concerns()
		if !known || len(kept) != 1 || len(decided) == 0 {
			rest = append(rest, r)
			continue
		}
		lostAt, keptAt := placeOf(r.rebased.Provenance), placeOf(kept[0])
		for _, member := range decided {
			facts = append(facts, fact{
				member: erase(member.Node, written), lost: erase(lostAt, written), kept: erase(keptAt, written),
				says: sayingOf(r.rebased, member, written), lostAt: lostAt, keptAt: keptAt, report: r,
			})
		}
	}
	return facts, rest
}

// concerns reads what r concerns besides its place, as rebased: the places it
// says were kept, the entries listing the nodes it says it decided, and
// whether it names nothing of another role.
func (r report) concerns() (kept, decided []ir.Provenance, known bool) {
	known = true
	for _, related := range r.rebased.Related {
		switch related.Role {
		case ir.RelatedKept:
			kept = append(kept, related.Provenance)
		case ir.RelatedDecided:
			decided = append(decided, related.Provenance)
		default:
			known = false
		}
	}
	return kept, decided, known
}

// placeOf is where p locates a declaration, as one string: its pointer, then
// its source, the input that wrote it, whether it has a position, and the
// heuristics that produced what it locates, each of which describes the place
// and so changes sides with it in a mirrored report.
func placeOf(p ir.Provenance) string {
	return string(p.Pointer) + "\x00" + strconv.Itoa(p.Source) + "\x00" + strconv.Itoa(p.WrittenBy) + "\x00" +
		strconv.FormatBool(p.Position != ir.Position{}) + "\x00" + string(p.Inferred)
}

// sayingOf is what d, a report carrying a pair, says of the member member
// lists beside its two places, as diagnosticKey reads it: its severity, its
// code, and the entry listing the member but for its node, each index of a
// reversed sequence, written, erased.
func sayingOf(d ir.Diagnostic, member ir.Provenance, written sequences) string {
	member.Node, member.Pointer = "", jsontext.Pointer(erase(string(member.Pointer), written))
	return diagnosticKey(ir.Diagnostic{
		Severity: d.Severity, Code: d.Code, Related: []ir.Related{{Role: ir.RelatedDecided, Provenance: member}},
	})
}

// tiedBy returns, sorted, the members a tie decided: each one a fact of a,
// the source as written, says lost one place and kept another, while a fact
// of b, the source reversed, says it lost that other and kept the first, the
// two reports saying the same beside those places. Two places only an erased
// index tells apart, entries of one allOf, are mirrored where they are as
// written. Nothing else of the two compiles is read: a tie is what the
// compiler reports as one in both orders.
func tiedBy(a, b []fact) []string {
	mirrored, exactly := make(map[decision]bool, len(b)), make(map[decision]bool, len(b))
	for _, f := range b {
		mirrored[decision{member: f.member, lost: f.kept, kept: f.lost, says: f.says}] = true
		exactly[decision{member: f.member, lost: f.keptAt, kept: f.lostAt, says: f.says}] = true
	}
	var tied []string
	for _, f := range a {
		apart := f.lost != f.kept || exactly[decision{member: f.member, lost: f.lostAt, kept: f.keptAt, says: f.says}]
		if mirrored[f.decision] && apart {
			tied = append(tied, f.member)
		}
	}
	slices.Sort(tied)
	return slices.Compact(tied)
}

// unpairedFacts returns the reports of a and of b stating a fact the other
// side does not, of a member no tie decided (tied, sorted): each side's facts
// pair one to one by the member, the place that lost and what the report says.
// The place kept is not compared: of several declarations saying the same, a
// report names the one written last, which follows the order, though the field
// names one in either (docs/compilers/openapi.md#allof-redeclared-property).
// A tied member's facts are not compared, so where a third declaration
// disagrees, its report dropped in one order passes.
func unpairedFacts(a, b []fact, tied []string) (left, right []report) {
	return factsWithout(a, b, tied), factsWithout(b, a, tied)
}

// factsWithout returns, once each and in order, the reports of a stating a
// fact of an untied member left once each fact of b takes one of a of its
// member, lost place and what its report says beside its places.
func factsWithout(a, b []fact, tied []string) []report {
	type lost struct{ member, at, says string }
	taken := map[lost]int{}
	for _, f := range b {
		taken[lost{f.member, f.lost, f.says}]++
	}
	var out []report
	seen := map[string]bool{}
	for _, f := range a {
		key := lost{f.member, f.lost, f.says}
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
