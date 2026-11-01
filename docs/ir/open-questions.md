# Open questions

Part of the [IR design](./README.md). Tracked for implementation.

1. **BigVal representation** — settled: a decimal string in the IR (`BigVal`), read by
   `ir.Decimal` without materializing it ([values](./values.md)).
2. **Dedup aliasing mechanics** — alias table on Document vs ID rewriting with a redirect map;
   leaning alias table (IDs immutable, always).
3. **Content negotiation in the plan layer** — settled: the IR keeps all media types, and one
   shared plan helper elects among them for a success, a failure and a stream alike, by a ranking
   the plan's policy holds ([one election](../emitters/plan/responses.md#one-election)).
4. **Whether `UsageFlags` is stored or always recomputed** — settled: derived by `ir/irfacts`,
   not stored ([derived-facts](./derived-facts.md)). On the golden spec (`cloudflare/api-schemas` at
   `843eae2f`) deriving usage for every type, the subtype index and the type graph included, takes
   about 50 ms of a compile of about 8 s (morphic `4545c23e`), so storing it bought nothing, and a
   stored copy goes stale after any transform that changes what an operation reaches.
5. **Dual identity for cross-revision correlation** — pointer-derived `TypeID`s are stable across
   runs but churn when a schema moves within its source file. TCGC solves cross-version
   correlation with name-based `crossLanguageDefinitionId`s. Likely resolution: keep pointer IDs
   as the structural identity and add a derived, name-based correlation key computed by a pass —
   decide once IR diffing between spec revisions is actually built. Evidence from oagen's
   differ/compat stack: (a) diffing IR-document-to-IR-document is the right plane (oagen already
   does this); (b) name-based correlation alone still misses renames — plan a third *structural*
   tier (content-hash / field-set similarity, shared with the `dedup` pass) as the fallback when
   both pointer and name churn. Without stable identity, rename detection costs hundreds of
   lines of best-effort heuristics downstream (see prior-art.md §1).
6. **Response discrimination placement** — `ResponseConditions` (status ranges) is HTTP-shaped
   living on the neutral core. Fresh evidence that it sits awkwardly: RPC single-response and OTP
   call replies both use empty conditions, and `Response.StatusCodeProp` is likewise
   HTTP-flavored. Alternatives: move conditions into `HTTPBinding` alongside parameter locations,
   leaving responses as an ordered named list. Revisit when the RPC or messaging compiler lands
   and either validates or strains the current shape.
7. **Semantic nullability (GraphQL)** — the `@semanticNonNull` RFC ("null only on error") is a
   third nullability state `TypeRef.Nullable` cannot express. Pre-spec today: preserved as
   `Unmodeled["graphql:@semanticNonNull"]`. If it merges into the spec, add a `NullKind` (or a
   `NullOnlyOnError` bool) rather than churning every compiler early.
