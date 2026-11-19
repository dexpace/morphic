// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// maxRefinedConjuncts bounds the conjuncts one election compares, every pair of
// them: an allOf past it stays a composition, which loses nothing.
const maxRefinedConjuncts = 64

// refinement is an allOf whose kinds are not an object alone: one use of the
// conjunct that decides it, its principal, rather than a composition
// (docs/compilers/openapi.md#allof-refinement).
type refinement struct {
	comp allOfComposition
	// principal is the branch the position is a use of, nil where the keywords
	// written beside the allOf decide it or nothing does.
	principal *allOfBranch
	// ref is the $ref branch a use reads on through: the principal where it is
	// one, else the one $ref written, nil where none or several are.
	ref *allOfBranch
	// votes is how each branch of comp stood in the election, by index: a
	// $ref the position's kinds outvoted, or one another conjunct beat.
	votes []refCandidacy
	// joint is what the conjuncts admit together.
	joint admitted
	// held is what the position lowers to admits, which decides what a keyword
	// left unread loses.
	held admitted
	// contradicts marks conjuncts sharing no value: a value set was elected
	// before the composition they would otherwise lower to, or, for value sets
	// sharing no member, no conjunct was (decide).
	contradicts bool
}

// principalIs reports whether e is the branch the position lowers from: its
// schema at its pointer, as an entry writing a $ref beside other keys is two
// branches at one pointer (statement.AllOfConjunct).
func (r refinement) principalIs(e allOfBranch) bool {
	return r.principal != nil && r.principal.pointer == e.pointer && r.principal.schema == e.schema
}

// refCandidacy is how a $ref conjunct stood in the election of a principal.
type refCandidacy uint8

const (
	// refCandidateForDecidingConjunct marks a $ref that could be the principal, and is, or lost
	// to a value set.
	refCandidateForDecidingConjunct refCandidacy = iota
	// refNarrowed marks one another $ref beat, lying within more conjuncts.
	refNarrowed
	// refInferredApart marks one whose target the kind-inference policy reads
	// as a kind the position's declarations exclude.
	refInferredApart
	// refAdmitsEvery marks one whose target admits every kind beside a
	// position declaring its kinds.
	refAdmitsEvery
	// refOutsideSet marks one whose target admits none of the value set
	// elected before the composition the conjuncts would lower to.
	refOutsideSet
	// refApart marks one of conjuncts whose value sets share no member, of
	// which none is the principal (decide).
	refApart
)

// conjunct is one conjunct's reading, for the election.
type conjunct struct {
	// branch is nil for the keywords written beside the allOf, and at is its
	// index among the composition's branches.
	branch *allOfBranch
	at     int
	read   admitted
	// schema writes the conjunct's keywords: the branch's, or the position's
	// for the keywords beside the allOf.
	schema *statement.Schema
	// says marks an inline conjunct writing what kind of value this is, and
	// form how it names it.
	says bool
	form conjunctForm
}

// conjunctForm is how an inline conjunct names its kinds, ranked for a tie
// between two lying within as many conjuncts: a union before the words, which
// then bound it, and a oneOf before an anyOf, as the one Union a schema
// writing both lowers to is its oneOf's
// (docs/compilers/openapi.md#competing-keywords).
type conjunctForm uint8

const (
	// formWords marks a conjunct naming its kinds by `type`, `enum` or `const`.
	formWords conjunctForm = iota
	// formAnyOf marks one writing an anyOf and no oneOf.
	formAnyOf
	// formOneOf marks one writing a oneOf.
	formOneOf
)

// unionForm is the form of a conjunct whose keywords s writes, its unions
// among them.
func unionForm(s *statement.Schema) conjunctForm {
	switch choice := s.ChoiceKeywords(); {
	case len(choice.OneOf) > 0:
		return formOneOf
	case len(choice.AnyOf) > 0:
		return formAnyOf
	default:
		return formWords
	}
}

// decisionAt keys a schema's decision: nothing decide reads turns on where the
// schema is written, which only prefixes its branches' pointers.
type decisionAt struct {
	schema *statement.Schema
}

// decision is what a position's readers share: a refinement, where it is one,
// its composition, principal and ref left for refinementOf to read at the
// pointer asked; the indices of that principal and ref among the branches, -1
// for none and the principal for the keywords beside the allOf; whether a
// union written beside the allOf shares no kind with the rest, which the
// position leaves out and keeps (unionAside); apart, what each conjunct
// states where the election found they share no value (apartNames).
type decision struct {
	r           refinement
	apart       []string
	principalAt int
	refAt       int
	refines     bool
	aside       bool
}

// refinementOf decides whether s, written at pointer, refines rather than
// composes (docs/compilers/openapi.md#allof-refinement). It reads the source and
// the bindings, never the registry, so a decision turns on no lowering order, and
// the many readers of a schema share one reading wherever it is written.
func refinementOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (refinement, bool) {
	d := decisionOf(c, s)
	if !d.refines {
		return refinement{}, false
	}
	r := d.r
	r.comp = allOfBranches(c, s, pointer)
	r.principal, r.ref = branchAt(r.comp, d.principalAt), branchAt(r.comp, d.refAt)
	return r, true
}

// branchAt returns the branch of comp at index i, nil for -1.
func branchAt(comp allOfComposition, i int) *allOfBranch {
	if i < 0 {
		return nil
	}
	return &comp.branches[i]
}

// branchStatement returns the statement of the branch of s's allOf at index i, as
// allOfBranches reads it, nil for -1: a reader of the decision with no pointer
// builds no branch list.
func branchStatement(c lowering.Ctx, s *statement.Schema, i int) *statement.Statement {
	if i < 0 {
		return nil
	}
	return c.Statements().Read(c.Statements().Read(s).Entries[i].Schema)
}

// decisionOf returns the decision on s, read once a run, and none where its
// allOf may not refine.
func decisionOf(c lowering.Ctx, s *statement.Schema) decision {
	if !allOfMayRefine(s) {
		return decision{}
	}
	return lowering.ReadOncePerRun(c, decisionAt{s}, func() decision { return decide(c, s) })
}

// unionAside reports whether the oneOf or anyOf s writes beside its allOf
// shares no kind with what the rest of s admits: no instance satisfies them
// together, so the position lowers as the rest does and keeps the union
// (docs/compilers/openapi.md#union-beside-structure).
func unionAside(c lowering.Ctx, s *statement.Schema) bool {
	return decisionOf(c, s).aside
}

// usesOneRef reports whether s, whose flattened conjuncts are comp, is a use of
// one target saying nothing of it: one conjunct is a $ref writing nothing
// read beside it, as an entry read whole may (statement.ReadBeside), and s
// and every other conjunct state nothing (statesNothing), or, where a carrier
// holds s's position (carriedAt), nothing but what binds the use
// (bindsUseOnly), which the carrier holds. It is the reference, as the $ref
// alone is (docs/compilers/openapi.md#allof-classification).
func usesOneRef(c lowering.Ctx, s *statement.Schema, comp allOfComposition) bool {
	carried := carriedAt(c, s)
	quiet := func(x *statement.Schema) bool { return statesNothing(x) || carried && bindsUseOnly(c, x) }
	refs := 0
	for _, e := range comp.branches {
		switch {
		case e.schema.Ref != nil && slices.ContainsFunc(e.schema.Keys, func(k statement.Key) bool {
			return statement.ReadBeside(k.Name, k.Node, e.schema.Ref.Keyword)
		}):
			return false
		case e.schema.Ref != nil:
			refs++
		case !quiet(e.schema.Schema):
			return false
		}
	}
	return refs == 1 && quiet(s)
}

// bindsUseOnly reports whether every key s writes that says something of the
// instance binds a use of the type rather than the type: one of
// residueKeywords, or a `nullable: true` the nullable-inference policy reads
// as widening the reference (docs/compilers/openapi.md#nullable), each of
// which a carrier holds and a node cannot
// (docs/compilers/openapi.md#use-bound-annotations).
func bindsUseOnly(c lowering.Ctx, s *statement.Schema) bool {
	return s.Truth == statement.Stated && !slices.ContainsFunc(s.Keys, func(k statement.Key) bool {
		switch {
		case k.Name == "allOf" && len(s.AllOf) > 0, !statement.ReadBeside(k.Name, k.Node, ""),
			slices.Contains(residueKeywords, k.Name):
			return false
		case k.Name == "nullable":
			return !c.NullableInference.Honours() || !nullableTrue(s)
		default:
			return true
		}
	})
}

// carriedAt reports whether a carrier holds the position the bindings place s
// at: a property a schema lists, or the schema of a parameter or a header,
// its content's included. It reads the placement, never the lowering that
// reaches s, so every reader of s's decision, the carrier's and a $ref's
// naming the position alike, reads one answer.
func carriedAt(c lowering.Ctx, s *statement.Schema) bool {
	at, placed := siteOf(s)
	if !placed {
		return false
	}
	p := at.Pointer
	if p.Parent().LastToken() == "properties" {
		owner := c.Statements().At(reftable.DocumentPointer{Doc: at.Doc, Pointer: p.Parent().Parent()})
		return owner.Truth == statement.Stated && slices.ContainsFunc(owner.ObjectKeywords().Properties, func(sub statement.Subschema) bool {
			return sub.Name == p.LastToken() && c.Statements().Subschema(sub).Schema == s
		})
	}
	if p.LastToken() != "schema" {
		return false
	}
	holder := reftable.DocumentPointer{Doc: at.Doc, Pointer: p.Parent()}
	if holder.Pointer.Parent().LastToken() == "content" {
		holder.Pointer = holder.Pointer.Parent().Parent()
	}
	return c.Bindings().ObjectAt(holder, reftable.RefParameter) != nil || c.Bindings().ObjectAt(holder, reftable.RefHeader) != nil
}

// statesNothing reports whether s states nothing of the instance: `true`, a
// value written as no schema (docs/compilers/openapi.md#null-schema), or a
// schema object writing nothing but the allOf its statement flattens, a list
// holding an entry, and keys that say nothing of the instance
// (statement.ReadBeside): a comment, an anchor, schemas a reference reaches,
// a keyword left out.
func statesNothing(s *statement.Schema) bool {
	if s.Truth == statement.Anything || s.Truth == statement.Unwritten {
		return true
	}
	return s.Truth == statement.Stated && !slices.ContainsFunc(s.Keys, func(k statement.Key) bool {
		return (k.Name != "allOf" || len(s.AllOf) == 0) && statement.ReadBeside(k.Name, k.Node, "")
	})
}

// decide reads what refinementOf remembers. Two unions standing alone elect
// one (unionPair). Else conjuncts sharing no value, or whose kinds, null
// aside, are an object alone, compose, unless it first elects a conjunct's
// value set (docs/compilers/openapi.md#competing-keywords) or, where they share
// one, a union standing alone (soleUnion); so does a read cut short, or past a
// cap but weighing one $ref alone (pastAlone). Value sets sharing no member
// compose a Model only where those kinds are an object alone; elsewhere the
// keywords beside the allOf decide. Anything else, a use of one target saying
// nothing more (usesOneRef) included, uses its principal.
func decide(c lowering.Ctx, s *statement.Schema) decision {
	comp := allOfBranches(c, s, "")
	if !refinableBranches(comp) {
		return decision{}
	}
	w := newAdmissionReader(c)
	// The decision is remembered by s alone, so s is read in its own
	// document's dialect, never in that of the reader asking first.
	w.nullable = definesNullableAt(c, c.Statements().Read(s))
	conjuncts, aside := w.conjuncts(s, comp)
	joint := conjuncts[0].read
	for _, cj := range conjuncts[1:] {
		joint = joint.and(cj.read)
	}
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	if aside {
		kinds, inferred = restKinds(c, s, comp)
	}
	var principal *conjunct
	if !aside {
		// A pair the reading puts aside lowers as the rest, standing alone or not.
		principal = unionPair(c, s, conjuncts)
	}
	one := usesOneRef(c, s, comp)
	contradicts := !one && disjoint(joint, conjuncts, pairEntries(s, conjuncts))
	apart := contradicts && joint.fixed && joint.kinds&^kindNull != 0 && kinds != kindObject
	var names []string
	if contradicts {
		names = apartNames(s, comp, conjuncts, joint)
	}
	if principal == nil && (contradicts || kinds == kindObject && !one) {
		principal = composingElected(c, &w, s, conjuncts, contradicts)
		if principal == nil && !apart {
			return decision{aside: aside, apart: names}
		}
	}
	votes, electable := voteRefs(c, conjuncts, kinds, inferred, len(comp.branches))
	undecided := principal == nil && apart
	switch {
	case undecided:
		principal = &conjuncts[0]
	case principal == nil:
		principal = pairedWith(c, s, conjuncts, w.chooseConjunctToLowerFrom(conjuncts, electable))
	}
	if body := nullPairBody(c, s, conjuncts, joint); body != nil {
		principal = body
	}
	r := refinement{votes: votes, joint: joint, held: pairHeld(c, s, conjuncts, principal), contradicts: contradicts}
	refAt := readThrough(conjuncts, principal)
	markBeaten(&w, r.votes, electable, principal, contradicts, undecided)
	// A use saying nothing more of a reference naming no schema is that
	// reference, which has nothing for the read to finish.
	failed := one && refAt >= 0 && !namesReferent(c, comp.branches[refAt].schema)
	refines := failed || !w.incomplete() || !w.cut && pastAlone(c, conjuncts, one)
	return decision{r: r, apart: names, principalAt: principal.at, refAt: refAt, refines: refines, aside: aside}
}

// composingElected returns the conjunct elected where the conjuncts of s
// would otherwise compose: a value set one states (setElected), else, where
// they share a value (contradicts says not), a union standing alone
// (soleUnion), and nil where neither is.
func composingElected(c lowering.Ctx, w *admissionReader, s *statement.Schema, conjuncts []conjunct, contradicts bool) *conjunct {
	if set := setElected(w, s, conjuncts); set != nil || contradicts {
		return set
	}
	return soleUnion(c, s, conjuncts)
}

// soleUnion returns the inline conjunct writing the union the position is,
// nothing beside it conjoining with it, as hasUnionSiblings reads one written
// at the position (docs/compilers/openapi.md#union-beside-structure): one
// conjunct writes a union (unionEntries) and nothing else conjoins with it
// (argumentStandsAlone).
func soleUnion(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct) *conjunct {
	unions := unionEntries(s, conjuncts)
	if len(unions) != 1 || !standsAlone(c, s, conjuncts, unions, unions[0]) {
		return nil
	}
	return unions[0]
}

// unionPair returns the conjunct writing the union the position elects where
// its conjuncts write a oneOf and an anyOf, on one inline entry or one each on
// two, and nothing else conjoining with them (argumentStandsAlone): the position is
// then one schema writing both (docs/compilers/openapi.md#competing-keywords),
// whatever the kinds their branches admit.
func unionPair(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct) *conjunct {
	unions := unionEntries(s, conjuncts)
	elected := pairElected(c, s, unions)
	if elected == nil || !standsAlone(c, s, conjuncts, unions, elected) {
		return nil
	}
	return elected
}

// pairElected returns the conjunct of unions whose union the position elects
// where they write one oneOf and one anyOf between them: the one entry
// writing both, which elects within itself (unionBranches), or of two entries
// the one whose branches declare shape against the position, the oneOf's where
// both or neither do, as anyOfElected reads one schema; else nil.
func pairElected(c lowering.Ctx, s *statement.Schema, unions []*conjunct) *conjunct {
	if len(unions) == 1 {
		if choice := unions[0].schema.ChoiceKeywords(); len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
			return unions[0]
		}
		return nil
	}
	if len(unions) != 2 {
		return nil
	}
	one, other := unions[0], unions[1]
	if len(one.schema.ChoiceKeywords().OneOf) == 0 {
		one, other = other, one
	}
	oneOf, anyOf := one.schema.ChoiceKeywords(), other.schema.ChoiceKeywords()
	if len(oneOf.AnyOf) > 0 || len(oneOf.OneOf) == 0 || len(anyOf.OneOf) > 0 || len(anyOf.AnyOf) == 0 {
		return nil
	}
	if !setsDeclareShape(c, s, one.schema, oneOf.OneOf) && setsDeclareShape(c, s, other.schema, anyOf.AnyOf) {
		return other
	}
	return one
}

// pairedWith returns the conjunct the refinement elects where elected writes
// one union of a oneOf and an anyOf written one each on two inline entries
// (pairEntries): of the two, the one competing keywords prefer at s
// (pairElected), as one schema writing both elects, not the one the kinds of
// the conjuncts favour (docs/compilers/openapi.md#competing-keywords). Else it
// is elected. pairEntries finds one oneOf and one anyOf, so pairElected
// returns one of them.
func pairedWith(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct, elected *conjunct) *conjunct {
	unions := pairEntries(s, conjuncts)
	if !slices.Contains(unions, elected) {
		return elected
	}
	return pairElected(c, s, unions)
}

// nullPairBody returns the conjunct a position elects where the oneOf and the
// anyOf written for it admit null alone (pairNullAlone) and its conjuncts
// share that null: the first entry writing no union that says what the
// position is, so the position is a use of that body keeping both, as a $ref
// body is elected by its vote (docs/compilers/openapi.md#allof-refinement).
// It is nil where none is.
func nullPairBody(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct, joint admitted) *conjunct {
	if !joint.reachable() || !pairNullAlone(c, s) {
		return nil
	}
	for i := range conjuncts {
		if cj := &conjuncts[i]; cj.branch != nil && cj.says && !declaresUnion(cj.schema) {
			return cj
		}
	}
	return nil
}

// pairEntries returns the two inline entries writing a union, one a oneOf
// and the other an anyOf, where they are the only conjuncts writing one, and
// nil otherwise.
func pairEntries(s *statement.Schema, conjuncts []conjunct) []*conjunct {
	if declaresUnion(s) {
		return nil
	}
	var unions []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		if cj.branch != nil && cj.branch.schema.Ref == nil && !cj.branch.wrapper && declaresUnion(cj.schema) {
			unions = append(unions, cj)
		}
	}
	if len(unions) != 2 || writesBothUnions(unions[0].schema) || writesBothUnions(unions[1].schema) ||
		(len(unions[0].schema.ChoiceKeywords().OneOf) > 0) == (len(unions[1].schema.ChoiceKeywords().OneOf) > 0) {
		return nil
	}
	return unions
}

// pairHeld is what the position lowering to principal admits: what principal
// does, and where it writes one union of a pair on two entries (pairEntries,
// pairElected), what the union passed over admits too, as one schema writing
// both holds both.
func pairHeld(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct, principal *conjunct) admitted {
	held := heldBy(c, principal)
	unions := pairEntries(s, conjuncts)
	if !slices.Contains(unions, principal) || pairElected(c, s, unions) != principal {
		return held
	}
	other := unions[0]
	if other == principal {
		other = unions[1]
	}
	return held.and(heldBy(c, other))
}

// unionEntries returns the conjuncts writing a union where each is an inline
// entry writing no allOf of its own, s writes no union and no conjunct is a
// $ref, and nil otherwise. The unions are looked for first, as most composing
// allOfs write none.
func unionEntries(s *statement.Schema, conjuncts []conjunct) []*conjunct {
	if declaresUnion(s) {
		return nil
	}
	var unions []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		switch {
		case cj.branch == nil || cj.branch.schema.Ref == nil && !declaresUnion(cj.schema):
		case cj.branch.schema.Ref != nil || cj.branch.wrapper:
			return nil
		default:
			unions = append(unions, cj)
		}
	}
	return unions
}

// argumentStandsAlone reports whether nothing beside unions, the conjuncts writing
// them, conjoins with them, elected writing the one the position is: no other
// conjunct writes a discriminator, which routes that union, and each keyword a
// conjunct writes that a variant would carry (siblingKeywords), an allOf
// aside, every branch of each union says (branchesSay), a union declaring shape
// against the rest where one is written.
func standsAlone(c lowering.Ctx, s *statement.Schema, conjuncts []conjunct, unions []*conjunct, elected *conjunct) bool {
	written := false
	for i := range conjuncts {
		cj := &conjuncts[i]
		if cj != elected && cj.schema.Discriminator() != nil {
			return false
		}
		keys := slices.DeleteFunc(siblingKeywords(cj.schema), func(k string) bool { return k == "allOf" })
		if len(keys) == 0 {
			continue
		}
		for _, u := range unions {
			said := branchesSay(c, u.schema, cj.schema)
			if slices.ContainsFunc(keys, func(k string) bool { return !slices.Contains(said, k) }) {
				return false
			}
		}
		written = true
	}
	return !written || slices.ContainsFunc(unions, func(u *conjunct) bool {
		choice := u.schema.ChoiceKeywords()
		return setsDeclareShape(c, s, u.schema, choice.OneOf, choice.AnyOf)
	})
}

// pastAlone reports whether conjuncts, read past a cap, still refine: the
// election weighs one $ref and nothing else (soleRef), so it compares no two
// readings, which is all a cap stops. What it leaves open is whether the
// conjunction admits a value, read through the target's kinds, whole past a
// cap, null aside, and not asked of a use saying nothing more (one), as within
// the caps (docs/compilers/openapi.md#allof-refinement).
func pastAlone(c lowering.Ctx, conjuncts []conjunct, one bool) bool {
	ref := soleRef(conjuncts)
	if ref == nil {
		return false
	}
	kinds, _ := lowersTo(c, ref.branch.schema)
	return one || kinds != 0
}

// soleRef returns the one $ref of conjuncts where every other conjunct names
// no kind and holds an instance to nothing a reading compares
// (admitsEverything), and nil where none or several are, or another conjunct
// is weighed.
func soleRef(conjuncts []conjunct) *conjunct {
	var ref *conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		isRef := cj.branch != nil && cj.branch.schema.Ref != nil
		switch {
		case isRef && ref == nil:
			ref = cj
		case isRef || cj.says || !cj.read.admitsEverything():
			return nil
		}
	}
	return ref
}

// markBeaten marks in votes how each $ref of electable stood once principal
// was elected: beaten by a $ref principal, outside the value set elected for
// conjuncts that contradict one another, or, where value sets sharing no
// member leave no conjunct the principal (undecided), apart.
func markBeaten(w *admissionReader, votes []refCandidacy, electable []*conjunct, principal *conjunct, contradicts, undecided bool) {
	for _, cj := range electable {
		switch {
		case undecided:
			votes[cj.at] = refApart
		case cj != principal && principal.branch != nil && principal.branch.schema.Ref != nil:
			votes[cj.at] = refNarrowed
		case contradicts && !w.within(principal.read, cj.read):
			votes[cj.at] = refOutsideSet
		}
	}
}

// setElected returns the conjunct stating a value set that is elected before
// a composition: of the sets lying within every other stated, the one untie
// picks among sets alike, and nil where none states one, none lies within the
// rest, or untie elects none. A $ref's target is no statement of the
// position's. A set its own keywords leave no value is read as the keyword
// competing keywords elect (competingSet), so it lowers alike beside the allOf
// and on an entry; one that keyword leaves none either, as `enum: []`, is none
// to lower: the composition reports it.
func setElected(w *admissionReader, s *statement.Schema, conjuncts []conjunct) *conjunct {
	var sets []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		bs := s
		if cj.branch != nil {
			bs = cj.branch.schema.Schema
		}
		inline := cj.branch == nil || cj.branch.schema.Ref == nil
		if !inline || !enumWritten(bs) && bs.Const() == nil {
			continue
		}
		if !cj.read.reachable() {
			cj.read = competingSet(w, bs)
		}
		if cj.read.fixed && cj.read.reachable() {
			sets = append(sets, cj)
		}
	}
	within := slices.DeleteFunc(slices.Clone(sets), func(cj *conjunct) bool {
		return slices.ContainsFunc(sets, func(o *conjunct) bool { return o != cj && !w.setWithin(cj.read, o.read) })
	})
	if len(within) == 0 {
		return nil
	}
	if elected := untie(w, conjuncts, strongest(within)); slices.Contains(within, elected) {
		return elected
	}
	return nil
}

// competingSet is what the value set s writes admits read as the election of
// competing keywords lowers it, its `const` before its `enum`, each alone
// (docs/compilers/openapi.md#competing-keywords).
func competingSet(w *admissionReader, s *statement.Schema) admitted {
	if constant := s.Const(); constant != nil {
		return w.valueSet(valueSetOf{s, "const"}, []*yaml.Node{constant})
	}
	return w.valueSet(valueSetOf{s, "enum"}, s.Enum())
}

// restKinds returns the kinds s lowers to with the union it writes beside
// its allOf left out, and whether the kind-inference policy read them: what
// its own keywords and comp's entries declare, else claim, as the run's
// admission settles a statement's (settleKinds).
func restKinds(c lowering.Ctx, s *statement.Schema, comp allOfComposition) (kindSet, bool) {
	rest := admission{firm: declaredInstanceTypes(s) & valueKinds(c, s), claims: claimedKinds(c, s)}
	for _, e := range comp.branches {
		bs := e.schema.Schema
		if e.schema.Ref == nil && declaresUnion(bs) {
			// An entry's union is the one left out, as s's own (entryUnionsApart).
			rest.conjoinKinds(&admission{firm: declaredInstanceTypes(bs) & valueKinds(c, bs), claims: claimedKinds(c, bs)})
			continue
		}
		rest.conjoinKinds(admissionOf(c, e.schema))
	}
	rest.settleKinds()
	return rest.kinds, rest.inferred
}

// voteRefs returns how each $ref conjunct stands where the position lowers to
// kinds, declared unless inferred, indexed by branch, and the conjuncts of
// those that may be the principal. The position says what it is where those
// kinds are declared, or where a conjunct names a kind, a union beside the
// allOf among them.
func voteRefs(c lowering.Ctx, conjuncts []conjunct, kinds kindSet, inferred bool, branches int) ([]refCandidacy, []*conjunct) {
	says := !inferred && kinds != anyInstance || slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.says })
	votes := make([]refCandidacy, branches)
	var electable []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		if cj.branch == nil || cj.branch.schema.Ref == nil {
			continue
		}
		votes[cj.at] = voteOf(c, kinds, says, cj)
		if votes[cj.at] == refCandidateForDecidingConjunct {
			electable = append(electable, cj)
		}
	}
	return votes, electable
}

// voteOf returns how the $ref conjunct ref stands where the position lowers
// to kinds and says, or not, what it is. What the document declares decides
// first (docs/compilers/openapi.md#policies): before a kind the policy reads
// off the target's shape keywords where kinds hold one, null aside, that it
// leaves out, as the target then admits that kind whole, and before a target
// admitting every kind, which says nothing of what the position is.
func voteOf(c lowering.Ctx, kinds kindSet, says bool, ref *conjunct) refCandidacy {
	target, targetInferred := lowersTo(c, ref.branch.schema)
	switch {
	case targetInferred && (kinds&^kindNull&^target != 0 || kinds&target == 0):
		return refInferredApart
	case !targetInferred && target == anyInstance && says:
		return refAdmitsEvery
	default:
		return refCandidateForDecidingConjunct
	}
}

// readThrough returns the index of the $ref branch a use of the position reads
// on through: the principal where it is a $ref, else the one $ref written, -1
// where none or several are.
func readThrough(conjuncts []conjunct, principal *conjunct) int {
	if principal.branch != nil && principal.branch.schema.Ref != nil {
		return principal.at
	}
	ref := -1
	for _, cj := range conjuncts {
		if cj.branch == nil || cj.branch.schema.Ref == nil {
			continue
		}
		if ref >= 0 {
			return -1
		}
		ref = cj.at
	}
	return ref
}

// heldBy returns what the position lowering to the conjunct principal admits:
// what the node a branch lowers to admits (loweredAs), and what the keywords
// beside the allOf admit where none is the principal.
func heldBy(c lowering.Ctx, principal *conjunct) admitted {
	if principal.branch == nil {
		return principal.read
	}
	return loweredAs(c, principal.branch.schema, principal.read)
}

// refinableBranches reports whether every branch of comp can be read as a
// conjunct of a refinement: none nests past the flattening depth or is
// `false`, and there are no more than the election compares. A union on a
// branch is a conjunct admitting what its branches do (inlineConjunct).
func refinableBranches(comp allOfComposition) bool {
	if len(comp.branches) > maxRefinedConjuncts {
		return false
	}
	return !slices.ContainsFunc(comp.branches, func(e allOfBranch) bool {
		return e.capped || e.schema.Truth.ReadsFalse()
	})
}

// conjuncts reads s's own keywords, then each branch of comp but a `true`,
// which says nothing, then a union s writes beside the allOf as part of its
// own keywords, as on an inline branch. A union sharing no kind with the rest,
// one of null alone beside a rest admitting no null among them, is left out,
// which the second result reports. A wrapper's own keywords are
// read alone: its entries are conjuncts of their own.
func (w *admissionReader) conjuncts(s *statement.Schema, comp allOfComposition) ([]conjunct, bool) {
	out := make([]conjunct, 0, 1+len(comp.branches))
	claims := inferredClaims(w.c, s)
	own := conjunct{read: w.written(s, w.nullable), schema: s, says: saysKind(s) || claimsAll(w.c, s, claims), at: -1}
	if nullIdiom(w.c, s) {
		// The null is the reference's, which the policy widens (nulls.widenRef).
		own.read.kinds, own.says = kindAny, false
	}
	out = append(out, own)
	for i := range comp.branches {
		e := &comp.branches[i]
		bs := e.schema.Schema
		switch {
		case !stated(bs):
		case e.schema.Ref != nil:
			out = append(out, conjunct{branch: e, at: i, read: w.of(e.schema), schema: bs})
		default:
			out = append(out, conjunct{branch: e, at: i, read: inlineConjunct(w, bs), schema: bs,
				says: saysKind(bs) || declaresUnion(bs) || claimsAll(w.c, bs, claims), form: unionForm(bs)})
		}
	}
	if !declaresUnion(s) {
		return out, w.entryUnionsApart(s, comp, out)
	}
	union, rest := unionsIn(w, s), out[0].read
	for _, cj := range out[1:] {
		rest = rest.and(cj.read)
	}
	if rest.reachable() && (pairSharesNoValue(w.c, s) && pairApartBeside(rest) || union.kinds != 0 && rest.kinds&union.kinds == 0) {
		return out, true
	}
	out[0].read, out[0].says, out[0].form = out[0].read.and(union), true, unionForm(s)
	return out, false
}

// entryUnionsApart reports whether the unions comp's inline entries write
// leave nothing beside a rest that admits a value and is no object: a oneOf
// and an anyOf sharing no value (pairApart), or unions sharing no kind with
// that rest. No instance satisfies them, so the position lowers as the rest
// does, as beside the allOf (unionAside); each such entry of out is then read
// by its own keywords alone. Beside a rest of objects alone, null aside, the
// allOf composes as it would anyway.
func (w *admissionReader) entryUnionsApart(s *statement.Schema, comp allOfComposition, out []conjunct) bool {
	rest, unions := out[0].read, admitted{kinds: kindAny}
	var holders []int
	for i := 1; i < len(out); i++ {
		cj := &out[i]
		if cj.branch == nil || cj.branch.schema.Ref != nil || !declaresUnion(cj.schema) {
			rest = rest.and(cj.read)
			continue
		}
		rest = rest.and(w.written(cj.schema, w.nullable))
		unions = unions.and(unionsIn(w, cj.schema))
		holders = append(holders, i)
	}
	if len(holders) == 0 || w.incomplete() || !rest.reachable() || !pairApartBeside(rest) {
		return false
	}
	noKind := unions.kinds != 0 && rest.kinds&unions.kinds == 0
	if !noKind && (!writesUnionPair(s, comp) || pairApart(w.c, s) == nil) {
		return false
	}
	for _, i := range holders {
		bs := out[i].schema
		out[i].read, out[i].says, out[i].form = w.written(bs, w.nullable), saysKind(bs), formWords
	}
	return true
}

// pairApartBeside reports whether a oneOf and an anyOf sharing no value
// (pairApart) lower apart from a rest admitting rest: one neither every kind,
// where the two stand alone (unionPair), nor objects alone, null aside, where
// the allOf composes the Model keeping both.
func pairApartBeside(rest admitted) bool {
	return rest.kinds != kindAny && rest.kinds&^kindNull != kindObject
}

// inferredClaims returns the kinds s lowers to, null aside, where the
// kind-inference policy read them off the claims of its conjuncts, and none
// where s or a conjunct declares a kind.
func inferredClaims(c lowering.Ctx, s *statement.Schema) kindSet {
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	if !inferred {
		return 0
	}
	return kinds &^ kindNull
}

// claimsAll reports whether the keywords s writes itself claim every kind of
// claims, the kinds the position lowers to by the kind-inference policy
// (inferredClaims): s then says what kind of value the position is, as one
// writing that `type` would, so `{allOf: [{items: X}]}` is a use of the list
// its entry is, as `{items: X}` is one (docs/compilers/openapi.md#shape-applicator).
func claimsAll(c lowering.Ctx, s *statement.Schema, claims kindSet) bool {
	return claims != 0 && claims&^claimedKinds(c, s) == 0
}

// inlineConjunct is what bs, an inline branch, admits as w reads it: its own
// keywords and the oneOf and anyOf it writes, whose branches name the kinds it
// admits. Its allOf is read apart, since its entries are conjuncts of their
// own.
func inlineConjunct(w *admissionReader, bs *statement.Schema) admitted {
	read := w.written(bs, w.nullable)
	if declaresUnion(bs) {
		read = read.and(unionsIn(w, bs))
	}
	return read
}

// unionsIn is what an instance of each oneOf and anyOf s writes admits
// together, any of a union's branches, as w reads them.
func unionsIn(w *admissionReader, s *statement.Schema) admitted {
	read := admitted{kinds: kindAny}
	choice := s.ChoiceKeywords()
	for _, branches := range [][]statement.Subschema{choice.OneOf, choice.AnyOf} {
		if len(branches) > 0 {
			read = read.and(w.union(branches))
		}
	}
	return read
}

// nullIdiom reports whether s writes beside its $ref a type naming null alone,
// which the nullable-inference policy reads as that reference or null
// (docs/compilers/openapi.md#nullable): the null widens the reference rather
// than conjoining with it, so the type says nothing else of the position.
func nullIdiom(c lowering.Ctx, s *statement.Schema) bool {
	return s.BesideRef != nil && c.NullableInference.Honours() && writesType(s) && len(effectiveTypes(s)) == 0
}

// saysKind reports whether s writes what kind of value it admits.
func saysKind(s *statement.Schema) bool {
	return writesType(s) || enumWritten(s) || s.Const() != nil
}

// disjoint reports whether the conjuncts share no value. A null they all admit
// is no value a type is left to describe, unless a conjunct admits nothing else,
// the two entries of a pair (pairEntries) read together as one schema writing
// both is. Such an allOf stays a composition, and
// docs/compilers/openapi.md#allof-refinement says which report each
// contradiction gets.
func disjoint(joint admitted, conjuncts []conjunct, pair []*conjunct) bool {
	if !joint.reachable() {
		return true
	}
	if joint.kinds != kindNull || len(pair) == 2 && pair[0].read.and(pair[1].read).kinds == kindNull {
		return false
	}
	return !slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.read.kinds == kindNull })
}
