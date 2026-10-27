# Review probes

Probes that found what earlier review rounds missed. Each one runs through
`morphic-verifying-changes`: compile the input, plant the defect, keep a `main` control.

## Probe the code before the claims
`code-before-claims`

- **Rule.** In the first round, before any corpus run: diff old against new semantics at every named
  bound, re-plant every mutation the PR lists, ask which input class the corpus cannot contain (deep
  nesting, oversized documents, aliasing), and grep for mutable globals and layering breaks.
- **Why.** Three rounds that took their agenda from the PR body found only body defects. A fourth
  that read the new file found two code defects in it (#340).

## Never accept "as on main" inside the change's mechanism
`better-than-main`

- **Rule.** Where head and `main` differ, decide which is right on the specification's own terms. If
  `main` is right, fix the head. If neither is, build the right rule at its owner. A better
  behaviour that changes output is listed under Breaking. A defect `main` shares is fixed or filed,
  never a reason to stop.
- **Why.** A parity round copied a library defect and kept an order dependence "as on main" (#1016).

## A PR that moves readers off the library inherits what the library skipped
`moved-reader-parity`

- **Rule.** Diff `resolutionBaseline` in `internal/archtest` between `main` and head: each package
  that loses a library call is a moved reader. Probe each one with inputs the old reader never
  reached (references inside other documents, components behind chains, aliases, escapes), and
  account for every diagnostic `main` reported that head drops.
- **Why.** Two regressions in #1016 were invisible to every corpus.

## A new route inherits the old route's open bugs
`new-route-open-bugs`

- **Rule.** When a change makes X "work as Y does", rewrite each open repro of Y (`gh issue list
  --search "<mechanism>"`) into X's spelling and run both builds. A new crash path is a regression
  even when its root is an open issue. Check merged and open PR bodies for handoffs:
  `gh pr list --state all --search "#N in:body"`.
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
  own. Delete a survivor only when a second reader of the same concept already covers it. When the
  masking reader is a later stage or a library, keep the guard and pin it directly.
- **Why.** Five survivors in #1066 were covered by a choke point; one in #1140 was the function's
  own grammar.

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
  base and head for a large input of the old shape and for the golden spec
  (`.claude/skills/morphic-verifying-changes/scripts/golden_spec.py` prints its path):
  `/usr/bin/time -l` on macOS, `perf stat -e instructions:u` on Linux.
- **Why.** Allocation counts miss CPU work, and load skews wall-clock time (#1133).

## Fuzz a rewritten reader against the one it replaces
`refactor-differential-fuzz`

- **Rule.** For a refactor that rewrites readers (parsers, splitters, containment tests), copy the
  old functions from `origin/main` beside the new ones in a scratch module and fuzz that each pair
  agrees. Turn intended divergences into narrow, named exclusions. Afterwards diff the shipped
  function bodies against the copies the harness ran.
- **Why.** On #510 this ran tens of millions of inputs per reader in a minute and made every
  intended divergence explicit.

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
