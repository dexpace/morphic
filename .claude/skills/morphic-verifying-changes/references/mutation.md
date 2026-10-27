# Planting defects

`scripts/mutate.py` handles the mechanics: one literal replacement, a compile check, the test run,
a restore whatever happens, and a verdict. These rules are the ways a verdict still lies.

## Route every temporary edit through a restoring script
`mutation-restore-always`

- **Rule.** Plant every temporary edit, measurement switches included, through `scripts/mutate.py`
  or another script that restores on exit. After a batch, `git diff` each touched file and search
  the tree for each planted string.
- **Why.** A hand-rolled backup restored from the wrong copy and left a switch planted (#591); two
  files sharing a basename shared one backup, so a restore put one package's file in another
  (#566); a full disk stopped a restore mid-batch (#1141).
- **Probe.** `git status --short` lists nothing you did not mean to change.

## A mutant that does not compile proves nothing
`mutation-must-compile`

- **Rule.** Build the mutant before reading results, and adjust it until it compiles (`_ = x` for
  an unused variable). A run that reports a kill with no `--- FAIL` line is unverified.
- **Why.** Unused variables and imports turned mutants into build failures that read as "not
  caught" (#321, #322) and as "killed" with an empty failure list (#1100).
- **Probe.** `mutate.py` refuses a mutant whose package and tests do not build.

## Require that tests ran, and run a control that must survive
`mutation-controls`

- **Rule.** Trust a batch only when the run printed at least one `ok` line and a no-op control
  (a changed comment) survived. Keep probe programs outside the module tree.
- **Why.** A quoting slip made `go test` match no packages, so every mutant read as killed (#591).
  A stray probe `main` package in the worktree kills every mutant through archtest.
- **Probe.** Run the control first.

## A survivor of one package is not yet a gap
`mutation-survivor-scope`

- **Rule.** Before calling a survivor untested, run it over `go test ./...` and ask whether
  anything can observe it. An unobservable mutant is equivalent: record it with the reason.
- **Why.** Two of three "untested" survivors were killed in another package, and the third was
  equivalent (#825).

## A kill by panic hides whether the pinning test pins
`mutation-crash-kill`

- **Rule.** When a mutant drops part of a key or identity, keep the data the reporting code reads
  and change only what is looked up. Then run the test written for that property alone.
- **Why.** A nil field panicked the binary, so the test written for exactly that property never
  reported (#737). A kill can also come from parallel subtests sharing a fixture slice rather
  than from the guard (#805).
- **Probe.** Read which test failed and why, not only that the run failed.

## Mutate each arm, each overwrite and each early return
`mutation-per-arm`

- **Rule.** For a disjunction, mutate each arm on its own. For a loop that folds a flag over
  siblings, plant an overwrite (`x = f()`) and an early return, and order the fixture with the
  flagged sibling first and a clean one after it, at every level the loop runs.
- **Why.** The arm where normalisation differed had no test (#731). Four OR-accumulator mutants
  survived 100% coverage because every fixture had one sibling (#814).

## Force an untaken guard open; coverage cannot see it
`coverage-untaken-guard`

- **Rule.** For a guard with no else-statement that a change keeps or rewrites, plant
  `cond || true` and require a red test.
- **Why.** Coverage read 100% while no test took the false side of a `CutLast` guard (#524), and a
  pure optimization filter survived "always true" (#566).

## Lower each bound by one, at each site
`bound-lowered-by-one`

- **Rule.** Pin each bound at exactly the limit and one past it, and assert that the production
  constructor wires the constant. Plant "lowered by one" at each site separately, on the
  arithmetic, not through a type that clamps it. Name in the PR any constant past test scale.
- **Why.** Ten bound sites survived on `main` because their tests used budgets far from the edge or
  built their own (#1140). `AtMost(-1)` clamps to zero, so planting through it changed nothing.

## A guard moved to N callers needs N tests
`split-guard-per-site`

- **Rule.** When a refactor moves a check from a choke point to its callers, keep a choke point, or
  plant the dropped check at every caller and add a test wherever one survives.
- **Why.** Two of six copied `ok` checks survived every test; the shared guard had been tested
  through one site (#1088, #1140).

## Test a cut at every alignment
`cut-every-alignment`

- **Rule.** For code that cuts on a boundary, put the bound at each byte of a multi-byte rune (pad
  0–3 bytes before a 4-byte rune), and add a run of continuation bytes for the start-of-text guard.
- **Why.** Four green tests put the bound one byte into a rune, so `for` → `if` and a dropped
  `n > 0` both survived (#1130).

## A case that names a fixture checks the fixture exists
`fixture-exists`

- **Rule.** A table case naming its own fixture asserts the fixture exists before running anything.
- **Why.** A misspelt fixture made the tool fail on a missing path, which satisfied the case's
  "fails" assertion.
- **Probe.** Rename one fixture; the case must fail with a reason about the fixture.

## Name fixtures so the right answer sorts last
`sort-order-fixture`

- **Rule.** When a change sorts for determinism, give each rule a fixture in which the semantically
  right site sorts last (declared at `/z`, reused at `/a`).
- **Why.** Pointer order picked the right site by coincidence in every fixture while the rule was
  not implemented (#548). The two-order oracle cannot see this.

## Retyping a field can make testify assertions vacuous
`testify-typed-field`

- **Rule.** After retyping a field, and after every merge of main into it, scan testify calls whose
  two sides have different types, with a `go/packages` scan validated on a planted case.
- **Why.** testify compares dynamic types, so `NotEqual(t, "/a", p.Pointer)` always passes once
  the field is a `jsontext.Pointer` (#549). grep cannot tell.

## A crash oracle reads the crash, not the exit status
`crash-oracle-text`

- **Rule.** Classify a subprocess crash by its output (`fatal error: stack overflow`), never by
  exit status 2 alone, which every panic also gives.

## A closure in a parallel subtest must not use the parent's t
`parent-t-in-subtest`

- **Rule.** Build files and directories once in the parent; a closure that needs a `t` takes the
  subtest's.
- **Why.** A `require` on the outer `t` inside a parallel subtest turned a failure into
  `panic(nil)`, and the loop created temp files for every check (#1131).
- **Probe.** Read the enclosing closure of each `t.TempDir()` in the test diff.

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
  Tidying the fixture removes the guard without any test noticing.

## Temporary instrumentation needs -v
`instrument-with-v`

- **Rule.** Run a temporary `println` probe with `go test -v`: without it, passing packages print
  nothing, and only the failing package's hits appear. Attribute a hit to its test from the
  `runtime.Callers` frame in a `_test.go` file.
