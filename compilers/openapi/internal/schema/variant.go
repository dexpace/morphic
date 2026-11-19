// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// buildsVariants reports whether a Union schema lowers to holds variants schema builds
// itself, each conjoining what schema writes: a type array's (lowerUnion) or a
// distributed composition's (lowerDistributedUnion). A union of schema's oneOf or
// anyOf alone (lowerOneOfAnyOf) holds the nodes its branches lowered to
// instead. It reads what lowerSchemaBody dispatches on.
func buildsVariants(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return !declaresUnion(schema) || hasUnionSiblings(loweringContext, schema)
}

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across the variants of unionAtValue: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (docs/ir/models.md#composition-beside-a-union) alongside its
// branch, so the composition is carried on every variant rather than merged
// into one or dropped. A union an allOf branch writes is the branch's to give,
// not residue each variant keeps (allOfComposition.conjoins).
func lowerDistributedUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, unionAtValue unionAt, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var census finished
	unionAtValue.distributed = true
	unionAtValue.dead = deadBranches(loweringContext, schema, unionAtValue.holder)
	unionAtValue.deadRoutes = deadRoutesOf(loweringContext, unionAtValue)
	id := internNode(loweringContext, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		body := composedBody{schema: schema, comp: readAllOf(loweringContext, schema, pointer),
			foldedBranches: positionFoldedBranches(loweringContext, schema, pointer, ir.TypeRef{}), inferred: bodyKindInferred(loweringContext, schema), union: unionAtValue}
		diags = append(diags, diagDisjointConjuncts(loweringContext, body.comp, composedShape)...)
		diags = append(diags, keepDeadBranches(loweringContext, &common.Unmodeled, schema, unionAtValue, pointer)...)
		def, unionDiags := buildUnion(loweringContext, ts, unionAtValue, common,
			func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
				ref, variantDiags := composedVariant(loweringContext, ts, depth, body, b, vptr)
				diags = append(diags, variantDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		// Each variant composes the body, reading its members and its bounds.
		return def, objectKeys | typeReadAs(schema, statement.KindObject) | boundsRead(schema, body.comp.bounds, nil)
	}, keepUnreadAcross(loweringContext, ts, schema, taglessShape(nil, schema, distributedShape), 0, pointer, stand{}, &census, fin))
	// The union read schema's discriminator, so one it cannot hold says why.
	diags = append(diags, census.diags...)
	where := "co-declared with structural keywords"
	if unionAtValue.holder != schema {
		where = "written on an allOf branch conjoins with the rest of the composition"
	}
	return id, append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf %s; the composition is distributed across the union variants", where))
}

// lowerSubtypeUnion lowers schema, a subtype a discriminator on an ancestor routes to,
// with the union unionAtValue it conjoins, beside its allOf or on a branch: schema stays the
// Model its composition lowers to, a subtype of that ancestor, and names as its
// last Mixin a Union minted for unionAtValue (mintUnion), as an allOf naming a component
// holding unionAtValue composes it (docs/ir/models.md#a-union-a-composition-names). A
// discriminator written beside unionAtValue is the Union's, so the Model neither lowers nor
// keeps it, unless the Union cannot hold it (tagLocated). The Model's build
// composes the union (composeMixin), so only a position's first lowering does.
func lowerSubtypeUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, unionAtValue unionAt, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	dispatch := dispatchOf(schema)
	dispatch.mixin = &unionAtValue
	if unionAtValue.disc != nil && unionAtValue.discAt == pointer && tagLocated(unionAtValue.disc, nil) {
		dispatch.said = append(dispatch.said, "discriminator")
	}
	id, diags := lowerElected(loweringContext, ts, depth, schema, pointer, dispatch, fin, stand{})
	if dispatch.won != "allOf" {
		// A discriminator routes to schema through the allOf naming its ancestor
		// (subtypeDiscriminatorValue), so no other election composes it.
		diags = append(diags, loweringContext.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: a routed subtype lowers through its allOf, so its union is composed into no model"))
	}
	where := "co-declared with"
	if unionAtValue.holder != schema {
		where = "written on an allOf branch of"
	}
	return id, append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf %s a schema a discriminator routes to as its subtype; the union is composed as a mixin of the model", where))
}

// mintUnion registers the Union unionAtValue, a routed subtype's union (lowerSubtypeUnion),
// at the keyword its holder writes it under, in the composed namespace: no
// schema stands there, and a variant's path ends in an index, a field's in a
// name. Each variant is its branch's own lowering, as a union written alone
// lowers it, so the node is what a component holding unionAtValue would be.
func mintUnion(loweringContext lowering.Ctx, ts *compile.Types, depth int, unionAtValue unionAt) (ir.TypeID, []ir.Diagnostic) {
	_, key, _ := unionAtValue.branches(loweringContext)
	at := unionAtValue.at + ids.Ptr(key)
	id := loweringContext.IDs().ComposedType(at)
	common := commonFor(loweringContext, id, at)
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(loweringContext, unionAtValue.at), key))
	var diags []ir.Diagnostic
	node, unionDiags := buildUnion(loweringContext, ts, unionAtValue, common, func(b *statement.Statement, vptr jsontext.Pointer) ir.TypeRef {
		ref, refDiags := Ref(loweringContext, ts, depth, b, vptr)
		diags = append(diags, refDiags...)
		return ref
	})
	ts.Register(id, node)
	return id, slices.Concat(diags, unionDiags)
}

// composeMixin mints the union unionAtValue, where not nil, and appends it to the Mixins
// of td, the node a routed subtype's build has just made, last, so the $ref
// entries its source writes classify first. Any node but a Model has no Mixins
// to compose it into, and the lowering that builds it elects one only for a
// subtype that composes, so that is a compiler bug.
func composeMixin(loweringContext lowering.Ctx, ts *compile.Types, depth int, td ir.TypeDef, unionAtValue *unionAt, pointer jsontext.Pointer) []ir.Diagnostic {
	if unionAtValue == nil {
		return nil
	}
	mixin, diags := mintUnion(loweringContext, ts, depth, *unionAtValue)
	model, isModel := td.(*ir.Model)
	if !isModel {
		return append(diags, loweringContext.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: type %q is not the model a routed subtype composes its union into", td.Common().ID))
	}
	model.Mixins = append(model.Mixins, ir.TypeRef{Target: mixin})
	return diags
}

// composedBody is the schema every distributed variant carries: the enclosing
// schema and where it is written.
type composedBody struct {
	schema         *statement.Schema
	comp           allOfComposition               // schema's flattened allOf, built once for every variant
	foldedBranches []annotation.FoldedAllOfBranch // what comp's folding branches say of the position, read once too
	// inferred marks a schema the kind-inference policy made the model its
	// variants compose (bodyKindInferred), so a variant over a branch that
	// declares no object names the policy.
	inferred bool
	// union is the union distributed across the body.
	union unionAt
}

// bodyKindInferred reports whether the kind-inference policy made schema, a body a
// union distributes across, the model its variants compose
// (docs/compilers/openapi.md#shape-applicator): schema names no type and composes
// no allOf, and the union's branches admit a kind that is no object, or name
// theirs by the policy's reading, as untypedKinds tells the two apart.
func bodyKindInferred(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	if !loweringContext.KindInference.Honours() || len(effectiveTypes(schema)) > 0 || allOfComposes(loweringContext, schema) {
		return false
	}
	return !admitsObjectsAlone(loweringContext, loweringContext.Statements().Read(schema))
}

// admitsObjectsAlone reports whether the document itself declares that st
// admits an object and no other kind, null aside: its read is whole and rests
// on no kind the kind-inference policy read.
func admitsObjectsAlone(loweringContext lowering.Ctx, st *statement.Statement) bool {
	admission := admissionOf(loweringContext, st)
	return !admission.inferred && !admission.cut && admission.kinds&^kindObject == 0
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
func composedVariant(loweringContext lowering.Ctx, ts *compile.Types, depth int, body composedBody,
	b *statement.Statement, vptr jsontext.Pointer,
) (ir.TypeRef, []ir.Diagnostic) {
	id := loweringContext.IDs().ComposedType(vptr)
	common := commonFor(loweringContext, id, vptr)
	// The Model conjoins the union's body with the branch, so it is named for
	// both.
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(loweringContext, body.comp.pointer), lowering.NameAt(loweringContext, vptr)))
	if body.inferred && !admitsObjectsAlone(loweringContext, b) {
		common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
	}
	variant, unpacked, composes := branchReferent(loweringContext, b)
	if !composes {
		// The variant is the branch's one home, so the branch lowers nowhere and
		// what it says of null is read off it, reported where the policy reads one.
		def, diags := buildComposedVariant(loweringContext, ts, depth, body, b, "", false, common)
		ts.Register(id, def)
		nulls := newNulls(loweringContext)
		nullReading := nulls.conjunct(b, siteAt(vptr))
		ref := composedVariantRef(loweringContext, id, body.schema, nullReading.declared == nullAdmitted, nullReading.lenient == nullAdmitted)
		return ref, append(diags, nulls.findings(loweringContext)...)
	}
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(loweringContext, ts, depth, b, vptr)
	referent := branch.Target
	if unpacked {
		referent = variant
	}
	def, variantDiags := buildComposedVariant(loweringContext, ts, depth, body, b, referent, true, common)
	ts.Register(id, def)
	ref := composedVariantRef(loweringContext, id, body.schema, branch.Nullable && branch.Inferred == "", branch.Nullable)
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
func composedVariantRef(loweringContext lowering.Ctx, id ir.TypeID, body *statement.Schema, declared, lenient bool) ir.TypeRef {
	if !declared && !lenient {
		return ir.TypeRef{Target: id}
	}
	nulls := newNulls(loweringContext)
	nullReading := nulls.schema(body, nullSite{})
	return nullRef(loweringContext, id, declared && nullReading.declared != nullForbidden, lenient && nullReading.lenient != nullForbidden)
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
func buildComposedVariant(loweringContext lowering.Ctx, ts *compile.Types, depth int, body composedBody, b *statement.Statement,
	branch ir.TypeID, composes bool, common ir.TypeCommon,
) (ir.TypeDef, []ir.Diagnostic) {
	kinds, _ := lowersTo(loweringContext, b)
	cons, diags := ConstraintsOf(loweringContext, &common.Unmodeled, body.schema, body.foldedBranches, body.comp.pointer, body.comp.bounds&kinds)
	model := &ir.Model{TypeCommon: common, Constraints: cons}
	fields := newModelFields(loweringContext, ts, model)
	if composes {
		// The branch is distributed into the body, whose own declarations are
		// read after it, as a model's own declarations follow what it composes.
		fields.compose(branch, model.Provenance.Pointer)
	}
	base, hasBase := selectAllOfBase(loweringContext, body.comp.branches, model.ID)
	diags = append(diags, fillAllOf(loweringContext, ts, depth, model, fields, body.comp, base, hasBase)...)
	diags = append(diags, fillModelProperties(loweringContext, ts, depth, fields, body.schema, body.comp.pointer)...)
	requireMembers(model, compositionRequired(body.schema.ObjectKeywords().Required, body.comp))
	diags = append(diags, fillComposedAdditional(loweringContext, ts, depth, model, body.schema, body.comp)...)
	diags = append(diags, applyFalseBranches(loweringContext, model, body.comp)...)
	_, key, _ := body.union.branches(loweringContext)
	diags = append(diags, preserveUnusedCombinator(loweringContext, &model.Unmodeled, body.union, key)...)
	if !composes {
		// A variant composing nothing over a body that is a map alone is that map.
		diags = append(diags, keepUncomposedBranch(loweringContext, model, b)...)
		return objectNode(model), diags
	}
	conjoinBranch(model, branch)
	// No variant carries a DiscriminatorValue, which tags a hierarchy's
	// subtype: a schema a discriminated ancestor routes to is never distributed
	// (unionRoutedSubtype), and a discriminator beside the union routes its
	// variants by its mapping (spellImplicitRoutes).
	return model, diags
}

// branchReferent reports whether the variant composed over the union branch b
// names a referent as its Base or a Mixin, as an allOf's $ref entry composes
// (composesRef), read before b lowers, so a branch the variant keeps lowers
// nowhere: the node b lowers to where it is an object alone, null aside, or its
// read is cut short; the object variant of a type list b's $ref names, unpacked
// (objectVariant), where b writes nothing beside its $ref, which would bind the
// whole list on no node the variant names; else none, since a model composes
// only models.
func branchReferent(loweringContext lowering.Ctx, b *statement.Statement) (variant ir.TypeID, unpacked, composes bool) {
	if admissionOf(loweringContext, b).cut {
		return "", false, true
	}
	if kinds, _ := lowersTo(loweringContext, b); kinds == kindObject {
		return "", false, true
	}
	variant, unpacked = objectVariant(loweringContext, b)
	if !unpacked || writesMoreThanRef(b.Schema) {
		return "", false, false
	}
	return variant, true, true
}

// keepUncomposedBranch keeps the union branch b, which the variant model does not
// compose (branchReferent), verbatim under model's Unmodeled (unionBranchKey), and
// reports it, as keepUncomposedRefs keeps an allOf's $ref entries. b lowers
// nowhere, so this is its one home; the target of a $ref it writes lowers
// where the $ref lands, as every landing does (ReserveNodesAtReferenceTargets).
func keepUncomposedBranch(loweringContext lowering.Ctx, model *ir.Model, b *statement.Statement) []ir.Diagnostic {
	at := model.Provenance.Pointer
	kept, diags := annotation.PreserveNodeInto(&model.Unmodeled, unionBranchKey(at), b.Node,
		ir.ReasonDegradedLowering, loweringContext.Place().At(at))
	if !kept {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at, "%s",
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
func conjoinBranch(model *ir.Model, branch ir.TypeID) {
	target := ir.TypeRef{Target: branch}
	if model.Base == nil && len(model.Mixins) == 0 {
		model.Base = &target
		return
	}
	model.Mixins = append(model.Mixins, target)
}
