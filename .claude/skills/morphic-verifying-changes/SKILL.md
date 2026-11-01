---
name: morphic-verifying-changes
description: Use when writing or changing a test, a verifier, a lint or archtest rule, or a script under .claude/skills in this repository, fixing a defect, claiming that a test, check or fix catches or prevents something, comparing behaviour with main, stating a count, a cost, or an every/never/only claim about the code, or before calling a change done.
---

# Verifying changes in morphic

A result is evidence only when something outside it could have disagreed: a planted defect, a
probe, a `main` control, the opposite order. `CLAUDE.md`'s "Verify by executing" is the rule; this
skill is how.

The bar is `CLAUDE.md`'s: done means nothing left to find. A defect `main` shares inside the
change's mechanism is fixed or filed, never a reason to stop
(`.claude/skills/morphic-reviewing-prs/references/review-probes.md#better-than-main`), and a fix
goes to the type that owns the broken state
(`references/sweep-every-site.md#fix-at-the-invariants-owner`).

## Claim → probe

| Claim | Probe |
|---|---|
| test T catches defect D | plant D in a throwaway worktree (`references/mutation.md#plant-in-a-throwaway-worktree`); the output names T failing, the mutant compiled, and the clean tree passes |
| this guard or branch matters | force each side (`cond \|\| true`, `cond && false`); a test reddens for each. Coverage cannot see a side that holds no statement |
| order-independent | swap the declaration order; diff the documents, and the diagnostics as a sorted multiset |
| no regression against main | `scripts/sweep.py`, or `references/differentials.md#argv-differential` for a change under `cmd/`; each new probe fails on `main`, and a build with the fix broken differs |
| the fixture reaches the check | confirm where it lands: `harness.Check` stops at the first error diagnostic |
| a bound of N holds | lower it by one at each site; a test at N and at N+1 notices |
| lowers to X, is dropped, is refused | compile that literal input; read the IR and every diagnostic |
| a count, size or cost | derive it with a command and name the revision; a cost by `morphic-measuring-performance` |

## Habits

- Before trusting "identical" or "none found", print what ran
  (`references/differentials.md#validate-the-instrument-first`).
- Read every result line: a mutant that did not compile, a test that never ran and a panic in
  another test all look like verdicts.
- Iterate with `go test` on the packages you touched, run `go test ./...` before concluding, as
  Go's test cache keeps the unchanged packages cheap, and `make gate` before calling it done:
  `go test` passing is not the coverage gate (`references/toolchain.md#coverage-per-package`).

## Script

`python3.14 .claude/skills/morphic-verifying-changes/scripts/sweep.py`, run from the repository
root, compiles a corpus at a base and at the working tree, then prints each side's exit codes and
each spec that differs past its IR version. It judges nothing: `references/differentials.md` says
what the codes mean, and its `--help` gives the options.

## References

- `references/claims.md`: what comments, docs, commits and PR bodies say.
- `references/derive-never-maintain.md`: counts, lists and copies that drift.
- `references/sweep-every-site.md`: every site and every sentence a change touches.
- `references/tests-that-bite.md`: tests that fail when the code is wrong.
- `references/assertions-and-reach.md`: what an assertion and a fixture actually reach.
- `references/checkers.md`: scanners, lint and archtest rules that reach what they claim.
- `references/mutation.md`: planting defects without fooling yourself.
- `references/differentials.md`: controls, corpus sweeps and generated inputs.
- `references/toolchain.md`: Go versions, caching, coverage, test output and port behaviour that
  mislead probes.
