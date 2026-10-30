### 4.5 Open enums

`Enum.Closed` tells the refiner whether extensibility must survive. Go cannot express open enums, so
`Closed=false` lowers to a named string type + typed consts + a passthrough that preserves unknown
values and honors `FallbackMember` on decode; `Closed=true` lowers to typed consts with strict
decode; `Flags=true` → bitset; numeric enums render `Value` through the Values channel (§4.10).
Duplicate member values (`allow_alias`) must not be rejected; slice order is the canonical name for
serialization. The open/closed bit, value type, member values, and flags bit all survived from the
IR to this point — Kiota's string-only-closed-enum mistake is designed out because the fact reached
refine.

A Go constant is declared at package level, where every enum's constants meet, so Go names a
member's constant by its enum's identifier, as rendered, followed by the member's name
(`StatusActive`); a Kotlin entry lives in its enum class and takes the member's name alone
(`ACTIVE`, as the naming policy cases it).
