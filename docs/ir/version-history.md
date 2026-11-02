# IR version history

Part of the [IR design](./README.md). The log a bump of [`IRVersion`](./ir-version.md) appends to.

What each generation changed in the JSON shape, and what a consumer pinned to the generation before
it sees. Changes made together in one bump are listed together under it.

## Generations

- **0.2.0** — three shape changes made together. `Extensions` became `Preserved`, with `RawConfig`
  split out. `Content.ItemEncoding` became a single encoding rather than a sentinel-keyed map. The
  diagnostic code `pass/dangling-auth-ref` became `ir/dangling-auth-ref` (the `pass` package doc
  explains the rename).
- **0.3.0** — `Preserved` was renamed `Unmodeled` on every carrier, so the JSON key `preserved`
  became `unmodeled`. A consumer pinned to 0.2.0 finds no key it recognizes and drops every
  unmodeled construct in silence.
- **0.4.0** — six shape changes made together, all of them closing a gap a consumer had to read
  around rather than adding a capability:
  - `ErrorCase` became `Response`'s sibling: `Type` was **removed**, and `Name`, `Payload` and
    `Headers` took its place. A consumer pinned to 0.3.0 finds no `type` on an error case and cannot
    reach its models at all; one that reads the new fields gets the status spelling, the headers and
    every media type, which 0.3.0 dumped into `Unmodeled` whatever their arity.
  - `Payload` gained `Required`. Body optionality stopped being an inverted `Unmodeled` sentinel
    read by absence, so a consumer that still reads `openapi:required` now finds nothing and reads
    every body as required.
  - `Parameter` gained `Provenance`, not `omitempty`, and with it `x-sunset` promotion at the
    parameter position.
  - `Deprecation` gained `RemovalDate`. `x-sunset` promotes into it rather than into
    `RemovalVersion`, so a consumer reading a removal date off the version field now finds it
    empty.
  - `Encoding` gained `Schema`, giving `contentSchema` a home at scalar positions.
  - `Constraints.ExclusiveMin` and `ExclusiveMax` changed from bool to a decimal string carrying the
    bound itself, so the two dialects' exclusive bounds no longer lose one keyword to the other. The
    JSON type of both keys changed; a consumer decoding them as booleans fails rather than degrades.
- **0.5.0** — the IR moved onto `encoding/json/v2` and absence got one spelling:
  - `Operation.Auth`, `Service.Auth` and `Server.Auth` write nil (inherit) as an absent key rather
    than `null`. An empty list, explicitly public, is still `[]`.
  - A `Value`'s bytes, list and object payloads, and a `CtorValue`'s args, are omitted when empty
    rather than written as `null`.
  - Strings use RFC 8785's minimal escaping, so `<`, `>` and `&` are written as themselves rather
    than as `\u003c`, `\u003e` and `\u0026`.
  - Decoding refuses what it used to take in silence: a member the schema does not define, a
    duplicate name, a string that is not UTF-8, and a missing or foreign `irVersion`, which is read
    before any other member.
- **0.6.0** — each kind of `Provenance` locator got its own key. `pointer` holds only an RFC 6901
  pointer; a line and column moved to `position`, and an IR pass's location in the document itself
  moved to `node`. A consumer pinned to 0.5.0 knows neither new key and reads those findings as
  unlocated.
- **0.7.0** — two additions made together:
  - `OperationGroup` gained `ID`. A group had been a name and a list of operations, so a consumer
    could key a sub-client only by its name, which two groups can share: the declared tag `default`
    and the group untagged operations fall into render from the same words. A consumer pinned to
    0.6.0 finds no `id` on a group and has nothing else to key it by.
  - `Document` gained `IDSpaces`, the namespaces its IDs live in, by kind prefix. Without it nothing
    could see an ID that lost the separator before its path in an operation, a service or a group,
    which record no pointer to agree with. A consumer pinned to 0.6.0 finds no `idSpaces` and
    cannot tell which namespaces a producer minted in.
- **0.8.0** — parameters and enum members gained an identity:
  - `Parameter` gained `ID`. A parameter had been named by its name, which one operation can repeat:
    `id` in the path and `id` in the query are two parameters. `HTTPParamBinding.Param`,
    `ParamPath.Param` and `Idempotency.TokenParam` changed from the parameter's name to its
    `ParamID`, so the three references name one parameter instead of two. A consumer pinned to 0.7.0
    finds no `id` on a parameter and reads those three as names; it cannot tell which of two
    same-named parameters a binding places on the wire. The `param` kind joined `IDSpaces`.
  - `EnumMember` gained `ID`. A member had been a name and a value, so a consumer could key it only
    by a name that two members can render alike (`a-b` and `a_b` are the same words) or by its
    position, which reordering changes. A consumer pinned to 0.7.0 finds no `id` on a member and
    has nothing else to key it by. The `e` kind joined `IDSpaces`.
- **0.9.0** — three additions, and one member that started being filled:
  - `Payload` gained `Docs`, a body's own documentation as against any one media type's. A consumer
    pinned to 0.8.0 finds no `docs` on a request body and drops its description.
  - `TagDef` gained `Parent` and `Kind`, OpenAPI 3.2's tag hierarchy and tag role, as declared. A
    consumer pinned to 0.8.0 reads every tag as top-level and of no kind.
  - The OpenAPI compiler now nests a tag's group under its declared parent's in
    `OperationGroup.Groups`, which no OpenAPI document filled before. The shape is unchanged, but a
    consumer that read only the top level misses every operation of a nested group.
- **0.10.0** — a tuple states what may follow its elements, and its bounds:
  - `Tuple` gained `Rest`, the type of every element past `Elems`, nil when none may follow. The
    OpenAPI compiler had lowered `prefixItems` alone and kept a trailing `items` verbatim under
    `Unmodeled["openapi:items-after-prefix"]`, so a tuple writing no `items`, which JSON Schema
    leaves open, read as closed, and one writing `items: false` was reported as open. That entry is
    no longer written. A consumer pinned to 0.9.0 finds no `rest` and reads every tuple as closed.
  - `Tuple` gained `Constraints`, the field `List` already had, so `minItems`, `maxItems` and
    `uniqueItems` beside `prefixItems` reach it rather than `Unmodeled`, where a consumer pinned to
    0.9.0 looks for them.
- **0.11.0** — `HTTPBinding` gained `WebhookName`, the OpenAPI `webhooks`-map key that declared a
  webhook operation, and a webhook binding stopped writing `uriTemplate`. The key names an event,
  not an RFC 6570 template, so a `{tenant}` in it read as a variable nothing binds. A consumer
  pinned to 0.10.0 finds a webhook's name in no key it reads.
- **0.12.0** — `SourceInfo` gained `SelfURI`, the URI a document declared for itself (OpenAPI 3.2
  `$self`), which the OpenAPI compiler had dropped. A consumer pinned to 0.11.0 finds no `selfURI`
  and cannot tell what URI a document gave itself.
- **0.13.0** — changes to auth ([auth](./auth.md)) and to what a provenance position means
  ([provenance](./provenance.md)):
  - `OAuthFlow.Scopes` became a list of `{name, description}` in source order, where it was a map
    from name to description that sorted them. A consumer pinned to 0.12.0 expects an object and
    cannot decode the list.
  - `OAuthFlow` gained `TokenEndpointAuthMethods`, how a client authenticates to the token
    endpoint, and `AuthScheme` gained `Credentials`, a declared credential type. A consumer pinned
    to 0.12.0 finds neither.
  - A `Position` no longer marks a finding that has no pointer: one made on a raw node carries the
    node's `Pointer` beside its line and column. A consumer that read a position as "no pointer"
    now finds both.
- **0.14.0** — a map has one shape ([containers](./containers.md)):
  - The OpenAPI compiler lowers an object whose one member fact is its `additionalProperties` to a
    `MapT`, where it had built a `Model` with no properties and an `AdditionalProps`. A consumer
    pinned to 0.13.0 finds a node of kind `map` where it read a model.
  - `MapT.Key` became a pointer, nil for string keys as `AdditionalProps.Key` is, and `MapT` gained
    `Constraints`, the bound on how many entries an instance may carry. A consumer pinned to 0.13.0
    decodes a map with no `key` to a reference naming no type, and reads no entry bound. A nil key
    is the one spelling of a string key on either, and `irverify` rejects one naming the `string`
    primitive.
  - `additionalProperties: true` became a catch-all typed by the top type, as `{}` already was,
    where it had lowered to nothing, like an absent keyword. A consumer pinned to 0.13.0 reads a
    model writing `true` beside its properties as stating nothing about the rest.
- **0.15.0** — a union a discriminator routes is `WireTagged` ([unions](./unions.md)). The shape is
  unchanged, but the OpenAPI compiler had lowered every discriminated `oneOf` and `anyOf` with
  `wireTagged` false beside its `discriminator`, a combination the tag modes do not define. A
  consumer pinned to 0.14.0 that follows them reads each such union as untagged and ignores its tag.
  `irverify` now refuses the combination (`ir/union-discriminator-untagged`).
- **0.16.0** — usage is derived, not stored ([derived-facts](./derived-facts.md)): `TypeCommon` lost
  `Usage`, and `UsageFlags` moved to `ir/irfacts`, whose `Index.Usage` derives it. No compiler or
  pass ever wrote the member, so every document carried none, and a consumer pinned to 0.15.0 loses
  nothing it read; the version check refuses it a 0.16.0 document all the same.
- **0.17.0** — a spec split across files is one document ([identity](./identity.md),
  [provenance](./provenance.md)). The shape is unchanged, but the OpenAPI compiler lists every
  document a reference reads in `Sources`, after the source and its overlay, and lowers what each
  declares where it declares it: its `Provenance.Source` names that entry, and its IDs are in
  namespaces qualified by the document's location (`t/openapi@common.yaml/...`). It had lowered no
  schema another document holds and placed a parameter, response or other object read from one at
  the source's use site. A consumer pinned to 0.16.0 reads `Sources` as the source and its overlay
  alone.
- **0.18.0** — a reference says when its null was inferred
  ([type-graph](./type-graph.md#type-references)): `TypeRef` gained `Inferred`, naming the heuristic
  that read null in, and empty where the source declares it. The OpenAPI compiler sets it where its
  nullable-widening policy reads a null in, from `nullable: true` or beside a `$ref`, and the
  document's version does not. A consumer pinned to 0.17.0 reads every nullable reference as
  declared.
- **0.19.0** — two members, each a home for something the OpenAPI compiler had classed or dropped:
  - `Operation` gained `Default`, the response that answers every status no entry of `Responses` or
    `Errors` names ([operations](./operations.md)). The compiler had lowered OpenAPI's `default` to
    `Errors`, classing it a failure, which the document does not say: an operation whose only
    response is `default` had no success response. A consumer pinned to 0.18.0 finds no `default`
    member and no longer finds the response among the errors.
  - `Model` gained `Requires`, the members an instance must carry that no property of its own
    declares: one a `Base` or `Mixin` declares, which a composing schema makes required, or one only
    its catch-all governs ([models](./models.md#required-members)). The compiler had dropped such a
    name, reporting it only on an `allOf`, and kept a map's verbatim, so an object writing a
    catch-all and a `required` name is now a `Model` where it had been a `MapT`. A consumer pinned
    to 0.18.0 reads those members as optional.
- **0.20.0** — a member a model's composition declares more than once is addressed by one
  declaration ([models](./models.md#the-flattening)). The shape is unchanged, but a `PropID` that
  names a model's member, an encoding key in `Content.Encoding` ([payloads](./payloads.md)) or a
  discriminator's `Property`, now names the declaration that model's flattening takes. The OpenAPI
  compiler had keyed a part two composed models declare by the first it found: the body's own, then
  its `Base`, then each `Mixin` in turn. It keys the later conjunct's now, the one the flattening
  takes, so `allOf: [{$ref: A}, {$ref: B}]` keys `B`'s. A consumer pinned to 0.19.0 that finds a
  part's entry by that old lookup finds none for such a part.
- **0.21.0** — an inferred-marker list has a type of its own
  ([unmodeled](./unmodeled.md#promotion)): `Provenance.Inferred` and `TypeRef.Inferred` are
  `Inferred`, the list `JoinInferred` writes, where each was a `string`. The JSON is unchanged, but
  a member's Go type is part of the shape, and `irverify` now holds every stored list to that form
  (`ir/inferred-not-canonical`), so a list that names a heuristic twice, or holds an empty name, is
  reported where it passed. A consumer pinned to 0.20.0 reads the same bytes; the version check
  refuses it a 0.21.0 document all the same.
- **0.22.0** — servers became entities, and serialization facts kept verbatim gained homes:
  - `Server` gained `ID` and `Provenance`, and `Document.Servers` became a registry keyed by
    `ServerID` that holds the servers the source declares, whichever object declares them
    ([servers](./servers.md)). `Service.Servers` and `Channel.Servers` name servers by ID where they
    had indexed a list, and an empty one names no server where it had meant every server.
    `Operation` gained `Servers`, the servers it is sent to in place of its service's. The OpenAPI
    compiler had kept a path item's and an operation's `servers` verbatim under `Unmodeled`, left
    `Service.Servers` unset, and lowered the `/` OpenAPI supplies for a document declaring none as
    if declared. A consumer pinned to 0.21.0 cannot read the registry, and sends every operation to
    the document's servers.
  - `HTTPParamBinding` gained `AllowEmptyValue`, `PartEncoding` gained `AllowReserved`, and
    `Response`, `ErrorCase` and `PartEncoding` hold `Headers` as `[]Header`, a `Property` with
    `Style` and `Explode` beside its members, which encode flat ([responses](./responses.md)). The
    compiler had kept each verbatim under `Unmodeled` as `no_ir_home`, where a consumer pinned to
    0.21.0 that reads those entries no longer finds them.
- **0.23.0** — what these facts say changed:
  - `StreamingNone` is `""`, where it was `"none"`, the state every operation that does not stream
    already encoded by having no `streaming` member, and `irverify` holds `Streaming` to the
    declared modes (`ir/unknown-streaming-mode`)
    ([interaction-patterns](./interaction-patterns.md)). No compiler wrote `"none"`, so no compiled
    document changes; one that writes it, or a consumer comparing against it, is out of date.
  - A file body's `Content.Type` holds what its schema lowered to ([payloads](./payloads.md)). The
    OpenAPI compiler had written `bytes` over it, which left the schema on a node nothing
    referenced. `File` still says the body is a file, so a consumer pinned to 0.22.0 that reads
    `File` first is unaffected, and one that expects `Type` to be `bytes` finds a reference to the
    schema instead.
  - `Enum.ValueType` holds the primitive a declared type and format select, where every member is
    a value of it ([enums](./enums.md)): `int64` where the compiler had written `integer` and kept
    the format verbatim under `Unmodeled`, an entry such a consumer no longer finds.
  - `StreamDetail.Events` holds what one frame holds where a compiler reads a stream from a media
    type ([interaction-patterns](./interaction-patterns.md)): a list's `Elem`, where the OpenAPI
    compiler had named the list, which describes the whole body. `Content.Type` keeps the list.
  - `Union.Exclusive` is false on a union whose variants can match one instance, which the source
    did not ask to exclude ([unions](./unions.md)): a type array naming `integer` and `number` or
    repeating a type, and an enum whose entry repeats or lowers to the top type. The compiler had
    set it, leaving such a union to refuse what its schema admits.
- **0.24.0** — two shape changes made together, neither of which a document compiled today sets:
  - `Docs` gained `Refs`, the spans of `Description` that name a type, each a `DocRef`
    ([annotations](./annotations.md#references-in-prose)). They replace the `{t:TypeID}` tokens
    `Description` was documented to hold, which no compiler wrote, so `{t:` in a description is now
    text and nothing else. A consumer that scanned descriptions for tokens must read `Refs`.
  - `ErrorCase` and `OAuthFlow` gained `Inferred`, and a promotion is now recorded on the node
    keeping its extension, whichever node holds the field it fills
    ([promotion](./unmodeled.md#promotion)). Where a node with a `Provenance` holds the field, as in
    every promotion the OpenAPI compiler makes, nothing moves.

  A consumer pinned to 0.23.0 refuses the document as another generation's.
- **0.25.0** — a security requirement keeps a scheme the compiler resolved no mechanism for
  ([auth](./auth.md#unresolved-schemes)). `AuthKind` gained `unresolved`, and auth IDs the
  `unresolved` namespace ([identity](./identity.md#unresolved-schemes)), which every OpenAPI document
  declares under `idSpaces.auth`, whether or not it mints there. The OpenAPI compiler had dropped
  such a requirement, so an `Auth` left with no option read as inheriting and one left with others
  lost the option. It also read a name whose value reads as no list of strings as not written, so
  `{name: null}` read as the empty option, and such a name now stands. A `securitySchemes` entry
  yielding no mechanism had lowered to nothing, and now lowers to that kind whether or not a
  requirement names it. A consumer pinned to 0.24.0 finds those operations naming a kind it has no
  reading for, where it read them as inheriting the service's credentials.
- **0.26.0** — no shape change; two changes to what a compiled document holds, made together:
  - An `allOf` no conjunct can make an object is no `Model`, short of the cases
    [models](./models.md#an-allof-no-object-satisfies) keeps composing: it lowers to a use of the
    conjunct that decides it, a reference to its `$ref` or the node its inline branch lowers to. The
    OpenAPI compiler had built a `Model` for every `allOf`, which asserted an object where the source
    rules one out, so a consumer pinned to 0.25.0 finds a `Scalar`, an `Enum` or a reference where
    it found a `Model`.
  - A node built at an inline `allOf` branch takes the name of the schema holding the `allOf`
    ([naming](./naming.md)), where it was named as a union's variant is, `variant_N`, so the
    symbols an emitter generates from such a hint are renamed.

  A consumer pinned to 0.25.0 refuses the document as another generation's.
