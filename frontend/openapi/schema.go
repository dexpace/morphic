package openapi

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// lowerComponentSchemas interns every named component schema in source order.
// It is the entry Parse's run() calls before any operation lowering so that
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
	for name, js := range schemas.All() {
		l.schemaRef(js, ptr("components", "schemas", name), name)
	}
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
	schema := js.GetSchema()
	if schema == nil {
		return l.primRef(ir.PrimAny)
	}
	if schema.Ref != nil {
		return l.refTypeRef(js, pointer)
	}
	if len(schema.GetOneOf()) > 0 || len(schema.GetAnyOf()) > 0 {
		return l.lowerOneOfAnyOf(schema, pointer, hint)
	}
	return ir.TypeRef{Target: l.lower(schema, pointer, hint), Nullable: schemaHasNull(schema)}
}

// refTypeRef resolves a $ref position to its target's stable ID, carrying the
// combined ref-site and target nullability. The target is lowered where it is
// defined, never here (single-hoisting-pass rule).
func (l *lowerer) refTypeRef(js *oas3.JSONSchema[oas3.Referenceable], pointer string) ir.TypeRef {
	id, err := refTypeID(js.GetRef().String())
	if err != nil {
		l.diags = append(l.diags, diagf(ir.SeverityError, codeUnresolvedRef,
			ir.Provenance{Source: l.srcIndex, Pointer: pointer}, "%s", err.Error()))
		return l.primRef(ir.PrimAny)
	}
	return ir.TypeRef{Target: id, Nullable: l.refNullable(js)}
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
		if d := l.modelDiscriminator(s, m); d != nil {
			m.Discriminator = d
		}
		return m
	})
}

// fillModelProperties lowers a model's own properties in source order.
func (l *lowerer) fillModelProperties(m *ir.Model, s *oas3.Schema, pointer string) {
	props := s.GetProperties()
	if props == nil {
		return
	}
	required := requiredSet(s.GetRequired())
	for name, js := range props.All() {
		ppointer := pointer + ptr("properties", name)
		ref := l.schemaRef(js, ppointer, name)
		m.Properties = append(m.Properties, ir.Property{
			ID:         propID(ppointer),
			Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
			Type:       ref,
			Required:   required[name],
			Provenance: ir.Provenance{Source: l.srcIndex, Pointer: ppointer},
		})
	}
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
		if raw := nodeToRaw(s.GetPropertyNode("items")); raw != nil {
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
	for _, t := range s.GetType() {
		if t == oas3.SchemaTypeNull {
			return true
		}
	}
	return false
}

// nullUnionCollapse detects a oneOf/anyOf whose variant set is {null-schema, X}
// and returns X's schema, pointer, and hint so it lowers as nullable X rather
// than a union node (ir-design §3.3).
func nullUnionCollapse(s *oas3.Schema, pointer, hint string) (*oas3.JSONSchema[oas3.Referenceable], string, string, bool) {
	variants, key := s.GetOneOf(), "oneOf"
	if len(variants) == 0 {
		variants, key = s.GetAnyOf(), "anyOf"
	}
	if len(variants) != 2 {
		return nil, "", "", false
	}
	var nonNull *oas3.JSONSchema[oas3.Referenceable]
	nonNullIdx, nullCount := -1, 0
	for i, v := range variants {
		if isNullSchema(v) {
			nullCount++
			continue
		}
		nonNull, nonNullIdx = v, i
	}
	if nullCount != 1 || nonNull == nil {
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

// canonicalWords renders name as a neutral lower_snake word sequence: it splits
// on _/-/space and on camel-case and letter/digit boundaries, lowercases, and
// joins with "_". It holds no acronym opinion beyond boundary detection; casing
// policy is a backend concern.
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
