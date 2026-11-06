# Round readers

Who runs a round's angles, and how a delegated reader is briefed, housed and audited.
`references/rounds.md` says how a round runs and when the loop ends.

## Fan a review out only when asked, an angle to a reader
`fan-out-an-angle-to-a-reader`

- **Rule.** A review runs its angles in one reader by default, bar the fresh one of each round after
  the first (`references/readers.md#fresh-reader-for-the-second-round`) and the two of a split round
  (`references/readers.md#split-whole-reads-from-what-moved`). When asked to fan out, give each
  reader one angle and the code, in a directory of its own
  (`references/readers.md#reader-owns-its-directory`), with no conclusion of yours and, for
  Correctness, no PR body, so that independent reads stay independent. What returns goes through
  `references/rounds.md#disprove-each-finding` and
  `references/readers.md#audit-what-a-reader-cleared` before it counts.
- **Why.** A finding comes back in the same confident register whether or not it holds, as the
  review of #1162's twelve PRs counted false alarms in both its readers (#1162).
- **Probe.** Each reader's brief names one angle and its directory, and holds no finding of
  another's.

## Give each later round an unanchored reader
`fresh-reader-for-the-second-round`

- **Rule.** From the second round, one fresh agent runs the angles, or two in a split round
  (`references/readers.md#split-whole-reads-from-what-moved`), given the PR, this skill and its own
  directory holding the merged tree (`references/readers.md#reader-owns-its-directory`), nothing
  from an earlier round but names of cells to read. Its brief follows
  `references/readers.md#brief-a-reader-to-its-cells`. The main session disproves each returned
  finding and audits each clearance (`references/rounds.md#disprove-each-finding`,
  `references/readers.md#audit-what-a-reader-cleared`) before it counts. Where no agent can run, the
  main session rereads the merged diff before its own notes.
- **Why.** Fixes #1329's rounds proposed each passed a prototype of its own property and failed a
  sibling document's requirement, seen a round later (#1402); seven finders on #1016 ran 55 minutes
  before one returned (#1016).
- **Probe.** The reader's brief holds nothing from an earlier round but cell names, and names a
  budget and directory.

## Start a reader at effort `xhigh`, briefed to reach each of its cells
`brief-a-reader-to-its-cells`

- **Rule.** Start each delegated reader at effort `xhigh`, whatever the session's. Its brief names
  each cell, sets a tool-call budget summed from a share per cell, and asks for a running list of
  what it reproduced. The reader reaches each cell or states why it cannot here; budget left
  unspent is no reason, as each cell named unreached is one more round
  (`references/rounds.md#review-ends-with-nothing-left`).
- **Why.** #1532's seventh reader stopped at 51 of about 80 budgeted tool calls, naming five cells
  unreached, as its brief allowed. #1532's reader transcripts record the session's effort, so a
  session lowered for its mechanical work would lower what they catch (#1532, #1542).
- **Probe.** The reader's brief states the effort, each cell with its share and the budget; its
  report runs each cell or quotes why it could not.

## Brief a reader to batch its calls and bound its commands
`reader-batches-and-bounds`

- **Rule.** A delegated reader's brief asks it to send independent reads and commands in one turn,
  write bulky output to a file in its directory and read it in parts with `grep` or `head`, bound
  each command that can outlast a few minutes, and build each trial merge by
  `.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-happened`.
- **Why.** Every call re-reads the whole context, yet #1532's readers of rounds 2 to 7 batched at
  most 4 of their 40 to 67 calls, and a sixth-round command returned after 47 minutes despite a
  10-minute timeout (#1532, #1542).
- **Probe.** The reader's brief asks all four, and
  `jq -r 'select(.type=="assistant") | "\(.message.id) \([.message.content[]? | select(.type=="tool_use")] | length)"' <transcript> | awk '{n[$1]+=$2} END {for (i in n) {c++; b+=n[i]>1}; print c, b}'`
  prints the reader's calls, each a unique `message.id`, and how many batched.

## Give a delegated reader a directory of its own, and write nowhere else
`reader-owns-its-directory`

- **Rule.** A delegated reader's brief names a new directory only it writes, holding a detached
  worktree of the merged tree, and forbids writing elsewhere: the reviewer's files and harnesses,
  the main checkout and the repository's refs, bar the objects and worktrees it adds. What the
  reviewer reruns is checked across the hand-off by `references/readers.md#one-harness-directory`.
- **Why.** A reader vetting the Go blocks #1337's reviewer vetted rewrote the reviewer's harness
  `go.mod`, so its `go vet` failed, and would have checked the reader's tree had it resolved
  (#1529). A reader of #1522 fetched six refs into the repository (#1522).
- **Probe.** The reader's brief names the directory and the prohibition.

## Keep every harness under one directory, fingerprinted across each hand-off
`one-harness-directory`

- **Rule.** Keep every harness the reviewer will rerun, such as a probe or a script planting
  mutants, under one directory outside the tree and every reader's, so that one fingerprint covers
  them. Fingerprint it before each hand-off and after the hand-back, and rebuild what changed
  before rerunning any of it.
- **Why.** #1532's main session reran a harness after a reader's hand-back that it had not
  fingerprinted before the hand-off (#1532).
- **Probe.** `find <harness directory> -type f -exec cksum {} + | sort | cksum` prints one line
  before the hand-off and the same line after.

## Split a scoped round carrying whole reads between two readers
`split-whole-reads-from-what-moved`

- **Rule.** A round reading what moved (`references/rounds.md#later-rounds-read-what-moved`) with
  whole reads among its cells goes to two parallel readers, each in its own directory. One takes
  what moved and the PR body, checking the body's claims against the diff. The other takes the
  whole reads: the closing issues with their comments, each issue's Direction, tracker searches and
  evidence outside the repository, looking up each site an ask points to but reviewing no diff.
  The main session merges both reports and disproves each finding. A round of the diff and body
  alone keeps one reader, and neither half splits by file
  (`references/readers.md#no-split-by-file`).
- **Why.** #1532's seventh reader left five cells unreached, the closing issues read whole and an
  issue's Direction among them (#1532).
- **Probe.** Each brief of a split round names its half and its directory.

## Never split a round's reading by file
`no-split-by-file`

- **Rule.** No round divides the files it reads between readers: a split round's diff half and each
  fan-out reader hold all of them, as a defect can sit between two files.
- **Why.** On #1532, a reader holding both files found a departure disagreeing with the sweep rule
  it cites (#1532).
- **Probe.** No two briefs of one round each name part of the files it reads.

## Read each cell a reader settled against what its rule asks
`audit-what-a-reader-cleared`

- **Rule.** Before a delegated reader's round counts, read each cell its report settles against
  the rule that cell answers to, and reopen one settled on less than it asks: a mutant judged
  equivalent on this tree's inputs alone
  (`.claude/skills/morphic-verifying-changes/references/mutation.md#equivalent-means-every-input`),
  an issue or file read in part, a class sampled
  (`references/claims-and-process.md#sweep-each-claim-class`). The next round reads it.
- **Why.** On #1522, round 3 judged a mutant dropping `normpath` equivalent because no path on the
  tree held a `.` or `..` segment, and round 4 cleared #1266's asks on lines cut at about 450
  characters; the round after each found a defect there (#1522).
- **Probe.** Each round's report lists the cells it reopened, each with what its clearance lacked.
