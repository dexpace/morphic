# Commits, PR text and scope

How to meet `CLAUDE.md`'s commit, PR and tracker rules, which no gate step reads.

## State a deliberate limitation at the code or rule that has it
`limitation-stated-where-it-lives`

- **Rule.** A limit left out of scope, a library limitation relied on, a known defect whose
  reproducer is removed, or a part of an issue declined is stated in a comment at the function or
  call that has it, or in the skill rule prescribing the mechanism, naming its tracking issue
  (`references/commits-and-prs.md#limitation-searched-on-what-it-names`), and in the PR body, in the
  same commit. A partly addressed issue is referenced, not closed.
- **Why.** Readers reach the code first; a limitation recorded only in a PR body, commit or tracker
  is invisible to whoever next edits that function (#239, #270). A skill rule named one of two
  gaps its PR body recorded (#1153).
- **Probe.** Quote, from where the Rule places it, the sentence stating each limitation the body or
  commits state; an issue number alone is none.

## Search the tracker on what a limitation names
`limitation-searched-on-what-it-names`

- **Rule.** Search for each limitation a body states on the tool or behaviour it names, not on the
  construct the change decides, and have it cite what turns up or say no issue exists.
- **Why.** A gap in `--explain` read as a decision while #693 tracked it, which searches on the
  construct do not return (#1191).
- **Probe.** List each limitation beside its
  `gh issue list --state all --limit 1000 --search "<tool or behaviour>"`.

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

- **Rule.** Every commit subject and the PR title are imperative Conventional Commits with no final
  period, within 72 characters counting the ` (#N)` a squash appends to the title and a branch's one
  commit, landing under its subject. A breaking commit carries `!` and explains the break in its
  body. No text carries attribution or session artifacts
  (`references/commits-and-prs.md#harness-attribution-yields`). No CI checks these (#1417).
- **Why.** Over-length and prose subjects land via squash, unfixable without rewriting a pushed
  branch; a break the title alone marks is invisible in branch history (#834, #157). A one-commit
  branch landed at 73 (#1232); a replaced subject was measured with the number (#1345).
- **Probe.** The push checklist; on an open PR's tree,
  `python3.14 .claude/skills/morphic-shipping-prs/scripts/subjects.py N "$(gh pr view N --json title --jq .title)"`
  and the checklist's artifact greps.

## Read a branch's own commits against the branch it targets
`own-commits-against-the-base`

- **Rule.** A branch's own subjects are those on its first-parent line that neither `origin/main`
  nor the branch it targets reaches, merges left out, as `subjects.py --base origin/<base>` reads
  them; its artifact grep reads every commit those two lack, merges included, as a default squash
  pastes them. A branch that left a stack also passes its former base's tip as a `--base`; the
  artifact grep does not.
- **Why.** Reading `origin/main..HEAD`, the checklist flagged a base PR's subject over the cap
  under a stacked PR's number (#1332), and a branch that left a stack read 55 commits as its own,
  of which 7 were (#1351), and one started on a moved `main` read `main`'s commits as its own
  (#1522 at b4795b61a).
- **Probe.** `git rev-list --count --first-parent --no-merges HEAD --not origin/main origin/<base>`
  matches the count `subjects.py` prints.

## The repository's rule overrides a harness's attribution reminder
`harness-attribution-yields`

- **Rule.** An agent harness reminding its sessions to end a PR body with a generated-with line, or
  a commit with attribution trailers, is overridden by
  `references/commits-and-prs.md#commit-and-pr-message-form`, as the reminder itself defers to
  project instructions: neither carries one.
- **Why.** #1387's body ended with a generated-with line and a session link that no PR below it in
  its stack carried, and its reviewer settled the clash unaided (#1387).
- **Probe.** The push checklist's artifact grep over the commits and the PR's text prints no hit.

## Hold a commit body when it is pushed and the landing body at merge
`body-lands-at-merge`

- **Rule.** A commit body could stand on `main` when it is pushed. A later commit making it stale is
  no finding, as a pushed branch is never rewritten and the body that lands is the one set at merge.
  A merge keeping the default paste edits a stale body out.
- **Why.** The repository's setting pastes every branch body while merges set their own, and a
  pushed body cannot change: `1b22933d` on #1233 went stale, and a review had to work out whether to
  report it.
- **Probe.** `git log -25 --first-parent --format=%b origin/main` shows the bodies merges set, and
  `gh api 'repos/{owner}/{repo}' --jq .squash_merge_commit_message` what an unedited squash pastes.

## Keep the diff to in-scope files and keep scratch notes out of the tree
`diff-contains-only-in-scope-files`

- **Rule.** Every changed path must belong to the PR's stated scope; session planning docs and
  working notes must be ignored before the first commit that would add them.
- **Why.** Plans and temporary rewrite notes were committed toward main (#5, #119).
- **Probe.** `git diff --stat origin/main...HEAD` and match each path to the PR title;
  `git ls-files docs | grep -iE 'plan|notes|rewrite|superpowers'`.

## Probe a change to an ignore rule at base and head
`ignore-rule-both-ways`

- **Rule.** Remove an ignore rule only after confirming local tools no longer generate those paths,
  and add one only after confirming it hides nothing meant to be committed.
- **Why.** A removed `.claude/` ignore entry had to be restored (#324), and an added one had to
  become `.claude/*` with an exception before the skills could be committed (#97, #1143).
- **Probe.** For a removed `.gitignore` line, `git status --ignored` before and after. For an added
  one, `git add -A --dry-run` and `git check-ignore -v <path>` at base and head over planted paths,
  one a tool documents as committable among them.

## Close only the issues the PR finishes, one keyword each
`closing-keywords`

- **Rule.** On merge, GitHub closes each issue that `close`, `fix` or `resolve`, in any tense and
  case, colon or not, precedes as `#N` or `<owner>/<repo>#N` anywhere in the body, and a
  keyword reaches only the reference after it. Write one `Closes #N` line per finished issue,
  narrate other work with other verbs ("landed"), and give a stack landing through one PR every
  child's `Closes #N`: a keyword links only in a PR based on `main`.
- **Why.** "closed #907", written about another PR, made #948 claim #907; #1016 wrote a line per
  issue, since a list after one keyword closes its first alone; #464 left its stack's issues open.
- **Probe.** Read each (`references/commits-and-prs.md#closing-issue-read-whole`). Seconds after
  each body edit, as the field lags, compare
  `gh pr view <N> --json closingIssuesReferences` with the body's:
  `grep -noiE '\b(close[sd]?|fix(e[sd])?|resolve[sd]?):? +([[:alnum:]_.-]+/[[:alnum:]_.-]+)?#[0-9]+'`.

## Read each issue a PR closes whole, and again before saying it closes
`closing-issue-read-whole`

- **Rule.** For each issue in `closingIssuesReferences`, read
  `gh issue view <n> --json state,stateReason,body,comments,updatedAt` when the PR opens and again
  before a report says the PR closes it. A closed issue's closing comment is a recorded decision,
  and its reference leaves the body; a comment or a body edit is part of the ask; each
  `## Out of scope` item or deferred alternative naming work still to do goes to an open issue,
  stated where `references/commits-and-prs.md#limitation-stated-where-it-lives` places it.
- **Why.** #1210 kept `Closes #1251` after #1251 closed as not planned, and missed asks eight of
  its issues held in comments or later edits; closing #1433 would have left a case it put out of
  scope stated nowhere (#1210, #1435).
- **Probe.** The report gives each closing issue's `updatedAt` as last read, none newer, then
  `references/commits-and-prs.md#closing-issue-met-at-its-site`.

## Hold a closing issue's title and Direction to the head
`closing-issue-met-at-its-site`

- **Rule.** Before a report says a PR closes an issue, read its title against the site it names on
  the head, where a defect it states must now be false, an imperative done and a question answered,
  and match each step of its Direction to the line meeting it, or say in the body why none does.
- **Why.** At 78c30a744, #1522 closed #1294, #1326, #1348, #1362, #1461 and #1495 while the rule
  each named, in its title or its Direction, read as before, the fix sitting in a new rule the old
  one never cited, and dropped #1347's step running the gate steps after a failure (#1522).
- **Probe.** For each closing issue, quote the site its title names, as it reads at the head.

## Check the tracker for a recorded decision before changing what it covers
`follow-recorded-decisions`

- **Rule.** Before changing or adding a line a decision may cover, search open and closed issues
  for the file, symbol or construct, and follow the decision or state the departure in code and PR
  body. Search too for each behaviour the PR's tests pin as correct or as a control, and read the
  normative docs before filing an issue questioning one. A recorded blocking edge must not
  contradict why the item was split out.
- **Why.** Changes settled questions issues had framed (#404, #379, #158); a pointer a closed issue
  had ruled out was written beside a component (#296, #107); a test pinned as a control what an open
  issue called a defect (#542, #534).
- **Probe.** `gh issue list --state all --limit 1000 --search "<file, symbol or construct>"`; the
  PR body cites each hit.

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

- **Rule.** A check meant to stop a commit or push gates it by its exit status, in one command:
  `check > log 2>&1 && git commit ...`. Never pipe a gating check into `tail` or `grep`, chain it
  with `;` or a newline, or print a length and commit on the same line: a pipeline's status is its
  last stage's, and a printed number or finding gates nothing. A check with hits to justify, such as
  an artifact grep, is read in full; the push runs behind the checks exiting non-zero on a hit.
- **Why.** Over-cap subjects and lint and archtest failures were committed past checks that printed
  them, one exiting 0 (#1007, #1005, #1046, #1141, #1147, #1153, #1402).
- **Probe.** Plant a failure the check must catch and run `<check> && echo pushed`: nothing
  prints.

## Mark a change to exported API as breaking
`exported-api-change-needs-bang`

- **Rule.** A change that removes or changes an exported identifier outside an `internal/` or
  `main` package, or adds a method to an exported interface with no unexported method, breaks
  callers or implementations outside the repository, even with nothing inside it affected: the
  title carries `!`, the commit body explains the break, and the PR body has a Breaking section.
- **Why.** `compile.NewTypes` lost its parameter in a PR titled as a plain `fix` until review added
  `!` (#551); `compilers/compile` is the framework other compilers build on, and earlier
  removals there were marked breaking (#239, #496).
- **Probe.** For each public package changed, write export data in worktrees of the merge base
  and head with
  `go run golang.org/x/exp/cmd/apidiff@v0.0.0-20260410095643-746e56fc9e2f -w <file> <import path>`,
  then compare with
  `go run golang.org/x/exp/cmd/apidiff@v0.0.0-20260410095643-746e56fc9e2f <old> <new>`. It
  misses struct tags, which `ir` holds by `TestIRVersion_PinsTheEncodedShape`.

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
  `gh pr view <N> --json body --template '{{.body}}'`, which adds no newline, and apply it behind
  `references/commits-and-prs.md#send-only-what-the-fetch-produced` with
  `gh api -X PATCH repos/<owner>/<repo>/pulls/<N> -F body=@<file>`, not `--input`, which sends the
  file as the request. Anchor a scripted edit on the text as written, line breaks included, and
  stop when an anchor is absent.
- **Why.** A body rebuilt from a stale draft erased another author's rewrite (#1143), and
  `gh pr edit` applied a body and never exited (#724). `--jq .body` appends a newline, so each
  edit added a blank line, and 26 gathered unnoticed (#1143).
- **Probe.** Fetch it back the same way and `cmp` it with the file sent.

## State the head in a PR body, and each round's fixes in its commits
`body-states-the-head`

- **Rule.** A PR body states the head: its Summary and Test plan as they now hold, rewritten in
  place when a commit changes what they say. What a review round fixed is told in that round's
  commit messages, never appended to the body as a log of rounds.
- **Why.** #1522's body gained a paragraph per review round saying what it fixed, which each later
  round's reader re-read and re-checked against the tree (#1530).
- **Probe.** Read `gh pr view <N> --json body --jq .body`: each sentence holds of the head, and none
  tells what a round or an earlier commit changed.

## Send only a body the fetch produced
`send-only-what-the-fetch-produced`

- **Rule.** Gate a body's send on its fetch, in one command, and keep the fetched copy to restore
  from, as a failed fetch leaves an empty or partial file and sending it replaces the description:
  `f=$(mktemp) && gh pr view <N> --json body --template '{{.body}}' > "$f" && test -s "$f" && [ "$(grep -c '^Closes #' "$f")" -eq <its Closes count> ] && cp "$f" "$f.orig" && <edit "$f"> && gh api -X PATCH repos/<owner>/<repo>/pulls/<N> -F body=@"$f"`.
- **Why.** A fetch that timed out left an empty file, and a send outside the `&&` chain blanked
  #1322's body (#1322); a fetch run from a removed worktree wrote an empty file on #1219 (#1219).
- **Probe.** With `<N>` set to 0, the command stops at the fetch and sends nothing.

## A conflicting PR gets no CI run
`conflicting-pr-has-no-run`

- **Rule.** After a push, read `mergeable` beside the checks: GitHub starts no `pull_request` run
  for a PR it cannot merge, so an empty check list with `CONFLICTING` is no run, and the next step
  is to merge `main`, not to wait. The newest run is then for an older commit, and says nothing of
  the head.
- **Why.** A wait for #1210's checks ran nine minutes after #1238 landed a conflicting edit, with
  no run to wait for and the newest run on the commit before the head (#1210).
- **Probe.** `gh pr view <N> --json mergeable,statusCheckRollup --jq '{m:.mergeable,n:(.statusCheckRollup|length)}'`.

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
