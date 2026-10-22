package schema

import (
	"cmp"
	"encoding/base64"
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same (ir-design
// §4.3). Each object lowering calls it once, on a model that lists none yet.
func requireMembers(m *ir.Model, required []string) {
	byWire := merge.WireNameIndex(m.Properties)
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		if i, ok := byWire[name]; ok {
			m.Properties[i].Required = true
			continue
		}
		if !listed[name] {
			listed[name] = true
			m.Requires = append(m.Requires, name)
		}
	}
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(c lowering.Ctx, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, ok := c.Bindings().Entry(b)
	return ok && e.Chain.Member
}

// subtypeDiscriminatorValue returns the wire tag this allOf subtype carries in
// its discriminator hierarchy, or "" when no ancestor anchors one (ir-design
// §4.3): the mapping key naming the subtype, else its schema name as declared,
// not as its escaped pointer token spells it (GitHub #505). An inline subtype
// has no name, so only a mapping entry can tag it (GitHub #517).
//
// Every discriminated ancestor is asked, since an intermediate may declare none
// (GitHub #305). Of several mapping keys for the subtype the smallest in byte
// order is elected, as a mapping is unordered (GitHub #410); the base's
// Discriminator keeps them all, and an info diagnostic reports it.
func subtypeDiscriminatorValue(c lowering.Ctx, s *oas3.Schema, id ir.TypeID, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	ds := ancestorDiscriminators(c.Bindings(), s)
	if len(ds) == 0 {
		return "", nil
	}
	for _, d := range ds {
		tags := mappingTagsFor(c, d, id)
		if len(tags) == 0 {
			continue
		}
		if len(tags) == 1 {
			return tags[0], nil
		}
		return tags[0], []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"the discriminator mapping names this schema under %d keys (%s); "+
				"discriminatorValue holds the smallest in byte order, and the base's mapping keeps them all",
			len(tags), strings.Join(tags, ", "))}
	}
	// Read ok, not only the name: for a top-level entry of another kind, such as
	// components/x-shared/Cat, ComponentSchemaName still returns a name, and it
	// is no component schema's (GitHub #572).
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		return name, nil
	}
	return "", nil
}

// mappingTagsFor returns every key d's mapping spells for the type id, sorted
// in byte order, or nothing when the mapping does not name it. An empty result
// is what sends the caller to the implicit name, and it is distinct from a
// mapping that names id under the empty key, which returns that one key.
//
// Sorted rather than in source order because the caller elects the first, and
// a mapping's key order is not a property of the document it is written in.
func mappingTagsFor(c lowering.Ctx, d *oas3.Discriminator, id ir.TypeID) []string {
	m := d.GetMapping()
	if m == nil {
		return nil
	}
	// Derived once, not per entry: every subtype reads its base's whole mapping,
	// so n subtypes make n² lookups, and each derivation allocates.
	scope := c.RefScope()
	var tags []string
	for tag, target := range m.All() {
		if tid, ok := mappingTargetID(c, scope, d, target); ok && tid == id {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	return tags
}

// maxDiscriminatorAncestorDepth bounds how many composition levels
// ancestorDiscriminators climbs (styleguide bounded-everything rule). The visited
// set below is what makes the walk terminate; this is the explicit limit, set far
// beyond any hierarchy a source document plausibly declares.
const maxDiscriminatorAncestorDepth = 256

// ancestorDiscriminators returns the discriminators declared on s's composition
// ancestors, nearest first: the resolved targets of s's own $ref branches in
// source order, then theirs, level by level. Nearest means fewest hops, which
// decides between two ancestors whose mappings name the same subtype.
//
// The visited set bounds the work; the depth cap bounds only the levels. A
// schema reached by several branches would be walked once per path, so the
// frontier multiplies by the fan-out each level (2^level for a chain of
// diamonds). TestAllOf_DiscriminatorValueCyclicComposition (cyclic) and
// TestCompile_SharedCompositionAncestorsDoNotAmplify (acyclic) pin it.
func ancestorDiscriminators(refs *typed.Bindings, s *oas3.Schema) []*oas3.Discriminator {
	var out []*oas3.Discriminator
	visited := make(map[*oas3.Schema]bool)
	// s is visited before the walk starts, so a composition cycle cannot bring it
	// back as one of its own ancestors. Without this a schema in a cycle that
	// declares a discriminator answers to its own mapping, which is a hierarchy of
	// one thing standing above itself.
	visited[s] = true
	level := []*oas3.Schema{s}
	for depth := 0; depth < maxDiscriminatorAncestorDepth && len(level) > 0; depth++ {
		next := make([]*oas3.Schema, 0, len(level))
		for _, cur := range level {
			next = append(next, unvisitedRefTargets(refs, cur, visited)...)
		}
		for _, target := range next {
			if d := target.GetDiscriminator(); d != nil {
				out = append(out, d)
			}
		}
		level = next
	}
	return out
}

// unvisitedRefTargets returns the schemas s's $ref composition branches resolve
// to, in source order, skipping those already seen and marking those it returns.
// Marking as it enqueues rather than when the walk reaches them is what keeps a
// schema two branches both name from being queued, and walked, twice.
func unvisitedRefTargets(refs *typed.Bindings, s *oas3.Schema, visited map[*oas3.Schema]bool) []*oas3.Schema {
	var out []*oas3.Schema
	for b := range allOfRefBranches(s) {
		target := refBranchTarget(refs, b)
		if target == nil || visited[target] {
			continue
		}
		visited[target] = true
		out = append(out, target)
	}
	return out
}

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A set of one non-null branch X
// beside any number of bare null branches collapses to nullable X (ir-design
// §3.3); a set with no non-null branch at all (every branch a bare `type:
// null`) has no X for that collapse to name, so it lowers to nullable `any`
// instead (lowerNullOnlyUnion, GitHub #416); everything else becomes a Union
// with one Variant per branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, i, ok := nullUnionCollapse(s, pointer); ok {
		ref, diags := Ref(c, ts, anchors, depth, inner, ip, branchHint(c.Bindings(), inner, i))
		// The null branch declares the null, so no heuristic's marker the
		// branch's own reference carried says anything of it.
		ref.Nullable, ref.Inferred = true, ""
		return ref, diags
	}
	if allNullUnion(s) {
		return lowerNullOnlyUnion(c, ts, s, pointer, hint)
	}
	var diags []ir.Diagnostic
	tid := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		def, unionDiags := buildUnion(c, ts, anchors, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, vhint string) ir.TypeRef {
				ref, refDiags := Ref(c, ts, anchors, depth, b, vptr, vhint)
				diags = append(diags, refDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	ref, nullDiags := schemaNullRef(c, tid, s, siteAt(pointer))
	return ref, append(diags, nullDiags...)
}

// lowerNullOnlyUnion lowers a oneOf/anyOf every one of whose branches is a bare
// `type: null` schema (allNullUnion). One null branch or several admit exactly
// the value a bare `{type: null}` schema at this position admits — null alone
// — so this position lowers the way lowerUntyped's own null-only body does:
// the shared `any` primitive with Nullable set. The combinator that produced
// it, and the other one too when both are declared, carries no shape a Union
// or a Scalar's fields could hold, so it is kept verbatim under Unmodeled
// instead of dropped (GitHub #416; docs/compilers/openapi.md#union-null-only).
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	// inner is the shared `any` primitive: no schema pointer ever interns at
	// its ID, so this position never already owns it. Hoist an alias
	// unconditionally, rather than testing for a case that cannot occur, so
	// the preserved union attaches to a node this pointer owns and not to the
	// primitive every other unrelated position also resolves to.
	inner := ts.PrimID(ir.PrimAny)
	owner, diags := internAlias(c, ts, s, pointer, hint, ir.TypeRef{Target: inner, Nullable: true})
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
	// least one names nothing this compilation can resolve — a cross-document
	// target, an undeclared component, a pointer addressing no schema.
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
// each of its five reasons, a structural body genuinely sits beside the union
// (that is what routed the schema here rather than to a $ref-site union
// keeper), so the sentence that follows is always true of it. refSiteUnionWhy
// does not share this prefix, because at a $ref site it would not be
// (preserveUnionSiblingsAt's doc comment says why).
const coDeclaredUnionWhyPrefix = "oneOf/anyOf co-declared with structural keywords intersects with them, and "

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings.
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(c, ts, anchors, depth, s, pointer, hint, reason, why)
	}
	switch classifyUnionSiblings(c, s) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, anchors, depth, s, pointer, hint)
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
// names nothing this compilation resolves, or none when they all resolve. The
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

// oneOfAnyOfHasNull reports whether any oneOf/anyOf branch is a bare `type: null`
// schema, so its nullability lifts onto the enclosing union ref rather than
// degrading to an `any` variant.
func oneOfAnyOfHasNull(s *oas3.Schema) bool {
	if slices.ContainsFunc(s.GetOneOf(), isNullSchema) {
		return true
	}
	return slices.ContainsFunc(s.GetAnyOf(), isNullSchema)
}

// variantTypeFunc lowers one union branch to the type its variant refers to. It
// is a parameter so the distributed lowering can conjoin the sibling composition
// into each branch while sharing every other rule about union shape — null-branch
// stripping, variant hints, exclusivity, pointer derivation.
type variantTypeFunc func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, vhint string) ir.TypeRef

// buildUnion assembles the Union node for a oneOf/anyOf schema, attaching a
// discriminator when one is declared. common is already built by the caller
// (internNode), so buildUnion needs no hint of its own to build one.
//
// The variants are lowered before the discriminator, so its mapping resolves
// knowing what each variant names (readsThrough). A union the discriminator
// routes is WireTagged, an anyOf as much as a oneOf: the tag property's value
// names its variant (ir-design §4.4).
func buildUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, variantType variantTypeFunc) (ir.TypeDef, []ir.Diagnostic) {
	branches, key, exclusive := unionBranches(s)
	diags := preserveUnusedCombinator(c, &common.Unmodeled, s, key, pointer)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if isNullSchema(b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vh := branchHint(c.Bindings(), b, i)
		vptr := pointer + ids.Ptr(key, strconv.Itoa(i))
		t := variantType(b, vptr, vh)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(decoratedBranchHint(c, ts, b, t, vh)),
			Type: t,
		})
	}
	u := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
	}
	disc, discDiags := lowerDiscriminator(c, ts, anchors, s, nil, variants, pointer)
	u.Discriminator = disc
	u.WireTagged = disc != nil
	return u, append(diags, discDiags...)
}

// variantTargets returns the set of types the variants name: the types a
// union's discriminator may route to, as pass.Validate holds its mapping. It is
// nil for none, which is a model's.
func variantTargets(variants []ir.Variant) map[ir.TypeID]bool {
	if len(variants) == 0 {
		return nil
	}
	targets := make(map[ir.TypeID]bool, len(variants))
	for _, v := range variants {
		targets[v.Type.Target] = true
	}
	return targets
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
// Where a structural sibling is written as well, classifyUnionSiblings reaches
// unionBothCombinators first and neither set is elected
// (lowerBesideUnmodeledUnion).
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

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across its variants: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (ir-design §4.3) alongside its branch, so the composition is
// carried on every variant rather than merged into one or dropped.
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, comp: allOfBranches(s, pointer), hint: hint, id: common.ID}
		def, unionDiags := buildUnion(c, ts, anchors, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, vhint string) ir.TypeRef {
				ref, variantDiags := composedVariant(c, ts, anchors, depth, body, b, vptr, vhint)
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
	hint   string
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
func composedVariant(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, body composedBody,
	b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, vhint string,
) (ir.TypeRef, []ir.Diagnostic) {
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(c, ts, anchors, depth, b, vptr, vhint)
	id := c.IDs().ComposedType(vptr)
	common := commonFor(c, id, vptr, compile.SubHint(body.hint, vhint))
	def, variantDiags := buildComposedVariant(c, ts, anchors, depth, body, branch.Target, common)
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
func buildComposedVariant(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, body composedBody, branch ir.TypeID, common ir.TypeCommon) (ir.TypeDef, []ir.Diagnostic) {
	cons, diags := ConstraintsOf(c, &common.Unmodeled, body.schema, body.comp.pointer)
	m := &ir.Model{TypeCommon: common, Constraints: cons}
	idx := propIndex(c, ts, m)
	diags = append(diags, fillAllOf(c, ts, anchors, depth, m, idx, body.comp)...)
	diags = append(diags, fillModelProperties(c, ts, anchors, depth, idx, body.schema, body.comp.pointer)...)
	requireMembers(m, compositionRequired(body.schema.GetRequired(), body.comp))
	diags = append(diags, fillAdditional(c, ts, anchors, depth, m, body.schema, body.comp.pointer, body.hint)...)
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

// branchHint derives a composition branch's naming hint from its $ref target's
// name, falling back to a positional hint for inline branches. It names both a
// union variant and an allOf branch, which reach it as the same question.
//
// A $ref branch's answer is also subSchemaHint's, and must stay so: an outside
// $ref can name the branch pointer, and hoistSubSchema names it by
// subSchemaHint where no lowering of the composition recorded this hint, so
// the branch takes one name whether or not its composition is lowered.
func branchHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], i int) string {
	return branchHintAt(refs, b, strconv.Itoa(i))
}

// branchHintAt is branchHint for a branch whose index is already a token, as the
// last one of its pointer is.
func branchHintAt(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], index string) string {
	// Only a true reference carries a target name: IsReference() is precisely
	// GetSchema().Ref != "" for a non-bool branch, so a schema whose Ref pointer is
	// set but empty (IsReference() false) suggests no name.
	if b != nil && b.IsReference() {
		if name := targetHint(refs, b); name != "" {
			return name
		}
	}
	return positionalBranchHint(index)
}

// decoratedBranchHint is the hint a union variant takes for b, which lowered
// to t: hint, unless b is written inline and t is a decorator (ir.Decorated)
// over the $ref b's allOf names, when it is the hint that $ref takes as a
// branch of its own (branchHint). 3.0 annotates a branch only by wrapping its
// $ref, so `{allOf: [{$ref: X}], description: …}` names its variant as 3.1's
// `{$ref: X, description: …}` does (GitHub #1018). The node t keeps hint: a
// decorator collapses into its reference wherever it is used.
func decoratedBranchHint(c lowering.Ctx, ts *compile.Types, b *oas3.JSONSchema[oas3.Referenceable], t ir.TypeRef, hint string) string {
	s := annotation.SchemaOf(b)
	if s == nil || isRefBranch(b) {
		return hint
	}
	// The flattened list, as b lowers: a $ref a nested wrapper holds names the
	// variant as it would written at the top.
	branches := allOfBranches(s, "").branches
	base := selectAllOfBase(c.Bindings(), branches)
	if base < 0 {
		return hint
	}
	if td, ok := ts.Node(t.Target); !ok || !decorates(td) {
		return hint
	}
	return cmp.Or(targetHint(c.Bindings(), branches[base].schema), hint)
}

// decorates reports whether td is a decorator (ir.Decorated).
func decorates(td ir.TypeDef) bool {
	_, ok := ir.Decorated(td)
	return ok
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a composition branch's pointer segment takes.
func isDecimalIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maxTargetHintHops bounds targetHint's walk along composition branches that
// hold a $ref: a loop of them, and any longer chain, stops there. An acyclic
// chain longer than the cap is legal, though no real document writes one: past
// it the walk keeps the name it has reached, so a union variant naming the
// chain's first branch can be named apart from the node it holds.
const maxTargetHintHops = 64

// targetHint returns the hint of the node a $ref resolves to, so a union
// variant or allOf branch is named as the type it holds is (GitHub #521).
//
// Each position on the way is read as subSchemaHint names it (ownHint); a
// composition branch holding a $ref moves the walk on to its target, keeping
// the last name found. The fragment is percent-decoded, then RFC 6901
// unescaped, so `#/components/schemas/Cat~1Dog` suggests "Cat/Dog" (GitHub
// #505); one spelling no pointer ends the walk with refHint's name. A position
// under /paths keeps ownHint's guess, which the declaration replaces on the
// node (GitHub #729).
func targetHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) string {
	hint := ""
	for range maxTargetHintHops {
		ref := b.GetRef().String()
		pointer, ok := resolve.FragmentPointer(ref)
		if !ok {
			return cmp.Or(refHint(ref), hint)
		}
		decl := refs.Declared(b)
		own, follow := ownHint(decl, pointer)
		hint = cmp.Or(own, hint)
		if !follow {
			return hint
		}
		b = decl
	}
	return hint
}

// refHint returns the name a reference suggests when its fragment spells no
// pointer — a whole-document URI, a $anchor, a fragment that decodes to bytes
// that are not UTF-8 (GitHub #520): what follows its last '/', percent-decoded
// when that decodes to UTF-8, so `./Fish%2DTank.yaml` suggests "Fish-Tank.yaml".
func refHint(ref string) string {
	name := ref
	if _, after, ok := strings.CutLast(ref, "/"); ok {
		name = after
	}
	if decoded, err := url.PathUnescape(name); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return name
}

// lowerDiscriminator lowers a schema's discriminator. Each mapping entry
// resolves to a target TypeID (a bare name implies
// #/components/schemas/<name>; nil stays nil, preserving infer-by-name
// semantics), and the tag is located via exactly one of Property /
// PropertyName / Index (ir-design §4.3). m is the allOf base model this
// discriminator is declared on, or nil for a oneOf/anyOf union, which has no
// single model and so is named only by PropertyName; otherwise the tag
// resolves to the declaring property's PropID, falling back to PropertyName
// if undeclared. variants are a union's, and nil for a model: what each names
// decides what the mapping names (readsThrough).
func lowerDiscriminator(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, m *ir.Model, variants []ir.Variant, pointer jsontext.Pointer) (*ir.Discriminator, []ir.Diagnostic) {
	d := s.GetDiscriminator()
	if d == nil {
		return nil, nil
	}
	variantTypes := variantTargets(variants)
	mapping, diags := discriminatorMapping(c, ts, anchors, d, variantTypes, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	defaultID, defaultDiags := discriminatorDefault(c, ts, anchors, d, variantTypes, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order. An entry that does not resolve yields one
// error diagnostic and is dropped — never a synthesized ID that nothing backs
// (issue #14). An all-dropped mapping collapses to nil, preserving infer-by-name
// semantics and a clean round-trip.
func discriminatorMapping(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, variantTypes map[ir.TypeID]bool, pointer jsontext.Pointer) (map[string]ir.TypeID, []ir.Diagnostic) {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make(map[string]ir.TypeID, m.Len())
	for tag, target := range m.All() {
		id, ok := resolveMappingTarget(c, ts, anchors, d, target, variantTypes)
		if !ok {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
				pointer+ids.Ptr("discriminator", "mapping", tag),
				"discriminator mapping %q references unresolved schema %q%s", tag, target,
				mappingWhy(c, d, target)))
			continue
		}
		out[tag] = id
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// discriminatorDefault resolves an OpenAPI 3.2 defaultMapping to its target ID,
// dropping it with one diagnostic when it does not resolve, as
// discriminatorMapping drops an entry.
func discriminatorDefault(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, variantTypes map[ir.TypeID]bool, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return "", nil
	}
	id, ok := resolveMappingTarget(c, ts, anchors, d, dm, variantTypes)
	if !ok {
		return "", []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr("discriminator", "defaultMapping"),
			"discriminator defaultMapping references unresolved schema %q%s", dm,
			mappingWhy(c, d, dm))}
	}
	return id, nil
}

// mappingWhy is the reason target, a value of d's mapping, reports when it
// resolves to no type: why it lands on no schema, or why the table never read
// it, as unresolvedWhy says for a $ref.
// A value with no '#' names a component, which the table never reads
// (bind.Table.Mapping), so neither its budget nor the merge bound is why.
func mappingWhy(c lowering.Ctx, d *oas3.Discriminator, target string) string {
	m, ok := c.Bindings().Mapping(d, target)
	switch {
	case ok:
		return hopWhy(m.Hop)
	case !strings.Contains(target, "#"):
		return ""
	}
	return unreadWhy(c.Bindings(), d)
}

// resolveMappingTarget resolves a mapping target, a mapping entry's or a
// defaultMapping's, to the ID of the schema it names: a declared component's
// bare name directly, and a reference as resolvePointer resolves a $ref at the
// position whose type it names (typePosition), promising the schema there.
//
// The parser never reads a mapping value
// (docs/parser-library.md#mapping-unresolved); the compiler reads one as a
// $ref written at the discriminator (resolve.Scope.MappingTarget). Hoisting
// what it lands on gives the target one pointer-derived ID in either
// declaration order (GitHub #530). variantTypes is as readsThrough takes it.
func resolveMappingTarget(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, target string, variantTypes map[ir.TypeID]bool) (ir.TypeID, bool) {
	if c.DeclaresSchema(target) {
		return componentIDByName(target), true
	}
	at, decl, ok := c.RefScope().MappingTarget(d, target)
	if !ok {
		return "", false
	}
	at, decl = typePosition(c, at, decl, variantTypes)
	return resolveAt(c, ts, anchors, at, decl)
}

// maxTypePositionHops bounds how many $ref positions typePosition reads
// through (styleguide bounded-everything rule). A loop of them, and any longer
// chain, stops at the position the bound reached, so the mapping names that
// position's alias.
const maxTypePositionHops = 64

// typePosition returns the position, in whichever document, whose type a
// mapping target naming at names, and the schema held there, starting from
// decl, unread at a source component. It is at unless the position reads
// through (readsThrough): a $ref holding nothing an alias would only uses its
// target's type, so it names the position the $ref does, as a union variant
// there does (GitHub #758). A keyword beside a property's $ref stops it
// (GitHub #764), and so does a $ref landing on nothing a type is read at,
// whose own position, lowered as an alias that reports why, is named.
func typePosition(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable],
	variantTypes map[ir.TypeID]bool,
) (bind.Position, *oas3.JSONSchema[oas3.Referenceable]) {
	if sourceComponent(at) {
		// Most targets name a component, and resolve.Scope.ComponentRef answers
		// for one by name, so nothing is fetched for it (see mappingTagsFor).
		return at, nil
	}
	scope := c.RefScope()
	for range maxTypePositionHops {
		if !readsThrough(c, at, decl, variantTypes) {
			break
		}
		next, ok := scope.Target(decl)
		target := scope.Bindings.Declared(decl)
		if !ok || target == nil && !sourceComponent(next) {
			break
		}
		at, decl = next, target
	}
	return at, decl
}

// sourceComponent reports whether at is one of the source's components, which
// is named by name rather than fetched where it is declared.
func sourceComponent(at bind.Position) bool {
	_, named := ids.ComponentSchemaName(at.Pointer)
	return named && at.Doc == bind.Source
}

// readsThrough reports whether a mapping target naming at, where decl is
// declared, names the position decl's $ref names instead (typePosition): not at
// a component, nor where a keyword beside the $ref gives the position an alias
// (refSiteHomesNothing).
//
// Nor where the node at pointer is in variantTypes, the types a union's
// variants name, nil for a model. That variant is a $ref to the position, which
// hoisted an alias there as a $ref to any position does, and the mapping must
// name what the variant names. Read through, it named a type no variant names
// (GitHub #758).
func readsThrough(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable],
	variantTypes map[ir.TypeID]bool,
) bool {
	_, named := ids.ComponentSchemaName(at.Pointer)
	if named || ids.ComponentSchemaNamedEmpty(at.Pointer) || decl == nil {
		return false
	}
	s := decl.GetSchema()
	if !resolve.IsRefSite(decl, s) || !refSiteHomesNothing(s) {
		return false
	}
	if len(variantTypes) == 0 {
		return true
	}
	in, ok := lowering.InDocument(c, at.Doc)
	return !ok || !variantTypes[in.IDs().ForPointer(at.Pointer)]
}

// componentIDByName is the ID a mapping target naming a declared component
// resolves to: its pointer's in the source, whose components a bare name names,
// so a component keyed "" gets the anonymous ID it is interned under rather
// than a named one nothing backs.
func componentIDByName(name string) ir.TypeID {
	return ids.Document{}.ForPointer(ids.Ptr("components", "schemas", name))
}

// mappingTargetID resolves a discriminator mapping target, a bare schema name
// or a $ref string, to the TypeID of the schema it names. A bare name (even one
// containing '/') naming a declared component resolves in any source order,
// since every declared name is recorded before lowering begins. Otherwise the
// target must be a $ref, read at the position whose type it names
// (typePosition), in whichever document: a source component, or any other
// position by the ID its pointer derives there, whose node resolveMappingTarget
// promises. It reads no registry, so no answer turns on which schema lowered
// first. scope is c's.
func mappingTargetID(c lowering.Ctx, scope resolve.Scope, d *oas3.Discriminator, target string) (ir.TypeID, bool) {
	if scope.Declares(target) {
		return componentIDByName(target), true
	}
	named, decl, ok := scope.MappingTarget(d, target)
	if !ok {
		return "", false
	}
	// No variant types: a tag asks whether the mapping reaches the subtype, so
	// it reads through whatever a union's variants name (readsThrough). Each
	// subtype asks it of every entry, so it is read once (Ctx.TypePosition).
	at := c.TypePosition(named, func() bind.Position {
		position, _ := typePosition(c, named, decl, nil)
		return position
	})
	in, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false
	}
	if in.IDs() == (ids.Document{}) {
		if id, resolved, handled := scope.ComponentRef(at.Pointer); handled {
			return id, resolved
		}
	}
	return in.IDs().ForPointer(at.Pointer), true
}

// propIDByName returns the PropID of the model property with the given source
// name, if the model declares it. m is nil-safe: lowerDiscriminator's
// oneOf/anyOf union case has no model to search, only a tag name.
func propIDByName(m *ir.Model, name string) (ir.PropID, bool) {
	if m == nil {
		return "", false
	}
	for i := range m.Properties {
		if m.Properties[i].Name.Source == name {
			return m.Properties[i].ID, true
		}
	}
	return "", false
}

// lowerEnum hoists a schema with `enum` as a closed Enum. A heterogeneous or
// non-scalar member set becomes a Union of Literals over the members, `null`
// included, with an info diagnostic; an empty list takes emptyEnum.
//
// A member count past the budget lowers as the top type with an error
// diagnostic, checked first because both paths cost per member, amplifying a
// small source (GitHub #75).
//
// A listed `null` member is stripped onto the reference's Nullable bit
// (ir-design §3.3) when schemaAdmitsNull holds, the predicate every Nullable
// site shares. A sibling `type: string` forbids that null, so it stays and the
// set becomes a Union.
func lowerEnum(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(s.GetEnum()) == 0 {
			def, emptyDiags := emptyEnum(c, s, common, pointer)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(s.GetEnum()); c.Limits.EnumMembersExceeded(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, pointer,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers))
			return &ir.Any{TypeCommon: common}
		}
		diags = append(diags, enumRepeats(c, s.GetEnum(), pointer)...)
		members, memberPrim, ok := enumMembers(s.GetEnum(), schemaAdmitsNull(c, s), c.IDs(), pointer)
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, s, common, pointer, hint)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
	return id, diags
}

// emptyEnum lowers `enum: []`, a value space that accepts no instance, as a
// closed Enum with no member and one warning. The IR has no bottom type, but a
// closed Enum admits only its members, so this is exact.
//
// It avoids enumAsUnion, which would yield a Union with no variants: a node
// nothing rejects and no reader can act on (GitHub #318).
//
// ValueType is the declared scalar type, else the top type. Whether a
// reference admits null is not settled here: schemaAdmitsNull reads the type
// keyword at each use site (GitHub #288).
func emptyEnum(c lowering.Ctx, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, pointer,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  enumValueType(s, ir.PrimAny),
		Closed:     true,
	}, diags
}

// enumMembers converts enum nodes into scalar members, reporting ok=false for a
// non-scalar member, members heterogeneous in kind, or a set that keeps none
// (an all-null set degrades rather than becoming a memberless Enum).
//
// dropNull skips `null` members, for a schema whose nullability the enclosing
// reference carries (see lowerEnum). Kind agreement is read off the members
// kept. The PrimKind is the one every kept member maps to, meaningful only when
// ok.
//
// Each member's ID is keyed by its value under the enum at pointer, and a
// repeated value is kept, its ID numbered by occurrence; enumRepeats reports it.
func enumMembers(nodes []values.Value, dropNull bool, d ids.Document, pointer jsontext.Pointer) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	occurrences := make(map[string]int, len(nodes))
	var kind ir.ValueKind
	var prim ir.PrimKind
	for _, node := range nodes {
		val, err := value.FromNode(node)
		if err != nil {
			return nil, "", false
		}
		if dropNull && val.Kind == ir.ValueNull {
			continue
		}
		memberPrim, text, admissible := enumMemberForm(val)
		key, keyed := enumMemberKey(val)
		if !admissible || !keyed {
			return nil, "", false
		}
		if len(members) == 0 {
			kind, prim = val.Kind, memberPrim
		} else if val.Kind != kind {
			return nil, "", false
		}
		occurrences[key]++
		members = append(members, ir.EnumMember{
			ID:    d.EnumMember(pointer, key, occurrences[key]),
			Name:  compile.NamingFor(text),
			Value: val,
		})
	}
	if len(members) == 0 {
		return nil, "", false
	}
	return members, prim, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, hint string) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	nodes := s.GetEnum()
	variants := make([]ir.Variant, 0, len(nodes))
	for i, node := range nodes {
		vh := compile.SubHint(hint, strconv.Itoa(i))
		lptr := pointer + ids.Ptr("enum", strconv.Itoa(i))
		litID, litDiags := hoistLiteral(c, ts, node, lptr, vh)
		diags = append(diags, litDiags...)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(vh),
			Type: ir.TypeRef{Target: litID},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  true,
	}, diags
}

// hoistLiteral hoists a single node as a Literal type at its own pointer, or
// falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's ids.ForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
//
// It returns the interned ID, plus the diagnostic an unconvertible node
// produces — none when the node converts.
func hoistLiteral(c lowering.Ctx, ts *compile.Types, node values.Value, pointer jsontext.Pointer, hint string,
) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error()))
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val}
	})
	return id, diags
}

// enumValueType picks an Enum's ValueType from the schema's declared scalar
// type, falling back to the primitive its members classified to. It does not
// re-derive that primitive from a ValueKind: enumMemberForm decided it once,
// for the same members, so there is nothing here to disagree with.
func enumValueType(s *oas3.Schema, memberPrim ir.PrimKind) ir.PrimKind {
	if types := effectiveTypes(s); len(types) == 1 {
		switch types[0] {
		case oas3.SchemaTypeString:
			return ir.PrimString
		case oas3.SchemaTypeInteger:
			return ir.PrimInteger
		case oas3.SchemaTypeNumber:
			return ir.PrimNumber
		case oas3.SchemaTypeBoolean:
			return ir.PrimBool
		}
	}
	return memberPrim
}

// enumMemberForm classifies one lowered value as an Enum member: the PrimKind
// its kind declares and the literal text that names the member. ok=false means
// the kind has no place in an Enum, and the caller degrades the whole enum to a
// Union of Literals with a diagnostic.
//
// Its switch has one arm per ir.ValueKind, as enumMemberKey's does; the
// exhaustive linter fails when ir adds a kind either lacks. The default arm
// refuses a kind rather than assert a type the source never wrote.
func enumMemberForm(v ir.Value) (ir.PrimKind, string, bool) {
	switch v.Kind {
	case ir.ValueString:
		return ir.PrimString, v.Str, true
	case ir.ValueSymbol:
		// An interned symbol has no primitive of its own; PrimString is the form
		// it takes on a JSON wire. Unreachable from OpenAPI, whose values come
		// from YAML nodes, but ValueKind is IR-wide and this is its enum home.
		return ir.PrimString, v.Str, true
	case ir.ValueNumber:
		return ir.PrimNumber, string(v.Num), true
	case ir.ValueBool:
		return ir.PrimBool, strconv.FormatBool(v.Bool), true
	case ir.ValueBytes:
		return ir.PrimBytes, base64.StdEncoding.EncodeToString(v.Bytes), true
	case ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		// Composite, reference and null values are not enum members: none has a
		// primitive to declare or a literal text form to name a member by.
		return "", "", false
	default:
		return "", "", false
	}
}
