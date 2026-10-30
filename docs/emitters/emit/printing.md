# The emit layer

`emitters/<target>/emit` plus per-target printer. Emit is a **pure fold** `print(TargetAST) → []byte`,
then the language's native formatter canonicalises the result.

## Typed AST → text → native formatter (the rationale)

The Stainless model, adapted to Go: the printer emits syntactically complete text and hands it to
`go/format.Source` (Python `black`, TS `prettier`), which is the canonical authority on formatting.
Indentation, spacing, and import ordering stop being the emitter's problem — an entire class of
whitespace/import-order bugs is eliminated by delegation. A language whose formatter Go cannot call
in process, as Kotlin's, takes the other route: its printer lays out canonical code itself, and the
formatter runs as a check in the verify stage, outside `Generate` ([Kotlin
formatting](../targets/kotlin/overview.md#formatting)).

## Determinism, and the velocity answer

Emit walks the AST in source order and any map in sorted-key order (INV7); identical `IR + options`
⇒ byte-identical output ([T-6](../testing/mandatory-tests.md)), which is what makes golden snapshots
and the ID-keyed manifest meaningful. On velocity: a new *language* is expensive (node set + printer
+ pipeline), but a new *non-SDK emitter* (docs, mock) is cheap because it reuses the shared plan and
most of the refine step library ([docs and mocks](../docs-and-mock.md)); and the per-language cost
buys compiler-enforced correctness, a free surface model, and identity-based merge that a template
pack cannot. The `Emit` seam remaining per-emitter means a target may opt into
templates-for-structure if it ever needs to — the design does not mandate typed-AST emit, it
defaults to it.
