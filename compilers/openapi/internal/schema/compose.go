package schema

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// lowerAllOf hoists an allOf schema as a Model, classifying each branch per
// ir-design §4.3: the sole $ref (or a $ref whose target anchors a discriminator
// hierarchy) becomes Base; other $refs become Mixins in source order; inline
// branches contribute their properties, each carrying provenance into the
// allOf branch it came from.
func lowerAllOf(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		m := &ir.Model{TypeCommon: common}
		diags = append(diags, fillAllOf(c, ts, anchors, depth, m, s, pointer)...)
		diags = append(diags, fillModelProperties(c, ts, anchors, depth, m, s, pointer)...)
		diags = append(diags, applyCompositionRequired(c, m, s, pointer)...)
		diags = append(diags, fillAdditional(c, ts, anchors, depth, m, s, pointer, hint)...)
		diags = append(diags, applyFalseBranches(c, m, s, pointer)...)
		d, discDiags := lowerDiscriminator(c, ts, s, m, pointer)
		diags = append(diags, discDiags...)
		if d != nil {
			m.Discriminator = d
		}
		m.DiscriminatorValue = subtypeDiscriminatorValue(c, ts, s, common.ID, pointer)
		return m
	})
	return id, diags
}

// branchCensusHandled names the census keywords an allOf $ref branch's own
// composition reads, so the branch census leaves them alone: applyCompositionRequired
// ORs every branch's required list onto the composed model.
var branchCensusHandled = []string{"required"}

// requiredEntry is one `required` name declared somewhere in an allOf
// composition, paired with the pointer of the schema that declared it (an
// allOf branch, or the composed schema itself) so a diagnostic can point the
// author at the right site.
type requiredEntry struct {
	name    string
	pointer string
}

// compositionRequired collects every required-property name declared across an
// allOf composition, in source order: each branch's required list, then the
// composed schema's own. A branch is read from its local schema, never its
// resolved $ref target — that required list belongs to the target's own
// model (issue #29).
func compositionRequired(s *oas3.Schema, pointer string) []requiredEntry {
	var out []requiredEntry
	for i, b := range s.GetAllOf() {
		bs := b.GetSchema() // the branch's own local schema; nil for a bare `false`
		if bs == nil {
			continue
		}
		bptr := pointer + ids.Ptr("allOf", strconv.Itoa(i))
		for _, name := range bs.GetRequired() {
			out = append(out, requiredEntry{name: name, pointer: bptr})
		}
	}
	for _, name := range s.GetRequired() {
		out = append(out, requiredEntry{name: name, pointer: pointer})
	}
	return out
}

// applyCompositionRequired OR-s every composition-scope required name onto m's
// own properties, matching by wire name; it never clears a Required already
// set. An entry matching no own property has no IR home (ir-design §4.3) and
// is diagnosed via diagUnattachableRequired instead of dropped silently.
func applyCompositionRequired(c lowering.Ctx, m *ir.Model, s *oas3.Schema, pointer string) []ir.Diagnostic {
	entries := compositionRequired(s, pointer)
	if len(entries) == 0 {
		return nil
	}
	byWire := merge.WireNameIndex(m.Properties)
	var diags []ir.Diagnostic
	for _, e := range entries {
		if i, ok := byWire[e.name]; ok {
			m.Properties[i].Required = true
			continue
		}
		diags = append(diags, diagUnattachableRequired(c, m, e))
	}
	return diags
}

// diagUnattachableRequired builds the diagnostic for a composition-scope
// required name matching no own property; the caller records it. It is a warning when m composes a ref (Base or a Mixin):
// the requirement plausibly belongs to that base/mixin's own property, so real
// fidelity is lost. Otherwise it is info: the spec just names a property
// nothing declares, which is legal JSON Schema with nothing to lose.
func diagUnattachableRequired(c lowering.Ctx, m *ir.Model, e requiredEntry) ir.Diagnostic {
	if m.Base != nil || len(m.Mixins) > 0 {
		return c.DiagAt(ir.SeverityWarning, diag.UnattachableRequired, e.pointer,
			"required %q matches no property declared here; a base or mixin may declare it, and the requirement cannot be carried across composition", e.name)
	}
	return c.DiagAt(ir.SeverityInfo, diag.UnattachableRequired, e.pointer,
		"required %q matches no property declared here", e.name)
}

// fillAllOf classifies and lowers the allOf branches into m.
//
// An inline branch is merged in place rather than lowered through Ref, so
// it has no node of its own and the merge reads only its `properties` — plus its
// `required` list, which applyCompositionRequired collects separately. Whatever
// else the branch declares is kept verbatim beside the composed model instead of
// being dropped (preserveUnmergedBranch, GitHub #123).
//
// A $ref branch does own a node, so keywords written beside its $ref are modeled
// rather than preserved: homeDeclaration hoists an alias at the branch position
// to carry them, and the composition points at that (GitHub #143). A branch
// writing nothing beside its $ref hoists nothing and composes straight to the
// target, so this costs a node only where there is something to keep.
//
// The keywords an alias cannot model are kept on it instead, by the same census
// every other $ref site runs (refSiteRef). `required` is left out of that census
// because this composition reads it: applyCompositionRequired ORs every branch's
// required list onto the composed model, so it is consumed here rather than lost.
//
// The merge itself is left as it is: merging a branch's own docs, constraints or
// openness upward onto m would need a precedence rule for branches that disagree,
// and some of it has no home to merge into at all — Model.Constraints bounds the
// property set's cardinality, so a scalar branch's maxLength cannot go there.
// Verbatim beside the model needs neither, and keeps the branch recoverable.
func fillAllOf(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, m *ir.Model, s *oas3.Schema, pointer string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	branches := s.GetAllOf()
	baseIdx := selectAllOfBase(branches)
	for i, b := range branches {
		bptr := pointer + ids.Ptr("allOf", strconv.Itoa(i))
		if !isRefBranch(b) {
			diags = append(diags, fillModelProperties(c, ts, anchors, depth, m, b.GetSchema(), bptr)...)
			diags = append(diags, preserveUnmergedBranch(c, m, b.GetSchema(), i, bptr)...)
			continue
		}
		id, ok, refDiags := resolveSchemaRef(c, ts, anchors, depth, b, b.GetRef().String())
		diags = append(diags, refDiags...)
		if !ok {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, bptr,
				"unresolved allOf $ref %q", b.GetRef().String()))
			continue
		}
		bs := b.GetSchema()
		unhomed := unhomedKeywords(bs, nil, branchCensusHandled)
		ref, homeDiags := homeDeclaration(c, ts, anchors, bs, ir.TypeRef{Target: id}, bptr, branchHint(b, i), annotation.HomeOwnNode, len(unhomed) > 0)
		diags = append(diags, homeDiags...)
		diags = append(diags, recordUnhomedKeywords(c, ts, ref.Target, bs, unhomed, refSiteShape, bptr)...)
		if i == baseIdx {
			m.Base = &ref
		} else {
			m.Mixins = append(m.Mixins, ref)
		}
	}
	return diags
}

// applyFalseBranches applies the lowering a boolean `false` allOf branch calls
// for. It runs after fillAdditional, whose result it overrides.
//
// A `false` branch admits nothing, so the composition it joins admits nothing.
// ir-design §4.8 already fixes the lowering of a `false` schema in its own right
// — a closed empty Model with an info diagnostic, which falseSchema applies —
// and the merge did not carry that rule across composition, so the same source
// construct lowered two ways depending on where it appeared: `allOf: [false]`
// became an *open* empty model, the most permissive shape the IR has, for a
// source that admits nothing.
//
// The composed model keeps what the other branches contributed rather than being
// emptied to match §4.8 literally: closing it is the nearest shape the IR has,
// and discarding the rest would trade one silent loss for another. The branch is
// kept verbatim beside it, which is what distinguishes a composition containing
// `false` from a model that merely wrote `additionalProperties: false` — the
// diagnostic says so too, but a diagnostic is not part of the document.
//
// A `true` branch admits everything, so contributing nothing from it is exact
// and there is nothing to report.
func applyFalseBranches(c lowering.Ctx, m *ir.Model, s *oas3.Schema, pointer string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for i, b := range s.GetAllOf() {
		if b == nil || !b.IsBool() {
			continue
		}
		if v := b.GetBool(); v == nil || *v {
			continue // `true` constrains nothing.
		}
		bptr := pointer + ids.Ptr("allOf", strconv.Itoa(i))
		m.Additional = ir.AdditionalClosed
		Preserve(c, &m.Unmodeled, "openapi:allOf/"+strconv.Itoa(i),
			ir.RawValue("false"), ir.ReasonDegradedLowering, bptr)

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, bptr,
			"boolean false allOf branch matches nothing, so the composition matches nothing; "+
				"composed model closed and the branch kept verbatim under Unmodeled"))
	}
	return diags
}

// preserveUnmergedBranch keeps an inline allOf branch verbatim beside the
// composed model when the branch declares more than the merge consumes, under
// ReasonDegradedLowering and located at the branch itself (ir-design §4.8).
// branchIdx keys the entry, so sibling branches never overwrite one another.
//
// A boolean branch has no keywords for a residue to be derived from and is
// handled by applyFalseBranches instead, which is a question about the composed
// node's shape rather than about which of a branch's keywords survive.
//
// A $ref branch is not an inline branch at all and takes neither path: it owns a
// node, so fillAllOf homes its `$ref`-adjacent siblings on an alias over the
// target rather than preserving them here.
func preserveUnmergedBranch(c lowering.Ctx, m *ir.Model, bs *oas3.Schema, branchIdx int, bptr string) []ir.Diagnostic {
	if bs == nil {
		return nil // boolean branch; applyFalseBranches handles it.
	}
	residue := unmergedBranchKeys(bs)
	if len(residue) == 0 {
		return nil
	}
	kept, keptDiags := PreserveNode(c, &m.Unmodeled, "openapi:allOf/"+strconv.Itoa(branchIdx),
		bs.GetRootNode(), ir.ReasonDegradedLowering, bptr)
	if !kept {
		return keptDiags
	}
	return append(keptDiags, diagUnmergedBranch(c, bs, residue, bptr))
}

// diagUnmergedBranch builds the diagnostic announcing one merged branch's
// residue, naming the keywords so two branches of the same schema are told
// apart. The caller records it.
//
// A branch declaring a type that cannot be an object is a warning rather than
// info: the composed node is an ir.Model, which asserts the value *is* an object,
// so the IR states something the branch contradicts — `allOf: [{type: string,
// maxLength: 3}]` lowers to an empty model. Any other residue leaves a model the
// IR describes correctly and only narrows it further, which is §4.8's
// under-constrained case.
func diagUnmergedBranch(c lowering.Ctx, bs *oas3.Schema, residue []string, bptr string) ir.Diagnostic {
	kept := strings.Join(residue, ", ")
	if branchExcludesObject(bs) {
		return c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, bptr,
			"inline allOf branch declares a type that is not an object, which the composed model asserts it is; the branch (%s) is kept verbatim under Unmodeled", kept)
	}
	return c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, bptr,
		"inline allOf branch is merged in place, so only its properties and required list compose; the branch (%s) is kept verbatim under Unmodeled", kept)
}

// unmergedBranchKeys returns the keywords the branch declares that the merge does
// not consume, in source order.
//
// The keys come off the branch's own raw mapping rather than from a list of
// keywords worth preserving, which is what keeps this from rotting: a keyword a
// later dialect adds counts as unconsumed the moment a source writes it, with
// nobody to remember it here.
func unmergedBranchKeys(bs *oas3.Schema) []string {
	declared := rawMappingKeys(bs.GetRootNode())
	out := make([]string, 0, len(declared))
	for _, key := range declared {
		if mergeConsumes(bs, key) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// mergeConsumes reports whether fillAllOf's in-place merge reads branch keyword
// key. It is the complete statement of what the merge keeps; everything else the
// branch wrote is residue.
func mergeConsumes(bs *oas3.Schema, key string) bool {
	switch key {
	case "properties":
		return true // fillModelProperties merges these onto m
	case "required":
		return true // applyCompositionRequired ORs these onto m's properties
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

// rawMappingKeys returns the keys a YAML mapping effectively writes, in source
// order, or nil when the node is no mapping. It is annotation.RawChildNode's
// enumerating counterpart: that answers "what is written at this key", this one
// "which keys are written".
//
// Effective, not literal: a branch spelled `*anchor` writes the anchored
// mapping's keys and one spelled `<<: *anchor` writes the merged-in keys, so
// deriving a residue from the literal text would report `<<` — or, for a whole
// branch replaced by an alias, no keys at all, which reads as "the merge consumed
// everything" and drops the branch silently. nodeview.View is the package's statement
// of how speakeasy reads a mapping, and bounds the expansion (maxMergeDepth).
func rawMappingKeys(root *yaml.Node) []string {
	mapping := rawMapping(root)
	if mapping == nil {
		return nil
	}
	pairs := nodeview.New().MappingPairs(mapping)
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	return keys
}

// rawMapping returns the mapping node root stands for: a document wrapper is
// unwrapped and an alias followed to its anchor (bounded by nodeview.Deref). Anything
// that is not a mapping after that — a scalar, a sequence, nil — yields nil, so
// a caller needs no kind check of its own.
func rawMapping(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	root = nodeview.Deref(root)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// selectAllOfBase returns the branch index that becomes Model.Base, or -1 when
// none qualifies (multiple non-hierarchy refs stay Mixins).
func selectAllOfBase(branches []*oas3.JSONSchema[oas3.Referenceable]) int {
	refIdxs := make([]int, 0, len(branches))
	for i, b := range branches {
		if isRefBranch(b) {
			refIdxs = append(refIdxs, i)
		}
	}
	if len(refIdxs) == 1 {
		return refIdxs[0]
	}
	for _, i := range refIdxs {
		if refTargetHasDiscriminator(branches[i]) {
			return i
		}
	}
	return -1
}

// isRefBranch reports whether a composition branch is a $ref rather than an
// inline schema.
func isRefBranch(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	if b == nil {
		return false
	}
	if b.IsReference() {
		return true
	}
	s := b.GetSchema()
	return s != nil && s.Ref != nil
}

// isInlineBranch reports whether a composition branch is written inline rather
// than as a $ref. It is isRefBranch's negation, named so a search over branches
// reads as the question being asked.
func isInlineBranch(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	return !isRefBranch(b)
}

// refTargetHasDiscriminator reports whether a $ref branch resolves to a schema
// that carries a discriminator (it anchors a polymorphic hierarchy).
func refTargetHasDiscriminator(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	target := refBranchTarget(b)
	return target != nil && target.GetDiscriminator() != nil
}

// refBranchTarget returns the schema a composition branch resolves to, or nil
// when the branch is inline, names nothing this compilation resolved, or names a
// bare boolean schema (which has no *oas3.Schema of its own).
func refBranchTarget(b *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if !isRefBranch(b) {
		return nil
	}
	resolved := b.GetResolvedSchema()
	if resolved == nil {
		return nil
	}
	return resolved.GetSchema()
}

// subtypeDiscriminatorValue returns the wire tag value this allOf subtype
// carries within its discriminator hierarchy, or "" when no ancestor of it
// anchors one. Per ir-design §4.3 the value is the mapping key that points at
// this subtype, falling back to the subtype's own schema name (OpenAPI's
// implicit mapping) when the mapping omits it.
//
// Every discriminated ancestor is asked, not only the immediate base: a
// hierarchy deeper than two levels composes an intermediate schema that declares
// no discriminator of its own, and reading one hop found nothing there and
// dropped the key the ancestor spells for this subtype without a word
// (GitHub #305).
func subtypeDiscriminatorValue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, id ir.TypeID, pointer string) string {
	ds := ancestorDiscriminators(s)
	if len(ds) == 0 {
		return ""
	}
	for _, d := range ds {
		if tag, ok := mappingTagFor(c, ts, d, id); ok {
			return tag
		}
	}
	return refLastSegment(pointer)
}

// mappingTagFor returns the key d's mapping spells for the type id, and whether
// the mapping names it at all. The two answers are distinct: a mapping that
// names no target for id leaves the caller to fall back to the implicit name,
// which an empty key would be indistinguishable from.
func mappingTagFor(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, id ir.TypeID) (string, bool) {
	m := d.GetMapping()
	if m == nil {
		return "", false
	}
	for tag, target := range m.All() {
		if tid, ok := mappingTargetID(c, ts, target); ok && tid == id {
			return tag, true
		}
	}
	return "", false
}

// maxDiscriminatorAncestorDepth bounds how many composition levels
// ancestorDiscriminators climbs (styleguide bounded-everything rule). The visited
// set below is what makes the walk terminate; this is the explicit limit, set far
// beyond any hierarchy a source document plausibly declares.
const maxDiscriminatorAncestorDepth = 256

// ancestorDiscriminators returns the discriminators declared on s's composition
// ancestors, nearest first: the resolved targets of s's own $ref branches in
// source order, then those targets' $ref branches, and so on.
//
// Level by level rather than chain by chain, so "nearest" means fewest hops —
// which is what decides between two ancestors whose mappings both name the same
// subtype.
//
// The visited set is what bounds the work, and the depth cap does not stand in
// for it wherever the walk branches. A schema more than one branch reaches is
// walked once with the set and once per path without it, so the frontier
// multiplies by the fan-out at every level: 2^level for a chain of diamonds,
// 5^level for the six-schema cycle where each composes all the others. The cap
// then bounds the number of levels, not the work, and the walk stops finishing.
//
// A cycle with no fan-out is the one shape the cap alone does handle — `A: allOf
// [$ref B]`, `B: allOf [$ref A]` keeps a frontier of one and simply runs the cap
// out. That is why the case is not the cycle but the branching, and why both
// TestAllOf_DiscriminatorValueCyclicComposition (cyclic, fan-out 5) and
// TestCompile_SharedCompositionAncestorsDoNotAmplify (acyclic, fan-out 2) are
// needed to hold it: the first stops finishing, the second says so and names why.
func ancestorDiscriminators(s *oas3.Schema) []*oas3.Discriminator {
	var out []*oas3.Discriminator
	visited := make(map[*oas3.Schema]bool)
	// s is visited before the walk starts, so a composition cycle cannot bring it
	// back as one of its own ancestors. Without this a schema in a cycle that
	// declares a discriminator answers to its own mapping, which is a hierarchy of
	// one thing standing above itself.
	visited[s] = true
	level := []*oas3.Schema{s}
	for depth := 0; depth < maxDiscriminatorAncestorDepth && len(level) > 0; depth++ {
		next := make([]*oas3.Schema, 0, len(level))
		for _, cur := range level {
			next = append(next, unvisitedRefTargets(cur, visited)...)
		}
		for _, target := range next {
			if d := target.GetDiscriminator(); d != nil {
				out = append(out, d)
			}
		}
		level = next
	}
	return out
}

// unvisitedRefTargets returns the schemas s's $ref composition branches resolve
// to, in source order, skipping those already seen and marking those it returns.
// Marking as it enqueues rather than when the walk reaches them is what keeps a
// schema two branches both name from being queued, and walked, twice.
func unvisitedRefTargets(s *oas3.Schema, visited map[*oas3.Schema]bool) []*oas3.Schema {
	var out []*oas3.Schema
	for _, b := range s.GetAllOf() {
		target := refBranchTarget(b)
		if target == nil || visited[target] {
			continue
		}
		visited[target] = true
		out = append(out, target)
	}
	return out
}

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A two-variant {X, null} set
// collapses to nullable X (ir-design §3.3); everything else becomes a Union
// with one Variant per branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, ih, ok := nullUnionCollapse(s, pointer); ok {
		ref, diags := Ref(c, ts, anchors, depth, inner, ip, ih)
		ref.Nullable = true
		return ref, diags
	}
	var diags []ir.Diagnostic
	tid := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		def, unionDiags := buildUnion(c, ts, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string) ir.TypeRef {
				ref, refDiags := Ref(c, ts, anchors, depth, b, vptr, vhint)
				diags = append(diags, refDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	return ir.TypeRef{Target: tid, Nullable: schemaAdmitsNull(s)}, diags
}

// unionLowering names how a oneOf/anyOf co-declared with structural keywords is
// lowered.
type unionLowering int

const (
	// unionDistributed distributes the sibling composition across the union
	// variants: one Model per branch carrying the enclosing Base/Mixins and own
	// properties conjoined with that branch. The Union is the value and the
	// composition rides on every variant, which is exactly the ∧-of-∨ the source
	// wrote.
	unionDistributed unionLowering = iota
	// unionValidationOnly marks a union whose branches only narrow what the
	// sibling body accepts; it is kept verbatim under ReasonValidationOnly.
	unionValidationOnly
	// unionUncomposableBody marks a union beside a body that is not a Model — a
	// const, an enum, a scalar, a bare catch-all — so there are no composition
	// fields to distribute into.
	unionUncomposableBody
	// unionBothCombinators marks a schema declaring oneOf and anyOf at once:
	// only one of the two can become the value, so distributing either drops the
	// other.
	unionBothCombinators
	// unionInlineBranch marks a union with a branch written inline rather than as
	// a $ref, so it names no node for Base/Mixins to point at.
	unionInlineBranch
	// unionUnresolvedBranch marks a union whose every branch is a $ref but at
	// least one names nothing this compilation can resolve — a cross-document
	// target, an undeclared component, a pointer addressing no schema.
	unionUnresolvedBranch
	// unionDiscriminated marks a union whose branches a declared discriminator
	// binds by name, which distribution would break.
	unionDiscriminated
)

// classifyUnionSiblings picks the lowering for a schema whose oneOf/anyOf sits
// beside structural keywords. JSON Schema conjoins keywords, so such a schema is
// an intersection of a structural body and a union, and the IR deliberately has
// no intersection combinator (ir-design §15). Every outcome keeps both sides —
// classified where the IR can express the conjunction, verbatim where it cannot.
//
// A declared discriminator rules distribution out: its mapping is written
// against the branch schemas, which distribution replaces with synthesized
// composed models the mapping cannot name (pass/validate's
// discriminator-missing-variant rule states the same requirement from the other
// side). ir-design §4.8 enumerates this and the other four residual shapes.
func classifyUnionSiblings(c lowering.Ctx, s *oas3.Schema) unionLowering {
	if !unionBranchesDeclareShape(s) {
		return unionValidationOnly
	}
	if !composesAsModel(s) {
		return unionUncomposableBody
	}
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return unionBothCombinators
	}
	if branches, _, _ := unionBranches(s); slices.ContainsFunc(branches, isInlineBranch) {
		return unionInlineBranch
	}
	if !branchesNameReferents(c, s) {
		return unionUnresolvedBranch
	}
	if s.GetDiscriminator() != nil {
		return unionDiscriminated
	}
	return unionDistributed
}

// branchesNameReferents reports whether every union branch can be conjoined with
// the sibling model. Base and Mixins conjoin *by reference*, so a branch
// qualifies exactly when it names a schema this compilation resolves; an inline
// branch (ruled out before this) could only be merged in keyword by keyword, and
// an unresolvable $ref names nothing to merge either way. The test is over the
// whole union because a half-distributed one would leave variants disagreeing
// about whether they carry the body, with nothing in the IR saying which.
func branchesNameReferents(c lowering.Ctx, s *oas3.Schema) bool {
	branches, _, _ := unionBranches(s)
	for _, b := range branches {
		if !c.RefScope().NamesReferent(b, b.GetRef().String()) {
			return false
		}
	}
	return true
}

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings.
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		return lowerBesideUnmodeledUnion(c, ts, anchors, depth, s, pointer, hint, reason, why)
	}
	switch classifyUnionSiblings(c, s) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, anchors, depth, s, pointer, hint)
	case unionValidationOnly:
		return beside(ir.ReasonValidationOnly, "")
	case unionUncomposableBody:
		return beside(ir.ReasonDegradedLowering,
			"the body is not a model, so it carries no composition to distribute into")
	case unionBothCombinators:
		return beside(ir.ReasonDegradedLowering,
			"oneOf and anyOf are both declared, so distributing either would drop the other")
	case unionInlineBranch:
		return beside(ir.ReasonDegradedLowering,
			"a branch is written inline, so it names no referent to conjoin the body with")
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(c, s, pointer), diags...)
	default: // unionDiscriminated
		return beside(ir.ReasonDegradedLowering,
			"a declared discriminator binds the branches by name, which distributing them would break")
	}
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, or none when they all resolve. The branch text survives verbatim under Unmodeled, so
// nothing is dropped — but a reference that resolves nowhere is a defect of the
// document itself, reported at the same severity as everywhere else, and the
// info diagnostic beside it explains only the lowering.
func diagUnresolvedBranches(c lowering.Ctx, s *oas3.Schema, pointer string) []ir.Diagnostic {
	branches, key, _ := unionBranches(s)
	var diags []ir.Diagnostic
	for i, b := range branches {
		ref := b.GetRef().String()
		if c.RefScope().NamesReferent(b, ref) {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr(key, strconv.Itoa(i)),
			"union branch $ref %q resolves to nothing this document declares; the branch is kept verbatim", ref))
	}
	return diags
}

// composesAsModel reports whether the structural siblings lower to a Model,
// which is the only node with composition fields for a union to distribute
// across. It mirrors lower's dispatch: const and enum win over everything, then
// allOf, then a declared object type or a bare property set.
func composesAsModel(s *oas3.Schema) bool {
	if s.GetConst() != nil || enumWritten(s) {
		return false
	}
	if len(s.GetAllOf()) > 0 {
		return true
	}
	types := effectiveTypes(s)
	if len(types) == 1 && types[0] == oas3.SchemaTypeObject {
		return true
	}
	props := s.GetProperties()
	return len(types) == 0 && props != nil && props.Len() > 0
}

// unionBranches returns the branches of whichever combinator the schema
// declares, the keyword's name (for pointers), and whether it is exclusive.
//
// oneOf wins when both are written, so every caller owns the set it passed over:
// buildUnion keeps it on the Union (preserveUnusedCombinator), nullUnionCollapse
// declines to collapse past it, and the verbatim lowering keeps both keywords
// (preserveUnionSiblings). Reading the preference as one only that last lowering
// could reach is what dropped the anyOf in silence (GitHub #35).
func unionBranches(s *oas3.Schema) ([]*oas3.JSONSchema[oas3.Referenceable], string, bool) {
	if branches := s.GetOneOf(); len(branches) > 0 {
		return branches, "oneOf", true
	}
	return s.GetAnyOf(), "anyOf", false
}

// unionBranchesDeclareShape reports whether any oneOf/anyOf branch contributes
// data shape. When none does — `oneOf: [{required: [a]}, {required: [b]}]`, the
// commonest instance — the union narrows what the sibling body accepts without
// changing it, which is dependentRequired's job description: validation logic,
// not shape (ir-design §4.7).
func unionBranchesDeclareShape(s *oas3.Schema) bool {
	return slices.ContainsFunc(s.GetOneOf(), branchDeclaresShape) ||
		slices.ContainsFunc(s.GetAnyOf(), branchDeclaresShape)
}

// branchDeclaresShape reports whether one union branch declares data shape. A
// $ref always does; a bare `type: null` never does, since it contributes only
// the enclosing reference's Nullable bit.
func branchDeclaresShape(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	if isRefBranch(b) {
		return true
	}
	s := b.GetSchema()
	if s == nil {
		return false // a boolean branch declares no shape of its own
	}
	return declaresShape(s) || len(s.GetOneOf())+len(s.GetAnyOf()) > 0
}

// oneOfAnyOfHasNull reports whether any oneOf/anyOf branch is a bare `type: null`
// schema, so its nullability lifts onto the enclosing union ref rather than
// degrading to an `any` variant.
func oneOfAnyOfHasNull(s *oas3.Schema) bool {
	if slices.ContainsFunc(s.GetOneOf(), isNullSchema) {
		return true
	}
	return slices.ContainsFunc(s.GetAnyOf(), isNullSchema)
}

// variantTypeFunc lowers one union branch to the type its variant refers to. It
// is a parameter so the distributed lowering can conjoin the sibling composition
// into each branch while sharing every other rule about union shape — null-branch
// stripping, variant hints, exclusivity, pointer derivation.
type variantTypeFunc func(b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string) ir.TypeRef

// buildUnion assembles the Union node for a oneOf/anyOf schema, attaching a
// discriminator when one is declared. common is already built by the caller
// (internNode), so buildUnion needs no hint of its own to build one.
func buildUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer string, variantType variantTypeFunc) (ir.TypeDef, []ir.Diagnostic) {
	branches, key, exclusive := unionBranches(s)
	diags := preserveUnusedCombinator(c, &common.Unmodeled, s, key, pointer)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if isNullSchema(b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vh := branchHint(b, i)
		vptr := pointer + ids.Ptr(key, strconv.Itoa(i))
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(vh),
			Type: variantType(b, vptr, vh),
		})
	}
	u := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		WireTagged: false,
	}
	disc, discDiags := lowerDiscriminator(c, ts, s, nil, pointer)
	u.Discriminator = disc
	return u, append(diags, discDiags...)
}

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over
// verbatim on the Union the position lowers to, and reports it. A schema
// declaring only one combinator passes over nothing and is left alone.
//
// A schema writing both conjoins them — an instance must satisfy the oneOf *and*
// the anyOf — and one ir.Union carries one branch set, so the loser had no place
// in the node and was dropped in silence. It is the union half of the same §4.8
// rule recordSkippedFamilies applies to the keyword families: the position keeps
// lowering to the branch set unionBranches elects, because that Union is a shape
// the IR can express and discarding it too would model nothing at all, and the
// set it did not elect stays recoverable beside it.
//
// Where a *structural* sibling is written as well, classifyUnionSiblings reaches
// unionBothCombinators first and neither set is elected — there the sibling body
// is the most the IR can express, and distributing either union across it would
// drop the other (lowerBesideUnmodeledUnion).
func preserveUnusedCombinator(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, won, pointer string) []ir.Diagnostic {
	if len(s.GetOneOf()) == 0 || len(s.GetAnyOf()) == 0 {
		return nil
	}
	unused := otherCombinator[won]
	kept, diags := PreserveSchemaKeyword(c, p, s, unused, ir.ReasonDegradedLowering, pointer+ids.Ptr(unused))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"oneOf and anyOf are both declared here and conjoin, and one Union carries one "+
			"branch set; this position lowered as its %s, with %s kept verbatim under Unmodeled",
		won, unused))
}

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across its variants: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (ir-design §4.3) alongside its branch, so the composition is
// carried on every variant rather than merged into one or dropped.
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, pointer: pointer, hint: hint, id: common.ID}
		def, unionDiags := buildUnion(c, ts, s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string) ir.TypeRef {
				ref, variantDiags := composedVariant(c, ts, anchors, depth, body, b, vptr, vhint)
				diags = append(diags, variantDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	return id, append(diags, c.DiagAt(ir.SeverityInfo, diag.CompositionLowering, pointer,
		"oneOf/anyOf co-declared with structural keywords; the composition is distributed across the union variants"))
}

// composedBody is the schema every distributed variant carries: the enclosing
// schema, where it is written, and the identity of the Union it lowered to —
// which is the node a base's discriminator mapping names, since the source
// declares one schema, not one per branch.
type composedBody struct {
	schema  *oas3.Schema
	pointer string
	hint    string
	id      ir.TypeID
}

// composedVariant synthesizes the Model for one distributed variant: the
// enclosing schema's composition and own properties conjoined with that
// branch's referent. The branch is conjoined last so the enclosing composition
// classifies first, keeping ir-design §4.3's "sole $ref becomes Base" reading of
// the allOf the source actually wrote.
//
// The Model gets an ID of its own (ids.ComposedType) rather than the branch
// pointer's, because the branch pointer already denotes the branch schema.
// Interning the variant there made the two race for one pointer: whichever
// lowered first won it, so a $ref to `…/oneOf/N` anywhere in the document could
// leave that variant as a bare alias of the branch while its siblings carried
// the body — order-dependent, and exactly the disagreement §4.3 forbids.
func composedVariant(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, body composedBody,
	b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string,
) (ir.TypeRef, []ir.Diagnostic) {
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(c, ts, anchors, depth, b, vptr, vhint)
	id := ids.ComposedType(vptr)
	common := commonFor(c, id, vptr, compile.SubHint(body.hint, vhint))
	def, variantDiags := buildComposedVariant(c, ts, anchors, depth, body, branch.Target, common)
	ts.Register(id, def)
	return ir.TypeRef{Target: id, Nullable: composedVariantNullable(body.schema, branch)},
		append(diags, variantDiags...)
}

// composedVariantNullable reports whether the union variant naming a
// synthesized variant admits null: the variant is the enclosing body conjoined
// with the branch, so it admits null exactly when the branch does and the body
// does not forbid it. That is schemaNullVerdict's allOf rule applied to the one
// conjunction the source does not spell as an allOf, which is what keeps a
// distributed union answering what the plain union over the same branch does.
//
// The bit belongs on this TypeRef rather than on the variant model's Base or
// Mixins for the reason conjoinBranch records: those name a conjunct, and
// nullability is a property of the usage that names the conjunction.
func composedVariantNullable(body *oas3.Schema, branch ir.TypeRef) bool {
	if !branch.Nullable {
		return false
	}
	budget := maxNullConjuncts
	return schemaNullVerdict(body, &budget) != nullForbidden
}

// buildComposedVariant assembles the variant Model itself. Every fill reads the
// enclosing schema at the enclosing pointer, so the properties, their PropIDs
// and any shared additionalProperties node are the single set the source
// declared, named after the enclosing schema rather than after whichever branch
// happened to build them first.
func buildComposedVariant(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, body composedBody, branch ir.TypeID, common ir.TypeCommon) (ir.TypeDef, []ir.Diagnostic) {
	m := &ir.Model{TypeCommon: common}
	diags := fillAllOf(c, ts, anchors, depth, m, body.schema, body.pointer)
	diags = append(diags, fillModelProperties(c, ts, anchors, depth, m, body.schema, body.pointer)...)
	diags = append(diags, applyCompositionRequired(c, m, body.schema, body.pointer)...)
	diags = append(diags, fillAdditional(c, ts, anchors, depth, m, body.schema, body.pointer, body.hint)...)
	conjoinBranch(m, branch)
	// The tag is the enclosing schema's: it is what a base's mapping names, and
	// the variants are its lowering. No discriminator of its own can be declared
	// here — a schema that declares one is never distributed.
	m.DiscriminatorValue = subtypeDiscriminatorValue(c, ts, body.schema, body.id, body.pointer)
	return m, diags
}

// conjoinBranch adds one union branch's referent to the variant model,
// classified per ir-design §4.3: Base when the enclosing schema composed
// nothing, a Mixin otherwise. Only the target is carried, never the branch
// ref's Nullable bit — fillAllOf drops it for an allOf entry on the same
// reasoning: Base and Mixins name a conjunct, and nullability is a property of
// the usage that names the conjunction, not of one side of it. The usage here is
// the union variant, and composedVariantNullable is what puts the bit on it.
func conjoinBranch(m *ir.Model, branch ir.TypeID) {
	target := ir.TypeRef{Target: branch}
	if m.Base == nil && len(m.Mixins) == 0 {
		m.Base = &target
		return
	}
	m.Mixins = append(m.Mixins, target)
}

// branchHint derives a composition branch's naming hint from its $ref target's
// name, falling back to a positional hint for inline branches. It names both a
// union variant and an allOf branch, which reach it as the same question.
//
// A $ref branch's answer is also hoistSubSchema's (subSchemaHint), and must
// stay so: an outside $ref can name the branch pointer, and the node it owns
// would otherwise be hinted by whichever of the two lowerings arrived first.
func branchHint(b *oas3.JSONSchema[oas3.Referenceable], i int) string {
	// Only a true reference carries a target name: IsReference() is precisely
	// GetSchema().Ref != "" for a non-bool branch, so a schema whose Ref pointer is
	// set but empty (IsReference() false) has no usable last segment.
	if b != nil && b.IsReference() {
		if name := refLastSegment(b.GetRef().String()); name != "" {
			return name
		}
	}
	return positionalBranchHint(strconv.Itoa(i))
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// branchPointerHint returns the hint the branch at pointer takes, for a caller
// holding only the pointer.
//
// It exists so hoistSubSchema answers what the composition would have
// (GitHub #181). An outside $ref can name a branch's pointer, and only the first
// lowering to arrive interns the node, so a hint derived differently there makes
// the document depend on declaration order — silently, since either spelling is a
// valid hint and nothing compares them. The $ref-branch half of that already
// agrees; this is the inline half, where the composition knows the branch's
// ordinal and a bare pointer walk knew only the last segment, which is the
// ordinal with nothing to say it is one.
func branchPointerHint(pointer string) (string, bool) {
	segments := strings.Split(pointer, "/")
	if len(segments) < 2 {
		return "", false
	}
	keyword, index := segments[len(segments)-2], segments[len(segments)-1]
	if !compositionKeywords[keyword] || !isDecimalIndex(index) {
		return "", false
	}
	return positionalBranchHint(index), true
}

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a composition branch's pointer segment takes.
func isDecimalIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// refLastSegment returns the final path segment of a $ref string.
func refLastSegment(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// lowerDiscriminator lowers a schema's discriminator. Each mapping entry
// resolves to a target TypeID (a bare name implies
// #/components/schemas/<name>; nil stays nil, preserving infer-by-name
// semantics), and the tag is located via exactly one of Property /
// PropertyName / Index (ir-design §4.3). m is the allOf base model this
// discriminator is declared on, or nil for a oneOf/anyOf union, which has no
// single model and so is named only by PropertyName; otherwise the tag
// resolves to the declaring property's PropID, falling back to PropertyName
// if undeclared.
func lowerDiscriminator(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, m *ir.Model, pointer string) (*ir.Discriminator, []ir.Diagnostic) {
	d := s.GetDiscriminator()
	if d == nil {
		return nil, nil
	}
	mapping, diags := discriminatorMapping(c, ts, d, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	defaultID, defaultDiags := discriminatorDefault(c, ts, d, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order. An entry that does not resolve to an interned
// schema yields one error diagnostic and is dropped — never a synthesized ID
// that nothing backs (issue #14). An all-dropped mapping collapses to nil,
// preserving infer-by-name semantics and a clean round-trip.
func discriminatorMapping(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, pointer string) (map[string]ir.TypeID, []ir.Diagnostic) {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make(map[string]ir.TypeID, m.Len())
	for tag, target := range m.All() {
		id, ok := mappingTargetID(c, ts, target)
		if !ok {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
				pointer+ids.Ptr("discriminator", "mapping", tag),
				"discriminator mapping %q references unresolved schema %q", tag, target))
			continue
		}
		out[tag] = id
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// discriminatorDefault resolves an OpenAPI 3.2 defaultMapping to its target ID,
// dropping it with one diagnostic when it does not resolve to an interned schema.
func discriminatorDefault(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, pointer string) (ir.TypeID, []ir.Diagnostic) {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return "", nil
	}
	id, ok := mappingTargetID(c, ts, dm)
	if !ok {
		return "", []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr("discriminator", "defaultMapping"),
			"discriminator defaultMapping references unresolved schema %q", dm)}
	}
	return id, nil
}

// mappingTargetID resolves a discriminator mapping target — a bare schema
// name or a $ref string — to the stable TypeID of an interned schema. A bare
// name (even one containing '/') that names a declared component resolves to
// it via ids.ForPointer regardless of source order, since every declared
// name is recorded before lowering begins — this also makes a degenerate
// empty-named component resolve to its real (anonymous) ID rather than an
// unbacked ids.NamedType. Otherwise the target must be a same-file $ref to a
// declared component or an already-interned node; a target that resolves to
// neither yields ok=false, since unlike a schema position, a discriminator
// subtype cannot be hoisted from a bare pointer — the caller drops and
// diagnoses it.
func mappingTargetID(c lowering.Ctx, ts *compile.Types, target string) (ir.TypeID, bool) {
	if c.DeclaresSchema(target) {
		return ids.ForPointer(ids.Ptr("components", "schemas", target)), true
	}
	pointer, ok := c.RefScope().InternalPointer(target)
	if !ok {
		return "", false
	}
	if id, resolved, handled := c.RefScope().ComponentRef(pointer); handled {
		return id, resolved
	}
	return resolve.InternedID(ts, pointer)
}

// propIDByName returns the PropID of the model property with the given source
// name, if the model declares it. m is nil-safe: lowerDiscriminator's
// oneOf/anyOf union case has no model to search, only a tag name.
func propIDByName(m *ir.Model, name string) (ir.PropID, bool) {
	if m == nil {
		return "", false
	}
	for i := range m.Properties {
		if m.Properties[i].Name.Source == name {
			return m.Properties[i].ID, true
		}
	}
	return "", false
}

// lowerEnum hoists a schema with `enum` as a closed Enum. A heterogeneous or
// non-scalar member set has no Enum home, so it falls back to a Union of
// Literals with an info diagnostic — nothing is dropped. An empty member list
// takes neither path (emptyEnum).
//
// A member set past the caller's budget is the one case where something is: the
// enum lowers as the top type with an error diagnostic naming the budget. The
// count is checked before either branch below, because both are linear in it —
// the Enum mints an EnumMember with a canonical word sequence per member, and
// the fallback a hoisted Literal type and a union variant per member — and it is
// that per-member cost, not the source's size, that GitHub #75 measured
// amplifying 10.9 MB of one enum into 2.6 GB of peak RSS.
//
// A schema that admits null spells its nullable enum by listing `null` among the
// members, so that member is stripped and normalized onto the enclosing
// reference's Nullable bit (ir-design §3.3) rather than degrading the whole
// enum. schemaAdmitsNull is what decides that here *and* what a reference
// re-derives the bit from (refNullable at a $ref site, lowerSchemaBody inline),
// so the null this drops is exactly the null those put back; a spelling only one
// of them recognized would lose it.
//
// A non-empty enum decides null admission by itself, so a bare
// `{enum: [red, green, null]}` normalizes like the type-array spelling of the
// same set. `{type: string, enum: [red, green, null]}` still does not: the type
// keyword conjoins with the members and forbids the null they list, so
// stripping there would widen the declared type rather than normalize it.
//
// A member set with no Enum home is unaffected by the stripping either way. It
// falls back to a Union over the members as written, null included, beside a
// reference that also says the position admits null — one fact stated twice,
// which is what the type-array spelling of such a set already produced and what
// the bare spelling now matches.
func lowerEnum(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(s.GetEnum()) == 0 {
			def, emptyDiags := emptyEnum(c, s, common, pointer)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(s.GetEnum()); c.Limits.EnumMembersExceeded(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, pointer,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers))
			return &ir.Any{TypeCommon: common}
		}
		members, memberPrim, ok := enumMembers(s.GetEnum(), schemaAdmitsNull(s))
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, s, common, pointer, hint)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
	return id, diags
}

// emptyEnum lowers `enum: []` — a value space fixed to the empty set, so the
// position accepts no instance — as the closed Enum over no member, with the one
// warning that says so.
//
// This is the IR's exact spelling of an empty value space, not an approximation
// of one. There is no bottom TypeKind to reach for, but a closed Enum admits its
// members and nothing else, so a closed Enum with none admits nothing. The
// neighbouring construct settles for less: a boolean `false` schema also matches
// nothing and lowers to a closed empty Model (falseSchema), which still admits
// the empty object.
//
// It deliberately does not reach enumAsUnion. That fallback mints one variant
// per member, so an empty member list would produce a Union with no variants —
// a node nothing in the IR rejects and no reader can act on, which is a quieter
// version of the same defect rather than a fix for it (GitHub #318).
//
// ValueType is the declared scalar type where one is written and the top type
// otherwise: with no member to classify, nothing narrower is known, and nothing
// narrower is needed either — the member list is what holds the values, and it
// is empty whatever this says.
//
// What the position lowers to is settled here; whether a *reference* to it
// admits null is not. That bit is computed by schemaAdmitsNull at each use site,
// which reads the type keyword and not the value set, so
// `{type: [T, "null"], enum: []}` still reads as nullable at its uses — a
// nullable type array beside an enum listing no null member, which is GitHub
// #288's shape exactly and is settled there rather than here.
func emptyEnum(c lowering.Ctx, s *oas3.Schema, common ir.TypeCommon, pointer string) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, pointer,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  enumValueType(s, ir.PrimAny),
		Closed:     true,
	}, diags
}

// enumMembers converts enum nodes into scalar members, reporting ok=false on any
// of three: a member that is non-scalar, members heterogeneous in kind, or a set
// that keeps no member at all.
//
// dropNull skips `null` members instead of refusing them, for a schema whose
// nullability the enclosing reference already carries (see lowerEnum). Kind
// agreement is read off the members actually kept, so a leading `null` fixes
// nothing: `enum: [null, red, green]` is the same string enum as
// `enum: [red, green, null]`.
//
// The third condition is the one an all-null set meets, and it is why such a set
// degrades rather than becoming a memberless Enum. The returned PrimKind is the
// one every kept member's kind maps to — meaningful only when ok, and since ok
// requires a kept member, never the zero PrimKind there.
func enumMembers(nodes []values.Value, dropNull bool) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	var kind ir.ValueKind
	var prim ir.PrimKind
	for _, node := range nodes {
		val, err := value.FromNode(node)
		if err != nil {
			return nil, "", false
		}
		if dropNull && val.Kind == ir.ValueNull {
			continue
		}
		memberPrim, text, admissible := enumMemberForm(val)
		if !admissible {
			return nil, "", false
		}
		if len(members) == 0 {
			kind, prim = val.Kind, memberPrim
		} else if val.Kind != kind {
			return nil, "", false
		}
		members = append(members, ir.EnumMember{
			Name:  compile.NamingFor(text),
			Value: val,
		})
	}
	if len(members) == 0 {
		return nil, "", false
	}
	return members, prim, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer, hint string) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	nodes := s.GetEnum()
	variants := make([]ir.Variant, 0, len(nodes))
	for i, node := range nodes {
		vh := compile.SubHint(hint, strconv.Itoa(i))
		lptr := pointer + ids.Ptr("enum", strconv.Itoa(i))
		litID, litDiags := hoistLiteral(c, ts, node, lptr, vh)
		diags = append(diags, litDiags...)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(vh),
			Type: ir.TypeRef{Target: litID},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  true,
	}, diags
}

// hoistLiteral hoists a single node as a Literal type at its own pointer, or
// falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's ids.ForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
//
// It returns the interned ID, plus the diagnostic an unconvertible node
// produces — none when the node converts.
func hoistLiteral(c lowering.Ctx, ts *compile.Types, node values.Value, pointer, hint string,
) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error()))
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val}
	})
	return id, diags
}

// enumValueType picks an Enum's ValueType from the schema's declared scalar
// type, falling back to the primitive its members classified to. It no longer
// re-derives that primitive from a ValueKind: enumMemberForm decided it once,
// for the same members, so there is nothing here to disagree with.
func enumValueType(s *oas3.Schema, memberPrim ir.PrimKind) ir.PrimKind {
	if types := effectiveTypes(s); len(types) == 1 {
		switch types[0] {
		case oas3.SchemaTypeString:
			return ir.PrimString
		case oas3.SchemaTypeInteger:
			return ir.PrimInteger
		case oas3.SchemaTypeNumber:
			return ir.PrimNumber
		case oas3.SchemaTypeBoolean:
			return ir.PrimBool
		}
	}
	return memberPrim
}

// enumMemberForm classifies one lowered value as an Enum member: the PrimKind an
// Enum over members of that kind declares, and the literal text the member takes
// as its source name. ok=false means the kind has no place in an Enum at all, and
// the caller degrades the whole enum to a Union of Literals with a diagnostic —
// nothing is dropped and nothing is guessed.
//
// This is the compiler's single switch over ir.ValueKind. It replaced three that
// each fell through to a guess, so a kind none of them named was described as a
// string by one, given no text by another, and admitted by the third. Every kind
// ir declares is named in exactly one arm here; the default is unreachable by
// construction, and TestEnumMemberForm_NamesEveryValueKind derives the sealed set
// from the ir sources so a kind added there without an arm reddens rather than
// being reclassified in silence.
//
// The default arm is the conservative half — refusing a kind degrades an enum
// with a diagnostic, where admitting one would assert a type the source never
// wrote.
func enumMemberForm(v ir.Value) (ir.PrimKind, string, bool) {
	switch v.Kind {
	case ir.ValueString:
		return ir.PrimString, v.Str, true
	case ir.ValueSymbol:
		// An interned symbol has no primitive of its own; PrimString is the form
		// it takes on a JSON wire. Unreachable from OpenAPI, whose values come
		// from YAML nodes, but ValueKind is IR-wide and this is its enum home.
		return ir.PrimString, v.Str, true
	case ir.ValueNumber:
		return ir.PrimNumber, string(v.Num), true
	case ir.ValueBool:
		return ir.PrimBool, strconv.FormatBool(v.Bool), true
	case ir.ValueBytes:
		return ir.PrimBytes, base64.StdEncoding.EncodeToString(v.Bytes), true
	case ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		// Composite, reference and null values are not enum members: none has a
		// primitive to declare or a literal text form to name a member by.
		return "", "", false
	default:
		return "", "", false
	}
}
