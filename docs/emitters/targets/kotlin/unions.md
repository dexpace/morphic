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
final class with a nullable property per variant, named by the variant as its wrapper class is, and
`unknown: JsonElement?` on an open one. Its `KSerializer` reads the value once as a `JsonElement`
and builds the class from it in generated code, as [Go's codec](../go/unions.md#an-inclusive-union)
does, rather than through `Json.decodeFromJsonElement` per variant: it decides which variants admit
each element by the shared rule, and decodes each element once per type, the properties holding it
sharing that value. It encodes the merge. `validate()` passes when a set property's does, reports
every set property's failures when none does, and reports a value with no property set, which no
`init` check does, since a decoded value always has one. `toString` redacts a member any set
property holds as `Secret` or at a `Sensitive` type, in every property it renders.
