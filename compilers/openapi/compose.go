package openapi

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// lowerAllOf hoists an allOf schema as a Model, classifying each branch per
// ir-design §4.3: the sole $ref (or a $ref whose target anchors a discriminator
// hierarchy) becomes Base; other $refs become Mixins in source order; inline
// branches contribute their properties, each carrying provenance into the
// allOf branch it came from.
func (l *lowerer) lowerAllOf(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		m := &ir.Model{TypeCommon: common}
		l.fillAllOf(m, s, pointer)
		l.fillModelProperties(m, s, pointer) // properties declared alongside allOf
		l.applyCompositionRequired(m, s, pointer)
		l.fillAdditional(m, s, pointer, hint)
		l.applyFalseBranches(m, s, pointer) // after fillAdditional: it closes m
		if d := l.lowerDiscriminator(s, m, pointer); d != nil {
			m.Discriminator = d
		}
		m.DiscriminatorValue = l.subtypeDiscriminatorValue(s, common.ID, pointer)
		return m
	})
}

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
		bptr := pointer + ptr("allOf", strconv.Itoa(i))
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
func (l *lowerer) applyCompositionRequired(m *ir.Model, s *oas3.Schema, pointer string) {
	entries := compositionRequired(s, pointer)
	if len(entries) == 0 {
		return
	}
	byWire := wireNameIndex(m.Properties)
	for _, e := range entries {
		if i, ok := byWire[e.name]; ok {
			m.Properties[i].Required = true
			continue
		}
		l.diagUnattachableRequired(m, e)
	}
}

// diagUnattachableRequired reports a composition-scope required name matching
// no own property. It is a warning when m composes a ref (Base or a Mixin):
// the requirement plausibly belongs to that base/mixin's own property, so real
// fidelity is lost. Otherwise it is info: the spec just names a property
// nothing declares, which is legal JSON Schema with nothing to lose.
func (l *lowerer) diagUnattachableRequired(m *ir.Model, e requiredEntry) {
	if m.Base != nil || len(m.Mixins) > 0 {
		l.diag(ir.SeverityWarning, codeUnattachableRequired, e.pointer,
			"required %q matches no property declared here; a base or mixin may declare it, and the requirement cannot be carried across composition", e.name)
		return
	}
	l.diag(ir.SeverityInfo, codeUnattachableRequired, e.pointer,
		"required %q matches no property declared here", e.name)
}

// fillAllOf classifies and lowers the allOf branches into m.
//
// An inline branch is merged in place rather than lowered through schemaRef, so
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
// The merge itself is left as it is: merging a branch's own docs, constraints or
// openness upward onto m would need a precedence rule for branches that disagree,
// and some of it has no home to merge into at all — Model.Constraints bounds the
// property set's cardinality, so a scalar branch's maxLength cannot go there.
// Verbatim beside the model needs neither, and keeps the branch recoverable.
func (l *lowerer) fillAllOf(m *ir.Model, s *oas3.Schema, pointer string) {
	branches := s.GetAllOf()
	baseIdx := l.selectAllOfBase(branches)
	for i, b := range branches {
		bptr := pointer + ptr("allOf", strconv.Itoa(i))
		if !isRefBranch(b) {
			l.fillModelProperties(m, b.GetSchema(), bptr)
			l.preserveUnmergedBranch(m, b.GetSchema(), i, bptr)
			continue
		}
		id, ok := l.resolveSchemaRef(b, b.GetRef().String())
		if !ok {
			l.diag(ir.SeverityError, codeUnresolvedRef, bptr,
				"unresolved allOf $ref %q", b.GetRef().String())
			continue
		}
		ref := l.homeDeclaration(b.GetSchema(), ir.TypeRef{Target: id}, bptr, branchHint(b, i), homeOwnNode)
		if i == baseIdx {
			m.Base = &ref
		} else {
			m.Mixins = append(m.Mixins, ref)
		}
	}
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
func (l *lowerer) applyFalseBranches(m *ir.Model, s *oas3.Schema, pointer string) {
	for i, b := range s.GetAllOf() {
		if b == nil || !b.IsBool() {
			continue
		}
		if v := b.GetBool(); v == nil || *v {
			continue // `true` constrains nothing.
		}
		bptr := pointer + ptr("allOf", strconv.Itoa(i))
		m.Additional = ir.AdditionalClosed
		l.preserve(&m.Unmodeled, "openapi:allOf/"+strconv.Itoa(i),
			ir.RawValue("false"), ir.ReasonDegradedLowering, bptr)
		l.diag(ir.SeverityInfo, codeFalseSchema, bptr,
			"boolean false allOf branch matches nothing, so the composition matches nothing; "+
				"composed model closed and the branch kept verbatim under Unmodeled")
	}
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
func (l *lowerer) preserveUnmergedBranch(m *ir.Model, bs *oas3.Schema, branchIdx int, bptr string) {
	if bs == nil {
		return // boolean branch; applyFalseBranches handles it.
	}
	residue := unmergedBranchKeys(bs)
	if len(residue) == 0 {
		return
	}
	l.preserve(&m.Unmodeled, "openapi:allOf/"+strconv.Itoa(branchIdx),
		nodeToRaw(bs.GetRootNode()), ir.ReasonDegradedLowering, bptr)
	l.diagUnmergedBranch(bs, residue, bptr)
}

// diagUnmergedBranch announces one merged branch's residue, naming the keywords
// so two branches of the same schema are told apart.
//
// A branch declaring a type that cannot be an object is a warning rather than
// info: the composed node is an ir.Model, which asserts the value *is* an object,
// so the IR states something the branch contradicts — `allOf: [{type: string,
// maxLength: 3}]` lowers to an empty model. Any other residue leaves a model the
// IR describes correctly and only narrows it further, which is §4.8's
// under-constrained case.
func (l *lowerer) diagUnmergedBranch(bs *oas3.Schema, residue []string, bptr string) {
	kept := strings.Join(residue, ", ")
	if branchExcludesObject(bs) {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, bptr,
			"inline allOf branch declares a type that is not an object, which the composed model asserts it is; the branch (%s) is kept verbatim under Unmodeled", kept)
		return
	}
	l.diag(ir.SeverityInfo, codeDegradedConstruct, bptr,
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
// order, or nil when the node is no mapping. It is rawChildNode's enumerating
// counterpart: that answers "what is written at this key", this one "which keys
// are written".
//
// Effective, not literal: a branch spelled `*anchor` writes the anchored
// mapping's keys and one spelled `<<: *anchor` writes the merged-in keys, so
// deriving a residue from the literal text would report `<<` — or, for a whole
// branch replaced by an alias, no keys at all, which reads as "the merge consumed
// everything" and drops the branch silently. nodeView is the package's statement
// of how speakeasy reads a mapping, and bounds the expansion (maxMergeDepth).
func rawMappingKeys(root *yaml.Node) []string {
	mapping := rawMapping(root)
	if mapping == nil {
		return nil
	}
	pairs := newNodeView().mappingPairs(mapping)
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.key)
	}
	return keys
}

// rawMapping returns the mapping node root stands for: a document wrapper is
// unwrapped and an alias followed to its anchor (bounded by deref). Anything
// that is not a mapping after that — a scalar, a sequence, nil — yields nil, so
// a caller needs no kind check of its own.
func rawMapping(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	root = deref(root)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// selectAllOfBase returns the branch index that becomes Model.Base, or -1 when
// none qualifies (multiple non-hierarchy refs stay Mixins).
func (l *lowerer) selectAllOfBase(branches []*oas3.JSONSchema[oas3.Referenceable]) int {
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
	resolved := b.GetResolvedSchema()
	if resolved == nil {
		return false
	}
	s := resolved.GetSchema()
	return s != nil && s.GetDiscriminator() != nil
}

// subtypeDiscriminatorValue returns the wire tag value this allOf subtype
// carries within its base's discriminator hierarchy, or "" when no allOf base
// anchors one. Per ir-design §4.3 the value is the base mapping key that points
// at this subtype, falling back to the subtype's own schema name (OpenAPI's
// implicit mapping) when the mapping omits it.
func (l *lowerer) subtypeDiscriminatorValue(s *oas3.Schema, id ir.TypeID, pointer string) string {
	d := baseBranchDiscriminator(s.GetAllOf())
	if d == nil {
		return ""
	}
	if m := d.GetMapping(); m != nil {
		for value, target := range m.All() {
			if tid, ok := l.mappingTargetID(target); ok && tid == id {
				return value
			}
		}
	}
	return refLastSegment(pointer)
}

// baseBranchDiscriminator returns the discriminator declared on the resolved
// target of the allOf base branch (the $ref anchoring the hierarchy), or nil
// when no ref branch carries one.
func baseBranchDiscriminator(branches []*oas3.JSONSchema[oas3.Referenceable]) *oas3.Discriminator {
	for _, b := range branches {
		if !isRefBranch(b) {
			continue
		}
		resolved := b.GetResolvedSchema()
		if resolved == nil {
			continue
		}
		rs := resolved.GetSchema()
		if rs == nil {
			continue
		}
		if d := rs.GetDiscriminator(); d != nil {
			return d
		}
	}
	return nil
}

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A two-variant {X, null} set
// collapses to nullable X (ir-design §3.3); everything else becomes a Union
// with one Variant per branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields.
func (l *lowerer) lowerOneOfAnyOf(s *oas3.Schema, pointer, hint string) ir.TypeRef {
	if inner, ip, ih, ok := nullUnionCollapse(s, pointer, hint); ok {
		ref := l.schemaRef(inner, ip, ih)
		ref.Nullable = true
		return ref
	}
	tid := l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		return l.buildUnion(s, common, pointer, l.schemaRef)
	})
	return ir.TypeRef{Target: tid, Nullable: schemaAdmitsNull(s)}
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
func (l *lowerer) classifyUnionSiblings(s *oas3.Schema) unionLowering {
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
	if !l.branchesNameReferents(s) {
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
func (l *lowerer) branchesNameReferents(s *oas3.Schema) bool {
	branches, _, _ := unionBranches(s)
	for _, b := range branches {
		if !l.refNamesReferent(b, b.GetRef().String()) {
			return false
		}
	}
	return true
}

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings.
func (l *lowerer) lowerCoDeclaredUnion(s *oas3.Schema, pointer, hint string) ir.TypeID {
	switch l.classifyUnionSiblings(s) {
	case unionDistributed:
		return l.lowerDistributedUnion(s, pointer, hint)
	case unionValidationOnly:
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonValidationOnly, "")
	case unionUncomposableBody:
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonDegradedLowering,
			"the body is not a model, so it carries no composition to distribute into")
	case unionBothCombinators:
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonDegradedLowering,
			"oneOf and anyOf are both declared, so distributing either would drop the other")
	case unionInlineBranch:
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonDegradedLowering,
			"a branch is written inline, so it names no referent to conjoin the body with")
	case unionUnresolvedBranch:
		l.diagUnresolvedBranches(s, pointer)
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
	default: // unionDiscriminated
		return l.lowerBesideUnmodeledUnion(s, pointer, hint, ir.ReasonDegradedLowering,
			"a declared discriminator binds the branches by name, which distributing them would break")
	}
}

// diagUnresolvedBranches reports each union branch whose $ref names nothing this
// compilation resolves. The branch text survives verbatim under Unmodeled, so
// nothing is dropped — but a reference that resolves nowhere is a defect of the
// document itself, reported at the same severity as everywhere else, and the
// info diagnostic beside it explains only the lowering.
func (l *lowerer) diagUnresolvedBranches(s *oas3.Schema, pointer string) {
	branches, key, _ := unionBranches(s)
	for i, b := range branches {
		ref := b.GetRef().String()
		if l.refNamesReferent(b, ref) {
			continue
		}
		l.diag(ir.SeverityError, codeUnresolvedRef, pointer+ptr(key, strconv.Itoa(i)),
			"union branch $ref %q resolves to nothing this document declares; the branch is kept verbatim", ref)
	}
}

// composesAsModel reports whether the structural siblings lower to a Model,
// which is the only node with composition fields for a union to distribute
// across. It mirrors lower's dispatch: const and enum win over everything, then
// allOf, then a declared object type or a bare property set.
func composesAsModel(s *oas3.Schema) bool {
	if s.GetConst() != nil || len(s.GetEnum()) > 0 {
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
// oneOf wins when both are present; only the verbatim lowering ever sees that
// shape, and it keeps both keywords.
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
func (l *lowerer) buildUnion(s *oas3.Schema, common ir.TypeCommon, pointer string, variantType variantTypeFunc) ir.TypeDef {
	branches, key, exclusive := unionBranches(s)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if isNullSchema(b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vh := branchHint(b, i)
		vptr := pointer + ptr(key, strconv.Itoa(i))
		variants = append(variants, ir.Variant{
			Name: ir.Naming{Hint: vh},
			Type: variantType(b, vptr, vh),
		})
	}
	u := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		WireTagged: false,
	}
	u.Discriminator = l.lowerDiscriminator(s, nil, pointer)
	return u
}

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across its variants: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (ir-design §4.3) alongside its branch, so the composition is
// carried on every variant rather than merged into one or dropped.
func (l *lowerer) lowerDistributedUnion(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, pointer: pointer, hint: hint, id: common.ID}
		return l.buildUnion(s, common, pointer,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string) ir.TypeRef {
				return l.composedVariant(body, b, vptr, vhint)
			})
	})
	l.diag(ir.SeverityInfo, codeCompositionLowering, pointer,
		"oneOf/anyOf co-declared with structural keywords; the composition is distributed across the union variants")
	return id
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
// The Model gets an ID of its own (composedTypeID) rather than the branch
// pointer's, because the branch pointer already denotes the branch schema.
// Interning the variant there made the two race for one pointer: whichever
// lowered first won it, so a $ref to `…/oneOf/N` anywhere in the document could
// leave that variant as a bare alias of the branch while its siblings carried
// the body — order-dependent, and exactly the disagreement §4.3 forbids.
func (l *lowerer) composedVariant(body composedBody,
	b *oas3.JSONSchema[oas3.Referenceable], vptr, vhint string) ir.TypeRef {
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch := l.schemaRef(b, vptr, vhint)
	id := composedTypeID(vptr)
	common := l.commonFor(id, vptr, body.hint+"_"+vhint)
	l.types.Register(id, l.buildComposedVariant(body, branch.Target, common))
	return ir.TypeRef{Target: id}
}

// buildComposedVariant assembles the variant Model itself. Every fill reads the
// enclosing schema at the enclosing pointer, so the properties, their PropIDs
// and any shared additionalProperties node are the single set the source
// declared, named after the enclosing schema rather than after whichever branch
// happened to build them first.
func (l *lowerer) buildComposedVariant(body composedBody, branch ir.TypeID, common ir.TypeCommon) ir.TypeDef {
	m := &ir.Model{TypeCommon: common}
	l.fillAllOf(m, body.schema, body.pointer)
	l.fillModelProperties(m, body.schema, body.pointer)
	l.applyCompositionRequired(m, body.schema, body.pointer)
	l.fillAdditional(m, body.schema, body.pointer, body.hint)
	conjoinBranch(m, branch)
	// The tag is the enclosing schema's: it is what a base's mapping names, and
	// the variants are its lowering. No discriminator of its own can be declared
	// here — a schema that declares one is never distributed.
	m.DiscriminatorValue = l.subtypeDiscriminatorValue(body.schema, body.id, body.pointer)
	return m
}

// conjoinBranch adds one union branch's referent to the variant model,
// classified per ir-design §4.3: Base when the enclosing schema composed
// nothing, a Mixin otherwise. Only the target is carried, never the branch
// ref's Nullable bit — fillAllOf drops it for an allOf entry on the same
// reasoning: Base and Mixins name a conjunct, and nullability is a property of
// the usage that names the conjunction, not of one side of it.
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
	return "variant_" + strconv.Itoa(i)
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
func (l *lowerer) lowerDiscriminator(s *oas3.Schema, m *ir.Model, pointer string) *ir.Discriminator {
	d := s.GetDiscriminator()
	if d == nil {
		return nil
	}
	disc := &ir.Discriminator{Mapping: l.discriminatorMapping(d, pointer)}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	disc.Default = l.discriminatorDefault(d, pointer)
	return disc
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order. An entry that does not resolve to an interned
// schema yields one error diagnostic and is dropped — never a synthesized ID
// that nothing backs (issue #14). An all-dropped mapping collapses to nil,
// preserving infer-by-name semantics and a clean round-trip.
func (l *lowerer) discriminatorMapping(d *oas3.Discriminator, pointer string) map[string]ir.TypeID {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil
	}
	out := make(map[string]ir.TypeID, m.Len())
	for value, target := range m.All() {
		id, ok := l.mappingTargetID(target)
		if !ok {
			l.diag(ir.SeverityError, codeUnresolvedRef, pointer+ptr("discriminator", "mapping", value),
				"discriminator mapping %q references unresolved schema %q", value, target)
			continue
		}
		out[value] = id
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// discriminatorDefault resolves an OpenAPI 3.2 defaultMapping to its target ID,
// dropping it with one diagnostic when it does not resolve to an interned schema.
func (l *lowerer) discriminatorDefault(d *oas3.Discriminator, pointer string) ir.TypeID {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return ""
	}
	id, ok := l.mappingTargetID(dm)
	if !ok {
		l.diag(ir.SeverityError, codeUnresolvedRef, pointer+ptr("discriminator", "defaultMapping"),
			"discriminator defaultMapping references unresolved schema %q", dm)
		return ""
	}
	return id
}

// mappingTargetID resolves a discriminator mapping target — a bare schema
// name or a $ref string — to the stable TypeID of an interned schema. A bare
// name (even one containing '/') that names a declared component resolves to
// it via typeIDForPointer regardless of source order, since every declared
// name is recorded before lowering begins — this also makes a degenerate
// empty-named component resolve to its real (anonymous) ID rather than an
// unbacked namedTypeID. Otherwise the target must be a same-file $ref to a
// declared component or an already-interned node; a target that resolves to
// neither yields ok=false, since unlike a schema position, a discriminator
// subtype cannot be hoisted from a bare pointer — the caller drops and
// diagnoses it.
func (l *lowerer) mappingTargetID(target string) (ir.TypeID, bool) {
	if l.schemas[target] {
		return typeIDForPointer(ptr("components", "schemas", target)), true
	}
	pointer, ok := l.internalPointer(target)
	if !ok {
		return "", false
	}
	if id, resolved, handled := l.resolveComponentRef(pointer); handled {
		return id, resolved
	}
	return l.internedID(pointer)
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
// Literals with an info diagnostic — nothing is dropped.
func (l *lowerer) lowerEnum(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		members, memberPrim, ok := l.enumMembers(s.GetEnum())
		if !ok {
			return l.enumAsUnion(s, common, pointer, hint)
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
}

// enumMembers converts enum nodes into scalar members, reporting ok=false when
// any member is non-scalar or the members are heterogeneous (mixed kinds). The
// returned PrimKind is the one every member's kind maps to; it is meaningful
// only when ok, and lowerEnum is reached only for a non-empty enum, so it is
// never the zero PrimKind there.
func (l *lowerer) enumMembers(nodes []values.Value) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	var kind ir.ValueKind
	var prim ir.PrimKind
	for i, node := range nodes {
		val, err := valueFromNode(node)
		if err != nil {
			return nil, "", false
		}
		memberPrim, text, admissible := enumMemberForm(val)
		if !admissible {
			return nil, "", false
		}
		if i == 0 {
			kind, prim = val.Kind, memberPrim
		} else if val.Kind != kind {
			return nil, "", false
		}
		members = append(members, ir.EnumMember{
			Name:  compile.NamingFor(text),
			Value: val,
		})
	}
	return members, prim, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func (l *lowerer) enumAsUnion(s *oas3.Schema, common ir.TypeCommon, pointer, hint string) ir.TypeDef {
	l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")
	nodes := s.GetEnum()
	variants := make([]ir.Variant, 0, len(nodes))
	for i, node := range nodes {
		vh := hint + "_" + strconv.Itoa(i)
		lptr := pointer + ptr("enum", strconv.Itoa(i))
		variants = append(variants, ir.Variant{
			Name: ir.Naming{Hint: vh},
			Type: ir.TypeRef{Target: l.hoistLiteral(node, lptr, vh)},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  true,
	}
}

// hoistLiteral hoists a single node as a Literal type at its own pointer, or
// falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's typeIDForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
func (l *lowerer) hoistLiteral(node values.Value, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		val, err := valueFromNode(node)
		if err != nil {
			l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error())
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val}
	})
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
