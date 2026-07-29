package openapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/speakeasy-api/openapi/extensions"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// lowerComponentSchemas interns every named component schema in source order.
// It is the entry Compile's run() calls before any operation lowering so that
// $refs resolve to already-registered IDs.
func (l *lowerer) lowerComponentSchemas() {
	comps := l.doc.Components
	if comps == nil {
		return
	}
	schemas := comps.GetSchemas()
	if schemas == nil {
		return
	}
	// Record every declared name before lowering any schema so a $ref or
	// discriminator mapping resolved mid-lowering sees forward-declared
	// components as valid targets regardless of source order.
	l.schemas = make(map[string]bool, schemas.Len())
	for name := range schemas.All() {
		l.schemas[name] = true
	}
	for name, js := range schemas.All() {
		l.lowerComponentSchema(js, ptr("components", "schemas", name), name)
	}
}

// lowerComponentSchema lowers one named component schema and guarantees a node
// is registered at the component's own TypeID even when its body reduces to a
// shared primitive/any or aliases another type. Without this, a component like
// `MyId: {type: string, format: uuid}` would leave nothing at its component
// pointer and every $ref to it would dangle (invariants 1 and 2).
func (l *lowerer) lowerComponentSchema(js *oas3.JSONSchema[oas3.Referenceable], pointer, name string) {
	s := siteAt(js)
	ref := l.schemaRef(js, pointer, name)
	// A body that interned the component's own node at its component ID needs no
	// alias, and its annotations were attached where it was lowered.
	if _, owned := l.types.Lookup(pointer); owned {
		return
	}
	l.internAlias(pointer, name, ref, l.schemaConstraints(s.Node, pointer))
	// This alias is the first node the pointer owns, so the annotations
	// schemaBody had nowhere to put now have a home.
	if s.Node != nil {
		l.attachDeclaredAnnotations(s.Node, pointer)
	}
}

// recordDeclarationResidue keeps, on the node pointer owns, every keyword the
// declaration wrote that only a use site can hold.
//
// ir-design §14's OpenAPI row applies these to referencing properties with
// use-site precedence, which the property path already does; this is the other
// half of that rule, and it is what a declaration nothing references would
// otherwise lose silently (GitHub #138).
//
// It runs only at a homeOwnNode position. A homeCarrier one — a model property,
// a header, a parameter — already lands all three in their real fields
// (fillPropertyDefault, effectiveVisibility), so residue there would restate
// what the carrier holds rather than rescue anything.
//
// residueKeywords is what declaresPositionScoped gates hoisting on, so a
// position that wrote one of them owns a node by the time this runs; a pointer
// with none is a position whose declaration wrote nothing at all.
func (l *lowerer) recordDeclarationResidue(s *oas3.Schema, pointer string, home annotationHome) {
	if home != homeOwnNode || s == nil {
		return
	}
	td, ok := l.types.NodeAt(pointer)
	if !ok {
		return
	}
	l.recordResidue(td.Common(), s, pointer)
}

// residueKeywords are the keywords a schema can write that bind a *use* of the
// type rather than the type itself, so no type node has a field to hold them:
// `default` (Property/Parameter.Default is its only home) and
// readOnly/writeOnly (Property.Visibility is theirs).
//
// One list, read by both the predicate that hoists a node for them
// (declaresResidue) and the recorder that fills it (recordResidue), so the two
// can never drift into either half of declaresPositionScoped's trap.
var residueKeywords = []string{"default", "readOnly", "writeOnly"}

// recordResidue keeps each declared residue keyword verbatim on c and reports
// it at the keyword's own pointer. The message says "any referencing property or
// parameter" because both halves vary: §14 pushes the keyword down to referencing
// carriers where there are some, and an inline position that owns a node has
// none at all — which is the case Unmodeled is rescuing.
func (l *lowerer) recordResidue(c *ir.TypeCommon, s *oas3.Schema, pointer string) {
	for _, keyword := range residueKeywords {
		raw := nodeToRaw(rawPropertyNode(s, keyword))
		if len(raw) == 0 {
			continue
		}
		l.preserve(&c.Unmodeled, "openapi:"+keyword, raw, ir.ReasonNoIRHome, pointer+ptr(keyword))
		l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer+ptr(keyword),
			"%s on a type declaration binds a use of the type, not the type; "+
				"it is applied to any referencing property or parameter and kept "+
				"verbatim under Unmodeled", keyword)
	}
}

// schemaConstraints reads the value constraints of a schema and stamps each
// constraint diagnostic with pointer's provenance. It returns nil only when
// there is no schema to read. It is the shared path for every alias a body
// reduces to — a named component (lowerComponentSchema) and a $ref-hoisted
// internal sub-schema (hoistSubSchema) — so a scalar that aliases a shared
// primitive never drops the constraints it carried, including a bound written
// beside a $ref, which constrains the position it is written at.
func (l *lowerer) schemaConstraints(s *oas3.Schema, pointer string) *ir.Constraints {
	if s == nil {
		return nil
	}
	c, diags := constraintsFromSchema(s, l.exclusiveBoundIsBoolean())
	l.appendConstraintDiags(diags, pointer)
	return c
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. Any value
// constraints the schema carried are attached so a scalar component never drops
// them.
func (l *lowerer) internAlias(pointer, hint string, target ir.TypeRef, constraints *ir.Constraints) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := target
		return &ir.Scalar{TypeCommon: common, Base: &base, Constraints: constraints}
	})
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef and
// records the schema's own annotations on whatever node the lowering hoisted at
// pointer. It is shared by schemaRef and by sub-schema hoisting
// (resolveSchemaRef), which both reach a body only after peeling off any
// leading $ref.
func (l *lowerer) schemaBody(schema *oas3.Schema, pointer, hint string, home annotationHome) ir.TypeRef {
	ref := l.hoistDeclarationHome(schema, l.lowerSchemaBody(schema, pointer, hint), pointer, hint, home)
	l.attachDeclaredAnnotations(schema, pointer)
	l.recordDeclarationResidue(schema, pointer, home)
	return ref
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
func (l *lowerer) hoistDeclarationHome(s *oas3.Schema, ref ir.TypeRef, pointer, hint string, home annotationHome) ir.TypeRef {
	if home != homeOwnNode || s == nil || !declaresPositionScoped(s) {
		return ref
	}
	if id, owned := l.types.Lookup(pointer); owned {
		return ir.TypeRef{Target: id, Nullable: ref.Nullable}
	}
	id := l.internAlias(pointer, hint, ref, l.schemaConstraints(s, pointer))
	return ir.TypeRef{Target: id, Nullable: ref.Nullable}
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
		declaresValidationOnly(s) || declaresResidue(s) ||
		declaresContentVocabulary(s) || declaresDynamicRef(s) || declaresDialect(s)
}

// declaresResidue reports whether s writes a keyword recordResidue keeps. It
// reads the raw nodes rather than the model fields for the reason
// declaresValueConstraints does: the recorder reads them there, and a predicate
// that consulted a different source could disagree with it in either direction.
func declaresResidue(s *oas3.Schema) bool {
	for _, keyword := range residueKeywords {
		if rawPropertyNode(s, keyword) != nil {
			return true
		}
	}
	return false
}

// declaresAnnotations reports whether s carries documentation, deprecation, XML
// hints, vendor extensions, or examples — the five TypeCommon fields
// attachDeclaredAnnotations fills.
func declaresAnnotations(s *oas3.Schema) bool {
	if s.GetTitle() != "" || s.GetDescription() != "" || s.GetExternalDocs() != nil {
		return true
	}
	if effectiveDeprecated(s, nil) || s.GetXML() != nil {
		return true
	}
	if ext := s.GetExtensions(); ext != nil && ext.Len() > 0 {
		return true
	}
	return s.GetExample() != nil || len(s.GetExamples()) > 0
}

// declaresValueConstraints reports whether s sets any keyword
// constraintsFromSchema reads. It does not call it: that reports a malformed
// bound, and a predicate must not emit diagnostics. The three numeric bounds
// are detected on their raw nodes for the same reason numericBounds reads them
// there — a magnitude beyond float64 leaves the model field nil while the
// keyword is plainly written.
func declaresValueConstraints(s *oas3.Schema) bool {
	for _, keyword := range []string{"minimum", "maximum", "multipleOf"} {
		if rawPropertyNode(s, keyword) != nil {
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
// keyword that fillValidationOnly keeps verbatim under Unmodeled. A `false`
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
	if rawPropertyNode(s, "dependentRequired") != nil {
		return true
	}
	up := s.GetUnevaluatedProperties()
	return s.GetUnevaluatedItems() != nil || (up != nil && !isFalseSchema(up))
}

// lowerSchemaBody lowers the body itself, handling the $dynamicRef expansion and
// the oneOf/anyOf dispatch that precede structural lowering.
func (l *lowerer) lowerSchemaBody(schema *oas3.Schema, pointer, hint string) ir.TypeRef {
	if target, why := l.dynamicExpansion(schema); why == "" {
		l.diag(ir.SeverityInfo, codeCompositionLowering, pointer+ptr("$dynamicRef"),
			"$dynamicRef expanded to %q, the one matching $dynamicAnchor in this document", target)
		return ir.TypeRef{Target: target, Nullable: schemaAdmitsNull(schema)}
	}
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			return ir.TypeRef{
				Target:   l.lowerCoDeclaredUnion(schema, pointer, hint),
				Nullable: schemaAdmitsNull(schema),
			}
		}
		return l.lowerOneOfAnyOf(schema, pointer, hint)
	}
	return ir.TypeRef{Target: l.lower(schema, pointer, hint), Nullable: schemaAdmitsNull(schema)}
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...) or
// a `required` list. When it does, the union alone cannot represent the schema,
// so the sibling body must be lowered too rather than dropped (invariant 2,
// ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	return len(s.GetRequired()) > 0 || declaresShape(s)
}

// declaresShape reports whether a schema declares data shape — a type, a
// property set, a value set, composition, or a catch-all — rather than only
// narrowing what an already-declared shape accepts. `required` is on the
// narrowing side of that line, which is why hasUnionSiblings counts it
// separately: it makes a schema's siblings structural, but on its own it
// declares nothing to build (ir-design §4.7).
func declaresShape(s *oas3.Schema) bool {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	if s.GetConst() != nil || len(s.GetEnum()) > 0 || len(s.GetAllOf()) > 0 {
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
func (l *lowerer) lowerBesideUnmodeledUnion(s *oas3.Schema, pointer, hint string, reason ir.UnmodeledReason, why string) ir.TypeID {
	inner := l.lower(s, pointer, hint)
	owner := inner
	if got, _ := l.types.Lookup(pointer); got != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		owner = l.internAlias(pointer, hint, ir.TypeRef{Target: inner}, nil)
	}
	l.preserveUnionSiblings(owner, s, pointer, reason, why)
	return owner
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Unmodeled. A validation-only union joins §4.7's keyword family and is reported
// with it; anything else is a §4.8 degradation and says so.
func (l *lowerer) preserveUnionSiblings(id ir.TypeID, s *oas3.Schema, pointer string, reason ir.UnmodeledReason, why string) {
	td, ok := l.registeredNode(id, pointer)
	if !ok {
		return
	}
	c := td.Common()
	for _, kw := range []string{"oneOf", "anyOf"} {
		raw := nodeToRaw(rawPropertyNode(s, kw))
		if reason == ir.ReasonValidationOnly {
			l.preserveKeyword(&c.Unmodeled, "openapi:"+kw, raw, pointer, pointer+ptr(kw), kw)
			continue
		}
		l.preserve(&c.Unmodeled, "openapi:"+kw, raw, reason, pointer+ptr(kw))
	}
	if reason == ir.ReasonValidationOnly {
		return
	}
	l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
		"oneOf/anyOf co-declared with structural keywords intersects with them, and %s; union branches kept verbatim under Unmodeled", why)
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing) and records one info diagnostic on first visit.
func (l *lowerer) falseSchema(pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		l.diag(ir.SeverityInfo, codeFalseSchema, pointer,
			"boolean false schema matches nothing; lowered as a closed empty model")
		return &ir.Model{TypeCommon: common, Additional: ir.AdditionalClosed}
	})
}

// lower interns the inline schema at pointer and returns its TypeID. Value
// constraints (const, enum) and allOf composition take precedence over the
// structural type; otherwise it dispatches on the effective (null-stripped)
// type set. const hoists through hoistLiteral — the same primitive that
// hoists each individual member of a heterogeneous enum (enumAsUnion) — since
// a bare `const` schema is exactly a Literal at its own pointer.
func (l *lowerer) lower(s *oas3.Schema, pointer, hint string) ir.TypeID {
	if c := s.GetConst(); c != nil {
		return l.hoistLiteral(c, pointer, hint)
	}
	if len(s.GetEnum()) > 0 {
		return l.lowerEnum(s, pointer, hint)
	}
	if len(s.GetAllOf()) > 0 {
		return l.lowerAllOf(s, pointer, hint)
	}
	types := effectiveTypes(s)
	switch {
	case len(types) > 1:
		return l.lowerUnion(s, pointer, hint, types)
	case len(types) == 1:
		return l.lowerTyped(s, pointer, hint, types[0])
	default:
		return l.lowerUntyped(s, pointer, hint)
	}
}

// lowerTyped dispatches a single-typed schema to its structural or scalar form.
func (l *lowerer) lowerTyped(s *oas3.Schema, pointer, hint string, st oas3.SchemaType) ir.TypeID {
	switch st {
	case oas3.SchemaTypeObject:
		return l.lowerModel(s, pointer, hint)
	case oas3.SchemaTypeArray:
		return l.lowerArray(s, pointer, hint)
	default:
		return l.scalarTypeID(s, st, pointer, hint)
	}
}

// lowerUntyped handles a schema with no declared type: a property set makes it a
// model; enum/const and composition are lowered by later passes; anything else
// is schemaless.
func (l *lowerer) lowerUntyped(s *oas3.Schema, pointer, hint string) ir.TypeID {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return l.lowerModel(s, pointer, hint)
	}
	return l.primID(ir.PrimAny)
}

// lowerUnion hoists a multi-typed schema (e.g. type: [string, integer]) as an
// exclusive, untagged union with one variant per declared type.
func (l *lowerer) lowerUnion(s *oas3.Schema, pointer, hint string, types []oas3.SchemaType) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ptr("type", strconv.Itoa(i))
			variants = append(variants, ir.Variant{
				Name: ir.Naming{Hint: string(st)},
				Type: ir.TypeRef{Target: l.lowerTyped(s, vptr, hint, st)},
			})
		}
		return &ir.Union{
			TypeCommon: common,
			Variants:   variants,
			Exclusive:  true,
		}
	})
}

// lowerModel hoists an object schema as a Model. This task lowers only the
// property shape (name, type, required) and the property-set cardinality; a
// later pass fills the rest. It reads no annotations: attachDeclaredAnnotations
// does that for every destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func (l *lowerer) lowerModel(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		m := &ir.Model{TypeCommon: common, Constraints: l.schemaConstraints(s, pointer)}
		l.fillModelProperties(m, s, pointer)
		l.fillAdditional(m, s, pointer, hint)
		if d := l.lowerDiscriminator(s, m, pointer); d != nil {
			m.Discriminator = d
		}
		return m
	})
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
func (l *lowerer) fillModelProperties(m *ir.Model, s *oas3.Schema, pointer string) {
	props := s.GetProperties()
	if props == nil {
		return
	}
	required := requiredSet(s.GetRequired())
	byWire := wireNameIndex(m.Properties)
	for name, js := range props.All() {
		ppointer := pointer + ptr("properties", name)
		p := ir.Property{
			ID:         propID(ppointer),
			Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
			WireName:   name,
			Type:       l.carriedSchemaRef(js, ppointer, name),
			Required:   required[name],
			Provenance: ir.Provenance{Source: l.srcIndex, Pointer: ppointer},
		}
		l.fillPropertyDetail(&p, js, ppointer)
		l.merge.mergeProperty(m, byWire, p, ppointer)
	}
}

// fillPropertyDetail enriches a property from its schema: the property-scoped
// facts a type node has no field for (default, visibility, secrecy,
// constraints), then the declaration's annotations. Annotations present at a
// $ref use-site override the target's (ir-design §14).
//
// Constraints stay unconditional: no node a property's schema can hoist holds
// them for it — an object lowers to a Model and a formatted scalar to a Scalar
// with an Encoding, neither of which carries the scalar bounds
// constraintsFromSchema reads — so ir.Property is their only home.
func (l *lowerer) fillPropertyDetail(p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer string) {
	ref := js.GetSchema()
	if ref == nil {
		return
	}
	tgt := l.refTargetSchema(js, ref)
	l.fillPropertyDefault(p, ref, tgt, pointer)
	if ref.GetFormat() == "password" {
		p.Secret = true
	}
	p.Visibility = effectiveVisibility(ref, tgt)
	l.fillPropertyConstraints(p, ref, pointer)
	l.fillPropertyAnnotations(p, ref, tgt, pointer)
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
func (l *lowerer) fillPropertyAnnotations(p *ir.Property, ref, tgt *oas3.Schema, pointer string) {
	if l.loweredToOwnNode(pointer, p.Type) {
		return
	}
	a, diags := annotations(site{Kind: siteReference, Node: ref, Referent: tgt}, pointer, l.srcIndex)
	l.diags.AppendAll(diags)

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
	p.Unmodeled = mergeUnmodeled(p.Unmodeled, a.Unmodeled)
	// nil node: this arm runs only when the schema lowered to no node of its own,
	// so nothing here can be carrying an Encoding.
	l.recordUnplacedContent(&p.Unmodeled, ref, nil, pointer)
	l.recordUnexpandedDynamicRef(&p.Unmodeled, ref, pointer)
}

// loweredToOwnNode reports whether the declaration at pointer lowered to a type
// node of its own — the node attachDeclaredAnnotations then fills, leaving its
// carrier nothing to hold.
//
// It asks whether the node interned at pointer is the one the declaration
// lowered to, not merely whether a node is interned there. A $ref naming an
// inline position hoists that position's home for its own use, in either
// declaration order, and a carrier reading the registry alone would keep its
// schema's annotations only when it happened to lower first.
func (l *lowerer) loweredToOwnNode(pointer string, t ir.TypeRef) bool {
	id, owned := l.types.Lookup(pointer)
	return owned && id == t.Target
}

// fillPropertyDefault sets the property default, preferring the use-site node
// over the $ref target's; an unconvertible node yields a diagnostic.
func (l *lowerer) fillPropertyDefault(p *ir.Property, ref, tgt *oas3.Schema, pointer string) {
	node := ref.GetDefault()
	if node == nil && tgt != nil {
		node = tgt.GetDefault()
	}
	if node == nil {
		return
	}
	v, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer, "default: %s", err.Error())
		return
	}
	p.Default = &v
}

// fillPropertyConstraints attaches the property's scalar constraints and stamps
// each constraint diagnostic with the property's provenance.
func (l *lowerer) fillPropertyConstraints(p *ir.Property, ref *oas3.Schema, pointer string) {
	c, diags := constraintsFromSchema(ref, l.exclusiveBoundIsBoolean())
	l.appendConstraintDiags(diags, pointer)
	if c != nil {
		p.Constraints = c
	}
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
// annotations stay with the declaring property (fillPropertyDetail), and the
// shared primitive must never carry a per-declaration annotation. Ownership is
// checked before conversion because the callers cover a pointer in either
// order, and only the one that finds a node may emit conversion diagnostics.
func (l *lowerer) attachDeclaredAnnotations(s *oas3.Schema, pointer string) {
	// NodeAt rather than Lookup-then-registeredNode: the coordinate and its node
	// are recorded together, so there is no state where the first resolves and
	// the second does not, and a branch for one could never be reached.
	td, ok := l.types.NodeAt(pointer)
	if !ok {
		return
	}
	a, diags := annotations(site{Kind: siteDeclaration, Node: s}, pointer, l.srcIndex)
	l.diags.AppendAll(diags)

	c := td.Common()
	c.Docs = a.Docs
	if a.Deprecated {
		c.Deprecation = &ir.Deprecation{}
	}
	if a.XML != nil {
		c.XML = a.XML
	}
	c.Unmodeled = mergeUnmodeled(c.Unmodeled, a.Unmodeled)
	if len(a.Examples) > 0 {
		c.Examples = a.Examples
	}
	l.recordUnplacedContent(&c.Unmodeled, s, td, pointer)
	l.recordUnexpandedDynamicRef(&c.Unmodeled, s, pointer)
}

// appendExample converts node into proto's value and appends the result to out;
// an unconvertible node is skipped with a warning diagnostic rather than silently
// dropped — an example is an annotation, not a structural hole, so losing it is
// fine as long as it isn't silent. proto carries the annotations that surround
// the value (name, summary, description); base and seg locate the node, joined
// into a pointer only on the failure path, so an example that converts builds no
// pointer string at all. Shared by every example site: schema (schemaExamples),
// media type, header, and parameter (exampleList).
func (l *lowerer) appendExample(out []ir.Example, proto ir.Example, node *yaml.Node, base string, seg ...string) []ir.Example {
	v, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, base+ptr(seg...), "example: %s", err.Error())
		return out
	}
	proto.Value = &v
	return append(out, proto)
}

// fillAdditional lowers additionalProperties, patternProperties, and
// unevaluatedProperties into the model's openness and catch-all shape.
func (l *lowerer) fillAdditional(m *ir.Model, s *oas3.Schema, pointer, hint string) {
	ap := s.GetAdditionalProperties()
	switch {
	case isFalseSchema(ap):
		m.Additional = ir.AdditionalClosed
	case ap != nil && !ap.IsBool():
		ref := l.schemaRef(ap, pointer+ptr("additionalProperties"), hint+"_value")
		m.AdditionalProps = &ir.AdditionalProps{Value: ref}
	}
	if patterns := l.patternProps(s, pointer, hint); len(patterns) > 0 {
		if m.AdditionalProps == nil {
			m.AdditionalProps = &ir.AdditionalProps{Value: l.primRef(ir.PrimAny)}
		}
		m.AdditionalProps.Patterns = patterns
	}
	if isFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
}

// patternProps lowers patternProperties into pattern/value bindings in source
// order.
func (l *lowerer) patternProps(s *oas3.Schema, pointer, hint string) []ir.PatternProps {
	pp := s.GetPatternProperties()
	if pp == nil || pp.Len() == 0 {
		return nil
	}
	out := make([]ir.PatternProps, 0, pp.Len())
	for pattern, js := range pp.All() {
		ref := l.schemaRef(js, pointer+ptr("patternProperties", pattern), hint+"_pattern")
		out = append(out, ir.PatternProps{Pattern: pattern, Value: ref})
	}
	return out
}

// preserve records raw under key in *p with why it was kept and where it was
// written, allocating the map on first write. An absent or unconvertible
// payload records nothing, so no caller needs a nil guard of its own.
func (l *lowerer) preserve(p *ir.Unmodeled, key string, raw ir.RawValue, reason ir.UnmodeledReason, pointer string) {
	preserveInto(p, key, raw, reason, pointer, l.srcIndex)
}

// preserveKeyword records a validation-only keyword's raw payload under key in
// p and emits one info diagnostic naming it at declPtr, the schema that wrote
// it. An absent or unconvertible payload records nothing and reports nothing.
//
// entryPtr locates the entry itself, which is what a validation emitter reports
// against: the keyword's own node where the source writes the entry as one
// keyword, and declPtr where a §4.7 entry combines several keywords into one
// synthesized object that no single node addresses.
func (l *lowerer) preserveKeyword(p *ir.Unmodeled, key string, raw ir.RawValue, declPtr, entryPtr, label string) {
	l.diags.AppendAll(preserveKeywordInto(p, key, raw, declPtr, entryPtr, label, l.srcIndex))
}

// diag appends one diagnostic at pointer with the given severity and code,
// stamping it with l.srcIndex. It is the single primitive for constructing a
// Provenance from l.srcIndex — lowering sites should use it instead of
// hand-writing the append+diagf+Provenance triple.
func (l *lowerer) diag(sev ir.Severity, code, pointer, format string, args ...any) {
	l.appendDiag(diagf(sev, code, ir.Provenance{Source: l.srcIndex, Pointer: pointer}, format, args...))
}

// appendDiag records d unless one identical to it — same severity, code,
// provenance and message — was already recorded. It is the single append point
// for lowering diagnostics, so a shared declaration reported from N use sites
// yields one diagnostic rather than N indistinguishable copies.
func (l *lowerer) appendDiag(d ir.Diagnostic) { l.diags.Append(d) }

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema with its collection constraints.
func (l *lowerer) lowerArray(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			return l.buildTuple(s, common, pointer, hint, prefix)
		}
		list := &ir.List{
			TypeCommon:  common,
			Elem:        l.schemaRef(s.GetItems(), pointer+ptr("items"), hint+"_item"),
			Constraints: listConstraints(s),
		}
		return list
	})
}

// buildTuple lowers prefixItems into a Tuple. A trailing `items` schema makes
// the source an *open* tuple — a fixed positional head plus a homogeneous tail
// — and the IR has no combinator for that: Tuple is fixed-arity, List is
// homogeneous, and there is no node that is both. The head lowers to a Tuple,
// which is the documented weaker shape, and the tail is kept beside it so the
// arity the Tuple now asserts falsely stays recoverable (ir-design §4.8).
func (l *lowerer) buildTuple(s *oas3.Schema, common ir.TypeCommon, pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) ir.TypeDef {
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		elems = append(elems, l.schemaRef(ps, pointer+ptr("prefixItems", strconv.Itoa(i)), hint+"_"+strconv.Itoa(i)))
	}
	t := &ir.Tuple{TypeCommon: common, Elems: elems}
	if s.GetItems() == nil {
		return t
	}
	l.preserve(&t.Unmodeled, "openapi:items-after-prefix", nodeToRaw(rawPropertyNode(s, "items")),
		ir.ReasonDegradedLowering, pointer+ptr("items"))
	l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer,
		"items after prefixItems is an open tuple; lowered as a fixed-arity Tuple with the tail kept under Unmodeled")
	return t
}

// scalarTypeID maps a scalar (type, format) pair to a TypeID via formatTable: a
// known pairing interns the shared primitive; byte, an unknown format, and the
// 2020-12 content vocabulary each hoist a named Scalar wrapping the base
// primitive with an Encoding, so what the position wrote never leaks onto the
// shared primitive every other declaration of that type also resolves to.
func (l *lowerer) scalarTypeID(s *oas3.Schema, st oas3.SchemaType, pointer, hint string) ir.TypeID {
	format := s.GetFormat()
	if st == oas3.SchemaTypeString && format == "byte" {
		return l.hoistByteScalar(s, pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	prim, known := formatTable[key]
	if !known {
		return l.hoistFormatScalar(s, baseForType(st), format, pointer, hint)
	}
	if !declaresContent(s) {
		return l.primID(prim)
	}
	return l.hoistContentScalar(s, prim, pointer, hint)
}

// hoistByteScalar hoists a base64-encoded byte scalar (string+byte).
func (l *lowerer) hoistByteScalar(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := l.primRef(ir.PrimBytes)
		wire := l.primRef(ir.PrimString)
		enc := l.scalarEncoding(s, "base64", &common, pointer)
		enc.WireType = &wire
		return &ir.Scalar{TypeCommon: common, Base: &base, Encoding: enc}
	})
}

// hoistFormatScalar hoists a scalar over base carrying an unknown format as its
// encoding name, preserving the format losslessly.
func (l *lowerer) hoistFormatScalar(s *oas3.Schema, base ir.PrimKind, format, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		baseRef := l.primRef(base)
		enc := l.scalarEncoding(s, format, &common, pointer)
		return &ir.Scalar{TypeCommon: common, Base: &baseRef, Encoding: enc}
	})
}

// hoistContentScalar hoists a scalar over the shared primitive a known
// (type, format) pair maps to, giving the content vocabulary written here a node
// of its own to sit on.
func (l *lowerer) hoistContentScalar(s *oas3.Schema, prim ir.PrimKind, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		base := l.primRef(prim)
		enc := l.scalarEncoding(s, "", &common, pointer)
		return &ir.Scalar{TypeCommon: common, Base: &base, Encoding: enc}
	})
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
func (l *lowerer) scalarEncoding(s *oas3.Schema, formatName string, c *ir.TypeCommon, pointer string) *ir.Encoding {
	enc := &ir.Encoding{Name: formatName, MediaType: s.GetContentMediaType()}
	content := s.GetContentEncoding()
	if content == "" || content == formatName {
		return enc
	}
	enc.Name = content
	if formatName == "" {
		return enc
	}
	at := pointer + ptr("format")
	l.preserve(&c.Unmodeled, "openapi:format", nodeToRaw(rawPropertyNode(s, "format")),
		ir.ReasonNoIRHome, at)
	l.diag(ir.SeverityInfo, codeDegradedConstruct, at,
		"format and contentEncoding both name an encoding and ir.Encoding holds one; "+
			"contentEncoding %q is lowered and format is kept verbatim under Unmodeled", content)
	return enc
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
func (l *lowerer) recordUnplacedContent(p *ir.Unmodeled, s *oas3.Schema, td ir.TypeDef, pointer string) {
	if !declaresContent(s) || scalarHasEncoding(td) {
		return
	}
	for _, keyword := range contentKeywords {
		raw := nodeToRaw(rawPropertyNode(s, keyword))
		if len(raw) == 0 {
			continue
		}
		l.preserve(p, "openapi:"+keyword, raw, ir.ReasonNoIRHome, pointer+ptr(keyword))
		l.diag(ir.SeverityInfo, codeDegradedConstruct, pointer+ptr(keyword),
			"%s encodes a string value and this position lowered to a shape with no "+
				"Encoding field; kept verbatim under Unmodeled", keyword)
	}
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

// dynamicExpansion resolves the $dynamicRef s writes to the type it names, or
// reports why it is irreducible; a schema writing no $dynamicRef is irreducible
// with the cheapest possible reason, so no caller pays for the index.
//
// Dynamic scope is static per document (ir-design §4.7): the set of
// $dynamicAnchor declarations is fixed before evaluation, so a name declared
// exactly once is the only match any dynamic scope containing a match can hold —
// which makes it JSON Schema 2020-12 §8.2.3.2's "outermost scope with a matching
// anchor" whatever path evaluation took. Two declarations of one name put the
// target back under the evaluation path's control, and no static lowering can
// pick between them; those are the irreducible cases §4.7 preserves.
//
// A $dynamicRef co-declared with a $ref, a oneOf/anyOf, or a shape of its own is
// a conjunction of applicators the IR has no node for, so it is irreducible too
// — recordUnexpandedDynamicRef keeps the reference beside the shape that did
// lower. The one caller that expands and the one that preserves both decide
// through this function, so neither can act on a verdict the other did not
// reach.
func (l *lowerer) dynamicExpansion(s *oas3.Schema) (ir.TypeID, string) {
	node := rawPropertyNode(s, "$dynamicRef")
	if node == nil {
		return "", "no $dynamicRef is written here"
	}
	if s.Ref != nil || len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 || declaresShape(s) {
		return "", "it is co-declared with another applicator, which the IR cannot intersect with it"
	}
	if node.Kind != yaml.ScalarNode {
		return "", "its value is not a reference string"
	}
	return l.dynamicAnchorTarget(node.Value)
}

// dynamicAnchorTarget resolves one $dynamicRef reference string against the
// document's $dynamicAnchor index.
//
// Only an anchor declared on a top-level component schema resolves: that is the
// one target whose TypeID is stable without reading the registry, so expansion
// cannot depend on which position happened to lower first (ir-design §4.3). An
// anchor deeper in the document is reported as irreducible rather than resolved
// order-dependently.
func (l *lowerer) dynamicAnchorTarget(ref string) (ir.TypeID, string) {
	name, ok := dynamicFragment(ref)
	if !ok {
		return "", fmt.Sprintf("%q is not a plain same-document fragment", ref)
	}
	at := l.dynamicAnchorIndex()[name]
	if len(at) == 0 {
		return "", fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name)
	}
	if len(at) > 1 {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the evaluation path",
			name, len(at))
	}
	id, resolved, handled := l.resolveComponentRef(at[0])
	if !handled || !resolved {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared at %q rather than on a component schema", name, at[0])
	}
	return id, ""
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
func (l *lowerer) recordUnexpandedDynamicRef(p *ir.Unmodeled, s *oas3.Schema, pointer string) {
	_, why := l.dynamicExpansion(s)
	raw := nodeToRaw(rawPropertyNode(s, "$dynamicRef"))
	if why == "" || len(raw) == 0 {
		return
	}
	at := pointer + ptr("$dynamicRef")
	l.preserve(p, "openapi:$dynamicRef", raw, ir.ReasonDegradedLowering, at)
	l.diag(ir.SeverityInfo, codeDegradedConstruct, at,
		"$dynamicRef was not expanded because %s; it is kept verbatim under Unmodeled", why)
}

// declaresDynamicRef reports whether s writes a $dynamicRef, so a position that
// wrote one owns a node to keep an unexpanded reference on.
func declaresDynamicRef(s *oas3.Schema) bool {
	return rawPropertyNode(s, "$dynamicRef") != nil
}

// dynamicAnchorIndex returns the document's $dynamicAnchor index, building it on
// first use.
func (l *lowerer) dynamicAnchorIndex() map[string][]string {
	if l.dynamicAnchors == nil {
		l.dynamicAnchors = dynamicAnchors(l.doc.GetRootNode())
	}
	return l.dynamicAnchors
}

// dynamicAnchors indexes every $dynamicAnchor in the raw source by name, mapping
// it to the JSON pointers that declare it, in document order. It reads the raw
// tree because oas3.Schema has no field for the keyword at v1.24.0 — the reason
// nothing expanded a $dynamicRef before.
//
// It does not follow YAML aliases. An anchored mapping reached only through an
// alias is left out of the index, so a $dynamicRef naming it reads as
// irreducible and is preserved rather than resolved — the safe direction — and
// the walk stays bounded by the raw tree alone.
func dynamicAnchors(root *yaml.Node) map[string][]string {
	out := map[string][]string{}
	collectDynamicAnchors(root, "", out, 0)
	return out
}

// collectDynamicAnchors walks n, recording each $dynamicAnchor it declares under
// the pointer of the mapping that declares it.
func collectDynamicAnchors(n *yaml.Node, pointer string, out map[string][]string, depth int) {
	if n == nil || depth > maxDynamicAnchorDepth {
		return
	}
	if n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			collectDynamicAnchors(c, pointer, out, depth+1)
		}
		return
	}
	if n.Kind == yaml.SequenceNode {
		for i, c := range n.Content {
			collectDynamicAnchors(c, pointer+ptr(strconv.Itoa(i)), out, depth+1)
		}
		return
	}
	if n.Kind != yaml.MappingNode {
		return
	}
	collectMappingAnchors(n, pointer, out, depth)
}

// collectMappingAnchors reads one mapping's pairs: a $dynamicAnchor scalar names
// this mapping, and every other value is walked in turn.
func collectMappingAnchors(n *yaml.Node, pointer string, out map[string][]string, depth int) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue // a non-scalar key cannot name a schema keyword
		}
		if key.Value == "$dynamicAnchor" {
			if val.Kind == yaml.ScalarNode && val.Value != "" {
				out[val.Value] = append(out[val.Value], pointer)
			}
			continue
		}
		collectDynamicAnchors(val, pointer+ptr(key.Value), out, depth+1)
	}
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
func nullUnionCollapse(s *oas3.Schema, pointer, hint string) (*oas3.JSONSchema[oas3.Referenceable], string, string, bool) {
	variants, key := s.GetOneOf(), "oneOf"
	if len(variants) == 0 {
		variants, key = s.GetAnyOf(), "anyOf"
	}
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
	return nonNull, pointer + ptr(key, strconv.Itoa(nonNullIdx)), hint, true
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

// nodeToRaw converts a YAML node to canonical JSON for lossless preservation in
// Unmodeled; a nil node or an unconvertible node yields nil.
func nodeToRaw(node *yaml.Node) ir.RawValue {
	if node == nil {
		return nil
	}
	var v any
	if err := node.Decode(&v); err != nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return ir.RawValue(data)
}

// effectiveDeprecated reports the deprecated flag, use-site over referent.
func effectiveDeprecated(ref, tgt *oas3.Schema) bool {
	return pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.Deprecated })
}

// effectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (ir-design §5.2): readOnly is present in every response lifecycle
// (read/delete/query) and absent only from requests; writeOnly is create+update.
func effectiveVisibility(ref, tgt *oas3.Schema) ir.Visibility {
	switch {
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.ReadOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	case pickFlag(ref, tgt, func(s *oas3.Schema) *bool { return s.WriteOnly }):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	default:
		return ir.Visibility{}
	}
}

// pickFlag returns the bool field extracted by accessor from ref when present,
// else from tgt, else false. It is the single nil-safe "use-site overrides
// referent" primitive for boolean schema flags (readOnly, writeOnly, deprecated).
func pickFlag(ref, tgt *oas3.Schema, accessor func(*oas3.Schema) *bool) bool {
	if ref != nil {
		if v := accessor(ref); v != nil {
			return *v
		}
	}
	if tgt != nil {
		if v := accessor(tgt); v != nil {
			return *v
		}
	}
	return false
}

// fillTypeDocs maps a schema's title, description, and externalDocs onto Docs.
// Every field is assigned, never accumulated: a schema declares at most one
// externalDocs, and a pointer read twice (a sub-schema lowered both in place
// and through a $ref that hoists it) must not end up with two copies of it.
func fillTypeDocs(d *ir.Docs, s *oas3.Schema) {
	if t := s.GetTitle(); t != "" {
		d.Summary = t
	}
	if desc := s.GetDescription(); desc != "" {
		d.Description = desc
	}
	if ed := s.GetExternalDocs(); ed != nil {
		d.ExternalDocs = []ir.Link{{URL: ed.GetURL(), Description: ed.GetDescription()}}
	}
}

// fillCarrierDocs fills the ir.Property or ir.Parameter carrying a position with
// the documentation effective there: the $ref referent's title, description and
// externalDocs first, then the use-site's over them, field by field. A carrier
// therefore ends up with documentation the position itself need not have
// written — a bare `$ref` reads all three from the referent, which keeps its own
// copy on its node.
//
// Both halves are deliberate. The use-site half is the only home a keyword
// written *at* the position has once the body reduced to a shared node
// (GitHub #116): dropping it there loses it outright. The referent half is
// ir-design §14 — ref-target annotations merge onto the referencing
// Property/Parameter with use-site precedence, applied uniformly — and it is how
// every other field this compiler reads through a $ref already behaves
// (fillPropertyDefault, effectiveVisibility, effectiveDeprecated).
//
// Uniform is the load-bearing word: description alone inheriting, while title
// and externalDocs stop at the position, is the ad-hoc per-keyword patching §14
// names as the counterexample to avoid.
func fillCarrierDocs(d *ir.Docs, ref, tgt *oas3.Schema) {
	if tgt != nil {
		fillTypeDocs(d, tgt)
	}
	if ref != nil {
		fillTypeDocs(d, ref)
	}
}

// xmlHints maps an OpenAPI XML object onto ir.XMLHints; an attribute flag becomes
// the "attribute" node type. Fields are read directly rather than via the
// library getters, which dereference unset (nil) field pointers.
func xmlHints(x *oas3.XML) *ir.XMLHints {
	if x == nil {
		return nil
	}
	h := &ir.XMLHints{}
	if x.Name != nil {
		h.Name = *x.Name
	}
	if x.Namespace != nil {
		h.Namespace = *x.Namespace
	}
	if x.Prefix != nil {
		h.Prefix = *x.Prefix
	}
	if x.Wrapped != nil {
		h.Wrapped = *x.Wrapped
	}
	if x.Attribute != nil && *x.Attribute {
		h.NodeType = "attribute"
	}
	return h
}

// extensionsFrom lowers an x-* extension map into namespaced ir.Unmodeled, keys
// prefixed "openapi:" and values serialized to raw JSON. owner is the pointer of
// the object the extensions were written on; each entry is located at its own
// key beneath it and marked ReasonVendorExtension, since the format assigns an
// x-* key no semantics at all.
func extensionsFrom(ext *extensions.Extensions, srcIndex int, owner string) (ir.Unmodeled, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	out := ir.Unmodeled{}
	var diags []ir.Diagnostic
	for name, node := range ext.All() {
		raw := nodeToRaw(node)
		if raw == nil {
			diags = append(diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
				ir.Provenance{Source: srcIndex, Pointer: owner},
				"extension %q could not be serialized", name))
			continue
		}
		out["openapi:"+name] = ir.UnmodeledEntry{
			Reason:     ir.ReasonVendorExtension,
			Value:      raw,
			Provenance: ir.Provenance{Source: srcIndex, Pointer: owner + ptr(name)},
		}
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// extensions lowers ext's x-* extensions into namespaced Unmodeled, recording
// any serialization-failure diagnostics unconditionally even when the result
// is empty. Every lowering site should call this rather than extensionsFrom
// directly: gating the diagnostic append behind the same "len(ext) > 0" that
// guards the assignment would drop every warning on an object whose
// extensions all failed to serialize — exactly when the result is empty.
func (l *lowerer) extensions(ext *extensions.Extensions, owner string) ir.Unmodeled {
	out, diags := extensionsFrom(ext, l.srcIndex, owner)
	l.diags.AppendAll(diags)
	return out
}

// mergeUnmodeled overlays src onto dst, allocating dst on first write.
func mergeUnmodeled(dst, src ir.Unmodeled) ir.Unmodeled {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = ir.Unmodeled{}
	}
	maps.Copy(dst, src)
	return dst
}

// isFalseSchema reports whether js is the boolean `false` schema.
func isFalseSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && js.GetBool() != nil && !*js.GetBool()
}

// ifThenElseRaw combines the present if/then/else arms into one raw JSON object.
func ifThenElseRaw(s *oas3.Schema) ir.RawValue {
	return jsonObject(presentMembers(s, "if", "then", "else"))
}

// containsRaw combines contains/minContains/maxContains into one raw JSON object.
func containsRaw(s *oas3.Schema) ir.RawValue {
	if s.GetContains() == nil && s.GetMinContains() == nil && s.GetMaxContains() == nil {
		return nil
	}
	return jsonObject(presentMembers(s, "contains", "minContains", "maxContains"))
}

// unevaluatedRaw combines a non-false unevaluatedProperties and any
// unevaluatedItems into one raw JSON object (a false unevaluatedProperties is a
// structural mode, handled in fillAdditional).
func unevaluatedRaw(s *oas3.Schema) ir.RawValue {
	var members []rawMember
	if up := s.GetUnevaluatedProperties(); up != nil && !isFalseSchema(up) {
		if raw := nodeToRaw(rawPropertyNode(s, "unevaluatedProperties")); raw != nil {
			members = append(members, rawMember{key: "unevaluatedProperties", val: raw})
		}
	}
	if s.GetUnevaluatedItems() != nil {
		if raw := nodeToRaw(rawPropertyNode(s, "unevaluatedItems")); raw != nil {
			members = append(members, rawMember{key: "unevaluatedItems", val: raw})
		}
	}
	return jsonObject(members)
}

// presentMembers collects the given keywords that are present on s as raw JSON
// members, preserving the requested order.
func presentMembers(s *oas3.Schema, keys ...string) []rawMember {
	var members []rawMember
	for _, k := range keys {
		if raw := nodeToRaw(rawPropertyNode(s, k)); raw != nil {
			members = append(members, rawMember{key: k, val: raw})
		}
	}
	return members
}

// rawPropertyNode returns the raw YAML value node of a top-level schema keyword,
// or nil when absent. The library's GetPropertyNode resolves Go core field names
// and returns key nodes; this scans the schema's root mapping for the on-wire
// keyword and returns its value node, which is where exact literals live.
func rawPropertyNode(s *oas3.Schema, key string) *yaml.Node {
	if s == nil {
		return nil
	}
	return rawChildNode(s.GetRootNode(), key)
}

// rawMember is one key/raw-JSON pair of a combined validation-only object.
type rawMember struct {
	key string
	val ir.RawValue
}

// jsonObject renders ordered raw members into a JSON object, or nil when empty.
func jsonObject(members []rawMember) ir.RawValue {
	if len(members) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return ir.RawValue(b.String())
}

// canonicalWords renders name as a neutral lower_snake word sequence: it splits
// on _/-/space and on camel-case and letter/digit boundaries, lowercases, and
// joins with "_". It holds no acronym opinion beyond boundary detection; casing
// policy is a emitter concern.
func canonicalWords(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' || r == ' ' {
			flush()
			continue
		}
		if len(cur) > 0 && wordBoundary(cur[len(cur)-1], r, runes, i) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return strings.Join(words, "_")
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "userID" -> user|ID
	case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit: "APIKey2" -> ...Key|2
	default:
		return false
	}
}
