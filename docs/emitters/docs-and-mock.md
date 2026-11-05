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
  and each response content's [example set](#one-set-of-examples) for canned responses,
  synthesizing none of its own, sending the whole `Envelope.Body` where a response is enveloped,
  `Others` to answer as each declared response does, the error taxonomy to simulate declared
  failures, `TypePlan.Open` to say whether a variant or an enum value the document does not list
  may be sent, `TypePlan.Strict` to refuse a `oneOf` value as the SDK does, the `BindingView` to
  route requests, and the visibility projections to honor the same wire shapes an SDK
  sends/receives. Refine lowers into a server-routing AST (routes from `HTTPBinding.URITemplate`,
  handlers validating against constraints). Because the same plan feeds SDK and mock, the two are
  **wire-consistent by construction** — the mock can *be* the interception target the SDK's
  wire-conformance harness ([mandatory tests](testing/mandatory-tests.md)) is diffed against. A
  response content whose example set is empty, as one can be with `Synthesize` off, is a coded
  diagnostic of the mock's, never a body it invents.
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
the docs needs every answer the API gives, so the docs emitter renders every `Response`, every
`ErrorCase`, the `Default` that answers the statuses they do not name, and every `Content` each
declares, the primary response first and the elected content first within each, and those choices
order the page and decide nothing else. A type is rendered once for each projection its
`TypePlan.Shapes` gives it ([ModelShape](plan/types.md#modelshape--visibility-projection)), each
table holding the members visible in that shape and marking those it requires, and shapes whose
projections coincide share one table, as they share one declaration
([models](refine/models.md#visibility-projections--n-wire-shapes)). So a write-only property is in
no read shape's table and a read-only one in no create shape's, and a model one operation creates
and another patches has a table for each, since a patch body's members are optional and a member
visible only at creation is absent from it. A direction alone would merge those two. A model whose
own members and those of the models it holds carry no visibility marker has one table for every
shape but a patch, and on the golden spec (spec @57b5196b, morphic @ab953d45) 17,305 of 17,872
models carry none on their own members, so a table per shape would repeat itself for any of them two
shapes but a patch reach. A type no shape reaches, one no operation sends or returns, is rendered
once, unprojected, as an SDK declares it ([a shape reaches what it
holds](plan/types.md#a-shape-reaches-what-it-holds)).

A table lists every member its model's flattening holds, and anchors a member only where the model
declaring it is rendered; another table holding it, a subtype's for an inherited member, links
there. An anonymous model another composes in place is no table of its own: its members are rows
of the composing model's table, anchored there, so no member is anchored twice.

A reference is read through `TypePlan.Alias`, as every consumer reads one
([decorators](refine/decorators.md)). A decorator has no page, table or anchor: a reference to it,
a `DocRef` naming it among them, renders and links as one to its `Alias`, and its annotations fold
onto the position holding it as `CollapseAliases` folds them, the docs reading from the IR those no
position holds. So a 3.0 `allOf` over a sole `$ref` and the 3.1 `$ref` beside the same annotations
render alike, as T-16 requires of every artifact. By `ir.Decorated`, 1,610 of the golden spec's
types are decorators, 645 of them models, and 758 carry the examples of the position they decorate.

An example is shown through the shape it is shown in, so a response's example holds no write-only
member and a request's no read-only one. A value `Property.Secret`, `Parameter.Secret` or
`TypeCommon.Sensitive` marks is redacted in docs ([properties](../ir/properties.md)), so pages,
agent documents and samples show a placeholder in its place, whether the value was declared or
synthesized; an SDK's tests and the mock still send it. A sample's placeholder is the SDK target's
to write, since the call is its code and the docs never edit it.

A description is the spec author's text, and a generated site that passed its raw HTML through would
publish whatever markup or script the document holds. So pages, like agent documents
([below](#documents-for-agents)), render raw HTML in a description as text, save a bare `<br>`,
written with or without its `/` and with no attribute, which renders as a line break. On the golden
spec (spec @57b5196b, morphic @ab953d45), descriptions hold 45 raw HTML tags by CommonMark's
grammar, outside code: 11 a `<br/>`, one an `<img>`, and 33 placeholders such as `<key>` and
`<uuid>`, which a browser handed them would parse as elements, so the placeholder would vanish from
the page.

A link in that text, and a URL the document gives, such as an `ExternalDocs` link's, an
`Example.ExternalURL` or a contact's, license's or terms-of-service URL, can carry script the same
way: a `javascript:` destination runs when it is followed. So a page or an agent document writes a
destination only where it is absolute under `http`, `https` or `mailto`, or a relative path, which
points into the site the author wrote for and is resolved against the base the docs options give
that site. A fragment alone names a place on one of that site's pages, which no base names. Any
other destination, and a relative path with no base set, renders as its link's text, with a warning
naming it. On the golden spec, at those revisions, 14 links in descriptions are relative, such as
`#requests` and `/api/resources/filters/`, and would otherwise break on a generated site.

## Routes and links

Every docs target needs URLs, links between pages, an answer for two entities wanting one URL, and
a way to show one entity across API versions. Each is decided by data, never by a renderer:

- **A catalog first, URLs second.** The emitter builds a catalog — services, groups, operations, and
  the types with a page of their own — holding no URL. A type the document declares, neither
  `Anonymous` nor a primitive, has a page whatever its kind, and so does an anonymous model, union
  or enum that more than one reference reaches, counting through the anonymous lists, maps, tuples
  and scalars that hold it and the decorators over it. Any other type but a decorator (above) is
  rendered where it is referenced: an anonymous model as a table on the page its one holder is
  rendered on, and any other anonymous type as the type it spells. So no type is rendered in two
  places. On the golden spec (spec @57b5196b, morphic @ab953d45), 28 anonymous models are bodies 2
  to 38 operations share, and a page only for a model, union or enum that more than one reference
  names directly would leave 184 more rendered in several places, such as the items of a declared
  list several models hold. A route plan then derives every URL from the catalog alone, through
  templates in the docs emitter's options, one per kind of page
  (`"/{service}/{group}/{operation}"`). A slug is presentation (INV3), rendered from names by the
  naming policy, and two that collide in one template's scope are told apart by the ID-keyed
  resolution identifiers take ([naming](refine/naming.md)): three pairs of the golden spec's
  declared types share their canonical words, which the SDKs resolve as well. Two slugs collide
  whatever their letter case, as artifact paths do ([emit rules](emit/rules.md)), since a file
  system that ignores case keeps one page of the two: two of the golden spec's operations are
  `deleteWebhook` and `DeleteWebhook`, which slugs spelt as the source spells them would part by
  case alone.
- **A total route plan.** Every `OpID` and every type with a page gets a route, every type rendered
  on another's page an anchor there, and every `PropID` an anchor where the model declaring it is
  rendered (above), a decorator answering with its `Alias`'s. Anything left without one is a coded
  diagnostic, and a recursive type is linked by its ID, never inlined to a depth and cut.
- **Collisions are refused.** Two entities whose routes resolve to one URL, letter case aside, are
  an error diagnostic naming both, never a page that overwrites another and never a page dropped.
  The check runs on the URLs the templates produce, over every kind of route in one table, so no
  template can reintroduce a collision the catalog did not have.
- **Links are requests.** A renderer never formats a URL. It writes a link request, an entity's ID
  with the projection it wants (a page, an anchor, the agent document of one target and language),
  and the route plan answers it. A spec's [`Docs.Refs`](../ir/annotations.md#references-in-prose)
  resolve the same way, so a link follows its target wherever its route moves.
- **Versions keep identity.** The emitter receives one version's snapshot ([contract
  rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and an entity's
  page is identified by its ID, never by the version, so a version switcher links one entity across
  versions. Each snapshot's run is a generation of its own, with a manifest of its own ([write /
  integrate](write-integrate.md)), and writes under a directory its label's slug names, which the
  route plan puts before every route and no template sets: one entity's pages in two versions then
  have two paths, and no snapshot's run overwrites or prunes another's. Two labels whose slugs
  collide are refused as two routes on one URL are, and a document stating no version has one
  snapshot, labelled `""`, written at the output root. For a target whose
  `Capabilities.CrossVersion` is set, the engine asks the target for every snapshot's route table
  through `Router` ([the contract](contract/emitter.md)), before any snapshot's `Generate`, handing
  it the input it will hand that `Generate` but for `Samples` and `Routes`, which the route plan
  never reads, and hands each run the tables of all of them in `EmitterInput.Routes`, keyed by the
  label `Document.Versions` gives each version. A run finds its own entry under
  `EmitterInput.Snapshot`, the label of the snapshot it was handed, which is also the version its
  switcher marks current: nothing in a snapshot's document says which version it is. The engine
  cannot plan the routes itself, since the catalog and the templates are the target's. A run plans
  its own routes by the same pure function, so its pages agree with its snapshot's entry. A table
  holds each entity's page and anchor from the output root, its snapshot's directory included,
  which is what a switcher links. What spans versions, the site-scope `llms.txt` linking each
  version's and an index of the versions, is written once, by the run of the last label
  `Document.Versions` lists, since every run holds the labels and tables it is built from. A link to
  an entity a version lacks resolves to that version's page saying so. Slugs may differ between
  versions; IDs do not.

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
  escaped for Markdown, raw HTML in a description is rendered as text, never passed through, a bare
  `<br>` as a line break, and a link is written only where a page would write it (above).

## Samples come from the SDK emitters

How a call is spelled in one language — the client accessor, the method name, the arguments, a
model's constructor — is that SDK emitter's refine output, and an emitter never sees another (INV1).
A docs emitter that spelled calls itself would re-derive them and could disagree with the SDK it
documents. So each SDK emitter returns its usage samples per operation in `EmitterOutput.Samples`,
one for each language its callers write: a Multiplatform target's callers write Kotlin and Swift
([Swift](targets/kotlin/swift.md)). The engine runs a target whose `Capabilities.ReadsSamples` is
set after every target of the run that does not set it, so two that set it wait on neither, and
hands it their samples in `EmitterInput.Samples`, each snapshot's docs run those of the SDK targets
run on the same snapshot; a snapshot no SDK target is run on has no samples, which is no diagnostic.
It runs an SDK target for its samples even when that target's generation writes nothing ([write /
integrate](write-integrate.md#what-the-manifest-records)), since a docs run handed none would drop
them. A docs generation is a new one whenever the samples it is handed change, as an SDK target's
own options can change them. A sample a target cannot render is a coded diagnostic of that target's,
never an empty code block on the page, and so is one for an operation whose required input holds no
example. A sample shows a call that succeeds, so its values are the `Input` of its operation's first
scenario ending in an `Output`, never one ending in an `Error`, whose input is chosen to fail
(below): one the operation declares in `Operation.Examples`, as a Smithy or TypeSpec document can,
else one assembled from each input's set, a body's from the elected request content's and a
parameter's from its own. A value `Secret` or `Sensitive` marks is written as the target's
placeholder for it (above).

## One set of examples

READMEs, samples, generated tests and mock responses all need example values, and each would
synthesize its own where the document declares none, so the docs would show values the SDK's own
tests never sent, and every regeneration could change them. So the plan holds one set,
`Plan.Examples`, keyed by the ID of each type, property (a header among them), parameter, enum
member and operation:

- **Declared first.** An entity's own `Examples` are its set, labelled authored in the docs. One
  declaring none that adds no constraint to its type's takes its type's set, so a property shows
  the value its type does, and an operation declaring no scenario ending in an `Output` takes one
  assembled from its inputs' sets and its return content's, after any it declares, marked
  `Inferred` where a value it takes is. A position whose type is a decorator reads it as
  `CollapseAliases` does: its own examples, else the nearest decorator's along the chain, else the
  set of the type its `Alias` names.
- **Positions with no key.** A content and a variant have no ID, and a message's ID carries no kind
  prefix yet to keep it apart from the other classes in one map
  ([IDs](../ir/identity.md#what-irverify-holds), #1527). So each one's set is its declared examples,
  read from the IR, else its type's, a message's those of its payload's contents, and the docs, the
  samples and the mock read a body's examples from its content's set. Contents are where a document
  most often declares a body's examples: on the golden spec (spec @57b5196b, morphic @ab953d45), 435
  request, response and error contents declare 651 examples.
- **Synthesized only where no declared set applies**, under `plan.Policy.Examples`: a value valid
  against the entity's type and constraints, marked `Inferred` and labelled generated. A model or
  list synthesized holds, for each member or item, the first value of that member's own set: a
  declared value whole, and a synthesized one cut to the depth the bound leaves, so a property's
  value is the same alone and inside its model wherever the bound leaves it whole, and an optional
  recursive member ends. The bounds cut only what validity leaves optional ([plan
  policy](plan/policy.md)): an optional member more than `MaxDepth` models deep, and the entries of
  a list or map past `MaxItems` beyond its minimum. A required member is filled as deep as its
  schema requires, and a `minItems` above `MaxItems` is met: on the golden spec at least 250 models
  require members nested four or more models deep, and one list four items, which the default
  bounds would otherwise leave without an example. A value the synthesizer cannot make valid, as a
  `pattern` it cannot satisfy or a required cycle no value ends, or one holding more values than
  `plan.MaxExampleValues`, a fixed limit the plan package states, is reported and left out, never
  emitted invalid.
- **Stable.** Each choice the synthesizer makes, such as which enum member or which variant, is
  seeded from the entity's [correlation key](../ir/derived-facts.md#correlation-keys), or from its
  ID where no key identifies it. So a value is the same in either declaration order, and on every
  regeneration until the entity's schema, its key, the example policy,
  `irfacts.CorrelationVersion` or the synthesizer's code changes.
- **Shared** by the docs emitter, the samples, each SDK's tests and the mock server, none of which
  synthesizes its own.

The manifest need not persist them: a seeded synthesizer reproduces each value from its inputs, and
a value that changes because one of them did is one that had to.

## Documentation extensions

Documents carry presentation as vendor extensions: a logo, a tag's display name, groups of tags,
code samples on an operation, a marker on an internal operation or property. They stay verbatim in
`Unmodeled`, and the docs emitter, like every emitter, never reads one itself. The [embedded
reader](contract/embedded-policy.md) reads those a configuration maps into members of a hint's
`Extra` ([shaping hints](contract/shaping-hints.md)), by its rules for where a key applies, for
two keys setting one member and for layers. Its default mapping is empty, since each key is one
tool's spelling. The docs emitter decodes the `Extra` members it defines strictly, reporting a
wrong shape and coercing nothing. A key held for a tag, as a display name is, configures nothing
until the IR carries a tag where it lowers (#1221). A code sample a document declares is shown as
authored beside the samples the SDK emitters return. An internal marker is the filter pass's to
read (architecture.md §2.2), never the docs emitter's: hiding an operation in the docs alone would
document an SDK different from the one generated. A search index over the pages, keyed by ID, is
an optional artifact of the same emitter.

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
| Body fields as flags, with the path to put each back | the request `Payload`'s model, by `PropID` |
| Flags that exclude or require each other | the branches of a request body's `Union` |
| A flag whose value is never echoed | `Property.Secret`, `Parameter.Secret`, `TypeCommon.Sensitive` |
| Completing an ID from a list call | `ResourceInfo` and its collection operations |

Command names, positional arguments, `@file` inputs and hidden commands are presentation, so they
are [shaping hints](contract/shaping-hints.md) keyed by ID, `Rename` and the `Extra` members a CLI
target defines. Flag constraints are derived at refine from the union's branches by `PropID`, never
by a property's leaf name: a leaf name derives no conflict between `{origin: {host}}` and
`{proxy: {host}}`, and lets two branches' `host` merge into one flag. Flag names collide as
identifiers do and are told apart by the same ID-keyed resolution ([naming](refine/naming.md)), so
`foo_bar` and a nested `foo.bar` do not both become `--foo-bar`. A body field nested at any depth,
or with `.` in its name, is a flag like any other, and a union beside properties, or a top-level
`anyOf` body, still yields flags, its branches' constraints among them.
