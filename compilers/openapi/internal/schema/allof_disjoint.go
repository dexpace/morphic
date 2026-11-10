// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxConjunctTypeReads bounds conjunctTypes' read (styleguide bounded-recursion
// rule), one step per schema it reads, so its depth and breadth: an allOf naming
// its own schema ends on it. A read past it states nothing, the answer that
// claims the least. A report also needs the null read, which maxNullConjuncts
// bounds on its own.
const maxConjunctTypeReads = 256

// anyInstance is what a schema writing no `type` admits, null aside: nulls is
// the one reading of null, so a set here holds no kindNull, and its zero is a
// schema writing `type: "null"`.
const anyInstance = kindAny &^ kindNull

// kindWords spells the members of a kindSet in the order a diagnostic lists
// them. A number holds an integer, so it is spelled before one: an integer is
// named only where no number is left beside it.
var kindWords = []struct {
	kinds kindSet
	word  oas3.SchemaType
}{
	{kindObject, oas3.SchemaTypeObject},
	{kindArray, oas3.SchemaTypeArray},
	{kindString, oas3.SchemaTypeString},
	{kindNumber, oas3.SchemaTypeNumber},
	{kindInteger, oas3.SchemaTypeInteger},
	{kindBoolean, oas3.SchemaTypeBoolean},
}

// instanceWords spells t, a set of instance types, for a diagnostic.
func instanceWords(t kindSet) string {
	if t == 0 {
		return string(oas3.SchemaTypeNull)
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
func declaredInstanceTypes(s *oas3.Schema) kindSet {
	declared := s.GetType()
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

// conjunctTypes reads what schemas state of their instance types, within one
// budget for the whole read, as nulls does. It runs out to anyInstance, the
// answer that claims the least, so a cycle ends on the budget.
type conjunctTypes struct {
	refs  *typed.Bindings
	steps budget.Budget
	// passedOver is set where the read meets what can refuse a value of some
	// instance type and that it does not read (passesOver), so anyInstance may
	// claim more than the schema admits. A report reads anyInstance as stating
	// nothing and needs no more; a merge narrowing a structure does.
	passedOver bool
}

// schema is what s admits: its `type` conjoined with its allOf's entries. A
// schema writing an allOf says nothing here where that conjunction leaves at
// most a null, which keeps a schema composing a reported one from drawing a
// second report. Where nothing reports it, as for a null its own entries share
// or a `type: "null"` beside them, that is a miss.
func (r *conjunctTypes) schema(s *oas3.Schema) kindSet {
	if s == nil || !r.steps.Spend(1) {
		return anyInstance
	}
	r.passedOver = r.passedOver || passesOver(s)
	out := declaredInstanceTypes(s)
	entries := s.GetAllOf()
	for _, b := range entries {
		out &= r.branch(b)
	}
	if out == 0 && len(entries) > 0 {
		return anyInstance
	}
	return out
}

// branch is what one allOf entry admits: an inline one's own schema, a $ref's
// the schema it lands on. A keyword beside a $ref says nothing: 3.0 ignores it,
// and reading it from 3.1 on, where it applies, is left open (GitHub #1390).
// A boolean entry has no schema, so its `false` is applyFalseBranches' to report.
// Each of these, and a $ref landing on no schema, is passed over.
func (r *conjunctTypes) branch(b *oas3.JSONSchema[oas3.Referenceable]) kindSet {
	if isRefBranch(b) {
		target := refBranchTarget(r.refs, b)
		r.passedOver = r.passedOver || target == nil || r.besideChain(b)
		return r.schema(target)
	}
	r.passedOver = r.passedOver || annotation.IsFalseSchema(b)
	return r.schema(b.GetSchema())
}

// besideChain reports whether b, an allOf entry writing a $ref, or an alias its
// chain passes on the way to what branch reads, writes beside its $ref what can
// refuse a value of some instance type. The hops are walked in a loop the
// chain's own length bounds, as nulls.lands walks them; one the bindings do
// not know has none.
func (r *conjunctTypes) besideChain(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, _ := r.refs.Entry(b)
	hop := b
	for i := 0; i < e.Chain.Hops && hop != nil; i++ {
		if writesBesideRef(hop.GetSchema()) {
			return true
		}
		hop = r.refs.Declared(hop)
	}
	return false
}

// passesOver reports whether s writes a keyword that can refuse a value of some
// instance type and that conjunctTypes does not read: a union, a `not`, a
// condition, an `enum` or a `const`, or a dynamic or recursive reference.
func passesOver(s *oas3.Schema) bool {
	return len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 || s.GetNot() != nil || s.GetIf() != nil ||
		enumWritten(s) || s.GetConst() != nil || declaresDynamicRef(s) ||
		annotation.RawPropertyNode(s, "$recursiveRef") != nil
}

// writesBesideRef reports whether s, a schema writing a $ref, writes beside it
// anything that can refuse a value of some instance type.
func writesBesideRef(s *oas3.Schema) bool {
	return s != nil && (writesType(s) || len(s.GetAllOf()) > 0 || passesOver(s))
}

// entry is what one flattened entry adds. A wrapper adds only its own `type`,
// since its allOf's entries stand beside it in the flattened list.
func (r *conjunctTypes) entry(e allOfBranch) kindSet {
	if e.wrapper {
		return declaredInstanceTypes(e.schema.GetSchema())
	}
	return r.branch(e.schema)
}

// disjointConjuncts names what each conjunct of comp, the allOf s writes,
// states of its instance types where they share no value, and returns nil
// where they share one (docs/compilers/openapi.md#allof-disjoint-conjuncts).
// A null is one: the shared reading of it is the nulls walk's. One conjunct
// contradicts nothing, so a lone `type: "null"` beside what takes its null away
// draws nil too.
func disjointConjuncts(c lowering.Ctx, s *oas3.Schema, comp allOfComposition) []string {
	r := conjunctTypes{refs: c.Bindings(), steps: budget.New(budget.AtMost(maxConjunctTypeReads))}
	own := declaredInstanceTypes(s)
	joint := own
	read := make([]kindSet, len(comp.branches))
	for i, e := range comp.branches {
		read[i] = r.entry(e)
		joint &= read[i]
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
	names := comp.namedConjuncts(own, read)
	if len(names) < 2 {
		return nil
	}
	return names
}

// namedConjuncts spells each conjunct that narrows the instance types, own
// being what the composing schema's `type` says and read each entry's.
func (a allOfComposition) namedConjuncts(own kindSet, read []kindSet) []string {
	out := make([]string, 0, len(read)+1)
	if own != anyInstance {
		out = append(out, "type: "+instanceWords(own))
	}
	for i, t := range read {
		if t != anyInstance {
			out = append(out, a.branchPath(a.branches[i].pointer)+": "+instanceWords(t))
		}
	}
	return out
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

// readAllOf is allOfBranches of the schema written at pointer, with what its
// conjuncts jointly admit read, for a lowering that composes it.
func readAllOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfBranches(s, pointer)
	comp.disjoint = disjointConjuncts(c, s, comp)
	return comp
}
