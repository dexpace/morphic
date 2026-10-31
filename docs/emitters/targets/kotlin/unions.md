# Unions in Kotlin

What holds on every target is in [unions](../../refine/unions.md).

Kotlin declares its `KSerializer` on the sealed interface itself, which a Kotlin interface allows,
so it needs no struct.

Kotlin's variant classes are the same wrapper as [Go's](../go/unions.md), for the same reason
([lowering table](lowering.md)).

## The fallback

The fallback ([the shared rule](../../refine/unions.md#a-value-no-variant-recognizes)) is
`Payment.Unknown`, holding a `JsonElement`.
