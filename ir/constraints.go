package ir

// Constraints restricts the admissible values of a scalar, list, string, or
// numeric type (ir-design §5.3). Numeric bounds are arbitrary-precision decimal
// strings, never float64.
type Constraints struct {
	// Min is the inclusive (or exclusive, per ExclusiveMin) lower numeric bound.
	Min *BigVal `json:"min,omitempty"`
	// Max is the inclusive (or exclusive, per ExclusiveMax) upper numeric bound.
	Max *BigVal `json:"max,omitempty"`
	// ExclusiveMin makes Min an exclusive bound.
	ExclusiveMin bool `json:"exclusiveMin"`
	// ExclusiveMax makes Max an exclusive bound.
	ExclusiveMax bool `json:"exclusiveMax"`
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
	// Pattern is an ECMA-262 regex as written; backends translate or drop it with
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
// TypeSpec @encode and absorbs OpenAPI format and Protobuf wire variants
// (ir-design §5.3). Property encoding overrides scalar encoding.
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
