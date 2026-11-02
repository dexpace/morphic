# Unions in Kotlin

What holds on every target is in [unions](../../refine/unions.md).

Kotlin declares its `KSerializer` on the sealed interface itself, which a Kotlin interface allows,
so it needs no struct.

Kotlin's variant classes are the same wrapper as [Go's](../go/unions.md), for the same reason
([lowering table](lowering.md)).

## The fallback

The fallback ([the shared rule](../../refine/unions.md#a-value-no-variant-recognizes)) is
`Payment.Unknown`, holding a `JsonElement`.

## An inclusive union

An inclusive union ([the shared rule](../../refine/unions.md#a-value-several-variants-admit)) is a
final class with a nullable property per variant, named as the variant's wrapper class is, and
`unknown: JsonElement?` on an open one. Its `KSerializer` reads the value as a `JsonElement`,
decodes it into each variant through `Json.decodeFromJsonElement`, and keeps each that admits it:
the decode succeeds and no required member was absent. It encodes the merge. `validate()` reports a
value with no property set, and no `init` check does, since a decoded value always has one.
