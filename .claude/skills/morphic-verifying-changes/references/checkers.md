# Checks that reach what they claim

Source scanners, lint and archtest rules, and loaders. A check is evidence only for the forms it can
see, so each rule here is about widening what a check sees or proving how far it reaches. The IR's
own verifier and codecs are in `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md`.

## Key static source checks on every syntactic form, ideally the resolved type
`source-scanners-match-every-form`

- **Rule.** An AST or grep check over Go source must match every form the language and codebase use:
  value and pointer receivers, method values, functions passed as values, aliases, every selector
  base, go statements and .Go calls, every write form and every constructor of an identity type.
- **Why.** Recursion and dependency pins went blind to value receivers and method values, and a grep
  audit reported clean while missing both planted writes (#332, #837).
- **Probe.** Plant one instance of each form the check must catch in a scratch package and confirm
  each reddens; prefer keying on go/types over selector text.

## Identify code with the type checker, never by name strings
`resolve-symbols-semantically`

- **Rule.** Match functions and packages by types.Object identity and the file's own import
  declarations (including aliases), and measure structure (call cycles, method counts) with a
  parser, cross-checked by a second method.
- **Why.** Name-string matching missed aliased imports and flagged unrelated packages, and a regex
  roughly doubled a mutual-recursion count and sized a plan on it (#801, #203).
- **Probe.** Add an aliased import and an unrelated same-named package to the guard's fixtures;
  recompute any reported metric with go/ast or go/types and compare.

## Match audit paths exactly, and plant a sibling to prove it
`archtest-rules-reach-exactly`

- **Rule.** An allowlist or skip rule must match full paths exactly unless a wildcard is written,
  and every in-module import -- including testdata fixtures under an audited tree -- is part of the
  graph the rules must cover.
- **Why.** A segment-prefix match let a planted sibling of compilers/openapi pass under its
  allowlist entry, a basename skip left nested irtest/irverify directories audited by nothing, and a
  testdata fixture's sibling import was missing from the allowlist (#182).
- **Probe.** Plant a forbidden import from a package sharing the allowed prefix, a same-named
  directory at another depth, and a testdata fixture importing a sibling; run go test
  ./internal/archtest and confirm each reddens.

## Fail when a loader or parser reads less than it was asked to
`loaders-fail-when-they-read-less`

- **Rule.** A source loader, package loader or document parser errors when its result is smaller
  than its input: a pattern matching nothing, a build-excluded file, a test-only package, a line it
  cannot place. A reader of a format reads every spelling the format admits, such as tilde and
  backtick fences and tables without outer pipes in Markdown.
- **Why.** Checkers reported clean on code they never saw, and the matrix reader dropped every row
  below an unexpected line (#837, #342).
- **Probe.** Feed the loader a pattern matching nothing, a build-tagged file, a test-only package
  and a malformed line mid-table, and assert each errors rather than returning less.

## Validate a source-reading checker against history
`history-sweep`

- **Rule.** A new check that reads the source (a shape description, a declaration scan) is run,
  built from the branch, at each past commit that changed what it reads: its working directory in a
  detached worktree at that commit. It must change exactly where history did.
- **Why.** The IR shape describer changed at every version bump and at no refactor, and found one
  unbumped shape change (#985).
