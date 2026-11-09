# Docs and mock-server emitters reuse the contract

Both are `Emitter` implementations consuming the **same `Doc + Plan`** — the payoff of computing the
plan once: docs and mocks agree with the SDK by construction, not by re-derivation.

- **Docs emitter (`emitters/docs`).** Reuses the shared plan verbatim (pagination, return shape,
  error taxonomy — what docs must describe; [what the docs render](#what-the-docs-render) says which
  responses) and most of the refine step library, but lowers into a `docast` (pages, operation
  blocks, schema tables, examples) and runs a **reduced pipeline**: no `EscapeReservedWords`, and
  casing is a presentation choice (SDK-style identifiers or spec-faithful source names, set in its
  options). The spans [`Docs.Refs`](../ir/annotations.md#references-in-prose) marks resolve to
  intra-doc links via the *same* IR-ID lookup the Go emitter uses for doc comments, so a link
  follows what its target renders as. Examples use the schema arm (`Value/Headers`) vs the operation
  arm (`Input/Output/Error`) correctly. A union or enum `TypePlan.Open` marks is documented as one
  that may hold a variant or value the document does not list, one inclusive in a shape as one whose
  value may be several variants at once, and every `PlannedError` and `PlannedResponse` as the body
  and headers it declares. An enveloped response is documented as the `Type` a method returns and
  the `Envelope.Body` the wire carries, and an example of it as both: the member at `Value` of the
  response's example for the first, and that example whole for the second. An operation is
  documented as safe or idempotent where its `Effects` say so. Its README is returned as blocks a
  user writes around ([write / integrate](write-integrate.md#a-document-the-user-writes-around)). No
  runtime `Policy` is consulted — proving the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`, the content `Return` elects
  and each response content's [example set](#one-set-of-examples) for canned responses, synthesizing
  none of its own, sending the whole `Envelope.Body` where a response is enveloped, `Others` to
  answer as each declared response does, the error taxonomy to simulate declared failures,
  `TypePlan.Open` to say whether a variant or an enum value the document does not list may be sent,
  `TypePlan.Strict` to refuse a `oneOf` value as the SDK does, the `BindingView` to route requests,
  and the visibility projections to honor the same wire shapes an SDK sends/receives. Refine lowers
  into a server-routing AST (routes from `HTTPBinding.URITemplate`, handlers validating against
  constraints). Because the same plan feeds SDK and mock, the two are **wire-consistent by
  construction** — the mock can *be* the interception target the SDK's wire-conformance harness
  ([mandatory tests](testing/mandatory-tests.md)) is diffed against. A response content whose
  example set is empty, as one can be with `Synthesize` off, is a warning
  `emitters/mock/example-missing`, never a body it invents.
- **Validation emitter (future).** Additionally consumes the validation-only constructs kept
  verbatim in `Unmodeled` (`not`/`if-then-else`/`dependentSchemas`, …) that SDK/docs emitters
  ignore. It selects them by `UnmodeledEntry.Reason == ir.ReasonValidationOnly` rather than by key
  prefix, so vendor metadata sharing the same map never reaches it, and reports each finding at
  `UnmodeledEntry.Provenance` — the keyword's own position, not the enclosing schema's. The
  contract already carries all of that; no IR change is needed (INV9).
- **CLI emitter (future).** A command-line client is the third surface APIs ship beside SDKs and
  docs, and almost all of it is IR and plan facts ([a command-line target](#a-command-line-target)).

Adding any of these is a registry entry over one `Doc + Plan` — no change to the ABI, no change to
the IR.

## What the docs render

The primary response and content are an SDK's ergonomic choice: a method returns one. A reader of
the docs needs every answer the API gives, so the docs emitter renders every answer the plan lists,
`Return`, `Others` and `Errors`, the last holding the service's `CommonErrors` too
([responses](plan/responses.md)), the `Default` once, with the statuses it answers, wherever the
plan places it, and every `Content` each declares, the primary response first and the elected
content first within each, and those choices order the page and decide nothing else. A type is
rendered once for each projection its `TypePlan.Shapes` gives it
([ModelShape](plan/types.md#modelshape--visibility-projection)), each table holding the members
visible in that shape and marking those it requires, and shapes whose projections coincide share one
table, as they share one declaration
([models](refine/models.md#visibility-projections--n-wire-shapes)). So a write-only property is in
no read shape's table and a read-only one in no create shape's, and a model one operation creates
and another patches has a table for each, since a patch body's members are optional and a member
visible only at creation is absent from it. A direction alone would merge those two. A model whose
own members and those of the models it holds carry no visibility marker has one table for every
shape, a patch's apart only where it or a model it holds requires a member or gives one a default,
since a patch relaxes what is required and carries no default onto the wire, so its table shows
none, and on the golden spec (spec @57b5196b, morphic @ab953d45) 15,694 of 17,872 models carry none
on their own members or on any model they hold or inherit, so a table per shape would repeat itself
for any of them two shapes but a patch reach. A type no shape reaches, one no operation sends or
returns, is rendered once, unprojected, its table anchored by its model's anchor alone and shown
through no shape, which `Shown` takes as a nil `ModelShape`, as an SDK declares it ([a shape reaches
what it holds](plan/types.md#a-shape-reaches-what-it-holds)).

A table lists every member its model's flattening holds, each name `TypePlan.Undeclared` lists among
them as a row typed as the catch-all types it, required save in a patch shape's table, which relaxes
it as it relaxes any member, with no anchor since it has no `PropID`, and anchors a member only
where the model declaring it is rendered; another table holding it, a subtype's for an inherited
member, links there. A model rendered as several tables anchors each member in the first of them, in
`TypePlan.Shapes`' order, that holds it, and the others link there, since a route holds one anchor
for each `PropID`. A member no table of its declaring model holds, as one of a base reached only in
shapes that leave the member out while a subtype's table holds it, is anchored in the first table,
in the byte order of their `TypeID`s, of a model whose flattening holds it. Each table has an anchor
of its own for each shape it shows, a table coinciding shapes share carrying one for each of them,
named through the page's ID-keyed resolution from its model and its shape, as identifiers are
([naming](refine/naming.md)), so it never takes a member's or another table's anchor, as a member
named `create` beside a create table would; how a shape is named is #1405's to settle. A switcher
links a model's page in another version, never one of its tables, since a table's anchor is resolved
within its own page and a version's page may resolve it otherwise, and links the page holding an
anonymous model's table where that version gives the model no page of its own, and a link to the
model names the shape it shows, so an operation's patch body links to the patch table, never to one
marking required what the patch relaxes. An anonymous model another composes in place is no table of
its own: its members are rows of the composing model's table, anchored there, so no member is
anchored twice.

A reference is read through `TypePlan.Alias`, as every consumer reads one
([decorators](refine/decorators.md)). A decorator has no page, table or anchor: a reference to it, a
`DocRef` naming it among them, renders and links as one to its `Alias`, and its annotations fold
onto the position holding it as `CollapseAliases` folds them, the docs reading from the IR those no
position holds. So a 3.0 `allOf` over a sole `$ref` and the 3.1 `$ref` beside the same annotations
render alike, as T-16 requires of every artifact. By `ir.Decorated`, 1,610 of the golden spec's
types (spec @57b5196b, morphic @ab953d45) are decorators, 645 of them models, and 758 carry the
examples of the position they decorate.

An example is shown through the shape it is shown in, so a response's example holds no write-only
member and a request's no read-only one, and a member that shape requires and the example lacks is
filled ([below](#one-set-of-examples)). A value `Property.Secret`, `Parameter.Secret` or
`TypeCommon.Sensitive` marks is redacted in docs ([properties](../ir/properties.md)), so pages,
agent documents and samples show a placeholder in its place, whether the value was declared or
synthesized; an SDK's tests and the mock still send it. A sample's placeholder is the SDK target's
to write, since the call is its code and the docs never edit it.

A description is the spec author's text, CommonMark that may hold raw HTML: `Docs.Description`,
which the IR states is CommonMark, and `Example.Description` and `Link.Description`, which the docs
read as CommonMark too, since OpenAPI writes them so. A generated site that passed that HTML through
would publish whatever markup or script the document holds, and one that rendered all of it as text
would show a Smithy model's `<p>` and `<code>` on every page. So pages and agent documents
([below](#documents-for-agents)) render it through an allowlist: `p`, `br`, `b`, `strong`, `i`,
`em`, `code`, `pre`, `ul`, `ol`, `li`, `blockquote`, `sub`, `sup`, `dl`, `dt`, `dd`, `h1` to `h6`
and `hr` with no attribute, `a` with an `href` and `img` with a `src` and, optionally, an `alt`,
each URL held to the rule below, render as themselves, read, its inline raw HTML and its HTML blocks
alike, by an HTML tokenizer, one that splits tags from text without building a tree, as
`x/net/html`'s `Tokenizer` does, against a stack of its own, the tokenizer never entering the
raw-text mode HTML gives `title`, `textarea`, `script`, `style` and their kin, since none of them is
listed (with `x/net/html`, `NextIsNotRawText` after each start or self-closing tag token, since a
self-closing `<title/>` enters the mode too), so an unlisted `<title>` cannot swallow the rest of
the description. A text token is decoded and written escaped. A listed start tag, or a self-closing
one, which HTML opens as a start tag, opens an element, `br`, `hr` and `img` opening none, written
from its name with its listed attributes decoded, checked and escaped and every other attribute
dropped, never from its raw bytes, so `<a href="https://…" target="_blank">` renders as a link; a
listed end tag closes the element it matches, and any above it on the stack, and renders as text
where none matches; and a `p`, `li`, `dt` or `dd` on top closes when a sibling opens or its parent
closes, a `p` on top closing before an `li`, `dt` or `dd` is checked against its list. Every other
token, a tag, an end tag or a comment, is written escaped from its raw bytes, so a stray `</td>`
renders as text rather than closing the page's own table, a placeholder such as `<key>` keeps its
place, and what is still open closes where the description ends. A refused `a` is its text, both its
tags dropped, and a refused `img` its `alt`, else its tag as text. An `li`, `dt` or `dd` opens only
with its list on top of the description's stack, and otherwise renders as text, so a description
cannot add an item to the page's own list. No table tag is listed, since a browser closes a page's
table around a description's own in ways no stack of the description's can follow, so a description
in a schema table's cell cannot add a cell or a row to it; a description's table renders as text.
The contract is that outcome, which T-41 checks by parsing each page as a browser does: the page's
own elements stand as written around every description, however hostile, and every token the
description holds stays visible or renders as a listed element. An HTML parser's tree-building
fragment mode is no substitute: it drops a stray end tag and turns `<key>` into an element holding
the rest of the text. Any other tag or comment renders as text, a custom element such as `<note>`
among them. A GitHub-style table, which CommonMark does not read and 12 golden-spec descriptions
write, is kept as preformatted text, so its rows stay apart. On the golden spec (spec @57b5196b,
morphic @ab953d45), descriptions hold 45 raw HTML tags by CommonMark's grammar, outside code, and 4
comments: 11 tags a `<br/>`, which renders as a line break, one an `<img>`, which renders as text
since the rule below refuses its `cid:` source, and 33 placeholders such as `<key>` and `<uuid>`,
which no list names, so a placeholder stays visible where a browser handed it would parse it as an
element and drop it. A summary, a name or any other spec string that is not CommonMark is escaped as
text.

A link or an image in that text, and a URL the document gives, such as an `ExternalDocs` link's, an
`Example.ExternalURL` or a contact's, license's or terms-of-service URL, can carry script the same
way: a `javascript:` destination runs when it is followed. A destination is checked once its
character references are decoded, by HTML's rules in an attribute and CommonMark's in a link, so
`&#106;avascript:` is refused too, and written escaped again. So a page or an agent document writes
a destination only where it is absolute under `http`, `https` or, for a link, `mailto`, or a
relative path in a description, which points into the site the author wrote for and is resolved
against the base the docs options give that site, as OpenAPI resolves a description's links in the
context they are rendered in. A fragment alone names a place on one of that site's pages, which no
base names, and a relative URL in a field of its own, such as an `ExternalDocs` link's, resolves
against a server URL, which may be one of several or templated, or the document's own location,
which may be a local file, so no one base serves it. Any other destination, and a relative path with
no base set, renders as its link's text, with a warning `emitters/docs/link-unwritten` naming it.
`Document.TermsOfService` holds a URL or text, so a value that parses as no absolute URL is rendered
as text, never resolved. On the golden spec, at those revisions, the links in `Docs` descriptions
are 988 under `https`, 7 under `http`, one an `https` autolink, and 14 relative, 8 of them a
fragment alone, such as `#requests`, and 6 a path, such as `/api/resources/filters/`, which would
otherwise break on a generated site.

## Routes and links

Every docs target needs URLs, links between pages, an answer for two entities wanting one URL, and
a way to show one entity across API versions. Each is decided by data, never by a renderer:

- **A catalog first, URLs second.** The emitter builds a catalog — services, groups, operations,
  channels, messages, auth schemes, and the types with a page of their own — holding no URL. A
  channel, a message and an auth scheme each have a page, an `unresolved` scheme's saying the
  document names it without defining it, since several operations or channels name one, routed by ID
  as every entity is; a message's and a channel's IDs carry no kind prefix yet to keep them apart
  from other classes in one table (#1527). A type the document declares, neither `Anonymous` nor a
  primitive, has a page whatever its kind, and so does an anonymous model, union or enum that more
  than one reference reaches, counting through the anonymous lists, maps, tuples and scalars that
  hold it and the decorators over it. Any other type but a decorator (above) is rendered where it is
  referenced: an anonymous model as a table on the page its one holder is rendered on, and any other
  anonymous type as the type it spells. So no table is rendered in two places, and a type spelled
  where it is used, such as a list of strings, is spelled at each use and takes no anchor, as a
  primitive takes none. On the golden spec (spec @57b5196b, morphic @ab953d45), 28 anonymous models
  are bodies 2 to 38 operations share, 24 of them no decorator and so pages, and a page only for a
  model, union or enum that more than one reference names directly would leave one more rendered in
  several places: the enum a component parameter's anonymous list holds, which five operations
  share. A route plan then derives every URL from the catalog alone, through templates in the docs
  emitter's options, one per kind of page (`"/{service}/{group}/{operation}"`), a nested group's
  `{group}` being its groups' slugs from the service down, joined by `/`, and one template's scope a
  group's direct children. A slug is presentation (INV3), rendered from names by the naming policy,
  and two that collide in one template's scope are told apart by the ID-keyed resolution identifiers
  take ([naming](refine/naming.md)): three pairs of the golden spec's declared types share their
  canonical words, which the SDKs resolve as well. Two slugs collide whatever their letter case, as
  artifact paths do ([emit rules](emit/rules.md)), since a file system that ignores case keeps one
  page of the two: two of the golden spec's groups, in its one service, are `Webhooks` and
  `webhooks`, which slugs spelt as the source spells them would part by case alone.
- **A total route plan.** Every `ServiceID`, `GroupID`, `OpID`, `ChannelID`, `MessageID` and
  `AuthID` and every type with a page gets a route, every anonymous model rendered as a table on
  another's page an anchor there, and every `PropID` a model declares an anchor where that model is
  rendered (above), save one no table holds, such as a write-only member of a model only responses
  reach, which is rendered nowhere, a decorator answering with its `Alias`'s. A `Property` no model
  declares, a header or a resource's identifier or state (`ResourceInfo`), is a row where its holder
  is rendered and takes no anchor, since no link names one. Anything left without one is an error
  `emitters/docs/route-missing`, and a recursive type is linked by its ID, never inlined to a depth
  and cut.
- **Collisions are refused.** Two entities whose routes resolve to one URL, letter case aside, are
  an error `emitters/docs/route-collision` naming both, never a page that overwrites another and
  never a page dropped. The check runs on the URLs the templates produce, over every route a run
  plans, its table's and its agent documents' together, so no template can reintroduce a collision
  the catalog did not have.
- **Links are requests.** A renderer never formats a URL. It writes a link request, an entity's ID
  with the projection it wants (a page, an anchor, a model's table in one shape, the agent document
  of one target and language), and the route plan answers it. A spec's
  [`Docs.Refs`](../ir/annotations.md#references-in-prose) resolve the same way, so a link follows
  its target wherever its route moves, save a span inside raw HTML, a link, an `a` open on the
  description's stack or a code span, which renders as its text, since a link there would split a
  tag, nest in another link or show as code, and a span naming a type no route reaches, a primitive
  or a type spelled where it is used, which renders as its text too, as the IR asks ([references in
  prose](../ir/annotations.md#references-in-prose)), and draws no `route-missing`. A page no entity
  owns, the index of the versions or an `llms.txt`, is requested by its kind and its scope, a
  version's label, a service's ID or an SDK target and language, which the same templates route.
- **Versions keep identity.** The emitter receives one version's snapshot ([contract
  rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and an entity's
  page is identified by its ID, never by the version, so a version switcher links one entity across
  versions. Each snapshot's run writes under the directory its label names, as the contract has a
  cross-version target's do ([contract
  rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), which the route
  plan, and no template, puts before every route but those of the pages spanning versions, so one
  entity's pages in two versions have two paths. The runs make one generation of the target, whose
  manifest holds each run's under its label ([write /
  integrate](write-integrate.md#what-the-manifest-records)). A document no pass slices by version,
  one stating none among them, is one snapshot labelled `""`, written at the output root. For a
  target whose `Capabilities.CrossVersion` is set, the engine asks the target for every snapshot's
  route table through `Router` ([the contract](contract/emitter.md)), before any snapshot's
  `Generate`, handing it the input it will hand that `Generate` but for `Samples` and `Routes`,
  which no page's or anchor's route reads, and hands each run the tables of all of them in
  `EmitterInput.Routes`, keyed by the label `Document.Versions` gives each version. An agent
  document's route, one for each target and language a run's samples name, is that run's own and in
  no table, though the collision check reads it. A label is percent-encoded where a link writes it,
  since a `#`, `?` or `%` in it would otherwise end or alter the URL. A switcher lists the labels
  `Routes` keys, in the order `Document.Versions` gives them, never one no snapshot was made for. A
  run finds its own entry under `EmitterInput.Snapshot`, the label of the snapshot it was handed,
  which is also the version its switcher marks current: nothing in a snapshot's document says which
  version it is. The engine cannot plan the routes itself, since the catalog and the templates are
  the target's. A run plans its own routes by the same pure function, so its pages agree with its
  snapshot's entry. A table holds each entity's page and anchor from the output root, its snapshot's
  directory included, which is what a switcher links. What spans versions, the site-scope `llms.txt`
  linking each version's and an index of the versions, is written once, by the run of the last of
  the labels `Routes` keys in `Document.Versions` order, which every snapshot keeps whole
  (architecture.md §2.2), since every run holds the labels and tables it is built from; those pages
  sit at the output root and belong to the one generation, so they keep their place when another
  label becomes last, and the collision check reads them against every label's directory, so a
  template putting one under a label's name is refused. The `""` snapshot's run writes the site
  scope, which is then its version's too, as one index. A link to an entity a version lacks resolves
  to that version's page saying so. Slugs may differ between versions; IDs do not.

## Documents for agents

Beside its pages, the docs emitter writes the same content as plain Markdown for coding agents, from
the same `Doc + Plan` and through the same route plan, so an agent document and a page never
disagree about a URL:

- **One document per operation**, holding how it is called, an HTTP method and path or what another
  binding names, parameters and schemas as nested lists, examples, code samples and links, as one
  aggregate document and one per SDK target and sample language, so an agent reads only the language
  it writes.
- **`llms.txt` indexes** at site, service, version and target scope, each a Markdown index linking
  the agent documents of its scope rather than the pages.
- **Facts, not guesses.** An operation is marked safe, or safe to retry, from `OpPlan.Effects`
  ([OpPlan](plan/operations.md)). A warning that it destroys something comes only from its
  `Destructive` hint ([shaping hints](contract/shaping-hints.md)), which a document's own extension
  can set through the [embedded reader](contract/embedded-policy.md). No IR fact states it, and a
  method cannot: `DELETE` and `PUT` are idempotent and may destroy.
- **Spec text is escaped.** A heading or a list item built from a summary or a description is
  escaped for Markdown, a description is parsed as CommonMark and written back out within its list
  item, so an unclosed fence closes at its end and a link reference definition reaches no other
  description, its raw HTML rendered through the pages' allowlist, and a link is written only where
  a page would write it (above). A code fence around an example or a sample is at least three
  backticks and one longer than the longest run of backticks inside it, so no value closes it.

## Samples come from the SDK emitters

How a call is spelled in one language — the client accessor, the method name, the arguments, a
model's constructor — is that SDK emitter's refine output, and an emitter never sees another's code
([emitters](README.md)). A docs emitter that spelled calls itself would re-derive them and could
disagree with the SDK it documents. So each SDK emitter returns its usage samples per operation in
`EmitterOutput.Samples`, one for each language its callers write: a Multiplatform target's callers
write Kotlin and Swift ([Swift](targets/kotlin/swift.md)). A target whose
`Capabilities.ReadsSamples` is set names in its options the targets whose samples it shows, which
the engine reads through `SampleReader` ([the contract](contract/emitter.md)), a decoded options
value without it being `engine/sample-target-invalid`, and the engine runs each of them on every
snapshot before it, whether or not the run names them, with the configuration that target would run
with, which the engine finds as it finds any target's ([target options](contract/options.md)), and
the prior manifest of that target's own output, as a run naming it would read it, writing none of
its artifacts unless the run names it too, and hands it their samples in `EmitterInput.Samples`. So
what a docs run shows depends on its options and never on which other targets share the run: a docs
target whose options name `go`, run alone, shows the samples a run of both would. A named target the
registry lacks, one whose `Capabilities.WritesSamples` is unset, or one that reads samples itself,
is an error `engine/sample-target-invalid`, which the engine reports since the docs target cannot
see the registry, and one with no configuration is refused as an absent configuration always is. The
engine runs a named target for its samples even when its generation writes nothing ([write /
integrate](write-integrate.md#what-the-manifest-records)), since a docs run handed none would drop
them, and, when the run names it too, runs it once with its artifacts, which on a sliced document
only a cross-version target may be (#1531). A run made for samples alone reports what any run of
that target reports, and the engine decides as for any run whether that is fatal: the docs show that
SDK's calls, so an SDK that fails to generate fails the docs showing it. A target returns a sample
for every operation its callers call, whatever its `Scope`, which gates its artifacts alone, since
its names are the whole document's: a scoped SDK run's docs keep every sample. It returns none, with
no diagnostic, for an operation the API starts, a webhook, a callback or a message the client
receives, since no call spells it. A docs generation is a new one whenever the samples it is handed
change, as an SDK target's own options can change them. A sample a target cannot render is a warning
`emitters/<target>/sample-unrendered`, never an empty code block on the page, and so is one for an
operation whose required input holds no example. A sample shows a call that succeeds, so its values
are the `Input` of its operation's first success scenario (below): one ending in no `Error`, since
an `Error` scenario's input is chosen to fail, and perhaps in no `Output`, as an operation answering
with no body does. Its values are shown through the shape the operation sends them in, and where
`Shown` cannot complete them the sample takes the next success scenario. That is one the operation
declares in `Operation.Examples`, as a Smithy or TypeSpec document can, else the one the plan
assembles. An operation with neither has no sample: where `Assemble` is off it draws no diagnostic,
whatever its inputs hold, since the policy chose it, and where `Assemble` is on and a required input
holding no example kept one from being assembled, it draws the warning above. A value `Secret` or
`Sensitive` marks is written as the target's placeholder for it (above).

## One set of examples

READMEs, samples, generated tests and mock responses all need example values, and each would
synthesize its own where the document declares none, so the docs would show values the SDK's own
tests never sent, and every regeneration could change them. So the plan holds one set,
`Plan.Examples`, keyed by the ID of each type, property (a header among them), parameter, enum
member and operation:

- **Declared first.** An entity's own `Examples` are its set, labelled authored in the docs, save
  one holding an `ExternalURL` and no value, which the docs show as a link and which no set holds,
  since no value of it can be checked, shown, filled from or sent; each checked against its type and
  constraints with visibility set aside, since the shape it is shown through settles that: a value
  no shape could make valid, such as one its enum does not list, is refused, an enum the document
  leaves open (`Enum.Closed` false) admitting a value it does not list. Shown through a shape, an
  example drops the members that shape does not hold, keeping a key no member declares that the
  model's catch-all admits, checked against the catch-all's type, and takes, for each member the
  shape requires and the example lacks, that member's own first value, an `Undeclared` name's from
  its catch-all type's set, which marks what is shown `Inferred` and labels it generated, its
  declared members kept as written; with `Assemble` off it fills nothing, and an example missing a
  member the shape requires gives way as one it cannot complete does. So a response-form example
  serves a create shape, a patch body's may leave out a required member, and, with `Assemble` on,
  every required member's set holding a value and no `Strict` union left ambiguous by the shape, no
  shape is left without one. `Build` refuses a union value several variants admit where the union is
  `TypePlan.Strict`, which decodes it as no variant, reporting it once as above, and `Shown` shows
  such a value elsewhere through all of them, keeping each member one of them holds in the shape,
  since an inclusive value may be several variants at once. A shape can make a value two variants of
  a `Strict` union admit, by dropping the member that kept one of them out, so `Shown` checks
  admission again in the shape it shows and is false there, as for a member it cannot fill.
  `plan.Shown` does this, the one projection every consumer reads an example through, reading
  `Assemble` from `Plan.Policy.Examples`, and returns false for an example it cannot complete, a
  required member's set being empty, so the consumer takes the set's next one; with none left the
  position has no example there, which the docs show as none, a sample reports as
  `sample-unrendered` and the mock as `example-missing`. One they refuse is reported as a warning
  `emitters/plan/example-refused` and left out, so every consumer falls to the next set alike: an
  SDK's test sending it would be refused by the mock, as by the API. On the golden spec (spec
  @57b5196b, morphic @ab953d45), a declared example fails its own type in at least 295 types: in 292
  no member of the enum, each closed, holds it, such as `json` against `JSON` and `CSV`, and in 3 it
  is longer than `maxLength`. One declaring none that adds no constraint to its type's takes its
  type's set, so a property shows the value its type does, save an enum member, which takes its own
  value. A position whose type is a decorator takes its own examples, else the nearest decorator's
  along the chain, as `CollapseAliases` folds them, else, where it adds no constraint, the set of
  the type its `Alias` names.
- **Assembled for an operation.** An operation declaring no success scenario, one ending in no
  `Error`, takes one after those it declares, where every required input's set holds a value, built
  from the sets of its required parameters and of the optional ones whose set is declared rather
  than synthesized, of its body, required or not, from the elected request content's, and of its
  return content where it returns one: a call shows no optional filter the document gives no value
  for, and always the body it sends, which 231 of the golden spec's operations, sending an optional
  body with no example on it, would otherwise lack. Its `Input` is an object holding each
  parameter's value under its `ParamID` and the body's under `body`, which no `ParamID` can be,
  since a parameter and a body member may share a name, as they do in 9 of the golden spec's
  operations; a declared scenario's `Input` is read in the shape #1528 settles. The document never
  paired those values, so the scenario is `Inferred` and labelled generated whatever values it
  takes, and `plan.Policy.Examples.Assemble` turns it off. On the golden spec no operation declares
  a scenario.
- **Positions with no key.** A content and a variant have no ID, and a message's ID carries no kind
  prefix yet to keep it apart from the other classes in one map
  ([IDs](../ir/identity.md#what-irverify-holds), #1527). So each one's set is what the plan's
  `ContentExamples`, `VariantExamples` or `MessageExamples` returns for it, the one function of its
  kind that the docs, the samples and the mock read it through: its declared examples, read from the
  IR and checked as above, else its type's, a message's those of its payload's contents, each paired
  with the first value of its `Headers` type's set as `Example.Headers`, since a message's examples
  are header and payload pairs, a pairing the document never made and so marked `Inferred` and
  turned off with `Assemble`, a body's examples being its content's set. `Build` reports each
  example of these positions it refuses, once, so the functions report nothing and no consumer
  reports it again. Contents are where a document most often declares a body's examples: on the
  golden spec (spec @57b5196b, morphic @ab953d45), 435 request, response and error contents declare
  651 examples.
- **Synthesized only where no declared set applies**, under `plan.Policy.Examples`: a value valid
  against the entity's type and constraints, and against the validation-only constructs `Unmodeled`
  keeps for it (`not`, `oneOf`, `anyOf` and their kin), evaluated as the JSON Schema they are, since
  the API holds a value to them: a synthesized value failing one is left out and reported, and a
  declared one failing one is refused, as the golden spec's 34 types carrying one require, marked
  `Inferred` and labelled generated. A model synthesized holds, for each member, an `Undeclared`
  name among them, the first value of that member's own set, and a list or map, for its k-th item or
  entry, the k-th, its set's values taken in turn and, once they run out, synthesized values seeded
  by index, so items differ where `uniqueItems` or a map's keys require it, a map's keys being
  distinct values of its key type; a `uniqueItems` list holds no more items than distinct values
  exist, and one left below its minimum is unsynthesizable. Each such value is a declared one whole,
  or a synthesized one cut to the depth the bound leaves, so a property's value is the same alone
  and inside its model wherever the bound leaves it whole, and an optional recursive member ends.
  The bounds cut only what validity leaves optional ([plan policy](plan/policy.md)): an optional
  member more than `MaxDepth` models deep, and the entries of a list or map past the larger of
  `MaxItems` and its minimum, or past its minimum alone once it lies deeper than `MaxDepth` or on a
  recurrence of its own type along the value, so a recursion through no model, as the golden spec's
  `cws_JSONValue` map and list recur, ends too, so a required list of its own model ends: the golden
  spec's `cloudforce-one_TreeNode` requires `children`, a list of `TreeNode`, which holds none past
  that depth. A required member is filled as deep as its schema requires, one whose reference is
  `Nullable` ending a cycle with `null`, and a `minItems` above `MaxItems` is met: on the golden
  spec 250 models reach a model four or more deep through their own or inherited required, non-null
  members, and one list requires four items, which the default bounds would otherwise leave without
  an example. A value the synthesizer cannot make valid, as a `pattern` it cannot satisfy or a
  required cycle no value ends, or one holding more values than `plan.MaxExampleValues`, a fixed
  limit the plan package states, 10,000, which bounds what a hostile schema can make one example
  cost, is reported as a warning `emitters/plan/example-unsynthesized` and left out, never emitted
  invalid.
- **Stable.** Each choice the synthesizer makes, such as which enum member or which variant, is
  seeded from the entity's [correlation key](../ir/derived-facts.md#correlation-keys), or from its
  ID where no key identifies it, save on a recurrence of its type along the value or deeper than
  `MaxDepth`, where it takes the first variant or member, in source order, from which a value ends.
  So a required recursive union ends: the golden spec's `cloudforce-one_EmailRuleCondition` reaches
  itself through a variant requiring a non-empty list of it. So a value is the same in either
  declaration order, and on every regeneration until the entity's schema, its key, or its ID where
  it seeds from one, the example policy, `irfacts.CorrelationVersion` or the synthesizer's code
  changes.
- **Shared** by the docs emitter, the samples, each SDK's tests and the mock server, none of which
  synthesizes its own.

The manifest need not persist them: a seeded synthesizer reproduces each value from its inputs. A
value still moves when its entity's key or `irfacts.CorrelationVersion` does though its schema
stays, as a renamed schema's would, and a persisted value would move with it, since it could be
found again only by that key or by an ID the rename moves too, so persisting would add state without
saving that churn.

## Documentation extensions

Documents carry presentation as vendor extensions: a logo, a tag's display name, groups of tags,
code samples on an operation, a marker on an internal operation or property. They stay verbatim in
`Unmodeled`, and the docs emitter, like every emitter, never reads one itself. The [embedded
reader](contract/embedded-policy.md) reads those a configuration maps into members of a hint's
`Extra` ([shaping hints](contract/shaping-hints.md)), by its rules for where a key applies, for two
keys setting one member and for layers. Its default mapping is empty, since each key is one tool's
spelling. The docs emitter decodes the `Extra` members it defines strictly, reporting a wrong shape
as a warning `emitters/docs/extra-invalid` and coercing nothing. A key held for a tag, as a display
name is, configures nothing until the IR carries a tag where it lowers (#1221). A code sample a
document declares is shown as authored beside the samples the SDK emitters return, its language kept
as the fence's info string only where it is one word of letters, digits, `+`, `-`, `#` or `.`, so no
value reopens the code as Markdown. An internal marker is the filter pass's to read (architecture.md
§2.2), never the docs emitter's: hiding an operation in the docs alone would document an SDK
different from the one generated. A search index over the pages, keyed by ID, is an optional
artifact of the same emitter.

## A command-line target

A CLI is a registry entry over `Doc + Plan`, planned as an extension point beyond the milestones
([milestones](milestones.md)) rather than a milestone target, since nothing in the first targets
depends on it. What it needs is almost all IR and plan facts:

| CLI behavior | Read from |
|---|---|
| `--dry-run` on a call that changes something; no confirmation on a safe one | `OpPlan.Effects` |
| A confirmation before a destructive call | the `Destructive` hint, which no method implies |
| Streamed output for a list | `OpPlan.Page` |
| Choices and defaults on a flag | `Enum`, `Parameter.Default`, `Property.Default` |
| Body fields as flags, with the path to put each back | the request `Payload`'s model, by `PropID`, and each `TypePlan.Undeclared` name, typed as its catch-all |
| Flags that exclude or require each other | the branches of a request body's `Union` |
| A flag whose value is never echoed | `Property.Secret`, `Parameter.Secret`, `TypeCommon.Sensitive` |
| Completing an ID from a list call | `ResourceInfo` and its collection operations |

A CLI's help reads a description as the pages do, through the same tokenizer and link rule, and
writes it as plain text: a listed element as its text, every other token, a placeholder such as
`<key>` or a comment such as `<!--sse-->` among them, as written, a link as its text followed by its
URL, and every control character but a line feed and a tab, a terminal escape sequence among them,
removed, so its lines and a code block's stay as written, since a description could otherwise drive
the user's terminal. Command names, positional arguments, `@file` inputs and hidden commands are
presentation, so they are [shaping hints](contract/shaping-hints.md) keyed by ID, `Rename` and the
`Extra` members a CLI target defines. Flag constraints are derived at refine from the union's
branches by `PropID`, never by a property's leaf name: a leaf name derives no conflict between
`{origin: {host}}` and `{proxy: {host}}`, and lets two branches' `host` merge into one flag. Flag
names collide as identifiers do and are told apart by the same ID-keyed resolution
([naming](refine/naming.md)), so `foo_bar` and a nested `foo.bar` do not both become `--foo-bar`. A
body field nested at any depth, or with `.` in its name, is a flag like any other, save past a
type's first recurrence along its path, where the field is one flag taking JSON or an `@file`, since
a recursive body has no last field (17 of the golden spec's operations send one), and a union beside
properties, or a top-level `anyOf` body, still yields flags, its branches' constraints among them.
