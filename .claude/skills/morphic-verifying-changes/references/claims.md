# Claims against reality

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

Every sentence in a comment, doc, commit or PR body that states behaviour is a claim to execute, not to read — and to execute outside the case its author measured.

## Check every 'never/always/only/every/none' claim against every site it quantifies over
`universal-claims-enumerate-every-site`

- **Rule.** Before a comment, assertion message or normative doc asserts an absolute (never, always, only, the only, every, read by nothing, nothing modelled behind, validated once), enumerate every caller, return path, producer, position, route and member it quantifies over and check each; otherwise scope the claim to the sites checked or name the exceptions. A field's doc must describe what every writer of that field actually writes.
- **Why.** Absolute claims written from one representative case were false at other sites: 'read by nothing' while constraints.go read it, 'never reports a spec-level problem' with three reachable paths, a field contract no producer obeyed.
- **Probe.** For each absolute word in a changed comment, list the quantified sites mechanically (git grep writers of the field, gopls callers, every return statement, every composition position such as allOf/anyOf/items) and probe one input per site through the compiler, reading the full diagnostic list; one counterexample means narrow or delete the claim.
- **Seen in #111** (7f4d3130): “minProperties is not read by nothing”
- **Seen in #190** (6af887e8): “The doc comment said the error return "never reports a spec-level problem", and three of its four paths are”

## Run the exact input a comment describes before stating what it lowers to, drops or fails as
`probe-behaviour-claims-by-running-the-input`

- **Rule.** A comment, test comment, design-doc sentence or PR body that says what a specific input does (lowers to X, is dropped, reaches a branch, deadlocks, is unsatisfiable, is order-independent, would fail these cases) must be backed by compiling or running that literal input, in both declaration orders where order is claimed; pin the observed value in a test where it can drift.
- **Why.** Claims written from the code's shape were wrong when executed: 1XY lowered to the defined 1XX range, a 'deadlock' was a stack overflow, maxLength 10 and 20 intersect, and an 'unreachable' branch was reachable via a YAML merge key.
- **Probe.** Compile the input named in the comment (go run ./cmd/morphic or the harness) and read the emitted IR and full diagnostic list; for order claims swap declaration order and cmp.Diff the documents; for 'unreachable', try merge keys, aliases and inline vs $ref positions; for 'would fail', actually run the alternative.
- **Seen in #263** (a47ca6f9): “"1XY" lowering to a range OpenAPI does not define when it lowered to 1XX”
- **Seen in #229** (a3caef0b): “The reproducer list described cycle_pointer_whitespace_self as a deadlock”

## Verify claims about specs, libraries, toolchains and standards at the source, with a version
`external-claims-checked-at-source`

- **Rule.** A comment or doc that states what an external spec, standard, library, Go toolchain feature or third-party codebase does must be checked against that source's text or a probe of it, cite the section number or dependency version it was read at, and name the component that actually consumes the input.
- **Why.** Claims from memory were wrong: the yaml.v3 alias budget never fires on node decode, json/v2 still sits behind goexperiment, a resolver handles some anchor fragments, wrong spec section numbers, and a prior-art survey written without reading oagen's source.
- **Probe.** For each external claim in the diff, open the dependency source at the go.mod version (go mod download -json; read the file) or the spec section, or write a one-off probe calling the library with the cited input; confirm the citation carries a version or section and names the reader actually used (e.g. speakeasy, not yaml.v3).
- **Seen in #480**: “`decode`'s doc comment claimed the decode is "the one place the yaml.v3 alias budget is spent, which is what bounds a billion-laughs expansion"”
- **Seen in #525** (e7a268c2): “"Generally available" became "built by default".”

## Test the reason a comment gives, not just its verdict, by removing the cited guard
`verify-the-stated-reason`

- **Rule.** When a comment credits a guard, call, set, value or missing symbol with a reason (this check distinguishes X, the set terminates cycles, the value is incidental, the IR cannot hold this, a repeat matches twice), verify the reason itself: remove the guard and rerun the cited input, trace consumers of the 'incidental' value, grep for the 'missing' symbol. A correct verdict with a false reason is still a defect.
- **Why.** Comments here credited a redundant directory guard, justified a visited set by cycles the depth cap already handled, named IR blockers that ir.LongRunning already modelled, and presented an unrecorded asymmetry as code intent.
- **Probe.** Comment out the guard or call the comment credits, rerun the input the comment cites, and see whether the stated effect appears; if the input still passes, the citation is wrong. For a named blocker, git grep the symbol; for an 'incidental' value, list every branch that reads it.
- **Seen in #368** (1b73b8ee): “The walk's comment attributed the set to cycle termination and cited a”
- **Seen in #193** (bfb84e82): “It credited the directory guard”

## Run the real external tool on a planted input before relying on it
`probe-the-real-external-tool`

- **Rule.** When a script or check depends on a linter, jq, git output, the go command or a CI action, plant the construct and run the real tool from an unrelated directory as CI runs it: clean files, disabled linters, null arrays, colons in paths, CRLF, exported env vars.
- **Why.** nolint directives naming disabled or unknown linters passed silently, jq exited 0 with a shrunk set, and a setup-go bump spammed GOTOOLCHAIN warnings in every step.
- **Probe.** Plant each directive or data shape in a scratch file, run the tool and the check against it from a different cwd, and compare their verdicts form by form.
- **Seen in #367**: “so a directive in a clean file is never read at all, and the”
- **Seen in #367** (8cf56224): “`jq -r '.Enabled[].name'` cannot iterate the null that golangci-lint emits when”

## Name every branch, outcome and uncovered input in a function's or option's contract
`doc-names-every-outcome`

- **Rule.** A doc comment on a function, type, option or diagnostic must name every outcome its branches produce (each failure condition in the lead sentence, each grade of a strict mode, each live error arm, what happens at the limit) and every input class that breaks a stated guarantee, not only the important or common case.
- **Why.** Contracts that named one outcome made live branches read as dead code, hid that strict mode reports without refusing, and promised once-per-node or order-independent behaviour without naming the re-read and declaration-order cases that break it.
- **Probe.** For each changed doc comment, list the function's return paths and switch arms and map each to a sentence in the comment; any unmapped path is a finding. For a guarantee, construct the worst-case input (multi-line, re-read through another $ref, swapped declaration order) and run it.
- **Seen in #267**: “`enumMembers`' lead sentence enumerated two conditions for `ok=false`”
- **Seen in #269** (e955e9cc): “The note above ParseFloat named only the IsInf outcome, which reads as if”

## Reconcile a doc's summary line, headers and universals with its own rows and sentences
`doc-agrees-with-itself`

- **Rule.** Within one doc, comment, table or PR body, a status line, section header, category split or universal sentence must agree with the rows and sentences beside it; after adding a row or rule, re-read the surrounding paragraph and headers for contradiction, including against the invariants the doc itself defines.
- **Why.** Docs here said 'not yet implemented' beside rows marked landed, filed a non-refused row under the refused header, said aliases are held to 'none of that' right after a rule they are held to, and made a name part of an ID contrary to invariant 3.
- **Probe.** After editing a table or paragraph, re-read every header, status line and universal in the same section against each row; grep the same input across sibling tables (e.g. divergence tables vs corpus comments) and confirm they classify it the same way.
- **Seen in #185** (1011beb9): “said "not yet implemented" while four rows were marked landed”
- **Seen in #246** (72d25ccc): “The block-form !!binary sat under a header reserved”

## Check claims about scripts, CI, linters and caps against what they actually execute
`tooling-claims-match-the-gate`

- **Rule.** A doc or comment that says what a script, CI step, action, linter or metric does (what it groups by, what it counts, whether it is generated, what order it runs in, what it puts on PATH) must be checked against the code path or a run of it, and any command quoted in docs must be copied from the gate step it mirrors and run verbatim on the branch it describes.
- **Why.** README claimed per-package coverage while the script computes one global total, documented a gofmt form the gate does not run, called a hand-written test 'generated', and a CI comment's false PATH claim caused a second linter install.
- **Probe.** Run every command a changed doc quotes, verbatim, and compare its output to the gate's; read the script or action entrypoint for each 'enforces/counts/runs every' claim and name exceptions (e.g. lint is not a Makefile target); for CI environment claims, print the environment inside a job.
- **Seen in #315**: “the script computes one global”
- **Seen in #315**: “ran `gofmt -l .`; the gate runs”

## Check each verb and flow word in a doc comment against the body and call sites
`doc-verbs-match-the-body`

- **Rule.** Every verb a doc comment uses about its function (checks, records, caches, reads once, intersects, passes, copies) and every ordering word (above, before) must match what the body does today and how call sites use it; describe what is returned and what is left to callers, and state a formal grammar or type check with the exact acceptance condition the code implements.
- **Why.** Comments described memoization the function lacked, a check it delegates to callers, a value flow that did not exist yet, set intersection where keys are adopted, and a grammar with digits* where the code requires a digit.
- **Probe.** Read the changed doc comment verb by verb next to the function body; for flow words, list call sites with git grep and trace the real call order; for a stated grammar or type rule, write a table test with the boundary input (zero digits, defined vs predeclared type) and confirm it agrees with the comment.
- **Seen in #266** (a0010adf): “Also drop a claim from thingIDVisibility's doc comment that it checks for”
- **Seen in #290** (1df07c58): “readDeclarations said it reads the identities”

## Make every name and cross-reference in a comment resolve to one visible target
`comment-references-resolve`

- **Rule.** Every identifier, test name, field name and in-comment cross-reference ('for that reason', 'see above') must resolve to exactly one existing target visible from the comment's package: no function locals in GoDoc, no constants from packages above, no nonexistent tests, and owner-qualified field names when a short name is ambiguous.
- **Why.** Comments pointed at tests that do not exist, at locals the API reader cannot see, at a constant in a higher layer, and used 'Auth' for two fields reaching opposite conclusions.
- **Probe.** For each identifier in a changed comment, git grep -nw it in the package (and go doc the package) to confirm it exists and is visible; for each cited test, go test -list '<Name>' ./... must print it; for each cross-reference phrase, read the referent sentence.
- **Seen in #365** (83254807): “named a test that does not exist, as did the comment on”
- **Seen in #266**: “The rationale comment used "Auth" for two different fields, two sentences apart, reaching opposite conclusions about each.”

## Build every code sample in a doc under its own rules and check every cited precedent
`doc-code-obeys-own-rules`

- **Rule.** Code examples in READMEs, style guides and design docs must compile and run under the flags and banned-construct lists the same document mandates, and any code location a doc cites as precedent must actually say what the doc claims.
- **Why.** Exemplars that do not compile or use the forms the guide bans teach the wrong thing, and a design doc prescribed a package-level mutable registry citing code that says the opposite.
- **Probe.** Extract each sample into a scratch module and build/run it with the mandated flags and linter; open each cited file/line and compare.
- **Seen in #1** (90e384fa): “typescript: declare error-class and resource-handle fields explicitly”
- **Seen in #760**: “It did not compile: the range variable `d` and `doc` were declared and never used.”

## Test a safety, idempotence, neutrality or stability argument in every direction it can fail
`prove-safety-arguments-in-every-direction`

- **Rule.** A comment or design claim that a refactor is behaviour-neutral, a mismatch fails safely, a rewrite is a fixed point, a sort is stable, or a derived cache never exceeds its source must be checked in each direction (lost, won, unknown; neighbouring elements the rewrite changes; equal keys; every gate condition of the source), and a derived gate should reuse the source's result rather than a copied admission test.
- **Why.** Partial arguments hid real bugs: a family that wins the election was dropped silently, sort.Slice broke a promised stable order, a copied memo gate dropped a condition, and unifying three disagreeing grammar copies was called neutral.
- **Probe.** Enumerate the outcomes the argument covers and write one test per direction (mismatch that wins vs loses, equal sort keys, a rune after the split, the source cache's other gate). For neutrality, run the unified and old versions over the corpus and diff the IR.
- **Seen in #270** (70dcd0dd): “dispatchOf's comment claimed a family listed in familyOrder but absent”
- **Seen in #379** (b92870d6): “keyIndex reasons only from the budget test and silently drops expand's other memoize gate, so the doc claim”

## Back every performance, memory or noise figure with a measurement on the claimed workload
`measure-before-quantifying`

- **Rule.** Do not claim a speedup, memory bound, noise immunity or quantified effect until a before/after measurement on the claimed workload shows it; a count bound described as a memory bound must be measured in retained bytes, and measured figures belong in the PR description with their revision, not in source comments where they go stale.
- **Why.** A per-walk view was kept for a benefit that never reproduced, a shared count bound let retained memory grow 3.4 times, and a 'no leverage' timing claim was disproved by a CI failure.
- **Probe.** Run go test -bench and a heap profile after runtime.GC on the stated workload before and after; compare retained bytes per charged unit; run timing tests under taskset load and record spread. Any number in a changed source comment is moved to the PR body.
- **Seen in #379** (b92870d6): “One bound covering both is what keeps a second memo from doubling the memory the first one was capped at”
- **Seen in #362** (8499518b): “reproduced — timings were identical and PointerPath already memoizes each”

## Check 'same', 'distinct' and 'shared' claims on both sides with differing inputs
`claims-that-two-things-agree`

- **Rule.** When a comment says two traversals cover the same set, two code paths behave alike, two inputs share a behaviour, two derivations yield distinct results, or several input forms have the same effect, check each side separately, on non-fresh destinations and on inputs that differ only in what the derivation drops, and cite the code path for each.
- **Why.** Parity checked on one side or on a fresh value hid divergences: a populated destination decoded differently, URL templates differing only in punctuation collided, and only one of two directive spellings actually suppressed nothing.
- **Probe.** Write a two-row probe per claimed equivalence: run both paths on a pre-populated value, both input spellings, and a pair of inputs differing only in characters the canonicalisation drops; cmp.Diff the results.
- **Seen in #264** (009429dc): “brings TypeRegistry "in line with" its plain-map sibling registries; decoding”
- **Seen in #261** (3e25c4fa): “serverName's comment claimed the whole URL template "distinguishes exactly the”

## State in PR bodies and commits only the verification results actually observed
`pr-claims-match-evidence`

- **Rule.** A PR body or commit message must report what was observed on the final tree: a fix it says landed must be in the diff, a mutation result must name the sites it was run at with per-site outcomes, and 'all routes' or 'dropped everywhere' must come from probing each route; generalising one probed site to its mechanism is an overclaim.
- **Why.** PR bodies here claimed fixes that had not landed, 'all six went red' when one did not, and that a pair was dropped on mounted items when it survived on every mounted operation.
- **Probe.** Before pushing, re-run each mutation and probe quoted in the body against the final test files and paste the observed output; for any 'every/all routes' sentence, list routes in code and run one input per route.
- **Seen in #349**: “each went red, and all six were restored before the gate ran.”
- **Seen in #135**: “A mutation verified at one site was written up as holding for every site of its kind”

## Back every behavioural claim, scope boundary or known-wrong answer in a comment with a test
`comment-claims-held-by-test`

- **Rule.** When a comment says something propagates automatically, that a measurement proves a property, that a check deliberately stops at a boundary, or that a known-wrong answer is left standing, add a test that fails if that statement stops being true (pin the known-wrong answer in a row naming its issue).
- **Why.** Unenforced claims rot silently: widening a check's scope or breaking the described wiring leaves every test green, and a benchmark no CI job runs checks nothing.
- **Probe.** For each such comment, make the claim false (drop the wiring, widen the scope, fix the known-wrong row) and confirm a test reddens; if the claim depends on a benchmark, confirm a CI step runs it.
- **Seen in #111** (e511a0b7): “constant added to the block without a matching entry in Annotations() left”
- **Seen in #361** (b75eec57): “which nothing held: hoisting seen into checkNaming's closure made”

## Re-check a doc's references, renames and capability claims against its final state
`docs-consistent-after-edit`

- **Rule.** After editing a doc, confirm every referenced type is defined, every 'see above/below' lands on its target, renamed terms left no residue (articles, plurals), and capability matrix cells match behaviour the same PR changed.
- **Why.** An undefined MessageRef, inverted cross-references, 'a emitter' and a matrix cell denying client streaming the PR enabled all shipped as stale prose.
- **Probe.** Grep backticked identifiers for definitions; grep the old term with neighbouring articles; for behaviour changes, grep docs/ir-spec-matrix.md for the capability and rerun the matrix test.
- **Seen in #352** (f1b7274f): “matrix still said OpenAPI reaches client streaming in no way at all, which the”
- **Seen in #3**: “and `MessageRef` was referenced but never defined”

