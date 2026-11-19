// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// nullDeclaration is what one keyword family says about the null value. JSON
// Schema conjoins keywords, so the families are read together rather than
// first-match: a schema admits null when one family puts it in the value space
// and no other takes it out.
type nullDeclaration int

const (
	// nullUnconstrained is a family the schema does not declare, or one that
	// constrains only non-null instances. It forbids nothing.
	nullUnconstrained nullDeclaration = iota
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
// defines its keywords, and lenient, as the nullable-inference policy reads
// them, a null spelled by `nullable` or beside a $ref widening what it is
// written on. The two differ only there, so lenient admits wherever declared
// does.
type nullReading struct {
	declared, lenient nullDeclaration
}

// nullReadingAlikeUnderBothPolicies is the reading of a family that reads the
// same under both.
func nullReadingAlikeUnderBothPolicies(nullDeclarationValue nullDeclaration) nullReading {
	return nullReading{declared: nullDeclarationValue, lenient: nullDeclarationValue}
}

// foldNullReadings conjoins readings, each reading on its own.
func foldNullReadings(a, b nullReading) nullReading {
	return nullReading{
		declared: foldNullVerdicts(a.declared, b.declared),
		lenient:  foldNullVerdicts(a.lenient, b.lenient),
	}
}

// admits reports whether the reading loweringContext's policy selects admits
// null: lenient while the policy is on, declared once it is off.
func (r nullReading) admits(loweringContext lowering.Ctx) bool {
	return r.selected(loweringContext) == nullAdmitted
}

// forbids reports whether the reading loweringContext's policy selects keeps
// null out.
func (r nullReading) forbids(loweringContext lowering.Ctx) bool {
	return r.selected(loweringContext) == nullForbidden
}

// selected is the verdict of the reading loweringContext's policy selects.
func (r nullReading) selected(loweringContext lowering.Ctx) nullDeclaration {
	if loweringContext.NullableInference.Honours() {
		return r.lenient
	}
	return r.declared
}

// ref returns a reference to target that admits null as loweringContext's
// policy reads it (nullRef).
func (r nullReading) ref(loweringContext lowering.Ctx, target ir.TypeID) ir.TypeRef {
	return nullRef(loweringContext, target, r.declared == nullAdmitted, r.lenient == nullAdmitted)
}

// nullRef returns a reference to target that admits null where the reading
// loweringContext's policy selects does: lenient while it is on, declared once
// it is off. Where only lenient admits null, the reference names the policy's
// heuristic in Inferred, so the declared bit is still Nullable with Inferred
// empty.
func nullRef(loweringContext lowering.Ctx, target ir.TypeID, declared, lenient bool) ir.TypeRef {
	if !loweringContext.NullableInference.Honours() {
		return ir.TypeRef{Target: target, Nullable: declared}
	}
	out := ir.TypeRef{Target: target, Nullable: lenient}
	if lenient && !declared {
		out.Inferred = lowering.NullableInferenceHeuristic
	}
	return out
}

// schemaAdmitsNull reports whether a schema admits the null value as
// loweringContext's policy reads it. Lowering lifts every spelling of that onto
// the enclosing TypeRef rather than into the type node, so the walk below is
// the one reading every site computing a Nullable bit goes through: a
// definition site, a union, an allOf conjunct and a $ref use site must never
// disagree about one schema. It reports nothing; schemaNullRef is the reading a
// position takes its own bit from.
func schemaAdmitsNull(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	nulls := newNulls(loweringContext)
	return nulls.schema(schema, nullSite{}).admits(loweringContext)
}

// schemaNullRef returns a reference to target from schema, a schema that writes
// no $ref, carrying what schema says about null, and reports each null the
// policy reads in where site's position writes it (nullSite).
func schemaNullRef(loweringContext lowering.Ctx, target ir.TypeID, schema *statement.Schema, site nullSite) (ir.TypeRef, []ir.Diagnostic) {
	nulls := newNulls(loweringContext)
	return nulls.schema(schema, site).ref(loweringContext, target), nulls.findings(loweringContext)
}

// refNullRef is schemaNullRef for a $ref usage, read across the reference.
func refNullRef(loweringContext lowering.Ctx, target ir.TypeID, js *statement.Statement, site nullSite) (ir.TypeRef, []ir.Diagnostic) {
	nulls := newNulls(loweringContext)
	return nulls.ref(js, site).ref(loweringContext, target), nulls.findings(loweringContext)
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
	c lowering.Ctx
	// nullable reports whether the dialect of the schema being read defines the
	// nullable keyword (specrules.SchemaRules.Defines), the one question of the
	// version this read asks.
	nullable bool
	// variants marks a read of whether a schema admits null wherever the IR
	// holds it, a variant included, so a union admits null where any branch
	// does and keeps it out where every branch does (variantsNull), not only
	// where its null lifts onto the reference (unionNullReading); rest is a
	// schema whose own union it leaves out.
	variants  bool
	rest      *statement.Schema
	conjuncts budget.Budget
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

// newNulls returns a read with the whole budget, over loweringContext's
// bindings and in the dialect loweringContext reads its schemas in. It returns
// a value so the read lives on its caller's stack, as one per position read is.
func newNulls(loweringContext lowering.Ctx) nulls {
	return nulls{c: loweringContext, nullable: loweringContext.SchemaRules().Defines("nullable"),
		conjuncts: budget.New(budget.AtMost(maxNullConjuncts))}
}

// findings returns what n found as diagnostics, or none while loweringContext's
// policy is off: a null then admits only what its version says it does, and
// nothing is read in for a report to say so.
func (n *nulls) findings(loweringContext lowering.Ctx) []ir.Diagnostic {
	if !loweringContext.NullableInference.Honours() || len(n.found) == 0 {
		return nil
	}
	out := make([]ir.Diagnostic, 0, len(n.found))
	for _, nullFinding := range n.found {
		out = append(out, loweringContext.DiagAt(ir.SeverityInfo, diag.InferredNullable, nullFinding.at, "%s", nullFinding.message))
	}
	return out
}

// schema reads a whole schema that writes no $ref, by conjoining what each of
// its keyword families says (keywords), then widening it where it writes
// `nullable: true` or an inline allOf branch speaking for it does.
func (n *nulls) schema(schema *statement.Schema, site nullSite) nullReading {
	if !stated(schema) || !n.conjuncts.Spend(1) {
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	}
	if schema.BesideRef != nil {
		// The keywords written beside a $ref are read over what it lands on, as
		// the policy reads its idiom there (docs/compilers/openapi.md#nullable).
		return n.widenByBranch(schema, n.usage(schema, n.lands(n.c.Statements().Read(schema.BesideRef)), site), site)
	}
	own := n.keywords(schema, site)
	return n.widenByBranch(schema, n.widen(schema, own, own, site), site)
}

// ref reads a $ref usage as JSON Schema reads it: the keywords beside the $ref
// conjoined with what it lands on (lands). The target is read here because one
// interned at its own ID discards the reference its definition produced.
func (n *nulls) ref(js *statement.Statement, site nullSite) nullReading {
	if !stated(js.Schema) || !n.conjuncts.Spend(1) {
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	}
	return n.usage(js.Schema, n.lands(js), site)
}

// usage is schema, a schema writing a $ref, read over target, what the $ref
// lands on: schema's own keywords conjoined with it, which the lenient reading
// widens where they admit null (widenRef) or write `nullable: true` (widen). A
// read of variants takes a union's null as its branches' and no keyword's
// beside the $ref, so it widens nothing there.
func (n *nulls) usage(schema *statement.Schema, target nullReading, site nullSite) nullReading {
	typed, union := n.valueParts(schema)
	own := foldNullReadings(typed, n.allOf(schema, site))
	all := foldNullReadings(own, union)
	if !n.variants {
		own = all
	}
	return n.widen(schema, all, n.widenRef(own, foldNullReadings(all, target), site), site)
}

// lands reads what js's $ref lands on along the chain the bindings resolved:
// the schema it ends on, read over by each alias it passes, each in its own
// document's dialect. A chain that ends on no schema says nothing. What each
// reference site lands on is read once per compile (landing): the hops are
// walked forward to the first already read, on a stack the chain's length
// bounds, then read backward, each once, so n sites of one chain cost n
// readings, not n squared over two.
func (n *nulls) lands(js *statement.Statement) nullReading {
	if js.Ref == nil || !js.Ref.Bound || js.Ref.Entry.Chain.Status != reftable.Resolved {
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	}
	referenceSite := js.Ref.Entry
	sites := []*landing{landingOf(n, js)}
	var aliases []nullHop
	hop, at := declaredOf(n.c, js)
	for i := 1; i < referenceSite.Chain.Hops && hop != nil && hop.Ref != nil && !sites[len(sites)-1].kept; i++ {
		aliases = append(aliases, nullHop{s: hop.Schema, nullable: definesNullableIn(n.c, at.Doc)})
		sites = append(sites, landingOf(n, hop))
		hop, at = declaredOf(n.c, hop)
	}
	// Each read below is in its own document's dialect, and the walk returns to
	// its own on every way out.
	defer func(outer bool) { n.nullable = outer }(n.nullable)
	if last := sites[len(sites)-1]; !last.kept {
		end, endDoc := hop, at.Doc
		if hop == nil || hop.Ref != nil {
			// The chain's end, unless a schema writing keys beside its $ref ended
			// the run, the two one conjunction (statement.Schema.BesideRef).
			end, endDoc = n.c.Statements().At(referenceSite.Chain.End), referenceSite.Chain.End.Doc
		}
		n.nullable = definesNullableIn(n.c, endDoc)
		n.keep(last, n.conjunct(end, nullSite{}))
	}
	// Only the last site can be kept already: the walk stops at the first.
	for i, a := range slices.Backward(aliases) {
		n.nullable = a.nullable
		*landingCount(n.c)++
		n.keep(sites[i], n.usage(a.s, sites[i+1].reading, nullSite{}))
	}
	return sites[0].reading
}

// landing is what one reference site lands on, read once per compile: the
// reading, and whether it is kept. A reading the conjunct budget cut short
// is not, so a later read with a budget of its own reads it again.
type landing struct {
	reading nullReading
	kept    bool
}

// landedOn keys the landing of a reference site in the compile's readings, one
// for each way of reading a union (nulls.variants) and a union left out.
type landedOn struct {
	js       *statement.Statement
	rest     *statement.Schema
	variants bool
}

// landsRead keys the count of hop readings lands has made.
type landsRead struct{}

// landingOf returns the landing of the reference site js as nullsValue reads
// it, one per compile.
func landingOf(nullsValue *nulls, js *statement.Statement) *landing {
	return lowering.ReadOncePerRun(nullsValue.c, landedOn{js: js, rest: nullsValue.rest, variants: nullsValue.variants}, func() *landing { return &landing{} })
}

// landingCount returns the count of hop readings lands has made in the
// compile, which only the tests read.
func landingCount(loweringContext lowering.Ctx) *int {
	return lowering.ReadOncePerRun(loweringContext, landsRead{}, func() *int { return new(int) })
}

// keep notes nullReadingValue as what landingValue lands on, kept unless the
// read's conjunct budget ran out on the way.
func (n *nulls) keep(landingValue *landing, nullReadingValue nullReading) {
	landingValue.reading, landingValue.kept = nullReadingValue, !n.conjuncts.Exhausted()
}

// nullHop is an alias a $ref chain passes, and whether the dialect of the
// document holding it defines nullable.
type nullHop struct {
	s        *statement.Schema
	nullable bool
}

// definesNullableIn reports whether the dialect the schemas of document doc
// are read in defines the nullable keyword.
func definesNullableIn(loweringContext lowering.Ctx, doc reftable.DocID) bool {
	return loweringContext.Rules().In(doc).Defines("nullable")
}

// widenRef is the lenient reading of keywords beside a $ref that admit null on
// their own, own, over the usage nullReadingValue they conjoin with what the
// $ref lands on: "this reference, or null", as `nullable: true` beside a $ref
// is meant and `type: [T, "null"]` is its spelling once nullable is gone. JSON
// Schema conjoins the two, so where the target excludes null the declared usage
// does too; one admitting null only so is noted at site.
func (n *nulls) widenRef(own, nullReadingValue nullReading, site nullSite) nullReading {
	if own.lenient != nullAdmitted || nullReadingValue.lenient == nullAdmitted {
		return nullReadingValue
	}
	// The declared reading admits null no more than the lenient one, which
	// does not here, so the usage admits it only so.
	if site.ok {
		n.found = append(n.found, nullFinding{at: site.at, message: "the keywords beside this $ref admit null " +
			"and what it lands on does not, which JSON Schema conjoins to no null, but the nullable-inference " +
			"policy reads them as admitting null for the whole reference; a reference whose null rests on " +
			"them is marked inferred"})
	}
	nullReadingValue.lenient = nullAdmitted
	return nullReadingValue
}

// keywords conjoins what schema's own keyword families say, its allOf conjuncts
// among them but not a $ref it writes. Only `type` has a rule of its own for
// each reading, the nullable keyword's declared effect on it
// (declaredTypeVerdict); the conjuncts differ only as each is read.
func (n *nulls) keywords(schema *statement.Schema, site nullSite) nullReading {
	typed, union := n.valueParts(schema)
	return foldNullReadings(foldNullReadings(typed, union), n.allOf(schema, site))
}

// own is what schema says of null by its own keywords, its unions among them
// and its conjuncts aside, widened as schema widens it.
func (n *nulls) own(schema *statement.Schema) nullReading {
	typed, union := n.valueParts(schema)
	own := foldNullReadings(typed, union)
	return n.widenByBranch(schema, n.widen(schema, own, own, nullSite{}), nullSite{})
}

// valueParts is what schema's type, const and enum say of null, and apart what
// its oneOf and anyOf say, as n reads a union.
func (n *nulls) valueParts(schema *statement.Schema) (typed, union nullReading) {
	var valueVerdictsValue valueVerdicts
	switch {
	case !n.variants:
		valueVerdictsValue = valueNulls(n.c, schema)
	case schema == n.rest:
		valueVerdictsValue = valueSetNulls(n.c, schema)
	default:
		valueVerdictsValue = valueSetNulls(n.c, schema)
		valueVerdictsValue.union = n.variantsNull(schema)
	}
	return n.typed(schema, valueVerdictsValue.constant, valueVerdictsValue.enum), valueVerdictsValue.union
}

// typed is what schema's type says of null under each reading, conjoined with
// the verdicts of its const and enum.
func (n *nulls) typed(schema *statement.Schema, constant, enum nullDeclaration) nullReading {
	return nullReading{
		declared: foldNullVerdicts(n.declaredTypeVerdict(schema), constant, enum),
		lenient:  foldNullVerdicts(typeNullVerdict(schema), constant, enum),
	}
}

// valueNullsOf keys what valueNulls read of one schema, and valueSetNullsOf
// what valueSetNulls did.
type (
	valueNullsOf struct {
		schema *statement.Schema
	}
	valueSetNullsOf struct {
		schema *statement.Schema
	}
)

// valueVerdicts is what a schema's const, enum and union families say of null.
// Each reads only the schema, the same whichever site asks; the union's is read
// under each reading, as its null branches are.
type valueVerdicts struct {
	constant, enum nullDeclaration
	union          nullReading
}

// valueNulls returns schema's valueVerdicts, read once a run: every $ref to
// schema asks, and the enum and union families scan each member and branch, so
// reading them at each reference would cost the references times the members.
func valueNulls(loweringContext lowering.Ctx, schema *statement.Schema) valueVerdicts {
	return lowering.ReadOncePerRun(loweringContext, valueNullsOf{schema}, func() valueVerdicts {
		valueVerdictsValue := valueSetNulls(loweringContext, schema)
		valueVerdictsValue.union = unionNullReading(loweringContext, schema)
		return valueVerdictsValue
	})
}

// valueSetNulls returns what schema's const and enum say of null, read once a
// run, with no union: a read of null on variants reads that otherwise
// (nulls.variantsNull), asking no union whether it is distributed.
func valueSetNulls(loweringContext lowering.Ctx, schema *statement.Schema) valueVerdicts {
	return lowering.ReadOncePerRun(loweringContext, valueSetNullsOf{schema}, func() valueVerdicts {
		return valueVerdicts{constant: constNullVerdict(schema), enum: enumNullVerdict(schema)}
	})
}

// widen is the lenient reading of `nullable: true` on schema: it admits null
// for the whole of schema, whatever else schema says, where the declared
// reading makes it a member of a `type` that the rest of schema conjoins with.
// own is what schema's own keywords say, and nullReadingValue what schema says
// in all. A keyword that admits null only so is noted at site, with why its
// version does not.
func (n *nulls) widen(schema *statement.Schema, own, nullReadingValue nullReading, site nullSite) nullReading {
	if !nullableTrue(schema) {
		return nullReadingValue
	}
	if nullReadingValue.declared != nullAdmitted && site.ok {
		n.found = append(n.found, nullFinding{at: site.at + ids.Ptr("nullable"), message: "nullable: true is " +
			"read as admitting null under the nullable-inference policy, though " + n.why(schema, own) +
			"; a reference whose null rests on it is marked inferred"})
	}
	nullReadingValue.lenient = nullAdmitted
	return nullReadingValue
}

// why says why the declared reading of schema leaves it without null although
// schema writes `nullable: true`, own being what schema's own keywords say.
// Once schema writes a `type` the keyword adds null to, only an enum, a const
// or an allOf beside it forbids it, which leaves own without null, or, for
// schema writing a $ref, what that lands on, which own's null conjoins with.
func (n *nulls) why(schema *statement.Schema, own nullReading) string {
	switch {
	case !n.nullable:
		return "the dialect this schema is read in defines no nullable keyword"
	case !writesType(schema):
		return "OpenAPI 3.0 applies nullable only to a type written in the same Schema Object, " +
			"and this schema writes none"
	case enumNullVerdict(schema) == nullForbidden:
		return "this schema's enum lists no null, and OpenAPI 3.0's nullable does not override it"
	case constNullVerdict(schema) == nullForbidden:
		return "this schema's const is not null, and OpenAPI 3.0's nullable does not override it"
	case own.declared != nullAdmitted:
		return "an allOf branch of this schema excludes null, and OpenAPI 3.0's nullable does not override it"
	default:
		return "what its $ref lands on excludes null, which the keywords beside it conjoin with"
	}
}

// declaredTypeVerdict is typeNullVerdict as the dialect schema is read in reads
// a `nullable: true` beside the type. Where it defines the keyword, as 3.0
// does, it adds null to a type written in the same Schema Object, beside a $ref
// as anywhere, as `type: [T, "null"]` spells it from 3.1, so the result then
// conjoins like any type. Where it does not, nullable says nothing.
func (n *nulls) declaredTypeVerdict(schema *statement.Schema) nullDeclaration {
	nullDeclarationValue := typeNullVerdict(schema)
	if nullDeclarationValue == nullForbidden && n.nullable && nullableTrue(schema) {
		return nullAdmitted
	}
	return nullDeclarationValue
}

// foldNullVerdicts conjoins keyword verdicts: one forbidding family decides the
// schema, otherwise one admitting family does, and a schema no family speaks for
// stays silent — which is not the same answer as forbidding, since a silent
// conjunct must not veto a sibling that admits.
func foldNullVerdicts(verdicts ...nullDeclaration) nullDeclaration {
	out := nullUnconstrained
	for _, nullDeclaration := range verdicts {
		if nullDeclaration == nullForbidden {
			return nullForbidden
		}
		if nullDeclaration == nullAdmitted {
			out = nullAdmitted
		}
	}
	return out
}

// typeNullVerdict reads the `type` keyword. A schema writing none constrains no
// instance kind at all, so it is silent rather than forbidding.
func typeNullVerdict(schema *statement.Schema) nullDeclaration {
	if !writesType(schema) {
		return nullUnconstrained
	}
	if slices.Contains(schema.Types(), statement.KindNull) {
		return nullAdmitted
	}
	return nullForbidden
}

// constNullVerdict reads `const`, which fixes the value space to one member.
func constNullVerdict(schema *statement.Schema) nullDeclaration {
	node := schema.Const()
	if node == nil {
		return nullUnconstrained
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
func enumNullVerdict(schema *statement.Schema) nullDeclaration {
	nodes := schema.Enum()
	if len(nodes) == 0 {
		return nullUnconstrained
	}
	if slices.ContainsFunc(nodes, isNullValue) {
		return nullAdmitted
	}
	return nullForbidden
}

// isNullValue reports whether an enum member or const node is the null literal,
// read through the same converter enumEntries drops a member by. Both must
// recognize one spelling: the null this predicate lifts onto a reference is
// exactly the member that lowering strips.
func isNullValue(node *yaml.Node) bool {
	val, err := value.FromNode(node)
	return err == nil && val.Kind == ir.ValueNull
}

// unionNullReading reads the null branches of schema's oneOf and anyOf
// (nullAloneReading) under each reading: the union admits null where a branch
// does, so its null rests on the policy where only a branch's lenient reading
// admits it. A null branch counts where the union is the type itself or is
// distributed across the structural siblings beside it (buildUnion), their
// verdicts conjoining with it; a union kept verbatim beside them says nothing.
// One union never forbids; a oneOf and an anyOf together forbid null where
// one keeps it out (pairMayAdmitNull).
func unionNullReading(loweringContext lowering.Ctx, schema *statement.Schema) nullReading {
	choice := schema.ChoiceKeywords()
	out := nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	for _, b := range slices.Concat(choice.OneOf, choice.AnyOf) {
		st := loweringContext.Statements().Subschema(b)
		if !inlineBranch(st) {
			continue
		}
		nullReading, ok := nullAloneReading(loweringContext, st.Schema)
		if !ok {
			continue
		}
		if nullReading.declared == nullAdmitted {
			out.declared = nullAdmitted
		}
		if nullReading.lenient == nullAdmitted {
			out.lenient = nullAdmitted
		}
	}
	// The lenient reading admits wherever the declared one does, so a union it
	// leaves silent is silent under both, and asks no more of its siblings.
	if !pairMayAdmitNull(loweringContext, choice) {
		// A oneOf and an anyOf are conjuncts: one keeping null out keeps it
		// out of the position, as on two allOf entries (pairKeepsNullOut).
		return nullReadingAlikeUnderBothPolicies(nullForbidden)
	}
	if out.lenient == nullUnconstrained || hasUnionSiblings(loweringContext, schema) && !distributesUnion(loweringContext, schema) {
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	}
	return out
}

// pairMayAdmitNull reports whether each of a oneOf and an anyOf written
// together may admit null, as an instance of both must, a null being shared
// only where every conjunct may admit it, read by one walk in the reading a
// passed-over union beside a body has (bodyAdmitsNull). One union alone asks
// nothing more.
func pairMayAdmitNull(loweringContext lowering.Ctx, choice statement.Choice) bool {
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return true
	}
	nulls := newNulls(loweringContext)
	nulls.variants = true
	return !pairForbidsNull(&nulls, choice)
}

// pairForbidsNull reports whether nullsValue's walk, whose budget bounds this
// read as the rest of the walk, finds the oneOf or the anyOf of choice keeping
// null out: every branch of it does within that budget.
func pairForbidsNull(nullsValue *nulls, choice statement.Choice) bool {
	for _, branches := range [][]statement.Subschema{choice.OneOf, choice.AnyOf} {
		if nullsValue.branchesNull(branches).forbids(nullsValue.c) && !nullsValue.conjuncts.Exhausted() {
			return true
		}
	}
	return false
}

// distributesUnion reports whether schema, writing a union beside structural
// keywords, lowers to a union of models conjoining its body with each branch
// (classifyUnionSiblings), as read at the position its bindings place it at.
func distributesUnion(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	at, placed := siteOf(schema)
	if !placed {
		return false
	}
	in, _ := lowering.InDocument(loweringContext, at.Doc)
	return classifyUnionSiblings(in, schema, at.Pointer) == unionDistributed
}

// NullAlone reports whether schema admits null and no other value by its own
// keywords, as loweringContext's policy reads null
// (docs/compilers/openapi.md#null-alone). Such a schema lowers to an Enum with
// no member behind a nullable reference, and a union branch written so is the
// union's null.
func NullAlone(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	nullReading, ok := nullAloneReading(loweringContext, schema)
	return ok && nullReading.admits(loweringContext)
}

// nullAloneOf keys what nullAloneReading read of one schema.
type nullAloneOf struct {
	schema *statement.Schema
}

// nullAloneRead is nullAloneReading's answer for one schema.
type nullAloneRead struct {
	reading nullReading
	ok      bool
}

// nullAloneReading is what schema, a schema whose own keywords leave it no
// value but null (leavesOnlyNull), says of that null under each reading, and
// false for any other schema. It writes no union, allOf or $ref, so its type,
// const and enum, widened where it writes `nullable: true`, are all a read of
// the whole schema finds (schemaAdmitsNull), read here without asking
// valueNulls, which reads a union's branches by this. Every reader of a branch
// asks, so it is read once a run.
func nullAloneReading(loweringContext lowering.Ctx, schema *statement.Schema) (nullReading, bool) {
	nullAloneReadValue := lowering.ReadOncePerRun(loweringContext, nullAloneOf{schema}, func() nullAloneRead {
		if !leavesOnlyNull(loweringContext, schema) {
			return nullAloneRead{}
		}
		nulls := newNulls(loweringContext)
		own := nulls.typed(schema, constNullVerdict(schema), enumNullVerdict(schema))
		return nullAloneRead{reading: nulls.widen(schema, own, own, nullSite{}), ok: true}
	})
	return nullAloneReadValue.reading, nullAloneReadValue.ok
}

// leavesOnlyNull reports whether the keywords schema itself writes leave it no
// value but null, whether or not they admit that one: it writes no $ref, allOf,
// oneOf, anyOf or $dynamicRef, which admit what other schemas do; an enum it
// writes lists null and nothing else, within the member budget an enum past
// which lowers to the top type (lowerEnum); and it writes a const, which the
// null reading holds to null, such an enum, or a type naming null alone.
func leavesOnlyNull(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !stated(schema) || refWritten(schema) || writesConjuncts(schema) || declaresUnion(schema) || declaresDynamicRef(schema) {
		return false
	}
	if enumWritten(schema) {
		members := schema.Enum()
		return len(members) > 0 && loweringContext.Limits.MaxEnumMembers.Admits(len(members)) &&
			!slices.ContainsFunc(members, func(node *yaml.Node) bool { return !isNullValue(node) })
	}
	return schema.Const() != nil || writesType(schema) && len(effectiveTypes(schema)) == 0
}

// allOf conjoins what the allOf branches say. A conjunction admits null when a
// branch does and none forbids it, which is what makes `{allOf: [{$ref: T}]}`
// answer the same as `{$ref: T}`: the composition declares no nullability of its
// own, and the usage naming it has nowhere else to derive the bit from (GitHub
// #279). Model.Base and Mixins still carry no Nullable bit: they name a
// conjunct, and nullability is a property of the usage that names the
// conjunction.
func (n *nulls) allOf(schema *statement.Schema, site nullSite) nullReading {
	out := nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	for _, b := range allOfIn(schema) {
		out = foldNullReadings(out, n.conjunct(n.c.Statements().Subschema(b), site.child("allOf", strconv.Itoa(b.Index))))
	}
	if pairKeepsNullOut(n, schema) {
		out = foldNullReadings(out, nullReadingAlikeUnderBothPolicies(nullForbidden))
	}
	return out
}

// pairKeepsNullOut reports whether schema and its inline allOf entries write a
// oneOf and an anyOf between them, no one of them writing both, of which a
// union keeps null out, read by nullsValue's walk (pairForbidsNull): an
// instance of the position is one of each, so that union's null is no value it
// admits. One schema writing both is read by unionNullReading.
func pairKeepsNullOut(nullsValue *nulls, schema *statement.Schema) bool {
	var pair statement.Choice
	note := func(statementSchema *statement.Schema) bool {
		choice := statementSchema.ChoiceKeywords()
		if len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
			return false
		}
		if len(choice.OneOf) > 0 {
			pair.OneOf = choice.OneOf
		}
		if len(choice.AnyOf) > 0 {
			pair.AnyOf = choice.AnyOf
		}
		return true
	}
	if !note(schema) {
		return false
	}
	for _, b := range allOfIn(schema) {
		if st := nullsValue.c.Statements().Subschema(b); inlineBranch(st) && !note(st.Schema) {
			return false
		}
	}
	if len(pair.OneOf) == 0 || len(pair.AnyOf) == 0 {
		return false
	}
	// Each union's null is its branches', as one schema writing both reads it
	// (pairMayAdmitNull), so a branch that is a union keeps null out where its
	// own branches do; the read stays on nullsValue's budget.
	variants := nullsValue.variants
	nullsValue.variants = true
	forbids := pairForbidsNull(nullsValue, pair)
	nullsValue.variants = variants
	return forbids
}

// widenByBranch is the lenient reading of a `nullable: true` that folds:
// written on an inline branch that speaks for the position, it says what the
// keyword beside the allOf says, and widens all of schema (widen), a `type`
// beside the allOf included, where the declared reading leaves it conjoined. A
// branch whose own keywords admit null said so by themselves, so widen said
// nothing of it, and it is noted here, where schema is written. A 3.1
// `type: [T, "null"]` on a conjunct stays conjoined, which beside a $ref it
// does not (docs/compilers/openapi.md#allof-refinement).
func (n *nulls) widenByBranch(schema *statement.Schema, nullReadingValue nullReading, site nullSite) nullReading {
	for _, allOfBranch := range nullableFoldingBranches(n.c, schema) {
		nullReadingValue.lenient = nullAdmitted
		if !site.ok {
			break // a read across a $ref reports nothing, so one widening is all it needs
		}
		if nullReadingValue.declared != nullAdmitted && n.keywords(allOfBranch.schema.Schema, nullSite{}).declared == nullAdmitted {
			n.found = append(n.found, nullFinding{at: site.at + allOfBranch.pointer + ids.Ptr("nullable"), message: "nullable: true " +
				"is read as admitting null under the nullable-inference policy, though another conjunct of this allOf " +
				"excludes null, and OpenAPI 3.0's nullable does not override it; a reference whose null rests on it " +
				"is marked inferred"})
		}
	}
	return nullReadingValue
}

// nullableFoldingBranchesKey keys what nullableFoldingBranches read of one
// schema.
type nullableFoldingBranchesKey struct {
	schema *statement.Schema
}

// nullableFoldingBranches returns the branches of schema's flattened allOf,
// each at its pointer under schema's, that speak for schema, write
// `nullable: true` and write no properties (foldsNullable). Every use of schema
// asks, so it is read once a run. A branch writing `false` widens nothing,
// though it speaks where the only `true` beside it is on a $ref branch, which
// no fold compares.
func nullableFoldingBranches(loweringContext lowering.Ctx, schema *statement.Schema) []allOfBranch {
	return lowering.ReadOncePerRun(loweringContext, nullableFoldingBranchesKey{schema}, func() []allOfBranch {
		if !allOfDecides(loweringContext, schema) {
			return nil
		}
		comp := allOfBranches(loweringContext, schema, "")
		var out []allOfBranch
		for i, speaks := range foldingAllOfBranches(loweringContext, schema) {
			allOfBranch := comp.branches[i]
			if speaks && nullableTrue(allOfBranch.schema.Schema) && foldsNullable(allOfBranch.schema.Schema) {
				out = append(out, allOfBranch)
			}
		}
		return out
	})
}

// conjunct reads one allOf branch, or what a $ref lands on. A `false` schema
// admits no instance whatever, null included; a `true` one constrains nothing,
// nor does a `null` written for one, which is no `false`
// (statement.SchemaValueForm.ReadsFalse).
func (n *nulls) conjunct(b *statement.Statement, site nullSite) nullReading {
	switch {
	case b == nil:
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	case b.Truth.ReadsFalse():
		return nullReadingAlikeUnderBothPolicies(nullForbidden)
	case b.Truth != statement.Stated:
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	case b.Ref != nil:
		return n.ref(b, site)
	default:
		return n.schema(b.Schema, site)
	}
}
