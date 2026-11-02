# Operations in Kotlin

Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
behind the runtime's `Transport` ([runtime
abstraction](../../emit/templates.md#serialization-behind-a-runtime-abstraction)), a Ktor client by
default on both. Java callers are not a goal of the JVM target at M3: its surface is Kotlin's.
Pagination, streaming, long-running operations and auth lower as the [lowering table](lowering.md)
says.

## The request type

Kotlin's request type ([request shaping](../../refine/requests.md)) is a class built like a
[model](models.md).

## The client

The client's settings ([the client](../../operations/client.md)) are setters on the client's
builder.

## Stability

The runtime declares `@AlphaApi` and `@BetaApi`, each a `@RequiresOptIn` annotation, at level
`ERROR` for `alpha` and `WARNING` for `beta`, and an operation's method carries its tier's
([stability](../../refine/redaction.md#a-stability-tier)). A caller acknowledges an `alpha` call
with `@OptIn(AlphaApi::class)`, and a `beta` one compiles with a warning until it does. The KDoc
names the tier as well, since Swift sees no Kotlin annotation and reads the framework header's
comment.

