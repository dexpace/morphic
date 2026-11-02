# Go lowering table

Go's normative lowering of each construct of the [lowering table](../../refine/lowering-table.md).

| IR construct | Go |
|---|---|
| fixed-width `Primitive` (`int8`…`uint64`, `float32`, `float64`) | the same-named Go type |
| unbounded `Primitive` (`integer`, `number`, `float`) | `int64`, `float64` by the built-in type-table rows ([type table](../../contract/type-table.md)), marked `Inferred`; exact where a row maps them so: `*big.Int`, the runtime decimal |
| `decimal`, `decimal128` | the runtime decimal type, over `math/big` |
| `bool`, `string`, `bytes`, `url` | `bool`, `string`, `[]byte`, which json/v2 writes as base64, `string` |
| `uuid` | the standard library's `uuid.UUID` |
| `date`, `time`, `datetime`, `datetime_offset`, `duration` | runtime `Date`, `TimeOfDay`, `DateTime`; `time.Time`; a runtime ISO 8601 `Duration` |
| `any`, `Any` | `jsontext.Value`, which keeps the bytes, distinct from a diagnosed unknown |
| nil `*TypeRef` | void |
| `Scalar{Base}` chain | over the chain's root (`TypePlan.Scalar`), typed by the type table's row for the root and the chain's encoding ([type table](../../contract/type-table.md)): a defined type with a `Validate` for merged constraints where the root's type has no methods (a predeclared type, a slice, a map), an alias (`type X = Root`) over any other, its constraints checked where it is used ([declarations over a root](types.md#a-declaration-over-a-root)); nil base → newtype |
| `Model` (Base/Implements/Mixins) | struct with `Base` and `Mixins` flattened in; `Implements` → interfaces; a discriminated or `Abstract` base → a struct holding a sealed interface of its subtypes ([unions](unions.md)); `Positional` → index-ordered struct |
| anonymous decorator: a `Model` with only a `Base`, or a `Scalar` with neither `Constraints` nor `Encoding`, beside annotations ([decorators](../../refine/decorators.md)) | no declaration: each reference lowered as one to the first type along its `Base` that is not a decorator, its docs, deprecation and examples folded onto the carrier |
| `Model` whose `Base` is no model, not a decorator | only a `Base` that may be an object, a conjunct writing what only a `Model` holds, or conjuncts no value satisfies, since any other `allOf` over no object is no `Model` ([models](../../../ir/models.md#an-allof-no-object-satisfies)): with no property, catch-all or discriminator of its own, lowered as the `Scalar` row lowers an alias over that base, its constraints, docs and examples binding the position ([scalars](../../../ir/scalars.md)); a union base beside properties distributed over the variants as [models](../../../ir/models.md#composition-beside-a-union) distributes a union co-declared with a composition; anything else a diagnosed degrade |
| `AdditionalProps` / `Additional` | catch-all `map` as the embedded fallback; `closed`→`RejectUnknownMembers(true)`, pinned per model ([`User`](models.md#user-in-go)) |
| `Union` (four wire shapes, [unions](../../refine/unions.md#the-wire-shapes)) | a struct holding a sealed interface + marker + a wrapper per variant + a tag-dispatch (de)serializer on the struct; `Open` → an `Unknown` fallback |
| `Enum` open/closed/flags | consts named by enum then member (`StatusActive`, [enums](types.md#enums)): closed→typed consts; open→string+consts+`Unknown`; flags→bitset; `FallbackMember` on decode |
| `List`/`MapT`/`Tuple` | `[]T`/`map[K]V`/positional struct (optional past `MinItems`, `Rest`→trailing slice); `List.Encoding`→serializer mode |
| `Literal` | typed constant (incl. `symbol`) |
| `External` | the type-table row for its `Identity` ([type table](../../contract/type-table.md)): imported library type + dep-manifest; no row → opaque handle, passthrough + diagnostic |
| `Visibility` sets | N structs from `ModelShape`; PATCH → optional |
| `Required` ⊥ `Nullable` ⊥ `Presence` | `T`/`Opt[T]`/`Nil[T]`/`OptNil[T]`/`omitzero`+presence; collapse→diagnostic; a reference on a cycle of references held by value `*T`, or `Opt[*T]` when optional and nullable ([recursive references](models.md#recursive-references)) |
| `Constraints` | field `validate` tags + refine-built `Validate()`; unrepresentable regex→diagnostic |
| `Discriminator{Mapping→ID}` | tag-dispatch `UnmarshalJSONFrom` on the union's struct + factories |
| `Pagination` (PropPaths) | `iter.Seq2[T, error]` auto-pager; item unwrap from plan; pacing from policy |
| `Streaming` + `StreamDetail` | a method per shape offered; `Stream[T].Recv()`/`Send()` over an element of any kind, `string` where frames go unread; a `bytes` side as a file; terminal ends; direction from core, framing from plan |
| `LongRunning` | poller type + `Poll()`/`PollUntilDone()`; result from `ResultPath`; pacing from policy |
| `OneWay` | fire-and-forget method, no response binding |
| `Channel`/`Message`/`MessageBinding` | `MsgView` → channel client + send/receive + reply routing (messaging target) |
| `ErrorCase{Fault, Retryable}` | one runtime `*APIError` holding the status, fault, kind, raw body and typed `Body` and `Headers`; `errors.Is` by fault and kind, `sdk.Body[T]` for the body ([failures](errors.md)); declared facts win over policy |
| `Response` beside the primary (`OpPlan.Others`) | one read as `Return` returned as the primary; no body → a nullable return, or a pager or stream that ends; another body, or a body that does not decode → `*OtherResponse` on the `error` path, with no fault and no retry; a 2xx the document does not list read as `Return` ([return shapes](returns.md)) |
| `Auth` OR-of-ANDs | `Authenticator` + per-scheme options; priority order; a scheme of kind `unresolved` takes the caller's `Authenticator`, and unconfigured with no other option is the `error` return naming the scheme, nothing sent ([auth](../../operations/auth.md#unresolved-schemes)) |
| `Value{Num BigVal, Ctor, Ref, symbol}` | literals via decimal (never float64); `Ctor`→call; `symbol`→string explicitly; a `Default` documented, never written onto a request ([values](../../refine/values.md)) |
| `Naming{Canonical}` | cased via `naming.Policy`; a name with no words named by its characters, and one that would begin with a digit led by its role's word ([naming](../../refine/naming.md)); a name a scope reserves escaped with a trailing underscore; collisions ID-keyed |

## The step list in Go

What each step of the [canonical list](../../refine/steps.md) that states its result neutrally
lowers to in Go.

| Step | Lowers to |
|---|---|
| `ResolveScalarChains` | a defined type over a root with no methods, an alias over any other ([declarations over a root](types.md#a-declaration-over-a-root)), the root typed by the type table ([type table](../../contract/type-table.md)), + merged constraints; nil-base → newtype (opaque-scalar strategy) |
| `LowerContainers` | `[]T` / `map[K]V` / positional struct, its positions past `MinItems` optional and a `Rest` as a trailing slice; a nil `MapT.Key` is `string`, and a non-string key → custom-key map or diagnostic |
| `LowerLiterals` | typed constant / single-value newtype (incl. `symbol`) |
| `LowerModels` | struct over the flattened properties, an `Undeclared` member a required field typed as the catch-all types it, catch-all map as the embedded fallback, unknown members rejected on `closed` ([`User`](models.md#user-in-go)), an `Unflattened` conjunct lowered or degraded with a code |
| `ProjectVisibilityShapes` | `User` / `UserCreate` / `UserPatch` structs |
| `LowerOptionality` | the four+ states → `T` / `*T` / `Opt[T]`/`Nil[T]`/`OptNil[T]` / `omitzero` + presence bit; a reference on a cycle the target holds by value held through a pointer ([recursive references](models.md#recursive-references)) |
| `LowerConstraints` | field-level `validate` tags + a refine-built `Validate()` method structure |
| `LowerUnions` | a struct holding a sealed interface + marker method + a wrapper per variant or subtype + a fallback on an open one |
| `LowerDiscriminator` | tag-dispatched `UnmarshalJSONFrom` on the union's struct + typed factories |
| `LowerOpenEnums` | closed → typed consts; open → `string`+consts+`Unknown(raw)`; flags → bitset |
| `ExtractInterfaces` | Go interfaces / client interfaces for testability |
| `LowerPagination` | auto-pager yielding `iter.Seq2[T, error]`; pacing left to policy |
| `LowerStreaming` | a method per shape the operation offers; `Stream[T]` with `Recv()`/`Send()` over an element of any kind; a `bytes` side lowered as a file; terminal ends |
| `LowerLRO` | poller type + `Poll()/PollUntilDone()` returning the final type |
