---
name: morphic-verifying-changes
description: Use when writing or changing a test, a verifier, or a lint or archtest rule in this repository, fixing a defect, claiming that a test, check or fix catches or prevents something, comparing behaviour with main, or stating a count, a cost, or an every/never/only claim about the code.
---

# Verifying changes in morphic

A result is evidence only when something outside it could have disagreed. Reading the code
or the test, regenerating a golden or compiling one declaration order cannot disagree with
you. A planted defect, a probe, a `main` control or the opposite order can. `CLAUDE.md`'s "Verify
by executing" is the rule; this skill is how.

## Claim → probe

| Claim | Probe |
|---|---|
| test T catches defect D | plant D with `scripts/mutate.py`; T reddens and the mutant compiled |
| this guard or branch matters | neutralise it (`cond \|\| true`, an early return); a test reddens. Coverage cannot see an untaken guard |
| order-independent | swap the declaration order; diff the documents, and the diagnostics as a sorted multiset |
| no regression against main | `scripts/sweep.py`; each new probe fails on `main`, and a build with the fix broken differs |
| the fixture reaches the check | confirm where it lands: `harness.Check` stops at the first error diagnostic |
| a bound of N holds | lower it by one at each site; a test at N and at N+1 notices |
| lowers to X, is dropped, is refused | compile that literal input; read the IR and every diagnostic |
| a count, size or cost | derive it with a command and name the revision |

## Habits

- Before trusting "identical" or "none found", print what ran: exit codes on each side, bytes of
  output, files parsed, a positive control's hit.
- Read every result line: a mutant that did not compile, a test that never ran and a panic in
  another test all look like verdicts.
- Iterate with `go test $(python3 .claude/skills/morphic-verifying-changes/scripts/affected_packages.py)`,
  run `go test ./...` before concluding, and `make gate` before calling it done. Coverage counts
  each package's own tests: `go test` passing is not the coverage gate.

## Scripts

Run each with `python3`; its docstring gives the options.

- `scripts/mutate.py FILE OLD NEW -- GO-TEST-COMMAND` plants one literal replacement, checks it
  compiles, runs the tests and always restores the file. `--batch MUTANTS` plants a
  JSON-lines set in turn, after the command passes on the clean tree.
- `scripts/sweep.py [-b BASE] [CORPUS...]` compiles a corpus (default `testdata/`) at BASE
  and at the working tree, and reports each difference.
- `scripts/affected_packages.py [BASE]` lists the packages a change can affect.
- `scripts/golden_spec.py [--count KEY...]` prints the cached golden spec's path, or objects
  per key.

## References

- `references/claims.md`: what comments, docs, commits and PR bodies say.
- `references/derive-never-maintain.md`: counts, lists and copies that drift.
- `references/sweep-every-site.md`: every site and every sentence a change touches.
- `references/tests-that-bite.md`: tests that fail when the code is wrong.
- `references/assertions-and-reach.md`: what an assertion and a fixture actually reach.
- `references/checkers.md`: verifiers, lint and archtest rules that reach what they claim.
- `references/mutation.md`: planting defects without fooling yourself.
- `references/differentials.md`: controls, corpus sweeps and generated inputs.
- `references/toolchain.md`: Go caching, coverage and scheduling behaviour that misleads probes.
