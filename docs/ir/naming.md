# Names

Part of the [IR design](./README.md). The `Naming` a named entity carries: the source's spelling,
the neutral word sequence an emitter renders from, and the aliases a reader may also match.

```go
type Naming struct {
    Source    string   // exactly as written in the spec ("user_id", "$ref name", GraphQL field)
    Canonical string   // IR-normalized identifier in neutral form: lower_snake words, no casing opinions
    Hint      string   // context-derived suggestion where there is no source name to render: an
                       // anonymous type ("connection_domain"), or one named with the empty string
    Aliases   []string // alternate names for schema-resolution matching (Avro aliases). Versionless —
                       // rename *history* tied to version labels lives in Availability.RenamedFrom.
}
```

`Canonical` is a cleaned word sequence, not a cased identifier — emitters apply casing, acronym
policy, and reserved-word escaping. An emitter may read `Source` to recover the letter case of the
words `Canonical` fixes (`iOS` in `iOSVersion`), segmenting it with this document's grammar rather
than one of its own, and never to find where a word ends ([naming](../emitters/refine/naming.md)).
Anonymous (hoisted) types have empty `Source` and a `Hint`; whether an emitter inlines them or names
them is its choice.

**`Hint` is the same word sequence in the same grammar.** It is not a presentation affordance
sitting outside the neutrality rule: a hoisted type has no other name, so `Hint` is exactly what an
emitter renders that type's identifier from, which is the job `Canonical` does for a declared one.
That matters because a hint is nearly always derived from something a source *did* spell — a
component key, an `operationId`, a header name, a `$ref` target — so a compiler that passes the
context through puts precisely the casing and punctuation this document rules out into the one
channel that carries a name and no spelling (GitHub #54). What `Hint` is not held to is the
recomputation below: it has no `Source` beside it to be derived from, which is why it exists.

## The word grammar

The segmentation is part of the contract, not each compiler's dialect: a word is a run of letters
and digits (with the combining marks that belong to them), a camel-case or letter/digit boundary
starts a new one, and **every other character separates** — `.`, `/`, `[`, `-`, a space alike. So
`com.example.User` and `com-example-user` canonicalize the same, and an emitter reading `Canonical`
never has to ask which compiler produced it. A source name with no word character in it
canonicalizes to the empty string; `Source` keeps the spelling.

At least one of `Source`, `Canonical` and `Hint` is set. A `Naming` with all three empty names the
entity to nobody, and every rule above is vacuously true of it, so `irverify` reports it
(`ir/naming-absent`). Where a source *does* name an entity with the empty string — a legal enum
value, property key, tag or component name — the compiler mints a hint rather than passing the
emptiness through; nothing is lost, because there was no spelling to keep.

**A camel-case boundary is where lowercasing changes the rune**, not where Unicode reports an
uppercase category. The two differ: double-struck `ℤ` and GREEK UPSILON WITH HOOK `ϒ` are
uppercase with no lowercase form, so they survive lowercasing unchanged and are not
boundaries — `COUNTℤ` is one word. The titlecase letters are boundaries and are not uppercase —
`xǅy` is two. Defining it by the effect rather than the category is what makes the grammar a fixed
point: a rune it split on but lowercasing left alone would still look like a boundary in the output,
and re-canonicalizing would split it again (GitHub #187). It is also the definition `irverify`
already applied to a whole canonical, so the producer and the checker agree by construction.

The **acronym tail** — the last capital of a run, before a lowercase letter — asks a different
question, and takes the category rather than the effect: `ℤServer` is `ℤ_server` because `ℤ` belongs
to a run of capitals whether or not lowercasing would change it, and `ǅBc` is `ǆ_bc` because a
titlecase letter belongs to one too. A transition test there would lose the first; the uppercase
category alone would lose the second.

`irverify` holds every `Naming` to this: the shape rules run over `Canonical` and `Hint` alike, and
it recomputes the canonical from the `Source` beside it, so a boundary in the wrong place is a
compiler bug rather than a variant reading. What no check can say is that the grammar itself is
right — a check that recomputes moves with what it recomputes through — so the answers are pinned
by a conformance table and the properties every answer must satisfy by a fuzz target beside it
(GitHub #186). Each decision it takes is held to `IRVersion` as well, since a changed answer renames
what emitters render: a change to this grammar, or to how a compiler composes a hint, is
[versioned](./ir-version.md) like a change of shape.

One rule sits under all of those and under `Aliases` too, because it is about the encoding rather
than the spelling: **every channel's bytes must decode**. It is not a naming rule at all: every
string a document holds must be well-formed UTF-8, because a `Document` refuses to encode one that
is not, so a single ill-formed name, ID or description fails the whole artifact. `irverify` holds
every string to it under one code, `ir/invalid-utf8`, at the path of the string that broke it.

## Aliases

**`Aliases` is held to none of the shape rules, and to rules of its own instead.** An alias is
matched against a name *another* schema wrote — an Avro type alias is a full name such as
`com.example.User`, or one relative to its type's namespace, and resolution compares it against the
writer schema's full name — so the separators and the casing are what the match is made of rather
than a spelling the IR gets to decide.
Neutralizing one would throw away precisely that, which is the lossy direction
lossless-by-default rules out. `Source` is the internal precedent: it carries `UserID` today and
no content rule touches it, because it records what the spec said rather than deciding a spelling.
What is left is decidable without any grammar, and `irverify` holds an alias to exactly that much:

- **Every entry names something** (`ir/naming-alias-blank`). An entry whose every rune is one
  Unicode classifies as invisible — white space, a control, or a `Default_Ignorable_Code_Point`
  (most format characters, the variation selectors, and fillers such as U+3164) — matches nothing
  under any grammar. `""`, `" "`, `"\u200b"`, `"\u3164"` and `"\ufe0f"` are alike here. The format
  characters Unicode takes out of that property because they render — the prepended concatenation
  marks, the interlinear annotation characters and the Egyptian hieroglyph format controls — are
  visible, so an entry made of them is not blank, as U+2800 is not.
  An invisible rune sitting *beside* a visible one is a different question and is not asked:
  whether `com.example.<ZWSP>User` is a legal name is decidable only under the grammar of the
  format it will be matched against, which the IR does not know.
- **Every entry decodes** — the shared byte rule above, which reaches an alias the same way it
  reaches the other three channels.
- **No entry repeats another, or the entity's own `Source`** (`ir/naming-alias-duplicate`,
  `ir/naming-alias-redundant`), reported at the later entry and naming the earlier, so the message
  says which to delete and which to keep. Either admits no name that was not already admitted, so a
  producer that wrote one built the list wrong. Only `Source` is compared against: `Canonical` and
  `Hint` are names the IR derived for an emitter to render, never names a writer schema could have
  spelled.

That such an entry is inert is also why a *source* declaring one is recorded once with a diagnostic
rather than carried through: dropping it is not the lossy direction, since the same set of names
resolves to the entity either way.

An alias can also collide with *another* entity's name, and that is not a rule of the list:

- **Within a record** it is a problem in the spec. An alias that another property of the same model
  answers to — by its wire name or by an alias of its own — leaves a reader resolving that name two
  fields to choose from, which is why Avro holds an alias to the same uniqueness as a name. A source
  can write one, and no lowering can drop either claim without choosing between them, so
  `pass/validate` reports it (`pass/ambiguous-alias`) beside the duplicate wire names of the same
  scope, and `irverify` does not.
- **Across the type registry** nothing reports it, because nothing in the IR decides it. A relative
  alias is qualified by its type's namespace under the format's grammar, so one string on two types
  in two namespaces is two names; and one document can hold several independent schemas — each
  message payload of an AsyncAPI document is one — that may each declare the same type and alias.
  A collision between named types is the compiler's to report, within the schema it parsed.

## Names one scope shares

Two entities may share a neutral name. Words cannot tell `getUser`, `get_user` and `GetUser` apart,
nor `-1` from `1`, and a source may write the same name twice. That is no defect of the IR: an
emitter tells such names apart by ID ([naming](../emitters/refine/naming.md)), and renames
nothing in the document. But any target may meet the collision, since rendering a name from its
words alone is conforming
([naming](../emitters/refine/naming.md#the-source-spelling-for-letter-case-only)), so one check over
the neutral words answers for all of them, and the `names` pass reports it to the document's author
(`pass/name-collision`, a warning).

- **The name compared** is the one a target renders from: `Canonical`, or `Hint` where there is
  none, so a hint meeting a source spelling collides with it, as the group of untagged operations,
  hinted `default`, does with a tag `default`.
- **The scopes** are the siblings a target renders into one namespace: the document's
  services, its declared types and its auth schemes; a service's groups; a group's groups and its
  operations; an operation's parameters; a model's own properties; an enum's members; a union's
  variants. One name in two scopes is no collision.
- **Declared types** are one scope, whatever their `Namespace`: a target places a type by its
  options, never by its namespace, and by default places every type in one unit
  ([output units](../emitters/refine/output-units.md)), so two types of one name meet wherever they
  were declared. An alias is another matter: it is matched against a writer's names within its
  namespace, which is why the registry is no scope of its aliases (above). An anonymous type is left
  out, since a target may inline it rather than name it, and its hint is the compiler's suggestion.
  So is a primitive, an `Any` node or an `External`, which a target resolves through its type table
  and never declares ([type table](../emitters/contract/type-table.md)).
- **A finding is placed** at what holds its scope, the document for its own. A party's place in
  its scope follows declaration order where a mapping declared it, so a finding placed at a party
  would move with that order.
- **A party is named** by its ID, or a union's variant, which has none, by the type it holds. An
  index names a place in the IR's list, which need not be the source's: an OpenAPI operation's
  parameters join those its path item declares, and two of one spelling differ only in the location
  their IDs hold; a union's variants leave out a `null` branch, and a hint has no spelling. A
  property or an enum member is named by its index beside its quoted spelling instead, because its
  ID holds a source key or value verbatim, control characters included: a key is unique in its
  mapping, and two members of one spelling are one value written twice. Any other ID can carry
  one from a tag, path, parameter or component name, which is written raw where diagnostics
  render (#816).
- **An exact repeat is reported** like any other collision: two documents each declaring a
  component `Pet` give two types that render alike, which nothing else reports. A repeat may also
  draw a finding of the compiler's, such as `openapi/duplicate-enum-value`, which says what the
  source repeats, where this one says what a target does with it.
- **A name with no word character** has no neutral name at all, and a target names it from its
  characters
  ([naming](../emitters/refine/naming.md#a-name-with-no-words-takes-its-characters-names)), so the
  pass reports each such entity as an info, `pass/name-without-words`. It compares the name only
  with the same spelling, which every target names alike: another, such as `==` beside `=`, meets
  it only through the table of character names, which a target's options may replace.

Left out, each for its reason:

- **A model's inherited properties.** A model renders the properties its composition exposes
  ([models](./models.md#the-flattening)), so an own property can meet an inherited one. Reading a
  flattening per model grows quadratically down a hierarchy, as `validate` found for its own
  checks, and the case is measured absent: no model compiled from the golden spec
  (`cloudflare/api-schemas` @ `5c37ac8c`, morphic @ `fd8b0730`) exposes two properties of one
  neutral name declared by different models (#1412). An own collision is reported at the model
  declaring it, whatever composes it.
- **A group beside an operation of the same parent.** Whether a sub-client and a method share a
  namespace is the target's: a Go struct cannot hold a field and a method of one name, while a
  Kotlin class can hold a property and a function.
- **A name a target composes**, such as an operation's request type beside a declared type of that
  name. The composition is the target's.
- **Servers, responses and error cases.** The emitter design renders no identifier from a server's
  `Name` yet ([the client](../emitters/operations/client.md)), and a response or error case is told
  apart by its status; the OpenAPI compiler reports a name one servers list repeats
  (`openapi/duplicate-server-name`) and two keys of one status range
  (`openapi/duplicate-status-key`).
- **Channels and messages, a field's arguments and a resource's parts** (`Property.Args`,
  `ResourceInfo.Identifiers` and `ResourceInfo.Properties`), which no compiler produces yet, and for
  which no emitter design says what scope their names render into. They and servers join once a
  target names them (#1413).
- **A response's or an error case's headers**, which a target renders as the fields of one type.
  Neither has an ID to report at, and a path through its operation's list moves with the order the
  source declares responses in (#1418).
- **A service's renames** (`Service.Renames`), which no compiler writes yet: a declared type is
  compared by its own name, though a rename presents it by another in its service (#1419).
- **An operation overloading another** (`OverloadOf`), which no compiler sets yet, is compared like
  any other operation, by its own name: no emitter design says yet how a target presents an
  overload beside its base.
- **Groups nested past `ir.MaxGroupDepth`**, which `ir.ForEachGroup` does not enter, and which
  `validate` reports (`ir/walk-truncated`).
