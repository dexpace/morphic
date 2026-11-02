# Unions and discriminators

Unions are the hardest axis. How a target spells a union is its own ([Go](../targets/go/unions.md),
[Kotlin](../targets/kotlin/unions.md)); what follows holds for every target.

## The wire shapes

`LowerUnions` reads the plan's wire shape (`TypePlan.Union`) and the target's capability. The
tag-mode matrix (each a distinct serializer) is normative ([unions](../../ir/unions.md#tag-modes)):

- `WireTagged=false` → untagged; variant inferred by validation (JSON oneOf/anyOf).
  `Exclusive=true` = exactly-one; `false` = one-or-more (anyOf). An untagged union carries
  no `Discriminator`.
- `WireTagged=true` + `Discriminator` → the tag travels in the payload, where the discriminator
  locates it: an OpenAPI `discriminator`, GraphQL `__typename`, an Erlang tagged tuple. It sits
  inline among the variant's own members (the property `PropertyName` names, or the tuple element
  at `Index`), or, with `Discriminator.Envelope == "object"`, in a `{kind, value}` wrapper around
  them.
- `WireTagged=true` + nil `Discriminator` → key-tagged: a single-key object keyed by the variant's
  wire name (Smithy unions, proto3-JSON oneof, GraphQL `@oneOf`).

## A polymorphic base

A reference to a model with a `Discriminator`, or an `Abstract` one
([derived-facts](../../ir/derived-facts.md)), holds any of its subtypes, so it lowers as a union of
them, and `Discrim` routes it as a union's. The base's own shape, unless it is `Abstract`, is a
variant too, a declaration of its flattened properties.

## Variant identity and recognition

Variant identity (`Variant.Name`) survives for named accessors / constructors; `WireName`/`WireID`
survive for (de)serialization. **Unions never degrade to optional-field merges** — flattening a
*body* union into method arguments is an opt-in `ShapeHint` ([shaping
hints](../contract/shaping-hints.md)), never a silent loss (the oagen counterexample, prior-art §1).
A future TS/Python target lowers the *same* facts to a native `A | B | C`; the decision differs, the
fact base is identical. What a union holds when several variants of an `anyOf` admit one value is
[below](#a-value-several-variants-admit).

Within the untagged case, choosing *how* a variant is recognized on decode is refine's job, and
ogen's `schema_gen_sum.go` is the battle-tested enumeration to lift: try, in order, explicit
discriminator → implicit discriminator (variant const/name) → JSON-type discrimination (variants
distinguishable by JSON kind) → unique-field discrimination → value-based. The decisive divergence:
ogen computes this *while building the Go model*, so a union it cannot discriminate aborts the whole
generation. Because Morphic keeps `Union` lossless in the IR and only an emitter picks a strategy, a
refiner that finds no strategy emits a coded diagnostic and decodes every value into the fallback
below, which keeps its bytes as they arrived — a degrade, never a whole-document failure
([T-7](../testing/mandatory-tests.md)). That graceful-degradation margin is exactly what the ABI
seam buys and the fused-pipeline generator cannot have.

## A null a variant admits

A variant whose reference is `Nullable` admits `null`, and `null` holds nothing a variant could keep
apart, so the union holds it itself. Every reference to a union with a nullable variant lowers as a
nullable reference does ([optionality](models.md#optionality--nullability--presence-inv8)), and a
`null` never reaches a variant. The golden spec's single-result envelope declares `result` as
`anyOf: [{type: object, nullable: true}, {type: string, nullable: true}]`: an object, a string or
`null`, the last held by the reference.

## A value several variants admit

An `anyOf` lets one value satisfy several variants, and what the union holds then turns on
`TypePlan.Inclusive`, which the plan proves once
([TypePlan](../plan/types.md#variants-that-can-share-a-value)):

- **Not inclusive.** No value is admitted by two variants, so holding the one that admits it loses
  nothing, and the union lowers as a `oneOf` does: the target's closed sum, recognized as above.
- **Inclusive.** The union holds every variant that admits the value, each whole and under its own
  name: an optional member per variant, set when that variant admits the value. Holding only the
  first would misname the value and drop what the others declare. An Access identity provider would
  decode as whichever provider is listed first, whatever its `type`, and its own `config` members
  would be lost.

A tag picks one variant, so a discriminated or key-tagged union is never inclusive, whatever
`Exclusive` says.

A variant admits a value when the value decodes into it, with each codec, closed model, closed enum
and literal refusing what it refuses, and every model the value reaches carries each member its
`TypePlan.Required` lists. `Constraints` take no part, since they are validation's on every decoded
value. So whether a variant admits a value never depends on a bound.

Decoding an inclusive union tries each variant whose kinds include the value's JSON kind, in
declaration order, and sets every one that admits the value. If none admits it, the order
[below](#a-value-no-variant-recognizes) applies: the fallback on an open union, a coded decode error
on a closed one. Trying every variant is what `anyOf` asks: no step of the ladder above can tell
whether a second variant admits a value too.

Encoding writes the merge of what each set variant encodes. Objects merge member by member, in the
order the first set variant writes them, then each later variant's new members; arrays merge element
by element and must have one length; scalars must be equal. A member or element two variants write
must be the same JSON value, numbers compared by value, and a disagreement is an encode error, never
a silent pick. A union with no variant set refuses to encode and fails validation, and the fallback
is never set beside a variant.

This is not the optional-field merge [the rule above](#variant-identity-and-recognition) forbids.
That merge pools the variants' properties into one bag, losing which variant a member belongs to and
admitting values no variant admits. Here each variant keeps its identity and its whole shape, and
validation still requires one.

## A value no variant recognizes

All four wire shapes decode in one order: the variant the tag or the key names, or the one
recognition finds; then `Discriminator.Default`, for a tag it does not name or no tag at all; then,
on an `Open` union ([plan policy](../plan/policy.md)), the fallback; and otherwise a coded decode
error. A tag that names a variant whose body fails to decode is an error either way: the variant is
known, and the value does not match it. The fallback is one more variant, `Unknown`, holding the
value as the target's untyped JSON value ([Go](../targets/go/unions.md#the-fallback),
[Kotlin](../targets/kotlin/unions.md#the-fallback)). It re-encodes as the same JSON value, its
members in order and each number's text intact. It has no factory, since only a response produces
one, and one a caller sends back goes out as it came. A closed union declares none.

## Discriminators

`LowerDiscriminator` reads a `Discriminator` whose `Mapping` points at **TypeIDs, not names**, so a
presentation rename never breaks the wiring. It emits a tag-dispatched decoder that peeks the
discriminator **wire** key (never a rendered field name), typed factory functions, and an encoder
that writes the tag. `Property` vs `PropertyName` vs `Index` (positional), `Envelope` vs inline, and
the `Default` variant on an absent/unrecognized tag all survive un-lowered; the choice of factory vs
native vs wrapper is the refiner's. A tag that selects no variant takes the order above.

A discriminator's tag is the wire string the mapping names, a numeric-looking one included
(`"10016"`): a target compares it as a string and never parses it as a number.

## A discriminated union

`Payment = Card | BankTransfer`, tag `type`.

IR: `Union{ Variants:[Card, BankTransfer], Exclusive:true, WireTagged:true,
Discriminator{PropertyName:"type", Mapping:{"card":Card.ID, "bank":BankTransfer.ID}} }`.

`TypePlan.Union` = `discriminated`, `Discrim` maps `card` and `bank` to the two variants' IDs, and
`Open` says whether a value no variant recognizes is kept ([plan policy](../plan/policy.md)). No
primary-variant collapse — the union stays whole (INV2).

What refine and emit build from it: [Go](../targets/go/unions.md#payment-in-go),
[Kotlin](../targets/kotlin/unions.md).
