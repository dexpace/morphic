# Types in Kotlin

What Kotlin decides for the [type table](../../contract/type-table.md), a [declaration over a
root](../../refine/scalars.md) and an [enum](../../refine/enums.md).

## Type-table rows

A Kotlin row is `{Type: "BigDecimal", Codec: "BigDecimalSerializer", Imports: {"BigDecimal":
"java.math.BigDecimal", "BigDecimalSerializer":
"com.example.BigDecimalSerializer"}}`. Each Kotlin target's built-in table is its `Platform`'s ([two
targets, one package](overview.md#two-targets-one-package)), which is where the JVM and
Multiplatform differ.

## What a mapped type must do

A Kotlin type codes through its row's `Codec`, a `KSerializer` the row names unless the type is
`@Serializable`.

## A declaration over a root

Kotlin draws the [shared line](../../refine/scalars.md) at its own types: a value class over
`Boolean`, a number, `String` or `ByteArray`, and a `typealias` over any other root, which leaves
the root's type, and its serializer, in every signature.

## Enums

An enum's forms are in the [lowering table](lowering.md). A Kotlin entry lives in its enum class and
takes the member's name alone (`ACTIVE`, as the naming policy cases it).

`kotlinx.serialization` writes an `enum class` as a string, its entry's `@SerialName`, which is a
member's wire form only for a `string` enum ([a member on the
wire](../../refine/enums.md#a-member-on-the-wire)). Any other closed enum is an `enum class` whose
`KSerializer` is generated: it writes each entry's wire form as the value type's JSON kind, a
boolean for `bool`, a number for `integer`, `number` and `decimal`, and decodes by comparing as JSON
does. An entry of a value type a row maps, such as a `date`, holds the member as that row's type,
`val value: LocalDate`, built through the row's codec when the class loads.
