# Sweep every site the change touches

A defect is a property of its mechanism: sweep every site of it and every sentence that describes
it.

## Fix the mechanism at every site that runs it
`sweep-every-site-of-the-mechanism`

- **Rule.** When a change fixes a defect, adds a guard or closes an issue, or a review finds a
  defect, name the mechanism, grep every site that runs it and every writer of the field it
  governs, and probe each node kind it can land on. Fix each site, or state the uncovered one in
  code and in the PR body. An issue's own site list is a claim, not the scope.
- **Why.** The pointer-collision fix left the same collision in the inline-position hoist; grids
  built from an issue's shapes passed while a type-array variant failed; a per-site revert showed
  most call sites untested (#157, #101, #808, #542); a review's fix missed a second file (#1143).
- **Probe.** For each hit of the mechanism's grep, show the fix or plant the reproducer and watch
  it go red.

## Sweep a mechanism that turns on a type with the types
`sweep-a-typed-mechanism-by-type`

- **Rule.** Where the mechanism turns on a type, such as a range over a map or a value holding an
  unexported field, sweep it with `go/types`: a `golang.org/x/tools/go/packages` program, run from
  the module root of a scratch clone and removed after. A grep finds a spelling, not a type.
- **Why.** Of 997 range statements outside tests at 0d09a96d, 63 range over a map, and 3 of those
  lines match `grep -iE 'map|byid|keys'`: `for index := range status` names no map (#1538).
- **Probe.** The program first reports a site planted for each form the mechanism takes, nested in a
  container and behind a pointer too, and passes a planted near miss; then the report gives its
  sites beside the grep's.

## Fix the state at the type that owns it, not where the defect surfaced
`fix-at-the-invariants-owner`

- **Rule.** When a defect comes from state a type owns, fix that type, so every reader through
  its accessors inherits the fix, rather than guard the call where the crash fired. Say in the PR
  body why each alternative loses, and hold the correctness argument with tests: a two-order case
  and a sweep across every position kind, with a `main` control that fails.
- **Why.** A nil check where #749's crash fired would have left `compile.Types` claiming every
  resolved coordinate has a node, and another reader would have turned the crash into a false
  internal-invariant error; the type gained `Building` beside `NodeAt` instead (#564, #749).
- **Probe.** `git grep` each reader of the state's accessors and fields; the fix changes what
  every one of them reads, not one call site.

## Retarget everything that names a renamed or deleted symbol
`sweep-old-name-after-rename-or-delete`

- **Rule.** When a change renames, moves, deletes or replaces a function, type, table or mechanism,
  rewrite in the same change every doc comment, test name, package doc, `docs/` page and skill
  that names the old symbol or describes the old mechanism. Grep the old token as a plain
  substring, not a whole word, and a renamed term with its article ("a emitter").
- **Why.** Comments pointed at deleted functions, removed receivers and replaced resolvers, sending
  readers to code that no longer exists (#195, #834).
- **Probe.** For each name the diff removes, `git grep -nF '<old>'` over `*.go`, `docs/`,
  `README.md`, `CLAUDE.md` and `.claude/skills` on the final tree; each hit outside the diff's own
  deletions is a finding. A design-stated move takes
  `references/sweep-every-site.md#sweep-a-design-stated-move`.

## Sweep a moved import path in its escaped spelling too
`moved-import-path-escaped`

- **Rule.** When a change moves an import path, grep its regex-escaped spelling as well as the plain
  one: `.golangci.yml` and other pattern lists write `go\.yaml\.in/yaml/v3`, which a substring grep
  for the plain path misses.
- **Why.** #1116 moved the YAML import path across the module, and lint then failed on `exhaustive`
  because `ignore-enum-types` still named the old path escaped, until a third commit pointed it at
  the new one (#1116).
- **Probe.** `git grep -nF 'go\.yaml\.in' -- .golangci.yml` prints the `ignore-enum-types` line,
  and `git grep -nF 'go.yaml.in' -- .golangci.yml` prints nothing.

## Sweep the spellings of a declaration a design document moves
`sweep-a-design-stated-move`

- **Rule.** A move a design document states counts as a move though the diff removes no name. List
  each moved declaration's spellings in other units — qualified by the unit it left
  (`git grep -nE '(^|[^A-Za-z0-9_])<old unit>\.(<names>)'`) and unqualified in another unit's sample
  or signature (`git grep -nE '[(*\[ ](<names>)[\[(,)]' -- docs`) — and read each hit against the
  new placement.
- **Why.** #1737 moved runtime types to `<root>/runtime` while `docs/` kept 9 `sdk.*` spellings at
  `c463b8253` and 4 unqualified signatures (`*RawBody`, `*Response[...]`) at `55d067d21`, with 0
  hits in the diff (#1747).
- **Probe.** Run both greps at `c463b8253` and `55d067d21`: 9 and 4 hits where diff greps find 0.

## Rewrite every comment a behaviour change makes false
`sweep-comments-a-behaviour-change-falsifies`

- **Rule.** A change to what code does (an effect turned into a return value, a new refusal or error
  class, a new caller, a retired premise) sweeps its package for comments stating the old behaviour,
  above all those saying only, never, the single, records or reports. Re-check each precondition a
  doc comment states against every call site, new ones included.
- **Why.** Converted functions still said they "record" diagnostics they now return, and "the single
  append point" survived the change that made it false (#198, #213); two examples quoting #1309's
  input outlived its fix, matching none of those words (#1642).
- **Probe.** `git grep -nE 'only|never|single|the one|records|reports'` in the touched packages, and
  `git grep -nF '<input>'` over the tree for each input whose result changed; read each hit against
  the new code; list callers for each precondition.

## Sweep the tree for prose stating a field's old meaning
`sweep-prose-a-meaning-change-falsifies`

- **Rule.** A change to what a field or a value means falsifies sentences stating the old meaning,
  in any package and in `docs/`, the changed document's own code blocks included, which need none of
  the words `references/sweep-every-site.md#sweep-comments-a-behaviour-change-falsifies` greps for.
  Grep the whole tree for the old meaning's words, the field's name among them, in the order a
  sentence states them, and read each hit's sentence against the new meaning.
- **Why.** #1370 made `Provenance.Source` name the patched document rather than the overlay, and
  three sentences crediting the overlay outlived several review rounds, one in
  `docs/architecture.md`, outside the touched packages (#1680). An amended lookup in
  `docs/emitters/contract/type-table.md` kept its old comparison in its own Go block (#2241 at
  095e4ba4f).
- **Probe.** `git grep -nE '<pattern>' -- . ':!testdata'`, as
  `(credit|Source).*overlay|overlay.*(credit|Source|index)|fallback` hit all three at `f060e417a`.

## Update the normative doc a change makes untrue, in the same change
`update-normative-docs-in-same-change`

- **Rule.** When a change ships a lowering, diagnostic code, `Unmodeled` key, gate step or
  capability, update in the same change the `docs/` row, matrix cell, README section or status line
  that describes it, and re-check every clause of that row, not only the one the fix named.
- **Why.** When the docs lag the code (a missing warning, an unnamed key, "not yet implemented"
  beside landed rows, a matrix cell denying client streaming the PR enabled), contributors build
  against a false spec (#310, #345, #352).
- **Probe.** For each new code, key or step, `git grep` `docs/`, `README.md` and `CLAUDE.md` for a
  neighbour and confirm the new one sits beside it. A widened pass or code takes
  `references/sweep-every-site.md#widened-reach-sweeps-the-docs`.

## Sweep every summary when a change widens an existing pass or code
`widened-reach-sweeps-the-docs`

- **Rule.** A change that widens what an existing pass or diagnostic code reports sweeps every
  summary of its old reach, not only newly added codes. Grep `docs/`, `README.md` and `CLAUDE.md`
  for each sentence naming the pass or code (backticked or bold), and read each hit against the new
  reach.
- **Why.** #1759 widened `names` (`name-collision`) while three summaries (`README.md`,
  `docs/architecture.md`, `pass/names.go`) kept "any target may render alike" across four rounds
  (`1d5b2bd69`; the package comment sweep's 212 lines held none of them, #1764).
- **Probe.** `git grep -nE 'any target may render alike' 1d5b2bd69 -- .` hits all three stale
  summaries.

## Find the guards a removed state served by coverage, not by name
`removed-state-guards-by-coverage`

- **Rule.** When a change makes a value's zero or empty state invalid, convert the tests off it,
  then profile every package whose tests built that state,
  `go test -covermode=set -coverprofile=<f> <packages>`, and read each statement left uncovered: a
  guard serving only the removed state is spelled on whatever name the value has where it is
  checked, a helper's parameter in another package among them, which a grep for the field misses.
- **Why.** #1646 made a lowering context's zero value invalid; a grep for its guards missed a
  `doc == nil` check on a helper's parameter, found only when that package's own coverage fell
  below full (#1646).
- **Probe.** `go run ./cmd/morphic-covergate <f>` lists the statements left uncovered.
