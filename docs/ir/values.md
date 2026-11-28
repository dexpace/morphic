# Values

Part of the [IR design](./README.md). The typed-data channel, kept separate from the type graph.

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind                // "null" | "bool" | "string" | "number" | "bytes" | "symbol" |
                                  // "list" | "object" | "reference" | "constructor" | "infinity" | "negative-infinity" | "nan"
    Bool        bool
    String      string            // payload for "string" AND "symbol"
    Number      BigVal            // arbitrary precision, decimal string form
    Bytes       []byte            // base64 in JSON form
    List        []Value
    Object      []Field           // ordered; Field{Name string; Value Value}
    Ref         *ValueRef         // reference to a declared constant: {Type TypeID; Member string}
                                  // (TypeSpec enum-member defaults, references to named consts)
    Constructor *ConstructorValue // constructor-built value (TypeSpec scalar constructors)
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
meets them as kinds it must handle, never as a `number` whose `Number` is empty; a NaN keeps no
payload bits, which no source format states. Keeping the decimal channel finite is still the rule:
`BigVal` stays a JSON number, and `ir.Decimal` never reads one of these. No JSON instance is
non-finite, so no OpenAPI or AsyncAPI document lowers to them.

## Values held to their type

`pass.Validate` reports a default a position's type does not admit, `pass/default-outside-type`, and
an example it does not admit, `pass/example-outside-type`, each a warning. They are two codes because
a default is written into the code an emitter generates and an example reaches its documentation. The
reading is shallow: the value's kind against the shape of the type, an enum's members, a literal's
value, null where the position is not nullable, whole numbers for an integer, and the bounds the
position declares with those of the alias scalars it reaches. A value that is a reference or a
constructor, a type holding anything, a union, an external type and an unknown type are not judged,
and of a value written in an encoding only an enum's members, a literal's value and null are, until
#2178 decides which checks an encoding excuses. A value is written in an encoding where an
`Encoding` writes it: that of a scalar or a list its type reaches through aliases, or a position's
own, as a property or a header has, which writes its default as it writes its examples unless it
names a `MediaType` and nothing else, as a header declared by `content` has. A parameter's own
`Encoding` counts only where it names a media type alone: one naming anything else, as the HTTP-date
one the OpenAPI compiler infers for a header parameter does, leaves its values judged, since whether
an encoding that keeps the kind excuses it is #2178's to decide. A media type alone writes the value
in its format without changing what the type holds, so there, and where an HTTP binding writes the
whole parameter through a `ContentType`, only the examples are judged as values written in an
encoding: the default, a value of the type, is judged as it is at any other position. The sites are
a property's, a parameter's and a header's default and examples, a type's own examples (a null one
says the example is absent and is not judged), and in an operation's examples each input parameter
against the type and bounds of the parameter it names, though not its own `Encoding`, and the error
content against the error's type.

A null example of a parameter or a header that is not `Required` stands for the value left out, as a
source writes one to show that it may be omitted, and is not judged, whatever its type, where HTTP
alone writes it in a style, as a query, path, cookie or header field, its own `Encoding` naming no
media type included: such a field has no null to write, so absent is all a null there can mean.
Where a media type writes it instead, through its own `Encoding` naming one alone, as a header
declared by `content` has, or a binding's `ContentType`, or a binding writes the parameter into the
body, or the operation has an RPC, message, GraphQL or OTP binding, its null example is judged as a
property's is: the body, RPC, a message and GraphQL can each write a null, and so can a media type
such as JSON. One that cannot, such as `application/x-www-form-urlencoded`, is judged all the same,
since the check reads no media type's grammar, and OTP has no compiler yet to say whether its terms
can. Still judged: a null example of a required one, whatever writes it; a null default, which is
written into code; a property's null example, since an object can hold a member whose value is null,
a state `Property.Required` and `TypeRef.Nullable` keep apart from the member left out; and a
scenario's null input, since a scenario leaves a parameter out by not naming it. A Schema Object's
null example is read two ways by where the OpenAPI compiler places it
([`schema-examples`](../compilers/openapi.md#schema-examples)): left on a type, as an enum's or a
named schema's is, it says the example is absent, while one moved onto a required parameter or a
property, as a string schema's is, is judged as the position's own. One reading for it wherever it
lands is a decision of its own. The emitter contract still holds a null example of a parameter or a
header that is not required to its type ([one set of
examples](../emitters/docs-and-mock.md#one-set-of-examples)), which #2180 proposes to change.

A parameter's and a header's own examples are their value as written on the wire, which is how
OpenAPI reads them, so a string example of a list or an object (a model or a map) is that value
written out where the position is written in a style that can write its shape: a parameter an HTTP
binding writes whole under a `Style`, and a header under its `Style`, `simple` where it states none.
A style can write a shape where OpenAPI's style table gives it a text: a list under every style but
`deepObject`, and an object under every style, `deepObject` included at any `Explode`, while
`spaceDelimited` or `pipeDelimited` writes either only where it does not explode; a style the table
does not name writes neither. It gives a value that text only where its items or members are leaves,
types whose value travels as one string, number or boolean, as #1731 settled for the emitters
([values as text](../emitters/refine/text.md#a-value-no-style-arranges)): a list or a tuple of
leaves, a map of leaves, and a model whose flattened members and the values of every catch-all its
flattening reads are leaves, a union of leaves being a leaf. So a string example of a list of
objects or of lists, or of an object holding one, is judged under every style, and under
`deepObject` one of a list is judged and one of an object of leaves is not. Such a string is not
split on the style's delimiter and judged item by item or member by member, which would need each
style's grammar, so neither its items nor its count are judged: `a,b` for a list of integers draws
nothing, nor does the string `"8"` under a `minItems` of two. A number or a bool holds no delimiter,
so it is judged as the list's one item: it is never an object's text, and is a list's text only
where the style writes a list, the list's `minItems` and `maxItems` admit one item, and the element
type holds that item, judged as a value of it is, whole numbers and bounds included, and as its text
where the element settles to text, so `true` is four characters long. An element holding anything
holds every item. Every other number or bool example of a list is judged: a bool for a list of
numbers, a fraction for a list of integers, a number beyond its item's bounds or under a `minItems`
of two, either for a tuple, whose items have no one type, and either for a list of an enum, a
literal or a union, though it may be one of its items, since the check judges no item against such a
type. No other binding writes a value in a style, so a parameter bound by RPC, a message, GraphQL or
OTP, or by no binding, one a binding writes only a member of, and a property, has every scalar
example of a list or an object judged, unless an encoding writes it. A default is a value of the
type, written into code, and so is a scenario's input, which holds each parameter's value whatever
its binding, and a type's own examples, which OpenAPI's Schema Object gives without regard to how a
parameter is written, even where the type is a parameter's own, which is where the OpenAPI compiler
keeps an `example` written on a parameter's schema (#2150): a style excuses none of them. An
example's `Value` does not say which form it is in, so one position's examples may mix the two, and
a scalar written as the list's or the object's data, as OpenAPI 3.2's `dataValue` is, goes unjudged
at such a position wherever it could be that value written out, as would a Schema Object's `example`
a lowering placed on the parameter: telling them apart needs `ir.Example` to record the form, a
decision of its own (#2311).

Not judged: a content's and a message's examples, which are in a media type's format, not the type's
value (whether a JSON content's are held is #1949); a scenario's `Body` and `Output`, which are one
content and one response, so no single type; a variant's and an enum member's examples; the elements
of a list and the members of an object; and `Pattern`, `MultipleOf`, `Precision` and `Scale`.
