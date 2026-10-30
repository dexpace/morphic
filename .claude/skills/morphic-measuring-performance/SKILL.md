---
name: morphic-measuring-performance
description: Use when changing code in this repository for speed or memory, writing or changing a cost, allocation or scaling test or a benchmark, or stating that a change is faster, cheaper, linear, allocation-free or costs nothing on the common path.
---

# Measuring performance in morphic

The styleguide ranks performance second, after correctness, so a cost claim is held like any
other: measured, against `main`, and in the build the gate runs. A number read off one run on a
busy machine measures the machine.

## Claim → measure

| Claim | Measure |
|---|---|
| faster or cheaper | instructions retired, base and head built and run alternately, the median and range of each (`references/finding-cost.md#instructions-not-wall-clock`) |
| no cost on the common path | the same over the golden spec and a large input of the old shape (`.claude/skills/morphic-reviewing-prs/references/review-probes.md#price-common-path`) |
| linear in the input | count steps through a budget (`references/cost-tests.md#count-work-not-wall-clock`) at n, 2n and 4n and assert the marginal growth (`references/cost-tests.md#marginal-growth-on-one-path`) |
| allocation-free | `testing.AllocsPerRun` under `-race -covermode=atomic`, the fewest of single runs (`references/cost-tests.md#count-in-the-gates-build`); a map reused to get there, timed on one huge collection before many small ones (`references/finding-cost.md#clear-never-shrinks`) |
| an optimization holds | a counting seam the test reads, since an optimization changes no output (`references/cost-tests.md#counting-seam-for-optimizations`) |
| bounded on any input | generated shapes, deep and wide, on both builds (`references/finding-cost.md#deep-generated-shapes`) |

## Habits

- Run nothing of your own beside a measurement, and quote each side's spread with its median.
- A test pins cost by counting work; wall time belongs in a PR body, taken back to back.
- Measure against a base the branch holds: `main` may have gained a speedup minutes ago
  (`references/finding-cost.md#check-the-base-build`).
- Planting the defect a cost test names applies here as anywhere: a quadratic loop, an uncharged
  step, an eager render.

## References

- `references/cost-tests.md`: tests that pin a cost: counting work, marginal growth, allocation
  counts under instrumentation, seams, renders, process-wide counters, write boundaries.
- `references/finding-cost.md`: where cost hides and how to quote it: claims, callers, generated
  shapes, quadratic appends and escapes, escape sites, reflect iterators, reused maps.
