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
  construct the change decides, and have it cite what turns up or say no issue exists. An issue it
  names takes `references/commits-and-prs.md#limitation-names-a-filed-issue`.
- **Why.** A gap in `--explain` read as a decision while #693 tracked it, which searches on the
  construct do not return (#1191).
- **Probe.** List each limitation beside its
  `gh issue list --state all --limit 1000 --search "<tool or behaviour>"`.

## Name a limitation's issue by a number the tracker holds
`limitation-names-a-filed-issue`

- **Rule.** Each issue a stated limitation names is `#` and digits, filed before the push. A
  placeholder standing for one still to file is filed first, and its number replaces it in the
  comment, the rule and the body.
- **Why.** #1498 named `#NEW-CATCHALL` in a doc comment, a normative Status line and its body, and
  six days on no such issue existed, every check green: `TestDocuments_LinksResolve` reads no `#`
  before letters (#1659).
- **Probe.** `gh issue view <N> --json url --jq .url` prints an `/issues/` URL for each issue a
  limitation names, where a pull request's number prints `/pull/`, and the push checklist's `cs`
  grep prints no `#` before capitals.

## Run the coverage gate on the final tree before claiming it
`coverage-gate-on-final-tree`

- **Rule.** Every new code path ships with the test that executes it, in its own package
  (`.claude/skills/morphic-verifying-changes/references/toolchain.md#coverage-per-package`). A PR
  body says the gate holds only for a tree `make coverage` or CI ran: the exact tree pushed.
- **Why.** One uncovered statement fails CI, and a pushed tree below the gate forces a follow-up
  commit and makes the body's claim false (#22, #373).
- **Probe.** Match each coverage claim in the PR body to that run, and a statement count to the
  line `gh run view <run> --log --job <job> | grep -o 'Coverage gate passed.*'` prints, `<job>`
  from `gh run view <run> --json jobs --jq '.jobs[]|select(.name|test("coverage"))|.databaseId'`;
  a count the body keeps names its run.

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

## Refuse only the subjects a push adds, and state a pushed one that breaks the rules
`pushed-subject-is-disclosed`

- **Rule.** The push checklist passes the branch as pushed, `subjects.py --pushed origin/<branch>`,
  and a review the head it fetched; `subjects.py` counts its commits among the branch's own
  (`references/commits-and-prs.md#own-commits-against-the-base`) but prints a finding on one as a
  `pushed commit` and refuses only the title and what the push adds: a pushed commit is never
  rewritten. The PR body names each such commit; on a one-commit branch, a second commit lands the
  title in its place.
- **Why.** Once a 73-character subject was pushed on #1689, the checklist refused every later push,
  so each round's fixes went up outside it, skipping the skills lint it gates (#1697).
- **Probe.** On a branch with a pushed subject over the cap, the checklist's `subjects.py` line
  exits 0 and prints the subject as a `pushed commit`.

## The repository's rule overrides a harness's attribution reminder
`harness-attribution-yields`

- **Rule.** An agent harness reminding its sessions to end a PR body with a generated-with line, or
  a commit with attribution trailers, is overridden by
  `references/commits-and-prs.md#commit-and-pr-message-form`, as the reminder itself defers to
  project instructions: neither carries one. One a pushed commit holds stays, as the branch is
  never rewritten: the body names the commit and says the squash body is set at merge, as the
  default paste carries it onto `main`.
- **Why.** #1387's body ended with a generated-with line and a session link that no PR below it in
  its stack carried, and its reviewer settled the clash unaided (#1387); reviews of two branches
  holding such a trailer each ruled on it alone (#1557).
- **Probe.** The push checklist's artifact grep over the commits and the PR's text prints no hit
  but on a commit the body names.

## Name a measured input by what it is, never by where it came from
`input-named-by-what-it-is`

- **Rule.** A PR's text and a commit message state an input they measure by what it is, such as the
  construct a schema writes or the API a spec describes, never by the third-party tool, repository
  or test suite it came from, nor by corpus counts identifying one. The golden spec stays nameable,
  as `CLAUDE.md` names it the thing measured.
- **Why.** Test plans named a generator project's spec, by repository and file, as a check's input,
  and counted the generator suites a corpus was drawn from; the push checklist's greps passed them,
  and only a later read caught each (#553, #1690).
- **Probe.** `gh pr view N --json title,body --jq '.title, .body' | grep -nE 'https?://|[[:alnum:]_.-]+\.(ya?ml|json|proto|graphql|tsp|smithy)([^[:alnum:]]|$)'`
  lists each link and spec file named; read each for the source it identifies.

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

## Open a pull request without labels
`pr-takes-no-labels`

- **Rule.** `gh pr create` takes no `--label`: labels are an issue's, set as `CLAUDE.md`'s "Filing
  issues" says, and a merged pull request carries none bar Dependabot's `dependencies`.
- **Why.** A labelled pull request shows in the label listings issue triage and tier audits read,
  and with no rule saying otherwise, #1691 was opened with an issue's labels (#1693).
- **Probe.** `gh pr list --state merged --limit 40 --json number,labels --jq '.[] | select(.labels | length > 0) | "\(.number) \(.labels | map(.name) | join(","))"'`
  prints only Dependabot's.

## Settle a PR's scope before its second review round
`scope-settled-by-the-second-round`

- **Rule.** From a PR's second review round, an issue joins it only when its fix changes the PR's
  own mechanism, bar a stacked child's issues a landing PR carries
  (`references/closing-issues.md#closing-keywords`). Any other goes in a PR of its own, reviewed
  alongside and converging on its own.
- **Why.** #1532 took on six more issues during its review, and four of its seventh round's six
  findings sat in their content (#1532).
- **Probe.** For each issue, bar a stacked child's, that a closing keyword in the body names
  (`references/closing-issues.md#closing-keywords`) and the body as the PR opened did not, the body
  names the change its fix makes to the PR's mechanism. The opening body is the `diff` that
  `gh api graphql -f query='{repository(owner:"dexpace",name:"morphic"){pullRequest(number:<N>){userContentEdits(last:1){nodes{diff}}}}}'`
  prints, or the body itself when it prints none; `closingIssuesReferences` is empty on a stacked
  PR.

## Probe a change to an ignore rule at base and head
`ignore-rule-both-ways`

- **Rule.** Remove an ignore rule only after confirming local tools no longer generate those paths,
  and add one only after confirming it hides nothing meant to be committed.
- **Why.** A removed `.claude/` ignore entry had to be restored (#324), and an added one had to
  become `.claude/*` with an exception before the skills could be committed (#97, #1143).
- **Probe.** For a removed `.gitignore` line, `git status --ignored` before and after. For an added
  one, `git add -A --dry-run` and `git check-ignore -v <path>` at base and head over planted paths,
  one a tool documents as committable among them.

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
  tells what a round or an earlier commit changed, bar what
  `references/commits-and-prs.md#earlier-tree-names-its-commit` allows.

## A sentence about an earlier tree names its commit
`earlier-tree-names-its-commit`

- **Rule.** A body sentence saying what a named commit's tree does under a named plant, as a Test
  plan's before and after does to show a test bites, holds of the head as a fact about that commit
  and stands. One naming no commit, as "before X" does, names it or says what the head's tree shows.
- **Why.** "Before the severity assertion both stayed green" left a review to rank it or pass it,
  and 6 of the last 60 merged bodies held the form (#1514, #1595).
- **Probe.** For each such sentence, its plant rerun at the commit it names.

## Send only a body the fetch produced
`send-only-what-the-fetch-produced`

- **Rule.** Gate a body's send on its fetch in one command, keeping the fetched copy to restore
  from: a failed fetch leaves an empty or partial file, and sending it replaces the description.
  `references/commits-and-prs.md#grep-an-edited-body-before-its-send` adds the greps:
  `f=$(mktemp) && gh pr view <N> --json body --template '{{.body}}' > "$f" && test -s "$f" && [ "$(grep -c '^Closes #' "$f")" -eq <its Closes count> ] && cp "$f" "$f.orig" && <edit "$f"> && ! grep -niE "${pat:?}" "$f" && ! grep -nE "${cs:?}" "$f" && gh api -X PATCH repos/<owner>/<repo>/pulls/<N> -F body=@"$f"`.
- **Why.** A timed-out fetch left an empty file that a send outside the `&&` chain put on #1322
  (#1322), and a fetch from a removed worktree wrote one on #1219 (#1219).
- **Probe.** With `<N>` set to 0, the command stops at the fetch and sends nothing.

## Grep an edited body before its send
`grep-an-edited-body-before-its-send`

- **Rule.** An edited body's send runs behind the push checklist's greps over the edited file, with
  its `pat` and `cs`, as `references/commits-and-prs.md#send-only-what-the-fetch-produced` chains
  them: an edit made once the push lands brings text the checklist never read.
- **Why.** The checklist grepped the body that the edit after the push replaced, so a sentence
  written for that push reached the live body unread (#1589).
- **Probe.** An edit adding a line the pattern holds stops the chain before its send.

## A conflicting PR gets no CI run
`conflicting-pr-has-no-run`

- **Rule.** After a push, read `mergeable` beside the checks: GitHub starts no `pull_request` run
  for a PR it cannot merge, so an empty check list with `CONFLICTING` is no run, and the next step
  is to merge `main`, not to wait. The newest run is then for an older commit, and says nothing of
  the head.
- **Why.** A wait for #1210's checks ran nine minutes after #1238 landed a conflicting edit, with
  no run to wait for and the newest run on the commit before the head (#1210).
- **Probe.** `gh pr view <N> --json mergeable,statusCheckRollup --jq '{m:.mergeable,n:(.statusCheckRollup|length)}'`.

## Read a CI job's log with `gh run view`, and check its size
`read-a-ci-jobs-log`

- **Rule.** Read a job's log with `gh run view <run> --log --job <job>`, the job's id from
  `gh run view <run> --json jobs`, and check the size of what a redirect wrote: the REST read,
  `gh api repos/<owner>/<repo>/actions/jobs/<job>/logs`, exits 1 and writes nothing without
  `--allow-escape-sequences`, so a count over its file reads as a real zero. While the run goes on,
  only that read, with the flag, returns a finished job's log.
- **Why.** A review reading statement counts and `(cached)` lines found no rule naming the command,
  and the obvious one wrote 0 bytes to its file; mid-run, `gh run view` refused a finished job's
  log (#1602).
- **Probe.** `gh run view <run> --log --job <job> | wc -c` prints more than 0.

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
