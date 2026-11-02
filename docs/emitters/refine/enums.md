# Open enums

`Enum.Closed` tells the refiner whether extensibility must survive, and each target lowers an open,
a closed and a flags enum in forms of its own ([Go](../targets/go/types.md#enums),
[Kotlin](../targets/kotlin/types.md#enums)). Numeric enums render `Value` through the Values channel
([values](values.md)). Duplicate member values (`allow_alias`) must not be rejected; slice order is
the canonical name for serialization. The open/closed bit, value type, member values, and flags bit
all survived from the IR to this point — Kiota's string-only-closed-enum mistake is designed out
because the fact reached refine.

An enum `TypePlan.Open` marks ([plan policy](../plan/policy.md#an-enum-a-client-reads-is-open))
lowers to the target's open form whatever `Closed` says, so a response carrying a member a later
version added decodes and keeps its value. Its constants still name only the members the document
lists, which is all `Closed` asks of what the SDK sends.

## A member on the wire

A member's wire form is its `WireName` where it has one, else its `Value` as the JSON kind its
enum's `ValueType` has: a string for a `string`, `date` or `uuid` enum, a number for an `integer`,
`number` or `decimal` one, a boolean for a `bool` one. Every target writes that form and decodes by
comparing a value with the members as JSON compares values, which is what an `enum` keyword
compares, whatever `format` says: numbers by value, so `0.1` and `0.10` are one member, and
everything else by its text. A target whose enum form holds only some value types lowers the others
over the wire form ([Go](../targets/go/types.md#enums), [Kotlin](../targets/kotlin/types.md#enums)),
never over a form that would change the JSON kind, as a `bool` enum written as the string `"true"`
would. On the golden spec (spec @319dc756, morphic @b206dc99) 1,069 enums are `bool`, 35 `integer`
and 348 `number`, beside 6,862 `string` ones.

