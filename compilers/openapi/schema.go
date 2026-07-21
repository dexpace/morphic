package openapi

import (
	"encoding/json"
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
	ref := l.schemaRef(js, pointer, name)
	if _, owned := l.byPointer[pointer]; owned {
		return // schemaRef interned the component's own node at its component ID
	}
	l.internAlias(pointer, name, ref, l.componentConstraints(js, pointer))
}

// componentConstraints reads the value constraints of a component schema whose
// body reduced to a shared or referenced target. A top-level scalar component
// (e.g. {minimum: 5} or {type: number, minimum: 5}) reduces to a shared
// primitive, so unlike a property — whose constraints land on the Property — it
// has no other node to hold them; the alias Scalar must carry them or they are
// silently dropped.
func (l *lowerer) componentConstraints(js *oas3.JSONSchema[oas3.Referenceable], pointer string) *ir.Constraints {
	if js == nil || js.IsBool() || js.IsReference() {
		return nil
	}
	s := js.GetSchema()
	if s == nil || s.Ref != nil {
		return nil
	}
	c, diags := constraintsFromSchema(s)
	for i := range diags {
		diags[i].Provenance = ir.Provenance{Source: l.srcIndex, Pointer: pointer}
	}
	l.diags = append(l.diags, diags...)
	return c
}

// internAlias interns a named Scalar at pointer whose Base is target, so a
// component (or a sibling-carrying schema) whose body lowered to a shared or
// referenced target still owns a resolvable node at its own TypeID. Any value
// constraints the schema carried are attached so a scalar component never drops
// them.
func (l *lowerer) internAlias(pointer, hint string, target ir.TypeRef, constraints *ir.Constraints) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		base := target
		return &ir.Scalar{TypeCommon: l.commonFor(id, pointer, hint), Base: &base, Constraints: constraints}
	})
}

// schemaRef is THE schema entry point: every schema position (property, items,
// params, bodies) flows through it, yielding a TypeRef into the type registry.
// It normalizes the two nullability dialects onto the single IR bit and never
// lowers a $ref target from the reference site.
func (l *lowerer) schemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer, hint string) ir.TypeRef {
	l.depth++
	defer func() { l.depth-- }()
	if l.depth > maxSchemaDepth {
		l.diags = append(l.diags, diagf(ir.SeverityError, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer},
			"schema nesting exceeds %d; lowered as any", maxSchemaDepth))
		return l.primRef(ir.PrimAny)
	}
	if js == nil {
		return l.primRef(ir.PrimAny)
	}
	if js.IsBool() {
		if b := js.GetBool(); b != nil && !*b {
			return ir.TypeRef{Target: l.falseSchema(pointer, hint)}
		}
		return l.primRef(ir.PrimAny)
	}
	if js.IsReference() {
		return l.refTypeRef(js, pointer)
	}
	// Past the IsBool check, the either's left schema is always set (an empty
	// either reads as a bool), so GetSchema never returns nil here.
	schema := js.GetSchema()
	if schema.Ref != nil {
		return l.refTypeRef(js, pointer)
	}
	return l.schemaBody(schema, pointer, hint)
}

// schemaBody lowers a concrete (non-reference) schema body to a TypeRef, handling
// the oneOf/anyOf dispatch that precedes structural lowering. It is shared by
// schemaRef and by sub-schema hoisting (resolveSchemaRef), which both reach a
// body only after peeling off any leading $ref.
func (l *lowerer) schemaBody(schema *oas3.Schema, pointer, hint string) ir.TypeRef {
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		if hasUnionSiblings(schema) {
			return ir.TypeRef{
				Target:   l.lowerWithUnionSiblings(schema, pointer, hint),
				Nullable: schemaHasNull(schema),
			}
		}
		return l.lowerOneOfAnyOf(schema, pointer, hint)
	}
	return ir.TypeRef{Target: l.lower(schema, pointer, hint), Nullable: schemaHasNull(schema)}
}

// hasUnionSiblings reports whether a oneOf/anyOf schema also carries structural
// keywords (a type, properties, allOf, const/enum, additionalProperties, ...).
// When it does, the union alone cannot represent the schema, so the structural
// body must be lowered too rather than dropped (invariant 2, ir-design §4.3).
func hasUnionSiblings(s *oas3.Schema) bool {
	if props := s.GetProperties(); props != nil && props.Len() > 0 {
		return true
	}
	if len(s.GetRequired()) > 0 || len(s.GetAllOf()) > 0 {
		return true
	}
	if s.GetConst() != nil || len(s.GetEnum()) > 0 {
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

// lowerWithUnionSiblings lowers the structural body of a schema that co-declares
// oneOf/anyOf, then preserves the union branches verbatim under the resulting
// node's Extensions with an info diagnostic — so neither the structural shape
// nor the union is dropped (ir-design §4.7-style preservation).
func (l *lowerer) lowerWithUnionSiblings(s *oas3.Schema, pointer, hint string) ir.TypeID {
	inner := l.lower(s, pointer, hint)
	owner := inner
	if l.byPointer[pointer] != inner {
		// The structural body reduced to a shared/aliased target; hoist an alias
		// so the preserved union attaches to a node this pointer owns, never to a
		// shared primitive.
		owner = l.internAlias(pointer, hint, ir.TypeRef{Target: inner}, nil)
	}
	l.preserveUnionSiblings(owner, s, pointer)
	return owner
}

// preserveUnionSiblings stores the raw oneOf/anyOf of s under the owning node's
// Extensions and emits one info diagnostic naming the preserved construct.
func (l *lowerer) preserveUnionSiblings(id ir.TypeID, s *oas3.Schema, pointer string) {
	td, ok := l.out.Types[id]
	if !ok {
		return
	}
	c := td.Common()
	if len(s.GetOneOf()) > 0 {
		if raw := nodeToRaw(rawPropertyNode(s, "oneOf")); raw != nil {
			c.Extensions = mergeExtensions(c.Extensions, ir.Extensions{"openapi:oneOf": raw})
		}
	}
	if len(s.GetAnyOf()) > 0 {
		if raw := nodeToRaw(rawPropertyNode(s, "anyOf")); raw != nil {
			c.Extensions = mergeExtensions(c.Extensions, ir.Extensions{"openapi:anyOf": raw})
		}
	}
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
		ir.Provenance{Source: l.srcIndex, Pointer: pointer},
		"oneOf/anyOf co-declared with structural keywords; union branches preserved verbatim under extensions"))
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. A top-level component target keeps
// its stable named ID (lowered where it is defined); an internal sub-schema
// target is hoisted at its pointer-derived ID so the reference never dangles. A
// genuinely external or unresolvable target is diagnosed and dropped to any.
func (l *lowerer) refTypeRef(js *oas3.JSONSchema[oas3.Referenceable], pointer string) ir.TypeRef {
	ref := js.GetRef().String()
	id, ok := l.resolveSchemaRef(js, ref)
	if !ok {
		l.diags = append(l.diags, diagf(ir.SeverityError, codeUnresolvedRef,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer},
			"unresolved $ref %q", ref))
		return l.primRef(ir.PrimAny)
	}
	return ir.TypeRef{Target: id, Nullable: l.refNullable(js)}
}

// resolveSchemaRef resolves a schema-position $ref to an interned TypeID, never
// synthesizing an ID that nothing backs. A top-level component keeps its stable
// named ID; an already-interned target reuses it; an internal sub-schema the
// resolver library resolved is hoisted at its pointer-derived ID. It returns
// ok=false for a cross-document reference, a reference to an undeclared
// component, or a pointer the library could not resolve.
func (l *lowerer) resolveSchemaRef(js *oas3.JSONSchema[oas3.Referenceable], ref string) (ir.TypeID, bool) {
	pointer, ok := l.internalPointer(ref)
	if !ok {
		return "", false
	}
	if id, resolved, handled := l.resolveComponentRef(pointer); handled {
		return id, resolved
	}
	if id, ok := l.internedID(pointer); ok {
		return id, true
	}
	target := js.GetResolvedSchema()
	if target == nil {
		return "", false
	}
	return l.hoistSubSchema(target.GetSchema(), pointer)
}

// hoistSubSchema lowers a resolved internal sub-schema at pointer and guarantees
// a node exists at the pointer-derived ID, aliasing when the body reduces to a
// shared target so a $ref to the sub-schema always resolves (invariants 1, 2).
func (l *lowerer) hoistSubSchema(schema *oas3.Schema, pointer string) (ir.TypeID, bool) {
	if schema == nil {
		return "", false
	}
	hint := refLastSegment(pointer)
	ref := l.schemaBody(schema, pointer, hint)
	if owned, ok := l.byPointer[pointer]; ok {
		return owned, true
	}
	return l.internAlias(pointer, hint, ref, nil), true
}

// refNullable reports whether a $ref usage admits null: either the reference
// site carries 3.0 nullable, or its resolved target does.
func (l *lowerer) refNullable(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if s := js.GetSchema(); s != nil && s.Nullable != nil && *s.Nullable {
		return true
	}
	resolved := js.GetResolvedSchema()
	if resolved == nil {
		return false
	}
	target := resolved.GetSchema()
	return target != nil && target.Nullable != nil && *target.Nullable
}

// falseSchema hoists a boolean `false` schema as a closed empty model (it
// matches nothing) and records one info diagnostic on first visit.
func (l *lowerer) falseSchema(pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeFalseSchema,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer},
			"boolean false schema matches nothing; lowered as a closed empty model"))
		return &ir.Model{TypeCommon: l.commonFor(id, pointer, hint), Additional: ir.AdditionalClosed}
	})
}

// lower interns the inline schema at pointer and returns its TypeID. Value
// constraints (const, enum) and allOf composition take precedence over the
// structural type; otherwise it dispatches on the effective (null-stripped)
// type set.
func (l *lowerer) lower(s *oas3.Schema, pointer, hint string) ir.TypeID {
	if s.GetConst() != nil {
		return l.lowerConst(s, pointer, hint)
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
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		variants := make([]ir.Variant, 0, len(types))
		for i, st := range types {
			vptr := pointer + ptr("type", strconv.Itoa(i))
			variants = append(variants, ir.Variant{
				Name: ir.Naming{Hint: string(st)},
				Type: l.variantRef(s, st, vptr, hint),
			})
		}
		return &ir.Union{
			TypeCommon: l.commonFor(id, pointer, hint),
			Variants:   variants,
			Exclusive:  true,
		}
	})
}

// variantRef lowers one type of a multi-typed schema to a TypeRef.
func (l *lowerer) variantRef(s *oas3.Schema, st oas3.SchemaType, vptr, hint string) ir.TypeRef {
	switch st {
	case oas3.SchemaTypeObject:
		return ir.TypeRef{Target: l.lowerModel(s, vptr, hint)}
	case oas3.SchemaTypeArray:
		return ir.TypeRef{Target: l.lowerArray(s, vptr, hint)}
	default:
		return ir.TypeRef{Target: l.scalarTypeID(s, st, vptr, hint)}
	}
}

// lowerModel hoists an object schema as a Model. This task lowers only the
// property shape (name, type, required); a later pass fills the rest.
func (l *lowerer) lowerModel(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		m := &ir.Model{TypeCommon: l.commonFor(id, pointer, hint)}
		l.fillModelProperties(m, s, pointer)
		l.fillModelDetail(m, s, pointer, hint)
		if d := l.modelDiscriminator(s, m, pointer); d != nil {
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
			Type:       l.schemaRef(js, ppointer, name),
			Required:   required[name],
			Provenance: ir.Provenance{Source: l.srcIndex, Pointer: ppointer},
		}
		l.fillPropertyDetail(&p, js, ppointer)
		l.mergeProperty(m, byWire, p, ppointer)
	}
}

// wireNameIndex maps each property's wire name to its position in props, so a
// redeclaration reconciles in one lookup rather than a rescan (fillModelProperties
// runs once per allOf branch, so a linear scan would be quadratic in a wide model).
func wireNameIndex(props []ir.Property) map[string]int {
	idx := make(map[string]int, len(props))
	for i := range props {
		idx[props[i].WireName] = i
	}
	return idx
}

// mergeProperty appends p to m and records it in byWire, or folds it into the
// property that already carries the same wire name. Overlapping inline allOf
// branches — and properties co-declared alongside allOf — redeclare one logical
// field (ir-design §4.3); allOf is an intersection, so a redeclaration reconciles
// into the existing property rather than becoming a second, wire-colliding one.
//
// fillModelProperties is the sole caller and always sets WireName to the
// (non-empty) property key, so the wire name keys byWire directly.
func (l *lowerer) mergeProperty(m *ir.Model, byWire map[string]int, p ir.Property, pointer string) {
	if i, ok := byWire[p.WireName]; ok {
		l.reconcileProperty(&m.Properties[i], p, pointer)
		return
	}
	byWire[p.WireName] = len(m.Properties)
	m.Properties = append(m.Properties, p)
}

// reconcileProperty folds a redeclaration src into the already-present property
// dst under allOf intersection semantics. The field is required when any branch
// requires it and secret when any branch marks it, so those bits are OR-ed. dst
// keeps its position, identity, and type shape (the first declaration in source
// order defines them); every optional detail dst lacks is adopted from src, so a
// documented declaration and a bare one reconcile to the richer property whatever
// their branch order. A description present-and-different in both branches is a
// genuine conflict dst cannot absorb — reported, never silently dropped.
func (l *lowerer) reconcileProperty(dst *ir.Property, src ir.Property, pointer string) {
	dst.Required = dst.Required || src.Required
	dst.Secret = dst.Secret || src.Secret

	if dst.Docs.Description == "" {
		dst.Docs.Description = src.Docs.Description
	} else if src.Docs.Description != "" && src.Docs.Description != dst.Docs.Description {
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer},
			"allOf branches describe field %q differently; kept the first declaration", dst.WireName))
	}
	if dst.Default == nil {
		dst.Default = src.Default
	}
	if dst.Constraints == nil {
		dst.Constraints = src.Constraints
	}
	if dst.Deprecation == nil {
		dst.Deprecation = src.Deprecation
	}
	if dst.XML == nil {
		dst.XML = src.XML
	}
	if len(dst.Examples) == 0 {
		dst.Examples = src.Examples
	}
	dst.Extensions = mergeExtensions(dst.Extensions, src.Extensions)
}

// fillPropertyDetail enriches a property from its schema: docs, default,
// visibility, deprecation, secrecy, examples, XML, constraints, and extensions.
// Annotations present at a $ref use-site override the target's (ir-design §14).
func (l *lowerer) fillPropertyDetail(p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer string) {
	ref := js.GetSchema()
	if ref == nil {
		return
	}
	tgt := l.refTargetSchema(js, ref)
	if d := effectiveDescription(ref, tgt); d != "" {
		p.Docs.Description = d
	}
	l.fillPropertyDefault(p, ref, tgt, pointer)
	if ref.GetFormat() == "password" {
		p.Secret = true
	}
	p.Visibility = effectiveVisibility(ref, tgt)
	if effectiveDeprecated(ref, tgt) {
		p.Deprecation = &ir.Deprecation{}
	}
	if ex := l.schemaExamples(ref); len(ex) > 0 {
		p.Examples = ex
	}
	if h := xmlHints(ref.GetXML()); h != nil {
		p.XML = h
	}
	l.fillPropertyConstraints(p, ref, pointer)
	if ext := l.schemaExtensions(ref); len(ext) > 0 {
		p.Extensions = ext
	}
}

// refTargetSchema returns the resolved target schema when js is a $ref, so
// use-site annotations can fall back to the referent; it returns nil otherwise.
func (l *lowerer) refTargetSchema(js *oas3.JSONSchema[oas3.Referenceable], ref *oas3.Schema) *oas3.Schema {
	if !js.IsReference() && ref.Ref == nil {
		return nil
	}
	resolved := js.GetResolvedSchema()
	if resolved == nil {
		return nil
	}
	return resolved.GetSchema()
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
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer}, "default: %s", err.Error()))
		return
	}
	p.Default = &v
}

// fillPropertyConstraints attaches the property's scalar constraints and stamps
// each constraint diagnostic with the property's provenance.
func (l *lowerer) fillPropertyConstraints(p *ir.Property, ref *oas3.Schema, pointer string) {
	c, diags := constraintsFromSchema(ref)
	for i := range diags {
		diags[i].Provenance = ir.Provenance{Source: l.srcIndex, Pointer: pointer}
	}
	l.diags = append(l.diags, diags...)
	if c != nil {
		p.Constraints = c
	}
}

// schemaExamples lowers a schema's single (3.0) and plural (3.1) examples into
// value examples in source order.
func (l *lowerer) schemaExamples(s *oas3.Schema) []ir.Example {
	var out []ir.Example
	if node := s.GetExample(); node != nil {
		if v, err := valueFromNode(node); err == nil {
			out = append(out, ir.Example{Value: &v})
		}
	}
	for _, node := range s.GetExamples() {
		if v, err := valueFromNode(node); err == nil {
			out = append(out, ir.Example{Value: &v})
		}
	}
	return out
}

// fillModelDetail lowers the model-level shape: docs, deprecation, additional-
// property openness, validation-only keywords, and x-* extensions.
func (l *lowerer) fillModelDetail(m *ir.Model, s *oas3.Schema, pointer, hint string) {
	fillTypeDocs(&m.Docs, s)
	if effectiveDeprecated(s, nil) {
		m.Deprecation = &ir.Deprecation{}
	}
	l.fillAdditional(m, s, pointer, hint)
	l.fillValidationOnly(m, s, pointer)
	m.Extensions = mergeExtensions(m.Extensions, l.schemaExtensions(s))
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

// fillValidationOnly preserves JSON Schema keywords that have no structural IR
// home verbatim in namespaced Extensions, one info diagnostic each (§4.7).
func (l *lowerer) fillValidationOnly(m *ir.Model, s *oas3.Schema, pointer string) {
	if s.GetNot() != nil {
		l.preserveKeyword(m, "openapi:not", nodeToRaw(rawPropertyNode(s, "not")), pointer, "not")
	}
	if ite := ifThenElseRaw(s); ite != nil {
		l.preserveKeyword(m, "openapi:if-then-else", ite, pointer, "if/then/else")
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		l.preserveKeyword(m, "openapi:dependentSchemas",
			nodeToRaw(rawPropertyNode(s, "dependentSchemas")), pointer, "dependentSchemas")
	}
	if craw := containsRaw(s); craw != nil {
		l.preserveKeyword(m, "openapi:contains", craw, pointer, "contains")
	}
	if u := unevaluatedRaw(s); u != nil {
		l.preserveKeyword(m, "openapi:unevaluated", u, pointer, "unevaluated")
	}
}

// preserveKeyword records a raw keyword payload under key and emits one info
// diagnostic naming the preserved construct.
func (l *lowerer) preserveKeyword(m *ir.Model, key string, raw ir.RawValue, pointer, label string) {
	if raw == nil {
		return
	}
	if m.Extensions == nil {
		m.Extensions = ir.Extensions{}
	}
	m.Extensions[key] = raw
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeValidationOnlyKeyword,
		ir.Provenance{Source: l.srcIndex, Pointer: pointer},
		"validation-only keyword %q preserved verbatim in extensions", label))
}

// schemaExtensions lowers a schema's x-* extensions into namespaced Extensions.
func (l *lowerer) schemaExtensions(s *oas3.Schema) ir.Extensions {
	ext, diags := extensionsFrom(s.GetExtensions())
	l.diags = append(l.diags, diags...)
	return ext
}

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema with its collection constraints.
func (l *lowerer) lowerArray(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			return l.buildTuple(s, id, pointer, hint, prefix)
		}
		list := &ir.List{
			TypeCommon:  l.commonFor(id, pointer, hint),
			Elem:        l.schemaRef(s.GetItems(), pointer+ptr("items"), hint+"_item"),
			Constraints: listConstraints(s),
		}
		return list
	})
}

// buildTuple lowers prefixItems into a Tuple, preserving any trailing items
// residue raw so the closed/open distinction is not lost.
func (l *lowerer) buildTuple(s *oas3.Schema, id ir.TypeID, pointer, hint string, prefix []*oas3.JSONSchema[oas3.Referenceable]) ir.TypeDef {
	elems := make([]ir.TypeRef, 0, len(prefix))
	for i, ps := range prefix {
		elems = append(elems, l.schemaRef(ps, pointer+ptr("prefixItems", strconv.Itoa(i)), hint+"_"+strconv.Itoa(i)))
	}
	t := &ir.Tuple{TypeCommon: l.commonFor(id, pointer, hint), Elems: elems}
	if s.GetItems() != nil {
		if raw := nodeToRaw(rawPropertyNode(s, "items")); raw != nil {
			t.Extensions = ir.Extensions{"openapi:items-after-prefix": raw}
		}
	}
	return t
}

// scalarTypeID maps a scalar (type, format) pair to a TypeID via formatTable:
// a known pairing interns the shared primitive; byte and unknown formats hoist
// a named Scalar wrapping the base primitive with an Encoding.
func (l *lowerer) scalarTypeID(s *oas3.Schema, st oas3.SchemaType, pointer, hint string) ir.TypeID {
	format := s.GetFormat()
	if st == oas3.SchemaTypeString && format == "byte" {
		return l.hoistByteScalar(pointer, hint)
	}
	key := string(st)
	if format != "" {
		key += "/" + format
	}
	if prim, ok := formatTable[key]; ok {
		return l.primID(prim)
	}
	return l.hoistFormatScalar(baseForType(st), format, pointer, hint)
}

// hoistByteScalar hoists a base64-encoded byte scalar (string+byte).
func (l *lowerer) hoistByteScalar(pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		base := l.primRef(ir.PrimBytes)
		wire := l.primRef(ir.PrimString)
		return &ir.Scalar{
			TypeCommon: l.commonFor(id, pointer, hint),
			Base:       &base,
			Encoding:   &ir.Encoding{Name: "base64", WireType: &wire},
		}
	})
}

// hoistFormatScalar hoists a scalar over base carrying an unknown format as its
// encoding name, preserving the format losslessly.
func (l *lowerer) hoistFormatScalar(base ir.PrimKind, format, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		baseRef := l.primRef(base)
		return &ir.Scalar{
			TypeCommon: l.commonFor(id, pointer, hint),
			Base:       &baseRef,
			Encoding:   &ir.Encoding{Name: format},
		}
	})
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
// Extensions; a nil node or an unconvertible node yields nil.
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

// effectiveDescription picks the description from the $ref use-site when present,
// else from the referent.
func effectiveDescription(ref, tgt *oas3.Schema) string {
	if ref != nil && ref.Description != nil {
		return *ref.Description
	}
	if tgt != nil && tgt.Description != nil {
		return *tgt.Description
	}
	return ""
}

// effectiveDeprecated reports the deprecated flag, use-site over referent.
func effectiveDeprecated(ref, tgt *oas3.Schema) bool {
	return pickBool(schemaDeprecated(ref), schemaDeprecated(tgt))
}

// effectiveVisibility maps readOnly/writeOnly to a lifecycle visibility set
// (ir-design §5.2): readOnly is present in every response lifecycle
// (read/delete/query) and absent only from requests; writeOnly is create+update.
func effectiveVisibility(ref, tgt *oas3.Schema) ir.Visibility {
	switch {
	case pickBool(schemaReadOnly(ref), schemaReadOnly(tgt)):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	case pickBool(schemaWriteOnly(ref), schemaWriteOnly(tgt)):
		return ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	default:
		return ir.Visibility{}
	}
}

// pickBool returns *ref when present, else *tgt, else false.
func pickBool(ref, tgt *bool) bool {
	if ref != nil {
		return *ref
	}
	if tgt != nil {
		return *tgt
	}
	return false
}

// schemaReadOnly returns a schema's readOnly pointer, nil-safe.
func schemaReadOnly(s *oas3.Schema) *bool {
	if s == nil {
		return nil
	}
	return s.ReadOnly
}

// schemaWriteOnly returns a schema's writeOnly pointer, nil-safe.
func schemaWriteOnly(s *oas3.Schema) *bool {
	if s == nil {
		return nil
	}
	return s.WriteOnly
}

// schemaDeprecated returns a schema's deprecated pointer, nil-safe.
func schemaDeprecated(s *oas3.Schema) *bool {
	if s == nil {
		return nil
	}
	return s.Deprecated
}

// fillTypeDocs maps a schema's title, description, and externalDocs onto Docs.
func fillTypeDocs(d *ir.Docs, s *oas3.Schema) {
	if t := s.GetTitle(); t != "" {
		d.Summary = t
	}
	if desc := s.GetDescription(); desc != "" {
		d.Description = desc
	}
	if ed := s.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
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

// extensionsFrom lowers an x-* extension map into namespaced ir.Extensions, keys
// prefixed "openapi:" and values serialized to raw JSON.
func extensionsFrom(ext *extensions.Extensions) (ir.Extensions, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	out := ir.Extensions{}
	var diags []ir.Diagnostic
	for name, node := range ext.All() {
		raw := nodeToRaw(node)
		if raw == nil {
			diags = append(diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
				ir.Provenance{}, "extension %q could not be serialized", name))
			continue
		}
		out["openapi:"+name] = raw
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// mergeExtensions overlays src onto dst, allocating dst on first write.
func mergeExtensions(dst, src ir.Extensions) ir.Extensions {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = ir.Extensions{}
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
