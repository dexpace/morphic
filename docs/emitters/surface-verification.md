## 12. Surface verification / compat

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
- **Extractors for the first targets.** Go's reads the existing package's exported API, as
  `apidiff` does. The Kotlin targets read the ABI dumps the Kotlin Gradle plugin's ABI validation
  writes: the JVM's class-file signatures, and for Multiplatform the KLib ABI plus the generated
  Objective-C header, since a change Swift sees can break callers the Kotlin ABI does not show.
  Kotlin's severity differs from Go's in two places: a parameter added with a default leaves
  Kotlin callers compiling but changes the JVM signature, which is binary-breaking, and on
  Multiplatform a rename only the Swift header shows still breaks Swift callers.
- **Behavioral channel separate from structural.** A default-value change is behavioral,
  tracked in a distinct channel from a signature change — detectable without re-parsing because the
  plan already carries defaults as typed `Value`s. Every finding carries drift provenance (which
  stage caused it).
