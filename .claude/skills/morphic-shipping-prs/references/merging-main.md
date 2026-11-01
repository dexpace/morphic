# Merging main into a branch

A branch that was right when its checks went green can be wrong once `main` moves, and a clean
merge says nothing about it. These rules are the ways a merge loses one side.

## Merge main before each review round and rerun what it can break
`merge-main-first`

- **Rule.** Merge `origin/main` into the branch before a review round and before calling it
  ready. Then build, vet and run the whole suite on the merge, `internal/archtest`'s caps among it,
  and re-plant each mutation the PR body cites: a clean merge can fail to compile, fail a test, or
  leave a test green for a mechanism the merge removed.
- **Why.** After a base PR's final round stopped handing over cached bytes, a stacked PR's dedup
  test stayed green with the dedup deleted (#565, #562); a conflict-free merge failed the doc
  comment cap `main` had adopted meanwhile (#562).
- **Probe.** `git merge --no-commit --no-ff origin/main`, then `go build ./...`, `go vet ./...`,
  `go test ./...`, and the PR's listed mutants.

## Merge a stacked branch's base first, as a merge of its own
`merge-the-base-first`

- **Rule.** A stacked PR lands on its base, not on `main`. Merge `origin/<base>` before
  `origin/main`, each as its own merge so that a defect shows against the merge causing it, and run
  on each what `references/merging-main.md#merge-main-first` names.
- **Why.** A head green alone, and reported `MERGEABLE` and `CLEAN`, stopped compiling once merged
  with its base, which merging `main` could not show (#1183, #1179).
- **Probe.** `gh pr view <N> --json baseRefName --jq .baseRefName`, then
  `git merge --no-commit --no-ff origin/<base>` and `go vet ./...` before touching `main`.

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

## Confirm a trial merge happened before reading its tests
`trial-merge-happened`

- **Rule.** Merge a trial by SHA, and confirm it with
  `git merge-base --is-ancestor <pr head> HEAD` before reading a result. `FETCH_HEAD` belongs to the
  worktree that fetched, so in a new one the merge fails, the tree stays the branch alone, and its
  tests pass.
- **Why.** A loop over the open PRs printed `ok` for each while every merge had ended in `fatal`
  (#1210).
- **Probe.** `git rev-list --parents -n1 HEAD` lists both heads.

## Trial-merge the open PRs after a change they may not survive
`trial-merge-open-prs`

- **Rule.** A change that removes or renames a target, file or identifier other open PRs may name,
  or alters what every file of a kind must hold, such as a golden, trial-merges each, before and
  after. Search each merged tree for the removed names and run the kind's tests there:
  `git merge-tree` reports conflicts, not failures, and GitHub's flag does not say which merge
  caused one.
- **Why.** Resolving a conflict over a removed make target would have kept the gate pointing at it
  (#828, #836, #503). A line every conformance golden gained merged cleanly with PRs adding goldens
  and failed on each (#1181, #1191, #1192).
- **Probe.** `git merge-tree --write-tree --name-only <base> <pr head>` for each open PR, then on
  the merge `git grep` each removed name and, for goldens,
  `go test ./compilers/openapi/e2e ./cmd/morphic-census`.
