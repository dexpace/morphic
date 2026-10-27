# Assertions, fixtures and corpus reach

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

Assert the property the name claims, with inputs that reach the code and an expectation from outside it.

## After any change upstream of a fixture, confirm it still reaches the check, branch or oracle it names
`fixture-still-reaches-its-stage`

- **Rule.** A new earlier check, a moved validation, a classifier change or a setup change can make a fixture stop earlier and pass for the wrong reason. Confirm each affected fixture reaches its target stage past every earlier error return, assert the precondition (it parses, it reached the oracle) before the outcome, and re-plant a neutralisation of each refusal arm the earlier check may now shadow.
- **Why.** Fixtures meant for the round-trip and order oracles stopped at an unrelated earlier error, a grammar check moved ahead of ParseFloat shadowed the IsInf arm, and a classifier change rerouted a row to the wrong branch, all while green.
- **Probe.** Run harness.Check on each fixture and read which oracle fired first; for each refusal arm downstream of the change, neutralise it and confirm a test reddens; count the target branch's hits in the test.
- **Seen in #549** (d40c4433): “stops at an unrelated error before the oracle runs, so it tests nothing”
- **Seen in #269** (e955e9cc): “the whole suite green, and only the 100% statement gate noticed”

## Enumerate every kind, position, site and field a lowering handles and show a corpus witness for each
`corpus-witnesses-every-site`

- **Rule.** For a lowering or convention, list the domain it branches over (literal kinds, keyword positions, reference-writing sites, compiler-assigned fields, edge kinds, both halves of a rule, every type a convention governs, every option an encoder pins) and confirm the committed corpus contains an input reaching each member. Per-capability presence is not per-branch coverage.
- **Why.** Keywords were dropped at declaration level because the corpus only wrote them on properties, a dangling ref at encoding headers was invisible to the closure check, and many compiler-assigned fields were never non-zero, so the code writing them had no witness.
- **Probe.** Grep the compiler for every site that writes the construct (TypeRef writes, keyword reads, field assignments), then grep testdata/ (or the compiled corpus IR) for an input reaching each one; any site with zero hits gets a fixture, confirmed by a planted mutation at that site.
- **Seen in #137**: “No corpus spec wrote `minProperties`”
- **Seen in #119** (39aed2b4): “No corpus spec declared encoding.<part>.headers, so the TypeRef the compiler”

## Make a test's name, comment and witness labels state only what its assertions reach
`test-claims-match-reach`

- **Rule.** A test name, test comment, witness label or coverage justification must describe what the assertions actually check and which inputs actually run: count skipped versus exercised inputs, confirm a named witness produces the outcome in its golden, check the matrix status for every row a fixture claims, and check that a timeout or guard is not negated by another test of the same input.
- **Why.** Sweeps implied coverage of fixtures the compiler declined, conformance specs claimed rows their goldens did not show, a test name asserted a property nothing checked, and a 10s bound protected nothing because the fixture ran unbounded elsewhere.
- **Probe.** Plant the defect the test claims to catch and watch it go red; for a sweep, print counts of skipped vs checked inputs; for a witness label, grep the golden for the construct; for a timeout, grep the suite for other tests compiling the same fixture.
- **Seen in #342** (ff7909bc): “Six specs named capability rows their goldens do not show: a spec whose”
- **Seen in #119** (e7686977): “The sweep reads as though it verifies every deliberately-broken fixture, and”

## Make each test assert the exact field, location, direction or property its name and comment claim
`assertion-targets-what-the-name-names`

- **Rule.** Read each test name and fixture comment as a claim and check the assertions pin it: a gap cell asserts the field the gap names (not neighbours), a location test fails when the location is remapped, a direction fixture traverses that direction, a row named for a code path reaches it, a copy test asserts preserved state, and claimed-distinct values are distinct. Rename or split a case whose input has a different property.
- **Why.** Gap cells asserted Extensions and Usage instead of the named field, querystring tests stayed green with the location remapped, and a fixture named for one traversal direction ran the other way.
- **Probe.** For each test, write the one-line mutation its name describes (remap the location, apply the named fix, fresh-construct instead of copy) and confirm that test, specifically, reddens; apply an unrelated change and confirm it stays green.
- **Seen in #111** (8591a4d7): “that Extensions was empty and XML was nil. That only catches a fix”
- **Seen in #364** (852f1d9c): “The two unit tests for declared keywords were named for a location they”

## Make test-side guards, globs and oracles fail on what they cannot classify, never skip or log
`test-guards-fail-closed`

- **Rule.** A guard over declarations, a self-check, a glob, a benchmark fixture path or an oracle that can decline must fail on input it cannot classify or find: no silent skip of unparsed spellings or aliases, no log-only self-check, no glob matching zero files, no t.Skip on a missing fixture in a gate run, no oracle returning ok without comparing, no guard keyed to a file path that moved.
- **Why.** Const-block guards skipped conversions, aliases and untyped literals, a benchmark read a nonexistent path and exited 0, and the order oracle returned ok without comparing on anchor-bearing specs, so each looked like coverage while checking nothing.
- **Probe.** Feed each guard one unclassifiable input (an alias, a conversion, an untyped constant, a blank reason) and confirm it fails; assert every glob matches at least one file; count executed comparisons and fail when zero.
- **Seen in #111** (6cce414a): “silently skipped any spec”
- **Seen in #344**: “`BenchmarkAnchorWalk`'s fixture path was two directory”

## Assert diagnostics by code, severity, subject and count, and assert the complete set the call emits
`assert-diagnostics-by-identity-and-completely`

- **Rule.** Assert each diagnostic by its stable code, severity, pointer or named subject and per-site count, not by message substring or a generic 'something degraded'; assert the full diagnostic list a call returns (never discard the error or leave diagnostics unasserted), give every emitted diagnostic a case that triggers it, and when matching an external tool's message match only the fragment the code controls.
- **Why.** Reworded or duplicated announcements passed substring tests, a dropped constraint-reporting call left the suite green, and count-based tests silently changed meaning when a split changed the count.
- **Probe.** Delete each diagnostic-emitting call and reword each message; the first must redden a test and the second must not. Compile the fixture and diff the full diagnostic list against the test's expectation.
- **Seen in #382** (4c06ee4e): “Reword the message and all three go green while covering nothing”
- **Seen in #834** (8414e706): “single call, and nothing asserted it: dropping the call left the suite”

## Test a consumer on the producing stage's real output and through the entry point production calls
`exercise-the-production-path`

- **Rule.** A verifier, pass or oracle must meet the real output of the stage that feeds it, built by production constructors and read from the same tree the system read; a cached or reused path must be diffed against the fresh path over the corpus. Tests on hand-built documents, hand-set internal fields, hand-written literals or test-only wrappers do not show the real path works.
- **Why.** The Preserved verifier ran only on hand-built fixtures so an empty reason passed everything, a reused parse dropped the stream tail on every CLI run with the suite green, and a lazy-init branch was reachable only by setting a field by hand.
- **Probe.** Plant the defect in the producing stage (empty reason, dropped field in the cache) and confirm the consumer's test reddens; grep tests for struct literals of internal state and for helpers no production caller reaches.
- **Seen in #119** (65539ac2): “nothing: planting Reason:"" at a preserve call passed go test ./... entirely.”
- **Seen in #485**: “a reused parse now has to carry the stream tail (a mutation dropping it silenced `openapi/stream-documents-dropped` with the whole suite green)”

## Assert the value itself, never only its count, length, existence, a negation, or non-panic
`assert-the-value-not-a-proxy`

- **Rule.** A test must compare the actual value or identity it is about against an expectation: not only Len, digest length, the entry's existence, that nothing panicked, that the outcome is not X, or count-plus-membership (each expected key exactly once), and never a compiled value discarded with _ =. A proxy that a wrong value also satisfies asserts nothing.
- **Why.** Wrong example values, dangling derived keys, path-derived hashes and empty stand-ins for skipped refs all passed tests that checked only count, existence, length or no-panic.
- **Probe.** For each assertion, plant a mutant that keeps the proxy true but changes the value (wrong example, different key, fresh hash, duplicate at one key and none at another) and confirm the test reddens; grep tests for `_ = ` on compiled results.
- **Seen in #222**: “The multipart-encoding test required only that the entry exist.”
- **Seen in #222**: “The ghost-reference test asserted that nothing panics.”

## Assert every field, key and side effect the code writes or must preserve, across every sibling case
`assert-every-output-the-change-touches`

- **Rule.** When code fills several outputs, merges several fields, writes several structural keys, or promises to leave its input or a copy untouched, assert each of them, and give sibling cases the same guard set. Every element a fixture declares should be deletable only by reddening some test.
- **Why.** A $ref docs merge narrowed to description alone, an alias losing its co-declared keyword, a builder dropping paths, and an in-place sort of the caller's slice all passed because only one projection was asserted.
- **Probe.** Delete each field the code writes (or each element of the fixture) one at a time and run the suite; for no-mutation promises, plant an in-place sort or fresh construction and assert on the caller's own value after the call.
- **Seen in #119** (8a8b6e00): “anywhere: narrowing the referent branch to the description alone left the whole”
- **Seen in #363** (ce662ba2): “Only the constraints were asserted, so”

## Make a failing test fail quickly, with a named cause, and never rewrite a golden on failure
`tests-fail-fast-and-legibly`

- **Rule.** Guard preconditions with require (NotNil, length) before dereferencing or indexing; diff member sets rather than counts; give a hang-prone guard a bounded-time case that fails in seconds; and stop before any golden write when an earlier assertion failed.
- **Why.** A panic or package timeout hides the cause, a count mismatch names no member, and a non-fatal assertion under -update rewrote the golden from regressed output.
- **Probe.** Break the code under test and read the failure: it must name the cause within seconds and not panic; run the golden test with -update while an assertion fails and confirm the golden file is unchanged.
- **Seen in #149** (27647532): “A golden could be rewritten by a failing run.”
- **Seen in #379** (b92870d6): “Two of the three new subtests dereference a possibly-nil *yaml.Node inline”

## When two paths give the same answer, assert which path ran or plant a disagreement between them
`prove-which-path-answered`

- **Rule.** A test of a fast path, cache, rebuild branch, reuse check or stop point must show which path produced the result, by asserting the path was taken (index populated, counter, hook, distinct sentinel) or by planting a value only one path would return. Comparing results alone passes when the path is bypassed.
- **Why.** An index-versus-scan test compared the scan with itself when the index was disabled, a reuse test passed under content equality, and a cancellation row passed whichever boundary fired.
- **Probe.** Disable the fast path or weaken the identity check (byte identity to content equality, skip the rebuild) and confirm the test reddens; if not, add the path-taken assertion or plant the divergence.
- **Seen in #379** (b92870d6): “TestChildByToken_IndexAgreesWithTheScanItReplaces never asserts the index was built, so a change that silently disables it leaves the test green”
- **Seen in #379**: “test asserted only the answer, which both readings give, so it now plants a divergence between index and pairs”

## Do not let a golden stand in for an assertion of a decision, a layout rule, or a non-empty result
`goldens-record-not-verify`

- **Rule.** A golden records what happened, not what is correct, and regeneration absorbs any change. Where a golden carries a design decision (field placement), a layout rule (column width) or is cited as pinning a behaviour, add a direct assertion; never hard-code a layout constant; and a golden writer must refuse trivially empty outputs such as nil.
- **Why.** A literal column width of 9 misaligned longer commands and the regeneration accepted it, and a golden for a nil document matched every later nil document, asserting nothing.
- **Probe.** Delete the behaviour the golden is cited for from the input and confirm the golden bytes change; lengthen a rendered name past any constant width; call the golden writer with nil and confirm it errors.
- **Seen in #312**: “The root-help command column was padded to a literal 9.”
- **Seen in #321**: “on disk that every later nil document matched: a snapshot that asserts nothing”

## Loop timing-, scheduling- and race-sensitive tests many times before committing them
`nondeterministic-tests-proven-stable`

- **Rule.** A timing ratio must interleave its two measurements and separate the growth rates well beyond noise (including under -race); a reach test for a goroutine or race check must be run hundreds of times to record its false-pass rate; helpers that type-check several packages in one load run repeatedly under -race.
- **Why.** Sequential timing and a ratio allowance of 3 against a separation of 2 failed CI on noise, a planted goroutine check passed intermittently, and go/types raced on a shared import in a fraction of runs.
- **Probe.** Run the new test with -count=200 (and -race) under taskset on a loaded machine and record the failure rate; it must be zero for a pass and one hundred percent for its planted defect.
- **Seen in #494**: “The test took the minimum over every walk of the 16000-key tree, then the minimum over every walk of the 8000-key tree.”
- **Seen in #560** (e07aad4e): “The timing test failed CI on noise.”

## Do not let a test's verdict depend on the host platform or the runner's privilege
`environment-independent-tests`

- **Rule.** A fix that changes behaviour only on a platform CI does not run must inject that platform's reading as an input and test it on the current host; a fixture whose failure mode a privileged runner can bypass (chmod under root) must be replaced with one nobody can bypass.
- **Also.** A test depending on privilege, permission bits or an external tool driver must pin the mode explicitly (e.g. GOPACKAGESDRIVER=off) or skip when the environment bypasses it, and be run once as the CI user.
- **Why.** A rooted-path fix was indistinguishable from the check it replaced on every CI host, and a chmod-based test skipped under root, leaving a statement uncovered and failing the coverage gate.
- **Probe.** Revert the platform-specific fix and confirm the injected-reading test reddens on Linux; run the coverage gate once as root (or in a root container) and confirm no test skips.
- **Seen in #833** (f61c8abf): “no test CI runs could tell it from the check it replaced”
- **Seen in #344**: “used a `chmod 0o000`”

## Pin the expected order literally; never compare a run with itself or rely on map order
`ordering-tests-pin-the-order`

- **Rule.** A test asserting a sort or an order-invariant result must pin the expected order or positions literally; a run compared with its own first output, or two orders compared only with each other, passes for any deterministic wrong answer, and an input whose order comes from a map passes a broken comparator by chance.
- **Why.** A comparator returning 0 passed sort tests, a two-order test passed when every finding was dropped, and a missing version tiebreak passed about one run in five.
- **Probe.** Plant a comparator returning 0 and a dropped tiebreak, run the test with -count=50, and confirm every run fails; plant a uniformly wrong result and confirm the two-order test fails.
- **Seen in #119** (65539ac2): “Two pass tests claimed the reference sites are sorted before any diagnostic is”
- **Seen in #565** (9e87c7da): “The two-order test compared the reports of a source as written and”

## When moving or rewriting tests, keep every case, the defining file, and the external package
`test-moves-preserve-cases-and-scope`

- **Rule.** Diff test case names before and after a table rewrite; file each test with the source file that defines the unexported symbol it calls; keep tests that use only the public API in package foo_test.
- **Why.** Consolidations silently dropped a table row and moved public-API tests into the internal package.
- **Probe.** Diff `go test -list . -v` subtest names before and after; check the package clause of every touched test file; map unexported calls to defining files.
- **Seen in #246** (72d25ccc): “Also restores the fractional-seconds row dropped when that table was”
- **Seen in #92** (c3e4e312): “and restore package_test (external) for the files whose remaining tests”

## Make a performance guard time only the code under test and measure the quantity the regression moves
`perf-guards-measure-the-defect`

- **Rule.** A cost test or benchmark must time only the function under test with inputs built outside the clock, pin the code path when input size is near a path-selecting threshold, and measure the quantity the defect changes (walk steps, not allocations); confirm it reddens on the reverted code.
- **Why.** An allocation-ratio guard could not see a read-only quadratic walk, a timing test measured the parser instead of the walk, and a benchmark crossed the sniff size cap and timed the byte scanner.
- **Probe.** Revert the fix and confirm the guard fails; add a counter or probe inside the timed region to confirm the target function, on the intended path, is what runs.
- **Seen in #488**: “A guard for this shape has to measure the walk, not its garbage.”
- **Seen in #488**: “so `sniff` took the byte-scan path and the "fixed" numbers I first measured were the scanner, not the walk at all.”

## Do not let an oracle's equality options or exclusions hide the difference it exists to catch
`no-equivalence-that-erases-the-distinction`

- **Rule.** An equality, round-trip or order-invariance oracle must not use an equivalence option (EquateEmpty and similar) or exclude a field unless a probe shows the exclusion is needed. Any option or exclusion that erases the distinction the oracle guards turns that oracle blind to exactly its defect.
- **Why.** The round-trip oracle compared with EquateEmpty and so hid null-versus-[] flips, and the order-invariance oracle excluded Naming.Hint, the very field the order-dependent hint bug lived in.
- **Probe.** For each cmpopts option or ignored field in an oracle, remove it and rerun the corpus; then plant the flip the oracle guards (null vs [], a differing Hint) and confirm the oracle goes red with the option in place.
- **Seen in #18** (73fac1cd): “That blanket equating also masks a real null-vs-[] flip”
- **Seen in #375**: “no longer excludes `ir.Naming.Hint` at all”

