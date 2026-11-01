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

Go cannot express open enums, so `Closed=false` lowers to a named string type + typed consts + a
passthrough that preserves unknown values and honors `FallbackMember` on decode; `Closed=true`
lowers to typed consts with strict decode; `Flags=true` → bitset. A `ValueType` whose Go type has
no constant form, such as `date`, `uuid` or `decimal`, has no Go lowering yet (#1202).

A Go constant is declared at package level, where every enum's constants meet, so Go names a
member's constant by its enum's identifier, as rendered, followed by the member's name
(`StatusActive`).
