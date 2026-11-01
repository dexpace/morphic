# Review probes

Probes that found what earlier review rounds missed, and the rules that let the first round find
everything. Each probe runs through `morphic-verifying-changes`: compile the input, plant the
defect, keep a `main` control.

## Probe the code before the claims
`code-before-claims`

- **Rule.** In the first round, before any corpus run: diff old against new semantics at every named
  bound, re-plant every mutation the PR lists and plant your own at each new branch, call or
  argument it skips, ask which input class the corpus cannot contain (deep nesting, oversized
  documents, aliasing), and grep for mutable globals and layering breaks.
- **Why.** Three rounds that took their agenda from the PR body found only body defects. A fourth
  that read the new file found two code defects in it (#340). Each mutant #1153 listed reddened a
  test, while dropping an argument its new code passed reddened none.

## Check every class of claim in full, in the first round
`sweep-each-claim-class`

- **Rule.** Before the first finding, list the change's classes of claim and check every member:
  each name against the tree, citation against the item it cites, statement about code on `main`,
  statement about a library against its `docs/parser-library.md` entry, quoted command run from
  the root, script in each context it runs in (a subdirectory, each platform branch), body sentence
  on the final tree, each construct a rule settles
  (`references/review-probes.md#tracker-on-what-a-rule-settles`) and each edited rule
  (`references/review-probes.md#edited-rule-against-its-neighbours`). A class checked by sample is
  reported unreviewed, never clean.
- **Why.** A review of #1143 took nine rounds, each sampling a class: citations were read for 58 of
  203 rules before all 383, and a misattribution sat in the rest.
- **Probe.** The report gives each class and the members checked; fewer than its size is a round
  still open.

## Read an edited rule against the rules governing the same act
`edited-rule-against-its-neighbours`

- **Rule.** For a change to `.claude/skills` or `CLAUDE.md`, name the acts each edited rule governs
  (commit, push, merge, review, plant a mutant), grep both for the other rules governing each act,
  and read each pair for an instruction one gives and the other forbids, keeping the best of both
  where they do. A script finds a pair but not a conflict, so this stays a reader's.
- **Why.** Two rules one PR edited each held against the tree, while one gated a push on reading
  what a checklist printed and the other on an exit status in the same command (#1153, #1160).
- **Probe.** The report lists each edited rule, its acts and the rules read against it; a rule with
  none is a round still open.

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

## Fan a review out only when asked, a pass to a reader
`fan-out-a-pass-to-a-reader`

- **Rule.** A review runs its passes in one reader by default. When asked to fan out, give each
  reader one pass and the code, with no conclusion of yours and, for pass 2, no PR body, so that
  independent reads stay independent. What returns goes through
  `references/review-probes.md#disprove-each-finding` before it counts.
- **Why.** A finding comes back in the same confident register whether or not it holds, as the
  review of #1162's twelve PRs counted false alarms in both its readers (#1162).
- **Probe.** Each reader's brief names one pass and holds no finding of another's.

## Read the reviews already standing once the passes are done
`read-standing-reviews`

- **Rule.** After the passes, not before, as a list read first sets the agenda as a PR body does,
  read the PR's reviews and comments with the commit each was written on. Check each against the
  head: one fixed is not reported again, one open is carried into the report, and one its author
  declined is answered, not repeated.
- **Why.** A review requested changes while later work went on as though the PR only awaited an
  approval, seen only when a status check happened to print it (#1169).
- **Probe.** `gh api repos/dexpace/morphic/pulls/<N>/reviews --jq '.[] | [.user.login, .state, .commit_id] | @tsv'`,
  and the same path ending `comments`.

## Post a finding so that acting on it is safe
`posted-findings`

- **Rule.** When asked to post, write one comment per defect, naming the rule it breaks, and link
  code by full SHA, as a line on a moving branch drifts. Offer a committable suggestion only if it
  fixes the defect alone, passes the checks it touches and breaks no rule governing the same act:
  otherwise describe the fix.
- **Why.** A suggested Probe said to delete a hit in a commit body, which a pushed branch cannot do,
  and a suggested regex missed a generic no test row held (#1169).
- **Probe.** Apply each suggestion in a throwaway worktree and run the checks it touches.

## Search the tracker for each construct a rule settles
`tracker-on-what-a-rule-settles`

- **Rule.** For each construct a change's rule decides, run
  `gh issue list --state all --limit 1000 --search "<construct>"`. Each open issue on it is closed
  by the change, cited as left open, or answered with why the change departs from it.
- **Why.** A stream rule keyed on a content having no `Item` while #601 expects `Item` on each
  content classified as a stream, and an element rule left #630 unnamed. The PR named neither until
  a later revision, and no pass would have looked for them (#1167, #1170).
- **Probe.** The report lists each construct, the issues its search returned, and what the change
  does about each.

## Give a normative document's rules something that can disagree with them
`review-design-rules`

- **Rule.** A document describing code that does not exist has no output to compare. So read each
  rule it states against the cases of every other, and against a format or protocol it was not
  written for; run the rules as a program over compiled specs, a planted defect showing the run
  reacts; check each premise
  (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#measure-the-premise`); and run
  each sample as printed under the document's own checks.
- **Why.** Names and citations were clean while a fallback overrode a gRPC stream's framing and a
  union of event shapes was misread, and `go vet` refused a sample that compiled (#1167, #1168).
- **Probe.** The report lists each rule with what could have disagreed with it; a rule with nothing
  is unreviewed.

## A fix the review makes is reviewed before it is pushed
`review-own-fixes`

- **Rule.** When a review fixes what it finds, give each fix's own diff the passes, sweep its
  mechanism, and reread what it adds as unreviewed
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#reread-prose-after-a-redesign`).
  Push a round's fixes once, behind the push checklist, then rerun the setup.
- **Why.** Later rounds of #1143's review kept finding defects its own fixes had added: a PR-body
  example the scripts listed as a closing reference, a command that failed from the repository
  root, and a sentence saying the lint ran on every branch.
- **Probe.** Before the push, read `git diff <round's base>..HEAD` with the passes, and run the
  push checklist on it.

## Review the tree as it will merge
`recheck-against-final-tree`

- **Rule.** After review fixes land or `main` or the base is merged in, review the branch as it
  will merge:
  rerun the probes the body and comments cite and every sweep the branch relies on over the merged
  tree, re-diff each change `main` made to files the branch touches, and look for helpers `main`
  renamed, features on both sides that interact, and normative text `main` moved.
- **Why.** Merges dropped `main`'s wider change in a conflict, left comments asserting behaviour
  `main` had changed, and left PR bodies stating facts a later commit falsified (#837, #382).
- **Probe.** `git diff <merge-base>..origin/main -- <files the branch touches>` and confirm each
  hunk survives; rerun each command the body quotes and diff its output.

## Never accept "as on main" inside the change's mechanism
`better-than-main`

- **Rule.** Where head and `main` differ, decide which is right on the specification's own terms. If
  `main` is right, fix the head. If neither is, build the right rule at its owner. A better
  behaviour that changes output is stated in the PR body, whose Breaking section goes with a `!` in
  its title (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#exported-api-change-needs-bang`).
  A defect `main` shares is fixed or filed, never a reason to stop.
- **Why.** A parity round copied a library defect and kept an order dependence "as on main" (#1016).
  Merged PRs tie the section to the `!`, so a `fix` changing an exit code states it in its Summary
  (#1181, #1186).

## A PR that moves readers off the library inherits what the library skipped
`moved-reader-parity`

- **Rule.** Diff `resolutionBaseline` in `internal/archtest` between `main` and head: each package
  that loses a library call is a moved reader. Probe each one with inputs the old reader never
  reached (references inside other documents, components behind chains, aliases, escapes), and
  account for every diagnostic `main` reported that head drops.
- **Why.** Readers moved off the library in #1016 followed `$ref`s written inside another
  document, which the library never resolved, and lent facts from them, dangling encoding keys
  among them; and they read a component written as a string by its YAML shape, reporting a `$ref`
  to it unresolved while the IR held its type. Every corpus missed both.

## A new route inherits the old route's open bugs
`new-route-open-bugs`

- **Rule.** When a change makes X "work as Y does", rewrite each open repro of Y
  (`gh issue list --limit 1000 --search "<mechanism>"`) into X's spelling and run both builds. A
  new crash path is a regression even when its root is an open issue. Check merged and open PR
  bodies for handoffs: `gh pr list --state all --limit 1000 --search "#N in:body"`.
- **Why.** A discriminator mapping resolved "as a `$ref` would" made a `$ref`-only crash reachable
  (#564, #749), and another reached an unbounded library recursion (#763).

## A refusal test can enshrine a regression
`refusal-regression`

- **Rule.** Run every spelling a change newly refuses through a `main` build. Exit 0 becoming exit 1
  is a regression unless the PR says it is deliberate.
- **Why.** An over-broad refusal was pinned by the PR's own unit test and passed several rounds
  (#763).

## A moved function's new tests pin what it does, not what the rule says
`moved-code-vs-rule`

- **Rule.** For each output a moved function's new tests pin, compare it with the governing rule in
  `docs/compilers/openapi.md`, and probe it on `main` and on the head.
- **Why.** A characterization test pinned a warning at the carrier against a rule that places it at
  the value's position (#1100).

## Revert each rewritten site to find redundant guards
`redundant-guard`

- **Rule.** When a PR adds one predicate at many sites, revert each site to `main`'s spelling on its
  own. For any guard a revert or a mutant survives, ask whether removing it changes an outcome, the
  reader it falls through to included. Where one changes, even if only in the function's own result
  before a later stage masks it, keep the guard and pin it directly. Where none can, a second reader
  already makes the check: delete the guard, and let a test reach the branch, which fails loudly if
  that reader changes.
- **Why.** Five survivors in #1066 were covered by a choke point; one in #1140 was the function's
  own grammar. A nil check before a nil-safe library getter had nothing to pin, while the same check
  beside it kept a test from panicking (#742, #1189).

## Moved code carries stale comments
`moved-comments`

- **Rule.** For a file split or a cross-package move, read each moved declaration's comment against
  its callers. Grep the whole repository for every moved name, and for the new package's name used
  as an ordinary word.
- **Why.** Byte-identical moves shipped false comments: 8 in #1064, 4 in #1073, 22 in #1099. A
  cross-package move broke comments in files it never touched (#1104).

## A new check, walk or golden may already exist
`mechanism-exists`

- **Rule.** Before judging a PR that adds a check, a walk or a golden, grep the concept's vocabulary
  across `*_test.go` and `testdata/` for one that already answers it.
- **Why.** A new census duplicated an unwitnessed-facts golden CI already held (#1005).

## A cheaper replacement must catch what the tool honours
`replacement-gate`

- **Rule.** Before accepting a grep or script in place of a linter, plant every spelling the tool
  honours, and require the new check to refuse a superset of what the old gate refused.
- **Why.** A `//nolint` grep missed spellings golangci-lint honours (#836).

## After a merge of main, sweep with the base's grammar
`ported-check-grammar`

- **Rule.** Removing a mechanism because the change makes it unnecessary holds only for the cases
  the change touched. After merging `main`, run the base's current generators over many seeds before
  the merge commit.
- **Why.** Evidence gathered on an older generator missed a stack overflow the base's grammar wrote
  (#566).

## Probe the near miss and the equivalence
`near-miss`

- **Rule.** For each predicate that decides whether a new path applies, compile the issue's example
  plus one keyword it does not expect (`description`, an `x-` extension). For "A lowers as B",
  compile both spellings and diff them with IDs and pointers projected away, with `main` and a
  planted defect as controls.
- **Why.** An all-or-nothing guard falls back to the reported defect on the near miss while the
  surrounding code handles the keyword partly, as a cycle guard skipped for any source that merely
  wrote `$id` did (#763).

## Price the common path
`price-common-path`

- **Rule.** When a change adds work to a path every input takes, compare instructions retired on
  base and head
  (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#instructions-not-wall-clock`)
  for a large input of the old shape and for the golden spec, which `CLAUDE.md`'s "Priority"
  section shows how to fetch.
- **Why.** Allocation counts miss CPU work, and load skews wall-clock time (#1133).

## Fuzz a rewritten reader against the one it replaces
`refactor-differential-fuzz`

- **Rule.** For a refactor that rewrites readers (parsers, splitters, containment tests), copy the
  old functions from `origin/main` beside the new ones in a scratch module and fuzz that each pair
  agrees. Turn intended divergences into narrow, named exclusions. Afterwards diff the shipped
  function bodies against the copies the harness ran.
- **Why.** A rewritten reader parts from the old one on inputs no table holds, since a table
  written beside the rewrite shares its blind spots; on #510 the fuzzed pairs covered tens of
  millions of inputs per reader in a minute and made every intended divergence explicit.

## Run a query command as a differential
`query-differential`

- **Rule.** For a change to a query such as `--explain`, run it on both builds at every coordinate
  each compilable spec interns, at each one's parent and at the root, and classify every difference
  against the ones the change intends.
- **Why.** A unit table covers only the cases its author thought of (#551).

## Check the failure form of every record a change re-attributes
`reattributed-failure-form`

- **Rule.** When a change re-attributes records (whose source a provenance names), also check the
  diagnostic reported in place of each record when it fails. Give a table's fallback a value
  distinct from 0 and from every real index.
- **Why.** A combined entry was credited to its keywords while the error reported in its place kept
  the old attribution (#552); with a fallback of 0, two planted mutants survived.

## Re-root a generated sweep at every root the mechanism reaches
`reroot-generated-sweep`

- **Rule.** A generator that places every target under one root (`/components/schemas`) proves
  nothing about the others: rerun it under each root the mechanism reaches, such as a response body
  or a parameter.
- **Why.** 0 misnamed variants under components, 61 of 164 under a response body (#553). When two
  functions must agree on a name and share no code, they drift outside the swept domain.

## A test that two mechanisms each keep green proves neither
`two-mechanisms-one-test`

- **Rule.** When a test survives removing one mechanism and separately survives removing another,
  plant the pair, and say in the test's doc which test holds each mechanism alone.
- **Why.** A components test survived removing the recorded declaration hint, and separately
  survived restoring the old target-first guess; only both removed turned it red (#553).
