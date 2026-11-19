// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// LowerComponentSchemas interns every named component schema schemas yields,
// the statements of the source's components/schemas in source order. It is the
// entry Compile's run() calls before any operation lowering.
//
// ctx bounds the walk in time: a document declares as many components as it
// likes, so this loop is one of the places a compile does work proportional to
// nothing the compiler chose. Cancellation stops it between components and
// returns what was lowered so far; the caller — run — sees ctx.Err() at the
// phase boundary immediately after and refuses the document there, so a partial
// registry never becomes a Document.
func LowerComponentSchemas(ctx context.Context, loweringContext lowering.Ctx, ts *compile.Types,
	schemas iter.Seq2[string, *statement.Statement],
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	// The declared-name index the $ref and discriminator-mapping resolutions read
	// arrives with the context (lowering.Loaded), so a component declared later
	// in the document is already a valid target here regardless of source order.
	for name, js := range schemas {
		if ctx.Err() != nil {
			return diags
		}
		diags = append(diags, lowerComponentSchema(loweringContext, ts, js, ids.Ptr("components", "schemas", name))...)
	}
	return diags
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func lowerComponentSchema(loweringContext lowering.Ctx, ts *compile.Types, js *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	schemaPositionAnnotationSources := annotation.At(js)
	ref, diags := Ref(loweringContext, ts, TopLevelDepth, js, pointer)
	// A body that lowered to the component's own node needs no alias, and its
	// annotations were attached where it was lowered.
	if LoweredToOwnNode(loweringContext, pointer, ref) {
		return diags
	}
	// This node is the first the pointer owns, so the annotations schemaBody
	// had nowhere to put now have a home, which its build gives them.
	var annotated finished
	var fin finisher
	if stated(schemaPositionAnnotationSources.Node) {
		fin = annotated.on(func(td ir.TypeDef) []ir.Diagnostic {
			return attachDeclaredAnnotations(loweringContext, ts, schemaPositionAnnotationSources.Node, pointer, ref, td)
		})
	}
	diags = append(diags, internOwnNode(loweringContext, ts, js, pointer, ref, fin)...)
	return append(diags, annotated.diags...)
}

// TopLevelDepth is the nesting a schema position outside the walk starts at.
// The walk counts its own frames, so every entry point into it begins at zero;
// naming it keeps a bare 0 out of the call sites that only pass it through.
const TopLevelDepth = 0

// maxSchemaDepth caps schema-lowering recursion (styleguide bounded-recursion
// rule). Interning by pointer is what terminates recursive and diamond schemas;
// this bound only guards pathologically deep inline nesting. It counts from where
// the lowering owning a position enters, and from TopLevelDepth for a node only
// a $ref's promise builds (hoistSubSchema): a $ref never lowers what it names
// (resolvePointer), so a position meets the bound at one depth, whichever
// reaches it first (GitHub #748).
const maxSchemaDepth = 256

// Ref is the schema entry point for a position no carrier holds — items, a
// catch-all, a media type, a union branch — and CarriedRef the one for a
// property, header or parameter. Either yields a TypeRef into the type
// registry, normalizes the two nullability dialects onto the single IR bit, and
// never lowers a $ref target from the reference site.
//
// It defaults to annotation.HomeOwnNode deliberately: a position added later
// inherits the lossless behaviour, and only a caller that can prove it already
// carries the annotations opts out through CarriedRef.
func Ref(loweringContext lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(loweringContext, ts, depth, js, pointer, annotation.HomeOwnNode)
}

// CarriedRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func CarriedRef(loweringContext lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(loweringContext, ts, depth, js, pointer, annotation.HomeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func schemaRefHomed(loweringContext lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer, home annotation.Home,
) (ir.TypeRef, []ir.Diagnostic) {
	// depth counts the active frames of this function, which is where every
	// recursive descent re-enters. Incrementing on entry and passing the result
	// down is the parameter form of a counter that used to live on a shared
	// struct, back when one held the whole compile.
	if js == nil || js.Truth == statement.Unwritten {
		// None, or a null, which states nothing: an entry written so — a
		// property, a branch, a component, items — lowers as the empty schema.
		// A keyword whose absence differs from the empty schema reads a null as
		// left out before it gets here (docs/compilers/openapi.md#null-schema).
		// It nests nothing, so the depth bound reports none at its pointer, as
		// a tuple's absent rest would otherwise draw.
		return ts.PrimitiveRef(ir.PrimitiveAny), nil
	}
	depth++
	if depth > maxSchemaDepth {
		return ts.PrimitiveRef(ir.PrimitiveAny), []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)}
	}
	switch js.Truth {
	case statement.Unread:
		// A mapping the parser read as false: the load phase decodes it again
		// unless it lies past typed.MaxMends
		// (docs/parser-library.md#failure-beneath-itself), so it says nothing of
		// the values there.
		return ts.PrimitiveRef(ir.PrimitiveAny), []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema read as false, as the parser fails to decode a keyword beneath it under its own name "+
				"and the load phase decodes no more than %d such schemas one below another; lowered as any; "+
				"moving the schemas below it into a component reads them",
			typed.MaxMends)}
	case statement.Nothing:
		id, diags := falseSchema(loweringContext, ts, pointer)
		return ir.TypeRef{Target: id}, diags
	case statement.Anything:
		return ts.PrimitiveRef(ir.PrimitiveAny), nil
	default:
		// Stated: a schema object, which a reference site writes too.
	}
	diags := mergedPastBound(loweringContext, js)
	if js.Ref != nil {
		ref, refDiags := refSiteRef(loweringContext, ts, js, js.Schema, pointer, home)
		return ref, append(diags, refDiags...)
	}
	ref, bodyDiags := schemaBody(loweringContext, ts, depth, js.Schema, pointer, home)
	return ref, append(diags, bodyDiags...)
}

// mergedPastOf keys whether mergedPastBound has reported one schema.
type mergedPastOf struct {
	schema *statement.Schema
}

// mergedPastBound reports each schema object of st, its own and its inline
// allOf entries', whose `<<` merges run past nodeview.MergeDepthLimit
// (pastBoundOnce).
func mergedPastBound(loweringContext lowering.Ctx, st *statement.Statement) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if diagnostic, first := pastBoundOnce(loweringContext, st.Schema); first {
		diags = append(diags, diagnostic)
	}
	for _, allOfConjunct := range st.Entries {
		if diagnostic, first := pastBoundOnce(loweringContext, allOfConjunct.Schema); first {
			diags = append(diags, diagnostic)
		}
	}
	return diags
}

// pastBoundOnce returns the report of schema, and true the first time a run asks
// for a Truncated schema: a warning at its own pointer that it is read from the
// keys within its merges alone, so a keyword merged in past them, or one a
// source past them outranks, is not read (docs/compilers/openapi.md#references).
func pastBoundOnce(loweringContext lowering.Ctx, schema *statement.Schema) (ir.Diagnostic, bool) {
	if !schema.Truncated {
		return ir.Diagnostic{}, false
	}
	reported := lowering.ReadOncePerRun(loweringContext, mergedPastOf{schema}, func() *bool { return new(bool) })
	if *reported {
		return ir.Diagnostic{}, false
	}
	*reported = true
	there, _ := lowering.InDocument(loweringContext, schema.At.Doc)
	return there.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, schema.At.Pointer,
		"the schema merges through `<<` deeper than %d levels and is read from the keys within them; "+
			"a keyword merged in past them, or one a source past them outranks, is not read",
		nodeview.MergeDepthLimit), true
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing), returning the interned ID and the one info diagnostic that
// announces it.
func falseSchema(loweringContext lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(loweringContext, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		// Kept verbatim beside the approximation, which is what
		// docs/ir/boundaries.md#degraded-constructs asks of every degraded
		// lowering that loses something, and what the composition half of this
		// same rule already did. Without it a `false` schema and a schema that
		// merely wrote `additionalProperties: false` are the same node, so
		// nothing downstream can tell "no instance" from "the empty object" —
		// the diagnostic says which one it was, but a diagnostic is not part of
		// the document.
		//
		// The key names the position rather than a keyword, because a boolean
		// schema writes none. Nothing can collide with it: a schema that is a
		// boolean has no other keywords to preserve.
		annotation.PreserveInto(&common.Unmodeled, "openapi:schema",
			ir.RawValue("false"), ir.ReasonDegradedLowering, loweringContext.Place().At(pointer))

		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.FalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model "+
				"with the schema kept verbatim under Unmodeled"))
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}, 0
	}, nil)
	return id, diags
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef, the
// schema's declaration finishing whatever node the lowering builds at pointer
// (declared), or the home hoisted here where it lowered to a node it does not
// own. Its one caller is schemaRefHomed, the body of Ref and CarriedRef, for a
// position that is no $ref site (refSiteRef takes those). A rule about the
// declaration itself is checked here, above the dispatch that picks what it
// lowers to (missingArrayItems). The findings keep their order: the body's, the
// home's, what the position keeps beside it.
func schemaBody(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, home annotation.Home,
) (ir.TypeRef, []ir.Diagnostic) {
	diags := missingArrayItemsConjoined(loweringContext, schema, pointer)
	decl := &declaring{c: loweringContext, ts: ts, s: schema, target: ir.TypeRef{Target: loweringContext.IDs().ForPointer(pointer)}, pointer: pointer, home: home}
	target, bodyDiags := lowerSchemaBody(loweringContext, ts, depth, schema, pointer, decl.finish, stand{})
	diags = append(diags, bodyDiags...)
	ref := target
	// A body's census ran where its node was built, the only thing that can
	// answer it. One lowering to a reference built none, so it takes the census
	// of a position lowered to one, and its verdict homes an alias.
	if !decl.ran && homesDeclaration(loweringContext, schema, target, pointer, home,
		keepsAtOutcome(loweringContext, schema, target, pointer).holdsAny() || declaresScoped(loweringContext, declarationFoldedBranches(loweringContext, schema, target, pointer))) {
		var homeDiags []ir.Diagnostic
		decl.target = target
		ref, homeDiags = hoistUseHome(loweringContext, ts, schema, target, pointer, decl.finish)
		diags = append(diags, homeDiags...)
	}
	return ref, slices.Concat(diags, decl.diags, decl.kept)
}

// declaring is a body's declaration s, written at pointer under home, on its
// way to the node the position lowered to target stands at: what finishes it
// there (finish), and what it reported.
type declaring struct {
	c       lowering.Ctx
	ts      *compile.Types
	s       *statement.Schema
	pointer jsontext.Pointer
	target  ir.TypeRef
	diags   []ir.Diagnostic
	kept    []ir.Diagnostic
	home    annotation.Home
	ran     bool
}

// finish writes the declaration and what the position keeps beside it on td,
// in hand (declarationOn, keptOn).
func (d *declaring) finish(td ir.TypeDef, _ keySet, held *principalKept) {
	d.ran = true
	d.diags = declarationOn(d.c, d.ts, d.s, d.target, d.pointer, d.home, td)
	d.kept = keptOn(d.c, d.ts, d.s, d.target, d.pointer, td, held)
}

// keptOn keeps on td, in hand, the node the position schema, written at
// pointer, lowered to target, stands at, what it keeps there: its census of a
// lowering to a reference and the branches its refinement left
// (keepsAtOutcome). A carrier keeps them where the position owns no node
// (PreserveRefSiteKeywords).
func keptOn(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer, td ir.TypeDef,
	held *principalKept,
) []ir.Diagnostic {
	keeps := keepsAtOutcome(loweringContext, schema, target, pointer)
	if !keeps.holdsAny() {
		return nil
	}
	var standing ir.TypeDef
	if LoweredToOwnNode(loweringContext, pointer, target) {
		standing = td
	}
	return keeps.record(loweringContext, ts, &td.Common().Unmodeled, schema, pointer, standing, held)
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
//
// fin finishes the node the body builds at pointer, and principal the node a
// refinement's principal builds at its own (lower).
func lowerSchemaBody(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer,
	fin finisher, st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	if at, _, expanded := dynamicExpansion(loweringContext, schema, pointer); expanded {
		// The expansion settled that the target resolves, by what resolveAt
		// reads in its own document (resolvesAt), so nothing falls through: that
		// would drop the reference unrecorded, as recordUnexpandedDynamicRef
		// keeps only one that does not expand, where an empty target fails
		// irverify.
		target, _ := resolveAt(loweringContext, ts, at, loweringContext.Statements().At(at))
		ref, nullDiags := schemaNullRef(loweringContext, target, schema, siteAt(pointer))
		return ref, append([]ir.Diagnostic{dynamicExpanded(loweringContext, schema, pointer, target)}, nullDiags...)
	}
	if declaresUnion(schema) && (st.of != nil || !refinesToBranch(loweringContext, schema, pointer)) {
		if hasUnionSiblings(loweringContext, schema) {
			id, unionDiags := lowerCoDeclaredUnion(loweringContext, ts, depth, schema, pointer, fin, st)
			ref, nullDiags := schemaNullRef(loweringContext, id, schema, siteAt(pointer))
			return ref, append(unionDiags, nullDiags...)
		}
		return lowerOneOfAnyOf(loweringContext, ts, depth, schema, pointer, fin, st)
	}
	unionAt, how, ok := branchUnion(loweringContext, schema, pointer)
	if ok {
		lowerUnion := lowerDistributedUnion
		if how == unionRoutedSubtype {
			lowerUnion = lowerSubtypeUnion
		}
		id, unionDiags := lowerUnion(loweringContext, ts, depth, schema, unionAt, pointer, fin)
		ref, nullDiags := schemaNullRef(loweringContext, id, schema, siteAt(pointer))
		return ref, append(unionDiags, nullDiags...)
	}
	if how == unionHierarchyBase || how == unionNoLiveBranch || how == unionSharesNoKind || how == unionPassedApart ||
		how == unionPassedNull {
		// The union stays with its branch, and the discriminator standing with it
		// on the model, held to its routes as one beside the union is.
		fin = discriminatorBesideUnion(loweringContext, ts, schema, unionAt.holder, pointer).then(fin)
	}
	id, bodyDiags := lower(loweringContext, ts, depth, schema, pointer, fin, st)
	if how == unionNoLiveBranch {
		bodyDiags = append(bodyDiags, diagDeadEntryUnion(loweringContext, schema, unionAt, pointer)...)
	}
	ref, nullDiags := schemaNullRef(loweringContext, id, schema, siteAt(pointer))
	return ref, append(bodyDiags, nullDiags...)
}

// refinesToBranch reports whether schema, written at pointer, refines to one of its
// allOf branches, which then decides the position, a union beside the allOf
// being one of the conjuncts it refines (docs/compilers/openapi.md#allof-refinement).
func refinesToBranch(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) bool {
	refinement, refines := refinementOf(loweringContext, schema, pointer)
	return refines && refinement.principal != nil
}

// allOfComposes reports whether schema writes an allOf that composes rather
// than refines, the one a union beside it distributes across or is kept beside.
func allOfComposes(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !writesConjuncts(schema) {
		return false
	}
	return !decisionOf(loweringContext, schema).refines
}

// lower interns the inline schema at pointer and returns its TypeID. An allOf
// that refines decides the position (docs/compilers/openapi.md#allof-refinement):
// its principal, or where no branch is one, the keywords beside it as if no allOf
// were written, an enum or a const there being one of its conjuncts rather than
// its rival. A value set an inline principal writes stands at the position, as
// one written beside the allOf does, and either is typed by every conjunct
// (valueSetSite.valueType). Any other schema lowers by its election (lowerElected).
// fin finishes the node built at pointer, principal an inline principal's.
func lower(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	if st.of != nil {
		// schema is the inline principal of the refinement st stands for, a
		// wrapper's allOf being the position's conjuncts and no rival of its
		// keywords (principalDispatch).
		return lowerElected(loweringContext, ts, depth, schema, pointer, dispatchOf(schema).besideAllOf(), fin, st)
	}
	dispatch, elected := electionOf(loweringContext, schema, pointer)
	refinement, refines := refinementOf(loweringContext, schema, pointer)
	if !refines {
		return lowerElected(loweringContext, ts, depth, schema, pointer, dispatch, fin, st)
	}
	// The node the position owns is reported on by the visit building it, as
	// its census is (keepingUnread); a node it does not own, by each visit.
	var disjoint finished
	fin = finisher(func(td ir.TypeDef, _ keySet, held *principalKept) {
		disjoint.ran = true
		disjoint.diags = refinedDisjoint(loweringContext, schema, refinement, td, held)
	}).then(fin)
	reported := func(id ir.TypeID, diags []ir.Diagnostic) (ir.TypeID, []ir.Diagnostic) {
		if !disjoint.ran && !LoweredToOwnNode(loweringContext, pointer, ir.TypeRef{Target: id}) {
			disjoint.diags = refinedDisjoint(loweringContext, schema, refinement, nil, nil)
		}
		return id, append(diags, disjoint.diags...)
	}
	if allOfBranch, ok := valueSetPrincipal(refinement); ok {
		resolveBeaten(loweringContext, ts, refinement)
		return reported(lowerPrincipalValueSet(loweringContext, ts, schema, allOfBranch, pointer, fin))
	}
	if elected {
		resolveBeaten(loweringContext, ts, refinement)
		return reported(lowerElected(loweringContext, ts, depth, schema, pointer, dispatch, fin, st))
	}
	return reported(lowerPrincipal(loweringContext, ts, depth, schema, pointer, refinement, fin))
}

// refinedDisjoint reports the refinement refinementValue of schema whose union
// beside the allOf shares no kind with the rest (unionAside), or whose
// conjuncts share no value and lowered to the value set elected before them or
// to what the keywords beside the allOf say (contradictionShape), at schema, as
// a composition reports conjuncts sharing no value, standing being the node the
// position owns, nil where it owns none, and held how its builder names it
// (refinedShape).
func refinedDisjoint(loweringContext lowering.Ctx, schema *statement.Schema, refinementValue refinement, standing ir.TypeDef, held *principalKept) []ir.Diagnostic {
	shape := refinedShape(loweringContext, schema, refinementValue.comp, refinementValue.principal, standing, held)
	switch {
	case unionAside(loweringContext, schema):
	case refinementValue.contradicts:
		shape = contradictionShape(refinementValue, schema)
	default:
		return nil
	}
	comp := refinementValue.comp
	comp.disjoint = disjointAtoms(loweringContext, schema, refinedAtoms(loweringContext, schema, refinementValue))
	if len(comp.disjoint) == 0 {
		comp.disjoint = pairApart(loweringContext, schema)
	}
	if len(comp.disjoint) == 0 && refinementValue.contradicts {
		comp.disjoint = conjunctsApart(loweringContext, schema)
	}
	return diagDisjointConjuncts(loweringContext, comp, shape)
}

// electionOf returns the election lower lowers schema by, written at pointer,
// and false where an allOf refining it has a principal, which decides the
// position instead: the keywords beside the allOf, as if none were written, for
// one refining with none.
func electionOf(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (dispatch, bool) {
	refinement, refines := refinementOf(loweringContext, schema, pointer)
	switch {
	case !refines:
		return dispatchOf(schema), true
	case refinement.principal != nil:
		return dispatch{}, false
	default:
		return dispatchOf(schema).besideAllOf(), true
	}
}

// lowersTypeArray reports whether schema, written at pointer, lowers by its
// type list (lowerUnion): it lists more than one type, and lower elects no
// family. Nothing lowerSchemaBody tries first takes such a schema: a type is a
// shape, so no $dynamicRef expands and no union collapses beside it, and a
// union beside it or on an allOf branch distributes only over the allOf, which
// the election elects.
func lowersTypeArray(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) bool {
	if len(effectiveTypes(schema)) < 2 {
		return false
	}
	dispatch, elected := electionOf(loweringContext, schema, pointer)
	return elected && dispatch.won == ""
}

// lowerElected lowers schema, at pointer, by the family dispatchValue elects,
// which takes precedence over the structural type, else by its effective
// (null-stripped) type set. The families are conjoined where a schema writes
// more than one, so electing a winner is a degradation
// (docs/compilers/openapi.md#competing-keywords): the ones passed over are kept
// verbatim beside it (recordSkippedFamilies). A keyword the elected lowering
// never reads, such as `type: string` beside an allOf, goes through the census
// (keepingUnread), which keeps what the build did not read. fin finishes the
// node built at pointer after the census.
func lowerElected(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, dispatchValue dispatch, fin finisher,
	st stand,
) (ir.TypeID, []ir.Diagnostic) {
	census := func(build func(fin finisher) (ir.TypeID, []ir.Diagnostic)) (ir.TypeID, []ir.Diagnostic) {
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := build(fin)
			return id, 0, diags
		})
	}
	switch dispatchValue.won {
	case "const":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerConst(loweringContext, ts, standingValueSet(schema, pointer, st).finishedBy(fin))
		})
	case "enum":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerEnum(loweringContext, ts, standingValueSet(schema, pointer, st).finishedBy(fin))
		})
	case "allOf":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerAllOf(loweringContext, ts, depth, schema, pointer, !slices.Contains(dispatchValue.said, "discriminator"), dispatchValue.mixin, fin)
		})
	}
	// won is "": the schema declares no family, or only an allOf that refines
	// with no branch deciding what the position is, so the type set decides.
	types := effectiveTypes(schema)
	switch {
	case len(types) > 1:
		// A multi-typed schema lowers one variant per declared type, each from
		// this same schema, so the homes are the variants', not the Union's, and
		// the type set itself is what built them.
		var census finished
		id, diags := lowerUnion(loweringContext, ts, depth, schema, pointer, typeArray{types: types},
			keepUnreadAcross(loweringContext, ts, schema, nodeShape(ir.KindUnion), keyBit("type"), pointer, st, &census, fin), st)
		return id, append(diags, census.diags...)
	case len(types) == 1:
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerTyped(loweringContext, ts, depth, schema, pointer, types[0], fin, st)
		})
	case writesType(schema):
		// Every type schema names is null, so its value space is null and nothing
		// else, which is no schemaless position's (docs/compilers/openapi.md#null-alone).
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerNullAlone(loweringContext, ts, standingValueSet(schema, pointer, st).finishedBy(fin)), nil
		})
	default:
		return lowerUntyped(loweringContext, ts, depth, schema, pointer, dispatchValue, fin, st)
	}
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form,
// fin finishing the node it builds at pointer, and returns what a lowering to
// a shared primitive read.
func lowerTyped(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, kind statement.JSONSchemaTypeName,
	fin finisher, st stand,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	switch kind {
	case statement.KindObject:
		// An object composes, so no refinement's principal reaches here.
		id, diags := lowerModel(loweringContext, ts, depth, schema, pointer, true /* discriminated */, kindObject, fin)
		return id, 0, diags
	case statement.KindArray:
		id, diags := lowerArray(loweringContext, ts, depth, schema, pointer, st.pointer(pointer), st.foldedBranches(loweringContext, schema, pointer), fin)
		return id, 0, diags
	default:
		return scalarTypeID(loweringContext, ts, depth, schema, kind, pointer, fin, st)
	}
}

// lowerUntyped lowers schema, which writes no kind and no family lower() elects
// first, by the election dispatchValue, as one writing the type its shape
// keywords speak of (untypedKinds), its nodes marked where the kind-inference
// policy read the kind, else to the top type. The keywords the node cannot hold
// are kept beside it (keepingUnread), as for a schema writing that type.
func lowerUntyped(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, dispatchValue dispatch,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	kinds, inferred := untypedKinds(loweringContext, schema)
	var mark finisher
	if inferred {
		mark = markKindInferred
	}
	switch kinds {
	case 0:
		return lowerUndecided(loweringContext, ts, depth, schema, pointer, dispatchValue, fin, st)
	case kindObject:
		// The node asserts an object, as the bounds beside it read it
		// (boundKinds): one an object does not take is kept.
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := lowerModel(loweringContext, ts, depth, schema, pointer, true /* discriminated */, boundKinds(loweringContext, schema), mark.then(fin))
			return id, 0, diags
		})
	case kindArray:
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := lowerArray(loweringContext, ts, depth, schema, pointer, st.pointer(pointer), st.foldedBranches(loweringContext, schema, pointer), mark.then(fin))
			return id, 0, diags
		})
	default:
		return lowerUntypedUnion(loweringContext, ts, depth, schema, pointer, inferredKinds, inferred, fin, st)
	}
}

// familyOrder is the order lower() tries the keyword families that outrank the
// structural type, and dispatchOf is the sole reader of it. First match wins, so
// a schema declaring more than one of them lowers as the first and passes over
// the rest — which is why the two are derived together rather than separately.
var familyOrder = []string{"const", "enum", "allOf"}

// declaresFamily reports whether schema declares the named family. It is the
// single definition of each family's guard: lower() lowers what dispatchOf
// elects and recordSkippedFamilies keeps what the same walk passed over, so the
// winner and the losers can never be read off two tests that disagree. A name
// it does not know declares nothing, rather than falling through to another
// family's guard: a familyOrder entry added without a case here would otherwise
// report whichever guard the default happened to hold, electing that name on
// schemas that never wrote it.
func declaresFamily(schema *statement.Schema, family string) bool {
	switch family {
	case "const":
		return schema.Const() != nil
	case "enum":
		return enumWritten(schema)
	case "allOf":
		return writesConjuncts(schema)
	default:
		return false
	}
}

// enumWritten reports whether schema writes `enum` at all, an empty member list
// included. `enum: []` is legal JSON Schema and fixes the value space to the
// empty set, so it declares one as a populated list does; testing len > 0
// instead would widen the position to whatever its siblings admit, in silence
// (GitHub #278).
//
// Nilness is what the parser keeps: an absent keyword leaves the field nil, an
// empty list leaves it non-nil. A member list the model layer could not parse
// reads as absent, which is a document the loader already refuses.
func enumWritten(schema *statement.Schema) bool {
	return schema.Enum() != nil
}

// dispatch records how lower() resolved a schema's competing keyword families:
// the one it lowered, and the ones it passed over. won is "" when the schema
// declares none of them and the type set decides the lowering instead. said
// names the keywords a union's branches all say already (branchesSay), which
// the position consumes though its node has no field for them.
type dispatch struct {
	won     string
	skipped []string
	said    []string
	// mixin is the union a routed subtype composes (lowerSubtypeUnion), which
	// the build of its model mints, nil where the position composes none.
	mixin *unionAt
}

// handled returns the keys of censusDomain the election accounted for: the
// families it passed over, which recordSkippedFamilies keeps, and the keywords
// the branches say.
func (d dispatch) handled() keySet {
	return keySetOf(d.skipped...) | keySetOf(d.said...)
}

// besideAllOf returns the election lower() makes of the keywords beside s's
// allOf, d being s's own: the one of a schema writing no allOf, as a refinement
// whose principal is no branch lowers. allOf is the last of familyOrder, so it
// wins only where no other family is written.
func (d dispatch) besideAllOf() dispatch {
	out := dispatch{skipped: slices.DeleteFunc(slices.Clone(d.skipped), func(f string) bool { return f == "allOf" })}
	if d.won != "allOf" {
		out.won = d.won
	}
	return out
}

// dispatchOf elects the family lower() lowers and collects the rest. A schema
// declaring none leaves won empty and skipped nil.
//
// familyOrder, declaresFamily and lowerElected's switch must name the same
// families. A name declaresFamily does not know is never elected, and a loser
// reaches recordSkippedFamilies whether or not lowerElected can lower it, but a
// winner lowerElected has no arm for is neither lowered nor skipped: the switch
// falls through to the type-set arms and drops the keyword in silence (GitHub #35).
// Adding a family means adding all three.
func dispatchOf(schema *statement.Schema) dispatch {
	var dispatchValue dispatch
	for _, family := range familyOrder {
		if !declaresFamily(schema, family) {
			continue
		}
		if dispatchValue.won == "" {
			dispatchValue.won = family
			continue
		}
		dispatchValue.skipped = append(dispatchValue.skipped, family)
	}
	return dispatchValue
}

// recordSkippedFamilies keeps verbatim the keyword families lower()'s election
// passed over, and reports them once.
//
// JSON Schema conjoins keywords, so `{const: a, enum: [a, b]}` admits a alone,
// no malformed document. The IR has no intersection combinator
// (docs/ir/boundaries.md#deliberate-exclusions), so only one family can be the
// value; keeping the loser beside it is what the rule
// docs/compilers/openapi.md#competing-keywords asks of an unrepresentable
// conjunction.
//
// It reports what it stored, as the node holds it (keepConjuncts), never what
// it was handed, so the message cannot claim a keyword that failed to convert.
// An allOf whose conjuncts share no value draws its warning, stored or not.
func recordSkippedFamilies(loweringContext lowering.Ctx, td ir.TypeDef, schema *statement.Schema, dispatchValue dispatch, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(dispatchValue.skipped) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	common := td.Common()
	written := make([]string, 0, len(dispatchValue.skipped))
	named := make([]string, 0, len(dispatchValue.skipped))
	for _, keyword := range dispatchValue.skipped {
		if keyword == "allOf" {
			conjuncts, as, keptDiags := keepConjuncts(loweringContext, &common.Unmodeled, schema, pointer)
			diags = append(diags, keptDiags...)
			written, named = append(written, conjuncts...), append(named, as...)
			continue
		}
		stored, storedDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, schema, keyword,
			ir.ReasonDegradedLowering, pointer, loweringContext.Place())
		diags = append(diags, storedDiags...)
		if stored {
			written, named = append(written, keyword), append(named, keyword)
		}
	}
	if len(written) > 0 {
		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"this position declares %s beside its %s, and JSON Schema conjoins them where only "+
				"one can be the value; it lowered as the %s, with %s kept verbatim under Unmodeled",
			strings.Join(written, " and "), dispatchValue.won, dispatchValue.won, strings.Join(named, " and ")))
	}
	if !slices.Contains(dispatchValue.skipped, "allOf") {
		return diags
	}
	// An allOf that refines is no rival of the value set (lower), so one passed
	// over composes, and its conjuncts may share no value, as they are read where
	// it wins (lowerAllOf), whether or not the allOf could be kept.
	return append(diags, diagDisjointConjuncts(loweringContext, readAllOf(loweringContext, schema, pointer), "the "+dispatchValue.won+" it lowered as")...)
}

// writesType reports whether schema writes a type. A YAML null, which the
// parser reads as a type spelled ""
// (docs/parser-library.md#null-type-is-empty), declares none: `type: null` is
// the keyword left out, unlike `type: "null"`
// (docs/compilers/openapi.md#null-keywords).
func writesType(schema *statement.Schema) bool {
	return slices.ContainsFunc(schema.Types(), func(jsonSchemaTypeName statement.JSONSchemaTypeName) bool { return jsonSchemaTypeName != "" })
}

// effectiveTypes returns a schema's declared types with the JSON Schema "null"
// member removed, and any null that declares none (writesType); nullability is
// normalized onto the TypeRef, not the type set.
func effectiveTypes(schema *statement.Schema) []statement.JSONSchemaTypeName {
	types := schema.Types()
	out := make([]statement.JSONSchemaTypeName, 0, len(types))
	for _, jsonSchemaTypeName := range types {
		if jsonSchemaTypeName == statement.KindNull || jsonSchemaTypeName == "" {
			continue
		}
		out = append(out, jsonSchemaTypeName)
	}
	return out
}

// nullableTrue reports whether schema's `nullable` reads true whatever the
// dialect defines, as a policy reads a 3.0 keyword in a 3.1 schema: the model's
// true states it only where the key schema lists is a boolean, never a string
// the model decodes as true, such as "yes" (docs/compilers/openapi.md#nullable,
// GitHub
// #2007). The key is the node the model read, as the statement lists no
// other (statement.Schema.Truncated).
func nullableTrue(schema *statement.Schema) bool {
	if schema == nil || schema.Kinds == nil || schema.Kinds.Nullable == nil || !*schema.Kinds.Nullable {
		return false
	}
	key, _ := schema.Key("nullable")
	written, err := value.Classify(key.Node)
	return err == nil && written.Kind == value.ScalarBool
}
