# Docs and mock-server emitters reuse the contract

Both are `Emitter` implementations consuming the **same `Doc + Plan`** — the payoff of computing the
plan once: docs and mocks agree with the SDK by construction, not by re-derivation.

- **Docs emitter (`emitters/docs`).** Reuses the shared plan verbatim (pagination, return shape,
  error taxonomy — what docs must describe; [what the docs render](#what-the-docs-render) says
  which responses) and most of the refine
  step library, but lowers into a `docast` (pages, operation blocks, schema tables, examples) and
  runs a **reduced pipeline**: no `EscapeReservedWords`, and casing is a presentation choice
  (SDK-style identifiers or spec-faithful source names, set in its options). The spans
  [`Docs.Refs`](../ir/annotations.md#references-in-prose) marks resolve to intra-doc links via the
  *same* IR-ID lookup the Go emitter uses for doc comments, so a link follows what its target
  renders as. Examples use the schema arm (`Value/Headers`) vs the operation arm
  (`Input/Output/Error`) correctly. A union or enum `TypePlan.Open` marks is documented as one that
  may hold a variant or value the document does not list, one inclusive in a shape as one whose
  value may be several variants at once, and every `PlannedError` and `PlannedResponse` as the body
  and headers it declares. An enveloped response is documented as the `Type` a method returns and
  the `Envelope.Body` the wire carries, and an example of it as both: the member at `Value` of the
  response's example for the first, and that example whole for the second. An operation is
  documented as safe or idempotent where its `Effects` say so. Its README is returned as blocks a
  user writes around ([write / integrate](write-integrate.md#a-document-the-user-writes-around)).
  No runtime `Policy` is consulted — proving the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`, the content `Return` elects
  and the plan's [examples](#one-set-of-examples) for canned responses, synthesizing none of its
  own, sending the whole `Envelope.Body` where a response is enveloped, `Others` to answer as each
  declared response does, the error taxonomy to simulate declared failures, `TypePlan.Open` to say
  whether a variant or an enum value the document does not list may be sent, `TypePlan.Strict` to
  refuse a `oneOf` value as the SDK does, the `BindingView` to route requests, and the visibility
  projections to honor the same wire shapes an SDK sends/receives. Refine lowers into a
  server-routing AST (routes from `HTTPBinding.URITemplate`, handlers validating against
  constraints). Because the same plan feeds SDK and mock, the two are **wire-consistent by
  construction** — the mock can *be* the interception target the SDK's wire-conformance harness
  ([mandatory tests](testing/mandatory-tests.md)) is diffed against.
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
`ErrorCase` and every `Content` the operation declares, the primary response first and the elected
content first within each, and those choices order the page and decide nothing else. A type is
rendered once per direction it travels, through its `ModelShape` per lifecycle
([TypePlan](plan/types.md#modelshape--visibility-projection)): a request shape lists what a caller
sends and a response shape what it receives, each member's requiredness read in that direction, so
a write-only property never appears in a response's table, nor a read-only one in a request's.

## Routes and links

Every docs target needs URLs, links between pages, an answer for two entities wanting one URL, and
a way to show one entity across API versions. Each is decided by data, never by a renderer:

- **A catalog first, URLs second.** The emitter builds a catalog — services, groups, operations,
  and the types that warrant a page — holding no URL. A route plan then derives every URL from the
  catalog alone, through templates in the docs emitter's options, one per kind of page
  (`"/{service}/{group}/{operation}"`). A slug is presentation (INV3), rendered from names by the
  naming policy and marked `Inferred` where a default derived it.
- **A total route plan.** Every `OpID` and every type with a page gets a route, and every `PropID`
  an anchor on its type's page. Anything left without one is a coded diagnostic, and a recursive
  type is linked by its ID, never inlined to a depth and cut.
- **Collisions are refused.** Two entities whose routes resolve to one URL are an error
  diagnostic naming both, never a page that overwrites another and never a page dropped. The check
  runs on the URLs the templates produce, over every kind of route in one table, so no template can
  reintroduce a collision the catalog did not have.
- **Links are requests.** A renderer never formats a URL. It writes a link request, an entity's ID
  with the projection it wants (a page, an anchor, the agent document of one target and language),
  and the route plan answers it. A spec's [`Docs.Refs`](../ir/annotations.md#references-in-prose)
  resolve the same way, so a link follows its target wherever its route moves.
- **Versions keep identity.** The emitter receives one version's snapshot ([contract
  rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and an entity's
  page is identified by its ID, never by the version, so a version switcher links one entity
  across versions. For a target whose `Capabilities.CrossVersion` is set, the engine asks the
  target for every snapshot's route table through `Router` ([the
  contract](contract/emitter.md)), before any snapshot's `Generate`, and hands each run the tables
  of all of them in `EmitterInput.Routes`, keyed by the label `Document.Versions` gives each
  version. The engine cannot plan the routes itself, since the catalog and the templates are the
  target's. A run plans its own routes by the same pure function, so its pages agree with its
  snapshot's entry. A table holds each entity's page and anchor, which is what a switcher links. A
  link to an entity a version lacks resolves to that version's page saying so. Slugs may differ
  between versions; IDs do not.

## Documents for agents

Beside its pages, the docs emitter writes the same content as plain Markdown for coding agents, from
the same `Doc + Plan` and through the same route plan, so an agent document and a page never
disagree about a URL:

- **One document per operation**, holding its method and path, parameters and schemas as nested
  lists, examples, code samples and links, as one aggregate document and one per SDK target and
  sample language, so an agent reads only the language it writes.
- **`llms.txt` indexes** at site, service, version and target scope, each a Markdown index linking
  the agent documents of its scope rather than the pages.
- **Facts, not guesses.** An operation is marked safe, or safe to retry, from `OpPlan.Effects`
  ([OpPlan](plan/operations.md)). A warning that it destroys something comes only from a declared
  fact, a hint or a promoted extension, since a method cannot say it: `DELETE` and `PUT` are
  idempotent and may destroy.
- **Spec text is escaped.** A heading or a list item built from a summary or a description is
  escaped for Markdown, and raw HTML in a description is rendered as text, never passed through.

## Samples come from the SDK emitters

How a call is spelled in one language — the client accessor, the method name, the arguments, a
model's constructor — is that SDK emitter's refine output, and an emitter never sees another
(INV1). A docs emitter that spelled calls itself would re-derive them and could disagree with the
SDK it documents. So each SDK emitter returns its usage samples per operation in
`EmitterOutput.Samples`, one for each language its callers write: a Multiplatform target's
callers write Kotlin and Swift ([Swift](targets/kotlin/swift.md)). The engine runs a target whose
`Capabilities.ReadsSamples` is set after the run's other targets, and hands it every one's samples
in `EmitterInput.Samples`. It runs an SDK target for its samples even when that target's
generation writes nothing ([write / integrate](write-integrate.md#what-the-manifest-records)), since
a docs run handed none would drop them. A docs generation is a new one whenever the samples it is
handed change, as an SDK target's own options can change them. A sample a target cannot render is
a coded diagnostic of that target's, never an empty code block on the page, and so is one for an
operation whose required input holds no example. Each sample's values are the first of each set
the plan's examples hold (below).

## One set of examples

READMEs, samples, generated tests and mock responses all need example values, and each would
synthesize its own where the document declares none, so the docs would show values the SDK's own
tests never sent, and every regeneration could change them. So the plan holds one set,
`Plan.Examples`, keyed by the ID of each type, property (a header among them), parameter, enum
member and operation:

- **Declared first.** An entity's own `Examples` are its set, labelled authored in the docs. One
  declaring none that adds no constraint to its type's takes its type's set, so a property shows
  the value its type does. A content, a variant and a message have no ID: each one's declared
  examples are read from the IR, and one declaring none takes its type's set. They are where a
  document most often declares a body's examples: on the golden spec (spec @57b5196b, morphic
  @ab953d45), 435 request, response and error contents declare 651 examples.
- **Synthesized only where no declared set applies**, under `plan.Policy.Examples`: a value valid
  against the entity's type and constraints, marked `Inferred` and labelled generated. A model or
  list synthesized holds, for each member or item, the first value of that member's own set, so a
  property's value is the same alone and inside its model. The bounds cut only what validity leaves
  optional ([plan policy](plan/policy.md)): an optional member more than `MaxDepth` models deep, and
  a list's items past `MaxItems` beyond its minimum. A required member is filled as deep as its
  schema requires, and a `minItems` above `MaxItems` is met: on the golden spec at least 250 models
  require members nested four or more models deep, and one list four items, which the default
  bounds would otherwise leave without an example. A value the synthesizer cannot make valid, as a
  `pattern` it cannot satisfy or a required cycle no value ends, or one past its fixed limit on the
  values one example holds, is reported and left out, never emitted invalid.
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
`Unmodeled`, and the docs emitter reads those its options name, as the [embedded
reader](contract/embedded-policy.md) reads policy: a key to a presentation field, the value decoded
strictly and a wrong shape reported, the entry left in place, and an empty default mapping, since
each key is one tool's spelling. A code sample a document declares is shown as authored beside the
samples the SDK emitters return. An internal marker is the filter pass's to read (architecture.md
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
| A confirmation before a destructive call | a declared fact only: a hint or a promoted extension |
| Streamed output for a list | `OpPlan.Page` |
| Choices and defaults on a flag | `Enum`, `Parameter.Default`, `Property.Default` |
| Body fields as flags, with the path to put each back | the request `Payload`'s model, by `PropID` |
| Flags that exclude or require each other | the branches of a request body's `Union` |
| A flag whose value is never echoed | `Property.Secret`, `TypeCommon.Sensitive` |
| Completing an ID from a list call | `ResourceInfo` and its collection operations |

Command names, positional arguments, `@file` inputs and hidden commands are presentation, so they
are [shaping hints](contract/shaping-hints.md) keyed by ID. Flag constraints are derived at refine
from the union's branches by `PropID`, never by a property's leaf name: a leaf name derives no
conflict between `{origin: {host}}` and `{proxy: {host}}`, and lets two branches' `host` merge into
one flag. Flag names collide as identifiers do and are told apart by the same ID-keyed resolution
([naming](refine/naming.md)), so `foo_bar` and a nested `foo.bar` do not both become `--foo-bar`. A
body field nested at any depth, or with `.` in its name, is a flag like any other, and a union
beside properties, or a top-level `anyOf` body, still yields flags, its branches' constraints among
them.
