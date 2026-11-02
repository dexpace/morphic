# Primitives

Part of the [IR design](./README.md). The built-in leaf types and how their value domains nest.

```go
type Primitive struct {
    TypeCommon
    Prim PrimKind
}

type PrimKind string
// bool string bytes
// int8 int16 int32 int64 uint8 uint16 uint32 uint64
// integer  (arbitrary precision — JSON Schema "integer", TypeSpec "integer")
// float32 float64
// float    (arbitrary precision *binary* floating point — TypeSpec "float", supertype of float32/64)
// number   (arbitrary precision — JSON Schema "number", TypeSpec "numeric")
// decimal decimal128
// date time datetime datetime_offset duration
// url uuid
// any      (unknown/JSON any — schemaless)
```

The set is the union of TypeSpec's intrinsic scalars, JSON Schema's types, and Protobuf's needs.
Protobuf's `fixed32`/`sfixed64`/`sint*` are **encodings of** `uint32`/`int64`/…, not distinct
primitives — they lower to `Encoding` ([constraints](./constraints.md)). There is no `void` type:
absence of a body/return is a `nil *TypeRef`. TypeSpec `safeint` is not a primitive either — its
canonical lowering is `Scalar{Base: integer, Constraints: ±(2^53−1)}`.

## Offsets

`time` and `datetime` hold no offset from UTC, and `datetime_offset` holds one. In RFC 3339's terms
`time` is a `partial-time`, `datetime` a `full-date`, `T` and `partial-time`, and `datetime_offset`
a `date-time`. A time of day with an offset, RFC 3339's `full-time`, has no kind of its own: a
compiler keeps it as a `Scalar` over `string` whose `Encoding` names the format as written, as
OpenAPI's `time` lowers ([openapi](../compilers/openapi.md#string-formats)).

A `time_offset` kind beside `datetime_offset` was weighed and not taken. JSON Schema's `time`
format, which OpenAPI and AsyncAPI read, is the only spelling of a time of day with an offset among
the formats the IR is designed against: TypeSpec's `plainTime` and Protobuf's
`google.type.TimeOfDay` hold none, and Smithy, GraphQL and Swagger 2.0 have no time of day. Specs
also write values under `time` with no offset, so a kind promising one would type such a value as
what it is not, and a target decoding the kind would refuse it. A `Scalar` keeps the format and
leaves the reading to the target's [type table](../emitters/contract/type-table.md), whose row for
`{string, "time"}` can choose.

## Containment

`PrimKind.StrictlyWithin` relates two kinds by the values they hold: `a` is
strictly within `b` when every finite value of `a` is a value of `b` and `b` lies above `a` in the
edges below. NaN and the infinities, which only the IEEE-754 kinds hold and no JSON instance
carries, are outside the domain; that is what puts `float` below `number` and `decimal128` below
`decimal`. The edges (`domainParents`) follow TypeSpec's declared numeric hierarchy — each integer
width extends the next (`int8` up to `int64`, `uint8` up to `uint64`), `int64` and `uint64` extend
`integer`, `float32` extends `float64` extends `float`, `decimal128` extends `decimal`, and
`numeric` (`PrimNumber`) is their top — with two of its edges refined to exact containments it does
not declare: `integer` lies within `float` and `float` within `decimal`, since every finite binary
float is a finite decimal. To these they add the cross-signed widths (`uint8` within `int16`,
`uint16` within `int32`, `uint32` within `int64`), the widths a float holds exactly (`int16` and
`uint16` within `float32`, `int32` and `uint32` within `float64`), and `int64` and `uint64` within
`decimal128`. `PrimAny` is the top: every other kind is strictly within it, which is a rule of the
walk and not an edge.

Every edge holds both of a kind's finite values and of the JSON instances its spellings admit.
OpenAPI floats round, so `format: float` admits every finite number in its range; a bounded integer
width sits inside either float width by rounding, while `integer` merely shares the values a float
width holds. That holds of JSON instances and not of values, so `int32`, `uint32`, `int64`, `uint64`
and `integer` against `float32`, and `int64`, `uint64` and `integer` against `float64`, are not
edges. The string-spelled kinds (`date`, `time`, `datetime`, `datetime_offset`, `duration`, `url`,
`uuid`, `bytes`) are **not** within `string` here, although TypeSpec declares `url extends string`:
they are distinct kinds in this domain, and a format's refinement of `string` is a fact about an
OpenAPI spelling, which a compiler holds.
