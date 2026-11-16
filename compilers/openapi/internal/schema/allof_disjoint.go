// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"
	"strings"

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
// them. A number holds an integer, so it is spelled before one: an integer is
// named only where no number is left beside it.
var kindWords = []struct {
	kinds kindSet
	word  statement.Kind
}{
	{kindObject, statement.KindObject},
	{kindArray, statement.KindArray},
	{kindString, statement.KindString},
	{kindNumber, statement.KindNumber},
	{kindInteger, statement.KindInteger},
	{kindBoolean, statement.KindBoolean},
}

// instanceWords spells t, a set of instance types, for a diagnostic.
func instanceWords(t kindSet) string {
	if t == 0 {
		return string(statement.KindNull)
	}
	out := make([]string, 0, len(kindWords))
	for _, w := range kindWords {
		if t&w.kinds == w.kinds {
			out = append(out, string(w.word))
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
type conjunctAtom struct {
	name   string
	admits kindSet
	st     *statement.Statement
	words  *statement.Schema
	cut    bool
}

// disjointConjuncts names what each conjunct of comp, the allOf s writes,
// admits where they share no value, and returns nil where they share one
// (docs/compilers/openapi.md#allof-disjoint-conjuncts). A null is one: the
// shared reading of it is the nulls walk's. A read cut short decides nothing,
// a conjunct admitting nothing by itself is its own schema's to report, and
// one conjunct contradicts nothing, so a lone `type: "null"` beside what takes
// its null away draws nil too.
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
	if slices.ContainsFunc(atoms, func(a conjunctAtom) bool { return empty(c, a) }) {
		return nil
	}
	var names []string
	for _, a := range atoms {
		if a.admits != anyInstance {
			names = append(names, a.name+": "+instanceWords(a.admits))
		}
	}
	if len(names) < 2 {
		return nil
	}
	return names
}

// conjunctAtoms returns the conjuncts of comp, in order: the `type` s writes
// beside it, and each flattened branch, a wrapper by its own `type`, an entry
// nested past statement.MaxFlattenDepth as cut, and a $ref entry writing
// keywords that narrow what it admits as two, the reference and the keywords
// beside it, which apply to an instance alike. A value set s writes beside the
// allOf is the election's to weigh (competing-keywords), and is not read.
func conjunctAtoms(c lowering.Ctx, s *statement.Schema, comp allOfComposition) []conjunctAtom {
	out := make([]conjunctAtom, 0, 1+len(comp.branches))
	out = append(out, conjunctAtom{name: "type", admits: declaredInstanceTypes(s), words: s})
	for _, e := range comp.branches {
		name := comp.branchPath(e.pointer)
		r := admissionOf(c, e.schema)
		switch {
		case e.wrapper:
			out = append(out, conjunctAtom{name: name, admits: declaredInstanceTypes(e.schema.Schema), words: e.schema.Schema})
		case e.schema.Ref != nil && r.words != anyInstance && !r.cut:
			ref, _ := declaredOf(c, e.schema)
			out = append(out, conjunctAtom{name: name + "/$ref", admits: admissionOf(c, ref).admits, st: ref},
				conjunctAtom{name: name, admits: r.words, words: e.schema.Schema})
		default:
			out = append(out, conjunctAtom{name: name, admits: r.admits, st: e.schema, cut: r.cut || e.capped})
		}
	}
	return out
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
		"the conjuncts of this allOf share no value (%s), so no instance satisfies this schema, which %s cannot state",
		strings.Join(comp.disjoint, ", "), shape)}
}

// composedShape is the shape a composition lowers to, for diagDisjointConjuncts.
const composedShape = "a composition"
