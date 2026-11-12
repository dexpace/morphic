# Naming and casing

Identifier rendering is owned **exclusively** by refine, through a `naming.Policy` each target
builds from its options. Its casing functions are the target's code. The tables they read — the
acronyms, the character names and the leading words below — are heuristics whose every change
renames identifiers, so each is a default of the target's options version, under the entries a
configuration gives it ([target options](../contract/options.md)), which `DecodeOptions` merges key
by key as it merges the type table ([type table](../contract/type-table.md)). They are not an
`EmitterInput` field, for the reason the type table is not: each is one target's.

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
`naming.Words` splits them on `_`, the one separator its grammar admits
([naming](../../ir/naming.md)), for `Case` to take each beside its source spelling (below).
`RenderCasing` is the only step that cases it; because the IR stores the pre-split neutral words,
there is no lossy runtime re-splitting of a cased string (oagen's `splitWords` counterexample).
`EscapeReservedWords` runs after casing and **never touches `WireName`/`WireID`** — struct tags are
built from the wire channel, so wire correctness never depends on the rendered identifier
([T-4](../testing/mandatory-tests.md)). Collisions are resolved deterministically in `Ctx.Syms`,
which is keyed by IR ID, and against the names the prior generation shipped, so adding one endpoint
never renames an existing method (the oagen collision-cascade counterexample; INV3;
[collisions](#collisions-and-the-names-a-generation-shipped)). Those any target may meet, neutral
names one scope shares, the `names` pass reports to the document's author
([naming](../../ir/naming.md#names-one-scope-shares)), and resolving them stays here. Anonymous
hoisted types get an emitter-chosen name from `Naming.Hint`. Per-service presentation renames
(`Service.Renames`) change how a shape is presented in a service without changing its `TypeID` or
its own `Naming`. Enum members are keyed by `EnumMember.ID`, which is derived from the value a
member holds, so two members whose names render alike are two entries to disambiguate, and
reordering members of distinct values renames none.

## Collisions, and the names a generation shipped

Names that render alike in one scope collide, and `Ctx.Syms` tells them apart. Keying by ID and
being deterministic are both needed and neither is enough: no rule reading only the current document
can tell which of two colliding operations shipped first, so any such rule can rename the incumbent
when a newcomer joins. A renamer that renames every member of a collision from its own identity is
deterministic and order-independent, and still renames `get` to `zonesGetZone` the day
`zonesGetZoneSettings` joins its group, a breaking SDK change. Only the prior generation knows the
incumbent, so the prior manifest ([write /
integrate](../write-integrate.md#what-the-manifest-records)) is an input to the rule. An entity's
prior entry is the one under its ID or, where its ID is new, the one entity entry recording its
correlation key under an ID the document no longer has, where exactly one does and no other new
entity has that key, since a guess between two entries could hand one entity another's name. A
symbol derived from an entity takes the entry derived the same way from its entity's, since it
records the entity's key, so matching it by key alone would find the entity's entry beside its
own. An entity with no correlation key, or whose
entry has none, has no prior entry, so rule 2 names it as on a first generation and nothing that
follows an entity by its prior entry follows it: its ID alone is what #678 shows can name another
entity, and two keyless entities would otherwise be taken for one. Taking two revisions' entities
for one because their keys agree is inferred, not declared, so the engine takes an option turning it
off (INV6). Every generation is then named as a first one, and the engine reports that once as a
warning where a prior manifest is given, since names it shipped may change. A prior manifest whose
`Keys` is not the running build's `irfacts.CorrelationVersion` is read the same way, its keys
coming from another derivation and agreeing with none ([correlation
keys](../../ir/derived-facts.md#correlation-keys)).

1. **A shipped name keeps its symbol.** An entity or symbol whose prior entry has the same
   correlation key, scope and `Base` takes the entry's `Symbol` again, a suffix it was given
   included, unless the scope now reserves it. Two such symbols were apart in that scope in the
   prior generation and stay apart. The scope, not the unit, is what is compared: by default every
   group's client is in the root unit, so an operation moved to another group would otherwise keep
   `get` beside the `get` that group's client already shipped.
2. **Every other member, in ID order**, takes its rendered name where nothing in the scope holds it,
   else that name with the lowest numeric suffix, `2` up, that nothing holds and the scope does not
   reserve: a suffix can make a reserved name, as `com2` is a Windows device name Kotlin's units
   reserve ([Kotlin](../targets/kotlin/naming.md#reserved-names)). The suffix is a digit word, set
   apart from a name ending in a digit as its role sets two digit words apart ([a break letter case
   cannot mark](#a-break-letter-case-cannot-mark-takes-an-underscore)), so a second `V12` is
   `V12_2`.

A name a target resolves in a second scope is an entry of its own, keyed as a symbol derived from
the entity is ([write / integrate](../write-integrate.md)): on Multiplatform, a declaration's
Objective-C name, resolved in the namespace every unit shares ([Swift](../targets/kotlin/swift.md)),
which Swift callers depend on as Kotlin callers depend on its Kotlin name.

Only the choice is pinned, never the name: an entity whose own rendered name changed, as a renamed
`operationId` does, gets a new `Base`, so rule 1 does not hold for it and it is named afresh. An ID
alone pins nothing, since an ID built from an array index can name another entity in the next
revision (#678): the correlation key must agree too. On a first generation, or one run without its
prior manifest, rule 2 decides alone; ID order is independent of declaration order, so the two-order
oracle holds it. What the promise rests on is stated with it: a name stays only while its entity's
correlation key survives the revision, under its ID or in place of it. An ID derived from a pointer
changes when the pointer does ([across revisions](../../ir/identity.md#across-revisions)), and the
key carries it across where the key's own source holds, as an operation's route does when a path
variable is renamed ([correlation keys](../../ir/derived-facts.md#correlation-keys)).

The prior manifest is an input like any other, so a generation is still a pure function of its
inputs ([contract rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)).

## The source spelling, for letter case only

`Case` receives each canonical word beside the spelling the source gave it.
[naming](../../ir/naming.md) derives `Canonical` from `Source` by one grammar, which lives in `ir`
(`ir.CanonicalWords`), and `irverify` holds every `Naming` that has a `Source` to it
(`ir/naming-not-derived`). `naming.Words` takes the spellings from that same grammar. `ir` has one
reading of a name into its words as written, and `CanonicalWords` is their lowercased join; `ir`
gains a sibling of `CanonicalWords` that returns those words, so each spelling aligns with its
canonical word and no emitter carries a second copy of the grammar. A name with no `Source` — one
built from a hint — has no spellings, and neither has a `Naming` the grammar does not derive, which
`Words` leaves unspelled rather than guess. A policy may read a spelling to recover the letter case
of a word whose boundaries the canonical form already fixes: `iOSVersion` is `i_os_version` and
`HTTPServer2FA` is `http_server_2_fa`, and the spellings `i`, `OS`, `Version` and `HTTP`, `Server`,
`2`, `FA` let a policy render `IOSVersion` and `HTTPServer2FA`, where casing the words gives
`IOsVersion` and `HttpServer2Fa` and an acronym table would have to capitalize `os` and `fa` in
every name. It never reads one to find a boundary — the words are fixed before any spelling is read
— so the rule against re-splitting a cased string holds. Ignoring the spellings is conforming, and
the acronym table applies either way.

## A name with no words takes its characters' names

A `Source` with no letter or digit in it — an enum value `>=`, an `operationId` `***`, a property
`<` — has neither `Canonical` nor `Hint` ([naming](../../ir/naming.md)), and still needs an
identifier, wherever it is written. `RenderCasing` names it: from each position, the longest
sequence the character-name table holds becomes that entry's words, and a character it does not hold
becomes one word, `u` and its code point in lowercase hex (`u2192` for `→`). The words have no
source spelling, so they are cased as a hint's are, and the node they name carries `Inferred`
provenance, `character-names` (INV6). The words read nothing but the name, as `EnumMember.ID` reads
nothing but the value, so reordering an enum renames none of its members. Two names that meet — `=`
and `==` are both `equal` — are `Ctx.Syms`'s, like any other collision.

The table is one of the naming tables, so a configuration may replace any entry of it, and a
target's is a default of its options version ([target options](../contract/options.md)). The first
targets share version 1's, in which a sequence is named by what it means where it has one meaning,
and each other character by its own name:

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

Canonical words may begin with a digit — a property `1000`, a schema `10_gbps_to_100_gbps`, an enum
value `10002` — and no first target accepts an identifier that does. Go takes one only from a
letter, and exports one only from an upper-case letter, so `1000` cannot be a field at all; Kotlin
takes one only in backticks, which the Objective-C export a Multiplatform SDK reaches iOS through
cannot carry ([Swift](../targets/kotlin/swift.md)). Where the identifier a role composes would begin
with a digit, `RenderCasing` puts the policy's leading word for that role before its words, and the
node it names carries `Inferred` provenance, `leading-word` (INV6). The test reads the composed
identifier, not the name: a Go enum constant begins with its enum's name ([Go
enums](../targets/go/types.md#enums)), so `Code` and `1000` make `Code1000` and need no word, where
a Kotlin entry is the member's name alone and takes one. A hint is held to the same test.

The words are a naming table too, one per role, and like the character names a default of the
target's options version ([target options](../contract/options.md)). Version 1's are `type` for a
type, `field` for a field, `call` for a method, `value` for a constant, `arg` for a parameter and
`pkg` for a package, so the examples render `Field1000`, `Type10GbpsTo100Gbps` and Kotlin's
`VALUE_10002`. A word that makes one name meet another — `Field1000` beside a property spelled
`field_1000` — is `Ctx.Syms`'s, like any collision.

## A break letter case cannot mark takes an underscore

Camel and Pascal case mark where a word begins by upper-casing its first character, as every role of
the first targets does but two: a package, whose words join with no separator, and a Kotlin
constant, which writes `_` between every two (`VALUE_10002`). Upper-casing leaves some characters as
they are — a digit, a letter of no case such as `日`, one already upper-case such as `ℤ` — so a word
beginning with one is not marked. `v_1_2` and `v_12` would both be Go's field `V12`, and an enum
`Version`'s members `1_0` and `10` both its constant `Version10`, though their words differ, and
words are what the `names` pass compares ([names one scope
shares](../../ir/naming.md#names-one-scope-shares)).

Where a word begins with such a character, `Case` writes `_` before it, unless one of the two
characters the break falls between is a letter and the other a digit: the grammar breaks wherever
those meet ([the word grammar](../../ir/naming.md#the-word-grammar)), so no word holds one beside
the other and nothing need set them apart. The pairs above render `V1_2` beside `V12` and
`Version1_0` beside `Version10`, `a_日本` and `a日本` render `A_日本` and `A日本`, and
`Type10GbpsTo100Gbps`, `HttpServer2Fa` and `Field1000` take none. The rule reads the words the role
composes, as the leading word's test does, so a Go constant writes one where its enum's name meets
its member's: `V1`'s member `2` is `V1_2`, apart from `V`'s `12`. The underscore adds no word and
reads nothing but the words, so it marks nothing `Inferred`. Where a name becomes a path the
underscore goes with it, and Go reads a file name ending in `_386` as a build constraint (#1756).

So two names whose words differ only where they break render apart in a camel or Pascal role whose
words are each cased by their first character alone, and the pass's exact comparison answers for
them. An acronym or a spelling that upper-cases more of a word can still make two meet (#1751), and
a package, which joins every word, drops this break with the rest (#1742).

Leaving the break unmarked was not taken, though it keeps the camel names Go and Kotlin write
without an underscore. The two names would collide, and `Ctx.Syms` would part them by ID order:
`1.0`, whose ID sorts first, would take `Version10` and `10` a suffix, `Version102`, each constant
reading as another value, which no warning of the pass could set right. Go's standard library writes
the underscore between two digit words itself (`sha512.Sum512_256`), and golint's naming rule, which
revive's `var-naming` keeps, accepts it there while reporting one between two letters (`A_日本`,
measured with golangci-lint 2.14.0). Kotlin's conventions write a camel name with none, so on Kotlin
this departs from them; kotlinc 2.4.0 compiles the names, writing `V1_2.class` beside `V12.class`.
The golden spec's unmarked breaks all fall between two digits, as in `zones_tls_1_2_only` and a SAML
`2_0` (`cloudflare/api-schemas` @ `bc6fad24`, morphic @ `ea70eba7`). Writing `_` before every word
with no upper case, `Type_10GbpsTo_100Gbps`, was not taken either: it marks breaks no name could
have joined.
