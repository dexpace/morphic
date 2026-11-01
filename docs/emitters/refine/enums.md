# Open enums

`Enum.Closed` tells the refiner whether extensibility must survive, and each target lowers an open,
a closed and a flags enum in forms of its own ([Go](../targets/go/types.md#enums),
[Kotlin](../targets/kotlin/types.md#enums)). Numeric enums render `Value` through the Values channel
([values](values.md)). Duplicate member values (`allow_alias`) must not be rejected; slice order is
the canonical name for serialization. The open/closed bit, value type, member values, and flags bit
all survived from the IR to this point — Kiota's string-only-closed-enum mistake is designed out
because the fact reached refine.
