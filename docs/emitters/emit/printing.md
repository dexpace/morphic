# The emit layer

`emitters/<target>/emit` plus per-target printer. Emit is a **pure fold**
`print(target AST) → []byte`,
then the language's native formatter canonicalises the result.

## The printer reads the AST alone

The printer takes the AST and nothing else: no IR document, plan, derived facts, options, policy or
hints. Beyond the language's syntax and the [generated-by
line](../contract/emitter.md#the-generated-by-line), whatever the text depends on is a node or a
node's field, a declaration's documentation and an import's alias among them, and a reference by ID
names a declaration of the same AST or carries the text refine resolved it to, a docs target's link
through its route plan among them. A runtime template reads only the scalar policy values it
interpolates ([structure and boilerplate](templates.md)). So a target's AST and those values are all
a renderer outside Morphic would need to print what this printer prints ([the
README](../README.md#a-renderer-outside-morphic)). T-79 holds this section.

## Typed AST → text → native formatter (the rationale)

The Stainless model: the printer emits syntactically complete text and hands it to the language's
formatter (`go/format.Source`, Python `black`, TS `prettier`), which is the canonical authority on
formatting. Indentation, spacing, and import ordering stop being the emitter's problem — an entire
class of whitespace/import-order bugs is eliminated by delegation. A language whose formatter Go
cannot call in process, as Kotlin's, takes the other route: its printer lays out canonical code
itself, and the formatter runs as one of the target's checks, outside `Generate`
([Kotlin formatting](../targets/kotlin/overview.md#formatting)).

## Determinism, and the velocity answer

Emit walks the AST in source order and any map in sorted-key order (INV7); identical inputs, the
prior manifest among them ([contract
rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), ⇒ byte-identical
output ([T-6](../testing/mandatory-tests.md)), which is what makes golden snapshots
and the ID-keyed manifest meaningful. On velocity: a new *language* is expensive (node set +
printer + pipeline), but a new *non-SDK emitter* (docs, mock) is cheap because it reuses the shared
plan and most of the refine step library ([docs and mocks](../docs-and-mock.md)); and the
per-language cost buys compiler-enforced correctness, a free surface model, and identity-based merge
that a template pack cannot. The `Emit` seam remaining per-emitter means a target may opt into
templates-for-structure if it ever needs to — the design does not mandate typed-AST emit, it
defaults to it.
