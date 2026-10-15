# Morphic review catalog

Snapshot: Omar's PRs opened before 2026-10-14 (main at 210283f); counts are true of that snapshot only. Derived from the fix commits, review comments and PR-body review notes on Omar Al Jarrah's PRs in dexpace/morphic (the 20 back-test hold-out PRs excluded). Each rule is a defect class that a local review caught **after** a PR's first push. `n` is the number of verified findings behind the rule and `PRs` the number of distinct PRs; both are computed from finding IDs by `build/catalog.py`, never written by hand. Exemplars quote the PR history verbatim.

## 1. Claims against reality

Every sentence in a comment, doc, commit or PR body that states behaviour is a claim to execute, not to read — and to execute outside the case its author measured.

### Check every 'never/always/only/every/none' claim against every site it quantifies over
`universal-claims-enumerate-every-site` · n=18 · PRs=12

- **Rule.** Before a comment, assertion message or normative doc asserts an absolute (never, always, only, the only, every, read by nothing, nothing modelled behind, validated once), enumerate every caller, return path, producer, position, route and member it quantifies over and check each; otherwise scope the claim to the sites checked or name the exceptions. A field's doc must describe what every writer of that field actually writes.
- **Why.** Absolute claims written from one representative case were false at other sites: 'read by nothing' while constraints.go read it, 'never reports a spec-level problem' with three reachable paths, a field contract no producer obeyed.
- **Probe.** For each absolute word in a changed comment, list the quantified sites mechanically (git grep writers of the field, gopls callers, every return statement, every composition position such as allOf/anyOf/items) and probe one input per site through the compiler, reading the full diagnostic list; one counterexample means narrow or delete the claim.
- **CLAUDE.md already says:** “Counts and universals in prose are where this repo's errors concentrate”
- **Seen in #111** (7f4d3130): “minProperties is not read by nothing”
- **Seen in #190** (6af887e8): “The doc comment said the error return "never reports a spec-level problem", and three of its four paths are”

### Run the exact input a comment describes before stating what it lowers to, drops or fails as
`probe-behaviour-claims-by-running-the-input` · n=13 · PRs=12

- **Rule.** A comment, test comment, design-doc sentence or PR body that says what a specific input does (lowers to X, is dropped, reaches a branch, deadlocks, is unsatisfiable, is order-independent, would fail these cases) must be backed by compiling or running that literal input, in both declaration orders where order is claimed; pin the observed value in a test where it can drift.
- **Why.** Claims written from the code's shape were wrong when executed: 1XY lowered to the defined 1XX range, a 'deadlock' was a stack overflow, maxLength 10 and 20 intersect, and an 'unreachable' branch was reachable via a YAML merge key.
- **Probe.** Compile the input named in the comment (go run ./cmd/morphic or the harness) and read the emitted IR and full diagnostic list; for order claims swap declaration order and cmp.Diff the documents; for 'unreachable', try merge keys, aliases and inline vs $ref positions; for 'would fail', actually run the alternative.
- **CLAUDE.md already says:** “Probe, don't read.”
- **Seen in #263** (a47ca6f9): “"1XY" lowering to a range OpenAPI does not define when it lowered to 1XX”
- **Seen in #229** (a3caef0b): “The reproducer list described cycle_pointer_whitespace_self as a deadlock”

### Verify claims about specs, libraries, toolchains and standards at the source, with a version
`external-claims-checked-at-source` · n=13 · PRs=11

- **Rule.** A comment or doc that states what an external spec, standard, library, Go toolchain feature or third-party codebase does must be checked against that source's text or a probe of it, cite the section number or dependency version it was read at, and name the component that actually consumes the input.
- **Why.** Claims from memory were wrong: the yaml.v3 alias budget never fires on node decode, json/v2 still sits behind goexperiment, a resolver handles some anchor fragments, wrong spec section numbers, and a prior-art survey written without reading oagen's source.
- **Probe.** For each external claim in the diff, open the dependency source at the go.mod version (go mod download -json; read the file) or the spec section, or write a one-off probe calling the library with the cited input; confirm the citation carries a version or section and names the reader actually used (e.g. speakeasy, not yaml.v3).
- **Seen in #480**: “`decode`'s doc comment claimed the decode is "the one place the yaml.v3 alias budget is spent, which is what bounds a billion-laughs expansion"”
- **Seen in #525** (e7a268c2): “"Generally available" became "built by default".”

### Test the reason a comment gives, not just its verdict, by removing the cited guard
`verify-the-stated-reason` · n=10 · PRs=10

- **Rule.** When a comment credits a guard, call, set, value or missing symbol with a reason (this check distinguishes X, the set terminates cycles, the value is incidental, the IR cannot hold this, a repeat matches twice), verify the reason itself: remove the guard and rerun the cited input, trace consumers of the 'incidental' value, grep for the 'missing' symbol. A correct verdict with a false reason is still a defect.
- **Why.** Comments here credited a redundant directory guard, justified a visited set by cycles the depth cap already handled, named IR blockers that ir.LongRunning already modelled, and presented an unrecorded asymmetry as code intent.
- **Probe.** Comment out the guard or call the comment credits, rerun the input the comment cites, and see whether the stated effect appears; if the input still passes, the citation is wrong. For a named blocker, git grep the symbol; for an 'incidental' value, list every branch that reads it.
- **CLAUDE.md already says:** “Disagreement has to enter from outside: a planted mutation, a probe”
- **Seen in #368** (1b73b8ee): “The walk's comment attributed the set to cycle termination and cited a”
- **Seen in #193** (bfb84e82): “It credited the directory guard”

### Run the real external tool on a planted input before relying on it
`probe-the-real-external-tool` · n=9 · PRs=3

- **Rule.** When a script or check depends on a linter, jq, git output, the go command or a CI action, plant the construct and run the real tool from an unrelated directory as CI runs it: clean files, disabled linters, null arrays, colons in paths, CRLF, exported env vars.
- **Why.** nolint directives naming disabled or unknown linters passed silently, jq exited 0 with a shrunk set, and a setup-go bump spammed GOTOOLCHAIN warnings in every step.
- **Probe.** Plant each directive or data shape in a scratch file, run the tool and the check against it from a different cwd, and compare their verdicts form by form.
- **CLAUDE.md already says:** “Probe, don't read.”
- **Seen in #367**: “so a directive in a clean file is never read at all, and the”
- **Seen in #367** (8cf56224): “`jq -r '.Enabled[].name'` cannot iterate the null that golangci-lint emits when”

### Name every branch, outcome and uncovered input in a function's or option's contract
`doc-names-every-outcome` · n=8 · PRs=7

- **Rule.** A doc comment on a function, type, option or diagnostic must name every outcome its branches produce (each failure condition in the lead sentence, each grade of a strict mode, each live error arm, what happens at the limit) and every input class that breaks a stated guarantee, not only the important or common case.
- **Why.** Contracts that named one outcome made live branches read as dead code, hid that strict mode reports without refusing, and promised once-per-node or order-independent behaviour without naming the re-read and declaration-order cases that break it.
- **Probe.** For each changed doc comment, list the function's return paths and switch arms and map each to a sentence in the comment; any unmapped path is a finding. For a guarantee, construct the worst-case input (multi-line, re-read through another $ref, swapped declaration order) and run it.
- **Seen in #267**: “`enumMembers`' lead sentence enumerated two conditions for `ok=false`”
- **Seen in #269** (e955e9cc): “The note above ParseFloat named only the IsInf outcome, which reads as if”

### Reconcile a doc's summary line, headers and universals with its own rows and sentences
`doc-agrees-with-itself` · n=8 · PRs=7

- **Rule.** Within one doc, comment, table or PR body, a status line, section header, category split or universal sentence must agree with the rows and sentences beside it; after adding a row or rule, re-read the surrounding paragraph and headers for contradiction, including against the invariants the doc itself defines.
- **Why.** Docs here said 'not yet implemented' beside rows marked landed, filed a non-refused row under the refused header, said aliases are held to 'none of that' right after a rule they are held to, and made a name part of an ID contrary to invariant 3.
- **Probe.** After editing a table or paragraph, re-read every header, status line and universal in the same section against each row; grep the same input across sibling tables (e.g. divergence tables vs corpus comments) and confirm they classify it the same way.
- **Seen in #185** (1011beb9): “said "not yet implemented" while four rows were marked landed”
- **Seen in #246** (72d25ccc): “The block-form !!binary sat under a header reserved”

### Check claims about scripts, CI, linters and caps against what they actually execute
`tooling-claims-match-the-gate` · n=8 · PRs=6

- **Rule.** A doc or comment that says what a script, CI step, action, linter or metric does (what it groups by, what it counts, whether it is generated, what order it runs in, what it puts on PATH) must be checked against the code path or a run of it, and any command quoted in docs must be copied from the gate step it mirrors and run verbatim on the branch it describes.
- **Why.** README claimed per-package coverage while the script computes one global total, documented a gofmt form the gate does not run, called a hand-written test 'generated', and a CI comment's false PATH claim caused a second linter install.
- **Probe.** Run every command a changed doc quotes, verbatim, and compare its output to the gate's; read the script or action entrypoint for each 'enforces/counts/runs every' claim and name exceptions (e.g. lint is not a Makefile target); for CI environment claims, print the environment inside a job.
- **CLAUDE.md already says:** “for the step list rather than restating it here”
- **Seen in #315**: “the script computes one global”
- **Seen in #315**: “ran `gofmt -l .`; the gate runs”

### Check each verb and flow word in a doc comment against the body and call sites
`doc-verbs-match-the-body` · n=7 · PRs=7

- **Rule.** Every verb a doc comment uses about its function (checks, records, caches, reads once, intersects, passes, copies) and every ordering word (above, before) must match what the body does today and how call sites use it; describe what is returned and what is left to callers, and state a formal grammar or type check with the exact acceptance condition the code implements.
- **Why.** Comments described memoization the function lacked, a check it delegates to callers, a value flow that did not exist yet, set intersection where keys are adopted, and a grammar with digits* where the code requires a digit.
- **Probe.** Read the changed doc comment verb by verb next to the function body; for flow words, list call sites with git grep and trace the real call order; for a stated grammar or type rule, write a table test with the boundary input (zero digits, defined vs predeclared type) and confirm it agrees with the comment.
- **Seen in #266** (a0010adf): “Also drop a claim from thingIDVisibility's doc comment that it checks for”
- **Seen in #290** (1df07c58): “readDeclarations said it reads the identities”

### Make every name and cross-reference in a comment resolve to one visible target
`comment-references-resolve` · n=6 · PRs=6

- **Rule.** Every identifier, test name, field name and in-comment cross-reference ('for that reason', 'see above') must resolve to exactly one existing target visible from the comment's package: no function locals in GoDoc, no constants from packages above, no nonexistent tests, and owner-qualified field names when a short name is ambiguous.
- **Why.** Comments pointed at tests that do not exist, at locals the API reader cannot see, at a constant in a higher layer, and used 'Auth' for two fields reaching opposite conclusions.
- **Probe.** For each identifier in a changed comment, git grep -nw it in the package (and go doc the package) to confirm it exists and is visible; for each cited test, go test -list '<Name>' ./... must print it; for each cross-reference phrase, read the referent sentence.
- **Seen in #365** (83254807): “named a test that does not exist, as did the comment on”
- **Seen in #266**: “The rationale comment used "Auth" for two different fields, two sentences apart, reaching opposite conclusions about each.”

### Build every code sample in a doc under its own rules and check every cited precedent
`doc-code-obeys-own-rules` · n=6 · PRs=3

- **Rule.** Code examples in READMEs, style guides and design docs must compile and run under the flags and banned-construct lists the same document mandates, and any code location a doc cites as precedent must actually say what the doc claims.
- **Why.** Exemplars that do not compile or use the forms the guide bans teach the wrong thing, and a design doc prescribed a package-level mutable registry citing code that says the opposite.
- **Probe.** Extract each sample into a scratch module and build/run it with the mandated flags and linter; open each cited file/line and compare.
- **Seen in #1** (90e384fa): “typescript: declare error-class and resource-handle fields explicitly”
- **Seen in #760**: “It did not compile: the range variable `d` and `doc` were declared and never used.”

### Test a safety, idempotence, neutrality or stability argument in every direction it can fail
`prove-safety-arguments-in-every-direction` · n=5 · PRs=5

- **Rule.** A comment or design claim that a refactor is behaviour-neutral, a mismatch fails safely, a rewrite is a fixed point, a sort is stable, or a derived cache never exceeds its source must be checked in each direction (lost, won, unknown; neighbouring elements the rewrite changes; equal keys; every gate condition of the source), and a derived gate should reuse the source's result rather than a copied admission test.
- **Why.** Partial arguments hid real bugs: a family that wins the election was dropped silently, sort.Slice broke a promised stable order, a copied memo gate dropped a condition, and unifying three disagreeing grammar copies was called neutral.
- **Probe.** Enumerate the outcomes the argument covers and write one test per direction (mismatch that wins vs loses, equal sort keys, a rune after the split, the source cache's other gate). For neutrality, run the unified and old versions over the corpus and diff the IR.
- **Seen in #270** (70dcd0dd): “dispatchOf's comment claimed a family listed in familyOrder but absent”
- **Seen in #379** (b92870d6): “keyIndex reasons only from the budget test and silently drops expand's other memoize gate, so the doc claim”

### Back every performance, memory or noise figure with a measurement on the claimed workload
`measure-before-quantifying` · n=5 · PRs=5

- **Rule.** Do not claim a speedup, memory bound, noise immunity or quantified effect until a before/after measurement on the claimed workload shows it; a count bound described as a memory bound must be measured in retained bytes, and measured figures belong in the PR description with their revision, not in source comments where they go stale.
- **Why.** A per-walk view was kept for a benefit that never reproduced, a shared count bound let retained memory grow 3.4 times, and a 'no leverage' timing claim was disproved by a CI failure.
- **Probe.** Run go test -bench and a heap profile after runtime.GC on the stated workload before and after; compare retained bytes per charged unit; run timing tests under taskset load and record spread. Any number in a changed source comment is moved to the PR body.
- **Seen in #379** (b92870d6): “One bound covering both is what keeps a second memo from doubling the memory the first one was capped at”
- **Seen in #362** (8499518b): “reproduced — timings were identical and PointerPath already memoizes each”

### Check 'same', 'distinct' and 'shared' claims on both sides with differing inputs
`claims-that-two-things-agree` · n=5 · PRs=5

- **Rule.** When a comment says two traversals cover the same set, two code paths behave alike, two inputs share a behaviour, two derivations yield distinct results, or several input forms have the same effect, check each side separately, on non-fresh destinations and on inputs that differ only in what the derivation drops, and cite the code path for each.
- **Why.** Parity checked on one side or on a fresh value hid divergences: a populated destination decoded differently, URL templates differing only in punctuation collided, and only one of two directive spellings actually suppressed nothing.
- **Probe.** Write a two-row probe per claimed equivalence: run both paths on a pre-populated value, both input spellings, and a pair of inputs differing only in characters the canonicalisation drops; cmp.Diff the results.
- **Seen in #264** (009429dc): “brings TypeRegistry "in line with" its plain-map sibling registries; decoding”
- **Seen in #261** (3e25c4fa): “serverName's comment claimed the whole URL template "distinguishes exactly the”

### State in PR bodies and commits only the verification results actually observed
`pr-claims-match-evidence` · n=5 · PRs=4

- **Rule.** A PR body or commit message must report what was observed on the final tree: a fix it says landed must be in the diff, a mutation result must name the sites it was run at with per-site outcomes, and 'all routes' or 'dropped everywhere' must come from probing each route; generalising one probed site to its mechanism is an overclaim.
- **Why.** PR bodies here claimed fixes that had not landed, 'all six went red' when one did not, and that a pair was dropped on mounted items when it survived on every mounted operation.
- **Probe.** Before pushing, re-run each mutation and probe quoted in the body against the final test files and paste the observed output; for any 'every/all routes' sentence, list routes in code and run one input per route.
- **CLAUDE.md already says:** “Prefer proving a claim over asserting it”
- **Seen in #349**: “each went red, and all six were restored before the gate ran.”
- **Seen in #135**: “A mutation verified at one site was written up as holding for every site of its kind”

### Back every behavioural claim, scope boundary or known-wrong answer in a comment with a test
`comment-claims-held-by-test` · n=4 · PRs=4

- **Rule.** When a comment says something propagates automatically, that a measurement proves a property, that a check deliberately stops at a boundary, or that a known-wrong answer is left standing, add a test that fails if that statement stops being true (pin the known-wrong answer in a row naming its issue).
- **Why.** Unenforced claims rot silently: widening a check's scope or breaking the described wiring leaves every test green, and a benchmark no CI job runs checks nothing.
- **Probe.** For each such comment, make the claim false (drop the wiring, widen the scope, fix the known-wrong row) and confirm a test reddens; if the claim depends on a benchmark, confirm a CI step runs it.
- **CLAUDE.md already says:** “Prefer proving a claim over asserting it”
- **Seen in #111** (e511a0b7): “constant added to the block without a matching entry in Annotations() left”
- **Seen in #361** (b75eec57): “which nothing held: hoisting seen into checkNaming's closure made”

### Re-check a doc's references, renames and capability claims against its final state
`docs-consistent-after-edit` · n=4 · PRs=4

- **Rule.** After editing a doc, confirm every referenced type is defined, every 'see above/below' lands on its target, renamed terms left no residue (articles, plurals), and capability matrix cells match behaviour the same PR changed.
- **Why.** An undefined MessageRef, inverted cross-references, 'a emitter' and a matrix cell denying client streaming the PR enabled all shipped as stale prose.
- **Probe.** Grep backticked identifiers for definitions; grep the old term with neighbouring articles; for behaviour changes, grep docs/ir-spec-matrix.md for the capability and rerun the matrix test.
- **Seen in #352** (f1b7274f): “matrix still said OpenAPI reaches client streaming in no way at all, which the”
- **Seen in #3**: “and `MessageRef` was referenced but never defined”

## 2. Sweep every site the change touches

A defect is a property of its mechanism: sweep every site of it, every stale sentence about it, every route and spelling into it.

### Fix the mechanism at every site that runs it, not only where it was noticed
`sweep-every-site-of-the-mechanism` · n=24 · PRs=24

- **Rule.** When a diff fixes a defect, adds a guard, sanitizer or check, or closes an issue, name the mechanism and grep every code site that runs it (sibling hoisters, other callers of the old helper, the parallel request/reply set, every registry, every channel of the same text); fix each in the same PR or state the uncovered site in code and PR body. An issue's own site list and a single-instance framing are claims to check against the code, not the scope.
- **Why.** The pointer-collision fix left the same collision standing in the inline-position hoist, and same-mechanism sites deferred to follow-up issues kept the defect live on main while the PR claimed it closed.
- **Probe.** Before push, write the mechanism as a grep (the helper name, the literal, the guard shape) and run it across the tree; for each hit either show the fix or plant the reproducer at that site and watch it go red.
- **CLAUDE.md already says:** “Sweep every site of the mechanism before closing it.”
- **Seen in #157**: “is what left the identical defect standing in the inline-position hoist.”
- **Seen in #101** (fe10b45c): “schemaBody read nullability with schemaHasNull while the $ref site and the union”

### Retarget every comment, test name and doc that names a renamed, moved or deleted symbol
`sweep-old-name-after-rename-or-delete` · n=20 · PRs=17

- **Rule.** When a diff renames, moves, deletes or replaces a function, type, receiver, table, call path or mechanism, every doc comment, test name, package doc and docs/ page that names the old symbol or describes the old mechanism must be rewritten in the same change. A package doc is a claim about the package's current contents and must be re-read after code moves out.
- **Also.** After a rename, move or single-source constant, grep the old token as a plain substring (not a whole word) across the whole tree, docs and comments included, and route every remaining literal through the canonical spelling.
- **Why.** Comments here outlive the code they describe: they pointed at deleted functions (intern, fillParamDefault), removed receivers (l.ctx) and replaced resolvers, sending readers to code that no longer exists.
- **Probe.** For every identifier the diff removes or renames (git diff main -U0 | grep '^-' for func/type names), run git grep -n '<oldName>' over *.go, docs/, README.md and CLAUDE.md on the final tree; any hit outside the diff's own deletions is a finding. Repeat for the old mechanism's vocabulary (e.g. 'go/ast', 'parse', '..').
- **Seen in #195** (c5d2611f): “registeredNode's contract credited intern with recording a pointer's ID”
- **Seen in #834** (e5afd969): “fillParamDefault no longer exists: fillParamSchema assigns the value”

### Lower the same source construct at every position that can hold it and diff
`same-construct-at-every-position` · n=15 · PRs=12

- **Rule.** When a change lowers, preserves or reports a keyword or rule at one schema position, enumerate every position the spec lets it appear (named declaration, inline, property, parameter, header, allOf branch, beside a $ref, composed body, nested object, under an x- extension) and make each one reach the same path or record what it cannot carry. A carrier type with no field for a keyword must record it as residue.
- **Why.** One construct lowered two ways depending on where it appeared, and parameter, header and nested-object positions silently dropped keywords the property position kept.
- **Probe.** Compile one fixture that writes the same construct (with a sibling keyword and an x- key) at every position, then diff the IR and the diagnostic list per position; any position that loses something is the missed site.
- **Seen in #149** (909436cf): “A parameter whose schema is spelled {$ref: ...} inherited nothing from its”
- **Seen in #156**: “so one source construct lowered two ways depending on where it appeared”

### When behaviour or a contract changes, rewrite every comment that states the old behaviour
`sweep-comments-a-behaviour-change-falsifies` · n=12 · PRs=10

- **Rule.** A change that alters what code does (effect turned into a return value, a new refusal or error class, a new emitter of a diagnostic, a new detector, a new caller, a retired premise) must sweep the package for comments asserting the old behaviour, especially those using only, never, the single, the one, records or reports. Preconditions in doc comments are re-checked against every call site, including callers added in the same diff.
- **Why.** Converted functions kept saying they 'record' diagnostics they now return, and 'the single append point' / 'the only way' claims survived the change that made them false, so the doc contract contradicted the code.
- **Probe.** From the diff, list each behavioural verb that changed (record->return, refuse->report, dropped->lowered) and each new caller or class; git grep -nE 'only|never|single|the one|records|reports|refus' in the touched packages and read each hit against the new code. For a precondition comment, list callers with gopls references / git grep '<func>(' and check each, including new ones.
- **Seen in #198** (703208ef): “Four of the six converted functions still described themselves as”
- **Seen in #213** (c7c7ae45): “appendDiag forwarded one argument to l.diags.Append and its comment claimed”

### Update the normative doc row, plan or gate list that a change makes untrue, in the same change
`update-normative-docs-in-same-change` · n=8 · PRs=8

- **Rule.** When a change ships a lowering, diagnostic, Unmodeled key, degradation case, gate step or resequenced item, the docs/*.md row, README section, planning document or status line that describes it must be updated in the same change, and every clause of that row re-checked, not only the one the fix named.
- **Why.** ir-design.md and the spec matrix are the contract; when they lag the code (a missing warning, an unnamed key, a 'not yet implemented' status beside landed rows, a gate list missing a step) contributors build against a false spec.
- **Probe.** For each new diagnostic code, Unmodeled key, gate step or lowering in the diff, git grep docs/ README.md CLAUDE.md for a neighbouring code/key/step name and confirm the new one sits beside it; for each doc row the diff touches, re-run the case for every clause of that row on the final tree.
- **Seen in #310** (cd5ea287): “The additionalOperations clause added with this change described the”
- **Seen in #345** (9b052f37): “error branch they were being dropped on, and that x-* is simply preserved,”

### Cover every spelling the spec allows for one concept
`every-spelling-of-one-concept` · n=8 · PRs=7

- **Rule.** When a change reads a concept the source can spell several ways (keyword nullability versus a null union branch, schema versus content, ASCII versus non-ASCII twins, both orders of an adjacency), write one case per spelling and assert they lower and diagnose identically.
- **Also.** For each construct a change reads, enumerate the spellings the governing spec admits -- case-insensitive tokens in another case, tilde and backtick fences, tables without outer pipes, a $ref versus a YAML alias for the same reuse -- and require the same IR, severities and exit codes for each.
- **Why.** The null-spelling fix handled keyword spellings only, so a null oneOf branch dropped Nullable at every reference, and a header written with content lowered as untyped.
- **Probe.** Build a table of equivalent spellings for the concept and assert identical IR and diagnostic codes across the table; re-run every twin pair after any reader changes.
- **Seen in #101** (e86cadfe): “spellings, so a target spelling nullability as a union null branch lost the bit”
- **Seen in #185** (b25e38bc): “header lowered as if it had no schema at all: the type became t/prim/any and”

### Probe every raw-YAML reader with alias, anchor, merge-key, tag and quoted spellings
`yaml-alias-anchor-merge-spellings` · n=8 · PRs=7

- **Rule.** Any code that reads raw YAML nodes or mirrors the parser's rules must resolve aliases, anchors, merge keys and tags the way the parser does, and must not treat quoted or aliased non-special spellings as special. Prefer the raw node or the library call over a hand-written copy of the rule.
- **Why.** Readers matched keys by raw node text or skipped anchor-valued pairs, so aliased, anchored or merged entries were dropped with exit 0, while quoted '<<' keys raised a false cycle.
- **Probe.** Feed each touched reader a fixture set: key as alias, value as alias, anchored entry, '<<' merge, quoted '<<', !!merge tag; diff IR and diagnostics against the plain spelling.
- **Seen in #500** (f91f5a3d): “On `main`, each of these compiles clean, exits 0, and loses the anchored entry:”
- **Seen in #356** (6fcfd8ab): “the anchor; searching it raw found nothing, and a key with no node kept”

### Update every doc copy of a fact the change alters, derived from the tree
`docs-restating-a-fact-move-with-it` · n=8 · PRs=6

- **Rule.** When a change moves packages, adds a command, departs from a design item or corrects a fact, grep every document and checklist that states it and fix each in the same PR; derive lists from ls, the package tree, or the Makefile rather than memory.
- **Why.** Layout tables in README, CLAUDE.md and architecture.md drifted apart, and a pre-flight list omitted the 100% coverage gate.
- **Probe.** Diff each documented list against its derivation (ls docs/*.md, git ls-files package dirs, Makefile gate targets) and grep docs for a sibling entry the new one should join.
- **CLAUDE.md already says:** “Restating it here would only give it a second place to rot”
- **Seen in #227**: “README carries a layout table of its own, and this PR was about to leave it contradicting the one it fixed.”
- **Seen in #301**: “The normative-docs list named four of eight documents.”

### Make every input route reach the new guard, path or oracle
`every-input-route-reaches-the-guard` · n=7 · PRs=7

- **Rule.** When a guard, transformation, validation or new branch is applied on one route into a consumer, list every other route into it (external $ref documents, overlays, callbacks, direct versus behind a $ref, harness oracles that take no options) and confirm each reaches it, or state the uncovered route where the next reader will see it.
- **Why.** The anchor release and alias refusals protected only the root document, so overlays and externally referenced documents still crashed or lost entries with exit 0, and the harness oracles never saw the overlay path.
- **Probe.** Run the same negative fixture through each route (wrap it in an external $ref, apply it as an overlay, reach it through a callback) and diff the diagnostics and IR against the direct compile.
- **CLAUDE.md already says:** “A check that runs is not a check that reaches.”
- **Seen in #556**: “A document an external `$ref` names never passed through that”
- **Seen in #492**: “The source has been refused for both shapes since #12 and #27.”

### Decide and test the combination when keywords co-occur
`test-co-declared-keywords-together` · n=6 · PRs=5

- **Rule.** When a lowering reads several keywords that may be co-declared (prefixEncoding with itemEncoding, readOnly with writeOnly, minimum with exclusiveMinimum, allOf with oneOf, explode with an empty style, siblings a rewrite target cannot carry), test the pair directly; a dropped partner goes to Unmodeled or a diagnostic, never only into a message.
- **Why.** Ordered early-return guards and single-path lowerings kept one keyword of a pair, so {minimum:10,exclusiveMinimum:0} and {minimum:10} compiled byte-identical.
- **Probe.** For each keyword pair the code reads, compile the pair, each keyword alone and neither, and diff all four documents; two identical outputs for different inputs is the drop.
- **Seen in #346**: “The displaced keyword went nowhere else.”
- **Seen in #322**: “guarded the two flags in order and returned on”

### Enumerate the spec versions, locations and override levels a rule depends on, and fixture each
`cover-every-version-location-and-level` · n=5 · PRs=4

- **Rule.** When a value's source or legality varies by spec version, parameter location or declaration level, list every value of that axis and test it: a name from a 3.2-only field needs a 3.0/3.1 fallback, every IR entity kind needs a naming channel, the most specific override level wins, a default must be legal at its exact location, and fixtures must respect the format's co-location prohibitions.
- **Why.** Every 3.0/3.1 server and every response reached the IR nameless, operation-level servers were dropped in favour of the superseded path-item list, a querystring parameter got an invented style the spec forbids, and a fixture violated 3.2's querystring rule.
- **Probe.** Compile one document per supported spec version and one declaring the keyword at both path-item and operation level; grep the IR for empty names per entity kind and check each emitted default keyword against the spec's legal set for that location.
- **Seen in #261**: “`Server.Name` was filled only from the OpenAPI 3.2 `name` field, which a”
- **Seen in #272** (c9d38d0f): “Only the path item's were read, so a document declaring both kept the”

## 3. Derive, never maintain

A hand-kept copy of a count, list or fact drifts silently; derive it from its source and hold the two together both ways.

### Delete hand-maintained counts, issue states and versions from comments and docs
`no-maintained-counts-or-state-in-prose` · n=19 · PRs=14

- **Rule.** A comment or doc must not state a count of fields, cases, packages, rules, refusals, policies or commits, nor an issue's open/closed state, unless the number is derived by a command named beside it or pinned to a revision; name the members or the mechanism instead, and never put a count next to the list it counts.
- **Also.** A doc comment must not list the callers, cases, invariants, reference classes or categories of a set that code defines; state the property or rule that derives membership and point at the source of truth. A list that is kept must cover the whole set.
- **Why.** Every such count in this repo went stale silently ('thirteen' internal packages when there were fourteen, 'four residual shapes' when there were five, 'one of the two' policies after a third merged in), and no test can catch prose.
- **Probe.** git diff main...HEAD | grep -nE '^\+.*\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|[0-9]+)\b' over comments and docs; for each hit, derive the number from the code (ls, grep -c, go list) and either replace it with the command or delete it.
- **CLAUDE.md already says:** “Derive counts; never maintain them.”
- **Seen in #301**: “"openapi has thirteen" internal packages.”
- **Seen in #119** (5623483e): “§4.8 listed four residual shapes. There are five: a branch whose `$ref` names no”

### Derive any list that mirrors code instead of maintaining it by hand
`derive-lists-from-their-source` · n=15 · PRs=9

- **Rule.** A hand-written list of fields, carriers, packages, classes or keys that mirrors a type graph, a writer, or the tree must be derived from it (reflection, AST, the writer's own result) or diffed against it by a test that fails in both directions.
- **Also.** A completeness test, sealed-sum guard or golden list must derive its member set from the declarations, the production table or one shared file walk, never a hand-written slice; a fixture directory and its capability table must each fail on an entry the other lacks; a test claiming 'the only X' must enumerate every X.
- **Also.** When a guard audits packages, kinds or files, derive the list from the sources and fail on any member missing from both the list and the exemptions; when the targets are discovered by parsing, cross-check the parse against an independent count so a narrowed parse fails instead of silently shrinking coverage.
- **Why.** A hand-written reference-field list drifted behind the IR and an announcement predicate restated the writer's construct list and omitted parameters, so new members went unchecked or unannounced with the suite green.
- **Probe.** Add a member to the source (an IR field, a writer construct, a package) in a scratch edit without touching the list and confirm a test reddens; remove one from the list and confirm the same.
- **CLAUDE.md already says:** “so adding a kind without updating a list reddens there instead of quietly narrowing what a test covers”
- **Seen in #119** (836f1020): “checkDanglingTypeRefs was documented as reporting every dangling”
- **Seen in #382** (a498517b): “`pathItemDeclaresAnything` is a hand-maintained mirror of `applyPathItem`'s construct list, so the next construct added there silently loses its announcement.”

### Derive package, file, import and feature lists in docs from the tree, not memory
`derive-inventories-from-the-tree` · n=11 · PRs=8

- **Rule.** Prose that lists packages, docs files, import edges, allowlists, oracle stages, method implementers or implemented passes must be derived from the tree, the archtest rules map, the checker's body or the import graph, and must mark planned items as planned rather than presenting them beside built ones.
- **Why.** Layout and inventory prose here omitted real packages, named a directory an issue was removing, listed unbuilt passes as present, and understated harness.Check's oracles, sending readers to rebuild mechanisms that already existed.
- **Probe.** Run git ls-files '*/*.go' | xargs -n1 dirname | sort -u, ls docs/, go list -deps, and read archtest's rules map and harness.Check's body; diff each against the doc's list. For each listed pass or feature, grep for its exported entry point.
- **CLAUDE.md already says:** “Read them off the tree”
- **Seen in #301**: “The oracle list named five oracles”
- **Seen in #837** (24548dd7): “internal/leakcheck had been missing from all three since it landed in #535”

### Make two implementations that must agree derive from one source or diff them on shared edge cases
`parallel-implementations-agree` · n=8 · PRs=8

- **Rule.** When a parser, derivation, seed list or validity rule exists in two places (two tokenizers, two tests' filters, a table test and a fuzz seed, a writer and its verifier), compute it once in a shared helper or run both over a shared edge-case table and assert identical results; when the writer starts refusing input, the checker refuses it in the same change.
- **Also.** When a pre-scan, validator or helper parses the same string (pointer, fragment, URI) that a downstream resolver later reads, it must call the resolver's own normalisation and acceptance rule; any second hand-rolled copy must be deleted or pinned by a shared input table and a differential fuzz target.
- **Why.** Copies drift: a lone '/' read as a token by one walk and the root by another, and irverify accepted strings the encoder refuses.
- **Probe.** Grep for a second implementation of the changed logic; run both over empty, lone separator, no leading separator and length-bound cases and diff; feed the writer's refused inputs to the checker.
- **Seen in #362** (fe7ccf50): “a lone "/" as one empty token where the sibling walk reads it as the”
- **Seen in #508** (9d56cb6b): “#507: `irverify` passes a string that is not UTF-8, which the encoder now refuses.”

### Point at the one definition instead of restating a fact, table, version or rationale
`single-source-no-restatement` · n=7 · PRs=7

- **Rule.** A fact that has a single definition (README's milestone table, go.mod's go directive, a coverage result, a rationale documented beside its mechanism, a canonical example) must be linked, generated or referenced, never copied into a second place; the same sentence stated twice in one comment is also a defect.
- **Why.** Copies drifted from their source: the README's Go version and badge disagreed with go.mod, a hard-coded 100% badge could never go red, and duplicated examples and rationales diverged between files.
- **Probe.** For each literal version, metric, table or rationale paragraph the diff adds, git grep for the same fact elsewhere (go.mod, README tables, the mechanism's own comment); if a source exists, replace the copy with a pointer or generated value. For a badge or status, confirm something can make it fail.
- **CLAUDE.md already says:** “Restating it here would only give it a second place to rot”
- **Seen in #760**: “It pinned 1.26 while `go.mod` names 1.27.1.”
- **Seen in #324**: “asserted 100% unconditionally and could not go red”

### Derive lookup keys and IDs canonically, through the same function the definition used
`keys-canonical-single-derivation` · n=4 · PRs=3

- **Rule.** A key or ID that must match a definition is computed through the one function that minted it, from the identifier the target actually stores, after normalising every equivalent spelling (escapes, percent-encoding); grouping must use every identity an item can carry.
- **Why.** Raw pointer spellings, a shortcut namedTypeID for empty names, or IDs re-derived from a composed pointer produce keys matching no interned node, leaving dangling references.
- **Probe.** Feed equivalent spellings (~ vs ~0, %7E, empty component name, $ref'd vs inline) and assert they resolve to the same ID; run irverify's no-dangling-refs check over each; grep for a second derivation of the same ID kind.
- **Seen in #22** (79422772): “always resolved to the component's namedTypeID”
- **Seen in #22** (79422772): “resolveComponentRef built the resolved ID from the raw $ref pointer text,”

## 4. Lossless lowering and honest diagnostics

Invariant 2 in practice: every source construct lands somewhere, and every diagnostic is true, located, and decided from what actually happened.

### Point every diagnostic at the node and document it is about
`diagnostic-provenance-names-the-real-site` · n=19 · PRs=13

- **Rule.** A diagnostic's provenance must name its own site (not the document root or the holding node), resolve to a node that exists, use a source index the table holds, name the document the position was read from, and keep distinct sibling sites distinct so deduplication cannot merge them.
- **Also.** Provenance for a sub-entry points at its own keyword, and for a node added by an overlay or merge layer names that layer's document; a combined entry is attributed by the positions that wrote its parts. Lower layers must get attribution through an injected locator, not the raw base source index.
- **Also.** A provenance pointer or verifier location must address a node that exists: compose it from the declaration the value was read from (not a $ref mount or a reflection-only struct path), and two checkers reporting the same code must spell the location identically.
- **Why.** Root-stamped notices for several unmounted items deduplicated into one naming none, and chained $ref findings sent readers to a line in the wrong file.
- **Probe.** Resolve every emitted pointer through the parsed source in a test; compile two sibling instances and assert two distinct diagnostics; exercise the diagnostic behind a chained $ref and an overlay graft.
- **Seen in #382** (a498517b): “`provenance: svc.Provenance` is the document root, so every unmounted item's servers diagnostic is byte-identical”
- **Seen in #565** (3d0e08e3): “A finding reached through a $ref put its line and column against "the”

### Make each diagnostic's message, code and severity match the case that fired
`diagnostic-text-names-what-fired` · n=13 · PRs=12

- **Rule.** Every branch, position and failure cause of a diagnostic needs wording true for that branch: name the specific cause, the earlier duplicate, the unit it counts and the correct repair; reuse a code only when its documented meaning fits; set severity from what the input means.
- **Why.** Messages described allOf branches for sibling positions, blamed the wrong failure direction, rendered blank slots, and reused a code whose documented meaning consumers select on.
- **Probe.** Trigger every branch and severity of the diagnostic, including zero-valued inputs and both directions of a symmetric check, and read each rendered message against the fixture.
- **Seen in #92** (0eb9ac18): “alongside allOf rather than inside a branch.”
- **Seen in #332** (20007a20): “A worker that stopped partway through left zero values in its results, and the”

### Prove each keyword the reader accepts changes the output
`twin-diff-every-accepted-keyword` · n=9 · PRs=7

- **Rule.** Every source keyword or field a change reads, lists, documents or comments on must reach a typed field, an Unmodeled entry or a diagnostic. A scanner list, a field doc comment naming a keyword as its home, or a census comment saying the rest is captured is a claim to test, not evidence.
- **Why.** Fields such as allowReserved, propertyNames, minItems and x-* on most objects compiled to byte-identical IR with or without them, which breaks the lossless-by-default invariant with a clean exit.
- **Probe.** For each keyword in the vocabulary, compile two specs that differ only in that keyword and diff the IR plus diagnostics; byte-identical output is a silent drop. Enumerate the keywords from the source schema, not a hand-picked sample.
- **CLAUDE.md already says:** “Compile the input; inspect the emitted IR *and* the full diagnostic list.”
- **Seen in #345**: “and nothing read it: two documents”
- **Seen in #348** (ccd0fc24): “minItems, maxItems and uniqueItems reached the IR in no form at all wherever”

### Report malformed or unusable input instead of mapping it to a valid-looking value
`no-silent-fallback-to-a-valid-looking-value` · n=7 · PRs=7

- **Rule.** A parser fallback, default switch arm or sentinel must never turn unreadable, forbidden or unusable input into a value that looks deliberate ({0,0} status, empty AuthRequirement, guessed kind, empty method); emit a diagnostic and a no-value result, and switch over closed enums without a guessing default.
- **Why.** A typo'd responses key arrived as a declared default and a malformed security entry granted public access, both with no diagnostic.
- **Probe.** Compile each malformed or forbidden spelling the parser can meet and diff its IR against the deliberate spelling; identical IR or an empty diagnostic list is the defect.
- **CLAUDE.md already says:** “Never accept garbage silently.”
- **Seen in #327**: “was lowered to `ir.AuthRequirement{}`, the encoding `ir-design.md` §9 reserves”
- **Seen in #263**: “A `responses`-map key the compiler could not read was folded into the `{0, 0}` status”

### Make every early return, fast path and carrier constructor carry the shared path's data
`fast-paths-carry-what-the-shared-path-does` · n=7 · PRs=5

- **Rule.** When a helper returns early (existing owner, interned node, zero children) or a new construct builds its carrier inline or calls a shared constructor, check it applies everything the main path applies: every argument at every call site, every fill function, every hook.
- **Why.** An early return on an existing owner and an interning short-circuit skipped the constraint path, and an inline-built header Property skipped the detail filler, dropping docs, extensions and constraints silently.
- **Probe.** List every return before the shared step and every call site of the constructor; write a case that reaches each with the node already present or the child count zero, and diff against the main path.
- **Seen in #363**: “Ownership is what made it terminal: `hoistDeclarationHome` resolves to whatever node the”
- **Seen in #149** (70d1ad0f): “the hoisted node was built without them and the interning short-circuit meant the”

### Merge redeclared fields per attribute and test both orders
`merge-each-field-by-rule-in-both-orders` · n=6 · PRs=6

- **Rule.** A merge or reconciliation of duplicate declarations, or two writers into one field or map, needs an explicit rule for every field of the struct; never keep one side wholesale, never assign a zero value over another source's value, and never skip a shared-map write because some other construct already holds the key.
- **Why.** Reconciliation kept the first allOf branch's constraints and visibility wholesale, so the result depended on branch order and later branches' keywords vanished with no diagnostic.
- **Probe.** List the struct's fields beside the merge and point at the rule for each; then compile complementary keys from each side in both declaration orders and cmp.Diff the documents.
- **CLAUDE.md already says:** “Order-dependence needs a two-order diff.”
- **Seen in #8** (4a0c13d5): “and the surviving shape depended on branch order.”
- **Seen in #92** (0eb9ac18): “Reconciliation also took the first branch's constraint set wholesale, so”

### Pair every new check with must-stay-silent fixtures for valid input
`check-stays-silent-on-valid-input` · n=6 · PRs=5

- **Rule.** A conflict detector, refusal or structural guard must flag only what it can prove wrong: test satisfiable narrowings, opaque types, every valid spelling a spec version allows, indirections the compiler unwraps, and file kinds it never reads, and run it over the corpus before merge.
- **Why.** New checks rejected legal multipart bodies and valid 3.0 boolean bounds, and warned on allOf narrowings that are satisfiable.
- **Probe.** For each check write a must-fire and a must-stay-silent fixture, including alias/$ref indirection and every valid spec-version spelling, then run morphic-harness over testdata and require no new diagnostics.
- **Seen in #149** (2fac7fce): “The encoding-key check added alongside the typed key rejected legal specs, so”
- **Seen in #92** (0eb9ac18): “so refining a field in a later allOf branch was reported as a contradiction.”

### Own every refusal and bound explicitly; never rely on a dependency or another bug to provide it
`explicit-guard-not-incidental` · n=4 · PRs=4

- **Rule.** A boundary check must be the compiler's own explicit check, not a dependency failing or a misreading that happens to refuse. When deleting or loosening a path that declined some inputs or incidentally bounded recursion, add the explicit bound or refusal in the same change and re-probe every input class the old path handled.
- **Why.** A self-$anchor reference crashed the process with a stack overflow on main because only a misreading refused one shape; lifting an anchor skip let a fixture grow past 57 GB; deleting a byte scan exposed prose files claimed as broken specs.
- **Probe.** Probe the dependency directly with the input class you rely on it to refuse; revert the incidental guard and run its covered inputs under a memory/time limit (ulimit, -timeout) on both sides of any old size threshold.
- **CLAUDE.md already says:** “validate at every public boundary”
- **Seen in #185** (c721f4f9): “the resolver library failing rather than from any check of its own.”
- **Seen in #541** (82454bc8): “The library crashes the process with an unrecoverable stack overflow”

### Decide a kept/dropped announcement from what was written
`announce-kept-only-after-the-write` · n=4 · PRs=3

- **Rule.** A diagnostic that says something was kept, preserved or dropped must be decided from the output that was actually written, after the write succeeded; a rebuild that replaces a node must retract the superseded build's diagnostics and Unmodeled entries.
- **Why.** Warnings claimed a preservation that never happened when a conversion failed or a merge key hid the construct from the raw reader.
- **Probe.** Force the write to fail (bad YAML-to-JSON value, merge-key-supplied construct) and assert the 'kept' diagnostic disappears; diff the Unmodeled map against what each announcement claims.
- **Seen in #382** (4c06ee4e): “The warning asserts a preservation that did not happen: it is gated on what the *source model* wrote, not on what was actually kept.”
- **Seen in #185** (0705181d): “nodeToRaw returned nil both for an absent construct and for one whose YAML”

### Lower into the typed field the IR has, honour design-promised lowerings, and pick the Unmodeled reason by the IR's actual strength
`unmodeled-reason-and-typed-home` · n=4 · PRs=2

- **Rule.** Before preserving a keyword raw, check the IR for a typed field; when the design doc promises a lowering plus fallback, compile each promised case; classify a preserved entry by whether the IR reflects it weaker, stronger or not at all, and test the classification on the construct's most common real-world form.
- **Why.** itemEncoding was preserved raw despite a typed field, $dynamicRef lowered silently to any against §4.7, an open tuple was tagged no_ir_home, and a required-narrowing oneOf was tagged degraded_lowering so validation emitters skipped it.
- **Probe.** For each keyword the change preserves, grep ir/ for a field that holds it; compile each case the design doc names for the construct and assert the promised IR kind and UnmodeledReason, never the top type.
- **CLAUDE.md already says:** “Every `Unmodeled` entry records *why* it was kept (`UnmodeledReason`)”
- **Seen in #119** (8e357a79): “the compiler preserved itemEncoding raw under extensions instead and reported”
- **Seen in #149** (c1aaab76): “$dynamicRef. It lowered silently to any, contradicting both halves of §4.7's”

### Fail when a loader or parser reads less than it was asked to
`loaders-fail-when-they-read-less` · n=4 · PRs=2

- **Rule.** A source loader, package loader or document parser must error when its result is smaller than its input: a pattern matching nothing, a build-excluded file, a test-only package, or a line it cannot place.
- **Why.** Checkers reported clean on code they never saw, and the matrix reader dropped every row below an unexpected line.
- **Probe.** Feed the loader a pattern matching nothing, a build-tagged file, a test-only package and a malformed line mid-table, and assert each errors rather than returning a shorter result.
- **Seen in #837** (1db27104): “A file the current build leaves out was not read. A declaration in”
- **Seen in #342** (ff7909bc): “The matrix reader ended the table at the first line not starting with a”

### Produce exactly one finding per defect
`one-defect-one-finding` · n=3 · PRs=3

- **Rule.** When a new check overlaps an existing one (a generalised check, an overriding reader, a scanner over delimiter runs), retire or suppress the overlap for the whole class in the same change.
- **Why.** A bad numeric bound and an ill-formed string each drew two reports, inflating counts and confusing which check owns the defect.
- **Probe.** Plant each defect the overlapping checks cover and assert the diagnostic or violation list holds exactly one entry for it.
- **Seen in #539** (915f28ea): “Keeping them would report every such string twice.”
- **Seen in #23** (32c99973): “bound keywords unconditionally, not only for in-range values. Morphic re-reads”

### Prove every return path and caller delivers its diagnostics
`every-return-path-delivers-diagnostics` · n=3 · PRs=3

- **Rule.** When a function's diagnostics must survive every return path or every caller, add a test per path and caller that fails when that report is removed; before deleting a diagnostic as redundant, show the other stage reaches every case including early exits and panics.
- **Why.** A refusal path omitted the stream-drop report and callers could drop the operation-extension warning with no test failing.
- **Probe.** Delete the report on each return path and each caller in turn and confirm a test reddens.
- **Seen in #480**: “drop omitted on the refusal path → survived until the refusal-path test was added, then red.”
- **Seen in #565** (fd3cc339): “reaches: a resolver panic ends the walk, and an entry after it was then”

### Read every ok flag, error and exit status, and report an impossible miss instead of returning silently
`check-every-failure-signal` · n=3 · PRs=3

- **Rule.** Every failure signal -- a comma-ok flag, an invariant-guaranteed lookup miss, a shell command's exit status in a Makefile recipe -- must be checked and surfaced as a diagnostic or a stopped recipe; never continue on a value whose failure was ignored.
- **Why.** Callers returned silently when an interned node was missing, dropping docs and examples with no diagnostic; discarding an ok flag would have produced a tag no component answers to; an unchecked mktemp ran the whole suite with an empty -coverprofile.
- **Probe.** grep the diff for `_ :=`, `, _ =`, bare `return` in lookup-miss branches and unchecked command substitutions in recipes; force each failure (delete the node, make mktemp fail via TMPDIR=/nonexistent) and confirm a diagnostic or non-zero exit.
- **CLAUDE.md already says:** “comma-ok on every type assertion”
- **Seen in #119** (876116e1): “and returned in silence when it was missing”
- **Seen in #828** (babe91b3): “The recipe mints a profile path with mktemp, but recipe lines run without”

### Strip null into nullability and derive attributes from the surviving members, testing null in every position
`null-member-handling` · n=3 · PRs=1

- **Rule.** When a spelling encodes nullability as a list member (type arrays, enum members), move the null into the nullability bit, derive kinds from the surviving members, and test null first, middle and last; a shared nullability predicate must also be tested on a schema whose enum or sibling excludes null.
- **Why.** A nullable enum in the 3.1 type-array spelling degraded to a union of literals, a leading null fixed the member kind so the enum degraded, and references admitted null that the enum forbade.
- **Probe.** Compile {type:[string,null], enum:[null,a]}, the same with null last, and {type:[string,null], enum:[a,b]}; assert an Enum kind, correct nullability per reference, and no degradation diagnostic.
- **CLAUDE.md already says:** “Optionality ≠ nullability.”
- **Seen in #267**: “`enumMembers` classified that member as inadmissible, so the whole enum fell to the”
- **Seen in #267**: “Member kinds are reconciled against the members actually kept”

### A keyword must land in the same IR place, never dropped, whichever lowering or guard handles it
`keyword-placement-path-independent` · n=2 · PRs=2

- **Rule.** When the same keyword or annotation can reach the IR through several lowerings or past a dedup guard, its placement must not depend on the path; a guard deciding to skip residue may only run after confirming the data has a home elsewhere.
- **Why.** Visibility residue was dropped exactly for parameters whose schema owns a node, and a constraint beside allOf went to Unmodeled while beside properties it went to Constraints, defeating lossless capture.
- **Probe.** Compile the keyword beside every container shape (properties, allOf, $ref, parameter with and without an owned node) and diff where it lands; confirm none disappears.
- **CLAUDE.md already says:** “Lossless by default, lowered late.”
- **Seen in #149** (ba887c58): “dropped them for precisely the parameters whose schema owns a node”
- **Seen in #363**: “a constraint written beside `allOf` is kept under `Unmodeled` rather than in”

## 5. Identity, order and determinism

Invariants 3 and 7 in practice: nothing about an ID, name, cache answer or output order may depend on which site was walked first.

### Decode pointer tokens and test name derivation for every position kind and escape
`pointer-token-names-decoded-and-total` · n=11 · PRs=7

- **Rule.** A name, hint or wire value derived from a JSON pointer or $ref token must be unescaped (RFC 6901 and percent-encoding) where extracted, must not use indexed or keyword-like tokens (0, items, properties) as the node's name, must branch on inline versus component targets, must never return empty for non-empty input, and word separators must be the complement of the word class rather than an enumerated list.
- **Also.** Every helper that splits, walks or keys on a JSON Pointer or URI fragment must handle the empty token (only the leading one is a split artifact), the lone "/" per the caller's meaning (reference root versus empty-keyed member), escaped tokens, and percent-decoding exactly once.
- **Why.** Raw tokens leaked into the IR: Cat/Dog became discriminatorValue Cat~1Dog, union variants were named '0' or 'items', invalid UTF-8 fragments hinted 'empty', and punctuation survived into canonical words.
- **Probe.** Compile a fixture whose schema names contain ~, /, %, non-UTF-8 bytes and structural keywords (items, properties, allOf), referenced both inline and via $ref, as union variants and allOf branches; read every Naming, hint and discriminatorValue in the IR.
- **Seen in #515** (66d2f196): “Names read off a pointer's last token were taken raw: still RFC 6901-escaped and, from a `$ref`, still percent-encoded.”
- **Seen in #553**: “took the pointer's last token”

### Derive every attribute of a shared node from its own declaration, never from the first route to reach it
`shared-node-attributes-from-declaration` · n=7 · PRs=6

- **Rule.** When a node can be interned or named by more than one route (its declaration, an outside $ref, a composition branch, an {X, null} collapse), every attribute written on it -- name hint, owner, home -- must come from the node's own location through the same normaliser every route uses. A value taken from the enclosing schema, the use site, or the raw caller hint makes the IR depend on which route lowered first.
- **Why.** Shared components lowered first by a reference took that reference's hint, raw or un-neutralised, so two declaration orders produced different documents with no diagnostic and pass/validate clean on both.
- **Probe.** For each new or touched interning path, write a spec where an outside $ref reaches the node before its declaration and one where it comes after, compile both and cmp.Diff the full documents including hints; also grep every writer of the field and confirm each routes through the same normaliser.
- **CLAUDE.md already says:** “Order-dependence needs a two-order diff.”
- **Seen in #108** (e1345613): “A shared component's hoisted schema takes its name hint from the component”
- **Seen in #354**: “handed the surviving branch the *enclosing* schema's hint,”

### Key every cache, visited set and dedup on the full identity
`cache-and-dedup-keys-carry-full-identity` · n=6 · PRs=6

- **Rule.** A visited set, cache, skip or identity key must include everything that distinguishes the reading (node plus role or kind, the spec's own uniqueness key, the canonical URL); a change to cache admission or reuse must reproduce the first answer, diagnostics included.
- **Why.** A shared visited set hid a node's second role, a URL respelling missed the cache, and cache reuse turned an error into a warning.
- **Probe.** Compile the same node read two ways, the same URL respelled, and a fixture across the cache-budget boundary, and diff both IR and diagnostic codes.
- **Seen in #94** (91659e59): “a shared set let the first role mark it seen so the second skipped it — dropping”
- **Seen in #562**: “so an unrelated respelled `$ref` turned that compile's”

### Never let map iteration, concurrent completion or string sorting decide an order
`iterate-and-sort-by-stable-key` · n=6 · PRs=5

- **Rule.** Any walk, decode, selection or test table that reads a Go map must iterate in sorted key order; results from concurrent parses must not be ordered by token.Pos; sort by numeric fields or source order, never by a formatted string that embeds numbers.
- **Why.** Randomised map order changed recorded paths, the first decode error, the node explain printed and subtest order between runs; token.Pos depended on which parse finished first; "file:14" sorted before "file:6".
- **Probe.** grep the diff for `range` over maps and sort.Slice/slices.Sort on strings; run the affected test or command with -count=50 and compare outputs; add a case with two candidates sharing a key and assert which one is chosen.
- **CLAUDE.md already says:** “maps emitted in sorted-key order”
- **Seen in #119** (a130ad61): “Both reference walks prune a shared pointer at whichever referrer they reach”
- **Seen in #822** (addef145): “the registry unsorted left every test green while the printed node changed”

### Run a two-order diff for every construct reachable by two routes, and never let fixture order hide a defect
`two-order-diff-every-reachable-construct` · n=5 · PRs=5

- **Rule.** Any lowering that reuses interned results, merges operands, or resolves a lookup a collection can name twice must be compiled with declaration (or operand) order swapped and the documents diffed. A fixture declared in the one order that dodges a known order bug must cite the filed issue, and an order dependence left standing must be named in the code and PR body.
- **Why.** Single-order tests pass on both orders of a colliding lowering, so inline-position homes, mapping keys, allOf visibility intersections and refused-pointer reuse all drifted with declaration order while the suite stayed green.
- **Probe.** Copy the PR's fixtures, reverse component/path/mapping-key/allOf-operand order, compile both, cmp.Diff the documents and diagnostic lists; then revert the fix and confirm the reversed-order case reddens.
- **CLAUDE.md already says:** “A fixture's own declaration order is part of the test.”
- **Seen in #119** (3ce90323): “fix(compilers/openapi): make an inline position's home order-independent”
- **Seen in #540** (f1ba9e6c): “Declared before its target, it has nothing to reuse and lowers to `any`.”

### Make every source-to-ID derivation injective and give minted nodes their own namespace
`derived-identity-must-be-injective` · n=5 · PRs=5

- **Rule.** A derived ID or lookup key must never let two distinct sources map to one value: synthesized nodes take IDs in a namespace no source pointer can produce, registries refuse a second distinct key deriving the same ID, lookups use the declaring package's namespace, and lossy byte conversions are either injective or guarded by a pinned upstream refusal.
- **Why.** A union variant interned at a pointer a $ref also names raced for it silently, a coordinate-keyed registry overwrote one node with another, U+FFFD rewriting could give two keys one pointer, and a flat bare-name table matched another package's method.
- **Probe.** For each new ID derivation, construct two distinct sources that could collide (a $ref to the minted pointer, two non-UTF-8 keys, two packages with the same type name) and assert the registry either keeps both or emits an error; round-trip a non-UTF-8 key through the token builder and require the same bytes back.
- **CLAUDE.md already says:** “a minted node needs a namespace of its own.”
- **Seen in #119** (668162cf): “raced for it: whichever lowered first won, and the loser silently became the”
- **Seen in #185** (9bd2337e): “the second node simply overwrites the first in”

### Follow reference chains to their end, inside their scope, and derive key and value from that one target
`resolve-to-true-target` · n=5 · PRs=3

- **Rule.** Any consumer that reads through a reference or alias must follow the bounded chain to its end (not one hop), stop at every keyword that opens a new scope ($id), adopt a walked pointer only if its final node exists in this document, and derive a key and the value it addresses from the same resolved target.
- **Why.** One-hop resolution hoisted aliases under pointers that addressed nothing and made pass.Validate reject valid annotated hierarchies; an encoding key and its body came from different hops; $dynamicRef looked through an $id boundary.
- **Probe.** For every resolving path touched, add a two-hop alias chain, an alias chain that leaves the document midway, and a nested $id between the reference and its anchor; compile and inspect the IR pointers and diagnostics.
- **Seen in #108** (ec7a9ace): “declaration-pointer rule, because the resolution stopped after one hop.”
- **Seen in #149** (2fac7fce): “And the key itself was minted from one hop off the literal $ref while the body”

### Place and identify each finding by a declaration-independent key, not by walk order or position
`finding-placement-declaration-independent` · n=4 · PRs=4

- **Rule.** When several sites can draw the same diagnostic, choose the reported site by a stable key (least pointer, the declaration itself), never by which claim lowered first or second or by walk order. When comparing findings across a permutation, identify them by code, message and structural pointer, never by line:col.
- **Why.** operationId conflicts, reached-validation findings and budget crossings moved when two paths were reversed, which morphic-harness flagged as order dependence; an oracle keyed on line:col reported every permutation as a difference.
- **Probe.** Reverse the paths or $refs that reach the finding's target, recompile, and diff the diagnostic list (code, message, pointer); it must be identical.
- **CLAUDE.md already says:** “Order-dependence needs a two-order diff.”
- **Seen in #548** (4e5ae2fa): “reported whichever claim was lowered second”
- **Seen in #565** (3e2a2d0d): “A finding in a target several $refs reach was reported at the first of”

### Cache only results that cannot depend on the path or depth that computed them
`memo-only-path-independent-results` · n=3 · PRs=3

- **Rule.** A memo or shared view that outlives one walk may store a verdict only when that verdict is complete and independent of the entry path: never memoise a result computed under a truncation/depth bound, a chain verdict that passed through a reference, or a shallow expansion later served to a deeper walk.
- **Why.** A node first reached near the depth cap was truncated and then skipped on a shallow revisit, and a shared nodeview served a shallow memo to a deeper walk, so refs vanished and refusals fired by declaration order.
- **Probe.** Construct a node reachable both deep (near the cap) and shallow, compile with each reached first, and diff the collected refs and diagnostics; for any shared memo, run the two-order diff with the memo shared and with it disabled.
- **Seen in #94** (8a747a68): “but memoization and a recursion depth cap are unsound together: a node first”
- **Seen in #362** (8499518b): “Sharing one nodeview.View across the boundary walks made the verdict”

### Escape document-supplied names as single segments when building a scoped key
`escape-separators-in-composite-keys` · n=3 · PRs=3

- **Rule.** When a key or scope is built by joining source-supplied names with a separator, escape the separator inside each name through one shared function and scope each entry by its full source path; never interpolate a raw name.
- **Why.** Form-part, callback and root keys containing "/" read as two segments and collided with another object's key, and Unmodeled entries written by sibling objects into one map overwrote each other by lowering order.
- **Probe.** Add a fixture whose property, form-part or callback name contains the separator ("a/b") alongside a real nested a.b, and two sub-objects writing the same key into one shared map; compile and assert both entries survive with distinct keys.
- **Seen in #345** (fd1df3c1): “scope unescaped, so a name containing a separator read as two segments and two”
- **Seen in #356** (6fcfd8ab): “A key holding a "/" spelled the scope of the object that path names, so a”

### Compare a $ref's document part by its normalised full path and resolve its fragment in that document
`ref-document-part-identity` · n=3 · PRs=3

- **Rule.** When deciding whether a reference names this document, normalise both paths (./, directory prefixes) and compare the full relative path, never the basename; when deriving a pointer from a $ref, split off the document part and resolve the fragment in the document it names.
- **Why.** A basename match treated a same-named file in another directory as a self-reference, ./spec.yaml self-references reported false conflicts, and a multipart encoding key minted an identity for a property in another document.
- **Probe.** Add fixtures with sub/spec.yaml referencing spec.yaml, a self-reference spelled ./spec.yaml#/..., and an external $ref used as an encoding key; compile and check each resolves to the intended document.
- **Seen in #22** (79422772): “sameFile compared a $ref's document part to the source purely by basename,”
- **Seen in #548** (4e5ae2fa): “A self-reference spelled through a directory, like `./spec.yaml#/...`, is read”

### Keep synthesized names in the hint channel, with canonical words, independent of unrelated content
`synthesized-name-channel-and-independence` · n=3 · PRs=3

- **Rule.** A name the compiler invents must be recorded as a hint rather than a source-declared name, must carry canonical words so emitters need not segment it, and must depend only on its own position, not on whether unrelated schemas reference that position; a new verifier check must be run over the whole committed corpus before merging.
- **Why.** A synthesized group name was recorded as API-author-declared, a webhook group shipped with no canonical words, and hints under /paths changed with unrelated references, which a two-order diff does not catch.
- **Probe.** Run go run ./cmd/morphic-harness testdata/ after adding a naming check; add a fixture pair differing only by an unrelated $ref to the position and cmp.Diff the names; inspect Naming.Source vs hint fields for every synthesized node.
- **Seen in #188** (99c9f70a): “settled the wrong half: it recorded an invented name as one the API author”
- **Seen in #375**: “depends on whether an unrelated schema points at the position”

### Mark the start node visited and refuse a cycle where the edge is created
`graph-walk-cycle-handling` · n=2 · PRs=2

- **Rule.** A graph walk with a visited set must mark the start node before its first expansion, and a lowering that mints a reference edge must refuse a cycle at the point it creates the edge rather than rely on a later pass that may not walk it.
- **Why.** An ancestor walk returned the start as its own ancestor on a composition cycle, and $dynamicRef expansion could close an alias loop no check walked, leaving an IR an emitter would follow forever.
- **Probe.** Add a fixture where the start of the walk is on a cycle (a schema whose mapping names itself, an expansion that loops back) and assert a refusal diagnostic and a finite IR.
- **CLAUDE.md already says:** “deep/cyclic specs are normal inputs, not edge cases”
- **Seen in #149** (70d1ad0f): “Expansion could close a cycle of aliases that a $ref cycle would have been”
- **Seen in #368** (93f980dc): “The ancestor walk marked every schema it enqueued but not the one it”

## 6. Spec, grammar and verifier semantics

Read the specification, the library and the grammar as they are, at the version pinned; verifiers must reach every class they claim to check.

### Give every field a check that rejects its zero, empty and malformed values
`verifier-rejects-every-degenerate-value` · n=12 · PRs=9

- **Rule.** For each field a consumer reads or routes on, the verifier must have a production read site that rejects its empty, null, absent, mismatched-kind and malformed values, read from the unfiltered source and mirroring every refusal the encoder makes.
- **Why.** Empty keys and reasons, an absent irVersion, unread Aliases, digitless numbers and ill-formed UTF-8 all verified clean, so a verified document could still fail to encode or misroute.
- **Probe.** For each field of the struct under check, write the zero, empty, null and wrong-kind variants and assert each yields a violation; encode every document that verifies clean.
- **Seen in #329**: “and `pass.Validate` with zero violations and zero diagnostics”
- **Seen in #361**: “`Hint` are read by `checkNaming`; `Aliases` was read by nothing anywhere in the”

### Key static source checks on every syntactic form, ideally the resolved type
`source-scanners-match-every-form` · n=9 · PRs=7

- **Rule.** An AST or grep check over Go source must match every form the language and codebase use: value and pointer receivers, method values, functions passed as values, aliases, every selector base, go statements and .Go calls, every write form and every constructor of an identity type.
- **Why.** Recursion and dependency pins went blind to value receivers and method values, and a grep audit reported clean while missing both planted writes.
- **Probe.** Plant one instance of each form the check must catch in a scratch package and confirm each reddens; prefer keying on go/types over selector text.
- **Seen in #332**: “A grep-based version of the same audit reported the same clean result and missed”
- **Seen in #837** (f9862aa6): “declaredFields skipped every alias, so a field declared inside”

### Check every reference position and every ID class, not a subset
`verifier-reaches-every-reference-and-id-class` · n=8 · PRs=6

- **Rule.** A validator or verifier over references or IDs must cover every position a reference can occupy (map keys, PropIDs, plain int indices) and every ID class and node class the document holds, derived from the type graph rather than from the data a fixture happens to contain.
- **Why.** Dangling rename targets, channel, message and PropID references, and malformed operation IDs verified clean because the walk saw only TypeIDs or only registry-reached nodes.
- **Probe.** Plant a dangling or malformed value in each reference-bearing field and each ID class in turn and assert a violation fires for each; a field whose plant stays green is uncovered.
- **Seen in #149** (50cbf651): “reported by nothing: irverify returned no violations and”
- **Seen in #119** (61e34370): “could every one name an entry that does not exist and still pass”

### When a model mirrors a library's read, match its duplicate-key, alias, cap and token rules exactly
`mirror-library-read-rules-exactly` · n=8 · PRs=4

- **Rule.** A pre-scan, index or stepped reader standing in for a library read must reproduce that library's rules: last-key-wins on duplicates, following YAML aliases and merge keys, no cap the library lacks, by-value model fields only, and leaving retried index tokens to the whole read. Build a differential case for every token kind the library treats specially.
- **Why.** First-key-wins pre-scans let a redeclared self-reference reach the resolver's stack overflow, an anchor index missed alias-reached declarations, and a stepped walk missed references a whole read reaches, while an invented cap made valid deep specs exit 1.
- **Probe.** For each special case (duplicate key in both positions, alias, merge key, retried index, by-value struct, deep chain past your cap) diff the mirror's answer against the library's whole read on a fixture, and confirm the fixture actually produces that token kind.
- **Seen in #95** (40e741e9): “The cycle pre-scan read a mapping that declares the same key twice as first-key-wins.”
- **Seen in #356** (d359ca9c): “RawChildNode returned the first pair spelling a key where the parser reads”

### Validate a fixed-shape token at every character and its escape-of-escape, and dispatch keyword forms on the dialect
`exact-token-grammar` · n=5 · PRs=4

- **Rule.** When validating a fixed-shape token, check its length and every character position; when splitting on an escapable delimiter, honour the escape character's own escaping; when a keyword's form depends on the dialect, dispatch on the dialect and report the other dialect's form rather than accepting it.
- **Also.** For every derived identifier or canonical name with a grammar, irverify must reject any value the grammar cannot produce, covering every property (casing, character set, segmentation, agreement with the recorded coordinate), with one rejecting case per property.
- **Why.** A wildcard status check read two of three characters and admitted 1XY and 9XX, a backslash-escaped backslash before a pipe merged two cells, and a boolean exclusiveMinimum under 2020-12 was silently accepted as a degenerate bound.
- **Probe.** Write rejected spellings for each length and each character position, a cell containing \\|, and each bound keyword in the other dialect's form; compile and confirm each is reported, not lowered.
- **CLAUDE.md already says:** “Never accept garbage silently.”
- **Seen in #263**: “The parse was loose in the other direction as well. The wildcard test read the first”
- **Seen in #23** (32c99973): “now reported and dropped rather than silently accepted as a degenerate”

### Identify and measure code with the parser and type checker, never by regex or constructed name strings
`resolve-symbols-semantically` · n=3 · PRs=3

- **Rule.** Match functions and packages by types.Object identity and the file's own import declarations (including aliases), and measure structure (call cycles, method counts) with a parser, cross-checked by a second method.
- **Why.** Name-string matching missed aliased imports and flagged unrelated packages, and a regex roughly doubled a mutual-recursion count and sized a plan on it.
- **Probe.** Add an aliased import and an unrelated same-named package to the guard's fixtures; recompute any reported metric with go/ast or go/types and compare.
- **Seen in #801** (6c71a672): “of data matched the conventional names ir and compile. A file importing”
- **Seen in #203**: “I first measured the core with a regex over the source and reported 57 mutually”

### Witness each character class a text predicate decides
`character-predicate-witness-per-class` · n=3 · PRs=2

- **Rule.** A predicate over text (cased, blank, invisible) must be tested against the producer's exact transformation and with one witness per Unicode class it claims (Cc, Cf, default-ignorable, Zs, caseless letters), not a hand-picked sample.
- **Why.** unicode.IsUpper flagged neutral caseless runes as cased, and TrimSpace let zero-width and filler-only aliases pass as non-blank.
- **Probe.** Generate one witness per class and per producer output and assert the predicate's verdict on each.
- **Seen in #361** (d1c19a8f): “The blank rule was strings.TrimSpace, which reports false for the”
- **Seen in #361** (b75eec57): “isBlankName missed the default-ignorable code points, so an alias of”

### Make audit matches exact, and plant a sibling or same-named path to prove each rule reaches
`archtest-rules-reach-exactly` · n=3 · PRs=2

- **Rule.** An allowlist or skip rule must match full paths exactly unless a wildcard is written, and every in-module import -- including testdata fixtures under an audited tree -- is part of the graph the rules must cover.
- **Why.** A segment-prefix match let compilers/graphql pass as compilers/openapi's sibling, a basename skip left nested irtest/irverify directories audited by nothing, and a testdata fixture's sibling import was missing from the allowlist.
- **Probe.** Plant a forbidden import from a package sharing the allowed prefix, a same-named directory at another depth, and a testdata fixture importing a sibling; run go test ./internal/archtest and confirm each reddens.
- **CLAUDE.md already says:** “A check that runs is not a check that reaches.”
- **Seen in #182** (2de404a1): “An allowlist entry matched as a segment prefix, and compilers/openapi is allowed”
- **Seen in #182** (2de404a1): “The skip decision for a subdirectory carrying its own rule compared basenames”

### Re-validate text after percent or escape decoding before it becomes an identifier
`revalidate-after-decoding` · n=2 · PRs=2

- **Rule.** Decoded untrusted text must pass the same validity check as the raw input, plus the target format's grammar (UTF-8 for a JSON Pointer), before use as a key or identifier.
- **Why.** A percent-encoded %FF in a $ref fragment reached an IR identifier the encoder cannot write.
- **Probe.** Compile a spec with `#/components/schemas/%FF` and similar decoded-invalid bytes; read the full diagnostic list and confirm the document still round-trips through JSON.
- **CLAUDE.md already says:** “Never accept garbage silently.”
- **Seen in #541** (82454bc8): “`#/components/schemas/%FF` became a pointer holding the byte 0xFF”
- **Seen in #515** (66d2f196): “#520: a percent-encoded non-UTF-8 byte in a `$ref` fragment reaches an IR identifier.”

### Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point` · n=2 · PRs=1

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types became non-nil and the round trip never settled.
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode again, and cmp.Diff the two decoded values.
- **CLAUDE.md already says:** “The whole `Document` round-trips through JSON”
- **Seen in #264**: “`TypeRegistry.UnmarshalJSON` unconditionally allocated and assigned a map even when the JSON input was the literal `null`”
- **Seen in #264** (009429dc): “TypeRegistry.UnmarshalJSON(" null ") still built and assigned an empty map,”

## 7. Tests that bite

A test proves nothing until the defect it names has been planted and it went red. Coverage reached is not behaviour held.

### Show every guard and branch is load-bearing by deleting it; coverage is not assertion
`every-guard-has-a-reddening-test` · n=26 · PRs=25

- **Rule.** For each guard, skip, decoder check, refusal path or edge branch added or moved, delete or loosen it and confirm a test in its own package reddens; statement coverage only proves reachability, and a case that changes no mutation's outcome adds no coverage. If no fixture can reach a guard, record the gap beside it rather than presenting it as tested.
- **Also.** For each action, branch, nested member, non-first family member, skip path, sub-shape and downstream output a change touches, there must be a test that reddens when that one is broken; a guard pinned in one check does not pin its siblings.
- **Also.** Line coverage is not holding: each exit of the function a PR rests on, each rejection branch, and each named guarantee needs a test that reddens, by name, when that step is mutated, and that test must live in the package that owns the contract. Treat every surviving mutant as a missing test and rerun the mutation set after adding it.
- **Why.** The ir package reported full coverage while omitempty on Required or Nullable passed, decoder guards loosened to len(data)==4 passed, and moved cycle guards and refactored branches lost every test that held them.
- **Probe.** For each new or moved guard, replace its condition with false (or a looser form), run go test for its package, and require red; for refactors, plant a mutation per edge branch before moving the code.
- **CLAUDE.md already says:** “Assertions that look correct on the page find nothing.”
- **Seen in #92** (b62b25fc): “It reported 100% statement coverage, but almost all of it came”
- **Seen in #264** (009429dc): “Weakening the guard to len(data) == 4 - so it swallows true, 1234 or any”

### Include inputs the transformation actually changes and that a plausible wrong implementation mishandles
`inputs-at-the-edges-the-code-transforms` · n=16 · PRs=13

- **Rule.** A table, property or fuzz test must contain inputs at the representation's edges and in the classes a wrong implementation would get wrong: characters that need escaping, numbers float64 or int cannot hold, non-UTF-8 bytes, wrong-case or substring matches, partial overlaps, refused grammars, forbidden spellings, zero values, and the exact seed that broke the property. Where two readers must agree, diff them over a generated space, not only hand-written rows.
- **Also.** A regression or rule test must contain the inputs that separate right from wrong: the exact shape from the issue, both edges of each range and the values just outside, the same name in both syntactic positions, inputs that sort differently under the wrong collation, and fixture text that parses as intended.
- **Why.** Removing escaping, bounding IsIndex by Atoi, widening an acceptor or intersecting to one side all left suites green because no row exercised the edge where the implementations diverge.
- **Probe.** For each transformation under test, delete it (or swap in the naive version: substring match, strconv.Atoi, return-one-input) and run the table; if it stays green, add the edge input and repeat until the naive version reddens.
- **CLAUDE.md already says:** “Disagreement has to enter from outside: a planted mutation, a probe, the opposite order.”
- **Seen in #801** (752d28b8): “Removing it left the spelled-out table green, because no”
- **Seen in #834** (f4f69b57): “an IsIndex bounded by strconv.Atoi passed the earlier table.”

### Give every route, kind, row, call site, condition, clause and exemption its own reddening case
`one-reddening-case-per-member` · n=11 · PRs=9

- **Rule.** When code handles a family (routes sharing a loop, kinds through a dispatch arm, rows of a classification table, call sites of a helper, conditions of a predicate, narrowing clauses of a copied predicate, shapes of a construct, branches of a verifier, exemptions of a check), each member needs a case that fails when that member alone is mutated; incidental coverage from a shared fixture or the real tree is not enough.
- **Why.** Webhook callbacks, six object kinds through a generic arm, ID classification rows, and recursion shapes leaving through a free function all passed when removed, because only one member of each family was tested.
- **Probe.** List the family's members by grep, mutate each one alone (skip the route, drop the clause, widen the exemption, pass no argument at that call site) and run the package's own tests; every mutant must redden.
- **CLAUDE.md already says:** “Sweep every site of the mechanism before closing it.”
- **Seen in #567** (c246dec2): “The tests reached a parameter, path item, response and schema. A”
- **Seen in #834** (19986964): “No test covered a webhook's own callbacks or a callback operation's”

### Build a negative fixture where the guarded condition is the only thing that changes the result
`negative-fixture-isolates-the-guard` · n=10 · PRs=10

- **Rule.** A guard or regression case needs an input the guarded code would mishandle without the guard: a typed nil rather than an untyped one, a callee that does not tolerate the input, a document truly over budget, structure for the mutation to land on, every input class the check claims, nodes of the kind the name says, rows of the shape a sizing or selection rule must exclude, and an outcome that differs between buggy and fixed code. A case both readings answer the same way guards nothing.
- **Why.** Nil-guard tests planted untyped nils that failed a type assertion first, a budget test's fixture fit the remaining budget, and a fallback assertion passed either way, so deleting each guard left the suite green.
- **Probe.** Delete or neutralise the guard and run the test; if it stays green, inspect why the input never needed the guard (tolerant callee, earlier failure, fits budget) and redesign the fixture until removing the guard reddens it.
- **CLAUDE.md already says:** “A negative fixture must be reachable.”
- **Seen in #360** (8f28f931): “TestVerify_NilTypeBesideAUnionDoesNotPanic planted an untyped nil, which”
- **Seen in #567** (939233e6): “The budget test said a document already over its budget is refused”

### Revert the fix and require a committed fixture to redden; if none does, add the triggering shape
`fix-must-move-a-golden` · n=9 · PRs=8

- **Rule.** A fix whose golden regeneration changes nothing, or whose revert leaves every oracle green, is evidence the corpus lacks the input shape the fix changes. Before pushing, add a committed fixture containing that exact shape (and, for a conflict rule, one where old and new behaviour disagree) and confirm reverting the fix reddens it.
- **Why.** Defects in encoded fragments, constraints beside verbatim unions, outside $refs to inline positions, timestamp rewrites and new lowering branches all survived because no committed spec held the triggering shape, so the goldens stayed green across the bug.
- **Probe.** git stash the fix's production hunk, run go test ./... and the harness over testdata/; if nothing reddens, write the minimal spec with the triggering shape, -update, restore the fix and confirm the golden diff is exactly the fix's effect.
- **CLAUDE.md already says:** “A corpus shares the blind spots of the code it covers, so confirm a deliberate deletion from a source spec reddens the suite.”
- **Seen in #363**: “No golden moved when the fix landed, which is the corpus sharing the code's blind spot”
- **Seen in #119** (a4365f05): “and patternProperties value in it is bare, which is why regenerating the”

### Give competing sources, symmetric fields and interacting rules distinct values so each reading differs
`distinct-values-for-competing-sources` · n=9 · PRs=8

- **Rule.** When the result depends on which of two sources, sides, arms, keywords or rules wins, the fixture must make them disagree: set the attribute on both sides with distinct values, set one of a symmetric pair alone, set one keyword without its companion, satisfy both rules' preconditions, and force two orders to contend for the same node. With equal or co-set values every reading passes.
- **Why.** Swapped exclusive bounds, wrong reply channels, losing precedence and order-dependent hints all passed because fixtures set both fields together, used the same channel on both sides, or used a bare branch that never contended for the pointer.
- **Probe.** Plant the swap (min for max, reply for operation channel, overlay order reversed, read moved below the check) and run the test; if green, change the fixture so the sources carry distinct values or only one is set, and rerun until the swap reddens.
- **CLAUDE.md already says:** “Order-dependence needs a two-order diff.”
- **Seen in #274** (629c2f07): “Every 3.0 case declared exclusiveMinimum and exclusiveMaximum together, so”
- **Seen in #311** (b8182d58): “The reply tests answered on the channel the operation binds, so both spellings”

### Validate an expected value independently before pinning it, so the test does not enshrine a defect
`expected-value-is-not-the-bug` · n=8 · PRs=7

- **Rule.** Before accepting an expected output, check it against something outside the code: run a 'clean' fixture under the bounds the code enforces, parse emitted output with the next stage, confirm a 'nothing to keep' input really has nothing, update every test that pinned an edge case's old meaning, and classify a new differential oracle's failures against the reference's raw output.
- **Also.** An expected count or grammar must come from a source that cannot shrink or drift with the code: the specification, the library's own behaviour, the raw text or delimiter structure. An expectation derived from the same table, or restated by hand from the same rule, agrees with the implementation even when both are wrong.
- **Why.** Tests pinned a memory-exhausting fan-out as clean, YAML with an alias above its anchor as correct, and the loss of a summary and description as silence, so each test guarded the bug.
- **Probe.** Run each expected 'clean' fixture under the time and memory caps; feed expected output to the next parser; grep the input for content the expected output omits; print the reference's raw result for each failing differential row.
- **CLAUDE.md already says:** “Reading the code, reading the test, regenerating the golden, compiling one declaration order”
- **Seen in #99** (1d4ac861): “pinned a 40-level × 2-way fan-out as **clean**.”
- **Seen in #243**: “`TestReverseMappings_AliasIsNotFollowed` asserted the anchor-above-alias”

### Check that the assertion itself can fail: selector specificity, zero-match, type agreement, reachable bounds
`assertion-must-be-able-to-fail` · n=6 · PRs=5

- **Rule.** An assertion is vacuous when its form cannot fail: a path selector that matches every map, a predicate wrong on its zero-match case, Equal/NotEqual between mismatched types, a bound a derived set can never exceed, or a grid whose axes cannot name the missing cell. Test the checker on its degenerate cases before trusting it. A completeness check must count the same units the fixture is built from (occurrences, not deduplicated sets).
- **Why.** A bare .Unmodeled selector admitted any carrier, Equal between a string and a jsontext.Pointer always failed and NotEqual always passed, and an enters-once test bounded the walk by the node count it could never exceed.
- **Probe.** Feed each assertion helper its zero-match, all-match and wrong-type inputs and confirm it reports failure; grep assertions on retyped fields for mismatched argument types; remove one cell from the grid and confirm completeness reports it.
- **CLAUDE.md already says:** “Assertions that look correct on the page find nothing.”
- **Seen in #549** (d40c4433): “which would always fail as `Equal` and always pass as `NotEqual`”
- **Seen in #345** (d0e36f47): “which ends every path the value walk produces”

### Run every mutation a docstring or PR body cites, and read each subtest's result
`bite-claims-are-executed` · n=5 · PRs=5

- **Rule.** Before a test docstring or PR body says a test catches a mutation, plant exactly that mutation, confirm it applies once and only at its site, and read per-subtest results with -v; passing and closing a coverage gap are not evidence a test bites, and neither is a test that was green on both branches before the change.
- **Why.** A PR body claimed a mutation reddened every package and none went red, a claimed three-subtest bite reddened two, and a mutation self-test first counted broken-file mutants as caught.
- **Probe.** Apply each cited mutation with a one-site patch, diff it against the original to confirm one hunk, run go test -v -count=1 on the cited test, and paste the per-subtest outcome into the claim.
- **CLAUDE.md already says:** “Plant the defect and watch it go red.”
- **Seen in #382** (4c06ee4e): “`go test ./compilers/openapi/... -count=1`: **every package green**”
- **Seen in #379** (96eeddb7): “it reddens two. This subtest passes under that mutation.”

### Prove a check, command or mutation actually ran against its target before reading its result
`validate-the-instrument-first` · n=5 · PRs=5

- **Rule.** An empty, passing or 'survived' result is evidence only if the instrument demonstrably reached the input: run each verifier, diagnosis command and mutation once against a known positive, confirm the mutated file differs and compiles, and confirm a differential oracle's transform preserved meaning before blaming the code under test.
- **Why.** A verifier that never met its output, a command reading a stale working tree, a mutation pattern that matched nothing, or a non-compiling mutant all look exactly like a clean result.
- **Probe.** Plant one violation at the exact path the check reads and watch it fail; diff the mutated file against the original and run `go build` on it; for differential oracles, re-parse the rewritten input and compare encoded values.
- **CLAUDE.md already says:** “A check that runs is not a check that reaches.”
- **Seen in #135**: “A verifier was complete, well-tested, wired into CI, and never met the output it existed to check.”
- **Seen in #200**: “A fourth mutation reported "survived" against a pattern that did not exist”

### Test caps and thresholds exactly at, below and above the boundary, and assert the counted amount
`boundary-cases-at-the-boundary` · n=3 · PRs=3

- **Rule.** Any cap, depth bound, budget or threshold needs cases exactly at the boundary, one below and one above, with the whole output compared; where a bound is charged, assert the charged amount itself, not only which side of the limit it landed on.
- **Why.** An alias placed one level beyond a depth bound and a 30-gap listing let off-by-one mutants of the comparison pass, and a refusal charge counted wrongly still landed on the same side of the limit.
- **Probe.** Mutate each comparison by one (<= to <, > to >=) and each charge amount, run the tests, and add the at-boundary case until every mutant reddens.
- **Seen in #340** (5176b0a5): “TestBuild_TracksAncestorsOnlyToItsDepthBound places its alias one level past”
- **Seen in #828** (2359a62b): “The only test of the cap listed 30 gaps, which left the count line's”

## 8. Assertions, fixtures and corpus reach

Assert the property the name claims, with inputs that reach the code and an expectation from outside it.

### After any change upstream of a fixture, confirm it still reaches the check, branch or oracle it names
`fixture-still-reaches-its-stage` · n=10 · PRs=10

- **Rule.** A new earlier check, a moved validation, a classifier change or a setup change can make a fixture stop earlier and pass for the wrong reason. Confirm each affected fixture reaches its target stage past every earlier error return, assert the precondition (it parses, it reached the oracle) before the outcome, and re-plant a neutralisation of each refusal arm the earlier check may now shadow.
- **Why.** Fixtures meant for the round-trip and order oracles stopped at an unrelated earlier error, a grammar check moved ahead of ParseFloat shadowed the IsInf arm, and a classifier change rerouted a row to the wrong branch, all while green.
- **Probe.** Run harness.Check on each fixture and read which oracle fired first; for each refusal arm downstream of the change, neutralise it and confirm a test reddens; count the target branch's hits in the test.
- **CLAUDE.md already says:** “A check that runs is not a check that reaches.”
- **Seen in #549** (d40c4433): “stops at an unrelated error before the oracle runs, so it tests nothing”
- **Seen in #269** (e955e9cc): “the whole suite green, and only the 100% statement gate noticed”

### Enumerate every kind, position, site and field a lowering handles and show a corpus witness for each
`corpus-witnesses-every-site` · n=10 · PRs=9

- **Rule.** For a lowering or convention, list the domain it branches over (literal kinds, keyword positions, reference-writing sites, compiler-assigned fields, edge kinds, both halves of a rule, every type a convention governs, every option an encoder pins) and confirm the committed corpus contains an input reaching each member. Per-capability presence is not per-branch coverage.
- **Why.** Keywords were dropped at declaration level because the corpus only wrote them on properties, a dangling ref at encoding headers was invisible to the closure check, and many compiler-assigned fields were never non-zero, so the code writing them had no witness.
- **Probe.** Grep the compiler for every site that writes the construct (TypeRef writes, keyword reads, field assignments), then grep testdata/ (or the compiled corpus IR) for an input reaching each one; any site with zero hits gets a fixture, confirmed by a planted mutation at that site.
- **CLAUDE.md already says:** “an input class no committed fixture produces”
- **Seen in #137**: “No corpus spec wrote `minProperties`”
- **Seen in #119** (39aed2b4): “No corpus spec declared encoding.<part>.headers, so the TypeRef the compiler”

### Make a test's name, comment and witness labels state only what its assertions reach
`test-claims-match-reach` · n=10 · PRs=7

- **Rule.** A test name, test comment, witness label or coverage justification must describe what the assertions actually check and which inputs actually run: count skipped versus exercised inputs, confirm a named witness produces the outcome in its golden, check the matrix status for every row a fixture claims, and check that a timeout or guard is not negated by another test of the same input.
- **Why.** Sweeps implied coverage of fixtures the compiler declined, conformance specs claimed rows their goldens did not show, a test name asserted a property nothing checked, and a 10s bound protected nothing because the fixture ran unbounded elsewhere.
- **Probe.** Plant the defect the test claims to catch and watch it go red; for a sweep, print counts of skipped vs checked inputs; for a witness label, grep the golden for the construct; for a timeout, grep the suite for other tests compiling the same fixture.
- **CLAUDE.md already says:** “a test that passes without asserting what its name claims”
- **Seen in #342** (ff7909bc): “Six specs named capability rows their goldens do not show: a spec whose”
- **Seen in #119** (e7686977): “The sweep reads as though it verifies every deliberately-broken fixture, and”

### Make each test assert the exact field, location, direction or property its name and comment claim
`assertion-targets-what-the-name-names` · n=9 · PRs=7

- **Rule.** Read each test name and fixture comment as a claim and check the assertions pin it: a gap cell asserts the field the gap names (not neighbours), a location test fails when the location is remapped, a direction fixture traverses that direction, a row named for a code path reaches it, a copy test asserts preserved state, and claimed-distinct values are distinct. Rename or split a case whose input has a different property.
- **Why.** Gap cells asserted Extensions and Usage instead of the named field, querystring tests stayed green with the location remapped, and a fixture named for one traversal direction ran the other way.
- **Probe.** For each test, write the one-line mutation its name describes (remap the location, apply the named fix, fresh-construct instead of copy) and confirm that test, specifically, reddens; apply an unrelated change and confirm it stays green.
- **CLAUDE.md already says:** “a test that passes without asserting what its name claims”
- **Seen in #111** (8591a4d7): “that Extensions was empty and XML was nil. That only catches a fix”
- **Seen in #364** (852f1d9c): “The two unit tests for declared keywords were named for a location they”

### Make test-side guards, globs and oracles fail on what they cannot classify, never skip or log
`test-guards-fail-closed` · n=9 · PRs=6

- **Rule.** A guard over declarations, a self-check, a glob, a benchmark fixture path or an oracle that can decline must fail on input it cannot classify or find: no silent skip of unparsed spellings or aliases, no log-only self-check, no glob matching zero files, no t.Skip on a missing fixture in a gate run, no oracle returning ok without comparing, no guard keyed to a file path that moved.
- **Why.** Const-block guards skipped conversions, aliases and untyped literals, a benchmark read a nonexistent path and exited 0, and the order oracle returned ok without comparing on anchor-bearing specs, so each looked like coverage while checking nothing.
- **Probe.** Feed each guard one unclassifiable input (an alias, a conversion, an untyped constant, a blank reason) and confirm it fails; assert every glob matches at least one file; count executed comparisons and fail when zero.
- **CLAUDE.md already says:** “Never accept garbage silently.”
- **Seen in #111** (6cce414a): “silently skipped any spec”
- **Seen in #344**: “`BenchmarkAnchorWalk`'s fixture path was two directory”

### Assert diagnostics by code, severity, subject and count, and assert the complete set the call emits
`assert-diagnostics-by-identity-and-completely` · n=8 · PRs=8

- **Rule.** Assert each diagnostic by its stable code, severity, pointer or named subject and per-site count, not by message substring or a generic 'something degraded'; assert the full diagnostic list a call returns (never discard the error or leave diagnostics unasserted), give every emitted diagnostic a case that triggers it, and when matching an external tool's message match only the fragment the code controls.
- **Why.** Reworded or duplicated announcements passed substring tests, a dropped constraint-reporting call left the suite green, and count-based tests silently changed meaning when a split changed the count.
- **Probe.** Delete each diagnostic-emitting call and reword each message; the first must redden a test and the second must not. Compile the fixture and diff the full diagnostic list against the test's expectation.
- **CLAUDE.md already says:** “Compile the input; inspect the emitted IR *and* the full diagnostic list.”
- **Seen in #382** (4c06ee4e): “Reword the message and all three go green while covering nothing”
- **Seen in #834** (8414e706): “single call, and nothing asserted it: dropping the call left the suite”

### Test a consumer on the producing stage's real output and through the entry point production calls
`exercise-the-production-path` · n=7 · PRs=6

- **Rule.** A verifier, pass or oracle must meet the real output of the stage that feeds it, built by production constructors and read from the same tree the system read; a cached or reused path must be diffed against the fresh path over the corpus. Tests on hand-built documents, hand-set internal fields, hand-written literals or test-only wrappers do not show the real path works.
- **Why.** The Preserved verifier ran only on hand-built fixtures so an empty reason passed everything, a reused parse dropped the stream tail on every CLI run with the suite green, and a lazy-init branch was reachable only by setting a field by hand.
- **Probe.** Plant the defect in the producing stage (empty reason, dropped field in the cache) and confirm the consumer's test reddens; grep tests for struct literals of internal state and for helpers no production caller reaches.
- **CLAUDE.md already says:** “A verifier can be complete, well-tested and wired into CI while never meeting the output it exists to check”
- **Seen in #119** (65539ac2): “nothing: planting Reason:"" at a preserve call passed go test ./... entirely.”
- **Seen in #485**: “a reused parse now has to carry the stream tail (a mutation dropping it silenced `openapi/stream-documents-dropped` with the whole suite green)”

### Assert the value itself, never only its count, length, existence, a negation, or non-panic
`assert-the-value-not-a-proxy` · n=7 · PRs=5

- **Rule.** A test must compare the actual value or identity it is about against an expectation: not only Len, digest length, the entry's existence, that nothing panicked, that the outcome is not X, or count-plus-membership (each expected key exactly once), and never a compiled value discarded with _ =. A proxy that a wrong value also satisfies asserts nothing.
- **Why.** Wrong example values, dangling derived keys, path-derived hashes and empty stand-ins for skipped refs all passed tests that checked only count, existence, length or no-panic.
- **Probe.** For each assertion, plant a mutant that keeps the proxy true but changes the value (wrong example, different key, fresh hash, duplicate at one key and none at another) and confirm the test reddens; grep tests for `_ = ` on compiled results.
- **CLAUDE.md already says:** “a test that passes without asserting what its name claims”
- **Seen in #222**: “The multipart-encoding test required only that the entry exist.”
- **Seen in #222**: “The ghost-reference test asserted that nothing panics.”

### Assert every field, key and side effect the code writes or must preserve, across every sibling case
`assert-every-output-the-change-touches` · n=6 · PRs=6

- **Rule.** When code fills several outputs, merges several fields, writes several structural keys, or promises to leave its input or a copy untouched, assert each of them, and give sibling cases the same guard set. Every element a fixture declares should be deletable only by reddening some test.
- **Why.** A $ref docs merge narrowed to description alone, an alias losing its co-declared keyword, a builder dropping paths, and an in-place sort of the caller's slice all passed because only one projection was asserted.
- **Probe.** Delete each field the code writes (or each element of the fixture) one at a time and run the suite; for no-mutation promises, plant an in-place sort or fresh construction and assert on the caller's own value after the call.
- **Seen in #119** (8a8b6e00): “anywhere: narrowing the referent branch to the description alone left the whole”
- **Seen in #363** (ce662ba2): “Only the constraints were asserted, so”

### Make a failing test fail quickly, with a named cause, and never rewrite a golden on failure
`tests-fail-fast-and-legibly` · n=5 · PRs=5

- **Rule.** Guard preconditions with require (NotNil, length) before dereferencing or indexing; diff member sets rather than counts; give a hang-prone guard a bounded-time case that fails in seconds; and stop before any golden write when an earlier assertion failed.
- **Why.** A panic or package timeout hides the cause, a count mismatch names no member, and a non-fatal assertion under -update rewrote the golden from regressed output.
- **Probe.** Break the code under test and read the failure: it must name the cause within seconds and not panic; run the golden test with -update while an assertion fails and confirm the golden file is unchanged.
- **CLAUDE.md already says:** “`testify/require` for preconditions, `assert` for values”
- **Seen in #149** (27647532): “A golden could be rewritten by a failing run.”
- **Seen in #379** (b92870d6): “Two of the three new subtests dereference a possibly-nil *yaml.Node inline”

### When two paths give the same answer, assert which path ran or plant a disagreement between them
`prove-which-path-answered` · n=5 · PRs=4

- **Rule.** A test of a fast path, cache, rebuild branch, reuse check or stop point must show which path produced the result, by asserting the path was taken (index populated, counter, hook, distinct sentinel) or by planting a value only one path would return. Comparing results alone passes when the path is bypassed.
- **Why.** An index-versus-scan test compared the scan with itself when the index was disabled, a reuse test passed under content equality, and a cancellation row passed whichever boundary fired.
- **Probe.** Disable the fast path or weaken the identity check (sameBytes to content equality, skip the rebuild) and confirm the test reddens; if not, add the path-taken assertion or plant the divergence.
- **CLAUDE.md already says:** “Disagreement has to enter from outside: a planted mutation, a probe, the opposite order.”
- **Seen in #379** (b92870d6): “TestChildByToken_IndexAgreesWithTheScanItReplaces never asserts the index was built, so a change that silently disables it leaves the test green”
- **Seen in #379**: “test asserted only the answer, which both readings give, so it now plants a divergence between index and pairs”

### Do not let a golden stand in for an assertion of a decision, a layout rule, or a non-empty result
`goldens-record-not-verify` · n=4 · PRs=4

- **Rule.** A golden records what happened, not what is correct, and regeneration absorbs any change. Where a golden carries a design decision (field placement), a layout rule (column width) or is cited as pinning a behaviour, add a direct assertion; never hard-code a layout constant; and a golden writer must refuse trivially empty outputs such as nil.
- **Why.** A literal column width of 9 misaligned longer commands and the regeneration accepted it, and a golden for a nil document matched every later nil document, asserting nothing.
- **Probe.** Delete the behaviour the golden is cited for from the input and confirm the golden bytes change; lengthen a rendered name past any constant width; call the golden writer with nil and confirm it errors.
- **CLAUDE.md already says:** “A regeneration that changes nothing looks exactly like a broken `-update`.”
- **Seen in #312**: “The root-help command column was padded to a literal 9.”
- **Seen in #321**: “on disk that every later nil document matched: a snapshot that asserts nothing”

### Loop timing-, scheduling- and race-sensitive tests many times before committing them
`nondeterministic-tests-proven-stable` · n=4 · PRs=4

- **Rule.** A timing ratio must interleave its two measurements and separate the growth rates well beyond noise (including under -race); a reach test for a goroutine or race check must be run hundreds of times to record its false-pass rate; helpers that type-check several packages in one load run repeatedly under -race.
- **Why.** Sequential timing and a ratio allowance of 3 against a separation of 2 failed CI on noise, a planted goroutine check passed intermittently, and go/types raced on a shared import in a fraction of runs.
- **Probe.** Run the new test with -count=200 (and -race) under taskset on a loaded machine and record the failure rate; it must be zero for a pass and one hundred percent for its planted defect.
- **Seen in #494**: “The test took the minimum over every walk of the 16000-key tree, then the minimum over every walk of the 8000-key tree.”
- **Seen in #560** (e07aad4e): “The timing test failed CI on noise.”

### Do not let a test's verdict depend on the host platform or the runner's privilege
`environment-independent-tests` · n=4 · PRs=4

- **Rule.** A fix that changes behaviour only on a platform CI does not run must inject that platform's reading as an input and test it on the current host; a fixture whose failure mode a privileged runner can bypass (chmod under root) must be replaced with one nobody can bypass.
- **Also.** A test depending on privilege, permission bits or an external tool driver must pin the mode explicitly (e.g. GOPACKAGESDRIVER=off) or skip when the environment bypasses it, and be run once as the CI user.
- **Why.** A rooted-path fix was indistinguishable from the check it replaced on every CI host, and a chmod-based test skipped under root, leaving a statement uncovered and failing the coverage gate.
- **Probe.** Revert the platform-specific fix and confirm the injected-reading test reddens on Linux; run the coverage gate once as root (or in a root container) and confirm no test skips.
- **Seen in #833** (f61c8abf): “no test CI runs could tell it from the check it replaced”
- **Seen in #344**: “used a `chmod 0o000`”

### Pin the expected order literally; never compare a run with itself or rely on map order
`ordering-tests-pin-the-order` · n=3 · PRs=3

- **Rule.** A test asserting a sort or an order-invariant result must pin the expected order or positions literally; a run compared with its own first output, or two orders compared only with each other, passes for any deterministic wrong answer, and an input whose order comes from a map passes a broken comparator by chance.
- **Why.** A comparator returning 0 passed sort tests, a two-order test passed when every finding was dropped, and a missing version tiebreak passed about one run in five.
- **Probe.** Plant a comparator returning 0 and a dropped tiebreak, run the test with -count=50, and confirm every run fails; plant a uniformly wrong result and confirm the two-order test fails.
- **CLAUDE.md already says:** “the oracle proves order-independence only for the constructs its inputs happen to contain”
- **Seen in #119** (65539ac2): “Two pass tests claimed the reference sites are sorted before any diagnostic is”
- **Seen in #565** (9e87c7da): “The two-order test compared the reports of a source as written and”

### When moving or rewriting tests, keep every case, the defining file, and the external package
`test-moves-preserve-cases-and-scope` · n=3 · PRs=2

- **Rule.** Diff test case names before and after a table rewrite; file each test with the source file that defines the unexported symbol it calls; keep tests that use only the public API in package foo_test.
- **Why.** Consolidations silently dropped a table row and moved public-API tests into the internal package.
- **Probe.** Diff `go test -list . -v` subtest names before and after; check the package clause of every touched test file; map unexported calls to defining files.
- **CLAUDE.md already says:** “external test packages (`package foo_test`) preferred”
- **Seen in #246** (72d25ccc): “Also restores the fractional-seconds row dropped when that table was”
- **Seen in #92** (c3e4e312): “and restore package_test (external) for the files whose remaining tests”

### Make a performance guard time only the code under test and measure the quantity the regression moves
`perf-guards-measure-the-defect` · n=3 · PRs=1

- **Rule.** A cost test or benchmark must time only the function under test with inputs built outside the clock, pin the code path when input size is near a path-selecting threshold, and measure the quantity the defect changes (walk steps, not allocations); confirm it reddens on the reverted code.
- **Why.** An allocation-ratio guard could not see a read-only quadratic walk, a timing test measured the parser instead of the walk, and a benchmark crossed the sniff size cap and timed the byte scanner.
- **Probe.** Revert the fix and confirm the guard fails; add a counter or probe inside the timed region to confirm the target function, on the intended path, is what runs.
- **Seen in #488**: “A guard for this shape has to measure the walk, not its garbage.”
- **Seen in #488**: “so `sniff` took the byte-scan path and the "fixed" numbers I first measured were the scanner, not the walk at all.”

### Do not let an oracle's equality options or exclusions hide the difference it exists to catch
`no-equivalence-that-erases-the-distinction` · n=2 · PRs=2

- **Rule.** An equality, round-trip or order-invariance oracle must not use an equivalence option (EquateEmpty and similar) or exclude a field unless a probe shows the exclusion is needed. Any option or exclusion that erases the distinction the oracle guards turns that oracle blind to exactly its defect.
- **Why.** The round-trip oracle compared with EquateEmpty and so hid null-versus-[] flips, and the order-invariance oracle excluded Naming.Hint, the very field the order-dependent hint bug lived in.
- **Probe.** For each cmpopts option or ignored field in an oracle, remove it and rerun the corpus; then plant the flip the oracle guards (null vs [], a differing Hint) and confirm the oracle goes red with the option in place.
- **CLAUDE.md already says:** “Break what a test claims to catch.”
- **Seen in #18** (73fac1cd): “That blanket equating also masks a real null-vs-[] flip”
- **Seen in #375**: “no longer excludes `ir.Naming.Hint` at all”

## 9. Bounds, budgets and performance

Bounded everything, measured honestly: a bound sits where the allocation is, a cap reports, and a speed claim is measured end to end on a real shape.

### Give each bound its own name and size, charge it where the work lands, and make zero-cost units count
`budget-accounting-sound` · n=10 · PRs=7

- **Rule.** A resource bound must be sized to the quantity it protects (not borrowed from recursion depth), must not be spent on a speed structure when it is a hang guard, must bound the allocation itself rather than trim afterwards, must charge the document where the work lands, and must charge a minimum for zero-unit entries.
- **Also.** A work bound must follow the real code path it prices (including the library's own duplicate-key handling), count only items the budgeted reader adds, degrade only the item that tripped it rather than latching scan-wide, and be measured against published specs at scale so valid input never reaches it.
- **Why.** Reusing maxCycleDepth let a 180 KB spec retain 2.2 GB, charging an index to the pair memo halved the anti-hang guard, empty mappings were indexed for free past the budget, and a two-document chain escaped its bound.
- **Probe.** For each budget touched, write an adversarial input that maximises the charged quantity (empty mappings, long chains across two documents, many keys) under a tight limit and measure RSS/allocations; confirm the cap trips.
- **CLAUDE.md already says:** “Bounded everything.”
- **Seen in #94** (5c93ddfa): “The mapping view added for alias and merge-key resolution reused maxCycleDepth”
- **Seen in #379** (96eeddb7): “A zero-pair mapping charges nothing, so it is indexed unconditionally”

### Put the cheap guard first and build strings, allocations and derivations only on the branch that uses them
`cheap-guard-before-expensive-work` · n=9 · PRs=7

- **Rule.** Order pure predicates cheapest and most-often-false first, keep early-return guards ahead of sorts and allocations, check the cache before deriving the uncached value, build pointer or diagnostic strings only on the failure path, allocate maps on first write, and stop a pointer read at the first token where the answer cannot appear.
- **Why.** Every $ref site allocated a discarded slice, every example built an unused pointer string, a moved sort ran on every no-op call (twice, once via a merge of main), and a warm cache hit paid two map lookups.
- **Probe.** Benchmark with -benchmem the no-op and success paths of each touched hot function before and after the change (and again after merging main), and require allocs/op not to rise.
- **Seen in #101** (6446320a): “schemaAdmitsNull ran hasUnionSiblings before oneOfAnyOfHasNull.”
- **Seen in #382** (4c06ee4e): “Moving the sort out of `undeclaredKeys` makes it run on the calls that previously short-circuited”

### Justify a performance change end to end on the motivating input and every caller, not a best-case microbenchmark
`justify-perf-with-real-measurement` · n=6 · PRs=2

- **Rule.** Before shipping a cache or index, histogram its builds and reads per build over the committed corpus, benchmark every caller of the changed read path at its real access pattern, and measure a full compile of the input class that motivated it.
- **Also.** When a fix removes a quadratic or width-cost scan at one site, grep every reader that walks the same nodes, including reads the resolver never runs, and measure each at scale.
- **Why.** A key index cost more than the scan for 99.5% of real mappings, slowed a build-and-discard caller 1.34-1.50x, and its benchmark measured a shape no caller has while the end-to-end compile showed no gain.
- **Probe.** Instrument build and read counts over testdata/, run each caller's benchmark before and after, and time a full morphic compile of the motivating spec class with benchstat.
- **CLAUDE.md already says:** “Prefer proving a claim over asserting it”
- **Seen in #379** (96eeddb7): “The index is a net pessimization at every mapping width the compiler actually sees”
- **Seen in #379** (96eeddb7): “The benchmark measures a shape no production caller has, covers no width below 64, and end-to-end the change is unmeasurable”

### Report when a cap is hit and never draw negative conclusions from a truncated walk
`bounded-walk-reports-truncation` · n=5 · PRs=4

- **Rule.** A bounded traversal or helper that stops at its cap must surface that (finding, flag consumed at every call site, failing test), a checker's cap must be at least the producer's, and a not-found answer from a truncated walk is not a not-declared claim.
- **Why.** Truncated walks let the oracle call a document clean, reported valid deep references as dangling, and flagged legitimately deep defaults as truncated.
- **Probe.** Build a document at the producer's maximum depth and one past the checker's cap; assert the first is clean and the second reports truncation; grep callers for discarded truncation returns.
- **Seen in #18** (5ea9f320): “so the oracle never claims a document is clean when part of it went unchecked.”
- **Seen in #271** (42055b78): “so an operation buried past the cap made a valid reference to it report as dangling”

### Hoist, cache or index work repeated per item, and pin every superlinear shape with a doubling test
`no-quadratic-rescans` · n=4 · PRs=4

- **Rule.** Inside a per-item or per-child loop, index the mapping read rather than scanning it, hoist parent-scoped work out of the child loop, and cache a materialised expansion instead of recomputing it per read; keep every input shape that ever went superlinear as a committed doubling test.
- **Why.** Recomputing a merge-key closure per read took a 3200-line document 41 seconds, a path item's census ran once per operation, and several shapes that were superlinear in earlier revisions exhausted a budget and crashed.
- **Probe.** Generate the input at N and 2N (paths, operations, merge-chain depth) and time or count work; assert the ratio stays near 2, and keep that generator in the suite.
- **Seen in #94** (8a747a68): “3200-line document took 41 seconds.”
- **Seen in #382** (4c06ee4e): “The path item's whole contribution is recomputed once per operation, producing N byte-identical results”

### Give every loop over untrusted input an explicit cap and every hang-prone test a timeout
`every-loop-and-test-bounded` · n=3 · PRs=3

- **Rule.** Every loop that walks a caller-supplied path or rescans matching lines of untrusted input carries a named bound, timed against an adversarial input that maximises matches, and any test whose regression mode is a hang runs under an explicit -timeout.
- **Why.** A copied pointer walk lost its segment bound, a per-line openapi: reparse spent 760 ms on a crafted file after a 273 ms parse, and a refusal regression would have stalled the coverage run instead of failing it.
- **Probe.** grep new for loops over input-derived collections for a cap constant; generate the worst-case input and time it against the plain parse; confirm the test script passes -timeout.
- **CLAUDE.md already says:** “Every loop, queue, retry, buffer, and timeout has an explicit limit.”
- **Seen in #362** (fe7ccf50): “its loop had no explicit bound where PointerPath counts against”
- **Seen in #499** (48add805): “Without the bound, a crafted 8 MB file of nothing but `openapi: prose` lines, broken at the end, spent 760 ms in the check after a 273 ms parse.”

## 10. Go API and code shape

The styleguide's API rules as they actually broke here.

### Validate nil, typed-nil and zero-value inputs before dereferencing, and keep sibling helpers equally nil-safe
`no-panic-on-nil-or-zero` · n=8 · PRs=7

- **Rule.** Exported functions, checkers and sweeps must test every argument and collection element for nil (including typed-nil) before calling a method on it, an exported type's zero value must be usable, a new accessor or sweep must match the nil handling of its siblings, and a per-item panic barrier must not stop the loop over the rest.
- **Also.** A function that slices or indexes its input must handle every value its doc or a neighbouring comment allows (empty string), or reject it explicitly; a possibly-nil pointer stored in an interface field is screened at the store and the reader.
- **Why.** A report-only verifier panicked on a nil TypeDef, Register called Formats on a nil compiler, a zero-value Registry panicked on its nil map, and one missing object stopped validation of every later object.
- **Probe.** Write a test that passes nil, a typed-nil and a zero-value struct to every new exported function, accessor and sweep, and plants a nil element in each collection the walker visits; require findings or errors, never a panic.
- **CLAUDE.md already says:** “no panics escaping a package”
- **Seen in #18** (5953629f): “checkRegistryKeys dereferenced Types entries via td.Common(), panicking on a”
- **Seen in #321**: “called `c.Formats()` on its argument before checking”

### Delete branches, guards, conjuncts and parameters that no input can reach or change
`delete-unreachable-code` · n=7 · PRs=6

- **Rule.** A fallback branch, recover, nil guard, guard conjunct or parameter is only kept if some real input reaches it and changes an outcome; check the producer's invariant or the dependency's contract before adding one. If none does, delete it (and say why in a comment) rather than covering it with a hand-built test.
- **Why.** A branch no input can redden reads as a live constraint, pads the 100% coverage gate with fake tests, and survives every mutation, so it hides what the code actually guarantees.
- **Probe.** For each new defensive branch or conjunct, replace its condition with true/false (or delete it) and run the package tests plus the golden corpus; if nothing moves, it is dead. Read the library contract (e.g. protocompile recovers panics, descriptor Options() never nil) before guarding.
- **Seen in #185** (144e1a3f): “The branch could not be reddened by any input, which is the shape this repository treats as worse than no branch at all”
- **Seen in #834** (9db3d12d): “DefaultOf returned one could not change a result: replacing it with”

### Scope every exemption, exclusion and suppression to its reason, and retire it when the reason goes
`exemptions-narrow-justified-retirable` · n=6 · PRs=5

- **Rule.** An exemption list entry, nolint directive, quarantine or check skip must cover only the members its stated reason excuses, be validated against the authority it excuses from, state the concrete condition that retires it, and be removed when it no longer suppresses anything or its issue closes.
- **Why.** A class-wide skip or type-level exemption hides real duplicates and nameless nodes; a stale suppression or quarantine reads as a live constraint and keeps a fuzz target from ever running.
- **Probe.** Remove each exemption/suppression in turn and rerun (lint with a cleared cache, the check, the fuzz quarantine): it must fire only on the cases its reason names. Plant two genuinely different items inside the exempt class and confirm they are still reported. For each issue-linked entry, check whether the issue is closed.
- **Seen in #319**: “the skip was wider than its reason: a component response materialized into”
- **Seen in #313**: “Three of the seven `//nolint` directives in the tree suppressed nothing.”

### Grep the package and file for a name before binding it, including tests and imports
`no-identifier-collisions` · n=5 · PRs=5

- **Rule.** Before naming a type, local or parameter, grep the enclosing function, package and its test files for an existing binding of that identifier and for an import of that name; when renaming to fix a collision, include test files in the sweep.
- **Why.** One identifier carried three meanings (ctx), a same-typed shadow would have silently changed output, and test locals shadowed the go/types import or reintroduced a collision production code had just removed.
- **Probe.** git grep -nw '<newName>' in the package including _test.go files and check import lists; enable the linter rules (e.g. importShadow / predeclared) that enforce the styleguide's shadowing rule and run make lint.
- **CLAUDE.md already says:** “never shadow builtins or imports”
- **Seen in #200** (3614dc61): “One is ok instead: recordUnhomedKeywords already had a kept, the slice of”
- **Seen in #837** (b56dabec): “test(ir): stop a local shadowing the go/types import”

### Check mechanical rewrites for changed behaviour, lint residue and dropped rationale
`rewrites-preserve-semantics` · n=4 · PRs=4

- **Rule.** An automated or stdlib-helper rewrite must be checked for the zero-result representation (nil vs empty slice, null vs []), operand evaluation timing when splitting a compound condition, lint/gofmt output, and the 'why not the library' comment it would delete.
- **Why.** slices.SortedFunc turned [] into null, go fix left f := f copies failing lint, and dropping a rationale comment invites a revert to jsontext.AppendToken.
- **Probe.** Run golangci-lint and gofmt on rewritten files; call each rewritten function on empty input and compare JSON with the original; diff removed comments in the rewrite.
- **Seen in #822** (7b1a78cf): “registry, where the loop it replaced returned an empty slice. A caller”
- **Seen in #834** (6547f751): “The hand-written escape this walk used said why it was not jsontext's”

### Export only what production code needs, and never export or return a writable internal collection
`minimal-immutable-exports` · n=3 · PRs=3

- **Rule.** Do not export a package-level slice or map, or return an unexported map from an accessor; expose a copy-returning function or a narrow predicate instead. A symbol only tests read goes in export_test.go.
- **Why.** Exporting residueKeywords and returning the auth map let any importer rewrite what every schema position preserves, and MaxRefChain widened the API for a test alone.
- **Probe.** For each newly exported identifier, grep non-test consumers; for each exported var or accessor of map/slice type, write a test that mutates the result and checks the package state is unchanged.
- **CLAUDE.md already says:** “copy slices/maps at API boundaries; no mutable globals”
- **Seen in #217** (f6433044): “DeclaredAuth handed the map back, which made it writable by every caller”
- **Seen in #219**: “The extraction created an exported mutable global.”

### Measure function length and doc-comment words on every touched symbol before pushing
`measure-size-caps` · n=3 · PRs=3

- **Rule.** After each edit, count the lines of every function touched against the 70-line cap and the words of every doc comment against the 100-word cap, and split or trim before pushing rather than waiting for a linter that may not flag it.
- **Why.** A test-case constructor reached 600+ lines, Load went to 71 lines, and amplification-constant comments ran ~92 lines restating the same figures.
- **Probe.** Run golangci-lint with funlen and go test ./internal/archtest -run TestDocComments on the branch; for test files the linter excludes, measure function length with a script over the diff.
- **CLAUDE.md already says:** “no function may exceed 70 lines or a cognitive complexity of 20”
- **Seen in #111** (9f70dcdb): “retentionCases() had grown to 600+ lines in a single function, well”
- **Seen in #478**: “`Load` was one line over the 70-line cap after the change; its tail — findings and resolution as diagnostics — is lifted into `findings` and `resolve`.”

### Settle public IR names, omission conventions and closed-set validation before merge
`ir-public-contract-settled` · n=3 · PRs=2

- **Rule.** A public IR field's name is settled in the PR that adds it; when a field's type changes, every field that held the old type is re-checked against the design doc's omission convention (nil pointer versus empty value); a string-encoded closed set is validated on decode with a test tying every constant to the validator.
- **Why.** Deferring a field name to an issue turned a free rename into a breaking change, two optional references used "" for none against the documented nil-pointer rule, and an invented PreserveReason round-tripped unroutably.
- **Probe.** grep the diff for new exported IR fields and "tracked separately"/"follow-up" notes; decode a document with an unknown enum string and require an error; check every changed-type field's omitzero/pointer shape against docs/ir-design.md.
- **CLAUDE.md already says:** “Field names and struct shapes in it are the contract”
- **Seen in #119** (79a997ea): “PreserveReason is a bare string enum, so nothing rejected an unknown or”
- **Seen in #119**: “Naming of the `Preserved` field is tracked separately in #120”

### Inject test seams per call or instance, never as package-level variables tests reassign
`no-package-level-test-seams` · n=2 · PRs=2

- **Rule.** A seam for compile, marshal or index steps is a struct field or option, so each call sees only its own choice.
- **Why.** Package-level function variables are mutable global state, serialize the tests that rebind them and are a data race hazard in a pure, reentrant stage.
- **Probe.** Grep the diff for package-level `var .* = func` or function-typed vars assigned in tests; run with -race and t.Parallel.
- **CLAUDE.md already says:** “no package-level mutable state”
- **Seen in #340** (0644e3f5): “The pre-parse index was built through a package-level function variable so a”
- **Seen in #18** (2d1634e4): “become package-level seams that default to”

### Never write into a slice, map or set the caller or an earlier result still holds
`copy-before-write` · n=2 · PRs=2

- **Rule.** Filtering via s[:0] or extending a received set aliases the input; build into a fresh allocation whenever the input or receiver may be read again.
- **Why.** In-place filtering and writes into a shared set silently rewrote callers' data and earlier results.
- **Probe.** Grep the diff for `[:0]` and writes into parameter or receiver maps/sets; add a test that calls twice (or rereads the input) and asserts the first result is unchanged.
- **CLAUDE.md already says:** “copy slices/maps at API boundaries”
- **Seen in #185** (2d41889e): “It filtered in place through parts[:0], so the returned slice shared a backing”
- **Seen in #271** (42055b78): “instead of writing into its set, so extending one call's result cannot mutate”

### Give each member of a closed set its own switch arm; never let the default answer for one
`closed-dispatch-explicit-arms` · n=2 · PRs=2

- **Rule.** Dispatch over roles or kinds names every member explicitly and the default fails loudly (or answers nothing), so a new or unknown member cannot be handled as some other kind.
- **Why.** A missing roleSchemaList case fell through silently, and a default arm answering for allOf made unknown family names report as declared.
- **Probe.** Add a bogus member to the enum or name list and confirm it hits the default and fails or declares nothing; grep changed switches for defaults that return a specific case's answer.
- **CLAUDE.md already says:** “switch-completeness test over the kind enum”
- **Seen in #270** (70dcd0dd): “declaresFamily answered allOf's guard from its default arm, so a name”
- **Seen in #94** (5c93ddfa): “The ref-collection walk's dispatch gains an explicit roleSchemaList case and a”

### Remove a parameter every call site sets to the same or a derivable value
`drop-constant-parameters` · n=2 · PRs=2

- **Rule.** In the change that introduces a helper's callers, delete any parameter that every call passes identically or that is derivable from another argument.
- **Why.** keepKeyword's label always equalled its keyword and multipleOf's formatter always received one keyword, so the parameters carried no choice and invited divergence.
- **Probe.** For each new or changed unexported function, list its call sites (grep or LSP references) and compare the argument values per parameter.
- **Seen in #185** (144e1a3f): “keepKeyword took a label alongside its keyword and was called three times with”
- **Seen in #190**: “`multipleOf`'s conflict formatter took a keyword parameter it was only ever”

### Name a constant or local for what its value actually is, never by a script counter
`identifier-says-what-it-holds` · n=2 · PRs=2

- **Rule.** An identifier must describe what it holds: no counter-generated names (kept0, keptDiags1) left from a rewrite script, and no name whose noun contradicts its value (a prefix with a trailing separator named a path).
- **Why.** Script-numbered locals meant nothing and restarted per file, giving unrelated sites the same name; a mismatched constant name misleads every reader of derived values.
- **Probe.** git diff main...HEAD | grep -nE '^\+.*\b[a-zA-Z]+[0-9]+\b\s*:?=' for numbered locals; for each new constant, compare its name's noun with its literal value.
- **Seen in #200** (3614dc61): “means nothing to a reader and restarts per file, giving two unrelated”
- **Seen in #837** (729821ca): “trailing slash, so it is modulePrefix now and irImportPath is derived”

### Declare any sentinel value in the contract and keep it distinct from a meaningful value
`sentinels-declared-and-distinct` · n=2 · PRs=2

- **Rule.** A sentinel written into a range-checked field must be declared in the IR contract and accepted by the verifier, and must not coincide with a real value (an empty JSON Pointer is the whole document, not 'no place').
- **Why.** An undeclared -1 source index failed every pass diagnostic in the structural oracle, and an empty pointer made the root appear to declare every shared primitive.
- **Probe.** Grep the diff for literal -1, "" or zero used as 'none' in provenance/index fields; run the producer's output through irverify and query the root location to confirm sentinel nodes are excluded.
- **CLAUDE.md already says:** “no in-band errors”
- **Seen in #119** (e80e2a4b): “source-range check rejected every negative index, so any document engine.Run”
- **Seen in #551**: “no pointer. An empty pointer is the RFC 6901 pointer to the whole document, so the IR said the root”

### Bump IRVersion in the same commit as any serialized key or shape change
`wire-change-bumps-version` · n=1 · PRs=1

- **Rule.** Any change to a JSON member name or shape in the IR encoding bumps the version constant, and a consumer pinned to the old version must reject the new document.
- **Why.** Old consumers would otherwise accept a document they cannot read.
- **Probe.** Diff json tags and MarshalJSONTo code in ir/; if any changed, confirm IRVersion changed and a decode with the old version fails.
- **Seen in #149** (f68aab6b): “The wire key for unmodeled constructs changed on this branch, and IRVersion did”

## 11. Scripts and tooling

Gate scripts are code under the same rules: structural parsing, loud failure, no dependence on the caller's environment.

### Isolate scripts and fixtures from shared paths, global config and make -j
`hermetic-scripts-and-fixtures` · n=3 · PRs=3

- **Rule.** Scripts write profiles and temp files to per-run paths, fixtures that run git isolate from global and system config, and a Makefile target whose steps must be ordered or share files declares .NOTPARALLEL or runs them in one recipe.
- **Why.** A fixed cover.out let concurrent runs corrupt each other's statement counts, a global gpgsign setting failed fixture commits, and make -j ran fuzz into testdata/ while coverage read it.
- **Probe.** Run the target twice concurrently, under make -j8, and with GIT_CONFIG_GLOBAL pointing at a config enabling commit.gpgsign; outputs must match a serial clean run.
- **Seen in #225**: “because `cover.out` is a fixed path and concurrent runs can duplicate blocks in it.”
- **Seen in #344** (70458cf1): “Prerequisites of a single target are eligible to run concurrently under -j, so”

## 12. Process, tracker and PR hygiene

The CLAUDE.md commit and PR rules, which no check enforces.

### Write a deliberate limitation or deferred defect at the code that has it, not only in the PR body
`limitation-stated-in-code` · n=9 · PRs=9

- **Rule.** Any limit deliberately left out of scope, known library limitation relied on, or known defect whose reproducer is removed must be stated in a comment at the function or call that has it, with its tracking issue, and also in the PR body, in the same commit.
- **Also.** When a change deliberately leaves something undone (an exemption wider than its reason, an unconditional report where invariant 6 would want a policy switch, declined parts of an issue), state it on the code that holds the gap, name the tracker issue, and use Refs rather than Closes for a partially addressed issue.
- **Why.** Readers reach the code first; a limitation recorded only in a PR body, commit message or tracker is invisible to the next person editing that function.
- **Probe.** For every 'out of scope', 'known limit', 'tracked in #N' or removed seed mentioned in the PR body or commits, grep the diff for a matching comment at the named function or call; any without one is a finding.
- **CLAUDE.md already says:** “A limitation that is deliberately out of scope must be stated in the code and in the PR body, in a place the next reader will actually reach”
- **Seen in #239** (34f99985): “Both are named in the pull request; a reader reaches the code first.”
- **Seen in #270** (da48be4c): “it was recorded only in the pull request body -- a reader arriving at”

### Re-review the final tree after review fixes or a merge of main
`recheck-against-final-tree` · n=8 · PRs=7

- **Rule.** After review fixes land or main is merged in, re-review the branch as it will merge: re-run the probes that comments and the PR body cite on the merged tree, re-diff every semantic change main made to files the branch touches, and rewrite any PR-body or comment claim the later commits changed.
- **Also.** After merging main into a branch, re-run every sweep, invariant and widened check the branch relies on over the merged tree, since merged-in code can reintroduce what the branch removed or predate its rule.
- **Why.** Merges here silently dropped main's wider change in a conflict, left comments asserting behaviour main had changed, and left PR bodies stating facts a later commit falsified; unreviewed fixes merged as if reviewed.
- **Probe.** After git merge main, run git diff <merge-base>..main -- <files the branch touches> and confirm each hunk survives in HEAD; re-run each command quoted in the PR body and comments on HEAD and diff its output against the quote; re-read the PR body top to bottom against git diff main...HEAD.
- **CLAUDE.md already says:** “Review the **final** state, not an intermediate one.”
- **Seen in #837** (a1c4f4aa): “One change is not carried over yet. Main widened the ValueKind”
- **Seen in #382** (a498517b): “Stale after the merge: `additionalOperations` is no longer dropped.”

### Run the 100% coverage gate on the final tree before pushing or claiming it holds
`coverage-gate-on-final-tree` · n=5 · PRs=5

- **Rule.** Every new branch ships with the test that executes it, in the same change, and the gate is run on the exact tree being pushed before a PR body says it holds. A new package needs its own test file, because coverage is attributed per package test binary.
- **Why.** One uncovered statement fails CI; pushing below the gate forces a follow-up commit and makes any PR-body claim that the gate holds false.
- **Probe.** Run `make coverage` (scripts/check-coverage.sh, statement count, not the rounded percentage) on the final commit; for a new package confirm it has a _test.go of its own; grep the PR body for coverage claims and match them to that run.
- **CLAUDE.md already says:** “Coverage is a gate at exactly 100%, not a target.”
- **Seen in #22** (14c5c37c): “Restores compilers/openapi statement coverage to 100%.”
- **Seen in #373**: “package is instrumented only by its own test binary, so one with statements and no test”

### Check commit subjects, PR titles and bodies against the message rules before first push
`commit-and-pr-message-form` · n=5 · PRs=5

- **Rule.** Every commit subject and the PR title must be Conventional Commits, imperative, at most 72 characters (the title at most 72 including the appended ' (#NNN)'); a breaking commit carries ! and explains the break in its own body; PR bodies carry no tool attribution or session artifacts.
- **Why.** Over-length subjects and prose titles land on main via squash and cannot be fixed without rewriting a pushed branch; a break marked only in the PR title is invisible in branch history.
- **Probe.** git log --format=%s main..HEAD | awk 'length($0)>72'; printf '%s (#999)' "<title>" | wc -c; grep the title against ^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: ; grep the body for 'Generated with' lines other than the required attribution.
- **CLAUDE.md already says:** “The PR title is the commit subject.”
- **Seen in #834**: “One commit subject on the branch, `hoist a content scalar through hoistFormatScalar`, is 77 characters, over the 72-character cap.”
- **Seen in #157** (f6fc15c3): “This PR opened with the same defect.”

### Keep the diff to in-scope files and keep scratch notes out of the tree
`diff-contains-only-in-scope-files` · n=4 · PRs=4

- **Rule.** Every changed path must belong to the PR's stated scope; session planning docs and working notes must be ignored before the first commit that would add them, and an ignore rule may be removed only after confirming local tools no longer generate those paths.
- **Why.** Design-only PRs carried go.mod changes, plans and temporary rewrite notes were committed toward main, and a removed .claude/ ignore entry had to be restored.
- **Probe.** git diff --stat main...HEAD and match each path to the PR title; git ls-files docs/ | grep -iE 'plan|notes|rewrite|superpowers'; for a removed .gitignore line, git status --ignored before and after.
- **CLAUDE.md already says:** “Keep PRs scoped to one logical change”
- **Seen in #5** (0b692476): “The planning documents under docs/superpowers/plans/ are local”
- **Seen in #119**: “it is temporary and should be deleted when the rewrite finishes.”

### Check the tracker for a recorded decision before changing what it covers
`follow-recorded-decisions` · n=2 · PRs=2

- **Rule.** Before touching a shared view, cache or plan dependency, search issues for a decision about it and follow it or state the departure in code and PR body; a recorded blocking edge must not contradict the reason the item was split out.
- **Why.** Changes settled questions issues had framed (a second memo on a view #404 wanted settled first) or made a standalone defect wait on the programme it was split from.
- **Probe.** `gh issue list --search "<file or symbol>"` for every shared structure touched; confirm the PR body cites any hit.
- **CLAUDE.md already says:** “A line you were forced onto may already have a decision recorded.”
- **Seen in #379** (b92870d6): “This PR is #338's index, adds a second memo to that same shared view, and cites neither #404 nor #402”
- **Seen in #158** (c493c8d2): “Also unblocks the naming-divergence defect, which had been made to wait”
