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

## Integers

Each built-in integer row names a serializer of the runtime's as its `Codec`, which decides [by
value](../../contract/type-table.md#an-integer-row-reads-a-number-by-its-value). Under a
`JsonDecoder` it reads the literal's text through `decodeJsonElement()`, the one way kotlinx hands
a serializer a number unparsed, and under any other format it reads as the type's own serializer
does. It applies wherever the row types a position, as any row's codec does: a property, a list's
element, a map's value, a nullable and the field of a value class each read a whole number spelled
with a fraction or an exponent under the SDK's `Json`, under a caller's own and from a
`JsonElement`, where the same member without it refuses `5.0`. On a 173 KB body of 2,000 models
holding fourteen integers each, decoding took 0.95 ms with
kotlinx alone and 1.90 ms through the serializer (Kotlin 2.4.0, kotlinx-serialization-json 1.11.0,
JVM 25). kotlinx offers no cheaper seam: its `Long` decoder cannot be asked to try and fall back,
since a refused literal has already been consumed.

## Bytes

A `ByteArray` equals only itself, and no declaration holding one repairs that. A value class over
one compares by identity, and Kotlin reserves `equals` on a value class (`member name 'equals' is
reserved for future releases`, Kotlin 2.4.0). A model that compares the member with `contentEquals`
still compares a list of them, a map's values, a union's variant and a value class over them by
identity. A `data class` copies the reference, so a copy's array is the original's. So the built-in
row for `bytes` is the runtime's `Bytes`: a final class over an array it copied in, with `size`,
`get`, a `toByteArray()` that copies out, `equals` and `hashCode` over the content, and a
`toString` that shows only its size. With it two values holding the same bytes are equal as
themselves, in a value class, in a model's generated `equals`, in a list, as a map's key and as a
union's variant, and hash alike; the Swift export gives the class the same `==` and `hash`, so it
keys a `Dictionary`, where a `KotlinByteArray` compares by identity as it does in Kotlin
(Kotlin/JVM and Kotlin/Native 2.4.0, Swift 6.2).

The copies are the price, one in and one out, on values that arrive as base64 inside a JSON text; a
body that is bytes whole is a file or a stream ([return shapes](returns.md)) and no `bytes`
position, and the raw body a failure holds stays a `ByteArray`, compared by identity as its
exception is. Three alternatives lost. Content equality in each generated member needs a rule for
every position bytes can sit in, and has none for a bare value class: on the golden spec (spec
@bc6fad24, morphic @fa253fe19) `bytes`, or one of the 25 scalars over it, types 24 model properties,
8 lists' elements, 4 maps' values and 2 union variants. A `typealias` over `ByteArray` leaves
identity everywhere. And `kotlinx-io`'s `ByteString` is this type as a dependency every SDK would
take for one class. A configuration that maps `bytes` to `ByteArray` takes its identity with it
([type table](../../contract/type-table.md)). T-71 holds this.

## A declaration over a root

Kotlin draws the [shared line](../../refine/scalars.md) at its own types: a value class over
`Boolean`, a number, `String` or the runtime's `Bytes`, and a `typealias` over any other root,
which leaves
the root's type, and its serializer, in every signature. A value class over a sensitive chain, and a
sensitive enum's class, whose entry's name would spell its member, override `toString` to show
`[REDACTED]` ([redaction](../../refine/redaction.md#a-redacted-value)), while their serializers and
a parameter write the value itself, never `toString`; a `typealias` can declare nothing, so only its
holders redact it.

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
type's JSON kind where it has no `WireName`, and decodes by comparing as JSON does, members holding
one value taking the first's form both ways. A closed one is an `enum class` whose entry holds its
wire text, `val wire: String`, and the member as the row's type, `val value: LocalDate`, decoded
from that text through the row's codec when first read wherever Kotlin can write no `const val` of
the type, as of a `LocalDate`. A member the codec refuses, such as a leap second `java.time` cannot
hold, then fails where it is read, as Go's `Value` returns its error, and not wherever the class
loads. A member a row's width cannot hold has no entry ([a member on the
wire](../../refine/enums.md#a-member-on-the-wire)), as a `Long` literal past its range does not
compile.

An open one is a value class, which equals as its underlying value does, since Kotlin reserves
`equals` and `hashCode` on a value class, so it holds the member's wire text, `val wire: String`,
wherever Kotlin can write no `const val` of the row's type, and its `value` is decoded through the
row's codec when read, as a closed entry's is. A value class over the row's type would tell members
apart or merge them unlike JSON: a `BigDecimal` keeps its scale, so `0.1` and `0.10` would be two,
and a `Uuid` or a time type reads two texts as one value, a `uuid`
in either case or `10:00:00` and `10:00:00.000`, and writes back a spelling of its own.

On Multiplatform an open enum is a final class instead, comparing by generated `equals` and
`hashCode` over what the value class would hold, with a constant per member in its companion,
because the Objective-C export gives a value class no declaration. Measured with Kotlin 2.4.0, a
`value class Status(val wire: String)` was absent from the framework's header, every position of its
type exported as `id`, so a Swift caller passed the string `"active"` where the Kotlin signature
asks a `Status`, and no constant of its companion was reachable. The class exported as `Status`, its
constants through `Status.companion`, two values compared with `==`, and a value the SDK kept was
read back through `wire`. Every enum the policy covers is open ([plan
policy](../../plan/policy.md#an-enum-keeps-a-value-it-does-not-list)), so the value class would
leave a Swift caller no enum type at all.

An enum with no members ([enums](../../refine/enums.md#an-enum-with-no-members)) is an `enum class`
with no entries, which Kotlin compiles and whose serializer, the plugin's or the generated one,
refuses every value: measured with Kotlin 2.4.0 and `kotlinx.serialization` 1.8.0, a nullable
property of one decoded where it was absent and threw on `"x"`.
