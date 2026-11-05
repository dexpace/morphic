# Fidelity

The angle that holds Morphic's first claim, that compiling loses nothing: each construct the change
touches lands in the IR typed, or verbatim in `Unmodeled` with its reason, at every position and in
every spelling the format allows, and each diagnostic is true for the input that drew it. It loads
the domain skill, `morphic-openapi-compiler` for that compiler and `morphic-changing-the-ir` for the
IR and the passes.

1. Diff each keyword the change accepts against the same spec without it
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#twin-diff-every-accepted-keyword`),
   with the keywords declared beside it
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#test-co-declared-keywords-together`).
2. Place the construct at every position, and spell it every way the format allows
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#same-construct-at-every-position`,
   `.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#every-spelling-of-one-concept`);
   measure a dialect's reading on schemas of that dialect
   (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#dialect-reading-on-real-schemas`).
3. Hold each `Unmodeled` entry to its reason and the typed home it lacks
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#unmodeled-reason-and-typed-home`),
   each inference to its `Inferred` mark and a policy that disables it, each name to its neutral
   words, and presence apart from null (`CLAUDE.md`, invariants 4, 6 and 8).
4. Read each diagnostic's provenance against the site that fired
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#diagnostic-provenance-names-the-real-site`),
   and each defect as reported once
   (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#one-defect-one-finding`).
5. For a reader moved off the library, account for every diagnostic `main` reported
   (`references/fidelity.md#moved-reader-parity`); for a predicate choosing a new path, compile its
   near miss (`references/fidelity.md#near-miss`); for re-attributed records, read the diagnostic
   reported in their place (`references/fidelity.md#reattributed-failure-form`).

The matrix is each construct the change touches, by position and spelling, with what lands in the IR
and what is reported.

## A PR that moves readers off the library inherits what the library skipped
`moved-reader-parity`

- **Rule.** Diff `resolutionBaseline` in `internal/archtest` between `main` and head: each package
  that loses a library call is a moved reader. Probe each one with inputs the old reader never
  reached (references inside other documents, components behind chains, aliases, escapes), and
  account for every diagnostic `main` reported that head drops.
- **Why.** Readers moved off the library in #1016 followed `$ref`s written inside another
  document, which the library never resolved, and lent facts from them, dangling encoding keys
  among them; and they read a component written as a string by its YAML shape, reporting a `$ref`
  to it unresolved while the IR held its type. Every corpus missed both.

## Probe the near miss and the equivalence
`near-miss`

- **Rule.** For each predicate that decides whether a new path applies, compile the issue's example
  plus a near miss from each class of keyword it tells apart: for a schema, one per vocabulary
  (`$id` among identifiers, an applicator, an assertion, `description` among annotations) beside an
  `x-` extension, naming the class each probes. For "A lowers as B", compile both spellings and diff
  them with IDs and pointers projected away, with `main` and a planted defect as controls.
- **Why.** An all-or-nothing guard falls back to the reported defect on the near miss while the
  surrounding code handles the keyword partly, as a cycle guard skipped for any source that merely
  wrote `$id` did (#763), and a draft 7 predicate probed with annotations and assertions alone
  missed that an `$id` beside a `$ref` moves its base (#1467).

## Check the failure form of every record a change re-attributes
`reattributed-failure-form`

- **Rule.** When a change re-attributes records (whose source a provenance names), also check the
  diagnostic reported in place of each record when it fails. Give a table's fallback a value
  distinct from 0 and from every real index.
- **Why.** A combined entry was credited to its keywords while the error reported in its place kept
  the old attribution (#552); with a fallback of 0, two planted mutants survived.
