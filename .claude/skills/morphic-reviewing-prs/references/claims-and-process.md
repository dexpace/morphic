# Claims and process

The last angle: whether every sentence the change writes is true on the final tree, and whether the
PR ships by the repository's rules. It loads `morphic-shipping-prs`.

1. **Claims.** Check every member of each claim class
   (`references/claims-and-process.md#sweep-each-claim-class`), counts by
   `.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md`, a gate sentence
   at a head with no run by `references/claims-and-process.md#gate-claim-without-a-run`, each
   edited rule and `SKILL.md` line, a cut to meet a word cap too, against `main`'s words
   (`references/claims-and-process.md#cut-against-mains-words`) and as an edited rule beside the
   rule it cites (`references/claims-and-process.md#edited-rule-against-its-neighbours`), and run
   each command an edited rule prescribes
   (`references/claims-and-process.md#run-prescribed-commands`), with the flags a neighbouring rule
   passes the same tool (`references/claims-and-process.md#command-keeps-its-neighbours-flags`).
2. **Process.** Run the push checklist in `morphic-shipping-prs` without its push, on the PR's tree
   and text; then hold title, commits, body, labels and scope to
   `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md`, closing keywords to
   `.claude/skills/morphic-shipping-prs/references/closing-issues.md`, and open PRs to
   `.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-open-prs`, counting a
   cap they share at each head
   (`.claude/skills/morphic-shipping-prs/references/merging-main.md#cap-two-prs-share`). An edited
   body is sent only from its fetch
   (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#send-only-what-the-fetch-produced`).
3. **Closed issues.** Read each issue the PR closes whole against the diff, its state, comments and
   out-of-scope items included
   (`.claude/skills/morphic-shipping-prs/references/closing-issues.md#closing-issue-read-whole`),
   its title and Direction against the head
   (`.claude/skills/morphic-shipping-prs/references/closing-issues.md#closing-issue-met-at-its-site`),
   its reproduction run on the base and on the head
   (`.claude/skills/morphic-shipping-prs/references/closing-issues.md#closing-issue-reproduction-reruns`),
   what in the tree still cites it
   (`.claude/skills/morphic-shipping-prs/references/closing-issues.md#closing-issue-cited-in-the-tree`)
   and what in the tracker does
   (`references/claims-and-process.md#tracker-on-what-cites-a-closing-issue`),
   a sidebar link to it
   (`.claude/skills/morphic-shipping-prs/references/closing-issues.md#closing-sidebar-link`),
   and each choice resting on an open issue against the open PRs settling it
   (`references/claims-and-process.md#open-issue-against-open-prs`).

The matrix is each claim class with the members checked, and each closed issue with its asks.

## Check every class of claim in full, in the first round
`sweep-each-claim-class`

- **Rule.** Before the first finding, list the change's claim classes and check every member: each
  name against the tree, citation against the item it cites
  (`references/claims-and-process.md#citation-per-case`), no claim resting on an open PR's head
  (`gh pr view <N> --json state,headRefOid`), statement about `main`'s code, library statement
  against its `docs/parser-library.md` entry, quoted command run from the root, script run in each
  context (a subdirectory, each platform branch), body sentence on the final tree, each construct a
  rule settles (`references/claims-and-process.md#tracker-on-what-a-rule-settles`) and each edited
  rule (`references/claims-and-process.md#edited-rule-against-its-neighbours`). A class sampled is
  reported unreviewed, never clean.
- **Why.** #1143's review took nine rounds, each sampling a class, as a misattribution sat
  unsampled. Whys pinned claims to moving open PRs (#1409, #1425).
- **Probe.** The report gives each class and the members checked; fewer than all is a round still
  open.

## Read a citation against every case the citing text sends to it
`citation-per-case`

- **Rule.** A rule that routes a set of cases to the entries it cites claims each case: check every
  case its scope reaches against the entry it is sent to, not only that the entry exists and reads
  as described. A widened or new scope is read case by case.
- **Why.** A rule widened from load and binding to lowering kept citing two entries that hold no
  lowering bound's kind; the citations resolved, so the claim sweep passed it (#1441).
- **Probe.** For each case the scope reaches,
  `awk -v pat='<the case>' '/^\040{0,3}(\140{3}|~{3})/{c=!c}!c&&/^#+ /{h=$0} $0 ~ pat {print FNR": "h}' <cited file>`
  prints the entry holding it, which must be the one the citing text sends it to.

## Read an edited rule against those on its acts and constructs
`edited-rule-against-its-neighbours`

- **Rule.** For a change to `.claude/skills` or `CLAUDE.md`, name the acts each edited rule governs
  (commit, push, review) and constructs it names (a keyword, a field, a check), grep both for other
  rules on each, and read each pair for an instruction one gives and the other forbids, keeping the
  best of both where they do. A script finds a pair, not a conflict.
- **Why.** Two rules a PR edited held against the tree, while one gated a push on reading a
  checklist's output and the other on its exit status in one command (#1153, #1160). A search by
  act missed a rule on the same construct (#1426).
- **Probe.** The report lists each edited rule, its acts and constructs and the rules read against
  it; a rule with none is a round still open.

## Read each cut against main's words, not only beside the rules it cites
`cut-against-mains-words`

- **Rule.** Where the change shortens or rewords a rule or a `SKILL.md` passage, as a word cap
  forces, read `git diff --word-diff "$main"...<head> -- '.claude/skills/*.md'` and place each
  removed span: carried into the head's text, or a change of meaning the PR states and wants. A
  span that is neither is a condition the cut dropped.
- **Why.** At #1619's de5d2d471, a cut fitting
  `.claude/skills/morphic-verifying-changes/references/mutation.md#read-the-kill-in-the-output`
  within 150 words dropped "a timeout" from the runs giving no verdict, so a new panic clause read a
  `-timeout` expiry as a kill. Rounds reading cut lines beside the rules they cite passed it, and
  the word diff against `main` showed it (#1619 at ef0739abe).
- **Probe.** The report lists each removed span beside where it went, or the stated change it
  makes.

## Run each command an edited rule prescribes
`run-prescribed-commands`

- **Rule.** For each command an edited Rule or Probe prescribes, run it on the head from the root,
  each placeholder filled with a real value, then with the hardest it admits, such as a pattern
  holding a backslash, a name holding a bracket, a value holding a plus, a path given as a glob, a
  PR stacked on another, a directory an earlier run left full. Quote each exit status. One that
  cannot run here, like a macOS tool on Linux, is named not run, with the reason.
- **Why.** A rewritten Probe joined `-covermode=set` and `-race` in one `go test`, which refuses
  the pair, and passed review on reading (#1364). Three `git grep` Probes matched nothing, or were
  refused, on such values until they took `-F` (#1532).
- **Probe.** The report lists each prescribed command with its exit status.

## Pass a tool the flags a neighbouring rule passes it
`command-keeps-its-neighbours-flags`

- **Rule.** For each tool a command in an edited rule runs, list the other rules running it
  (`git grep -nF '<tool>' -- .claude/skills CLAUDE.md CONTRIBUTING.md`) and compare the flags.
  Each flag a neighbour passes for the same reading, such as one lifting a default cutoff, is
  passed too, or the edited rule says why not.
- **Why.** The Probe of
  `.claude/skills/morphic-measuring-performance/references/finding-cost.md#share-no-module-frame-holds`
  read a total off pprof's `-top` without the `-nodefraction=0` the rule before it passes. On a
  15.40 s profile of the OpenAPI end-to-end tests at c524701b5, it printed 9.57 s where those
  stacks held 10.14 s, as pprof dropped 137 nodes (#1667).
- **Probe.** The report lists each edited command beside each neighbour's call of its tool, and each
  flag only one of them passes, with its reason.

## Search the tracker for each construct a rule settles
`tracker-on-what-a-rule-settles`

- **Rule.** For each construct a change's rule decides, run
  `gh issue list --state all --limit 1000 --search "<construct>"`. Each open issue on it is closed
  by the change, cited as left open, or answered with why the change departs from it. A limitation
  the body states is searched on what it names
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#limitation-searched-on-what-it-names`).
- **Why.** A stream rule keyed on a content having no `Item` while #601 expects `Item` on each
  content classified as a stream, and an element rule left #630 unnamed. The PR named neither until
  a later revision, and no pass would have looked for them (#1167, #1170).
- **Probe.** The report lists each construct, the issues its search returned, and what the change
  does about each. Then `references/claims-and-process.md#tracker-on-what-cites-a-closing-issue`.

## Search the tracker for what cites each issue the PR closes
`tracker-on-what-cites-a-closing-issue`

- **Rule.** For each issue the PR closes, by keyword or sidebar link, and for the PR's own number,
  run `gh issue list --state all --limit 1000 --search "#<N> in:body"`, and settle each open hit as
  `references/claims-and-process.md#tracker-on-what-a-rule-settles` settles a construct's. Run it
  before judging the body's universal claims and before calling a defect beside the change
  unfiled.
- **Why.** A review of #1514, which closes #1086, called a defect beside it unfiled and passed the
  body's claim that a branch on a loop "is reported once", while #1566 held the defect and #1570
  contradicted the claim, both citing #1086; a search by construct found them only with their
  words (#1669).
- **Probe.** The report lists each number searched and each open hit, with what the change does
  about it.

## Check a choice resting on an open issue against the open PRs settling it
`open-issue-against-open-prs`

- **Rule.** When a change rests on an open issue, list the open PRs whose bodies close it, stacked
  ones included, as `closingIssuesReferences` covers PRs into `main` alone:
  `gh pr list --state open --limit 200 --search '"Closes #<N>" in:body' --json number,baseRefName`.
  Read each one's answer before clearing the choice; a disagreement is an `issue:` naming what each
  merge order owes.
- **Why.** #1327 kept `IRVersion` while renaming two `Unmodeled` keys, citing #429 as open, while
  #1351 closed #429 with the rule that such a rename moves the version, and the two merged without
  a conflict (#1327, #1351).
- **Probe.** The search above, for each issue the change cites as open.

## A gate sentence at a head with no CI run is unverified
`gate-claim-without-a-run`

- **Rule.** A body sentence saying a gate target passes, at a head with no CI run yet, as before the
  first push or a fix's push, is reported unverified with the commit it names, and read against
  CI's run once a push makes one. Its author runs it before writing it; a review does not rerun the
  gate (`references/rounds.md#ci-is-the-gate`), but may run a package's `go test` or one `make`
  target a sentence's claim turns on, as a probe.
- **Why.** `references/rounds.md#ci-is-the-gate` forbade the run that executing every body
  sentence on the final tree asked for, at a review planned before any push (#1596).
- **Probe.** `references/rounds.md#gate-run-by-workflow`'s command at the head, beside the commit
  each such sentence names.
