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
- A new conformance spec has no YAML aliases, is named by a rule's `Cases:` line, and passes
  `go test ./internal/harness ./compilers/openapi/...`.
- Run `go test ./...` once: tests in other packages read the corpus too.

## Traps that recur

- One construct has several readers. Before changing how a keyword is read, find every reader of
  it.
- A reading the library already implements is called, not re-derived.
- A malformed construct is reported once, by the grammar-driven load phase. A lowering reports
  only what it models.
- A version's specification text decides validity, not its published JSON Schemas.
- A new bound in the load scan warns and lets the compile continue, as the existing ones do.

Each is a rule in `references/openapi-lessons.md`, with its evidence and probe.

## References

- `references/openapi-lessons.md`: readers, the load phase, library mirrors, the corpus.
- `references/lossless-and-diagnostics.md`: every keyword lands somewhere and every diagnostic is
  right.
- `references/spec-and-grammar.md`: zero values, spellings, tokens and escapes.
