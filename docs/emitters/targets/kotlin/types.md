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

A Kotlin entry lives in its enum class and takes the member's name alone (`ACTIVE`, as the naming
policy cases it).
