package protobuf

import (
	"math"
	"strconv"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/dexpace/morphic/ir"
)

// fillModel populates a registered Model from its message descriptor: metadata
// (extension ranges, reserved names, custom options) and then its properties.
func (l *lowerer) fillModel(m *ir.Model, md protoreflect.MessageDescriptor) {
	l.applyMessageMeta(m, md)
	m.Properties = l.lowerFields(md)
}

// applyMessageMeta attaches a message's extension ranges and its reserved and
// custom-option metadata (the latter two preserved in Extensions).
func (l *lowerer) applyMessageMeta(m *ir.Model, md protoreflect.MessageDescriptor) {
	if ranges := halfOpenRanges(md.ExtensionRanges()); len(ranges) > 0 {
		m.ExtensionRanges = ranges
	}
	ext := l.customOptions(md)
	reserved := halfOpenRanges(md.ReservedRanges())
	names := nameList(md.ReservedNames())
	if raw := reservedRaw(reserved, names); raw != nil {
		ext = mergeRaw(ext, "protobuf:reserved", raw)
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeReserved, m.Provenance,
			"reserved field numbers/names for %s preserved in Extensions", md.FullName()))
	}
	if len(ext) > 0 {
		m.Extensions = ext
	}
}

// lowerFields lowers a message's fields in source order. Each non-synthetic
// oneof collapses into one synthetic wrapper property (a WireTagged Union)
// emitted at the position of its first member; synthetic oneofs (proto3
// optional) are presence markers and lower as ordinary fields.
func (l *lowerer) lowerFields(md protoreflect.MessageDescriptor) []ir.Property {
	fields := md.Fields()
	emitted := make(map[protoreflect.Name]bool)
	props := make([]ir.Property, 0, fields.Len())
	for i := range fields.Len() {
		fd := fields.Get(i)
		if oo := fd.ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			if !emitted[oo.Name()] {
				emitted[oo.Name()] = true
				props = append(props, l.lowerOneof(oo))
			}
			continue
		}
		props = append(props, l.lowerField(fd))
	}
	return props
}

// lowerField lowers one ordinary (non-oneof) field into a Property, carrying its
// wire ID, presence discipline, type, default, and metadata.
func (l *lowerer) lowerField(fd protoreflect.FieldDescriptor) ir.Property {
	name := string(fd.Name())
	num := int(fd.Number())
	p := ir.Property{
		ID:         propID(string(fd.FullName())),
		Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
		WireID:     &num,
		Docs:       l.docsFor(fd),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: string(fd.FullName())},
	}
	p.WireName = explicitWireName(fd)
	l.applyFieldType(&p, fd)
	if fd.HasDefault() {
		p.Default = l.lowerDefault(fd)
	}
	if dep := deprecationOf(fd); dep != nil {
		p.Deprecation = dep
	}
	if ext := l.customOptions(fd); len(ext) > 0 {
		p.Extensions = ext
	}
	return p
}

// applyFieldType sets a property's Type, Encoding, Required, and Presence from
// the field's cardinality and kind. Repeated and map fields hoist container
// nodes; singular fields resolve their presence discipline (ir-design §5.1).
func (l *lowerer) applyFieldType(p *ir.Property, fd protoreflect.FieldDescriptor) {
	switch {
	case fd.IsMap():
		p.Type = l.mapRef(fd)
	case fd.IsList():
		p.Type = l.listRef(fd)
	default:
		ref, enc := l.leafType(fd)
		p.Type = ref
		p.Encoding = enc
		if fd.Cardinality() == protoreflect.Required {
			p.Required = true
			p.Presence = ir.PresenceRequired
			return
		}
		if fd.HasPresence() {
			p.Presence = ir.PresenceExplicit
			return
		}
		p.Presence = ir.PresenceImplicit
	}
}

// lowerOneof builds the synthetic wrapper property for a real oneof: a Flatten
// property whose type is a hoisted WireTagged, Exclusive Union of the members.
func (l *lowerer) lowerOneof(oo protoreflect.OneofDescriptor) ir.Property {
	name := string(oo.Name())
	return ir.Property{
		ID:         propID(string(oo.FullName())),
		Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
		Type:       l.unionRef(oo),
		Flatten:    true,                // oneof members are top-level wire fields
		Presence:   ir.PresenceExplicit, // which member (if any) is set is observable
		Docs:       l.docsFor(oo),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: string(oo.FullName())},
	}
}

// unionRef interns the Union for a oneof and returns a reference to it. The Union
// is registered before its variants are lowered so a variant referencing the
// enclosing message resolves to an interned ID.
func (l *lowerer) unionRef(oo protoreflect.OneofDescriptor) ir.TypeRef {
	scope := string(oo.FullName())
	id := anonTypeID(scope + "/oneof")
	if _, ok := l.out.Types[id]; !ok {
		u := &ir.Union{
			TypeCommon: l.anonCommon(id, scope, canonicalWords(string(oo.Name()))),
			Exclusive:  true,
			WireTagged: true,
		}
		l.out.Types[id] = u
		u.Variants = l.oneofVariants(oo)
	}
	return ir.TypeRef{Target: id}
}

// oneofVariants lowers a oneof's member fields into union variants, preserving
// each member's field number as the variant wire ID.
func (l *lowerer) oneofVariants(oo protoreflect.OneofDescriptor) []ir.Variant {
	fields := oo.Fields()
	vars := make([]ir.Variant, 0, fields.Len())
	for i := range fields.Len() {
		fd := fields.Get(i)
		name := string(fd.Name())
		num := int(fd.Number())
		v := ir.Variant{
			Name:   ir.Naming{Source: name, Canonical: canonicalWords(name)},
			Type:   l.elementRef(fd),
			WireID: &num,
			Docs:   l.docsFor(fd),
		}
		v.WireName = explicitWireName(fd)
		if dep := deprecationOf(fd); dep != nil {
			v.Deprecation = dep
		}
		vars = append(vars, v)
	}
	return vars
}

// listRef interns the List node for a repeated field. The container encoding
// (packed vs expanded) is recorded for packable element kinds and stacks with
// any element-level encoding carried by the element type.
func (l *lowerer) listRef(fd protoreflect.FieldDescriptor) ir.TypeRef {
	id := anonTypeID(string(fd.FullName()) + "/list")
	if _, ok := l.out.Types[id]; !ok {
		list := &ir.List{
			TypeCommon: l.anonCommon(id, string(fd.FullName()), canonicalWords(string(fd.Name()))),
			Elem:       l.elementRef(fd),
			Encoding:   listEncoding(fd),
		}
		l.out.Types[id] = list
	}
	return ir.TypeRef{Target: id}
}

// mapRef interns the MapT node for a map field, resolving key and value element
// types (each wrapped in a Scalar when it carries a wire encoding).
func (l *lowerer) mapRef(fd protoreflect.FieldDescriptor) ir.TypeRef {
	id := anonTypeID(string(fd.FullName()) + "/map")
	if _, ok := l.out.Types[id]; !ok {
		l.out.Types[id] = &ir.MapT{
			TypeCommon: l.anonCommon(id, string(fd.FullName()), canonicalWords(string(fd.Name()))),
			Key:        l.elementRef(fd.MapKey()),
			Value:      l.elementRef(fd.MapValue()),
		}
	}
	return ir.TypeRef{Target: id}
}

// elementRef resolves the element/key/value type of a nested position (list
// element, map entry, or oneof variant). A packable scalar carrying a wire
// encoding (sint/fixed) is wrapped in a hoisted Scalar so the encoding survives
// where a bare TypeRef has no encoding slot; every other leaf resolves directly.
func (l *lowerer) elementRef(fd protoreflect.FieldDescriptor) ir.TypeRef {
	if prim, enc, ok := scalarPrim(fd.Kind()); ok && enc != "" {
		return l.scalarWrap(fd, l.primRef(prim), &ir.Encoding{Name: enc})
	}
	ref, _ := l.leafType(fd)
	return ref
}

// scalarWrap interns a Scalar node that restricts base with a wire encoding,
// used for encoded scalars in nested positions.
func (l *lowerer) scalarWrap(fd protoreflect.FieldDescriptor, base ir.TypeRef, enc *ir.Encoding) ir.TypeRef {
	id := anonTypeID(string(fd.FullName()) + "/elem")
	if _, ok := l.out.Types[id]; !ok {
		b := base
		l.out.Types[id] = &ir.Scalar{
			TypeCommon: l.anonCommon(id, string(fd.FullName()), canonicalWords(string(fd.Name()))),
			Base:       &b,
			Encoding:   enc,
		}
	}
	return ir.TypeRef{Target: id}
}

// leafType resolves a field's scalar/message/enum leaf into a TypeRef plus any
// property-level wire encoding (zigzag/fixed for scalars, delimited for groups).
func (l *lowerer) leafType(fd protoreflect.FieldDescriptor) (ir.TypeRef, *ir.Encoding) {
	if prim, enc, ok := scalarPrim(fd.Kind()); ok {
		ref := l.primRef(prim)
		if enc != "" {
			return ref, &ir.Encoding{Name: enc}
		}
		return ref, nil
	}
	switch fd.Kind() {
	case protoreflect.EnumKind:
		return l.enumRef(fd.Enum()), nil
	case protoreflect.GroupKind:
		return l.messageOrWKT(fd.Message()), &ir.Encoding{Name: "delimited"}
	case protoreflect.MessageKind:
		return l.messageOrWKT(fd.Message()), nil
	default:
		return l.anyRef(), nil
	}
}

// scalarPrim maps a protobuf scalar kind to its IR primitive and the name of its
// wire encoding when the kind is an encoded variant of that primitive (sint* →
// zigzag, fixed*/sfixed* → fixed). It reports false for non-scalar kinds.
func scalarPrim(k protoreflect.Kind) (ir.PrimKind, string, bool) {
	switch k {
	case protoreflect.BoolKind:
		return ir.PrimBool, "", true
	case protoreflect.StringKind:
		return ir.PrimString, "", true
	case protoreflect.BytesKind:
		return ir.PrimBytes, "", true
	case protoreflect.Int32Kind:
		return ir.PrimInt32, "", true
	case protoreflect.Sint32Kind:
		return ir.PrimInt32, "zigzag", true
	case protoreflect.Sfixed32Kind:
		return ir.PrimInt32, "fixed", true
	case protoreflect.Uint32Kind:
		return ir.PrimUint32, "", true
	case protoreflect.Fixed32Kind:
		return ir.PrimUint32, "fixed", true
	case protoreflect.Int64Kind:
		return ir.PrimInt64, "", true
	case protoreflect.Sint64Kind:
		return ir.PrimInt64, "zigzag", true
	case protoreflect.Sfixed64Kind:
		return ir.PrimInt64, "fixed", true
	case protoreflect.Uint64Kind:
		return ir.PrimUint64, "", true
	case protoreflect.Fixed64Kind:
		return ir.PrimUint64, "fixed", true
	case protoreflect.FloatKind:
		return ir.PrimFloat32, "", true
	case protoreflect.DoubleKind:
		return ir.PrimFloat64, "", true
	default:
		return "", "", false
	}
}

// listEncoding reports the container-level encoding of a repeated field: packed
// or expanded for packable element kinds, nil for strings, bytes, and messages
// (which are never packed).
func listEncoding(fd protoreflect.FieldDescriptor) *ir.Encoding {
	if !packable(fd.Kind()) {
		return nil
	}
	if fd.IsPacked() {
		return &ir.Encoding{Name: "packed"}
	}
	return &ir.Encoding{Name: "expanded"}
}

// packable reports whether a repeated element kind is subject to packed
// encoding (numeric, bool, and enum kinds are; length-delimited kinds are not).
func packable(k protoreflect.Kind) bool {
	switch k {
	case protoreflect.StringKind, protoreflect.BytesKind,
		protoreflect.MessageKind, protoreflect.GroupKind:
		return false
	default:
		return true
	}
}

// fillEnum populates a registered Enum from its descriptor. Proto enums are
// int32-valued; openness follows the resolved closed/open semantics, and
// duplicate member values (allow_alias) survive as distinct members.
func (l *lowerer) fillEnum(e *ir.Enum, ed protoreflect.EnumDescriptor) {
	e.ValueType = ir.PrimInt32
	e.Closed = ed.IsClosed()
	vals := ed.Values()
	e.Members = make([]ir.EnumMember, 0, vals.Len())
	for i := range vals.Len() {
		v := vals.Get(i)
		name := string(v.Name())
		mem := ir.EnumMember{
			Name:  ir.Naming{Source: name, Canonical: canonicalWords(name)},
			Value: ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(strconv.FormatInt(int64(v.Number()), 10))},
			Docs:  l.docsFor(v),
		}
		if dep := deprecationOf(v); dep != nil {
			mem.Deprecation = dep
		}
		if ext := l.customOptions(v); len(ext) > 0 {
			mem.Extensions = ext
		}
		e.Members = append(e.Members, mem)
	}
	l.applyEnumMeta(e, ed)
}

// applyEnumMeta attaches an enum's reserved and custom-option metadata,
// preserved in Extensions (enum reserved ranges are inclusive).
func (l *lowerer) applyEnumMeta(e *ir.Enum, ed protoreflect.EnumDescriptor) {
	ext := l.customOptions(ed)
	if raw := reservedRaw(inclusiveEnumRanges(ed.ReservedRanges()), nameList(ed.ReservedNames())); raw != nil {
		ext = mergeRaw(ext, "protobuf:reserved", raw)
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeReserved, e.Provenance,
			"reserved enum numbers/names for %s preserved in Extensions", ed.FullName()))
	}
	if len(ext) > 0 {
		e.Extensions = ext
	}
}

// lowerDefault lowers a proto2 field default into the Values channel. Numeric
// defaults are carried as BigVal decimal strings, never float64 (ir-design §6).
func (l *lowerer) lowerDefault(fd protoreflect.FieldDescriptor) *ir.Value {
	d := fd.Default()
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return &ir.Value{Kind: ir.ValueBool, Bool: d.Bool()}
	case protoreflect.StringKind:
		return &ir.Value{Kind: ir.ValueString, Str: d.String()}
	case protoreflect.BytesKind:
		return &ir.Value{Kind: ir.ValueBytes, Bytes: d.Bytes()}
	case protoreflect.EnumKind:
		ev := fd.DefaultEnumValue()
		return &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{
			Type: namedTypeID(string(fd.Enum().FullName())), Member: string(ev.Name())}}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return l.floatDefault(fd, d.Float())
	default:
		return l.numericDefault(fd, d)
	}
}

// floatDefault lowers a floating default, degrading non-finite defaults
// (inf/nan, which proto permits) to a diagnostic rather than an invalid BigVal.
func (l *lowerer) floatDefault(fd protoreflect.FieldDescriptor, f float64) *ir.Value {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: string(fd.FullName())},
			"non-finite default for %s dropped (not representable as a decimal)", fd.FullName()))
		return nil
	}
	return &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(strconv.FormatFloat(f, 'g', -1, 64))}
}

// numericDefault lowers an integral field default (signed or unsigned).
func (l *lowerer) numericDefault(fd protoreflect.FieldDescriptor, d protoreflect.Value) *ir.Value {
	switch fd.Kind() {
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(strconv.FormatUint(d.Uint(), 10))}
	default:
		return &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(strconv.FormatInt(d.Int(), 10))}
	}
}
