# Sweep every site the change touches

A defect is a property of its mechanism: sweep every site of it and every sentence that describes
it.

## Fix the mechanism at every site that runs it
`sweep-every-site-of-the-mechanism`

- **Rule.** When a change fixes a defect, adds a guard or closes an issue, name the mechanism, grep
  every site that runs it and every writer of the field it governs, and probe each node kind it can
  land on. Fix each site, or state the uncovered one in code and in the PR body. An issue's own site
  list is a claim, not the scope.
- **Why.** The pointer-collision fix left the same collision in the inline-position hoist; grids
  built from an issue's shapes passed while a type-array variant still failed; a per-site revert
  showed most call sites untested (#157, #101, #808, #542).
- **Probe.** For each hit of the mechanism's grep, show the fix or plant the reproducer there and
  watch it go red.

## Retarget everything that names a renamed or deleted symbol
`sweep-old-name-after-rename-or-delete`

- **Rule.** When a change renames, moves, deletes or replaces a function, type, table or mechanism,
  rewrite in the same change every doc comment, test name, package doc, `docs/` page and skill
  that names the old symbol or describes the old mechanism. Grep the old token as a plain
  substring, not a whole word, and a renamed term with its article ("a emitter").
- **Why.** Comments pointed at deleted functions, removed receivers and replaced resolvers, sending
  readers to code that no longer exists (#195, #834).
- **Probe.** For each name the diff removes, `git grep -n '<old>'` over `*.go`, `docs/`,
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
