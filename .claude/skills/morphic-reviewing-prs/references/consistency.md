# Consistency

The angle that holds the change to the ways the codebase already has, its idioms, vocabulary and
meanings, so a reader who knows one part reads the next without relearning. It loads
`morphic-checking-style`, whose departures say where this repository differs from the guide, and
`morphic-designing-code`, whose table names the code to copy. Consistency is `CLAUDE.md`'s last
tiebreaker: matching the code around it never defends what is unclear or wrong there.

1. For each construct the change writes (an error, a diagnostic, an option, a walk, a limit's
   charge, a test, a doc comment), find how the nearest code writes the same thing, in its package
   first, and hold the change to that idiom (`references/consistency.md#match-the-codebase`).
2. Hold each name to the one its package, `docs/ir` and `docs/compilers` give the same concept; a
   second name for one concept is a finding.
3. Hold each meaning to the codebase's: what nil, empty and absent say, what a severity does to a
   run, which tag a field takes, how an ID is derived.

The matrix is each construct and name the change writes, with the example it was held to.

## Match the form and the meaning the codebase gives a construct
`match-the-codebase`

- **Rule.** For each name and construct the change writes, find the codebase's own, in its package
  first, and match both its form (the grammar its neighbours' names take, such as an adjective or
  predicate for a bool and a verb for a mutator) and its meaning, as a name copying a neighbour's
  form but not its meaning misleads the reader who knows the package. A departure better by
  `CLAUDE.md`'s style order is argued in the PR, and the sites still written the old way are filed.
- **Why.** A `replay bool` field was a verb where the package's bools are adjectives or predicates,
  among names #562 first chose that broke the grammar their neighbours set (#562).
- **Probe.** For each new name, grep the package's names of the same kind and compare their form.
