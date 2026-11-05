# Determinism

The angle that holds one input to one output: in either declaration order, at any limit, on any run
and machine, with IDs from source pointers and never from names or order (`CLAUDE.md`, invariants 3,
5 and 7). It loads `morphic-verifying-changes`.

1. Diff the two declaration orders of each construct the change lowers, its diagnostics as a sorted
   multiset
   (`.claude/skills/morphic-verifying-changes/references/differentials.md#two-order-diagnostics`),
   and declare a fixture meant to guard an order fix in the order that was wrong.
2. Diff by hand each fact the change places outside what the order oracle compares
   (`.claude/skills/morphic-verifying-changes/references/differentials.md#order-oracle-reads-what-it-compares`),
   and each verdict a limit decides (`references/determinism.md#two-orders-at-every-limit`).
3. Give each node the change mints an ID namespace apart from every pointer the source can also
   name (`CLAUDE.md`, invariant 3).
4. Search the change for package-level state, map order reaching output, and time, randomness or
   the environment reaching a result.
5. Run `go run ./cmd/morphic-harness <spec>` over each spec the review writes: its oracles compare
   reruns, the JSON round trip, pass idempotence and both orders, but stop at the first error
   diagnostic, so a spec drawing one reaches none of them.

The matrix is each construct and fact by order and limit, with the diff each produced.

## Diff both orders at every limit a verdict turns on
`two-orders-at-every-limit`

- **Rule.** Where a verdict turns on a limit, a count or an exemption for whichever comes first,
  compile both declaration orders at each limit from below the document's cost to above it, and
  diff the verdicts and diagnostics. The order oracle compiles at the default limits alone, so it
  passes such a verdict on any document whose cost is far from them.
- **Why.** A 3.2 path item's first `additionalOperations` entry went uncharged, so the order of two
  entries decided the total, and the oracle passed it on every committed spec (#1470, #1519).
- **Probe.** Plant a first-is-free exemption, and confirm the sweep reports the two orders apart.
