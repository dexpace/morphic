# Review rounds

How a round runs and when the loop ends: where the review reads the change, what a decision rests
on, how a finding is ranked, a fix designed and checked, and what is posted. The angle references
say what a round attacks, and `references/readers.md` who reads it. The rules are read from `main`,
by `git show origin/main:<path>` or `git archive origin/main .claude/skills | tar -x -C <dir>`,
whatever the head holds, unless the change under review edits them, when its copy is what is
reviewed.

Before any fetch: with no `origin`, or in a repository that must stay unwritten, take
`references/rounds.md#review-without-origin`; check that no other checkout holds work on the PR's
branch (`references/rounds.md#branch-held-elsewhere`); and where the PR's stacked base has landed,
as `gh pr view <base PR> --json state,mergedAt` shows while a `baseRefName` read before the
retarget still names the deleted branch, take
`.claude/skills/morphic-shipping-prs/references/merging-main.md#merge-a-landed-bases-final-head`.
Before the PR exists, as before a branch's first push, the base is the branch it will target, the
worktree is added at the branch's head, and `<N>` is the number the push checklist in
`morphic-shipping-prs` derives.

Each round reviews the tree as it will land, in a throwaway worktree
(`references/rounds.md#review-head-in-own-ref`):

```bash
base=$(gh pr view <N> --json baseRefName --jq .baseRefName)
git fetch origin main "$base" "+pull/<N>/head:refs/rv<session>/<N>" && main=$(git rev-parse origin/main)
git worktree add --detach <dir> refs/rv<session>/<N> && cd <dir>
git merge --no-edit "origin/$base" && git merge --no-edit "$main"
git merge-base --is-ancestor refs/rv<session>/<N> HEAD && git merge-base --is-ancestor "$main" HEAD
```

A conflict the review can resolve mechanically is resolved in that worktree and reported as the
review's own resolution, so every angle reads `main`'s new commits beside the branch; one whose
resolution is a design choice is aborted and reported with what could not be checked. Either way
the conflict is an `issue:`. Read and run only against that tree or a copy of it: a plant, a trial
merge or a sweep takes a worktree or copy of its own, and nothing writes to the checkout you started
from. Every later read of `main` is `$main` (`references/rounds.md#pin-main-for-the-round`). A
round's report names what it reviewed, the merged commit among it, each delegated reader's minutes
and tool calls from its hand-back, each angle's matrix, then the findings ranked:

```text
Round <n>: head <sha>, base <sha>, main <sha>, merged <sha>
reader <minutes> minutes, <tool calls> tool calls
<angle>: <cells run> run, <cells settled> settled (<reason each>), findings <refs>
unreached <cells>; reopened <cells>, each with what its clearance lacked
narrowed: <round before>, <its findings>, <commits since>
<rank> <angle> <file>:<line> <rule broken> <command> <output> <smallest fix>
```

## Build the review from a ref only your session writes
`review-head-in-own-ref`

- **Rule.** Fetch the PR's head into a ref only your session names and add the worktree from it,
  never from `FETCH_HEAD`, which every session in a checkout shares and another's fetch replaces.
  The worktree's `HEAD` is the PR's `headRefOid` before any merge, and after the merges both of the
  setup's `--is-ancestor` checks exit 0. "Already up to date" from both merges marks a wrong fetch
  only when the first check fails, as a head already holding `main` prints it too.
- **Why.** A worktree added at `FETCH_HEAD` after the PR's fetch held `main`'s head, and the merge
  of `main` printed "Already up to date" (#1464); a correct no-op printed the same and failed a
  check by both parents (#1605).
- **Probe.** `git rev-parse HEAD` in the worktree, beside
  `gh pr view <N> --json headRefOid --jq .headRefOid`.

## Pin main's commit for the round, as every worktree shares its ref
`pin-main-for-the-round`

- **Rule.** `refs/remotes/origin/main` is one ref across a repository's worktrees, so another
  session's fetch moves it mid-round and its deletion fails a read. The setup pins what it fetched
  as `$main`, which each later merge, diff and log of the round reads, and the report's first line
  names. Where other sessions share the repository and a tool reads `origin/main` itself, as
  `subjects.py` does, review in the clone `references/rounds.md#review-without-origin` makes.
- **Why.** In two worktrees of one clone, a fetch in one moved `origin/main` in the other, and a
  deletion there failed its merge (#1590); `subjects.py` exits 2 on `main` under another name
  (#1403).
- **Probe.** `git reflog show refs/remotes/origin/main` lists no fetch after the setup's.

## Review with no `origin`, or in a repository you must not write to
`review-without-origin`

- **Rule.** With no `origin`, fetch `main` by URL as the remote-tracking branch `origin/main`, the
  name the setup, checklist and scripts read, and `pull/<N>/head` into a ref of your own, and
  set `GH_REPO=<owner>/<repo>`, which `gh` and its placeholders read without a remote. Where the
  repository must stay unwritten, review in a local clone, which holds the history and writes only
  its directory: `git clone -n <repo> <dir>`,
  `git -C <dir> update-ref refs/remotes/origin/main <base>`, then
  `git -C <dir> switch --detach <head>`.
- **Why.** #1233's, #1235's and #1238's reviews improvised these, one writing metadata its round
  ruled out. `subjects.py` exits 2 when `main` sits in a ref of another name (#1403). With no
  remote, `gh` found no repository; an unpacked copy, no history (#1537).
- **Probe.** In a new `git init`, run both fetches, then `git for-each-ref`.

## Clone with no checkout a switch would keep
`clone-with-no-checkout`

- **Rule.** A clone that goes on to another commit, for a review or a simulated run, is made with
  `git clone -n`: a first checkout converts line endings by the attributes of the branch it starts
  on, and a switch to a commit holding the same blob keeps the conversion, which `git status` does
  not show.
- **Why.** With `eol=crlf` asked for a golden on the starting branch alone, a clone switched to
  `main` held the golden converted, `git status` empty and `TestGolden_HintPositions` failing,
  where a `-n` clone passed (#1599).
- **Probe.** After a switch, `cmp` a file the starting branch's attributes convert against the same
  file in a `-n` clone.

## Check that no other checkout holds work on the PR's branch
`branch-held-elsewhere`

- **Rule.** Before the setup's fetch, look for another checkout on the PR's branch:
  `git worktree list | grep -F "[<head ref>]"`. Where one holds commits its remote lacks
  (`git -C <dir> log --oneline origin/<branch>..HEAD`), read them before reviewing and ask who owns
  the branch before pushing, and before each push
  `git merge-base --is-ancestor origin/<branch> HEAD` confirms nothing moved the remote meanwhile.
- **Why.** A second worktree on #1269's branch held eight unpushed commits fixing three inputs whose
  cost grew with the square of their size, which the setup never led to (#1269).
- **Probe.** `git worktree list`, read before the fetch.

## Read the gate from CI, and run locally only what CI cannot
`ci-is-the-gate`

- **Rule.** The gate is CI's run of `.github/workflows/gate.yml`, read with its commit.
  Never rerun it locally, suites included, nor re-check by eye what it enforces. For a head behind
  `main`, list `main`'s commits touching its files, run `go build ./...` on the merged tree, and
  name merging `main` the next step. Locally, run only what CI cannot: probes, mutants,
  differentials, measurements, and the skills lint and scripts' tests, run by no CI. A run still
  going is reported running, never waited on; a conflicting PR has none, nor an unpushed
  branch (`references/claims-and-process.md#gate-claim-without-a-run`). Read job logs by
  `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#read-a-ci-jobs-log`.
- **Why.** A local `make gate` on #1278's merged tree failed on a stale linter, then stalled in
  coverage (#1286); a conflicting PR's empty check list read as pending (#1285).
- **Probe.** `gh pr view <N> --json headRefOid,mergeable,statusCheckRollup`.

## Decide nothing on an assumption
`decide-on-evidence`

- **Rule.** Each decision a review makes or accepts (a finding, its rank, a cell settled by a
  reason, a fix, a design) rests on two pieces of evidence: the text of the vision it serves
  (`CLAUDE.md`'s invariants, `docs/architecture.md`, `docs/prior-art.md`, a normative document, the
  tracker's plan) and a run showing it holds in practice (a minimal spec, the corpus, the golden
  spec, a measurement). Measure each premise
  (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#measure-the-premise`) and check
  a fact where it happens, not where it is configured; what lacks either is a `question:`.
- **Why.** A review ranked a pushed commit body as landing on `main` from the squash setting, while
  the last 25 merges had set their own (#1153), and #1233's rounds checked an archtest rule's
  completeness, never its fit with the code #704 plans (#1401).
- **Probe.** Each decision in the report cites its text and its run.

## Read the change deep, and the code living with it broad
`deep-and-broad`

- **Rule.** Deep: state each changed function's contract and the reason for each line, and test the
  PR's reasoning against the code, branch by branch. Broad: on the final tree, read each caller
  against the contract it now gets, each callee for the precondition the change relies on, often a
  comment saying "before" or "assumes", each site of the same mechanism, and each consumer of what
  it writes, in files the diff never touches too.
- **Why.** A new caller of a `nodeview` reader skipped the scan its comment said runs first, so a
  self-merging YAML file drew a false error past every corpus (#1038), and a move broke comments in
  files it never touched (#1104).
- **Probe.** The report lists the callers, callees, sites and consumers read, and what each showed.

## Try to disprove a finding before reporting it
`disprove-each-finding`

- **Rule.** A finding rests on a rule and a fact, so attack both. Read the rule to its end for its
  scope and exceptions, in `CLAUDE.md`, `CONTRIBUTING.md` or the guide; look for a decision on the
  line where `references/rounds.md#oddity-needs-a-comment` says one excuses it; weigh the PR's
  stated intent, read only now, after a blind first pass; and where a tool gave the count, sort its
  hits by what each one is before calling any a defect. Report what survives.
- **Why.** The 298 functions `thelper` reported were filed as a breach of `t.Helper`, though
  `CONTRIBUTING.md` exempts a table's case and none was a breach (#1178), and a row said nothing
  recorded a position `CONTRIBUTING.md` holds (#1173).
- **Probe.** For each finding, name the text that could have excused it and what sorted the tool's
  hits.

## A deliberate oddity is excused only by a comment at its line
`oddity-needs-a-comment`

- **Rule.** A deliberate oddity, such as a policy or a choice resting on an open issue, is a finding
  unless a comment at its line says so, or, for a lint finding, its exclusion in `.golangci.yml`
  with the reason beside it, as `CLAUDE.md` places one. A decision recorded only in the tracker, a
  PR body or a commit excuses nothing, as the next reader reaches the code first.
- **Why.** A limitation recorded only in a PR body, a commit or the tracker was invisible to
  whoever next edited the function (#239, #270).
- **Probe.** For each oddity cleared, quote the comment at its line.

## Rank a finding by what was shown
`rank-by-what-was-shown`

- **Rule.** Nothing left to find sets what gets fixed, not what may be claimed. An `issue:` or `nit:`
  gives its command, on the input it depends on, and quotes the rule it breaks. What is likely but
  unshown is a `question:` saying what was tried, the home of low confidence with no score to argue
  over, and taste with no rule to quote is a `suggestion:`. A defect the change exposes stays in
  scope; one outside its mechanism is filed, not ranked.
- **Why.** A defect that shows on one input is real here, as every corpus missed two (#1016), so a
  bar of certain findings alone would drop it, while a tool's count reported unsorted filed 298
  false breaches (#1178).
- **Probe.** Each `issue:` and `nit:` in the report gives its command, its input and its quoted rule.

## Read the reviews already standing once the angles are done
`read-standing-reviews`

- **Rule.** After the angles, not before, as a list read first sets the agenda as a PR body does,
  read the PR's reviews and comments with the commit each was written on. Check each against the
  head: one fixed is not reported again, one open is carried into the report, and one its author
  declined is answered, not repeated (`references/rounds.md#disprove-a-decline`).
- **Why.** A review requested changes while later work went on as though the PR only awaited an
  approval, seen only when a status check happened to print it (#1169).
- **Probe.** `gh api repos/dexpace/morphic/pulls/<N>/reviews --jq '.[] | [.user.login, .state, .commit_id] | @tsv'`,
  and the same path ending `comments`.

## Disprove the fact a decline rests on, as a finding
`disprove-a-decline`

- **Rule.** A finding its author declined stays declined only once the fact the decline rests on is
  named and attacked as `references/rounds.md#disprove-each-finding` attacks a finding: a comment
  calling a case unreachable is a claim to probe, not a reason.
- **Why.** A first review of what became #1515 declined a check because a comment called the case
  "unreachable from a parsed document", and #1508's input reached it (#1538).
- **Probe.** For each decline the report answers, the fact it rests on and the run that tested it.

## Design a fix before writing it
`design-the-fix-first`

- **Rule.** Before writing a fix, a redesign or any edit a finding calls for, name the mechanism
  behind the finding and every site of it, and at least two candidates: the smallest change and the
  one at the owner of the broken invariant. Check each against every requirement the mechanism must
  meet
  (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#prototype-against-every-requirement`),
  and choose by `CLAUDE.md`'s order, correctness first, writing down why the choice wins. Then see
  the test showing the defect fail, the fix make it pass, and the fix reverted make it fail again.
- **Why.** Three decode mechanisms #1329's rounds proposed in turn each passed a prototype of the
  one property they were for, and failed a requirement a sibling document states (#1402).
- **Probe.** The round's report gives each fix its candidates, why one won, and its fail, pass and
  fail runs.

## A fix a review hands on is reviewed before it leaves
`review-own-fixes`

- **Rule.** Give each fix a review pushes, posts as a patch or offers as a suggestion each angle
  its own diff can affect, Developer experience included, sweep its mechanism, and reread what it
  adds as unreviewed
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#reread-prose-after-a-redesign`).
  A fix to a mechanism a design document specifies first takes
  `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#prototype-against-every-requirement`
  and its row of `references/design.md#design-review-matrix`. Push a round's fixes once, after
  `references/rounds.md#author-pass-before-hand-off`, by the push checklist from the detached setup,
  then rerun the setup.
- **Why.** #1143's later rounds found defects its own fixes had added, such as a command failing
  from the root. #1233's author applied a posted redesign verbatim, and a later round found five
  nits in it (#1411).
- **Probe.** Before pushing or posting, apply the fixes in a throwaway worktree and read
  `git diff <round's base>..HEAD` with those angles.

## Recheck what a round's fixes can move before the next hand-off
`author-pass-before-hand-off`

- **Rule.** Before each hand-off, on the fix commit: rerun each plant whose verdict
  `.claude/skills/morphic-verifying-changes/references/mutation.md#verdict-outlives-unchanged-files`
  does not carry across the fixes; derive each number the PR body states from a command run there,
  its output kept beside the body draft
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#body-states-the-head`); and
  rerun the trial merges
  `.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-open-prs` and
  `.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-lints-the-skills` call
  for.
- **Why.** #1532's seventh-round findings sat in the sixth round's fixes, body or commit message: a
  rewritten test passed a mutant stopping a scan at a file's first citing line, and the body counted
  14 open PRs sharing a file, not six. Trial merges rerun after a fix found a conflict it gave an
  open PR (#1532).
- **Probe.** The round's saved state (`references/rounds.md#compact-at-a-round-boundary`), never the
  reader's brief, lists each plant rerun with its verdict, each body number with its command, and
  each trial merge's commit.

## Compact the main session only at a clean round boundary
`compact-at-a-round-boundary`

- **Rule.** Compact the main session, or ask whoever drives it to, at a clean round boundary, before
  its window's limit compacts it wherever it stands, and never mid-fix or mid-disproof. A clean
  boundary has the fixes pushed, the body updated, the next reader dispatched and the round's state
  written to a file outside the tree. After compacting, treat the summary as unverified, re-reading
  the round's report, the PR body and the saved state before acting on it.
- **Why.** #1532's main session reached 962k tokens before compacting on its own, and its summary
  carried two wrong figures forward as settled, the open PRs sharing a file and a token count
  doubled by counting transcript records, with no sign either was unchecked (#1532, #1542).
- **Probe.** The first calls after a compaction read those three.

## Review the tree as it will merge
`recheck-against-final-tree`

- **Rule.** After review fixes land, short of
  `references/later-rounds.md#nit-round-checks-its-fixes`, or `main` or the base is merged in,
  review the branch as it will merge: rerun the probes the body and comments cite and every sweep
  the branch relies on over the merged tree, re-diff each change `main` made to files the branch
  touches, and look for helpers `main` renamed, features on both sides that interact, and normative
  text `main` moved.
- **Why.** Merges dropped `main`'s wider change in a conflict, left comments asserting behaviour
  `main` had changed, and left PR bodies stating facts a later commit falsified (#837, #382); a
  merge base read after the merge was `main` itself (#1613).
- **Probe.** `git diff "$(git merge-base <merge>^1 "$main")" "$main" -- <files the branch touches>`,
  `<merge>` the merge of `main`: each hunk survives; rerun each command the body quotes,
  diffing its output.

## Find everything in the first round, and confirm the fixes in the second
`first-round-finds-everything`

- **Rule.** The first round runs every angle over the whole change, fills every cell of each
  angle's matrix however many findings come first, and keeps its probes in a script outside the
  tree; the second reruns it while a fresh reader runs every angle again
  (`references/readers.md#fresh-reader-for-the-second-round`), and the review ends by
  `references/rounds.md#review-ends-with-nothing-left`. A defect the second finds in what the
  first reviewed is a miss: name the cell or rule that should have held it, and sharpen that angle
  (`morphic-curating-skills`). One a fix added is a miss of
  `references/rounds.md#design-the-fix-first`; many send it back to Design.
- **Why.** #1329's review took eight rounds, each finding what the first should have, as each
  sampled what it checked (#1402).
- **Probe.** The report gives each round's matrices in full, and each miss its cell.

## End a review only on a round that finds nothing and leaves nothing
`review-ends-with-nothing-left`

- **Rule.** A cell a round's budget leaves is named unreached, never reported clean. The review
  ends on a round with no `issue:` or `nit:` and no cell unreached or reopened
  (`references/readers.md#audit-what-a-reader-cleared`); after any other, the next round runs from
  the setup, and from the third round reads what
  `references/later-rounds.md#later-rounds-read-what-moved` or
  `references/later-rounds.md#nit-round-checks-its-fixes` says.
- **Why.** A round of #1522 listed beside its findings the prescribed commands it had not run;
  under a bar of no `issue:` or `nit:` alone, that list beside no findings would have ended the
  review (#1522).
- **Probe.** The last round's report names no cell unreached or reopened.

## Post a finding so that acting on it is safe
`posted-findings`

- **Rule.** When asked to post, write one comment per defect, naming the rule it breaks, and link
  code by full SHA, as a line on a moving branch drifts. Offer a committable suggestion only if it
  fixes the defect alone, passes the checks it touches and breaks no rule on the same act or
  construct: otherwise describe the fix.
- **Why.** A suggested Probe said to delete a hit in a commit body, which a pushed branch cannot do,
  and a suggested regex missed a generic no test row held (#1169).
- **Probe.** Apply each suggestion in a throwaway worktree, run the checks it touches, and give it
  `references/rounds.md#review-own-fixes`.
