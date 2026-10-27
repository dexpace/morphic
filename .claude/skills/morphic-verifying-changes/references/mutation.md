# Planting defects

`scripts/mutate.py` handles the mechanics: one literal replacement, a compile check, the test run,
a restore whatever happens, a verdict, and the run's whole output kept in a file. These rules are
the ways a verdict still lies.

## Route every temporary edit through a restoring script
`mutation-restore-always`

- **Rule.** Plant every temporary edit of a tracked file, instrumentation and measurement switches
  included, through `scripts/mutate.py`, which restores it whatever happens and keeps the output
  they print. Delete a scratch file you added before anything is committed. After a batch,
  `git diff` each touched file and search the tree for each planted string.
- **Why.** A hand-rolled backup restored from the wrong copy and left a switch planted (#591); two
  files sharing a basename shared one backup, so a restore put one package's file in another (#566);
  a full disk stopped a restore mid-batch (#1141).
- **Probe.** `git status --short` lists nothing you did not mean to change.

## A mutant that does not compile proves nothing
`mutation-must-compile`

- **Rule.** Build the mutant before reading results, and adjust it until it compiles (`_ = x` for an
  unused variable). A run that reports a kill with no `--- FAIL` line is unverified; `mutate.py`
  reports both as UNVERIFIED rather than as a verdict.
- **Why.** Unused variables and imports turned mutants into build failures that read as "not caught"
  (#321, #322) and as "killed" with an empty failure list (#1100).

## Require a clean baseline and a control that must survive
`mutation-controls`

- **Rule.** Plant a set of mutants with `scripts/mutate.py --batch`, which judges none unless the
  command passes on the clean tree and runs a test, and include a no-op control (a changed
  comment) that must survive. Keep probe programs outside the module tree.
- **Why.** A quoting slip made `go test` match no packages, so every mutant read as killed, and a
  stray probe `main` package in the worktree kills every mutant through archtest (#591).

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
