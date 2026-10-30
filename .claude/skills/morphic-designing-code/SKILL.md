---
name: morphic-designing-code
description: Use when writing or restructuring production Go code in this repository, such as a new type, method, package, helper or interface, a walk, index or lookup over a document, a value with a format or a state meaning none, a limit, or code in a layer every format or emitter shares.
---

# Designing morphic code

Code here should scale with the document, take the next format and emitter without a rewrite, and
stay simple to read and extend. Lean functional, not strictly: data in plain values, behaviour in
functions of explicit inputs that return their results and diagnostics, and a method only where a
type guards its own state. `CLAUDE.md`'s invariants and Go style come first; each rule here is a
cost a later refactor or performance change paid.

## Before writing

| Adding | Rule |
|---|---|
| a lowering, check, pass or helper | `references/design-rules.md#functional-core` |
| a walk, index, lookup or cache over a document | `references/design-rules.md#read-once-per-run` |
| a value with a format, or a state meaning none | `references/design-rules.md#type-the-form` |
| code in `engine`, `compilers`, `compilers/compile`, `ir` or `pass` | `references/design-rules.md#second-user` |
| a wrapper, interface or option | `references/design-rules.md#no-forwarding-layer` |
| a limit | `references/design-rules.md#bound-the-product` |
| a rule another package already implements | `.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md#parallel-implementations-agree` |
| a reading the parser library already makes | `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#reuse-library-readings` |

## Already checked

Leave these to their checks. `.golangci.yml` caps a function at 70 lines and a cognitive complexity
of 20, and its `modernize` linter proposes the standard library's forms. `internal/archtest` holds
the layering, caps a type under `compilers/` at 20 methods, and refuses a function only tests call.
A claim about cost goes through `morphic-measuring-performance`.

## References

- `references/design-rules.md`: functions over growing structs, one read per run, typed forms,
  placement by the second user, no forwarding layers, bounds on what the work grows with.
