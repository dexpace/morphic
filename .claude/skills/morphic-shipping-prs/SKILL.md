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
- A removed or changed exported identifier outside `internal/` and `main` packages, or a method
  added to an exported interface with no unexported method, makes the change breaking
  (`references/commits-and-prs.md#exported-api-change-needs-bang`).

## Before each push

After `git fetch origin main`, run this checklist from the repository root and read what each line
prints. N is the PR's number, or before it exists one past
`gh api 'repos/{owner}/{repo}/issues?state=all&per_page=1' --jq '.[0].number'`.

```bash
pat='co-auth[o]red-by:.*(cl[a]ude|anthr[o]pic|open[a]i|chatg[p]t|cop[i]lot|cur[s]or|gem[i]ni)|gene[r]ated (with|by)[^a-z0-9]{0,3}\[?(cl[a]ude|open[a]i|chatg[p]t|cop[i]lot|cur[s]or|gem[i]ni|cod[e]x)|cl[a]ude\.(ai|com)/|(^|[^.])cl[a]ude([^./a-z]|$)|anthr[o]pic|chatg[p]t|(the|this|your) bri[e]f([^a-z]|$)|scr[a]tchpad|sub[a]gent|coordi[n]ator|/u[s]ers/|/h[o]me/[a-z]|/p[r]ivate/(tmp|var)|remedi[a]tion|audit sw[e]ep|(^|[^a-z0-9])[hm][0-9]{1,2}([^0-9a-z]|$)'
{ git log --no-merges --format=%s origin/main..HEAD; printf '%s (#%s)\n' "<title>" N; } |
  awk 'length > 72 || !/^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: [^ ]/ || /\.( \(#[0-9]+\))?$/'
git status --short --untracked-files=all
git merge-tree --write-tree origin/main HEAD >/dev/null || echo 'conflicts with origin/main'
git log --format=%B origin/main..HEAD | grep -niE "$pat"
git diff origin/main...HEAD -- ':!go.sum' | grep '^+' | grep -niE "$pat"
python3.14 .claude/skills/morphic-curating-skills/scripts/skills_lint.py
```

Push separately once every hit is fixed or justified
(`references/commits-and-prs.md#gate-by-exit-status`). The subject check prints each commit
subject, and the title as the squash lands it, that runs past 72 characters, breaks the form or
ends in a period; `git status` is empty because the push carries HEAD; each artifact hit is a line
to judge, and `go.sum` stays out, as every `h1:` hash would be one.
Grep the PR's own text the same way (`gh pr view N --json title,body --jq '.title, .body'`). The
lint exits 1 on a name a skill uses that this branch leaves naming nothing.

## Merging main in

Merge `origin/main` into a pushed branch; never rebase it. Read `references/merging-main.md`
before resolving a conflict, and rerun what the merge can break
(`references/merging-main.md#merge-main-first`). A branch that bumps `IRVersion` takes its version
by `morphic-changing-the-ir`.

## Before the first push

Give the branch the review's first round yourself, every class of claim checked in full
(`.claude/skills/morphic-reviewing-prs/references/review-probes.md#sweep-each-claim-class`), so a
reviewer's first round finds nothing. That sweep finds false sentences, not missing ones: also find
the sentence stating each limitation the body states, where the rule places it
(`references/commits-and-prs.md#limitation-stated-where-it-lives`). Once the PR is open, run
`morphic-reviewing-prs` on it.

## PR text

A Summary and a Test plan, and a Breaking section when the title carries `!`. Every sentence that
states behaviour is executed on the final tree first
(`.claude/skills/morphic-verifying-changes/references/claims.md#pr-claims-match-evidence`). Edit a
posted body from its live copy (`references/commits-and-prs.md#edit-pr-body-from-live-copy`).

## References

- `references/commits-and-prs.md`: messages, titles, bodies, closing keywords, scope, recorded
  decisions, and what a commit may hold.
- `references/merging-main.md`: merging `main` into a branch without losing either side.
