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
  what it states. Rule 1.2's `gosec` is #1173's.
- **Why.** Checks written by hand or held only by review moved into linters, each where one can
  state it, which the guide's set could not do (#1001).
- **Probe.** `grep -n funlen .golangci.yml`.

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

## Take an IR struct's shape from `docs/ir-design.md`
`ir-shapes-stand`

- **Rule.** Rule 5.27 avoids embedding in a public struct. `docs/ir-design.md` is normative for
  `ir` and stands where the two differ: each type kind embeds `TypeCommon`, and `Header` embeds
  `Property` so the two encode flat as one object.
- **Why.** A header's shape was weighed against fields on `Property`, and chosen so that the type
  states the rule and no check has to (#1144).
- **Probe.** `grep -n '^    TypeCommon$' docs/ir-design.md`.

## A change is done at `make gate`
`done-is-make-gate`

- **Rule.** The go-styleguide skill ends on `gofmt`, `golangci-lint`, `go vet` and `go test`. Here a
  Go change is done at `make gate`, which adds the pinned linter version, the planted findings, the
  repository's analyzers, fuzzing and exactly 100% coverage.
- **Why.** One uncovered statement fails CI while `go test` passes, which forced follow-up commits
  and falsified a PR body (#22, #373).
- **Probe.** `grep -n '^gate:' Makefile`.

## Report as a question a rule the tree breaks with no decision behind it
`breaches-with-no-decision`

- **Rule.** Nothing records why the tree differs from rules 1.2 (`gosec`), 2.23 (import aliases),
  5.9 (type aliases), 5.26 (compile-time assertions), 6.4 (assertion helpers), 11.5 (Example
  functions) and 11.9 (license headers), and #1173 asks which are departures. Until it answers, a
  line a change adds is still held to the rule, since surrounding code is no defence (`CLAUDE.md`,
  "Correctness comes first"), and reported as a `question:` naming #1173. Rule 7.9 is on #1173 too,
  but `CLAUDE.md` restates it, so a new package is held to `doc.go` outright. Rule 10.6's
  `http.DefaultClient` is #1036's.
- **Why.** Each was counted against the tree when this list was made, and none has a decision on
  the tracker (#1172, #1173).
- **Probe.** #1173 gives the command behind each count, and a row it settles leaves this rule.
