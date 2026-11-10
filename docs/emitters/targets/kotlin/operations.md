# Operations in Kotlin

Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
behind the runtime's `Transport` ([runtime
abstraction](../../emit/templates.md#serialization-behind-a-runtime-abstraction)), a Ktor client by
default on both. The runtime follows redirects itself, with the client's own following off: by
default Ktor follows one only for a `GET` or a `HEAD`, so it would return a 307 to a `POST` that
[runtime policy](../../contract/runtime-policy.md#the-canonical-vocabulary) sends again. Java
callers are not a goal of the JVM target at M3: its surface is Kotlin's. Pagination, streaming,
long-running operations and auth lower as the [lowering table](lowering.md) says.

## The request type

Kotlin's request type ([request shaping](../../refine/requests.md)) is a class built like a
[model](models.md).

## The client

The client's settings ([the client](../../operations/client.md)) are setters on the client's
builder. A [client setting](../../operations/client.md#client-settings) is one more,
`accountId("…")`, and the request type's property for it is nullable, null taking the client's.

## Stability

The runtime declares `@AlphaApi` and `@BetaApi`, each a `@RequiresOptIn` annotation, at level
`ERROR` for `alpha` and `WARNING` for `beta`, and each method an operation lowers to, its
`WithResponse` method among them, carries its tier's ([stability](../../operations/stability.md)). A
caller acknowledges an `alpha` call with `@OptIn(AlphaApi::class)`, and a `beta` one compiles with a
warning until it does. The KDoc says the tier and what it means as well, in a paragraph after its
summary, since Swift sees no Kotlin annotation and reads the framework header's comment.
