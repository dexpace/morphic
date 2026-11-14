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
- **0.26.0** — `MinItems`, `MaxItems` and a true `UniqueItems` written for one use of a shared
  schema reach the `Constraints` of the `Property`, `Parameter` or alias `Scalar` holding that use
  ([annotations](./annotations.md#one-structural-home-per-declaration)), where a position that
  lowers to a reference had kept them verbatim under `Unmodeled`. No key changed. A consumer pinned
  to 0.25.0 that reads those entries no longer finds them, and a consumer that reads `ir.Decorated`
  finds such an alias is no longer a decorator, since it says something of the value.
- **0.27.0** — a stability tier and a parameter's redaction gained homes, and the OpenAPI compiler
  reads more of a document's keys into fields it had:
  - `Operation` and `OperationGroup` gained `Stability`, a closed `alpha | beta | stable`, empty
    where a source states none ([operations](./operations.md#stability)), and `irverify` reports a
    tier the IR does not declare (`ir/unknown-stability`). A consumer pinned to 0.26.0 cannot decode
    a document that sets it.
  - `Parameter` gained `Secret`, the per-use redaction `Property.Secret` is, and the OpenAPI
    compiler no longer keeps a parameter's `format: password` under `Unmodeled`. A consumer pinned
    to 0.26.0 that reads `openapi:format` on a parameter no longer finds it, and a parameter that
    keeps redaction through a `$ref` now states it itself.
  - A vendor key can state `Deprecation` where no keyword does, `x-sensitive` sets `Sensitive` and
    `Secret`, and a property, header or parameter naming a `$ref` reads what the target's keys state
    ([`extension-promotion`](../compilers/openapi.md#extension-promotion)), listing the marker the
    target lists ([promotion](./unmodeled.md#promotion)). These shapes are unchanged, so to a
    consumer pinned to 0.26.0 such an operation reads as live and such a value as plain.
- **0.28.0** — `time` holds no offset, as `datetime` does beside `datetime_offset`
  ([primitives](./primitives.md#offsets)). The OpenAPI compiler had lowered `format: time`, RFC
  3339's `full-time`, to `time`, and now keeps it as a `Scalar` over `string` whose `Encoding.Name`
  is `time`, as it keeps any format it has no primitive for. `time-local` lowers to `time`, and
  `date-time-local` to `datetime`, which no compiler had written, where each had been such a
  `Scalar`. An `Enum` takes those primitives as its `ValueType` where its members fit them, and one
  declaring `format: time` keeps the format verbatim under `Unmodeled`. A consumer pinned to 0.27.0
  that reads `time` as carrying an offset finds values with none, and one reading the `time-local`
  or `date-time-local` encodings no longer finds them.
- **0.29.0** — `Content` gained `Docs`: a media type's own documentation, as against the body's
  ([payloads](./payloads.md)). The OpenAPI compiler writes there an OpenAPI 3.2 Media Type Object's
  `description`, which it had kept verbatim under `Unmodeled` as `openapi:description`, and the
  `summary` and `description` beside a content entry's `$ref` into `components/mediaTypes`, which
  it now follows. A consumer pinned to 0.28.0 refuses the document as another generation's.
- **0.30.0** — the OpenAPI compiler keeps a path item's own `summary` and `description` on each
  operation it mounts under `openapi:pathItem/summary` and `openapi:pathItem/description`, beneath
  the scope its extensions and undeclared keys already took, rather than under
  `openapi:pathItemSummary` and `openapi:pathItemDescription`
  ([`path-item-docs`](../compilers/openapi.md#path-item-docs)). No shape changed. A consumer pinned
  to 0.29.0 that reads the old keys no longer finds the path's text there. Where an operation writes
  an undeclared key of that name, it finds that key's value, which 0.29.0 let the path's text
  replace.
- **0.31.0** — no shape change; two changes to what a compiled document holds, made together:
  - An `allOf` no conjunct can make an object is no `Model`, short of the cases
    [models](./models.md#an-allof-no-object-satisfies) keeps composing: it lowers to a use of the
    conjunct that decides it, a reference to its `$ref` or the node its inline branch lowers to. The
    OpenAPI compiler had built a `Model` for every `allOf`, which asserted an object where the source
    rules one out, so a consumer pinned to 0.30.0 finds a `Scalar`, an `Enum` or a reference where
    it found a `Model`.
  - A node built at an inline `allOf` branch takes the name of the schema holding the `allOf`
    ([naming](./naming.md)), where it was named as a union's variant is, `variant_N`, so the
    symbols an emitter generates from such a hint are renamed.

  A consumer pinned to 0.30.0 refuses the document as another generation's.
- **0.32.0** — `FileInfo` and `PartEncoding` gained `Inferred`, the heuristics that read a body or a
  multipart part as a file ([payloads](./payloads.md)), held to `Filename` on a part
  (`ir/part-encoding-inferred-not-filename`). The OpenAPI compiler reads more of the specification's
  file idioms ([`file-bodies`](../compilers/openapi.md#file-bodies),
  [`multipart-encoding`](../compilers/openapi.md#multipart-encoding),
  [`item-schema`](../compilers/openapi.md#item-schema)). As declared files: a body on
  `application/octet-stream` whose schema says no more than that it is a string, or a union of such
  schemas and raw bytes; a schema declaring `format: binary` with no `type`; a multipart part, or
  each element of an array part (its `prefixItems` and the `items` after them), declaring raw bytes,
  writing no `type` from 3.1, or saying no more than that it is a string while its encoding entry
  names `application/octet-stream` alone; a union part whose every value is one of those, read off
  its variants, or each branch of its declaration, an array branch through its elements; and each
  item of a multipart sequential body read so off `itemSchema` and `itemEncoding`, with an
  `ItemEncoding` saying so where none is written. A union part with a list or tuple among its
  variants, file or not, has `Multi`. Marked
  `binary-media-type`: such a body, or such a part or item through its encoding's `contentType`, on
  a media type the compiler's binary policy names
  ([`binary-media`](../compilers/openapi.md#binary-media)). A `false` schema on
  `application/octet-stream` is no longer a file, nor is `format: binary` beside a
  `contentEncoding`. A consumer pinned to 0.31.0 refuses the document as another generation's.
- **0.33.0** — four changes to the type nodes, made together:
  - `Union` gained `Closed` and `InputOnly`. `Closed` is false for an open union, whose payload may
    hold a variant it does not list; every union the OpenAPI compiler lowers is closed, since a
    `oneOf` or `anyOf` is, unless a promoted extension opens it, as `x-extensible-enum` does under
    the default mapping on the union of literals a mixed enum lowers to. `InputOnly` marks a union
    only a request carries, as `Model.InputOnly` marks an input object. Both are always written. A
    consumer pinned to 0.32.0 refuses the document for its unknown members.
  - `Literal` gained `ValueType`, the primitive the source types its value as, so an integer and a
    number of one value, or a string and a date, are told apart where they were one `Value`, with
    the type or format kept verbatim under `Unmodeled`. Those entries are no longer written where
    the value fits what was declared, so a consumer pinned to 0.32.0 that read them finds nothing.
  - `AdditionalProps.Value` became optional: nil says no key beyond the patterns is allowed, and
    `additionalProperties: false` beside `patternProperties` lowers to it, where it had typed the
    catch-all as the top type beside a closure that forbade the same keys. A `false` `allOf` branch,
    which closes its model, takes the value too, keeping a written `additionalProperties` verbatim.
    A consumer pinned to 0.32.0 decoding the value as required fails on the missing member.
  - `ValueKind` gained `inf`, `neginf` and `nan`, the IEEE 754 non-finite numbers, each with no
    payload. No OpenAPI value is one, so no document the OpenAPI compiler writes holds them.
- **0.34.0** — three changes to the HTTP binding, made together:
  - `HTTPBinding.IsWebhook` became `Inbound`, a closed `webhook | callback`, empty for an operation
    the client sends, so a callback operation is marked the way a webhook is, where only a reverse
    lookup through its declaring operation's `Callbacks` showed it. `Callback` gained `Name`, the
    callbacks-map key. A consumer pinned to 0.33.0 refuses the document for its unknown members,
    and finds no `isWebhook` where it read one.
  - `HTTPBinding` gained `PathDocs`, a path item's own summary and description, which the OpenAPI
    compiler had kept on each operation the item mounts under `openapi:pathItem/summary` and
    `openapi:pathItem/description` (`no_ir_home`) with an info report. Those entries and that report
    are retired there; an item mounting no operation still keeps its pair on the nearest node.
  - An inbound operation's `Auth` now states only what it declares, inheriting no service, server
    or document requirement ([auth](./auth.md#inbound-operations)). No key changed for this; a
    consumer pinned to 0.33.0 reading a webhook with no `Auth` as inheriting hands it the client's
    credentials.
- **0.35.0** — no shape change; three changes to what a compiled document holds, made together, each
  reading what an `allOf` conjunct writes as the OpenAPI compiler reads it written beside the `allOf`
  ([`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue),
  [`allof-refinement`](../compilers/openapi.md#allof-refinement)):
  - A `minItems`, `maxItems` or true `uniqueItems` on an inline branch reaches the `Constraints` one
    written beside the `allOf` reaches: of the `Property`, `Parameter` or alias `Scalar` holding a
    refinement's use of its principal, or of the `List` or `Tuple` the position lowers to. The branch
    had been kept whole under `Unmodeled`. A collection bound on a branch beside a type list lands
    on its `List` or `Tuple` variant.
  - An `enum` or a `const` beside an `allOf` that refines is one of its conjuncts rather than the
    winner of the election of competing keywords, so the `allOf` is no longer kept whole under
    `Unmodeled`: its branches fold, a `$ref` it writes lowers, and a narrower branch is what the
    position uses. One beside an `allOf` that composes still wins it, and conjuncts sharing no value
    there draw the warning a composition's do. A union variant written as such a value set narrowing
    a `$ref` takes the hint the `$ref` suggests, as the same conjunction written in its branches
    does, where it took `variant_N`, so the symbol an emitter generates from it is renamed.
  - The `ValueType` of the `Enum` or `Literal` a refinement lowers to reads the type and format every
    conjunct declares, where it read its own schema's alone, and a branch whose format it holds is
    no longer kept.

  A consumer pinned to 0.34.0 that read those `Unmodeled` entries no longer finds them, and refuses
  the document as another generation's.
- **0.36.0** — pagination and streaming gained the facts declared pagination and conditional
  streams state:
  - `ParamPath` became `InputPath`, whose `Param` is optional: nil roots the path in the request
    body, so a search endpoint's body cursor or page size is an input. `Pagination` gained
    `TotalPages`, counting pages where `TotalCount` counts items, and `Stop`, the closed
    `empty | no_next | short | total` conditions that end iteration, where each consumer guessed.
  - `StreamDetail` gained `Terminator`, the frame data that ends a stream and is no element (an
    SSE `data: [DONE]`), and `Condition`, the request input and value that ask for a stream an
    operation sends only when asked.
  - No compiler fills these yet. A consumer pinned to 0.35.0 refuses a document that sets one, and
    finds a body input's `param` absent where it required one.
- **0.37.0** — fields that could not say what a source left unstated, or who wrote it:
  - `ServerVariable.Default` became `*string`: nil states no default, and an empty string
    substitutes nothing, as an OpenAPI `default: ""` does. The OpenAPI compiler writes the empty
    string where it had written no key, so a consumer pinned to 0.36.0 reading the absent key as
    no default now reads `""`.
  - A property's `XMLHints` and `Encoding` override its type's field by field, a field the property
    leaves unset taking the type's, where the property's had replaced the type's whole
    ([XML hints](./constraints.md#xml-hints), [encodings](./constraints.md#encodings)). So each
    field is written only where declared: `XMLHints.Wrapped` became `*bool`, and `XMLHints` gained
    `Attribute`, a `*bool` holding what `attribute` writes, which no longer lowers into `NodeType`.
    A consumer pinned to 0.36.0 finds no `wrapped` member where it read `false`, no `nodeType`
    where `attribute: true` wrote `attribute`, and, replacing a type's hints or encoding with a
    property's whole, loses what the property left unset.
  - `Provenance` gained `WrittenBy`: an overlay's writing into the source is located at the
    source's index with the overlay as its writer, where `Source` had named the overlay at a
    position of another document. A consumer pinned to 0.36.0 reading such a pointer against the
    overlay's file reads it at the wrong place.
- **0.38.0** — no shape change: an inline branch of an `allOf` beside a `oneOf`, an `anyOf` or a
  `$dynamicRef` folds what it writes for the position into the composition, as a composing `allOf`'s
  branch does with none of them there, where the OpenAPI compiler had kept it whole under
  `Unmodeled["openapi:allOf/<n>"]`
  ([`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue)). Where a union
  distributes ([models](./models.md#composition-beside-a-union)), the branch's docs, deprecation,
  examples, secrecy and XML hints reach the `Union`, what a use binds its carriers, and a value
  bound each composed variant, and a branch with no residue left is kept on no variant; beside a
  union or a `$dynamicRef` kept verbatim they reach the `Model`, as in a composition with neither
  beside it. A consumer pinned to 0.37.0 that read the branch under that key no longer finds it, and
  refuses the document as another generation's.
- **0.39.0** — no shape change. The OpenAPI compiler lowers four spellings of a union it had kept
  verbatim under `Unmodeled`: one on an inline branch of a composing `allOf`, distributed across the
  rest of the composition as one beside the `allOf` is, and one on a branch of an `allOf` no object
  satisfies, read as a conjunct, where it kept the branch under `openapi:allOf/<n>`; one beside a
  model with inline branches, each conjoined by the node it lowers to at its own pointer, and one
  beside keywords its every branch already says, lowered as the union alone, where it kept the
  branches under `openapi:oneOf` or `openapi:anyOf`
  ([`union-beside-structure`](../compilers/openapi.md#union-beside-structure)). `Content.Encoding`
  on a body that is a union, as a distributed one is, keys each part by the property of the first
  variant declaring it, where its keys were defined only against a body model and `pass.Validate`
  refused them. A consumer pinned to 0.38.0 that read such a union under those keys no longer finds
  it there, and one looking such an encoding key up among the union's own members finds none.
- **0.40.0** — no shape change. The OpenAPI compiler lowers a `oneOf` or `anyOf` with a
  `discriminator` beside structure, where no branch composes the schema, as it lowers one with
  none, a union of its branches where they say the keywords beside it and a union of composed
  variants otherwise, the discriminator routing the union, where it kept the branches under
  `openapi:oneOf` or `openapi:anyOf` and the discriminator under `openapi:discriminator` beside a
  model ([`union-beside-structure`](../compilers/openapi.md#union-beside-structure)). Its
  discriminator-inference policy, on by default, reads a union's tags off the string each variant
  pins ([`discriminator-inference`](../compilers/openapi.md#discriminator-inference)): it fills the
  `Mapping` of a discriminator declaring none where OpenAPI's implicit mapping by component name
  would route a variant by a tag it rejects, and gives a union declaring none a `Discriminator` with
  `Inferred` set, the union `WireTagged`, where every variant requires a property pinned to a
  distinct string. The union's `Provenance.Inferred` names `discriminator-inference` either way. A
  consumer pinned to 0.39.0 no longer finds those keys on such a schema, reads an undeclared union
  it read as untagged as tagged, and routes a declared one by a mapping it had read as nil.
- **0.41.0** — no shape change. The OpenAPI compiler lowers a `type` listing more than one type with
  no `Discriminator` on its `object` variant, keeping the schema's `discriminator` verbatim under
  `Unmodeled["openapi:discriminator"]` on the `Union` the type array lowers to
  ([`type-arrays`](../compilers/openapi.md#type-arrays)). The variant had held it except beside a
  `oneOf` or `anyOf` kept verbatim, where a mapping, a default or a `$ref` branch moved it to the
  `Union`. Nothing can name the variant, so no type is its subtype: a mapping to a subtype of the
  schema failed validation, and a route by a subtype's component name went unread. An object variant
  that held the discriminator beside a catch-all and nothing else now lowers to a `MapT`, as that
  object alone does. A consumer pinned to 0.40.0 that read the variant's `Discriminator` no longer
  finds it there.
- **0.42.0** — no shape change. The engine's `const-unions` pass, on by default, rewrites a closed
  union whose every variant names an anonymous string `Literal`, a closed `Enum` of one string or a
  union of one value in turn, such as a `oneOf` of `const`s or an `anyOf` of one-member `enum`s, as
  the closed `Enum` of its values at the union's own ID, each member holding what its branch said of
  its value, and removes the branch types nothing else references
  ([`string-const-union`](../compilers/openapi.md#string-const-union)). A consumer pinned to 0.41.0
  that read such a position as a `Union` finds an `Enum` there, and no longer finds the types its
  variants named.
- **0.43.0** — no shape change. The OpenAPI compiler tags a subtype by its component name only where
  a discriminator routes it by name
  ([`discriminator-value`](../compilers/openapi.md#discriminator-value)): one written beside a
  `oneOf` or `anyOf` routes only what that union lists, so a schema composing a discriminated union
  that does not list it, a narrowing of the union, has no `DiscriminatorValue`, where it had carried
  its name, which the union's discriminator routes to no variant, and a union written beside such a
  schema's `allOf` distributes over it, as beside any schema no route names, where it was kept
  verbatim under `openapi:oneOf` or `openapi:anyOf`. Its discriminator-inference policy reads what a
  subtype routed by name admits for the tag
  ([`discriminator-inference`](../compilers/openapi.md#discriminator-inference)): admitting only
  strings other than its name, it carries the one it admits, or no tag where it admits several or
  none, the subtype's `Provenance.Inferred` naming `discriminator-inference` either way. A consumer
  pinned to 0.42.0 that wrote or routed such a subtype's tag by its name no longer finds the name
  there, and one that read a union kept verbatim beside such a schema finds the schema a `Union`
  instead.
- **0.44.0** — a response's links gained a typed home ([links](./responses.md#links)):
  - `Response` and `ErrorCase` gained `Links`, each an `OperationLink` naming the operation it leads
    to, the value it gives each of that operation's parameters and its body, and the server it is
    called on. A `LinkValue` is a constant, the URL, method or status of the exchange, or a value of
    the request answered or of the response, as an `InputPath` or a `PropPath`.
  - An `InputPath` with neither a parameter nor a segment is the request body whole, which
    `ir/input-path-empty` had refused; that check is gone, and `ir/input-body-absent` holds the
    path, a link's among them, to an operation that sends a body.
  - The OpenAPI compiler lowers a link it resolves whole and keeps any other verbatim under
    `openapi:links/<name>`, with an `openapi/invalid-link` or `openapi/untyped-link` finding, where it
    had kept the whole map under `openapi:links`. A consumer pinned to 0.43.0 refuses a document with
    a link, and finds no `openapi:links` entry where it read one.
- **0.45.0** — `Deprecation` gained `ReplacedBy`, what the source says to use instead, each an
  `EntityRef`: a new value naming one entity by its ID, in the field of its class
  ([replacements](./annotations.md#replacements)). The OpenAPI compiler fills it only through a
  promotion mapping naming the `deprecation.replacedBy` target, which no default does. A consumer
  pinned to 0.44.0 refuses a document whose deprecation names a replacement.
- **0.46.0** — `Header` gained `Prefix`, which makes a response's, an error's or a part's header a
  map of every field under the prefix, the empty prefix taking every field, as Smithy's
  `@httpPrefixHeaders` binds one on an output or an error ([prefix
  headers](./responses.md#prefix-headers)). `irverify` holds such a header to a map type and no
  wire name, and `irfacts.CorrelationVersion` moved to 3, keying it by its prefix. No compiler
  fills it yet. A consumer pinned to 0.45.0 refuses a document with a prefix header.
- **0.47.0** — the empty prefix of a parameter's binding got a spelling, and other fields a request
  is written by changed meaning ([HTTP binding](./http-binding.md)):
  - `HTTPParamBinding.Prefix` became a pointer, nil for a binding of one field, so the empty prefix,
    which takes every header or query parameter, as Smithy's `@httpPrefixHeaders("")` and
    `@httpQueryParams` bind one, is a prefix ([prefix bindings](./http-binding.md#prefix-bindings)).
    `irverify` holds a binding under a prefix to a `header` or `query` location, to no wire name
    and, binding a whole parameter, to a map type, and `irfacts.CorrelationVersion` moved to 4,
    keying such a parameter by its prefix. No compiler fills it yet. A consumer pinned to 0.46.0
    reads the empty prefix as no prefix.
  - A nil `HTTPBinding.PatchImplicitOptionality` states nothing, so a `PATCH` body requires what
    its schema requires; only `true` makes every member optional ([patch
    optionality](./http-binding.md#patch-optionality)). A consumer pinned to 0.46.0 relaxed every
    `PATCH` body on nil, and so let a caller leave out what the server requires.
  - A value whose `HTTPParamBinding.PathPattern` names several segments is percent-encoded in all
    but `/` and RFC 3986's unreserved characters, whatever `AllowReserved` says, and a consumer
    that cannot check a value against the pattern skips that check, never the pattern. One pinned
    to 0.46.0 sent such a value under RFC 6570's reserved expansion, or as one segment.
  - `AllowReserved`, on a parameter's binding and on a part's encoding, passes percent-encoded
    triples unescaped beside the reserved characters, as RFC 6570's reserved expansion does. A
    consumer pinned to 0.46.0 encoded a triple's `%` again.
  - `HTTPBinding.RequestContentTypes` lists the request body's media types in source order and
    states no preference among them, where it was worded as priority-ordered.
- **0.48.0** — four changes of meaning, made together, with no change of shape:
  - An inbound operation's empty `Servers` names no server where it had named its service's
    ([servers](./servers.md#inbound-operations)), as its `Auth` inherits nothing: the service's
    servers are where the API is sent requests. A consumer pinned to 0.47.0 sends a webhook or a
    callback to the API itself.
  - An `OperationGroup` may hold no operation and no group. The OpenAPI compiler's grouping by
    extension makes one for each path a `groups` list names after the first and each path an
    operation's aliases name ([`extension-groups`](../compilers/openapi.md#extension-groups)), for a
    remount to name. A consumer pinned to 0.47.0 meets a sub-client with nothing in it.
  - A callback's operation may list callbacks of its own on its binding, each lowered as its
    parent's are ([`callbacks`](../compilers/openapi.md#callbacks)), where the OpenAPI compiler had
    dropped them. A consumer pinned to 0.47.0 that walks one level of callbacks misses them.
  - The OpenAPI compiler leaves a path key's fragment out of `URITemplate`, keeping it on the binding
    as `openapi:fragment`, and sets `SharedRoute` on each path operation whose method and route
    another sends ([`path-keys`](../compilers/openapi.md#path-keys)). A consumer pinned to 0.47.0
    builds the fragment into a URL and reads no route as shared.
- **0.49.0** — no shape change. The engine's `const-unions` pass rewrites a closed union of strings
  whose variant names an anonymous branch of several values, a closed `Enum` of strings or a union of
  them, where that variant is the branch's one reference and neither says anything of all its
  values: the branch's values become members of the union's `Enum`, each holding what its own
  member held, and the branch type goes
  ([`string-const-union`](../compilers/openapi.md#string-const-union)). A model's `Discriminator`
  may route to the model itself where it is not `Abstract`, from its `Mapping` or its `Default`,
  which `pass.Validate` had refused ([discriminator](./models.md#discriminator)). A consumer pinned
  to 0.48.0 that read such a union as a `Union` finds an `Enum` there, no longer finds the branch
  type, and meets a base among the types its own discriminator routes to.
- **0.50.0** — no shape change. A union's `Discriminator.Mapping` that holds an entry its source
  writes holds, beside those entries, the route the source implies to each variant it declares none
  for ([models](./models.md#discriminator)): the OpenAPI compiler writes into it the name of the
  component that variant's branch names, and writes the routes out too where a variant's type is an
  alias for a keyword beside its `$ref`, as it did for a variant composed over its branch
  ([`discriminator`](../compilers/openapi.md#discriminator)). Its discriminator-inference policy
  reads what a variant or a subtype admits as a set of strings
  ([`discriminator-inference`](../compilers/openapi.md#discriminator-inference)): a variant pinning
  its tag to several strings no other variant admits maps each to it, in a declared discriminator's
  `Mapping` and in an inferred one; a tag written as a union of string values bounds them as an
  `enum` does; and what a schema composes as a union, its variants read as objects in turn, meets
  what it declares. A consumer pinned to 0.49.0 that routed a variant a mapping left out by its
  type's name finds the route in the `Mapping`, reads unions it read as untagged, or as routed by
  component names, as routed by the strings their variants pin, and finds no `DiscriminatorValue` on
  a subtype whose composed union rules its name out.
- **0.51.0** — no shape changed: four OpenAPI policies, each on by default and switched off by its
  option, fill facts a document implies by its names and messages, each node they fill marked in its
  `Provenance.Inferred`. An operation sent by `POST`, `PUT`, `PATCH` or `DELETE` takes its one
  header parameter `Idempotency-Key` as its `Idempotency` token
  ([`idempotency-inference`](../compilers/openapi.md#idempotency-inference)); a parameter, a header
  or a member whose name ends in a credential's and whose value is text or bytes is `Secret`
  ([`secret-inference`](../compilers/openapi.md#secret-inference)); a deprecation's message naming a
  declared route or a sibling in code fills `ReplacedBy`
  ([`replacement-inference`](../compilers/openapi.md#replacement-inference)); and a `date-time`
  response header whose field an RFC defines as an HTTP-date takes `Encoding` `http-date`
  ([`http-date-inference`](../compilers/openapi.md#http-date-inference)). A consumer pinned to
  0.50.0 refuses a document from this generation, whose idempotency, redaction, replacements and
  header texts it would read as no OpenAPI document wrote them.
- **0.52.0** — `Content` gained `Stream`, a `ContentStream` marking a body that is a sequence of
  frames rather than one document, its `Inferred` naming the heuristics that read it as one
  ([payloads](./payloads.md#a-content-that-streams)). Every content that streams sets it, in a
  request, a response, an error or the default, one declaring an `Item` among them, so a response
  offering `application/json` beside `text/event-stream` says which of the two streams. The OpenAPI
  compiler sets it for a 3.2 `itemSchema` and for a media type its streaming policy names, the
  latter marked `streaming-media-type`. `irverify` holds a content declaring an `Item` to a `Stream`
  (`ir/content-item-without-stream`), and a streaming content of a request or a success response to
  its side's `StreamDetail` (`ir/content-stream-without-detail`). A consumer pinned to 0.51.0
  refuses a document with a streaming content.
- **0.53.0** — four changes of meaning, the shape kept:
  - A `null` is no `Enum` member, no variant of the `Union` an enumeration lowers to and no
    `Literal`'s value, but its reference's `Nullable` bit, so an enumeration of `null` alone and a
    constant of `null` are each a closed `Enum` with no member behind a nullable reference
    ([enums](./enums.md#the-empty-enum), [containers](./containers.md#literals)). The OpenAPI
    compiler had lowered a `null` beside members of several kinds to a `null` `Literal` variant as
    well as a nullable reference, `null` alone to a union of that one variant, and `const: null` to
    a `null` `Literal`. A consumer pinned to 0.52.0 finds no `null` variant or `Literal` where it
    read one.
  - An `Enum`'s `ValueType` is a primitive every member is a value of. The OpenAPI compiler had
    taken a declared `type` the members are not values of, a `type: integer` over strings among
    them; it now takes the members' primitive and keeps the type verbatim.
  - A number's format on a string, `type: string, format: int64` among them, is a `Scalar` over the
    number whose `Encoding` is named `string`, a number written as the text of its value, with a
    `string` `WireType`, as `bytes` under `base64` is ([encodings](./constraints.md#encodings)), and
    its `Constraints`, which speak of the number ([constraints](./constraints.md#constraints)), hold
    none of the bounds written beside it, which bound its text or no string instance and are kept
    verbatim. It had been a `Scalar` over `string` whose encoding named the format, holding those
    bounds. A consumer pinned to 0.52.0 reads such a value's base as the number where it read a
    string.
  - Over a `string`, a `WireType` of `string` marks text the encoding is applied to, and none marks
    text already encoded. No compiler writes the first yet.
- **0.54.0** — names an emitter renders moved, and one lowering changed, at an unmoved shape:
  - The [word grammar](./naming.md#the-word-grammar) separates on a letter or mark that is a
    `Default_Ignorable_Code_Point`, a variation selector or the Hangul filler U+3164 among them, as
    on the format characters in that property, so a name of only such characters has no
    `Canonical`, and `irverify` refuses one in a canonical or a hint. A consumer pinned to 0.53.0
    meets `Canonical` and `Hint` it would have carried such a character in without it.
  - The OpenAPI compiler names an anonymous schema that writes a `title`, a union branch's variant
    included, by the title's words, where its position, its owner or the `$ref` beside it named it,
    under a policy `title-names=false` turns off ([`naming`](../compilers/openapi.md#naming)); names the items of a `components/mediaTypes` entry
    keyed `""` from the minted name rather than the pointer; and names a schema under a header of an
    Encoding Object another nests by the header's key.
  - The OpenAPI compiler compares a header parameter's name in any case
    ([`parameters`](../compilers/openapi.md#parameters)), so an operation's `x-trace` overrides
    its path item's `X-Trace`, and one list writing both reports the second as a repeat, each kept
    under the ID its spelling gives it.
- **0.55.0** — no shape change. The OpenAPI compiler writes a count, `minLength`, `maxLength`,
  `minItems`, `maxItems`, `minProperties` or `maxProperties`, past what a 64-bit integer holds to
  no `Constraints` field, where it wrote `0` or a number the source does not state, and keeps it
  verbatim under `openapi:<keyword>` (`degraded_lowering`) on the node or carrier the position owns
  ([`constraints`](../compilers/openapi.md#constraints)); a count literal that is no non-negative
  integer, which the load phase refuses, reaches no field either. An inline `allOf` branch whose
  bound keyword states no value, a 3.0 `exclusiveMinimum` with no bound beside it, one of `false` or
  one the load phase refuses, parts no other branch, so it folds where nothing else parts it
  ([`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue)), what it keeps landing
  under its keyword rather than under `openapi:allOf/<n>`. A field several `allOf` conjuncts
  declare ([`allof-redeclared-property`](../compilers/openapi.md#allof-redeclared-property)) now
  holds two enums' shared values of the narrower value type, where it kept the first enum of
  different value types, and holds what an alias of an enum, a `const`, a `Scalar`, a list, a map
  or a tuple meets beside the node it stands for, where it kept the first; an alias adding only
  null to a type beside it gives that type. A consumer pinned to 0.54.0 that read such a count's
  field finds none there, one reading such a branch under `openapi:allOf/<n>` finds its keywords at
  the position instead, and one reading such a field may find a node minted for it.
- **0.56.0** — three changes of meaning, the shape kept:
  - A schema admitting `null` and nothing else is a closed `Enum` with no member behind a nullable
    reference, whatever spells it ([enums](./enums.md#the-empty-enum)). The OpenAPI compiler had
    lowered a `type` naming `null` alone to the `any` primitive behind a nullable reference, which
    also admits every other value, or to a `Model` where `properties` was written beside it, and a
    `oneOf` or `anyOf` of nothing but such branches to an alias over that `any`. A consumer pinned
    to 0.55.0 finds an `Enum` with no member where it read nullable `any` or an alias of it.
  - A union branch admitting `null` alone, written in place, is the union's `null` in every
    spelling, carried by the reference to the union rather than by a variant
    ([type-graph](./type-graph.md#type-references)). The OpenAPI compiler had lifted only a branch
    of `type: "null"`, and kept an `enum` of `null` alone or a `const: null` as a variant naming an
    `Enum` with no member, so a union of one such branch and one other is now a nullable reference
    to the other, and a union of more has one variant fewer, while one written beside a structural
    body is kept verbatim beside it, as one holding a `type: "null"` branch already was. A consumer
    pinned to 0.55.0 finds no variant where it read one admitting `null` alone. The reverse holds
    too: a `type: "null"` branch writing a `$ref`, a `$dynamicRef`, an `allOf`, a union, or an
    `enum` or a `const` of other than `null` alone is a variant where the compiler had lifted it
    and dropped what it wrote beside its type, so such a consumer also finds a variant where it
    read none.
  - Such a branch writing more than its `null`, a `description` among it, is kept whole under the
    new key `openapi:oneOf/<index>` or `openapi:anyOf/<index>`, reason `no_ir_home`, where the
    compiler had dropped what a lifted `type: "null"` branch wrote beside its type.
- **0.57.0** — no shape change. In the OpenAPI compiler, a `oneOf` or `anyOf` beside an `allOf` is
  one of its conjuncts ([`allof-refinement`](../compilers/openapi.md#allof-refinement)), so an
  `allOf` no conjunct makes an object refines with a union beside it, where it composed a `Model` or
  a union of `Model`s. A union on an `allOf` branch lowers with the discriminator written on the
  branch or beside the `allOf`, and below a discriminated base at a position no route names, where it
  was kept whole under `openapi:allOf/<n>`. A subtype a discriminator routes to composes a union
  written beside or on its `allOf` as a `Mixin` naming a `Union` minted for it, whose hint the
  compiler composes ([models](./models.md#composition-beside-a-union)), where the union was kept
  under `openapi:oneOf`, `openapi:anyOf` or `openapi:allOf/<n>`. A `$ref` entry to a schema composing
  a discriminated one at any depth is the `Base`
  ([`allof-classification`](../compilers/openapi.md#allof-classification)), and so is a type a field
  is redeclared as that holds a declared discriminator on a `Model` or a `Union`, or composes one
  ([`allof-redeclared-property`](../compilers/openapi.md#allof-redeclared-property)). A consumer
  pinned to 0.56.0 finds those keys gone at those positions, a refined node, a `Union` or a `Mixin` where it
  read a `Model` keeping them, and a `Base` where it read a `Mixin`.
- **0.58.0** — the shape is unchanged, and the OpenAPI compiler lowers three registered formats as it
  lowers `format: byte`: to a `Scalar` over the primitive the value is, whose `Encoding` names the
  text it is written in as the [IR](./constraints.md#encodings) does. `http-date` is a
  `datetime_offset` under `http-date`, `unixtime` a `datetime_offset` under `unixTimestamp`, a name
  no compiler wrote before, with the JSON type it is written on as `WireType`, and `base64url` is
  `bytes` under `base64url` with a `string` `WireType`. So the secret policy, which reads a
  credential's name on text or bytes, no longer marks a use declared `http-date`, or `unixtime` on a
  string, `Secret`. A consumer pinned to 0.57.0 refuses the document; read regardless, it would find
  an instant or bytes where it read text or a number, and `unixTimestamp` where it read `unixtime`.
- **0.59.0** — what the OpenAPI compiler writes for `unevaluatedProperties` and `unevaluatedItems`
  changed, the shape unchanged
  ([`additional-properties`](../compilers/openapi.md#additional-properties),
  [`validation-only`](../compilers/openapi.md#validation-only),
  [`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue)):
  - `Model.Additional` is `closed_after_composition` only where no `additionalProperties` is written
    beside the `unevaluatedProperties: false`, which would leave it inert. Beside one the model is
    `closed` for `false`, and otherwise open with its catch-all, so `{type: object,
    additionalProperties: {…}, unevaluatedProperties: false}` is a `MapT`. A consumer pinned to
    0.58.0 read such a model as closed.
  - A catch-all beside `closed_after_composition` holds its patterns and no `Value`, where it held
    the top type: a key no pattern matches is allowed only where composition evaluates it.
  - `openapi:unevaluated` holds an `unevaluatedProperties: false` wherever no model holds it as its
    closure, where it was dropped, and holds an inert keyword together with the
    `additionalProperties`, `items` or `additionalItems` beside it that leaves it inert. A consumer
    pinned to 0.58.0 applying the entry read every keyword in it as live.
  - Neither keyword an inline `allOf` branch writes folds into the position's `openapi:unevaluated`
    any more: the branch is kept whole under `openapi:allOf/<n>`, as a branch writing
    `unevaluatedProperties: false` already was. At the position the keyword exempted what the other
    conjuncts evaluate, where on its branch it exempts only what the branch evaluates. A consumer
    pinned to 0.58.0 finds the keyword at the position.
- **0.60.0** — no shape change. The OpenAPI compiler writes a value bound to `Constraints` only
  where a type the schema declares takes it, as JSON Schema applies each to instances of one kind:
  `minimum`, `maximum`, their exclusive forms and `multipleOf` beside `integer` or `number`,
  `minLength`, `maxLength` and `pattern` beside `string`, and `minProperties` and `maxProperties`
  beside `object` ([`constraints`](../compilers/openapi.md#constraints)). One beside another type
  is kept verbatim under `openapi:<keyword>` (`degraded_lowering`), on an alias hoisted where the
  position lowered to a shared primitive, which then holds the docs its property held, and the
  content vocabulary beside a type other than `string` reaches no `Encoding`
  ([`content-vocabulary`](../compilers/openapi.md#content-vocabulary)). A type array's scalar
  variant holding a bound, an encoding or the content vocabulary is a `Scalar` at
  `<pointer>/type/<index>`, each bound and a format the table pairs with one listed type land on
  the variant whose type takes them, and the property or parameter carrying the union holds no
  bound ([`type-arrays`](../compilers/openapi.md#type-arrays)); a bound on an `allOf` branch beside
  the type list folds onto that variant, and one on a branch declaring a type it does not apply to
  stays with that branch ([`allof-inline-residue`](../compilers/openapi.md#allof-inline-residue)).
  A consumer pinned to 0.59.0 that read a numeric bound on a string, a bound on the carrier of a
  type array, or a variant's encoding taken from a format another listed type pairs with finds it
  under `openapi:<keyword>`, on the variant it applies to, or not at all.
