# IR lessons

What an IR change meets that `docs/ir` does not say: other branches claiming the same version,
hand lists that must name a new field, guarantees a type held without stating them, and what the
verifier and codecs must reach.

## The PR merging second takes the next version
`version-collisions`

- **Rule.** A branch bumping `IRVersion` takes the version after its base's, whatever other open
  PRs claim, and the PR merging second keeps `main`'s row and entry, moves the constant to the next
  version and appends its own under it. Any of the constant, the `generations` row and the
  version-history entry can merge silently, so check for a repeated version after each merge
  moving one.
- **Why.** #1065 and #1066 both set 0.18.0 and the constant merged with no conflict; #734 and #735
  conflicted only in the history; and two setting 0.30.0 conflicted in the constant, their rows and
  entries merging clean, doubled (#1378).
- **Probe.** After the merge, `grep -o 'version: "[0-9.]*"' ir/irversion_test.go | sort | uniq -d`
  prints only 0.3.0 and 0.4.0, repeated by design, and
  `grep -oE '^- \*\*[0-9]+\.[0-9]+\.[0-9]+\*\*' docs/ir/version-history.md | sort | uniq -d` prints
  nothing.

## Classify every field you add to package ir, walker state included
`classify-ir-fields`

- **Rule.** A struct field anywhere in package `ir`, unexported walker state included, meets the
  classification tests in `ir/irverify`: an integer field needs an `integerFields` entry, an
  ID-typed field an `idPositions` entry, a `[]string` a `stringLists` entry, and no map may key an
  ID to an ID. Shrink walker state before classifying it. Remove a deleted field's entry with it:
  each table's test refuses a stale entry.
- **Why.** Walker state added for `ir.FlattenedProperties` failed all three tests, and an alias
  memo mapping an ID to an ID was refused (#1141); a pair of reference-key fields failed the
  integer test (#737).
- **Probe.** `go test ./ir/irverify -run 'AreAllClassified|EveryShapeIsOneIdShapeOfReads'`; plant
  an entry for a field that does not exist in each table and watch its test fail.

## A new named string type says whether it is a class of ID
`classify-named-strings`

- **Rule.** A named string type in package `ir` needs an `identityClasses` entry in
  `ir/irverify/duplicates_internal_test.go` saying whether it is a class of ID and, if so, what
  resolves its references. An entry for a type no longer declared fails too.
- **Why.** A class nothing resolves goes unchecked in silence (#50), and
  `references/ir-lessons.md#classify-ir-fields` does not reach a type, so a change sized from it
  missed this list (#1225).
- **Probe.** `go test ./ir/irverify -run TestIdentityClasses_AreAllClassified`.

## A position that only mentions a type is no use of it
`mention-is-no-use`

- **Rule.** A new `TypeID` position that names a type without using it stays out of `ir.TypeEdges`
  and each reader that takes the class as a use, as reachability does; a reader resolving references
  keeps it, since a mention can dangle.
  `TestAppendTypeIDs_ReadsWhatTheTypeGraphReads` holds `appendTypeIDs` to `ir.TypeEdges` for a type
  definition's fields, and nothing holds it for a position only an operation holds.
- **Why.** `appendTypeIDs` reads an operation by reflection, so such a position becomes a GraphQL
  root and the check on field arguments goes silent on its type, which classifying the position
  does not show (#1223).
- **Probe.** `git grep -nE 'TypeFor\[(ir\.)?TypeID\]' -- '*.go' ':!*_test.go'` lists the code readers
  (documents: `references/ir-lessons.md#reference-lists-in-documents`).
  Add a struct holding a `TypeID` to `ir.Docs`, leave it out of `ir/edges_test.go`'s oracle, and
  `TestAppendTypeIDs_ReadsWhatTheTypeGraphReads` fails until `appendTypeIDs` skips the struct.

## A new TypeID position is read in the documents listing references
`reference-lists-in-documents`

- **Rule.** Check a new `TypeID` position against each list of references a design document gives,
  as well as the readers in code: a reader the documents specify and no code implements has no line
  for a grep to find and no test to redden.
- **Why.** `docs/emitters/refine/decorators.md` listed what an aliased type's collapse rewrites
  without the new `DocRef.Type`, so a span naming a decorator pointed at a type with no
  declaration, past the PR's sweep of the code readers (#1219).
- **Probe.** `git grep -nE 'each reference|every reference|mapping or default' -- docs/emitters docs/ir`
  finds the lists, each read for the new position.

## Find every list a new ID class must join
`new-id-class`

- **Rule.** A new ID type joins every hand list of ID classes: the kind constants and
  `ir.IDKinds`, the compiler's kind table, `irverify`'s prefixes, ID types and identity classes,
  the reference label tables, and each hand-written loop over the registries. List the files by
  searching for existing ID types together, never from memory.
- **Why.** Adding a server ID class touched lists across `ir`, `ir/irverify` and
  `compilers/compile`, one of which only `TestIDPositions_AreAllClassified` notices missing, and
  the registry checks loop over each registry by hand (#1144).
- **Probe.** `git grep -l EnumMemberID -- '*.go' | xargs grep -l GroupID` lists the files; each
  must name the new type.

## Expect a new ID namespace to rewrite every golden document
`new-id-namespace`

- **Rule.** `Document.IDSpaces` declares what a compiler can mint, not what a document uses, and
  goldens store it, so a new entry in a compiler's `Namespaces` changes every golden document, where
  a version bump changes none. Show that no other line moved, land a spec that mints the namespace,
  and trial-merge the open PRs
  (`.claude/skills/morphic-shipping-prs/references/merging-main.md#trial-merge-open-prs`).
- **Why.** One entry in the `auth` list changed all 128 goldens by one hunk each, and
  `TestNamespaces_DeclaredAreExactlyWhatTheCorpusUses` refused it while no spec minted it (#1213).
- **Probe.** `go test ./compilers/openapi/e2e -run 'TestConformance|TestGolden' -update`, then
  `git diff -U0 -- testdata | grep -E '^[-+] ' | sort | uniq -c` prints the namespace's lines alone.

## Never give a type in ir a method named typeDef
`sealed-marker-reserved`

- **Rule.** The `TypeDef` sum's completeness tests find its members by the unexported marker
  method `typeDef`, so any type in `ir` with a method of that name becomes a phantom member. When
  those tests redden after adding a type, check its method names before touching `allKinds`.
- **Why.** A walker's `typeDef` method failed the kind-list tests and miscounted the payload
  carriers in `pass` (#814).
- **Probe.** `go test ./ir -run TestTypeDef` and `go test ./pass -run TestEncodingCarriers`.

## Restore what the old type guaranteed when you change it
`type-held-invariants`

- **Rule.** When a map becomes a list, a set a slice, or a single field a list, write down what
  the old type guaranteed (unique keys, no order, no nil) and restore each as an `irverify` check,
  unless a valid source may break it, in which case the compiler folds and reports. Probe first
  whether the parser can produce the violation.
- **Why.** `OAuthFlow.Scopes` went from a map to a list for source order, and a scope named twice
  verified clean until `irverify` gained a check for a duplicate scope (#878).
- **Probe.** In a test, build a document holding what the old type could not and assert
  `irverify.Verify` reports it.

## Sweep the predicates that name a struct's fields when it grows one
`predicates-that-list-fields`

- **Rule.** When an `ir` struct gains a field, find the code that names its siblings one by one (an
  emptiness test, an equality, a comparison with the zero literal) and give each such predicate a
  reflection test that sets every field alone, so the next field added fails there. A value a test
  calls fully populated takes `references/ir-lessons.md#populated-value-sets-every-field`.
- **Why.** A part-encoding emptiness check named each field, so an encoding declaring only the new
  `allowReserved` would have been dropped, with every golden and conformance test green (#1144).
- **Probe.** `git grep -nE '\.<Sibling> (==|!=)|== ir\.<Struct>\{'`, with `<Sibling>` a sibling
  field's name and `<Struct>` the struct's.

## Hold a value a test calls fully populated to every field
`populated-value-sets-every-field`

- **Rule.** A value a test calls populated in full names a struct's fields by hand, so a field the
  struct gains is left zero there with every round trip green. Hold it with
  `assertNoFieldZero` in `ir/helpers_test.go`, which `assertJSONContract` and
  `assertTypeDefRoundTrip` there call, naming each field it leaves zero with the reason. A struct no
  test hands `assertNoFieldZero`, as one round-tripped arm by arm or held only inside another, is
  held by none until the check of #1679 lands: until then, set each field it gains in a round-trip
  value.
- **Why.** #1367's four fields went unset in two values their contract tests called fully
  populated, so a `json:"-"` tag on any of them passed both (#1670).
- **Probe.** Each test whose comment `git grep -niE 'fully( populated|$)' -- 'ir/*_test.go'` hits
  calls one of those three.

## Read what the documents assume of a field before widening its values
`widened-field-values`

- **Rule.** When a lowering starts writing a value a field did not hold (a primitive, a mode, a
  media type), grep the field and its old values across `docs/`, read each normative statement for
  one that assumes the old set, and change it or name the gap where it stands. No emitter reads the
  value yet, so no test objects, but it is a change of meaning, which `docs/ir/ir-version.md` bumps
  for.
- **Why.** The default streaming policy gained `+json-seq` while the `Framings` default named media
  types exactly, and `Enum.ValueType` gained `date`, `uuid` and `decimal` while Go lowers a closed
  enum to typed consts, which those types lack (#1191, #1202).
- **Probe.** `git grep -nF -e '<field>' -e '<old value>' -- docs`, and list each statement read in
  the PR body. Then take `references/ir-lessons.md#widened-field-consumers`.

## A verdict landing on another node changes no meaning
`verdict-on-another-node`

- **Rule.** A lowering that changes which node a field names, mints nodes of a kind the IR already
  declares, or adds an ID namespace changes no key's meaning, IDs being opaque to consumers
  (`docs/ir/identity.md`), and bumps nothing. A value a consumer switches on, as a primitive, a
  mode or a media type, is `references/ir-lessons.md#widened-field-values`'s, and bumps.
- **Why.** The redeclared-field merge added the `composed` namespace and minted composed models
  (#1181), then nodes holding two declarations' bounds (#1307), each without a bump, while the
  skill's values bullet read as asking a bump for each (#1657).
- **Probe.** `git show <merge> --stat -- ir/document.go docs/ir/version-history.md` lists neither
  file for #1181's `20017d99b` or #1307's `7cf3f95fc`.

## Ask each consumer of a widened field what it did with the old value
`widened-field-consumers`

- **Rule.** When a field's values widen, list each consumer that reads the field and ask what it did
  with the old value, since one may depend on it with no sentence saying so.
- **Why.** A file body's `Content.Type` held only `bytes`, so a plain upload needed no rule and had
  none, and grepping the field and its old values on the request side found nothing (#1191, #1204).
- **Probe.** List in the PR body each consumer asked, and its answer.

## Check a design rule against every input it reads
`design-rule-over-its-document`

- **Rule.** When an IR or emitter design states a rule, placement argument or step order, list
  its inputs from their declarations: each field of every IR type it reads, every `TypeKind` and
  enumeration value those fields reach, each kind's `docs/ir` page, and each sibling section of
  its document, cited, not restated
  (`.claude/skills/morphic-verifying-changes/references/derive-never-maintain.md#single-source-no-restatement`).
  Say whether each changes the answer, compiling a minimal spec for one the golden spec lacks.
  Check a step order by `references/ir-lessons.md#step-order-both-ways`.
- **Why.** A placement covered one per-target table and not its twin, an import step preceded the
  steps renaming what it imports (#1019), and #1329's rules missed fields, enumeration values,
  kind documents and restated sections (#1402).
- **Probe.** `go doc -all ./ir <Type>` and
  `awk '/^\040{0,3}(\140{3}|~{3})/{c=!c}!c&&/^#/{print FNR": "$0}' <document>`; an input
  with no stated effect is unchecked.

## Check a step order against what later steps change and declare
`step-order-both-ways`

- **Rule.** Check a step order both ways: what each step reads that a later one changes, and what
  each adds to that a later one declares. A step adding to a declaration runs after the step
  declaring it.
- **Why.** An import step preceded the steps renaming what it imports (#1019). At 4cb221a80,
  `docs/emitters/refine/steps.md` placed `LowerRedaction` before `BuildRequestShaping`, whose
  request types it gives a redacting form, and `MarkStability` before the steps declaring the
  methods it marks; reading only what each step reads passed both (#1630).
- **Probe.** List the order with
  `awk -F'|' '/^\| \140/{print NR": "$2}' docs/emitters/refine/steps.md`, and beside each step
  what it reads and adds to, against the steps after it, as the ordering paragraph of
  `docs/emitters/refine/pipeline.md` argues each placement.

## Prototype a specified mechanism against every requirement on its layer
`prototype-against-every-requirement`

- **Rule.** Prototype a mechanism a design document specifies on the target's own library, against
  every requirement the emitter documents put on the layer it runs in, not only the property it is
  for, and name each requirement's result in the document.
- **Why.** Three decode mechanisms in turn passed a prototype of their cost and failed a sibling
  document's requirement: builders could not reach a variant in another output unit, a second
  `json.WithUnmarshalers` replaced a caller's own, and Kotlin `@Contextual` fields threw under a
  caller's own `Json` (#1329); a `must|never|only` grep missed #1340's "every error message"
  (#1631).
- **Probe.** With `<layer>` each entry of `ls docs/emitters` the mechanism runs in,
  `grep -rnwE 'must|never|only|every|each|all|always' docs/emitters/README.md docs/emitters/contract docs/emitters/testing docs/emitters/<layer> docs/emitters/targets/<target>`
  lists the candidate requirements; each that bears on the mechanism has a prototype run, or is
  unmet.

## Shape a declaration from what an additive change leaves alone
`declaration-reads-its-definition`

- **Rule.** A plan fact that shapes a declaration, such as a closed sum against a struct of
  pointers, reads what adding a member, a variant or an operation leaves as it was, such as the
  type's value type and row, never one member's value or the operations reaching it. Where it
  must read a fact such a change moves, the document names that fact and the break it causes the
  declaration's callers.
- **Why.** A fix to #1329's member rule read which way a union travels, so a new operation would
  flip its form; an enum lowered as text once a member passed `int64` would turn
  `type Code int64` into `type Code string` (#1334).
- **Probe.** Add a member, a variant and an operation to a fixture: the declaration's form holds,
  or the document names its break.

## Count what a design assumes about real input before choosing it
`measure-the-premise`

- **Rule.** Before choosing a shape a consumer will use, write the job they do with it and the fact
  about real input it rests on, then count it on the golden spec and the corpus, through `ir`'s
  own derivations
  (`.claude/skills/morphic-reviewing-prs/references/design.md#program-from-the-document`).
  A program running the proposed rules shows only that they are total, since it assumes that fact;
  a worked example, chosen because it fits, shows nothing.
- **Why.** A design kept one error type per body, as a worked example had it, and a run placed every
  failure, yet most operations with several error bodies used one type for all, status telling
  failures apart, uncounted until a reviewer asked (#1167).
- **Probe.** For each decision in the PR body, name its premise and the command that could
  have falsified it. A decision with neither is unproved.

## Weigh designs that differ in what they rest on, not only in how they spell it
`weigh-the-framing`

- **Rule.** Include a design the issue's framing leaves out, and make the designs differ in the fact
  they key on. Call a choice forced only after naming what the comparison held fixed and changing
  it, and give each rejected option's failing input, not only its cost.
- **Why.** Comparing a leaf per body with client and server branches held fixed that the exception
  is the failure, and concluded only one fact could be a class. A wrapper lay outside the comparison
  until a reviewer asked, and the count that followed showed a status tree nesting inside fault
  (#1167).
- **Probe.** For each option rejected as impossible, compile or cite the constraint, then restate it
  without what the options shared and check that it still holds.

## Give every field a check that rejects its zero and malformed values
`verifier-rejects-every-degenerate-value`

- **Rule.** For each field a consumer reads or routes on, the verifier rejects its empty, null,
  absent, wrong-kind and malformed values, read from the unfiltered source and mirroring every
  refusal the encoder makes. A derived identifier or canonical name is rejected wherever its grammar
  cannot produce it, with one rejecting case per property: casing, character set, segmentation,
  agreement with its coordinate.
- **Why.** Empty keys and reasons, an absent `irVersion` and ill-formed UTF-8 all verified clean, so
  a verified document could still fail to encode or misroute (#329, #361).
- **Probe.** Write the zero, empty, null and wrong-kind variant of each field and assert each is a
  violation; encode every document that verifies clean.

## Check every reference position and every ID class, not a subset
`verifier-reaches-every-reference-and-id-class`

- **Rule.** A validator or verifier over references or IDs must cover every position a reference can
  occupy (map keys, PropIDs, plain int indices) and every ID class and node class the document
  holds, derived from the type graph rather than from the data a fixture happens to contain.
- **Why.** Dangling rename targets, channel, message and PropID references, and malformed operation
  IDs verified clean because the walk saw only TypeIDs or only registry-reached nodes (#149, #119).
- **Probe.** Plant a dangling or malformed value in each reference-bearing field and each ID class
  in turn and assert a violation fires for each; a field whose plant stays green is uncovered.

## Witness each character class a text predicate decides
`character-predicate-witness-per-class`

- **Rule.** A predicate over text (cased, blank, invisible) is tested against the producer's exact
  transformation, with a witness per general category each set it claims spans, never a sample:
  white space's Zs, Zl, Zp and Cc, default-ignorable's Cf, Mn, Lo and Cn, and caseless letters.
  Redefining one rechecks it at each call site, as two rules sharing it can ask different questions.
- **Why.** unicode.IsUpper flagged neutral caseless runes as cased, and TrimSpace let zero-width and
  filler-only aliases pass as non-blank (#361). A definition of "upper" settled for one
  case-boundary rule was applied to both, and the second went unverified until a mutation sweep
  (#189). `isInvisible`'s tests passed with its white space narrowed to Zs (#1584).
- **Probe.** Assert its verdict on each witness and producer output at each call site, taking
  `references/ir-lessons.md#witness-past-the-bmp` and
  `references/ir-lessons.md#sweep-a-published-property` where they apply.

## Witness past the BMP each term whose set reaches there
`witness-past-the-bmp`

- **Rule.** Each term of a text predicate whose set reaches past the Basic Multilingual Plane, such
  as `unicode.Cf` or `unicode.Other_Default_Ignorable_Code_Point`, has a committed witness above
  U+FFFF that it alone decides. A row an earlier term answers first, such as an exclusion or
  `unicode.Variation_Selector`, witnesses nothing of a later one.
- **Why.** `isInvisible`'s tests passed with its `Cf` term, and separately its
  `Other_Default_Ignorable_Code_Point` term, held below U+10000, and still did with a row per class
  and per range bound, as its rows past the BMP reached earlier terms (#1621).
- **Probe.** Narrow each such term alone with `&& r < 0x10000`: a committed row reddens.

## Sweep a predicate over every code point where Unicode publishes its property
`sweep-a-published-property`

- **Rule.** A predicate claiming a property Unicode publishes, such as default-ignorable or white
  space, is swept by a throwaway test over every code point against the UCD files for
  `unicode.Version`, beside its witnesses, and each range bound it holds has a witness on each side.
- **Why.** A fix for #832 had a witness per class in `isInvisible`, yet moving a range's lower
  bound from U+FFF9 to U+FFF8 reddened no test (#832).
- **Probe.** The sweep prints no code point where the predicate and the UCD files differ.

## Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point`

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not
  allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types
  became non-nil and the round trip never settled (#264).
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode
  again, and cmp.Diff the two decoded values.
