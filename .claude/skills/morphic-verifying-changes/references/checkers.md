# Checks that reach what they claim

Source scanners, lint and archtest rules, and loaders. A check is evidence only for the forms it can
see, so each rule here is about widening what a check sees or proving how far it reaches. The IR's
own verifier and codecs are in `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md`.

## Key static source checks on every syntactic form, ideally the resolved type
`source-scanners-match-every-form`

- **Rule.** An AST or grep check over Go source must match every form the language and codebase use:
  value and pointer receivers, method values and expressions, instantiated callees (`f[T]()`,
  `v.M[T]()`), functions passed as values, aliases, every selector base, go statements and .Go
  calls, every write form (a `range` clause's `=` too), every constructor of an identity type, and
  every import of a path, blank, named or dotted, bar a spelling `gofmt` rewrites, which `make fmt`
  refuses. Plant them by `references/checkers.md#plant-each-form-by-each-route`.
- **Why.** Pins missed value receivers and method values, a grep audit both planted writes (#332,
  #837), a blank `os/exec` import hid a named one, a `range` clause passed a check of assignments
  (#1233, #1252), and an analyzer meeting every form then listed missed `(*T).Add(v)` and read
  `v.G[int]()` as a method value (#1447, #1549).

## Plant each form by each route a check follows to it
`plant-each-form-by-each-route`

- **Rule.** Cross each form `references/checkers.md#source-scanners-match-every-form` lists with
  each route the check follows to it: directly, through an alias, and inside another form, as a
  walk can stop early. Plant every sibling a branch lists, not one standing for the rest. Key on
  go/types over selectors. Where the plants may stop is
  `references/mutation.md#plants-stop-at-a-held-grid`.
- **Why.** A fixture planted `delete`, `clear`, `copy` and `append` directly and only `delete`
  through an alias, so a mutant stopping the other three through an alias passed it, as did one
  dropping arrays from the ranged types (#1447, #1586).
- **Probe.** In a scratch package each cell's plant draws a finding, and removing the check's
  handling of one route reddens that route's plants.

## A guard on how a value is built must see what undoes the build
`build-guards-see-later-writes`

- **Rule.** A source check guarding how a value is built also matches a later write that resets what
  the build set, the forbidden value in each argument of an `append` or other variadic call, and a
  composite literal with an elided type.
- **Why.** A guard on how a command is built passed each of these in turn (#1233). `os/exec` keeps
  a key's last occurrence, so `cmd.Env = append(cmd.Env, os.Environ()...)` hands the parent's
  values back.
- **Probe.** Plant the forbidden value after the build and in every argument position, and confirm
  each reddens.

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

## A count over a whole-program analysis takes a fixed input set
`analysis-input-is-fixed`

- **Rule.** A check counting over a whole-program analysis takes an input set the program
  determines, such as RTA's reachable set from fixed roots, never one a library derives from map
  order, as `ssautil.AllFunctions` does. Root a check over production code where production
  starts: each binary's `main` and the API kept for a caller to come, never a function only tests
  call.
- **Why.** One program's `AllFunctions` gave 45,411 to 45,638 functions on successive calls, and a
  call graph rooted at every declared function credited a test-only helper with seven calls no
  binary makes (#1504).
- **Probe.** Run it four times on one program and once on several builds, and compare the input
  set's size and a digest of the result; then diff the result against one rooted at production's
  entries alone.

## Validate a source-reading checker against history
`history-sweep`

- **Rule.** A new check that reads the source (a shape description, a declaration scan) is run,
  built from the branch, at each past commit that changed what it reads: its working directory in a
  detached worktree at that commit. It must change exactly where history did. For a check reading
  all production source, which nearly every commit changes, run it where a diff holds the construct
  it reads (`git log --first-parent -G '<construct>' --format=%h origin/main -- '*.go'`), plus a
  sample the report calls one.
- **Why.** The IR shape describer changed at every version bump and at no refactor, and found one
  unbumped shape change (#985). A lint rule over every package met 398 first-parent commits, and
  its review sampled five with no rule saying how to choose (#1447, #1550).

## A new gate target draws a finding planted for it
`gate-target-draws-a-plant`

- **Rule.** A new gate target, CI step or flag turning a check on carries a planted finding it
  must report, and the gate fails when it does not, as `make vet` requires the finding planted in
  `internal/lintrules/testdata/reach`. A target that runs and passes looks the same whether or not
  its tool is on and meets the defect.
- **Why.** `make race` ran `-race` over a listed set of packages with no plant, so dropping `-race`
  or merging coverage back into it left the gate green until a planted race was added (#1346).
- **Probe.** Remove the flag, or empty the target's input: the gate goes red.

## A probe of a lint rule lists every finding
`lint-probe-lists-every-finding`

- **Rule.** Run a lint rule's probe with `--max-same-issues=0 --max-issues-per-linter=0`: by
  default golangci-lint reports at most three findings of one text and fifty of one linter, and
  says nothing of the rest, so planted lines it catches read as missed. `make lint` keeps both
  defaults, so fixing what it shows can turn up more; whether it should list every finding is
  #1526.
- **Why.** Five planted `exec.Command` lines drew three `forbidigo` findings by default and five
  with the first flag (#1473); sixty planted `os.Remove` calls drew fifty `errcheck` findings with
  it alone and sixty with both (#1522 at 3b4d18424).
- **Probe.** Over sixty planted lines,
  `golangci-lint run --enable-only <linter> --max-same-issues=0 --max-issues-per-linter=0 <packages>`
  reports sixty.
