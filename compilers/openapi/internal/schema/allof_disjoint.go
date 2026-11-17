// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// anyInstance is what a schema writing no `type` admits, null aside: nulls is
// the one reading of null, so a set here holds no kindNull, and its zero is a
// schema writing `type: "null"`.
const anyInstance = kindAny &^ kindNull

// kindWords spells the members of a kindSet in the order a diagnostic lists
// them. A number holds an integer and a number with a fraction, so it is
// spelled before either: each is named only where the other is not left
// beside it, the second as no type names it.
var kindWords = []struct {
	kinds kindSet
	word  string
}{
	{kindObject, string(statement.KindObject)},
	{kindArray, string(statement.KindArray)},
	{kindString, string(statement.KindString)},
	{kindNumber, string(statement.KindNumber)},
	{kindInteger, string(statement.KindInteger)},
	{kindFraction, "number that is no integer"},
	{kindBoolean, string(statement.KindBoolean)},
}

// instanceWords spells t, a set of instance types, for a diagnostic.
func instanceWords(t kindSet) string {
	if t == 0 {
		return string(statement.KindNull)
	}
	out := make([]string, 0, len(kindWords))
	for _, w := range kindWords {
		if t&w.kinds == w.kinds {
			out = append(out, w.word)
			t &^= w.kinds
		}
	}
	return strings.Join(out, " or ")
}

// declaredInstanceTypes is the set s's own `type` keyword states. A schema
// writing none excludes nothing, whatever else it writes: `properties` alone
// constrains objects and admits a string. A word no dialect defines says
// nothing either: load reports it, so it is no conjunct's to contradict another.
func declaredInstanceTypes(s *statement.Schema) kindSet {
	declared := kindsIn(s)
	if len(declared) == 0 {
		return anyInstance
	}
	var out kindSet
	for _, st := range declared {
		kinds := kindOfType(st)
		if kinds == 0 {
			kinds = anyInstance
		}
		out |= kinds &^ kindNull
	}
	return out
}

// conjunctAtom is one conjunct of an allOf, as a disjoint report names it:
// its name below the composing schema, what it admits, and whether its read
// was cut short. Its null is the statement st's, or, for keywords read apart
// from a $ref or an allOf they stand beside, those of the schema words.
// written is where the conjunct is written apart from the allOf, whose report
// of admitting nothing would be its own (toldWhereWritten): what a $ref entry
// lands on, or a `false` entry itself, and nil for any other.
type conjunctAtom struct {
	name    string
	admits  kindSet
	st      *statement.Statement
	words   *statement.Schema
	written *statement.Statement
	cut     bool
}

// noValue is how a disjoint report names a conjunct admitting no value at all.
const noValue = "no value"

// disjointConjuncts names what each conjunct of comp, the allOf s writes,
// admits where they share no value, and returns nil where they share one
// (docs/compilers/openapi.md#allof-disjoint-conjuncts). A null is one: the
// shared reading of it is the nulls walk's. A read cut short decides nothing,
// and a conjunct admitting nothing whose schema reports it where it is written
// leaves the report to it. One conjunct contradicts nothing, so a lone
// `type: "null"` beside what takes its null away draws nil too, but one
// admitting nothing by itself is named, as nothing else reports it.
func disjointConjuncts(c lowering.Ctx, s *statement.Schema, comp allOfComposition) []string {
	atoms := conjunctAtoms(c, s, comp)
	joint := anyInstance
	for _, a := range atoms {
		if a.cut {
			return nil
		}
		joint &= a.admits
	}
	if joint != 0 {
		return nil
	}
	// A null read that ran out of budget is undecided, not a refusal of null: a
	// miss is the answer that claims the least.
	n := newNulls(c)
	if n.schema(s, nullSite{}).admits(c) || n.conjuncts.Exhausted() {
		return nil
	}
	var names []string
	void := false
	for _, a := range atoms {
		switch {
		case !empty(c, a):
			if a.admits != anyInstance {
				names = append(names, a.name+": "+instanceWords(a.admits))
			}
		case a.written != nil && toldWhereWritten(c, a.written):
			return nil
		default:
			names, void = append(names, a.name+": "+noValue), true
		}
	}
	if len(names) < 2 && !void {
		return nil
	}
	return names
}

// conjunctAtoms returns the conjuncts of comp, in order: the `type` s writes
// beside it, the value set it writes there (valueSetAtom), each union s writes
// beside it that shares no kind with the rest (unionAside), and each flattened
// branch, a wrapper by its own `type`, an entry nested past
// statement.MaxFlattenDepth as cut, and a $ref entry writing keywords that
// narrow what it admits as two, the reference and the keywords beside it,
// which apply to an instance alike.
func conjunctAtoms(c lowering.Ctx, s *statement.Schema, comp allOfComposition) []conjunctAtom {
	out := make([]conjunctAtom, 0, 4+len(comp.branches))
	out = append(out, conjunctAtom{name: "type", admits: declaredInstanceTypes(s), words: s})
	if set, ok := valueSetAtom(c, s); ok {
		out = append(out, set)
	}
	if unionAside(c, s) {
		choice := choiceIn(s)
		out = append(out, unionAtoms(c, "oneOf", choice.OneOf)...)
		out = append(out, unionAtoms(c, "anyOf", choice.AnyOf)...)
	}
	for i, e := range comp.branches {
		name := comp.branchPath(e.pointer)
		r := admissionOf(c, e.schema)
		ref, _ := declaredOf(c, e.schema)
		switch {
		case e.wrapper:
			out = append(out, conjunctAtom{name: name, admits: declaredInstanceTypes(e.schema.Schema), words: e.schema.Schema})
		case e.schema.BesideRef != nil:
			// The rest of an entry writing a $ref beside it, whose reference is the
			// branch before it (splitRef): what its own keywords admit, its unions
			// among them.
			w := newAdmission(c)
			out = append(out, conjunctAtom{name: name, admits: inlineConjunct(&w, e.schema.Schema).kinds,
				words: e.schema.Schema})
		case splitRef(comp, i):
			out = append(out, conjunctAtom{name: name + "/$ref", admits: r.admits, st: e.schema, cut: r.cut, written: ref})
		default:
			a := conjunctAtom{name: name, admits: r.admits, st: e.schema, cut: r.cut || e.capped, written: ref}
			if e.schema.Truth.ReadsFalse() {
				a.written = e.schema
			}
			out = append(out, a)
		}
	}
	return out
}

// valueSetAtom returns the enum or the const s writes beside its allOf as a
// conjunct, as one an entry writes is (docs/compilers/openapi.md#allof-refinement):
// the kinds its members leave, null aside (valueKinds), its read cut where no
// reading converts them, and false where s writes neither. It is written at s,
// whose report that the set holds no value its type admits leaves this report
// none to make (toldWhereWritten).
func valueSetAtom(c lowering.Ctx, s *statement.Schema) (conjunctAtom, bool) {
	name, members := "enum", enumIn(s)
	switch {
	case enumWritten(s):
	case constIn(s) != nil:
		name, members = "const", []*yaml.Node{constIn(s)}
	default:
		return conjunctAtom{}, false
	}
	unread := memberSetOf(c, valueSetOf{s, name}, members).unread
	return conjunctAtom{name: name, admits: valueKinds(c, s), words: s, written: c.Statements().Read(s), cut: unread}, true
}

// splitRef reports whether the branch at i of comp is the reference an entry
// writes beside other keys, the next branch, at its pointer, being the rest of
// that entry (statement.Entry).
func splitRef(comp allOfComposition, i int) bool {
	return i+1 < len(comp.branches) && comp.branches[i+1].pointer == comp.branches[i].pointer &&
		comp.branches[i].pointer != comp.pointer
}

// toldWhereWritten reports whether st, written apart from the allOf naming it
// and admitting no value, says so where it is written, so the allOf need not:
// it reads as `false` (docs/compilers/openapi.md#allof-false-branch), its
// value set holds no value its own type admits (docs/compilers/openapi.md#enum-const),
// its allOf's conjuncts share no kind, which this report reads at it, or no
// branch of a union beside its object body lives (keepsDeadUnion). A $ref
// landing on a schema admitting no value is read through, up to
// maxAdmissionReads hops. Anything else leaving st no value is reported
// nowhere else.
func toldWhereWritten(c lowering.Ctx, st *statement.Statement) bool {
	for range maxAdmissionReads {
		switch {
		case st.Truth.ReadsFalse():
			return true
		case st.Truth != statement.Stated:
			return false
		case (enumWritten(st.Schema) || constIn(st.Schema) != nil) && declaredInstanceTypes(st.Schema)&valueKinds(c, st.Schema) == 0:
			return true
		case st.Ref == nil && len(st.AllOf) > 0:
			return conjunctsShareNothing(c, st.Schema, nil)
		case st.Ref == nil && keepsDeadUnion(c, st.Schema):
			return true
		}
		next, _ := declaredOf(c, st)
		if next == nil || !empty(c, conjunctAtom{admits: admissionOf(c, next).admits, st: next}) {
			return false
		}
		st = next
	}
	return false
}

// conjunctsShareNothing reports whether the conjuncts of the allOf s writes,
// read as a disjoint report reads them, share no kind, the entry whose schema
// is aside left out where aside is not nil. A read cut short admits more than
// the schema does, never less, so it never makes them share nothing.
func conjunctsShareNothing(c lowering.Ctx, s, aside *statement.Schema) bool {
	at, _ := siteOf(s)
	joint := anyInstance
	for _, a := range conjunctAtoms(c, s, allOfBranches(c, s, at.Pointer)) {
		if aside == nil || a.st == nil || a.st.Schema != aside {
			joint &= a.admits
		}
	}
	return joint == 0
}

// unionAtoms returns the union keyword names, written beside an allOf with
// branches, as one conjunct admitting what any branch does, and none where it
// writes none.
func unionAtoms(c lowering.Ctx, keyword string, branches []statement.Sub) []conjunctAtom {
	if len(branches) == 0 {
		return nil
	}
	atom := conjunctAtom{name: keyword}
	for _, b := range branches {
		r := admissionOf(c, c.Statements().Sub(b))
		atom.admits |= r.admits
		atom.cut = atom.cut || r.cut
	}
	return []conjunctAtom{atom}
}

// empty reports whether a admits no value by itself, the null included as
// nulls reads it, which makes it its own schema's to report rather than the
// allOf's.
func empty(c lowering.Ctx, a conjunctAtom) bool {
	if a.admits != 0 {
		return false
	}
	if a.st != nil {
		n := newNulls(c)
		return !n.conjunct(a.st, nullSite{}).admits(c)
	}
	return typeNullVerdict(a.words) != nullAdmitted
}

// diagDisjointConjuncts is the one report an allOf whose conjuncts share no
// value draws, at the schema that writes them, which lowered to shape: a
// composition, or the value set an election passed it over for.
func diagDisjointConjuncts(c lowering.Ctx, comp allOfComposition, shape string) []ir.Diagnostic {
	if len(comp.disjoint) == 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, comp.pointer,
		"the conjuncts of %s share no value (%s), so no instance satisfies this schema, which %s cannot state",
		comp.subject(), strings.Join(comp.disjoint, ", "), shape)}
}

// composedShape is the shape a composition lowers to, for diagDisjointConjuncts.
const composedShape = "a composition"
