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

## Trial-merge the open PRs after removing a name they may use
`trial-merge-open-prs`

- **Rule.** A change that removes or renames a target, file or identifier other open PRs may name
  trial-merges each of them, before and after, and searches each merged tree for the removed
  names; GitHub's conflict flag does not say which merge caused a conflict.
- **Why.** Removing a make target made two open PRs conflict in the lines naming it, and the obvious
  resolution would have kept the gate pointing at a deleted target (#828, #836, #503).
- **Probe.** `git merge-tree --write-tree --name-only <base> <pr head>` for each open PR, then
  `git grep` the merged tree for each removed name.
