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
  (`Input/Output/Error`) correctly. A union or enum `TypePlan.Open` marks is documented as one a
  response may extend, one `TypePlan.Inclusive` marks as one whose value may be several variants at
  once, and every `PlannedError` and `PlannedResponse` as the body and headers it declares. An
  enveloped response is documented as the `Type` a method returns and the `Envelope.Body` the wire
  carries, and an operation as safe or idempotent where its `Effects` say so. No runtime `Policy` is
  consulted — proving the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`, the content `Return` elects
  and the IR `Examples` to synthesize canned responses, sending the whole `Envelope.Body` where a
  response is enveloped, `Others` to answer as each declared response does, the error taxonomy to
  simulate declared failures, `TypePlan.Open` to say whether a variant or an enum value the document
  does not list may be sent, the `BindingView` to route requests, and the visibility projections to
  honor the same wire shapes an SDK sends/receives. Refine lowers into a server-routing AST (routes
  from `HTTPBinding.URITemplate`, handlers validating against constraints). Because the same plan
  feeds SDK and mock, the two are **wire-consistent by construction** — the mock can *be* the
  interception target the SDK's wire-conformance harness ([mandatory
  tests](testing/mandatory-tests.md)) is diffed against.
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
  with the projection it wants (a page, an anchor, the agent document of one target), and the
  route plan answers it. A spec's [`Docs.Refs`](../ir/annotations.md#references-in-prose) resolve
  the same way, so a link follows its target wherever its route moves.
- **Versions keep identity.** The emitter receives one version's snapshot ([contract
  rules](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and an entity's
  page is identified by its ID, never by the version, so a version switcher links one entity
  across versions. The engine computes each snapshot's route table, a pure function of the snapshot
  and the options, and hands every snapshot's docs run the tables of all of them in
  `EmitterInput.Routes`, for a target whose `Capabilities.CrossVersion` is set. A link to an entity
  a version lacks resolves to that version's page saying so. Slugs may differ between versions; IDs
  do not.

## Documents for agents

Beside its pages, the docs emitter writes the same content as plain Markdown for coding agents, from
the same `Doc + Plan` and through the same route plan, so an agent document and a page never
disagree about a URL:

- **One document per operation**, holding its method and path, parameters and schemas as nested
  lists, examples, code samples and links, as one aggregate document and one per SDK target, so an
  agent reads only the language it writes.
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
SDK it documents. So each SDK emitter returns a usage sample per operation in
`EmitterOutput.Samples`, and the engine, which runs the SDK targets first, hands the docs emitter
every target's samples in `EmitterInput.Samples`. A sample a target cannot render is a coded
diagnostic of that target's, never an empty code block on the page. Each sample's values are the
plan's examples (below).

## One set of examples

READMEs, samples, generated tests and mock responses all need example values, and each would
synthesize its own where the document declares none, so the docs would show values the SDK's own
tests never sent, and every regeneration could change them. So the plan holds one set,
`Plan.Examples`, keyed by the ID of each type, property, parameter and operation:

- **Declared first.** An entity's `Examples` are its set, labelled authored in the docs.
- **Synthesized only where none is declared**, under `plan.Policy.Examples`: a value valid against
  the entity's type and constraints, bounded in depth and in the items it lists, marked `Inferred`
  and labelled generated. Each choice the synthesizer makes, such as which enum member or which
  variant, is seeded from the entity's correlation key, so the value is the same on every
  regeneration until the entity's schema or the synthesizer's step version changes. A value it
  cannot make valid, as a `pattern` it cannot satisfy, is reported and left out, never emitted
  invalid.
- **Shared** by the docs emitter, the samples, each SDK's tests and the mock server.

The manifest need not persist them: a seeded synthesizer reproduces each value from its inputs, and
a value that changes because its schema did is one that had to.

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
