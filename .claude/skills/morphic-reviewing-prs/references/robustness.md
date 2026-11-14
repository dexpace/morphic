# Robustness

The angle that treats every document as hostile, since deep, cyclic and huge specs are normal input
here: each new loop, recursion, queue, buffer and read has an explicit limit, a panic in code the
package does not own becomes an error, a cancelled run stops, and no read leaves what the run
allows. It loads `morphic-designing-code` and `morphic-measuring-performance`.

1. Name the limit on each loop, recursion, queue, buffer and retry the change adds, and what the
   work grows with under it
   (`.claude/skills/morphic-designing-code/references/design-rules.md#bound-the-product`), each part
   of its charge held to the cost it stands for
   (`.claude/skills/morphic-designing-code/references/design-rules.md#charge-each-part-to-its-cost`).
   In the OpenAPI compiler a count held to a limit counts through
   `compilers/openapi/internal/budget` (`CLAUDE.md`, "Bounded everything"), and its outcome comes
   from the rule for its kind
   (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#bound-outcome-from-the-rules`).
   A charge the change widens re-measures what its comments state
   (`references/robustness.md#remeasure-a-widened-charge`).
2. Hold each limit at N and N+1 by lowering it by one at each site, as `morphic-verifying-changes`'s
   claim table says.
3. Build the shapes that grow the work fastest, deep, wide, cyclic, aliased and chained across
   documents, and run them on both builds
   (`.claude/skills/morphic-measuring-performance/references/finding-cost.md#deep-generated-shapes`).
   A crash or exhaustion on the head that `main` survives is an `issue:`.
4. Route each call into code the package does not own through `barrier.Catch`
   (`.claude/skills/morphic-designing-code/references/design-rules.md#catch-reaches-one-goroutine`).
5. Cancel the run inside each new long stretch
   (`.claude/skills/morphic-verifying-changes/references/claims.md#cancellation-gaps`), and feed
   each new read of a file or the network a reference leaving the spec's directory: only the OpenAPI
   compiler's `AllowExternalRefs` option may let it through.
6. Reach each guard from every input route
   (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#every-input-route-reaches-the-guard`),
   and rewrite each open repro of a route the change joins into its new spelling
   (`references/robustness.md#new-route-open-bugs`).

The matrix is each new limit, foreign call and read, with the input that tests it and both builds'
results.

## A new route inherits the old route's open bugs
`new-route-open-bugs`

- **Rule.** When a change makes X "work as Y does", rewrite each open repro of Y
  (`gh issue list --limit 1000 --search "<mechanism>"`) into X's spelling and run both builds. A
  new crash path is a regression even when its root is an open issue. Check merged and open PR
  bodies for handoffs: `gh pr list --state all --limit 1000 --search "#N in:body"`.
- **Why.** A discriminator mapping resolved "as a `$ref` would" made a `$ref`-only crash reachable
  (#564, #749), and another reached an unbounded library recursion (#763).

## Re-measure a budget's stated margin when a change widens its charge
`remeasure-a-widened-charge`

- **Rule.** When a change widens what a budget, bound or limit charges, `git grep -n` its name
  across comments and documents for a stated margin or measured figure, rerun each on base and
  head, and give both. The comment sits outside the diff, so a sweep of the diff's claims never
  reaches it.
- **Why.** #1470 made the reference expansion budget charge what a path item's operations copy, and
  the 4.3x margin its default's comment states fell to 1.6x on the description it names (#1470).
- **Probe.** The report gives each stated figure with its base and head reruns.
