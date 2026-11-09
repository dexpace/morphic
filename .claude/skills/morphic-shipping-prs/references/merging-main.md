# Merging main into a branch

A branch that was right when its checks went green can be wrong once `main` moves, and a clean
merge says nothing about it. These rules are the ways a merge loses one side.

## Merge main before each review round and rerun what it can break
`merge-main-first`

- **Rule.** Merge `origin/main` into the branch before a review round and before calling it ready, a
  stacked branch's base first (`references/merging-main.md#merge-the-base-first`). Then build, vet,
  lint (`references/merging-main.md#lint-the-merge`) and run the whole suite on the merge,
  `internal/archtest`'s caps among it, and re-plant what the merge can have changed
  (`.claude/skills/morphic-verifying-changes/references/mutation.md#verdict-outlives-unchanged-files`):
  a clean merge can fail to compile, a test or the lint, or leave a test green for a mechanism the
  merge removed.
- **Why.** After a base PR's final round stopped handing over cached bytes, a stacked PR's dedup
  test stayed green with the dedup deleted (#565, #562); a conflict-free merge failed the doc
  comment cap `main` had adopted meanwhile (#562).
- **Probe.** `git merge --no-commit --no-ff origin/main`, then `go build ./...`,
  `make vet lint lint-planted`, `go test ./...`, and the plants that rule leaves to rerun.

## Hold a merge to the lint before a stacked branch takes it
`lint-the-merge`

- **Rule.** A merge can break the lint alone, where a rule one side adopted meets code the other
  added. Run `make vet lint lint-planted` on the merge, or read CI's `vet` and `lint` on the pushed
  merge, before any branch stacked on it merges it in.
- **Why.** A merge left six failing sites. `go vet` saw one, and with it fixed, build, vet and test
  passed while the lint refused the rest, four under a rule `main` had adopted, and the four
  branches stacked on it failed CI (#1437).
- **Probe.** `git merge --no-commit --no-ff origin/main`, then `make vet lint lint-planted`, reading
  each finding beside `go vet ./...`'s.

## Merge a stacked branch's base first, as a merge of its own
`merge-the-base-first`

- **Rule.** A stacked PR lands on its base, not on `main`. Merge `origin/<base>` before
  `origin/main`, each as its own merge so that a defect shows against the merge causing it, and run
  on each what `references/merging-main.md#merge-main-first` names: the push checklist sees only a
  conflict.
- **Why.** A head green alone, and reported `MERGEABLE` and `CLEAN`, stopped compiling once merged
  with its base, which merging `main` could not show (#1183, #1179).
- **Probe.** `gh pr view <N> --json baseRefName --jq .baseRefName`, then
  `git merge --no-commit --no-ff origin/<base>` and `go vet ./...` before touching `main`.

## When a stacked base has landed, merge its final head first
`merge-a-landed-bases-final-head`

- **Rule.** Once a stacked PR's base squash-merges and the PR retargets to `main`, merging `main`
  conflicts wherever the branch holds the base's text from an older merge. Merge the base PR's final
  head first (`git fetch origin +pull/<base PR>/head:<own ref>`), then `origin/main`; where that
  still conflicts at the base's text, take the branch's side outside the files
  `git diff --name-only <squash> origin/main` lists and `main`'s inside them, so
  `git diff HEAD^1 HEAD` holds only that diff. With no base head, take
  `references/merging-main.md#replay-own-diff-onto-main`. Then read each concept it adds by
  `references/merging-main.md#main-landed-the-concept`.
- **Why.** Merging `main` after #1331 squash-landed conflicted in 17 files, its final head in the 4
  real overlaps (#1332); on another stack, `main` past the squash drew 24 more (#1427).
- **Probe.** `git merge-tree --write-tree --name-only <head> <squash>` beside it against the base's
  final head.

## Replay a branch's own diff onto main to leave a stack
`replay-own-diff-onto-main`

- **Rule.** To move a PR off a base it does not need, or merge one whose base head is gone, replay
  its diff onto `main` unrewritten: `git merge --no-commit --no-ff origin/main`,
  `git restore --source=origin/main --staged --worktree .` from the root, then
  `git diff --binary <base tip> <old head> | git apply --3way`, taking `main`'s side on a line
  `git blame` at the base tip shows only the base wrote. Check what the PR closes against what
  only the base delivers, retarget it, and merge it into each PR stacked on it; its checklist
  excludes the former base (`references/commits-and-prs.md#own-commits-against-the-base`).
- **Why.** #1351 left the emitter stack it needed nothing from, and stopped closing #678,
  whose second ask only #1336 met (#1351).
- **Probe.** `git diff --stat origin/main HEAD` then lists only the PR's own files.

## Find out what a grown parameter carries before filling it in
`grown-parameter-carries-something`

- **Rule.** When a conflict comes from a signature growing a parameter on `main`, find what the
  parameter carries and who else passes it before choosing a value; then mutate the resolution and
  confirm a test reddens.
- **Why.** Passing `nil` for a new parameter compiled and passed the suite while it dropped a
  co-declared keyword the diagnostic still said was kept (#363).
- **Probe.** `git grep -n '<callee>(' origin/main` for every caller and what each passes.

## Read the normative text before changing a test the merge reddens
`norm-main-adopted`

- **Rule.** When a merge reddens a test another PR added, read that PR's normative text before
  touching the expectation: `main` may have adopted a rule the branch now breaks.
- **Why.** A rule reported one defect under two codes after `main` made one code the rule for every
  string, and adding the second code to the test was the wrong fix (#549, #539).
- **Probe.** `git log origin/main -S'<test name>'` names the PR; read its normative text.

## Reuse what main landed for the concept the branch adds
`main-landed-the-concept`

- **Rule.** After a merge, search the helpers and predicates the landed PRs added for the concept
  the branch adds, and reuse one rather than keep a second spelling of it.
- **Why.** A branch kept its own predicate beside one #549 had landed for the same concept, which
  also covered a case the branch's missed (#551).
- **Probe.** `git diff <merge-base> origin/main -- '*.go' | grep '^+func'`, then grep the result for
  the concept's vocabulary.

## Read what main wrote into a conflict before dropping it
`read-the-side-you-discard`

- **Rule.** In each conflict, read `main`'s side before discarding it: it can hold the evidence
  against the branch's premise. Diff a base PR's squash against the commit a stacked branch was
  built on.
- **Why.** The side a stacked PR's conflict would have dropped held a comment, written in the base
  PR's later rounds, refuting the claim the branch deleted a test row for (#554).
- **Probe.** `git diff <merge-base> origin/main -- <conflicted file>` before resolving.

## Carry main's intent, not only its hunks
`replay-intent-not-hunks`

- **Rule.** For every conflicted file, read `main`'s whole patch for it and decide per hunk whether
  it is mechanical or a change of behaviour to carry over; a rejected hunk means the branch deleted
  that code, not that `main`'s change does not apply. Where `main` landed an overlapping design,
  take `main`'s design, and say in the merge and PR bodies wherever the branch departs from it.
- **Why.** A merge kept the branch's rewritten test and dropped the widening `main` had made to it,
  and the merge compiled and passed (#837, #811); a branch merging two landed designs took both
  rather than its own (#1034, #1009, #1038).
- **Probe.** `git diff <merge-base> origin/main -- <file>` for each conflicted file.

## Reread what the branch says about a PR the merge brought in
`reread-what-cites-a-merged-pr`

- **Rule.** After a merge, reread each sentence the branch adds that cites a PR the merge brought
  in, against that PR as it landed: the sentence was written against the PR as it then stood.
- **Why.** A Why said two issues never appeared in #1167, true of the revision #1170 quotes and
  false of the merge, which named both (#1169).
- **Probe.** `git diff origin/main...HEAD | grep -n '^+.*#<N>'` for each PR the merge brought in.

## Confirm a merge commit has both parents
`merge-commit-has-two-parents`

- **Rule.** Merge with `git merge --no-commit --no-ff`, create any branch before merging, and check
  the parents before pushing. Without `--no-commit` a merge that meets no conflict commits by
  itself, so fixups committed after it under a merge message make a single-parent commit; creating
  or switching a branch mid-merge drops the merge state. While the commit is local, redo the merge.
- **Why.** A branch created during a resolved but uncommitted merge recorded the merge with one
  parent (#724).
- **Probe.** `git rev-list --parents -n1 HEAD` lists the branch's head and `main`'s.

## Fold and compare against a recorded base, not a moving one
`recorded-base`

- **Rule.** `origin/main` is whatever any worktree of the repository fetched last. Fold commits
  onto the merge base recorded before anything moved, and compare with `main` in a detached
  worktree, never by stashing: the stash is shared by every worktree.
- **Why.** A fold onto a just-fetched `origin/main` reverted #870 in silence (#878); a pop applied
  another worktree's stash (#966, #475).
- **Probe.** `git diff --stat origin/main...HEAD`, read from the merge base, lists only files the
  branch changed; a two-dot diff against `origin/main` also lists what `main` gained since.

## Build a trial merge as a worktree of a commit of the merged tree
`trial-merge-by-commit-tree`

- **Rule.** Build a trial merge by SHA, without copying the merge into a fresh repository:
  `t=$(git merge-tree --write-tree <head> <pr head>) && git worktree add --detach <dir> "$(git commit-tree "$t" -p <head> -p <pr head> -m trial)"`,
  which a conflict stops.
- **Why.** Copying 14 merges out by `git archive`, `git init` and `git add -A` took 47 minutes; this
  form trial-merged all 27 open PRs in about a minute (#1532, #1542).

## Confirm a trial merge by both its parents before reading a result
`trial-merge-happened`

- **Rule.** Build each trial merge by `references/merging-main.md#trial-merge-by-commit-tree`, then,
  from the worktree that built it, confirm both parents with
  `test "$(git -C <dir> rev-parse HEAD^1 HEAD^2)" = "$(git rev-parse <head> <pr head>)"` before
  reading a result, as a failed step leaves `<dir>` missing or holding an earlier merge, of another
  `<pr head>` or of an earlier `<head>`. Remove `<dir>` once read.
- **Why.** A loop over the open PRs printed `ok` while every merge of another worktree's
  `FETCH_HEAD` had ended in `fatal` (#1210). Run inside `<dir>`, a `<head>` of `HEAD` names the
  trial's own commit, so the confirm runs where the build did.

## Trial-merge the open PRs after a change they may not survive
`trial-merge-open-prs`

- **Rule.** A change that removes or renames a target, file or identifier other open PRs may name,
  alters what every file of a kind must hold, such as a golden, or adds a package or a whole-tree
  check, trial-merges each, before and after, runs on each merged tree what
  `references/merging-main.md#trial-merge-runs-its-checks` names, and reads its conflicts by
  `references/merging-main.md#trial-merge-reads-what-the-head-adds`. `git merge-tree` reports
  conflicts, not failures, and GitHub's flag names no culprit.
- **Why.** Resolving a conflict over a removed make target would have kept the gate pointing at it
  (#828, #836, #503). A line every conformance golden gained merged cleanly with PRs adding goldens
  and failed on each (#1181, #1191, #1192), as an archtest rule over every package did with a PR
  adding two (#1556).
- **Probe.** `git merge-tree --write-tree --name-only <head> <pr head>` per open PR.

## Run each kind's checks in the trial merge's own tree
`trial-merge-runs-its-checks`

- **Rule.** On each merged tree `references/merging-main.md#trial-merge-happened` confirms, build
  and run there, never a binary built from another tree: `git grep` each removed name; for goldens,
  `go test ./compilers/openapi/e2e ./cmd/morphic-census`; for an added package or archtest rule,
  `go test ./internal/archtest`; for an analyzer, `go vet -vettool=<tool> <changed packages>`; for
  a skill edit, `references/merging-main.md#trial-merge-lints-the-skills`.
- **Why.** A test binary built from one head flagged three open PRs for a package one of them
  exempts, and rebuilt in that PR's merged tree its test passed (#1311, #1556).
- **Probe.** Each result names the merged commit and the command run in it.

## Read a trial merge's conflicts against the open PR's own with main
`trial-merge-reads-what-the-head-adds`

- **Rule.** A PR drifted from `main` conflicts with any head, so only the files a head adds to the
  PR's conflicts with `main` are the change's. Where a conflict leaves no merged tree, search the
  PR's added lines for each removed name,
  `git diff "$(git merge-base origin/main <pr head>)" <pr head> | grep '^+'`, and report that no
  test ran on that merge.
- **Why.** Twelve of 26 open PRs conflicted with a head, each conflict their own with `main`, and
  none left a tree to test (#1583).
- **Probe.** With
  `c() { git merge-tree --write-tree --name-only "$1" "$2" | sed '1d;/^$/,$d' | sort; }`,
  `comm -23 <(c <head> <pr head>) <(c origin/main <pr head>)` prints what the head adds.

## Run the skills lint on each trial merge of a skill edit
`trial-merge-lints-the-skills`

- **Rule.** A change editing a skill runs `skills_lint.py` on each trial merge, as a word cap is
  shared across open PRs and neither a conflict check nor a test reads it.
- **Why.** Two PRs each under a skill's body cap merged with no conflict at 512 words against 500,
  which would have failed the second to land on nothing in its own change (#1219).
- **Probe.** The lint on each tree `references/merging-main.md#trial-merge-happened` builds and
  confirms.

## Count a cap two open PRs share at each one's head
`cap-two-prs-share`

- **Rule.** When a change brings a capped type or file within what another open PR adds, count the
  net, added less removed, at each open PR's head and on their trial merge, rather than trusting a
  clean merge: methods per type under `internal/archtest`'s cap, a `SKILL.md` body, a function's
  length. Of two that each pass alone, the one leaving headroom is preferred.
- **Why.** Two PRs each brought `lowering.Ctx` to 20 methods against a cap of 20, merged clean
  pairwise, and held 21 together (#1191, #1227).
- **Probe.** `git grep -hE '^func \(c \*?Ctx\) ' <ref> -- 'compilers/openapi/internal/lowering/*.go' | wc -l`
  at each head and on the trial merge.
