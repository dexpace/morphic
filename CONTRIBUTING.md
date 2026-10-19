# Contributing to Morphic

This guide is the way in. It says what to read for the change you are making, then walks the three
changes most contributions are: a lowering rule in the OpenAPI compiler, a pass, and a compiler.
Each step names the check that fails when the step is missed, so a red test points back to the
step, and says so where nothing checks it, so a green run is not read as more than it proves. The
rules themselves live in the root guidance, [`CLAUDE.md`](CLAUDE.md), and in the design documents
under [`docs/`](docs/); this guide links them rather than restating them.

## Before you start

- **Toolchain.** The Go release `go.mod`'s `go` directive names, or newer. Lint runs
  golangci-lint at the version `make print-lint-version` prints. Another build can fail to
  type-check this module, reporting `typecheck` errors rather than findings, so install that
  version with the toolchain you build with:
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
| A lowering rule of the OpenAPI compiler | the rules near yours in [`docs/compilers/openapi.md`](docs/compilers/openapi.md), and the [`ir-design.md`](docs/ir-design.md) section of the shape it fills | [`micro-compiler-design.md`](docs/micro-compiler-design.md), for why the compiler's packages split where they do; [`parser-library.md`](docs/parser-library.md), before relying on what the parser library returns |
| A pass | [`architecture.md`](docs/architecture.md) §2.2 | the `ir-design.md` sections of the nodes it reads |
| A compiler | `architecture.md` §2 and §3, `ir-design.md`, [`ir-spec-matrix.md`](docs/ir-spec-matrix.md) | [`prior-art.md`](docs/prior-art.md), [`reference-learnings.md`](docs/reference-learnings.md) |
| The IR | `ir-design.md`, `architecture.md` §2.5, `prior-art.md` | `reference-learnings.md` |
| An emitter (none exists yet) | [`emitter-design.md`](docs/emitter-design.md), `architecture.md` §2.3 to §2.5 | `prior-art.md` |

## Add a lowering rule to the OpenAPI compiler

A lowering rule says how one OpenAPI construct reaches the IR: the field it fills, what it keeps in
`Unmodeled` and under which reason, and what it reports. Each rule is one entry of
[`docs/compilers/openapi.md`](docs/compilers/openapi.md).

1. **State the rule.** Edit the entry that already covers the construct, and add an entry only for
   a construct no rule covers: a `### ` heading whose text is the rule's key in code, its text, and
   the fields that close it (the document's opening paragraphs say what each field holds). The text
   says what the construct lowers to, and when each code the rule reports fires, written
   `openapi/<code>`; what the compiler does not do yet goes in the Status, with its issue, and the
   text names no issue. Write the rule before the code; it is what the review checks the code
   against.
   *Fails if missed:* `TestLoweringSpec_RulesAreKeyedAndCarryEvidence` for an entry that names no
   evidence, `TestLoweringSpec_TextStatesEachCodeAndNoIssue` for a code the text never writes or an
   issue it names, and every `TestLoweringSpec` test for an entry in a shape the reader refuses. A
   rule never written fails the checks of steps 2 and 4 instead, once its code or case exists and no
   rule names it.

   If the IR has no field for what the rule needs, stop here: that is a change to the IR, which
   [`architecture.md`](docs/architecture.md) §2.5 governs. Until it lands, the rule keeps the
   construct verbatim under `no_ir_home`.
2. **Add a diagnostic code, if the rule reports one.** Declare it in
   `compilers/openapi/internal/diag/diag.go`, add it to `codes()` in `diag_test.go`, and name it in
   the rule's Codes field and its text. Its doc comment names every site that reports it; a reviewer
   checks that, and no test does.
   *Fails if missed:* `TestCodes_MatchTheDeclaredSet` for `codes()`,
   `TestLoweringSpec_CodesAreDeclared` for a code no rule names, and
   `TestLoweringSpec_TextStatesEachCodeAndNoIssue` for one the Codes field names and the text does
   not.
3. **Implement it** where the rules near yours are lowered: their Tests fields name the packages
   that hold the code, and `micro-compiler-design.md` says why each package holds what it does. The
   parser library's model can read an absent field and an empty one alike, so check what it
   returns before you branch on it. The root guidance's caps apply: no function over 70 lines or a
   cognitive complexity of 20, no type under `compilers/` over 20 methods, no doc comment over 100
   words, and no lint suppression in the source.
   *Fails if missed:* `make lint`, `TestMethodsPerType_StayUnderTheCap`,
   `TestDocComments_StayUnderTheCap`, `TestSuppressions_NoDirectiveStandsInTheSource`.
4. **Add a conformance case and a unit test.** Write a minimal spec at
   `testdata/conformance/openapi/<name>.yaml` that exercises the rule, add an entry to
   `conformanceCases()` in `compilers/openapi/conformance_test.go` naming its assertion and the
   [`ir-spec-matrix.md`](docs/ir-spec-matrix.md) rows it witnesses (none, for a case that pins only
   a diagnostic), write the assertion, and name the case in the rule's Cases field. Then write its
   golden alone, since a run that also reaches `TestConformance_TableNamesEveryCorpusSpec` reads the
   directory before the golden exists:

   ```bash
   go test ./compilers/openapi -run '^TestConformance$/^<name>$' -update
   ```

   Coverage is counted per package, and the case runs in `compilers/openapi`, so it covers nothing
   under `internal/`: the code you wrote there also needs a test in its own package, which the
   rule's Tests field then names.
   *Fails if missed:* `TestConformance_TableNamesEveryCorpusSpec` for a spec with no entry,
   `TestConformance/<name>` for a golden that does not match, `TestLoweringSpec_CasesAreTheCorpus`
   for a case no rule names, and `make coverage` for a statement only the case reaches.
5. **Regenerate what the change moves, and read the diff.** Other goldens may change too:

   ```bash
   go test ./compilers/openapi -run 'TestGolden|TestConformance' -update
   git status --short testdata/
   ```

   A regeneration that changes nothing looks exactly like a broken `-update`, and nothing checks
   that an assertion asserts anything: delete the construct from your spec once and watch the case
   go red before trusting it.
6. **Cite the rule, don't restate it.** A comment or test that relies on the rule cites it as
   `docs/compilers/openapi.md#<key>`, on one line.
   *Checked by:* `TestLoweringSpec_CitationsNameARule`, which fails on a citation naming no rule: a
   misspelt key, or a rule renamed without its citations. Nothing requires a citation.
7. **Run the gate:** `make gate`. A statement only a fault-injection seam reaches still needs a test
   that reaches it; `make coverage` names the file and line it misses. On a loaded machine
   `TestDetectCycles_MergeChainPastBoundStaysFastAndWarns` can fail its wall-clock bound under
   `make coverage` (#979); rerun before reading that failure as yours.

## Add a pass

A pass is an IR-to-IR step the engine runs after a compile
([`architecture.md`](docs/architecture.md) §2.2). Today a pass can analyse a document and report on
it, or return a new document built from it, but it cannot replace or remove a type: writing the type
registry outside `compilers/compile` and `ir` fails the `registryWrites` lint rule
(`internal/lintrules`), and the sanctioned way for a pass to do it is #966's.

1. **Implement `pass.Pass`** in a file of its own under `pass/`, shaped as `Validator` and
   `ValidateName` are in `pass/pass.go`: an exported type, and a constant holding its stable
   `Name()`, unique among the engine's passes. `Run` never mutates its input, returns its input when
   it only analyses, and reports through its diagnostics alone. `ir.ForEachOperation` walks the
   operations, and the package's `diag` helper in `pass/validate.go` builds a diagnostic on a node.
   A code names the defect, as `pass/doc.go` says: `pass/<defect>` for a finding the pass owns,
   `ir/<defect>` for a structural one `irverify` reports too.
   *Fails if missed:* the build, once step 3 lists the pass, for a method `pass.Pass` requires.
2. **Test it** beside it in `pass/`, table-driven, against documents built in the test.
   `pass/validate_test.go` has builders (`validDoc`, `docWithOperation`) and readers (`codes`,
   `countCode`) to reuse.
   *Fails if missed:* `make coverage`.
3. **Add it to the default list** in `engine/passes.go`, before `validate`, which stays last, and
   update the tests that pin the list: `TestDefaultPasses_Order`, and `TestRun_UnknownPassIsMisuse`
   in `cmd/morphic` for the names a refusal lists. Nothing fails if the pass never joins the list,
   where it simply never runs; the pins catch a list changed without them, and a pass placed after
   `validate`.
   A default pass runs on every CLI test's spec. One that reports on the shared specs in
   `internal/testspec` breaks the `cmd/morphic` tests that expect a clean run, such as
   `TestRun_ValidateExitCodes/clean_spec`, so keep those specs clean of what it reports.
4. **Settings**, if the pass takes any, are a typed field on `engine.RunOptions` and a flag of the
   pass's own in `cmd/morphic`, both added with the pass (architecture.md §2.2).
5. **Record it** in `architecture.md` §2.2 under "Built, and in the default list", moving it from
   "Designed, not yet built" where it is listed there, and in the two other places that name the
   built passes: the README's Passes bullet and `emitter-design.md` §1.1. No test reads any of them.
6. **Run the gate:** `make gate`.

## Add a compiler

A compiler lowers one source format into an IR document ([`architecture.md`](docs/architecture.md)
§2.1). `compilers.Compiler`'s doc comments are its contract; read them before writing any of it.

1. **Create the package** `compilers/<format>`, with a `doc.go`, a `Compiler` implementing all five
   methods of `compilers.Compiler`, and the `Options` type its `FormatOptions` holds.
   *Fails if missed:* `make lint` for a package with no package comment, and the build, once step 4
   registers the compiler, for a missing method.
2. **Rule its imports.** Add `compilers/<format>` to the `rules` map in
   `internal/archtest/arch_test.go`, allowing `ir`, `compilers` and `compilers/compile` by exact
   path, its own internal tree, and its format libraries. Give each package under its `internal/`
   an entry of its own; nothing enforces that yet, and an unkeyed one is audited under its
   compiler's allowlist (#993). Run `gofmt -w` on the file afterwards: a longer key realigns its
   neighbours.
   *Fails if missed:* `TestImportGraph_LayeringHolds`. A package with no entry falls back to the
   `compilers` rule, which allows only `ir`, so its first import of `compilers` or
   `compilers/compile` fails with a message naming that allowlist.
   `TestImportGraph_EveryPackageIsRuledOrExempt` does not fire for a package under `compilers/`.
3. **Mint IDs and names through the framework.** Intern types through `compile.Types`, derive IDs
   and canonical names with `compilers/compile`, declare in `Document.IDSpaces` every namespace an
   ID lives in, source-addressed as well as minted (`ir-design.md` §3.1), and stamp
   `ir.IRVersion`. Call `compile.Types.Violations()` after interning and report what it returns, as
   the OpenAPI compiler does; take the `*compile.Types` as a parameter, as that compiler's `run`
   does, so a test can drive the refusal no source provokes.
   *Fails if missed:* `TestNamingGrammar_NameChannelsAreFilledByTheFrameworkOnly`,
   `TestIDGrammar_CompilersDeriveIDsThroughTheFramework` and the `registryWrites` lint rule
   (`make lint-planted`) for the framework. Nothing in the repository checks the namespaces, the
   version or the violations (#967): step 5's `irverify` tests do, and `irtest.SpacesUsed` holds
   `IDSpaces` to what the output uses.
4. **Register it** in `engine.New` (`engine/engine.go`), the one place built-in compilers are
   listed, and add the package to the `engine` entry of the `rules` map.
   *Fails if missed:* `TestImportGraph_LayeringHolds/engine` for the allowlist. Nothing fails if the
   compiler is never registered, so add an engine test that compiles a document of the format.
5. **Test it** with golden snapshots through `ir/irtest` and `irverify` over its output. Keep its
   fixtures and goldens under `compilers/<format>/testdata/`: the harness sweeps the repository's
   `testdata/` through the OpenAPI compiler alone, and its determinism and order-invariance oracles
   reach no other compiler yet (#968), so write those checks yourself. A goroutine needs
   `internal/leakcheck` in the package's `TestMain`.
   *Fails if missed:* `TestHarness_InRepoCorpus` for a fixture under the root `testdata/`, and
   `TestGoroutineLeak_EveryGoStatementHasALeakCheck`.
6. **Write its lowering specification** at `docs/compilers/<format>.md`, and link it from
   `ir-design.md` §14: move the format's list there into the new document, or add a row for a
   format §14 does not list. The `TestLoweringSpec` tests read `docs/compilers/openapi.md` alone, so
   give the new specification tests of its own, reading it through `internal/keydoc` as they do.
   Nothing fails if this step is missed.
7. **Run the gate:** `make gate`.
