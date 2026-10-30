# Naming and casing

Identifier rendering is owned **exclusively** by refine, through a `naming.Policy` each target
builds from its options. Its casing functions are the target's code. The tables they read — the
acronyms, the character names and the leading words below — are heuristics whose every change
renames identifiers, so each is a default of the target's options version, under the entries a
configuration gives it (§2.5), which `DecodeOptions` merges key by key as it merges the type table
(§2.6). They are not an `EmitterInput` field, for the reason the type table is not: each is one
target's.

```go
package naming

type Policy interface {
    Case(words []Word, role Role) string  // Role: TypeName | Field | Method | Const | Param | Package
    Acronyms() AcronymTable               // from the options
    Reserved() ReservedSet                // the language's keywords and predeclared names
    CharacterNames() CharacterNameTable   // from the options: character sequences → words, for a
                                          // name with none
    LeadingWord(role Role) string         // from the options: the word before an identifier that
                                          // would begin with a digit, one per role (below)
}

type Word struct {
    Canonical string // one word of Naming.Canonical: the boundaries, never re-derived
    Source    string // that word as Naming.Source spells it, letter case kept; "" when it has none
}

func Words(n ir.Naming) []Word // the one alignment every target uses
```

The `Ident` on every AST node starts as the neutral `Naming.Canonical` word sequence — a *word
sequence, not an identifier*. `Canonical` holds those words as one lower_snake string, and
`naming.Words` splits them on `_`, the one separator its grammar admits (ir-design §3.2), for `Case`
to take each beside its source spelling (below). `RenderCasing` is the only step that cases it;
because the IR stores the pre-split neutral words, there is no lossy runtime re-splitting of a cased
string (oagen's `splitWords` counterexample). `EscapeReservedWords` runs after casing and **never
touches `WireName`/`WireID`** — struct tags are built from the wire channel, so wire correctness
never depends on the rendered identifier (§13 test T-4). Collisions are resolved deterministically
in `Ctx.Syms`, which is keyed by IR ID: adding one endpoint never renames an existing method (the
oagen collision-cascade counterexample; INV3). Anonymous hoisted types get an emitter-chosen name
from `Naming.Hint`. Per-service presentation renames (`Service.Renames`) change how a shape is
presented in a service without changing its `TypeID` or its own `Naming`. Enum members are keyed by
`EnumMember.ID`, which is derived from the value a member holds, so two members whose names render
alike are two entries to disambiguate, and reordering members of distinct values renames none.

## The source spelling, for letter case only

`Case` receives each canonical word beside the
spelling the source gave it. ir-design §3.2 derives `Canonical` from `Source` by one grammar, which
lives in `ir` (`ir.CanonicalWords`), and `irverify` holds every `Naming` that has a `Source` to it
(`ir/naming-not-derived`). `naming.Words` takes the spellings from that same grammar. `ir` has one
reading of a name into its words as written, and `CanonicalWords` is their lowercased join; `ir`
gains a sibling of `CanonicalWords` that returns those words, so each spelling aligns with its
canonical word and no emitter carries a second copy of the grammar.
A name with no `Source` — one built from a hint — has no spellings, and neither has a `Naming` the
grammar does not derive, which `Words` leaves unspelled rather than guess. A policy may read a
spelling to recover the letter case of a word whose boundaries the canonical form already fixes:
`iOSVersion` is `i_os_version` and `HTTPServer2FA` is `http_server_2_fa`, and the spellings `i`,
`OS`, `Version` and `HTTP`, `Server`, `2`, `FA` let a policy render `IOSVersion` and
`HTTPServer2FA`, where casing the words gives `IOsVersion` and `HttpServer2Fa` and an acronym table
would have to capitalize `os` and `fa` in every name. It never reads one to find a boundary — the
words are fixed before any spelling is read — so the rule against re-splitting a cased string holds.
Ignoring the spellings is conforming, and the acronym table applies either way.

## A name with no words takes its characters' names

A `Source` with no letter or digit in it — an
enum value `>=`, an `operationId` `***`, a property `<` — has neither `Canonical` nor `Hint`
(ir-design §3.2), and still needs an identifier, wherever it is written. `RenderCasing` names it:
from each position, the longest sequence the character-name table holds becomes that entry's words,
and a character it does not hold becomes one word, `u` and its code point in lowercase hex (`u2192`
for `→`). The words have no source spelling, so they are cased as a hint's are, and the node they
name carries `Inferred` provenance, `character-names` (INV6). The words read nothing but the name,
as `EnumMember.ID` reads nothing but the value, so reordering an enum renames none of its members.
Two names that meet — `=` and `==` are both `equal` — are `Ctx.Syms`'s, like any other collision.

The table is one of the naming tables, so a configuration may replace any entry of it, and a
target's is a default of its options version (§2.5). The first targets share version 1's, in which
a sequence is named by what it means where it has one meaning, and each other character by its own
name:

```text
<=  less_than_or_equal      >=  greater_than_or_equal   !=  not_equal       ==  equal
<   less_than               >   greater_than            =   equal           !   exclamation_mark
*   asterisk                +   plus                    -   minus           /   slash
\   backslash               %   percent                 &   ampersand       |   pipe
^   caret                   ~   tilde                   @   at              #   hash
$   dollar                  ?   question_mark           .   period          ,   comma
:   colon                   ;   semicolon               _   underscore      '   apostrophe
"   quotation_mark          `   backtick                (   left_parenthesis
)   right_parenthesis       [   left_bracket            ]   right_bracket
{   left_brace              }   right_brace             U+0020   space
```

## An identifier that would begin with a digit takes a leading word

Canonical words may begin with
a digit — a property `1000`, a schema `10_gbps_to_100_gbps`, an enum value `10002` — and no first
target accepts an identifier that does. Go takes one only from a letter, and exports one only from
an upper-case letter, so `1000` cannot be a field at all; Kotlin takes one only in backticks, which
the Objective-C export a Multiplatform SDK reaches iOS through cannot carry (§8.8). Where the
identifier a role composes would begin with a digit, `RenderCasing` puts the policy's leading word
for that role before its words, and the node it names carries `Inferred` provenance, `leading-word`
(INV6). The test reads the composed identifier, not the name: a Go enum constant begins with its
enum's name (§4.5), so `Code` and `1000` make `Code1000` and need no word, where a Kotlin entry is
the member's name alone and takes one. A hint is held to the same test.

The words are a naming table too, one per role, and like the character names a default of the
target's options version (§2.5). Version 1's are `type` for a type, `field` for a field, `call` for
a method, `value` for a constant, `arg` for a parameter and `pkg` for a package, so the examples
render `Field1000`, `Type10GbpsTo100Gbps` and Kotlin's `VALUE_10002`. A word that makes one name
meet another — `Field1000` beside a property spelled `field_1000` — is `Ctx.Syms`'s, like any
collision.
