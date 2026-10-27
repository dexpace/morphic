# Differentials and controls

A comparison is evidence only when it could have come out the other way. `scripts/sweep.py`
compiles a corpus with two builds; these rules decide whether its answer means anything.

## A probe that passes on main did not reach the defect
`main-control`

- **Rule.** Run every probe on `main` too and keep only those that fail there. A "main does the
  same" sentence is a claim about `main`: probe it.
- **Why.** A batch of order probes passed on both builds because hint composition resets at property
  keys (#561). A commit's claim about `main` held for the cyclic case only (#763).

## A differential also needs a planted-defect control
`planted-control`

- **Rule.** Before trusting "identical to main", run a build with the fix deliberately broken. It
  must differ.
- **Why.** All 40 generated documents agreed with `main` and with a broken fix, because none held
  the shape that differs (#772).

## A differential is as wide as its input generator
`generator-width`

- **Rule.** List the read paths and spellings the claim covers and make the generator write each
  one: positions past a `$ref`, aliases, merge keys, empty keys, every position kind, both
  declaration orders, wide documents as well as deep ones. Assert a floor on what was exercised.
- **Why.** A corpus differential never read through a reference (#774). A generator without `''`
  keys missed 69 regressions (#946). A corpus with no `+` in any fragment reached nothing (#933).
- **Probe.** Count the positions passed and the answers given, not only the inputs.

## Probe the join, not only the output
`invariant-probe`

- **Rule.** Where two computations of positions must agree, assert the agreement for every object
  over the corpora, and keep the probe as a test.
- **Why.** A corpus differential was identical on 2,460 runs while a join probe found a reference
  end the binder could not place (#1016; `TestBindSchemas_PlacesEveryEndAndBindsEveryRefInTheCorpus`
  is the kept probe).

## Use a transform differential for spelling and equivalence claims
`transform-differential`

- **Rule.** For a fix about how a character in keys or references is read, rename every key with two
  suffixes (`+Q` and `_Q`), compile both, map one onto the other and compare. For "X is the whole
  change", apply the expected rule to `main`'s output and require equality with the branch.
- **Why.** The rename oracle found error text leaking a respelled form after a byte-identical corpus
  check had reached nothing (#933). A transform differential showed docs moving rather than lost
  (#728).

## Classify crashes against a build with the refusal off
`three-build`

- **Rule.** For a change to what is refused, run each generated document through `main`, the branch,
  and the branch with the refusal planted off. Require: a branch crash implies a `main` crash; a
  clean `main` compile implies a clean branch compile; a branch-only refusal fails in the no-refusal
  build.
- **Why.** It found crashes `main` had and a draft refusal of a clean document (#946).

## A crash oracle reads the crash, not the exit status
`crash-oracle-text`

- **Rule.** Classify a subprocess crash by its output (`fatal error: stack overflow`), never by exit
  status 2 alone, which every panic also gives.
- **Why.** A panic also exits with status 2, so an oracle reading the status alone records any panic
  as the overflow it looks for; the reach oracle classifies its child's output instead (#942).

## Compare diagnostics as a sorted multiset
`two-order-diagnostics`

- **Rule.** In a two-order test, compare types order-invariantly and diagnostics as a sorted
  multiset keyed as `diagnosticSet` in `internal/harness/order.go` keys them: severity, code,
  pointer, node, source and whether a position is set, but not the message, which may list
  keywords in source order. Compare each operation's type targets too.
- **Why.** Diagnostics append in walk order, which the permutation changes (#808). A types-only diff
  missed a reference that took a type already interned at its pointer (#591).

## The order oracle never reaches specs with errors
`order-oracle-errors`

- **Rule.** `harness.Check` stops at the first error diagnostic. To measure order dependence over
  specs with errors, call `orderInvariant` from a scratch test in `internal/harness` on both
  builds, and delete it after.
- **Why.** Order-dependent specs hid behind their errors (#917).

## Never compare cmp.Diff text across runs
`cmp-diff-random`

- **Rule.** Compare outcomes (exit codes, the verdict word of a harness line), never `cmp.Diff`
  output, which go-cmp randomizes on purpose. Repeat a flagged difference before calling it one.
- **Why.** A sweep flagged a spec whose oracle printed a diff that split two ways on both builds
  (#934).

## Re-run I/O against a server whose answers change
`flaky-io`

- **Rule.** When a design re-runs I/O (a retry, a second pass), probe it with a handler that fails
  once and then succeeds, and with one whose content changes. Then force every compile in the suite
  through the second pass.
- **Why.** A deterministic test server answers alike, so "reaches no document the first pass did
  not" hid a false internal fault (#562).

## Ask the library rather than restating it
`ask-the-library`

- **Rule.** Test a helper documented "as the library reads it" against the library's own dispatch
  point, row by row, and plant the nearest plausible wrong grammar.
- **Why.** A hand-written index-grammar table agreed with an `Atoi`-bounded mutant (#834). Swapping
  a copy for the library call left wrong call sites alive (#835).

## A real-world corpus guards against regressions; it does not prove a fix
`corpus-is-a-guard`

- **Rule.** Sweep every corpus you have, but show that a fix works with inputs that spell the case.
- **Why.** About two thousand real specs were identical on `main` and a fixing branch, because a few
  dozen wrote the construct at all (#781).

## Prove the instrument reached its target before reading its result
`validate-the-instrument-first`

- **Rule.** An empty, passing or "identical" result counts only if the check reached the input. Run
  each verifier, command and differential once against a known positive. Beside any "identical", "0
  differ" or "none found", report the exit codes on each side, the bytes of output, the files parsed
  and the positive control's hit. A transform oracle re-parses its rewritten input before blaming
  the code.
- **Why.** A verifier was wired into CI and never met the output it checked; an invalid flag made
  every run exit 2, so a sweep read "identical"; a missing YAML parser, relative paths and a missing
  `timeout` each produced a false zero (#135, #200, #566, #591, #592, #933, #1032).
