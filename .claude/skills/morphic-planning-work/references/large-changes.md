# Large changes

Rules for a change that closes several issues or builds a whole feature, in three runs: those for
any such change, from `references/large-changes.md#measure-the-family-first`; those for one built in
slices or parallel parts, from `references/large-changes.md#review-each-slice-as-it-lands`; and
those for several pull requests at once, from
`references/large-changes.md#one-owner-per-shared-fact`. A rule of an earlier run holds in the later
ones.

## Measure the family before designing its fixes
`measure-the-family-first`

- **Rule.** Where the issues cluster in one package or mechanism, measure the family whole before
  designing a fix for any one of them: a generated differential over every spelling and position of
  one meaning, or a count of the code reading the shared input. Where one structure produces them,
  design the change to that structure.
- **Why.** A design for 41 schema-composition issues, in six slices challenged before any was
  built, was overtaken two days later by a measurement of the whole family: of 3,825 keyword ×
  target × position × version groups, 2,047 lowered differently across spellings of one meaning,
  and the changes after it closed issues of the family by that model (#1918, #1991, #1996).
- **Probe.** The design quotes the measurement's command and its count at a named revision.

## Read every consumer against every mechanism before recommending a design
`sweep-consumers-before-recommending`

- **Rule.** Before recommending one design among several, list every consumer the design reaches
  against every mechanism it touches, read the document each cell names, search closed issues for a
  recorded decision on each mechanism, and read `docs/prior-art.md` and
  `docs/reference-learnings.md` for a verdict on the idea. A cell left unread is named beside the
  recommendation.
- **Why.** A recommendation for the emitter foundation, made on a third of `docs/emitters`, changed
  once every consumer was read: one of its claims was false, one of its pieces broke a design rule,
  and a decision the tracker had recorded went unseen (#2214).
- **Probe.** The recommendation lists its consumer × mechanism cells, each read or named unread.

## Write a design when its change is next, against a recorded base
`design-against-its-base`

- **Rule.** Write a design when its change is next to be built, and record the commit it was
  checked against. Before building, re-check each finding whose files `main` has changed since. A
  design written earlier, on a base still moving, is input to the next design, never its truth.
- **Why.** Asked to move faster, the work before #1918 started every remaining design at once on
  bases still moving; twenty were never built, and #1918 had to read them as "input, never truth"
  (#1918).
- **Probe.** The design names its commit, and each finding resting on a file that
  `git diff --stat <recorded>..origin/main -- <the design's files>` lists was re-checked after it.

## Make each decision in the design, before the build
`decisions-made-before-the-build`

- **Rule.** A design states each decision as made. A builder who finds one wrong on real input
  stops that part and reports the input, the observed and the expected result, and the options;
  the design's author decides, and the design records the answer before the part is built on. A
  builder changes no decision inside its build.
- **Why.** Nine builds under #1918 found the design's expectation wrong on real input, and what a
  builder should do then went through three versions during the work; builders of #2098 then
  changed or overturned decisions inside their own builds (#1918, #2098).
- **Probe.** Each departure from the design names its report and the decision recorded for it.

## Hand work on by pointing at the design, with every count a cap
`handoff-points-at-the-design`

- **Rule.** A task handed to another session cites the design's section rather than restating it,
  and states every count as a cap, never a floor. A returned "Breaking: none" or "not checked" is a
  claim the design's author probes before relying on it.
- **Why.** A test author's task contradicted the design it restated, a verifier asked for "at least
  sixty" plants made 343 and filled a 49 GB build cache, and two builds of #1940 returned Breaking
  sections a later pass had to correct (#1918, #1940).
- **Probe.** Each number in the task reads as a maximum, and each section it builds from is cited,
  not copied.

## Change a decision in every record that states it, at once
`changed-decision-updates-every-record`

- **Rule.** When a decision changes, update at once every record that states it: the design, the
  tracker's body, each task statement handed on, and the pull request's body, read by
  `.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#reread-prose-after-a-redesign`.
- **Why.** A decision moved #988 into #1937, but the design's header still said it stayed open, so a
  reviewer turned its closing line into `Related: #988` and the next round restored it (#1937).
- **Probe.** A grep of each record for the old wording prints nothing: the design's file,
  `gh issue view <tracker> --json body`, and `gh pr view <N> --json body`.

## Give each issue found an owner and a number
`found-issue-goes-to-its-mechanism`

- **Rule.** A report names each defect it saw outside its task with an issue number or a written
  reason, and the issue goes to the change that owns its mechanism, by the tracker's routing table
  where it has one: the change that found it only when it owns that mechanism. Three new issues in
  one merged change's own mechanism mean its design missed the mechanism, which
  `references/large-changes.md#measure-the-family-first` then measures.
- **Why.** Before #1918, issues found were left for later; at least six reports during it left a
  defect unfiled; and a reviewer of #1940 named a gap in the review skill and filed nothing
  (#1918, #1940).
- **Probe.** Each report lists every defect it saw outside its task, each with an issue number or a
  reason.

## Record a deferred finding by symbol, and clear it before its files move
`deferred-finding-by-symbol`

- **Rule.** Record each finding deferred past a merge by its symbol and the code it quotes, never by
  a line number, and land its cleanup before another change touches its files.
- **Why.** The nits deferred from the reviews of #1937, #1938 and #1940 were recorded by file and
  line; of the 23 files they cite, 20 have changed since #1938 merged and two are gone, so each
  nit has to be found again (#1944).
- **Probe.** `git diff --name-only <recorded>..origin/main -- <the files the findings cite>` prints
  nothing before the cleanup lands.

## Keep a tracker's state in its body
`tracker-state-in-its-body`

- **Rule.** A tracking issue's current plan and state live in its body: update the body in place,
  from a fetched copy, by `gh issue edit <N> --body-file <file>`, never by a comment, which leaves
  the reader to assemble the state from history. A comment giving one issue's closing evidence is
  no tracker update.
- **Why.** An update of #1918's progress was begun as a comment and moved into the body at the
  maintainer's request (#1918).
- **Probe.** `gh issue view <N> --json body` holds the latest state, and
  `gh issue view <N> --json comments` adds none.

## Review each slice as it lands, the foundation first
`review-each-slice-as-it-lands`

- **Rule.** Hold a change built in slices to one mechanism family. Review each slice when it is
  built, by `morphic-reviewing-prs` over that slice's diff, and the foundation slice before
  anything is built on it. The pull request's own review still reads the whole change.
- **Why.** #1940 was built in nine slices and only the first had a reviewer before the whole review,
  whose first round found ten defects in code two verifiers and two structure passes had passed
  (#1918, #1940).
- **Probe.** Each slice's commits, `git log --first-parent --format='%h %s' <base>..<head>`, have a
  review report naming them.

## Settle in the design what parallel parts share, and diff it at the join
`parts-settle-what-they-share`

- **Rule.** Where parts are built in parallel, the design names one owner for each name, diagnostic
  code, option and file two parts touch, and each join diffs the parts at those meeting points
  before merging them.
- **Why.** Two side branches of #1937 each added the same diagnostic code, and #1938's join met a
  test both sides had rewritten (#1937, #1938).
- **Probe.** `git diff --name-only <base>...<part>` for two parts shares only files the design names
  with an owner, and a grep of each part's added lines for the other's new codes and names prints
  only the owner's.

## Take one version row for parallel parts, at the join
`one-version-row-at-the-join`

- **Rule.** Parts built in parallel toward one pull request share the one `IRVersion` after their
  base's, and the join keeps one `generations` row and one history entry saying what every part
  changed, by `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#version-collisions`.
- **Why.** #1937's parts each held a row for 0.68.0, which the join had to make one (#1937).
- **Probe.** After the join, `grep -o 'version: "[0-9.]*"' ir/irversion_test.go | sort | uniq -d`
  prints only the versions repeated by design.

## Give each fact several pull requests share one owner
`one-owner-per-shared-fact`

- **Rule.** Before pull requests in neighbouring areas start, the plan names one owner for each
  field, diagnostic code, option and file two of them read or write; the others build on the
  owner's branch or wait for its merge, and none copies the owner's file.
- **Why.** Pull requests in neighbouring areas settled shared questions apart, each defect silent
  until both were built, and #1809 and #1819 each copied a file from #1813 byte for byte (#1918,
  #1809).
- **Probe.** For each pair of the plan's open pull requests, `git diff --name-only origin/main...<head>`
  shares only files the plan names with an owner.

## Review together the pull requests that touch one fact
`review-together-what-touches-one-fact`

- **Rule.** Pull requests that read or write one fact are reviewed on their trial merge before
  either lands, or become one pull request: reviewed apart, a defect only their merged build shows
  passes both.
- **Why.** A review of eleven open pull requests found defects only their merged build showed, which
  no test catches if they merge in another order (#1809, #1810, #1813), and the merge train found
  more (#1840 with #1819, #1846 with #1833).
- **Probe.** `git merge-tree --write-tree <head> <other head>` exits 0, and the review's angles run
  on a worktree of that merge.

## Assign no version ahead of the merge
`no-version-ahead-of-the-merge`

- **Rule.** A plan of several pull requests assigns none of them an `IRVersion`: each branch takes
  the version after its base's, and the one merging second moves, by
  `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#version-collisions`, so a change
  of merge order renumbers nothing.
- **Why.** Versions assigned down a merge queue were renumbered at each change of order: #1809 held
  0.47.0 when first reviewed and merged at 0.60.0, and a script was written only to resolve the
  conflicts (#1809, #1918).
- **Probe.** `grep -nE '0\.[0-9]+\.[0-9]+' <the plan>` prints no version assigned to an open pull
  request.
