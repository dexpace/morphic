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
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
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
func LowerComponentSchemas(ctx context.Context, c lowering.Ctx, ts *compile.Types,
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
		diags = append(diags, lowerComponentSchema(c, ts, js, ids.Ptr("components", "schemas", name))...)
	}
	return diags
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func lowerComponentSchema(c lowering.Ctx, ts *compile.Types, js *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	s := annotation.At(js)
	ref, diags := Ref(c, ts, TopLevelDepth, js, pointer)
	// A body that lowered to the component's own node needs no alias, and its
	// annotations were attached where it was lowered.
	if LoweredToOwnNode(c, pointer, ref) {
		return diags
	}
	// This node is the first the pointer owns, so the annotations schemaBody
	// had nowhere to put now have a home, which its build gives them.
	var annotated finished
	var fin finisher
	if stated(s.Node) {
		fin = annotated.on(func(td ir.TypeDef) []ir.Diagnostic {
			return attachDeclaredAnnotations(c, ts, s.Node, pointer, ref, td)
		})
	}
	diags = append(diags, internOwnNode(c, ts, js, pointer, ref, fin)...)
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
func Ref(c lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, depth, js, pointer, annotation.HomeOwnNode, nil)
}

// CarriedRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func CarriedRef(c lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, depth, js, pointer, annotation.HomeCarrier, nil)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias. fin, an
// outer frame's finisher, runs after the position's own on the node standing
// at pointer, where this lowering builds one.
func schemaRefHomed(c lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer, home annotation.Home,
	fin finisher,
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
		return ts.PrimRef(ir.PrimAny), nil
	}
	depth++
	if depth > maxSchemaDepth {
		return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth)}
	}
	switch js.Truth {
	case statement.Unread:
		// A mapping the parser read as false: the load phase decodes it again
		// unless it lies past typed.MaxMends
		// (docs/parser-library.md#failure-beneath-itself), so it says nothing of
		// the values there.
		return ts.PrimRef(ir.PrimAny), []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.DegradedConstruct, pointer,
			"schema read as false, as the parser fails to decode a keyword beneath it under its own name "+
				"and the load phase decodes no more than %d such schemas one below another; lowered as any; "+
				"moving the schemas below it into a component reads them",
			typed.MaxMends)}
	case statement.Nothing:
		id, diags := falseSchema(c, ts, pointer)
		return ir.TypeRef{Target: id}, diags
	case statement.Anything:
		return ts.PrimRef(ir.PrimAny), nil
	default:
		// Stated: a schema object, which a reference site writes too.
	}
	diags := mergedPastBound(c, js)
	if js.Ref != nil {
		ref, refDiags := refSiteRef(c, ts, js, js.Schema, pointer, home, fin)
		return ref, append(diags, refDiags...)
	}
	ref, bodyDiags := schemaBody(c, ts, depth, js.Schema, pointer, home, fin)
	return ref, append(diags, bodyDiags...)
}

// mergedPastOf keys whether mergedPastBound has reported one schema.
type mergedPastOf struct {
	schema *statement.Schema
}

// mergedPastBound reports each schema object of st, its own and its inline
// allOf entries', whose `<<` merges run past nodeview.MergeDepthLimit
// (pastBoundOnce).
func mergedPastBound(c lowering.Ctx, st *statement.Statement) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if d, first := pastBoundOnce(c, st.Schema); first {
		diags = append(diags, d)
	}
	for _, e := range st.Entries {
		if d, first := pastBoundOnce(c, e.Schema); first {
			diags = append(diags, d)
		}
	}
	return diags
}

// pastBoundOnce returns the report of s, and true the first time a run asks
// for a Truncated s: a warning at its own pointer that it is read from the
// keys within its merges alone, so a keyword merged in past them, or one a
// source past them outranks, is not read (docs/compilers/openapi.md#references).
func pastBoundOnce(c lowering.Ctx, s *statement.Schema) (ir.Diagnostic, bool) {
	if !s.Truncated {
		return ir.Diagnostic{}, false
	}
	reported := lowering.Remembered(c, mergedPastOf{s}, func() *bool { return new(bool) })
	if *reported {
		return ir.Diagnostic{}, false
	}
	*reported = true
	there, _ := lowering.InDocument(c, s.At.Doc)
	return there.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, s.At.Pointer,
		"the schema merges through `<<` deeper than %d levels and is read from the keys within them; "+
			"a keyword merged in past them, or one a source past them outranks, is not read",
		nodeview.MergeDepthLimit), true
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing), returning the interned ID and the one info diagnostic that
// announces it.
func falseSchema(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
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
			ir.RawValue("false"), ir.ReasonDegradedLowering, c.Place().At(pointer))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, pointer,
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
// home's, a principal's residue, what the position keeps beside it.
func schemaBody(c lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, home annotation.Home,
	fin finisher,
) (ir.TypeRef, []ir.Diagnostic) {
	diags := missingArrayItemsConjoined(c, schema, pointer)
	decl := &declaring{c: c, ts: ts, s: schema, target: ir.TypeRef{Target: c.IDs().ForPointer(pointer)}, pointer: pointer, home: home, fin: fin}
	target, bodyDiags := lowerSchemaBody(c, ts, depth, schema, pointer, decl.finish,
		principalResidue(c, schema, pointer, home, &decl.principal), stand{})
	diags = append(diags, bodyDiags...)
	ref := target
	// A body's census ran where its node was built, the only thing that can
	// answer it. One lowering to a reference built none, so it takes the census
	// of a position lowered to one, and its verdict homes an alias.
	if !decl.ran && homesDeclaration(c, schema, target, pointer, home,
		keepsAtOutcome(c, schema, target, pointer).holdsAny() || declaresScoped(c, declarationVoices(c, schema, target, pointer))) {
		var homeDiags []ir.Diagnostic
		decl.target = target
		ref, homeDiags = hoistUseHome(c, ts, schema, target, pointer, decl.finish)
		diags = append(diags, homeDiags...)
	}
	return ref, slices.Concat(diags, decl.diags, decl.principal, decl.kept)
}

// declaring is a body's declaration s, written at pointer under home, on its
// way to the node the position lowered to target stands at: what finishes it
// there (finish), fin after, and what it reported, its principal's residue
// among it.
type declaring struct {
	c       lowering.Ctx
	ts      *compile.Types
	s       *statement.Schema
	fin     finisher
	pointer jsontext.Pointer
	target  ir.TypeRef
	diags   []ir.Diagnostic
	// principal is what a refinement's principal reported keeping on its own
	// node (principalResidue).
	principal []ir.Diagnostic
	kept      []ir.Diagnostic
	home      annotation.Home
	ran       bool
}

// finish writes the declaration and what the position keeps beside it on td,
// in hand (declarationOn, keptOn), then d.fin.
func (d *declaring) finish(td ir.TypeDef, read keySet) {
	d.ran = true
	d.diags = declarationOn(d.c, d.ts, d.s, d.target, d.pointer, d.home, td)
	d.kept = keptOn(d.c, d.ts, d.s, d.target, d.pointer, td)
	if d.fin != nil {
		d.fin(td, read)
	}
}

// keptOn keeps on td, in hand, the node the position s, written at pointer,
// lowered to target, stands at, what it keeps there: its census of a lowering
// to a reference and the branches its refinement left (keepsAtOutcome). A
// carrier keeps them where the position owns no node (PreserveRefSiteKeywords).
func keptOn(c lowering.Ctx, ts *compile.Types, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer, td ir.TypeDef,
) []ir.Diagnostic {
	keeps := keepsAtOutcome(c, s, target, pointer)
	if !keeps.holdsAny() {
		return nil
	}
	return keeps.record(c, ts, &td.Common().Unmodeled, s, pointer)
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
//
// fin finishes the node the body builds at pointer, and principal the node a
// refinement's principal builds at its own (lower).
func lowerSchemaBody(c lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer,
	fin, principal finisher, st stand,
) (ir.TypeRef, []ir.Diagnostic) {
	if at, _, expanded := dynamicExpansion(c, schema, pointer); expanded {
		// The expansion settled that the target resolves, by what resolveAt
		// reads in its own document (resolvesAt), so nothing falls through: that
		// would drop the reference unrecorded, as recordUnexpandedDynamicRef
		// keeps only one that does not expand, where an empty target fails
		// irverify.
		target, _ := resolveAt(c, ts, at, c.Statements().At(at))
		ref, nullDiags := schemaNullRef(c, target, schema, siteAt(pointer))
		return ref, append([]ir.Diagnostic{dynamicExpanded(c, schema, pointer, target)}, nullDiags...)
	}
	if declaresUnion(schema) && (st.of != nil || !refinesToBranch(c, schema, pointer)) {
		if hasUnionSiblings(c, schema) {
			id, unionDiags := lowerCoDeclaredUnion(c, ts, depth, schema, pointer, fin, st)
			ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
			return ref, append(unionDiags, nullDiags...)
		}
		return lowerOneOfAnyOf(c, ts, depth, schema, pointer, fin, st)
	}
	u, how, ok := branchUnion(c, schema, pointer)
	if ok {
		lowerUnion := lowerDistributedUnion
		if how == unionRoutedSubtype {
			lowerUnion = lowerSubtypeUnion
		}
		id, unionDiags := lowerUnion(c, ts, depth, schema, u, pointer, fin)
		ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
		return ref, append(unionDiags, nullDiags...)
	}
	if how == unionHierarchyBase || how == unionNoLiveBranch {
		// The union stays with its branch, and the discriminator standing with it
		// on the model, held to its routes as one beside the union is.
		fin = discriminatorBesideUnion(c, ts, schema, u.holder, pointer).then(fin)
	}
	id, bodyDiags := lower(c, ts, depth, schema, pointer, fin, principal, st)
	if how == unionNoLiveBranch {
		bodyDiags = append(bodyDiags, diagDeadEntryUnion(c, schema, u, pointer)...)
	}
	ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
	return ref, append(bodyDiags, nullDiags...)
}

// refinesToBranch reports whether s, written at pointer, refines to one of its
// allOf branches, which then decides the position, a union beside the allOf
// being one of the conjuncts it refines (docs/compilers/openapi.md#allof-refinement).
func refinesToBranch(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) bool {
	r, refines := refinementOf(c, s, pointer)
	return refines && r.principal != nil
}

// allOfComposes reports whether s writes an allOf that composes rather than
// refines, the one a union beside it distributes across or is kept beside.
func allOfComposes(c lowering.Ctx, s *statement.Schema) bool {
	if !writesConjuncts(s) {
		return false
	}
	return !decisionOf(c, s).refines
}

// lower interns the inline schema at pointer and returns its TypeID. An allOf
// that refines decides the position (docs/compilers/openapi.md#allof-refinement):
// its principal, or where no branch is one, the keywords beside it as if no allOf
// were written, an enum or a const there being one of its conjuncts rather than
// its rival. A value set an inline principal writes stands at the position, as
// one written beside the allOf does, and either is typed by every conjunct
// (valueSetSite.valueType). Any other schema lowers by its election (lowerElected).
// fin finishes the node built at pointer, principal an inline principal's.
func lower(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, fin, principal finisher,
	st stand,
) (ir.TypeID, []ir.Diagnostic) {
	if st.of != nil {
		// s is the inline principal of the refinement st stands for, a wrapper's
		// allOf being the position's conjuncts and no rival of its keywords
		// (principalDispatch).
		return lowerElected(c, ts, depth, s, pointer, dispatchOf(s).besideAllOf(), fin, st)
	}
	d, elected := electionOf(c, s, pointer)
	r, refines := refinementOf(c, s, pointer)
	if !refines {
		return lowerElected(c, ts, depth, s, pointer, d, fin, st)
	}
	reported := refinedDisjoint(c, s, r)
	if e, ok := valueSetPrincipal(r); ok {
		resolveBeaten(c, ts, r)
		id, diags := lowerPrincipalValueSet(c, ts, s, e, pointer, fin)
		return id, append(diags, reported...)
	}
	if elected {
		resolveBeaten(c, ts, r)
		id, diags := lowerElected(c, ts, depth, s, pointer, d, fin, st)
		return id, append(diags, reported...)
	}
	id, diags := lowerPrincipal(c, ts, depth, s, pointer, r, fin, principal)
	return id, append(diags, reported...)
}

// refinedDisjoint reports the refinement r of s whose union beside the allOf
// shares no kind with the rest (unionAside), or whose conjuncts share no value
// and lowered to the value set elected before them or to what the keywords
// beside the allOf say (contradictionShape), at s, as a composition reports
// conjuncts sharing no value.
func refinedDisjoint(c lowering.Ctx, s *statement.Schema, r refinement) []ir.Diagnostic {
	shape := refinedShape(r.comp)
	switch {
	case unionAside(c, s):
	case r.contradicts:
		shape = contradictionShape(r, s)
	default:
		return nil
	}
	comp := r.comp
	comp.disjoint = disjointAtoms(c, s, refinedAtoms(c, s, r))
	if len(comp.disjoint) == 0 && r.contradicts {
		comp.disjoint = conjunctsApart(c, s)
	}
	return diagDisjointConjuncts(c, comp, shape)
}

// electionOf returns the election lower lowers s by, written at pointer, and
// false where an allOf refining it has a principal, which decides the position
// instead: the keywords beside the allOf, as if none were written, for one
// refining with none.
func electionOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) (dispatch, bool) {
	r, refines := refinementOf(c, s, pointer)
	switch {
	case !refines:
		return dispatchOf(s), true
	case r.principal != nil:
		return dispatch{}, false
	default:
		return dispatchOf(s).besideAllOf(), true
	}
}

// lowersTypeArray reports whether s, written at pointer, lowers by its type list
// (lowerUnion): it lists more than one type, and lower elects no family. Nothing
// lowerSchemaBody tries first takes such a schema: a type is a shape, so no
// $dynamicRef expands and no union collapses beside it, and a union beside it or
// on an allOf branch distributes only over the allOf, which the election elects.
func lowersTypeArray(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) bool {
	if len(effectiveTypes(s)) < 2 {
		return false
	}
	d, elected := electionOf(c, s, pointer)
	return elected && d.won == ""
}

// lowerElected lowers s, at pointer, by the family d elects, which takes
// precedence over the structural type, else by its effective (null-stripped)
// type set. The families are conjoined where a schema writes more than one, so
// electing a winner is a degradation
// (docs/compilers/openapi.md#competing-keywords): the ones passed over are kept
// verbatim beside it (recordSkippedFamilies). A keyword the elected lowering
// never reads, such as `type: string` beside an allOf, goes through the census
// (keepingUnread), which keeps what the build did not read. fin
// finishes the node built at pointer after the census.
func lowerElected(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, d dispatch, fin finisher,
	st stand,
) (ir.TypeID, []ir.Diagnostic) {
	census := func(build func(fin finisher) (ir.TypeID, []ir.Diagnostic)) (ir.TypeID, []ir.Diagnostic) {
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := build(fin)
			return id, 0, diags
		})
	}
	switch d.won {
	case "const":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerConst(c, ts, standingValueSet(s, pointer, st).finishedBy(fin))
		})
	case "enum":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerEnum(c, ts, standingValueSet(s, pointer, st).finishedBy(fin))
		})
	case "allOf":
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerAllOf(c, ts, depth, s, pointer, !slices.Contains(d.said, "discriminator"), d.mixin, fin)
		})
	}
	// won is "": the schema declares no family, or only an allOf that refines
	// with no branch deciding what the position is, so the type set decides.
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		// A multi-typed schema lowers one variant per declared type, each from
		// this same schema, so the homes are the variants', not the Union's, and
		// the type set itself is what built them.
		var census finished
		id, diags := lowerUnion(c, ts, depth, s, pointer, typeArray{types: types},
			census.onRead(keepUnreadAcross(c, ts, s, nodeShape(ir.KindUnion), keyBit("type"), pointer)).then(fin), st)
		return id, append(diags, census.diags...)
	case len(types) == 1:
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerTyped(c, ts, depth, s, pointer, types[0], fin, st)
		})
	case writesType(s):
		// Every type s names is null, so its value space is null and nothing
		// else, which is no schemaless position's (docs/compilers/openapi.md#null-alone).
		return census(func(fin finisher) (ir.TypeID, []ir.Diagnostic) {
			return lowerNullAlone(c, ts, standingValueSet(s, pointer, st).finishedBy(fin)), nil
		})
	default:
		return lowerUntyped(c, ts, depth, s, pointer, d, fin, st)
	}
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form,
// fin finishing the node it builds at pointer, and returns what a lowering to
// a shared primitive read.
func lowerTyped(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, kind statement.Kind,
	fin finisher, st stand,
) (ir.TypeID, keySet, []ir.Diagnostic) {
	switch kind {
	case statement.KindObject:
		// An object composes, so no refinement's principal reaches here.
		id, diags := lowerModel(c, ts, depth, s, pointer, true /* discriminated */, kindObject, fin)
		return id, 0, diags
	case statement.KindArray:
		id, diags := lowerArray(c, ts, depth, s, pointer, fin, st)
		return id, 0, diags
	default:
		return scalarTypeID(c, ts, depth, s, kind, pointer, fin, st)
	}
}

// lowerUntyped lowers s, which writes no kind and no family lower() elects
// first, by the election d, as one writing the type its shape keywords speak
// of (untypedKinds), its nodes marked where the kind-inference policy read the
// kind, else to the top type. The keywords the node cannot hold are kept beside
// it (keepingUnread), as for a schema writing that type.
func lowerUntyped(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, d dispatch,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	kinds, inferred := untypedKinds(c, s)
	var mark finisher
	if inferred {
		mark = markKindInferred
	}
	switch kinds {
	case 0:
		return lowerUndecided(c, ts, depth, s, pointer, d, fin, st)
	case kindObject:
		// The node asserts an object, as the bounds beside it read it
		// (boundKinds): one an object does not take is kept.
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := lowerModel(c, ts, depth, s, pointer, true /* discriminated */, boundKinds(c, s), mark.then(fin))
			return id, 0, diags
		})
	case kindArray:
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			id, diags := lowerArray(c, ts, depth, s, pointer, mark.then(fin), st)
			return id, 0, diags
		})
	default:
		return lowerUntypedUnion(c, ts, depth, s, pointer, inferredKinds, inferred, fin, st)
	}
}

// familyOrder is the order lower() tries the keyword families that outrank the
// structural type, and dispatchOf is the sole reader of it. First match wins, so
// a schema declaring more than one of them lowers as the first and passes over
// the rest — which is why the two are derived together rather than separately.
var familyOrder = []string{"const", "enum", "allOf"}

// declaresFamily reports whether s declares the named family. It is the single
// definition of each family's guard: lower() lowers what dispatchOf elects and
// recordSkippedFamilies keeps what the same walk passed over, so the winner and
// the losers can never be read off two tests that disagree.
// A name it does not know declares nothing, rather than falling through to
// another family's guard: a familyOrder entry added without a case here would
// otherwise report whichever guard the default happened to hold, electing that
// name on schemas that never wrote it.
func declaresFamily(s *statement.Schema, family string) bool {
	switch family {
	case "const":
		return s.Const() != nil
	case "enum":
		return enumWritten(s)
	case "allOf":
		return writesConjuncts(s)
	default:
		return false
	}
}

// enumWritten reports whether s writes `enum` at all, an empty member list
// included. `enum: []` is legal JSON Schema and fixes the value space to the
// empty set, so it declares one as a populated list does; testing len > 0
// instead would widen the position to whatever its siblings admit, in silence
// (GitHub #278).
//
// Nilness is what the parser keeps: an absent keyword leaves the field nil, an
// empty list leaves it non-nil. A member list the model layer could not parse
// reads as absent, which is a document the loader already refuses.
func enumWritten(s *statement.Schema) bool {
	return s.Enum() != nil
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
func dispatchOf(s *statement.Schema) dispatch {
	var d dispatch
	for _, family := range familyOrder {
		if !declaresFamily(s, family) {
			continue
		}
		if d.won == "" {
			d.won = family
			continue
		}
		d.skipped = append(d.skipped, family)
	}
	return d
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
func recordSkippedFamilies(c lowering.Ctx, td ir.TypeDef, s *statement.Schema, d dispatch, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(d.skipped) == 0 {
		return nil
	}
	var diags []ir.Diagnostic
	common := td.Common()
	written := make([]string, 0, len(d.skipped))
	named := make([]string, 0, len(d.skipped))
	for _, keyword := range d.skipped {
		if keyword == "allOf" {
			conjuncts, as, keptDiags := keepConjuncts(c, &common.Unmodeled, s, pointer)
			diags = append(diags, keptDiags...)
			written, named = append(written, conjuncts...), append(named, as...)
			continue
		}
		stored, storedDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.Place())
		diags = append(diags, storedDiags...)
		if stored {
			written, named = append(written, keyword), append(named, keyword)
		}
	}
	if len(written) > 0 {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"this position declares %s beside its %s, and JSON Schema conjoins them where only "+
				"one can be the value; it lowered as the %s, with %s kept verbatim under Unmodeled",
			strings.Join(written, " and "), d.won, d.won, strings.Join(named, " and ")))
	}
	if !slices.Contains(d.skipped, "allOf") {
		return diags
	}
	// An allOf that refines is no rival of the value set (lower), so one passed
	// over composes, and its conjuncts may share no value, as they are read where
	// it wins (lowerAllOf), whether or not the allOf could be kept.
	return append(diags, diagDisjointConjuncts(c, readAllOf(c, s, pointer), "the "+d.won+" it lowered as")...)
}

// writesType reports whether s writes a type. A YAML null, which the parser reads
// as a type spelled "" (docs/parser-library.md#null-type-is-empty), declares
// none: `type: null` is the keyword left out, unlike `type: "null"`
// (docs/compilers/openapi.md#null-keywords).
func writesType(s *statement.Schema) bool {
	return slices.ContainsFunc(s.Types(), func(t statement.Kind) bool { return t != "" })
}

// effectiveTypes returns a schema's declared types with the JSON Schema "null"
// member removed, and any null that declares none (writesType); nullability is
// normalized onto the TypeRef, not the type set.
func effectiveTypes(s *statement.Schema) []statement.Kind {
	types := s.Types()
	out := make([]statement.Kind, 0, len(types))
	for _, t := range types {
		if t == statement.KindNull || t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// nullableTrue reports whether s's `nullable` reads true whatever the dialect
// defines, as a policy reads a 3.0 keyword in a 3.1 schema: the model's true
// states it only where the key s lists is a boolean, never a string the model
// decodes as true, such as "yes" (docs/compilers/openapi.md#nullable, GitHub
// #2007). The key is the node the model read, as the statement lists no
// other (statement.Schema.Truncated).
func nullableTrue(s *statement.Schema) bool {
	if s == nil || s.Kinds == nil || s.Kinds.Nullable == nil || !*s.Kinds.Nullable {
		return false
	}
	k, _ := s.Key("nullable")
	written, err := value.Classify(k.Node)
	return err == nil && written.Kind == value.ScalarBool
}
