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
`unknown: JsonElement?` on an open one. Its `KSerializer` reads the value once as a `JsonElement`,
decides which variants admit it by a check generated from the IR, and decodes each that does from
that element through the variant's own serializer, as [Go's
codec](../go/unions.md#an-inclusive-union) does. It decodes them under a `Json` derived for the one
decode, whose module supplies the serializer of each inclusive union a field holds, marked
`@Contextual`, with a memo keyed by the element's identity and the type: an element one variant
decoded is handed to the next without being walked, so the decode is linear in the value.
Multiplatform keys it with an identity map of its own, since `IdentityHashMap` is the JVM's. It
encodes the merge. `validate()` passes when a set property's does, reports every set property's
failures when none does, and reports a value with no property set, which no `init` check does, since
a decoded value always has one. `toString` redacts a member any set property holds as `Secret` or at
a `Sensitive` type, in every property it renders.
