package schema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
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

// LowerComponentSchemas interns every named component schema in source order.
// It is the entry Compile's run() calls before any operation lowering so that
// $refs resolve to already-registered IDs.
func LowerComponentSchemas(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex) []ir.Diagnostic {
	comps := c.Doc.Components
	if comps == nil {
		return nil
	}
	schemas := comps.GetSchemas()
	if schemas == nil {
		return nil
	}
	var diags []ir.Diagnostic
	// The declared-name index the $ref and discriminator-mapping resolutions read
	// is derived at entry (lowering.New), so a component declared later in the
	// document is already a valid target here regardless of source order.
	for name, js := range schemas.All() {
		diags = append(diags, lowerComponentSchema(c, ts, anchors, js, ids.Ptr("components", "schemas", name), name)...)
	}
	return diags
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func lowerComponentSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable], pointer, name string) []ir.Diagnostic {
	s := annotation.At(js)
	ref, diags := Ref(c, ts, anchors, TopLevelDepth, js, pointer, name)
	// A body that interned the component's own node at its component ID needs no
	// alias, and its annotations were attached where it was lowered.
	if _, owned := ts.Lookup(pointer); owned {
		return diags
	}
	cons, consDiags := schemaConstraints(c, s.Node, pointer)
	diags = append(diags, consDiags...)
	internAlias(c, ts, pointer, name, ref, cons)
	// This alias is the first node the pointer owns, so the annotations
	// schemaBody had nowhere to put now have a home.
	if s.Node != nil {
		diags = append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
	}
	return diags
}

// recordDeclarationResidue keeps, on the node pointer owns, every keyword the
// declaration wrote that only a use site can hold.
//
// ir-design §14's OpenAPI row applies these to referencing properties with
// use-site precedence, which the property path already does; this is the other
// half of that rule, and it is what a declaration nothing references would
// otherwise lose silently (GitHub #138).
//
// It runs only at an annotation.HomeOwnNode position, the one position with no
// carrier at all. Every annotation.HomeCarrier position has one that already
// keeps these: `default` lands in a real field at each of them
// (fillPropertyDefault, fillParamDefault), readOnly/writeOnly land in
// ir.Property.Visibility at a model property and at a header
// (annotation.EffectiveVisibility, both carried as ir.Property), and at a
// parameter — the one carrier ir-design gives no Visibility field — params.go's
// preserveParamVisibility keeps them verbatim on the ir.Parameter instead.
// Residue on the node would restate what one of those holds rather than rescue
// anything.
//
// residueKeywords is what declaresPositionScoped gates hoisting on, so a
// position that wrote one of them owns a node by the time this runs; a pointer
// with none is a position whose declaration wrote nothing at all.
func recordDeclarationResidue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer string, home annotation.Home) []ir.Diagnostic {
	if home != annotation.HomeOwnNode || s == nil {
		return nil
	}
	td, ok := ts.NodeAt(pointer)
	if !ok {
		return nil
	}
	return recordResidue(c, td.Common(), s, pointer)
}

// residueKeywords are the keywords a schema can write that bind a *use* of the
// type rather than the type itself, so no type node has a field to hold them:
// `default` (Property/Parameter.Default is its only home) and
// readOnly/writeOnly (Property.Visibility is theirs).
//
// One list, read by both the predicate that hoists a node for them
// (declaresPositionScoped, via annotation.DeclaresAny) and the recorder that
// fills it (recordResidue), so the two can never drift into either half of
// declaresPositionScoped's trap.
var residueKeywords = []string{"default", "readOnly", "writeOnly"}

// ResidueKeywords returns that list, for the carrier lowerings outside this
// package that preserve the same set at their own positions.
//
// It hands back a copy rather than the slice. Being one list is the whole point
// — a keyword added here has to reach every position that preserves one — and an
// exported slice is a mutable global: any importer could rewrite what every
// schema position in the process preserves, silently and for good.
func ResidueKeywords() []string { return slices.Clone(residueKeywords) }

// recordResidue keeps each declared residue keyword verbatim on c and reports
// it at the keyword's own pointer.
//
// The message qualifies where §14 applies the keyword, because a carrier applies
// it only where it has a field for it. `default` reaches one at every carrier,
// but readOnly/writeOnly reach ir.Property.Visibility at a property or a header
// and nothing at all at a parameter, which has no such field — so a $ref'd
// declaration's readOnly is visible to a referencing parameter only here, on the
// declaration's own node. An inline position that owns a node has no referencing
// carrier at all, which is the case Unmodeled is rescuing.
func recordResidue(c lowering.Ctx, common *ir.TypeCommon, s *oas3.Schema, pointer string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range residueKeywords {
		kept, keptDiags := PreserveSchemaKeyword(c, &common.Unmodeled, s, keyword, ir.ReasonNoIRHome, pointer+ids.Ptr(keyword))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s on a type declaration binds a use of the type, not the type; it is kept "+
				"verbatim under Unmodeled and applied to a referencing property, header or "+
				"parameter wherever that carrier has a field for it", keyword))
	}
	return diags
}

// schemaConstraints reads the value constraints of a schema and stamps each
// constraint diagnostic with pointer's provenance. It returns nil only when
// there is no schema to read. It is the shared path for every alias a body
// reduces to — a named component (lowerComponentSchema) and a $ref-hoisted
// internal sub-schema (hoistSubSchema) — so a scalar that aliases a shared
// primitive never drops the constraints it carried, including a bound written
// beside a $ref, which constrains the position it is written at.
func schemaConstraints(c lowering.Ctx, s *oas3.Schema, pointer string) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	cons, diags := annotation.Constraints(s, c.ExclusiveBoundIsBoolean())
	return cons, StampConstraintDiags(c, diags, pointer)
}

// StampConstraintDiags gives every constraint diagnostic the provenance of the
// pointer that read the schema, which is what makes two reads of one sub-schema
// — its owning property and a $ref that hoists it — identical and so deduped by
// Diags.Append rather than reported twice.
func StampConstraintDiags(c lowering.Ctx, diags []ir.Diagnostic, pointer string) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance = c.ProvenanceAt(pointer)
	}
	return diags
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. Any value
// constraints the schema carried are attached so a scalar component never drops
// them.
func internAlias(c lowering.Ctx, ts *compile.Types, pointer, hint string,
	target ir.TypeRef, constraints *ir.Constraints,
) ir.TypeID {
	return internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := target
		return &ir.Scalar{TypeCommon: common, Base: &base, Constraints: constraints}
	})

}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. It is shared by Ref and by sub-schema hoisting
// (resolveSchemaRef), which both reach a body only after peeling off any
// leading $ref.
func schemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	target, diags := lowerSchemaBody(c, ts, anchors, depth, schema, pointer, hint)
	ref, homeDiags := homeDeclaration(c, ts, anchors, schema, target, pointer, hint, home)
	return ref, append(diags, homeDiags...)
}

// hoistDeclarationHome gives a declaration a node of its own when its lowering
// left it none and it wrote something only a node can hold. A body reducing to
// a shared primitive (or to another type's ID) is what leaves a pointer
// unowned, and a shared node must never carry one declaration's annotations —
// so without this the docs, x-*, xml, examples, validation-only keywords and
// value constraints written at that position are dropped, silently and
// including the constraints an emitter needs to generate a correct validator
// (GitHub #116).
//
// Ownership therefore follows what a declaration says, not what its body
// happened to lower to. A schema that declares none of it keeps resolving
// straight to the shared node: owning one would add nothing and give every
// bare `items: {type: string}` an anonymous node for nothing.
//
// A declaration that has a home already resolves to it instead of hoisting a
// second one. Usually that home is the node its own body interned, but a $ref
// naming an inline position hoists the position's home before the position
// itself is reached (resolveSchemaRef), and taking the body's target there
// would leave the annotations on a node only the $ref can see — the
// declaration-order dependence ir-design §4.3 rules out. The lookup stays
// behind the gate above so a position that declares nothing keeps resolving
// straight to its target however many references hoisted an alias over it.
func hoistDeclarationHome(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, ref ir.TypeRef, pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	if home != annotation.HomeOwnNode || s == nil || !declaresPositionScoped(s) {
		return ref, nil
	}
	if id, owned := ts.Lookup(pointer); owned {
		return ir.TypeRef{Target: id, Nullable: ref.Nullable}, nil
	}
	cons, diags := schemaConstraints(c, s, pointer)
	id := internAlias(c, ts, pointer, hint, ref, cons)
	return ir.TypeRef{Target: id, Nullable: ref.Nullable}, diags
}

// declaresPositionScoped reports whether s writes anything that binds the
// position it is written at rather than the shape it lowers to — an annotation
// attachDeclaredAnnotations records, a value constraint internAlias carries, or
// a keyword recordDeclarationResidue keeps. It is the gate on hoisting an alias,
// so it must stay in step with what those actually keep: a keyword listed here
// but stored nowhere would hoist an empty node, and one stored but missing here
// would still be dropped.
func declaresPositionScoped(s *oas3.Schema) bool {
	return declaresAnnotations(s) || declaresValueConstraints(s) ||
		declaresValidationOnly(s) || annotation.DeclaresAny(s, residueKeywords) ||
		declaresContentVocabulary(s) || declaresDynamicRef(s) ||
		annotation.DeclaresAny(s, annotation.DialectKeywords)
}

// declaresAnnotations reports whether s carries documentation, deprecation, XML
// hints, vendor extensions, or examples — the five TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *oas3.Schema) bool {
	if s.GetTitle() != "" || s.GetDescription() != "" || s.GetExternalDocs() != nil {
		return true
	}
	if annotation.EffectiveDeprecated(s, nil) || s.GetXML() != nil {
		return true
	}
	if ext := s.GetExtensions(); ext != nil && ext.Len() > 0 {
		return true
	}
	return s.GetExample() != nil || len(s.GetExamples()) > 0
}

// declaresValueConstraints reports whether s sets any keyword
// annotation.Constraints reads. It does not call it: that reports a malformed
// bound, and a predicate must not emit diagnostics. The three numeric bounds
// are detected on their raw nodes for the same reason numericBounds reads them
// there — a magnitude beyond float64 leaves the model field nil while the
// keyword is plainly written.
func declaresValueConstraints(s *oas3.Schema) bool {
	for _, keyword := range []string{"minimum", "maximum", "multipleOf"} {
		if annotation.RawPropertyNode(s, keyword) != nil {
			return true
		}
	}
	if s.GetExclusiveMinimum() != nil || s.GetExclusiveMaximum() != nil {
		return true
	}
	if s.MinLength != nil || s.MaxLength != nil || s.GetPattern() != "" {
		return true
	}
	return s.MinProperties != nil || s.MaxProperties != nil
}

// declaresValidationOnly reports whether s writes a §4.7 validation-only
// keyword that preserveKeyword keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties is excluded on purpose: fillAdditional lowers it into
// the model's openness, so it is structure rather than a preserved keyword.
func declaresValidationOnly(s *oas3.Schema) bool {
	if s.GetNot() != nil || s.GetContains() != nil || s.GetPropertyNames() != nil {
		return true
	}
	if s.GetIf() != nil || s.GetThen() != nil || s.GetElse() != nil {
		return true
	}
	if s.GetMinContains() != nil || s.GetMaxContains() != nil {
		return true
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		return true
	}
	if annotation.RawPropertyNode(s, "dependentRequired") != nil {
		return true
	}
	up := s.GetUnevaluatedProperties()
	return s.GetUnevaluatedItems() != nil || (up != nil && !annotation.IsFalseSchema(up))
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
func lowerSchemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	target, _, expanded, diags := dynamicExpansion(c, anchors, schema, pointer)
	if expanded {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DynamicRefExpanded, pointer+ids.Ptr("$dynamicRef"),
			"$dynamicRef expanded to %q, the one matching $dynamicAnchor in this document", target))
		return ir.TypeRef{Target: target, Nullable: schemaAdmitsNull(schema)}, diags
	}
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			id, unionDiags := lowerCoDeclaredUnion(c, ts, anchors, depth, schema, pointer, hint)
			return ir.TypeRef{Target: id, Nullable: schemaAdmitsNull(schema)}, append(diags, unionDiags...)
		}
		ref, unionDiags := lowerOneOfAnyOf(c, ts, anchors, depth, schema, pointer, hint)
		return ref, append(diags, unionDiags...)
	}
	id, bodyDiags := lower(c, ts, anchors, depth, schema, pointer, hint)
	return ir.TypeRef{Target: id, Nullable: schemaAdmitsNull(schema)}, append(diags, bodyDiags...)
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...) or
// a keyword that narrows an instance without declaring one. When it does, the
// union alone cannot represent the schema, so the sibling body must be lowered
// too rather than dropped (invariant 2, ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	return narrowsInstance(s) || declaresShape(s)
}

// narrowsInstance reports whether s writes a keyword that constrains an instance
// without declaring a shape to build: `required`, `items`, `prefixItems`.
//
// These sit on the narrowing side of declaresShape's line, and that line is about
// what to *build*. Wherever the question is instead whether a lowering can carry
// what the position wrote, the distinction does not apply: an ir.Union has no
// element type, so `{oneOf: [...], items: {...}}` loses the `items` as surely as
// it would lose a co-declared property set.
func narrowsInstance(s *oas3.Schema) bool {
	return len(s.GetRequired()) > 0 || s.GetItems() != nil || len(s.GetPrefixItems()) > 0
}

// declaresShape reports whether a schema declares data shape — a type, a
// property set, a value set, composition, or a catch-all — rather than only
// narrowing what an already-declared shape accepts. The narrowing keywords are
// narrowsInstance's, which is why the callers combine the two (ir-design §4.7).
func declaresShape(s *oas3.Schema) bool {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	if s.GetConst() != nil || enumWritten(s) || len(s.GetAllOf()) > 0 {
		return true
	}
	if s.GetAdditionalProperties() != nil {
		return true
	}
	if pp := s.GetPatternProperties(); pp != nil && pp.Len() > 0 {
		return true
	}
	return len(effectiveTypes(s)) > 0
}

// lowerBesideUnmodeledUnion lowers the structural body of a schema that
// co-declares oneOf/anyOf and keeps the union verbatim beside it, so neither the
// structural shape nor the union is dropped. reason says which kind of union it
// is and why says what stopped a classified lowering; classifyUnionSiblings
// picks both.
func lowerBesideUnmodeledUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string, reason ir.UnmodeledReason, why string) (ir.TypeID, []ir.Diagnostic) {
	inner, diags := lower(c, ts, anchors, depth, s, pointer, hint)
	owner := inner
	if got, _ := ts.Lookup(pointer); got != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		owner = internAlias(c, ts, pointer, hint, ir.TypeRef{Target: inner}, nil)
	}
	return owner, append(diags, preserveUnionSiblings(c, ts, owner, s, pointer, reason, why)...)
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins §4.7's keyword family and is reported
// with it; anything else is a §4.8 degradation and says so.
func preserveUnionSiblings(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer string, reason ir.UnmodeledReason, why string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	kept := false
	for _, kw := range []string{"oneOf", "anyOf"} {
		raw, err := annotation.RawFromNode(annotation.RawPropertyNode(s, kw))
		if err != nil {
			diags = append(diags, annotation.UnpreservableDiag("openapi:"+kw, pointer+ids.Ptr(kw), c.SrcIndex, err))
			continue
		}
		if reason == ir.ReasonValidationOnly {
			diags = append(diags, preserveKeyword(c, &common.Unmodeled, "openapi:"+kw, raw,
				pointer, pointer+ids.Ptr(kw), kw)...)
			continue
		}
		Preserve(c, &common.Unmodeled, "openapi:"+kw, raw, reason, pointer+ids.Ptr(kw))
		kept = kept || len(raw) > 0
	}
	if reason == ir.ReasonValidationOnly || !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"oneOf/anyOf co-declared with structural keywords intersects with them, and %s; union branches kept verbatim under Unmodeled", why))
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing), returning the interned ID and the one info diagnostic that
// announces it.
func falseSchema(c lowering.Ctx, ts *compile.Types, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model"))
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}
	})
	return id, diags
}

// familyOrder is the order lower() tries the keyword families that outrank the
// structural type, and dispatchOf is the sole reader of it. First match wins, so
// a schema declaring more than one of them lowers as the first and passes over
// the rest — which is why the two are derived together rather than separately.
var familyOrder = []string{"const", "enum", "allOf"}

// declaresFamily reports whether s declares the named family. It is the single
// definition of each family's guard: lower() lowers what dispatchOf elects and
// recordSkippedFamilies keeps what the same walk passed over, so the winner and
// the losers can never be read off two tests that disagree.
// A name it does not know declares nothing, rather than falling through to
// another family's guard: a familyOrder entry added without a case here would
// otherwise report whichever guard the default happened to hold, electing that
// name on schemas that never wrote it.
func declaresFamily(s *oas3.Schema, family string) bool {
	switch family {
	case "const":
		return s.GetConst() != nil
	case "enum":
		return enumWritten(s)
	case "allOf":
		return len(s.GetAllOf()) > 0
	default:
		return false
	}
}

// enumWritten reports whether s writes `enum` at all, an empty member list
// included. The three predicates that read the keyword — this family guard,
// declaresShape and composesAsModel — each spelled it `len(...) > 0`, which
// cannot tell `enum: []` from no enum keyword at all, so the degenerate spelling
// was elected by none of them and preserved by none of them either. The position
// then widened to whatever its siblings admitted, in silence (GitHub #278).
//
// `enum: []` is legal JSON Schema and it fixes the value space to the empty set,
// so it declares one exactly as a populated list does. Nilness is the
// distinction the parser keeps: an absent keyword leaves the field nil, an empty
// list leaves it non-nil and empty. A member list the model layer could not
// parse at all reads as absent here, which is a document the loader already
// refuses.
func enumWritten(s *oas3.Schema) bool {
	return s.GetEnum() != nil
}

// dispatch records how lower() resolved a schema's competing keyword families:
// the one it lowered, and the ones it passed over. won is "" when the schema
// declares none of them and the type set decides the lowering instead.
type dispatch struct {
	won     string
	skipped []string
}

// dispatchOf elects the family lower() lowers and collects the rest. A schema
// declaring none leaves won empty and skipped nil, so the type-set arms preserve
// nothing.
//
// familyOrder, declaresFamily and lower()'s switch have to name the same three,
// and only two of the three pairings fail safely. A name familyOrder lists that
// declaresFamily does not know is never declared, so it is never elected; a name
// that loses the election reaches recordSkippedFamilies whether or not lower()
// can lower it. But a name that *wins* an election lower() has no arm for is
// neither lowered nor skipped: the switch falls through to the type-set arms and
// the keyword is dropped in silence — the very failure GitHub #35 is about.
// Adding a family means adding all three.
func dispatchOf(s *oas3.Schema) dispatch {
	var d dispatch
	for _, family := range familyOrder {
		if !declaresFamily(s, family) {
			continue
		}
		if d.won == "" {
			d.won = family
			continue
		}
		d.skipped = append(d.skipped, family)
	}
	return d
}

// lower interns the inline schema at pointer and returns its TypeID. Value
// constraints (const, enum) and allOf composition take precedence over the
// structural type; otherwise it dispatches on the effective (null-stripped)
// type set. const hoists through hoistLiteral — the same primitive that
// hoists each individual member of a heterogeneous enum (enumAsUnion) — since
// a bare `const` schema is exactly a Literal at its own pointer.
//
// The families are conjoined where a schema writes more than one, so electing a
// winner is a §4.8 degradation rather than a reading of the source: the ones
// passed over are kept verbatim beside it (recordSkippedFamilies), never
// dropped.
//
// What that does not cover is a keyword the *elected* lowering never reads —
// `type: string` beside an allOf, `format` beside a const. Deciding those needs a
// per-winner rule rather than a keyword list, since `allOf` beside `type: object`
// is the common case and loses nothing, so it is left open at GitHub #268 rather
// than settled here.
func lower(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	d := dispatchOf(s)
	unhomed := func(id ir.TypeID, diags []ir.Diagnostic) (ir.TypeID, []ir.Diagnostic) {
		owner, ownDiags := preserveUnhomedKeywords(c, ts, s, pointer, hint, id, d)
		return owner, append(diags, ownDiags...)
	}
	switch d.won {
	case "const":
		return unhomed(hoistLiteral(c, ts, s.GetConst(), pointer, hint))
	case "enum":
		return unhomed(lowerEnum(c, ts, s, pointer, hint))
	case "allOf":
		return unhomed(lowerAllOf(c, ts, anchors, depth, s, pointer, hint))
	}
	// won is "" — the schema declares no family, so the type set decides.
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		// A multi-typed schema lowers one variant per declared type, each from
		// this same schema, so an applicator with no home in one variant has one
		// in another: `{type: [string, object], properties: {...}}` puts the
		// property set on the object variant. The homes here are the variants',
		// not the Union's, so the applicator check does not apply.
		return lowerUnion(c, ts, anchors, depth, s, pointer, hint, types)
	case len(types) == 1:
		return unhomed(lowerTyped(c, ts, anchors, depth, s, pointer, hint, types[0]))
	default:
		return unhomed(lowerUntyped(c, ts, anchors, depth, s, pointer, hint))
	}
}

// shapeApplicators are the JSON Schema applicator keywords that constrain
// instance shape. Each has exactly one IR home: a Model's property set, openness
// and pattern bindings, or a List's element and a Tuple's positions.
//
// Everything else a schema can write is captured elsewhere — annotations by
// attachDeclaredAnnotations, bounds by schemaConstraints, the §4.7
// validation-only family by preserveKeyword, the content vocabulary by
// recordUnplacedContent, use-site keywords by recordResidue. The keywords listed
// here had no such recorder, so a position writing one that its lowering could
// not consume dropped it in silence.
var shapeApplicators = []string{
	"properties", "patternProperties", "additionalProperties", "required",
	"items", "prefixItems",
}

// applicatorHome reports whether the node a position lowered to has a field that
// carries the named applicator. It asks the node rather than re-deriving lower()'s
// dispatch, so the two cannot drift apart (the rule recordUnplacedContent states).
func applicatorHome(td ir.TypeDef, keyword string) bool {
	switch td.(type) {
	case *ir.Model:
		switch keyword {
		case "properties", "patternProperties", "additionalProperties", "required":
			return true
		default:
			return false
		}
	case *ir.List, *ir.Tuple:
		return keyword == "items" || keyword == "prefixItems"
	default:
		// An Enum, Literal, Scalar, Primitive or Union carries no property set
		// and no element type, so every applicator written beside one is homeless.
		return false
	}
}

// unhomedKeywords returns the keywords s declares that nothing reading this
// position can carry, in the order shapeApplicators lists them, with `format`
// last. It reads the raw nodes for the reason declaresValueConstraints does: a
// keyword the model layer failed to parse is still plainly written.
//
// `format` is homed by a declared type, not by a node kind: scalarTypeID pairs
// the two to select a primitive or hoist a Scalar carrying the encoding. Written
// with no type beside it, nothing reads it at all — which is the one case that
// can be decided here without asking how the pairing turned out.
func unhomedKeywords(s *oas3.Schema, td ir.TypeDef) []string {
	var out []string
	for _, keyword := range shapeApplicators {
		if annotation.RawPropertyNode(s, keyword) == nil || applicatorHome(td, keyword) {
			continue
		}
		out = append(out, keyword)
	}
	if len(effectiveTypes(s)) == 0 && annotation.RawPropertyNode(s, "format") != nil {
		out = append(out, "format")
	}
	return out
}

// preserveUnhomedKeywords keeps verbatim the shape applicators a position
// declared that the node it lowered to has nowhere to carry, and returns the ID
// the position resolves to afterwards.
//
// Two losses share this shape. A contradictory schema — `{type: string, enum:
// [a, b], properties: {f: ...}}` — cannot be both a two-member string enum and an
// object, so lowering to one half is a reasonable degradation, but §4.8 requires
// the other half to stay beside it rather than vanish; dropping it reported
// nothing and surfaced only downstream, as a multipart encoding key addressing a
// model the IR no longer held. And an applicator written with no `type` beside it
// still constrains an instance — `{items: {type: string}}` constrains every array
// — where the position lowers to the top type, which understates it entirely.
//
// Lowering an untyped applicator on its own (reading `items` as a list shape) is
// a §4 decision this compiler has not taken. Keeping the source is the honest
// alternative, and it leaves that decision open.
//
// What it does not do is stop a multipart body's encoding keys being minted from
// a property set the lowered node no longer holds, so pass.Validate still reports
// ir/encoding-key-unknown-property for such a body. That diagnostic is correct
// about what it checks — the document does carry a key addressing no property —
// and the reader now gets the cause beside the symptom rather than only the
// symptom. Whether a contradictory schema should mint those keys at all is a
// separate question about Content.Encoding, not about what is dropped here.
//
// A lowering that reduced to a shared node gets an alias of its own first: a
// shared primitive must never carry one declaration's keywords. The alias takes
// the position's constraints too, because owning the pointer is what stops
// hoistDeclarationHome attaching them afterwards.
//
// The families lower()'s election passed over ride the same path: they too were
// declared here, they too have no home on the node the election produced, and
// they too must not land on a shared one.
func preserveUnhomedKeywords(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer, hint string, id ir.TypeID, d dispatch) (ir.TypeID, []ir.Diagnostic) {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return id, diags
	}
	unhomed := unhomedKeywords(s, td)
	if len(unhomed) == 0 && len(d.skipped) == 0 {
		return id, diags
	}
	owner := id
	if got, _ := ts.Lookup(pointer); got != id {
		cons, consDiags := schemaConstraints(c, s, pointer)
		diags = append(diags, consDiags...)
		owner = internAlias(c, ts, pointer, hint, ir.TypeRef{Target: id}, cons)
	}
	diags = append(diags, recordUnhomedKeywords(c, ts, owner, s, unhomed, td.Kind(), pointer)...)
	return owner, append(diags, recordSkippedFamilies(c, ts, owner, s, d, pointer)...)
}

// recordSkippedFamilies keeps verbatim the keyword families lower()'s election
// passed over, and reports them once.
//
// JSON Schema conjoins keywords, so `{allOf: [{$ref: Base}], enum: [a, b]}` is a
// narrowing of Base rather than a malformed document, and `{const: a, enum: [a,
// b]}` is a redundant but legal restatement. The IR has no intersection
// combinator (ir-design §15), so only one of them can be the value — but the
// loser was dropped outright, taking the whole relationship to Base with it and
// reporting nothing at all. Keeping it beside the elected form is §4.8's rule for
// every other unrepresentable conjunction here (preserveUnionSiblings,
// buildTuple's open tuple).
//
// It reports the keywords it actually stored, never the ones it was handed, so
// the message cannot claim one that failed to convert and was reported
// unpreservable instead.
func recordSkippedFamilies(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, d dispatch, pointer string) []ir.Diagnostic {
	if len(d.skipped) == 0 {
		return nil
	}
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	kept := make([]string, 0, len(d.skipped))
	for _, keyword := range d.skipped {
		stored, storedDiags := PreserveSchemaKeyword(c, &common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer+ids.Ptr(keyword))
		diags = append(diags, storedDiags...)
		if stored {
			kept = append(kept, keyword)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	skipped := strings.Join(kept, " and ")
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position declares %s beside its %s, and JSON Schema conjoins them where only "+
			"one can be the value; it lowered as the %s, with %s kept verbatim under Unmodeled",
		skipped, d.won, d.won, skipped))
}

// recordUnhomedKeywords stores each unhomed applicator on the owning node and
// reports them once, naming the form the lowering took so a reader is told which
// half of a contradictory schema the IR describes.
func recordUnhomedKeywords(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, unhomed []string, kind ir.TypeKind, pointer string) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	common := td.Common()
	// Only the keywords actually written are named, so the message cannot claim
	// one that failed to convert and was reported unpreservable instead.
	kept := make([]string, 0, len(unhomed))
	for _, keyword := range unhomed {
		ok, keptDiags := PreserveSchemaKeyword(c, &common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer+ids.Ptr(keyword))
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, keyword)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position lowered to a node of kind %q, which has no home for %s declared beside it; kept verbatim under Unmodeled",
		kind, strings.Join(kept, ", ")))
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func lowerTyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string, st oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	switch st {
	case oas3.SchemaTypeObject:
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	case oas3.SchemaTypeArray:
		return lowerArray(c, ts, anchors, depth, s, pointer, hint)
	default:
		return scalarTypeID(c, ts, s, st, pointer, hint)
	}
}

// lowerUntyped handles a schema with no declared type: a property set makes it a
// model; enum/const and composition are lowered by later passes; anything else
// is schemaless.
func lowerUntyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	}
	return ts.PrimID(ir.PrimAny), nil
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// exclusive, untagged union with one variant per declared type.
func lowerUnion(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string, types []oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ids.Ptr("type", strconv.Itoa(i))
			vid, vDiags := lowerTyped(c, ts, anchors, depth, s, vptr, hint, st)
			diags = append(diags, vDiags...)
			variants = append(variants, ir.Variant{
				Name: compile.NamingHint(string(st)),
				Type: ir.TypeRef{Target: vid},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  true,
		}
	})
	return id, diags
}

// lowerModel hoists an object schema as a Model. This task lowers only the
// property shape (name, type, required) and the property-set cardinality; a
// later pass fills the rest. It reads no annotations: attachDeclaredAnnotations
// does that for every destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func lowerModel(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		cons, consDiags := schemaConstraints(c, s, pointer)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, fillModelProperties(c, ts, anchors, depth, m, s, pointer)...)
		diags = append(diags, fillAdditional(c, ts, anchors, depth, m, s, pointer, hint)...)
		d, discDiags := lowerDiscriminator(c, ts, s, m, pointer)
		diags = append(diags, discDiags...)
		if d != nil {
			m.Discriminator = d
		}
		return m
	})
	return id, diags
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
func fillModelProperties(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, m *ir.Model, s *oas3.Schema, pointer string) []ir.Diagnostic {
	props := s.GetProperties()
	if props == nil {
		return nil
	}
	var diags []ir.Diagnostic
	required := requiredSet(s.GetRequired())
	byWire := merge.WireNameIndex(m.Properties)
	for name, js := range props.All() {
		ppointer := pointer + ids.Ptr("properties", name)
		ref, refDiags := CarriedRef(c, ts, anchors, depth, js, ppointer, name)
		diags = append(diags, refDiags...)
		p := ir.Property{
			ID:         ids.Prop(ppointer),
			Name:       compile.NamingFor(name),
			WireName:   name,
			Type:       ref,
			Required:   required[name],
			Provenance: c.ProvenanceAt(ppointer),
		}
		diags = append(diags, FillPropertyDetail(c, ts, anchors, &p, js, ppointer)...)
		var mergeDiags []ir.Diagnostic
		mg := merger(c, ts, &mergeDiags)
		mg.MergeProperty(m, byWire, p, ppointer)
		diags = append(diags, mergeDiags...)
	}
	return diags
}

// FillPropertyDetail enriches a property from its schema: the property-scoped
// facts a type node has no field for (default, visibility, secrecy,
// constraints), then the declaration's annotations. Annotations present at a
// $ref use-site override the target's (ir-design §14).
//
// Constraints stay unconditional: ir.Property is the only home every property
// has. Some nodes a property's schema can hoist carry the same bounds (a Model,
// and the Scalar the content vocabulary hoists) and some carry none (the Scalar
// a byte or unknown format hoists, or no node at all), so reading the node
// instead would drop them wherever it carries none. Restating them is the safe
// half of that trade.
func FillPropertyDetail(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer string) []ir.Diagnostic {
	ref := js.GetSchema()
	if ref == nil {
		return nil
	}
	tgt := resolve.TargetSchema(js, ref)
	diags := fillPropertyDefault(c, p, ref, tgt, pointer)
	if ref.GetFormat() == "password" {
		p.Secret = true
	}
	p.Visibility = annotation.EffectiveVisibility(ref, tgt)
	diags = append(diags, fillPropertyConstraints(c, p, ref, pointer)...)
	return append(diags, fillPropertyAnnotations(c, ts, anchors, p, ref, tgt, pointer)...)
}

// fillPropertyAnnotations records the annotations the property's schema
// declares — docs, deprecation, XML hints, examples, vendor extensions and
// validation-only keywords — on the property, but only when that schema lowered
// to no node of its own to hold them. A schema that reduced to a shared
// primitive has nowhere else to put them, and the shared primitive must never
// carry one declaration's annotations; a schema that lowered to a node keeps
// them there (attachDeclaredAnnotations). The property never doubles that node,
// so the two can never drift apart.
//
// A node another *reference* hoisted at the property's pointer is not that
// node: `$ref: '#/…/properties/foo'` names the property's schema, so the node
// it resolves to carries what that schema declares, and the property carries it
// too — two IR entities reflecting one source schema, not one entity with two
// homes. Which of them is reached first must not decide either (GitHub #116).
//
// A $ref position never hoists a node, which is what gives an if/then/else, a
// bound or a description written beside a *property's* $ref somewhere to land
// (GitHub #114).
func fillPropertyAnnotations(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, p *ir.Property, ref, tgt *oas3.Schema, pointer string) []ir.Diagnostic {
	if LoweredToOwnNode(ts, pointer, p.Type) {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Reference, Node: ref, Referent: tgt}, pointer, c.SrcIndex)

	p.Docs = a.Docs
	if a.Deprecated {
		p.Deprecation = &ir.Deprecation{}
	}
	if a.XML != nil {
		p.XML = a.XML
	}
	if len(a.Examples) > 0 {
		p.Examples = a.Examples
	}
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, a.Unmodeled)
	// nil node: this arm runs only when the schema lowered to no node of its own,
	// so nothing here can be carrying an Encoding.
	diags = append(diags, recordUnplacedContent(c, &p.Unmodeled, ref, nil, pointer)...)
	return append(diags, recordUnexpandedDynamicRef(c, anchors, &p.Unmodeled, ref, pointer)...)
}

// LoweredToOwnNode reports whether the declaration at pointer lowered to a type
// node of its own — the node attachDeclaredAnnotations then fills, leaving its
// carrier nothing to hold.
//
// It asks whether the node interned at pointer is the one the declaration
// lowered to, not merely whether a node is interned there. A $ref naming an
// inline position hoists that position's home for its own use, in either
// declaration order, and a carrier reading the registry alone would keep its
// schema's annotations only when it happened to lower first.
func LoweredToOwnNode(ts *compile.Types, pointer string, t ir.TypeRef) bool {
	id, owned := ts.Lookup(pointer)
	return owned && id == t.Target
}

// fillPropertyDefault sets the property default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic.
func fillPropertyDefault(c lowering.Ctx, p *ir.Property, ref, tgt *oas3.Schema, pointer string) []ir.Diagnostic {
	node := ref.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return nil
	}
	v, err := value.FromNode(node)
	if err != nil {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
			"default: %s", err.Error())}
	}
	p.Default = &v
	return nil
}

// fillPropertyConstraints attaches the property's scalar constraints and stamps
// each constraint diagnostic with the property's provenance.
func fillPropertyConstraints(c lowering.Ctx, p *ir.Property, ref *oas3.Schema, pointer string) []ir.Diagnostic {
	cons, diags := annotation.Constraints(ref, c.ExclusiveBoundIsBoolean())
	if cons != nil {
		p.Constraints = cons
	}
	return StampConstraintDiags(c, diags, pointer)
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns — the one structural home a declaration's annotations have
// (ir.TypeCommon, embedded in every type node).
//
// It is the sole reader of declaration-scoped annotations, and it runs *above*
// lower()'s dispatch: every declaration reaches it through schemaBody or an
// alias fallback, whatever its body turns out to lower to. That is what keeps
// a new lowering destination from silently dropping annotations — a
// destination never reads them, so it cannot forget to (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail), and the
// shared primitive must never carry a per-declaration annotation. Ownership is
// checked before conversion because the callers cover a pointer in either
// order, and only the one that finds a node may emit conversion diagnostics.
func attachDeclaredAnnotations(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, pointer string) []ir.Diagnostic {
	// NodeAt rather than Lookup-then-registeredNode: the coordinate and its node
	// are recorded together, so there is no state where the first resolves and
	// the second does not, and a branch for one could never be reached.
	td, ok := ts.NodeAt(pointer)
	if !ok {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Declaration, Node: s}, pointer, c.SrcIndex)

	common := td.Common()
	common.Docs = a.Docs
	if a.Deprecated {
		common.Deprecation = &ir.Deprecation{}
	}
	if a.XML != nil {
		common.XML = a.XML
	}
	common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, a.Unmodeled)
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	diags = append(diags, recordUnplacedContent(c, &common.Unmodeled, s, td, pointer)...)
	return append(diags, recordUnexpandedDynamicRef(c, anchors, &common.Unmodeled, s, pointer)...)
}

// fillAdditional lowers additionalProperties, patternProperties, and
// unevaluatedProperties into the model's openness and catch-all shape.
func fillAdditional(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, m *ir.Model, s *oas3.Schema, pointer, hint string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ap := s.GetAdditionalProperties()
	switch {
	case annotation.IsFalseSchema(ap):
		m.Additional = ir.AdditionalClosed
	case ap != nil && !ap.IsBool():
		ref, refDiags := Ref(c, ts, anchors, depth, ap, pointer+ids.Ptr("additionalProperties"), compile.SubHint(hint, "value"))
		diags = append(diags, refDiags...)
		m.AdditionalProps = &ir.AdditionalProps{Value: ref}
	}
	patterns, patternDiags := patternProps(c, ts, anchors, depth, s, pointer, hint)
	diags = append(diags, patternDiags...)
	if len(patterns) > 0 {
		if m.AdditionalProps == nil {
			m.AdditionalProps = &ir.AdditionalProps{Value: ts.PrimRef(ir.PrimAny)}
		}
		m.AdditionalProps.Patterns = patterns
	}
	if annotation.IsFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
	return diags
}

// patternProps lowers patternProperties into pattern/value bindings in source
// order.
func patternProps(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) ([]ir.PatternProps, []ir.Diagnostic) {
	pp := s.GetPatternProperties()
	if pp == nil || pp.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.PatternProps, 0, pp.Len())
	for pattern, js := range pp.All() {
		ref, refDiags := Ref(c, ts, anchors, depth, js, pointer+ids.Ptr("patternProperties", pattern), compile.SubHint(hint, "pattern"))
		diags = append(diags, refDiags...)
		out = append(out, ir.PatternProps{Pattern: pattern, Value: ref})
	}
	return out, diags
}

// buildTuple lowers prefixItems into a Tuple. A trailing `items` schema makes
// the source an *open* tuple — a fixed positional head plus a homogeneous tail
// — and the IR has no combinator for that: Tuple is fixed-arity, List is
// homogeneous, and there is no node that is both. The head lowers to a Tuple,
// which is the documented weaker shape, and the tail is kept beside it so the
// arity the Tuple now asserts falsely stays recoverable (ir-design §4.8).
func buildTuple(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, common ir.TypeCommon, pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) (ir.TypeDef, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		ref, refDiags := Ref(c, ts, anchors, depth, ps, pointer+ids.Ptr("prefixItems", strconv.Itoa(i)), compile.SubHint(hint, strconv.Itoa(i)))
		diags = append(diags, refDiags...)
		elems = append(elems, ref)
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems}
	if s.GetItems() == nil {
		return t, diags
	}
	kept, keptDiags := PreserveNode(c, &t.Unmodeled, "openapi:items-after-prefix", annotation.RawPropertyNode(s, "items"), ir.ReasonDegradedLowering, pointer+ids.Ptr("items"))
	diags = append(diags, keptDiags...)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"items after prefixItems is an open tuple; lowered as a fixed-arity Tuple with the tail kept under Unmodeled"))
	}
	return t, diags
}

// scalarTypeID maps a scalar (type, format) pair to a TypeID via formatTable: a
// known pairing interns the shared primitive; byte, an unknown format, and the
// 2020-12 content vocabulary each hoist a named Scalar wrapping the base
// primitive with an Encoding, so what the position wrote never leaks onto the
// shared primitive every other declaration of that type also resolves to.
func scalarTypeID(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, st oas3.SchemaType, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	format := s.GetFormat()
	if st == oas3.SchemaTypeString && format == "byte" {
		return hoistByteScalar(c, ts, s, pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	prim, known := formatTable[key]
	if !known {
		return hoistFormatScalar(c, ts, s, baseForType(st), format, pointer, hint)
	}
	if !declaresContent(s) {
		return ts.PrimID(prim), nil
	}
	return hoistContentScalar(c, ts, s, prim, pointer, hint)
}

// hoistByteScalar hoists a base64-encoded byte scalar (string+byte).
//
// Every hoister here carries the position's value constraints itself, because
// owning a node is what stops hoistDeclarationHome hoisting the alias that would
// otherwise carry them: that fallback resolves to whatever node the pointer
// already owns and returns early. A scalar that hoisted because it wrote a
// format must not lose the bounds it wrote beside it (invariant 2).
func hoistByteScalar(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := ts.PrimRef(ir.PrimBytes)
		wire := ts.PrimRef(ir.PrimString)
		enc, encDiags := scalarEncoding(c, s, "base64", &common, pointer)
		diags = append(diags, encDiags...)
		enc.WireType = &wire
		cons, consDiags := schemaConstraints(c, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &base,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// hoistFormatScalar hoists a scalar over base carrying an unknown format as its
// encoding name, preserving the format losslessly.
func hoistFormatScalar(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, base ir.PrimKind, format, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := ts.PrimRef(base)
		enc, encDiags := scalarEncoding(c, s, format, &common, pointer)
		diags = append(diags, encDiags...)
		cons, consDiags := schemaConstraints(c, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &baseRef,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// hoistContentScalar hoists a scalar over the shared primitive a known
// (type, format) pair maps to, giving the content vocabulary written here a node
// of its own to sit on. It carries the position's value constraints for the
// reason hoistByteScalar records.
func hoistContentScalar(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, prim ir.PrimKind, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := ts.PrimRef(prim)
		enc, encDiags := scalarEncoding(c, s, "", &common, pointer)
		diags = append(diags, encDiags...)
		cons, consDiags := schemaConstraints(c, s, pointer)
		diags = append(diags, consDiags...)
		return &ir.Scalar{
			TypeCommon:  common,
			Base:        &base,
			Encoding:    enc,
			Constraints: cons,
		}
	})
	return id, diags
}

// scalarEncoding builds the Encoding a scalar position declares: the 2020-12
// content vocabulary over the OpenAPI `format` spelling of the same thing.
// formatName is the encoding name the format contributes — "base64" for
// format: byte, an unrecognized format verbatim, "" when the pairing is already
// captured by the primitive kind.
//
// contentEncoding wins Encoding.Name: it is the standard keyword, where a format
// the IR could not place is only parked there. Encoding holds one name, so a
// format that named a *different* encoding is kept verbatim on c rather than
// overwritten away.
func scalarEncoding(c lowering.Ctx, s *oas3.Schema, formatName string, common *ir.TypeCommon, pointer string) (*ir.Encoding, []ir.Diagnostic) {
	enc := &ir.Encoding{Name: formatName, MediaType: s.GetContentMediaType()}
	content := s.GetContentEncoding()
	if content == "" || content == formatName {
		return enc, nil
	}
	enc.Name = content
	if formatName == "" {
		return enc, nil
	}
	at := pointer + ids.Ptr("format")
	kept, diags := PreserveSchemaKeyword(c, &common.Unmodeled, s, "format", ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
				"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content))
	}
	return enc, diags
}

// contentKeywords are the content-vocabulary keywords that lower into
// ir.Encoding: contentEncoding names Encoding.Name and contentMediaType names
// Encoding.MediaType (ir/constraints.go, ir-design §5.3). contentSchema is not
// one of them — it is a schema rather than an encoding, and noIRHomeAt keeps it
// verbatim at every position.
var contentKeywords = []string{"contentEncoding", "contentMediaType"}

// declaresContent reports whether s writes a contentKeywords entry.
func declaresContent(s *oas3.Schema) bool {
	return s.GetContentEncoding() != "" || s.GetContentMediaType() != ""
}

// declaresContentVocabulary reports whether s writes any content-vocabulary
// keyword, so a position that wrote one owns a node to keep it on.
func declaresContentVocabulary(s *oas3.Schema) bool {
	return declaresContent(s) || s.GetContentSchema() != nil
}

// recordUnplacedContent keeps each content keyword verbatim on p, for a position
// whose lowering produced no ir.Encoding to hold it.
//
// A string position lowers them (scalarEncoding). An object, an array, a union,
// an alias over a $ref, or a schema with no declared type at all has no Encoding
// field — yet 2020-12 §8.3 gives these keywords meaning on whatever instance
// turns out to be a string, so they are kept rather than dropped. It asks the
// node the position actually lowered to instead of re-deriving lower()'s
// dispatch, so the two cannot drift apart.
func recordUnplacedContent(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, td ir.TypeDef, pointer string) []ir.Diagnostic {
	if !declaresContent(s) || scalarHasEncoding(td) {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range contentKeywords {
		kept, keptDiags := PreserveSchemaKeyword(c, p, s, keyword, ir.ReasonNoIRHome, pointer+ids.Ptr(keyword))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
			"%s encodes a string value and this position lowered to a shape with no "+
				"Encoding field; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// scalarHasEncoding reports whether td is a Scalar already carrying the
// position's Encoding. A nil td is the property-carrier case — no node, so no
// Encoding.
func scalarHasEncoding(td ir.TypeDef) bool {
	sc, ok := td.(*ir.Scalar)
	return ok && sc.Encoding != nil
}

// maxDynamicAnchorDepth bounds the raw-source walk that indexes $dynamicAnchor
// declarations (styleguide bounded-recursion rule). Document trees nest far
// shallower — a schema under a media type under a response is about ten levels —
// so the cap only ever fires on a pathological structure.
const maxDynamicAnchorDepth = 512

// maxDynamicAnchorNodes bounds how many nodes that walk visits in total. Depth
// stopped bounding it once the walk began following YAML aliases: an alias graph
// is a DAG, so a shallow document can present far more paths than it has nodes.
// Both bounds report the same incomplete-index warning when they fire.
const maxDynamicAnchorNodes = 1 << 20

// dynamicExpansion resolves the $dynamicRef s writes at pointer to the type it
// names, or reports why it is irreducible; a schema writing no $dynamicRef is
// irreducible with the cheapest possible reason, so no caller pays for the index.
//
// Dynamic scope is static per document (ir-design §4.7): the set of
// $dynamicAnchor declarations is fixed before evaluation, so a name declared
// exactly once is the only match any dynamic scope containing a match can hold
// — which makes it JSON Schema 2020-12 §8.2.3.2's "outermost scope with a
// matching anchor" whatever path evaluation took. That reasoning holds only
// while the whole document is one schema resource, which is why an $id anywhere
// on the path to either end makes the reference irreducible
// (dynamicChainVerdict): §8.2.1 says $id starts a new resource, and the IR
// honours no resource boundaries at all (annotation.DialectKeywords). An anchor
// reached through *different* dynamic scopes of one resource still expands, and
// correctly so: with no $id in play the outermost resource of every dynamic
// scope is the document itself.
//
// Only an anchor declared on a top-level component schema resolves: that is the
// one target whose TypeID is stable without reading the registry, so expansion
// cannot depend on which position happened to lower first (ir-design §4.3). An
// anchor deeper in the document is reported as irreducible rather than resolved
// order-dependently.
//
// A $dynamicRef co-declared with a $ref, a oneOf/anyOf, or a shape of its own is
// a conjunction of applicators the IR has no node for, so it is irreducible too
// — recordUnexpandedDynamicRef keeps the reference beside the shape that did
// lower. The one caller that expands and the one that preserves both decide
// through this function, so neither can act on a verdict the other did not
// reach.
func dynamicExpansion(c lowering.Ctx, anchors *AnchorIndex, s *oas3.Schema, pointer string) (target ir.TypeID, why string, ok bool, diags []ir.Diagnostic) {
	name, why, ok := dynamicRefName(s)
	if !ok {
		return "", why, false, nil
	}
	at, why, ok, diags := soleAnchorSite(c, anchors, name)
	if !ok {
		return "", why, false, diags
	}
	id, resolved, handled := c.RefScope().ComponentRef(at)
	if !handled || !resolved {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared at %q rather than on a component schema", name, at), false, diags
	}
	chainWhy, chainOK, chainDiags := dynamicChainVerdict(c, anchors, at, pointer)
	diags = append(diags, chainDiags...)
	if !chainOK {
		return "", chainWhy, false, diags
	}
	return id, "", true, diags
}

// dynamicRefName returns the $dynamicAnchor name the $dynamicRef s writes
// addresses, or the reason it addresses none. It stops short of the index, so
// the chain walk and the lowering path ask one function what a schema requests.
func dynamicRefName(s *oas3.Schema) (name, why string, ok bool) {
	node := annotation.RawPropertyNode(s, "$dynamicRef")
	if node == nil {
		return "", "no $dynamicRef is written here", false
	}
	if dynamicRefSiblings(s) {
		return "", "it is co-declared with another applicator, which the IR cannot intersect with it", false
	}
	if node.Kind != yaml.ScalarNode {
		return "", "its value is not a reference string", false
	}
	fragment, found := dynamicFragment(node.Value)
	if !found {
		return "", fmt.Sprintf("%q is not a plain same-document fragment", node.Value), false
	}
	return fragment, "", true
}

// dynamicRefSiblings reports whether s writes anything beside its $dynamicRef
// that the expansion would have to intersect with. JSON Schema conjoins
// keywords and the IR has no node that intersects a reference with a shape, so
// any of these makes the reference irreducible; expanding regardless would
// assert the target's shape and drop the sibling in silence.
//
// It is broader than declaresShape by narrowsInstance's keywords, for the reason
// that predicate records: an expansion takes the target whole, so a keyword it
// cannot carry is lost whether or not it declares a shape by itself.
func dynamicRefSiblings(s *oas3.Schema) bool {
	if s.Ref != nil || len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 {
		return true
	}
	return narrowsInstance(s) || declaresShape(s)
}

// soleAnchorSite returns the one pointer declaring the named $dynamicAnchor, or
// the reason no single declaration answers for it. Two declarations put the
// target back under the evaluation path's control, which no static lowering can
// resolve (ir-design §4.7).
//
// The index is a parameter because the memo is state the caller owns. Its
// diagnostics come back on every path, including the two answering "no single
// declaration": the walk bound they report is a fact about the document, not
// about this lookup's verdict, so a failed lookup must not swallow it.
func soleAnchorSite(c lowering.Ctx, anchors *AnchorIndex, name string) (at, why string, ok bool, diags []ir.Diagnostic) {
	sites, diags := anchors.sites(c, name)
	if len(sites) == 0 {
		return "", fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name), false, diags
	}
	if len(sites) > 1 {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the evaluation path",
			name, len(sites)), false, diags
	}
	return sites[0], "", true, diags
}

// dynamicChainVerdict reports whether the position at from may take the
// expansion landing on the anchor declared at at, by following the chain of
// expansions the target would take in turn.
//
// Two things end it. A chain that comes back to from would make the position's
// own type its base — a Scalar alias loop no emitter resolving Base can
// terminate on, and the shape refCycles refuses for every pure-$ref cycle.
// Every member of such a cycle reaches this verdict independently, so the whole
// cycle is preserved rather than one arbitrary edge of it. An $id on the path to
// any link means the two ends are in different schema resources, which §8.2.3.2
// degrades to a plain $ref against a base URI the IR does not model.
//
// The loop is bounded by seen: cur only ever takes values from the anchor
// index, and each turn either returns or adds one of them.
func dynamicChainVerdict(c lowering.Ctx, anchors *AnchorIndex, at, from string) (why string, ok bool, diags []ir.Diagnostic) {
	if declaresResourceIDAbove(c, from) {
		return resourceBoundaryWhy(from), false, nil
	}
	seen := map[string]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from), false, diags
		}
		if declaresResourceIDAbove(c, cur) {
			return resourceBoundaryWhy(cur), false, diags
		}
		seen[cur] = true
		next, hops, hopDiags := dynamicHop(c, anchors, cur)
		diags = append(diags, hopDiags...)
		if !hops {
			break
		}
		cur = next
	}
	return "", true, diags
}

// resourceBoundaryWhy words the one irreducible case that is about resources
// rather than shapes, for either end of a chain.
func resourceBoundaryWhy(at string) string {
	return fmt.Sprintf("an $id at or above %q starts a schema resource of its own, "+
		"and the IR resolves no resource base URIs", at)
}

// dynamicHop returns the anchor declaration the schema at a component pointer
// would itself expand to, when it writes an expandable $dynamicRef of its own.
// Anything else ends the chain: a position that lowers to a shape rather than to
// another reference cannot extend a cycle of references.
//
// The diagnostics are the index's, so the paths that return before consulting
// it carry none. Once it is consulted they come back even if the chain ends
// there: whether the hop happened and whether the index was complete are
// independent answers.
func dynamicHop(c lowering.Ctx, anchors *AnchorIndex, at string) (string, bool, []ir.Diagnostic) {
	s := componentSchemaAt(c, at)
	if s == nil {
		return "", false, nil
	}
	name, _, ok := dynamicRefName(s)
	if !ok {
		return "", false, nil
	}
	next, nextDiags := anchors.sites(c, name)
	if len(next) != 1 {
		return "", false, nextDiags
	}
	return next[0], true, nextDiags
}

// componentSchemaAt returns the schema body of the top-level component pointer
// addresses, and nil for any other pointer. Every accessor on the way is
// nil-safe and annotation.At reads a missing entry as "no body written", so the
// one guard is what distinguishes a component pointer from a deeper one.
func componentSchemaAt(c lowering.Ctx, pointer string) *oas3.Schema {
	name, ok := ids.ComponentSchemaName(pointer)
	if !ok {
		return nil
	}
	js, _ := c.Doc.GetComponents().GetSchemas().Get(name)
	return annotation.At(js).Node
}

// declaresResourceIDAbove reports whether any mapping on the path from the
// document root down to pointer writes $id, which §8.2.1 makes the root of a
// schema resource of its own.
//
// It reads every step of the path, not only the schema-shaped ones, so a
// property literally named "$id" reads as a resource boundary that is not
// there. That is the same direction the anchor index errs in: a false boundary
// costs an expansion that would have been safe, where a missed one mints a
// reference the IR cannot express.
func declaresResourceIDAbove(c lowering.Ctx, pointer string) bool {
	view := nodeview.New()
	cur := nodeview.DocumentRoot(nodeview.Deref(c.Doc.GetRootNode()))
	for seg := range strings.SplitSeq(pointer, "/") {
		if cur == nil {
			return false
		}
		if view.ChildByToken(cur, "$id") != nil {
			return true
		}
		if seg != "" { // every pointer starts with the empty segment
			cur = nodeview.Deref(view.ChildByToken(cur, ids.UnescapeSegment(seg)))
		}
	}
	return cur != nil && view.ChildByToken(cur, "$id") != nil
}

// dynamicFragment returns the plain fragment name a $dynamicRef addresses. Only
// the same-document `#name` spelling resolves here: a URI part names another
// resource (Milestone 1 interns only same-file targets) and a `#/…` pointer
// addresses a position rather than an anchor.
func dynamicFragment(ref string) (string, bool) {
	name, found := strings.CutPrefix(ref, "#")
	if !found || name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

// recordUnexpandedDynamicRef keeps a $dynamicRef verbatim on p when it was not
// expanded, so the reference survives the position lowering without it
// (ir-design §4.7's irreducible half). An expanded one is already the position's
// type and must not also be preserved — that would tell a consumer the compiler
// ignored it.
func recordUnexpandedDynamicRef(c lowering.Ctx, anchors *AnchorIndex, p *ir.Unmodeled, s *oas3.Schema, pointer string) []ir.Diagnostic {
	_, why, expanded, diags := dynamicExpansion(c, anchors, s, pointer)
	if expanded {
		return diags
	}
	at := pointer + ids.Ptr("$dynamicRef")
	kept, keptDiags := PreserveSchemaKeyword(c, p, s, "$dynamicRef", ir.ReasonDegradedLowering, at)
	diags = append(diags, keptDiags...)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"$dynamicRef was not expanded because %s; it is kept verbatim under Unmodeled", why))
	}
	return diags
}

// declaresDynamicRef reports whether s writes a $dynamicRef, so a position that
// wrote one owns a node to keep an unexpanded reference on.
func declaresDynamicRef(s *oas3.Schema) bool {
	return annotation.RawPropertyNode(s, "$dynamicRef") != nil
}

// AnchorIndex memoizes the document's $dynamicAnchor index.
//
// It is the one thing this compiler shares and mutates besides the interning
// table, which micro-compiler-design §4 did not expect: it holds that interning
// is "irreducibly shared and stateful; nothing else is". This is the exception,
// and it is a memo rather than an accumulator — the value it caches is a pure
// function of the document.
//
// It stays a memo rather than moving into the immutable context, which §4.1
// prescribes, for two measured reasons recorded there: building it emits a
// diagnostic, so deriving it at entry reports on documents that never write
// $dynamicRef; and the walk costs about 1.4% of a compile, which is a poor
// trade for a keyword almost no document uses.
type AnchorIndex struct {
	byName map[string][]string
}

// sites returns the pointers declaring the named $dynamicAnchor, building the
// index on first use and announcing once when a bound stopped the walk short.
//
// A truncated index can only undercount, and every verdict resting on it reads
// an undercount as "declared exactly once" — the one count that expands. So the
// warning is what tells a reader that an expansion reported below was decided
// against an index nothing verified, which is exactly what diag.CycleScanFailed
// says for the pre-parse scan.
func (a *AnchorIndex) sites(c lowering.Ctx, name string) ([]string, []ir.Diagnostic) {
	if a.byName != nil {
		return a.byName[name], nil
	}
	index, complete := dynamicAnchors(c.Doc.GetRootNode())
	a.byName = index
	if complete {
		return a.byName[name], nil
	}
	return a.byName[name], []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "",
		"the $dynamicAnchor index stopped at its walk bounds (%d levels, %d nodes); "+
			"a $dynamicRef expanded below is not verified to name the document's only anchor of its name",
		maxDynamicAnchorDepth, maxDynamicAnchorNodes)}
}

// dynamicAnchors indexes every $dynamicAnchor in the raw source by name, mapping
// it to the JSON pointers that declare it, in document order, and reports
// whether the walk ran to completion. It reads the raw tree because oas3.Schema
// has no field for the keyword at v1.24.0 — the reason nothing expanded a
// $dynamicRef before.
func dynamicAnchors(root *yaml.Node) (map[string][]string, bool) {
	w := newAnchorWalk(maxDynamicAnchorNodes)
	w.walk(root, "", 0)
	return w.out, !w.truncated && !w.view.Exhausted()
}

// anchorWalk is the state of one $dynamicAnchor index build: the source view,
// the index under construction, and what is left of the visit budget.
//
// It reads mappings through a nodeview.View, so a `<<` merge key and a YAML alias
// contribute the anchors they carry to every position that pulls them in —
// which is what the parser downstream sees, and what makes "declared exactly
// once" a count of what the document declares rather than of what it spells
// out. The count decides whether a $dynamicRef expands, so an anchor reached
// only through an alias must not be invisible to it.
type anchorWalk struct {
	view      *nodeview.View
	out       map[string][]string
	budget    int
	truncated bool
}

// newAnchorWalk returns a walk that will visit at most budget nodes.
func newAnchorWalk(budget int) *anchorWalk {
	return &anchorWalk{view: nodeview.New(), out: map[string][]string{}, budget: budget}
}

// walk indexes the anchors n declares, under the pointer of the mapping
// declaring each.
func (w *anchorWalk) walk(n *yaml.Node, pointer string, depth int) {
	n = nodeview.Deref(n)
	if n == nil || !w.charge(depth) {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			w.walk(c, pointer, depth+1)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			w.walk(c, pointer+ids.Ptr(strconv.Itoa(i)), depth+1)
		}
	case yaml.MappingNode:
		w.walkMapping(n, pointer, depth)
	default:
		// A scalar declares no anchor and has no children; an alias survived
		// nodeview.Deref only by dangling, so it has nothing to stand in for.
	}
}

// walkMapping reads one mapping's effective pairs: a $dynamicAnchor names this
// mapping, and every other value is walked in turn.
func (w *anchorWalk) walkMapping(n *yaml.Node, pointer string, depth int) {
	for _, p := range w.view.MappingPairs(n) {
		if p.Key == "$dynamicAnchor" {
			w.record(p.Val, pointer)
			continue
		}
		w.walk(p.Val, pointer+ids.Ptr(p.Key), depth+1)
	}
}

// record indexes the anchor val names at pointer. A value that is not a
// non-empty scalar names nothing a $dynamicRef could spell.
func (w *anchorWalk) record(val *yaml.Node, pointer string) {
	if val == nil || val.Kind != yaml.ScalarNode || val.Value == "" {
		return
	}
	w.out[val.Value] = append(w.out[val.Value], pointer)
}

// charge reports whether the walk may read one more node, recording the refusal
// when it may not so the caller of dynamicAnchors learns the index is partial.
func (w *anchorWalk) charge(depth int) bool {
	if depth > maxDynamicAnchorDepth || w.budget == 0 {
		w.truncated = true
		return false
	}
	w.budget--
	return true
}

// formatTable maps a scalar "type" or "type/format" key to its IR primitive.
// Keys absent here (byte, and any unknown format) hoist a Scalar instead.
var formatTable = map[string]ir.PrimKind{
	"string":           ir.PrimString,
	"string/date":      ir.PrimDate,
	"string/time":      ir.PrimTime,
	"string/duration":  ir.PrimDuration,
	"string/uuid":      ir.PrimUUID,
	"string/uri":       ir.PrimURL,
	"string/date-time": ir.PrimDatetimeOffset,
	"string/binary":    ir.PrimBytes,
	"string/password":  ir.PrimString,
	"integer":          ir.PrimInteger,
	"integer/int32":    ir.PrimInt32,
	"integer/int64":    ir.PrimInt64,
	"number":           ir.PrimNumber,
	"number/float":     ir.PrimFloat32,
	"number/double":    ir.PrimFloat64,
	"number/decimal":   ir.PrimDecimal,
	"boolean":          ir.PrimBool,
}

// baseForType returns the base primitive for an unknown-format scalar of type st.
func baseForType(st oas3.SchemaType) ir.PrimKind {
	switch st {
	case oas3.SchemaTypeInteger:
		return ir.PrimInteger
	case oas3.SchemaTypeNumber:
		return ir.PrimNumber
	case oas3.SchemaTypeBoolean:
		return ir.PrimBool
	default:
		return ir.PrimString
	}
}

// effectiveTypes returns a schema's declared types with the JSON Schema "null"
// member removed; nullability is normalized onto the TypeRef, not the type set.
func effectiveTypes(s *oas3.Schema) []oas3.SchemaType {
	types := s.GetType()
	out := make([]oas3.SchemaType, 0, len(types))
	for _, t := range types {
		if t == oas3.SchemaTypeNull {
			continue
		}
		out = append(out, t)
	}
	return out
}

// schemaHasNull reports whether a schema admits null via either dialect: 3.0
// nullable: true or a 3.1 type array containing "null".
func schemaHasNull(s *oas3.Schema) bool {
	if s.Nullable != nil && *s.Nullable {
		return true
	}
	return slices.Contains(s.GetType(), oas3.SchemaTypeNull)
}

// schemaAdmitsNull reports whether a schema admits null in any spelling: the two
// keyword dialects (schemaHasNull) or a oneOf/anyOf null branch. Lowering lifts
// all of them onto the enclosing TypeRef rather than into the type node, so this
// is the one predicate every site computing a Nullable bit goes through — a
// definition site, a union, and a $ref use site must never disagree about the
// same schema.
//
// A null branch counts only when the union is the type itself. Structural
// siblings intersect with the union (JSON Schema conjoins keywords), so
// `{type: object, oneOf: [{type: string}, {type: null}]}` admits neither string
// nor null; that union is kept verbatim under Unmodeled instead. A `type: null`
// branch is written inline, so it also blocks distribution — no distributed
// union can strip a null branch out from under this rule.
func schemaAdmitsNull(s *oas3.Schema) bool {
	if schemaHasNull(s) {
		return true
	}
	return oneOfAnyOfHasNull(s) && !hasUnionSiblings(s)
}

// nullUnionCollapse detects a oneOf/anyOf that has exactly one non-null branch
// alongside one or more `type: null` branches and returns that branch's schema,
// pointer, and hint so it lowers as nullable X rather than a union node
// (ir-design §3.3). A set with two or more non-null branches falls through to a
// Union (with its null branches stripped and lifted onto the enclosing ref).
//
// The hint it returns for the surviving branch is the *enclosing* schema's,
// while an outside $ref to that same branch pointer derives variant_<index>
// through subSchemaHint — so which of the two lowerings reaches the pointer
// first decides the name, and the two declaration orders produce different
// documents. That predates this function's co-declaration rule and is #181's
// mechanism at a site #181 did not sweep; GitHub #281 holds it. It is narrowed
// but not settled here: declining the collapse below removes the one order in
// which a co-declared anyOf could reach it.
//
// A schema declaring both combinators collapses neither. The collapse says the
// position *is* nullable X, and a co-declared anyOf conjoins with it, so it is
// not; the position falls through to the Union instead, which is the one node
// that keeps the branch set unionBranches passed over
// (preserveUnusedCombinator). Collapsing here would resolve the position
// straight to X's own node — a shared primitive for `{type: string}` — leaving
// the loser nowhere to sit that is not shared with every other declaration of X.
func nullUnionCollapse(s *oas3.Schema, pointer, hint string) (*oas3.JSONSchema[oas3.Referenceable], string, string, bool) {
	if len(s.GetOneOf()) > 0 && len(s.GetAnyOf()) > 0 {
		return nil, "", "", false
	}
	variants, key, _ := unionBranches(s)
	var nonNull *oas3.JSONSchema[oas3.Referenceable]
	nonNullIdx, nonNullCount, nullCount := -1, 0, 0
	for i, v := range variants {
		if isNullSchema(v) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = v, i
		nonNullCount++
	}
	if nullCount == 0 || nonNullCount != 1 {
		return nil, "", "", false
	}
	return nonNull, pointer + ids.Ptr(key, strconv.Itoa(nonNullIdx)), hint, true
}

// isNullSchema reports whether a variant schema is the bare null-typed schema.
func isNullSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if js == nil || !js.IsSchema() {
		return false
	}
	s := js.GetSchema()
	if s == nil {
		return false
	}
	types := s.GetType()
	return len(types) == 1 && types[0] == oas3.SchemaTypeNull
}

// listConstraints reads a list schema's collection constraints. Only the safe
// integer/bool bounds are read here; numeric-value bounds go through raw nodes
// elsewhere to avoid the float64 trap.
func listConstraints(s *oas3.Schema) *ir.Constraints {
	if s.MinItems == nil && s.MaxItems == nil && s.UniqueItems == nil {
		return nil
	}
	c := &ir.Constraints{MinItems: s.MinItems, MaxItems: s.MaxItems}
	if s.UniqueItems != nil {
		c.UniqueItems = *s.UniqueItems
	}
	return c
}

// requiredSet builds a lookup of a model's required property names.
func requiredSet(required []string) map[string]bool {
	if len(required) == 0 {
		return nil
	}
	set := make(map[string]bool, len(required))
	for _, r := range required {
		set[r] = true
	}
	return set
}

// merger builds the allOf property reconciler over an explicit diagnostic sink.
//
// Merger.Report records rather than returns, which is the one place this package
// still hands diagnostics to a callback. diags is the caller's own slice, so what
// the reconciler reports travels out the way everything else does; the callback
// is the merge package's contract, not accumulation surviving here. It is two
// closures over state already at hand, so building it per use costs nothing
// worth measuring.
func merger(c lowering.Ctx, ts *compile.Types, diags *[]ir.Diagnostic) merge.Merger {
	return merge.Merger{
		Resolve: ts.Node,
		Report: func(sev ir.Severity, code, pointer, format string, args ...any) {
			*diags = append(*diags, c.DiagAt(sev, code, pointer, format, args...))
		},
	}
}
