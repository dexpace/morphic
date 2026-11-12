# Operations in Kotlin

Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
behind the runtime's `Transport` ([runtime
abstraction](../../emit/templates.md#serialization-behind-a-runtime-abstraction)), a Ktor client by
default on both. The runtime follows redirects itself, with the client's own following off: by
default Ktor follows one only for a `GET` or a `HEAD`, so it would return a 307 to a `POST` that
[runtime policy](../../contract/runtime-policy.md#the-canonical-vocabulary) sends again. Java
callers are not a goal of the JVM target at M3: its surface is Kotlin's. Pagination, streaming,
long-running operations and auth lower as the [lowering table](lowering.md) says.

## Timeouts

The runtime times each attempt, and each read or write of a body it does not buffer, with
`withTimeout`, and sets none of Ktor's timeouts ([runtime
policy](../../contract/runtime-policy.md#what-a-layer-writes)). Measured with Ktor 3.2.3's CIO
engine on Kotlin 2.4.0: the library's request timeout of 300ms ended a server stream 300ms after its
request, a frame already read, so it cannot stand for `Timeout.Request`, which ends with the
headers; a read of that stream under `withTimeout(300)` failed at 300ms with nothing of the request
in its text, which is `Timeout.Idle`; and each of the library's timeouts wrote the request's URL
into its message ([a transport's failure](errors.md#a-transports-failure)). T-37 holds this section.

## The request type

Kotlin's request type ([request shaping](../../refine/requests.md)) is a class built like a
[model](models.md). An input no style arranges ([values as
text](../../refine/text.md#a-value-no-style-arranges)) is a `String` property, or a
`List<Pair<String, String>>` of names and values where its position carries pairs, which Swift sees
as an array of `KotlinPair` whose `first` and `second` are optionals (Kotlin/Native 2.4.0).

## The client

The client's settings ([the client](../../operations/client.md)) are setters on the client's
builder, `hook { event -> … }` for the hook its events go to among them ([what an event
holds](../../contract/runtime-policy.md#what-an-event-holds)). A [client
setting](../../operations/client.md#client-settings) is one more,
`accountId("…")`, and the request type's property for it is nullable, null taking the client's.

## Operations the API sends

A service's receiver ([operations the API sends](../../operations/inbound.md)) is a class `Receiver`
built with a value for each scheme its operations check, `verify: ((InboundRequest) -> Boolean)?`
for the user's check and `allowUnauthenticated: Boolean`, stating that an operation declaring no
security admits any sender, each unset by default. A parse method takes the runtime's
`InboundRequest(method, target, headers, body: ByteArray, peerCertificates: List<ByteArray>)`, the
target its path and query as received and each certificate its DER bytes, the client's first and
none where the server exposes none, since Kotlin has no request type every server hands over, and a
JVM certificate type would not reach Swift; it returns the request type, throwing the runtime's
`InboundException` on a refused check and a decode failure apart from it. Each parse method carries
`@Throws(InboundException::class)`: without it Kotlin/Native 2.4.0 exports the method with no
`error:` parameter, and a refusal terminates a Swift caller's process, while with it Swift calls
`try receiver.parseZoneCreated(request:)` and catches the refusal, as a Swift caller of the
framework did, building the receiver with every parameter, since Swift sees no default, and the
check as a closure returning `KotlinBoolean`; on the JVM and from Swift, a receiver given no key for
a query `apiKey`, or no setting for an operation declaring no security, refused the request. A
response type's `encode()` returns the runtime's `OutboundResponse(status, headers, body)`, which
the user's server writes. T-71 holds this section.

## Stability

The runtime declares `@AlphaApi` and `@BetaApi`, each a `@RequiresOptIn` annotation, at level
`ERROR` for `alpha` and `WARNING` for `beta`, and each method an operation lowers to, its
`WithResponse` method among them, carries its tier's ([stability](../../operations/stability.md)). A
caller acknowledges an `alpha` call with `@OptIn(AlphaApi::class)`, and a `beta` one compiles with a
warning until it does. The KDoc says the tier and what it means as well, in a paragraph after its
summary, since Swift sees no opt-in annotation and reads the framework header's comment.
