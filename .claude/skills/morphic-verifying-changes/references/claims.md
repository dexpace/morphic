# Claims against reality

Every sentence in a comment, doc, commit or PR body that states behaviour is a claim to execute, not
to read — and to execute outside the case its author measured.

## Check an every, never or only claim at every site it covers
`universal-claims-enumerate-every-site`

- **Rule.** Before a comment, assertion message or doc states an absolute (never, always, only,
  every, read by nothing, validated once), enumerate each caller, return path, producer, position
  and route it covers and check each; otherwise scope the claim to the sites checked or name the
  exceptions. A field's doc describes what every writer of the field writes.
- **Why.** "Read by nothing" was written while `constraints.go` read the field, and "never reports a
  spec-level problem" over paths that do (#111, #190).
- **Probe.** List the sites mechanically (writers by `git grep`, callers, return statements,
  composition positions) and compile one input per site, reading every diagnostic.

## Run the input a comment describes before saying what it does
`probe-behaviour-claims-by-running-the-input`

- **Rule.** A comment, test comment, design sentence or PR body saying what an input does (lowers to
  X, is dropped, deadlocks, is unreachable, is order-independent) is backed by compiling or running
  that literal input, in both orders where order is claimed. Pin the observed value in a test where
  it can drift.
- **Why.** `1XY` was said to lower to an undefined range and lowered to `1XX`, a "deadlock" was a
  stack overflow, and an "unreachable" branch was reached through a merge key (#263, #229).
- **Probe.** For "unreachable", try merge keys, aliases, and inline against `$ref` positions; for
  "would fail", run the alternative.

## Check claims about specs, libraries and toolchains at the source
`external-claims-checked-at-source`

- **Rule.** A comment stating what a specification, library, Go toolchain feature or other codebase
  does is checked against its text or a probe of it, cites the section or the version read, and
  names the component that consumes the input. A parser-library behaviour is cited by its
  `docs/parser-library.md` entry.
- **Why.** Claims from memory were wrong: the yaml.v3 alias budget never fires on node decode,
  json/v2 still sat behind an experiment flag, and section numbers were wrong (#480, #525).
- **Probe.** Read the dependency at the `go.mod` version, or write a one-off probe calling it with
  the cited input.

## Test the reason a comment gives, not only its verdict
`verify-the-stated-reason`

- **Rule.** When a comment credits a guard, call, set or value with a reason (this check
  distinguishes X, the set terminates cycles, the value is incidental), remove the cited thing and
  rerun the cited input. A right verdict with a wrong reason is still a defect.
- **Why.** Comments credited a redundant directory guard and justified a visited set by cycles the
  depth cap already handled (#368, #193).
- **Probe.** If the input still passes without the guard, the citation is wrong. For a named
  blocker, `git grep` the symbol; for an incidental value, list every branch that reads it.

## Run the real external tool on a planted input before relying on it
`probe-the-real-external-tool`

- **Rule.** When a script or check depends on a linter, jq, git output, the go command or a CI
  action, plant the construct and run the real tool from an unrelated directory as CI runs it: clean
  files, disabled linters, null arrays, colons in paths, CRLF, exported env vars.
- **Why.** nolint directives naming disabled or unknown linters passed silently, jq exited 0 with a
  shrunk set, and a setup-go bump spammed GOTOOLCHAIN warnings in every step (#367).
- **Probe.** Plant each directive or data shape in a scratch file, run the tool and the check
  against it from a different cwd, and compare their verdicts form by form.

## Name every outcome in a function's or option's contract
`doc-names-every-outcome`

- **Rule.** A doc comment names every outcome its branches produce (each failure condition, each
  grade of a strict mode, what happens at the limit) and every input class that breaks a guarantee
  it states.
- **Why.** A lead sentence naming one outcome made live branches read as dead code, and a promise of
  order-independence left out the declaration orders that break it (#267, #269).
- **Probe.** Map each return path and switch arm to a sentence in the comment; an unmapped path is a
  finding. For a guarantee, build the worst-case input and run it.

## Make a doc's headers and universals agree with its rows
`doc-agrees-with-itself`

- **Rule.** Within one doc, comment, table or PR body, a status line, header, category or universal
  agrees with the rows and sentences beside it and with the invariants the doc defines. After adding
  a row or a rule, re-read its section.
- **Why.** Docs said "not yet implemented" beside rows marked landed, and filed a row that is not
  refused under the refused header (#185, #246).
- **Probe.** Re-read each header and status line against each row; grep the same input across
  sibling tables and confirm they classify it alike.

## Check claims about scripts, CI and linters against what runs
`tooling-claims-match-the-gate`

- **Rule.** A doc or comment saying what a script, CI step, linter or metric does (what it counts,
  whether it is generated, what it puts on `PATH`) is checked against the code path or a run, and a
  command a doc quotes is copied from the gate step it mirrors and run verbatim.
- **Why.** A README claimed per-package coverage where the script computed one total, and documented
  a `gofmt` form the gate does not run (#315).
- **Probe.** Run each quoted command and compare its output with the gate's; for an environment
  claim, print the environment inside a CI job.

## Check each verb in a doc comment against the body
`doc-verbs-match-the-body`

- **Rule.** Each verb a doc comment uses (checks, records, caches, reads once, copies) and each
  ordering word (before, above) matches what the body does and how callers use it. A stated grammar
  or type rule gives the exact condition the code accepts.
- **Why.** Comments described memoization the function lacked, a check it left to callers, and a
  grammar with `digits*` where the code requires a digit (#266, #290).
- **Probe.** Read the comment verb by verb beside the body, list callers for each flow word, and
  test a stated grammar at its boundary input.

## Make every name in a comment resolve to one visible target
`comment-references-resolve`

- **Rule.** Every identifier, test name, field name and cross-reference ("see above") in a comment
  or doc resolves to exactly one existing target visible from where it is read: no function locals
  in GoDoc, no constants from a package above, no tests that do not exist, and owner-qualified field
  names where a short one is ambiguous.
- **Why.** Comments named a test that did not exist and used `Auth` for two fields reaching opposite
  conclusions, and a design doc referenced a type it never defined (#365, #266, #3).
- **Probe.** `git grep -nw` each identifier in its package, `go test -list '<Name>' ./...` each
  cited test, and read each cross-reference's target.

## Build every code sample in a doc under its own rules and check every cited precedent
`doc-code-obeys-own-rules`

- **Rule.** Code examples in READMEs, style guides and design docs must compile and run under the
  flags and banned-construct lists the same document mandates, and any code location a doc cites as
  precedent must actually say what the doc claims.
- **Why.** Exemplars that do not compile or use the forms the guide bans teach the wrong thing, and
  a design doc prescribed a package-level mutable registry citing code that says the opposite (#1,
  #760).
- **Probe.** Extract each sample into a scratch module and build/run it with the mandated flags and
  linter; open each cited file/line and compare.

## Test a safety or neutrality argument in every direction
`prove-safety-arguments-in-every-direction`

- **Rule.** A claim that a refactor is behaviour-neutral, a mismatch fails safely, a rewrite is a
  fixed point or a sort is stable is checked in each direction it can fail: lost, won and unknown;
  equal keys; each gate condition of the source. A derived gate reuses the source's result rather
  than a copied test.
- **Why.** A family winning an election was dropped silently, `sort.Slice` broke a promised stable
  order, and a copied memo gate dropped a condition (#270, #379).
- **Probe.** Write one case per direction; for neutrality, run old and new over the corpus and diff
  the IR.

## Measure before quantifying a cost or an effect
`measure-before-quantifying`

- **Rule.** Claim a speedup, memory bound or noise immunity only after a before-and-after
  measurement on the claimed workload. A count bound described as a memory bound is measured in
  retained bytes. Figures go in the PR body with their revision, not in source comments.
- **Why.** A per-walk view was kept for a benefit that never reproduced, and a shared count bound
  let retained memory grow (#379, #362).
- **Probe.** `go test -bench` and a heap profile after `runtime.GC` on the workload, before and
  after.

## Check 'same', 'distinct' and 'shared' claims on both sides with differing inputs
`claims-that-two-things-agree`

- **Rule.** When a comment says two traversals cover the same set, two code paths behave alike, two
  inputs share a behaviour, two derivations yield distinct results, or several input forms have the
  same effect, check each side separately, on non-fresh destinations and on inputs that differ only
  in what the derivation drops, and cite the code path for each.
- **Why.** Parity checked on one side or on a fresh value hid divergences: a populated destination
  decoded differently, URL templates differing only in punctuation collided, and only one of two
  directive spellings actually suppressed nothing (#264, #261).
- **Probe.** Write a two-row probe per claimed equivalence: run both paths on a pre-populated value,
  both input spellings, and a pair of inputs differing only in characters the canonicalisation
  drops; cmp.Diff the results.

## Report in a PR or commit only what was observed
`pr-claims-match-evidence`

- **Rule.** A PR body or commit reports what the final tree showed: a fix it names is in the diff; a
  mutation it cites was planted exactly, once, at the named site, with each subtest's outcome read
  under `-v`; "all routes" comes from probing each route. A result at one site is not a result for
  its mechanism.
- **Why.** Bodies claimed fixes that had not landed, "every package green" where none went red, and
  a bite in subtests that passed under the mutation (#349, #135, #382, #379).
- **Probe.** Before pushing, rerun each quoted mutation and probe on the final tree and paste what
  it printed.

## Hold every behavioural claim in a comment with a test
`comment-claims-held-by-test`

- **Rule.** When a comment says something propagates automatically, that a measurement proves a
  property, that a check deliberately stops at a boundary, or that a known-wrong answer is left
  standing, add a test that fails if that statement stops being true (pin the known-wrong answer in
  a row naming its issue).
- **Why.** Unenforced claims rot silently: widening a check's scope or breaking the described wiring
  leaves every test green, and a benchmark no CI job runs checks nothing (#111, #361).
- **Probe.** For each such comment, make the claim false (drop the wiring, widen the scope, fix the
  known-wrong row) and confirm a test reddens; if the claim depends on a benchmark, confirm a CI
  step runs it.
