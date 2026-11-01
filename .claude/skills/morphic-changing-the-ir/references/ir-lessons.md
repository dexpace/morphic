# IR lessons

What an IR change meets that `docs/ir` does not say: other branches claiming the same version,
hand lists that must name a new field, guarantees a type held without stating them, and what the
verifier and codecs must reach.

## Take the next version when another PR claims yours
`version-collisions`

- **Rule.** Two open PRs that bump `IRVersion` from one base claim one version: the constant
  merges silently, and only the `generations` row and the version-history entry conflict. The
  PR merging second keeps `main`'s row and entry, moves the constant to the next version, and
  appends its own row and entry under it.
- **Why.** #1065 and #1066 both set 0.18.0, and the constant merged with no conflict; #734 and #735
  both bumped to 0.10.0 and conflicted only in the history.
- **Probe.** List the open PRs that set a version, or whose diff did not load:
  `for n in $(gh pr list --state open --limit 1000 --json number --jq '.[].number'); do d=$(gh pr diff $n) || echo "$n: diff unread"; grep -q '^+const IRVersion' <<<"$d" && echo $n; done`.
  After the merge, `TestIRVersion_PinsTheEncodedShape` prints the row to append.

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
- **Probe.** `git grep -nE 'TypeFor\[(ir\.)?TypeID\]' -- '*.go' ':!*_test.go'` lists the readers.
  Add a struct holding a `TypeID` to `ir.Docs`, keep it out of the oracle in `ir/edges_test.go`, and
  watch `TestAppendTypeIDs_ReadsWhatTheTypeGraphReads` fail until `appendTypeIDs` skips the struct.

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
  reflection test that sets every field alone, so the next field added fails there.
- **Why.** A part-encoding emptiness check named each field, so an encoding declaring only the new
  `allowReserved` would have been dropped, with every golden and conformance test green (#1144).
- **Probe.** `git grep -nE '\.<Sibling> (==|!=)|== ir\.<Struct>\{'`, with `<Sibling>` a sibling
  field's name and `<Struct>` the struct's.

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
- **Probe.** `git grep -nE '<field>|<old value>' -- docs`, and list each statement read in the PR
  body. Then take `references/ir-lessons.md#widened-field-consumers`.

## Ask each consumer of a widened field what it did with the old value
`widened-field-consumers`

- **Rule.** When a field's values widen, list each consumer that reads the field and ask what it did
  with the old value, since one may depend on it with no sentence saying so.
- **Why.** A file body's `Content.Type` held only `bytes`, so a plain upload needed no rule and had
  none, and grepping the field and its old values on the request side found nothing (#1191, #1204).
- **Probe.** List in the PR body each consumer asked, and its answer.

## Check a design rule against everything in its document it governs
`design-rule-over-its-document`

- **Rule.** When a change to the IR or emitter design states a general rule, a placement
  argument or a step order, list every field, input or step in the document it applies to and
  check each; check a step order against what each step reads that a later step can still change.
- **Why.** A rule giving derived facts no plan field stood beside a plan field copying one, a
  placement argued for one per-target table and not its twin, and an import step ran before the
  steps renaming what it imports (#1019).
- **Probe.** `grep -n` the document for each field, input and step the rule names, and for their
  neighbours.

## Count what a design assumes about real input before choosing it
`measure-the-premise`

- **Rule.** Before choosing a shape a consumer will use, write the job they do with it and the fact
  about real input it rests on, then count it on the golden spec and the corpus. A program
  running the proposed rules shows only that they are total, since it assumes that fact; a worked
  example, chosen because it fits, shows nothing.
- **Why.** A design kept one error type per body, as a worked example had it, and a run placed every
  failure. Most operations with several error bodies used one type for all, so status told failures
  apart, which nobody counted until a reviewer's question led there (#1167).
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

- **Rule.** A predicate over text (cased, blank, invisible) must be tested against the producer's
  exact transformation and with one witness per Unicode class it claims (Cc, Cf, default-ignorable,
  Zs, caseless letters), not a hand-picked sample. A change that redefines one rechecks it at each
  call site, since two rules sharing it can ask different questions.
- **Why.** unicode.IsUpper flagged neutral caseless runes as cased, and TrimSpace let zero-width and
  filler-only aliases pass as non-blank (#361). A definition of "upper" settled for one
  case-boundary rule was applied to both, and the second went unverified until a mutation sweep
  (#189).
- **Probe.** Generate one witness per class and per producer output and assert the predicate's
  verdict on each, at each call site.

## Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point`

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not
  allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types
  became non-nil and the round trip never settled (#264).
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode
  again, and cmp.Diff the two decoded values.
