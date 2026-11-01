---
name: morphic-reviewing-prs
description: Use when asked to review a pull request, branch or commit range in this repository, right after opening a pull request here, or for a re-review of a PR's final state after new commits or a merge of main.
---

# Reviewing morphic pull requests

The bar is `CLAUDE.md`'s: the first round leaves nothing to find. A finding needs the command that
showed it, and must survive an attempt to disprove it
(`references/review-probes.md#disprove-each-finding`). Post nothing unless asked, then by
`references/review-probes.md#posted-findings`.

## Setup

Review the tree as it will land, in a throwaway worktree:

```bash
base=$(gh pr view <N> --json baseRefName --jq .baseRefName)
git fetch origin main "$base" && git fetch origin "pull/<N>/head"
git worktree add --detach <dir> FETCH_HEAD && cd <dir>
git merge --no-edit "origin/$base" && git merge --no-edit origin/main
```

Abort a merge that conflicts (`git merge --abort`) and review what merged; the conflict is an
`issue:`. Probe only in that worktree, with `morphic-verifying-changes` and the changed files'
domain skill.

## Passes

Run all, fanning out only when asked (`references/review-probes.md#fan-out-a-pass-to-a-reader`).

1. **Gate.** `make gate`, unless CI passed on a head holding everything the setup merges. Never
   re-check by eye what it enforces.
2. **Code, before the claims.** Read the diff without the PR body. For each changed mechanism,
   diff old against new semantics at every bound, sweep every site of it
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#sweep-every-site-of-the-mechanism`),
   re-plant the PR's mutants
   (`.claude/skills/morphic-verifying-changes/references/mutation.md#rebuild-lost-plants`), plant
   your own, and ask which input class the corpus cannot contain
   (`references/review-probes.md#code-before-claims`).
3. **Written rules.** Check each invariant in `CLAUDE.md` the diff could touch, and take
   `morphic-checking-style` for its Go. Question any added line nobody can explain.
4. **Behaviour against main.** For each output the change alters, compile a minimal spec on
   `origin/main` and in the worktree; a normative document has none, so take
   `references/review-probes.md#review-design-rules`. "As on main" never excuses a defect inside the
   change's mechanism.
5. **Claims.** Check every member of each class of claim, never a sample
   (`references/review-probes.md#sweep-each-claim-class`). Counts are derived
   (`.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md`).
6. **Process.** The push checklist in `morphic-shipping-prs`, without its push, on the PR's tree
   and text; then title, commits, body, closing keywords and scope, by
   `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md`, and open PRs, by
   `.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-open-prs`.
7. **New commits.** When the branch, its base or `origin/main` moves, rerun the setup and every
   pass the new commits can affect.

## Findings

Rank each finding `issue:`, `nit:` (a defect too small to break anything), `suggestion:` (no
defect) or `question:` (`references/review-probes.md#rank-by-what-was-shown`). The rank orders the work, not what may ship: every `issue:` and `nit:` is
fixed or justified in writing before merge. Each names the rule it breaks, file and line, what was
run, what came back, and the smallest fix removing the class, not the instance.

- A defect the change introduces, exposes or makes reachable is in scope even when its root is
  older, and a stated limitation is an `issue:` when a valid input shows head worse than base.
- A deliberate oddity (a documented policy, an open issue) is a finding only where no comment says
  so.
- Read what reviews already stand (`references/review-probes.md#read-standing-reviews`), then list
  what was probed clean, by class and count, and each equivalent mutant with its reason.
- A fix the review makes is a change under review
  (`references/review-probes.md#review-own-fixes`).

## References

- `references/review-probes.md`: what earlier rounds missed.

