# Enums

`TypePlan.Open` tells the refiner whether extensibility must survive, holding every enum
`Enum.Closed` declares open and each the plan's policy opens ([plan
policy](../plan/policy.md#an-enum-a-client-reads-is-open)), and each target lowers an open, a closed
and a flags enum in forms of its own ([Go](../targets/go/types.md#enums),
[Kotlin](../targets/kotlin/types.md#enums)). Numeric enums render `Value` through the Values channel
([values](values.md)). Duplicate member values (`allow_alias`) must not be rejected; slice order is
the canonical name for serialization. The open/closed bit, value type, member values, and flags bit
all survived from the IR to this point — Kiota's string-only-closed-enum mistake is designed out
because the fact reached refine.

An enum `TypePlan.Open` marks ([plan policy](../plan/policy.md#an-enum-a-client-reads-is-open))
lowers to the target's open form whatever `Closed` says, so a response carrying a member a later
version added decodes and keeps its value. Its constants still name only the members the document
lists, which is all `Closed` asks of what the SDK sends. A `Flags` enum keeps its flags form either
way, its value a set of bits, and an open one keeps a bit no member names.

## A member on the wire

A member's wire form is its `WireName` where it has one, else its `Value` as the JSON kind its
enum's `ValueType` has, as [TypePlan](../plan/types.md#variants-that-can-share-a-value) gives each
primitive's: a boolean for `bool`, a number for a numeric kind and a string for any other but `any`,
under which a member keeps its own value's kind, a `bytes` value in the base64 its row's codec
writes. Every emitter writes that form, and an SDK and a mock compare a value with the members, or
with their combinations for a `Flags` enum, as JSON compares values, which is what an `enum` keyword
compares, whatever `format` says: numbers by value, exactly unless a row's width rounds them
(below), so `0.1` and `0.10` are one value, and everything else by its text, so two `uuid` members
differing only in case are two. A value two members hold, as `allow_alias` declares and `0.1` beside
`0.10` is, decodes as the first of them, which slice order makes canonical. Where text carries a
member, in a parameter, a header or a form field, it travels as its wire form's text, a string's
characters or a number's or a boolean's literal, never as a name the target gives the member, and is
read back by the same comparison.

A target whose enum form holds only some value types lowers the others over the wire form
([Go](../targets/go/types.md#enums), [Kotlin](../targets/kotlin/types.md#enums)), never over a form
that changes what JSON compares: a `bool` enum written as the string `"true"`, or a `uuid` enum over
a type that reads one in either case as one value. On the golden spec (spec @319dc756, morphic
@d3ce8e8f) 1,070 enums are `bool`, 35 `integer` and 348 `number`, beside 6,881 `string` ones.

A row that gives an unbounded kind a width, the [type table](../contract/type-table.md)'s heuristic,
keeps an enum over that width, so adding a member never changes the enum's form. A member past the
width's range has no constant, and members apart only beyond its precision hold one value there,
decoding as the first of them. Each member past the range, and each one an earlier member's value
takes over, is reported as a warning `emitters/<target>/unrepresentable-enum-member` at its enum,
naming the member, since a member has no provenance of its own. A configuration that wants every
member maps the kind exactly, which no width bounds.
