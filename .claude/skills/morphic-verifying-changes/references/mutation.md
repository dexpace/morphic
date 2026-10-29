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
  can observe it. An unobservable mutant is equivalent: record it with the reason.
- **Why.** Two of three "untested" survivors were killed in another package, and the third was
  equivalent (#825).

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
  plant the dropped check at every caller and add a test wherever one survives.
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
