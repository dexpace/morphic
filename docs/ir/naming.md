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
(GitHub #186).

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
  Unicode classifies as invisible — a space, a control, a format character, or a default-ignorable
  one — matches nothing under any grammar. `""`, `" "`, `"\u200b"` and `"\u3164"` are alike here.
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
