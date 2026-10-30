## 5. The emit layer

`emitters/<target>/emit` plus per-target printer. Emit is a **pure fold** `print(TargetAST) → []byte`,
then the language's native formatter canonicalises the result.

### 5.1 Typed AST → text → native formatter (the rationale)

The Stainless model, adapted to Go: the printer emits syntactically complete text and hands it to
`go/format.Source` (Python `black`, TS `prettier`), which is the canonical authority on formatting.
Indentation, spacing, and import ordering stop being the emitter's problem — an entire class of
whitespace/import-order bugs is eliminated by delegation. A language whose formatter Go cannot call
in process, as Kotlin's, takes the other route: its printer lays out canonical code itself, and the
formatter runs as a check in the verify stage, outside `Generate` (§8.8).

**Why a domain AST → text → `go/format`, not `go/ast` directly.** `go/ast` is awkward to *synthesize*
(it wants a `token.FileSet` and real positions and cannot carry generator intent like a field's
`Origin` IR ID or a `{t:TypeID}` doc token). The domain AST is smaller, holds provenance and wire
metadata on its nodes, and prints to text that `go/format` then makes canonical. `go/ast` is used in
exactly one place — re-parsing *existing* files for additive merge (§11) — where its parser is the
right tool. The two Go trees are kept aligned by ID-keying: "a symbol I generated" ↔ "a symbol I
parsed" is a lookup.

### 5.5 Determinism, and the velocity answer

Emit walks the AST in source order and any map in sorted-key order (INV7); identical `IR + options`
⇒ byte-identical output (§13 test T-6), which is what makes golden snapshots and the ID-keyed
manifest meaningful. On velocity: a new *language* is expensive (node set + printer + pipeline), but
a new *non-SDK emitter* (docs, mock) is cheap because it reuses the shared plan and most of the
refine step library (§10); and the per-language cost buys compiler-enforced correctness, a free
surface model, and identity-based merge that a template pack cannot. The `Emit` seam remaining
per-emitter means a target may opt into templates-for-structure if it ever needs to — the design
does not mandate typed-AST emit, it defaults to it.
