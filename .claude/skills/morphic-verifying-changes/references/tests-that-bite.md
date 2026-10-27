# Tests that bite

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

A test proves nothing until the defect it names has been planted and it went red. Coverage reached is not behaviour held.

## Show every guard and branch is load-bearing by deleting it; coverage is not assertion
`every-guard-has-a-reddening-test`

- **Rule.** For each guard, skip, decoder check, refusal path or edge branch added or moved, delete or loosen it and confirm a test in its own package reddens; statement coverage only proves reachability, and a case that changes no mutation's outcome adds no coverage. If no fixture can reach a guard, record the gap beside it rather than presenting it as tested.
- **Also.** For each action, branch, nested member, non-first family member, skip path, sub-shape and downstream output a change touches, there must be a test that reddens when that one is broken; a guard pinned in one check does not pin its siblings.
- **Also.** Line coverage is not holding: each exit of the function a PR rests on, each rejection branch, and each named guarantee needs a test that reddens, by name, when that step is mutated, and that test must live in the package that owns the contract. Treat every surviving mutant as a missing test and rerun the mutation set after adding it.
- **Why.** The ir package reported full coverage while omitempty on Required or Nullable passed, decoder guards loosened to len(data)==4 passed, and moved cycle guards and refactored branches lost every test that held them.
- **Probe.** For each new or moved guard, replace its condition with false (or a looser form), run go test for its package, and require red; for refactors, plant a mutation per edge branch before moving the code.
- **Seen in #92** (b62b25fc): “It reported 100% statement coverage, but almost all of it came”
- **Seen in #264** (009429dc): “Weakening the guard to len(data) == 4 - so it swallows true, 1234 or any”

## Include inputs the transformation actually changes and that a plausible wrong implementation mishandles
`inputs-at-the-edges-the-code-transforms`

- **Rule.** A table, property or fuzz test must contain inputs at the representation's edges and in the classes a wrong implementation would get wrong: characters that need escaping, numbers float64 or int cannot hold, non-UTF-8 bytes, wrong-case or substring matches, partial overlaps, refused grammars, forbidden spellings, zero values, and the exact seed that broke the property. Where two readers must agree, diff them over a generated space, not only hand-written rows.
- **Also.** A regression or rule test must contain the inputs that separate right from wrong: the exact shape from the issue, both edges of each range and the values just outside, the same name in both syntactic positions, inputs that sort differently under the wrong collation, and fixture text that parses as intended.
- **Why.** Removing escaping, bounding IsIndex by Atoi, widening an acceptor or intersecting to one side all left suites green because no row exercised the edge where the implementations diverge.
- **Probe.** For each transformation under test, delete it (or swap in the naive version: substring match, strconv.Atoi, return-one-input) and run the table; if it stays green, add the edge input and repeat until the naive version reddens.
- **Seen in #801** (752d28b8): “Removing it left the spelled-out table green, because no”
- **Seen in #834** (f4f69b57): “an IsIndex bounded by strconv.Atoi passed the earlier table.”

## Give every route, kind, row, call site, condition, clause and exemption its own reddening case
`one-reddening-case-per-member`

- **Rule.** When code handles a family (routes sharing a loop, kinds through a dispatch arm, rows of a classification table, call sites of a helper, conditions of a predicate, narrowing clauses of a copied predicate, shapes of a construct, branches of a verifier, exemptions of a check), each member needs a case that fails when that member alone is mutated; incidental coverage from a shared fixture or the real tree is not enough.
- **Why.** Webhook callbacks, six object kinds through a generic arm, ID classification rows, and recursion shapes leaving through a free function all passed when removed, because only one member of each family was tested.
- **Probe.** List the family's members by grep, mutate each one alone (skip the route, drop the clause, widen the exemption, pass no argument at that call site) and run the package's own tests; every mutant must redden.
- **Seen in #567** (c246dec2): “The tests reached a parameter, path item, response and schema. A”
- **Seen in #834** (19986964): “No test covered a webhook's own callbacks or a callback operation's”

## Build a negative fixture where the guarded condition is the only thing that changes the result
`negative-fixture-isolates-the-guard`

- **Rule.** A guard or regression case needs an input the guarded code would mishandle without the guard: a typed nil rather than an untyped one, a callee that does not tolerate the input, a document truly over budget, structure for the mutation to land on, every input class the check claims, nodes of the kind the name says, rows of the shape a sizing or selection rule must exclude, and an outcome that differs between buggy and fixed code. A case both readings answer the same way guards nothing.
- **Why.** Nil-guard tests planted untyped nils that failed a type assertion first, a budget test's fixture fit the remaining budget, and a fallback assertion passed either way, so deleting each guard left the suite green.
- **Probe.** Delete or neutralise the guard and run the test; if it stays green, inspect why the input never needed the guard (tolerant callee, earlier failure, fits budget) and redesign the fixture until removing the guard reddens it.
- **Seen in #360** (8f28f931): “TestVerify_NilTypeBesideAUnionDoesNotPanic planted an untyped nil, which”
- **Seen in #567** (939233e6): “The budget test said a document already over its budget is refused”

## Revert the fix and require a committed fixture to redden; if none does, add the triggering shape
`fix-must-move-a-golden`

- **Rule.** A fix whose golden regeneration changes nothing, or whose revert leaves every oracle green, is evidence the corpus lacks the input shape the fix changes. Before pushing, add a committed fixture containing that exact shape (and, for a conflict rule, one where old and new behaviour disagree) and confirm reverting the fix reddens it.
- **Why.** Defects in encoded fragments, constraints beside verbatim unions, outside $refs to inline positions, timestamp rewrites and new lowering branches all survived because no committed spec held the triggering shape, so the goldens stayed green across the bug.
- **Probe.** git stash the fix's production hunk, run go test ./... and the harness over testdata/; if nothing reddens, write the minimal spec with the triggering shape, -update, restore the fix and confirm the golden diff is exactly the fix's effect.
- **Seen in #363**: “No golden moved when the fix landed, which is the corpus sharing the code's blind spot”
- **Seen in #119** (a4365f05): “and patternProperties value in it is bare, which is why regenerating the”

## Give competing sources, symmetric fields and interacting rules distinct values so each reading differs
`distinct-values-for-competing-sources`

- **Rule.** When the result depends on which of two sources, sides, arms, keywords or rules wins, the fixture must make them disagree: set the attribute on both sides with distinct values, set one of a symmetric pair alone, set one keyword without its companion, satisfy both rules' preconditions, and force two orders to contend for the same node. With equal or co-set values every reading passes.
- **Why.** Swapped exclusive bounds, wrong reply channels, losing precedence and order-dependent hints all passed because fixtures set both fields together, used the same channel on both sides, or used a bare branch that never contended for the pointer.
- **Probe.** Plant the swap (min for max, reply for operation channel, overlay order reversed, read moved below the check) and run the test; if green, change the fixture so the sources carry distinct values or only one is set, and rerun until the swap reddens.
- **Seen in #274** (629c2f07): “Every 3.0 case declared exclusiveMinimum and exclusiveMaximum together, so”
- **Seen in #311** (b8182d58): “The reply tests answered on the channel the operation binds, so both spellings”

## Validate an expected value independently before pinning it, so the test does not enshrine a defect
`expected-value-is-not-the-bug`

- **Rule.** Before accepting an expected output, check it against something outside the code: run a 'clean' fixture under the bounds the code enforces, parse emitted output with the next stage, confirm a 'nothing to keep' input really has nothing, update every test that pinned an edge case's old meaning, and classify a new differential oracle's failures against the reference's raw output.
- **Also.** An expected count or grammar must come from a source that cannot shrink or drift with the code: the specification, the library's own behaviour, the raw text or delimiter structure. An expectation derived from the same table, or restated by hand from the same rule, agrees with the implementation even when both are wrong.
- **Why.** Tests pinned a memory-exhausting fan-out as clean, YAML with an alias above its anchor as correct, and the loss of a summary and description as silence, so each test guarded the bug.
- **Probe.** Run each expected 'clean' fixture under the time and memory caps; feed expected output to the next parser; grep the input for content the expected output omits; print the reference's raw result for each failing differential row.
- **Seen in #99** (1d4ac861): “pinned a 40-level × 2-way fan-out as **clean**.”
- **Seen in #243**: “`TestReverseMappings_AliasIsNotFollowed` asserted the anchor-above-alias”

## Check that the assertion itself can fail: selector specificity, zero-match, type agreement, reachable bounds
`assertion-must-be-able-to-fail`

- **Rule.** An assertion is vacuous when its form cannot fail: a path selector that matches every map, a predicate wrong on its zero-match case, Equal/NotEqual between mismatched types, a bound a derived set can never exceed, or a grid whose axes cannot name the missing cell. Test the checker on its degenerate cases before trusting it. A completeness check must count the same units the fixture is built from (occurrences, not deduplicated sets).
- **Why.** A bare .Unmodeled selector admitted any carrier, Equal between a string and a jsontext.Pointer always failed and NotEqual always passed, and an enters-once test bounded the walk by the node count it could never exceed.
- **Probe.** Feed each assertion helper its zero-match, all-match and wrong-type inputs and confirm it reports failure; grep assertions on retyped fields for mismatched argument types; remove one cell from the grid and confirm completeness reports it.
- **Seen in #549** (d40c4433): “which would always fail as `Equal` and always pass as `NotEqual`”
- **Seen in #345** (d0e36f47): “which ends every path the value walk produces”

## Run every mutation a docstring or PR body cites, and read each subtest's result
`bite-claims-are-executed`

- **Rule.** Before a test docstring or PR body says a test catches a mutation, plant exactly that mutation, confirm it applies once and only at its site, and read per-subtest results with -v; passing and closing a coverage gap are not evidence a test bites, and neither is a test that was green on both branches before the change.
- **Why.** A PR body claimed a mutation reddened every package and none went red, a claimed three-subtest bite reddened two, and a mutation self-test first counted broken-file mutants as caught.
- **Probe.** Apply each cited mutation with a one-site patch, diff it against the original to confirm one hunk, run go test -v -count=1 on the cited test, and paste the per-subtest outcome into the claim.
- **Seen in #382** (4c06ee4e): “`go test ./compilers/openapi/... -count=1`: **every package green**”
- **Seen in #379** (96eeddb7): “it reddens two. This subtest passes under that mutation.”

## Prove a check, command or mutation actually ran against its target before reading its result
`validate-the-instrument-first`

- **Rule.** An empty, passing or 'survived' result is evidence only if the instrument demonstrably reached the input: run each verifier, diagnosis command and mutation once against a known positive, confirm the mutated file differs and compiles, and confirm a differential oracle's transform preserved meaning before blaming the code under test.
- **Why.** A verifier that never met its output, a command reading a stale working tree, a mutation pattern that matched nothing, or a non-compiling mutant all look exactly like a clean result.
- **Probe.** Plant one violation at the exact path the check reads and watch it fail; diff the mutated file against the original and run `go build` on it; for differential oracles, re-parse the rewritten input and compare encoded values.
- **Seen in #135**: “A verifier was complete, well-tested, wired into CI, and never met the output it existed to check.”
- **Seen in #200**: “A fourth mutation reported "survived" against a pattern that did not exist”

## Test caps and thresholds exactly at, below and above the boundary, and assert the counted amount
`boundary-cases-at-the-boundary`

- **Rule.** Any cap, depth bound, budget or threshold needs cases exactly at the boundary, one below and one above, with the whole output compared; where a bound is charged, assert the charged amount itself, not only which side of the limit it landed on.
- **Why.** An alias placed one level beyond a depth bound and a 30-gap listing let off-by-one mutants of the comparison pass, and a refusal charge counted wrongly still landed on the same side of the limit.
- **Probe.** Mutate each comparison by one (<= to <, > to >=) and each charge amount, run the tests, and add the at-boundary case until every mutant reddens.
- **Seen in #340** (5176b0a5): “TestBuild_TracksAncestorsOnlyToItsDepthBound places its alias one level past”
- **Seen in #828** (2359a62b): “The only test of the cap listed 30 gaps, which left the count line's”

