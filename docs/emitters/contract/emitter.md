# The emitter contract

Package `emitters` is the Layer-2 contract, as package `compilers` is the compilers'. It imports
**only** `ir` and `ir/irfacts` (Layer 0) plus its own shared sub-packages (`plan`, `policy`,
`naming`, `typemap`, `manifest`, `verify`) — never a compiler, never `engine`, never a sibling
target package (INV1; architecture.md §3). Enforced by the import-graph test ([package
layout](../packages.md)).

```go
package emitters

// TargetKey identifies an emitter in the registry: "go", "kotlin-jvm", "kotlin-multiplatform",
// "docs", "mock/go", …
type TargetKey string

// Emitter is one target — a language SDK, a docs site, a mock server, a validator.
// Pure and reentrant: no package-level mutable state, no clock, no randomness, no stderr,
// no filesystem I/O. Same input ⇒ byte-identical output (INV5, INV7).
type Emitter interface {
    Target() TargetKey
    Capabilities() Capabilities          // lets the engine reason without running (registry.md)
    DecodeOptions(raw []byte) (TargetOptions, []ir.Diagnostic, error) // strict, versioned (options.md)
    Generate(context.Context, EmitterInput) EmitterOutput // the whole emitter, as one pure function
}

type EmitterInput struct {
    Doc     *ir.Document          // the ABI — the ONLY structural input (INV1)
    Facts   *irfacts.Index        // what Doc implies: subtypes, usage, recursion (ir-design
                                  // §4.9), indexed ONCE by the engine, shared with the plan (below)
    Plan    *plan.Plan            // language-neutral decisions, computed ONCE by the engine (../plan/overview.md)
    Policy  policy.Policy         // runtime/SDK behavior — a SEPARATE tree, never in the IR (INV10; runtime-policy.md)
    Hints   map[EntityID]ShapeHint// SDK-shaping hints keyed by IR stable ID (shaping-hints.md)
    Scope   *Scope                // nil = whole document; else "emit only these" — full Doc still
                                  // consumed so global decisions stay byte-identical
    Prior   *PriorState           // prior manifest + prior surface for write/integrate; nil first run
    Options TargetOptions         // the target's own typed options, decoded and validated before
                                  // Generate runs, its type table and naming tables among them
                                  // (options.md, type-table.md, ../refine/naming.md)
}

type EmitterOutput struct {
    Artifacts   []Artifact          // files (+ integration directives) — no I/O performed here
    Diagnostics []ir.Diagnostic     // typed, coded, provenance-bearing; engine decides fatality
    Manifest    *manifest.Manifest  // entity→symbol map keyed by IR stable IDs (../write-integrate.md)
    Surface     *verify.Surface     // neutral API-surface projection of the generated output (../surface-verification.md)
}

// EntityID is the string form of any IR stable ID (TypeID, OpID, PropID, ServiceID, …).
// Every IR ID is a `type X string`, so one keyspace addresses them all.
type EntityID = string

// Artifact is oagen's GeneratedFile shape, minus every SDK-policy-shaped field.
type Artifact struct {
    Path         string          // deterministic, forward-slash, relative to the output root and
                                 // held inside it (../emit/rules.md)
    Content      []byte
    Kind         ArtifactKind    // source | test | doc | manifest | dependency-manifest | scaffold
    Header       HeaderPlacement // where the generated-by provenance header goes (top/after-shebang/none)
    SkipIfExists bool            // scaffolding written once (config, .gitignore) — never overwritten
    Overwrite    bool            // regenerate but preserve ignore-regions (default is additive)
    Integrate    *IntegrateHint  // path-prefix stripping / tree-shake root for live-repo merge (../write-integrate.md)
    Origins      []EntityID      // IR IDs whose emission produced this file (drives ID-keyed prune)
}
```

`error` is not part of the `Generate` signature: an emitter has no failure mode that is not either a
`Diagnostic` (something about the input it cannot lower) or a programmer bug: a `TypeKind` with no
dispatch, caught by the switch-completeness test in the suite ([mandatory
tests](../testing/mandatory-tests.md)) rather than at runtime, or an options value of another
target's type, reported as an error diagnostic ([target options](options.md)). This keeps the
contract pure and total.

`Generate` takes the run's context, for the reason a pass's `Run` does (architecture.md §2.2):
emitting an SDK costs time in proportion to the document, and a deadline or a cancel must reach it.
An emitter asks the context between artifacts and inside its walks, and stops soon once it is done.
The engine asks it after `Generate` returns, and a run whose context is done by then fails with the
context's error, whatever `Generate` returned: an emitter cut short may have stopped anywhere, and
no artifact of it is written.

## Who computes the plan

`plan.Build(doc, facts, planPolicy)` is a pure function ([the plan](../plan/overview.md)). For a
multi-target run the **engine computes it once** and passes the same `*plan.Plan` into every
`Generate` call. This is what makes "computed once, shared, cannot drift" literally true rather than
a convention, and it lets the plan be cached and golden-tested on its own corpus, independent of any
language. Plan policy is document-level, not per-target — the plan is language-neutral by
definition, so there is nothing per-language to vary in it. A single-target invocation may compute
the plan inline; the function is the same.

The derived facts work the same way. The engine builds one `irfacts.Index` for the document once the
last pass has run, since an index answers for the document it was built from and a pass may change
it (ir-design §4.9), and hands that index to `plan.Build` and, as `EmitterInput.Facts`, to every
`Generate`. So the strongly connected components a refiner reads to break a recursive reference
([recursive references](../refine/models.md#recursive-references)) are found once per document, not
once per target. An index is safe for concurrent use, so targets running side by side share it. It
is not serialized: a consumer outside the process rebuilds it from the document, which determines
it.

## Contract rules the type system and tests enforce

- **Purity / determinism.** No I/O, clock, randomness, or stderr inside `Generate`. Same
  `(Doc, Plan, Policy, Hints, Scope, Options)`, under a context that stays live, ⇒ byte-identical
  `Artifacts`; `Facts` is a function of `Doc` and adds nothing to the tuple. Maps are walked in
  sorted-key order, slices in source order ([T-6](../testing/mandatory-tests.md)).
- **Diagnostics are the only input-failure channel.** A construct the emitter cannot lower emits
  `ir.Diagnostic{Severity, Code:"emitters/<target>/…", Message, Provenance}` — never a silent
  fallback, never a panic ([T-7](../testing/mandatory-tests.md)). Codes live in a stable
  `emitters/<target>/…` namespace.
- **Full document under scope.** Scope gates *emission*, not the plan or naming; global decisions
  are computed over the whole document so a scoped run is byte-identical to the corresponding slice
  of a full run. Scope is never a filtered `Document`.
- **Snapshots only.** The engine runs `version-slice` before any emitter; the emitter receives a
  single version snapshot and never interprets `Availability`. The pass is designed, not built
  (architecture.md §2.2); until it is, no pass slices a document.
