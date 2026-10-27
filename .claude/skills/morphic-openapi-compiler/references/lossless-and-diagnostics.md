# Lossless lowering and honest diagnostics

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

Invariant 2 in practice: every source construct lands somewhere, and every diagnostic is true, located, and decided from what actually happened.

## Point every diagnostic at the node and document it is about
`diagnostic-provenance-names-the-real-site`

- **Rule.** A diagnostic's provenance must name its own site (not the document root or the holding node), resolve to a node that exists, use a source index the table holds, name the document the position was read from, and keep distinct sibling sites distinct so deduplication cannot merge them.
- **Also.** Provenance for a sub-entry points at its own keyword, and for a node added by an overlay or merge layer names that layer's document; a combined entry is attributed by the positions that wrote its parts. Lower layers must get attribution through an injected locator, not the raw base source index.
- **Also.** A provenance pointer or verifier location must address a node that exists: compose it from the declaration the value was read from (not a $ref mount or a reflection-only struct path), and two checkers reporting the same code must spell the location identically.
- **Why.** Root-stamped notices for several unmounted items deduplicated into one naming none, and chained $ref findings sent readers to a line in the wrong file.
- **Probe.** Resolve every emitted pointer through the parsed source in a test; compile two sibling instances and assert two distinct diagnostics; exercise the diagnostic behind a chained $ref and an overlay graft.
- **Seen in #382** (a498517b): “`provenance: svc.Provenance` is the document root, so every unmounted item's servers diagnostic is byte-identical”
- **Seen in #565** (3d0e08e3): “A finding reached through a $ref put its line and column against "the”

## Make each diagnostic's message, code and severity match the case that fired
`diagnostic-text-names-what-fired`

- **Rule.** Every branch, position and failure cause of a diagnostic needs wording true for that branch: name the specific cause, the earlier duplicate, the unit it counts and the correct repair; reuse a code only when its documented meaning fits; set severity from what the input means.
- **Why.** Messages described allOf branches for sibling positions, blamed the wrong failure direction, rendered blank slots, and reused a code whose documented meaning consumers select on.
- **Probe.** Trigger every branch and severity of the diagnostic, including zero-valued inputs and both directions of a symmetric check, and read each rendered message against the fixture.
- **Seen in #92** (0eb9ac18): “alongside allOf rather than inside a branch.”
- **Seen in #332** (20007a20): “A worker that stopped partway through left zero values in its results, and the”

## Prove each keyword the reader accepts changes the output
`twin-diff-every-accepted-keyword`

- **Rule.** Every source keyword or field a change reads, lists, documents or comments on must reach a typed field, an Unmodeled entry or a diagnostic. A scanner list, a field doc comment naming a keyword as its home, or a census comment saying the rest is captured is a claim to test, not evidence.
- **Why.** Fields such as allowReserved, propertyNames, minItems and x-* on most objects compiled to byte-identical IR with or without them, which breaks the lossless-by-default invariant with a clean exit.
- **Probe.** For each keyword in the vocabulary, compile two specs that differ only in that keyword and diff the IR plus diagnostics; byte-identical output is a silent drop. Enumerate the keywords from the source schema, not a hand-picked sample.
- **Seen in #345**: “and nothing read it: two documents”
- **Seen in #348** (ccd0fc24): “minItems, maxItems and uniqueItems reached the IR in no form at all wherever”

## Report malformed or unusable input instead of mapping it to a valid-looking value
`no-silent-fallback-to-a-valid-looking-value`

- **Rule.** A parser fallback, default switch arm or sentinel must never turn unreadable, forbidden or unusable input into a value that looks deliberate ({0,0} status, empty AuthRequirement, guessed kind, empty method); emit a diagnostic and a no-value result, and switch over closed enums without a guessing default.
- **Why.** A typo'd responses key arrived as a declared default and a malformed security entry granted public access, both with no diagnostic.
- **Probe.** Compile each malformed or forbidden spelling the parser can meet and diff its IR against the deliberate spelling; identical IR or an empty diagnostic list is the defect.
- **Seen in #327**: “was lowered to `ir.AuthRequirement{}`, the encoding `ir-design.md` §9 reserves”
- **Seen in #263**: “A `responses`-map key the compiler could not read was folded into the `{0, 0}` status”

## Make every early return, fast path and carrier constructor carry the shared path's data
`fast-paths-carry-what-the-shared-path-does`

- **Rule.** When a helper returns early (existing owner, interned node, zero children) or a new construct builds its carrier inline or calls a shared constructor, check it applies everything the main path applies: every argument at every call site, every fill function, every hook.
- **Why.** An early return on an existing owner and an interning short-circuit skipped the constraint path, and an inline-built header Property skipped the detail filler, dropping docs, extensions and constraints silently.
- **Probe.** List every return before the shared step and every call site of the constructor; write a case that reaches each with the node already present or the child count zero, and diff against the main path.
- **Seen in #363**: “Ownership is what made it terminal: `hoistDeclarationHome` resolves to whatever node the”
- **Seen in #149** (70d1ad0f): “the hoisted node was built without them and the interning short-circuit meant the”

## Merge redeclared fields per attribute and test both orders
`merge-each-field-by-rule-in-both-orders`

- **Rule.** A merge or reconciliation of duplicate declarations, or two writers into one field or map, needs an explicit rule for every field of the struct; never keep one side wholesale, never assign a zero value over another source's value, and never skip a shared-map write because some other construct already holds the key.
- **Why.** Reconciliation kept the first allOf branch's constraints and visibility wholesale, so the result depended on branch order and later branches' keywords vanished with no diagnostic.
- **Probe.** List the struct's fields beside the merge and point at the rule for each; then compile complementary keys from each side in both declaration orders and cmp.Diff the documents.
- **Seen in #8** (4a0c13d5): “and the surviving shape depended on branch order.”
- **Seen in #92** (0eb9ac18): “Reconciliation also took the first branch's constraint set wholesale, so”

## Pair every new check with must-stay-silent fixtures for valid input
`check-stays-silent-on-valid-input`

- **Rule.** A conflict detector, refusal or structural guard must flag only what it can prove wrong: test satisfiable narrowings, opaque types, every valid spelling a spec version allows, indirections the compiler unwraps, and file kinds it never reads, and run it over the corpus before merge.
- **Why.** New checks rejected legal multipart bodies and valid 3.0 boolean bounds, and warned on allOf narrowings that are satisfiable.
- **Probe.** For each check write a must-fire and a must-stay-silent fixture, including alias/$ref indirection and every valid spec-version spelling, then run morphic-harness over testdata and require no new diagnostics.
- **Seen in #149** (2fac7fce): “The encoding-key check added alongside the typed key rejected legal specs, so”
- **Seen in #92** (0eb9ac18): “so refining a field in a later allOf branch was reported as a contradiction.”

## Own every refusal and bound explicitly; never rely on a dependency or another bug to provide it
`explicit-guard-not-incidental`

- **Rule.** A boundary check must be the compiler's own explicit check, not a dependency failing or a misreading that happens to refuse. When deleting or loosening a path that declined some inputs or incidentally bounded recursion, add the explicit bound or refusal in the same change and re-probe every input class the old path handled.
- **Why.** A self-$anchor reference crashed the process with a stack overflow on main because only a misreading refused one shape; lifting an anchor skip let a fixture grow past 57 GB; deleting a byte scan exposed prose files claimed as broken specs.
- **Probe.** Probe the dependency directly with the input class you rely on it to refuse; revert the incidental guard and run its covered inputs under a memory/time limit (ulimit, -timeout) on both sides of any old size threshold.
- **Seen in #185** (c721f4f9): “the resolver library failing rather than from any check of its own.”
- **Seen in #541** (82454bc8): “The library crashes the process with an unrecoverable stack overflow”

## Decide a kept/dropped announcement from what was written
`announce-kept-only-after-the-write`

- **Rule.** A diagnostic that says something was kept, preserved or dropped must be decided from the output that was actually written, after the write succeeded; a rebuild that replaces a node must retract the superseded build's diagnostics and Unmodeled entries.
- **Why.** Warnings claimed a preservation that never happened when a conversion failed or a merge key hid the construct from the raw reader.
- **Probe.** Force the write to fail (bad YAML-to-JSON value, merge-key-supplied construct) and assert the 'kept' diagnostic disappears; diff the Unmodeled map against what each announcement claims.
- **Seen in #382** (4c06ee4e): “The warning asserts a preservation that did not happen: it is gated on what the *source model* wrote, not on what was actually kept.”
- **Seen in #185** (0705181d): “nodeToRaw returned nil both for an absent construct and for one whose YAML”

## Lower into the typed field the IR has, honour design-promised lowerings, and pick the Unmodeled reason by the IR's actual strength
`unmodeled-reason-and-typed-home`

- **Rule.** Before preserving a keyword raw, check the IR for a typed field; when the design doc promises a lowering plus fallback, compile each promised case; classify a preserved entry by whether the IR reflects it weaker, stronger or not at all, and test the classification on the construct's most common real-world form.
- **Why.** itemEncoding was preserved raw despite a typed field, $dynamicRef lowered silently to any against §4.7, an open tuple was tagged no_ir_home, and a required-narrowing oneOf was tagged degraded_lowering so validation emitters skipped it.
- **Probe.** For each keyword the change preserves, grep ir/ for a field that holds it; compile each case the design doc names for the construct and assert the promised IR kind and UnmodeledReason, never the top type.
- **Seen in #119** (8e357a79): “the compiler preserved itemEncoding raw under extensions instead and reported”
- **Seen in #149** (c1aaab76): “$dynamicRef. It lowered silently to any, contradicting both halves of §4.7's”

## Fail when a loader or parser reads less than it was asked to
`loaders-fail-when-they-read-less`

- **Rule.** A source loader, package loader or document parser must error when its result is smaller than its input: a pattern matching nothing, a build-excluded file, a test-only package, or a line it cannot place.
- **Why.** Checkers reported clean on code they never saw, and the matrix reader dropped every row below an unexpected line.
- **Probe.** Feed the loader a pattern matching nothing, a build-tagged file, a test-only package and a malformed line mid-table, and assert each errors rather than returning a shorter result.
- **Seen in #837** (1db27104): “A file the current build leaves out was not read. A declaration in”
- **Seen in #342** (ff7909bc): “The matrix reader ended the table at the first line not starting with a”

## Produce exactly one finding per defect
`one-defect-one-finding`

- **Rule.** When a new check overlaps an existing one (a generalised check, an overriding reader, a scanner over delimiter runs), retire or suppress the overlap for the whole class in the same change.
- **Why.** A bad numeric bound and an ill-formed string each drew two reports, inflating counts and confusing which check owns the defect.
- **Probe.** Plant each defect the overlapping checks cover and assert the diagnostic or violation list holds exactly one entry for it.
- **Seen in #539** (915f28ea): “Keeping them would report every such string twice.”
- **Seen in #23** (32c99973): “bound keywords unconditionally, not only for in-range values. Morphic re-reads”

## Prove every return path and caller delivers its diagnostics
`every-return-path-delivers-diagnostics`

- **Rule.** When a function's diagnostics must survive every return path or every caller, add a test per path and caller that fails when that report is removed; before deleting a diagnostic as redundant, show the other stage reaches every case including early exits and panics.
- **Why.** A refusal path omitted the stream-drop report and callers could drop the operation-extension warning with no test failing.
- **Probe.** Delete the report on each return path and each caller in turn and confirm a test reddens.
- **Seen in #480**: “drop omitted on the refusal path → survived until the refusal-path test was added, then red.”
- **Seen in #565** (fd3cc339): “reaches: a resolver panic ends the walk, and an entry after it was then”

## Read every ok flag, error and exit status, and report an impossible miss instead of returning silently
`check-every-failure-signal`

- **Rule.** Every failure signal -- a comma-ok flag, an invariant-guaranteed lookup miss, a shell command's exit status in a Makefile recipe -- must be checked and surfaced as a diagnostic or a stopped recipe; never continue on a value whose failure was ignored.
- **Why.** Callers returned silently when an interned node was missing, dropping docs and examples with no diagnostic; discarding an ok flag would have produced a tag no component answers to; an unchecked mktemp ran the whole suite with an empty -coverprofile.
- **Probe.** grep the diff for `_ :=`, `, _ =`, bare `return` in lookup-miss branches and unchecked command substitutions in recipes; force each failure (delete the node, make mktemp fail via TMPDIR=/nonexistent) and confirm a diagnostic or non-zero exit.
- **Seen in #119** (876116e1): “and returned in silence when it was missing”
- **Seen in #828** (babe91b3): “The recipe mints a profile path with mktemp, but recipe lines run without”

## Strip null into nullability and derive attributes from the surviving members, testing null in every position
`null-member-handling`

- **Rule.** When a spelling encodes nullability as a list member (type arrays, enum members), move the null into the nullability bit, derive kinds from the surviving members, and test null first, middle and last; a shared nullability predicate must also be tested on a schema whose enum or sibling excludes null.
- **Why.** A nullable enum in the 3.1 type-array spelling degraded to a union of literals, a leading null fixed the member kind so the enum degraded, and references admitted null that the enum forbade.
- **Probe.** Compile {type:[string,null], enum:[null,a]}, the same with null last, and {type:[string,null], enum:[a,b]}; assert an Enum kind, correct nullability per reference, and no degradation diagnostic.
- **Seen in #267**: “`enumMembers` classified that member as inadmissible, so the whole enum fell to the”
- **Seen in #267**: “Member kinds are reconciled against the members actually kept”

## A keyword must land in the same IR place, never dropped, whichever lowering or guard handles it
`keyword-placement-path-independent`

- **Rule.** When the same keyword or annotation can reach the IR through several lowerings or past a dedup guard, its placement must not depend on the path; a guard deciding to skip residue may only run after confirming the data has a home elsewhere.
- **Why.** Visibility residue was dropped exactly for parameters whose schema owns a node, and a constraint beside allOf went to Unmodeled while beside properties it went to Constraints, defeating lossless capture.
- **Probe.** Compile the keyword beside every container shape (properties, allOf, $ref, parameter with and without an owned node) and diff where it lands; confirm none disappears.
- **Seen in #149** (ba887c58): “dropped them for precisely the parameters whose schema owns a node”
- **Seen in #363**: “a constraint written beside `allOf` is kept under `Unmodeled` rather than in”

