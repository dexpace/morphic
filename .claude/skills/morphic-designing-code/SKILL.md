---
name: morphic-designing-code
description: Use when writing or restructuring production Go code in this repository, such as a new type, method, package, helper or interface, a walk, index or lookup over a document, a value with a format or a state meaning none, a limit, or code in a layer every format or emitter shares.
---

# Designing morphic code

Correct comes first: a design that scales and reads well but gives a wrong result has no value
(`CLAUDE.md`, "Correctness comes first", which also orders style concerns). Among correct options,
code here should scale with the document, take the next format and emitter without a rewrite, and
stay simple to read and extend. Lean functional, not strictly: data in plain values, behaviour in
functions of explicit inputs that return their results and diagnostics, and a method only where a
type guards its own state. When in doubt, default to the styleguide's stance: data and functions,
immutable, explicit, errors handled. `CLAUDE.md`'s invariants and Go style outrank what follows;
each rule here is a cost a later refactor or performance change paid.

## Before writing

Each row names the rule, and code holding it to copy.

| Adding | Rule | Good example |
|---|---|---|
| any function | ask what would make it wrong, and refuse that at entry (`CLAUDE.md`, "Assert aggressively") | `Classify` in `compilers/openapi/internal/value/scalar.go` |
| work on a path every input takes | ask what it costs before writing it (`.claude/skills/morphic-reviewing-prs/references/performance.md#price-common-path`) | `TestWalkValues_StructsAndMapsCostNoAllocation` |
| a lowering, check, pass or helper | `references/design-rules.md#functional-core` | `Read` in `compilers/openapi/internal/annotation/annotation.go` |
| state every walk of a run must share | `references/design-rules.md#shared-run-state` | `readings` in `compilers/openapi/internal/lowering/lowering.go` |
| a walk, index, lookup or cache over a document | `references/design-rules.md#read-once-per-run`, pruned by `references/design-rules.md#prune-a-visitor-to-its-carriers` | `checkInferred` in `ir/irverify/inferred.go` |
| a value with a format, or a state meaning none | `references/design-rules.md#type-the-form` | `Limit` in `compilers/openapi/internal/budget/limit.go` |
| code in `engine`, `compilers`, `compilers/compile`, `ir` or `pass` | `references/design-rules.md#second-user` | `Compiler` in `compilers/compilers.go` |
| a wrapper, interface or option | `references/design-rules.md#no-forwarding-layer` | `DocumentReader` in `compilers/reader.go` |
| a function another package calls back | `references/design-rules.md#function-crosses-for-a-reason` | `Config` in `compilers/openapi/internal/bind/table.go` |
| a limit | `references/design-rules.md#bound-the-product` | `maxResumeWork` in `compilers/openapi/internal/load/resume.go` |
| a call into code the package does not own | `references/design-rules.md#catch-reaches-one-goroutine` | `taggedMappingRefusal` in `compilers/openapi/internal/load/load.go` |
| a shape a caller or another consumer will use | `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#measure-the-premise` | `Deprecation` in `ir/docs.go` |
| a flattened member's reader | `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#flattened-member-declarations` | `pinnedMember` in `compilers/openapi/internal/schema/tags.go` |
| a rule another package already implements | `.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md#parallel-implementations-agree` | `Inferred` in `ir/inferred.go` |
| a reading the parser library already makes | `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#reuse-library-readings` | `Pointer` in `compilers/openapi/internal/fragment/fragment.go` |
| a helper or algorithm another site may hold | `references/design-rules.md#share-only-what-means-the-same`, `references/design-rules.md#fallback-suits-the-decision` | `hoistScalar` in `compilers/openapi/internal/schema/scalar.go` |
| code that needs no morphic concept | `references/design-rules.md#domain-free-code-lives-apart` | `Catch` in `barrier/barrier.go` |

## Already checked

Leave these to their checks. `.golangci.yml` caps a function at 70 lines and a cognitive complexity
of 20, and its `modernize` linter proposes the standard library's forms. `internal/archtest` holds
the layering, caps a type under `compilers/` at 20 methods, refuses a function only tests call, and
holds the calls a package makes into one it does not import to `callShapeBaseline`.
A claim about cost goes through `morphic-measuring-performance`.

## References

- `references/design-rules.md`: the rules the table names, with their costs.
