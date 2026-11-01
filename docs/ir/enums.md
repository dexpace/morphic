# Enums

Part of the [IR design](./README.md). Enumerations, how a member is identified, and why a count
never decides the kind.

```go
type Enum struct {
    TypeCommon
    ValueType PrimKind      // string | int32 | int64 | float64 …
    Members   []EnumMember
    Closed    bool          // false = open/extensible: unknown values must be representable
    Flags     bool          // bitfield semantics
    FallbackMember string   // wire name of the member to substitute when an unknown value is read
                            // (Avro enum `default` symbol); "" = none
}

type EnumMember struct {
    ID         EnumMemberID // e/<enum's namespace>/<enum's path>/<key>, keyed by the value held,
                            // and /<n> after it on the n-th holder of one value
    Name       Naming
    Value      Value        // typed value, matches ValueType
    WireName   string       // serialized form when it differs from Value (rare)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability // members appear/disappear across versions (TypeSpec @added on EnumMember)
    Examples   []Example
    Unmodeled  Unmodeled
}
```

`Closed` defaults per source semantics: JSON Schema enums are closed; Smithy enums are open;
protobuf enums are open or closed **per file syntax / editions feature** (proto2 closed, proto3
open, editions `enum_type` per enum) — the compiler lowers the *resolved* value. Emitters that
can't express open enums (plain Go consts, TS string literals) lower via their refiners — the
bit must survive to that point (Kiota's string-only closed enums are the counterexample).
Duplicate member values are legal (protobuf `allow_alias`); slice order preserves which name is
canonical for serialization, and the validate pass must not reject them.

## Member identity

A member's `ID` is keyed by the value it holds, never by its name or its position: `a-b` and `a_b`
render from the same words and are still two members, and reordering a list that holds each value
once renames none. The key
is one RFC 6901 token, `<tag>:<payload>`, where the tag names the kind of value — `s` string, `y`
symbol, `n` number, `b` bool, `x` bytes as unpadded URL-safe base64, `z` null — so the string `1`
and the number `1` are two members. A number's payload is its canonical decimal
(`ir.Decimal.String`): the leading significant digit, any others after a point, and the power of ten
that digit carries, so `1500`, `1500.0` and `15e2` are all `1.5e3` and every zero is `0`. Only the
key is canonical: the member's `Value` is unchanged. A list, object, reference or constructor value
has no key, and an enum holding one is not an `Enum`. A value held twice keeps every member: the
first has the bare key, and each repeat appends its occurrence number as one more token
(`…/<key>/2`), as a repeated parameter does ([identity](./identity.md#parameters)), which no key can
be, since every key opens with its tag and a colon. The OpenAPI compiler reports each repeat in the
list as written (`openapi/duplicate-enum-value`), whether the list lowers to an `Enum` or degrades
to a `Union`. Which repeat holds the bare ID follows source order, so a reorder keeps the set of IDs
and may move that one. `pass.Validate` reports an ID repeated within one enum as
`pass/duplicate-enum-member-id`.

## The empty enum

A **closed** Enum with **no members** is the empty value space — it admits its members and has
none — so it is how a compiler states a position that accepts no instance at all (JSON Schema's
`enum: []`) without a bottom `TypeKind`. Emitters render it as their uninhabited type where they
have one (TypeScript `never`); none may widen it to the top type.

## One member

**The member count never decides an enumeration's kind.** An enumeration whose members an `Enum` can
hold is an `Enum`, with one member as with many. One it cannot hold — a list or object member as
above, members of different kinds, or only `null` — lowers to an exclusive `Union` of `Literal`s
whatever its count, with a diagnostic. In particular, one member does not make an enumeration a
`Literal` ([containers](./containers.md)), which is for a source's *constant*: JSON Schema `const`,
a TypeSpec literal type, an Erlang atom. The count is the wrong thing to decide the kind on:

- A member has an identity, a name, docs and a deprecation of its own (`EnumMember`), none of
  which a `Literal` has a field for.
- `Closed` belongs to the enum, and a one-member enum can be open — an `x-extensible-enum`, a
  Smithy enum, a proto3 enum — which no single value can state.
- Adding a member to an enum is ordinary evolution of an API, and it must not change the kind
  of the node, which is what every consumer dispatches on.
- Every other format's one-member enum is an `Enum`, as the memberless one above already is.

So OpenAPI 3.0, which has no `const` and writes a pinned value as a one-member `enum`, lowers it to
an `Enum`. A consumer that reads a pinned value — discriminator inference (#659), a target that
types a tag as a literal type — reads a closed one-member `Enum` the way it reads a `Literal`, and
that reading is its policy rather than a lowering.
