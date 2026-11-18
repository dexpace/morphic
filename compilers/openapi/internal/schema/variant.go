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
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, u unionAt, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var census finished
	u.distributed = true
	u.dead = deadBranches(c, s, u.holder)
	u.deadRoutes = deadRoutesOf(c, u)
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
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
		// Each variant composes the body, reading its members and its bounds.
		return def, objectKeys | typeReadAs(s, statement.KindObject) | boundsRead(s, body.comp.bounds, nil)
	}, census.onRead(keepUnreadAcross(c, ts, s, taglessShape(nil, s, distributedShape), 0, pointer)).then(fin))
	// The union read s's discriminator, so one it cannot hold says why.
	diags = append(diags, census.diags...)
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
// discriminator written beside u is the Union's, so the Model neither lowers nor
// keeps it, unless the Union cannot hold it (tagLocated). The Model's build
// composes the union (composeMixin), so only a position's first lowering does.
func lowerSubtypeUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, u unionAt, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	d := dispatchOf(s)
	d.mixin = &u
	if u.disc != nil && u.discAt == pointer && tagLocated(u.disc, nil) {
		d.said = append(d.said, "discriminator")
	}
	id, diags := lowerElected(c, ts, depth, s, pointer, d, fin, stand{})
	if d.won != "allOf" {
		// A discriminator routes to s through the allOf naming its ancestor
		// (subtypeDiscriminatorValue), so no other election composes it.
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: a routed subtype lowers through its allOf, so its union is composed into no model"))
	}
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
	_, key, _ := u.branches(c)
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

// composeMixin mints the union u, where not nil, and appends it to the Mixins
// of td, the node a routed subtype's build has just made, last, so the $ref
// entries its source writes classify first. Any node but a Model has no Mixins
// to compose it into, and the lowering that builds it elects one only for a
// subtype that composes, so that is a compiler bug.
func composeMixin(c lowering.Ctx, ts *compile.Types, depth int, td ir.TypeDef, u *unionAt, pointer jsontext.Pointer) []ir.Diagnostic {
	if u == nil {
		return nil
	}
	mixin, diags := mintUnion(c, ts, depth, *u)
	m, isModel := td.(*ir.Model)
	if !isModel {
		return append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: type %q is not the model a routed subtype composes its union into", td.Common().ID))
	}
	m.Mixins = append(m.Mixins, ir.TypeRef{Target: mixin})
	return diags
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
	requireMembers(m, compositionRequired(body.schema.ObjectKeywords().Required, body.comp))
	diags = append(diags, fillComposedAdditional(c, ts, depth, m, body.schema, body.comp)...)
	diags = append(diags, applyFalseBranches(c, m, body.comp)...)
	_, key, _ := body.union.branches(c)
	diags = append(diags, preserveUnusedCombinator(c, &m.Unmodeled, body.union, key)...)
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
// reports it, as keepUncomposedRefs keeps an allOf's $ref entries. b lowers
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
