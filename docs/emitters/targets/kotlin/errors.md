# Failures in Kotlin

What holds on every target is in [failures](../../operations/errors.md).

## How a failure reaches its caller

Kotlin carries a failure in an exception, the default of `errorStyle` and the only style M3 builds:

```kotlin
class ApiFailure(val statusCode: Int, val rawHeaders: Headers, val rawBody: ByteArray,
                 val bodyError: Throwable?, val fault: Fault, val kind: Kind?,
                 val body: Any?, val headers: Any?)

open class ApiException(val failure: ApiFailure) : Exception()
open class ClientException(failure: ApiFailure) : ApiException(failure)
open class ServerException(failure: ApiFailure) : ApiException(failure)
class NotFoundException(failure: ApiFailure) : ClientException(failure)   // one per kind

inline fun <reified B> ApiException.body(): B? = failure.body as? B
inline fun <reified H> ApiException.headers(): H? = failure.headers as? H
```

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

## A failure's place

A failure's class is its kind's where the kind nests under its `Fault`, else its fault's, else the
root's.
