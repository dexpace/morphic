// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// lowerOneOfAnyOf lowers a oneOf/anyOf schema. A set of one non-null branch X
// beside any number of null branches collapses to nullable X
// (docs/ir/type-graph.md#type-references); a set with no non-null branch at all
// has no X for that collapse to name, so it lowers as a schema of null alone
// does instead (lowerNullOnlyUnion); everything else becomes a Union with one
// Variant per non-null branch (oneOf exclusive, anyOf not), never collapsing a
// union into optional fields.
func lowerOneOfAnyOf(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	if inner, ip, ok := nullUnionCollapse(c, s, pointer); ok {
		ref, diags := Ref(c, ts, depth, inner, ip)
		// The null branches say the null, so the reference admits it whatever
		// the branch it names says, and declares it where theirs or that
		// branch's own reference does (unionNullReading).
		declared := ref.Nullable && ref.Inferred == "" || valueNulls(c, s).union.declared == nullAdmitted
		return nullRef(c, ref.Target, declared, true), append(diags, liftedNullFindings(c, s, pointer)...)
	}
	if allNullUnion(c, s) {
		return lowerNullOnlyUnion(c, ts, s, pointer)
	}
	var diags []ir.Diagnostic
	tid := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		def, unionDiags := buildUnion(c, ts, besideUnion(s, pointer), common,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef {
				ref, refDiags := Ref(c, ts, depth, b, vptr)
				diags = append(diags, refDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	owner, unhomedDiags := preserveUnhomedKeywords(c, ts, s, pointer, tid, dispatch{said: branchesSay(c, s)})
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, unhomedDiags, nullDiags, liftedNullFindings(c, s, pointer))
}

// nullUnionCollapse detects a oneOf/anyOf with exactly one non-null branch
// beside null branches (nullBranch), and returns that branch's schema and
// pointer, so it lowers as nullable X, not a union node
// (docs/ir/type-graph.md#type-references).
//
// A schema declaring both combinators collapses neither: the co-declared anyOf
// conjoins, so the position is not nullable X. The Union keeps the unused
// combinator (preserveUnusedCombinator), which X's shared node cannot.
func nullUnionCollapse(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (*oas3.JSONSchema[oas3.Referenceable], jsontext.Pointer, bool) {
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return nil, "", false
	}
	variants, key, _ := unionBranches(s)
	var nonNull *oas3.JSONSchema[oas3.Referenceable]
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, v := range variants {
		if nullBranch(c, v) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = v, i
		nonNullCount++
	}
	if nullCount == 0 || nonNullCount != 1 {
		return nil, "", false
	}
	return nonNull, pointer + ids.Ptr(key, strconv.Itoa(nonNullIdx)), true
}

// allNullUnion reports whether every branch of the oneOf/anyOf combinator
// unionBranches elects is a null branch (nullBranch). nullUnionCollapse has no
// non-null branch to collapse a set like this onto, so without this check
// lowerOneOfAnyOf's buildUnion fallback strips every branch as a null marker
// and interns a Union with none left — the empty value space irverify's
// ir/union-no-variants rejects (GitHub #416). The branch set still admits
// exactly one value, the one a schema of null alone at this position admits,
// so lowerOneOfAnyOf lowers it the same way instead.
func allNullUnion(c lowering.Ctx, s *oas3.Schema) bool {
	variants, _, _ := unionBranches(s)
	return len(variants) > 0 && !slices.ContainsFunc(variants, func(v *oas3.JSONSchema[oas3.Referenceable]) bool {
		return !nullBranch(c, v)
	})
}

// lowerNullOnlyUnion lowers a oneOf/anyOf every one of whose branches admits
// null alone (allNullUnion). One such branch or several admit what a schema of
// null alone at this position admits, so the position lowers as one does
// (lowerNullAlone), and the census a body's lowering runs keeps what the node
// cannot hold. The combinator that produced it, and the other one too when both
// are declared, carries no shape the node's fields could hold, so it is kept
// verbatim under Unmodeled instead of dropped
// (docs/compilers/openapi.md#union-null-only).
func lowerNullOnlyUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeRef, []ir.Diagnostic) {
	owner, diags := preserveUnhomedKeywords(c, ts, s, pointer, lowerNullAlone(c, ts, s, pointer), dispatch{})
	diags = append(diags, preserveNullOnlyUnion(c, ts, owner, s, pointer)...)
	ref, nullDiags := schemaNullRef(c, owner, s, siteAt(pointer))
	return ref, slices.Concat(diags, nullDiags, liftedNullFindings(c, s, pointer))
}

// preserveNullOnlyUnion keeps s's oneOf/anyOf verbatim under the Unmodeled of
// the node lowerNullOnlyUnion built at id, an Enum with no member, which holds
// no branch set of its own, and reports what it kept.
func preserveNullOnlyUnion(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	kept, keepDiags := preserveBranchSets(c, &td.Common().Unmodeled, s, ir.ReasonDegradedLowering, pointer)
	diags = append(diags, keepDiags...)
	if len(kept) == 0 {
		return diags
	}
	// Both keywords land here when both are declared: buildUnion, and its
	// preserveUnusedCombinator, are never reached from this arm, so this is the
	// only place either combinator gets a home.
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"every branch admits only the null value, so this position lowered as an enum with no member "+
			"behind a nullable reference; %s kept verbatim under Unmodeled", strings.Join(kept, " and ")))
}

// preserveBranchSets stores s's declared oneOf/anyOf verbatim under p, in
// keyword order, reporting one it cannot convert. ir.ReasonValidationOnly
// routes each through the validation-only keyword-family reporting
// (annotation.PreserveValidationOnlyKeyword) and returns no keywords. Every
// other reason keeps each plainly (annotation.PreserveSchemaKeyword) and returns
// the keywords kept, so a caller with a message of its own knows what to name.
//
// preserveUnionSiblingsAt and preserveNullOnlyUnion share this loop: they keep
// the same keywords because the node they attach to has no branch set of its
// own, and differ only in the sentence that explains why.
func preserveBranchSets(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, reason ir.UnmodeledReason, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	var kept []string
	for _, kw := range []string{"oneOf", "anyOf"} {
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, kw, pointer, c.Place())...)
			continue
		}
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, kw, reason, pointer, c.Place())
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, kw)
		}
	}
	return kept, diags
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

// otherCombinator names each union keyword's counterpart, so the branch set
// unionBranches passed over is read off the choice it returned rather than
// restated beside it.
var otherCombinator = map[string]string{"oneOf": "anyOf", "anyOf": "oneOf"}

// preserveUnusedCombinator keeps the branch set unionBranches passed over
// verbatim on the Union the position lowers to, and reports it. A schema
// declaring only one combinator passes over nothing and is left alone.
//
// A schema writing both conjoins them, but one ir.Union carries one branch set.
// The position still lowers to the elected set, a shape the IR can express, and
// the other stays recoverable beside it, as recordSkippedFamilies does for
// keyword families (docs/compilers/openapi.md#competing-keywords).
//
// Where a structural sibling is written as well, classifyUnionSiblings never
// distributes the schema, so neither set is elected and both are kept beside
// the body (lowerBesideUnmodeledUnion).
func preserveUnusedCombinator(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, won string, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(s.GetOneOf()) == 0 || len(s.GetAnyOf()) == 0 {
		return nil
	}
	unused := otherCombinator[won]
	kept, diags := annotation.PreserveSchemaKeyword(p, s, unused, ir.ReasonDegradedLowering, pointer, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"oneOf and anyOf are both declared here and conjoin, and one Union carries one "+
			"branch set; this position lowered as its %s, with %s kept verbatim under Unmodeled",
		won, unused))
}

// variantTypeFunc lowers one union branch to the type its variant refers to. It
// is a parameter so the distributed lowering can conjoin the sibling composition
// into each branch while sharing every other rule about union shape — null-branch
// stripping, variant hints, exclusivity, pointer derivation.
type variantTypeFunc func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef

// buildUnion assembles the Union node for the union u, attaching the
// discriminator standing with it, where one does, with the routes it implies
// (spellImplicitRoutes). common is already built by the caller (internNode), so
// buildUnion needs no hint of its own to build one, and it places the union.
//
// The variants are lowered before the discriminator, so its mapping resolves
// knowing what each variant names (readsThrough). A union the discriminator
// routes is WireTagged, an anyOf as much as a oneOf: the tag property's value
// names its variant (docs/ir/unions.md).
func buildUnion(c lowering.Ctx, ts *compile.Types, u unionAt, common ir.TypeCommon, variantType variantTypeFunc,
) (*ir.Union, []ir.Diagnostic) {
	branches, key, exclusive := unionBranches(u.holder)
	diags := preserveUnusedCombinator(c, &common.Unmodeled, u.holder, key, u.at)
	diags = append(diags, recordNullBranches(c, &common.Unmodeled, nullBranchKeeps(c, u.holder, u.at))...)
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if nullBranch(c, b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vptr := u.at + ids.Ptr(key, strconv.Itoa(i))
		t := variantType(b, vptr)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(variantHint(c, ts, b, vptr, t)),
			Type: t,
		})
	}
	node := &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		Closed:     true,
	}
	route := mappingRoute{variants: variantTargets(variants), referents: referencedVariants(c, ts, u.holder, variants)}
	disc, discDiags := lowerDiscriminator(c, ts, u.disc, u.discAt, nil, route)
	node.Discriminator = disc
	node.WireTagged = disc != nil
	diags = append(diags, discDiags...)
	diags = append(diags, keepBranchDiscriminatorKeys(c, node, u)...)
	diags = append(diags, spellImplicitRoutes(c, u.holder, node, common.Provenance.Pointer)...)
	deferUnionTags(c, ts, u, common.ID, common.Provenance.Pointer)
	return node, diags
}

// keepBranchDiscriminatorKeys keeps on n, the Union of u, what the discriminator
// u's allOf branch writes says that reaches no field: its extensions and the
// keys OpenAPI defines for no Discriminator Object. A schema's own are read onto
// its position's node (annotation.Read), and a branch has no node, so they ride
// on the node holding the discriminator, scoped by their path from it where they
// lie beneath it (annotation.ExtensionsUnder).
func keepBranchDiscriminatorKeys(c lowering.Ctx, n *ir.Union, u unionAt) []ir.Diagnostic {
	if !u.branch || u.disc == nil || u.discAt != u.at {
		return nil
	}
	owner := u.at + ids.Ptr("discriminator")
	scope := "discriminator"
	if rest, beneath := strings.CutPrefix(string(owner), string(n.Provenance.Pointer)+"/"); beneath {
		scope = rest
	}
	kept, diags := annotation.ExtensionsUnder(u.disc.GetExtensions(), c.Place(), owner, scope)
	diags = append(diags, annotation.UnknownKeysUnder(&kept, u.disc, c.Place(), owner, scope)...)
	n.Unmodeled = annotation.MergeUnmodeled(n.Unmodeled, kept)
	return diags
}

// unionAt is a union a position lowers: the oneOf or anyOf, whichever
// unionBranches elects, that holder writes at the pointer at, and the
// discriminator standing with it, written by the schema at discAt. holder is the
// schema written at the position, or an inline branch of its allOf, which JSON
// Schema conjoins with the rest of the composition as it would one written
// beside it (docs/compilers/openapi.md#union-beside-structure).
type unionAt struct {
	holder *oas3.Schema
	at     jsontext.Pointer
	// disc is the discriminator the union is routed by, nil for none: one written
	// beside it, else one beside the allOf holding its branch (branchUnion).
	disc   *oas3.Discriminator
	discAt jsontext.Pointer
	// branch marks a holder that is an allOf branch, no schema at a position.
	branch bool
}

// besideUnion is the union s, written at pointer, writes itself, with the
// discriminator written beside it.
func besideUnion(s *oas3.Schema, pointer jsontext.Pointer) unionAt {
	return unionAt{holder: s, at: pointer, disc: s.GetDiscriminator(), discAt: pointer}
}

// branchUnionOf keys what branchUnion read of one schema.
type branchUnionOf struct {
	schema *oas3.Schema
}

// branchUnionIndex is where in a schema's flattened allOf the branch whose
// union it conjoins is, where one is.
type branchUnionIndex struct {
	index int
	ok    bool
}

// branchUnion returns the union an inline branch of s's allOf writes that s,
// written at pointer, conjoins with the rest of its composition, as it would
// the same union written beside the allOf
// (docs/compilers/openapi.md#union-beside-structure), with the discriminator
// written on the branch, else beside the allOf, and how the position lowers it
// (classifyConjoined): ok is false where s is the base of the hierarchy the
// discriminator routes, which leaves the union with its branch.
func branchUnion(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (unionAt, unionLowering, bool) {
	at := branchUnionIndexOf(c, s)
	if !at.ok {
		return unionAt{}, 0, false
	}
	e := allOfBranches(s, pointer).branches[at.index]
	u := unionAt{holder: e.schema.GetSchema(), at: e.pointer, disc: s.GetDiscriminator(), discAt: pointer, branch: true}
	if d := u.holder.GetDiscriminator(); d != nil {
		u.disc, u.discAt = d, e.pointer
	}
	how := classifyConjoined(c, s, u, pointer)
	return u, how, how != unionHierarchyBase
}

// branchUnionIndexOf returns findBranchUnion's reading of s, read once a run, as
// every reader of a position s is written at, and of a hierarchy s is the base
// of, asks.
func branchUnionIndexOf(c lowering.Ctx, s *oas3.Schema) branchUnionIndex {
	return lowering.Remembered(c, branchUnionOf{s}, func() branchUnionIndex { return findBranchUnion(c, s) })
}

// findBranchUnion is branchUnion's reading of s alone: where s lowers its
// allOf, composing, not refining, the one branch whose union declares shape
// against s, where that branch lies within the flattening depth and
// classifyBranchSet distributes its union. Two such unions leave none, either
// dropping the other, and one declaring no shape stays residue. A union s and
// its branch each write a discriminator beside leaves none either: nothing says
// which routes it. A $ref site or a union beside the allOf lowers first
// (lowerSchemaBody), and is no reading of a branch's.
func findBranchUnion(c lowering.Ctx, s *oas3.Schema) branchUnionIndex {
	if declaresUnion(s) || dispatchOf(s).won != "allOf" {
		return branchUnionIndex{}
	}
	branches := allOfBranches(s, "").branches
	index, count := 0, 0
	for i, e := range branches {
		bs := e.schema.GetSchema()
		if isRefBranch(e.schema) || !declaresUnion(bs) || !setsDeclareShape(c, s, bs.GetOneOf(), bs.GetAnyOf()) {
			continue
		}
		index, count = i, count+1
	}
	if count != 1 || branches[index].capped {
		return branchUnionIndex{}
	}
	if holder := branches[index].schema.GetSchema(); holder.GetDiscriminator() != nil && s.GetDiscriminator() != nil ||
		classifyBranchSet(c, holder) != unionDistributed {
		return branchUnionIndex{}
	}
	if _, refines := refinementOf(c, s, ""); refines {
		return branchUnionIndex{}
	}
	return branchUnionIndex{index: index, ok: true}
}

// variantHint is the hint the union variant for the branch b written at vptr,
// which lowered to t, takes: the branch's own (lowering.NameAt), unless b is
// inline, no title names it (lowering.NameTitle), and it refines the $ref its
// allOf names, or t is a decorator (ir.Decorated) over it, when it is the name
// the $ref suggests (lowering.NameThrough). 3.0 annotates a branch only by
// wrapping its $ref, so `{allOf: [{$ref: X}], description: …}` names its
// variant as 3.1's `{$ref: X, description: …}` does (GitHub #1018), a title
// beside either too. The node t keeps the branch's own.
func variantHint(c lowering.Ctx, ts *compile.Types, b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer, t ir.TypeRef) string {
	own := lowering.NameAt(c, vptr)
	s := annotation.SchemaOf(b)
	if s == nil || isRefBranch(b) {
		return own
	}
	if _, titled := lowering.NameTitle(c, vptr); titled {
		return own
	}
	if r, refines := refinementOf(c, s, vptr); refines && r.ref != nil {
		return cmp.Or(lowering.NameThrough(c, r.ref.pointer), own)
	}
	// The flattened list, as b lowers: a $ref a nested wrapper holds names the
	// variant as it would written at the top.
	branches := allOfBranches(s, vptr).branches
	base, ok := selectAllOfBase(c, branches)
	if !ok {
		return own
	}
	if td, ok := ts.Node(t.Target); !ok || !decorates(td) {
		return own
	}
	return cmp.Or(lowering.NameThrough(c, branches[base].pointer), own)
}

// decorates reports whether td is a decorator (ir.Decorated).
func decorates(td ir.TypeDef) bool {
	_, ok := ir.Decorated(td)
	return ok
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// untagged union with one variant per declared type, exclusive unless two of
// them overlap (typesDisjoint). A type array asks for one of its types, not for
// exactly one, so overlapping variants must not exclude each other; with an
// integer width, integer and number name the same primitive, and the two
// variants stay, for an emitter to keep apart (docs/emitters/targets/go/unions.md).
// Each variant reads what the position's folding allOf branches write, at the
// pointers they are written at.
func lowerUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, types []oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		list := typeArray{types: types, voices: positionVoices(c, ts, s, pointer, ir.TypeRef{})}
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, vDiags := lowerTypeVariant(c, ts, depth, s, vptr, st, list)
			diags = append(diags, vDiags...)
			variants = append(variants, ir.Variant{
				Name: compile.NamingHint(string(st)),
				Type: ir.TypeRef{Target: vid},
			})
		}
		// A type array listing no object routes no instance and resolves nothing,
		// as no node keeping a discriminator verbatim does (GitHub #1662, #1708).
		if slices.Contains(types, oas3.SchemaTypeObject) {
			diags = append(diags, resolveDiscriminatorRoutes(c, ts, s, pointer)...)
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  typesDisjoint(types),
			Closed:     true,
		}
	})
	return id, diags
}

// typeArray is what every variant of one type array reads beside its schema: the
// types it lists, and the voices of the position it is written at.
type typeArray struct {
	types  []oas3.SchemaType
	voices []annotation.Voice
}

// lowerTypeVariant lowers the variant of s's type array that st names, at
// pointer, as lowerTyped lowers st alone, but for what applies to another listed
// type (docs/compilers/openapi.md#type-arrays). An object variant holds no
// discriminator: no $ref or mapping can name the variant's position, which holds
// a type's name and no schema, so no type is its subtype; the census keeps the
// discriminator on the union instead. A scalar variant reads the value bounds,
// the format and the content vocabulary st takes, and is a Scalar of its own
// where it holds one, else the shared primitive.
func lowerTypeVariant(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, st oas3.SchemaType, list typeArray) (ir.TypeID, []ir.Diagnostic) {
	switch st {
	case oas3.SchemaTypeObject:
		return lowerModel(c, ts, depth, s, pointer, false /* discriminated */, kindObject)
	case oas3.SchemaTypeArray:
		return lowerArray(c, ts, depth, s, pointer)
	default:
		takes := typeKinds(st)
		shape := scalarShapeOf(s, st, variantFormat(s, st, list.types))
		if shape.shared && !statesBounds(c, s, list.voices, pointer, takes) {
			return ts.PrimID(shape.base), nil
		}
		return hoistScalar(c, ts, depth, s, shape, takes, list.voices, pointer)
	}
}

// variantFormat returns the format s writes, for its type array's variant of
// type st among types: the format where it pairs with st, or with no listed
// type, since nothing then says which type it describes, and a format validates
// only the instances of the types it is defined for (JSON Schema Validation
// 2020-12 §7.1); none where it pairs with another listed type.
func variantFormat(s *oas3.Schema, st oas3.SchemaType, types []oas3.SchemaType) string {
	format := writtenFormat(s)
	if format == "" || pairs(st, format) {
		return format
	}
	if slices.ContainsFunc(types, func(t oas3.SchemaType) bool { return pairs(t, format) }) {
		return ""
	}
	return format
}

// pairs reports whether format pairs with st: spelling.Lookup lists the
// pairing, or spelling.LookupEncoded does, as it does byte beside a string.
func pairs(st oas3.SchemaType, format string) bool {
	key := string(st) + "/" + format
	_, listed := spelling.Lookup(key)
	_, encoded := spelling.LookupEncoded(key)
	return listed || encoded
}

// statesBounds reports whether s or one of voices, read at pointer, writes a
// value bound a reader of the kinds takes holds: one ConstraintsOf reads into a
// field or keeps, as an empty pattern or a keyword written null is not. It reads
// apart from the node, so what it reads is reported where hoistScalar reads it.
func statesBounds(c lowering.Ctx, s *oas3.Schema, voices []annotation.Voice, pointer jsontext.Pointer, takes kindSet) bool {
	var kept ir.Unmodeled
	cons, _ := ConstraintsOf(c, &kept, s, voices, pointer, takes)
	return cons != nil || len(kept) > 0
}

// typesDisjoint reports whether no instance has two of types. A type listed
// twice overlaps itself, and JSON's types are otherwise disjoint but for one
// pair: every integer is a number.
func typesDisjoint(types []oas3.SchemaType) bool {
	if len(slices.Compact(slices.Sorted(slices.Values(types)))) != len(types) {
		return false
	}
	return !slices.Contains(types, oas3.SchemaTypeInteger) || !slices.Contains(types, oas3.SchemaTypeNumber)
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, a composing allOf, const/enum, ...) or a keyword
// that narrows an instance without declaring one. When it does, the union alone
// cannot represent the schema, so the sibling body is lowered too (invariant 2,
// docs/ir/models.md#composition-beside-a-union), unless the branches declare
// shape and say every one of those keywords (branchesSay): the schema then means
// the union alone. Branches declaring none narrow the body, whatever they
// repeat, and a discriminator routing to branches composing the body keeps it,
// their base (composedByBranch). A refining allOf is no body
// (docs/compilers/openapi.md#allof-refinement).
func hasUnionSiblings(c lowering.Ctx, s *oas3.Schema) bool {
	if !declaresUnion(s) || composedByBranch(c, s, besideUnion(s, "")) {
		return writesSiblingKeyword(s) // no branch says it, or the body is their base
	}
	siblings := siblingKeywords(s)
	if !allOfComposes(c, s) {
		siblings = slices.DeleteFunc(siblings, func(k string) bool { return k == "allOf" })
	}
	if len(siblings) == 0 {
		return false
	}
	said := branchesSay(c, s)
	if slices.ContainsFunc(siblings, func(k string) bool { return !slices.Contains(said, k) }) {
		return true
	}
	return !unionBranchesDeclareShape(c, s)
}

// restatableKeywords are the keywords beside a union that its branches can say
// in full, read as admission reads what a schema admits.
var restatableKeywords = []string{"type", "format", "enum", "const", "items"}

// branchesSayOf keys what branchesSay read of one schema.
type branchesSayOf struct {
	schema *oas3.Schema
}

// branchesSay returns the keywords of restatableKeywords s writes beside its
// oneOf and anyOf that every branch of both already says: a `type` naming each
// kind a branch admits, null included, and a `format`, an `enum`, a `const` or
// an `items` each branch is held to (admission.holds). Such a keyword adds
// nothing to the union, so the position reads as if it were not written
// (docs/compilers/openapi.md#union-beside-structure). A read cut short says
// none, and so does a schema writing no union, of which "every branch" would
// say anything. Every reader of the position asks, so it is read once a run.
func branchesSay(c lowering.Ctx, s *oas3.Schema) []string {
	return lowering.Remembered(c, branchesSayOf{s}, func() []string {
		branches := slices.Concat(s.GetOneOf(), s.GetAnyOf())
		if len(branches) == 0 {
			return nil
		}
		w := newAdmission(c)
		reads := make([]admitted, len(branches))
		for i, b := range branches {
			reads[i] = w.whole(b)
		}
		if w.incomplete() {
			return nil
		}
		var out []string
		for _, key := range restatableKeywords {
			if annotation.RawPropertyNode(s, key) == nil {
				continue
			}
			if !slices.ContainsFunc(reads, func(r admitted) bool { return !branchSays(&w, r, s, key) }) {
				out = append(out, key)
			}
		}
		return out
	})
}

// branchSays reports whether a branch admitting branch, as w reads it, says all
// that s's keyword key does. A `type` is compared with null in it, which
// admission.holds leaves to the nulls reader: a union lowered alone lifts a null
// branch onto the position, so a `type` leaving null out says more than a branch
// admitting it. An `items` list says what no single schema does.
func branchSays(w *admission, branch admitted, s *oas3.Schema, key string) bool {
	switch key {
	case "type":
		return branch.kinds&^writtenKinds(s, w.nullable) == 0
	case "items":
		_, listed := listedItems(s)
		return !listed && w.holds(branch, s, key)
	default:
		return w.holds(branch, s, key)
	}
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
	// unionUnresolvedBranch marks a union with a $ref branch naming nothing this
	// compilation can resolve — a target in a document it did not read, an
	// undeclared component, a pointer addressing no schema.
	unionUnresolvedBranch
	// unionNullBranch marks a union with a null branch (nullBranch), which
	// buildUnion lifts onto the position's Nullable rather than conjoining it, so
	// no variant could say whether the body admits the null (GitHub #1856).
	unionNullBranch
	// unionHierarchyBase marks a union beside a discriminator routing to
	// branches that compose the body through allOf, its subtypes, which a union
	// of composed variants would no longer have for a base
	// (composedByBranch).
	unionHierarchyBase
	// unionRoutedSubtype marks a body a discriminator on an ancestor routes to
	// (routedSubtype). The route names the type the body lowers to, which as a
	// union would be no subtype of that ancestor, so it stays a Model composing
	// the union as a mixin (lowerSubtypeUnion).
	unionRoutedSubtype
)

// classifyUnionSiblings picks the lowering for a schema whose oneOf/anyOf sits
// beside structural keywords. JSON Schema conjoins keywords, so such a schema
// is an intersection of a structural body and a union, which the IR
// deliberately has no combinator for
// (docs/ir/boundaries.md#deliberate-exclusions). Every outcome keeps both
// sides: classified where the IR can express the conjunction, verbatim where it
// cannot.
//
// What the body does with a union it may distribute is classifyConjoined's.
// docs/compilers/openapi.md#union-beside-structure enumerates the shapes.
func classifyUnionSiblings(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) unionLowering {
	if !unionBranchesDeclareShape(c, s) {
		return unionValidationOnly
	}
	if !composesAsModel(c, s) {
		return unionUncomposableBody
	}
	if outcome := classifyBranchSet(c, s); outcome != unionDistributed {
		return outcome
	}
	return classifyConjoined(c, s, besideUnion(s, pointer), pointer)
}

// classifyConjoined is how s, written at pointer, lowers the union u it
// conjoins with a composing body, beside it or on a branch of its allOf, once
// u's own shape allows distribution: kept whole where the discriminator standing
// with u routes to branches composing s (composedByBranch), composed as a mixin
// where one on an ancestor routes to s (routedSubtype, lowerSubtypeUnion); else
// distributed, the discriminator routing the composed variants
// (referencedVariants).
func classifyConjoined(c lowering.Ctx, s *oas3.Schema, u unionAt, pointer jsontext.Pointer) unionLowering {
	if composedByBranch(c, s, u) {
		return unionHierarchyBase
	}
	if routedSubtype(c, s, pointer) {
		return unionRoutedSubtype
	}
	return unionDistributed
}

// routedSubtype reports whether a discriminator on an ancestor of s, written at
// pointer, routes to s as its subtype: by a mapping key or by its name as a
// component, the tag its lowering takes (subtypeDiscriminatorValue). That
// lowering reports the tag's own diagnostics, so this reading drops them.
func routedSubtype(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) bool {
	tag, _ := subtypeDiscriminatorValue(c, s, c.IDs().ForPointer(pointer), pointer)
	return tag.routed
}

// classifyBranchSet is the part of classifyUnionSiblings that reads the union
// holder writes rather than the body: whichever of a union beside the body and
// one on a branch of its allOf the body distributes (branchUnion), it does so
// only where this says unionDistributed.
func classifyBranchSet(c lowering.Ctx, holder *oas3.Schema) unionLowering {
	if len(holder.GetOneOf()) > 0 && len(holder.GetAnyOf()) > 0 {
		return unionBothCombinators
	}
	branches, _, _ := unionBranches(holder)
	if slices.ContainsFunc(branches, func(b *oas3.JSONSchema[oas3.Referenceable]) bool { return nullBranch(c, b) }) {
		return unionNullBranch
	}
	if !branchesNameReferents(c, holder) {
		return unionUnresolvedBranch
	}
	return unionDistributed
}

// composesAsModel reports whether the structural siblings lower to a Model,
// which is the only node with composition fields for a union to distribute
// across. It mirrors lower's dispatch: const and enum win over everything, then
// an allOf that composes, then a declared object type or a bare property set. A
// bare `required` is one too where every branch admits only an object, the one
// instance it constrains, or null, which it leaves alone
// (branchesAdmitOnlyObjects).
func composesAsModel(c lowering.Ctx, s *oas3.Schema) bool {
	if s.GetConst() != nil || enumWritten(s) {
		return false
	}
	if allOfComposes(c, s) {
		return true
	}
	types := effectiveTypes(s)
	if len(types) > 0 {
		return len(types) == 1 && types[0] == oas3.SchemaTypeObject
	}
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	return slices.Equal(siblingKeywords(s), []string{"required"}) && branchesAdmitOnlyObjects(c, s)
}

// branchesAdmitOnlyObjects reports whether each branch of the combinator
// unionBranches elects admits an object or null and nothing else, read as
// admission reads it. A read cut short says no.
func branchesAdmitOnlyObjects(c lowering.Ctx, s *oas3.Schema) bool {
	branches, _, _ := unionBranches(s)
	w := newAdmission(c)
	objectsOnly := !slices.ContainsFunc(branches, func(b *oas3.JSONSchema[oas3.Referenceable]) bool {
		return w.whole(b).kinds&^(kindObject|kindNull) != 0
	})
	return objectsOnly && !w.incomplete()
}

// branchesNameReferents reports whether every union branch can be conjoined with
// the sibling model. Base and Mixins conjoin *by reference*, so a $ref branch
// qualifies exactly when it names a schema this compilation resolves, and an
// inline one does as the node hoisted at its own pointer. The test is over the
// whole union because a half-distributed one would leave variants disagreeing
// about whether they carry the body, with nothing in the IR saying which.
func branchesNameReferents(c lowering.Ctx, s *oas3.Schema) bool {
	branches, _, _ := unionBranches(s)
	return !slices.ContainsFunc(branches, func(b *oas3.JSONSchema[oas3.Referenceable]) bool {
		return isRefBranch(b) && !c.RefScope().NamesReferent(b)
	})
}

// coDeclaredUnionWhyPrefix opens every lowerCoDeclaredUnion why string: at
// each of its reasons, a structural body genuinely sits beside the union
// (that is what routed the schema here rather than to a $ref-site union
// keeper), so the sentence that follows is always true of it. refSiteUnionWhy
// does not share this prefix, because at a $ref site it would not be
// (preserveUnionSiblingsAt's doc comment says why).
const coDeclaredUnionWhyPrefix = "oneOf/anyOf co-declared with structural keywords intersects with them, and "

// lowerCoDeclaredUnion lowers a schema whose oneOf/anyOf sits beside structural
// keywords, per classifyUnionSiblings.
func lowerCoDeclaredUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	beside := func(reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
		if why != "" {
			why = coDeclaredUnionWhyPrefix + why
		}
		return lowerBesideUnmodeledUnion(c, ts, depth, s, pointer, reason, why)
	}
	switch classifyUnionSiblings(c, s, pointer) {
	case unionDistributed:
		return lowerDistributedUnion(c, ts, depth, s, besideUnion(s, pointer), pointer)
	case unionValidationOnly:
		return beside(ir.ReasonValidationOnly, "")
	case unionUncomposableBody:
		return beside(ir.ReasonDegradedLowering,
			"the body is not a model, so it carries no composition to distribute into")
	case unionBothCombinators:
		return beside(ir.ReasonDegradedLowering,
			"oneOf and anyOf are both declared, so distributing either would drop the other")
	case unionUnresolvedBranch:
		id, diags := beside(ir.ReasonDegradedLowering,
			"a branch's $ref names no referent this compilation resolves")
		return id, append(diagUnresolvedBranches(c, s, pointer), diags...)
	case unionNullBranch:
		return beside(ir.ReasonDegradedLowering,
			"a branch admits null alone, which the union lifts onto the position, so no variant can conjoin the body with it")
	case unionRoutedSubtype:
		return lowerSubtypeUnion(c, ts, depth, s, besideUnion(s, pointer), pointer)
	default: // unionHierarchyBase
		return beside(ir.ReasonDegradedLowering,
			"the discriminator beside it routes to branches composing this schema, its subtypes, which distributing them into a union would undo")
	}
}

// diagUnresolvedBranches returns one diagnostic per union branch whose $ref
// names nothing this compilation resolves, but none for a $ref on a loop of
// $refs, which the load phase reports once (onLoop). The
// branch text survives verbatim under Unmodeled, so nothing is dropped, but a
// reference that resolves nowhere is a defect of the document itself, reported
// at the same severity as everywhere else, with the reason refTypeRef's report
// gives (unresolvedWhy).
func diagUnresolvedBranches(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	branches, key, _ := unionBranches(s)
	var diags []ir.Diagnostic
	for i, b := range branches {
		if !isRefBranch(b) || c.RefScope().NamesReferent(b) || onLoop(c, b) {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr(key, strconv.Itoa(i)),
			"union branch $ref %q is unresolved%s; the branch is kept verbatim", b.GetRef().String(), unresolvedWhy(c, b)))
	}
	return diags
}

// lowerBesideUnmodeledUnion lowers the structural body of a schema that
// co-declares oneOf/anyOf and keeps the union verbatim beside it, so neither the
// structural shape nor the union is dropped. reason says which kind of union it
// is and why says what stopped a classified lowering; lowerCoDeclaredUnion picks
// both from classifyUnionSiblings' verdict. A discriminator the body's model
// holds is settled once every node is interned (deferDiscriminatorBesideUnion).
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
	inner, diags := lower(c, ts, depth, s, pointer)
	owner := inner
	if got, _ := ts.Lookup(c.IDs().Coordinate(pointer)); got != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, ir.TypeRef{Target: inner})
		diags = append(diags, aliasDiags...)
	}
	deferDiscriminatorBesideUnion(c, ts, owner, s, s, pointer)
	return owner, append(diags, preserveUnionSiblings(c, ts, owner, s, pointer, reason, why)...)
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins the validation-only keywords and is
// reported with it; anything else is a degraded lowering and says so.
func preserveUnionSiblings(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	return append(diags, preserveUnionSiblingsAt(c, &td.Common().Unmodeled, s, pointer, reason, why)...)
}

// preserveUnionSiblingsAt is preserveUnionSiblings' body, addressed by the
// Unmodeled map to write into. PreserveRefSiteKeywords calls it directly to
// keep a $ref site's union on a carrier's Unmodeled, where there is no node to
// look up by ID (GitHub #406).
//
// why is the caller's complete sentence, not a clause in a shared frame:
// lowerCoDeclaredUnion's reasons presuppose a structural body at the position,
// and a $ref site has none, so a shared template would contradict itself there.
func preserveUnionSiblingsAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	kept, diags := preserveBranchSets(c, p, s, reason, pointer)
	if reason == ir.ReasonValidationOnly || len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"%s; union branches kept verbatim under Unmodeled", why))
}

// declaresUnion reports whether s writes oneOf or anyOf at all. It is the one
// predicate every $ref-site union keeper — a component or inline sub-schema
// (refSiteRef), a carrier (PreserveRefSiteKeywords), an allOf branch spelled
// as a $ref (fillAllOf) — gates hoisting a home for the union on, so the
// three cannot drift into checking it three different ways.
func declaresUnion(s *oas3.Schema) bool {
	return len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0
}

// buildsVariants reports whether a Union s lowers to holds variants s builds
// itself, each conjoining what s writes: a type array's (lowerUnion) or a
// distributed composition's (lowerDistributedUnion). A union of s's oneOf or
// anyOf alone (lowerOneOfAnyOf) holds the nodes its branches lowered to
// instead. It reads what lowerSchemaBody dispatches on.
func buildsVariants(c lowering.Ctx, s *oas3.Schema) bool {
	return !declaresUnion(s) || hasUnionSiblings(c, s)
}

// refSiteUnionWhy is preserveUnionSiblingsAt's why at every $ref site: a
// complete sentence in its own right (see preserveUnionSiblingsAt's doc for
// why it cannot share lowerCoDeclaredUnion's framing), because a $ref site's
// own declaration is the reference alone — there is no structural body at
// that position for the union to distribute its composition across.
const refSiteUnionWhy = "oneOf/anyOf beside a $ref conjoin with it rather than composing into a structural body, so there is nothing at this position to distribute the union's composition across"

// lowerDistributedUnion emits the Union that is the schema's value, distributing
// the sibling composition across the variants of u: `S ∧ (X | Y)` becomes
// `(S ∧ X) | (S ∧ Y)`. Each variant is a Model classifying S's Base/Mixins and
// own properties (docs/ir/models.md#composition-beside-a-union) alongside its
// branch, so the composition is carried on every variant rather than merged
// into one or dropped. A union an allOf branch writes is the branch's to give,
// not residue each variant keeps (allOfComposition.conjoins).
func lowerDistributedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, u unionAt, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		body := composedBody{schema: s, comp: readAllOf(c, s, pointer),
			voices: positionVoices(c, ts, s, pointer, ir.TypeRef{})}
		diags = append(diags, diagDisjointConjuncts(c, body.comp, composedShape)...)
		def, unionDiags := buildUnion(c, ts, u, common,
			func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef {
				ref, variantDiags := composedVariant(c, ts, depth, body, b, vptr)
				diags = append(diags, variantDiags...)
				return ref
			})
		diags = append(diags, unionDiags...)
		return def
	})
	diags = append(diags, preserveVariantsUnhomed(c, ts, s, id, distributedShape, nil, pointer)...)
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
// discriminator written beside u is the Union's, routing its variants, so the
// Model neither lowers nor keeps it. The first lowering of the position composes
// the union, and any later one finds it.
func lowerSubtypeUnion(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, u unionAt, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	_, lowered := ts.Lookup(c.IDs().Coordinate(pointer))
	d := dispatchOf(s)
	if u.disc != nil && u.discAt == pointer {
		d.said = append(d.said, "discriminator")
	}
	id, diags := lowerElected(c, ts, depth, s, pointer, d)
	if lowered {
		return id, diags
	}
	mixin, mintDiags := mintUnion(c, ts, depth, u)
	diags = append(diags, mintDiags...)
	diags = append(diags, composeMixin(c, ts, id, mixin, pointer)...)
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
	_, key, _ := unionBranches(u.holder)
	at := u.at + ids.Ptr(key)
	id := c.IDs().ComposedType(at)
	common := commonFor(c, id, at)
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(c, u.at), key))
	var diags []ir.Diagnostic
	node, unionDiags := buildUnion(c, ts, u, common, func(b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer) ir.TypeRef {
		ref, refDiags := Ref(c, ts, depth, b, vptr)
		diags = append(diags, refDiags...)
		return ref
	})
	ts.Register(id, node)
	return id, slices.Concat(diags, unionDiags)
}

// composeMixin appends mixin to the Mixins of the model id names, last, so the
// $ref entries its source writes classify first. Its one caller has just
// lowered the model, so any other node there is a compiler bug.
func composeMixin(c lowering.Ctx, ts *compile.Types, id, mixin ir.TypeID, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	m, isModel := td.(*ir.Model)
	if !isModel {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: type %q is not the model a routed subtype composes its union into", id)}
	}
	m.Mixins = append(m.Mixins, ir.TypeRef{Target: mixin})
	return nil
}

// composedBody is the schema every distributed variant carries: the enclosing
// schema and where it is written.
type composedBody struct {
	schema *oas3.Schema
	comp   allOfComposition   // schema's flattened allOf, built once for every variant
	voices []annotation.Voice // what comp's folding branches say of the position, read once too
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
	b *oas3.JSONSchema[oas3.Referenceable], vptr jsontext.Pointer,
) (ir.TypeRef, []ir.Diagnostic) {
	// The branch lowers through the ordinary schema path, at its own pointer, so
	// it keeps whatever it declares beside the $ref and a reference to that
	// pointer still finds the branch rather than the variant.
	branch, diags := Ref(c, ts, depth, b, vptr)
	id := c.IDs().ComposedType(vptr)
	common := commonFor(c, id, vptr)
	// The Model conjoins the union's body with the branch, so it is named for
	// both.
	common.Name = compile.NamingHint(compile.SubHint(lowering.NameAt(c, body.comp.pointer), lowering.NameAt(c, vptr)))
	def, variantDiags := buildComposedVariant(c, ts, depth, body, branch.Target, common)
	ts.Register(id, def)
	return composedVariantRef(c, id, body.schema, branch), append(diags, variantDiags...)
}

// composedVariantRef returns the reference a union variant naming the
// synthesized variant id takes. The variant conjoins the enclosing body with
// the branch, so it admits null exactly when the branch does and the body does
// not forbid it: the allOf rule, applied to a conjunction the source does not
// spell as an allOf, under each reading of nullable. The branch's declared
// reading is its bit with no marker. The bit lives on this TypeRef, not on the
// variant's Base or Mixins, for the reason conjoinBranch records.
func composedVariantRef(c lowering.Ctx, id ir.TypeID, body *oas3.Schema, branch ir.TypeRef) ir.TypeRef {
	if !branch.Nullable {
		return ir.TypeRef{Target: id}
	}
	n := newNulls(c)
	r := n.schema(body, nullSite{})
	return nullRef(c, id, branch.Inferred == "" && r.declared != nullForbidden, r.lenient != nullForbidden)
}

// buildComposedVariant assembles the variant Model itself. Every fill reads the
// enclosing schema at the enclosing pointer, so the properties, their PropIDs
// and any shared additionalProperties node are the single set the source
// declared, named after the enclosing schema, not after whichever branch built
// them first.
//
// The enclosing schema's value constraints, its folding branches' among them,
// are read as lowerAllOf reads them (GitHub #407). Each variant carries a copy,
// since a bound on the schema constrains both sides of the disjunction.
func buildComposedVariant(c lowering.Ctx, ts *compile.Types, depth int, body composedBody, branch ir.TypeID, common ir.TypeCommon) (ir.TypeDef, []ir.Diagnostic) {
	cons, diags := ConstraintsOf(c, &common.Unmodeled, body.schema, body.voices, body.comp.pointer, declaredInstanceTypes(body.schema))
	m := &ir.Model{TypeCommon: common, Constraints: cons}
	fields := newModelFields(c, ts, m)
	diags = append(diags, fillAllOf(c, ts, depth, m, fields, body.comp)...)
	diags = append(diags, fillModelProperties(c, ts, depth, fields, body.schema, body.comp.pointer)...)
	requireMembers(m, compositionRequired(body.schema.GetRequired(), body.comp))
	diags = append(diags, fillAdditional(c, ts, depth, m, body.schema, body.comp.pointer)...)
	diags = append(diags, applyFalseBranches(c, m, body.schema, body.comp)...)
	conjoinBranch(m, branch)
	// No variant carries a DiscriminatorValue, which tags a hierarchy's
	// subtype: a schema a discriminated ancestor routes to is never distributed
	// (unionRoutedSubtype), and a discriminator beside the union routes its
	// variants by its mapping (spellImplicitRoutes).
	return m, diags
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
