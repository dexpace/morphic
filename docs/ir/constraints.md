# Constraints, encodings and XML hints

Part of the [IR design](./README.md). What a value admits, how it travels on the wire, and the XML
shape where that diverges from the JSON one.

## Constraints

```go
type Constraints struct {
    // numeric — arbitrary-precision decimal strings, never float64 (TypeSpec Numeric lesson)
    Min, Max           *BigVal    // inclusive bounds (minimum / maximum)
    ExclusiveMin, ExclusiveMax *BigVal // exclusive bounds (exclusiveMinimum / exclusiveMaximum);
                                  // independent of Min/Max, not flags on them — a schema may
                                  // declare both per side and both apply
    MultipleOf         *BigVal
    Precision, Scale   *int64     // decimal digit bounds (Avro decimal, XSD totalDigits/fractionDigits,
                                  // OData Edm.Decimal)
    // string & bytes — length constraints apply to both (Avro fixed(N) = bytes, MinLength=MaxLength=N)
    MinLength, MaxLength *int64
    Pattern            string      // ECMA-262 regex as written; emitters translate or drop with a diagnostic
    PatternMessage     string      // human-readable validation message (TypeSpec @pattern's second arg)
    // collections
    MinItems, MaxItems *int64
    UniqueItems        bool
    MinProps, MaxProps *int64
}
```

## Encodings

```go
type Encoding struct {
    Name     string    // "rfc3339" | "unixTimestamp" | "base64" | "base64url" | "iso8601" |
                       // "seconds" | "http-date" | "varint" | "zigzag" | "fixed" |
                       // "packed" | "expanded" | "delimited" | format strings
                       // (packed/expanded are container encodings on List.Encoding; "delimited" =
                       // protobuf group / editions DELIMITED message encoding)
    WireType *TypeRef  // the on-wire primitive when it differs from the logical type
                       // (utcDateTime encoded as int32; bytes as base64 string)
    MediaType string   // content media type of the value itself (Smithy @mediaType on string/blob,
                       // JSON Schema contentMediaType); "" = none
    Schema   *TypeRef  // the shape the encoded value has once decoded (JSON Schema contentSchema):
                       // what a base64 blob or an application/json-typed string holds; nil = unstated
}
```

The logical-type / encoding-name / wire-type triple is TCGC's reification of TypeSpec `@encode`
and also absorbs OpenAPI `format` and Protobuf's `sint*/fixed*` wire variants. `MediaType` and
`Schema` sit beside it because they answer the same question one level in: what the encoded value
*is*. `Schema` is a `TypeRef` like any other schema position — the decoded shape is hoisted into
the registry at its own source pointer, never carried here as a raw payload. Encoding attaches
at the scalar definition or overrides at the property — property wins. Protobuf editions features
lower here per element after the compiler resolves the feature cascade (descriptors expose
resolved values): `field_presence` → `Presence`, `enum_type` → `Closed`,
`repeated_field_encoding` → `List.Encoding`, `message_encoding` → `"delimited"`; remaining axes
(`utf8_validation`, `json_format`) → namespaced `Unmodeled` entries.

## XML hints

XML-capable formats (OpenAPI's `xml` object, Smithy's `xmlName`/`xmlAttribute`/`xmlNamespace`/
`xmlFlattened` traits, TypeSpec's `@Xml.*` decorators) describe an XML wire shape that diverges
from the JSON-implied one. This is typed, not an extension, because multiple formats express it
and emitters must act on it. Hints attach at both levels: `TypeCommon.XML` (root element
name/namespace of the shape itself) and `Property.XML` (per-use overrides; property wins).

```go
type XMLHints struct {
    Name      string   // element/attribute name override
    Namespace string   // namespace URI
    Prefix    string
    NodeType  string   // "" | "element" | "attribute" | "text" | "cdata" | "none"
                       // (OpenAPI 3.2 nodeType; "text" covers Smithy httpPayload text)
    Wrapped   bool     // list items wrapped in a container element
}
```
