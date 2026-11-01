# Where this repository departs from the styleguide

Each rule says where the guide, or the org's go-styleguide skill, tells a reader otherwise than this
repository does, and what holds the difference. A number such as 9.1 is a rule of the guide, read at
styleguide @591e5243. If one no longer says what a rule here quotes, the guide has moved: correct
the rule here rather than reading it across.

## Take the linters from `.golangci.yml`, not from chapter 01
`linter-set-lives-here`

- **Rule.** Rule 1.2 says to enable exactly its linters, commit its configuration verbatim and
  disable `funlen`. The set here is `.golangci.yml`, which enables `funlen` to hold the guide's own
  rule 1.5 and adds the linters that hold this repository's invariants, each with a comment saying
  what it states. It leaves rule 1.2's `gosec` off.
- **Why.** Checks written by hand or held only by review moved into linters, each where one can
  state it, which the guide's set could not do (#1001). `gosec` gave 174 findings, 16 in production
  and none of those a defect: a compiler opens the paths it is named (#1173).
- **Probe.** `grep -n funlen .golangci.yml`, and `grep -c gosec .golangci.yml` prints 0.

## Serialize through `encoding/json/v2`, whatever chapter 09 calls
`json-through-v2`

- **Rule.** Rules 9.1, 9.3, 9.6 and 9.8 name `encoding/json` with its `Marshaler`, `Unmarshal` and
  `Decoder`. Here JSON goes through `encoding/json/v2` and `encoding/json/jsontext`, tests included
  (`CLAUDE.md`, "Serialization"). Keep what each rule protects and take its spelling from that
  section, `omitzero` beside `omitempty` among it.
- **Why.** Every v1 call runs v2 under its legacy options, with HTML escaping, invalid UTF-8
  rewritten and duplicate names accepted, so depguard refuses v1 in every file (#1001).
- **Probe.** `git grep -ln '"encoding/json"' -- '*.go'` lists only the plants under
  `internal/lintrules/testdata`.

## Recover a panic through `barrier.Catch` alone
`recover-through-barrier`

- **Rule.** Rule 3.16 allows `recover()` at a boundary. Outside tests a boundary here calls
  `barrier.Catch` and writes no `recover` of its own.
- **Why.** A hand-written recover quoted what it caught with no bound on its length or its lines,
  and took a `panic(nil)` for a return under `GODEBUG=panicnil=1` (#1130).
- **Probe.** `git grep -ln 'recover()' -- '*.go' ':!*_test.go'` lists `barrier` and the plants
  alone.

## Check for a leak with `internal/leakcheck`, not `goleak`
`leak-check-not-goleak`

- **Rule.** Rule 4.16 names `goleak`. Here `internal/leakcheck` wraps `TestMain` around the
  toolchain's `goroutineleak` profile, and `internal/archtest` requires it wherever a goroutine
  starts. It sees only a goroutine blocked on an unreachable primitive, and whether `goleak` should
  cover the rest is #568.
- **Why.** The profile needs no dependency, so the check took it first and left `goleak` as a
  separate call (#514, #535).
- **Probe.** `go test ./internal/archtest -run TestGoroutineLeak`.

## Recurse to a bound, whatever the org's skill says
`bounded-recursion-stands`

- **Rule.** The go-styleguide skill's rule 9 says no recursion in library code. Rule 5.15 and the
  guide's deviations table allow recursion to an explicit, asserted limit, as `CLAUDE.md` does under
  "Bounded everything", so the skill's line is the one out of step. A cycle the lowering gains joins
  `TestLoweringRecursion_IsOnlyTheKnownCycles`, which holds each known one.
- **Why.** A compiler reads a document's nesting, so the lowering recurses on purpose, and the test
  pins its cycles so that a new one shows (#212).
- **Probe.** `go test ./internal/archtest -run TestLoweringRecursion`.

## Keep a date a source writes as its text
`source-dates-stay-text`

- **Rule.** Rule 9.4 carries time as `time.Time`. A date a source document writes stays its text, as
  `Deprecation.RemovalDate` and a YAML timestamp do (`docs/compilers/openapi.md#values`), since
  parsing it loses the spelling invariant 2 keeps. Rule 9.4 governs a time the code itself holds.
- **Why.** No source format defines a removal date's format, so the IR records the text and leaves
  the calendar to the consumer (#464).
- **Probe.** `git grep -n 'time\.Time' -- 'ir/*.go' ':!*_test.go'` finds no field.

## A pipeline stage logs nothing and reports a spec problem as a diagnostic
`stages-return-diagnostics`

- **Rule.** Chapter 08 assumes code logs, and chapter 03 that a failure is an `error`. A pipeline
  stage writes to no stream and logs nothing, and a problem in the source is an `ir.Diagnostic`,
  with an `error` for I/O and programmer faults alone (`CLAUDE.md`, invariant 5). Chapter 08 applies
  to an injected logger, which no package takes yet.
- **Why.** What a run shows is the engine's or the CLI's to decide, so forbidigo refuses a stage
  every way to reach a stream (#974, #1001).
- **Probe.** `git grep -ln '"log/slog"' -- '*.go' ':!*_test.go'` lists only a plant.

## Take an IR struct's shape from `docs/ir`
`ir-shapes-stand`

- **Rule.** Rule 5.27 avoids embedding in a public struct. `docs/ir` is normative for
  `ir` and stands where the two differ: each type kind embeds `TypeCommon`, and `Header` embeds
  `Property` so the two encode flat as one object.
- **Why.** A header's shape was weighed against fields on `Property`, and chosen so that the type
  states the rule and no check has to (#1144).
- **Probe.** `grep -rn '^    TypeCommon$' docs/ir`.

## A change is done at `make gate`
`done-is-make-gate`

- **Rule.** The go-styleguide skill ends on `gofmt`, `golangci-lint`, `go vet` and `go test`. Here a
  Go change is done at `make gate`, which adds the pinned linter version, the planted findings, the
  repository's analyzers, fuzzing and exactly 100% coverage.
- **Why.** One uncovered statement fails CI while `go test` passes, which forced follow-up commits
  and falsified a PR body (#22, #373).
- **Probe.** `grep -n '^gate:' Makefile`.

## Write an assertion helper where tests share the check
`assertion-helpers-stand`

- **Rule.** Rule 6.4 bars a project helper that takes `*testing.T` and fails the test itself, test
  double setup excepted, and rule 6.3 asks `t.Helper()` of every helper. Here a helper asserting
  through testify stands and starts with `t.Helper()`, and one a table runs as a case's body needs
  no such call: it is the case, and its failure reports its own line (`CONTRIBUTING.md`, "A test is
  named for what it tests"). Another assertion library stays barred.
- **Why.** The rule's concerns are a lost caller's line and a stop at the first failure, which
  `t.Helper()` and `assert` answer, so rewriting the helpers would add no correctness (#1173).
  `thelper` reads a signature, so it reported 298 functions and none was a breach (#1178).
- **Probe.** `go test ./internal/archtest -run TestTestHelpers`.

## Compare a plain value with `assert.Equal`
`assert-equal-on-plain-values`

- **Rule.** Rule 6.16 takes `cmp` for structs, slices, maps and interfaces and bars
  `reflect.DeepEqual`, which `assert.Equal` calls. Here `assert.Equal` stands on a value with no
  unexported field or `Equal` method, which `cmp` alone calls. Any other value takes `cmp.Diff` with
  a direction key (rule 6.15), and no test calls `reflect.DeepEqual` itself.
- **Why.** Such a value lacks what the rule's reason names: the two agree on nil against empty, on
  order and on a NaN in separate values, and `assert.Equal` labels both sides where a bare
  `cmp.Diff` labels neither (#1206). A literal slice or map goes this way about twice as often as
  the tree calls `cmp.Diff` (#1246).
- **Probe.** `git grep -n 'reflect\.DeepEqual(' -- '*_test.go' ':!*testdata*'` names no call, and
  `git grep -nE '^func \([a-zA-Z]+ \*?[a-zA-Z]+\) Equal\(' -- '*.go' ':!*testdata*'` no method.

## Alias a type to name it at a boundary or to shorten it
`type-aliases-that-stand`

- **Rule.** Rule 5.9 keeps type aliases for migration. Three kinds stand here: a public face naming
  a type an internal package declares, which no caller outside could name otherwise, as
  `compilers/openapi/options.go` does; a shape `docs/ir` gives, as `ir.Lifecycle`; and a
  package's private shorthand for a long type, as `schemaRef`. Any other alias is held to the rule.
- **Why.** A defined type in the public face would restate a vocabulary, and two declarations of one
  strategy set can drift (#1173).
- **Probe.** `git grep -nE '^type [A-Za-z]+ = ' -- '*.go' ':!*_test.go' ':!*testdata*'`.

## Leave Example functions until the API settles
`examples-wait-for-a-stable-api`

- **Rule.** Rule 11.5 asks for a testable Example of every public function. Not yet here, while the
  public API takes breaking changes: add one where it is the clearest documentation, and take the
  rule up at the first stable release.
- **Why.** An Example is one more thing each breaking change rewrites, and tests pin the behaviour
  already (#1173).
- **Probe.** `git log -30 --first-parent --format=%s origin/main | grep -cE '^[a-z]+(\([^)]*\))?!:'`
  counts the recent merges that broke the API.

## Assert interface compliance for an exported pair alone
`interface-assertions-stay-narrow`

- **Rule.** Rule 5.26 asks for compliance to be asserted at compile time. Here an exported type
  implementing an interface this module exports carries a `var _` beside its imports, and an
  unexported one does not, since the call that passes it fails the build. A standard library
  interface such as `error` takes none.
- **Why.** An exported pair is a contract no call in its package need exercise (#1173, #1176).
- **Probe.** `git grep -nE '^(var )?[[:space:]]*_ +[A-Za-z.*]+ += ' -- '*.go' ':!*_test.go' ':!*testdata*'`
  lists them.

## Hold a new line to a rule the tree still breaks
`rules-the-tree-still-breaks`

- **Rule.** Rule 10.6 bars the `http.DefaultClient` `engine/reader/disk.go` falls back to (#1036),
  and rule 6.15 the `cmp.Diff` failures most tests print with no direction key (#1206). A line a
  change adds is held to the rule, since surrounding code is no defence (`CLAUDE.md`, "Correctness
  comes first"). A row leaves this rule when its issue's fix lands.
- **Why.** The list is what someone met and filed, not every rule the tree breaks: rule 6.15 went
  unlisted until a review of new lines raised it (#1192, #1208). The fallback waits on a time bound
  for a network read, no matter of style (#1036, #1173).
- **Probe.** `git grep -n 'http.DefaultClient' -- 'engine/reader/*.go' ':!*_test.go'`, and #1206's
  script, which counts the bare calls. `gh issue list --state open --limit 1000 --search styleguide`
  lists what else has been filed.
