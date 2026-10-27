---
name: morphic-verifying-changes
description: Use when writing or changing a test in this repository, claiming that a test, check or fix catches or prevents something, comparing behaviour with main, or stating a count, a cost, or an every/never/only claim about the code.
---

# Verifying changes in morphic

A result is evidence only when something outside it could have disagreed. Reading the code,
reading the test, regenerating a golden or compiling one declaration order cannot disagree with
you. A planted defect, a probe, a `main` control or the opposite order can. `CLAUDE.md`'s "Verify
by executing" is the rule; this skill is how.

## Claim → probe

| Claim | Probe |
|---|---|
| test T catches defect D | plant D with `scripts/mutate.sh`; T reddens and the mutant compiled |
| this guard or branch matters | neutralise it (`cond \|\| true`, an early return); a test reddens. Coverage cannot see an untaken guard |
| order-independent | swap the declaration order; diff the documents, and the diagnostics as a sorted multiset |
| no regression against main | `scripts/sweep.sh`, plus each new probe run on `main`, where it must fail |
| the fixture reaches the check | confirm where it lands: `harness.Check` stops at the first error diagnostic |
| a bound of N holds | lower it by one at each site; a test at N and at N+1 notices |
| lowers to X, is dropped, is refused | compile that literal input; read the IR and every diagnostic |
| a count, size or cost | derive it with a command and name the revision |

## Habits

- A differential needs two controls: `main` differs where the fix applies, and so does a build
  with the fix deliberately broken.
- Before trusting "identical" or "none found", print what ran: exit codes on each side, bytes of
  output, files parsed, a positive control's hit.
- Read every result line. A mutant that did not compile, a test that never ran and a panic in
  another test all look like verdicts.
- Scope a passing result to the site it was planted at; scope a defect to every site of its
  mechanism.
- Iterate with `go test $(bash .claude/skills/morphic-verifying-changes/scripts/affected-packages.sh)`,
  run `go test ./...` before concluding, and `make gate` before calling it done. Coverage counts
  each package's own tests, so `go test` passing says nothing about the coverage gate.

## Scripts

- `scripts/mutate.sh FILE OLD NEW -- TEST-COMMAND` plants one literal replacement, checks that it
  compiles, runs the command, restores the file whatever happens, and prints KILLED, SURVIVED or
  UNVERIFIED.
- `scripts/sweep.sh [-b BASE] [CORPUS...]` builds `cmd/morphic` at BASE and at the working tree,
  compiles every spec in the corpus (default `testdata/`) with both, and reports each difference in
  exit code, IR or diagnostics.
- `scripts/affected-packages.sh [BASE]` lists the packages a change can affect, reverse
  dependencies included.

## References

- `references/claims.md`: what comments, docs, commits and PR bodies say.
- `references/tests-that-bite.md`: tests that fail when the code is wrong.
- `references/assertions-and-reach.md`: what an assertion and a fixture actually reach.
- `references/mutation.md`: planting defects without fooling yourself.
- `references/differentials.md`: controls, corpus sweeps and generated inputs.
- `references/toolchain.md`: Go caching, coverage and scheduling behaviour that misleads probes.
