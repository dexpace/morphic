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
func hasUnionSiblings(c lowering.Ctx, s *statement.Schema) bool {
	if !declaresUnion(s) || composedByBranch(c, s, besideUnion(s, "")) {
		return writesSiblingKeyword(s) // no branch says it, or the body is their base
	}
	siblings := siblingKeywords(s)
	if !allOfComposes(c, s) {
		siblings = slices.DeleteFunc(siblings, func(k string) bool { return k == "allOf" })
	}
	if len(siblings) == 0 {
		return false
	}
	said := branchesSay(c, s, s)
	if slices.ContainsFunc(siblings, func(k string) bool { return !slices.Contains(said, k) }) {
		return true
	}
	// A oneOf and an anyOf sharing no value lower as the body beside them
	// where it is no Model (unionPairApart), whatever their branches say.
	return !unionBranchesDeclareShape(c, s) || pairSharesNoValue(c, s) && !composesAsModel(c, s)
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
func branchesSay(c lowering.Ctx, union, beside *statement.Schema) []string {
	return lowering.Remembered(c, branchesSayOf{union, beside}, func() []string {
		choice := union.ChoiceKeywords()
		branches := slices.Concat(choice.OneOf, choice.AnyOf)
		if len(branches) == 0 {
			return nil
		}
		w := newAdmissionReader(c)
		reads := make([]admitted, len(branches))
		for i, b := range branches {
			reads[i] = lowered(&w, c.Statements().Sub(b))
		}
		if w.incomplete() {
			return nil
		}
		var out []string
		for _, key := range restatableKeywords {
			if !hasKey(beside, key) {
				continue
			}
			if !slices.ContainsFunc(reads, func(r admitted) bool { return !branchSays(&w, r, beside, key) }) {
				out = append(out, key)
			}
		}
		return out
	})
}

// branchSays reports whether a branch admitting branch, as w reads it, says all
// that s's keyword key does. A `type` is compared with null in it, which
// admissionReader.holds leaves to the nulls reader: a union lowered alone lifts a null
// branch onto the position, so a `type` leaving null out says more than a branch
// admitting it. An `items` list says what no single schema does.
func branchSays(w *admissionReader, branch admitted, s *statement.Schema, key string) bool {
	switch key {
	case "type":
		return branch.kinds&^writtenKinds(s, w.nullable) == 0
	case "items":
		_, listed := listedItems(s)
		return !listed && w.holds(branch, s, key)
	default:
		return w.holds(branch, s, key)
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
func classifyUnionSiblings(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) unionLowering {
	if !unionBranchesDeclareShape(c, s) {
		return unionValidationOnly
	}
	if unionAside(c, s) {
		return unionSharesNoKind
	}
	if !composesAsModel(c, s) {
		if pairSharesNoValue(c, s) {
			return unionPairApart
		}
		return unionUncomposableBody
	}
	// Of a oneOf and an anyOf written beside the body, the oneOf is elected
	// (unionBranches), as on one entry of its allOf or two, and the other is
	// kept on each variant (buildComposedVariant).
	if !branchesNameReferents(c, s, s) {
		return unionUnresolvedBranch
	}
	return classifyConjoined(c, s, besideUnion(s, pointer), pointer)
}

// classifyConjoined is how s, written at pointer, lowers the union u it
// conjoins with a composing body, beside it or on a branch of its allOf, once
// u's own shape allows distribution: kept whole where the discriminator standing
// with u routes to branches composing s (composedByBranch), composed as a mixin
// where one on an ancestor routes to s (routedSubtype, lowerSubtypeUnion), kept
// whole where no branch lives beside s (deadBranches); else distributed, the
// discriminator routing the composed variants (indexBranches).
func classifyConjoined(c lowering.Ctx, s *statement.Schema, u unionAt, pointer jsontext.Pointer) unionLowering {
	if passedApart(c, s, u) {
		return unionPassedApart
	}
	if composedByBranch(c, s, u) {
		return unionHierarchyBase
	}
	if routedSubtype(c, s, pointer) {
		return unionRoutedSubtype
	}
	if !hasLiveBranch(c, u, deadBranches(c, s, u.holder)) {
		return unionNoLiveBranch
	}
	return unionDistributed
}

// passedApart reports whether a union the position written by s passes over
// for u shares no kind with the rest of s read without any union: the other
// combinator u's holder writes, or the union another inline entry of s's allOf
// writes, one of null alone among them where the rest admits no null. It is a
// conjunct of the rest u's branches are read against, which it leaves
// admitting nothing, so no branch of u reads as dead though none can be
// inhabited. A read cut short says no.
func passedApart(c lowering.Ctx, s *statement.Schema, u unionAt) bool {
	w := newAdmissionReader(c)
	var passed []admitted
	if set, _ := passedOver(c, s, u.holder); set != nil {
		passed = append(passed, w.union(set))
	}
	rest := w.written(s, w.nullable)
	for _, e := range allOfBranches(c, s, "").branches {
		bs := e.schema.Schema
		switch {
		case !stated(bs):
		case e.schema.Ref != nil:
			rest = rest.and(w.of(e.schema))
		case bs == u.holder || u.branch && declaresUnion(bs):
			rest = rest.and(w.written(bs, w.nullable))
			if bs != u.holder {
				passed = append(passed, unionsIn(&w, bs))
			}
		default:
			rest = rest.and(inlineConjunct(&w, bs))
		}
	}
	if w.incomplete() || !rest.reachable() {
		return false
	}
	return slices.ContainsFunc(passed, func(p admitted) bool { return p.kinds != 0 && rest.kinds&p.kinds == 0 })
}

// pairSharesNoValue reports whether s writes a oneOf and an anyOf that share
// no value, as every spelling of the two reads it (pairApart).
func pairSharesNoValue(c lowering.Ctx, s *statement.Schema) bool {
	return writesBothUnions(s) && pairApart(c, s) != nil
}

// routedSubtype reports whether a discriminator on an ancestor of s, written at
// pointer, routes to s as its subtype: by a mapping key or by its name as a
// component, the tag its lowering takes (subtypeDiscriminatorValue). That
// lowering reports the tag's own diagnostics, so this reading drops them.
func routedSubtype(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) bool {
	tag, _ := subtypeDiscriminatorValue(c, s, c.IDs().ForPointer(pointer), pointer, nil)
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
func composesAsModel(c lowering.Ctx, s *statement.Schema) bool {
	if s.Const() != nil || enumWritten(s) {
		return false
	}
	if allOfComposes(c, s) {
		return true
	}
	types := effectiveTypes(s)
	if len(types) > 0 {
		return len(types) == 1 && types[0] == statement.KindObject
	}
	kinds, _ := untypedKinds(c, s)
	return kinds == kindObject
}

// branchesNameReferents reports whether every union branch can be conjoined with
// the sibling model. Base and Mixins conjoin *by reference*, so a $ref branch
// qualifies exactly when it names a schema this compilation resolves, and an
// inline one does as the node hoisted at its own pointer. The test is over the
// whole union because a half-distributed one would leave variants disagreeing
// about whether they carry the body, with nothing in the IR saying which. A
// branch writing its $ref beside other keywords is read by that $ref.
func branchesNameReferents(c lowering.Ctx, position, s *statement.Schema) bool {
	branches, _, _ := unionBranches(c, position, s)
	return !slices.ContainsFunc(branches, func(b statement.Sub) bool {
		ref := referenceOf(c, c.Statements().Sub(b))
		return ref != nil && !namesReferent(c, ref)
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
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(c, ts, depth, s, pointer, reason, why, fin, st)
	}
	switch classifyUnionSiblings(c, s, pointer) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, depth, s, besideUnion(s, pointer), pointer, fin)
	case unionValidationOnly:
		id, diags := beside(ir.ReasonValidationOnly, "")
		switch {
		case writesConjuncts(s):
			// The conjuncts' report names them (diagDisjointConjuncts).
			return id, diags
		case pairSharesNoValue(c, s):
			return id, append(diags, diagUnionsShareNothing(c, s, pointer, "its body alone")...)
		case writesBothUnions(s):
			// Two sharing only a null the body keeps out share no value with it.
			return id, append(diags, diagUnionApartFromBody(c, s, pointer)...)
		default:
			return id, diags
		}
	case unionSharesNoKind:
		return beside(ir.ReasonDegradedLowering,
			"none of its branches shares a kind with the allOf beside it, so no instance satisfies them together")
	case unionUncomposableBody:
		id, diags := beside(ir.ReasonDegradedLowering, "the body is not a model, so it carries no composition to distribute into")
		return id, append(diags, diagUnionApartFromBody(c, s, pointer)...)
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(c, s, pointer), diags...)
	case unionRoutedSubtype:
		return lowerSubtypeUnion(c, ts, depth, s, besideUnion(s, pointer), pointer, fin)
	case unionNoLiveBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"none of its branches admits an object its body admits, so no variant conjoining the two would hold a value")
		return id, append(diags, diagDeadBranches(c, s, besideUnion(s, pointer), deadBranches(c, s, s), pointer, false)...)
	case unionPairApart:
		// A schema writing conjuncts beside them refines past them (entryUnionsApart).
		id, diags := beside(ir.ReasonDegradedLowering, "its oneOf and anyOf share no value, so no instance satisfies them together")
		return id, append(diags, diagUnionsShareNothing(c, s, pointer, "its body alone")...)
	case unionPassedApart:
		_, passed := passedOver(c, s, s)
		id, diags := beside(ir.ReasonDegradedLowering, "the "+passed+" beside it shares no kind with the body, so no "+
			"instance satisfies them together")
		return id, append(diags, diagUnionsApart(c, s, pointer)...)
	default: // unionHierarchyBase
		return beside(ir.ReasonDegradedLowering,
			"the discriminator beside it routes to branches composing this schema, its subtypes, which distributing them into a union would undo")
	}
}

// diagUnionsApart is the warning s, written at pointer, draws where a union it
// passes over shares no kind with its body and no allOf it writes reports its
// conjuncts (docs/compilers/openapi.md#allof-disjoint-conjuncts): the body's
// keywords and that union, as that report names them.
func diagUnionsApart(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if writesConjuncts(s) {
		return nil
	}
	comp := allOfComposition{pointer: pointer}
	comp.disjoint = disjointAtoms(c, s, conjunctAtoms(c, s, comp))
	return diagDisjointConjuncts(c, comp, "its body alone")
}

// diagUnionApartFromBody is the warning s, written at pointer beside a body
// that is no Model, draws where the unions it writes share no kind with that
// body (unionApartFromBodyNames) and no allOf it writes reports its conjuncts.
func diagUnionApartFromBody(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if writesConjuncts(s) {
		return nil
	}
	comp := allOfComposition{pointer: pointer}
	comp.disjoint = unionApartFromBodyNames(c, s)
	return diagDisjointConjuncts(c, comp, "its body alone")
}

// unionApartFromBodyNames names the body s writes and each union beside it,
// as conjunctAtoms names them, where they share no kind, and is nil where
// they share one.
func unionApartFromBodyNames(c lowering.Ctx, s *statement.Schema) []string {
	return disjointAtoms(c, s, unionAndBodyAtoms(c, s))
}

// unionApartFromBody reports whether the atoms unionApartFromBodyNames reads
// share no kind, a read cut short admitting every kind and a null every one
// may admit being shared, without naming them: naming reads each target that
// admits nothing, which may read s again.
func unionApartFromBody(c lowering.Ctx, s *statement.Schema) bool {
	joint := anyInstance
	for _, a := range unionAndBodyAtoms(c, s) {
		joint &= a.admits
	}
	return joint == 0 && !nullShared(c, s)
}

// unionAndBodyAtoms are the atoms of the body s writes and of each union it
// writes, the union atoms conjunctAtoms adds replaced by both unions'.
func unionAndBodyAtoms(c lowering.Ctx, s *statement.Schema) []conjunctAtom {
	choice := s.ChoiceKeywords()
	atoms := slices.DeleteFunc(conjunctAtoms(c, s, allOfComposition{}), func(a conjunctAtom) bool { return a.union != nil })
	return slices.Concat(atoms, unionAtoms(c, "oneOf", choice.OneOf), unionAtoms(c, "anyOf", choice.AnyOf))
}

// keepsUnionApartFromBody reports whether s, read at the position its
// bindings place it at, writes a union sharing no kind with a body beside it
// that is no Model, which s reports where it is written
// (diagUnionApartFromBody), as keepsDeadUnion reads a dead union.
func keepsUnionApartFromBody(c lowering.Ctx, s *statement.Schema) bool {
	at, placed := siteOf(s)
	if !placed || !declaresUnion(s) || writesConjuncts(s) || !hasUnionSiblings(c, s) {
		return false
	}
	in, _ := lowering.InDocument(c, at.Doc)
	return classifyUnionSiblings(in, s, at.Pointer) == unionUncomposableBody && unionApartFromBody(in, s)
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, but none for a $ref on a loop of
// $refs, which the load phase reports once (onLoop). The
// branch text survives verbatim under Unmodeled, so nothing is dropped, but a
// reference that resolves nowhere is a defect of the document itself, reported
// at the same severity as everywhere else, with the reason refTypeRef's report
// gives (unresolvedWhy).
func diagUnresolvedBranches(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	branches, key, _ := unionBranches(c, s, s)
	var diags []ir.Diagnostic
	for i, b := range branches {
		st := referenceOf(c, c.Statements().Sub(b))
		if st == nil || namesReferent(c, st) || onLoop(st) {
			continue
		}
		keyword, ref := refSpelling(st)
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr(key, strconv.Itoa(i)),
			"union branch %s %q is unresolved%s; the branch is kept verbatim", keyword, ref, unresolvedWhy(c, st)))
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
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer,
	reason ir.UnmodeledReason, why string, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var siblings finished
	beside := discriminatorBesideUnion(c, ts, s, s, pointer).then(siblings.on(func(td ir.TypeDef) []ir.Diagnostic {
		return preserveUnionSiblingsAt(c, &td.Common().Unmodeled, s, pointer, reason, why)
	})).then(fin)
	inner, diags := lower(c, ts, depth, s, pointer, beside, st)
	owner := inner
	if !siblings.ran && !LoweredToOwnNode(c, st.pointer(pointer), ir.TypeRef{Target: inner}) {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, ir.TypeRef{Target: inner}, beside, st)
		diags = append(diags, aliasDiags...)
	}
	return owner, append(diags, siblings.diags...)
}

// preserveUnionSiblingsAt stores the raw oneOf/anyOf of s under p, the
// Unmodeled of the node standing at the position. A validation-only union joins
// the validation-only keywords and is reported with it; anything else is a
// degraded lowering and says so. why is the caller's complete sentence.
func preserveUnionSiblingsAt(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, p, s, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
}
