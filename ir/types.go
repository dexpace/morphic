package ir

// PrimKind names a built-in primitive scalar (ir-design §4.1). The set is the
// union of TypeSpec's intrinsic scalars, JSON Schema's types, and Protobuf's
// needs.
type PrimKind string

// Primitive kinds.
const (
	// PrimBool is a boolean.
	PrimBool PrimKind = "bool"
	// PrimString is a Unicode string.
	PrimString PrimKind = "string"
	// PrimBytes is a byte string.
	PrimBytes PrimKind = "bytes"
	// PrimInt8 is a signed 8-bit integer.
	PrimInt8 PrimKind = "int8"
	// PrimInt16 is a signed 16-bit integer.
	PrimInt16 PrimKind = "int16"
	// PrimInt32 is a signed 32-bit integer.
	PrimInt32 PrimKind = "int32"
	// PrimInt64 is a signed 64-bit integer.
	PrimInt64 PrimKind = "int64"
	// PrimUint8 is an unsigned 8-bit integer.
	PrimUint8 PrimKind = "uint8"
	// PrimUint16 is an unsigned 16-bit integer.
	PrimUint16 PrimKind = "uint16"
	// PrimUint32 is an unsigned 32-bit integer.
	PrimUint32 PrimKind = "uint32"
	// PrimUint64 is an unsigned 64-bit integer.
	PrimUint64 PrimKind = "uint64"
	// PrimInteger is an arbitrary-precision integer (JSON Schema/TypeSpec integer).
	PrimInteger PrimKind = "integer"
	// PrimFloat32 is a 32-bit IEEE-754 float.
	PrimFloat32 PrimKind = "float32"
	// PrimFloat64 is a 64-bit IEEE-754 float.
	PrimFloat64 PrimKind = "float64"
	// PrimFloat is an arbitrary-precision binary float (supertype of float32/64).
	PrimFloat PrimKind = "float"
	// PrimNumber is an arbitrary-precision number (JSON Schema number, TypeSpec numeric).
	PrimNumber PrimKind = "number"
	// PrimDecimal is an arbitrary-precision decimal.
	PrimDecimal PrimKind = "decimal"
	// PrimDecimal128 is a 128-bit IEEE-754 decimal.
	PrimDecimal128 PrimKind = "decimal128"
	// PrimDate is a calendar date without time.
	PrimDate PrimKind = "date"
	// PrimTime is a time of day without date.
	PrimTime PrimKind = "time"
	// PrimDatetime is a date and time without offset.
	PrimDatetime PrimKind = "datetime"
	// PrimDatetimeOffset is a date and time with UTC offset.
	PrimDatetimeOffset PrimKind = "datetime_offset"
	// PrimDuration is a time span.
	PrimDuration PrimKind = "duration"
	// PrimURL is a URL.
	PrimURL PrimKind = "url"
	// PrimUUID is a UUID.
	PrimUUID PrimKind = "uuid"
	// PrimAny is an unknown/JSON any (schemaless) primitive.
	PrimAny PrimKind = "any"
)

// AdditionalMode describes the openness of a model's property set beyond its
// declared properties and AdditionalProps (ir-design §4.3).
type AdditionalMode string

// Additional-property modes.
const (
	// AdditionalUnspecified leaves openness unspecified (open by JSON Schema default).
	AdditionalUnspecified AdditionalMode = ""
	// AdditionalClosed forbids properties beyond the declared set
	// (additionalProperties: false, closed-by-construction records).
	AdditionalClosed AdditionalMode = "closed"
	// AdditionalClosedAfterComposition closes the set once composition is resolved
	// (unevaluatedProperties: false).
	AdditionalClosedAfterComposition AdditionalMode = "closed_after_composition"
)

// Primitive is a built-in scalar leaf type (ir-design §4.1).
type Primitive struct {
	TypeCommon
	// Prim selects the primitive kind.
	Prim PrimKind `json:"prim"`
}

// Scalar is a named restricted or extended primitive (ir-design §4.2). Emitters
// resolve the Base chain to the nearest representable base, accumulating
// constraints and encoding along the way.
type Scalar struct {
	TypeCommon
	// Base is the primitive or scalar this scalar extends; nil = opaque scalar
	// with implementation-defined representation (GraphQL custom scalars).
	Base *TypeRef `json:"base,omitempty"`
	// Constraints restricts the scalar's admissible values.
	Constraints *Constraints `json:"constraints,omitempty"`
	// Encoding overrides the scalar's wire encoding.
	Encoding *Encoding `json:"encoding,omitempty"`
}

// Model is a struct, object, or message shape (ir-design §4.3). Properties holds
// only the model's own properties; consumers walk Base, Implements, and Mixins
// for the full shape.
type Model struct {
	TypeCommon
	// Properties are the model's own properties, in source order.
	Properties []Property `json:"properties,omitempty"`
	// Base is the declared single inheritance parent (TypeSpec extends,
	// allOf-as-inheritance).
	Base *TypeRef `json:"base,omitempty"`
	// Implements is N-ary interface conformance (GraphQL implements A & B);
	// targets are Abstract models.
	Implements []TypeRef `json:"implements,omitempty"`
	// Mixins is composition without subtyping (Smithy mixins, TypeSpec spread,
	// extra allOf entries).
	Mixins []TypeRef `json:"mixins,omitempty"`
	// AdditionalProps is a map-like catch-all alongside declared properties.
	AdditionalProps *AdditionalProps `json:"additionalProps,omitempty"`
	// Additional is the openness of the property set.
	Additional AdditionalMode `json:"additional,omitempty"`
	// Abstract marks a model that cannot be instantiated directly (GraphQL
	// interface types).
	Abstract bool `json:"abstract"`
	// Positional serializes properties positionally as a tuple ordered by WireID
	// (Erlang records).
	Positional bool `json:"positional"`
	// ExtensionRanges are wire-ID ranges reserved for third-party extension fields
	// (protobuf extensions 100 to 199).
	ExtensionRanges []WireIDRange `json:"extensionRanges,omitempty"`
	// Discriminator is set on the polymorphic base.
	Discriminator *Discriminator `json:"discriminator,omitempty"`
	// DiscriminatorValue is set on each subtype: its wire tag value.
	DiscriminatorValue string `json:"discriminatorValue,omitempty"`
	// InputOnly marks GraphQL input types, which have distinct identity from
	// output types.
	InputOnly bool `json:"inputOnly"`
}

// WireIDRange is an inclusive range of wire IDs (ir-design §4.3).
type WireIDRange struct {
	// From is the first wire ID in the range.
	From int `json:"from"`
	// To is the last wire ID in the range.
	To int `json:"to"`
}

// AdditionalProps describes a model's map-like catch-all for undeclared
// properties (ir-design §4.3).
type AdditionalProps struct {
	// Value is the value schema for catch-all properties.
	Value TypeRef `json:"value"`
	// Key is the key schema; nil = string keys.
	Key *TypeRef `json:"key,omitempty"`
	// Patterns are key-pattern-scoped value schemas (JSON Schema patternProperties).
	Patterns []PatternProps `json:"patterns,omitempty"`
}

// PatternProps binds a key pattern to a value schema (JSON Schema
// patternProperties).
type PatternProps struct {
	// Pattern is the ECMA-262 key pattern.
	Pattern string `json:"pattern"`
	// Value is the value schema for matching keys.
	Value TypeRef `json:"value"`
}

// Discriminator locates and maps the tag that selects a variant in a
// polymorphic model hierarchy or union (ir-design §4.3). Exactly one of
// Property, PropertyName, or Index locates the tag.
type Discriminator struct {
	// Property is the property carrying the tag in model hierarchies.
	Property PropID `json:"property,omitempty"`
	// PropertyName is the wire name of the tag property in unions, where the
	// property exists on no single model (TypeSpec @discriminated
	// discriminatorPropertyName, OpenAPI discriminator on oneOf).
	PropertyName string `json:"propertyName,omitempty"`
	// Index is the 0-based tuple element carrying the tag Literal (Erlang tagged
	// tuples, JSON arrays with a const head via prefixItems).
	Index *int `json:"index,omitempty"`
	// Mapping maps wire value to subtype; nil = infer by type name.
	Mapping map[string]TypeID `json:"mapping,omitempty"`
	// Default is the variant to use when the tag is absent/unrecognized (OpenAPI
	// 3.2 defaultMapping); zero = none.
	Default TypeID `json:"default,omitempty"`
	// Envelope is "" for an inline tag or "object" for a {kind, value} wrapper
	// (TypeSpec @discriminated envelope).
	Envelope string `json:"envelope,omitempty"`
	// EnvelopeValueName is the wire name of the envelope's value property (default
	// "value"); meaningful only when Envelope == "object".
	EnvelopeValueName string `json:"envelopeValueName,omitempty"`
	// Inferred marks a discriminator discovered heuristically, not declared.
	Inferred bool `json:"inferred"`
}

// Union is a sum over variant types (ir-design §4.4). One node covers untagged
// anyOf/oneOf, discriminated oneOf, and natively tagged unions.
type Union struct {
	TypeCommon
	// Variants are the union's members, in source order.
	Variants []Variant `json:"variants,omitempty"`
	// Exclusive is true for oneOf/tagged (exactly one variant matches) and false
	// for anyOf (one-or-more).
	Exclusive bool `json:"exclusive"`
	// WireTagged reports that the wire format itself encodes the variant (protobuf
	// oneof, Smithy union, GraphQL __typename) rather than untagged JSON oneOf.
	WireTagged bool `json:"wireTagged"`
	// Discriminator is the internal tag property, when one exists.
	Discriminator *Discriminator `json:"discriminator,omitempty"`
}

// Variant is one member of a Union (ir-design §4.4).
type Variant struct {
	// Name is the variant's naming; Hint-only for bare oneOf members.
	Name Naming `json:"name"`
	// Type is the variant's type.
	Type TypeRef `json:"type"`
	// WireName is the serialized tag when it differs from Name.Source (Smithy
	// @jsonName on union members, protobuf oneof json_name).
	WireName string `json:"wireName,omitempty"`
	// WireID is the protobuf oneof field number or Cap'n Proto/Avro ordinal; nil =
	// none (pointer because 0 is a legal ordinal).
	WireID *int `json:"wireID,omitempty"`
	// XML is @xmlName/@xmlNamespace on union members.
	XML *XMLHints `json:"xml,omitempty"`
	// Event is event-stream metadata when the union is a stream's event set.
	Event *EventInfo `json:"event,omitempty"`
	// Docs is the variant's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the variant as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the variant's versioning timeline.
	Availability *Availability `json:"availability,omitempty"`
	// Examples are typed example values for the variant.
	Examples []Example `json:"examples,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// EventInfo carries per-event metadata when a union is an event stream's event
// set (ir-design §4.4).
type EventInfo struct {
	// ContentType is the per-event content type (TypeSpec @Events.contentType).
	ContentType string `json:"contentType,omitempty"`
	// Terminal reports that receiving this event ends the stream (TypeSpec
	// @SSE.terminalEvent).
	Terminal bool `json:"terminal"`
}

// Enum is a closed or open set of named values (ir-design §4.5).
type Enum struct {
	TypeCommon
	// ValueType is the primitive type of the members' values.
	ValueType PrimKind `json:"valueType"`
	// Members are the enum's members, in source order.
	Members []EnumMember `json:"members,omitempty"`
	// Closed is false for open/extensible enums: unknown values must be
	// representable.
	Closed bool `json:"closed"`
	// Flags marks bitfield semantics.
	Flags bool `json:"flags"`
	// FallbackMember is the wire name of the member to substitute for an unknown
	// value (Avro enum default symbol); "" = none.
	FallbackMember string `json:"fallbackMember,omitempty"`
}

// EnumMember is one member of an Enum (ir-design §4.5).
type EnumMember struct {
	// Name is the member's naming.
	Name Naming `json:"name"`
	// Value is the member's typed value, matching Enum.ValueType.
	Value Value `json:"value"`
	// WireName is the serialized form when it differs from Value (rare).
	WireName string `json:"wireName,omitempty"`
	// Docs is the member's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the member as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the member's versioning timeline (TypeSpec @added on
	// EnumMember).
	Availability *Availability `json:"availability,omitempty"`
	// Examples are typed example values for the member.
	Examples []Example `json:"examples,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// List is an ordered collection (ir-design §4.6).
type List struct {
	TypeCommon
	// Elem is the element type.
	Elem TypeRef `json:"elem"`
	// Constraints restricts minItems/maxItems/uniqueItems.
	Constraints *Constraints `json:"constraints,omitempty"`
	// Encoding is the container-level wire encoding (protobuf packed vs expanded
	// repeated fields); it stacks with the element's own encoding.
	Encoding *Encoding `json:"encoding,omitempty"`
}

// MapT is a keyed collection (Record/additionalProperties-only/proto map)
// (ir-design §4.6).
type MapT struct {
	TypeCommon
	// Key is the key type.
	Key TypeRef `json:"key"`
	// Value is the value type.
	Value TypeRef `json:"value"`
}

// Tuple is a positional, fixed-arity sequence (prefixItems, TypeSpec tuples,
// Erlang tuples) (ir-design §4.6).
type Tuple struct {
	TypeCommon
	// Elems are the element types in position order.
	Elems []TypeRef `json:"elems,omitempty"`
}

// Literal is a single constant value used as a type (const, single-value enum,
// discriminator pin) (ir-design §4.6).
type Literal struct {
	TypeCommon
	// Value is the constant value.
	Value Value `json:"value"`
}

// External is a well-known library type that no target can structurally model,
// resolved to a runtime handle by the emitter (TCGC external) (ir-design §4.6).
type External struct {
	TypeCommon
	// Identity is the external type's stable identity (e.g. "erlang:pid").
	Identity string `json:"identity"`
	// Package is the providing package.
	Package string `json:"package,omitempty"`
	// MinVersion is the minimum package version.
	MinVersion string `json:"minVersion,omitempty"`
}

// Any is a schemaless type (ir-design §4.6).
type Any struct {
	TypeCommon
}

func (*Primitive) typeDef() {}

// Kind reports the TypeKind of a Primitive.
func (*Primitive) Kind() TypeKind { return KindPrimitive }

// Common returns the shared TypeCommon of a Primitive.
func (p *Primitive) Common() *TypeCommon { return &p.TypeCommon }

func (*Scalar) typeDef() {}

// Kind reports the TypeKind of a Scalar.
func (*Scalar) Kind() TypeKind { return KindScalar }

// Common returns the shared TypeCommon of a Scalar.
func (s *Scalar) Common() *TypeCommon { return &s.TypeCommon }

func (*Model) typeDef() {}

// Kind reports the TypeKind of a Model.
func (*Model) Kind() TypeKind { return KindModel }

// Common returns the shared TypeCommon of a Model.
func (m *Model) Common() *TypeCommon { return &m.TypeCommon }

func (*Union) typeDef() {}

// Kind reports the TypeKind of a Union.
func (*Union) Kind() TypeKind { return KindUnion }

// Common returns the shared TypeCommon of a Union.
func (u *Union) Common() *TypeCommon { return &u.TypeCommon }

func (*Enum) typeDef() {}

// Kind reports the TypeKind of an Enum.
func (*Enum) Kind() TypeKind { return KindEnum }

// Common returns the shared TypeCommon of an Enum.
func (e *Enum) Common() *TypeCommon { return &e.TypeCommon }

func (*List) typeDef() {}

// Kind reports the TypeKind of a List.
func (*List) Kind() TypeKind { return KindList }

// Common returns the shared TypeCommon of a List.
func (l *List) Common() *TypeCommon { return &l.TypeCommon }

func (*MapT) typeDef() {}

// Kind reports the TypeKind of a MapT.
func (*MapT) Kind() TypeKind { return KindMap }

// Common returns the shared TypeCommon of a MapT.
func (m *MapT) Common() *TypeCommon { return &m.TypeCommon }

func (*Tuple) typeDef() {}

// Kind reports the TypeKind of a Tuple.
func (*Tuple) Kind() TypeKind { return KindTuple }

// Common returns the shared TypeCommon of a Tuple.
func (t *Tuple) Common() *TypeCommon { return &t.TypeCommon }

func (*Literal) typeDef() {}

// Kind reports the TypeKind of a Literal.
func (*Literal) Kind() TypeKind { return KindLiteral }

// Common returns the shared TypeCommon of a Literal.
func (l *Literal) Common() *TypeCommon { return &l.TypeCommon }

func (*External) typeDef() {}

// Kind reports the TypeKind of an External.
func (*External) Kind() TypeKind { return KindExternal }

// Common returns the shared TypeCommon of an External.
func (e *External) Common() *TypeCommon { return &e.TypeCommon }

func (*Any) typeDef() {}

// Kind reports the TypeKind of an Any.
func (*Any) Kind() TypeKind { return KindAny }

// Common returns the shared TypeCommon of an Any.
func (a *Any) Common() *TypeCommon { return &a.TypeCommon }
