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
`unknown: JsonElement?` on an open one. It is built through a `Builder` and compares by generated
`equals` and `hashCode`, as a
[model](models.md#models-and-request-types-are-classes-not-data-classes) is, so a variant the spec
adds is a new setter rather than a new constructor parameter. The class names its `KSerializer` in
`@Serializable(with = …)`, so any `Json` decodes it, a caller's own included. The serializer reads
the value once as a `JsonElement`, decides which variants admit it by a check generated from the IR,
and decodes each that does from that element through the variant's own serializer, as [Go's
codec](../go/unions.md#an-inclusive-union) does. The outermost inclusive union of a decode derives a
`Json` whose module carries the decode's memo, keyed by the element's identity and the type, as a
contextual entry, and decodes the variants under it; a nested one finds the memo there. So an
element one variant decoded is handed to the next without being walked, and the decode is linear in
the value. Supplying the union through `@Contextual` fields instead would fail under a caller's own
`Json`, whose module holds no such entry. Multiplatform keys the memo with an identity map of its
own, since `IdentityHashMap` is the JVM's. It encodes the merge and runs each set property's
admission check over it. `validate()` passes when a set property's does, reports every set
property's failures when none does, and reports a value with no property set, which no `init` check
does, since a decoded value always has one. `toString` redacts a member any set property holds as
`Secret` or at a `Sensitive` type, in every property it renders.
