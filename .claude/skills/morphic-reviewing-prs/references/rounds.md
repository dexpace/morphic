# Review rounds

How a round runs and when the loop ends: where the review reads the change, what a decision rests
on, how a finding is ranked, a fix designed and checked, and what is posted. The angle references
say what a round attacks.

Each round reviews the tree as it will land, in a throwaway worktree
(`references/rounds.md#review-head-in-own-ref`):

```bash
base=$(gh pr view <N> --json baseRefName --jq .baseRefName)
git fetch origin main "$base" "+pull/<N>/head:refs/rv<session>/<N>"
git worktree add --detach <dir> refs/rv<session>/<N> && cd <dir>
git merge --no-edit "origin/$base" && git merge --no-edit origin/main
```

Before the PR exists, as before a branch's first push, the tree is the branch's own worktree with
its base and `origin/main` merged in, and `<N>` is the number the push checklist in
`morphic-shipping-prs` derives. With no `origin`, or in a repository that must stay unwritten, take
`references/rounds.md#review-without-origin` before any fetch. Abort a conflicting merge and review what merged; the conflict is an
`issue:`. Probe only in that worktree. A round's report names what it reviewed, each angle's matrix, then the findings ranked:

```text
Round <n>: head <sha>, base <sha>, main <sha>
<angle>: <cells run> run, <cells settled> settled (<reason each>), findings <refs>
<rank> <angle> <file>:<line> <rule broken> <command> <output> <smallest fix>
```

## Build the review from a ref only your session writes
`review-head-in-own-ref`

- **Rule.** Fetch the PR's head into a ref only your session names and add the worktree from it,
  never from `FETCH_HEAD`, which every session in a checkout shares and another's fetch replaces.
  The worktree's `HEAD` is the PR's `headRefOid` before any merge.
- **Why.** A worktree added at `FETCH_HEAD` after the PR's fetch held `main`'s head, and the merge
  of `main` printed "Already up to date" (#1464).
- **Probe.** `git rev-parse HEAD` in the worktree, beside
  `gh pr view <N> --json headRefOid --jq .headRefOid`.

## Review with no `origin`, or in a repository you must not write to
`review-without-origin`

- **Rule.** With no `origin`, fetch `main` by URL as the remote-tracking branch `origin/main`, the
  name the setup, the push checklist and its scripts read, and `pull/<N>/head` into a ref of your
  own. Where the repository must stay unwritten, `git worktree add` is out: take
  `.claude/skills/morphic-verifying-changes/references/mutation.md#plant-in-an-unpacked-copy`.
- **Why.** Reviews of #1233, #1235 and #1238 each improvised these, and one wrote the worktree
  metadata its round had ruled out. `subjects.py` exits 2 when `main` sits in a ref of another name
  (#1403).
- **Probe.** In a new `git init`, run both fetches, then `git for-each-ref`.

## Read the gate from CI, and run locally only what CI cannot
`ci-is-the-gate`

- **Rule.** The gate is CI's run of `.github/workflows/gate.yml`, read with the commit it ran on.
  Never rerun it locally, a package's suite included, nor re-check by eye what it enforces. For a
  head behind a moved `main`, list `main`'s commits touching its files, run `go build ./...` on
  the merged tree, and name merging `main` the author's next step.
  Locally, run only what CI cannot answer: probes, mutants, differentials, measurements. A run
  still going is reported as running, never waited on; a conflicting PR has none.
- **Why.** On #1278's merged tree a local `make gate` failed on a stale linter, then was stopped in
  its coverage step (#1286), and a conflicting PR's empty check list read as pending (#1285).
- **Probe.** `gh pr view <N> --json headRefOid,mergeable,statusCheckRollup` names the head, its
  mergeability and checks.

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
  line in `.golangci.yml`'s exclusions, a comment or the tracker; weigh the PR's stated intent, read
  only now, after a blind first pass; and where a tool gave the count, sort its hits by what each
  one is before calling any a defect. Report what survives.
- **Why.** The 298 functions `thelper` reported were filed as a breach of `t.Helper`, though
  `CONTRIBUTING.md` exempts a table's case and none was a breach (#1178), and a row said nothing
  recorded a position `CONTRIBUTING.md` holds (#1173).
- **Probe.** For each finding, name the text that could have excused it and what sorted the tool's
  hits.

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

## Fan a review out only when asked, an angle to a reader
`fan-out-an-angle-to-a-reader`

- **Rule.** A review runs its angles in one reader by default, bar the second round's fresh one
  (`references/rounds.md#fresh-reader-for-the-second-round`). When asked to fan out, give each
  reader one angle and the code, with no conclusion of yours and, for Correctness, no PR body, so
  that independent reads stay independent. What returns goes through
  `references/rounds.md#disprove-each-finding` before it counts.
- **Why.** A finding comes back in the same confident register whether or not it holds, as the
  review of #1162's twelve PRs counted false alarms in both its readers (#1162).
- **Probe.** Each reader's brief names one angle and holds no finding of another's.

## Read the reviews already standing once the angles are done
`read-standing-reviews`

- **Rule.** After the angles, not before, as a list read first sets the agenda as a PR body does,
  read the PR's reviews and comments with the commit each was written on. Check each against the
  head: one fixed is not reported again, one open is carried into the report, and one its author
  declined is answered, not repeated.
- **Why.** A review requested changes while later work went on as though the PR only awaited an
  approval, seen only when a status check happened to print it (#1169).
- **Probe.** `gh api repos/dexpace/morphic/pulls/<N>/reviews --jq '.[] | [.user.login, .state, .commit_id] | @tsv'`,
  and the same path ending `comments`.

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
  and its row of `references/design.md#design-review-matrix`. Push a round's fixes once,
  behind the push checklist, as `git push origin HEAD:<branch>` from the detached setup, then rerun
  the setup.
- **Why.** #1143's later rounds found defects its own fixes had added, such as a command failing
  from the root. #1233's author applied a posted redesign verbatim, and a later round found five
  nits in it (#1411).
- **Probe.** Before pushing or posting, apply the fixes in a throwaway worktree and read
  `git diff <round's base>..HEAD` with those angles.

## Review the tree as it will merge
`recheck-against-final-tree`

- **Rule.** After review fixes land or `main` or the base is merged in, review the branch as it
  will merge: rerun the probes the body and comments cite and every sweep the branch relies on over
  the merged tree, re-diff each change `main` made to files the branch touches, and look for helpers
  `main` renamed, features on both sides that interact, and normative text `main` moved.
- **Why.** Merges dropped `main`'s wider change in a conflict, left comments asserting behaviour
  `main` had changed, and left PR bodies stating facts a later commit falsified (#837, #382).
- **Probe.** `git diff <merge-base>..origin/main -- <files the branch touches>` and confirm each
  hunk survives; rerun each command the body quotes and diff its output.

## Find everything in the first round, and confirm the fixes in the second
`first-round-finds-everything`

- **Rule.** The first round runs every angle over the whole change and fills every cell of each
  angle's matrix, however many findings come first. The second reruns the first's probes from a
  script outside the tree, while a fresh reader runs every angle again
  (`references/rounds.md#fresh-reader-for-the-second-round`), and ends the review on no `issue:` or
  `nit:`. A defect it finds in what the first reviewed is a miss: name the cell or rule that should
  have held it, and sharpen that angle (`morphic-curating-skills`). One a fix added is a miss of
  `references/rounds.md#design-the-fix-first`; many send the mechanism back to Design.
- **Why.** #1329's review took eight rounds, each finding what the first should have, as each
  sampled what it checked (#1402).
- **Probe.** The report gives each round's matrices in full, and each miss its cell.

## Give the second round a reader the first did not anchor
`fresh-reader-for-the-second-round`

- **Rule.** The second round's angles are run by one fresh agent, given the merged tree, the PR and
  this skill, never the first round's findings, matrices or reasons for its fixes. Its brief sets a
  tool-call budget and asks for a running list of what it reproduced. Each finding
  it returns is disproved in the main session (`references/rounds.md#disprove-each-finding`)
  before it counts. Where no agent can run, the main session rereads the merged diff before its
  own notes.
- **Why.** Fixes #1329's rounds proposed each passed a prototype of the property it was for and
  failed a requirement a sibling document states, seen only a round later (#1402), and seven
  finders on #1016's review ran 55 minutes before one returned (#1016).
- **Probe.** The reader's brief holds nothing from the first round, and names its budget.

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
