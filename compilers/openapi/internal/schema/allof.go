// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerAllOf hoists an allOf schema as a Model, classifying each branch per
// docs/compilers/openapi.md#allof-classification, which also says which
// branches are flattened first. Inline branches contribute their properties,
// with provenance into their source branch.
//
// Value constraints written beside the allOf (minProperties, ...), or on a
// branch that folds, are read as lowerModel reads a plain object's, on the same
// field, not Unmodeled (GitHub #407). A composition that leaves only a
// catch-all is a map, as a plain object's is (objectNode). Where discriminated
// is false, a union the model composes, mixin where not nil, took schema's
// discriminator (lowerSubtypeUnion). fin finishes the node.
func lowerAllOf(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, discriminated bool, mixin *unionAt,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(loweringContext, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		// The one flattened list; the readers composeModel calls take it, not
		// schema.
		td, read, modelDiags := composeModel(loweringContext, ts, depth, schema, pointer, discriminated, common, readAllOf(loweringContext, schema, pointer))
		diags = modelDiags
		diags = append(diags, composeMixin(loweringContext, ts, depth, td, mixin, pointer)...)
		return td, read
	}, fin)
	return id, diags
}

// composeModel builds the Model of schema, written at pointer, composing comp,
// its flattened conjuncts, and returns what it read of schema.
func composeModel(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer,
	discriminated bool, common ir.TypeCommon, comp allOfComposition,
) (ir.TypeDef, keySet, []ir.Diagnostic) {
	if admissionOf(loweringContext, loweringContext.Statements().Read(schema)).inferred {
		// No conjunct declares the kind: the model is one by the
		// kind-inference policy's reading of their shape keywords.
		common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
	}
	cons, diags := ConstraintsOf(loweringContext, &common.Unmodeled, schema, positionFoldedBranches(loweringContext, schema, pointer, ir.TypeRef{}), pointer, comp.bounds)
	model := &ir.Model{TypeCommon: common, Constraints: cons}
	diags = append(diags, diagDisjointConjuncts(loweringContext, comp, composedShape)...)
	fields := newModelFields(loweringContext, ts, model)
	base, hasBase := selectAllOfBase(loweringContext, comp.branches, common.ID)
	diags = append(diags, fillAllOf(loweringContext, ts, depth, model, fields, comp, base, hasBase)...)
	diags = append(diags, fillModelProperties(loweringContext, ts, depth, fields, schema, pointer)...)
	requireMembers(model, compositionRequired(schema.ObjectKeywords().Required, comp))
	diags = append(diags, fillComposedAdditional(loweringContext, ts, depth, model, schema, comp)...)
	diags = append(diags, applyFalseBranches(loweringContext, model, comp)...)
	if discriminated {
		discriminator, discDiags := lowerDiscriminator(loweringContext, ts, schema.Discriminator(), pointer, model, nil)
		diags = append(diags, discDiags...)
		model.Discriminator = discriminator
	}
	var first *statement.Schema // the base's hierarchy tags the model first
	if hasBase {
		first = refBranchTarget(loweringContext, comp.branches[base].schema)
	}
	tag, tagDiags := subtypeDiscriminatorValue(loweringContext, schema, common.ID, pointer, first)
	model.DiscriminatorValue = tag.value
	diags = append(diags, tagDiags...)
	deferSubtypeTag(loweringContext, ts, tag, common.ID, pointer)
	return objectNode(model), modelReads(schema, model, comp.bounds), diags
}

// compositionCensusHandled names the census keywords an allOf composition
// reads itself, so neither the census of a $ref branch nor that of the composed
// position reports them again: lowerAllOf lands every required list on the
// composed model (requireMembers).
var compositionCensusHandled = keyBit("required")

// allOfBranch is one inline-or-ref branch of an allOf composition after nested
// wrappers are flattened into the list (GitHub #599).
type allOfBranch struct {
	schema *statement.Statement
	// pointer is where the branch was written, never rewritten to a flat index:
	// property IDs, provenance, diagnostics, the branch's Unmodeled key
	// (branchKey) and its name (lowering.NameAt) all derive from it.
	pointer jsontext.Pointer
	// wrapper marks a flattened wrapper, which contributes its properties and
	// required list, and is kept verbatim only for what else it writes.
	wrapper bool
	// capped marks an inline branch whose allOf was not flattened because it sits
	// past statement.MaxFlattenDepth, so the diagnostic can say why it was kept.
	capped bool
}

// allOfComposition is the flattened allOf of the schema written at pointer: its
// branches, each with the pointer it was written at. A reader that takes it
// cannot reach the unflattened list.
type allOfComposition struct {
	pointer  jsontext.Pointer
	branches []allOfBranch
	// disjoint names what each conjunct states where the schema draws the
	// warning docs/compilers/openapi.md#allof-disjoint-conjuncts describes, and
	// is nil otherwise (readAllOf).
	disjoint []string
	// branchFoldsIntoPosition marks each branch that folds at the position
	// (foldingAllOfBranches), and rebased each whose folding keywords write a
	// relative $ref it reads against another base, so it does not. They are nil
	// where the allOf is not what the position lowers (allOfDecides).
	branchFoldsIntoPosition []bool
	rebased                 []bool
	// conjoins is the pointer of the branch whose union the position lowers,
	// distributing it across its variants (branchUnion), whose merge consumes it,
	// and empty where no branch's is.
	conjoins jsontext.Pointer
	// bounds is the kinds whose value bounds the Model composing the branches
	// reads, those of the types the position admits (boundKinds) that it takes
	// (modelTakes), so a branch's bound folds where they take it.
	bounds kindSet
	// closer names the schema object whose additionalProperties and
	// patternProperties the Model composing the branches types (closerOf).
	closer closure
	// kept is what each inline branch keeps verbatim beside the Model
	// composing them, by index (entryKeeps).
	kept []keptEntry
}

// foldsIntoPositionAt reports whether the branch at index i folds at the
// position.
func (a allOfComposition) foldsIntoPositionAt(i int) bool {
	return a.branchFoldsIntoPosition != nil && a.branchFoldsIntoPosition[i]
}

// allOfBranches returns the branches of schema's allOf as its statement
// flattens them (statement.Statement.Entries), each with the pointer it was
// written at, under the schema written at pointer. allOf is associative, so the
// list classifies exactly as its flat spelling would.
func allOfBranches(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	entries := loweringContext.Statements().Read(schema).Entries
	out := make([]allOfBranch, 0, len(entries))
	for _, allOfConjunct := range entries {
		out = append(out, allOfBranch{schema: loweringContext.Statements().Read(allOfConjunct.Schema), pointer: pointer + allOfConjunct.Rel, wrapper: allOfConjunct.Wrapper, capped: allOfConjunct.Capped})
	}
	return allOfComposition{pointer: pointer, branches: out}
}

// readAllOf is allOfBranches of the schema written at pointer, with what its
// conjuncts jointly admit read and, where the allOf is what the position
// lowers, which branches fold there (allOfBranchFoldingOf) and which writes the
// union the position lowers (branchUnion), for a lowering that composes it.
// That gate is positionFoldedBranches', so a branch gives up a keyword only
// where a reader of the position takes it up; both composers pass it, and the
// report of an allOf the election passed over does not.
func readAllOf(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfBranches(loweringContext, schema, pointer)
	comp.disjoint = disjointNames(loweringContext, schema, comp)
	comp.bounds = modelTakes(boundKinds(loweringContext, schema))
	comp.closer = closerOf(loweringContext, schema, comp)
	if allOfDecides(loweringContext, schema) {
		allOfBranchFolding := allOfBranchFoldingOf(loweringContext, schema)
		comp.branchFoldsIntoPosition, comp.rebased = allOfBranchFolding.branchFoldsIntoPosition, allOfBranchFolding.rebased
	}
	if unionAt, _, ok := branchUnion(loweringContext, schema, pointer); ok {
		comp.conjoins = unionAt.at
	}
	comp.kept = entryKeeps(loweringContext, schema, comp, func(i int) []string { return compositionResidue(loweringContext, comp, i) },
		allOfBranchFolding{branchFoldsIntoPosition: comp.branchFoldsIntoPosition, rebased: comp.rebased}, catchAllKeyword, nil)
	return comp
}

// allOfRefBranches yields the statements of the $ref branches of schema's allOf
// after flattening: the view an ancestor walk needs, which reads no pointer.
func allOfRefBranches(loweringContext lowering.Ctx, schema *statement.Schema) iter.Seq[*statement.Statement] {
	return func(yield func(*statement.Statement) bool) {
		for _, allOfConjunct := range loweringContext.Statements().Read(schema).Entries {
			if allOfConjunct.Ref != nil && !yield(loweringContext.Statements().Read(allOfConjunct.Schema)) {
				return
			}
		}
	}
}

// FlatBranch is one branch of the allOf FlattenedAllOf reads.
type FlatBranch struct {
	// Schema is the branch as written, or the wrapper a run of its own entries
	// ended with.
	Schema *statement.Statement
	// Ref marks a branch that names its schema by $ref, which the reader follows
	// itself and bounds on its own.
	Ref bool
	// Capped marks an inline branch whose own allOf sits past
	// statement.MaxFlattenDepth and was not flattened, so the reader has not reached
	// what it declares.
	Capped bool
}

// FlattenedAllOf returns schema's allOf as the lowering reads it, so a reader
// outside this package takes the same flattening and the same cap, in the same
// order (docs/compilers/openapi.md#allof-classification). It reads the entries
// allOfBranches does.
func FlattenedAllOf(loweringContext lowering.Ctx, schema *statement.Schema) []FlatBranch {
	entries := loweringContext.Statements().Read(schema).Entries
	out := make([]FlatBranch, 0, len(entries))
	for _, allOfConjunct := range entries {
		out = append(out, FlatBranch{Schema: loweringContext.Statements().Read(allOfConjunct.Schema), Ref: allOfConjunct.Ref != nil, Capped: allOfConjunct.Capped})
	}
	return out
}

// branchKey is the Unmodeled key an allOf branch at bptr is kept under: "openapi:"
// and its path from the composed schema. The path's tokens are `allOf` and
// decimal indices, so trimming the prefix is exact.
func (a allOfComposition) branchKey(bptr jsontext.Pointer) string {
	return "openapi:" + a.branchPath(bptr)
}

// branchPath is the path of the allOf branch at bptr from the composed schema,
// "$ref" for the reference the composed schema writes beside its keywords.
func (a allOfComposition) branchPath(bptr jsontext.Pointer) string {
	if bptr == a.pointer {
		return "$ref"
	}
	return strings.TrimPrefix(string(bptr), string(a.pointer)+"/")
}

// subject names the schema writing a's conjuncts in a report: "this allOf",
// or, where the schema writes a $ref beside its keywords, the conjunct that
// reference is, or no allOf at all, "this schema", which writes none to name.
func (a allOfComposition) subject() string {
	if len(a.branches) == 0 || a.branches[0].pointer == a.pointer {
		return "this schema"
	}
	return "this allOf"
}

// compositionRequired collects every required-property name declared across an
// allOf composition, in source order: each branch's required list (a flattened
// wrapper's included), then own, the list the composed schema writes itself. A
// branch is read from its local schema, never its resolved $ref target — that
// required list belongs to the target's own model (issue #29).
func compositionRequired(own []string, comp allOfComposition) []string {
	var out []string
	for _, allOfBranch := range comp.branches {
		// A boolean branch writes no object keyword, so ObjectKeywords reads none.
		out = append(out, allOfBranch.schema.ObjectKeywords().Required...)
	}
	return append(out, own...)
}

// fillAllOf classifies and lowers the branches of comp into model (GitHub #599).
//
// An inline branch merges in place: its `properties` and `required`
// (requireMembers) merge, its catch-all keywords where it closes the model
// (closerOf), what it writes for the position folds there
// (foldingAllOfBranches), and the rest stays verbatim beside the model
// (preserveUnmergedBranch, GitHub
// #123).
//
// A $ref branch is the reference alone, composing what it names
// (fillRefBranch). The keywords written beside its $ref are an inline branch
// of their own at its pointer, merged as above, a oneOf or anyOf among them
// lowering as on any branch (docs/ir/models.md#composition-entries).
func fillAllOf(loweringContext lowering.Ctx, ts *compile.Types, depth int, model *ir.Model, fields modelFields, comp allOfComposition,
	base int, hasBase bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var failed []int
	var uncomposed []keptBranch
	for i, allOfBranch := range comp.branches {
		switch {
		case allOfBranch.schema.Ref == nil:
			diags = append(diags, fillModelProperties(loweringContext, ts, depth, fields, allOfBranch.schema.Schema, allOfBranch.pointer)...)
			diags = append(diags, preserveUnmergedBranch(loweringContext, ts, model, comp, i)...)
		case composesRef(loweringContext, allOfBranch):
			refDiags, resolved := fillRefBranch(loweringContext, ts, fields, allOfBranch, hasBase && i == base)
			diags = append(diags, refDiags...)
			if !resolved {
				failed = append(failed, i)
			}
		default:
			uncomposed = append(uncomposed, keptBranch{entry: allOfBranch})
		}
	}
	apart, failedDiags := keepFailedRefs(loweringContext, ts, model, comp, failed)
	diags = append(diags, failedDiags...)
	return append(diags, keepUncomposedRefs(loweringContext, ts, model, comp.pointer, apart, uncomposed)...)
}

// composesRef reports whether the $ref entry allOfBranchValue is a Base or a
// Mixin of the composition: the schema it lands on lowers to an object alone,
// null aside (lowersTo), or to the union of a type list holding an object,
// whose object variant it composes (objectVariant), or its read is cut short,
// which leaves the composition as it is with no read. A model composes only
// models (docs/compilers/openapi.md#allof-classification).
func composesRef(loweringContext lowering.Ctx, allOfBranchValue allOfBranch) bool {
	target, _ := declaredOf(loweringContext, allOfBranchValue.schema)
	if target == nil || admissionOf(loweringContext, target).cut {
		return true
	}
	if kinds, _ := lowersTo(loweringContext, target); kinds == kindObject {
		return true
	}
	_, unpacked := objectVariant(loweringContext, allOfBranchValue.schema)
	return unpacked
}

// objectVariant returns the ID of the object variant of the union the $ref
// conjunct ref lands on, an allOf entry or a distributed union's branch, where
// that schema lowers by a type list holding an object (lowerUnion), and false
// for any other: the variant stands at the list's position, one per type, so
// it is named before the union is built. A union a oneOf or anyOf writes may
// hold several objects, or none a model is, and is not unpacked.
func objectVariant(loweringContext lowering.Ctx, ref *statement.Statement) (ir.TypeID, bool) {
	target, at := declaredOf(loweringContext, ref)
	if target == nil || target.Ref != nil || declaresUnion(target.Schema) {
		return "", false
	}
	in, placed := lowering.InDocument(loweringContext, at.Doc)
	i := slices.Index(effectiveTypes(target.Schema), statement.KindObject)
	if !placed || i < 0 || !lowersTypeArray(in, target.Schema, at.Pointer) {
		return "", false
	}
	return in.IDs().ForPointer(at.Pointer + ids.Ptr("type", strconv.Itoa(i))), true
}

// keepUncomposedRefs keeps beside model the $ref entries of its composition,
// written at pointer, it does not compose: apart, those whose reference names
// nothing (keepFailedRefs), and uncomposed, those whose target is no object
// alone, ranked together (keepRefConjuncts). It reports each uncomposed one
// kept, resolving its target so the document holds what it names; a failed
// one is reported where it is written.
func keepUncomposedRefs(loweringContext lowering.Ctx, ts *compile.Types, model *ir.Model, pointer jsontext.Pointer,
	apart, uncomposed []keptBranch,
) []ir.Diagnostic {
	for _, b := range uncomposed {
		resolveSchemaRef(loweringContext, ts, b.entry.schema)
	}
	kept, diags := keepRefConjuncts(loweringContext, &model.Unmodeled, slices.Concat(apart, uncomposed), pointer)
	for _, b := range uncomposed {
		if kept[b.entry.pointer] {
			diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s",
				"the schema this $ref names is not an object alone, and a model composes only models, so the $ref "+
					"is kept verbatim under Unmodeled"))
		}
	}
	return diags
}

// fillRefBranch lowers one $ref allOf branch and adds it to the model fields
// builds as its Base, when isBase, or as a Mixin, recording the entry
// (modelFields.compose): what it names, or the object variant of the type
// list it names (objectVariant). A $ref it cannot resolve adds nothing and
// reports false, which keepFailedRefs answers; it is reported unless it lies on a
// loop of $refs, which load reports once (onLoop).
func fillRefBranch(loweringContext lowering.Ctx, ts *compile.Types, fields modelFields, allOfBranchValue allOfBranch, isBase bool) ([]ir.Diagnostic, bool) {
	var diags []ir.Diagnostic
	b := allOfBranchValue.schema
	id, ok := resolveSchemaRef(loweringContext, ts, b)
	if !ok {
		if !onLoop(b) {
			keyword, ref := refSpelling(b)
			diags = append(diags, loweringContext.DiagAt(ir.SeverityError, diag.UnresolvedRef, allOfBranchValue.pointer,
				"unresolved allOf %s %q%s", keyword, ref, unresolvedWhy(loweringContext, b)))
		}
		return diags, false
	}
	if variant, unpacked := objectVariant(loweringContext, allOfBranchValue.schema); unpacked {
		id = variant
	}
	unhomed := unreadByReference(loweringContext, b.Schema, compositionCensusHandled)
	var kept finished
	keep := keepUnhomedOn(loweringContext, ts, b.Schema, unhomed, refSiteShape, allOfBranchValue.pointer, stand{}, &kept, nil)
	ref, homeDiags := homeDeclaration(loweringContext, ts, b.Schema, ir.TypeRef{Target: id}, allOfBranchValue.pointer, annotation.HomeOwnNode, len(unhomed) > 0, keep)
	diags = append(diags, homeDiags...)
	diags = append(diags, kept.diags...)
	if isBase {
		fields.m.Base = &ref
	} else {
		fields.m.Mixins = append(fields.m.Mixins, ref)
	}
	fields.compose(ref.Target, allOfBranchValue.pointer)
	return diags, true
}

// applyFalseBranches closes model for each boolean `false` allOf branch and
// keeps the branch verbatim under Unmodeled. It runs after
// fillComposedAdditional, whose result it overrides, the catch-all included
// (docs/compilers/openapi.md#allof-false-branch).
//
// A `false` branch admits nothing, so model must not stay open; the rule
// docs/compilers/openapi.md#whole-schema-fallbacks closes a bare `false` schema
// the same way. model keeps the other branches' contributions, because emptying
// it would trade one silent loss for another. The verbatim branch tells this
// from a plain `additionalProperties: false`; a diagnostic does not, being no
// part of the document. A `true` branch constrains nothing and is skipped.
func applyFalseBranches(loweringContext lowering.Ctx, model *ir.Model, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, allOfBranch := range comp.branches {
		if !allOfBranch.schema.Truth.ReadsFalse() {
			continue // `true`, a null and any schema with a body close nothing.
		}
		model.Additional = ir.AdditionalClosed
		annotation.PreserveInto(&model.Unmodeled, comp.branchKey(allOfBranch.pointer),
			ir.RawValue("false"), ir.ReasonDegradedLowering, loweringContext.Place().At(allOfBranch.pointer))

		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.FalseSchema, allOfBranch.pointer,
			"boolean false allOf branch matches nothing, so the composition matches nothing; "+
				"composed model closed and the branch kept verbatim under Unmodeled"))
	}

	closer := comp.closer
	if closer.schema != nil && model.Additional == ir.AdditionalClosed && model.AdditionalProperties != nil && model.AdditionalProperties.Value != nil {
		_, keptDiags := annotation.PreserveSchemaKeyword(&model.Unmodeled, closer.schema, "additionalProperties",
			ir.ReasonDegradedLowering, closer.at, loweringContext.Place())
		diags = append(diags, keptDiags...)
	}
	forbidRest(model)
	return diags
}

// preserveUnmergedBranch keeps what the inline allOf branch at index i of comp
// writes that the merge consumes nowhere beside the composed model, as comp.kept
// says: each keyword under its own key and reason, or the branch whole under a
// key comp gives it, so sibling branches never overwrite one another
// (docs/compilers/openapi.md#allof-inline-residue).
func preserveUnmergedBranch(loweringContext lowering.Ctx, ts *compile.Types, model *ir.Model, comp allOfComposition, i int) []ir.Diagnostic {
	keptEntryValue := comp.kept[i]
	disjoint := len(comp.disjoint) > 0
	warn := branchExcludesObject(keptEntryValue.entry.schema.Schema) && !disjoint
	return recordKeptEntry(loweringContext, ts, &model.Unmodeled, comp, keptEntryValue, nodeShape(ir.KindModel), warn, func(otherKeptEntry keptEntry) ir.Diagnostic {
		return diagUnmergedBranch(loweringContext, otherKeptEntry, disjoint)
	})
}

// compositionResidue returns what the inline allOf branch at index i of comp
// writes that the in-place merge reads nowhere: not its properties and
// required, nor what folds where comp says it speaks, nor the union the
// position lowers, nor the catch-all of the branch closing the model. A
// flattened branch's allOf is consumed: allOfBranches replaced it by its own
// entries. A boolean branch has no keywords to leave a residue
// (applyFalseBranches), and a $ref branch homes its siblings on an alias over
// its target (fillRefBranch), unless it names nothing (failedRefResidue).
func compositionResidue(loweringContext lowering.Ctx, comp allOfComposition, i int) []string {
	allOfBranch := comp.branches[i]
	if allOfBranch.schema.Truth != statement.Stated {
		return nil
	}
	if allOfBranch.schema.Ref != nil {
		return failedRefResidue(loweringContext, allOfBranch)
	}
	speaks := comp.foldsIntoPositionAt(i)
	conjoined := comp.conjoins != "" && allOfBranch.pointer == comp.conjoins
	closes := comp.closer.entryAt(allOfBranch.pointer)
	return unmergedBranchKeys(allOfBranch.schema.Schema, func(keyword string) bool {
		return mergeConsumes(allOfBranch.schema.Schema, keyword, allOfBranch.wrapper) || (speaks && folds(loweringContext, allOfBranch.schema.Schema, keyword, boundHomes{value: comp.bounds})) ||
			(conjoined && conjoinedKeyword(keyword)) || (closes && catchAllKeyword(keyword))
	})
}

// diagUnmergedBranch builds the diagnostic announcing the branch keptEntryValue
// keeps whole, naming the keywords so two branches of the same schema are told
// apart. The caller records it.
//
// A branch declaring a type that cannot be an object is a warning rather than
// info: the composed ir.Model asserts the value *is* an object, which the branch
// contradicts. Any other residue only narrows a model the IR describes correctly,
// the under-constrained case (docs/compilers/openapi.md#allof-inline-residue).
// disjoint says the schema reports its conjuncts share no value, so this
// reports residue alone.
func diagUnmergedBranch(loweringContext lowering.Ctx, keptEntryValue keptEntry, disjoint bool) ir.Diagnostic {
	allOfBranch, residue := keptEntryValue.entry, keptEntryValue.residue
	if branchExcludesObject(allOfBranch.schema.Schema) && !disjoint {
		branchNames := namesOf(allOfBranch)
		msg := branchNames.lead + " declares a type that is not an object, which the composed model asserts it is; " + branchNames.kept + " (" +
			strings.Join(keptKeywords(residue, allOfBranch), ", ") + ") is kept verbatim under Unmodeled"
		if allOfBranch.capped {
			msg += ", and its own allOf, nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) + ", was not flattened"
		}
		return loweringContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, allOfBranch.pointer, "%s", msg)
	}
	if why := wholeWhy(keptEntryValue); why != "" && !allOfBranch.capped {
		return loweringContext.DiagAt(ir.SeverityInfo, wholeCode(keptEntryValue), allOfBranch.pointer, "%s", why)
	}
	return loweringContext.DiagAt(ir.SeverityInfo, wholeCode(keptEntryValue), allOfBranch.pointer, "%s", unmergedWhy(residue, allOfBranch))
}

// keptKeywords is residue as a diagnostic names it: a capped branch is kept for
// its depth, so its allOf is not listed as a keyword the merge declines.
func keptKeywords(residue []string, allOfBranchValue allOfBranch) []string {
	if !allOfBranchValue.capped {
		return residue
	}
	return slices.DeleteFunc(slices.Clone(residue), func(k string) bool { return k == "allOf" })
}

// unmergedWhy words why a branch with residue is kept: what merges and folds is
// gone from residue, which is the rest. A flattened branch's nested allOf composes
// too. A capped one is kept for its depth, not for allOf being a keyword the merge
// declines, so allOf is left out of the keywords it lists.
func unmergedWhy(residue []string, allOfBranchValue allOfBranch) string {
	const keptTail = " is kept verbatim under Unmodeled"
	branchNames := namesOf(allOfBranchValue)
	switch {
	case allOfBranchValue.wrapper:
		return branchNames.lead + " is merged in place, its nested allOf composes, and nothing reads the rest of it; " + branchNames.kept + " (" +
			strings.Join(residue, ", ") + ")" + keptTail
	case allOfBranchValue.capped:
		rest := keptKeywords(residue, allOfBranchValue)
		why := branchNames.lead + " is nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) +
			", so its own allOf was not flattened, and " + branchNames.kept
		if len(rest) > 0 {
			why += " (" + strings.Join(rest, ", ") + ")"
		}
		return why + keptTail
	default:
		return branchNames.lead + " is merged in place, and nothing reads the rest of it; " + branchNames.kept + " (" +
			strings.Join(residue, ", ") + ")" + keptTail
	}
}

// unmergedBranchKeys returns the keywords the branch declares that consumes does
// not claim, in the order the parser's model holds them.
//
// The keys are the branch's own (statement.Schema.Keys) rather than a list of
// keywords worth preserving, which is what keeps this from rotting: a keyword a
// later dialect adds counts as unconsumed the moment a source writes it, with
// nobody to remember it here.
func unmergedBranchKeys(bs *statement.Schema, consumes func(key string) bool) []string {
	out := make([]string, 0, len(bs.Keys))
	for _, key := range bs.Keys {
		if consumes(key.Name) || annotation.WritesKeywordLeftOut(bs, key.Name) {
			continue
		}
		out = append(out, key.Name)
	}
	return out
}

// mergeConsumes reports whether fillAllOf's in-place merge reads branch keyword
// key. It is the complete statement of what the merge keeps; everything else the
// branch wrote is residue. `allOf` is consumed exactly when flattened
// (docs/compilers/openapi.md#allof-classification); otherwise it is residue like
// the rest.
func mergeConsumes(bs *statement.Schema, key string, flattened bool) bool {
	switch key {
	case "allOf":
		return flattened // allOfBranches replaced the branch by its own entries
	case "properties":
		return true // fillModelProperties merges these onto m
	case "required":
		return true // requireMembers lands these on m
	case "type":
		return declaresObjectOnly(bs) // a bare `object` only restates the composed Model
	default:
		return false
	}
}

// conjoinedKeyword reports whether keyword is one a branch whose union the
// position lowers gives up to that union: its oneOf or anyOf, and the
// discriminator it writes, which is the union's where the position lowers it
// (findBranchUnion).
func conjoinedKeyword(keyword string) bool {
	return keyword == "oneOf" || keyword == "anyOf" || keyword == "discriminator"
}

// declaresObjectOnly reports whether the branch's declared type is exactly
// `object`, which the composed ir.Model already restates so the merge loses
// nothing by reading it. A set — `[object, "null"]` included — says more than
// that, and the more is what the merge would drop.
func declaresObjectOnly(bs *statement.Schema) bool {
	types := bs.Types()
	return len(types) == 1 && types[0] == statement.KindObject
}

// branchExcludesObject reports whether the branch declares a type set with no
// `object` in it. An undeclared type set excludes nothing.
func branchExcludesObject(bs *statement.Schema) bool {
	return writesType(bs) && !slices.Contains(bs.Types(), statement.KindObject)
}

// selectAllOfBase returns the index into branches, the conjuncts of the
// composition whose node is id, of the one that becomes Model.Base, and false
// when none qualifies (multiple non-hierarchy refs stay Mixins): of the $refs
// the composition takes (composesRef), the one, or of several, of those whose
// $ref lands on a schema taking part in a discriminator hierarchy
// (inHierarchy), one whose hierarchy's mapping routes id before one whose does
// not, then the one whose reference text is the least, so no order written
// decides.
func selectAllOfBase(loweringContext lowering.Ctx, branches []allOfBranch, id ir.TypeID) (int, bool) {
	refIdxs := make([]int, 0, len(branches))
	for i, allOfBranch := range branches {
		if allOfBranch.schema.Ref != nil && composesRef(loweringContext, allOfBranch) {
			refIdxs = append(refIdxs, i)
		}
	}
	// Ranked by whether the hierarchy routes id, by reference text, then by
	// pointer, for a $ref written twice.
	// Only a target in a hierarchy can be elected, so only its routes are read:
	// a walk of the ancestors per $ref costs a wide composition naming no
	// hierarchy a walk of the whole graph each.
	routes := make(map[int]bool, len(refIdxs))
	for _, i := range refIdxs {
		target := refBranchTarget(loweringContext, branches[i].schema)
		routes[i] = len(refIdxs) > 1 && stated(target) && inHierarchy(loweringContext, target) && routesTo(loweringContext, target, id)
	}
	slices.SortStableFunc(refIdxs, func(i, j int) int {
		_, a := refSpelling(branches[i].schema)
		_, b := refSpelling(branches[j].schema)
		return cmp.Or(compareTrueFirst(routes[i], routes[j]), strings.Compare(a, b),
			strings.Compare(string(branches[i].pointer), string(branches[j].pointer)))
	})
	return merge.SelectBase(refIdxs, func(i int) bool {
		target := refBranchTarget(loweringContext, branches[i].schema)
		return stated(target) && inHierarchy(loweringContext, target)
	})
}

// routesTo reports whether the mapping of a discriminator schema, a stated
// schema, or one of its discriminated ancestors declares names id, the node a
// composition of schema lowers to; an empty id is no node, which none names.
func routesTo(loweringContext lowering.Ctx, schema *statement.Schema, id ir.TypeID) bool {
	return slices.ContainsFunc(append([]*statement.Schema{schema}, discriminatedAncestors(loweringContext, schema)...), func(a *statement.Schema) bool {
		discriminator := a.Discriminator()
		return discriminator != nil && len(mappingTagsFor(loweringContext, discriminator, id)) > 0
	})
}

// compareTrueFirst orders true before false.
func compareTrueFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}

// inHierarchyOf keys what inHierarchy read of one schema.
type inHierarchyOf struct {
	schema *statement.Schema
}

// inHierarchy reports whether schema takes part in a discriminator hierarchy:
// it declares a discriminator, or composes through allOf, at any depth, a
// schema that does, the ancestors whose routes tag it (discriminatedAncestors).
// It reads the source, so no lowering order decides it, and every composition
// naming schema asks, so it is read once a run.
func inHierarchy(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return lowering.ReadOncePerRun(loweringContext, inHierarchyOf{schema}, func() bool {
		return schema.Discriminator() != nil || len(discriminatedAncestors(loweringContext, schema)) > 0
	})
}
