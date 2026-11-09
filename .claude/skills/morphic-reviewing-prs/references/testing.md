# Testing

The angle that attacks each function the change adds or alters with inputs meant to break it, and
each test with defects it should catch. It loads `morphic-verifying-changes`. A scratch test or a
compiled spec in the worktree is a probe; a package's suite is CI's
(`references/rounds.md#ci-is-the-gate`).

1. Cross each changed function with the input classes it can meet
   (`references/testing.md#function-by-input-class`), a path with each spelling of the working
   directory (`references/testing.md#path-by-working-directory-spelling`), at the edges the code
   transforms
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#inputs-at-the-edges-the-code-transforms`)
   and at each bound
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#boundary-cases-at-the-boundary`).
2. Plant defects past the PR's list (`references/testing.md#plant-beyond-the-listed-mutants`), by
   `.claude/skills/morphic-verifying-changes/references/mutation.md`: where only the callers a check
   leaves out reach
   (`.claude/skills/morphic-verifying-changes/references/mutation.md#plant-past-the-included-callers`),
   on a run owning its cache
   (`.claude/skills/morphic-verifying-changes/references/mutation.md#mutant-run-owns-its-cache`),
   each kill read against its plant
   (`.claude/skills/morphic-verifying-changes/references/mutation.md#kill-reaches-the-plant`),
   and a Python plant run without stale bytecode
   (`.claude/skills/morphic-verifying-changes/references/mutation.md#python-plant-without-bytecode`).
3. Hold each test the change adds or edits, and each whose claim it widens
   (`references/testing.md#claim-the-change-widens`), to what its name claims
   (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#assertion-targets-what-the-name-names`),
   to the stage it must reach
   (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#fixture-still-reaches-its-stage`)
   and to failing when the code is wrong
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#assertion-must-be-able-to-fail`),
   an absence read on its field
   (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#absence-on-the-field`),
   a time limit's outcome raised through a seam
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#limit-outcome-through-a-seam`)
   and each table row setting up the state it can change
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#row-owns-its-state`).
4. For a check, lint or archtest rule or a gate target the change adds, plant what it must catch and
   what it must pass, by `.claude/skills/morphic-verifying-changes/references/checkers.md`, among
   them `.claude/skills/morphic-verifying-changes/references/checkers.md#gate-target-draws-a-plant`,
   `.claude/skills/morphic-verifying-changes/references/checkers.md#lint-probe-lists-every-finding`
   and `.claude/skills/morphic-verifying-changes/references/checkers.md#analysis-input-is-fixed`;
   for one replacing another, take `references/testing.md#replacement-gate`.
5. For a reader, parser or grammar the change rewrites, fuzz it against the one it replaces
   (`references/correctness.md#refactor-differential-fuzz`).
6. Where the change adds one predicate at many sites, revert each site alone
   (`references/testing.md#redundant-guard`), and where a test stays green with either of two
   mechanisms removed, remove both (`references/testing.md#two-mechanisms-one-test`).
7. Tell a failure that is the machine's, a full disk, a removed cache or load, from the change's
   (`.claude/skills/morphic-verifying-changes/references/toolchain.md#full-disk-reads-as-a-failure`,
   `.claude/skills/morphic-verifying-changes/references/toolchain.md#gate-failure-under-load`,
   `.claude/skills/morphic-verifying-changes/references/toolchain.md#stale-linter-reads-as-a-failure`),
   and instrument a test's `go` children as
   `.claude/skills/morphic-verifying-changes/references/toolchain.md#test-go-children-ignore-path`
   says.

When the review fixes, each gap becomes a test seen to fail on its planted defect. The matrix is the
function-by-class cells and the mutants planted, killed and judged equivalent, each with its reason.

## Attack each changed function with every input class it can meet
`function-by-input-class`

- **Rule.** Cross each function the change adds or alters with the input classes it can meet:
  empty, one and many; each limit at N-1, N and N+1; duplicates and the opposite order; deep,
  cyclic, wide and aliased shapes; escapes and invalid UTF-8; each callee's error; a cancelled
  context; for a path, `references/testing.md#path-by-working-directory-spelling`. Run each cell
  as a probe, or settle it by a reason the report names, such as a type ruling the class out. A
  cell that misbehaves is a correctness finding; one the code takes its own path for needs a test
  failing when it breaks.
- **Why.** Every corpus missed two defects in #1016's moved readers, each shown by one minimal
  input, and rounds sampling cases found what the first should have (#1402).
- **Probe.** Count the cells, and those a reason settled.

## Plant defects past the ones a PR lists
`plant-beyond-the-listed-mutants`

- **Rule.** Re-plant every mutation the PR lists, from its literal edits
  (`.claude/skills/morphic-verifying-changes/references/claims.md#cite-a-plant-by-its-edit`) or,
  where it gives only a count and class, by
  `.claude/skills/morphic-verifying-changes/references/mutation.md#rebuild-lost-plants`, then plant
  your own at each new branch, call and argument the list skips, past each member of a limit the
  change states
  (`.claude/skills/morphic-verifying-changes/references/claims.md#comment-claims-held-by-test`) and
  in each class a claim covers (`references/testing.md#plant-each-class-a-claim-covers`), until
  `.claude/skills/morphic-verifying-changes/references/mutation.md#plants-stop-at-a-held-grid`. A
  listed mutant reddening a test shows only that its own site is pinned.
- **Why.** Each mutant #1153 listed reddened a test, while dropping an argument its new code passed
  reddened none. At #1406's a4dda68a its listed mutants reddened tests too, while its new checks
  fired on text writing no `$ref`.

## Plant in each input class a claim covers and its table leaves out
`plant-each-class-a-claim-covers`

- **Rule.** For a claim of every or no in a PR body, test name or comment, list the input classes it
  covers beside the rows its test's table holds, and plant a change scoped to each class with no
  row, such as a guard skipping only that class: every listed plant can redden while the claim
  fails for a class the table omits.
- **Why.** A body said a change adding a report on a union branch over a `$ref` loop now fails a
  test, while its table held a union only beside structural keywords; a plant skipping `oneOf` and
  `anyOf` pointers drew the report and passed (#1514, #1594).
- **Probe.** Each class with no row names its plant and the test that reddened.

## Hold a test the change leaves alone to a claim it widens
`claim-the-change-widens`

- **Rule.** Where the change adds a member to a set, such as a method to a type, hold to it each
  test name and each doc comment on the set that claims something of every member, though the
  change leaves them alone: plant in the new member a defect the claim covers
  (`references/testing.md#plant-each-class-a-claim-covers`) and see a test redden.
- **Why.** #1354 added two methods to `irfacts.Index`, documented as safe for concurrent use, and
  its first round, holding only tests it edited, never met
  `TestIndex_EveryMethodIsSafeForConcurrentUse`, which asked neither: an unsynchronized build
  behind them passed `go test -race` until it asked both (#1623, #1354 at aa62909a2 and
  cf810cca9).
- **Probe.** For the set's package, `go doc <package> <Type>` and
  `git grep -nE 'func Test[[:alnum:]_]*(Every|Each|All|No|Never|Only|Always)[A-Z]' -- '<directory>/*_test.go'`
  list candidates.

## Cross a function holding a path with each spelling of the working directory
`path-by-working-directory-spelling`

- **Rule.** A function the change adds or alters that stores or compares a path, a test helper's
  too, is probed from the working directory reached directly and through a symlink, with the path
  relative and absolute, as
  `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#invocation-axis` holds the
  compiler's ID, label and path rules.
- **Why.** Two absolute `$ref`s naming one file through a directory and a symlink to it each went
  unresolved from the other's directory (#1040), and a test helper failed run from a symlink to its
  tree (#1373, #1582).
- **Probe.** Each such function's probe from `cd <dir>` and from `cd <link to dir>`, both spellings
  of the path.

## Revert each rewritten site to find redundant guards
`redundant-guard`

- **Rule.** When a PR adds one predicate at many sites, revert each site to `main`'s spelling on its
  own. For any guard whose revert or mutant survives, ask whether removing it changes an outcome,
  the reader it falls through to included. Where one changes, even if only in the function's own
  result before a later stage masks it, keep the guard and pin it directly. Where none can, a second
  reader already makes the check: delete the guard, and let a test reach the branch, which fails
  loudly if that reader changes.
- **Why.** Five survivors in #1066 were covered by a choke point; one in #1140 was the function's
  own grammar. A nil check before a nil-safe library getter had nothing to pin, while the same check
  beside it kept a test from panicking (#742, #1189).

## A test that two mechanisms each keep green proves neither
`two-mechanisms-one-test`

- **Rule.** When a test survives removing one mechanism and separately survives removing another,
  plant the pair. Where each has an input only it decides, plant that input and say in the test's
  doc which test holds each mechanism alone. Where neither has one,
  `references/testing.md#redundant-guard` decides: keep the one the code needs, and name the pair's
  plant in the comment on it.
- **Why.** A components test survived removing the recorded declaration hint, and separately
  survived restoring the old target-first guess; only both removed turned it red (#553). For two
  guards no input separates, the two rules asked opposite things (#1610).
- **Probe.** For each mechanism, the input only it decides, or the reason none exists.

## A cheaper replacement must catch what the tool honours
`replacement-gate`

- **Rule.** Before accepting a grep or script in place of a linter, plant every spelling the tool
  honours, and require the new check to refuse a superset of what the old gate refused.
- **Why.** A `//nolint` grep missed spellings golangci-lint honours (#836).
