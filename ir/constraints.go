package ir

// Constraints restricts the admissible values of a scalar, list, string, or
// numeric type (ir-design §5.3). Numeric bounds are arbitrary-precision decimal
// strings, never float64.
//
// Every Constraints is position-scoped: it holds what the position carrying it
// declared, and nothing is ever copied across a TypeRef. Bounds conjoin rather
// than override, so the effective restriction on a value is this struct
// together with the Constraints of every node reached from the position's
// TypeRef, and an absent Constraints means that position declared no bound —
// never that the value is unbounded (ir-design §12.2). Documentation,
// deprecation and Default are the other way round: a compiler merges them from
// a $ref's target onto the referencing carrier with use-site precedence, so a
// use site already carries those and resolves nothing to read them.
type Constraints struct {
	// Min is the inclusive lower numeric bound (JSON Schema minimum): an
	// admissible value is >= it. nil = this position declared none.
	Min *BigVal `json:"min,omitempty"`
	// Max is the inclusive upper numeric bound (JSON Schema maximum): an
	// admissible value is <= it. nil = this position declared none.
	Max *BigVal `json:"max,omitempty"`
	// ExclusiveMin is the exclusive lower numeric bound (JSON Schema
	// exclusiveMinimum): an admissible value is > it. nil = this position
	// declared none.
	//
	// It is a bound of its own rather than a flag on Min, because the two
	// keywords are independent and conjunctive: a schema may declare both, both
	// then apply, and the effective floor is whichever admits fewer values. One
	// slot per side would have to keep that one and lower the other some other
	// way, which is a change to the weaker keyword that a consumer diffing two
	// revisions of a spec could not see at all (GitHub #425).
	ExclusiveMin *BigVal `json:"exclusiveMin,omitempty"`
	// ExclusiveMax is the exclusive upper numeric bound (JSON Schema
	// exclusiveMaximum): an admissible value is < it. nil = this position
	// declared none. It is independent of Max exactly as ExclusiveMin is of Min.
	ExclusiveMax *BigVal `json:"exclusiveMax,omitempty"`
	// MultipleOf constrains the value to a multiple of this number.
	MultipleOf *BigVal `json:"multipleOf,omitempty"`
	// Precision bounds the total decimal digits (Avro decimal, XSD totalDigits,
	// OData Edm.Decimal).
	Precision *int64 `json:"precision,omitempty"`
	// Scale bounds the fractional decimal digits (XSD fractionDigits).
	Scale *int64 `json:"scale,omitempty"`
	// MinLength is the minimum string/bytes length.
	MinLength *int64 `json:"minLength,omitempty"`
	// MaxLength is the maximum string/bytes length.
	MaxLength *int64 `json:"maxLength,omitempty"`
	// Pattern is an ECMA-262 regex as written; emitters translate or drop it with
	// a diagnostic.
	Pattern string `json:"pattern,omitempty"`
	// PatternMessage is a human-readable validation message (TypeSpec @pattern's
	// second argument).
	PatternMessage string `json:"patternMessage,omitempty"`
	// MinItems is the minimum collection length.
	MinItems *int64 `json:"minItems,omitempty"`
	// MaxItems is the maximum collection length.
	MaxItems *int64 `json:"maxItems,omitempty"`
	// UniqueItems requires distinct collection elements.
	UniqueItems bool `json:"uniqueItems"`
	// MinProps is the minimum number of properties.
	MinProps *int64 `json:"minProps,omitempty"`
	// MaxProps is the maximum number of properties.
	MaxProps *int64 `json:"maxProps,omitempty"`
}

// Encoding is the logical-type / encoding-name / wire-type triple that reifies
// TypeSpec @encode and absorbs OpenAPI format and Protobuf wire variants, plus
// the media type and decoded shape of an encoded payload (ir-design §5.3).
// Property encoding overrides scalar encoding.
type Encoding struct {
	// Name is the encoding scheme ("rfc3339", "base64", "zigzag", "packed",
	// "delimited", format strings, ...).
	Name string `json:"name,omitempty"`
	// WireType is the on-wire primitive when it differs from the logical type
	// (utcDateTime encoded as int32; bytes as base64 string).
	WireType *TypeRef `json:"wireType,omitempty"`
	// MediaType is the content media type of the value itself (Smithy @mediaType,
	// JSON Schema contentMediaType); "" = none.
	MediaType string `json:"mediaType,omitempty"`
	// Schema is the shape the encoded value has once decoded — what a base64 blob
	// or an application/json-typed string holds (JSON Schema contentSchema); nil =
	// unstated. It is a reference into the type registry like any other schema,
	// never the encoded value's own type.
	Schema *TypeRef `json:"schema,omitempty"`
}

// XMLHints describes an XML wire shape that diverges from the JSON-implied one
// (ir-design §5.4). Hints attach at TypeCommon (root shape) and Property (per-use
// overrides; property wins).
type XMLHints struct {
	// Name is the element/attribute name override.
	Name string `json:"name,omitempty"`
	// Namespace is the namespace URI.
	Namespace string `json:"namespace,omitempty"`
	// Prefix is the namespace prefix.
	Prefix string `json:"prefix,omitempty"`
	// NodeType is "", "element", "attribute", "text", "cdata", or "none" (OpenAPI
	// 3.2 nodeType; "text" covers Smithy httpPayload text).
	NodeType string `json:"nodeType,omitempty"`
	// Wrapped reports that list items are wrapped in a container element.
	Wrapped bool `json:"wrapped"`
}
