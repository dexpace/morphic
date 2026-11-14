# Values

Part of the [IR design](./README.md). The typed-data channel, kept separate from the type graph.

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "bytes" | "symbol" |
                          // "list" | "object" | "ref" | "ctor" | "inf" | "neginf" | "nan"
    Bool   bool
    Str    string         // payload for "string" AND "symbol"
    Num    BigVal         // arbitrary precision, decimal string form
    Bytes  []byte         // base64 in JSON form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
    Ref    *ValueRef      // reference to a declared constant: {Type TypeID; Member string}
                          // (TypeSpec enum-member defaults, references to named consts)
    Ctor   *CtorValue     // constructor-built value (TypeSpec scalar constructors)
}

type CtorValue struct {
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
not accidentally. `ctor` captures values built by named scalar constructors
(`utcDateTime.now()`, `plainDate.fromISO("2024-05-06")`) — inherently non-literal, so compilers
must not fold them.

`inf`, `neginf` and `nan` are the IEEE 754 values no `BigVal` holds, each a kind of its own with no
payload. A protobuf field may default to `inf`, `-inf` or `nan`, and every target language with an
IEEE float type has a constant for each, so a source stating one is lossless only if the value
channel can too. They are kinds rather than a flag on `number`, so a consumer switching on `Kind`
meets them as kinds it must handle, never as a `number` whose `Num` is empty; a NaN keeps no payload
bits, which no source format states. Keeping the decimal channel finite is still the rule:
`BigVal` stays a JSON number, and `ir.Decimal` never reads one of these. No JSON instance is
non-finite, so no OpenAPI or AsyncAPI document lowers to them.
