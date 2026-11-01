# Assertions, fixtures and corpus reach

Assert the property the name claims, with inputs that reach the code and an expectation from outside
it.

## Confirm a fixture still reaches its stage after an upstream change
`fixture-still-reaches-its-stage`

- **Rule.** A new earlier check, a moved validation or a classifier change can stop a fixture
  earlier, where it passes for the wrong reason. Confirm each affected fixture reaches its stage
  past every earlier error return, assert that precondition before the outcome, and re-plant each
  refusal arm the earlier check may now shadow.
- **Why.** Fixtures for the round-trip and order oracles stopped at an unrelated error, and a
  grammar check moved ahead of `ParseFloat` shadowed its `IsInf` arm, all while green (#549, #269).
- **Probe.** `go run ./cmd/morphic-harness <fixture>` runs `harness.Check` and prints `ok` or the
  first oracle that fired, with its code.

## Show a corpus witness for every site a lowering handles
`corpus-witnesses-every-site`

- **Rule.** List the domain a lowering branches over (literal kinds, keyword positions, sites
  writing a reference, compiler-assigned fields, both halves of a rule) and confirm the committed
  corpus has an input reaching each member. Presence per capability is not coverage per branch.
- **Why.** Keywords were dropped at declaration level because the corpus wrote them only on
  properties, and compiler-assigned fields were never non-zero, so the code writing them had no
  witness (#137, #119).
- **Probe.** Grep the compiler for every site writing the construct, then `testdata/` for an input
  reaching each; give each site with no hit a fixture, confirmed by a mutant planted there.

## Make a test's name and labels claim only what it reaches
`test-claims-match-reach`

- **Rule.** A test name, comment, witness label or coverage note describes what its assertions check
  and which inputs run: count skipped against exercised inputs, confirm a named witness shows the
  outcome in its golden, and check that no other test of the same input negates a timeout or guard.
- **Why.** Sweeps implied coverage of fixtures the compiler declined, conformance specs claimed rows
  their goldens did not show, and a time bound protected nothing because the fixture ran unbounded
  elsewhere (#342, #119).
- **Probe.** Plant the defect the test claims to catch; for a sweep, print the skipped and checked
  counts; for a witness, grep the golden for the construct.

## Assert the field, location or direction the name claims
`assertion-targets-what-the-name-names`

- **Rule.** Read each test name and fixture comment as a claim and check its assertions pin it: a
  gap cell asserts the field the gap names, a location test fails when the location is remapped, a
  direction fixture traverses that direction, a copy test asserts the state it preserves. Rename or
  split a case whose input has another property.
- **Why.** Gap cells asserted `Extensions` and `Usage` instead of the named field, and querystring
  tests stayed green with the location remapped (#111, #364).
- **Probe.** Write the one-line mutation the name describes and confirm that test, specifically,
  reddens; an unrelated change leaves it green.

## Make test guards, globs and oracles fail on what they cannot classify
`test-guards-fail-closed`

- **Rule.** A guard over declarations, a self-check, a glob, a fixture path or an oracle that can
  decline fails on input it cannot classify or find: no silent skip of unparsed spellings or
  aliases, no log-only self-check, no glob matching nothing, no `t.Skip` on a missing fixture in a
  gate run, no oracle returning ok without comparing, no case expecting failure that does not
  first assert its fixture exists.
- **Why.** Const-block guards skipped conversions and aliases, a benchmark read a missing path and
  exited 0, the order oracle returned ok without comparing on anchor-bearing specs, and a
  misspelt fixture satisfied a "fails" case (#111, #344, #381).
- **Probe.** Feed each guard an input it cannot classify, assert every glob matches a file, fail
  when no comparison ran, and rename a fixture.

## Assert diagnostics by code, severity and subject, and the whole list
`assert-diagnostics-by-identity-and-completely`

- **Rule.** Assert each diagnostic by its stable code, severity, pointer or named subject and
  per-site count, not by message substring or a generic 'something degraded'; assert the full
  diagnostic list a call returns (never discard the error or leave diagnostics unasserted), give
  every emitted diagnostic a case that triggers it, and when matching an external tool's message
  match only the fragment the code controls.
- **Why.** Reworded or duplicated announcements passed substring tests, a dropped
  constraint-reporting call left the suite green, and count-based tests silently changed meaning
  when a split changed the count (#382, #834).
- **Probe.** Delete each diagnostic-emitting call and reword each message; the first must redden a
  test and the second must not. Compile the fixture and diff the full diagnostic list against the
  test's expectation.

## Test a consumer on real output through the production entry point
`exercise-the-production-path`

- **Rule.** A verifier, pass or oracle meets the real output of the stage feeding it, built by
  production constructors and read from the tree the system read, and a cached path is diffed
  against the fresh one over the corpus. Hand-built documents, hand-set internal fields and
  test-only wrappers do not show the real path works.
- **Why.** The `Preserved` verifier ran only on hand-built fixtures, so an empty reason passed, and
  a reused parse dropped a stream's tail on every CLI run with the suite green (#119, #485).
- **Probe.** Plant the defect in the producing stage and confirm the consumer's test reddens; grep
  tests for literals of internal state.

## Assert the value itself, not a count, length or non-panic
`assert-the-value-not-a-proxy`

- **Rule.** A test must compare the actual value or identity it is about against an expectation: not
  only Len, digest length, the entry's existence, that nothing panicked, that the outcome is not X,
  or count-plus-membership (each expected key exactly once), and never a compiled value discarded
  with _ =. A proxy that a wrong value also satisfies asserts nothing.
- **Why.** Wrong example values, dangling derived keys, path-derived hashes and empty stand-ins for
  skipped refs all passed tests that checked only count, existence, length or no-panic (#222).
- **Probe.** For each assertion, plant a mutant that keeps the proxy true but changes the value
  (wrong example, different key, fresh hash, duplicate at one key and none at another) and confirm
  the test reddens; grep tests for `_ = ` on compiled results.

## Assert every output a change writes or preserves
`assert-every-output-the-change-touches`

- **Rule.** When code fills several outputs, merges several fields, writes several structural keys,
  or promises to leave its input or a copy untouched, assert each of them, and give sibling cases
  the same guard set. Deleting any element a fixture declares reddens some test.
- **Why.** A $ref docs merge narrowed to description alone, an alias losing its co-declared keyword,
  a builder dropping paths, and an in-place sort of the caller's slice all passed because only one
  projection was asserted (#119, #363).
- **Probe.** Delete each field the code writes (or each element of the fixture) one at a time and
  run the suite; for no-mutation promises, plant an in-place sort or fresh construction and assert
  on the caller's own value after the call.

## Make a failing test fail fast and name its cause
`tests-fail-fast-and-legibly`

- **Rule.** Guard preconditions with require (NotNil, length) before dereferencing or indexing; diff
  member sets rather than counts; and give a hang-prone guard a bounded-time case that fails in
  seconds.
- **Why.** A panic or package timeout hides the cause, and a count mismatch names no member (#149,
  #379).
- **Probe.** Break the code under test and read the failure: it must name the cause within seconds
  and not panic.

## Finish every check before a golden write
`finish-checks-before-the-golden`

- **Rule.** Finish every check before any golden write. No assertion follows the write, and one that
  may start a parallel subtest runs inside a serial `t.Run`, so the guard reads a settled
  `t.Failed()`.
- **Why.** A non-fatal assertion under -update rewrote the golden from regressed output (#149,
  #379). A parallel subtest runs after its parent returns, and an assertion after the write has not
  run, so neither stops a guard placed ahead of the write (#1242).
- **Probe.** With -update, plant a regression that changes the golden at each kind of check, on both
  sides of the write and inside a parallel subtest, and confirm no golden changes.

## A closure in a parallel subtest must not use the parent's t
`parent-t-in-subtest`

- **Rule.** Build files and directories once in the parent; a closure that needs a `t` takes the
  subtest's.
- **Why.** A `require` on the outer `t` inside a parallel subtest turned a failure into
  `panic(nil)`, and the loop created temp files for every check (#1131).
- **Probe.** Make one subtest's `require` fail: the run must name that subtest's failure, not
  report `panic(nil)` or `runtime.Goexit`.

## When two paths give one answer, assert which one ran
`prove-which-path-answered`

- **Rule.** A test of a fast path, cache, rebuild branch, reuse check or stop point must show which
  path produced the result, by asserting the path was taken (index populated, counter, hook,
  distinct sentinel) or by planting a value only one path would return. Comparing results alone
  passes when the path is bypassed.
- **Why.** An index-versus-scan test compared the scan with itself when the index was disabled, a
  reuse test passed under content equality, and a cancellation row passed whichever boundary fired
  (#379).
- **Probe.** Disable the fast path or weaken the identity check (byte identity to content equality,
  skip the rebuild) and confirm the test reddens; if not, add the path-taken assertion or plant the
  divergence.

## A golden records; it does not assert a decision
`goldens-record-not-verify`

- **Rule.** A golden records what happened, not what is correct, and regeneration absorbs any
  change. Where a golden carries a design decision (field placement), a layout rule (column width)
  or is cited as pinning a behaviour, add a direct assertion; never hard-code a layout constant; and
  a golden writer must refuse trivially empty outputs such as nil.
- **Why.** A literal column width of 9 misaligned longer commands and the regeneration accepted it,
  and a golden for a nil document matched every later nil document, asserting nothing (#312, #321).
- **Probe.** Delete the behaviour the golden is cited for from the input and confirm the golden
  bytes change; lengthen a rendered name past any constant width; call the golden writer with nil
  and confirm it errors.

## Loop timing- and scheduling-sensitive tests before committing them
`nondeterministic-tests-proven-stable`

- **Rule.** Run a test that depends on timing or goroutine scheduling hundreds of times, through the
  path CI uses, with and without `-race`, before calling it stable, and diagnose without adding
  output. A cost is pinned by a count, never a timing ratio
  (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#count-work-not-wall-clock`).
  A helper type-checking several packages in one load runs repeatedly under `-race`.
- **Why.** A ratio allowance of 3 against a separation of 2 failed CI on noise, `go/types` raced on
  a shared import in a fraction of runs, and a leak test failing 2 in 1000 runs passed every run
  once a print was added (#494, #560, #535).
- **Probe.** `go test ./<pkg> -count=1000 -run '<Test>'`, plain and under `-race`: no failure for
  the test, every run for its planted defect.

## Keep a test's verdict independent of platform and privilege
`environment-independent-tests`

- **Rule.** Test each platform branch on any host by injecting that platform's reading: a test
  that runs the code on the host reaches the host's branch alone, and a fix that matters only on
  a platform CI does not run is such a branch. A fixture a privileged runner can bypass (`chmod`
  under root) is replaced by one nobody can bypass, and a test that depends on a tool driver pins
  its mode (`GOPACKAGESDRIVER=off`).
- **Why.** A rooted-path fix was indistinguishable from the check it replaced on every CI host, and
  a `chmod`-based test skipped under root, failing the coverage gate (#833, #344); scripts tested
  only the host's branch of a unit conversion and a command choice (#1143).
- **Probe.** Revert the platform fix and confirm the injected-reading test reddens; run the coverage
  gate once as root.

## Pin the expected order literally; never compare a run with itself or rely on map order
`ordering-tests-pin-the-order`

- **Rule.** A test asserting a sort or an order-invariant result must pin the expected order or
  positions literally; a run compared with its own first output, or two orders compared only with
  each other, passes for any deterministic wrong answer, and an input whose order comes from a map
  passes a broken comparator by chance.
- **Why.** A comparator returning 0 passed sort tests, a two-order test passed when every finding
  was dropped, and a missing version tiebreak passed about one run in five (#119, #565).
- **Probe.** Plant a comparator returning 0 and a dropped tiebreak, run the test with -count=50, and
  confirm every run fails; plant a uniformly wrong result and confirm the two-order test fails.

## Name fixtures so the right answer sorts last
`sort-order-fixture`

- **Rule.** When a change sorts for determinism, give each rule a fixture in which the semantically
  right site sorts last (declared at `/z`, reused at `/a`).
- **Why.** Pointer order picked the right site by coincidence in every fixture while the rule was
  not implemented (#548). The two-order oracle cannot see this.
- **Probe.** Make the code take the first site in sort order instead of applying the rule; the
  fixture whose right site sorts last reddens.

## When moving tests, keep every case and the right package
`test-moves-preserve-cases-and-scope`

- **Rule.** Diff test case names before and after a table rewrite; file each test with the source
  file that defines the unexported symbol it calls; keep tests that use only the public API in
  package foo_test.
- **Why.** Consolidations silently dropped a table row and moved public-API tests into the internal
  package (#246, #92).
- **Probe.** Diff the `--- PASS` lines of `go test ./<pkg> -run '<Test>' -v` before and after,
  since `go test -list` names no subtest; check the package clause of every touched test file; map
  unexported calls to defining files.

## Keep an oracle's equality options from hiding its defect
`no-equivalence-that-erases-the-distinction`

- **Rule.** An equality, round-trip or order-invariance oracle must not use an equivalence option
  (EquateEmpty and similar) or exclude a field unless a probe shows the exclusion is needed. Any
  option or exclusion that erases the distinction the oracle guards turns that oracle blind to
  exactly its defect.
- **Why.** The round-trip oracle compared with EquateEmpty and so hid null-versus-[] flips, and the
  order-invariance oracle excluded Naming.Hint, the very field the order-dependent hint bug lived in
  (#18, #375).
- **Probe.** For each cmpopts option or ignored field in an oracle, remove it and rerun the corpus;
  then plant the flip the oracle guards (null vs [], a differing Hint) and confirm the oracle goes
  red with the option in place.
