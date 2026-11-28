# Verifier and codec lessons

What `irverify`, text predicates and custom JSON decoders must reach and reject.

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
  occupy (map keys, PropertyIDs, plain int indices) and every ID class and node class the document
  holds, derived from the type graph rather than from the data a fixture happens to contain.
- **Why.** Dangling rename targets, channel, message and PropertyID references, and malformed
  operation IDs verified clean because the walk saw only TypeIDs or only registry-reached nodes
  (#149, #119).
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
  `references/verifier-and-codecs.md#witness-past-the-bmp` and
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

## Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point`

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not
  allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types
  became non-nil and the round trip never settled (#264).
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode
  again, and cmp.Diff the two decoded values.
