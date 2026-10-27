# IR lessons

What an IR change meets that ir-design does not say: other branches claiming the same version,
hand lists that must name a new field, guarantees a type held without stating them, and what the
verifier and codecs must reach.

## Take the next version when another PR claims yours
`version-collisions`

- **Rule.** Two open PRs that bump `IRVersion` from one base claim the same version, and the
  constant merges silently because both sides wrote the same line; only the `generations` row and
  the ir-design history entry conflict. The PR that merges second keeps `main`'s row and entry,
  moves the constant to the next version, and appends its own row and entry under that version.
- **Why.** #1065 and #1066 both set 0.18.0, and the constant merged with no conflict; #734 and #735
  both bumped to 0.10.0 and conflicted only in ir-design.
- **Probe.** `scripts/irversion_peers.py` before the bump and after each merge of `main`; after the
  merge, `TestIRVersion_PinsTheEncodedShape` prints the row to append.

## Classify every field you add to package ir, walker state included
`classify-ir-fields`

- **Rule.** A struct field anywhere in package `ir`, unexported walker state included, meets the
  classification tests in `ir/irverify`: an integer field needs an `integerFields` entry, an
  ID-typed field an `idPositions` entry, a `[]string` a `stringLists` entry, and no map may key an
  ID to an ID. Shrink walker state before classifying it. Remove a deleted field's entry yourself:
  a stale `integerFields` entry passes, though the other two tables' tests refuse one.
- **Why.** Walker state added for `ir.FlattenedProperties` failed all three tests, and an alias
  memo mapping an ID to an ID was refused (#1141); a pair of reference-key fields failed the
  integer test (#737).
- **Probe.** `go test ./ir/irverify -run 'AreAllClassified|EveryShapeIsOneIdShapeOfReads'`; plant
  an `integerFields` entry for a field that does not exist and watch it pass, then the same in
  `idPositions` and watch it fail.

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
- **Probe.** Build a document holding what the old type could not, and run `irverify` on it.

## Sweep the predicates that name a struct's fields when it grows one
`predicates-that-list-fields`

- **Rule.** When an `ir` struct gains a field, find the code that names its siblings one by one (an
  emptiness test, an equality, a comparison with the zero literal) and give each such predicate a
  reflection test that sets every field alone, so the next field added fails there.
- **Why.** A part-encoding emptiness check named each field, so an encoding declaring only the new
  `allowReserved` would have been dropped, with every golden and conformance test green (#1144).
- **Probe.** `git grep -nE '\.<Sibling> (==|!=)|== ir\.<Struct>\{'`, with `<Sibling>` a sibling
  field's name and `<Struct>` the struct's.

## Check a design rule against everything in its document it governs
`design-rule-over-its-document`

- **Rule.** When a change to ir-design or emitter-design states a general rule, a placement
  argument or a step order, list every field, input or step in the document it applies to and
  check each; check a step order against what each step reads that a later step can still change.
- **Why.** A rule giving derived facts no plan field stood beside a plan field copying one, a
  placement argued for one per-target table and not its twin, and an import step ran before the
  steps renaming what it imports (#1019).
- **Probe.** `grep -n` the document for each field, input and step the rule names, and for their
  neighbours.

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
  Zs, caseless letters), not a hand-picked sample.
- **Why.** unicode.IsUpper flagged neutral caseless runes as cased, and TrimSpace let zero-width and
  filler-only aliases pass as non-blank (#361).
- **Probe.** Generate one witness per class and per producer output and assert the predicate's
  verdict on each.

## Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point`

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not
  allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types
  became non-nil and the round trip never settled (#264).
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode
  again, and cmp.Diff the two decoded values.
