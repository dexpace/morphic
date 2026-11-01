# Surface verification / compat

Post-milestone. `EmitterOutput.Surface` is the neutral API-surface projection — classes, interfaces,
enums, methods, fields as neutral records — built by the **same walk** over `Origin`-tagged AST nodes
that emit performs.

- **Neutral projection with IR IDs.** Both the freshly-generated SDK and an extracted
  existing SDK (via a per-language extractor) project into one `verify.Surface`; each record carries
  its **IR stable ID** (threaded from `Origin`), so cross-language findings correlate without
  name-matching, and rename detection is an ID lookup rather than oagen's Jaccard-similarity
  heuristics. `Generate` returns the generated side directly; only the extractor parses source.
- **Injectable per-language severity.** The differ emits **neutral change records**; a
  per-target `verify.Severity` function decides breaking/additive (parameter names are public API in
  PHP, arity in Go, ~nothing in JS) — heuristics-as-policy (INV6).
- **An extractor per target.** Each first target's extractor and where its severity differs are in
  its document ([Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)).
- **Behavioral channel separate from structural.** A default-value change is behavioral,
  tracked in a distinct channel from a signature change — detectable without re-parsing because the
  plan already carries defaults as typed `Value`s. Every finding carries drift provenance (which
  stage caused it).
