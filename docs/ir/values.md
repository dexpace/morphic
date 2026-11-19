# Values

Part of the [IR design](./README.md). The typed-data channel, kept separate from the type graph.

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "bytes" | "symbol" |
                          // "list" | "object" | "reference" | "constructor" | "infinity" | "negative-infinity" | "nan"
    Bool   bool
    String    string         // payload for "string" AND "symbol"
    Number    BigVal         // arbitrary precision, decimal string form
    Bytes  []byte         // base64 in JSON form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
    Ref    *ValueRef      // reference to a declared constant: {Type TypeID; Member string}
                          // (TypeSpec enum-member defaults, references to named consts)
    Constructor   *ConstructorValue     // constructor-built value (TypeSpec scalar constructors)
}

type ConstructorValue struct {
    Scalar TypeID         // the scalar whose constructor is invoked
    Name   string         // constructor name ("fromISO", "now", custom inits)
    Args   []Value
}
```

`BigVal` is a decimal-string wrapper: no float64 round-tripping anywhere in the IR. Its text is a
JSON number: `NewBigVal` drops a leading `+`, gives the integer part one digit, drops a trailing
point, and keeps every significant digit and the exponent as written. Its leading significant digit
carries a power of ten within ±(10¹⁸ − 1) (`ir.MaxDecimalExp`), and zero is held at any exponent.
`ir.Decimal` is the one reading of that text (sign, significant digits, that power of ten), never
the value materialized. Order, equality, integrality and the canonical decimal of an [enum
key](./enums.md#member-identity) (`ir.Decimal.String`) all derive from it.

`symbol` is an interned-symbol value distinct from `string` (Erlang atoms: on the native wire
`ok` ≠ `<<"ok">>`). Emitters without a symbol concept render symbols as strings — explicitly,
not accidentally. `constructor` captures values built by named scalar constructors
(`utcDateTime.now()`, `plainDate.fromISO("2024-05-06")`) — inherently non-literal, so compilers
must not fold them.

`inf`, `neginf` and `nan` are the IEEE 754 values no `BigVal` holds, each a kind of its own with no
payload. A protobuf field may default to `inf`, `-inf` or `nan`, and every target language with an
IEEE float type has a constant for each, so a source stating one is lossless only if the value
channel can too. They are kinds rather than a flag on `number`, so a consumer switching on `Kind`
meets them as kinds it must handle, never as a `number` whose `Number` is empty; a NaN keeps no payload
bits, which no source format states. Keeping the decimal channel finite is still the rule:
`BigVal` stays a JSON number, and `ir.Decimal` never reads one of these. No JSON instance is
non-finite, so no OpenAPI or AsyncAPI document lowers to them.

## Values held to their type

`pass.Validate` reports a default a position's type does not admit, `pass/default-outside-type`, and
an example it does not admit, `pass/example-outside-type`, each a warning. They are two codes because
a default is written into the code an emitter generates and an example reaches its documentation. The
reading is shallow: the value's kind against the shape of the type, an enum's members, a literal's
value, null where the position is not nullable, whole numbers for an integer, and the bounds the
position declares with those of the alias scalars it reaches. A value that is a reference or a
constructor, a type holding anything, a union, an external type, an unknown type and a position written
in an encoding are not judged. The sites are a property's, a parameter's and a header's default and
examples, a type's own examples (a null one says the example is absent and is not judged), and in an
operation's examples each input parameter against the parameter it names, and the error content
against the error's type.

Not judged: a content's and a message's examples, which are in a media type's format, not the type's
value (whether a JSON content's are held is #1949); a scenario's `Body` and `Output`, which are one
content and one response, so no single type; a variant's and an enum member's examples; the elements
of a list and the members of an object; and `Pattern`, `MultipleOf`, `Precision` and `Scale`.
