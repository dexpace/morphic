// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A set of one non-null branch X
// beside any number of bare null branches collapses to nullable X (ir-design
// §3.3); a set with no non-null branch at all (every branch a bare `type:
// null`) has no X for that collapse to name, so it lowers to nullable `any`
// instead (lowerNullOnlyUnion, GitHub #416); everything else becomes a Union
// with one Variant per non-null branch (oneOf exclusive, anyOf not), never
// collapsing a union into optional fields.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, ok := nullUnionCollapse(s, pointer); ok {
		ref, diags := Ref(c, ts, depth, inner, ip)
		// The null branch declares the null, so no heuristic's marker the
		// branch's own reference carried says anything of it.
		ref.Nullable, ref.Inferred = true, ""
		return ref, diags
	}
	if allNullUnion(s) {
		return lowerNullOnlyUnion(c, ts, s, pointer)
	}
	var diags []ir.Diagnostic
	tid := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		def, unionDiags := buildUnion(c, ts, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef {
				ref, refDiags := Ref(c, ts, depth, b, vptr)
				diags = append(diags, refDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	ref, nullDiags := schemaNullRef(c, tid, s, siteAt(pointer))
	return ref, append(diags, nullDiags...)
}

// nullUnionCollapse detects a oneOf/anyOf with exactly one non-null branch
// beside `type: null` branches, and returns that branch's schema and pointer,
// so it lowers as nullable X, not a union node (ir-design §3.3).
//
// A schema declaring both combinators collapses neither: the co-declared anyOf
// conjoins, so the position is not nullable X. The Union keeps the unused
// combinator (preserveUnusedCombinator), which X's shared node cannot.
func nullUnionCollapse(s *oas3.Schema, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return nil, "", false
	}
	variants, key, _ := unionBranches(s)
	var nonNull *oas3.JSONSchema[oas3.Referenceable]
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, v := range variants {
		if isNullSchema(v) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = v, i
		nonNullCount++
	}
	if nullCount == 0 || nonNullCount != 1 {
		return nil, "", false
	}
	return nonNull, pointer + ids.Ptr(key, strconv.Itoa(nonNullIdx)), true
}

// isNullSchema reports whether a variant schema is the bare null-typed schema.
func isNullSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if js == nil || !js.IsSchema() {
		return false
	}
	s := js.GetSchema()
	if s == nil {
		return false
	}
	types := s.GetType()
	return len(types) == 1 && types[0] == oas3.SchemaTypeNull
}

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects is a bare `type: null` schema. nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without this check
// lowerOneOfAnyOf's buildUnion fallback strips every branch as a null marker
// and interns a Union with none left — the empty value space irverify's
// ir/union-no-variants rejects (GitHub #416). The branch set still admits
// exactly one value, the same one a bare `{type: null}` schema at this
// position admits, so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(s *oas3.Schema) bool {
	variants, _, _ := unionBranches(s)
	if len(variants) == 0 {
		return false
	}
	for _, v := range variants {
		if !isNullSchema(v) {
			return false
		}
	}
	return true
}

// oneOfAnyOfHasNull reports whether any oneOf/anyOf branch is a bare `type: null`
// schema, so its nullability lifts onto the enclosing union ref rather than
// degrading to an `any` variant.
func oneOfAnyOfHasNull(s *oas3.Schema) bool {
	if slices.ContainsFunc(s.GetOneOf(), isNullSchema) {
		return true
	}
	return slices.ContainsFunc(s.GetAnyOf(), isNullSchema)
}

// lowerNullOnlyUnion lowers a oneOf/anyOf every one of whose branches is a bare
// `type: null` schema (allNullUnion). One null branch or several admit exactly
// the value a bare `{type: null}` schema at this position admits — null alone
// — so this position lowers the way lowerUntyped's own null-only body does:
// the shared `any` primitive with Nullable set. The combinator that produced
// it, and the other one too when both are declared, carries no shape a Union
// or a Scalar's fields could hold, so it is kept verbatim under Unmodeled
// instead of dropped (GitHub #416; docs/compilers/openapi.md#union-null-only).
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	// inner is the shared `any` primitive: no schema pointer ever interns at
	// its ID, so this position never already owns it. Hoist an alias
	// unconditionally, rather than testing for a case that cannot occur, so
	// the preserved union attaches to a node this pointer owns and not to the
	// primitive every other unrelated position also resolves to.
	inner := ts.PrimID(ir.PrimAny)
	owner, diags := internAlias(c, ts, s, pointer, ir.TypeRef{Target: inner, Nullable: true})
	return ir.TypeRef{Target: owner, Nullable: true}, append(diags, preserveNullOnlyUnion(c, ts, owner, s, pointer)...)
}

// preserveNullOnlyUnion keeps s's oneOf/anyOf verbatim under the owning node's
// Unmodeled: lowerNullOnlyUnion's node is the shared `any` primitive or an
// alias over it, neither of which carries a branch set of its own to hold
// them in. It also runs the census lower() runs for a body, which this arm
// never reaches, so a discriminator written beside the branches is kept too.
// Its report names the shared primitive as what the position lowered to, as
// that census does for a bare `{type: null}`, not the alias keeping the
// keywords.
func preserveNullOnlyUnion(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	diags = append(diags, recordUnhomedAt(c, &common.Unmodeled, s, unhomedKeywords(s, td, nil), nodeShape(ir.KindPrimitive), pointer)...)
	kept, keepDiags := preserveBranchSets(c, &common.Unmodeled, s, ir.ReasonDegradedLowering, pointer)
	diags = append(diags, keepDiags...)
	if len(kept) == 0 {
		return diags
	}
	// Both keywords land here when both are declared: buildUnion, and its
	// preserveUnusedCombinator, are never reached from this arm, so this is the
	// only place either combinator gets a home.
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"every branch admits only the null value, so this position lowered as nullable any; "+
			"%s kept verbatim under Unmodeled", strings.Join(kept, " and ")))
}

// preserveBranchSets stores s's declared oneOf/anyOf verbatim under p, in
// keyword order, reporting one it cannot convert. ir.ReasonValidationOnly
// routes each through §4.7's keyword-family reporting
// (annotation.PreserveValidationOnlyKeyword) and returns no keywords. Every
// other reason keeps each plainly (annotation.PreserveSchemaKeyword) and returns
// the keywords kept, so a caller with a message of its own knows what to name.
//
// preserveUnionSiblingsAt and preserveNullOnlyUnion share this loop: they keep
// the same keywords because the node they attach to has no branch set of its
// own, and differ only in the sentence that explains why.
func preserveBranchSets(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, reason ir.UnmodeledReason, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var kept []string
	for _, kw := range []string{"oneOf", "anyOf"} {
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, kw, pointer, c.ProvenanceAt)...)
			continue
		}
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, kw, reason, pointer, c.ProvenanceAt)
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
func unionBranches(s *oas3.Schema) ([]*oas3.JSONSchema[oas3.Referenceable], string, bool) {
	if branches := s.GetOneOf(); len(branches) > 0 {
		return branches, "oneOf", true
	}
	return s.GetAnyOf(), "anyOf", false
}

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over
// verbatim on the Union the position lowers to, and reports it. A schema
// declaring only one combinator passes over nothing and is left alone.
//
// A schema writing both conjoins them, but one ir.Union carries one branch set.
// The position still lowers to the elected set, a shape the IR can express, and
// the other stays recoverable beside it, as recordSkippedFamilies does for
// keyword families (docs/compilers/openapi.md#competing-keywords).
//
// Where a structural sibling is written as well, classifyUnionSiblings never
// distributes the schema, so neither set is elected and both are kept beside
// the body (lowerBesideUnmodeledUnion).
func preserveUnusedCombinator(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, won string, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(s.GetOneOf()) == 0 || len(s.GetAnyOf()) == 0 {
		return nil
	}
	unused := otherCombinator[won]
	kept, diags := annotation.PreserveSchemaKeyword(p, s, unused, ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
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
type variantTypeFunc func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef

// buildUnion assembles the Union node for a oneOf/anyOf schema, attaching a
// discriminator when one is declared. common is already built by the caller
// (internNode), so buildUnion needs no hint of its own to build one.
//
// The variants are lowered before the discriminator, so its mapping resolves
// knowing what each variant names (readsThrough). A union the discriminator
// routes is WireTagged, an anyOf as much as a oneOf: the tag property's value
// names its variant (ir-design §4.4).
func buildUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, variantType variantTypeFunc) (ir.TypeDef, []ir.Diagnostic) {
	branches, key, exclusive := unionBranches(s)
	diags := preserveUnusedCombinator(c, &common.Unmodeled, s, key, pointer)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if isNullSchema(b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vptr := pointer + ids.Ptr(key, strconv.Itoa(i))
		t := variantType(b, vptr)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(variantHint(c, ts, b, vptr, t)),
			Type: t,
		})
	}
	u := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
	}
	disc, discDiags := lowerDiscriminator(c, ts, s, nil, variants, pointer)
	u.Discriminator = disc
	u.WireTagged = disc != nil
	return u, append(diags, discDiags...)
}

// variantHint is the hint the union variant for the branch b written at vptr,
// which lowered to t, takes: the branch's own (lowering.NameAt), unless b is
// written inline and t is a decorator (ir.Decorated) over the $ref b's allOf
// names, when it is the name that $ref suggests (lowering.NameThrough), as a
// branch holding it would be named. 3.0 annotates a branch only by wrapping
// its $ref, so `{allOf: [{$ref: X}], description: …}` names its variant as
// 3.1's `{$ref: X, description: …}` does (GitHub #1018). The node t keeps the
// branch's own: a decorator collapses into its reference wherever it is used.
func variantHint(c lowering.Ctx, ts *compile.Types, b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, t ir.TypeRef) string {
	own := lowering.NameAt(c, vptr)
	s := annotation.SchemaOf(b)
	if s == nil || isRefBranch(b) {
		return own
	}
	// The flattened list, as b lowers: a $ref a nested wrapper holds names the
	// variant as it would written at the top.
	branches := allOfBranches(s, vptr).branches
	base, ok := selectAllOfBase(c.Bindings(), branches)
	if !ok {
		return own
	}
	if td, ok := ts.Node(t.Target); !ok || !decorates(td) {
		return own
	}
	return cmp.Or(lowering.NameThrough(c, branches[base].pointer), own)
}

// decorates reports whether td is a decorator (ir.Decorated).
func decorates(td ir.TypeDef) bool {
	_, ok := ir.Decorated(td)
	return ok
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// untagged union with one variant per declared type, exclusive unless two of
// them overlap (typesDisjoint). A type array asks for one of its types, not for
// exactly one, so overlapping variants must not exclude each other; with an
// integer width, integer and number name the same primitive.
func lowerUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, types []oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, vDiags := lowerTyped(c, ts, depth, s, vptr, st)
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
		}
	})
	return id, diags
}

// typesDisjoint reports whether no instance has two of types. JSON's types are
// disjoint but for one pair: every integer is a number.
func typesDisjoint(types []oas3.SchemaType) bool {
	return !slices.Contains(types, oas3.SchemaTypeInteger) || !slices.Contains(types, oas3.SchemaTypeNumber)
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...) or
// a keyword that narrows an instance without declaring one. When it does, the
// union alone cannot represent the schema, so the sibling body must be lowered
// too rather than dropped (invariant 2, ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	return narrowsInstance(s) || declaresShape(s)
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
	// unionUncomposableBody marks a union beside a body that is not a Model — a
	// const, an enum, a scalar, a bare catch-all — so there are no composition
	// fields to distribute into.
	unionUncomposableBody
	// unionBothCombinators marks a schema declaring oneOf and anyOf at once:
	// only one of the two can become the value, so distributing either drops the
	// other.
	unionBothCombinators
	// unionInlineBranch marks a union with a branch written inline rather than as
	// a $ref, so it names no node for Base/Mixins to point at.
	unionInlineBranch
	// unionUnresolvedBranch marks a union whose every branch is a $ref but at
	// least one names nothing this compilation can resolve — a target in a
	// document it did not read, an undeclared component, a pointer addressing no
	// schema.
	unionUnresolvedBranch
	// unionDiscriminated marks a union whose branches a declared discriminator
	// binds by name, which distribution would break.
	unionDiscriminated
)

// classifyUnionSiblings picks the lowering for a schema whose oneOf/anyOf sits
// beside structural keywords. JSON Schema conjoins keywords, so such a schema
// is an intersection of a structural body and a union, which the IR
// deliberately has no combinator for (ir-design §15). Every outcome keeps both
// sides: classified where the IR can express the conjunction, verbatim where it
// cannot.
//
// A declared discriminator rules distribution out: its mapping names the branch
// schemas, which distribution replaces with synthesized composed models
// (pass/validate's discriminator-missing-variant rule is the mirror). The rule
// docs/compilers/openapi.md#union-beside-structure enumerates this and the
// other residual shapes.
func classifyUnionSiblings(c lowering.Ctx, s *oas3.Schema) unionLowering {
	if !unionBranchesDeclareShape(s) {
		return unionValidationOnly
	}
	if !composesAsModel(s) {
		return unionUncomposableBody
	}
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return unionBothCombinators
	}
	if branches, _, _ := unionBranches(s); slices.ContainsFunc(branches, isInlineBranch) {
		return unionInlineBranch
	}
	if !branchesNameReferents(c, s) {
		return unionUnresolvedBranch
	}
	if s.GetDiscriminator() != nil {
		return unionDiscriminated
	}
	return unionDistributed
}

// unionBranchesDeclareShape reports whether any oneOf/anyOf branch contributes
// data shape. When none does — `oneOf: [{required: [a]}, {required: [b]}]`, the
// commonest instance — the union narrows what the sibling body accepts without
// changing it, which is dependentRequired's job description: validation logic,
// not shape (ir-design §4.7).
func unionBranchesDeclareShape(s *oas3.Schema) bool {
	return slices.ContainsFunc(s.GetOneOf(), branchDeclaresShape) ||
		slices.ContainsFunc(s.GetAnyOf(), branchDeclaresShape)
}

// branchDeclaresShape reports whether one union branch declares data shape. A
// $ref always does; a bare `type: null` never does, since it contributes only
// the enclosing reference's Nullable bit.
func branchDeclaresShape(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	if isRefBranch(b) {
		return true
	}
	s := b.GetSchema()
	if s == nil {
		return false // a boolean branch declares no shape of its own
	}
	return declaresShape(s) || len(s.GetOneOf())+len(s.GetAnyOf()) > 0
}

// composesAsModel reports whether the structural siblings lower to a Model,
// which is the only node with composition fields for a union to distribute
// across. It mirrors lower's dispatch: const and enum win over everything, then
// allOf, then a declared object type or a bare property set.
func composesAsModel(s *oas3.Schema) bool {
	if s.GetConst() != nil || enumWritten(s) {
		return false
	}
	if len(s.GetAllOf()) > 0 {
		return true
	}
	types := effectiveTypes(s)
	if len(types) == 1 && types[0] == oas3.SchemaTypeObject {
		return true
	}
	props := s.GetProperties()
	return len(types) == 0 && props != nil && props.Len() > 0
}

// branchesNameReferents reports whether every union branch can be conjoined with
// the sibling model. Base and Mixins conjoin *by reference*, so a branch
// qualifies exactly when it names a schema this compilation resolves; an inline
// branch (ruled out before this) could only be merged in keyword by keyword, and
// an unresolvable $ref names nothing to merge either way. The test is over the
// whole union because a half-distributed one would leave variants disagreeing
// about whether they carry the body, with nothing in the IR saying which.
func branchesNameReferents(c lowering.Ctx, s *oas3.Schema) bool {
	branches, _, _ := unionBranches(s)
	for _, b := range branches {
		if !c.RefScope().NamesReferent(b) {
			return false
		}
	}
	return true
}

// coDeclaredUnionWhyPrefix opens every lowerCoDeclaredUnion why string: at
// each of its reasons, a structural body genuinely sits beside the union
// (that is what routed the schema here rather than to a $ref-site union
// keeper), so the sentence that follows is always true of it. refSiteUnionWhy
// does not share this prefix, because at a $ref site it would not be
// (preserveUnionSiblingsAt's doc comment says why).
const coDeclaredUnionWhyPrefix = "oneOf/anyOf co-declared with structural keywords intersects with them, and "

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings.
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(c, ts, depth, s, pointer, reason, why)
	}
	switch classifyUnionSiblings(c, s) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, depth, s, pointer)
	case unionValidationOnly:
		return beside(ir.ReasonValidationOnly, "")
	case unionUncomposableBody:
		return beside(ir.ReasonDegradedLowering,
			"the body is not a model, so it carries no composition to distribute into")
	case unionBothCombinators:
		return beside(ir.ReasonDegradedLowering,
			"oneOf and anyOf are both declared, so distributing either would drop the other")
	case unionInlineBranch:
		return beside(ir.ReasonDegradedLowering,
			"a branch is written inline, so it names no referent to conjoin the body with")
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(c, s, pointer), diags...)
	default: // unionDiscriminated
		return beside(ir.ReasonDegradedLowering,
			"a declared discriminator binds the branches by name, which distributing them would break")
	}
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, but none for a $ref on a loop of
// $refs, which the load phase reports once (onLoop). The
// branch text survives verbatim under Unmodeled, so nothing is dropped, but a
// reference that resolves nowhere is a defect of the document itself, reported
// at the same severity as everywhere else, with the reason refTypeRef's report
// gives (unresolvedWhy).
func diagUnresolvedBranches(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	branches, key, _ := unionBranches(s)
	var diags []ir.Diagnostic
	for i, b := range branches {
		if c.RefScope().NamesReferent(b) || onLoop(c, b) {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr(key, strconv.Itoa(i)),
			"union branch $ref %q is unresolved%s; the branch is kept verbatim", b.GetRef().String(), unresolvedWhy(c, b)))
	}
	return diags
}

// lowerBesideUnmodeledUnion lowers the structural body of a schema that
// co-declares oneOf/anyOf and keeps the union verbatim beside it, so neither the
// structural shape nor the union is dropped. reason says which kind of union it
// is and why says what stopped a classified lowering; lowerCoDeclaredUnion picks
// both from classifyUnionSiblings' verdict. A discriminator the body's model
// holds is settled once every node is interned (deferDiscriminatorBesideUnion).
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
	inner, diags := lower(c, ts, depth, s, pointer)
	owner := inner
	if got, _ := ts.Lookup(c.IDs().Coordinate(pointer)); got != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, ir.TypeRef{Target: inner})
		diags = append(diags, aliasDiags...)
	}
	deferDiscriminatorBesideUnion(c, ts, owner, s, pointer)
	return owner, append(diags, preserveUnionSiblings(c, ts, owner, s, pointer, reason, why)...)
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins §4.7's keyword family and is reported
// with it; anything else is a §4.8 degradation and says so.
func preserveUnionSiblings(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	return append(diags, preserveUnionSiblingsAt(c, &td.Common().Unmodeled, s, pointer, reason, why)...)
}

// preserveUnionSiblingsAt is preserveUnionSiblings' body, addressed by the
// Unmodeled map to write into. PreserveRefSiteKeywords calls it directly to
// keep a $ref site's union on a carrier's Unmodeled, where there is no node to
// look up by ID (GitHub #406).
//
// why is the caller's complete sentence, not a clause in a shared frame:
// lowerCoDeclaredUnion's reasons presuppose a structural body at the position,
// and a $ref site has none, so a shared template would contradict itself there.
func preserveUnionSiblingsAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, p, s, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
}

// declaresUnion reports whether s writes oneOf or anyOf at all. It is the one
// predicate every $ref-site union keeper — a component or inline sub-schema
// (refSiteRef), a carrier (PreserveRefSiteKeywords), an allOf branch spelled
// as a $ref (fillAllOf) — gates hoisting a home for the union on, so the
// three cannot drift into checking it three different ways.
func declaresUnion(s *oas3.Schema) bool {
	return len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0
}

// refSiteUnionWhy is preserveUnionSiblingsAt's why at every $ref site: a
// complete sentence in its own right (see preserveUnionSiblingsAt's doc for
// why it cannot share lowerCoDeclaredUnion's framing), because a $ref site's
// own declaration is the reference alone — there is no structural body at
// that position for the union to distribute its composition across.
const refSiteUnionWhy = "oneOf/anyOf beside a $ref conjoin with it rather than composing into a structural body, so there is nothing at this position to distribute the union's composition across"

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across its variants: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (ir-design §4.3) alongside its branch, so the composition is
// carried on every variant rather than merged into one or dropped.
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, comp: allOfBranches(s, pointer), id: common.ID}
		def, unionDiags := buildUnion(c, ts, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef {
				ref, variantDiags := composedVariant(c, ts, depth, body, b, vptr)
				diags = append(diags, variantDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	return id, append(diags, c.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf co-declared with structural keywords; the composition is distributed across the union variants"))
}

// composedBody is the schema every distributed variant carries: the enclosing
// schema, where it is written, and the identity of the Union it lowered to —
// which is the node a base's discriminator mapping names, since the source
// declares one schema, not one per branch.
type composedBody struct {
	schema *oas3.Schema
	comp   allOfComposition // schema's flattened allOf, built once for every variant
	id     ir.TypeID
}

// composedVariant synthesizes the Model for one distributed variant: the
// enclosing schema's composition and own properties conjoined with that
// branch's referent. The branch is conjoined last so the enclosing composition
// classifies first, keeping ir-design §4.3's "sole $ref becomes Base" reading
// of the allOf the source actually wrote.
//
// The Model gets an ID of its own (ids.ComposedType), not the branch pointer's,
// which already denotes the branch schema. Interning there made the two race
// for one pointer, so a $ref to the branch could find either the bare branch or
// the full variant, depending on lowering order (§4.3).
func composedVariant(c lowering.Ctx, ts *compile.Types, depth int, body composedBody,
	b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer,
) (ir.TypeRef, []ir.Diagnostic) {
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(c, ts, depth, b, vptr)
	id := c.IDs().ComposedType(vptr)
	common := commonFor(c, id, vptr)
	// The Model conjoins the union's body with the branch, so it is named for
	// both.
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(c, body.comp.pointer), lowering.NameAt(c, vptr)))
	def, variantDiags := buildComposedVariant(c, ts, depth, body, branch.Target, common)
	ts.Register(id, def)
	return composedVariantRef(c, id, body.schema, branch), append(diags, variantDiags...)
}

// composedVariantRef returns the reference a union variant naming the
// synthesized variant id takes. The variant conjoins the enclosing body with
// the branch, so it admits null exactly when the branch does and the body does
// not forbid it: the allOf rule, applied to a conjunction the source does not
// spell as an allOf, under each reading of nullable. The branch's declared
// reading is its bit with no marker. The bit lives on this TypeRef, not on the
// variant's Base or Mixins, for the reason conjoinBranch records.
func composedVariantRef(c lowering.Ctx, id ir.TypeID, body *oas3.Schema, branch ir.TypeRef) ir.TypeRef {
	if !branch.Nullable {
		return ir.TypeRef{Target: id}
	}
	n := newNulls(c)
	r := n.schema(body, nullSite{})
	return nullRef(c, id, branch.Inferred == "" && r.declared != nullForbidden, r.lenient != nullForbidden)
}

// buildComposedVariant assembles the variant Model itself. Every fill reads the
// enclosing schema at the enclosing pointer, so the properties, their PropIDs
// and any shared additionalProperties node are the single set the source
// declared, named after the enclosing schema, not after whichever branch built
// them first.
//
// The enclosing schema's value constraints are read as lowerAllOf reads them
// (GitHub #407). Each variant carries a copy, since a bound on the schema
// constrains both sides of the disjunction and this path never runs the
// pointer's own census.
func buildComposedVariant(c lowering.Ctx, ts *compile.Types, depth int, body composedBody, branch ir.TypeID, common ir.TypeCommon) (ir.TypeDef, []ir.Diagnostic) {
	cons, diags := ConstraintsOf(c, &common.Unmodeled, body.schema, body.comp.pointer)
	m := &ir.Model{TypeCommon: common, Constraints: cons}
	idx := propIndex(c, ts, m)
	diags = append(diags, fillAllOf(c, ts, depth, m, idx, body.comp)...)
	diags = append(diags, fillModelProperties(c, ts, depth, idx, body.schema, body.comp.pointer)...)
	requireMembers(m, compositionRequired(body.schema.GetRequired(), body.comp))
	diags = append(diags, fillAdditional(c, ts, depth, m, body.schema, body.comp.pointer)...)
	diags = append(diags, applyFalseBranches(c, m, body.comp)...)
	conjoinBranch(m, branch)
	// The tag is the enclosing schema's: it is what a base's mapping names, and
	// the variants are its lowering. No discriminator of its own can be declared
	// here — a schema that declares one is never distributed.
	tag, tagDiags := subtypeDiscriminatorValue(c, body.schema, body.id, body.comp.pointer)
	m.DiscriminatorValue = tag
	diags = append(diags, tagDiags...)
	return m, diags
}

// conjoinBranch adds one union branch's referent to the variant model,
// classified per ir-design §4.3: Base when the enclosing schema composed
// nothing, a Mixin otherwise. Only the target is carried, never the branch
// ref's Nullable bit — fillAllOf drops it for an allOf entry on the same
// reasoning: Base and Mixins name a conjunct, and nullability is a property of
// the usage that names the conjunction, not of one side of it. The usage here is
// the union variant, and composedVariantRef is what puts the bit on it.
func conjoinBranch(m *ir.Model, branch ir.TypeID) {
	target := ir.TypeRef{Target: branch}
	if m.Base == nil && len(m.Mixins) == 0 {
		m.Base = &target
		return
	}
	m.Mixins = append(m.Mixins, target)
}
