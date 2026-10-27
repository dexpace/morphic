# Lossless lowering and honest diagnostics

`CLAUDE.md`'s "lossless by default" invariant in practice: every source construct lands somewhere,
and every diagnostic is true, located, and decided from what actually happened.

## Point every diagnostic at the node and document it is about
`diagnostic-provenance-names-the-real-site`

- **Rule.** A diagnostic's provenance names its own site, not the root or the holding node: a
  sub-entry points at its own keyword, an overlay-added node names the overlay, and a pointer is
  composed from the declaration the value was read from, not a `$ref` mount, with a source index the
  table holds. Distinct sibling sites stay distinct so deduplication cannot merge them, and two
  checkers reporting one code spell the location alike.
- **Why.** Root-stamped notices for several unmounted items deduplicated into one naming none, and
  chained `$ref` findings sent readers to a line in the wrong file (#382, #565).
- **Probe.** Resolve every emitted pointer through the parsed source in a test; compile two sibling
  instances and assert two diagnostics; try a chained `$ref` and an overlay graft.

## Make each diagnostic's message, code and severity match the case that fired
`diagnostic-text-names-what-fired`

- **Rule.** Every branch, position and failure cause of a diagnostic needs wording true for that
  branch: name the specific cause, the earlier duplicate, the unit it counts and the correct repair;
  reuse a code only when its documented meaning fits; set severity from what the input means.
- **Why.** Messages described allOf branches for sibling positions, blamed the wrong failure
  direction, rendered blank slots, and reused a code whose documented meaning consumers select on
  (#92, #332).
- **Probe.** Trigger every branch and severity of the diagnostic, including zero-valued inputs and
  both directions of a symmetric check, and read each rendered message against the fixture.

## Prove each keyword the reader accepts changes the output
`twin-diff-every-accepted-keyword`

- **Rule.** Every source keyword or field a change reads, lists, documents or comments on must reach
  a typed field, an Unmodeled entry or a diagnostic. A scanner list, a field doc comment naming a
  keyword as its home, or a census comment saying the rest is captured is a claim to test, not
  evidence.
- **Why.** Fields such as allowReserved, propertyNames, minItems and x-* on most objects compiled to
  byte-identical IR with or without them, which breaks the lossless-by-default invariant with a
  clean exit (#345, #348).
- **Probe.** For each keyword in the vocabulary, compile two specs that differ only in that keyword
  and diff the IR plus diagnostics; byte-identical output is a silent drop. Enumerate the keywords
  from the source schema, not a hand-picked sample.

## Report malformed or unusable input instead of mapping it to a valid-looking value
`no-silent-fallback-to-a-valid-looking-value`

- **Rule.** A parser fallback, default switch arm or sentinel must never turn unreadable, forbidden
  or unusable input into a value that looks deliberate ({0,0} status, empty AuthRequirement, guessed
  kind, empty method); emit a diagnostic and a no-value result, and switch over closed enums without
  a guessing default.
- **Why.** A typo'd responses key arrived as a declared default and a malformed security entry
  granted public access, both with no diagnostic (#327, #263).
- **Probe.** Compile each malformed or forbidden spelling the parser can meet and diff its IR
  against the deliberate spelling; identical IR or an empty diagnostic list is the defect.

## Make every early return, fast path and carrier constructor carry the shared path's data
`fast-paths-carry-what-the-shared-path-does`

- **Rule.** When a helper returns early (existing owner, interned node, zero children) or a new
  construct builds its carrier inline or calls a shared constructor, check it applies everything the
  main path applies: every argument at every call site, every fill function, every hook.
- **Why.** An early return on an existing owner and an interning short-circuit skipped the
  constraint path, and an inline-built header Property skipped the detail filler, dropping docs,
  extensions and constraints silently (#363, #149).
- **Probe.** List every return before the shared step and every call site of the constructor; write
  a case that reaches each with the node already present or the child count zero, and diff against
  the main path.

## Merge redeclared fields per attribute and test both orders
`merge-each-field-by-rule-in-both-orders`

- **Rule.** A merge or reconciliation of duplicate declarations, or two writers into one field or
  map, needs an explicit rule for every field of the struct; never keep one side wholesale, never
  assign a zero value over another source's value, and never skip a shared-map write because some
  other construct already holds the key.
- **Why.** Reconciliation kept the first allOf branch's constraints and visibility wholesale, so the
  result depended on branch order and later branches' keywords vanished with no diagnostic (#8,
  #92).
- **Probe.** List the struct's fields beside the merge and point at the rule for each; then compile
  complementary keys from each side in both declaration orders and cmp.Diff the documents.

## Pair every new check with must-stay-silent fixtures for valid input
`check-stays-silent-on-valid-input`

- **Rule.** A conflict detector, refusal or structural guard must flag only what it can prove wrong:
  test satisfiable narrowings, opaque types, every valid spelling a spec version allows,
  indirections the compiler unwraps, and file kinds it never reads, and run it over the corpus
  before merge.
- **Why.** New checks rejected legal multipart bodies and valid 3.0 boolean bounds, and warned on
  allOf narrowings that are satisfiable (#149, #92).
- **Probe.** For each check write a must-fire and a must-stay-silent fixture, including alias/$ref
  indirection and every valid spec-version spelling, then run morphic-harness over testdata and
  require no new diagnostics.

## Own every refusal and bound; never borrow one from a dependency
`explicit-guard-not-incidental`

- **Rule.** A boundary check is the compiler's own, not a dependency failing or a misreading that
  happens to refuse. When deleting or loosening a path that declined inputs or incidentally bounded
  recursion, add the explicit bound in the same change and re-probe every input class the old path
  handled.
- **Why.** A self-`$anchor` reference overflowed the stack on `main` because only a misreading
  refused one shape, and lifting an anchor skip let a fixture grow past 57 GB (#185, #541).
- **Probe.** Probe the dependency directly with the input you rely on it to refuse; revert the
  incidental guard and run its inputs under a memory and time limit.

## Decide a kept/dropped announcement from what was written
`announce-kept-only-after-the-write`

- **Rule.** A diagnostic that says something was kept, preserved or dropped must be decided from the
  output that was actually written, after the write succeeded; a rebuild that replaces a node must
  retract the superseded build's diagnostics and Unmodeled entries.
- **Why.** Warnings claimed a preservation that never happened when a conversion failed or a merge
  key hid the construct from the raw reader (#382, #185).
- **Probe.** Force the write to fail (bad YAML-to-JSON value, merge-key-supplied construct) and
  assert the 'kept' diagnostic disappears; diff the Unmodeled map against what each announcement
  claims.

## Lower into the IR's typed field, and pick the Unmodeled reason by its strength
`unmodeled-reason-and-typed-home`

- **Rule.** Before keeping a keyword verbatim, check the IR for a typed field. When the design
  promises a lowering, compile each promised case. Classify a kept entry by whether the IR reflects
  it weaker, stronger or not at all, and test the class on the construct's commonest real form.
- **Why.** `itemEncoding` was kept raw beside a typed field, `$dynamicRef` lowered silently to any,
  and an open tuple was tagged `no_ir_home` (#119, #149).
- **Probe.** Grep `ir/` for a field holding each kept keyword, and assert the promised IR kind and
  `UnmodeledReason` for each case the design names.

## Produce exactly one finding per defect
`one-defect-one-finding`

- **Rule.** When a new check overlaps an existing one (a generalised check, an overriding reader),
  retire or suppress the overlap for the whole class in the same change. A malformed construct is
  reported once, by the grammar-driven load phase: before adding a lowering diagnostic for one,
  probe whether load already reports every instance.
- **Why.** A bad numeric bound and an ill-formed string each drew two reports, and per-reader
  reports of a `null` schema missed positions load already reported (#539, #23, #1066).
- **Probe.** Plant each defect the overlapping checks cover and assert exactly one entry for it.

## Prove every return path and caller delivers its diagnostics
`every-return-path-delivers-diagnostics`

- **Rule.** When a function's diagnostics must survive every return path or every caller, add a test
  per path and caller that fails when that report is removed; before deleting a diagnostic as
  redundant, show the other stage reaches every case including early exits and panics.
- **Why.** A refusal path omitted the stream-drop report and callers could drop the
  operation-extension warning with no test failing (#480, #565).
- **Probe.** Delete the report on each return path and each caller in turn and confirm a test
  reddens.

## Read every ok flag, error and exit status
`check-every-failure-signal`

- **Rule.** Every failure signal -- a comma-ok flag, an invariant-guaranteed lookup miss, a shell
  command's exit status in a Makefile recipe -- must be checked and surfaced as a diagnostic or a
  stopped recipe; never continue on a value whose failure was ignored.
- **Why.** Callers returned silently when an interned node was missing, dropping docs and examples
  with no diagnostic; discarding an ok flag would have produced a tag no component answers to; an
  unchecked mktemp ran the whole suite with an empty -coverprofile (#119, #828).
- **Probe.** grep the diff for `_ :=`, `, _ =`, bare `return` in lookup-miss branches and unchecked
  command substitutions in recipes; force each failure (delete the node, make mktemp fail via
  TMPDIR=/nonexistent) and confirm a diagnostic or non-zero exit.

## Move a null member into nullability, and test null in every position
`null-member-handling`

- **Rule.** When a spelling encodes nullability as a list member (type arrays, enum members), move
  the null into the nullability bit, derive kinds from the surviving members, and test null first,
  middle and last; a shared nullability predicate must also be tested on a schema whose enum or
  sibling excludes null.
- **Why.** A nullable enum in the 3.1 type-array spelling degraded to a union of literals, a leading
  null fixed the member kind so the enum degraded, and references admitted null that the enum
  forbade (#267).
- **Probe.** Compile {type:[string,null], enum:[null,a]}, the same with null last, and
  {type:[string,null], enum:[a,b]}; assert an Enum kind, correct nullability per reference, and no
  degradation diagnostic.

## Land a keyword in one IR place whichever path lowers it
`keyword-placement-path-independent`

- **Rule.** When the same keyword or annotation can reach the IR through several lowerings or past a
  dedup guard, its placement must not depend on the path; a guard deciding to skip residue may only
  run after confirming the data has a home elsewhere.
- **Why.** Visibility residue was dropped exactly for parameters whose schema owns a node, and a
  constraint beside allOf went to Unmodeled while beside properties it went to Constraints,
  defeating lossless capture (#149, #363).
- **Probe.** Compile the keyword beside every container shape (properties, allOf, $ref, parameter
  with and without an owned node) and diff where it lands; confirm none disappears.

## Cover every spelling the spec allows for one concept
`every-spelling-of-one-concept`

- **Rule.** When a change reads a concept the source can spell several ways (nullability as a
  keyword or a null branch, `schema` or `content`, ASCII and non-ASCII twins, case-insensitive
  tokens in another case, both orders of an adjacency), write one case per spelling and assert
  identical IR, severities and exit codes.
- **Why.** The null-spelling fix handled keyword spellings only, so a null `oneOf` branch dropped
  `Nullable` at every reference, and a header written with `content` lowered as untyped (#101,
  #185).
- **Probe.** Build a table of equivalent spellings and assert identical IR and diagnostic codes
  across it.

## Fixture every version, location and override level a rule depends on
`cover-every-version-location-and-level`

- **Rule.** When a value's source or legality varies by spec version, parameter location or
  declaration level, test each value of that axis: a name from a 3.2-only field needs a 3.0 and 3.1
  fallback, the most specific level wins, a default is legal at its exact location, and fixtures
  respect the format's co-location rules.
- **Why.** Every 3.0 and 3.1 server and response reached the IR nameless, operation-level servers
  lost to the path item's list, and a querystring parameter got a style the spec forbids (#261,
  #272).
- **Probe.** Compile one document per supported version and one declaring the keyword at path-item
  and operation level; grep the IR for empty names per entity kind.

## Lower one construct at every position that can hold it
`same-construct-at-every-position`

- **Rule.** When a change lowers, keeps or reports a keyword at one schema position, enumerate each
  position the spec allows (declaration, inline, property, parameter, header, `allOf` branch, beside
  a `$ref`, nested object) and make each reach the same path or record what it cannot carry.
- **Why.** One construct lowered two ways by position, and parameter, header and nested-object
  positions dropped keywords the property position kept (#149, #156).
- **Probe.** Compile one fixture writing the construct, with a sibling keyword and an `x-` key, at
  every position, and diff the IR and diagnostics per position.

## Decide and test the combination when keywords co-occur
`test-co-declared-keywords-together`

- **Rule.** When a lowering reads several keywords that may be co-declared (prefixEncoding with
  itemEncoding, readOnly with writeOnly, minimum with exclusiveMinimum, allOf with oneOf, explode
  with an empty style, siblings a rewrite target cannot carry), test the pair directly; a dropped
  partner goes to Unmodeled or a diagnostic, never only into a message.
- **Why.** Ordered early-return guards and single-path lowerings kept one keyword of a pair, so
  {minimum:10,exclusiveMinimum:0} and {minimum:10} compiled byte-identical (#346, #322).
- **Probe.** For each keyword pair the code reads, compile the pair, each keyword alone and neither,
  and diff all four documents; two identical outputs for different inputs is the drop.
