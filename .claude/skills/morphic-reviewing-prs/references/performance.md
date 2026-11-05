# Performance

The angle that asks what each piece of new work costs in time and memory, what it grows with, and
whether a correct alternative costs less. It loads `morphic-measuring-performance`, whose table says
how each kind of cost claim is measured; a cost read off the code is a `question:` until measured.

1. Name each loop, walk, recursion, cache and growing collection the change adds, the input
   dimension it grows with and its worst shape; its limit is Robustness's.
2. Count its work at n, 2n and 4n on that shape, on both builds
   (`.claude/skills/morphic-measuring-performance/references/cost-tests.md#count-work-not-wall-clock`,
   `.claude/skills/morphic-measuring-performance/references/finding-cost.md#deep-generated-shapes`), in the process doing the work
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#count-stops-at-the-timed-process`) and across two rounds beside a control
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#count-holds-across-rounds`).
3. Price the path every input takes (`references/performance.md#price-common-path`), and drive a
   loop the change reruns until it settles through many turns
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#drive-a-settling-loop`).
4. Read allocations and escapes on hot paths
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#escapes-on-hot-paths`),
   and whether a reused collection keeps its peak
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#clear-never-shrinks`), never taking a resident set read with the collector
   off for a peak (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#resident-set-without-collector`).
5. Hold each cost the change states to the measure its kind takes, and find where a small
   difference comes from (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#attribute-by-statement-counts`).

A finding gives the gap measured on both builds and the correct alternative closing it. The matrix
is each new piece of work with its growth, bound and measured cost.

## Price the common path
`price-common-path`

- **Rule.** When a change adds work to a path every input takes, compare instructions retired on
  base and head
  (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#instructions-not-wall-clock`)
  on a spec of the old shape small enough to hold uncollected, under `GOMAXPROCS=1 GOGC=off`. The
  golden spec, which `CLAUDE.md`'s "Priority" section shows how to fetch, answers only a claim of a
  large effect: with the collector on, its spread hides a percent or two.
- **Why.** Allocation counts miss CPU work, and load skews wall-clock time (#1133); two builds a nil
  check apart spread 20% on the golden spec and read -0.23% pinned on a 1.2 MB spec (#1304).
