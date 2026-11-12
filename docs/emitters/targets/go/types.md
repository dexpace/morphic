# Types in Go

What Go decides for the [type table](../../contract/type-table.md), a [declaration over a
root](../../refine/scalars.md) and an [enum](../../refine/enums.md).

## Type-table rows

A Go row is `{Type: "decimal.Decimal", Imports: {"decimal": "github.com/shopspring/decimal"}}`,
keyed by the name `Type` gives the package, so a package whose name is not its path's last element
(`go.yaml.in/yaml/v3` is `yaml`) is bound as surely as one whose name is.

## What a mapped type must do

A Go type codes itself, through whichever of the pairs json/v2 calls it declares —
`MarshalJSONTo`/`UnmarshalJSONFrom`, `MarshalJSON`/`UnmarshalJSON` or `MarshalText`/`UnmarshalText`
— because json/v2 calls a type's methods wherever its value sits, a caller's own `json.Marshal`
included, where a codec passed as an option reaches only the values encoded under it. So a Go row's
`Codec` is an options error, and a type without those methods is mapped through a type the user
declares with them.

## A declaration over a root

Go defines a new type, `type Name string`, only where the root's type has no methods of its own — a
predeclared type, a slice, a map — so the new type codes as the root does and can carry a `Validate`
of its own. Over any other root it declares an alias, `type Created = time.Time` or
`type Owner = User`. A defined type has none of its root's methods: json/v2 refuses
`type Created time.Time` outright, writes `type Key uuid.UUID` and `type Blob jsontext.Value` as
base64, and never runs a closed model's strict decoder or an open enum's keeper of unknown values
for one.

A defined type over a sensitive chain, and a sensitive enum's named type ([enums](#enums)), declare
`String`, `GoString`, `Format` and `LogValue`, each showing `[REDACTED]`
([redaction](../../refine/redaction.md#a-redacted-value)), so `fmt` redacts one in a slice or a
caller's own struct too, and `log/slog` when handed one, though not inside a slice, whose elements
its JSON handler writes as JSON. json/v2 reads none of them, and the SDK writes a path segment or
query value from the value, `string(k)`, never through `fmt`, which would send the marker. An alias
can declare no method, so only its holders redact it.

## Integers

json/v2 reads only plain digits into an integer and has no option for more, so the runtime carries a
reader for each fixed-width integer type that decides [by
value](../../contract/type-table.md#an-integer-row-reads-a-number-by-its-value), and every codec the
SDK generates decodes under it. A model's `UnmarshalJSONFrom` decodes its members under the readers
beside the `RejectUnknownMembers` it pins ([`User`](models.md#user-in-go)), joined after the
caller's own unmarshalers as an inclusive union joins its own ([unions](unions.md)), so a caller's
`json.Unmarshal` into a model reads `1.0` too, through a box, a slice, a map and a nested model, and
a caller's unmarshaler for `int64` keeps its precedence. The runtime joins once for a caller's set
and a nested model finds that join again, so a value 50 models deep joins once. A defined type over
an integer, a scalar's or an enum's, declares the pair itself, since a function registered for
`int64` is not called for a type defined over it, and the client decodes a body that is an integer,
or a list or map of them, under the same readers.

A name reaches a reader as a string token, where a value's reader refuses a string. So each reader,
and each defined type's pair, asks the decoder where it stands (`jsontext.Decoder.StackIndex`: in an
object, an even count of tokens read) and reads a name as [a map's
key](../../contract/type-table.md#a-maps-key-is-text) is read; a `bool` key and a defined integer
type's, which json/v2 refuses to write, go out through a writer that asks the encoder the same. With
them `map[int64]V`, `map[bool]V` and a map keyed by an integer enum, a string enum and a date each
wrote and read their names, `{"1.0": …}` read as the key 1 inside a nested map, `{"N": "1"}` was
still refused for an `int64` field, and json/v2 refused `{"1": …, "1e0": …}` as a duplicate name.

On a 242 KB body of 2,000 models holding thirteen integers each, decoding took 2.25 ms with json/v2
alone and 2.97 ms under the readers, with no allocation more; registering an unmarshaler that
matches nothing accounts for 0.29 ms of that (Go 1.27.3, darwin/arm64). Three other placements lost.
Readers passed only where the client decodes leave a caller's own `json.Unmarshal` refusing what the
client reads. A runtime type in the field's place, `runtime.Int64`, changes every caller's field.
And a second struct with such types, converted after decoding, cannot convert a `[]int64` or a
`map[string]int64` without copying it. T-61 holds this section.

## Enums

An enum is a named type with a typed constant per member, over its `ValueType`'s Go row or, where
that row's type has no constants, over the members' wire text (below). A `Flags` enum lowers to a
bitset, open or closed, an open one keeping a bit no member names. Go cannot express other open
enums, so any other enum `TypePlan.Open` marks, every one `Closed=false` declares among them
([enums](../../refine/enums.md)), lowers to that type + its consts + a passthrough that preserves
unknown values, and any other lowers to that type + its consts with strict decode. Either decodes a
value no member holds as its `FallbackMember` where the enum declares one, which is why `OpenEnums`
leaves such a closed enum closed ([plan
policy](../../plan/policy.md#an-enum-keeps-a-value-it-does-not-list)). The two forms are one named
type with the same constants, so opening an enum changes its decoder and takes no declaration from a
caller. An enum with no members is that type with no constant and the strict decoder, which then
refuses every value ([enums](../../refine/enums.md#an-enum-with-no-members)), and its
`MarshalJSONTo` refuses every value too, the zero value among them: a struct requiring one then
fails to encode, where decoding alone refused and `json.Marshal` wrote `{"must":""}`, and an
optional member left unset is still left out (Go 1.27.3).

Go declares constants of boolean, numeric and string types alone, so an enum lowers over its
members' wire forms ([a member on the wire](../../refine/enums.md#a-member-on-the-wire)) where its
`ValueType`'s row is none of them: a `bytes`, `date`, `time`, `datetime`, `datetime_offset`,
`duration`, `uuid`, `decimal`, `decimal128` or `any` enum under the built-in rows, an `integer`,
`float` or `number` one where a row maps them exactly, and any one whose row names a type only a
user's row knows, which [the type table](../../contract/type-table.md) takes to have no constants.
Such an enum is a named type over `string`, each constant the wire text of the first member holding
its value, whose codec writes that text as the value type's JSON kind, a number for a numeric kind,
and decodes by comparing as JSON does, so the constant of a decimal `0.1` is the value a `0.10`
decodes to. A method returns the value as the row's type, decoding its wire form as json/v2 decodes
that type, as [the type table](../../contract/type-table.md) builds any literal of a mapped type:

```go
type Day string                            // date, enum: ["2020-01-01"]

const Day20200101 Day = "2020-01-01"

func (d Day) Value() (runtime.Date, error) // the member as the row's type
```

Two designs lost to it. Package-level variables built through the codec cover every type, but a
`var` can be assigned, and `==` is wrong for `time.Time` and absent for a decimal over `math/big`,
so a strict decoder and a caller's `switch` would each need a comparison of their own. A named type
over the row's type has no constant to give a caller at all.

A Go constant is declared at package level, where every enum's constants meet, so Go names a
member's constant by its enum's identifier, as rendered, followed by the member's name
(`StatusActive`). Members holding one value get constants of that one value, so generated code
names each value once: a `switch` refuses two cases of one constant. A member a row's width cannot
hold has no constant ([a member on the wire](../../refine/enums.md#a-member-on-the-wire)), since Go
refuses a constant past its type's range.
