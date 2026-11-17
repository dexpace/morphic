// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
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
	votes []refVote
	// joint is what the conjuncts admit together.
	joint admitted
	// held is what the position lowers to admits, which decides what a keyword
	// left unread loses.
	held admitted
}

// principalIs reports whether e is the branch the position lowers from.
func (r refinement) principalIs(e allOfBranch) bool {
	return r.principal != nil && r.principal.pointer == e.pointer
}

// refVote is how a $ref conjunct stood in the election of a principal.
type refVote uint8

const (
	// refElectable marks a $ref that could be the principal, and is, or lost
	// to a value set.
	refElectable refVote = iota
	// refNarrowed marks one another $ref beat, lying within more conjuncts.
	refNarrowed
	// refInferredApart marks one whose target the kind-inference policy reads
	// as a kind the position's declarations exclude.
	refInferredApart
	// refAdmitsEvery marks one whose target admits every kind beside a
	// position declaring its kinds.
	refAdmitsEvery
)

// conjunct is one conjunct's reading, for the election.
type conjunct struct {
	// branch is nil for the keywords written beside the allOf, and at is its
	// index among the composition's branches.
	branch *allOfBranch
	at     int
	read   admitted
	// says marks an inline conjunct writing what kind of value this is.
	says bool
}

// refinementAt keys a position's decision: where a schema is read from says where
// its branches are, so one schema read from two pointers is two answers.
type refinementAt struct {
	schema  *statement.Schema
	pointer jsontext.Pointer
}

// decision is what a position's readers share: a refinement, where it is one,
// and whether a union written beside the allOf shares no kind with the rest,
// which the position leaves out and keeps (unionAside).
type decision struct {
	r       refinement
	refines bool
	aside   bool
}

// refinementOf decides whether s, written at pointer, refines rather than
// composes (docs/compilers/openapi.md#allof-refinement). It reads the source and
// the bindings, never the registry, so a decision turns on no lowering order, and
// each position's many readers share one reading.
func refinementOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (refinement, bool) {
	d := decisionOf(c, s, pointer)
	return d.r, d.refines
}

// decisionOf returns the decision on s, written at pointer, read once a run,
// and none where its allOf may not refine.
func decisionOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) decision {
	if !allOfMayRefine(s) {
		return decision{}
	}
	return lowering.Remembered(c, refinementAt{s, pointer}, func() decision { return decide(c, s, pointer) })
}

// unionAside reports whether the oneOf or anyOf s writes beside its allOf
// shares no kind with what the rest of s admits: no instance satisfies them
// together, so the position lowers as the rest does and keeps the union
// (docs/compilers/openapi.md#union-beside-structure).
func unionAside(c lowering.Ctx, s *statement.Schema) bool {
	return decisionOf(c, s, "").aside
}

// decide reads the decision refinementOf remembers. The position composes
// where the conjuncts share no value, where the kinds it lowers to, null
// aside, are an object alone, and where the read is cut short; anything else
// is one use of its principal.
func decide(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) decision {
	comp := allOfBranches(c, s, pointer)
	if !refinableBranches(comp) {
		return decision{}
	}
	w := newAdmission(c)
	conjuncts, aside := w.conjuncts(s, comp)
	joint := conjuncts[0].read
	for _, cj := range conjuncts[1:] {
		joint = joint.and(cj.read)
	}
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	if aside {
		kinds, inferred = restKinds(c, s, comp)
	}
	if disjoint(joint, conjuncts) || kinds == kindObject {
		return decision{aside: aside}
	}
	votes, electable := voteRefs(c, conjuncts, kinds, inferred, len(comp.branches))
	principal := w.elect(conjuncts, electable)
	r := refinement{comp: comp, principal: principal.branch, votes: votes, joint: joint, held: heldBy(c, principal)}
	r.ref = readThrough(conjuncts, principal)
	for _, cj := range electable {
		if cj != principal && principal.branch != nil && principal.branch.schema.Ref != nil {
			r.votes[cj.at] = refNarrowed
		}
	}
	return decision{r: r, refines: !w.incomplete(), aside: aside}
}

// restKinds returns the kinds s lowers to with the union it writes beside
// its allOf left out, and whether the kind-inference policy read them: what
// its own keywords and comp's entries declare, else claim, as the run's
// admission settles a statement's (settleKinds).
func restKinds(c lowering.Ctx, s *statement.Schema, comp allOfComposition) (kindSet, bool) {
	rest := reading{firm: declaredInstanceTypes(s) & valueKinds(c, s), claims: claimedKinds(c, s)}
	for _, e := range comp.branches {
		rest.conjoinKinds(admissionOf(c, e.schema))
	}
	rest.settleKinds()
	return rest.kinds, rest.inferred
}

// voteRefs returns how each $ref conjunct stands where the position lowers to
// kinds, declared unless inferred, indexed by branch, and the conjuncts of
// those that may be the principal.
func voteRefs(c lowering.Ctx, conjuncts []conjunct, kinds kindSet, inferred bool, branches int) ([]refVote, []*conjunct) {
	votes := make([]refVote, branches)
	var electable []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		if cj.branch == nil || cj.branch.schema.Ref == nil {
			continue
		}
		votes[cj.at] = voteOf(c, kinds, inferred, cj)
		if votes[cj.at] == refElectable {
			electable = append(electable, cj)
		}
	}
	return votes, electable
}

// voteOf returns how the $ref conjunct ref stands where the position lowers
// to kinds, declared unless inferred. What the document declares decides
// first (docs/compilers/openapi.md#policies): before a kind the policy reads
// off the target's shape keywords that kinds exclude, and before a target
// admitting every kind, which says nothing of what the position is.
func voteOf(c lowering.Ctx, kinds kindSet, inferred bool, ref *conjunct) refVote {
	target, targetInferred := lowersTo(c, ref.branch.schema)
	switch {
	case targetInferred && target&kinds == 0:
		return refInferredApart
	case !targetInferred && target == anyInstance && !inferred && kinds != anyInstance:
		return refAdmitsEvery
	default:
		return refElectable
	}
}

// readThrough returns the $ref branch a use of the position reads on through:
// the principal where it is a $ref, else the one $ref written, nil where none
// or several are.
func readThrough(conjuncts []conjunct, principal *conjunct) *allOfBranch {
	if principal.branch != nil && principal.branch.schema.Ref != nil {
		return principal.branch
	}
	var ref *allOfBranch
	for _, cj := range conjuncts {
		if cj.branch == nil || cj.branch.schema.Ref == nil {
			continue
		}
		if ref != nil {
			return nil
		}
		ref = cj.branch
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
// own keywords, as on an inline branch. A union sharing no kind with the rest
// is left out, which the second result reports. A wrapper's own keywords are
// read alone: its entries are conjuncts of their own.
func (w *admission) conjuncts(s *statement.Schema, comp allOfComposition) ([]conjunct, bool) {
	out := make([]conjunct, 0, 1+len(comp.branches))
	out = append(out, conjunct{read: w.written(s, w.nullable), says: saysKind(s), at: -1})
	for i := range comp.branches {
		e := &comp.branches[i]
		switch {
		case !stated(e.schema.Schema):
		case e.schema.Ref != nil:
			out = append(out, conjunct{branch: e, at: i, read: w.of(e.schema)})
		default:
			out = append(out, conjunct{branch: e, at: i, read: inlineConjunct(w, e.schema.Schema),
				says: saysKind(e.schema.Schema) || declaresUnion(e.schema.Schema)})
		}
	}
	if !declaresUnion(s) {
		return out, false
	}
	union, rest := unionsIn(w, s), out[0].read
	for _, cj := range out[1:] {
		rest = rest.and(cj.read)
	}
	if rest.reachable() && union.kinds&^kindNull != 0 && rest.kinds&union.kinds == 0 {
		return out, true
	}
	out[0].read, out[0].says = out[0].read.and(union), true
	return out, false
}

// inlineConjunct is what bs, an inline branch, admits as w reads it: its own
// keywords and the oneOf and anyOf it writes, whose branches name the kinds it
// admits. Its allOf is read apart, since its entries are conjuncts of their
// own.
func inlineConjunct(w *admission, bs *statement.Schema) admitted {
	read := w.written(bs, w.nullable)
	if declaresUnion(bs) {
		read = read.and(unionsIn(w, bs))
	}
	return read
}

// unionsIn is what an instance of each oneOf and anyOf s writes admits
// together, any of a union's branches, as w reads them.
func unionsIn(w *admission, s *statement.Schema) admitted {
	read := admitted{kinds: kindAny}
	choice := choiceIn(s)
	for _, branches := range [][]statement.Sub{choice.OneOf, choice.AnyOf} {
		if len(branches) > 0 {
			read = read.and(w.union(branches))
		}
	}
	return read
}

// saysKind reports whether s writes what kind of value it admits.
func saysKind(s *statement.Schema) bool {
	return writesType(s) || enumWritten(s) || constIn(s) != nil
}

// disjoint reports whether the conjuncts share no value. A null they all admit
// is no value a type is left to describe, unless a conjunct admits nothing else.
// Such an allOf stays a composition, and
// docs/compilers/openapi.md#allof-refinement says which report each
// contradiction gets.
func disjoint(joint admitted, conjuncts []conjunct) bool {
	if !joint.reachable() {
		return true
	}
	return joint.kinds == kindNull &&
		!slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.read.kinds == kindNull })
}

// elect picks the conjunct the position lowers from, of refs the $refs that
// may be: the inline value set within every other, since it fixes the values
// and what it narrows cannot; else a $ref, since replacing it drops what its
// target says (narrowestRef); else, of those naming a kind, the conjunct
// within the most others, the keywords beside the allOf first, a tie going to
// the first and leaving the other kept and reported. A `format` or `items`
// naming no kind only qualifies one, so it never decides, and where nothing
// names a kind no branch does.
func (w *admission) elect(conjuncts []conjunct, refs []*conjunct) *conjunct {
	for i := range conjuncts {
		if w.narrowsAll(conjuncts, i, refs) {
			return &conjuncts[i]
		}
	}
	if len(refs) > 0 {
		return narrowestRef(w, conjuncts, refs)
	}
	best, most := 0, 0
	for i := range conjuncts {
		if !conjuncts[i].says {
			continue
		}
		if n := w.conesOver(conjuncts, conjuncts[i].read); n > most {
			best, most = i, n
		}
	}
	return &conjuncts[best]
}

// narrowestRef returns the one of refs lying within the most conjuncts, a tie
// going to the least reference text: a $ref's name stays put under edits to
// what it names, so no order written decides.
func narrowestRef(w *admission, conjuncts []conjunct, refs []*conjunct) *conjunct {
	best, most := refs[0], w.conesOver(conjuncts, refs[0].read)
	for _, ref := range refs[1:] {
		n := w.conesOver(conjuncts, ref.read)
		if n > most || n == most && refText(ref) < refText(best) {
			best, most = ref, n
		}
	}
	return best
}

// refText is the reference the $ref conjunct cj writes, as written.
func refText(cj *conjunct) string {
	_, text := refSpelling(cj.branch.schema)
	return text
}

// narrowsAll reports whether conjunct i is a value set within every other
// conjunct and more than a restatement of what a $ref of refs fixes, which a
// $ref's own is not. The keywords beside the allOf are one where they write an
// enum or a const (allOfDecides). No set is compared with itself, which answers
// nothing and walks the whole set.
func (w *admission) narrowsAll(conjuncts []conjunct, i int, refs []*conjunct) bool {
	cj := &conjuncts[i]
	if !cj.read.fixed || slices.Contains(refs, cj) ||
		slices.ContainsFunc(refs, func(ref *conjunct) bool { return w.setWithin(ref.read, cj.read) }) {
		return false
	}
	for j := range conjuncts {
		if o := &conjuncts[j]; j != i && (!cj.read.kindsWithin(o.read) || !w.setWithin(cj.read, o.read)) {
			return false
		}
	}
	return true
}

// setWithin is a.valuesWithin(b), telling walked how many members it looked up.
func (w *admission) setWithin(a, b admitted) bool {
	within, walked := a.walkWithin(b)
	if w.walked != nil {
		w.walked(walked)
	}
	return within
}

// conesOver counts the conjuncts what read admits lies within.
func (w *admission) conesOver(conjuncts []conjunct, read admitted) int {
	n := 0
	for j := range conjuncts {
		if w.within(read, conjuncts[j].read) {
			n++
		}
	}
	return n
}

// lowerPrincipal lowers the branch r uses, at the pointer it is written at, so
// nothing is minted and each node stands at a pointer naming its schema. A $ref
// goes through Ref, as fillRefBranch's does, keeping the alias what is written
// beside it always has. An inline branch that reduces to a shared node leaves
// what it writes to the position, as a voice. Either takes a frame of its own, so
// a refinement counts two against maxSchemaDepth, a limit no real schema nears. A
// $ref the principal beat is still resolved: nothing else may lower what it names.
func lowerPrincipal(c lowering.Ctx, ts *compile.Types, depth int, r refinement) (ir.TypeID, []ir.Diagnostic) {
	e := r.principal
	resolveBeaten(c, ts, r)
	if e.schema.Ref != nil {
		ref, diags := Ref(c, ts, depth, e.schema, e.pointer)
		return ref.Target, diags
	}
	ref, diags := CarriedRef(c, ts, depth, e.schema, e.pointer)
	return ref.Target, diags
}

// resolveBeaten resolves each $ref of r but its principal: nothing else may
// lower what a $ref the principal beat names.
func resolveBeaten(c lowering.Ctx, ts *compile.Types, r refinement) {
	for _, e := range r.comp.branches {
		if e.schema.Ref != nil && !r.principalIs(e) {
			resolveSchemaRef(c, ts, e.schema)
		}
	}
}

// refinedShape describes what a refinement lowers to, for the diagnostic
// recordUnhomedAt reports.
const refinedShape = "a use of one of its allOf conjuncts"

// outcomeKeeps is what a position that lowered to a reference keeps verbatim: the
// census keywords it wrote, described by shape, the allOf branches its lowering
// left unread, the null branches a collapsed union lifts that say more than
// their null (nullBranchKeeps), and the union keywords beside the allOf it
// keeps as validation logic, their branches declaring no shape (keptUnion).
type outcomeKeeps struct {
	keywords   []string
	shape      string
	comp       allOfComposition
	branches   []keptBranch
	nulls      []keptNullBranch
	validation []string
}

// keptBranch is one allOf branch a refinement keeps whole, the keywords nothing
// consumed, none for a $ref the position lost, how that $ref stood in the
// election (voteOf), and why what it writes did not fold where that is the
// reason (unfoldedWhy).
type keptBranch struct {
	entry    allOfBranch
	residue  []string
	vote     refVote
	unfolded string
}

// holdsAny reports whether k keeps anything.
func (k outcomeKeeps) holdsAny() bool {
	return len(k.keywords) > 0 || len(k.branches) > 0 || len(k.nulls) > 0 || len(k.validation) > 0
}

// keepsAtOutcome returns what the position s is written at keeps, having lowered
// to target: a refinement's census and the branches it left, or the census of
// any other outcome lowering to a reference (referenceOutcomeKeywords).
func keepsAtOutcome(c lowering.Ctx, ts *compile.Types, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) outcomeKeeps {
	r, refined := refinementOf(c, s, pointer)
	if !refined {
		keywords, shape := referenceOutcomeKeywords(c, s, pointer)
		k := outcomeKeeps{keywords: keywords, shape: shape}
		if shape == collapsedShape {
			k.nulls = nullBranchKeeps(c, s, pointer)
		}
		return k
	}
	k := outcomeKeeps{shape: refinedShape, comp: r.comp}
	typed := valueSetNode(c, ts, r, pointer)
	if r.principal != nil {
		degraded, validation := keptUnion(c, r, s)
		k.keywords, k.validation = append(refinedUnhomed(c, r, s, typed), degraded...), validation
	}
	homes := homesAt(c, ts, s, target, pointer)
	voices := voicingOf(c, s)
	w := newAdmission(c)
	for i, e := range r.comp.branches {
		// A principal keeps what it writes where it lowered: a $ref on its alias, an
		// inline branch on the node it owns, a type array's census included.
		if r.principalIs(e) && (e.schema.Ref != nil || LoweredToOwnNode(c, e.pointer, target)) {
			continue
		}
		switch {
		case e.schema.Ref != nil:
			k.branches = append(k.branches, keptBranch{entry: e, vote: r.votes[i]})
		case stated(e.schema.Schema):
			residue := unmergedBranchKeys(e.schema.Schema, func(key string) bool {
				return (key == "allOf" && e.wrapper) || (voices.speaks[i] && folds(c, e.schema.Schema, key, homes)) || said(&w, r, typed, e.schema.Schema, key)
			})
			if len(residue) > 0 {
				k.branches = append(k.branches, keptBranch{entry: e, residue: residue,
					unfolded: unfoldedWhy(c, voices.speaks[i], voices.rebased[i], e.schema.Schema, residue)})
			}
		}
	}
	return k
}

// homesAt returns which bounds the position s is written at, lowered to target,
// holds for its voices, as the census asks of those written beside its allOf. A
// carrier or an alias holds the value bounds the types the position admits take
// (boundKinds), and collection bounds where it is a use of the principal, a
// reference (keywordHome), where those types admit an array. A node the position
// owns holds what its kind does, of the value bounds those take, and the Union a
// type list lowers to the bounds its variants do, each the bounds of its own type
// (lowerTypeVariant).
func homesAt(c lowering.Ctx, ts *compile.Types, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) boundHomes {
	if !LoweredToOwnNode(c, pointer, target) {
		r, refined := refinementOf(c, s, pointer)
		takes := boundKinds(c, s)
		return boundHomes{value: takes, collection: refined && r.principal != nil && takes&kindArray != 0}
	}
	// A node still building holds nothing yet.
	var homes boundHomes
	if td, ok := ts.Node(target.Target); ok {
		nodes := withVariants(ts, td)
		if slices.ContainsFunc(nodes, constraintsHome) {
			homes.value = heldAmong(nodes, boundKinds(c, s))
		}
		homes.collection = slices.ContainsFunc(nodes, holdsCollectionBounds)
	}
	return homes
}

// withVariants returns td, a node a position owns, and, where it is a Union,
// the registered nodes of its variants: what may hold the bounds the position
// writes, as a oneOf's or an anyOf's Union does itself and the variants of a
// type list's or a distributed union's do (lowerTypeVariant, composedVariant).
func withVariants(ts *compile.Types, td ir.TypeDef) []ir.TypeDef {
	u, isUnion := td.(*ir.Union)
	if !isUnion {
		return []ir.TypeDef{td}
	}
	return append(variantNodes(ts, u), u)
}

// variantNodes returns the registered nodes of u's variants.
func variantNodes(ts *compile.Types, u *ir.Union) []ir.TypeDef {
	nodes := make([]ir.TypeDef, 0, len(u.Variants))
	for _, v := range u.Variants {
		if td, ok := ts.Node(v.Type.Target); ok {
			nodes = append(nodes, td)
		}
	}
	return nodes
}

// refinedUnhomed returns the census keywords s writes beside an allOf that
// lowered to its principal, but for those the principal already says, where a
// $ref site reads no target and keeps them all (GitHub #1067). typed is the value
// set the principal lowered to, if any (valueSetNode).
func refinedUnhomed(c lowering.Ctx, r refinement, s *statement.Schema, typed ir.TypeDef) []string {
	w := newAdmission(c)
	held := slices.DeleteFunc(slices.Clone(censusKeywords), func(key string) bool { return !said(&w, r, typed, s, key) })
	return referenceUnhomed(c, s, held)
}

// keptUnion returns the union keywords s writes beside an allOf that lowered to
// its principal r, a keyword beside a refinement, that the position keeps: none
// the principal says (unionHeld), as validation logic those whose branches
// declare no shape, as the same union beside any body is, and every other one
// degraded (docs/compilers/openapi.md#allof-refinement).
func keptUnion(c lowering.Ctx, r refinement, s *statement.Schema) (degraded, validation []string) {
	if !declaresUnion(s) {
		return nil, nil
	}
	shapeless := !unionBranchesDeclareShape(c, s)
	w := newAdmission(c)
	for _, key := range []string{"oneOf", "anyOf"} {
		switch {
		case !hasKey(s, key) || unionHeld(&w, r, s, key):
		case shapeless:
			validation = append(validation, key)
		default:
			degraded = append(degraded, key)
		}
	}
	return degraded, validation
}

// said reports whether the principal of r already says what s's keyword key
// does: what it admits holds it, a union one of whose branches it restates
// (unionHeld), or it is a format the value set it lowered to, typed, holds in
// its ValueType (typeValueSet), that format's primitive or a narrower one. A
// type that ValueType holds, its values hold too.
func said(w *admission, r refinement, typed ir.TypeDef, s *statement.Schema, key string) bool {
	if w.holds(r.held, s, key) || unionHeld(w, r, s, key) {
		return true
	}
	return typed != nil && key == "format" && formatHome(typed, s)
}

// unionHeld reports whether every value r holds satisfies s's oneOf or anyOf,
// the keyword key names: a branch says nothing r's principal does not
// (admission.restates), and for a oneOf one alone, sharing no kind with any
// other, so that exactly one admits each of its values. A branch is compared
// keyword by keyword, as what it admits leaves its bounds and annotations unread,
// and one writing what no reading compares says more. A read cut short says no.
func unionHeld(w *admission, r refinement, s *statement.Schema, key string) bool {
	var branches []statement.Sub
	switch key {
	case "oneOf":
		branches = choiceIn(s).OneOf
	case "anyOf":
		branches = choiceIn(s).AnyOf
	default:
		return false
	}
	principal := w.c.Statements().Read(nil)
	if r.principal != nil {
		principal = r.principal.schema
	}
	within, overlaps := 0, 0
	for _, b := range branches {
		branch := w.c.Statements().Sub(b)
		switch {
		case w.restates(r.held, principal, branch):
			within++
		case r.held.kinds&lowered(w, branch).kinds != 0:
			overlaps++
		}
	}
	if w.incomplete() || within == 0 {
		return false
	}
	return key == "anyOf" || (within == 1 && overlaps == 0)
}

// recordOn keeps k on the node owner names.
func (k outcomeKeeps) recordOn(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	return append(diags, k.record(c, ts, &td.Common().Unmodeled, s, pointer)...)
}

// record keeps k on p and reports it. A branch the IR cannot hold is reported by
// the keeper, and nothing here claims it was kept.
func (k outcomeKeeps) record(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	diags := recordUnhomedAt(c, ts, p, s, k.keywords, k.shape, pointer)
	for _, key := range k.validation {
		diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, key, pointer, c.Place())...)
	}
	diags = append(diags, recordNullBranches(c, p, k.nulls)...)
	for _, b := range k.branches {
		kept, keptDiags := annotation.PreserveNodeInto(p, k.comp.branchKey(b.entry.pointer),
			b.entry.schema.Node, ir.ReasonDegradedLowering, c.Place().At(b.entry.pointer))
		diags = append(diags, keptDiags...)
		if kept {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s", keptWhy(b)))
		}
	}
	return diags
}

// keptWhy words why a refinement keeps a branch.
func keptWhy(b keptBranch) string {
	switch {
	case b.vote == refInferredApart:
		return "the kind-inference policy reads this $ref's target as a kind the other conjuncts of this allOf " +
			"declare none of, and a declared kind decides first, so the $ref is kept verbatim under Unmodeled"
	case b.vote == refAdmitsEvery:
		return "this $ref's target admits every kind, and the kind the other conjuncts of this allOf declare " +
			"decides first, so the $ref is kept verbatim under Unmodeled"
	case b.vote == refNarrowed:
		return "another $ref of this allOf lies within more of its conjuncts, and a use names one target, so " +
			"this $ref is kept verbatim under Unmodeled"
	case len(b.residue) == 0:
		return "the value set of this allOf narrows this $ref, and one node holds one value set, so the $ref is " +
			"kept verbatim under Unmodeled; what its target says of the position still reaches it"
	case b.unfolded != "":
		return b.unfolded
	default:
		return "this allOf lowered to a use of one of its conjuncts, which has no home for what this inline branch " +
			"adds; the branch (" + strings.Join(b.residue, ", ") + ") is kept verbatim under Unmodeled"
	}
}

// recordPrincipalResidue keeps what binds a use on a node r's principal owns,
// where no carrier reads it: lowered with no home of its own, the branch keeps it
// as any declaration owning a node does.
func recordPrincipalResidue(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	r, refined := refinementOf(c, s, pointer)
	if !refined || r.principal == nil {
		return nil
	}
	return recordDeclarationResidue(c, ts, r.principal.schema.Schema, r.principal.pointer, home)
}
