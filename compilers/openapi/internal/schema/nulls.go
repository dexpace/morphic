// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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
// defines its keywords, and lenient, as the nullable-inference policy reads
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
	if c.NullableInference.Honours() {
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
	if !c.NullableInference.Honours() {
		return ir.TypeRef{Target: target, Nullable: declared}
	}
	out := ir.TypeRef{Target: target, Nullable: lenient}
	if lenient && !declared {
		out.Inferred = lowering.NullableInferenceHeuristic
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
func schemaAdmitsNull(c lowering.Ctx, s *statement.Schema) bool {
	n := newNulls(c)
	return n.schema(s, nullSite{}).admits(c)
}

// schemaNullRef returns a reference to target from s, a schema that writes no
// $ref, carrying what s says about null, and reports each null the policy reads
// in where site's position writes it (nullSite).
func schemaNullRef(c lowering.Ctx, target ir.TypeID, s *statement.Schema, site nullSite) (ir.TypeRef, []ir.Diagnostic) {
	n := newNulls(c)
	return n.schema(s, site).ref(c, target), n.findings(c)
}

// refNullRef is schemaNullRef for a $ref usage, read across the reference.
func refNullRef(c lowering.Ctx, target ir.TypeID, js *statement.Statement, site nullSite) (ir.TypeRef, []ir.Diagnostic) {
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
	c    lowering.Ctx
	refs *typed.Bindings
	// nullable reports whether the dialect of the schema being read defines the
	// nullable keyword (oas.SchemaRules.Defines), the one question of the
	// version this read asks.
	nullable  bool
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

// newNulls returns a read with the whole budget, over c's bindings and in the
// dialect c reads its schemas in. It returns a value so the read lives on its
// caller's stack, as one per position read is.
func newNulls(c lowering.Ctx) nulls {
	return nulls{c: c, refs: c.Bindings(), nullable: c.SchemaRules().Defines("nullable"),
		conjuncts: budget.New(budget.AtMost(maxNullConjuncts))}
}

// findings returns what n found as diagnostics, or none while c's policy is
// off: a null then admits only what its version says it does, and nothing is
// read in for a report to say so.
func (n *nulls) findings(c lowering.Ctx) []ir.Diagnostic {
	if !c.NullableInference.Honours() || len(n.found) == 0 {
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
// `nullable: true` or an inline allOf branch speaking for it does.
func (n *nulls) schema(s *statement.Schema, site nullSite) nullReading {
	m := statement.Parsed(s)
	if m == nil || !n.conjuncts.Spend(1) {
		return alike(nullSilent)
	}
	own := n.keywords(s, site)
	return n.widenByBranch(s, n.widen(s, own, own, site), site)
}

// ref reads a $ref usage as JSON Schema reads it: the keywords beside the $ref
// conjoined with what it lands on (lands). The target is read here because one
// interned at its own ID discards the reference its definition produced.
func (n *nulls) ref(js *statement.Statement, site nullSite) nullReading {
	m := statement.ParsedAt(js)
	s := m.GetSchema()
	if s == nil || !n.conjuncts.Spend(1) {
		return alike(nullSilent)
	}
	return n.usage(js.Schema, n.lands(js), site)
}

// usage is s, a schema writing a $ref, read over target, what the $ref lands
// on: s's own keywords conjoined with it, which the lenient reading widens
// where they admit null (widenRef) or write `nullable: true` (widen).
func (n *nulls) usage(s *statement.Schema, target nullReading, site nullSite) nullReading {
	own := n.keywords(s, site)
	return n.widen(s, own, n.widenRef(own, foldNullReadings(own, target), site), site)
}

// lands reads what js's $ref lands on along the chain the bindings resolved:
// the schema it ends on, read over by each alias it passes, each in its own
// document's dialect. A chain that ends on no schema says nothing. What each
// reference site lands on is read once per compile (landing): the hops are
// walked forward to the first already read, on a stack the chain's length
// bounds, then read backward, each once, so n sites of one chain cost n
// readings, not n squared over two.
func (n *nulls) lands(js *statement.Statement) nullReading {
	m := statement.ParsedAt(js)
	e, ok := n.refs.Entry(m)
	if !ok || e.Chain.Status != bind.Resolved {
		return alike(nullSilent)
	}
	sites := []*landing{landingOf(n.c, js)}
	var aliases []nullHop
	hop, at := n.refs.DeclaredAt(m)
	for i := 1; i < e.Chain.Hops && hop != nil && n.refs.RefSite(hop) && !sites[len(sites)-1].kept; i++ {
		aliases = append(aliases, nullHop{s: n.c.Statements().Of(hop).Schema, nullable: definesNullableIn(n.c, at.Doc)})
		sites = append(sites, landingOf(n.c, n.c.Statements().Of(hop)))
		hop, at = n.refs.DeclaredAt(hop)
	}
	// Each read below is in its own document's dialect, and the walk returns to
	// its own on every way out.
	defer func(outer bool) { n.nullable = outer }(n.nullable)
	last := sites[len(sites)-1]
	if !last.kept {
		n.nullable = definesNullableIn(n.c, e.Chain.End.Doc)
		n.keep(last, n.conjunct(n.c.Statements().Of(n.refs.At(e.Chain.End)), nullSite{}))
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

// landedOn keys the landing of a reference site in the compile's readings.
type landedOn struct {
	js *statement.Statement
}

// landsRead keys the count of hop readings lands has made.
type landsRead struct{}

// landingOf returns the landing of the reference site js, one per compile.
func landingOf(c lowering.Ctx, js *statement.Statement) *landing {
	return lowering.Remembered(c, landedOn{js}, func() *landing { return &landing{} })
}

// landingCount returns the count of hop readings lands has made in the
// compile, which only the tests read.
func landingCount(c lowering.Ctx) *int {
	return lowering.Remembered(c, landsRead{}, func() *int { return new(int) })
}

// keep notes r as what l lands on, kept unless the read's conjunct budget ran
// out on the way.
func (n *nulls) keep(l *landing, r nullReading) {
	l.reading, l.kept = r, !n.conjuncts.Exhausted()
}

// nullHop is an alias a $ref chain passes, and whether the dialect of the
// document holding it defines nullable.
type nullHop struct {
	s        *statement.Schema
	nullable bool
}

// definesNullableIn reports whether the dialect the schemas of document doc
// are read in defines the nullable keyword.
func definesNullableIn(c lowering.Ctx, doc bind.DocID) bool {
	return c.Rules().In(doc).Defines("nullable")
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
			"and what it lands on does not, which JSON Schema conjoins to no null, but the nullable-inference " +
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
func (n *nulls) keywords(s *statement.Schema, site nullSite) nullReading {
	v := valueNulls(n.c, s)
	own := foldNullReadings(n.typed(s, v.constant, v.enum), v.union)
	return foldNullReadings(own, n.allOf(s, site))
}

// typed is what s's type says of null under each reading, conjoined with the
// verdicts of its const and enum.
func (n *nulls) typed(s *statement.Schema, constant, enum nullVerdict) nullReading {
	return nullReading{
		declared: foldNullVerdicts(n.declaredTypeVerdict(s), constant, enum),
		lenient:  foldNullVerdicts(typeNullVerdict(s), constant, enum),
	}
}

// valueNullsOf keys what valueNulls read of one schema.
type valueNullsOf struct {
	schema *statement.Schema
}

// valueVerdicts is what a schema's const, enum and union families say of null.
// Each reads only the schema, the same whichever site asks; the union's is read
// under each reading, as its null branches are.
type valueVerdicts struct {
	constant, enum nullVerdict
	union          nullReading
}

// valueNulls returns s's valueVerdicts, read once a run: every $ref to s asks,
// and the enum and union families scan each member and branch, so reading them
// at each reference would cost the references times the members.
func valueNulls(c lowering.Ctx, s *statement.Schema) valueVerdicts {
	return lowering.Remembered(c, valueNullsOf{s}, func() valueVerdicts {
		return valueVerdicts{constant: constNullVerdict(s), enum: enumNullVerdict(s), union: unionNullReading(c, s)}
	})
}

// widen is the lenient reading of `nullable: true` on s: it admits null for
// the whole of s, whatever else s says, where the declared reading makes it a
// member of a `type` that the rest of s conjoins with. own is what s's own
// keywords say, and r what s says in all. A keyword that admits null only so
// is noted at site, with why its version does not.
func (n *nulls) widen(s *statement.Schema, own, r nullReading, site nullSite) nullReading {
	m := statement.Parsed(s)
	if m.Nullable == nil || !*m.Nullable {
		return r
	}
	if r.declared != nullAdmitted && site.ok {
		n.found = append(n.found, nullFinding{at: site.at + ids.Ptr("nullable"), message: "nullable: true is " +
			"read as admitting null under the nullable-inference policy, though " + n.why(s, own) +
			"; a reference whose null rests on it is marked inferred"})
	}
	r.lenient = nullAdmitted
	return r
}

// why says why the declared reading of s leaves it without null although s
// writes `nullable: true`, own being what s's own keywords say. Once s writes
// a `type` the keyword adds null to, only an enum, a const or an allOf beside
// it forbids it, which leaves own without null, or, for s writing a $ref, what
// that lands on, which own's null conjoins with.
func (n *nulls) why(s *statement.Schema, own nullReading) string {
	switch {
	case !n.nullable:
		return "the dialect this schema is read in defines no nullable keyword"
	case !writesType(s):
		return "OpenAPI 3.0 applies nullable only to a type written in the same Schema Object, " +
			"and this schema writes none"
	case enumNullVerdict(s) == nullForbidden:
		return "this schema's enum lists no null, and OpenAPI 3.0's nullable does not override it"
	case constNullVerdict(s) == nullForbidden:
		return "this schema's const is not null, and OpenAPI 3.0's nullable does not override it"
	case own.declared != nullAdmitted:
		return "an allOf branch of this schema excludes null, and OpenAPI 3.0's nullable does not override it"
	default:
		return "what its $ref lands on excludes null, which the keywords beside it conjoin with"
	}
}

// declaredTypeVerdict is typeNullVerdict as the dialect s is read in reads a
// `nullable: true` beside the type. Where it defines the keyword, as 3.0 does,
// it adds null to a type written in the same Schema Object, beside a $ref as
// anywhere, as `type: [T, "null"]` spells it from 3.1, so the result then
// conjoins like any type. Where it does not, nullable says nothing.
func (n *nulls) declaredTypeVerdict(s *statement.Schema) nullVerdict {
	m := statement.Parsed(s)
	v := typeNullVerdict(s)
	if v == nullForbidden && n.nullable && m.Nullable != nil && *m.Nullable {
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
func typeNullVerdict(s *statement.Schema) nullVerdict {
	m := statement.Parsed(s)
	if !writesType(s) {
		return nullSilent
	}
	if slices.Contains(m.GetType(), oas3.SchemaTypeNull) {
		return nullAdmitted
	}
	return nullForbidden
}

// constNullVerdict reads `const`, which fixes the value space to one member.
func constNullVerdict(s *statement.Schema) nullVerdict {
	m := statement.Parsed(s)
	node := m.GetConst()
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
func enumNullVerdict(s *statement.Schema) nullVerdict {
	m := statement.Parsed(s)
	nodes := m.GetEnum()
	if len(nodes) == 0 {
		return nullSilent
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

// unionNullReading reads the null branches of s's oneOf and anyOf
// (nullAloneReading) under each reading: the union admits null where a branch
// does, so its null rests on the policy where only a branch's lenient reading
// admits it. A null branch counts only when the union is the type itself:
// structural siblings intersect with the union, which is then kept verbatim,
// and block its distribution. It never forbids: a union with no null branch
// says nothing of a null a sibling admits, as in `{nullable: true, oneOf: …}`.
func unionNullReading(c lowering.Ctx, s *statement.Schema) nullReading {
	m := statement.Parsed(s)
	out := alike(nullSilent)
	for _, b := range slices.Concat(m.GetOneOf(), m.GetAnyOf()) {
		if !inlineBranch(c.Bindings(), c.Statements().Of(b)) {
			continue
		}
		r, ok := nullAloneReading(c, c.Statements().Of(b).Schema)
		if !ok {
			continue
		}
		if r.declared == nullAdmitted {
			out.declared = nullAdmitted
		}
		if r.lenient == nullAdmitted {
			out.lenient = nullAdmitted
		}
	}
	// The lenient reading admits wherever the declared one does, so a union it
	// leaves silent is silent under both, and asks no more of its siblings.
	if out.lenient == nullSilent || hasUnionSiblings(c, s) {
		return alike(nullSilent)
	}
	return out
}

// NullAlone reports whether s admits null and no other value by its own
// keywords, as c's policy reads null (docs/compilers/openapi.md#null-alone).
// Such a schema lowers to an Enum with no member behind a nullable reference,
// and a union branch written so is the union's null.
func NullAlone(c lowering.Ctx, s *statement.Schema) bool {
	r, ok := nullAloneReading(c, s)
	return ok && r.admits(c)
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

// nullAloneReading is what s, a schema whose own keywords leave it no value but
// null (leavesOnlyNull), says of that null under each reading, and false for
// any other schema. It writes no union, allOf or $ref, so its type, const and
// enum, widened where it writes `nullable: true`, are all a read of the whole
// schema finds (schemaAdmitsNull), read here without asking valueNulls, which
// reads a union's branches by this. Every reader of a branch asks, so it is
// read once a run.
func nullAloneReading(c lowering.Ctx, s *statement.Schema) (nullReading, bool) {
	r := lowering.Remembered(c, nullAloneOf{s}, func() nullAloneRead {
		if !leavesOnlyNull(c, s) {
			return nullAloneRead{}
		}
		n := newNulls(c)
		own := n.typed(s, constNullVerdict(s), enumNullVerdict(s))
		return nullAloneRead{reading: n.widen(s, own, own, nullSite{}), ok: true}
	})
	return r.reading, r.ok
}

// leavesOnlyNull reports whether the keywords s itself writes leave it no value
// but null, whether or not they admit that one: it writes no $ref, allOf,
// oneOf, anyOf or $dynamicRef, which admit what other schemas do; an enum it
// writes lists null and nothing else, within the member budget an enum past
// which lowers to the top type (lowerEnum); and it writes a const, which the
// null reading holds to null, such an enum, or a type naming null alone.
func leavesOnlyNull(c lowering.Ctx, s *statement.Schema) bool {
	m := statement.Parsed(s)
	if m == nil || m.Ref != nil || len(m.GetAllOf()) > 0 || declaresUnion(s) || declaresDynamicRef(s) {
		return false
	}
	if enumWritten(s) {
		members := m.GetEnum()
		return len(members) > 0 && c.Limits.MaxEnumMembers.Admits(len(members)) &&
			!slices.ContainsFunc(members, func(v values.Value) bool { return !isNullValue(v) })
	}
	return m.GetConst() != nil || writesType(s) && len(effectiveTypes(s)) == 0
}

// allOf conjoins what the allOf branches say. A conjunction admits null when a
// branch does and none forbids it, which is what makes `{allOf: [{$ref: T}]}`
// answer the same as `{$ref: T}`: the composition declares no nullability of its
// own, and the usage naming it has nowhere else to derive the bit from (GitHub
// #279). Model.Base and Mixins still carry no Nullable bit: they name a
// conjunct, and nullability is a property of the usage that names the
// conjunction.
func (n *nulls) allOf(s *statement.Schema, site nullSite) nullReading {
	m := statement.Parsed(s)
	out := alike(nullSilent)
	for i, b := range m.GetAllOf() {
		out = foldNullReadings(out, n.conjunct(n.c.Statements().Of(b), site.child("allOf", strconv.Itoa(i))))
	}
	return out
}

// widenByBranch is the lenient reading of a `nullable: true` that folds: written
// on an inline branch that speaks for the position, it says what the keyword
// beside the allOf says, and widens all of s (widen), a `type` beside the allOf
// included, where the declared reading leaves it conjoined. A branch whose own
// keywords admit null said so by themselves, so widen said nothing of it, and it
// is noted here, where s is written. A 3.1 `type: [T, "null"]` on a conjunct
// stays conjoined, which beside a $ref it does not
// (docs/compilers/openapi.md#allof-refinement).
func (n *nulls) widenByBranch(s *statement.Schema, r nullReading, site nullSite) nullReading {
	for _, e := range nullableVoices(n.c, s) {
		r.lenient = nullAdmitted
		if !site.ok {
			break // a read across a $ref reports nothing, so one widening is all it needs
		}
		if r.declared != nullAdmitted && n.keywords(e.schema.Schema, nullSite{}).declared == nullAdmitted {
			n.found = append(n.found, nullFinding{at: site.at + e.pointer + ids.Ptr("nullable"), message: "nullable: true " +
				"is read as admitting null under the nullable-inference policy, though another conjunct of this allOf " +
				"excludes null, and OpenAPI 3.0's nullable does not override it; a reference whose null rests on it " +
				"is marked inferred"})
		}
	}
	return r
}

// nullableVoicesOf keys what nullableVoices read of one schema.
type nullableVoicesOf struct {
	schema *statement.Schema
}

// nullableVoices returns the branches of s's flattened allOf, each at its pointer
// under s's, that speak for s, write `nullable: true` and write no properties
// (foldsNullable). Every use of s asks, so it is read once a run. A branch writing
// `false` widens nothing, though it speaks where the only `true` beside it is on a
// $ref branch, which no fold compares.
func nullableVoices(c lowering.Ctx, s *statement.Schema) []allOfBranch {
	return lowering.Remembered(c, nullableVoicesOf{s}, func() []allOfBranch {
		if !allOfDecides(c, s) {
			return nil
		}
		comp := allOfBranches(c, s, "")
		var out []allOfBranch
		for i, speaks := range voicesAmong(c, s) {
			e := comp.branches[i]
			if bs := statement.ParsedAt(e.schema).GetSchema(); speaks && bs.Nullable != nil && *bs.Nullable && foldsNullable(e.schema.Schema) {
				out = append(out, e)
			}
		}
		return out
	})
}

// conjunct reads one allOf branch, or what a $ref lands on. A `false` schema
// admits no instance whatever, null included; a `true` one constrains nothing,
// nor does a `null` written for one, which is no `false`
// (annotation.IsFalseSchema).
func (n *nulls) conjunct(b *statement.Statement, site nullSite) nullReading {
	m := statement.ParsedAt(b)
	if m == nil {
		return alike(nullSilent)
	}
	if m.IsBool() {
		if annotation.IsFalseSchema(m) {
			return alike(nullForbidden)
		}
		return alike(nullSilent)
	}
	if n.refs.RefSite(m) {
		return n.ref(b, site)
	}
	return n.schema(b.Schema, site)
}
