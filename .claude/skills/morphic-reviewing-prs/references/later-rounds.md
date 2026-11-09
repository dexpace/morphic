# Later rounds

What a round reads from the third on, when it narrows to the fixes, and when a miss sends it over
the whole change again. `references/rounds.md` says how a round runs and when the loop ends.

## Read what moved, from the third round on
`later-rounds-read-what-moved`

- **Rule.** From the third round, short of `references/later-rounds.md#nit-round-checks-its-fixes`,
  readers run every angle over each file both `git diff --name-only <merged> HEAD` and
  `git diff --name-only origin/<base>...<head>` list, `<merged>` the round before's commit and
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
  a finding, leaving unchanged what production code, a script, rule or normative text does, and no
  merge touched the change's files. Its brief records that, or its report's `narrowed:` line where
  no agent runs. An `issue:` sends the next round to
  `references/later-rounds.md#later-rounds-read-what-moved`; a wrong record, this one.
- **Why.** Whatever their fixes, such rounds took 34% of reviewer CPU for 17% of `issue:` findings
  (#1611); a `question:` beside a `nit:` read either way (#1617).
- **Probe.** `git diff --name-only <main the previous report records> "$main"`, and the same for
  the base, share no file with `git diff --name-only origin/<base>...<head>`.

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
  sends the next round over the whole change only when it is a wrong result in code or a test unable
  to catch a real failure. Any other, such as a false sentence in a body or comment, is fixed and
  the next round reads what moved.
- **Why.** Only a whole-change round met an ask in #1506's comments round 2 reported reading
  (#1522), while a body's clause sent a PR over its whole change for plants its reader called
  contrived (#1563).
- **Probe.** For a code or test finding, the main session says from the earlier reports and
  `git log` whether its cell was filled and unchanged; a body sentence needs no date, as it widens
  nothing (#1588).
