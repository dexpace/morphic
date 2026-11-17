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
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
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
// (docs/compilers/openapi.md#constraints).
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, ok := nullUnionCollapse(c, s, pointer); ok {
		ref, diags := Ref(c, ts, depth, inner, ip)
		// The null branches say the null, so the reference admits it whatever
		// the branch it names says, and declares it where theirs or that
		// branch's own reference does (unionNullReading).
		declared := ref.Nullable && ref.Inferred == "" || valueNulls(c, s).union.declared == nullAdmitted
		return nullRef(c, ref.Target, declared, true), append(diags, liftedNullFindings(c, s, pointer)...)
	}
	if allNullUnion(c, s) {
		return lowerNullOnlyUnion(c, ts, s, pointer)
	}
	var diags []ir.Diagnostic
	tid := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
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
		return def
	})
	owner, unhomedDiags := preserveUnhomedKeywords(c, ts, s, pointer, pointer, tid, dispatch{said: branchesSay(c, s)})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, unhomedDiags, nullDiags, liftedNullFindings(c, s, pointer))
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
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	owner, diags := preserveUnhomedKeywords(c, ts, s, pointer, pointer, lowerNullAlone(c, ts, ownValueSet(s, pointer)), dispatch{})
	diags = append(diags, preserveNullOnlyUnion(c, ts, owner, s, pointer)...)
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, nullDiags, liftedNullFindings(c, s, pointer))
}

// preserveNullOnlyUnion keeps s's oneOf/anyOf verbatim under the Unmodeled of
// the node lowerNullOnlyUnion built at id, an Enum with no member, which holds
// no branch set of its own, and reports what it kept.
func preserveNullOnlyUnion(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	kept, keepDiags := preserveBranchSets(c, &td.Common().Unmodeled, s, ir.ReasonDegradedLowering, pointer)
	diags = append(diags, keepDiags...)
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
// (preserveUnionSiblings). Reading the preference as one only that last lowering
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
	owner := u.at + ids.Ptr("discriminator")
	scope := "discriminator"
	if rest, beneath := strings.CutPrefix(string(owner), string(n.Provenance.Pointer)+"/"); beneath {
		scope = rest
	}
	// u.disc is the holder's, as discAt is at.
	kept, diags := annotation.SchemaExtensions(u.disc.Extensions, c.Place(), owner, scope)
	diags = append(diags, annotation.UnknownKeysNamed(&kept, u.disc.Unknown, u.disc.Node, c.Place(), owner, scope)...)
	n.Unmodeled = annotation.MergeUnmodeled(n.Unmodeled, kept)
	return diags
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
	if _, refines := refinementOf(c, s, ""); refines {
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

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// untagged union with one variant per declared type, exclusive unless two of
// them overlap (typesDisjoint). A type array asks for one of its types, not for
// exactly one, so overlapping variants must not exclude each other; with an
// integer width, integer and number name the same primitive, and the two
// variants stay, for an emitter to keep apart (docs/emitters/targets/go/unions.md).
// Each variant reads what the position's folding allOf branches write, at the
// pointers they are written at.
func lowerUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, types []statement.Kind) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		list := typeArray{types: types, voices: positionVoices(c, s, pointer, ir.TypeRef{})}
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, vDiags := lowerTypeVariant(c, ts, depth, s, vptr, st, list)
			diags = append(diags, vDiags...)
			variants = append(variants, ir.Variant{
				Name: compile.NamingHint(string(st)),
				Type: ir.TypeRef{Target: vid},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  typesDisjoint(types),
			Closed:     true,
		}
	})
	return id, diags
}

// typeArray is what every variant of one type array reads beside its schema: the
// types it lists, and the voices of the position it is written at.
type typeArray struct {
	types  []statement.Kind
	voices []annotation.Voice
}

// lowerTypeVariant lowers the variant of s's type array that st names, at
// pointer, as lowerTyped lowers st alone, but for what applies to another listed
// type (docs/compilers/openapi.md#type-arrays). An object variant holds no
// discriminator, as no $ref or mapping can name its position: the census keeps
// it on the union. A variant reads the value bounds of the kinds of st the
// position admits (boundKinds), as the census does, so a bound is held or
// kept, never both. A scalar one, holding them, the format or the content
// vocabulary, is a Scalar of its own, else the shared primitive.
func lowerTypeVariant(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, st statement.Kind, list typeArray) (ir.TypeID, []ir.Diagnostic) {
	takes := typeKinds(st) & boundKinds(c, s)
	switch st {
	case statement.KindObject:
		return lowerModel(c, ts, depth, s, pointer, false /* discriminated */, takes)
	case statement.KindArray:
		return lowerArray(c, ts, depth, s, pointer)
	default:
		shape := scalarShapeOf(s, st, variantFormat(s, st, list.types))
		if shape.shared && !statesBounds(c, s, list.voices, pointer, takes) {
			return ts.PrimID(shape.base), nil
		}
		return hoistScalar(c, ts, depth, s, shape, takes, list.voices, pointer)
	}
}

// variantFormat returns the format s writes, for its type array's variant of
// type st among types: the format where it pairs with st, or with no listed
// type, since nothing then says which type it describes, and a format validates
// only the instances of the types it is defined for (JSON Schema Validation
// 2020-12 §7.1); none where it pairs with another listed type.
func variantFormat(s *statement.Schema, st statement.Kind, types []statement.Kind) string {
	format := writtenFormat(s)
	if format == "" || pairs(st, format) {
		return format
	}
	if slices.ContainsFunc(types, func(t statement.Kind) bool { return pairs(t, format) }) {
		return ""
	}
	return format
}

// pairs reports whether format pairs with st: spelling.Lookup lists the
// pairing, or spelling.LookupEncoded does, as it does byte beside a string.
func pairs(st statement.Kind, format string) bool {
	key := string(st) + "/" + format
	_, listed := spelling.Lookup(key)
	_, encoded := spelling.LookupEncoded(key)
	return listed || encoded
}

// statesBounds reports whether s or one of voices, read at pointer, writes a
// value bound a reader of the kinds takes holds: one ConstraintsOf reads into a
// field or keeps, as an empty pattern or a keyword written null is not. It reads
// apart from the node, so what it reads is reported where hoistScalar reads it.
func statesBounds(c lowering.Ctx, s *statement.Schema, voices []annotation.Voice, pointer jsontext.Pointer, takes kindSet) bool {
	var kept ir.Unmodeled
	cons, _ := ConstraintsOf(c, &kept, s, voices, pointer, takes)
	return cons != nil || len(kept) > 0
}

// typesDisjoint reports whether no instance has two of types. A type listed
// twice overlaps itself, and JSON's types are otherwise disjoint but for one
// pair: every integer is a number.
func typesDisjoint(types []statement.Kind) bool {
	if len(slices.Compact(slices.Sorted(slices.Values(types)))) != len(types) {
		return false
	}
	return !slices.Contains(types, statement.KindInteger) || !slices.Contains(types, statement.KindNumber)
}

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
	said := branchesSay(c, s)
	if slices.ContainsFunc(siblings, func(k string) bool { return !slices.Contains(said, k) }) {
		return true
	}
	return !unionBranchesDeclareShape(c, s)
}

// restatableKeywords are the keywords beside a union that its branches can say
// in full, read as admission reads what a schema admits.
var restatableKeywords = []string{"type", "format", "enum", "const", "items"}

// branchesSayOf keys what branchesSay read of one schema.
type branchesSayOf struct {
	schema *statement.Schema
}

// branchesSay returns the keywords of restatableKeywords s writes beside its
// oneOf and anyOf that every branch of both already says: a `type` naming each
// kind a branch admits, null included, and a `format`, an `enum`, a `const` or
// an `items` each branch is held to (admission.holds). Such a keyword adds
// nothing to the union, so the position reads as if it were not written
// (docs/compilers/openapi.md#union-beside-structure). A read cut short says
// none, and so does a schema writing no union, of which "every branch" would
// say anything. Every reader of the position asks, so it is read once a run.
func branchesSay(c lowering.Ctx, s *statement.Schema) []string {
	return lowering.Remembered(c, branchesSayOf{s}, func() []string {
		choice := choiceIn(s)
		branches := slices.Concat(choice.OneOf, choice.AnyOf)
		if len(branches) == 0 {
			return nil
		}
		w := newAdmission(c)
		reads := make([]admitted, len(branches))
		for i, b := range branches {
			reads[i] = lowered(&w, c.Statements().Sub(b))
		}
		if w.incomplete() {
			return nil
		}
		var out []string
		for _, key := range restatableKeywords {
			if !hasKey(s, key) {
				continue
			}
			if !slices.ContainsFunc(reads, func(r admitted) bool { return !branchSays(&w, r, s, key) }) {
				out = append(out, key)
			}
		}
		return out
	})
}

// branchSays reports whether a branch admitting branch, as w reads it, says all
// that s's keyword key does. A `type` is compared with null in it, which
// admission.holds leaves to the nulls reader: a union lowered alone lifts a null
// branch onto the position, so a `type` leaving null out says more than a branch
// admitting it. An `items` list says what no single schema does.
func branchSays(w *admission, branch admitted, s *statement.Schema, key string) bool {
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
	// unionBothCombinators marks an allOf branch declaring oneOf and anyOf at
	// once: only one of the two can become the value, so distributing either
	// would leave the other in no branch's keeping.
	unionBothCombinators
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
		return unionUncomposableBody
	}
	// Of a oneOf and an anyOf written beside the body, the oneOf is elected
	// (unionBranches), as on two entries of its allOf, and the other is kept on
	// each variant (buildComposedVariant).
	if !branchesNameReferents(c, s) {
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

// routedSubtype reports whether a discriminator on an ancestor of s, written at
// pointer, routes to s as its subtype: by a mapping key or by its name as a
// component, the tag its lowering takes (subtypeDiscriminatorValue). That
// lowering reports the tag's own diagnostics, so this reading drops them.
func routedSubtype(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) bool {
	tag, _ := subtypeDiscriminatorValue(c, s, c.IDs().ForPointer(pointer), pointer, nil)
	return tag.routed
}

// classifyBranchSet is the part of classifyUnionSiblings that reads the union
// holder writes rather than the body: whichever of a union beside the body and
// one on a branch of its allOf the body distributes (branchUnion), it does so
// only where this says unionDistributed.
func classifyBranchSet(c lowering.Ctx, holder *statement.Schema) unionLowering {
	if choice := choiceIn(holder); len(choice.OneOf) > 0 && len(choice.AnyOf) > 0 {
		return unionBothCombinators
	}
	if !branchesNameReferents(c, holder) {
		return unionUnresolvedBranch
	}
	return unionDistributed
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
	if constIn(s) != nil || enumWritten(s) {
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
func branchesNameReferents(c lowering.Ctx, s *statement.Schema) bool {
	branches, _, _ := unionBranches(s)
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
// keywords, per classifyUnionSiblings.
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(c, ts, depth, s, pointer, reason, why)
	}
	switch classifyUnionSiblings(c, s, pointer) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, depth, s, besideUnion(s, pointer), pointer)
	case unionValidationOnly:
		return beside(ir.ReasonValidationOnly, "")
	case unionSharesNoKind:
		return beside(ir.ReasonDegradedLowering,
			"none of its branches shares a kind with the allOf beside it, so no instance satisfies them together")
	case unionUncomposableBody:
		return beside(ir.ReasonDegradedLowering, "the body is not a model, so it carries no composition to distribute into")
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(c, s, pointer), diags...)
	case unionRoutedSubtype:
		return lowerSubtypeUnion(c, ts, depth, s, besideUnion(s, pointer), pointer)
	case unionNoLiveBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"none of its branches admits a value its body admits, so no instance satisfies them together")
		return id, append(diags, diagDeadBranches(c, s, besideUnion(s, pointer), deadBranches(c, s, s), pointer, false)...)
	default: // unionHierarchyBase
		return beside(ir.ReasonDegradedLowering,
			"the discriminator beside it routes to branches composing this schema, its subtypes, which distributing them into a union would undo")
	}
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, but none for a $ref on a loop of
// $refs, which the load phase reports once (onLoop). The
// branch text survives verbatim under Unmodeled, so nothing is dropped, but a
// reference that resolves nowhere is a defect of the document itself, reported
// at the same severity as everywhere else, with the reason refTypeRef's report
// gives (unresolvedWhy).
func diagUnresolvedBranches(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	branches, key, _ := unionBranches(s)
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
// holds is settled once every node is interned (deferDiscriminatorBesideUnion).
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
	inner, diags := lower(c, ts, depth, s, pointer)
	owner := inner
	if !LoweredToOwnNode(c, pointer, ir.TypeRef{Target: inner}) {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, ir.TypeRef{Target: inner})
		diags = append(diags, aliasDiags...)
	}
	deferDiscriminatorBesideUnion(c, ts, owner, s, s, pointer)
	return owner, append(diags, preserveUnionSiblings(c, ts, owner, s, pointer, reason, why)...)
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins the validation-only keywords and is
// reported with it; anything else is a degraded lowering and says so.
func preserveUnionSiblings(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *statement.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	return append(diags, preserveUnionSiblingsAt(c, &td.Common().Unmodeled, s, pointer, reason, why)...)
}

// preserveUnionSiblingsAt is preserveUnionSiblings' body, addressed by the
// Unmodeled map to write into. why is the caller's complete sentence.
func preserveUnionSiblingsAt(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, p, s, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
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

// buildsVariants reports whether a Union s lowers to holds variants s builds
// itself, each conjoining what s writes: a type array's (lowerUnion) or a
// distributed composition's (lowerDistributedUnion). A union of s's oneOf or
// anyOf alone (lowerOneOfAnyOf) holds the nodes its branches lowered to
// instead. It reads what lowerSchemaBody dispatches on.
func buildsVariants(c lowering.Ctx, s *statement.Schema) bool {
	return !declaresUnion(s) || hasUnionSiblings(c, s)
}

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across the variants of u: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (docs/ir/models.md#composition-beside-a-union) alongside its
// branch, so the composition is carried on every variant rather than merged
// into one or dropped. A union an allOf branch writes is the branch's to give,
// not residue each variant keeps (allOfComposition.conjoins).
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, u unionAt, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	u.distributed = true
	u.dead = deadBranches(c, s, u.holder)
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, comp: readAllOf(c, s, pointer),
			voices: positionVoices(c, s, pointer, ir.TypeRef{}), inferred: bodyKindInferred(c, s), union: u}
		diags = append(diags, diagDisjointConjuncts(c, body.comp, composedShape)...)
		diags = append(diags, keepDeadBranches(c, &common.Unmodeled, s, u, pointer)...)
		def, unionDiags := buildUnion(c, ts, u, common,
			func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
				ref, variantDiags := composedVariant(c, ts, depth, body, b, vptr)
				diags = append(diags, variantDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	// The union read s's discriminator, so one it cannot hold says why.
	diags = append(diags, preserveVariantsUnhomed(c, ts, s, id, taglessShape(nil, s, distributedShape), nil, pointer)...)
	where := "co-declared with structural keywords"
	if u.holder != s {
		where = "written on an allOf branch conjoins with the rest of the composition"
	}
	return id, append(diags, c.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf %s; the composition is distributed across the union variants", where))
}

// lowerSubtypeUnion lowers s, a subtype a discriminator on an ancestor routes to,
// with the union u it conjoins, beside its allOf or on a branch: s stays the
// Model its composition lowers to, a subtype of that ancestor, and names as its
// last Mixin a Union minted for u (mintUnion), as an allOf naming a component
// holding u composes it (docs/ir/models.md#a-union-a-composition-names). A
// discriminator written beside u is the Union's, routing its variants, so the
// Model neither lowers nor keeps it, unless the Union cannot hold it
// (tagLocated). The position's first lowering composes the union; a later one
// finds it.
func lowerSubtypeUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, u unionAt, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	_, lowered := ts.Lookup(c.IDs().Coordinate(pointer))
	d := dispatchOf(s)
	if u.disc != nil && u.discAt == pointer && tagLocated(u.disc, nil) {
		d.said = append(d.said, "discriminator")
	}
	id, diags := lowerElected(c, ts, depth, s, pointer, d)
	if lowered {
		return id, diags
	}
	mixin, mintDiags := mintUnion(c, ts, depth, u)
	diags = append(diags, mintDiags...)
	diags = append(diags, composeMixin(c, ts, id, mixin, pointer)...)
	where := "co-declared with"
	if u.holder != s {
		where = "written on an allOf branch of"
	}
	return id, append(diags, c.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf %s a schema a discriminator routes to as its subtype; the union is composed as a mixin of the model", where))
}

// mintUnion registers the Union u, a routed subtype's union (lowerSubtypeUnion),
// at the keyword its holder writes it under, in the composed namespace: no
// schema stands there, and a variant's path ends in an index, a field's in a
// name. Each variant is its branch's own lowering, as a union written alone
// lowers it, so the node is what a component holding u would be.
func mintUnion(c lowering.Ctx, ts *compile.Types, depth int, u unionAt) (ir.TypeID, []ir.Diagnostic) {
	_, key, _ := unionBranches(u.holder)
	at := u.at + ids.Ptr(key)
	id := c.IDs().ComposedType(at)
	common := commonFor(c, id, at)
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(c, u.at), key))
	var diags []ir.Diagnostic
	node, unionDiags := buildUnion(c, ts, u, common, func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
		ref, refDiags := Ref(c, ts, depth, b, vptr)
		diags = append(diags, refDiags...)
		return ref
	})
	ts.Register(id, node)
	return id, slices.Concat(diags, unionDiags)
}

// composeMixin appends mixin to the Mixins of the model id names, last, so the
// $ref entries its source writes classify first. Its one caller has just
// lowered the model, so any other node there is a compiler bug.
func composeMixin(c lowering.Ctx, ts *compile.Types, id, mixin ir.TypeID, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	m, isModel := td.(*ir.Model)
	if !isModel {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: type %q is not the model a routed subtype composes its union into", id)}
	}
	m.Mixins = append(m.Mixins, ir.TypeRef{Target: mixin})
	return nil
}

// composedBody is the schema every distributed variant carries: the enclosing
// schema and where it is written.
type composedBody struct {
	schema *statement.Schema
	comp   allOfComposition   // schema's flattened allOf, built once for every variant
	voices []annotation.Voice // what comp's folding branches say of the position, read once too
	// inferred marks a schema the kind-inference policy made the model its
	// variants compose (bodyKindInferred), so a variant over a branch that
	// declares no object names the policy.
	inferred bool
	// union is the union distributed across the body.
	union unionAt
}

// bodyKindInferred reports whether the kind-inference policy made s, a body a
// union distributes across, the model its variants compose
// (docs/compilers/openapi.md#shape-applicator): s names no type and composes
// no allOf, and the union's branches admit a kind that is no object, or name
// theirs by the policy's reading, as untypedKinds tells the two apart.
func bodyKindInferred(c lowering.Ctx, s *statement.Schema) bool {
	if !c.KindInference.Honours() || len(effectiveTypes(s)) > 0 || allOfComposes(c, s) {
		return false
	}
	return !admitsObjectsAlone(c, c.Statements().Read(s))
}

// admitsObjectsAlone reports whether the document itself declares that st
// admits an object and no other kind, null aside: its read is whole and rests
// on no kind the kind-inference policy read.
func admitsObjectsAlone(c lowering.Ctx, st *statement.Statement) bool {
	r := admissionOf(c, st)
	return !r.inferred && !r.cut && r.kinds&^kindObject == 0
}

// composedVariant synthesizes the Model for one distributed variant: the
// enclosing schema's composition and own properties conjoined with that
// branch's referent. The branch is conjoined last so the enclosing composition
// classifies first, keeping the "sole $ref becomes Base" reading of
// docs/compilers/openapi.md#allof-classification of the allOf the source
// actually wrote.
//
// The Model gets an ID of its own (ids.ComposedType), not the branch pointer's,
// which already denotes the branch schema. Interning there made the two race
// for one pointer, so a $ref to the branch could find either the bare branch or
// the full variant, depending on lowering order
// (docs/ir/models.md#composition-beside-a-union).
func composedVariant(c lowering.Ctx, ts *compile.Types, depth int, body composedBody,
	b *statement.Statement, vptr jsontext.Pointer,
) (ir.TypeRef, []ir.Diagnostic) {
	id := c.IDs().ComposedType(vptr)
	common := commonFor(c, id, vptr)
	// The Model conjoins the union's body with the branch, so it is named for
	// both.
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(c, body.comp.pointer), lowering.NameAt(c, vptr)))
	if body.inferred && !admitsObjectsAlone(c, b) {
		common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
	}
	variant, unpacked, composes := branchReferent(c, b)
	if !composes {
		// The variant is the branch's one home, so the branch lowers nowhere and
		// what it says of null is read off it, reported where the policy reads one.
		def, diags := buildComposedVariant(c, ts, depth, body, b, "", false, common)
		ts.Register(id, def)
		n := newNulls(c)
		r := n.conjunct(b, siteAt(vptr))
		ref := composedVariantRef(c, id, body.schema, r.declared == nullAdmitted, r.lenient == nullAdmitted)
		return ref, append(diags, n.findings(c)...)
	}
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(c, ts, depth, b, vptr)
	referent := branch.Target
	if unpacked {
		referent = variant
	}
	def, variantDiags := buildComposedVariant(c, ts, depth, body, b, referent, true, common)
	ts.Register(id, def)
	ref := composedVariantRef(c, id, body.schema, branch.Nullable && branch.Inferred == "", branch.Nullable)
	return ref, append(diags, variantDiags...)
}

// composedVariantRef returns the reference a union variant naming the
// synthesized variant id takes, its branch admitting null under the declared
// reading and the lenient one as given. The variant conjoins the enclosing
// body with the branch, so it admits null exactly when the branch does and the
// body does not forbid it: the allOf rule, applied to a conjunction the source
// does not spell as an allOf, under each reading of nullable. The bit lives on
// this TypeRef, not on the variant's Base or Mixins, for the reason
// conjoinBranch records.
func composedVariantRef(c lowering.Ctx, id ir.TypeID, body *statement.Schema, declared, lenient bool) ir.TypeRef {
	if !declared && !lenient {
		return ir.TypeRef{Target: id}
	}
	n := newNulls(c)
	r := n.schema(body, nullSite{})
	return nullRef(c, id, declared && r.declared != nullForbidden, lenient && r.lenient != nullForbidden)
}

// buildComposedVariant assembles the variant Model itself, composing branch,
// b's referent, or keeping b where composes is false. Every fill reads the
// enclosing schema at its pointer, so the properties, their PropIDs and any
// shared additionalProperties node are the one set the source declared.
//
// The enclosing schema's value constraints, its folding branches' among them,
// are read as lowerAllOf reads them (GitHub #407), for the kinds the variant
// admits: those b lowers to among the model's. So a variant over a branch no
// object satisfies holds none, and the census keeps a bound no variant holds
// on the Union (GitHub #1936).
func buildComposedVariant(c lowering.Ctx, ts *compile.Types, depth int, body composedBody, b *statement.Statement,
	branch ir.TypeID, composes bool, common ir.TypeCommon,
) (ir.TypeDef, []ir.Diagnostic) {
	kinds, _ := lowersTo(c, b)
	cons, diags := ConstraintsOf(c, &common.Unmodeled, body.schema, body.voices, body.comp.pointer, body.comp.bounds&kinds)
	m := &ir.Model{TypeCommon: common, Constraints: cons}
	fields := newModelFields(c, ts, m)
	if composes {
		// The branch is distributed into the body, whose own declarations are
		// read after it, as a model's own declarations follow what it composes.
		fields.compose(branch, m.Provenance.Pointer)
	}
	base, hasBase := selectAllOfBase(c, body.comp.branches, m.ID)
	diags = append(diags, fillAllOf(c, ts, depth, m, fields, body.comp, base, hasBase)...)
	diags = append(diags, fillModelProperties(c, ts, depth, fields, body.schema, body.comp.pointer)...)
	requireMembers(m, compositionRequired(objectIn(body.schema).Required, body.comp))
	diags = append(diags, fillComposedAdditional(c, ts, depth, m, body.schema, body.comp)...)
	diags = append(diags, applyFalseBranches(c, m, body.comp)...)
	_, key, _ := unionBranches(body.union.holder)
	diags = append(diags, preserveUnusedCombinator(c, &m.Unmodeled, body.union.holder, key, body.union.at)...)
	if !composes {
		// A variant composing nothing over a body that is a map alone is that map.
		diags = append(diags, keepUncomposedBranch(c, m, b)...)
		return objectNode(m), diags
	}
	conjoinBranch(m, branch)
	// No variant carries a DiscriminatorValue, which tags a hierarchy's
	// subtype: a schema a discriminated ancestor routes to is never distributed
	// (unionRoutedSubtype), and a discriminator beside the union routes its
	// variants by its mapping (spellImplicitRoutes).
	return m, diags
}

// branchReferent reports whether the variant composed over the union branch b
// names a referent as its Base or a Mixin, as an allOf's $ref entry composes
// (composesRef), read before b lowers, so a branch the variant keeps lowers
// nowhere: the node b lowers to where it is an object alone, null aside, or its
// read is cut short; the object variant of a type list b's $ref names, unpacked
// (objectVariant), where b writes nothing beside its $ref, which would bind the
// whole list on no node the variant names; else none, since a model composes
// only models.
func branchReferent(c lowering.Ctx, b *statement.Statement) (variant ir.TypeID, unpacked, composes bool) {
	if admissionOf(c, b).cut {
		return "", false, true
	}
	if kinds, _ := lowersTo(c, b); kinds == kindObject {
		return "", false, true
	}
	variant, unpacked = objectVariant(c, b)
	if !unpacked || writesMoreThanRef(b.Schema) {
		return "", false, false
	}
	return variant, true, true
}

// keepUncomposedBranch keeps the union branch b, which the variant m does not
// compose (branchReferent), verbatim under m's Unmodeled (unionBranchKey), and
// reports it, as keepUncomposedRef keeps an allOf's $ref entry. b lowers
// nowhere, so this is its one home; the target of a $ref it writes lowers
// where the $ref lands, as every landing does (PromiseLandings).
func keepUncomposedBranch(c lowering.Ctx, m *ir.Model, b *statement.Statement) []ir.Diagnostic {
	at := m.Provenance.Pointer
	kept, diags := annotation.PreserveNodeInto(&m.Unmodeled, unionBranchKey(at), b.Node,
		ir.ReasonDegradedLowering, c.Place().At(at))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at, "%s",
		"this branch is not an object alone, and the model distributing the body over it composes only "+
			"models, so the branch is kept verbatim under Unmodeled"))
}

// conjoinBranch adds one union branch's referent to the variant model,
// classified per docs/ir/models.md#composition-beside-a-union: Base when the
// enclosing schema composed nothing, a Mixin otherwise. Only the target is
// carried, never the branch ref's Nullable bit — fillAllOf drops it for an
// allOf entry on the same reasoning: Base and Mixins name a conjunct, and
// nullability is a property of the usage that names the conjunction, not of one
// side of it. The usage here is the union variant, and composedVariantRef is
// what puts the bit on it.
func conjoinBranch(m *ir.Model, branch ir.TypeID) {
	target := ir.TypeRef{Target: branch}
	if m.Base == nil && len(m.Mixins) == 0 {
		m.Base = &target
		return
	}
	m.Mixins = append(m.Mixins, target)
}
