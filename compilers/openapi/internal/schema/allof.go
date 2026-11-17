// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
// Value constraints written beside the allOf (minProperties, ...), or on a branch
// that folds, are read as lowerModel reads a plain object's, so they land on the
// same field, not Unmodeled (GitHub #407). A composition that leaves only a
// catch-all is a map, as a plain object's is (objectNode). Where discriminated is
// false, a union the model composes took s's discriminator (lowerSubtypeUnion).
func lowerAllOf(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, discriminated bool) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		comp := readAllOf(c, s, pointer) // the one flattened list; the readers below take it, not s
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, positionVoices(c, ts, s, pointer, ir.TypeRef{}), pointer, comp.bounds)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, diagDisjointConjuncts(c, comp, composedShape)...)
		fields := newModelFields(c, ts, m)
		diags = append(diags, fillAllOf(c, ts, depth, m, fields, comp)...)
		diags = append(diags, fillModelProperties(c, ts, depth, fields, s, pointer)...)
		requireMembers(m, compositionRequired(objectIn(s).Required, comp))
		diags = append(diags, fillAdditional(c, ts, depth, m, s, pointer)...)
		diags = append(diags, applyFalseBranches(c, m, s, comp)...)
		if discriminated {
			d, discDiags := lowerDiscriminator(c, ts, discriminatorIn(s), pointer, m, nil)
			diags = append(diags, discDiags...)
			m.Discriminator = d
		}
		tag, tagDiags := subtypeDiscriminatorValue(c, s, common.ID, pointer)
		m.DiscriminatorValue = tag.value
		diags = append(diags, tagDiags...)
		deferSubtypeTag(c, ts, tag, common.ID, pointer)
		return objectNode(m)
	})
	return id, diags
}

// compositionCensusHandled names the census keywords an allOf composition
// reads itself, so neither the census of a $ref branch nor that of the composed
// position reports them again: lowerAllOf lands every required list on the
// composed model (requireMembers).
var compositionCensusHandled = []string{"required"}

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
	// speaks marks each branch that folds at the position (voicesAmong), and
	// rebased each whose folding keywords write a relative $ref it reads against
	// another base, so it does not. They are nil where the allOf is not what the
	// position lowers (allOfDecides).
	speaks  []bool
	rebased []bool
	// conjoins is the pointer of the branch whose union the position lowers,
	// distributing it across its variants (branchUnion), whose merge consumes it,
	// and empty where no branch's is.
	conjoins jsontext.Pointer
	// bounds is the kinds whose value bounds the Model composing the branches
	// reads, those of the types the position admits (boundKinds) that it takes
	// (modelTakes), so a branch's bound folds where they take it.
	bounds kindSet
}

// branchSpeaks reports whether the branch at index i folds at the position.
func (a allOfComposition) branchSpeaks(i int) bool { return a.speaks != nil && a.speaks[i] }

// allOfBranches returns the branches of s's allOf as its statement flattens
// them (statement.Statement.Entries), each with the pointer it was written at,
// under the schema written at pointer. allOf is associative, so the list
// classifies exactly as its flat spelling would.
func allOfBranches(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	entries := c.Statements().Read(s).Entries
	out := make([]allOfBranch, 0, len(entries))
	for _, e := range entries {
		out = append(out, allOfBranch{schema: c.Statements().Read(e.Schema), pointer: pointer + e.Rel, wrapper: e.Wrapper, capped: e.Capped})
	}
	return allOfComposition{pointer: pointer, branches: out}
}

// readAllOf is allOfBranches of the schema written at pointer, with what its
// conjuncts jointly admit read and, where the allOf is what the position lowers,
// which branches fold there (voicingOf) and which writes the union the position
// lowers (branchUnion), for a lowering that composes it. That gate is
// positionVoices', so a branch gives up a keyword only where a reader of the
// position takes it up; both composers pass it, and the report of an allOf the
// election passed over does not.
func readAllOf(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) allOfComposition {
	comp := allOfBranches(c, s, pointer)
	comp.disjoint = disjointConjuncts(c, s, comp)
	comp.bounds = modelTakes(boundKinds(c, s))
	if allOfDecides(c, s) {
		v := voicingOf(c, s)
		comp.speaks, comp.rebased = v.speaks, v.rebased
	}
	if u, _, ok := branchUnion(c, s, pointer); ok {
		comp.conjoins = u.at
	}
	return comp
}

// allOfRefBranches yields the statements of the $ref branches of s's allOf
// after flattening: the view an ancestor walk needs, which reads no pointer.
func allOfRefBranches(c lowering.Ctx, s *statement.Schema) iter.Seq[*statement.Statement] {
	return func(yield func(*statement.Statement) bool) {
		for _, e := range c.Statements().Read(s).Entries {
			if e.Ref != nil && !yield(c.Statements().Read(e.Schema)) {
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

// FlattenedAllOf returns s's allOf as the lowering reads it, so a reader outside
// this package takes the same flattening and the same cap, in the same order
// (docs/compilers/openapi.md#allof-classification). It reads the entries
// allOfBranches does.
func FlattenedAllOf(c lowering.Ctx, s *statement.Schema) []FlatBranch {
	entries := c.Statements().Read(s).Entries
	out := make([]FlatBranch, 0, len(entries))
	for _, e := range entries {
		out = append(out, FlatBranch{Schema: c.Statements().Read(e.Schema), Ref: e.Ref != nil, Capped: e.Capped})
	}
	return out
}

// branchKey is the Unmodeled key an allOf branch at bptr is kept under: "openapi:"
// and its path from the composed schema. The path's tokens are `allOf` and
// decimal indices, so trimming the prefix is exact.
func (a allOfComposition) branchKey(bptr jsontext.Pointer) string {
	return "openapi:" + a.branchPath(bptr)
}

// branchPath is the path of the allOf branch at bptr from the composed schema.
func (a allOfComposition) branchPath(bptr jsontext.Pointer) string {
	return strings.TrimPrefix(string(bptr), string(a.pointer)+"/")
}

// compositionRequired collects every required-property name declared across an
// allOf composition, in source order: each branch's required list (a flattened
// wrapper's included), then own, the list the composed schema writes itself. A
// branch is read from its local schema, never its resolved $ref target — that
// required list belongs to the target's own model (issue #29).
func compositionRequired(own []string, comp allOfComposition) []string {
	var out []string
	for _, e := range comp.branches {
		// A boolean branch writes no object keyword, so objectIn reads none.
		out = append(out, objectIn(e.schema.Schema).Required...)
	}
	return append(out, own...)
}

// fillAllOf classifies and lowers the branches of comp into m (GitHub #599).
//
// An inline branch merges in place: its `properties` and `required`
// (requireMembers) merge, what it writes for the position folds there
// (voicesAmong), and the rest stays verbatim beside the model
// (preserveUnmergedBranch, GitHub #123).
//
// A $ref branch owns a node: keywords beside its $ref, if any, go on an alias
// that homeDeclaration hoists there, and the composition points at it (GitHub
// #143). The census every $ref site runs keeps what the alias cannot model,
// except `required` (compositionCensusHandled); a co-declared oneOf/anyOf is kept
// too (GitHub #406).
func fillAllOf(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, fields modelFields, comp allOfComposition,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	base, hasBase := selectAllOfBase(c, comp.branches)
	for i, e := range comp.branches {
		if e.schema.Ref == nil {
			diags = append(diags, fillModelProperties(c, ts, depth, fields, e.schema.Schema, e.pointer)...)
			diags = append(diags, preserveUnmergedBranch(c, m, comp, i)...)
			continue
		}
		diags = append(diags, fillRefBranch(c, ts, fields, e, hasBase && i == base)...)
	}
	return diags
}

// fillRefBranch lowers one $ref allOf branch and adds it to the model fields
// builds as its Base, when isBase, or as a Mixin, recording the entry
// (modelFields.compose). A $ref it cannot resolve adds nothing, and is reported
// unless it lies on a loop of $refs, which load reports once (onLoop).
func fillRefBranch(c lowering.Ctx, ts *compile.Types, fields modelFields, e allOfBranch, isBase bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	b := e.schema
	id, ok := resolveSchemaRef(c, ts, b)
	if !ok {
		if !onLoop(b) {
			keyword, ref := refSpelling(b)
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, e.pointer,
				"unresolved allOf %s %q%s", keyword, ref, unresolvedWhy(c, b)))
		}
		return diags
	}
	unhomed := refSiteUnhomedKeywords(c, b.Schema, compositionCensusHandled)
	hasUnion := declaresUnion(b.Schema)
	ref, homeDiags := homeDeclaration(c, ts, b.Schema, ir.TypeRef{Target: id}, e.pointer, annotation.HomeOwnNode, len(unhomed) > 0 || hasUnion)
	diags = append(diags, homeDiags...)
	diags = append(diags, recordUnhomedKeywords(c, ts, ref.Target, b.Schema, unhomed, refSiteShape, e.pointer)...)
	if hasUnion {
		diags = append(diags, preserveUnionSiblings(c, ts, ref.Target, b.Schema, e.pointer, ir.ReasonDegradedLowering, refSiteUnionWhy)...)
	}
	if isBase {
		fields.m.Base = &ref
	} else {
		fields.m.Mixins = append(fields.m.Mixins, ref)
	}
	fields.compose(ref.Target, e.pointer)
	return diags
}

// applyFalseBranches closes m for each boolean `false` allOf branch and keeps
// the branch verbatim under Unmodeled. It runs after fillAdditional, whose
// result it overrides, the catch-all included
// (docs/compilers/openapi.md#allof-false-branch).
//
// A `false` branch admits nothing, so m must not stay open; the rule
// docs/compilers/openapi.md#whole-schema-fallbacks closes a bare `false` schema
// the same way. m keeps the other branches' contributions, because emptying it
// would trade one silent loss for another. The verbatim branch tells this from
// a plain `additionalProperties: false`; a diagnostic does not, being no part
// of the document. A `true` branch constrains nothing and is skipped.
func applyFalseBranches(c lowering.Ctx, m *ir.Model, s *statement.Schema, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range comp.branches {
		if !e.schema.Truth.ReadsFalse() {
			continue // `true`, a null and any schema with a body close nothing.
		}
		m.Additional = ir.AdditionalClosed
		annotation.PreserveInto(&m.Unmodeled, comp.branchKey(e.pointer),
			ir.RawValue("false"), ir.ReasonDegradedLowering, c.Place().At(e.pointer))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, e.pointer,
			"boolean false allOf branch matches nothing, so the composition matches nothing; "+
				"composed model closed and the branch kept verbatim under Unmodeled"))
	}

	if m.Additional == ir.AdditionalClosed && m.AdditionalProps != nil && m.AdditionalProps.Value != nil {
		_, keptDiags := annotation.PreserveSchemaKeyword(&m.Unmodeled, s, "additionalProperties",
			ir.ReasonDegradedLowering, comp.pointer, c.Place())
		diags = append(diags, keptDiags...)
	}
	forbidRest(m)
	return diags
}

// preserveUnmergedBranch keeps an inline allOf branch verbatim beside the
// composed model when it declares more than the merge consumes, under
// ReasonDegradedLowering and located at the branch
// (docs/compilers/openapi.md#allof-inline-residue). comp keys the entry, so
// sibling branches never overwrite one another. A flattened branch's allOf is
// consumed: allOfBranches replaced it by its own entries.
//
// A boolean branch has no keywords to leave a residue; applyFalseBranches
// handles it. A $ref branch takes neither path: fillAllOf homes its siblings on
// an alias over the target. What the branch writes for the position folds there
// where comp says it speaks.
func preserveUnmergedBranch(c lowering.Ctx, m *ir.Model, comp allOfComposition, i int) []ir.Diagnostic {
	e := comp.branches[i]
	if e.schema.Truth != statement.Stated {
		return nil // boolean branch; applyFalseBranches handles it.
	}
	speaks := comp.branchSpeaks(i)
	conjoined := comp.conjoins != "" && e.pointer == comp.conjoins
	residue := unmergedBranchKeys(e.schema.Schema, func(keyword string) bool {
		return mergeConsumes(e.schema.Schema, keyword, e.wrapper) || (speaks && folds(c, e.schema.Schema, keyword, boundHomes{value: comp.bounds})) ||
			(conjoined && conjoinedKeyword(keyword))
	})
	if len(residue) == 0 {
		return nil
	}
	kept, keptDiags := annotation.PreserveNodeInto(&m.Unmodeled, comp.branchKey(e.pointer),
		e.schema.Node, ir.ReasonDegradedLowering, c.Place().At(e.pointer))
	if !kept {
		return keptDiags
	}
	unfolded := ""
	if comp.speaks != nil {
		unfolded = unfoldedWhy(c, speaks, comp.rebased[i], e.schema.Schema, residue)
	}
	return append(keptDiags, diagUnmergedBranch(c, e.schema.Schema, residue, e, unfolded, len(comp.disjoint) > 0))
}

// diagUnmergedBranch builds the diagnostic announcing one merged branch's
// residue, naming the keywords so two branches of the same schema are told
// apart. The caller records it.
//
// A branch declaring a type that cannot be an object is a warning rather than
// info: the composed ir.Model asserts the value *is* an object, which the branch
// contradicts. Any other residue only narrows a model the IR describes correctly,
// the under-constrained case (docs/compilers/openapi.md#allof-inline-residue).
// unfolded says why the branch did not fold (unfoldedWhy), if that is the
// reason. disjoint says the schema reports its conjuncts share no value, so this
// reports residue alone.
func diagUnmergedBranch(c lowering.Ctx, bs *statement.Schema, residue []string, e allOfBranch, unfolded string, disjoint bool) ir.Diagnostic {
	if branchExcludesObject(bs) && !disjoint {
		msg := "inline allOf branch declares a type that is not an object, which the composed model asserts it is; the branch (" +
			strings.Join(keptKeywords(residue, e), ", ") + ") is kept verbatim under Unmodeled"
		if e.capped {
			msg += ", and its own allOf, nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) + ", was not flattened"
		}
		return c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, e.pointer, "%s", msg)
	}
	if unfolded != "" && !e.capped {
		return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, e.pointer, "%s", unfolded)
	}
	return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, e.pointer, "%s", unmergedWhy(residue, e))
}

// keptKeywords is residue as a diagnostic names it: a capped branch is kept for
// its depth, so its allOf is not listed as a keyword the merge declines.
func keptKeywords(residue []string, e allOfBranch) []string {
	if !e.capped {
		return residue
	}
	return slices.DeleteFunc(slices.Clone(residue), func(k string) bool { return k == "allOf" })
}

// unmergedWhy words why a branch with residue is kept: what merges and folds is
// gone from residue, which is the rest. A flattened branch's nested allOf composes
// too. A capped one is kept for its depth, not for allOf being a keyword the merge
// declines, so allOf is left out of the keywords it lists.
func unmergedWhy(residue []string, e allOfBranch) string {
	const keptTail = " is kept verbatim under Unmodeled"
	switch {
	case e.wrapper:
		return "inline allOf branch is merged in place, its nested allOf composes, and nothing reads the rest of it; the branch (" +
			strings.Join(residue, ", ") + ")" + keptTail
	case e.capped:
		rest := keptKeywords(residue, e)
		why := "inline allOf branch is nested past the flattening depth of " + strconv.Itoa(statement.MaxFlattenDepth) +
			", so its own allOf was not flattened, and the branch"
		if len(rest) > 0 {
			why += " (" + strings.Join(rest, ", ") + ")"
		}
		return why + keptTail
	default:
		return "inline allOf branch is merged in place, and nothing reads the rest of it; the branch (" +
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
	for _, k := range bs.Keys {
		if consumes(k.Name) || annotation.WritesKeywordLeftOut(bs, k.Name) {
			continue
		}
		out = append(out, k.Name)
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
	types := kindsIn(bs)
	return len(types) == 1 && types[0] == statement.KindObject
}

// branchExcludesObject reports whether the branch declares a type set with no
// `object` in it. An undeclared type set excludes nothing.
func branchExcludesObject(bs *statement.Schema) bool {
	return writesType(bs) && !slices.Contains(kindsIn(bs), statement.KindObject)
}

// selectAllOfBase returns the index into branches of the one that becomes
// Model.Base, and false when none qualifies (multiple non-hierarchy refs stay
// Mixins): of several, the first whose $ref lands on a schema taking part in a
// discriminator hierarchy (inHierarchy).
func selectAllOfBase(c lowering.Ctx, branches []allOfBranch) (int, bool) {
	refIdxs := make([]int, 0, len(branches))
	for i, e := range branches {
		if e.schema.Ref != nil {
			refIdxs = append(refIdxs, i)
		}
	}
	return merge.SelectBase(refIdxs, func(i int) bool {
		target := refBranchTarget(c, branches[i].schema)
		return stated(target) && inHierarchy(c, target)
	})
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
		return discriminatorIn(s) != nil || len(discriminatedAncestors(c, s)) > 0
	})
}
