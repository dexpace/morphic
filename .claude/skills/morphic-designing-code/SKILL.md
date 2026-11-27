---
name: morphic-designing-code
description: Use when writing or restructuring production Go code in this repository (a new type, method, package, helper or interface, a walk, index or lookup over a document, a value with a format or a state meaning none, a limit, or code in a shared layer), or before adding or growing a script, prescribed command, CI step, Makefile target or custom check.
---

# Designing morphic code

Correct comes first: a design that scales and reads well but gives a wrong result has no value
(`CLAUDE.md`, "Correctness comes first", which also orders style concerns). Among correct options,
code here should scale with the document, take the next format and emitter without a rewrite, and
stay simple to read and extend. Lean functional, not strictly: data in plain values, behaviour in
functions of explicit inputs that return their results and diagnostics, and a method only where a
type guards its own state. `CLAUDE.md`'s invariants and Go style outrank what follows;
each rule here is a cost a later change paid.

## Before writing

Each row names a rule and code to copy.

| Adding | Rule | Good example |
|---|---|---|
| any function | ask what would make it wrong, and refuse that at entry (`CLAUDE.md`, "Assert aggressively") | `Classify` in `compilers/openapi/internal/value/scalar.go` |
| work on a path every input takes | ask what it costs before writing it (`.claude/skills/morphic-reviewing-prs/references/performance.md#price-common-path`) | `TestWalkValues_StructsAndMapsCostNoAllocation` |
| a lowering, check, pass or helper | `references/design-rules.md#functional-core` | `Read` in `compilers/openapi/internal/annotation/annotation.go` |
| a name | `references/design-rules.md#name-what-it-holds` | `HasFilename` in `ir/operation.go` |
| state every walk of a run must share | `references/design-rules.md#shared-run-state` | `readings` in `compilers/openapi/internal/lowering/lowering.go` |
| a walk, index, lookup or cache over a document | `references/design-rules.md#read-once-per-run`, pruned by `references/design-rules.md#prune-a-visitor-to-its-carriers` | `checkInferred` in `ir/irverify/inferred.go` |
| a value with a format, or a state meaning none | `references/design-rules.md#type-the-form` | `Limit` in `compilers/openapi/internal/budget/limit.go` |
| code in `engine`, `compilers`, `compilers/compile`, `ir` or `pass` | `references/design-rules.md#second-user` | `Compiler` in `compilers/compilers.go` |
| a wrapper, interface or option | `references/design-rules.md#no-forwarding-layer` | `DocumentReader` in `compilers/reader.go` |
| a function another package calls back | `references/design-rules.md#function-crosses-for-a-reason` | `Config` in `compilers/openapi/internal/reftable/table.go` |
| a limit | `references/design-rules.md#bound-the-product` | `MaxSteps` in `compilers/openapi/internal/reftable/table.go` |
| a call into code the package does not own | `references/design-rules.md#catch-reaches-one-goroutine` | `taggedMappingRefusal` in `compilers/openapi/internal/load/load.go` |
| a shape a caller or another consumer will use | `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#measure-the-premise` | `Deprecation` in `ir/docs.go` |
| a flattened member's reader | `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#flattened-member-declarations` | `member` in `compilers/openapi/internal/schema/tagreadings.go` |
| a rule or decision another package or phase already makes | `references/design-rules.md#decide-once` | `Inferred` in `ir/inferred.go` |
| a reading the parser library already makes | `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#reuse-library-readings` | `Pointer` in `compilers/openapi/internal/fragment/fragment.go` |
| a helper or algorithm another site may hold | `references/design-rules.md#share-only-what-means-the-same`, `references/design-rules.md#fallback-suits-the-decision` | `hoistScalar` in `compilers/openapi/internal/schema/scalar.go` |
| code that needs no morphic concept | `references/design-rules.md#domain-free-code-lives-apart` | `Catch` in `barrier/barrier.go` |
| code only some systems build | `references/design-rules.md#no-build-constrained-production-file` | `imagePath` in `engine/cache/build.go` |
| a script, command, CI step, Makefile target or check | `references/design-rules.md#maintained-mechanism-first` | `forbidigo` in `.golangci.yml` |

## Already checked

Leave function caps, `modernize`, layering, method caps, reachability and `callShapeBaseline` to
`.golangci.yml` and `internal/archtest`, and cost claims to `morphic-measuring-performance`.

## References

- `references/design-rules.md`: the rules the table names, with their costs.
