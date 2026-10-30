# Unions in Kotlin

Kotlin declares its `KSerializer` on
the sealed interface itself, which a Kotlin interface allows, so it needs no struct.

Kotlin's variant classes are the wrappers [Go declares](../go/unions.md), for the same reason: a
variant in another unit, or one spelled as a type that declares no method, cannot join a sealed
interface itself.

## The fallback

The fallback (§4.4) is `Payment.Unknown`, holding a `JsonElement`.
