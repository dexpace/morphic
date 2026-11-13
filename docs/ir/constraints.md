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

Constraints speak of the value, the instance of the node's logical type, and never of its wire form
([the type table](../emitters/contract/type-table.md#a-row-keyed-by-an-encoding)). Under an
[encoding](#encodings) that turns the value into another primitive, a bound the source wrote of the
wire form is no bound of the value: the length or the pattern of the text an `int64` travels as
bounds no `int64`, and a source's numeric bound written beside that text, which JSON Schema applies
to no string, bounds nothing it says. A compiler keeps such a bound verbatim
([boundaries](./boundaries.md)) rather than move it onto the value, as the OpenAPI compiler does for
a number carried as text ([`string-formats`](../compilers/openapi.md#string-formats)); it still
holds a length or pattern written beside `format: byte` or `base64url` as a bound of the bytes, one
written beside `format: http-date`, or a numeric bound beside `format: unixtime`, as a bound of the
instant, and one written beside a reference to a number carried as text as a bound of the number
(#1851).

A count field, `MinLength`, `MaxLength`, `MinItems`, `MaxItems`, `MinProps` or `MaxProps`, holds a
count from 0 to 2^63−1. A source may write a larger one, as JSON Schema's counts have no upper limit:
it reaches no field and is kept verbatim under `Unmodeled` with `ReasonDegradedLowering`
([degraded constructs](./boundaries.md#degraded-constructs)), so an absent count beside such an
entry bounds nothing less than the entry does.

## Encodings

```go
type Encoding struct {
    Name     string    // "rfc3339" | "unixTimestamp" | "base64" | "base64url" | "iso8601" |
                       // "seconds" | "http-date" | "varint" | "zigzag" | "fixed" |
                       // "packed" | "expanded" | "delimited" | "string" | format strings
                       // (packed/expanded are container encodings on List.Encoding; "delimited" =
                       // protobuf group / editions DELIMITED message encoding; "string" = a number
                       // written as the text of its value)
    WireType *TypeRef  // the on-wire primitive when the encoding turns the logical value into
                       // another (utcDateTime encoded as int32; bytes, or a number, as a string;
                       // over a string, text the encoding is applied to)
    MediaType string   // content media type of the value itself (Smithy @mediaType on string/blob,
                       // JSON Schema contentMediaType); "" = none
    Schema   *TypeRef  // the shape the encoded value has once decoded (JSON Schema contentSchema):
                       // what a base64 blob or an application/json-typed string holds; nil = unstated
}
```

The logical-type / encoding-name / wire-type triple is TCGC's reification of TypeSpec `@encode`
and also absorbs OpenAPI `format` and Protobuf's `sint*/fixed*` wire variants. It has one
orientation: a scalar's `Base` is the logical type, the value a caller holds, and `WireType` the
primitive the encoding turns it into on the wire, so `bytes` under `base64` and an `int64` under
`string`, the number written as the text of its value, both travel as a `string`, as OpenAPI's
`format: byte` and `type: string, format: int64` write them. Over a `string` the logical and the wire
primitive are one, and `WireType` is what tells the two directions apart. Absent, the string is the
encoded text already, as JSON Schema defines `contentEncoding`, and a consumer passes it through. A
`WireType` of `string` makes it text the encoding is applied to on its way to the wire, as Smithy
sends a `@mediaType` string in a header: its UTF-8 octets, encoded. The other spelling of that
header, a `bytes` position under `base64`, was weighed and not taken: the header would then name a
type of its own where it binds the string shape every other use names, and only the position's own
`Encoding`, overriding the shape's, says how it travels there. `MediaType` and
`Schema` sit beside it because they answer the same question one level in: what the encoded value
*is*. `Schema` is a `TypeRef` like any other schema position — the decoded shape is hoisted into
the registry at its own source pointer, never carried here as a raw payload. Encoding attaches
at the scalar definition and overrides at the property field by field: a field the property sets
replaces the scalar's, and one it leaves unset, an empty string or a nil reference, takes the
scalar's, so a property renaming the encoding keeps the scalar's `Schema` and `MediaType`. A
compiler writes only what the position declared, as JSON Schema applies the keywords at both places;
the alternative, the property's `Encoding` replacing the scalar's whole, would make a compiler copy
every field it did not mean to change, and lose them where it forgot one. Protobuf editions features
lower here per element after the compiler resolves the feature cascade (descriptors expose
resolved values): `field_presence` → `Presence`, `enum_type` → `Closed`,
`repeated_field_encoding` → `List.Encoding`, `message_encoding` → `"delimited"`; remaining axes
(`utf8_validation`, `json_format`) → namespaced `Unmodeled` entries.

## XML hints

XML-capable formats (OpenAPI's `xml` object, Smithy's `xmlName`/`xmlAttribute`/`xmlNamespace`/
`xmlFlattened` traits, TypeSpec's `@Xml.*` decorators) describe an XML wire shape that diverges
from the JSON-implied one. This is typed, not an extension, because multiple formats express it
and emitters must act on it. Hints attach at both levels: `TypeCommon.XML` (root element
name/namespace of the shape itself) and `Property.XML`, overriding the type's field by field, as
an encoding does: a field the property sets replaces the type's, and one it leaves unset takes it.
So each field keeps what its source declared, a declared `false` included, and nothing a compiler
writes stands for a value the source left unstated.

`NodeType` and `Attribute` spell one fact, the node's kind: `NodeType` as OpenAPI 3.2 names it, and
`Attribute` as a flag says it, OpenAPI's `attribute` or Smithy's `xmlAttribute`, the field a format
writing only the flag fills. They override as one, so a property setting either replaces the type's
kind, and a level setting both is read by `NodeType`. Folding the flag into `NodeType` would have
left a declared `attribute: false` nothing to be: it is not `element` on an array, where 3.2's
`element` means wrapped, and an unset `NodeType` would take the type's.

```go
type XMLHints struct {
    Name      string   // element/attribute name override
    Namespace string   // namespace URI
    Prefix    string
    NodeType  string   // "" | "element" | "attribute" | "text" | "cdata" | "none"
                       // (OpenAPI 3.2 nodeType; "text" covers Smithy httpPayload text)
    Attribute *bool    // the node is an attribute, not an element; nil = unstated, so a
                       // property's false overrides a type's true
    Wrapped   *bool    // list items wrapped in a container element; nil = unstated, so a
                       // property's false overrides a type's true
}
```
