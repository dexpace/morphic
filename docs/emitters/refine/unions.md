# Unions and discriminators

Unions are the hardest axis. How a target spells a union is its own ([Go](../targets/go/unions.md),
[Kotlin](../targets/kotlin/unions.md)); what follows holds for every target.

## The wire shapes

`LowerUnions` reads the plan's wire shape (`TypePlan.Union`) and the target's capability. The
tag-mode matrix (each a distinct serializer) is normative ([unions](../../ir/unions.md#tag-modes)):

- `WireTagged=false` → untagged: no tag names the variant, which the value's shape decides (JSON
  oneOf/anyOf). `Exclusive=true` = exactly-one; `false` = one-or-more (anyOf). An untagged union
  carries no `Discriminator`.
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

On an untagged union, `null` is the union's value, never a variant's. A variant admits `null` when
`null` decodes into it: its reference is `Nullable`, or its type, read through decorators and alias
scalars, is the `null` literal, an enum with a `null` member, `any`, or a union one of whose
variants admits `null`. `null` holds nothing a variant could keep apart, so the union holds it
itself: every reference to a union with such a variant lowers as a nullable reference does
([optionality](models.md#optionality--nullability--presence-inv8)), a `null` never reaches a
variant, and a variant admitting nothing else lowers to no member. The rule reads through unions:
`Outer: {anyOf: [{$ref: Inner}, {type: integer}]}` admits `null` when `Inner`'s variants are
nullable, and holds it, where a rule read only at `Outer`'s own variants would let a `null` reach
them and match none. Both spellings the compiler can give a null member (#618) then lower alike. The
golden spec's single-result envelope declares `result` as
`anyOf: [{type: object, nullable: true}, {type: string, nullable: true}]`: an object, a string or
`null`, the last held by the reference.

Under a tag or a key, `null` is a variant's payload rather than the union's value, as in an
envelope `{kind, value}` whose `value` is `null`: the tag names that variant, which keeps it.

## A value several variants admit

An `anyOf` lets one value satisfy several variants, and what the union holds then turns on
`TypePlan.Inclusive`, which the plan proves once
([TypePlan](../plan/types.md#variants-that-can-share-a-value)):

- **Not inclusive.** No value is admitted by two variants, so holding the one that admits it loses
  nothing, and the union lowers as a `oneOf` does: the target's closed sum, recognized as above. The
  variant recognition names must admit the value: where it does not, as a unique-field strategy can
  when the value carries the field and fails the variant elsewhere, or the ladder finds no strategy,
  trying each variant as an inclusive union does recognizes the one, since at most one admits a
  value. So no proven-disjoint union holds a value in a variant that refuses it, nor takes the
  degrade a missing strategy forces. The ladder stays the fast path: one admission check confirms
  what it names, where trying each of a union's variants would check them all.
- **Inclusive.** The union holds every variant that admits the value, each whole and under its own
  name: an optional member per variant, set when that variant admits the value. Holding only the
  first would misname the value and drop what the others declare. An Access identity provider would
  decode as whichever provider is listed first, whatever its `type`, and its own `config` members
  would be lost. A target whose types are structural, as TypeScript's are, holds the value as the
  one object it is, typed by the union, and needs no member per variant.

A tag picks one variant, so a discriminated or key-tagged union is never inclusive, whatever
`Exclusive` says.

A variant admits a value when the value fits it at every position it reaches, read as the JSON wire
lays it out ([TypePlan](../plan/types.md#variants-that-can-share-a-value)), a polymorphic base's
value fitting the subtype its tag names: its JSON kind is one the position's type admits, `null`
only where the position admits `null`; each closed model, closed enum and literal holds it; each
scalar's codec reads its text; and every model it reaches carries each member its
`TypePlan.Required` and `Undeclared` list, as the shape it travels in requires: a `readOnly` member
only of a read shape, a `writeOnly` one only of a create or update shape, and none of a patch shape,
nor one `ClientOptional` marks. Bar what each scalar's codec reads, that is a fact of the value and
the IR, so targets set the same variants wherever their codecs read the same texts, and a target's
check decides it from the IR rather than from what its JSON library accepts: json/v2 decodes `null`
into a Go `string` as `""` without an error. `Constraints` take no part, since they are validation's
on every decoded value. So whether a variant admits a value never depends on a bound.

Decoding an inclusive union tries each variant whose kinds include the value's JSON kind, in
declaration order, and sets every one that admits the value. If none admits it, the order
[below](#a-value-no-variant-recognizes) applies: the fallback on an open union, a coded decode error
on a closed one. Trying every variant is what `anyOf` asks: no step of the ladder above can tell
whether a second variant admits a value too.

Decoding never repeats beneath a position, however inclusive unions nest. Whether a type admits a
position of the value is decided once per type and position, and what the position decodes to at
that type is decoded once and given to every set variant holding it there. Decoding each variant
afresh would decode a position once per path to it: under a union of two models that both hold a
list of the union, the innermost position of a value nested `d` deep would decode `2^d` times.
Decoded once per type, a value costs time in proportion to its size where a target decodes from one
parsed tree, and to its size times the variants tried along its deepest path of inclusive unions
where a target reads the bytes once per variant.

Encoding writes one value that each set variant decodes back to what it holds. Objects merge member
by member, in the order the first set variant writes them, then each later variant's new members,
and arrays element by element, of one length. Where the set variants write a position differently,
the merge writes the first of their texts that each set variant holding the position reads back as
its own value, which a variant decides with its own codec alone: the text, decoded at the position's
type and encoded again, is what the variant itself writes there. A `datetime_offset` variant may
write `2025-01-01T00:00:00Z` for a timestamp that arrived as `2025-01-01T00:00:00.000+00:00`, which
a `string` variant beside it holds as it arrived: the string's text is the one both read back, so it
is the one written. A position with no such text is an encode error, never a silent pick, as a
member two variants hold at different values is. The merged value is then checked against each set
variant's admission, as a decoded one is: a member one variant writes that a closed one beside it
cannot hold, as after a caller adds it to the open one, is an encode error too, since the closed
variant would no longer read the value back. A union with no variant set refuses to encode and
fails validation, and the fallback is never set beside a variant.

An inclusive union is valid when one set variant is, which is all `anyOf` asks. A variant can admit
a value it does not validate, since admitting leaves bounds to validation: the golden spec's API
Shield token sources are strings under two `pattern`s, so a header's value is admitted by the cookie
variant too and fails only its pattern. Validation reports every set variant's failures when none
passes, and refuses a union with no variant set.

The set variants hold one value, so a member any of them redacts is redacted in all: where an SDK
renders or logs an inclusive union, a position is redacted when a set variant holding it marks it
`Secret` or types it `Sensitive`. Rendering one variant alone follows that variant's own type.

This is not the optional-field merge [the rule above](#variant-identity-and-recognition) forbids.
That merge pools the variants' properties into one bag, losing which variant a member belongs to and
admitting values no variant admits. Here each variant keeps its identity and its whole shape, and
validation still requires one.

The designs this was weighed against:

- **The first variant that admits the value**, the closed sum every untagged union lowered to: it
  misnames a value several variants admit and drops what only the others declare, as the identity
  providers show.
- **A record of which variant pairs overlap**, beside `Inclusive`, so a target could close a sum
  over the variants no other shares a value with: on the golden spec, 376 of the 394 variants of
  inclusive unions overlap another (spec @319dc756, morphic @3188471d), so it would close almost
  nothing.
- **Builders generated over one parse**, for every variant: decoding is then linear outright, but a
  variant in another output unit, a Go package or a Gradle module, is reachable only through its
  own codec unless that unit exports a helper for it, and each type would carry a second reading of
  its wire form beside its codec. Keying decoded positions keeps one codec per type.
- **An `Inclusive` per direction**, one for the shapes an SDK decodes and one for those a mock does,
  so a union a PATCH body reaches could stay a closed sum where it is read: the SDK and the mock
  would then declare one type two ways, where the plan gives every artifact one answer, and on the
  golden spec it would close one union, the zone settings.
- **Writing a position only where every set variant writes one text**: a codec that normalizes, as
  a timestamp's does, would then refuse to encode a value just decoded.

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
