# Later rounds

What a round reads from the third on, when it narrows to the fixes, and when a miss sends it over
the whole change again, each from the reports
`references/later-rounds.md#keep-each-rounds-report` keeps. `references/rounds.md` says how a
round runs and when the loop ends.

## Keep each round's report where the next round reads it
`keep-each-rounds-report`

- **Rule.** Each round appends its report, first line included, to
  `"$(git rev-parse --path-format=absolute --git-common-dir)/morphic-review/<N>.md"`, which every
  worktree of the clone shares and no commit carries. A later round's main session reads it before
  choosing what the round reads, its reader getting only what
  `references/readers.md#fresh-reader-for-the-second-round` allows. A round finding no report there,
  as in another clone, reads the whole change as the first two rounds do, and its report says why.
- **Why.** A round of #1647 at 894da789c, after rounds whose fixes were on the branch, found no
  comment, review, body line, ref or file holding their reports, so neither `<merged>` nor whether
  its `issue:` sat in a filled cell could be told (#1649).
- **Probe.** The file's last `Round` line names the round before, its `merged` commit one
  `git cat-file -t` finds.

## Read what moved, from the third round on
`later-rounds-read-what-moved`

- **Rule.** From the third round, short of `references/later-rounds.md#nit-round-checks-its-fixes`,
  readers run every angle over each file both `git diff --name-only <merged> HEAD` and
  `git diff --name-only "$b"...<head>` list, `<merged>` the round before's commit and
  `<head>` the PR's unmerged head, and each line citing a rule changed there
  (`git grep -n '#<key>'`); over each fix since, wording included, read by
  `references/rounds.md#deep-and-broad`; over each unreached or reopened cell, named without
  findings; and over the PR body, not the whole change
  (`references/later-rounds.md#missed-cell-widens-the-round`).
- **Why.** #1522's later rounds reread the whole change, though 12 of its 13 findings in rounds 3
  to 5 sat in what moved, the body, a refused clearance or an unlisted member (#1522, #1530); from
  the merged `HEAD`, a stacked PR's list held `main`'s files (#1614).
- **Probe.** The reader's brief lists those cells; the report runs or settles each.

## Narrow the round after a nit-only round to its fixes
`nit-round-checks-its-fixes`

- **Rule.** A nit-only round finds a `nit:` and no `issue:` or `question:`, or nothing but an
  unreached or reopened cell. From the third round, the round after one reads only what
  `references/later-rounds.md#narrowed-round-reads-the-fixes` lists, where each commit since fixes
  a finding, leaving unchanged what production code, a script, rule or normative text does
  (`references/later-rounds.md#narrowing-reads-each-commit`), and no merge touched the change's
  files. Its brief records that, or its report's `narrowed:` line where no agent runs. An `issue:`
  sends the next round to `references/later-rounds.md#later-rounds-read-what-moved`; a wrong
  record, this one.
- **Why.** Whatever their fixes, such rounds took 34% of reviewer CPU for 17% of `issue:` findings
  (#1611); a `question:` beside a `nit:` read either way (#1617).
- **Probe.** `git diff --name-only <main the previous report records> "$main"`, and the base's,
  share no file with `git diff --name-only "$b"...<head>`.

## Narrow a round only on a record of each commit since
`narrowing-reads-each-commit`

- **Rule.** Before narrowing by `references/later-rounds.md#nit-round-checks-its-fixes`, read each
  commit since in `git log --first-parent --no-merges -p <merged>..<head>`, as that rule's own
  condition holds a merge, and record beside it the finding it fixes and why it changes no
  command, condition, output or claim of what it touches: a rewording or a refill alone. A commit
  with no such record sends the round to `references/later-rounds.md#later-rounds-read-what-moved`.
- **Why.** Of eight recorded rounds from the third on after a nit-only round, one met the
  conditions, and its `issue:` sat in its fix. The one round read narrowed followed a commit
  changing rules' commands, and the next round, read whole, found an `issue:` outside its fixes
  (#1618).
- **Probe.** The record names every commit
  `git log --first-parent --no-merges --format=%h <merged>..<head>` lists, each with its finding
  and reason.

## A narrowed round reads the fixes, the body and the open cells
`narrowed-round-reads-the-fixes`

- **Rule.** A round `references/later-rounds.md#nit-round-checks-its-fixes` narrows runs no angle:
  it reads CI's run, each fix since by `references/rounds.md#deep-and-broad`, wording included, and
  each body sentence against the fixes, and runs each open cell, planting only for a fix's test. A
  merge adds `references/rounds.md#recheck-against-final-tree`.
- **Why.** Reading each fix only against the tree gave it less than the round it replaced did, and
  "each body sentence against them" named nothing for "them" (#1617).
- **Probe.** The report gives each fix's callers, callees and consumers read, each body sentence
  checked and each open cell's run.

## Widen a round only for a wrong result a filled cell missed
`missed-cell-widens-the-round`

- **Rule.** A defect in a cell an earlier round filled and no audit reopened, unchanged since,
  sends the next round over the whole change only when it is a wrong result of code, a script, a
  rule or normative text, or a test unable to catch a real failure. Any other, such as a false
  sentence in a body or comment, is fixed and the next round reads what moved.
- **Why.** Only a whole-change round met an ask in #1506's comments round 2 reported reading
  (#1522), while a body's clause sent a PR over its whole change for plants its reader called
  contrived (#1563).
- **Probe.** For such a finding, the main session says from earlier reports and `git log` whether
  its cell was filled and unchanged; a body sentence needs no date (#1588).
