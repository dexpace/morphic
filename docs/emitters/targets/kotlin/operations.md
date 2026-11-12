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

## Operations the API sends

A service's receiver ([operations the API sends](../../operations/inbound.md)) is a class `Receiver`
built with a value for each scheme its operations check and `verify: (InboundRequest) -> Boolean`
for the user's check. A parse method takes the runtime's `InboundRequest(method, headers, body:
ByteArray)`, since Kotlin has no request type every server hands over, and returns the request
type, throwing the runtime's `InboundException` on a refused check and a decode failure apart from
it. Each parse method carries `@Throws(InboundException::class)`: without it Kotlin/Native 2.4.0
exports the method with no `error:` parameter, and a refusal terminates a Swift caller's process,
while with it Swift calls `try receiver.parseZoneCreated(request:)` and catches the refusal, as a
Swift caller of the framework did. A response type's `encode()` returns the runtime's
`OutboundResponse(status, headers, body)`, which the user's server writes.

## Stability

The runtime declares `@AlphaApi` and `@BetaApi`, each a `@RequiresOptIn` annotation, at level
`ERROR` for `alpha` and `WARNING` for `beta`, and each method an operation lowers to, its
`WithResponse` method among them, carries its tier's ([stability](../../operations/stability.md)). A
caller acknowledges an `alpha` call with `@OptIn(AlphaApi::class)`, and a `beta` one compiles with a
warning until it does. The KDoc says the tier and what it means as well, in a paragraph after its
summary, since Swift sees no Kotlin annotation and reads the framework header's comment.
