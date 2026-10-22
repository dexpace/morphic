package schema

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// LowerComponentSchemas interns every named component schema in source order.
// It is the entry Compile's run() calls before any operation lowering so that
// $refs resolve to already-registered IDs.
//
// ctx bounds the walk in time: a document declares as many components as it
// likes, so this loop is one of the two places a compile does work proportional
// to nothing the compiler chose. Cancellation stops it between components and
// returns what was lowered so far; the caller — run — sees ctx.Err() at the
// phase boundary immediately after and refuses the document there, so a partial
// registry never becomes a Document.
func LowerComponentSchemas(ctx context.Context, c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex) []ir.Diagnostic {
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
		if ctx.Err() != nil {
			return diags
		}
		diags = append(diags, lowerComponentSchema(c, ts, anchors, js, ids.Ptr("components", "schemas", name), name)...)
	}
	return diags
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func lowerComponentSchema(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer, name string) []ir.Diagnostic {
	s := annotation.At(js)
	ref, diags := Ref(c, ts, anchors, TopLevelDepth, js, pointer, name)
	// A body that interned the component's own node at its component ID needs no
	// alias, and its annotations were attached where it was lowered.
	if _, owned := ts.Lookup(c.IDs().Coordinate(pointer)); owned {
		return diags
	}
	_, aliasDiags := internAlias(c, ts, s.Node, pointer, name, ref)
	diags = append(diags, aliasDiags...)
	// This alias is the first node the pointer owns, so the annotations
	// schemaBody had nowhere to put now have a home.
	if s.Node != nil {
		diags = append(diags, attachDeclaredAnnotations(c, ts, anchors, s.Node, pointer)...)
	}
	return diags
}

// recordDeclarationResidue keeps, on the node pointer owns, every keyword the
// declaration wrote that only a use site can hold. The rule
// docs/compilers/openapi.md#use-bound-annotations applies them to referencing
// properties, which the property path already does; this covers a declaration
// nothing references (GitHub #138).
//
// It runs only at an annotation.HomeOwnNode position, which has no carrier.
// Every annotation.HomeCarrier position already keeps them in its own field:
// `default` in a default field, readOnly/writeOnly in ir.Property.Visibility
// or, at a parameter, verbatim on the ir.Parameter (preserveParamVisibility).
// A pointer with no node wrote none (declaresPositionScoped hoists one for any
// it wrote) or is mid-build, and its builder records them.
func recordDeclarationResidue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	// A $ref never lowers a carrier's own schema, which its declaration lowers as
	// HomeCarrier, so a position reaches here as HomeOwnNode only where nothing
	// carries it, and the record here is its only one (GitHub #750).
	if home != annotation.HomeOwnNode || s == nil {
		return nil
	}
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
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

// recordResidue keeps each declared residue keyword verbatim in
// common.Unmodeled and reports it at the keyword's own pointer.
//
// The message says the keyword is applied wherever a carrier has a field for
// it: `default` at every carrier, readOnly/writeOnly at a property or header
// but not at a parameter, which has no Visibility field. So a $ref'd
// declaration's readOnly reaches a referencing parameter only here, on the
// declaration's own node.
func recordResidue(c lowering.Ctx, common *ir.TypeCommon, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range residueKeywords {
		kept, keptDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword, ir.ReasonNoIRHome, pointer, c.ProvenanceAt)
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

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. It is shared by Ref and by sub-schema hoisting
// (resolveSchemaRef), which both reach a body only after peeling off any
// leading $ref. A rule about the declaration itself is checked here, above the
// dispatch that picks what it lowers to (missingArrayItems).
func schemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer jsontext.Pointer, hint string, home annotation.Home) (ir.TypeRef, []ir.Diagnostic) {
	diags := missingArrayItems(c, schema, pointer)
	target, bodyDiags := lowerSchemaBody(c, ts, anchors, depth, schema, pointer, hint)
	diags = append(diags, bodyDiags...)
	// A body's census ran inside lower(), against the node the walk actually
	// built, which is the only thing that can answer it. A collapsed union built
	// none, so it takes a $ref site's census, and its verdict homes an alias.
	unhomed := collapsedUnhomedKeywords(schema)
	ref, homeDiags := homeDeclaration(c, ts, anchors, schema, target, pointer, hint, home, len(unhomed) > 0)
	diags = append(diags, homeDiags...)
	if home != annotation.HomeOwnNode {
		return ref, diags // the carrier keeps them (PreserveRefSiteKeywords)
	}
	return ref, append(diags, recordUnhomedKeywords(c, ts, ref.Target, schema, unhomed, collapsedShape, pointer)...)
}

// hoistDeclarationHome gives a declaration a node of its own when its lowering
// left it none and it wrote something only a node can hold
// (declaresPositionScoped; GitHub #116), since a shared node must never carry
// one declaration's annotations.
//
// A declaration that already has a home resolves to it, checked after that gate
// so a position declaring nothing still resolves to its target. A $ref naming
// an inline position hoists that home first (resolveSchemaRef); taking the
// body's target would make annotations depend on declaration order (ir-design
// §4.3).
//
// unhomed is the census verdict of a $ref site or a collapsed union, which
// declaresPositionScoped cannot judge.
func hoistDeclarationHome(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, ref ir.TypeRef, pointer jsontext.Pointer, hint string, home annotation.Home, unhomed bool) (ir.TypeRef, []ir.Diagnostic) {
	if home != annotation.HomeOwnNode || s == nil {
		return ref, nil
	}
	if !unhomed && !declaresPositionScoped(s) {
		return ref, nil
	}
	// The home stands where its target stood, so it keeps what the reference
	// said of null, the marker naming a heuristic that read it in included.
	out := ref
	if id, owned := ts.Lookup(c.IDs().Coordinate(pointer)); owned {
		out.Target = id
		return out, nil
	}
	id, diags := internAlias(c, ts, s, pointer, hint, ref)
	out.Target = id
	return out, diags
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

// declaresAnnotations reports whether s carries documentation, deprecation, a
// redaction, XML hints, vendor extensions, or examples — the TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *oas3.Schema) bool {
	if s.GetTitle() != "" || s.GetDescription() != "" || s.GetExternalDocs() != nil {
		return true
	}
	if annotation.EffectiveDeprecated(s, nil) || annotation.DeclaresRedaction(s) || s.GetXML() != nil {
		return true
	}
	if ext := s.GetExtensions(); ext != nil && ext.Len() > 0 {
		return true
	}
	return s.GetExample() != nil || len(s.GetExamples()) > 0
}

// declaresValidationOnly reports whether s writes a §4.7 validation-only
// keyword that annotation.Read keeps verbatim under Unmodeled. A `false`
// unevaluatedProperties is excluded on purpose: fillAdditional lowers it into
// the model's openness, so it is structure rather than a preserved keyword.
func declaresValidationOnly(s *oas3.Schema) bool {
	if annotation.Written(s.GetNot()) || annotation.Written(s.GetContains()) ||
		annotation.Written(s.GetPropertyNames()) {
		return true
	}
	if annotation.Written(s.GetIf()) || annotation.Written(s.GetThen()) || annotation.Written(s.GetElse()) {
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
	return annotation.Written(s.GetUnevaluatedItems()) || (annotation.Written(up) && !annotation.IsFalseSchema(up))
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
func lowerSchemaBody(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, schema *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeRef, []ir.Diagnostic) {
	target, _, expanded, diags := dynamicExpansion(c, anchors, schema, pointer)
	if expanded {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DynamicRefExpanded, pointer+ids.Ptr("$dynamicRef"),
			"$dynamicRef expanded to %q, the one matching $dynamicAnchor in this document", target))
		ref, nullDiags := schemaNullRef(c, target, schema, siteAt(pointer))
		return ref, append(diags, nullDiags...)
	}
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			id, unionDiags := lowerCoDeclaredUnion(c, ts, anchors, depth, schema, pointer, hint)
			ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
			return ref, slices.Concat(diags, unionDiags, nullDiags)
		}
		ref, unionDiags := lowerOneOfAnyOf(c, ts, anchors, depth, schema, pointer, hint)
		return ref, append(diags, unionDiags...)
	}
	id, bodyDiags := lower(c, ts, anchors, depth, schema, pointer, hint)
	ref, nullDiags := schemaNullRef(c, id, schema, siteAt(pointer))
	return ref, slices.Concat(diags, bodyDiags, nullDiags)
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
	return len(s.GetRequired()) > 0 || annotation.Written(s.GetItems()) || len(s.GetPrefixItems()) > 0
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
	if annotation.Written(s.GetAdditionalProperties()) {
		return true
	}
	if pp := s.GetPatternProperties(); pp != nil && pp.Len() > 0 {
		return true
	}
	return len(effectiveTypes(s)) > 0
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing), returning the interned ID and the one info diagnostic that
// announces it.
func falseSchema(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// Kept verbatim beside the approximation, which is what §4.8 asks of
		// every degraded lowering that loses something, and what the composition
		// half of this same rule already did. Without it a `false` schema and a
		// schema that merely wrote `additionalProperties: false` are the same
		// node, so nothing downstream can tell "no instance" from "the empty
		// object" — the diagnostic says which one it was, but a diagnostic is not
		// part of the document.
		//
		// The key names the position rather than a keyword, because a boolean
		// schema writes none. Nothing can collide with it: a schema that is a
		// boolean has no other keywords to preserve.
		annotation.PreserveInto(&common.Unmodeled, "openapi:schema",
			ir.RawValue("false"), ir.ReasonDegradedLowering, c.ProvenanceAt(pointer))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.FalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model "+
				"with the schema kept verbatim under Unmodeled"))
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
// included. `enum: []` is legal JSON Schema and fixes the value space to the
// empty set, so it declares one as a populated list does; testing len > 0
// instead would widen the position to whatever its siblings admit, in silence
// (GitHub #278).
//
// Nilness is what the parser keeps: an absent keyword leaves the field nil, an
// empty list leaves it non-nil. A member list the model layer could not parse
// reads as absent, which is a document the loader already refuses.
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
// declaring none leaves won empty and skipped nil.
//
// familyOrder, declaresFamily and lower()'s switch must name the same families.
// A name declaresFamily does not know is never elected, and a loser reaches
// recordSkippedFamilies whether or not lower() can lower it, but a winner
// lower() has no arm for is neither lowered nor skipped: the switch falls
// through to the type-set arms and drops the keyword in silence (GitHub #35).
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

// lower interns the inline schema at pointer and returns its TypeID. const,
// enum and allOf composition take precedence over the structural type, which
// is otherwise dispatched on the effective (null-stripped) type set.
//
// The families are conjoined where a schema writes more than one, so electing
// a winner is a degradation (docs/compilers/openapi.md#competing-keywords): the
// ones passed over are kept verbatim beside it (recordSkippedFamilies). A
// keyword the elected lowering never reads, such as `type: string` beside an
// allOf, goes through preserveUnhomedKeywords, which asks the built node
// whether it has a field for it.
func lower(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
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
		// not the Union's, so the applicator check does not apply. A keyword no
		// declared type's variant holds, such as a discriminator with no object
		// variant, is dropped (GitHub #852).
		return lowerUnion(c, ts, anchors, depth, s, pointer, hint, types)
	case len(types) == 1:
		return unhomed(lowerTyped(c, ts, anchors, depth, s, pointer, hint, types[0]))
	default:
		return unhomed(lowerUntyped(c, ts, anchors, depth, s, pointer, hint))
	}
}

// recordSkippedFamilies keeps verbatim the keyword families lower()'s election
// passed over, and reports them once.
//
// JSON Schema conjoins keywords, so `{allOf: [{$ref: Base}], enum: [a, b]}` is a
// narrowing of Base, not a malformed document. The IR has no intersection
// combinator (ir-design §15), so only one family can be the value; keeping the
// loser beside it is what the rule docs/compilers/openapi.md#competing-keywords
// asks of an unrepresentable conjunction.
//
// It reports the keywords it stored, never the ones it was handed, so the
// message cannot claim one that failed to convert.
func recordSkippedFamilies(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, d dispatch, pointer jsontext.Pointer) []ir.Diagnostic {
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
		stored, storedDiags := annotation.PreserveSchemaKeyword(&common.Unmodeled, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
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

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func lowerTyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string, st oas3.SchemaType) (ir.TypeID, []ir.Diagnostic) {
	switch st {
	case oas3.SchemaTypeObject:
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	case oas3.SchemaTypeArray:
		return lowerArray(c, ts, anchors, depth, s, pointer, hint)
	default:
		return scalarTypeID(c, ts, anchors, depth, s, st, pointer, hint)
	}
}

// lowerUntyped handles a schema with no declared type: a property set makes it a
// model; enum/const and composition are lowered by later passes; anything else
// is schemaless.
func lowerUntyped(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return lowerModel(c, ts, anchors, depth, s, pointer, hint)
	}
	return ts.PrimID(ir.PrimAny), nil
}

// attachDeclaredAnnotations records every annotation s declares on the type
// node pointer owns, the one structural home they have (ir.TypeCommon).
//
// It is the sole reader of declaration-scoped annotations and runs above
// lower()'s dispatch: every declaration reaches it through schemaBody or an
// alias fallback, so a new lowering destination cannot forget to read them
// (GitHub #114).
//
// A schema whose body reduced to a shared primitive owns no node; its
// annotations stay with the declaring property (FillPropertyDetail). Ownership
// is checked before conversion because callers cover a pointer in either order,
// and only the one that finds a node may emit conversion diagnostics.
func attachDeclaredAnnotations(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// No node here means a position that reduced to a shared primitive, or one
	// still being built further up this walk, whose builder attaches the same
	// declaration once its build returns (GitHub #749).
	td, ok := ts.NodeAt(c.IDs().Coordinate(pointer))
	if !ok {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Declaration, Node: s}, pointer, c.ProvenanceAt)

	common := td.Common()
	common.Docs = a.Docs
	if a.Deprecated {
		common.Deprecation = &ir.Deprecation{}
	}
	if a.Sensitive {
		common.Sensitive = true
	}
	if a.XML != nil {
		common.XML = a.XML
	}
	common.Unmodeled = annotation.MergeUnmodeled(common.Unmodeled, a.Unmodeled)
	diags = append(diags, c.PromoteDeprecation(common.Unmodeled, common.Deprecation, &common.Provenance)...)
	// The enum-openness promotion is applied here rather than where the Enum is
	// built, for the same reason the deprecation one is: a promotion reads the
	// preserved Unmodeled entries, and this is the point at which the
	// declaration's extensions have reached the node's map.
	if enum, isEnum := td.(*ir.Enum); isEnum {
		c.PromoteEnumOpenness(common.Unmodeled, enum, &common.Provenance)
	}
	if len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	diags = append(diags, recordUnplacedContent(c, &common.Unmodeled, s, td, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(c, anchors, &common.Unmodeled, s, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &common.Unmodeled, s, pointer)...)
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
