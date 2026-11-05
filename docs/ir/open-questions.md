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
5. **Cross-revision correlation** — answered for the two tiers a document alone supports. An ID
   identifies an entity within one document, and across revisions only while its pointer holds
   still; a reordered array even hands an old ID to another entity ([across
   revisions](./identity.md#across-revisions)). The [correlation
   key](./derived-facts.md#correlation-keys) carries an entity across the rest: derived on demand
   rather than stored, built from what the document names the entity by, and none where two entities
   share one. Still open: a third, *structural* tier for an entity whose pointer and name both
   change — a content hash or field-set similarity shared with the `dedup` pass, which oagen's
   compat stack found it needed once name-based correlation missed renames (prior-art.md §1). Decide
   it once a diff consumer shows the first two tiers miss what it must match.
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
