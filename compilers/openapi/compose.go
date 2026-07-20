package openapi

import (
	"slices"
	"strconv"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/ir"
)

// lowerAllOf hoists an allOf schema as a Model, classifying each branch per
// ir-design §4.3: the sole $ref (or a $ref whose target anchors a discriminator
// hierarchy) becomes Base; other $refs become Mixins in source order; inline
// branches contribute their properties, each carrying provenance into the
// allOf branch it came from. Refs are never flattened.
func (l *lowerer) lowerAllOf(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		m := &ir.Model{TypeCommon: l.commonFor(id, pointer, hint)}
		l.fillAllOf(m, s, pointer)
		l.fillModelProperties(m, s, pointer) // properties declared alongside allOf
		l.fillModelDetail(m, s, pointer, hint)
		if d := l.modelDiscriminator(s, m); d != nil {
			m.Discriminator = d
		}
		m.DiscriminatorValue = subtypeDiscriminatorValue(s, id, pointer)
		return m
	})
}

// fillAllOf classifies and lowers the allOf branches into m.
func (l *lowerer) fillAllOf(m *ir.Model, s *oas3.Schema, pointer string) {
	branches := s.GetAllOf()
	baseIdx := l.selectAllOfBase(branches)
	for i, b := range branches {
		bptr := pointer + ptr("allOf", strconv.Itoa(i))
		if !isRefBranch(b) {
			l.fillModelProperties(m, b.GetSchema(), bptr)
			continue
		}
		id, err := refTypeID(b.GetRef().String())
		if err != nil {
			l.diags = append(l.diags, diagf(ir.SeverityError, codeUnresolvedRef,
				ir.Provenance{Source: l.srcIndex, Pointer: bptr}, "%s", err.Error()))
			continue
		}
		ref := ir.TypeRef{Target: id}
		if i == baseIdx {
			m.Base = &ref
		} else {
			m.Mixins = append(m.Mixins, ref)
		}
	}
}

// selectAllOfBase returns the branch index that becomes Model.Base, or -1 when
// none qualifies (multiple non-hierarchy refs stay Mixins). A sole $ref is the
// base; otherwise the first $ref whose target anchors a discriminator hierarchy
// is the base.
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
func subtypeDiscriminatorValue(s *oas3.Schema, id ir.TypeID, pointer string) string {
	d := baseBranchDiscriminator(s.GetAllOf())
	if d == nil {
		return ""
	}
	if m := d.GetMapping(); m != nil {
		for value, target := range m.All() {
			if tid, err := mappingTargetID(target); err == nil && tid == id {
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
	id := typeIDForPointer(pointer)
	tid := l.intern(pointer, id, func() ir.TypeDef {
		return l.buildUnion(s, id, pointer, hint)
	})
	return ir.TypeRef{Target: tid, Nullable: schemaHasNull(s) || oneOfAnyOfHasNull(s)}
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

// buildUnion assembles the Union node for a oneOf/anyOf schema, attaching a
// discriminator when one is declared.
func (l *lowerer) buildUnion(s *oas3.Schema, id ir.TypeID, pointer, hint string) ir.TypeDef {
	branches, key, exclusive := s.GetOneOf(), "oneOf", true
	if len(branches) == 0 {
		branches, key, exclusive = s.GetAnyOf(), "anyOf", false
	}
	variants := make([]ir.Variant, 0, len(branches))
	for i, b := range branches {
		if isNullSchema(b) {
			continue // null branches lift to the enclosing ref's Nullable bit
		}
		vh := variantHint(b, i)
		vptr := pointer + ptr(key, strconv.Itoa(i))
		variants = append(variants, ir.Variant{
			Name: ir.Naming{Hint: vh},
			Type: l.schemaRef(b, vptr, vh),
		})
	}
	u := &ir.Union{
		TypeCommon: l.commonFor(id, pointer, hint),
		Variants:   variants,
		Exclusive:  exclusive,
		WireTagged: false,
	}
	u.Discriminator = l.lowerDiscriminator(s, u)
	return u
}

// variantHint derives a Union variant's naming hint from its $ref target's name,
// falling back to a positional hint for inline branches.
func variantHint(b *oas3.JSONSchema[oas3.Referenceable], i int) string {
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

// lowerDiscriminator lowers the discriminator of a oneOf/anyOf union: the tag's
// wire name to Discriminator.PropertyName and each mapping entry to a target
// TypeID (a bare name implies #/components/schemas/<name>). A nil mapping stays
// nil, preserving infer-by-name semantics.
func (l *lowerer) lowerDiscriminator(s *oas3.Schema, u *ir.Union) *ir.Discriminator {
	_ = u // signature carries the union per ir-design §4.4; the tag lives on the schema
	d := s.GetDiscriminator()
	if d == nil {
		return nil
	}
	disc := &ir.Discriminator{
		PropertyName: d.GetPropertyName(),
		Mapping:      l.discriminatorMapping(d),
	}
	if dm := d.GetDefaultMapping(); dm != "" {
		if id, err := mappingTargetID(dm); err == nil {
			disc.Default = id
		}
	}
	return disc
}

// modelDiscriminator lowers a discriminator declared on a Model base (allOf
// hierarchies): the tag resolves to the property's PropID when the model
// declares it, else to its wire name, and the mapping resolves to target IDs.
func (l *lowerer) modelDiscriminator(s *oas3.Schema, m *ir.Model) *ir.Discriminator {
	d := s.GetDiscriminator()
	if d == nil {
		return nil
	}
	disc := &ir.Discriminator{Mapping: l.discriminatorMapping(d)}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	if dm := d.GetDefaultMapping(); dm != "" {
		if id, err := mappingTargetID(dm); err == nil {
			disc.Default = id
		}
	}
	return disc
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order; an unresolvable entry yields one diagnostic and
// is skipped rather than dropping the whole mapping silently.
func (l *lowerer) discriminatorMapping(d *oas3.Discriminator) map[string]ir.TypeID {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil
	}
	out := make(map[string]ir.TypeID, m.Len())
	for value, target := range m.All() {
		id, err := mappingTargetID(target)
		if err != nil {
			l.diags = append(l.diags, diagf(ir.SeverityError, codeUnresolvedRef,
				ir.Provenance{Source: l.srcIndex}, "discriminator mapping %q: %s", value, err.Error()))
			continue
		}
		out[value] = id
	}
	return out
}

// mappingTargetID resolves a discriminator mapping target — a $ref string or a
// bare schema name — to its stable TypeID.
func mappingTargetID(target string) (ir.TypeID, error) {
	if strings.HasPrefix(target, "#") || strings.Contains(target, "/") {
		return refTypeID(target)
	}
	return namedTypeID(ptr("components", "schemas", target)), nil
}

// propIDByName returns the PropID of the model property with the given source
// name, if the model declares it.
func propIDByName(m *ir.Model, name string) (ir.PropID, bool) {
	for i := range m.Properties {
		if m.Properties[i].Name.Source == name {
			return m.Properties[i].ID, true
		}
	}
	return "", false
}

// lowerConst hoists a schema with `const` as a Literal over the constant value.
func (l *lowerer) lowerConst(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		return &ir.Literal{
			TypeCommon: l.commonFor(id, pointer, hint),
			Value:      l.valueOrNull(s.GetConst(), pointer),
		}
	})
}

// lowerEnum hoists a schema with `enum` as a closed Enum. A heterogeneous or
// non-scalar member set has no Enum home, so it falls back to a Union of
// Literals with an info diagnostic — nothing is dropped.
func (l *lowerer) lowerEnum(s *oas3.Schema, pointer, hint string) ir.TypeID {
	id := typeIDForPointer(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		members, kind, ok := l.enumMembers(s.GetEnum())
		if !ok {
			return l.enumAsUnion(s, id, pointer, hint)
		}
		return &ir.Enum{
			TypeCommon: l.commonFor(id, pointer, hint),
			ValueType:  enumValueType(s, kind),
			Members:    members,
			Closed:     true,
		}
	})
}

// enumMembers converts enum nodes into scalar members, reporting ok=false when
// any member is non-scalar or the members are heterogeneous (mixed kinds).
func (l *lowerer) enumMembers(nodes []values.Value) ([]ir.EnumMember, ir.ValueKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	var kind ir.ValueKind
	for i, node := range nodes {
		val, err := valueFromNode(node)
		if err != nil || !isScalarValueKind(val.Kind) {
			return nil, "", false
		}
		if i == 0 {
			kind = val.Kind
		} else if val.Kind != kind {
			return nil, "", false
		}
		text := valueText(val)
		members = append(members, ir.EnumMember{
			Name:  ir.Naming{Source: text, Canonical: canonicalWords(text)},
			Value: val,
		})
	}
	return members, kind, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func (l *lowerer) enumAsUnion(s *oas3.Schema, id ir.TypeID, pointer, hint string) ir.TypeDef {
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
		ir.Provenance{Source: l.srcIndex, Pointer: pointer},
		"heterogeneous or non-scalar enum lowered as a union of literals"))
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
		TypeCommon: l.commonFor(id, pointer, hint),
		Variants:   variants,
		Exclusive:  true,
	}
}

// hoistLiteral hoists a single enum node as a Literal type at its own pointer.
func (l *lowerer) hoistLiteral(node values.Value, pointer, hint string) ir.TypeID {
	id := anonTypeID(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		return &ir.Literal{
			TypeCommon: l.commonFor(id, pointer, hint),
			Value:      l.valueOrNull(node, pointer),
		}
	})
}

// valueOrNull converts a node to an ir.Value, emitting a diagnostic and using
// null when the node is structurally unconvertible.
func (l *lowerer) valueOrNull(node values.Value, pointer string) ir.Value {
	val, err := valueFromNode(node)
	if err != nil {
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer}, "value: %s", err.Error()))
		return ir.Value{Kind: ir.ValueNull}
	}
	return val
}

// enumValueType picks an Enum's ValueType from the schema's declared scalar
// type, falling back to the kind inferred from its members.
func enumValueType(s *oas3.Schema, kind ir.ValueKind) ir.PrimKind {
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
	switch kind {
	case ir.ValueString:
		return ir.PrimString
	case ir.ValueBool:
		return ir.PrimBool
	case ir.ValueNumber:
		return ir.PrimNumber
	default:
		return ir.PrimString
	}
}

// isScalarValueKind reports whether a value kind is a scalar admissible as an
// enum member (composite and reference kinds are not).
func isScalarValueKind(k ir.ValueKind) bool {
	switch k {
	case ir.ValueBool, ir.ValueString, ir.ValueNumber, ir.ValueBytes, ir.ValueSymbol:
		return true
	default:
		return false
	}
}

// valueText renders a scalar value's literal string form for use as a member's
// source name.
func valueText(v ir.Value) string {
	switch v.Kind {
	case ir.ValueString, ir.ValueSymbol:
		return v.Str
	case ir.ValueNumber:
		return string(v.Num)
	case ir.ValueBool:
		return strconv.FormatBool(v.Bool)
	default:
		return ""
	}
}
