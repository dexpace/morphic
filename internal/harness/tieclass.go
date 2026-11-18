// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// tieClasses is what two compiles' documents show an allOf's entry order
// decided among declarations several models compose. A conflict is reported
// at the declaration that lost, once for the models keeping the other, naming
// their member where one does, so the reversal moves and regroups reports. A member conflicts where the declarations it overrides do not all
// say the same (memberForm); a report says something of it at one of them,
// naming it or no node (attributed). The oracle forgives a tied member's node
// (tiedMembers) and a report the other compile matches within a class
// (unmatched), and compares the rest.
type tieClasses struct {
	// least maps each declaration of a conflicting member a report says
	// something of, erased, to its class's least pointer: two such members
	// sharing a declaration are one class.
	least map[string]string
	// at maps each such declaration to the members folding it, erased.
	at map[string][]string
	// members are the tied members' IDs, erased, sorted (tiedMembers).
	members []string
}

// tieClassesOf reads the tie classes of first and second, compiled as written
// and reversed, whose reports are a and b. It declines where a member form
// does (formDecline).
func tieClassesOf(first, second *formReader, a, b []report) (tieClasses, Decline) {
	if first.doc == nil || second.doc == nil {
		return tieClasses{}, ""
	}
	members := foldedDeclarations(first.doc, first.seqs)
	at := declaredAt(members)
	written, reversed := attributed(a, at, first.seqs), attributed(b, at, first.seqs)
	if len(written) == 0 || len(reversed) == 0 {
		return tieClasses{}, ""
	}
	maps.DeleteFunc(members, func(id string, _ folds) bool { return len(written[id])+len(reversed[id]) == 0 })
	asWritten, err := memberForms(first, members)
	if err != nil {
		return tieClasses{}, formDecline(err)
	}
	maps.DeleteFunc(members, func(id string, _ folds) bool {
		return !slices.ContainsFunc(asWritten[id], func(f memberForm) bool { return f.conflicts })
	})
	classes := tieClasses{least: map[string]string{}, at: declaredAt(members)}
	for _, id := range slices.Sorted(maps.Keys(members)) {
		classes.join(members[id].at)
	}
	classes.settle()
	asReversed, err := memberForms(second, members)
	if err != nil {
		return tieClasses{}, formDecline(err)
	}
	classes.members = tiedMembers(members, asWritten, asReversed, written, reversed, first.seqs)
	return classes, ""
}

// tiedMembers returns, sorted, the members a tie decided among members, the
// conflicting ones reports say something of: each taking something else in
// each document (memberForms), whose declarations hold such a report in each
// compile, at two of them at least across both, as a tie's moves from the
// declaration that lost, and that the compiles show trading one declaration
// for another (sides.shown, sameOutcomes). It deletes the rest from members.
func tiedMembers(members map[string]folds, asWritten, asReversed map[string][]memberForm, written, reversed map[string][]report,
	seqs sequences,
) []string {
	maps.DeleteFunc(members, func(id string, _ folds) bool {
		return len(written[id]) == 0 || len(reversed[id]) == 0 || reportedAt(written[id], reversed[id], seqs) < 2 ||
			slices.EqualFunc(asWritten[id], asReversed[id], func(x, y memberForm) bool { return x.model == y.model && x.shape == y.shape })
	})
	same := sameOutcomes(members, asWritten, asReversed)
	var tied []string
	for _, id := range slices.Sorted(maps.Keys(members)) {
		s := sides{forms: [2][]memberForm{asWritten[id], asReversed[id]}, reports: [2][]report{written[id], reversed[id]}}
		if s.shown(seqs, same[family(members[id].family)]) {
			tied = append(tied, id)
		}
	}
	return tied
}

// sides is what each compile, as written and reversed, shows of one member:
// its forms, and the reports saying something of it.
type sides struct {
	forms   [2][]memberForm
	reports [2][]report
}

// shown reports whether the compiles show a tie traded what the member takes,
// seqs erasing their pointers: its reports change and one sits in each
// compile where it does not take the type; or its family takes the same
// outcomes in either, as same says, and a report sits in each where it does
// not take the declaration; or traded holds.
func (s sides) shown(seqs sequences, same bool) bool {
	moved := !slices.Equal(reportKeys(s.reports[0]), reportKeys(s.reports[1]))
	return moved && s.lostBeside(seqs, typedBy) || same && s.lostBeside(seqs, takenBy) || s.traded(seqs)
}

// traded reports whether, in each compile, the member takes what declarations
// it overrides say, in every model holding it, others than in the other
// compile, and a report saying something of it sits at one it takes in the
// other: the declaration a tie made lose there.
func (s sides) traded(seqs sequences) bool {
	took, takes := taken(s.forms[0]), taken(s.forms[1])
	if slices.ContainsFunc(took, func(at string) bool { return slices.Contains(takes, at) }) {
		return false
	}
	return lostAt(s.reports[0], takes, seqs) && lostAt(s.reports[1], took, seqs)
}

// lostBeside reports whether, in each compile and each of its forms, one of
// the reports saying something of the member sits at a declaration other
// than those beside gives, as a report sits at the declaration that lost.
func (s sides) lostBeside(seqs sequences, beside func(memberForm) []string) bool {
	for i, forms := range s.forms {
		if slices.ContainsFunc(forms, func(f memberForm) bool {
			return !slices.ContainsFunc(s.reports[i], func(r report) bool { return !slices.Contains(beside(f), erase(r.pointer, seqs)) })
		}) {
			return false
		}
	}
	return true
}

// typedBy is the declarations whose type f's member takes.
func typedBy(f memberForm) []string { return f.typed }

// takenBy is the declarations f's member takes whole.
func takenBy(f memberForm) []string { return f.takes }

// sameOutcomes returns which families of members, as family writes them, take
// the same tie shapes in either compile, whatever model takes each: the
// outcomes two ties trading declarations permute among the models.
func sameOutcomes(members map[string]folds, asWritten, asReversed map[string][]memberForm) map[string]bool {
	shapes := map[string][2]map[string]bool{}
	for id, folded := range members {
		f := family(folded.family)
		both, ok := shapes[f]
		if !ok {
			both = [2]map[string]bool{{}, {}}
			shapes[f] = both
		}
		for i, forms := range [][]memberForm{asWritten[id], asReversed[id]} {
			for _, form := range forms {
				both[i][form.tie] = true
			}
		}
	}
	out := make(map[string]bool, len(shapes))
	for f, both := range shapes {
		out[f] = maps.Equal(both[0], both[1])
	}
	return out
}

// taken is the declarations every one of forms takes, or none where one takes
// none.
func taken(forms []memberForm) []string {
	if len(forms) == 0 {
		return nil
	}
	out := forms[0].takes
	for _, f := range forms[1:] {
		out = slices.DeleteFunc(slices.Clone(out), func(at string) bool { return !slices.Contains(f.takes, at) })
	}
	return out
}

// lostAt reports whether one of rs sits at one of at, erased in written.
func lostAt(rs []report, at []string, written sequences) bool {
	return slices.ContainsFunc(rs, func(r report) bool { return slices.Contains(at, erase(r.pointer, written)) })
}

// family is the declarations a member overrides as one string: members
// choosing among the same declarations hold the same outcomes.
func family(at []string) string { return strings.Join(at, "\n") }

// reportKeys is rs's keys, sorted.
func reportKeys(rs []report) []string {
	keys := make([]string, len(rs))
	for i, r := range rs {
		keys[i] = r.key
	}
	slices.Sort(keys)
	return keys
}

// reportedAt counts the pointers, erased in written, that the reports of
// either compile sit at.
func reportedAt(asWritten, reversed []report, written sequences) int {
	at := map[string]bool{}
	for _, r := range slices.Concat(asWritten, reversed) {
		at[erase(r.pointer, written)] = true
	}
	return len(at)
}

// declaredAt maps each declaration of members to the members folding it,
// sorted.
func declaredAt(members map[string]folds) map[string][]string {
	out := map[string][]string{}
	for id, folded := range members {
		for _, p := range folded.at {
			out[p] = append(out[p], id)
		}
	}
	for _, ids := range out {
		slices.Sort(ids)
	}
	return out
}

// attributed maps each member the reports of rs say something of to those
// reports: one at a pointer at holds, erased in written, naming that member
// or no node, as a conflict said for several models names none.
func attributed(rs []report, at map[string][]string, written sequences) map[string][]report {
	out := map[string][]report{}
	for _, r := range rs {
		node := erase(r.node, written)
		for _, id := range at[erase(r.pointer, written)] {
			if r.node == "" || node == id {
				out[id] = append(out[id], r)
			}
		}
	}
	return out
}

// folds is what a member folds (ir.FlatProperty): the pointers of its own
// declaration and those it overrides, and its family, the pointers of those
// it overrides alone, since a model settling a member declares it where no
// other model does. Each is sorted and erased.
type folds struct{ at, family []string }

// foldedDeclarations maps the ID of each member of doc's models' shapes that
// folds declarations at two or more pointers to what it folds, each pointer
// erased in seqs, doc's coordinates.
func foldedDeclarations(doc *ir.Document, seqs sequences) map[string]folds {
	out := map[string]folds{}
	for id, td := range doc.Types {
		if _, isModel := td.(*ir.Model); !isModel {
			continue
		}
		for _, p := range ir.FlattenedProperties(doc.Types, id).Properties {
			member := erase(string(p.Property.ID), seqs)
			f := out[member]
			f.at = append(f.at, erase(string(p.Property.Provenance.Pointer), seqs))
			for _, d := range p.Overridden {
				f.family = append(f.family, erase(string(d.Property.Provenance.Pointer), seqs))
			}
			out[member] = f
		}
	}
	for member, f := range out {
		f.family = slices.Compact(slices.Sorted(slices.Values(f.family)))
		if f.at = slices.Compact(slices.Sorted(slices.Values(slices.Concat(f.at, f.family)))); len(f.at) < 2 {
			delete(out, member)
			continue
		}
		out[member] = f
	}
	return out
}

// join puts pointers, a conflicting member's declarations, in one class with
// every class one of them is in, whose least pointer is the least of theirs
// and of those classes'.
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

// unmatched returns rs without each report that one of other, the other
// compile's reports, matches within a class (classKey): what a class's
// declarations draw is compared as the keys the class holds, whatever
// declaration each sits at, whatever member it names and however many say
// it, since the reversal moves a report and regroups the models it is said
// for.
func (c tieClasses) unmatched(rs, other []report, written sequences) []report {
	held := map[string]bool{}
	for _, r := range other {
		if key, ok := c.classKey(r, written); ok {
			held[key] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(rs), func(r report) bool {
		key, ok := c.classKey(r, written)
		return ok && held[key]
	})
}

// classKey is r's key at its class's least pointer naming no node, where r
// sits at a class's declaration, erased in written, naming no node or a
// member folding that declaration; false otherwise.
func (c tieClasses) classKey(r report, written sequences) (string, bool) {
	at := erase(r.pointer, written)
	least, ok := c.least[at]
	if !ok {
		return "", false
	}
	if _, folds := slices.BinarySearch(c.at[at], erase(r.node, written)); r.node != "" && !folds {
		return "", false
	}
	d := r.raw
	d.Provenance.Pointer, d.Provenance.Node = jsontext.Pointer(least), ""
	return diagnosticKey(d), true
}
