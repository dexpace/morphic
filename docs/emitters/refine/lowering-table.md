# The lowering table

The complete emitter acceptance surface is `ir-design.md §4–§9`; this table is its condensed
normative list: every construct a target answers, and the wire truth each lowering must preserve.
Each target's table gives its lowering of every row under the same first column
([Go](../targets/go/lowering.md), [Kotlin](../targets/kotlin/lowering.md)), which `TestEmitterDocs_EveryTargetAnswersEveryConstruct` holds. Every row
must have an emit path or a diagnosed degrade on each target; a missing row is an IR bug (INV9), not
a compiler change.

| IR construct | Wire truth preserved |
|---|---|
| fixed-width `Primitive` (`int8`…`uint64`, `float32`, `float64`) | — |
| unbounded `Primitive` (`integer`, `number`, `float`) | every digit, when exact |
| `decimal`, `decimal128` | every digit |
| `bool`, `string`, `bytes`, `url` | bytes as base64 text, where `kotlinx.serialization`'s own `ByteArray` serializer writes an array of numbers; a URL's text, never re-serialized from a parsed form |
| `uuid` | canonical text |
| `date`, `time`, `datetime`, `datetime_offset`, `duration` | the offset a timestamp was written with; a duration's years and months, which neither language's own duration holds |
| `any`, `Any` | the value, its members in order and each number's text; decoding into Go's `any` would turn each number into a `float64` |
| nil `*TypeRef` | — |
| `Scalar{Base}` chain | encoding triple; the root's codec |
| `Model` (Base/Implements/Mixins) | `WireID` order |
| anonymous decorator: a `Model` with only a `Base`, or a `Scalar` with neither `Constraints` nor `Encoding`, beside annotations (§4.15) | — |
| `Model` whose `Base` is no model, not a decorator | — |
| `AdditionalProps` / `Additional` | pattern channel |
| `Union` (four wire shapes, §4.4) | `WireName`/`WireID` per variant; an unknown variant's value |
| `Enum` open/closed/flags | member `WireName` |
| `List`/`MapT`/`Tuple` | packed/expanded |
| `Literal` | — |
| `External` | — |
| `Visibility` sets | — |
| `Required` ⊥ `Nullable` ⊥ `Presence` | — |
| `Constraints` | numeric bounds as decimal |
| `Discriminator{Mapping→ID}` | discriminator wire key |
| `Pagination` (PropPaths) | path-based item/cursor read |
| `Streaming` + `StreamDetail` | frame boundaries; per-event content-type |
| `LongRunning` | monitor/final-state channel |
| `OneWay` | — |
| `Channel`/`Message`/`MessageBinding` | address/correlation paths |
| `ErrorCase{Fault, Retryable}` | status→kind map; an error's raw body |
| `Response` beside the primary (`OpPlan.Others`) | the status; the body as it arrived |
| `Auth` OR-of-ANDs | — |
| `Value{Num BigVal, Ctor, Ref, symbol}` | — |
| `Naming{Canonical}` | `WireName`/`WireID` never cased |
