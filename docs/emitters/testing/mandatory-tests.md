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
8. **T-8 · Manifest ID-keying** — the entity→symbol map survives path/name churn.
9. **T-9 · Policy precedence** — declared `Retryable`/`Fault`/`Idempotency` override policy defaults
   at a document-wide setting and at an operation's, in every layer. A project's document-wide
   setting beats a per-operation one the document embeds, an operation's own setting beats its
   layer's document-wide one, an operation ID no operation has is reported, an embedded value a
   higher layer replaces, or a status a higher layer's retry list takes from an embedded one, draws
   an info naming both, a value a configuration writes outside a field's vocabulary or range, a
   `MaxAttempts` of 0 and a status both of one layer's retry lists name at one scope among them, is
   an error, an operation's giving way to its layer's document-wide value, and a status both
   resolved retry lists name stays only in the one a higher layer set, while one a layer moves into
   a list a higher layer replaces stays in the other
   ([layers](../contract/runtime-policy.md#layers-and-operations),
   [retries](../contract/runtime-policy.md#the-canonical-vocabulary)).
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
    in another unit compiles; two groups whose operations `AlsoMount` puts in each other's client
    compile, the types those operations reach in the shared unit; and every partition error and
    warning of [output units](../refine/output-units.md) has a fixture that draws it.
19. **T-19 · Reserved scopes** — a fixture naming its types, in the root and in a group's unit, and
    its properties, parameters, path parameters, enum members and the variants of a closed and of an
    inclusive union after every name each scope of [reserved names](../refine/reserved-names.md)
    reserves, after characters alone and after names that begin with a digit, and its groups after
    the standard-library packages its files import, after every units name and after each name a
    Kotlin build keeps at its root ([output units](../targets/kotlin/overview.md#output-units)),
    generates code that compiles on every target, with every template's declared names held to what
    it binds. On Kotlin a `clean` and a `wrapper` at the build's root leave every module's sources,
    no path the build writes, an artifact's or a class file's, has a Windows device name for a
    segment, and the Multiplatform framework's header gives each declaration the Swift name the
    naming policy chose, the target's own names unchanged ([Swift](../targets/kotlin/swift.md)).
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
    primary's type returns it as the primary, in the nullable form where only that response's type
    is nullable; one with no body, and a `304`, give the nullable return and no error, with
    `WithResponse` stating the status, and a pager and a stream end at one
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
    write at different values is an encode error, as is a member a caller adds to the open variant
    beside a closed one that cannot hold it, and a union with neither set refuses to encode. A
    `string` variant beside a `datetime_offset` one re-encodes a timestamp as the text it arrived
    as. A value one set variant holds within its bounds validates though another's bound fails, and
    a member one variant marks `Secret` is redacted in the union's rendering though the other
    declares it plainly. A union of two models that both hold a list of the union decodes a value
    nested 64 deep, each position once per type, and does so under a caller's own json/v2 options or
    `Json`, whose settings keep applying inside the variants. An `anyOf` the plan proves disjoint,
    by JSON kind, by enum values and by a member one model requires and the other cannot hold,
    lowers as a closed sum and decodes each value into the one variant admitting it, by trying each
    where the ladder tells none apart or names one that does not admit it, while a closed model with
    a catch-all governing that member separates nothing, nor does a `ClientOptional` member, a
    member compared under any name but its JSON one, or, where the union is read, a `writeOnly`
    member or one an open model declares at another type only `writeOnly`; an `any` primitive (`{}`)
    shares a kind with every variant, and `Inclusive` is the same in either order the unions are
    planned. An `anyOf` of `string` and `integer` that a query parameter carries is inclusive, and a
    mock reads `since=-10` into both. A union of two models a required member separates, sent in a
    `PATCH` body and read in a response, is inclusive in its patch shape and a closed sum where it
    is read, and adding the `PATCH` operation changes no other declaration; sent as a `PATCH` body's
    list, its elements keep the member and it stays closed. A required member no shape holds
    (`Visibility.None`) is required of no value, and of two tuple variants, one with two required
    positions and one with one and no `Rest`, each admits only its own length. A `null` that two
    nullable variants admit, a `null` literal variant does or a variant that is such a union does,
    is held by the reference, as a nullable one's is. A value no variant admits takes the fallback
    on an open union and is a coded error on a closed one
    ([unions](../refine/unions.md#a-value-several-variants-admit),
    [TypePlan](../plan/types.md#variants-that-can-share-a-value)).
30. **T-30 · Visibility projections** — on every target, a model holding a model, a union, a list
    and a map of models with a `readOnly` and a `writeOnly` member each, sent in a `POST`, a `PUT`
    and a `PATCH` body and read in a response, gets a declaration per shape for every type holding a
    projected one: `readOnly` members absent from each request shape and `writeOnly` ones from the
    response, a `PUT` body holding what `create` or `update` makes visible, and the `PATCH` body's
    members optional at every depth but inside the list, whose elements keep their required members.
    `ParameterVisibility` and `ReturnTypeVisibility` replace the default filter, a webhook's request
    takes `read`, two shapes whose projections coincide take one answer, and a model no operation
    reaches holds its required `readOnly` and `writeOnly` members and requires neither
    ([TypePlan](../plan/types.md#a-shape-reaches-what-it-holds)).
31. **T-31 · Plan identity** — one document under one policy builds plans of one `Version` across
    processes and runs, a plan golden holding it across builds, and the policy round-trips through
    JSON unchanged; changing any policy field or the document outside `sources` and `diagnostics`
    changes the `Version`, and changing only those, as reformatting the spec does, does not. A plan
    one build cached answers no other build. A configuration that leaves a field out reads the
    default of the version it states, and one stating no version, or none at all, is refused; a
    primary rule outside the vocabulary or listed twice draws `emitters/plan/policy-invalid` and is
    planned as if left out; and the zero `Policy` returns `void` from every operation
    ([the plan's version](../plan/overview.md#the-plans-version),
    [plan policy](../plan/policy.md#the-policy-is-data)).
32. **T-32 · Envelopes** — on every target, under an envelope matched by `Members`, one matched by
    `Bases`, a body whose type is a listed base among those it matches, and one with a nil `Value`:
    a method returns the member at `Value`, at the end of a nested path too, in the target's
    nullable form where a member on the path is optional or nullable, and the body whole under a nil
    `Value`, its flag still read. Its `WithResponse` method returns the envelope whole and typed,
    nullable where a `204` beside it has none and the target's top type where a `200` and a `202`
    are wrapped in two envelope types, and an operation returning no enveloped response keeps
    `Response`. Another 2xx whose member at `Value` differs from the primary's only in being
    optional is returned as the primary is, in the nullable form, and one of another type reaches an
    `OtherResponse` holding the member at `Value`. A flag of `0.0` succeeds where `Succeeds` lists
    `0`. A success whose flag holds a value `Succeeds` does not list is a failure with no fault or
    kind, retried by its response alone and holding that response's headers; its body decodes as
    the first of `Failures` its flag admits, JSend's `error` beside a `fail` among them, as untyped
    JSON where none does, and into `BodyErr` where it does not decode, and an error entry whose flag
    admits only `true` is never tried. A flag absent or null is a success. A paginated operation's
    page and a poller's monitor are read whole, their flag still read. A body naming only some of
    the members, a failure body and a webhook's response are read as they would be with no envelope,
    an operation one envelope's `Except` lists is read by the next envelope that matches it, and the
    default policy unwraps nothing. An envelope with no `Name`, a shared one or one holding a comma,
    neither `Bases` nor `Members`, neither `Value` nor `Flag`, or only one of `Flag` and `Succeeds`
    draws `emitters/plan/policy-invalid` and matches nothing; a body whose flag admits no value
    `Succeeds` lists, a string `success` under a boolean `Succeeds` among them, is read by the next
    envelope that matches it, or as with none, drawing `emitters/plan/envelope-flag-mismatch` once
    and only then; one no body matches, and an `Except` or `Bases` entry naming nothing, draw
    `emitters/plan/envelope-unused`. Each field a match sets is marked `Inferred` as `envelope:` and
    the envelope's `Name`; the docs emitter shows the member at `Value` of a response's example as
    what the method returns, and the mock sends the envelope whole; and the surface differ reports a
    body that gains the last name `Members` lists as a break
    ([envelopes](../plan/responses.md#envelopes), [plan policy](../plan/policy.md#envelopes),
    [return shapes](../operations/returns.md)).
33. **T-33 · Embedded policy and hints** — a mapping reading a key on the document, on a path item,
    on an operation and on a parameter configures the document, each of that path item's operations,
    that operation and that parameter's hint, a `hint` key at an OpenAPI document's root configures
    its one service, and one reading an `out_of_scope` entry reads it as a `vendor_extension` one;
    an operation's own key beats its path item's for one field, and for a status their retry lists
    both name, and two other keys setting one field set it from neither; a key on a node it cannot
    configure, a tag's key the document holds, a value of the wrong shape or outside its field's
    vocabulary or range, a group named by words rather than its ID, a conflict and a key the
    document holds under a reason the reader refuses are each reported under their codes, the entry
    staying in `Unmodeled` and an operation's key whose value is invalid, or which the reader
    refuses, leaving its path item's to set the field; and the default mapping, which is empty,
    reads nothing ([embedded policy](../contract/embedded-policy.md)).
34. **T-34 · Client settings** — on every target, a service hint naming a path parameter makes it a
    client setting, which every operation declaring it in that wire form sends unless the call gives
    its own; a call with neither is refused unsent; an operation declaring it at another wire form
    keeps it as an input and is reported once; a header setting fills a header two operations spell
    in different letter case; a hint naming a parameter no operation declares is reported, as is one
    at a location a setting cannot fill, such as `body`; a `host` label setting fills the host; no
    parameter becomes a setting unless a hint names it, and the plan reports every candidate in one
    info, the most used first ([client settings](../operations/client.md#client-settings)).
35. **T-35 · Effects** — a declared `Idempotency` of each kind beats the binding, a `GET` with none
    is safe and idempotent, a `PUT` and a `DELETE` idempotent, a `POST` neither, a `QUERY` safe and
    idempotent, a method `additionalOperations` declares as the method registry records it and one
    the registry does not list neither, a GraphQL `query` safe and idempotent and a `mutation`
    neither, each derived one marked `Inferred`; turning `ProtocolEffects` off leaves every
    undeclared operation neither; and on every target a request that got no response is sent again
    for an idempotent operation and never for a `POST`, under the default `Retry.WhenUncertain`
    ([effects](../plan/policy.md#an-operations-effects-come-from-its-binding)).
36. **T-36 · Open enums and overlapping oneOfs** — on every target, a closed enum a response carries
    decodes a value it does not list and re-encodes it as it came, its constants naming only the
    listed members, while one only ever sent stays strict and turning `OpenEnums` off restores
    strict decoding, one declaring a `FallbackMember` reads an unlisted value as that member, and an
    open `Flags` enum keeps a bit no member names in its flags form; an enum declared open is `Open`
    wherever it is used, with no `Inferred` mark. A `oneOf` of two string variants only their
    `pattern`s tell apart decodes each value into the variant whose pattern it meets, in either
    declaration order; a value both admit and meet decodes into the first, and under
    `StrictExclusive` is a coded error, as is one both admit and neither meets ([plan
    policy](../plan/policy.md#an-enum-a-client-reads-is-open),
    [unions](../refine/unions.md#variant-identity-and-recognition)).
37. **T-37 · Retries and redirects** — on every target, against the behavior server (T-14) and with
    each wait timed as T-13 times one, under the default policy: a 429 and a 503 are sent again for
    a `POST`; a 500, a 502, a 504 and a request that got no response are sent again for a `PUT` and
    never for a `POST`; a request whose body was a stream is not sent again after a 503; a
    `Retry-After` is waited for up to `Backoff.Max`, and one past it returns the response. A 307
    sends the method and body again, a 303 a `GET`, a 301 to a `DELETE` the `DELETE`, while a 302
    the operation declares, a 301 to a `POST`, a 307 whose body was a stream, a redirect from
    `https` to `http` and the redirect past `Redirects.Max` are returned as the response; a redirect
    to another origin carries none of the client's credentials ([runtime
    policy](../contract/runtime-policy.md#the-canonical-vocabulary)).
38. **T-38 · Enums over every value type** — on every target, an enum of each primitive a
    `ValueType` can name, `bool`, `integer`, `number`, `decimal`, `date`, `uuid` and `bytes` among
    them, closed and open, encodes each member with no `WireName` as its value type's JSON kind and
    decodes it back to the constant of the first member holding its value, a number spelled with
    trailing zeros included and a `string` enum repeating a value among them, and two members
    holding one value under two `WireName`s write the first's and read either as it, while two
    `uuid` members differing only in case decode to two, and a member sent in a query parameter and
    read from a response header travels as its wire form's text. Where the target's constants cannot
    hold the type, the member's value is reachable as the row's type, and a `datetime_offset` member
    the row's codec refuses, such as a leap second, fails only where its value is read. An `integer`
    member past the default row's range draws `emitters/<target>/unrepresentable-enum-member` and
    leaves the enum's form as it would be without it, and every member is written as a value JSON
    compares equal to the wire form of the first member holding its value ([a member on the
    wire](../refine/enums.md#a-member-on-the-wire)).
