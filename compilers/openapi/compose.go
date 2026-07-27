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
// allOf branch it came from.
func (l *lowerer) lowerAllOf(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		m := &ir.Model{TypeCommon: common}
		l.fillAllOf(m, s, pointer)
		l.fillModelProperties(m, s, pointer) // properties declared alongside allOf
		l.applyCompositionRequired(m, s, pointer)
		l.fillModelDetail(m, s, pointer, hint)
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
// allOf composition, in source order: each branch's own required list (via
// its local schema — never its resolved $ref target, which belongs to that
// target's own model), then the composed schema's own required list. allOf is
// an intersection, so a required list constrains the whole composed object
// regardless of which branch declares the named property — unlike
// fillModelProperties, which only ever applies a required list to the
// properties declared in the very same properties map (issue #29).
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
	sev := ir.SeverityInfo
	if m.Base != nil || len(m.Mixins) > 0 {
		sev = ir.SeverityWarning
	}
	l.diag(sev, codeUnattachableRequired, e.pointer,
		"required property %q matches no own property; cannot attach across allOf composition", e.name)
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
		id, ok := l.resolveSchemaRef(b, b.GetRef().String())
		if !ok {
			l.diag(ir.SeverityError, codeUnresolvedRef, bptr,
				"unresolved allOf $ref %q", b.GetRef().String())
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
		return l.buildUnion(s, common, pointer)
	})
	return ir.TypeRef{Target: tid, Nullable: schemaAdmitsNull(s)}
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
// discriminator when one is declared. common is already built by the caller
// (internNode), so buildUnion needs no hint of its own to build one.
func (l *lowerer) buildUnion(s *oas3.Schema, common ir.TypeCommon, pointer string) ir.TypeDef {
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
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  exclusive,
		WireTagged: false,
	}
	u.Discriminator = l.lowerDiscriminator(s, nil, pointer)
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
		members, kind, ok := l.enumMembers(s.GetEnum())
		if !ok {
			return l.enumAsUnion(s, common, pointer, hint)
		}
		return &ir.Enum{
			TypeCommon: common,
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

// hoistLiteral hoists a single node as a Literal type at its own pointer. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's typeIDForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
func (l *lowerer) hoistLiteral(node values.Value, pointer, hint string) ir.TypeID {
	return l.internNode(pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		return &ir.Literal{
			TypeCommon: common,
			Value:      l.valueOrNull(node, pointer),
		}
	})
}

// valueOrNull converts a node to an ir.Value, emitting a diagnostic and using
// null when the node is structurally unconvertible.
func (l *lowerer) valueOrNull(node values.Value, pointer string) ir.Value {
	val, err := valueFromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer, "value: %s", err.Error())
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
