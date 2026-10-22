package schema

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
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

// ConstraintsOf returns the value constraints s writes, or nil where it writes
// none, for the node or carrier at pointer to hold, so a node a position owns,
// a property and a parameter read them alike.
//
// p is the Unmodeled map of the node or carrier the constraints land on. A
// co-declared numeric bound with no field in ir.Constraints is kept there,
// beside the constraints it did not reach (GitHub #286).
func ConstraintsOf(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) (*ir.Constraints, []ir.Diagnostic) {
	if s == nil {
		return nil, nil
	}
	cons, kept, diags := annotation.Constraints(s, c.Is30(), pointer, c.ProvenanceAt)
	*p = annotation.MergeUnmodeled(*p, kept)
	return cons, stampConstraintDiags(c, diags, pointer)
}

// stampConstraintDiags gives every constraint diagnostic the provenance of the
// pointer that read the schema, which is what makes two reads of one sub-schema
// — its owning property and a $ref that hoists it — identical and so deduped by
// Diags.Append rather than reported twice.
func stampConstraintDiags(c lowering.Ctx, diags []ir.Diagnostic, pointer jsontext.Pointer) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance = c.ProvenanceAt(pointer)
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

// valueConstraintKeywords are the keywords annotation.Constraints reads into an
// ir.Constraints, sorted — which is both the order declaredConstraints walks
// them in and what makes the list comparable to the reader it must keep pace
// with.
//
// One list, read by the predicate that hoists a node for them
// (declaresValueConstraints) and by the recorder that keeps the ones no node can
// hold (declaredConstraints), so the two can never disagree about what a schema
// wrote. Collection bounds are absent because they belong to the collection
// nodes and are read by collectionConstraints, not here.
var valueConstraintKeywords = []string{
	"exclusiveMaximum", "exclusiveMinimum", "maxLength", "maxProperties",
	"maximum", "minLength", "minProperties", "minimum", "multipleOf", "pattern",
}

// declaresValueConstraints reports whether s sets any keyword
// annotation.Constraints reads. It does not call it: that reports a malformed
// bound, and a predicate must not emit diagnostics. The keywords are detected on
// their raw nodes for the same reason numericBounds reads them there — a
// magnitude beyond float64 leaves the model field nil while the keyword is
// plainly written.
func declaresValueConstraints(s *oas3.Schema) bool {
	return annotation.DeclaresAny(s, valueConstraintKeywords)
}

// declaredConstraints returns the value-constraint keywords s writes, in
// valueConstraintKeywords order.
func declaredConstraints(s *oas3.Schema) []string {
	out := make([]string, 0, len(valueConstraintKeywords))
	for _, keyword := range valueConstraintKeywords {
		if annotation.RawPropertyNode(s, keyword) != nil {
			out = append(out, keyword)
		}
	}
	return out
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

// censusKeywords are the keywords whose IR home depends on what the position
// lowered to: a Model's properties, a Model's or MapT's catch-all, a List's
// element type and bounds, a Literal's value, an Enum's members, a Model's or
// Union's discriminator. keywordHome asks the built node which a position kept.
//
// Annotations, validation-only and use-site keywords, content vocabulary and
// value constraints have their own keepers.
//
// The list and keywordHome's switch must agree. A keyword listed without an
// arm is homeless and preserved everywhere, noisy but lossless; one in neither
// is dropped in silence (GitHub #268, #283).
// oneOf/anyOf/allOf stay out; see refSiteUnhomedKeywords (GitHub #406).
var censusKeywords = []string{
	"additionalProperties", "const", "discriminator", "enum", "format", "items",
	"maxItems", "minItems", "patternProperties", "prefixItems", "properties",
	"required", "type", "uniqueItems",
}

// keywordHome reports whether td, the node this position's own declaration
// lowered to, has the field the named keyword lowers into. It asks the node
// rather than re-deriving lower()'s dispatch, so the two cannot drift apart
// (see recordUnplacedContent).
//
// A nil td is a position lowered to a reference: a $ref site, or a collapsed
// union (collapsedUnhomedKeywords). Its alias carries only the position's
// constraints and annotations, and the target's fields belong to the
// referent's declaration, so a `format` beside a $ref must not read the
// referent's Encoding as its home. So every keyword is homeless there except
// a null-only `type`.
func keywordHome(td ir.TypeDef, s *oas3.Schema, keyword string) bool {
	switch keyword {
	case "properties", "patternProperties", "additionalProperties":
		// A MapT homes all three: its Value is the catch-all, and it is a map
		// only because the other two declared no member (ir.AsMap).
		return isKind(td, ir.KindModel) || isKind(td, ir.KindMap)
	case "required":
		// A MapT has no key to require, so it homes only an empty list. A name
		// lands on a property or in Model.Requires (requireMembers), either of
		// which keeps the node a Model, so a MapT never meets a longer one.
		return isKind(td, ir.KindModel) || (isKind(td, ir.KindMap) && len(s.GetRequired()) == 0)
	case "items", "prefixItems":
		return isKind(td, ir.KindList) || isKind(td, ir.KindTuple)
	case "maxItems", "minItems", "uniqueItems":
		// Either collection node, since collectionConstraints fills both. That is
		// also why valueConstraintKeywords leaves them out, which puts them in this
		// census rather than in declaredConstraints.
		return isKind(td, ir.KindList) || isKind(td, ir.KindTuple)
	case "const":
		// Any counts as well as Literal. hoistLiteral degrades a value ir.Value
		// cannot represent to the top type and reports it, so the const was read
		// — badly, and already announced — rather than left unread. Claiming it
		// here would report one keyword twice, the second time as an error, since
		// a value no converter can read is one no preserver can read either.
		return isKind(td, ir.KindLiteral) || isKind(td, ir.KindAny)
	case "enum":
		// Any counts here for the reason it counts for const: lowerEnum degrades a
		// member set past the caller's budget to the top type and reports it, so
		// the enum was read and announced rather than left unread. Claiming it
		// again would also undo the budget — the census preserves a homeless
		// keyword verbatim, which would put every member back into the IR as raw
		// bytes and leave only the per-member amplification bounded (GitHub #75).
		return isKind(td, ir.KindEnum) || isKind(td, ir.KindUnion) || isKind(td, ir.KindAny)
	case "format":
		return formatHome(td, s)
	case "type":
		return typeHome(td, s)
	case "discriminator":
		return discriminatorHome(td)
	default:
		return false
	}
}

// discriminatorHome reports whether td holds the discriminator its position
// declared. Only a Model and a Union have the field, and having it is not
// filling it: the Union a mixed-type enum lowers to carries none. One kept for
// want of a home keeps its extensions a second time, under
// openapi:discriminator/x-* (GitHub #849).
func discriminatorHome(td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Model:
		return n.Discriminator != nil
	case *ir.Union:
		return n.Discriminator != nil
	default:
		return false
	}
}

// isKind reports whether td is a live node of kind k. A nil td — the $ref site's
// alias, which is no node of the walk's making — is of no kind at all.
func isKind(td ir.TypeDef, k ir.TypeKind) bool {
	return !ir.IsNilTypeDef(td) && td.Kind() == k
}

// formatHome reports whether the position's `format` reached a field. A Scalar
// hoisted for it carries it in Encoding; a (type, format) pairing
// spelling.Lookup knows selects a primitive, which carries the pairing in the
// primitive kind itself. With no type declared there was no pairing to make
// and nothing read it, and a node of any other kind has no Encoding field at
// all.
func formatHome(td ir.TypeDef, s *oas3.Schema) bool {
	// The redaction format names no shape, so no node kind is its home: the
	// annotation readers hold it, as Sensitive on the node the position owns or
	// Secret on its carrier, and verbatim at a parameter, which has no field.
	if annotation.DeclaresRedaction(s) {
		return true
	}
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Encoding != nil
	case *ir.Primitive:
		return len(effectiveTypes(s)) > 0
	default:
		return false
	}
}

// typeHome reports whether every shape s's declared type set names is a shape td
// can be. One that td cannot be was read by nothing: the walk built a different
// shape, so `type: string` beside an allOf that composed a Model states something
// the IR no longer holds.
//
// A type set that reduces to nothing — `type: "null"` on its own — is carried by
// TypeRef.Nullable rather than by a node, so it is homed wherever it is written.
func typeHome(td ir.TypeDef, s *oas3.Schema) bool {
	for _, st := range effectiveTypes(s) {
		if !typeShapedBy(td, st) {
			return false
		}
	}
	return true
}

// typeShapedBy reports whether td is a node kind that carries the shape st names.
//
// An Enum carries a scalar type in ValueType, which enumValueType fills from
// exactly that type set. A Literal carries only its value, so a `type` written
// beside a const reached no field at all — including the case where the two
// agree, since the IR then holds the value and nothing about what was declared
// about it. Any, External and keywordHome's nil td carry no shape.
func typeShapedBy(td ir.TypeDef, st oas3.SchemaType) bool {
	switch td.(type) {
	case *ir.Model, *ir.MapT:
		return st == oas3.SchemaTypeObject
	case *ir.List, *ir.Tuple:
		return st == oas3.SchemaTypeArray
	case *ir.Primitive, *ir.Scalar, *ir.Enum:
		return st != oas3.SchemaTypeObject && st != oas3.SchemaTypeArray
	case *ir.Any:
		// The top type admits every shape, so a declared type says nothing it
		// contradicts. A position only reaches Any with a type declared by being
		// degraded there — an unrepresentable const, an enum past its budget — and
		// that degradation is already reported, so restating the type beside it
		// would announce the same collapse twice.
		return true
	default:
		return false
	}
}

// constraintsHome reports whether the value constraints a position declared
// reached td's Constraints field. A position that owns its node hoists no alias
// over it (hoistDeclarationHome), so a constraint that missed the field reaches
// none anywhere.
//
// Having the field is not filling it, so a Scalar, Model or MapT is asked
// whether it is non-nil; lowerModel and lowerAllOf fill a Model's, and so its
// MapT's, the same way (GitHub #407). A List or Tuple is absent because
// lowerArray and buildTuple fill theirs from collectionConstraints, whose
// collection bounds valueConstraintKeywords excludes, so no value constraint
// reaches either however full the field looks.
func constraintsHome(td ir.TypeDef) bool {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints != nil
	case *ir.Model:
		return n.Constraints != nil
	case *ir.MapT:
		return n.Constraints != nil
	default:
		// Every other kind has no Constraints field, or holds only collection
		// bounds in it as a List and a Tuple do, so a value bound written beside
		// one was read by nothing.
		return false
	}
}

// censusSchemaKeywords are the census keywords whose value is one schema, so a
// null written for one states nothing (docs/compilers/openapi.md#null-schema).
// Elsewhere a null may be a value: `const: null` writes one.
var censusSchemaKeywords = map[string]bool{"additionalProperties": true, "items": true}

// censusWrote reports whether s writes keyword for the census to read: its raw
// node is present and, where the value is a schema, no null.
func censusWrote(s *oas3.Schema, keyword string) bool {
	node := annotation.RawPropertyNode(s, keyword)
	return node != nil && (!censusSchemaKeywords[keyword] || !annotation.IsNullNode(node))
}

// unhomedKeywords returns the census keywords s declares that td has nowhere to
// carry, in censusKeywords order. It reads the raw nodes for the reason
// declaresValueConstraints does: a keyword the model layer failed to parse is
// still plainly written.
//
// handled names keywords another reader at this position already accounts for:
// the families lower()'s election passed over go to recordSkippedFamilies, which
// knows why they lost, and an allOf branch's `required` is consumed by
// requireMembers. Recording them here too would report one keyword twice.
func unhomedKeywords(s *oas3.Schema, td ir.TypeDef, handled []string) []string {
	out := make([]string, 0, len(censusKeywords))
	for _, keyword := range censusKeywords {
		if !censusWrote(s, keyword) || keywordHome(td, s, keyword) {
			continue
		}
		if slices.Contains(handled, keyword) {
			continue
		}
		out = append(out, keyword)
	}
	return out
}

// refSiteUnhomedKeywords is unhomedKeywords for a $ref site (td is always nil),
// plus allOf. handled passes through so an allOf branch spelled as a $ref still
// routes its own `required` to requireMembers.
//
// allOf is as homeless here as every census keyword, because none of the
// $ref-site callers goes through dispatchOf, so it never wins an election.
// Adding it to censusKeywords instead would also hit ordinarily composed
// schemas, since keywordHome has no arm saying a Model built from a winning
// allOf carries it.
//
// oneOf/anyOf stay out: callers check declaresUnion and call the union keepers
// directly (GitHub #406).
func refSiteUnhomedKeywords(s *oas3.Schema, handled []string) []string {
	unhomed := unhomedKeywords(s, nil, handled)
	if len(s.GetAllOf()) > 0 {
		unhomed = append(unhomed, "allOf")
	}
	return unhomed
}

// collapsedShape describes what a collapsed union lowers to, for the diagnostic
// recordUnhomedAt reports.
const collapsedShape = "a nullable reference to its one non-null branch"

// collapsedUnhomedKeywords returns the census keywords s writes when its
// oneOf/anyOf collapses to a nullable reference (lowerOneOfAnyOf), or nil when
// it does not. Every one is homeless: the position lowers to a reference to its
// branch's node, which belongs to the branch, as a $ref site's target belongs
// to the referent (keywordHome's nil td). The census keywords such a schema can
// write are the few neither declaresShape nor narrowsInstance counts, the
// discriminator among them (GitHub #595). lowerSchemaBody's other outcomes
// that skip lower(), a Union and an expanded $dynamicRef, run no census yet
// (GitHub #848).
func collapsedUnhomedKeywords(s *oas3.Schema) []string {
	if hasUnionSiblings(s) {
		return nil
	}
	if _, _, _, collapses := nullUnionCollapse(s, ""); !collapses {
		return nil
	}
	return unhomedKeywords(s, nil, nil)
}

// preserveUnhomedKeywords keeps verbatim the census keywords and value
// constraints a position declared that its lowered node cannot carry, and the
// families lower()'s election skipped, returning the position's resulting ID.
// docs/compilers/openapi.md#shape-applicator covers a contradictory schema's
// dropped half and an untyped applicator such as `items`.
//
// A lowering that reduced to a shared node gets an alias first, with the
// position's constraints, so a shared node never carries one declaration's
// keywords. A node the position owns gets none, so a bound beside an Enum is
// kept verbatim.
//
// A multipart body still mints encoding keys for `properties` the node lacks,
// so pass.Validate reports ir/encoding-key-unknown-property.
func preserveUnhomedKeywords(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string, id ir.TypeID, d dispatch) (ir.TypeID, []ir.Diagnostic) {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return id, diags
	}
	owns, _ := ts.Lookup(c.IDs().Coordinate(pointer))
	unhomed := unhomedKeywords(s, td, d.skipped)
	if owns == id && !constraintsHome(td) {
		// No alias will be hoisted over a node the position already owns, so the
		// bounds it wrote reach no Constraints field anywhere.
		unhomed = append(unhomed, declaredConstraints(s)...)
	}
	if len(unhomed) == 0 && len(d.skipped) == 0 {
		return id, diags
	}
	owner := id
	if owns != id {
		var aliasDiags []ir.Diagnostic
		owner, aliasDiags = internAlias(c, ts, s, pointer, hint, ir.TypeRef{Target: id})
		diags = append(diags, aliasDiags...)
	}
	diags = append(diags, recordUnhomedKeywords(c, ts, owner, s, unhomed, nodeShape(td.Kind()), pointer)...)
	return owner, append(diags, recordSkippedFamilies(c, ts, owner, s, d, pointer)...)
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

// refSiteShape describes what a $ref position's own declaration lowers to, for
// the diagnostic recordUnhomedAt reports.
const refSiteShape = "an alias over its $ref target"

// nodeShape describes a node of kind k, for that same diagnostic.
func nodeShape(kind ir.TypeKind) string { return fmt.Sprintf("a node of kind %q", kind) }

// recordUnhomedKeywords stores each unhomed keyword on the node owner names.
// shape describes what the position lowered to, so a reader is told which half
// of a contradictory schema the IR describes.
func recordUnhomedKeywords(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(unhomed) == 0 {
		return nil
	}
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	return append(diags, recordUnhomedAt(c, &td.Common().Unmodeled, s, unhomed, shape, pointer)...)
}

// recordUnhomedAt keeps each unhomed keyword verbatim in p and reports them once.
//
// It names the keywords it actually stored, never the ones it was handed, so the
// message cannot claim one that failed to convert and was reported unpreservable
// instead (GitHub #144).
func recordUnhomedAt(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	kept := make([]string, 0, len(unhomed))
	for _, keyword := range unhomed {
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, keyword)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position lowered to %s, which has no home for %s declared beside it; kept verbatim under Unmodeled",
		shape, strings.Join(kept, ", ")))
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
