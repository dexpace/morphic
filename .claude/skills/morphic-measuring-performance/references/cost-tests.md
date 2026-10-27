# Tests that pin a cost

A cost test reddens when the cost moves, in every build the gate runs, and for the reason its name
gives. A clock does none of that reliably; a counter does.

## Count the work; never time it in a test
`count-work-not-wall-clock`

- **Rule.** Pin a cost by counting the steps a walk takes through an explicit budget
  (`compilers/openapi/internal/budget`, passed as a parameter, never package state) and asserting
  `work(2n) <= 2*work(n) + c` over shapes that vary the spelling as well as the size. Charge every
  loop, and test that an exactly spent budget takes no step, since other phases' charges mask a
  loop that charges nothing. Count steps, not allocations, where each step is a map insert.
- **Why.** A wall-clock ratio failed CI on noise (#555); shapes varying only size missed quadratic
  lookups, and uncharged loops took seconds while recording no work (#563); allocations grew
  linearly while time grew with the square (#1006).
- **Probe.** `TestBind_CountsWorkLinearInTheDocument` is the form; plant a quadratic walk and an
  uncharged loop, and see each redden it.

## Assert the marginal growth, on one path
`marginal-growth-on-one-path`

- **Rule.** Measure at n, 2n and 4n and assert `A(4n)-A(2n) <= 2.5*(A(2n)-A(n))`: fixed cost
  cancels, linear growth gives 2 and quadratic 4. Require the same diagnostics at all three sizes,
  since a refused size runs other code, and pick sizes at which every planted defect still
  finishes.
- **Why.** A ratio of totals let a quadratic term hide behind fixed cost at small n, bounds pinned
  to one toolchain's measurement failed on the next, and an oversized row made a mutant run take
  minutes (#999).
- **Probe.** Plant a quadratic term and time the mutant run; it reddens within seconds.

## Average a byte count across a doubling
`smooth-step-growth`

- **Rule.** Maps and slices grow in steps, so bytes measured at a few sizes can read as
  superlinear when one step falls between them. When a cost cut makes a scaling test fail, measure
  ratios at many sizes on `main` and on the branch first; if the branch is linear, average each
  size over sizes spread across one doubling, as `costAround` in `pass` does, and keep the bar.
- **Why.** Cutting the smooth mass of path strings exposed the step growth beneath it, and a linear
  cost failed its ratio test (#1125).
- **Probe.** Plant a quadratic, such as a walk per question, and see the averaged test redden.

## Count allocations in the build the gate runs, and take the fewest
`count-in-the-gates-build`

- **Rule.** Run an allocation or byte-count test with `-race -covermode=atomic` before claiming it,
  as `make coverage` runs it. Under `-race`, `sync.Pool` drops one `Put` in four, so anything
  touching a pool allocates at random: build every input before the measured call, take the
  fewest of single measurements, and where one visit makes many pool round trips, take the pool
  off the hot path instead.
- **Why.** Allocation tests that passed `go test` failed the coverage step (#1137); a byte count
  flaked under `-race` until its inputs were formatted outside it (#1006); fewest-of-twenty failed
  for a visit with a pool round trip per entry (#1147).
- **Probe.** `go test -race -covermode=atomic -coverprofile=/dev/null -count=50 -run '<Test>'`.

## Expect instrumentation to move what escapes
`what-allocates-when-instrumented`

- **Rule.** A closure over an iterator is free only where the compiler inlines it, which
  instrumentation can stop: range the stack-allocated source directly instead, as `ir/entries.go`
  ranges `MapRange`. Escape analysis does not tell fields apart, so one leaking field moves its
  neighbours to the heap. A small local map is free up to eight entries, and a size hint past
  eight allocates even an empty one.
- **Why.** Iterator closures and an embedded stack allocated only under `-race` and coverage
  (#1137); a size hint made a one-name map allocate (#1142).
- **Probe.** `go build -gcflags=-m=1` for "moved to heap" and "leaking param content" on the types
  involved, then the allocation test in the gate's build.

## Give an optimization a counting seam
`counting-seam-for-optimizations`

- **Rule.** An optimization changes no output, so a mutant that removes it survives as
  "equivalent". Give the type doing the work a function field set at construction and count its
  calls through an `export_test.go` helper, as `OnVerify` in `engine/export_test.go` counts
  verifications; assert the count per scenario.
- **Why.** Removing a "do not verify again" shortcut changed no result and would have doubled
  `--verify`'s cost (#1119).
- **Probe.** Remove the shortcut; the count assertion reddens.

## Measure a render through a real walk, not a zero path
`allocs-miss-a-free-render`

- **Rule.** `testing.AllocsPerRun` cannot see a render the compiler turns into a no-copy map lookup,
  nor one of the zero `ir.WalkPath`, which renders "" without allocating. Measure a visitor through
  `irtest.AllocatingVisits`, which visits inside a real walk with the walk's own path, one visitor
  per call so another's pruning cannot hide it, and assert the exact set of allocating paths; check `go build -gcflags=-m` before trusting a count to see a
  render at all.
- **Why.** An eager-render mutant survived a test indexing a map with the rendered path (#1125),
  and four survived visit tests run on the zero path (#1133).
- **Probe.** Plant an eager render in the visitor; the test reddens.

## Remove a process-wide confound from the measured window
`process-counter-window`

- **Rule.** For a delta of a process-wide runtime counter, such as goroutines created, switch the
  confound off for the window rather than paying one instance of it first: turn the collector off,
  lift the memory limit, then call `runtime.GC()` to wait out a collection already started, as the
  canary's decoding test does.
- **Why.** A garbage collection started its mark workers inside one window and not the other, and a
  goroutine count with zero margin failed CI on unrelated PRs (#1007).
- **Probe.** Allocate 64 MB inside the window and call `runtime.GC()` before the other window only;
  the count must hold.

## Read allocation totals after the runtime has published them
`exact-alloc-totals`

- **Rule.** The `allocs` profile reflects the heap as of the last completed collection, and
  `runtime/metrics` counts a small object only when its span leaves the processor's cache. For a
  profile, set `runtime.MemProfileRate = 1` and collect twice before writing it; for a total, read
  `runtime.ReadMemStats`, which flushes every cache, before and after.
- **Why.** A main-against-branch profile showed a walk missing on `main` that had run before its
  last collection (#591); a metrics reading trailed by a span per size class (#948).
- **Probe.** Take the same reading twice around a fixed workload; it must not move.

## Place a writer's stop by bytes held, not by write count
`write-boundaries-follow-pool-state`

- **Rule.** How `json.MarshalWrite` splits a document into writes depends on the buffer the pooled
  encoder carries in, which the test does not control. Place a test writer's stop or refusal by how
  many document bytes it holds; to force a write before a value, put many small members ahead of
  it, and assert that precondition with `require`.
- **Why.** A test pinning the write count failed without `-race` too, since the pool is per
  processor (#1132).
- **Probe.** Seed the encoder pool from a probe with fresh and full buffers, and diff `main` against
  the branch.

## Time only the code under test, and measure what the defect moves
`perf-guards-measure-the-defect`

- **Rule.** A cost test or benchmark must time only the function under test with inputs built
  outside the clock, pin the code path when input size is near a path-selecting threshold, and
  measure the quantity the defect changes (walk steps, not allocations); confirm it reddens on the
  reverted code.
- **Why.** An allocation-ratio guard could not see a read-only quadratic walk, a timing test
  measured the parser instead of the walk, and a benchmark crossed the sniff size cap and timed the
  byte scanner (#488).
- **Probe.** Revert the fix and confirm the guard fails; add a counter or probe inside the timed
  region to confirm the target function, on the intended path, is what runs.
