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
distinguishable by JSON kind) → unique-field discrimination → value-based. A compiler may have read
the implicit one in already: a `Discriminator` marked `Inferred`
([unions](../../ir/unions.md#tag-modes)) tags the union, and refine reads it as an explicit one,
since the payload names its variant by it all the same. The decisive divergence: ogen computes this
*while building the Go model*, so a union it cannot discriminate aborts the whole generation.
Because Morphic keeps `Union` lossless in the IR and only an emitter picks a strategy, a refiner
that finds no strategy emits a coded diagnostic and decodes every value into the fallback
below, which keeps its bytes as they arrived — a degrade, never a whole-document failure
([T-7](../testing/mandatory-tests.md)). That graceful-degradation margin is exactly what the ABI
seam buys and the fused-pipeline generator cannot have.

A `oneOf` value is valid in exactly one variant, yet two variants can admit it, since admitting
leaves constraints to validation ([below](#a-value-several-variants-admit)) and a `pattern` may be
all that tells two string variants apart. So the variant recognition names holds the value where it
admits it and meets its constraints. Otherwise each variant is tried in declaration order, as an
inclusive union's are, and the value is held by the first that admits it and meets its constraints,
or, where none meets them, by the first that admits it. Under `TypePlan.Strict` ([plan
policy](../plan/policy.md)) the variants are tried so whatever recognition names, and a value two of
them admit is a coded decode error unless its constraints single one out. A `pattern` a target drops
([constraints](constraints.md)) counts as met there, as its validation counts it.

## A null a variant admits

On an untagged union, `null` is the union's value, never a variant's. A variant admits `null` when
`null` decodes into it: its reference is `Nullable`, or its type, read through decorators and alias
scalars, is the `Any` type or the `any` primitive, or a union one of whose variants admits `null`;
no enum has a `null` member and no `Literal` a `null` value
([enums](../../ir/enums.md#the-empty-enum), [containers](../../ir/containers.md#literals)). `null`
holds nothing a variant could keep apart, so the union holds it itself: every reference to a union
with such a variant lowers as a nullable reference does
([optionality](models.md#optionality--nullability--presence-inv8)), a `null` never reaches a
variant, and a variant admitting nothing else lowers to no member, as a nullable reference to an
enum with no member does. The OpenAPI compiler lifts most branches of `null` alone onto the union's
reference itself ([union-null-variant](../../compilers/openapi.md#union-null-variant)), but not a
`$ref` to a declaration of `null` alone, nor a branch it reads more of than its null, such as an
empty `enum` beside `type: "null"`, which reach an emitter as such variants. The rule reads through
unions:
`Outer: {anyOf: [{$ref: Inner}, {type: integer}]}` admits `null` when `Inner`'s variants are
nullable, and holds it, where a rule read only at `Outer`'s own variants would let a `null` reach
them and match none. The golden spec's single-result envelope declares `result` as
`anyOf: [{type: object, nullable: true}, {type: string, nullable: true}]`: an object, a string or
`null`, the last held by the reference.

Under a tag or a key, `null` is a variant's payload rather than the union's value, as in an
envelope `{kind, value}` whose `value` is `null`: the tag names that variant, which keeps it.

## A value several variants admit

An `anyOf` lets one value satisfy several variants, and what the union holds then turns on whether
it is inclusive in the shape it travels in, which the plan proves once
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
lays it out ([TypePlan](../plan/types.md#variants-that-can-share-a-value)), a polymorphic base
admitting what its own decode order [below](#a-value-no-variant-recognizes) holds: its JSON kind is
one the position's type admits, or, where text carries it, the position's type reads that text;
`null` only where the position admits `null`; each closed model, literal and closed enum holds it,
an enum's values counted as [TypePlan](../plan/types.md#variants-that-can-share-a-value) counts
them, and a tuple with no `Rest` holds no element past its `Elems`; each scalar's codec reads its
text; every model it reaches reads its members as the shape it travels in declares them, a member
that shape leaves out being undeclared, and carries each member its `TypePlan.Required` and
`Undeclared` list that the shape holds and keeps required
([ModelShape](../plan/types.md#modelshape--visibility-projection)), none of a patch shape making
them optional, nor one `ClientOptional` marks; and every tuple it reaches carries the positions its
`MinItems` requires ([tuples](../../ir/containers.md#tuples)). Bar what each scalar's codec reads,
that is a fact of the value and the IR, so targets set the same variants wherever their codecs read
the same texts, and a target's check decides it from the IR rather than from what its JSON library
accepts: json/v2 decodes `null` into a Go `string` as `""` without an error. `Constraints` take no
part, since they are validation's on every decoded value, bar a tuple's `MinItems`, which states its
required positions rather than bounding them. So whether a variant admits a value never depends on a
bound.

Decoding an inclusive union tries each variant whose kinds include the value's JSON kind, or every
variant where text carries the value, in declaration order, and sets every one that admits the
value. If none admits it, the order [below](#a-value-no-variant-recognizes) applies: the fallback on
an open union, a coded decode error on a closed one. Trying every variant is what `anyOf` asks: no
step of the ladder above can tell whether a second variant admits a value too.

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

An inclusive union within the nesting limits ([how deep a value
nests](models.md#how-deep-a-value-nests)) is valid when one set variant is, which is all `anyOf`
asks. A variant can admit a value it does not validate, since admitting leaves bounds to validation:
the golden spec's API Shield token sources are strings under two `pattern`s, so a header's value is
admitted by the cookie variant too and fails only its pattern. Validation reports every set
variant's failures when none passes, and refuses a union with no variant set.

The set variants hold one value, so a member any of them redacts is redacted in all: where an SDK
renders or logs an inclusive union, a position is redacted when a set variant holding it redacts it
([redaction](redaction.md#a-redacted-value)). Rendering one variant alone follows that variant's
own type.

This is not the optional-field merge [the rule above](#variant-identity-and-recognition) forbids.
That merge pools the variants' properties into one bag, losing which variant a member belongs to and
admitting values no variant admits. Here each variant keeps its identity and its whole shape, and
validation still requires one.

The designs this was weighed against:

- **The first variant that admits the value**, the closed sum every untagged union lowered to: it
  misnames a value several variants admit and drops what only the others declare, as the identity
  providers show.
- **A record of which variant pairs overlap**, beside `Inclusive`, so a target could close a sum
  over the variants no other shares a value with: on the golden spec, 425 of the 443 variants of
  inclusive unions overlap another (spec @319dc756, morphic @438bebe2), so it would close almost
  nothing.
- **Builders generated over one parse**, for every variant: decoding is then linear outright, but a
  variant in another output unit, a Go package or a Gradle module, is reachable only through its
  own codec unless that unit exports a helper for it, and each type would carry a second reading of
  its wire form beside its codec. Keying decoded positions keeps one codec per type.
- **One answer for every shape**, which a union a `PATCH` body reaches makes inclusive wherever it
  travels: an operation that first sent a union in a `PATCH` body would turn the closed sum every
  other operation reads into an inclusive one, breaking callers that never touch the patch. Each
  shape is a declaration of its own in the SDK, the docs and the mock alike, so an answer per shape
  still gives every artifact one answer for each declaration.
- **An answer per direction**, one for the shapes an SDK decodes and one for those a mock does: the
  SDK and the mock would then declare one type two ways.
- **Writing a position only where every set variant writes one text**: a codec that normalizes, as
  a timestamp's does, would then refuse to encode a value just decoded.

## A value no variant recognizes

All four wire shapes decode in one order: the variant the tag or the key names, or the one
recognition finds; then, on a discriminated union, for a tag that names none or no tag at all, the
one recognition finds among the variants no tag routes to
([below](#the-tag-a-variants-value-carries)); then `Discriminator.Default`; then, on an `Open` union
([plan policy](../plan/policy.md)), the fallback; and otherwise a coded decode error. A tag that
names a variant whose body fails to decode is an error either way: the variant is known, and the
value does not match it. The fallback is one more variant, `Unknown`, holding the value as the
target's untyped JSON value ([Go](../targets/go/unions.md#the-fallback),
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

A route's tag is the wire string the mapping names, a numeric-looking one included (`"10016"`). A
tag a payload carries as a JSON string is compared with it as that string. One it carries as a
number is compared as JSON compares values ([a member on the wire](enums.md#a-member-on-the-wire)),
with each route whose text spells a number, by value and exactly, read off the two literals' digits
and never through a float: `10016.0` and `1.0016e4` route as `10016` does, as the member's own
integer row reads them ([type
table](../contract/type-table.md#an-integer-row-reads-a-number-by-its-value)). Two routes whose
texts spell one number, `1` and `1.0`, both meet such a tag, which takes the one whose text is its
own literal and, failing that, the first by the mapping's keys in byte order. A boolean is compared
with `true` and `false`. Comparing a number's literal as text would refuse `{"code": 10016.0}` at a
closed union, with no variant for the tag `10016.0`, where the variant pinning `code` to 10016 reads
that member as 10016. T-56 holds it.

## The tag a variant's value carries

A value of a variant has to decode as that variant, so the tag it carries is one the discriminator
routes there. A tag routes to a variant where `Discrim.Mapping` names the variant under it, compared
as [above](#discriminators). The plan writes a union's routes out: the entries of the IR's
`Mapping`, or, where the IR leaves it nil, each variant under the source name of its type, the name
OpenAPI's implicit mapping routes by, which an anonymous variant lacks
([discriminator](../../ir/models.md#discriminator)). A `Mapping` holding an entry holds, beside the
routes declared or inferred, the one the source implies to each variant they leave out, so a variant
it leaves out has none.

A variant's own tags are the tags routing to it that its tag member admits: every declaration of the
member in the variant's flattening, the one it takes and those it overrides, admits the value of the
member's type whose wire text is the tag, a string's characters or a number's literal. So a variant
pinning `code` to the number `10016` owns the tag `10016`, and one whose base lists `http` and `tcp`
owns only the one of them that routes to it. A variant declaring no such member admits every tag. A
variant that is a union, or a model narrowing one, is read through: a value of it is a value of one
of its variants, which owns the tags of the outer variant its own member admits. The member is the
property the discriminator names, or the tuple element at its `Index`. Where the tag sits outside
the variant's value, in an `Envelope` or as the one key of a key-tagged union, no declaration admits
or refuses it, so every tag routing to a variant is its own, a key-tagged variant's being its wire
name. A polymorphic base routes a subtype by its `DiscriminatorValue` as well as by its `Mapping`
([discriminator](../../ir/models.md#discriminator)), and the subtype owns each of those its member
admits.

- **Encoding.** A variant with one own tag writes it at the tag's member, whatever the value holds
  there, and adds the member where the variant declares none: the variant fixes its tag, so a caller
  need not set it, and a value built with the member unset does not go out with a tag nothing
  routes. A variant with several, as an alias in the mapping gives one, writes the tag its value
  holds, and a value holding none of them is a coded encode error, never a pick among them; in an
  envelope, where no member says which, it writes the first in the byte order of their texts, as the
  compiler elects a subtype's `DiscriminatorValue`. A variant with none, one `Default` names or one
  no tag routes to, writes its value as held, and a value whose tag routes to another variant is a
  coded encode error, since decoding would hand it to that one, as is a value of `Default`'s that a
  variant no tag routes to admits. So an encoded value decodes back to the variant that wrote it. A
  factory takes the variant's value and sets nothing.
- **Examples.** A value the plan synthesizes for a variant, and a member `plan.Shown` fills, takes
  the tag by the same rule ([one set of examples](../docs-and-mock.md#one-set-of-examples)), and a
  declared example is checked as decoding reads it, by the variant its tag routes to.

A variant no tag routes to is still a variant some value takes, so decoding finds it by recognition,
as an untagged union finds a variant ([above](#variant-identity-and-recognition)), among the
variants no tag routes to, `Default`'s aside, and before `Default` ([a value no variant
recognizes](#a-value-no-variant-recognizes)). `Default` takes every tag no route names, so tried
first it would take each value meant for such a variant, and tried after it still takes what none of
them admits, recognition holding a value only in a variant that admits it. Without that step no
value would decode as one: on the golden spec (spec @bc6fad24, morphic @fa253fe19), 5 of the 145
unions a property name routes declare no mapping over two inline variants, the `calculations` of the
`workers-observability` query bodies, each variant pinning `operator` to several values, so the IR
routed nothing to either, as it still does with the discriminator-inference policy off. Of the 586
routes of the other 140, 575 lead to a variant that pins the tag, 34 of them as a number, 10 to one
leaving the member a plain string, as `nsc_Interconnect`'s two do, and one to a union. Two variants,
those of `infra_ServiceConfig`, admit both `http` and `tcp` through a base they share, and 5
variants own several tags, 50 between them.

Writing the tag a value holds, as an encoder that only copies the member does, was the rule this
replaces: a `nsc_Interconnect` variant built with `type` unset went out as `"type": ""`, which no
route reads, and one synthesized from the member's own set carried a string routing nowhere, or, for
`infra_ServiceConfig`, the other variant's tag. Electing one tag among several, the first in byte
order as the compiler elects a subtype's `DiscriminatorValue`, would serve an example but not the
encoder of a value whose member says which, which would send a tag the caller did not choose. T-56
holds the rule.

## A discriminated union

`Payment = Card | BankTransfer`, tag `type`.

IR: `Union{ Variants:[Card, BankTransfer], Exclusive:true, WireTagged:true,
Discriminator{PropertyName:"type", Mapping:{"card":Card.ID, "bank":BankTransfer.ID}} }`.

`TypePlan.Union` = `discriminated`, `Discrim` maps `card` and `bank` to the two variants' IDs, and
`Open` says whether a value no variant recognizes is kept ([plan policy](../plan/policy.md)). No
primary-variant collapse — the union stays whole (INV2).

What refine and emit build from it: [Go](../targets/go/unions.md#payment-in-go),
[Kotlin](../targets/kotlin/unions.md).
