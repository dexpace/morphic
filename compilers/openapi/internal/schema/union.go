// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A set of one non-null branch X
// beside any number of null branches collapses to nullable X
// (docs/ir/type-graph.md#type-references); a set with no non-null branch at all
// has no X to name, so it lowers as a schema of null alone does
// (lowerNullOnlyUnion); everything else becomes a Union with one
// Variant per non-null branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields. The Union, standing where st says, holds the
// value bounds written beside it, as a branch's node, shared by every use of
// the branch, cannot (docs/compilers/openapi.md#constraints). fin finishes it.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	at := st.position(s)
	if inner, ip, ok := nullUnionCollapse(c, s, pointer); ok && !pairedPrincipal(c, st) {
		ref, diags := Ref(c, ts, depth, inner, ip)
		// The null branches say the null, so the reference admits it whatever
		// the branch it names says, and declares it where theirs or that
		// branch's own reference does (unionNullReading).
		declared := ref.Nullable && ref.Inferred == "" || valueNulls(c, s).union.declared == nullAdmitted
		return nullRef(c, ref.Target, declared, true), append(diags, liftedNullFindings(c, at, s, pointer)...)
	}
	if allNullUnion(c, at, s) {
		ref, diags := lowerNullOnlyUnion(c, ts, s, pointer, fin, st)
		if st.of == nil {
			// The union of null alone is a conjunct like any other, so a pair
			// sharing no value is reported here as where it is not null alone.
			diags = append(diags, diagUnionsShareNothing(c, s, pointer, unionShape)...)
		}
		return ref, diags
	}
	owner, diags := keepingUnread(c, ts, s, pointer, dispatch{said: branchesSay(c, s, s)}, fin, st,
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			var diags []ir.Diagnostic
			tid := internNode(c, ts, st.pointer(pointer), func(common ir.TypeCommon) (ir.TypeDef, keySet) {
				u := besideUnion(s, pointer)
				u.schema, u.dead = at, pairDeadNulls(c, at, s, st)
				def, unionDiags := buildUnion(c, ts, u, common,
					func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
						ref, refDiags := Ref(c, ts, depth, b, vptr)
						diags = append(diags, refDiags...)
						return ref
					})
				diags = append(diags, unionDiags...)
				if u.dead != nil {
					// The report names an entry's union by the entry it is written on,
					// and a dead branch by the null the rest keeps out, not by a body.
					reported := u
					reported.entry, reported.nulls = st.of != nil, true
					diags = append(diags, keepDeadBranches(c, &def.Unmodeled, at, reported, st.pointer(pointer))...)
				}
				cons, consDiags := ConstraintsOf(c, &def.Unmodeled, s, st.voices(c, s, pointer), pointer, st.takes(c, s))
				def.Constraints = cons
				diags = append(diags, consDiags...)
				read := nodeBoundsRead(s, st.takes(c, s), def.Constraints, def.Unmodeled)
				if def.Discriminator != nil {
					read |= keyBit("discriminator")
				}
				return def, read
			}, fin)
			return tid, 0, diags
		})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	if st.of == nil {
		// A principal's conjunction is reported at the schema writing it (refinedDisjoint).
		diags = append(diags, diagUnionsShareNothing(c, s, pointer, unionShape)...)
	}
	return ref, slices.Concat(diags, nullDiags, liftedNullFindings(c, at, s, pointer))
}

// pairDeadNulls marks, by index, each branch of the union s elects at
// position that is a variant admitting null alone, as a $ref to a schema of
// null alone is, where position elects one union of a oneOf and an anyOf and
// what it writes beside that union, the one passed over among it, keeps null
// out (restForbidsNull): no instance is one of both. It is nil where none is,
// where no variant would live, the pair then sharing no value, which its own
// warning says, or where s elects no union of a pair
// (docs/compilers/openapi.md#union-beside-structure).
func pairDeadNulls(c lowering.Ctx, position, s *statement.Schema, st stand) []bool {
	if !writesBothUnions(s) && !pairedPrincipal(c, st) {
		return nil
	}
	branches, _, _ := unionBranches(c, position, s)
	var dead []bool
	live := false
	for i, b := range branches {
		bs := c.Statements().Sub(b)
		if nullBranch(c, bs) {
			continue
		}
		if kinds, inferred := lowersTo(c, bs); kinds&^kindNull != 0 || inferred {
			live = true
			continue
		}
		if dead == nil {
			if !restForbidsNull(c, position, s) {
				return nil
			}
			dead = make([]bool, len(branches))
		}
		dead[i] = true
	}
	if !live {
		return nil
	}
	return dead
}

// nullUnionCollapse detects a oneOf/anyOf with exactly one non-null branch
// beside null branches (nullBranch), and returns that branch's schema and
// pointer, so it lowers as nullable X, not a union node
// (docs/ir/type-graph.md#type-references).
//
// A schema declaring both combinators collapses neither: the co-declared anyOf
// conjoins, so the position is not nullable X. The Union keeps the unused
// combinator (preserveUnusedCombinator), which X's shared node cannot.
func nullUnionCollapse(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (*statement.Statement, jsontext.Pointer, bool) {
	if choice := s.ChoiceKeywords(); len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
		return nil, "", false
	}
	variants, key, _ := unionBranches(c, s, s)
	var nonNull *statement.Statement
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, v := range variants {
		st := c.Statements().Sub(v)
		if nullBranch(c, st) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = st, i
		nonNullCount++
	}
	if nullCount == 0 || nonNullCount != 1 {
		return nil, "", false
	}
	return nonNull, pointer + ids.Ptr(key, strconv.Itoa(nonNullIdx)), true
}

// pairedPrincipal reports whether the union lowering where st says is the
// principal of a refinement passing over another entry's union
// (electsOneOfPair), which conjoins with it as a co-declared anyOf does, so it
// collapses no more than one schema writing both does.
func pairedPrincipal(c lowering.Ctx, st stand) bool {
	return st.of != nil && electsOneOfPair(c, st.of)
}

// electsOneOfPair reports whether s refines to an inline entry writing one
// union of a oneOf and an anyOf that s and its entries write between them
// (writesUnionPair), a body beside them or none, the other passed over.
func electsOneOfPair(c lowering.Ctx, s *statement.Schema) bool {
	d := decisionOf(c, s)
	if !d.refines || d.principalAt < 0 {
		return false
	}
	p := entryStatement(c, s, d.principalAt)
	if p.Ref != nil || !declaresUnion(p.Schema) || writesBothUnions(p.Schema) {
		return false
	}
	return writesUnionPair(s, allOfEntries(c, s, ""))
}

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects of s at position is a null branch (nullBranch). nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without this check
// lowerOneOfAnyOf's buildUnion fallback strips every branch as a null marker
// and interns a Union with none left — the empty value space irverify's
// ir/union-no-variants rejects (GitHub #416). The branch set still admits
// exactly one value, the one a schema of null alone at this position admits,
// so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(c lowering.Ctx, position, s *statement.Schema) bool {
	variants, _, _ := unionBranches(c, position, s)
	return len(variants) > 0 && !slices.ContainsFunc(variants, func(v statement.Sub) bool {
		return !nullBranch(c, c.Statements().Sub(v))
	})
}

// lowerNullOnlyUnion lowers a oneOf/anyOf every one of whose branches admits
// null alone (allNullUnion). One such branch or several admit what a schema of
// null alone at this position admits, so the position lowers as one does
// (lowerNullAlone), and the census a body's lowering runs keeps what the node
// cannot hold. The combinator that produced it, and the other one too when both
// are declared, carries no shape the node's fields could hold, so it is kept
// verbatim under Unmodeled instead of dropped
// (docs/compilers/openapi.md#union-null-only). The node stands where st says.
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	var branches finished
	owner, diags := keepingUnread(c, ts, s, pointer, dispatch{},
		branches.on(func(td ir.TypeDef) []ir.Diagnostic { return preserveNullOnlyUnion(c, td, s, pointer) }).then(fin), st,
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerNullAlone(c, ts, standingValueSet(s, pointer, st).finishedBy(fin)), 0, nil
		})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, branches.diags, nullDiags, liftedNullFindings(c, st.position(s), s, pointer))
}

// preserveNullOnlyUnion keeps s's oneOf/anyOf verbatim under the Unmodeled of
// td, in hand, the node lowerNullOnlyUnion built, an Enum with no member, which
// holds no branch set of its own, and reports what it kept.
func preserveNullOnlyUnion(c lowering.Ctx, td ir.TypeDef, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, &td.Common().Unmodeled, s, ir.ReasonDegradedLowering, pointer)
	if len(kept) == 0 {
		return diags
	}
	// Both keywords land here when both are declared: buildUnion, and its
	// preserveUnusedCombinator, are never reached from this arm, so this is the
	// only place either combinator gets a home.
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"every branch admits only the null value, so this position lowered as an enum with no member "+
			"behind a nullable reference; %s kept verbatim under Unmodeled", strings.Join(kept, " and ")))
}

// preserveBranchSets stores s's declared oneOf/anyOf verbatim under p, in
// keyword order, reporting one it cannot convert. ir.ReasonValidationOnly
// routes each through the validation-only keyword-family reporting
// (annotation.PreserveValidationOnlyKeyword) and returns no keywords. Every
// other reason keeps each plainly (annotation.PreserveSchemaKeyword) and returns
// the keywords kept, so a caller with a message of its own knows what to name.
//
// preserveUnionSiblingsAt and preserveNullOnlyUnion share this loop: they keep
// the same keywords because the node they attach to has no branch set of its
// own, and differ only in the sentence that explains why.
func preserveBranchSets(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, reason ir.UnmodeledReason, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var kept []string
	for _, kw := range []string{"oneOf", "anyOf"} {
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, kw, pointer, c.Place())...)
			continue
		}
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, kw, reason, pointer, c.Place())
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, kw)
		}
	}
	return kept, diags
}

// unionBranches returns the branches of the combinator holder elects at the
// position written by position, holder itself or the schema writing the allOf
// holding it, the keyword's name (for pointers), and whether it is exclusive.
//
// Of both, the oneOf wins unless only the anyOf declares shape against the
// position (anyOfElected), so every caller owns the set it passed over:
// buildUnion keeps it on the Union (preserveUnusedCombinator), nullUnionCollapse
// declines to collapse past it, and the verbatim lowering keeps both keywords
// (preserveUnionSiblingsAt). Reading the preference as one only that last
// lowering could reach is what dropped the anyOf in silence (GitHub #35).
func unionBranches(c lowering.Ctx, position, holder *statement.Schema) ([]statement.Sub, string, bool) {
	choice := holder.ChoiceKeywords()
	if len(choice.OneOf) > 0 && (len(choice.AnyOf) == 0 || !anyOfElected(c, position, holder)) {
		return choice.OneOf, "oneOf", true
	}
	return choice.AnyOf, "anyOf", false
}

// unionElectionOf keys what anyOfElected read of a holder at a position.
type unionElectionOf struct {
	position, holder *statement.Schema
}

// anyOfElected reports whether holder, writing a oneOf and an anyOf, elects its
// anyOf at position: its branches declare shape against the position and the
// oneOf's do not, so the oneOf only narrows what the position declares and
// types nothing (docs/compilers/openapi.md#competing-keywords). Read once a run,
// as every reader of the union asks.
func anyOfElected(c lowering.Ctx, position, holder *statement.Schema) bool {
	return lowering.Remembered(c, unionElectionOf{position, holder}, func() bool {
		choice := holder.ChoiceKeywords()
		return !setsDeclareShape(c, position, holder, choice.OneOf) && setsDeclareShape(c, position, holder, choice.AnyOf)
	})
}

// passedOver returns the branch set unionBranches passes over in holder at
// position, a conjunct of the rest of that position, and its keyword: none
// where holder writes one union.
func passedOver(c lowering.Ctx, position, holder *statement.Schema) ([]statement.Sub, string) {
	choice := holder.ChoiceKeywords()
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil, ""
	}
	if _, key, _ := unionBranches(c, position, holder); key == "oneOf" {
		return choice.AnyOf, "anyOf"
	}
	return choice.OneOf, "oneOf"
}

// branches is unionBranches for u's holder at its position.
func (u unionAt) branches(c lowering.Ctx) ([]statement.Sub, string, bool) {
	return unionBranches(c, u.schema, u.holder)
}

// restKeepsNull returns whether what u's position writes but u's own union
// keeps null out (restForbidsNull), read on first asking: most unions hold no
// variant admitting null.
func restKeepsNull(c lowering.Ctx, u unionAt) func() bool {
	var read, forbids bool
	return func() bool {
		if !read {
			read, forbids = true, restForbidsNull(c, u.schema, u.holder)
		}
		return forbids
	}
}

// restForbidsNull reports whether body keeps null out by what it writes but
// the union of holder, the union holder passes over among it, as
// bodyAdmitsNull reads that rest: a rest saying nothing of null keeps none
// out, and a passed-over union read past the budget decides nothing.
func restForbidsNull(c lowering.Ctx, body, holder *statement.Schema) bool {
	n := newNulls(c)
	n.variants, n.rest = true, holder
	if n.conjunct(c.Statements().Read(body), nullSite{}).forbids(c) {
		return true
	}
	passed, _ := passedOver(c, body, holder)
	if passed == nil {
		return false
	}
	m := newNulls(c)
	m.variants = true
	return m.branchesNull(passed).forbids(c) && !m.conjuncts.Exhausted()
}

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over in
// u's holder verbatim on the Union the position lowers to, and reports it. A
// holder declaring only one combinator passes over nothing and is left alone.
//
// A schema writing both conjoins them, but one ir.Union carries one branch set,
// so the other stays recoverable beside the elected one
// (docs/compilers/openapi.md#competing-keywords), as validation logic where
// its branches declare no shape against the schema at the position, an entry
// holding both as much as one written beside the allOf. Beside a body the union
// distributes across, each variant keeps it instead (buildComposedVariant).
func preserveUnusedCombinator(c lowering.Ctx, p *ir.Unmodeled, u unionAt, won string) []ir.Diagnostic {
	choice := u.holder.ChoiceKeywords()
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil
	}
	unused, set := otherCombinator[won], choice.OneOf
	if unused == "anyOf" {
		set = choice.AnyOf
	}
	if !setsDeclareShape(c, u.schema, u.holder, set) {
		// Branches narrowing only what the position declares are validation
		// logic, as the same union on an entry of its own is (entryUnionReason).
		return annotation.PreserveValidationOnlyKeyword(p, u.holder, unused, u.at, c.Place())
	}
	kept, diags := annotation.PreserveSchemaKeyword(p, u.holder, unused, ir.ReasonDegradedLowering, u.at, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, u.at,
		"oneOf and anyOf are both declared here and conjoin, and one Union carries one "+
			"branch set; this position lowered as its %s, with %s kept verbatim under Unmodeled",
		won, unused))
}

// variantTypeFunc lowers one union branch to the type its variant refers to. It
// is a parameter so the distributed lowering can conjoin the sibling composition
// into each branch while sharing every other rule about union shape — null-branch
// stripping, variant hints, exclusivity, pointer derivation.
type variantTypeFunc func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef

// buildUnion assembles the Union node for the union u, attaching the
// discriminator standing with it, where one does, with the routes it implies
// (spellImplicitRoutes). common is already built by the caller (internNode), so
// buildUnion needs no hint of its own to build one, and it places the union.
//
// The variants are lowered before the discriminator, so its mapping routes to
// them (indexBranches). A union the discriminator routes is WireTagged, an
// anyOf as much as a oneOf: the tag property's value names its variant
// (docs/ir/unions.md).
func buildUnion(c lowering.Ctx, ts *compile.Types, u unionAt, common ir.TypeCommon, variantType variantTypeFunc,
) (*ir.Union, []ir.Diagnostic) {
	branches, key, exclusive := u.branches(c)
	var diags []ir.Diagnostic
	if !u.distributed {
		diags = preserveUnusedCombinator(c, &common.Unmodeled, u, key)
	}
	diags = append(diags, recordNullBranches(c, &common.Unmodeled, nullBranchKeeps(c, u.schema, u.holder, u.at))...)
	variants := make([]ir.Variant, 0, len(branches))
	keepsNull := restKeepsNull(c, u)
	for i, b := range branches {
		st := c.Statements().Sub(b)
		if !u.variantBranch(c, i, st) {
			continue // a null branch lifts to the enclosing ref's Nullable bit; a dead one is kept
		}
		vptr := u.at + ids.Ptr(key, strconv.Itoa(i))
		t := variantType(st, vptr)
		if t.Nullable && keepsNull() {
			// A null is shared only where every conjunct may admit it, so a
			// rest keeping it out leaves the variant none, its other values
			// living (docs/compilers/openapi.md#union-null-variant).
			t.Nullable, t.Inferred = false, ""
		}
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(variantHint(c, st, vptr)),
			Type: t,
		})
	}
	node := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		Closed:     true,
	}
	var x *branchIndex
	if u.disc != nil {
		x = indexBranches(c, u, variants)
	}
	disc, discDiags := lowerDiscriminator(c, ts, u.disc, u.discAt, nil, x)
	node.Discriminator = disc
	node.WireTagged = disc != nil
	diags = append(diags, discDiags...)
	diags = append(diags, keepBranchDiscriminatorKeys(c, node, u)...)
	diags = append(diags, keepDeadRoutes(c, node, u)...)
	diags = append(diags, spellImplicitRoutes(c, u, node, common.Provenance.Pointer)...)
	deferUnionTags(c, ts, u, node, common.Provenance.Pointer)
	return node, diags
}

// keepBranchDiscriminatorKeys keeps on n, the Union of u, what the discriminator
// u's allOf entry writes says that reaches no field: its extensions and the
// keys OpenAPI defines for no Discriminator Object. A schema's own are read onto
// its position's node (annotation.Read), and an entry has no node, so they ride
// on the node holding the discriminator, scoped by their path from it where they
// lie beneath it (annotation.SchemaExtensions).
func keepBranchDiscriminatorKeys(c lowering.Ctx, n *ir.Union, u unionAt) []ir.Diagnostic {
	if !u.entry || u.disc == nil || u.discAt != u.at {
		return nil
	}
	owner, scope := discriminatorScope(u, n.Provenance.Pointer)
	// u.disc is the holder's, as discAt is at.
	kept, diags := annotation.SchemaExtensions(u.disc.Extensions, c.Place(), owner, scope)
	diags = append(diags, annotation.UnknownKeysNamed(&kept, u.disc.Unknown, u.disc.Node, c.Place(), owner, scope)...)
	n.Unmodeled = annotation.MergeUnmodeled(n.Unmodeled, kept)
	return diags
}

// discriminatorScope is where the discriminator standing with u is written,
// and the scope what a node at pointer keeps of it is keyed by: its path
// beneath pointer, or `discriminator` where it is not beneath it.
func discriminatorScope(u unionAt, pointer jsontext.Pointer) (jsontext.Pointer, string) {
	owner := u.discAt + ids.Ptr("discriminator")
	if rest, beneath := strings.CutPrefix(string(owner), string(pointer)+"/"); beneath {
		return owner, rest
	}
	return owner, "discriminator"
}

// unionAt is a union a position lowers: the oneOf or anyOf, whichever
// unionBranches elects, that holder writes at the pointer at, and the
// discriminator standing with it, written by the schema at discAt. holder is the
// schema written at the position, or an inline entry of its allOf, which JSON
// Schema conjoins with the rest of the composition as it would one written
// beside it (docs/compilers/openapi.md#union-beside-structure).
type unionAt struct {
	holder *statement.Schema
	at     jsontext.Pointer
	// schema is the schema written at the position, holder itself or the one
	// writing the allOf holding it: what each branch the union elects or passes
	// over conjoins with.
	schema *statement.Schema
	// disc is the discriminator the union is routed by, nil for none: one written
	// beside it, else one beside the allOf holding its entry (entryUnion).
	disc   *statement.Discriminator
	discAt jsontext.Pointer
	// entry marks a holder that is an allOf entry, no schema at a position.
	entry bool
	// nulls marks a union whose dead branches admit nothing but a null the
	// rest of the position keeps out (pairDeadNulls), beside any body.
	nulls bool
	// distributed marks a union a body is distributed across, each of whose
	// variants keeps the combinator it does not elect (buildComposedVariant).
	distributed bool
	// dead marks, by branch index, each branch of a distributed union no
	// instance of its body satisfies (deadBranches), which is no variant; nil
	// for a union distributing nothing.
	dead []bool
	// deadRoutes are the routes disc takes to a dead branch (deadRoutesOf),
	// which route to no variant.
	deadRoutes []deadRoute
}

// variantBranch reports whether st, the branch of u at index i, is a variant
// of the Union u lowers to: every branch but a null one, which lifts to the
// reference's Nullable, and one no instance of the body u is distributed over
// satisfies. Each reader aligning branches with variants asks it.
func (u unionAt) variantBranch(c lowering.Ctx, i int, st *statement.Statement) bool {
	return !nullBranch(c, st) && (u.dead == nil || !u.dead[i])
}

// besideUnion is the union s, written at pointer, writes itself, with the
// discriminator written beside it.
func besideUnion(s *statement.Schema, pointer jsontext.Pointer) unionAt {
	return unionAt{holder: s, at: pointer, schema: s, disc: s.Discriminator(), discAt: pointer}
}

// entryUnionOf keys what entryUnion read of one schema.
type entryUnionOf struct {
	schema *statement.Schema
}

// entryUnionIndex is where in a schema's flattened allOf the entry whose
// union it conjoins is, where one is.
type entryUnionIndex struct {
	index int
	ok    bool
}

// entryUnion returns the union an inline entry of s's allOf writes that s,
// written at pointer, conjoins with the rest of its composition, as it would
// the same union written beside the allOf
// (docs/compilers/openapi.md#union-beside-structure), with the discriminator
// written on the entry, else beside the allOf, and how the position lowers it:
// unionSharesNoKind where the unions its entries write share no kind with the
// rest (unionApart), else as classifyConjoined says. ok is false where the
// union stays with its entry, as for the base of a hierarchy the
// discriminator routes.
func entryUnion(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (unionAt, unionLowering, bool) {
	at := entryUnionIndexOf(c, s)
	if !at.ok {
		return unionAt{}, 0, false
	}
	comp := allOfEntries(c, s, pointer)
	e := comp.entries[at.index]
	u := unionAt{holder: e.schema.Schema, at: e.pointer, schema: s, disc: s.Discriminator(), discAt: pointer, entry: true}
	if d := u.holder.Discriminator(); d != nil {
		u.disc, u.discAt = d, e.pointer
	}
	if unionApart(c, s, comp, at.index) {
		return u, unionSharesNoKind, false
	}
	how := classifyConjoined(c, s, u, pointer)
	return u, how, how == unionDistributed || how == unionRoutedSubtype
}

// entryUnionIndexOf returns findEntryUnion's reading of s, read once a run, as
// every reader of a position s is written at, and of a hierarchy s is the base
// of, asks.
func entryUnionIndexOf(c lowering.Ctx, s *statement.Schema) entryUnionIndex {
	return lowering.Remembered(c, entryUnionOf{s}, func() entryUnionIndex { return findEntryUnion(c, s) })
}

// findEntryUnion is entryUnion's reading of s alone: where s lowers its
// allOf, composing, not refining, the entry whose union declares shape
// against s without any entry's union, within the flattening depth, each $ref
// branch naming a referent. Of several, the one writing a oneOf, each other
// kept on every variant; two writing a oneOf, or several anyOfs, leave none,
// in any order. One declaring no shape stays residue. A discriminator on both
// s and the entry leaves none: nothing says which routes it. A $ref site or a
// union beside the allOf lowers first.
func findEntryUnion(c lowering.Ctx, s *statement.Schema) entryUnionIndex {
	if declaresUnion(s) || dispatchOf(s).won != "allOf" {
		return entryUnionIndex{}
	}
	entries := allOfEntries(c, s, "").entries
	var shaped, oneOfs []int
	for i, e := range entries {
		choice := e.schema.ChoiceKeywords()
		// Judged against the rest of the conjunction, the entry's own unions
		// apart, as a union beside the allOf and the election within an entry are.
		if e.schema.Ref != nil || !declaresUnion(e.schema.Schema) ||
			!setsDeclareShape(c, s, e.schema.Schema, choice.OneOf, choice.AnyOf) {
			continue
		}
		shaped = append(shaped, i)
		if _, key, _ := unionBranches(c, s, e.schema.Schema); key == "oneOf" {
			oneOfs = append(oneOfs, i)
		}
	}
	if len(shaped) > 1 {
		shaped = oneOfs
	}
	if len(shaped) != 1 || entries[shaped[0]].capped {
		return entryUnionIndex{}
	}
	index := shaped[0]
	if entries[index].schema.Discriminator() != nil && s.Discriminator() != nil ||
		!branchesNameReferents(c, s, entries[index].schema.Schema) {
		return entryUnionIndex{}
	}
	if decisionOf(c, s).refines {
		return entryUnionIndex{}
	}
	return entryUnionIndex{index: index, ok: true}
}

// variantHint is the hint the union variant for the branch b written at vptr
// takes: the branch's own (lowering.NameAt), unless b is inline, no title
// names it (lowering.NameTitle), and it refines the $ref its conjuncts name,
// or composes one $ref declaring no member of its own (variantNamesTarget),
// when it is the name the $ref suggests (lowering.NameThrough). So a use of
// one target, `{$ref: X, description: …}`, `{allOf: [{$ref: X}], minLength:
// 1}` or an entry of either, names its variant as the bare $ref does, a title
// beside it too (GitHub #1018, #2000). The node the branch owns keeps the
// branch's own.
func variantHint(c lowering.Ctx, b *statement.Statement, vptr jsontext.Pointer) string {
	own := lowering.NameAt(c, vptr)
	if !stated(b.Schema) || b.Ref != nil {
		return own
	}
	if _, titled := lowering.NameTitle(c, vptr); titled {
		return own
	}
	if r, refines := refinementOf(c, b.Schema, vptr); refines && r.ref != nil {
		return cmp.Or(lowering.NameThrough(c, r.ref.pointer), own)
	}
	// The flattened list, as b lowers: a $ref a nested wrapper holds names the
	// variant as it would written at the top.
	entries := allOfEntries(c, b.Schema, vptr).entries
	base, ok := selectAllOfBase(c, entries, "")
	if !ok || !variantNamesTarget(b.Schema, entries) {
		return own
	}
	return cmp.Or(lowering.NameThrough(c, entries[base].pointer), own)
}

// variantNamesTarget reports whether s, a branch composing the base of
// entries, its flattened conjuncts, is a use of that one target: it and no
// conjunct writes a member, a catch-all, a discriminator or a union, and no
// $ref but the base's is written (docs/compilers/openapi.md#union).
func variantNamesTarget(s *statement.Schema, entries []allOfEntry) bool {
	refs := 0
	declares := func(o *statement.Schema) bool {
		object := o.ObjectKeywords()
		return len(object.Properties) > 0 || len(object.PatternProperties) > 0 || len(object.Required) > 0 ||
			written(object.AdditionalProperties) || o.Discriminator() != nil || declaresUnion(o)
	}
	if declares(s) {
		return false
	}
	for _, e := range entries {
		if e.schema.Ref != nil {
			refs++
			continue
		}
		if stated(e.schema.Schema) && declares(e.schema.Schema) {
			return false
		}
	}
	return refs == 1
}

// writesBothUnions reports whether s writes a oneOf and an anyOf.
func writesBothUnions(s *statement.Schema) bool {
	choice := s.ChoiceKeywords()
	return len(choice.OneOf) > 0 && len(choice.AnyOf) > 0
}

// declaresUnion reports whether s writes oneOf or anyOf at all.
func declaresUnion(s *statement.Schema) bool {
	choice := s.ChoiceKeywords()
	return len(choice.OneOf) > 0 || len(choice.AnyOf) > 0
}
