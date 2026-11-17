// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

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
	// contradicts marks conjuncts sharing no value, of which a value set was
	// elected before the composition they would otherwise lower to.
	contradicts bool
}

// principalIs reports whether e is the branch the position lowers from: its
// schema at its pointer, as an entry writing a $ref beside other keys is two
// branches at one pointer (statement.Entry).
func (r refinement) principalIs(e allOfBranch) bool {
	return r.principal != nil && r.principal.pointer == e.pointer && r.principal.schema == e.schema
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
	// refOutsideSet marks one whose target admits none of the value set
	// elected before the composition the conjuncts would lower to.
	refOutsideSet
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
	switch choice := choiceIn(s); {
	case len(choice.OneOf) > 0:
		return formOneOf
	case len(choice.AnyOf) > 0:
		return formAnyOf
	default:
		return formWords
	}
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

// usesOneRef reports whether s, whose flattened conjuncts are comp, is a use of
// one target saying nothing of it: one conjunct is a $ref writing nothing
// read beside it, as an entry read whole may (statement.ReadBeside), and s
// and every other conjunct state nothing (statesNothing). It is the
// reference, as the $ref alone is (docs/compilers/openapi.md#allof-classification).
func usesOneRef(s *statement.Schema, comp allOfComposition) bool {
	refs := 0
	for _, e := range comp.branches {
		switch {
		case e.schema.Ref != nil && slices.ContainsFunc(e.schema.Keys, func(k statement.Key) bool {
			return statement.ReadBeside(k.Name, k.Node, e.schema.Ref.Keyword)
		}):
			return false
		case e.schema.Ref != nil:
			refs++
		case !statesNothing(e.schema.Schema):
			return false
		}
	}
	return refs == 1 && statesNothing(s)
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

// decide reads the decision refinementOf remembers. Where the conjuncts share
// no value, or the kinds the position lowers to, null aside, are an object
// alone, it composes, unless a conjunct states a value set, which is elected
// first (docs/compilers/openapi.md#competing-keywords); it composes too where
// the read is cut short. Anything else, and a use of one target saying nothing
// more (usesOneRef), whatever that target admits, is one use of its principal.
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
	var principal *conjunct
	one := usesOneRef(s, comp)
	contradicts := !one && disjoint(joint, conjuncts)
	if contradicts || kinds == kindObject && !one {
		if principal = setElected(&w, s, conjuncts); principal == nil {
			return decision{aside: aside}
		}
	}
	votes, electable := voteRefs(c, conjuncts, kinds, inferred, len(comp.branches))
	if principal == nil {
		principal = w.elect(conjuncts, electable)
	}
	r := refinement{comp: comp, principal: principal.branch, votes: votes, joint: joint, held: heldBy(c, principal),
		contradicts: contradicts}
	r.ref = readThrough(conjuncts, principal)
	for _, cj := range electable {
		switch {
		case cj != principal && principal.branch != nil && principal.branch.schema.Ref != nil:
			r.votes[cj.at] = refNarrowed
		case contradicts && !w.within(principal.read, cj.read):
			r.votes[cj.at] = refOutsideSet
		}
	}
	// A use saying nothing more of a reference naming no schema is that
	// reference, which has nothing for the read to finish.
	failed := one && r.ref != nil && !namesReferent(c, r.ref.schema)
	return decision{r: r, refines: failed || !w.incomplete(), aside: aside}
}

// setElected returns the conjunct stating a value set that is elected before
// a composition: of the sets lying within every other stated, the one untie
// picks among sets alike, and nil where none states one, none lies within the
// rest, or untie elects none. A $ref's target is no statement of the
// position's, and a set its own keywords leave no value is none to lower: the
// composition reports it.
func setElected(w *admission, s *statement.Schema, conjuncts []conjunct) *conjunct {
	var sets []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		bs := s
		if cj.branch != nil {
			bs = cj.branch.schema.Schema
		}
		inline := cj.branch == nil || cj.branch.schema.Ref == nil
		if inline && cj.read.fixed && cj.read.reachable() && (enumWritten(bs) || constIn(bs) != nil) {
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
// those that may be the principal. The position says what it is where those
// kinds are declared, or where a conjunct names a kind, a union beside the
// allOf among them.
func voteRefs(c lowering.Ctx, conjuncts []conjunct, kinds kindSet, inferred bool, branches int) ([]refVote, []*conjunct) {
	says := !inferred && kinds != anyInstance || slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.says })
	votes := make([]refVote, branches)
	var electable []*conjunct
	for i := range conjuncts {
		cj := &conjuncts[i]
		if cj.branch == nil || cj.branch.schema.Ref == nil {
			continue
		}
		votes[cj.at] = voteOf(c, kinds, says, cj)
		if votes[cj.at] == refElectable {
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
func voteOf(c lowering.Ctx, kinds kindSet, says bool, ref *conjunct) refVote {
	target, targetInferred := lowersTo(c, ref.branch.schema)
	switch {
	case targetInferred && (kinds&^kindNull&^target != 0 || kinds&target == 0):
		return refInferredApart
	case !targetInferred && target == anyInstance && says:
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
	own := conjunct{read: w.written(s, w.nullable), schema: s, says: saysKind(s), at: -1}
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
				says: saysKind(bs) || declaresUnion(bs), form: unionForm(bs)})
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
	out[0].read, out[0].says, out[0].form = out[0].read.and(union), true, unionForm(s)
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

// nullIdiom reports whether s writes beside its $ref a type naming null alone,
// which the nullable-inference policy reads as that reference or null
// (docs/compilers/openapi.md#nullable): the null widens the reference rather
// than conjoining with it, so the type says nothing else of the position.
func nullIdiom(c lowering.Ctx, s *statement.Schema) bool {
	return s.BesideRef != nil && c.NullableInference.Honours() && writesType(s) && len(effectiveTypes(s)) == 0
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
// may be: an inline value set within every other, since it fixes the values
// and what it narrows cannot; else a $ref, since replacing it drops what its
// target says (narrowestRef); else, of those naming a kind, the conjunct
// within the most others. A `format` or `items` naming no kind only qualifies
// one, so it never decides, and where nothing names a kind no branch does. No
// tie goes by the order written: untie breaks one, and words neither within
// the other elect none.
func (w *admission) elect(conjuncts []conjunct, refs []*conjunct) *conjunct {
	for i := range conjuncts {
		if w.narrowsAll(conjuncts, i, refs) {
			return untie(w, conjuncts, strongest(narrowingSets(conjuncts, i, refs)))
		}
	}
	if len(refs) > 0 {
		return narrowestRef(w, conjuncts, refs)
	}
	tied := strongest(mostWithin(w, conjuncts))
	if len(tied) > 1 && tied[0].form == formWords && !admitAlike(w, tied) {
		return &conjuncts[0]
	}
	return untie(w, conjuncts, tied)
}

// narrowingSets returns the value sets within every other conjunct, the first
// of them at i: those after it whose kinds and values are its own, as each
// set within every other is within the first, and the first within it.
func narrowingSets(conjuncts []conjunct, i int, refs []*conjunct) []*conjunct {
	first := &conjuncts[i]
	sets := []*conjunct{first}
	for j := i + 1; j < len(conjuncts); j++ {
		cj := &conjuncts[j]
		if cj.read.fixed && !slices.Contains(refs, cj) && sameValues(cj.read, first.read) {
			sets = append(sets, cj)
		}
	}
	return sets
}

// sameValues reports whether a and b, two value sets, admit the same kinds and
// hold the same members.
func sameValues(a, b admitted) bool {
	return a.kindsWithin(b) && b.kindsWithin(a) && a.valuesWithin(b) && b.valuesWithin(a)
}

// mostWithin returns the conjuncts naming a kind that lie within the most
// conjuncts, in the order written.
func mostWithin(w *admission, conjuncts []conjunct) []*conjunct {
	var tied []*conjunct
	most := 0
	for i := range conjuncts {
		cj := &conjuncts[i]
		if !cj.says {
			continue
		}
		switch n := w.conesOver(conjuncts, cj.read); {
		case n > most:
			tied, most = append(tied[:0], cj), n
		case n == most:
			tied = append(tied, cj)
		}
	}
	return tied
}

// strongest returns those of tied whose form ranks first (conjunctForm).
func strongest(tied []*conjunct) []*conjunct {
	top := formWords
	for _, cj := range tied {
		top = max(top, cj.form)
	}
	return slices.DeleteFunc(tied, func(cj *conjunct) bool { return cj.form != top })
}

// admitAlike reports whether each of tied lies within every other: what they admit
// is one, so far as a reading holds it.
func admitAlike(w *admission, tied []*conjunct) bool {
	first := tied[0].read
	return !slices.ContainsFunc(tied[1:], func(cj *conjunct) bool {
		return !w.within(first, cj.read) || !w.within(cj.read, first)
	})
}

// untie returns which of tied, conjuncts of one form, the position lowers
// from: the one there is; of words admitting alike, the pick untieWords makes;
// of unions of one keyword, the first where each is that one schema written
// again, else none, as no rule ranks their branches, and the keywords beside
// the allOf decide (docs/compilers/openapi.md#allof-refinement). A tie of no
// conjunct leaves those keywords too.
func untie(w *admission, conjuncts []conjunct, tied []*conjunct) *conjunct {
	switch {
	case len(tied) == 0:
		return &conjuncts[0]
	case len(tied) == 1:
		return tied[0]
	case tied[0].form == formWords:
		return untieWords(w, &conjuncts[0], tied)
	case writtenAlike(w, tied):
		return tied[0]
	default:
		return &conjuncts[0]
	}
}

// writtenAlike reports whether each of tied is an allOf branch written as the
// first is (admission.sameText): one schema written several times.
func writtenAlike(w *admission, tied []*conjunct) bool {
	return !slices.ContainsFunc(tied, func(cj *conjunct) bool {
		return cj.branch == nil || !w.sameText(tied[0].branch.schema, cj.branch.schema)
	})
}

// principalKeywords are the keywords a node's build reads from the principal
// alone and no other conjunct folds into, so two conjuncts admitting alike
// still part on them: what each lowers its elements or its content by.
var principalKeywords = slices.Concat([]string{"items", "prefixItems", "additionalItems"}, contentKeywords)

// untieWords returns which of tied, conjuncts admitting alike that name their
// kinds by words, the position lowers from. The pick shows only in
// principalKeywords: of those writing every such keyword any other does, and
// alike, one is elected, and where none does, none is, and beside, the
// keywords beside the allOf, decide. Where nothing tells them apart, beside
// stands if tied, else the least text (leastText).
func untieWords(w *admission, beside *conjunct, tied []*conjunct) *conjunct {
	most := slices.MaxFunc(tied, func(a, b *conjunct) int {
		return countKeys(a.schema, principalKeywords) - countKeys(b.schema, principalKeywords)
	})
	if slices.ContainsFunc(tied, func(cj *conjunct) bool { return !writesWithin(cj.schema, most.schema) }) {
		return beside
	}
	tied = slices.DeleteFunc(slices.Clone(tied), func(cj *conjunct) bool { return !writesWithin(most.schema, cj.schema) })
	if slices.Contains(tied, beside) {
		return beside
	}
	return leastText(w, tied)
}

// countKeys counts the keys of keys s writes (hasKey).
func countKeys(s *statement.Schema, keys []string) int {
	n := 0
	for _, key := range keys {
		if hasKey(s, key) {
			n++
		}
	}
	return n
}

// writesWithin reports whether each of principalKeywords a writes, b writes
// alike.
func writesWithin(a, b *statement.Schema) bool {
	return !slices.ContainsFunc(principalKeywords, func(key string) bool {
		return hasKey(a, key) && (!hasKey(b, key) || !foldValue(a, key).agrees(foldValue(b, key)))
	})
}

// leastText returns the one of tied, allOf branches admitting alike, whose
// text is the least, bytewise: the pick shows in little more than where its
// node stands, and the text, its keys sorted, reads no order the branches are
// written in. Of a schema written twice, the first copy stands for both, the
// one place order shows, and only in that node's pointer.
func leastText(w *admission, tied []*conjunct) *conjunct {
	best := tied[0]
	for _, cj := range tied[1:] {
		if bytes.Compare(w.text(cj.branch.schema), w.text(best.branch.schema)) < 0 {
			best = cj
		}
	}
	return best
}

// narrowestRef returns the one of refs lying within the most conjuncts, a tie
// going to the least reference text, then to the least text of the entry
// writing it: a $ref's name stays put under edits to what it names, so no
// order written decides. Of a $ref written twice alike, the first copy stands
// for both, and keepsAtOutcome keeps neither.
func narrowestRef(w *admission, conjuncts []conjunct, refs []*conjunct) *conjunct {
	best, most := refs[0], w.conesOver(conjuncts, refs[0].read)
	for _, ref := range refs[1:] {
		n := w.conesOver(conjuncts, ref.read)
		if n > most || n == most && refBefore(w, ref, best) {
			best, most = ref, n
		}
	}
	return best
}

// refBefore reports whether the $ref conjunct a goes before b where both lie
// within as many conjuncts: by reference text, then by the text of the entry.
func refBefore(w *admission, a, b *conjunct) bool {
	if order := strings.Compare(refText(a), refText(b)); order != 0 {
		return order < 0
	}
	return bytes.Compare(w.text(a.branch.schema), w.text(b.branch.schema)) < 0
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
// recordUnhomedAt reports, its conjuncts named as comp's subject names them.
func refinedShape(comp allOfComposition) string {
	if comp.subject() == "this schema" {
		return "a use of one of its conjuncts"
	}
	return "a use of one of its allOf conjuncts"
}

// outcomeKeeps is what a position that lowered to a reference keeps verbatim: the
// census keywords it wrote, described by shape, the $ref branches of its allOf
// its lowering left unread, what its inline branches keep (entryKeeps), the
// null branches a collapsed union lifts that say more than their null
// (nullBranchKeeps), and the union keywords beside the allOf it keeps as
// validation logic, their branches declaring no shape (keptUnion).
type outcomeKeeps struct {
	keywords   []string
	shape      string
	comp       allOfComposition
	branches   []keptBranch
	entries    []keptEntry
	nulls      []keptNullBranch
	validation []string
	// undecided marks a refinement electing no entry, which lowered by the
	// keywords beside its allOf.
	undecided bool
}

// keptBranch is one $ref branch of an allOf a refinement keeps whole, the
// position having lost it, and how it stood in the election (voteOf).
type keptBranch struct {
	entry allOfBranch
	vote  refVote
}

// holdsAny reports whether k keeps anything.
func (k outcomeKeeps) holdsAny() bool {
	return len(k.keywords) > 0 || len(k.branches) > 0 || len(k.nulls) > 0 || len(k.validation) > 0 ||
		slices.ContainsFunc(k.entries, keptEntry.keeps)
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
	k := outcomeKeeps{shape: refinedShape(r.comp), comp: r.comp, undecided: r.principal == nil}
	typed := refinedValueType(c, r, s, pointer)
	if r.principal != nil {
		degraded, validation := keptUnion(c, r, s)
		k.keywords, k.validation = append(refinedUnhomed(c, r, s, typed), degraded...), validation
	}
	homes := homesAt(c, ts, s, target, pointer)
	voices := voicingOf(c, s)
	w := newAdmission(c)
	set, setAtPosition := valueSetPrincipal(r)
	residues := make([][]string, len(r.comp.branches))
	for i, e := range r.comp.branches {
		// A principal keeps what it writes where it lowered: a $ref on its alias, an
		// inline branch on the node it owns, a type array's census included. A
		// branch written as the principal is says nothing it does not. A value
		// set's census ran on the position's node, which keeps the rest as any
		// conjunct's.
		if r.principalIs(e) && (e.schema.Ref != nil || LoweredToOwnNode(c, e.pointer, target)) || principalTwin(&w, r, e) {
			continue
		}
		censused := setAtPosition && set.pointer == e.pointer
		switch {
		case e.schema.Ref != nil:
			k.branches = append(k.branches, keptBranch{entry: e, vote: r.votes[i]})
		case stated(e.schema.Schema):
			residues[i] = unmergedBranchKeys(e.schema.Schema, func(key string) bool {
				return (key == "allOf" && e.wrapper) || (voices.speaks[i] && folds(c, e.schema.Schema, key, homes)) ||
					said(&w, r, typed, e.schema.Schema, key) || censused && censusReads(key)
			})
		}
	}
	k.entries = entryKeeps(c, s, r.comp, func(i int) []string { return residues[i] }, voices, isAdditionalProperties)
	return k
}

// isAdditionalProperties reports whether keyword is additionalProperties: the
// closure no use of a conjunct types, which reads the keywords beside it.
func isAdditionalProperties(keyword string) bool { return keyword == "additionalProperties" }

// principalTwin reports whether e, a branch of r other than its principal, is
// written as the principal is (admission.sameText): one schema written twice,
// whose first copy the election takes for both.
func principalTwin(w *admission, r refinement, e allOfBranch) bool {
	return r.principal != nil && !r.principalIs(e) && w.sameText(r.principal.schema, e.schema)
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
// $ref site reads no target and keeps them all (GitHub #1067). typed is the type
// of the value set the principal lowered to, if any (refinedValueType). A value
// set standing at the position keeps them on its own node, in its build
// (lowerPrincipalValueSet), so the outcome keeps none.
func refinedUnhomed(c lowering.Ctx, r refinement, s *statement.Schema, typed setType) []string {
	if _, atPosition := valueSetPrincipal(r); atPosition {
		return nil
	}
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
// its ValueType, that format's primitive or a narrower one, or a redaction. A
// type that ValueType holds, its values hold too.
func said(w *admission, r refinement, typed setType, s *statement.Schema, key string) bool {
	if w.holds(r.held, s, key) || unionHeld(w, r, s, key) || key == "type" && nullIdiom(w.c, s) {
		return true
	}
	return typed.ok && key == "format" && (annotation.DeclaresRedaction(s) || valueTypeHoldsFormat(typed.prim, s))
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
	kept, keptDiags := keepRefConjuncts(c, p, k.branches, pointer)
	diags = append(diags, keptDiags...)
	for _, b := range k.branches {
		if kept[b.entry.pointer] {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s", keptWhy(k.comp, b)))
		}
	}
	shape := refinedShape(k.comp)
	if k.undecided {
		shape = undecidedShape
	}
	for _, e := range k.entries {
		diags = append(diags, recordKeptEntry(c, ts, p, k.comp, e, shape, false, func(e keptEntry) ir.Diagnostic {
			return c.DiagAt(ir.SeverityInfo, wholeCode(e), e.entry.pointer, "%s", keptEntryWhy(k.comp, e, k.undecided))
		})...)
	}
	dropFoldedIntoWhole(c, p)
	return diags
}

// undecidedShape describes what a refinement electing no entry lowers to, for
// the report of what an entry of it keeps.
const undecidedShape = "what the keywords beside it say, as no entry of it is the one it lowers from " +
	"(the top type where they name no kind)"

// keptRef is the key a kept $ref conjunct is kept under, and where the entry
// is said to be written.
type keptRef struct {
	key string
	at  jsontext.Pointer
}

// keptRefKeys returns, by its pointer, the key each $ref of branches is kept
// under: openapi:$ref where one is kept, else openapi:$ref/<i>, i its rank by
// reference text, then by the entry as written, so no order of the entries
// decides (docs/compilers/openapi.md#allof-refinement). Entries written alike
// are one entry, at pointer, the position writing them.
func keptRefKeys(branches []keptBranch, pointer jsontext.Pointer) map[jsontext.Pointer]keptRef {
	alike := map[[2]string][]jsontext.Pointer{}
	for _, b := range branches {
		raw, _ := annotation.RawFromNode(b.entry.schema.Node)
		_, text := refSpelling(b.entry.schema)
		alike[[2]string{text, string(raw)}] = append(alike[[2]string{text, string(raw)}], b.entry.pointer)
	}
	written := slices.SortedFunc(maps.Keys(alike), func(a, b [2]string) int {
		return cmp.Or(strings.Compare(a[0], b[0]), strings.Compare(a[1], b[1]))
	})
	out := make(map[jsontext.Pointer]keptRef, len(branches))
	for i, w := range written {
		key := "openapi:$ref"
		if len(written) > 1 {
			key += "/" + strconv.Itoa(i)
		}
		for _, at := range alike[w] {
			out[at] = keptRef{key: key, at: at}
			if len(alike[w]) > 1 {
				out[at] = keptRef{key: key, at: pointer}
			}
		}
	}
	return out
}

// keptEntryWhy words why a refinement keeps an inline branch whole, undecided
// saying it elected no entry, naming the schema writing its conjuncts as comp's
// subject does.
func keptEntryWhy(comp allOfComposition, e keptEntry, undecided bool) string {
	if why := wholeWhy(e); why != "" {
		return why
	}
	of := comp.subject()
	if undecided {
		return "no conjunct of " + of + " is the one it lowers from, so it lowered by the keywords beside them, the " +
			"top type where those name no kind, and nothing there homes what this inline branch adds; the branch (" +
			strings.Join(e.residue, ", ") + ") is kept verbatim under Unmodeled"
	}
	return of + " lowered to a use of one of its conjuncts, which has no home for what this inline branch " +
		"adds; the branch (" + strings.Join(e.residue, ", ") + ") is kept verbatim under Unmodeled"
}

// keepRefConjuncts keeps on p each $ref conjunct of branches, one a
// refinement, a composition or a failed read leaves unlowered: the reference
// as written, its text alone, under the key keptRefKeys ranks it by, at the
// entry writing it. What an entry writes beside its $ref is kept apart, as an
// inline entry's keywords are. It reports which it kept, by pointer.
func keepRefConjuncts(c lowering.Ctx, p *ir.Unmodeled, branches []keptBranch, pointer jsontext.Pointer,
) (map[jsontext.Pointer]bool, []ir.Diagnostic) {
	var keys map[jsontext.Pointer]keptRef
	if len(branches) == 1 {
		// One kept $ref is openapi:$ref at its entry, as keptRefKeys ranks it.
		keys = map[jsontext.Pointer]keptRef{branches[0].entry.pointer: {key: "openapi:$ref", at: branches[0].entry.pointer}}
	} else {
		keys = keptRefKeys(branches, pointer)
	}
	kept := make(map[jsontext.Pointer]bool, len(branches))
	var diags []ir.Diagnostic
	for _, b := range branches {
		_, text := refSpelling(b.entry.schema)
		ref := keys[b.entry.pointer]
		ok, keptDiags := annotation.PreserveNodeInto(p, ref.key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text},
			ir.ReasonDegradedLowering, c.Place().At(ref.at))
		diags = append(diags, keptDiags...)
		kept[b.entry.pointer] = ok
	}
	return kept, diags
}

// keptWhy words why a refinement keeps a $ref branch, naming the schema
// writing its conjuncts as comp's subject does.
func keptWhy(comp allOfComposition, b keptBranch) string {
	of := comp.subject()
	switch b.vote {
	case refInferredApart:
		return "the kind-inference policy reads this $ref's target as kinds leaving out one the rest of " + of +
			" admits, so the $ref is kept verbatim under Unmodeled"
	case refAdmitsEvery:
		return "this $ref's target admits every kind, and what the other conjuncts of " + of + " name, a kind " +
			"or a union, decides first, so the $ref is kept verbatim under Unmodeled"
	case refNarrowed:
		return "another $ref of " + of + " lies within as many of its conjuncts or more, a tie going to the " +
			"least reference text, then to the least text of its entry, and a use names one target, so this " +
			"$ref is kept verbatim under Unmodeled"
	case refOutsideSet:
		return "the conjuncts of " + of + " share no value, and its value set is elected before the rest, " +
			"holding values this $ref's target does not, so the $ref is kept verbatim under Unmodeled"
	default:
		return "the value set of " + of + " narrows this $ref, and one node holds one value set, so the $ref is " +
			"kept verbatim under Unmodeled; what its target says of the position still reaches it"
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
