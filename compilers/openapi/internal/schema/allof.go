// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// lowerAllOf hoists an allOf schema as a Model, classifying each branch per
// docs/compilers/openapi.md#allof-classification, which also says which
// branches are flattened first. Inline branches contribute their properties,
// with provenance into their source branch.
//
// Value constraints written beside the allOf (minProperties, ...) are read as
// lowerModel reads a plain object's, so they land on the same field, not
// Unmodeled (GitHub #407). A composition that leaves only a catch-all is a map,
// as a plain object's is (objectNode).
func lowerAllOf(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		comp := allOfBranches(s, pointer) // the one flattened list; the readers below take it, not s
		idx := propIndex(c, ts, m)
		diags = append(diags, fillAllOf(c, ts, depth, m, idx, comp)...)
		diags = append(diags, fillModelProperties(c, ts, depth, idx, s, pointer)...)
		requireMembers(m, compositionRequired(s.GetRequired(), comp))
		diags = append(diags, fillAdditional(c, ts, depth, m, s, pointer)...)
		diags = append(diags, applyFalseBranches(c, m, comp)...)
		d, discDiags := lowerDiscriminator(c, ts, s, m, nil, pointer)
		diags = append(diags, discDiags...)
		if d != nil {
			m.Discriminator = d
		}
		tag, tagDiags := subtypeDiscriminatorValue(c, s, common.ID, pointer)
		m.DiscriminatorValue = tag
		diags = append(diags, tagDiags...)
		return objectNode(m)
	})
	return id, diags
}

// compositionCensusHandled names the census keywords an allOf composition
// reads itself, so neither the census of a $ref branch nor that of the composed
// position reports them again: lowerAllOf lands every required list on the
// composed model (requireMembers).
var compositionCensusHandled = []string{"required"}

// MaxAllOfFlattenDepth bounds how many levels of nested inline allOf wrappers
// the lowering flattens, and FlattenedAllOf with it (styleguide
// bounded-everything rule). A wrapper inside this many flattened ones is not
// flattened: it is kept verbatim like any other branch with residue, so a
// source nesting deeper than any author writes still lowers, with nothing
// dropped.
const MaxAllOfFlattenDepth = 32

// allOfBranch is one inline-or-ref branch of an allOf composition after nested
// wrappers are flattened into the list (GitHub #599).
type allOfBranch struct {
	schema *oas3.JSONSchema[oas3.Referenceable]
	// pointer is where the branch was written, never rewritten to a flat index:
	// property IDs, provenance, diagnostics, the branch's Unmodeled key
	// (branchKey) and its name (lowering.NameAt) all derive from it.
	pointer jsontext.Pointer
	// wrapper marks a flattened wrapper, which contributes its properties and
	// required list, and is kept verbatim only for what else it writes.
	wrapper bool
	// capped marks an inline branch whose allOf was not flattened because it sits
	// past MaxAllOfFlattenDepth, so the diagnostic can say why it was kept.
	capped bool
}

// allOfStep is one branch walkAllOf reached.
type allOfStep struct {
	schema *oas3.JSONSchema[oas3.Referenceable]
	// path holds the index of the step within each allOf list from the walked
	// schema down. It is a buffer the walk reuses, valid until the next step.
	path    []int
	wrapper bool
	capped  bool // see allOfBranch.capped
}

// allOfFrame is one allOf list walkAllOf is walking.
type allOfFrame struct {
	list  []*oas3.JSONSchema[oas3.Referenceable]
	next  int
	owner *oas3.JSONSchema[oas3.Referenceable] // the wrapper owning list, emitted once list is done; nil for the walked schema
}

// walkAllOf yields the branches of s's allOf in source order, depth first, with
// every nested wrapper flattensInto accepts replaced by its own branches
// followed by the wrapper itself. It is the one walk the flattened list comes
// from, so a reader of the whole list (allOfBranches) and one of the schemas
// alone (allOfRefBranches) cannot disagree about what is flattened. It builds
// no pointer or key. The walk is iterative, over an explicit stack, so it adds
// no recursion cycle to this package.
func walkAllOf(s *oas3.Schema) iter.Seq[allOfStep] {
	return func(yield func(allOfStep) bool) {
		stack := []allOfFrame{{list: s.GetAllOf()}}
		var path []int
		for len(stack) > 0 {
			f := &stack[len(stack)-1]
			if f.next == len(f.list) {
				owner := f.owner
				stack = stack[:len(stack)-1]
				path = stackPath(path[:0], stack)
				if owner != nil && !yield(allOfStep{schema: owner, path: path, wrapper: true}) {
					return
				}
				continue
			}
			b := f.list[f.next]
			f.next++
			if flattensInto(b, len(stack)-1) {
				stack = append(stack, allOfFrame{list: b.GetSchema().GetAllOf(), owner: b})
				continue
			}
			path = stackPath(path[:0], stack)
			if !yield(allOfStep{schema: b, path: path, capped: wrapsAllOf(b)}) {
				return
			}
		}
	}
}

// stackPath appends to buf the index each frame of stack is at.
func stackPath(buf []int, stack []allOfFrame) []int {
	for i := range stack {
		buf = append(buf, stack[i].next-1)
	}
	return buf
}

// allOfComposition is the flattened allOf of the schema written at pointer: its
// branches, each with the pointer it was written at. A reader that takes it
// cannot reach the unflattened list.
type allOfComposition struct {
	pointer  jsontext.Pointer
	branches []allOfBranch
}

// allOfBranches returns the branches of s's allOf as walkAllOf yields them, each
// with the pointer it was written at, under the schema written at pointer. allOf
// is associative, so the list classifies exactly as its flat spelling would.
func allOfBranches(s *oas3.Schema, pointer jsontext.Pointer) allOfComposition {
	var out []allOfBranch
	for st := range walkAllOf(s) {
		p := pointer
		for _, i := range st.path {
			p += ids.Ptr("allOf", strconv.Itoa(i))
		}
		out = append(out, allOfBranch{schema: st.schema, pointer: p, wrapper: st.wrapper, capped: st.capped})
	}
	return allOfComposition{pointer: pointer, branches: out}
}

// allOfRefBranches yields the $ref branches of s's allOf after flattening,
// schemas only: the view an ancestor walk needs, which reads no pointer.
func allOfRefBranches(s *oas3.Schema) iter.Seq[*oas3.JSONSchema[oas3.Referenceable]] {
	return func(yield func(*oas3.JSONSchema[oas3.Referenceable]) bool) {
		if list := s.GetAllOf(); !slices.ContainsFunc(list, wrapsAllOf) {
			for _, b := range list { // nothing flattens: walkAllOf would yield the list itself
				if isRefBranch(b) && !yield(b) {
					return
				}
			}
			return
		}
		for st := range walkAllOf(s) {
			if isRefBranch(st.schema) && !yield(st.schema) {
				return
			}
		}
	}
}

// FlatBranch is one branch of the allOf FlattenedAllOf reads.
type FlatBranch struct {
	// Schema is the branch as written, or the wrapper a run of its own entries
	// ended with.
	Schema *oas3.JSONSchema[oas3.Referenceable]
	// Ref marks a branch that names its schema by $ref, which the reader follows
	// itself and bounds on its own.
	Ref bool
	// Capped marks an inline branch whose own allOf sits past
	// MaxAllOfFlattenDepth and was not flattened, so the reader has not reached
	// what it declares.
	Capped bool
}

// FlattenedAllOf returns s's allOf as the lowering reads it, so a reader outside
// this package takes the same flattening and the same cap, in the same order
// (docs/compilers/openapi.md#allof-classification). It reads the one walk
// allOfBranches does.
func FlattenedAllOf(s *oas3.Schema) []FlatBranch {
	var out []FlatBranch
	for st := range walkAllOf(s) {
		out = append(out, FlatBranch{Schema: st.schema, Ref: isRefBranch(st.schema), Capped: st.capped})
	}
	return out
}

// branchKey is the Unmodeled key an allOf branch at bptr is kept under: "openapi:"
// and its path from the composed schema. The path's tokens are `allOf` and
// decimal indices, so trimming the prefix is exact.
func (a allOfComposition) branchKey(bptr jsontext.Pointer) string {
	return "openapi:" + strings.TrimPrefix(string(bptr), string(a.pointer)+"/")
}

// flattensInto reports whether the allOf branch b, at nesting depth depth, is a
// wrapper walkAllOf flattens, as
// docs/compilers/openapi.md#allof-classification states it. What else b writes
// is no concern of this predicate: the merge keeps that as residue
// (preserveUnmergedBranch).
func flattensInto(b *oas3.JSONSchema[oas3.Referenceable], depth int) bool {
	return depth < MaxAllOfFlattenDepth && wrapsAllOf(b)
}

// wrapsAllOf reports whether b is inline and declares a non-empty allOf. An empty
// allOf, and a boolean branch (its schema is nil; GetAllOf is nil-safe), wrap
// nothing.
func wrapsAllOf(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	return !isRefBranch(b) && len(b.GetSchema().GetAllOf()) > 0
}

// compositionRequired collects every required-property name declared across an
// allOf composition, in source order: each branch's required list (a flattened
// wrapper's included), then own, the list the composed schema writes itself. A
// branch is read from its local schema, never its resolved $ref target — that
// required list belongs to the target's own model (issue #29).
func compositionRequired(own []string, comp allOfComposition) []string {
	var out []string
	for _, e := range comp.branches {
		bs := e.schema.GetSchema() // the branch's own local schema; nil for a bare `false`
		if bs == nil {
			continue
		}
		out = append(out, bs.GetRequired()...)
	}
	return append(out, own...)
}

// fillAllOf classifies and lowers the branches of comp into m (GitHub #599).
//
// An inline branch merges in place: only its `properties` and `required`
// (requireMembers) merge, the rest staying verbatim beside the model
// (preserveUnmergedBranch, GitHub #123). Merging more would need a precedence
// rule between branches.
//
// A $ref branch owns a node: keywords beside its $ref, if any, go on an alias
// that homeDeclaration hoists there, and the composition points at it (GitHub
// #143). The census every $ref site runs keeps what the alias cannot model,
// except `required` (compositionCensusHandled); a co-declared oneOf/anyOf is kept
// too (GitHub #406).
func fillAllOf(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, idx *merge.PropIndex, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	base, hasBase := selectAllOfBase(c.Bindings(), comp.branches)
	for i, e := range comp.branches {
		if !isRefBranch(e.schema) {
			diags = append(diags, fillModelProperties(c, ts, depth, idx, e.schema.GetSchema(), e.pointer)...)
			diags = append(diags, preserveUnmergedBranch(c, m, e, comp.branchKey(e.pointer))...)
			continue
		}
		diags = append(diags, fillRefBranch(c, ts, m, e, hasBase && i == base)...)
	}
	return diags
}

// fillRefBranch lowers one $ref allOf branch and adds it to m as its Base, when
// isBase, or as a Mixin. A $ref it cannot resolve adds nothing, and is reported
// unless it lies on a loop of $refs, which load reports once (onLoop).
func fillRefBranch(c lowering.Ctx, ts *compile.Types, m *ir.Model, e allOfBranch, isBase bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	b := e.schema
	id, ok := resolveSchemaRef(c, ts, b)
	if !ok {
		if !onLoop(c, b) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, e.pointer,
				"unresolved allOf $ref %q%s", b.GetRef().String(), unresolvedWhy(c, b)))
		}
		return diags
	}
	bs := b.GetSchema()
	unhomed := refSiteUnhomedKeywords(bs, compositionCensusHandled)
	hasUnion := declaresUnion(bs)
	ref, homeDiags := homeDeclaration(c, ts, bs, ir.TypeRef{Target: id}, e.pointer, annotation.HomeOwnNode, len(unhomed) > 0 || hasUnion)
	diags = append(diags, homeDiags...)
	diags = append(diags, recordUnhomedKeywords(c, ts, ref.Target, bs, unhomed, refSiteShape, e.pointer)...)
	if hasUnion {
		diags = append(diags, preserveUnionSiblings(c, ts, ref.Target, bs, e.pointer, ir.ReasonDegradedLowering, refSiteUnionWhy)...)
	}
	if isBase {
		m.Base = &ref
	} else {
		m.Mixins = append(m.Mixins, ref)
	}
	return diags
}

// applyFalseBranches closes m for each boolean `false` allOf branch and keeps
// the branch verbatim under Unmodeled. It runs after fillAdditional, whose
// result it overrides.
//
// A `false` branch admits nothing, so m must not stay open; the rule
// docs/compilers/openapi.md#whole-schema-fallbacks closes a bare `false` schema
// the same way. m keeps the other branches' contributions, because emptying it
// would trade one silent loss for another. The verbatim branch tells this from
// a plain `additionalProperties: false`; a diagnostic does not, being no part
// of the document. A `true` branch constrains nothing and is skipped.
func applyFalseBranches(c lowering.Ctx, m *ir.Model, comp allOfComposition) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, e := range comp.branches {
		if !annotation.IsFalseSchema(e.schema) {
			continue // `true`, a null and any schema with a body close nothing.
		}
		m.Additional = ir.AdditionalClosed
		annotation.PreserveInto(&m.Unmodeled, comp.branchKey(e.pointer),
			ir.RawValue("false"), ir.ReasonDegradedLowering, c.ProvenanceAt(e.pointer))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, e.pointer,
			"boolean false allOf branch matches nothing, so the composition matches nothing; "+
				"composed model closed and the branch kept verbatim under Unmodeled"))
	}
	return diags
}

// preserveUnmergedBranch keeps an inline allOf branch verbatim beside the
// composed model when it declares more than the merge consumes, under
// ReasonDegradedLowering and located at the branch
// (docs/compilers/openapi.md#allof-inline-residue). key names the entry, so
// sibling branches never overwrite one another. A flattened branch's allOf is
// consumed: allOfBranches replaced it by its own entries.
//
// A boolean branch has no keywords to leave a residue; applyFalseBranches
// handles it. A $ref branch takes neither path: fillAllOf homes its siblings on
// an alias over the target.
func preserveUnmergedBranch(c lowering.Ctx, m *ir.Model, e allOfBranch, key string) []ir.Diagnostic {
	bs := e.schema.GetSchema()
	if bs == nil {
		return nil // boolean branch; applyFalseBranches handles it.
	}
	residue := unmergedBranchKeys(bs, e.wrapper)
	if len(residue) == 0 {
		return nil
	}
	kept, keptDiags := annotation.PreserveNodeInto(&m.Unmodeled, key,
		bs.GetRootNode(), ir.ReasonDegradedLowering, c.ProvenanceAt(e.pointer))
	if !kept {
		return keptDiags
	}
	return append(keptDiags, diagUnmergedBranch(c, bs, residue, e))
}

// diagUnmergedBranch builds the diagnostic announcing one merged branch's
// residue, naming the keywords so two branches of the same schema are told
// apart. The caller records it.
//
// A branch declaring a type that cannot be an object is a warning rather than
// info: the composed node is an ir.Model, which asserts the value *is* an object,
// so the IR contradicts the branch — `allOf: [{type: string, maxLength: 3}]`
// lowers to an empty model. Any other residue only narrows a model the IR
// describes correctly, the under-constrained case
// (docs/compilers/openapi.md#allof-inline-residue).
func diagUnmergedBranch(c lowering.Ctx, bs *oas3.Schema, residue []string, e allOfBranch) ir.Diagnostic {
	if branchExcludesObject(bs) {
		msg := "inline allOf branch declares a type that is not an object, which the composed model asserts it is; the branch (" +
			strings.Join(keptKeywords(residue, e), ", ") + ") is kept verbatim under Unmodeled"
		if e.capped {
			msg += ", and its own allOf, nested past the flattening depth of " + strconv.Itoa(MaxAllOfFlattenDepth) + ", was not flattened"
		}
		return c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, e.pointer, "%s", msg)
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

// unmergedWhy words why a branch with residue is kept. A flattened branch's
// nested allOf composes too. A capped one is kept for its depth, not for allOf
// being a keyword the merge declines, so allOf is left out of the keywords it
// lists.
func unmergedWhy(residue []string, e allOfBranch) string {
	const keptTail = " is kept verbatim under Unmodeled"
	switch {
	case e.wrapper:
		return "inline allOf branch is merged in place, so only its properties, required list and nested allOf compose; the branch (" +
			strings.Join(residue, ", ") + ")" + keptTail
	case e.capped:
		rest := keptKeywords(residue, e)
		why := "inline allOf branch is nested past the flattening depth of " + strconv.Itoa(MaxAllOfFlattenDepth) +
			", so its own allOf was not flattened; only its properties and required list compose, and the branch"
		if len(rest) > 0 {
			why += " (" + strings.Join(rest, ", ") + ")"
		}
		return why + keptTail
	default:
		return "inline allOf branch is merged in place, so only its properties and required list compose; the branch (" +
			strings.Join(residue, ", ") + ")" + keptTail
	}
}

// unmergedBranchKeys returns the keywords the branch declares that the merge does
// not consume, in the order the parser's model holds them.
//
// The keys come off the branch's own raw mapping rather than from a list of
// keywords worth preserving, which is what keeps this from rotting: a keyword a
// later dialect adds counts as unconsumed the moment a source writes it, with
// nobody to remember it here.
func unmergedBranchKeys(bs *oas3.Schema, flattened bool) []string {
	declared := rawMappingKeys(bs.GetRootNode())
	out := make([]string, 0, len(declared))
	for _, key := range declared {
		if mergeConsumes(bs, key, flattened) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// mergeConsumes reports whether fillAllOf's in-place merge reads branch keyword
// key. It is the complete statement of what the merge keeps; everything else the
// branch wrote is residue. `allOf` is consumed exactly when flattened
// (docs/compilers/openapi.md#allof-classification); otherwise it is residue like
// the rest.
func mergeConsumes(bs *oas3.Schema, key string, flattened bool) bool {
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

// declaresObjectOnly reports whether the branch's declared type is exactly
// `object`, which the composed ir.Model already restates so the merge loses
// nothing by reading it. A set — `[object, "null"]` included — says more than
// that, and the more is what the merge would drop.
func declaresObjectOnly(bs *oas3.Schema) bool {
	types := bs.GetType()
	return len(types) == 1 && types[0] == oas3.SchemaTypeObject
}

// branchExcludesObject reports whether the branch declares a type set with no
// `object` in it. An undeclared type set excludes nothing.
func branchExcludesObject(bs *oas3.Schema) bool {
	types := bs.GetType()
	return len(types) > 0 && !slices.Contains(types, oas3.SchemaTypeObject)
}

// rawMappingKeys returns the keys a YAML mapping effectively writes, a
// document node unwrapped first, in the order the parser's model holds them,
// or nil when the node is no mapping. It enumerates what nodeview.Child looks
// up one key at a time.
//
// Effective, not literal: a branch spelled `*anchor` writes the anchored
// mapping's keys, and `<<: *anchor` the merged-in keys. A residue derived from
// the literal text would report `<<`, or for a whole-branch alias no keys at
// all, which reads as "the merge consumed everything" and drops the branch
// silently. nodeview.View defines how a mapping is read and bounds the
// expansion (nodeview.MergeDepthLimit).
func rawMappingKeys(root *yaml.Node) []string {
	pairs := nodeview.Pairs(root)
	if len(pairs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	return keys
}

// selectAllOfBase returns the index into branches of the one that becomes
// Model.Base, and false when none qualifies (multiple non-hierarchy refs stay
// Mixins). refs says where each branch's $ref ends.
func selectAllOfBase(refs *typed.Bindings, branches []allOfBranch) (int, bool) {
	refIdxs := make([]int, 0, len(branches))
	for i, e := range branches {
		if isRefBranch(e.schema) {
			refIdxs = append(refIdxs, i)
		}
	}
	if len(refIdxs) == 1 {
		return refIdxs[0], true
	}
	for _, i := range refIdxs {
		if refTargetHasDiscriminator(refs, branches[i].schema) {
			return i, true
		}
	}
	return 0, false
}

// refTargetHasDiscriminator reports whether a $ref branch resolves to a schema
// that carries a discriminator (it anchors a polymorphic hierarchy).
func refTargetHasDiscriminator(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	target := refBranchTarget(refs, b)
	return target != nil && target.GetDiscriminator() != nil
}
