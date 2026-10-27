# Checks that reach what they claim

Verifiers, lint and archtest rules, codecs and loaders. A check is evidence only for the forms it
can see, so each rule here is about widening what a check sees or proving how far it reaches.

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

## Key static source checks on every syntactic form, ideally the resolved type
`source-scanners-match-every-form`

- **Rule.** An AST or grep check over Go source must match every form the language and codebase use:
  value and pointer receivers, method values, functions passed as values, aliases, every selector
  base, go statements and .Go calls, every write form and every constructor of an identity type.
- **Why.** Recursion and dependency pins went blind to value receivers and method values, and a grep
  audit reported clean while missing both planted writes (#332, #837).
- **Probe.** Plant one instance of each form the check must catch in a scratch package and confirm
  each reddens; prefer keying on go/types over selector text.

## Check every reference position and every ID class, not a subset
`verifier-reaches-every-reference-and-id-class`

- **Rule.** A validator or verifier over references or IDs must cover every position a reference can
  occupy (map keys, PropIDs, plain int indices) and every ID class and node class the document
  holds, derived from the type graph rather than from the data a fixture happens to contain.
- **Why.** Dangling rename targets, channel, message and PropID references, and malformed operation
  IDs verified clean because the walk saw only TypeIDs or only registry-reached nodes (#149, #119).
- **Probe.** Plant a dangling or malformed value in each reference-bearing field and each ID class
  in turn and assert a violation fires for each; a field whose plant stays green is uncovered.

## Identify code with the type checker, never by name strings
`resolve-symbols-semantically`

- **Rule.** Match functions and packages by types.Object identity and the file's own import
  declarations (including aliases), and measure structure (call cycles, method counts) with a
  parser, cross-checked by a second method.
- **Why.** Name-string matching missed aliased imports and flagged unrelated packages, and a regex
  roughly doubled a mutual-recursion count and sized a plan on it (#801, #203).
- **Probe.** Add an aliased import and an unrelated same-named package to the guard's fixtures;
  recompute any reported metric with go/ast or go/types and compare.

## Match audit paths exactly, and plant a sibling to prove it
`archtest-rules-reach-exactly`

- **Rule.** An allowlist or skip rule must match full paths exactly unless a wildcard is written,
  and every in-module import -- including testdata fixtures under an audited tree -- is part of the
  graph the rules must cover.
- **Why.** A segment-prefix match let a planted sibling of compilers/openapi pass under its
  allowlist entry, a basename skip left nested irtest/irverify directories audited by nothing, and a
  testdata fixture's sibling import was missing from the allowlist (#182).
- **Probe.** Plant a forbidden import from a package sharing the allowed prefix, a same-named
  directory at another depth, and a testdata fixture importing a sibling; run go test
  ./internal/archtest and confirm each reddens.

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

## Fail when a loader or parser reads less than it was asked to
`loaders-fail-when-they-read-less`

- **Rule.** A source loader, package loader or document parser errors when its result is smaller
  than its input: a pattern matching nothing, a build-excluded file, a test-only package, a line it
  cannot place. A reader of a format reads every spelling the format admits, such as tilde and
  backtick fences and tables without outer pipes in Markdown.
- **Why.** Checkers reported clean on code they never saw, and the matrix reader dropped every row
  below an unexpected line (#837, #342).
- **Probe.** Feed the loader a pattern matching nothing, a build-tagged file, a test-only package
  and a malformed line mid-table, and assert each errors rather than returning less.

## Validate a source-reading checker against history
`history-sweep`

- **Rule.** A new check that reads the source (a shape description, a declaration scan) is run,
  built from the branch, at each past commit that changed what it reads: its working directory in a
  detached worktree at that commit. It must change exactly where history did.
- **Why.** The IR shape describer changed at every version bump and at no refactor, and found one
  unbumped shape change (#985).
