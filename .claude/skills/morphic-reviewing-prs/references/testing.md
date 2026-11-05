# Testing

The angle that attacks each function the change adds or alters with inputs meant to break it, and
each test with defects it should catch. It loads `morphic-verifying-changes`. A scratch test or a
compiled spec in the worktree is a probe; a package's suite is CI's
(`references/rounds.md#ci-is-the-gate`).

1. Cross each changed function with the input classes it can meet
   (`references/testing.md#function-by-input-class`), at the edges the code transforms
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#inputs-at-the-edges-the-code-transforms`)
   and at each bound
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#boundary-cases-at-the-boundary`).
2. Plant defects past the PR's list (`references/testing.md#plant-beyond-the-listed-mutants`), by
   `.claude/skills/morphic-verifying-changes/references/mutation.md`.
3. Hold each test the change adds or edits to what its name claims
   (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#assertion-targets-what-the-name-names`),
   to the stage it must reach
   (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#fixture-still-reaches-its-stage`)
   and to failing when the code is wrong
   (`.claude/skills/morphic-verifying-changes/references/tests-that-bite.md#assertion-must-be-able-to-fail`).
4. For a check, lint or archtest rule the change adds, plant what it must catch and what it must
   pass, by `.claude/skills/morphic-verifying-changes/references/checkers.md`; for one replacing
   another, take `references/testing.md#replacement-gate`.
5. For a reader, parser or grammar the change rewrites, fuzz it against the one it replaces
   (`references/correctness.md#refactor-differential-fuzz`).
6. Where the change adds one predicate at many sites, revert each site alone
   (`references/testing.md#redundant-guard`), and where a test stays green with either of two
   mechanisms removed, remove both (`references/testing.md#two-mechanisms-one-test`).

When the review fixes, each gap becomes a test seen to fail on its planted defect. The matrix is the
function-by-class cells and the mutants planted, killed and judged equivalent, each with its reason.

## Attack each changed function with every input class it can meet
`function-by-input-class`

- **Rule.** Cross each function the change adds or alters with the input classes it can meet:
  empty, one and many; each limit at N-1, N and N+1; duplicates and the opposite order; deep,
  cyclic, wide and aliased shapes; escapes and invalid UTF-8; each callee's error; a cancelled
  context. Run each cell as a probe, or settle it by a reason the report names, such as a type
  ruling the class out. A cell that misbehaves is a correctness finding; one the code takes its own
  path for needs a test failing when that path breaks.
- **Why.** Every corpus missed two defects in #1016's moved readers, each shown by one minimal
  input, and rounds sampling cases kept finding what the first should have (#1402).
- **Probe.** Count the cells, and those settled by a reason.

## Plant defects past the ones a PR lists
`plant-beyond-the-listed-mutants`

- **Rule.** Re-plant every mutation the PR lists
  (`.claude/skills/morphic-verifying-changes/references/mutation.md#rebuild-lost-plants`), then
  plant your own at each new branch, call and argument the list skips. A listed mutant reddening a
  test shows only that its own site is pinned.
- **Why.** Each mutant #1153 listed reddened a test, while dropping an argument its new code passed
  reddened none. At #1406's a4dda68a its listed mutants reddened tests too, while its new checks
  fired on text writing no `$ref`.

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
  plant the pair, and say in the test's doc which test holds each mechanism alone.
- **Why.** A components test survived removing the recorded declaration hint, and separately
  survived restoring the old target-first guess; only both removed turned it red (#553).

## A cheaper replacement must catch what the tool honours
`replacement-gate`

- **Rule.** Before accepting a grep or script in place of a linter, plant every spelling the tool
  honours, and require the new check to refuse a superset of what the old gate refused.
- **Why.** A `//nolint` grep missed spellings golangci-lint honours (#836).
