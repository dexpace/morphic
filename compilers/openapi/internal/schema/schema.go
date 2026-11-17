// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

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
	nodeDiags := internOwnNode(c, ts, js, pointer, ref)
	diags = append(diags, nodeDiags...)
	// This node is the first the pointer owns, so the annotations schemaBody
	// had nowhere to put now have a home.
	if stated(s.Node) {
		diags = append(diags, attachDeclaredAnnotations(c, ts, s.Node, pointer, ref)...)
	}
	return diags
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
	return schemaRefHomed(c, ts, depth, js, pointer, annotation.HomeOwnNode)
}

// CarriedRef lowers a schema whose annotations the calling position
// already carries — a model property, a header, a parameter. Those callers
// copy the declaration onto their own ir.Property/ir.Parameter, so the pointer
// must not also hoist a node to hold it: one home per declaration.
func CarriedRef(c lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	return schemaRefHomed(c, ts, depth, js, pointer, annotation.HomeCarrier)
}

// schemaRefHomed is the shared body of the two entry points above; home only
// reaches the two places that can hoist an annotation-holding alias.
func schemaRefHomed(c lowering.Ctx, ts *compile.Types, depth int, js *statement.Statement, pointer jsontext.Pointer, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
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
		ref, refDiags := refSiteRef(c, ts, js, js.Schema, pointer, home)
		return ref, append(diags, refDiags...)
	}
	ref, bodyDiags := schemaBody(c, ts, depth, js.Schema, pointer, home)
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
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
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
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}
	})
	return id, diags
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. Its one caller is schemaRefHomed, the body of Ref and CarriedRef,
// for a position that is no $ref site (refSiteRef takes those); sub-schema
// hoisting (hoistSubSchema) arrives through Ref. A rule about the
// declaration itself is checked here, above the dispatch that picks what it
// lowers to (missingArrayItems).
func schemaBody(c lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	diags := missingArrayItemsConjoined(c, schema, pointer)
	target, bodyDiags := lowerSchemaBody(c, ts, depth, schema, pointer)
	diags = append(diags, bodyDiags...)
	// A body's census ran where its node was built, the only thing that can
	// answer it. One lowering to a reference built none, so it takes a $ref
	// site's census, and its verdict homes an alias.
	keeps := keepsAtOutcome(c, ts, schema, target, pointer)
	ref, homeDiags := homeDeclaration(c, ts, schema, target, pointer, home, keeps.holdsAny())
	diags = append(diags, homeDiags...)
	diags = append(diags, recordPrincipalResidue(c, ts, schema, pointer, home)...)
	if !keeps.holdsAny() || (home != annotation.HomeOwnNode && !LoweredToOwnNode(c, pointer, ref)) {
		return ref, diags // the carrier keeps them (PreserveRefSiteKeywords)
	}
	return ref, append(diags, keeps.recordOn(c, ts, ref.Target, schema, pointer)...)
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
func lowerSchemaBody(c lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
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
	if declaresUnion(schema) && !refinesToBranch(c, schema, pointer) {
		if hasUnionSiblings(c, schema) {
			id, unionDiags := lowerCoDeclaredUnion(c, ts, depth, schema, pointer)
			ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
			return ref, append(unionDiags, nullDiags...)
		}
		return lowerOneOfAnyOf(c, ts, depth, schema, pointer)
	}
	u, how, ok := branchUnion(c, schema, pointer)
	if ok {
		lowerUnion := lowerDistributedUnion
		if how == unionRoutedSubtype {
			lowerUnion = lowerSubtypeUnion
		}
		id, unionDiags := lowerUnion(c, ts, depth, schema, u, pointer)
		ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
		return ref, append(unionDiags, nullDiags...)
	}
	id, bodyDiags := lower(c, ts, depth, schema, pointer)
	if how == unionHierarchyBase {
		// The union stays with its branch, and the discriminator standing with it
		// on the model, held to its routes as one beside the union is.
		deferDiscriminatorBesideUnion(c, ts, id, schema, u.holder, pointer)
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
	_, refines := refinementOf(c, s, "")
	return !refines
}

// lower interns the inline schema at pointer and returns its TypeID. An allOf
// that refines decides the position (docs/compilers/openapi.md#allof-refinement):
// its principal, or where no branch is one, the keywords beside it as if no allOf
// were written, an enum or a const there being one of its conjuncts rather than
// its rival. A value set an inline principal writes stands at the position, as
// one written beside the allOf does, and either is typed by every conjunct
// (valueSetSite.valueType). Any other schema lowers by its election (lowerElected).
func lower(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	d, elected := electionOf(c, s, pointer)
	r, refines := refinementOf(c, s, pointer)
	if !refines {
		return lowerElected(c, ts, depth, s, pointer, d)
	}
	reported := refinedDisjoint(c, s, r)
	if e, ok := valueSetPrincipal(r); ok {
		resolveBeaten(c, ts, r)
		id, diags := lowerPrincipalValueSet(c, ts, s, e, pointer)
		return id, append(diags, reported...)
	}
	if elected {
		resolveBeaten(c, ts, r)
		id, diags := lowerElected(c, ts, depth, s, pointer, d)
		return id, append(diags, reported...)
	}
	id, diags := lowerPrincipal(c, ts, depth, r)
	return id, append(diags, reported...)
}

// refinedDisjoint reports the refinement r of s whose union beside the allOf
// shares no kind with the rest (unionAside), or whose conjuncts share no value
// and lowered to the value set elected before them, at s, as a composition
// reports conjuncts sharing no value.
func refinedDisjoint(c lowering.Ctx, s *statement.Schema, r refinement) []ir.Diagnostic {
	shape := refinedShape(r.comp)
	switch {
	case unionAside(c, s):
	case r.contradicts:
		shape = "the " + electedSet(r, s) + " it lowered as"
	default:
		return nil
	}
	comp := r.comp
	comp.disjoint = disjointConjuncts(c, s, comp)
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
// never reads, such as `type: string` beside an allOf, goes through
// preserveUnhomedKeywords, which asks the built node whether it has a field for it.
func lowerElected(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, d dispatch) (ir.TypeID, []ir.Diagnostic) {
	unhomed := func(id ir.TypeID, diags []ir.Diagnostic) (ir.TypeID, []ir.Diagnostic) {
		owner, ownDiags := preserveUnhomedKeywords(c, ts, s, pointer, pointer, id, d)
		return owner, append(diags, ownDiags...)
	}
	switch d.won {
	case "const":
		return unhomed(lowerConst(c, ts, ownValueSet(s, pointer)))
	case "enum":
		return unhomed(lowerEnum(c, ts, ownValueSet(s, pointer)))
	case "allOf":
		return unhomed(lowerAllOf(c, ts, depth, s, pointer, !slices.Contains(d.said, "discriminator")))
	}
	// won is "": the schema declares no family, or only an allOf that refines
	// with no branch deciding what the position is, so the type set decides.
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		// A multi-typed schema lowers one variant per declared type, each from
		// this same schema, so the homes are the variants', not the Union's, and
		// the type set itself is what built them.
		id, diags := lowerUnion(c, ts, depth, s, pointer, types)
		return id, append(diags, preserveVariantsUnhomed(c, ts, s, id, nodeShape(ir.KindUnion), []string{"type"}, pointer)...)
	case len(types) == 1:
		return unhomed(lowerTyped(c, ts, depth, s, pointer, types[0]))
	case writesType(s):
		// Every type s names is null, so its value space is null and nothing
		// else, which is no schemaless position's (docs/compilers/openapi.md#null-alone).
		return unhomed(lowerNullAlone(c, ts, ownValueSet(s, pointer)), nil)
	default:
		return lowerUntyped(c, ts, depth, s, pointer, d)
	}
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func lowerTyped(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, st statement.Kind) (ir.TypeID, []ir.Diagnostic) {
	switch st {
	case statement.KindObject:
		return lowerModel(c, ts, depth, s, pointer, true /* discriminated */, kindObject)
	case statement.KindArray:
		return lowerArray(c, ts, depth, s, pointer)
	default:
		return scalarTypeID(c, ts, depth, s, st, pointer)
	}
}

// lowerUntyped lowers s, which writes no kind and no family lower() elects
// first, by the election d, as one writing the type its shape keywords speak
// of (untypedKinds), its nodes marked where the kind-inference policy read the
// kind, else to the top type. The keywords the node cannot hold are kept beside
// it (preserveUnhomedKeywords), as for a schema writing that type.
func lowerUntyped(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, d dispatch,
) (ir.TypeID, []ir.Diagnostic) {
	kinds, inferred := untypedKinds(c, s)
	var id ir.TypeID
	var diags []ir.Diagnostic
	switch kinds {
	case 0:
		return preserveUnhomedKeywords(c, ts, s, pointer, pointer, ts.PrimID(ir.PrimAny), d)
	case kindObject:
		// The node asserts an object, as the bounds beside it read it
		// (boundKinds): one an object does not take is kept.
		id, diags = lowerModel(c, ts, depth, s, pointer, true /* discriminated */, boundKinds(c, s))
	case kindArray:
		id, diags = lowerArray(c, ts, depth, s, pointer)
	default:
		return lowerUntypedUnion(c, ts, depth, s, pointer, inferred)
	}
	if inferred {
		diags = append(diags, markKindInferred(c, ts, id, pointer)...)
	}
	owner, ownDiags := preserveUnhomedKeywords(c, ts, s, pointer, pointer, id, d)
	return owner, append(diags, ownDiags...)
}

// untypedKinds returns the kinds s, which writes no kind of its own, lowers to
// as its shape keywords speak of them (shapeKinds), and whether the
// kind-inference policy read them (lowersTo). Where its conjuncts, as a
// union kept beside it, admit a kind the keywords do not speak of, there are
// none, as where nothing names a kind. Else s lowers to the kinds they admit:
// the policy's where it read any, off s or a conjunct, the document's
// otherwise.
func untypedKinds(c lowering.Ctx, s *statement.Schema) (kindSet, bool) {
	claims := shapeKinds(s)
	if claims == 0 || !stated(s) {
		return 0, false
	}
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	if kinds&^claims != 0 {
		return 0, false
	}
	return kinds, inferred
}

// inferredKinds are the types a schema whose shape keywords speak of an object
// and an array lowers by, in the order a type array listing both writes them,
// whichever order the keywords are written in.
var inferredKinds = []statement.Kind{statement.KindObject, statement.KindArray}

// lowerUntypedUnion lowers s, whose shape keywords speak of an object and an
// array, as `type: [object, array]` beside them lowers (lowerUnion). Where the
// policy read the kinds, the union and each variant are marked, as each
// variant's kind was read off the keywords too.
func lowerUntypedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer,
	inferred bool,
) (ir.TypeID, []ir.Diagnostic) {
	id, diags := lowerUnion(c, ts, depth, s, pointer, inferredKinds)
	if inferred {
		diags = append(diags, markKindInferred(c, ts, id, pointer)...)
		for i := range inferredKinds {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			diags = append(diags, markKindInferred(c, ts, c.IDs().ForPointer(vptr), vptr)...)
		}
	}
	return id, append(diags, preserveVariantsUnhomed(c, ts, s, id, nodeShape(ir.KindUnion), nil, pointer)...)
}

// claimedKinds returns the kinds c's kind-inference policy reads off the
// keywords s writes that give a type to the instances they apply to, a
// member's, an element's or a value's (siblingKeyword.infers), and none where
// the policy is off. The run's admission reads it as the kinds a statement
// declaring none lowers to (readOne), which lowerUntyped lowers by
// (untypedKinds).
func claimedKinds(c lowering.Ctx, s *statement.Schema) kindSet {
	if !c.KindInference.Honours() {
		return 0
	}
	var out kindSet
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.infers != nil && k.claims&^out != 0 && k.infers(c.Statements(), s) {
				out |= k.claims
			}
		}
	}
	return out
}

// shapeKinds returns the kinds of instance the shape keywords s writes apply
// to alone, whether or not they infer one: an object for a `discriminator` and
// for each keyword whose row in shapeKeywords or narrowingKeywords claims one,
// as its predicate reads it written, an array likewise
// (docs/compilers/openapi.md#shape-applicator). It is the one reading of what
// they speak of.
func shapeKinds(s *statement.Schema) kindSet {
	var out kindSet
	if discriminatorIn(s) != nil {
		out = kindObject
	}
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.claims&^out != 0 && k.writes(s) {
				out |= k.claims
			}
		}
	}
	return out
}

// markKindInferred names the kind-inference policy in the provenance of id,
// the node pointer owns, whose kind lowerUntyped read off its shape keywords
// (docs/compilers/openapi.md#policies). A node still building is marked by the
// frame building it.
func markKindInferred(c lowering.Ctx, ts *compile.Types, id ir.TypeID, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
	return nil
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
		return constIn(s) != nil
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
	return enumIn(s) != nil
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
// JSON Schema conjoins keywords, so `{const: a, enum: [a, b]}` admits a alone and
// is no malformed document. The IR has no intersection combinator
// (docs/ir/boundaries.md#deliberate-exclusions), so only one family can be the
// value; keeping the loser beside it is what the rule
// docs/compilers/openapi.md#competing-keywords asks of an unrepresentable
// conjunction.
//
// It reports the keywords it stored, never the ones it was handed, so the
// message cannot claim one that failed to convert. An allOf whose conjuncts
// share no value draws its warning, stored or not.
func recordSkippedFamilies(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *statement.Schema, d dispatch, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(d.skipped) == 0 {
		return nil
	}
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	kept := make([]string, 0, len(d.skipped))
	for _, keyword := range d.skipped {
		if keyword == "allOf" {
			written, keptDiags := keepConjuncts(c, &common.Unmodeled, s, pointer)
			diags = append(diags, keptDiags...)
			kept = append(kept, written...)
			continue
		}
		stored, storedDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.Place())
		diags = append(diags, storedDiags...)
		if stored {
			kept = append(kept, keyword)
		}
	}
	if len(kept) > 0 {
		skipped := strings.Join(kept, " and ")
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"this position declares %s beside its %s, and JSON Schema conjoins them where only "+
				"one can be the value; it lowered as the %s, with %s kept verbatim under Unmodeled",
			skipped, d.won, d.won, skipped))
	}
	if !slices.Contains(d.skipped, "allOf") {
		return diags
	}
	// An allOf that refines is no rival of the value set (lower), so one passed
	// over composes, and its conjuncts may share no value, as they are read where
	// it wins (lowerAllOf), whether or not the allOf could be kept.
	return append(diags, diagDisjointConjuncts(c, readAllOf(c, s, pointer), "the "+d.won+" it lowered as")...)
}

// keepConjuncts keeps on p the conjuncts s, written at pointer, writes beside
// its own keywords, which an election passed over: the $ref written beside them
// under keptRefKey, and the allOf whole. It returns the keywords it kept, `$ref`
// and `allOf`, in that order.
func keepConjuncts(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var written []string
	var diags []ir.Diagnostic
	if s.BesideRef != nil {
		e := allOfBranch{schema: c.Statements().Read(s.BesideRef), pointer: pointer}
		kept, keptDiags := keepRef(c, p, allOfComposition{pointer: pointer}, e)
		diags = append(diags, keptDiags...)
		if kept {
			written = append(written, "$ref")
		}
	}
	kept, keptDiags := annotation.PreserveSchemaKeyword(p, s, "allOf", ir.ReasonDegradedLowering, pointer, c.Place())
	diags = append(diags, keptDiags...)
	if kept {
		written = append(written, "allOf")
	}
	return written, diags
}

// writesType reports whether s writes a type. A YAML null, which the parser reads
// as a type spelled "" (docs/parser-library.md#null-type-is-empty), declares
// none: `type: null` is the keyword left out, unlike `type: "null"`
// (docs/compilers/openapi.md#null-keywords).
func writesType(s *statement.Schema) bool {
	return slices.ContainsFunc(kindsIn(s), func(t statement.Kind) bool { return t != "" })
}

// effectiveTypes returns a schema's declared types with the JSON Schema "null"
// member removed, and any null that declares none (writesType); nullability is
// normalized onto the TypeRef, not the type set.
func effectiveTypes(s *statement.Schema) []statement.Kind {
	types := kindsIn(s)
	out := make([]statement.Kind, 0, len(types))
	for _, t := range types {
		if t == statement.KindNull || t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// kindsIn returns the types s's `type` names, as written, nil for none.
func kindsIn(s *statement.Schema) []statement.Kind {
	if s == nil || s.Kinds == nil {
		return nil
	}
	return s.Kinds.Types
}

// enumIn returns the members s's `enum` lists, nil where it writes none and
// empty where it writes an empty list (enumWritten).
func enumIn(s *statement.Schema) []*yaml.Node {
	if s == nil || s.Values == nil {
		return nil
	}
	return s.Values.Enum
}

// constIn returns the value s's `const` holds, nil where it writes none.
func constIn(s *statement.Schema) *yaml.Node {
	if s == nil || s.Values == nil {
		return nil
	}
	return s.Values.Const
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

// siblingKeyword is a keyword whose presence beside a union or a $dynamicRef
// leaves something for the position to carry, with how a schema writes it, the
// kind of instance it speaks of where it applies to one kind alone, and, for a
// keyword giving the type of a member, an element or a value, where it does,
// which a schema naming no kind is read as (claimedKinds). infers is nil for a
// keyword that never does.
type siblingKeyword struct {
	key    string
	writes func(*statement.Schema) bool
	claims kindSet
	infers func(*statement.Table, *statement.Schema) bool
}

// shapeKeywords are the keywords that declare data shape — a type, a property
// set, a value set, composition, or a catch-all — rather than only narrowing
// what an already-declared shape accepts (docs/compilers/openapi.md#union-beside-structure).
var shapeKeywords = []siblingKeyword{
	{"properties", func(s *statement.Schema) bool { return len(objectIn(s).Properties) > 0 }, kindObject,
		func(_ *statement.Table, s *statement.Schema) bool { return len(objectIn(s).Properties) > 0 }},
	{"const", func(s *statement.Schema) bool { return constIn(s) != nil }, 0, nil},
	{"enum", enumWritten, 0, nil},
	{"allOf", writesConjuncts, 0, nil},
	{"additionalProperties", func(s *statement.Schema) bool {
		return written(objectIn(s).AdditionalProperties)
	}, kindObject, func(t *statement.Table, s *statement.Schema) bool {
		return typesInstances(t, objectIn(s).AdditionalProperties)
	}},
	{"patternProperties", func(s *statement.Schema) bool { return len(objectIn(s).PatternProperties) > 0 }, kindObject,
		func(_ *statement.Table, s *statement.Schema) bool { return len(objectIn(s).PatternProperties) > 0 }},
	{"type", func(s *statement.Schema) bool { return len(effectiveTypes(s)) > 0 }, 0, nil},
}

// narrowingKeywords are the keywords that constrain an instance without
// declaring a shape to build. They sit on the narrowing side of shapeKeywords'
// line, and that line is about what to *build*. Wherever the question is
// instead whether a lowering can carry what the position wrote, the distinction
// does not apply: an ir.Union has no element type, so
// `{oneOf: [...], items: {...}}` loses the `items` as surely as it would lose a
// co-declared property set. An `items` list narrows too, read off the raw node,
// since nothing documents what the parser's model holds there.
var narrowingKeywords = []siblingKeyword{
	{"required", func(s *statement.Schema) bool { return len(objectIn(s).Required) > 0 }, kindObject, nil},
	{"items", func(s *statement.Schema) bool {
		_, listed := listedItems(s)
		return listed || written(arrayIn(s).Items)
	}, kindArray, func(t *statement.Table, s *statement.Schema) bool {
		_, listed := listedItems(s)
		return listed || typesInstances(t, arrayIn(s).Items)
	}},
	{"prefixItems", func(s *statement.Schema) bool { return len(arrayIn(s).PrefixItems) > 0 }, kindArray,
		func(_ *statement.Table, s *statement.Schema) bool { return len(arrayIn(s).PrefixItems) > 0 }},
}

// typesInstances reports whether sub, the schema an `items` or an
// `additionalProperties` writes, gives a type to the instances it applies to: a
// schema object writing a keyword. `true` and `{}` state nothing, as leaving
// the keyword out does (JSON Schema Core 2020-12 §10.3.1.2, §10.3.2.3), and
// `false`, which admits no instance, types none either.
func typesInstances(t *statement.Table, sub statement.Sub) bool {
	return sub.Truth() == statement.Stated && len(t.Sub(sub).Keys) > 0
}

// siblingKeywords returns the keywords of shapeKeywords and narrowingKeywords
// that s writes, in that order.
func siblingKeywords(s *statement.Schema) []string {
	var out []string
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.writes(s) {
				out = append(out, k.key)
			}
		}
	}
	return out
}

// writesSiblingKeyword reports whether siblingKeywords(s) holds any keyword,
// without building the list, as most schemas a reader asks of write one.
func writesSiblingKeyword(s *statement.Schema) bool {
	writes := func(k siblingKeyword) bool { return k.writes(s) }
	return slices.ContainsFunc(shapeKeywords, writes) || slices.ContainsFunc(narrowingKeywords, writes)
}
