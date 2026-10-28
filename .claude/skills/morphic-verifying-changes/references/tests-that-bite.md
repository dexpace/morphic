# Tests that bite

A test proves nothing until the defect it names has been planted and it went red. Coverage reached
is not behaviour held.

## Show every guard and branch is load-bearing by breaking it
`every-guard-has-a-reddening-test`

- **Rule.** For each guard, skip, decoder check, refusal path or edge branch a change adds, moves or
  keeps, break it (`cond || true` for a guard with no else, a looser condition, an early return) and
  require a test in the owning package to redden. Coverage proves reachability, not holding, and
  cannot see an untaken guard. A guard pinned in one check does not pin its siblings. If no fixture
  can reach a guard, say so beside it.
- **Why.** The ir package read 100% while `omitempty` on `Required` passed and a decoder guard
  loosened to `len(data) == 4` passed; no test took a `CutLast` guard's false side; a pure filter
  survived "always true" (#92, #264, #524, #566).
- **Probe.** Before a refactor moves code, plant a mutation per edge branch, and again after.

## Include the inputs a plausible wrong implementation mishandles
`inputs-at-the-edges-the-code-transforms`

- **Rule.** A table, property or fuzz test holds inputs at the representation's edges and in the
  classes a naive version gets wrong: characters needing escape, numbers `float64` or `int` cannot
  hold, non-UTF-8 bytes, wrong-case or substring matches, partial overlaps, both edges of each range
  and the values just past them, one name in both syntactic positions, inputs a wrong collation
  sorts differently, the exact shape from the issue, and the seed that broke the property. Where two
  readers must agree, diff them over a generated space.
- **Why.** Removing escaping, bounding `IsIndex` by `Atoi`, widening an acceptor or intersecting to
  one side all left suites green, because no row sat where the implementations diverge (#801, #834).
- **Probe.** Swap in the naive version (substring match, `strconv.Atoi`, return one input) and add
  edge rows until it reddens.

## Test a cut at every alignment
`cut-every-alignment`

- **Rule.** For code that cuts on a boundary, put the bound at each byte of a multi-byte rune (pad
  0–3 bytes before a 4-byte rune), and add a run of continuation bytes for the start-of-text guard.
- **Why.** Four green tests put the bound one byte into a rune, so `for` → `if` and a dropped
  `n > 0` both survived (#1130).
- **Probe.** As on the cut in `barrier/text.go`: plant `for` → `if` in the rune-boundary loop, then
  drop its `n > 0`; each must redden a case.

## Give every member of a family its own reddening case
`one-reddening-case-per-member`

- **Rule.** When code handles a family (routes sharing a loop, kinds through one dispatch arm, rows
  of a table, call sites of a helper, conditions of a predicate, exemptions of a check), each member
  needs a case that fails when that member alone is mutated. Coverage from a shared fixture or the
  real tree is not enough.
- **Why.** Webhook callbacks, object kinds through a generic arm and ID classification rows all
  passed when removed, because one member of each family was tested (#567, #834).
- **Probe.** List the members by grep, mutate each alone (skip the route, drop the clause, pass no
  argument at that site) and run the package's own tests.

## Build a negative fixture that only the guard decides
`negative-fixture-isolates-the-guard`

- **Rule.** A guard's case needs an input the code would mishandle without it: a typed nil, not an
  untyped one; a callee that does not tolerate the input; a document truly over budget; an outcome
  that differs between the buggy and the fixed code. A case both readings answer alike guards
  nothing.
- **Why.** A nil-guard test planted an untyped nil that failed a type assertion first, and a budget
  test's fixture fit the remaining budget, so deleting either guard left the suite green (#360,
  #567).
- **Probe.** Delete the guard; if the case stays green, find why the input never needed it (tolerant
  callee, earlier failure, fits the budget) and redesign it.

## Revert the fix; a committed fixture must redden
`fix-must-move-a-golden`

- **Rule.** A fix whose regeneration changes no golden, or whose revert leaves every oracle green,
  shows the corpus lacks the shape it changes. Add a committed fixture with that exact shape, one
  where old and new behaviour disagree, and confirm the revert reddens it.
- **Why.** Defects in encoded fragments, constraints beside verbatim unions and outside `$ref`s to
  inline positions survived because no committed spec held the shape (#363, #119).
- **Probe.** Revert the fix's code and keep its tests and fixtures, with `scripts/mutate.py` for a
  one-hunk fix, else in a detached worktree of the head, removed after:
  `git diff <merge-base> HEAD -- ':(exclude)*_test.go' ':(exclude,glob)**/testdata/**' | git apply -R`.
  Run `go test ./...`, which sweeps `testdata/` through the harness; once a fixture reddens, check
  the golden diff is exactly the fix's effect.

## Give competing sources distinct values
`distinct-values-for-competing-sources`

- **Rule.** When the result depends on which of two sources, sides, arms or rules wins, make them
  disagree: set the attribute on both sides with distinct values, set one of a symmetric pair alone,
  set a keyword without its companion, and make two orders contend for one node. With equal or
  co-set values every reading passes.
- **Why.** Swapped exclusive bounds and a wrong reply channel passed, because fixtures set both
  bounds together and answered on the channel the operation binds (#274, #311).
- **Probe.** Plant the swap (min for max, reply for operation channel, overlay order reversed) and
  require red.

## Check an expected value outside the code before pinning it
`expected-value-is-not-the-bug`

- **Rule.** Take an expected output, count or grammar from a source that cannot drift with the code:
  the specification, the library's behaviour, the raw input, the next stage's parser. Run a "clean"
  fixture under the bounds the code enforces, confirm a "nothing to keep" input has nothing, and
  classify a new oracle's failures against the reference's raw output. An expectation restated from
  the same table agrees with the implementation when both are wrong.
- **Why.** Tests pinned a memory-exhausting fan-out as clean, YAML with an alias above its anchor as
  correct, and the loss of a summary as silence, so each guarded the bug (#99, #243).
- **Probe.** Run each expected-clean fixture under time and memory caps, feed expected output to the
  next parser, and grep the input for what the expected output omits.

## Check that the assertion itself can fail
`assertion-must-be-able-to-fail`

- **Rule.** An assertion is vacuous when its form cannot fail: a selector matching every map, a
  predicate wrong on zero matches, `Equal` or `NotEqual` between different types, a bound a derived
  set can never exceed, a completeness grid counting deduplicated sets where the fixture builds
  occurrences. Test a checker on its degenerate inputs before trusting it. After retyping a field,
  and after each merge of `main` into that branch, find testify calls whose sides differ in type
  with a `go/packages` scan: grep cannot see types.
- **Why.** testify compares dynamic types: once a field became a `jsontext.Pointer`, `Equal` with a
  string always failed and `NotEqual` always passed. A bare `.Unmodeled` selector admitted any
  carrier (#549, #345).
- **Probe.** Feed each helper its zero-match, all-match and wrong-type inputs; remove one grid cell
  and see completeness report it.

## Pin each bound at the limit and one past it, at every site
`boundary-cases-at-the-boundary`

- **Rule.** A cap, depth bound, budget or threshold needs cases at the limit, one below and one
  above, comparing the whole output and asserting the charged amount where a bound charges. Assert
  that the production constructor wires the constant. Plant "lowered by one" at each site
  separately, on the arithmetic rather than through a type that clamps it. Name in the PR any
  constant past test scale.
- **Why.** Off-by-one mutants passed tests placed a level past the bound; ten bound sites survived
  on `main` because their tests used budgets far from the edge; `AtMost(-1)` clamps to zero, so
  planting through it changed nothing (#340, #1140).
