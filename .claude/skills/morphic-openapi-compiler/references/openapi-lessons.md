# OpenAPI compiler lessons

Parser-library behaviours are not here: each is an entry in `docs/parser-library.md` with its
canary. A behaviour found that the page lacks is added there.

## Find every reader of a construct before changing how it is read
`every-reader`

- **Rule.** Before changing which `allOf` branches compose, how deep a walk goes, or how a keyword
  is read, find every package that reads the same keyword through its own walk (`GetAllOf()`
  outside `compilers/openapi/internal/schema`) and make each agree in predicate and in bound.
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

- **Rule.** Call the library's functions for every reading it already implements (reference
  parsing, anchors, URI resolution) and `nodeview.View` for tree reads. Key a per-node map by a set
  when an alias can make one node several schemas. A guard that must agree with the resolver makes
  the call the resolver makes.
- **Why.** Each miss of a reach check was a re-derived reading (#563). `View` and the resolver
  disagree on a key written twice (#763).
- **Probe.** Generate documents with a vocabulary of spellings (percent-encoding, a second `#`,
  `.` and `..`, aliases, `<<`) and run each through the raw resolver in a bounded subprocess.

## A mirror of a library read carries no fixed depth cap
`mirror-no-cap`

- **Rule.** When a mirror walks what the library recurses through, bound it by the read's counted
  work, never by a constant.
- **Why.** A cap on merged mappings left references unresolved that `main` resolved (#774).

## Moving work out of the library moves its duties
`library-duties`

- **Rule.** When a change replaces a library lookup, rebuild what the library did for free:
  reporting each failed reference, caching (the cost stays linear), decode fidelity, meta-schema
  checks. Compare error sets with `main` on generated documents, deep and wide.
- **Why.** #566 lost errors `main` gave and went cubic in depth. #1046 dropped the only report of
  values the decoder cannot hold.

## Library calls run before the escape restore
`escape-window`

- **Rule.** Load respells `+` in reference fragments for the library and restores the trees after.
  A new library call over models or trees runs inside that window; pin it with a `+`-spelled
  reference.
- **Why.** Moving validation after the restore regressed messages (#1016).

## One report per malformed construct, from the grammar
`grammar-driven-report`

- **Rule.** Before adding a lowering diagnostic for a malformed construct, probe whether load
  already reports every instance. A report emitted by each reader misses positions no reader
  reaches.
- **Why.** Per-reader reports of a `null` schema missed positions load already reported (#1066).

## A new site under an existing code changes its contract
`diag-code-sites`

- **Rule.** When a change emits an existing diagnostic code from a new site, grep every site of
  the code and re-read the code's doc in `compilers/openapi/internal/diag`.
- **Why.** A code's doc missed two budgets that reported it (#774).

## The specification's text decides validity, not its schemas
`spec-text`

- **Rule.** Before deciding a value is valid for a version, read that version's text: fixed fields,
  MUSTs, ABNF grammars, and the RFC grammars it cites. Diff the requirements across versions.
- **Why.** The published schemas admit an empty server URL and an empty path parameter name that
  the text refuses (#884).

## A two-run difference shows where versions disagree, not why
`two-run-diff`

- **Rule.** When findings are dropped by validating at two versions, diff the two meta-schemas
  first and scope the drop by finding class. Pin it with a counterfactual test.
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

## A heuristic shared with another reader becomes a fact
`heuristic-is-not-knowledge`

- **Rule.** When one predicate serves two readers, ask of each return value whether it is
  knowledge or a guess, and generate inputs at every position the guess covers.
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

- **Rule.** A file `$ref` names its document as written, with dot segments removed (RFC 3986
  §5.2). The compiler never asks the file system which file it is.
- **Why.** Decided in #781 for #772 and #780; file-system identity made a compile depend on disk
  state.

## New bounds warn and continue
`bounds-warn`

- **Rule.** A new bound in the load scan follows the existing ones: a warning ending "protection is
  incomplete for this source", and the compile proceeds. That is safe only when the work is linear
  in the document and every step is charged.
- **Why.** Making one an error refusal broke a test two packages away (#563).

## A conformance case must survive every reader of the corpus
`conformance-case`

- **Rule.** A spec under `testdata/conformance/openapi/` has no YAML aliases (use inline `<<`), so
  the order oracle can permute it; gives an unknown path-item key a mapping value; and is named by
  a rule's `Cases:` line. A value the library refuses adds rows to the validation oracle's
  disagreements: run `TestValidationOracle_AgreesWithTheLibrary` with `-update`, then classify each
  row with a reason.
- **Why.** One case failed all three checks on its first run (#1106); a numeric case added rows
  (#1113).
- **Probe.** `go test ./internal/harness ./compilers/openapi/...`.

## Ask the resolver directly
`resolver-probe`

- **Rule.** To learn what the resolver does with a shape, build the harness test binary and run
  `TestResolverOracle_ProbeHelper` with `MORPHIC_RESOLVER_PROBE` set to the spec. Bound it with a
  watchdog that kills; a Go binary ignores `SIGALRM`.

## Trees that reach the lowering are acyclic
`acyclic-trees`

- **Rule.** Every YAML tree that reaches the lowering through load has passed the scan's
  anchor-cycle refusal, so a reader that follows aliases and `<<` may rely on termination. A
  mirror's own cycle check is defence only, and its test builds the cycle by hand.

## Probe the external-document reader by counting fetches
`external-reader-probes`

- **Rule.** For a change to how external documents are read:
  - count fetches per refused document with an `httptest` server that counts hits, because the
    resolver asks again until it builds a model, so a refusal must be remembered;
  - try `math.MaxInt` wherever a public limit gets `+1`;
  - a skip on re-read compares the bytes read, not the key they were read under.
- **Why.** 50 references fetched a refused document 50 times; `io.LimitReader(r, limit+1)`
  overflowed at `MaxInt`; a key-based skip let changed bytes through unjudged (#556).
