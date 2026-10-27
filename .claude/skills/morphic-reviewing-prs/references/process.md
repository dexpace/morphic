# Process, tracker and PR hygiene

The CLAUDE.md commit and PR rules, which no check enforces.

## State a deliberate limitation at the code that has it
`limitation-stated-in-code`

- **Rule.** A limit left out of scope, a library limitation relied on, a known defect whose
  reproducer is removed, or a part of an issue declined is stated in a comment at the function or
  call that has it, naming its tracking issue, and in the PR body, in the same commit. A partly
  addressed issue is referenced, not closed.
- **Why.** Readers reach the code first; a limitation recorded only in a PR body, commit or tracker
  is invisible to the next person editing that function (#239, #270).
- **Probe.** For each "out of scope", "known limit" or "tracked in #N" in the body or commits, grep
  the diff for a matching comment at the named code.

## Review the tree as it will merge
`recheck-against-final-tree`

- **Rule.** After review fixes land or `main` is merged in, review the branch as it will merge:
  rerun the probes the body and comments cite and every sweep the branch relies on over the merged
  tree, re-diff each change `main` made to files the branch touches, and look for helpers `main`
  renamed, features on both sides that interact, and normative text `main` moved.
- **Why.** Merges dropped `main`'s wider change in a conflict, left comments asserting behaviour
  `main` had changed, and left PR bodies stating facts a later commit falsified (#837, #382).
- **Probe.** `git diff <merge-base>..origin/main -- <files the branch touches>` and confirm each
  hunk survives; rerun each command the body quotes and diff its output.

## Run the coverage gate on the final tree before claiming it
`coverage-gate-on-final-tree`

- **Rule.** Every new branch ships with the test that executes it, in its own package
  (`coverage-per-package` in `.claude/skills/morphic-verifying-changes/references/toolchain.md`),
  and `make coverage` runs on the exact tree pushed before a PR body says the gate holds.
- **Why.** One uncovered statement fails CI, and a pushed tree below the gate forces a follow-up
  commit and makes the body's claim false (#22, #373).
- **Probe.** Match each coverage claim in the PR body to that run.

## Check commit subjects, PR titles and bodies against the message rules before first push
`commit-and-pr-message-form`

- **Rule.** Every commit subject and the PR title must be Conventional Commits, imperative, at most
  72 characters (the title at most 72 including the appended ' (#NNN)'); a breaking commit carries !
  and explains the break in its own body; PR bodies carry no tool attribution or session artifacts.
- **Why.** Over-length subjects and prose titles land on main via squash and cannot be fixed without
  rewriting a pushed branch; a break marked only in the PR title is invisible in branch history
  (#834, #157).
- **Probe.** git log --format=%s main..HEAD | awk 'length($0)>72'; printf '%s (#999)' "<title>" | wc
  -c; grep the title against ^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: ; run
  `scripts/artifact_scan.py` over the range and the PR body.

## Keep the diff to in-scope files and keep scratch notes out of the tree
`diff-contains-only-in-scope-files`

- **Rule.** Every changed path must belong to the PR's stated scope; session planning docs and
  working notes must be ignored before the first commit that would add them, and an ignore rule may
  be removed only after confirming local tools no longer generate those paths.
- **Why.** Design-only PRs carried go.mod changes, plans and temporary rewrite notes were committed
  toward main, and a removed .claude/ ignore entry had to be restored (#5, #119).
- **Probe.** git diff --stat main...HEAD and match each path to the PR title; git ls-files docs/ |
  grep -iE 'plan|notes|rewrite|superpowers'; for a removed .gitignore line, git status --ignored
  before and after.

## Check the tracker for a recorded decision before changing what it covers
`follow-recorded-decisions`

- **Rule.** Before touching a shared view, cache or plan dependency, search issues for a decision
  about it and follow it or state the departure in code and PR body; a recorded blocking edge must
  not contradict the reason the item was split out.
- **Why.** Changes settled questions issues had framed (a second memo on a view #404 wanted settled
  first) or made a standalone defect wait on the programme it was split from (#379, #158).
- **Probe.** `gh issue list --search "<file or symbol>"` for every shared structure touched; confirm
  the PR body cites any hit.
