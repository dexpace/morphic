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
// has no X for that collapse to name, so it lowers as a schema of null alone
// does instead (lowerNullOnlyUnion); everything else becomes a Union with one
// Variant per non-null branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields. The Union holds the value bounds written beside
// it, as a branch's node, which every use of the branch shares, cannot
// (docs/compilers/openapi.md#constraints). fin finishes the node built.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, ok := nullUnionCollapse(c, s, pointer); ok {
		ref, diags := Ref(c, ts, depth, inner, ip)
		// The null branches say the null, so the reference admits it whatever
		// the branch it names says, and declares it where theirs or that
		// branch's own reference does (unionNullReading).
		declared := ref.Nullable && ref.Inferred == "" || valueNulls(c, s).union.declared == nullAdmitted
		return nullRef(c, ref.Target, declared, true), append(diags, liftedNullFindings(c, s, pointer)...)
	}
	if allNullUnion(c, s) {
		return lowerNullOnlyUnion(c, ts, s, pointer, fin)
	}
	owner, diags := keepingUnread(c, ts, s, pointer, dispatch{said: branchesSay(c, s)}, fin,
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			var diags []ir.Diagnostic
			tid := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
				def, unionDiags := buildUnion(c, ts, besideUnion(s, pointer), common,
					func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
						ref, refDiags := Ref(c, ts, depth, b, vptr)
						diags = append(diags, refDiags...)
						return ref
					})
				diags = append(diags, unionDiags...)
				cons, consDiags := ConstraintsOf(c, &def.Unmodeled, s, positionVoices(c, s, pointer, ir.TypeRef{}), pointer, boundKinds(c, s))
				def.Constraints = cons
				diags = append(diags, consDiags...)
				read := nodeBoundsRead(s, boundKinds(c, s), def.Constraints, def.Unmodeled)
				if def.Discriminator != nil {
					read |= keyBit("discriminator")
				}
				return def, read
			}, fin)
			return tid, 0, diags
		})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, nullDiags, liftedNullFindings(c, s, pointer))
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
	if choice := choiceIn(s); len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
		return nil, "", false
	}
	variants, key, _ := unionBranches(s)
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

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects is a null branch (nullBranch). nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without this check
// lowerOneOfAnyOf's buildUnion fallback strips every branch as a null marker
// and interns a Union with none left — the empty value space irverify's
// ir/union-no-variants rejects (GitHub #416). The branch set still admits
// exactly one value, the one a schema of null alone at this position admits,
// so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(c lowering.Ctx, s *statement.Schema) bool {
	variants, _, _ := unionBranches(s)
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
// (docs/compilers/openapi.md#union-null-only).
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	var branches finished
	owner, diags := keepingUnread(c, ts, s, pointer, dispatch{},
		branches.on(func(td ir.TypeDef) []ir.Diagnostic { return preserveNullOnlyUnion(c, td, s, pointer) }).then(fin),
		func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerNullAlone(c, ts, ownValueSet(s, pointer).finishedBy(fin)), 0, nil
		})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, branches.diags, nullDiags, liftedNullFindings(c, s, pointer))
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

// unionBranches returns the branches of whichever combinator the schema
// declares, the keyword's name (for pointers), and whether it is exclusive.
//
// oneOf wins when both are written, so every caller owns the set it passed over:
// buildUnion keeps it on the Union (preserveUnusedCombinator), nullUnionCollapse
// declines to collapse past it, and the verbatim lowering keeps both keywords
// (preserveUnionSiblingsAt). Reading the preference as one only that last lowering
// could reach is what dropped the anyOf in silence (GitHub #35).
func unionBranches(s *statement.Schema) ([]statement.Sub, string, bool) {
	choice := choiceIn(s)
	if len(choice.OneOf) > 0 {
		return choice.OneOf, "oneOf", true
	}
	return choice.AnyOf, "anyOf", false
}

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over
// verbatim on the Union the position lowers to, and reports it. A schema
// declaring only one combinator passes over nothing and is left alone.
//
// A schema writing both conjoins them, but one ir.Union carries one branch set,
// so the other stays recoverable beside the elected one
// (docs/compilers/openapi.md#competing-keywords), as validation logic where
// its branches declare no shape against s. Beside a body the union distributes
// across, each variant keeps it instead (buildComposedVariant).
func preserveUnusedCombinator(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, won string, pointer jsontext.Pointer) []ir.Diagnostic {
	choice := choiceIn(s)
	if len(choice.OneOf) == 0 || len(choice.AnyOf) == 0 {
		return nil
	}
	unused, set := otherCombinator[won], choice.OneOf
	if unused == "anyOf" {
		set = choice.AnyOf
	}
	if !setsDeclareShape(c, s, set) {
		// Branches narrowing only what s declares are validation logic, as the
		// same union on an entry of an allOf is (entryUnionReason).
		return annotation.PreserveValidationOnlyKeyword(p, s, unused, pointer, c.Place())
	}
	kept, diags := annotation.PreserveSchemaKeyword(p, s, unused, ir.ReasonDegradedLowering, pointer, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
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
	branches, key, exclusive := unionBranches(u.holder)
	var diags []ir.Diagnostic
	if !u.distributed {
		diags = preserveUnusedCombinator(c, &common.Unmodeled, u.holder, key, u.at)
	}
	diags = append(diags, recordNullBranches(c, &common.Unmodeled, nullBranchKeeps(c, u.holder, u.at))...)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		st := c.Statements().Sub(b)
		if !u.variantBranch(c, i, st) {
			continue // a null branch lifts to the enclosing ref's Nullable bit; a dead one is kept
		}
		vptr := u.at + ids.Ptr(key, strconv.Itoa(i))
		t := variantType(st, vptr)
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
// u's allOf branch writes says that reaches no field: its extensions and the
// keys OpenAPI defines for no Discriminator Object. A schema's own are read onto
// its position's node (annotation.Read), and a branch has no node, so they ride
// on the node holding the discriminator, scoped by their path from it where they
// lie beneath it (annotation.SchemaExtensions).
func keepBranchDiscriminatorKeys(c lowering.Ctx, n *ir.Union, u unionAt) []ir.Diagnostic {
	if !u.branch || u.disc == nil || u.discAt != u.at {
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
// schema written at the position, or an inline branch of its allOf, which JSON
// Schema conjoins with the rest of the composition as it would one written
// beside it (docs/compilers/openapi.md#union-beside-structure).
type unionAt struct {
	holder *statement.Schema
	at     jsontext.Pointer
	// disc is the discriminator the union is routed by, nil for none: one written
	// beside it, else one beside the allOf holding its branch (branchUnion).
	disc   *statement.Discriminator
	discAt jsontext.Pointer
	// branch marks a holder that is an allOf branch, no schema at a position.
	branch bool
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
	return unionAt{holder: s, at: pointer, disc: discriminatorIn(s), discAt: pointer}
}

// branchUnionOf keys what branchUnion read of one schema.
type branchUnionOf struct {
	schema *statement.Schema
}

// branchUnionIndex is where in a schema's flattened allOf the branch whose
// union it conjoins is, where one is.
type branchUnionIndex struct {
	index int
	ok    bool
}

// branchUnion returns the union an inline branch of s's allOf writes that s,
// written at pointer, conjoins with the rest of its composition, as it would
// the same union written beside the allOf
// (docs/compilers/openapi.md#union-beside-structure), with the discriminator
// written on the branch, else beside the allOf, and how the position lowers it
// (classifyConjoined): ok is false where s is the base of the hierarchy the
// discriminator routes, which leaves the union with its branch.
func branchUnion(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (unionAt, unionLowering, bool) {
	at := branchUnionIndexOf(c, s)
	if !at.ok {
		return unionAt{}, 0, false
	}
	e := allOfBranches(c, s, pointer).branches[at.index]
	u := unionAt{holder: e.schema.Schema, at: e.pointer, disc: discriminatorIn(s), discAt: pointer, branch: true}
	if d := discriminatorIn(u.holder); d != nil {
		u.disc, u.discAt = d, e.pointer
	}
	how := classifyConjoined(c, s, u, pointer)
	return u, how, how == unionDistributed || how == unionRoutedSubtype
}

// branchUnionIndexOf returns findBranchUnion's reading of s, read once a run, as
// every reader of a position s is written at, and of a hierarchy s is the base
// of, asks.
func branchUnionIndexOf(c lowering.Ctx, s *statement.Schema) branchUnionIndex {
	return lowering.Remembered(c, branchUnionOf{s}, func() branchUnionIndex { return findBranchUnion(c, s) })
}

// findBranchUnion is branchUnion's reading of s alone: where s lowers its
// allOf, composing, not refining, the branch whose union declares shape
// against s, where it lies within the flattening depth and classifyBranchSet
// distributes it. Of several, the one writing a oneOf, as a schema writing
// both lowers to its oneOf, each other kept on every variant; two writing a
// oneOf, or several anyOfs, leave none, in any order. One declaring no shape
// stays residue. A discriminator on both s and the branch leaves none: nothing
// says which routes it. A $ref site or a union beside the allOf lowers first.
func findBranchUnion(c lowering.Ctx, s *statement.Schema) branchUnionIndex {
	if declaresUnion(s) || dispatchOf(s).won != "allOf" {
		return branchUnionIndex{}
	}
	branches := allOfBranches(c, s, "").branches
	var shaped, oneOfs []int
	for i, e := range branches {
		choice := choiceIn(e.schema.Schema)
		if e.schema.Ref != nil || !declaresUnion(e.schema.Schema) || !setsDeclareShape(c, s, choice.OneOf, choice.AnyOf) {
			continue
		}
		shaped = append(shaped, i)
		if len(choice.OneOf) > 0 {
			oneOfs = append(oneOfs, i)
		}
	}
	if len(shaped) > 1 {
		shaped = oneOfs
	}
	if len(shaped) != 1 || branches[shaped[0]].capped {
		return branchUnionIndex{}
	}
	index := shaped[0]
	if discriminatorIn(branches[index].schema.Schema) != nil && discriminatorIn(s) != nil ||
		classifyBranchSet(c, branches[index].schema.Schema) != unionDistributed {
		return branchUnionIndex{}
	}
	if decisionOf(c, s).refines {
		return branchUnionIndex{}
	}
	return branchUnionIndex{index: index, ok: true}
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
	branches := allOfBranches(c, b.Schema, vptr).branches
	base, ok := selectAllOfBase(c, branches, "")
	if !ok || !variantNamesTarget(b.Schema, branches) {
		return own
	}
	return cmp.Or(lowering.NameThrough(c, branches[base].pointer), own)
}

// variantNamesTarget reports whether s, a branch composing the base of
// branches, its flattened conjuncts, is a use of that one target: it and no
// conjunct writes a member, a catch-all, a discriminator or a union, and no
// $ref but the base's is written (docs/compilers/openapi.md#union).
func variantNamesTarget(s *statement.Schema, branches []allOfBranch) bool {
	refs := 0
	declares := func(o *statement.Schema) bool {
		object := objectIn(o)
		return len(object.Properties) > 0 || len(object.PatternProperties) > 0 || len(object.Required) > 0 ||
			written(object.AdditionalProperties) || discriminatorIn(o) != nil || declaresUnion(o)
	}
	if declares(s) {
		return false
	}
	for _, e := range branches {
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

// declaresUnion reports whether s writes oneOf or anyOf at all.
func declaresUnion(s *statement.Schema) bool {
	choice := choiceIn(s)
	return len(choice.OneOf) > 0 || len(choice.AnyOf) > 0
}

// choiceIn returns what s's oneOf and anyOf list, the zero Choice where it
// writes neither.
func choiceIn(s *statement.Schema) statement.Choice {
	if s == nil || s.Choice == nil {
		return statement.Choice{}
	}
	return *s.Choice
}
