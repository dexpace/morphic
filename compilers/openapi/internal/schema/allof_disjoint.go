// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// instanceTypes is a set of JSON instance types, null aside: schemaAdmitsNull
// is the one reading of null. Its zero is a schema writing `type: "null"`.
type instanceTypes uint8

const (
	instanceObject instanceTypes = 1 << iota
	instanceArray
	instanceString
	instanceNumber // integer too: every integer is a number
	instanceBoolean

	// anyInstance is what a schema writing no `type` admits.
	anyInstance = instanceObject | instanceArray | instanceString | instanceNumber | instanceBoolean
)

// instanceTypeWords spells each member of an instanceTypes, in the order a
// diagnostic lists them.
var instanceTypeWords = []struct {
	member instanceTypes
	word   oas3.SchemaType
}{
	{instanceObject, oas3.SchemaTypeObject},
	{instanceArray, oas3.SchemaTypeArray},
	{instanceString, oas3.SchemaTypeString},
	{instanceNumber, oas3.SchemaTypeNumber},
	{instanceBoolean, oas3.SchemaTypeBoolean},
}

// words spells t for a diagnostic.
func (t instanceTypes) words() string {
	if t == 0 {
		return string(oas3.SchemaTypeNull)
	}
	out := make([]string, 0, len(instanceTypeWords))
	for _, w := range instanceTypeWords {
		if t&w.member != 0 {
			out = append(out, string(w.word))
		}
	}
	return strings.Join(out, " or ")
}

// instanceTypeOf is the set st names. A word no dialect defines says nothing:
// load reports it, so it is no conjunct's to contradict another.
func instanceTypeOf(st oas3.SchemaType) instanceTypes {
	switch st {
	case oas3.SchemaTypeObject:
		return instanceObject
	case oas3.SchemaTypeArray:
		return instanceArray
	case oas3.SchemaTypeString:
		return instanceString
	case oas3.SchemaTypeNumber, oas3.SchemaTypeInteger:
		return instanceNumber
	case oas3.SchemaTypeBoolean:
		return instanceBoolean
	case oas3.SchemaTypeNull:
		return 0
	default:
		return anyInstance
	}
}

// declaredInstanceTypes is the set s's own `type` keyword states. A schema
// writing none excludes nothing, whatever else it writes: `properties` alone
// constrains objects and admits a string.
func declaredInstanceTypes(s *oas3.Schema) instanceTypes {
	declared := s.GetType()
	if len(declared) == 0 {
		return anyInstance
	}
	var out instanceTypes
	for _, st := range declared {
		out |= instanceTypeOf(st)
	}
	return out
}

// conjunctTypes reads what schemas state of their instance types, within one
// budget for the whole read, as nulls does. It runs out to anyInstance, the
// answer that claims the least, so a cycle ends on the budget.
type conjunctTypes struct {
	refs  *typed.Bindings
	steps budget.Budget
}

// schema is what s admits: its `type` conjoined with its allOf's entries. An
// allOf that shares no value says nothing here, which keeps a schema composing
// a reported one from drawing a second report. Where nothing reports it, as for
// a null its own entries share, that is a miss.
func (r *conjunctTypes) schema(s *oas3.Schema) instanceTypes {
	if s == nil || !r.steps.Spend(1) {
		return anyInstance
	}
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
// and reading it from 3.1 on, where it applies, is left open.
// A boolean entry has no schema, so its `false` is applyFalseBranches' to report.
func (r *conjunctTypes) branch(b *oas3.JSONSchema[oas3.Referenceable]) instanceTypes {
	if isRefBranch(b) {
		return r.schema(refBranchTarget(r.refs, b))
	}
	return r.schema(b.GetSchema())
}

// entry is what one flattened entry adds. A wrapper adds only its own `type`,
// since its allOf's entries stand beside it in the flattened list.
func (r *conjunctTypes) entry(e allOfBranch) instanceTypes {
	if e.wrapper {
		return declaredInstanceTypes(e.schema.GetSchema())
	}
	return r.branch(e.schema)
}

// disjointConjuncts names what each conjunct of comp, the allOf s writes,
// states of its instance types where they share no value, and returns nil
// where they share one (docs/compilers/openapi.md#allof-disjoint-conjuncts).
// A null is one: the shared reading of it is schemaAdmitsNull's.
func disjointConjuncts(c lowering.Ctx, s *oas3.Schema, comp allOfComposition) []string {
	r := conjunctTypes{refs: c.Bindings(), steps: budget.New(budget.AtMost(maxNullConjuncts))}
	own := declaredInstanceTypes(s)
	joint := own
	read := make([]instanceTypes, len(comp.branches))
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
	return comp.namedConjuncts(own, read)
}

// namedConjuncts spells each conjunct that narrows the instance types, own
// being what the composing schema's `type` says and read each entry's.
func (a allOfComposition) namedConjuncts(own instanceTypes, read []instanceTypes) []string {
	out := make([]string, 0, len(read)+1)
	if own != anyInstance {
		out = append(out, "type: "+own.words())
	}
	for i, t := range read {
		if t != anyInstance {
			out = append(out, a.branchPath(a.branches[i].pointer)+": "+t.words())
		}
	}
	return out
}

// diagDisjointConjuncts is the one report a composition whose conjuncts share
// no value draws, at the schema that writes them.
func diagDisjointConjuncts(c lowering.Ctx, comp allOfComposition) []ir.Diagnostic {
	if len(comp.disjoint) == 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, comp.pointer,
		"the conjuncts of this allOf share no value (%s), so no instance satisfies this schema, which lowers as written",
		strings.Join(comp.disjoint, ", "))}
}

// readAllOf is allOfBranches of the schema written at pointer, with what its
// conjuncts jointly admit read, for a lowering that composes it.
func readAllOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfBranches(s, pointer)
	comp.disjoint = disjointConjuncts(c, s, comp)
	return comp
}
