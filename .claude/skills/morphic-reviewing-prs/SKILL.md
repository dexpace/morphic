---
name: morphic-reviewing-prs
description: Use when asked to review a pull request, branch or commit range in this repository, including a re-review of a PR's final state after new commits or a merge of main.
---

# Reviewing morphic pull requests

The bar is `CLAUDE.md`'s: a PR is ready when there is nothing left to find. A finding counts only
with the command that showed it. Post nothing to the PR unless asked; report to whoever asked.

## Setup

Run `python3 .claude/skills/morphic-reviewing-prs/scripts/review_setup.py <PR>`. It checks the head
out in a worktree of its own and prints the title measured as it will land, commit subjects,
closing keywords, files by area, whether the branch merges cleanly into `origin/main`, CI, the
squash-message setting and an artifact scan, and names the domain skill to load. Probe only in
that worktree, with `morphic-verifying-changes`.

## Passes

Each finds what the others cannot. Run all of them against the final state.

1. **Gate.** `make gate` in the worktree. Report its result; do not re-check by eye what it
   enforces.
2. **Code, before the claims.** Read the diff without the PR body. For each changed mechanism,
   diff old against new semantics at every bound, sweep every site of it
   (`references/sweep-every-site.md`), re-plant every mutation the PR lists, and ask which input
   class the corpus cannot contain (`references/review-probes.md`).
3. **Written rules.** Check each invariant in `CLAUDE.md` the diff could touch, and the Go style
   rules there that no linter enforces.
4. **Behaviour against main.** For each output the change alters, compile a minimal spec on `main`
   and on the head. "As on main" never excuses a defect inside the change's mechanism.
5. **Claims.** Every sentence in the body, commits, comments and docs that states behaviour is a
   claim to execute. Counts are derived (`references/derive-never-maintain.md`).
6. **Process.** Title, body, closing keywords, squash message and scope (`references/process.md`),
   and `scripts/artifact_scan.py` over the range and the PR body.
7. **Merged state.** Merge `origin/main` into a scratch copy and rerun what the merge can break.

A review after new commits reviews the branch as it will merge, not the latest fix.

## Findings

Rank them `issue:` (must fix before merge), `question:`, `suggestion:`, `nit:`. Each names the
rule it breaks, the file and line, what was run and what came back, and the smallest fix that
removes the class rather than the instance.

- A defect the change introduces, exposes or makes reachable in more cases is in scope even when
  its root is older.
- A regression is never a limitation. For each limitation the change states, compile a valid input
  that reaches it on base and on head; if head is worse, it is an `issue:`.
- Before calling an oddity a defect, check whether it is deliberate (a documented policy, an open
  issue). If it is, the finding is the comment that fails to say so.
- Also list what was probed and came back clean, and each equivalent mutant with the reason.

## References

- `references/review-probes.md`: probes that found what earlier rounds missed.
- `references/sweep-every-site.md`: every site, spelling and route a change touches.
- `references/derive-never-maintain.md`: counts, lists and copies that drift.
- `references/process.md`: commits, PR text, scope and the tracker.
