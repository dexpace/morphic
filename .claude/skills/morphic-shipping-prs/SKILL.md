---
name: morphic-shipping-prs
description: Use when committing in this repository, writing or editing a commit message or a PR title or body, opening or updating a pull request, merging main into a branch, or splitting a change into commits.
---

# Shipping morphic changes

`CLAUDE.md`'s "Commits & pull requests" section is the rule; this skill is how to meet it before a
reviewer has to. A PR is squash-merged, so its title lands on `main`, and its branch is never
rewritten once pushed.

## Before each commit

- Work in a worktree of your own, and check `git branch --show-current` before committing: other
  work can move the branch of a checkout you share.
- Gate the commit on each check's exit status, in the same command
  (`references/commits-and-prs.md#gate-by-exit-status`).
- `git status --untracked-files=all` lists only what you meant to add
  (`references/commits-and-prs.md#commit-only-what-you-meant`).
- A removed or changed exported identifier outside `internal/` makes the change breaking
  (`references/commits-and-prs.md#exported-api-change-needs-bang`).

## Before each push

Run `python3 .claude/skills/morphic-shipping-prs/scripts/preflight.py` from the repository root,
with `--pr N` once the PR exists, or `--title` and `--body FILE` for a draft. It checks each
subject, the title measured with the number GitHub appends, the body's sections against the
title's `!`, its closing keywords, the exported functions, methods, types, constants and variables
the branch removes or changes, untracked files, the merge with `origin/main`, the artifact scan,
and every name a skill uses. It exits 1 on a finding, so chain the push on it with `&&`.

## Merging main in

Merge `origin/main` into a pushed branch; never rebase it. Read `references/merging-main.md`
before resolving a conflict, and rerun what the merge can break
(`references/merging-main.md#merge-main-first`). A branch that bumps `IRVersion` takes its version
by `morphic-changing-the-ir`.

## PR text

A Summary and a Test plan, and a Breaking section when the title carries `!`. Every sentence that
states behaviour is executed on the final tree first
(`.claude/skills/morphic-verifying-changes/references/claims.md#pr-claims-match-evidence`). Edit a
posted body from its live copy (`references/commits-and-prs.md#edit-pr-body-from-live-copy`).

## References

- `references/commits-and-prs.md`: messages, titles, bodies, closing keywords, scope, recorded
  decisions, and what a commit may hold.
- `references/merging-main.md`: merging `main` into a branch without losing either side.
