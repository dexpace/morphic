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

## Enums

An enum is a named type over its `ValueType`'s Go row, with a typed constant per member. Go cannot
express open enums, so `Closed=false` lowers to that type + its consts + a passthrough that
preserves unknown values and honors `FallbackMember` on decode, as does an enum `TypePlan.Open`
marks ([enums](../../refine/enums.md)); `Closed=true` lowers to that type + its consts with strict
decode; `Flags=true` → bitset.

Go declares constants of boolean, numeric and string types alone, so a `ValueType` whose row is none
of them — `date`, `time`, `datetime`, `datetime_offset`, `duration`, `uuid`, `bytes`, `decimal`, and
`integer` or `number` where a row maps them exactly — lowers over the member's wire form ([a member
on the wire](../../refine/enums.md#a-member-on-the-wire)): a named type over `string`, each constant
the member's wire text, whose codec writes that text as the value type's JSON kind, a number for a
`decimal`, and decodes by comparing as JSON does, so the constant of a decimal `0.1` is the value a
`0.10` decodes to. A method returns the value as the row's type, decoding the text through the row's
codec, as [the type table](../../contract/type-table.md) builds any literal of a mapped type:

```go
type Day string                          // date, enum: ["2020-01-01"]

const Day20200101 Day = "2020-01-01"

func (d Day) Value() (sdk.Date, error)   // the member as the row's type
```

Two designs lost to it. Package-level variables built through the codec cover every type, but a
`var` can be assigned, and `==` is wrong for `time.Time` and absent for a decimal over `math/big`,
so a strict decoder and a caller's `switch` would each need a comparison of their own. A named type
over the row's type has no constant to give a caller at all.

A Go constant is declared at package level, where every enum's constants meet, so Go names a
member's constant by its enum's identifier, as rendered, followed by the member's name
(`StatusActive`).
