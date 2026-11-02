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
name, and every rule below that differs by minor follows that one. A version that is not three runs
of digits, major, minor and patch, such as `3.0` or `3.1.0-rc1`, is reported as an
`openapi/validation` error, of the class `validation-invalid-format` ([`validation`](#validation)),
and the document still lowers under the minor it names. Any other
version is not this compiler's. The engine refuses a document declaring one before any compiler
runs, as it refuses a Swagger 2.0 document ([`swagger-lift`](#swagger-lift)), and the compiler,
handed one directly, refuses it with an `openapi/unsupported-version` error and compiles to no
Document.

- Codes: `openapi/unsupported-version`, `openapi/validation`
- Tests: `openapi.TestCompile_UnsupportedVersion`,
  `load.TestSupportedMinor_AcceptsOnlyTheThreeMinors`,
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

- Codes: `openapi/undecodable-source`, `openapi/stream-documents-dropped`, `openapi/tagged-mapping`,
  `openapi/budget-exceeded`
- Tests: `detect.TestRecognize_ABrokenDocumentIsReportedAtAnySize`,
  `openapi.TestDetect_ReadsTheVersionTheCompileReads`,
  `detect.TestRecognize_AReadCutShortIsReported`,
  `detect.TestSniff_ReadsAMergeChainAsFarAsTheViewExpandsIt`,
  `detect.TestRecognize_AMergeCycleHidesNoKey`,
  `load.TestLoad_AStreamLowersItsFirstDocumentAndReportsTheRest`,
  `load.TestLoad_ATaggedMappingIsRefusedBeforeParsing`
- Status: partly built: a JSON source holding a surrogate-pair escape is refused as undecodable
  (#900).

### `source-budgets`

`Options.Limits` bounds how large a source the compiler lowers, each budget taking its default when
unset and none when negative. A source past `MaxSourceBytes` is refused before it is parsed, as one
past the budget rather than one of a size: `MaxSourceBytes` is the compiler's `SourceLimit`, so a
caller may hand detection only its first bytes, one past the budget. A source whose YAML parses to
more than `MaxSourceNodes` nodes, or whose aliases add more than `MaxAliasSurplus` nodes to it, is
refused before its model is built. Each is an `openapi/budget-exceeded` error, compiling to no
Document. Past `MaxExternalDocuments`, a document an external reference names is not read, each
reference into it is unresolved ([`references`](#references)), and the compile reports
`openapi/budget-exceeded` once. The rule table ([`validation`](#validation)) reads no more objects
than the node and alias budgets admit together, without bound where either has none, and nests no
deeper than the scan's depth bound, so it stops on no document they admit; a check it does stop is
an `openapi/budget-exceeded` error at the object it stopped on. Three bounds hold whatever the
caller sets, since past them no answer the pre-parse scan gives is whole: a document nested past the
scan's depth bound, its aliases expanded, is refused as an `openapi/source-too-deep` error at the
least pointer where its nesting passes the bound; one with more nodes than the scan indexes, as an
`openapi/source-too-large` error; and one whose aliases expand it past a fixed multiple of its own
size, with a floor for a small document, as an `openapi/alias-amplification` error at the least
pointer of the smallest structures past the ratio. An overlay is held to the same refusals before it
is applied, and so is a document an external reference names, which fails the reference instead;
each is read no further than the byte budget. The member budget of an enum is
[`enum-const`](#enum-const)'s.

- Codes: `openapi/budget-exceeded`, `openapi/source-too-deep`, `openapi/source-too-large`,
  `openapi/alias-amplification`
- Tests: `openapi.TestCompile_SourceByteBudgetBindsOnlyPastTheLimit`,
  `openapi.TestCompiler_SourceLimitReadsTheCallersOptions`,
  `load.TestLoad_ANamedOverlayPastTheBudgetIsRefused`,
  `openapi.TestCompile_AnUnboundedAliasBudgetStillRefusesABomb`,
  `load.TestLoad_RefusesANestingPastTheBoundBeforeBuildingTheModel`,
  `load.TestLoad_ADocumentTooLargeToIndexIsRefused`,
  `openapi.TestCompile_TheDocumentBudgetIsTheCallersToSet`,
  `load.TestJudgeSource_ReadsEveryObjectTheCompilerAdmits`, `oas.TestCheck_WorkIsBounded`
- Status: partly built: an external object reached after its contents is charged against the
  document budget twice (#571), and long keys nested a few hundred levels deep cost gigabytes within
  every bound (#931).

### `overlays`

An OpenAPI Overlay document handed in as `Options.Overlay` is applied to the source's parsed tree
before its model is built. Its bytes are the caller's, or, when it hands none, read in the compile
from the local file `Overlay.Path` names, through the caller's reader and unconfined, since the
caller named it and not the spec. One the reader cannot read is a Go error, as an unreadable spec
is, and so are an overlay naming no file and one naming an http(s) URL, since no setting takes a
compile onto the network. Once applied, the pre-parse refusals and the node budgets hold the patched
tree as they hold the source, and validation and every lowering read it. Each action is charged the
nodes it can build before it runs: one whose charge would carry the tree, counted afresh, past
`MaxSourceNodes` is refused as an `openapi/budget-exceeded` error with nothing it would build built,
so it is refused while the tree is past the budget whatever a later action removes. An action that
builds nothing, such as a remove, is applied however large the tree, since the budget holds the tree
the overlay leaves. The overlay becomes a second entry in `Document.Sources`, and every position it
introduced or rewrote names that entry as its `Provenance.Source`, while a position it left alone
keeps the source's line and column. An overlay that does not parse, or is not a valid Overlay — no
version, no actions, an action naming no target — is refused as an `openapi/invalid-overlay` error
and nothing is applied. One the parser cannot apply as written, or whose grafted content expands
past the walk's node budget, is refused as an `openapi/overlay-failed` error. Either way the source
compiles to no Document rather than lowering a tree the caller believes was patched. Application is
strict unless `Overlay.Lax` is set: each action that changed nothing, or relies on JSONPath
behaviour the overlay did not opt into, is reported as an `openapi/overlay-action` warning naming
the action, and the compile proceeds; an action whose selector matches nothing is reported the same
way and refuses the overlay as `openapi/overlay-failed`. Lax application reports none of these, and
refuses only an overlay it cannot apply. Where the walk that attributes positions to the overlay
runs out of its node budget, the overlay is applied and every position names the source, as with no
overlay, reported as an `openapi/overlay-origin-incomplete` warning.

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
  `load.TestLoad_TheNodeBudgetReadsTheTreeTheOverlayLeaves`
- Status: partly built: an Overlay 1.0.0 document's RFC 9535 filter selector fails to apply (#651).
  Open: whether lax application should report an action that matches nothing (#652), how an update
  merges a nested array (#653), and whether an overlay that replaces a value the source declares is
  reported (#588).

### `validation`

The load phase holds the source to a table of the OpenAPI Specification's rules, at the minor it
declares, and the parser's own validation does not run. Each row of the table cites the sentence of
the specification it comes from. It gives every object's fields, the shape, values and syntax of
each, which fields each minor requires, and the rules that bind fields together; what data cannot
state is written as code beside it, such as a path template against its path parameters, a server
URL against its variables, a tag's parent, and where an encoding applies. Each finding is reported
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

The source is read as it writes it, an applied overlay's patch included, each object as the kind
its position gives it. An object a `$ref` reaches that the source's own reading did not take as
that kind, one under an `x-` key or in another document, is read as the kind the reference
expects and held to the source's minor. A finding about it is placed at its node, in the entry of
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
- Status: partly built: a document a reference reaches is held to the source's minor until which
  minor governs it is settled (#839).

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
- Status: open: which minor governs a document a reference reaches, held to the source's until it is
  settled (#839).

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
added and which [`reference-object-siblings`](#reference-object-siblings) folds at every version,
and every keyword beside a Schema Object's `$ref`, which 3.1 applies alongside the reference as JSON
Schema 2020-12 conjoins them ([`ref-use-site`](#ref-use-site)). That is the one case: 3.0 reads
either position as a Reference Object, which admits no key beside its `$ref` and says one SHALL be
ignored, so a 3.0 document writing one is warned that it was read as 3.1 reads it. What the keyword
then means is the 3.1 dialect's, so a `nullable` there, which no minor gives an effect beside a
`$ref`, is read by the policy [`nullable`](#nullable) sets out. A keyword whose meaning differs
between minors, such as `exclusiveMinimum`, follows the declared one
([`constraints`](#constraints)).

Whether a construct is a later minor's is asked in one place, the table [`validation`](#validation)
holds a document to, which gives the minor each field and value was added in; no lowering asks it,
so no field's treatment can depend on whether the parser happens to model it. The construct's value
is held to the shape its table row gives it, and any object inside it to the table like any other.
The keys beside a `$ref` are judged as the object they belong to: a Reference Object's, a Path
Item's own fields beside its `$ref`, or a schema's keywords, which the schema lowering reads beside a
3.0 `$ref` too. Those are held to 3.0's keywords as written and, since 3.0 ignores them, each is
warned of as a later minor's construct as well, whatever it is, so no row of the table names them.
An object a `$ref` reaches is judged as the source's own are, wherever it is written, and reported
where [`validation`](#validation) places a finding about it. A
Path Item's `additionalOperations` before 3.2, and a field 3.2 added to a Discriminator or XML
Object, draw the warning as any other construct does. A keyword a later minor's JSON Schema dialect
added is read by the schema lowering as it finds it, so the table leaves it alone. The same table
judges a Parameter Object's `allowReserved` against where the declared minor applies it
([`allow-reserved-locations`](#allow-reserved-locations)), and its `allowEmptyValue` against the
style it is written under ([`allow-empty-value`](#allow-empty-value)).

- Cases: `later-version-constructs`, `reached-outside-model`, `schema-ref-siblings-30`
- Codes: `openapi/later-version-construct`
- Tests: `e2e.TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem`,
  `e2e.TestSchemaRefSiblings_LowerAsIn31`,
  `oas.TestCheck_LaterConstructsAreWarnedOfInTheCompilersWords`,
  `oas.TestCheck_EveryKeyBesideA30SchemaRefIsALaterConstruct`,
  `oas.TestCheck_ValuesAddedLaterAreWarnedOf`,
  `oas.TestTable_AgreesWithTheConstructsLaterMinorsAdded`,
  `load.TestResolve_AReachedObjectIsHeldToTheLaterVersionTable`
- Status: partly built: a JSON Schema 2020-12 keyword in a 3.0 schema lowers with no diagnostic
  (#820). Open: which minor governs a document a reference reaches (#839).

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

Each document read is a `Document.Sources` entry after the source and its overlay, ordered by its
location from the source's directory, and what it declares lowers there, as the source's own lowers
where the source declares it: a schema a `$ref` lands on, and a parameter, request body, response,
header, example, callback, path item or security scheme a reference brings in, with the schemas
inline in it. A reference object written in what another document brings in is resolved against that
document, as the source's are against the source, and what it names lowers where it is declared, in
whichever document, the source included. What a document declares names its entry in its
`Provenance`, and has IDs in that document's namespaces, the source's qualified by its location
(`t/openapi@common.yaml/components/schemas/Pet`, [identity](../ir/identity.md)), so one
pointer in two documents is two nodes. An operation and a parameter keep the IDs of the mount that
lowers them.

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
fragment names the schema of that resource declaring it as its `$anchor` or `$dynamicAnchor`, and a
URI an `$id` declares names that schema, as one a `$ref`'s document part names; a name or a URI two
schemas declare names neither, and a name 2020-12's grammar refuses is declared by none
([`validation`](#validation) reports it). Identifiers are read only where the OpenAPI grammar places
a schema in the source, so one in an example, an extension, the fields beside a reference object's
`$ref`, or another document declares none. An `$id` names the URI its part before the `#` resolves
to against the base above it, by RFC 3986 where that base is a URI and as a `$ref`'s document is
placed where it is a file's path, and identifiers match as RFC 3986 §6.2.2 normalizes them. One with
a fragment that is not empty names none, as 2020-12 admits none, one naming the URI of the resource
holding it starts no resource, and one naming the source, however spelled, returns to the
document's. A URI no schema declares names the document at that URI, read as any other is: one read
against an `$id` is the URI the `$id` makes of it, never the file of that name beside the source,
and is judged for refusal as written while only relative paths moved its base, and as that URI once
an absolute `$id` did. A pointer passing a reference object reads on where the resolver resolved
that, in whichever document, and a `$ref` written in another document's content is read in that
document. What a `$ref` lands on in another document lowers where that document declares it; one in
the source lowers at the pointer the `$ref` spells. A use site takes what its schema `$ref`'s chain
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

Binding is held to one work budget, reported as an `openapi/budget-exceeded` error once it runs out,
the `$ref`s past it left unresolved. A `<<` merge chain deeper than the levels a mapping's merges
are expanded to (`nodeview.MergeDepthLimit`) is reported as an `openapi/degraded-construct` warning,
and a `$ref` past it is left unresolved, as is one that reads a key past it: a key its pointer
names, an `$id` that may give the base it is read against, or, for a `#/$defs/...` pointer, a
definition the rule reads before the one that would answer. So is a plain name no schema declares
where a schema merging past the bound may; a URI is read as a location all the same. A chain of
references that closes on itself is [`reference-cycles`](#reference-cycles)'.

- Cases: `schema-identifiers`
- Codes: `openapi/unresolved-ref`, `openapi/budget-exceeded`, `openapi/degraded-construct`
- Tests: `e2e.TestCompile_ExternalFileRefIsNotReadByDefault`,
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
  `bind.TestBind_StopsAtItsBudget`, `bind.TestBind_CountsWorkLinearInTheDocument`,
  `bind.TestBind_ReadsEveryWebhookAsAPathItem`, `bind.TestBind_PlacesAnEndAtTheNodeItsChainReached`,
  `e2e.TestReferenceEnds_AnEndingSlashIsReadWhereTheParserResolvedIt`,
  `e2e.TestReferenceEnds_AnEndInTheSourceIsReadThereUnderSelf`,
  `load.TestBindSchemas_PlacesEveryEndAndBindsEveryRefInTheCorpus`,
  `e2e.TestReferenceEnds_AnEndTheBinderCannotPlaceSaysWhy`,
  `bind.TestTable_SaysWhyARefWentUnread`,
  `bind.TestBind_SaysAKeyMayLiePastTheMergeBound`,
  `bind.TestBind_TakesNoDefinitionPastAReadTheMergeBoundStopped`,
  `canary.TestBindingOracle_AgreesWithTheLibrary`,
  `load.TestResolve_AFindingNamesTheDocumentItIsIn`,
  `load.TestResolve_ReachedFindingsDoNotDependOnDeclarationOrder`,
  `e2e.TestCompile_AReferenceObjectToANonObjectIsReportedOnce`
- Status: partly built: an `encoding` entry naming no part of its body is dropped with no record
  (#604). Another document's identifiers are not read, so a plain name naming one of its resources
  is reported unresolved (#1049). An absolute-path `$ref` in a remote document is read as a local
  path (#936); a remote document can make the compiler request a private address (#937); and a
  remote document reaching a local file is refused naming only its last such `$ref` (#940). A
  reference object's pointer holding a character above U+FFFF is refused as malformed (#516), and an
  unresolved reference object's message ends in the resolver's own wording, unless its chain ends
  on a position holding no object of its kind (#809).

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
home ([annotations](../ir/annotations.md#one-structural-home-per-declaration)). A value of
another shape, or a non-empty `nodeType` outside the five the specification defines, is kept
verbatim at the key and pointer the census itself uses, under `degraded_lowering`, with an
`openapi/degraded-construct` warning at the field, and the census finds the entry and adds nothing.
A key the raw mapping does not present directly — merged in through `<<` — is not read at all, so
the census still reports it, as an `openapi/unknown-key-unreachable` warning
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
merge keys, so a merged-in entry is kept under its own name, and an entry whose key is no scalar
under the empty name the parser reads it by ([`validation`](#validation) reports the key).

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
that decision lives ([unmodeled](../ir/unmodeled.md#reasons)), which is what keeps a
promotion pass from mistaking one for an oversight.

- Cases: `unreferenced-components`, `component-media-types-32`
- Codes: `openapi/degraded-construct`
- Tests: `document.TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget`,
  `document.TestUnreferencedComponents_ReadAsTheParserReadsThem`,
  `document.TestUnreferencedComponents_SelfReferenceByFileNameReaches`
- Status: partly built: no reference into `components/mediaTypes` is followed, so the section is
  kept whole however much of it a document uses (#856).

## 3. Names and values

### `naming`

A name the document spells lowers to `Naming{Source, Canonical}`: the spelling as written, beside
the word sequence [naming](../ir/naming.md) derives from it, at every position that spells one —
`info.title`, a tag, an `operationId`, a parameter, response-header or property name, a component
schema or security scheme key, an enum member's value as text, a 3.2 server `name`, and under
`grouping=path-prefix` a first path segment. A name with no letter or digit (`***`, `>=`) keeps its
`Source` and has no `Canonical`; an emitter names it by its characters
([naming](../emitters/refine/naming.md)). A name spelled as the empty string is minted
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
`$ref` lands on, in whichever document, and keeps its own name where that names nothing. `Aliases`
is never set.

- Cases: `neutral-naming`, `empty-names`
- Tests: `ids.TestComponentSchemaNamedEmpty_SeparatesAnEmptyNameFromNoEntry`,
  `schema.TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs`,
  `schema.TestInlinePosition_HintIsTheSameInBothOrders`,
  `operation.TestContent_ASharedBodyOrHeaderIsNamedForItsDeclaration`,
  `schema.TestComponentBody_SubtreeIsNamedByItsDeclaration`,
  `schema.TestUnionVariant_UnderPathsIsNamedAsTheNodeItHolds`,
  `schema.TestUnionVariant_AWholeDocumentIsNamedForIt`, `naming.TestAt_NamesASchemaAfterItsOwner`,
  `naming.TestAt_NamesABranchAfterItsTarget`, `naming.TestAt_NamesAStructureAReferenceReadsInsideRawYAML`
- Status: partly built: union variants and inline schemas ignore their `title` as a hint (#662).

### `values`

A default, `const`, enum member or example lowers to an `ir.Value` read off the YAML node by its
resolved tag alone, as the parser resolves it
([`scalar-resolution`](../parser-library.md#scalar-resolution)), never by the type the schema
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

The keywords written beside a schema's `$ref`, which 3.1 admits and which are read the same way in a
3.0 document ([`later-version-construct`](#later-version-construct)), and the annotations its target
declares — documentation, deprecation, visibility and `default` — merge onto the property, parameter
or header that references the target, field by field, a keyword the use site writes winning over the
target's ([annotations](../ir/annotations.md#what-a-reference-carries-down)), and by the same
rule at every carrier. At a position that carries no property, parameter or header — an `allOf`,
`oneOf` or `anyOf` branch, an `items`, a component — the keywords beside the `$ref` bind an alias
hoisted at that position instead ([models](../ir/models.md#composition-entries)). Constraints
are the exception: bounds conjoin rather than override, so each position keeps the ones it declared
and none is copied to a use site
([annotations](../ir/annotations.md#what-a-reference-carries-down)).

- Cases: `param-ref-inheritance`, `allof-ref-branch-siblings`, `readonly-writeonly`,
  `schema-ref-siblings-30`
- Tests: `e2e.TestAnnotationRetention`, `schema.TestModel_RefSiblingDescriptionWins`,
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
clarification of every 3.0 release. With no `type` beside it, or beside a `$ref`, it has no effect,
and 3.1 removed the keyword. Documents written to 3.0.0's looser wording, or carried to 3.1, still
use it to say that a schema admits null, and write `{$ref: T, type: [object, "null"]}` for the
nullable reference that `{$ref: T, nullable: true}` was meant to be. So the nullable-widening
policy, on by default, reads such a null as widening what it is written on rather than conjoining
with it: `nullable: true` admits null for the whole schema it is written on, whatever else that
schema says, and keywords beside a `$ref` that admit null by themselves admit it for the whole
reference. A `type` without `null` beside a `$ref` still narrows what it lands on. A reference whose
null only that reading admits names `nullable-widening` in `TypeRef.Inferred` (invariant 6), so
`Nullable` with no marker is the declared reading, and each such null is reported once as an info
`openapi/inferred-nullable`, at the `nullable` keyword or, for keywords beside a `$ref`, at the
schema that writes them, saying why its version does not admit it, whatever references its schema.
The policy is the Go option `Options.NullableWidening`, whose `Disabled` reads the keywords only as
their version defines them, and the text option `nullable-widening`, which `false` turns off. A
`nullable` in a 3.1 or 3.2 schema, or beside a `$ref`, is then a keyword its dialect does not
define, kept verbatim under `openapi:nullable` (`out_of_scope`) with an info
`openapi/unknown-schema-keyword`, where [`unknown-keys`](#unknown-keys) keeps a keyword the schema
model does not name.

- Cases: `nullable-30`, `nullable-31-ref`, `nullable-enum-31`, `nullability-four-states`,
  `nullability-conjunction`, `nullable-keyword-30`, `nullable-keyword-31`
- Codes: `openapi/inferred-nullable`, `openapi/unknown-schema-keyword`
- Tests: `schema.TestSchemaRef_NullableNormalization`,
  `schema.TestNullableWidening_EachShapeReadsByItsVersion`,
  `e2e.TestNullableWidening_AKeywordIsReportedOnceAtItself`,
  `e2e.TestNullableWidening_DisabledKeepsWhatTheDialectDoesNotDefine`,
  `e2e.TestNullableWidening_ACompositionKeepsTheMarker`,
  `merge.TestIntersectNull_TheMarkerFollowsTheBit`
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
([constraints](../ir/constraints.md)): `name`, `namespace`, `prefix` and `wrapped` as
written, and the node type from 3.2's `nodeType`
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

An `allOf` lowers to the composing `Model`'s `Base` and `Mixins`
([models](../ir/models.md#composition)). An entry that is a `$ref` to a schema taking part in
a discriminator hierarchy, or the sole `$ref` entry, becomes `Base`; every other `$ref` entry
becomes a `Mixin`; and an inline entry merges into the composing model's own `Properties`, each
property keeping its own provenance ([`allof-inline-residue`](#allof-inline-residue),
[`allof-redeclared-property`](#allof-redeclared-property)). So every branch stays reconstructible
from the composition.

`allOf` is associative, so an inline entry writing a non-empty `allOf` is flattened into the
enclosing list before the entries are classified: its own entries take its place, in source order
and depth first, each keeping the pointer it was written at, and its `properties` and `required`
merge after them, as a composing schema's own merge after its entries'. So `allOf: [{allOf: [A,
B]}]` classifies as `allOf: [A, B]`, and nothing is reported. Whatever else the entry writes that
the merge does not consume is kept beside the composition as residue
([`allof-inline-residue`](#allof-inline-residue)), so `{description: d, allOf: [A, B]}` composes `A`
and `B` and is also kept whole. Flattening stops 32 levels down: an entry nested inside 32 flattened
entries is not flattened itself, so its `allOf` is residue, and it is reported as nested past the
flattening depth of 32. This is not the flattening the
[design rules](../ir/README.md#design-rules) leave to emitters: every `$ref` entry still composes as
a `Base` or `Mixin`, and all that goes is a grouping no instance can observe, which an inline entry
owns no node to keep ([`allof-inline-residue`](#allof-inline-residue)).

- Cases: `allof-inheritance`, `allof-mixins`, `allof-inline-merge`, `allof-nested-inline`
- Tests: `schema.TestAllOf_DiscriminatorHierarchy`, `schema.TestAllOf_SoleRefBecomesBase`,
  `schema.TestAllOf_NestedInlineWrapperFlattens`, `schema.TestAllOf_NestedWrapperBaseSelection`,
  `schema.TestAllOfBranches_DepthCapKeepsWrapperVerbatim`,
  `schema.TestAllOfBranches_DepthCapNamesWhatElseTheWrapperWrites`,
  `schema.TestAllOf_NestedWrapperWithResidueKeptVerbatim`, `schema.TestAllOf_NestedLowersAsFlat`
- Status: partly built: a `$ref` to a subtype of a discriminated base, which declares no
  discriminator itself, is composed as a `Mixin` rather than the `Base` (#986).

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
That merge consumes the branch's `properties`, its
`required` list, a `type` that is exactly `object` — which the composed `Model` already restates —
and, for an entry flattened under [`allof-classification`](#allof-classification), its `allOf` — and
nothing else. The IR could hold every other keyword a branch writes, and does hold them one level up
— the same keywords on the composing schema lower to `Docs`, `Model.Additional`, `Model.Constraints`
and `Unmodeled` — but attributing them to the composition means either giving the branch a node
(changing the composition shape) or merging them upward with a precedence rule for branches that
disagree. So a branch declaring anything the merge does not consume is kept verbatim in
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
the source contradicts — `allOf: [{type: string, maxLength: 3}]` composes an empty model of kind
`model` over a source that says `string`. Two branch kinds are outside this rule rather than limits
of it: a `$ref` branch owns a node, so what is written beside the `$ref` is *modeled* on the branch
alias [models](../ir/models.md#composition-entries) describes rather than kept as residue,
and a boolean branch declares no keywords at all, so there is no residue to derive — it is a
decision about the composed node instead, stated with the `false`-schema rule
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
  `schema.TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice`
- Status: partly built: an `allOf` of one `$ref` and an annotation-only branch keeps none of the
  branch's annotations typed (#660). An `allOf` whose branches are all primitives lowers to a model
  with no properties, each branch kept as residue (#1043).

### `allof-redeclared-property`

`allOf` intersection folds a field declared in more than one branch into one `Property`
([models](../ir/models.md#the-flattening)), which keeps the first declaration's identity unless
another model carries that declaration. The fold reads every declaration of the field at once,
once every type has lowered, and decides each fact from the whole set, so neither the order of the
branches nor where a component is declared decides what the field holds; declaration order breaks
only a tie, and the merge reports each one. One judgment compares two declarations' types and
returns one of six verdicts: the same type, either one narrower, an overlap, disjoint, or unrelated.
A disjoint pair is unsatisfiable but for a null both declarations admit: no other value satisfies
both. An overlap shares values and is merely not narrowed to one. The arms are tried in this order.
`{}` loses to anything but the node a degraded lowering mints (an enum past its member budget, an
unconvertible const), which is unrelated to every type; where `{}` loses, the winner's own null
admission stands alone, since `{}` admits null yet lowers non-nullable. Two bare primitives compare
by value domain and spelling (`integer` against `number`, `uuid` against `string`, `int32` against
`int64`). A `Scalar` or `Enum` against a bare primitive wins over a kind it equals or narrows
(`string` against `format: byte`, or an unknown format over bare `string`), is disjoint from a kind
of another family, or from another string format where neither is `bytes` (`uuid` against
`format: date`), and overlaps any other kind (`uuid` against `format: byte`); for an `Enum`, that
overlap is disjoint where the kind's judge refuses every member. An enum with no members is refused
vacuously, so it is disjoint from any kind it would otherwise overlap, `bytes` included; an enum
with members against `bytes` stays an overlap. A `const` wins when the kind admits its value; where
it does not, it is disjoint from a kind with a judge and overlaps `bytes`, which has none. Two
enums, two `const`s or two `Scalar`s equal in every field but where they were declared (IDs, names,
whether hoisted inline, and the provenance of the node and of what it keeps under `Unmodeled`) are
one type where both were written inline at the field's own declarations, in their document, as the
same declaration written twice; a node declared elsewhere, a component or a schema a `$ref` reaches
in this document or another, wins over an inline copy of itself in either order, and two such nodes
are two types the source declared apart, which overlap. Two lists, or two maps with one key, compare
by element or value, nested up to a bound, and the narrower one wins only where it admits null no
more widely than the other and the other holds no constraint, encoding, documentation, examples or
`Unmodeled` the winner lacks; disjoint members still overlap where both admit null, or where both
containers admit the empty one. Two alike in every member and in that detail are judged as two
equal enums are: one type where both were written inline, the one declared elsewhere winning over
an inline copy in either order, and two declared elsewhere overlapping. A structure is narrower than
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
in a kind or another `const`; and unrelated where nothing says what they share, as between two
models neither of which composes the other, a model and a map, a list and a tuple, an opaque scalar
and a kind, or a union or an external and anything else. A pair gets the same verdict as a field's
types and as two containers' members.

The field takes the type of the declaration whose type is the same as, or narrower than, the most
declarations' types, the first of them on a tie. Where one declaration's type is within every other's
it is the intersection, and it wins whatever the order. Each declaration whose type the winner's is
the same as or narrower than folds into the field, which adopts its shape detail; every other one is
dropped. So `int32`, `uint32` and `uint16` fold to `uint16` with nothing dropped, in every order,
while `int32` and `uint32` alone keep the first declared and report the other.

Two models neither of which holds the other are the exception, since an instance satisfying both is
a value of a type neither names. Where the declarations whose types no other declaration holds are
two or more models, each named directly or through an alias, the field's type is a `Model` the merge
mints: anonymous, holding no property of its own, and composing each of those types once as a
written `allOf` of them composes ([`allof-classification`](#allof-classification)). The one written
as the only `$ref`, or else the first `$ref` whose model carries a discriminator, is its `Base`, and
the rest are `Mixins` in declaration order. So a field declared as `Meeting` and as an inline object
names one type holding both, whichever branch comes first, rather than flattening them into one
property list. Whatever another declaration's type holds adds no conjunct: a `{}`, a model saying
nothing but `type: object` beside one saying more, and a model another declaration composes. Two
models saying nothing, declared apart, are two types neither of which holds the other, so they
compose, and an object saying anything else composes too, so what it says stays reachable. Two
aliases of one model are two types, each able to say more than the model, so they compose, and so
does an alias of a bare object beside another model. Any other type beside the models (a map, a
kind, a degraded node) leaves the choice to the rule above. Every declaration then folds into the
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
or nothing. The winning type admits a value by its kind and by the constraints its node carries:
each bound, `multipleOf`, a length counted in code points, an item or property count and
`uniqueItems`, while a `pattern`, which nothing here evaluates, admits no string. Constraints
written on the property rather than the node are not judged. Nothing judges a value against a model,
and no declaration's type is a composed field's own, so a composed field takes no value but a null
it admits. A value the field does not take is reported in an info `openapi/degraded-construct` at
each declaration that wrote it, which is kept verbatim beside the field. Of the values it takes, the
first written is kept, and a declaration giving another is reported and kept; two values are one
where they are one JSON value, a number by its magnitude and an object by its members in any order.
Values are refused before one is kept, so a value the field takes is never lost to a refused one
written before it. Constraint keywords merge
over the declarations folded into the field, the first to set a keyword keeping it; the title,
description, external links, deprecation, XML hints and each `Unmodeled` key over every declaration,
the first to give one keeping it, and a declaration giving one differently is reported and kept. A
field keeping the first declaration's identity keeps that declaration's provenance, and either way
it names the heuristic that promoted its deprecation from an extension, wherever the deprecation it
keeps came from.

The IR has no combinator for "string here, integer there", nor a second slot for a default, title,
description, external link, examples, deprecation or XML hint another declaration already fills, so
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
warning. Each diagnostic sits at the declaration that lost and names the one whose say the field
kept. A dropped declaration is reported against the winner as an `openapi/conflicting-redeclaration`
warning where their types are incompatible — disjoint primitive kinds (`string` against `integer`,
`uuid` against `date-time`), an enum none of whose members the other kind admits, a scalar against a
structure, two structures of different instance types, or two lists or maps whose members share no
value where one is never empty. Types that share values but that the merge does not narrow to one
(`int32` against `uint32`, an enum of `integer` against an `int32` that admits one of its members,
two enums of one kind, each with members, a `const` against a kind with no judge, against a node
that ends in a kind or against another `const`, or two lists or maps whose members share only null or share nothing while
both admit the empty one) are named in an info `openapi/degraded-construct`, and so are two types
the judgment does not compare (a model and a map, a list and a tuple, a union, an opaque scalar, a
node a degraded lowering minted, or two models beside a type that keeps them from composing), since
which of them the field keeps then follows declaration order. No declaration is dropped without a
diagnostic. A dropped declaration's `default`, constraints and examples go with
its type rather than folding onto the winner — they describe the shape that lost, and adopting them
would leave one field asserting two contradictory things, with nothing downstream comparing a
value's kind to its property's type; this is keyed on the type being dropped, not on the drop being
called a conflict. A declaration folded into the field is reported as well where it pins a
constraint keyword the field keeps to another value, an `openapi/conflicting-redeclaration` warning
naming the declaration the value was kept from, or gives a detail differently from the one kept, an
info `openapi/degraded-construct`; differing only in nullability, it takes no entry: the merged
field admits null only where every folded declaration does, which is the conjunction a single
schema's null verdicts fold under ([`nullable`](#nullable)).

The fold waits for every type to lower because the judgment reads the nodes the declarations name, a
component declared after the schema composing it or a node a `$ref` into another document promises
among them; it finishes the model before any other decision reads it (`compile.Types.Complete`), so
a form part's flags read the merged field. A fold judges every pair of declarations, so it judges a
field's first 64 at most; each later one is kept verbatim, unjudged, under an
`openapi/budget-exceeded` error at the first of them.

- Cases: `allof-conflicting-type`, `allof-narrowing-redeclaration`, `allof-redeclared-models`
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
  `merge.TestFold_DeclarationsPastTheBoundAreKeptUnjudged`,
  `merge.TestFold_ADroppedDeclarationStillSaysWhatIsNotShape`,
  `merge.TestFold_UnmodeledKeepsTheFirstOfAKeyAndReportsAnother`,
  `merge.TestFold_AValueSpelledTwoWaysIsOneValue`,
  `merge.TestFold_ThePromotionMarkerFollowsTheDeprecationKept`,
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
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`
- Status: partly built: two distinct nodes of related kinds (`format: hex` against `format: byte`,
  two enums whose members fit each other's kind, two different literals), and a list or map whose
  broader side carries detail the narrower lacks, are reported as an overlap and never narrowed
  (#1042); two models alike in every property compose rather than collapse to one type (#1083); a
  model and a map or a union are reported as types the merge does not compare (#1182), and so are two
  lists, tuples or maps whose members are different models (#1180); a model, an alias of `{}` and
  another model keep the first and report the rest (#1194); a model and the wrapper it
  flattens that fold different sets only because the model stops at the flattening depth are refused
  as `compile/registry-refused`, not told apart, since one declaration names both. Open:
  intersecting the redeclared bounds and defaults rather than keeping one, and judging an adopted
  value against the constraints written on the property (#10).

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
model would trade one silent loss for another. A `true` conjunct admits everything, so contributing
nothing from it is exact and nothing is reported. Where a `oneOf` or `anyOf` beside the `allOf` is
distributed ([`union-beside-structure`](#union-beside-structure)), every composed variant is closed
and keeps the branch, and the branch is reported once.

- Cases: `allof-boolean-branch`, `allof-nested-inline`
- Codes: `openapi/false-schema`
- Tests: `schema.TestAllOf_NestedFalseBranchClosesModel`,
  `schema.TestAllOf_FalseBranchBesideUnionClosesEveryVariant`

### `union`

A `oneOf` lowers to a `Union` with `Exclusive` set and an `anyOf` to one without it
([unions](../ir/unions.md)): one `Variant` per branch, in source order, each a reference to
the branch's lowering — a `$ref` branch to its target, an inline one to its own node or primitive
([`inline-hoisting`](#inline-hoisting)) — and never collapsed into optional fields. The union is
untagged, `WireTagged` false, unless a `discriminator` beside it routes it
([`discriminator`](#discriminator)): the tag property's value then names the variant, so the union
is `WireTagged`, an `anyOf` as much as a `oneOf` ([unions](../ir/unions.md)). A branch set of
one schema and `null` ([`union-null-variant`](#union-null-variant)), of `null` alone
([`union-null-only`](#union-null-only)), and one written beside structure, a `$ref` or the other
combinator ([`union-beside-structure`](#union-beside-structure),
[`union-beside-ref`](#union-beside-ref), [`competing-keywords`](#competing-keywords)) lower
otherwise.

Each variant's `Name.Hint` is its branch's: a `$ref` branch's is its target's name, and an inline
branch's its position (`variant_1`). An inline branch whose node is a decorator
([annotations](../ir/annotations.md#decorators)) over a `$ref` its `allOf` names, the one way
3.0 annotates a branch, takes the hint that `$ref` takes as a branch of its own, as 3.1's spelling
of the same branch, `{$ref: X, description: …}`, does, and the position's where that names nothing.
Its node keeps the positional hint, which an outside `$ref` to the branch derives too; no emitter
renders it, since a decorator collapses into its reference.

- Cases: `oneof-discriminated`, `anyof-discriminated`, `anyof-untagged`, `union-decorated-branch`
- Tests: `schema.TestUnion_ADecoratedBranchIsHintedAsItsRef`

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

- Cases: `keywords-at-every-outcome`, `type-array-numeric`
- Tests: `schema.TestLower_MultiTypeUnion`,
  `schema.TestLower_ATypeArrayIsExclusiveOnlyOverDisjointTypes`,
  `schema.TestLower_ATypeListedTwiceIsNotExclusive`,
  `schema.TestDiscriminatorBesideUnion_MultiTypedBodyIsSettled`
- Status: partly built: a multi-typed base's discriminator routes to no subtype (#853), and a value
  bound lands on any variant with a `Constraints` field, whether or not it constrains that variant's
  type (#1310).

### `union-null-variant`

A `oneOf` or `anyOf` with exactly one branch that is not a bare `type: null`, beside one or more
that are, lowers to a nullable reference to that branch's lowering rather than to a `Union`
([type-graph](../ir/type-graph.md#type-references)). The collapse is held to a `$ref` site's
rule ([models](../ir/models.md#composition-entries)), since it too lowers to a reference: a
keyword beside it that no reference holds — a `discriminator`, a `format` other than `password`, a
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
combinator: `Base`/`Mixins` ([models](../ir/models.md#composition-entries)) composes one
referent per conjunct, the shape `allOf` writes, and cannot state "conjoined with a union", and
[boundaries](../ir/boundaries.md#deliberate-exclusions) records why none was added. Target
languages are not the constraint here; TypeScript writes `{a: string} & (X | Y)` directly. Where the
conjunction *distributes*, [models](../ir/models.md#composition-beside-a-union) gives the
rule: the body is composed into each variant, and the result is an ordinary `Union` of composed
`Model`s, reported as an info `openapi/composition-lowering` at the schema, since nothing is lost
but the IR's shape no longer mirrors the source's, bar one thing: a `false` in the `allOf` states
that the composition admits nothing, which every composed variant then holds only as the closed
model [`allof-false-branch`](#allof-false-branch) gives it, with the branch kept on each. Five
shapes distribution cannot reach:

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
a body the move leaves holding a map and nothing else lowers to that map's `MapT`
([containers](../ir/containers.md)). A union whose branches declare no shape
(`validation_only`) is held to the same rule, and so is a multi-typed body, which lowers to a
`Union` whose object variant, built from the same schema, holds the discriminator. The compiler
decides once every node is interned (`compile.Settlement`), because neither the walk nor the source
schemas can answer it: a subtype declared after the `Model` has no `Base` while the `Model` lowers,
and a schema whose `allOf` names the `Model` may still lower to no subtype — an `enum`, a `const`,
an alias over a `$ref` beside it, or a union its `allOf` is distributed across.

A union whose branches declare no shape at all (`oneOf: [{required: [a]}, {required: [b]}]`) is not
a degradation and is not one of the five: it narrows the body without reshaping it, which is
validation logic, so it is kept under `validation_only` with the keyword family of
[boundaries](../ir/boundaries.md#validation-only-constructs) and reported as an info
`openapi/validation-only-keyword` at the schema ([`validation-only`](#validation-only)).

- Cases: `allof-oneof-cooccurrence`, `discriminated-union-beside-model`, `unhomed-keywords`,
  `keywords-at-every-outcome`
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
[`union-beside-structure`](#union-beside-structure) applies instead and both branch sets are kept. A
keyword the *elected* lowering never reads — `type: string` beside an `allOf`, `format` beside a
`const` — is kept by the census [`shape-applicator`](#shape-applicator) runs, which asks the node
that was built: `type: object` beside an `allOf` is one the composed `Model` already asserts, and
keeps nothing.

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
  type two variants alias, which a valid document can write (#1298). Open: inferring a
  discriminator from pinned tag values, as an injectable policy (#659).

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
  `schema.TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue`,
  `schema.TestAllOf_ExtensionSubtypeIsTaggedOnlyByAMapping`

### `string-const-union`

A `oneOf` or `anyOf` whose every branch is a string `const` is to normalize to a closed `Enum` in a
`pass/` normalization, not in the compiler, so that each branch's `Docs` survive until a pass
chooses the collapse.

- Status: not built: the compiler keeps such a union a `Union` of `Literal`s with each branch's
  docs, and no pass normalizes it to an `Enum` (#990).

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
`Literal` of its value, and one that cannot convert to the top type, with an
`openapi/degraded-construct` warning at the schema. A
`$ref` into a member of such a union names the member's `Literal`, the node the enumeration lowers
at that position, whichever is lowered first.

- Cases: `enum-string`, `enum-numeric`, `empty-enum`, `enum-member-identity`, `nullable-enum-31`,
  `literal-const`
- Codes: `openapi/empty-enum`, `openapi/duplicate-enum-value`, `openapi/degraded-construct`,
  `openapi/budget-exceeded`
- Tests: `schema.TestEnum_HeterogeneousBecomesUnionWithBadValue`,
  `schema.TestEnum_NonScalarAndMidListMismatch`,
  `schema.TestEnum_ValueTypeReadsTheFormatItsMembersFit`,
  `schema.TestEnum_ReportsWhyAFormatStayedVerbatim`,
  `schema.TestEnum_NarrowsARedeclaredPrimitiveInEitherOrder`,
  `schema.TestEnumAsUnion_UnconvertibleMemberBecomesAny`,
  `schema.TestEnumAsUnion_ExclusiveWhereNoInstanceMatchesTwoEntries`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`
- Status: partly built: an enum whose members do not match its declared type lowers to a union with
  only an info diagnostic (#619); and a `Literal` carries no value type or format (#668). Open:
  whether a mixed-type enum's `null` lifts out of its union (#618).

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
Nor is a schema reported beside a `$ref`, whose siblings 3.0 ignores, an inline `allOf` branch,
which merges in place, or a subschema kept verbatim, such as one under `not`.

- Cases: `missing-array-items`
- Codes: `openapi/missing-array-items`
- Tests: `schema.TestLower_MissingArrayItems`, `schema.TestLower_ListWithoutItems`,
  `schema.TestLower_MissingArrayItemsAtAReferencedPositionWarnsOnce`

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
bound on an object is kept the same way, and a keyword the elected lowering of
[`competing-keywords`](#competing-keywords) never reads is too. `discriminator` joins the list,
homed by a `Model` or `Union` that routes by it and by nothing else, so one beside any other node —
the top type, an enum, a literal, a scalar, an alias over a `$ref` — is kept the same way. At a
`$ref` position the census runs on the alias the use-site rule of
[models](../ir/models.md#composition-entries) hoists, since the siblings bind the position rather
than the referent. Every outcome of a schema runs it, and each says what it lowered to. A union
holds what a node of its kind holds. A collapsed union and an expanded `$dynamicRef` lower to a
reference, so they are read as a `$ref` position is. A multi-typed schema
(`type: [string, object]`) and a union distributed over its branches are built from the one schema
per variant, so the census asks the variants, and a keyword is homed where any holds it: a
distributed union's `properties` are consumed, and a `format` or `maxItems` beside them is kept on
the union, under a report naming a union of models.

- Cases: `unhomed-keywords`, `empty-enum`, `keywords-at-every-outcome`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestObject_ACatchAllBesideARequiredNameStaysAModel`,
  `schema.TestUnhomedApplicator_KeptOnAListAndAnnounced`,
  `schema.TestUnhomedApplicator_FormatWithNoTypeIsKept`
- Status: partly built. Open: whether `format: set` on an array reads as `uniqueItems` (#638).

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

- Cases: `encoding-byte`, `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_UnknownFormatPerBaseType`,
  `schema.TestLower_NamedScalarComponentResolves`,
  `spelling.TestFormatTable_EveryRowDistinguishesTheBareType`,
  `spelling.TestFormatTable_EveryStringRowLowersToItsPrimitive`
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
closed tuple's `items: false`, is lowered for the `$ref` alone.

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
- Tests: `schema.TestLower_DepthCapExceeded`,
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
  `anchors.TestBuild_ANullAnchorNamesNothing`,
  `oas.TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional`
- Status: partly built: a keyword written `null` beneath the same keyword, such as an `items` inside
  an `items`, turns the outer position into the `false` schema (#1299).

## 7. Schemas: keywords beyond the shape

### `constraints`

`minimum`, `maximum` and `multipleOf` lower to `Min`, `Max` and `MultipleOf`, and `minLength`,
`maxLength` and `pattern` to `MinLength`, `MaxLength` and `Pattern` as written, in the `Constraints`
of the node or carrier the position owns (a `Scalar`, a `Property` or a `Parameter`); `minItems`,
`maxItems` and `uniqueItems` to the `List`'s, and a `Tuple`'s per [`tuples`](#tuples); and
`minProperties` and `maxProperties` per [`additional-properties`](#additional-properties). A bound
is read off the raw node, never the parser's `float64`, so its `BigVal` keeps every digit the
literal wrote ([`values`](#values)), a quoted one as the decimal it spells; one that is no decimal
literal a `BigVal` holds is dropped with an `openapi/invalid-numeric-literal` error at the schema
that wrote it. Exclusive bounds follow the declared dialect. In 3.0 the boolean modifies the bound
beside it: `true` moves that bound to `ExclusiveMin` or `ExclusiveMax` and clears `Min` or `Max`, so
a 3.0 document and its 3.1 translation lower alike, `false` states nothing, and `true` with no bound
beside it bounds nothing and is kept verbatim under `openapi:exclusiveMinimum` or
`openapi:exclusiveMaximum` (`degraded_lowering`) with an `openapi/degraded-construct` warning. In
3.1 and 3.2 the keyword is a bound of its own, kept beside `Min`/`Max` when both are declared, a
field apiece and no `Unmodeled` entry ([constraints](../ir/constraints.md)). The other
dialect's form (a boolean in 3.1 or 3.2, a number in 3.0) is dropped with an
`openapi/invalid-exclusive-bound` error. A bound beside a node with no `Constraints` field to take
it (an `Enum`, a `Literal`, a `List` for a value bound) is kept verbatim under `openapi:<keyword>`
(`degraded_lowering`) with an info `openapi/degraded-construct` naming the node it lowered to, as
[`shape-applicator`](#shape-applicator) keeps a collection bound on an object.

- Cases: `constraints`, `numeric-precision`
- Codes: `openapi/invalid-numeric-literal`, `openapi/invalid-exclusive-bound`,
  `openapi/degraded-construct`
- Tests: `schema.TestConstraints_ExclusiveBoolean30`,
  `schema.TestConstraints_ExclusiveWrongDialectForm`,
  `schema.TestConstraints_LosslessNumericLiterals`,
  `schema.TestConstraints_ABoundPastTheExponentRangeIsRefused`
- Status: partly built: an integer bound past what a 64-bit integer holds lowers as `0`, unreported
  (#1050).

### `validation-only`

JSON Schema's validation-only keywords are kept verbatim under `validation_only`
([boundaries](../ir/boundaries.md#validation-only-constructs)) on the node or carrier the
position owns, one entry per family: `not`, `dependentSchemas`, `dependentRequired` and
`propertyNames` each under `openapi:<keyword>`, at the keyword; and three families each folded into
one object, at the schema — `if`, `then` and `else` under `openapi:if-then-else`, `contains` with
`minContains` and `maxContains` under `openapi:contains`, and `unevaluatedItems` with an
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
would make the reference depend on which position happened to lower first
([models](../ir/models.md#composition-beside-a-union)), so it is preserved instead.

Anchors are counted through YAML aliases and merge keys, so an anchor reached only by an alias still
makes its name a duplicate. The walk is bounded by depth and by node count, and says so when it
truncates, once, with an `openapi/degraded-construct` warning at the document, since a truncated
index undercounts declarations and an undercount is what turns "irreducible" into a confident
expansion. A keyword written beside a reference that expands is read as one beside a `$ref` is
([`shape-applicator`](#shape-applicator)).

- Cases: `dynamic-ref`, `keywords-at-every-outcome`
- Codes: `openapi/dynamic-ref-expanded`, `openapi/degraded-construct`
- Tests: `schema.TestDynamicRef_IrreducibleIsKeptAndSaysWhy`,
  `anchors.TestBuild_CountsWhatAnAliasBringsIn`,
  `lowering.TestDynamicAnchorSites_ReportsATruncatedWalkOnce`
- Status: partly built: a merge chain past the view's merge bound hides an `$id`, so a `$dynamicRef`
  can expand across a resource boundary (#401), and the truncation warning names the walk's own
  bounds when the merge bound stopped it (#454). A `$dynamicRef` written in another document is not
  expanded, since only the source's anchors are indexed (#1031).

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
kept on the Document the same way, every schema read in the OpenAPI dialect whatever it names.
`$comment` is dropped outright — 2020-12 §8.3 forbids presenting it to end users, so no emitter may
see it. `$dynamicAnchor` is consumed as a reference target rather than carried as content — it is
what lets a `$dynamicRef` expand ([`dynamic-ref`](#dynamic-ref)), and a `$ref` naming it reads it as
an `$anchor`. `$anchor` is consumed the same way: a `$ref` naming one that
[`references`](#references) reads resolves to the schema declaring it.

- Cases: `dialect-keywords`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestDialectKeywords_KeptOutOfScope`, `document.TestMeta_JSONSchemaDialectIsKept`

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
mounts no operation registers its servers too, named by nothing, and keeps its `x-*` names on the
service instead, or on the declaring operation's HTTP binding for a callback expression, under
`openapi:pathItem/<item pointer>/<key>`, the item's pointer in the key since one carrier holds every
such item; it is reported as one `openapi/degraded-construct` warning at the mount.

- Cases: `webhooks`, `callbacks`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperations_PathItemServersNamedOnEveryRoute`,
  `operation.TestOperations_OwnServersOverrideThePathItems`,
  `operation.TestOperations_ServersEachLevelAlone`,
  `servers.TestRegistry_DeclaredOneDeclarationLowersOnce`,
  `operation.TestPathItem_WithNoOperationKeepsWhatItWroteOnTheService`
- Status: open: an explicitly empty list declares nothing to name, as an absent one does, and
  whether it should override the list above it is undecided (#1081).

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
or not it lowers, and a list another document writes where it lowers; the rule table
([`validation`](#validation)) states this rule and leaves it to this one.

- Cases: `param-styles`, `param-style-matrix`, `param-identity`
- Codes: `openapi/duplicate-parameter`
- Tests: `operation.TestParams_RepeatedNameAndLocationKeepsEveryParameter`
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
- Status: open: which minor governs a parameter another document holds, held to the source's until
  it is settled (#839).

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
key escaped as a pointer token, or the top type where it writes none. A request body's keys also go,
as written and in order, to `HTTPBinding.RequestContentTypes`. A response whose `content` is absent
or empty has no `Payload` ([`request-body`](#request-body) for the request side). A key is
classified on a copy that is lowercased, since a media type's names are case-insensitive (RFC 6838
§4.2), trimmed and cut at its first `;`, and the IR never keeps the copy: `multipart/*` and
`application/x-www-form-urlencoded` read the media type's `encoding` as part encodings
([`multipart-encoding`](#multipart-encoding)), and a binary body is a file
([`file-bodies`](#file-bodies)); the streaming policy reads the same copy
([`streaming-media`](#streaming-media)). No other key is classified, so `application/*+json`,
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
`content` left out is [`minor-required-fields`](#minor-required-fields)'.

- Cases: `empty-request-content`, `request-body-docs`
- Codes: `openapi/empty-request-content`
- Tests: `operation.TestContent_NonRequiredRequestBody`,
  `operation.TestLowerRequestBody_AnEmptyContentKeepsTheBody`

### `file-bodies`

A content entry whose schema is `type: string` with `format: binary`, read through a `$ref` and
whatever its media type, or whose schema is absent or boolean on `application/octet-stream`, matched
on the normalized key ([`media-types`](#media-types)), lowers to `Content.File` with `IsText` false
and `ContentTypes` the key as written, so that a body to stream is not read as a model.
`Content.Type` stays what the schema lowered to, which keeps its docs, bounds and examples
reachable, and is the non-nullable `bytes` primitive only where the schema is absent or `true` and
states nothing. No other `FileInfo` field is set. The same holds for a response and an error case.
An `octet-stream` entry with a schema that is not `string` with `binary`, a `format: byte` or
`contentMediaType` string, and a schema-less entry on any other media type are ordinary Contents.

- Cases: `file-body`
- Tests: `operation.TestContent_BinaryOctetStreamBody`,
  `operation.TestContent_MixedCaseOctetStreamBodyIsFile`,
  `operation.TestContent_BinaryRefBodyDetectedAsFile`,
  `operation.TestContent_AFileBodyKeepsTheSchemaItWrites`
- Status: partly built: the 3.1 idioms for a file, `contentMediaType` without `contentEncoding` and
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
`Content.Item`. On a media type whose body is one document, JSON's or XML's by name or `+json` or
`+xml` suffix, and which the streaming policy ([`streaming-media`](#streaming-media)) does not name,
`itemSchema` has no sequence to describe: it lowers all the same, with a warning
`openapi/item-schema-on-document` at it. Which other media types repeat a structure is no closed
set, so nothing else draws one.

- Cases: `sequential-media`
- Codes: `openapi/degraded-construct`, `openapi/item-schema-on-document`
- Tests: `operation.TestStreaming_AnItemSchemaOnOneDocumentIsReported`,
  `operation.TestStreaming_AnItemSchemaIsItsFrameWhateverItHolds`,
  `lowering.TestHoldsOneDocument_AnswersOnlyForJSONAndXML`

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
  offering a plain body and an event stream does not record which content streams (#601); and an
  event stream's terminator, and a response that streams only when a request field asks it to, have
  no IR home (#666, #667).

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
the part and never from the `encoding` map, read once every type has lowered. Where the body model
has the property and its type is a list, a tuple, a primitive, or a scalar whose bases end in one of
those or in no base, they are read off that type, the one the `allOf` merge settled on, of the
declaration the key names: `Multi` where it ends in a list or tuple, `Filename` where it ends in
`bytes` (`format` `binary` or `byte`). For a property of any other kind, such as a union, enum,
literal, model or map, and for a body with no model, they are read from every declaration of the
part's name: `Multi` where any is an array, `Filename` where any is a `string` with `format`
`binary` or `byte`. A part is entered only when its `PartEncoding` holds something, so a body that
declares no encoding still gets entries for its array and file parts, and an entry declaring nothing
leaves none. What an Encoding Object writes beyond those fields is
[`encoding-allow-reserved`](#encoding-allow-reserved). An encoding written where OpenAPI gives it no
effect is reported as an `openapi/validation` warning, of the class `validation-no-effect`, at the
field ([`validation`](#validation)): an `encoding` under a media type that is neither multipart nor
form, which lowers nowhere, or before 3.2 outside a request body, and a 3.2 `prefixEncoding` or
`itemEncoding` under one that is not multipart. A media type is read without its parameters and in
any case.

- Cases: `multipart-encoding`
- Codes: `openapi/degraded-construct`, `openapi/validation`
- Tests: `operation.TestContent_MultipartPartEncoding`,
  `oas.TestCheck_EncodingsApplyWhereTheSpecificationSays`,
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
  `operation.TestPartEncodings_CutPastReachedPart`
- Status: partly built: an `encoding` key that names no property, or one declared only inside a
  `oneOf` or `anyOf` branch, is dropped with no diagnostic (#604); on a multi-typed body the entry
  is keyed by a property no node holds, which validation then refuses (#744); an array of files is
  not marked as files (#605); and an encoding under a media type it does not apply to is kept in no
  form (#1047).

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
([http-binding](../ir/http-binding.md)). A key is a runtime expression, by the grammar each
minor gives, text embedding runtime expressions in braces, as OpenAPI's own example does, or a
constant URL, which embeds none; one that is none of them is reported as an `openapi/validation`
error, of the class `validation-invalid-format`, at the key ([`validation`](#validation)), and
lowers as written.

- Cases: `callbacks`
- Codes: `openapi/validation`
- Tests: `operation.TestCallbacks_NameWithSlashKeepsItsOwnKey`,
  `oas.TestCheck_CallbackKeysAreRuntimeExpressions`
- Status: partly built: callbacks declared on an operation inside a callback are dropped in silence
  (#858); and a callback operation is not marked inbound, and a callback has no name (#625). Open:
  whether a callback operation inherits the service's auth (#626).

## 13. Security

### `security-requirements`

`components/securitySchemes` lowers to the Document's auth schemes, each under an ID derived from
its pointer, and a `security` list — the document's to the Service's, an operation's to the
Operation's — to an OR of ANDs: each requirement object one alternative, naming its schemes, each
with the scopes it lists ([auth](../ir/auth.md)). An operation that writes no `security`
inherits the document's, and one writing `security: []` is public; the two stay apart, absent
against empty ([operations](../ir/operations.md)). The document's list keeps the same two
spellings apart in `Service.Auth`. A requirement naming a scheme the section
does not declare is reported as an `openapi/unresolved-ref` error at the requirement and dropped. An
entry that omits the field naming its mechanism — `type`, or for `http` the `scheme` token — is
interned as no scheme, reported as an `openapi/incomplete-security-scheme` error at the entry, and
every requirement naming it is dropped. 3.2's device flow lowers to `Flows` beside the other flows,
and `oauth2MetadataUrl` to `OAuth2MetadataURL`. An `oauth2` scheme whose `flows` declares no flow —
`flows: {}`, only `x-*` and keys that name no flow, or only flows written null — and that has no
`oauth2MetadataUrl` lowers as declared, reported as an `openapi/oauth2-no-flow` warning at the
declaration; a `flows` left out, null or no mapping is the rule table's to report instead
([`minor-required-fields`](#minor-required-fields)). The table states the rules this one reports, a
requirement naming no scheme and a scheme naming no mechanism, and leaves them to it.

- Cases: `security-schemes`, `security-or-and`, `security-root-empty`, `oauth2-no-flow`
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

The document's `servers` lower to `Document.Servers`, each server registered at the pointer of its
Server Object, as its ID's path and its provenance ([servers](../ir/servers.md)), and the
service names them, in order, on `Service.Servers`: `url` to `URLTemplate` as written, `description`
to its docs, 3.2's `name` to its `Name` ([`naming`](#naming)), each entry of `variables`, in the
order written, to a variable with its `default`, `enum` and `description`, and the server's `x-*`
and undeclared keys to its own `Unmodeled`. A document whose `servers` is absent or empty is served
at the one server OpenAPI supplies, at `/`, minted as `server/default` and recording no pointer,
since nothing in the document writes it. A name one servers list repeats — the document's, a path
item's or an operation's, compared as written at every version, since the parser reads `name` on
each — is kept as declared on both entries and reported as an `openapi/duplicate-server-name`
warning at the repeat's name, naming the first.

- Cases: `servers-variables`
- Codes: `openapi/duplicate-server-name`
- Tests: `servers.TestDuplicateServerName_BothServersLower`,
  `servers.TestRegistry_RootServesADocumentWithNoServersAtTheRoot`,
  `servers.TestRegistry_RootNamesTheDeclaredServersInSourceOrder`,
  `load.TestServerRepeats_ReportsEveryListTheSourceWrites`,
  `load.TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly`
- Status: partly built: a variable's `default` cannot tell an empty default from none (#883); and a
  servers list only a `$ref` reaches is never checked for a repeated name (#880).

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
policy is `Options.Promotions`, on by default: its `Targets` replace the default mapping —
`x-deprecated-reason` to `Deprecation.Message`, `x-deprecated-since` to `Deprecation.Since`,
`x-sunset` to `Deprecation.RemovalDate`, and `x-extensible-enum` to an `Enum`'s openness — and its
`Disabled` turns it off. A deprecation member is filled on an operation, a parameter, a header, a
property, a type node or a security scheme that is deprecated; on one that is not, the key annotates
nothing and stays where it is. A node a promotion writes into lists the marker `extension-promotion`
in its `Provenance.Inferred`, and the extension stays in `Unmodeled` untouched.

A value the mapped field cannot hold — anything but text, for the four `Deprecation` members — is
reported as an info `openapi/degraded-construct` at the extension and not coerced, since the
document means something else by the key. Text of the right JSON shape is taken as written:
`x-sunset` fills `RemovalDate` and not `RemovalVersion` because the header it echoes ([RFC
8594](https://www.rfc-editor.org/rfc/rfc8594)) is a date by definition, and the mapping is where
that reading is stated — the promotion does not then parse the date to confirm it. No default key
names `RemovalVersion`: a document stating a removal *version* names its own key, per rule 1 of
[unmodeled](../ir/unmodeled.md#promotion).

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
  `e2e.TestPromotion_RemovalDateAndVersionAreSeparateFacts`
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
