# Process, tracker and PR hygiene

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

The CLAUDE.md commit and PR rules, which no check enforces.

## Write a deliberate limitation or deferred defect at the code that has it, not only in the PR body
`limitation-stated-in-code`

- **Rule.** Any limit deliberately left out of scope, known library limitation relied on, or known defect whose reproducer is removed must be stated in a comment at the function or call that has it, with its tracking issue, and also in the PR body, in the same commit.
- **Also.** When a change deliberately leaves something undone (an exemption wider than its reason, an unconditional report where invariant 6 would want a policy switch, declined parts of an issue), state it on the code that holds the gap, name the tracker issue, and use Refs rather than Closes for a partially addressed issue.
- **Why.** Readers reach the code first; a limitation recorded only in a PR body, commit message or tracker is invisible to the next person editing that function.
- **Probe.** For every 'out of scope', 'known limit', 'tracked in #N' or removed seed mentioned in the PR body or commits, grep the diff for a matching comment at the named function or call; any without one is a finding.
- **Seen in #239** (34f99985): “Both are named in the pull request; a reader reaches the code first.”
- **Seen in #270** (da48be4c): “it was recorded only in the pull request body -- a reader arriving at”

## Re-review the final tree after review fixes or a merge of main
`recheck-against-final-tree`

- **Rule.** After review fixes land or main is merged in, re-review the branch as it will merge: re-run the probes that comments and the PR body cite on the merged tree, re-diff every semantic change main made to files the branch touches, and rewrite any PR-body or comment claim the later commits changed.
- **Also.** After merging main into a branch, re-run every sweep, invariant and widened check the branch relies on over the merged tree, since merged-in code can reintroduce what the branch removed or predate its rule.
- **Why.** Merges here silently dropped main's wider change in a conflict, left comments asserting behaviour main had changed, and left PR bodies stating facts a later commit falsified; unreviewed fixes merged as if reviewed.
- **Probe.** After git merge main, run git diff <merge-base>..main -- <files the branch touches> and confirm each hunk survives in HEAD; re-run each command quoted in the PR body and comments on HEAD and diff its output against the quote; re-read the PR body top to bottom against git diff main...HEAD.
- **Seen in #837** (a1c4f4aa): “One change is not carried over yet. Main widened the ValueKind”
- **Seen in #382** (a498517b): “Stale after the merge: `additionalOperations` is no longer dropped.”

## Run the 100% coverage gate on the final tree before pushing or claiming it holds
`coverage-gate-on-final-tree`

- **Rule.** Every new branch ships with the test that executes it, in the same change, and the gate is run on the exact tree being pushed before a PR body says it holds. A new package needs its own test file, because coverage is attributed per package test binary.
- **Why.** One uncovered statement fails CI; pushing below the gate forces a follow-up commit and makes any PR-body claim that the gate holds false.
- **Probe.** Run `make coverage` (judged by `cmd/morphic-covergate` from the profile's statement count, not the rounded percentage) on the final commit; for a new package confirm it has a _test.go of its own; grep the PR body for coverage claims and match them to that run.
- **Seen in #22** (14c5c37c): “Restores compilers/openapi statement coverage to 100%.”
- **Seen in #373**: “package is instrumented only by its own test binary, so one with statements and no test”

## Check commit subjects, PR titles and bodies against the message rules before first push
`commit-and-pr-message-form`

- **Rule.** Every commit subject and the PR title must be Conventional Commits, imperative, at most 72 characters (the title at most 72 including the appended ' (#NNN)'); a breaking commit carries ! and explains the break in its own body; PR bodies carry no tool attribution or session artifacts.
- **Why.** Over-length subjects and prose titles land on main via squash and cannot be fixed without rewriting a pushed branch; a break marked only in the PR title is invisible in branch history.
- **Probe.** git log --format=%s main..HEAD | awk 'length($0)>72'; printf '%s (#999)' "<title>" | wc -c; grep the title against ^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: ; run `scripts/artifact-scan.sh` over the range and the PR body.
- **Seen in #834**: “One commit subject on the branch, `hoist a content scalar through hoistFormatScalar`, is 77 characters, over the 72-character cap.”
- **Seen in #157** (f6fc15c3): “This PR opened with the same defect.”

## Keep the diff to in-scope files and keep scratch notes out of the tree
`diff-contains-only-in-scope-files`

- **Rule.** Every changed path must belong to the PR's stated scope; session planning docs and working notes must be ignored before the first commit that would add them, and an ignore rule may be removed only after confirming local tools no longer generate those paths.
- **Why.** Design-only PRs carried go.mod changes, plans and temporary rewrite notes were committed toward main, and a removed .claude/ ignore entry had to be restored.
- **Probe.** git diff --stat main...HEAD and match each path to the PR title; git ls-files docs/ | grep -iE 'plan|notes|rewrite|superpowers'; for a removed .gitignore line, git status --ignored before and after.
- **Seen in #5** (0b692476): “The planning documents under docs/superpowers/plans/ are local”
- **Seen in #119**: “it is temporary and should be deleted when the rewrite finishes.”

## Check the tracker for a recorded decision before changing what it covers
`follow-recorded-decisions`

- **Rule.** Before touching a shared view, cache or plan dependency, search issues for a decision about it and follow it or state the departure in code and PR body; a recorded blocking edge must not contradict the reason the item was split out.
- **Why.** Changes settled questions issues had framed (a second memo on a view #404 wanted settled first) or made a standalone defect wait on the programme it was split from.
- **Probe.** `gh issue list --search "<file or symbol>"` for every shared structure touched; confirm the PR body cites any hit.
- **Seen in #379** (b92870d6): “This PR is #338's index, adds a second memo to that same shared view, and cites neither #404 nor #402”
- **Seen in #158** (c493c8d2): “Also unblocks the naming-divergence defect, which had been made to wait”
