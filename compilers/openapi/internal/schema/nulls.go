// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// nullVerdict is what one keyword family says about the null value. JSON Schema
// conjoins keywords, so the families are read together rather than first-match:
// a schema admits null when one family puts it in the value space and no other
// takes it out.
type nullVerdict int

const (
	// nullSilent is a family the schema does not declare, or one that constrains
	// only non-null instances. It forbids nothing.
	nullSilent nullVerdict = iota
	// nullAdmitted is a family that puts null in the value space.
	nullAdmitted
	// nullForbidden is a family that takes null out of it.
	nullForbidden
)

// maxNullConjuncts bounds the walk below (styleguide bounded-recursion rule).
// One step is spent per schema the walk reads, each conjunct and $ref
// site among them, so it caps the walk's depth as well as its breadth: an
// `allOf` naming its own schema, or a diamond of conjunctions, terminates on it
// rather than on the shape of the source. A walk deeper or wider than this
// reads as silent, the answer that claims the least. The aliases a $ref chain
// passes are read without it, in a loop the chain bounds (lands).
const maxNullConjuncts = 256

// nullReading is what a schema says about null under each reading
// (docs/compilers/openapi.md#nullable): declared, as the document's version
// defines its keywords, and lenient, as the nullable-widening policy reads
// them, a null spelled by `nullable` or beside a $ref widening what it is
// written on. The two differ only there, so lenient admits wherever declared
// does.
type nullReading struct {
	declared, lenient nullVerdict
}

// alike is the reading of a family that reads the same under both.
func alike(v nullVerdict) nullReading { return nullReading{declared: v, lenient: v} }

// foldNullReadings conjoins readings, each reading on its own.
func foldNullReadings(a, b nullReading) nullReading {
	return nullReading{
		declared: foldNullVerdicts(a.declared, b.declared),
		lenient:  foldNullVerdicts(a.lenient, b.lenient),
	}
}

// admits reports whether the reading c's policy selects admits null: lenient
// while the policy is on, declared once it is off.
func (r nullReading) admits(c lowering.Ctx) bool {
	if c.NullableWidening.Honours() {
		return r.lenient == nullAdmitted
	}
	return r.declared == nullAdmitted
}

// ref returns a reference to target that admits null as c's policy reads it
// (nullRef).
func (r nullReading) ref(c lowering.Ctx, target ir.TypeID) ir.TypeRef {
	return nullRef(c, target, r.declared == nullAdmitted, r.lenient == nullAdmitted)
}

// nullRef returns a reference to target that admits null where the reading c's
// policy selects does: lenient while it is on, declared once it is off. Where
// only lenient admits null, the reference names the policy's heuristic in
// Inferred, so the declared bit is still Nullable with Inferred empty.
func nullRef(c lowering.Ctx, target ir.TypeID, declared, lenient bool) ir.TypeRef {
	if !c.NullableWidening.Honours() {
		return ir.TypeRef{Target: target, Nullable: declared}
	}
	out := ir.TypeRef{Target: target, Nullable: lenient}
	if lenient && !declared {
		out.Inferred = lowering.NullableWideningHeuristic
	}
	return out
}

// schemaAdmitsNull reports whether a schema admits the null value as c's policy
// reads it. Lowering lifts every spelling of that onto the enclosing TypeRef
// rather than into the type node, so the walk below is the one reading every
// site computing a Nullable bit goes through: a definition site, a union, an
// allOf conjunct and a $ref use site must never disagree about one schema. It
// reports nothing; schemaNullRef is the reading a position takes its own bit
// from.
func schemaAdmitsNull(c lowering.Ctx, s *oas3.Schema) bool {
	n := newNulls(c)
	return n.schema(s, nullSite{}).admits(c)
}

// schemaNullRef returns a reference to target from s, a schema that writes no
// $ref, carrying what s says about null, and reports each null the policy reads
// in where site's position writes it (nullSite).
func schemaNullRef(c lowering.Ctx, target ir.TypeID, s *oas3.Schema, site nullSite) (ir.TypeRef, []ir.Diagnostic) {
	n := newNulls(c)
	return n.schema(s, site).ref(c, target), n.findings(c)
}

// refNullRef is schemaNullRef for a $ref usage, read across the reference.
func refNullRef(c lowering.Ctx, target ir.TypeID, js *oas3.JSONSchema[oas3.Referenceable], site nullSite) (ir.TypeRef, []ir.Diagnostic) {
	n := newNulls(c)
	return n.ref(js, site).ref(c, target), n.findings(c)
}

// nullSite is where a schema the walk reads is written, for a report about a
// null the policy reads in there, or none. A schema read across a $ref has
// none: its own position lowers it, and reports it there, so a report is made
// once per schema whatever references it.
type nullSite struct {
	at jsontext.Pointer
	ok bool
}

// siteAt is the site of the schema written at pointer.
func siteAt(pointer jsontext.Pointer) nullSite { return nullSite{at: pointer, ok: true} }

// child is the site of a schema written under s at the tokens.
func (s nullSite) child(tokens ...string) nullSite {
	if !s.ok {
		return s
	}
	return siteAt(s.at + ids.Ptr(tokens...))
}

// nulls reads what schemas say about null: across each $ref along its chain,
// an alias's own keywords included, within one budget for the whole read,
// conjuncts.
type nulls struct {
	refs      *typed.Bindings
	is30      bool
	conjuncts budget.Budget
	// declared is the version the document declares, which a report names.
	declared string
	// found is each null the lenient reading reads in where the declared one
	// leaves its schema without null, at a site the walk has.
	found []nullFinding
}

// nullFinding is one null the policy reads in: where the schema spelling it is
// written, and the report's message.
type nullFinding struct {
	at      jsontext.Pointer
	message string
}

// newNulls returns a read with the whole budget, over c's bindings and in the
// dialect c's document declares. It returns a value so the read lives on its
// caller's stack, as one per position read is.
func newNulls(c lowering.Ctx) nulls {
	return nulls{refs: c.Bindings(), is30: c.Is30(), conjuncts: budget.New(budget.AtMost(maxNullConjuncts)),
		declared: c.Doc.GetOpenAPI()}
}

// findings returns what n found as diagnostics, or none while c's policy is
// off: a null then admits only what its version says it does, and nothing is
// read in for a report to say so.
func (n *nulls) findings(c lowering.Ctx) []ir.Diagnostic {
	if !c.NullableWidening.Honours() || len(n.found) == 0 {
		return nil
	}
	out := make([]ir.Diagnostic, 0, len(n.found))
	for _, f := range n.found {
		out = append(out, c.DiagAt(ir.SeverityInfo, diag.InferredNullable, f.at, "%s", f.message))
	}
	return out
}

// schema reads a whole schema that writes no $ref, by conjoining what each of
// its keyword families says (keywords), then widening it where it writes
// `nullable: true`.
func (n *nulls) schema(s *oas3.Schema, site nullSite) nullReading {
	if s == nil || !n.conjuncts.Spend(1) {
		return alike(nullSilent)
	}
	return n.widen(s, n.keywords(s, site), site)
}

// ref reads a $ref usage as JSON Schema reads it: the keywords beside the $ref
// conjoined with what it lands on (lands). The target is read here because one
// interned at its own ID discards the reference its definition produced.
func (n *nulls) ref(js *oas3.JSONSchema[oas3.Referenceable], site nullSite) nullReading {
	s := js.GetSchema()
	if s == nil || !n.conjuncts.Spend(1) {
		return alike(nullSilent)
	}
	return n.usage(s, n.lands(js), site)
}

// usage is s, a schema writing a $ref, read over target, what the $ref lands
// on: s's own keywords conjoined with it, which the lenient reading widens
// where they admit null (widenRef) or write `nullable: true` (widen).
func (n *nulls) usage(s *oas3.Schema, target nullReading, site nullSite) nullReading {
	own := n.keywords(s, site)
	return n.widen(s, n.widenRef(own, foldNullReadings(own, target), site), site)
}

// lands reads what js's $ref lands on along the chain the bindings resolved:
// the schema it ends on, read over by each alias it passes on the way, so an
// alias's own keywords count as well as the end's. A chain that ends on no
// schema says nothing. The hops are walked in a loop the chain's own length
// bounds, not from the read's budget, so a long chain of aliases reads as a
// short one does; what each alias conjoins still spends it.
func (n *nulls) lands(js *oas3.JSONSchema[oas3.Referenceable]) nullReading {
	e, ok := n.refs.Entry(js)
	if !ok || e.Chain.Status != bind.Resolved {
		return alike(nullSilent)
	}
	var aliases []*oas3.Schema
	hop := n.refs.Declared(js)
	for i := 1; i < e.Chain.Hops && hop != nil && resolve.IsRefSite(hop, hop.GetSchema()); i++ {
		aliases = append(aliases, hop.GetSchema())
		hop = n.refs.Declared(hop)
	}
	r := n.conjunct(n.refs.At(e.Chain.End), nullSite{})
	for _, s := range slices.Backward(aliases) {
		r = n.usage(s, r, nullSite{})
	}
	return r
}

// widenRef is the lenient reading of keywords beside a $ref that admit null
// on their own, own, over the usage r they conjoin with what the $ref lands
// on: "this reference, or null", as `nullable: true` beside a $ref is meant and
// `type: [T, "null"]` is its spelling once nullable is gone. JSON Schema
// conjoins the two, so where the target excludes null the declared usage does
// too; one admitting null only so is noted at site.
func (n *nulls) widenRef(own, r nullReading, site nullSite) nullReading {
	if own.lenient != nullAdmitted || r.lenient == nullAdmitted {
		return r
	}
	// The declared reading admits null no more than the lenient one, which
	// does not here, so the usage admits it only so.
	if site.ok {
		n.found = append(n.found, nullFinding{at: site.at, message: "the keywords beside this $ref admit null " +
			"and what it lands on does not, which JSON Schema conjoins to no null, but the nullable-widening " +
			"policy reads them as admitting null for the whole reference; a reference whose null rests on " +
			"them is marked inferred"})
	}
	r.lenient = nullAdmitted
	return r
}

// keywords conjoins what s's own keyword families say, its allOf conjuncts
// among them but not a $ref it writes. Only `type` has a rule of its own for
// each reading, the nullable keyword's declared effect on it
// (declaredTypeVerdict); the conjuncts differ only as each is read.
func (n *nulls) keywords(s *oas3.Schema, site nullSite) nullReading {
	c, e, u := constNullVerdict(s), enumNullVerdict(s), unionNullVerdict(s)
	all := n.allOf(s, site)
	return nullReading{
		declared: foldNullVerdicts(n.declaredTypeVerdict(s), c, e, u, all.declared),
		lenient:  foldNullVerdicts(typeNullVerdict(s), c, e, u, all.lenient),
	}
}

// widen is the lenient reading of `nullable: true` on s: it admits null for
// the whole of s, whatever else s says, where the declared reading makes it a
// member of a `type` that the rest of s conjoins with. A keyword that admits
// null only so is noted at site, with why its version does not.
func (n *nulls) widen(s *oas3.Schema, r nullReading, site nullSite) nullReading {
	if s.Nullable == nil || !*s.Nullable {
		return r
	}
	if r.declared != nullAdmitted && site.ok {
		n.found = append(n.found, nullFinding{at: site.at + ids.Ptr("nullable"), message: "nullable: true is " +
			"read as admitting null under the nullable-widening policy, though " + n.why(s) +
			"; a reference whose null rests on it is marked inferred"})
	}
	r.lenient = nullAdmitted
	return r
}

// why says why the declared reading of s leaves it without null although s
// writes `nullable: true`. Beside a $ref no version gives the keyword an
// effect, so that reason names none. The last case is the one left once s
// writes a `type` the keyword adds null to: a family the type conjoins with
// forbids it, and only const, enum and allOf ever forbid with a type that
// admits null.
func (n *nulls) why(s *oas3.Schema) string {
	switch {
	case s.Ref != nil:
		return "no OpenAPI version gives it an effect beside a $ref: 3.0 ignores every keyword there, " +
			"and 3.1, which applies them, defines no nullable"
	case !n.is30:
		return fmt.Sprintf("this document declares OpenAPI %s, whose schemas define no nullable keyword", n.declared)
	case len(declaredTypes(s)) == 0:
		return "OpenAPI 3.0 applies nullable only to a type written in the same Schema Object, " +
			"and this schema writes none"
	case enumNullVerdict(s) == nullForbidden:
		return "this schema's enum lists no null, and OpenAPI 3.0's nullable does not override it"
	case constNullVerdict(s) == nullForbidden:
		return "this schema's const is not null, and OpenAPI 3.0's nullable does not override it"
	default:
		return "an allOf branch of this schema excludes null, and OpenAPI 3.0's nullable does not override it"
	}
}

// declaredTypeVerdict is typeNullVerdict as the document's version reads a
// `nullable: true` beside the type. OpenAPI 3.0 adds null to a type written in
// the same Schema Object, as `type: [T, "null"]` spells it from 3.1, so the
// result then conjoins like any type. Beside a $ref, whose keywords 3.0 reads
// as 3.1 does, and from 3.1, nullable is no keyword of the dialect.
func (n *nulls) declaredTypeVerdict(s *oas3.Schema) nullVerdict {
	v := typeNullVerdict(s)
	if v == nullForbidden && n.is30 && s.Ref == nil && s.Nullable != nil && *s.Nullable {
		return nullAdmitted
	}
	return v
}

// foldNullVerdicts conjoins keyword verdicts: one forbidding family decides the
// schema, otherwise one admitting family does, and a schema no family speaks for
// stays silent — which is not the same answer as forbidding, since a silent
// conjunct must not veto a sibling that admits.
func foldNullVerdicts(verdicts ...nullVerdict) nullVerdict {
	out := nullSilent
	for _, v := range verdicts {
		if v == nullForbidden {
			return nullForbidden
		}
		if v == nullAdmitted {
			out = nullAdmitted
		}
	}
	return out
}

// typeNullVerdict reads the `type` keyword. A schema writing none constrains no
// instance kind at all, so it is silent rather than forbidding.
func typeNullVerdict(s *oas3.Schema) nullVerdict {
	types := declaredTypes(s)
	if len(types) == 0 {
		return nullSilent
	}
	if slices.Contains(types, oas3.SchemaTypeNull) {
		return nullAdmitted
	}
	return nullForbidden
}

// constNullVerdict reads `const`, which fixes the value space to one member.
func constNullVerdict(s *oas3.Schema) nullVerdict {
	node := s.GetConst()
	if node == nil {
		return nullSilent
	}
	if isNullValue(node) {
		return nullAdmitted
	}
	return nullForbidden
}

// enumNullVerdict reads `enum`, which fixes the value space to its members: the
// position admits null exactly when a member is null.
//
// An empty enum is silent rather than forbidding. It lists no member at all, so
// reading it as "no null member" would let a degenerate keyword strip a
// co-declared type array's null; what an empty enum lowers to is GitHub #278's
// question, and this rule leaves it open.
func enumNullVerdict(s *oas3.Schema) nullVerdict {
	nodes := s.GetEnum()
	if len(nodes) == 0 {
		return nullSilent
	}
	if slices.ContainsFunc(nodes, isNullValue) {
		return nullAdmitted
	}
	return nullForbidden
}

// isNullValue reports whether an enum member or const node is the null literal,
// read through the same converter enumMembers drops a member by. Both must
// recognize one spelling: the null this predicate lifts onto a reference is
// exactly the member that lowering strips.
func isNullValue(node values.Value) bool {
	val, err := value.FromNode(node)
	return err == nil && val.Kind == ir.ValueNull
}

// unionNullVerdict reads a oneOf/anyOf null branch, which counts only when the
// union is the type itself. Structural siblings intersect with the union, so
// `{type: object, oneOf: [{type: string}, {type: null}]}` admits neither string
// nor null, and that union is kept verbatim under Unmodeled. An inline
// `type: null` branch also blocks distribution, so no distributed union can
// strip a null branch from under this verdict.
//
// It never forbids: a union with no null branch says nothing about a null that
// a sibling keyword admits, as in `{nullable: true, oneOf: [...]}`.
func unionNullVerdict(s *oas3.Schema) nullVerdict {
	if oneOfAnyOfHasNull(s) && !hasUnionSiblings(s) {
		return nullAdmitted
	}
	return nullSilent
}

// allOf conjoins what the allOf branches say. A conjunction admits null when a
// branch does and none forbids it, which is what makes `{allOf: [{$ref: T}]}`
// answer the same as `{$ref: T}`: the composition declares no nullability of its
// own, and the usage naming it has nowhere else to derive the bit from (GitHub
// #279). Model.Base and Mixins still carry no Nullable bit: they name a
// conjunct, and nullability is a property of the usage that names the
// conjunction.
func (n *nulls) allOf(s *oas3.Schema, site nullSite) nullReading {
	out := alike(nullSilent)
	for i, b := range s.GetAllOf() {
		out = foldNullReadings(out, n.conjunct(b, site.child("allOf", strconv.Itoa(i))))
	}
	return out
}

// conjunct reads one allOf branch, or what a $ref lands on. A `false` schema
// admits no instance whatever, null included; a `true` one constrains nothing,
// nor does a `null` written for one, which is no `false`
// (annotation.IsFalseSchema).
func (n *nulls) conjunct(b *oas3.JSONSchema[oas3.Referenceable], site nullSite) nullReading {
	if b == nil {
		return alike(nullSilent)
	}
	if b.IsBool() {
		if annotation.IsFalseSchema(b) {
			return alike(nullForbidden)
		}
		return alike(nullSilent)
	}
	s := b.GetSchema()
	if resolve.IsRefSite(b, s) {
		return n.ref(b, site)
	}
	return n.schema(s, site)
}
