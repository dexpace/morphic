# Where cost hides

Reading the code shows neither a quadratic loop nor an escape. These rules say where to look, and
how to measure so the number means what the claim says.

## Measure before quantifying a cost or an effect
`measure-before-quantifying`

- **Rule.** Claim a speedup, memory bound or noise immunity only after a before-and-after
  measurement on the claimed workload, the two taken back to back on one machine. A count bound
  described as a memory bound is measured in retained bytes. Figures go in the PR body with their
  revision; code, help text and docs say it relatively.
- **Why.** A per-walk view was kept for a benefit that never reproduced, and a shared count bound
  let retained memory grow (#379, #362); a cost quoted from runs taken under other load sat in five
  places, a commit body among them (#1119).
- **Probe.** `go test ./<pkg> -run '^$' -bench '<Benchmark>'` and a heap profile after
  `runtime.GC` on the workload, before and after.

## Quote instructions retired, never alone and never under your own load
`instructions-not-wall-clock`

- **Rule.** For a cost claim on a loaded machine, compare instructions retired, run base and head
  alternately, and report each side's median with its spread; quote allocations beside them, but
  never in their place, since an allocation-free path can cost more. Run nothing else of your own
  while measuring.
- **Why.** Replacing `FieldByName` with an allocation-free field scan made `Verify` slower (#1133);
  runs alternating with the author's own test runs showed a gap quiet runs did not (#1140).
- **Probe.** Build `./cmd/morphic` with `go build -o <bin>` in a worktree of the merge base and at
  the head, run the two alternately (`references/finding-cost.md#count-on-one-thread`) under
  `/usr/bin/time -l` on macOS or `perf stat -e instructions:u` on Linux, and compare each side's
  median and range. A run that exits non-zero measured a failure. With neither tool, take
  `references/finding-cost.md#cpu-time-without-a-counter`.

## Count instructions on one thread with the collector off
`count-on-one-thread`

- **Rule.** Set `GOMAXPROCS=1 GOGC=off` for an instruction count over a compile: scheduling and
  collection can move a total past the gap, whichever build they land on, and alternating does not
  average that out. A spec too large to hold uncollected keeps `GOMAXPROCS=1` alone and says so.
  Count each side's readings before quoting a median.
- **Why.** Two builds on one spec read +2.73%, then -2.08%; pinned, the same pair read +2.06% with
  each side's range under 1% (#1205, #1210).
- **Probe.** `GOMAXPROCS=1 GOGC=off /usr/bin/time -l <bin> compile -- <spec>`, three alternating
  runs a side: a range wider than the gap means the count is still noise.

## Without an instruction counter, compare CPU time and say so
`cpu-time-without-a-counter`

- **Rule.** On a host with neither `/usr/bin/time -l` nor `perf`, run base and head alternately and
  compare the user plus sys seconds of `bash -c 'time -p <bin> compile <spec>'`, saying that is what
  was measured. A benchmark is no stand-in for a claim about a spec: `go test -bench` reports wall
  clock, which load skews, and each one reads the fixtures it names.
- **Why.** A Linux host with no `perf` left a review of a cost claim no instrument (#1162).
- **Probe.** `command -v perf`, then each side's range from the alternated runs against the gap.

## Price a stage after the compile from a saved document
`cost-of-a-later-stage`

- **Rule.** A count over a whole compile cannot see a stage the compile dwarfs, such as an
  `ir.Editor` write or a pass, since the compile's own spread is larger. Save the compiled document
  once, have a throwaway program decode it and run the stage, raise the repetitions until the stage
  dominates, and subtract a run that only decodes. Compare each side's range with the gap before
  quoting it.
- **Why.** On the golden spec, reading an operation came to -11.2k instructions a call over a
  compile, whose range spanned 2.09G, and 3.4k over a decode (#1165).
- **Probe.** Run the program with no stage at least three times: a range wider than the gap claimed,
  or a negative cost, means the run measured noise.

## Measure against a base the branch holds
`check-the-base-build`

- **Rule.** A build of `origin/main` holds whatever was fetched last, which can include a change
  merged minutes ago that the branch lacks. Measure against the branch's merge base, or merge
  `main` in first; `sweep.py` warns when HEAD does not hold the base. Before calling a difference a
  regression, read both builds' revisions.
- **Why.** A branch read 33% more CPU than a `main` build that had just gained another PR's
  speedup, which the branch lacked (#1131, #1125).
- **Probe.** `git merge-base --is-ancestor <base> HEAD`; on a false answer, rerun with
  `-b $(git merge-base HEAD origin/main)`.

## Check a cost argument for every caller of the function it is about
`cost-claims-per-caller`

- **Rule.** A claim that a cost is already paid, or a constant factor, is about the callers of the
  function it concerns: list them and check the claim for each, then profile the one it fails for
  against `main`.
- **Why.** A scan justified by the resolver making the same check held for `$ref` pointers, but a
  second caller reading discriminator mapping values made it quadratic (#774).
- **Probe.** `git grep -n '<function>('`, then time the failing caller's shape on both builds.

## Measure a cost on every arm of the switch it runs through
`cost-claims-per-arm`

- **Rule.** When a change sends a loop through a function that branches on its input's kind, one
  input measures one arm. Generate an input per arm, quote the range rather than one figure, and
  name any arm left out with the reason: reading does not find the dear one.
- **Why.** A PR read every enum member through a switch on primitive kind, and one figure, 2.1%,
  stood for every format. Measured per format, `uuid` added 0.15% and `double` 35.45%, by rebuilding
  a constant on each call (#1191, #1205).
- **Probe.** List each arm with the input that reached it, and take each arm's instructions on both
  builds (`references/finding-cost.md#instructions-not-wall-clock`).

## Time generated shapes, deep and wide, on main and the branch
`deep-generated-shapes`

- **Rule.** When a change adds a walk, index or per-position reader, time `main` against the branch
  on generated shapes no corpus holds: a spine a few hundred levels deep with a wide leaf, many
  parallel deep chains, many `$ref`s into distinct entries of one wide mapping, and many `$ref`s to
  one wide component, growing both together. Rerun the reproducers of closed cost issues whose tests
  the change deletes.
- **Why.** A pointer builder quadratic in depth passed a corpus of real specs and the full gate, and
  three closed cost issues regressed with their tests deleted (#1016, #775, #777, #778). Those shapes
  ran linear past a references-by-size product in output bytes, which doubling either alone hides
  (#1191, #1240).
- **Probe.** Instructions retired and output bytes on each shape at n and 2n, on both builds
  (`references/finding-cost.md#instructions-not-wall-clock`).

## Do not take main's fast failure as the baseline for a fix
`failing-fast-is-no-baseline`

- **Rule.** When a change makes a failing resolution succeed, the work `main` skipped by failing is
  now paid: time growing counts of references into one wide mapping against `main`, then the same
  keys split into narrow mappings. To profile, use a throwaway `main` package with
  `GOMAXPROCS=1`, since decoding's goroutine per node (`docs/parser-library.md#goroutine-per-node`)
  scatters a multi-threaded profile.
- **Why.** Chains through another document were quadratic in one wide mapping, while `main` failed
  each after the first and paid nothing (#763, #773).
- **Probe.** Time each layout at n, 2n and 4n; superlinear growth in the wide one alone names the
  key scan.

## Build a pointer once, never by appending in a loop
`quadratic-pointer-append`

- **Rule.** `jsontext.Pointer` is a string and `AppendToken` returns a new one, so
  `p = p.AppendToken(t)` in a loop copies the prefix at every token. Spell the pointer into one
  byte slice, or slice a prefix of an existing pointer at its n-th `/`.
- **Why.** A per-schema pointer builder allocated 112 GB on a deep schema (#1016).
- **Probe.** A `TotalAlloc` doubling test: twice the depth, under three times the bytes.

## Never escape output that is already escaped
`nested-escaping-is-exponential`

- **Rule.** A canonical key built by quoting each child's own quoted key doubles at every level.
  Length-prefix each child instead, which stays injective and linear, and pin it with a growth test
  at a shallow depth under `-timeout`.
- **Why.** A test at the nesting bound ran a binary to 21 GB before it was killed (#811).
- **Probe.** Build the key for twenty levels and assert it stays under a few kilobytes.

## Diff escape sites and size classes on a hot path
`escapes-on-hot-paths`

- **Rule.** Escape analysis is field-insensitive: storing a value in a heap slice moves its whole
  struct, and a method value on a large struct copies it into a closure. For a change to a hot
  lowering path, diff `go build -gcflags=-m` escape sites per package against `main`, and compare
  allocations and bytes per compile on both trees.
- **Why.** A change cost every path item mount allocations and every reference bytes, invisible to
  the allocation counts its body quoted (#591).
- **Probe.** `go build -gcflags=-m ./... 2>&1 | grep -E 'escapes to heap|moved to heap'` on both
  trees, diffed.

## Range over reflect values without its iterators on a hot path
`reflect-iterators-allocate`

- **Rule.** `reflect.Value.Fields`, `Seq` and `Seq2`, and `reflect.Type.Fields`, are not inlined, so
  the loop body's closure escapes and takes loop-carried locals with it. A rewrite to them is a
  behaviour change for cost: count allocations with `testing.AllocsPerRun` and run the nearest
  benchmark on both trees.
- **Why.** A refactor to reflect iterators raised validation's allocations by more than half and
  made every walk's visitor escape (#825).
- **Probe.** `TestWalkValues_StructsAndMapsCostNoAllocation` is the form.

## Bound a reused map: `clear` never shrinks it
`clear-never-shrinks`

- **Rule.** Clearing a non-empty map costs more the more it ever held. Clear a map reused across
  collections until one fills it past a bound, then replace it, through a `reusedMap` and a source
  test refusing any other `clear`, as `ir/irverify` and `pass` keep, each with its own copy
  (#1155). `reusedMap`'s length check can miss a collection that deletes, and the source test
  refuses no delete loop (#1152).
- **Why.** Reuse with no bound passed every cost test, while one model listing a million names
  beside 200,000 listing two took `checkRequires` from a median of 168 ms to 622 ms (#1147, #1153).
- **Probe.** Time a million-entry collection, then many small ones, on both builds: put it first,
  or take the median of many runs where map order places it.
  `TestReusedMap_EmptiesInPlaceUpToTheLimit` pins the bound, and
  `TestReusedMaps_AreEmptiedThroughReusedMapAlone` the `clear`s.
