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
6. **T-6 · Determinism / golden IR→artifact** — identical inputs, the prior manifest among them ⇒
   identical bytes; a scoped run equals the corresponding slice of a full run.
7. **T-7 · Diagnostic-not-silence** — every un-lowerable construct emits a coded diagnostic, never a
   silent `any` and never a panic, a fact outside M3's scope included ([what M3
   lowers](../milestones.md#what-m3-lowers)).
8. **T-8 · Manifest ID-keying** — the entity→symbol map survives a rename of what an ID is not
   built from, such as an `operationId`, and, through the correlation key, a renamed path variable.
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
   [retries](../contract/runtime-policy.md#the-canonical-vocabulary)). Every leaf of the record has
   a value it takes and, each an error naming the leaf's path and leaving the layers below to set
   it, a value of another JSON type, `null`, its zero where zero is no value, a value past each end
   of its range or outside its grammar, a duration with no unit and one in a unit the record lacks,
   a ratio with a fourth fraction digit, a sign or an exponent, `-0` among them, a `Template`
   holding a `{` or a `}` outside its two placeholders, a `Guards.Keys` entry that is empty or holds
   a space, a list naming an entry twice or holding more than it may, a `Backoff` or a `Kinds` row
   written in part and a `Backoff` whose `Initial` passes its `Max`; so are a member the record does
   not declare, a `Kinds` row keyed `0404` among them, a member named twice, an operation's entry
   writing a member that is the client's, a kind named for a fault, and two spellings of one kind
   that one layer alone writes, while a kind a lower layer spells in another letter case takes that
   spelling and draws an info, whatever order the rows are read in. A leaf written at its zero, an
   empty list among them, replaces the layer below where one left out does not, and a schedule of
   waits under a fractional `Multiplier` is the same number of milliseconds on every target ([what a
   layer writes](../contract/runtime-policy.md#what-a-layer-writes)).
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
    row under its key whole; an encoding with no row is typed as its value travels and reported once
    (T-51); a row naming an import `Imports` does not bind, an `Imports` key it does not use, or a
    Go `Codec` is an options error; and two rows that spell one name for two packages generate code
    that compiles in a file using both ([type table](../contract/type-table.md)).
18. **T-18 · Placement** — adding a type no operation reaches, an operation that reaches no existing
    type, or declarations past a unit's size limit moves no existing declaration to another unit,
    while an operation in another group moves to the shared unit exactly the types it makes a second
    group reach; under `SplitByGroup` with every group's operations failing with the runtime's
    failure value, and under `Partition` with a partition's model holding a runtime type, the units'
    imports form no cycle and build; with either rule on and with both off, the runtime's
    declarations are in its unit, which imports none, and on Kotlin, code compiling against the root
    project or any one module names the runtime's types its signatures use; groups whose names
    render alike, or as a name the units' scope reserves, or as a partition's but for letter case,
    get distinct units; a union with a variant in another unit compiles; two groups whose operations
    `AlsoMount` puts in each other's client compile, the types those operations reach and what a
    target derives from each of them for a caller to name, its request, headers and response headers
    types, in the shared unit, a caller passing one request value to both methods, and each group's
    unit holding its own row of the runtime's settings, which no package exports, while an operation
    `Remount` moves, and one mounted in a group and in its subgroup, keep what they derive in the
    one top-level group's unit, and with `SplitByGroup` off what an operation derives lands in the
    root; and every partition error and warning of [output units](../refine/output-units.md) has a
    fixture that draws it.
19. **T-19 · Reserved scopes** — a fixture naming its types, in the root and in a group's unit, and
    its properties, parameters, path parameters, enum members and the variants of a closed and of an
    inclusive union after every name each scope of [reserved names](../refine/reserved-names.md)
    reserves, after characters alone and after names that begin with a digit, two of its types in
    each unit, two variants of each union and two operations of one group, a paged and a streamed
    one among them, in names that differ in letter case alone, from each other and from a name the
    scope reserves, one of its types in each unit after each file a template writes there and one
    after the class kotlinc names for a `typealias`'s file (`MoneyKt` beside `Money`), and its
    groups after every units name, a keyword and `documentation` among them, after each name a
    Kotlin build keeps at its root ([output units](../targets/kotlin/overview.md#output-units)),
    after the standard-library packages its files import, after a predeclared identifier its files
    write (`error`) and after a name the generated code binds (`ctx`), two of them `com` and `Com`,
    whose suffix would make `com2`, and its types and groups after each Windows device name in two
    letter cases, after names whose last word `go/build` reads as a platform's or as `test`, and in
    pairs past ASCII that fold alike (`Straße` and `Strasse`, an `é` composed and decomposed, `ı`
    and `I`), generates code that compiles on every target, no two of its paths, an artifact's or a
    class file's, folding alike, the Go file holding two such types byte-identical in both
    declaration orders, with every template's declared names held to what it binds, and on Kotlin
    the table of default-imported classes to each name that resolves, on each platform the target
    builds for, in a file importing nothing. On Go `go list` lists every file the target writes
    among a package's `GoFiles`, none among the files it ignores or builds for tests alone, on each
    port `go tool dist list` gives, and `golang.org/x/mod/zip` zips the module, so every file and
    import path is one the module proxy takes ([Go](../targets/go/naming.md#file-names), [package
    directories](../targets/go/naming.md#package-directories)). On Kotlin a root project the options
    name `shared` or `runtime` is refused, a caller's file importing each unit with `*` compiles its
    use of the classes the default imports give it, `System.currentTimeMillis()` beside a type the
    spec names `System`, and `Error("boom")` there is still `kotlin.Error` beside a type `Error`
    whose constructor takes a string, a `clean` and a `wrapper` at the build's root leave every
    module's sources, no path the build writes, an artifact's or a class file's, has a Windows
    device name for a segment, and the Multiplatform framework links, its header holding none of
    what only generated code calls and giving each declaration the Swift name the naming policy
    chose, none of them a type Swift's standard library or Foundation declares or a module's name,
    so a Swift file that imports the framework and Foundation compiles its uses of both and
    qualifies a name by either module ([Swift](../targets/kotlin/swift.md)).
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
    answer, once the IR has one, wins; a closed union only requests carry is `Open` as one a
    response carries is, and one the IR marks `InputOnly` stays closed ([plan
    policy](../plan/policy.md), [unions](../refine/unions.md)).
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
    stream gets a method for each, each sending its own `Accept`, and so does one whose stream a
    `Condition` selects, the stream's method sending the condition's value and the other never
    sending it; a frame whose data is the `Terminator` of a side the client reads ends the stream
    undecoded, a side it writes sends it last, and a `bytes` side draws `terminator-unframed`; a
    line under a content with no `Item` and a `string` root reaches the caller as text, where a
    declared `string` `Item` decodes as JSON; a list schema with no `Item` yields its `Elem`, one
    frame at a time, a `string` `Elem` decoding as JSON on a line and arriving as `data` on an SSE
    frame; an SSE element, a union of event shapes among them, reads the frame's fields under
    `FrameMembers` and the frame's `data` otherwise; an `sse` reader ends a line at a CR, an LF and
    a CRLF, skips a comment line and yields nothing for a frame with no `data` line; over HTTP a
    stream whose `Initial` is set reads its first frame as an element and sends the initial
    request's members as the binding places them; a stream under a binding other than HTTP draws no
    `unframed-stream`; a media type only a compiler option names gets the one stream method, as
    `bytes`, with that warning; a primary `Default` that streams is `bidi` beside a `Send`; a
    `bytes` side lowers as a file, and a request's file body is a byte source whatever its `Type`;
    and each operation of the golden spec that streams gets a `StreamPlan`
    ([streams](../plan/streams.md), [streaming](../operations/streaming.md), [request
    shaping](../refine/requests.md)).
25. **T-25 · Emit-layer rules** — one check per rule of the [emit rules](../emit/rules.md). A
    template's context type holds no pointer, map, function or method, by reflection. A golden whose
    enum value, description and example hold `{{ .X }}`, a `$x` and a `*/` keeps each verbatim and
    compiles. Each function map is held to a literal list, and the lint refuses `call`. One worker
    and many give identical artifacts, and so do two runs under different `LC_ALL`, over a fixture
    whose sort order a collation would change. Two artifacts on one path, and two whose paths fold
    alike, each draw the error, and a segment of 86 three-byte characters, 258 bytes, where one of
    85 draws nothing, a Windows device name before a first dot in two letter cases and before the
    first of two (`aux.v1.go`), a character Windows refuses and a trailing dot or space each draw
    `artifact-path-unportable`. A template that fails the formatter draws a located error and no
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
    merge-patch body and read in a response, is inclusive in its patch shape and a closed sum where
    it is read, and adding the `PATCH` operation changes no other declaration; sent as that body's
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
    and only then; one no body matches draws `emitters/plan/envelope-unused`, and an `Except` or
    `Bases` entry naming nothing `emitters/unmatched-key`. Each field a match sets is marked
    `Inferred` as `envelope:` and the envelope's `Name`; the docs emitter shows the member at
    `Value` of a response's example as what the method returns, and the mock sends the envelope
    whole; and the surface differ reports a body that gains the last name `Members` lists as a break
    ([envelopes](../plan/responses.md#envelopes), [plan policy](../plan/policy.md#envelopes),
    [return shapes](../operations/returns.md)).
33. **T-33 · Embedded policy and hints** — a mapping reading a key on the document, on a path item,
    on an operation and on a parameter configures the document, each of that path item's operations,
    that operation and that parameter's hint, a `hint` key at an OpenAPI document's root configures
    its one service, and one reading an `out_of_scope` entry reads it as a `vendor_extension` one; a
    path item's key read under its own spelling with `Outer` set, a mapping naming the operation's
    spelling alone reading none of it and none of a responses object's key; an operation's own key
    beats its path item's for one leaf, and for a status their retry lists both name, a key mapped
    to `["retry"]` and one to `["retry", "maxAttempts"]` meeting at that leaf alone, two keys of one
    node whose values for a leaf are equal setting it with no report, status lists in two orders
    among them, and two other keys setting one leaf set it from neither, an operation's conflicting
    keys leaving its path item's key to set it and a path item's conflicting keys its operation's
    own key or the layer's document-wide value, under every order of the keys and of the mapping's
    entries; a key on a node it cannot configure, a tag's key the document holds, a value of the
    wrong shape or outside its field's vocabulary or range, a duration written as a bare number,
    which is read in no unit, a group named by words rather than its ID, a conflict and a key the
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
    listed members, one only requests carry is open the same, turning `OpenEnums` off restores
    strict decoding, one with no members stays closed under either setting and refuses every value,
    a flags one refusing 0 too, an optional member of it decoding only where it is absent, and
    refuses to encode every value a target can hand it, so that Go fails to encode a struct
    requiring one left at its zero value and leaves out an optional one unset, one declaring a
    `FallbackMember` reads an unlisted value as that member, and an open `Flags` enum keeps a bit no
    member names in its flags form; an enum declared open is `Open` wherever it is used, with no
    `Inferred` mark. A `oneOf` of two string variants only their `pattern`s tell apart decodes each
    value into the variant whose pattern it meets, in either declaration order; a value both admit
    and meet decodes into the first, and under `StrictExclusive` is a coded error, as is one both
    admit and neither meets ([plan
    policy](../plan/policy.md#an-enum-keeps-a-value-it-does-not-list),
    [unions](../refine/unions.md#variant-identity-and-recognition)).
37. **T-37 · Retries and redirects** — on every target, against the behavior server (T-14) and with
    each wait timed as T-13 times one, under the default policy: a 429 and a 503 are sent again for
    a `POST`; a 500, a 502, a 504 and a request that got no response are sent again for a `PUT` and
    never for a `POST`; a request whose body was a stream is not sent again after a 503; a
    `Retry-After` is waited for up to the `Max` of the backoff that applies, `Throttled`'s after a
    429 no error case marks and `Backoff`'s after a 503, one past it returns the response, one
    shorter than the wait the backoff drew waits that, a `Retry-After: 0` under a `Jitter` of 1
    waiting the wait drawn and not the schedule's, and one that is no count of seconds and no date
    is read as absent. Under a `MaxAttempts` of 5 and a `Jitter` of 0, a call failing 503, 503, 429,
    429 waits 500ms, 1s, 4s and 8s, the step being the attempt's number. A stream held open with no
    byte for `Timeout.Idle` fails its reader with a timeout and one sent a comment line within it
    reads on, a writer whose bytes the server stops taking fails the same way, and neither is ended
    by `Timeout.Request`; a walk refuses the fetch past `Pagination.MaxPages` and a poller the poll
    past `Polling.MaxPolls`, each failing by naming its member, the poller pollable again. A 307
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
39. **T-39 · Shipped names** — on every target, a generation whose operation collides with nothing,
    then a revision adding one that collides with it and whose ID sorts before it, so that ID order
    alone would hand the newcomer the name, regenerated against the first generation's manifest,
    keeps the first symbol unchanged and gives the newcomer a suffix; the first generation is
    byte-identical in both declaration orders; a renamed `operationId` renames its symbol, and a
    renamed path variable keeps it, a suffix included, and so do the symbols derived from it; an ID
    that now names another entity, its correlation key differing, is not pinned; an entity with no
    key is never pinned, and a removed and an added one are not matched; a prior manifest of another
    `Keys` pins nothing; an operation moved into a group whose client shipped its name takes a
    suffix there, in the root unit as under a split by group, the incumbent's symbol unchanged; a
    suffix that would make a name the scope reserves, as two groups rendering `com` would on Kotlin,
    is passed over; a type added in a name differing from a shipped one's in letter case alone, its
    ID sorting first, leaves the incumbent's name as it shipped and on Kotlin takes a suffix; names
    whose words differ only at a break letter case cannot mark, properties `v1.2` beside `v12`,
    `a-日本` beside `a日本`, `count-ℤ` beside `countℤ` and `a-ß` beside `aß`, an enum's members `1.0`
    and `10`, and on Go the constants of `V1`'s member `2` and `V`'s `12`, render apart with no
    suffix, a letter beside a digit taking no `_`, as a schema `10_gbps_to_100_gbps` renders
    `Type10GbpsTo100Gbps`, and on Multiplatform the entries of `1.0` and `10` export to Swift as
    `value1_0` and `value10`, while of `V12` and `v12`, one name, the second takes its suffix after
    `_` (Go's `V12_2`); names of different words that render alike, `iOSVersion` beside `IOSVersion`
    under a policy reading spellings, `user_id` beside `user_i_d` under an acronym `ID`, `ıd` beside
    `id`, a property `1000` beside one spelled `field_1000`, on Go a decomposed `café` beside a
    property spelled `cafeu0301`, two names with no words, `=` beside `==`, and in a scope compared
    folded two that render apart, types `Straße` and `Strasse` on Kotlin, each draw one
    `emitters/naming/collision` on a target that gives one a suffix, naming both and what each was
    given, two of one neutral name drawing none; and on Multiplatform a type added in another unit,
    its ID sorting first, leaves an incumbent's Objective-C name as it shipped
    ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped),
    [breaks](../refine/naming.md#a-break-letter-case-cannot-mark-takes-an-underscore)).
40. **T-40 · Manifest and integration** — a regeneration whose inputs all match writes nothing; a
    spec reformatted, one drawing a warning among them, is the same generation; a generated file
    edited outside its regions is reported and neither overwritten nor pruned, by the next
    generation too, an entity a rename moves out of it written in its new place and its regions left
    in the kept file, written once, while one edited only inside a region, or checked out with other
    line endings, is not an edit; a region follows its entity into the file a rename moves it to,
    one the declaration enclosed still inside it, and one whose entity is gone is reported and its
    file kept; a scoped run's manifest keeps every entry it left out; the staleness check lists no
    entity a correlation key carried across a renamed path variable; a file written once is never
    written again, even after its user deletes it, nor reported when its user edits it, and one
    marked `@morphic:ignore-file` is never written, pruned or reported; a hint's key, a
    runtime-policy key, an envelope's `Except` entry and a `Remount` group naming nothing in the
    document each draw `emitters/unmatched-key` once, naming the entity of the same correlation key,
    and no other diagnostic; a README's text outside its blocks and its frozen blocks survive byte
    for byte, an edit outside its blocks draws nothing and one inside an unfrozen block is replaced
    and draws a warning, a block new to the generation lands beside its neighbours in the generated
    order, the first of them included, a block whose ID holds `-->` keeps its markers whole, one
    frozen before its entity's path variable is renamed gains no twin, and its table of contents
    lists the user's headings; and a manifest of an older `Schema` is migrated, one of a newer
    refused with nothing written ([write / integrate](../write-integrate.md)).
41. **T-41 · Docs** — the docs emitter renders every answer the plan lists, `Return`, `Others` and
    `Errors` with the service's `CommonErrors` among them, the `Default` once wherever the plan
    places it, and every content, the primary response and the elected content first; a table for
    each projection a type's `TypePlan.Shapes` gives it, marking the members it requires, a
    write-only property in no read shape's, a model created by one operation and patched by another
    given a table for each, a model whose members and those of the models it holds carry no
    visibility marker one table for every shape, a patch's apart only where it or a model it holds
    requires a member or gives one a default, a patch table showing no default, and a type no shape
    reaches one unprojected table anchored by its model alone; each `TypePlan.Undeclared` name a row
    with no anchor, required save in a patch table, each member anchored once, where the model
    declaring it is rendered and in the first of its tables that holds it, each table anchored too,
    once for each shape it shows, through the page's ID-keyed resolution so a member named `create`
    beside a create table keeps its own anchor, two tables on one page never sharing one and the
    collision check reading anchors within a page, and a patch body linking to its patch table, a
    member no table of its declaring model holds anchored in the first table of a model whose
    flattening holds it, an inherited one linked there and an anonymous model composed in place no
    table of its own; a model narrowing a union a table for each entry of its `TypePlan.Composed` in
    each shape, each holding its variant's members beside the model's as the entry's flattening
    takes them, so `rulesets_ResponseRule`'s tables mark required the five members it requires and
    its union's variants do not, each naming the variants its `Over` lists and the tags routing to
    it and anchored from the model, that `Over` and its shape, a value of it redacted at each member
    the `Redacted` of an entry admitting it lists, an inclusive one at those of every entry
    admitting it, and whole where a variant its `Over` lists is sensitive, a union of unions a table
    for each entry it expands to, each member anchored once, one the model declares in the first
    table holding it and one a variant declares where that variant is rendered, an anonymous union
    only the model's composition reaches giving its anonymous variants no table of their own and
    naming them unlinked, a model `TypePlan.Open` marks documented as holding variants the document
    does not list, a model adding nothing to its union showing that union and no table, and one the
    plan composes no entry for listing its own members beside its union, linked; each example
    through the shape it is shown in, a write-only member in no response's; a placeholder for every
    redacted value, on pages, in agent documents and in samples, a sample's written by its SDK
    target; and raw HTML in a description rendered through the allowlist on pages and in agent
    documents alike, a `<p>` and a `<code>` as themselves, a `<br/>` as a line break, the
    description's inline raw HTML and HTML blocks read by an HTML tokenizer against its own stack,
    never in raw-text mode, so `<p>Use the <title> field</p>` and `<p>a <title/> <b>b</b></p>` keep
    their paragraph, a self-closing `<p/>` read as its start tag, every listed tag written from its
    name, a description's `<table>`, nested or not, rendered as text, and each page, parsed as a
    browser parses it, keeping its own elements, cells and rows around every description of a fixed
    hostile corpus, text decoded and written escaped, a `<p>`-led description among them, an element
    left open closed at its end and an end tag it never opened, such as `</td>`, rendered as text,
    `<key>` keeping its place, `<ul><li>a<li>b</ul>` closing the first item implicitly, an end tag
    matching an element below the top closing it and those above it, `<ul><li><p>a<li>b</ul>` two
    items, a listed tag's unlisted attribute such as `target` dropped, a GitHub-style table kept as
    preformatted text, a refused `a` its text and a refused `img` its `alt`, else its tag as text,
    as the golden spec's `<img src="cid:logo">` is, an `&#106;avascript:` destination refused once
    decoded and an image's `mailto:` source refused, and a `<td>` or `<li>` with no parent on the
    description's stack rendered as text so a description in a cell adds no cell to the page's
    table, `Example.Description` and `Link.Description` read as CommonMark like `Docs.Description`,
    and an attribute outside the list, a comment and a placeholder such as `<key>` as text, a
    summary escaped as text; a link from spec text written only under `http`, `https` or `mailto`
    or, in a description, as a relative path resolved against the configured base, a relative URL in
    a field of its own rendered as text with the warning, and a `javascript:` destination, a
    fragment alone and a relative path with no base each rendered as the link's text with the
    warning `emitters/docs/link-unwritten`, in a description's links and images, `ExternalDocs` and
    every other URL the document gives alike, and a `TermsOfService` that is text rendered as text.
    Every channel, message, auth scheme, an `unresolved` one included, and declared type but a
    primitive has a page, and so does an anonymous model, union or enum more than one reference
    reaches through anonymous containers, so no table is rendered in two places and a type spelled
    where it is used takes no anchor; a decorator has no page, table or anchor of its own, renders
    and links as its `Alias` and folds its annotations onto the position, so a 3.0 decorator and the
    3.1 `$ref` render alike; a recursive type is linked by its ID and never inlined, and an entity
    left without a route is the error `emitters/docs/route-missing`, every service and group routed,
    a property no model declares, a header's or a resource's, a row of its holder with no anchor and
    no diagnostic, and so is a write-only member of a model only responses reach, which no table
    holds. Two slugs colliding in one template's scope are told apart by ID, also when they differ
    only by letter case, and two entities resolving to one URL, agent documents among them, are the
    error `emitters/docs/route-collision` naming both, also when the URL templates are changed;
    every link, one to a page no entity owns among them, and every `Docs.Refs` span resolves through
    the route plan, a span inside raw HTML, a link, an `a` the description opened or a code span,
    and one naming a type no route reaches, rendering as its text with no `route-missing`; a version
    switcher, listing only the labels `Routes` keys in `Document.Versions` order, links one entity
    across snapshots, a model's page and never one of its tables, the page holding an anonymous
    model's table where a version gives it no page, from the tables `Router` returns, marks the
    run's `Snapshot` current, and an entity a version lacks to that version's page saying so; two
    snapshots' pages of one entity lie under their labels' directories, neither run overwriting or
    pruning the other's, all of them one generation whose manifest holds each run's under its label,
    a dropped label's and a renamed slug's files pruned, the `""` snapshot written at the root and a
    label that is no single path segment `engine/snapshot-label-invalid`, `.` and `..` among them,
    labels `v1`, `V1` and `V1-2`, in that order, writing under `v1`, `V1-2` and `V1-2-2`, labels
    `v1-2`, `v1` and `V1` under `v1-2`, `v1` and `V1-3`, and `Straße` and `STRASSE` under `Straße`
    and `STRASSE-2`, the switcher showing `V1` and linking `V1-2`, and a document of `V1` alone
    under `V1`, a label that is a Windows device name before a first dot, one holding a character
    Windows refuses, as a `:` is, or ending in a dot or a space, and two of 255 bytes that fold
    alike, the second's directory passing the limit, each drawing `engine/snapshot-label-invalid`
    before any target runs, naming the label and the rule it breaks, a label with `#` or `?`
    percent-encoded in links, a target without `CrossVersion` named for its artifacts on a sliced
    document writing the one snapshot T-69 says, run for samples alone on each other, and the last
    label's run taking a prior's `""` entry when the document gains versions; only the run of the
    last label `Routes` keys writes the site-scope `llms.txt`, at the output root, one routed under
    a label's directory refused as a collision, neither pruned nor taken for an edit when another
    label becomes last, the generation's manifest a `Generations`, and the `""` snapshot's run
    writes one index for its site and version; a target gaining `CrossVersion` reads its prior
    `Manifest` as the `""` entry and one losing it reads the entry holding the spanning files; a run
    whose context is done after `Router` returns fails; and the registry refuses a target setting
    `CrossVersion` without implementing `Router`. A CLI's help keeps a placeholder, a comment and a
    link's URL as text, keeps line feeds and tabs and drops every other control character. A body
    narrowing a union gives the flags of each of its entries, those of two entries excluding each
    other, and never echoes one whose member an entry's `Redacted` lists. A nested group's route
    joins its groups' slugs. Agent documents and `llms.txt`, at site, service, version and target
    scope, share the pages' URLs, escape spec text, write each description back out within its item,
    an unclosed fence closed there and a reference definition reaching no other, mark an operation
    safe or idempotent from `OpPlan.Effects`, and warn of destruction only where the `Destructive`
    hint is set. The docs target decodes the `Extra` members it defines strictly, reporting a wrong
    shape as `emitters/docs/extra-invalid` and coercing nothing, and shows a code sample the
    document declares as authored beside the SDK targets' samples, a language that is no single word
    left off its fence. A code fence in an agent document is longer than any backtick run it holds.
    A sample comes from each SDK target, for each operation its callers call, in each language its
    callers write, Kotlin and Swift from a Multiplatform one, a missing one
    `emitters/<target>/sample-unrendered`, a target run under a `Scope` still returning one for
    every operation, and a target whose generation writes nothing still returns its samples; a
    sample's values are the `Input` of its operation's first success scenario, declared or else
    assembled, one ending in no `Error`, a bodiless one among them; a run made for samples alone
    reports what any run of its target reports; an operation the API starts, a webhook, a callback
    or a message the client receives, has no sample and no diagnostic; a docs target run alone shows
    the samples of the targets its options name, which the engine runs for it with the configuration
    and prior manifest each would run with, writing none of their artifacts the run does not name
    and running one the run does name once on each snapshot, with its artifacts on the one T-69
    says, one with no configuration refused; an operation with no success scenario has no sample,
    drawing no diagnostic where `Assemble` off chose that and `sample-unrendered` where a required
    input held no example; a named target the registry lacks, without `WritesSamples` or that reads
    samples, and a `ReadsSamples` target whose options do not implement `SampleReader`, are
    `engine/sample-target-invalid`; and a docs generation whose samples, routes or snapshot label
    changed, its own inputs unchanged, is a new generation ([docs and mocks](../docs-and-mock.md)).
42. **T-42 · One set of examples** — a declared example is used everywhere and labelled authored,
    one holding only an `ExternalURL` shown as a link and held by no set, and one its type and
    constraints refuse, visibility set aside, is `emitters/plan/example-refused` and left out
    everywhere, an open enum admitting a value it does not list; shown through a shape, through
    `plan.Shown` alone, an example drops the members the shape does not hold and fills each it
    requires and lacks from that member's set, marked `Inferred`, and fills nothing with `Assemble`
    off, so a response-form example serves a create shape and a patch body's may leave out a
    required member, one it cannot complete, or a `Strict` union's value two variants admit once
    shown in the shape, giving way to the set's next, a union value several variants admit refused
    by `Build` where the union is `Strict` and elsewhere shown by `Shown` through all of them, a key
    the catch-all admits kept, an `Undeclared` name filled from its catch-all's set and a filled
    example labelled generated; a property or a parameter declaring none and adding no constraint
    takes its type's set, an enum member its own value, an operation declaring no success scenario,
    where every required input's set holds a value, one assembled, each value shown through the
    shape its operation sends it in, from its required parameters' sets, its optional parameters'
    declared ones, its body's, required or not, and its return content's, after those it declares,
    always `Inferred`, its `Input` keyed by `ParamID` with the body under `body`, and none with
    `Assemble` off, a position whose type is a decorator its own examples, else the nearest
    decorator's, else, adding no constraint, its `Alias`'s set, and a content's, a variant's or a
    message's set, read through its one plan function alone, which reports nothing since `Build`
    reports each refused example once, is its own declared examples, else its type's, a message's
    those of its payload's contents paired with its headers' set, that pairing `Inferred` and off
    with `Assemble`, which the docs, the samples and the mock read a body's examples from. A model
    narrowing a union is checked and shown as that union over its `TypePlan.Composed` entries, a
    value no entry admits refused and one shown through each entry admitting it, filled from what
    that entry requires in the shape, and a model adding nothing to its union as that union. Where
    none applies, the value synthesized is valid against its constraints and its type's
    validation-only constructs, as `browser-rendering`'s body holding `url` or `html` and never
    both, a value failing them left out and reported and a declared one refused, marked `Inferred`,
    identical across regenerations and in both declaration orders, seeded from its ID where no
    correlation key identifies it, a choice on a recurrence or past `MaxDepth` taking the first
    variant or member from which a value ends, so a required recursive union ends, and the same in
    the docs, the samples, an SDK's tests and the mock. A synthesized model holds for each member
    the first value, of its own set and then of the sets of the declarations its flattening
    overrides, that every declaration of it admits, else its own set's first, so a member a later
    conjunct redeclares as a plain string keeps the value an earlier one pins and a failure schema's
    `success: false` stands over the success shape's `true`, a model narrowing a union a value of
    one of its entries, chosen as a variant is and the same in both declaration orders, so
    `rulesets_ResponseRule` has one carrying the tag routing to its variant, and a model adding
    nothing to its union its union's set where it adds no constraint to it, else a value of one of
    the union's variants, and a list its set's values in turn, distinct where `uniqueItems` or a
    map's keys
    require it, a `uniqueItems` list left below its minimum unsynthesizable, a declared one whole
    and a synthesized one cut to the depth the bound leaves, so an optional recursive member ends
    and a required `Nullable` one ends with `null`; the bounds cut only optional members and the
    entries of a list or map past the larger of `MaxItems` and its minimum, or past its minimum
    deeper than `MaxDepth` or on a recurrence of its type, so a model requiring a list of itself and
    a recursion through no model, as `cws_JSONValue`'s, end, so a required member nested deeper than
    `MaxDepth` and a `minItems` or `minProps` above `MaxItems` are met; one the synthesizer cannot
    make valid, a required cycle among them, or holding more than `plan.MaxExampleValues` values is
    `emitters/plan/example-unsynthesized` and left out; with `Synthesize` and `Assemble` off, no
    entry of `Plan.Examples` is `Inferred`, and with `Synthesize` off a response content left with
    none is `emitters/mock/example-missing`, never a body it invents; and a negative bound is
    `emitters/plan/policy-invalid` ([one set of examples](../docs-and-mock.md#one-set-of-examples)).
43. **T-43 · Redaction** — on every target, a value redacted through `Property.Secret`, through
    `Parameter.Secret` and a response header's `Secret`, through a sensitive component and a type
    whose `Base` reaches one, through a list's sensitive element and a map's sensitive key and
    value, through a union's sensitive variant, and through a member a later conjunct redeclares
    without `Secret`, shows as `[REDACTED]` in every string form of each type holding it, held by
    value or by reference, in Go under each `fmt` verb that asks a method, `%d` among them, a
    union's and its variant wrapper's among them, and in a sensitive type's own where the target
    declares one, an enum's among them, in every logging event, one a structured logger writes as
    JSON included, and in every error message, a transport failure's that quotes a redacted path
    segment or query value included, a collection as the marker alone and a map whose values alone
    are redacted with its keys shown, and goes out and comes back on the wire byte for byte; a raw
    body holding one is never logged or printed ([redaction](../refine/redaction.md)).
44. **T-44 · Stability** — an operation's own tier, its nearest group's when it states none, and
    none when no group states one, each resolved once in `OpPlan.Stability`; an `alpha` and a `beta`
    operation's documentation names its tier and what it means on every target, an unstated one
    names none and no badge reads it as `stable`, and each target's opt-in requirement is held where
    it has one, on each method the operation lowers to, its `WithResponse` method among them
    ([stability](../operations/stability.md)).
45. **T-45 · Page walks** — on every target, a pager ends at the first page meeting each condition
    `PagePlan.Stop` lists, alone and together, `short` against the limit the call sent and `total`
    against both totals; a condition the page gives nothing to read for ends nothing; and whatever
    `Stop` lists, the walk ends at a page giving no continuation under each strategy reading one,
    `link_header` among them, a link resolving to its own page's URL, an empty one and one spelt
    with other letter case in its host or its percent-encoding among them, giving none, and a cursor
    or token page handing back the value its request sent giving none, and at a page holding no
    items under `offset` or `page`, which is where an empty `Stop` ends it; each next request is the
    last with the cursor or token read at `CursorIn`, the offset plus the items read, or the number
    plus one, the first counted from a declared default, unmarked, else `plan.Policy.FirstPage`,
    marked under `Page.FirstPage` in `OpPlan.Inferred`, a declared minimum of 0 beside a default of
    1 starting at 1, and a string input sent the number's decimal text, and with that unset an
    operation whose input declares no default has no pager and draws
    `emitters/plan/first-page-unknown`; and a link is a `GET` of it, on a `POST` operation too,
    resolved against its page's URL, sending no body and each header input again, carrying
    credentials to another origin only as a redirect would
    ([pagination](../operations/pagination.md#what-a-pager-sends-next), [plan
    policy](../plan/policy.md#a-page-walks-first-page)). A pager that follows a cursor equal to the
    one it sent, or counts from a minimum, fails it.
46. **T-46 · A model composing a union** — on every target, a model composing a discriminated union
    by `$ref` beside a property of its own decodes a payload of each variant into the variant
    composed over it, the model's property beside the variant's members, and encodes it back byte
    for byte, each tag and the default routing as they do where the same union is written beside the
    model's `allOf` and the compiler distributes it; so do an undiscriminated union as a `Base`, a
    union one of whose variants is a union, a union a subtype composes as a `Mixin`, under its
    base's discriminator, and a union the model reaches through a model it composes. A variant's
    `readOnly` member is in no create or patch shape of the model's, and each variant composed there
    requires what its entry's `Required` lists and redacts what its entry's `Redacted` lists, so
    each of `rulesets_ResponseRule`'s variants requires the five members the model requires beside
    those its union's variant does, and a member only one of the union's variants redacts is
    redacted in the variant composed over it alone. Adding a variant to the union renames no variant
    composed there and no union nesting entries, each keyed by the model and its `Over`, or the
    start of `Over` its entries share. Read by a client under `OpenUnions`, such a model keeps a
    variant its union does not list, and one whose variant holds the model compiles, the union
    breaking the cycle. Each target declares one variant for each entry of the model's
    `TypePlan.Composed` and no other, entries sharing the start of their `Over` nested as a union of
    them, and a model composing its union through an alias scalar, as a 3.1 `$ref` beside a
    `description` hoists, narrows it as one naming the union does. An entry requiring a member that
    the model and a variant it is over pin to finite sets sharing no value is left out: `Item:
    {allOf: [Leaf, Node]}` over a `Node` of `Group` and `Leaf`, each pinning `kind`, declares the
    one variant over `Leaf`, still as a union, routes the tag `group` to no variant and has no
    table, flag or example over `Group`; an entry is kept where the member the two pin apart is one
    its `Required` does not list; a model every entry of which is such keeps them all, each member
    as its flattening takes it; and a variant that pins a member both ways itself keeps its entry
    under a model pinning it as the variant's later declaration does, `SelfBody: {allOf: [Env,
    Failure]}` keeping its entry over `ErrBody: {allOf: [OkB, Failure]}`, since neither side reads a
    declaration its own flattening overrides. A model its union lists is that variant, a model the
    union's sum holds. A model adding nothing to its union aliases it, and one narrowing two unions,
    one whose union has a variant composing it or reaches itself through its variants, one over a
    key-tagged, enveloped or index-tagged union, and one whose entries, with what their walks read,
    would pass `plan.MaxComposedSize`, among them one holding few members over a long composition
    and one over unions nested deep, each beside one leaf, which `Build` reports as
    `emitters/plan/composed-limit`, each draw a diagnostic and degrade
    ([inheritance](../refine/models.md#inheritance)).
47. **T-47 · Values as text** — on every target, under T-12's interception, a value of each
    primitive sent in a path, a query, a header, a cookie, a form field and a `text/plain` body is
    the text [values as text](../refine/text.md) gives it: a `float64` 1 as `1` and 10²¹ in plain
    notation, a `float32` `0.1` as `0.1`, a `decimal` `0.10` as `0.1`, a `datetime_offset` at a
    whole minute with its seconds, at a zero offset with `Z` and at `+02:00` in a query as
    `%2B02%3A00`, a `uuid` given in upper case as lower case, `bytes` as padded base64
    percent-encoded in a path and as it is in a header, a space as `%20`, a query parameter named
    `a<b` as `a%3Cb`, a `,` in a member of an unexploded query list as `%2C`, an exploded `form`
    list in a cookie as `p=a; p=b`, `a/b?c#d` and `a//b` in a path under `AllowReserved` as
    `a/b%3Fc%23d` and `a//b`, a map's entries and a `Prefix` map's fields in their keys' order, the
    map's entry, and a `form`-exploded map's, giving way to a field another binding writes, a string
    under `ContentType` `application/json` with its quotes, a `text/plain` body as its UTF-8 octets
    with no byte-order mark, and a scalar's class and an `enum class` entry as their wire forms,
    never their `toString()`. An optional query input holding null or an empty list is sent as an
    absent one, a null member of a list is left out, and a path parameter or a `host` label holding
    null or an empty string, a path parameter holding `..`, or `/` alone under `AllowReserved`, an
    `event_t` of `1.5` in `{event_t}.{event_n}`, a required query list holding no member, a header
    holding a CR LF, a `\u0001`, an `é` or a trailing space, a header list holding an empty string,
    a header `Prefix` map holding keys `A` and `a`, a `text/plain` request whose media type names
    `charset=iso-8859-1`, a NaN, a year past 9999, an offset with seconds and a `decimal`
    `1E+999999999` each fail before anything is sent, as a NaN, a year past 9999 and an offset with
    seconds in a body do. The mock server reads each such request back to the values sent, a header
    `Prefix` map's keys in lower case, as an SDK reads an inbound request's parameters, and either
    reads a query's `a+b` as `a b`, a scalar's `page=1&page=2` as a text that does not parse, a
    `page=` as no `page` where `AllowEmptyValue` is set, and an `any` path parameter's `abc` as the
    JSON string `"abc"`. A response header of each primitive reads `+00:00`, a fraction of each
    length, one finer than its type's precision cut to it, an upper-case `uuid`, a list sent on two
    lines and one holding an empty element without it, a `Set-Cookie` list of two lines whose values
    hold commas as two elements, a header whose `Prefix` is set holding a field another header reads
    too, keyed in lower case, a `string` header sent as `no-cache, no-store` keeps its space, a
    `text/plain` body reads `42` after a byte-order mark and does not decode under
    `charset=iso-8859-1`, and a leap second leaves its field unset with `RawHeader` holding the text
    (T-26) ([the type table](../contract/type-table.md#what-a-built-in-row-writes)).
48. **T-48 · A value no style arranges** — on every target, under T-12's interception, a query
    parameter whose list holds objects, one whose list holds lists, one whose list holds a union of
    objects, a `form` object holding an object, exploded and not, a `deepObject` parameter with a
    list member, a `spaceDelimited` parameter of one string, a `pipeDelimited` list that explodes, a
    cookie of a list of objects, and a form field and a multipart part of an object that declare no
    encoding are each a list of pairs of strings in the SDK; a header list of objects, a path
    parameter of a list of objects and a `text/plain` request body of an object are each a string;
    and each is reported as `emitters/plan/unarranged-value`. The pairs `deep[a][b]` and `x`,
    `ids[]` and `1`, `ids[]` and `2`, and `search` and
    `[{"field":"value","op":"in","value":["evil.com","bad.org"]}]` are sent as four parameters in
    that order, each name percent-encoded as a parameter's name is and each value as a string's text
    is, whatever the binding's name, `Style` and `Explode`, as four fields of a form body and as
    four parts of a multipart one, none with a `Content-Type` of its own; the string is sent as its
    header's value, in its segment and as the body's UTF-8 octets. A pair with an empty name, one
    naming another parameter of the operation, and a required input holding no pair each fail before
    anything is sent. A multipart part of a string that declares no encoding is its text with no
    `Content-Type` of its own, and one of a list of strings a part for each element. The mock server
    and an inbound request's reader give back the pairs sent, in order, and the string sent, for the
    values `a&b=c`, `a+b c`, `100%`, `é` and an empty string too, a pair named for a declared
    parameter left to that parameter. A `text/plain` response declaring an object returns its text
    as a string, the mock answers it with no body and a warning `emitters/mock/example-missing`, and
    an inbound operation declaring a `text/plain` request content of an object beside an
    `application/json` one reports the content and reads a request sent as `text/plain` as the
    string it held. The same parameter declaring `content` as `application/json` is typed as its
    list and sends that JSON text, a `string | list of string` parameter keeps both arrangements, an
    `any` parameter holding a list fails before anything is sent, a `text/plain` content of an
    object beside an `application/json` one the ranking elects draws no diagnostic, and a declared
    example of such a parameter is refused as `emitters/plan/example-refused`, none being
    synthesized in its place ([values as text](../refine/text.md#a-value-no-style-arranges)).
49. **T-49 · A date in a header** — on every target, a response header declared `datetime_offset`
    with no encoding reads `2023-10-21T07:28:00Z`, `Sun, 06 Nov 1994 08:49:37 GMT`, `Sunday,
    06-Nov-94 08:49:37 GMT`, `Sun Nov  6 08:49:37 1994` and `Wed Nov 16 08:49:37 1994` each as its
    instant at a zero offset,
    reads `Wed, 21 Oct 2023 07:28:00 GMT`, whose day name is not its date's, and leaves its field
    unset with `RawHeader` holding the text for `Wed, 21 Oct 2023 07:28:60 GMT`, `Wed, 31 Feb 2023
    07:28:00 GMT`, `sun, 06 nov 1994 08:49:37 gmt`, a date ending `UTC` and a day name alone
    (T-26). The row's reader, given 2026-11-12T00:00:00Z as its clock, reads `Thursday, 12-Nov-76
    00:00:00 GMT` as 2076 and the same date one second later as 1976; given 2028-02-29T12:00:00Z,
    `Wednesday, 01-Mar-78 00:00:00 GMT` as 1978 and `Monday, 28-Feb-78 18:00:00 GMT` as 2078; and
    given 2099-06-01T00:00:00Z, `Thursday, 01-Jan-05 00:00:00 GMT` as 2105. A header list of such
    dates sent as `Sun, 06 Nov 1994 08:49:37 GMT, Sunday, 06-Nov-94 08:49:37 GMT, Sun Nov  6
    08:49:37 1994, 1994-11-06T08:49:37Z`, on one line and on four, reads as four elements,
    and one holding an empty element reads without it. The mock server reads a request header the
    same way, a `datetime_offset` header an SDK or the mock writes is RFC 3339 text, and a `date`,
    a `time` and a `datetime` header read no HTTP-date ([values as
    text](../refine/text.md#reading-a-text)).
50. **T-50 · A path pattern** — on every target, under T-12's interception, a path parameter whose
    binding's `PathPattern` is `shelves/*/books/*` or `**` sends `shelves/s1/books/b2` with its
    slashes, `a:b%41?c` as `a%3Ab%2541%3Fc`, `a b/é` as `a%20b/%C3%A9` and `a//b` as written, with
    `AllowReserved` set or unset, and fails before anything is sent for `a/../b`, a null and an
    empty string; one whose pattern is `*` sends `a/b` as `a%2Fb`. One whose pattern is `**` in the
    template `/prefix/{label}/suffix` sends `foo/suffix/bar` as written, and the mock reads
    `/prefix/foo/suffix/bar/suffix` back to it. The mock server reads each text back as sent, and
    reads a path another client sent as `a%2Fb/c` or as `a%2fb/c` as that text, its triple kept.
    Each such binding draws an info `emitters/<target>/unchecked-constraint`, and none drops its
    escaping with the check ([values as text](../refine/text.md#escaping)).
51. **T-51 · A row keyed by an encoding** — each target's built-in table has, at every options
    version, a row for every key the type table lists for an encoding. On every target, `bytes` `fb
    ff` under `base64url` is written `-_8=` in a body and in a header and `-_8%3D` in a path, and
    reads back from `-_8=` and from `-_8`, while `+/8=`, `-_9=`, whose pad bits are not zero, and
    `-_8=` broken by a line feed after its second character do not parse; a `datetime_offset` under
    `http-date` at `2020-02-29T23:59:59.999+02:00` is written `Sat, 29 Feb 2020 21:59:59 GMT` in a
    header and as a body's string, reads back at a zero offset, reads `Sunday, 06-Nov-94 08:49:37
    GMT` and `Sun Nov 6 08:49:37 1994` as `1994-11-06T08:49:37Z`, and at `9999-12-31T23:59:59-01:00`
    fails before anything is sent; and `base64`, `rfc3339` and `iso8601` write what their
    primitives' rows do. A `bytes` scalar under an encoding no row names whose `WireType` is
    `string`, and a `datetime_offset` under one with no `WireType`, are each a string in the SDK
    that is sent as given, with a warning `emitters/<target>/unmapped-encoding`; a `datetime_offset`
    under `unixTimestamp` whose `WireType` is `int64` is an `int64`; and a `string` under `email`
    stays a string with an info ([the type
    table](../contract/type-table.md#a-row-keyed-by-an-encoding)).
52. **T-52 · The path fold** — the fold joins each pair of a table holding what APFS makes one path,
    `Setup` and `SetUp`, `ſetup` and `setup`, a Kelvin sign and `K`, `σς` and `σσ`, `Straße` and
    `Strasse`, an `é` composed and decomposed, `q` with U+0323 then U+0307 and with the two
    reversed, and a Cherokee letter in each case; and on a case-insensitive APFS volume, both sweeps
    the rule names, every spelling of each assigned character behind an `a` and names of one to four
    characters, each written to one empty directory, leave no two names of one file in two folds,
    where the same sweeps under a fold decomposing each character alone, under `cases.Fold` with no
    upper-casing before it and under a fold of ASCII letters alone each do ([the path
    fold](../emit/rules.md)).
53. **T-53 · Names past the length cap** — a fixture naming a type, a union and its variant, a
    group, an operation and a property at the cap, a byte past it and far past it, in ASCII and in
    characters of two, three and four bytes, the operation in the client of the group at the cap
    once as each shape a method lowers to, a plain, a paged and a streamed one, with and without its
    `WithResponse` method, generates code that compiles on every target: each identifier of a role
    written into a path within the cap, a shortened one keeping its leading words and its hash word
    and drawing one `name-shortened`, two that share their first 110 bytes apart, among them two
    names with no words (twelve and thirteen `_`), two types named from hints and one long type's
    create and patch declarations, the property's name whole, and no segment of a path the build
    writes past 255 bytes, every class file kotlinc writes among them, none of which a method's body
    has it name for an inline function
    ([naming](../refine/naming.md#a-name-too-long-for-a-path-is-shortened)).
54. **T-54 · Names past ASCII** — a fixture naming a type, a property, a parameter, an operation, an
    enum member and a group with a decomposed `café`, with `नाम`, with a letter Kotlin 2.4.0's lexer
    does not know, U+0870 and past the BMP U+10570, with a digit it does not know, `a𐵀`, with a mark
    past the BMP, U+11046 after a letter, with `ɤx`, whose capital it does not know, with a name
    whose first word and one whose second begins with U+0301, and with names whose first letter Go
    does not export once cased, `名前`, `ʰx`, `ßx`, `ᾳx` and past the BMP `𐀀x`, generates code that
    compiles on every target: on Go each character a role cannot hold written by its name once the
    identifier is cased, its `u` in lower case and its hex four digits at least (`Cafeu0301`,
    `Field_u0301x`, `u11046`), and every import path in ASCII; on Kotlin each such identifier in
    backticks with the spec's characters (a type `Ɤx`, a property of `field_`, U+0301 and `x`), a
    collision's suffix inside them, built for the JVM, JS, Wasm and a framework, under the
    serialization plugin, every KDoc link to one resolving, and every module published to a Maven
    repository; and the policy's casing of every code point equal to a golden held to the options
    version, so a Go release moving its Unicode tables fails the suite and renames nothing. A caller
    in another unit, a Go package or a Kotlin module, and a Java caller, sets and reads each
    property and calls each operation by its rendered name; a name holding U+2E2F links into a
    Multiplatform framework whose header clang compiles, and a Swift file importing the framework
    reaches each class, property and entry by the Swift name the policy chose, past ASCII its
    Objective-C name, as `ProbeA日本` for a class `A日本` in a framework `Probe` and `ProbeA日本Builder`
    for the builder it nests, or an explicit name in ASCII where it takes one, as enum entries
    `A_日本` and `A日本` are `a_u65e5u672c` and `a日本` in both declaration orders, and each method whose
    name or a parameter's is past ASCII, an ASCII `fetch(id:名前:)` and a `listBy(id:名前:)` among them,
    by the name read from the built module, its selector the policy's
    ([characters](../refine/naming.md#a-character-a-target-cannot-write-is-written-by-its-name),
    [leading
    words](../refine/naming.md#an-identifier-its-role-cannot-begin-with-takes-a-leading-word),
    [Swift](../targets/kotlin/swift.md)).
55. **T-55 · References resolve** — a fixture holding, in the root and in a group's unit, a union
    whose variants are named after the types they wrap, one in another unit among them, after a
    class Kotlin imports by default (`List`) and after the union itself; an open union beside a
    model named `Unknown`; models named `Builder` and `Companion` that another model's properties
    hold; a model whose properties are named after the first segment of each package its file names,
    the root's and `kotlin`'s among them; on Go a spec type beside a runtime type of its name; two
    units declaring one name and a file naming both; an operation named after each function its
    client's code calls and after each member of a lambda's receiver it writes (`emit`), a parameter
    named `it`, an enum entry, a property and a model named after what each file names (`size`,
    `serializer`), and on Kotlin a declaration its file imports under an alias named in a KDoc link,
    generates code that compiles on every target. A consumer in another module and package, and on
    Multiplatform a Swift caller, builds each wrapper from the type it wraps and each model from the
    types its properties name, each variant decodes as its wrapped type, and the client's calls
    against the [behavior server](behavior-server.md) do what the same fixture's do with each such
    name changed to one nothing else takes. No Kotlin file writes a reference qualified by its
    package, the rendered docs show each declaration's own name where its file imports it under an
    alias and open its page, and adding a variant, a model and a property renames no declaration the
    manifest records. On Go, a model whose fields and methods are named after each package its file
    imports compiles ([Kotlin](../targets/kotlin/naming.md#imports),
    [Go](../targets/go/naming.md#imports)).
56. **T-56 · The tag a variant's value carries** — on every target, a discriminated union whose
    variants leave the tag a plain string encodes a variant built with that member unset, or set to
    another variant's tag, with the one tag routing to it, and decodes the result as that variant; a
    variant two tags route to writes the one its value holds and refuses, with a coded error, a
    value holding neither; a variant pinning its tag as a number is routed by that number's value, a
    payload spelling it `10016.0` or `1.0016e4` decoding at a closed union as `10016` does and one
    carrying it as the string `"10016"` failing in that variant; a variant that is a union carries
    the outer union's tag in the object its value is, added where that object has no such member;
    and a variant no tag routes to decodes by recognition, before `Default`, in a union declaring a
    `Default` too, where a value no such variant admits decodes as `Default`, and refuses to encode
    a value carrying another variant's tag, as `Default`'s variant refuses one such a variant
    admits. Every example the plan synthesizes for a variant, and every member `plan.Shown` fills,
    carries one of that variant's own tags and decodes as the variant it was made from, one whose
    member's set lists another variant's tag first among them, and a declared example that the
    variant its tag routes to does not admit is `emitters/plan/example-refused`
    ([unions](../refine/unions.md#the-tag-a-variants-value-carries)).
57. **T-57 · What a mock accepts** — the mock accepts every example the plan holds for a request,
    shown in the shape its operation sends, and refuses a body narrowing a discriminated union that
    lacks a member the entry its tag routes to requires, breaks a constraint of a declaration that
    entry takes or of one it overrides, a member redeclared with no bound over a declaration
    bounding its length refusing a value past the bound in a model narrowing no union too, or
    carries a tag routing to no entry, the model's own members admitting all of them. It refuses, in
    a request, an enum value and a variant the document does not list whatever `TypePlan.Open` says,
    and accepts one where the IR declares the type open; and it tries each entry of a body narrowing
    an untagged union at most once ([what a mock accepts](../docs-and-mock.md#what-a-mock-accepts)).
58. **T-58 · A declaration no operation moves** — adding to a fixture an operation that returns an
    enum, a union, a polymorphic base and a model narrowing a union until then only sent, and
    removing the one operation returning others, changes no declaration on any target: each is
    `Open` before and after, marked `Inferred`. On Multiplatform an open enum, a flags enum, a
    scalar's declaration over a string, over a number and over `bytes`, and the runtime's `Decimal`
    each reach the Swift export as a class a Swift caller names, constructs and compares with `==`,
    an enum's with a constant for each member, with which that caller builds a request and reads a
    value the SDK kept, a sensitive scalar printing `[REDACTED]` there; a Swift call passing a
    string or a number where one is asked does not compile; and on both Kotlin targets each encodes
    as its wire form, never as an object holding it, as a map's key too, and decodes back equal
    ([plan policy](../plan/policy.md#neither-policy-reads-which-way-a-type-travels),
    [Swift](../targets/kotlin/swift.md#no-value-class)).
59. **T-59 · What a patch relaxes** — on every target, in the docs and in a mock, a `PATCH` body
    sent as `application/json` requires each member its schema requires, a builder refusing a
    request without one and a mock one arriving without it; the same type sent as
    `application/merge-patch+json`, spelled in any letter case and with a parameter, is a patch
    shape whose members are optional and whose `Inferred` marks `Patch`, the members of a model it
    holds relaxed and a list's elements not; a binding whose `PatchImplicitOptionality` points at
    `true` relaxes an `application/json` body unmarked, and one pointing at `false` relaxes no merge
    patch; an operation offering both contents sends the merge patch, a request built without the
    members the `application/json` schema requires going out under that media type and accepted by
    the mock, with `RequestContent` marked `Inferred`, and sends `application/json`, requiring them,
    where the ranking names that media type by itself or the policy is off; turning
    `plan.Policy.MergePatch` off removes the patch shape and its mark; and a `POST` sent as a merge
    patch relaxes nothing ([TypePlan](../plan/types.md#what-a-patch-relaxes), [one
    election](../plan/responses.md#one-election)).
60. **T-60 · The declaration a shape takes** — a model with a `readOnly` and a `writeOnly` member,
    read by one operation and written by a `POST`, a `PUT`, a `PATCH` and a merge patch, is declared
    as `User`, `UserCreate`, `UserCreateUpdate`, `UserUpdate` and `UserUpdatePatch` on every target,
    in the docs' tables and in the manifest's keys, and a union and a model holding it are declared
    under the same words; a model no member of which carries a visibility is declared once, under
    its own name, with an alias of that declaration for each shape that writes it, which each
    signature names, and giving one member a `readOnly` marker turns those aliases into declarations
    under the names and keys they had, a caller's program in another module that builds each request
    by its shape's name compiling before and after on Go and on Kotlin, the Swift export showing no
    alias; a model only a `PUT` writes, with a `readOnly` member, is `UserCreateUpdate` with no
    `User` beside it, and adding a `POST`, then an operation returning it, renames no declaration
    and regenerates the earlier ones byte for byte; a custom lifecycle `<class>:<member>` gives its
    class's words and then its member's; a filter an operation names for its view is named as a
    method's is; a component the source calls `UserCreate` keeps that name in either declaration
    order and the shape takes a suffix, while one added after a generation shipped the shape's
    `UserCreate` takes the suffix itself; two custom lifecycles whose words join alike, `Foo:BarBaz`
    and `FooBar:Baz`, give two declarations told apart by a suffix; two lifecycles whose keys would
    join alike, one holding a `,`, key apart; and the `create` shape of a property `x`'s type keys
    apart from the type of a property `x#create` beside it
    ([TypePlan](../plan/types.md#the-declaration-a-shape-takes)).
61. **T-61 · Integers by value** — on every target, a member of each integer kind, bare, optional,
    nullable, in a list, as a map's value, in a declaration over the kind and in a union's variant,
    decoded by the client and by a caller's own decoder call, reads `1`, `1.0`, `1e0`, `10e-1` and
    `1E+2` as their values and `-0` and `-0.0` as zero, an unsigned kind among them; refuses `1.5`,
    `15e-1`, `1e-400`, the string `"1"`, the first value past each kind's range in plain digits and
    in exponent form, and a negative number into an unsigned kind; reads each kind's least and
    greatest value and 9007199254740993, in plain digits and in exponent form, to that exact value,
    which no reader passing through a float does, and `0e99999999999999999999` as zero; refuses
    `1e1000000000` and reads a literal a megabyte long that names 1, each within a time bound a
    literal's expansion would pass; writes plain digits; reads a parameter's and a header's text by
    the same table, in an SDK and in a mock; and leaves a caller's own decoder for an integer type
    its precedence at any depth ([type
    table](../contract/type-table.md#an-integer-row-reads-a-number-by-its-value)).
62. **T-62 · Bytes compare by content** — on both Kotlin targets and from a Swift caller, two values
    decoded from the same `bytes` text are equal and hash alike as themselves, in a scalar's
    declaration over `bytes`, as a model's member, as a list's element, as a map's value and key and
    as a union's variant, and a model holding each equals its twin and finds it in a set; changing
    the array a value was built from, or the one it hands out, changes neither the value nor its
    hash; and a `bytes` member still writes and reads its row's base64
    ([Kotlin](../targets/kotlin/types.md#bytes)).
63. **T-63 · Nesting** — on every target, through the client, a response body, an error's body and a
    stream's frame nested one level past `MaxDepth`, at its default and at a value a configuration
    sets for one operation, through a recursive model's member, through its list, and through an
    `any` position, each reach the caller as a body that does not decode, the status and the bytes
    kept, on a thread with the smallest stack its target names, 256 KB on the JVM and 512 KB on
    Kotlin/Native, and never as a crash or a stack overflow; a body at the limit decodes, and the
    value then encodes, prints, hashes and compares on that thread; an opening bracket inside a
    string counts for nothing, a string ending in an escaped backslash among them, the value after
    it nested past the limit still refused; a value a caller builds past the limit is refused by the
    client's encoder, nothing sent, and prints, hashes and compares without recursing into it, on
    Kotlin/Native on a 64 KB stack; in Go a value holding itself through a pointer, a slice and a
    map is refused by the encoder and printed by `String`, `GoString`, `LogValue` and every `fmt`
    verb to the limit and no further; a Kotlin model built from a list its caller then changes keeps
    its members and its hash; and a `MaxDepth` a target cannot hold to, one above 64 on
    Multiplatform, is reported and none is read to another depth
    ([models](../refine/models.md#how-deep-a-value-nests)).
64. **T-64 · The runtime's surface** — on every target, a caller's program built outside the SDK, as
    another Go module, another Gradle build and a Swift file against the framework, compiles while
    it uses the client, a sub-client's method, the failure value, a stream and an event, and fails
    to compile for each declaration generated code alone calls that it names: a helper, an
    operation's settings type and what a sub-client is built from. In Go the failure is the import
    of a package under `internal/`; no struct of `internal/state` has an exported field, no package
    outside `internal/` exports a function that builds a sub-client, and a sub-client or a stream
    written as its zero value returns an error from each method. Read from the packages' types, the
    method set of each exported type holds no method a package under `internal/` declares, which a
    state struct embedding a type breaks; no two exported struct types convert to each other, which
    two sub-clients declared over one struct break; no exported signature names an internal type;
    each field of a struct an exported type is declared over is a function; and no package outside
    `internal/` exports an operation's row of the settings. In Kotlin the failure is the missing
    opt-in, in a caller's file and in a generated file with its opt-in removed; Java cannot resolve
    a marked function; and the framework's header holds no marked declaration. No class file of that
    caller's program, which calls each inline function a caller can name, names a declaration the
    unfiltered dump lists and the filtered one does not, as it would with an `inline` function or a
    type alias declared in a file that opts in, and no caller-facing class has a marked supertype.
    The surface each target's extractor reads, `apidiff`'s over the packages outside `internal/` and
    the ABI dumps written with the marker excluded, holds none of them, is unchanged when a helper's
    signature, a settings type or a state struct changes, and changes when a field or a method of a
    caller-facing type does. A Gradle build asking for two modules of one Kotlin SDK at different
    versions resolves every module to the newer and runs, as does a Maven build importing the SDK's
    platform and naming no version, and of two SDKs in one program each failure matches its own
    SDK's failure value and not the other's ([the runtime's
    unit](../refine/output-units.md#the-runtimes-unit),
    [Go](../targets/go/overview.md#what-a-caller-reaches-of-the-runtime),
    [Kotlin](../targets/kotlin/overview.md#what-a-caller-reaches-of-the-runtime)).
65. **T-65 · Credentials** — on every target, against the behavior server (T-14), a client whose
    credential is a value the test can search for, configured in turn for an `apiKey` in a header,
    in the query and in a cookie, for `http_basic` and `http_bearer`, and through an `Authenticator`
    writing a header, a query parameter and a cookie no document names, over a base URL holding a
    user, a password and a query of that value. Across a call that returns, one a status fails with
    a `Set-Cookie` of the value, one redirected to a `Location` holding it in its path and its
    query, one retried, one timed out, one whose connection is refused and one whose credential a
    header cannot carry, the value is found in no event, no failure, no response a method returned,
    and no client, sub-client, stream or poller: not in any form the target's printing, logging and
    encoders read of one held by reference, by value or in a field of the test's own type, exported
    or not, each `fmt` verb, `%O` and `%p` among them, both `log/slog` handlers, `encoding/json` and
    `encoding/json/v2` in Go, and `toString`, Gson and Jackson in Kotlin, not in an event encoded as
    JSON, nor in what a Kotlin event's `copy` and each `componentN` hand on, and not in the messages
    of a failure's chain, which holds no `*url.Error` in Go and no exception of the HTTP library's
    in Kotlin, a call past `Timeout.Request` and a stream past `Timeout.Idle` among the failures.
    Each event's `URL` is the URL as shown, a redirect's the origin and the marker, and what a
    redacted parameter expanded to in the path the marker; an `end` event's `Cause` is a type's
    name; a Go hook is handed the context its caller gave the method, and the events of two
    overlapping calls are told apart by `Call`; a `RequestID` longer than 128 bytes or holding a
    line break is empty; a parameter in the slot of a scheme its operation's options name is no
    input of the request type, and the slot goes out once; one in the slot of a scheme they do not
    name, and a header parameter named `Authorization` under no scheme, print as `[REDACTED]` in
    their request type; a value of a type a scheme names as its `Credentials` prints as the marker;
    and the test fails when the event is built after the auth step, which it is run once to show
    ([auth](../operations/auth.md#where-a-credential-can-be-read), [what an event
    holds](../contract/runtime-policy.md#what-an-event-holds),
    [redaction](../refine/redaction.md#a-redacted-value)).
66. **T-66 · The mock's streams** — on every target, each method of an operation offering a value
    beside a stream, and of one whose stream a `Condition` selects, run against the mock, gets the
    content it asks for, by `Accept` and by the condition's input whatever `Accept` says, a range of
    `q=0` excluding a content and `text/*` ranking above `*/*`; each framing, with an `Element` and
    without, a terminal variant and a `Terminator`, alone and together, reaches the caller frame for
    frame in that order and ends with the body, a value holding a line feed arriving whole and one
    holding a carriage return with a line feed in its place under `sse`, and as its lines under
    `json_lines`, an `sse` element with no `data` arriving as an event, a declared body text ending
    mid-line keeping its last frame apart from the `Terminator`, a declared example giving the
    frames it holds and none giving `MaxItems` synthesized ones, or with `Synthesize` off those its
    set declares and `example-missing` where it declares none, a synthesized terminal variant never
    before the last, and an `Initial` message on an HTTP binding writing and reading no frame; a
    side the client writes is read to its end or its `Terminator`, a frame after the `Terminator`
    and one failing validation refused, a body past `MaxRequestBytes` refused as too large and never
    as a frame that does not decode, one whose `RequiresLength` is set refused (411) with no length,
    and then answered as `Return` reads; a `bidi` exchange ends, each frame the mock read before its
    side ended validated, for a caller that writes first, one that reads to the end before ending
    its side and one that alternates, over HTTP/2, and over HTTP/1.1 where the target's transport
    delivers a response while its request is open, one that does not running the first caller alone;
    an exchange in which no byte moves for `IdleTimeout` is ended, over HTTP/2 beside a walk on its
    connection, which other requests keep using; a gRPC method with an HTTP transcoding is routed
    through the transcoding, and an operation with no HTTP binding draws
    `emitters/mock/unsupported-binding`; and one request gets the same bytes on every run. A mock
    holding a stream open, ending a `bidi` response only once the client's side ends, answering one
    in lockstep, or bounding idleness per connection, which leaves a stalled stream open beside a
    walk, fails it ([what the mock streams](../docs-and-mock.md#what-the-mock-streams), [the mock's
    options](../docs-and-mock.md#the-mocks-options)).
67. **T-67 · The mock's page walks** — on every target, under each strategy and each set of `Stop`
    conditions, with no limit and limits of 0, 1, 2 and a billion, a continuation member optional,
    nullable and required, and `FirstPage` 0 and 1, a pager run against the mock reads each item of
    the walk once, in order, in at most one request more than the walk holds items, and ends, a
    limit of 0 included; under a cursor or token that is required and not nullable it ends on a
    refusal, drawing `emitters/mock/page-walk-refused`, unless `Stop` lists `empty`, where it ends
    on the page past the last item, `no_next` over a type admitting the empty value, or `total` with
    a total to read; a continuation the mock did not mint is refused, a page number below
    `FirstPage` that the input admits and an offset past the end get an empty page, a limit of a
    billion one page of the walk, and one request the same page whatever came before it; with
    `Synthesize` off a walk's items and continuations are values the document declares; a pager's
    `GET` of a link a `POST` operation wrote gets its page; and a caller whose filter is spelt
    `q=a%20b&z=1&a=2` reads each item once at a limit of a billion. A mock whose last page keeps a
    continuation, that ignores an offset, counts pages from another first page, refuses a number
    below it that the input admits, ends a page short before the last, sizes a page by a limit of 0,
    or writes its last link rebuilt from the query it parsed fails it ([a page walk on the
    mock](../docs-and-mock.md#a-page-walk-on-the-mock)).
68. **T-68 · Bindings a target covers** — a plan binding of each `OpBindings` field carries that
    field's `Protocol` and its binding, a GraphQL and an OTP one as the IR types it and not as
    `RawConfig`, an RPC binding of every `System` `rpc` with the system in `RPC.System`; `All` holds
    the HTTP bindings in their order, then the RPC, message, GraphQL and OTP ones, whatever order
    the source declared them in; an operation bound as a gRPC method and two HTTP transcodings of it
    lowers through the first transcoding on a target listing `http` alone and on one listing `http`
    and `rpc` with `grpc`, its path parameters in that template's order and its server stream framed
    by its content, the mock routing that one transcoding, and through gRPC on one listing `rpc`
    with `grpc` alone, its stream framed as gRPC; a JSON-RPC method on the last draws
    `emitters/<target>/unsupported-binding` and generates no method; the registry refuses `rpc`
    without `RPCSystems`, `RPCSystems` without `rpc`, and a protocol outside the vocabulary
    ([bindings](../plan/bindings.md), [capabilities](../contract/registry.md#capabilities)).
69. **T-69 · One snapshot for a target without `CrossVersion`** — on a document sliced into three
    versions, the Go, Kotlin and mock-server targets each write the snapshot `RunOptions.Version`
    names at the output root, its `Snapshot` that label, the first and the last alike; a run naming
    none is `engine/target-unversioned` before any run, its message naming the three labels, and
    writes nothing, while a docs run naming one of them for samples alone is not refused; an SDK and
    its mock server in one run read one version; a label the document lacks is
    `engine/snapshot-unknown` before any run; each such target is run on the other snapshots for its
    samples alone, writing nothing there; and a regeneration moving the label keeps the name of
    every entity both snapshots hold and prunes the files of one only the first held ([contract
    rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)).
70. **T-70 · Deprecations** — on every target, a deprecated operation, type, property, parameter,
    enum member and union variant each carry the target's mark on every declaration they lower to,
    each method of an operation and each shape of a model among them, a positional path parameter
    its text in its method's documentation, and one setting no field the shared sentence; its text
    holds the message escaped, each replacement as it renders after a collision renamed it, linked
    within its unit and from another, and the removal; a replacement with no declaration is named as
    text, never as a link that resolves to nothing, and a deprecated one is named and not followed,
    two naming each other included; on Go, `go/doc` parses each link and gopls reports each use from
    another package, a deprecation stating nothing and one whose message holds a blank line
    included; on Kotlin, a file in another module using each deprecated declaration as the samples
    do, its members included, compiles with a deprecation warning, an enum entry's excepted, whose
    text its enum's KDoc holds, and with each `ReplaceWith` applied as an IDE applies it, the call
    replaced by the expression with the arguments for the parameters and the imports added, compiles
    with none, while a replacement of another signature or result type, a type's, an enum member's,
    a second replacement and one on another client get none; and a Swift caller of the Multiplatform
    framework is shown the message at each function, property and initializer, and the header holds
    a deprecated class's text and, in its enum's documentation, a deprecated entry's
    ([deprecations](../refine/deprecation.md)).
71. **T-71 · Operations the API sends** — on every target, a webhook and a callback get no client
    method and one receiver for their service, with a parse method each; a request of each content
    the operation declares, carrying a valid API key or basic or bearer credentials, parses into the
    `read` projection, while a wrong method, an undeclared content type, a wrong key, a body past
    the bound, read no further than it, a failing user check, and a scheme the receiver cannot check
    with no user check configured are each refused, a refused check a verification failure apart
    from a decode failure; an `apiKey` in a header, a query parameter and a cookie, and basic and
    bearer credentials, are each refused, a request carrying none among them, by a receiver given no
    value for that scheme and no check; an operation declaring no security, and an option of no
    scheme, are refused by a receiver given neither a check nor the unauthenticated setting and
    parse with either; the user's check reads the body's bytes as they arrived, so an HMAC over them
    verifies, and the request's target and, over TLS, the client certificate, so a `mutual_tls`
    check and a query key verify; each declared response writes its status, headers and body with no
    envelope, a range's refusing a status outside it and a `Default`'s one another response
    declares, none of them a `CommonErrors` entry; a Swift caller builds a receiver and catches a
    refusal; the mock server sends a callback once, after its answer, to the URL its expression
    resolves to, follows no redirect and gives up past `SendTimeout`, never dials an address outside
    `SendTo`, a name resolving outside it included, and with 200 requests naming one loopback URL
    has no more than `MaxSends` sends in flight, and sends a webhook only through its control route,
    which the API's listener does not answer; and the docs give each a page naming its event or its
    declaring operation and expression, with no sample ([operations the API
    sends](../operations/inbound.md)).
