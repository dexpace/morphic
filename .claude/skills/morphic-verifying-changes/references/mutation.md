# Planting defects

Plant a defect in a throwaway worktree, run the real test command there, and read its output
yourself. These rules are the ways that reading still lies.

## Plant every temporary edit in a throwaway worktree
`plant-in-a-throwaway-worktree`

- **Rule.** Plant a mutant, instrumentation or a measurement switch in a detached worktree, never in
  the tree you work in: `git add` new files, then
  `git worktree add --detach <dir> $(git stash create)`, which holds uncommitted work without
  touching the stash list. Restore with `git -C <dir> reset -q --hard && git -C <dir> clean -qfd`,
  which undoes a `git mv` too, and remove the worktree at the end. Run Python with
  `PYTHONDONTWRITEBYTECODE=1`.
- **Why.** Restores in the working tree failed: a backup from the wrong copy (#591), one backup
  shared by two same-named files (#566), a full disk (#1141, #1143), bytecode outliving a same-size
  Python mutant (#1143), and a `git mv` plant surviving `checkout` (#1541).
- **Probe.** `git status --short` in your own tree lists only your changes, and
  `git worktree list` no longer names the throwaway.

## Plant in an unpacked copy where the repository must stay unwritten
`plant-in-an-unpacked-copy`

- **Rule.** `git worktree add` writes metadata into the repository. Where that is ruled out, unpack
  a copy with `git archive <head> | tar -x -C <dir>`, then `git init`, `git add -A` and a commit
  there, since `git init` alone leaves `status` and `diff HEAD` nothing to compare. The copy holds
  no history, so it serves a plant alone; a review reading the branch's commits takes the clone,
  made with `git clone -n`, that
  `.claude/skills/morphic-reviewing-prs/references/rounds.md#review-without-origin` names.
- **Why.** Reviews improvised this, and one wrote the metadata its round had ruled out (#1233,
  #1235, #1238). In a copy, `subjects.py` read only the copy's own commit, and the merge of `main`
  refused unrelated histories (#1537).
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
  line, a `# <package>` header, a package `FAIL` with no failing test above it (an `os.Exit`),
  `[no tests to run]`, `Ran 0 tests`, an `ERROR` naming `unittest.loader._FailedTest`, a module
  that did not load, and what `references/mutation.md#run-a-limit-or-a-stray-panic-ended` names are
  no verdict. Adjust a mutant until it compiles (`_ = x`). A test binary's flag, such as `-update`,
  follows the package, or `go` ends its own flags there and tests `.`.
- **Why.** Unused variables and imports turned mutants into build failures read as "not caught"
  (#321, #322) and as "killed" with no failure listed (#1100); `-update` before the package failed
  setup (#1555).

## A run a limit or a stray panic ended is read by what it printed
`run-a-limit-or-a-stray-panic-ended`

- **Rule.** A run that exits 124 or 137, the codes `timeout` and a `KILL` give, or prints fewer
  package lines than it was given packages, is no verdict: an outer limit ended it before any
  `FAIL`. A panic with no `--- FAIL` above it, as one off the test goroutine prints, is a kill only
  where its stack passes through the plant, and is reported as one whose test goes unnamed.
- **Why.** In a scratch module, `timeout 5 go test` over a hanging test exited 124 with no
  `--- FAIL` and no package `FAIL`, and a panic on another goroutine printed a package `FAIL` alone
  (#1585).
- **Probe.** `timeout 5 go test -count=1 ./<hanging package>; echo $?` prints 124 and no `FAIL`.

## Count a table row's kill only from the row run alone
`a-kill-in-a-row-run-alone`

- **Rule.** Where a table's rows share state, run each row a mutant fails on its own before counting
  it, as `go test -v -run '^<Test>$/^<row>$'` with the row spelt as `-v` prints it, its
  regular-expression characters escaped and a quote written `'\''`, and see one `=== RUN` line name
  it: a row failing only after another left state behind is fallout, not a kill.
- **Why.** A plant at `cmd/morphic/dashes.go:93` failed four rows of
  `TestRun_SingleDashLongFlagsAreRefused`, and two passed alone, failing only on a file the first
  row left (#1538, #1516 at 9aa24431). A pattern in double quotes expanded a row's `$ref` to nothing
  and ran no test (#1532).
- **Probe.** Each row counted as a kill shows its `=== RUN` line from a run alone under the plant.

## A mutant run owns its cache and stops before the disk fills
`mutant-run-owns-its-cache`

- **Rule.** Point each mutant worker at a `GOCACHE` of its own, cleaned past a size, pass
  `-vet=off` to its `go test`, and check free disk before each verdict inside the planting loop,
  stopping under a floor. A mutant in a package others import rebuilds everything above it, and a
  full or shared cache turns verdicts into `[build failed]`.
- **Why.** 57 mutants grew a cache to 30 GB, leaving 250 MiB free (#1229), and 16 of 20 mutants
  read as build failures while other work cleared the shared cache (#1333).
- **Probe.** `df -h` and `du -sh "$GOCACHE"` before and after a batch.

## Stop a plant run by each `timeout`'s own pid
`stop-a-plant-run`

- **Rule.** To stop a plant run before the next restore, signal each `timeout` in it by its own pid:
  `timeout` moves itself and its command into a process group of their own, so a kill of the run's
  group leaves `go test` building or testing the tree the restore is about to change. Then see the
  session empty (`ps -o pid= -s <sid>` prints nothing) before `git reset`.
- **Why.** A run's group killed, `timeout` and its `sleep` lived on; a kill of `timeout` itself
  ended both (#1603).
- **Probe.** `setsid bash -c 'timeout 25 sleep 24 & wait' &`, then a kill of that session's group:
  `ps -o pid=,comm= -s <sid>` still lists `timeout` and `sleep`.

## A kill counts only from a test that reaches the plant
`kill-reaches-the-plant`

- **Rule.** A mutant reported killed by a test that cannot reach the planted line is no verdict
  until it reproduces, on a machine with free disk, by a test that does.
- **Why.** A mutant in `readStability` was reported killed by a test reading no stability while the
  disk sat near full; rerun with the disk free, a stability test killed it (#1229).
- **Probe.** Rerun the killing test alone on the mutant and on the clean tree: it fails on the
  first and passes on the second.

## Run a planted Python file without its cached bytecode
`python-plant-without-bytecode`

- **Rule.** Run the tests of a planted Python file with `python3.14 -B`, from a copy holding no
  `__pycache__`, beside the unplanted copy's run (`references/mutation.md#mutation-controls`).
  CPython keeps a cached `.pyc` while the source's recorded size and modification second match, so
  a plant the size of the last one, written within that second, is never read, and the run reports
  the last plant's verdict.
- **Why.** In a review of #1522, a plant that `skills_lint.py`'s tests kill read as surviving
  until it ran again with `-B`, which writes no `.pyc` for the next plant (#1522 at 29372a248).
- **Probe.** In an empty directory, write `X = 1`, then `X = 2`, to module `m` within one second,
  running `python3.14 -c 'import m; print(m.X)'` after each: without `-B` both print 1, and so does
  `-B` while the first run's `__pycache__` remains.

## Plant where only the callers a check leaves out reach
`plant-past-the-included-callers`

- **Rule.** To test what a check leaves out, such as a list claimed to hold every package whose
  code runs concurrently, plant on a branch only the excluded callers take, found by
  `references/mutation.md#find-what-only-excluded-callers-reach`: the included set's run stays
  green while the excluded set's reports the plant. A plant at a function's entry measures
  its busiest caller, and is the control, not the evidence.
- **Why.** An increment at `pass.validate`'s entry drew 291 race reports through the engine's
  listed tests, while the same increment on branches only `pass`'s and `irverify`'s own tests take
  drew none from the listed packages and 14 from those two (#1380).
- **Probe.** The included set's run exits 0, and the excluded set's reports the plant.

## Find what only the excluded callers reach from a coverage profile
`find-what-only-excluded-callers-reach`

- **Rule.** For `references/mutation.md#plant-past-the-included-callers`, profile the included set's
  run over the packages it plants in, `go test -coverpkg=<those> -coverprofile=<f> <included>`,
  and read it with `go run ./cmd/morphic-covergate <f>`, which lists each statement that run leaves
  uncovered: as each package's own tests cover all of it, each is one only the excluded callers
  reach; without `-coverpkg`, a package the excluded tests own is not in the profile. A candidate
  is no result until its plant is measured both ways.
- **Why.** On a change excluding tests from the race run, a listed statement's plant drew race
  reports in every run of the excluded test alone and none under the exclusion, and another's drew
  none either way (#1311, #1551).
- **Probe.** Each candidate's plant, run with the excluded test alone and with the included set,
  each at `references/mutation.md#kill-rate-for-a-scheduled-detector`'s count.

## A plant a run's schedule decides needs a measured kill rate
`kill-rate-for-a-scheduled-detector`

- **Rule.** The race detector reports a race only when no synchronization in that run orders the two
  accesses, so one run is no verdict, and reports the same race once per process, so each run is its
  own `go test -count=1`. Run it at least 10 times and report the rate. A plant caught in fewer than
  every run moves to a path more tests take, or its rate is the result.
- **Why.** A race planted in `pass`' `checkPropIDRefs` was reported in 3 of 14 runs, one on a path
  many `pass` tests take in 5 of 5, so a single run read either way could be wrong (#1438).
- **Probe.** `for i in $(seq 10); do go test -race -count=1 ./<pkg> 2>&1 | grep -c 'WARNING: DATA RACE'; done`,
  counting the runs above 0.

## Rebuild a plant set nobody kept, rather than carrying its result over
`rebuild-lost-plants`

- **Rule.** Where a PR body cites plants only by count and class, and the script that made them is
  gone, rebuild a set from the conditions the change adds when a merge or a review asks for them
  again, and run it once before merge, even after a merge that touched only comments: a count
  carries nothing over.
- **Why.** A body cited 70 plants by class and the repository held none; a set rebuilt from the diff
  found a check no test held, which none of them had planted (#1191, #1216).
- **Probe.** Run
  `git grep -lF '<condition the change adds>' <head> -- '*.py' '*.sh' '*.txt' ':!<file the condition is in>'`
  to show whether a set survives; the report lists each condition the diff adds beside its plant.

## Rerun only the plants a merge or commit can have changed
`verdict-outlives-unchanged-files`

- **Rule.** A plant at hand keeps its verdict across a merge or commit changing neither the file it
  mutates, the test that killed it, the fixtures it reads nor what lies between them. Rerun the
  rest, and name each verdict's revision in the body. A set nobody kept has no verdicts
  (`references/mutation.md#rebuild-lost-plants`).
- **Why.** A merge that changed no lowering left 79 plants at 100 to 170 s each, about 3.5 hours,
  where a diff left 23 (#1191, #1216). A diff of production files alone misses a merge removing the
  test that held a kill.
- **Probe.** For each verdict carried over,
  `git diff --stat <verdict's revision> <head> -- <paths>`, over those four, prints nothing, and
  `git ls-files --error-unmatch -- <paths>` exits 0 at the head, lest a path matching nothing pass.

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

## Show a plant fires before reading its survival
`a-survivor-needs-a-plant-that-fires`

- **Rule.** Before reading a survivor as untested or equivalent, show the plant changes what it
  aims at: under the same condition, plant a failure any test reaching it must show, such as
  `panic("reached")` or a `raise`, and watch a test fail of it. A plant that never fires is no
  mutant, and its survival says nothing.
- **Why.** A plant meant to break a parameter's content in `ownerOf` tested `kinds[i-1]`, where the
  parameter sat at `kinds[i-2]`: it survived, and so did the same condition with a panic (#1538,
  #1518 at beb3aa08).
- **Probe.** Each survivor the report lists names the failing plant that fired at its condition.

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

## Stop planting a check once each form, route and limit holds
`plants-stop-at-a-held-grid`

- **Rule.** Plants across a check's forms, routes and limits may stop once every form and every
  route has a plant a test holds, each form only one branch reads has its own, and each limit its
  comments or PR state has a plant widening the check past it, one per member the limit names
  (`references/claims.md#comment-claims-held-by-test`). A cell past that is reported unplanted with
  its reason, not ranked.
- **Why.** A fixture of 109 planted lines left its reviews no rule for enough, so one spent a pass
  on plants its author called possibly contrived, while a commit titled for what its check cannot
  see held only some of its stated limits (#1447, #1563, #1593).
- **Probe.** The report lists each form, route and limit beside the plant holding it, and each
  unplanted cell beside its reason.

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
