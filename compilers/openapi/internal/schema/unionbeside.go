// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, a composing allOf, const/enum, ...) or a keyword
// that narrows an instance without declaring one. When it does, the union alone
// cannot represent the schema, so the sibling body is lowered too (invariant 2,
// docs/ir/models.md#composition-beside-a-union), unless the branches declare
// shape and say every one of those keywords (branchesSay): the schema then means
// the union alone. Branches declaring none narrow the body, whatever they
// repeat, and a discriminator routing to branches composing the body keeps it,
// their base (composedByBranch). A refining allOf is no body
// (docs/compilers/openapi.md#allof-refinement).
func hasUnionSiblings(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !declaresUnion(schema) || composedByBranch(loweringContext, schema, besideUnion(schema, "")) {
		return writesSiblingKeyword(schema) // no branch says it, or the body is their base
	}
	siblings := siblingKeywords(schema)
	if !allOfComposes(loweringContext, schema) {
		siblings = slices.DeleteFunc(siblings, func(k string) bool { return k == "allOf" })
	}
	if len(siblings) == 0 {
		return false
	}
	said := branchesSay(loweringContext, schema, schema)
	if slices.ContainsFunc(siblings, func(k string) bool { return !slices.Contains(said, k) }) {
		return true
	}
	// A oneOf and an anyOf sharing no value lower as the body beside them
	// where it is no Model (unionPairApart), whatever their branches say.
	return !unionBranchesDeclareShape(loweringContext, schema) || pairSharesNoValue(loweringContext, schema) && !composesAsModel(loweringContext, schema)
}

// restatableKeywords are the keywords beside a union that its branches can say
// in full, read as an admissionReader reads what a schema admits.
var restatableKeywords = []string{"type", "format", "enum", "const", "items"}

// branchesSayOf keys what branchesSay read of one union and the schema
// writing keywords beside it.
type branchesSayOf struct {
	union, beside *statement.Schema
}

// branchesSay returns the keywords of restatableKeywords beside writes that
// every branch of union's oneOf and anyOf already says: a `type` naming each
// kind a branch admits, null included, and a `format`, an `enum`, a `const` or
// an `items` each branch is held to (admissionReader.holds). Such a keyword adds
// nothing to the union, as if it were not written
// (docs/compilers/openapi.md#union-beside-structure); beside is union, or a
// conjunct of the allOf holding it. A read cut short says none, as does a union
// of no branch, which every keyword would pass. Every reader asks, so it is
// read once a run.
func branchesSay(loweringContext lowering.Ctx, union, beside *statement.Schema) []string {
	return lowering.ReadOncePerRun(loweringContext, branchesSayOf{union, beside}, func() []string {
		choice := union.ChoiceKeywords()
		branches := slices.Concat(choice.OneOf, choice.AnyOf)
		if len(branches) == 0 {
			return nil
		}
		admissionReader := newAdmissionReader(loweringContext)
		reads := make([]admitted, len(branches))
		for i, b := range branches {
			reads[i] = lowered(&admissionReader, loweringContext.Statements().Subschema(b))
		}
		if admissionReader.incomplete() {
			return nil
		}
		var out []string
		for _, key := range restatableKeywords {
			if !hasKey(beside, key) {
				continue
			}
			if !slices.ContainsFunc(reads, func(admittedValue admitted) bool { return !branchSays(&admissionReader, admittedValue, beside, key) }) {
				out = append(out, key)
			}
		}
		return out
	})
}

// branchSays reports whether a branch admitting branch, as admissionReaderValue reads it, says all
// that schema's keyword key does. A `type` is compared with null in it, which
// admissionReader.holds leaves to the nulls reader: a union lowered alone lifts a null
// branch onto the position, so a `type` leaving null out says more than a branch
// admitting it. An `items` list says what no single schema does.
func branchSays(admissionReaderValue *admissionReader, branch admitted, schema *statement.Schema, key string) bool {
	switch key {
	case "type":
		return branch.kinds&^writtenKinds(schema, admissionReaderValue.nullable) == 0
	case "items":
		_, listed := listedItems(schema)
		return !listed && admissionReaderValue.holds(branch, schema, key)
	default:
		return admissionReaderValue.holds(branch, schema, key)
	}
}

// unionLowering names how a oneOf/anyOf co-declared with structural keywords is
// lowered.
type unionLowering int

const (
	// unionDistributed distributes the sibling composition across the union
	// variants: one Model per branch carrying the enclosing Base/Mixins and own
	// properties conjoined with that branch. The Union is the value and the
	// composition rides on every variant, which is exactly the ∧-of-∨ the source
	// wrote.
	unionDistributed unionLowering = iota
	// unionValidationOnly marks a union whose branches only narrow what the
	// sibling body accepts; it is kept verbatim under ReasonValidationOnly.
	unionValidationOnly
	// unionSharesNoKind marks a union none of whose branches shares a kind with
	// what the allOf beside it admits (unionAside): no instance satisfies them
	// together, so no variant would hold a value.
	unionSharesNoKind
	// unionUncomposableBody marks a union beside a body that is not a Model — a
	// const, an enum, a scalar, or object keywords with no type where the kinds
	// the position lowers to are not an object alone — so there are no
	// composition fields to distribute into.
	unionUncomposableBody
	// unionUnresolvedBranch marks a union with a $ref branch naming nothing this
	// compilation can resolve — a target in a document it did not read, an
	// undeclared component, a pointer addressing no schema.
	unionUnresolvedBranch
	// unionHierarchyBase marks a union beside a discriminator routing to
	// branches that compose the body through allOf, its subtypes, which a union
	// of composed variants would no longer have for a base
	// (composedByBranch).
	unionHierarchyBase
	// unionRoutedSubtype marks a body a discriminator on an ancestor routes to
	// (routedSubtype). The route names the type the body lowers to, which as a
	// union would be no subtype of that ancestor, so it stays a Model composing
	// the union as a mixin (lowerSubtypeUnion).
	unionRoutedSubtype
	// unionNoLiveBranch marks a union no branch of which an instance of the
	// body satisfies (deadBranch), a null branch aside: distributing it would
	// build no variant a value inhabits.
	unionNoLiveBranch
	// unionPassedApart marks a union beside another the position does not
	// elect, sharing no kind with the body (passedApart): no instance satisfies
	// the two together, whichever branch it takes.
	unionPassedApart
	// unionPassedNull marks a union beside another the position does not
	// elect, the two leaving the body null alone (passedSharesNull): the
	// position admits that null and nothing else, so it lowers as the body
	// keeping both.
	unionPassedNull
	// unionPairApart marks a oneOf and an anyOf sharing no value with each
	// other (pairSharesNoValue) beside a body that is not a Model: no instance
	// satisfies them, whatever it admits.
	unionPairApart
)

// classifyUnionSiblings picks the lowering for a schema whose oneOf/anyOf sits
// beside structural keywords. JSON Schema conjoins keywords, so such a schema
// is an intersection of a structural body and a union, which the IR
// deliberately has no combinator for
// (docs/ir/boundaries.md#deliberate-exclusions). Every outcome keeps both
// sides: classified where the IR can express the conjunction, verbatim where it
// cannot.
//
// What the body does with a union it may distribute is classifyConjoined's.
// docs/compilers/openapi.md#union-beside-structure enumerates the shapes.
func classifyUnionSiblings(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) unionLowering {
	if !unionBranchesDeclareShape(loweringContext, schema) {
		return unionValidationOnly
	}
	if unionAside(loweringContext, schema) {
		return unionSharesNoKind
	}
	if !composesAsModel(loweringContext, schema) {
		if pairSharesNoValue(loweringContext, schema) {
			return unionPairApart
		}
		return unionUncomposableBody
	}
	// Of a oneOf and an anyOf written beside the body, the oneOf is elected
	// (unionBranches), as on one entry of its allOf or two, and the other is
	// kept on each variant (buildComposedVariant).
	if !branchesNameReferents(loweringContext, schema, schema) {
		return unionUnresolvedBranch
	}
	return classifyConjoined(loweringContext, schema, besideUnion(schema, pointer), pointer)
}

// classifyConjoined is how schema, written at pointer, lowers the union unionAtValue it
// conjoins with a composing body, beside it or on a branch of its allOf, once
// unionAtValue's own shape allows distribution: kept whole where the discriminator standing
// with unionAtValue routes to branches composing schema (composedByBranch), composed as a mixin
// where one on an ancestor routes to schema (routedSubtype, lowerSubtypeUnion), kept
// whole where no branch lives beside schema (deadBranches); else distributed, the
// discriminator routing the composed variants (indexBranches).
func classifyConjoined(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt, pointer jsontext.Pointer) unionLowering {
	if passedApart(loweringContext, schema, unionAtValue) {
		return unionPassedApart
	}
	if passedSharesNull(loweringContext, schema, unionAtValue) {
		return unionPassedNull
	}
	if composedByBranch(loweringContext, schema, unionAtValue) {
		return unionHierarchyBase
	}
	if routedSubtype(loweringContext, schema, pointer) {
		return unionRoutedSubtype
	}
	if !hasLiveBranch(loweringContext, unionAtValue, deadBranches(loweringContext, schema, unionAtValue.holder)) {
		return unionNoLiveBranch
	}
	return unionDistributed
}

// passedApart reports whether a union the position written by schema passes over
// for unionAtValue leaves no value unionAtValue admits beside the rest of schema (readPassed): one
// shares no kind with that rest, one of null alone among them where the rest
// admits no null, or together they leave the rest null alone and no branch of
// unionAtValue admits null. It is a conjunct of the rest unionAtValue's branches are read against,
// so no branch of unionAtValue reads as dead though none can be inhabited. A read cut
// short says no.
func passedApart(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt) bool {
	passedRead, ok := readPassed(loweringContext, schema, unionAtValue)
	if !ok {
		return false
	}
	if slices.ContainsFunc(passedRead.passed, func(admittedValue admitted) bool {
		return admittedValue.kinds != 0 && passedRead.rest.kinds&admittedValue.kinds == 0
	}) {
		return true
	}
	// Left null alone, the rest admits no object for deadBranch to read unionAtValue's
	// branches against (restAdmitsObjects).
	return passedRead.nullLeft() && passedRead.union.kinds&kindNull == 0
}

// passedSharesNull reports whether the unions the position written by schema
// passes over for unionAtValue leave the rest of schema null alone (readPassed), which unionAtValue
// admits: the position admits that null and nothing else, as on the entries
// of its allOf (docs/compilers/openapi.md#union-beside-structure).
func passedSharesNull(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt) bool {
	passedRead, ok := readPassed(loweringContext, schema, unionAtValue)
	return ok && passedRead.nullLeft() && passedRead.union.kinds&kindNull != 0
}

// passedRead is what the position written by s admits beside u: the rest of
// s read without any union, each union the position passes over for u, and
// what u's branches admit.
type passedRead struct {
	rest, union admitted
	passed      []admitted
}

// nullLeft reports whether the rest, with every union passed over, admits
// null alone.
func (r passedRead) nullLeft() bool {
	left := r.rest
	for _, admitted := range r.passed {
		left = left.and(admitted)
	}
	return len(r.passed) > 0 && left.reachable() && left.kinds == kindNull
}

// readPassed reads passedRead for unionAtValue: the other combinator unionAtValue's holder writes
// and the union another inline entry of schema's allOf writes are passed over, each
// other entry a conjunct of the rest. ok is false where a read is cut short
// or the rest admits nothing.
func readPassed(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt) (passedRead, bool) {
	admissionReader := newAdmissionReader(loweringContext)
	var passedReadValue passedRead
	if set, _ := passedOver(loweringContext, schema, unionAtValue.holder); set != nil {
		passedReadValue.passed = append(passedReadValue.passed, admissionReader.union(set))
	}
	passedReadValue.rest = admissionReader.written(schema, admissionReader.nullable)
	for _, allOfBranch := range allOfBranches(loweringContext, schema, "").branches {
		bs := allOfBranch.schema.Schema
		switch {
		case !stated(bs):
		case allOfBranch.schema.Ref != nil:
			passedReadValue.rest = passedReadValue.rest.and(admissionReader.of(allOfBranch.schema))
		case bs == unionAtValue.holder || unionAtValue.branch && declaresUnion(bs):
			passedReadValue.rest = passedReadValue.rest.and(admissionReader.written(bs, admissionReader.nullable))
			if bs != unionAtValue.holder {
				passedReadValue.passed = append(passedReadValue.passed, unionsIn(&admissionReader, bs))
			}
		default:
			passedReadValue.rest = passedReadValue.rest.and(inlineConjunct(&admissionReader, bs))
		}
	}
	branches, _, _ := unionAtValue.branches(loweringContext)
	passedReadValue.union = admissionReader.union(branches)
	return passedReadValue, !admissionReader.incomplete() && passedReadValue.rest.reachable()
}

// pairSharesNoValue reports whether schema writes a oneOf and an anyOf that share
// no value, as every spelling of the two reads it (pairApart).
func pairSharesNoValue(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return writesBothUnions(schema) && pairApart(loweringContext, schema) != nil
}

// routedSubtype reports whether a discriminator on an ancestor of schema, written at
// pointer, routes to schema as its subtype: by a mapping key or by its name as a
// component, the tag its lowering takes (subtypeDiscriminatorValue). That
// lowering reports the tag's own diagnostics, so this reading drops them.
func routedSubtype(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) bool {
	tag, _ := subtypeDiscriminatorValue(loweringContext, schema, loweringContext.IDs().ForPointer(pointer), pointer, nil)
	return tag.routed
}

// composesAsModel reports whether the structural siblings lower to a Model,
// which is the only node with composition fields for a union to distribute
// across. It mirrors lower's dispatch: const and enum win over everything, then
// an allOf that composes, then a declared object type, then object keywords
// with no type where the kinds the position lowers to are an object alone
// (untypedKinds): what the union's branches declare, else what the
// kind-inference policy reads off the keywords. So a bare property set,
// `required`, closure or catch-all beside branches each declaring an object
// composes, as beside `type: object`.
func composesAsModel(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if schema.Const() != nil || enumWritten(schema) {
		return false
	}
	if allOfComposes(loweringContext, schema) {
		return true
	}
	types := effectiveTypes(schema)
	if len(types) > 0 {
		return len(types) == 1 && types[0] == statement.KindObject
	}
	kinds, _ := untypedKinds(loweringContext, schema)
	return kinds == kindObject
}

// branchesNameReferents reports whether every union branch can be conjoined with
// the sibling model. Base and Mixins conjoin *by reference*, so a $ref branch
// qualifies exactly when it names a schema this compilation resolves, and an
// inline one does as the node hoisted at its own pointer. The test is over the
// whole union because a half-distributed one would leave variants disagreeing
// about whether they carry the body, with nothing in the IR saying which. A
// branch writing its $ref beside other keywords is read by that $ref.
func branchesNameReferents(loweringContext lowering.Ctx, position, schema *statement.Schema) bool {
	branches, _, _ := unionBranches(loweringContext, position, schema)
	return !slices.ContainsFunc(branches, func(b statement.Subschema) bool {
		ref := referenceOf(loweringContext, loweringContext.Statements().Subschema(b))
		return ref != nil && !namesReferent(loweringContext, ref)
	})
}

// coDeclaredUnionWhyPrefix opens every lowerCoDeclaredUnion why string: at
// each of its reasons, a structural body genuinely sits beside the union, so
// the sentence that follows is always true of it.
const coDeclaredUnionWhyPrefix = "oneOf/anyOf co-declared with structural keywords intersects with them, and "

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings, fin finishing the node built, where st
// says. A union distributed over its body composes an object, which no
// refinement's principal is, so only the union kept beside the body stands
// elsewhere.
func lowerCoDeclaredUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(loweringContext, ts, depth, schema, pointer, reason, why, fin, st)
	}
	switch classifyUnionSiblings(loweringContext, schema, pointer) {
	case unionDistributed:
		return lowerDistributedUnion(loweringContext, ts, depth, schema, besideUnion(schema, pointer), pointer, fin)
	case unionValidationOnly:
		id, diags := beside(ir.ReasonValidationOnly, "")
		switch {
		case writesConjuncts(schema):
			// The conjuncts' report names them (diagDisjointConjuncts).
			return id, diags
		case pairSharesNoValue(loweringContext, schema):
			return id, append(diags, diagUnionsShareNothing(loweringContext, schema, pointer, "its body alone")...)
		case writesBothUnions(schema) || !composesAsModel(loweringContext, schema):
			// Two sharing only a null the body keeps out share no value with it,
			// and a union sharing no kind with a body that is no Model none.
			return id, append(diags, diagUnionApartFromBody(loweringContext, schema, pointer)...)
		default:
			return id, diags
		}
	case unionSharesNoKind:
		return beside(ir.ReasonDegradedLowering,
			"none of its branches shares a kind with the allOf beside it, so no instance satisfies them together")
	case unionUncomposableBody:
		id, diags := beside(ir.ReasonDegradedLowering, "the body is not a model, so it carries no composition to distribute into")
		if st.of != nil {
			// A principal's conjunction is reported at the schema writing it (refinedDisjoint).
			return id, diags
		}
		return id, append(diags, diagUnionApartFromBody(loweringContext, schema, pointer)...)
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(loweringContext, schema, pointer), diags...)
	case unionRoutedSubtype:
		return lowerSubtypeUnion(loweringContext, ts, depth, schema, besideUnion(schema, pointer), pointer, fin)
	case unionNoLiveBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"none of its branches admits an object its body admits, so no variant conjoining the two would hold a value")
		dead := diagDeadBranches(loweringContext, schema, besideUnion(schema, pointer), deadBranches(loweringContext, schema, schema), pointer, false)
		if len(dead) == 0 && pairSharesNoValue(loweringContext, schema) {
			// A union of null alone has no branch to mark dead, and the
			// conjuncts' report names no union: the pair says it.
			dead = diagUnionsShareNothing(loweringContext, schema, pointer, "its body alone")
		}
		return id, append(diags, dead...)
	case unionPairApart:
		// A schema writing conjuncts beside them refines past them (entryUnionsApart).
		id, diags := beside(ir.ReasonDegradedLowering, "its oneOf and anyOf share no value, so no instance satisfies them together")
		return id, append(diags, diagUnionsShareNothing(loweringContext, schema, pointer, "its body alone")...)
	case unionPassedNull:
		_, passed := passedOver(loweringContext, schema, schema)
		return beside(ir.ReasonDegradedLowering, "the "+passed+" beside it leaves the body null alone, which this "+
			"union admits, so the position admits that null and no variant would hold a value")
	case unionPassedApart:
		_, passed := passedOver(loweringContext, schema, schema)
		id, diags := beside(ir.ReasonDegradedLowering, "the "+passed+" beside it leaves the body no value this union "+
			"admits, so no instance satisfies them together")
		return id, append(diags, diagUnionsApart(loweringContext, schema, pointer)...)
	default: // unionHierarchyBase
		return beside(ir.ReasonDegradedLowering,
			"the discriminator beside it routes to branches composing this schema, its subtypes, which distributing them into a union would undo")
	}
}

// diagUnionsApart is the warning schema, written at pointer, draws where a union it
// passes over shares no kind with its body and no allOf it writes reports its
// conjuncts (docs/compilers/openapi.md#allof-disjoint-conjuncts): the body's
// keywords and that union, as that report names them.
func diagUnionsApart(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if writesConjuncts(schema) {
		return nil
	}
	comp := allOfComposition{pointer: pointer}
	comp.disjoint = disjointAtoms(loweringContext, schema, conjunctAtoms(loweringContext, schema, comp))
	return diagDisjointConjuncts(loweringContext, comp, "its body alone")
}

// diagUnionApartFromBody is the warning schema, written at pointer beside a body
// that is no Model, draws where the unions it writes share no kind with that
// body (unionApartFromBodyNames) and no allOf it writes reports its conjuncts.
func diagUnionApartFromBody(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if writesConjuncts(schema) {
		return nil
	}
	comp := allOfComposition{pointer: pointer}
	comp.disjoint = unionApartFromBodyNames(loweringContext, schema)
	return diagDisjointConjuncts(loweringContext, comp, "its body alone")
}

// unionApartFromBodyNames names the body schema writes and each union beside it,
// as conjunctAtoms names them, where they share no kind, and is nil where
// they share one.
func unionApartFromBodyNames(loweringContext lowering.Ctx, schema *statement.Schema) []string {
	return disjointAtoms(loweringContext, schema, unionAndBodyAtoms(loweringContext, schema))
}

// unionApartFromBody reports whether the atoms unionApartFromBodyNames reads
// share no kind, a read cut short admitting every kind and a null every one
// may admit being shared, without naming them: naming reads each target that
// admits nothing, which may read schema again.
func unionApartFromBody(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	joint := anyInstance
	for _, a := range unionAndBodyAtoms(loweringContext, schema) {
		joint &= a.admits
	}
	return joint == 0 && !nullShared(loweringContext, schema)
}

// unionAndBodyAtoms are the atoms of the body schema writes and of each union it
// writes, the union atoms conjunctAtoms adds replaced by both unions'.
func unionAndBodyAtoms(loweringContext lowering.Ctx, schema *statement.Schema) []conjunctAtom {
	choice := schema.ChoiceKeywords()
	atoms := slices.DeleteFunc(conjunctAtoms(loweringContext, schema, allOfComposition{}), func(a conjunctAtom) bool { return a.union != nil })
	return slices.Concat(atoms, unionAtoms(loweringContext, "oneOf", choice.OneOf), unionAtoms(loweringContext, "anyOf", choice.AnyOf))
}

// keepsUnionApartFromBody reports whether schema, read at the position its
// bindings place it at, writes a union sharing no kind with a body beside it
// that is no Model, which schema reports where it is written
// (diagUnionApartFromBody), as keepsDeadUnion reads a dead union.
func keepsUnionApartFromBody(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	at, placed := siteOf(schema)
	if !placed || !declaresUnion(schema) || writesConjuncts(schema) || !hasUnionSiblings(loweringContext, schema) {
		return false
	}
	in, _ := lowering.InDocument(loweringContext, at.Doc)
	return classifyUnionSiblings(in, schema, at.Pointer) == unionUncomposableBody && unionApartFromBody(in, schema)
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, but none for a $ref on a loop of
// $refs, which the load phase reports once (onLoop). The
// branch text survives verbatim under Unmodeled, so nothing is dropped, but a
// reference that resolves nowhere is a defect of the document itself, reported
// at the same severity as everywhere else, with the reason refTypeRef's report
// gives (unresolvedWhy).
func diagUnresolvedBranches(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	branches, key, _ := unionBranches(loweringContext, schema, schema)
	var diags []ir.Diagnostic
	for i, b := range branches {
		st := referenceOf(loweringContext, loweringContext.Statements().Subschema(b))
		if st == nil || namesReferent(loweringContext, st) || onLoop(st) {
			continue
		}
		keyword, ref := refSpelling(st)
		diags = append(diags, loweringContext.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr(key, strconv.Itoa(i)),
			"union branch %s %q is unresolved%s; the branch is kept verbatim", keyword, ref, unresolvedWhy(loweringContext, st)))
	}
	return diags
}

// lowerBesideUnmodeledUnion lowers the structural body of a schema that
// co-declares oneOf/anyOf and keeps the union verbatim beside it, so neither the
// structural shape nor the union is dropped. reason says which kind of union it
// is and why says what stopped a classified lowering; lowerCoDeclaredUnion picks
// both from classifyUnionSiblings' verdict. A discriminator the body's model
// holds is settled once every node is interned (discriminatorBesideUnion). The
// node stands where st says.
func lowerBesideUnmodeledUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer,
	reason ir.UnmodeledReason, why string, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var siblings finished
	beside := discriminatorBesideUnion(loweringContext, ts, schema, schema, pointer).then(siblings.on(func(td ir.TypeDef) []ir.Diagnostic {
		return preserveUnionSiblingsAt(loweringContext, &td.Common().Unmodeled, schema, pointer, reason, why)
	})).then(fin)
	inner, diags := lower(loweringContext, ts, depth, schema, pointer, beside, st)
	owner := inner
	if !siblings.ran && !LoweredToOwnNode(loweringContext, st.pointer(pointer), ir.TypeRef{Target: inner}) {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(loweringContext, ts, schema, pointer, ir.TypeRef{Target: inner}, beside, st)
		diags = append(diags, aliasDiags...)
	}
	return owner, append(diags, siblings.diags...)
}

// preserveUnionSiblingsAt stores the raw oneOf/anyOf of schema under unmodeled, the
// Unmodeled of the node standing at the position. A validation-only union joins
// the validation-only keywords and is reported with it; anything else is a
// degraded lowering and says so. why is the caller's complete sentence.
func preserveUnionSiblingsAt(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(loweringContext, unmodeled, schema, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
}
