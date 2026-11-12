# Return shapes in Kotlin

How each [return shape](../../operations/returns.md) lowers in Kotlin.

- **A scalar, a list, a map or a union.** `suspend fun count(): Long`, `Map<String, String>`, the
  sealed interface `Pet`.
- **A file.** `suspend fun download(): RawBody`, over the transport's byte channel.
- **No body.** `Unit`.
- **Declared headers.** `suspend fun getZoneWithResponse(): Response<Zone, GetZoneHeaders>`. The
  `Response`'s `Body` is `Unit` for `void`, and its `Headers` is `Unit` where they declare none.
- **An envelope.** `suspend fun getZone(): Zone`, and `suspend fun getZoneWithResponse():
  EnvelopedResponse<Zone, GetZoneHeaders, GetZoneResponse>`, which holds what `Response` does and
  `val envelope`, the body whole and typed: `GetZoneResponse?` where a response the method returns
  has no envelope, such as a `204`, whose `envelope` is then null, and `Any?` only where the
  responses it returns are wrapped in different envelope types.
- **Another declared response.** With no body, `suspend fun deleteMethod(): Deleted?`.

`Response`, `EnvelopedResponse` and `OtherResponse` hold the headers and the body as they arrived
the way a failure does ([what a failure prints](errors.md#what-a-failure-prints)): in private
`@Transient` fields read through `rawHeaders()` and `rawBody()`, in a class that is never a `data
class`. The `toString` of `Response` and of `EnvelopedResponse` shows the status and the body
through the body's own ([redaction](../../refine/redaction.md#a-redacted-value)), and no raw
header, as [Go's](../go/returns.md#what-a-response-prints) do. `OtherResponse` and the failures
keep a class's `toString`, which prints neither, and their exceptions carry no message.

## `OtherResponse`

Kotlin throws an exception that carries an `OtherResponse`, outside [the tree of
failures](errors.md), so `catch (e: ApiException)` still takes failures alone:

```kotlin
class OtherResponse(val statusCode: Int, rawHeaders: Headers, rawBody: ByteArray,
                    val bodyError: Throwable?, val body: Any?, val headers: Any?)
                    // rawHeaders() and rawBody(), as ApiFailure declares them

class OtherResponseException(val response: OtherResponse) : Exception()
```
