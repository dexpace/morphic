# Sweep every site the change touches

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

A defect is a property of its mechanism: sweep every site of it, every stale sentence about it, every route and spelling into it.

## Fix the mechanism at every site that runs it, not only where it was noticed
`sweep-every-site-of-the-mechanism`

- **Rule.** When a diff fixes a defect, adds a guard, sanitizer or check, or closes an issue, name the mechanism and grep every code site that runs it (sibling hoisters, other callers of the old helper, the parallel request/reply set, every registry, every channel of the same text); fix each in the same PR or state the uncovered site in code and PR body. An issue's own site list and a single-instance framing are claims to check against the code, not the scope.
- **Why.** The pointer-collision fix left the same collision standing in the inline-position hoist, and same-mechanism sites deferred to follow-up issues kept the defect live on main while the PR claimed it closed.
- **Probe.** Before push, write the mechanism as a grep (the helper name, the literal, the guard shape) and run it across the tree; for each hit either show the fix or plant the reproducer at that site and watch it go red.
- **Seen in #157**: “is what left the identical defect standing in the inline-position hoist.”
- **Seen in #101** (fe10b45c): “schemaBody read nullability with schemaHasNull while the $ref site and the union”

## Retarget every comment, test name and doc that names a renamed, moved or deleted symbol
`sweep-old-name-after-rename-or-delete`

- **Rule.** When a diff renames, moves, deletes or replaces a function, type, receiver, table, call path or mechanism, every doc comment, test name, package doc and docs/ page that names the old symbol or describes the old mechanism must be rewritten in the same change. A package doc is a claim about the package's current contents and must be re-read after code moves out.
- **Also.** After a rename, move or single-source constant, grep the old token as a plain substring (not a whole word) across the whole tree, docs and comments included, and route every remaining literal through the canonical spelling.
- **Why.** Comments here outlive the code they describe: they pointed at deleted functions (intern, fillParamDefault), removed receivers (l.ctx) and replaced resolvers, sending readers to code that no longer exists.
- **Probe.** For every identifier the diff removes or renames (git diff main -U0 | grep '^-' for func/type names), run git grep -n '<oldName>' over *.go, docs/, README.md and CLAUDE.md on the final tree; any hit outside the diff's own deletions is a finding. Repeat for the old mechanism's vocabulary (e.g. 'go/ast', 'parse', '..').
- **Seen in #195** (c5d2611f): “registeredNode's contract credited intern with recording a pointer's ID”
- **Seen in #834** (e5afd969): “fillParamDefault no longer exists: fillParamSchema assigns the value”

## Lower the same source construct at every position that can hold it and diff
`same-construct-at-every-position`

- **Rule.** When a change lowers, preserves or reports a keyword or rule at one schema position, enumerate every position the spec lets it appear (named declaration, inline, property, parameter, header, allOf branch, beside a $ref, composed body, nested object, under an x- extension) and make each one reach the same path or record what it cannot carry. A carrier type with no field for a keyword must record it as residue.
- **Why.** One construct lowered two ways depending on where it appeared, and parameter, header and nested-object positions silently dropped keywords the property position kept.
- **Probe.** Compile one fixture that writes the same construct (with a sibling keyword and an x- key) at every position, then diff the IR and the diagnostic list per position; any position that loses something is the missed site.
- **Seen in #149** (909436cf): “A parameter whose schema is spelled {$ref: ...} inherited nothing from its”
- **Seen in #156**: “so one source construct lowered two ways depending on where it appeared”

## When behaviour or a contract changes, rewrite every comment that states the old behaviour
`sweep-comments-a-behaviour-change-falsifies`

- **Rule.** A change that alters what code does (effect turned into a return value, a new refusal or error class, a new emitter of a diagnostic, a new detector, a new caller, a retired premise) must sweep the package for comments asserting the old behaviour, especially those using only, never, the single, the one, records or reports. Preconditions in doc comments are re-checked against every call site, including callers added in the same diff.
- **Why.** Converted functions kept saying they 'record' diagnostics they now return, and 'the single append point' / 'the only way' claims survived the change that made them false, so the doc contract contradicted the code.
- **Probe.** From the diff, list each behavioural verb that changed (record->return, refuse->report, dropped->lowered) and each new caller or class; git grep -nE 'only|never|single|the one|records|reports|refus' in the touched packages and read each hit against the new code. For a precondition comment, list callers with gopls references / git grep '<func>(' and check each, including new ones.
- **Seen in #198** (703208ef): “Four of the six converted functions still described themselves as”
- **Seen in #213** (c7c7ae45): “appendDiag forwarded one argument to l.diags.Append and its comment claimed”

## Update the normative doc row, plan or gate list that a change makes untrue, in the same change
`update-normative-docs-in-same-change`

- **Rule.** When a change ships a lowering, diagnostic, Unmodeled key, degradation case, gate step or resequenced item, the docs/*.md row, README section, planning document or status line that describes it must be updated in the same change, and every clause of that row re-checked, not only the one the fix named.
- **Why.** ir-design.md and the spec matrix are the contract; when they lag the code (a missing warning, an unnamed key, a 'not yet implemented' status beside landed rows, a gate list missing a step) contributors build against a false spec.
- **Probe.** For each new diagnostic code, Unmodeled key, gate step or lowering in the diff, git grep docs/ README.md CLAUDE.md for a neighbouring code/key/step name and confirm the new one sits beside it; for each doc row the diff touches, re-run the case for every clause of that row on the final tree.
- **Seen in #310** (cd5ea287): “The additionalOperations clause added with this change described the”
- **Seen in #345** (9b052f37): “error branch they were being dropped on, and that x-* is simply preserved,”

## Cover every spelling the spec allows for one concept
`every-spelling-of-one-concept`

- **Rule.** When a change reads a concept the source can spell several ways (keyword nullability versus a null union branch, schema versus content, ASCII versus non-ASCII twins, both orders of an adjacency), write one case per spelling and assert they lower and diagnose identically.
- **Also.** For each construct a change reads, enumerate the spellings the governing spec admits -- case-insensitive tokens in another case, tilde and backtick fences, tables without outer pipes, a $ref versus a YAML alias for the same reuse -- and require the same IR, severities and exit codes for each.
- **Why.** The null-spelling fix handled keyword spellings only, so a null oneOf branch dropped Nullable at every reference, and a header written with content lowered as untyped.
- **Probe.** Build a table of equivalent spellings for the concept and assert identical IR and diagnostic codes across the table; re-run every twin pair after any reader changes.
- **Seen in #101** (e86cadfe): “spellings, so a target spelling nullability as a union null branch lost the bit”
- **Seen in #185** (b25e38bc): “header lowered as if it had no schema at all: the type became t/prim/any and”

## Probe every raw-YAML reader with alias, anchor, merge-key, tag and quoted spellings
`yaml-alias-anchor-merge-spellings`

- **Rule.** Any code that reads raw YAML nodes or mirrors the parser's rules must resolve aliases, anchors, merge keys and tags the way the parser does, and must not treat quoted or aliased non-special spellings as special. Prefer the raw node or the library call over a hand-written copy of the rule.
- **Why.** Readers matched keys by raw node text or skipped anchor-valued pairs, so aliased, anchored or merged entries were dropped with exit 0, while quoted '<<' keys raised a false cycle.
- **Probe.** Feed each touched reader a fixture set: key as alias, value as alias, anchored entry, '<<' merge, quoted '<<', !!merge tag; diff IR and diagnostics against the plain spelling.
- **Seen in #500** (f91f5a3d): “On `main`, each of these compiles clean, exits 0, and loses the anchored entry:”
- **Seen in #356** (6fcfd8ab): “the anchor; searching it raw found nothing, and a key with no node kept”

## Update every doc copy of a fact the change alters, derived from the tree
`docs-restating-a-fact-move-with-it`

- **Rule.** When a change moves packages, adds a command, departs from a design item or corrects a fact, grep every document and checklist that states it and fix each in the same PR; derive lists from ls, the package tree, or the Makefile rather than memory.
- **Why.** Layout tables in README, CLAUDE.md and architecture.md drifted apart, and a pre-flight list omitted the 100% coverage gate.
- **Probe.** Diff each documented list against its derivation (ls docs/*.md, git ls-files package dirs, Makefile gate targets) and grep docs for a sibling entry the new one should join.
- **Seen in #227**: “README carries a layout table of its own, and this PR was about to leave it contradicting the one it fixed.”
- **Seen in #301**: “The normative-docs list named four of eight documents.”

## Make every input route reach the new guard, path or oracle
`every-input-route-reaches-the-guard`

- **Rule.** When a guard, transformation, validation or new branch is applied on one route into a consumer, list every other route into it (external $ref documents, overlays, callbacks, direct versus behind a $ref, harness oracles that take no options) and confirm each reaches it, or state the uncovered route where the next reader will see it.
- **Why.** The anchor release and alias refusals protected only the root document, so overlays and externally referenced documents still crashed or lost entries with exit 0, and the harness oracles never saw the overlay path.
- **Probe.** Run the same negative fixture through each route (wrap it in an external $ref, apply it as an overlay, reach it through a callback) and diff the diagnostics and IR against the direct compile.
- **Seen in #556**: “A document an external `$ref` names never passed through that”
- **Seen in #492**: “The source has been refused for both shapes since #12 and #27.”

## Decide and test the combination when keywords co-occur
`test-co-declared-keywords-together`

- **Rule.** When a lowering reads several keywords that may be co-declared (prefixEncoding with itemEncoding, readOnly with writeOnly, minimum with exclusiveMinimum, allOf with oneOf, explode with an empty style, siblings a rewrite target cannot carry), test the pair directly; a dropped partner goes to Unmodeled or a diagnostic, never only into a message.
- **Why.** Ordered early-return guards and single-path lowerings kept one keyword of a pair, so {minimum:10,exclusiveMinimum:0} and {minimum:10} compiled byte-identical.
- **Probe.** For each keyword pair the code reads, compile the pair, each keyword alone and neither, and diff all four documents; two identical outputs for different inputs is the drop.
- **Seen in #346**: “The displaced keyword went nowhere else.”
- **Seen in #322**: “guarded the two flags in order and returned on”

## Enumerate the spec versions, locations and override levels a rule depends on, and fixture each
`cover-every-version-location-and-level`

- **Rule.** When a value's source or legality varies by spec version, parameter location or declaration level, list every value of that axis and test it: a name from a 3.2-only field needs a 3.0/3.1 fallback, every IR entity kind needs a naming channel, the most specific override level wins, a default must be legal at its exact location, and fixtures must respect the format's co-location prohibitions.
- **Why.** Every 3.0/3.1 server and every response reached the IR nameless, operation-level servers were dropped in favour of the superseded path-item list, a querystring parameter got an invented style the spec forbids, and a fixture violated 3.2's querystring rule.
- **Probe.** Compile one document per supported spec version and one declaring the keyword at both path-item and operation level; grep the IR for empty names per entity kind and check each emitted default keyword against the spec's legal set for that location.
- **Seen in #261**: “`Server.Name` was filled only from the OpenAPI 3.2 `name` field, which a”
- **Seen in #272** (c9d38d0f): “Only the path item's were read, so a document declaring both kept the”

