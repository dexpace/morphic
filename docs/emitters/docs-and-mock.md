# Docs and mock-server emitters reuse the contract

Both are `Emitter` implementations consuming the **same `Doc + Plan`** — the payoff of computing the
plan once: docs and mocks agree with the SDK by construction, not by re-derivation.

- **Docs emitter (`emitters/docs`).** Reuses the shared plan verbatim (primary response/content,
  pagination, return-shape, error taxonomy — exactly what docs must describe) and most of the refine
  step library, but lowers into a `docast` (pages, operation blocks, schema tables, examples) and
  runs a **reduced pipeline**: no `EscapeReservedWords`, and casing is a presentation choice
  (SDK-style identifiers or spec-faithful source names, set in its options). The spans
  [`Docs.Refs`](../ir/annotations.md#references-in-prose) marks resolve to intra-doc links via the
  *same* IR-ID lookup the Go emitter uses for doc comments, so a link follows what its target
  renders as. Examples use the schema arm (`Value/Headers`) vs the operation arm
  (`Input/Output/Error`) correctly. A union `TypePlan.Open` marks is documented as one a response
  may extend, one `TypePlan.Inclusive` marks as one whose value may be several variants at once, and
  every `PlannedError` and `PlannedResponse` as the body and headers it declares. An enveloped
  response is documented as the `Type` a method returns and the `Envelope.Body` the wire carries. No
  runtime `Policy` is consulted — proving the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`, the content `Return` elects
  and the IR `Examples` to synthesize canned responses, sending the whole `Envelope.Body` where a
  response is enveloped, `Others` to answer as each declared response
  does, the error taxonomy to simulate declared failures, `TypePlan.Open` to say whether a variant
  the document does not list may be sent, the `BindingView` to route requests, and the visibility
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

Adding any of these is a registry entry over one `Doc + Plan` — no change to the ABI, no change to
the IR.
