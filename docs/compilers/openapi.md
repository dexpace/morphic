# OpenAPI compiler — lowering specification

This document is normative for `compilers/openapi`, as the [IR design](../ir/README.md) is
for the IR: it states which OpenAPI 3.0, 3.1 and 3.2 documents the compiler reads, how each
construct of one lowers into the IR, where it keeps what the IR does not model, and every diagnostic
it reports. The IR's shapes, and what a consumer may rely on, are the IR design's, and nothing here
restates them.

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
  compiler's own package, `e2e` for its end-to-end tests, an internal package's name otherwise.
- **Status** — what the rule leaves open: where the compiler does not yet do all the rule says, or a
  question the rule does not settle, each naming the issue that tracks it, or the milestone that
  schedules work not yet begun. An issue is named here and nowhere in a rule's text.

The fields are the only bullets a rule writes at the margin, since each is read as one; a list in a
rule's text is numbered.

A rule names a case or a test, or says in its Status what it leaves open. The `TestLoweringSpec`
tests in `compilers/openapi/e2e` hold every field to the code and read it both ways: each
conformance case is named by a rule, and each code by a rule or by `codesOutsideTheRules`, which
gives the reason a code has no rule. [`CONTRIBUTING.md`](../../CONTRIBUTING.md) walks adding a rule.

## 1. Reading a document

### `openapi-versions`

A document declaring `openapi: 3.0.x`, `3.1.x` or `3.2.x` is read as the minor its first two parts
name, and every rule below that differs by minor follows that one. So is a document a reference
reaches beside the source: its schemas are read in that minor's dialect, where neither its
`jsonSchemaDialect` nor a schema's `$schema` names another ([`dialect-keywords`](#dialect-keywords)),
its Encoding Objects take that minor's defaults, its objects are held to that minor's table, and its
`Document.Sources` entry names that minor. One declaring no version, such as a bare schema file, or
a version the compiler does not read, such as `3.3.0` or a Swagger 2.0 file, is read at the source's
minor, and the compile stands. A source declaring a version that is not three runs of digits, major,
minor and patch, such as `3.0` or `3.1.0-rc1`, is reported as an `openapi/validation` error, of the
class `validation-invalid-format` ([`validation`](#validation)), and still lowers under the minor it
names; a document a reference reaches is read at the minor such a version names too, with no
finding, since only what a reference reaches is judged. Any other version is not this compiler's.
The engine refuses a document declaring one before any compiler runs, as it refuses a Swagger 2.0
document ([`swagger-lift`](#swagger-lift)), and the compiler, handed one directly, refuses it with
an `openapi/unsupported-version` error and compiles to no Document.

- Codes: `openapi/unsupported-version`, `openapi/validation`
- Tests: `openapi.TestCompile_UnsupportedVersion`,
  `load.TestSupportedMinor_AcceptsOnlyTheThreeMinors`,
  `e2e.TestDocumentMinor_EachDocumentIsReadAtTheMinorItDeclares`,
  `e2e.TestDocumentMinor_AnObjectIsHeldToItsDocumentsTable`,
  `e2e.TestDocumentMinor_ANullIsReadInTheDialectOfItsDocument`,
  `e2e.TestDocumentMinor_AdmissionReadsEachSchemaInItsDialect`,
  `e2e.TestDocumentMinor_DocumentsOfTwoMinorsAreEachReadAtTheirOwn`,
  `e2e.TestDocumentMinor_AnEncodingDefaultsAtItsDocumentsMinor`,
  `oas.TestCheck_TheOpenAPIFieldIsAVersionNumber`,
  `detect.TestRecognize_AVersionThatIsNotServedIsStillADeclaration`

### `yaml-decoding`

A source is one YAML document, a JSON one included. A source that declares an `openapi` or `swagger`
version and does not parse is reported, when the engine asks the compiler whether the source is its,
as an `openapi/undecodable-source` error, and nothing compiles. A YAML stream lowers the first of
its documents that holds content; the documents after it that hold content reach the IR in no form,
reported once as an `openapi/stream-documents-dropped` error at the first of them, and the first
still lowers. A document holding nothing, a bare separator or an explicit null, is not counted.
Detection reads the version off the root mapping as the parser does, through aliases, `<<` merges
and a repeated key's last value, so the compile reads the version the source is routed by. A root
that merges deeper than that reading expands is declined as an `openapi/budget-exceeded` error,
whatever its format, since a version past the bound may outrank one found above it, unless the
root's own keys settle the version: it writes `openapi` with a version, or writes both keys. A
`swagger` it writes alone settles nothing, as an `openapi` past the bound would outrank it. A merge
cycle hides no key, and is the compile's to refuse. A mapping tagged anything but `!!map`, which
OpenAPI's limit on YAML tags forbids, refuses the source before the parser reads it, as an
`openapi/tagged-mapping` error at the tagged mapping with the least pointer, and it compiles to no
Document. A tagged scalar is not refused here; what it lowers to is [`values`](#values)'.

A date such as `2021-01-01`, and a `<<` anywhere but as a mapping's key, written with no tag or
under YAML's non-specific `!`, are the strings they spell, in the source and in each document read
beside it. YAML 1.2, which OpenAPI recommends, has no timestamp or merge type: its core schema reads
both as strings, `!` makes any scalar one, and `!!str` is the one tag of the JSON schema ruleset, to
which OpenAPI limits YAML's tags, that holds them. The parser tags the first a timestamp and the
second a merge, from neither of which its model decodes a string
([`undecoded-scalar`](../parser-library.md#undecoded-scalar)), so the compiler tags each a string
before the model is built: a field so written holds its text, and a `$ref` so written is followed
like any other. A `<<` key still merges ([`merge-keys`](../parser-library.md#merge-keys)), and a
scalar under any other tag keeps it.

- Cases: `yaml-timestamp-scalars`
- Codes: `openapi/undecodable-source`, `openapi/stream-documents-dropped`, `openapi/tagged-mapping`,
  `openapi/budget-exceeded`
- Tests: `detect.TestRecognize_ABrokenDocumentIsReportedAtAnySize`,
  `openapi.TestDetect_ReadsTheVersionTheCompileReads`,
  `detect.TestRecognize_AReadCutShortIsReported`,
  `detect.TestSniff_ReadsAMergeChainAsFarAsTheViewExpandsIt`,
  `detect.TestRecognize_AMergeCycleHidesNoKey`,
  `load.TestLoad_AStreamLowersItsFirstDocumentAndReportsTheRest`,
  `load.TestLoad_ATaggedMappingIsRefusedBeforeParsing`,
  `e2e.TestCompile_APlainDateCompilesAsItsQuotedTwin`,
  `e2e.TestCompile_APlainMergeValueCompilesAsItsQuotedTwin`,
  `e2e.TestCompile_APlainDateInAnExternalDocumentCompilesAsItsTwin`,
  `e2e.TestCompile_APlainDateAnOverlayWritesCompilesAsItsTwin`,
  `e2e.TestCompile_ADateTaggedOutrightKeepsItsTag`,
  `load.TestResolveExternal_RecoversAPlainDateBehindARespelledURL`, `load.TestUnready`
- Status: partly built: a JSON source holding a surrogate-pair escape is refused as undecodable
  (#900). A scalar or a sequence under a tag outside YAML's JSON schema ruleset is not reported,
  and where the model reads a string or a list it reads as left out (#1368).

### `source-budgets`

`Options.Limits` bounds how large a source the compiler lowers, and how much its lowering copies,
each budget taking its default when unset and none when negative. A source past `MaxSourceBytes` is
refused before it is parsed, as one past the budget rather than one of a size: `MaxSourceBytes` is
the compiler's `SourceLimit`, so a caller may hand detection only its first bytes, one past the
budget. A source whose YAML parses to
more than `MaxSourceNodes` nodes, or whose aliases add more than `MaxAliasSurplus` nodes to it, is
refused before its model is built. Each is an `openapi/budget-exceeded` error, compiling to no
Document. Past `MaxExternalDocuments`, a document an external reference names is not read, each
reference into it is unresolved ([`references`](#references)), and the compile reports
`openapi/budget-exceeded` once. The rule table ([`validation`](#validation)) reads no more objects
than the node and alias budgets admit together, without bound where either has none, and nests no
deeper than the scan's depth bound, so it stops on no document they admit; a check it does stop is
an `openapi/budget-exceeded` error at the object it stopped on. Four bounds hold whatever the caller
sets, since past them no answer the pre-parse scan gives is whole, or what the compile spends is
out of proportion to the document: a document nested past the scan's depth bound, its aliases
expanded, is refused as an `openapi/source-too-deep` error at the least pointer where its nesting
passes the bound; one with more nodes than the scan indexes, as an `openapi/source-too-large`
error; one whose aliases expand it past a fixed multiple of its own size, with a floor for a small
document, as an `openapi/alias-amplification` error at the least pointer of the smallest structures
past the ratio; and one whose nesting and aliases the scan admits, but whose nodes' JSON pointers,
its aliases expanded, total past a fixed multiple of the bytes it writes, with a floor for a small
document, as an `openapi/pointer-amplification` error at the document, since the compile spells a
pointer for each node it reads and the IR's IDs embed them. An overlay is held to the same refusals
before it is applied, and so is a document an external reference names, which fails the reference
instead; each is read no further than the byte budget. The member budget of an enum is
[`enum-const`](#enum-const)'s.

`MaxReferenceExpansion` bounds what the lowering copies through `$ref`s, the product of references
and size the budgets above leave free. A declaration a `$ref` names, a schema aside,
lowers again at each `$ref` naming it, so each such `$ref` is charged what the declarations its
chain names weigh: the one it ends at, and each component alias it passes through, whose fields
beside its own `$ref` lower at every use too
([`reference-object-siblings`](#reference-object-siblings),
[`path-item-ref-fields`](#path-item-ref-fields)). A declaration weighs a node for each YAML node, a
YAML alias as a copy of what it names, and a byte for each byte of scalar text, but a schema it
holds weighs one node whatever the schema holds: it lowers once, to the type every copy refers to. A
schema is weighed whole beneath a merge key, which the grammar reads as raw YAML, and wherever the
grammar cannot place the `$ref`'s position or reads no object a `$ref` names there. Every `$ref` is
charged, the first to a declaration and one nested in another's among them, since each lowers a
copy, a link's among them, since each response naming one lowers its own. A schema's `$ref` names
one registry entry ([`schema-registry`](#schema-registry)), so it is not charged. A path item's operations copy what the item
declares itself. Its fixed methods bound those copies, and OpenAPI 3.2's `additionalOperations` does
not, so each operation that field declares is charged too, the first among them, which charges an
item the same in any order of its entries: an inline parameter of the item that it takes, weighed in
the document writing it as a `$ref`'s declaration is, the entries it keeps of the item, a byte for
each byte of their keywords and raw values and one between, the item's summary and description its
`PathDocs` holds ([`path-item-docs`](#path-item-docs)), a byte for each byte of their keywords and
text and one between, and the IDs of the item's servers where it names none of its own
([`path-item-servers`](#path-item-servers)). Past the budget no `$ref` lowers, nor such an
operation, and the compile is refused as one `openapi/budget-exceeded` error at the source and no
position in it, compiling to no Document, beside what the load phase found and nothing the lowering
did: which copy crossed the budget, and so what lowered before it, follows the walk's order. With no
Document to list the documents `$ref`s read, what the load phase found in one of them is left out
too.

- Codes: `openapi/budget-exceeded`, `openapi/source-too-deep`, `openapi/source-too-large`,
  `openapi/alias-amplification`, `openapi/pointer-amplification`
- Tests: `openapi.TestCompile_SourceByteBudgetBindsOnlyPastTheLimit`,
  `openapi.TestCompiler_SourceLimitReadsTheCallersOptions`,
  `load.TestLoad_ANamedOverlayPastTheBudgetIsRefused`,
  `openapi.TestCompile_AnUnboundedAliasBudgetStillRefusesABomb`,
  `load.TestLoad_RefusesANestingPastTheBoundBeforeBuildingTheModel`,
  `load.TestLoad_RefusesLongKeysNestedDeepBeforeBuildingTheModel`,
  `load.TestLoad_ReportsAShapeOnceItsNestingRefuses`,
  `load.TestLoad_ADocumentTooLargeToIndexIsRefused`,
  `openapi.TestCompile_TheDocumentBudgetIsTheCallersToSet`,
  `load.TestJudgeSource_ReadsEveryObjectTheCompilerAdmits`, `oas.TestCheck_WorkIsBounded`,
  `openapi.TestCompile_ReferenceExpansionBindsOnlyPastTheLimit`,
  `openapi.TestCompile_EveryCopiedDeclarationIsCharged`,
  `openapi.TestCompile_AReferenceIntoAnotherDeclarationIsChargedInFull`,
  `openapi.TestCompile_AReferenceExpansionRefusalIsTheSameInEitherOrder`,
  `openapi.TestCompile_AReferenceExpansionRefusalKeepsWhatItsTableCanPlace`,
  `openapi.TestCompile_ASchemaIsChargedAsOneNode`,
  `openapi.TestCompile_AnAliasIsChargedBesideItsEnd`, `lowering.TestExpansion_ChargesEveryCopy`,
  `lowering.TestWeightOf_CostsTheBudgetNotTheDeclaration`,
  `lowering.TestWeightOf_CountsASchemaAsOneNode`,
  `lowering.TestDeclared_ChargesEachDeclarationOfAChain`,
  `lowering.TestDeclared_ResolvesNoReferencePastTheExpansionBudget`,
  `openapi.TestCompile_APathItemsAdditionalOperationsAreChargedWhatTheyCopy`,
  `openapi.TestCompile_APathItemsFixedMethodsAreNotCharged`,
  `openapi.TestCompile_AnItemsAdditionalOperationsAreChargedInAnyOrder`,
  `openapi.TestCompile_AnItemsParameterIsWeighedInItsOwnDocument`,
  `operation.TestLowerService_NoAdditionalOperationLowersPastTheBudget`
- Status: partly built: an external object reached after its contents is charged against the
  document budget twice (#571), aliases of a long scalar (#1429) cost gigabytes within every
  bound, and the pointer bound counts the values a compile keeps verbatim, an example or an
  extension, whose nodes it never names one by one, so a document whose values nest long keys deep
  is refused though it would compile cheaply (#1499).

### `overlays`

An OpenAPI Overlay document handed in as `Options.Overlay` is applied to the source's parsed tree
before its model is built. Its bytes are the caller's, or, when it hands none, read in the compile
from the local file `Overlay.Path` names, through the caller's reader and unconfined, since the
caller named it and not the spec. One the reader cannot read is a Go error, as an unreadable spec
is, and so are an overlay naming no file and one naming an http(s) URL, since no setting takes a
compile onto the network. Once applied, the pre-parse refusals and the node budgets hold the patched
tree as they hold the source, and validation and every lowering read it. A refusal at a node the
overlay grafted names the overlay, and so does a refusal of the whole patched tree, such as its
pointers' bytes, since the source passed the same refusals alone; its message says it reads the
tree the overlay leaves, which the overlay's own refusal does not. Each action is charged the
nodes it can build before it runs: one whose charge would carry the tree, counted afresh, past
`MaxSourceNodes` is refused as an `openapi/budget-exceeded` error with nothing it would build built,
so it is refused while the tree is past the budget whatever a later action removes. An action that
builds nothing, such as a remove, is applied however large the tree, since the budget holds the tree
the overlay leaves. The overlay becomes a second entry in `Document.Sources`, and every position it
introduced or rewrote names the source as its `Provenance.Source` and that entry as its `WrittenBy`
([provenance](../ir/provenance.md)), while a position it left alone keeps the source's line and
column; a finding about the overlay document itself names the overlay as its `Source`. An overlay
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
the overlay is applied and no position names it as a writer, as with no overlay, reported as an
`openapi/overlay-origin-incomplete` warning.

- Codes: `openapi/invalid-overlay`, `openapi/overlay-failed`, `openapi/overlay-action`,
  `openapi/overlay-origin-incomplete`, `openapi/budget-exceeded`
- Tests: `overlay.TestApply_RejectsAnOverlayItCannotRead`,
  `load.TestLoad_ReadsANamedOverlayThroughTheReader`,
  `load.TestLoad_ANamedOverlayThatCannotBeReadIsAGoError`,
  `e2e.TestCompile_StrictRefusesAnActionThatMatchesNothing`,
  `overlay.TestApply_StrictReportsAnActionThatChangesNothingWithoutRefusing`,
  `overlay.TestApplyWithin_DegradesToTheSourceAtTheNodeBudget`,
  `e2e.TestCompile_OverlayIsRecordedAsASource`, `load.TestLoad_AnOverlayBuildsWithinTheNodeBudget`,
  `overlay.TestSequence_ChargesAnActionToTheBudgetsEdge`,
  `overlay.TestSequence_ChargesAnActionAfterARemoveOnTheTreeItLeft`,
  `load.TestLoad_TheNodeBudgetReadsTheTreeTheOverlayLeaves`,
  `load.TestLoad_APointerAmplificationAnOverlayGraftedNamesTheOverlay`
- Status: partly built: an Overlay 1.0.0 document's RFC 9535 filter selector fails to apply (#651).
  Open: whether lax application should report an action that matches nothing (#652), how an update
  merges a nested array (#653), and whether an overlay that replaces a value the source declares is
  reported (#588).

### `validation`

The load phase holds the source to a table of the OpenAPI Specification's rules, at the minor it
declares, and the parser's own validation does not run. Each row of the table cites the sentence of
the specification it comes from. It gives every object's fields, the shape, values and syntax of
each, which fields each minor requires, the rules that bind fields together, and the defaults of
fields left unwritten that a lowering reads ([`multipart-encoding`](#multipart-encoding)); what data
cannot state is written as code beside it, such as a path template against its path parameters, a
server URL against its variables, a tag's parent, and where an encoding applies. Each finding is reported
as `openapi/validation` suffixed with its class (`openapi/validation/validation-required-field`), at
the node it is about, its message naming the object, the field, the minor and the section of the
specification it cites. A finding never refuses the document,
which lowers as written.

A finding is an error where the document's meaning is missing or contradicted: a REQUIRED field
absent, a value of the wrong type, a value outside the set the specification allows, fields that
exclude each other written together, a path template and its path parameters disagreeing, a list or
map holding fewer entries than the specification requires or repeating one it requires to be unique,
as JSON Schema requires of `allOf` and of `required`, or a value a grammar the specification cites
refuses, such as a token or an anchor's name. Every mapping, values and extensions included, is held
to the Format section's rule that a key is a string, so a key that is no scalar, which the parser
reads as the empty string ([`non-scalar-key`](../parser-library.md#non-scalar-key)), is an error,
and so is a `<<` merge key naming neither a mapping nor a list of mappings, from which the parser
merges nothing ([`merge-keys`](../parser-library.md#merge-keys)). It is a warning where the document
breaks a rule of its version and still says what it means: a key outside the pattern the
specification gives, a key a mapping writes twice, a number or a boolean where a string belongs, a
field written where the specification gives it no effect (`validation-no-effect`), or a `null`
written for an optional field, which is read as the field left out, by the rules that bind it to
other fields as well. Every keyword of a Schema Object is such a field, since the schema lowering
reads its `null` as left out too ([`null-keywords`](#null-keywords)), a numeric bound included,
whose shape the table otherwise leaves to that lowering. A `null` written
as a schema entry, which is another object's `schema`, a component, or an entry of a map or a list
of schemas, is an error, since that lowering reads it as the empty schema, a value, and a list
counts it among its entries. One written for a field
whose requirement another rule reports is left to that rule.
A security requirement's name is no field to leave out: its `null` lists no scopes, and the name
stands ([`security-requirements`](#security-requirements)).

The source is read as it writes it, an applied overlay's patch included, each object as the kind
its position gives it. An object a `$ref` reaches that the source's own reading did not take as
that kind, one under an `x-` key or in another document, is read as the kind the reference
expects and held to the minor of the document that writes it
([`openapi-versions`](#openapi-versions)). A finding about it is placed at its node, in the entry of
`Document.Sources` for the document that writes that node, a file the source spells two ways being
one document. One about a node of a document no entry lists goes to the least `$ref` reaching it,
its message naming where in that document it lies. Each finding is made once: a node an alias or a
merge key mounts at several positions draws one, placed where the node is written and naming the
position in that document that first read it.

A rule another lowering reports is stated in the table and left to it, so a document draws one
report: [`operation-ids`](#operation-ids), a repeated parameter ([`parameters`](#parameters)), a
security requirement naming no scheme and a scheme naming no mechanism
([`security-requirements`](#security-requirements)), an array schema without `items` in 3.0
([`missing-array-items`](#missing-array-items)), a numeric bound that is no decimal a `BigVal` holds
([`constraints`](#constraints)), and a request body's empty `content`
([`request-body`](#request-body)). A construct a later minor added is
[`later-version-construct`](#later-version-construct)'s, and a keyword written where its location
gives it no effect is [`allow-reserved-locations`](#allow-reserved-locations)' and
[`querystring`](#querystring)'s.

- Codes: `openapi/validation`
- Tests: `oas.TestCheck_RulesGradeByLevel`,
  `oas.TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional`, `oas.TestCheck_CountsBoundTheEntries`,
  `oas.TestCheck_ANullReadAsLeftOutIsNoFieldToARule`, `oas.TestCheck_ShapesFollowTheField`,
  `oas.TestCheck_NumbersFollowTheirShape`,
  `oas.TestCheck_PathTemplatesAnswerToPathParameters`,
  `oas.TestCheck_AServerURLNamesOnlyItsVariables`,
  `oas.TestCheck_EncodingsApplyWhereTheSpecificationSays`,
  `oas.TestCheck_AFindingIsMadeOnceWhereItIsPlaced`,
  `oas.TestCheck_RulesAnotherPackageOwnsAreNotApplied`,
  `oas.TestCheck_AnAnchorIsANameJSONSchemaAdmits`,
  `oas.TestCheck_KeywordsHoldWhatJSONSchemaRequires`, `oas.TestCheckFormat_AKeyIsAString`,
  `oas.TestTable_EveryRowCitesTheSpecification`, `load.TestJudgeSource_PlacesEachFindingAtItsNode`,
  `load.TestJudgeSource_RefusesTheAnchorsNoReferenceCanName`,
  `load.TestLoad_WhatAnAliasMountsIsReportedOnce`,
  `load.TestResolve_AnObjectReachedTwiceIsReportedOnce`,
  `load.TestResolve_AFileSpelledTwoWaysPlacesAFindingOnce`,
  `load.TestResolve_AReachedFindingNamesThePositionThatReadIt`,
  `e2e.TestCompile_AFindingInAnExternalDocumentKeepsItsRule`,
  `canary.TestValidationOracle_AgreesWithTheLibrary`
- Status: partly built: a schema read in a JSON Schema draft ([`dialect-keywords`](#dialect-keywords))
  is held to its minor's keyword rows, so a keyword the draft constrains otherwise passes as 2020-12
  admits it, and a keyword only the draft defines, or a `definitions` entry no `$ref` reaches, is
  held to none (#1466).

### `minor-required-fields`

A document is held to the fields its declared minor requires, which its own Fixed Fields say: an
Operation's `responses` is required in 3.0 and not from 3.1, and a Response's `description` up to
3.1 and not in 3.2; 3.0 requires `paths`, while 3.1 and 3.2 require at least one of `paths`,
`components` and `webhooks`. A required field left out is reported as an `openapi/validation`
error, of the class `validation-required-field`, at the object ([`validation`](#validation)),
and one a rule requires only beside another field's value, such as an oauth2 scheme's `flows`, only
where that value is written. A required field written as the empty string, or as an empty map, is
written, since every minor types it as a bare string or map, and stands unless the specification,
or a grammar it cites, refuses the empty value: an enumerated field (a Parameter's `in`, a security
scheme's `type`); a path parameter's `name`, which must match a template expression, none of which
the empty string names; a name sent in a header or a cookie, by a parameter or an API key, and an
HTTP `scheme`, each a token of RFC 9110 or RFC 6265; and a server `url` from 3.2, whose server URL
grammar requires a character. A name sent in the query, or a `querystring` parameter's, which is
never sent, is admitted. A `null` written for a required field is the value missing, an error at
the field.

- Cases: `empty-required-values`, `relaxed-required-fields`
- Codes: `openapi/validation`
- Tests: `oas.TestCheck_RequiredFieldsFollowTheMinor`,
  `oas.TestCheck_AtLeastOneSectionStandsInADocument`,
  `oas.TestCheck_EmptyStringsAreRefusedWhereTheGrammarSaysSo`,
  `oas.TestCheck_NamesSentInAHeaderOrACookieAreTokens`, `oas.TestCheck_AnHTTPSchemeIsAToken`

### `later-version-construct`

A document may write a field, or one value of a field, that a later minor of its format added to the
object it sits on: a `name` on an OpenAPI 3.1 Server Object, `in: querystring` on a 3.0 Parameter
Object. It lowers exactly as in a document declaring the minor that added it, but for the one case
set out below, whose keywords are read as the declared minor defines them, and is reported once, at
its own key, as an `openapi/later-version-construct` warning. That reading loses the least: from
the lowering and the diagnostic a consumer can still set the construct aside, while from a verbatim
copy it could not rebuild the operation a `query` key declares. Except in the one case set out
below, the declared minor gives the construct no meaning at all, so the lowering asserts nothing the
document does not write. It is a warning, as an undefined key is ([`unknown-keys`](#unknown-keys)):
an OpenAPI object's key set is closed, so the document is invalid for the version it declares, but
it still lowers, and a consumer that wants it refused has `--fail-on warning`. The rule covers
constructs a minor *added*, including the Reference Object's `summary` and `description`, which 3.1
added and which [`reference-object-siblings`](#reference-object-siblings) folds at every version,
and every keyword beside a Schema Object's `$ref`, which 3.1 applies alongside the reference as JSON
Schema 2020-12 conjoins them ([`ref-use-site`](#ref-use-site)). That is the one case: 3.0 reads
either position as a Reference Object, which admits no key beside its `$ref` and says one SHALL be
ignored, so a 3.0 document writing one is warned that it was applied as 3.1 applies it. It is then
read as the declared minor's dialect defines it, the only reading that gives what the author wrote
its meaning: a `nullable` there adds `null` to a `type` written beside it
([`nullable`](#nullable)), an `exclusiveMinimum` there is a boolean modifying the bound beside it
([`constraints`](#constraints)), and a `type: array` there requires `items` unless what the `$ref`
lands on supplies one ([`missing-array-items`](#missing-array-items)).

Whether a construct is a later minor's is asked in one place, the table [`validation`](#validation)
holds a document to, which gives the minor each field and value was added in; no lowering asks it,
so no field's treatment can depend on whether the parser happens to model it. How a schema keyword
reads at its document's minor is asked of the same table, the Schema Object's rows at that minor:
whether it defines `nullable`, the shape `exclusiveMinimum` takes, and whether `items` is required
beside `type: array`; and what an Encoding Object leaves unwritten defaults to is asked of the
Encoding Object's rows, the `contentType` of a part writing no `type`
([`multipart-encoding`](#multipart-encoding)). No lowering asks the version: outside format
detection, which reads it to name the format a file declares, only the load phase and the table
read the version a document declares, and `make lint` refuses the parser library's version field
and accessor, and the table's reading of a version, anywhere else (a `forbidigo` rule in
`.golangci.yml`). The construct's value is held to the shape its table row gives it, and any object
inside it to the table like any other.
The keys beside a `$ref` are judged as the object they belong to: a Reference Object's, a Path
Item's own fields beside its `$ref`, or a schema's keywords, which the schema lowering applies beside
a 3.0 `$ref` too. Those are held to 3.0's keywords as written and, since 3.0 ignores them, each is
warned of as a later minor's construct as well, whatever it is, so no row of the table names them.
JSON Schema drafts 4, 6 and 7 ignore a schema's keywords beside its `$ref` too, and 2019-09 applies
them, so in a schema read in one of those drafts ([`dialect-keywords`](#dialect-keywords)) the
schema lowering applies each as 2020-12 does and warns of each field the draft's rows define at its
key, OpenAPI's own among them, bar those whose reading applying them leaves alike: the `$schema`
naming the draft, `definitions`, whose schemas only a `$ref` reads, and `$comment`. The identifier
is not applied, as applying it would change what the `$ref` names: it names nothing there, as the
draft has it ([`references`](#references)), and draws no warning. A key the rows define nothing
for, an extension or an unknown keyword, is kept as written either way, and draws none.
An object a `$ref` reaches is judged as the source's own are, wherever it is written, and reported
where [`validation`](#validation) places a finding about it. A
Path Item's `additionalOperations` before 3.2, and a field 3.2 added to a Discriminator or XML
Object, draw the warning as any other construct does. A keyword a later minor's JSON Schema dialect
added is read by the schema lowering as it finds it, so the table leaves it alone. The same table
judges a Parameter Object's `allowReserved` against where the declared minor applies it
([`allow-reserved-locations`](#allow-reserved-locations)), and its `allowEmptyValue` against the
style it is written under ([`allow-empty-value`](#allow-empty-value)).

- Cases: `later-version-constructs`, `reached-outside-model`, `schema-ref-siblings-30`,
  `schema-drafts`
- Codes: `openapi/later-version-construct`
- Tests: `e2e.TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem`,
  `e2e.TestOlderDrafts_RefSiblingsAreWarnedOf`,
  `e2e.TestOlderDrafts_AnIdentifierBesideARefSetsNoBase`,
  `e2e.TestSchemaRefSiblings_ApplyAsIn31`, `e2e.TestSchemaRefSiblings_ReadInTheDeclaredDialect`,
  `oas.TestCheck_LaterConstructsAreWarnedOfInTheCompilersWords`,
  `oas.TestCheck_EveryKeyBesideA30SchemaRefIsALaterConstruct`,
  `oas.TestCheck_ValuesAddedLaterAreWarnedOf`,
  `oas.TestTable_AgreesWithTheConstructsLaterMinorsAdded`,
  `load.TestResolve_AReachedObjectIsHeldToTheLaterVersionTable`
- Status: partly built: a JSON Schema 2020-12 keyword in a 3.0 schema lowers with no diagnostic
  (#820).

### `references`

A `$ref` resolves within the source, as a JSON pointer. One naming another document is read through
the reader the caller supplies (`compilers.Options.Reader`), against the directory of the source's
path, each file or URL read once however many references name it, and held to the source's budgets
and refusals ([`source-budgets`](#source-budgets)), where a document past one fails each reference
naming it. The read stays inside the directory of the source's path, symbolic links included, and
follows relative references alone, unless `Options.AllowExternalRefs` lets it reach any file and
http(s) URLs: a `$ref` naming a document by a `file:` URI, or one other than the source by an
absolute path, fails unread, whatever another `$ref` names that document by. Each is judged by its
own spelling, so what the default reads depends neither on how the working directory is spelled nor
on the order references are read in. In either mode a `$ref` naming a URL whose scheme is none of
`http`, `https` and `file` fails unread, where a reader would take it for a path against the working
directory; a file whose name holds a colon is written `./a:b.yaml`. A document outside fails each
reference naming it, and with no reader no document is read.

Each document a reference reaches is a `Document.Sources` entry after the source and its overlay,
ordered by its location from the source's directory, and what it declares lowers there, as the
source's own lowers where the source declares it: a schema a `$ref` lands on, and a parameter,
request body, response, header, example, callback, path item, security scheme or media type a
reference brings in, with the schemas inline in it. A reference object written in what another
document brings in is resolved against that document, as the source's are against the source, and
what it names lowers where it is declared, in whichever document, the source included. What a
document declares names its entry in its `Provenance`, and has IDs in that document's namespaces,
the source's qualified by its location (`t/openapi@common.yaml/components/schemas/Pet`,
[identity](../ir/identity.md)), so one pointer in two documents is two nodes. An operation and a
parameter keep the IDs of the mount that lowers them.

A content entry OpenAPI 3.2 lets a document write as a Reference Object, and an entry of
`components/mediaTypes`, is a reference object like any other, though the parser models neither
([`media-type-references`](../parser-library.md#media-type-references)): the load phase models each,
wherever a walk meets it, and hands it to the parser's resolver as it hands every other kind, so it
is read under the same reader, budgets and refusals, a chain of entries is followed as any chain is,
and what one reaches is held to the table ([`validation`](#validation)). What it names lowers once,
at the pointer declaring it, however many content entries reach it, with the references it holds
resolved where it is declared. The resolver reads a `$ref` into the source through the model, whose
own Media Type Object is no reference it can take, so each one the source writes is held where the
resolver looks first, and the source's media type references are resolved once its walk has held
them all: a `$ref` to an inline content entry reaches it, as one to an inline parameter does. One
the parser faults building a reference from is reported once, as an `openapi/validation` error where
it is written, and lowers nothing.

A schema `$ref`, and a discriminator mapping value spelled as one, is bound by the compiler itself,
never by the parser's resolver
([`schema-cycle-overflow`](../parser-library.md#schema-cycle-overflow)), and the answer depends on
nothing read before it. It is read against the base URI of the schema resource it is written in
(JSON Schema 2020-12 §8.2.1): the document, for a schema under no `$id`, as OAS 3.1 reads one, or
the nearest schema above it, itself included, whose `$id` names another URI. Its fragment is trimmed
of the white space around it and percent-decoded, nothing else, so a `+` names itself. A pointer is
read from the root of that resource, as RFC 6901 reads one: an index is a decimal with no leading
zero, a pointer ending in `/` names a member whose key is empty, and a fragment holding a second `#`
names nothing. Under an `$id` a pointer names a position of the resource, with no fallback, and `#`
names the schema writing the `$id`; under none, `#` names the document's root. A plain-name
fragment names the schema of that resource declaring it as its `$anchor` or `$dynamicAnchor`, in the
source or another document, so a name under an `$id` is that `$id`'s resource's and not its
document's, and a URI an `$id` declares names that schema, as one a `$ref`'s document part names; a
name or a URI two schemas declare names neither, and a name its schema's dialect's grammar refuses
is declared by none, the load phase reporting it: 2020-12's admits a leading `_` and no `:`, the
grammar of 2019-09's `$anchor` and of drafts 6 and 7's identifier naming a fragment alone the
reverse. Draft 4 states none, so its `id` naming a fragment alone declares any name no JSON pointer
spells, read as a `$ref`'s fragment is, and a `$ref` naming one no grammar admits resolves to it;
where no schema declares such a name, the `$ref` is reported as naming no JSON pointer.
An anchor keyword a draft defines none of, draft 7's `$anchor` or 2019-09's `$dynamicAnchor`, is
read as 2020-12 reads it, by 2020-12's grammar. Identifiers are read only where a schema is placed:
by the OpenAPI grammar in the source and in another document whose root names an `openapi` version,
so one in an example, an extension or the fields beside a reference object's `$ref` declares none. A
document that names none has no grammar, so it is read as a document of schemas: its root and any
key of no schema vocabulary hold schemas, and within one a keyword reads as it does in the source,
so one in a literal, a value or an extension declares none, and the document's root may be a schema
or a map of them. An `$id` gives its base to what is written below it by the same reading, wherever
it is read. A schema read in JSON Schema draft 4 ([`dialect-keywords`](#dialect-keywords)) spells
it `id`, its `$id` then naming nothing, and a document fetched over the network whose `$schema` or
`jsonSchemaDialect` names draft 4 is refused for an `id` naming a location on disk, as a `file:` URI
or a drive letter does, as for an `$id` reaching a local file, even one an example writes, since the
check reads no grammar. In a document naming no such dialect no `id` is an identifier, so none is
refused for one. In drafts 4, 6 and 7 one naming a fragment alone, `#name`, declares that plain
name as an `$anchor` does, which is why the load phase holds an `$id`'s format to the schema's
dialect rather than to the rule table's 2020-12 row; one
beside a `$ref` gives it no base, roots no resource and declares no name, as those drafts ignore
every keyword there; `definitions` holds schemas as `$defs` does, and so does `dependencies` where
a value is one. A schema another holds roots a resource where it writes an identifier as the
dialect embedding it spells one, as 2020-12 has an embedded resource do (§9.3.1), or as the dialect
its own `$schema` names does, unless it writes a `$ref` whose siblings the dialect embedding it
ignores. An `$id` names the URI its part before the `#` resolves to against the base above it, by
RFC 3986 where that base is a URI and as a `$ref`'s document is placed where it is a file's path,
and identifiers match as RFC 3986 §6.2.2 normalizes them. One with
a fragment that is not empty names none, as 2020-12 admits none, one naming the URI of the resource
holding it starts no resource, and one naming the source, however spelled, returns to the
document's. A URI no schema declares names the document at that URI, read as any other is: one read
against an `$id` is the URI the `$id` makes of it, never the file of that name beside the source,
and is judged for refusal as written while only relative paths moved its base, and as that URI once
an absolute `$id` did. Every document a reached reference names is parsed before any reference is
deemed unresolvable, as OAS 3.1.1 §4.3.1 requires, so a URI a document declares answers as that
identifier whichever reference comes first, and is never refused. References are read in passes
over the same set, each against the URIs the documents the last one reached declare, and only the
last pass's answers stand, so no declaration order reaches an answer. A document a pass read as a
location before another declared that URI is reached by no later pass, so it declares nothing,
lowers nothing and is no `Document.Sources` entry. Where what a pass reaches turns on what those
documents declare, so that no pass agrees with the one before it, every document read declares what
it does from the pass that would read against what an earlier one read. A pass reads the documents
absolute URIs name once its walks are done, together, and goes on, reading again only the references
that waited for them, while what the documents it reached declare changes no URI it asked for, one a
waiting reference asked for included. So a chain of documents each named by an absolute URI, none
declaring a URI a reference asked for, is read in one pass, its cost growing with its length. Where
one does, the pass is read again: a document held beside one declaring its URI is then read for
nothing, and a chain in which each document declares the URI the one before it asked for costs a
pass per link. A pointer passing a reference object reads on where the resolver resolved that, in
whichever document, and a `$ref` written in another document's content is read in that document.
What a `$ref` lands on in another document lowers where that document declares it; one in the source
lowers at the pointer the `$ref` spells. A use site takes what its schema `$ref`'s chain
ends on — an annotation, a form body's parts, a file body or a discriminator — whichever documents
the chain passes, as its type names that schema. An empty `$ref` names the document's root, which
holds no schema, so a chain reaching one ends on none and lends nothing, as any chain that fails
does, though the schema writing it keeps what it declares beside it. A YAML alias or merge key is a
copy written where it is placed, so what it holds is read at that position, as the document's JSON
places it, and raw YAML a schema `$ref` lands on, under an extension or in another document, is read
as a schema. A schema position of the model holds the schema the parser built there, whatever its
YAML, so a `$ref` to a component written as a string names that component, as it lowers. A key
written twice in one mapping is read as its last occurrence, raw YAML included, as the parser's
model reads one ([`duplicate-key`](../parser-library.md#duplicate-key)).

A reference that resolves to nothing — a pointer naming no node, a position holding no schema, a
component its section does not hold, another document the compile may not read, a plain name or a
URI no schema declares, or two do — is reported as an `openapi/unresolved-ref` error at the
referencing position, with why, and the position lowers without it: a schema position to the top
type, any other position to nothing at all, so the parameter, response or body it would have named
is absent. Each is reported once, where it is written, in whichever document, and a schema `$ref`
whose chain ends on another failing `$ref` is not reported for it, as that one is. A reference
object whose chain ends on a position holding no object of its kind, such as a string or a `null`
under an extension, draws that error alone, and nothing is reported at the position, whichever
reference reaches it first. Its reason is worded as a schema `$ref`'s is (`it names a position that
holds no parameter`), and where the chain passed through another reference it quotes the `$ref`
written at the hop before, naming the document that writes it where that is not the source. What a
reference object resolved to is read at the node the resolver's chain reached
([`trailing-empty-token`](../parser-library.md#trailing-empty-token)), and where no position holds
that node, as for the first of a key written twice in raw YAML
([`duplicate-key`](../parser-library.md#duplicate-key)), it lowers at the use site, and each schema
`$ref` and mapping value in it is reported unresolved, saying so.

A same-document `#/$defs/...` pointer in the document's own resource is read by a rule of its own,
in an order no declaration changes: the position the pointer names from the document's root, raw
YAML included, so a root `$defs` answers though the OpenAPI Object does not define the key
([`unknown-keys`](#unknown-keys) still reports it) and before any schema's own
([`root-defs`](../parser-library.md#root-defs)), else the definitions of the nearest schema of the
model above it holding the path, where raw YAML answers too. That object may be a map of schemas,
such as `properties` or `components.schemas`, whose entry keyed `$defs` holds the path, as the
parser's resolver reads one. A schema's own `$defs` answer only where an `$id` makes it a resource
of its own, which the pointer is then read from as any other is. Raw YAML holds no schema of the
model, so a schema under an extension answers no such pointer from its own `$defs` unless it writes
an `$id` itself, and an `$id` raw YAML writes above it makes it no resource of its own: as OAS 3.1
reads a schema with no `$id`, the document is its base, and its root holds none.

Binding is held to one work budget, which every pass spends, reported as an
`openapi/budget-exceeded` error once it runs out. The pass it runs out in is the last: the `$ref`s
past it are left unresolved, and one it answered keeps that answer, though a later pass might have
changed it. A `<<` merge chain deeper than the levels a mapping's merges are expanded to
(`nodeview.MergeDepthLimit`) is reported as an `openapi/degraded-construct` warning,
and a `$ref` past it is left unresolved, as is one that reads a key past it: a key its pointer
names, an `$id` that may give the base it is read against, or, for a `#/$defs/...` pointer, a
definition the rule reads before the one that would answer. So is a plain name no schema declares
where a schema merging past the bound may; a URI is read as a location all the same, and a
`$dynamicRef` in that document is not expanded ([`dynamic-ref`](#dynamic-ref)). A chain of
references that closes on itself is [`reference-cycles`](#reference-cycles)'.

- Cases: `schema-identifiers`, `component-media-types-32`
- Codes: `openapi/unresolved-ref`, `openapi/budget-exceeded`, `openapi/degraded-construct`,
  `openapi/validation`
- Tests: `e2e.TestCompile_ExternalFileRefIsNotReadByDefault`,
  `e2e.TestOlderDrafts_AnIDNamesAResource`,
  `e2e.TestOlderDrafts_AnIdentifierBesideARefSetsNoBase`,
  `e2e.TestOlderDrafts_AnAnchorTakesItsDraftsGrammar`,
  `bind.TestDialectRule_AFragmentIdentifierDeclaresByItsDialectsGrammar`,
  `bind.TestFragmentName_DeclaresAnyNameDraft4Writes`,
  `load.TestJudgeSource_RefusesTheAnchorsNoReferenceCanName`,
  `bind.TestDialectRule_AnIdentifierBesideARefNamesNothing`,
  `bind.TestDialectRule_AnAnchorTakesItsDialectsGrammar`,
  `bind.TestDialectRule_ReadsWhatADialectSpellsOtherwise`,
  `bind.TestDialectRule_ANestedSchemaNamesItsDialectAtAResourceRoot`,
  `bind.TestDialectRule_AWalkStartsInTheDialectAboveIt`,
  `e2e.TestGolden_SplitSpec`, `e2e.TestExternalContent_ARefInItNamesItsOwnDocument`,
  `e2e.TestCompile_AResolutionFailureIsReportedOnceAtItsRef`,
  `schema.TestMapping_InAnotherDocumentIsReportedWhereItIsWritten`,
  `lowering.TestInDocument_PlacesWhatAnotherDocumentDeclares`,
  `lowering.TestDeclared_LowersAReferencedObjectWhereItIsDeclared`,
  `lowering.TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference`,
  `bind.TestBind_PassesAReferenceObjectOfAnotherDocument`,
  `e2e.TestCompile_ASiblingRefResolvesWithinTheSourcesDirectory`,
  `e2e.TestCompile_ARefOutsideTheSourcesDirectoryIsRefused`,
  `e2e.TestCompile_ASymlinkOutOfTheSourcesDirectoryIsRefused`,
  `e2e.TestCompile_ARefOutsideTheSourcesDirectoryResolvesWhenAllowed`,
  `e2e.TestCompile_ByDefaultOnlyARelativeReferenceIsFollowed`,
  `e2e.TestCompile_ByDefaultEachReferenceIsJudgedByItsOwnSpelling`,
  `e2e.TestCompile_AURLNoReaderReadsIsRefusedUnread`,
  `e2e.TestCompile_ARefAnIDAnswersIsNeverRefusedUnread`,
  `e2e.TestCompile_ARelativeRefUnderAnIDReadsWhereTheIDPlacesIt`,
  `bind.TestBind_ReadsARelativeURIUnderAnIDWhereTheIDPlacesIt`,
  `bind.TestBind_RefusesAReadUnderAnIDAsItsSpellingsWould`,
  `e2e.TestCompile_OneFileUnderTwoNamesIsOneDocument`,
  `e2e.TestExternalContent_ASchemaRefPastARefNamesItsContent`,
  `e2e.TestExternalContent_ASourcePositionItNamesByFileResolvesInEitherOrder`,
  `e2e.TestCompile_SchemaEmptyPointerSegmentIsUnresolved`,
  `load.TestLoad_ARootDefinitionResolvesAsTheResolverReadsIt`,
  `load.TestLoad_ARootDefinitionAnswersBeforeASchemasOwn`,
  `bind.TestBind_ReadsEachHop`, `bind.TestBind_ReadsADefinitionByTheDefsRule`,
  `e2e.TestCompile_DefsPointerUnderANestedIDReadsTheBaseItNames`,
  `e2e.TestExternalContent_ARefLendsItsUseSiteWhatItsTypeReaches`,
  `schema.TestSchema_ARefThroughAnEmptyRefLendsNothing`,
  `bind.TestBind_ReadsEachPositionAsTheJSONItSpells`, `bind.TestBind_ReadsARepeatedKeyAsItsLast`,
  `bind.TestBind_StopsAtItsBudget`, `bind.TestBind_ThePassTheBudgetRunsOutInStands`,
  `bind.TestBind_CountsWorkLinearInTheDocument`,
  `bind.TestBind_CountsWorkLinearAcrossHeldDocuments`,
  `bind.TestBind_ReadsEveryWebhookAsAPathItem`, `bind.TestBind_PlacesAnEndAtTheNodeItsChainReached`,
  `e2e.TestReferenceEnds_AnEndingSlashIsReadWhereTheParserResolvedIt`,
  `e2e.TestReferenceEnds_AnEndInTheSourceIsReadThereUnderSelf`,
  `load.TestBindSchemas_PlacesEveryEndAndBindsEveryRefInTheCorpus`,
  `bind.TestBind_ReadsWhatAnotherDocumentDeclares`, `bind.TestBind_ReadsBelowAnIDOfAnotherDocument`,
  `bind.TestBind_AnIDNamingItsDocumentReturnsToIt`, `bind.TestBind_AnIdentifierIsNoLocationToFetch`,
  `bind.TestBind_AHeldDocumentMayDeclareAnothersURI`,
  `bind.TestBind_ARefusedURIADocumentDeclaresIsAnIdentifier`,
  `bind.TestBind_ALocationADocumentDeclaresIsThatIdentifier`,
  `bind.TestBind_DeclarationOrderReachesNoAnswer`,
  `bind.TestBind_ADocumentNoPassReachesDeclaresNothing`,
  `bind.TestBind_TheSourceDeclaresOnceWhateverThePasses`,
  `bind.TestBind_AReadingNoPassSettlesCountsEveryDocument`,
  `e2e.TestReferenceEnds_AnEndTheBinderCannotPlaceSaysWhy`,
  `bind.TestTable_SaysWhyARefWentUnread`,
  `bind.TestBind_SaysAKeyMayLiePastTheMergeBound`,
  `bind.TestBind_TakesNoDefinitionPastAReadTheMergeBoundStopped`,
  `canary.TestBindingOracle_AgreesWithTheLibrary`,
  `load.TestResolve_AFindingNamesTheDocumentItIsIn`,
  `load.TestResolve_ReachedFindingsDoNotDependOnDeclarationOrder`,
  `e2e.TestCompile_AReferenceObjectToANonObjectIsReportedOnce`,
  `operation.TestMediaTypeRef_OneDeclarationIsOneType`,
  `operation.TestMediaTypeRef_ReferencesInsideTheDeclarationResolve`,
  `operation.TestMediaTypeRef_AnUnresolvedRefLowersNothing`,
  `load.TestLoad_AMediaTypeRefEndsOnTheDeclarationsOwnModel`,
  `load.TestLoad_AMediaTypeRefToNoObjectIsReportedAsOne`,
  `load.TestResolve_AMediaTypeUseTheParserFaultsOnIsReportedOnce`,
  `resolve.TestReferenceEnd_NamesWhereAMediaTypeReferenceEnds`,
  `load.TestResolve_EveryKindOfObjectIsValidated`,
  `e2e.TestCompile_AMediaTypeReferenceLowersAlikeAfterARebuild`,
  `operation.TestMediaTypeRef_ACarrierReadsTheDeclarationInItsDocument`
- Status: partly built: an `encoding` entry naming no part of its body is dropped with no record
  (#604). An absolute-path `$ref` in a remote document is read as a local path (#936); a remote
  document can make the compiler request a private address (#937); and a remote document reaching a
  local file is refused naming only its last such `$ref` (#940). A reference object's pointer
  holding a character above U+FFFF is refused as malformed (#516), and an unresolved reference
  object's message ends in the resolver's own wording, unless its chain ends on a position holding
  no object of its kind (#809). Whether an `$id` spelled as an absolute path names the file a
  relative `$ref` reaches turns on whether the spec was named by an absolute path (#1040), and a
  chain of documents each declaring the URI the one before it asked for costs a binding pass per
  link (#1372). A reference object reaching raw YAML of the source leaves the references inside what
  it reaches unresolved (#1320), and one whose pointer ends in `/` declares what it reaches at a
  second pointer (#1330). A reference object whose `$ref` is a scalar under a tag outside YAML's
  JSON schema ruleset, which the model decodes no string from, lowers nothing, and nothing reports
  it (#1368). A schema `$ref` whose pointer passes through a reference object's alias lowers what
  it reaches a second time, at the pointer it spells (#1359). The parser's resolver takes time cubic
  in the length of a chain of reference objects, each naming the next (#1468).

### `reference-cycles`

A chain of schema `$ref`s that never reaches a schema — `A: {$ref: B}`, `B: {$ref: A}` — is a loop,
found however it closes: through same-document pointers, a `$defs` pointer, a root definition, the
source's own file name, a pointer read on past a reference object, or another document's content. It
is reported once, as an `openapi/cyclic-ref` error at its least member, in the document that writes
it, the source's members first and another document's by its location, so the report does not move
with declaration order. Each member lowers to the top type with no report of its own, and the
document compiles. A YAML anchor recursive in itself would run the parser out of stack, so a source
holding one is refused before the parser reads it, as an `openapi/cyclic-ref` error, and compiles to
no Document; so is an overlay's patched tree. A document an external reference reads that holds one
fails each reference naming it instead, as it fails the source's other refusals
([`source-budgets`](#source-budgets)): each is an `openapi/unresolved-ref` error saying why, and the
rest of the document lowers. A cycle of Reference Objects the resolver reports itself, as an
unresolved reference ([`references`](#references)). A schema reaching itself through what it
declares is recursion, not a cycle ([`recursion`](#recursion)). An alias scan that aborts on a fault
of its own is reported as an `openapi/cycle-scan-failed` warning, and the compile proceeds.

- Codes: `openapi/cyclic-ref`, `openapi/cycle-scan-failed`
- Tests: `e2e.TestCompile_SchemaRefCycleIsReportedAndStillCompiles`,
  `e2e.TestCompile_CycleIsReportedAtItsLeastMemberInEitherOrder`,
  `e2e.TestCompile_ReentrantPrefixReportsAlikeInBothDeclarationOrders`,
  `e2e.TestCompile_AliasCycleIsRefused`, `load.TestLoad_ADegenerateCycleLoadsAndIsReportedOnce`,
  `e2e.TestCompile_ALoopThroughAnotherDocumentIsReportedOnce`,
  `load.TestBindingDiags_NamesALoopsMembersByTheirDocument`,
  `bind.TestBind_ReportsALoopOutsideTheSourceAtItsLeastMember`,
  `bind.TestBind_ReportsALoopOfDefinitionsAtItsLeastMember`,
  `e2e.TestCompile_MergeChainPastBoundStillCompiles`, `bind.TestBind_AnswersAlikeInEitherOrder`
- Status: partly built: a cycle of component Reference Objects is reported as an unresolved
  reference rather than as `openapi/cyclic-ref` (#580), and a reference object on a failing chain is
  reported only where the walk reaches it first (#767).

## 2. The document and what it keeps

### `document-metadata`

`info.title` lowers to `Document.Name` and `info.version` to `Document.Version`, each as written;
`info.contact`, `info.license`, its 3.1 `identifier` included, and `info.termsOfService` to
`Document.Contact`, `Document.License` and `Document.TermsOfService`, each field as written; and
`info.summary`, `info.description` and the root `externalDocs` to the Document's `Docs`
([`docs-and-deprecation`](#docs-and-deprecation)). The document lowers to one `Service`, named by
`info.title` ([`naming`](#naming)).

- Cases: `docs-summary-desc`
- Tests: `document.TestMeta_FullDocumentMetadata`, `openapi.TestCompile_EndToEnd`

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

A construct a rule keeps verbatim becomes an `Unmodeled` entry
([unmodeled](../ir/unmodeled.md)) on the node or carrier that owns its position, or on the Document
where the rule says no node stands for it: keyed `openapi:<keyword>` for a keyword or field, or by
the scoped key the rule names where several objects reach one carrier
([`vendor-extensions`](#vendor-extensions)), under the reason the rule gives, and with a
`Provenance` locating the construct itself rather than the node that carries it.
The entry holds the construct as JSON read off its YAML node as the parser reads it: aliases
followed, `<<` merge keys merged in the parser's order
([`merge-keys`](../parser-library.md#merge-keys)), a mapping key as the text it spells, since the
specification reads every key as a string, and one that is no scalar as the empty string
([`validation`](#validation) reports it), a number in its digits with its YAML base resolved, past
float64's range included, a YAML timestamp in the spelling written, a `!!binary` as its source text,
and a scalar under a tag of the document's own as its text. A field a reader finds is found the same
way, so one supplied through a merge key or an alias is kept as a written one is. A node no JSON can
hold — a key a mapping or a mapping merged into it writes twice, `.nan` or `.inf`, a scalar whose
text its tag does not admit, invalid UTF-8, an alias that cycles or expands past the converter's
node budget, a merge chain deeper than the reader expands — reaches the IR in no form. It is
reported as an `openapi/unpreservable-construct` error at the construct, and the rule that would
have kept it announces nothing as kept, so no diagnostic claims what the Document does not hold. A
vendor extension that converts to no JSON is [`vendor-extensions`](#vendor-extensions)' instead.

- Cases: `yaml-merge-keys`
- Codes: `openapi/unpreservable-construct`
- Tests: `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`,
  `schema.TestCoDeclaredFamily_UnpreservableIsNotAnnounced`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`,
  `annotation.TestRawFromNode_PreservesMergeAndOrderingSemantics`,
  `annotation.TestRawFromNode_NamesTheRepeatedKey`, `annotation.TestScalarRenderers_AgreeOnKind`,
  `annotation.TestRawPropertyNode_ReadsAKeywordAsTheParserDoes`
- Status: partly built: whether a scalar its own tag does not admit is refused or kept as its text
  is open (#245).

### `unmodeled-dialect-fields`

A source revision may add a field the bundled parser's model names no member for; OpenAPI 3.2's
`Response.summary`, XML `nodeType`, the nested Encoding fields, a Media Type Object's `description`,
`components/mediaTypes` and a content entry written as a Reference Object are the live set. Such a
field is read off the raw node by a reader of its own, and the object census is *told* which keys
that reader took: the census grades a key by the parser's model, so left alone it reports a field
the document defines as an undefined key. The reader runs whatever version the document declares, as
[`later-version-construct`](#later-version-construct) requires, so the census never calls such a
field undefined.

The census is told what the reader actually took, never a key list by version. A value the parser
would decode as a string goes to its home — `summary` into `Docs.Summary`, `nodeType` into
`XMLHints.NodeType`, a body's Media Type Object's `description` into `Content.Docs` — and is not
also kept verbatim, because one declaration gets one structural
home ([annotations](../ir/annotations.md#one-structural-home-per-declaration)). A value of
another shape, or a non-empty `nodeType` outside the five the specification defines, is kept
verbatim at the key and pointer the census itself uses, under `degraded_lowering`, with an
`openapi/degraded-construct` warning at the field, and the census finds the entry and adds nothing.
A key the raw mapping does not present directly — merged in through `<<` — is not read at all, so
the census still reports it, as an `openapi/unknown-key-unreachable` warning
([`unknown-keys`](#unknown-keys)). An empty or null value is read as unset. A reading with no IR
home is kept under `no_ir_home`, with an info `openapi/degraded-construct` at the field: the nested
Encoding fields, and a Media Type Object's `description` at a parameter's or header's content
entry, whose carrier has no docs for the one media type it names. The load phase models
`components/mediaTypes` and each content entry written as a `$ref`, which the parser models as
neither a section nor a reference, and the reference is followed as any other is
([`references`](#references)); each entry no reference reaches is kept
([`unreferenced-components`](#unreferenced-components)).

- Cases: `response-summary-32`, `xml-nodetype-32`, `nested-encoding-32`, `component-media-types-32`
- Codes: `openapi/degraded-construct`, `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryNoReaderTakesIsStillReported`,
  `annotation.TestRead_NodeTypeOnlyADefinedValueIsPlaced`,
  `operation.TestContent_MediaTypeDescriptionReachesItsDocs`,
  `document.TestComponentMediaTypes_AnUnreferencedEntryIsKept`

### `unreferenced-components`

A compiler lowers a component where a reference finds it, so an entry no reference reaches lowers
nowhere — no node, no `Unmodeled` entry, no diagnostic — and a declaration the document makes
disappears in silence. Every component section with no registry is therefore kept: each entry no
lowering reached is preserved verbatim at
`Document.Unmodeled["openapi:components/<section>/<name>"]` under `no_ir_home`, at the entry's own
pointer, with no diagnostic. The sections are `responses`, `parameters`, `examples`,
`requestBodies`, `headers`, `links`, `callbacks`, `pathItems` and 3.2's `mediaTypes`, which is read
at every version, as [`unmodeled-dialect-fields`](#unmodeled-dialect-fields) reads a field the
parser does not model; `components/schemas` and `components/securitySchemes` are lowered
unconditionally. A section is read as the parser reads it, through `<<` merge keys, so a merged-in
entry is kept under its own name, and an entry whose key is no scalar under the empty name the
parser reads it by ([`validation`](#validation) reports the key). Where the components merge
through `<<` deeper than the levels a mapping's merges are expanded to (`nodeview.MergeDepthLimit`),
an `openapi/degraded-construct` warning at `components` says so, and an entry past that depth is not
kept.

*Referenced* means a lowering followed a reference to the entry, not that some `$ref` string names
it. The resolution every non-schema reference passes through records each position in the source its
chain lands on, and the retention runs after every other lowering and keeps what that record lacks.
A `$ref` written where nothing follows it — under an `x-` extension, inside an example's literal
value, inside an entry that is itself not lowered — carries nothing of its target into the IR, so
the target is kept; and a reference spelled another way than
its target's key, percent-encoded or naming this document by its file, still reaches the entry it
resolves to, so that entry is not kept beside its lowering. Deciding this from the `$ref` strings
instead would answer both questions by a reading of its own, and wrongly: it drops the first kind of
target in silence and gives the second two homes. The record is written by lowerings that never read
it, so it cannot change their answers or depend on their order.

`no_ir_home` rather than a boundary, and no diagnostic, are the links precedent generalized: the gap
is one the IR can close by finding a use for the entry, so a promotion pass should read it, and a
document that declares a component is not making a mistake a warning could name. `Reason` is where
that decision lives ([unmodeled](../ir/unmodeled.md#reasons)), which is what keeps a
promotion pass from mistaking one for an oversight.

- Cases: `unreferenced-components`, `component-media-types-32`
- Codes: `openapi/degraded-construct`
- Tests: `document.TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget`,
  `document.TestUnreferencedComponents_ReadAsTheParserReadsThem`,
  `document.TestUnreferencedComponents_SelfReferenceByFileNameReaches`,
  `document.TestUnreferencedComponents_MergeDepthBoundIsReported`,
  `document.TestComponentMediaTypes_AnUnreferencedEntryIsKept`

## 3. Names and values

### `naming`

A name the document spells lowers to `Naming{Source, Canonical}`: the spelling as written, beside
the word sequence [naming](../ir/naming.md) derives from it, at every position that spells one —
`info.title`, a tag, an `operationId`, a parameter, response-header or property name, a component
schema or security scheme key, a scheme a security requirement names, an enum member's value as
text, a 3.2 server `name`, and under `grouping=path-prefix` a first path segment. A name with no
letter or digit (`***`, `>=`) keeps its `Source` and has no `Canonical`; an emitter names it by its
characters ([naming](../emitters/refine/naming.md)). A name spelled as the empty string is minted
`Hint: "empty"` and no `Source` rather than passed through (an `operationId` of `""` is read as
absent instead, [`operation-ids`](#operation-ids)), and a property's wire name stays `""`. A
component schema keyed `""` earns no named `TypeID`: it hoists anonymously at its own pointer
(`t/anon/components/schemas/`) with that hint. An entity the document gives no name — an inline
schema, a union variant, a response ([`response-conditions`](#response-conditions)), a server with
no `name` (its URL's words, else `server`), the synthesized `default` and `webhooks` groups —
carries a `Hint` alone, in the same word grammar and composed from its position, each half
neutralized first so an empty parent gives `empty_item`, never `_item`. A request body or header
that references reach is named for its declaration, never for whichever reference lowered first: a
component's name, a header's own key, the `operationId` of the operation declaring the body, in this
document or another and whether or not that operation is lowered, or `request` for a body no
operation with an `operationId` declares. A schema is named for the nearest object holding it, so
one a parameter, request body or response holds is named for that object even where the object is
kept under an extension and read through a `$ref`, and a document a `$ref` reads whole as a schema
is named for the document (`./Cat.yaml` names its root `cat_yaml`). Every such name is a function of
the documents and the position alone, read where the position is written, so no lowering order
changes it: a union variant, or a composition branch holding a `$ref`, is named as the position the
`$ref` lands on, in whichever document, and keeps its own name where that names nothing. An inline
`allOf` branch is a conjunct of the schema holding it, so a node built there
([`allof-refinement`](#allof-refinement)) is named as that schema is. `Aliases` is never set. These
names are what an emitter renders, so a change to how one is derived moves `IRVersion`
([ir-version](../ir/ir-version.md)), and `testdata/golden/openapi/hint_positions.yaml` writes the
positions within one document that a hint is composed from, one for each way the naming composes
one.

- Cases: `neutral-naming`, `empty-names`
- Tests: `ids.TestComponentSchema_ClassifiesEveryPointer`,
  `schema.TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs`,
  `schema.TestInlinePosition_HintIsTheSameInBothOrders`,
  `operation.TestContent_ASharedBodyOrHeaderIsNamedForItsDeclaration`,
  `schema.TestComponentBody_SubtreeIsNamedByItsDeclaration`,
  `schema.TestUnionVariant_UnderPathsIsNamedAsTheNodeItHolds`,
  `schema.TestUnionVariant_AWholeDocumentIsNamedForIt`, `naming.TestAt_NamesASchemaAfterItsOwner`,
  `naming.TestAt_NamesABranchAfterItsTarget`, `naming.TestAt_NamesAStructureAReferenceReadsInsideRawYAML`,
  `e2e.TestGolden_HintPositions`
- Status: partly built: union variants and inline schemas ignore their `title` as a hint (#662).

### `values`

A default, `const`, enum member or example lowers to an `ir.Value` read off the YAML node by its
resolved tag alone, as the parser resolves it
([`scalar-resolution`](../parser-library.md#scalar-resolution)) but for a date written with no tag,
which is a string ([`yaml-decoding`](#yaml-decoding)), never by the type the schema
declares ([values](../ir/values.md)): `null`, `bool`, `string`, `list` and `object` (members as the
parser reads them, `<<` merge keys merged in its order
([`merge-keys`](../parser-library.md#merge-keys)), each keyed by its text and a key that is no
scalar by the empty string, an alias followed), a `!!binary` to `bytes`, decoded, and a YAML
timestamp to `string` in the spelling written (`2021-1-1` stays `2021-1-1`, never RFC 3339). A
number is the literal's exact decimal text in `Num`, never a `float64`: a magnitude past float64 or
int64 keeps every digit, `.5`, `+5` and `09` take their JSON spellings (`0.5`, `5`, `9`), `_`
separators drop, a YAML integer base resolves (`0644` is `420`, `0x1f` is `31`, `0b1010` is `10`),
and digits, exponent and trailing zeros otherwise stand as written (`10.0`, `1.10e+3`). A plain
scalar that YAML resolves to a string only because it is past float64's range is still a number when
it is a decimal literal a `BigVal` holds (its exponent within `ir.MaxDecimalExp`), and a quoted one
stays a string. A value no `Value` holds — `.inf` or `.nan`, a `!!binary` that is not base64, a
scalar whose text its tag does not admit (`!!null abc`, `!!timestamp soon`), a tag the compiler does
not lower (`!acme`), a key a mapping or a mapping merged into it writes twice, a merge chain deeper
than the reader expands, nesting past the reader's depth bound — is not coerced: a default or
example is dropped with an `openapi/degraded-construct` warning at its position, and a `const` or an
enum member is [`enum-const`](#enum-const)'s. The number reader is the numeric bounds' as well
([`constraints`](#constraints)). A value kept verbatim instead is
[`verbatim-preservation`](#verbatim-preservation)'s.

- Cases: `defaults`, `yaml-timestamp-scalars`, `numeric-precision`, `yaml-merge-keys`
- Codes: `openapi/degraded-construct`
- Tests: `value.TestNumericLiteral_YAMLBases`, `value.TestValueFromNode_Timestamp`,
  `value.TestValueFromNode_Scalars`, `value.TestValueFromNode_ReadsAMappingAsTheParserDoes`,
  `value.TestValueFromNode_RefusesWhatNoObjectHolds`, `value.TestClassify_ReadsEachSpellingOnce`,
  `annotation.TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped`
- Status: partly built: an unconvertible default is reported at the schema of the property or
  parameter that reads it rather than at its own position (#1101); and a plain number past
  `ir.MaxDecimalExp` lowers as the string YAML reads, unreported (#1115).

## 4. Schemas: identity, references and annotations

### `schema-registry`

Each entry of `components/schemas` lowers to a node of its own in the type registry, whether or not
anything references it, under a `TypeID` derived from its pointer
(`t/openapi/components/schemas/<name>`, [identity](../ir/identity.md)) and named by its key
([`naming`](#naming)). One whose type is a primitive (`{type: string}`) lowers to a `Scalar` over
that primitive, and one that is only a `$ref` lowers to a `Scalar` alias whose `Base` is its target
([scalars](../ir/scalars.md)). A reference to the entry names that ID.

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
in the type graph by ID ([design rules](../ir/README.md#design-rules)), whichever declaration
lowers first, with no diagnostic. A chain of `$ref`s that never reaches a node without one
(`A: {$ref: B}`, `B: {$ref: A}`) is not recursion but a cycle with no schema in it, which
[`reference-cycles`](#reference-cycles) reports.

- Cases: `recursive`
- Tests: `schema.TestLower_RecursiveSchemaTerminates`, `scan.TestAliases_LegalRecursionClean`,
  `e2e.TestCompile_SchemaRefCycleIsReportedAndStillCompiles`
- Status: partly built: an `allOf` that names its own schema, or two schemas that are each other's
  `Base`, lowers to a base chain that returns to where it started with no diagnostic, and a cycle of
  required properties that admits no finite value is reported by nothing (#642).

### `ref-use-site`

The keywords written beside a schema's `$ref`, which 3.1 admits and which are applied the same way in
a 3.0 document, each read in its own document's dialect
([`later-version-construct`](#later-version-construct)), and the annotations its target
declares — documentation, deprecation, redaction, visibility and `default` — merge onto the property,
parameter or header that references the target, field by field, a keyword the use site writes
winning over the target's ([annotations](../ir/annotations.md#what-a-reference-carries-down)), and by
the same rule at every carrier. Redaction has nothing to win with: `format: password` on either side
asks for it, and no keyword beside the `$ref` withdraws the target's. An extension the promotion
policy maps is read the same way ([`extension-promotion`](#extension-promotion)). At a position that
carries no property, parameter or header — an `allOf`, `oneOf` or `anyOf` branch, an `items`, a
component — the keywords beside the `$ref` bind an alias hoisted at that position instead
([models](../ir/models.md#composition-entries)). Constraints are the exception: bounds conjoin rather
than override, so each position keeps the ones it declared and none is copied to a use site
([annotations](../ir/annotations.md#what-a-reference-carries-down)).

A refinement ([`allof-refinement`](#allof-refinement)) is a use of what it names, so a carrier holding
one reads its target by this rule, and one holding a `$ref` to a refinement reads through it, up to 64
`$ref`s: what the target declares, what its inline branches lend it
([`allof-inline-residue`](#allof-inline-residue)), then what its own `$ref` declares, nearest first. A
`$ref` that loses the type to a value set narrowing it still speaks, for documentation, deprecation,
visibility and redaction, on the carrier or else on the alias keeping it, and so does one an `enum`
or a `const` beside its `allOf` passes over ([`competing-keywords`](#competing-keywords)), on the node
that lowers to; its examples stay on its node, as any target's do
([`schema-examples`](#schema-examples)). A `default` a `$ref` lends across a conjunct admitting less
than it does counts only where the position takes it, and with no such conjunct it reads as beside
the `$ref`, whatever the target's own type says of it. A `default` written with the value set is the
position's own, in the set or not, as a plain `enum`'s is. A composition of a sole `$ref` holds what
it composes as `Base` and `Mixins`, which lends a carrier nothing, where a conjunct writes what only a
`Model` holds, a branch writes a `$dynamicRef`, or points 2 to 4 of
[`allof-refinement`](#allof-refinement) keep it composing. Any other, such as a decorator
([annotations](../ir/annotations.md#decorators)) or a bound beside the `$ref`, lends a carrier what
the `$ref` would lend written alone, since the visibility and `default` of the use have no other home.

- Cases: `param-ref-inheritance`, `allof-ref-branch-siblings`, `readonly-writeonly`,
  `schema-ref-siblings-30`, `allof-refinement`
- Tests: `e2e.TestAnnotationRetention`, `schema.TestModel_RefSiblingDescriptionWins`,
  `schema.TestSchemaExamples_ComponentRefSiblingKeepsThem`,
  `schema.TestRefinement_ACarrierReadsThroughARefinement`,
  `schema.TestUse_ADefaultBesideItsValueSetStands`,
  `schema.TestUse_ADefaultNoNarrowingExcludesReadsAsBesideARef`,
  `schema.TestUse_ACarrierReadsThroughADecorator`,
  `schema.TestUse_ALoopOfNarrowingEnumsEnds`
- Status: partly built: a use reads the end of its chain and not the schema it names (#1029), and a
  `deprecated: false` on an `allOf` decorator over an object reaches no carrier, where beside a `$ref`
  it decides (#1258).

### `reference-object-siblings`

A Reference Object's own siblings are the annotations of the use site. OpenAPI lets the entry that
writes a `$ref` carry `summary` and `description` beside it, and the pair describes *that use* of
the referenced object rather than the object. Where the IR has a docs field at the position — a
parameter, a response or error case, a header, an example, a request body, a security scheme, a
body's media type, a link — both are read off the entry and folded over the declaration's docs field
by field, a sibling the entry writes winning and one it omits leaving the declaration's value
standing.
A parameter's or header's content entry has no docs field of its own, so what the fold takes there
is kept verbatim under `no_ir_home`, each from the position writing it, with an info
`openapi/degraded-construct`; one an alias of the chain writes in another document, which this
compiler does not place, is reported with an `openapi/degraded-construct` warning instead. A component that is itself
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

- Cases: `ref-site-docs`, `later-version-constructs`, `component-media-types-32`
- Codes: `openapi/later-version-construct`, `openapi/degraded-construct`
- Tests: `operation.TestMediaTypeRef_UseSiteDocsFoldAlongTheChain`,
  `operation.TestMediaTypeRef_DocsAnotherDocumentsAliasWritesAreReported`,
  `operation.TestContent_MediaTypeRefIsFollowed`,
  `operation.TestMediaTypeRef_ABodyAndAParameterFoldAlike`,
  `operation.TestLinks_UseSiteDocsFoldOverTheDeclaration`
- Status: partly built: a key beside a `$ref` other than `summary` and `description` is dropped with
  no record (#1321). At a parameter's or header's content entry, a `summary` the media type's
  declaration writes, which a Media Type Object does not define, loses its entry to the one the fold
  takes, and is reported as represented in no form (#396).

### `use-bound-annotations`

`readOnly` and `writeOnly` lower to the `Visibility` of the property or header that uses the schema
— `readOnly` to `Only: [read, delete, query]`, `writeOnly` to `Only: [create, update]`
([properties](../ir/properties.md#visibility)) — and a schema's `default` to the `Default` of
the property, parameter or header that uses it, never to the type: both bind a *use* of the type,
not the type. One a declaration writes, on a component or any other position that owns a node of its
own, is therefore pushed down to each property, parameter or header referencing it that has a field
for it, with use-site precedence ([`ref-use-site`](#ref-use-site)), and the declaration keeps its
own copy verbatim under `no_ir_home`, reported with an info `openapi/degraded-construct` at the
keyword. That copy is what a component nothing references would otherwise lose in silence. A
position a property, parameter or header carries keeps them on its carrier whichever reaches it
first, the carrier or a `$ref` naming the position: a `$ref` leaves lowering a position to the
declaration that owns it.

- Cases: `defaults`, `readonly-writeonly`, `inline-residue`
- Codes: `openapi/degraded-construct`
- Tests: `e2e.TestAnnotationRetention_OtherDeclarationShapes`,
  `schema.TestPropertyPosition_ResidueStaysOutOfTheTypeNode`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`

### `docs-and-deprecation`

`summary`, `description` and `externalDocs` lower to `Docs`, and `deprecated: true` to
`Deprecation{}`, on the entity whose object writes them. `Deprecation` is an empty marker: the
keyword states no message, version or date, which only [`extension-promotion`](#extension-promotion)
can supply, and which can state the deprecation itself where no keyword does. An operation, and a
tag on its `TagDef` and on the group it names, take all three; a schema's `title` is the `Summary`,
beside its `description` and `externalDocs`; a response and an error case take `description` and
`summary` ([`response-summary`](#response-summary)); a request
body, a security scheme, a server and a server variable take `description`, an example `summary` and
`description`, and a security scheme `deprecated` (3.2). `info` and the root `externalDocs` make the
Document's one `Docs`, while the Service takes `info.description` alone. A schema's documentation
lands on the node its position owns; where the position owns none (a shared primitive, a `$ref`), it
lands on the property, parameter or header that carries it, the referent's under the use site's
field by field ([`ref-use-site`](#ref-use-site)). A parameter's or header's own `description` wins
over its schema's. `deprecated: true` on a parameter or header and on its schema each deprecate it,
neither one's `false` clearing the other. A request body's `description` lands on its Payload
([`request-body`](#request-body)), and a response's payload states no docs of its own. OpenAPI has
no syntax for naming a type in prose, so a `description` is kept as written and no span of it is
marked ([references in prose](../ir/annotations.md#references-in-prose)).

- Cases: `docs-summary-desc`, `deprecation`
- Tests: `operation.TestOperation_DeprecatedIsCarried`,
  `operation.TestHeaders_DeprecationUnionsWithTheSchema`,
  `operation.TestContent_RequestBodyDocsReachThePayload`,
  `e2e.TestDocs_TextShapedLikeAReferenceStaysText`
- Status: partly built: a deprecation cannot name its replacement, which the IR has no field for
  (#670).

### `disjoint-visibility`

A field both `readOnly` and `writeOnly` — on one schema, across a `$ref` whose site sets one flag
and referent the other, or on inline `allOf` branches that redeclare it — admits no lifecycle. It
lowers as declared, to `Visibility{None: true}`
([properties](../ir/properties.md#visibility)), and is reported once, as an
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
  `e2e.TestAllOfVisibilityMerge_DisjointRestrictionsAreInvisibleNotAConflict`,
  `merge.TestFold_AlreadyInvisibleVisibilityIsNotReAnnounced`

### `nullable`

Whether a position admits null is a question about its whole schema, since JSON Schema conjoins
keywords, and the answer lowers to `TypeRef.Nullable` on the reference that names the position's
type — a property's, a parameter's, a list's element, a union variant — never to a node of its own
([type-graph](../ir/type-graph.md#type-references)). A schema admits null where its `type`
names `null`, and only where the keywords beside agree: an `enum` admits null only where it lists
`null`, which then names no member, so a non-empty `enum` listing `null` admits it with no `type`
beside it, and one not listing it takes a `type: [string, "null"]` back out; an `allOf` admits null
only where every conjunct does, the null sitting on the reference that names the composition rather
than on its `Base` or a `Mixin`; a `$ref` admits it only where the keywords beside it and what it
lands on both do, read one hop at a time so that an alias's own keywords count; and a union's
variant that admits null is marked on that variant's reference. Whether a property must be present
is `Property.Required`, apart from this ([properties](../ir/properties.md)), so required and
nullable make four distinct lowerings. A union of one branch and `null` collapses to a nullable
reference ([`union-null-variant`](#union-null-variant)).

OpenAPI 3.0's `nullable: true` adds `null` to the `type` written in the same Schema Object, as
`type: [T, "null"]` does from 3.1, and then conjoins as that does: 3.0.3 and 3.0.4 state it, as a
clarification of every 3.0 release. With no `type` beside it, it has no effect, and 3.1 removed the
keyword. Documents written to 3.0.0's looser wording, or carried to 3.1, still
use it to say that a schema admits null, and write `{$ref: T, type: [object, "null"]}` for the
nullable reference that `{$ref: T, nullable: true}` was meant to be. So the nullable-widening
policy, on by default, reads such a null as widening what it is written on rather than conjoining
with it: `nullable: true` admits null for the whole schema it is written on, whatever else that
schema says, and keywords beside a `$ref` that admit null by themselves admit it for the whole
reference, and a `nullable: true` on an inline `allOf` branch writing no `properties` admits it for
the whole schema holding the `allOf`, a `type` beside the `allOf` included, as beside the `allOf` it
does ([`allof-inline-residue`](#allof-inline-residue)). A `type` without `null` beside a `$ref` still
narrows what it lands on, and a `type: [T, "null"]` on a conjunct of an `allOf` conjoins with the
rest. A reference whose null only that reading admits names `nullable-widening` in
`TypeRef.Inferred` (invariant 6), so `Nullable` with no marker is the declared reading, and each
such null is reported once as an info `openapi/inferred-nullable`, at the `nullable` keyword or, for
keywords beside a `$ref`, at the schema that writes them, saying why its version does not admit it,
whatever references its schema. The policy is the Go option `Options.NullableWidening`, whose
`Disabled` reads the keywords only as their version defines them, and the text option
`nullable-widening`, which `false` turns off. A `nullable` in a 3.1 or 3.2 schema is then a keyword
its dialect does not define, kept verbatim under `openapi:nullable` (`out_of_scope`) with an info
`openapi/unknown-schema-keyword`, where [`unknown-keys`](#unknown-keys) keeps a keyword the schema
model does not name.

- Cases: `nullable-30`, `nullable-31-ref`, `nullable-enum-31`, `nullability-four-states`,
  `nullability-conjunction`, `nullable-keyword-30`, `nullable-keyword-31`
- Codes: `openapi/inferred-nullable`, `openapi/unknown-schema-keyword`
- Tests: `schema.TestSchemaRef_NullableNormalization`,
  `schema.TestNullableWidening_EachShapeReadsByItsVersion`,
  `e2e.TestNullableWidening_AKeywordIsReportedOnceAtItself`,
  `e2e.TestNullableWidening_DisabledKeepsWhatTheDialectDoesNotDefine`,
  `e2e.TestSchemaRefSiblings_ReadInTheDeclaredDialect`,
  `e2e.TestNullableWidening_ACompositionKeepsTheMarker`,
  `merge.TestIntersectNull_TheMarkerFollowsTheBit`,
  `schema.TestNullableWidening_ABranchWidensTheAllOfHoldingIt`
- Status: partly built: a 3.0 `nullable` with no `type` beside it, which has no effect, draws no
  report once the policy is off (#1070).

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
on the property that carries it, the property's hints its own beside its type's
([constraints](../ir/constraints.md)): `name`, `namespace`, `prefix`, `attribute` and `wrapped`
as written, a declared `false` included, and 3.2's `nodeType`
([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)). An `attribute` flag that a `nodeType`
beside it contradicts, which 3.2 forbids, is kept verbatim under `openapi:xml/attribute`
(`degraded_lowering`) with an `openapi/degraded-construct` warning at it, and the `nodeType` is
placed alone. The object's `x-*` extensions are kept as `openapi:xml/x-*`
([`vendor-extensions`](#vendor-extensions)), and the hints of a parameter's schema, which
`ir.Parameter` has no field for, are [`parameter-schema-keywords`](#parameter-schema-keywords)'.

- Cases: `xml-hints`, `xml-nodetype-32`
- Codes: `openapi/degraded-construct`
- Tests: `annotation.TestXMLHints_ReadsEveryFieldDirectly`, `schema.TestModel_PropertyXML`

## 5. Schemas: composition, unions and enumerations

### `allof-classification`

An `allOf` a conjunct can make an object lowers to the composing `Model`'s `Base` and `Mixins`
([models](../ir/models.md#composition)); one no conjunct can is
[`allof-refinement`](#allof-refinement)'s. An entry that is a `$ref` to a schema taking part in
a discriminator hierarchy, or the sole `$ref` entry, becomes `Base`; every other `$ref` entry
becomes a `Mixin`; and an inline entry merges into the composing model's own `Properties`, each
property keeping its own provenance ([`allof-inline-residue`](#allof-inline-residue),
[`allof-redeclared-property`](#allof-redeclared-property)). So every `$ref` entry stays composed
and a declaration a fold drops stays kept, while what a declaration folded into a field adds lands
on the field without its source. A `$ref` entry naming a union is no exception: the union is the
`Base` or a `Mixin`, never distributed over its variants as one written beside the `allOf` is
([models](../ir/models.md#a-union-a-composition-names)).

`allOf` is associative, so an inline entry writing a non-empty `allOf` is flattened into the
enclosing list before the entries are classified: its own entries take its place, in source order
and depth first, each keeping the pointer it was written at, and its `properties` and `required`
merge after them, as a composing schema's own merge after its entries'. So `allOf: [{allOf: [A,
B]}]` classifies as `allOf: [A, B]`, and nothing is reported. Whatever else the entry writes that
the merge does not consume is kept beside the composition as residue
([`allof-inline-residue`](#allof-inline-residue)), so `{additionalProperties: false, allOf: [A, B]}`
composes `A` and `B` and is also kept whole. Flattening stops 32 levels down: an entry nested inside
32 flattened entries is not flattened itself, so its `allOf` is residue, and it is reported as nested
past the flattening depth of 32. This is not the flattening the
[design rules](../ir/README.md#design-rules) leave to emitters: every `$ref` entry still composes as
a `Base` or `Mixin`, and all that goes is a grouping no instance can observe, which an inline entry
owns no node to keep ([`allof-inline-residue`](#allof-inline-residue)).

- Cases: `allof-inheritance`, `allof-mixins`, `allof-inline-merge`, `allof-nested-inline`
- Tests: `schema.TestAllOf_DiscriminatorHierarchy`, `schema.TestAllOf_SoleRefBecomesBase`,
  `schema.TestAllOf_NestedInlineWrapperFlattens`, `schema.TestAllOf_NestedWrapperBaseSelection`,
  `schema.TestAllOfBranches_DepthCapKeepsWrapperVerbatim`,
  `schema.TestAllOfBranches_DepthCapNamesWhatElseTheWrapperWrites`,
  `schema.TestAllOf_NestedWrapperWithResidueKeptVerbatim`, `schema.TestAllOf_NestedLowersAsFlat`,
  `schema.TestAllOf_AUnionItNamesComposesAsWritten`
- Status: partly built: a `$ref` to a subtype of a discriminated base, which declares no
  discriminator itself, is composed as a `Mixin` rather than the `Base` (#986).

### `allof-refinement`

An `allOf` is a composition where a conjunct writes `properties`, `patternProperties`,
`additionalProperties`, `required` or `discriminator`, which only a `Model` holds, or where the
conjuncts together admit an object and one of them is a `$ref` or names a kind, the keywords beside
the `allOf` being a conjunct as much as a branch is. A `Model` anywhere else asserts an object the
source rules out, so that `{allOf: [{type: integer}, {type: integer, format: int32}]}` reaches a
consumer as an object with no fields, where it means `int32`. Any other `allOf` is a *refinement*:
one use of the conjunct that decides it, its *principal*, with what the other conjuncts say read as
said of that use. So `{allOf: [{$ref: T}, {type: string}]}` refines a `T` that may be a string or an
object, as the two together admit no object.

What the conjuncts admit is read from the source and the bindings, never the registry, so no
lowering order decides it. Each conjunct's JSON kinds and value set are read across `$ref`, `allOf`,
`oneOf`, `anyOf`, `type`, `enum` and `const`, a `properties` with no `type` beside it claiming an
object, values comparing as an `enum`'s entries do, so that `1` and `1.0` are one, and the read takes
in at most 256 schemas and 4096 `enum` members. An `allOf` stays the composition it would otherwise
be, with nothing lost, where:

1. the read does not finish: a cycle, either bound, a `$ref` landing on no schema, a `$dynamicRef`,
   or a value it cannot convert;
2. it writes more than 64 conjuncts, which the election below compares pairwise;
3. a conjunct is `false` ([`allof-false-branch`](#allof-false-branch)), or nests past the flattening
   depth ([`allof-classification`](#allof-classification));
4. `oneOf` or `anyOf` is written beside the `allOf`
   ([`union-beside-structure`](#union-beside-structure));
5. two conjuncts are `$ref`s: both keep composing, since keeping one reference typed would mean
   dropping the other ([models](../ir/models.md#composition-entries));
6. the conjuncts share no value, a null they all admit being no shared value unless a conjunct
   admits nothing else. A warning says so at the schema where their instance types share none
   ([`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts)), and otherwise at a branch writing a
   `type` that excludes an object ([`allof-inline-residue`](#allof-inline-residue)). Where neither
   holds, as for two `enum`s sharing no member, each branch is kept with an info and no report
   names the contradiction.

An `enum` or a `const` beside an `allOf` that refines is one of its conjuncts, the keywords beside
the `allOf` writing a value set, rather than its rival in the election of
[`competing-keywords`](#competing-keywords): `{enum: [a], allOf: [{$ref: Level}]}` elects its
principal from the same set as `{allOf: [{$ref: Level}, {enum: [a]}]}`, its branches fold and its
`$ref` still lowers. Beside an `allOf` that composes, the value set wins that election and the
`allOf` stays verbatim.

The principal is chosen from the whole set:

1. an inline value set, `enum` or `const`, within every other conjunct and more than a restatement of
   what a `$ref` fixes: it says which values the position holds, and what it narrows cannot;
2. else the `$ref`, since a narrower inline `type` replacing it would drop what its target says;
3. else, of the conjuncts naming a kind by `type`, `enum` or `const`, the one within the most others,
   the keywords beside the `allOf` among them, those first and then source order on a tie. A conjunct
   is within another where its kinds, its values, its `format` and its `items` are. Two formats of the
   table ([`string-formats`](#string-formats), [`numeric-formats`](#numeric-formats)) compare by what
   they select, a format narrows the bare kind it applies to, one the table does not list included,
   `items` narrow an `array` writing none, and two `items` compare by what each says, so `uuid` beats
   `string`, `email` beats `string` and a list of `uuid` beats a list of `string`, in either order,
   as a field redeclared the same ways does
   ([`allof-redeclared-property`](#allof-redeclared-property)). A tie, such as two formats neither of
   which narrows the other, is the one place order decides, and the loser is kept and reported, again
   as such a field is.

A `format` or `items` written with no `type` qualifies a kind and names none, so it decides nothing:
`{allOf: [{type: integer}, {format: int32}]}` lowers to `integer` with the second branch kept, in
either order, since no one conjunct says `int32` and a schema built from two would stand at a pointer
naming neither. An `allOf` no conjunct names a kind in, `{allOf: [{description: d}]}`, has no
principal and is the top type. Where the keywords beside the `allOf` win, the position lowers by them
as if no `allOf` were written. A principal lowers at the pointer it is written at, so nothing is
minted and each node stands at a pointer naming its schema: a `$ref` as any `$ref` branch does, what
is written beside it on the alias [models](../ir/models.md#composition-entries) describes, and an
inline branch as a schema carried by the position. A branch that reduces to a shared node reads as
written beside the `allOf`, and one owning a node keeps there what it writes, a parameter keeping its
`readOnly` and `writeOnly` as it keeps its schema's.

The value set a refinement lowers to, the `Enum` or `Literal` of a principal or of the keywords
beside the `allOf`, is typed by every conjunct. Each selects a primitive as
[`enum-const`](#enum-const) reads one schema, from its `type` and `format`, else its `type`, where
every value is of it, a `$ref` conjunct by the schema deciding what it lands on, past a chain of
`$ref`s and refinements. The `ValueType` is the one of these, the value set's own included, that
narrows every other, and each `type` and `format` it holds is consumed, a wider one included. So
`{allOf: [{type: integer}, {const: 80.0}]}` is an `integer`, as `{type: integer, const: 80.0}` is,
and keeps nothing. Where none narrows every other, as of two formats neither of which narrows the
other, the value set keeps the `ValueType` its own schema gives it, and a format that reaches none
stays with its branch.

What the conjuncts say lands where the position keeps what it writes: on the carrier
([`ref-use-site`](#ref-use-site)), else on an alias hoisted at the position only if it writes
something ([annotations](../ir/annotations.md#one-structural-home-per-declaration)), so a position
that says nothing beyond its `$ref` is that `$ref`, with no node and no diagnostic. An inline branch's
keywords fold as [`allof-inline-residue`](#allof-inline-residue) says. A `type`, a `format`, an `enum`,
a `const` or `items`, on a branch or beside the `allOf`, that the principal already says is consumed,
so `{allOf: [{$ref: Port}], type: integer}` keeps nothing. Two schemas written alike in one document
say the same where every relative `$ref` in them reads against one base: under an `$id` naming
another URI, the same spelling may name another schema, while an absolute one names the same
wherever it is read. A copy in another document is read apart. One saying
more is kept: a branch whole under `openapi:allOf/<n>` (`degraded_lowering`), a keyword beside the
`allOf` under `openapi:<keyword>`, each reported as an info `openapi/degraded-construct`, at the
branch or at the position. A `$ref` a value set beats is kept the same way, and its target still
speaks for the position ([`ref-use-site`](#ref-use-site)), its `default` only where the value set
takes it, and still lowers, though nothing else names it, as a position no component holds lowers
for a `$ref`. A `oneOf` or `anyOf` written on a branch is a conjunct admitting what any of its
branches admits, naming the kinds they name, so it may be the principal, lowering at its own pointer
as an inline branch does. The principal says the union where one of its branches says nothing the
principal does not, each keyword the branch writes compared as one written beside the `allOf` is,
and a `oneOf` only where that branch also shares no kind with another, so that exactly one admits
each value the position holds:
`{allOf: [{anyOf: [{type: string}, {type: number}]}, {type: string, maxLength: 1000}]}` lowers as
`{type: string, maxLength: 1000}` does, keeping nothing. A branch writing a bound, a `pattern` or an
annotation says more than the principal, so
`{allOf: [{anyOf: [{type: string, maxLength: 5}, {type: integer}]}, {type: string, maxLength: 1000}]}`
keeps the union with its branch.

Over a target that admits no object, `{allOf: [{$ref: T}], K}` lowers as 3.1's `{$ref: T, K}` where
`K` is annotations and value bounds. The two part on a structural keyword, which a `$ref` site keeps
verbatim reading no target ([`shape-applicator`](#shape-applicator)), and on `type: [T, "null"]`,
which the nullable-widening policy reads as widening beside a `$ref` and which conjoins here, as JSON
Schema says ([`nullable`](#nullable)). A `$ref` naming either parts as well: it reads what `K` says
here, and past it there, as a chain of `$ref`s is read to its end ([`ref-use-site`](#ref-use-site)).

The IR's rule that compilers leave flattening to emitters
([design rules](../ir/README.md#design-rules)) is not what this departs from, and the alternative of
keeping such an `allOf` a decorator for emitters to collapse is weighed in
[models](../ir/models.md#an-allof-no-object-satisfies).

- Cases: `allof-refinement`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestRefinement_AnAnnotationOnlyBranchLandsTyped`,
  `schema.TestRefinement_PrimitiveBranchesLowerToTheirMeet`,
  `schema.TestRefinement_DisjointConjunctsStayAComposition`,
  `schema.TestRefinement_NamesItsTargetAtEveryPosition`,
  `schema.TestRefinement_KeywordsLandAtEveryPosition`,
  `schema.TestRefinement_KeptKeywordsLandAtEveryPosition`,
  `schema.TestRefinement_ASecondRefStaysAComposition`,
  `schema.TestRefinement_ReachesNoFurtherThanItCanRead`,
  `schema.TestRefinement_NothingSaidIsTheTopType`,
  `schema.TestRefinement_ATypeBesideTheAllOfIsThePosition`,
  `schema.TestRefinement_AValueSetNarrowsTheRefItBeats`,
  `schema.TestRefinement_AValueComparesAsAnEnumEntryDoes`,
  `schema.TestRefinement_ANarrowingKeepsItsTargetOnANode`,
  `schema.TestRefinement_ANarrowedRefStillLowersItsTarget`,
  `schema.TestRefinement_AValueSetBesideTheAllOfIsAConjunct`,
  `schema.TestRefinement_AValueSetIsTypedByEveryConjunct`,
  `schema.TestRefinement_ARefToAPrincipalNamesItsTypedNode`,
  `schema.TestRefinement_AHoistingFormatNarrowsItsKind`,
  `schema.TestRefinement_ATieTakesTheFirstAndKeepsTheOther`,
  `schema.TestRefinement_ANoKindConjunctIsNeverThePrincipal`,
  `schema.TestRefinement_NoConjunctNamingAKindLeavesTheTopType`,
  `schema.TestRefinement_DecidedByTheSetInEveryOrder`, `schema.TestRefinement_TwinsTheRefSpelling`,
  `schema.TestRefinement_StructuralKeywordsPartFromTheRefSpelling`,
  `schema.TestRefinement_ABranchHoldsWhatItsNodeDoes`,
  `schema.TestRefinement_ATypeArrayPrincipalKeepsWhatNoVariantHolds`,
  `schema.TestRefinement_FlattensAsItsFlatSpelling`,
  `schema.TestRefinement_PastItsBudgetStaysAComposition`,
  `schema.TestRefinement_PastItsConjunctBoundStaysAComposition`,
  `schema.TestRefinement_ACappedBranchStaysAComposition`,
  `schema.TestRefinement_RawTextIsComparedAgainstOneBase`,
  `schema.TestRefinement_AUnionBranchIsAConjunct`,
  `e2e.TestExternalContent_ARefinementReadsACopyInAnotherDocumentApart`
- Status: partly built: a structural keyword beside a 3.1 `$ref` keeps what the same keyword here
  consumes (#1067).

### `allof-beside-ref`

An `allOf` written beside a `$ref` at the same level is not composed. A `$ref` site lowers to the
alias over its target ([models](../ir/models.md#composition-entries)), which elects no
composition family for the `allOf` to win or lose, so the alias's census of the keywords it cannot
hold ([`shape-applicator`](#shape-applicator)) keeps the `allOf` verbatim under `openapi:allOf`
(`degraded_lowering`), with an info `openapi/degraded-construct` at the position.

- Cases: `allof-ref-branch-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestRefSiteKeywords_KeptAtEveryPosition`

### `allof-inline-residue`

A branch written inline rather than as a `$ref` owns no node, so composition has nowhere to point
([models](../ir/models.md#composition)) and the branch is merged into the composing model in place.
That merge consumes the branch's `properties`, its `required` list, a `type` that is exactly
`object`, which the composed `Model` already restates, for an entry flattened under
[`allof-classification`](#allof-classification), its `allOf`, and the `oneOf` or `anyOf` the
position distributes across its variants ([`union-beside-structure`](#union-beside-structure)).

JSON Schema conjoins an `allOf`'s keywords with those beside it, so what a branch writes for the
position reads as written beside the `allOf`, in a composition and in a refinement
([`allof-refinement`](#allof-refinement)) alike: its `title`, `description`, `externalDocs`,
`deprecated`, `example`, `examples`, `xml`, `default`, `readOnly`, `writeOnly`, value constraints,
collection bounds, `format: password` ([`password`](#password)), vendor extensions and
validation-only keywords ([`validation-only`](#validation-only)) land where the same keyword beside
the `allOf` lands, each read at the branch's own pointer and reported as that keyword is there,
which for most is not at all. A `nullable` folds only from a branch writing no `properties`, under
the nullable-widening policy ([`nullable`](#nullable)). A `unevaluatedProperties: false` does not
fold, being a composing model's openness; a bound does not where the position has no field for it,
as one beside the `allOf` reaches none ([`constraints`](#constraints)): a value bound on a node with
no `Constraints` for one, and a collection bound on any node but a `List` or a `Tuple`, the `Union`
of a type list holding one where such a variant does; a collection bound `Constraints` cannot state,
a `uniqueItems` of `false` or a count past 64 bits, never folds. A `oneOf`, an `anyOf` or a
`$dynamicRef` beside the `allOf` keeps it from refining ([`allof-refinement`](#allof-refinement))
and changes nothing else here: where the `allOf` composes, its branches fold into the composition
as they do with none of them there, beside a `$dynamicRef` it keeps from expanding
([`dynamic-ref`](#dynamic-ref)) or a union kept verbatim
([`union-beside-structure`](#union-beside-structure)). Where a union beside the `allOf` or on one of
its branches distributes, a branch's annotations and what a use binds reach the `Union` and its
carriers, and a value bound each variant, as the same keyword beside the `allOf` does, and a branch
with no residue is kept on no variant
([models](../ir/models.md#composition-beside-a-union)).

Folding needs no precedence, because a branch that disagrees with any conjunct on one of these, the
keywords beside the `allOf` among them, folds nothing, in every order: the keywords beside the
`allOf` stand, and each branch writing that keyword otherwise stays whole, with its report. Two
keywords a position keeps in one entry, `if` and `then` say, are compared as that entry. Value
bounds and the counts `minItems` and `maxItems` agree where they meet, as a redeclared field's do
([`allof-redeclared-property`](#allof-redeclared-property)): `maxLength: 3` beside `maxLength: 5`
folds to `3`, `minItems: 1` beside `minItems: 2` to `2`, and `multipleOf: 4` beside `multipleOf: 6`
to `12`, whatever their order, a number written two ways keeping the first spelling. Two `pattern`s
have no one value, and neither has a 3.0 `exclusiveMinimum` with no bound beside it, which fills no
field, so each disagrees with another written differently. A branch whose folding keywords write a
relative `$ref` it reads against another base than the position does, an `$id` at or above it naming
another URI, folds nothing either, since that `$ref` would name another schema read at the position;
it stays whole, with its report, and is no conjunct the others are compared with. One writing none
there, or only absolute ones, reads the same at the position, so an `$id` alone keeps nothing it
writes from folding.

A branch declaring anything no reader consumes is kept verbatim in
`Unmodeled["openapi:<path>"]` under `degraded_lowering` — the *whole* branch, consumed keywords
included, so the source stays reconstructable rather than having to be reassembled from two places —
keyed by its path from the composed schema (`allOf/1`, or `allOf/1/allOf/0` for an entry of a
flattened one) so siblings cannot overwrite each other, and reported by an
`openapi/degraded-construct` at the branch naming the keywords that made it residue. Which keywords
those are is derived from the branch's own declared key set rather than from a fixed list, so a
keyword the dialect gains later is residue without anyone maintaining an enumeration. The key set is
read through YAML aliases and merge keys, so a branch written `[*shared]` or `[{<<: *shared}]`
derives the same residue as one written out. The diagnostic is an info, and a warning where the
branch declares a `type` that excludes `object`, because the composing `Model` then asserts a shape
the source contradicts: `{allOf: [{$ref: Pet}, {type: string, maxLength: 3}]}`, over a `Pet` writing
no `type`, composes a model over a source that says `string`. Where the conjuncts share no value, as
they do when `Pet` writes `type: object`, [`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts)
reports that once at the schema and the branch's report is an info. Two branch kinds are outside this
rule rather than limits of it: a `$ref` branch owns a node, so what is written beside the `$ref` is
*modeled* on the branch alias [models](../ir/models.md#composition-entries) describes rather than kept
as residue, and a boolean branch declares no keywords at all, so there is no residue to derive — it is
a decision about the composed node instead, stated with the `false`-schema rule
[`allof-false-branch`](#allof-false-branch).

A flattened entry kept as residue is whole too, its own entries included, so an entry inside it that
is kept on its own account, or a `false` one ([`allof-false-branch`](#allof-false-branch)), is in
`Unmodeled` twice: inside the flattened entry's copy and under its own path. A consumer that reads
every entry reads that one twice.

- Cases: `allof-inline-residue`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestAllOf_BranchWrittenByReferenceStillDerivesResidue`,
  `schema.TestAllOf_MergedBranchKeywordsAreNotResidue`,
  `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`,
  `schema.TestAllOf_NestedInlineBranchKeyedByPointer`,
  `schema.TestAllOf_NestedWrapperWithResidueKeptVerbatim`,
  `schema.TestAllOf_NestedEmptyAllOfKeptVerbatim`,
  `schema.TestAllOf_EmptyAllOfBesideNonObjectTypeNamesBoth`,
  `schema.TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice`,
  `schema.TestVoices_AKeywordFoldsWhereTheAllOfWouldKeepIt`,
  `schema.TestVoices_ABranchDisagreeingFoldsNothingInEveryOrder`,
  `schema.TestVoices_BoundsMeetInEveryOrder`,
  `schema.TestVoices_KeywordsOneEntryHoldsDisagreeAsOne`,
  `schema.TestVoices_UnevaluatedFalseStaysTheModels`,
  `schema.TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode`,
  `schema.TestVoices_KeptAndMetBranchesHoldAtEveryOutcome`,
  `schema.TestAllOf_BranchResidueRidesEveryDistributedVariant`,
  `schema.TestAllOf_ABranchReadAgainstAnotherBaseFoldsNothing`,
  `schema.TestAllOf_ABranchWritingNoRelativeReferenceFoldsUnderAnyBase`,
  `schema.TestRefinement_ABranchReadAgainstAnotherBaseFoldsNothing`,
  `schema.TestCollectionBound_OnABranchLandsAsItsFlatSpelling`,
  `schema.TestCollectionBound_OnABranchMeetsInEveryOrder`,
  `schema.TestCollectionBound_OnABranchWithNoFieldStaysWithItsBranch`,
  `schema.TestRefinement_AValueBoundOnABranchBesideATypeListStaysWithIt`
- Status: partly built: a value bound on a branch beside a type list stays with its branch, though
  the variants that hold one beside the `allOf` hold it too, since those need not be variants it
  constrains (#1310).

### `allof-redeclared-property`

`allOf` intersection folds a field declared in more than one branch into one `Property`
([models](../ir/models.md#the-flattening)), which keeps the first declaration's identity unless
another model carries that declaration. The fold reads every declaration of the field at once,
once every type has lowered, and decides each fact from the whole set, so neither the order of the
branches nor where a component is declared decides what the field holds; declaration order breaks
only a tie, and the merge reports each one. One judgment compares two declarations' types and
returns a verdict: the same type, either one narrower, two nodes that meet, an overlap, disjoint, or
unrelated. A disjoint pair is unsatisfiable but for a null both declarations admit: no other value
satisfies both. An overlap shares values and is merely not narrowed to one. The arms are tried in
this order.
`{}` loses to anything but the node a degraded lowering mints (an enum past its member budget, an
unconvertible const), which is unrelated to every type, and so does an alias of `{}` that says
nothing of its values: as a field's own type it may hold the docs, deprecation and redaction a
reference carries down to the field ([annotations](../ir/annotations.md#what-a-reference-carries-down)),
and anywhere else nothing but where it was declared. Where `{}` loses, the winner's own null
admission stands alone, since `{}` admits null yet lowers non-nullable. Two bare primitives compare
by value domain and spelling (`integer` against `number`, `uuid` against `string`, `int32` against
`int64`). A `Scalar` or `Enum` against a bare primitive wins over a kind it equals or narrows
(`string` against `format: byte`, or an unknown format over bare `string`), is disjoint from a kind
of another family, or from another string format where neither is `bytes` (`uuid` against
`format: date`), and overlaps any other kind (`uuid` against `format: byte`); for an `Enum`, that
overlap is disjoint where the kind's judge refuses every member. A `Scalar` hoisted for `time`,
whose grammar, RFC 3339's `full-time`, the compiler holds though the table pairs it with no kind
([`string-formats`](#string-formats)), is read wherever a kind is, through an alias too, as a
string format of its own: within `string`, an overlap with `bytes`, and disjoint from every other
kind, `time-local` and `date-time` among them, and from an `Enum` none of whose members it
admits. An enum with no members is refused
vacuously, so it is disjoint from any kind it would otherwise overlap, `bytes` included; an enum
with members against `bytes` stays an overlap. A `const` wins when the kind admits its value; where
it does not, it is disjoint from a kind with a judge and overlaps `bytes`, which has none. Against a
`Scalar` hoisted for `time`, a `const` is disjoint where the node refuses its value, and wins where
the node admits it and says nothing but its base, the format and bounds, so a field typed by the
`const` loses none of it. Where the node also says what a `const` can hold beside its value, a
description or a deprecation, the two meet in a `const` holding that too (below); a node giving
examples overlaps it, since nothing shows the `const` admits them. Two
enums, two `const`s or two `Scalar`s equal in every field but where they were declared (IDs, names,
whether hoisted inline, and the provenance of the node and of what it keeps under `Unmodeled`) are
one type where both were written inline at the field's own declarations, in their document, as the
same declaration written twice; a node declared elsewhere, a component or a schema a `$ref` reaches
in this document or another, wins over an inline copy of itself in either order, and two such nodes
are two types the source declared apart, which overlap. A copy is one type with the other node, or
gives way to it, only where what the two keep under `Unmodeled` writes no relative `$ref`, or both
read it against one base where each is written: under an `$id` naming another URI, or in another
document, a `$ref` spelled alike names another schema, so the two were declared apart and overlap.
Two `const`s of different values share none, so they are disjoint. Two enums of one value type,
closedness, bit-flag reading and fallback, whose members holding one value say the same of it, are
judged by the values they hold: sharing none, they are disjoint; one whose values the other holds
every one of is narrower where it also says all the other does beside its members, and otherwise the
two meet, as do two each holding a value the other lacks, in an enum of the values they share
(below), where both list those values in one order. Two listing them in different orders overlap,
since an enum of both would take its order from one declaration. Two holding one set of values in
another order or saying the rest differently, and any other pair of enums, are judged by their kind.
Two models alike in every member but where they and what they name were declared (the same
properties in the same order, each with the same wire name, required flag, visibility, docs,
constraints, values and kept text, and the same catch-all, composition and discriminator, its tag
read by position) are judged the same way where every type they name relates to the other's as the
same type, or as that rule relates the two models: a component wins over an inline copy of itself,
a nested object included, in either order, and two inline copies are one type. A property keeping a
relative `$ref` makes them one only under one base, as above. Two components alike were declared
apart, and models alike in all but a type that relates otherwise are no copies, so either pair
composes as two models do (below). One judgment compares at most 64 pairs of models member by
member, since a `$ref` to a sibling position adds width at no cost in document size; past that the
two are unrelated and compose, which loses nothing.
Two lists, two maps with one key, or two tuples of one length are judged by their parts: each
element, value or position, with what may follow a tuple's last, judged as a pair a level down, null
included, nested up to a bound; their bounds, a keyword with no order (`pattern`) given two ways
meeting; and what else each says, read as two `Scalar`s are but where each was declared: encoding,
documentation, examples, deprecation, XML hints and `Unmodeled`. Two `Scalar`s of one base and
encoding are judged by their bounds and what else they say the same way. A node says all another
does of a part where its member is the narrower or admits null no more widely, its bounds say all
the other's do, or it sets every detail the other sets, alike. Two saying one set of values are
judged as two equal enums are where they say the rest alike; where they do not, they are two types
declared apart, which overlap, but for two `Scalar`s, then judged by their kind. Otherwise the one
saying all the other does in every part is narrower, and two each saying what the other lacks meet in
a node holding both (below), unless they give one detail differently, or one gives examples the
other does not, since nothing shows that node admits them, which leaves them an overlap. Members that
meet, or that compose as two models or a model and a map or a union do (below), make their
containers meet, so lists of two models stand on a list of a model composing both. Disjoint members
still overlap where both admit null, or where both containers admit the empty one, and members
related otherwise leave their containers so. A tuple that a container or another tuple holds is not
compared, since a tuple has a member per position where any other node has one, and comparing them
nested would cost its width to the power of the depth. A structure is narrower than
one it says it is: an alias than the type it stands for, and a model than what it composes through
`Base` or `Mixins`, read through aliases, up to 64 types for one question. A model, a map or a
`const` object is narrower than a model saying nothing but `type: object`, and two such models are
judged as two equal enums are. That model says nothing at all but where it was declared, and an
alias of it is not one, since an alias may say more: a type composing another still reaches it,
while one narrowing a bare object does not, so a title or an example there would be lost. Any other
pair is judged by what the two types hold, an alias as the
type it stands for and a `const` object or array as a structure of that instance type: disjoint
where their kinds share no value, or a value type meets a structure, or two structures of different
instance types meet; an overlap where their kinds share values or a `const` meets a type that ends
in a kind, but for the `time` node above, or a `const` of its value; and unrelated where nothing says
what they share, as between two
models neither of which composes the other, a model and a map, a list and a tuple, an opaque scalar
and a kind, or a union or an external and anything else. A structure whose source states no
instance type is the exception to the disjoint pairs: its own `type` and its `allOf`, read as
[`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts) reads a conjunct, state none, so it admits
every value of another instance type, as `properties` alone constrains objects and admits a string.
That read follows only `type`, `allOf` and an entry's `$ref`, so a schema it reaches writing what
else could refuse a value of some instance type reads as stating its type: a `oneOf`, `anyOf`,
`not`, `if`, `enum`, `const`, `$dynamicRef` or `$recursiveRef`, a keyword beside an entry's or an
alias's `$ref`, a `$ref` landing on no schema or on `false`, or a `false` entry. A value type or a
structure of another instance type is narrower than it, in either order, where it and each alias
naming it hold no more than an alias of `{}` may to be read as `{}`, and overlaps it otherwise,
since a type replacing it would lose what it holds. So a structure written at the declaration
itself, which keeps its docs, deprecation and redaction on its own node, overlaps a value type or a
structure of another instance type where it holds any of them; one a `$ref` reached carries them
down to the field. Two such structures of different instance types overlap, sharing every value of
a third. The source is read in the document holding the composing model, so a structure another
document declares, and one whose `allOf` runs past the 256 schemas that read spends, reads as
stating its type. A pair gets the same verdict as a field's types and as two containers' members,
but for tuples, above.

The field takes the type of the declaration whose type is the same as, narrower than, or meets the
most declarations' types, the first of them on a tie. A declaration's type within every other's
is the intersection, and it wins whatever the order. Each declaration whose type the winner's is
the same as or narrower than, or meets, folds into the field, which adopts its shape detail; every
other one is dropped. So `int32`, `uint32` and `uint16` fold to `uint16` with nothing dropped, in
every order, while `int32` and `uint32` alone keep the first declared and report the other.

Two nodes that meet say what no declared node says, since each adds what the other lacks. The field
takes a node the merge mints: all the winner's node holds, with what each declaration meeting it
adds. Its bounds fold as a property's do, below; each detail the winner leaves empty is taken as a
reference to that node would read it; an enum's values are cut to those every one holds, in the
order the winner lists them; and each member is built from theirs a level down, composed or meeting
theirs in turn, at a position of its own beneath the field's (`/items`, `/additionalProperties`,
`/prefixItems/<n>`). That position is named beneath the declaration naming the field whether the
declaration writes the container or a `$ref` there reaches it, so it may be one the source does not
write; the member's own position would give one ID to two fields building different members of
it. A `const` meeting a
`Scalar` is the node, holding what the `Scalar` says beside its values. So `minItems: 1` beside
`maxItems: 5` is one list holding both, a described list of `integer` beside a list of `int32` one
list of `int32` holding the description, `[a, b]` beside `[b, c]` the enum `[b]`, and lists of two
models a list of a model composing both, each in either order. Every node it mints is anonymous, in
the `composed` namespace, an enum's members too, and derives from its position: the declaration
naming the field, as a composed model does below, or a position beneath it. Where two declarations
meeting the winner give one detail differently, the first's is kept and the later one reported and
kept, as a property's detail is. Where no node holds every declaration meeting the winner, as when
three meet in pairs but no member holds all of theirs, or no value is in every enum, the field keeps
the winner's node, and each declaration meeting it is dropped and reported as an overlap. Where the
declarations leave the winner's node as it stands, because only a keyword with no order differs, the
field keeps that node and nothing is minted.

Two models neither of which holds the other are the exception, and so are a model and a map or a
union, since an instance satisfying both is a value of a type neither names. Where the declarations
whose types no other declaration holds are two or more models, maps or unions, each named directly
or through an alias, and one of them a model, the field's type is a `Model` the merge mints:
anonymous, holding no property of its own, and composing each of those types once as a written
`allOf` of them composes ([`allof-classification`](#allof-classification)). The one written as the
only `$ref`, or else the first `$ref` whose model carries a discriminator, is its `Base`, and
the rest are `Mixins` in declaration order. So a field declared as `Meeting` and as an inline object
names one type holding both, whichever branch comes first, rather than flattening them into one
property list. Whatever another declaration's type holds adds no conjunct: a `{}` or an alias of it
read as one, a model saying
nothing but `type: object` beside one saying more, and a model another declaration composes. Two
models saying nothing, declared apart, are two types neither of which holds the other, so they
compose, and an object saying anything else composes too, so what it says stays reachable. Two
aliases of one model are two types, each able to say more than the model, so they compose, and so
does an alias of a bare object beside another model. Any other type beside them (a kind, a list, a
degraded node, an alias of `{}` saying something of its values), or maps and unions with no model
among them, leaves the choice to the rule above. Every declaration then folds into the
field, so none is dropped for its type, and each conjunct keeps the node its own pointer denotes.
The minted model's ID is in the `composed` namespace ([identity](../ir/identity.md#the-id-grammar),
[models](../ir/models.md#composition-beside-a-union)), so no `$ref` reaches it, and derives from the
declaration naming the field, which its provenance records: the last declaration to widen the schema
holding them all, so the first outside the branch holding the first declaration. A model and a
wrapper it flattens can share a first declaration or a last one while folding different
sets, and not this one. Folds of one set share the one node, as the variants of a distributed union
do.

A `$ref` to an `allOf` branch or wrapper makes a model carrying the declarations under it and not
the others, so one declaration can stand on two models whose fields differ. Where a node stands at a
schema inside the one holding every declaration that holds the first, the field is a different
property from that node's: it takes a `PropID` in the `composed` namespace, derived from the
declaration naming it, and its provenance points at that declaration, and the model's discriminator
follows it.
Every other fold keeps the first declaration's identity, which the walk has already handed out.

The relation between kinds is stated over JSON instances, so that a narrowing, which is adopted
silently, holds in every reading. Its value-domain half is `PrimKind.StrictlyWithin`
([primitives](../ir/primitives.md)) and its spelling half is the string rows of `formatTable`
(`string-formats`): a format refines `string`. Only exact containments are edges. OpenAPI floats
round, so `format: float` admits every finite number in its range; a bounded integer width sits
inside either float width by rounding, while `integer` merely shares the values a float width holds.
That containment holds of instances and not of values, so `int32`, `uint32`, `int64`, `uint64` and
`integer` against `float32`, and `int64`, `uint64` and `integer` against `float64`, are overlaps,
and reported.

A narrower type wins in either order, the intersection being that type, and is no drop. The merged
field admits null only where every declaration folded into it does, and a null any of them writes is
judged against that: a `default: null` or a null example is refused where the merged field does not
admit null, a declaration of the field's own type included. The top type admits null whatever its
bit says, and one folded under another type says nothing of null. A `default` or `examples` a
broader declaration gives is taken only where the winning type admits it, judged per keyword and all
or nothing. The winning type admits a value by its kind, by the `full-time` grammar where it is a
`Scalar` hoisted for `time`, and by the constraints its node carries:
each bound, `multipleOf`, a length counted in code points, an item or property count and
`uniqueItems`, while a `pattern`, which nothing here evaluates, admits no string. Every
declaration's value is held by the same checks to each bound the field keeps that another
declaration brought, on the property or on a node. A value is its declaration's own to answer for
against a bound it states itself at the value the field keeps, so a default its own bound refuses
stays, and against its own type where that is the field's but for bounds; a node the merge builds
can hold members or values the declaration's own does not, so a value written beside it is judged
against that node. Nothing
judges a value against a model, and no declaration's type is a composed field's own, so a composed
field takes no value but a null it admits. A value the field does not take is reported in an info
`openapi/degraded-construct` at
each declaration that wrote it, which is kept verbatim beside the field. Of the values it takes, the
first written is kept, and a declaration giving another is reported and kept; two values are one
where they are one JSON value, a number by its magnitude and an object by its members in any order.
Values are refused before one is kept, so a value the field takes is never lost to a refused one
written before it.

Constraint keywords fold over the declarations folded into the field, each taking the value that
says what they all do, so no order decides it. `minimum`, `exclusiveMinimum`, `minLength`,
`minItems` and `minProperties` take their largest value and `maximum`, `exclusiveMaximum`,
`maxLength`, `maxItems` and `maxProperties` their smallest, by magnitude, a number written two ways
keeping the first spelling; `uniqueItems` holds where any declaration sets it. A bound and its
exclusive form are two keywords, each folding alone, and both apply. `multipleOf` takes whichever
value is a multiple of the other, and otherwise their least common multiple, a number no
declaration wrote, whose multiples are exactly those both admit. `pattern`, `patternMessage`,
`precision` and `scale` have no order and no one value saying two of them, so the first declaration
to set one keeps it, as does `multipleOf` for two steps past 1,024 significant digits between them
or a multiple past what a `BigVal` holds. The title, description, external links, deprecation, XML
hints and each `Unmodeled` key fold over every declaration, the first to give one keeping it, and a
declaration giving one differently is reported and kept. An entry two declarations write alike is
one where it writes no relative `$ref`, or both read it against one base: under an `$id` naming
another URI, a `$ref` spelled alike names another schema.

A field keeping the first declaration's identity keeps that declaration's provenance, and either way
it names the heuristic that promoted its deprecation or its redaction from an extension, wherever
the one it keeps came from. So a field names one source. `Provenance` holds one pointer, and what a
folded declaration adds lands on the field with no record of which wrote it, so writing a keyword on
one branch or on another compiles to the same `types`. A list of the declarations on `Property` was
weighed and declined: it would say which declarations contributed and not which wrote each keyword,
no consumer reads either, and a consumer needing it can take it as an ordinary versioned change ([IR
version](../ir/ir-version.md)).

The IR has no combinator for "string here, integer there", nor a second slot for a `pattern`,
default, title, description, external link, examples, deprecation or XML hint another declaration
already fills, so
whatever of a declaration the fold cannot carry has nowhere to sit on the merged property, and with
nowhere to sit it would go nowhere at all: a release in which a losing branch's type or default
changed would produce no change in the document, which a consumer diffing two versions reads as
nothing having happened. The losing declaration is therefore kept verbatim, as the document wrote
it, in `Unmodeled["openapi:conflicting-redeclaration<pointer>"]` under `degraded_lowering`, keyed by
its own JSON Pointer so a field three branches type three incompatible ways keeps all three entries,
and with the entry's `Provenance` locating that declaration rather than the merged property. The
source node is kept rather than the IR values the merge dropped:
[unmodeled](../ir/unmodeled.md) defines an `Unmodeled` value as the source construct, a
`TypeID` is a compiler-minted registry ID that `irverify`'s reference walk cannot see dangle inside
a byte slice, and one node keeps everything the redeclaration said at once — a `$ref` as the `$ref`
written, a nullability, a default.

Preservation is owed wherever something is dropped, which is wider than the disagreements worth a
warning, and nothing is owed for what the field implies. A broader type and a looser bound say no
more than the field does, and a release changing either changes no value the field admits, so
neither is kept or reported. Each diagnostic sits at the declaration that lost and names the one
whose say the field kept. A dropped declaration is reported against the winner as an
`openapi/conflicting-redeclaration`
warning where their types are incompatible — disjoint kinds (`string` against `integer`, `uuid`
against `date-time`, `time` against `time-local`), an enum none of whose members the other kind
admits, two `const`s of different values, two enums sharing no value, a scalar against a
structure stating its instance type, two structures of different instance types each stating
theirs, or two lists or maps whose members share no
value where one is never empty. Types that share values but that the merge does not narrow to one
(`int32` against `uint32`, an enum of `integer` against an `int32` that admits one of its members,
a `const` against a kind with no judge, against a node that ends in a kind, but for the `time` node
above, or against a `const` of its value saying more, two `Scalar`s of different encodings, such as
`format: hex` against `format: byte`, which no one node holds, two nodes saying one set of values
differently, two enums listing the values they share in different orders, two nodes giving one
detail differently or examples only one gives, or two lists or maps whose members share only null
or share nothing while both admit the empty one) are named in an info `openapi/degraded-construct`,
and so are two types the judgment does not compare (a list and a tuple, a map or a union with no
model to compose with, an opaque scalar, a node a degraded lowering minted, or models beside a type
that keeps them from composing), since which of them the field keeps then follows declaration
order. No declaration is dropped without a
diagnostic. A dropped declaration's `default`, constraints and examples go with
its type rather than folding onto the winner — they describe the shape that lost, and adopting them
would leave one field asserting two contradictory things, with nothing downstream comparing a
value's kind to its property's type; this is keyed on the type being dropped, not on the drop being
called a conflict. A declaration folded into the field is reported as well where no one value says
what it and the field say of a constraint keyword, an `openapi/conflicting-redeclaration` warning
naming the declaration the value was kept from, or where it gives a detail differently from the one
kept, an info `openapi/degraded-construct`; differing only in nullability, it takes no entry: the
merged
field admits null only where every folded declaration does, which is the conjunction a single
schema's null verdicts fold under ([`nullable`](#nullable)).

The fold waits for every type to lower because the judgment reads the nodes the declarations name, a
component declared after the schema composing it or a node a `$ref` into another document promises
among them; it finishes the model before any other decision reads it (`compile.Types.Complete`), so
a form part's flags read the merged field. A fold judges every pair of declarations, so it judges a
field's first 64 at most; each later one is kept verbatim, unjudged, under an
`openapi/budget-exceeded` error at the first of them.

- Cases: `allof-conflicting-type`, `allof-narrowing-redeclaration`, `allof-redeclared-models`,
  `allof-redeclared-bounds`
- Codes: `openapi/conflicting-redeclaration`, `openapi/degraded-construct`, `openapi/budget-exceeded`
- Tests: `merge.TestFold_NullabilityIntersectsWhenTargetsAgree`,
  `merge.TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict`,
  `merge.TestFold_ALosingDetailIsKeptAndReported`,
  `merge.TestFold_TitleAndExternalDocsFoldLikeDescription`,
  `merge.TestFold_ANarrowingIntersectsNullability`,
  `merge.TestFold_ADroppedTargetKeepsTheFirstNullAdmission`,
  `merge.TestFold_TheJudgmentsVerdictIsWhatIsReported`,
  `merge.TestFold_RedeclaredModelsCompose`,
  `merge.TestFold_OnlyTwoOrMoreModelsCompose`,
  `merge.TestFold_AComposedFieldIntersectsNull`,
  `merge.TestFold_AComposedFieldKeepsEveryDeclarationsDetail`,
  `merge.TestFold_AComposedFieldAdoptsOnlyANull`,
  `merge.TestFold_ComposesOnlyTheDeclarationsItJudges`,
  `merge.TestFieldSpan_NamesEachSetOfDeclarations`,
  `merge.TestFold_AFieldAnotherModelCarriesTakesAnIdentityOfItsOwn`,
  `merge.TestPropIndex_ADiscriminatorFollowsItsFieldToANewIdentity`,
  `e2e.TestAllOf_ACollisionPastTheFlatteningDepthIsRefused`,
  `merge.TestSelectBase_ChoosesAsTheRuleSays`,
  `schema.TestAllOf_AFieldRedeclaredAsTwoModelsComposesThem`,
  `schema.TestAllOf_AComposedFieldKeepsADiscriminatedBase`,
  `schema.TestAllOf_EveryDistributedVariantSharesOneComposedField`,
  `schema.TestAllOf_AnAliasIsJudgedAsTheTypeItStandsFor`,
  `schema.TestAllOf_RelatedStructuresAreNotCalledAConflict`,
  `schema.TestAllOf_ATypeHoldingAnotherTakesTheField`,
  `schema.TestAllOf_AnObjectSayingMoreThanItsTypeComposes`,
  `schema.TestAllOf_AnAliasOfTheTopTypeAddsNoConjunct`,
  `schema.TestAllOf_AStructureStatingNoTypeAdmitsEveryOtherKind`,
  `schema.TestAllOf_AnUntypedStructureKeepsWhatItsDeclarationWrites`,
  `schema.TestAllOf_AnUntypedReadPassingOverAKeywordKeepsTheConflict`,
  `schema.TestAllOf_ASourceIsReadOnlyInItsOwnDocument`,
  `merge.TestTypeJudge_ModelsAlikeAreCopies`,
  `merge.TestTypeJudge_ModelsKeepingAReferenceAreAlikeUnderOneBase`,
  `merge.TestFold_ModelsAlikeAreOneType`,
  `merge.TestTypeJudge_ModelPairsStopAtTheirBudget`,
  `merge.TestTypeJudge_NestedModelsCostTheirBudgetNotTheirWidthToTheDepth`,
  `schema.TestAllOf_ModelsAlikeAreOneType`,
  `schema.TestAllOf_AModelComposesWithAMapOrAUnion`,
  `merge.TestFold_EnumsAreJudgedByTheValuesTheyShare`,
  `merge.TestFold_EnumsSharingNoValueTogetherKeepTheWinner`,
  `merge.TestFold_EnumsListingSharedValuesApartKeepTheWinner`,
  `merge.TestFold_TwoEnumsCostTheirWidth`,
  `merge.TestFold_ANarrowerListAdoptsTheBroadersDetail`,
  `merge.TestFold_ANarrowerListAdoptsTheBroadersEncoding`,
  `merge.TestFold_ContainersOfModelsComposeTheirMembers`,
  `merge.TestFold_AConstMeetsATimeScalarHoldingDetail`,
  `merge.TestFold_AMeetingNoNodeHoldsKeepsTheWinner`,
  `merge.TestFold_AFailedMeetingReportsOnlyItsDrops`,
  `merge.TestFold_ADetailTwoMeetingDeclarationsGiveDifferentlyIsKept`,
  `schema.TestAllOf_EnumsAndListsNarrowToWhatTheyShare`,
  `schema.TestAllOf_ContainersOfDifferentModelsComposeTheirMembers`,
  `schema.TestAllOf_EnumsSharingSomeValuesMeetInAnEnumOfThem`,
  `merge.TestFold_AnEnumWithinAnotherAdoptsItsDetail`,
  `merge.TestFold_ANarrowerListAdoptsWhatTheBroaderKeeps`,
  `merge.TestFold_TuplesBuildWhatFollowsTheirPositions`,
  `merge.TestBuilder_RefusesWhatNoNodeHolds`,
  `merge.TestFold_AValueIsJudgedByTheNodeTheFieldHolds`,
  `schema.TestAllOf_ADefaultIsJudgedByTheNodeTheFieldHolds`,
  `merge.TestUnaliased_ReadsAChainUpToItsBound`,
  `merge.TestComposes_ReadsAChainUpToItsBound`,
  `merge.TestComposes_ReadsEachPartOnce`,
  `merge.TestSaysOnlyObject_ReadsEveryFieldThatSaysSomething`,
  `e2e.TestExternalContent_AComposedFieldIsNamedInItsDocument`,
  `e2e.TestExternalContent_AFieldCarriedApartIsReadInItsDocument`,
  `merge.TestFold_ANarrowingIsNoDrop`,
  `merge.TestFold_ANarrowingCarriesTheNullMarkers`,
  `merge.TestIntersectNull_TheMarkerFollowsTheBit`,
  `merge.TestFold_OverlappingKindsAreKeptAndReportedAsInfo`,
  `merge.TestFold_DisjointKindsStillWarn`,
  `schema.TestAllOf_OverlappingPrimitivesAreReportedNotConflicting`,
  `merge.TestFold_EnumPairsAreJudgedByKind`,
  `schema.TestAllOf_EnumPairsAreJudgedByKind`,
  `merge.TestTypeJudge_RelateArms`,
  `merge.TestTypeJudge_AdmitsPerNode`,
  `merge.TestAdmitsConstraints_JudgesEachKeyword`,
  `merge.TestIsMultipleOf_JudgesExactly`,
  `schema.TestAllOf_AWinnersConstraintsRefuseADefaultInEitherOrder`,
  `merge.TestClassify_ALiteralOverlapsWhatHoldsValues`,
  `schema.TestAllOf_NonBareNarrowingIsOrderInvariant`,
  `merge.TestFold_AScalarOverANarrowerKindWins`,
  `merge.TestFold_TheTopTypeTakesTheOtherSidesNullAdmission`,
  `schema.TestAllOf_TheTopTypeAndANullableKindMeetAsNullableInEitherOrder`,
  `schema.TestAllOf_ADefaultTheNarrowerNodeRefusesIsKeptInEitherOrder`,
  `schema.TestAllOf_TwoEqualListsRefuseANullTheMergeDoesNotAdmit`,
  `schema.TestAllOf_AnUnknownFormatWinsInEitherOrder`,
  `schema.TestAllOf_ReconcileAccumulatesRicherDetailForAnUnknownFormat`,
  `merge.TestFold_AUint8NarrowsInt16InEitherOrder`,
  `spelling.TestNarrows_HoldsEveryContainment`,
  `spelling.TestNarrows_OmitsRoundingContainments`,
  `spelling.TestRelate_ClassifiesEveryVerdict`,
  `spelling.TestFormatTable_EveryRowNarrowsItsBareType`,
  `merge.TestAdmits_JudgesEachKindExactly`,
  `merge.TestAdmitsFormat_JudgesTimeAsAFullTime`,
  `merge.TestAdmitsFormat_SharesNoStringWithAKind`,
  `merge.TestRelateKinds_ReadsAHeldFormatAsAStringFormat`,
  `merge.TestKindOf_TheFirstEncodingDecidesTheFormat`,
  `schema.TestAllOf_ATimeIsJudgedByItsGrammar`,
  `merge.TestAdmits_ReadsAHugeExponentWithoutMaterializingIt`,
  `merge.TestTypeJudge_NullIsJudgedByNullability`,
  `merge.TestFold_ExamplesAreJudgedAsOneKeyword`,
  `merge.TestFold_AnInadmissibleValueIsKeptNotAdopted`,
  `schema.TestAllOf_ANullOnARefOverTheTopTypeIsKept`,
  `merge.TestFold_IdenticalNodesAreOneType`,
  `schema.TestAllOf_IdenticalNodesAreOneType`,
  `merge.TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole`,
  `merge.TestFold_TheSetDecidesWhateverTheOrder`,
  `merge.TestFold_ATieGoesToTheFirstAndIsReported`,
  `merge.TestFold_ADroppedFirstDeclarationIsKept`,
  `merge.TestFold_AConstraintConflictNamesTheDeclarationKeptFrom`,
  `merge.TestFold_ABoundTakesItsTightestValueInEveryOrder`,
  `merge.TestFold_AKeywordNoOneValueStatesIsKeptAndReported`,
  `merge.TestCommonStep_HoldsTheMultiplesOfBoth`,
  `merge.TestCommonStep_RefusesWhatItCannotRead`,
  `merge.TestBoundsRelation_JudgesWhichSaysAllTheOtherDoes`,
  `merge.TestFold_NodesThatMeetStandOnAMintedNode`,
  `merge.TestFold_TheBoundsOfEveryDeclarationMeetInEveryOrder`,
  `merge.TestFold_ANodeHoldingEveryBoundIsNotMinted`,
  `merge.TestFold_NodesDifferingInAKeywordNoOneValueStatesKeepTheWinner`,
  `merge.TestFold_AliasesOfOneModelComposeThoughTheirBoundsMeet`,
  `merge.TestFold_AValueIsHeldToTheBoundsOthersBring`,
  `merge.TestFold_ANodesBoundsJudgeAValueWrittenAgainstItsOwnKind`,
  `merge.TestFold_AValueOfABroaderMemberIsJudgedByTheField`,
  `merge.TestFold_AValueOnAListInAnotherDocumentIsJudgedAsItsOwn`,
  `merge.TestFold_ADroppedDeclarationsBoundsStayOffAMintedNode`,
  `merge.TestTypeJudge_ATupleOfTuplesCostsItsWidthNotItsWidthToTheDepth`,
  `merge.TestTypeJudge_ATuplesPositionsCompareOnlyAtAFieldsOwnDepth`,
  `schema.TestAllOf_ABoundTakesItsTightestValueInEitherOrder`,
  `schema.TestAllOf_ANodesBoundsReachTheFieldInEitherOrder`,
  `schema.TestAllOf_ANodeHoldingMoreThanBoundsIsAdoptedInEitherOrder`,
  `schema.TestAllOf_ADefaultABoundOnThePropertyRefusesIsKeptInEitherOrder`,
  `schema.TestAllOf_AFoldedDeclarationLeavesNoRecordOfItself`,
  `merge.TestFold_DeclarationsPastTheBoundAreKeptUnjudged`,
  `merge.TestFold_ADroppedDeclarationStillSaysWhatIsNotShape`,
  `merge.TestFold_UnmodeledKeepsTheFirstOfAKeyAndReportsAnother`,
  `merge.TestFold_AValueSpelledTwoWaysIsOneValue`,
  `merge.TestFold_ThePromotionMarkerFollowsTheDeprecationKept`,
  `merge.TestFold_ThePromotionMarkerFollowsTheSecretTaken`,
  `schema.TestAllOf_RedeclarationMarksAPromotedSecretOnly`,
  `schema.TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree`,
  `schema.TestAllOf_ContainerMembersAreJudgedAsFieldsAre`,
  `merge.TestClassify_ComparesResolvedKinds`,
  `schema.TestAllOf_ARefToAnInlineNodeIsDeclaredApart`,
  `schema.TestAllOf_ACopyANarrowingKeptIsStillACopy`,
  `schema.TestAllOf_AListOfAComponentWinsOverAListOfItsCopy`,
  `schema.TestAllOf_ARefToASiblingFieldIsDeclaredApart`,
  `e2e.TestExternalContent_ANodeAnotherDocumentHoistsIsNoCopyOfAnInlineOne`,
  `e2e.TestExternalContent_ACopyInAnotherDocumentIsJudgedThere`,
  `schema.TestAllOf_ARedeclarationIsJudgedOnceTheComponentsHaveLowered`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`,
  `merge.TestFold_UnmodeledAlikeUnderAnotherBaseIsKept`,
  `merge.TestTypeJudge_CopiesUnderAnotherBaseOverlap`,
  `schema.TestAllOf_RedeclarationsUnderAnotherBaseStayApart`,
  `schema.TestAllOf_RedeclarationsWritingNoRelativeReferenceFoldUnderAnyBase`,
  `schema.TestAllOf_ACopyOfAComponentUnderAnotherBaseStaysApart`,
  `e2e.TestExternalContent_ACopyOfAnotherDocumentsComponentStaysApart`
- Status: partly built: two enums of different value types are judged by their kind, so one within
  the other overlaps it rather than narrowing (#1654); a structure stating no instance type that
  refuses some kinds only through a `oneOf`, `anyOf`, `not` or `if` reads as stating its type, so
  it conflicts with a kind it admits (#1671); an alias of an enum, a `Scalar` or a list is judged
  by its kind beside a node it could meet, so the two overlap or are not compared rather than
  meeting (#1672); a model and the wrapper it flattens that fold different sets only because the
  model stops at the flattening depth are refused as `compile/registry-refused`, not told apart,
  since one declaration names both.

### `allof-required`

A `required` name written on an `allOf` branch, inline or beside a branch's `$ref` (a flattened
entry's own and its entries' included), or beside the `allOf` itself lands on the composed `Model`
by [`required-members`](#required-members), whichever branch declares the property: a branch holding
nothing but `required` attaches across the whole composition, not only its own `properties`. A
branch is read from its local schema, never from its `$ref` target, whose `required` list belongs to
the target's own model. A name only a `Base` or `Mixin` declares, or one nothing declares, is in
`Model.Requires`, since a `Model` stores its own properties and no flattened view across `Base` and
`Mixins` ([models](../ir/models.md#required-members)): the composed model makes an inherited
property required without declaring it a second time.

- Cases: `allof-required-only`
- Tests: `schema.TestAllOf_CompositionRequiredAttaches`,
  `schema.TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyLandsInRequires`,
  `schema.TestAllOf_RequiredNameNothingDeclaresLandsInRequires`,
  `schema.TestAllOf_NestedWrapperRequiredAttaches`,
  `schema.TestAllOf_NestedWrapperRequiredAttachesBesideUnion`

### `allof-false-branch`

The `false` rule ([`whole-schema-fallbacks`](#whole-schema-fallbacks)) holds wherever the schema
appears, composition included. A `false` conjunct (`allOf: [false, …]`) admits nothing, so the
composition admits nothing: the composed `Model` is closed and the branch is kept verbatim beside
it, under `openapi:<path>` as [`allof-inline-residue`](#allof-inline-residue) keys a branch
(`degraded_lowering`), which is what distinguishes it from a model that merely wrote
`additionalProperties: false`, and reported as an info `openapi/false-schema` at the branch. What
the other branches contributed stays — closing is the nearest shape the IR has, and emptying the
model would trade one silent loss for another. The model's catch-all keeps only its patterns, as
beside `additionalProperties: false` ([catch-alls](../ir/models.md#catch-alls)), since the closure
forbids every key they leave: its value goes, an `additionalProperties` the schema writes being kept
verbatim under `openapi:additionalProperties` (`degraded_lowering`), and a catch-all left no pattern
goes whole. A `true` conjunct admits everything, so contributing nothing from it is exact and
nothing is reported. Where a `oneOf` or `anyOf` beside the `allOf` is distributed
([`union-beside-structure`](#union-beside-structure)), every composed variant is closed and keeps
the branch, and the branch is reported once.

- Cases: `allof-boolean-branch`, `allof-nested-inline`
- Codes: `openapi/false-schema`
- Tests: `schema.TestAllOf_NestedFalseBranchClosesModel`,
  `schema.TestAllOf_FalseBranchBesideUnionClosesEveryVariant`,
  `schema.TestAllOf_FalseBranchClosesTheCatchAllItForbids`

### `allof-disjoint-conjuncts`

An `allOf` whose conjuncts share no value admits no instance, as
`{allOf: [{$ref: List}], type: object}` does over an array and `{allOf: [{$ref: Str}, {$ref: Obj}]}`
over a string and an object. Such a schema lowers as any composition does
([`allof-classification`](#allof-classification)), a distributed one included
([`union-beside-structure`](#union-beside-structure)), and draws one `openapi/degraded-construct`
warning at the composing schema, naming each conjunct that narrows the instance types and what it
states. An inline branch that [`allof-inline-residue`](#allof-inline-residue) would warn about
reports its residue as an info there, so the contradiction is reported once however it is spelled.

The reading is of the source, since a `Model` cannot say whether its source wrote `type: object`,
and `{properties: …}` alone admits a string. That is where it parts from the reading
[`allof-refinement`](#allof-refinement) elects a principal by, which takes such a `properties` to
claim an object: one chooses a lowering, and this one says what the source admits. Each conjunct
states a set of JSON instance types, an `integer` within a `number` and named as written: the `type`
beside the `allOf`; an inline entry's `type`, conjoined with
what its own `allOf` states; and for a `$ref` entry, what the schema it lands on states. The schema
is reported where those sets share no member and the schema admits no null as
[`nullable`](#nullable) reads it, under the nullable-widening policy where that is on: a null every
conjunct admits is a shared value, as
[`allof-redeclared-property`](#allof-redeclared-property) reads a pair of declarations. A read of
null that runs out of its budget is undecided and reports nothing. Where only one conjunct narrows
the instance types nothing is reported, since one conjunct contradicts nothing: a `type: "null"`
beside a `false`, `enum` or `const` is silent. The null reading is the whole schema's, so two or
more narrowing conjuncts beside a `false`, `enum` or `const` are reported though that entry alone
takes away the null they share: `{allOf: [{type: [string, "null"]}, {type: [object, "null"]},
false]}` draws the warning beside the `false`'s own report.

What writes no `type` states nothing, so a report is made only of what the source declares: a
boolean entry, whose `false` is [`allof-false-branch`](#allof-false-branch)'s to report, a `$ref`
landing on no schema, and a composition only `enum`, `const`, `format`, `oneOf` or `anyOf` empties.
A keyword beside a `$ref` entry states nothing either: OpenAPI 3.0 ignores it, and reading it from
3.1 on, where it applies, is left open, so `{allOf: [{$ref: Str, type: object}]}` draws no report at
any version. A conjunct writing an `allOf` also states nothing here where its `type` and its entries
leave at most a null, which is what keeps a schema composing a reported one from drawing a second
report; the cost is a miss where nothing reports it, as for a conjunct admitting only a null its own
entries share or a `type: "null"` beside them, or one nested past `MaxAllOfFlattenDepth`. An
`allOf` beside a `$ref` ([`allof-beside-ref`](#allof-beside-ref)) does not compose and is not read.
One passed over for a `const` or an `enum` ([`competing-keywords`](#competing-keywords)) is read as
any composition is, and the warning names the value set the position lowered as.

- Cases: `allof-disjoint-conjuncts`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestAllOf_DisjointConjunctsReportOnceAtTheSchema`,
  `schema.TestAllOf_DisjointReportStaysSilent`,
  `schema.TestAllOf_KeywordBesideARefStatesNothing`,
  `schema.TestAllOf_ComposingADisjointSchemaDrawsNoSecondReport`,
  `schema.TestAllOf_DisjointInlineBranchReportsItsResidueAsInfo`,
  `schema.TestAllOf_DisjointConjunctsFollowTheNullableReading`,
  `schema.TestAllOf_DisjointConjunctsIgnoreDeclarationOrder`,
  `schema.TestAllOf_DisjointConjunctsReportOnModelsAndOperations`,
  `schema.TestAllOf_DisjointReportNamesWhatThePositionLoweredAs`
- Status: partly built: a keyword beside a `$ref` entry is not read from 3.1 on, where it applies
  (#1390).

### `union`

A `oneOf` lowers to a `Union` with `Exclusive` set and an `anyOf` to one without it
([unions](../ir/unions.md)): one `Variant` per branch, in source order, each a reference to
the branch's lowering — a `$ref` branch to its target, an inline one to its own node or primitive
([`inline-hoisting`](#inline-hoisting)) — and never collapsed into optional fields. The union is
untagged, `WireTagged` false, unless a `discriminator` beside it routes it
([`discriminator`](#discriminator)), or [`discriminator-inference`](#discriminator-inference) reads
one off its variants: the tag property's value then names the variant, so the union is `WireTagged`,
an `anyOf` as much as a `oneOf` ([unions](../ir/unions.md)). Every union is
`Closed`, since JSON Schema admits only what matches a listed branch; an extension a promotion
mapping names may open one ([`extension-promotion`](#extension-promotion)). A branch set of
one schema and `null` ([`union-null-variant`](#union-null-variant)), of `null` alone
([`union-null-only`](#union-null-only)), and one written beside structure, a `$ref` or the other
combinator ([`union-beside-structure`](#union-beside-structure),
[`union-beside-ref`](#union-beside-ref), [`competing-keywords`](#competing-keywords)) lower
otherwise.

Each variant's `Name.Hint` is its branch's: a `$ref` branch's is its target's name, and an inline
branch's its position (`variant_1`). An inline branch that refines a `$ref`
([`allof-refinement`](#allof-refinement)), or whose node is a decorator
([annotations](../ir/annotations.md#decorators)) over a `$ref` its `allOf` names, the one way
3.0 annotates a branch, takes the hint that `$ref` takes as a branch of its own, as 3.1's spelling
of the same branch, `{$ref: X, description: …}`, does, and the position's where that names nothing.
Its node keeps the positional hint, which an outside `$ref` to the branch derives too; no emitter
renders it, since a decorator collapses into its reference.

- Cases: `oneof-discriminated`, `anyof-discriminated`, `anyof-untagged`, `union-decorated-branch`
- Tests: `schema.TestUnion_ADecoratedBranchIsHintedAsItsRef`,
  `schema.TestRefinement_VariantNamedAsItsRef`

### `type-arrays`

A schema whose `type` lists more than one type lowers to a `Union` with one variant per listed type
other than `null`, in order, each lowered from the same schema as that type alone and named by it: a
shared primitive for a scalar type, and for `object` or `array` a node hoisted at
`<pointer>/type/<index>`. A `null` among them makes the position nullable instead
([`nullable`](#nullable)), so one type and `null` lower as that type alone, nullable. The union is
exclusive unless two listed types overlap: `integer` and `number`, the one pair of JSON types that
do, since every integer is a number, or a type listed twice, which [`validation`](#validation)
reports. Two variants the types resolve to one primitive, as a width does
([`numeric-formats`](#numeric-formats)), stay two. The bounds a carried schema writes land on its
property or parameter ([`constraints`](#constraints)). Each variant is built from the whole schema,
so the keyword census asks the variants rather than the union: a keyword or bound some variant holds
is homed, and one none holds is kept on the union ([`shape-applicator`](#shape-applicator)).

The `object` variant holds no `discriminator`, so the census keeps it on the union, whether or not a
`oneOf` or `anyOf` is written beside it. No `$ref` or `mapping` can name the variant's position,
which holds a type's name and no schema, so no type is its subtype, and every route the
discriminator takes leads elsewhere: a `mapping` target, the `defaultMapping`, or a component whose
`allOf` names the schema, which composes the union and keeps it as its `Base`, with the
`DiscriminatorValue` the discriminator gives it ([`discriminator-value`](#discriminator-value)). The
object instances the union admits still route by the discriminator, so its targets are resolved as
[`discriminator`](#discriminator) resolves a model's: each position one names is lowered, and one
naming no schema is reported as an `openapi/unresolved-ref` error at its entry. A type array listing
no `object` admits no instance a tag routes, and resolves nothing, as a scalar beside a
discriminator does. Settling the variant's discriminator by its routes, as one beside a union kept
verbatim is settled ([`union-beside-structure`](#union-beside-structure)), would keep it only where
it routes nowhere, and would have to find every component composing the schema to know that. Two
typed homes lose as well. A subtype's `Base` naming the variant would compose the schema's object
half where its `allOf` names the schema, and `Base` names what the source wrote
([composition entries](../ir/models.md#composition-entries)). And `pass/validate` holds a model
discriminator to `ir.IsSubtype`, the subtype relation every consumer reads: a check stepping from a
`Union` base into its object variant would part from that relation, and teaching the relation the
step would change what it means for every document.

- Cases: `keywords-at-every-outcome`, `type-array-numeric`, `type-array-discriminator`
- Codes: `openapi/unresolved-ref`
- Tests: `schema.TestLower_MultiTypeUnion`,
  `schema.TestLower_ATypeArrayIsExclusiveOnlyOverDisjointTypes`,
  `schema.TestLower_ATypeListedTwiceIsNotExclusive`,
  `schema.TestLower_ATypeArrayKeepsItsDiscriminatorOnTheUnion`,
  `schema.TestLower_ATypeArrayResolvesItsDiscriminatorRoutes`
- Status: partly built: a value bound lands on any variant with a `Constraints` field, whether or
  not it constrains that variant's type (#1310); and a type array listing no `object`, as every
  node keeping a discriminator verbatim, reports no target naming nothing (#1662) and lowers no
  position only a target reaches (#1708).

### `union-null-variant`

A `oneOf` or `anyOf` with exactly one branch that is not a bare `type: null`, beside one or more
that are, lowers to a nullable reference to that branch's lowering rather than to a `Union`
([type-graph](../ir/type-graph.md#type-references)). The collapse is held to a `$ref` site's
rule ([models](../ir/models.md#composition-entries)), since it too lowers to a reference: a
collection bound beside it reaches what the position owns ([`constraints`](#constraints)), and a
keyword beside it that no reference holds — a `discriminator`, a `format` other than `password` —
is kept verbatim under `openapi:<keyword>` (`degraded_lowering`) on the node or carrier owning the
position, with an info `openapi/degraded-construct` at the position.

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
combinator: `Base`/`Mixins` ([models](../ir/models.md#composition-entries)) composes one
referent per conjunct, the shape `allOf` writes, and cannot state "conjoined with a union", and
[boundaries](../ir/boundaries.md#deliberate-exclusions) records why none was added. Target
languages are not the constraint here; TypeScript writes `{a: string} & (X | Y)` directly. Where the
conjunction *distributes*, [models](../ir/models.md#composition-beside-a-union) gives the
rule: the body is composed into each variant, and the result is an ordinary `Union` of composed
`Model`s, reported as an info `openapi/composition-lowering` at the schema, since nothing is lost
but the IR's shape no longer mirrors the source's, bar one thing: a `false` in the `allOf` states
that the composition admits nothing, which every composed variant then holds only as the closed
model [`allof-false-branch`](#allof-false-branch) gives it, with the branch kept on each. A
variant conjoins its branch by reference: a `$ref` branch's target, through the alias what is
written beside the `$ref` hoists, and an inline branch's own node, hoisted at its pointer as any
inline schema is ([`inline-hoisting`](#inline-hoisting)), which an outside `$ref` to that pointer
finds too. The referent is carried as written, a branch admitting no object among them
([models](../ir/models.md#composition-entries)). A bare `required` is a body too where every
branch admits only an object or null, the values it constrains or leaves alone, so each variant
requires those names of its branch.

A `discriminator` beside the union, where no branch composes the schema (shape 4), routes the
distributed union ([`discriminator`](#discriminator)): a `mapping` target, or the
`defaultMapping`, naming the component a `$ref` branch names routes to the variant composed over
that branch. OpenAPI routes a variant no entry names by the name of the component its branch
names, which a composed variant, named for its position, does not carry, so that route is written
into the `Mapping`: each such name keys its variant, unless two branches name that component or an
entry spells that key already. No variant carries a `DiscriminatorValue`, which tags a subtype in a
hierarchy, since the mapping's keys tag the union's variants, and
[`discriminator-inference`](#discriminator-inference) may read the mapping off the tags they pin.

A `oneOf` or `anyOf` written as an inline branch of the schema's `allOf` conjoins with the rest of
the composition as one written beside the `allOf` does, so it distributes the same way, where the
schema writes no union of its own and neither it nor the branch a `discriminator`, its `allOf`
composes rather than refines ([`allof-refinement`](#allof-refinement)), and the union is the one
branch union whose branches declare shape (below). Its variants take their IDs from the
branch's pointers, its `$ref` and inline branches conjoin as above, and the branch keeps nothing of
the union, what else it writes merging as any branch does
([`allof-inline-residue`](#allof-inline-residue)). A union on a branch that is one
of the shapes below, one of two branch unions declaring shape, distributing either of which would
drop the other, or one on a branch nested past the flattening depth stays whole with its branch, and
so does one whose branches declare no shape, on each variant where another union distributes. Below
a discriminated ancestor (shape 6) a union on a branch stays whole even at a position no route
names: which positions a route names is read from the position, and every reader of the schema
shares one reading of it, which has none. Distribution cannot reach these shapes:

1. the structural body is not a `Model` (a `const`, an `enum`, a scalar, or a bare
   `items`/`prefixItems`/`additionalProperties`/`patternProperties`, or `required` beside a branch
   that may be no object, with neither `properties` nor a `type`), so there are no composition
   fields for the variants to carry;
2. a branch is a bare `type: null`, which a union lifts onto the position's `Nullable` rather than
   conjoining it ([`union-null-variant`](#union-null-variant)), so no variant could say whether the
   body admits the null;
3. a branch is a `$ref` that names no referent the compilation resolves — a cross-document target
   it does not intern, an undeclared component, a pointer addressing no schema — so it declines
   rather than composing a variant whose branch quietly went missing;
4. a `discriminator` is declared and a branch composes the schema through `allOf`, at any depth
   (`Cat: {allOf: [{$ref: Pet}, …]}`): the schema is the base of a hierarchy the discriminator
   routes, which a union of composed variants would no longer be the base of;
5. `oneOf` and `anyOf` are both declared, so distributing one would drop the other;
6. a `discriminator` on an `allOf` ancestor routes to the schema, by a `mapping` key or by its name
   as a component ([`discriminator-value`](#discriminator-value)): the route names what the schema
   lowers to, which as a `Union` would be no subtype of that ancestor (`ir.IsSubtype`, what
   `pass/validate` holds a mapping to), so it stays a `Model` under it, with the tag that rule gives
   it, if any. One written beside a union routes by name only what that union lists, so beside a
   schema it does not list, and no `mapping` key names, a union distributes.

In each, whatever structural body the schema declares is lowered — `properties` or a declared
`type` yields a `Model` or typed node — and the union branches are kept verbatim in
`Unmodeled["openapi:oneOf"]` / `["openapi:anyOf"]` (`degraded_lowering`), reported as an info
`openapi/degraded-construct` at the schema naming which applied; in case 3 the broken
reference is reported as well, as an `openapi/unresolved-ref` error. Case 1's body-less sibling
leaves nothing to build: it reduces to `any`, and the sibling itself is kept by
[`shape-applicator`](#shape-applicator) rather than dropped, so the union's diagnostic and the
sibling's are both reported. A `null` branch in this shape is **not** lifted to `Nullable`: the
union it belongs to is no longer structural, so lifting one branch out of it would assert something
the lowered type does not say. The degradation is that the generated SDK is *under-constrained* — it
admits values the union would reject — which is why the branches must survive for a validating
emitter to reach.

A keyword beside the union that every branch already says adds nothing to it: a `type` naming each
kind a branch admits, null included, and a `format`, an `enum`, a `const` or an `items` each branch
is held to, read as [`allof-refinement`](#allof-refinement) reads what a conjunct admits and within
its bounds, a read cut short saying none. An `items` written as a list, a draft's tuple, is said by
no branch. Where the branches declare shape (below) and say every
keyword that makes a body, the schema means the union alone, and it lowers as if those keywords
were not written: `{type: string, anyOf: [{$ref: V4}, {$ref: V6}]}` as `{anyOf: [{$ref: V4},
{$ref: V6}]}`, and `{type: object, oneOf: [{$ref: A}, {$ref: B}]}` as the union of `A` and `B`, not
a union of models composed over each, which a `discriminator` beside it routes. A `nullable` beside
such a union reads as it reads beside any union ([`nullable`](#nullable)), and a keyword the
branches do not all say, a bound or a `format` among them, is kept as the census keeps it beside a
union ([`shape-applicator`](#shape-applicator)). Branches declaring no shape only narrow the body,
which stays validation logic beside it whatever they repeat, and a `discriminator` beside the union
keeps the body where a branch composes it (shape 4).

A `discriminator` beside any of these shapes whose body lowers to a `Model` stays on that `Model` only
when every type it routes to is a subtype of it at any depth (`ir.IsSubtype`, the rule
`pass/validate` holds a model discriminator to): each `mapping` target, the `defaultMapping`, and
each component a `$ref` branch names, which it routes to by name — `oneOf`'s branches where `oneOf`
and `anyOf` are both declared, since `oneOf` wins. An inline branch, or one naming no component, is
no route unless the `mapping` names it. Otherwise the whole object (`propertyName`, `mapping`,
`defaultMapping`) is kept verbatim in `Unmodeled["openapi:discriminator"]` on the node keeping the
union, with an info `openapi/degraded-construct` at the discriminator; a subtype keeps the
`DiscriminatorValue` its mapping key gives it, since that is still the tag its instances carry, and
a body the move leaves holding a map and nothing else lowers to that map's `MapT`
([containers](../ir/containers.md)). A union whose branches declare no shape
(`validation_only`) is held to the same rule. A multi-typed body leaves no discriminator to settle:
[`type-arrays`](#type-arrays) keeps it on the `Union` the body lowers to, beside the kept union.
The compiler decides once every node is interned (`compile.Settlement`), because neither the walk
nor the source schemas can answer it: a subtype declared after the `Model` has no `Base` while the
`Model` lowers, and a schema whose `allOf` names the `Model` may still lower to no subtype — an
`enum`, a `const`, or an alias over a `$ref` beside it.

A union whose branches declare no shape at all (`oneOf: [{required: [a]}, {required: [b]}]`) is not
a degradation and is not one of these shapes: it narrows the body without reshaping it, which is
validation logic, so it is kept under `validation_only` with the keyword family of
[boundaries](../ir/boundaries.md#validation-only-constructs) and reported as an info
`openapi/validation-only-keyword` at the schema ([`validation-only`](#validation-only)). A `$ref`
branch declares shape and a boolean one declares none. An inline branch declares shape where one
keyword it writes does, and a keyword is judged alike whether the branch writes it alone or beside
the body's own `type`, so repeating the body never changes a verdict:

1. A keyword that documents the instance (`title`, `description`, `$comment`, `deprecated`,
   `examples`, `example`, `externalDocs` or an `x-` extension), names the schema (`$anchor`,
   `$dynamicAnchor`), holds schemas it does not apply (`$defs`), says whether it may be null
   (`nullable`), or only narrows what the body shapes declares none, whatever it holds. The
   narrowing keywords are `required`, the value constraints and collection bounds of
   [`constraints`](#constraints), and the validation-only keywords of
   [boundaries](../ir/boundaries.md#validation-only-constructs), an `unevaluatedProperties` other
   than `false` among them. A keyword written `null` is left out
   ([`null-keywords`](#null-keywords)).
2. `type`, `format`, `enum` and `const` declare none where every value the body admits holds to
   them, null aside for `type`, read as [`allof-refinement`](#allof-refinement) reads what a
   conjunct admits: from the body's own keywords and its `allOf`, across each `$ref`, leaving out
   the unions the body itself writes. So `type: object` beside an object body declares none, nor
   does `type: [object, 'null']`, a `type` naming only `null` contributes the null and nothing else,
   and `format: date` beside a bare `type: string` declares shape. `format: password` asks for
   redaction and names no form ([`password`](#password)), so it declares none, as an `x-sensitive`
   asking the same declares none by point 1: an SDK reads neither reason's entry, so a secret one
   branch alone writes is lost to it either way.
3. `properties` and `items` declare none where each property the branch writes, and its element,
   restates the body's declarations of it: those in the body's own `properties` or `items`, and in
   a schema its `allOf` composes, through any nesting and across `$ref`s, but not in a union's
   branch. A schema restates its declarations where it is a boolean, which says nothing or only
   forbids what it is written for; where one of them is written alike against one base, as
   [`allof-refinement`](#allof-refinement) compares two schemas; where they admit nothing together;
   or where each key it writes says nothing they do not say together, by points 1, 2 and 4 read
   against what they admit together, a `$ref` landing on a schema they compose through `allOf`
   conjuncts and `$ref`s, and its own `properties` and `items` restating what they declare of those
   in turn. The keywords beside a `$ref` conjoin with it at every version
   ([`ref-use-site`](#ref-use-site)). So `{type: string}` restates
   `{type: string, format: uri, minLength: 1}`, `{type: string, minLength: 2}` restates
   `{type: string}`, `{type: string, format: uri}` restates `{type: string}` declared beside
   `{format: uri}` but not `{type: string}` alone, and `{$ref: X}` restates
   `{allOf: [{$ref: X}], description: d}`. A property or element none of the body's declarations
   names is new shape.
4. An `allOf` declares none where each conjunct declares none, judged as the schema writing it is,
   so `{allOf: [{required: [a]}]}` reads as `{required: [a]}`. A `$ref` conjunct of a branch itself
   declares shape, as a `$ref` branch does.
5. Every other keyword declares shape: `prefixItems`, `additionalProperties`, `patternProperties`,
   an `unevaluatedProperties` of `false`, a nested `oneOf` or `anyOf`, `$dynamicRef`,
   `discriminator`, `readOnly`, `writeOnly`, `default`, `xml`, the content vocabulary, `$id`,
   `$schema`, `$vocabulary` and any keyword of no vocabulary.

A branch declaring shape falls to the shapes above. The branches and the body are read within
the bounds of [`allof-refinement`](#allof-refinement), which a union's branches share, an `enum`
a branch writes counting its members, and the body only as far as they need: a branch writing only
keywords point 1 names reads none of it, the declarations of what a schema nests are read only
where it nests something, and what the body declares of one property or element is read once,
however many branches restate it. A read that does not finish says nothing, so its union is judged
as one whose branches declare shape.

- Cases: `allof-oneof-cooccurrence`, `discriminated-union-beside-model`,
  `discriminated-union-distributed`, `unhomed-keywords`,
  `keywords-at-every-outcome`, `union-restated-branches`, `union-restated-type`,
  `union-inline-branches`, `allof-union-branch`
- Codes: `openapi/composition-lowering`, `openapi/degraded-construct`, `openapi/unresolved-ref`,
  `openapi/validation-only-keyword`
- Tests: `schema.TestOneOf_CoDeclaredNotDistributedReasons`,
  `schema.TestOneOf_CoDeclaredKeywordsEveryBranchSaysLeaveTheUnionAlone`,
  `schema.TestOneOf_CoDeclaredTypeReadsNullableByItsDialect`,
  `schema.TestOneOf_CoDeclaredInlineBranchIsConjoinedByItsNode`,
  `schema.TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches`,
  `schema.TestUnion_AListOfItemsBesideTheBranchesIsNotSaid`,
  `schema.TestAllOf_AUnionBranchDistributesAsBesideTheAllOf`,
  `schema.TestUnion_ASubtypeADiscriminatorRoutesToStaysASubtype`,
  `schema.TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed`,
  `schema.TestDiscriminatorBesideUnion_LeavesAMapWhenItMoves`,
  `schema.TestOneOf_CoDeclaredRestatingBranchDeclaresNoShape`,
  `schema.TestOneOf_CoDeclaredBranchSayingMoreDeclaresShape`,
  `schema.TestOneOf_CoDeclaredBranchSayingMoreBesideANonModel`,
  `schema.TestOneOf_CoDeclaredBranchKeywordIsJudgedAloneAsBesideTheType`,
  `schema.TestOneOf_CoDeclaredUnionReadPastItsBudgetDeclaresShape`,
  `schema.TestOneOf_CoDeclaredRestatementReadsReferencesAgainstTheirBase`,
  `schema.TestOneOf_CoDeclaredShapelessBranchesAreValidationLogicAtEachPosition`,
  `schema.TestOneOf_CoDeclaredDiscriminatorRoutesTheComposedVariants`,
  `schema.TestOneOf_CoDeclaredDiscriminatorImpliesNoRouteIntoAnotherDocument`,
  `schema.TestUnion_ASchemaNoRouteNamesDistributesBesideItsAllOf`
- Status: partly built: a union beside a composition the bounds cut short degrades even where each
  branch restates only what the body itself writes (#1423), a subtype a discriminator routes to
  keeps its union verbatim (#1694), a union on a branch below a discriminated base stays whole where
  no route names the schema (#1695), and a discriminated union on a branch stays whole with its
  branch (#1698).

### `union-beside-ref`

A `oneOf` or `anyOf` written beside a `$ref` at the same level, rather than beside a structural
body, has nothing at that position to distribute the union across either: it lowers to the alias
over the `$ref` target with the union kept verbatim beside it, under `openapi:oneOf` or
`openapi:anyOf` (`degraded_lowering`), through the same keeper as the structural-body case, and
reported as an info `openapi/degraded-construct` at the position.

- Cases: `union-beside-ref`
- Codes: `openapi/degraded-construct`

### `competing-keywords`

Keywords conjoin, so `{const: a, enum: [a, b]}` admits `a` alone rather than being a malformed
document, and `{oneOf: […], anyOf: […]}` demands both, yet a position lowers to one node.
Three families compete for it: the value keywords `const`, `enum` and `allOf`, elected in that
order; the two union combinators, where `oneOf` wins; and a parameter's or header's `schema` beside
its `content`, where `content` wins, since a media-type entry names both a schema and the media type
serializing it and the IR models both ([`content-parameters`](#content-parameters)). The IR has no
intersection combinator ([boundaries](../ir/boundaries.md#deliberate-exclusions), as for
[`union-beside-structure`](#union-beside-structure)), so the position lowers as the elected keyword
— the most it can express there — and each keyword the election passed over is kept verbatim in
`Unmodeled["openapi:<keyword>"]` under `degraded_lowering` at that keyword's own pointer, reported
as one `openapi/degraded-construct` naming what was elected and what was kept: an info, and a
warning where a parameter's or header's `schema` is passed over for its `content`. Two consequences
are deliberate. The elected combinator still becomes a `Union` rather than degrading to the top
type, because that `Union` is a shape the IR *can* express and discarding it as well would model
nothing at all. And a `{X, null}` `oneOf` written beside an `anyOf` does **not** collapse to
nullable `X` ([type-graph](../ir/type-graph.md#type-references)), because the collapse
asserts the position *is* nullable `X` while the co-declared `anyOf` conjoins with it, and
collapsing would resolve the position to a shared primitive, which must never carry one
declaration's keywords. Where a *structural* sibling is written as well,
[`union-beside-structure`](#union-beside-structure) applies instead and both branch sets are kept.
An `allOf` that refines ([`allof-refinement`](#allof-refinement)) is no rival of an `enum` or a
`const` beside it, which is one of its conjuncts, so the election passes over it only where it
composes. A keyword the *elected* lowering never reads — `type: string` beside an `allOf` composing
an object,
`format` beside a `const` — is kept by the census [`shape-applicator`](#shape-applicator) runs, which
asks the node that was built: `type: object` beside an `allOf` is one the composed `Model` already
asserts, and keeps nothing, and a `type` beside an `allOf` that refines is
[`allof-refinement`](#allof-refinement)'s.

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
`Discriminator.Default`. `Inferred` is not set: the document declares the discriminator. With no
`mapping`, the `Mapping` is nil, and a union routes each variant by the name of its type, as
OpenAPI's implicit mapping does by component name, unless
[`discriminator-inference`](#discriminator-inference) reads it off the tags the variants pin, or the
variants are composed over their branches ([`union-beside-structure`](#union-beside-structure)),
which writes those names out. A mapping
target that resolves to no schema is reported as an `openapi/unresolved-ref` error at the entry,
which the `Mapping` leaves out. A subtype the mapping names under several keys — an alias tag —
takes the smallest in byte order as its `DiscriminatorValue`
([`discriminator-value`](#discriminator-value)), since the field holds one and a mapping's key order
is not a property of the document, with every key kept in the base's `Mapping` and the election
reported as an info `openapi/degraded-construct` at the subtype. A discriminator beside a node that
routes by none is kept verbatim by [`shape-applicator`](#shape-applicator), and one beside a union
written with structure is [`union-beside-structure`](#union-beside-structure)'s. A union's mapping
names a variant, what `pass/discriminator-missing-variant` holds it to and what an emitter routes
by, so a target naming a type that one variant's branch aliases, for a keyword written beside that
branch's `$ref`, lowers to that variant. A type two variants alias stays the target, since nothing
in the mapping says which, and that is reported as any target that is no variant is. A target naming
a position that writes a `$ref`, other than a component or a variant, is read through to what the
`$ref` names, whatever is written beside it: an alias there is neither a variant nor a subtype.

- Cases: `oneof-discriminated`, `discriminator-inheritance`, `discriminator-default-mapping`,
  `discriminator-alias-mapping`, `nullable-union-discriminator`, `discriminator-aliased-branch`
- Codes: `openapi/degraded-construct`, `openapi/unresolved-ref`
- Tests: `schema.TestHomelessDiscriminator_IsKeptBesideEveryOtherNode`,
  `schema.TestUnplacedDiscriminator_IsKeptAtEveryUnionSite`,
  `e2e.TestMappingTargets_ADiscriminatorAReferenceReachesIsBoundWhereItSits`,
  `schema.TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget`,
  `schema.TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget`,
  `schema.TestDiscriminatorMapping_TwoVariantsAliasingATypeRouteToNeither`
- Status: partly built: an empty `propertyName` lowers to IR that `irverify` refuses (#882); a
  mapping to a component that is a `$ref` to a subtype fails validation (#844), and so does one to a
  type two variants alias, which a valid document can write (#1298); a mapping naming some variants
  of a union leaves the routes OpenAPI implies for the others unwritten, where a distributed union's
  carries them, and the IR does not say how a partial mapping routes (#1702).

### `discriminator-value`

A subtype's wire tag lowers to `Model.DiscriminatorValue`, left empty on the base itself. It is read
off the discriminators of the subtype's `allOf` ancestors — the `$ref` branches' targets, then
theirs, level by level to the walk's depth bound, nearest first, a schema that declares none passed
through and no schema its own ancestor — as the key an ancestor's `mapping` spells for the subtype,
the mapping naming it by reference or by bare schema name, an inline subtype by its position's
reference, and where several keys name it [`discriminator`](#discriminator) elects one. A key of
`""` is a tag, not an absence. Once no ancestor's mapping names it, the tag is the subtype's schema
name as declared (`Cat/Dog`, not its pointer token) if it is a component schema that an ancestor's
discriminator routes by name, OpenAPI's implicit mapping, unless
[`discriminator-inference`](#discriminator-inference) reads that the subtype rules that name out.
OpenAPI's conditions for a discriminator say which schemas it routes by name: one written beside no
`oneOf` or `anyOf`, a parent schema, routes every schema building on it through `allOf`, and one
beside either routes the schemas that union lists, where "all possible schemas MUST be listed
explicitly": each schema a `$ref` branch, of the `oneOf` where both are written, ends on, in
whichever document, as the ancestors are read. A schema composing a discriminated union that does
not list it is a narrowing of that union, every instance of it an instance of one variant and
carrying that variant's tag, so no tag routes to it and it carries none, and it composes the union
as written ([models](../ir/models.md#a-union-a-composition-names)). Reading what such a schema
admits for the tag instead, as
[`discriminator-inference`](#discriminator-inference) reads a subtype's, would not do: where the
union's variants pin nothing it admits its name, which still routes to no variant. At
`cloudflare/api-schemas` @`e411be7e` three schemas compose such a union, every branch of which holds
the tag to values other than the composing schema's name. A subtype that is no component schema —
inline as a property or a union branch, or under an extension — has no name to answer to, so only a
mapping entry tags it, and a chain with no discriminated ancestor carries no tag however deep. A
subtype a discriminator routes to is never distributed across a union written beside its `allOf` or
on one of its branches ([`union-beside-structure`](#union-beside-structure)), since a union would be
no subtype, so no variant of a distributed union carries a tag.

- Cases: `discriminator-transitive`, `discriminator-inline-subtype`, `oneof-discriminated`
- Tests: `schema.TestAllOf_DiscriminatorSubtypeValue`,
  `schema.TestAllOf_DiscriminatorValueFromNearestAncestor`,
  `schema.TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue`,
  `schema.TestAllOf_ExtensionSubtypeIsTaggedOnlyByAMapping`,
  `schema.TestDiscriminatorValue_TheImplicitMappingRoutesWhatAUnionLists`,
  `schema.TestRoutesByName_ReadsAUnionsListOnce`,
  `schema.TestDiscriminatorValue_AUnionListsTheSchemaItsBranchEndsOn`

### `discriminator-inference`

The discriminator-inference policy reads a union's tags off what its variants pin, and a subtype's
off what it admits: the Go option `Options.DiscriminatorInference`, whose zero value is on and whose
`Disabled` reads only what the document declares, and the text option `discriminator-inference`,
which `false` turns off (invariant 6). A variant pins a property where the declarations its
flattening reads of it (`ir.FlattenedProperties`), the one the member takes and those it overrides,
hold it to one string, as an instance meets them all: one is a reference to a string `Literal`, or
to a closed `Enum` of one string member, read through alias scalars, none pins another value, and
not every one admits null. So a body beside a union that redeclares the tag as a string, which its
composed variants take, leaves each branch's pin in force
([`union-beside-structure`](#union-beside-structure)). A number, a boolean, or a value set of more
than one pins nothing a `Mapping` holds, since a discriminator's tag is the wire string. The reading
waits for every node to be interned (`compile.Settlement`), since a variant lowered after its union
has no node while the union lowers, and what it finds no other settlement changes, whichever settles
first. Two unions and a subtype meet it:

1. A `discriminator` declaring no `mapping` routes each variant by the name of the component its
   `$ref` branch names, OpenAPI's implicit mapping, which reaches no inline variant and names a tag
   a variant pinning another value rejects. Where every variant pins the `propertyName` to a
   distinct string and some variant's is not that name, the `Mapping` is read off the pinned
   values, the union's `Provenance.Inferred` names `discriminator-inference`, and an info
   `openapi/inferred-discriminator` at the discriminator names the first variants it reads
   otherwise. `Discriminator.Inferred` stays false: the tag property is declared.
2. A closed union of two or more variants declaring no discriminator, no variant admitting null,
   gets one where every variant requires a property and pins it to a distinct string: the first
   such property in the first variant's layout order is its `PropertyName`, each pinned value maps
   to its variant, and the union is `WireTagged`, `Discriminator.Inferred` set and
   `Provenance.Inferred` naming `discriminator-inference`. Each variant requiring a value no other
   admits, a payload names its variant as validation finds it, departing from nothing the document
   says, so nothing is reported. An open union may meet a variant carrying any tag
   ([unions](../ir/unions.md#open-unions)), and is left alone.
3. A subtype a discriminator routes by its name ([`discriminator-value`](#discriminator-value))
   carries that name, which validation rejects where the subtype admits only other strings for the
   `propertyName`. A declaration admits a string `Literal`'s one string, or the members of a closed
   `Enum` every one of which is a string, read through alias scalars, and any string otherwise,
   null being no tag; the subtype admits what every declaration its flattening reads of the member
   admits. Where that rules the name out, the subtype carries the one string it admits, or,
   admitting several or none, no tag, since none routes to it; its `Provenance.Inferred` names
   `discriminator-inference`, and an info `openapi/inferred-discriminator` at it names what it
   admits. Each subtype is read alone, so two admitting one string both carry it, as their instances
   do ([models](../ir/models.md#discriminator)).

- Cases: `discriminator-inferred`
- Codes: `openapi/inferred-discriminator`
- Tests: `schema.TestDiscriminatorInference_MapsAnUnmappedDiscriminator`,
  `schema.TestDiscriminatorInference_LeavesADeclaredMapping`,
  `schema.TestDiscriminatorInference_NamesAFewDisagreements`,
  `schema.TestDiscriminatorInference_InfersAnUndeclaredDiscriminator`,
  `schema.TestDiscriminatorInference_LeavesALoneOrNullableVariant`,
  `schema.TestDiscriminatorInference_DisabledReadsNothing`,
  `schema.TestDiscriminatorInference_ReadsADistributedUnion`,
  `schema.TestUnionTags_ReadsOnlyWhatItWasDeferredFor`, `schema.TestPinnedValue_ReadsOneStringOnly`,
  `schema.TestPinnedMember_ReadsEveryDeclaration`,
  `schema.TestDiscriminatorInference_ReadsTheTagASubtypeAdmits`,
  `schema.TestSubtypeTagReading_ReportsWhatItCannotSettle`,
  `schema.TestTypeStrings_ReadsAClosedSetOfStrings`,
  `schema.TestDiscriminatorInference_JoinsTheMarksANodeCarries`
- Status: partly built: what a union a subtype composes admits is not read, so a name only it rules
  out stands (#1712), and a declaration writing its strings as a union of values, `oneOf` or
  `anyOf` of `const` branches, is read as admitting any, for a variant as for a subtype (#1715).

### `string-const-union`

A `oneOf` or `anyOf` whose every branch is a string `const`, a one-member string `enum` or such a
union is a closed set of strings in another spelling. The compiler lowers it as any union
([`union`](#union)), each branch to its `Literal` or `Enum` holding what the branch says of its
value, and the engine's `const-unions` pass, not the compiler, normalizes it to the closed `Enum` of
those values at the union's own ID, so an emitter meets one shape however the document wrote the
set. A branch that is such a union is read as the `Enum` it becomes, in the same run, so no second
run finds one. Each member is identified and named as the same value in an `enum` is
([`enum-const`](#enum-const)), in branch order, and holds what its branch says of its value: its
docs, deprecation, availability, examples and kept constructs. A branch's type nothing else
references goes with the union.

The union stays where an `Enum` could not say all it does: a branch naming a component, a type of
its own that an `Enum` member is not; a branch holding several values, a set of its own that the
`Enum` would copy into each union naming it, which a document can make grow with the square of its
size; a value two branches hold, which a `oneOf` rejects and whose two branches may say two things
of it; values typed by two primitives; a nullable branch; a construct a branch keeps verbatim for
any reason but being a vendor extension, such as a constraint its `Literal` has no home for, which
narrows the branch and would not narrow a member; a branch whose `Provenance.Inferred` names a
heuristic, whose mark no member could carry; and one fact two places say of one value. A union of
numbers, or of values of several kinds, is outside this rule and stays a union.

- Cases: `string-const-union`
- Tests: `e2e.TestStringConstUnion_NormalizesToItsEnumSpelling`

### `enum-const`

An `enum` lowers to a closed `Enum` at any member count, one member included, each entry a member
holding its value ([`values`](#values)) in source order ([enums](../ir/enums.md)); a `null`
entry beside others is no member but makes the position nullable ([`nullable`](#nullable)). Its
`ValueType` is the primitive one declared `type` and its `format` select, as a scalar's are read
([`string-formats`](#string-formats), [`numeric-formats`](#numeric-formats)), where every member is
a value of that primitive. Otherwise it is the primitive of the `type` alone, else of the members,
and a `format` that reaches no `ValueType`, whether the table lacks the pairing or a member does not
fit it, as none is a value of `binary`'s `bytes`, is kept verbatim under `openapi:format`
(`degraded_lowering`), with an info `openapi/degraded-construct` at the schema, but for `password`,
which is a redaction ([`password`](#password)). An enumeration an `Enum` cannot hold — a list or
object member, members of different kinds, or only `null` — lowers to a `Union` of `Literal`s
instead, reported as an info `openapi/degraded-construct` at the schema, and a member whose value
cannot convert makes the enumeration such a union, its own variant the top type, reported as an
`openapi/degraded-construct` warning at the member. That union is exclusive unless an instance can
match two variants: an entry repeats, or one lowered to the top type. `enum: []` lowers to a closed
`Enum` with no members, a position accepting no value, reported as an `openapi/empty-enum` warning.
An entry repeating an earlier entry's value, compared as JSON Schema compares instances, is kept,
each occurrence a member of its own ([enums](../ir/enums.md)), and reported as an
`openapi/duplicate-enum-value` warning at the repeat. An `enum` past `Limits.MaxEnumMembers` lowers
to the top type, with an `openapi/budget-exceeded` error at the schema. A `const` lowers to a
`Literal` of its value, whose `ValueType` is read as an enum's is, from the type and format the
value fits, else the type, else the value's own kind ([containers](../ir/containers.md#literals)),
so a `type` or `format` the value fits is held and draws nothing, and one it does not fit is kept
verbatim with the same info report. A `const` that cannot convert lowers to the top type, with an
`openapi/degraded-construct` warning at the schema. A value set an `allOf` refines is typed by every
conjunct ([`allof-refinement`](#allof-refinement)). A
`$ref` into a member of such a union names the member's `Literal`, the node the enumeration lowers
at that position, whichever is lowered first.

- Cases: `enum-string`, `enum-numeric`, `empty-enum`, `enum-member-identity`, `nullable-enum-31`,
  `literal-const`
- Codes: `openapi/empty-enum`, `openapi/duplicate-enum-value`, `openapi/degraded-construct`,
  `openapi/budget-exceeded`
- Tests: `schema.TestEnum_HeterogeneousBecomesUnionWithBadValue`,
  `schema.TestEnum_NonScalarAndMidListMismatch`,
  `schema.TestEnum_ValueTypeReadsTheFormatItsMembersFit`,
  `schema.TestLiteral_ValueTypeReadsTheTypeAndFormatItsValueFits`,
  `schema.TestEnum_ReportsWhyAFormatStayedVerbatim`,
  `schema.TestEnum_NarrowsARedeclaredPrimitiveInEitherOrder`,
  `schema.TestEnumAsUnion_UnconvertibleMemberBecomesAny`,
  `schema.TestEnumAsUnion_ExclusiveWhereNoInstanceMatchesTwoEntries`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`
- Status: partly built: an enum whose members do not match its declared type lowers to a union with
  only an info diagnostic (#619). Open: whether a mixed-type enum's `null` lifts out of its union
  (#618).

## 6. Schemas: objects, arrays and scalars

### `additional-properties`

`additionalProperties: false` lowers to `Model.Additional` closed, and `additionalProperties: true`
or a schema to `Model.AdditionalProps`, whose `Value` types every key no property names. An object
whose one member fact is that catch-all lowers to a `MapT` instead
([containers](../ir/containers.md)). `unevaluatedProperties: false` lowers to `Additional`
closed after composition. `minProperties` and `maxProperties` lower to the `Model`'s `Constraints`,
which bound the property set's cardinality as against `Additional`'s openness, or to a `MapT`'s,
which bound its entry count.

`true` writes a catch-all; an absent keyword does not. In JSON Schema, `additionalProperties: true`
is the empty schema, so it types the catch-all as the top type exactly as `{}` does: alone it is a
`MapT` of the top type, and beside properties an `AdditionalProps` of the top type. An absent
`additionalProperties` states nothing, so `{type: object}` stays a `Model` with no properties and
`Additional` unspecified; whether such an object is a free-form map is a target's policy, not a fact
the source declared. Beside `patternProperties` the catch-all's `Value` types a key no pattern
matches ([catch-alls](../ir/models.md#catch-alls)): absent, such a key is unconstrained, so it is
the top type, and the IR does not tell an absent `additionalProperties` from `true`; `false` admits
no such key, so the catch-all holds the patterns and no `Value` beside `Additional` closed. That
object stays a `Model`, since a `MapT` has no field for a pattern-scoped value.

- Cases: `maps`, `constraints`, `allof-position-constraints`
- Tests: `schema.TestObject_CatchAllBesideAnotherFactStaysModel`,
  `schema.TestObject_MapAndNothingElseLowersToMapT`,
  `schema.TestModel_UnevaluatedPropertiesClosedAfterComposition`,
  `schema.TestAllOf_AClosureAfterCompositionKeepsTheCatchAll`
- Status: partly built: an `unevaluatedProperties: false` that a catch-all beside it makes inert
  in JSON Schema still lowers to a closure, which keeps the object a `Model`, and one beside
  `patternProperties` alone leaves the catch-all typing a key no pattern matches as the top type,
  where JSON Schema admits such a key only if composition evaluates it (#862).

### `required-members`

Each name a `required` list writes lands on the object it is written for: as `Property.Required`
([properties](../ir/properties.md)) on the `Model`'s own property of that wire name, which is
never cleared once set, or else in `Model.Requires`
([models](../ir/models.md#required-members)), once, in the order the lists name it, an
`allOf`'s branches before the schema's own. A name in `Requires` is one a `Base` or `Mixin`
declares, which the object makes required (`allOf: [{$ref: User}, {required: [id]}]`), or one only
its catch-all governs, which JSON Schema requires whether or not a schema declares it. The rule is
one for every way an object lowers — a plain object, an `allOf`
([`allof-required`](#allof-required)), the object variant of a multi-typed schema, a distributed
union's variant — and reports nothing, since nothing is lost. An object writing a catch-all beside a
name its properties do not declare is a `Model`, not a `MapT`, since a map requires no key, and
stays one when a discriminator moves off it. A `required` beside a `$ref` is kept on the alias the
`$ref` site hoists ([`shape-applicator`](#shape-applicator)), as any keyword is there that composes
the referent.

- Cases: `allof-required-only`, `required-members`
- Tests: `schema.TestModel_PlainRequiredUndeclaredPropertyLandsInRequires`,
  `schema.TestObject_ACatchAllBesideARequiredNameStaysAModel`,
  `schema.TestDiscriminatorBesideUnion_ARequiredNameKeepsTheBodyAModel`,
  `schema.TestAllOf_RequiresIsTheSameInEitherComponentOrder`,
  `schema.TestAllOf_ARequiredNameWrittenInSeveralListsIsListedOnce`,
  `schema.TestDistributedUnion_EachVariantCarriesTheRequiresOfItsBody`
- Status: partly built: a `required` beside a 3.1 `$ref` stays verbatim on the alias that `$ref`
  hoists, with every other structural keyword written there (#1067).

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
A schema writing a `$ref` is held to the rule by the keywords beside it, which the lowering applies
([`later-version-construct`](#later-version-construct)), where nothing they conjoin with writes an
`items` either: beside a `$ref` landing on a schema that writes one, through an alias or an `allOf`
as well, a `type: array` leaves the elements to it, and `{$ref: X, type: array}` is then as silent as
`{allOf: [{$ref: X}, {type: array}]}`. An `items` written in a union's branches does not count, nor
does `items: true`, which holds the elements to nothing. An inline `allOf` branch, which merges in
place or restates what a refinement names, is not reported, nor is a subschema kept verbatim, such as
one under `not`; the branch a refinement lowers from is reported as the position it is
([`allof-refinement`](#allof-refinement)).

- Cases: `missing-array-items`
- Codes: `openapi/missing-array-items`
- Tests: `schema.TestLower_MissingArrayItems`, `schema.TestLower_ListWithoutItems`,
  `schema.TestLower_MissingArrayItemsAtAReferencedPositionWarnsOnce`,
  `schema.TestRefinement_MissingItemsIsAskedOfTheBranchThatLowers`

### `tuples`

`prefixItems` lowers to a `Tuple`'s `Elems`, one per position, and `minItems`, `maxItems` and
`uniqueItems` beside it to the `Tuple`'s `Constraints`. Its `Rest` is read from the `items` beside
`prefixItems`, which 2020-12 applies to every element past them: `items: false` leaves no `Rest`, an
absent `items` reads as `true`, the top type, and either of those or a schema lowers exactly as a
`List`'s `items` would, at the same pointer and under the same hint. An `unevaluatedItems` beside
them stays the verbatim entry [`validation-only`](#validation-only) keeps, as it does beside a
`List`, reported as an info `openapi/validation-only-keyword`.

JSON Schema drafts 4, 6 and 7 and 2019-09 write a tuple as `items` holding a list, its rest as the
`additionalItems` beside it. In a schema read in one of them
([`dialect-keywords`](#dialect-keywords)) the list lowers to a `Tuple`'s `Elems`, one per entry,
each a schema whose `$ref` is bound, and `additionalItems` to its `Rest` as 2020-12's `items` does
beside `prefixItems`: `false` leaves none, and an absent one reads as `true`. Each entry is named as
a `prefixItems` entry is, and the rest as an `items` schema is. Each is judged as its 2020-12 twin
is ([`validation`](#validation)), whether or not a `$ref` reaches it: an entry no schema spells,
such as `5` or `null`, is an `openapi/validation` error at it, and a rest no schema spells an error
at it, or for `null` the warning a `null` field draws, read as left out, as `items` is beside
`prefixItems`. Each lowers as its twin does: `5` as `false` and `null` as
[`null-schema`](#null-schema) reads it. A type list's array variant is the tuple the schema writes,
which leaves nothing on the union. Any other position that lowers to no `Tuple`, such as one with no
`type: array`, keeps the list and the `additionalItems` beside it as
[`shape-applicator`](#shape-applicator) keeps `items` and `prefixItems`. Beside an `items` that is no
list, `additionalItems` is the rest of no tuple, kept verbatim with an info
`openapi/unknown-schema-keyword`.

In any other dialect a list of items is no schema: the load phase reports it as an
`openapi/invalid-items-list` error, at every schema the [`validation`](#validation) check reads, and
the lowering reads `items` as absent; an `additionalItems` beside it is kept as at a position
lowering to no `Tuple`.

- Cases: `tuples-prefixitems`, `schema-drafts`
- Codes: `openapi/validation-only-keyword`, `openapi/invalid-items-list`,
  `openapi/unknown-schema-keyword`, `openapi/validation`
- Tests: `schema.TestLower_TupleRest`, `e2e.TestOlderDrafts_AListOfItemsIsATuple`,
  `e2e.TestOlderDrafts_AMalformedTupleIsReportedAsItsTwinIs`,
  `e2e.TestOlderDrafts_ATypeListsArrayVariantIsTheTupleWritten`,
  `schema.TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise`,
  `bind.TestTuples_AListOfItemsHoldsSchemas`,
  `load.TestOwnedFinding_JudgesEachValueInItsSchemasDialect`, `load.TestBuiltTargets`,
  `oas.TestSchemaRules_TupleRestFindingJudgesTheRestBesideAList`
- Status: partly built: `items: true` and an absent `items` lower alike, though only the first makes
  an `unevaluatedItems` beside them inert, and that one is still kept as live (#862).

### `shape-applicator`

`properties`, `patternProperties`, `additionalProperties`, `required`, `items` and `prefixItems`,
and the `additionalItems` written beside a list of `items` ([`tuples`](#tuples)), each
constrain an instance whether or not a `type` is written beside them, and each has exactly one IR
home: a `Model`'s property set, openness and pattern bindings, or a `List`'s element and a `Tuple`'s
positions and rest. A `MapT` homes an object's catch-all and the empty `properties`,
`patternProperties` and `required` lists that left it a map
([containers](../ir/containers.md)), and a `required` naming a key keeps it a `Model`
([`required-members`](#required-members)). A position whose lowering produced none of these — an
`Enum`, a `Literal`, a `Scalar`, or the top type — has nowhere to put them, and so does a position
whose lowering produced the other of the two shapes (`{type: object, properties: {…}, items: {…}}`
lowers to a `Model`, which holds no element type). This covers both a contradictory schema, where
one half is taken and the other must stay recoverable
(`{type: string, enum: [a, b], properties: {f: …}}` is not satisfiable, so lowering to the enum is a
reasonable degradation), and an applicator written with no type at all, where the position lowers to
the top type, which says strictly less than the applicator does (`{items: {type: string}}`
constrains every array instance). The IR could model the untyped case by reading the applicator as
the shape it implies — an untyped `items` as a list — but that is a lowering decision this document
has not taken, and keeping the source leaves it open.

Each unhomed keyword is kept verbatim in `Unmodeled["openapi:<keyword>"]` under `degraded_lowering`,
reported as one info `openapi/degraded-construct` at the position naming the kind it lowered to and
the keywords it cannot hold. Which keyword has a home is asked of the node the lowering produced,
never re-derived from the dispatch that produced it. A position that reduced to a shared node gets
an alias of its own first: a shared primitive must never carry one declaration's keywords. The
`format` keyword joins the list wherever the lowered node does not pair it with a primitive — no
`type` declared, or an object, an array or an enum — since it is homed by pairing with a type rather
than by a node kind, and `password` is never unhomed ([`password`](#password)). The census reads
`type`, `const`, `enum`, `minItems`, `maxItems` and `uniqueItems` beside these, so a collection
bound on an object, a scalar or a position with no type is kept the same way, and a keyword the
elected lowering of [`competing-keywords`](#competing-keywords) never reads is too. `discriminator`
joins the list, homed by a `Model` or `Union` that routes by it and by nothing else, so one beside
any other node — the top type, an enum, a literal, a scalar, an alias over a `$ref` — is kept the
same way. At a
`$ref` position the census runs on the alias the use-site rule of
[models](../ir/models.md#composition-entries) hoists, since the siblings bind the position rather
than the referent. Every outcome of a schema runs it, and each says what it lowered to. A union
holds what a node of its kind holds. A collapsed union and an expanded `$dynamicRef` lower to a
reference, so they are read as a `$ref` position is, and an `allOf` that refines takes the same
census, but for what its principal already says ([`allof-refinement`](#allof-refinement)). A
multi-typed schema (`type: [string, object]`) and a union distributed over its branches are built
from the one schema per variant, so the census asks the variants, and a keyword is homed where any
holds it: a distributed union's `properties` are consumed, and a `format` or `maxItems` beside them
is kept on the union, under a report naming a union of models.

- Cases: `unhomed-keywords`, `empty-enum`, `keywords-at-every-outcome`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestObject_ACatchAllBesideARequiredNameStaysAModel`,
  `schema.TestUnhomedApplicator_KeptOnAListAndAnnounced`,
  `schema.TestUnhomedApplicator_FormatWithNoTypeIsKept`,
  `schema.TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise`
- Status: partly built. Open: whether `format: set` on an array reads as `uniqueItems` (#638).

### `string-formats`

A `type: string` with a `format` the table names lowers to the shared primitive the pairing selects,
and no node of its own: `date` to `date`, `time-local` to `time`, `duration` to `duration`, `uuid`
to `uuid`, `uri` to `url`, `date-time` to `datetime_offset`, `date-time-local` to `datetime`,
`binary` to `bytes`. `byte` lowers to a hoisted `Scalar` over `bytes` whose `Encoding` is `base64`,
with the `string` it travels as in `WireType`. Any other format (`time`, `email`, `uri-reference`,
`ipv4`, an invented one) lowers to a hoisted `Scalar` over `string` whose `Encoding.Name` is the
format as written, case kept, since the compiler knows no registry of formats beyond the table, and
the table leaves out `time`, RFC 3339's `full-time`, because it carries an offset and the `time`
primitive holds none ([primitives](../ir/primitives.md#offsets)). A `contentEncoding` beside a
`byte` or unknown format names the encoding instead; where the two name different encodings the
format is kept verbatim under `openapi:format` (`no_ir_home`) with an info
`openapi/degraded-construct` at it. A scalar hoisted for its format carries the bounds written
beside it. The table is the same at every minor, and a pairing of `format` with another `type` is
[`numeric-formats`](#numeric-formats)'.

- Cases: `encoding-byte`, `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_UnknownFormatPerBaseType`,
  `schema.TestLower_NamedScalarComponentResolves`,
  `spelling.TestFormatTable_EveryRowDistinguishesTheBareType`,
  `spelling.TestFormatTable_EveryStringRowLowersToItsPrimitive`
- Status: partly built: a `byte` string and a string-carried number put the logical and the wire
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
property, header or parameter that owns none, as `Secret`, inherited from a `$ref` target as
`deprecated` is. An inline `allOf` branch writing it folds as any annotation does
([`allof-inline-residue`](#allof-inline-residue)).

- Cases: `secret-format`, `scalar-format`
- Tests: `operation.TestParams_SchemaRedactionReachesTheParameter`,
  `operation.TestParams_SchemaRedactionStaysOnANode`

### `whole-schema-fallbacks`

Two whole-schema fallbacks answer to a compiler's own limits rather than to anything about the
construct, which is why the list of degradations in
[boundaries](../ir/boundaries.md#degraded-constructs) does not hold them.
A schema nested past the lowering's depth cap becomes the top type,
reported as an `openapi/degraded-construct` error at it; the cap counts from where the position's
own declaration enters, or from the top level for a body only a `$ref` reaches, never from the depth
of a `$ref` naming it. A boolean `false` schema becomes a closed empty `Model`, with the schema
itself kept verbatim under `degraded_lowering` at `Unmodeled["openapi:schema"]` and an info
`openapi/false-schema` at it — the key names the position because a boolean schema writes no keyword
to name it by, and nothing can collide with it, since such a schema has nothing else to preserve. A
boolean `true` schema is the top type, as `{}` is, and reports nothing. A `$ref` naming a boolean
schema below a component takes what that position lowers to, whichever is declared first: the closed
`Model` at its pointer, or an alias there of the top type. A position nothing else lowers, such as a
closed tuple's `items: false`, is lowered for the `$ref` alone. The parser decodes as `false` a
schema the document writes as a mapping where a keyword beneath it fails to decode under its own
name, such as an `items` above a JSON Schema draft's list of `items`
([`failure-beneath-itself`](../parser-library.md#failure-beneath-itself)). The load phase decodes
each such schema again as written, and in turn each one that leaves `false` below it, up to
`typed.MaxMends` of them one below another; one past them becomes the top type, reported as an
`openapi/degraded-construct` error at it, since it is no `false` the document writes.

That preservation is load-bearing, not decorative: a closed empty `Model` admits `{}`, and the
source admits nothing, so the lowered type is one value wider than what was written. Without the
verbatim entry, `false` and `{type: object, additionalProperties: false}` are the same node, and
nothing downstream can tell "no instance" from "the empty object". The diagnostic says which one it
was, but a diagnostic is not part of the document.

The approximation is preferred to an exactly empty shape, and the reason is worth stating because
the IR does have one. A closed `Enum` with no members is an exactly empty value space
([enums](../ir/enums.md)), but `Enum.ValueType` is a `PrimKind` and a `false` schema declares
no type, so lowering to one means inventing a value type the source never wrote — an exact-looking
node resting on a fabricated field, which is not more faithful than an approximate one with the
source beside it. It would also split the rule: the composition case
([`allof-false-branch`](#allof-false-branch)) must stay a `Model`, because it keeps what the other
branches contributed, so one source construct would lower to two different kinds depending on where
it appeared. The overstatement is bounded, recorded, and recoverable from the `Unmodeled` entry; an
emitter that wants the exact reading has everything it needs to produce it.

- Cases: `allof-boolean-branch`
- Codes: `openapi/degraded-construct`, `openapi/false-schema`
- Tests: `schema.TestLower_DepthCapExceeded`, `schema.TestLower_ASchemaPastTheMendsIsTheTopType`,
  `e2e.TestOlderDrafts_ATupleBelowAnotherSchemaIsATuple`,
  `e2e.TestCompile_ANullBeneathItsOwnNameLowersAsTheKeywordLeftOut`,
  `e2e.TestMaxNesting_AdmitsWhatTheLoweringLowersInFull`,
  `e2e.TestCompile_ARefToABooleanSchemaResolvesInEitherOrder`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`

### `null-schema`

A `null` written where a schema belongs states nothing about the values there, so it is read as no
schema written, never as the `false` schema the parser decodes it as
([`null-schema-is-false`](../parser-library.md#null-schema-is-false)), which admits no value. An
entry, which exists whatever its value — a property, a composition branch, a component, a
`prefixItems` position, a parameter's, header's or media type's `schema` — lowers as the empty
schema does: to the top type, or as an `allOf` branch to nothing composed, and a `$ref` naming it as
one naming `{}` does. A keyword whose value is a schema, whose absence states something its empty
schema may not, is read as left out: `additionalProperties: null` writes no catch-all,
`contentSchema: null` no `Encoding.Schema`, and a `null` `not`, `if`, `then`, `else`, `contains`,
`propertyNames`, `unevaluatedItems` or `unevaluatedProperties` is kept nowhere. `items: null` lowers
as an absent `items` does, and a `null` `items` or `additionalProperties` a node has no home for is
not kept for want of one ([`shape-applicator`](#shape-applicator)). Nothing is kept verbatim, since
a `null` means nothing a consumer could recover, and the lowering reports nothing of its own, since
reading it so loses nothing: the load phase reports each such `null` at the node
([`validation`](#validation)), an entry's as an error and a keyword's as the warning a field left
out draws, but for `items` beside a 3.0 `type: array`, whose absence
[`missing-array-items`](#missing-array-items) reports instead. One inside a value the IR keeps
verbatim whole, a `dependentSchemas` entry, stays in that value, since nothing reads it as a schema.

- Tests: `e2e.TestCompile_ANullWhereASchemaBelongsIsNoSchema`,
  `schema.TestNullSchema_AnEntryIsTheEmptySchemaAndAKeywordIsLeftOut`,
  `annotation.TestIsNullLiteral_TellsANullFromFalse`

### `null-keywords`

A `null` written for a keyword of a Schema Object is read as that keyword left out, whatever the
keyword and whatever node the schema lowers to, as [`null-schema`](#null-schema) reads one whose
value is a schema: no keyword admits it, so it states nothing, and the schema lowers exactly as it
does with the key removed. A `null` bound draws no `openapi/invalid-numeric-literal`, `type: null`
declares no type, unlike `type: "null"`, and nothing is kept verbatim for want of a home. `const`,
`default` and `example` hold an instance, of which `null` is one, so a `null` there is that value
([`enum-const`](#enum-const), [`nullable`](#nullable)), as an `enum` member is. A keyword of no
vocabulary has no type to break and is kept with the `null` it wrote, as
[`unknown-keys`](#unknown-keys) keeps any value. The load phase reports each such `null` at the node
as the warning a field left out draws ([`validation`](#validation)); the lowering reports nothing
of its own, since reading it so loses nothing.

- Cases: `null-keywords`
- Codes: `openapi/invalid-numeric-literal`
- Tests: `e2e.TestCompile_ANullKeywordLowersAsTheKeywordLeftOut`,
  `e2e.TestCompile_ANullIdentifierLowersAsTheKeywordLeftOut`,
  `annotation.TestRawPropertyNode_ReadsANullKeywordAsLeftOut`,
  `bind.TestDynamicAnchors_ANullAnchorNamesNothing`,
  `oas.TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional`,
  `e2e.TestCompile_ANullBeneathItsOwnNameLowersAsTheKeywordLeftOut`

## 7. Schemas: keywords beyond the shape

### `constraints`

`minimum`, `maximum` and `multipleOf` lower to `Min`, `Max` and `MultipleOf`, and `minLength`,
`maxLength` and `pattern` to `MinLength`, `MaxLength` and `Pattern` as written, in the `Constraints`
of the node or carrier the position owns (a `Scalar`, a `Property` or a `Parameter`); `minItems`,
`maxItems` and `uniqueItems` to the `List`'s, and a `Tuple`'s per [`tuples`](#tuples), which its
carrier does not restate, or, where the position lowers to a reference (a `$ref` site, a collapsed
union or an expanded `$dynamicRef`), to the `Constraints` of the alias or carrier it owns, whatever
the target is, as does one on an inline `allOf` branch
([`allof-inline-residue`](#allof-inline-residue)); and
`minProperties` and `maxProperties` per [`additional-properties`](#additional-properties). A bound
is read off the raw node, never the parser's `float64`, so its `BigVal` keeps every digit the
literal wrote ([`values`](#values)), a quoted one as the decimal it spells; one that is no decimal
literal a `BigVal` holds is dropped with an `openapi/invalid-numeric-literal` error at the keyword.
Exclusive bounds follow the schema's dialect
([`dialect-keywords`](#dialect-keywords)). In 3.0's, and in JSON Schema draft 4's, the boolean
modifies the bound beside it: `true` moves that bound to `ExclusiveMin` or `ExclusiveMax` and clears
`Min` or `Max`, so a 3.0 document and its 3.1 translation lower alike, `false` states nothing, and
`true` with no bound beside it bounds nothing and is kept verbatim under `openapi:exclusiveMinimum`
or `openapi:exclusiveMaximum` (`degraded_lowering`) with an `openapi/degraded-construct` warning. In
2020-12's, the dialect 3.1 and 3.2 name, the keyword is a bound of its own, kept beside `Min`/`Max`
when both are declared, a field apiece and no `Unmodeled` entry
([constraints](../ir/constraints.md)). The other dialect's form (a boolean in 2020-12's, a number in
3.0's or draft 4's) is dropped with an `openapi/invalid-exclusive-bound` error at the keyword. Both
errors are the load phase's, once each schema's dialect is known, at every schema the
[`validation`](#validation) check reads, which the rule table leaves these keywords' shape to: one
under `not`, `if` or `propertyNames`, or beside an `enum`, is reported as one the lowering reads,
and an aliased value once. A bound beside a
node with no `Constraints` field to take it (an `Enum`, a `Literal`, a `List` for a value bound) is
kept verbatim under `openapi:<keyword>`
(`degraded_lowering`) with an info `openapi/degraded-construct` naming the node it lowered to, as
[`shape-applicator`](#shape-applicator) keeps a collection bound on an object. A collection bound
beside a reference that `Constraints` cannot state is kept the same way: a `uniqueItems` of `false`,
which a bool cannot tell from one never written, and a count that does not fit in 64 bits.

- Cases: `constraints`, `numeric-precision`, `ref-collection-bounds`
- Codes: `openapi/invalid-numeric-literal`, `openapi/invalid-exclusive-bound`,
  `openapi/degraded-construct`
- Tests: `e2e.TestOwnedShapes_AreReportedWhereverTheCheckReadsASchema`,
  `load.TestOwnedFinding_JudgesEachValueInItsSchemasDialect`,
  `load.TestOwnedFindings_ReportAnAliasedValueOnce`,
  `schema.TestConstraints_ExclusiveBoolean30`,
  `schema.TestConstraints_ExclusiveWrongDialectForm`,
  `schema.TestConstraints_LosslessNumericLiterals`,
  `schema.TestConstraints_ABoundPastTheExponentRangeIsRefused`,
  `schema.TestCollectionBound_BesideAReferenceReachesTheAlias`,
  `schema.TestCollectionBound_BesideAReferenceReachesTheCarrier`,
  `schema.TestCollectionBound_AReferenceKeepsWhatConstraintsCannotState`,
  `schema.TestCollectionBound_ACarrierLeavesItToTheNodeItsPositionOwns`,
  `schema.TestCollectionBound_ANodeOfAnotherShapeKeepsItVerbatim`,
  `schema.TestCollectionBound_ReadsThePositionNotItsTarget`,
  `schema.TestCollectionBound_BesideAnExpandedDynamicRefReachesWhatThePositionOwns`,
  `operation.TestParams_CollectionBoundBesideARefReachesTheCarrier`,
  `schema.TestCollectionBound_ARefinementReachesWhatThePositionOwns`,
  `schema.TestRefinement_ACollectionBoundLandsAtEveryPosition`
- Status: partly built: an integer bound past what a 64-bit integer holds lowers as `0`, unreported,
  everywhere but the collection bounds beside a reference or on an inline `allOf` branch (#1050).

### `validation-only`

JSON Schema's validation-only keywords are kept verbatim under `validation_only`
([boundaries](../ir/boundaries.md#validation-only-constructs)) on the node or carrier the
position owns, one entry per family: `not`, `dependentSchemas`, `dependentRequired` and
`propertyNames` each under `openapi:<keyword>`, at the keyword; and three families each folded into
one object, at the schema — `if`, `then` and `else` under `openapi:if-then-else`, `contains` with
`minContains` and `maxContains` under `openapi:contains`, and `unevaluatedItems` with an
`unevaluatedProperties` that is not `false` under `openapi:unevaluated`, the `false` one being a
closure ([`additional-properties`](#additional-properties)). In a schema read in JSON Schema draft
4, 6 or 7 ([`dialect-keywords`](#dialect-keywords)), `dependencies`, which 2019-09 split into
`dependentSchemas` and `dependentRequired`, is kept the same way under `openapi:dependencies`, and
elsewhere is an unknown keyword ([`unknown-keys`](#unknown-keys)). Each entry is reported as an
info `openapi/validation-only-keyword` at the schema that wrote it.

- Cases: `negation-not`, `dependent-required`, `schema-drafts`
- Codes: `openapi/validation-only-keyword`
- Tests: `annotation.TestValidationOnlyAt_KeepsEveryKeywordItClaims`,
  `e2e.TestOlderDrafts_DependenciesAreValidationOnly`,
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
when the name is declared more than once in its document (the target is back under the evaluation
path's control), when it is unresolvable, names another document, or is not a plain fragment, when
its value is not a string at all, when a resource boundary stands between it and the anchor as
above, when expanding it would close a cycle of aliases (a `$ref` cycle is refused outright for the
same reason, and expansion must not mint one the refusal cannot see), or when it is co-declared with
a `$ref`, a `oneOf`/`anyOf`, a shape of its own, or one of `items`/`prefixItems`/`required` — an
expansion takes its target whole, so it has nowhere to put a sibling applicator. One further case is
irreducible in the OpenAPI compiler specifically: in the source, an anchor declared anywhere but the
top level of `components/schemas` has no ID that is stable without consulting the registry, and
resolving it would make the reference depend on which position happened to lower first
([models](../ir/models.md#composition-beside-a-union)), so it is preserved instead.

A `$dynamicRef` is read against the anchors of the document it is written in, by this same rule, so
one in another document expands to the type lowered where that document declares its anchor, in that
document's namespace ([identity](../ir/identity.md#across-documents)), never to a source anchor of
that name. There any position may hold the anchor, since a reference hoists what it reaches
([`references`](#references)), and the schema an expansion reaches is read and validated as one a
`$ref` reaches. What a reference resolved to where no position of its document places it reads no
anchors, so a `$dynamicRef` in it is kept.

Anchors are the identifiers [`references`](#references) reads, where it reads them, so one in an
example or an extension declares nothing, and one read through a YAML alias or a merge key is a
declaration where it is placed, so an anchor reached only by an alias still makes its name a
duplicate. A resource boundary is read the same way, so a property named `$id` is none. A document
read short of the whole, by the budget or by a merge past
[`references`](#references)' bound, may hide a second declaration, and a merge past it may hide an
`$id`, so a `$dynamicRef` resting on either is kept, since an undercount is what would turn
"irreducible" into a confident expansion; the warning that reports the bound says so. A keyword
written beside a reference that expands is read as one beside a `$ref` is
([`shape-applicator`](#shape-applicator)).

- Cases: `dynamic-ref`, `keywords-at-every-outcome`
- Codes: `openapi/dynamic-ref-expanded`, `openapi/degraded-construct`
- Tests: `schema.TestDynamicRef_IrreducibleIsKeptAndSaysWhy`,
  `schema.TestDynamicRef_ExpandsAgainstTheDocumentItIsWrittenIn`,
  `bind.TestDynamicAnchors_CountsWhatADocumentDeclares`,
  `bind.TestDynamicAnchors_SaysWhenTheCountProvesNothing`,
  `bind.TestInOwnResource_ReadsTheResourceAPositionIsIn`

### `content-vocabulary`

The content vocabulary is modelled, not preserved. All three keywords share one home, so a position
keeps them all or lowers them all: `contentEncoding` to `Encoding.Name`, `contentMediaType` to
`Encoding.MediaType`, and `contentSchema` to `Encoding.Schema`
([constraints](../ir/constraints.md)) on the scalar the position lowers to. `contentSchema`'s
value is a schema and lowers like one — hoisted at its own source pointer and referenced by ID —
rather than riding along as a payload a consumer would have to re-parse. Where the position lowers
to a shape with no `Encoding` field, all three stay verbatim under `no_ir_home`, each under
`openapi:<keyword>` and each reported as an info `openapi/degraded-construct` at it
([boundaries](../ir/boundaries.md#validation-only-constructs)).

- Cases: `content-vocabulary`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestContentVocabulary_LowersToEncoding`

### `dialect-keywords`

`$id`, `$schema` and `$vocabulary` identify and configure a JSON Schema *resource*. The IR
identifies every type by a synthetic ID derived from its source pointer rather than by `$id`
([identity](../ir/identity.md)), and describes one API surface rather than a resource graph,
so it has no dialect axis and none is coming: each is kept verbatim under `openapi:<keyword>`
(`out_of_scope`), with an info `openapi/degraded-construct` at it. `$id` is read besides, as the
identifier and base URI a `$ref` resolves against where [`references`](#references) reads one, and
it stops a `$dynamicRef` expanding across it ([`dynamic-ref`](#dynamic-ref)); a reference to it
lowers to the schema's type, whose ID its pointer gives, so nothing of the identifier reaches the IR
beyond the verbatim copy. A document's `jsonSchemaDialect`, the default `$schema` of its schemas, is
kept on the Document the same way.

`$schema` and `jsonSchemaDialect` are read besides for the dialect a schema is read in, as OpenAPI
3.1 has them choose it. A schema of a 3.1 or 3.2 document is read in the dialect the `$schema` of
the innermost schema resource root holding it names, itself included: a Schema Object no other
Schema Object holds, the root of a document that is no OpenAPI document, which holds every schema in
it, or a schema whose identifier roots a resource of its own ([`references`](#references)), as JSON
Schema lets an embedded resource name its dialect. Else it is read in its document's
`jsonSchemaDialect`, and else in its minor's own, the OAS dialect. A `$schema` on any other schema
chooses nothing, since JSON Schema writes one at a resource's root, and an `$id` naming its own
document roots no resource. A 3.0 document defines neither keyword, so its schemas are read in 3.0's
dialect whatever either names. The rule table has rows for each dialect named here by the id its
meta-schema declares, with or without a trailing empty fragment. The OAS dialect,
`https://spec.openapis.org/oas/3.1/dialect/base`, and JSON Schema 2020-12,
`https://json-schema.org/draft/2020-12/schema`, read as the minor's own keywords, and the OAS
dialect is also named by each dated id its meta-schema is published under:
`https://spec.openapis.org/oas/3.1/dialect/2024-10-25` and `2024-11-10`, and
`https://spec.openapis.org/oas/3.2/dialect/2025-09-17` and `2026-02-26`, as OpenAPI lets tooling
support values of `$schema` besides. JSON Schema 2019-09,
`https://json-schema.org/draft/2019-09/schema`, draft 7, `http://json-schema.org/draft-07/schema#`,
draft 6, `http://json-schema.org/draft-06/schema#`, and draft 4,
`http://json-schema.org/draft-04/schema#`, have keyword rows of their own. A dialect changes the
keywords alone: the fields OpenAPI adds to a Schema Object, such as `discriminator` and `xml`, and
the rules it states of one stay the minor's. A reading the rows decide follows the schema's: draft 4
spells an exclusive bound as a boolean modifying the bound beside it ([`constraints`](#constraints)),
the drafts before 2020-12 a tuple as a list of `items` ([`tuples`](#tuples)), those before 2019-09
`dependencies` ([`validation-only`](#validation-only)) and an identifier naming a fragment alone
([`references`](#references)), draft 4 the identifier as `id`, and drafts 4, 6 and 7 ignore the
keywords beside a `$ref` ([`later-version-construct`](#later-version-construct)). A schema naming
any other dialect by an absolute URI, an earlier draft or one of the author's, is read in its
minor's own, with an `openapi/unread-dialect` warning at the `$schema` or the `jsonSchemaDialect`
naming it. A value no absolute URI names chooses nothing, as though it were not written, and draws
no such warning: the rule table reports it where it judges it ([`validation`](#validation)), which
in a document a reference reaches is only what the reference reaches
([`openapi-versions`](#openapi-versions)). A schema is read in the dialect of where it is, so a node
an alias places under two roots is read in each one's at each, by the folding of an `allOf` and the
reading of a body as a file too.

`$comment` is dropped outright — 2020-12 §8.3 forbids presenting it to end users, so no emitter may
see it. `$dynamicAnchor` is consumed as a reference target rather than carried as content — it is
what lets a `$dynamicRef` expand ([`dynamic-ref`](#dynamic-ref)), and a `$ref` naming it reads it as
an `$anchor`. `$anchor` is consumed the same way: a `$ref` naming one that
[`references`](#references) reads resolves to the schema declaring it.

- Cases: `dialect-keywords`, `schema-dialects`
- Codes: `openapi/degraded-construct`, `openapi/unread-dialect`
- Tests: `schema.TestDialectKeywords_KeptOutOfScope`, `document.TestMeta_JSONSchemaDialectIsKept`,
  `e2e.TestDocumentDialect_ASchemaIsReadInTheDialectItsRootNames`,
  `e2e.TestDocumentDialect_AParameterReadsItsSchemasDialect`,
  `e2e.TestDocumentDialect_AReferenceReadsWhatItNamesInItsRootsDialect`,
  `e2e.TestDocumentDialect_ADocumentOfSchemasNamesItsDialectAtItsRoot`,
  `e2e.TestDocumentDialect_ADialectWithNoRowsIsReadAsTheMinorsOwn`,
  `e2e.TestDocumentDialect_AValueNamingNoDialectChoosesNothing`,
  `e2e.TestDocumentDialect_ASchemaIsReadInItsOwnDocumentsDialect`,
  `e2e.TestOlderDrafts_AnAliasedNodeIsReadInTheDialectOfEachPlaceByEveryReader`,
  `oas.TestSchemaRules_ADialectNamesItsKeywordsAlone`,
  `bind.TestDialectRoots_ASchemaNoOtherHoldsNamesItsDialect`,
  `load.TestReadDialects_GovernsWhatEachRootHoldsAndWarnsOfWhatItCannotRead`,
  `load.TestReadDialects_GivesAnotherDocumentsOwnRowsToWhatNoRootHolds`
- Status: partly built: the rule table's check holds a schema to its minor's keywords whatever its
  dialect, bar the shapes and formats it leaves to the load phase ([`constraints`](#constraints),
  [`tuples`](#tuples), [`references`](#references)), so a keyword only the draft defines, such as
  `dependencies` or draft 4's `id`, is held to none, as is a `definitions` entry no `$ref` reaches
  (#1466); a keyword 2020-12 added, such as `prefixItems`, is read as
  2020-12 reads it in a draft's schema too (#820); 2019-09's `$recursiveRef` is kept verbatim, never
  expanded as its `$dynamicRef` is (#1475); and an identifier naming its document by the 3.2 `$self`
  roots a resource of its own, as the document is known by its location alone (#863).

### `vocabulary-coverage`

Every keyword of the JSON Schema 2020-12 vocabularies reaches the compiled IR, lowered by a rule of
this document or kept verbatim, its presence and its value both, with four exceptions that change
nothing by design: `$comment`, which the specification forbids surfacing; `$defs`, whose members
lower where a `$ref` reaches them; and `$dynamicAnchor` and `$anchor`, each consumed as a reference
target rather than carried as content ([`dialect-keywords`](#dialect-keywords)). The keyword lists
of [boundaries](../ir/boundaries.md#validation-only-constructs) and of this section are held
to that rather than merely asserted: `TestVocabulary2020_12_EveryKeywordIsLoweredOrKept` enumerates
the vocabularies keyword by keyword and, for each, compiles three documents — the keyword absent,
written with one value, written with a second — requiring that the absent form differs from the
first and the first from the second, and an exception asserts the inverse with its written
justification. A keyword that stops being carried fails there rather than waiting for a reader to
notice. The guarantee is a floor rather than an inventory: it compares one pair of values per
keyword, so a lowering that carried one spelling and dropped another would still pass.

- Tests: `schema.TestVocabulary2020_12_EveryKeywordIsLoweredOrKept`

## 8. Paths and operations

### `path-keys`

A Paths Object key lowers to `HTTPBinding.URITemplate` as written. One that breaks OpenAPI's path
templating rules, those of 3.2.0 §4.8.2 read at every version — no leading `/`, a character a path
admits only percent-encoded, a brace outside a template expression, an expression used twice, an
empty segment, or a query — lowers as declared and is reported as one `openapi/invalid-path-key`
warning at its path item, naming each rule the key breaks. That every template expression has a path
parameter, in the path item or in each of its operations, that every path parameter names an
expression, and that no two templated paths differ only in their names is
[`validation`](#validation)'s to report, as an error.

- Cases: `http-binding`
- Codes: `openapi/invalid-path-key`
- Tests: `operation.TestPathKeys_MalformedKeyIsReportedOnce`,
  `operation.TestPathKeys_RulesAreNotVersionGated`,
  `operation.TestPathKeys_FragmentIsLeftToGitHub602`
- Status: partly built: a `#` and what follows it are not read, so a fragment lowers into the
  template unreported (#602).

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
([`naming`](#naming)), while the `OpID` is derived from the operation's mount pointer
([identity](../ir/identity.md)) and never from the `operationId`, so a repeated or renamed id
moves no ID. An operation with none, or with `""`, claims no id and is named by a hint of its method
and the route's key in neutral words — the path template, a webhook's name, a callback's expression,
or an `additionalOperations` key standing as the method (`get_ping`, `post_new_pet`;
[http-binding](../ir/http-binding.md)). Uniqueness is judged once every operation has
lowered, across paths, webhooks and callbacks alike, and by declaration, so a finding does not move
with the order the document is lowered in. A second declaration writing a claimed id is reported as
an `openapi/conflicting-operation-id` error at the pointer it is written at, naming the first
declaration, the declarations taken in pointer order; one declaration mounted more than once, by a
path item `$ref`, a YAML alias or a merge key, as an `openapi/duplicate-operation-id` warning at
each mount but the first, naming that mount. Every operation lowers whatever the verdict, repeated
names included; the rule table ([`validation`](#validation)) states this rule and leaves it to this
one.

- Codes: `openapi/duplicate-operation-id`, `openapi/conflicting-operation-id`
- Tests: `operation.TestOperations_DuplicateOperationIDReported`,
  `operation.TestJudge_ReportsEveryDeclarationAndMountButTheFirst`,
  `operation.TestOperation_NoOperationIdHint`

### `path-item-servers`

A path item's `servers`, under `paths`, `webhooks` and a callback expression alike, and an
operation's own, lower as the document's do ([`servers`](#servers)): each server into
`Document.Servers`, registered once at the pointer of its Server Object in the document declaring
it, however many mounts reach it. An operation names its own servers on `Operation.Servers` and its
path item's where it declares none, since OpenAPI says the operation's override the path item's; one
that neither declares names none, and its service's apply ([servers](../ir/servers.md)). A
path item's list is registered even where every one of its operations overrides it. Two lists are
not: one a nearer layer of a mount overrides, which [`path-item-ref-fields`](#path-item-ref-fields)
reports rather than lowers, and one written by a component path item no reference reaches, kept
whole with that component ([`unreferenced-components`](#unreferenced-components)). A path item that
mounts no operation registers its servers too, named by nothing, and keeps what else it writes on
the service instead, or on the declaring operation's HTTP binding for a callback expression: its
`x-*` names and undeclared keys under `openapi:pathItem/<item pointer>/<key>`, its summary and
description as [`path-item-docs`](#path-item-docs) says, and its parameters as
[`parameters`](#parameters) says. The item's pointer is in every key, since one carrier holds every
such item, and a component item two paths mount is kept once per mount. It is reported as one
`openapi/degraded-construct` warning at the mount.

- Cases: `webhooks`, `callbacks`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperations_PathItemServersNamedOnEveryRoute`,
  `operation.TestOperations_OwnServersOverrideThePathItems`,
  `operation.TestOperations_ServersEachLevelAlone`,
  `servers.TestRegistry_DeclaredOneDeclarationLowersOnce`,
  `operation.TestPathItem_WithNoOperationKeepsWhatItWroteOnTheService`,
  `operation.TestPathItem_WithNoOperationKeepsEachMountApart`
- Status: open: an explicitly empty list declares nothing to name, as an absent one does, and
  whether it should override the list above it is undecided (#1081).

### `path-item-docs`

A path item's own `summary` and `description`, at the same three mounts, lower to the `PathDocs`
of each HTTP binding of each operation it mounts ([http-binding](../ir/http-binding.md#path-documentation)),
with nothing reported, rather than merged into the operation's `Docs`: `ir.Docs` holds the
operation's own pair, and a path item's documents the path, so merging would need a precedence rule
and would attach documentation the operation's author never wrote, an inference invariant 6 places
in policy. Each keyword is read where the nearest layer of a `$ref`'d path item declares it
([`path-item-ref-fields`](#path-item-ref-fields)). A path item that mounts no operation has no
binding to hold the pair, so it keeps it where it keeps what else it writes
([`path-item-servers`](#path-item-servers)), with the item's pointer in the scope, under
`openapi:pathItem/<item pointer>/summary` and `openapi:pathItem/<item pointer>/description`, since
one carrier holds every such item; the warning announcing that item covers them, so they draw no
finding of their own.

- Cases: `path-item-docs`, `path-item-ref-siblings`
- Tests: `operation.TestPathItem_DocsKeptOnEveryRoute`, `operation.TestPathItem_DocsAddNoMarker`,
  `operation.TestPathItem_DocsKeepApartFromAnOperationsOwnKeys`,
  `operation.TestPathItem_KeepsWhatItDeclaresOncePerMount`,
  `operation.TestPathItem_WithNoOperationKeepsItsDocs`,
  `operation.TestPathItemRefSiblings_UnmountedCollisionKeepsTheUseSite`

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
reference other than a schema's `$ref` or a mapping value when the compile reads other documents,
which lowers as that field. An item another document holds is declared where that document declares
it, and a finding names its positions by the reference; one the compile cannot place there, as past
the longest chain it follows or at a key that document writes twice, is declared under the use
site's `$ref` (`…/$ref/…`), which no document names. The fields beside a `$ref` the load phase's
walk does not reach, in another document or under an `x-*` key, are not lowered, reported as an
`openapi/degraded-construct` warning at the mount naming the reference that reaches them.

- Cases: `path-item-ref-siblings`
- Codes: `openapi/path-item-ref-collision`, `openapi/unresolved-ref`, `openapi/degraded-construct`
- Tests: `operation.TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins`,
  `operation.TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefIsRefused`,
  `operation.TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported`,
  `operation.TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions`
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
parent still nests under it. A tag name declared twice nests as its first declaration says. Each of
the three breaks a MUST of 3.2, and the rule table reports it as an error
([`validation`](#validation)): a parent naming no tag, a parent cycle, at each tag on it, and a
repeated name, at the repeat, naming the first. Group and sibling order stay first-seen from the
operations, with a missing ancestor created immediately before its first descendant, so a document
that declares no parents groups exactly as it would were parents never read.

- Cases: `tags-grouping`, `tags-grouping-32`, `group-identity`
- Tests: `operation.TestGrouping_ParentCycleNestsNoMemberUnderAnother`,
  `operation.TestGrouping_DanglingParentStaysTopLevel`,
  `operation.TestGrouping_SameNameDeclaredTwiceIsDeterministic`,
  `operation.TestGrouping_BadgeOnlyOperationFallsToDefault`, `oas.TestCheck_TagParentsExist`,
  `oas.TestCheck_DuplicatesAreFound`
- Status: partly built: a tag an operation repeats passes into the IR undeduplicated (#738). Open:
  grouping operations by a vendor extension (#582).

## 9. Parameters

### `parameters`

An operation's own parameters, in order, and then those of its path item it does not override, in
order, lower to the operation's `Params`, an operation's parameter overriding a path item's of the
same `name` and `in`. Each is placed on the HTTP binding at its location (`path`, `query`,
`querystring`, `header` or `cookie`) under its wire name, with its `style`, `explode` and
`allowReserved`, each the default where the parameter writes none
([http-binding](../ir/http-binding.md)): the location's style, and an `explode` true under
`form` and 3.2's `cookie` style, false under any other. A parameter's ID is derived from the
operation's mount pointer, its name and its location ([identity](../ir/identity.md)). A list
repeating an earlier entry's `name` and `in`, its `$ref`s resolved, which OpenAPI forbids, keeps
every occurrence, each repeat's ID ending in its occurrence number, and is reported as an
`openapi/duplicate-parameter` error at the repeat. Every list the source writes is checked, whether
or not it lowers, and a list another document writes where it lowers or where a path item that
mounts no operation keeps it; the rule table
([`validation`](#validation)) states this rule and leaves it to this one. A path item that mounts
no operation has none to merge its parameters into, so it keeps each entry as written, a `$ref`
included, where it keeps what else it writes ([`path-item-servers`](#path-item-servers)), under
`openapi:pathItem/<item pointer>/parameters/<name>/<in>`, the path its ID would be derived from, a
repeat's ending in its occurrence number (`no_ir_home`).

- Cases: `param-styles`, `param-style-matrix`, `param-identity`
- Codes: `openapi/duplicate-parameter`
- Tests: `operation.TestParams_RepeatedNameAndLocationKeepsEveryParameter`,
  `operation.TestPathItem_WithNoOperationKeepsItsParameters`,
  `operation.TestPathItem_WithNoOperationNumbersARepeatedParameter`,
  `operation.TestPathItem_WithNoOperationReportsARepeatInAnotherDocument`
- Status: partly built: an undeclared `explode` is written out as the location's default, which
  erases whether the document declared it (#627); header parameters whose names differ only in case
  are two parameters, unreported (#635); and a list only a `$ref` reaches is never checked for
  repeats (#880).

### `content-parameters`

A parameter or header that states its type with `content` instead of `schema` lowers through one
media-type entry, which OpenAPI requires to be the only one: the lowest key as written where a
document declares more, so that no declaration order changes the type. Its schema is the type,
lowered as the `schema` spelling's is with the same constraints and `xml` hints, hoisted at
`<owner>/content/<key>/schema`, or the top type where the entry writes none, and the key as written
is the media type that serializes the value: `HTTPParamBinding.ContentType` on a parameter,
`Property.Encoding.MediaType` on a header, neither set by the `schema` spelling. Every other entry
lowers nothing and is kept whole on the Document under `openapi:<coordinate>`, its own pointer behind
a document's qualifier where it is not the source (`degraded_lowering`), once however many references
reach it: such an entry belongs to the declaration, which no node stands for, and a parameter or
header lowers once per reference. Each is reported in one `openapi/degraded-construct` warning at the
`content` map, beside the `openapi/validation` error ([`validation`](#validation)) such a map draws,
which names the lowest `annotation.MaxUnknownKeys` and counts the rest.
What else the elected entry writes is kept: its `example` and `examples` follow the parameter's or
header's own in `Examples`, and the rest goes verbatim under `openapi:content/<key>/<field>`, the
key escaped as a pointer segment — its `x-*` extensions under `vendor_extension`; an undeclared
key under `out_of_scope`, with the `openapi/unknown-object-key` warning
[`unknown-keys`](#unknown-keys) gives it; and its `description` and the fields only a body Content
models, `itemSchema`, `itemEncoding`, `prefixEncoding` and `encoding`, under `no_ir_home`, each with
an info `openapi/degraded-construct` at it. A `content` map with no entry elects nothing: a `schema`
beside it stands, and the empty map is kept under `degraded_lowering` with an
`openapi/degraded-construct` warning. The converse, `schema` beside a non-empty `content`, is
[`competing-keywords`](#competing-keywords)'.

- Cases: `param-content-fields`, `header-content-schema`, `content-several-entries`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_ContentStyleParameter`,
  `operation.TestPassedOverEntries_ReportsExtraMediaTypes`,
  `operation.TestPassedOverEntries_KeepsEveryEntryAndNamesTheLowest`,
  `operation.TestPassedOverEntries_KeepsOneCopyPerDeclaration`,
  `operation.TestElectContentEntry_DoesNotFollowDeclarationOrder`,
  `operation.TestParamContent_OwnAndMediaExamplesAreBothKept`
- Status: partly built: a `style`, `explode` or `allowReserved` beside a parameter's `content` lowers
  in silence (#887), and what a referenced parameter or header keeps is copied per reference (#1240).

### `allow-empty-value`

A parameter's `allowEmptyValue` lowers to its binding's `AllowEmptyValue`, `false` where it declares
none, as `allowReserved` lowers to `AllowReserved`. Written under a `style` whose empty value
OpenAPI's style table leaves _n/a_ — `spaceDelimited`, `pipeDelimited` or `deepObject` — where it
says the field SHALL be ignored, it still lowers as declared and is reported as an
`openapi/inapplicable-location-keyword` warning at the keyword, as an `allowReserved` its version
gives no effect is ([`allow-reserved-locations`](#allow-reserved-locations)). Written at any
location but `query`, which OpenAPI forbids, it is an `openapi/validation` error of the class
`validation-forbidden-field` ([`validation`](#validation)).

- Cases: `param-styles`
- Codes: `openapi/inapplicable-location-keyword`
- Tests: `operation.TestParams_AllowEmptyValueLowers`,
  `operation.TestParams_AllowEmptyValueUnderAStyleThatCannotWriteOneIsReported`,
  `oas.TestCheck_AllowEmptyValueWarnsUnderAStyleThatCannotWriteIt`

### `reserved-header-names`

A header parameter named `Accept`, `Content-Type` or `Authorization`, which OpenAPI says SHALL be
ignored because the protocol layer owns the name, lowers as declared and is reported as an
`openapi/reserved-header-name` warning at the parameter: dropping declared content is an emitter's
call, not a compiler's, and the diagnostic lets an emitter suppress it.

- Codes: `openapi/reserved-header-name`
- Tests: `operation.TestParams_ReservedHeaderNamesAreReported`

### `querystring`

3.2's `in: querystring` lowers to the `querystring` location, the parameter binding the whole query
string ([`content-parameters`](#content-parameters)). A `style`, `explode` or `allowReserved`
written there, which 3.2 forbids, lowers as declared and is reported as an
`openapi/invalid-location-keyword` warning at the keyword: the parameter's `content` states its
serialization, so the keyword has nothing left to qualify. A `schema` written there leaves the
parameter no content to bind the query string by, and is reported as an `openapi/validation` error,
of the class `validation-forbidden-field` ([`validation`](#validation)). Each Parameter
Object is judged where it is written, whether anything lowers it or not.

- Cases: `param-querystring`, `querystring-forbidden-keywords`
- Codes: `openapi/invalid-location-keyword`, `openapi/validation`
- Tests: `oas.TestCheck_QuerystringTakesOnlyContent`,
  `operation.TestParams_QueryStringDeclaredStyleIsKeptAndReported`

### `allow-reserved-locations`

An `allowReserved` its document's version gives no effect — at `path`, `header` or `cookie` before
3.2, which applies it to `in: query` alone, and at `header` or a `cookie` in `style: cookie` in 3.2,
which applies it wherever the value is percent-encoded — lowers as declared and is reported as an
`openapi/inapplicable-location-keyword` warning at the keyword, a code of its own since no MUST is
broken, once, where the parameter is written, whether anything lowers it or not.

- Cases: `param-allowreserved-locations`
- Codes: `openapi/inapplicable-location-keyword`
- Tests: `oas.TestCheck_AllowReservedWarnsWhereItHasNoEffect`,
  `oas.TestCheck_AllowReservedWordsWhereItAppliesAndWhereItDoesNot`

### `parameter-schema-keywords`

A parameter schema's `xml`, and its `readOnly` and `writeOnly`, are kept verbatim in the parameter's
`Unmodeled` (`no_ir_home`), reported as an info `openapi/degraded-construct` at the keyword, since
`ir.Parameter` has no field for either. The `xml` of a schema that owns a node of its own stays on
that node's `XMLHints` instead. A schema with no node of its own keeps there too what any carrier
keeps: the content vocabulary ([`content-vocabulary`](#content-vocabulary)), a `$dynamicRef` that
did not expand ([`dynamic-ref`](#dynamic-ref)) and a keyword nothing reads
([`unknown-keys`](#unknown-keys)).

- Cases: `param-xml-residue`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_SchemaVisibilityKeptUnderUnmodeled`,
  `operation.TestParams_KeepWhatAnyCarrierKeeps`,
  `operation.TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode`,
  `operation.TestParams_SchemaXMLHintsStayOnAnOwnedNode`

## 10. Request bodies and content

### `media-types`

Every media type of a `content` map lowers to one `Content` apiece, in source order and with none
elected primary ([responses](../ir/responses.md)). `Content.MediaType` is the key as written
— case, parameters and wildcards kept, so `application/json` and `Application/JSON` are two Contents
— and its `Type` is the media type's `schema`, hoisted at `<owner>/content/<key>/schema` with the
key escaped as a pointer token, or the top type where it writes none. An entry written as a `$ref`
lowers what it names under its own key, its schema hoisted and named where the media type is
declared, after its key there when that is `components/mediaTypes` ([`references`](#references)),
and one whose `$ref` resolves to nothing lowers no `Content`. A media type's `description` and the
`summary` and `description` beside its `$ref` are its `Content.Docs`
([`reference-object-siblings`](#reference-object-siblings)). A request body's keys also go, as
written and in order, to `HTTPBinding.RequestContentTypes`. A response whose `content` is absent
or empty has no `Payload` ([`request-body`](#request-body) for the request side). A key is
classified on a copy that is lowercased, since a media type's names are case-insensitive (RFC 6838
§4.2), trimmed and cut at its first `;`, and the IR never keeps the copy: `multipart/*` and
`application/x-www-form-urlencoded` read the media type's `encoding` as part encodings
([`multipart-encoding`](#multipart-encoding)), and a binary body is a file
([`file-bodies`](#file-bodies)); the streaming policy reads the same copy
([`streaming-media`](#streaming-media)). No other key is classified, so `application/*+json`,
`text/*` and `*/*` are Contents like any other.

- Cases: `media-type-spelling`, `multi-content`, `component-media-types-32`
- Tests: `operation.TestContent_AllMediaTypesKeptInOrder`,
  `operation.TestContent_MixedCaseFormContentKeepsEncoding`,
  `lowering.TestNormalizeMediaType_CollapsesTheSpellingsOneMediaTypeHas`,
  `operation.TestContent_AMediaTypeRefHoldingNoStringLowersNothing`,
  `naming.TestAt_NamesASchemaAfterItsOwner`
- Status: partly built: a malformed media-type key, a multipart body that is not an object, and a
  body on a GET pass in silence (#649).

### `request-body`

A request body lowers to the operation's request `Payload`: each media type of its `content` to a
`Content` ([`media-types`](#media-types)), its `required` to `Payload.Required`, always set, since
OpenAPI's own default makes an undeclared `required` mean false rather than unstated, and its
`description` to `Payload.Docs`. A request body whose `content` map is empty, which every minor's
schema admits and 3.2 says SHOULD hold an entry, lowers to a `Payload` with no Contents that keeps
all three and its `x-*`, reported as an `openapi/empty-request-content` warning at the map; a
`content` left out is [`minor-required-fields`](#minor-required-fields)'.

- Cases: `empty-request-content`, `request-body-docs`
- Codes: `openapi/empty-request-content`
- Tests: `operation.TestContent_NonRequiredRequestBody`,
  `operation.TestLowerRequestBody_AnEmptyContentKeepsTheBody`

### `file-bodies`

A content entry is a file body, `Content.File` with `IsText` false and `ContentTypes` the key as
written, where its schema declares raw bytes or leaves its media type to say, so that a body to
stream is not read as a model. The schema is read through a `$ref`, and through the branches of an
`anyOf` or `oneOf` it writes, as the least any of them admits: a branch admitting no value but
`null`, or none as `false` does, is passed over, and one that is itself such a union says the body
is something else. A schema declares
raw bytes with `format: binary` on a `type` that includes `string` or on none, and no
`contentEncoding`, which makes them encoded text as the type lowering reads it, read through a
refinement ([`allof-refinement`](#allof-refinement)) to the schema deciding it, whatever the media
type. It leaves the media type to say where it is absent, `true`, or writes no keyword but `type` as
`string`, alone or with `null`, the annotations `title`, `description`, `$comment`, `example`,
`examples`, `default`, `deprecated`, `readOnly`, `writeOnly`, `externalDocs`, `xml` and `nullable`,
the identifiers `$id`, `$anchor` and `$dynamicAnchor` and the store `$defs`, which say nothing of
the value, as a JSON Schema draft's spellings of them do where the schema's dialect defines one,
draft 4's `id`, the drafts' `definitions` and 2019-09's `$recursiveAnchor`, the length bounds
`maxLength` and `minLength`, which OpenAPI applies to a binary payload's octets,
`contentMediaType`, which a media type set by the content's key overrides, and its own `x-*`, and
where each `$ref` on its chain writes beside it no keyword but those, `type` aside, since such a
keyword holds with what the `$ref` names, beside a 3.0 `$ref` too as the schema lowering applies one
([`later-version-construct`](#later-version-construct)); a chain is followed up to a bound of its
own, and one longer says the body is something else. Such a body is a declared file on
`application/octet-stream`, matched on the normalized key ([`media-types`](#media-types)), since RFC
2046 defines that type as arbitrary octets, and a file the binary policy reads on a media type it
names ([`binary-media`](#binary-media)), where `FileInfo.Inferred` is `binary-media-type`. Any other
keyword, such as a `format` naming a text form, `contentEncoding`, `enum`, `allOf` or an object's,
or a `false` schema, makes the content an ordinary Content whatever its media type, as is a body
leaving its media type to say under one neither names. `Content.Type` stays what the schema lowered
to, which keeps its docs, bounds and examples reachable. Where a declared file's schema states
nothing at all it is `bytes` instead, with what the reference says of null, since the media type
alone says the body is bytes; a file the policy read keeps the top type, so a consumer setting the
guess aside finds what the schema said. No other `FileInfo` field is set. The same holds for a
response and an error case.

- Cases: `file-body`, `file-media`
- Tests: `operation.TestContent_BinaryOctetStreamBody`,
  `operation.TestContent_MixedCaseOctetStreamBodyIsFile`,
  `operation.TestContent_BinaryRefBodyDetectedAsFile`,
  `operation.TestContent_ARefinedBinaryBodyIsAFile`,
  `operation.TestContent_AFileBodyKeepsTheSchemaItWrites`,
  `operation.TestBodyFile_ReadsTheSchemaAndTheMediaType`,
  `operation.TestBodyFile_ReadsAChainOfRefsUpToItsBound`,
  `operation.TestBodyFile_ReadsKeywordsBesideA30Ref`,
  `operation.TestBodyFile_ReadsADraftsSpellingAsItsTwin`

### `binary-media`

The binary policy names the media types under which a body or a multipart part whose schema leaves
its media type to say is a file ([`file-bodies`](#file-bodies),
[`multipart-encoding`](#multipart-encoding)): by default `application/gzip`, `application/pdf`,
`application/vnd.tcpdump.pcap`, `application/wasm`, `application/zip`, `audio/*`, `font/*`,
`image/*` and `video/*`, matched on the normalized key ([`media-types`](#media-types)) at every
minor. OpenAPI says any binary media type marks binary content, but no registry says which media
types are binary, so the reading is a guess and each file it makes carries `binary-media-type` in
its `Inferred`. An entry `type/*` matches each media type of that type whose subtype has no
structured suffix, since a suffix names the syntax a body is written in: `image/*` matches
`image/png`, not `image/svg+xml`. An entry starting with `+` names a structured suffix.
`application/octet-stream` is no entry: a file it carries is declared, whatever the policy says. The
policy is the Go option `Options.BinaryMedia`: `Disabled` turns the reading off, and `MediaTypes`
replaces the default list instead of extending it. The `binary-media` setting sets `MediaTypes`, its
entries separated by commas, and refuses one that is neither a media type nor a suffix, and
`binary-inference=false` sets `Disabled`.

- Cases: `file-media`
- Tests: `lowering.TestMediaTypeBinary_AnswersFromThePolicy`,
  `openapi.TestDecodeOptions_TextReachesEveryField`

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
`itemEncoding` to `Content.ItemEncoding`, with `Multi` set. In a multipart body its `Filename`, and
the `Inferred` beside it, are read off `itemSchema` and the `itemEncoding`'s `contentType` as a
multipart part's declaration is read ([`multipart-encoding`](#multipart-encoding)), an item being
one value where an array part is many, so an array item is no file; an item no `itemSchema`
describes is none either. A multipart body whose items read as files gets an `ItemEncoding` saying
so where it writes no `itemEncoding`, as a file part gets an entry. Beside a positional
`prefixEncoding`, a single every-item encoding cannot state ordinals, so `itemEncoding` and
`prefixEncoding` are both kept verbatim in the content's `Unmodeled`, under `openapi:itemEncoding`
and `openapi:prefixEncoding` (`no_ir_home`), reported as one info `openapi/degraded-construct` at
the content, while `itemSchema` still lowers to `Content.Item`. On a media type whose body is one
document, JSON's or XML's by name or `+json` or `+xml` suffix, and which the streaming policy
([`streaming-media`](#streaming-media)) does not name, `itemSchema` has no sequence to describe: it
lowers all the same, with a warning `openapi/item-schema-on-document` at it. Which other media types
repeat a structure is no closed set, so nothing else draws one.

- Cases: `sequential-media`, `file-items-32`
- Codes: `openapi/degraded-construct`, `openapi/item-schema-on-document`
- Tests: `operation.TestStreaming_AnItemSchemaOnOneDocumentIsReported`,
  `operation.TestStreaming_AnItemSchemaIsItsFrameWhateverItHolds`,
  `lowering.TestHoldsOneDocument_AnswersOnlyForJSONAndXML`,
  `operation.TestItemFile_ReadsTheItemAsAPartDeclarationIs`

### `streaming-media`

A request body or success response with a Content whose media type the streaming policy names —
`application/json-seq`, `application/jsonl`, `application/x-ndjson`, `text/event-stream` and the
`+json-seq` structured suffix by default, matched on the normalized key
([`media-types`](#media-types)) at every minor — lowers to `Operation.RequestStream`
and `ResponseStream` for the directions that have one, each with `Events` what one frame holds: the
`Elem` of a `Type` whose root, read through aliases, is a list, since a schema applies to the whole
body and OpenAPI 3.2 §4.14.3.1 maps a sequential body to an array of its values, else the `Type`
itself, a schema with no array to read being the frame, or `bytes` on a file body
([`file-bodies`](#file-bodies)), whose schema describes the file. `Content.Type` keeps the list.
`Streaming` is the `client`, `server` or `bidi` summary of them, and `Provenance.Inferred` the marker
`streaming-media-type` on
the operation, joined by `,` to any other marker, since below 3.2 a media type is a guess about
framing and not a declaration. A 3.2 `itemSchema` ([`item-schema`](#item-schema)) is a declaration:
it streams on any media type with `Events` the `Item` type and no marker, and wins over the
media-type reading at its own Content. An element settles once the registry is whole, since a `$ref`
can name a list nothing has built yet. Streaming Contents whose frames differ leave `Events` unset
on a direction that still streams, every Content kept, reported as an info
`openapi/degraded-construct` at the `requestBody` or `responses` pointer; Contents whose frames are
one element elect it, a list of a type agreeing with that type. Error responses and the default are not read: whether the default answers a
success is the plan's to decide ([`response-conditions`](#response-conditions)). The policy is the
Go option `Options.StreamingMedia`: `Disabled` turns the media-type reading off, leaving only what
an `itemSchema` declares, and `MediaTypes` replaces the default list instead of extending it, an
entry starting with `+` naming a structured suffix. The `streaming-media` setting sets `MediaTypes`,
its entries separated by commas, and refuses one that is neither a media type nor a suffix, and
`streaming-inference=false` sets `Disabled`.

- Cases: `streaming-media-30`, `streaming-media-31`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestStreaming_ResponseMediaTypeImpliesServerStream`,
  `operation.TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed`,
  `operation.TestStreaming_ItemSchemaDeclaresTheStream`,
  `operation.TestStreaming_AFileBodyStreamsBytes`,
  `operation.TestStreaming_AListBodyStreamsItsElement`,
  `operation.TestStreaming_ContentsCompareByTheirFrames`,
  `e2e.TestExternalContent_AStreamReadsAListAnotherDocumentHolds`,
  `lowering.TestMediaTypeStreams_AnswersFromThePolicy`
- Status: partly built: a response
  offering a plain body and an event stream does not record which content streams (#601); and no
  promotion reads an extension stating a stream's `Terminator`, or the input whose `Condition`
  selects it, into either field (#1668).

### `multipart-encoding`

An Encoding Object of a `multipart/*` or `application/x-www-form-urlencoded` Content lowers to a
`PartEncoding` in `Content.Encoding`, keyed by the `PropID` of the body property its key names,
wherever the body declares that property: on the schema, in an `allOf` branch with the first
declaration of a name winning, or on a model it composes. Where several models declare the part, the
key is the declaration the body's flattening takes
([models](../ir/models.md#the-flattening)): a model's over one it composes, else the later
conjunct's, so `allOf: [{$ref: A}, {$ref: B}]` keys `B`'s. The `allOf` is read as
[`allof-classification`](#allof-classification) flattens it, with the same cap, and a `$ref` entry
is followed up to a bound of its own, and entered again only by a shorter path. A bound that cuts a
target no path enters is reported as a warning `openapi/degraded-construct` at the body schema,
since a part reached only past it has no entry. `contentType` lowers to `ContentTypes`, split on `,`
and trimmed with empty items dropped, and one naming no media type is an `openapi/validation` error
([`validation`](#validation)); `headers` to `Headers`, lowered as a response's are; and `style` and
`explode` to `Style` and `Explode` as declared, nothing defaulted. `Multi` and `Filename` come from
the part, read once every type has lowered, and `Filename` also from the media types its encoding
entry names. Where the body model has the property and its type is a list, a tuple, a primitive, or
a scalar whose bases end in one of those or in no base, they are read off that type, the one the
`allOf` merge settled on, of the declaration the key names: `Multi` where it ends in a list or
tuple, `Filename` where it ends in `bytes` (`format` `binary` or `byte`) or, in a multipart body, in
a list or tuple every element of which ends in `bytes` no encoding such as `format: byte`
re-encodes, a tuple's rest included where elements may follow its prefix. A union is read off its
variants, through the unions nested in them, each as such a type is and one of another kind as
neither: `Multi` where any is, since a part that may repeat travels as repeated parts, and, in a
multipart body, `Filename` where every one is `bytes` no encoding re-encodes or a list or tuple of
them. For a property of any other kind, such as an enum, literal, model or
map, for a union reaching more nested unions than a bound of its own, and for a body with no model,
they are read from every declaration of the part's name: `Multi` where any is an array, or writes
no `type` and a union with an array among its branches, `Filename` where any is a `string` with
`format` `binary` or `byte`. In a multipart body a part is a file too where every declaration of
it reads so as a body's schema does ([`file-bodies`](#file-bodies)), an array through each of its
elements, its `prefixItems` and the `items` after them, or a draft's list of `items` and the
`additionalItems` after them ([`tuples`](#tuples)), a rest of `false` admitting none, as is an
array branch of a union it writes, whose other branches are read as a body's union branches are: one
declaring raw bytes; one leaving its media type to say, where its encoding entry's `contentType`
names only `application/octet-stream` or media types the binary policy names
([`binary-media`](#binary-media)), `PartEncoding.Inferred` being `binary-media-type` where it names
one of the latter; and, from OpenAPI 3.1 and with no `contentType`, one writing no `type`, whose
default content type OpenAPI makes `application/octet-stream`, a union being read so only where
each of its branches, an array's through its elements, writes no `type` or declares raw bytes, so
that `{anyOf: [{}, {type: string}]}` reads as a string. A `string` part with
neither is `text/plain`, whatever its `contentMediaType`, which OpenAPI ignores where an encoding's
default disagrees, and an array writing no `items`, or a draft's tuple no `additionalItems`, says
nothing of its elements past its prefix, so neither is a file. A part is entered only when its
`PartEncoding` holds something, so a body that declares no encoding still gets entries for its array
and file parts, and an entry declaring nothing leaves none. What an Encoding Object writes beyond
those fields is [`encoding-allow-reserved`](#encoding-allow-reserved). An encoding written where
OpenAPI gives it no effect is reported as an `openapi/validation` warning, of the class
`validation-no-effect`, at the field ([`validation`](#validation)): an `encoding` under a media type
that is neither multipart nor form, which lowers nowhere, or before 3.2 outside a request body, and
a 3.2 `prefixEncoding` or `itemEncoding` under one that is not multipart. A media type is read
without its parameters and in any case.

- Cases: `multipart-encoding`, `file-media`
- Codes: `openapi/degraded-construct`, `openapi/validation`
- Tests: `operation.TestContent_MultipartPartEncoding`,
  `oas.TestCheck_EncodingsApplyWhereTheSpecificationSays`, `oas.TestEncodingRules_AnswerAsTheTableDoes`,
  `operation.TestContent_ArrayMultipartPartMulti`,
  `operation.TestPartEncodings_MixinPartIsKeyedOnTheMixin`,
  `operation.TestPartEncodings_KeyedOnTheDeclarationTheFlatteningTakes`,
  `operation.TestPartEncoding_FlagsComeFromTheMergedType`,
  `operation.TestPartEncoding_FlagsOfAnUndecidedTypeComeFromTheSchema`,
  `operation.TestPartEncoding_FlagsOfABodyWithNoModelPropertyComeFromTheSchema`,
  `operation.TestPartEncoding_FlagsOfAnUndecidedMergedTypeComeFromEveryDeclaration`,
  `operation.TestPartEncoding_ADecidedTypeNeverFallsBack`,
  `operation.TestContent_MultipartRedeclaredPartFlagsComeFromTheSettledType`,
  `operation.TestPartEncodings_ReadsTheFlattenedComposition`,
  `operation.TestPartEncodings_RefHopBoundIsReported`,
  `operation.TestPartEncodings_ShorterPathReentersTarget`,
  `operation.TestPartEncodings_CappedEntryReachedByAnotherPath`,
  `operation.TestPartEncodings_CutPastReachedPart`,
  `operation.TestPartTypeFlags_AListOrTupleOfRawBytesIsFiles`,
  `operation.TestPartTypeFlags_AUnionReadsThroughItsMembers`,
  `operation.TestPartEncoding_AMergedUnionReadsThroughItsVariants`,
  `operation.TestReadPartFile_ReadsDeclarationsEncodingAndVersion`,
  `operation.TestReadPartFile_ReadsADraftsTupleThroughItsEntries`,
  `schema.TestListedTuple_ReadsADraftsEntriesAndRest`
- Status: partly built: an `encoding` key that names no property, or one declared only inside a
  `oneOf` or `anyOf` branch, is dropped with no diagnostic (#604); on a multi-typed body the entry
  is keyed by a property no node holds, which validation then refuses (#744); a part's own
  `format: byte`, and a part of a url-encoded body, is marked as carrying a filename (#1214); and an
  encoding under a media type it does not apply to is kept in no form (#1047).

### `encoding-allow-reserved`

An Encoding Object's `allowReserved` lowers to its `PartEncoding.AllowReserved`, on the part's entry
or on the content's `ItemEncoding`, `false` where it declares none, as a parameter's lowers onto its
binding. A part declaring it and nothing else keeps its `PartEncoding`, which a part declaring no
field of one leaves out of `Content.Encoding`.

- Cases: `multipart-encoding`
- Tests: `operation.TestEncoding_AllowReservedAndExtensionsReachTheIR`,
  `operation.TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding`,
  `operation.TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey`,
  `operation.TestPartEncodingEmpty_ReadsEveryField`

## 11. Responses and errors

### `response-conditions`

Each entry of an operation's `responses` lowers to a `Response`, for a status below 400 or a range
below `4XX`, or to an `ErrorCase`, for a status from 400 or a `4XX` or `5XX` range, its `Fault`
`client` or `server` by the class ([responses](../ir/responses.md)): RFC 9110 gives a status code
its class, so the split states what the key means. `default` lowers to `Operation.Default`, in
neither list: it answers every status the map does not name, successes and failures alike, and is
the operation's only description of success where the map names none, so the compiler classes it
neither way and leaves the reading to the plan's policy ([plan policy](../emitters/plan/policy.md)).
The `Conditions` of each state the statuses its key names: a code as that code, a range, in either
case, as its hundred codes, and `default` as the catch-all alone. The key, as declared and then
neutralized, is the `Name.Hint` of each (`404`, `5_xx`, `default`), which records the spelling a
range cannot state, though neutralizing a range folds its case (`5XX` and `5xx` both give `5_xx`). A
key naming no status — not a code from 100 to 599, not a range, not `default` — lowers to a
`Response` with no status condition, rather than the catch-all `default` alone denotes, reported as
an `openapi/invalid-status-key` warning at the operation's entry for it.

- Cases: `per-status-errors`, `default-response`
- Codes: `openapi/invalid-status-key`
- Tests: `operation.TestResponses_NamedByStatusKey`,
  `operation.TestResponses_InvalidStatusKeyIsReported`,
  `operation.TestResponses_ErrorSplitAndRanges`,
  `operation.TestResponses_ALoneDefaultIsNeitherClass`

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

An error response lowers exactly as a success one and a default do: its `headers` to
`ErrorCase.Headers` and every media type of its `content` to `ErrorCase.Payload.Contents`, neither
degraded and neither kept under `Unmodeled`, and the payload's naming hint derived from the
declaration pointer on every side, so that one `components/responses` entry mounted at a success
status, an error status and `default` interns one type whichever reaches it first.

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

A header of a response or an encoding lowers to a `Header` of its headers, and its `style` and
`explode` to that header's `Style` and `Explode` ([responses](../ir/responses.md)), read from
its declaration and each left unset where the header writes none. A `style` other than `simple`, the
one OpenAPI allows a header, is an `openapi/validation` error of the class
`validation-allowed-values` ([`validation`](#validation)). A `Content-Type` entry in either headers
map, which OpenAPI says SHALL be ignored, lowers as declared and is reported as an
`openapi/reserved-header-name` warning at the entry, as a parameter is
([`reserved-header-names`](#reserved-header-names)).

- Cases: `header-content-schema`
- Codes: `openapi/reserved-header-name`
- Tests: `operation.TestHeaders_SerializationKeywordsLower`,
  `operation.TestHeaders_SerializationKeywordsAbsentRecordNothing`,
  `operation.TestHeaders_SerializationKeywordsFollowTheDeclaration`,
  `operation.TestHeaders_ReservedContentTypeEntryIsReported`,
  `operation.TestHeaders_ReservedContentTypeInEncodingIsReported`
- Status: partly built: response headers whose names differ only in case are two headers, unreported
  (#635); and a `style` or `explode` beside a header's `content`, where they have no effect, lowers
  in silence, as a parameter's does (#887).

### `links`

A response's `links` lower to `Links` on the `Response`, the `ErrorCase` or the `Default` that
carries them, in source order ([links](../ir/responses.md#links)): the three are lowerings of one
Response Object, so a construct kept on only one would make a declaration survive or vanish on its
status code alone. A link is resolved once every operation has lowered and the type registry is
whole, since it may name an operation declared after it and a member of a type the registry builds
last. Each `$ref` in the map is followed, so the `components/links` entry it names lowers there and
is not also kept on the document ([`unreferenced-components`](#unreferenced-components)), and the
`summary` and `description` beside it fold over the link's docs
([`reference-object-siblings`](#reference-object-siblings)).

1. Its target is the operation its `operationId` names, or the one its `operationRef` points to
   in this document: the operation mounted at that position, else the one declared there when it is
   mounted once.
2. Each `parameters` key names a parameter of the target: as `<in>.<name>`, or as a name alone that
   one parameter carries, a header's matched in any case. Each parameter is given one value.
3. Each value, and the `requestBody`, is a constant, or a runtime expression written whole. `$url`,
   `$method` and `$statusCode` lower to their kinds; `$request.<path|query|header>.<name>` to the
   parameter the operation answered binds there; `$response.header.<name>` to the one header the
   response declares under that name in any case; and a `body` reference to the body whole, or to
   the chain of members its pointer walks from the root of every content of that body, which every
   content must walk alike.
4. Its `server` registers as a server at the link's own pointer, and its extensions and undeclared
   keys stay on it.

A link that does not lower whole is kept verbatim on its response under `openapi:links/<name>`
(`no_ir_home`), as it resolved, with a finding at its entry in the links map:

1. An `openapi/invalid-link` warning when it names its operation both ways or neither, or by an
   `operationRef` that is no JSON pointer, or its target, a parameter it names or a value it reads
   names nothing the document declares or names it more than once, or a value breaks the
   runtime-expression grammar or reads a response's query or path.
2. An `openapi/untyped-link` info when the link is sound but reads what no link value spells: an
   operation of another document or one mounted more than once, a request value no parameter of the
   operation answered binds, a header the response does not declare once, a body the document does
   not describe, a member reached through an array index, a map, a union or any type but a model,
   members the contents of a body disagree on, a string an expression is substituted into, whether
   it is the value or sits within a list or an object the value holds, or a constant the IR has no
   value for.

- Cases: `response-links`
- Codes: `openapi/invalid-link`, `openapi/untyped-link`
- Tests: `operation.TestLinks_AFindingKeepsTheLinkWhole`, `operation.TestLinks_ResolvedFormsLower`,
  `operation.TestLinks_EveryValueKindLowers`, `operation.TestLinks_ErrorsAndDefaultsCarryLinks`,
  `operation.TestLinks_ASharedResponseResolvesPerOperation`,
  `operation.TestLinks_MediaTypesDisagreeingOnAMember`,
  `operation.TestLinks_AHeaderTwoCasesDeclareIsNeither`,
  `operation.TestLinks_LowerAlikeInEitherDeclarationOrder`, `operation.TestParseExpression_ReadsTheGrammar`,
  `operation.TestEmbedsExpression_FindsAnExpressionInBraces`,
  `e2e.TestUnpreservable_AnnouncementNeverOutrunsTheEntry`
- Status: partly built: a key beside a link's `$ref` other than `summary` and `description` is
  dropped with no record, as beside any other reference (#1321).

## 12. Webhooks and callbacks

### `webhooks`

Each operation of a `webhooks` entry's path item lowers to an `Operation` mounted at its own
pointer, in the synthesized `webhooks` group, its HTTP binding's `Inbound` a webhook, with the
entry's key as `WebhookName` and no `URITemplate`, since the key names an event rather than a path.
Its `Auth` is what its own `security` states, and nothing when it writes none: the document's
`security` describes requests clients send, and a webhook is one the API sends
([auth](../ir/auth.md#inbound-operations)).

- Cases: `webhooks`

### `callbacks`

An operation's `callbacks` lower to its HTTP binding's `Callbacks`: for each runtime expression of
each named callback, a `Callback` holding the callback's name and the expression as written and the
`OpID`s of the operations its path item declares. Each of those lowers to an `Operation` of its own,
mounted at its own pointer below the declaring operation's
(`…/callbacks/<name>/<expression>/<method>`, the name escaped as one token), grouped as any other
operation is, its binding's `Inbound` a callback, with the expression as its `URITemplate`
([http-binding](../ir/http-binding.md#inbound-operations)), and inheriting no `security`, as a
webhook does. A key is a runtime expression, by the grammar each
minor gives, text embedding runtime expressions in braces, as OpenAPI's own example does, or a
constant URL, which embeds none; one that is none of them is reported as an `openapi/validation`
error, of the class `validation-invalid-format`, at the key ([`validation`](#validation)), and
lowers as written.

- Cases: `callbacks`
- Codes: `openapi/validation`
- Tests: `operation.TestCallbacks_NameWithSlashKeepsItsOwnKey`,
  `oas.TestCheck_CallbackKeysAreRuntimeExpressions`
- Status: partly built: callbacks declared on an operation inside a callback are dropped in silence
  (#858).

## 13. Security

### `security-requirements`

`components/securitySchemes` lowers to the Document's auth schemes, each under an ID derived from
its pointer, and a `security` list — the document's to the Service's, an operation's to the
Operation's — to an OR of ANDs: each requirement object one alternative, naming its schemes, each
with the scopes it lists ([auth](../ir/auth.md)). An operation that writes no `security`
inherits the document's, save a webhook or a callback, which inherits none
([`webhooks`](#webhooks)), and one writing `security: []` is public; the two stay apart, absent
against empty ([operations](../ir/operations.md)). The document's list keeps the same two
spellings apart in `Service.Auth`.

A requirement names a scheme by its key under the source's `securitySchemes`, whichever document
holds the requirement, matched exactly. A name that reaches no mechanism lowers to a scheme of kind
`unresolved` ([auth](../ir/auth.md#unresolved-schemes)), one per name, and is reported as an
`openapi/unresolved-security-scheme` error at each requirement.
Nothing else about the requirement changes: the option keeps its other schemes, its scopes and its
place in the list, so an operation stating its own requirement does not read as inheriting, and an
option whose name reaches no mechanism does not vanish from the options beside it. The error stands
however the rest lowers, since the specification requires the name to correspond to a scheme and
what it means is missing ([`validation`](#validation)). A name stands whatever is beside it, whether
or not it reaches a mechanism, since an option demands every scheme it names: a value that reads as
no list of strings lists no scopes, a warning from the rule table for a `null` and an error for any
other ([`requirement-scopes`](../parser-library.md#requirement-scopes)). A scope is the text an item
writes, so a plain date is one ([`undecoded-scalar`](../parser-library.md#undecoded-scalar)).

A name reaches no mechanism in three ways. No entry declares it, and the scheme records no pointer,
since no position writes it. Or its entry omits the field naming the mechanism — `type`, or for
`http` the `scheme` token — which is reported as an `openapi/incomplete-security-scheme` error at
the entry. Or its entry resolves to no object, whose `$ref` is reported as an `openapi/unresolved-ref`
error at the entry, and whose other shapes the rule table reports
([`minor-required-fields`](#minor-required-fields)). An entry lowers to that kind whether or not a
requirement names it, recording its pointer and keeping what it wrote: its `description` and
`deprecated`, the `summary` and `description` a Reference Object writes beside its `$ref`
([`reference-object-siblings`](#reference-object-siblings)), its `type` and each mechanism field
verbatim in `Unmodeled` under `openapi:<field>` (`degraded_lowering`), each reported as an info
`openapi/degraded-construct` at the field, and its extensions and undeclared keys as any scheme's are.
A `$ref` that reached nothing is kept the same way, under `openapi:$ref`, with only its error. An
entry written as no object holds no field to keep. A `security` entry written as no object names
nothing, so a list holding nothing else, though it writes entries, lowers to no option: it collapses
to nil, beside the rule table's error, since `[]` would read as public. A `security` that is no list
lowers as one left out, beside the rule table's finding, a warning for a `null` and otherwise an
error.

3.2's device flow lowers to `Flows` beside the other flows, and `oauth2MetadataUrl` to
`OAuth2MetadataURL`. An `oauth2` scheme whose `flows` declares no flow — `flows: {}`, only `x-*` and
keys that name no flow, or only flows written null — and that has no `oauth2MetadataUrl` lowers as
declared, reported as an `openapi/oauth2-no-flow` warning at the declaration; a `flows` left out,
null or no mapping is the rule table's to report instead
([`minor-required-fields`](#minor-required-fields)). The table states the rules this one reports, a
requirement naming no scheme and a scheme naming no mechanism, and leaves them to it.

- Cases: `security-schemes`, `security-or-and`, `security-root-empty`, `security-unresolved`,
  `oauth2-no-flow`
- Codes: `openapi/oauth2-no-flow`, `openapi/incomplete-security-scheme`,
  `openapi/unresolved-security-scheme`, `openapi/unresolved-ref`
- Tests: `auth.TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismLowersAsUnresolved`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsWhatItWrote`,
  `auth.TestLowerSecuritySchemes_AKeptTypeIsTheEntrysOwn`, `auth.TestLowerSecuritySchemes_ABrokenRefIsKept`,
  `auth.TestSecurityRequirement_AnUnresolvedMemberKeepsItsOption`,
  `auth.TestSecurityRequirement_AMemberWhoseScopesDoNotReadStaysInItsOption`,
  `auth.TestSecurityRequirement_AScalarTheModelDoesNotDecodeIsAScope`,
  `auth.TestLowerSecuritySchemes_ARefTheModelDoesNotDecodeIsReported`,
  `auth.TestLowerSecuritySchemes_ABrokenRefKeepsTheDocsBesideIt`,
  `auth.TestSecurityRequirement_ASecurityThatIsNoListReadsAsLeftOut`,
  `oas.TestCheck_ANullBesideARequirementsNameListsNoScopes`,
  `e2e.TestSecurityUnresolved_ANameTwoDocumentsUseIsOneSchemeOfTheSource`,
  `auth.TestSecurityRequirement_AnOperationsOwnUnresolvedListDoesNotInherit`,
  `auth.TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil`,
  `auth.TestAuth_RequirementsOrOfAnds`
- Status: partly built: a 3.2 requirement naming a scheme by URI lowers as unresolved (#1212), an
  unresolved scheme name gets no near-miss hint (#694), a scope a requirement repeats passes into
  the IR undeduplicated (#738), a requirement whose only key is a `<<` naming no mapping lowers to
  the empty option, beside the rule table's error (#1244), and a field under a tag outside YAML's
  JSON schema ruleset reads as left out, so `scheme: !local bearer` is reported as omitted and names
  no mechanism, and a `security` list under a local tag inherits with no finding (#1368), and a
  `$ref` written as null refuses the whole document (#1284).

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

The document's `servers` lower to `Document.Servers`, each server registered at the pointer of its
Server Object, as its ID's path and its provenance ([servers](../ir/servers.md)), and the service
names them, in order, on `Service.Servers`: `url` to `URLTemplate` as written, `description` to its
docs, 3.2's `name` to its `Name` ([`naming`](#naming)), each entry of `variables`, in the order
written, to a variable with its `default`, an empty string kept and none where the entry writes no
`default`, its `enum` and its `description`, and the server's `x-*` and undeclared keys to its own
`Unmodeled`. A document whose `servers` is absent or empty is served at the one server OpenAPI
supplies, at `/`, minted as `server/default` and recording no pointer, since nothing in the document
writes it. A name one servers list repeats — the document's, a path item's or an operation's,
compared as written at every version, since the parser reads `name` on each — is kept as declared on
both entries and reported as an `openapi/duplicate-server-name` warning at the repeat's name, naming
the first.

- Cases: `servers-variables`
- Codes: `openapi/duplicate-server-name`
- Tests: `servers.TestDuplicateServerName_BothServersLower`,
  `servers.TestRegistry_RootServesADocumentWithNoServersAtTheRoot`,
  `servers.TestRegistry_RootNamesTheDeclaredServersInSourceOrder`,
  `load.TestServerRepeats_ReportsEveryListTheSourceWrites`,
  `load.TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly`,
  `servers.TestServerVariables_AnEmptyDefaultIsNotNone`
- Status: partly built: a servers list only a `$ref` reaches is never checked for a repeated name
  (#880).

## 15. Vendor extensions and policy

### `vendor-extensions`

An `x-*` key is legal on every object, so it is read at every object that admits one and kept
verbatim under `vendor_extension` ([`verbatim-preservation`](#verbatim-preservation)), with no
diagnostic. An object that lowers to a node with an `Unmodeled` map of its own keeps its extensions
there, unscoped. One that lowers to no node of its own is keyed by the path from its carrier down to
it, since several such objects reach one map and an unscoped key would leave the survivor to
lowering order ([unmodeled](../ir/unmodeled.md#keys)): on the Document, `openapi:info/x-*`,
`openapi:info/contact/x-*`, `openapi:info/license/x-*`, `openapi:externalDocs/x-*`,
`openapi:components/x-*`, `openapi:tags/<i>/x-*` and `openapi:tags/<i>/externalDocs/x-*`; on the
service, `openapi:paths/x-*`; on each operation, the path item's `openapi:pathItem/x-*`, and
`openapi:responses/x-*` and `openapi:externalDocs/x-*`; on the HTTP binding,
`openapi:callbacks/<name>/x-*`; on a content, `openapi:encoding/<part>/x-*` and
`openapi:itemEncoding/x-*`; on a schema's node, `openapi:xml/x-*`, `openapi:discriminator/x-*` and
`openapi:externalDocs/x-*`; and on a security scheme, `openapi:flows/x-*`. `<part>` and `<name>` are
escaped as RFC 6901 tokens, so a name the document chooses stays one segment
([unmodeled](../ir/unmodeled.md#keys)). A Link Object's own extensions ride inside the
verbatim `links` entry rather than taking a key beside it, and a path item that mounts no operation
keeps its own under `openapi:pathItem/<item pointer>/x-*`
([`path-item-servers`](#path-item-servers)). An extension whose value converts to no JSON reaches
the IR in no form, reported as an `openapi/degraded-construct` warning at the object holding it.

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
to ignore a keyword it does not know, and a position that lowers to a shared node and carries no
property or parameter hoists an alias for it to land on; `$comment`, `$dynamicAnchor` and
`$dynamicRef` are decided elsewhere and left out. A keyword the model names and the dialect the
schema is read in does not define, which only `nullable` is, with the nullable-widening policy off
([`nullable`](#nullable)), is kept the same way. One object contributes at most `annotation.MaxUnknownKeys` keys,
taken in byte order after the keys another reader already kept at the same pointer are set aside,
and the rest reach the IR in no form, reported as one `openapi/unknown-key-budget` warning at the
object.
Two other outcomes also keep nothing and warn: `openapi/unknown-key-entry-taken`, where
`openapi:<key>` is already held by a construct written elsewhere on a carrier two objects share, a
parameter's own key and its schema's keyword, whichever lowered first surviving; and
`openapi/unknown-key-unreachable`, where the key is merged in through a `<<` chain deeper than the
compiler's reader expands, so it reads no pair for it. A key merged in within that depth is read,
kept and reported as a written one is.

- Cases: `path-item-anchored-key`, `unknown-keywords-at-shared-nodes`
- Codes: `openapi/unknown-object-key`, `openapi/unknown-schema-keyword`,
  `openapi/unknown-key-budget`, `openapi/unknown-key-entry-taken`, `openapi/unknown-key-unreachable`
- Tests: `e2e.TestUnknownKeys_KeptAtEveryObject`,
  `e2e.TestUnknownKeys_SchemaAndObjectAreGradedApart`,
  `annotation.TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes`,
  `annotation.TestUndefinedKeywordsIn_KeepsWhatTheCallerNames`,
  `annotation.TestWritesUnknownKeyword_AsksWhatTheCensusWouldKeep`,
  `e2e.TestNullableWidening_DisabledKeepsItAtASharedNode`
- Status: partly built: a parameter's key and its schema's keyword of one spelling share an entry
  (#396).

### `extension-promotion`

A vendor extension the promotion policy maps to a typed field is read into that field as well as
kept ([unmodeled](../ir/unmodeled.md#promotion) states the four rules a promotion keeps). The
policy is `Options.Promotions`, on by default: its `Targets` replace the default mapping and its
`Disabled` turns it off. A target reads a list of sources, each an extension key and, where the
key's value is an object holding several facts, the member of it to read. The list is the
precedence: the first source stating a value fills the field, and a later one stating another is
reported as an info `openapi/degraded-construct` at its own key. An absent member states nothing,
and a key with no members to read is reported the same way, once however many sources read it. The
node keeping the extension lists the marker `extension-promotion` in its `Provenance.Inferred`, as
does a use its `$ref`'s target lends a field to, and the extension stays in `Unmodeled` untouched.

`Deprecation` is filled on an operation, a parameter, a header, a property, a type node or a
security scheme. A key can state the deprecation itself: the default reads `x-fern-availability`,
then `x-stability`, whose text `deprecated` or boolean `true` creates the `Deprecation`, whatever a
`deprecated: false` beside it says, since the marker and the entry let a consumer that disagrees
see what was read. `false` and any other word state nothing, and any other shape is reported. A
member is filled on what is deprecated by then, and on anything else the key annotates nothing and
stays where it is. `Message` reads `x-deprecated-reason`, `x-stainless-deprecation-message` and the
`description` of `x-cfDeprecation`; `Since` reads `x-deprecated-since`; and `RemovalDate` reads
`x-sunset`, the `date` of `x-forge-sunset` and the `eol` of `x-cfDeprecation`.

A property, header or parameter naming a `$ref` reads the target's mapped keys beneath its own, as
[`ref-use-site`](#ref-use-site) merges the target's other annotations: a key beside the `$ref` wins
field by field and without a report, and the target is the end of the chain, so a schema between the
two lends nothing, as it lends no keyword. One holding a refinement
([`allof-refinement`](#allof-refinement)), or a `$ref` to one, reads along the chain its keywords
are read along, nearest first: the nearest link stating a deprecation, by a keyword or a key,
decides it and lends its members, and any link asking for redaction asks it of the use. A node a
`$ref` still speaks on once a value set takes its type reads that `$ref`'s keys the same way, as it
reads its keyword. A key on the target stating a deprecation counts as the target's `deprecated`, so
a `deprecated` beside the `$ref` decides over it as it does over the keyword, and a key beside the
`$ref`, being the use's own, is not stopped by one. A target lends a member only where it is
deprecated itself, by a keyword or a key: on a live target the member annotates nothing, as it does
anywhere, and a use deprecated on its own does not change that. So the target's `Deprecation` is all
a use reads of it, which is what the [fold](../emitters/refine/decorators.md) of a 3.0 decorator
over the same target reads, and what the target's key cannot hold is reported where that key is
written and read, once, with a use reading past it in silence.

A value a member cannot hold, anything but text, is reported as an info `openapi/degraded-construct`
at the extension and not coerced, since the document means something else by the key. Empty text
states nothing, and neither fills a member nor stands before a later source. Other text of the right
JSON shape is taken as written: `x-sunset` fills `RemovalDate` and not `RemovalVersion` because the
header it echoes ([RFC 8594](https://www.rfc-editor.org/rfc/rfc8594)) is a date by definition, and
the mapping is where that reading is stated — the promotion does not then parse the date to confirm
it. No default key names `RemovalVersion`: a document stating a removal *version* names its own key,
per rule 1 of [unmodeled](../ir/unmodeled.md#promotion).

`Operation.Stability` reads `x-stability`, then `x-fern-availability`. `alpha` is `alpha`, `beta` and
`preview` are `beta`, and `stable` and `generally-available` are `stable`. What the deprecation
reads of a key, `deprecated` or `true`, names no tier and is not reported here; `legacy`, any other
word and any other shape are reported, once, and leave the field unset. Where the two keys disagree
`x-stability` wins, as the order decides and not the tiers, so `x-stability: stable` wins over
`x-fern-availability: beta`. That order is chosen for what the golden spec writes, where every
disagreement is `x-stability: beta` against `generally-available`, 47 operations (spec @319dc756), so
there it reads the less stable statement. It is the right call where the text can tell, as the 2 of
them whose own description says beta show, and none of the 2,636 operations marked generally
available alone says so. It over-reports where the mark sits on one method of a resource and not on
its neighbours, as `GET` and `PUT` of `/accounts/{account_id}/access/apps/{app_id}` carry it and
`DELETE` does not, which reads as a mark on a payload and not on the operation. A consumer gating on
the tier hides such an operation, and the report on each disagreement and the kept entries are what
let it audit that.

`x-sensitive` asks for redaction as `format: password` does ([`password`](#password)), and lands
where that does: `Sensitive` on the node a position owns, `Secret` on a property, header or parameter
that owns none, the target of a `$ref` lending it to the use. `true` writes it, `false` states
nothing, and any other shape is reported. A `false` clears neither a `format: password` nor what a
`$ref`'s target asks for, though on a parameter or a header it hides a `true` on its own schema, as
the two share one entry.

`Enum.Closed` is the one target whose fact is stated by a key being *present* rather than by a
value, so a value is not read as a statement there. Its default key, `x-extensible-enum`, writes the
member list as its own value, and a list of members says nothing about openness that the key naming
it has not already said; the promotion therefore clears `Closed` on presence. A boolean value is the
one shape that does state openness by itself, and an explicit `false` is read as written rather than
inverted. Only openness is ever promoted: a schema's `enum` is closed by definition, so a document
declares the open case or nothing, and the mapping names that fact rather than the field's own
polarity. A document that writes `x-extensible-enum` *instead* of `enum` lowers to no `Enum` at all
and there is no node to open — minting one from a vendor key would be a compiler reading a member
list out of an extension, not a promotion, so the entry is left for a consumer that wants to.

`Union.Closed` is read the same way, by presence, through the target `union.open`: a `oneOf` or
`anyOf` is closed by definition, so a document can only declare the open case. No default key names
it, since no spelling for an open union has become common, so a union opens only under a mapping
that names its key. The union a mixed `enum` lowers to ([`enum-const`](#enum-const)) reads
`enum.open` instead, as the enum would, since a target names what the document writes, whatever
node holds it: `x-extensible-enum` opens it under the default mapping, a `union.open` key does not,
and on a `oneOf` the enum's key opens nothing. Each node reads one target, so no fact is read twice,
where one reading could override what the other reports.

- Cases: `extension-promotion`, `extension-stability`, `extension-redaction`
- Codes: `openapi/degraded-construct`
- Tests: `lowering.TestPromoterDeprecation_ValueThatIsNotTextIsReported`,
  `lowering.TestPromoterDeprecation_KeyStatesIt`,
  `lowering.TestPromoterStability_ReadsEachWord`,
  `lowering.TestPromoterRedaction_OnlyTrueWrites`,
  `lowering.TestPromoterEnumOpenness_WritesNothing`,
  `lowering.TestPromoterUnionOpenness_OpensOnAMappedKey`,
  `e2e.TestPromotion_AMappedKeyOpensAUnion`,
  `e2e.TestPromotion_AMixedEnumsUnionReadsTheEnumTargetAlone`,
  `lowering.TestPromoter_ReadsSourcesInPolicyOrder`,
  `lowering.TestPromoter_MemberReadsOneFactOfAnObject`,
  `lowering.TestPromoter_ReferentLendsBeneathOwnKeys`,
  `lowering.TestPromoter_ReferentReadsWhatASchemaLends`,
  `lowering.TestExtensionTarget_EveryTargetIsInThePromotionTable`,
  `e2e.TestPromotion_RemovalDateAndVersionAreSeparateFacts`,
  `e2e.TestPromotion_AUseReadsItsReferentsKeys`,
  `e2e.TestPromotion_ARefinementLendsWhatItRefines`,
  `e2e.TestPromotion_ALostRefLendsTheNodeItSpeaksOn`,
  `e2e.TestPromotion_ADecoratorMatchesTheRefItStandsFor`,
  `e2e.TestPromotion_UsesHoldTheirOwnAnswerInEitherOrder`,
  `e2e.TestPromotion_AContentCarrierReadsTheEntryItElects`
- Status: partly built: the default mapping leaves pagination, open enums stated by value, reserved
  expansion and server names to come (#656); a mapping cannot say which extension values state the
  fact, so a stability key's `true` reads as a deprecation (#654); an unknown key under a prefix the
  policy owns is kept in silence (#657); a key beside an `allOf` over an object, or on its branch,
  lands on the decorator the `allOf` lowers to, where beside a `$ref` it lands on the carrier (#1220); a key on a path item, a tag or a
  content entry, kept under a scope, reaches no field (#1221); a key both a parameter or a header and
  its schema write shares one entry, so the object's is all that is read (#396); a schema a use names
  between it and the end of its chain lends no key, as it lends no keyword (#1029); a key inside a
  `oneOf` kept verbatim beside a body, as the base of a hierarchy keeps the union of its subtypes
  ([`union-beside-structure`](#union-beside-structure)), reaches no field; and a mapping naming a
  target nothing reads is accepted in silence (#1281).

### `mutually-exclusive-parameters`

The groups an `x-mutually-exclusive-parameter-groups` extension declares are kept as any extension
is ([`vendor-extensions`](#vendor-extensions)). Their promotion, which needs no node of its own, is
to be a pass that synthesizes one logical `Parameter` typed by a `Union` of variant models, each
field bound through `HTTPParamBinding.ParamPath`.

- Status: not built: no pass promotes the groups yet (#988).

### `pagination-policy`

Pagination is set only by an injectable policy, and what it sets is marked `Inferred` (invariant 6):
the compiler reads no pagination out of a document's parameter or response names by itself.

- Status: not built: no policy infers pagination yet, and no promotion reads it from an extension,
  so the compiler sets no `Pagination` (#989, #656).

## 16. Swagger 2.0

### `swagger-lift`

A Swagger 2.0 document is to be lifted to the OpenAPI 3.x shape first — `body` and `formData`
parameters to a `Payload`, `host`, `basePath` and `schemes` to `Servers`, `consumes` and `produces`
to content types — and then lowered by the rules above.

- Status: not built: a Swagger 2.0 document is recognized and refused with
  `engine/no-compiler-for-format` until milestone 2, and the lift is to keep provenance pointing
  into the 2.0 source (#681).
