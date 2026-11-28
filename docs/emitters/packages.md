# Package layout & layering

Consistent with architecture.md §3: `emitters/*` is Layer 2, which imports Layer 0 as [the
table](#the-layers) lists for each package and never a compiler or `engine`, and the engine's
packages are Layer 3. Shared code is libraries a target calls, never a framework: a target is a
package that imports the contract and whichever shared packages it needs, and composes them
itself. Nothing shared runs a target but the contract: its registry reads a target's key and
capabilities, and its checked calls configure, inspect and run one ([the
contract](contract/emitter.md#the-checked-calls)). Any other shared package calls a target's code
only where the target hands it that code, as `refine.RunSteps` applies a target's steps and
`options.Decode` its migrations.

## The layers

Inside Layer 2 the packages form tiers, and no import points up.

```
                     engine            Layer 3: orchestration, every read, write and process
        ┌──────────────┼───────────────┐
  engine/project   engine/emit   engine/artifacts
        └──────────────┼───────────────┘
            emitters/<target>          tier 4: one target; imports the contract and the kernel
                       │
                   emitters            tier 3: the contract, the registry, the checked calls
                       │
   naming  typemap  policy  hints  embed  refine  placement  prose  manifest  verify
                       │                                  tier 2: mechanisms, one rule each
                 emitters/plan         tier 1: decisions, computed once
                       │
        options   symbol   pathfold    tier 0: vocabulary with no emitter concept
                       │               (pathfold is top-level, in Layer 0)
                 ir, ir/irfacts        Layer 0
```

| Package | Holds | Imports beside the standard library |
|---|---|---|
| `pathfold` | the path fold and the rules of a portable path segment ([emit rules](emit/rules.md)) | `golang.org/x/text` |
| `emitters/symbol` | a symbol's key and its text form ([write / integrate](write-integrate.md)) | `ir` |
| `emitters/options` | versioned, strict configuration decoding ([target options](contract/options.md)) | `ir` |
| `emitters/plan` | the shared plan, its policy decoding, and under `plantest` its goldens ([the plan](plan/overview.md)) | `ir`, `ir/irfacts`, `emitters/options` |
| `emitters/naming` | words, casing primitives, scopes, collisions ([naming](refine/naming.md)) | `ir`, `pathfold` |
| `emitters/typemap` | the type table: shape, merge, lookup ([type table](contract/type-table.md)) | `ir`, `emitters/options` |
| `emitters/policy` | runtime policy: vocabulary, layers, resolve ([runtime policy](contract/runtime-policy.md)) | `ir`, `emitters/options` |
| `emitters/hints` | shaping hints: vocabulary, layers, resolve ([shaping hints](contract/shaping-hints.md)) | `ir`, `emitters/options` |
| `emitters/embed` | the policy and hints a document embeds ([embedded policy](contract/embedded-policy.md)) | `ir`, `emitters/policy`, `emitters/hints` |
| `emitters/refine` | the step runner and the lowering helpers targets share, never a plan fact ([the refine layer](refine/pipeline.md)) | `ir`, `ir/irfacts`, `emitters/plan`, `emitters/typemap` |
| `emitters/placement` | which output unit a declaration lands in ([output units](refine/output-units.md)) | `ir`, `emitters/plan`, `emitters/hints` |
| `emitters/prose` | documentation text holding its references by ID | `ir`, `emitters/symbol` |
| `emitters/manifest` | the generation manifest as data, and its migrations ([write / integrate](write-integrate.md)) | `pathfold`; `emitters/symbol`, once it holds entities |
| `emitters/verify` | the neutral surface, the differ, the severity type ([surface verification](surface-verification.md)) | `emitters/symbol` |
| `emitters` | the contract, the registry, the checked calls ([the contract](contract/emitter.md)) | `ir`, `ir/irfacts`, `barrier`, `pathfold`, `emitters/plan`, `emitters/policy`, `emitters/hints`, `emitters/symbol`, `emitters/manifest`, `emitters/verify` |
| `emitters/emittertest` | the fake target, the golden tree, the oracles every target is held to | `ir`, `ir/irfacts`, `ir/irtest`, `emitters`, the kernel, go-cmp |
| `emitters/<target>` | one target | `ir`, `ir/irfacts`, `emitters`, any kernel package, its own packages; `emitters/golang` also `github.com/dave/dst` and `golang.org/x/mod` |
| `engine/project` | the project configuration, decoded ([the project](contract/project.md)) | `ir`, `pathfold`, `emitters`, `emitters/options`, `emitters/plan`, `emitters/policy`, `emitters/hints`, `emitters/embed` |
| `engine/emit` | the emit stage, with no I/O | `ir`, `ir/irfacts`, `pathfold`, `emitters`, `emitters/plan`, `emitters/policy`, `emitters/hints`, `emitters/embed`, `engine/project` |
| `engine/artifacts` | planning and applying a generation's writes under its root ([write / integrate](write-integrate.md)) | `ir`, `barrier`, `pathfold`, `emitters`, `emitters/manifest`, `engine/internal/atomicfile` |

The kernel is tiers 0 to 2. What the table implies:

- **No kernel package imports `emitters`.** The contract sits above the kernel, so a mechanism is
  tested with no target and no engine.
- **No kernel package imports a sibling the table does not list.** `naming` does not know the
  manifest: a shipped name reaches it as a plain value its caller fills
  ([naming](refine/naming.md)). `typemap` does not know the plan: a refine helper resolves a
  reference to its key.
- **`engine/emit` and `engine/artifacts` do not import each other.** The first has no I/O, the
  second no knowledge of how a generation was made.
- **`ShapeHint` is `emitters/hints`'s**, not the contract's, so `embed` reads it without importing
  the contract, and policy and hints are two packages of one shape.
- **`pathfold` is top-level**, beside `barrier`, since the engine, the contract, `naming` and
  `manifest` compare by it and it names no Morphic concept.

A package's entry in `internal/archtest` holds what its files import at the time, so it grows to
its row here with the code that needs each import, and never past it.

## What is built

The table is the design, and most of it is ahead of the code. A package is built where the tree
holds it:

```bash
git ls-files 'pathfold/*.go' 'emitters/*.go' 'engine/project/*.go' 'engine/emit/*.go' \
  'engine/artifacts/*.go' | xargs -r -n1 dirname | sort -u
```

Every other row is planned. The [milestone plan](milestones.md#the-foundation) says which land
with the foundation, some of them ahead of their first caller, and which with their first reader.

## A target's packages

```
emitters/
├── golang/            # ◀ FIRST TARGET (TargetKey "go")
│   ├── emitter.go     #   New() returns the target the engine registers; plan→refine→emit
│   ├── goast/         #   typed target AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan → goast — the ordered lowering pipeline (refine/pipeline.md)
│   ├── emit/          #   goast → []byte via printer + go/format; templates/ (boilerplate only)
│   ├── naming/        #   Go's naming data: each role's convention, the reserved names, the
│   │                  #   default tables of each options version
│   └── mergeadapter.go#   dst additive-merge adapter (write-integrate.md)
├── kotlin/            # ◀ FIRST TARGETS (TargetKeys "kotlin-jvm" and "kotlin-multiplatform")
│   ├── emitter.go     #   NewJVM() and NewMultiplatform(): one pipeline, two Platform profiles
│   ├── platform.go    #   Platform: the built-in type table, identifier rules, transport
│   ├── ktast/         #   typed Kotlin AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan+Platform → ktast — the ordered lowering pipeline (refine/pipeline.md)
│   ├── emit/          #   ktast → []byte via a printer laying out canonical code; templates/
│   └── naming/        #   Kotlin's naming data: hard keywords, the Objective-C/Swift export rules
│                      #   (Multiplatform)
├── docs/              # docs emitter: reuses plan + refine step library; docast + Markdown/HTML printer (docs-and-mock.md)
└── mock/              # mock-server emitter: reuses plan; server-routing AST + printer (docs-and-mock.md)
```

`goast` lives *under* `emitters/golang`, not in the kernel, because a target AST is per-language by
definition. The kernel's packages are shared because they are language-neutral: `typemap` holds a
table's shape and rules, and each target its rows; `naming` holds the rules two targets must agree
on, and each target the data it hands them. This document lays out Morphic's own packages; how a
generated SDK divides into packages of its own, by source document or by group, is in [output
units](refine/output-units.md).

**Architecture test.** The import-graph rule is in place before the contract package:
`internal/archtest` rules `emitters`, and names it in `unbuilt` while the directory holds only
packages carrying rules of their own, so the contract is audited from its first import. Each
package under `emitters/` and each of the engine's carries an entry of its own, as a compiler's
packages do, and none may be exempt, which only tooling under `internal/` is. A target's entry
asserts: it may import `emitters`, any kernel package, `ir` and `ir/irfacts`; it may not import
`compilers/*`, `engine`, or another target.
`emitters/golang` may also import `github.com/dave/dst`, for its merge adapter
([merging](targets/go/overview.md#merging-and-surface-extraction)), and `golang.org/x/mod`, whose
`module.CheckPath` its options decoding calls ([package
directories](targets/go/naming.md#package-directories)), and no other package may import either.
Its severity test's oracle, `golang.org/x/exp/apidiff` (T-75), is a test's import alone.
**Switch-completeness tests** live beside `goast` and `ktast` (over each one's `NodeKind`) and
beside every refiner (over every `ir.TypeKind`): adding an IR kind fails the test of every dispatch
that must handle it (the `assertNever` obligation).
