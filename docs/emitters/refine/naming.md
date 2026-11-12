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
                                          // would begin with what its role cannot (below)
    Unwritable(role Role) RuneSet         // the options version's: what an identifier of the role
                                          // cannot hold as it stands, written in the target's form
                                          // for one (below)
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
[collisions](#collisions-and-the-names-a-generation-shipped)). Those a target may meet, neutral
names one scope shares and, in a scope a first target joins or folds, names whose words differ only
where they break, the `names` pass reports to the document's author
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
   reserve: a suffix can make a reserved name, as `com2` is a Windows device name the units of Go
   and Kotlin reserve ([Go](../targets/go/naming.md#reserved-names),
   [Kotlin](../targets/kotlin/naming.md#reserved-names)). The suffix is a digit word, set
   apart from a name ending in a digit as its role sets two digit words apart ([a break letter case
   cannot mark](#a-break-letter-case-cannot-mark-takes-an-underscore)), so a second `V12` is
   `V12_2`. The declaration a visibility shape adds to a type is named after every type's own in
   its scope, in the order of its key ([TypePlan](../plan/types.md#the-declaration-a-shape-takes)),
   so a name the plan derives never takes one the source gives.

In a scope whose names a target writes as paths, a directory's, a file's or a JVM class file's, two
names that fold alike collide too. A file system that ignores case, as macOS's and Windows' do by
default, makes them one path: on macOS, Kotlin 2.4.0 wrote classes `Setup` and `SetUp` of one
package to a single `p/Setup.class`, and the program then failed to load it
(`NoClassDefFoundError: p/Setup (wrong name: p/SetUp)`), as it did classes `Straße` and `Strasse`
(`wrong name: p/Strasse`). Such a scope compares names case-folded, by the fold the engine compares
artifact paths by ([the path fold](../emit/rules.md)), in both rules: a
name held, shipped or reserved there meets every name that folds as it does, and a suffix is chosen
against them so. This holds for a reserved name that is no path too, such as a name Kotlin imports
by default, since one comparison per scope is the simpler rule and errs toward a suffix rather than
a lost file. A target compares each scope one way whatever the spec says, so two symbols rule 1
keeps were apart under the comparison they shipped under. The units' scope is one ([output
units](output-units.md)), and each target names its others
([Go](../targets/go/naming.md#file-names), [Kotlin](../targets/kotlin/naming.md#reserved-names),
[docs](../docs-and-mock.md#routes-and-links)). A target that can write two such names to one path,
as Go writes two declarations to one file, compares that scope's names as they are. The engine's
refusal of two paths that fold alike is the backstop rather than the rule, since a refusal would
fail a run on a valid spec. Such a pair draws `pass/name-collision` where the `names` pass compares
words joined, and elsewhere only where the two share their neutral words
([naming](../../ir/naming.md#names-one-scope-shares)).

A target renders more than a name's words, so two names whose words differ can still render alike in
one scope: an acronym or a source spelling upper-cases more of a word than its first character ([the
source spelling](#the-source-spelling-for-letter-case-only)), upper-casing gives two letters one
capital, as it does `ı` and `i` and each of the 22 letters Go 1.27.3 upper-cases to another's
capital, and a leading word or a character's name adds words (below). The `names` pass compares
words, so it reports such a pair only where it compares them joined. `Ctx.Syms` reports each name it
gives a suffix because another holds its rendered identifier, or in a scope compared folded one that
folds as it does, as `Strasse` does beside `Straße`, where the two names' neutral names differ, or,
for two with no words, their spellings do, as a warning `emitters/naming/collision` naming both
entities, the identifier each rendered to and the one each was given, so a scope of `n` names draws
fewer than `n` ([T-39](../testing/mandatory-tests.md)). So a target keeps names of different words
apart or says where it did not, under every table a configuration gives it. In a scope the pass
compares joined, both can report one pair: the pass's warning says the two may meet on some target,
and this one that they met on this target, and which took the suffix.

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

An acronym or a spelling can make two names meet: `iOSVersion` and `IOSVersion`, of words
`i_os_version` and `ios_version`, both render `IOSVersion` once their spellings are read, and
`user_id` under an acronym `ID` renders `UserID`, as `user_i_d` does with none. `Ctx.Syms` tells
such a pair apart and reports it ([collisions](#collisions-and-the-names-a-generation-shipped)).
Reading a spelling or an acronym only where the identifier, read back by the grammar, gives the
name's words, or writing `_` where it would not, keeps every pair apart unreported, at a cost real
names show: of the 106,743 names with words that the golden spec's source spells in its declared
types and its lists of properties, members, parameters, operations and groups, 70 more having a
source and no words, 6,292 read back as other words once spelled, such as `Secondary DNS (ACL)`'s
`SecondaryDNSACL` and `SSL/TLS Mode Recommendation`'s `SSLTLSModeRecommendation`, while no two names
of one of those lists render alike either way (`cloudflare/api-schemas` @ `bc6fad24`, morphic @
`fa253fe19`). Either rule would rewrite one spelled name in seventeen to part pairs the spec does
not hold. Writing `_` only in a name that met another, `UserI_D` beside `UserID`, was not taken
either: a shipped name keeps its symbol
([collisions](#collisions-and-the-names-a-generation-shipped)), so only a newcomer could take it,
and only where the newcomer is the one misread, which leaves the number for every other pair.
Comparing names in the `names` pass as an acronym table and spellings render them was not taken
either, since a configuration replaces the table and the pass serves every target.

## A name with no words takes its characters' names

A `Source` with no letter or digit in it — an enum value `>=`, an `operationId` `***`, a property
`<` — has neither `Canonical` nor `Hint` ([naming](../../ir/naming.md)), and still needs an
identifier, wherever it is written. `RenderCasing` names it: from each position, the longest
sequence the character-name table holds becomes that entry's words, and a character it does not hold
becomes one word, `u` and its code point in lowercase hex, four digits at least (`u2192` for `→`,
`u00a7` for `§`). The words have no source spelling, so they are cased as a hint's are, and the node
they name carries `Inferred` provenance, `character-names` (INV6). The words read nothing but the
name, as `EnumMember.ID` reads nothing but the value, so reordering an enum renames none of its
members. Two names that meet — `=` and `==` are both `equal` — are `Ctx.Syms`'s, like any other
collision.

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

## A character a target cannot write is written by its name

The word grammar keeps a combining mark in the word of the letter it follows, and takes every
letter and decimal digit Go's Unicode tables hold
([the word grammar](../../ir/naming.md#the-word-grammar)), and no first target can write each of
them in an identifier as it stands. Measured over every one past ASCII, the 145,620 letters, 2,543
marks and 760 digits of Unicode 17.0.0:

- **Go** 1.27.3 takes every letter, and every decimal digit after the first character, and no mark:
  `go/token.IsIdentifier` refuses each of the 2,543 marks, and a field `Café` spelled with U+0301
  COMBINING ACUTE ACCENT fails `go build` (`invalid character U+0301 '́' in identifier`). An import
  path takes no character past ASCII (`malformed import path "goexp/名前": invalid char '名'`), so a
  package, whose name its unit's import path ends in, takes none.
- **Kotlin** 2.4.0's lexer takes no mark either, and of the letters and digits only those of an
  older Unicode: it refuses 20,029 letters, such as U+0870 ARABIC LETTER ALEF WITH ATTACHED FATHA,
  past the BMP U+10570 VITHKUQI CAPITAL LETTER A, and U+A7CB LATIN CAPITAL LETTER RAMS HORN, the
  capital of a `ɤ` it takes, and 140 digits, such as U+10D40 GARAY DIGIT ZERO. In backticks it takes
  each of them, first in a name or later: kotlinc built a property named for every one in both
  places, and a class, a property, a parameter, a method, an enum entry and a nested class so named
  built and ran on the JVM, JS and Wasm, and on the JVM under the serialization plugin, from a
  caller in another module and from Java, their class files named as they are
  (`नाम$$serializer.class`).
- **On Multiplatform** an exported name reaches Objective-C and Swift too, and Kotlin/Native 2.4.0
  writes one in backticks to the framework's header as it is: Swift imported a property named for
  each character the lexer refuses. After an identifier's first character, clang 17.0.0 and Swift
  6.2.3 refuse U+2E2F VERTICAL TILDE alone, and as its first U+2E2F and 225 marks (U+0300 to U+036F,
  U+1DC0 to U+1DFF, U+20D0 to U+20F0, U+FE20 to U+FE2F). A framework whose header holds either
  builds no module, so a Swift file using none of its names fails as well (`could not build
  Objective-C module 'Probe'`).

`RenderCasing` settles an identifier before it writes any character another way. `Case` reads the
grammar's words beside their spellings and the tables, the role composes the identifier, and the
leading word and the underscore below are decided on that identifier, which still holds the name's
own characters. The target's form for a character the role cannot hold as it stands comes last, so
it changes nothing those read:

- **Go** writes each such character where it stands in its word, as the character-name table writes
  a character it does not hold: `u`, in lower case wherever it falls, and its code point in
  lowercase hex, four digits at least (`u0301`, `u10570`). A decomposed `café` is the field
  `Cafeu0301`, `नाम`, whose `ा` is U+093E DEVANAGARI VOWEL SIGN AA, `Field_नu093eम` (below), a
  property of U+0301 and `x`, whose word begins with the mark, `Field_u0301x`, and a group `名前` the
  package `u540du524d`. The node carries `Inferred` provenance, `character-names`, as a name with no
  words does.
- **Kotlin** writes the identifier in backticks and keeps every character: a property `` `café` ``,
  a class `` `नाम` ``, and for a type `ɤx`, cased `Ɤx`, `` `Ɤx` ``. Backticks are no part of a name,
  in a class file or in the header, so the node is marked nothing, a caller writes them where its
  own compiler needs them, and they go around the identifier as the later steps leave it, since a
  reserved name's underscore and a collision's suffix neither add such a character nor remove one. A
  KDoc link writes them too, around each such segment: Dokka 2.3.0 linked ``[`नाम`.`café`]`` and
  left `[नाम]` plain text. What backticks cannot hold is ASCII punctuation, a backtick and a line
  break (kotlinc refused `.`, `;`, `[`, `]`, `/`, `<`, `>`, `:` and `\` on the JVM and a further
  thirteen on Native), none of which an identifier holds: a word is letters, marks and digits, and a
  name with none is named in ASCII (above). A Java caller writes such a name where its JDK's tables
  hold each character, as javac 25 did `नाम` and the decomposed `café`; Java 25's lack 4,709 of the
  code points above. A unit's name, a package segment and a module's directory too, is rendered as
  the target's naming document says ([Kotlin](../targets/kotlin/naming.md#reserved-names)).

Which characters a role cannot hold as they stand is a table of the target's options version, read
from its toolchain and no configuration's to replace: on Go a toolchain that learns a mark would
otherwise rename every identifier holding it, and on Kotlin a later table moves only where backticks
are written ([target options](../contract/options.md)). The letter case `Case` gives a character,
and the categories the tests below read, are tables of that version as well, Unicode 17.0.0's in
version 1, which Go's language floor reads identifiers by
([Go](../targets/go/overview.md#the-language-floor)), and never the `unicode` package's of the Go
release that builds Morphic: `unicode.ToUpper` differs between Go 1.26.3 and Go 1.27.3 at 55 code
points, `ɤ` among them, whose capital the later alone knows, so a type `ɤx` would otherwise be
renamed by the toolchain a release was built with. On Multiplatform a declaration whose exported
name would hold U+2E2F keeps its Kotlin name, which a JVM caller shares, and takes an explicit
Objective-C name instead, in ASCII, each character past it written as Go writes one
([Swift](../targets/kotlin/swift.md)); none begins with a mark, since a name that would takes a
leading word (below). A name that meets another so, as a Go property `cafeu0301` meets the
decomposed `café`, is `Ctx.Syms`'s, and reported
([collisions](#collisions-and-the-names-a-generation-shipped)). T-54 holds every class above.

Composing a decomposed letter with its mark (Unicode NFC) was not taken: it gives `café` its `é`,
leaves every mark with no composed form, as most Indic vowel signs have none, and makes a decomposed
and a composed spelling, which the IR holds as two names, render alike; whether a name composes is
the word grammar's question. Dropping a mark misspells the word and makes `नाम` meet `नम`. Go has no
form that holds a mark, so it alone writes a character by its name. Writing Kotlin's so as well,
`नu093eम`, was not taken: backticks keep the spec's name on every platform the target builds for,
and Swift reads it from the header as it reads any name past ASCII.

## An identifier its role cannot begin with takes a leading word

Canonical words may begin with a digit — a property `1000`, a schema `10_gbps_to_100_gbps`, an enum
value `10002` — and no first target accepts an identifier that does. Go takes one only from a
letter, and exports one only from an upper-case letter, so `1000` cannot be a field at all; Kotlin
takes one only in backticks, where no Java caller can write it (javac 25 refused `E.10002`) and the
Objective-C export a Multiplatform SDK reaches iOS through renames it, a property `1000` to `_1000`.
A word may begin with a combining mark too, which Go, javac and, for the 225 above, clang and Swift
refuse as an identifier's first character. Upper-casing a name's first letter often gives no
upper-case letter either: a letter of no case, as every Han, Kana, Hangul, Arabic and Hebrew letter
is, a modifier letter, a lower-case letter with no upper-case form, such as `ß`, and a Greek letter
with ypogegrammeni, whose upper-case form is titlecase (`ᾳ` is `ᾼ`). With Go 1.27.3, a second
package read none of the fields `名前`, `ʰx`, `ßx`, `ᾼx` and, past the BMP, `𐀀x` (`cannot refer to
unexported field 名前`), and read `Ꭰx`, `𝐀x` and `ℤx`, which begin with upper-case letters.

Where the identifier a role composes would begin with a character the role cannot begin with,
`RenderCasing` puts the policy's leading word for that role before its words, and the node it names
carries `Inferred` provenance, `leading-word` (INV6). That is a digit or a mark in every role, and
on Go, in a role it exports, a type, a field, a method or a constant, any character but an
upper-case letter (category `Lu`). The test reads the identifier once cased and before any character
is written by its name ([above](#a-character-a-target-cannot-write-is-written-by-its-name)), so a
word beginning with a mark takes the word on every target, where Go's would else begin with the `u`
of the mark's name. The test reads the composed identifier, not the name: a Go enum constant begins
with its enum's name ([Go enums](../targets/go/types.md#enums)), so `Code` and `1000` make
`Code1000` and need no word, where a Kotlin entry is the member's name alone and takes one. A hint
is held to the same test. A parameter and a package, which Go leaves unexported, and every Kotlin
role, which takes `名前` as it is, take a word before a digit or a mark alone. Leaving such a Go name
unexported was not taken, since a caller outside its package could neither set nor read it, nor was
writing Kotlin's in backticks as it stands, a name javac cannot write and the Objective-C export
renames or, before a mark, cannot write.

The words are a naming table too, one per role, and like the character names a default of the
target's options version ([target options](../contract/options.md)). Version 1's are `type` for a
type, `field` for a field, `call` for a method, `value` for a constant, `arg` for a parameter and
`pkg` for a package, so the examples render `Field1000`, `Type10GbpsTo100Gbps` and Kotlin's
`VALUE_10002`, and `名前` is Go's field `Field_名前`, a word beginning with a letter of no case taking
`_` before it ([a break](#a-break-letter-case-cannot-mark-takes-an-underscore)). A word that makes
one name meet another — `Field1000` beside a property spelled `field_1000` — is `Ctx.Syms`'s, like
any collision, and reported. T-19 holds the digit test and T-54 the mark's and Go's export.

## A break letter case cannot mark takes an underscore

Camel and Pascal case mark where a word begins by upper-casing its first character, as every role of
the first targets does but two: a package, whose words join with no separator, and a Kotlin
constant, which writes `_` between every two (`VALUE_10002`). Upper-casing leaves some characters as
they are — a digit, a mark, a letter of no case such as `日`, one already upper-case such as `ℤ`, or
a lower-case one with no upper-case form such as `ß` — so a word beginning with one is not marked.
`v_1_2` and `v_12` would both be Go's field `V12`, and an enum `Version`'s members `1_0` and `10`
both its constant `Version10`, though their words differ, and words are what the `names` pass
compares ([names one scope shares](../../ir/naming.md#names-one-scope-shares)).

Where a word after the first begins with such a character, `Case` writes `_` before it, unless one
of the two characters the break falls between is a letter and the other a digit: the grammar breaks
wherever those meet ([the word grammar](../../ir/naming.md#the-word-grammar)), so no word holds one
beside the other and nothing need set them apart. The pairs above render `V1_2` beside `V12` and
`Version1_0` beside `Version10`, `a_日本` and `a日本` render `A_日本` and `A日本`, and
`Type10GbpsTo100Gbps`, `HttpServer2Fa` and `Field1000` take none. The rule reads the words the role
composes, as the leading word's test does, so a Go constant writes one where its enum's name meets
its member's: `V1`'s member `2` is `V1_2`, apart from `V`'s `12`. The underscore adds no word and
reads nothing but the words, so it marks nothing `Inferred`. A path named from the identifier takes
the underscore with it, as Kotlin's class file `V1_2.class` does, while Go names a declaration's
file from its words joined ([Go](../targets/go/naming.md#file-names)): a type `V1_386` is declared
in `v1386.go`, which Go builds on every platform, where `v1_386.go` would build on `386` alone.

So two names whose words differ only where they break render apart in a camel or Pascal role whose
words are each cased by their first character alone, and the pass's exact comparison answers for
them. An acronym or a spelling that upper-cases more of a word can still make two meet, which
`Ctx.Syms` reports ([collisions](#collisions-and-the-names-a-generation-shipped)), and a package,
which joins every word, drops this break with the rest, a pair the pass reports where it compares
words joined ([names one scope shares](../../ir/naming.md#names-one-scope-shares)). A Multiplatform
SDK's Objective-C export writes a Kotlin constant's Swift name in camel case, dropping each `_`, so
a constant whose words hold such a break takes an explicit one, its words cased as a camel role
cases them: `VALUE_1_0` is `value1_0` in Swift, beside `VALUE_10`'s `value10`, and `A_日本`, whose
explicit name is written in ASCII, `a_u65e5u672c`, beside `A日本`'s `a日本`
([Swift](../targets/kotlin/swift.md)).

Leaving the break unmarked was not taken, though it keeps the camel names Go and Kotlin write
without an underscore. The two names would collide, and `Ctx.Syms` would tell them apart by ID
order: `1.0`, whose ID sorts first, would take `Version10` and `10` a suffix, `Version102`, each
constant reading as another value, which no warning of the pass could set right. Go's standard
library writes the underscore between two digit words itself (`sha512.Sum512_256`), and golint's
naming rule, which revive's `var-naming` keeps, accepts it there while reporting one between two
letters (`A_日本`, measured with golangci-lint 2.14.0). Kotlin's conventions write a camel name with
none, so on Kotlin this departs from them; kotlinc 2.4.0 compiles the names, writing `V1_2.class`
beside `V12.class`. The golden spec's unmarked breaks all fall between two digits, as in
`zones_tls_1_2_only` and a SAML `2_0` (`cloudflare/api-schemas` @ `bc6fad24`, morphic @ `ea70eba7`).
Writing `_` before every word with no upper case, `Type_10GbpsTo_100Gbps`, was not taken either: it
marks breaks no name could have joined.

## A name too long for a path is shortened

A spec name has no length limit, and a name becomes a path segment, or part of one, wherever a
target writes it as one: a Go package's directory, a Kotlin class's file, which joins a nested
class's name to its outer's by `$` (`Payment$Card.class`), and the classes kotlinc writes beside
them, such as the continuation of each `suspend` method, named for the method (`Client$getA$1.class`
with Kotlin 2.4.0). No segment past 255 bytes is one every file system writes ([emit
rules](../emit/rules.md)). So `RenderCasing` holds each identifier of a role a target writes into a
path, as its naming document names them ([Go](../targets/go/naming.md#package-directories),
[Kotlin](../targets/kotlin/naming.md#reserved-names)), to a cap, 110 bytes of UTF-8 in version 1, a
default of the target's options version as the naming tables are. A longer identifier keeps the
leading words that fit beside one more, the first eight hex digits of the SHA-256 of the identifier
whole, a first word too long alone being cut at a character's end, and the node carries `Inferred`
provenance, `shortened` (INV6), with a warning `emitters/<target>/name-shortened` naming the entity
and both names. The shortened name reads nothing but the name, so it moves only with it, and two
names that keep the same words still differ in their last. A path the target names apart from any
identifier, as Go names a declaration's file, is shortened as its target says, and the identifier
is not ([Go](../targets/go/naming.md#file-names)).

The cap measures the identifier as it is written, backticks aside, and the hash reads that
identifier whole rather than `Naming.Canonical`, which a name with no words and a type named from a
hint lack, and which holds no word a role or the plan adds: `____________` and `_____________`,
twelve and thirteen `Underscore`s, would hash nothing and meet, and a long type's `Create` and
`UpdatePatch` declarations ([TypePlan](../plan/types.md#the-declaration-a-shape-takes)) their type's
words alone. T-53 holds it.

The cap leaves room for two names joined and the suffixes the Kotlin toolchain adds: two names of
110 bytes make 239 bytes in the serialization plugin's `Payment$Card$$serializer.class`, and a
method of 110 bytes in a client of 110 makes at most 229 in a class kotlinc 2.4.0 writes for its
body, `<client>$<method>$1.class`, over a plain, a paged and a streamed method each handing its
lambdas to the runtime, with the kotlinx.coroutines kotlinc ships. That leaves 28 bytes after a
method's name, which a class kotlinc copies from an inline function's lambda does not fit: it names
one for the function, as in `$$inlined$filterIsInstance$1$1`, 30 bytes, and failed to write it at
the cap (`error while writing`). So a method's body hands each lambda to a function that is not
inline, as the runtime's pager and stream are and as `flow` is, and only the runtime's unit, whose
names are its own, calls an inline function that writes a class. No name of the golden spec
reaches the cap: its longest is 99 bytes as words joined, 106 with the `WithResponse` a method may
add, and its longest union and variant joined by `$` 156, by `jq` over its compiled IR
(`cloudflare/api-schemas` @ `bc6fad24`, morphic @ `fa253fe19`). A suffix the target's own code
makes kotlinc add beyond that room is held by T-53, which lowers the cap in the options version
adding it.

Refusing the run was not taken: a valid spec would generate nothing, where shortening costs only
the readability of a name no reader takes in whole. Nor was a cap on the segment rather than the
name, since a nested name's room would read its outer's length, so renaming a union would shorten
its variants, and an outer near the limit would leave its variants none. Nor was cutting with no
hash: two names that keep the same leading words would meet, and `Ctx.Syms` would part them by ID
order, giving each a name that reads as the other's.
