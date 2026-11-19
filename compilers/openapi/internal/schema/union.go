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
func lowerOneOfAnyOf(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	at := st.position(schema)
	if inner, ip, ok := nullUnionCollapse(loweringContext, schema, pointer); ok && !pairedPrincipal(loweringContext, st) {
		ref, diags := Ref(loweringContext, ts, depth, inner, ip)
		// The null branches say the null, so the reference admits it whatever
		// the branch it names says, and declares it where theirs or that
		// branch's own reference does (unionNullReading).
		declared := ref.Nullable && ref.Inferred == "" || valueNulls(loweringContext, schema).union.declared == nullAdmitted
		return nullRef(loweringContext, ref.Target, declared, true), append(diags, liftedNullFindings(loweringContext, at, schema, pointer)...)
	}
	if allNullUnion(loweringContext, at, schema) {
		ref, diags := lowerNullOnlyUnion(loweringContext, ts, schema, pointer, fin, st)
		if st.of == nil {
			// The union of null alone is a conjunct like any other, so a pair
			// sharing no value is reported here as where it is not null alone.
			diags = append(diags, diagUnionsShareNothing(loweringContext, schema, pointer, unionShape)...)
		}
		return ref, diags
	}
	owner, diags := keepingUnread(loweringContext, ts, schema, pointer, dispatch{said: branchesSay(loweringContext, schema, schema)}, fin, st,
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			var diags []ir.Diagnostic
			tid := internNode(loweringContext, ts, st.pointer(pointer), func(common ir.TypeCommon) (ir.TypeDef, keySet) {
				unionAt := besideUnion(schema, pointer)
				unionAt.schema, unionAt.dead = at, pairDeadNulls(loweringContext, at, schema, st)
				def, unionDiags := buildUnion(loweringContext, ts, unionAt, common,
					func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
						ref, refDiags := Ref(loweringContext, ts, depth, b, vptr)
						diags = append(diags, refDiags...)
						return ref
					})
				diags = append(diags, unionDiags...)
				if unionAt.dead != nil {
					// The report names an entry's union by the entry it is written on,
					// and a dead branch by the null the rest keeps out, not by a body.
					reported := unionAt
					reported.entry, reported.nulls = st.of != nil, true
					diags = append(diags, keepDeadBranches(loweringContext, &def.Unmodeled, at, reported, st.pointer(pointer))...)
				}
				cons, consDiags := ConstraintsOf(loweringContext, &def.Unmodeled, schema, st.foldedEntries(loweringContext, schema, pointer), pointer, st.takes(loweringContext, schema))
				def.Constraints = cons
				diags = append(diags, consDiags...)
				read := nodeBoundsRead(schema, st.takes(loweringContext, schema), def.Constraints, def.Unmodeled)
				if def.Discriminator != nil {
					read |= keyBit("discriminator")
				}
				return def, read
			}, fin)
			return tid, 0, diags
		})
	ref, nullDiags := schemaNullRef(loweringContext, owner, schema, siteAt(pointer))
	if st.of == nil {
		// A principal's conjunction is reported at the schema writing it (refinedDisjoint).
		diags = append(diags, diagUnionsShareNothing(loweringContext, schema, pointer, unionShape)...)
	}
	return ref, slices.Concat(diags, nullDiags, liftedNullFindings(loweringContext, at, schema, pointer))
}

// pairDeadNulls marks, by index, each branch of the union schema elects at
// position that is a variant admitting null alone, as a $ref to a schema of
// null alone is, where position elects one union of a oneOf and an anyOf and
// what it writes beside that union, the one passed over among it, keeps null
// out (restForbidsNull): no instance is one of both. It is nil where none is,
// where no variant would live, the pair then sharing no value, which its own
// warning says, or where schema elects no union of a pair
// (docs/compilers/openapi.md#union-beside-structure).
func pairDeadNulls(loweringContext lowering.Ctx, position, schema *statement.Schema, st stand) []bool {
	if !writesBothUnions(schema) && !pairedPrincipal(loweringContext, st) {
		return nil
	}
	branches, _, _ := unionBranches(loweringContext, position, schema)
	var dead []bool
	live := false
	for i, b := range branches {
		bs := loweringContext.Statements().Subschema(b)
		if nullBranch(loweringContext, bs) {
			continue
		}
		if kinds, inferred := lowersTo(loweringContext, bs); kinds&^kindNull != 0 || inferred {
			live = true
			continue
		}
		if dead == nil {
			if !restForbidsNull(loweringContext, position, schema) {
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
func nullUnionCollapse(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (*statement.Statement, jsontext.Pointer, bool) {
	if choice := schema.ChoiceKeywords(); len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
		return nil, "", false
	}
	variants, key, _ := unionBranches(loweringContext, schema, schema)
	var nonNull *statement.Statement
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, subschema := range variants {
		st := loweringContext.Statements().Subschema(subschema)
		if nullBranch(loweringContext, st) {
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
func pairedPrincipal(loweringContext lowering.Ctx, st stand) bool {
	return st.of != nil && electsOneOfPair(loweringContext, st.of)
}

// electsOneOfPair reports whether schema refines to an inline entry writing one
// union of a oneOf and an anyOf that schema and its entries write between them
// (writesUnionPair), a body beside them or none, the other passed over.
func electsOneOfPair(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	decision := decisionOf(loweringContext, schema)
	if !decision.refines || decision.principalAt < 0 {
		return false
	}
	schemaStatement := entryStatement(loweringContext, schema, decision.principalAt)
	if schemaStatement.Ref != nil || !declaresUnion(schemaStatement.Schema) || writesBothUnions(schemaStatement.Schema) {
		return false
	}
	return writesUnionPair(schema, allOfEntries(loweringContext, schema, ""))
}

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects of schema at position is a null branch (nullBranch).
// nullUnionCollapse has no non-null branch to collapse a set like this onto, so
// without this check lowerOneOfAnyOf's buildUnion fallback strips every branch
// as a null marker and interns a Union with none left — the empty value space
// irverify's ir/union-no-variants rejects (GitHub #416). The branch set still
// admits exactly one value, the one a schema of null alone at this position
// admits, so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(loweringContext lowering.Ctx, position, schema *statement.Schema) bool {
	variants, _, _ := unionBranches(loweringContext, position, schema)
	return len(variants) > 0 && !slices.ContainsFunc(variants, func(subschema statement.Subschema) bool {
		return !nullBranch(loweringContext, loweringContext.Statements().Subschema(subschema))
	})
}

// lowerNullOnlyUnion lowers a oneOf/anyOf every one of whose branches admits
// null alone (allNullUnion). One such branch or several admit what a schema of
// null alone at this position admits, so the position lowers as one does
// (lowerNullAlone), and a body's lowering keeps unread what the node
// cannot hold. The combinator that produced it, and the other one too when both
// are declared, carries no shape the node's fields could hold, so it is kept
// verbatim under Unmodeled instead of dropped
// (docs/compilers/openapi.md#union-null-only). The node stands where st says.
func lowerNullOnlyUnion(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, pointer jsontext.Pointer, fin finisher,
	st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	var branches finished
	owner, diags := keepingUnread(loweringContext, ts, schema, pointer, dispatch{},
		branches.on(func(td ir.TypeDef) []ir.Diagnostic {
			return preserveNullOnlyUnion(loweringContext, td, schema, pointer)
		}).then(fin), st,
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerNullAlone(loweringContext, ts, standingValueSet(schema, pointer, st).finishedBy(fin)), 0, nil
		})
	ref, nullDiags := schemaNullRef(loweringContext, owner, schema, siteAt(pointer))
	return ref, slices.Concat(diags, branches.diags, nullDiags, liftedNullFindings(loweringContext, st.position(schema), schema, pointer))
}

// preserveNullOnlyUnion keeps schema's oneOf/anyOf verbatim under the Unmodeled
// of td, in hand, the node lowerNullOnlyUnion built, an Enum with no member,
// which holds no branch set of its own, and reports what it kept.
func preserveNullOnlyUnion(loweringContext lowering.Ctx, td ir.TypeDef, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := preserveBranchSets(loweringContext, &td.Common().Unmodeled, schema, ir.ReasonDegradedLowering, pointer)
	if len(kept) == 0 {
		return diags
	}
	// Both keywords land here when both are declared: buildUnion, and its
	// preserveUnusedCombinator, are never reached from this arm, so this is the
	// only place either combinator gets a home.
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"every branch admits only the null value, so this position lowered as an enum with no member "+
			"behind a nullable reference; %s kept verbatim under Unmodeled", strings.Join(kept, " and ")))
}

// preserveBranchSets stores schema's declared oneOf/anyOf verbatim under
// unmodeled, in keyword order, reporting one it cannot convert.
// ir.ReasonValidationOnly routes each through the validation-only
// keyword-family reporting (annotation.PreserveValidationOnlyKeyword) and
// returns no keywords. Every other reason keeps each plainly
// (annotation.PreserveSchemaKeyword) and returns the keywords kept, so a caller
// with a message of its own knows what to name.
//
// preserveUnionSiblingsAt and preserveNullOnlyUnion share this loop: they keep
// the same keywords because the node they attach to has no branch set of its
// own, and differ only in the sentence that explains why.
func preserveBranchSets(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, reason ir.UnmodeledReason, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var kept []string
	for _, kw := range []string{"oneOf", "anyOf"} {
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, annotation.PreserveValidationOnlyKeyword(unmodeled, schema, kw, pointer, loweringContext.Place())...)
			continue
		}
		ok, keptDiags := annotation.PreserveSchemaKeyword(unmodeled, schema, kw, reason, pointer, loweringContext.Place())
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
func unionBranches(loweringContext lowering.Ctx, position, holder *statement.Schema) ([]statement.Subschema, string, bool) {
	choice := holder.ChoiceKeywords()
	if len(choice.OneOf) > 0 && (len(choice.AnyOf) == 0 || !anyOfElected(loweringContext, position, holder)) {
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
func anyOfElected(loweringContext lowering.Ctx, position, holder *statement.Schema) bool {
	return lowering.ReadOncePerRun(loweringContext, unionElectionOf{position, holder}, func() bool {
		choice := holder.ChoiceKeywords()
		return !setsDeclareShape(loweringContext, position, holder, choice.OneOf) && setsDeclareShape(loweringContext, position, holder, choice.AnyOf)
	})
}

// passedOver returns the branch set unionBranches passes over in holder at
// position, a conjunct of the rest of that position, and its keyword: none
// where holder writes one union.
func passedOver(loweringContext lowering.Ctx, position, holder *statement.Schema) ([]statement.Subschema, string) {
	choice := holder.ChoiceKeywords()
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil, ""
	}
	if _, key, _ := unionBranches(loweringContext, position, holder); key == "oneOf" {
		return choice.AnyOf, "anyOf"
	}
	return choice.OneOf, "oneOf"
}

// branches is unionBranches for u's holder at its position.
func (u unionAt) branches(loweringContext lowering.Ctx) ([]statement.Subschema, string, bool) {
	return unionBranches(loweringContext, u.schema, u.holder)
}

// restKeepsNull returns whether what unionAtValue's position writes but
// unionAtValue's own union keeps null out (restForbidsNull), read on first
// asking: most unions hold no variant admitting null.
func restKeepsNull(loweringContext lowering.Ctx, unionAtValue unionAt) func() bool {
	var read, forbids bool
	return func() bool {
		if !read {
			read, forbids = true, restForbidsNull(loweringContext, unionAtValue.schema, unionAtValue.holder)
		}
		return forbids
	}
}

// restForbidsNull reports whether body keeps null out by what it writes but
// the union of holder, the union holder passes over among it, as
// bodyAdmitsNull reads that rest: a rest saying nothing of null keeps none
// out, and a passed-over union read past the budget decides nothing.
func restForbidsNull(loweringContext lowering.Ctx, body, holder *statement.Schema) bool {
	nulls := newNulls(loweringContext)
	nulls.variants, nulls.rest = true, holder
	if nulls.conjunct(loweringContext.Statements().Read(body), nullSite{}).forbids(loweringContext) {
		return true
	}
	passed, _ := passedOver(loweringContext, body, holder)
	if passed == nil {
		return false
	}
	nullsValue := newNulls(loweringContext)
	nullsValue.variants = true
	return nullsValue.branchesNull(passed).forbids(loweringContext) && !nullsValue.conjuncts.Exhausted()
}

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over in
// unionAtValue's holder verbatim on the Union the position lowers to, and
// reports it. A holder declaring only one combinator passes over nothing and is
// left alone.
//
// A schema writing both conjoins them, but one ir.Union carries one branch set,
// so the other stays recoverable beside the elected one
// (docs/compilers/openapi.md#competing-keywords), as validation logic where
// its branches declare no shape against the schema at the position, an entry
// holding both as much as one written beside the allOf. Beside a body the union
// distributes across, each variant keeps it instead (buildComposedVariant).
func preserveUnusedCombinator(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, unionAtValue unionAt, won string) []ir.Diagnostic {
	choice := unionAtValue.holder.ChoiceKeywords()
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil
	}
	unused, set := otherCombinator[won], choice.OneOf
	if unused == "anyOf" {
		set = choice.AnyOf
	}
	if !setsDeclareShape(loweringContext, unionAtValue.schema, unionAtValue.holder, set) {
		// Branches narrowing only what the position declares are validation
		// logic, as the same union on an entry of its own is (entryUnionReason).
		return annotation.PreserveValidationOnlyKeyword(unmodeled, unionAtValue.holder, unused, unionAtValue.at, loweringContext.Place())
	}
	kept, diags := annotation.PreserveSchemaKeyword(unmodeled, unionAtValue.holder, unused, ir.ReasonDegradedLowering, unionAtValue.at, loweringContext.Place())
	if !kept {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, unionAtValue.at,
		"oneOf and anyOf are both declared here and conjoin, and one Union carries one "+
			"branch set; this position lowered as its %s, with %s kept verbatim under Unmodeled",
		won, unused))
}

// variantTypeFunc lowers one union branch to the type its variant refers to. It
// is a parameter so the distributed lowering can conjoin the sibling composition
// into each branch while sharing every other rule about union shape — null-branch
// stripping, variant hints, exclusivity, pointer derivation.
type variantTypeFunc func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef

// buildUnion assembles the Union node for the union unionAtValue, attaching the
// discriminator standing with it, where one does, with the routes it implies
// (spellImplicitRoutes). common is already built by the caller (internNode), so
// buildUnion needs no hint of its own to build one, and it places the union.
//
// The variants are lowered before the discriminator, so its mapping routes to
// them (indexBranches). A union the discriminator routes is WireTagged, an
// anyOf as much as a oneOf: the tag property's value names its variant
// (docs/ir/unions.md).
func buildUnion(loweringContext lowering.Ctx, ts *compile.Types, unionAtValue unionAt, common ir.TypeCommon, variantType variantTypeFunc,
) (*ir.Union, []ir.Diagnostic) {
	branches, key, exclusive := unionAtValue.branches(loweringContext)
	var diags []ir.Diagnostic
	if !unionAtValue.distributed {
		diags = preserveUnusedCombinator(loweringContext, &common.Unmodeled, unionAtValue, key)
	}
	diags = append(diags, recordNullBranches(loweringContext, &common.Unmodeled, nullBranchKeeps(loweringContext, unionAtValue.schema, unionAtValue.holder, unionAtValue.at))...)
	variants := make([]ir.Variant, 0, len(branches))
	keepsNull := restKeepsNull(loweringContext, unionAtValue)
	for i, b := range branches {
		st := loweringContext.Statements().Subschema(b)
		if !unionAtValue.variantBranch(loweringContext, i, st) {
			continue // a null branch lifts to the enclosing ref's Nullable bit; a dead one is kept
		}
		vptr := unionAtValue.at + ids.Ptr(key, strconv.Itoa(i))
		typeRef := variantType(st, vptr)
		if typeRef.Nullable && keepsNull() {
			// A null is shared only where every conjunct may admit it, so a
			// rest keeping it out leaves the variant none, its other values
			// living (docs/compilers/openapi.md#union-null-variant).
			typeRef.Nullable, typeRef.Inferred = false, ""
		}
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(variantHint(loweringContext, st, vptr)),
			Type: typeRef,
		})
	}
	node := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		Closed:     true,
	}
	var branchIndexValue *branchIndex
	if unionAtValue.disc != nil {
		branchIndexValue = indexBranches(loweringContext, unionAtValue, variants)
	}
	disc, discDiags := lowerDiscriminator(loweringContext, ts, unionAtValue.disc, unionAtValue.discAt, nil, branchIndexValue)
	node.Discriminator = disc
	node.WireTagged = disc != nil
	diags = append(diags, discDiags...)
	diags = append(diags, keepEntryDiscriminatorKeys(loweringContext, node, unionAtValue)...)
	diags = append(diags, keepDeadRoutes(loweringContext, node, unionAtValue)...)
	diags = append(diags, spellImplicitRoutes(loweringContext, unionAtValue, node, common.Provenance.Pointer)...)
	deferUnionTags(loweringContext, ts, unionAtValue, node, common.Provenance.Pointer)
	return node, diags
}

// keepEntryDiscriminatorKeys keeps on n, the Union of u, what the discriminator
// u's allOf entry writes says that reaches no field: its extensions and the
// keys OpenAPI defines for no Discriminator Object. A schema's own are read onto
// its position's node (annotation.Read), and an entry has no node, so they ride
// on the node holding the discriminator, scoped by their path from it where they
// lie beneath it (annotation.SchemaExtensions).
func keepEntryDiscriminatorKeys(loweringContext lowering.Ctx, union *ir.Union, unionAtValue unionAt) []ir.Diagnostic {
	if !unionAtValue.entry || unionAtValue.disc == nil || unionAtValue.discAt != unionAtValue.at {
		return nil
	}
	owner, scope := discriminatorScope(unionAtValue, union.Provenance.Pointer)
	// unionAtValue.disc is the holder's, as discAt is at.
	kept, diags := annotation.SchemaExtensions(unionAtValue.disc.Extensions, loweringContext.Place(), owner, scope)
	diags = append(diags, annotation.UnknownKeysNamed(&kept, unionAtValue.disc.Unknown, unionAtValue.disc.Node, loweringContext.Place(), owner, scope)...)
	union.Unmodeled = annotation.MergeUnmodeled(union.Unmodeled, kept)
	return diags
}

// discriminatorScope is where the discriminator standing with unionAtValue is
// written, and the scope what a node at pointer keeps of it is keyed by: its
// path beneath pointer, or `discriminator` where it is not beneath it.
func discriminatorScope(unionAtValue unionAt, pointer jsontext.Pointer) (jsontext.Pointer, string) {
	owner := unionAtValue.discAt + ids.Ptr("discriminator")
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
func (u unionAt) variantBranch(loweringContext lowering.Ctx, i int, st *statement.Statement) bool {
	return !nullBranch(loweringContext, st) && (u.dead == nil || !u.dead[i])
}

// besideUnion is the union schema, written at pointer, writes itself, with the
// discriminator written beside it.
func besideUnion(schema *statement.Schema, pointer jsontext.Pointer) unionAt {
	return unionAt{holder: schema, at: pointer, schema: schema, disc: schema.Discriminator(), discAt: pointer}
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
func entryUnion(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (unionAt, unionLowering, bool) {
	at := entryUnionIndexOf(loweringContext, schema)
	if !at.ok {
		return unionAt{}, 0, false
	}
	comp := allOfEntries(loweringContext, schema, pointer)
	entry := comp.entries[at.index]
	unionAtValue := unionAt{holder: entry.schema.Schema, at: entry.pointer, schema: schema, disc: schema.Discriminator(), discAt: pointer, entry: true}
	if discriminator := unionAtValue.holder.Discriminator(); discriminator != nil {
		unionAtValue.disc, unionAtValue.discAt = discriminator, entry.pointer
	}
	if unionApart(loweringContext, schema, comp, at.index) {
		return unionAtValue, unionSharesNoKind, false
	}
	how := classifyConjoined(loweringContext, schema, unionAtValue, pointer)
	return unionAtValue, how, how == unionDistributed || how == unionRoutedSubtype
}

// entryUnionIndexOf returns findEntryUnion's reading of s, read once a run, as
// every reader of a position s is written at, and of a hierarchy s is the base
// of, asks.
func entryUnionIndexOf(loweringContext lowering.Ctx, schema *statement.Schema) entryUnionIndex {
	return lowering.ReadOncePerRun(loweringContext, entryUnionOf{schema}, func() entryUnionIndex { return findEntryUnion(loweringContext, schema) })
}

// findEntryUnion is entryUnion's reading of s alone: where s lowers its
// allOf, composing, not refining, the entry whose union declares shape
// against s without any entry's union, within the flattening depth, each $ref
// branch naming a referent. Of several, the one writing a oneOf, each other
// kept on every variant; two writing a oneOf, or several anyOfs, leave none,
// in any order. One declaring no shape stays residue. A discriminator on both
// s and the entry leaves none: nothing says which routes it. A $ref site or a
// union beside the allOf lowers first.
func findEntryUnion(loweringContext lowering.Ctx, schema *statement.Schema) entryUnionIndex {
	if declaresUnion(schema) || dispatchOf(schema).won != "allOf" {
		return entryUnionIndex{}
	}
	entries := allOfEntries(loweringContext, schema, "").entries
	var shaped, oneOfs []int
	for i, entry := range entries {
		choice := entry.schema.ChoiceKeywords()
		// Judged against the rest of the conjunction, the entry's own unions
		// apart, as a union beside the allOf and the election within an entry are.
		if entry.schema.Ref != nil || !declaresUnion(entry.schema.Schema) ||
			!setsDeclareShape(loweringContext, schema, entry.schema.Schema, choice.OneOf, choice.AnyOf) {
			continue
		}
		shaped = append(shaped, i)
		if _, key, _ := unionBranches(loweringContext, schema, entry.schema.Schema); key == "oneOf" {
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
	if entries[index].schema.Discriminator() != nil && schema.Discriminator() != nil ||
		!branchesNameReferents(loweringContext, schema, entries[index].schema.Schema) {
		return entryUnionIndex{}
	}
	if decisionOf(loweringContext, schema).refines {
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
func variantHint(loweringContext lowering.Ctx, b *statement.Statement, vptr jsontext.Pointer) string {
	own := lowering.NameAt(loweringContext, vptr)
	if !stated(b.Schema) || b.Ref != nil {
		return own
	}
	if _, titled := lowering.NameTitle(loweringContext, vptr); titled {
		return own
	}
	if refinement, refines := refinementOf(loweringContext, b.Schema, vptr); refines && refinement.ref != nil {
		return cmp.Or(lowering.NameThrough(loweringContext, refinement.ref.pointer), own)
	}
	// The flattened list, as b lowers: a $ref a nested wrapper holds names the
	// variant as it would written at the top.
	entries := allOfEntries(loweringContext, b.Schema, vptr).entries
	base, ok := selectAllOfBase(loweringContext, entries, "")
	if !ok || !variantNamesTarget(b.Schema, entries) {
		return own
	}
	return cmp.Or(lowering.NameThrough(loweringContext, entries[base].pointer), own)
}

// variantNamesTarget reports whether s, a branch composing the base of
// entries, its flattened conjuncts, is a use of that one target: it and no
// conjunct writes a member, a catch-all, a discriminator or a union, and no
// $ref but the base's is written (docs/compilers/openapi.md#union).
func variantNamesTarget(schema *statement.Schema, entries []allOfEntry) bool {
	refs := 0
	declares := func(statementSchema *statement.Schema) bool {
		object := statementSchema.ObjectKeywords()
		return len(object.Properties) > 0 || len(object.PatternProperties) > 0 || len(object.Required) > 0 ||
			written(object.AdditionalProperties) || statementSchema.Discriminator() != nil || declaresUnion(statementSchema)
	}
	if declares(schema) {
		return false
	}
	for _, entry := range entries {
		if entry.schema.Ref != nil {
			refs++
			continue
		}
		if stated(entry.schema.Schema) && declares(entry.schema.Schema) {
			return false
		}
	}
	return refs == 1
}

// writesBothUnions reports whether schema writes a oneOf and an anyOf.
func writesBothUnions(schema *statement.Schema) bool {
	choice := schema.ChoiceKeywords()
	return len(choice.OneOf) > 0 && len(choice.AnyOf) > 0
}

// declaresUnion reports whether schema writes oneOf or anyOf at all.
func declaresUnion(schema *statement.Schema) bool {
	choice := schema.ChoiceKeywords()
	return len(choice.OneOf) > 0 || len(choice.AnyOf) > 0
}
