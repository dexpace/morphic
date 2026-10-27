---
name: morphic-openapi-compiler
description: Use when changing, reviewing or debugging code under compilers/openapi, the OpenAPI specs and goldens under testdata, docs/compilers/openapi.md or docs/parser-library.md.
---

# Working in the OpenAPI compiler

## Where things are

- **Rules.** `docs/compilers/openapi.md` is normative: one keyed entry per lowering rule, each
  naming its cases, codes and tests. Find a rule by its key (`grep -n '^### ' docs/compilers/openapi.md`)
  rather than reading the whole file, and cite it as `docs/compilers/openapi.md#<key>`.
- **The library.** `docs/parser-library.md` records each parser-library behaviour the compiler
  depends on, each pinned by a canary in `compilers/openapi/internal/canary`. Cite the entry; never
  restate a behaviour or name a library version.
- **Packages.** `compilers/openapi` is the public face, and the work is in
  `compilers/openapi/internal`, whose import order `internal/archtest` enforces. Read the order
  from the `rules` map there, not from memory.
- **Tests.** End-to-end tests, goldens and the conformance corpus run in `compilers/openapi/e2e`.
  `TestLoweringSpec` holds the rules document and the code to each other in both directions.
  `CONTRIBUTING.md` has the recipe for a new lowering rule.

## Before calling a change done

- Compile the input and read the IR and every diagnostic: `go run ./cmd/morphic compile <spec>`
  (the IR goes to stdout, diagnostics to stderr).
- A new conformance case follows step 4 of `CONTRIBUTING.md`'s lowering-rule recipe and the
  `conformance-case` rule, and passes `go test ./internal/harness ./compilers/openapi/...`.
- Run `go test ./...` once: tests in other packages read the corpus too.

## Traps that recur

Each is a rule in the references below, by its key, with its evidence and probe.

- One construct has several readers: before changing how a keyword is read, find every reader of
  it (`every-reader`).
- A reading the library already implements is called, not re-derived (`reuse-library-readings`).
- A malformed construct is reported once, by load's rule table
  (`docs/compilers/openapi.md#validation`); a lowering adds no second report
  (`one-defect-one-finding`).
- A version's specification text decides validity, not its published JSON Schemas (`spec-text`).
- A new bound's outcome, refusal or warning, comes from the rule entry for its kind
  (`bound-outcome-from-the-rules`).

## References

- `references/openapi-lessons.md`: readers, the load phase, library mirrors, YAML spellings, input
  routes, tokens and the corpus.
- `references/lossless-and-diagnostics.md`: every keyword lands somewhere, at every position and
  in every spelling, and every diagnostic is right.
