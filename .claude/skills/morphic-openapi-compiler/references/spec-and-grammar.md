# Spec, grammar and verifier semantics

Each rule below is a class of defect that past PRs fixed after their first push. "Seen in"
quotes the PR history at the commit named.

Read the specification, the library and the grammar as they are, at the version pinned; verifiers must reach every class they claim to check.

## Give every field a check that rejects its zero, empty and malformed values
`verifier-rejects-every-degenerate-value`

- **Rule.** For each field a consumer reads or routes on, the verifier must have a production read site that rejects its empty, null, absent, mismatched-kind and malformed values, read from the unfiltered source and mirroring every refusal the encoder makes.
- **Why.** Empty keys and reasons, an absent irVersion, unread Aliases, digitless numbers and ill-formed UTF-8 all verified clean, so a verified document could still fail to encode or misroute.
- **Probe.** For each field of the struct under check, write the zero, empty, null and wrong-kind variants and assert each yields a violation; encode every document that verifies clean.
- **Seen in #329**: “and `pass.Validate` with zero violations and zero diagnostics”
- **Seen in #361**: “`Hint` are read by `checkNaming`; `Aliases` was read by nothing anywhere in the”

## Key static source checks on every syntactic form, ideally the resolved type
`source-scanners-match-every-form`

- **Rule.** An AST or grep check over Go source must match every form the language and codebase use: value and pointer receivers, method values, functions passed as values, aliases, every selector base, go statements and .Go calls, every write form and every constructor of an identity type.
- **Why.** Recursion and dependency pins went blind to value receivers and method values, and a grep audit reported clean while missing both planted writes.
- **Probe.** Plant one instance of each form the check must catch in a scratch package and confirm each reddens; prefer keying on go/types over selector text.
- **Seen in #332**: “A grep-based version of the same audit reported the same clean result and missed”
- **Seen in #837** (f9862aa6): “declaredFields skipped every alias, so a field declared inside”

## Check every reference position and every ID class, not a subset
`verifier-reaches-every-reference-and-id-class`

- **Rule.** A validator or verifier over references or IDs must cover every position a reference can occupy (map keys, PropIDs, plain int indices) and every ID class and node class the document holds, derived from the type graph rather than from the data a fixture happens to contain.
- **Why.** Dangling rename targets, channel, message and PropID references, and malformed operation IDs verified clean because the walk saw only TypeIDs or only registry-reached nodes.
- **Probe.** Plant a dangling or malformed value in each reference-bearing field and each ID class in turn and assert a violation fires for each; a field whose plant stays green is uncovered.
- **Seen in #149** (50cbf651): “reported by nothing: irverify returned no violations and”
- **Seen in #119** (61e34370): “could every one name an entry that does not exist and still pass”

## When a model mirrors a library's read, match its duplicate-key, alias, cap and token rules exactly
`mirror-library-read-rules-exactly`

- **Rule.** A pre-scan, index or stepped reader standing in for a library read must reproduce that library's rules: last-key-wins on duplicates, following YAML aliases and merge keys, no cap the library lacks, by-value model fields only, and leaving retried index tokens to the whole read. Build a differential case for every token kind the library treats specially.
- **Why.** First-key-wins pre-scans let a redeclared self-reference reach the resolver's stack overflow, an anchor index missed alias-reached declarations, and a stepped walk missed references a whole read reaches, while an invented cap made valid deep specs exit 1.
- **Probe.** For each special case (duplicate key in both positions, alias, merge key, retried index, by-value struct, deep chain past your cap) diff the mirror's answer against the library's whole read on a fixture, and confirm the fixture actually produces that token kind.
- **Seen in #95** (40e741e9): “The cycle pre-scan read a mapping that declares the same key twice as first-key-wins.”
- **Seen in #356** (d359ca9c): “RawChildNode returned the first pair spelling a key where the parser reads”

## Validate a fixed-shape token at every character and its escape-of-escape, and dispatch keyword forms on the dialect
`exact-token-grammar`

- **Rule.** When validating a fixed-shape token, check its length and every character position; when splitting on an escapable delimiter, honour the escape character's own escaping; when a keyword's form depends on the dialect, dispatch on the dialect and report the other dialect's form rather than accepting it.
- **Also.** For every derived identifier or canonical name with a grammar, irverify must reject any value the grammar cannot produce, covering every property (casing, character set, segmentation, agreement with the recorded coordinate), with one rejecting case per property.
- **Why.** A wildcard status check read two of three characters and admitted 1XY and 9XX, a backslash-escaped backslash before a pipe merged two cells, and a boolean exclusiveMinimum under 2020-12 was silently accepted as a degenerate bound.
- **Probe.** Write rejected spellings for each length and each character position, a cell containing \\|, and each bound keyword in the other dialect's form; compile and confirm each is reported, not lowered.
- **Seen in #263**: “The parse was loose in the other direction as well. The wildcard test read the first”
- **Seen in #23** (32c99973): “now reported and dropped rather than silently accepted as a degenerate”

## Identify and measure code with the parser and type checker, never by regex or constructed name strings
`resolve-symbols-semantically`

- **Rule.** Match functions and packages by types.Object identity and the file's own import declarations (including aliases), and measure structure (call cycles, method counts) with a parser, cross-checked by a second method.
- **Why.** Name-string matching missed aliased imports and flagged unrelated packages, and a regex roughly doubled a mutual-recursion count and sized a plan on it.
- **Probe.** Add an aliased import and an unrelated same-named package to the guard's fixtures; recompute any reported metric with go/ast or go/types and compare.
- **Seen in #801** (6c71a672): “of data matched the conventional names ir and compile. A file importing”
- **Seen in #203**: “I first measured the core with a regex over the source and reported 57 mutually”

## Witness each character class a text predicate decides
`character-predicate-witness-per-class`

- **Rule.** A predicate over text (cased, blank, invisible) must be tested against the producer's exact transformation and with one witness per Unicode class it claims (Cc, Cf, default-ignorable, Zs, caseless letters), not a hand-picked sample.
- **Why.** unicode.IsUpper flagged neutral caseless runes as cased, and TrimSpace let zero-width and filler-only aliases pass as non-blank.
- **Probe.** Generate one witness per class and per producer output and assert the predicate's verdict on each.
- **Seen in #361** (d1c19a8f): “The blank rule was strings.TrimSpace, which reports false for the”
- **Seen in #361** (b75eec57): “isBlankName missed the default-ignorable code points, so an alias of”

## Make audit matches exact, and plant a sibling or same-named path to prove each rule reaches
`archtest-rules-reach-exactly`

- **Rule.** An allowlist or skip rule must match full paths exactly unless a wildcard is written, and every in-module import -- including testdata fixtures under an audited tree -- is part of the graph the rules must cover.
- **Why.** A segment-prefix match let a planted sibling of compilers/openapi pass under its allowlist entry, a basename skip left nested irtest/irverify directories audited by nothing, and a testdata fixture's sibling import was missing from the allowlist.
- **Probe.** Plant a forbidden import from a package sharing the allowed prefix, a same-named directory at another depth, and a testdata fixture importing a sibling; run go test ./internal/archtest and confirm each reddens.
- **Seen in #182** (2de404a1): “An allowlist entry matched as a segment prefix, and compilers/openapi is allowed”
- **Seen in #182** (2de404a1): “The skip decision for a subdirectory carrying its own rule compared basenames”

## Re-validate text after percent or escape decoding before it becomes an identifier
`revalidate-after-decoding`

- **Rule.** Decoded untrusted text must pass the same validity check as the raw input, plus the target format's grammar (UTF-8 for a JSON Pointer), before use as a key or identifier.
- **Why.** A percent-encoded %FF in a $ref fragment reached an IR identifier the encoder cannot write.
- **Probe.** Compile a spec with `#/components/schemas/%FF` and similar decoded-invalid bytes; read the full diagnostic list and confirm the document still round-trips through JSON.
- **Seen in #541** (82454bc8): “`#/components/schemas/%FF` became a pointer holding the byte 0xFF”
- **Seen in #515** (66d2f196): “#520: a percent-encoded non-UTF-8 byte in a `$ref` fragment reaches an IR identifier.”

## Make every custom JSON decoder reach a fixed point on null, whitespace and empty forms
`json-decoder-fixed-point`

- **Rule.** A custom decoder must detect JSON literals by decoded value, not raw bytes, and must not allocate on null; decode-encode-decode of null, padded null and the empty form must be stable.
- **Why.** TypeRegistry allocated an empty map on null, and again on " null ", so Document.Types became non-nil and the round trip never settled.
- **Probe.** For each UnmarshalJSONFrom touched, decode "null", " null ", "{}", re-encode, decode again, and cmp.Diff the two decoded values.
- **Seen in #264**: “`TypeRegistry.UnmarshalJSON` unconditionally allocated and assigned a map even when the JSON input was the literal `null`”
- **Seen in #264** (009429dc): “TypeRegistry.UnmarshalJSON(" null ") still built and assigned an empty map,”

