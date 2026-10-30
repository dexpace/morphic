# Kotlin lowering table

Kotlin's lowering of each construct of the [lowering table](../../refine/lowering-table.md). This
table holds for both Kotlin targets, and "KMP:" marks where Multiplatform differs from the JVM
([Multiplatform common code](multiplatform.md)).

| IR construct | Kotlin (JVM and KMP) |
|---|---|
| fixed-width `Primitive` (`int8`…`uint64`, `float32`, `float64`) | `Byte`…`Long`, `UByte`…`ULong`, `Float`, `Double` |
| unbounded `Primitive` (`integer`, `number`, `float`) | `Long`, `Double` by the same rows; exact where a row maps them so: JVM `BigInteger`/`BigDecimal`, KMP the runtime `Decimal` |
| `decimal`, `decimal128` | JVM `BigDecimal`; KMP the runtime `Decimal` |
| `bool`, `string`, `bytes`, `url` | `Boolean`, `String`, `ByteArray` through the runtime's base64 serializer, `String` |
| `uuid` | the standard library's `kotlin.uuid.Uuid` on both ([the language floor](overview.md#the-language-floor)) |
| `date`, `time`, `datetime`, `datetime_offset`, `duration` | JVM `LocalDate`, `LocalTime`, `LocalDateTime`, `OffsetDateTime`; KMP `kotlinx-datetime`'s `LocalDate`, `LocalTime`, `LocalDateTime`, the runtime `OffsetDateTime` ([Multiplatform](multiplatform.md)); a runtime ISO 8601 `Duration` on both |
| `any`, `Any` | `JsonElement` |
| nil `*TypeRef` | `Unit` |
| `Scalar{Base}` chain | `@JvmInline value class` over a `Boolean`, number, `String` or `ByteArray` root, a `typealias` over any other ([declarations over a root](types.md#a-declaration-over-a-root)); nil base → value class over the wire form |
| `Model` (Base/Implements/Mixins) | final class with `Base` and `Mixins` flattened in, built through a builder, never a `data class` ([models](models.md)); `Implements` → interface; discriminated or abstract base → sealed interface |
| anonymous decorator: a `Model` with only a `Base`, or a `Scalar` with neither `Constraints` nor `Encoding`, beside annotations ([decorators](../../refine/decorators.md)) | the same |
| `Model` whose `Base` is no model, not a decorator | the same |
| `AdditionalProps` / `Additional` | catch-all `Map` filled by a serializer that reads the whole object ([unknown members](models.md#unknown-members)); strict `Json`, `@JsonIgnoreUnknownKeys` on each other open model |
| `Union` (four wire shapes, [unions](../../refine/unions.md#the-wire-shapes)) | sealed interface + one wrapper class per variant + a `KSerializer` per wire shape; `Open` → an `Unknown` fallback; KMP: not exhaustive in Swift |
| `Enum` open/closed/flags | closed → `enum class` with `@SerialName` members, `FallbackMember` on decode; open → value class over the value type with a constant per member, which keeps an unknown value; flags → value class over an integer |
| `List`/`MapT`/`Tuple` | `List<T>`/`Map<K, V>`/a class with a positional serializer (nullable past `MinItems`, `Rest` → trailing `List`) |
| `Literal` | `const val`, or an `object` for a non-primitive value |
| `External` | the type-table row for its `Identity`, each platform's own; no row → diagnostic |
| `Visibility` sets | N classes from `ModelShape`; PATCH → optional |
| `Required` ⊥ `Nullable` ⊥ `Presence` | `T` / `T?`, left out when null / `T?`, written as null / runtime `Optional<T?>`, absent or null or a value; collapse → diagnostic |
| `Constraints` | a generated `validate()`, never an `init` check, which would refuse a response; unrepresentable regex → diagnostic |
| `Discriminator{Mapping→ID}` | tag-dispatch `KSerializer` that reads the wire key first |
| `Pagination` (PropPaths) | `Flow<T>`; item unwrap from plan; pacing from policy |
| `Streaming` + `StreamDetail` | a method per shape offered; `Flow` of elements received, a send channel for a client stream; the same sources |
| `LongRunning` | poller with `suspend` `poll()`/`pollUntilDone()`; the same sources |
| `OneWay` | `suspend` function returning `Unit`, no response binding |
| `Channel`/`Message`/`MessageBinding` | the same, over `Flow` |
| `ErrorCase{Fault, Retryable}` | `ApiException` carrying an `ApiFailure`, a runtime tree by fault then kind (`ClientException` → `NotFoundException`), `e.body<T>()` for the body; declared facts win over policy |
| `Response` beside the primary (`OpPlan.Others`) | one read as `Return` returned as the primary; no body → `T?`, or a `Flow` that ends; another body, or a body that does not decode → `OtherResponseException`, outside `ApiException`; a 2xx the document does not list read as `Return` |
| `Auth` OR-of-ANDs | `Authenticator` + per-scheme configuration; priority order |
| `Value{Num BigVal, Ctor, Ref, symbol}` | literals built from the decimal text, never a `Double` literal; `Ctor` → call; `symbol` → string explicitly; a `Default` documented, never sent |
| `Naming{Canonical}` | cased via `naming.Policy`, characters named and leading digits led the same; hard keywords in backticks, other reserved names a trailing underscore; KMP: explicit Objective-C/Swift names where the export would mangle; collisions ID-keyed |
