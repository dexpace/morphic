# Failures in Kotlin

What holds on every target is in [failures](../../operations/errors.md).

## How a failure reaches its caller

Kotlin carries a failure in an exception, the default of `errorStyle` and the only style M3 builds:

```kotlin
class ApiFailure(val statusCode: Int, rawHeaders: Headers, rawBody: ByteArray,
                 val bodyError: Throwable?, val fault: Fault, val kind: Kind?,
                 val body: Any?, val headers: Any?) {
    @Transient private val h = rawHeaders
    @Transient private val b = rawBody
    fun rawHeaders(): Headers = h    // functions, and no properties (below)
    fun rawBody(): ByteArray = b
}

open class ApiException(val failure: ApiFailure) : Exception()
open class ClientException(failure: ApiFailure) : ApiException(failure)
open class ServerException(failure: ApiFailure) : ApiException(failure)
class NotFoundException(failure: ApiFailure) : ClientException(failure)   // one per kind

inline fun <reified B> ApiException.body(): B? = failure.body as? B
inline fun <reified H> ApiException.headers(): H? = failure.headers as? H
```

`ApiFailure` is no data class and an `ApiException` has no message, so the `toString` of each names
its class and no member, and nothing else that reads either finds the headers or the body
([below](#what-a-failure-prints)).

`e.body<T>()` checks a class, and Kotlin erases a generic type's arguments even when `T` is reified,
so `e.body<List<A>>()` also matches a `List<B>` and fails when an element is read: a caller reads a
list or a map body by status. Go's assertion is exact.

A kind nests under its fault because a status states its class, so `catch (e: NotFoundException)`
and `catch (e: ClientException)` both work; a body type could not nest, since one body can arrive
under either fault. `Fault` is an enum, with `Unknown` for what Go leaves empty, so a `when`
over it is exhaustive. An entry whose declared `Fault` contradicts its status takes that fault's
class, and `fault` and `kind` still state each. `ApiFailure` is no `Throwable`, so a later style,
such as a `Result` return, or exceptions typed per body for a spec whose errors are named, is one
more value of `errorStyle`: a template and a signature over the same value, with no plan or IR
change.

## What a failure prints

A `toString` is not all that reads a class: on the JVM an encoder walks a value's fields or its
getters and asks it nothing. So what a runtime class holds raw ([what the runtime holds
raw](../../refine/redaction.md#what-the-runtime-holds-raw)) is no property of it. `ApiFailure`,
`OtherResponse`, `Response`, `EnvelopedResponse`, the runtime's `Headers` and an `InboundRequest`
each keep it in a private `@Transient` field read through a function, are never a `data class`,
whose `toString`, `copy` and `componentN` hand on every member, and declare no `toString` reading
it. Measured with Kotlin 2.4.0, Gson 2.11.0 and Jackson 2.17.2, over `toString`, a string template,
`String.format`, both encoders, Jackson told to read every field, Java serialization and an
exception's stack trace, for a failure, its exception, a response and `Headers`, each alone and in
another class's property: none of 72 renderings held a header's value or the body's bytes, where 10
of 36 did with the same members as properties, and 6 of 18 as private fields without `@Transient`,
which Gson reads. `kotlinx.serialization` encodes none of them, since a class without
`@Serializable` has no serializer to compile against, and from Swift a failure's `description`,
`String(reflecting:)` and `dump` showed none of it. `@Transient` is `kotlin.jvm.Transient`, which
common code may write and the other platforms ignore. T-65 holds this section.

## A transport's failure

An exception the HTTP library raises, in a send or while a request is built for it, never reaches a
caller and is never a `cause`. The runtime throws its own:

```kotlin
class TransportException(val method: String, val url: String,   // the URL as shown
                         val causes: List<String>) : Exception("$method $url")
```

`causes` holds the qualified names of the classes in the library's chain, outermost first: a class's
name is the program's text, and a message is the library's, built from what it was handed. Measured
with Ktor 3.2.3's CIO engine on Kotlin 2.4.0 and a key in the query, the messages of the library's
request, connect and socket timeouts each held the URL with the key (`Request timeout has expired
[url=http://…?limit=5&api_key=…`), and so did each one's stack trace. With none of them set
([timeouts](operations.md#timeouts)), a refused connection raised `java.net.ConnectException:
Connection refused` and the runtime's own bound a `TimeoutCancellationException` saying how long it
waited, neither with the URL. Keeping a cause whose class is the platform's and dropping the
library's was weighed, and the socket timeout is why not: its class is
`java.net.SocketTimeoutException` and its message Ktor's. The cost is the library's stack trace,
which a caller who wants it reads in a `Transport` wrapper
([auth](../../operations/auth.md#where-a-credential-can-be-read)). A caller's own cancelling passes
through as the `CancellationException` it is. T-65 holds this section.

## A failure's place

A failure's class is its kind's where the kind nests under its `Fault`, else its fault's, else the
root's.
