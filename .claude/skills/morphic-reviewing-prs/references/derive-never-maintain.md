# Derive, never maintain

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

A hand-kept copy of a count, list or fact drifts silently; derive it from its source and hold the two together both ways.

## Delete hand-maintained counts, issue states and versions from comments and docs
`no-maintained-counts-or-state-in-prose`

- **Rule.** A comment or doc must not state a count of fields, cases, packages, rules, refusals, policies or commits, nor an issue's open/closed state, unless the number is derived by a command named beside it or pinned to a revision; name the members or the mechanism instead, and never put a count next to the list it counts.
- **Also.** A doc comment must not list the callers, cases, invariants, reference classes or categories of a set that code defines; state the property or rule that derives membership and point at the source of truth. A list that is kept must cover the whole set.
- **Why.** Every such count in this repo went stale silently ('thirteen' internal packages when there were fourteen, 'four residual shapes' when there were five, 'one of the two' policies after a third merged in), and no test can catch prose.
- **Probe.** git diff main...HEAD | grep -nE '^\+.*\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|[0-9]+)\b' over comments and docs; for each hit, derive the number from the code (ls, grep -c, go list) and either replace it with the command or delete it.
- **Seen in #301**: “"openapi has thirteen" internal packages.”
- **Seen in #119** (5623483e): “§4.8 listed four residual shapes. There are five: a branch whose `$ref` names no”

## Derive any list that mirrors code instead of maintaining it by hand
`derive-lists-from-their-source`

- **Rule.** A hand-written list of fields, carriers, packages, classes or keys that mirrors a type graph, a writer, or the tree must be derived from it (reflection, AST, the writer's own result) or diffed against it by a test that fails in both directions.
- **Also.** A completeness test, sealed-sum guard or golden list must derive its member set from the declarations, the production table or one shared file walk, never a hand-written slice; a fixture directory and its capability table must each fail on an entry the other lacks; a test claiming 'the only X' must enumerate every X.
- **Also.** When a guard audits packages, kinds or files, derive the list from the sources and fail on any member missing from both the list and the exemptions; when the targets are discovered by parsing, cross-check the parse against an independent count so a narrowed parse fails instead of silently shrinking coverage.
- **Why.** A hand-written reference-field list drifted behind the IR and an announcement predicate restated the writer's construct list and omitted parameters, so new members went unchecked or unannounced with the suite green.
- **Probe.** Add a member to the source (an IR field, a writer construct, a package) in a scratch edit without touching the list and confirm a test reddens; remove one from the list and confirm the same.
- **Seen in #119** (836f1020): “checkDanglingTypeRefs was documented as reporting every dangling”
- **Seen in #382** (a498517b): “`pathItemDeclaresAnything` is a hand-maintained mirror of `applyPathItem`'s construct list, so the next construct added there silently loses its announcement.”

## Derive package, file, import and feature lists in docs from the tree, not memory
`derive-inventories-from-the-tree`

- **Rule.** Prose that lists packages, docs files, import edges, allowlists, oracle stages, method implementers or implemented passes must be derived from the tree, the archtest rules map, the checker's body or the import graph, and must mark planned items as planned rather than presenting them beside built ones.
- **Why.** Layout and inventory prose here omitted real packages, named a directory an issue was removing, listed unbuilt passes as present, and understated harness.Check's oracles, sending readers to rebuild mechanisms that already existed.
- **Probe.** Run git ls-files '*/*.go' | xargs -n1 dirname | sort -u, ls docs/, go list -deps, and read archtest's rules map and harness.Check's body; diff each against the doc's list. For each listed pass or feature, grep for its exported entry point.
- **Seen in #301**: “The oracle list named five oracles”
- **Seen in #837** (24548dd7): “internal/leakcheck had been missing from all three since it landed in #535”

## Make two implementations that must agree derive from one source or diff them on shared edge cases
`parallel-implementations-agree`

- **Rule.** When a parser, derivation, seed list or validity rule exists in two places (two tokenizers, two tests' filters, a table test and a fuzz seed, a writer and its verifier), compute it once in a shared helper or run both over a shared edge-case table and assert identical results; when the writer starts refusing input, the checker refuses it in the same change.
- **Also.** When a pre-scan, validator or helper parses the same string (pointer, fragment, URI) that a downstream resolver later reads, it must call the resolver's own normalisation and acceptance rule; any second hand-rolled copy must be deleted or pinned by a shared input table and a differential fuzz target.
- **Why.** Copies drift: a lone '/' read as a token by one walk and the root by another, and irverify accepted strings the encoder refuses.
- **Probe.** Grep for a second implementation of the changed logic; run both over empty, lone separator, no leading separator and length-bound cases and diff; feed the writer's refused inputs to the checker.
- **Seen in #362** (fe7ccf50): “a lone "/" as one empty token where the sibling walk reads it as the”
- **Seen in #508** (9d56cb6b): “#507: `irverify` passes a string that is not UTF-8, which the encoder now refuses.”

## Point at the one definition instead of restating a fact, table, version or rationale
`single-source-no-restatement`

- **Rule.** A fact that has a single definition (README's milestone table, go.mod's go directive, a coverage result, a rationale documented beside its mechanism, a canonical example) must be linked, generated or referenced, never copied into a second place; the same sentence stated twice in one comment is also a defect.
- **Why.** Copies drifted from their source: the README's Go version and badge disagreed with go.mod, a hard-coded 100% badge could never go red, and duplicated examples and rationales diverged between files.
- **Probe.** For each literal version, metric, table or rationale paragraph the diff adds, git grep for the same fact elsewhere (go.mod, README tables, the mechanism's own comment); if a source exists, replace the copy with a pointer or generated value. For a badge or status, confirm something can make it fail.
- **Seen in #760**: “It pinned 1.26 while `go.mod` names 1.27.1.”
- **Seen in #324**: “asserted 100% unconditionally and could not go red”

## Derive lookup keys and IDs canonically, through the same function the definition used
`keys-canonical-single-derivation`

- **Rule.** A key or ID that must match a definition is computed through the one function that minted it, from the identifier the target actually stores, after normalising every equivalent spelling (escapes, percent-encoding); grouping must use every identity an item can carry.
- **Why.** Raw pointer spellings, a shortcut namedTypeID for empty names, or IDs re-derived from a composed pointer produce keys matching no interned node, leaving dangling references.
- **Probe.** Feed equivalent spellings (~ vs ~0, %7E, empty component name, $ref'd vs inline) and assert they resolve to the same ID; run irverify's no-dangling-refs check over each; grep for a second derivation of the same ID kind.
- **Seen in #22** (79422772): “always resolved to the component's namedTypeID”
- **Seen in #22** (79422772): “resolveComponentRef built the resolved ID from the raw $ref pointer text,”

