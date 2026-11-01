# Planting defects

Plant a defect in a throwaway worktree, run the real test command there, and read its output
yourself. These rules are the ways that reading still lies.

## Plant every temporary edit in a throwaway worktree
`plant-in-a-throwaway-worktree`

- **Rule.** Plant a mutant, instrumentation or a measurement switch in a detached worktree, never
  in the tree you work in: `git add` new files, then
  `git worktree add --detach <dir> $(git stash create)`, which holds uncommitted work without
  touching the stash list. Put a file back with `git -C <dir> checkout -- <file>`, and remove the
  worktree at the end. Run Python with `PYTHONDONTWRITEBYTECODE=1`.
- **Why.** Restores in the working tree failed: a backup restored from the wrong copy (#591), one
  backup shared by two files of one basename (#566), a full disk that left a mutant planted (#1141)
  and emptied a file mid-restore (#1143), and bytecode outliving a same-size Python mutant restored
  within its second (#1143).
- **Probe.** `git status --short` in your own tree lists only your changes, and
  `git worktree list` no longer names the throwaway.

## Plant in an unpacked copy where the repository must stay unwritten
`plant-in-an-unpacked-copy`

- **Rule.** `git worktree add` writes metadata into the repository. Where that is ruled out, unpack
  a copy with `git archive <head> | tar -x -C <dir>`, then `git init`, `git add -A` and a commit
  there, since `git init` alone leaves `status` and `diff HEAD` nothing to compare.
- **Why.** Reviews improvised this, and one wrote the metadata its round had ruled out (#1233,
  #1235, #1238).
- **Probe.** Plant a line in the copy: `status` names one file.

## Check the tree holds one mutant before reading its verdict
`one-mutant-per-verdict`

- **Rule.** Before reading a mutant's verdict, check that the tree holds that mutant and nothing
  else: `git -C <dir> status --short` names only its file, and `git -C <dir> diff HEAD` shows its
  one change to a tracked file, since `git diff` alone hides a staged or untracked plant. A loop
  planting several runs the check every round and reads no verdict past a failed one. A plant
  removing a call removes the call: one discarding its result or error still runs it.
- **Why.** A plant loop restored from a path it never wrote, so six mutants stacked, each reading
  as a kill, and every verdict after the first was void (#1153). A plant ignoring a call's error
  left the call running, and read as a hole in a PR's central claim (#1233).

## Read a kill in the output, never in the exit status
`read-the-kill-in-the-output`

- **Rule.** A mutant is killed only where the output names a failing test (`--- FAIL: <test>`, or
  unittest's `FAIL:` or `ERROR:`) and the mutant compiled. A `[build failed]` or `[setup failed]`
  line, a `# <package>` header, a package `FAIL` with no failing test above it (an `os.Exit`, a
  timeout), `[no tests to run]`, `Ran 0 tests` and an `ERROR` naming
  `unittest.loader._FailedTest`, a module that did not load, are no verdict. Adjust a mutant
  until it compiles (`_ = x` for an unused variable).
- **Why.** Unused variables and imports turned mutants into build failures that read as "not caught"
  (#321, #322) and as "killed" with an empty failure list (#1100).

## Rebuild a plant set nobody kept, rather than carrying its result over
`rebuild-lost-plants`

- **Rule.** A PR body cites plants by count and class, and the script that made them is gone. When
  a merge or a review asks for them again, rebuild a set from the conditions the change adds and run
  it once before merge, even after a merge that touched only comments: a count carries nothing
  over.
- **Why.** A body cited 70 plants by class and the repository held none; a set rebuilt from the diff
  found a check no test held, which none of them had planted (#1191, #1216).
- **Probe.** `git grep -l '<condition the change adds>' <head> -- '*.py' '*.sh' '*.txt'` shows
  whether a set survives, and the report lists each condition the diff adds beside its plant.

## Rerun only the plants a merge can have changed
`verdict-outlives-unchanged-files`

- **Rule.** A plant at hand keeps its verdict across a merge that changed neither the file it
  mutates nor the test that killed it, with the fixtures it reads. Rerun the rest, and every plant
  after a merge rewriting what lies between them, and name each verdict's revision in the body. A
  set nobody kept has no verdicts (`references/mutation.md#rebuild-lost-plants`).
- **Why.** A merge that changed no lowering left 79 plants at 100 to 170 s each, about 3.5 hours,
  where a diff left 23 (#1191, #1216). A diff of production files alone misses a merge removing the
  test that held a kill.
- **Probe.** `git diff --stat <verdict's revision> <head> -- <mutated file> <test's file>` prints
  nothing for each verdict carried over.

## Plant through an overlay only where no test reads the source
`overlay-reaches-the-compiler-alone`

- **Rule.** `go test -overlay=<file>`, with `{"Replace": {"<absolute path>": "<planted copy>"}}`,
  writes no tree, so several plants run at once beside a control. It reaches the compiler alone: a
  test reading sources at run time, as `internal/archtest` and `internal/decls` do, reads the disk
  and passes. Re-plant in a worktree before calling such a survivor untested.
- **Why.** Six at once took 48 to 121 s a plant on a loaded machine (#1216), and a `//nolint` line
  and a new `TypeKind` each passed through an overlay and failed on disk (#1216).
- **Probe.** Plant `//nolint:errcheck` above `package pass` in `pass/doc.go` through an overlay:
  `TestSuppressions_NoDirectiveStandsInTheSource` passes, and fails with the same line on disk.

## Require a clean baseline and a control that must survive
`mutation-controls`

- **Rule.** Run the test command on the clean worktree first: it passes and runs a test. Run it
  bare, since a pipe or `;` hands back another command's status. Include a no-op control (a
  changed comment) that must survive. Keep probe programs outside the module tree.
- **Why.** A quoting slip made `go test` match no packages, so every mutant read as killed, and a
  stray probe `main` package in the worktree kills every mutant through archtest (#591); a run
  piped into `cat` exited 0 past a failing test (#1143).

## A survivor of one package is not yet a gap
`mutation-survivor-scope`

- **Rule.** Before calling a survivor untested, run it over `go test ./...` and ask whether anything
  can observe it. An unobservable mutant is equivalent
  (`references/mutation.md#equivalent-means-every-input`). An arm's removal alone does not make it
  so: plant a changed result too. For a helper in a `_test.go` file,
  `go test` on its directory and on `./internal/archtest` is that run: no other package compiles the
  file, `export_test.go` included, and archtest reads it as text.
- **Why.** Two of three "untested" survivors were killed in another package, and the third was
  equivalent (#825). The rule asked for a module run per survivor where
  `go test ./internal/archtest` settled three (#1233), yet a `t.Helper()` deleted in
  `pass/validate_payloads_test.go` passes `go test ./pass` and fails archtest. Two arms recorded
  equivalent by removal alone had results a row saw (#1233).

## Equivalence is a claim about every input
`equivalent-means-every-input`

- **Rule.** Record a survivor as equivalent only with the reason no input can observe it. A green
  suite answers for the inputs it holds, so first compile inputs across the construct's domain, in
  both orders where order could matter, and diff a build holding the mutant against a clean one.
- **Why.** A guard's removal passed `go test ./...` and was recorded as equivalent, while
  `{allOf: [{format: int32}, {type: integer}]}` then lowered to `any` and the swapped order to
  `integer` (#1254, #1269).
- **Probe.** Each equivalent the report lists states its reason, and names the inputs swept beside
  the diff that came back empty.

## A kill by panic hides whether the pinning test pins
`mutation-crash-kill`

- **Rule.** When a mutant drops part of a key or identity, keep the data the reporting code reads
  and change only what is looked up. Then run the test written for that property alone.
- **Why.** A nil field panicked the binary, so the test written for exactly that property never
  reported (#737). A kill can also come from parallel subtests sharing a fixture slice rather than
  from the guard (#805).
- **Probe.** Read which test failed and why, not only that the run failed.

## Mutate each arm, each overwrite and each early return
`mutation-per-arm`

- **Rule.** For a disjunction, mutate each arm on its own. For a loop that folds a flag over
  siblings, plant an overwrite (`x = f()`) and an early return, and order the fixture with the
  flagged sibling first and a clean one after it, at every level the loop runs.
- **Why.** The arm where normalisation differed had no test (#731). Four OR-accumulator mutants
  survived 100% coverage because every fixture had one sibling (#814).

## A guard moved to N callers needs N tests
`split-guard-per-site`

- **Rule.** When a refactor moves a check from a choke point to its callers, keep a choke point, or
  plant the dropped check at every caller and add a test wherever one survives. An effect turned
  into a return value is the same move
  (`.claude/skills/morphic-openapi-compiler/references/lossless-and-diagnostics.md#every-return-path-delivers-diagnostics`).
- **Why.** Two of six copied `ok` checks survived every test; the shared guard had been tested
  through one site (#1088, #1140).

## Mutate the library through -modfile to prove a canary
`library-mutants-modfile`

- **Rule.** To prove a test reddens when the library changes, copy the module, write an alternate
  go.mod that replaces the library with the copy, and run `go test -modfile=<alt.mod>`; the
  repository's go.mod is never touched. Run the same library mutant against `main`'s test too: a
  test that already reddened there proves nothing new.
- **Why.** On #942, `main`'s scan test already reddened under the decode mutants; only a
  resolver-only mutant separated old from new. Tests that load sources through `go/packages` read
  the module cache and ignore `-modfile`.

## Mutate each table entry before tidying shared fixtures
`table-entry-before-tidying`

- **Rule.** Before cleaning up fixtures that a classification table (an ID class, a field kind)
  reads, give each entry its own case that plants a misclassification, and prove each plant fails
  exactly that case.
- **Why.** An entry can be guarded only by accident: an unrelated fixture that happened to fail.
  Tidying the fixture removes the guard without any test noticing (#544).
