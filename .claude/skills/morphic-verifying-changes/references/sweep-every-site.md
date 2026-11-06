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
  deletions is a finding.

## Rewrite every comment a behaviour change makes false
`sweep-comments-a-behaviour-change-falsifies`

- **Rule.** A change to what code does (an effect turned into a return value, a new refusal or error
  class, a new caller, a retired premise) sweeps its package for comments stating the old behaviour,
  above all those saying only, never, the single, records or reports. Re-check each precondition a
  doc comment states against every call site, new ones included.
- **Why.** Converted functions still said they "record" diagnostics they now return, and "the single
  append point" survived the change that made it false (#198, #213).
- **Probe.** `git grep -nE 'only|never|single|the one|records|reports'` in the touched packages and
  read each hit against the new code; list callers for each precondition.

## Update the normative doc a change makes untrue, in the same change
`update-normative-docs-in-same-change`

- **Rule.** When a change ships a lowering, diagnostic code, `Unmodeled` key, gate step or
  capability, update in the same change the `docs/` row, matrix cell, README section or status line
  that describes it, and re-check every clause of that row, not only the one the fix named.
- **Why.** When the docs lag the code (a missing warning, an unnamed key, "not yet implemented"
  beside landed rows, a matrix cell denying client streaming the PR enabled), contributors build
  against a false spec (#310, #345, #352).
- **Probe.** For each new code, key or step, `git grep` `docs/`, `README.md` and `CLAUDE.md` for a
  neighbour and confirm the new one sits beside it.
