# Mandatory emitter tests

Built as the code lands, extending the testing strategy in `architecture.md §5`. Cited as `T-N`
throughout these documents.

1. **T-1 · TypeKind switch-completeness** — a test over every `ir.TypeKind` and every target-AST
   `NodeKind`, its kind lists held to what the packages declare; adding a kind fails every dispatch
   that misses it (`assertNever`).
2. **T-2 · Union tag-mode coverage** — all four wire shapes emit distinct (de)serializers, and each
   decodes through the client and through a caller's own decoder call, into the union and into a
   type of the caller's that holds it, and a wrapper alone codes as its variant does
   ([unions](../refine/unions.md)).
3. **T-3 · Four-state field matrix** — required×nullable×presence produce distinguishable
   declarations, or a diagnosed collapse.
4. **T-4 · Wire fidelity** — serialized output uses wire names/IDs, never rendered identifiers;
   positional models serialize by index; `WireID==0` ≠ absent.
5. **T-5 · No-float64** — numeric defaults/constraints/enum values round-trip through `BigVal` with
   no float parsing, including any constraint-merge path.
6. **T-6 · Determinism / golden IR→artifact** — identical IR ⇒ identical bytes; a scoped run equals
   the corresponding slice of a full run.
7. **T-7 · Diagnostic-not-silence** — every un-lowerable construct emits a coded diagnostic, never a
   silent `any` and never a panic, a fact outside M3's scope included ([what M3
   lowers](../milestones.md#what-m3-lowers)).
8. **T-8 · Manifest ID-keying** — the entity→symbol map survives a rename of what an ID is not
   built from, such as an `operationId`, and, through the correlation key, a path or component
   rename.
9. **T-9 · Policy precedence** — declared `Retryable`/`Fault`/`Idempotency` override policy defaults
   at a document-wide setting and at an operation's, in every layer. A project's document-wide
   setting beats a per-operation one the document embeds, an operation's own setting beats its
   layer's document-wide one, an operation ID no operation has is reported, and an embedded value a
   higher layer replaces draws an info naming both
   ([layers](../contract/runtime-policy.md#layers-and-operations)).
10. **T-10 · Step-trace golden** — the ordered refine step list produces a pinned, deterministic
    trace (guards implicit inter-step coupling).
11. **T-11 · Architecture import-graph** — the layering in [package layout](../packages.md).
12. **T-12 · Wire-conformance (milestone 3)** — expected request shapes derived from `Doc + Plan`
    alone, the elected media type among them, diffed against the generated SDK under HTTP
    interception; request-side mismatches block, response-side inform.
13. **T-13 · Runtime determinism** — the retry, backoff and timeout templates run inside a
    `testing/synctest` bubble against `httptest.NewTestServer`, whose in-memory network works within
    the bubble: a backoff schedule is asserted exactly under the fake clock and completes instantly,
    and `synctest.Sleep` advances the clock and waits for the runtime under test to settle.
14. **T-14 · Runtime behavior against a shared server, with a coverage gate (milestone 3)** — every
    target's generated SDK runs one hand-written suite of behaviors against one behavior server, and
    a gate fails a target that silently skips one ([behavior server](behavior-server.md)).
15. **T-15 · Unmodeled routing** — a document carrying an `Unmodeled` entry of each reason, on a
    type an operation reaches, on the operation and on the document, generates byte-identical
    artifacts and the same diagnostics as the document without them, unless a policy names an
    entry's key ([unmodeled entries](../refine/unmodeled.md)).
16. **T-16 · Decorator spellings agree** — a 3.0 `allOf` decorator and the 3.1 `$ref` carrying the
    same annotations beside it, at a property, a parameter, a header, a union variant, a list
    element, a map value and a content, generate byte-identical artifacts
    ([decorators](../refine/decorators.md)), a variant's name included. Which nodes are decorators
    is `ir.Decorated`'s, whose own test enumerates the fields of `Model`, `Scalar` and `TypeCommon`
    by reflection and fails on one [annotations](../../ir/annotations.md#decorators) has not
    classified.
17. **T-17 · Type table** — each target's built-in table has, at every options version, a row for
    every `PrimKind` `ir` declares, read through `internal/decls`; a user row replaces the built-in
    row under its key whole; an encoding with no row falls back to its primitive's and is reported
    once; a row naming an import `Imports` does not bind, an `Imports` key it does not use, or a Go
    `Codec` is an options error; and two rows that spell one name for two packages generate code
    that compiles in a file using both ([type table](../contract/type-table.md)).
18. **T-18 · Placement** — adding a type no operation reaches, an operation that reaches no existing
    type, or declarations past a unit's size limit moves no existing declaration to another unit,
    while an operation in another group moves to the shared unit exactly the types it makes a second
    group reach; under `SplitByGroup` the units' imports form no cycle, and groups whose names
    render alike, or as a name the units' scope reserves, get distinct units; a union with a variant
    in another unit compiles; and every partition error and warning of [output
    units](../refine/output-units.md) has a fixture that draws it.
19. **T-19 · Reserved scopes** — a fixture naming its properties, parameters, path parameters, enum
    members and union variants after every name each scope of [reserved
    names](../refine/reserved-names.md) reserves, after characters alone and after names that begin
    with a digit, and its groups after the standard-library packages its files import and after
    every units name, generates code that compiles on every target, a `clean` of a Kotlin build
    leaving every module's sources, with every template's declared names held to what it binds.
20. **T-20 · Recursive types** — a model that holds itself directly, through a base, through a tuple
    and through another model, and one whose cycle passes through a list or a union, generate code
    that compiles on every target, and exactly the references on a cycle Go holds by value are held
    through a pointer ([recursive references](../refine/models.md#recursive-references)).
21. **T-21 · Declarations keep their root's codec** — a named scalar over every primitive, and over
    a model, an enum and a union, encodes and decodes each value byte for byte as its root does on
    every target, and a constraint the scalar declares is still checked
    ([scalars](../refine/scalars.md)).
22. **T-22 · Unknown variants** — on each wire shape and on a polymorphic base, a value no variant
    recognizes decodes to the fallback on an open union and re-encodes as the same JSON value, its
    members in order and each number's text intact; `Default` takes it first; a closed union refuses
    it with a coded error, and a known tag over a body that does not match is an error on both;
    turning `OpenUnions` off closes every union and removes its `Inferred` mark, and a declared
    answer, once the IR has one, wins ([plan policy](../plan/policy.md),
    [unions](../refine/unions.md)).
23. **T-23 · Errors** — a fixture whose failures have as bodies one model across several statuses, a
    model with a member named `error`, a union, a list and a primitive, beside a case with no body,
    a `4XX` range and a `default` beside a success, generates no error type and code that compiles
    on every target and passes `go vet`. Each response raises the class or sentinels of its status's
    kind and fault, a status outside the table its fault's, with its typed `Body` and `Headers`
    reachable through the accessors; a body that does not decode raises it with the bytes read and
    the decode error; a body past the bound stops there; an entry whose declared `Fault` contradicts
    its status raises that fault's class with its kind still stated; and cases whose contents
    disagree elect one content on every target, by the entries' classes and the compared media type
    ([one election](../plan/responses.md#one-election), [failures](../operations/errors.md)).
24. **T-24 · Streams** — each framing, under an `Item` and by media type where the framing admits
    both, with an element of each kind, decodes on every target. An operation offering a value and a
    stream gets a method for each, each sending its own `Accept`; a line under a content with no
    `Item` and a `string` root reaches the caller as text, where a declared `string` `Item` decodes
    as JSON; a list schema with no `Item` yields its `Elem`, one frame at a time, a `string` `Elem`
    decoding as JSON on a line and arriving as `data` on an SSE frame; an SSE element, a union of
    event shapes among them, reads the frame's fields under `FrameMembers` and the frame's `data`
    otherwise; a stream under a binding other than HTTP draws no `unframed-stream`; a media type
    only a compiler option names gets the one stream method, as `bytes`, with that warning; a
    primary `Default` that streams is `bidi` beside a `Send`; a `bytes` side lowers as a file, and
    a request's file body is a byte source whatever its `Type`; and each operation of the golden
    spec that streams gets a `StreamPlan` ([streams](../plan/streams.md),
    [streaming](../operations/streaming.md), [request shaping](../refine/requests.md)).
25. **T-25 · Emit-layer rules** — one check per rule of the [emit rules](../emit/rules.md). A
    template's context type holds no pointer, map, function or method, by reflection. A golden whose
    enum value, description and example hold `{{ .X }}`, a `$x` and a `*/` keeps each verbatim and
    compiles. Each function map is held to a literal list, and the lint refuses `call`. One worker
    and many give identical artifacts, and so do two runs under different `LC_ALL`, over a fixture
    whose sort order a collation would change. Two artifacts on one path, and two differing only by
    case, each draw the error. A template that fails the formatter draws a located error and no
    artifact, and a build with and without `-trimpath` give identical artifacts. A `Scope` matching
    nothing and a document with nothing to emit each draw `no-artifacts`. `..`, an absolute path, a
    sibling sharing the root's prefix and a symlink leaving the root are each refused.
26. **T-26 · Response headers** — an operation whose response declares headers of each kind, under
    each shape, gets a `WithResponse` method beside a plain method whose signature the headers do
    not change, and adding a first header or a further one is additive in the surface differ
    ([surface verification](../surface-verification.md)). A field is found whatever the case its
    name arrives in; a header that is absent, or whose value does not parse, leaves its field unset
    and the body returned, with `RawHeader` holding what arrived; and a paginated operation gets no
    such method ([headers](../plan/responses.md#headers), [return
    shapes](../operations/returns.md)).
27. **T-27 · Other responses** — on every target, an operation declaring a second response of the
    primary's type returns it as the primary; one with no body, and a `304`, give the nullable
    return and no error, with `WithResponse` stating the status, and a pager and a stream end at one
    with no error; a header only such a response declares is a field of `WithResponse`'s `Headers`;
    one of another type, and a body beside a `void` primary, reach the caller as `OtherResponse`
    with its typed `Body`, `Headers` and raw bytes, a body that does not decode as `BodyErr` and one
    past the bound stopped there, and neither fault, kind nor retry answers it. An exact `200` is
    primary over a `2XX` written first; an exact `202` beats a `default` catch-all; a `4XX` beside a
    primary `Default` is a failure; a `201` the document does not list reads as `Return`, beside a
    `default` catch-all too; a `200` whose body does not decode, an unlisted `204` under a `value`
    and a frame that does not decode reach the caller as `OtherResponse` with `BodyErr`, and nothing
    retries them; a `HEAD` reads no body; a poller's start reads each response by its `LROPlan`; an
    RPC operation's one response is its primary, leaving `Others` empty; and no golden operation's
    declared status is left to no entry ([which entry
    answers](../plan/responses.md#which-entry-answers), [return shapes](../operations/returns.md)).
28. **T-28 · Unresolved schemes** — on every target, an operation whose only option names a scheme of
    kind `unresolved` sends what the caller's `Authenticator` writes, and with none configured is
    refused unsent, the error naming the scheme, from a call, a pager's first page and a stream's
    open. An option beside it is taken in priority order, an option ANDing it with a declared scheme
    needs both, and the service's credentials are never sent in its place
    ([auth](../operations/auth.md#unresolved-schemes)).
29. **T-29 · Inclusive unions** — on every target, an `anyOf` over two open models that one value
    fits decodes that value into both and re-encodes it as an equal JSON value; a member the two
    write at different values is an encode error, and a union with neither set refuses to encode.
    An `anyOf` the plan proves disjoint, by JSON kind, by enum values and by a member one model
    requires and the other cannot hold, lowers as a closed sum and decodes each value into the one
    variant admitting it. A `null` that two nullable variants admit, or a `null` literal variant
    does, is held by the reference, as a nullable one's is. A value no variant admits takes the
    fallback on an open union and is a coded error on a closed one
    ([unions](../refine/unions.md#a-value-several-variants-admit),
    [TypePlan](../plan/types.md#variants-that-can-share-a-value)).
30. **T-30 · Plan identity** — one document under one policy builds plans of one `Version` across
    processes and runs, and the policy round-trips through JSON unchanged; changing any policy
    field, the `Revision`, or the document outside `sources` changes the `Version`, and changing
    only `sources` does not. A plan golden that changes at an unmoved `Revision` fails
    ([the plan's version](../plan/overview.md#the-plans-version)).
31. **T-31 · Envelopes** — under an envelope matched by `Members` and one matched by `Bases`, on
    every target, a method returns the member at `Value`, its `WithResponse` method holds the
    envelope whole, and a 2xx whose flag holds `Fails` is a failure with no fault or kind whose body
    decodes as `Failure`, even where the success type admits only `true`. A paginated operation's
    page is read whole, its flag still read. A body naming only some of the members, an operation
    `Except` lists and a failure body are read as they would be with no envelope, and the default
    policy unwraps nothing ([envelopes](../plan/responses.md#envelopes),
    [plan policy](../plan/policy.md#envelopes)).
32. **T-32 · Embedded policy and hints** — a mapping reading a key on the document, on an operation
    and on a parameter configures the document, that operation and that parameter's hint; a key on
    a node it cannot configure, a value of the wrong shape and a mapping naming a key of a reason
    other than `vendor_extension` are each reported, the entry staying in `Unmodeled`; the default
    mapping reads nothing, and `Disabled` reads nothing however the mapping is written
    ([embedded policy](../contract/embedded-policy.md)).
33. **T-33 · Client settings** — on every target, a service hint naming a path parameter makes it
    a client setting, which every operation declaring it in that wire form sends unless the call
    gives its own; a call with neither is refused unsent; an operation declaring it at another
    wire form keeps it as an input and is reported once; no parameter becomes a setting unless a
    hint names it, and the plan reports each candidate as an info
    ([client settings](../operations/client.md#client-settings)).
34. **T-34 · Effects** — a declared `Idempotency` of each kind beats the method, a `GET` with none
    is safe and idempotent, a `PUT` and a `DELETE` idempotent, a `POST` neither, each derived one
    marked `Inferred`; turning `MethodSemantics` off leaves every undeclared operation neither; and
    on every target a request that got no response is sent again for an idempotent operation and
    never for a `POST`, under the default `Retry.Connection`
    ([effects](../plan/policy.md#an-operations-effects-come-from-its-method)).
35. **T-35 · Open enums and overlapping oneOfs** — on every target, a closed enum a response carries
    decodes a value it does not list and re-encodes it as it came, its constants naming only the
    listed members, while one only ever sent stays strict and turning `OpenEnums` off restores
    strict decoding; a `oneOf` value two variants admit decodes into the first under `first` and is
    a coded error under `only` ([plan policy](../plan/policy.md#an-enum-a-client-reads-is-open),
    [unions](../refine/unions.md#variant-identity-and-recognition)).

36. **T-36 · Enums over every value type** — on every target, an enum of each primitive a
    `ValueType` can name, `bool`, `integer`, `number`, `decimal`, `date`, `uuid` and `bytes` among
    them, closed and open, encodes each member as its value type's JSON kind and decodes it back to
    the same constant, a number spelled with trailing zeros included; where the target's constants
    cannot hold the type, the member's value is reachable as the row's type, and no member is ever
    written as a string its value type does not admit
    ([a member on the wire](../refine/enums.md#a-member-on-the-wire)).
37. **T-37 · Shipped names** — on every target, a generation whose operation collides with nothing,
    then a revision adding one that collides with it, regenerated against the first generation's
    manifest, keeps the first symbol unchanged and gives the newcomer a suffix; the first
    generation is byte-identical in both declaration orders; a renamed `operationId` renames its
    symbol; and an ID that now names another entity, its correlation key differing, is not pinned
    ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)).
38. **T-38 · Manifest and integration** — a regeneration whose inputs all match writes nothing; a
    spec reformatted into the same IR is the same generation; a generated file edited outside its
    regions is reported and not overwritten, while one checked out with other line endings is not
    an edit; a region follows its entity into the file a rename moves it to, and one whose entity
    is gone is reported and its file kept; a file written once is never written again; a hint, an
    override and an overlay key matching nothing each draw `emitters/unmatched-key`, naming the
    entity of the same correlation key; a README's text outside its blocks and its frozen blocks
    survive byte for byte, and its table of contents lists the user's headings; and a manifest of
    an older `Schema` is migrated, one of a newer refused
    ([write / integrate](../write-integrate.md)).

