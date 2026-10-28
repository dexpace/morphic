# OpenAPI compiler lessons

Parser-library behaviours are not here: each is an entry in `docs/parser-library.md` with its
canary. A behaviour found that the page lacks is added there.

## Find every reader of a construct before changing how it is read
`every-reader`

- **Rule.** Before changing which `allOf` branches compose, how deep a walk goes, or how a keyword
  is read, find every package that reads the same keyword through its own walk (`GetAllOf()` outside
  `compilers/openapi/internal/schema`) and make each agree in predicate and in bound.
- **Why.** The multipart part reader in `compilers/openapi/internal/operation` walks `allOf` with
  its own bound and drifted from the schema lowering, dropping an `encoding` silently (#998).
- **Probe.** A multipart body with an `encoding` entry for a part inside the changed shape, on
  `main` and on the branch.

## Enumerate a source object's fields against their readers
`fields-vs-readers`

- **Rule.** For losslessness, list each source struct's fields from the library's source and grep
  for a reader of each. For a field with none, compile a minimal spec and read the IR and every
  diagnostic. A field can also be read from its YAML node rather than its accessor, so no accessor
  reader is a candidate, not a verdict.
- **Why.** This found four drops a careful read of the diff missed (#272; #291, #292, #293).

## Call the library's reading; never re-derive it
`reuse-library-readings`

- **Rule.** Call the library's functions for every reading it already implements (reference parsing,
  anchors, URI resolution) and `nodeview.View` for tree reads. Key a per-node map by a set when an
  alias can make one node several schemas. A guard that must agree with the resolver makes the call
  the resolver makes.
- **Why.** Each miss of a reach check was a re-derived reading (#563). `View` and the resolver
  disagree on a key written twice (#763, `docs/parser-library.md#duplicate-key`).
- **Probe.** Generate documents with a vocabulary of spellings (percent-encoding, a second `#`, `.`
  and `..`, aliases, `<<`) and run each through the raw resolver in a bounded subprocess.

## Moving work out of the library moves its duties
`library-duties`

- **Rule.** When a change replaces a library lookup, rebuild what the library did for free:
  reporting each failed reference, caching (the cost stays linear), decode fidelity, meta-schema
  checks. Compare error sets with `main` on generated documents, deep and wide.
- **Why.** #566 lost errors `main` gave and went cubic in depth. #1046 dropped the only report of
  values the decoder cannot hold.

## Library calls run before the escape restore
`escape-window`

- **Rule.** Load respells `+` in reference fragments for the library and restores the trees after. A
  new library call over models or trees runs inside that window; pin it with a `+`-spelled
  reference.
- **Why.** Moving validation after the restore regressed messages (#1016).

## A new site under an existing code changes its contract
`diag-code-sites`

- **Rule.** When a change emits an existing diagnostic code from a new site, grep every site of the
  code and re-read the code's doc in `compilers/openapi/internal/diag`.
- **Why.** A code's doc missed two budgets that reported it (#774).

## The specification's text decides validity, not its schemas
`spec-text`

- **Rule.** Before deciding a value is valid for a version, read that version's text: fixed fields,
  MUSTs, ABNF grammars, and the RFC grammars it cites. Diff the requirements across versions.
- **Why.** The published schemas admit an empty server URL and an empty path parameter name that the
  text refuses (#884).

## A two-run difference shows where versions disagree, not why
`two-run-diff`

- **Rule.** When findings are dropped by validating at two versions, diff the two meta-schemas first
  and scope the drop by finding class. Pin it with a counterfactual test.
- **Why.** One 3.2 keyword dropped every other schema's missing-`propertyName` finding (#870).

## "The validator already reports it" holds only for what it walks
`validator-scope`

- **Rule.** When an IR invariant leans on a validator finding, probe every spelling (`$ref`,
  path-level and operation-level, a path item mounted twice) through the harness, `validate` and
  `compile --skip-validate`, against `main`.
- **Why.** The duplicate-parameter check fired for inline entries only (#811).

## Spec validity is not the IR's range
`validity-vs-range`

- **Rule.** When a reader moves onto an IR type with a bounded domain, tell "invalid source" from
  "unrepresentable" at each caller.
- **Why.** A valid but huge `maxLength` drew "must be a non-negative integer" (#1113).
- **Probe.** Compile a valid value just past the IR type's domain; its diagnostic names the limit
  the IR cannot hold, never the source as invalid.

## A heuristic shared with another reader becomes a fact
`heuristic-is-not-knowledge`

- **Rule.** When one predicate serves two readers, ask of each return value whether it is knowledge
  or a guess, and generate inputs at every position the guess covers.
- **Why.** A skip-by-name guess, read as "raw YAML" by a second reader, let documents overflow the
  stack (#946).

## IDs and labels must not depend on how the spec is invoked
`invocation-axis`

- **Rule.** For any ID, label or path rule, compile by a relative path, an absolute path and from a
  working directory reached through a symlink, and compare. Build two operations that share one
  referenced body on purpose.
- **Why.** Both shapes survived every corpus sweep (#1009).

## A file reference's identity is lexical
`file-ref-lexical`

- **Rule.** Identify the document a file `$ref` names by its spelling with dot segments removed
  (RFC 3986 §5.2), as `docs/compilers/openapi.md#references` judges each `$ref` by its own
  spelling. Never ask the file system which file it is.
- **Why.** Keying on the raw spelling read one file as several documents and held the source once
  per spelling (#780, #772); #781 removed dot segments rather than ask the file system, whose answer
  makes a compile depend on disk state.
- **Probe.** Reference one file as `./a.yaml` and as `./x/../a.yaml`; the document's `sources`
  lists it once.

## Take a new bound's outcome from the rule entry for its kind
`bound-outcome-from-the-rules`

- **Rule.** A new bound in load or binding takes the outcome, refusal or warning, that
  `docs/compilers/openapi.md#source-budgets` or `docs/compilers/openapi.md#references` gives a
  bound of its kind, and joins that entry in the same change. A bound that stops a walk is
  reported; a cut result never reads as a clean one.
- **Why.** Outcomes moved with the code: the reference-chain bounds that warned and let the compile
  go on are gone (#1016), nesting past the scan's bound is refused (#905), and changing one bound
  against its siblings broke a test two packages away (#563).
- **Probe.** Compile a document at the bound and one past it; read each diagnostic's code, severity
  and pointer, and whether a Document came back.

## A conformance case must survive every reader of the corpus
`conformance-case`

- **Rule.** Beyond step 4 of `CONTRIBUTING.md`'s lowering-rule recipe, a spec under
  `testdata/conformance/openapi/` has no YAML aliases (use inline `<<`), so the order oracle can
  permute it, and gives an unknown path-item key a mapping value with `responses`, an operation as
  `docs/parser-library.md#embedded-map-keys` reads it. A value the library refuses adds rows to the
  validation oracle's disagreements: run
  `go test ./compilers/openapi/internal/canary -run TestValidationOracle_AgreesWithTheLibrary -update`,
  then classify each row with a reason.
- **Why.** One case failed `TestOrderInvariant_ReachesTheCorpus`, the validation oracle and
  `TestLoweringSpec_CasesAreTheCorpus` on its first run (#1106); a numeric case added rows (#1113).
- **Probe.** `go test ./internal/harness ./compilers/openapi/...`.

## Ask the resolver directly
`resolver-probe`

- **Rule.** To learn what the resolver does with a shape, build the harness test binary and run
  `TestResolverOracle_ProbeHelper` with `MORPHIC_RESOLVER_PROBE` set to the spec. Bound it with a
  watchdog that kills; a Go binary ignores `SIGALRM`.
- **Why.** The scan's comments said a re-entrant prefix deadlocks the resolver, and the probe showed
  within minutes that it does not; a runaway compile then outlived a `perl` alarm, since Go ignores
  `SIGALRM` (#541, #574, #591).

## Trees that reach the lowering are acyclic
`acyclic-trees`

- **Rule.** A reader that follows aliases and `<<` in a tree that came through load may rely on
  termination: load refuses an anchor cycle in every tree it hands the lowering, before anything
  recursive runs (`docs/parser-library.md#alias-cycle`). A mirror's own cycle check is defence only, and its test
  builds the cycle by hand.
- **Why.** Through load a mirror's cycle check is unreachable, so a test that feeds it a document
  never runs it (#563).
- **Probe.** Compile `x-a: &a [*a]` and see load refuse it as `openapi/cyclic-ref`; delete the
  mirror's check and its hand-built cycle test reddens.

## Probe the external-document reader by counting fetches
`external-reader-probes`

- **Rule.** For a change to how external documents are read, count fetches per refused document with
  an `httptest` server that counts hits, since a refusal not remembered is fetched again for every
  reference to its document; try `math.MaxInt` wherever a public limit gets `+1`; and make a
  skip on re-read compare the bytes read, not the key they were read under.
- **Why.** References to a refused document each fetched it again, `io.LimitReader(r, limit+1)`
  overflowed at `MaxInt`, and a key-based skip let changed bytes through unjudged (#556).

## Match a library read's rules exactly when mirroring it
`mirror-library-read-rules-exactly`

- **Rule.** A pre-scan, index or stepped reader mirroring a library read follows aliases, and takes
  a key written twice and a merge key's pairs as `docs/parser-library.md#duplicate-key` and
  `docs/parser-library.md#merge-keys` give them for the read it mirrors, the model's or raw YAML's.
  It adds no cap the library lacks: the read's counted work bounds it, never a constant, and
  retried index tokens are left to the whole read. Build a differential case per token kind the
  library treats specially.
- **Why.** A first-key-wins pre-scan let a redeclared self-reference reach the resolver's stack
  overflow, an anchor index missed alias-reached declarations, and an invented cap left references
  unresolved that `main` resolved (#95, #356, #774).
- **Probe.** Diff the mirror's answer against the library's whole read for each special case: a
  duplicate key, an alias, a merge key, a chain past your cap.

## Validate a fixed-shape token at every character, and dispatch on the dialect
`exact-token-grammar`

- **Rule.** When validating a fixed-shape token, check its length and every character position. When
  splitting on an escapable delimiter, honour the escape character's own escaping. When a keyword's
  form depends on the dialect, dispatch on the dialect and report the other dialect's form rather
  than accepting it.
- **Why.** A wildcard status check read two of three characters and admitted `1XY`, an escaped
  backslash before a pipe merged two cells, and a boolean `exclusiveMinimum` under 2020-12 was
  accepted as a degenerate bound (#263, #23).
- **Probe.** Write rejected spellings for each length and character position, a cell holding `\|`,
  and each bound keyword in the other dialect's form.

## Re-validate text after percent or escape decoding before it becomes an identifier
`revalidate-after-decoding`

- **Rule.** Decoded untrusted text must pass the same validity check as the raw input, plus the
  target format's grammar (UTF-8 for a JSON Pointer), before use as a key or identifier.
- **Why.** A percent-encoded %FF in a $ref fragment reached an IR identifier the encoder cannot
  write (#541, #515).
- **Probe.** Compile a spec with `#/components/schemas/%FF` and similar decoded-invalid bytes; read
  the full diagnostic list and confirm the document still round-trips through JSON.

## Probe every raw-YAML reader with alias, anchor, merge-key, tag and quoted spellings
`yaml-alias-anchor-merge-spellings`

- **Rule.** Any code that reads raw YAML nodes or mirrors the parser's rules must resolve aliases,
  anchors, merge keys and tags the way the parser does, and must not treat quoted or aliased
  non-special spellings as special. Prefer the raw node or the library call over a hand-written copy
  of the rule.
- **Why.** Readers matched keys by raw node text or skipped anchor-valued pairs, so aliased,
  anchored or merged entries were dropped with exit 0, while quoted '<<' keys raised a false cycle
  (#500, #356).
- **Probe.** Feed each touched reader a fixture set: key as alias, value as alias, anchored entry,
  '<<' merge, quoted '<<', !!merge tag; diff IR and diagnostics against the plain spelling.

## Make every input route reach the new guard, path or oracle
`every-input-route-reaches-the-guard`

- **Rule.** When a guard, transformation, validation or new branch is applied on one route into a
  consumer, list every other route into it (external $ref documents, overlays, callbacks, direct
  versus behind a $ref, harness oracles that take no options) and confirm each reaches it, or state
  the uncovered route where the next reader will see it.
- **Why.** The anchor release and alias refusals protected only the root document, so overlays and
  externally referenced documents still crashed or lost entries with exit 0, and the harness oracles
  never saw the overlay path (#556, #492).
- **Probe.** Run the same negative fixture through each route (wrap it in an external $ref, apply it
  as an overlay, reach it through a callback) and diff the diagnostics and IR against the direct
  compile.
