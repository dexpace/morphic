# Package layout & layering

Consistent with architecture.md §3 (`emitters/*` is Layer 2: imports `ir`, `ir/irfacts` + the emitter contract,
never `compiler`, never `engine`, never a sibling target):

```
emitters/
├── contract.go        # Emitter, EmitterInput/Output, Artifact, Capabilities, TargetKey, registry
├── plan/              # SHARED, language-neutral — imports ir, ir/irfacts. Build(), Plan, OpPlan, TypePlan, ModelShape
│   └── plantest/      #   golden snapshots of Plan (language-independent corpus)
├── policy/            # runtime SDK-policy vocabulary + Defaults() + Merge(DeepPartial) (INV10)
├── naming/            # SHARED naming engine: acronym/compound tables, the character-name table,
│                      #   reserved-word set type, ID-keyed collision resolver. Casing FUNCTIONS are
│                      #   per-target (naming.Policy impl)
├── typemap/           # SHARED type table: Table, Row, lookup, merge, strict decoding (contract/type-table.md)
├── refine/            # SHARED refiner contract + code-shaping helpers more than one target uses
│                      #   (untagged-union recognition, the decorator fold, import-set harness);
│                      #   never a plan fact
├── manifest/          # generation manifest (ID-keyed), header provenance, additive-merge driver (write-integrate.md)
├── verify/            # neutral Surface projection, differ, injectable per-language severity (surface-verification.md)
├── golang/            # ◀ FIRST TARGET (TargetKey "go")
│   ├── emitter.go     #   New() returns the target the engine registers; plan→refine→emit
│   ├── goast/         #   typed target AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan → goast — the ordered lowering pipeline (refine/pipeline.md)
│   ├── emit/          #   goast → []byte via printer + go/format; templates/ (boilerplate only)
│   ├── naming/        #   Go reserved words + default acronym table (naming.Policy impl)
│   └── mergeadapter.go#   go/ast additive-merge adapter (registers into manifest driver)
├── kotlin/            # ◀ FIRST TARGETS (TargetKeys "kotlin-jvm" and "kotlin-multiplatform")
│   ├── emitter.go     #   NewJVM() and NewMultiplatform(): one pipeline, two Platform profiles
│   ├── platform.go    #   Platform: the built-in type table, identifier rules, transport
│   ├── ktast/         #   typed Kotlin AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan+Platform → ktast — the ordered lowering pipeline (refine/pipeline.md)
│   ├── emit/          #   ktast → []byte via a printer laying out canonical code; templates/
│   └── naming/        #   Kotlin hard keywords, the Objective-C/Swift export rules (Multiplatform)
├── docs/              # docs emitter: reuses plan + refine step library; docast + Markdown/HTML printer (docs-and-mock.md)
└── mock/              # mock-server emitter: reuses plan; server-routing AST + printer (docs-and-mock.md)
```

`goast` lives *under* `emitters/golang`, not in the shared contract, because a target AST is
per-language by definition. `plan`, `policy`, `naming`, `typemap`, `refine` (the contract + shared
helpers), `manifest`, and `verify` are shared because they are language-neutral: `typemap` holds a
table's shape and rules, and each target its rows. This section lays out Morphic's own packages; how
a generated SDK divides into packages of its own, by source document or by group, is in [output
units](refine/output-units.md).

**Architecture test.** The import-graph rule is in place before the first emitter package:
`internal/archtest` rules `emitters`, and names it in `unbuilt` until the package exists, so the
first package is audited from its first import. Each package under `emitters/` carries an entry of
its own, as a compiler's packages do, and none may be exempt, which only tooling under `internal/`
is. The entries the first packages add assert: `emitters/golang` and `emitters/kotlin` may each
import `emitters`, `emitters/plan`, `emitters/policy`, `emitters/naming`, `emitters/typemap`,
`emitters/refine`, `emitters/manifest`, `emitters/verify`, `ir` and `ir/irfacts`; neither may import
`compilers/*`, `engine`, or the other.
**Switch-completeness tests** live beside `goast` and `ktast` (over each one's `NodeKind`) and
beside every refiner (over every `ir.TypeKind`): adding an IR kind fails the test of every dispatch
that must handle it (the `assertNever` obligation).
