# Failures in Kotlin

What holds on every target is in [failures](../../operations/errors.md).

## How a failure reaches its caller

Kotlin carries a failure in an exception, the default of `errorStyle` and the only style M3 builds:

```kotlin
class ApiFailure(val statusCode: Int, rawHeaders: Headers, rawBody: ByteArray,
                 val bodyError: DecodeException?, val fault: Fault, val kind: Kind?,
                 val body: Any?, val headers: Any?) {
    @Transient private val h = rawHeaders
    @Transient private val b = rawBody
    fun rawHeaders(): Headers = h    // functions, and no properties (below)
    fun rawBody(): ByteArray = b
}

open class ApiException(val failure: ApiFailure) : Exception("status ${failure.statusCode}")
open class ClientException(failure: ApiFailure) : ApiException(failure)
open class ServerException(failure: ApiFailure) : ApiException(failure)
class NotFoundException(failure: ApiFailure) : ClientException(failure)   // one per kind

fun <B : Any> ApiException.body(type: KClass<B>): B? = type.safeCast(failure.body)
fun <H : Any> ApiException.headers(type: KClass<H>): H? = type.safeCast(failure.headers)
```

`ApiFailure` is no data class and an `ApiException`'s message is its status, so the `toString` of
each names its class and, for the exception, the status, as Go's `Error` does, and nothing else that
reads either finds the headers or the body ([below](#what-a-failure-prints)). With no message, a JVM
log line read `ServerException` alone and Swift's `localizedDescription` named no status
(Kotlin/Native 2.4.0).

`e.body(Problem::class)` checks a class, which holds no type arguments, so `e.body(List::class)`
matches any list: a caller reads a list or a map body by status. Go's assertion is exact. The
accessors take the class rather than a `reified` type parameter of an `inline` function, which is
compiled to no method the ABI dump lists, so changing one would fail no `checkKotlinAbi`: with the
Kotlin Gradle plugin 2.4.0 the dump listed `body(ApiException, KClass)` and not the reified form
([merging and surface extraction](overview.md#merging-and-surface-extraction)). Swift, which sees no
`inline` function, reads `failure.body` and casts it.

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
`OtherResponse`, `Response`, `EnvelopedResponse`, the runtime's `Headers`, `TransportRequest` and
`TransportResponse` ([the client](operations.md#the-client)) and an `InboundRequest` each keep it in
a private `@Transient` field read through a function, are never a `data class`, whose `toString`,
`copy` and `componentN` hand on every member, and declare no `toString` reading it. Measured with
Kotlin 2.4.0, Gson 2.11.0 and Jackson 2.17.2, over `toString`, a string template, `String.format`,
both encoders, Jackson told to read every field, Java serialization and an exception's stack trace,
for a failure, its exception, a response and `Headers`, each alone and in another class's property:
none of 72 renderings held a header's value or the body's bytes, where 10 of 36 did with the same
members as properties, and 6 of 18 as private fields without `@Transient`, which Gson reads.
`kotlinx.serialization` encodes none of them, since a class without `@Serializable` has no
serializer to compile against, and from Swift a failure's `description`, `String(reflecting:)` and
`dump` showed none of it. `@Transient` is `kotlin.jvm.Transient`, which common code may write and
the other platforms ignore. T-65 holds this section.

## A transport's failure

An exception a library raises, in a send, while a request is built for it, in a decoder or in an
`Authenticator`, never reaches a caller and is never a `cause`. The runtime throws its own, whose
`cause` is a chain of `LibraryCause`, one per exception of the library's chain, outermost first, its
first 32 and no more, as Go's `Causes` ([Go](../go/errors.md#a-transports-failure)): on the JVM
`initCause` let two exceptions each be the other's cause, a chain no walk without a bound leaves
([redaction](../../refine/redaction.md#nothing-a-library-wrote-is-shown)):

```kotlin
class TransportException(val method: String, val url: String,   // the URL as shown
                         cause: LibraryCause?) : Exception("$method $url", cause)
class DecodeException(val type: String,                         // the type read or written
                      cause: LibraryCause?) : Exception("body does not decode as $type", cause)
class LibraryCause internal constructor(className: String,     // the message: the class's name
                                        cause: LibraryCause?) : Exception(className, cause)
```

A `LibraryCause`'s message is the qualified name of its exception's class, the program's text, and
on the JVM it carries that exception's stack frames, a frame being a class, a method, a file and a
line: so a stack trace still leads into the library, while a message, built from what the library
was handed, stays out. Common code's `Throwable` declares no way to set frames, so where the
platform gives none, a link carries the runtime's own. Measured with Kotlin 2.4.0 and
kotlinx.serialization 1.8.1, a decoder's chain kept 9 frames of `kotlinx.serialization` and none of
the 28 renderings of the failure, Java serialization, Gson and Jackson among them, held the body.
Measured with Ktor 3.2.3's CIO engine on Kotlin 2.4.0 and a key in the query, the messages of the
library's request, connect and socket timeouts each held the URL with the key (`Request timeout has
expired [url=http://…?limit=5&api_key=…`), and so did each one's stack trace. With none of them set
([timeouts](operations.md#timeouts)), a refused connection raised `java.net.ConnectException:
Connection refused` and the runtime's own bound a `TimeoutCancellationException` saying how long it
waited, neither with the URL. Keeping a cause whose class is the platform's and dropping the
library's was weighed, and the socket timeout is why not: its class is
`java.net.SocketTimeoutException` and its message Ktor's. A `causes` list of class names, which the
chain replaced, kept no frame at all. A caller's own cancelling passes through as the
`CancellationException` it is. An `ApiException` does not survive Java serialization
(`NotSerializableException: ApiFailure`), which is what keeps a raw body out of a serialized one: a
caller who must serialize a failure serializes what it reads of it. T-65 holds this section.

## A failure's place

A failure's class is its kind's where the kind nests under its `Fault`, else its fault's, else the
root's.
