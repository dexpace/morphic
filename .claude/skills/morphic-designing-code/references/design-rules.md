# Design rules

How new code keeps scaling with the document, takes the next format and emitter without a rewrite,
and stays simple. Each rule is a cost a later refactor or performance change had to pay.

## Write a function of what it reads, not a method on a growing struct
`functional-core`

- **Rule.** A lowering, check, pass or helper is a function of its inputs (a context, the document,
  its options) returning its value and diagnostics (in `irverify`, its violations). A run's
  accumulators are locals of the function owning it, bar
  `references/design-rules.md#shared-run-state`. A method belongs on a type only when it reads or
  guards that type's own state, and a type nearing the method cap is split by the state its methods
  read.
- **Why.** The OpenAPI lowerer reached 159 methods on 13 fields without one long function, and
  dissolving it into functions of an immutable context ended in #217. The cap that now holds it
  counts `compilers/` alone (#1159).
- **Probe.** For a type outside `compilers/`, against `maxMethodsPerType`:
  `git grep -hE '^func \(([[:alnum:]_]+ )?\*?<Type>(\[[^]]*\])?\) ' -- ':(glob)<dir>/*.go' ':!*_test.go' | wc -l`.

## Share a run-wide holder through one pointer, and say what it changes
`shared-run-state`

- **Rule.** A run's accumulators are locals of the function owning the run
  (`references/design-rules.md#functional-core`), unless every walk must reach one, as a memo of
  answers or a record read once the walks return. Such a holder sits behind one pointer in the
  context, and its field says why copies share it and whether it can change what another lowering
  reads. One that cannot is preferred; one that can, as a budget counted down across walks does,
  says which answer, and the context's documentation stops calling it immutable.
- **Why.** The rule named none of the holders `lowering.Ctx` already shared, and a context
  documented as immutable gained one that changed an answer (#1436).
- **Probe.** `awk '/^type (Ctx|shared|loaded|Promoter) struct/,/^}/' compilers/openapi/internal/{lowering,service,promotion}/*.go | grep -E '^[[:space:]]+[[:alnum:]_]+ '`
  lists their fields, each holder among them read for both statements.

## Read a document once per run, through a shared walk or index
`read-once-per-run`

- **Rule.** A new check, lowering or pass needing a whole-document fact reads a walk or index built
  once per run, adding a visitor to `ir.WalkValues`
  (`references/design-rules.md#prune-a-visitor-to-its-carriers`) or a field to the run's index,
  never its own scan per query; a remembered reading takes
  `references/design-rules.md#remembered-reading-keys-what-it-reads`. Compute a fact once, at its
  owning level: a path item's, not per operation it mounts.
- **Why.** Each repeat was removed later: two parses and four walks per compile (#340), seventeen
  walks per `irverify.Verify` (#1125), two walks building one list (#290), a scan per pointer hop
  making resolution quadratic (#379), and per-operation path item entries (#414).
- **Probe.** Count walks per run
  (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#counting-seam-for-optimizations`)
  and work at n, 2n and 4n
  (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#count-work-not-wall-clock`).

## Key a remembered reading on everything it reads that can vary
`remembered-reading-keys-what-it-reads`

- **Rule.** A remembered reading's key (`ReadOncePerRun` in
  `compilers/openapi/internal/lowering/lowering.go`) holds everything the reading reads that can
  differ between callers: the context's document, and through it its dialect, as `admissionsIn` in
  `compilers/openapi/internal/schema/admission.go` keys admissions by the asking dialect.
- **Why.** #1996 keyed an `allOf` decision by schema alone while reading `c.SchemaRules()`, leaking
  the first caller's dialect across documents (#2093).
- **Probe.** List what the reading and its callees read off the context (`git grep -n 'c\.'`) and
  confirm each item that can differ between callers sits in the key.

## Read another node's source schema through bindings, not the registry
`read-the-source-not-the-registry`

- **Rule.** A lowering deciding one node's fields from another node reads that node's source schema
  through the bindings, not its registry node (`Node`, `NodeAt` in `compilers/compile/types.go`),
  except the position's own node built before the read.
- **Why.** A referenced node registers when its own declaration lowers or its build returns, so
  reading the registry made #1977's draft `order-dependent` on a `$ref` cycle (`U`/`C`/`D`/`H`)
  (#1978).
- **Probe.** `go run ./cmd/morphic-harness <spec>` over a spec declaring the read node after the
  reader, and over #1978's `$ref` cycle.

## Prune a visitor to the path to its carriers
`prune-a-visitor-to-its-carriers`

- **Rule.** A visitor reading a type with few carriers returns false off the path to them, as
  `checkStreamingModes` in `ir/irverify/kinds.go` does, rather than being shown every value, as
  `checkInferred` is because every `Provenance` holds its field. The pruning changes no finding, so
  it takes a test failing on a carrier off the path and a counting seam showing the values visited.
- **Why.** `checkStability`, written in `checkInferred`'s shape for a type only operations and
  their groups carry, made `irverify.Verify` over the golden spec about 4% slower than the pruned
  walk (#1229), and two more checks had the same shape (#1267).
- **Probe.** A document holding more beside the carriers shows the visitor the same number of
  values.

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

## Name a declaration for what it holds, in the specification's words
`name-what-it-holds`

- **Rule.** A name is long, literal and plain: the specification's term where it has one
  (`foldingAllOfEntries`, not `voicesAmong`), the kind of thing it declares (`Measurements`, not
  an `Index` that indexes nothing), a question for a bool (`HasFilename`), and its type's noun for a
  parameter (`schema`, not `s`). One letter names only a loop counter, a receiver, a comparison
  pair or a standard-library convention (`t *testing.T`). A figurative word, an abbreviation or a
  noun another package uses otherwise is renamed, not explained in a doc comment.
- **Why.** Names that needed their comments, and comments that needed other coined names, left
  the code and the compiled IR unreadable to their owner (#2130).
- **Probe.** This lists the one-letter parameters a change adds:
  `git diff origin/main -- '*.go' | grep -nE '^\+func [A-Za-z][^(]*\((.*[, ])?[a-z] [*A-Za-z.]+[,)]'`.

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

## Weigh a maintained tool's feature before a hand-rolled mechanism
`maintained-mechanism-first`

- **Rule.** Before adding or growing a mechanism (a script, a prescribed command or pipeline, a CI
  step, a Makefile target, custom checker code), name the maintained feature that could do the job:
  a tool flag, a repository script or Go binary feature, a linter entry or a CI check. Take it
  unless it cannot serve, and say why in the PR. Replace, never patch, a mechanism needing extra
  rounds for quoting, signals, word splitting or network state.
- **Why.** #1532 rejected a review-facts script, #1233 used `forbidigo` over custom code, and #2292
  replaced three drafted mechanisms with `go test -timeout`, `golangci-lint --new-from-merge-base`
  and existing `subjects.py`.
- **Probe.** The PR names the feature weighed; grep the diff for `perl -e`, `python3.* -c`, `xargs`
  and `while read` and hold each hit to that answer.

## A function crosses into another package only for a reason
`function-crosses-for-a-reason`

- **Rule.** A function one package hands another, as a field or a parameter, stays a function only
  for I/O, a policy the callee needs inside its logic but must not know, a live or lazily built
  query over changing state, a seam tests substitute, or a traversal or immediate callback. Any
  other becomes the data the caller holds, a named type with methods in the lowest package needing
  them. One that stays is a named type's method value, so its construction names its
  implementation.
- **Why.** `merge.Merger` relayed function fields its tests stubbed, one of them `Resolve`, a map
  read in production and in its stub alike, where a map served both (#1477, #1486).
- **Probe.** `callShapeBaseline` in `internal/archtest` counts calls into packages a package does
  not import; each new one is argued here.

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
  length, count times size or references times what they reach, a bound per dimension leaves the
  product free: bound the product, or the steps or bytes spent, through
  `compilers/openapi/internal/budget` in the OpenAPI compiler.
- **Why.** A 4 MB document of about 1,000 nodes a few hundred levels deep, inside every bound, made
  a compile peak at 8.2 GB, spelling each node's pointer with every key above it (#931).
- **Probe.** At a fixed input size, grow one dimension while shrinking the other, on `main` and the
  branch, as #931's table does, and take `references/design-rules.md#charge-each-part-to-its-cost`.
  A new recursion under `compilers/openapi` also joins `loweringRecursions` in
  `internal/archtest/recursion_test.go` with its bound
  (`.claude/skills/morphic-checking-style/references/departures.md#bounded-recursion-stands`);
  whether the pin covers other packages is #1525.

## Hold each part of a charge to the cost it stands for
`charge-each-part-to-its-cost`

- **Rule.** For each part of what a bound's charge weighs, grow that part alone, on `main` and on
  the branch. A charge growing while the cost stays flat refuses a valid document for nothing, and
  a cost growing while the charge stays flat is a copy left unbounded. Read a survey of cost per
  unit of charge at both ends: the low end is a charge standing for almost no cost.
- **Why.** A `$ref` budget's first revision charged at every use a response's inline schema, which
  lowers once, refusing 400 `$ref`s `main` compiles, and charged nothing for an alias's own fields,
  which copy at every use, through four rounds reading the survey's high end (#1430).
- **Probe.** Grow each part alone with the rest of the document fixed: charge and cost move
  together.

## Share one implementation only among sites that mean the same
`share-only-what-means-the-same`

- **Rule.** Before sites share one implementation, run them over one table of edge cases and show
  they agree: the library reading each mirrors, what each reports, its bound, its failure. Share
  what agrees, in the lowest package its users may import; keep apart what differs by design, saying
  why in a comment
  (`.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md#parallel-implementations-agree`).
  Sharing pays with two users in the tree and a form no harder to read than each copy; a planned
  user decides where code lives, not whether it is built.
- **Why.** Three scalar hoisters differed in three inputs, so #147's missing constraints were fixed
  in each (#834). Of five reference-cycle checks, one chain walk was owned once and two YAML guards
  stayed apart, as sharing would change what they report (#789).
- **Probe.** Diff every site's results over the shared table.

## Decide a fact once, or ask the code that decides
`decide-once`

- **Rule.** Decide a fact one phase sets and another reports or acts on once, recording it on the
  value read later or asking the deciding code; a phase that cannot says why in a comment. Call
  library readings (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#reuse-library-readings`,
  `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#override-a-library-misreading`);
  a mirror stays only where no library call answers, held by a differential
  (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#mirror-library-read-rules-exactly`,
  `.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#acyclic-trees`).
- **Why.** `disjoint` in `compilers/openapi/internal/schema/decision.go` parted from `disjointAtoms`
  in `compilers/openapi/internal/schema/allof_disjoint.go` (#2077); `declaresPositionScoped` drifted
  thrice (#138, #1053, #2009); #1016 and #2132 deleted copies (#2280).
- **Probe.** Find each predicate behind a message or later phase (`git grep -n`) and compile a grid
  over kinds, value sets and formats (#2077's `EnumEnum`/`ConstConst`/`StrEnum`).

## Implement a specification rule once across all spellings
`one-rule-one-implementation`

- **Rule.** A lowering rule the specification states once for every place it can be written is
  implemented once, each spelling reaching it. An implementation per spelling leaves mixed spellings
  to none of them.
- **Why.** Separate `oneOf`/`anyOf` lowerings for standalone schemas, beside `allOf` and on `allOf`
  entries left a `oneOf` beside an `allOf` whose partner sat on an entry to fall between them
  (#2110, #2129).
- **Probe.** Write the mixed spelling (a `oneOf` beside an `allOf` whose partner sits on an entry)
  and compile it beside each pure spelling.

## Fold several declarations of one thing from the whole set
`fold-the-whole-set`

- **Rule.** A merge of several declarations of one thing chooses from all of them at once, never
  folding each into what the ones before it left; refuses values the result cannot admit before any
  first-wins tiebreak; reports every drop; and bounds any pairwise judgement it makes. Sweep it with
  three declarations in all six orders, as pairs cannot show a chain's order dependence.
- **Why.** #1000's allOf merge first folded a field's declarations as a chain, so
  `int32, uint32, uint16` reported an overlap and kept `uint32` verbatim while
  `uint16, int32, uint32` reported nothing; a 741-pair sweep was clean, and only triples showed it
  (#1000).
- **Probe.** The triple sweep in every order gives each result and report alike, up to the ties
  the merge reports.

## Refuse a callback's whole operation through one choke point
`refusal-inside-a-callback`

- **Rule.** An API that records refusals for a later report and runs a caller's callback refuses
  the enclosing operation when anything inside the callback is refused, marked where every refusal
  passes (as `refuse` in `ir/edit.go` does) and cleared by `defer`; loses nothing to a report or
  drain called inside the callback; and rolls back, not only reports, a bad state found late.
- **Why.** `ir.Editor`'s change callback applied an update whose own write was refused, a drain
  inside it discarded earlier refusals, a node moved off its key was reported but kept, a mark set
  at some refusal sites missed a call refusing earlier, and a panicking change left the mark set
  (#1097).
- **Probe.** Each case against a control edit making no misuse: the update is absent and its
  refusal reported.

## A reused reader's fallback must suit its new caller's decision
`fallback-suits-the-decision`

- **Rule.** When a new caller reuses a reader, name the decision its fallback (its answer for what
  it does not read) was chosen for, and the caller's. Where the fallback is unsafe for the caller,
  the reader reports that it fell back, as `Budget.Exhausted` reports a cut, and the caller takes
  its own safe answer.
- **Why.** `conjunctTypes` answers `anyInstance` for what it skips, keeping
  `allof-disjoint-conjuncts` silent. #1656 reused it as `Merger.Untyped` (now `Walk.Untyped`), where
  `anyInstance` let `{allOf: [{$ref: NotStr}], properties: …}` beside `{type: string}` lower to a
  bare string past three rounds (#1673).
- **Probe.** Through the new caller, compile one input per construct the reader does not read
  (`references/design-rules.md#reused-reader-past-its-bound`), checking each verdict against the
  source.

## Probe a reused reader past every bound it holds
`reused-reader-past-its-bound`

- **Rule.** A reader reused through a new caller is also compiled with an input at N and N+1 of
  every bound it holds, since its answer past the bound is a fallback chosen for its first caller.
- **Why.** On #1977, reusing the 64-type composition walk missed a 65-level hierarchy taking the
  base's description over the leaf's through eight review rounds (#1983).
- **Probe.** Through the new caller, compile inputs at N and N+1 of each bound the reader holds and
  check each verdict against the source.

## Keep code that needs no morphic concept apart from the domain
`domain-free-code-lives-apart`

- **Rule.** Code needing no morphic concept, such as a bounded count, a panic barrier, a graph walk
  or a URL or pointer grammar, names no IR or format type and lives in a package named for what it
  does, never `utils`, `helpers` or `common`. It stays under its first user's `internal/` until a
  second tree needs it, then moves down a layer under its own `internal/archtest` entry. Generic
  means free of a domain it does not need, not parameterized for users it lacks.
- **Why.** The compiler's bounds were a dozen hand-written counters until one type counted them all
  (#785, #1140), and five panic barriers rendered what they caught each its own way until
  `barrier.Catch` alone did (#1130).
- **Probe.** List the domain types each new declaration names; one naming none is a candidate.

## Keep build constraints out of production files
`no-build-constrained-production-file`

- **Rule.** Production code holds no file only some systems build (`//go:build`, `_<goos>.go`), as
  `internal/decls` reads packages whole (#409). Select by a value tests pass per system, as
  `imagePath` in `engine/cache/build.go` does, and run `GOOS=<os> go vet` per target. Where only a
  constrained file reaches an API, the tracker decides (#1318).
- **Why.** OS-tagged production files passed `go build` and package tests, then failed
  `TestInterfaceAssertions_CoverEveryExportedPair` and `TestResolutionCalls_HeldToTheBaseline`
  (#1839).
- **Probe.** Add a production file in `engine/cache` opening with `//go:build never` (like
  `internal/decls/testdata/constrained/left_out.go`) in a throwaway tree:
  `go test ./internal/archtest -run 'TestInterfaceAssertions|TestResolutionCalls'` fails.
