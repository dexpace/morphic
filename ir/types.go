// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

var (
	_ TypeDef = (*Primitive)(nil)
	_ TypeDef = (*Scalar)(nil)
	_ TypeDef = (*Model)(nil)
	_ TypeDef = (*Union)(nil)
	_ TypeDef = (*Enum)(nil)
	_ TypeDef = (*List)(nil)
	_ TypeDef = (*MapT)(nil)
	_ TypeDef = (*Tuple)(nil)
	_ TypeDef = (*Literal)(nil)
	_ TypeDef = (*External)(nil)
	_ TypeDef = (*Any)(nil)
)

// PrimitiveKind names a built-in primitive scalar (docs/ir/primitives.md). The set
// is the union of TypeSpec's intrinsic scalars, JSON Schema's types, and
// Protobuf's needs.
type PrimitiveKind string

// Primitive kinds.
const (
	// PrimitiveBool is a boolean.
	PrimitiveBool PrimitiveKind = "bool"
	// PrimitiveString is a Unicode string.
	PrimitiveString PrimitiveKind = "string"
	// PrimitiveBytes is a byte string.
	PrimitiveBytes PrimitiveKind = "bytes"
	// PrimitiveInt8 is a signed 8-bit integer.
	PrimitiveInt8 PrimitiveKind = "int8"
	// PrimitiveInt16 is a signed 16-bit integer.
	PrimitiveInt16 PrimitiveKind = "int16"
	// PrimitiveInt32 is a signed 32-bit integer.
	PrimitiveInt32 PrimitiveKind = "int32"
	// PrimitiveInt64 is a signed 64-bit integer.
	PrimitiveInt64 PrimitiveKind = "int64"
	// PrimitiveUint8 is an unsigned 8-bit integer.
	PrimitiveUint8 PrimitiveKind = "uint8"
	// PrimitiveUint16 is an unsigned 16-bit integer.
	PrimitiveUint16 PrimitiveKind = "uint16"
	// PrimitiveUint32 is an unsigned 32-bit integer.
	PrimitiveUint32 PrimitiveKind = "uint32"
	// PrimitiveUint64 is an unsigned 64-bit integer.
	PrimitiveUint64 PrimitiveKind = "uint64"
	// PrimitiveInteger is an arbitrary-precision integer (JSON Schema/TypeSpec integer).
	PrimitiveInteger PrimitiveKind = "integer"
	// PrimitiveFloat32 is a 32-bit IEEE-754 float.
	PrimitiveFloat32 PrimitiveKind = "float32"
	// PrimitiveFloat64 is a 64-bit IEEE-754 float.
	PrimitiveFloat64 PrimitiveKind = "float64"
	// PrimitiveFloat is an arbitrary-precision binary float (supertype of float32/64).
	PrimitiveFloat PrimitiveKind = "float"
	// PrimitiveNumber is an arbitrary-precision number (JSON Schema number, TypeSpec numeric).
	PrimitiveNumber PrimitiveKind = "number"
	// PrimitiveDecimal is an arbitrary-precision decimal.
	PrimitiveDecimal PrimitiveKind = "decimal"
	// PrimitiveDecimal128 is a 128-bit IEEE-754 decimal.
	PrimitiveDecimal128 PrimitiveKind = "decimal128"
	// PrimitiveDate is a calendar date without time.
	PrimitiveDate PrimitiveKind = "date"
	// PrimitiveTime is a time of day without date or offset.
	PrimitiveTime PrimitiveKind = "time"
	// PrimitiveDatetime is a date and time without offset.
	PrimitiveDatetime PrimitiveKind = "datetime"
	// PrimitiveDatetimeOffset is a date and time with UTC offset.
	PrimitiveDatetimeOffset PrimitiveKind = "datetime_offset"
	// PrimitiveDuration is a time span.
	PrimitiveDuration PrimitiveKind = "duration"
	// PrimitiveURL is a URL.
	PrimitiveURL PrimitiveKind = "url"
	// PrimitiveUUID is a UUID.
	PrimitiveUUID PrimitiveKind = "uuid"
	// PrimitiveAny is an unknown/JSON any (schemaless) primitive.
	PrimitiveAny PrimitiveKind = "any"
)

// Valid reports whether k is one of the kinds declared above. PrimitiveKind is a bare
// string enum, so nothing rejects an invented or stale value on the wire, and
// PrimitiveTypeID derives a consistent ID from any string it is handed — an invented
// kind agrees with its own ID and reads as sound. irverify calls this so a kind
// no emitter can switch on is reported as the compiler bug it is, rather than
// reaching a target that has no type to lower it to.
func (k PrimitiveKind) Valid() bool {
	switch k {
	case PrimitiveBool, PrimitiveString, PrimitiveBytes,
		PrimitiveInt8, PrimitiveInt16, PrimitiveInt32, PrimitiveInt64,
		PrimitiveUint8, PrimitiveUint16, PrimitiveUint32, PrimitiveUint64,
		PrimitiveInteger, PrimitiveFloat32, PrimitiveFloat64, PrimitiveFloat, PrimitiveNumber,
		PrimitiveDecimal, PrimitiveDecimal128,
		PrimitiveDate, PrimitiveTime, PrimitiveDatetime, PrimitiveDatetimeOffset, PrimitiveDuration,
		PrimitiveURL, PrimitiveUUID, PrimitiveAny:
		return true
	default:
		return false
	}
}

// maxDomainDepth bounds the walk up domainParents. It exceeds the longest
// chain, int8 up to number, so it only guards a table edited into a cycle.
const maxDomainDepth = 8

// domainParents returns the immediate supertypes of k by value domain: the
// kinds that hold every finite value of k, with no kind between
// (docs/ir/primitives.md). A kind with none is a root. PrimitiveAny is the top of
// every kind without being an edge; see StrictlyWithin. The result is fresh, so
// a caller may keep it.
func (k PrimitiveKind) domainParents() []PrimitiveKind {
	switch k {
	case PrimitiveInt8:
		return []PrimitiveKind{PrimitiveInt16}
	case PrimitiveInt16:
		return []PrimitiveKind{PrimitiveInt32, PrimitiveFloat32}
	case PrimitiveInt32:
		return []PrimitiveKind{PrimitiveInt64, PrimitiveFloat64}
	case PrimitiveInt64, PrimitiveUint64:
		return []PrimitiveKind{PrimitiveInteger, PrimitiveDecimal128}
	case PrimitiveUint8:
		return []PrimitiveKind{PrimitiveUint16, PrimitiveInt16}
	case PrimitiveUint16:
		return []PrimitiveKind{PrimitiveUint32, PrimitiveInt32, PrimitiveFloat32}
	case PrimitiveUint32:
		return []PrimitiveKind{PrimitiveUint64, PrimitiveInt64, PrimitiveFloat64}
	case PrimitiveInteger:
		return []PrimitiveKind{PrimitiveFloat}
	case PrimitiveFloat32:
		return []PrimitiveKind{PrimitiveFloat64}
	case PrimitiveFloat64:
		return []PrimitiveKind{PrimitiveFloat}
	case PrimitiveFloat:
		return []PrimitiveKind{PrimitiveDecimal}
	case PrimitiveDecimal:
		return []PrimitiveKind{PrimitiveNumber}
	case PrimitiveDecimal128:
		return []PrimitiveKind{PrimitiveDecimal}
	case PrimitiveBool, PrimitiveString, PrimitiveBytes, PrimitiveNumber,
		PrimitiveDate, PrimitiveTime, PrimitiveDatetime, PrimitiveDatetimeOffset, PrimitiveDuration,
		PrimitiveURL, PrimitiveUUID, PrimitiveAny:
		return nil
	default:
		return nil
	}
}

// StrictlyWithin reports whether every finite value of k is a value of broad and the
// kinds differ: broad is reached by walking domainParents up from k, or is
// PrimitiveAny and k is another valid kind. It is a strict partial order, and the
// walk is bounded by maxDomainDepth.
func (k PrimitiveKind) StrictlyWithin(broad PrimitiveKind) bool {
	if k == broad || !k.Valid() || !broad.Valid() {
		return false
	}
	if broad == PrimitiveAny {
		return true
	}
	frontier := []PrimitiveKind{k}
	for depth := 0; depth < maxDomainDepth && len(frontier) > 0; depth++ {
		var next []PrimitiveKind
		for _, kind := range frontier {
			for _, parent := range kind.domainParents() {
				if parent == broad {
					return true
				}
				next = append(next, parent)
			}
		}
		frontier = next
	}
	return false
}

// AdditionalMode describes the openness of a model's property set beyond its
// declared properties and its catch-all's patterns (docs/ir/models.md#catch-alls).
type AdditionalMode string

// Additional-property modes.
const (
	// AdditionalUnspecified leaves openness unspecified (open by JSON Schema default).
	AdditionalUnspecified AdditionalMode = ""
	// AdditionalClosed forbids properties beyond the declared set and the
	// catch-all's patterns (additionalProperties: false, closed-by-construction
	// records).
	AdditionalClosed AdditionalMode = "closed"
	// AdditionalClosedAfterComposition closes the set once composition is resolved
	// (unevaluatedProperties: false with no additionalProperties beside it).
	AdditionalClosedAfterComposition AdditionalMode = "closed_after_composition"
)

// Valid reports whether m is one of the modes declared above. AdditionalMode is
// a bare string enum, so nothing rejects an invented value on the wire;
// irverify calls this so that one is reported rather than read as open.
func (m AdditionalMode) Valid() bool {
	switch m {
	case AdditionalUnspecified, AdditionalClosed, AdditionalClosedAfterComposition:
		return true
	default:
		return false
	}
}

// Primitive is a built-in scalar leaf type (docs/ir/primitives.md).
type Primitive struct {
	TypeCommon
	// PrimitiveKind selects the primitive kind.
	PrimitiveKind PrimitiveKind `json:"primitiveKind"`
}

// Scalar is a named restricted or extended primitive (docs/ir/scalars.md).
// Emitters resolve the Base chain to the nearest representable base,
// accumulating constraints and encoding along the way.
type Scalar struct {
	TypeCommon
	// Base is what this scalar extends: a primitive or another scalar in a
	// restriction chain, or — where the scalar is the alias a declaring position
	// hoisted over a shared target
	// (docs/ir/annotations.md#one-structural-home-per-declaration) — a node of
	// any kind, which a consumer following the chain must expect and stop at.
	// nil = opaque scalar with implementation-defined representation (GraphQL
	// custom scalars).
	Base *TypeRef `json:"base,omitzero"`
	// Constraints restricts the scalar's admissible values.
	Constraints *Constraints `json:"constraints,omitzero"`
	// Encoding overrides the scalar's wire encoding.
	Encoding *Encoding `json:"encoding,omitzero"`
}

// Model is a struct, object, or message shape (docs/ir/models.md). Properties
// holds only the model's own properties; consumers walk Base, Implements, and
// Mixins for the full shape.
type Model struct {
	TypeCommon
	// Properties are the model's own properties, in source order.
	Properties []Property `json:"properties,omitempty"`
	// Requires names, by wire name and in the order the source lists them, each
	// member an instance must carry that no property of the model's own
	// declares: one its Base, Implements or Mixins declare, which it makes
	// required, or one only its catch-all governs. A name an own property
	// declares is that property's Required instead, so no name is in both.
	Requires []string `json:"requires,omitempty"`
	// Base is the declared single inheritance parent (TypeSpec extends,
	// allOf-as-inheritance).
	Base *TypeRef `json:"base,omitzero"`
	// Implements is N-ary interface conformance (GraphQL implements A & B);
	// targets are Abstract models.
	Implements []TypeRef `json:"implements,omitempty"`
	// Mixins is composition without subtyping (Smithy mixins, TypeSpec spread,
	// extra allOf entries).
	Mixins []TypeRef `json:"mixins,omitempty"`
	// AdditionalProperties is the catch-all for members beside the model's declared
	// structure. A catch-all with nothing beside it is a MapT, never a Model
	// (AsMap).
	AdditionalProperties *AdditionalProperties `json:"additionalProperties,omitzero"`
	// Additional is the openness of the property set.
	Additional AdditionalMode `json:"additional,omitempty"`
	// Constraints bounds the property set's cardinality (JSON Schema
	// minProperties/maxProperties on an object). Openness is Additional's
	// concern; this is how many properties an instance may carry.
	Constraints *Constraints `json:"constraints,omitzero"`
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
	Discriminator *Discriminator `json:"discriminator,omitzero"`
	// DiscriminatorValue is set on each subtype one tag routes to: that wire tag
	// value (docs/ir/models.md#discriminator).
	DiscriminatorValue string `json:"discriminatorValue,omitempty"`
	// InputOnly marks GraphQL input types, which have distinct identity from
	// output types.
	InputOnly bool `json:"inputOnly"`
}

// WireIDRange is an inclusive range of wire IDs (docs/ir/models.md).
type WireIDRange struct {
	// From is the first wire ID in the range.
	From int `json:"from"`
	// To is the last wire ID in the range.
	To int `json:"to"`
}

// AdditionalProperties describes a model's catch-all for undeclared properties
// (docs/ir/models.md). It types the members beside a model's declared
// structure; a map that declares nothing else is a MapT.
type AdditionalProperties struct {
	// Value is the value schema for a key no pattern matches; nil = no such key
	// is allowed, as Tuple.Rest is nil when no element may follow. It is nil
	// only beside Patterns, since a catch-all naming neither admits nothing a
	// closed model does not already say.
	Value *TypeRef `json:"value,omitzero"`
	// Key is the key schema; nil = string keys, the one spelling of them.
	Key *TypeRef `json:"key,omitzero"`
	// Patterns are key-pattern-scoped value schemas (JSON Schema patternProperties).
	Patterns []PatternProperties `json:"patterns,omitempty"`
}

// PatternProperties binds a key pattern to a value schema (JSON Schema
// patternProperties).
type PatternProperties struct {
	// Pattern is the ECMA-262 key pattern.
	Pattern string `json:"pattern"`
	// Value is the value schema for matching keys.
	Value TypeRef `json:"value"`
}

// Discriminator locates and maps the tag that selects a variant in a
// polymorphic model hierarchy or union (docs/ir/models.md#discriminator).
// Exactly one of Property, PropertyName, or Index locates the tag.
type Discriminator struct {
	// Property is the property carrying the tag in model hierarchies, the
	// declaration the flattening of the model holding it takes
	// (docs/ir/models.md#the-flattening); empty when PropertyName or Index
	// locates the tag instead.
	Property PropertyID `json:"property,omitempty"`
	// PropertyName is the wire name of the tag property in unions, where the
	// property exists on no single model (TypeSpec @discriminated
	// discriminatorPropertyName, OpenAPI discriminator on oneOf).
	PropertyName string `json:"propertyName,omitempty"`
	// TupleElementIndex is the 0-based tuple element carrying the tag Literal (Erlang tagged
	// tuples, JSON arrays with a const head via prefixItems).
	TupleElementIndex *int `json:"tupleElementIndex,omitzero"`
	// Mapping maps a wire value to the type it routes to, a union's variant or
	// a hierarchy's base or subtype, and no value is empty. Nil on a union, it
	// routes by type name; otherwise it holds the routes declared or inferred
	// and the one the source implies to each variant they leave out. In a model
	// hierarchy, each subtype's DiscriminatorValue routes to it too.
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
	// Inferred marks a discriminator discovered heuristically, not declared: its
	// tag is read off what each variant pins. A heuristic filling only the
	// Mapping of a declared one leaves it false, and names itself in the
	// holder's Provenance.Inferred, as it does where it sets this.
	Inferred bool `json:"inferred"`
}

// Union is a sum over variant types (docs/ir/unions.md). One node covers
// untagged anyOf/oneOf, unions a tag in the payload routes, and key-tagged
// unions.
type Union struct {
	TypeCommon
	// Variants are the union's members, in source order.
	Variants []Variant `json:"variants,omitempty"`
	// Exclusive is true when exactly one variant may match (oneOf) and false
	// when one or more may (anyOf). It records what validation admits, which a
	// tag does not change: a discriminator on an anyOf picks one of the
	// variants a payload satisfies.
	Exclusive bool `json:"exclusive"`
	// WireTagged reports that the payload names its variant: by the tag
	// Discriminator locates (an OpenAPI discriminator, GraphQL __typename, an
	// Erlang tagged tuple), or, with no Discriminator, by the one key of a
	// single-key object (Smithy union, protobuf oneof). False means validation
	// infers the variant (untagged JSON oneOf/anyOf).
	WireTagged bool `json:"wireTagged"`
	// Discriminator locates the tag in the payload, by property name or tuple
	// position; nil for an untagged or key-tagged union. A union carrying one
	// is WireTagged.
	Discriminator *Discriminator `json:"discriminator,omitzero"`
	// Constraints are the bounds a source writes beside the union, each bounding
	// a value of the kind it applies to, whichever variant holds the value, and
	// admitting every value of another kind
	// (docs/ir/unions.md#bounds-beside-a-union); nil = none written.
	Constraints *Constraints `json:"constraints,omitzero"`
	// Closed is false for an open union, whose payload may hold a variant the
	// union does not list, which a consumer must be able to represent (Smithy
	// unions, protobuf oneofs, GraphQL output unions). It defaults per source
	// semantics, as Enum.Closed does: a JSON Schema oneOf or anyOf is closed.
	Closed bool `json:"closed"`
	// InputOnly marks a union only a request may carry, with an identity
	// distinct from every output type, as Model.InputOnly marks an input
	// object (GraphQL @oneOf input objects).
	InputOnly bool `json:"inputOnly"`
}

// Variant is one member of a Union (docs/ir/unions.md).
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
	WireID *int `json:"wireID,omitzero"`
	// XML is @xmlName/@xmlNamespace on union members.
	XML *XMLHints `json:"xml,omitzero"`
	// Event is event-stream metadata when the union is a stream's event set.
	Event *EventInfo `json:"event,omitzero"`
	// Docs is the variant's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the variant as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitzero"`
	// Availability records the variant's versioning timeline.
	Availability *Availability `json:"availability,omitzero"`
	// Examples are typed example values for the variant.
	Examples []Example `json:"examples,omitempty"`
	// Unmodeled holds source constructs the IR does not model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
}

// EventInfo carries per-event metadata when a union is an event stream's event
// set (docs/ir/unions.md).
type EventInfo struct {
	// ContentType is the per-event content type (TypeSpec @Events.contentType).
	ContentType string `json:"contentType,omitempty"`
	// Terminal reports that receiving this event ends the stream (TypeSpec
	// @SSE.terminalEvent).
	Terminal bool `json:"terminal"`
}

// Enum is a closed or open set of named values (docs/ir/enums.md).
type Enum struct {
	TypeCommon
	// ValueType is the primitive type of the members' values.
	ValueType PrimitiveKind `json:"valueType"`
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

// EnumMember is one member of an Enum (docs/ir/enums.md).
type EnumMember struct {
	// ID identifies the member within the document. It is keyed by the member's
	// value, never its name or position, so reordering members whose values are
	// distinct renames none.
	ID EnumMemberID `json:"id"`
	// Name is the member's naming.
	Name Naming `json:"name"`
	// Value is the member's typed value, matching Enum.ValueType.
	Value Value `json:"value"`
	// WireName is the serialized form when it differs from Value (rare).
	WireName string `json:"wireName,omitempty"`
	// Docs is the member's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the member as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitzero"`
	// Availability records the member's versioning timeline (TypeSpec @added on
	// EnumMember).
	Availability *Availability `json:"availability,omitzero"`
	// Examples are typed example values for the member.
	Examples []Example `json:"examples,omitempty"`
	// Unmodeled holds source constructs the IR does not model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
}

// List is an ordered collection (docs/ir/containers.md).
type List struct {
	TypeCommon
	// ElementType is the element type.
	ElementType TypeRef `json:"elementType"`
	// Constraints restricts minItems/maxItems/uniqueItems.
	Constraints *Constraints `json:"constraints,omitzero"`
	// Encoding is the container-level wire encoding (protobuf packed vs expanded
	// repeated fields); it stacks with the element's own encoding.
	Encoding *Encoding `json:"encoding,omitzero"`
}

// MapT is a keyed collection and nothing else: TypeSpec Record, a protobuf
// map, an OpenAPI object whose only statement about its members is
// additionalProperties (docs/ir/containers.md). A catch-all beside any other
// member fact stays a Model's AdditionalProperties; AsMap draws that line for every
// producer.
type MapT struct {
	TypeCommon
	// Key is the key type; nil = string keys, the one spelling of them, as for
	// AdditionalProperties.Key.
	Key *TypeRef `json:"key,omitzero"`
	// Value is the value type.
	Value TypeRef `json:"value"`
	// Constraints bounds how many entries an instance may carry (JSON Schema
	// minProperties/maxProperties, Smithy @length on a map).
	Constraints *Constraints `json:"constraints,omitzero"`
}

// Tuple is a positional sequence: a type for each leading position, then the
// type of whatever follows them (prefixItems and items, TypeSpec tuples, Erlang
// tuples) (docs/ir/containers.md). Elems types positions without requiring
// them: how many elements an instance must hold is Constraints.MinItems, so a
// tuple of fixed length sets it to len(Elems) and has no Rest.
type Tuple struct {
	TypeCommon
	// ElementTypes are the element types in position order.
	ElementTypes []TypeRef `json:"elementTypes,omitempty"`
	// Rest types every element past Elems; nil = no element may follow them.
	Rest *TypeRef `json:"rest,omitzero"`
	// Constraints bounds the whole instance, Elems and Rest together
	// (minItems/maxItems/uniqueItems).
	Constraints *Constraints `json:"constraints,omitzero"`
}

// Literal is a single constant value used as a type: JSON Schema const, a
// TypeSpec literal type, an Erlang atom (docs/ir/containers.md). An enum of one
// member is an Enum, never a Literal (docs/ir/enums.md), and a constant of null
// is an Enum with no member behind a nullable reference.
type Literal struct {
	TypeCommon
	// Value is the constant value.
	Value Value `json:"value"`
	// ValueType is the primitive the source types the value as, as
	// Enum.ValueType types an enum's members: an integer and a number of one
	// value differ here, as do a string and a date. "" where the value is of no
	// primitive, a list or an object.
	ValueType PrimitiveKind `json:"valueType,omitempty"`
}

// External is a well-known library type that no target can structurally model,
// resolved to a runtime handle by the emitter (TCGC external)
// (docs/ir/containers.md).
type External struct {
	TypeCommon
	// Identity is the external type's stable identity (e.g. "erlang:pid").
	Identity string `json:"identity"`
	// Package is the providing package.
	Package string `json:"package,omitempty"`
	// MinVersion is the minimum package version.
	MinVersion string `json:"minVersion,omitempty"`
}

// Any is a schemaless type (docs/ir/containers.md).
type Any struct {
	TypeCommon
}

func (*Primitive) typeDef() {}

// Kind reports the TypeKind of a Primitive.
func (*Primitive) Kind() TypeKind { return KindPrimitive }

func (*Scalar) typeDef() {}

// Kind reports the TypeKind of a Scalar.
func (*Scalar) Kind() TypeKind { return KindScalar }

func (*Model) typeDef() {}

// Kind reports the TypeKind of a Model.
func (*Model) Kind() TypeKind { return KindModel }

func (*Union) typeDef() {}

// Kind reports the TypeKind of a Union.
func (*Union) Kind() TypeKind { return KindUnion }

func (*Enum) typeDef() {}

// Kind reports the TypeKind of an Enum.
func (*Enum) Kind() TypeKind { return KindEnum }

func (*List) typeDef() {}

// Kind reports the TypeKind of a List.
func (*List) Kind() TypeKind { return KindList }

func (*MapT) typeDef() {}

// Kind reports the TypeKind of a MapT.
func (*MapT) Kind() TypeKind { return KindMap }

func (*Tuple) typeDef() {}

// Kind reports the TypeKind of a Tuple.
func (*Tuple) Kind() TypeKind { return KindTuple }

func (*Literal) typeDef() {}

// Kind reports the TypeKind of a Literal.
func (*Literal) Kind() TypeKind { return KindLiteral }

func (*External) typeDef() {}

// Kind reports the TypeKind of an External.
func (*External) Kind() TypeKind { return KindExternal }

func (*Any) typeDef() {}

// Kind reports the TypeKind of an Any.
func (*Any) Kind() TypeKind { return KindAny }

// Common returns c itself. TypeCommon embeds by value into every concrete
// TypeDef kind (Primitive, Scalar, Model, Union, Enum, List, MapT, Tuple,
// Literal, External, Any), so this single pointer method is promoted to each
// kind's own pointer type and satisfies the TypeDef.Common contract for every
// one of them; only the unexported typeDef() marker stays declared per kind, so
// embedding TypeCommon elsewhere cannot join the sealed sum.
func (c *TypeCommon) Common() *TypeCommon { return c }
