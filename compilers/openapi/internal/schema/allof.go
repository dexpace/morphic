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

// lowerAllOf hoists an allOf schema as a Model, classifying each entry per
// docs/compilers/openapi.md#allof-classification, which also says which
// entries are flattened first. Inline entries contribute their properties,
// with provenance into their source entry.
//
// Value constraints written beside the allOf (minProperties, ...), or on an entry
// that folds, are read as lowerModel reads a plain object's, on the same field,
// not Unmodeled (GitHub #407). A composition that leaves only a
// catch-all is a map, as a plain object's is (objectNode). Where discriminated is
// false, a union the model composes, mixin where not nil, took s's discriminator
// (lowerSubtypeUnion). fin finishes the node.
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
	cons, diags := ConstraintsOf(loweringContext, &common.Unmodeled, schema, positionFoldedEntries(loweringContext, schema, pointer, ir.TypeRef{}), pointer, comp.bounds)
	model := &ir.Model{TypeCommon: common, Constraints: cons}
	diags = append(diags, diagDisjointConjuncts(loweringContext, comp, composedShape)...)
	fields := newModelFields(loweringContext, ts, model)
	base, hasBase := selectAllOfBase(loweringContext, comp.entries, common.ID)
	diags = append(diags, fillAllOf(loweringContext, ts, depth, model, fields, comp, base, hasBase)...)
	diags = append(diags, fillModelProperties(loweringContext, ts, depth, fields, schema, pointer)...)
	requireMembers(model, compositionRequired(schema.ObjectKeywords().Required, comp))
	diags = append(diags, fillComposedAdditional(loweringContext, ts, depth, model, schema, comp)...)
	diags = append(diags, applyFalseEntries(loweringContext, model, comp)...)
	if discriminated {
		discriminator, discDiags := lowerDiscriminator(loweringContext, ts, schema.Discriminator(), pointer, model, nil)
		diags = append(diags, discDiags...)
		model.Discriminator = discriminator
	}
	var first *statement.Schema // the base's hierarchy tags the model first
	if hasBase {
		first = refBranchTarget(loweringContext, comp.entries[base].schema)
	}
	tag, tagDiags := subtypeDiscriminatorValue(loweringContext, schema, common.ID, pointer, first)
	model.DiscriminatorValue = tag.value
	diags = append(diags, tagDiags...)
	deferSubtypeTag(loweringContext, ts, tag, common.ID, pointer)
	return objectNode(model), modelReads(schema, model, comp.bounds), diags
}

// compositionReads names the readDomain keywords an allOf composition reads
// itself, so neither the keeping of a $ref entry's unread keywords nor that of
// the composed position reports them again: lowerAllOf lands every required
// list on the composed model (requireMembers).
var compositionReads = keyBit("required")

// allOfEntry is one inline-or-ref entry of an allOf composition after nested
// wrappers are flattened into the list (GitHub #599).
type allOfEntry struct {
	schema *statement.Statement
	// pointer is where the entry was written, never rewritten to a flat index:
	// property IDs, provenance, diagnostics, the entry's Unmodeled key
	// (entryKey) and its name (lowering.NameAt) all derive from it.
	pointer jsontext.Pointer
	// wrapper marks a flattened wrapper, which contributes its properties and
	// required list, and is kept verbatim only for what else it writes.
	wrapper bool
	// capped marks an inline entry whose allOf was not flattened because it sits
	// past statement.MaxFlattenDepth, so the diagnostic can say why it was kept.
	capped bool
	// split marks the reference of an entry writing one beside other keys,
	// which the statement lists apart from them (statement.Entry.Split).
	split bool
}

// allOfComposition is the flattened allOf of the schema written at pointer: its
// entries, each with the pointer it was written at. A reader that takes it
// cannot reach the unflattened list.
type allOfComposition struct {
	pointer jsontext.Pointer
	entries []allOfEntry
	// disjoint names what each conjunct states where the schema draws the
	// warning docs/compilers/openapi.md#allof-disjoint-conjuncts describes, and
	// is nil otherwise (readAllOf).
	disjoint []string
	// speaks marks each entry that folds at the position (foldingAllOfEntries),
	// and rebased each whose folding keywords write a relative $ref it reads
	// against another base, so it does not. They are nil where the allOf is not
	// what the position lowers (allOfDecides).
	entryFoldsIntoPosition []bool
	rebased                []bool
	// conjoins is the pointer of the entry whose union the position lowers,
	// distributing it across its variants (entryUnion), whose merge consumes it,
	// and empty where no entry's is.
	conjoins jsontext.Pointer
	// bounds is the kinds whose value bounds the Model composing the entries
	// reads, those of the types the position admits (boundKinds) that it takes
	// (modelTakes), so an entry's bound folds where they take it.
	bounds kindSet
	// closer names the schema object whose additionalProperties and
	// patternProperties the Model composing the entries types (closerOf).
	closer closure
	// kept is what each inline entry keeps verbatim beside the Model
	// composing them, by index (entryKeeps).
	kept []keptEntry
}

// entrySpeaks reports whether the entry at index i folds at the position.
func (a allOfComposition) entrySpeaks(i int) bool {
	return a.entryFoldsIntoPosition != nil && a.entryFoldsIntoPosition[i]
}

// allOfEntries returns the entries of s's allOf as its statement flattens
// them (statement.Statement.Entries), each with the pointer it was written at,
// under the schema written at pointer. allOf is associative, so the list
// classifies exactly as its flat spelling would.
func allOfEntries(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	entries := loweringContext.Statements().Read(schema).Entries
	out := make([]allOfEntry, 0, len(entries))
	for _, allOfConjunct := range entries {
		out = append(out, allOfEntry{
			schema: loweringContext.Statements().Read(allOfConjunct.Schema), pointer: pointer + allOfConjunct.Rel, wrapper: allOfConjunct.Wrapper, capped: allOfConjunct.Capped, split: allOfConjunct.Split,
		})
	}
	return allOfComposition{pointer: pointer, entries: out}
}

// readAllOf is allOfEntries of the schema written at pointer, with what its
// conjuncts jointly admit read and, where the allOf is what the position
// lowers, which entries fold there (allOfEntryFoldingOf) and which writes the
// union the position lowers (entryUnion), for a lowering that composes it. That
// gate is positionFoldedEntries', so an entry gives up a keyword only where a
// reader of the position takes it up; both composers pass it, and the report of
// an allOf the election passed over does not.
func readAllOf(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfEntries(loweringContext, schema, pointer)
	comp.disjoint = disjointNames(loweringContext, schema, comp)
	comp.bounds = modelTakes(boundKinds(loweringContext, schema))
	comp.closer = closerOf(loweringContext, schema, comp)
	if allOfDecides(loweringContext, schema) {
		allOfEntryFolding := allOfEntryFoldingOf(loweringContext, schema)
		comp.entryFoldsIntoPosition, comp.rebased = allOfEntryFolding.entryFoldsIntoPosition, allOfEntryFolding.rebased
	}
	if unionAt, _, ok := entryUnion(loweringContext, schema, pointer); ok {
		comp.conjoins = unionAt.at
	}
	comp.kept = entryKeeps(loweringContext, schema, comp, func(i int) []string { return compositionResidue(loweringContext, comp, i) },
		allOfEntryFolding{entryFoldsIntoPosition: comp.entryFoldsIntoPosition, rebased: comp.rebased}, catchAllKeyword, nil)
	return comp
}

// allOfRefEntries yields the statements of the $ref entries of s's allOf
// after flattening: the view an ancestor walk needs, which reads no pointer.
func allOfRefEntries(loweringContext lowering.Ctx, schema *statement.Schema) iter.Seq[*statement.Statement] {
	return func(yield func(*statement.Statement) bool) {
		for _, allOfConjunct := range loweringContext.Statements().Read(schema).Entries {
			if allOfConjunct.Ref != nil && !yield(loweringContext.Statements().Read(allOfConjunct.Schema)) {
				return
			}
		}
	}
}

// FlatEntry is one entry of the allOf FlattenedAllOf reads.
type FlatEntry struct {
	// Schema is the entry as written, or the wrapper a run of its own entries
	// ended with.
	Schema *statement.Statement
	// Ref marks an entry that names its schema by $ref, which the reader follows
	// itself and bounds on its own.
	Ref bool
	// Capped marks an inline entry whose own allOf sits past
	// statement.MaxFlattenDepth and was not flattened, so the reader has not reached
	// what it declares.
	Capped bool
}

// FlattenedAllOf returns s's allOf as the lowering reads it, so a reader outside
// this package takes the same flattening and the same cap, in the same order
// (docs/compilers/openapi.md#allof-classification). It reads the entries
// allOfEntries does.
func FlattenedAllOf(loweringContext lowering.Ctx, schema *statement.Schema) []FlatEntry {
	entries := loweringContext.Statements().Read(schema).Entries
	out := make([]FlatEntry, 0, len(entries))
	for _, allOfConjunct := range entries {
		out = append(out, FlatEntry{Schema: loweringContext.Statements().Read(allOfConjunct.Schema), Ref: allOfConjunct.Ref != nil, Capped: allOfConjunct.Capped})
	}
	return out
}

// entryKey is the Unmodeled key an allOf entry at bptr is kept under: "openapi:"
// and its path from the composed schema. The path's tokens are `allOf` and
// decimal indices, so trimming the prefix is exact.
func (a allOfComposition) entryKey(bptr jsontext.Pointer) string {
	return "openapi:" + a.entryPath(bptr)
}

// entryPath is the path of the allOf entry at bptr from the composed schema,
// "$ref" for the reference the composed schema writes beside its keywords.
func (a allOfComposition) entryPath(bptr jsontext.Pointer) string {
	if bptr == a.pointer {
		return "$ref"
	}
	return strings.TrimPrefix(string(bptr), string(a.pointer)+"/")
}

// subject names the schema writing a's conjuncts in a report: "this allOf",
// or, where the schema writes a $ref beside its keywords, the conjunct that
// reference is, or no allOf at all, "this schema", which writes none to name.
func (a allOfComposition) subject() string {
	if len(a.entries) == 0 || a.entries[0].pointer == a.pointer {
		return "this schema"
	}
	return "this allOf"
}

// compositionRequired collects every required-property name declared across an
// allOf composition, in source order: each entry's required list (a flattened
// wrapper's included), then own, the list the composed schema writes itself. An
// entry is read from its local schema, never its resolved $ref target — that
// required list belongs to the target's own model (issue #29).
func compositionRequired(own []string, comp allOfComposition) []string {
	var out []string
	for _, entry := range comp.entries {
		// A boolean entry writes no object keyword, so ObjectKeywords reads none.
		out = append(out, entry.schema.ObjectKeywords().Required...)
	}
	return append(out, own...)
}

// fillAllOf classifies and lowers the entries of comp into m (GitHub #599).
//
// An inline entry merges in place: its `properties` and `required`
// (requireMembers) merge, its catch-all keywords where it closes the model
// (closerOf), what it writes for the position folds there
// (foldingAllOfEntries), and the rest stays verbatim beside the model
// (preserveUnmergedEntry, GitHub
// #123).
//
// A $ref entry is the reference alone, composing what it names
// (fillRefEntry). The keywords written beside its $ref are an inline entry
// of their own at its pointer, merged as above, a oneOf or anyOf among them
// lowering as on any entry (docs/ir/models.md#composition-entries).
func fillAllOf(loweringContext lowering.Ctx, ts *compile.Types, depth int, model *ir.Model, fields modelFields, comp allOfComposition,
	base int, hasBase bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var failed []int
	var uncomposed []wholeRefEntry
	for i, entry := range comp.entries {
		switch {
		case entry.schema.Ref == nil:
			diags = append(diags, fillModelProperties(loweringContext, ts, depth, fields, entry.schema.Schema, entry.pointer)...)
			diags = append(diags, preserveUnmergedEntry(loweringContext, ts, model, comp, i)...)
		case composesRef(loweringContext, entry):
			refDiags, resolved := fillRefEntry(loweringContext, ts, fields, entry, hasBase && i == base)
			diags = append(diags, refDiags...)
			if !resolved {
				failed = append(failed, i)
			}
		default:
			uncomposed = append(uncomposed, wholeRefEntry{entry: entry})
		}
	}
	apart, failedDiags := keepFailedRefs(loweringContext, ts, model, comp, failed)
	diags = append(diags, failedDiags...)
	return append(diags, keepUncomposedRefs(loweringContext, ts, model, comp.pointer, apart, uncomposed)...)
}

// composesRef reports whether the $ref entry allOfEntryValue is a Base or a
// Mixin of the composition: the schema it lands on lowers to an object alone,
// null aside (lowersTo), or to the union of a type list holding an object,
// whose object variant it composes (objectVariant), or its read is cut short,
// which leaves the composition as it is with no read. A model composes only
// models (docs/compilers/openapi.md#allof-classification).
func composesRef(loweringContext lowering.Ctx, allOfEntryValue allOfEntry) bool {
	target, _ := declaredOf(loweringContext, allOfEntryValue.schema)
	if target == nil || admissionOf(loweringContext, target).cut {
		return true
	}
	if kinds, _ := lowersTo(loweringContext, target); kinds == kindObject {
		return true
	}
	_, unpacked := objectVariant(loweringContext, allOfEntryValue.schema)
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
	if !placed || !lowersTypeArray(in, target.Schema, at.Pointer) {
		return "", false
	}
	i := slices.Index(typingOf(in, target.Schema).types, statement.KindObject)
	if i < 0 {
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
	apart, uncomposed []wholeRefEntry,
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

// fillRefEntry lowers one $ref allOf entry and adds it to the model fields
// builds as its Base, when isBase, or as a Mixin, recording the entry
// (modelFields.compose): what it names, or the object variant of the type
// list it names (objectVariant). A $ref it cannot resolve adds nothing and
// reports false, which keepFailedRefs answers; it is reported unless it lies on a
// loop of $refs, which load reports once (onLoop).
func fillRefEntry(loweringContext lowering.Ctx, ts *compile.Types, fields modelFields, allOfEntryValue allOfEntry, isBase bool) ([]ir.Diagnostic, bool) {
	var diags []ir.Diagnostic
	b := allOfEntryValue.schema
	id, ok := resolveSchemaRef(loweringContext, ts, b)
	if !ok {
		if !onLoop(b) {
			keyword, ref := refSpelling(b)
			diags = append(diags, loweringContext.DiagAt(ir.SeverityError, diag.UnresolvedRef, allOfEntryValue.pointer,
				"unresolved allOf %s %q%s", keyword, ref, unresolvedWhy(loweringContext, b)))
		}
		return diags, false
	}
	if variant, unpacked := objectVariant(loweringContext, allOfEntryValue.schema); unpacked {
		recordDiscarded(loweringContext, id)
		id = variant
	}
	unhomed := unreadByReference(loweringContext, b.Schema, compositionReads)
	var kept finished
	keep := keepUnreadOn(loweringContext, ts, b.Schema, unhomed, refSiteShape, allOfEntryValue.pointer, stand{}, &kept, nil)
	ref, homeDiags := homeDeclaration(loweringContext, ts, b.Schema, ir.TypeRef{Target: id}, allOfEntryValue.pointer, annotation.HomeOwnNode, len(unhomed) > 0, keep)
	diags = append(diags, homeDiags...)
	diags = append(diags, kept.diags...)
	if isBase {
		fields.m.Base = &ref
	} else {
		fields.m.Mixins = append(fields.m.Mixins, ref)
	}
	fields.compose(ref.Target, allOfEntryValue.pointer)
	return diags, true
}

// applyFalseEntries closes m for each boolean `false` allOf entry and keeps
// the entry verbatim under Unmodeled. It runs after fillComposedAdditional,
// whose result it overrides, the catch-all included
// (docs/compilers/openapi.md#allof-false-branch).
//
// A `false` entry admits nothing, so m must not stay open; the rule
// docs/compilers/openapi.md#whole-schema-fallbacks closes a bare `false` schema
// the same way. m keeps the other entries' contributions, because emptying it
// would trade one silent loss for another. The verbatim entry tells this from
// a plain `additionalProperties: false`; a diagnostic does not, being no part
// of the document. A `true` entry constrains nothing and is skipped.
func applyFalseEntries(loweringContext lowering.Ctx, model *ir.Model, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, entry := range comp.entries {
		if !entry.schema.Truth.ReadsFalse() {
			continue // `true`, a null and any schema with a body close nothing.
		}
		model.Additional = ir.AdditionalClosed
		annotation.PreserveInto(&model.Unmodeled, comp.entryKey(entry.pointer),
			ir.RawValue("false"), ir.ReasonDegradedLowering, loweringContext.Place().At(entry.pointer))

		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.FalseSchema, entry.pointer,
			"boolean false allOf entry matches nothing, so the composition matches nothing; "+
				"composed model closed and the entry kept verbatim under Unmodeled"))
	}

	closer := comp.closer
	if closer.schema != nil && model.Additional == ir.AdditionalClosed && model.AdditionalProperties != nil && model.AdditionalProperties.Value != nil {
		_, keptDiags := annotation.PreserveSchemaKeyword(&model.Unmodeled, closer.schema, "additionalProperties",
			ir.ReasonDegradedLowering, closer.at, loweringContext.Place())
		diags = append(diags, keptDiags...)
	}
	forbidRest(loweringContext, model)
	return diags
}

// preserveUnmergedEntry keeps what the inline allOf entry at index i of comp
// writes that the merge consumes nowhere beside the composed model, as comp.kept
// says: each keyword under its own key and reason, or the entry whole under a
// key comp gives it, so sibling entries never overwrite one another
// (docs/compilers/openapi.md#allof-inline-residue).
func preserveUnmergedEntry(loweringContext lowering.Ctx, ts *compile.Types, model *ir.Model, comp allOfComposition, i int) []ir.Diagnostic {
	keptEntryValue := comp.kept[i]
	disjoint := len(comp.disjoint) > 0
	warn := entryExcludesObject(keptEntryValue.entry.schema.Schema) && !disjoint
	return recordKeptEntry(loweringContext, ts, &model.Unmodeled, comp, keptEntryValue, nodeShape(ir.KindModel), warn, func(otherKeptEntry keptEntry) ir.Diagnostic {
		return diagUnmergedEntry(loweringContext, otherKeptEntry, disjoint)
	})
}

// compositionResidue returns what the inline allOf entry at index i of comp
// writes that the in-place merge reads nowhere: not its properties and
// required, nor what folds where comp says it speaks, nor the union the
// position lowers, nor the catch-all of the entry closing the model. A
// flattened entry's allOf is consumed: allOfEntries replaced it by its own
// entries. A boolean entry has no keywords to leave a residue
// (applyFalseEntries), and a $ref entry homes its siblings on an alias over
// its target (fillRefEntry), unless it names nothing (failedRefResidue).
func compositionResidue(loweringContext lowering.Ctx, comp allOfComposition, i int) []string {
	entry := comp.entries[i]
	if entry.schema.Truth != statement.Stated {
		return nil
	}
	if entry.schema.Ref != nil {
		return failedRefResidue(loweringContext, entry)
	}
	speaks := comp.entrySpeaks(i)
	conjoined := comp.conjoins != "" && entry.pointer == comp.conjoins
	closes := comp.closer.entryAt(entry.pointer)
	return unmergedEntryKeys(entry.schema.Schema, func(keyword string) bool {
		return mergeConsumes(entry.schema.Schema, keyword, entry.wrapper) || (speaks && folds(loweringContext, entry.schema.Schema, keyword, boundHomes{value: comp.bounds})) ||
			(conjoined && conjoinedKeyword(keyword)) || (closes && catchAllKeyword(keyword))
	})
}

// diagUnmergedEntry builds the diagnostic announcing the entry k keeps whole,
// naming the keywords so two entries of the same schema are told apart. The
// caller records it.
//
// An entry declaring a type that cannot be an object is a warning rather than
// info: the composed ir.Model asserts the value *is* an object, which the entry
// contradicts. Any other residue only narrows a model the IR describes correctly,
// the under-constrained case (docs/compilers/openapi.md#allof-inline-residue).
// disjoint says the schema reports its conjuncts share no value, so this
// reports residue alone.
func diagUnmergedEntry(loweringContext lowering.Ctx, keptEntryValue keptEntry, disjoint bool) ir.Diagnostic {
	entry, residue := keptEntryValue.entry, keptEntryValue.residue
	if entryExcludesObject(entry.schema.Schema) && !disjoint {
		names := namesOf(entry)
		message := names.lead + " declares a type that is not an object, which the composed model asserts it is; " + names.kept + " (" +
			strings.Join(keptKeywords(residue, entry), ", ") + ") is kept verbatim under Unmodeled"
		if entry.capped {
			message += ", and its own allOf, nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) + ", was not flattened"
		}
		return loweringContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, entry.pointer, "%s", message)
	}
	if why := wholeWhy(keptEntryValue); why != "" && !entry.capped {
		return loweringContext.DiagAt(ir.SeverityInfo, wholeCode(keptEntryValue), entry.pointer, "%s", why)
	}
	return loweringContext.DiagAt(ir.SeverityInfo, wholeCode(keptEntryValue), entry.pointer, "%s", unmergedWhy(residue, entry))
}

// keptKeywords is residue as a diagnostic names it: a capped entry is kept for
// its depth, so its allOf is not listed as a keyword the merge declines.
func keptKeywords(residue []string, allOfEntryValue allOfEntry) []string {
	if !allOfEntryValue.capped {
		return residue
	}
	return slices.DeleteFunc(slices.Clone(residue), func(k string) bool { return k == "allOf" })
}

// unmergedWhy words why an entry with residue is kept: what merges and folds is
// gone from residue, which is the rest. A flattened entry's nested allOf composes
// too. A capped one is kept for its depth, not for allOf being a keyword the merge
// declines, so allOf is left out of the keywords it lists.
func unmergedWhy(residue []string, allOfEntryValue allOfEntry) string {
	const keptTail = " is kept verbatim under Unmodeled"
	names := namesOf(allOfEntryValue)
	switch {
	case allOfEntryValue.wrapper:
		return names.lead + " is merged in place, its nested allOf composes, and nothing reads the rest of it; " + names.kept + " (" +
			strings.Join(residue, ", ") + ")" + keptTail
	case allOfEntryValue.capped:
		rest := keptKeywords(residue, allOfEntryValue)
		why := names.lead + " is nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) +
			", so its own allOf was not flattened, and " + names.kept
		if len(rest) > 0 {
			why += " (" + strings.Join(rest, ", ") + ")"
		}
		return why + keptTail
	default:
		return names.lead + " is merged in place, and nothing reads the rest of it; " + names.kept + " (" +
			strings.Join(residue, ", ") + ")" + keptTail
	}
}

// unmergedEntryKeys returns the keywords the entry declares that consumes does
// not claim, in the order the parser's model holds them.
//
// The keys are the entry's own (statement.Schema.Keys) rather than a list of
// keywords worth preserving, which is what keeps this from rotting: a keyword a
// later dialect adds counts as unconsumed the moment a source writes it, with
// nobody to remember it here.
func unmergedEntryKeys(bs *statement.Schema, consumes func(key string) bool) []string {
	out := make([]string, 0, len(bs.Keys))
	for _, key := range bs.Keys {
		if consumes(key.Name) || annotation.WritesKeywordLeftOut(bs, key.Name) {
			continue
		}
		out = append(out, key.Name)
	}
	return out
}

// mergeConsumes reports whether fillAllOf's in-place merge reads entry keyword
// key. It is the complete statement of what the merge keeps; everything else the
// entry wrote is residue. `allOf` is consumed exactly when flattened
// (docs/compilers/openapi.md#allof-classification); otherwise it is residue like
// the rest.
func mergeConsumes(bs *statement.Schema, key string, flattened bool) bool {
	switch key {
	case "allOf":
		return flattened // allOfEntries replaced the entry by its own entries
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

// conjoinedKeyword reports whether keyword is one an entry whose union the
// position lowers gives up to that union: its oneOf or anyOf, and the
// discriminator it writes, which is the union's where the position lowers it
// (findEntryUnion).
func conjoinedKeyword(keyword string) bool {
	return keyword == "oneOf" || keyword == "anyOf" || keyword == "discriminator"
}

// declaresObjectOnly reports whether the entry's declared type is exactly
// `object`, which the composed ir.Model already restates so the merge loses
// nothing by reading it. A set — `[object, "null"]` included — says more than
// that, and the more is what the merge would drop.
func declaresObjectOnly(bs *statement.Schema) bool {
	types := bs.Types()
	return len(types) == 1 && types[0] == statement.KindObject
}

// entryExcludesObject reports whether the entry declares a type set with no
// `object` in it. An undeclared type set excludes nothing.
func entryExcludesObject(bs *statement.Schema) bool {
	return writesType(bs) && !slices.Contains(bs.Types(), statement.KindObject)
}

// selectAllOfBase returns the index into entries, the conjuncts of the
// composition whose node is id, of the one that becomes Model.Base, and false
// when none qualifies (multiple non-hierarchy refs stay Mixins): of the $refs
// the composition takes (composesRef), the one, or of several, of those whose
// $ref lands on a schema taking part in a discriminator hierarchy
// (inHierarchy), one whose hierarchy's mapping routes id before one whose does
// not, then the one whose reference text is the least, so no order written
// decides.
func selectAllOfBase(loweringContext lowering.Ctx, entries []allOfEntry, id ir.TypeID) (int, bool) {
	refIdxs := make([]int, 0, len(entries))
	for i, entry := range entries {
		if entry.schema.Ref != nil && composesRef(loweringContext, entry) {
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
		target := refBranchTarget(loweringContext, entries[i].schema)
		routes[i] = len(refIdxs) > 1 && stated(target) && inHierarchy(loweringContext, target) && routesTo(loweringContext, target, id)
	}
	slices.SortStableFunc(refIdxs, func(i, j int) int {
		_, a := refSpelling(entries[i].schema)
		_, b := refSpelling(entries[j].schema)
		return cmp.Or(compareTrueFirst(routes[i], routes[j]), strings.Compare(a, b),
			strings.Compare(string(entries[i].pointer), string(entries[j].pointer)))
	})
	return merge.SelectBase(refIdxs, func(i int) bool {
		target := refBranchTarget(loweringContext, entries[i].schema)
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
