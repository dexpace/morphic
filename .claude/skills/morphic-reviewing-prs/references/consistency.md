# Consistency

The angle that holds the change to the ways the codebase already has, its idioms, vocabulary and
meanings, so a reader who knows one part reads the next without relearning. It loads
`morphic-checking-style`, whose departures say where this repository differs from the guide, and
`morphic-designing-code`, whose table names the code to copy. Consistency is `CLAUDE.md`'s last
tiebreaker: matching the code around it never defends what is unclear or wrong there.

1. For each construct the change writes (an error, a diagnostic, an option, a walk, a limit's
   charge, a test, a doc comment), find how the nearest code writes the same thing, in its package
   first, and hold the change to that idiom (`references/consistency.md#match-the-codebase`).
2. Hold each name to the one its package, `docs/ir` and `docs/compilers` give the same concept (a
   second name is a finding), and hold each rule a change writes into `docs/ir`, `docs/compilers`
   or `docs/emitters` to every statement of the same rule elsewhere under `docs/`, found by the
   construct's vocabulary (`git grep -n '<construct>' -- docs/`); a disagreement is an `issue:`.
3. Hold each meaning to the codebase's: what nil, empty and absent say, what a severity does to a
   run, which tag a field takes, how an ID is derived.
4. Sweep every name the change renames or deletes (`references/consistency.md#renamed-names-leave-no-trace`,
   `references/consistency.md#pattern-rename-rewrites-prose`).

The matrix is each construct and name the change writes, with the example it was held to.

## Match the form and the meaning the codebase gives a construct
`match-the-codebase`

- **Rule.** For each name and construct the change writes, find the codebase's own, in its package
  first, and match both its form (the grammar its neighbours' names take, such as an adjective or
  predicate for a bool and a verb for a mutator) and its meaning, as a name copying a neighbour's
  form but not its meaning misleads the reader who knows the package. A departure better by
  `CLAUDE.md`'s style order is argued in the PR, and the sites still written the old way are filed.
- **Why.** Names first chosen for #562 broke the grammar their neighbours set, among them a bool
  field named as a verb where the package's bools are adjectives or predicates (#562).
- **Probe.** For each new name, grep the package's names of the same kind and compare their form.

## Sweep every renamed or deleted name from the code, not from memory
`renamed-names-leave-no-trace`

- **Rule.** Build the rename map from the code with git's word diff,
  `git diff --word-diff --word-diff-regex='[A-Za-z_][A-Za-z0-9_]*' "$main"...HEAD -- '*.go'`,
  which prints a renamed identifier as `[-old-]{+new+}` and a deleted one as `[-old-]`, fields,
  methods and locals included, where `^-(func|type) ` lists only deleted declarations. Run
  `git grep -nw` for each old name across comments, documents and `Test<Old>_` names; search single
  English words (`None`, `Site`) per directory where they no longer name an identifier. Also apply
  `references/consistency.md#pattern-rename-rewrites-prose`.
- **Why.** #1725 left `groupFor` in untouched `callbacks.go` (#1727); #2130 (`0cf8e899e`) left
  `Elems` across 85 lines in 49 files and 22 test names while `^-(func|type) ` matched nothing
  (#2146).
- **Probe.** After `git fetch origin refs/pull/2130/head`, the word diff from `a22fdf935^` to
  `0cf8e899e` prints `[-Elems-]{+ElementTypes+}`, and
  `git grep -nw 'Elems' 0cf8e899e -- ir/types.go` hits four comment lines.

## Check pattern renames and wording sweeps against comments and test names
`pattern-rename-rewrites-prose`

- **Rule.** For a rename applied by pattern, map each changed comment line back through the reverse
  rename: a line that no longer reads as before is prose the rename rewrote, especially for
  English-word identifiers (`at`, `doc`, `place`). A wording sweep also greps the identifier
  spelling (`git grep -niE 'referent ?has'`), not prose alone.
- **Why.** #1665's pattern rename of `at` to `place` rewrote three comment lines ("ended place,",
  fixed in `bf2d65eca`), and a case-sensitive prose sweep for "referent has" missed a test named
  with `ReferentHasIt` (#2146).
- **Probe.** Reverse-map changed comment lines of a pattern rename, and run
  `git grep -niE '<word1> ?<word2>'` for wording sweeps.
