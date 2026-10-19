# Contributing to Morphic

This guide is the way in. It says what to read for the change you are making, then walks the three
changes most contributions are: a lowering rule in the OpenAPI compiler, a pass, and a compiler.
Every step names the check that fails when the step is missed, so a red test points back to the
step. The rules themselves live in the root guidance, [`CLAUDE.md`](CLAUDE.md), and in the design
documents under [`docs/`](docs/); this guide links them rather than restating them.

## Before you start

- **Toolchain.** The Go release `go.mod`'s `go` directive names, or newer. Lint runs
  golangci-lint at the version `make print-lint-version` prints; a release built with an older Go
  cannot type-check this module, so install that version with the toolchain you build with:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(make print-lint-version)`.
- **The gate.** `make gate` runs every check CI runs, in the order CI runs them. Coverage is gated
  at exactly 100% of statements, counted from the profile, so `go test ./...` passing is not
  evidence the gate passes. While iterating, run one package (`go test ./compilers/openapi/...`) or
  one test (`go test ./ir -run TestName`), and `make coverage` or `make lint` on their own.
- **Conventions.** Branch names, commit messages and pull requests follow the root guidance's
  [Commits & pull requests](CLAUDE.md#commits--pull-requests); Go code follows its style section.

### What to read

Read the documents for your layer, and skip the rest until you need them.

| You are changing | Read first | Read when you need it |
|---|---|---|
| A lowering rule of the OpenAPI compiler | the rules near yours in [`docs/compilers/openapi.md`](docs/compilers/openapi.md), and the [`ir-design.md`](docs/ir-design.md) section of the shape it fills | [`micro-compiler-design.md`](docs/micro-compiler-design.md), for which package a lowering belongs in |
| A pass | [`architecture.md`](docs/architecture.md) §2.2 | the `ir-design.md` sections of the nodes it reads |
| A compiler | `architecture.md` §2 and §3, `ir-design.md`, [`ir-spec-matrix.md`](docs/ir-spec-matrix.md) | [`prior-art.md`](docs/prior-art.md), [`reference-learnings.md`](docs/reference-learnings.md) |
| The IR | `ir-design.md`, `architecture.md` §2.5, `prior-art.md` | `reference-learnings.md` |
| An emitter (none exists yet) | [`emitter-design.md`](docs/emitter-design.md), `architecture.md` §2.3 to §2.5 | `prior-art.md` |

## Add a lowering rule to the OpenAPI compiler

A lowering rule says how one OpenAPI construct reaches the IR: the field it fills, what it keeps in
`Unmodeled` and under which reason, and what it reports. Each rule is one entry of
[`docs/compilers/openapi.md`](docs/compilers/openapi.md).

1. **State the rule.** Add its entry under the section of the construct, or edit the entry that
   already covers it: a `### ` heading whose text is the rule's key in code, its text, and the
   fields that close it (the document's opening paragraphs say what each field holds). Write the
   rule before the code; it is what the review checks the code against.
   *Fails if missed:* `TestLoweringSpec_RulesAreKeyedAndCarryEvidence` for a malformed entry or one
   that names no evidence.

   If the IR has no field for what the rule needs, stop here: that is a change to the IR, which
   [`architecture.md`](docs/architecture.md) §2.5 governs. Until it lands, the rule keeps the
   construct verbatim under `no_ir_home`.
2. **Add a diagnostic code, if the rule reports one.** Declare it in
   `compilers/openapi/internal/diag/diag.go`, with a doc comment naming every site that reports it,
   add it to `codes()` in `diag_test.go`, and name it in the rule's Codes field.
   *Fails if missed:* `TestCodes_MatchTheDeclaredSet` for `codes()`, and
   `TestLoweringSpec_CodesAreDeclared` for a code no rule names.
3. **Implement it** in the package `micro-compiler-design.md` assigns the construct to. The root
   guidance's caps apply: no function over 70 lines or a cognitive complexity of 20, no type under
   `compilers/` over 20 methods, no doc comment over 100 words, and no lint suppression in the
   source.
   *Fails if missed:* `make lint`, `TestMethodsPerType_StayUnderTheCap`,
   `TestDocComments_StayUnderTheCap`, `TestSuppressions_NoDirectiveStandsInTheSource`.
4. **Add a conformance case.** Write a minimal spec at `testdata/conformance/openapi/<name>.yaml`
   that exercises the rule, add an entry to `conformanceCases()` in
   `compilers/openapi/conformance_test.go` naming its assertion and the
   [`ir-spec-matrix.md`](docs/ir-spec-matrix.md) rows it witnesses, write the assertion, and name
   the case in the rule's Cases field. Then write its golden:

   ```bash
   go test ./compilers/openapi -run 'TestConformance' -update
   ```

   *Fails if missed:* `TestConformance_TableNamesEveryCorpusSpec` for a spec with no entry,
   `TestConformance/<name>` for a golden that does not match, and
   `TestLoweringSpec_CasesAreTheCorpus` for a case no rule names.
5. **Regenerate what the change moves, and read the diff.** Other goldens may change too:

   ```bash
   go test ./compilers/openapi -run 'TestGolden|TestConformance' -update
   git diff --stat testdata/
   ```

   A regeneration that changes nothing looks exactly like a broken `-update`; delete the construct
   from your spec once and watch the case go red before trusting it.
6. **Cite the rule, don't restate it.** A comment or test that relies on the rule cites it as
   `docs/compilers/openapi.md#<key>`.
   *Fails if missed:* `TestLoweringSpec_CitationsNameARule`, for a citation of a key that does not
   exist.
7. **Run the gate:** `make gate`. A statement only a fault-injection seam reaches still needs a test
   that reaches it; `make coverage` names the file and line it misses.

## Add a pass

A pass is an IR-to-IR step the engine runs after a compile ([`architecture.md`](docs/architecture.md)
§2.2). Today a pass can analyse a document and report on it, or return a new document built from
it, but it cannot replace or remove a type: writing the type registry outside `compilers/compile`
and `ir` fails `TestRegistryWrites_StayInsideTheFramework`, and the sanctioned way for a pass to do
it is #966's.

1. **Implement `pass.Pass`** in a file of its own under `pass/`: a stable `Name()`, unique among
   the engine's passes, and a `Run` that never mutates its input, returns its input when it only
   analyses, and reports through its diagnostics alone. Codes are `pass/<name>`, or `ir/<name>` for
   a structural finding.
2. **Test it** beside it in `pass/`, table-driven, against documents built in the test.
3. **Add it to the default list** in `engine/passes.go`, before `validate`, which stays last.
   *Fails if missed or misplaced:* `TestDefaultPasses_Order` pins the list, and must be updated on
   purpose; `TestRun_UnknownPassIsMisuse` in `cmd/morphic` pins the names a refusal lists.
4. **Settings**, if the pass takes any, are a typed field on `engine.RunOptions` and a flag of the
   pass's own in `cmd/morphic`, both added with the pass (architecture.md §2.2).
5. **Record it** in `architecture.md` §2.2, from "Designed, not yet built" to "Built".
6. **Run the gate:** `make gate`.

## Add a compiler

A compiler lowers one source format into an IR document ([`architecture.md`](docs/architecture.md)
§2.1). `compilers.Compiler`'s doc comments are its contract; read them before writing any of it.

1. **Create the package** `compilers/<format>`, with a `doc.go`, a `Compiler` implementing all five
   methods of `compilers.Compiler`, and the `Options` type its `FormatOptions` holds.
2. **Rule its imports.** Add `compilers/<format>` to the `rules` map in
   `internal/archtest/arch_test.go`, allowing `ir`, `compilers` and `compilers/compile` by exact
   path, its own internal tree, and its format libraries. Give each package under its `internal/`
   an entry of its own.
   *Fails if missed:* `TestImportGraph_EveryPackageIsRuledOrExempt`, whose message names the
   package; an import outside the allowlist fails `TestImportGraph_LayeringHolds`.
3. **Mint IDs and names through the framework.** Intern types through `compile.Types`, derive IDs
   and canonical names with `compilers/compile`, declare the namespaces you mint in
   (`Document.IDSpaces`), and stamp `ir.IRVersion`. Call `compile.Types.Violations()` after
   interning, as the OpenAPI compiler does.
   *Fails if missed:* `TestNamingGrammar_NameChannelsAreFilledByTheFrameworkOnly`,
   `TestIDGrammar_CompilersDeriveIDsThroughTheFramework`, `TestRegistryWrites_StayInsideTheFramework`.
4. **Register it** in `engine.New` (`engine/engine.go`), the one place built-in compilers are
   listed.
5. **Test it** with golden snapshots through `ir/irtest` and `irverify` over its output. A goroutine
   needs `internal/leakcheck` in the package's `TestMain`.
   *Fails if missed:* `TestGoroutineLeak_EveryGoStatementHasALeakCheck`.
6. **Write its lowering specification** at `docs/compilers/<format>.md`, moving the format's list
   out of `ir-design.md` §14, and link it from there.
7. **Run the gate:** `make gate`.
