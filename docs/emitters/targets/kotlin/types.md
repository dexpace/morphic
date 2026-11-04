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
takes the member's name alone (`ACTIVE`, as the naming policy cases it), which need not be the
member's wire text, so a parameter or a header writes an entry's wire form and not its `name` or
`toString()` ([a member on the wire](../../refine/enums.md#a-member-on-the-wire)).

`kotlinx.serialization` writes an `enum class` as a string, its entry's `@SerialName`, which is a
member's wire form only for a `string` enum ([a member on the
wire](../../refine/enums.md#a-member-on-the-wire)). Of two entries sharing a `@SerialName` it
decodes the last, where the first is canonical; it throws on a name no entry has, which a
`FallbackMember` decodes; and it refuses `1.0` as a `Long`, which an `integer` admits. So a closed
enum keeps `@SerialName` only where it is a `string` one holding each value once and declaring no
`FallbackMember`, and every other one but a flags one and an open `string` one, a value class over
`String`, codes through a generated `KSerializer`. It writes each member's wire form, the value
type's JSON kind where it has no `WireName`, and decodes by comparing as JSON does, a value two
members hold as the first. A closed one is an `enum class` whose entry holds the member as the row's
type, `val value: LocalDate`, decoded through the row's codec when first read wherever Kotlin can
write no `const val` of the type, as of a `LocalDate`. A member the codec refuses, such as a leap
second `java.time` cannot hold, then fails where it is read, as Go's `Value` returns its error, and
not wherever the class loads. A member a row's width cannot hold has no entry ([a member on the
wire](../../refine/enums.md#a-member-on-the-wire)), as a `Long` literal past its range does not
compile.

An open one is a value class, which equals as its underlying value does, since Kotlin reserves
`equals` and `hashCode` on a value class, so it holds the member's wire text, `val wire: String`,
wherever the row's type is no `Boolean`, primitive number or `String`, and its `value` is decoded
through the row's codec when read, as a closed entry's is. A value class over the row's type would
tell members apart or merge them unlike JSON: a `BigDecimal` keeps its scale, so `0.1` and `0.10`
would be two, a `ByteArray` equals only itself, and a `Uuid` or a time type reads two texts as one
value, a `uuid` in either case or `10:00:00` and `10:00:00.000`, and writes back a spelling of its
own.
