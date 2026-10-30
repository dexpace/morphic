# Derive, never maintain

A hand-kept copy of a count, list or fact drifts silently; derive it from its source and hold the
two together both ways.

## Keep hand-maintained counts and issue states out of prose
`no-maintained-counts-or-state-in-prose`

- **Rule.** A comment or doc states no count of fields, cases, packages or rules, and no issue's
  open or closed state, unless a command beside it derives the number or a revision pins it. Name
  the members or the mechanism instead, and never put a count beside the list it counts. For a set
  code defines, state the rule that decides membership, not a copy of the set.
- **Why.** Each such count went stale silently ("thirteen" internal packages when there were
  fourteen, "one of the two" policies after a third merged), and no test reads prose (#301, #119).
- **Probe.** `git diff origin/main...HEAD | grep -nE '^\+.*\b(one|two|three|four|five|six|seven|eight|nine|ten|[0-9]+)\b'`
  over comments and docs; derive or delete each hit.

## Derive a list that mirrors code, or hold it to the code both ways
`derive-lists-from-their-source`

- **Rule.** A hand-written list of fields, carriers, packages, kinds or keys that mirrors a type
  graph, a writer or the tree is derived from it (reflection, the AST, the writer's own result) or
  held to it by a test that fails in both directions. A completeness test takes its members from the
  declarations, a test claiming "the only X" enumerates every X, and a parse that discovers targets
  is cross-checked against an independent count.
- **Why.** A hand-written reference-field list drifted behind the IR, and an announcement predicate
  restated a writer's construct list without parameters, so new members went unchecked with the
  suite green (#119, #382).
- **Probe.** Add a member to the source without touching the list and confirm a test reddens; remove
  one from the list and confirm the same.

## Derive inventories in docs from the tree
`derive-inventories-from-the-tree`

- **Rule.** Prose listing packages, docs, import edges, allowlists, oracle stages or implemented
  passes is derived from the tree, the archtest `rules` map or the checker's body, marks planned
  items as planned, and is fixed in the same change that moves what it lists, in every document that
  states it.
- **Why.** Layout prose omitted real packages and listed unbuilt passes as present, the layout
  tables in `README.md`, `CLAUDE.md` and `docs/architecture.md` drifted apart, and a pre-flight list
  omitted the coverage gate (#301, #837, #227).
- **Probe.** Diff each list against `git ls-files '*/*.go' | xargs -n1 dirname | sort -u`,
  `ls docs/`, the `rules` map and `harness.Check`'s body.

## Make two implementations that must agree share one source
`parallel-implementations-agree`

- **Rule.** When a parser, derivation, seed list or validity rule exists twice (two tokenizers, a
  writer and its verifier, a pre-scan and the resolver it stands in for), compute it once in a
  shared helper, or run both over a shared edge-case table and assert identical results. When the
  writer starts refusing an input, the checker refuses it in the same change.
- **Why.** A lone `/` read as a token by one walk and as the root by another, and `irverify`
  accepted strings the encoder refuses (#362, #508). `irverify` and `pass` each kept a copy of the
  reference visitor, in step by hand (#1133).
- **Probe.** Run both over empty, lone-separator, missing-leading-separator and length-bound inputs
  and diff; feed the writer's refused inputs to the checker.

## Point at the one definition instead of restating it
`single-source-no-restatement`

- **Rule.** A fact with one definition (a milestone table, the `go` directive in `go.mod`, a
  coverage result, a rationale beside its mechanism) is linked, generated or referenced, never
  copied. The same sentence twice in one comment is also a defect.
- **Why.** The README's Go version and badge disagreed with `go.mod`, a hard-coded 100% badge could
  never go red, and duplicated rationales diverged between files (#760, #324).
- **Probe.** For each version, metric or rationale the diff adds, `git grep` the same fact elsewhere
  and replace the copy with a pointer.

## Derive lookup keys and IDs canonically, through the same function the definition used
`keys-canonical-single-derivation`

- **Rule.** A key or ID that must match a definition is computed through the one function that
  minted it, from the identifier the target actually stores, after normalising every equivalent
  spelling (escapes, percent-encoding); grouping must use every identity an item can carry.
- **Why.** Raw pointer spellings, a shortcut namedTypeID for empty names, or IDs re-derived from a
  composed pointer produce keys matching no interned node, leaving dangling references (#22).
- **Probe.** Feed equivalent spellings (~ vs ~0, %7E, empty component name, $ref'd vs inline) and
  assert they resolve to the same ID; run irverify's no-dangling-refs check over each; grep for a
  second derivation of the same ID kind.
