---
name: morphic-reviewing-prs
description: Use when asked to review a pull request, branch or commit range in this repository, including a re-review of a PR's final state after new commits or a merge of main.
---

# Reviewing morphic pull requests

The bar is `CLAUDE.md`'s: a PR is ready when there is nothing left to find. A finding counts only
with the command that showed it. Post nothing to the PR unless asked.

## Setup

Run `python3 .claude/skills/morphic-reviewing-prs/scripts/review_setup.py <PR>`. It checks out
the head merged into `origin/main` in a worktree of its own, so every pass sees the tree as it will
land, and names the domain skill to load. Probe only in that worktree, with
`morphic-verifying-changes`. A head that conflicts with `main` is checked out alone, and the
conflict is an `issue:`. For a branch or range with no PR, build that worktree by hand and run
`python3 .claude/skills/morphic-reviewing-prs/scripts/artifact_scan.py BASE HEAD`, the scan the
setup runs.

## Passes

Each finds what the others cannot; run all of them.

1. **Gate.** `make gate` in the worktree, unless the head already contains `origin/main` and CI
   passed on it, which ran this tree. Never re-check by eye what the gate enforces.
2. **Code, before the claims.** Read the diff without the PR body. For each changed mechanism,
   diff old against new semantics at every bound, sweep every site of it
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md`), re-plant every
   mutation the PR lists, and ask which input class the corpus cannot contain
   (`references/review-probes.md`).
3. **Written rules.** Check each invariant in `CLAUDE.md` the diff could touch, and the Go style
   rules there that no linter enforces.
4. **Behaviour against main.** For each output the change alters, compile a minimal spec on
   `origin/main` and in the worktree. "As on main" never excuses a defect inside the change's
   mechanism.
5. **Claims.** Every sentence in the body, commits, comments and docs that states behaviour is a
   claim to execute. Counts are derived
   (`.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md`).
6. **Process.** Title, commit messages, body, closing keywords and scope, by
   `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md`.
7. **New commits.** When the branch or `origin/main` moves, rerun the setup and every pass the new
   commits can affect.

## Findings

Rank each `issue:`, `nit:` (a defect too small to break anything), `suggestion:` (no defect) or
`question:`. The rank orders the work, not what may ship: every `issue:` and `nit:` is fixed or
justified in writing before merge. Each names the rule it breaks (in `CLAUDE.md` or a skill), the
file and line, what was run and what came back, and the smallest fix that removes the class rather
than the instance.

- A defect the change introduces, exposes or makes reachable in more cases is in scope even when
  its root is older.
- A regression is never a limitation. For each limitation the change states, compile a valid input
  that reaches it on base and on head; if head is worse, it is an `issue:`.
- An oddity that is deliberate (a documented policy, an open issue) is no defect; the finding is
  then a comment that fails to say so.
- Also list what was probed clean, and each equivalent mutant with its reason.

## References

- `references/review-probes.md`: probes that found what earlier rounds missed.

