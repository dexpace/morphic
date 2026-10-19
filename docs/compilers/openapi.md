# OpenAPI compiler — lowering specification

This document is normative for `compilers/openapi`, as [`ir-design.md`](../ir-design.md) is for the
IR: it states which OpenAPI 3.0, 3.1 and 3.2 documents the compiler reads, how each construct of one
lowers into the IR, where it keeps what the IR does not model, and every diagnostic it reports. The
IR's shapes, and what a consumer may rely on, are ir-design's, and nothing here restates them; a
bare section number (`§4.3`) names a section of ir-design.

Each rule is a heading whose text is its key, a stable slug that code, tests and the other documents
cite as `docs/compilers/openapi.md#<key>`. A rule keeps its key when its text changes, and a rule
that is renamed takes every citation of it along. Its text states the rule: what the construct
lowers to, what is kept verbatim and under which reason, and each diagnostic, written in full as
`openapi/<code>` with its severity and the position it is reported at. The rule then closes with the
evidence that pins it, one bullet per field:

- **Cases** — the conformance cases (`testdata/conformance/openapi`) that exercise it.
- **Codes** — every diagnostic code it reports. Each is written in the text, where the text says
  when it fires.
- **Tests** — Go tests that pin what no case reaches, as `package.TestName`: `openapi` for the
  compiler's own package, an internal package's name otherwise.
- **Status** — what the rule leaves open: where the compiler does not yet do all the rule says, or a
  question the rule does not settle, each naming the issue that tracks it, or the milestone that
  schedules work not yet begun. An issue is named here and nowhere in a rule's text.

The fields are the only bullets a rule writes at the margin, since each is read as one; a list in a
rule's text is numbered.

A rule names a case or a test, or says in its Status what it leaves open. The `TestLoweringSpec`
tests in `compilers/openapi` hold every field to the code and read it both ways: each conformance
case is named by a rule, and each code by a rule or by `codesOutsideTheRules`, which gives the
reason a code has no rule. [`CONTRIBUTING.md`](../../CONTRIBUTING.md) walks adding a rule.

## 1. Reading a document

### `openapi-versions`

A document declaring `openapi: 3.0.x`, `3.1.x` or `3.2.x` is read as the minor its first two parts
name, and every rule below that differs by minor follows that one. The patch is the parser's to
check: a version it cannot read as major, minor and patch, such as `3.0` or `3.1.0-rc1`, is reported
as an `openapi/validation` error, and the document still lowers under the minor it names. Any other
version is not this compiler's. The engine refuses a document declaring one before any compiler
runs, as it refuses a Swagger 2.0 document ([`swagger-lift`](#swagger-lift)), and the compiler,
handed one directly, refuses it with an `openapi/unsupported-version` error and compiles to no
Document.

- Codes: `openapi/unsupported-version`, `openapi/validation`
- Tests: `openapi.TestCompile_UnsupportedVersion`,
  `load.TestSupportedMinor_AcceptsOnlyTheThreeMinors`,
  `openapi.TestDetect_AVersionThatIsNotServedIsStillADeclaration`

### `yaml-decoding`

A source is one YAML document, a JSON one included. A source that declares an `openapi` or `swagger`
version and does not parse is reported, when the engine asks the compiler whether the source is its,
as an `openapi/undecodable-source` error, and nothing compiles. A YAML stream lowers the first of
its documents that holds content; the documents after it that hold content reach the IR in no form,
reported once as an `openapi/stream-documents-dropped` error at the first of them, and the first
still lowers. A document holding nothing, a bare separator or an explicit null, is not counted. A
mapping tagged anything but `!!map`, which OpenAPI's limit on YAML tags forbids, refuses the source
before the parser reads it, as an `openapi/tagged-mapping` error at the tagged mapping with the
least pointer, and it compiles to no Document. A tagged scalar is not refused here; what it lowers
to is [`values`](#values)'.

- Codes: `openapi/undecodable-source`, `openapi/stream-documents-dropped`, `openapi/tagged-mapping`
- Tests: `openapi.TestDetect_ABrokenDocumentIsReportedAtAnySize`,
  `load.TestLoad_AStreamLowersItsFirstDocumentAndReportsTheRest`,
  `load.TestLoad_ATaggedMappingIsRefusedBeforeParsing`
- Status: partly built: a JSON source holding a surrogate-pair escape is refused as undecodable
  (#900), and detection reads any `!!merge`-tagged key as a merge key (#845).

### `source-budgets`

`Options.Limits` bounds how large a source the compiler lowers, each budget taking its default when
unset and none when negative. A source past `MaxSourceBytes` is refused before it is parsed, and one
whose YAML parses to more than `MaxSourceNodes` nodes, or whose aliases add more than
`MaxAliasSurplus` nodes to it, before its model is built: each as an `openapi/budget-exceeded`
error, compiling to no Document. Past `MaxExternalDocuments`, a document an external reference names
is not read, each reference into it is unresolved ([`references`](#references)), and the compile
reports `openapi/budget-exceeded` once. Three bounds hold whatever the caller sets, since past them
no answer the pre-parse scan gives is whole: a document nested past the scan's depth bound, its
aliases expanded, is refused as an `openapi/source-too-deep` error at the least pointer where its
nesting passes the bound; one with more nodes than the scan indexes, as an
`openapi/source-too-large` error; and one whose aliases expand it past a fixed multiple of its own
size, with a floor for a small document, as an `openapi/alias-amplification` error at the least
pointer of the smallest structures past the ratio. An overlay is held to the same refusals before it
is applied, and so is a document an external reference names, which fails the reference instead. The
member budget of an enum is [`enum-const`](#enum-const)'s.

- Codes: `openapi/budget-exceeded`, `openapi/source-too-deep`, `openapi/source-too-large`,
  `openapi/alias-amplification`
- Tests: `openapi.TestCompile_SourceByteBudgetBindsOnlyPastTheLimit`,
  `openapi.TestCompile_AnUnboundedAliasBudgetStillRefusesABomb`,
  `load.TestLoad_RefusesANestingPastTheBoundBeforeBuildingTheModel`,
  `load.TestLoad_ADocumentTooLargeToIndexIsRefused`,
  `openapi.TestCompile_TheDocumentBudgetIsTheCallersToSet`
- Status: partly built: an external object reached after its contents is charged against the
  document budget twice (#571), and long keys nested a few hundred levels deep cost gigabytes within
  every bound (#931).

### `overlays`

An OpenAPI Overlay document handed in as `Options.Overlay` is applied to the source's parsed tree
before its model is built: the pre-parse refusals and the node budgets hold the patched tree as they
hold the source, and validation and every lowering read it. The overlay becomes a second entry in
`Document.Sources`, and every position it introduced or rewrote names that entry as its
`Provenance.Source`, while a position it left alone keeps the source's line and column. An overlay
that does not parse, or is not a valid Overlay — no version, no actions, an action naming no target
— is refused as an `openapi/invalid-overlay` error and nothing is applied. One the parser cannot
apply as written, or whose grafted content expands past the walk's node budget, is refused as an
`openapi/overlay-failed` error. Either way the source compiles to no Document rather than lowering a
tree the caller believes was patched. Application is strict unless `Overlay.Lax` is set: each action
that changed nothing, or relies on JSONPath behaviour the overlay did not opt into, is reported as
an `openapi/overlay-action` warning naming the action, and the compile proceeds; an action whose
selector matches nothing is reported the same way and refuses the overlay as
`openapi/overlay-failed`. Lax application reports none of these, and refuses only an overlay it
cannot apply. Where the walk that attributes positions to the overlay runs out of its node budget,
the overlay is applied and every position names the source, as with no overlay, reported as an
`openapi/overlay-origin-incomplete` warning.

- Codes: `openapi/invalid-overlay`, `openapi/overlay-failed`, `openapi/overlay-action`,
  `openapi/overlay-origin-incomplete`
- Tests: `overlay.TestApply_RejectsAnOverlayItCannotRead`,
  `openapi.TestCompile_StrictRefusesAnActionThatMatchesNothing`,
  `overlay.TestApply_StrictReportsAnActionThatChangesNothingWithoutRefusing`,
  `overlay.TestApplyWithin_DegradesToTheSourceAtTheNodeBudget`,
  `openapi.TestCompile_OverlayIsRecordedAsASource`
- Status: partly built: an Overlay 1.0.0 document's RFC 9535 filter selector fails to apply (#651).
  Open: whether lax application should report an action that matches nothing (#652), how an update
  merges a nested array (#653), and whether an overlay that replaces a value the source declares is
  reported (#588).

### `parser-validation`

The parser validates the source against the declared minor, and each finding is reported as
`openapi/validation` suffixed with the parser's rule name
(`openapi/validation/validation-required-field`), at the parser's severity, an error or a warning,
with a hint reported as info, and at the node it names. A finding does not refuse the document,
which lowers as written. The findings some rules take over are dropped and stated by those rules
instead: [`minor-required-fields`](#minor-required-fields),
[`later-version-construct`](#later-version-construct), [`parameters`](#parameters) and
[`operation-ids`](#operation-ids).

- Codes: `openapi/validation`
- Tests: `load.TestLoad_ValidationErrorsBecomeDiagnostics`, `load.TestMapSeverity`,
  `openapi.TestCompile_AFindingInAnExternalDocumentKeepsItsRule`
- Status: partly built: a finding about a declaration in another document is reported once per
  `$ref` that reaches it (#874), and one in an object an alias mounts twice is reported twice
  (#889); a null written in some validated fields faults the parser, and the whole document is
  refused as undecodable rather than the field reported (#891).

### `minor-required-fields`

The parser's REQUIRED-field rule is held to the declared version. It requires at every minor two
fields a later one stopped requiring — an Operation's `responses` from 3.1, a Response's
`description` from 3.2 — and it reads a field written as the empty string, or as an empty map, as
missing, where every minor's schema types the field as a bare string or map; its server URL check
repeats that reading for a variable's `default`. The load phase drops those findings wherever the
parser makes them: in the source, in an object a `$ref` reaches outside the model, and in another
document, which is held to the source's declared minor as its schemas' findings are. Nothing else is
dropped, and what stands is reported as an `openapi/validation` error
([`parser-validation`](#parser-validation)). An empty value the specification, or a grammar it
cites, refuses stands: an enumerated field (a Parameter's `in`, a security scheme's `type`); a path
parameter's `name`, which must match a template expression, none of which the empty string names; a
name sent in a header or a cookie, by a parameter or an API key, and an HTTP `scheme`, each a token
of RFC 9110 or RFC 6265; and a server `url` from 3.2, whose server URL grammar requires a character.
A name sent in the query, or a `querystring` parameter's, which is never sent, is admitted. A field
left out below the minor that relaxed it stands too, as does a `null`, and so does every finding at
a node two reads share through a YAML alias or merge key, unless every read there draws one of those
dropped there. The objects a later minor's construct holds are held to the declared minor like any
other.

- Cases: `empty-required-values`, `relaxed-required-fields`
- Codes: `openapi/validation`
- Tests: `load.TestRequired_AnEmptyValueNoMinorAdmitsIsStillRefused`,
  `load.TestRequired_AnEmptyServerURLFollowsTheDeclaredVersion`,
  `load.TestRequired_ARelaxedFieldFollowsTheDeclaredVersion`
- Status: partly built: the root fields each minor requires are never checked (#894), and a 3.0
  server variable is held to 3.1's `enum` rules (#890). Open: which minor governs a document a
  reference reaches, held to the source's until it is settled (#839).

### `later-version-construct`

A document may write a field, or one value of a field, that a later minor of its format added to the
object it sits on: a `name` on an OpenAPI 3.1 Server Object, `in: querystring` on a 3.0 Parameter
Object. It lowers exactly as in a document declaring the minor that added it, and is reported once,
at its own key, as an `openapi/later-version-construct` warning. That reading loses the least: from
the lowering and the diagnostic a consumer can still set the construct aside, while from a verbatim
copy it could not rebuild the operation a `query` key declares. Except in the one case set out
below, the declared minor gives the construct no meaning at all, so the lowering asserts nothing the
document does not write. It is a warning, as an undefined key is ([`unknown-keys`](#unknown-keys)):
an OpenAPI object's key set is closed, so the document is invalid for the version it declares, but
it still lowers, and a consumer that wants it refused has `--fail-on warning`. The rule covers
constructs a minor *added*, including the Reference Object's `summary` and `description`, which 3.1
added and which [`reference-object-siblings`](#reference-object-siblings) folds at every version.
That is the one case: 3.0 admits no key beside a `$ref` and says one SHALL be ignored, so a 3.0
document writing one is warned that it was read as 3.1 reads it. A keyword whose meaning differs
between minors, such as `exclusiveMinimum`, follows the declared one
([`constraints`](#constraints)).

Whether a construct is a later minor's is asked in one place: the load phase holds every object the
source's model holds to one table of what each minor added, and no lowering asks it, so no field's
treatment can depend on whether the parser happens to model it. The keys beside a `$ref` are judged
as the object they belong to: a Reference Object's, or a Path Item's own fields beside its `$ref`,
which are modelled and walked with the source. An object a `$ref` reaches in the source outside the
model, such as under an `x-` key, is held to the table as one the model holds is, and reported at
the pointers it is written at. Where the parser refuses such a construct itself — a Path Item's
`additionalOperations` below 3.2, a schema keyword the 3.1 meta-schema forbids — the compiler takes
the refusal over, dropping the parser's finding, and only the refusal: what the 3.2 meta-schema
merely stopped requiring still binds the declared version. Where refusing made the parser skip
checking what the construct holds, as inside `additionalOperations`, the compiler runs those checks
itself and reports what they find as the parser would, under `openapi/validation`. The table and the
takeover cover the source document; in a document a reference reaches, the parser's own treatment
stands. The same pass, with the same reach, judges a Parameter Object's `allowReserved` against
where the declared minor applies it ([`allow-reserved-locations`](#allow-reserved-locations)).

- Cases: `later-version-constructs`, `reached-outside-model`
- Codes: `openapi/later-version-construct`, `openapi/validation`
- Tests: `openapi.TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem`,
  `load.TestAdditions_EachIsReportedBelowItsMinorOnly`,
  `load.TestMetaSchemaVersionArtifacts_OlderMinorsOnlyWhenLater`
- Status: partly built: a 3.0 schema's `$ref` siblings are read as 3.1 reads them, which admits any
  keyword there, and no row of the table reports them (#871), and a JSON Schema 2020-12 keyword in a
  3.0 schema lowers with no diagnostic (#820). Open: how a keyword a later minor removed, such as
  3.1's `nullable`, is read (#632), and which minor governs a document a reference reaches (#839).

### `references`

A `$ref` resolves within the source, as a JSON pointer. One naming another document is followed only
when `Options.AllowExternalRefs` is set, against the directory of the source's path, each file or
URL read once however many references name it, and held to the source's budgets and refusals
([`source-budgets`](#source-budgets)), where a document past one fails each reference naming it.
What a reference to another document's parameter, request body, response, header, example, callback
or path item brings in lowers at the use site as the source's own would. A reference that resolves
to nothing — a pointer naming no node, a component its section does not hold, another document while
external references are off, an `$anchor` — is reported as an `openapi/unresolved-ref` error at the
referencing position, and the position lowers without it: a schema position to the top type, any
other position to nothing at all, so the parameter, response or body it would have named is absent.
A same-document `#/$defs/...` pointer is read by the rule the resolver follows for a reference it
meets on its own walk, in an order no declaration changes: the `$defs` of the schema writing the
`$ref` where that schema declares an `$id`, else the position the pointer names from the document's
root, raw YAML included, so a root `$defs` answers though the OpenAPI Object does not define the key
([`unknown-keys`](#unknown-keys) still reports it), else the definitions of the nearest ancestor
holding the path. A schema with no `$id` that writes such a pointer, and holds a definition under
its key while the root holds another, is left unresolved, reported as an `openapi/unresolved-ref`
error, since the resolver reads one or the other depending on what it resolved first
([`root-defs`](../parser-library.md#root-defs)). A chain of references that closes on itself is
[`reference-cycles`](#reference-cycles)'.

- Codes: `openapi/unresolved-ref`
- Tests: `openapi.TestCompile_ExternalFileRefIsNotReadByDefault`,
  `openapi.TestExternalContent_ASchemaRefPastARefNamesItsContent`,
  `openapi.TestCompile_SchemaEmptyPointerSegmentIsUnresolved`,
  `load.TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt`,
  `load.TestLoad_ARootDefinitionItsSchemaShadowsIsLeftUnresolved`,
  `defs.TestTarget_LocalIDRuleAndEscapes`
- Status: partly built: no schema another document holds is lowered, so a schema `$ref` naming one
  is reported unresolved (#74), and an `$anchor` is not honoured as a reference target (#914). A
  reference inside an object another document holds is never resolved, and nothing reports it
  (#751); an absolute-path `$ref` in a remote document is read as a local path (#936); a remote
  document can make the compiler request a private address (#937); an external document that does
  not parse is handed on rather than refused (#939); and a remote document reaching a local file is
  refused naming only its last such `$ref` (#940). A `$defs` reference inside a schema under an
  extension is reported unresolved (#570); a `$ref` whose pointer holds a character above U+FFFF is
  refused as malformed (#516), and one ending in `/` resolves to the parent (#770); a `$ref` to a
  position that holds no schema reports two diagnostics (#769); an unresolved reference's message
  ends in the resolver's own wording (#809); and what a reference into another document's content
  names can follow declaration order (#765, #766). A `$defs` pointer an ancestor answers with raw
  YAML is reported unresolved (#997).

### `reference-cycles`

A chain of `$ref`s that never reaches a node without one — `A: {$ref: B}`, `B: {$ref: A}` — and a
YAML anchor recursive in itself would each run the parser out of stack, so the source is refused
before the parser reads it, as an `openapi/cyclic-ref` error, and compiles to no Document. The cycle
is found whether it closes through same-document pointers, through `$anchor`, `$id` or a `$defs`
pointer, a root definition included, or through the source's own file name, and it is reported at
the reference of the cycle with the least pointer, never one only leading in, so the report does not
move with declaration order; a chain closing through the file name is reported at every reference
whose chain loops. A cycle of Reference Objects the resolver reports itself, as an unresolved
reference ([`references`](#references)). A schema reaching itself through what it declares is
recursion, not a cycle ([`recursion`](#recursion)). A cycle check that cannot finish, at one of its
bounds or on a fault of its own, is reported as an `openapi/cycle-scan-failed` warning, and the
compile proceeds with every cycle the check did classify still refused; past the chain read's bound,
a `$ref` it did not read is left unresolved where the source is held.

- Codes: `openapi/cyclic-ref`, `openapi/cycle-scan-failed`
- Tests: `openapi.TestCompile_CyclicSpecDoesNotCrash`,
  `openapi.TestCompile_ReentrantPrefixReportsAlikeInBothDeclarationOrders`,
  `load.TestLoad_DegenerateCycleIsRefusedBeforeParsing`,
  `openapi.TestCompile_MergeChainPastBoundStillCompiles`
- Status: partly built: a cycle inside a document an external reference names (#547), a source cycle
  entered twice from another document (#771), and a schema `$ref` through a Reference Object that
  leads back to it (#945) each overflow the stack; a cycle of component Reference Objects is
  reported at the root rather than at a reference of the cycle (#580); a reference on a failing
  chain is reported only where the walk reaches it first (#767); and the scan reads pointer
  spellings the resolver refuses (#843) and, outside a schema, a position by its key's name alone,
  refusing a loop the resolver never reads (#949).

## 2. The document and what it keeps

### `document-metadata`

`info.title` lowers to `Document.Name` and `info.version` to `Document.Version`, each as written;
`info.contact`, `info.license`, its 3.1 `identifier` included, and `info.termsOfService` to
`Document.Contact`, `Document.License` and `Document.TermsOfService`, each field as written; and
`info.summary`, `info.description` and the root `externalDocs` to the Document's `Docs`
([`docs-and-deprecation`](#docs-and-deprecation)). The document lowers to one `Service`, named by
`info.title` ([`naming`](#naming)).

- Cases: `docs-summary-desc`
- Tests: `openapi.TestMeta_FullDocumentMetadata`, `openapi.TestCompile_EndToEnd`

### `self-uri`

A 3.2 document's `$self` lowers to its source's `SourceInfo.SelfURI`, as written, a relative one
left unresolved. It is read at every minor: a 3.0 or 3.1 document writing one lowers it the same way
and is reported, as for every construct a later minor added, with an
`openapi/later-version-construct` warning ([`later-version-construct`](#later-version-construct)).

- Cases: `self-uri`
- Codes: `openapi/later-version-construct`
- Tests: `openapi.TestCompile_RecordsSelfURI`
- Status: partly built: `$self` is the base URI for some references and not others (#863).

### `verbatim-preservation`

A construct a rule keeps verbatim becomes an `Unmodeled` entry (§12) on the node or carrier that
owns its position: keyed `openapi:<keyword>` for a keyword or field, or by the scoped key the rule
names where several objects reach one carrier ([`vendor-extensions`](#vendor-extensions)), under the
reason the rule gives, and with a `Provenance` locating the construct itself rather than the node
that carries it. The entry holds the construct as JSON read off its YAML node: aliases followed and
merge keys merged, a number in its digits with its YAML base resolved, a YAML timestamp in the
spelling written, a `!!binary` as its source text, and a scalar under a tag of the document's own as
its text. A node no JSON can hold — a mapping key that is not a string, or repeats, `.nan` or
`.inf`, a scalar whose text its tag does not admit, invalid UTF-8, an alias that cycles or expands
past the converter's node budget — reaches the IR in no form. It is reported as an
`openapi/unpreservable-construct` error at the construct, and the rule that would have kept it
announces nothing as kept, so no diagnostic claims what the Document does not hold. A vendor
extension that converts to no JSON is [`vendor-extensions`](#vendor-extensions)' instead.

- Codes: `openapi/unpreservable-construct`
- Tests: `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`,
  `schema.TestCoDeclaredFamily_UnpreservableIsNotAnnounced`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`
- Status: partly built: a number past float64's range is kept as a JSON string (#783), and a field
  supplied through a merge key or an alias is found by no reader that keeps it, reaching the IR in
  no form (#384). Open: whether a scalar its own tag does not admit is refused or kept as its text
  (#245), and whether an extension mapping with non-string keys is kept with its keys stringified
  (#617).

### `unmodeled-dialect-fields`

A source revision may add a field the bundled parser's model names no member for; OpenAPI 3.2's
`Response.summary`, XML `nodeType`, the nested Encoding fields, a Media Type Object's `description`
and `components/mediaTypes` are the live set. Such a field is read off the raw node by a reader of
its own, and the object census is *told* which keys that reader took: the census grades a key by the
parser's model, so left alone it reports a field the document defines as an undefined key. The
reader runs whatever version the document declares, as
[`later-version-construct`](#later-version-construct) requires, so the census never calls such a
field undefined.

The census is told what the reader actually took, never a key list by version. A value the parser
would decode as a string goes to its home — `summary` into `Docs.Summary`, `nodeType` into
`XMLHints.NodeType` — and is not also kept verbatim, because one declaration gets one structural
home (§12.1). A value of another shape, or a non-empty `nodeType` outside the five the specification
defines, is kept verbatim at the key and pointer the census itself uses, under `degraded_lowering`,
with an `openapi/degraded-construct` warning at the field, and the census finds the entry and adds
nothing. A key the raw mapping does not present directly — merged in through `<<` — is not read at
all, so the census still reports it, as an `openapi/unknown-key-unreachable` warning
([`unknown-keys`](#unknown-keys)). An empty or null value is read as unset. A reading with no IR
home is kept under `no_ir_home`, with an info `openapi/degraded-construct` at the field: the nested
Encoding fields, a Media Type Object's `description`, since `ir.Content` has no docs, and the
`components/mediaTypes` section, which is kept whole
([`unreferenced-components`](#unreferenced-components)). A content entry written as a `$ref` into
that section lowers without what it names, the reference kept verbatim under `degraded_lowering`
with an `openapi/degraded-construct` warning at it.

- Cases: `response-summary-32`, `xml-nodetype-32`, `nested-encoding-32`, `component-media-types-32`
- Codes: `openapi/degraded-construct`, `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryNoReaderTakesIsStillReported`,
  `annotation.TestRead_NodeTypeOnlyADefinedValueIsPlaced`,
  `operation.TestContent_MediaTypeDescriptionIsKept`
- Status: partly built: no reference into `components/mediaTypes` is followed (#856).

### `unreferenced-components`

A compiler lowers a component where a reference finds it, so an entry no reference reaches lowers
nowhere — no node, no `Unmodeled` entry, no diagnostic — and a declaration the document makes
disappears in silence. Every component section with no registry is therefore kept: each entry no
lowering reached is preserved verbatim at
`Document.Unmodeled["openapi:components/<section>/<name>"]` under `no_ir_home`, at the entry's own
pointer, with no diagnostic. The sections are `responses`, `parameters`, `examples`,
`requestBodies`, `headers`, `links`, `callbacks` and `pathItems`; `components/schemas` and
`components/securitySchemes` are lowered unconditionally. 3.2's `components/mediaTypes`, into which
no reference is followed, is kept whole as `openapi:components/mediaTypes`, with an info
`openapi/degraded-construct` at the section. A section is read as the parser reads it, through `<<`
merge keys, so a merged-in entry is kept under its own name.

*Referenced* means a lowering followed a reference to the entry, not that some `$ref` string names
it. The resolution every non-schema reference passes through records each position in the source its
chain lands on, and the retention runs after every other lowering and keeps what that record lacks.
A `$ref` written where nothing follows it — in a response's verbatim `links` map, under an `x-`
extension, inside an example's literal value, inside an entry that is itself not lowered — carries
nothing of its target into the IR, so the target is kept; and a reference spelled another way than
its target's key, percent-encoded or naming this document by its file, still reaches the entry it
resolves to, so that entry is not kept beside its lowering. Deciding this from the `$ref` strings
instead would answer both questions by a reading of its own, and wrongly: it drops the first kind of
target in silence and gives the second two homes. The record is written by lowerings that never read
it, so it cannot change their answers or depend on their order.

`no_ir_home` rather than a boundary, and no diagnostic, are the links precedent generalized: the gap
is one the IR can close by finding a use for the entry, so a promotion pass should read it, and a
document that declares a component is not making a mistake a warning could name. `Reason` is where
that decision lives (§12), which is what keeps a promotion pass from mistaking one for an oversight.

- Cases: `unreferenced-components`, `component-media-types-32`
- Codes: `openapi/degraded-construct`
- Tests: `openapi.TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget`,
  `openapi.TestUnreferencedComponents_ReadAsTheParserReadsThem`,
  `openapi.TestUnreferencedComponents_SelfReferenceByFileNameReaches`
- Status: partly built: no reference into `components/mediaTypes` is followed, so the section is
  kept whole however much of it a document uses (#856), and a section entry whose key is no scalar
  is dropped without a diagnostic (#947).

## 3. Names and values

### `naming`

A name the document spells lowers to `Naming{Source, Canonical}`: the spelling as written, beside
the word sequence §3.2 derives from it, at every position that spells one — `info.title`, a tag, an
`operationId`, a parameter, response-header or property name, a component schema or security scheme
key, an enum member's value as text, a 3.2 server `name`, and under `grouping=path-prefix` a first
path segment. A name with no letter or digit (`***`, `>=`) keeps its `Source` and has no
`Canonical`. A name spelled as the empty string is minted `Hint: "empty"` and no `Source` rather
than passed through (an `operationId` of `""` is read as absent instead,
[`operation-ids`](#operation-ids)), and a property's wire name stays `""`. A component schema keyed
`""` earns no named `TypeID`: it hoists anonymously at its own pointer
(`t/anon/components/schemas/`) with that hint. An entity the document gives no name — an inline
schema, a union variant, a response ([`response-conditions`](#response-conditions)), a server with
no `name` (its URL's words, else `server`), the synthesized `default` and `webhooks` groups —
carries a `Hint` alone, in the same word grammar and composed from its position, each half
neutralized first so an empty parent gives `empty_item`, never `_item`. `Aliases` is never set.

- Cases: `neutral-naming`, `empty-names`
- Tests: `ids.TestComponentSchemaNamedEmpty_SeparatesAnEmptyNameFromNoEntry`,
  `schema.TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs`,
  `schema.TestInlinePosition_HintIsTheSameInBothOrders`
- Status: partly built: a name with no letter or digit reaches an emitter with neither `Canonical`
  nor `Hint`, which #956 tracks for an enum member; union variants and inline schemas ignore their
  `title` as a hint (#662); a union variant naming a position in a path's body takes the pointer's
  name (#729); and a `$ref` into a component request body or header names the subtree beneath its
  target by the reference (#747).

### `values`

A default, `const`, enum member or example lowers to an `ir.Value` read off the YAML node by its
resolved tag alone, never by the type the schema declares (§6): `null`, `bool`, `string`, `list` and
`object` (members in source order, keyed by their text, an alias followed), a `!!binary` to `bytes`,
decoded, and a YAML timestamp to `string` in the spelling written (`2021-1-1` stays `2021-1-1`,
never RFC 3339). A number is the literal's exact decimal text in `Num`, never a `float64`: a
magnitude past float64 or int64 keeps every digit, `.5`, `+5` and `09` take their JSON spellings
(`0.5`, `5`, `9`), `_` separators drop, a YAML integer base resolves (`0644` is `420`, `0x1f` is
`31`, `0b1010` is `10`), and digits, exponent and trailing zeros otherwise stand as written (`10.0`,
`1.10e+3`). A plain scalar that YAML resolves to a string only because it is past float64's range is
still a number when it is a finite decimal literal; a quoted one stays a string. A value no `Value`
holds — `.inf` or `.nan`, a `!!binary` that is not base64, a tag the compiler does not lower
(`!acme`), nesting past the reader's depth bound — is not coerced: a default or example is dropped
with an `openapi/degraded-construct` warning at its position, and a `const` or an enum member is
[`enum-const`](#enum-const)'s. The number reader is the numeric bounds' as well
([`constraints`](#constraints)). A value kept verbatim instead is
[`verbatim-preservation`](#verbatim-preservation)'s.

- Cases: `defaults`, `yaml-timestamp-scalars`, `numeric-precision`
- Codes: `openapi/degraded-construct`
- Tests: `value.TestNumericLiteral_YAMLBases`, `value.TestValueFromNode_Timestamp`,
  `annotation.TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped`
- Status: partly built: a `<<` merge key in a mapping value stays a member named `<<`, where a
  verbatim payload merges it (#395).

## 4. Schemas: identity, references and annotations

### `schema-registry`

Each entry of `components/schemas` lowers to a node of its own in the type registry, whether or not
anything references it, under a `TypeID` derived from its pointer
(`t/openapi/components/schemas/<name>`, §3.1) and named by its key ([`naming`](#naming)). One whose
type is a primitive (`{type: string}`) lowers to a `Scalar` over that primitive, and one that is
only a `$ref` lowers to a `Scalar` alias whose `Base` is its target (§4.2). A reference to the entry
names that ID.

- Cases: `named-types`, `component-reuse`
- Tests: `ids.TestForPointer_ChoosesTheNamespace`

### `inline-hoisting`

An inline schema whose lowering is a node — a model, an enum, a union, a list, a map, a tuple, or a
scalar a format names ([`string-formats`](#string-formats)) — is hoisted into the registry under an
anonymous `TypeID` derived from its own pointer (`t/anon/<pointer>`), with a `Hint` composed from
its position ([`naming`](#naming)), and the position holds a `TypeRef` to it. One whose lowering is
a primitive, `{type: string}` with or without bounds beside it, hoists nothing: the position names
the shared primitive, and what it declares beside the type lands on the property or parameter that
carries it ([`constraints`](#constraints)).

- Cases: `inline-types`, `inline-hoist-positions`, `inline-annotations`

### `recursion`

A schema that reaches itself through `$ref` — directly, through a property, `items`,
`additionalProperties`, a union branch or an inline position, or through another component — lowers
to a `TypeRef` to the ID the schema was registered under before its body lowered, so the cycle lives
in the type graph by ID (§1), whichever declaration lowers first, with no diagnostic. A chain of
`$ref`s that never reaches a node without one (`A: {$ref: B}`, `B: {$ref: A}`) is not recursion but
a cycle with no schema in it, which [`reference-cycles`](#reference-cycles) refuses before anything
lowers.

- Cases: `recursive`
- Tests: `schema.TestLower_RecursiveSchemaTerminates`, `scan.TestDetectCycles_LegalRecursionClean`,
  `openapi.TestCompile_CyclicSpecDoesNotCrash`
- Status: partly built: an `allOf` that names its own schema, or two schemas that are each other's
  `Base`, lowers to a base chain that returns to where it started with no diagnostic, and a cycle of
  required properties that admits no finite value is reported by nothing (#642).

### `ref-use-site`

The keywords written beside a schema's `$ref`, which 3.1 admits, and the annotations its target
declares — documentation, deprecation, visibility and `default` — merge onto the property, parameter
or header that references the target, field by field, a keyword the use site writes winning over the
target's (§12.2), and by the same rule at every carrier. At a position that carries no property,
parameter or header — an `allOf`, `oneOf` or `anyOf` branch, an `items`, a component — the keywords
beside the `$ref` bind an alias hoisted at that position instead (§4.3). Constraints are the
exception: bounds conjoin rather than override, so each position keeps the ones it declared and none
is copied to a use site (§12.2).

- Cases: `param-ref-inheritance`, `allof-ref-branch-siblings`, `readonly-writeonly`
- Tests: `openapi.TestAnnotationRetention`, `schema.TestModel_RefSiblingDescriptionWins`,
  `schema.TestSchemaExamples_ComponentRefSiblingKeepsThem`

### `reference-object-siblings`

A Reference Object's own siblings are the annotations of the use site. OpenAPI lets the entry that
writes a `$ref` carry `summary` and `description` beside it, and the pair describes *that use* of
the referenced object rather than the object. Where the IR has a docs field at the position — a
parameter, a response or error case, a header, an example, a request body, a security scheme — both
are read off the entry and folded over the declaration's docs field by field, a sibling the entry
writes winning and one it omits leaving the declaration's value standing. A component that is itself
a `$ref` (an alias) is a use of what it names, so the fold runs along the whole chain from the
declaration outward: an alias's siblings override the declaration's, and a use site's override the
alias's. The fold applies at every version whose parser presents the siblings, which is all of them:
the Reference Object's wrapper is version-independent, and dropping a declared sibling in silence is
the defect the rule fixes. It is a documented departure from OpenAPI's clause that such a sibling
has "no effect" where the referenced object type does not allow one: the IR has a docs field at each
of these positions, keeping it loses nothing, and the alternative is the silent loss the clause
permitted. OpenAPI 3.0 goes further, admitting no key beside a `$ref` and saying one SHALL be
ignored; there the fold is the 3.1 reading, and each sibling it reads is reported with an
`openapi/later-version-construct` warning ([`later-version-construct`](#later-version-construct)).

- Cases: `ref-site-docs`, `later-version-constructs`
- Codes: `openapi/later-version-construct`

### `use-bound-annotations`

`readOnly` and `writeOnly` lower to the `Visibility` of the property or header that uses the schema
— `readOnly` to `Only: [read, delete, query]`, `writeOnly` to `Only: [create, update]` (§5.2) — and
a schema's `default` to the `Default` of the property, parameter or header that uses it, never to
the type: both bind a *use* of the type, not the type. One a declaration writes, on a component or
any other position that owns a node of its own, is therefore pushed down to each property, parameter
or header referencing it that has a field for it, with use-site precedence
([`ref-use-site`](#ref-use-site)), and the declaration keeps its own copy verbatim under
`no_ir_home`, reported with an info `openapi/degraded-construct` at the keyword. That copy is what a
component nothing references would otherwise lose in silence.

- Cases: `defaults`, `readonly-writeonly`, `inline-residue`
- Codes: `openapi/degraded-construct`
- Tests: `openapi.TestAnnotationRetention_OtherDeclarationShapes`,
  `schema.TestPropertyPosition_ResidueStaysOutOfTheTypeNode`
- Status: partly built: a `$ref` to a property's schema reports `readOnly` as kept on a node that
  lacks it (#750).

### `docs-and-deprecation`

`summary`, `description` and `externalDocs` lower to `Docs`, and `deprecated: true` to
`Deprecation{}`, on the entity whose object writes them. `Deprecation` is an empty marker: the
keyword states no message, version or date, which only [`extension-promotion`](#extension-promotion)
can supply. An operation, and a tag on its `TagDef` and on the group it names, take all three; a
schema's `title` is the `Summary`, beside its `description` and `externalDocs`; a response and an
error case take `description` and `summary` ([`response-summary`](#response-summary)); a request
body, a security scheme, a server and a server variable take `description`, an example `summary` and
`description`, and a security scheme `deprecated` (3.2). `info` and the root `externalDocs` make the
Document's one `Docs`, while the Service takes `info.description` alone. A schema's documentation
lands on the node its position owns; where the position owns none (a shared primitive, a `$ref`), it
lands on the property, parameter or header that carries it, the referent's under the use site's
field by field ([`ref-use-site`](#ref-use-site)). A parameter's or header's own `description` wins
over its schema's. `deprecated: true` on a parameter or header and on its schema each deprecate it,
neither one's `false` clearing the other. A request body's `description` lands on its Payload
([`request-body`](#request-body)), and a response's payload states no docs of its own.

- Cases: `docs-summary-desc`, `deprecation`
- Tests: `operation.TestOperation_DeprecatedIsCarried`,
  `operation.TestHeaders_DeprecationUnionsWithTheSchema`,
  `operation.TestContent_RequestBodyDocsReachThePayload`
- Status: partly built: a deprecation cannot name its replacement, which the IR has no field for
  (#670).

### `disjoint-visibility`

A field both `readOnly` and `writeOnly` — on one schema, across a `$ref` whose site sets one flag
and referent the other, or on inline `allOf` branches that redeclare it — admits no lifecycle. It
lowers as declared, to `Visibility{None: true}` (§5.2), and is reported once, as an
`openapi/disjoint-visibility` warning at the invisible property or at the schema of the invisible
header, or for `allOf` at the redeclaration that empties the set (a branch repeating a field already
invisible reports nothing more). It is not a conflicting redeclaration
([`allof-redeclared-property`](#allof-redeclared-property)), which keeps one declaration over
another, nor a degraded construct: `None` states the intersection exactly, and the warning says only
that no request or response can carry the field. A flag set `false` states nothing. A pair written
where no `Visibility` can land — an `items` schema, a parameter's schema — is kept verbatim and not
reported ([`parameter-schema-keywords`](#parameter-schema-keywords)); a component's is reported at
each property that references it.

- Cases: `readonly-writeonly`
- Codes: `openapi/disjoint-visibility`
- Tests: `schema.TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere`,
  `openapi.TestAllOfVisibilityMerge_DisjointRestrictionsAreInvisibleNotAConflict`,
  `merge.TestReconcileProperty_AlreadyInvisibleVisibilityIsNotReAnnounced`

### `nullable`

Whether a position admits null is a question about its whole schema, since JSON Schema conjoins
keywords, and the answer lowers to `TypeRef.Nullable` on the reference that names the position's
type — a property's, a parameter's, a list's element, a union variant — never to a node of its own
(§3.3). A schema admits null where 3.0's `nullable: true` says so or 3.1's `type` names `null`, and
only where the keywords beside agree: an `enum` admits null only where it lists `null`, which then
names no member, so a non-empty `enum` listing `null` admits it with no `type` beside it, and one
not listing it takes a `type: [string, "null"]` back out; an `allOf` admits null only where every
conjunct does, the null sitting on the reference that names the composition rather than on its
`Base` or a `Mixin`; and a union's variant that admits null is marked on that variant's reference.
Whether a property must be present is `Property.Required`, apart from this (§5.1), so required and
nullable make four distinct lowerings. A union of one branch and `null` collapses to a nullable
reference ([`union-null-variant`](#union-null-variant)).

- Cases: `nullable-30`, `nullable-31-ref`, `nullable-enum-31`, `nullability-four-states`,
  `nullability-conjunction`
- Tests: `schema.TestSchemaRef_NullableNormalization`

### `schema-examples`

A schema's `example`, then each entry of its `examples` in order, lowers to `Examples`, one
`Example` apiece holding the value ([`values`](#values)), on the node the position owns or, where it
owns none, on the property that carries it. An example written beside a `$ref` describes the
position, not the referent, whose own examples stay on its node. A value no `Value` holds is dropped
with an `openapi/degraded-construct` warning at it.

- Cases: `examples`
- Codes: `openapi/degraded-construct`
- Tests: `annotation.TestSchemaExamplesAt_ReadsBothKeywordsInOrder`,
  `schema.TestSchemaExamples_ComponentRefSiblingKeepsThem`

### `xml-hints`

A schema's `xml` object lowers to `XMLHints` on the node the position owns, or, where it owns none,
on the property that carries it, the property's hints its own beside its type's (§5.3): `name`,
`namespace`, `prefix` and `wrapped` as written, and the node type from 3.2's `nodeType`
([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)) or, where none is written, from
`attribute: true` as `attribute`. An `attribute` flag that a `nodeType` beside it contradicts, which
3.2 forbids, is kept verbatim under `openapi:xml/attribute` (`degraded_lowering`) with an
`openapi/degraded-construct` warning at it, and the `nodeType` is placed. The object's `x-*`
extensions are kept as `openapi:xml/x-*` ([`vendor-extensions`](#vendor-extensions)), and the hints
of a parameter's schema, which `ir.Parameter` has no field for, are
[`parameter-schema-keywords`](#parameter-schema-keywords)'.

- Cases: `xml-hints`, `xml-nodetype-32`
- Codes: `openapi/degraded-construct`
- Tests: `annotation.TestXMLHints_ReadsEveryFieldDirectly`, `schema.TestModel_PropertyXML`
- Status: open: whether a property's hints override its type's field by field or whole (#465).

## 5. Schemas: composition, unions and enumerations

### `allof-classification`

An `allOf` lowers to the composing `Model`'s `Base` and `Mixins` (§4.3). An entry that is a `$ref`
to a schema taking part in a discriminator hierarchy, or the sole `$ref` entry, becomes `Base`;
every other `$ref` entry becomes a `Mixin`; and an inline entry merges into the composing model's
own `Properties`, each property keeping its own provenance
([`allof-inline-residue`](#allof-inline-residue),
[`allof-redeclared-property`](#allof-redeclared-property)). So every branch stays reconstructible
from the composition.

- Cases: `allof-inheritance`, `allof-mixins`, `allof-inline-merge`
- Tests: `schema.TestAllOf_DiscriminatorHierarchy`, `schema.TestAllOf_SoleRefBecomesBase`
- Status: partly built: a `$ref` to a subtype of a discriminated base, which declares no
  discriminator itself, is composed as a `Mixin` rather than the `Base` (#986).

### `allof-beside-ref`

An `allOf` written beside a `$ref` at the same level is not composed. A `$ref` site lowers to the
alias over its target (§4.3), which elects no composition family for the `allOf` to win or lose, so
the alias's census of the keywords it cannot hold ([`shape-applicator`](#shape-applicator)) keeps
the `allOf` verbatim under `openapi:allOf` (`degraded_lowering`), with an info
`openapi/degraded-construct` at the position.

- Cases: `allof-ref-branch-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestRefSiteKeywords_KeptAtEveryPosition`

### `allof-inline-residue`

A branch written inline rather than as a `$ref` owns no node, so §4.3's composition has nowhere to
point and the branch is merged into the composing model in place. That merge consumes the branch's
`properties`, its `required` list, and a `type` that is exactly `object` — which the composed
`Model` already restates — and nothing else. The IR could hold every other keyword a branch writes,
and does hold them one level up — the same keywords on the composing schema lower to `Docs`,
`Model.Additional`, `Model.Constraints` and `Unmodeled` — but attributing them to the composition
means either giving the branch a node (changing the composition shape) or merging them upward with a
precedence rule for branches that disagree. So a branch declaring anything the merge does not
consume is kept verbatim in `Unmodeled["openapi:allOf/<index>"]` under `degraded_lowering` — the
*whole* branch, consumed keywords included, so the source stays reconstructable rather than having
to be reassembled from two places — keyed by branch index so siblings cannot overwrite each other,
and reported by an `openapi/degraded-construct` at the branch naming the keywords that made it
residue. Which keywords those are is derived from the branch's own declared key set rather than from
a fixed list, so a keyword the dialect gains later is residue without anyone maintaining an
enumeration. The key set is read through YAML aliases and merge keys, so a branch written
`[*shared]` or `[{<<: *shared}]` derives the same residue as one written out. The diagnostic is an
info, and a warning where the branch declares a `type` that excludes `object`, because the composing
`Model` then asserts a shape the source contradicts — `allOf: [{type: string, maxLength: 3}]`
composes an empty model of kind `model` over a source that says `string`. Two branch kinds are
outside this rule rather than limits of it: a `$ref` branch owns a node, so what is written beside
the `$ref` is *modeled* on the branch alias §4.3 describes rather than kept as residue, and a
boolean branch declares no keywords at all, so there is no residue to derive — it is a decision
about the composed node instead, stated with the `false`-schema rule
[`allof-false-branch`](#allof-false-branch).

- Cases: `allof-inline-residue`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestAllOf_BranchWrittenByReferenceStillDerivesResidue`,
  `schema.TestAllOf_MergedBranchKeywordsAreNotResidue`,
  `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`
- Status: partly built: a nested inline `allOf` is kept as residue rather than flattened into the
  composition, so the composed model has no base, mixins or properties (#599), and an `allOf` of one
  `$ref` and an annotation-only branch keeps none of the branch's annotations typed (#660).

### `allof-redeclared-property`

`allOf` intersection folds a field declared in more than one branch into one `Property`, and the
first declaration wins its shape (§4.3). The IR has no combinator for "string here, integer there",
nor a second slot for a default, description, examples, deprecation or XML hint the first
declaration already fills, so whatever of the redeclaration the fold cannot carry has nowhere to sit
on the merged property, and with nowhere to sit it would go nowhere at all: a release in which a
losing branch's type or default changed would produce no change in the document, which a consumer
diffing two versions reads as nothing having happened. The losing declaration is therefore kept
verbatim, as the document wrote it, in `Unmodeled["openapi:conflicting-redeclaration<pointer>"]`
under `degraded_lowering`, keyed by its own JSON Pointer so a field three branches type three
incompatible ways keeps all three entries, and with the entry's `Provenance` locating that
declaration rather than the merged property. The source node is kept rather than the IR values the
merge dropped: §12 defines an `Unmodeled` value as the source construct, a `TypeID` is a
compiler-minted registry ID that `irverify`'s reference walk cannot see dangle inside a byte slice,
and one node keeps everything the redeclaration said at once — a `$ref` as the `$ref` written, a
nullability, a default.

Preservation is owed wherever something is dropped, which is wider than the disagreements worth a
warning. The two declarations are reported as an `openapi/conflicting-redeclaration` warning only
where they are genuinely unsatisfiable — an incompatible type, or one constraint keyword pinned to
two values; a detail the first declaration already holds differently is named in an info
`openapi/degraded-construct`; and two composites of one kind, an unresolvable target, and the top
type against anything are dropped without any diagnostic and kept all the same. The loser's
`default`, constraints and examples go with its type rather than folding onto the winner — they
describe the shape that lost, and adopting them would leave one field asserting two contradictory
things, with nothing downstream comparing a value's kind to its property's type; this is keyed on
the type being dropped, not on the drop being called a conflict. Two declarations naming the same
target and differing only in nullability are not a drop and take no entry: the merged field admits
null only where both branches do, which is the same conjunction a single schema's null verdicts fold
under ([`nullable`](#nullable)).

- Cases: `allof-conflicting-type`
- Codes: `openapi/conflicting-redeclaration`, `openapi/degraded-construct`
- Tests: `merge.TestReconcileProperty_NullabilityIntersectsWhenTargetsAgree`,
  `merge.TestReconcileProperty_ADroppedTypeIsKeptEvenWhenItDoesNotConflict`,
  `merge.TestReconcileProperty_ALosingDetailIsKeptAndReported`
- Status: partly built: a primitive a `format` narrows is reported as conflicting with the bare
  primitive, which it does not contradict (#446); an `allOf` that narrows a property's type is
  reported as a conflict and resolved by branch order (#598), as is a redeclaration with an unknown
  format (#782); a property redeclared as a list and as a tuple is reported as a conflict (#899);
  and a conflict goes unreported when the other type lowers later (#898). Open: intersecting the
  redeclared bounds and defaults rather than keeping one (#10).

### `allof-required`

A `required` name written on an `allOf` branch, inline or beside a branch's `$ref`, or beside the
`allOf` itself lowers to `Property.Required` (§5.1) on the composed `Model`'s own property of that
wire name, whichever branch declares the property: a branch holding nothing but `required` attaches
across the whole composition, not only its own `properties`, and an attachment never clears a
`Required` already set. A branch is read from its local schema, never from its `$ref` target, whose
`required` list belongs to the target's own model. A name that matches no property the composed
`Model` declares — one only a `Base` or `Mixin` declares, or one nothing declares — has no field to
land on, because a `Model` stores its own properties and no flattened view across `Base`/`Mixins`
(§4.3). It is reported as an `openapi/unattachable-required` at the branch or schema that wrote it,
a warning where the `Model` composes a `Base` or `Mixin` that may declare it, an info where it
composes none.

- Cases: `allof-required-only`
- Codes: `openapi/unattachable-required`
- Tests: `schema.TestAllOf_CompositionRequiredAttaches`,
  `schema.TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyDiagnosed`,
  `schema.TestAllOf_RequiredOnlyBranchNoBaseOrMixinDiagnosedInfo`
- Status: partly built: the unattached name is reported and kept nowhere, where a map keeps its
  `required` verbatim (#896), and an inherited property a composing schema makes required has no IR
  home (#663).

### `allof-false-branch`

The `false` rule ([`whole-schema-fallbacks`](#whole-schema-fallbacks)) holds wherever the schema
appears, composition included. A `false` conjunct (`allOf: [false, …]`) admits nothing, so the
composition admits nothing: the composed `Model` is closed and the branch is kept verbatim beside
it, under `openapi:allOf/<index>` (`degraded_lowering`), which is what distinguishes it from a model
that merely wrote `additionalProperties: false`, and reported as an info `openapi/false-schema` at
the branch. What the other branches contributed stays — closing is the nearest shape the IR has, and
emptying the model would trade one silent loss for another. A `true` conjunct admits everything, so
contributing nothing from it is exact and nothing is reported.

- Cases: `allof-boolean-branch`
- Codes: `openapi/false-schema`

### `union`

A `oneOf` lowers to a `Union` with `Exclusive` set and an `anyOf` to one without it (§4.4): one
`Variant` per branch, in source order, each a reference to the branch's lowering — a `$ref` branch
to its target, an inline one to its own node or primitive ([`inline-hoisting`](#inline-hoisting)) —
and never collapsed into optional fields. The union is untagged, `WireTagged` false, and a
`discriminator` beside it routes it ([`discriminator`](#discriminator)). A branch set of one schema
and `null` ([`union-null-variant`](#union-null-variant)), of `null` alone
([`union-null-only`](#union-null-only)), and one written beside structure, a `$ref` or the other
combinator ([`union-beside-structure`](#union-beside-structure),
[`union-beside-ref`](#union-beside-ref), [`competing-keywords`](#competing-keywords)) lower
otherwise.

- Cases: `oneof-discriminated`, `anyof-untagged`
- Status: open: a discriminated union lowers with `WireTagged` false, a combination the IR's tag
  modes do not define (#954).

### `type-arrays`

A schema whose `type` lists more than one type lowers to an exclusive `Union` with one variant per
listed type other than `null`, in order, each lowered from the same schema as that type alone and
named by it: a shared primitive for a scalar type, and for `object` or `array` a node hoisted at
`<pointer>/type/<index>`. A `null` among them makes the position nullable instead
([`nullable`](#nullable)), so one type and `null` lower as that type alone, nullable. The bounds a
carried schema writes land on its property or parameter ([`constraints`](#constraints)). No keyword
census runs over the union, since each variant is built from the whole schema and holds what its own
type admits ([`shape-applicator`](#shape-applicator)).

- Tests: `schema.TestLower_MultiTypeUnion`,
  `schema.TestDiscriminatorBesideUnion_MultiTypedBodyIsSettled`
- Status: partly built: a keyword or bound that no variant holds, as at a component, where no
  carrier takes a bound, is dropped in silence (#852); a list naming `integer` and `number` lowers
  to an exclusive union that no integer satisfies, since every integer is a number (#868); and a
  multi-typed base's discriminator routes to no subtype (#853).

### `union-null-variant`

A `oneOf` or `anyOf` with exactly one branch that is not a bare `type: null`, beside one or more
that are, lowers to a nullable reference to that branch's lowering rather than to a `Union` (§3.3).
The collapse is held to a `$ref` site's rule (§4.3), since it too lowers to a reference: a keyword
beside it that no reference holds — a `discriminator`, a `format` other than `password`, a
collection bound — is kept verbatim under `openapi:<keyword>` (`degraded_lowering`) on the node or
carrier owning the position, with an info `openapi/degraded-construct` at the position.

- Cases: `nullable-union-discriminator`, `nullable-31-ref`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold`,
  `schema.TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition`

### `union-null-only`

A `oneOf` or `anyOf` whose every branch is a bare `type: null` lowers to the same nullable `any` a
bare `{type: null}` schema at that position lowers to. No node built for it carries a branch set, so
the combinator, and the other one too where both are written, is kept verbatim under `openapi:oneOf`
or `openapi:anyOf` (`degraded_lowering`), with an info `openapi/degraded-construct` at the schema.

- Cases: `null-only-union`
- Codes: `openapi/degraded-construct`

### `union-beside-structure`

A `oneOf` or `anyOf` written beside structural keywords
(`{type: object, properties: {…}, oneOf: [{$ref: A}, {type: string}]}`) conjoins with them, so the
schema is an *intersection* of a structural type and a union. The IR has `Union` but no intersection
combinator: §4.3's `Base`/`Mixins` composes one referent per conjunct, the shape `allOf` writes, and
cannot state "conjoined with a union", and §15 records why none was added. Target languages are not
the constraint here; TypeScript writes `{a: string} & (X | Y)` directly. Where the conjunction
*distributes*, §4.3 gives the rule: the body is composed into each variant, and the result is an
ordinary `Union` of composed `Model`s, reported as an info `openapi/composition-lowering` at the
schema, since nothing is lost but the IR's shape no longer mirrors the source's. Five shapes
distribution cannot reach:

1. the structural body is not a `Model` (a `const`, an `enum`, a scalar, or a bare
   `required`/`items`/`prefixItems`/`additionalProperties`/`patternProperties` with neither
   `properties` nor a `type`), so there are no composition fields for the variants to carry;
2. a branch is written inline rather than as a `$ref`, so it names no node for `Base`/`Mixins` to
   point at — a `type: null` branch included;
3. a branch *is* a `$ref` but names no referent the compilation resolves — a cross-document target
   it does not intern, an undeclared component, a pointer addressing no schema. The branch is
   unusable for the same reason an inline one is, so it declines the same way rather than composing
   a variant whose branch quietly went missing;
4. a `discriminator` is declared, binding the branch schemas by name;
5. `oneOf` and `anyOf` are both declared, so distributing one would drop the other.

In all five, whatever structural body the schema declares is lowered — `properties` or a declared
`type` yields a `Model` or typed node — and the union branches are kept verbatim in
`Unmodeled["openapi:oneOf"]` / `["openapi:anyOf"]` (`degraded_lowering`), reported as an info
`openapi/degraded-construct` at the schema naming which of the five applied; in case 3 the broken
reference is reported as well, as an `openapi/unresolved-ref` error. Case 1's body-less sibling
leaves nothing to build: it reduces to `any`, and the sibling itself is kept by
[`shape-applicator`](#shape-applicator) rather than dropped, so the union's diagnostic and the
sibling's are both reported. A `null` branch in this shape is **not** lifted to `Nullable`: the
union it belongs to is no longer structural, so lifting one branch out of it would assert something
the lowered type does not say. The degradation is that the generated SDK is *under-constrained* — it
admits values the union would reject — which is why the branches must survive for a validating
emitter to reach.

A `discriminator` beside any of the five whose body lowers to a `Model` stays on that `Model` only
when every type it routes to is a subtype of it at any depth (`ir.IsSubtype`, the rule
`pass/validate` holds a model discriminator to): each `mapping` target, the `defaultMapping`, and
each component a `$ref` branch names, which it routes to by name — `oneOf`'s branches where `oneOf`
and `anyOf` are both declared, since `oneOf` wins. An inline branch, or one naming no component, is
no route unless the `mapping` names it. Otherwise the whole object (`propertyName`, `mapping`,
`defaultMapping`) is kept verbatim in `Unmodeled["openapi:discriminator"]` on the node keeping the
union, with an info `openapi/degraded-construct` at the discriminator; a subtype keeps the
`DiscriminatorValue` its mapping key gives it, since that is still the tag its instances carry, and
a body the move leaves holding a map and nothing else lowers to that map's `MapT` (§4.6). A union
whose branches declare no shape (`validation_only`) is held to the same rule, and so is a
multi-typed body, which lowers to a `Union` whose object variant, built from the same schema, holds
the discriminator. The compiler decides once every node is interned (`compile.Settlement`), because
neither the walk nor the source schemas can answer it: a subtype declared after the `Model` has no
`Base` while the `Model` lowers, and a schema whose `allOf` names the `Model` may still lower to no
subtype — an `enum`, a `const`, an alias over a `$ref` beside it, or a union its `allOf` is
distributed across.

A union whose branches declare no shape at all (`oneOf: [{required: [a]}, {required: [b]}]`) is not
a degradation and is not one of the five: it narrows the body without reshaping it, which is
validation logic, so it is kept under `validation_only` with §4.7's keyword family and reported as
an info `openapi/validation-only-keyword` at the schema ([`validation-only`](#validation-only)).

- Cases: `allof-oneof-cooccurrence`, `discriminated-union-beside-model`, `unhomed-keywords`
- Codes: `openapi/composition-lowering`, `openapi/degraded-construct`, `openapi/unresolved-ref`,
  `openapi/validation-only-keyword`
- Tests: `schema.TestOneOf_CoDeclaredNotDistributedReasons`,
  `schema.TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed`,
  `schema.TestDiscriminatorBesideUnion_LeavesAMapWhenItMoves`
- Status: partly built: restating a property's type inside xor-of-required branches degrades the
  union (#620), and a multi-typed base's discriminator routes to no subtype (#853). Open: keeping a
  discriminated `oneOf` a union when structural keywords sit beside it (#594), and distributing a
  union's inline branches when properties sit beside it (#661).

### `union-beside-ref`

A `oneOf` or `anyOf` written beside a `$ref` at the same level, rather than beside a structural
body, has nothing at that position to distribute the union across either: it lowers to the alias
over the `$ref` target with the union kept verbatim beside it, under `openapi:oneOf` or
`openapi:anyOf` (`degraded_lowering`), through the same keeper as the structural-body case, and
reported as an info `openapi/degraded-construct` at the position.

- Cases: `union-beside-ref`
- Codes: `openapi/degraded-construct`

### `competing-keywords`

Keywords conjoin, so `{allOf: [{$ref: Base}], enum: [a, b]}` is a narrowing of `Base` rather than a
malformed document, and `{oneOf: […], anyOf: […]}` demands both, yet a position lowers to one node.
Three families compete for it: the value keywords `const`, `enum` and `allOf`, elected in that
order; the two union combinators, where `oneOf` wins; and a parameter's or header's `schema` beside
its `content`, where `content` wins, since a media-type entry names both a schema and the media type
serializing it and the IR models both ([`content-parameters`](#content-parameters)). The IR has no
intersection combinator (§15, as for [`union-beside-structure`](#union-beside-structure)), so the
position lowers as the elected keyword — the most it can express there — and each keyword the
election passed over is kept verbatim in `Unmodeled["openapi:<keyword>"]` under `degraded_lowering`
at that keyword's own pointer, reported as one `openapi/degraded-construct` naming what was elected
and what was kept: an info, and a warning where a parameter's or header's `schema` is passed over
for its `content`. Two consequences are deliberate. The elected combinator still becomes a `Union`
rather than degrading to the top type, because that `Union` is a shape the IR *can* express and
discarding it as well would model nothing at all. And a `{X, null}` `oneOf` written beside an
`anyOf` does **not** collapse to nullable `X` (§3.3), because the collapse asserts the position *is*
nullable `X` while the co-declared `anyOf` conjoins with it, and collapsing would resolve the
position to a shared primitive, which must never carry one declaration's keywords. Where a
*structural* sibling is written as well, [`union-beside-structure`](#union-beside-structure) applies
instead and both branch sets are kept. A keyword the *elected* lowering never reads — `type: string`
beside an `allOf`, `format` beside a `const` — is kept by the census
[`shape-applicator`](#shape-applicator) runs, which asks the node that was built: `type: object`
beside an `allOf` is one the composed `Model` already asserts, and keeps nothing.

- Cases: `codeclared-keywords`, `codeclared-schema-content`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestCoDeclaredFamily_EveryPassedOverKeywordIsKept`,
  `schema.TestCoDeclaredFamily_UnpreservableIsNotAnnounced`,
  `schema.TestUnionCombinators_UnpreservableIsNotAnnounced`

### `discriminator`

A `discriminator` on a schema that lowers to a `Model` or a `Union` lowers to its `Discriminator`.
On a `Model` whose own properties include the one `propertyName` names, the tag is that property's
`PropID`, in `Discriminator.Property`; otherwise, and on every `Union`, it is the name as written,
in `Discriminator.PropertyName`. Each `mapping` entry lowers to its key and the `TypeID` its target
lowers to, the target named by reference or by bare schema name, and 3.2's `defaultMapping` to
`Discriminator.Default`. `Inferred` is not set: the document declares the discriminator. A mapping
target that resolves to no schema is reported as an `openapi/unresolved-ref` error at the entry,
which the `Mapping` leaves out. A subtype the mapping names under several keys — an alias tag —
takes the smallest in byte order as its `DiscriminatorValue`
([`discriminator-value`](#discriminator-value)), since the field holds one and a mapping's key order
is not a property of the document, with every key kept in the base's `Mapping` and the election
reported as an info `openapi/degraded-construct` at the subtype. A discriminator beside a node that
routes by none is kept verbatim by [`shape-applicator`](#shape-applicator), and one beside a union
written with structure is [`union-beside-structure`](#union-beside-structure)'s.

- Cases: `oneof-discriminated`, `discriminator-inheritance`, `discriminator-default-mapping`,
  `discriminator-alias-mapping`, `nullable-union-discriminator`
- Codes: `openapi/degraded-construct`, `openapi/unresolved-ref`
- Tests: `schema.TestHomelessDiscriminator_IsKeptBesideEveryOtherNode`,
  `schema.TestUnplacedDiscriminator_IsKeptAtEveryUnionSite`
- Status: partly built: an empty `propertyName` lowers to IR that `irverify` refuses (#882); a
  mapping to a component that is a `$ref` to a subtype fails validation (#844), a keyword beside a
  union branch's `$ref` breaks a mapping to its component (#847), and a mapping to a property whose
  `$ref` has a sibling names an unused alias (#764); and a discriminator inside a reached path item
  is recorded at a pointer with an empty segment (#886). Open: inferring a discriminator from pinned
  tag values, as an injectable policy (#659).

### `discriminator-value`

A subtype's wire tag lowers to `Model.DiscriminatorValue`, left empty on the base itself. It is read
off the discriminators of the subtype's `allOf` ancestors — the `$ref` branches' targets, then
theirs, level by level to the walk's depth bound, nearest first, a schema that declares none passed
through and no schema its own ancestor — as the key an ancestor's `mapping` spells for the subtype,
the mapping naming it by reference or by bare schema name, an inline subtype by its position's
reference, and where several keys name it [`discriminator`](#discriminator) elects one. A key of
`""` is a tag, not an absence. Once no ancestor's mapping names it, the tag is the subtype's schema
name as declared (`Cat/Dog`, not its pointer token) if it is a component schema, OpenAPI's implicit
mapping. A subtype that is no component schema — inline as a property or a union branch, or under an
extension — has no name to answer to, so only a mapping entry tags it, and a chain with no
discriminated ancestor carries no tag however deep. A variant of a distributed `oneOf`/`anyOf`
([`union-beside-structure`](#union-beside-structure)) carries the tag of the schema enclosing it.

- Cases: `discriminator-transitive`, `discriminator-inline-subtype`
- Tests: `schema.TestAllOf_DiscriminatorSubtypeValue`,
  `schema.TestAllOf_DiscriminatorValueFromNearestAncestor`,
  `schema.TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue`

### `string-const-union`

A `oneOf` or `anyOf` whose every branch is a string `const` is to normalize to a closed `Enum` in a
`pass/` normalization, not in the compiler, so that each branch's `Docs` survive until a pass
chooses the collapse.

- Status: not built: the compiler keeps such a union a `Union` of `Literal`s with each branch's
  docs, and no pass normalizes it to an `Enum` (#990).

### `enum-const`

An `enum` lowers to a closed `Enum` at any member count, one member included, each entry a member
holding its value ([`values`](#values)) in source order (§4.5); a `null` entry beside others is no
member but makes the position nullable ([`nullable`](#nullable)). An enumeration an `Enum` cannot
hold — a list or object member, members of different kinds, or only `null` — lowers to an exclusive
`Union` of `Literal`s instead, reported as an info `openapi/degraded-construct` at the schema, and a
member whose value cannot convert makes the enumeration such a union, its own variant the top type,
reported as an `openapi/degraded-construct` warning at the member. `enum: []` lowers to a closed
`Enum` with no members, a position accepting no value, reported as an `openapi/empty-enum` warning.
An entry repeating an earlier entry's value, compared as JSON Schema compares instances, is kept,
each occurrence a member of its own (§4.5), and reported as an `openapi/duplicate-enum-value`
warning at the repeat. An `enum` past `Limits.MaxEnumMembers` lowers to the top type, with an
`openapi/budget-exceeded` error at the schema. A `const` lowers to a `Literal` of its value, and one
that cannot convert to the top type, with an `openapi/degraded-construct` warning at the schema.

- Cases: `enum-string`, `enum-numeric`, `empty-enum`, `enum-member-identity`, `nullable-enum-31`,
  `literal-const`
- Codes: `openapi/empty-enum`, `openapi/duplicate-enum-value`, `openapi/degraded-construct`,
  `openapi/budget-exceeded`
- Tests: `schema.TestEnum_HeterogeneousBecomesUnionWithBadValue`,
  `schema.TestEnum_NonScalarAndMidListMismatch`,
  `schema.TestEnumAsUnion_UnconvertibleMemberBecomesAny`
- Status: partly built: an `enum` ignores its `format`, though `Enum.ValueType` can carry it (#607);
  an enum whose members do not match its declared type lowers to a union with only an info
  diagnostic (#619); a `Literal` carries no value type or format (#668); and a `$ref` into an enum
  member lowers it as a schema in one declaration order (#730). Open: whether a mixed-type enum's
  `null` lifts out of its union (#618).

## 6. Schemas: objects, arrays and scalars

### `additional-properties`

`additionalProperties: false` lowers to `Model.Additional` closed, and `additionalProperties: true`
or a schema to `Model.AdditionalProps`, whose `Value` types every key no property names. An object
whose one member fact is that catch-all lowers to a `MapT` instead (§4.6).
`unevaluatedProperties: false` lowers to `Additional` closed after composition. `minProperties` and
`maxProperties` lower to the `Model`'s `Constraints`, which bound the property set's cardinality as
against `Additional`'s openness, or to a `MapT`'s, which bound its entry count.

`true` writes a catch-all; an absent keyword does not. In JSON Schema, `additionalProperties: true`
is the empty schema, so it types the catch-all as the top type exactly as `{}` does: alone it is a
`MapT` of the top type, and beside properties an `AdditionalProps` of the top type. An absent
`additionalProperties` states nothing, so `{type: object}` stays a `Model` with no properties and
`Additional` unspecified; whether such an object is a free-form map is a target's policy, not a fact
the source declared. The one catch-all the source does not write is beside `patternProperties`:
`AdditionalProps.Value` is not optional, and a key no pattern matches is unconstrained, so it is the
top type, and beside patterns the IR does not tell an absent `additionalProperties` from `true`.
That object stays a `Model`, since a `MapT` has no field for a pattern-scoped value.

- Cases: `maps`, `constraints`, `allof-position-constraints`
- Tests: `schema.TestObject_CatchAllBesideAnotherFactStaysModel`,
  `schema.TestObject_MapAndNothingElseLowersToMapT`,
  `schema.TestModel_UnevaluatedPropertiesClosedAfterComposition`
- Status: partly built: `additionalProperties: false` beside `patternProperties` also carries a
  catch-all typed as the top type, which the closure contradicts (#987), and an
  `unevaluatedProperties: false` that a catch-all beside it makes inert in JSON Schema still lowers
  to a closure, which keeps the object a `Model` (#862).

### `pattern-properties`

Each `patternProperties` entry lowers to one `AdditionalProps.Patterns` entry, in source order: its
pattern as written, and its schema lowered as a property's would be. The object stays a `Model`,
whose catch-all beside the patterns is [`additional-properties`](#additional-properties)'.

- Cases: `maps`
- Tests: `schema.TestModel_PatternPropertiesOrder`

### `missing-array-items`

A `type: array` schema with no `items` in a 3.0 document, where 3.0 requires one, lowers as 2020-12
reads it, a `List` over the top type, and is reported once as an `openapi/missing-array-items`
warning at the schema's own pointer, wherever it sits (a property, a parameter's schema, an `items`,
a union branch) and whatever it lowers to (a `Tuple`, an enum of arrays), since the report reads the
declaration and not the node. Nothing is dropped or degraded, so no `openapi/degraded-construct`
accompanies it. `items: {}` is silent, as is every schema at 3.1 and 3.2, where `items` is optional.
Nor is a schema reported beside a `$ref`, whose siblings 3.0 ignores, an inline `allOf` branch,
which merges in place, or a subschema kept verbatim, such as one under `not`.

- Cases: `missing-array-items`
- Codes: `openapi/missing-array-items`
- Tests: `schema.TestLower_MissingArrayItems`, `schema.TestLower_ListWithoutItems`,
  `schema.TestLower_MissingArrayItemsLoweredTwiceWarnsOnce`

### `tuples`

`prefixItems` lowers to a `Tuple`'s `Elems`, one per position, and `minItems`, `maxItems` and
`uniqueItems` beside it to the `Tuple`'s `Constraints`. Its `Rest` is read from the `items` beside
`prefixItems`, which 2020-12 applies to every element past them: `items: false` leaves no `Rest`, an
absent `items` reads as `true`, the top type, and either of those or a schema lowers exactly as a
`List`'s `items` would, at the same pointer and under the same hint. An `unevaluatedItems` beside
them stays the verbatim entry [`validation-only`](#validation-only) keeps, as it does beside a
`List`, reported as an info `openapi/validation-only-keyword`.

- Cases: `tuples-prefixitems`
- Codes: `openapi/validation-only-keyword`
- Tests: `schema.TestLower_TupleRest`
- Status: partly built: `items: true` and an absent `items` lower alike, though only the first makes
  an `unevaluatedItems` beside them inert, and that one is still kept as live (#862).

### `shape-applicator`

`properties`, `patternProperties`, `additionalProperties`, `required`, `items` and `prefixItems`
each constrain an instance whether or not a `type` is written beside them, and each has exactly one
IR home: a `Model`'s property set, openness and pattern bindings, or a `List`'s element and a
`Tuple`'s positions and rest. A `MapT` homes an object's catch-all and the empty `properties`,
`patternProperties` and `required` lists that left it a map (§4.6), but not a `required` that names
a key. A position whose lowering produced none of these — an `Enum`, a `Literal`, a `Scalar`, or the
top type — has nowhere to put them, and so does a position whose lowering produced the other of the
two shapes (`{type: object, properties: {…}, items: {…}}` lowers to a `Model`, which holds no
element type). This covers both a contradictory schema, where one half is taken and the other must
stay recoverable (`{type: string, enum: [a, b], properties: {f: …}}` is not satisfiable, so lowering
to the enum is a reasonable degradation), and an applicator written with no type at all, where the
position lowers to the top type, which says strictly less than the applicator does
(`{items: {type: string}}` constrains every array instance). The IR could model the untyped case by
reading the applicator as the shape it implies — an untyped `items` as a list — but that is a
lowering decision this document has not taken, and keeping the source leaves it open.

Each unhomed keyword is kept verbatim in `Unmodeled["openapi:<keyword>"]` under `degraded_lowering`,
reported as one info `openapi/degraded-construct` at the position naming the kind it lowered to and
the keywords it cannot hold. Which keyword has a home is asked of the node the lowering produced,
never re-derived from the dispatch that produced it. A position that reduced to a shared node gets
an alias of its own first: a shared primitive must never carry one declaration's keywords. The
`format` keyword joins the list wherever the lowered node does not pair it with a primitive — no
`type` declared, or an object, an array or an enum — since it is homed by pairing with a type rather
than by a node kind, and `password` is never unhomed ([`password`](#password)). The census reads
`type`, `const`, `enum`, `minItems`, `maxItems` and `uniqueItems` beside these, so a collection
bound on an object is kept the same way, and a keyword the elected lowering of
[`competing-keywords`](#competing-keywords) never reads is too. `discriminator` joins the list,
homed by a `Model` or `Union` that routes by it and by nothing else, so one beside any other node —
the top type, an enum, a literal, a scalar, an alias over a `$ref` — is kept the same way. At a
`$ref` position the census runs on the alias §4.3's use-site rule hoists, since the siblings bind
the position rather than the referent. One limit is deliberate: a multi-typed schema
(`type: [string, object]`) is exempt, because it lowers one variant per declared type from the same
schema and the homes are the variants'.

- Cases: `unhomed-keywords`, `empty-enum`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestObject_MapKeepsARequiredListVerbatim`,
  `schema.TestUnhomedApplicator_KeptOnAListAndAnnounced`,
  `schema.TestUnhomedApplicator_FormatWithNoTypeIsKept`
- Status: partly built: a keyword beside a multi-typed schema that no variant holds is dropped in
  silence (#852). Open: whether `format: set` on an array reads as `uniqueItems` (#638).

### `string-formats`

A `type: string` with a `format` the table names lowers to the shared primitive the pairing selects,
and no node of its own: `date` to `date`, `time` to `time`, `duration` to `duration`, `uuid` to
`uuid`, `uri` to `url`, `date-time` to `datetime_offset`, `binary` to `bytes`. `byte` lowers to a
hoisted `Scalar` over `bytes` whose `Encoding` is `base64`, with the `string` it travels as in
`WireType`. Any other format (`email`, `uri-reference`, `ipv4`, an invented one) lowers to a hoisted
`Scalar` over `string` whose `Encoding.Name` is the format as written, case kept, since the compiler
knows no registry of formats beyond the table. A `contentEncoding` beside a `byte` or unknown format
names the encoding instead; where the two name different encodings the format is kept verbatim under
`openapi:format` (`no_ir_home`) with an info `openapi/degraded-construct` at it. A scalar hoisted
for its format carries the bounds written beside it. The table is the same at every minor, and a
pairing of `format` with another `type` is [`numeric-formats`](#numeric-formats)'.

- Cases: `encoding-byte`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_UnknownFormatPerBaseType`,
  `schema.TestLower_NamedScalarComponentResolves`,
  `schema.TestFormatTable_EveryRowDistinguishesTheBareType`
- Status: partly built: `date-time-local` and `time-local` hoist over `string`, and `time`'s offset
  has no home (#869), and a `byte` string and a string-carried number put the logical and the wire
  type on opposite fields (#637).

### `numeric-formats`

A numeric `type` with a `format` the table names lowers to the primitive the pairing selects, and no
node of its own: an integer width — `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`,
`uint64` — under `integer` and `number` alike, since OpenAPI applies a format to both (3.1.1
§4.4.1), and `float`, `double`, `decimal` and `decimal128`, to `float32`, `float64`, `decimal` and
`decimal128`, under `number` alone, since under `integer` they admit only integral values, which no
primitive narrower than `integer` holds. A pairing the table does not name lowers to a hoisted
`Scalar` over the type's primitive that keeps the format as `Encoding.Name`, or, where a
`contentEncoding` beside it takes that field, keeps the format verbatim under `openapi:format`
(`no_ir_home`) with an info `openapi/degraded-construct` at it.

- Cases: `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_NumericFormatPairings`,
  `schema.TestContentVocabulary_KeptWhereNoEncodingHolds`

### `password`

`format: password` is a redaction request rather than a pairing of type and format, so the shape
lowers without it, and it lands where a declaration's annotations do: on the node the position owns,
whatever its kind, as `Sensitive`, an alias hoisted where the shape alone would own none; or, at a
property or header that owns none, as `Property.Secret`, inherited from a `$ref` target as
`deprecated` is. At a parameter that owns none it is kept verbatim in the parameter's `Unmodeled`
under `openapi:format` (`no_ir_home`), with an info `openapi/degraded-construct` at the `format`.

- Cases: `secret-format`, `scalar-format`
- Codes: `openapi/degraded-construct`
- Status: partly built: `ir.Parameter` has no field to mark a parameter for redaction (#578).

### `whole-schema-fallbacks`

Two whole-schema fallbacks answer to a compiler's own limits rather than to anything about the
construct, which is why §4.8's list of degradations does not hold them. A schema nested past the
lowering's depth cap becomes the top type, reported as an `openapi/degraded-construct` error at it.
A boolean `false` schema becomes a closed empty `Model`, with the schema itself kept verbatim under
`degraded_lowering` at `Unmodeled["openapi:schema"]` and an info `openapi/false-schema` at it — the
key names the position because a boolean schema writes no keyword to name it by, and nothing can
collide with it, since such a schema has nothing else to preserve. A boolean `true` schema is the
top type, as `{}` is, and reports nothing.

That preservation is load-bearing, not decorative: a closed empty `Model` admits `{}`, and the
source admits nothing, so the lowered type is one value wider than what was written. Without the
verbatim entry, `false` and `{type: object, additionalProperties: false}` are the same node, and
nothing downstream can tell "no instance" from "the empty object". The diagnostic says which one it
was, but a diagnostic is not part of the document.

The approximation is preferred to an exactly empty shape, and the reason is worth stating because
the IR does have one. A closed `Enum` with no members is an exactly empty value space (§4.5), but
`Enum.ValueType` is a `PrimKind` and a `false` schema declares no type, so lowering to one means
inventing a value type the source never wrote — an exact-looking node resting on a fabricated field,
which is not more faithful than an approximate one with the source beside it. It would also split
the rule: the composition case ([`allof-false-branch`](#allof-false-branch)) must stay a `Model`,
because it keeps what the other branches contributed, so one source construct would lower to two
different kinds depending on where it appeared. The overstatement is bounded, recorded, and
recoverable from the `Unmodeled` entry; an emitter that wants the exact reading has everything it
needs to produce it.

- Cases: `allof-boolean-branch`
- Codes: `openapi/degraded-construct`, `openapi/false-schema`
- Tests: `schema.TestLower_DepthCapExceeded`,
  `openapi.TestMaxNesting_AdmitsWhatTheLoweringLowersInFull`
- Status: partly built: a `$ref` and the declaration it targets reach the depth cap at different
  coordinates (#748), and a `$ref` to an inline `false` schema resolves only after its owner lowers
  (#861).

## 7. Schemas: keywords beyond the shape

### `constraints`

`minimum`, `maximum` and `multipleOf` lower to `Min`, `Max` and `MultipleOf`, and `minLength`,
`maxLength` and `pattern` to `MinLength`, `MaxLength` and `Pattern` as written, in the `Constraints`
of the node or carrier the position owns (a `Scalar`, a `Property` or a `Parameter`); `minItems`,
`maxItems` and `uniqueItems` to the `List`'s, and a `Tuple`'s per [`tuples`](#tuples); and
`minProperties` and `maxProperties` per [`additional-properties`](#additional-properties). A bound
is read off the raw node, never the parser's `float64`, so its `BigVal` keeps every digit the
literal wrote ([`values`](#values)), a quoted one as the decimal it spells; one that is no finite
decimal literal is dropped with an `openapi/invalid-numeric-literal` error at the schema that wrote
it. Exclusive bounds follow the declared dialect. In 3.0 the boolean modifies the bound beside it:
`true` moves that bound to `ExclusiveMin` or `ExclusiveMax` and clears `Min` or `Max`, so a 3.0
document and its 3.1 translation lower alike, `false` states nothing, and `true` with no bound
beside it bounds nothing and is kept verbatim under `openapi:exclusiveMinimum` or
`openapi:exclusiveMaximum` (`degraded_lowering`) with an `openapi/degraded-construct` warning. In
3.1 and 3.2 the keyword is a bound of its own, kept beside `Min`/`Max` when both are declared, a
field apiece and no `Unmodeled` entry (§5.3). The other dialect's form (a boolean in 3.1 or 3.2, a
number in 3.0) is dropped with an `openapi/invalid-exclusive-bound` error. A bound beside a node
with no `Constraints` field to take it (an `Enum`, a `Literal`, a `List` for a value bound) is kept
verbatim under `openapi:<keyword>` (`degraded_lowering`) with an info `openapi/degraded-construct`
naming the node it lowered to, as [`shape-applicator`](#shape-applicator) keeps a collection bound
on an object.

- Cases: `constraints`, `numeric-precision`
- Codes: `openapi/invalid-numeric-literal`, `openapi/invalid-exclusive-bound`,
  `openapi/degraded-construct`
- Tests: `openapi.TestConstraints_ExclusiveBoolean30`,
  `openapi.TestConstraints_ExclusiveWrongDialectForm`,
  `openapi.TestConstraints_LosslessNumericLiterals`
- Status: partly built: a bound beside a `oneOf` or `anyOf` that lowers to a `Union` is dropped in
  silence (#848), and so is one beside a multi-typed schema that no carrier holds
  ([`type-arrays`](#type-arrays), #852).

### `validation-only`

JSON Schema's validation-only keywords are kept verbatim under `validation_only` (§4.7) on the node
or carrier the position owns, one entry per family: `not`, `dependentSchemas`, `dependentRequired`
and `propertyNames` each under `openapi:<keyword>`, at the keyword; and three families each folded
into one object, at the schema — `if`, `then` and `else` under `openapi:if-then-else`, `contains`
with `minContains` and `maxContains` under `openapi:contains`, and `unevaluatedItems` with an
`unevaluatedProperties` that is not `false` under `openapi:unevaluated`, the `false` one being a
closure ([`additional-properties`](#additional-properties)). Each entry is reported as an info
`openapi/validation-only-keyword` at the schema that wrote it.

- Cases: `negation-not`, `dependent-required`
- Codes: `openapi/validation-only-keyword`
- Tests: `annotation.TestValidationOnlyAt_KeepsEveryKeywordItClaims`,
  `schema.TestLower_ValidationOnlyKeywords`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`

### `dynamic-ref`

`$dynamicRef` is resolved per reference site by compiler expansion, to a reference to the anchored
type, reported as an info `openapi/dynamic-ref-expanded` at the `$dynamicRef`, since what the source
wrote as dynamic is now a fixed target. What makes that possible is that a document's set of
`$dynamicAnchor` declarations is fixed before evaluation: a name declared exactly once is the only
match any dynamic scope containing a match can hold, so it *is* 2020-12 §8.2.3.2's outermost
matching anchor whatever path evaluation took. That argument assumes the document declares no `$id`,
which the compiler enforces rather than hopes for: `$id` starts a new schema resource, so an `$id`
anywhere on the path between a reference and a candidate anchor makes the reference irreducible.

A reference is irreducible — and is then kept verbatim under `openapi:$dynamicRef`
(`degraded_lowering`) with an info `openapi/degraded-construct` at it naming which case it was —
when the name is declared more than once (the target is back under the evaluation path's control),
when it is unresolvable, names another document, or is not a plain fragment, when its value is not a
string at all, when a resource boundary stands between it and the anchor as above, when expanding it
would close a cycle of aliases (a `$ref` cycle is refused outright for the same reason, and
expansion must not mint one the refusal cannot see), or when it is co-declared with a `$ref`, a
`oneOf`/`anyOf`, a shape of its own, or one of `items`/`prefixItems`/`required` — an expansion takes
its target whole, so it has nowhere to put a sibling applicator. One further case is irreducible in
the OpenAPI compiler specifically: an anchor declared anywhere but the top level of
`components/schemas` has no ID that is stable without consulting the registry, and resolving it
would make the reference depend on which position happened to lower first (§4.3), so it is preserved
instead.

Anchors are counted through YAML aliases and merge keys, so an anchor reached only by an alias still
makes its name a duplicate. The walk is bounded by depth and by node count, and says so when it
truncates, once, with an `openapi/degraded-construct` warning at the document, since a truncated
index undercounts declarations and an undercount is what turns "irreducible" into a confident
expansion.

- Cases: `dynamic-ref`
- Codes: `openapi/dynamic-ref-expanded`, `openapi/degraded-construct`
- Tests: `schema.TestDynamicRef_IrreducibleIsKeptAndSaysWhy`,
  `schema.TestDynamicAnchors_CountsWhatAnAliasBringsIn`,
  `schema.TestDynamicAnchorIndex_ReportsATruncatedWalk`
- Status: partly built: a merge chain past the view's merge bound hides an `$id`, so a `$dynamicRef`
  can expand across a resource boundary (#401), and the truncation warning names the walk's own
  bounds when the merge bound stopped it (#454).

### `content-vocabulary`

The content vocabulary is modelled, not preserved. All three keywords share one home, so a position
keeps them all or lowers them all: `contentEncoding` to `Encoding.Name`, `contentMediaType` to
`Encoding.MediaType`, and `contentSchema` to `Encoding.Schema` (§5.3) on the scalar the position
lowers to. `contentSchema`'s value is a schema and lowers like one — hoisted at its own source
pointer and referenced by ID — rather than riding along as a payload a consumer would have to
re-parse. Where the position lowers to a shape with no `Encoding` field, all three stay verbatim
under `no_ir_home`, each under `openapi:<keyword>` and each reported as an info
`openapi/degraded-construct` at it (§4.7).

- Cases: `content-vocabulary`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestContentVocabulary_LowersToEncoding`

### `dialect-keywords`

`$id`, `$schema` and `$vocabulary` identify and configure a JSON Schema *resource*. The IR
identifies every type by a synthetic ID derived from its source pointer rather than by `$id` (§3),
and describes one API surface rather than a resource graph, so it has no dialect axis and none is
coming: each is kept verbatim under `openapi:<keyword>` (`out_of_scope`), with an info
`openapi/degraded-construct` at it, and `$id` is explicitly *not* honoured as a base URI for
reference resolution: it counts only as the boundary of a schema resource, which makes a schema's
own `$defs` answer a `#/$defs/...` pointer it writes ([`references`](#references)) and stops a
`$dynamicRef` expanding across it ([`dynamic-ref`](#dynamic-ref)). A document's `jsonSchemaDialect`,
the default `$schema` of its schemas, is kept on the Document the same way, every schema read in the
OpenAPI dialect whatever it names. `$comment` is dropped outright — 2020-12 §8.3 forbids presenting
it to end users, so no emitter may see it. `$dynamicAnchor` is consumed as a reference target rather
than carried as content — it is what lets a `$dynamicRef` expand ([`dynamic-ref`](#dynamic-ref)).
`$anchor` is neither: a `$ref` naming an anchor is reported unresolved rather than resolving through
it ([`references`](#references)). It is excluded here because the compiler does nothing with it
today, not because dropping it is settled.

- Cases: `dialect-keywords`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestDialectKeywords_KeptOutOfScope`, `openapi.TestMeta_JSONSchemaDialectIsKept`
- Status: partly built: `$anchor` is not honoured as a reference target (#914).

### `vocabulary-coverage`

Every keyword of the JSON Schema 2020-12 vocabularies reaches the compiled IR, lowered by a rule of
this document or kept verbatim, its presence and its value both, with four exceptions that change
nothing by design: `$comment`, which the specification forbids surfacing; `$defs`, whose members
lower where a `$ref` reaches them; `$dynamicAnchor`, read as a reference target; and `$anchor`,
which the compiler does not yet honour ([`dialect-keywords`](#dialect-keywords)). The keyword lists
of §4.7 and of this section are held to that rather than merely asserted:
`TestVocabulary2020_12_EveryKeywordIsLoweredOrKept` enumerates the vocabularies keyword by keyword
and, for each, compiles three documents — the keyword absent, written with one value, written with a
second — requiring that the absent form differs from the first and the first from the second, and an
exception asserts the inverse with its written justification. A keyword that stops being carried
fails there rather than waiting for a reader to notice. The guarantee is a floor rather than an
inventory: it compares one pair of values per keyword, so a lowering that carried one spelling and
dropped another would still pass.

- Tests: `schema.TestVocabulary2020_12_EveryKeywordIsLoweredOrKept`

## 8. Paths and operations

### `path-keys`

A Paths Object key lowers to `HTTPBinding.URITemplate` as written. One that breaks OpenAPI's path
templating rules, those of 3.2.0 §4.8.2 read at every version — no leading `/`, a character a path
admits only percent-encoded, a brace outside a template expression, an expression used twice, an
empty segment, or a query — lowers as declared and is reported as one `openapi/invalid-path-key`
warning at its path item, naming each rule the key breaks.

- Cases: `http-binding`
- Codes: `openapi/invalid-path-key`
- Tests: `operation.TestPathKeys_MalformedKeyIsReportedOnce`,
  `operation.TestPathKeys_RulesAreNotVersionGated`,
  `operation.TestPathKeys_FragmentIsLeftToGitHub602`
- Status: partly built: a `#` and what follows it are not read, so a fragment lowers into the
  template unreported (#602), and nothing matches a template's expressions to the path parameters
  (#640).

### `path-item-operations`

Every operation a path item declares — the fixed method fields, 3.2's `query`, and 3.2's
`additionalOperations` keyed by method — lowers to an `Operation` apiece, mounted at its own
pointer. An `additionalOperations` key is used verbatim as `HTTPBinding.Method`, since OpenAPI reads
a method name case-sensitively. A key that names no method, the empty string, lowers as declared and
is reported as an `openapi/invalid-method-key` warning at the operation it declares: the binding is
unusable, but dropping the entry would lose every operation it declares.

- Cases: `path-item-operations`, `later-version-constructs`
- Codes: `openapi/invalid-method-key`
- Tests: `operation.TestPathItem_EmptyAdditionalOperationsKeyReported`,
  `operation.TestPathItem_AdditionalOperationsLowerOnEveryRoute`,
  `operation.TestPathItem_AdditionalOperationsBindCallbacks`

### `operation-ids`

An `operationId` lowers to `Operation.Name`, as its `Source` and neutral `Canonical`
([`naming`](#naming)), while the `OpID` is derived from the operation's mount pointer (§3.1) and
never from the `operationId`, so a repeated or renamed id moves no ID. An operation with none, or
with `""`, claims no id and is named by a hint of its method and the route's key in neutral words —
the path template, a webhook's name, a callback's expression, or an `additionalOperations` key
standing as the method (`get_ping`, `post_new_pet`; §8.1). Uniqueness is judged once every operation
has lowered, across paths, webhooks and callbacks alike, and by declaration, so a finding does not
move with the order the document is lowered in. A second declaration writing a claimed id is
reported as an `openapi/conflicting-operation-id` error at the pointer it is written at, naming the
first declaration, the declarations taken in pointer order; one declaration mounted more than once,
by a path item `$ref`, a YAML alias or a merge key, as an `openapi/duplicate-operation-id` warning
at each mount but the first, naming that mount. Every operation lowers whatever the verdict,
repeated names included, and the parser's own uniqueness finding is dropped for this one
([`parser-validation`](#parser-validation)).

- Codes: `openapi/duplicate-operation-id`, `openapi/conflicting-operation-id`
- Tests: `operation.TestOperations_DuplicateOperationIDReported`,
  `operation.TestJudge_ReportsEveryDeclarationAndMountButTheFirst`,
  `operation.TestOperation_NoOperationIdHint`

### `path-item-servers`

A path item's `servers`, under `paths`, `webhooks` and a callback expression alike, are kept
verbatim on each operation it mounts, under `openapi:servers` (`no_ir_home`), since §10 scopes
servers by an index list at service and channel and an operation has no such list yet; an
operation's own `servers`, which OpenAPI says override the path item's, are kept beside them under
`openapi:operationServers`, the one key here not named for the keyword it holds, since two
declarations at two pointers cannot share one map key without the survivor depending on lowering
order. Each is reported as an info `openapi/degraded-construct` at the operation. A path item that
mounts no operation keeps its `servers` and `x-*` names on the service instead, or on the declaring
operation's HTTP binding for a callback expression, under `openapi:pathItem/<item pointer>/<key>`,
the item's pointer in the key since one carrier holds every such item, reported as one
`openapi/degraded-construct` warning at the mount.

- Cases: `webhooks`, `callbacks`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperations_PathItemServersKeptOnEveryRoute`,
  `operation.TestOperations_OwnServersKeptBesideThePathItems`,
  `operation.TestOperations_ServersKeysAreIndependent`
- Status: open: whether an operation gets a field for the servers a path item declares (#285).

### `path-item-docs`

A path item's own `summary` and `description`, at the same three mounts, are kept verbatim on each
operation it mounts, under `openapi:pathItemSummary` and `openapi:pathItemDescription`
(`no_ir_home`), reported as an info `openapi/degraded-construct` at the operation, rather than
merged into the operation's `Docs`: `ir.Docs` holds the operation's own pair, and a path item's
documents the path, so merging would need a precedence rule and would attach documentation the
operation's author never wrote — an inference, which invariant 6 places in policy rather than in a
lowering.

- Cases: `path-item-docs`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Status: partly built: a path item that mounts no operation drops its own summary and description
  in silence (#383).

### `path-item-ref-fields`

OpenAPI allows a Path Item Object to write fields beside its `$ref`, and leaves them undefined only
where both sides declare one field. They lower as a layer of their own, at their own pointers: a
mount reads the fields beside each `$ref` of its chain, nearest the use site first, and then the
item the chain ends at. The nearer layer keeps every method or `additionalOperations` key,
`summary`, `description`, `servers`, `x-*` name, undeclared key and item-level `(name, in)`
parameter both declare, and the collision is reported as an `openapi/path-item-ref-collision`
warning at the nearer declaration rather than merged, so no construct is attributed to a declaration
that did not write it. The mount lists its operations in the order one item holding them all would.
A reference whose pointer passes through those fields, which the resolver reads through the `$ref`
into the item it names while the mount declares the field written there at that very pointer, is
left unresolved and reported as an `openapi/unresolved-ref` error rather than lowered as either; so
is a `#/$defs/...` pointer written in them, since its definition is read the same way. The exception
is a reference whose target the resolver finds to be the field written there, as it can for a
reference other than a schema's `$ref` or a mapping value when external references are allowed,
which lowers as that field. An item another document holds, mounted where the use site writes fields
too, is declared under the use site's `$ref` (`…/$ref/…`), a position the source cannot name. The
fields beside a `$ref` the load phase's walk does not reach, in another document or under an `x-*`
key, are not lowered, reported as an `openapi/degraded-construct` warning at the mount naming the
reference that reaches them.

- Cases: `path-item-ref-siblings`
- Codes: `openapi/path-item-ref-collision`, `openapi/unresolved-ref`, `openapi/degraded-construct`
- Tests: `operation.TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins`,
  `operation.TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefIsRefused`,
  `operation.TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported`
- Status: partly built: a reference into the fields beside a path item `$ref` is left unresolved,
  rather than lowered as the field written there (#824); the fields beside a `$ref` the load phase's
  walk does not reach are not lowered (#823); and a pointer through a path item `$ref` resolves only
  if that `$ref` resolved first (#876).

### `tag-groups`

Tags lower to the Document's `TagDef`s, and the operations they name to the service's groups. Two
facts decide which group an operation lands in, and both are declared rather than inferred. The
group comes from the operation's first *navigational* tag, not simply its first: OpenAPI 3.2 gives a
tag a `kind`, and the only kind that groups is the registry's navigational value (`nav`), with a tag
that declares no kind — as a 3.0 or 3.1 tag does — navigational too. Any other declared kind is
skipped, whether or not the registry names it; reading a section out of an unregistered string would
be an inference (invariant 6), and nothing is reported because the document loses nothing —
`TagDef.Kind` records the kind verbatim and `Operation.Tags` keeps every membership. An operation
whose every tag is non-navigational falls to the same default group an operation with no tags
reaches, rather than being filed under a tag that does not group.

A tag's declared `parent` (3.2) nests its group under the parent's, one level per declared ancestor,
so `Service.Groups` reads as the section tree the document describes. A parent that is not itself a
declared navigational tag ends the walk there, and the group stays top-level. Where declared parents
form a cycle, the tags on it nest under none of one another while a tag declaring one of them as its
parent still nests under it; the parser's own report of a missing or circular parent is left to
stand. Group and sibling order stay first-seen from the operations, with a missing ancestor created
immediately before its first descendant, so a document that declares no parents groups exactly as it
would were parents never read.

- Cases: `tags-grouping`, `tags-grouping-32`, `group-identity`
- Tests: `operation.TestGrouping_ParentCycleNestsNoMemberUnderAnother`,
  `operation.TestGrouping_DanglingParentStaysTopLevel`,
  `operation.TestGrouping_BadgeOnlyOperationFallsToDefault`
- Status: partly built: two tags with one name compile clean (#881), and a tag an operation repeats
  passes into the IR undeduplicated (#738). Open: grouping operations by a vendor extension (#582).

## 9. Parameters

### `parameters`

An operation's own parameters, in order, and then those of its path item it does not override, in
order, lower to the operation's `Params`, an operation's parameter overriding a path item's of the
same `name` and `in`. Each is placed on the HTTP binding at its location (`path`, `query`,
`querystring`, `header` or `cookie`) under its wire name, with its `style`, `explode` and
`allowReserved`, each the location's default where the parameter writes none (§8.1). A parameter's
ID is derived from the operation's mount pointer, its name and its location (§3.1). A list repeating
an earlier entry's `name` and `in`, its `$ref`s resolved, which OpenAPI forbids, keeps every
occurrence, each repeat's ID ending in its occurrence number, and is reported as an
`openapi/duplicate-parameter` error at the repeat. Every list the source writes is checked, whether
or not it lowers, and a list another document writes where it lowers; the parser's own finding,
which sees only inline entries, is dropped for this one ([`parser-validation`](#parser-validation)).

- Cases: `param-styles`, `param-style-matrix`, `param-identity`
- Codes: `openapi/duplicate-parameter`
- Tests: `operation.TestParams_RepeatedNameAndLocationKeepsEveryParameter`
- Status: partly built: an undeclared `explode` is written out as the location's default, which
  erases whether the document declared it (#627); header parameters whose names differ only in case
  are two parameters, unreported (#635); the 3.2 cookie `style` is refused as invalid (#389); and a
  list only a `$ref` reaches is never checked for repeats (#880).

### `content-parameters`

A parameter or header that states its type with `content` instead of `schema` lowers through the
first media-type entry, which OpenAPI requires to be the only one. Its schema is the type, lowered
as the `schema` spelling's is with the same constraints and `xml` hints, hoisted at
`<owner>/content/<key>/schema`, or the top type where the entry writes none, and the key as written
is the media type that serializes the value: `HTTPParamBinding.ContentType` on a parameter,
`Property.Encoding.MediaType` on a header, neither set by the `schema` spelling. Further entries
lower nothing, reported as an `openapi/degraded-construct` warning at the `content` map naming them.
What else the entry writes is kept: its `example` and `examples` follow the parameter's or header's
own in `Examples`, and its `x-*` extensions, `description`, undeclared keys and the fields only a
body Content models — `itemSchema`, `itemEncoding`, `prefixEncoding` and `encoding`, each reported
as an info `openapi/degraded-construct` — go verbatim under `openapi:content/<key>/<field>`, the key
escaped as a pointer segment, as `vendor_extension` or `no_ir_home`. A `content` map with no entry
elects nothing: a `schema` beside it stands, and the empty map is kept under `degraded_lowering`
with an `openapi/degraded-construct` warning. The converse, `schema` beside a non-empty `content`,
is [`competing-keywords`](#competing-keywords)'.

- Cases: `param-content-fields`, `header-content-schema`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_ContentStyleParameter`,
  `operation.TestSingleContentEntry_ReportsExtraMediaTypes`,
  `operation.TestParamContent_OwnAndMediaExamplesAreBothKept`
- Status: partly built: a parameter or header with several `content` entries takes its type from
  whichever comes first (#606), and a `style`, `explode` or `allowReserved` beside a parameter's
  `content` lowers in silence (#887).

### `allow-empty-value`

A parameter's `allowEmptyValue` is kept verbatim in its `Unmodeled` under `openapi:allowEmptyValue`
(`no_ir_home`), reported as an info `openapi/degraded-construct` at the keyword: `HTTPParamBinding`
holds its neighbours but not this one.

- Cases: `param-styles`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_AllowEmptyValueKept`
- Status: open: whether the HTTP binding gets a field for it (#285).

### `reserved-header-names`

A header parameter named `Accept`, `Content-Type` or `Authorization`, which OpenAPI says SHALL be
ignored because the protocol layer owns the name, lowers as declared and is reported as an
`openapi/reserved-header-name` warning at the parameter: dropping declared content is an emitter's
call, not a compiler's, and the diagnostic lets an emitter suppress it.

- Codes: `openapi/reserved-header-name`
- Tests: `operation.TestParams_ReservedHeaderNamesAreReported`

### `querystring`

3.2's `in: querystring` lowers to the `querystring` location, the parameter binding the whole query
string ([`content-parameters`](#content-parameters)). A `style` written there, which 3.2 forbids,
lowers as declared and is reported by the parser as an `openapi/validation` error. An `explode` or
`allowReserved`, which the parser does not check there, lowers as declared and is reported as an
`openapi/invalid-location-keyword` warning at the keyword.

- Cases: `param-querystring`, `querystring-forbidden-keywords`
- Codes: `openapi/invalid-location-keyword`, `openapi/validation`
- Tests: `operation.TestParams_QueryStringDeclaredStyleIsKeptAndReported`
- Status: partly built: `explode` and `allowReserved` there are judged only where the parameter is
  lowered (#885).

### `allow-reserved-locations`

An `allowReserved` its document's version gives no effect — at `path`, `header` or `cookie` before
3.2, which applies it to `in: query` alone, and at `header` or a `cookie` in `style: cookie` in 3.2,
which applies it wherever the value is percent-encoded — lowers as declared and is reported as an
`openapi/inapplicable-location-keyword` warning at the keyword, a code of its own since no MUST is
broken, once, where the parameter is written, whether anything lowers it or not.

- Cases: `param-allowreserved-locations`
- Codes: `openapi/inapplicable-location-keyword`
- Tests: `load.TestReservedIgnored_FollowsEachMinorsText`,
  `load.TestReservedFinding_ReportsWhereTheVersionGivesNoEffect`,
  `load.TestReservedFinding_AnotherDocumentIsNotJudgedByTheRoots`
- Status: partly built: a parameter another document holds is not judged, until it is settled which
  minor governs that document (#839).

### `parameter-schema-keywords`

A parameter schema's `xml`, and its `readOnly` and `writeOnly`, are kept verbatim in the parameter's
`Unmodeled` (`no_ir_home`), reported as an info `openapi/degraded-construct` at the keyword, since
`ir.Parameter` has no field for either. The `xml` of a schema that owns a node of its own stays on
that node's `XMLHints` instead.

- Cases: `param-xml-residue`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_SchemaVisibilityKeptUnderUnmodeled`,
  `operation.TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode`,
  `operation.TestParams_SchemaXMLHintsStayOnAnOwnedNode`

## 10. Request bodies and content

### `media-types`

Every media type of a `content` map lowers to one `Content` apiece, in source order and with none
elected primary (§7.2). `Content.MediaType` is the key as written — case, parameters and wildcards
kept, so `application/json` and `Application/JSON` are two Contents — and its `Type` is the media
type's `schema`, hoisted at `<owner>/content/<key>/schema` with the key escaped as a pointer token,
or the top type where it writes none. A request body's keys also go, as written and in order, to
`HTTPBinding.RequestContentTypes`. A response whose `content` is absent or empty has no `Payload`
([`request-body`](#request-body) for the request side). A key is classified on a copy that is
lowercased, since a media type's names are case-insensitive (RFC 6838 §4.2), trimmed and cut at its
first `;`, and the IR never keeps the copy: `multipart/*` and `application/x-www-form-urlencoded`
read the media type's `encoding` as part encodings ([`multipart-encoding`](#multipart-encoding)),
and a binary body is a file ([`file-bodies`](#file-bodies)); the streaming policy reads the same
copy ([`streaming-media`](#streaming-media)). No other key is classified, so `application/*+json`,
`text/*` and `*/*` are Contents like any other.

- Cases: `media-type-spelling`, `multi-content`
- Tests: `operation.TestContent_AllMediaTypesKeptInOrder`,
  `operation.TestContent_MixedCaseFormContentKeepsEncoding`,
  `lowering.TestNormalizeMediaType_CollapsesTheSpellingsOneMediaTypeHas`
- Status: partly built: a malformed media-type key, a multipart body that is not an object, and a
  body on a GET pass in silence (#649).

### `request-body`

A request body lowers to the operation's request `Payload`: each media type of its `content` to a
`Content` ([`media-types`](#media-types)), its `required` to `Payload.Required`, always set, since
OpenAPI's own default makes an undeclared `required` mean false rather than unstated, and its
`description` to `Payload.Docs`. A request body whose `content` map is empty, which every minor's
schema admits and 3.2 says SHOULD hold an entry, lowers to a `Payload` with no Contents that keeps
all three and its `x-*`, reported as an `openapi/empty-request-content` warning at the map; a
`content` the parser finds missing is [`minor-required-fields`](#minor-required-fields)'.

- Cases: `empty-request-content`, `request-body-docs`
- Codes: `openapi/empty-request-content`
- Tests: `operation.TestContent_NonRequiredRequestBody`,
  `operation.TestLowerRequestBody_AnEmptyContentKeepsTheBody`

### `file-bodies`

A content entry whose schema is `type: string` with `format: binary`, read through a `$ref` and
whatever its media type, or whose schema is absent or boolean on `application/octet-stream`, matched
on the normalized key ([`media-types`](#media-types)), lowers to `Content.File` with `IsText` false
and `ContentTypes` the key as written, and `Content.Type` the non-nullable `bytes` primitive in
place of whatever the schema lowered to, so that a body to stream is not read as a model. No other
`FileInfo` field is set. The same holds for a response and an error case. An `octet-stream` entry
with a schema that is not `string` with `binary`, a `format: byte` or `contentMediaType` string, and
a schema-less entry on any other media type are ordinary Contents.

- Cases: `file-body`
- Tests: `operation.TestContent_BinaryOctetStreamBody`,
  `operation.TestContent_MixedCaseOctetStreamBodyIsFile`,
  `operation.TestContent_BinaryRefBodyDetectedAsFile`
- Status: partly built: a binary body's schema keeps its docs and bounds on a node no Content
  references (#600), and the 3.1 idioms for a file, `contentMediaType` without `contentEncoding` and
  a schema-less entry on a media type other than `application/octet-stream`, are not recognized
  (#605).

### `example-values`

A media type's, parameter's or header's `example` lowers to an `Example` holding its value, ahead of
each entry of its `examples` map in order, each an `Example` named by its key: `summary` and
`description` as written, `value`, or 3.2's `dataValue`, as `Value` ([`values`](#values)), and
`externalValue` as `ExternalURL`. 3.2's `serializedValue`, which `ir.Example` has no field for, is
kept verbatim in the example's `Unmodeled` under `openapi:serializedValue` (`no_ir_home`), reported
as an info `openapi/degraded-construct` at it. A schema's examples are
[`schema-examples`](#schema-examples)'.

- Cases: `examples-32`
- Codes: `openapi/degraded-construct`

### `item-schema`

3.2's `itemSchema` lowers to `Content.Item`, the type of each item of a sequential body, and
`itemEncoding` to `Content.ItemEncoding`. Beside a positional `prefixEncoding`, a single every-item
encoding cannot state ordinals, so `itemEncoding` and `prefixEncoding` are both kept verbatim in the
content's `Unmodeled`, under `openapi:itemEncoding` and `openapi:prefixEncoding` (`no_ir_home`),
reported as one info `openapi/degraded-construct` at the content, while `itemSchema` still lowers to
`Content.Item`.

- Cases: `sequential-media`
- Codes: `openapi/degraded-construct`

### `streaming-media`

A request body or success response with a Content whose media type the streaming policy names —
`application/jsonl`, `application/x-ndjson` and `text/event-stream` by default, matched on the
normalized key ([`media-types`](#media-types)) at every minor — lowers to `Operation.RequestStream`
and `ResponseStream` for the directions that have one, each with `Events` the Content's own `Type`
(for a frame format the schema describes one frame, not the body), `Streaming` the `client`,
`server` or `bidi` summary of them, and `Provenance.Inferred` the marker `streaming-media-type` on
the operation, joined by `,` to any other marker, since below 3.2 a media type is a guess about
framing and not a declaration. A 3.2 `itemSchema` ([`item-schema`](#item-schema)) is a declaration:
it streams on any media type with `Events` the `Item` type and no marker, and wins over the
media-type reading at its own Content. Streaming Contents that name different element types leave
`Events` unset on a direction that still streams, every Content kept, reported as an info
`openapi/degraded-construct` at the `requestBody` or `responses` pointer; Contents that name the
same element elect it. Error responses are not read. The policy is the Go option
`Options.StreamingMedia`: `Disabled` turns the media-type reading off, leaving only what an
`itemSchema` declares, and `MediaTypes` replaces the default list instead of extending it.

- Cases: `streaming-media-30`, `streaming-media-31`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestStreaming_ResponseMediaTypeImpliesServerStream`,
  `operation.TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed`,
  `operation.TestStreaming_ItemSchemaDeclaresTheStream`
- Status: partly built: an operation that does not stream carries an empty `Streaming`, where §7.2
  declares `none` (#978); the default policy misses `application/json-seq` (#629); sequential media
  types derive the wrong item type (#630); a response offering a plain body and an event stream does
  not record which content streams (#601); and an event stream's terminator, and a response that
  streams only when a request field asks it to, have no IR home (#666, #667).

### `multipart-encoding`

An Encoding Object of a `multipart/*` or `application/x-www-form-urlencoded` Content lowers to a
`PartEncoding` in `Content.Encoding`, keyed by the `PropID` of the body property its key names,
wherever the body declares that property: on the schema, in an `allOf` branch with the first
declaration of a name winning, or on a `Base` or `Mixin` it composes. `contentType` lowers to
`ContentTypes`, split on `,` and trimmed with empty items dropped; `headers` to `Headers`, lowered
as a response's are; and `style` and `explode` to `Style` and `Explode` as declared, nothing
defaulted. `Multi` and `Filename` come from the part's own schema and never from the `encoding` map:
`Multi` where it is an array, `Filename` where it is a `string` with `format` `binary` or `byte`. A
part is entered only when its `PartEncoding` holds something, so a body that declares no encoding
still gets entries for its array and file parts, and an entry declaring nothing leaves none. What an
Encoding Object writes beyond those fields is
[`encoding-allow-reserved`](#encoding-allow-reserved)'.

- Cases: `multipart-encoding`
- Tests: `operation.TestContent_MultipartPartEncoding`,
  `operation.TestContent_ArrayMultipartPartMulti`,
  `operation.TestPartEncodings_MixinPartIsKeyedOnTheMixin`
- Status: partly built: an `encoding` key that names no property, or one declared only inside a
  `oneOf` or `anyOf` branch, is dropped with no diagnostic (#604); on a multi-typed body the entry
  is keyed by a property no node holds, which validation then refuses (#744); an array of files is
  not marked as files (#605); and an encoding on a form body whose media type carries parameters is
  refused by the parser's validation (#623).

### `encoding-allow-reserved`

An Encoding Object's `allowReserved` is kept verbatim in the content's `Unmodeled`, under
`openapi:encoding/<part>/allowReserved` or `openapi:itemEncoding/allowReserved` (`no_ir_home`),
reported as an info `openapi/degraded-construct` at the keyword, since `ir.PartEncoding` holds
`style` and `explode` beside it but no field for this one. It is read before the entry's emptiness
is judged, so an entry declaring nothing else is not dropped with the empty `PartEncoding` it lowers
to.

- Cases: `multipart-encoding`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding`,
  `operation.TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey`

## 11. Responses and errors

### `response-conditions`

Each entry of an operation's `responses` lowers to a `Response`, for a status below 400 or a range
below `4XX`, or to an `ErrorCase`, for a status from 400, a `4XX` or `5XX` range, or `default`
(§7.2). Its `Conditions` state the statuses its key names: a code as that code, a range, in either
case, as its hundred codes, and `default` as the catch-all. The key, as declared and then
neutralized, is the `Name.Hint` of either (`404`, `5_xx`, `default`), which records the spelling a
range cannot state, though neutralizing a range folds its case (`5XX` and `5xx` both give `5_xx`). A
key naming no status — not a code from 100 to 599, not a range, not `default` — lowers to a
`Response` with no status condition, rather than the catch-all `default` alone denotes, reported as
an `openapi/invalid-status-key` warning at the operation's entry for it.

- Cases: `per-status-errors`
- Codes: `openapi/invalid-status-key`
- Tests: `operation.TestResponses_NamedByStatusKey`,
  `operation.TestResponses_InvalidStatusKeyIsReported`
- Status: partly built: an operation whose only response is `default` has no success response
  (#608).

### `duplicate-status-key`

Two keys of one `responses` map resolving to one status range — `4XX` beside `4xx`, or a `200` a
merge key introduces beside a second — reach the IR with one name and one condition, and an
`ErrorCase` carries no ID, so nothing else tells them apart. Both are kept, since neither key is
wrong on its own and dropping one would pick a winner on nothing but declaration order, and they are
reported as one `openapi/duplicate-status-key` warning at the operation's `responses` map, naming
every key that collided.

- Codes: `openapi/duplicate-status-key`
- Tests: `operation.TestResponses_TwoKeysForOneRangeAreReported`,
  `operation.TestResponses_ACollisionIsReportedTheSameInEitherOrder`,
  `operation.TestResponses_StatusKeyDiagsAreSitedAtTheOperation`

### `error-responses`

An error response lowers exactly as a success one: its `headers` to `ErrorCase.Headers` and every
media type of its `content` to `ErrorCase.Payload.Contents`, neither degraded and neither kept under
`Unmodeled`, and the payload's naming hint derived from the declaration pointer on both sides, so
that one `components/responses` entry mounted at a success and an error status interns one type
whichever side reaches it first.

- Cases: `per-status-errors`, `shared-response-across-status`

### `response-summary`

A Response Object's `summary`, which 3.2 added, lowers to the `Docs.Summary` of the `Response` or
`ErrorCase`, read off the raw node at every version
([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)). A 3.0 or 3.1 document writing one is
reported with an `openapi/later-version-construct` warning at it; a value that is not a string is
kept verbatim under `openapi:summary` (`degraded_lowering`), with an `openapi/degraded-construct`
warning at it; and one merged in through `<<` is not read, reported as an
`openapi/unknown-key-unreachable` warning.

- Cases: `response-summary-32`
- Codes: `openapi/later-version-construct`, `openapi/degraded-construct`,
  `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryIsReadAtEveryVersion`,
  `operation.TestResponses_SummaryNoReaderTakesIsStillReported`

### `header-style`

A header of a response or an encoding lowers to a `Property` of its headers, and its `style` and
`explode` are kept verbatim in the property's `Unmodeled` (`no_ir_home`), reported as an info
`openapi/degraded-construct` at the keyword, since `ir.Property` has neither field. A `Content-Type`
entry in either headers map, which OpenAPI says SHALL be ignored, lowers as declared and is reported
as an `openapi/reserved-header-name` warning at the entry, as a parameter is
([`reserved-header-names`](#reserved-header-names)).

- Cases: `header-content-schema`
- Codes: `openapi/degraded-construct`, `openapi/reserved-header-name`
- Tests: `operation.TestHeaders_SerializationKeywordsKept`,
  `operation.TestHeaders_ReservedContentTypeEntryIsReported`,
  `operation.TestHeaders_ReservedContentTypeInEncodingIsReported`
- Status: partly built: response headers whose names differ only in case are two headers, unreported
  (#635). Open: whether a header gets fields for `style` and `explode` (#285).

### `links`

A response's `links` are kept verbatim under `openapi:links` (`no_ir_home`), with no diagnostic, on
the `Response` and the `ErrorCase` alike: the two are lowerings of one Response Object, so a
construct kept on only the success one would make a declaration survive or vanish on nothing but its
status code. The gap is one the IR can close, so a promotion pass may read the entry later.

- Cases: `response-links`
- Status: not built: no IR field holds a link yet (#669).

## 12. Webhooks and callbacks

### `webhooks`

Each operation of a `webhooks` entry's path item lowers to an `Operation` mounted at its own
pointer, in the synthesized `webhooks` group, its HTTP binding marked `IsWebhook`, with the entry's
key as `WebhookName` and no `URITemplate`, since the key names an event rather than a path.

- Cases: `webhooks`
- Status: open: whether a webhook operation inherits the service's auth (#626).

### `callbacks`

An operation's `callbacks` lower to its HTTP binding's `Callbacks`: for each runtime expression of
each named callback, a `Callback` holding the expression as written and the `OpID`s of the
operations its path item declares. Each of those lowers to an `Operation` of its own, mounted at its
own pointer below the declaring operation's (`…/callbacks/<name>/<expression>/<method>`, the name
escaped as one token), grouped as any other operation is, with the expression as its `URITemplate`
(§8.1).

- Cases: `callbacks`
- Tests: `operation.TestCallbacks_NameWithSlashKeepsItsOwnKey`
- Status: partly built: callbacks declared on an operation inside a callback are dropped in silence
  (#858); a callback operation is not marked inbound, and a callback has no name (#625); and a
  callback key that embeds runtime expressions in a URL, as OpenAPI's own example does, draws a
  validation warning (#621). Open: whether a callback operation inherits the service's auth (#626).

## 13. Security

### `security-requirements`

`components/securitySchemes` lowers to the Document's auth schemes, each under an ID derived from
its pointer, and a `security` list — the document's to the Service's, an operation's to the
Operation's — to an OR of ANDs: each requirement object one alternative, naming its schemes, each
with the scopes it lists (§9). An operation that writes no `security` inherits the document's, and
one writing `security: []` is public; the two stay apart, absent against empty (§7.2). A requirement
naming a scheme the section does not declare is reported as an `openapi/unresolved-ref` error at the
requirement and dropped. An entry that omits the field naming its mechanism — `type`, or for `http`
the `scheme` token — is interned as no scheme, reported as an `openapi/incomplete-security-scheme`
error at the entry, and every requirement naming it is dropped. 3.2's device flow lowers to `Flows`
beside the other flows, and `oauth2MetadataUrl` to `OAuth2MetadataURL`. An `oauth2` scheme whose
`flows` declares no flow — `flows: {}`, or only `x-*` and keys that name no flow — and that has no
`oauth2MetadataUrl` lowers as declared, reported as an `openapi/oauth2-no-flow` warning at the
declaration; a `flows` the parser's REQUIRED-field rule finds missing is that rule's to report
instead ([`minor-required-fields`](#minor-required-fields)).

- Cases: `security-schemes`, `security-or-and`, `oauth2-no-flow`
- Codes: `openapi/oauth2-no-flow`, `openapi/incomplete-security-scheme`, `openapi/unresolved-ref`
- Tests: `auth.TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismIsRefused`,
  `auth.TestAuth_RequirementsOrOfAnds`
- Status: partly built: a requirement naming an unknown scheme is dropped, so an operation whose
  every requirement names one lowers as inheriting the service's security (#633); an unresolved
  scheme name gets no near-miss hint (#694); and a scope a requirement repeats passes into the IR
  undeduplicated (#738).

### `security-scheme-fields`

A security scheme lowers from the mechanism fields its `type` defines: `in` and `name` for `apiKey`,
`scheme` and `bearerFormat` for `http`, `flows` and `oauth2MetadataUrl` for `oauth2`, and
`openIdConnectUrl` for `openIdConnect`. A mechanism field another type defines, written on a scheme
whose own type does not, is kept verbatim in the scheme's `Unmodeled` under `openapi:<field>`
(`degraded_lowering`), reported as an info `openapi/degraded-construct` at the field, rather than
filling the `AuthScheme` field of its name, which would give the mechanism a property it does not
define. The field is read where it is written, at the declaration a `$ref` entry names.

- Cases: `security-schemes`
- Codes: `openapi/degraded-construct`
- Tests: `auth.TestLowerSecuritySchemes_FieldsTheTypeDoesNotDefineSurvive`,
  `auth.TestLowerSecuritySchemes_KeptFieldsAndExtensionsShareOneMap`

### `oauth-flow-fields`

A flow field the flow's kind does not define — `authorizationUrl` beside a token-only flow,
`tokenUrl` on `implicit`, `deviceAuthorizationUrl` off the device flow — is kept verbatim in that
flow's `Unmodeled` (`degraded_lowering`), reported as an info `openapi/degraded-construct` at the
field, and never lowered into a field the flow lacks.

- Cases: `oauth-flow-fields`
- Codes: `openapi/degraded-construct`

## 14. Servers

### `servers`

The document's `servers` lower to `Document.Servers`, in order: `url` to `URLTemplate` as written,
`description` to its docs, 3.2's `name` to its `Name` ([`naming`](#naming)), and each entry of
`variables` to a variable with its `default`, `enum` and `description`. A document writing no
`servers` lowers to the one server OpenAPI implies, at `/`. A name one servers list repeats — the
document's, a path item's or an operation's, compared as written at every version, since the parser
reads `name` on each — is kept as declared on both entries, the document's in `Servers` and the
others in the lists [`path-item-servers`](#path-item-servers) keeps, and reported as an
`openapi/duplicate-server-name` warning at the repeat's name, naming the first.

- Cases: `servers-variables`
- Codes: `openapi/duplicate-server-name`
- Tests: `openapi.TestDuplicateServerName_BothServersLower`,
  `load.TestServerRepeats_ReportsEveryListTheSourceWrites`,
  `load.TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly`
- Status: partly built: the default server the parser injects for a document writing none is lowered
  as if declared, and a server has no provenance (#672); a variable's `default` cannot tell an empty
  default from none (#883); and a servers list only a `$ref` reaches is never checked for a repeated
  name (#880).

## 15. Vendor extensions and policy

### `vendor-extensions`

An `x-*` key is legal on every object, so it is read at every object that admits one and kept
verbatim under `vendor_extension` ([`verbatim-preservation`](#verbatim-preservation)), with no
diagnostic. An object that lowers to a node with an `Unmodeled` map of its own keeps its extensions
there, unscoped. One that lowers to no node of its own is keyed by the path from its carrier down to
it, since several such objects reach one map and an unscoped key would leave the survivor to
lowering order (§12): on the Document, `openapi:info/x-*`, `openapi:info/contact/x-*`,
`openapi:info/license/x-*`, `openapi:externalDocs/x-*`, `openapi:components/x-*`,
`openapi:tags/<i>/x-*` and `openapi:tags/<i>/externalDocs/x-*`; on the service, `openapi:paths/x-*`;
on each operation, the path item's `openapi:pathItem/x-*`, and `openapi:responses/x-*` and
`openapi:externalDocs/x-*`; on the HTTP binding, `openapi:callbacks/<name>/x-*`; on a content,
`openapi:encoding/<part>/x-*` and `openapi:itemEncoding/x-*`; on a schema's node, `openapi:xml/x-*`,
`openapi:discriminator/x-*` and `openapi:externalDocs/x-*`; and on a security scheme,
`openapi:flows/x-*`. `<part>` and `<name>` are escaped as RFC 6901 tokens, so a name the document
chooses stays one segment (§12). A Link Object's own extensions ride inside the verbatim `links`
entry rather than taking a key beside it, and a path item that mounts no operation keeps its own
under `openapi:pathItem/<item pointer>/x-*` ([`path-item-servers`](#path-item-servers)). An
extension whose value converts to no JSON reaches the IR in no form, reported as an
`openapi/degraded-construct` warning at the object holding it.

- Cases: `extensions-x`, `inline-annotations`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperation_UnserializableExtensionStillWarns`,
  `operation.TestEncoding_PartNameWithSlashKeepsItsOwnKey`
- Status: partly built: an object kept verbatim also keeps its extensions under a scoped key (#849).

### `unknown-keys`

A key an OpenAPI object neither defines nor admits as an `x-` extension is kept verbatim under
`openapi:<key>` in the `Unmodeled` of the node that carries the object (`out_of_scope`), reported as
an `openapi/unknown-object-key` warning at the key, since the object's key set is closed and such a
key is usually a misspelling of its neighbour; an object that lowers to no node of its own is keyed
beneath the scope its extensions use (`openapi:info/<key>`, `openapi:pathItem/<key>`,
[`vendor-extensions`](#vendor-extensions)). A path item's is judged like any other, a value carrying
a YAML anchor included. A schema keyword no field of the schema model names is kept the same way,
reported as an info `openapi/unknown-schema-keyword`, because JSON Schema obliges an implementation
to ignore a keyword it does not know; `$comment`, `$dynamicAnchor` and `$dynamicRef` are decided
elsewhere and left out. One object contributes at most `annotation.MaxUnknownKeys` keys, taken in
byte order after the keys another reader already kept at the same pointer are set aside, and the
rest reach the IR in no form, reported as one `openapi/unknown-key-budget` warning at the object.
Two other outcomes also keep nothing and warn: `openapi/unknown-key-entry-taken`, where
`openapi:<key>` is already held by a construct written elsewhere on a carrier two objects share, a
parameter's own key and its schema's keyword, whichever lowered first surviving; and
`openapi/unknown-key-unreachable`, where the key is merged in through `<<` and the raw mapping holds
no pair for it.

- Cases: `path-item-anchored-key`
- Codes: `openapi/unknown-object-key`, `openapi/unknown-schema-keyword`,
  `openapi/unknown-key-budget`, `openapi/unknown-key-entry-taken`, `openapi/unknown-key-unreachable`
- Tests: `openapi.TestUnknownKeys_KeptAtEveryObject`,
  `openapi.TestUnknownKeys_SchemaAndObjectAreGradedApart`,
  `annotation.TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes`
- Status: partly built: a parameter's key and its schema's keyword of one spelling share an entry
  (#396), and a key merged in through `<<` is reported and kept nowhere (#395).

### `extension-promotion`

A vendor extension the promotion policy maps to a typed field is read into that field as well as
kept (§12 states the four rules a promotion keeps). The policy is `Options.Promotions`, on by
default: its `Targets` replace the default mapping — `x-deprecated-reason` to `Deprecation.Message`,
`x-deprecated-since` to `Deprecation.Since`, `x-sunset` to `Deprecation.RemovalDate`, and
`x-extensible-enum` to an `Enum`'s openness — and its `Disabled` turns it off. A deprecation member
is filled on an operation, a parameter, a header, a property, a type node or a security scheme that
is deprecated; on one that is not, the key annotates nothing and stays where it is. A node a
promotion writes into lists the marker `extension-promotion` in its `Provenance.Inferred`, and the
extension stays in `Unmodeled` untouched.

A value the mapped field cannot hold — anything but text, for the four `Deprecation` members — is
reported as an info `openapi/degraded-construct` at the extension and not coerced, since the
document means something else by the key. Text of the right JSON shape is taken as written:
`x-sunset` fills `RemovalDate` and not `RemovalVersion` because the header it echoes ([RFC
8594](https://www.rfc-editor.org/rfc/rfc8594)) is a date by definition, and the mapping is where
that reading is stated — the promotion does not then parse the date to confirm it. No default key
names `RemovalVersion`: a document stating a removal *version* names its own key, per §12's rule 1.

`Enum.Closed` is the one target whose fact is stated by a key being *present* rather than by a
value, so nothing is read or reported there. Its default key, `x-extensible-enum`, writes the member
list as its own value, and a list of members says nothing about openness that the key naming it has
not already said; the promotion therefore clears `Closed` on presence. A boolean value is the one
shape that does state openness by itself, and an explicit `false` is read as written rather than
inverted. Only openness is ever promoted: a schema's `enum` is closed by definition, so a document
declares the open case or nothing, and the mapping names that fact rather than the field's own
polarity. A document that writes `x-extensible-enum` *instead* of `enum` lowers to no `Enum` at all
and there is no node to open — minting one from a vendor key would be a compiler reading a member
list out of an extension, not a promotion, so the entry is left for a consumer that wants to.

- Cases: `extension-promotion`
- Codes: `openapi/degraded-construct`
- Tests: `lowering.TestPromoteDeprecation_ValueThatIsNotTextIsReported`,
  `lowering.TestPromoteEnumOpenness_WritesNothing`,
  `openapi.TestPromotion_RemovalDateAndVersionAreSeparateFacts`
- Status: partly built: the default mapping covers only deprecation and extensible enums (#656); two
  keys mapped to one field resolve by sort order in silence (#655); a mapping cannot say which
  extension values state the fact (#654); and an unknown key under a prefix the policy owns is kept
  in silence (#657).

### `mutually-exclusive-parameters`

The groups an `x-mutually-exclusive-parameter-groups` extension declares are kept as any extension
is ([`vendor-extensions`](#vendor-extensions)). Their promotion, which needs no node of its own, is
to be a pass that synthesizes one logical `Parameter` typed by a `Union` of variant models, each
field bound through `HTTPParamBinding.ParamPath`.

- Status: not built: no pass promotes the groups yet (#988).

### `pagination-policy`

Pagination is set only by an injectable policy, and what it sets is marked `Inferred` (invariant 6):
the compiler reads no pagination out of a document's parameter or response names by itself.

- Status: not built: no policy infers pagination yet, so the compiler sets no `Pagination` (#989),
  and the IR cannot address a request-body input, a page count or a stop condition (#665).

## 16. Swagger 2.0

### `swagger-lift`

A Swagger 2.0 document is to be lifted to the OpenAPI 3.x shape first — `body` and `formData`
parameters to a `Payload`, `host`, `basePath` and `schemes` to `Servers`, `consumes` and `produces`
to content types — and then lowered by the rules above.

- Status: not built: a Swagger 2.0 document is recognized and refused with
  `engine/no-compiler-for-format` until milestone 2, and the lift is to keep provenance pointing
  into the 2.0 source (#681).
