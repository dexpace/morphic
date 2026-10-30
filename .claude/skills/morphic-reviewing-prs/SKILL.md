---
name: morphic-reviewing-prs
description: Use when asked to review a pull request, branch or commit range in this repository, right after opening a pull request here, or for a re-review of a PR's final state after new commits or a merge of main.
---

# Reviewing morphic pull requests

The bar is `CLAUDE.md`'s: nothing left to find, and the first round finds it all. A finding counts
only with the command that showed it. Post nothing to the PR
unless asked.

## Setup

Review the tree as it will land, in a throwaway worktree:

```bash
git fetch origin main && git fetch origin "pull/<N>/head"
git worktree add --detach <dir> FETCH_HEAD && cd <dir> && git merge --no-edit origin/main
```

A head that conflicts with `main` is reviewed alone (`git merge --abort`); the conflict is an
`issue:`. Probe only in that worktree, with `morphic-verifying-changes` and the domain skill the
changed files call for.

## Passes

Run all.

1. **Gate.** `make gate` in the worktree, unless the head already contains `origin/main` and CI
   passed on it, which ran this tree. Never re-check by eye what the gate enforces.
2. **Code, before the claims.** Read the diff without the PR body. For each changed mechanism,
   diff old against new semantics at every bound, sweep every site of it
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#sweep-every-site-of-the-mechanism`),
   re-plant the PR's mutants, plant your own, and ask which input class the corpus cannot contain
   (`references/review-probes.md#code-before-claims`).
3. **Written rules.** Check each invariant in `CLAUDE.md` the diff could touch, and the Go style
   rules there that no linter enforces.
4. **Behaviour against main.** For each output the change alters, compile a minimal spec on
   `origin/main` and in the worktree; a normative document has none, so take
   `references/review-probes.md#review-design-rules`. "As on main" never excuses a defect inside the change's
   mechanism.
5. **Claims, every class in full.** Check every member of each class of claim, never a sample
   (`references/review-probes.md#sweep-each-claim-class`). Counts are derived
   (`.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md`).
6. **Process.** The push checklist in `morphic-shipping-prs`, without its push, on the PR's tree
   and text; then title, commit messages, body, closing keywords and scope, by
   `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md`.
7. **New commits.** When the branch or `origin/main` moves, rerun the setup and every pass the new
   commits can affect.

## Findings

Rank each finding `issue:`, `nit:` (a defect too small to break anything), `suggestion:` (no
defect) or `question:`. The rank orders the work, not what may ship: every `issue:` and `nit:` is
fixed or justified in writing before merge. Each names the rule it breaks (in `CLAUDE.md` or a
skill), the file and line, what was run and what came back, and the smallest fix that removes the
class rather than the instance.

- A defect the change introduces, exposes or makes reachable is in scope even when its root is
  older, and a stated limitation is an `issue:` when a valid input shows head worse than base.
- A deliberate oddity (a documented policy, an open issue) is a finding only where its comment
  fails to say so.
- List what was probed clean, each class with how many members were checked, and each
  equivalent mutant with its reason.
- A fix the review makes is a change under review
  (`references/review-probes.md#review-own-fixes`).

## References

- `references/review-probes.md`: probes that found what earlier rounds missed.

