# Contributing to Morphic

This guide is the way in. It says what to read for the change you are making, then walks the three
changes most contributions are: a lowering rule in the OpenAPI compiler, a pass, and a compiler,
and ends with merging a dependency update. Each step names the check that fails when the step is
missed, so a red test points back to the step, and says so where nothing checks it, so a green run
is not read as more than it proves. The rules themselves live in the root guidance,
[`CLAUDE.md`](CLAUDE.md), and in the design documents under [`docs/`](docs/); this guide links them
rather than restating them.

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

### Where a test goes

- **A unit test sits beside the code it tests,** in its package, in the `_test.go` named after the
  file that declares what the test's name says it tests, as `TestTypes_RegisterTakesNoCoordinate`,
  of `Types` in `compilers/compile/types.go`, sits in `types_test.go`. Once a file's tests split by
  topic, each part keeps that prefix, as `load_entry_internal_test.go` does beside `load.go`. A
  scenario several files of a package decide together goes in a file named for the scenario, as
  `pass/validate_propids_test.go` is, a check over a whole package in one named for what it checks,
  as `ir/nofloat_test.go` is, and in a package of one source file a test file needs no prefix.
  *Fails if missed:* `make coverage`, for a test in another package than its code, since coverage
  counts only a package's own tests. Nothing fails for a test in the wrong file of its package.
- **A test is external, `package x_test`, unless it cannot be.** It reaches what it must of the
  package's internals through the package's `export_test.go`, which names each one once, under its
  exported spelling or, where that is taken, a name that says what it adds, as
  `irtest.CompareGoldenOn` does. A test that needs an unexported field or method, a generic, or a
  variable it assigns stays inside the package, in `x_internal_test.go`, and so do a package's
  tests when moving them out would take more than ten aliases, enough to make a second API, as
  `schema`'s and `operation`'s would. A fixture both sides use is declared once, exported, in
  `export_test.go`, as `schema.Lowerer` is. A `cmd` program's tests are all inside it, under plain
  names, since a `main` package cannot be imported.
  *Fails if missed:* nothing. A test written inside the package that could be external compiles and
  runs.
- **A test is named for what it tests.** A `TestFunc_Scenario` name is a function's or a type's, and
  the file follows from it as above. A test that pins a construct or a lowering rule may take the
  construct's name instead, as the `TestPathItemRefSiblings` tests do, and then sits in the file of
  the construct or of the entry point it runs through. A function that asserts on a test's behalf
  calls `t.Helper`, except one a table runs as a case's body, as the conformance corpus's assertions
  and the canaries are: it is the case, so its failure reports the line in it that fails.
  *Fails if missed:* nothing checks a test's name or its `t.Helper`.
- **A helper is written once.** One that several test files of a package share sits in one of
  them or, when it is new, in the package's `helpers_test.go`, or `helpers_internal_test.go` inside
  the package. One that several packages share is in `compilers/openapi/internal/openapitest` or
  `ir/irtest`, as `openapitest.SchemaFromYAML` and `irtest.RepoRoot` are, when it builds or reads
  what those packages serve, a spec and its diagnostics or the IR. One that belongs there but
  cannot live there, as `parseFull`, which compiles through `openapi.New`, is written in each
  package that needs it, and each copy says why. A helper outside what either serves, such as a
  file read or a stub compiler, is written where it is needed, and so is one whose body is a single
  expression, such as `ir.TypeRef{Target: id}`, which holds no behaviour for a copy to drift from.
  *Fails if missed:* nothing. A copy compiles beside the original.
- **An end-to-end test of the OpenAPI compiler goes in `compilers/openapi/e2e`,** in a file named
  for the construct or the property it checks: a test of what the compiler does as a whole, which no
  one package decides, such as the conformance corpus, the goldens, fuzzing, benchmarks and a
  scenario several packages settle together. A test of one package's code stays in that package even
  when it compiles a whole spec through `openapi.New` to reach the code, as `internal/schema`'s
  `parseFull` tests do, since only there does it count toward that package's coverage. The e2e
  package holds no code, so its tests cover nothing, and the code they reach still needs a unit test
  beside it; `compilers/openapi` itself holds only the tests of its own files. A helper several of
  the package's files share is in its `helpers_test.go`, except the corpus's own reader, which
  `conformance_test.go` declares, and a fixture the files of one construct share, which stays with
  that construct.
  *Fails if missed:* `make coverage`, for code only an end-to-end test reaches. Nothing fails for an
  end-to-end test written in the compiler's own package or in one of its internal packages.

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
   that hold the code, and `micro-compiler-design.md` says why each package holds what it does.
   Within the package, the code joins the file that lowers its construct, as an allOf rule's code
   joins `schema`'s `allof.go`, or, in a package split by responsibility, the file of the
   responsibility it serves, as a visibility rule's joins `merge`'s `visibility.go`. A construct or
   responsibility no file holds yet starts a file named for it, however small, and a helper goes
   with what it serves, not with its caller. The
   parser library's model can read an absent field and an empty one alike, so check what it
   returns before you branch on it. The root guidance's caps apply: no function over 70 lines or a
   cognitive complexity of 20, no type under `compilers/` over 20 methods, no doc comment over 100
   words, and no lint suppression in the source.
   *Fails if missed:* `make lint`, `TestMethodsPerType_StayUnderTheCap`,
   `TestDocComments_StayUnderTheCap`, `TestSuppressions_NoDirectiveStandsInTheSource`. Nothing
   fails for code in the wrong file of its package.
4. **Add a conformance case and a unit test.** Write a minimal spec at
   `testdata/conformance/openapi/<name>.yaml` that exercises the rule, add an entry to
   `conformanceCases()` in `compilers/openapi/e2e/conformance_test.go` naming its assertion, list it
   under each [`ir-spec-matrix.md`](docs/ir-spec-matrix.md) row it witnesses in
   `testdata/conformance/openapi/matrix.md` (none, for a case that pins only a diagnostic), write
   the assertion in the `conformance_<family>_test.go` of its construct, and name the case in the
   rule's Cases field. Then write its golden alone, since
   a run that also reaches `TestConformance_TableNamesEveryCorpusSpec` reads the directory before
   the golden exists:

   ```bash
   go test ./compilers/openapi/e2e -run '^TestConformance$/^<name>$' -update
   ```

   Coverage is counted per package, and the case runs in `compilers/openapi/e2e`, which holds no
   code of its own, so it covers nothing: the code you wrote also needs a test in its own package,
   placed as [Where a test goes](#where-a-test-goes) says, which the rule's Tests field then names.
   *Fails if missed:* `TestConformance_TableNamesEveryCorpusSpec` for a spec with no entry,
   `TestConformance/<name>` for a golden that does not match, `TestLoweringSpec_CasesAreTheCorpus`
   for a case no rule names, `TestConformance_EveryRegisteredFormatIsHeldToItsColumn` for a name in
   `matrix.md` that is no spec and for a row that has both a witness and a reason, and
   `make coverage` for a statement only the case reaches. Nothing fails for a spec that witnesses a
   row but is not listed under it.
5. **Regenerate what the change moves, and read the diff.** Other goldens may change too:

   ```bash
   go test ./compilers/openapi/e2e -run 'TestGolden|TestConformance' -update
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
   `TestLibraryBehaviour` and `TestResolverOracle_VerdictMatchesResolverBehavior` can read a slow
   child as hung under `make coverage` (#1010); rerun before reading that failure as yours, and
   before recording it with `-update`.

## Add a pass

A pass is an IR-to-IR step the engine runs after a compile
([`architecture.md`](docs/architecture.md) §2.2). An analysis reports on the document it is handed;
a transform returns a new one. A transform changes types through an `ir.Editor` (`ir.NewEditor`):
`UpdateType` hands it a deep copy of a node to change, `ReplaceType` puts a node built afresh in
place of the one at its ID, `RemoveType` removes one, and `Finish` returns the new document, sharing
everything the edit left alone, beside an `ir/edit-refused` error for each call the editor refused,
such as a write at an ID the document does not hold. Building a type registry, or writing one of its
entries, any other way fails the `registrywrites` rule (`internal/lintrules`, run by `make vet`). No
rule sees a node of the input changed in place, which `UpdateType`'s copy is there to make needless.

1. **Implement `pass.Pass`** in a file of its own under `pass/`, shaped as `Validator` and
   `ValidateName` are in `pass/validate.go`: an exported type, and a constant holding its stable
   `Name()`, unique among the engine's passes. A pass whose rules fall into families keeps each
   family in a `<name>_<family>.go` beside that file, as `validate` does, and a new rule joins the
   family it belongs to or starts one. `Run` never mutates its input, returns its input when it only
   analyses, and reports through its diagnostics alone. `ir.ForEachOperation` walks the
   operations, and the package's `diag` helper in `pass/diag.go` builds a diagnostic on a node.
   A code names the defect, as `pass/doc.go` says: `pass/<defect>` for a finding the pass owns,
   `ir/<defect>` for a structural one `irverify` reports too. A transform returns what `Finish`
   returns, its own findings appended.
   *Fails if missed:* the build, once step 3 lists the pass, for a method `pass.Pass` requires, and
   `make vet` for a registry built, or an entry of one written, other than through an `ir.Editor`.
   Nothing fails for a rule in another family's file.
2. **Test it** beside it in `pass/`, table-driven, against documents built in the test: a file's
   cases in the `_test.go` named after it, and a scenario that several files decide in one named for
   the scenario, as `validate_propids_test.go` is. `pass/validate_test.go` has builders (`validDoc`,
   `docWithOperation`) and readers (`codes`, `countCode`) to reuse.
   *Fails if missed:* `make coverage`. Nothing fails for a case in another file's `_test.go`.
3. **Add it to the default list**, `engine.Passes` in `engine/passes.go`, before `validate`, which
   stays last. No test restates the list: `TestPasses_ValidateRunsLast` holds `validate` to the end,
   `engine.New` refuses a name two passes share, and `TestRun_UnknownPassIsMisuse` in `cmd/morphic`
   reads the names a refusal lists from the list itself. Nothing fails if the pass never joins the
   list, where it simply never runs. An embedder runs a pass of its own without editing the list,
   through `engine.WithPasses`.
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
   methods of `compilers.Compiler`, and the `Options` type its `FormatOptions` holds. Before writing
   a YAML reader, a version sniff or a test of document identity of its own, read what
   [`architecture.md`](docs/architecture.md) §2.1 says stays in the OpenAPI compiler, and why.
   *Fails if missed:* `make lint` for a package with no package comment, and the build, once step 4
   registers the compiler, for a missing method.
2. **Rule its imports.** Add `compilers/<format>` to the `rules` map in
   `internal/archtest/arch_test.go`, allowing what it imports and nothing more: `ir`, `compilers`
   and `compilers/compile` by exact path, its own internal tree once it has one, and its format
   libraries. Give each package under its `internal/` an entry of its own. Run `gofmt -w` on the
   file afterwards: a longer key realigns its neighbours.
   *Fails if missed:* `TestImportGraph_EveryPackageIsRuledOrExempt`, which requires every package
   under `compilers/` to carry an entry of its own and names the one that does not, and
   `TestImportGraph_LayeringHolds` for an import an entry does not allow, whose message says when
   the package is audited under an ancestor's entry for want of its own, and for an entry no
   production file the rule audits imports.
3. **Mint IDs and names through the framework.** Intern types through `compile.Types`, derive IDs
   and canonical names with `compilers/compile`, declare in `Document.IDSpaces` every namespace an
   ID lives in, source-addressed as well as minted (`ir-design.md` §3.1), and stamp
   `ir.IRVersion`. Once the last lowering returns, take the registry from `compile.Types.Finish` and
   report the diagnostics it returns beside it, as the OpenAPI compiler's `run` does: they carry a
   `compile/registry-refused` error for each entry the registry refused. Take the `*compile.Types`
   as a parameter, as `run` does, so a test can drive the refusal no source provokes. Name each
   source in `Document.Sources` with the format it was recognized as and `ir.SourceHash` of its
   bytes, in `SourceTable`'s order, and after them each document a reference read through
   `compilers.Options.Reader`.
   *Fails if missed:* the `namechannels`, `idderivations` and `registrywrites` rules in
   `internal/lintrules` (`make vet`) for the framework; `irverify`, in step 5's tests, for the
   namespaces, the version, the services and a malformed source table, with `irtest.SpacesUsed`
   holding `IDSpaces` to what the output uses; and `compilers.Detection.Compile`, which every engine
   run compiles through, for a source table that does not describe the inputs. The refusals cannot
   be dropped apart from what settling reports, since `Finish` returns the two as one list.
4. **Register it** in `engine.Compilers` (`engine/engine.go`), the one place built-in compilers are
   listed, and add the package to the `engine` entry of the `rules` map. A registered compiler is
   also held to its column of [`ir-spec-matrix.md`](docs/ir-spec-matrix.md): end the format's
   column header with its registered name in code, and write
   `testdata/conformance/<format>/matrix.md`, one entry for each row the column marks expressible,
   as `testdata/conformance/openapi/matrix.md` does.
   *Fails if missed:* `TestConformance_EveryRegisteredFormatIsHeldToItsColumn` for the column and
   the document, and `TestImportGraph_LayeringHolds/engine` for the allowlist. Nothing fails here
   if the compiler is never registered, though step 5's `TestHarness_InRepoCorpus` does once its
   fixtures are under `testdata/`, so add an engine test that compiles a document of the format.
5. **Test it** with golden snapshots through `ir/irtest` and `irverify` over its output. Put its
   corpus under the repository's `testdata/` (the conformance corpus is
   `testdata/conformance/<format>/`): the harness sweeps every spec there through whichever
   registered compiler takes it, with every oracle, so registering the compiler in step 4 is all
   it takes to be swept. Order-invariance permutes YAML and JSON mappings, so a source that is
   neither is not permuted; write that check yourself. A goroutine needs `internal/leakcheck` in
   the package's `TestMain`.
   *Fails if missed:* `TestHarness_InRepoCorpus`, which sweeps the corpus and requires every
   fixture in it to be listed, so one that no registered compiler recognizes fails there instead of
   dropping out of the sweep, and `TestGoroutineLeak_EveryGoStatementHasALeakCheck`.
6. **Write its lowering specification** at `docs/compilers/<format>.md`, and link it from
   `ir-design.md` §14: move the format's list there into the new document, or add a row for a
   format §14 does not list. The `TestLoweringSpec` tests read `docs/compilers/openapi.md` alone, so
   give the new specification tests of its own, reading it through `internal/keydoc` as they do.
   Nothing fails if this step is missed.
7. **Run the gate:** `make gate`.

## Merge a dependency update

Dependabot opens the pull requests [`.github/dependabot.yml`](.github/dependabot.yml) asks for each
week: the Go modules' minor and patch updates together, the parser library and any major update
each on its own, and the workflow's actions together. It proposes direct modules only, so an
indirect one such as go-yit, which `go.mod` holds back (#798), moves only when a direct module
requires it. Every proposal waits a week after its release. A security update opens when an
advisory names a module in `go.mod`, direct or indirect, and does not wait. Nothing proposes a Go or
golangci-lint release: the `Makefile` comment above `GO_VERSION` says what moves with them, by hand.

1. **Re-read [`docs/parser-library.md`](docs/parser-library.md)** when the update is to the parser
   library, following its steps to bump the library, and push the result onto Dependabot's branch.
   *Fails if missed:* `TestRecord_EntriesAreVerifiedAtTheLinkedVersion`, for each entry verified at
   the previous version.
2. **Finish the toolchain move** when an update raises the `go` directive, which `go get` does for a
   module that needs a newer Go: the pins the `Makefile` lists above `GO_VERSION` move on the same
   branch. Nothing is sure to fail if this step is missed; an old pin fails the gate only when it
   cannot read the new toolchain.
3. **Keep what you push.** Pushing to a Dependabot branch stops Dependabot rebasing it, so bring
   `main` in by merging it, as on any branch. `@dependabot recreate` overwrites what was pushed.
   Nothing checks this.
4. **Set the squash subject when merging.** A Dependabot pull request has one commit, and this
   repository squashes a one-commit pull request under that commit's subject rather than the pull
   request's title, so retitling the pull request changes nothing. A subject naming one module
   often passes the 72-character cap once the number is appended; shorten it in the merge dialog,
   or with `gh pr merge <n> --squash --subject "build: bump <module> to <version>"`. Nothing checks
   this.
