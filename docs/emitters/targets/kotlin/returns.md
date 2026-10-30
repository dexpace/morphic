# Return shapes in Kotlin

How each [return shape](../../operations/returns.md) lowers in Kotlin.

- **A scalar, a list, a map or a union.** `suspend fun count(): Long`, `Map<String, String>`, the
  sealed interface `Pet`.
- **A file.** `suspend fun download(): RawBody`, over the transport's byte channel.
- **No body.** `Unit`.
- **Declared headers.** `suspend fun getZoneWithResponse(): Response<Zone, GetZoneHeaders>`. The
  `Response`'s `Body` is `Unit` for `void`, and its `Headers` is `Unit` where they declare none.
- **Another declared response.** With no body, `suspend fun deleteMethod(): Deleted?`.

## `OtherResponse`

Kotlin throws an exception that carries an `OtherResponse`, outside [the tree of
failures](errors.md), so `catch (e: ApiException)` still takes failures alone:

```kotlin
class OtherResponse(val statusCode: Int, val rawHeaders: Headers, val rawBody: ByteArray,
                    val bodyError: Throwable?, val body: Any?, val headers: Any?)

class OtherResponseException(val response: OtherResponse) : Exception()
```
