---
name: morphic-shipping-prs
description: Use when committing in this repository, writing or editing a commit message or a PR title or body, opening or updating a pull request, merging main into a branch, or splitting a change into commits.
---

# Shipping morphic changes

`CLAUDE.md`'s "Commits & pull requests" section is the rule; this skill is how to meet it before a
reviewer has to. A PR lands under its title, or its one commit's subject, and its pushed branch is
never rewritten.

## Before each commit

- Work in your own worktree, checking `git branch --show-current` before committing: other work
  moves a shared checkout's branch.
- Gate the commit on each check's exit status, in one command
  (`references/commits-and-prs.md#gate-by-exit-status`).
- `git status --untracked-files=all` lists only what you meant to add
  (`references/commits-and-prs.md#commit-only-what-you-meant`).
- A removed or changed exported identifier, or a method added to an exported interface with no
  unexported method, outside `internal/` and `main` packages makes the change breaking
  (`references/commits-and-prs.md#exported-api-change-needs-bang`).

## Before each push

After `git fetch origin main <base>`, run this from the repository root. N is the PR's number, or
before it exists one past
`gh api 'repos/{owner}/{repo}/issues?state=all&per_page=1' --jq '.[0].number'`. `<base>` is the
branch it targets (`gh pr view N --json baseRefName --jq .baseRefName`).

```bash
pat='co-auth[o]red-by:.*(cl[a]ude|anthr[o]pic|open[a]i|chatg[p]t|cop[i]lot|cur[s]or|gem[i]ni)|gene[r]ated (with|by)[^a-z0-9]{0,3}\[?(cl[a]ude|open[a]i|chatg[p]t|cop[i]lot|cur[s]or|gem[i]ni|cod[e]x)|cl[a]ude\.(ai|com)/|(^|[^.])cl[a]ude([^./a-z]|$)|anthr[o]pic|chatg[p]t|(the|this|your) bri[e]f([^a-z]|$)|scr[a]tchpad|sub[a]gent|coordi[n]ator|/h[o]me/[a-z]|/p[r]ivate/(tmp|var)|remedi[a]tion|audit sw[e]ep'
cs='/U[s]ers/[^/[:space:]]+/|(^|[^A-Za-z0-9])[HM][0-9]{1,2}([^0-9A-Za-z]|$)|(^|[^A-Za-z0-9&])#[A-Z][A-Z0-9_-]+([^A-Za-z0-9]|$)|into[[:space:]]HEAD$'
git status --short --untracked-files=all
for b in main <base>; do git merge-tree --write-tree "origin/$b" HEAD >/dev/null || echo "conflicts with $b"; done
o=$(mktemp)
git log --format='@%h%n%B' HEAD --not "origin/<base>" origin/main | awk '/^@[0-9a-f]+$/{c=substr($(0),2);next}{print(c":"$(0))}' > "$o"
git diff "origin/<base>"...HEAD -- ':!go.sum' | grep '^+' >> "$o"
grep -niE "${pat:?}" "$o"; grep -nE "${cs:?}" "$o"
```

`git status` is empty, as the push carries HEAD; judge each hit. Grep the PR's text the same way
(`gh pr view N --json title,body --jq '.title, .body'`), and an edited body before its send.

Once every hit is fixed or justified, push behind the lint and the subject check
(`references/commits-and-prs.md#pushed-subject-is-disclosed`), which gate it by exit status. `<title>`
serves until the PR opens.

```bash
t=$(gh pr view N --json title --jq .title 2>/dev/null) || t="<title>"
h=$(gh pr view N --json headRefName --jq .headRefName 2>/dev/null) || h=$(git branch --show-current)
python3.14 .claude/skills/morphic-curating-skills/scripts/skills_lint.py &&
  python3.14 .claude/skills/morphic-shipping-prs/scripts/subjects.py --base "origin/<base>" --pushed "origin/$h" N "$t" &&
  git push origin "HEAD:refs/heads/${h:?name the branch}"
```

After the push, read CI by `references/commits-and-prs.md#conflicting-pr-has-no-run`.

## Merging main in

Merge a stacked branch's base, then `origin/main`, into a pushed branch; never rebase it. Read
`references/merging-main.md` before resolving a conflict, and rerun what the merge can break
(`references/merging-main.md#merge-main-first`,
`references/merging-main.md#merged-code-meets-a-changed-type`). `morphic-changing-the-ir` picks an `IRVersion`
bump's version.

## Before the first push

Give the branch the review's first round yourself, every angle in full (`morphic-reviewing-prs`);
the opened PR's review is its second. Its claim sweep finds false sentences, not missing ones: also
find each limitation the body states where the rule places it
(`references/commits-and-prs.md#limitation-stated-where-it-lives`).

## PR text

A Summary and a Test plan stating the head (`references/commits-and-prs.md#body-states-the-head`),
and a Breaking section when the title carries `!`; no labels
(`references/commits-and-prs.md#pr-takes-no-labels`). Every sentence stating behaviour is executed
on the final tree first
(`.claude/skills/morphic-verifying-changes/references/claims.md#pr-claims-match-evidence`). Edit a
posted body from its live copy (`references/commits-and-prs.md#edit-pr-body-from-live-copy`).

## References

- `references/commits-and-prs.md`: commit and PR text, scope, recorded decisions and a commit's
  contents.
- `references/closing-issues.md`: closing issues.
- `references/merging-main.md`: merging `main` in without losing either side.
