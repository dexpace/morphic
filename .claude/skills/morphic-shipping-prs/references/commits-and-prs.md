# Commits, PR text and scope

How to meet `CLAUDE.md`'s commit, PR and tracker rules, which no gate step reads.

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

## Run the coverage gate on the final tree before claiming it
`coverage-gate-on-final-tree`

- **Rule.** Every new code path ships with the test that executes it, in its own package
  (`.claude/skills/morphic-verifying-changes/references/toolchain.md#coverage-per-package`). A PR
  body says the gate holds only for a tree `make coverage` or CI ran: the exact tree pushed.
- **Why.** One uncovered statement fails CI, and a pushed tree below the gate forces a follow-up
  commit and makes the body's claim false (#22, #373).
- **Probe.** Match each coverage claim in the PR body to that run.

## Check commit subjects, PR titles and bodies against the message rules before first push
`commit-and-pr-message-form`

- **Rule.** Every commit subject and the PR title are Conventional Commits, imperative, at most 72
  characters, the title counted with the ` (#N)` the squash appends for this PR's own number. A
  breaking commit carries `!` and explains the break in its own body. Every commit body could stand
  on `main`, since the default squash pastes them all. No text carries tool attribution or session
  artifacts.
- **Why.** Over-length subjects and prose titles land on main via squash and cannot be fixed without
  rewriting a pushed branch; a break marked only in the PR title is invisible in branch history
  (#834, #157).
- **Probe.** `scripts/preflight.py` measures each subject and the title and runs the artifact scan
  before a push; a reviewer's `review_setup.py` does the same on the PR.

## Keep the diff to in-scope files and keep scratch notes out of the tree
`diff-contains-only-in-scope-files`

- **Rule.** Every changed path must belong to the PR's stated scope; session planning docs and
  working notes must be ignored before the first commit that would add them, and an ignore rule may
  be removed only after confirming local tools no longer generate those paths.
- **Why.** Design-only PRs carried go.mod changes, plans and temporary rewrite notes were committed
  toward main, and a removed .claude/ ignore entry had to be restored (#5, #119).
- **Probe.** `git diff --stat origin/main...HEAD` and match each path to the PR title;
  `git ls-files docs | grep -iE 'plan|notes|rewrite|superpowers'`; for a removed `.gitignore` line,
  `git status --ignored` before and after.

## Close only the issues the PR finishes, one keyword each
`closing-keywords`

- **Rule.** On merge, GitHub closes each issue that `close`, `fix` or `resolve`, in any tense and
  case, a colon after it or not, precedes as `#N` or `OWNER/REPO#N` anywhere in the body, and a
  keyword reaches only the reference after it. Write one `Closes #N` line per finished issue,
  narrate other work with other verbs ("landed"), and give a stack landing through one PR every
  child's `Closes #N`: a keyword links only in a PR based on `main`.
- **Why.** "closed #907", written about another PR, made #948 claim #907; #1016 wrote a line per
  issue, since a list after one keyword closes its first alone; #464 left its stack's issues open.
- **Probe.** After each body edit, compare `gh pr view <N> --json closingIssuesReferences` with
  what this finds in the body:
  `grep -noiE '\b(close[sd]?|fix(e[sd])?|resolve[sd]?):? +([[:alnum:]_.-]+/[[:alnum:]_.-]+)?#[0-9]+'`.

## Check the tracker for a recorded decision before changing what it covers
`follow-recorded-decisions`

- **Rule.** Before changing or adding a line a decision may cover, search open and closed issues
  for the file, symbol or construct, and follow the decision or state the departure in code and PR
  body. Search too for each behaviour the PR's tests pin as correct or as a control, and read the
  normative docs before filing an issue that questions one. A recorded blocking edge must not
  contradict the reason the item was split out.
- **Why.** Changes settled questions issues had framed (#404, #379, #158); a pointer a closed issue
  had ruled out was written beside a component (#296, #107); a test pinned as a control what an open
  issue called a defect (#542, #534).
- **Probe.** `gh issue list --state all --search "<file, symbol or construct>"`; the PR body cites
  each hit.

## Commit only the files you meant to
`commit-only-what-you-meant`

- **Rule.** Build a command to `-o /dev/null` or a temporary path, keep probes and scratch files
  outside the tree, and read `git status --untracked-files=all` before committing. A new file under
  `<package>/testdata/fuzz/` is an input a fuzz run wrote when it failed or hung, which every
  later `go test` replays: move it out unless it is a deliberate regression seed.
- **Why.** `go build ./cmd/morphic` writes the binary into the working directory, one `git add -A`
  from a commit (#828).

## Gate a commit or push on each check's exit status
`gate-by-exit-status`

- **Rule.** A check meant to stop a commit or push gates it by its exit status, in the same
  command: `check > log 2>&1 && git commit ...`. Never pipe a gating check into `tail` or `grep`,
  chain it with `;`, or print a length and commit on the same line: a pipeline's status is its last
  stage's, and a printed number gates nothing.
- **Why.** Over-cap subjects and lint and archtest failures were committed past checks that printed
  them (#1007, #1005, #1046, #1141, #1147).
- **Probe.** `scripts/preflight.py` exits 1 on any finding; chain the push on it with `&&`.

## Mark a change to exported API as breaking
`exported-api-change-needs-bang`

- **Rule.** A change that removes or changes an exported identifier outside an `internal/` or
  `main` package breaks the callers outside the repository, even when no caller inside it uses it:
  the title carries `!`, the commit body explains the break, and the PR body has a Breaking
  section.
- **Why.** `compile.NewTypes` lost its parameter in a plain `fix` (#551); `compilers/compile` is
  the framework other compilers build on, and earlier removals there were marked breaking (#239,
  #496).
- **Probe.** `scripts/preflight.py` lists each exported function, method, type, constant and
  variable the branch removes or changes; read the diff for a struct field or a member of a
  grouped declaration, which it does not see.

## Build each commit of a split from whole file states, and test each
`split-into-tested-commits`

- **Rule.** When a finished diff becomes several commits, build each commit's tree from whole file
  states rather than hunk subsets, check that the last tree equals the working tree, and run vet
  and the touched packages' tests in a detached worktree of each commit. An exported helper
  committed before its first caller needs an `apiRoots` entry in `internal/archtest`, which the
  caller's commit removes.
- **Why.** Hunk subsets applied with `git apply --unidiff-zero` produced trees that did not parse
  (#948); a helper split from its caller failed the reachability test on two trees (#1141).
- **Probe.** `git worktree add --detach <dir> <commit>` for each commit, then `go vet` and the
  tests there.

## Edit a posted PR body from its live copy
`edit-pr-body-from-live-copy`

- **Rule.** Build an edited body from the live one, not a local draft, after fetching the branch
  to see who else works on it. Fetch it with
  `gh pr view <N> --json body --template '{{.body}}'`, which adds no newline, and apply it with
  `gh api -X PATCH repos/<owner>/<repo>/pulls/<N> -F body=@<file>`, not `--input`, which sends the
  file as the request. Anchor a scripted edit on the text as written, line breaks included, and
  stop when an anchor is absent.
- **Why.** A body rebuilt from a stale draft erased another author's rewrite (#1143), and
  `gh pr edit` applied a body and never exited (#724). `--jq .body` appends a newline, so each
  edit added a blank line, 26 before a diff showed them (#1143).
- **Probe.** Fetch it back the same way and `cmp` it with the file sent.

## Re-read the prose when a review round changes the design
`reread-prose-after-a-redesign`

- **Rule.** After a round changes the design, re-read against the new design the whole doc comment
  of every function whose contract changed, the commit messages, the PR body from its first
  sentence, the normative rule for the behaviour, and the neighbours' cross-references (the same,
  beside, above) around anything inserted. Read your own earlier rewrites as unreviewed.
- **Why.** A final round found five stale sentences the redesign never touched (#541); a stated
  limit falsified universals in other packages (#562); a fix left the normative rule stating the
  old bound (#1140).
- **Probe.** `git grep -n` each changed contract's old wording and number, and the issue a limit
  cites.
