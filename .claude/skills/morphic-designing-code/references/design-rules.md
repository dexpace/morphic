# Design rules

How new code keeps scaling with the document, takes the next format and emitter without a rewrite,
and stays simple. Each rule is a cost a later refactor or performance change had to pay.

## Write a function of what it reads, not a method on a growing struct
`functional-core`

- **Rule.** A lowering, check, pass or helper is a function of its inputs (an immutable context,
  the document, its options) that returns its value and its diagnostics (in `irverify`, its
  violations). A run's accumulators are locals of the function that owns the run. A method belongs
  on a type only when it reads or guards that type's own state, and a type nearing the method cap is
  split by the state its methods read.
- **Why.** The OpenAPI lowerer reached 159 methods on 13 fields without one long function, and
  dissolving it into functions of an immutable context ended in #217. The cap that now holds it
  counts `compilers/` alone (#1159).
- **Probe.** For a type outside `compilers/`, against `maxMethodsPerType`:
  `git grep -hE '^func \(([[:alnum:]_]+ )?\*?<Type>(\[[^]]*\])?\) ' -- ':(glob)<dir>/*.go' ':!*_test.go' | wc -l`.

## Read a document once per run, through a shared walk or index
`read-once-per-run`

- **Rule.** A new check, lowering or pass that needs a fact about the whole document reads it from
  a walk or index built once per run, adding a visitor to `ir.WalkValues` or a field to an index
  the run builds, never a walk or scan of its own per check or query. A fact is computed once, at
  the level owning it: a path item's, not once per operation it mounts.
- **Why.** Each repeat was found and removed later: two parses and four walks per compile (#340),
  seventeen walks per `irverify.Verify` (#1125), two walks building one list (#290), and a scan per
  pointer hop that made resolution quadratic (#379). #414 is still open.
- **Probe.** Count walks per run through a seam
  (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#counting-seam-for-optimizations`)
  and the work at n, 2n and 4n
  (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#count-work-not-wall-clock`).

## Give a value with a form a type of its own
`type-the-form`

- **Rule.** A value with a form, such as a pointer, a joined list, a locator of several kinds,
  "none" or "no bound", gets a type: a constructor that checks the form, a field per kind, and an
  absent state the type spells (comma-ok, or a meaningful zero), never a string or an int with a
  convention. In the IR, `irverify` checks the form too.
- **Why.** Each convention was replaced once it broke: pointers built and split by hand (#515),
  three locator kinds in one string the CLI misprinted (#549), a marker list stored malformed and
  unverified (#1142), and "no bound" spelled four ways beside `-1` for none, two of whose
  disagreements were bugs (#1140).
- **Probe.** This lists what to type or justify:
  `git diff origin/main -- '*.go' ':!*_test.go' | grep -nE '^\+.*((return|[=!]=|:?=) -1\b|strings\.(Split|Join)\()'`.

## Place shared code by its second user
`second-user`

- **Rule.** Before code enters a layer every format or emitter shares (`engine`, `compilers`,
  `compilers/compile`, `ir`, `pass`), name how a second format that is not YAML, or a second
  emitter, would call it. What fits only the first stays in its compiler. A duty every compiler
  owes is stated on `compilers.Compiler` and checked where its data is, and an identity every
  compiler must reach is derived in `ir`.
- **Why.** The engine's format detection knew two YAML keys while three planned compilers read no
  YAML (#341), a compiler's duties were checked for the OpenAPI compiler alone (#1038), and the
  shared primitive's ID was derived where nothing outside this repository's compilers could be held
  to it (#239).
- **Probe.** The PR body names the second user's call, or the format column of
  `docs/ir-spec-matrix.md` that needs the code.

## Build for the callers there are: no forwarding layer, no unused seam
`no-forwarding-layer`

- **Rule.** Add no wrapper, re-export or helper that only forwards a call or supplies one argument:
  call the function. Add an interface only for a second implementation or a seam a test uses, and
  an option only with a caller that sets it. The second user decides where code goes, not what is
  built before it exists.
- **Why.** Three of the hoisting helpers' six methods were one-line delegations (#195), four
  wrappers only supplied the source index and swallowed diagnostics (#199), and a re-export gave one
  call two spellings and a false dependency (#1100). Members only tests reached were removed, and
  are now refused (#1122).
- **Probe.** Each new function does more than call another, each new interface has two
  implementations or a test seam, and `go test ./internal/archtest` passes.

## Count on `barrier.Catch` only for what it reaches
`catch-reaches-one-goroutine`

- **Rule.** Before relying on `barrier.Catch` to contain code it runs, read what `barrier/doc.go`
  says it cannot catch and, for the parser library, `docs/parser-library.md#goroutine-per-node`.
  What it cannot reach is refused before the call, or run in a child process.
- **Why.** A `$ref` cycle overflowed the stack, a fatal error no recovery traps, so it had to be
  prevented before the parser ran (#19).
- **Probe.** Plant the failure on a goroutine the called code starts, in a child process as
  `TestLibraryBehaviour` does, and read how the process ended.

## Bound the quantity the work grows with
`bound-the-product`

- **Rule.** A limit caps what the work grows with. Where cost is a product, such as depth times key
  length or count times size, a bound per dimension leaves the product free: bound the product, or
  the steps or bytes spent, through `compilers/openapi/internal/budget` in the OpenAPI compiler.
- **Why.** A 4 MB document of about 1,000 nodes a few hundred levels deep, inside every bound, made
  the parser peak at 8.2 GB (#931).
- **Probe.** At a fixed input size, grow one dimension while shrinking the other, on `main` and the
  branch, as #931's table does.
