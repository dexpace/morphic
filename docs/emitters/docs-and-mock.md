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
  arm (`Input/Output/Error`) correctly. A union, a model narrowing one ([what the docs
  render](#what-the-docs-render)) or an enum `TypePlan.Open` marks is documented as one whose value
  in a response may be a variant or a value the document does not list, which the SDK keeps, while a
  request carries only what the document lists unless the IR declares the type open ([plan
  policy](plan/policy.md#a-union-keeps-a-variant-it-does-not-know)), a union inclusive in a shape,
  or a model narrowing it there, as one whose value may be several variants at once, and every
  `PlannedError` and `PlannedResponse` as the body and headers it declares. An enveloped response is
  documented as the `Type` a method returns and the `Envelope.Body` the wire carries, and an example
  of it as both: the member at `Value` of the response's example for the first, and that example
  whole for the second. An operation is documented as safe or idempotent where its `Effects` say so.
  Its README is returned as blocks a user writes around ([write /
  integrate](write-integrate.md#a-document-the-user-writes-around)). No runtime `Policy` is
  consulted — proving the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`, the content `Return` elects
  and each response content's [example set](#one-set-of-examples) for canned responses, synthesizing
  none of its own, so with `Synthesize` off its streams and page walks hold only values the document
  declares, bar the links and totals a walk states of itself, sending the whole `Envelope.Body`
  where a response is enveloped, `Others` to answer as each declared response does, the error
  taxonomy to simulate declared failures, the declared `Closed` of a union or an enum to say whether
  a request may carry a variant or a value the document does not list, `TypePlan.Composed` to read a
  body narrowing a union ([what a mock accepts](#what-a-mock-accepts)), `TypePlan.Strict` to refuse
  a `oneOf` value as the SDK does, the binding it lowers ([bindings](plan/bindings.md)) to route
  requests, `Stream` to answer each stream as its sides frame it and `Page` to end each page walk a
  pager makes ([the mock's streams and page walks](#the-mocks-streams-and-page-walks)), and the
  visibility projections to honor the same wire shapes an SDK sends/receives. Refine lowers into a
  server-routing AST (routes from `HTTPBinding.URITemplate`, handlers validating against
  constraints). Because the same plan feeds SDK and mock, the two are **wire-consistent by
  construction** — the mock can *be* the interception target the SDK's wire-conformance harness
  ([mandatory tests](testing/mandatory-tests.md)) is diffed against. A response content whose
  example set is empty, as one can be with `Synthesize` off, is a warning
  `emitters/mock/example-missing`, never a body it invents. What it logs of a request is what an
  SDK's event holds of one ([auth](operations/auth.md#where-a-credential-can-be-read)), so no header
  and no body. It sends a webhook or a callback rather than routing one, as [operations the API
  sends](operations/inbound.md) says, and the docs give each a page of its own there.
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
table, which names each declaration an SDK gives them ([the declaration a shape
takes](plan/types.md#the-declaration-a-shape-takes)). So a write-only property is in no read shape's
table and a read-only one in no create shape's, and a model one operation creates and another
patches has a table for each where the patch relaxes its members ([what a patch
relaxes](plan/types.md#what-a-patch-relaxes)) or a member is visible only at creation. A direction
alone would merge those two. A model whose own members and those of the models it holds carry no
visibility marker has one table for every shape, a patch's apart only where it or a model it holds
requires a member or gives one a default, since a patch relaxes what is required and carries no
default onto the wire, so its table shows none, and on the golden spec (spec @57b5196b, morphic
@ab953d45) 15,694 of 17,872 models carry none on their own members or on any model they hold or
inherit, so a table per shape would repeat itself for any of them two shapes but a patch reach. A
type no shape reaches, one no operation sends or returns, is rendered once, unprojected, its table
anchored by its model's anchor alone and shown through no shape, which `Shown` takes as a nil
`ModelShape`, as an SDK declares it ([a shape reaches what it
holds](plan/types.md#a-shape-reaches-what-it-holds)).

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
named `create` beside a create table would; a shape's words are its key's, its lifecycles and its
patch ([the declaration a shape takes](plan/types.md#the-declaration-a-shape-takes)). A switcher
links a model's page in another version, never one of its tables, since a table's anchor is resolved
within its own page and a version's page may resolve it otherwise, and links the page holding an
anonymous model's table where that version gives the model no page of its own, and a link to the
model names the shape it shows, so an operation's patch body links to the patch table, never to one
marking required what the patch relaxes. An anonymous model another composes in place is no table of
its own: its members are rows of the composing model's table, anchored there, so no member is
anchored twice.

A model narrowing a union is rendered as the union it narrows, as the SDK targets declare it
([inheritance](refine/models.md#inheritance)): a table for each entry of its `TypePlan.Composed`, by
the rules above with the entry's `Flattened`, `Required`, `Undeclared` and `Shapes` in place of the
model's, so each lists every member an instance of that shape carries, as the entry's flattening
takes it ([TypePlan](plan/types.md#a-model-narrowing-a-union)). A table names the variants its
entry's `Over` lists, each linked where it is rendered, and the tags routing to it where its unions
are discriminated, its anchor named from the model, that `Over` and its shape, and the model is
documented as its union is (above). A member is anchored where the model declaring it is rendered,
one the model declares in the first of its tables that holds it, in `Composed`'s order and then
`Shapes`', and one a variant declares where that variant is rendered, which the table links as a
subtype's links an inherited member; the rule for a member no table of its declaring model holds
reads these tables as the model's. An anonymous union no reference but the model's composition
reaches is rendered by those tables alone, so an anonymous variant of it no other reference reaches
is composed in place, no table of its own, named unlinked since no route reaches it, its members
anchored in the first of the model's tables holding them. A value of the model is redacted at each
member the `Redacted` of any entry admitting it lists, an inclusive value being several of them at
once ([a value several variants admit](refine/unions.md#a-value-several-variants-admit)), and whole
where a variant such an entry's `Over` lists is sensitive, as a union's variant is. A model adding
nothing to its union, which the SDK targets alias, has no table: it is shown as the union it stands
for, linked, on its page or, where it has none, where it is referenced. One the plan composes no
entry for otherwise, which they degrade, lists its flattening's members and names its union, linked.

One table of the model's own members headed by its union, linked, would be shorter: 107 rows for the
golden spec's 23 such models, where a table for each entry holds 1,671 in one shape (spec @bc6fad24,
morphic @ea70eba79). But in 16 of them the model redeclares a member a variant declares too.
`rulesets_ResponseRule` requires `id`, `action`, `enabled`, `expression` and `ref`, which each of
its union's 21 variants leaves optional, so a reader joining its table with a variant's would find
the variant's declaration where an instance takes the model's, and no page would show a shape the
SDK declares.

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
filled ([below](#one-set-of-examples)). A value an SDK redacts
([redaction](refine/redaction.md#a-redacted-value)) is redacted in docs too, so pages, agent
documents and samples show a placeholder in its place, whether the value was declared or
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

## What a mock accepts

A handler reads a request body as the SDK's types decode it in the shape its operation sends it in
([a shape reaches what it holds](plan/types.md#a-shape-reaches-what-it-holds)), and holds it to what
`Build` holds a declared example to ([below](#one-set-of-examples)): admission in that shape
([unions](refine/unions.md#a-value-several-variants-admit)), then the constraints of every
declaration of each member the value lands on, the one its flattening takes and those that one
overrides. A member's type is the taken declaration's, but an instance carries what each declaration
asks of it ([the flattening](../ir/models.md#the-flattening)): the third variant of the golden
spec's `realtimekit_StorageConfig` redeclares `access_key` with no bound over a declaration bounding
it to one character at least, and a handler reading the taken one alone accepts `""` (spec
@bc6fad24, morphic @fa253fe19). So an example the plan keeps for a request is a body the mock
accepts, and a body the mock refuses is one the plan would refuse as an example.

- **A union** is read by its `TypePlan.Union`: a discriminated one by the variant its tag routes to,
  a tag naming none taking the order [unions](refine/unions.md#a-value-no-variant-recognizes) gives,
  and an untagged one by the variants admitting the value, which is refused where two do and the
  union is `Strict`, and held by each where the shape's `ModelShape.Inclusive` is set.
- **A model narrowing a union** is read as that union, each entry of its `TypePlan.Composed` a
  variant ([inheritance](refine/models.md#inheritance)). The tag routes to the entry over the
  variant it names, through each union the entry's `Over` passes, and the body is held to the
  members the entry's `Shapes` hold and require in the request's shape and to the constraints of
  every declaration of each member its `Flattened` holds, taken and overridden. The model's own
  `Flattened` holds only what it adds, so a handler reading it alone accepts what the API refuses.
  The golden spec's `rulesets_Rule` request body adds `position` to a `oneOf` of 21 rules that
  `action` routes, and the rule `block` routes to bounds a response's `status_code` to 400 through
  499 and requires `content` and `content_type` beside it, none of which the body's own member says
  (spec @bc6fad24, morphic @fa253fe19). There 15 models a client writes narrow a union beside a
  member of their own, 3 of them over a discriminated one.
- **What the document lists is all a request carries.** A handler refuses a variant or an enum value
  the document does not list wherever the IR declares the type closed, whatever `TypePlan.Open`
  says: `Open` is what a client survives in a response, and the API accepts what `Closed` states
  ([plan policy](plan/policy.md#a-union-keeps-a-variant-it-does-not-know)). A mock built to accept
  what the SDK keeps would pass a test sending a member the API has never listed.

A handler reads a body once and tries an entry at most once, so a body narrowing an untagged union
costs no more than its size times the entries its model composes, which `plan.MaxComposedSize`
bounds when the plan is built ([TypePlan](plan/types.md#a-model-narrowing-a-union)), and one a tag
routes is read by one entry. T-57 holds these rules.

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
  canonical words, which the SDKs resolve as well. Two slugs collide where they fold alike, as
  artifact paths do ([the path fold](emit/rules.md)), since a file system that ignores case keeps
  one page of the two: two of the golden spec's groups, in its one service, are `Webhooks` and
  `webhooks`, which slugs spelt as the source spells them would part by case alone. A slug that is a
  Windows device name, which no page's path may hold ([emit rules](emit/rules.md)), is told apart as
  a colliding one is, and a slug is held to the length cap of a name a path holds
  ([naming](refine/naming.md#a-name-too-long-for-a-path-is-shortened)).
- **A total route plan.** Every `ServiceID`, `GroupID`, `OpID`, `ChannelID`, `MessageID` and
  `AuthID` and every type with a page gets a route, every anonymous model rendered as a table on
  another's page an anchor there, every table of a model narrowing a union an anchor where the model
  is rendered, and every `PropID` a model declares an anchor where that model is
  rendered (above), save one no table holds, such as a write-only member of a model only responses
  reach, which is rendered nowhere, a decorator answering with its `Alias`'s. A `Property` no model
  declares, a header or a resource's identifier or state (`ResourceInfo`), is a row where its holder
  is rendered and takes no anchor, since no link names one. Anything left without one is an error
  `emitters/docs/route-missing`, and a recursive type is linked by its ID, never inlined to a depth
  and cut.
- **Collisions are refused.** Two entities whose routes resolve to one URL, or to two that fold
  alike ([the path fold](emit/rules.md)), are an error `emitters/docs/route-collision` naming both,
  never a page that overwrites another and never a page dropped. The check runs on the URLs the
  templates produce, over every route a run plans, its table's and its agent documents' together, so
  no template can reintroduce a collision the catalog did not have.
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
  versions. Each snapshot's run writes under the directory its label names, two that fold alike set
  apart by a suffix on the later, as the contract has a cross-version target's do ([contract
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
  no table, though the collision check reads it. A label's directory is percent-encoded where a link
  writes it, since a `#` or `%` in it would otherwise end or alter the URL, and a switcher shows the
  label as written though its directory takes a suffix. A switcher lists the labels `Routes` keys,
  in the order `Document.Versions` gives them, never one no snapshot was made for. A run finds its
  own entry under `EmitterInput.Snapshot`, the label of the snapshot it was handed, which is also
  the version its switcher marks current: nothing in a snapshot's document says which version it is.
  The engine cannot plan the routes itself, since the catalog and the templates are the target's. A
  run plans its own routes by the same pure function, so its pages agree with its snapshot's entry.
  A table holds each entity's page and anchor from the output root, its snapshot's directory
  included, which is what a switcher links. What spans versions, the site-scope `llms.txt` linking
  each version's and an index of the versions, is written once, by the run of the last of the labels
  `Routes` keys in `Document.Versions` order, which every snapshot keeps whole (architecture.md
  §2.2), since every run holds the labels and tables it is built from; those pages sit at the output
  root and belong to the one generation, so they keep their place when another label becomes last,
  and the collision check reads them against every label's directory, so a template putting one
  under a label's name is refused. The `""` snapshot's run writes the site scope, which is then its
  version's too, as one index. A link to an entity a version lacks resolves to that version's page
  saying so. Slugs may differ between versions; IDs do not.

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
them, and, when the run names it too, runs it once with its artifacts, on a sliced document on the
one snapshot such a target writes ([the contract](contract/emitter.md)). An SDK so written holds
that one version, and a call spelled from another may not exist in it, so on a document stating
versions each page shows a sample under its own snapshot's label, as the version of the SDK that
spells the call. The label is on every version's pages, the written one's among them, since a docs
target cannot tell which versions' SDKs are published: a pipeline running once per label publishes
each, and a run naming a target for samples alone names no version. Showing samples on the written
snapshot's pages alone was rejected for dropping what such a pipeline ships, and labelling only the
others for making a docs page depend on which targets share its run (T-69). A run made for samples
alone reports what any run of that target reports, and the engine decides as for any run whether
that is fatal: the docs show that SDK's calls, so an SDK that fails to generate fails the docs
showing it. A target returns a sample for every operation its callers call, whatever its `Scope`,
which gates its artifacts alone, since its names are the whole document's: a scoped SDK run's docs
keep every sample. It returns none, with no diagnostic, for an operation the API starts, a webhook,
a callback or a message the client receives, since no call spells it. A docs generation is a new one
whenever the samples it is handed change, as an SDK target's own options can change them. A sample a
target cannot render is a warning `emitters/<target>/sample-unrendered`, never an empty code block
on the page, and so is one for an operation whose required input holds no example. A sample shows a
call that succeeds, so its values are the `Input` of its operation's first success scenario (below):
one ending in no `Error`, since an `Error` scenario's input is chosen to fail, and perhaps in no
`Output`, as an operation answering with no body does. Its values are shown through the shape the
operation sends them in, and where `Shown` cannot complete them the sample takes the next success
scenario. That is one the operation declares in `Operation.Examples`, as a Smithy or TypeSpec
document can, else the one the plan assembles. An operation with neither has no sample: where
`Assemble` is off it draws no diagnostic, whatever its inputs hold, since the policy chose it, and
where `Assemble` is on and a required input holding no example kept one from being assembled, it
draws the warning above. A redacted value is written as the target's placeholder for it (above).

## One set of examples

READMEs, samples, generated tests and mock responses all need example values, and each would
synthesize its own where the document declares none, so the docs would show values the SDK's own
tests never sent, and every regeneration could change them. So the plan holds one set,
`Plan.Examples`, keyed by the ID of each type, property (a header among them), parameter, enum
member and operation:

- **Declared first.** An entity's own `Examples` are its set, labelled authored in the docs, save
  one holding an `ExternalURL` and no value, which the docs show as a link and which no set holds,
  since no value of it can be checked, shown, filled from or sent; each checked against its type and
  constraints, a header's as the value it is and never through the readings a header's text admits
  ([values as text](refine/text.md#reading-a-text)), a member's being those of every declaration its
  flattening takes or overrides, as a synthesized value's are (below), with visibility set aside,
  since the shape it is shown through settles that: a value no shape could make valid, such as one
  its enum does not list, is refused, an enum the document leaves open (`Enum.Closed` false)
  admitting a value it does not list. Shown through a shape, an example drops the members that shape
  does not hold, keeping a key no member declares that the model's catch-all admits, checked against
  the catch-all's type, and takes, for each member the shape requires and the example lacks, the
  value a synthesized model gives that member (below), an `Undeclared` name's from its catch-all
  type's set, which marks what is shown `Inferred` and labels it generated, its declared members
  kept as written; with `Assemble` off it fills nothing, and an example missing a member the shape
  requires gives way as one it cannot complete does. So a response-form example serves a create
  shape, a patch body's may leave out a required member, and, with `Assemble` on, every required
  member's set holding a value and no `Strict` union left ambiguous by the shape, no shape is left
  without one. `Build` refuses a union value several variants admit where the union is
  `TypePlan.Strict`, which decodes it as no variant, reporting it once as above, and a value of a
  discriminated union that the variant its tag routes to does not admit, whichever other variant
  does, since decoding reads it by its tag
  ([unions](refine/unions.md#the-tag-a-variants-value-carries)), and `Shown` shows such a value
  elsewhere through all of them, keeping each member one of them holds in the shape, since an
  inclusive value may be several variants at once. A shape can make a value two variants of a
  `Strict` union admit, by dropping the member that kept one of them out, so `Shown` checks
  admission again in the shape it shows and is false there, as for a member it cannot fill. A model
  narrowing a union is checked and shown as that union, the entries of its `TypePlan.Composed` its
  variants, each in its entry's shape and filled from what that entry requires there, strict and
  inclusive where the union is ([TypePlan](plan/types.md#a-model-narrowing-a-union)), so a value no
  entry admits is refused, and a model adding nothing to its union as that union. `plan.Shown` does
  this, the one projection every consumer reads an example through, reading `Assemble` from
  `Plan.Policy.Examples`, and returns false for an example it cannot complete, a required member's
  set being empty, so the consumer takes the set's next one; with none left the position has no
  example there, which the docs show as none, a sample reports as `sample-unrendered` and the mock
  as `example-missing`. One they refuse is reported as a warning `emitters/plan/example-refused` and
  left out, so every consumer falls to the next set alike: an SDK's test sending it would be refused
  by the mock, as by the API. On the golden spec (spec @57b5196b, morphic @ab953d45), a declared
  example fails its own type in at least 295 types: in 292 no member of the enum, each closed, holds
  it, such as `json` against `JSON` and `CSV`, and in 3 it is longer than `maxLength`. One declaring
  none that adds no constraint to its type's takes its type's set, so a property shows the value its
  type does, save an enum member, which takes its own value. A position whose type is a decorator
  takes its own examples, else the nearest decorator's along the chain, as `CollapseAliases` folds
  them, else, where it adds no constraint, the set of the type its `Alias` names.
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
  name among them, the first value of that member's own set, and then of the set of each declaration
  its flattening overrides, in the order the walk reads them, that every declaration of the member
  admits, else the first of its own set, as the declaration a flattening takes governs where its
  declarations admit no value in common ([the flattening](../ir/models.md#the-flattening)): each of
  the golden spec's 355 error bodies composing a success shape and then a failure schema keeps the
  failure's `success: false`, which the success shape's `true` admits no value in common with (spec
  @bc6fad24, morphic @ea70eba79). A list or map holds, for its k-th item or entry, the k-th, its
  set's values taken in turn and, once they run out, synthesized values seeded by index, so items
  differ where `uniqueItems` or a map's keys require it, a map's keys being distinct values of its
  key type; a `uniqueItems` list holds no more items than distinct values exist, and one left below
  its minimum is unsynthesizable. Each such value is a declared one whole, or a synthesized one cut
  to the depth the bound leaves, so a property's value is the same alone and inside its model
  wherever the bound leaves it whole and no other declaration of it refuses it, and an optional
  recursive member ends. The bounds cut only what validity leaves optional ([plan
  policy](plan/policy.md)): an optional member more than `ExamplePolicy.MaxDepth` models deep, and
  the entries of a list or map past the larger of `MaxItems` and its minimum, or past its minimum
  alone once deeper than that or on a recurrence of its own type along the value, so a recursion
  through no model, as the golden spec's `cws_JSONValue` map and list recur, ends too, so a required
  list of its own model ends: the golden spec's `cloudforce-one_TreeNode` requires `children`, a
  list of `TreeNode`, which holds none past that depth. A required member is filled as deep as its
  schema requires, one whose reference is `Nullable` ending a cycle with `null`, and a `minItems`
  above `MaxItems` is met: on the golden spec 250 models reach a model four or more deep through
  their own or inherited required, non-null members, and one list requires four items, which the
  default bounds would otherwise leave without an example. A value the synthesizer cannot make
  valid, as a `pattern` it cannot satisfy or a required cycle no value ends, or one holding more
  values than `plan.MaxExampleValues`, a fixed limit the plan package states, 10,000, which bounds
  what a hostile schema can make one example cost, is reported as a warning
  `emitters/plan/example-unsynthesized` and left out, never emitted invalid. A model narrowing a
  union is synthesized as a value of one entry of its `TypePlan.Composed`, chosen as a union's
  variant is (below), holding what that entry's flattening holds as a model's value does, and a
  model adding nothing to its union takes the union's set where it adds no constraint to it, and is
  otherwise synthesized as that union. So `rulesets_ResponseRule`'s value is one of its union's
  rules carrying the five members it requires, its `action`, which the model redeclares as a plain
  string, taking the value the variant's own declaration pins, the tag routing to that variant. A
  value synthesized as a variant of a discriminated union, whatever unions lie between, holds at the
  tag's member one of that variant's own tags
  ([unions](refine/unions.md#the-tag-a-variants-value-carries)): the first of them in the member's
  own set where its declarations pin one, and otherwise the first in the byte order of their texts,
  as a value of the member's type, and `plan.Shown` fills a tag a shape requires and an example
  lacks the same way. A variant with no own tag takes the first value of its member's set that
  routes to no variant, and has no example where the member is required and its set holds none,
  which is reported as `emitters/plan/example-unsynthesized`. So a synthesized `nsc_Interconnect`
  carries `direct` or `gcp_partner` as its `type`, which its variants leave a plain string, and a
  value of either variant of `infra_ServiceConfig`, whose shared base lists both tags, the one
  routing to it.
- **Stable.** Each choice the synthesizer makes, such as which enum member, which variant or which
  entry of a model narrowing a union, is seeded from the entity's [correlation
  key](../ir/derived-facts.md#correlation-keys), or from its ID where no key identifies it, save on
  a recurrence of its type along the value or deeper than `ExamplePolicy.MaxDepth`, where it takes
  the first variant, entry or member, in source order, from which a value ends. So a required
  recursive union ends: the golden spec's `cloudforce-one_EmailRuleCondition` reaches itself through
  a variant requiring a non-empty list of it. So a value is the same in either declaration order,
  and on every regeneration until the entity's schema, its key, or its ID where it seeds from one,
  the example policy, `irfacts.CorrelationVersion` or the synthesizer's code changes.
- **Shared** by the docs emitter, the samples, each SDK's tests and the mock server, none of which
  synthesizes its own.

The manifest need not persist them: a seeded synthesizer reproduces each value from its inputs. A
value still moves when its entity's key or `irfacts.CorrelationVersion` does though its schema
stays, as a renamed schema's would, and a persisted value would move with it, since it could be
found again only by that key or by an ID the rename moves too, so persisting would add state without
saving that churn.

## The mock's streams and page walks

An operation whose answer is no single body, a stream (`OpPlan.Stream`) or a page of a walk
(`OpPlan.Page`), is answered so that each method an SDK generates for it meets the shape it asks for
and ends, through the code a real server's answer takes. Each frame and page is a function of the
document, the plan's policy, the mock's options and the request alone, its values drawn from the
[example set](#one-set-of-examples), so the mock keeps no state between calls and answers one
request with the same bytes on every run (INV7). The [behavior server](testing/behavior-server.md)
is no second copy of these rules: it is written by hand so that a test can disagree with the
generator, which a mock generated from the same plan cannot. T-66 and T-67 run an SDK's streams and
pagers against the mock, which shows the two read the plan alike; T-14's routes show the plan reads
the API rightly.

### What the mock streams

- **The shape asked for.** Where a side has a `Condition`, a request asks for its stream when the
  input the condition names holds its value, and otherwise for the other content, whatever its
  `Accept` says, since that input is what the API reads. Otherwise it asks by `Accept`: each content
  of the primary response and of the response declaring `Recv`'s takes the `q` of the most specific
  range matching it, a media type before `text/*` before `*/*` (RFC 9110 §12.5.1), compared as [one
  election](plan/responses.md#one-election) compares, one taking `q=0` is excluded, and the mock
  answers with the content of highest `q`, a tie going to the content `Return` elects, else to the
  first declared, and with the content `Return` elects where none is left or `Accept` is absent,
  each with the status and headers of the response declaring it. So the value method and the stream
  method of one operation ([streaming](operations/streaming.md)) each get what they read, and
  answering every request with the content `Return` elects would give the stream method a JSON body.
- **What the frames hold.** A side the mock writes sends, in order, its elements; one value of the
  first variant, in source order, whose `Variant.Event.Terminal` is true, where `Element` is a union
  with one and the last element holds none; its `Terminator` as one frame's data, where it is set;
  and then the body ends, the mock never holding a stream open. An `Initial` message is no frame
  ([streams](plan/streams.md)). The elements are the content's declared examples, read through
  `ContentExamples`: each item of a list value is a frame where `Element` is the `Elem` of the
  content's list `Type`, a string value on a side with no `Element` is the body's text and is
  written as it stands, since the schema describes the whole body (OpenAPI 3.2 §4.14.3), closed by a
  line feed under `json_lines` and a blank line under `sse` where it does not end with one, so a
  frame after it stays apart, and any other value `Element` admits is one frame. With none declared,
  a side has `MaxItems` frames, the k-th the k-th value of the set of `Element`, or of the content's
  `Type` where it has none, then one synthesized seeded by index, as a list's k-th item is. With
  `Synthesize` off the mock makes none: a side has as many of those values as the set holds, up to
  `MaxItems`, and where it holds none sends no element and draws `emitters/mock/example-missing`,
  and a terminal variant whose set is empty is not sent, the body's end ending the stream. A
  synthesized element holding a terminal variant before the last is left out, since the SDK would
  end at it; a declared one stays, since the document says the stream ends there.
- **How each is framed.** `sse` writes one event per frame, closed by a blank line, its data as one
  `data:` line per line it holds, its lines split at each CR, LF and CRLF as a reader splits them
  (WHATWG HTML §9.2.5), so a value holding a line feed arrives whole and one holding a carriage
  return arrives with a line feed in its place, which an event stream cannot carry. Each field is
  written with one space after its colon, which a reader strips, so a line starting with a space
  keeps it. Under `FrameMembers` the element's `event`, `id` and `retry` are those fields and its
  `data` the data, one with no `data` writing an empty `data:` line, since a reader dispatches no
  event without one (§9.2.6), and an element whose `event`, `id` or `retry` holds a CR or an LF,
  which would end that field and forge the next, is not sent, as no server can send it; otherwise
  the data is the element's encoding, or its text where its root is `string`. `json_lines` writes
  each frame's encoding and a line feed, its reader ending a line at an LF alone
  ([streaming](operations/streaming.md#what-a-target-generates)), so a text frame arrives as one
  frame per line it holds, a CR before an LF dropped, a lone CR kept, and a blank line none, the
  mock writing no frame that is empty. `json_seq` writes a record separator, the encoding and a line
  feed (RFC 7464). `multipart` writes a part per frame, typed as the side's `ItemEncoding` says and
  otherwise by its element's codec, under a boundary derived from the parts' bytes and lengthened
  until no part holds it. `bytes` sends the content's value as the body, a file having no frames.
  `protocol` frames are another protocol's own, and the mock covers `http` alone, so it routes
  `HTTP[0]`, as an HTTP SDK lowers, and an operation with no HTTP binding has no route there and
  draws a warning `emitters/mock/unsupported-binding`, the code every target draws for it
  ([bindings](plan/bindings.md)), never an answer its SDK would not read.
- **A side the client writes.** The mock reads it as the SDK writes it: by its `Framing`, each frame
  validated against `Element` as a body is, until the body ends or a frame whose data is its
  `Terminator` arrives, after which a frame is refused; a `bytes` side is read to its end; and one
  whose `RequiresLength` is set is refused (411) when its body arrives with no length, as the API
  would refuse it. It then answers as for any call, as `Return` reads, a server answering once it
  has read the request. It refuses at once a frame that fails validation, or the body past the bound
  below, the frame that bound cuts refused as too large (413), never as one that does not decode.
- **Both sides.** A `bidi` operation's sides run at once, neither waiting on the other: the mock
  writes its own as a server stream, the same bytes whatever the client sends, since its frames
  depend on none of the request's, and beside it reads the client's side and discards it, validating
  no frame. Its response then ends as each version lets a server end one while the request is open.
  Over HTTP/1.1 it states its length, a full-duplex response (Go's
  `http.ResponseController.EnableFullDuplex`) whose handler returns only once the client's side has
  ended, so the connection never closes on bytes the mock has not read, which would reset it and
  lose a response its caller has not yet read. Over HTTP/2, which the mock serves on both its
  listeners ([its options](#the-mocks-options)), the response ends once written, the read stopped
  before the handler returns and the stream reset with `NO_ERROR`, which asks the client to stop
  sending (RFC 9113 §8.1). Measured with Go 1.27.3's client, 20 runs of each caller and of sides up
  to 8 MiB on each version, and Ktor 3.2.3's CIO engine over HTTP/1.1, 10 of each, every caller read
  the mock's side whole on every run, where returning before the client's side ended over HTTP/1.1,
  as a server stream does, lost it for a Go caller that wrote 1.6 MiB first in 6 of 20 runs and
  failed its read in 8 more. Validating the client's frames was weighed and refused: a refusal after
  the status could only cut the response at a point timing chooses, one request giving 1, 2 or 3
  frames over 40 runs. Waiting for the client's side over HTTP/2 as well was refused too: there the
  response ends only as the handler returns, so a caller that reads it to its end before ending its
  own side would wait on a mock waiting on it. So was writing a frame for each frame read, since no
  IR fact pairs one side's frames with the other's, and reading the client's side to its end before
  writing, which stalls a caller that writes only after it reads. A caller reads before its side
  ends only over a transport that delivers a response while its request is still open: Go's
  `net/http` client does on both versions and Ktor's CIO engine on HTTP/1.1, the one it speaks,
  while the JDK's `HttpClient` 25, under Ktor's Java engine, does on neither, so on it only a caller
  that writes first can run. A caller's send after the mock's side has ended may fail at its own
  transport, as Go's does on both versions, which T-66 does not count against the mock.
- **What a caller controls.** Nothing in a request lengthens a stream: its frames are the declared
  examples or `MaxItems`, each value within `plan.MaxExampleValues`. A side the client writes is
  read to `MaxRequestBytes` in all and refused past it, and an exchange in which no byte of it moves
  for `IdleTimeout`, a caller that stops reading or one that never ends a side the mock reads to its
  end, is ended: over HTTP/2 its stream is reset and the connection, which other requests share,
  kept. On both versions the read stops before the handler returns, since a read's deadline set
  after it dereferenced nil in Go's HTTP/2 server. A `bidi` side past the bound is refused by
  closing the connection over HTTP/1.1, so a caller that wrote that much before reading may lose the
  mock's side, as 11 of 20 runs writing 20 MiB against a bound of 16 MiB did, its exchange failing
  either way. Both are [options of the mock](#the-mocks-options).

On the golden spec (spec @bc6fad24, morphic @fa253fe19) 13 operations stream. 8 the client reads: 6
offer `json_lines` text beside JSON, 2 of them declaring a three-line example, one elects it for its
one method beside CSV and Markdown no method asks for, and one offers `sse` frames of a model beside
JSON. 5 the client writes: one `json_lines` text and 4 `bytes` files. None is `bidi` or sets a
`Condition` or a `Terminator`, which no compiler fills yet (#1668). T-66 holds this section.

### A page walk on the mock

A paginated operation is answered as a walk a pager can finish: a finite list of items, cut into
pages the request sizes, each page read from the request alone. The same canned page for every call,
which the example set alone gives, never lets a walk end, since each page carries the same
continuation and the same items. Counting calls was weighed and refused: a retry, a second walk or a
test running beside it would move the count, and a request's page would turn on the calls before it.

- **The items.** A walk holds `2n` items, `n` being those the primary response's example holds at
  `ItemPath`, at least 1: a list of `ItemType` built as a synthesized list is (above), whose first
  `n` are the example's. So a walk that sends no limit has two full pages and follows one
  continuation. With `Synthesize` off the mock makes no value: an item the item type's set does not
  give is the example's own again, from its first, and a walk whose example and set hold no item is
  one page holding none.
- **A page.** A request's page holds the items from the position it asks for, as many as the limit
  it sends through `LimitIn`, else `n`, and none past the walk's end, and is the last where it holds
  the walk's last item, save where the mock mints the walk's end (below), or none. A limit below 1
  that the input admits sizes a page as no limit does, since a page of no items would end every walk
  at once and state no page count. Under `cursor` and `token` the position is the start where the
  request sends no continuation, else the one its continuation names: the mock mints distinct values
  valid against `CursorOut`'s type and `CursorIn`'s, built as a `uniqueItems` list's items are, the
  example's own first, the k-th naming the start of the walk's page k+1 at the request's size, so
  one value serves a walk of two pages at any size, or of one where the walk's end is minted
  (below), which takes a value too; where its type holds too few values, or with `Synthesize` off
  its set declares too few, the walk ends after the last page they reach. It refuses a value it did
  not mint. Under `offset` it is the offset sent, 0 where none is; under `page` the number sent less
  `Page.FirstPage`, times the page's size, the first page where none is sent. Each is read and
  computed at full precision, as a pager's are ([the numbers a walk reads and
  sends](operations/pagination.md#the-numbers-a-walk-reads-and-sends)), so a number whose position
  passes the walk's end gets a page holding none however large it is, as does one naming no position
  of the walk: a number below `FirstPage` or an offset below 0 that the input admits, or one holding
  a fraction. Under `next_link` and `link_header` the link names it: the request's own URL with one
  query parameter of the mock's, `mock_page` prefixed with `_` until no input an operation at that
  path declares is bound under its name, its value naming the operation, the position and the page's
  size, which a limit sent in the body would otherwise lose, a value naming none of the walk
  refused; an input owning the rest, as an exploded map does, holds no entry the mock reads as its
  own. The mock answers a `GET` carrying it as that page whatever the method of the operation that
  wrote it, since a pager follows a link with one ([what a pager sends
  next](operations/pagination.md#what-a-pager-sends-next)).
- **What a page says.** Each page but the last carries the continuation of the position after its
  last item, where the strategy reads one, at the place `CursorOut` reads it or in the `Link`
  header; every page states the walk's items where `TotalCount` reads them and its pages at the
  request's size where `TotalPages` does; each states the flag `HasMore` reads, true on every page
  but the last and false on the last; and the rest of each is the response's example, shown as any
  is. The last carries no continuation: its member absent where optional, else null where
  nullable; else the empty value where `no_next` is listed and its type admits one. Where the
  continuation is required and not nullable and neither of those ends the walk, the mock mints one
  more position, the walk's end, `total` with a total to read listed or not, since `total` counts
  what the walk read and a walk a caller begins past the first page never reaches it: the page
  holding the last item carries its continuation, and the page asking for it, the end page, holds
  none, which `empty` ends, and `short` where the request sent a limit. Ending a walk `total` ends
  on a value naming no page was weighed and refused: 5,208 walks begun at the second page ended on a
  refusal with no warning, under every `Stop` listing `total`, `[empty, total]` among them. The end
  page carries, where `Stop` is empty, the continuation its own request sent, a link the URL it was
  sent to, the target written as it arrived, so the repeat ends the walk however the caller spelt
  its query ([where a page walk ends](operations/pagination.md#where-a-page-walk-ends)), and
  otherwise a value the mock did not mint, refused when sent back, which a type holding too few
  values does not leave it (#1788). A repeat, an empty link among them, ends a walk only under an
  empty `Stop` and on a page holding no items, so the mock writes one only on the end page there or
  where `no_next` ends the walk, never where a pager would fetch the page again until its bound. A
  walk whose continuation is required and not nullable therefore ends on a refusal where no listed
  condition ends it first, and such an operation draws a warning `emitters/mock/page-walk-refused`,
  once, unless `Stop` is empty or lists `empty` or `no_next` over a type admitting the empty value.
  Leaving the member out all the same was weighed and refused: the SDK would fail to decode the last
  page and lose its items.
- **Why every walk ends.** Each continuation the mock writes names a position past the page's last
  item, save three that end the walk: the end page's repeat under an empty `Stop`, the empty value
  `no_next` ends on, and a value it did not mint, which it refuses. So a page holding none is the
  last and the walk is finite, and a pager sending [its next
  request](operations/pagination.md#what-a-pager-sends-next) reads each item once, in order, in at
  most one request more than the walk has items, and one more again where it ends on a refusal. A
  page but the last is full, so `short` ends nothing early; `total` is reached on the last data page
  by a walk begun at the first, `no_next` holds on it, and `empty` holds on the page past the end,
  where a walk under `offset` or `page` ends whatever `Stop` lists. Run over each strategy, each set
  of `Stop` conditions, a continuation optional, nullable and required, each total, both first
  pages, a `GET` and a `POST` operation, `n` of 1 to 3, one, two or unbounded declared cursor values
  and seven limits, 70,560 walks begun at the first page and 43,008 begun at the second with the
  continuation the mock wrote, a link's walks by a `GET`, a pager following these rules read each
  item from where it began once and ended within that bound, refused only where the warning was
  drawn; a link whose value named no size lost items in 1,824 of the first.
- **What a caller controls.** A limit sizes only its own page, which never holds more than the walk:
  a limit of a billion gets one page of the `2n` items, short and so the last, so a response's size
  is bounded by the document's example and never by the request. An offset or a page number past the
  end, however large, or one naming no position, gets a page holding none, and a continuation the
  mock did not mint a refusal. A limit, an offset or a number its input's constraints refuse is
  refused as any invalid input is.

The golden spec declares no `Pagination`: the OpenAPI compiler's policy reads each walk off the
names an operation uses ([`pagination-policy`](../compilers/openapi.md#pagination-policy)), `Stop`
listing `no_next` under `cursor` and `token`. By the names of their query parameters (spec
@bc6fad24, morphic @fa253fe19), its `GET`s take `page` in 254, `cursor` in 92, `offset` beside
`limit` in 37, and a token in 15, `page_token` in 13 and `continuation_token` and
`continuationToken` in one each, 13 of them taking two of those; two `POST`s take `page`. None
declares a `Link` header, and 15 return an optional, nullable `next` string in `result_info`, 12 of
them `format: uri`. Of the 92, 12 require the `cursor` that `result_info` returns, 8 as a string
that is not nullable, whose walks the empty cursor ends cleanly under `no_next` and the end position
under `empty`. T-67 holds this section.

## The mock's options

The mock target's options ([target options](contract/options.md)) are what bounds the work a request
can make it do, and these are all of them, with their defaults at its first version. A zero or
negative number or duration is the strictest value of its option, never one that turns its bound
off: a body of no bytes admitted, an exchange or a send ended at once, no send in flight; and an
empty address listens on none.

```go
type Options struct {
    MaxRequestBytes int64    // 16 MiB: a request body read in all, a streamed side or any other;
                             // past it the request is refused (413) and read no further
    IdleTimeout     Duration // 30s: an exchange in which no byte of it moves is ended, and a
                             // connection whose request's head takes longer or that idles longer
    Address         string   // "127.0.0.1:0": where the API listens, HTTP/1.1 and HTTP/2 with
                             // prior knowledge (RFC 9113 §3.3)
    TLSAddress      string   // "127.0.0.1:0": where the API listens over TLS, HTTP/2 and HTTP/1.1
                             // by ALPN, under a certificate the mock makes when it starts
    ControlAddress  string   // "127.0.0.1:0": where the control route listens, apart from the API
    SendTo          []string // "127.0.0.0/8", "::1/128": the addresses a webhook or a callback
                             // may be sent to; empty sends none
    MaxSends        int      // 8: webhooks and callbacks in flight at once
    SendTimeout     Duration // 10s: one send, from dialling to its answer's status
}
```

The control route ([operations the API sends](operations/inbound.md#the-mock-server-and-the-docs))
listens at `ControlAddress`, apart from the API's listeners, which the mock states when it starts
beside the API's addresses and its certificate, as PEM: a route on the API's listener would have to
miss every declared route, and a template whose first segment is a variable, such as
`/{tenant}/items`, matches any path a prefix could choose. A `POST` to `/webhooks/` and a webhook's
`WebhookName`, percent-encoded, sends that webhook to the URL its body's text names, and a name the
document lacks is refused (404).

The TLS listener is how a transport that offers HTTP/2 only by ALPN reaches HTTP/2: the JDK's
`HttpClient` 25, asked for HTTP/2 over cleartext, sent an upgrade the mock does not take and spoke
HTTP/1.1, saying nothing, and over the TLS listener negotiated HTTP/2, while Ktor's CIO engine
speaks HTTP/1.1 alone. So a test trusting the certificate the mock states asserts the version its
transport negotiated, and one that fell back to HTTP/1.1 fails rather than passing as the HTTP/2
case.

Three costs are left to the server the mock runs in. The number of connections is the operating
system's to bound, as the mock serves a test's own process, on loopback by default. The size of a
request's head is the server's, Go's `net/http` at 1 MB by default, which is what bounds the pairs a
query or a cookie brings an input no style arranges ([values as
text](refine/text.md#a-value-no-style-arranges)). A reader taking a byte within each `IdleTimeout`
keeps its exchange open as long as the response lasts, which costs the mock one connection and its
buffers for a response the document's examples bound. A connection holding no request, or part of
one, is closed after `IdleTimeout`, the bound on a request's head and on a connection idle between
requests: 200 such connections were all closed within 5 times it, and with neither bound all stayed
open.

A webhook or a callback is sent only to an `http` or `https` URL whose host resolves to an address
`SendTo` lists, checked on the connection the mock dials, through no proxy, so a name resolving
elsewhere by then is refused too: with a proxy from the environment, the connection dialled was the
proxy's, on loopback, and the proxy sent a `POST` to a host outside the list. Any other is left
unsent and logged, as the webhook's name or the callback's operation and expression, the URL as an
event shows one ([what an event holds](contract/runtime-policy.md#what-an-event-holds)) and the
bound that refused it, the request naming it answered all the same: a callback's URL comes from the
request the mock answers, so without the list a request could make the mock call an address of its
caller's choosing, and loopback serves a test's receiver, which runs beside it. A send that finds
`MaxSends` in flight is left unsent and logged, never queued, so a burst of requests costs at most
that many connections at once, where with no such bound 200 requests naming one loopback URL drew
200 sends at once. Each send gives up past `SendTimeout`, follows no redirect and reads its answer's
status alone. T-66, T-67 and T-71 hold these bounds.

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
| Body fields as flags, with the path to put each back | the request `Payload`'s model, by `PropID`, and each `TypePlan.Undeclared` name, typed as its catch-all, and for a model narrowing a union, the `PropID`s its `TypePlan.Composed` entries hold |
| Flags that exclude or require each other | the branches of a request body's `Union`, or the entries of a body narrowing one |
| A flag whose value is never echoed | `TypePlan.Redacted`, `TypePlan.Sensitive`, `Parameter.Secret` ([redaction](refine/redaction.md#a-redacted-value)), and for a body narrowing a union each `PropID` the `Redacted` of one of its `TypePlan.Composed` entries lists |
| A command's help naming its tier and what it means, and an `alpha` command run only with a flag acknowledging it | `OpPlan.Stability` ([stability](operations/stability.md)) |
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
