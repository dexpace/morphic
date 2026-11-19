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
func lowerAllOf(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, discriminated bool, mixin *unionAt,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		// The one flattened list; the readers composeModel calls take it, not s.
		td, read, modelDiags := composeModel(c, ts, depth, s, pointer, discriminated, common, readAllOf(c, s, pointer))
		diags = modelDiags
		diags = append(diags, composeMixin(c, ts, depth, td, mixin, pointer)...)
		return td, read
	}, fin)
	return id, diags
}

// composeModel builds the Model of s, written at pointer, composing comp, its
// flattened conjuncts, and returns what it read of s.
func composeModel(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer,
	discriminated bool, common ir.TypeCommon, comp allOfComposition,
) (ir.TypeDef, keySet, []ir.Diagnostic) {
	if admissionOf(c, c.Statements().Read(s)).inferred {
		// No conjunct declares the kind: the model is one by the
		// kind-inference policy's reading of their shape keywords.
		common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
	}
	cons, diags := ConstraintsOf(c, &common.Unmodeled, s, positionVoices(c, s, pointer, ir.TypeRef{}), pointer, comp.bounds)
	m := &ir.Model{TypeCommon: common, Constraints: cons}
	diags = append(diags, diagDisjointConjuncts(c, comp, composedShape)...)
	fields := newModelFields(c, ts, m)
	base, hasBase := selectAllOfBase(c, comp.entries, common.ID)
	diags = append(diags, fillAllOf(c, ts, depth, m, fields, comp, base, hasBase)...)
	diags = append(diags, fillModelProperties(c, ts, depth, fields, s, pointer)...)
	requireMembers(m, compositionRequired(s.ObjectKeywords().Required, comp))
	diags = append(diags, fillComposedAdditional(c, ts, depth, m, s, comp)...)
	diags = append(diags, applyFalseEntries(c, m, comp)...)
	if discriminated {
		d, discDiags := lowerDiscriminator(c, ts, s.Discriminator(), pointer, m, nil)
		diags = append(diags, discDiags...)
		m.Discriminator = d
	}
	var first *statement.Schema // the base's hierarchy tags the model first
	if hasBase {
		first = refBranchTarget(c, comp.entries[base].schema)
	}
	tag, tagDiags := subtypeDiscriminatorValue(c, s, common.ID, pointer, first)
	m.DiscriminatorValue = tag.value
	diags = append(diags, tagDiags...)
	deferSubtypeTag(c, ts, tag, common.ID, pointer)
	return objectNode(m), modelReads(s, m, comp.bounds), diags
}

// compositionReads names the readDomain keywords an allOf composition
// reads itself, so neither the keeping of a $ref entry's unread keywords nor
// that of the composed position reports them again: lowerAllOf lands every required list on the
// composed model (requireMembers).
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
	// speaks marks each entry that folds at the position (voicesAmong), and
	// rebased each whose folding keywords write a relative $ref it reads against
	// another base, so it does not. They are nil where the allOf is not what the
	// position lowers (allOfDecides).
	speaks  []bool
	rebased []bool
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
func (a allOfComposition) entrySpeaks(i int) bool { return a.speaks != nil && a.speaks[i] }

// allOfEntries returns the entries of s's allOf as its statement flattens
// them (statement.Statement.Entries), each with the pointer it was written at,
// under the schema written at pointer. allOf is associative, so the list
// classifies exactly as its flat spelling would.
func allOfEntries(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	entries := c.Statements().Read(s).Entries
	out := make([]allOfEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, allOfEntry{schema: c.Statements().Read(e.Schema), pointer: pointer + e.Rel, wrapper: e.Wrapper, capped: e.Capped})
	}
	return allOfComposition{pointer: pointer, entries: out}
}

// readAllOf is allOfEntries of the schema written at pointer, with what its
// conjuncts jointly admit read and, where the allOf is what the position lowers,
// which entries fold there (voicingOf) and which writes the union the position
// lowers (entryUnion), for a lowering that composes it. That gate is
// positionVoices', so an entry gives up a keyword only where a reader of the
// position takes it up; both composers pass it, and the report of an allOf the
// election passed over does not.
func readAllOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfEntries(c, s, pointer)
	comp.disjoint = disjointNames(c, s, comp)
	comp.bounds = modelTakes(boundKinds(c, s))
	comp.closer = closerOf(c, s, comp)
	if allOfDecides(c, s) {
		v := voicingOf(c, s)
		comp.speaks, comp.rebased = v.speaks, v.rebased
	}
	if u, _, ok := entryUnion(c, s, pointer); ok {
		comp.conjoins = u.at
	}
	comp.kept = entryKeeps(c, s, comp, func(i int) []string { return compositionResidue(c, comp, i) },
		voicing{speaks: comp.speaks, rebased: comp.rebased}, catchAllKeyword, nil)
	return comp
}

// allOfRefEntries yields the statements of the $ref entries of s's allOf
// after flattening: the view an ancestor walk needs, which reads no pointer.
func allOfRefEntries(c lowering.Ctx, s *statement.Schema) iter.Seq[*statement.Statement] {
	return func(yield func(*statement.Statement) bool) {
		for _, e := range c.Statements().Read(s).Entries {
			if e.Ref != nil && !yield(c.Statements().Read(e.Schema)) {
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
func FlattenedAllOf(c lowering.Ctx, s *statement.Schema) []FlatEntry {
	entries := c.Statements().Read(s).Entries
	out := make([]FlatEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FlatEntry{Schema: c.Statements().Read(e.Schema), Ref: e.Ref != nil, Capped: e.Capped})
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
	for _, e := range comp.entries {
		// A boolean entry writes no object keyword, so ObjectKeywords reads none.
		out = append(out, e.schema.ObjectKeywords().Required...)
	}
	return append(out, own...)
}

// fillAllOf classifies and lowers the entries of comp into m (GitHub #599).
//
// An inline entry merges in place: its `properties` and `required`
// (requireMembers) merge, its catch-all keywords where it closes the model
// (closerOf), what it writes for the position folds there (voicesAmong), and
// the rest stays verbatim beside the model (preserveUnmergedEntry, GitHub
// #123).
//
// A $ref entry is the reference alone, composing what it names
// (fillRefEntry). The keywords written beside its $ref are an inline entry
// of their own at its pointer, merged as above, a oneOf or anyOf among them
// lowering as on any entry (docs/ir/models.md#composition-entries).
func fillAllOf(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, fields modelFields, comp allOfComposition,
	base int, hasBase bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var failed []int
	var uncomposed []wholeRefEntry
	for i, e := range comp.entries {
		switch {
		case e.schema.Ref == nil:
			diags = append(diags, fillModelProperties(c, ts, depth, fields, e.schema.Schema, e.pointer)...)
			diags = append(diags, preserveUnmergedEntry(c, ts, m, comp, i)...)
		case composesRef(c, e):
			refDiags, resolved := fillRefEntry(c, ts, fields, e, hasBase && i == base)
			diags = append(diags, refDiags...)
			if !resolved {
				failed = append(failed, i)
			}
		default:
			uncomposed = append(uncomposed, wholeRefEntry{entry: e})
		}
	}
	apart, failedDiags := keepFailedRefs(c, ts, m, comp, failed)
	diags = append(diags, failedDiags...)
	return append(diags, keepUncomposedRefs(c, ts, m, comp.pointer, apart, uncomposed)...)
}

// composesRef reports whether the $ref entry e is a Base or a Mixin of the
// composition: the schema it lands on lowers to an object alone, null aside
// (lowersTo), or to the union of a type list holding an object, whose object
// variant it composes (objectVariant), or its read is cut short, which leaves
// the composition as it is with no read. A model composes only models
// (docs/compilers/openapi.md#allof-classification).
func composesRef(c lowering.Ctx, e allOfEntry) bool {
	target, _ := declaredOf(c, e.schema)
	if target == nil || admissionOf(c, target).cut {
		return true
	}
	if kinds, _ := lowersTo(c, target); kinds == kindObject {
		return true
	}
	_, unpacked := objectVariant(c, e.schema)
	return unpacked
}

// objectVariant returns the ID of the object variant of the union the $ref
// conjunct ref lands on, an allOf entry or a distributed union's branch, where
// that schema lowers by a type list holding an object (lowerUnion), and false
// for any other: the variant stands at the list's position, one per type, so
// it is named before the union is built. A union a oneOf or anyOf writes may
// hold several objects, or none a model is, and is not unpacked.
func objectVariant(c lowering.Ctx, ref *statement.Statement) (ir.TypeID, bool) {
	target, at := declaredOf(c, ref)
	if target == nil || target.Ref != nil || declaresUnion(target.Schema) {
		return "", false
	}
	in, placed := lowering.InDocument(c, at.Doc)
	i := slices.Index(effectiveTypes(target.Schema), statement.KindObject)
	if !placed || i < 0 || !lowersTypeArray(in, target.Schema, at.Pointer) {
		return "", false
	}
	return in.IDs().ForPointer(at.Pointer + ids.Ptr("type", strconv.Itoa(i))), true
}

// keepUncomposedRefs keeps beside m the $ref entries of its composition,
// written at pointer, it does not compose: apart, those whose reference names
// nothing (keepFailedRefs), and uncomposed, those whose target is no object
// alone, ranked together (keepRefConjuncts). It reports each uncomposed one
// kept, resolving its target so the document holds what it names; a failed
// one is reported where it is written.
func keepUncomposedRefs(c lowering.Ctx, ts *compile.Types, m *ir.Model, pointer jsontext.Pointer,
	apart, uncomposed []wholeRefEntry,
) []ir.Diagnostic {
	for _, b := range uncomposed {
		resolveSchemaRef(c, ts, b.entry.schema)
	}
	kept, diags := keepRefConjuncts(c, &m.Unmodeled, slices.Concat(apart, uncomposed), pointer)
	for _, b := range uncomposed {
		if kept[b.entry.pointer] {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s",
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
func fillRefEntry(c lowering.Ctx, ts *compile.Types, fields modelFields, e allOfEntry, isBase bool) ([]ir.Diagnostic, bool) {
	var diags []ir.Diagnostic
	b := e.schema
	id, ok := resolveSchemaRef(c, ts, b)
	if !ok {
		if !onLoop(b) {
			keyword, ref := refSpelling(b)
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, e.pointer,
				"unresolved allOf %s %q%s", keyword, ref, unresolvedWhy(c, b)))
		}
		return diags, false
	}
	if variant, unpacked := objectVariant(c, e.schema); unpacked {
		id = variant
	}
	unhomed := unreadByReference(c, b.Schema, compositionReads)
	var kept finished
	keep := keepUnreadOn(c, ts, b.Schema, unhomed, refSiteShape, e.pointer, stand{}, &kept, nil)
	ref, homeDiags := homeDeclaration(c, ts, b.Schema, ir.TypeRef{Target: id}, e.pointer, annotation.HomeOwnNode, len(unhomed) > 0, keep)
	diags = append(diags, homeDiags...)
	diags = append(diags, kept.diags...)
	if isBase {
		fields.m.Base = &ref
	} else {
		fields.m.Mixins = append(fields.m.Mixins, ref)
	}
	fields.compose(ref.Target, e.pointer)
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
func applyFalseEntries(c lowering.Ctx, m *ir.Model, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range comp.entries {
		if !e.schema.Truth.ReadsFalse() {
			continue // `true`, a null and any schema with a body close nothing.
		}
		m.Additional = ir.AdditionalClosed
		annotation.PreserveInto(&m.Unmodeled, comp.entryKey(e.pointer),
			ir.RawValue("false"), ir.ReasonDegradedLowering, c.Place().At(e.pointer))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, e.pointer,
			"boolean false allOf entry matches nothing, so the composition matches nothing; "+
				"composed model closed and the entry kept verbatim under Unmodeled"))
	}

	closer := comp.closer
	if closer.schema != nil && m.Additional == ir.AdditionalClosed && m.AdditionalProps != nil && m.AdditionalProps.Value != nil {
		_, keptDiags := annotation.PreserveSchemaKeyword(&m.Unmodeled, closer.schema, "additionalProperties",
			ir.ReasonDegradedLowering, closer.at, c.Place())
		diags = append(diags, keptDiags...)
	}
	forbidRest(m)
	return diags
}

// preserveUnmergedEntry keeps what the inline allOf entry at index i of comp
// writes that the merge consumes nowhere beside the composed model, as comp.kept
// says: each keyword under its own key and reason, or the entry whole under a
// key comp gives it, so sibling entries never overwrite one another
// (docs/compilers/openapi.md#allof-inline-residue).
func preserveUnmergedEntry(c lowering.Ctx, ts *compile.Types, m *ir.Model, comp allOfComposition, i int) []ir.Diagnostic {
	k := comp.kept[i]
	disjoint := len(comp.disjoint) > 0
	warn := entryExcludesObject(k.entry.schema.Schema) && !disjoint
	return recordKeptEntry(c, ts, &m.Unmodeled, comp, k, nodeShape(ir.KindModel), warn, func(k keptEntry) ir.Diagnostic {
		return diagUnmergedEntry(c, k, disjoint)
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
func compositionResidue(c lowering.Ctx, comp allOfComposition, i int) []string {
	e := comp.entries[i]
	if e.schema.Truth != statement.Stated {
		return nil
	}
	if e.schema.Ref != nil {
		return failedRefResidue(c, e)
	}
	speaks := comp.entrySpeaks(i)
	conjoined := comp.conjoins != "" && e.pointer == comp.conjoins
	closes := comp.closer.entryAt(e.pointer)
	return unmergedEntryKeys(e.schema.Schema, func(keyword string) bool {
		return mergeConsumes(e.schema.Schema, keyword, e.wrapper) || (speaks && folds(c, e.schema.Schema, keyword, boundHomes{value: comp.bounds})) ||
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
func diagUnmergedEntry(c lowering.Ctx, k keptEntry, disjoint bool) ir.Diagnostic {
	e, residue := k.entry, k.residue
	if entryExcludesObject(e.schema.Schema) && !disjoint {
		n := namesOf(e)
		msg := n.lead + " declares a type that is not an object, which the composed model asserts it is; " + n.kept + " (" +
			strings.Join(keptKeywords(residue, e), ", ") + ") is kept verbatim under Unmodeled"
		if e.capped {
			msg += ", and its own allOf, nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) + ", was not flattened"
		}
		return c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, e.pointer, "%s", msg)
	}
	if why := wholeWhy(k); why != "" && !e.capped {
		return c.DiagAt(ir.SeverityInfo, wholeCode(k), e.pointer, "%s", why)
	}
	return c.DiagAt(ir.SeverityInfo, wholeCode(k), e.pointer, "%s", unmergedWhy(residue, e))
}

// keptKeywords is residue as a diagnostic names it: a capped entry is kept for
// its depth, so its allOf is not listed as a keyword the merge declines.
func keptKeywords(residue []string, e allOfEntry) []string {
	if !e.capped {
		return residue
	}
	return slices.DeleteFunc(slices.Clone(residue), func(k string) bool { return k == "allOf" })
}

// unmergedWhy words why an entry with residue is kept: what merges and folds is
// gone from residue, which is the rest. A flattened entry's nested allOf composes
// too. A capped one is kept for its depth, not for allOf being a keyword the merge
// declines, so allOf is left out of the keywords it lists.
func unmergedWhy(residue []string, e allOfEntry) string {
	const keptTail = " is kept verbatim under Unmodeled"
	n := namesOf(e)
	switch {
	case e.wrapper:
		return n.lead + " is merged in place, its nested allOf composes, and nothing reads the rest of it; " + n.kept + " (" +
			strings.Join(residue, ", ") + ")" + keptTail
	case e.capped:
		rest := keptKeywords(residue, e)
		why := n.lead + " is nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) +
			", so its own allOf was not flattened, and " + n.kept
		if len(rest) > 0 {
			why += " (" + strings.Join(rest, ", ") + ")"
		}
		return why + keptTail
	default:
		return n.lead + " is merged in place, and nothing reads the rest of it; " + n.kept + " (" +
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
	for _, k := range bs.Keys {
		if consumes(k.Name) || annotation.WritesKeywordLeftOut(bs, k.Name) {
			continue
		}
		out = append(out, k.Name)
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
func selectAllOfBase(c lowering.Ctx, entries []allOfEntry, id ir.TypeID) (int, bool) {
	refIdxs := make([]int, 0, len(entries))
	for i, e := range entries {
		if e.schema.Ref != nil && composesRef(c, e) {
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
		target := refBranchTarget(c, entries[i].schema)
		routes[i] = len(refIdxs) > 1 && stated(target) && inHierarchy(c, target) && routesTo(c, target, id)
	}
	slices.SortStableFunc(refIdxs, func(i, j int) int {
		_, a := refSpelling(entries[i].schema)
		_, b := refSpelling(entries[j].schema)
		return cmp.Or(compareTrueFirst(routes[i], routes[j]), strings.Compare(a, b),
			strings.Compare(string(entries[i].pointer), string(entries[j].pointer)))
	})
	return merge.SelectBase(refIdxs, func(i int) bool {
		target := refBranchTarget(c, entries[i].schema)
		return stated(target) && inHierarchy(c, target)
	})
}

// routesTo reports whether the mapping of a discriminator s, a stated schema,
// or one of its discriminated ancestors declares names id, the node a
// composition of s lowers to; an empty id is no node, which none names.
func routesTo(c lowering.Ctx, s *statement.Schema, id ir.TypeID) bool {
	return slices.ContainsFunc(append([]*statement.Schema{s}, discriminatedAncestors(c, s)...), func(a *statement.Schema) bool {
		d := a.Discriminator()
		return d != nil && len(mappingTagsFor(c, d, id)) > 0
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

// inHierarchy reports whether s takes part in a discriminator hierarchy: it
// declares a discriminator, or composes through allOf, at any depth, a schema
// that does, the ancestors whose routes tag it (discriminatedAncestors). It
// reads the source, so no lowering order decides it, and every composition
// naming s asks, so it is read once a run.
func inHierarchy(c lowering.Ctx, s *statement.Schema) bool {
	return lowering.Remembered(c, inHierarchyOf{s}, func() bool {
		return s.Discriminator() != nil || len(discriminatedAncestors(c, s)) > 0
	})
}
