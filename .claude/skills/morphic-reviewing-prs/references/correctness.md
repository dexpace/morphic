# Correctness

The first angle, as the rest weigh only correct options: whether every input gives the result a
consumer should get, on the head as it will merge. It loads `morphic-verifying-changes` and the
changed files' domain skill, such as `morphic-openapi-compiler`, `morphic-changing-the-ir`, or
`morphic-curating-skills` for `.claude/skills`.

1. Read the diff before the PR body (`references/correctness.md#code-before-claims`).
2. For each output the change alters, state the result a consumer should get on real input, then
   compile a minimal spec on `$main` and in the worktree and read the IR and every diagnostic
   (`.claude/skills/morphic-verifying-changes/references/differentials.md#main-control`), and the
   golden spec where it holds the construct, fetched as `CLAUDE.md`'s "Priority" section shows.
   Where they differ, take `references/correctness.md#better-than-main`.
3. Probe each `CLAUDE.md` invariant the diff could reach, rather than reading for it. Fidelity,
   Determinism, Robustness and Contract take losslessness, order, bounds and what consumers read.
4. Sweep every site of each changed mechanism
   (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#sweep-every-site-of-the-mechanism`).
5. Take each rule below that names this kind of change: moved code
   (`references/correctness.md#moved-code-vs-rule`), a rewritten reader
   (`references/correctness.md#refactor-differential-fuzz`), a query
   (`references/correctness.md#query-differential`), a generated sweep
   (`references/correctness.md#reroot-generated-sweep`), a merge of `main`
   (`references/correctness.md#ported-check-grammar`) or a CI or cache change
   (`references/correctness.md#simulate-ci-runs`).

A normative document has no output to compare: run its rules as a program
(`references/design.md#review-design-rules`). The matrix is each altered output with its spec and
both builds' results.

## Probe the code before the claims
`code-before-claims`

- **Rule.** Read the diff before the PR body, and in the first round, before any corpus run: diff
  old against new semantics at every named bound, ask which input class the corpus cannot contain
  (deep nesting, oversized documents, aliasing), compile each new distinction where what it
  compares ignores its property
  (`.claude/skills/morphic-verifying-changes/references/assertions-and-reach.md#distinction-where-nothing-depends`),
  and grep for mutable globals and layering breaks.
- **Why.** Three rounds that took their agenda from the PR body found only body defects. A fourth
  that read the new file found two code defects in it (#340). At #1406's a4dda68a its suite passed
  while its new checks fired on text writing no `$ref`.

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

## A moved function's new tests pin what it does, not what the rule says
`moved-code-vs-rule`

- **Rule.** For each output a moved function's new tests pin, compare it with the governing rule in
  `docs/compilers/openapi.md`, and probe it on `main` and on the head.
- **Why.** A characterization test pinned a warning at the carrier against a rule that places it at
  the value's position (#1100).

## Fuzz a rewritten reader against the one it replaces
`refactor-differential-fuzz`

- **Rule.** For a refactor that rewrites readers (parsers, splitters, containment tests), copy the
  old functions from `$main` beside the new ones in a scratch module and fuzz that each pair
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

## Re-root a generated sweep at every root the mechanism reaches
`reroot-generated-sweep`

- **Rule.** A generator that places every target under one root (`/components/schemas`) proves
  nothing about the others: rerun it under each root the mechanism reaches, such as a response body
  or a parameter.
- **Why.** 0 misnamed variants under components, 61 of 164 under a response body (#553). When two
  functions must agree on a name and share no code, they drift outside the swept domain.

## After a merge of main, sweep with the base's grammar
`ported-check-grammar`

- **Rule.** Removing a mechanism because the change makes it unnecessary holds only for the cases
  the change touched. After merging `main`, run the base's current generators over many seeds before
  the merge commit.
- **Why.** Evidence gathered on an older generator missed a stack overflow the base's grammar wrote
  (#566).

## Simulate the runs a CI or cache change claims
`simulate-ci-runs`

- **Rule.** For a change to `.github/workflows`, the `Makefile`'s CI targets or anything setting
  what Go caches, check each claim of what a run reuses, reruns or saves by simulating the runs:
  fresh clones (`git clone -n`) at one fixed path, the CI steps in order and a carried `GOCACHE`,
  on base and head, quoting the `(cached)` lines and reading each test's opens from
  `-test.testlogfile`, the environment held fixed
  (`.claude/skills/morphic-verifying-changes/references/toolchain.md#cache-probe-holds-the-environment`).
  Steps reading a file's age take `references/correctness.md#age-based-step-at-mains-cadence`, jobs
  no pull request triggers `references/correctness.md#job-no-pull-request-triggers`.
- **Why.** Simulated runs of #1272's work found a test reading through `git` replay a stale pass
  once files were dated, the listing closing that gap with no test, and a CI step deleting nothing
  in its job (#1272, #1374).
- **Probe.** The report gives each claim beside its simulated runs' `(cached)` lines.

## Simulate a step that reads a file's age at the gap main's pushes keep
`age-based-step-at-mains-cadence`

- **Rule.** A step deciding by a file's age, such as a prune of the cache, is simulated at the gap
  between `main`'s pushes, from `git log --first-parent -40 --format=%ct "$main"`, as well as a
  long one, and each pull request's run reads a copy of the saved cache: a pull request never saves,
  and the go command's reads move a live cache's times.
- **Why.** A prune by age, simulated three hours apart, kept the cache flat, and 40 minutes apart,
  near `main`'s median gap of 35, let it grow from 598 to 1280 MB, as Go moves an entry's time on a
  use only once it is over an hour old (#1373, #1559).
- **Probe.** The report gives each gap simulated beside the median measured.

## Read a real run for a job no pull request triggers
`job-no-pull-request-triggers`

- **Rule.** A job that only `schedule` or `workflow_dispatch` starts runs from the default branch's
  workflow, so no pull request and no simulation reaches its trigger. Say so, read a real run's log
  from a trial branch in its place (`gh run view <id> --log`), and hold each body sentence about the
  job to what that run predates.
- **Why.** A nightly job's PR cited a push run on a trial branch, and `gh run list` filtered to
  `--event schedule` printed none (#1382, #1606).
- **Probe.** `gh run view <id> --json event,headSha,headBranch`, beside the commits the branch
  gained after that run.
